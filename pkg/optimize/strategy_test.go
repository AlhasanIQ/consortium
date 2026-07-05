package optimize

import "testing"

func TestNormalizeOptimizeStrategy_Table(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "evolutionary"},
		{"evolutionary", "evolutionary"},
		{"Evolutionary", "evolutionary"},
		{"  EVOLUTIONARY  ", "evolutionary"},
		{"darwinian", "evolutionary"},
		{"Darwinian", "evolutionary"},
		{"dspy", "dspy"},
		{"DSPY", "dspy"},
		{"  dspy  ", "dspy"},
		{"random_walk", "random_walk"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := NormalizeOptimizeStrategy(tc.input)
			if got != tc.want {
				t.Errorf("NormalizeOptimizeStrategy(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestIsSupportedOptimizeStrategy_Table(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"evolutionary", true},
		{"darwinian", true},
		{"", true},
		{"dspy", true},
		{"DSPY", true},
		{"random_walk", false},
		{"bayesian", false},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := IsSupportedOptimizeStrategy(tc.input)
			if got != tc.want {
				t.Errorf("IsSupportedOptimizeStrategy(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeMutatorMode_Table(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "auto"},
		{"auto", "auto"},
		{"AUTO", "auto"},
		{"combinatorial", "combinatorial"},
		{"COMBINATORIAL", "combinatorial"},
		{"llm", "llm"},
		{"miprov2", "miprov2"},
		{"gepa", "gepa"},
		{"unknown", "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := NormalizeMutatorMode(tc.input)
			if got != tc.want {
				t.Errorf("NormalizeMutatorMode(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestIsSupportedMutatorMode_Table(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"auto", true},
		{"", true},
		{"combinatorial", true},
		{"llm", true},
		{"miprov2", true},
		{"gepa", true},
		{"unknown", false},
		{"bayesian", false},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := IsSupportedMutatorMode(tc.input)
			if got != tc.want {
				t.Errorf("IsSupportedMutatorMode(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestResolveAutoMutatorMode_Extended(t *testing.T) {
	tests := []struct {
		name string
		spec *OptimizeSpec
		want string
	}{
		{
			name: "nil spec",
			spec: nil,
			want: MutatorModeAuto,
		},
		{
			name: "empty params",
			spec: &OptimizeSpec{},
			want: MutatorModeAuto,
		},
		{
			name: "prompt only resolves to gepa",
			spec: &OptimizeSpec{
				Params: []ParamDeclaration{
					{Path: "nodes[a].data.config.prompt", Type: ParamTypePrompt},
				},
			},
			want: MutatorModeGEPA,
		},
		{
			name: "combinatorial only",
			spec: &OptimizeSpec{
				Params: []ParamDeclaration{
					{Path: "nodes[a].data.config.model", Type: ParamTypeModel, Candidates: []string{"a", "b"}},
				},
			},
			want: MutatorModeCombinatorial,
		},
		{
			name: "mixed prompt and combinatorial resolves to miprov2",
			spec: &OptimizeSpec{
				Params: []ParamDeclaration{
					{Path: "nodes[a].data.config.prompt", Type: ParamTypePrompt},
					{Path: "nodes[a].data.config.temperature", Type: ParamTypeFloat, Range: &ParamRange{Min: 0, Max: 1}},
				},
			},
			want: MutatorModeMIPROv2,
		},
		{
			name: "multiple combinatorial types stays combinatorial",
			spec: &OptimizeSpec{
				Params: []ParamDeclaration{
					{Path: "nodes[a].data.config.model", Type: ParamTypeModel, Candidates: []string{"a"}},
					{Path: "nodes[a].data.config.count", Type: ParamTypeInt, Range: &ParamRange{Min: 1, Max: 10}},
					{Path: "nodes[a].data.config.mode", Type: ParamTypeEnum, Candidates: []string{"fast", "slow"}},
				},
			},
			want: MutatorModeCombinatorial,
		},
		{
			name: "multiple prompts with no combinatorial resolves to gepa",
			spec: &OptimizeSpec{
				Params: []ParamDeclaration{
					{Path: "nodes[a].data.config.system_prompt", Type: ParamTypePrompt},
					{Path: "nodes[a].data.config.user_prompt", Type: ParamTypePrompt},
				},
			},
			want: MutatorModeGEPA,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveAutoMutatorMode(tc.spec)
			if got != tc.want {
				t.Errorf("ResolveAutoMutatorMode() = %q, want %q", got, tc.want)
			}
		})
	}
}
