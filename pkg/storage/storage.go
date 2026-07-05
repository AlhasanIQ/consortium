package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/pkg/seeds"
	_ "modernc.org/sqlite"
)

var (
	// ErrNotFound is returned when a resource is not found
	ErrNotFound = errors.New("not found")
	// ErrConflict is returned when optimistic-lock expectations are not met.
	ErrConflict = errors.New("conflict")
)

//go:embed schema.sql
var schema string

// WorkflowExecution represents a workflow execution in the database
// Previously named "Job" - renamed for semantic clarity since all executions are workflows
type WorkflowExecution struct {
	ID                string     `json:"id"`
	Description       string     `json:"description"` // Workflow description (stored as 'query' in DB)
	Model             string     `json:"model"`
	Status            string     `json:"status"`
	RequestData       string     `json:"request_data,omitempty"`
	ResponseData      string     `json:"response_data,omitempty"`
	ResultText        string     `json:"result_text,omitempty"`
	TokensInput       int        `json:"tokens_input"`
	TokensOutput      int        `json:"tokens_output"`
	TokensTotal       int        `json:"tokens_total"`
	Cost              float64    `json:"cost"`
	ErrorMessage      string     `json:"error_message,omitempty"`
	RetryCount        int        `json:"retry_count"`
	WorkflowID        string     `json:"workflow_id,omitempty"`
	ParentExecutionID string     `json:"parent_execution_id,omitempty"` // For parent/child linkage
	IdempotencyKey    string     `json:"idempotency_key,omitempty"`
	RequestHash       string     `json:"request_hash,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	ArchivedAt        *time.Time `json:"archived_at,omitempty"`

	// Stream/event durability fields
	LastEventSequence int64 `json:"last_event_sequence"` // Atomic sequence counter for events

	// Execution fingerprint for config-level dedup/reproducibility.
	ConfigHash string `json:"config_hash,omitempty"` // Deterministic fingerprint of workflow config

	// Optional user scope for deduplication.
	UserID string `json:"user_id,omitempty"`

	// Durable runtime identity fields.
	WorkflowExecutionID string `json:"workflow_execution_id,omitempty"` // Stable logical execution identity
	RunID               string `json:"run_id,omitempty"`                // Per-run identity (changes on rollover)
	RunNumber           int    `json:"run_number,omitempty"`            // 1-based run counter within execution
	PreviousRunID       string `json:"previous_run_id,omitempty"`       // Links to previous run on rollover
	DAGSnapshot         string `json:"dag_snapshot,omitempty"`          // Frozen canonical workflow JSON
	DAGHash             string `json:"dag_hash,omitempty"`              // SHA256 of dag_snapshot
}

// Job is a compatibility alias retained during the Job -> WorkflowExecution rename.
type Job = WorkflowExecution

// WorkflowNode represents a single node in a workflow execution
type WorkflowNode struct {
	ID           int       `json:"id"`
	ExecutionID  string    `json:"execution_id"` // DB column: job_id
	RunID        string    `json:"run_id,omitempty"`
	NodeID       string    `json:"node_id"`
	NodeType     string    `json:"node_type"`
	NodeOrder    int       `json:"node_order"`
	Status       string    `json:"status"`
	NodeLabel    string    `json:"node_label,omitempty"` // Human-readable label (e.g., "Claude")
	NodeName     string    `json:"node_name,omitempty"`  // Full name (e.g., "Claude Response")
	Prompt       string    `json:"prompt,omitempty"`
	Model        string    `json:"model,omitempty"`
	Output       string    `json:"output,omitempty"`
	TokensInput  int       `json:"tokens_input"`
	TokensOutput int       `json:"tokens_output"`
	Cost         float64   `json:"cost"`
	LatencyMs    float64   `json:"latency_ms"`
	ErrorMessage string    `json:"error_message,omitempty"`
	ErrorCode    string    `json:"error_code,omitempty"`
	Metadata     string    `json:"metadata,omitempty"`
	ActivityID   string    `json:"activity_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	// Node recovery fields.
	ExecutionUID  string     `json:"execution_uid,omitempty"`  // Deterministic: job_id:node_id:attempt
	AttemptNumber int        `json:"attempt_number"`           // Attempt number (1-based)
	StartedAt     *time.Time `json:"started_at,omitempty"`     // When node execution started
	CompletedAt   *time.Time `json:"completed_at,omitempty"`   // When node execution completed
	ParentNodeID  string     `json:"parent_node_id,omitempty"` // Parent node ID (for aggregation child nodes)
}

// WorkflowDefinition represents a stored workflow definition
type WorkflowDefinition struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Definition  string    `json:"definition"` // JSON of complete workflow file format
	Version     int       `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Storage manages job persistence
type Storage struct {
	db            *sql.DB
	dbPath        string
	dbStatsCancel context.CancelFunc
}

// ExecutionFilters for filtering job listings
type ExecutionFilters struct {
	Status     string
	WorkflowID string
	ConfigHash string // Filter by config fingerprint.
}

// PaginatedExecutions is the result of a paginated execution query
type PaginatedExecutions struct {
	Executions []WorkflowExecution `json:"executions"`
	NextCursor string              `json:"next_cursor,omitempty"`
	HasMore    bool                `json:"has_more"`
}

// cursorData holds decoded cursor information
type cursorData struct {
	Time time.Time
	ID   string
}

// SeedWorkflowInfo is a compatibility alias for seeds.Info.
type SeedWorkflowInfo = seeds.Info

// DB returns the underlying *sql.DB connection.
// This is used by the admin server for raw SQL queries.
func (s *Storage) DB() *sql.DB {
	return s.db
}

// NewStorage creates a new storage instance
func NewStorage(dbPath string) (*Storage, error) {
	// TODO(v0.1-security): File-backed SQLite databases inherit host directory
	// permissions. Deployment tooling should create DB directories with
	// restrictive permissions and document backup/encryption expectations.
	// Add connection parameters for better concurrency handling
	// _busy_timeout: wait up to 5s for locks to clear
	// _journal_mode=WAL: Write-Ahead Logging for better concurrent access
	// _synchronous=NORMAL: balance between safety and performance
	dsn := sqliteDSNWithPragmas(dbPath)
	dbDiagConfig := dbQueryDiagnosticsConfigFromEnv()
	configureDBQueryDiagnostics(dbDiagConfig)

	db, err := sql.Open(diagnosticDriverName(dbDiagConfig), dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool limits for SQLite.
	// File-backed databases default to a small WAL-friendly read pool; in-memory
	// databases must remain single-connection because each connection gets its own DB.
	maxOpenConns, maxIdleConns := sqliteConnectionPoolLimits(dbPath)
	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleConns)
	db.SetConnMaxLifetime(0)

	// Initialize schema
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}
	// Keep planner statistics fresh for newly added indexes.
	// PRAGMA optimize is lightweight and may trigger targeted ANALYZE work.
	if _, err := db.Exec(`PRAGMA optimize`); err != nil {
		log.Printf("Warning: failed to run PRAGMA optimize: %v", err)
	}

	storage := &Storage{db: db, dbPath: dbPath}

	if err := storage.runStartupMigrations(); err != nil {
		return nil, fmt.Errorf("failed startup migrations: %w", err)
	}

	// Seed all workflows if they don't exist
	if err := storage.seedWorkflows(); err != nil {
		log.Printf("Warning: failed to seed workflows: %v", err)
	}
	if err := storage.seedDefaultAPIModelRoutes(); err != nil {
		log.Printf("Warning: failed to seed API model routes: %v", err)
	}

	storage.startDBStatsLogger(dbDiagConfig.StatsInterval)

	return storage, nil
}

func sqliteDSNWithPragmas(dbPath string) string {
	separator := "?"
	if strings.Contains(dbPath, "?") {
		separator = "&"
	}
	pragmas := []string{
		"_pragma=" + url.QueryEscape("busy_timeout(5000)"),
		"_pragma=" + url.QueryEscape("journal_mode(WAL)"),
		"_pragma=" + url.QueryEscape("synchronous(NORMAL)"),
	}
	return dbPath + separator + strings.Join(pragmas, "&")
}

func sqliteConnectionPoolLimits(dbPath string) (maxOpenConns, maxIdleConns int) {
	maxOpenConns = parsePositiveEnvInt("DB_MAX_OPEN_CONNS", 4)
	if isInMemorySQLiteDSN(dbPath) {
		maxOpenConns = 1
	}
	maxIdleConns = parsePositiveEnvInt("DB_MAX_IDLE_CONNS", maxOpenConns)
	if maxIdleConns > maxOpenConns {
		maxIdleConns = maxOpenConns
	}
	return maxOpenConns, maxIdleConns
}

func isInMemorySQLiteDSN(dbPath string) bool {
	lower := strings.ToLower(strings.TrimSpace(dbPath))
	return lower == ":memory:" || strings.HasPrefix(lower, "file::memory:") || strings.Contains(lower, "mode=memory")
}

func parsePositiveEnvInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return fallback
	}
	return n
}

// Close closes the database connection
func (s *Storage) Close() error {
	if s.dbStatsCancel != nil {
		s.dbStatsCancel()
	}
	return s.db.Close()
}

// encodeCursor creates a cursor from timestamp and ID
func encodeCursor(t time.Time, id string) string {
	data := fmt.Sprintf("%d:%s", t.UnixNano(), id)
	return base64.StdEncoding.EncodeToString([]byte(data))
}

// decodeCursor parses a cursor string
func decodeCursor(cursor string) (*cursorData, error) {
	decoded, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return nil, fmt.Errorf("failed to decode cursor: %w", err)
	}

	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid cursor format")
	}

	nanos, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid timestamp in cursor: %w", err)
	}

	return &cursorData{
		Time: time.Unix(0, nanos),
		ID:   parts[1],
	}, nil
}

func (s *Storage) resolveRunID(jobID, preferredRunID string) string {
	if strings.TrimSpace(preferredRunID) != "" {
		return preferredRunID
	}

	var runID string
	err := s.db.QueryRow(`
		SELECT COALESCE(NULLIF(run_id, ''), id)
		FROM jobs
		WHERE id = ?
	`, jobID).Scan(&runID)
	if err != nil || strings.TrimSpace(runID) == "" {
		return jobID
	}
	return runID
}

// MarshalJSON is a helper to marshal data to JSON string
func MarshalJSON(v interface{}) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
