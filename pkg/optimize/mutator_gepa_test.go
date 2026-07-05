package optimize

import (
	"context"
	"math/rand"
	"testing"
)

func TestGEPAPrepareMutationValidation(t *testing.T) {
	tests := []struct {
		name    string
		req     *MutationRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "nil parent",
			req:     &MutationRequest{Spec: &OptimizeSpec{}},
			wantErr: true,
		},
		{
			name:    "nil spec",
			req:     &MutationRequest{Parent: &Organism{}},
			wantErr: true,
		},
		{
			name: "valid request with count 0 defaults to 1",
			req: &MutationRequest{
				Parent: &Organism{ParamValues: map[string]string{}},
				Spec:   &OptimizeSpec{Params: []ParamDeclaration{{Path: "a", Type: ParamTypePrompt}}},
				Count:  0,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, rng, err := prepareMutation(tt.req, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("prepareMutation() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				if count < 1 {
					t.Errorf("count = %d, want >= 1", count)
				}
				if rng == nil {
					t.Error("rng should not be nil when no error")
				}
			}
		})
	}
}

func TestGEPAPrepareMutationWithExistingRng(t *testing.T) {
	rng := rand.New(rand.NewSource(42)) //nolint:gosec
	req := &MutationRequest{
		Parent: &Organism{ParamValues: map[string]string{}},
		Spec:   &OptimizeSpec{Params: []ParamDeclaration{{Path: "a", Type: ParamTypePrompt}}},
		Count:  3,
	}
	count, gotRng, err := prepareMutation(req, rng)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
	if gotRng != rng {
		t.Error("should reuse provided rng")
	}
}

func TestGEPASelectComponents(t *testing.T) {
	groups := []promptParamGroup{
		{Key: "system_prompt", Declarations: []ParamDeclaration{{Path: "a", Type: ParamTypePrompt}}},
		{Key: "user_prompt", Declarations: []ParamDeclaration{{Path: "b", Type: ParamTypePrompt}}},
		{Key: "rubric", Declarations: []ParamDeclaration{{Path: "c", Type: ParamTypePrompt}}},
	}

	tests := []struct {
		name       string
		selector   string
		generation int
		wantCount  int
		wantIndex  int // only checked when wantCount == 1
	}{
		{
			name:       "all selector returns all groups",
			selector:   "all",
			generation: 1,
			wantCount:  3,
		},
		{
			name:       "round_robin generation 1 selects index 0",
			selector:   "round_robin",
			generation: 1,
			wantCount:  1,
			wantIndex:  0,
		},
		{
			name:       "round_robin generation 2 selects index 1",
			selector:   "round_robin",
			generation: 2,
			wantCount:  1,
			wantIndex:  1,
		},
		{
			name:       "round_robin generation 4 wraps to index 0",
			selector:   "round_robin",
			generation: 4,
			wantCount:  1,
			wantIndex:  0,
		},
		{
			name:       "default selector is round_robin",
			selector:   "",
			generation: 3,
			wantCount:  1,
			wantIndex:  2,
		},
		{
			name:       "empty groups returns nil",
			selector:   "all",
			generation: 1,
			wantCount:  0,
		},
		{
			name:       "generation 0 selects index 0",
			selector:   "round_robin",
			generation: 0,
			wantCount:  1,
			wantIndex:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := groups
			if tt.name == "empty groups returns nil" {
				input = nil
			}
			indices := selectGEPAComponents(input, tt.selector, tt.generation, nil, nil)
			if len(indices) != tt.wantCount {
				t.Fatalf("selectGEPAComponents() returned %d indices, want %d", len(indices), tt.wantCount)
			}
			if tt.wantCount == 1 && indices[0] != tt.wantIndex {
				t.Errorf("selectGEPAComponents() index = %d, want %d", indices[0], tt.wantIndex)
			}
		})
	}
}

func TestGEPAChooseCandidateIndex(t *testing.T) {
	rng := rand.New(rand.NewSource(0)) //nolint:gosec

	candidates := make([]ClaudePromptCandidate, 5)
	for i := range candidates {
		candidates[i] = ClaudePromptCandidate{RevisedPrompt: "prompt"}
	}

	tests := []struct {
		name       string
		strategy   string
		trial      int
		numCands   int
		wantIndex  int
		checkRange bool // if true, just check in valid range instead of exact
	}{
		{
			name:      "single candidate always returns 0",
			strategy:  "pareto",
			trial:     5,
			numCands:  1,
			wantIndex: 0,
		},
		{
			name:      "current_best returns 1 for multi-candidate",
			strategy:  "current_best",
			trial:     0,
			numCands:  5,
			wantIndex: 1,
		},
		{
			name:       "pareto cycles through non-baseline candidates",
			strategy:   "pareto",
			trial:      0,
			numCands:   5,
			checkRange: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cands := candidates[:tt.numCands]
			idx := chooseGEPACandidateIndex(tt.strategy, tt.trial, cands, rng)
			if tt.checkRange {
				if idx < 0 || idx >= tt.numCands {
					t.Errorf("index %d out of range [0, %d)", idx, tt.numCands)
				}
			} else if idx != tt.wantIndex {
				t.Errorf("chooseGEPACandidateIndex() = %d, want %d", idx, tt.wantIndex)
			}
		})
	}
}

func TestGEPAMutatorNilRequest(t *testing.T) {
	m := &GEPAMutator{Model: "test-model"}
	_, err := m.Mutate(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil request")
	}
}

func TestGEPAMutatorNoPromptParams(t *testing.T) {
	m := &GEPAMutator{Model: "test-model"}
	req := &MutationRequest{
		Parent: &Organism{ParamValues: map[string]string{"a": `"val"`}},
		Spec: &OptimizeSpec{
			Params: []ParamDeclaration{
				{Path: "a", Type: ParamTypeModel, Candidates: []string{"m1", "m2"}},
			},
			Objectives: []Objective{{Metric: "accuracy", Direction: "maximize", Weight: 1.0}},
		},
		Count: 1,
	}
	_, err := m.Mutate(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for no prompt params")
	}
}

func TestGEPABuildMutationPrompt(t *testing.T) {
	failures := []FailureCase{
		{
			ItemID:        "q1",
			CorrectAnswer: "A",
			Predicted:     "B",
			Category:      "wrong_answer",
			Question:      "What is 1+1?",
		},
	}
	spec := &OptimizeSpec{
		Objectives: []Objective{
			{Metric: "accuracy", Direction: "maximize", Weight: 0.8},
			{Metric: "cost_per_item", Direction: "minimize", Weight: 0.2},
		},
	}

	prompt := buildGEPAMutationPrompt("current prompt text", failures, nil, spec, nil)
	if prompt == "" {
		t.Fatal("buildGEPAMutationPrompt returned empty string")
	}

	// Verify key sections are present
	for _, section := range []string{
		"GEPA",
		"Current Prompt",
		"current prompt text",
		"Reflection Batch",
		"q1",
		"Objectives",
		"Instructions",
		"revised_prompt",
	} {
		if !containsSubstring(prompt, section) {
			t.Errorf("prompt missing expected section %q", section)
		}
	}
}

func TestGEPABuildCandidateProposalPrompt(t *testing.T) {
	failures := []FailureCase{
		{ItemID: "q1", CorrectAnswer: "A", Predicted: "B", Category: "reasoning_error"},
	}
	settings := DSPYRuntimeSettings{
		CandidateSelectionStrategy: "pareto",
		ComponentSelector:          "round_robin",
	}
	spec := &OptimizeSpec{
		Objectives: []Objective{
			{Metric: "accuracy", Direction: "maximize", Weight: 1.0},
		},
	}

	prompt := buildGEPACandidateProposalPrompt(
		"system_prompt",
		"You are a helpful assistant.",
		failures,
		nil,
		spec,
		settings,
		3,
		nil,
	)

	if prompt == "" {
		t.Fatal("buildGEPACandidateProposalPrompt returned empty string")
	}

	for _, section := range []string{
		"GEPA",
		"Component key: system_prompt",
		"You are a helpful assistant.",
		"Reflective Dataset",
		"q1",
		"candidates",
	} {
		if !containsSubstring(prompt, section) {
			t.Errorf("prompt missing expected section %q", section)
		}
	}
}

// containsSubstring is a simple helper for test assertions.
func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
