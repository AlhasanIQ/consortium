package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// sqlTime formats a time.Time for consistent SQL storage as a UTC RFC 3339 string.
// This avoids storing Go's time.Time.String() representation, which includes
// timezone offsets and monotonic clock readings that break string-based sorting.
func sqlTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// sqlTimePtr is like sqlTime but for optional times; returns nil when zero.
func sqlTimePtr(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return sqlTime(t)
}

// BenchmarkRunSummaryInput is a storage-facing run summary payload.
type BenchmarkRunSummaryInput struct {
	RunID                     string
	Benchmark                 string
	Split                     string
	ItemLimit                 int
	WorkflowID                string
	WorkflowName              string
	DatasetPath               string
	TotalItems                int
	CompletedItems            int
	FailedItems               int
	ParsedItems               int
	CorrectItems              int
	Accuracy                  float64
	ParseRate                 float64
	RetriedItems              int
	TotalAttempts             int
	TotalNonLetterRetries     int
	AdmissionRetries          int
	ItemsWithAdmissionRetries int
	FailureReasonCounts       map[string]int
	AllAttemptFailureCounts   map[string]int
	TotalLatencyMS            float64
	AvgLatencyMS              float64
	P50LatencyMS              float64
	P95LatencyMS              float64
	P99LatencyMS              float64
	TotalTokensInput          int
	TotalTokensOutput         int
	TotalTokens               int
	AvgTokensPerItem          float64
	TotalCostUSD              float64
	AvgCostUSDPerItem         float64
	StartedAt                 time.Time
	CompletedAt               time.Time
	ElapsedSeconds            float64
	ExecutionEngine           string
	ExecutionEngineNotes      string
}

// BenchmarkRunResultInput is a storage-facing full run payload.
type BenchmarkRunResultInput struct {
	Summary BenchmarkRunSummaryInput
	Items   []BenchmarkRunItemInput
}

// BenchmarkRunPersistMeta controls how benchmark run metadata is persisted.
type BenchmarkRunPersistMeta struct {
	Status        string
	Source        string
	ArtifactPath  string
	OptRunID      string
	OptOrganismID string
}

// BenchmarkRun is the persisted summary for one benchmark run.
type BenchmarkRun struct {
	ID                        string         `json:"id"`
	Benchmark                 string         `json:"benchmark"`
	Split                     string         `json:"split"`
	ItemLimit                 int            `json:"item_limit"`
	WorkflowID                string         `json:"workflow_id"`
	WorkflowName              string         `json:"workflow_name"`
	DatasetPath               string         `json:"dataset_path"`
	Status                    string         `json:"status"`
	TotalItems                int            `json:"total_items"`
	CompletedItems            int            `json:"completed_items"`
	FailedItems               int            `json:"failed_items"`
	ParsedItems               int            `json:"parsed_items"`
	CorrectItems              int            `json:"correct_items"`
	Accuracy                  float64        `json:"accuracy"`
	ParseRate                 float64        `json:"parse_rate"`
	RetriedItems              int            `json:"retried_items"`
	TotalAttempts             int            `json:"total_attempts"`
	TotalNonLetterRetries     int            `json:"total_non_letter_retries"`
	AdmissionRetries          int            `json:"admission_retries"`
	ItemsWithAdmissionRetries int            `json:"items_with_admission_retries"`
	FailureReasonCounts       map[string]int `json:"failure_reason_counts"`
	AllAttemptFailureCounts   map[string]int `json:"all_attempt_failure_counts"`
	TotalLatencyMS            float64        `json:"total_latency_ms"`
	AvgLatencyMS              float64        `json:"avg_latency_ms"`
	P50LatencyMS              float64        `json:"p50_latency_ms"`
	P95LatencyMS              float64        `json:"p95_latency_ms"`
	P99LatencyMS              float64        `json:"p99_latency_ms"`
	TotalTokensInput          int            `json:"total_tokens_input"`
	TotalTokensOutput         int            `json:"total_tokens_output"`
	TotalTokens               int            `json:"total_tokens"`
	AvgTokensPerItem          float64        `json:"avg_tokens_per_item"`
	TotalCostUSD              float64        `json:"total_cost_usd"`
	AvgCostUSDPerItem         float64        `json:"avg_cost_usd_per_item"`
	StartedAt                 *time.Time     `json:"started_at"`
	CompletedAt               *time.Time     `json:"completed_at"`
	ElapsedSeconds            float64        `json:"elapsed_seconds"`
	ExecutionEngine           string         `json:"execution_engine"`
	ExecutionEngineNotes      string         `json:"execution_engine_notes"`
	Source                    string         `json:"source"`
	OptRunID                  string         `json:"opt_run_id"`
	OptOrganismID             string         `json:"opt_organism_id"`
	ArtifactPath              string         `json:"artifact_path"`
	CreatedAt                 time.Time      `json:"created_at"`
	UpdatedAt                 time.Time      `json:"updated_at"`
}

// BenchmarkSplitOption is a (benchmark, split) pair for filter dropdowns.
type BenchmarkSplitOption struct {
	Benchmark string `json:"benchmark"`
	Split     string `json:"split"`
}

// UpsertBenchmarkRunResult stores one benchmark run summary and all item outcomes.
func (s *Storage) UpsertBenchmarkRunResult(result *BenchmarkRunResultInput, meta BenchmarkRunPersistMeta) error {
	const maxAttempts = 20
	backoff := 50 * time.Millisecond
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := s.upsertBenchmarkRunResultOnce(result, meta); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if !IsRetryableSQLiteError(lastErr) || attempt == maxAttempts {
			break
		}
		time.Sleep(backoff)
		if backoff < time.Second {
			backoff *= 2
		}
	}
	return lastErr
}

func normalizeBenchmarkRunStatus(status string) string {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "queued", "running", "completed", "failed", "cancelled", "imported":
		return strings.TrimSpace(strings.ToLower(status))
	default:
		return "completed"
	}
}

func normalizeBenchmarkRunSource(source string) string {
	switch strings.TrimSpace(strings.ToLower(source)) {
	case "manual", "benchloop", "optimizer", "imported", "replay":
		return strings.TrimSpace(strings.ToLower(source))
	default:
		return "manual"
	}
}

func (s *Storage) upsertBenchmarkRunResultOnce(result *BenchmarkRunResultInput, meta BenchmarkRunPersistMeta) error {
	if result == nil {
		return fmt.Errorf("benchmark result is nil")
	}
	if strings.TrimSpace(result.Summary.RunID) == "" {
		return fmt.Errorf("benchmark summary.run_id is required")
	}

	status := normalizeBenchmarkRunStatus(meta.Status)
	source := normalizeBenchmarkRunSource(meta.Source)
	itemLimit := result.Summary.ItemLimit
	if itemLimit < 0 {
		itemLimit = 0
	}
	artifactPath := strings.TrimSpace(meta.ArtifactPath)
	optRunID := strings.TrimSpace(meta.OptRunID)
	optOrganismID := strings.TrimSpace(meta.OptOrganismID)

	failureCountsJSON, err := marshalIntMap(result.Summary.FailureReasonCounts)
	if err != nil {
		return fmt.Errorf("marshal failure_reason_counts: %w", err)
	}
	attemptFailureCountsJSON, err := marshalIntMap(result.Summary.AllAttemptFailureCounts)
	if err != nil {
		return fmt.Errorf("marshal all_attempt_failure_counts: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin benchmark run tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	startedAt := sqlTimePtr(result.Summary.StartedAt)
	completedAt := sqlTimePtr(result.Summary.CompletedAt)

	now := sqlTime(time.Now())
	_, err = tx.Exec(`
			INSERT INTO benchmark_runs (
				id, benchmark, split, item_limit, workflow_id, workflow_name, dataset_path, status,
				total_items, completed_items, failed_items, parsed_items, correct_items, accuracy, parse_rate,
				retried_items, total_attempts, total_non_letter_retries, admission_retries, items_with_admission_retries,
				failure_reason_counts, all_attempt_failure_counts,
				total_latency_ms, avg_latency_ms, p50_latency_ms, p95_latency_ms, p99_latency_ms,
				total_tokens_input, total_tokens_output, total_tokens, avg_tokens_per_item,
				total_cost_usd, avg_cost_usd_per_item,
				started_at, completed_at, elapsed_seconds,
				execution_engine, execution_engine_notes, source, opt_run_id, opt_organism_id, artifact_path,
				created_at, updated_at
			) VALUES (
				?, ?, ?, ?, ?, ?, ?, ?,
				?, ?, ?, ?, ?, ?, ?,
				?, ?, ?, ?, ?,
				?, ?,
				?, ?, ?, ?, ?,
				?, ?, ?, ?,
				?, ?,
				?, ?, ?,
				?, ?, ?, ?, ?, ?,
				?, ?
			)
			ON CONFLICT(id) DO UPDATE SET
				benchmark = excluded.benchmark,
				split = excluded.split,
				item_limit = excluded.item_limit,
				workflow_id = excluded.workflow_id,
				workflow_name = excluded.workflow_name,
				dataset_path = excluded.dataset_path,
			status = excluded.status,
			total_items = excluded.total_items,
			completed_items = excluded.completed_items,
			failed_items = excluded.failed_items,
			parsed_items = excluded.parsed_items,
			correct_items = excluded.correct_items,
			accuracy = excluded.accuracy,
			parse_rate = excluded.parse_rate,
			retried_items = excluded.retried_items,
			total_attempts = excluded.total_attempts,
			total_non_letter_retries = excluded.total_non_letter_retries,
			admission_retries = excluded.admission_retries,
			items_with_admission_retries = excluded.items_with_admission_retries,
			failure_reason_counts = excluded.failure_reason_counts,
			all_attempt_failure_counts = excluded.all_attempt_failure_counts,
			total_latency_ms = excluded.total_latency_ms,
			avg_latency_ms = excluded.avg_latency_ms,
			p50_latency_ms = excluded.p50_latency_ms,
			p95_latency_ms = excluded.p95_latency_ms,
			p99_latency_ms = excluded.p99_latency_ms,
			total_tokens_input = excluded.total_tokens_input,
			total_tokens_output = excluded.total_tokens_output,
			total_tokens = excluded.total_tokens,
			avg_tokens_per_item = excluded.avg_tokens_per_item,
			total_cost_usd = excluded.total_cost_usd,
			avg_cost_usd_per_item = excluded.avg_cost_usd_per_item,
			started_at = excluded.started_at,
			completed_at = excluded.completed_at,
			elapsed_seconds = excluded.elapsed_seconds,
				execution_engine = excluded.execution_engine,
				execution_engine_notes = excluded.execution_engine_notes,
				source = excluded.source,
				opt_run_id = excluded.opt_run_id,
				opt_organism_id = excluded.opt_organism_id,
				artifact_path = excluded.artifact_path,
				updated_at = excluded.updated_at
		`,
		result.Summary.RunID,
		result.Summary.Benchmark,
		result.Summary.Split,
		itemLimit,
		result.Summary.WorkflowID,
		result.Summary.WorkflowName,
		result.Summary.DatasetPath,
		status,
		result.Summary.TotalItems,
		result.Summary.CompletedItems,
		result.Summary.FailedItems,
		result.Summary.ParsedItems,
		result.Summary.CorrectItems,
		result.Summary.Accuracy,
		result.Summary.ParseRate,
		result.Summary.RetriedItems,
		result.Summary.TotalAttempts,
		result.Summary.TotalNonLetterRetries,
		result.Summary.AdmissionRetries,
		result.Summary.ItemsWithAdmissionRetries,
		failureCountsJSON,
		attemptFailureCountsJSON,
		result.Summary.TotalLatencyMS,
		result.Summary.AvgLatencyMS,
		result.Summary.P50LatencyMS,
		result.Summary.P95LatencyMS,
		result.Summary.P99LatencyMS,
		result.Summary.TotalTokensInput,
		result.Summary.TotalTokensOutput,
		result.Summary.TotalTokens,
		result.Summary.AvgTokensPerItem,
		result.Summary.TotalCostUSD,
		result.Summary.AvgCostUSDPerItem,
		startedAt,
		completedAt,
		result.Summary.ElapsedSeconds,
		result.Summary.ExecutionEngine,
		nullIfEmpty(result.Summary.ExecutionEngineNotes),
		source,
		nullIfEmpty(optRunID),
		nullIfEmpty(optOrganismID),
		artifactPath,
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("upsert benchmark run: %w", err)
	}

	if _, err := tx.Exec(`DELETE FROM benchmark_run_items WHERE run_id = ?`, result.Summary.RunID); err != nil {
		return fmt.Errorf("delete existing benchmark run items: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM benchmark_run_item_attempts WHERE run_id = ?`, result.Summary.RunID); err != nil {
		return fmt.Errorf("delete existing benchmark run item attempts: %w", err)
	}

	for _, item := range result.Items {
		if err := upsertBenchmarkRunItemSummaryReplaceTx(tx, result.Summary.RunID, &item); err != nil {
			return fmt.Errorf("insert benchmark run item %s: %w", item.ItemID, err)
		}

		if err := insertBenchmarkRunItemAttemptsTx(tx, result.Summary.RunID, item.ItemID, item.AttemptDetails, 0); err != nil {
			return fmt.Errorf("insert benchmark attempts %s: %w", item.ItemID, err)
		}
	}

	// Dual-write normalized failure counts for cross-run SQL aggregation.
	if _, err := tx.Exec(`DELETE FROM benchmark_run_failure_counts WHERE run_id = ?`, result.Summary.RunID); err != nil {
		return fmt.Errorf("delete existing benchmark failure counts: %w", err)
	}
	for reason, count := range result.Summary.FailureReasonCounts {
		if count <= 0 {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO benchmark_run_failure_counts (run_id, failure_reason, scope, count) VALUES (?, ?, 'final', ?)`,
			result.Summary.RunID, reason, count); err != nil {
			return fmt.Errorf("insert benchmark failure count (final) %s: %w", reason, err)
		}
	}
	for reason, count := range result.Summary.AllAttemptFailureCounts {
		if count <= 0 {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO benchmark_run_failure_counts (run_id, failure_reason, scope, count) VALUES (?, ?, 'all_attempts', ?)`,
			result.Summary.RunID, reason, count); err != nil {
			return fmt.Errorf("insert benchmark failure count (all_attempts) %s: %w", reason, err)
		}
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("commit benchmark run tx: %w", err)
	}
	committed = true
	return nil
}

// benchmarkRunColumns is the canonical SELECT column list for BenchmarkRun.
const benchmarkRunColumns = `id, benchmark, split, item_limit, workflow_id, workflow_name, dataset_path, status,
	total_items, completed_items, failed_items, parsed_items, correct_items, accuracy, parse_rate,
	retried_items, total_attempts, total_non_letter_retries, admission_retries, items_with_admission_retries,
	failure_reason_counts, all_attempt_failure_counts,
	total_latency_ms, avg_latency_ms, p50_latency_ms, p95_latency_ms, p99_latency_ms,
	total_tokens_input, total_tokens_output, total_tokens, avg_tokens_per_item,
	total_cost_usd, avg_cost_usd_per_item,
	started_at, completed_at, elapsed_seconds,
	execution_engine, COALESCE(execution_engine_notes, ''), source, COALESCE(opt_run_id, ''), COALESCE(opt_organism_id, ''), artifact_path,
	created_at, updated_at`

// scanBenchmarkRunRows scans all rows into BenchmarkRun slices.
func scanBenchmarkRunRows(rows *sql.Rows) ([]BenchmarkRun, error) {
	out := make([]BenchmarkRun, 0)
	for rows.Next() {
		run, err := scanBenchmarkRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *run)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ListLatestBenchmarkRunsByGroup returns the most recent run per workflow for a
// given benchmark+split combination. Results are ordered by accuracy descending.
func (s *Storage) ListLatestBenchmarkRunsByGroup(benchmarkName, split string) ([]BenchmarkRun, error) {
	query := `SELECT ` + benchmarkRunColumns + ` FROM benchmark_runs
		WHERE benchmark = ? AND split = ?
		  AND (benchmark, split, workflow_id, started_at) IN (
		      SELECT benchmark, split, workflow_id, MAX(started_at)
		      FROM benchmark_runs
		      WHERE benchmark = ? AND split = ?
		      GROUP BY benchmark, split, workflow_id
		  )
		ORDER BY accuracy DESC`
	rows, err := s.db.Query(query, benchmarkName, split, benchmarkName, split)
	if err != nil {
		return nil, fmt.Errorf("list latest benchmark runs by group: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanBenchmarkRunRows(rows)
}

// ListAllBenchmarkRunsForSplit returns all runs for a benchmark+split, ordered by
// started_at descending (most recent first). No deduplication by workflow.
func (s *Storage) ListAllBenchmarkRunsForSplit(benchmarkName, split string, includeOptimizer bool) ([]BenchmarkRun, error) {
	query := `SELECT ` + benchmarkRunColumns + ` FROM benchmark_runs
		WHERE benchmark = ? AND split = ?`
	if !includeOptimizer {
		query += ` AND source != 'optimizer'`
	}
	query += `
		ORDER BY started_at DESC, created_at DESC`
	rows, err := s.db.Query(query, benchmarkName, split)
	if err != nil {
		return nil, fmt.Errorf("list all benchmark runs for split: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanBenchmarkRunRows(rows)
}

// ListDistinctBenchmarkSplits returns distinct (benchmark, split) pairs from stored runs.
func (s *Storage) ListDistinctBenchmarkSplits(includeOptimizer bool) ([]BenchmarkSplitOption, error) {
	query := `SELECT DISTINCT benchmark, split FROM benchmark_runs`
	if !includeOptimizer {
		query += ` WHERE source != 'optimizer'`
	}
	query += ` ORDER BY benchmark, split`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("list distinct benchmark splits: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]BenchmarkSplitOption, 0)
	for rows.Next() {
		var opt BenchmarkSplitOption
		if err := rows.Scan(&opt.Benchmark, &opt.Split); err != nil {
			return nil, fmt.Errorf("scan benchmark split option: %w", err)
		}
		out = append(out, opt)
	}
	return out, nil
}

// ListBenchmarkRuns returns recent benchmark runs with optional filters.
func (s *Storage) ListBenchmarkRuns(limit int, benchmark, workflowID, status, splitFilter string, includeOptimizer bool) ([]BenchmarkRun, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	conditions := make([]string, 0, 3)
	args := make([]interface{}, 0, 4)
	if benchmark = strings.TrimSpace(benchmark); benchmark != "" {
		conditions = append(conditions, "benchmark = ?")
		args = append(args, benchmark)
	}
	if workflowID = strings.TrimSpace(workflowID); workflowID != "" {
		conditions = append(conditions, "workflow_id = ?")
		args = append(args, workflowID)
	}
	if status = strings.TrimSpace(status); status != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, status)
	}
	if splitFilter = strings.TrimSpace(splitFilter); splitFilter != "" {
		conditions = append(conditions, "split = ?")
		args = append(args, splitFilter)
	}
	if !includeOptimizer {
		conditions = append(conditions, "source != 'optimizer'")
	}

	query := `SELECT ` + benchmarkRunColumns + ` FROM benchmark_runs`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY COALESCE(started_at, created_at) DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list benchmark runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanBenchmarkRunRows(rows)
}

// GetBenchmarkRun returns one benchmark run by run_id.
func (s *Storage) GetBenchmarkRun(runID string) (*BenchmarkRun, error) {
	row := s.db.QueryRow(`SELECT `+benchmarkRunColumns+` FROM benchmark_runs WHERE id = ?`, runID)
	run, err := scanBenchmarkRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return run, nil
}

func scanBenchmarkRun(scanner interface {
	Scan(dest ...interface{}) error
}) (*BenchmarkRun, error) {
	var run BenchmarkRun
	var startedAt sql.NullTime
	var completedAt sql.NullTime
	var failureCountsJSON string
	var attemptFailureCountsJSON string
	err := scanner.Scan(
		&run.ID,
		&run.Benchmark,
		&run.Split,
		&run.ItemLimit,
		&run.WorkflowID,
		&run.WorkflowName,
		&run.DatasetPath,
		&run.Status,
		&run.TotalItems,
		&run.CompletedItems,
		&run.FailedItems,
		&run.ParsedItems,
		&run.CorrectItems,
		&run.Accuracy,
		&run.ParseRate,
		&run.RetriedItems,
		&run.TotalAttempts,
		&run.TotalNonLetterRetries,
		&run.AdmissionRetries,
		&run.ItemsWithAdmissionRetries,
		&failureCountsJSON,
		&attemptFailureCountsJSON,
		&run.TotalLatencyMS,
		&run.AvgLatencyMS,
		&run.P50LatencyMS,
		&run.P95LatencyMS,
		&run.P99LatencyMS,
		&run.TotalTokensInput,
		&run.TotalTokensOutput,
		&run.TotalTokens,
		&run.AvgTokensPerItem,
		&run.TotalCostUSD,
		&run.AvgCostUSDPerItem,
		&startedAt,
		&completedAt,
		&run.ElapsedSeconds,
		&run.ExecutionEngine,
		&run.ExecutionEngineNotes,
		&run.Source,
		&run.OptRunID,
		&run.OptOrganismID,
		&run.ArtifactPath,
		&run.CreatedAt,
		&run.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if startedAt.Valid {
		start := startedAt.Time
		run.StartedAt = &start
	}
	if completedAt.Valid {
		end := completedAt.Time
		run.CompletedAt = &end
	}
	run.FailureReasonCounts = make(map[string]int)
	_ = json.Unmarshal([]byte(failureCountsJSON), &run.FailureReasonCounts)
	run.AllAttemptFailureCounts = make(map[string]int)
	_ = json.Unmarshal([]byte(attemptFailureCountsJSON), &run.AllAttemptFailureCounts)
	return &run, nil
}

func marshalIntMap(input map[string]int) (string, error) {
	if input == nil {
		input = map[string]int{}
	}
	payload, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

// AggregateBenchmarkFailureCounts sums normalized failure counts across runs,
// optionally filtered by benchmark name and/or workflow ID. The scope parameter
// must be "final" or "all_attempts".
func (s *Storage) AggregateBenchmarkFailureCounts(benchmark, workflowID, scope string) (map[string]int, error) {
	conditions := []string{"fc.scope = ?"}
	args := []interface{}{scope}

	if benchmark = strings.TrimSpace(benchmark); benchmark != "" {
		conditions = append(conditions, "r.benchmark = ?")
		args = append(args, benchmark)
	}
	if workflowID = strings.TrimSpace(workflowID); workflowID != "" {
		conditions = append(conditions, "r.workflow_id = ?")
		args = append(args, workflowID)
	}

	query := `
		SELECT fc.failure_reason, SUM(fc.count) AS total
		FROM benchmark_run_failure_counts fc
		JOIN benchmark_runs r ON r.id = fc.run_id
		WHERE ` + strings.Join(conditions, " AND ") + `
		GROUP BY fc.failure_reason
		ORDER BY total DESC
	`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("aggregate benchmark failure counts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]int)
	for rows.Next() {
		var reason string
		var total int
		if err := rows.Scan(&reason, &total); err != nil {
			return nil, fmt.Errorf("scan benchmark failure count: %w", err)
		}
		out[reason] = total
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate benchmark failure counts: %w", err)
	}
	return out, nil
}

// UpdateBenchmarkRunCosts persists inclusive cost/token totals for a benchmark run.
// This is called after read-path enrichment so data survives job deletion.
func (s *Storage) UpdateBenchmarkRunCosts(runID string, totalTokens int, totalCostUSD float64) error {
	_, err := s.db.Exec(`
		UPDATE benchmark_runs
		SET total_tokens = ?,
		    total_cost_usd = ?,
		    avg_tokens_per_item = CASE WHEN total_items > 0 THEN CAST(? AS REAL) / total_items ELSE 0 END,
		    avg_cost_usd_per_item = CASE WHEN total_items > 0 THEN ? / total_items ELSE 0 END,
		    updated_at = ?
		WHERE id = ?`,
		totalTokens, totalCostUSD, totalTokens, totalCostUSD, sqlTime(time.Now()), runID)
	if err != nil {
		return fmt.Errorf("update benchmark run costs %s: %w", runID, err)
	}
	return nil
}

// FailStaleBenchmarkRuns marks all benchmark_runs with status 'running' as 'failed'.
// Called on server startup to recover from interrupted benchmark runs.
func (s *Storage) FailStaleBenchmarkRuns() (int, error) {
	now := sqlTime(time.Now())
	result, err := s.db.Exec(`
		UPDATE benchmark_runs
		SET status = 'failed', completed_at = ?
		WHERE status = 'running'`, now)
	if err != nil {
		return 0, fmt.Errorf("fail stale benchmark runs: %w", err)
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// EnsureBenchmarkRunExists creates a benchmark_runs row if one doesn't already exist.
func (s *Storage) EnsureBenchmarkRunExists(
	runID, benchmark, split, workflowID, workflowName, datasetPath string,
	totalItems, itemLimit int,
	source, optRunID, optOrganismID string,
) error {
	if itemLimit < 0 {
		itemLimit = 0
	}
	source = normalizeBenchmarkRunSource(source)
	optRunID = strings.TrimSpace(optRunID)
	optOrganismID = strings.TrimSpace(optOrganismID)
	_, err := s.db.Exec(`
		INSERT OR IGNORE INTO benchmark_runs
		(id, benchmark, split, item_limit, workflow_id, workflow_name, dataset_path, status, source, opt_run_id, opt_organism_id, total_items, started_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'running', ?, ?, ?, ?, ?)`,
		runID, benchmark, split, itemLimit, workflowID, workflowName, datasetPath, source, nullIfEmpty(optRunID), nullIfEmpty(optOrganismID), totalItems, sqlTime(time.Now()))
	if err != nil {
		return fmt.Errorf("ensure benchmark run exists %s: %w", runID, err)
	}
	return nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
