package optimize

import (
	"strings"
	"testing"
)

func TestParamType_Valid(t *testing.T) {
	valid := []ParamType{ParamTypeModel, ParamTypePrompt, ParamTypeEnum, ParamTypeFloat, ParamTypeInt, ParamTypeTopology}
	for _, pt := range valid {
		if !pt.Valid() {
			t.Errorf("ParamType(%q).Valid() = false, want true", pt)
		}
	}
	invalid := []ParamType{"", "boolean", "array", "FLOAT"}
	for _, pt := range invalid {
		if pt.Valid() {
			t.Errorf("ParamType(%q).Valid() = true, want false", pt)
		}
	}
}

func TestParamDeclaration_Validate(t *testing.T) {
	tests := []struct {
		name    string
		param   ParamDeclaration
		wantErr string
	}{
		{
			name:    "empty path",
			param:   ParamDeclaration{Path: "", Type: ParamTypeModel},
			wantErr: "path is required",
		},
		{
			name:    "invalid path format",
			param:   ParamDeclaration{Path: "model", Type: ParamTypeModel},
			wantErr: "invalid path format",
		},
		{
			name:    "unsupported type",
			param:   ParamDeclaration{Path: "nodes[a].data.config.x", Type: "boolean"},
			wantErr: "unsupported type",
		},
		{
			name:    "model without candidates",
			param:   ParamDeclaration{Path: "nodes[a].data.config.model", Type: ParamTypeModel},
			wantErr: "candidates are required",
		},
		{
			name:    "model with empty candidate",
			param:   ParamDeclaration{Path: "nodes[a].data.config.model", Type: ParamTypeModel, Candidates: []string{"a", ""}},
			wantErr: "non-empty",
		},
		{
			name:    "model with duplicate candidates",
			param:   ParamDeclaration{Path: "nodes[a].data.config.model", Type: ParamTypeModel, Candidates: []string{"a", "a"}},
			wantErr: "duplicate candidate",
		},
		{
			name:    "float without range",
			param:   ParamDeclaration{Path: "nodes[a].data.config.temp", Type: ParamTypeFloat},
			wantErr: "range is required",
		},
		{
			name:    "int without range",
			param:   ParamDeclaration{Path: "nodes[a].data.config.count", Type: ParamTypeInt},
			wantErr: "range is required",
		},
		{
			name:  "valid model param",
			param: ParamDeclaration{Path: "nodes[a].data.config.model", Type: ParamTypeModel, Candidates: []string{"gpt-4", "claude-3"}},
		},
		{
			name:  "valid float param",
			param: ParamDeclaration{Path: "nodes[a].data.config.temp", Type: ParamTypeFloat, Range: &ParamRange{Min: 0, Max: 1}},
		},
		{
			name:  "valid prompt param",
			param: ParamDeclaration{Path: "nodes[a].data.config.prompt", Type: ParamTypePrompt},
		},
		{
			name:  "valid topology param",
			param: ParamDeclaration{Path: "nodes[a].data.config.layout", Type: ParamTypeTopology},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.param.Validate()
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

func TestParamRange_Validate(t *testing.T) {
	tests := []struct {
		name    string
		r       *ParamRange
		wantErr string
	}{
		{"nil range", nil, "range is required"},
		{"max < min", &ParamRange{Min: 10, Max: 5}, "must be >= min"},
		{"negative step", &ParamRange{Min: 0, Max: 1, Step: -0.1}, "must be >= 0"},
		{"valid range", &ParamRange{Min: 0, Max: 1, Step: 0.1}, ""},
		{"zero step (no stepping)", &ParamRange{Min: 0, Max: 1, Step: 0}, ""},
		{"equal min/max", &ParamRange{Min: 5, Max: 5}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.r.Validate()
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

func TestObjective_Validate(t *testing.T) {
	tests := []struct {
		name    string
		obj     Objective
		wantErr string
	}{
		{"empty metric", Objective{Metric: "", Direction: "maximize", Weight: 1}, "metric is required"},
		{"invalid direction", Objective{Metric: "accuracy", Direction: "up", Weight: 1}, "direction must be"},
		{"zero weight", Objective{Metric: "accuracy", Direction: "maximize", Weight: 0}, "weight must be non-zero"},
		{"valid maximize", Objective{Metric: "accuracy", Direction: "maximize", Weight: 1}, ""},
		{"valid minimize", Objective{Metric: "cost", Direction: "minimize", Weight: 0.5}, ""},
		{"negative weight allowed", Objective{Metric: "cost", Direction: "minimize", Weight: -1}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.obj.Validate()
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

func TestConstraint_Validate(t *testing.T) {
	tests := []struct {
		name    string
		c       Constraint
		wantErr string
	}{
		{"empty metric", Constraint{Op: "<="}, "metric is required"},
		{"invalid op", Constraint{Metric: "cost", Op: "=="}, "unsupported op"},
		{"valid <=", Constraint{Metric: "cost", Op: "<=", Value: 10}, ""},
		{"valid >=", Constraint{Metric: "accuracy", Op: ">=", Value: 0.5}, ""},
		{"valid <", Constraint{Metric: "latency", Op: "<", Value: 1000}, ""},
		{"valid >", Constraint{Metric: "accuracy", Op: ">", Value: 0.8}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.c.Validate()
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

func TestStopPolicy_Validate(t *testing.T) {
	tests := []struct {
		name    string
		sp      StopPolicy
		wantErr string
	}{
		{"zero generations", StopPolicy{MaxGenerations: 0, BudgetUSD: 1}, "max_generations must be >= 1"},
		{"zero budget", StopPolicy{MaxGenerations: 1, BudgetUSD: 0}, "budget_usd must be > 0"},
		{"negative plateau", StopPolicy{MaxGenerations: 1, BudgetUSD: 1, PlateauGenerations: -1}, "plateau_generations must be >= 0"},
		{"negative stability", StopPolicy{MaxGenerations: 1, BudgetUSD: 1, StabilityTopK: -1}, "stability_top_k must be >= 0"},
		{"valid", StopPolicy{MaxGenerations: 10, BudgetUSD: 5, PlateauGenerations: 3, StabilityTopK: 5}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.sp.Validate()
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

func TestPromotionPolicy_Validate(t *testing.T) {
	tests := []struct {
		name    string
		pp      PromotionPolicy
		wantErr string
	}{
		{"negative accuracy gain", PromotionPolicy{MinAccuracyGain: -0.1}, "min_accuracy_gain must be >= 0"},
		{"empty no_regression_on metric", PromotionPolicy{NoRegressionOn: []string{""}}, "must not include empty"},
		{"duplicate no_regression_on", PromotionPolicy{NoRegressionOn: []string{"accuracy", "accuracy"}}, "duplicate"},
		{"valid empty", PromotionPolicy{}, ""},
		{"valid with metrics", PromotionPolicy{MinAccuracyGain: 0.01, NoRegressionOn: []string{"accuracy", "parse_rate"}}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.pp.Validate()
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

func TestOptimizeSpec_Validate(t *testing.T) {
	validSpec := func() *OptimizeSpec {
		return &OptimizeSpec{
			Params: []ParamDeclaration{
				{Path: "nodes[a].data.config.model", Type: ParamTypeModel, Candidates: []string{"gpt-4", "claude-3"}},
			},
			Objectives: []Objective{{Metric: "accuracy", Direction: "maximize", Weight: 1}},
			StopPolicy: StopPolicy{MaxGenerations: 10, BudgetUSD: 5},
		}
	}

	t.Run("valid spec", func(t *testing.T) {
		if err := validSpec().Validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("nil spec", func(t *testing.T) {
		var s *OptimizeSpec
		if err := s.Validate(); err == nil {
			t.Error("expected error for nil spec")
		}
	})

	t.Run("no params", func(t *testing.T) {
		s := validSpec()
		s.Params = nil
		if err := s.Validate(); err == nil {
			t.Error("expected error for no params")
		}
	})

	t.Run("duplicate param paths", func(t *testing.T) {
		s := validSpec()
		s.Params = append(s.Params, s.Params[0])
		err := s.Validate()
		if err == nil {
			t.Fatal("expected error for duplicate param paths")
		}
		if !strings.Contains(err.Error(), "duplicate param path") {
			t.Errorf("error should mention 'duplicate param path', got: %v", err)
		}
	})

	t.Run("no objectives", func(t *testing.T) {
		s := validSpec()
		s.Objectives = nil
		if err := s.Validate(); err == nil {
			t.Error("expected error for no objectives")
		}
	})

	t.Run("invalid locked path format", func(t *testing.T) {
		s := validSpec()
		s.Locked = []string{"bad-path"}
		err := s.Validate()
		if err == nil {
			t.Fatal("expected error for invalid locked path")
		}
		if !strings.Contains(err.Error(), "invalid path format") {
			t.Errorf("error should mention 'invalid path format', got: %v", err)
		}
	})

	t.Run("duplicate locked paths", func(t *testing.T) {
		s := validSpec()
		lockedPath := "nodes[a].data.config.prompt"
		s.Locked = []string{lockedPath, lockedPath}
		err := s.Validate()
		if err == nil {
			t.Fatal("expected error for duplicate locked paths")
		}
		if !strings.Contains(err.Error(), "duplicate locked path") {
			t.Errorf("error should mention 'duplicate locked path', got: %v", err)
		}
	})
}

func TestDSPYConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *DSPYConfig
		wantErr string
	}{
		{"nil config", nil, ""},
		{"valid empty", &DSPYConfig{}, ""},
		{"valid optimizer miprov2", &DSPYConfig{Optimizer: "miprov2"}, ""},
		{"valid optimizer gepa", &DSPYConfig{Optimizer: "gepa"}, ""},
		{"invalid optimizer", &DSPYConfig{Optimizer: "bayesian"}, "optimizer must be"},
		{"valid auto light", &DSPYConfig{Auto: "light"}, ""},
		{"valid auto medium", &DSPYConfig{Auto: "medium"}, ""},
		{"valid auto heavy", &DSPYConfig{Auto: "heavy"}, ""},
		{"invalid auto", &DSPYConfig{Auto: "turbo"}, "auto must be one of"},
		{"negative num_candidates", &DSPYConfig{NumCandidates: -1}, "num_candidates must be >= 0"},
		{"negative num_trials", &DSPYConfig{NumTrials: -1}, "num_trials must be >= 0"},
		{"negative minibatch_size", &DSPYConfig{MinibatchSize: -1}, "minibatch_size must be >= 0"},
		{"negative minibatch_full_eval_steps", &DSPYConfig{MinibatchFullEvalSteps: -1}, "minibatch_full_eval_steps must be >= 0"},
		{"negative reflection_minibatch_size", &DSPYConfig{ReflectionMinibatchSize: -1}, "reflection_minibatch_size must be >= 0"},
		{"invalid candidate_selection", &DSPYConfig{CandidateSelection: "greedy"}, "candidate_selection_strategy must be"},
		{"valid candidate_selection pareto", &DSPYConfig{CandidateSelection: "pareto"}, ""},
		{"invalid component_selector", &DSPYConfig{ComponentSelector: "random"}, "component_selector must be"},
		{"valid component_selector", &DSPYConfig{ComponentSelector: "round_robin"}, ""},
		{"negative max_merge_invocations", &DSPYConfig{MaxMergeInvocations: -1}, "max_merge_invocations must be >= 0"},
		{"negative max_full_evals", &DSPYConfig{MaxFullEvals: -1}, "max_full_evals must be >= 0"},
		{"negative max_metric_calls", &DSPYConfig{MaxMetricCalls: -1}, "max_metric_calls must be >= 0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
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

func TestSortParamPaths(t *testing.T) {
	params := []ParamDeclaration{
		{Path: "nodes[c].data.config.z"},
		{Path: "nodes[a].data.config.model"},
		{Path: "nodes[b].data.config.temp"},
	}
	paths := SortParamPaths(params)
	if len(paths) != 3 {
		t.Fatalf("expected 3 paths, got %d", len(paths))
	}
	if paths[0] != "nodes[a].data.config.model" || paths[1] != "nodes[b].data.config.temp" || paths[2] != "nodes[c].data.config.z" {
		t.Errorf("unexpected sort order: %v", paths)
	}
}
