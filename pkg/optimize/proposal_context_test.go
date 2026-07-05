package optimize

import (
	"math/rand"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// randomProposalTip
// ---------------------------------------------------------------------------

func TestRandomProposalTip_NilRngReturnsEmpty(t *testing.T) {
	got := randomProposalTip(nil)
	if got != "" {
		t.Errorf("randomProposalTip(nil) = %q, want empty string", got)
	}
}

func TestRandomProposalTip_ReturnsFromDefinedSet(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 100; i++ {
		tip := randomProposalTip(rng)
		found := false
		for _, candidate := range proposalTips {
			if tip == candidate {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("randomProposalTip returned value not in proposalTips: %q", tip)
		}
	}
}

func TestRandomProposalTip_CanReturnEmptyTip(t *testing.T) {
	// The first entry in proposalTips is "" (no tip). Verify it can be sampled.
	rng := rand.New(rand.NewSource(0))
	sawEmpty := false
	for i := 0; i < 500; i++ {
		if randomProposalTip(rng) == "" {
			sawEmpty = true
			break
		}
	}
	if !sawEmpty {
		t.Error("randomProposalTip never returned empty tip in 500 attempts")
	}
}

// ---------------------------------------------------------------------------
// formatProposalContextSection
// ---------------------------------------------------------------------------

func TestFormatProposalContextSection_NilReturnsEmpty(t *testing.T) {
	got := formatProposalContextSection(nil)
	if got != "" {
		t.Errorf("formatProposalContextSection(nil) = %q, want empty", got)
	}
}

func TestFormatProposalContextSection_EmptyContextReturnsEmpty(t *testing.T) {
	pc := &ProposalContext{}
	got := formatProposalContextSection(pc)
	if got != "" {
		t.Errorf("formatProposalContextSection({}) = %q, want empty", got)
	}
}

func TestFormatProposalContextSection_DatasetSummary(t *testing.T) {
	pc := &ProposalContext{DatasetSummary: "MMLU science subset, 100 items"}
	got := formatProposalContextSection(pc)
	if !strings.Contains(got, "Dataset Summary") {
		t.Errorf("missing 'Dataset Summary' header")
	}
	if !strings.Contains(got, "MMLU science subset, 100 items") {
		t.Errorf("missing dataset summary text")
	}
}

func TestFormatProposalContextSection_WorkflowDescription(t *testing.T) {
	pc := &ProposalContext{WorkflowDescription: "3-node ensemble pipeline"}
	got := formatProposalContextSection(pc)
	if !strings.Contains(got, "Workflow Context") {
		t.Errorf("missing 'Workflow Context' header")
	}
	if !strings.Contains(got, "3-node ensemble pipeline") {
		t.Errorf("missing workflow description text")
	}
}

func TestFormatProposalContextSection_SuccessExamples(t *testing.T) {
	pc := &ProposalContext{
		SuccessExamples: []SuccessExample{
			{ItemID: "item-1", Question: "What is 2+2?", CorrectAnswer: "4", Predicted: "4"},
			{ItemID: "item-2", Question: "Capital of France?", CorrectAnswer: "Paris", Predicted: "Paris"},
		},
	}
	got := formatProposalContextSection(pc)
	if !strings.Contains(got, "Successful Examples") {
		t.Errorf("missing 'Successful Examples' header")
	}
	if !strings.Contains(got, "item-1") {
		t.Errorf("missing example item-1")
	}
	if !strings.Contains(got, "item-2") {
		t.Errorf("missing example item-2")
	}
	if !strings.Contains(got, `"4"`) {
		t.Errorf("missing answer for item-1")
	}
}

func TestFormatProposalContextSection_ImprovingEntries(t *testing.T) {
	pc := &ProposalContext{
		ImprovingEntries: []LearningEntry{
			{Generation: 2, MutationType: "gepa_prompt", FitnessDelta: 0.04, Description: "tightened rubric"},
		},
	}
	got := formatProposalContextSection(pc)
	if !strings.Contains(got, "Previous Improving Mutations") {
		t.Errorf("missing 'Previous Improving Mutations' header")
	}
	if !strings.Contains(got, "tightened rubric") {
		t.Errorf("missing improving entry description")
	}
	if !strings.Contains(got, "0.0400") {
		t.Errorf("missing fitness delta in output")
	}
}

func TestFormatProposalContextSection_Tip(t *testing.T) {
	pc := &ProposalContext{Tip: "Be concise and precise."}
	got := formatProposalContextSection(pc)
	if !strings.Contains(got, "Tip") {
		t.Errorf("missing 'Tip' header")
	}
	if !strings.Contains(got, "Be concise and precise.") {
		t.Errorf("missing tip text")
	}
}

func TestFormatProposalContextSection_AllFieldsPopulated(t *testing.T) {
	pc := &ProposalContext{
		DatasetSummary:      "MMLU chem",
		WorkflowDescription: "Captain synthesis",
		SuccessExamples:     []SuccessExample{{ItemID: "s1", CorrectAnswer: "X"}},
		ImprovingEntries:    []LearningEntry{{Generation: 1, MutationType: "p", FitnessDelta: 0.01, Description: "d"}},
		Tip:                 "A helpful tip",
	}
	got := formatProposalContextSection(pc)

	for _, want := range []string{
		"Dataset Summary", "MMLU chem",
		"Workflow Context", "Captain synthesis",
		"Successful Examples", "s1",
		"Previous Improving Mutations",
		"Tip", "A helpful tip",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing expected content %q", want)
		}
	}
}

func TestFormatProposalContextSection_QuestionTruncated(t *testing.T) {
	// Questions longer than 200 chars should be trimmed in success examples.
	longQ := strings.Repeat("a", 500)
	pc := &ProposalContext{
		SuccessExamples: []SuccessExample{
			{ItemID: "i1", Question: longQ, CorrectAnswer: "Z"},
		},
	}
	got := formatProposalContextSection(pc)
	// The full 500-char string should not appear verbatim.
	if strings.Contains(got, longQ) {
		t.Error("expected long question to be truncated, but full text appeared")
	}
}

// ---------------------------------------------------------------------------
// selectImprovingLearningEntries
// ---------------------------------------------------------------------------

func TestSelectImprovingLearningEntries_EmptyInput(t *testing.T) {
	got := selectImprovingLearningEntries(nil, 5)
	if got != nil {
		t.Errorf("expected nil for nil input, got %v", got)
	}
}

func TestSelectImprovingLearningEntries_ZeroLimit(t *testing.T) {
	learning := []LearningEntry{
		{Outcome: "improvement", FitnessDelta: 0.05, MutationType: "miprov2_prompt"},
	}
	got := selectImprovingLearningEntries(learning, 0)
	if got != nil {
		t.Errorf("expected nil for limit=0, got %v", got)
	}
}

func TestSelectImprovingLearningEntries_SelectsOnlyImprovements(t *testing.T) {
	learning := []LearningEntry{
		{Generation: 1, Outcome: "regression", FitnessDelta: -0.02, MutationType: "miprov2_prompt"},
		{Generation: 2, Outcome: "no_change", FitnessDelta: 0, MutationType: "gepa_prompt"},
		{Generation: 3, Outcome: "improvement", FitnessDelta: 0.05, MutationType: "miprov2_prompt"},
		{Generation: 4, Outcome: "improvement", FitnessDelta: 0.03, MutationType: "gepa_reflective_prompt"},
	}
	got := selectImprovingLearningEntries(learning, 10)
	if len(got) != 2 {
		t.Fatalf("expected 2 improving entries, got %d", len(got))
	}
	for _, e := range got {
		if e.Outcome != "improvement" || e.FitnessDelta <= 0 {
			t.Errorf("non-improving entry included: %+v", e)
		}
	}
}

func TestSelectImprovingLearningEntries_RequiresPromptMutationType(t *testing.T) {
	learning := []LearningEntry{
		// This is an improvement but NOT a prompt mutation type.
		{Generation: 1, Outcome: "improvement", FitnessDelta: 0.05, MutationType: "combinatorial"},
		// This is a prompt improvement.
		{Generation: 2, Outcome: "improvement", FitnessDelta: 0.03, MutationType: "miprov2_prompt"},
	}
	got := selectImprovingLearningEntries(learning, 10)
	// Only the prompt mutation should be selected.
	if len(got) != 1 {
		t.Fatalf("expected 1 entry (prompt-only), got %d", len(got))
	}
	if got[0].Generation != 2 {
		t.Errorf("wrong entry selected: %+v", got[0])
	}
}

func TestSelectImprovingLearningEntries_RespectsLimit(t *testing.T) {
	learning := []LearningEntry{
		{Generation: 1, Outcome: "improvement", FitnessDelta: 0.01, MutationType: "miprov2_prompt"},
		{Generation: 2, Outcome: "improvement", FitnessDelta: 0.02, MutationType: "gepa_prompt"},
		{Generation: 3, Outcome: "improvement", FitnessDelta: 0.03, MutationType: "miprov2_prompt"},
	}
	got := selectImprovingLearningEntries(learning, 2)
	if len(got) != 2 {
		t.Fatalf("expected 2 (limit), got %d", len(got))
	}
}

func TestSelectImprovingLearningEntries_TakesFromTail(t *testing.T) {
	// Should iterate from the end (most recent) first.
	learning := []LearningEntry{
		{Generation: 1, Outcome: "improvement", FitnessDelta: 0.01, MutationType: "gepa_prompt", Description: "old"},
		{Generation: 5, Outcome: "improvement", FitnessDelta: 0.08, MutationType: "miprov2_prompt", Description: "recent"},
	}
	got := selectImprovingLearningEntries(learning, 1)
	if len(got) != 1 {
		t.Fatalf("expected 1, got %d", len(got))
	}
	if got[0].Description != "recent" {
		t.Errorf("expected most-recent entry first, got generation=%d desc=%q", got[0].Generation, got[0].Description)
	}
}

// ---------------------------------------------------------------------------
// buildWorkflowDescription
// ---------------------------------------------------------------------------

func TestBuildWorkflowDescription_EmptyJSON(t *testing.T) {
	got := buildWorkflowDescription(nil, "key")
	if got != "" {
		t.Errorf("expected empty for nil workflow, got %q", got)
	}
}

func TestBuildWorkflowDescription_InvalidJSON(t *testing.T) {
	got := buildWorkflowDescription([]byte("not-json"), "key")
	if got != "" {
		t.Errorf("expected empty for invalid JSON, got %q", got)
	}
}

func TestBuildWorkflowDescription_WorkflowIDAndName(t *testing.T) {
	wf := []byte(`{"id": "my-workflow", "name": "My Workflow", "nodes": []}`)
	got := buildWorkflowDescription(wf, "")
	if !strings.Contains(got, "my-workflow") {
		t.Errorf("missing workflow id, got: %q", got)
	}
	if !strings.Contains(got, "My Workflow") {
		t.Errorf("missing workflow name, got: %q", got)
	}
}

func TestBuildWorkflowDescription_NodesListed(t *testing.T) {
	wf := []byte(`{
		"id": "wf1",
		"nodes": [
			{"id": "agent-a", "data": {"type": "llm_call", "label": "Reasoner", "config": {"model": "gpt-4"}}},
			{"id": "agent-b", "data": {"type": "llm_call", "label": "Scorer", "config": {}}}
		]
	}`)
	got := buildWorkflowDescription(wf, "")
	if !strings.Contains(got, "agent-a") {
		t.Errorf("missing node agent-a, got: %q", got)
	}
	if !strings.Contains(got, "agent-b") {
		t.Errorf("missing node agent-b, got: %q", got)
	}
	if !strings.Contains(got, "gpt-4") {
		t.Errorf("missing model field, got: %q", got)
	}
}

func TestBuildWorkflowDescription_TargetNodeMarked(t *testing.T) {
	wf := []byte(`{
		"id": "wf1",
		"nodes": [
			{"id": "agent-a", "data": {"type": "llm_call", "config": {}}},
			{"id": "agent-b", "data": {"type": "llm_call", "config": {}}}
		]
	}`)
	// componentKey contains "agent-a" so that node should be marked with "*".
	got := buildWorkflowDescription(wf, "nodes[agent-a].data.config.prompt")
	if !strings.Contains(got, "* ") {
		t.Errorf("expected target node to be marked with '*', got: %q", got)
	}
	if !strings.Contains(got, "agent-a") {
		t.Errorf("target node missing from output")
	}
}

func TestBuildWorkflowDescription_NodeCountPresent(t *testing.T) {
	wf := []byte(`{
		"id": "wf1",
		"nodes": [
			{"id": "n1", "data": {"config": {}}},
			{"id": "n2", "data": {"config": {}}},
			{"id": "n3", "data": {"config": {}}}
		]
	}`)
	got := buildWorkflowDescription(wf, "")
	if !strings.Contains(got, "3 total") {
		t.Errorf("missing node count, got: %q", got)
	}
}

// ---------------------------------------------------------------------------
// buildDatasetSummary
// ---------------------------------------------------------------------------

func TestBuildDatasetSummary_EmptyInputs(t *testing.T) {
	got := buildDatasetSummary(nil, nil)
	if got != "" {
		t.Errorf("expected empty for nil inputs, got %q", got)
	}
}

func TestBuildDatasetSummary_CountsFailuresAndSuccesses(t *testing.T) {
	failures := []FailureCase{{ItemID: "f1"}, {ItemID: "f2"}}
	successes := []SuccessExample{{ItemID: "s1"}}
	got := buildDatasetSummary(failures, successes)
	if !strings.Contains(got, "3 items") {
		t.Errorf("expected total count of 3, got: %q", got)
	}
	if !strings.Contains(got, "2 failures") {
		t.Errorf("expected 2 failures, got: %q", got)
	}
	if !strings.Contains(got, "1 successes") {
		t.Errorf("expected 1 successes, got: %q", got)
	}
}

func TestBuildDatasetSummary_CategoryDistribution(t *testing.T) {
	failures := []FailureCase{
		{ItemID: "f1", Category: "reasoning_error"},
		{ItemID: "f2", Category: "reasoning_error"},
		{ItemID: "f3", Category: "parse_failure"},
	}
	got := buildDatasetSummary(failures, nil)
	if !strings.Contains(got, "reasoning_error") {
		t.Errorf("missing category reasoning_error, got: %q", got)
	}
	if !strings.Contains(got, "parse_failure") {
		t.Errorf("missing category parse_failure, got: %q", got)
	}
}

func TestBuildDatasetSummary_EmptyCategoryFallsBack(t *testing.T) {
	failures := []FailureCase{{ItemID: "f1", Category: ""}}
	got := buildDatasetSummary(failures, nil)
	if !strings.Contains(got, "uncategorized") {
		t.Errorf("expected 'uncategorized' for empty category, got: %q", got)
	}
}

func TestBuildDatasetSummary_QuestionLengthComplexity(t *testing.T) {
	// Short questions (< 200 chars)
	shortFailures := []FailureCase{{ItemID: "f1", Question: "Short?"}}
	got := buildDatasetSummary(shortFailures, nil)
	if !strings.Contains(got, "short") {
		t.Errorf("expected 'short' complexity label, got: %q", got)
	}

	// Medium questions (200-500 chars)
	mediumQ := strings.Repeat("a", 300)
	mediumFailures := []FailureCase{{ItemID: "f1", Question: mediumQ}}
	got = buildDatasetSummary(mediumFailures, nil)
	if !strings.Contains(got, "medium-length") {
		t.Errorf("expected 'medium-length' complexity label, got: %q", got)
	}

	// Long questions (> 500 chars)
	longQ := strings.Repeat("b", 600)
	longFailures := []FailureCase{{ItemID: "f1", Question: longQ}}
	got = buildDatasetSummary(longFailures, nil)
	if !strings.Contains(got, "long") {
		t.Errorf("expected 'long' complexity label, got: %q", got)
	}
}

func TestBuildDatasetSummary_SuccessQuestionsCountedForLength(t *testing.T) {
	// Success examples also contribute to average length calculation.
	successes := []SuccessExample{{ItemID: "s1", Question: strings.Repeat("x", 600)}}
	got := buildDatasetSummary(nil, successes)
	if !strings.Contains(got, "long") {
		t.Errorf("expected 'long' complexity from success examples, got: %q", got)
	}
}

func TestBuildDatasetSummary_NoQuestionsNoLengthLine(t *testing.T) {
	// Failures without Question fields — no avg length line should appear.
	failures := []FailureCase{{ItemID: "f1", Category: "error"}}
	got := buildDatasetSummary(failures, nil)
	if strings.Contains(got, "avg ~") {
		t.Errorf("unexpected avg length line when no questions, got: %q", got)
	}
}
