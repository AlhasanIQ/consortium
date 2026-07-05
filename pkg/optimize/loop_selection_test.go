package optimize

import (
	"testing"
	"time"
)

// --- filterEligibleParents ---

func TestFilterEligibleParents(t *testing.T) {
	now := time.Now()
	eligible := &Organism{
		ID:          "eligible",
		Fitness:     &Fitness{CompositeScore: 0.8},
		EvaluatedAt: &now,
		BenchRunID:  "run-1",
	}
	notEvaluated := &Organism{
		ID:      "not-evaluated",
		Fitness: &Fitness{CompositeScore: 0.8},
		// EvaluatedAt is nil
		BenchRunID: "run-1",
	}
	noBenchRun := &Organism{
		ID:          "no-bench",
		Fitness:     &Fitness{CompositeScore: 0.8},
		EvaluatedAt: &now,
		BenchRunID:  "",
	}
	noFitness := &Organism{
		ID:          "no-fitness",
		EvaluatedAt: &now,
		BenchRunID:  "run-1",
	}

	tests := []struct {
		name       string
		population []*Organism
		wantIDs    []string
	}{
		{"nil population", nil, nil},
		{"empty population", []*Organism{}, nil},
		{"all eligible", []*Organism{eligible}, []string{"eligible"}},
		{"not evaluated excluded", []*Organism{eligible, notEvaluated}, []string{"eligible"}},
		{"no bench run excluded", []*Organism{eligible, noBenchRun}, []string{"eligible"}},
		{"no fitness excluded", []*Organism{eligible, noFitness}, []string{"eligible"}},
		{
			"nil organism excluded",
			[]*Organism{nil, eligible},
			[]string{"eligible"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := filterEligibleParents(tc.population)
			if len(result) != len(tc.wantIDs) {
				t.Fatalf("got %d results, want %d", len(result), len(tc.wantIDs))
			}
			for i, id := range tc.wantIDs {
				if result[i].ID != id {
					t.Errorf("result[%d].ID = %q, want %q", i, result[i].ID, id)
				}
			}
		})
	}
}

// --- filterFeasibleParents ---

func TestFilterFeasibleParents(t *testing.T) {
	feasible := &Organism{ID: "feasible", Fitness: &Fitness{Feasible: true}}
	infeasible := &Organism{ID: "infeasible", Fitness: &Fitness{Feasible: false}}
	noFitness := &Organism{ID: "no-fitness"}

	tests := []struct {
		name       string
		population []*Organism
		wantCount  int
	}{
		{"nil population", nil, 0},
		{"only feasible", []*Organism{feasible}, 1},
		{"only infeasible", []*Organism{infeasible}, 0},
		{"mixed", []*Organism{feasible, infeasible, noFitness}, 1},
		{"nil organism skipped", []*Organism{nil, feasible}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := filterFeasibleParents(tc.population)
			if len(result) != tc.wantCount {
				t.Errorf("got %d, want %d", len(result), tc.wantCount)
			}
		})
	}
}

// --- filterParentsWithFailures ---

func TestFilterParentsWithFailures(t *testing.T) {
	withFailures := &Organism{ID: "failures", Fitness: &Fitness{FailedItems: 3}}
	noFailures := &Organism{ID: "clean", Fitness: &Fitness{FailedItems: 0}}
	noFitness := &Organism{ID: "no-fitness"}

	tests := []struct {
		name       string
		population []*Organism
		wantCount  int
		wantIDs    []string
	}{
		{"nil population", nil, 0, nil},
		{"has failures", []*Organism{withFailures}, 1, []string{"failures"}},
		{"no failures", []*Organism{noFailures}, 0, nil},
		{"mixed", []*Organism{withFailures, noFailures, noFitness}, 1, []string{"failures"}},
		{"nil organism skipped", []*Organism{nil, withFailures}, 1, []string{"failures"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := filterParentsWithFailures(tc.population)
			if len(result) != tc.wantCount {
				t.Fatalf("got %d results, want %d", len(result), tc.wantCount)
			}
			for i, id := range tc.wantIDs {
				if result[i].ID != id {
					t.Errorf("result[%d].ID = %q, want %q", i, result[i].ID, id)
				}
			}
		})
	}
}

// --- hasPromptParams ---

func TestHasPromptParams(t *testing.T) {
	tests := []struct {
		name string
		spec *OptimizeSpec
		want bool
	}{
		{"nil spec", nil, false},
		{
			"no params",
			&OptimizeSpec{Params: nil},
			false,
		},
		{
			"model params only",
			&OptimizeSpec{Params: []ParamDeclaration{{Type: ParamTypeModel}}},
			false,
		},
		{
			"has prompt param",
			&OptimizeSpec{Params: []ParamDeclaration{{Type: ParamTypePrompt}}},
			true,
		},
		{
			"mixed: model + prompt",
			&OptimizeSpec{Params: []ParamDeclaration{
				{Type: ParamTypeModel},
				{Type: ParamTypePrompt},
			}},
			true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := hasPromptParams(tc.spec)
			if got != tc.want {
				t.Errorf("hasPromptParams() = %v, want %v", got, tc.want)
			}
		})
	}
}

// --- isEligibleParent ---

func TestIsEligibleParent(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name     string
		organism *Organism
		want     bool
	}{
		{"nil organism", nil, false},
		{"nil fitness", &Organism{ID: "a", EvaluatedAt: &now, BenchRunID: "r"}, false},
		{
			"nil evaluated at",
			&Organism{ID: "a", Fitness: &Fitness{}, BenchRunID: "r"},
			false,
		},
		{
			"empty bench run id",
			&Organism{ID: "a", Fitness: &Fitness{}, EvaluatedAt: &now, BenchRunID: ""},
			false,
		},
		{
			"whitespace bench run id",
			&Organism{ID: "a", Fitness: &Fitness{}, EvaluatedAt: &now, BenchRunID: "   "},
			false,
		},
		{
			"fully eligible",
			&Organism{ID: "a", Fitness: &Fitness{}, EvaluatedAt: &now, BenchRunID: "run-1"},
			true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isEligibleParent(tc.organism)
			if got != tc.want {
				t.Errorf("isEligibleParent() = %v, want %v", got, tc.want)
			}
		})
	}
}

// --- isFitnessBetter ---

func TestIsFitnessBetter(t *testing.T) {
	tests := []struct {
		name      string
		candidate *Fitness
		incumbent *Fitness
		want      bool
	}{
		{"nil candidate is never better", nil, &Fitness{CompositeScore: 0.5}, false},
		{"nil incumbent means candidate is better", &Fitness{CompositeScore: 0.5}, nil, true},
		{
			"feasible beats infeasible",
			&Fitness{Feasible: true, CompositeScore: 0.0},
			&Fitness{Feasible: false, CompositeScore: 1.0},
			true,
		},
		{
			"infeasible loses to feasible",
			&Fitness{Feasible: false, CompositeScore: 1.0},
			&Fitness{Feasible: true, CompositeScore: 0.0},
			false,
		},
		{
			"higher composite score wins",
			&Fitness{Feasible: true, CompositeScore: 0.8},
			&Fitness{Feasible: true, CompositeScore: 0.5},
			true,
		},
		{
			"lower composite score loses",
			&Fitness{Feasible: true, CompositeScore: 0.3},
			&Fitness{Feasible: true, CompositeScore: 0.5},
			false,
		},
		{
			"equal composite — higher adjusted accuracy wins",
			&Fitness{Feasible: true, CompositeScore: 0.5, AdjustedAccuracy: 0.9},
			&Fitness{Feasible: true, CompositeScore: 0.5, AdjustedAccuracy: 0.7},
			true,
		},
		{
			"equal composite and accuracy — lower cost wins",
			&Fitness{Feasible: true, CompositeScore: 0.5, AdjustedAccuracy: 0.8, CostPerItem: 0.01},
			&Fitness{Feasible: true, CompositeScore: 0.5, AdjustedAccuracy: 0.8, CostPerItem: 0.05},
			true,
		},
		{
			"completely equal — not better",
			&Fitness{Feasible: true, CompositeScore: 0.5, AdjustedAccuracy: 0.8, CostPerItem: 0.01},
			&Fitness{Feasible: true, CompositeScore: 0.5, AdjustedAccuracy: 0.8, CostPerItem: 0.01},
			false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isFitnessBetter(tc.candidate, tc.incumbent)
			if got != tc.want {
				t.Errorf("isFitnessBetter() = %v, want %v", got, tc.want)
			}
		})
	}
}

// --- isOrganismBetter ---

func TestIsOrganismBetter(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-time.Hour)
	later := now.Add(time.Hour)

	t.Run("nil candidate is not better", func(t *testing.T) {
		incumbent := &Organism{ID: "b", Fitness: &Fitness{CompositeScore: 0.5}}
		if isOrganismBetter(nil, incumbent) {
			t.Error("nil candidate should not be better")
		}
	})

	t.Run("candidate with nil fitness is not better", func(t *testing.T) {
		candidate := &Organism{ID: "a"}
		incumbent := &Organism{ID: "b", Fitness: &Fitness{CompositeScore: 0.5}}
		if isOrganismBetter(candidate, incumbent) {
			t.Error("candidate with nil fitness should not be better")
		}
	})

	t.Run("better fitness wins", func(t *testing.T) {
		candidate := &Organism{ID: "a", Fitness: &Fitness{CompositeScore: 0.9}, CreatedAt: later}
		incumbent := &Organism{ID: "b", Fitness: &Fitness{CompositeScore: 0.5}, CreatedAt: earlier}
		if !isOrganismBetter(candidate, incumbent) {
			t.Error("higher scoring candidate should be better")
		}
	})

	t.Run("equal fitness ties broken by earlier creation time", func(t *testing.T) {
		candidate := &Organism{
			ID:        "a",
			Fitness:   &Fitness{CompositeScore: 0.5, AdjustedAccuracy: 0.8, CostPerItem: 0.01},
			CreatedAt: earlier,
		}
		incumbent := &Organism{
			ID:        "b",
			Fitness:   &Fitness{CompositeScore: 0.5, AdjustedAccuracy: 0.8, CostPerItem: 0.01},
			CreatedAt: later,
		}
		if !isOrganismBetter(candidate, incumbent) {
			t.Error("earlier candidate should win tie by creation time")
		}
	})

	t.Run("later creation time loses tie", func(t *testing.T) {
		candidate := &Organism{
			ID:        "a",
			Fitness:   &Fitness{CompositeScore: 0.5, AdjustedAccuracy: 0.8, CostPerItem: 0.01},
			CreatedAt: later,
		}
		incumbent := &Organism{
			ID:        "b",
			Fitness:   &Fitness{CompositeScore: 0.5, AdjustedAccuracy: 0.8, CostPerItem: 0.01},
			CreatedAt: earlier,
		}
		if isOrganismBetter(candidate, incumbent) {
			t.Error("later candidate should lose tie by creation time")
		}
	})
}

// --- childrenPerParent adaptive edge cases not covered by loop_test.go ---

func TestChildrenPerParent_AdaptiveFanout(t *testing.T) {
	l := &Loop{}

	t.Run("adaptive fanout triggers when plateau reached", func(t *testing.T) {
		run := &OptimizationRun{
			ChildrenPerParent: 1,
			AdaptiveFanout:    true,
			TotalBudgetUSD:    100.0,
			SpentUSD:          50.0, // 50% remaining > 30% threshold
			Spec: &OptimizeSpec{
				StopPolicy: StopPolicy{PlateauGenerations: 5},
			},
		}
		got := l.childrenPerParent(run, 5) // withoutImprovement == PlateauGenerations
		if got != 2 {
			t.Errorf("expected adaptive fanout to increase children to 2, got %d", got)
		}
	})

	t.Run("adaptive fanout does not trigger when budget almost exhausted", func(t *testing.T) {
		run := &OptimizationRun{
			ChildrenPerParent: 1,
			AdaptiveFanout:    true,
			TotalBudgetUSD:    100.0,
			SpentUSD:          75.0, // only 25% remaining < 30% threshold
			Spec: &OptimizeSpec{
				StopPolicy: StopPolicy{PlateauGenerations: 5},
			},
		}
		got := l.childrenPerParent(run, 5)
		if got != 1 {
			t.Errorf("expected no adaptive fanout (budget too low), got %d", got)
		}
	})

	t.Run("adaptive fanout requires plateau to be reached", func(t *testing.T) {
		run := &OptimizationRun{
			ChildrenPerParent: 1,
			AdaptiveFanout:    true,
			TotalBudgetUSD:    100.0,
			SpentUSD:          50.0,
			Spec: &OptimizeSpec{
				StopPolicy: StopPolicy{PlateauGenerations: 5},
			},
		}
		got := l.childrenPerParent(run, 3) // withoutImprovement < PlateauGenerations
		if got != 1 {
			t.Errorf("expected no fanout before plateau, got %d", got)
		}
	})
}
