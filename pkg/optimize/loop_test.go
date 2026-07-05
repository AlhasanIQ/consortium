package optimize

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// isFitnessBetter
// ---------------------------------------------------------------------------

func TestIsFitnessBetter_Comprehensive(t *testing.T) {
	tests := []struct {
		name      string
		candidate *Fitness
		incumbent *Fitness
		want      bool
	}{
		{
			name:      "nil candidate is never better",
			candidate: nil,
			incumbent: &Fitness{CompositeScore: 0.5},
			want:      false,
		},
		{
			name:      "any candidate beats nil incumbent",
			candidate: &Fitness{CompositeScore: 0.1},
			incumbent: nil,
			want:      true,
		},
		{
			name:      "both nil",
			candidate: nil,
			incumbent: nil,
			want:      false,
		},
		{
			name:      "feasible beats infeasible",
			candidate: &Fitness{Feasible: true, CompositeScore: 0.1},
			incumbent: &Fitness{Feasible: false, CompositeScore: 0.9},
			want:      true,
		},
		{
			name:      "infeasible loses to feasible",
			candidate: &Fitness{Feasible: false, CompositeScore: 0.9},
			incumbent: &Fitness{Feasible: true, CompositeScore: 0.1},
			want:      false,
		},
		{
			name:      "higher composite score wins",
			candidate: &Fitness{Feasible: true, CompositeScore: 0.8},
			incumbent: &Fitness{Feasible: true, CompositeScore: 0.5},
			want:      true,
		},
		{
			name:      "lower composite score loses",
			candidate: &Fitness{Feasible: true, CompositeScore: 0.3},
			incumbent: &Fitness{Feasible: true, CompositeScore: 0.5},
			want:      false,
		},
		{
			name:      "equal composite, higher accuracy wins",
			candidate: &Fitness{Feasible: true, CompositeScore: 0.5, AdjustedAccuracy: 0.9},
			incumbent: &Fitness{Feasible: true, CompositeScore: 0.5, AdjustedAccuracy: 0.7},
			want:      true,
		},
		{
			name:      "equal composite and accuracy, lower cost wins",
			candidate: &Fitness{Feasible: true, CompositeScore: 0.5, AdjustedAccuracy: 0.7, CostPerItem: 0.01},
			incumbent: &Fitness{Feasible: true, CompositeScore: 0.5, AdjustedAccuracy: 0.7, CostPerItem: 0.05},
			want:      true,
		},
		{
			name:      "all equal returns false",
			candidate: &Fitness{Feasible: true, CompositeScore: 0.5, AdjustedAccuracy: 0.7, CostPerItem: 0.03},
			incumbent: &Fitness{Feasible: true, CompositeScore: 0.5, AdjustedAccuracy: 0.7, CostPerItem: 0.03},
			want:      false,
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

// ---------------------------------------------------------------------------
// CheckConstraints
// ---------------------------------------------------------------------------

func TestCheckConstraints_Comprehensive(t *testing.T) {
	tests := []struct {
		name           string
		fitness        *Fitness
		constraints    []Constraint
		wantFeasible   bool
		wantViolations int
	}{
		{
			name:         "no constraints is feasible",
			fitness:      &Fitness{Accuracy: 0.5},
			constraints:  nil,
			wantFeasible: true,
		},
		{
			name:         "nil fitness with no constraints",
			fitness:      nil,
			constraints:  nil,
			wantFeasible: true,
		},
		{
			name:           "nil fitness with constraints",
			fitness:        nil,
			constraints:    []Constraint{{Metric: "accuracy", Op: ">=", Value: 0.5}},
			wantFeasible:   false,
			wantViolations: 1,
		},
		{
			name:         "satisfied <= constraint",
			fitness:      &Fitness{CostPerItem: 0.03},
			constraints:  []Constraint{{Metric: "cost_per_item", Op: "<=", Value: 0.05}},
			wantFeasible: true,
		},
		{
			name:           "violated <= constraint",
			fitness:        &Fitness{CostPerItem: 0.10},
			constraints:    []Constraint{{Metric: "cost_per_item", Op: "<=", Value: 0.05}},
			wantFeasible:   false,
			wantViolations: 1,
		},
		{
			name:         "satisfied >= constraint",
			fitness:      &Fitness{AdjustedAccuracy: 0.90},
			constraints:  []Constraint{{Metric: "accuracy", Op: ">=", Value: 0.80}},
			wantFeasible: true,
		},
		{
			name:           "violated >= constraint",
			fitness:        &Fitness{AdjustedAccuracy: 0.60},
			constraints:    []Constraint{{Metric: "accuracy", Op: ">=", Value: 0.80}},
			wantFeasible:   false,
			wantViolations: 1,
		},
		{
			name:    "multiple constraints mixed",
			fitness: &Fitness{AdjustedAccuracy: 0.90, CostPerItem: 0.10},
			constraints: []Constraint{
				{Metric: "accuracy", Op: ">=", Value: 0.80},
				{Metric: "cost_per_item", Op: "<=", Value: 0.05},
			},
			wantFeasible:   false,
			wantViolations: 1,
		},
		{
			name:           "unsupported op",
			fitness:        &Fitness{},
			constraints:    []Constraint{{Metric: "accuracy", Op: "==", Value: 0.5}},
			wantFeasible:   false,
			wantViolations: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			feasible, violations := CheckConstraints(tc.fitness, tc.constraints)
			if feasible != tc.wantFeasible {
				t.Errorf("feasible = %v, want %v", feasible, tc.wantFeasible)
			}
			if len(violations) != tc.wantViolations {
				t.Errorf("violations count = %d, want %d (violations: %v)", len(violations), tc.wantViolations, violations)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ComputeCompositeScore
// ---------------------------------------------------------------------------

func TestComputeCompositeScore_Extended(t *testing.T) {
	tests := []struct {
		name       string
		fitness    *Fitness
		objectives []Objective
		want       float64
	}{
		{
			name:       "nil fitness",
			fitness:    nil,
			objectives: []Objective{{Metric: "accuracy", Direction: "maximize", Weight: 1}},
			want:       0,
		},
		{
			name:       "maximize accuracy",
			fitness:    &Fitness{AdjustedAccuracy: 0.90},
			objectives: []Objective{{Metric: "accuracy", Direction: "maximize", Weight: 1}},
			want:       0.90,
		},
		{
			name:       "minimize cost inverts",
			fitness:    &Fitness{CostPerItem: 0.05},
			objectives: []Objective{{Metric: "cost_per_item", Direction: "minimize", Weight: 1}},
			want:       20.0, // 1/0.05
		},
		{
			name:       "minimize zero cost gives zero",
			fitness:    &Fitness{CostPerItem: 0},
			objectives: []Objective{{Metric: "cost_per_item", Direction: "minimize", Weight: 1}},
			want:       0,
		},
		{
			name:    "weighted multi-objective",
			fitness: &Fitness{AdjustedAccuracy: 0.80, CostPerItem: 0.04},
			objectives: []Objective{
				{Metric: "accuracy", Direction: "maximize", Weight: 3},
				{Metric: "cost_per_item", Direction: "minimize", Weight: 1},
			},
			want: 0.80*3.0 + (1.0/0.04)*1.0, // 2.4 + 25.0 = 27.4
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ComputeCompositeScore(tc.fitness, tc.objectives)
			if math.Abs(got-tc.want) > 1e-6 {
				t.Errorf("ComputeCompositeScore() = %v, want %v", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// childrenPerParent
// ---------------------------------------------------------------------------

func TestChildrenPerParent(t *testing.T) {
	tests := []struct {
		name               string
		run                *OptimizationRun
		withoutImprovement int
		want               int
	}{
		{
			name: "nil run",
			run:  nil,
			want: 1,
		},
		{
			name: "default when ChildrenPerParent=0",
			run:  &OptimizationRun{},
			want: 1,
		},
		{
			name: "explicit children count",
			run:  &OptimizationRun{ChildrenPerParent: 3},
			want: 3,
		},
		{
			name: "capped by MaxChildrenPerGeneration",
			run:  &OptimizationRun{ChildrenPerParent: 5, MaxChildrenPerGeneration: 2},
			want: 2,
		},
		{
			name: "adaptive fanout doubles at plateau",
			run: &OptimizationRun{
				ChildrenPerParent: 1,
				AdaptiveFanout:    true,
				TotalBudgetUSD:    10.0,
				SpentUSD:          2.0, // 80% remaining > 30%
				Spec: &OptimizeSpec{
					StopPolicy: StopPolicy{PlateauGenerations: 3},
				},
			},
			withoutImprovement: 3, // equals plateau threshold
			want:               2,
		},
		{
			name: "adaptive fanout not triggered when budget low",
			run: &OptimizationRun{
				ChildrenPerParent: 1,
				AdaptiveFanout:    true,
				TotalBudgetUSD:    10.0,
				SpentUSD:          8.0, // 20% remaining < 30%
				Spec: &OptimizeSpec{
					StopPolicy: StopPolicy{PlateauGenerations: 3},
				},
			},
			withoutImprovement: 3,
			want:               1,
		},
		{
			name: "adaptive fanout not triggered when not at plateau",
			run: &OptimizationRun{
				ChildrenPerParent: 1,
				AdaptiveFanout:    true,
				TotalBudgetUSD:    10.0,
				SpentUSD:          2.0,
				Spec: &OptimizeSpec{
					StopPolicy: StopPolicy{PlateauGenerations: 5},
				},
			},
			withoutImprovement: 2, // below plateau threshold
			want:               1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := &Loop{}
			got := l.childrenPerParent(tc.run, tc.withoutImprovement)
			if got != tc.want {
				t.Errorf("childrenPerParent() = %d, want %d", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// OptimizationRun helpers
// ---------------------------------------------------------------------------

func TestOptimizationRun_IsTerminal(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{"completed", true},
		{"failed", true},
		{"cancelled", true},
		{"running", false},
		{"paused", false},
		{"pending", false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(tc.status, func(t *testing.T) {
			r := &OptimizationRun{Status: tc.status}
			if got := r.IsTerminal(); got != tc.want {
				t.Errorf("IsTerminal(%q) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

func TestOptimizationRun_IsOwnedAndActive(t *testing.T) {
	now := time.Now()
	ttl := 5 * time.Minute
	recentHeartbeat := now.Add(-2 * time.Minute)
	staleHeartbeat := now.Add(-10 * time.Minute)

	tests := []struct {
		name string
		run  *OptimizationRun
		want bool
	}{
		{"nil run", nil, false},
		{"no heartbeat", &OptimizationRun{OwnerPID: 123, OwnerHostname: "host"}, false},
		{"no owner pid", &OptimizationRun{OwnerHostname: "host", LastHeartbeatAt: &recentHeartbeat}, false},
		{"no hostname", &OptimizationRun{OwnerPID: 123, LastHeartbeatAt: &recentHeartbeat}, false},
		{"stale heartbeat", &OptimizationRun{OwnerPID: 123, OwnerHostname: "host", LastHeartbeatAt: &staleHeartbeat}, false},
		{"active and owned", &OptimizationRun{OwnerPID: 123, OwnerHostname: "host", LastHeartbeatAt: &recentHeartbeat}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.run.IsOwnedAndActive(now, ttl)
			if got != tc.want {
				t.Errorf("IsOwnedAndActive() = %v, want %v", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// classifyOutcome
// ---------------------------------------------------------------------------

func TestClassifyOutcome_Extended(t *testing.T) {
	tests := []struct {
		name    string
		fitness *Fitness
		delta   float64
		want    string
	}{
		{"nil fitness", nil, 0, "no_change"},
		{"infeasible", &Fitness{Feasible: false}, 0.5, "constraint_violation"},
		{"improvement", &Fitness{Feasible: true}, 0.05, "improvement"},
		{"regression", &Fitness{Feasible: true}, -0.05, "regression"},
		{"no change", &Fitness{Feasible: true}, 0, "no_change"},
		{"tiny positive within epsilon", &Fitness{Feasible: true}, 1e-10, "no_change"},
		{"tiny negative within epsilon", &Fitness{Feasible: true}, -1e-10, "no_change"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyOutcome(tc.fitness, tc.delta)
			if got != tc.want {
				t.Errorf("classifyOutcome() = %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// extractParamValuesFromWorkflow
// ---------------------------------------------------------------------------

func TestExtractParamValuesFromWorkflow(t *testing.T) {
	wfJSON := json.RawMessage(`{
		"nodes": [
			{"id": "agent-a", "data": {"config": {"model": "gpt-4", "temperature": 0.7}}}
		]
	}`)
	spec := &OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[agent-a].data.config.model", Type: ParamTypeModel, Candidates: []string{"gpt-4"}},
			{Path: "nodes[agent-a].data.config.temperature", Type: ParamTypeFloat, Range: &ParamRange{Min: 0, Max: 1}},
		},
	}

	values, err := extractParamValuesFromWorkflow(wfJSON, spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(values) != 2 {
		t.Fatalf("expected 2 values, got %d", len(values))
	}
	if values["nodes[agent-a].data.config.model"] != `"gpt-4"` {
		t.Errorf("model = %q, want %q", values["nodes[agent-a].data.config.model"], `"gpt-4"`)
	}
	if values["nodes[agent-a].data.config.temperature"] != "0.7" {
		t.Errorf("temperature = %q, want %q", values["nodes[agent-a].data.config.temperature"], "0.7")
	}
}

func TestExtractParamValuesFromWorkflow_MissingPath(t *testing.T) {
	wfJSON := json.RawMessage(`{"nodes": [{"id": "a", "data": {"config": {}}}]}`)
	spec := &OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[a].data.config.missing", Type: ParamTypePrompt},
		},
	}
	_, err := extractParamValuesFromWorkflow(wfJSON, spec)
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}

// ---------------------------------------------------------------------------
// applyInitialValuesToWorkflow
// ---------------------------------------------------------------------------

func TestApplyInitialValuesToWorkflow(t *testing.T) {
	base := json.RawMessage(`{
		"nodes": [
			{"id": "a", "data": {"config": {"temperature": 0.7}}}
		]
	}`)

	t.Run("empty initial values returns unchanged", func(t *testing.T) {
		result, err := applyInitialValuesToWorkflow(base, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(result) != string(base) {
			t.Error("expected identical output for nil initial values")
		}
	})

	t.Run("applies value", func(t *testing.T) {
		result, err := applyInitialValuesToWorkflow(base, map[string]string{
			"nodes[a].data.config.temperature": "0.3",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		var wf map[string]interface{}
		if err := json.Unmarshal(result, &wf); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		nodes := wf["nodes"].([]interface{})
		config := nodes[0].(map[string]interface{})["data"].(map[string]interface{})["config"].(map[string]interface{})
		if temp := config["temperature"].(float64); temp != 0.3 {
			t.Errorf("temperature = %v, want 0.3", temp)
		}
	})

	t.Run("invalid JSON value errors", func(t *testing.T) {
		_, err := applyInitialValuesToWorkflow(base, map[string]string{
			"nodes[a].data.config.temperature": "not-json",
		})
		if err == nil {
			t.Fatal("expected error for invalid JSON value")
		}
	})
}

// ---------------------------------------------------------------------------
// sanitizeID and shortID
// ---------------------------------------------------------------------------

func TestSanitizeID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"My Workflow", "my-workflow"},
		{"test/path", "test-path"},
		{"under_score", "under-score"},
		{"dot.name", "dot-name"},
		{"  spaces  ", "spaces"},
		{"", "run"},
		{"UPPERCASE", "uppercase"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := sanitizeID(tc.input)
			if got != tc.want {
				t.Errorf("sanitizeID(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestShortID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"abcdefghijklmnop", "abcdefgh"},
		{"short", "short"},
		{"12345678", "12345678"},
		{"123456789", "12345678"},
		{"", ""},
		{"  spaced  ", "spaced"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := shortID(tc.input)
			if got != tc.want {
				t.Errorf("shortID(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// normalizeVerifyMode and normalizeReplayMode
// ---------------------------------------------------------------------------

func TestNormalizeVerifyMode(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "replay"},
		{"replay", "replay"},
		{"REPLAY", "replay"},
		{"full", "full"},
		{"FULL", "full"},
		{"unknown", "replay"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := normalizeVerifyMode(tc.input)
			if got != tc.want {
				t.Errorf("normalizeVerifyMode(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeReplayMode(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "best_effort"},
		{"best_effort", "best_effort"},
		{"best-effort", "best_effort"},
		{"BEST_EFFORT", "best_effort"},
		{"required", "required"},
		{"strict", "required"},
		{"unknown", "best_effort"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := normalizeReplayMode(tc.input)
			if got != tc.want {
				t.Errorf("normalizeReplayMode(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// sampleFailureCasesByType and sampleFailureCasesRandom
// ---------------------------------------------------------------------------

func TestSampleFailureCasesByType(t *testing.T) {
	t.Run("empty cases", func(t *testing.T) {
		result := sampleFailureCasesByType(nil, 5, nil)
		if len(result) != 0 {
			t.Errorf("expected 0, got %d", len(result))
		}
	})

	t.Run("fewer cases than limit returns all", func(t *testing.T) {
		cases := []FailureCase{
			{ItemID: "1", Category: "parse"},
			{ItemID: "2", Category: "wrong"},
		}
		result := sampleFailureCasesByType(cases, 10, nil)
		if len(result) != 2 {
			t.Errorf("expected 2, got %d", len(result))
		}
	})

	t.Run("samples within limit", func(t *testing.T) {
		cases := make([]FailureCase, 50)
		for i := range cases {
			cases[i] = FailureCase{
				ItemID:   fmt.Sprintf("item-%d", i),
				Category: fmt.Sprintf("cat-%d", i%5),
			}
		}
		rng := rand.New(rand.NewSource(42))
		result := sampleFailureCasesByType(cases, 5, rng)
		if len(result) != 5 {
			t.Errorf("expected 5 samples, got %d", len(result))
		}
	})

	t.Run("zero limit returns copy of input", func(t *testing.T) {
		cases := []FailureCase{{ItemID: "1"}}
		result := sampleFailureCasesByType(cases, 0, nil)
		if len(result) != 1 {
			t.Errorf("expected 1 (full copy), got %d", len(result))
		}
	})
}

func TestSampleFailureCasesRandom(t *testing.T) {
	t.Run("empty cases", func(t *testing.T) {
		result := sampleFailureCasesRandom(nil, 5, nil)
		if len(result) != 0 {
			t.Errorf("expected 0, got %d", len(result))
		}
	})

	t.Run("fewer than limit returns all", func(t *testing.T) {
		cases := []FailureCase{{ItemID: "1"}, {ItemID: "2"}}
		result := sampleFailureCasesRandom(cases, 10, nil)
		if len(result) != 2 {
			t.Errorf("expected 2, got %d", len(result))
		}
	})

	t.Run("samples exact count", func(t *testing.T) {
		cases := make([]FailureCase, 20)
		for i := range cases {
			cases[i] = FailureCase{ItemID: fmt.Sprintf("item-%d", i)}
		}
		rng := rand.New(rand.NewSource(42))
		result := sampleFailureCasesRandom(cases, 5, rng)
		if len(result) != 5 {
			t.Errorf("expected 5, got %d", len(result))
		}
		// All sampled items should be unique.
		seen := map[string]bool{}
		for _, c := range result {
			if seen[c.ItemID] {
				t.Errorf("duplicate item %q in sample", c.ItemID)
			}
			seen[c.ItemID] = true
		}
	})
}

func TestFailureCaseType(t *testing.T) {
	tests := []struct {
		name    string
		failure FailureCase
		want    string
	}{
		{"category set", FailureCase{Category: "Parse Error"}, "parse error"},
		{"no category, reason set", FailureCase{FailureReason: "Timeout"}, "timeout"},
		{"neither set", FailureCase{}, "uncategorized"},
		{"whitespace category", FailureCase{Category: "  "}, "uncategorized"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := failureCaseType(tc.failure)
			if got != tc.want {
				t.Errorf("failureCaseType() = %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Loop.Execute precondition checks
// ---------------------------------------------------------------------------

// mockStore implements PopulationStore for testing.
type mockStore struct {
	organisms    []*Organism
	runs         map[string]*OptimizationRun
	createRunErr error
}

func newMockStore() *mockStore {
	return &mockStore{runs: map[string]*OptimizationRun{}}
}

func (m *mockStore) CreateRun(_ context.Context, run *OptimizationRun) error {
	if m.createRunErr != nil {
		return m.createRunErr
	}
	m.runs[run.ID] = run
	return nil
}
func (m *mockStore) UpdateRunProgress(_ context.Context, runID string, generation int, bestOrgID string, bestFitness *Fitness, spentUSD float64, totalOrganisms int, dspyMetricCallsUsed int) error {
	return nil
}
func (m *mockStore) UpdateRunStatus(_ context.Context, runID string, status string) error {
	if r, ok := m.runs[runID]; ok {
		r.Status = status
	}
	return nil
}
func (m *mockStore) UpdateRunLease(_ context.Context, runID string, ownerPID int, ownerHostname string) error {
	return nil
}
func (m *mockStore) ClearRunLease(_ context.Context, runID string) error { return nil }
func (m *mockStore) GetRun(_ context.Context, runID string) (*OptimizationRun, error) {
	r, ok := m.runs[runID]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return r, nil
}
func (m *mockStore) ListRuns(_ context.Context, status string, workflowID string, limit int) ([]OptimizationRun, error) {
	return nil, nil
}
func (m *mockStore) CreateOrganism(_ context.Context, org *Organism) error {
	m.organisms = append(m.organisms, org)
	return nil
}
func (m *mockStore) UpdateOrganismFitness(_ context.Context, orgID, benchRunID string, fitness *Fitness) error {
	for _, o := range m.organisms {
		if o.ID == orgID {
			o.Fitness = fitness
			o.BenchRunID = benchRunID
			now := time.Now()
			o.EvaluatedAt = &now
		}
	}
	return nil
}
func (m *mockStore) GetOrganism(_ context.Context, orgID string) (*Organism, error) {
	for _, o := range m.organisms {
		if o.ID == orgID {
			return o, nil
		}
	}
	return nil, fmt.Errorf("not found")
}
func (m *mockStore) GetOrganismsByGeneration(_ context.Context, runID string, generation int, limit int) ([]*Organism, error) {
	var result []*Organism
	for _, o := range m.organisms {
		if o.OptRunID == runID && o.Generation == generation {
			result = append(result, o)
		}
	}
	return result, nil
}
func (m *mockStore) GetAllOrganisms(_ context.Context, runID string) ([]*Organism, error) {
	var result []*Organism
	for _, o := range m.organisms {
		if o.OptRunID == runID {
			result = append(result, o)
		}
	}
	return result, nil
}
func (m *mockStore) GetBestOrganisms(_ context.Context, runID string, limit int) ([]*Organism, error) {
	return m.organisms, nil
}
func (m *mockStore) GetOrganismLineage(_ context.Context, orgID string) ([]*Organism, error) {
	return nil, nil
}
func (m *mockStore) AppendLearningEntry(_ context.Context, runID string, entry *LearningEntry) error {
	return nil
}
func (m *mockStore) GetLearningLog(_ context.Context, runID string, limit int) ([]LearningEntry, error) {
	return nil, nil
}
func (m *mockStore) CreateParamChanges(_ context.Context, organismID string, changes []ParamChange) error {
	return nil
}
func (m *mockStore) GetParamChanges(_ context.Context, organismID string) ([]ParamChange, error) {
	return nil, nil
}
func (m *mockStore) CreateMutationArtifacts(_ context.Context, organismID string, artifacts []MutationArtifact, compact bool) error {
	return nil
}
func (m *mockStore) GetMutationArtifacts(_ context.Context, organismID string) ([]MutationArtifact, error) {
	return nil, nil
}

// mockEvaluator implements BenchmarkEvaluator for testing.
type mockEvaluator struct {
	fitness *Fitness
}

func (m *mockEvaluator) RunBenchmark(_ context.Context, req RunBenchmarkRequest) (string, error) {
	return "bench-run-1", nil
}
func (m *mockEvaluator) RunBatchBenchmark(_ context.Context, req RunBatchBenchmarkRequest) (map[string]string, error) {
	result := map[string]string{}
	for _, wfID := range req.WorkflowIDs {
		result[wfID] = "bench-run-" + wfID
	}
	return result, nil
}
func (m *mockEvaluator) ReplayBenchmark(_ context.Context, req ReplayBenchmarkRequest) (string, error) {
	return "replay-run-1", nil
}
func (m *mockEvaluator) WaitForCompletion(_ context.Context) error { return nil }
func (m *mockEvaluator) GetRunSummary(_ context.Context, runID string) (*Fitness, error) {
	if m.fitness != nil {
		return m.fitness, nil
	}
	return &Fitness{
		Accuracy:         0.80,
		AdjustedAccuracy: 0.80,
		ParseRate:        1.0,
		CostPerItem:      0.01,
		CompositeScore:   0.80,
		Feasible:         true,
		TotalItems:       10,
	}, nil
}
func (m *mockEvaluator) GetFailureCases(_ context.Context, runID string, limit int) ([]FailureCase, error) {
	return nil, nil
}

// mockWorkflowManager implements WorkflowManager for testing.
type mockWorkflowManager struct {
	created map[string]json.RawMessage
	deleted []string
}

func newMockWorkflowManager() *mockWorkflowManager {
	return &mockWorkflowManager{created: map[string]json.RawMessage{}}
}

func (m *mockWorkflowManager) CreateTemporaryWorkflow(_ context.Context, id string, wfJSON json.RawMessage, name string, description string) error {
	m.created[id] = wfJSON
	return nil
}
func (m *mockWorkflowManager) DeleteTemporaryWorkflow(_ context.Context, id string) error {
	m.deleted = append(m.deleted, id)
	return nil
}

func TestLoop_Execute_NilRun(t *testing.T) {
	l := &Loop{}
	err := l.Execute(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error for nil run")
	}
}

func TestLoop_Execute_MissingDependencies(t *testing.T) {
	run := &OptimizationRun{
		Spec: &OptimizeSpec{
			Params:     []ParamDeclaration{{Path: "nodes[a].data.config.model", Type: ParamTypeModel, Candidates: []string{"x"}}},
			Objectives: []Objective{{Metric: "accuracy", Direction: "maximize", Weight: 1}},
			StopPolicy: StopPolicy{MaxGenerations: 1, BudgetUSD: 1},
		},
	}

	l := &Loop{}
	err := l.Execute(context.Background(), run, nil)
	if err == nil {
		t.Fatal("expected error for missing evaluator/store/workflows")
	}
}

func TestLoop_Execute_NilSpec(t *testing.T) {
	l := &Loop{
		Evaluator: &mockEvaluator{},
		Store:     newMockStore(),
		Workflows: newMockWorkflowManager(),
	}
	run := &OptimizationRun{}
	err := l.Execute(context.Background(), run, nil)
	if err == nil {
		t.Fatal("expected error for nil spec")
	}
}

func TestLoop_Execute_MissingMutatorForEvolutionary(t *testing.T) {
	l := &Loop{
		Evaluator: &mockEvaluator{},
		Store:     newMockStore(),
		Workflows: newMockWorkflowManager(),
	}
	run := &OptimizationRun{
		Strategy: "evolutionary",
		Spec: &OptimizeSpec{
			Params:     []ParamDeclaration{{Path: "nodes[a].data.config.model", Type: ParamTypeModel, Candidates: []string{"x"}}},
			Objectives: []Objective{{Metric: "accuracy", Direction: "maximize", Weight: 1}},
			StopPolicy: StopPolicy{MaxGenerations: 1, BudgetUSD: 1},
		},
	}
	err := l.Execute(context.Background(), run, json.RawMessage(`{"nodes":[{"id":"a","data":{"config":{"model":"x"}}}]}`))
	if err == nil {
		t.Fatal("expected error for missing mutator with evolutionary strategy")
	}
}
