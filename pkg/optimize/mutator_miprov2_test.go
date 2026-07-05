package optimize

import (
	"math/rand"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// buildMIPRODemoSets
// ---------------------------------------------------------------------------

func TestBuildMIPRODemoSets_EmptyInputs(t *testing.T) {
	tests := []struct {
		name     string
		failures []FailureCase
		numSets  int
		perSet   int
		wantLen  int
	}{
		{"nil failures", nil, 3, 4, 0},
		{"zero numSets", []FailureCase{{ItemID: "a"}}, 0, 4, 0},
		{"zero perSet", []FailureCase{{ItemID: "a"}}, 3, 0, 0},
		{"all zero", nil, 0, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildMIPRODemoSets(tc.failures, tc.numSets, tc.perSet, nil)
			if len(got) != tc.wantLen {
				t.Errorf("buildMIPRODemoSets() len=%d, want %d", len(got), tc.wantLen)
			}
		})
	}
}

func TestBuildMIPRODemoSets_ProducesCorrectNumberOfSets(t *testing.T) {
	failures := make([]FailureCase, 20)
	for i := range failures {
		failures[i] = FailureCase{ItemID: string(rune('a' + i))}
	}
	rng := rand.New(rand.NewSource(42))

	sets := buildMIPRODemoSets(failures, 5, 4, rng)
	if len(sets) != 5 {
		t.Fatalf("expected 5 sets, got %d", len(sets))
	}
	for i, set := range sets {
		if len(set) != 4 {
			t.Errorf("set[%d] has %d items, want 4", i, len(set))
		}
	}
}

func TestBuildMIPRODemoSets_CapsSizeToAvailable(t *testing.T) {
	// Only 2 failures but perSet=10 — should cap at 2 per set.
	failures := []FailureCase{{ItemID: "x"}, {ItemID: "y"}}
	rng := rand.New(rand.NewSource(7))

	sets := buildMIPRODemoSets(failures, 3, 10, rng)
	if len(sets) != 3 {
		t.Fatalf("expected 3 sets, got %d", len(sets))
	}
	for i, set := range sets {
		if len(set) != 2 {
			t.Errorf("set[%d] has %d items, want 2 (capped)", i, len(set))
		}
	}
}

func TestBuildMIPRODemoSets_NilRngDoesNotPanic(t *testing.T) {
	failures := []FailureCase{{ItemID: "a"}, {ItemID: "b"}, {ItemID: "c"}}
	// Should not panic; uses its own RNG internally.
	sets := buildMIPRODemoSets(failures, 2, 2, nil)
	if len(sets) != 2 {
		t.Fatalf("expected 2 sets with nil rng, got %d", len(sets))
	}
}

// ---------------------------------------------------------------------------
// dedupePromptCandidates
// ---------------------------------------------------------------------------

func TestDedupePromptCandidates_EmptyInput(t *testing.T) {
	// Should always add the baseline (currentPrompt), resulting in 1 entry.
	got := dedupePromptCandidates(nil, "baseline prompt", 10)
	if len(got) != 1 {
		t.Fatalf("expected 1 (baseline only), got %d", len(got))
	}
	if got[0].RevisedPrompt != "baseline prompt" {
		t.Errorf("first entry should be baseline, got %q", got[0].RevisedPrompt)
	}
}

func TestDedupePromptCandidates_DedupesExactMatches(t *testing.T) {
	candidates := []ClaudePromptCandidate{
		{RevisedPrompt: "variant A"},
		{RevisedPrompt: "variant A"}, // duplicate
		{RevisedPrompt: "variant B"},
	}
	got := dedupePromptCandidates(candidates, "baseline", 100)
	// Should have: baseline + variant A + variant B = 3
	if len(got) != 3 {
		t.Errorf("expected 3 after dedup, got %d (entries: %v)", len(got), got)
	}
}

func TestDedupePromptCandidates_DedupesBaselineMatch(t *testing.T) {
	// Candidate that matches the current prompt should be deduped against the injected baseline.
	candidates := []ClaudePromptCandidate{
		{RevisedPrompt: "  baseline  "}, // whitespace around the exact match
		{RevisedPrompt: "variant"},
	}
	got := dedupePromptCandidates(candidates, "baseline", 100)
	// baseline (injected) + variant = 2 (the whitespace-variant is deduped after trim)
	if len(got) != 2 {
		t.Errorf("expected 2, got %d", len(got))
	}
}

func TestDedupePromptCandidates_RespectsLimit(t *testing.T) {
	candidates := []ClaudePromptCandidate{
		{RevisedPrompt: "a"},
		{RevisedPrompt: "b"},
		{RevisedPrompt: "c"},
		{RevisedPrompt: "d"},
	}
	// limit=3 → baseline + a + b = 3 (c and d are cut)
	got := dedupePromptCandidates(candidates, "baseline", 3)
	if len(got) != 3 {
		t.Errorf("expected 3 (limit), got %d", len(got))
	}
}

func TestDedupePromptCandidates_EmptyPromptSkipped(t *testing.T) {
	candidates := []ClaudePromptCandidate{
		{RevisedPrompt: ""},    // empty — should be skipped
		{RevisedPrompt: "   "}, // whitespace-only — should be skipped
		{RevisedPrompt: "valid"},
	}
	got := dedupePromptCandidates(candidates, "baseline", 100)
	// baseline + valid = 2
	if len(got) != 2 {
		t.Errorf("expected 2 (baseline + 1 valid), got %d", len(got))
	}
}

func TestDedupePromptCandidates_TrimsWhitespace(t *testing.T) {
	candidates := []ClaudePromptCandidate{
		{RevisedPrompt: "  padded  "},
	}
	got := dedupePromptCandidates(candidates, "baseline", 100)
	// Should have trimmed variant.
	for _, c := range got {
		if c.RevisedPrompt != strings.TrimSpace(c.RevisedPrompt) {
			t.Errorf("prompt not trimmed: %q", c.RevisedPrompt)
		}
	}
}

func TestDedupePromptCandidates_ZeroLimitNoTruncation(t *testing.T) {
	candidates := []ClaudePromptCandidate{
		{RevisedPrompt: "x"},
		{RevisedPrompt: "y"},
	}
	// limit=0 means no limit enforced.
	got := dedupePromptCandidates(candidates, "base", 0)
	if len(got) != 3 { // base + x + y
		t.Errorf("expected 3 with limit=0, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// chooseMIPROCandidateIndex
// ---------------------------------------------------------------------------

func TestChooseMIPROCandidateIndex_SingleCandidateAlwaysZero(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for trial := 0; trial < 20; trial++ {
		idx := chooseMIPROCandidateIndex(trial, 1, rng)
		if idx != 0 {
			t.Errorf("trial %d: expected 0 for total=1, got %d", trial, idx)
		}
	}
}

func TestChooseMIPROCandidateIndex_ZeroOrNegativeTotalReturnsZero(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	if got := chooseMIPROCandidateIndex(0, 0, rng); got != 0 {
		t.Errorf("total=0 expected 0, got %d", got)
	}
}

func TestChooseMIPROCandidateIndex_ReturnsInRange(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	total := 7
	seen := make(map[int]bool)
	for i := 0; i < 200; i++ {
		idx := chooseMIPROCandidateIndex(i, total, rng)
		if idx < 0 || idx >= total {
			t.Fatalf("index %d out of range [0, %d)", idx, total)
		}
		seen[idx] = true
	}
	// With 200 trials over 7 slots, all indices should appear.
	if len(seen) < total {
		t.Errorf("not all indices sampled: got %d distinct, want %d", len(seen), total)
	}
}

func TestChooseMIPROCandidateIndex_NilRngDoesNotPanic(t *testing.T) {
	// Should not panic; should allocate its own RNG.
	idx := chooseMIPROCandidateIndex(0, 5, nil)
	if idx < 0 || idx >= 5 {
		t.Errorf("index %d out of range with nil rng", idx)
	}
}

func TestChooseMIPROCandidateIndex_TrialParamIgnored(t *testing.T) {
	// The trial parameter is explicitly unused (per the source `_ = trial`).
	// Different trial values with the same rng state should produce same index.
	rng1 := rand.New(rand.NewSource(10))
	rng2 := rand.New(rand.NewSource(10))
	idx1 := chooseMIPROCandidateIndex(0, 5, rng1)
	idx2 := chooseMIPROCandidateIndex(99, 5, rng2)
	if idx1 != idx2 {
		t.Errorf("trial param should not affect result: idx1=%d idx2=%d", idx1, idx2)
	}
}

// ---------------------------------------------------------------------------
// buildMIPROv2CandidateProposalPrompt
// ---------------------------------------------------------------------------

func TestBuildMIPROv2CandidateProposalPrompt_ContainsRequiredSections(t *testing.T) {
	failures := []FailureCase{
		{
			ItemID:        "q1",
			CorrectAnswer: "B",
			Predicted:     "A",
			Category:      "reasoning_error",
			Question:      "What is 2+2?",
			Diagnosis:     "arithmetic miscalculation",
		},
	}
	demoSets := [][]FailureCase{
		{{ItemID: "demo1", CorrectAnswer: "C", Question: "Why is the sky blue?"}},
	}
	spec := &OptimizeSpec{
		Objectives: []Objective{
			{Metric: "accuracy", Direction: "maximize", Weight: 1.0},
			{Metric: "cost_per_item", Direction: "minimize", Weight: 0.5},
		},
	}

	prompt := buildMIPROv2CandidateProposalPrompt(
		"reasoning_component",
		"You are an analytical assistant.",
		failures,
		nil,
		spec,
		demoSets,
		3,
		nil,
	)

	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}

	expectedSections := []string{
		"MIPROv2",
		"reasoning_component",
		"You are an analytical assistant.",
		"Failure Minibatch",
		"q1",
		"Demo Candidate Sets",
		"Set 1",
		// Demo sets render "Q: <question> Gold: <answer>" — not ItemID; check the question text
		"Why is the sky blue?",
		"Instructions",
		"candidates",
		"revised_prompt",
	}
	for _, section := range expectedSections {
		if !strings.Contains(prompt, section) {
			t.Errorf("prompt missing expected section %q", section)
		}
	}
}

func TestBuildMIPROv2CandidateProposalPrompt_NumCandidatesInPrompt(t *testing.T) {
	spec := &OptimizeSpec{
		Objectives: []Objective{{Metric: "accuracy", Direction: "maximize", Weight: 1.0}},
	}
	prompt := buildMIPROv2CandidateProposalPrompt(
		"key", "current", nil, nil, spec, nil, 7, nil,
	)
	if !strings.Contains(prompt, "7") {
		t.Errorf("prompt should contain num_candidates=7, got: %s", prompt[:min(200, len(prompt))])
	}
}

func TestBuildMIPROv2CandidateProposalPrompt_NilSpecHandled(t *testing.T) {
	// Should not panic with nil spec.
	prompt := buildMIPROv2CandidateProposalPrompt(
		"key", "current", nil, nil, nil, nil, 3, nil,
	)
	if prompt == "" {
		t.Fatal("expected non-empty prompt even with nil spec")
	}
}

func TestBuildMIPROv2CandidateProposalPrompt_EmptyDemoSets(t *testing.T) {
	spec := &OptimizeSpec{
		Objectives: []Objective{{Metric: "accuracy", Direction: "maximize", Weight: 1.0}},
	}
	prompt := buildMIPROv2CandidateProposalPrompt(
		"key", "current", nil, nil, spec, nil, 3, nil,
	)
	if !strings.Contains(prompt, "none") {
		t.Errorf("expected 'none' for empty demo sets, prompt: %s", prompt[:min(300, len(prompt))])
	}
}

func TestBuildMIPROv2CandidateProposalPrompt_WithProposalContext(t *testing.T) {
	pc := &ProposalContext{
		DatasetSummary:      "MMLU chemistry subset",
		WorkflowDescription: "3-node reasoning pipeline",
		Tip:                 "Be precise.",
	}
	spec := &OptimizeSpec{
		Objectives: []Objective{{Metric: "accuracy", Direction: "maximize", Weight: 1.0}},
	}
	prompt := buildMIPROv2CandidateProposalPrompt(
		"key", "current", nil, nil, spec, nil, 2, pc,
	)
	for _, want := range []string{"MMLU chemistry subset", "3-node reasoning pipeline", "Be precise."} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing proposal context field %q", want)
		}
	}
}

func TestBuildMIPROv2CandidateProposalPrompt_ObjectiveWeightsPresent(t *testing.T) {
	spec := &OptimizeSpec{
		Objectives: []Objective{
			{Metric: "accuracy", Direction: "maximize", Weight: 0.85},
			{Metric: "cost_per_item", Direction: "minimize", Weight: 0.15},
		},
		Constraints: []Constraint{
			{Metric: "cost_per_item", Op: "<=", Value: 0.05},
		},
	}
	prompt := buildMIPROv2CandidateProposalPrompt(
		"key", "current", nil, nil, spec, nil, 2, nil,
	)
	if !strings.Contains(prompt, "Objectives") {
		t.Errorf("prompt missing Objectives section")
	}
	if !strings.Contains(prompt, "Constraints") {
		t.Errorf("prompt missing Constraints section")
	}
}

// ---------------------------------------------------------------------------
// buildMIPROv2MutationPrompt (extended coverage)
// ---------------------------------------------------------------------------

func TestBuildMIPROv2MutationPrompt_NilSpecDoesNotPanic(t *testing.T) {
	prompt := buildMIPROv2MutationPrompt("current prompt", nil, nil, nil, nil)
	if prompt == "" {
		t.Fatal("expected non-empty prompt with nil spec")
	}
}

func TestBuildMIPROv2MutationPrompt_LearningLogIncluded(t *testing.T) {
	// selectPromptLearningEntries used by buildMIPROv2MutationPrompt only includes
	// non-improvement entries (regressions, no_change) — filter matches that contract.
	learning := []LearningEntry{
		{Generation: 1, MutationType: "miprov2_prompt", Description: "tightened rubric", Outcome: "no_change", FitnessDelta: 0.0},
	}
	prompt := buildMIPROv2MutationPrompt("current", nil, learning, nil, nil)
	if !strings.Contains(prompt, "tightened rubric") {
		t.Errorf("prompt should include learning log entry description")
	}
}

func TestBuildMIPROv2MutationPrompt_ChildPredictedIncluded(t *testing.T) {
	failures := []FailureCase{
		{ItemID: "q1", CorrectAnswer: "A", Predicted: "B", ChildPredicted: "C"},
	}
	prompt := buildMIPROv2MutationPrompt("current", failures, nil, nil, nil)
	if !strings.Contains(prompt, "Child predicted") {
		t.Errorf("prompt should include child_predicted when set")
	}
}

func TestBuildMIPROv2MutationPrompt_NodeTracesIncluded(t *testing.T) {
	failures := []FailureCase{
		{
			ItemID:        "q1",
			CorrectAnswer: "A",
			Predicted:     "B",
			NodeTraces:    []FailureNodeTrace{{NodeID: "step-1", Model: "gpt-4o", Output: "intermediate output"}},
		},
	}
	spec := &OptimizeSpec{
		Objectives: []Objective{{Metric: "accuracy", Direction: "maximize", Weight: 1.0}},
	}
	prompt := buildMIPROv2MutationPrompt("current", failures, nil, spec, nil)
	if !strings.Contains(prompt, "step-1") {
		t.Errorf("prompt should include node trace node_id")
	}
}

func TestBuildMIPROv2MutationPrompt_AgentVotesSummary(t *testing.T) {
	failures := []FailureCase{
		{
			ItemID:        "q1",
			CorrectAnswer: "A",
			Predicted:     "B",
			AgentAnswers: []FailureAgentAnswer{
				{Model: "gpt-4", Answer: "A", ParseOK: true, Correct: true},
				{Model: "claude", Answer: "B", ParseOK: true, Correct: false},
			},
		},
	}
	prompt := buildMIPROv2MutationPrompt("current", failures, nil, nil, nil)
	if !strings.Contains(prompt, "Agent votes") {
		t.Errorf("prompt should include agent votes summary")
	}
}

func TestBuildMIPROv2MutationPrompt_FailureTypeSummary(t *testing.T) {
	failures := []FailureCase{
		{ItemID: "q1", Category: "reasoning_error"},
		{ItemID: "q2", Category: "reasoning_error"},
		{ItemID: "q3", Category: "parse_failure"},
	}
	prompt := buildMIPROv2MutationPrompt("current", failures, nil, nil, nil)
	if !strings.Contains(prompt, "Failure-type frequency") {
		t.Errorf("prompt should include failure-type frequency section")
	}
	if !strings.Contains(prompt, "reasoning_error") {
		t.Errorf("prompt should include the category name")
	}
}
