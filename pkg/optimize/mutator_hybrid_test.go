package optimize

import (
	"testing"
)

// --- requestHasPromptParams ---

func TestRequestHasPromptParams(t *testing.T) {
	tests := []struct {
		name string
		req  *MutationRequest
		want bool
	}{
		{"nil request", nil, false},
		{"nil spec", &MutationRequest{}, false},
		{
			"no prompt params",
			&MutationRequest{Spec: &OptimizeSpec{Params: []ParamDeclaration{
				{Type: ParamTypeModel},
				{Type: ParamTypeFloat},
			}}},
			false,
		},
		{
			"has prompt param",
			&MutationRequest{Spec: &OptimizeSpec{Params: []ParamDeclaration{
				{Type: ParamTypeModel},
				{Type: ParamTypePrompt},
			}}},
			true,
		},
		{
			"only prompt param",
			&MutationRequest{Spec: &OptimizeSpec{Params: []ParamDeclaration{
				{Type: ParamTypePrompt},
			}}},
			true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := requestHasPromptParams(tc.req)
			if got != tc.want {
				t.Errorf("requestHasPromptParams() = %v, want %v", got, tc.want)
			}
		})
	}
}

// --- requestHasCombinatorialParams ---

func TestRequestHasCombinatorialParams(t *testing.T) {
	tests := []struct {
		name string
		req  *MutationRequest
		want bool
	}{
		{"nil request", nil, false},
		{"nil spec", &MutationRequest{}, false},
		{
			"prompt-only spec",
			&MutationRequest{Spec: &OptimizeSpec{Params: []ParamDeclaration{
				{Type: ParamTypePrompt},
			}}},
			false,
		},
		{
			"has model param",
			&MutationRequest{Spec: &OptimizeSpec{Params: []ParamDeclaration{
				{Type: ParamTypeModel},
			}}},
			true,
		},
		{
			"has float param",
			&MutationRequest{Spec: &OptimizeSpec{Params: []ParamDeclaration{
				{Type: ParamTypeFloat},
			}}},
			true,
		},
		{
			"has int param",
			&MutationRequest{Spec: &OptimizeSpec{Params: []ParamDeclaration{
				{Type: ParamTypeInt},
			}}},
			true,
		},
		{
			"has enum param",
			&MutationRequest{Spec: &OptimizeSpec{Params: []ParamDeclaration{
				{Type: ParamTypeEnum},
			}}},
			true,
		},
		{
			"mixed prompt and model",
			&MutationRequest{Spec: &OptimizeSpec{Params: []ParamDeclaration{
				{Type: ParamTypePrompt},
				{Type: ParamTypeModel},
			}}},
			true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := requestHasCombinatorialParams(tc.req)
			if got != tc.want {
				t.Errorf("requestHasCombinatorialParams() = %v, want %v", got, tc.want)
			}
		})
	}
}

// --- firstAvailableAlternateMutator ---

func TestFirstAvailableAlternateMutator(t *testing.T) {
	stubCombinatorial := &CombinatorialMutator{}
	stubLLM := &LLMMutator{}

	tests := []struct {
		name             string
		usePrompt        bool
		hasCombinatorial bool
		hasPrompt        bool
		combinatorial    Mutator
		llm              Mutator
		wantNil          bool
		wantType         string
	}{
		{
			name:             "usePrompt=true hasCombinatorial=true → returns combinatorial",
			usePrompt:        true,
			hasCombinatorial: true,
			hasPrompt:        false,
			combinatorial:    stubCombinatorial,
			llm:              stubLLM,
			wantType:         "combinatorial",
		},
		{
			name:             "usePrompt=true hasCombinatorial=false → nil",
			usePrompt:        true,
			hasCombinatorial: false,
			hasPrompt:        true,
			combinatorial:    stubCombinatorial,
			llm:              stubLLM,
			wantNil:          true,
		},
		{
			name:             "usePrompt=false hasPrompt=true → returns llm",
			usePrompt:        false,
			hasCombinatorial: false,
			hasPrompt:        true,
			combinatorial:    stubCombinatorial,
			llm:              stubLLM,
			wantType:         "llm",
		},
		{
			name:             "usePrompt=false hasPrompt=false → nil",
			usePrompt:        false,
			hasCombinatorial: true,
			hasPrompt:        false,
			combinatorial:    stubCombinatorial,
			llm:              stubLLM,
			wantNil:          true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := firstAvailableAlternateMutator(tc.usePrompt, tc.hasCombinatorial, tc.hasPrompt, tc.combinatorial, tc.llm)
			if tc.wantNil {
				if result != nil {
					t.Errorf("expected nil mutator, got %T", result)
				}
				return
			}
			if result == nil {
				t.Fatal("expected non-nil mutator")
			}
			switch tc.wantType {
			case "combinatorial":
				if result != tc.combinatorial {
					t.Errorf("expected combinatorial mutator, got %T", result)
				}
			case "llm":
				if result != tc.llm {
					t.Errorf("expected LLM mutator, got %T", result)
				}
			}
		})
	}
}

// --- mutationTypeFamily ---

func TestMutationTypeFamily(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"combinatorial", "combinatorial"},
		{"COMBINATORIAL", "combinatorial"},
		{"auto:combinatorial", "combinatorial"},
		{"llm_prompt", "prompt"},
		{"PROMPT", "prompt"},
		{"auto:llm_prompt", "prompt"},
		{"gepa_prompt", "prompt"},
		{"unknown", ""},
		{"", ""},
		{"   ", ""},
		{"topology", ""},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := mutationTypeFamily(tc.input)
			if got != tc.want {
				t.Errorf("mutationTypeFamily(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// --- outcomeWeights ---

func TestOutcomeWeights(t *testing.T) {
	tests := []struct {
		outcome    string
		wantWins   float64
		wantLosses float64
	}{
		{"improvement", 1.0, 0.0},
		{"IMPROVEMENT", 1.0, 0.0},
		{"no_change", 0.15, 0.35},
		{"NO_CHANGE", 0.15, 0.35},
		{"regression", 0.0, 1.0},
		{"constraint_violation", 0.0, 1.0},
		{"verify_error", 0.0, 1.0},
		{"unknown_outcome", 0.0, 0.5},
		{"", 0.0, 0.5},
		{"  regression  ", 0.0, 1.0},
	}
	for _, tc := range tests {
		t.Run(tc.outcome, func(t *testing.T) {
			wins, losses := outcomeWeights(tc.outcome)
			if wins != tc.wantWins {
				t.Errorf("outcomeWeights(%q).wins = %v, want %v", tc.outcome, wins, tc.wantWins)
			}
			if losses != tc.wantLosses {
				t.Errorf("outcomeWeights(%q).losses = %v, want %v", tc.outcome, losses, tc.wantLosses)
			}
		})
	}
}

// --- adaptivePromptProbability ---

func TestAdaptivePromptProbability_EmptyLearning(t *testing.T) {
	base := 0.6
	result := adaptivePromptProbability(base, nil)
	if result != base {
		t.Errorf("expected base %v with no learning, got %v", base, result)
	}
}

func TestAdaptivePromptProbability_ClampedBase(t *testing.T) {
	// Negative base should be treated as 0.5 per the implementation.
	result := adaptivePromptProbability(-0.1, nil)
	if result != 0.5 {
		t.Errorf("expected 0.5 for negative base, got %v", result)
	}

	// Base > 1 should be clamped to 1.0.
	result = adaptivePromptProbability(1.5, nil)
	if result != 1.0 {
		t.Errorf("expected 1.0 for base > 1, got %v", result)
	}
}

func TestAdaptivePromptProbability_LearnsFromHistory(t *testing.T) {
	// Many prompt improvements → probability should shift upward.
	learning := make([]LearningEntry, 30)
	for i := range learning {
		learning[i] = LearningEntry{MutationType: "llm_prompt", Outcome: "improvement"}
	}
	base := 0.5
	result := adaptivePromptProbability(base, learning)
	if result <= base {
		t.Errorf("expected probability > base=%v after prompt improvements, got %v", base, result)
	}
}

func TestAdaptivePromptProbability_CombinatorialWinsReducePromptProb(t *testing.T) {
	// Many combinatorial improvements → probability should shift downward.
	learning := make([]LearningEntry, 30)
	for i := range learning {
		learning[i] = LearningEntry{MutationType: "combinatorial", Outcome: "improvement"}
	}
	base := 0.5
	result := adaptivePromptProbability(base, learning)
	if result >= base {
		t.Errorf("expected probability < base=%v after combinatorial improvements, got %v", base, result)
	}
}

func TestAdaptivePromptProbability_ResultBoundsRespected(t *testing.T) {
	// Result must always stay in [0.1, 0.9].
	extremeLearning := make([]LearningEntry, 60)
	for i := range extremeLearning {
		extremeLearning[i] = LearningEntry{MutationType: "llm_prompt", Outcome: "improvement"}
	}
	result := adaptivePromptProbability(0.5, extremeLearning)
	if result < 0.1 || result > 0.9 {
		t.Errorf("result %v outside [0.1, 0.9]", result)
	}

	for i := range extremeLearning {
		extremeLearning[i] = LearningEntry{MutationType: "combinatorial", Outcome: "improvement"}
	}
	result = adaptivePromptProbability(0.5, extremeLearning)
	if result < 0.1 || result > 0.9 {
		t.Errorf("result %v outside [0.1, 0.9]", result)
	}
}

func TestAdaptivePromptProbability_UnknownOutcomesIgnored(t *testing.T) {
	// Entries with unknown mutation type should not shift probability.
	learning := []LearningEntry{
		{MutationType: "topology", Outcome: "improvement"},
		{MutationType: "topology", Outcome: "improvement"},
	}
	base := 0.65
	result := adaptivePromptProbability(base, learning)
	// With no seen relevant entries, returns base unchanged.
	if result != base {
		t.Errorf("expected base %v with no relevant entries, got %v", base, result)
	}
}
