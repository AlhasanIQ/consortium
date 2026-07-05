package optimize

import "testing"

func TestNormalizeMutatorMode_EdgeCases(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"  ", MutatorModeAuto},
		{" llm ", MutatorModeLLM},
		{"COMBINATORIAL", MutatorModeCombinatorial},
		{"MiProV2", MutatorModeMIPROv2},
		{"GEPA", MutatorModeGEPA},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		got := NormalizeMutatorMode(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeMutatorMode(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsSupportedMutatorMode_Unsupported(t *testing.T) {
	unsupported := []string{"random", "bayesian", "grid_search"}
	for _, mode := range unsupported {
		if IsSupportedMutatorMode(mode) {
			t.Errorf("IsSupportedMutatorMode(%q) should be false", mode)
		}
	}
}

func TestResolveAutoMutatorMode_AllParamTypes(t *testing.T) {
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
			name: "float only yields combinatorial",
			spec: &OptimizeSpec{
				Params: []ParamDeclaration{
					{Path: "nodes[a].data.config.temp", Type: ParamTypeFloat},
				},
			},
			want: MutatorModeCombinatorial,
		},
		{
			name: "int only yields combinatorial",
			spec: &OptimizeSpec{
				Params: []ParamDeclaration{
					{Path: "nodes[a].data.config.top_k", Type: ParamTypeInt},
				},
			},
			want: MutatorModeCombinatorial,
		},
		{
			name: "enum only yields combinatorial",
			spec: &OptimizeSpec{
				Params: []ParamDeclaration{
					{Path: "nodes[a].data.config.mode", Type: ParamTypeEnum},
				},
			},
			want: MutatorModeCombinatorial,
		},
		{
			name: "prompt plus enum yields miprov2",
			spec: &OptimizeSpec{
				Params: []ParamDeclaration{
					{Path: "nodes[a].data.config.prompt", Type: ParamTypePrompt},
					{Path: "nodes[a].data.config.mode", Type: ParamTypeEnum},
				},
			},
			want: MutatorModeMIPROv2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveAutoMutatorMode(tt.spec)
			if got != tt.want {
				t.Errorf("ResolveAutoMutatorMode() = %q, want %q", got, tt.want)
			}
		})
	}
}
