package optimize

import (
	"strings"
	"testing"
)

func TestValidateDSPYSpec_Extended(t *testing.T) {
	tests := []struct {
		name    string
		spec    *OptimizeSpec
		wantErr string
	}{
		{
			name:    "nil spec",
			spec:    nil,
			wantErr: "requires optimize spec",
		},
		{
			name:    "no params",
			spec:    &OptimizeSpec{},
			wantErr: "at least one optimize param",
		},
		{
			name: "no prompt params",
			spec: &OptimizeSpec{
				Params: []ParamDeclaration{
					{Path: "nodes[a].data.config.model", Type: ParamTypeModel, Candidates: []string{"x"}},
				},
			},
			wantErr: "at least one prompt param",
		},
		{
			name: "non-prompt params present",
			spec: &OptimizeSpec{
				Params: []ParamDeclaration{
					{Path: "nodes[a].data.config.prompt", Type: ParamTypePrompt},
					{Path: "nodes[a].data.config.model", Type: ParamTypeModel, Candidates: []string{"x"}},
				},
			},
			wantErr: "supports prompt params only",
		},
		{
			name: "valid prompt-only spec",
			spec: &OptimizeSpec{
				Params: []ParamDeclaration{
					{Path: "nodes[a].data.config.prompt", Type: ParamTypePrompt},
					{Path: "nodes[b].data.config.system_prompt", Type: ParamTypePrompt},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDSPYSpec(tc.spec)
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q should contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestResolveDSPYOptimizerMode_Extended(t *testing.T) {
	tests := []struct {
		name        string
		mutatorMode string
		spec        *OptimizeSpec
		want        string
		wantErr     bool
	}{
		{
			name:        "explicit miprov2",
			mutatorMode: "miprov2",
			want:        MutatorModeMIPROv2,
		},
		{
			name:        "explicit gepa",
			mutatorMode: "gepa",
			want:        MutatorModeGEPA,
		},
		{
			name:        "auto defaults to miprov2",
			mutatorMode: "auto",
			want:        MutatorModeMIPROv2,
		},
		{
			name:        "auto with spec dspy.optimizer=gepa",
			mutatorMode: "auto",
			spec: &OptimizeSpec{
				DSPY: &DSPYConfig{Optimizer: "gepa"},
			},
			want: MutatorModeGEPA,
		},
		{
			name:        "unsupported mode errors",
			mutatorMode: "combinatorial",
			wantErr:     true,
		},
		{
			name:        "llm mode errors",
			mutatorMode: "llm",
			wantErr:     true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveDSPYOptimizerMode(tc.mutatorMode, tc.spec)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("ResolveDSPYOptimizerMode() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestValidateRunConfiguration_NonDSPY(t *testing.T) {
	// For non-dspy strategies, validation should always pass.
	err := ValidateRunConfiguration("evolutionary", "combinatorial", nil)
	if err != nil {
		t.Errorf("expected no error for evolutionary strategy, got: %v", err)
	}
}

func TestValidateRunConfiguration_DSPY_ValidMIPRO(t *testing.T) {
	spec := &OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[a].data.config.prompt", Type: ParamTypePrompt},
		},
		DSPY: &DSPYConfig{
			NumCandidates: 5,
			NumTrials:     10,
		},
	}
	err := ValidateRunConfiguration("dspy", "miprov2", spec)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateRunConfiguration_DSPY_ValidGEPA(t *testing.T) {
	spec := &OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[a].data.config.prompt", Type: ParamTypePrompt},
		},
		DSPY: &DSPYConfig{
			Auto: "light",
		},
	}
	err := ValidateRunConfiguration("dspy", "gepa", spec)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateRunConfiguration_DSPY_GEPANoBudgetControl(t *testing.T) {
	spec := &OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[a].data.config.prompt", Type: ParamTypePrompt},
		},
		DSPY: &DSPYConfig{},
	}
	err := ValidateRunConfiguration("dspy", "gepa", spec)
	if err == nil {
		t.Fatal("expected error for GEPA without budget control")
	}
}

func TestValidateRunConfiguration_DSPY_MIPROAutoWithManualConflict(t *testing.T) {
	spec := &OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[a].data.config.prompt", Type: ParamTypePrompt},
		},
		DSPY: &DSPYConfig{
			Auto:          "light",
			NumCandidates: 5,
		},
	}
	err := ValidateRunConfiguration("dspy", "miprov2", spec)
	if err == nil {
		t.Fatal("expected error for MIPRO auto+manual conflict")
	}
}
