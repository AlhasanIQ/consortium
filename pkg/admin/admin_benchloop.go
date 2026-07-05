package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/internal/benchloop"
	"github.com/gorilla/mux"
)

// ---------------------------------------------------------------------------
// Response types (local — mirrors internal/benchloop JSON shapes)
// ---------------------------------------------------------------------------

type benchloopStatusResponse struct {
	Active      bool               `json:"active"`
	State       *benchloopState    `json:"state"`
	Matrix      *benchloopMatrix   `json:"matrix"`
	Lock        *benchloopLockInfo `json:"lock"`
	Sessions    []benchloopSession `json:"sessions"`
	Decision    *benchloopDecision `json:"decision"`
	ArchivedIDs []string           `json:"archived_ids"`
	ElapsedMs   int64              `json:"elapsed_ms"`
}

type benchloopState struct {
	SessionID       string   `json:"session_id"`
	TargetWorkflows []string `json:"target_workflows"`
	Benchmark       string   `json:"benchmark"`
	Split           string   `json:"split"`
	ItemLimit       int      `json:"item_limit"`
	Concurrency     int      `json:"concurrency"`

	BaselineAccuracy     float64 `json:"baseline_accuracy"`
	BaselineParseRate    float64 `json:"baseline_parse_rate"`
	BaselineCostPerItem  float64 `json:"baseline_cost_per_item"`
	BaselineFailedItems  int     `json:"baseline_failed_items"`
	BaselineRunID        string  `json:"baseline_run_id"`
	BaselineTotalItems   int     `json:"baseline_total_items"`
	BaselineAvgLatencyMS float64 `json:"baseline_avg_latency_ms"`
	BaselineP95LatencyMS float64 `json:"baseline_p95_latency_ms"`

	CurrentAccuracy     float64 `json:"current_accuracy"`
	CurrentParseRate    float64 `json:"current_parse_rate"`
	CurrentCostPerItem  float64 `json:"current_cost_per_item"`
	CurrentFailedItems  int     `json:"current_failed_items"`
	CurrentRunID        string  `json:"current_run_id"`
	CurrentAvgLatencyMS float64 `json:"current_avg_latency_ms"`
	CurrentP95LatencyMS float64 `json:"current_p95_latency_ms"`

	Iteration       int `json:"iteration"`
	MaxIterations   int `json:"max_iterations"`
	PlateauCount    int `json:"plateau_count"`
	AgentCrashCount int `json:"agent_crash_count"`
	TotalAttempts   int `json:"total_attempts"`

	TotalAgentCostUSD float64 `json:"total_agent_cost_usd"`
	TotalBenchCostUSD float64 `json:"total_bench_cost_usd"`

	Status          string     `json:"status"`
	StartedAt       time.Time  `json:"started_at"`
	LastIterationAt *time.Time `json:"last_iteration_at,omitempty"`

	Live *benchloopLiveStatus `json:"live,omitempty"`

	LastClaudeSessionID string `json:"last_claude_session_id,omitempty"`
	LastTranscriptPath  string `json:"last_transcript_path,omitempty"`
}

type benchloopLiveStatus struct {
	Phase          string     `json:"phase,omitempty"`
	Step           string     `json:"step,omitempty"`
	Message        string     `json:"message,omitempty"`
	Iteration      int        `json:"iteration,omitempty"`
	Attempt        int        `json:"attempt,omitempty"`
	AgentPID       int        `json:"agent_pid,omitempty"`
	AgentSessionID string     `json:"agent_session_id,omitempty"`
	AgentLogPath   string     `json:"agent_log_path,omitempty"`
	TranscriptPath string     `json:"transcript_path,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	UpdatedAt      *time.Time `json:"updated_at,omitempty"`
}

type benchloopMatrix struct {
	Benchmark       string   `json:"benchmark"`
	RunSet          string   `json:"run_set"`
	Split           string   `json:"split"`
	ItemLimit       int      `json:"item_limit"`
	Concurrency     int      `json:"concurrency"`
	WorkflowOrder   []string `json:"workflow_order"`
	TargetWorkflows []string `json:"target_workflows"`
	Rationale       string   `json:"rationale"`
	ApprovedAt      string   `json:"approved_at"`
	SessionID       string   `json:"session_id"`
}

type benchloopLockInfo struct {
	PID       int    `json:"pid"`
	CreatedAt string `json:"created_at"`
	SessionID string `json:"session_id"`
	Alive     bool   `json:"alive"`
}

type benchloopSession struct {
	LoopSessionID   string `json:"loop_session_id"`
	Phase           string `json:"phase"`
	Iteration       int    `json:"iteration"`
	Attempt         int    `json:"attempt"`
	ClaudeSessionID string `json:"claude_session_id"`
	TranscriptPath  string `json:"transcript_path"`
	LogPath         string `json:"log_path"`
	ExitCode        int    `json:"exit_code"`
	StartedAt       string `json:"started_at"`
	FinishedAt      string `json:"finished_at"`
	InFlight        bool   `json:"in_flight,omitempty"`
	DurationMs      int64  `json:"duration_ms,omitempty"`
}

type benchloopDecision struct {
	Verdict           string   `json:"verdict"`
	RunID             string   `json:"run_id"`
	Accuracy          float64  `json:"accuracy"`
	ParseRate         float64  `json:"parse_rate"`
	FailedItems       int      `json:"failed_items"`
	TotalItems        int      `json:"total_items"`
	CostPerItem       float64  `json:"cost_per_item"`
	AvgLatencyMS      float64  `json:"avg_latency_ms,omitempty"`
	P95LatencyMS      float64  `json:"p95_latency_ms,omitempty"`
	WorkflowsModified []string `json:"workflows_modified"`
	ChangesSummary    string   `json:"changes_summary"`
	Reasoning         string   `json:"reasoning"`
	SanityPassed      bool     `json:"sanity_passed"`
	FailureAnalysis   string   `json:"failure_analysis,omitempty"`
}

type benchloopTranscriptResponse struct {
	ClaudeSessionID string                        `json:"claude_session_id"`
	Messages        []benchloop.TranscriptMessage `json:"messages"`
	Error           string                        `json:"error,omitempty"`
}

type benchloopMemoryResponse struct {
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

type benchloopLogResponse struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Lines   int    `json:"lines"`
}

// ---------------------------------------------------------------------------
// Path helpers
// ---------------------------------------------------------------------------

func (s *Server) benchloopDir() string {
	return filepath.Join(s.workdir, "benchmarks", "loop")
}

// ---------------------------------------------------------------------------
// File readers
// ---------------------------------------------------------------------------

func readJSONFile[T any](path string) (*T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	return &v, nil
}

func readSessionIndex(path string) ([]benchloopSession, error) {
	raw, err := benchloop.ReadSessionRecords(path)
	if err != nil {
		return nil, err
	}
	records := make([]benchloopSession, 0, len(raw))
	for _, r := range raw {
		var durationMs int64
		if !r.FinishedAt.IsZero() && !r.StartedAt.IsZero() {
			durationMs = r.FinishedAt.Sub(r.StartedAt).Milliseconds()
		}
		records = append(records, benchloopSession{
			LoopSessionID:   r.LoopSessionID,
			Phase:           r.Phase,
			Iteration:       r.Iteration,
			Attempt:         r.Attempt,
			ClaudeSessionID: r.ClaudeSessionID,
			TranscriptPath:  r.TranscriptPath,
			LogPath:         r.LogPath,
			ExitCode:        r.ExitCode,
			StartedAt:       formatTimeIfSet(r.StartedAt),
			FinishedAt:      formatTimeIfSet(r.FinishedAt),
			InFlight:        r.InFlight,
			DurationMs:      durationMs,
		})
	}
	return records, nil
}

func formatTimeIfSet(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func readLockInfo(path string) (*benchloopLockInfo, error) {
	type rawLock struct {
		PID       int       `json:"pid"`
		CreatedAt time.Time `json:"created_at"`
		SessionID string    `json:"session_id"`
	}
	raw, err := readJSONFile[rawLock](path)
	if err != nil || raw == nil {
		return nil, err
	}
	return &benchloopLockInfo{
		PID:       raw.PID,
		CreatedAt: raw.CreatedAt.Format(time.RFC3339),
		SessionID: raw.SessionID,
		Alive:     processAlive(raw.PID),
	}, nil
}

func listArchivedIDs(dir string) []string {
	archiveDir := filepath.Join(dir, "archive")
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		return nil
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	return ids
}

// mergeLiveSession synthesizes an in-flight session record from state.live.
func mergeLiveSession(state *benchloopState, sessions []benchloopSession, lockInfo *benchloopLockInfo) []benchloopSession {
	if state == nil || state.Live == nil || strings.TrimSpace(state.Live.AgentSessionID) == "" {
		return sessions
	}
	if state.Status != "running" {
		return sessions
	}
	if lockInfo != nil && !lockInfo.Alive {
		return sessions
	}

	liveSessionID := strings.TrimSpace(state.Live.AgentSessionID)
	for _, s := range sessions {
		if s.ClaudeSessionID == liveSessionID {
			return sessions
		}
		if s.LoopSessionID == state.SessionID && s.Attempt == state.Live.Attempt && s.Attempt > 0 {
			return sessions
		}
	}

	startedAt := ""
	if state.Live.StartedAt != nil {
		startedAt = state.Live.StartedAt.Format(time.RFC3339)
	}
	live := benchloopSession{
		LoopSessionID:   state.SessionID,
		Phase:           state.Live.Phase,
		Iteration:       state.Live.Iteration,
		Attempt:         state.Live.Attempt,
		ClaudeSessionID: liveSessionID,
		TranscriptPath:  state.Live.TranscriptPath,
		LogPath:         state.Live.AgentLogPath,
		ExitCode:        -1,
		StartedAt:       startedAt,
		InFlight:        true,
	}
	if live.Phase == "" {
		live.Phase = "iteration"
	}
	return append(sessions, live)
}

// ---------------------------------------------------------------------------
// Log reader
// ---------------------------------------------------------------------------

func resolveLogPath(loopDir, loopSessionID string, attempt int, logPath string) string {
	attemptFile := strconv.Itoa(attempt) + ".log"
	candidates := make([]string, 0, 5)

	if loopSessionID != "" {
		candidates = append(candidates,
			filepath.Join(loopDir, "sessions", loopSessionID, "iterations", attemptFile),
			filepath.Join(loopDir, "archive", loopSessionID, "sessions", "iterations", attemptFile),
			filepath.Join(loopDir, "archive", loopSessionID, "iterations", attemptFile),
		)
	}
	candidates = append(candidates, filepath.Join(loopDir, "iterations", attemptFile))
	if logPath != "" {
		candidates = append(candidates, logPath)
	}

	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if logPath != "" {
		return logPath
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return ""
}

func readLogTail(path string, tail int) (string, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	lines := strings.Split(string(data), "\n")
	// Remove trailing empty line from final newline.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	total := len(lines)
	if tail > 0 && len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	return strings.Join(lines, "\n"), total, nil
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func (s *Server) buildBenchloopStatus(dir string) benchloopStatusResponse {
	state, _ := readJSONFile[benchloopState](filepath.Join(dir, "state.json"))
	matrix, _ := readJSONFile[benchloopMatrix](filepath.Join(dir, "matrix.lock.json"))
	lockInfo, _ := readLockInfo(filepath.Join(dir, ".lock"))
	decision, _ := readJSONFile[benchloopDecision](filepath.Join(dir, "decision.json"))
	sessions, _ := readSessionIndex(filepath.Join(dir, "session_index.jsonl"))

	if sessions == nil {
		sessions = []benchloopSession{}
	}

	sessions = mergeLiveSession(state, sessions, lockInfo)

	active := false
	var elapsedMs int64
	if state != nil && state.Status == "running" {
		if lockInfo != nil && lockInfo.Alive {
			active = true
		}
		if !state.StartedAt.IsZero() {
			elapsedMs = time.Since(state.StartedAt).Milliseconds()
		}
	}

	archived := listArchivedIDs(dir)
	if archived == nil {
		archived = []string{}
	}

	return benchloopStatusResponse{
		Active:      active,
		State:       state,
		Matrix:      matrix,
		Lock:        lockInfo,
		Sessions:    sessions,
		Decision:    decision,
		ArchivedIDs: archived,
		ElapsedMs:   elapsedMs,
	}
}

func (s *Server) handleBenchloopStatus(w http.ResponseWriter, _ *http.Request) {
	resp := s.buildBenchloopStatus(s.benchloopDir())
	writeJSONResponse(w, resp)
}

func (s *Server) handleBenchloopArchive(w http.ResponseWriter, r *http.Request) {
	sessionID := mux.Vars(r)["sessionID"]
	if sessionID == "" {
		writeJSONError(w, "sessionID required", http.StatusBadRequest)
		return
	}
	archiveDir := filepath.Join(s.benchloopDir(), "archive", sessionID)
	if _, err := os.Stat(archiveDir); os.IsNotExist(err) {
		writeJSONError(w, "archive not found", http.StatusNotFound)
		return
	}
	resp := s.buildBenchloopStatus(archiveDir)
	writeJSONResponse(w, resp)
}

func (s *Server) handleBenchloopTranscript(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeJSONError(w, "session_id query parameter required", http.StatusBadRequest)
		return
	}

	includeUser := r.URL.Query().Get("include_user") == "true"
	includeTools := r.URL.Query().Get("include_tools") == "true"
	maxMessages := 50
	if v := r.URL.Query().Get("max_messages"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxMessages = n
		}
	}

	path, err := benchloop.ClaudeTranscriptPath(s.workdir, sessionID)
	if err != nil {
		writeJSONResponse(w, benchloopTranscriptResponse{
			ClaudeSessionID: sessionID,
			Error:           fmt.Sprintf("resolve path: %v", err),
		})
		return
	}

	msgs, err := benchloop.ReadTranscriptMessages(path, benchloop.TranscriptReadOptions{
		IncludeUser:  includeUser,
		IncludeTools: includeTools,
		MaxMessages:  maxMessages,
	})
	if err != nil {
		writeJSONResponse(w, benchloopTranscriptResponse{
			ClaudeSessionID: sessionID,
			Messages:        []benchloop.TranscriptMessage{},
			Error:           fmt.Sprintf("read transcript: %v", err),
		})
		return
	}
	if msgs == nil {
		msgs = []benchloop.TranscriptMessage{}
	}

	writeJSONResponse(w, benchloopTranscriptResponse{
		ClaudeSessionID: sessionID,
		Messages:        msgs,
	})
}

func (s *Server) handleBenchloopLog(w http.ResponseWriter, r *http.Request) {
	loopSessionID := r.URL.Query().Get("session_id")
	attemptStr := r.URL.Query().Get("attempt")
	if loopSessionID == "" || attemptStr == "" {
		writeJSONError(w, "session_id and attempt query parameters required", http.StatusBadRequest)
		return
	}

	attempt, err := strconv.Atoi(attemptStr)
	if err != nil {
		writeJSONError(w, "invalid attempt number", http.StatusBadRequest)
		return
	}

	tail := 200
	if v := r.URL.Query().Get("tail"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			tail = n
		}
	}

	logPath := r.URL.Query().Get("log_path")
	path := resolveLogPath(s.benchloopDir(), loopSessionID, attempt, logPath)
	if path == "" {
		writeJSONError(w, "log path could not be resolved", http.StatusNotFound)
		return
	}

	content, lines, err := readLogTail(path, tail)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSONError(w, "log file not found", http.StatusNotFound)
			return
		}
		writeJSONError(w, fmt.Sprintf("read log: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSONResponse(w, benchloopLogResponse{
		Path:    path,
		Content: content,
		Lines:   lines,
	})
}

func (s *Server) handleBenchloopMemory(w http.ResponseWriter, r *http.Request) {
	dir := s.benchloopDir()
	if sessionID := r.URL.Query().Get("session_id"); sessionID != "" {
		dir = filepath.Join(s.benchloopDir(), "archive", sessionID)
	}

	path := filepath.Join(dir, "loop_memory.md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSONResponse(w, benchloopMemoryResponse{Content: "", Truncated: false})
			return
		}
		writeJSONError(w, fmt.Sprintf("read memory: %v", err), http.StatusInternalServerError)
		return
	}

	maxLines := 500
	if v := r.URL.Query().Get("max_lines"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxLines = n
		}
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	truncated := false
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		truncated = true
		content = strings.Join(lines, "\n")
	}

	writeJSONResponse(w, benchloopMemoryResponse{
		Content:   content,
		Truncated: truncated,
	})
}
