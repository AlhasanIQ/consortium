package benchloop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// State tracks the loop's persistent state across iterations.
type State struct {
	SessionID       string   `json:"session_id"`
	TargetWorkflows []string `json:"target_workflows"`
	Benchmark       string   `json:"benchmark"`
	Split           string   `json:"split"`
	ItemLimit       int      `json:"item_limit"`
	Concurrency     int      `json:"concurrency"`

	// Baseline (current best).
	BaselineAccuracy     float64 `json:"baseline_accuracy"`
	BaselineParseRate    float64 `json:"baseline_parse_rate"`
	BaselineCostPerItem  float64 `json:"baseline_cost_per_item"`
	BaselineFailedItems  int     `json:"baseline_failed_items"`
	BaselineRunID        string  `json:"baseline_run_id"`
	BaselineTotalItems   int     `json:"baseline_total_items"`
	BaselineAvgLatencyMS float64 `json:"baseline_avg_latency_ms"`
	BaselineP95LatencyMS float64 `json:"baseline_p95_latency_ms"`

	// Current best (updated on accept).
	CurrentAccuracy     float64 `json:"current_accuracy"`
	CurrentParseRate    float64 `json:"current_parse_rate"`
	CurrentCostPerItem  float64 `json:"current_cost_per_item"`
	CurrentFailedItems  int     `json:"current_failed_items"`
	CurrentRunID        string  `json:"current_run_id"`
	CurrentAvgLatencyMS float64 `json:"current_avg_latency_ms"`
	CurrentP95LatencyMS float64 `json:"current_p95_latency_ms"`

	// Loop progress.
	Iteration       int `json:"iteration"`
	MaxIterations   int `json:"max_iterations"`
	PlateauCount    int `json:"plateau_count"`
	AgentCrashCount int `json:"agent_crash_count"`
	TotalAttempts   int `json:"total_attempts"` // monotonic counter for log file naming

	// Cost tracking.
	TotalAgentCostUSD  float64  `json:"total_agent_cost_usd"`
	TotalBenchCostUSD  float64  `json:"total_bench_cost_usd"`
	CountedBenchRunIDs []string `json:"counted_bench_run_ids,omitempty"`

	// Tooling feedback accumulated across iterations.
	UXRecommendations []string `json:"ux_recommendations,omitempty"`

	// Lifecycle.
	Status          string     `json:"status"` // "setup", "running", "completed", "failed"
	StartedAt       time.Time  `json:"started_at"`
	LastIterationAt *time.Time `json:"last_iteration_at,omitempty"`

	// Live execution state for in-flight visibility.
	Live *LiveStatus `json:"live,omitempty"`

	// Debug pointers to the most recent Claude session/transcript.
	LastClaudeSessionID string `json:"last_claude_session_id,omitempty"`
	LastTranscriptPath  string `json:"last_transcript_path,omitempty"`
}

// LiveStatus captures real-time loop progress while a run is active.
type LiveStatus struct {
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

// HasCountedBenchRun reports whether the run cost was already accounted for.
func (s *State) HasCountedBenchRun(runID string) bool {
	if runID == "" {
		return false
	}
	for _, id := range s.CountedBenchRunIDs {
		if id == runID {
			return true
		}
	}
	return false
}

// MarkBenchRunCounted marks a benchmark run as cost-accounted.
func (s *State) MarkBenchRunCounted(runID string) {
	if runID == "" || s.HasCountedBenchRun(runID) {
		return
	}
	s.CountedBenchRunIDs = append(s.CountedBenchRunIDs, runID)
}

// NewState creates initial state from config.
func NewState(cfg *Config) *State {
	return &State{
		SessionID:       time.Now().Format("20060102_150405"),
		TargetWorkflows: cfg.Workflows,
		Benchmark:       cfg.Benchmark,
		Split:           cfg.InferSplit(),
		ItemLimit:       cfg.ItemLimit,
		Concurrency:     cfg.Concurrency,
		MaxIterations:   cfg.MaxIterations,
		Status:          "setup",
		StartedAt:       time.Now(),
	}
}

// StatePath returns the path to state.json within the loop directory.
func StatePath(workdir string) string {
	return filepath.Join(workdir, "benchmarks", "loop", "state.json")
}

// LoadState reads state from disk.
func LoadState(workdir string) (*State, error) {
	path := StatePath(workdir)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	return &s, nil
}

// Save writes state to disk atomically (write tmp, rename).
func (s *State) Save(workdir string) error {
	path := StatePath(workdir)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write state tmp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename state: %w", err)
	}
	return nil
}
