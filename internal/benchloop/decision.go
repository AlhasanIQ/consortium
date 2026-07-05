package benchloop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Verdict represents the agent's decision outcome.
type Verdict string

const (
	VerdictContinue            Verdict = "continue"
	VerdictStopImproved        Verdict = "stop_improved"
	VerdictStopNoProgress      Verdict = "stop_no_progress"
	VerdictRollback            Verdict = "rollback"
	VerdictRequestMatrixChange Verdict = "request_matrix_change"
)

// Decision is the agent's structured output written to decision.json.
type Decision struct {
	Verdict           Verdict  `json:"verdict"`
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

	// Optional fields.
	FailureAnalysis     string               `json:"failure_analysis,omitempty"`
	SanityRunID         string               `json:"sanity_run_id,omitempty"`
	RecommendFullRun    bool                 `json:"recommendation_for_full_run,omitempty"`
	MatrixChangeRequest *MatrixChangeRequest `json:"matrix_change_request,omitempty"`
	UXRecommendations   string               `json:"ux_recommendations,omitempty"`
	TargetedReplayRunID string               `json:"targeted_replay_run_id,omitempty"`
	TargetedReplayItem  string               `json:"targeted_replay_item,omitempty"`
	TargetedReplayPass  bool                 `json:"targeted_replay_pass,omitempty"`
}

// MatrixChangeRequest describes a requested scope expansion.
type MatrixChangeRequest struct {
	Description string   `json:"description"`
	Workflows   []string `json:"workflows,omitempty"`
	Benchmark   string   `json:"benchmark,omitempty"`
	RunSet      string   `json:"run_set,omitempty"`
	Split       string   `json:"split,omitempty"`
}

// DecisionPath returns the path to decision.json within the loop directory.
func DecisionPath(workdir string) string {
	return filepath.Join(workdir, "benchmarks", "loop", "decision.json")
}

// ReadDecision reads and validates the agent's decision file.
func ReadDecision(workdir string) (*Decision, error) {
	path := DecisionPath(workdir)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read decision: %w", err)
	}

	var d Decision
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("parse decision: %w", err)
	}

	if err := d.Validate(); err != nil {
		return nil, fmt.Errorf("invalid decision: %w", err)
	}

	return &d, nil
}

// RemoveDecision deletes any stale decision.json from a previous iteration.
func RemoveDecision(workdir string) error {
	path := DecisionPath(workdir)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale decision: %w", err)
	}
	return nil
}

// Validate checks that all required fields are present and valid.
func (d *Decision) Validate() error {
	switch d.Verdict {
	case VerdictContinue, VerdictStopImproved, VerdictStopNoProgress, VerdictRollback, VerdictRequestMatrixChange:
		// valid
	default:
		return fmt.Errorf("unknown verdict %q", d.Verdict)
	}

	if d.Verdict == VerdictRequestMatrixChange && d.MatrixChangeRequest == nil {
		return fmt.Errorf("verdict is %q but matrix_change_request is missing", d.Verdict)
	}

	if d.RunID == "" {
		return fmt.Errorf("run_id is required")
	}
	if d.ChangesSummary == "" {
		return fmt.Errorf("changes_summary is required")
	}
	if d.Reasoning == "" {
		return fmt.Errorf("reasoning is required")
	}

	return nil
}
