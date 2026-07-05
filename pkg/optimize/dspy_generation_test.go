package optimize

import (
	"testing"
)

// --- canSpendDSPYMetricBudget ---

func TestCanSpendDSPYMetricBudget_NonGEPA(t *testing.T) {
	// Non-GEPA optimizer: always allowed regardless of budget
	runtime := DSPYRuntimeSettings{
		Optimizer:      MutatorModeMIPROv2,
		MaxMetricCalls: 10,
	}
	used := 100
	if !canSpendDSPYMetricBudget(runtime, &used, 5) {
		t.Error("expected spend to be allowed for non-GEPA optimizer")
	}
}

func TestCanSpendDSPYMetricBudget_NoMaxLimit(t *testing.T) {
	// GEPA with no max: always allowed
	runtime := DSPYRuntimeSettings{
		Optimizer:      MutatorModeGEPA,
		MaxMetricCalls: 0,
	}
	used := 9999
	if !canSpendDSPYMetricBudget(runtime, &used, 100) {
		t.Error("expected spend to be allowed when MaxMetricCalls=0")
	}
}

func TestCanSpendDSPYMetricBudget_NilPointer(t *testing.T) {
	// Nil pointer: always allowed
	runtime := DSPYRuntimeSettings{
		Optimizer:      MutatorModeGEPA,
		MaxMetricCalls: 10,
	}
	if !canSpendDSPYMetricBudget(runtime, nil, 5) {
		t.Error("expected spend to be allowed when metricCallsUsed is nil")
	}
}

func TestCanSpendDSPYMetricBudget_WithinBudget(t *testing.T) {
	runtime := DSPYRuntimeSettings{
		Optimizer:      MutatorModeGEPA,
		MaxMetricCalls: 100,
	}
	used := 90
	if !canSpendDSPYMetricBudget(runtime, &used, 10) {
		t.Error("expected spend to be allowed when used+estimated == max")
	}
}

func TestCanSpendDSPYMetricBudget_OverBudget(t *testing.T) {
	runtime := DSPYRuntimeSettings{
		Optimizer:      MutatorModeGEPA,
		MaxMetricCalls: 100,
	}
	used := 91
	if canSpendDSPYMetricBudget(runtime, &used, 10) {
		t.Error("expected spend to be disallowed when used+estimated > max")
	}
}

func TestCanSpendDSPYMetricBudget_ZeroEstimatedCostsTreatedAsOne(t *testing.T) {
	runtime := DSPYRuntimeSettings{
		Optimizer:      MutatorModeGEPA,
		MaxMetricCalls: 5,
	}
	used := 5
	// estimatedCalls=0 is treated as 1, so 5+1 > 5 => disallowed
	if canSpendDSPYMetricBudget(runtime, &used, 0) {
		t.Error("expected spend to be disallowed when at exact budget with zero estimate")
	}
}

// --- shouldRunDSPYPeriodicFullEval ---

func TestShouldRunDSPYPeriodicFullEval_ZeroTrialCount(t *testing.T) {
	if shouldRunDSPYPeriodicFullEval(0, 0, 5) {
		t.Error("expected false when trialCount=0")
	}
}

func TestShouldRunDSPYPeriodicFullEval_LastTrial(t *testing.T) {
	// trial == trialCount-1 always returns true
	if !shouldRunDSPYPeriodicFullEval(9, 10, 5) {
		t.Error("expected true for last trial")
	}
	if !shouldRunDSPYPeriodicFullEval(0, 1, 0) {
		t.Error("expected true for last trial (single trial)")
	}
}

func TestShouldRunDSPYPeriodicFullEval_IntervalBoundary(t *testing.T) {
	// fullEvalSteps=4 → interval=5 → full eval at trial 4, 9, 14 (index 0-based, so when (trial+1)%5==0)
	if !shouldRunDSPYPeriodicFullEval(4, 20, 4) {
		t.Error("expected full eval at trial 4 with steps=4")
	}
	if shouldRunDSPYPeriodicFullEval(3, 20, 4) {
		t.Error("expected no full eval at trial 3 with steps=4")
	}
	if !shouldRunDSPYPeriodicFullEval(9, 20, 4) {
		t.Error("expected full eval at trial 9 with steps=4")
	}
}

func TestShouldRunDSPYPeriodicFullEval_ZeroFullEvalSteps(t *testing.T) {
	// fullEvalSteps=0 → interval=max(1,1)=1 → every trial is a full eval
	for trial := 0; trial < 9; trial++ {
		if !shouldRunDSPYPeriodicFullEval(trial, 10, 0) {
			t.Errorf("expected full eval at every trial when steps=0, failed at trial %d", trial)
		}
	}
}

// --- parentScore ---

func TestParentScore_NilParent(t *testing.T) {
	if parentScore(nil) != 0 {
		t.Error("expected 0 for nil parent")
	}
}

func TestParentScore_NilFitness(t *testing.T) {
	org := &Organism{}
	if parentScore(org) != 0 {
		t.Error("expected 0 for nil fitness")
	}
}

func TestParentScore_WithFitness(t *testing.T) {
	org := &Organism{
		Fitness: &Fitness{AdjustedAccuracy: 0.85},
	}
	if parentScore(org) != 0.85 {
		t.Errorf("expected 0.85, got %f", parentScore(org))
	}
}

// --- copyIntMap ---

func TestCopyIntMap_Basic(t *testing.T) {
	in := map[string]int{"a": 1, "b": 2, "c": 3}
	out := copyIntMap(in)
	for k, v := range in {
		if out[k] != v {
			t.Errorf("copyIntMap[%q] = %d, want %d", k, out[k], v)
		}
	}
	if len(out) != len(in) {
		t.Errorf("copyIntMap length = %d, want %d", len(out), len(in))
	}
}

func TestCopyIntMap_Independence(t *testing.T) {
	in := map[string]int{"x": 10}
	out := copyIntMap(in)
	out["x"] = 99
	if in["x"] != 10 {
		t.Error("modifying copy should not affect original map")
	}
}

func TestCopyIntMap_Empty(t *testing.T) {
	out := copyIntMap(nil)
	if len(out) != 0 {
		t.Errorf("expected empty map for nil input, got len=%d", len(out))
	}
}

// --- filterStagedWithoutReplayFitness ---

func TestFilterStagedWithoutReplayFitness_FiltersNilAndWithFitness(t *testing.T) {
	org1 := &Organism{ID: "org-1"}
	org2 := &Organism{ID: "org-2"}
	org3 := &Organism{ID: "org-3"}

	fitness := &Fitness{AdjustedAccuracy: 0.8}
	staged := []*stagedOrganism{
		nil,                                      // nil → excluded
		{Organism: org1, ReplayFitness: fitness}, // has fitness → excluded
		{Organism: org2, ReplayFitness: nil},     // no fitness → included
		{Organism: org3, ReplayFitness: nil},     // no fitness → included
	}

	got := filterStagedWithoutReplayFitness(staged)
	if len(got) != 2 {
		t.Fatalf("expected 2 items without replay fitness, got %d", len(got))
	}
	if got[0].Organism.ID != "org-2" || got[1].Organism.ID != "org-3" {
		t.Errorf("unexpected organisms: %v", got)
	}
}

func TestFilterStagedWithoutReplayFitness_AllHaveFitness(t *testing.T) {
	fitness := &Fitness{AdjustedAccuracy: 0.9}
	staged := []*stagedOrganism{
		{Organism: &Organism{ID: "a"}, ReplayFitness: fitness},
		{Organism: &Organism{ID: "b"}, ReplayFitness: fitness},
	}
	got := filterStagedWithoutReplayFitness(staged)
	if len(got) != 0 {
		t.Errorf("expected 0 items when all have fitness, got %d", len(got))
	}
}

// --- pickTrialScore ---

func TestPickTrialScore_NilItem(t *testing.T) {
	if pickTrialScore(nil, nil) != 0 {
		t.Error("expected 0 for nil item")
	}
}

func TestPickTrialScore_NilOrganism(t *testing.T) {
	item := &stagedOrganism{Organism: nil}
	if pickTrialScore(item, nil) != 0 {
		t.Error("expected 0 for item with nil organism")
	}
}

func TestPickTrialScore_UsesReplayFitness(t *testing.T) {
	item := &stagedOrganism{
		Organism:      &Organism{ID: "org-1"},
		ReplayFitness: &Fitness{AdjustedAccuracy: 0.75},
	}
	batchResults := map[string]transientEvaluatedCandidate{
		"org-1": {Fitness: &Fitness{AdjustedAccuracy: 0.50}}, // lower than replay
	}
	score := pickTrialScore(item, batchResults)
	if score != 0.75 {
		t.Errorf("expected replay fitness score 0.75, got %f", score)
	}
}

func TestPickTrialScore_UsesBatchResultWhenNoReplay(t *testing.T) {
	item := &stagedOrganism{
		Organism:      &Organism{ID: "org-2"},
		ReplayFitness: nil,
	}
	batchResults := map[string]transientEvaluatedCandidate{
		"org-2": {Fitness: &Fitness{AdjustedAccuracy: 0.65}},
	}
	score := pickTrialScore(item, batchResults)
	if score != 0.65 {
		t.Errorf("expected batch result score 0.65, got %f", score)
	}
}

func TestPickTrialScore_ZeroWhenNotInBatchAndNoReplay(t *testing.T) {
	item := &stagedOrganism{
		Organism:      &Organism{ID: "org-3"},
		ReplayFitness: nil,
	}
	score := pickTrialScore(item, map[string]transientEvaluatedCandidate{})
	if score != 0 {
		t.Errorf("expected 0 when not in batch, got %f", score)
	}
}

// --- resolveDSPYCandidateParent ---

func TestResolveDSPYCandidateParent_NilOrganism(t *testing.T) {
	fallback := &Organism{ID: "fallback"}
	got := resolveDSPYCandidateParent(nil, nil, fallback)
	if got != fallback {
		t.Error("expected fallback for nil organism")
	}
}

func TestResolveDSPYCandidateParent_NoParentIDs(t *testing.T) {
	org := &Organism{ID: "child", ParentIDs: nil}
	fallback := &Organism{ID: "fallback"}
	got := resolveDSPYCandidateParent(nil, org, fallback)
	if got != fallback {
		t.Error("expected fallback when no parent IDs")
	}
}

func TestResolveDSPYCandidateParent_FoundInStates(t *testing.T) {
	parent := &Organism{ID: "parent-1"}
	states := []*dspyCandidateState{
		{organism: &Organism{ID: "other"}},
		{organism: parent},
	}
	child := &Organism{ID: "child", ParentIDs: []string{"parent-1"}}
	fallback := &Organism{ID: "fallback"}

	got := resolveDSPYCandidateParent(states, child, fallback)
	if got != parent {
		t.Errorf("expected parent-1, got %v", got)
	}
}

func TestResolveDSPYCandidateParent_NotFoundFallsBack(t *testing.T) {
	states := []*dspyCandidateState{
		{organism: &Organism{ID: "some-other"}},
	}
	child := &Organism{ID: "child", ParentIDs: []string{"missing-parent"}}
	fallback := &Organism{ID: "fallback"}

	got := resolveDSPYCandidateParent(states, child, fallback)
	if got != fallback {
		t.Error("expected fallback when parent ID not in states")
	}
}

func TestResolveDSPYCandidateParent_NilStatesEntries(t *testing.T) {
	parent := &Organism{ID: "parent-x"}
	states := []*dspyCandidateState{
		nil,
		{organism: nil},
		{organism: parent},
	}
	child := &Organism{ID: "child", ParentIDs: []string{"parent-x"}}

	got := resolveDSPYCandidateParent(states, child, nil)
	if got != parent {
		t.Error("expected to find parent despite nil entries in states")
	}
}

// --- accumulateDSPYMetricCalls ---

func TestAccumulateDSPYMetricCalls_NilPointer(t *testing.T) {
	// Should not panic
	accumulateDSPYMetricCalls(nil, &Fitness{}, 5)
}

func TestAccumulateDSPYMetricCalls_WithFitness(t *testing.T) {
	fitness := &Fitness{TotalItems: 50}
	used := 0
	accumulateDSPYMetricCalls(&used, fitness, 10)
	// metricCallsFromFitness uses TotalItems when available
	if used != 50 {
		t.Errorf("expected used=50 (from TotalItems), got %d", used)
	}
}

func TestAccumulateDSPYMetricCalls_NilFitnessFallback(t *testing.T) {
	used := 5
	accumulateDSPYMetricCalls(&used, nil, 7)
	// Fallback of 7 should be used
	if used != 12 {
		t.Errorf("expected used=12 with nil fitness and fallback=7, got %d", used)
	}
}

func TestAccumulateDSPYMetricCallsRaw(t *testing.T) {
	used := 10
	accumulateDSPYMetricCallsRaw(&used, 15)
	if used != 25 {
		t.Errorf("expected used=25, got %d", used)
	}
}

func TestAccumulateDSPYMetricCallsRaw_NilPointer(t *testing.T) {
	// Should not panic
	accumulateDSPYMetricCallsRaw(nil, 10)
}
