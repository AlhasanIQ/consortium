package benchloop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- Config tests ---

func TestConfigValidate_RequiredFields(t *testing.T) {
	cfg := &Config{}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for empty config")
	}
}

func TestConfigValidate_InvalidRunSet(t *testing.T) {
	cfg := &Config{
		Workflows:        []string{"wf1"},
		Benchmark:        "test",
		RunSet:           "invalid",
		ItemLimit:        10,
		Concurrency:      5,
		MaxIterations:    3,
		StopAfterPlateau: 2,
		IterationTimeout: 5 * time.Minute,
		TotalBudgetUSD:   10,
		AgentBudgetUSD:   3,
		ClaudeBin:        "echo", // use echo as a stand-in
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for invalid run-set")
	}
	if got := err.Error(); got != "--run-set must be 'lite' or 'custom', got \"invalid\"" {
		t.Fatalf("unexpected error: %s", got)
	}
}

func TestConfigValidate_CustomRunSetRequiresSplit(t *testing.T) {
	cfg := &Config{
		Workflows:        []string{"wf1"},
		Benchmark:        "test",
		RunSet:           "custom",
		Split:            "", // missing
		ItemLimit:        10,
		Concurrency:      5,
		MaxIterations:    3,
		StopAfterPlateau: 2,
		IterationTimeout: 5 * time.Minute,
		TotalBudgetUSD:   10,
		AgentBudgetUSD:   3,
		ClaudeBin:        "echo",
	}
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected error when run-set custom omits split")
	}
}

func TestConfigValidate_ExplicitEmptySplitIsRejected(t *testing.T) {
	cfg := &Config{
		Workflows:        []string{"wf1"},
		Benchmark:        "test",
		RunSet:           "custom",
		Split:            "",
		ItemLimit:        10,
		Concurrency:      5,
		MaxIterations:    3,
		StopAfterPlateau: 2,
		IterationTimeout: 5 * time.Minute,
		TotalBudgetUSD:   10,
		AgentBudgetUSD:   3,
		ClaudeBin:        "echo",
		ExplicitFlags: map[string]bool{
			"split": true,
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error when split was explicitly provided as empty")
	}
}

func TestConfigValidate_LiteRunSetExplicitTestSplitRejected(t *testing.T) {
	cfg := &Config{
		Workflows:        []string{"wf1"},
		Benchmark:        "test",
		RunSet:           "lite",
		Split:            "test",
		ItemLimit:        10,
		Concurrency:      5,
		MaxIterations:    3,
		StopAfterPlateau: 2,
		IterationTimeout: 5 * time.Minute,
		TotalBudgetUSD:   10,
		AgentBudgetUSD:   3,
		ClaudeBin:        "echo",
		ExplicitFlags: map[string]bool{
			"split": true,
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for explicit test split with run-set lite")
	}
}

func TestConfigValidate_LiteRunSetExplicitDevSplitAllowed(t *testing.T) {
	cfg := &Config{
		Workflows:        []string{"wf1"},
		Benchmark:        "test",
		RunSet:           "lite",
		Split:            "dev",
		ItemLimit:        10,
		Concurrency:      5,
		MaxIterations:    3,
		StopAfterPlateau: 2,
		IterationTimeout: 5 * time.Minute,
		TotalBudgetUSD:   10,
		AgentBudgetUSD:   3,
		ClaudeBin:        "echo",
		ExplicitFlags: map[string]bool{
			"split":       true,
			"item-limit":  true,
			"concurrency": true,
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected dev split with run-set lite to be valid, got: %v", err)
	}
}

func TestConfigValidate_AgentBudgetExceedsTotal(t *testing.T) {
	cfg := &Config{
		Workflows:        []string{"wf1"},
		Benchmark:        "test",
		RunSet:           "lite",
		ItemLimit:        10,
		Concurrency:      5,
		MaxIterations:    3,
		StopAfterPlateau: 2,
		IterationTimeout: 5 * time.Minute,
		TotalBudgetUSD:   5,
		AgentBudgetUSD:   10, // exceeds total
		ClaudeBin:        "echo",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error when agent budget exceeds total")
	}
}

func TestConfigValidate_NewRunsRequireMatrixFlags(t *testing.T) {
	cfg := &Config{
		Workflows:        []string{"wf1"},
		Benchmark:        "test",
		RunSet:           "lite",
		MaxIterations:    3,
		StopAfterPlateau: 2,
		IterationTimeout: 5 * time.Minute,
		ClaudeBin:        "echo",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error when new run omits split/item-limit/concurrency")
	}
	if !contains(err.Error(), "new runs require --split, --item-limit, and --concurrency") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConfigValidate_ResumeAllowsOmittedWorkflows(t *testing.T) {
	cfg := &Config{
		Resume:           true,
		Benchmark:        "test",
		RunSet:           "lite",
		MaxIterations:    3,
		StopAfterPlateau: 2,
		IterationTimeout: 5 * time.Minute,
		ClaudeBin:        "echo",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("resume should allow omitting --workflows: %v", err)
	}
}

func TestConfigValidate_ResumeRejectsExplicitEmptyWorkflows(t *testing.T) {
	cfg := &Config{
		Resume:           true,
		Benchmark:        "test",
		RunSet:           "lite",
		MaxIterations:    3,
		StopAfterPlateau: 2,
		IterationTimeout: 5 * time.Minute,
		ClaudeBin:        "echo",
		ExplicitFlags: map[string]bool{
			"workflows": true,
		},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for explicitly-empty --workflows on resume")
	}
	if !contains(err.Error(), "--workflows cannot be empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoopValidateResumeInputs_RejectsExplicitMismatch(t *testing.T) {
	loop := &Loop{
		cfg: &Config{
			Resume:      true,
			Benchmark:   "global-mmlu-lite",
			RunSet:      "custom",
			Split:       "dev",
			ItemLimit:   50,
			Concurrency: 20,
			ExplicitFlags: map[string]bool{
				"split": true,
			},
		},
	}
	lock := &MatrixLock{
		MatrixProposal: MatrixProposal{
			Benchmark:       "global-mmlu-lite",
			RunSet:          "custom",
			Split:           "test",
			ItemLimit:       50,
			Concurrency:     20,
			WorkflowOrder:   []string{"benchmark-informed-captain-synthesis-cheap"},
			TargetWorkflows: []string{"benchmark-informed-captain-synthesis-cheap", "reasoning-informed-captain-synthesis-cheap"},
		},
	}

	err := loop.validateResumeInputs(lock)
	if err == nil {
		t.Fatal("expected explicit split mismatch to be rejected on resume")
	}
	if !contains(err.Error(), "does not match resumed matrix split") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoopValidateResumeInputs_AllowsMatchingWorkflows(t *testing.T) {
	loop := &Loop{
		cfg: &Config{
			Resume:    true,
			Workflows: []string{"benchmark-informed-captain-synthesis-cheap"},
			ExplicitFlags: map[string]bool{
				"workflows": true,
			},
		},
	}
	lock := &MatrixLock{
		MatrixProposal: MatrixProposal{
			Benchmark:       "global-mmlu-lite",
			RunSet:          "custom",
			Split:           "test",
			ItemLimit:       50,
			Concurrency:     20,
			WorkflowOrder:   []string{"benchmark-informed-captain-synthesis-cheap"},
			TargetWorkflows: []string{"benchmark-informed-captain-synthesis-cheap", "reasoning-informed-captain-synthesis-cheap"},
		},
	}

	if err := loop.validateResumeInputs(lock); err != nil {
		t.Fatalf("matching resume workflows should be accepted: %v", err)
	}
}

func TestInferSplit(t *testing.T) {
	cfg := &Config{RunSet: "lite"}
	if got := cfg.InferSplit(); got != "dev" {
		t.Fatalf("expected dev, got %s", got)
	}

	cfg.Split = "test"
	if got := cfg.InferSplit(); got != "test" {
		t.Fatalf("expected test, got %s", got)
	}
}

// --- State tests ---

func TestStateSaveLoad(t *testing.T) {
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		t.Fatal(err)
	}

	state := &State{
		SessionID:       "test_session",
		TargetWorkflows: []string{"wf1", "wf2"},
		Benchmark:       "test-bench",
		Split:           "dev",
		ItemLimit:       50,
		CurrentAccuracy: 0.85,
		CurrentRunID:    "run123",
		Iteration:       3,
		MaxIterations:   10,
		Status:          "running",
		StartedAt:       time.Now().Truncate(time.Second),
	}

	if err := state.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := LoadState(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.SessionID != state.SessionID {
		t.Errorf("session_id: got %s, want %s", loaded.SessionID, state.SessionID)
	}
	if loaded.CurrentAccuracy != 0.85 {
		t.Errorf("accuracy: got %f, want 0.85", loaded.CurrentAccuracy)
	}
	if loaded.Iteration != 3 {
		t.Errorf("iteration: got %d, want 3", loaded.Iteration)
	}
	if loaded.CurrentRunID != "run123" {
		t.Errorf("run_id: got %s, want run123", loaded.CurrentRunID)
	}
}

func TestStateSaveLoad_BaselineTotalItems(t *testing.T) {
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		t.Fatal(err)
	}

	state := &State{
		SessionID:          "test_session",
		Benchmark:          "test-bench",
		Split:              "dev",
		ItemLimit:          50,
		BaselineAccuracy:   0.94,
		BaselineRunID:      "baseline-run",
		BaselineTotalItems: 50,
		Status:             "running",
		StartedAt:          time.Now().Truncate(time.Second),
	}

	if err := state.Save(dir); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := LoadState(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.BaselineTotalItems != 50 {
		t.Errorf("baseline_total_items: got %d, want 50", loaded.BaselineTotalItems)
	}
}

// --- Decision tests ---

func TestDecisionValidate_ValidContinue(t *testing.T) {
	d := &Decision{
		Verdict:           VerdictContinue,
		RunID:             "run1",
		Accuracy:          0.84,
		ParseRate:         0.98,
		CostPerItem:       0.005,
		WorkflowsModified: []string{"reasoning-informed-captain-synthesis"},
		ChangesSummary:    "Changed model",
		Reasoning:         "Model was bottleneck",
		SanityPassed:      true,
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}
}

func TestDecisionValidate_UnknownVerdict(t *testing.T) {
	d := &Decision{
		Verdict:           "invalid",
		RunID:             "run1",
		WorkflowsModified: []string{"wf1"},
		ChangesSummary:    "test",
		Reasoning:         "test",
	}
	if err := d.Validate(); err == nil {
		t.Fatal("expected error for unknown verdict")
	}
}

func TestDecisionValidate_MatrixChangeWithoutRequest(t *testing.T) {
	d := &Decision{
		Verdict:           VerdictRequestMatrixChange,
		RunID:             "run1",
		WorkflowsModified: []string{"wf1"},
		ChangesSummary:    "test",
		Reasoning:         "test",
	}
	if err := d.Validate(); err == nil {
		t.Fatal("expected error for matrix change without request")
	}
}

func TestDecisionValidate_MissingRequiredFields(t *testing.T) {
	tests := []struct {
		name string
		d    Decision
	}{
		{"missing run_id", Decision{Verdict: VerdictContinue, WorkflowsModified: []string{"wf"}, ChangesSummary: "x", Reasoning: "x"}},
		{"missing changes_summary", Decision{Verdict: VerdictContinue, RunID: "r", WorkflowsModified: []string{"wf"}, Reasoning: "x"}},
		{"missing reasoning", Decision{Verdict: VerdictContinue, RunID: "r", WorkflowsModified: []string{"wf"}, ChangesSummary: "x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.d.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestReadDecision_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		t.Fatal(err)
	}

	d := &Decision{
		Verdict:           VerdictStopImproved,
		RunID:             "run42",
		Accuracy:          0.90,
		ParseRate:         0.99,
		FailedItems:       1,
		CostPerItem:       0.003,
		WorkflowsModified: []string{"reasoning-informed-captain-synthesis"},
		ChangesSummary:    "Improved prompt",
		Reasoning:         "Better instructions helped",
		SanityPassed:      true,
		RecommendFullRun:  true,
	}

	data, _ := json.MarshalIndent(d, "", "  ")
	if err := os.WriteFile(DecisionPath(dir), data, 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := ReadDecision(dir)
	if err != nil {
		t.Fatalf("read decision: %v", err)
	}
	if loaded.Verdict != VerdictStopImproved {
		t.Errorf("verdict: got %s, want stop_improved", loaded.Verdict)
	}
	if loaded.Accuracy != 0.90 {
		t.Errorf("accuracy: got %f, want 0.90", loaded.Accuracy)
	}
}

func TestRemoveDecision(t *testing.T) {
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Remove non-existent file should not error.
	if err := RemoveDecision(dir); err != nil {
		t.Fatalf("remove non-existent: %v", err)
	}

	// Create and remove.
	if err := os.WriteFile(DecisionPath(dir), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveDecision(dir); err != nil {
		t.Fatalf("remove existing: %v", err)
	}
	if _, err := os.Stat(DecisionPath(dir)); !os.IsNotExist(err) {
		t.Fatal("decision file should be gone")
	}
}

// --- Matrix tests ---

func TestMatrixProposalValidation(t *testing.T) {
	valid := &MatrixProposal{
		Benchmark:       "test",
		RunSet:          "lite",
		Split:           "dev",
		ItemLimit:       50,
		Concurrency:     20,
		WorkflowOrder:   []string{"benchmark-informed-captain-synthesis"},
		TargetWorkflows: []string{"reasoning-informed-captain-synthesis", "benchmark-informed-captain-synthesis"},
		Rationale:       "test",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid: %v", err)
	}

	// Missing benchmark.
	invalid := *valid
	invalid.Benchmark = ""
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected error for missing benchmark")
	}

	// Empty workflow order.
	invalid = *valid
	invalid.WorkflowOrder = nil
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected error for empty workflow_order")
	}

	// Item limit 0 is valid (means "all items").
	invalid = *valid
	invalid.ItemLimit = 0
	if err := invalid.Validate(); err != nil {
		t.Fatalf("item_limit 0 should be valid (all items), got: %v", err)
	}

	// Item limit < 0 is invalid.
	invalid = *valid
	invalid.ItemLimit = -1
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected error for item_limit < 0")
	}
}

func TestMatrixLockApproveAndRead(t *testing.T) {
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		t.Fatal(err)
	}

	proposal := &MatrixProposal{
		Benchmark:       "global-mmlu-lite",
		RunSet:          "lite",
		Split:           "dev",
		ItemLimit:       50,
		Concurrency:     20,
		WorkflowOrder:   []string{"benchmark-informed-captain-synthesis"},
		TargetWorkflows: []string{"reasoning-informed-captain-synthesis", "benchmark-informed-captain-synthesis"},
	}

	if err := ApproveMatrix(dir, proposal, "session1"); err != nil {
		t.Fatalf("approve: %v", err)
	}

	lock, err := ReadMatrixLock(dir)
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if lock.Benchmark != "global-mmlu-lite" {
		t.Errorf("benchmark: got %s", lock.Benchmark)
	}
	if lock.SessionID != "session1" {
		t.Errorf("session_id: got %s", lock.SessionID)
	}
	if lock.ApprovedAt.IsZero() {
		t.Error("approved_at should be set")
	}
}

func TestMatrixLockIsInMatrix(t *testing.T) {
	lock := &MatrixLock{
		MatrixProposal: MatrixProposal{
			Benchmark:     "global-mmlu-lite",
			Split:         "dev",
			WorkflowOrder: []string{"benchmark-informed-captain-synthesis"},
		},
	}

	if !lock.IsInMatrix("global-mmlu-lite", "benchmark-informed-captain-synthesis", "dev") {
		t.Error("should be in matrix")
	}
	if lock.IsInMatrix("other-bench", "benchmark-informed-captain-synthesis", "dev") {
		t.Error("wrong benchmark should not be in matrix")
	}
	if lock.IsInMatrix("global-mmlu-lite", "other-wf", "dev") {
		t.Error("wrong workflow should not be in matrix")
	}
	if lock.IsInMatrix("global-mmlu-lite", "benchmark-informed-captain-synthesis", "test") {
		t.Error("wrong split should not be in matrix")
	}
}

func TestMatrixLockValidateRunSetSplit(t *testing.T) {
	validCustom := &MatrixLock{MatrixProposal: MatrixProposal{RunSet: "custom", Split: "test"}}
	if err := validCustom.ValidateRunSetSplit(); err != nil {
		t.Fatalf("custom/test should be valid: %v", err)
	}

	validLite := &MatrixLock{MatrixProposal: MatrixProposal{RunSet: "lite", Split: "dev"}}
	if err := validLite.ValidateRunSetSplit(); err != nil {
		t.Fatalf("lite/dev should be valid: %v", err)
	}

	invalidLite := &MatrixLock{MatrixProposal: MatrixProposal{RunSet: "lite", Split: "test"}}
	if err := invalidLite.ValidateRunSetSplit(); err == nil {
		t.Fatal("lite/test should be invalid")
	}

	invalidCustom := &MatrixLock{MatrixProposal: MatrixProposal{RunSet: "custom", Split: ""}}
	if err := invalidCustom.ValidateRunSetSplit(); err == nil {
		t.Fatal("custom with empty split should be invalid")
	}
}

func TestMatrixLockMigrateLegacyRunSetSplit(t *testing.T) {
	legacy := &MatrixLock{MatrixProposal: MatrixProposal{RunSet: "lite", Split: "test"}}
	if !legacy.MigrateLegacyRunSetSplit() {
		t.Fatal("expected legacy lite/test lock to be migrated")
	}
	if legacy.RunSet != "custom" {
		t.Fatalf("expected run_set=custom after migration, got %q", legacy.RunSet)
	}
	if legacy.Split != "test" {
		t.Fatalf("expected split to remain test, got %q", legacy.Split)
	}
	if err := legacy.ValidateRunSetSplit(); err != nil {
		t.Fatalf("migrated lock should be valid: %v", err)
	}

	valid := &MatrixLock{MatrixProposal: MatrixProposal{RunSet: "lite", Split: "dev"}}
	if valid.MigrateLegacyRunSetSplit() {
		t.Fatal("did not expect migration for lite/dev")
	}
	if valid.RunSet != "lite" {
		t.Fatalf("run_set should remain lite, got %q", valid.RunSet)
	}
}

// --- Gate tests ---

func TestGate_AccuracyImprovement(t *testing.T) {
	state := &State{
		CurrentAccuracy:    0.80,
		CurrentParseRate:   0.98,
		CurrentCostPerItem: 0.005,
		CurrentFailedItems: 2,
	}
	decision := &Decision{
		Verdict:     VerdictContinue,
		Accuracy:    0.85,
		ParseRate:   0.98,
		CostPerItem: 0.005,
	}
	result := EvaluateGate(decision, state)
	if !result.Accept {
		t.Fatalf("expected accept, got reject: %s", result.Reason)
	}
}

func TestGate_AccuracyRegression(t *testing.T) {
	state := &State{
		CurrentAccuracy:    0.85,
		CurrentParseRate:   0.98,
		CurrentCostPerItem: 0.005,
	}
	decision := &Decision{
		Verdict:     VerdictContinue,
		Accuracy:    0.80,
		ParseRate:   0.98,
		CostPerItem: 0.005,
	}
	result := EvaluateGate(decision, state)
	if result.Accept {
		t.Fatal("expected reject on accuracy regression")
	}
	if !result.Override {
		t.Fatal("expected override when agent says continue but accuracy regressed")
	}
}

func TestGate_ParseRateBelowFloor(t *testing.T) {
	state := &State{
		CurrentAccuracy:    0.80,
		CurrentParseRate:   0.99,
		CurrentCostPerItem: 0.005,
	}
	decision := &Decision{
		Verdict:   VerdictContinue,
		Accuracy:  0.85,
		ParseRate: 0.90, // below MinParseRate (0.95) and below baseline-slack
	}
	result := EvaluateGate(decision, state)
	if result.Accept {
		t.Fatal("expected reject for low parse rate")
	}
}

func TestGate_FailedItemsIncrease(t *testing.T) {
	state := &State{
		CurrentAccuracy:    0.80,
		CurrentParseRate:   0.98,
		CurrentFailedItems: 2,
	}
	decision := &Decision{
		Verdict:     VerdictContinue,
		Accuracy:    0.85,
		ParseRate:   0.98,
		FailedItems: 5, // more than baseline
	}
	result := EvaluateGate(decision, state)
	if result.Accept {
		t.Fatal("expected reject for increased failed items")
	}
}

func TestGate_RollbackVerdict(t *testing.T) {
	state := &State{CurrentAccuracy: 0.80, CurrentParseRate: 0.98}
	decision := &Decision{
		Verdict:   VerdictRollback,
		Accuracy:  0.85,
		ParseRate: 0.98,
	}
	result := EvaluateGate(decision, state)
	if result.Accept {
		t.Fatal("expected reject for rollback verdict")
	}
}

func TestGate_StopNoProgress(t *testing.T) {
	state := &State{CurrentAccuracy: 0.80, CurrentParseRate: 0.98}
	decision := &Decision{
		Verdict:   VerdictStopNoProgress,
		Accuracy:  0.80,
		ParseRate: 0.98,
	}
	result := EvaluateGate(decision, state)
	if result.Accept {
		t.Fatal("expected reject for stop_no_progress")
	}
}

func TestGate_TiedAccuracy_ParseRateImprovement(t *testing.T) {
	state := &State{
		CurrentAccuracy:    0.80,
		CurrentParseRate:   0.95,
		CurrentCostPerItem: 0.005,
	}
	decision := &Decision{
		Verdict:     VerdictContinue,
		Accuracy:    0.80, // same
		ParseRate:   0.98, // better
		CostPerItem: 0.005,
	}
	result := EvaluateGate(decision, state)
	if !result.Accept {
		t.Fatalf("expected accept on parse rate improvement: %s", result.Reason)
	}
}

func TestGate_TiedAccuracyAndParseRate_CostReduction(t *testing.T) {
	state := &State{
		CurrentAccuracy:    0.80,
		CurrentParseRate:   0.98,
		CurrentCostPerItem: 0.010,
	}
	decision := &Decision{
		Verdict:     VerdictContinue,
		Accuracy:    0.80,
		ParseRate:   0.98,
		CostPerItem: 0.005, // 50% cost reduction
	}
	result := EvaluateGate(decision, state)
	if !result.Accept {
		t.Fatalf("expected accept on cost reduction: %s", result.Reason)
	}
}

func TestGate_NoImprovement(t *testing.T) {
	state := &State{
		CurrentAccuracy:    0.80,
		CurrentParseRate:   0.98,
		CurrentCostPerItem: 0.005,
	}
	decision := &Decision{
		Verdict:     VerdictContinue,
		Accuracy:    0.80,
		ParseRate:   0.98,
		CostPerItem: 0.005,
	}
	result := EvaluateGate(decision, state)
	if result.Accept {
		t.Fatal("expected reject for no improvement")
	}
}

func TestGate_StopImproved_Accepted(t *testing.T) {
	state := &State{
		CurrentAccuracy:    0.80,
		CurrentParseRate:   0.98,
		CurrentCostPerItem: 0.005,
	}
	decision := &Decision{
		Verdict:     VerdictStopImproved,
		Accuracy:    0.85,
		ParseRate:   0.99,
		CostPerItem: 0.004,
	}
	result := EvaluateGate(decision, state)
	if !result.Accept {
		t.Fatalf("expected accept for stop_improved with improvement: %s", result.Reason)
	}
}

// --- Memory tests ---

func TestMemoryInitAndAppend(t *testing.T) {
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		t.Fatal(err)
	}

	state := &State{
		TargetWorkflows: []string{"wf1", "wf2"},
		Benchmark:       "test-bench",
		Split:           "dev",
		ItemLimit:       50,
		StartedAt:       time.Now(),
	}

	if err := InitMemory(dir, state); err != nil {
		t.Fatalf("init: %v", err)
	}

	// Init again should be a no-op.
	if err := InitMemory(dir, state); err != nil {
		t.Fatalf("init again: %v", err)
	}

	// Append an accepted iteration.
	decision := &Decision{
		Verdict:        VerdictContinue,
		RunID:          "run1",
		Accuracy:       0.85,
		ParseRate:      0.98,
		CostPerItem:    0.005,
		FailedItems:    1,
		ChangesSummary: "Changed model",
		Reasoning:      "Model was the bottleneck",
	}
	if err := AppendIteration(dir, 1, decision, true); err != nil {
		t.Fatalf("append accepted: %v", err)
	}

	// First decision had TotalItems=0, so "Items:" should NOT appear.
	memContent := func() string { c, _ := ReadMemory(dir); return c }()
	if contains(memContent, "Items:") {
		t.Error("expected no 'Items:' in memory when TotalItems=0")
	}

	// Append a rolled-back iteration with TotalItems set.
	decision2 := &Decision{
		Verdict:         VerdictStopNoProgress,
		RunID:           "run2",
		Accuracy:        0.85,
		ParseRate:       0.98,
		CostPerItem:     0.005,
		TotalItems:      50,
		ChangesSummary:  "Tried new prompt",
		Reasoning:       "No change",
		FailureAnalysis: "3/5 synthesis error",
	}
	if err := AppendIteration(dir, 2, decision2, false); err != nil {
		t.Fatalf("append rejected: %v", err)
	}

	// Append a note.
	if err := AppendNote(dir, 3, "Agent crashed with timeout"); err != nil {
		t.Fatalf("append note: %v", err)
	}

	// Read and check content.
	content, err := ReadMemory(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	checks := []string{
		"# Benchmark Tuning Loop Memory",
		"wf1, wf2",
		"test-bench",
		"## Iteration 1 — ACCEPTED",
		"85.0%",
		"Changed model",
		"## Iteration 2 — ROLLED BACK (no progress)",
		"Items: 50",
		"3/5 synthesis error",
		"## Iteration 3 — NOTE",
		"Agent crashed with timeout",
	}
	for _, check := range checks {
		if !contains(content, check) {
			t.Errorf("memory should contain %q", check)
		}
	}
}

func TestReadMemory_NonExistent(t *testing.T) {
	content, err := ReadMemory(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "" {
		t.Fatalf("expected empty, got: %s", content)
	}
}

// --- Prompt tests ---

func TestBuildBaseSystemPrompt(t *testing.T) {
	cfg := &Config{}
	prompt := BuildBaseSystemPrompt(cfg)

	checks := []string{
		"reasoning workflow optimization agent",
		"Conctl Reference",
		"What You Can Change",
		"What You Must NOT Do",
	}
	for _, check := range checks {
		if !contains(prompt, check) {
			t.Errorf("base system prompt should contain %q", check)
		}
	}

	// Base prompt should NOT contain iteration-specific content.
	iterationOnly := []string{
		"Progressive Benchmark Protocol",
		"Decision File Contract",
		"Investigation Methodology",
	}
	for _, check := range iterationOnly {
		if contains(prompt, check) {
			t.Errorf("base system prompt should NOT contain %q (iteration-only)", check)
		}
	}

	// Permission-note section was removed when benchloop switched to fixed bypass mode.
	if contains(prompt, "Permission Note") {
		t.Error("should not contain permission note")
	}
}

func TestBuildIterationSystemPrompt(t *testing.T) {
	cfg := &Config{}
	prompt := BuildIterationSystemPrompt(cfg)

	checks := []string{
		"reasoning workflow optimization agent",
		"Progressive Benchmark Protocol",
		"Decision File Contract",
		"Investigation Methodology",
		"Do NOT run extra benchmarks",
	}
	for _, check := range checks {
		if !contains(prompt, check) {
			t.Errorf("iteration system prompt should contain %q", check)
		}
	}
}

func TestBuildSystemPrompt_NoPermissionSection(t *testing.T) {
	cfg := &Config{}
	prompt := BuildBaseSystemPrompt(cfg)
	if contains(prompt, "Permission Note") {
		t.Error("should not contain permission note section")
	}
}

func TestBuildIterationPrompt(t *testing.T) {
	cfg := &Config{
		TotalBudgetUSD:  20,
		AgentBudgetUSD:  3,
		AllowModelSwaps: true,
	}
	state := &State{
		Iteration:          2,
		MaxIterations:      10,
		CurrentAccuracy:    0.82,
		CurrentParseRate:   0.98,
		CurrentCostPerItem: 0.005,
		CurrentRunID:       "baseline-run",
	}
	lock := &MatrixLock{
		MatrixProposal: MatrixProposal{
			Benchmark:       "global-mmlu-lite",
			RunSet:          "lite",
			Split:           "dev",
			ItemLimit:       50,
			Concurrency:     20,
			WorkflowOrder:   []string{"benchmark-informed-captain-synthesis"},
			TargetWorkflows: []string{"reasoning-informed-captain-synthesis", "benchmark-informed-captain-synthesis"},
		},
	}

	prompt := BuildIterationPrompt(cfg, state, lock, "Previous memory content")

	checks := []string{
		"Iteration 3 of 10",
		"82.0%",
		"baseline-run",
		"Frozen Matrix",
		"global-mmlu-lite",
		"Previous memory content",
		"Targeted replay (1 item):",
		"Sanity:",
		"Small set:",
		"Compare with baseline:",
	}
	for _, check := range checks {
		if !contains(prompt, check) {
			t.Errorf("iteration prompt should contain %q", check)
		}
	}
}

func TestBuildIterationPrompt_Bootstrap(t *testing.T) {
	cfg := &Config{TotalBudgetUSD: 20, AgentBudgetUSD: 3}
	state := &State{MaxIterations: 10}
	lock := &MatrixLock{
		MatrixProposal: MatrixProposal{
			Benchmark:       "test",
			RunSet:          "lite",
			Split:           "dev",
			ItemLimit:       50,
			Concurrency:     20,
			WorkflowOrder:   []string{"wf1"},
			TargetWorkflows: []string{"wf1"},
		},
	}

	prompt := BuildIterationPrompt(cfg, state, lock, "")
	if !contains(prompt, "No baseline yet") {
		t.Error("bootstrap iteration should say 'No baseline yet'")
	}
	if contains(prompt, "Targeted replay (1 item):") {
		t.Error("bootstrap iteration should not have targeted replay command")
	}
	if contains(prompt, "Compare with baseline") {
		t.Error("bootstrap iteration should not have compare command")
	}
}

func TestBuildIterationPrompt_CustomRunSetIncludesSplit(t *testing.T) {
	cfg := &Config{AllowModelSwaps: false}
	state := &State{
		MaxIterations:      3,
		CurrentRunID:       "baseline-run",
		CurrentAccuracy:    0.82,
		CurrentParseRate:   0.98,
		CurrentCostPerItem: 0.004,
	}
	lock := &MatrixLock{
		MatrixProposal: MatrixProposal{
			Benchmark:       "global-mmlu-lite",
			RunSet:          "custom",
			Split:           "test",
			ItemLimit:       50,
			Concurrency:     20,
			WorkflowOrder:   []string{"benchmark-informed-captain-synthesis-cheap"},
			TargetWorkflows: []string{"reasoning-informed-captain-synthesis-cheap", "benchmark-informed-captain-synthesis-cheap"},
		},
	}

	prompt := BuildIterationPrompt(cfg, state, lock, "")
	if !contains(prompt, "--run-set custom --split test") {
		t.Fatalf("expected custom run command to include split, got:\n%s", prompt)
	}
	if contains(prompt, "--mode required --wait") {
		t.Fatalf("targeted replay command should not include unsupported --wait flag")
	}
	if !contains(prompt, "Model hints: disabled for this run") {
		t.Fatalf("expected model hints section to note swaps are disabled")
	}
}

func TestConctlInvocation(t *testing.T) {
	got := conctlInvocation("", "benchmarks runner-status --wait-until idle --interval 5s")
	want := `"${CONCTL_BIN}" benchmarks runner-status --wait-until idle --interval 5s`
	if got != want {
		t.Fatalf("conctlInvocation() = %q, want %q", got, want)
	}
}

func TestConctlInvocation_IgnoresWorkdir(t *testing.T) {
	got := conctlInvocation("/tmp/benchloop-workspace", "benchmarks runner-status --wait-until idle --interval 5s")
	want := `"${CONCTL_BIN}" benchmarks runner-status --wait-until idle --interval 5s`
	if got != want {
		t.Fatalf("conctlInvocation() = %q, want %q", got, want)
	}
}

func TestBuildIterationPrompt_UsesConctlBinEnv(t *testing.T) {
	cfg := &Config{
		AllowModelSwaps: false,
		Workdir:         "/tmp/benchloop-workspace",
	}
	state := &State{
		MaxIterations:      3,
		CurrentRunID:       "baseline-run",
		CurrentAccuracy:    0.82,
		CurrentParseRate:   0.98,
		CurrentCostPerItem: 0.004,
	}
	lock := &MatrixLock{
		MatrixProposal: MatrixProposal{
			Benchmark:       "global-mmlu-lite",
			RunSet:          "custom",
			Split:           "test",
			ItemLimit:       50,
			Concurrency:     20,
			WorkflowOrder:   []string{"benchmark-informed-captain-synthesis-cheap"},
			TargetWorkflows: []string{"reasoning-informed-captain-synthesis-cheap", "benchmark-informed-captain-synthesis-cheap"},
		},
	}

	prompt := BuildIterationPrompt(cfg, state, lock, "")
	if !contains(prompt, `"${CONCTL_BIN}" benchmarks runner-status --wait-until idle --interval 5s`) {
		t.Fatalf("expected CONCTL_BIN-aware conctl command in prompt, got:\n%s", prompt)
	}
}

func TestUpsertEnv(t *testing.T) {
	env := []string{"PATH=/usr/bin", "FOO=bar"}
	updated := upsertEnv(env, "PATH", "/custom/bin:/usr/bin")
	if updated[0] != "PATH=/custom/bin:/usr/bin" {
		t.Fatalf("expected PATH to be updated, got %q", updated[0])
	}
	updated = upsertEnv(updated, "CONCTL_BIN", "/repo/bin/conctl")
	if !containsStr(updated, "CONCTL_BIN=/repo/bin/conctl") {
		t.Fatalf("expected CONCTL_BIN to be appended, got %v", updated)
	}
}

// --- Agent args tests ---

func TestBuildClaudeArgs(t *testing.T) {
	cfg := &Config{
		Model:          "opus",
		AgentBudgetUSD: 3.0,
	}
	args := buildClaudeArgs(cfg, "session-1", "test system prompt")

	if !containsStr(args, "-p") {
		t.Error("should contain -p flag")
	}
	if !containsStr(args, "--model") {
		t.Error("should contain --model flag")
	}
	if !containsStr(args, "opus") {
		t.Error("should contain opus model value")
	}
	if !containsStr(args, "--dangerously-skip-permissions") {
		t.Error("should contain --dangerously-skip-permissions")
	}
	if !containsStr(args, "--permission-mode") || !containsStr(args, "bypassPermissions") {
		t.Error("should always use bypass permission mode")
	}
	if !containsStr(args, "--max-budget-usd") {
		t.Error("should contain --max-budget-usd")
	}
	if !containsStr(args, "--output-format") || !containsStr(args, "json") {
		t.Error("should contain --output-format json")
	}
	if !containsStr(args, "--append-system-prompt") {
		t.Error("should contain --append-system-prompt")
	}
	if !containsStr(args, "test system prompt") {
		t.Error("should contain the provided system prompt")
	}
	if !containsStr(args, "--session-id") {
		t.Error("should contain --session-id")
	}
	if !containsStr(args, "session-1") {
		t.Error("should contain session-id value")
	}
}

func TestBuildClaudeArgs_AlwaysBypassesPermissions(t *testing.T) {
	cfg := &Config{
		Model:          "sonnet",
		AgentBudgetUSD: 2.0,
	}
	args := buildClaudeArgs(cfg, "session-1", "test prompt")

	if !containsStr(args, "--dangerously-skip-permissions") {
		t.Error("should always contain --dangerously-skip-permissions")
	}
	if !containsStr(args, "--permission-mode") || !containsStr(args, "bypassPermissions") {
		t.Error("should always set permission mode to bypassPermissions")
	}
}

func TestBuildClaudeArgs_NoBudget(t *testing.T) {
	cfg := &Config{
		Model:          "opus",
		AgentBudgetUSD: 0, // unlimited
	}
	args := buildClaudeArgs(cfg, "session-1", "test prompt")

	if containsStr(args, "--max-budget-usd") {
		t.Error("should not contain --max-budget-usd when budget is 0")
	}
}

func TestBuildClaudeArgs_StreamJSONOutput(t *testing.T) {
	cfg := &Config{
		Model:             "opus",
		AgentOutputFormat: "stream-json",
	}
	args := buildClaudeArgs(cfg, "session-1", "test prompt")
	if !containsStr(args, "--output-format") || !containsStr(args, "stream-json") {
		t.Fatalf("expected stream-json output format, args=%v", args)
	}
}

func TestSessionIndexAppendReadAndCurrentLoopSession(t *testing.T) {
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		t.Fatal(err)
	}

	state := &State{
		SessionID: "loop-session-1",
		StartedAt: time.Now(),
	}
	if err := state.Save(dir); err != nil {
		t.Fatalf("save state: %v", err)
	}

	rec := &SessionRecord{
		LoopSessionID:   "loop-session-1",
		Phase:           "iteration",
		Iteration:       1,
		Attempt:         1,
		ClaudeSessionID: "claude-session-1",
		TranscriptPath:  "/tmp/claude-session-1.jsonl",
		LogPath:         "benchmarks/loop/iterations/1.log",
		ExitCode:        0,
		StartedAt:       time.Now(),
		FinishedAt:      time.Now(),
	}
	if err := AppendSessionRecord(dir, rec); err != nil {
		t.Fatalf("append session record: %v", err)
	}

	records, err := ReadSessionRecords(dir)
	if err != nil {
		t.Fatalf("read session records: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 session record, got %d", len(records))
	}
	if records[0].ClaudeSessionID != "claude-session-1" {
		t.Fatalf("unexpected claude session id: %s", records[0].ClaudeSessionID)
	}

	current, err := CurrentLoopSessionID(dir)
	if err != nil {
		t.Fatalf("current loop session: %v", err)
	}
	if current != "loop-session-1" {
		t.Fatalf("expected current loop session loop-session-1, got %s", current)
	}
}

func TestReadTranscriptMessages(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	lines := []string{
		`{"type":"assistant","timestamp":"2026-01-01T00:00:00Z","message":{"role":"assistant","content":[{"type":"text","text":"First assistant message"}]}}`,
		`{"type":"user","timestamp":"2026-01-01T00:00:01Z","message":{"role":"user","content":[{"type":"text","text":"User question"}]}}`,
		`{"type":"assistant","timestamp":"2026-01-01T00:00:02Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash"},{"type":"text","text":"Second assistant message"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	assistantOnly, err := ReadTranscriptMessages(path, TranscriptReadOptions{IncludeUser: false, MaxMessages: 10})
	if err != nil {
		t.Fatalf("read transcript (assistant only): %v", err)
	}
	if len(assistantOnly) != 2 {
		t.Fatalf("expected 2 assistant messages, got %d", len(assistantOnly))
	}

	allMessages, err := ReadTranscriptMessages(path, TranscriptReadOptions{IncludeUser: true, MaxMessages: 10})
	if err != nil {
		t.Fatalf("read transcript (include user): %v", err)
	}
	if len(allMessages) != 3 {
		t.Fatalf("expected 3 messages including user, got %d", len(allMessages))
	}

	lastOne, err := ReadTranscriptMessages(path, TranscriptReadOptions{IncludeUser: true, MaxMessages: 1})
	if err != nil {
		t.Fatalf("read transcript (last one): %v", err)
	}
	if len(lastOne) != 1 || lastOne[0].Text != "Second assistant message" {
		t.Fatalf("unexpected last message: %+v", lastOne)
	}
}

// --- Gate boundary tests ---

func TestGate_AccuracyWithinEpsilon(t *testing.T) {
	// Accuracy delta of 0.0005 is clearly within EpsilonAccuracy (0.001).
	// Code uses >, so deltas <= epsilon are not treated as improvement.
	state := &State{
		CurrentAccuracy:    0.800,
		CurrentParseRate:   0.98,
		CurrentCostPerItem: 0.005,
	}
	decision := &Decision{
		Verdict:     VerdictContinue,
		Accuracy:    0.8005, // delta 0.0005 — half of epsilon
		ParseRate:   0.98,
		CostPerItem: 0.005,
	}
	result := EvaluateGate(decision, state)
	if result.Accept {
		t.Fatal("accuracy delta within epsilon should NOT be accepted")
	}
}

func TestGate_ParseRateFloor_DynamicVsAbsolute(t *testing.T) {
	// High baseline: floor = max(0.95, 0.99-0.01) = 0.98
	t.Run("high_baseline_reject", func(t *testing.T) {
		state := &State{CurrentAccuracy: 0.80, CurrentParseRate: 0.99}
		decision := &Decision{
			Verdict:   VerdictContinue,
			Accuracy:  0.85,
			ParseRate: 0.979, // below 0.98 floor
		}
		result := EvaluateGate(decision, state)
		if result.Accept {
			t.Fatal("parse rate 0.979 below dynamic floor 0.98 should reject")
		}
	})

	// Low baseline: floor = max(0.95, 0.96-0.01) = 0.95
	t.Run("low_baseline_accept_at_floor", func(t *testing.T) {
		state := &State{CurrentAccuracy: 0.80, CurrentParseRate: 0.96, CurrentCostPerItem: 0.005}
		decision := &Decision{
			Verdict:     VerdictContinue,
			Accuracy:    0.85,
			ParseRate:   0.95, // exactly at floor (< is strict, so equal passes)
			CostPerItem: 0.005,
		}
		result := EvaluateGate(decision, state)
		if !result.Accept {
			t.Fatalf("parse rate exactly at floor 0.95 should pass: %s", result.Reason)
		}
	})
}

func TestGate_ParseRateExactlyAtFloor(t *testing.T) {
	// With baseline 0.98, floor = max(0.95, 0.97) = 0.97
	state := &State{CurrentAccuracy: 0.80, CurrentParseRate: 0.98, CurrentCostPerItem: 0.005}
	decision := &Decision{
		Verdict:     VerdictContinue,
		Accuracy:    0.85,
		ParseRate:   0.97, // exactly at floor
		CostPerItem: 0.005,
	}
	result := EvaluateGate(decision, state)
	if !result.Accept {
		t.Fatalf("parse rate exactly at floor should pass (< not <=): %s", result.Reason)
	}
}

func TestGate_CostTiebreaker_ZeroBaselineCost(t *testing.T) {
	// When baseline cost is 0, the cost comparison branch is skipped entirely.
	state := &State{
		CurrentAccuracy:    0.80,
		CurrentParseRate:   0.98,
		CurrentCostPerItem: 0, // zero cost baseline
	}
	decision := &Decision{
		Verdict:     VerdictContinue,
		Accuracy:    0.80,  // within epsilon
		ParseRate:   0.98,  // within epsilon
		CostPerItem: 0.001, // "improvement" — but guard skips this check
	}
	result := EvaluateGate(decision, state)
	if result.Accept {
		t.Fatal("with zero baseline cost, cost tiebreaker is skipped; should reject as no improvement")
	}
}

func TestGate_CostRatioBelowThreshold(t *testing.T) {
	// Cost ratio = (0.100 - 0.097) / 0.100 = 0.03, clearly below EpsilonCostRatio (0.05).
	// Code uses >, so ratios <= epsilon don't count as improvement.
	state := &State{
		CurrentAccuracy:    0.80,
		CurrentParseRate:   0.98,
		CurrentCostPerItem: 0.100,
	}
	decision := &Decision{
		Verdict:     VerdictContinue,
		Accuracy:    0.80,
		ParseRate:   0.98,
		CostPerItem: 0.097, // 3% reduction — below threshold
	}
	result := EvaluateGate(decision, state)
	if result.Accept {
		t.Fatal("cost ratio below 5% threshold should NOT count as improvement")
	}
}

func TestGate_StopImproved_ButAccuracyRegressed(t *testing.T) {
	state := &State{CurrentAccuracy: 0.85, CurrentParseRate: 0.98}
	decision := &Decision{
		Verdict:   VerdictStopImproved,
		Accuracy:  0.80, // regressed
		ParseRate: 0.98,
	}
	result := EvaluateGate(decision, state)
	if result.Accept {
		t.Fatal("should reject when agent says stop_improved but accuracy regressed")
	}
	if !result.Override {
		t.Fatal("should set Override=true when agent claims improvement but accuracy regressed")
	}
}

func TestGate_RollbackVerdict_WithAccuracyRegression(t *testing.T) {
	// Agent says rollback AND accuracy regressed. Rollback check fires first,
	// so Override should be false (agent already agrees to rollback).
	state := &State{CurrentAccuracy: 0.85, CurrentParseRate: 0.98}
	decision := &Decision{
		Verdict:   VerdictRollback,
		Accuracy:  0.80, // regressed too
		ParseRate: 0.98,
	}
	result := EvaluateGate(decision, state)
	if result.Accept {
		t.Fatal("rollback verdict should always reject")
	}
	if result.Override {
		t.Fatal("Override should be false when agent already requested rollback")
	}
	if !contains(result.Reason, "rollback") {
		t.Errorf("reason should mention rollback, got: %s", result.Reason)
	}
}

func TestGate_MatrixChangeRequest_EvenWithImprovement(t *testing.T) {
	state := &State{CurrentAccuracy: 0.80, CurrentParseRate: 0.98, CurrentCostPerItem: 0.005}
	decision := &Decision{
		Verdict:     VerdictRequestMatrixChange,
		Accuracy:    0.90, // significant improvement
		ParseRate:   0.99,
		CostPerItem: 0.003,
		MatrixChangeRequest: &MatrixChangeRequest{
			Description: "need more workflows",
		},
	}
	result := EvaluateGate(decision, state)
	if result.Accept {
		t.Fatal("matrix change request should reject even when metrics improved")
	}
}

func TestGate_FailedItems_EqualToBaseline(t *testing.T) {
	// FailedItems == baseline (not greater). Code uses >, so equal should pass.
	state := &State{
		CurrentAccuracy:    0.80,
		CurrentParseRate:   0.98,
		CurrentCostPerItem: 0.005,
		CurrentFailedItems: 3,
	}
	decision := &Decision{
		Verdict:     VerdictContinue,
		Accuracy:    0.85,
		ParseRate:   0.98,
		CostPerItem: 0.005,
		FailedItems: 3, // same as baseline
	}
	result := EvaluateGate(decision, state)
	if !result.Accept {
		t.Fatalf("failed items equal to baseline should pass (> not >=): %s", result.Reason)
	}
}

// --- Memory truncation tests ---

func TestReadMemoryTruncated_ShorterThanMax(t *testing.T) {
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		t.Fatal(err)
	}

	content := "# Header\nLine 1\nLine 2\nLine 3\n"
	if err := os.WriteFile(MemoryPath(dir), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := ReadMemoryTruncated(dir, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != content {
		t.Errorf("content should be unchanged when shorter than maxLines")
	}
}

func TestReadMemoryTruncated_NoSeparator(t *testing.T) {
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		t.Fatal(err)
	}

	// 30 lines, no --- separator. headerEnd=0, all goes to remaining.
	var lines []string
	for i := 0; i < 30; i++ {
		lines = append(lines, fmt.Sprintf("Line %d", i))
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(MemoryPath(dir), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := ReadMemoryTruncated(dir, 15)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(result, "truncated") {
		t.Error("should contain truncation notice")
	}
	if !contains(result, "Line 29") {
		t.Error("should contain the last line")
	}
}

func TestReadMemoryTruncated_SmallMaxForcesTailMinimum(t *testing.T) {
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Header: 5 lines before separator, then 20 remaining lines.
	var lines []string
	lines = append(lines, "# Header", "Target: wf1", "Benchmark: test", "Split: dev", "Baseline: TBD")
	lines = append(lines, "---")
	for i := 0; i < 19; i++ {
		lines = append(lines, fmt.Sprintf("Iteration line %d", i))
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(MemoryPath(dir), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// maxLines=3 with 5-line header: tailBudget = 3-5-2 = -4, clamped to 10.
	// remaining has 20 lines, 10 < 20, so truncation happens keeping last 10.
	result, err := ReadMemoryTruncated(dir, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !contains(result, "truncated") {
		t.Error("should contain truncation notice")
	}
	if !contains(result, "Iteration line 18") {
		t.Error("should contain the last iteration line")
	}
	// Header should be preserved.
	if !contains(result, "# Header") {
		t.Error("header should be preserved")
	}
}

func TestReadMemoryTruncated_PreservesHeader(t *testing.T) {
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		t.Fatal(err)
	}

	var lines []string
	lines = append(lines, "# Benchmark Tuning Loop Memory", "", "## Session", "- Target: wf1", "- Benchmark: test-bench")
	lines = append(lines, "---", "")
	for i := 0; i < 200; i++ {
		lines = append(lines, fmt.Sprintf("## Iteration %d — ACCEPTED", i+1))
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(MemoryPath(dir), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := ReadMemoryTruncated(dir, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Header lines (before ---) should be intact.
	if !contains(result, "# Benchmark Tuning Loop Memory") {
		t.Error("header first line should be preserved")
	}
	if !contains(result, "- Target: wf1") {
		t.Error("header target line should be preserved")
	}
	// Truncation notice should appear.
	if !contains(result, "truncated") {
		t.Error("should contain truncation notice")
	}
	// Last iteration should be present.
	if !contains(result, "## Iteration 200") {
		t.Error("last iteration should be present")
	}
}

// --- Archive tests ---

func TestArchive_NoPreviousSession(t *testing.T) {
	dir := t.TempDir()
	sessionID, err := ArchivePreviousSession(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sessionID != "" {
		t.Fatalf("expected empty session ID, got %q", sessionID)
	}
}

func TestArchive_PartialArtifacts(t *testing.T) {
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create only state.json and matrix.lock.json (no iterations/, baselines/, etc.)
	stateJSON := `{"session_id":"sess_partial"}`
	if err := os.WriteFile(filepath.Join(loopDir, "state.json"), []byte(stateJSON), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(loopDir, "matrix.lock.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	sessionID, err := ArchivePreviousSession(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sessionID != "sess_partial" {
		t.Fatalf("expected sess_partial, got %q", sessionID)
	}

	// Verify archived files exist.
	archiveDir := filepath.Join(loopDir, "archive", "sess_partial")
	if _, err := os.Stat(filepath.Join(archiveDir, "state.json")); err != nil {
		t.Error("state.json should be archived")
	}
	if _, err := os.Stat(filepath.Join(archiveDir, "matrix.lock.json")); err != nil {
		t.Error("matrix.lock.json should be archived")
	}
	// Original files should be gone.
	if _, err := os.Stat(filepath.Join(loopDir, "state.json")); !os.IsNotExist(err) {
		t.Error("original state.json should be moved")
	}
}

func TestArchive_PreservesMemory(t *testing.T) {
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		t.Fatal(err)
	}

	stateJSON := `{"session_id":"sess_memory"}`
	if err := os.WriteFile(filepath.Join(loopDir, "state.json"), []byte(stateJSON), 0644); err != nil {
		t.Fatal(err)
	}
	memoryContent := "# Memory\nImportant accumulated knowledge\n"
	if err := os.WriteFile(filepath.Join(loopDir, "loop_memory.md"), []byte(memoryContent), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ArchivePreviousSession(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Memory file must still exist at original location.
	data, err := os.ReadFile(filepath.Join(loopDir, "loop_memory.md"))
	if err != nil {
		t.Fatal("loop_memory.md should NOT be archived — it must persist across sessions")
	}
	if string(data) != memoryContent {
		t.Error("memory content should be unchanged")
	}

	// Memory should NOT be in the archive.
	archiveDir := filepath.Join(loopDir, "archive", "sess_memory")
	if _, err := os.Stat(filepath.Join(archiveDir, "loop_memory.md")); !os.IsNotExist(err) {
		t.Error("loop_memory.md should NOT appear in archive")
	}
}

func TestArchiveCleanSlate_ArchivesMemoryIndexAndDecision(t *testing.T) {
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		t.Fatal(err)
	}

	sessionID := "sess_clean"
	if err := os.WriteFile(filepath.Join(loopDir, "state.json"), []byte(`{"session_id":"`+sessionID+`"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(loopDir, "matrix.lock.json"), []byte(`{"benchmark":"x","run_set":"custom","split":"test","item_limit":5,"concurrency":2,"workflow_order":["benchmark-a"],"target_workflows":["benchmark-a","reasoning-a"]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(loopDir, "loop_memory.md"), []byte("# Memory\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(loopDir, "session_index.jsonl"), []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	decisionJSON := `{
  "verdict": "continue",
  "run_id": "run_1",
  "accuracy": 0.84,
  "parse_rate": 0.98,
  "failed_items": 1,
  "total_items": 50,
  "cost_per_item": 0.0045,
  "workflows_modified": ["reasoning-informed-captain-synthesis"],
  "changes_summary": "test",
  "reasoning": "test reasoning",
  "sanity_passed": true
}`
	if err := os.WriteFile(filepath.Join(loopDir, "decision.json"), []byte(decisionJSON), 0644); err != nil {
		t.Fatal(err)
	}

	gotSession, err := ArchiveCleanSlate(dir, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotSession != sessionID {
		t.Fatalf("expected session %q, got %q", sessionID, gotSession)
	}

	archiveDir := filepath.Join(loopDir, "archive", sessionID)
	for _, name := range []string{"state.json", "matrix.lock.json", "decision.json", "loop_memory.md", "session_index.jsonl"} {
		if _, err := os.Stat(filepath.Join(archiveDir, name)); err != nil {
			t.Fatalf("expected archived %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(loopDir, "loop_memory.md")); !os.IsNotExist(err) {
		t.Fatal("loop_memory.md should be moved during clean-slate archive")
	}
}

func TestArchiveCleanSlate_InvalidDecisionRequiresForce(t *testing.T) {
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		t.Fatal(err)
	}

	sessionID := "sess_force"
	if err := os.WriteFile(filepath.Join(loopDir, "state.json"), []byte(`{"session_id":"`+sessionID+`"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(loopDir, "decision.json"), []byte(`{"verdict":"continue"}`), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := ArchiveCleanSlate(dir, false); err == nil {
		t.Fatal("expected validation error for invalid decision without --force")
	}
	if _, err := os.Stat(filepath.Join(loopDir, "state.json")); err != nil {
		t.Fatalf("state.json should remain in place on validation failure: %v", err)
	}

	gotSession, err := ArchiveCleanSlate(dir, true)
	if err != nil {
		t.Fatalf("unexpected error with force=true: %v", err)
	}
	if gotSession != sessionID {
		t.Fatalf("expected session %q, got %q", sessionID, gotSession)
	}
	if _, err := os.Stat(filepath.Join(loopDir, "archive", sessionID, "decision.json")); err != nil {
		t.Fatalf("decision.json should be archived when forced: %v", err)
	}
}

func TestArchive_MovesSessionScopedLogs(t *testing.T) {
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	sessionID := "sess_logs"
	if err := os.MkdirAll(filepath.Join(loopDir, "sessions", sessionID, "iterations"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(loopDir, "state.json"), []byte(`{"session_id":"`+sessionID+`"}`), 0644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(loopDir, "sessions", sessionID, "iterations", "1.log")
	if err := os.WriteFile(logPath, []byte("log"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := ArchivePreviousSession(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	archived := filepath.Join(loopDir, "archive", sessionID, "sessions", "iterations", "1.log")
	if _, err := os.Stat(archived); err != nil {
		t.Fatalf("session-scoped log should be archived: %v", err)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatal("original session-scoped log should be moved")
	}
}

func TestArchive_MovesLegacyIterationsWhenSessionLogsMissing(t *testing.T) {
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	sessionID := "sess_legacy"
	if err := os.MkdirAll(filepath.Join(loopDir, "iterations"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(loopDir, "state.json"), []byte(`{"session_id":"`+sessionID+`"}`), 0644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(loopDir, "iterations", "2.log")
	if err := os.WriteFile(logPath, []byte("legacy"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := ArchivePreviousSession(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	archived := filepath.Join(loopDir, "archive", sessionID, "iterations", "2.log")
	if _, err := os.Stat(archived); err != nil {
		t.Fatalf("legacy iteration log should be archived: %v", err)
	}
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatal("original legacy iteration log should be moved")
	}
}

func TestArchive_EmptySessionID(t *testing.T) {
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		t.Fatal(err)
	}

	// state.json with empty session_id.
	stateJSON := `{"session_id":""}`
	if err := os.WriteFile(filepath.Join(loopDir, "state.json"), []byte(stateJSON), 0644); err != nil {
		t.Fatal(err)
	}

	sessionID, err := ArchivePreviousSession(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sessionID != "" {
		t.Fatalf("expected empty session ID for empty session_id in state, got %q", sessionID)
	}
}

// --- Transcript tests ---

func TestTranscript_EmptyAndWhitespaceLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	lines := []string{
		"",
		"   ",
		`{"type":"assistant","timestamp":"2026-01-01T00:00:00Z","message":{"role":"assistant","content":[{"type":"text","text":"Hello"}]}}`,
		"",
		`{"type":"assistant","timestamp":"2026-01-01T00:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"World"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	msgs, err := ReadTranscriptMessages(path, TranscriptReadOptions{MaxMessages: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (blank lines skipped), got %d", len(msgs))
	}
}

func TestTranscript_MalformedJSONSkipped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	lines := []string{
		`{"type":"assistant","timestamp":"2026-01-01T00:00:00Z","message":{"role":"assistant","content":[{"type":"text","text":"Valid 1"}]}}`,
		`not valid json at all`,
		`{"broken json`,
		`{"type":"assistant","timestamp":"2026-01-01T00:00:02Z","message":{"role":"assistant","content":[{"type":"text","text":"Valid 2"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	msgs, err := ReadTranscriptMessages(path, TranscriptReadOptions{MaxMessages: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 valid messages (malformed skipped), got %d", len(msgs))
	}
	if msgs[0].Text != "Valid 1" || msgs[1].Text != "Valid 2" {
		t.Errorf("wrong messages: %+v", msgs)
	}
}

func TestTranscript_ContentAsPlainString(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	// User message with content as a plain JSON string (not array).
	lines := []string{
		`{"type":"user","timestamp":"2026-01-01T00:00:00Z","message":{"role":"user","content":"Plain text user message"}}`,
		`{"type":"assistant","timestamp":"2026-01-01T00:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"Response"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	msgs, err := ReadTranscriptMessages(path, TranscriptReadOptions{IncludeUser: true, MaxMessages: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Text != "Plain text user message" {
		t.Errorf("user message not parsed from plain string: %q", msgs[0].Text)
	}
	if msgs[0].Role != "user" {
		t.Errorf("expected user role, got %s", msgs[0].Role)
	}
}

func TestTranscript_ToolUseFormatting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	lines := []string{
		// Bash with command + description.
		`{"type":"assistant","timestamp":"T1","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"ls -la","description":"List files"}}]}}`,
		// Bash with command only.
		`{"type":"assistant","timestamp":"T2","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"git status"}}]}}`,
		// Read with file_path.
		`{"type":"assistant","timestamp":"T3","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read","input":{"file_path":"/tmp/test.go"}}]}}`,
		// Grep with pattern.
		`{"type":"assistant","timestamp":"T4","message":{"role":"assistant","content":[{"type":"tool_use","name":"Grep","input":{"pattern":"TODO"}}]}}`,
		// Unknown tool with no known fields.
		`{"type":"assistant","timestamp":"T5","message":{"role":"assistant","content":[{"type":"tool_use","name":"CustomTool","input":{"foo":"bar"}}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	msgs, err := ReadTranscriptMessages(path, TranscriptReadOptions{IncludeTools: true, MaxMessages: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 5 {
		t.Fatalf("expected 5 tool call messages, got %d", len(msgs))
	}

	// Bash with command + description: [Bash (`ls -la`)]List files
	if !contains(msgs[0].Text, "Bash") || !contains(msgs[0].Text, "ls -la") || !contains(msgs[0].Text, "List files") {
		t.Errorf("bash+desc format wrong: %q", msgs[0].Text)
	}
	// Bash with command only: [Bash] `git status`
	if !contains(msgs[1].Text, "Bash") || !contains(msgs[1].Text, "git status") {
		t.Errorf("bash-only format wrong: %q", msgs[1].Text)
	}
	// Read: [Read] /tmp/test.go
	if !contains(msgs[2].Text, "Read") || !contains(msgs[2].Text, "/tmp/test.go") {
		t.Errorf("read format wrong: %q", msgs[2].Text)
	}
	// Grep: [Grep] TODO
	if !contains(msgs[3].Text, "Grep") || !contains(msgs[3].Text, "TODO") {
		t.Errorf("grep format wrong: %q", msgs[3].Text)
	}
	// Unknown: [CustomTool]
	if !contains(msgs[4].Text, "CustomTool") {
		t.Errorf("custom tool format wrong: %q", msgs[4].Text)
	}
}

func TestTranscript_ToolResultFormatting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	lines := []string{
		// tool_result with string content.
		`{"type":"user","timestamp":"T1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"id1","content":"String result content"}]}}`,
		// tool_result with JSON content.
		`{"type":"user","timestamp":"T2","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"id2","content":{"key":"value"}}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	msgs, err := ReadTranscriptMessages(path, TranscriptReadOptions{IncludeTools: true, MaxMessages: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 tool result messages, got %d", len(msgs))
	}
	if msgs[0].Text != "String result content" {
		t.Errorf("string result wrong: %q", msgs[0].Text)
	}
	if !contains(msgs[1].Text, "key") {
		t.Errorf("JSON result should contain key: %q", msgs[1].Text)
	}
}

func TestTranscript_MaxMessagesZero(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	var lines []string
	for i := 0; i < 10; i++ {
		lines = append(lines, fmt.Sprintf(`{"type":"assistant","timestamp":"T%d","message":{"role":"assistant","content":[{"type":"text","text":"Msg %d"}]}}`, i, i))
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	msgs, err := ReadTranscriptMessages(path, TranscriptReadOptions{MaxMessages: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 10 {
		t.Fatalf("MaxMessages=0 should return all messages, got %d", len(msgs))
	}
}

func TestTranscript_IncludeToolsFlag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	lines := []string{
		`{"type":"assistant","timestamp":"T1","message":{"role":"assistant","content":[{"type":"text","text":"Text msg"},{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"user","timestamp":"T2","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"id1","content":"result"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Without tools: only text message.
	noTools, err := ReadTranscriptMessages(path, TranscriptReadOptions{IncludeTools: false, MaxMessages: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(noTools) != 1 {
		t.Fatalf("without tools expected 1 message, got %d", len(noTools))
	}
	if noTools[0].Text != "Text msg" {
		t.Errorf("expected text msg, got %q", noTools[0].Text)
	}

	// With tools: text + tool_use + tool_result.
	withTools, err := ReadTranscriptMessages(path, TranscriptReadOptions{IncludeTools: true, MaxMessages: 0})
	if err != nil {
		t.Fatal(err)
	}
	if len(withTools) != 3 {
		t.Fatalf("with tools expected 3 messages, got %d", len(withTools))
	}
}

// --- Config edge case tests ---

func TestConfig_IsExplicit_NilMap(t *testing.T) {
	cfg := &Config{ExplicitFlags: nil}
	if cfg.IsExplicit("anything") {
		t.Fatal("IsExplicit should return false for nil map")
	}
}

func TestConfig_WhitespaceOnlyWorkflow(t *testing.T) {
	cfg := &Config{
		Workflows:        []string{"  ", "wf1"},
		Benchmark:        "test",
		RunSet:           "lite",
		MaxIterations:    3,
		StopAfterPlateau: 2,
		IterationTimeout: 5 * time.Minute,
		ClaudeBin:        "echo",
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for whitespace-only workflow")
	}
	if !contains(err.Error(), "empty") {
		t.Errorf("error should mention empty, got: %s", err.Error())
	}
}

func TestConfig_ZeroBudgets_BothUnlimited(t *testing.T) {
	cfg := &Config{
		Workflows:        []string{"wf1"},
		Benchmark:        "test",
		RunSet:           "lite",
		Split:            "dev",
		ItemLimit:        50,
		Concurrency:      20,
		MaxIterations:    3,
		StopAfterPlateau: 2,
		IterationTimeout: 5 * time.Minute,
		TotalBudgetUSD:   0, // unlimited
		AgentBudgetUSD:   0, // unlimited
		ClaudeBin:        "echo",
		ExplicitFlags: map[string]bool{
			"split":       true,
			"item-limit":  true,
			"concurrency": true,
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("zero budgets (both unlimited) should be valid: %v", err)
	}
}

// --- Matrix edge case tests ---

func TestMatrix_UpdateMatrix_ChangesApprovedAt(t *testing.T) {
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		t.Fatal(err)
	}

	proposal := &MatrixProposal{
		Benchmark:       "test",
		RunSet:          "lite",
		Split:           "dev",
		ItemLimit:       50,
		Concurrency:     20,
		WorkflowOrder:   []string{"wf1"},
		TargetWorkflows: []string{"wf1"},
	}
	if err := ApproveMatrix(dir, proposal, "sess1"); err != nil {
		t.Fatal(err)
	}

	lock1, err := ReadMatrixLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	originalTime := lock1.ApprovedAt

	// Update the matrix (simulating approved expansion).
	lock1.WorkflowOrder = append(lock1.WorkflowOrder, "wf2")
	if err := UpdateMatrix(dir, lock1); err != nil {
		t.Fatal(err)
	}

	lock2, err := ReadMatrixLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	// ApprovedAt is set to time.Now() in UpdateMatrix, so it should be >= original.
	// In fast tests they might be equal, which is fine.
	if lock2.ApprovedAt.Before(originalTime) {
		t.Errorf("expected ApprovedAt >= original, got %v < %v", lock2.ApprovedAt, originalTime)
	}
	if len(lock2.WorkflowOrder) != 2 {
		t.Errorf("expected 2 workflows after update, got %d", len(lock2.WorkflowOrder))
	}
}

func TestMatrix_IsInMatrix_TargetWorkflowNotInOrder(t *testing.T) {
	// A workflow in TargetWorkflows but NOT in WorkflowOrder should not be "in matrix".
	lock := &MatrixLock{
		MatrixProposal: MatrixProposal{
			Benchmark:       "global-mmlu-lite",
			Split:           "dev",
			WorkflowOrder:   []string{"benchmark-informed-captain-synthesis"},
			TargetWorkflows: []string{"reasoning-informed-captain-synthesis", "benchmark-informed-captain-synthesis"},
		},
	}

	// reasoning-informed-captain-synthesis is in TargetWorkflows but NOT in WorkflowOrder.
	if lock.IsInMatrix("global-mmlu-lite", "reasoning-informed-captain-synthesis", "dev") {
		t.Error("workflow in TargetWorkflows but NOT in WorkflowOrder should not be in matrix")
	}
	// benchmark-informed-captain-synthesis is in WorkflowOrder.
	if !lock.IsInMatrix("global-mmlu-lite", "benchmark-informed-captain-synthesis", "dev") {
		t.Error("workflow in WorkflowOrder should be in matrix")
	}
}

// --- Decision edge case tests ---

func TestDecision_ValidMatrixChangeWithRequest(t *testing.T) {
	d := &Decision{
		Verdict:           VerdictRequestMatrixChange,
		RunID:             "run1",
		WorkflowsModified: []string{"wf1"},
		ChangesSummary:    "Need more workflows",
		Reasoning:         "Current matrix is too narrow",
		MatrixChangeRequest: &MatrixChangeRequest{
			Description: "Add benchmark-judge to the matrix",
			Workflows:   []string{"benchmark-judge-pick"},
		},
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("valid matrix change with request should pass: %v", err)
	}
}

func TestReadDecision_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(DecisionPath(dir), []byte("not json {{{"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ReadDecision(dir)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !contains(err.Error(), "parse") {
		t.Errorf("error should mention parse, got: %s", err.Error())
	}
}

// --- Session index tests ---

func TestSessionIndex_MultipleRecords(t *testing.T) {
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	records := []*SessionRecord{
		{LoopSessionID: "sess1", Phase: "iteration", Iteration: 1, Attempt: 1, ClaudeSessionID: "c1", StartedAt: now, FinishedAt: now},
		{LoopSessionID: "sess1", Phase: "iteration", Iteration: 1, Attempt: 2, ClaudeSessionID: "c2", ExitCode: 1, StartedAt: now, FinishedAt: now},
		{LoopSessionID: "sess1", Phase: "iteration", Iteration: 2, Attempt: 3, ClaudeSessionID: "c3", StartedAt: now, FinishedAt: now},
	}
	for _, r := range records {
		if err := AppendSessionRecord(dir, r); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	loaded, err := ReadSessionRecords(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("expected 3 records, got %d", len(loaded))
	}
	if loaded[0].Phase != "iteration" {
		t.Errorf("first record phase: got %s", loaded[0].Phase)
	}
	if loaded[1].ExitCode != 1 {
		t.Errorf("second record exit code: got %d", loaded[1].ExitCode)
	}
	if loaded[2].ClaudeSessionID != "c3" {
		t.Errorf("third record session: got %s", loaded[2].ClaudeSessionID)
	}
}

func TestSessionIndex_ReadNonExistent(t *testing.T) {
	records, err := ReadSessionRecords(t.TempDir())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if records != nil {
		t.Fatalf("expected nil for non-existent file, got %v", records)
	}
}

func TestSessionIndex_SkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		t.Fatal(err)
	}

	lines := strings.Join([]string{
		`{"loop_session_id":"sess1","phase":"iteration","iteration":1,"attempt":1,"claude_session_id":"c1","started_at":"2026-01-01T00:00:00Z","finished_at":"2026-01-01T00:00:01Z"}`,
		`{not-json`,
		`{"loop_session_id":"sess1","phase":"iteration","iteration":1,"attempt":2,"claude_session_id":"c2","started_at":"2026-01-01T00:00:02Z","finished_at":"2026-01-01T00:00:03Z"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(SessionIndexPath(dir), []byte(lines), 0644); err != nil {
		t.Fatal(err)
	}

	records, err := ReadSessionRecords(dir)
	if err != nil {
		t.Fatalf("read should skip malformed lines, got error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 valid records, got %d", len(records))
	}
	if records[0].ClaudeSessionID != "c1" || records[1].ClaudeSessionID != "c2" {
		t.Fatalf("unexpected records: %+v", records)
	}
}

func TestReadLiveSessionRecord_FromState(t *testing.T) {
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		t.Fatal(err)
	}

	start := time.Now().Add(-2 * time.Minute).UTC()
	state := &State{
		SessionID: "sess-live",
		Status:    "running",
		StartedAt: time.Now().UTC(),
		Live: &LiveStatus{
			Phase:          "iteration",
			Step:           "agent_running",
			Iteration:      2,
			Attempt:        3,
			AgentSessionID: "live-uuid",
			AgentLogPath:   "/tmp/attempt.log",
			TranscriptPath: "/tmp/live.jsonl",
			StartedAt:      &start,
		},
	}
	if err := state.Save(dir); err != nil {
		t.Fatalf("save state: %v", err)
	}

	record, err := ReadLiveSessionRecord(dir)
	if err != nil {
		t.Fatalf("read live session record: %v", err)
	}
	if record == nil {
		t.Fatal("expected live session record")
	}
	if !record.InFlight {
		t.Fatal("expected in-flight live record")
	}
	if record.LoopSessionID != "sess-live" || record.ClaudeSessionID != "live-uuid" {
		t.Fatalf("unexpected live record: %+v", record)
	}
	if record.Attempt != 3 || record.Iteration != 2 {
		t.Fatalf("unexpected iteration/attempt: %+v", record)
	}
}

func TestReadLiveSessionRecord_IgnoresNonRunningState(t *testing.T) {
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		t.Fatal(err)
	}

	state := &State{
		SessionID: "sess-live",
		Status:    "completed",
		StartedAt: time.Now().UTC(),
		Live: &LiveStatus{
			Phase:          "iteration",
			Step:           "agent_running",
			Iteration:      2,
			Attempt:        3,
			AgentSessionID: "live-uuid",
		},
	}
	if err := state.Save(dir); err != nil {
		t.Fatalf("save state: %v", err)
	}

	record, err := ReadLiveSessionRecord(dir)
	if err != nil {
		t.Fatalf("read live session record: %v", err)
	}
	if record != nil {
		t.Fatalf("expected nil live record for non-running state, got %+v", record)
	}
}

func TestReadLiveSessionRecord_IgnoresStaleLock(t *testing.T) {
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		t.Fatal(err)
	}

	state := &State{
		SessionID: "sess-live",
		Status:    "running",
		StartedAt: time.Now().UTC(),
		Live: &LiveStatus{
			Phase:          "iteration",
			Step:           "agent_running",
			Iteration:      2,
			Attempt:        3,
			AgentSessionID: "live-uuid",
		},
	}
	if err := state.Save(dir); err != nil {
		t.Fatalf("save state: %v", err)
	}

	meta := RunLockMetadata{
		PID:       999999, // intentionally stale/non-existent
		CreatedAt: time.Now().UTC(),
		SessionID: "sess-live",
		Workdir:   dir,
		Resume:    true,
	}
	lockBytes, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal lock: %v", err)
	}
	if err := os.WriteFile(LockPath(dir), lockBytes, 0644); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	record, err := ReadLiveSessionRecord(dir)
	if err != nil {
		t.Fatalf("read live session record: %v", err)
	}
	if record != nil {
		t.Fatalf("expected nil live record for stale lock, got %+v", record)
	}
}

func TestMergeLiveSessionRecord_DedupesPersistedAttempt(t *testing.T) {
	base := []SessionRecord{
		{
			LoopSessionID:   "sess1",
			Iteration:       1,
			Attempt:         2,
			ClaudeSessionID: "c2",
		},
	}
	live := &SessionRecord{
		LoopSessionID:   "sess1",
		Iteration:       1,
		Attempt:         2,
		ClaudeSessionID: "c2",
		InFlight:        true,
	}
	merged := MergeLiveSessionRecord(base, live)
	if len(merged) != 1 {
		t.Fatalf("expected deduped records length 1, got %d", len(merged))
	}
}

func TestUpdateLiveStatus_ResetsOnNewAttempt(t *testing.T) {
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		t.Fatal(err)
	}

	l := &Loop{
		cfg:   &Config{Workdir: dir},
		state: &State{SessionID: "sess-live", StartedAt: time.Now().UTC()},
	}

	l.updateLiveStatus("iteration", "agent_running", 1, 1, "attempt one")
	if l.state.Live == nil || l.state.Live.StartedAt == nil {
		t.Fatal("expected live status initialized")
	}
	firstStart := *l.state.Live.StartedAt
	l.state.Live.AgentSessionID = "old-session"

	time.Sleep(10 * time.Millisecond)
	l.updateLiveStatus("iteration", "preflight", 1, 2, "attempt two")

	if l.state.Live.AgentSessionID != "" {
		t.Fatal("expected live agent fields reset on new attempt")
	}
	if l.state.Live.StartedAt == nil || !l.state.Live.StartedAt.After(firstStart) {
		t.Fatal("expected started_at to reset for new attempt")
	}
	if l.state.Live.Attempt != 2 {
		t.Fatalf("expected attempt=2, got %d", l.state.Live.Attempt)
	}
}

// helpers

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func containsStr(ss []string, target string) bool {
	for _, s := range ss {
		if s == target {
			return true
		}
	}
	return false
}
