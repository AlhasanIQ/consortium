package benchloop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MatrixProposal is the immutable run matrix payload.
type MatrixProposal struct {
	Benchmark       string   `json:"benchmark"`
	RunSet          string   `json:"run_set"`
	Split           string   `json:"split"`
	ItemLimit       int      `json:"item_limit"`
	Concurrency     int      `json:"concurrency"`
	WorkflowOrder   []string `json:"workflow_order"`
	TargetWorkflows []string `json:"target_workflows"`
	Rationale       string   `json:"rationale"`
}

// MatrixLock is the approved immutable run matrix.
type MatrixLock struct {
	MatrixProposal
	ApprovedAt time.Time `json:"approved_at"`
	SessionID  string    `json:"session_id"`
}

// MatrixLockPath returns the path to matrix.lock.json.
func MatrixLockPath(workdir string) string {
	return filepath.Join(workdir, "benchmarks", "loop", "matrix.lock.json")
}

// Validate checks that the proposal has all required fields.
func (p *MatrixProposal) Validate() error {
	if p.Benchmark == "" {
		return fmt.Errorf("benchmark is required")
	}
	if p.RunSet == "" {
		return fmt.Errorf("run_set is required")
	}
	if p.Split == "" {
		return fmt.Errorf("split is required")
	}
	if p.ItemLimit < 0 {
		return fmt.Errorf("item_limit must be >= 0 (0 = all items)")
	}
	if p.Concurrency < 1 {
		return fmt.Errorf("concurrency must be >= 1")
	}
	if len(p.WorkflowOrder) == 0 {
		return fmt.Errorf("workflow_order is required")
	}
	if len(p.TargetWorkflows) == 0 {
		return fmt.Errorf("target_workflows is required")
	}

	targetSet := make(map[string]struct{}, len(p.TargetWorkflows))
	for _, wf := range p.TargetWorkflows {
		if strings.TrimSpace(wf) == "" {
			return fmt.Errorf("target_workflows cannot contain empty ids")
		}
		targetSet[wf] = struct{}{}
	}
	for _, wf := range p.WorkflowOrder {
		if _, ok := targetSet[wf]; !ok {
			return fmt.Errorf("workflow_order entry %q is not present in target_workflows", wf)
		}
	}
	return nil
}

// ReadMatrixLock reads the approved matrix lock.
func ReadMatrixLock(workdir string) (*MatrixLock, error) {
	data, err := os.ReadFile(MatrixLockPath(workdir))
	if err != nil {
		return nil, fmt.Errorf("read matrix lock: %w", err)
	}
	var lock MatrixLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("parse matrix lock: %w", err)
	}
	lock.Normalize()
	return &lock, nil
}

// ApproveMatrix writes the approved matrix lock from a proposal.
func ApproveMatrix(workdir string, proposal *MatrixProposal, sessionID string) error {
	lock := &MatrixLock{
		MatrixProposal: *proposal,
		ApprovedAt:     time.Now(),
		SessionID:      sessionID,
	}
	lock.Normalize()
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal matrix lock: %w", err)
	}
	path := MatrixLockPath(workdir)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write matrix lock: %w", err)
	}
	return nil
}

// IsInMatrix checks whether a run's benchmark/workflow/split matches the locked matrix.
func (lock *MatrixLock) IsInMatrix(benchmark, workflowID, split string) bool {
	if lock.Benchmark != benchmark || lock.Split != split {
		return false
	}
	for _, wf := range lock.WorkflowOrder {
		if wf == workflowID {
			return true
		}
	}
	return false
}

// UpdateMatrix writes a new matrix lock (for approved matrix expansion).
func UpdateMatrix(workdir string, lock *MatrixLock) error {
	lock.Normalize()
	lock.ApprovedAt = time.Now()
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal updated matrix lock: %w", err)
	}
	path := MatrixLockPath(workdir)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write updated matrix lock: %w", err)
	}
	return nil
}

// Normalize deduplicates matrix fields and guarantees that every workflow_order
// entry is also present in target_workflows.
func (lock *MatrixLock) Normalize() {
	lock.WorkflowOrder = dedupeNonEmpty(lock.WorkflowOrder)
	lock.TargetWorkflows = dedupeNonEmpty(lock.TargetWorkflows)

	seen := make(map[string]struct{}, len(lock.TargetWorkflows))
	for _, wf := range lock.TargetWorkflows {
		seen[wf] = struct{}{}
	}
	for _, wf := range lock.WorkflowOrder {
		if _, ok := seen[wf]; ok {
			continue
		}
		lock.TargetWorkflows = append(lock.TargetWorkflows, wf)
		seen[wf] = struct{}{}
	}
}

// ValidateRunSetSplit ensures run_set and split are compatible so the loop does
// not silently execute on a different split than intended.
func (lock *MatrixLock) ValidateRunSetSplit() error {
	if lock == nil {
		return fmt.Errorf("matrix lock is nil")
	}
	runSet := strings.ToLower(strings.TrimSpace(lock.RunSet))
	split := strings.ToLower(strings.TrimSpace(lock.Split))
	switch runSet {
	case "custom":
		if split == "" {
			return fmt.Errorf("matrix lock invalid: run_set=custom requires split")
		}
	case "lite":
		if split != "" && split != "dev" && split != "validation" {
			return fmt.Errorf("matrix lock invalid: run_set=lite cannot use split=%q; use run_set=custom for test split", lock.Split)
		}
	default:
		return fmt.Errorf("matrix lock invalid: unsupported run_set %q", lock.RunSet)
	}
	return nil
}

// MigrateLegacyRunSetSplit rewrites legacy run_set=lite + non-dev/validation split
// locks to run_set=custom, preserving the intended split.
// Returns true when a migration was applied.
func (lock *MatrixLock) MigrateLegacyRunSetSplit() bool {
	if lock == nil {
		return false
	}
	runSet := strings.ToLower(strings.TrimSpace(lock.RunSet))
	split := strings.ToLower(strings.TrimSpace(lock.Split))
	if runSet == "lite" && split != "" && split != "dev" && split != "validation" {
		lock.RunSet = "custom"
		return true
	}
	return false
}

func dedupeNonEmpty(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
