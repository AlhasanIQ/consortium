package optimize

import (
	"context"
	"encoding/json"
	"math"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Fitness helpers
// ---------------------------------------------------------------------------

func TestMetricValue(t *testing.T) {
	f := &Fitness{
		Accuracy:         0.80,
		AdjustedAccuracy: 0.85,
		ParseRate:        0.95,
		CostPerItem:      0.03,
		AvgLatencyMS:     1200,
		P95LatencyMS:     3000,
	}

	tests := []struct {
		metric string
		want   float64
	}{
		{"accuracy", 0.85},
		{"adjusted_accuracy", 0.85},
		{"raw_accuracy", 0.80},
		{"parse_rate", 0.95},
		{"cost_per_item", 0.03},
		{"avg_latency_ms", 1200},
		{"p95_latency_ms", 3000},
		{"unknown_metric", 0},
	}
	for _, tc := range tests {
		got := f.MetricValue(tc.metric)
		if math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("MetricValue(%q) = %v, want %v", tc.metric, got, tc.want)
		}
	}

	// nil receiver
	var nilF *Fitness
	if got := nilF.MetricValue("accuracy"); got != 0 {
		t.Errorf("nil Fitness MetricValue = %v, want 0", got)
	}
}

func TestComputeCompositeScore(t *testing.T) {
	objectives := []Objective{
		{Metric: "accuracy", Direction: "maximize", Weight: 3.0},
		{Metric: "cost_per_item", Direction: "minimize", Weight: 1.0},
	}

	f := &Fitness{AdjustedAccuracy: 0.90, CostPerItem: 0.05}
	score := ComputeCompositeScore(f, objectives)
	// accuracy component: 0.90 * 3.0 = 2.70
	// cost component: (1/0.05) * 1.0 = 20.0
	expected := 2.70 + 20.0
	if math.Abs(score-expected) > 1e-9 {
		t.Errorf("ComputeCompositeScore = %v, want %v", score, expected)
	}
}

func TestComputeCompositeScoreNilFitness(t *testing.T) {
	objectives := []Objective{{Metric: "accuracy", Direction: "maximize", Weight: 1.0}}
	if got := ComputeCompositeScore(nil, objectives); got != 0 {
		t.Errorf("ComputeCompositeScore(nil) = %v, want 0", got)
	}
}

func TestComputeCompositeScoreZeroCost(t *testing.T) {
	objectives := []Objective{
		{Metric: "cost_per_item", Direction: "minimize", Weight: 1.0},
	}
	f := &Fitness{CostPerItem: 0.0}
	score := ComputeCompositeScore(f, objectives)
	// Zero cost → score 0 (conservative, avoids rewarding broken runs).
	if score != 0 {
		t.Errorf("ComputeCompositeScore with zero cost = %v, want 0", score)
	}
}

func TestCheckConstraints(t *testing.T) {
	f := &Fitness{AdjustedAccuracy: 0.90, CostPerItem: 0.04, ParseRate: 0.97}

	constraints := []Constraint{
		{Metric: "cost_per_item", Op: "<=", Value: 0.05},
		{Metric: "parse_rate", Op: ">=", Value: 0.95},
	}
	feasible, violations := CheckConstraints(f, constraints)
	if !feasible {
		t.Errorf("expected feasible, got violations: %v", violations)
	}
}

func TestCheckConstraintsViolation(t *testing.T) {
	f := &Fitness{CostPerItem: 0.10, ParseRate: 0.80}
	constraints := []Constraint{
		{Metric: "cost_per_item", Op: "<=", Value: 0.05},
		{Metric: "parse_rate", Op: ">=", Value: 0.95},
	}
	feasible, violations := CheckConstraints(f, constraints)
	if feasible {
		t.Errorf("expected infeasible")
	}
	if len(violations) != 2 {
		t.Errorf("expected 2 violations, got %d: %v", len(violations), violations)
	}
}

func TestCheckConstraintsNilFitness(t *testing.T) {
	constraints := []Constraint{{Metric: "accuracy", Op: ">=", Value: 0.5}}
	feasible, violations := CheckConstraints(nil, constraints)
	if feasible {
		t.Errorf("expected infeasible for nil fitness")
	}
	if len(violations) != 1 || violations[0] != "fitness is nil" {
		t.Errorf("unexpected violations: %v", violations)
	}
}

func TestCheckConstraintsNoConstraints(t *testing.T) {
	feasible, _ := CheckConstraints(nil, nil)
	if !feasible {
		t.Errorf("expected feasible when no constraints")
	}
}

// ---------------------------------------------------------------------------
// OptimizeSpec validation
// ---------------------------------------------------------------------------

func TestOptimizeSpecValidate(t *testing.T) {
	spec := validSpec()
	if err := spec.Validate(); err != nil {
		t.Fatalf("valid spec returned error: %v", err)
	}
}

func TestOptimizeSpecValidateNoParams(t *testing.T) {
	spec := validSpec()
	spec.Params = nil
	if err := spec.Validate(); err == nil {
		t.Fatal("expected error for empty params")
	}
}

func TestOptimizeSpecValidateDuplicatePath(t *testing.T) {
	spec := validSpec()
	spec.Params = append(spec.Params, spec.Params[0])
	if err := spec.Validate(); err == nil {
		t.Fatal("expected error for duplicate path")
	}
}

func TestParamDeclarationValidate(t *testing.T) {
	tests := []struct {
		name    string
		param   ParamDeclaration
		wantErr bool
	}{
		{
			name:    "valid model",
			param:   ParamDeclaration{Path: "nodes[a].data.config.model", Type: ParamTypeModel, Candidates: []string{"m1", "m2"}},
			wantErr: false,
		},
		{
			name:    "empty path",
			param:   ParamDeclaration{Path: "", Type: ParamTypeModel, Candidates: []string{"m1"}},
			wantErr: true,
		},
		{
			name:    "model missing candidates",
			param:   ParamDeclaration{Path: "nodes[a].data.config.model", Type: ParamTypeModel},
			wantErr: true,
		},
		{
			name:    "float missing range",
			param:   ParamDeclaration{Path: "nodes[a].data.config.temperature", Type: ParamTypeFloat},
			wantErr: true,
		},
		{
			name:    "valid float",
			param:   ParamDeclaration{Path: "nodes[a].data.config.temperature", Type: ParamTypeFloat, Range: &ParamRange{Min: 0, Max: 1, Step: 0.1}},
			wantErr: false,
		},
		{
			name:    "invalid path format",
			param:   ParamDeclaration{Path: "config.model", Type: ParamTypeModel, Candidates: []string{"m1"}},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.param.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Materialize
// ---------------------------------------------------------------------------

func TestMaterializeWorkflow(t *testing.T) {
	base := testWorkflowJSON()
	spec := testMaterializeSpec()

	values := map[string]string{
		"nodes[agent-a].data.config.model":       `"model-b"`,
		"nodes[agent-a].data.config.temperature": `0.5`,
	}

	result, err := MaterializeWorkflow(base, values, spec)
	if err != nil {
		t.Fatalf("MaterializeWorkflow: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("parse result: %v", err)
	}

	// Verify model was changed.
	val, found, err := getPathValue(parsed, "nodes[agent-a].data.config.model")
	if err != nil || !found {
		t.Fatalf("model path not found: err=%v, found=%v", err, found)
	}
	if val != "model-b" {
		t.Errorf("model = %v, want model-b", val)
	}

	// Verify temperature was changed.
	val, found, err = getPathValue(parsed, "nodes[agent-a].data.config.temperature")
	if err != nil || !found {
		t.Fatalf("temperature path not found")
	}
	if f, ok := val.(float64); !ok || math.Abs(f-0.5) > 1e-9 {
		t.Errorf("temperature = %v, want 0.5", val)
	}
}

func TestMaterializeLockedPathViolation(t *testing.T) {
	base := testWorkflowJSON()
	spec := testMaterializeSpec()
	spec.Locked = []string{"nodes[agent-a].data.config.model"}

	values := map[string]string{
		"nodes[agent-a].data.config.model":       `"model-b"`,
		"nodes[agent-a].data.config.temperature": `0.5`,
	}

	_, err := MaterializeWorkflow(base, values, spec)
	if err == nil {
		t.Fatal("expected error for locked path violation")
	}
}

func TestMaterializeUndeclaredPath(t *testing.T) {
	base := testWorkflowJSON()
	spec := testMaterializeSpec()

	values := map[string]string{
		"nodes[agent-a].data.config.model":       `"model-a"`,
		"nodes[agent-a].data.config.temperature": `0.5`,
		"nodes[agent-a].data.config.unknown":     `"foo"`,
	}

	_, err := MaterializeWorkflow(base, values, spec)
	if err == nil {
		t.Fatal("expected error for undeclared path")
	}
}

func TestMaterializeInvalidValue(t *testing.T) {
	base := testWorkflowJSON()
	spec := testMaterializeSpec()

	// "model-z" is not in the candidates list.
	values := map[string]string{
		"nodes[agent-a].data.config.model":       `"model-z"`,
		"nodes[agent-a].data.config.temperature": `0.5`,
	}

	_, err := MaterializeWorkflow(base, values, spec)
	if err == nil {
		t.Fatal("expected error for invalid model candidate")
	}
}

// ---------------------------------------------------------------------------
// Path parsing
// ---------------------------------------------------------------------------

func TestParsePath(t *testing.T) {
	tests := []struct {
		path     string
		wantNode string
		wantLen  int
		wantErr  bool
	}{
		{"nodes[agent-a].data.config.model", "agent-a", 3, false},
		{"nodes[result-synthesis].data.config.aggregationConfig.temperature", "result-synthesis", 4, false},
		{"config.model", "", 0, true},              // missing nodes[]
		{"nodes[].data.config.model", "", 0, true}, // empty node ID
	}
	for _, tc := range tests {
		parsed, err := parsePath(tc.path)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parsePath(%q) expected error", tc.path)
			}
			continue
		}
		if err != nil {
			t.Errorf("parsePath(%q) unexpected error: %v", tc.path, err)
			continue
		}
		if parsed.NodeID != tc.wantNode {
			t.Errorf("parsePath(%q).NodeID = %q, want %q", tc.path, parsed.NodeID, tc.wantNode)
		}
		if len(parsed.Fields) != tc.wantLen {
			t.Errorf("parsePath(%q) fields len = %d, want %d", tc.path, len(parsed.Fields), tc.wantLen)
		}
	}
}

// ---------------------------------------------------------------------------
// Selection
// ---------------------------------------------------------------------------

func TestSelectParentsEmpty(t *testing.T) {
	_, err := SelectParents(nil, nil, 3, DefaultSelectionConfig(), nil)
	if err == nil {
		t.Fatal("expected error for empty population")
	}
}

func TestSelectParentsBasic(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	population := []*Organism{
		{ID: "a", Fitness: &Fitness{CompositeScore: 10.0}},
		{ID: "b", Fitness: &Fitness{CompositeScore: 5.0}},
		{ID: "c", Fitness: &Fitness{CompositeScore: 1.0}},
	}
	childCounts := map[string]int{"a": 3, "b": 0, "c": 0}
	parents, err := SelectParents(population, childCounts, 5, DefaultSelectionConfig(), rng)
	if err != nil {
		t.Fatalf("SelectParents: %v", err)
	}
	if len(parents) != 5 {
		t.Errorf("expected 5 parents, got %d", len(parents))
	}

	// With these settings, high-scoring "a" is still likely to be selected despite
	// having 3 children (novelty penalty), and "b" with 0 children should also appear.
	ids := map[string]int{}
	for _, p := range parents {
		ids[p.ID]++
	}
	// Just verify all parents are from the population.
	for id := range ids {
		found := false
		for _, org := range population {
			if org.ID == id {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("parent %q not in population", id)
		}
	}
}

func TestSelectParentsSingleOrganism(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	population := []*Organism{
		{ID: "only", Fitness: &Fitness{CompositeScore: 5.0}},
	}
	parents, err := SelectParents(population, nil, 3, DefaultSelectionConfig(), rng)
	if err != nil {
		t.Fatalf("SelectParents: %v", err)
	}
	for _, p := range parents {
		if p.ID != "only" {
			t.Errorf("expected all parents to be 'only', got %q", p.ID)
		}
	}
}

func TestFilterEligibleParentsRequiresFitnessAndEvaluation(t *testing.T) {
	now := time.Now().UTC()
	population := []*Organism{
		{
			ID:          "valid",
			BenchRunID:  "bench-1",
			Fitness:     &Fitness{CompositeScore: 1, Feasible: true},
			EvaluatedAt: &now,
		},
		{
			ID:          "missing-bench",
			Fitness:     &Fitness{CompositeScore: 2, Feasible: true},
			EvaluatedAt: &now,
		},
		{
			ID:         "missing-evaluated-at",
			BenchRunID: "bench-2",
			Fitness:    &Fitness{CompositeScore: 3, Feasible: true},
		},
		{
			ID:          "missing-fitness",
			BenchRunID:  "bench-3",
			EvaluatedAt: &now,
		},
	}
	eligible := filterEligibleParents(population)
	if len(eligible) != 1 || eligible[0].ID != "valid" {
		t.Fatalf("unexpected eligible parents: %+v", eligible)
	}
}

// ---------------------------------------------------------------------------
// JSON extraction (claude_cli.go)
// ---------------------------------------------------------------------------

func TestExtractFirstJSONObject(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple",
			input: `Here is the result: {"revised_prompt": "test", "reasoning": "r"}`,
			want:  `{"revised_prompt": "test", "reasoning": "r"}`,
		},
		{
			name:  "nested braces",
			input: `{"outer": {"inner": "val"}}`,
			want:  `{"outer": {"inner": "val"}}`,
		},
		{
			name:  "escaped quotes",
			input: `prefix {"key": "value with \"quotes\""}`,
			want:  `{"key": "value with \"quotes\""}`,
		},
		{
			name:  "no json",
			input: `just plain text`,
			want:  ``,
		},
		{
			name:  "incomplete",
			input: `{"key": "val"`,
			want:  ``,
		},
		{
			name:  "braces in string",
			input: `{"key": "value with { and } inside"}`,
			want:  `{"key": "value with { and } inside"}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractFirstJSONObject(tc.input)
			if got != tc.want {
				t.Errorf("extractFirstJSONObject = %q, want %q", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// SeedParser
// ---------------------------------------------------------------------------

func TestParseSeedOptimizeSpec(t *testing.T) {
	seed := testSeedJSON()
	parsed, err := ParseSeedOptimizeSpec(seed)
	if err != nil {
		t.Fatalf("ParseSeedOptimizeSpec: %v", err)
	}
	if parsed.WorkflowID != "test-seed" {
		t.Errorf("WorkflowID = %q, want test-seed", parsed.WorkflowID)
	}
	if parsed.OptimizeSpec == nil {
		t.Fatal("OptimizeSpec is nil")
	}
	if len(parsed.OptimizeSpec.Params) != 2 {
		t.Errorf("expected 2 params, got %d", len(parsed.OptimizeSpec.Params))
	}

	// Verify optimize key is stripped from workflow JSON.
	var wf map[string]interface{}
	if err := json.Unmarshal(parsed.WorkflowJSON, &wf); err != nil {
		t.Fatalf("parse workflow JSON: %v", err)
	}
	if _, exists := wf["optimize"]; exists {
		t.Error("optimize key should be stripped from workflow JSON")
	}
}

func TestParseSeedOptimizeSpecMissing(t *testing.T) {
	seed := json.RawMessage(`{"id":"test","name":"Test","nodes":[]}`)
	_, err := ParseSeedOptimizeSpec(seed)
	if err == nil {
		t.Fatal("expected error for missing optimize spec")
	}
}

// ---------------------------------------------------------------------------
// Combinatorial mutator
// ---------------------------------------------------------------------------

func TestCombinatorialMutator(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	mutator := &CombinatorialMutator{Rand: rng}

	parent := &Organism{
		ID:       "parent-1",
		OptRunID: "run-1",
		ParamValues: map[string]string{
			"nodes[agent-a].data.config.model":       `"model-a"`,
			"nodes[agent-a].data.config.temperature": `0.7`,
		},
		WorkflowJSON: testWorkflowJSON(),
	}

	spec := testMaterializeSpec()
	req := &MutationRequest{
		Parent:     parent,
		Spec:       spec,
		Generation: 1,
		Count:      3,
	}

	results, err := mutator.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	for i, result := range results {
		if result.Organism == nil {
			t.Errorf("result[%d].Organism is nil", i)
			continue
		}
		if result.Organism.MutationType != "combinatorial" {
			t.Errorf("result[%d].MutationType = %q, want combinatorial", i, result.Organism.MutationType)
		}
		if len(result.Changes) == 0 {
			t.Errorf("result[%d] has no changes", i)
		}
		if result.Organism.Generation != 1 {
			t.Errorf("result[%d].Generation = %d, want 1", i, result.Organism.Generation)
		}
		if len(result.Organism.ParentIDs) != 1 || result.Organism.ParentIDs[0] != "parent-1" {
			t.Errorf("result[%d] parent IDs = %v", i, result.Organism.ParentIDs)
		}
	}
}

// ---------------------------------------------------------------------------
// Hybrid mutator provenance
// ---------------------------------------------------------------------------

func TestHybridMutatorProvenance(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	combMutator := &CombinatorialMutator{Rand: rng}

	hybrid := &AdaptiveMixMutator{
		Combinatorial:             combMutator,
		LLM:                       nil, // LLM nil → always uses combinatorial
		PromptMutationProbability: 0.0, // Force combinatorial
		Rand:                      rng,
	}

	parent := &Organism{
		ID:       "parent-1",
		OptRunID: "run-1",
		ParamValues: map[string]string{
			"nodes[agent-a].data.config.model":       `"model-a"`,
			"nodes[agent-a].data.config.temperature": `0.7`,
		},
		WorkflowJSON: testWorkflowJSON(),
	}

	spec := testMaterializeSpec()
	req := &MutationRequest{
		Parent:     parent,
		Spec:       spec,
		Generation: 1,
		Count:      1,
	}

	results, err := hybrid.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Should preserve provenance as "auto:combinatorial" not just "hybrid".
	got := results[0].Organism.MutationType
	if got != "auto:combinatorial" {
		t.Errorf("MutationType = %q, want auto:combinatorial", got)
	}
}

type stubMutator struct {
	mutate func(req *MutationRequest) ([]*MutationResult, error)
	calls  int
}

func (m *stubMutator) Mutate(_ context.Context, req *MutationRequest) ([]*MutationResult, error) {
	m.calls++
	return m.mutate(req)
}

func TestHybridMutatorPromptOnlySpecAvoidsCombinatorialPath(t *testing.T) {
	comb := &stubMutator{
		mutate: func(_ *MutationRequest) ([]*MutationResult, error) {
			t.Fatalf("combinatorial mutator should not be used for prompt-only spec")
			return nil, nil
		},
	}
	llm := &stubMutator{
		mutate: func(req *MutationRequest) ([]*MutationResult, error) {
			return []*MutationResult{{
				Organism: newChildOrganism(req.Parent, req.Generation, "llm_prompt", "stub llm", map[string]string{
					"nodes[a].data.config.prompt": `"new prompt"`,
				}),
			}}, nil
		},
	}
	hybrid := &AdaptiveMixMutator{
		Combinatorial:             comb,
		LLM:                       llm,
		PromptMutationProbability: 0.0, // Explicitly prefer combinatorial if both branches are eligible.
		Rand:                      rand.New(rand.NewSource(1)),
	}
	req := &MutationRequest{
		Parent: &Organism{
			ID: "parent",
			ParamValues: map[string]string{
				"nodes[a].data.config.prompt": `"old prompt"`,
			},
		},
		Spec: &OptimizeSpec{
			Params: []ParamDeclaration{{
				Path: "nodes[a].data.config.prompt",
				Type: ParamTypePrompt,
			}},
			Objectives:      []Objective{{Metric: "adjusted_accuracy", Direction: "maximize", Weight: 1}},
			StopPolicy:      StopPolicy{MaxGenerations: 1, BudgetUSD: 1},
			PromotionPolicy: PromotionPolicy{},
		},
		Generation: 1,
		Count:      1,
	}

	results, err := hybrid.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if comb.calls != 0 {
		t.Fatalf("combinatorial calls = %d, want 0", comb.calls)
	}
	if llm.calls != 1 {
		t.Fatalf("llm calls = %d, want 1", llm.calls)
	}
	if got := results[0].Organism.MutationType; got != "auto:llm_prompt" {
		t.Fatalf("MutationType = %q, want auto:llm_prompt", got)
	}
}

func TestHybridMutatorCombinatorialOnlySpecAvoidsPromptPath(t *testing.T) {
	comb := &stubMutator{
		mutate: func(req *MutationRequest) ([]*MutationResult, error) {
			return []*MutationResult{{
				Organism: newChildOrganism(req.Parent, req.Generation, "combinatorial", "stub combinatorial", map[string]string{
					"nodes[a].data.config.temperature": "0.2",
				}),
			}}, nil
		},
	}
	llm := &stubMutator{
		mutate: func(_ *MutationRequest) ([]*MutationResult, error) {
			t.Fatalf("llm mutator should not be used for combinatorial-only spec")
			return nil, nil
		},
	}
	hybrid := &AdaptiveMixMutator{
		Combinatorial:             comb,
		LLM:                       llm,
		PromptMutationProbability: 1.0, // Explicitly prefer prompt if both branches are eligible.
		Rand:                      rand.New(rand.NewSource(1)),
	}
	req := &MutationRequest{
		Parent: &Organism{
			ID: "parent",
			ParamValues: map[string]string{
				"nodes[a].data.config.temperature": "0.1",
			},
		},
		Spec: &OptimizeSpec{
			Params: []ParamDeclaration{{
				Path:  "nodes[a].data.config.temperature",
				Type:  ParamTypeFloat,
				Range: &ParamRange{Min: 0, Max: 1, Step: 0.1},
			}},
			Objectives:      []Objective{{Metric: "adjusted_accuracy", Direction: "maximize", Weight: 1}},
			StopPolicy:      StopPolicy{MaxGenerations: 1, BudgetUSD: 1},
			PromotionPolicy: PromotionPolicy{},
		},
		Generation: 1,
		Count:      1,
	}

	results, err := hybrid.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if comb.calls != 1 {
		t.Fatalf("combinatorial calls = %d, want 1", comb.calls)
	}
	if llm.calls != 0 {
		t.Fatalf("llm calls = %d, want 0", llm.calls)
	}
	if got := results[0].Organism.MutationType; got != "auto:combinatorial" {
		t.Fatalf("MutationType = %q, want auto:combinatorial", got)
	}
}

func TestSelectPromptLearningEntriesPrefersPromptMutations(t *testing.T) {
	entries := []LearningEntry{
		{Generation: 1, MutationType: "auto:combinatorial", Outcome: "regression", Description: "comb"},
		{Generation: 2, MutationType: "auto:llm_prompt", Outcome: "no_change", Description: "prompt-old"},
		{Generation: 3, MutationType: "auto:llm_prompt", Outcome: "improvement", Description: "prompt-improve"},
		{Generation: 4, MutationType: "llm_prompt", Outcome: "regression", Description: "prompt-new"},
	}
	selected := selectPromptLearningEntries(entries, 10)
	if len(selected) != 2 {
		t.Fatalf("expected 2 prompt-related non-improving entries, got %d", len(selected))
	}
	if selected[0].Description != "prompt-new" || selected[1].Description != "prompt-old" {
		t.Fatalf("unexpected selected order: %#v", selected)
	}
}

func TestBuildLLMMutationPromptIncludesQuestionAndCategorySummary(t *testing.T) {
	prompt := buildLLMMutationPrompt(
		"You are helpful.",
		[]FailureCase{{
			ItemID:         "item-1",
			Question:       "What is the best answer to this question?",
			CorrectAnswer:  "A",
			Predicted:      "B",
			Category:       "reasoning_error",
			FailureReason:  "wrong reasoning",
			RawOutput:      "Long raw output text",
			ChildPredicted: "C",
			Diagnosis:      "Some child agents were right but synthesis picked wrong.",
			AgentAnswers: []FailureAgentAnswer{
				{Model: "model-a", Answer: "A", ParseOK: true, Correct: true},
				{Model: "model-b", Answer: "B", ParseOK: true, Correct: false},
			},
		}},
		nil,
		&OptimizeSpec{
			Objectives: []Objective{{Metric: "adjusted_accuracy", Direction: "maximize", Weight: 1}},
		},
		nil,
	)
	if !strings.Contains(prompt, "Question excerpt:") {
		t.Fatalf("prompt missing question excerpt: %s", prompt)
	}
	if !strings.Contains(prompt, "Top failure categories:") {
		t.Fatalf("prompt missing category summary: %s", prompt)
	}
	if !strings.Contains(prompt, "reasoning_error (1)") {
		t.Fatalf("prompt missing category count: %s", prompt)
	}
	if !strings.Contains(prompt, "Diagnosis hint:") {
		t.Fatalf("prompt missing diagnosis hint: %s", prompt)
	}
	if !strings.Contains(prompt, "Agent votes (model:answer:mark):") {
		t.Fatalf("prompt missing agent votes summary: %s", prompt)
	}
}

func TestBuildMIPROv2MutationPromptIncludesStrategySections(t *testing.T) {
	prompt := buildMIPROv2MutationPrompt(
		"You are a reasoning assistant.",
		[]FailureCase{{
			ItemID:        "item-1",
			Question:      "Why does this fail?",
			CorrectAnswer: "A",
			Predicted:     "B",
			Category:      "reasoning_error",
			FailureReason: "missed constraint",
		}},
		nil,
		&OptimizeSpec{
			Objectives: []Objective{{Metric: "adjusted_accuracy", Direction: "maximize", Weight: 1}},
		},
		nil,
	)
	if !strings.Contains(prompt, "MIPROv2-style") {
		t.Fatalf("prompt missing MIPROv2 marker: %s", prompt)
	}
	if !strings.Contains(prompt, "Failure-type frequency") {
		t.Fatalf("prompt missing failure frequency section: %s", prompt)
	}
	if !strings.Contains(prompt, "candidate instruction edits") {
		t.Fatalf("prompt missing candidate-instruction guidance: %s", prompt)
	}
}

func TestBuildGEPAMutationPromptIncludesReflectiveSections(t *testing.T) {
	prompt := buildGEPAMutationPrompt(
		"You are a reasoning assistant.",
		[]FailureCase{{
			ItemID:        "item-1",
			Question:      "Why does this fail?",
			CorrectAnswer: "A",
			Predicted:     "B",
			Category:      "aggregation_error",
			FailureReason: "picked weak evidence",
		}},
		[]LearningEntry{{
			Generation:   2,
			MutationType: "auto:llm_prompt",
			Description:  "tightened rubric",
			Outcome:      "no_change",
			FitnessDelta: 0,
		}},
		&OptimizeSpec{
			Objectives: []Objective{{Metric: "adjusted_accuracy", Direction: "maximize", Weight: 1}},
		},
		nil,
	)
	if !strings.Contains(prompt, "GEPA-style reflective") {
		t.Fatalf("prompt missing GEPA marker: %s", prompt)
	}
	if !strings.Contains(prompt, "Trajectory Feedback") {
		t.Fatalf("prompt missing trajectory feedback section: %s", prompt)
	}
	if !strings.Contains(prompt, "Perform reflective evolution") {
		t.Fatalf("prompt missing reflective evolution instructions: %s", prompt)
	}
}

func TestFailureEnrichmentSimulationBuildsDrillAwarePrompt(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/admin/benchmarks/test-run/analysis", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"item_id":          "row-17",
					"answer_label":     "A",
					"parent_predicted": "D",
					"child_predicted":  "B",
					"category":         "some_right_child_wrong",
					"parent_job_id":    "job-parent-17",
					"child_job_id":     "job-child-17",
					"agent_answers": []map[string]interface{}{
						{"node_id": "agent-1", "model": "mimo-v2", "answer": "A", "parse_ok": true, "correct": true},
						{"node_id": "agent-2", "model": "grok-fast", "answer": "B", "parse_ok": true, "correct": false},
					},
				},
			},
		})
	})
	mux.HandleFunc("/api/admin/benchmarks/test-run/items", func(w http.ResponseWriter, r *http.Request) {
		if got := strings.TrimSpace(r.URL.Query().Get("item_id")); got != "row-17" {
			t.Fatalf("expected item_id=row-17, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"DatasetItem": map[string]interface{}{
				"question": "Which option best explains the observed failure?",
			},
			"Detail": map[string]interface{}{
				"item": map[string]interface{}{
					"failure_reason": "child aggregation disagreement",
					"raw_output":     "Final answer: D",
				},
				"attempts": []map[string]interface{}{
					{"failure_reason": "child aggregation disagreement", "raw_output": "Final answer: D"},
				},
			},
		})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	evaluator := NewHTTPBenchmarkEvaluator(server.URL, 3*time.Second)
	cases, err := evaluator.GetFailureCases(context.Background(), "test-run", 5)
	if err != nil {
		t.Fatalf("GetFailureCases: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("expected 1 failure case, got %d", len(cases))
	}
	enriched, err := evaluator.EnrichFailureCases(context.Background(), "test-run", cases)
	if err != nil {
		t.Fatalf("EnrichFailureCases: %v", err)
	}
	if len(enriched) != 1 {
		t.Fatalf("expected 1 enriched failure case, got %d", len(enriched))
	}
	if strings.TrimSpace(enriched[0].Question) == "" {
		t.Fatalf("expected enriched failure to include question")
	}
	if strings.TrimSpace(enriched[0].RawOutput) == "" {
		t.Fatalf("expected enriched failure to include raw output")
	}
	if strings.TrimSpace(enriched[0].Diagnosis) == "" {
		t.Fatalf("expected enriched failure to include diagnosis")
	}

	prompt := buildLLMMutationPrompt(
		"You are a reasoning workflow prompt.",
		enriched,
		[]LearningEntry{{Generation: 2, MutationType: "llm_prompt", Description: "tightened rubric", Outcome: "regression", FitnessDelta: -0.1}},
		&OptimizeSpec{
			Objectives:  []Objective{{Metric: "adjusted_accuracy", Direction: "maximize", Weight: 1}},
			Constraints: []Constraint{{Metric: "parse_rate", Op: ">=", Value: 0.95}},
		},
		nil,
	)
	t.Logf("simulated_claude_input:\n%s", prompt)

	expected := []string{
		"Question excerpt:",
		"Agent votes (model:answer:mark):",
		"Diagnosis hint:",
		"Raw output excerpt:",
		"Top failure categories:",
		"Failure: child aggregation disagreement | Category: some_right_child_wrong",
	}
	for _, needle := range expected {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("expected prompt to contain %q, got:\n%s", needle, prompt)
		}
	}
}

func TestResolveAutoMutatorMode(t *testing.T) {
	promptOnly := &OptimizeSpec{
		Params: []ParamDeclaration{{
			Path: "nodes[a].data.config.prompt",
			Type: ParamTypePrompt,
		}},
	}
	if got := ResolveAutoMutatorMode(promptOnly); got != MutatorModeGEPA {
		t.Fatalf("ResolveAutoMutatorMode(promptOnly) = %q, want %q", got, MutatorModeGEPA)
	}

	combinatorialOnly := &OptimizeSpec{
		Params: []ParamDeclaration{{
			Path:       "nodes[a].data.config.model",
			Type:       ParamTypeModel,
			Candidates: []string{"m1", "m2"},
		}},
	}
	if got := ResolveAutoMutatorMode(combinatorialOnly); got != MutatorModeCombinatorial {
		t.Fatalf("ResolveAutoMutatorMode(combinatorialOnly) = %q, want %q", got, MutatorModeCombinatorial)
	}

	mixed := &OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[a].data.config.prompt", Type: ParamTypePrompt},
			{Path: "nodes[a].data.config.temperature", Type: ParamTypeFloat, Range: &ParamRange{Min: 0, Max: 1}},
		},
	}
	if got := ResolveAutoMutatorMode(mixed); got != MutatorModeMIPROv2 {
		t.Fatalf("ResolveAutoMutatorMode(mixed) = %q, want %q", got, MutatorModeMIPROv2)
	}
}

func TestNormalizeMutatorMode(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", MutatorModeAuto},
		{"miprov2", MutatorModeMIPROv2},
		{"unknown", "unknown"},
	}
	for _, tc := range tests {
		if got := NormalizeMutatorMode(tc.in); got != tc.want {
			t.Fatalf("NormalizeMutatorMode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsSupportedMutatorMode(t *testing.T) {
	if !IsSupportedMutatorMode(MutatorModeAuto) {
		t.Fatalf("expected %q to be supported", MutatorModeAuto)
	}
	if IsSupportedMutatorMode("hybrid_comb_llm_adaptive") {
		t.Fatalf("expected removed mode %q to be unsupported", "hybrid_comb_llm_adaptive")
	}
	if IsSupportedMutatorMode("hybrid") {
		t.Fatalf("expected removed alias %q to be unsupported", "hybrid")
	}
	if IsSupportedMutatorMode("multi_operator") {
		t.Fatalf("expected removed alias %q to be unsupported", "multi_operator")
	}
}

func TestNormalizeOptimizeStrategy(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", OptimizeStrategyEvolutionary},
		{"evolutionary", OptimizeStrategyEvolutionary},
		{"darwinian", OptimizeStrategyEvolutionary},
		{"dspy", OptimizeStrategyDSPY},
		{"unknown", "unknown"},
	}
	for _, tc := range tests {
		if got := NormalizeOptimizeStrategy(tc.in); got != tc.want {
			t.Fatalf("NormalizeOptimizeStrategy(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIsSupportedOptimizeStrategy(t *testing.T) {
	if !IsSupportedOptimizeStrategy("evolutionary") {
		t.Fatal("expected evolutionary strategy to be supported")
	}
	if !IsSupportedOptimizeStrategy("darwinian") {
		t.Fatal("expected darwinian alias to be supported")
	}
	if !IsSupportedOptimizeStrategy("dspy") {
		t.Fatal("expected dspy strategy to be supported")
	}
	if IsSupportedOptimizeStrategy("unknown") {
		t.Fatal("expected unknown strategy to be unsupported")
	}
}

func TestResolveDSPYOptimizerMode(t *testing.T) {
	promptOnly := &OptimizeSpec{
		Params: []ParamDeclaration{{Path: "nodes[a].data.config.prompt", Type: ParamTypePrompt}},
	}
	if got, err := ResolveDSPYOptimizerMode(MutatorModeMIPROv2, promptOnly); err != nil || got != MutatorModeMIPROv2 {
		t.Fatalf("ResolveDSPYOptimizerMode(miprov2) = (%q, %v), want (%q, nil)", got, err, MutatorModeMIPROv2)
	}
	if got, err := ResolveDSPYOptimizerMode(MutatorModeAuto, promptOnly); err != nil || got != MutatorModeMIPROv2 {
		t.Fatalf("ResolveDSPYOptimizerMode(auto) = (%q, %v), want (%q, nil)", got, err, MutatorModeMIPROv2)
	}
	promptOnly.DSPY = &DSPYConfig{Optimizer: MutatorModeGEPA}
	if got, err := ResolveDSPYOptimizerMode(MutatorModeAuto, promptOnly); err != nil || got != MutatorModeGEPA {
		t.Fatalf("ResolveDSPYOptimizerMode(auto with dspy.optimizer=gepa) = (%q, %v), want (%q, nil)", got, err, MutatorModeGEPA)
	}
	if _, err := ResolveDSPYOptimizerMode("hybrid_comb_llm_adaptive", promptOnly); err == nil {
		t.Fatal("expected removed mutator mode alias to be rejected in dspy strategy")
	}
}

func TestValidateDSPYSpec(t *testing.T) {
	if err := ValidateDSPYSpec(&OptimizeSpec{
		Params: []ParamDeclaration{{Path: "nodes[a].data.config.prompt", Type: ParamTypePrompt}},
	}); err != nil {
		t.Fatalf("ValidateDSPYSpec(prompt-only): %v", err)
	}
	if err := ValidateDSPYSpec(&OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[a].data.config.prompt", Type: ParamTypePrompt},
			{Path: "nodes[a].data.config.temperature", Type: ParamTypeFloat, Range: &ParamRange{Min: 0, Max: 1}},
		},
	}); err == nil {
		t.Fatal("expected non-prompt params to be rejected for dspy strategy")
	}
}

func TestValidateRunConfigurationRejectsGEPAConflictingBudgetControls(t *testing.T) {
	spec := &OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[a].data.config.prompt", Type: ParamTypePrompt},
		},
		DSPY: &DSPYConfig{
			Optimizer:      MutatorModeGEPA,
			Auto:           "light",
			MaxMetricCalls: 1200,
		},
	}
	if err := ValidateRunConfiguration(OptimizeStrategyDSPY, MutatorModeAuto, spec); err == nil {
		t.Fatal("expected conflicting GEPA budget controls to be rejected")
	}
}

func TestValidateRunConfigurationRejectsGEPAMissingBudgetControl(t *testing.T) {
	spec := &OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[a].data.config.prompt", Type: ParamTypePrompt},
		},
		DSPY: &DSPYConfig{
			Optimizer: MutatorModeGEPA,
		},
	}
	if err := ValidateRunConfiguration(OptimizeStrategyDSPY, MutatorModeAuto, spec); err == nil {
		t.Fatal("expected missing GEPA budget controls to be rejected")
	}
}

func TestValidateRunConfigurationRejectsGEPAMissingDSPYConfig(t *testing.T) {
	spec := &OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[a].data.config.prompt", Type: ParamTypePrompt},
		},
	}
	if err := ValidateRunConfiguration(OptimizeStrategyDSPY, MutatorModeGEPA, spec); err == nil {
		t.Fatal("expected GEPA without dspy config to be rejected")
	}
}

func TestValidateRunConfigurationRejectsMIPROAutoWithManualParams(t *testing.T) {
	spec := &OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[a].data.config.prompt", Type: ParamTypePrompt},
		},
		DSPY: &DSPYConfig{
			Optimizer:     MutatorModeMIPROv2,
			Auto:          "light",
			NumCandidates: 8,
		},
	}
	if err := ValidateRunConfiguration(OptimizeStrategyDSPY, MutatorModeAuto, spec); err == nil {
		t.Fatal("expected mipro auto+manual mix to be rejected")
	}
}

func TestValidateRunConfigurationRejectsMIPROPartialManualParams(t *testing.T) {
	spec := &OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[a].data.config.prompt", Type: ParamTypePrompt},
		},
		DSPY: &DSPYConfig{
			Optimizer:     MutatorModeMIPROv2,
			NumCandidates: 8,
		},
	}
	if err := ValidateRunConfiguration(OptimizeStrategyDSPY, MutatorModeAuto, spec); err == nil {
		t.Fatal("expected mipro partial manual params to be rejected")
	}
}

func TestValidateRunConfigurationAllowsMIPROManualParams(t *testing.T) {
	spec := &OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[a].data.config.prompt", Type: ParamTypePrompt},
		},
		DSPY: &DSPYConfig{
			Optimizer:     MutatorModeMIPROv2,
			NumCandidates: 8,
			NumTrials:     32,
		},
	}
	if err := ValidateRunConfiguration(OptimizeStrategyDSPY, MutatorModeAuto, spec); err != nil {
		t.Fatalf("expected valid mipro manual params, got error: %v", err)
	}
}

func TestResolveDSPYRuntimeSettingsMIPROManualMode(t *testing.T) {
	spec := &OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[a].data.config.prompt", Type: ParamTypePrompt},
		},
		DSPY: &DSPYConfig{
			Optimizer:     MutatorModeMIPROv2,
			NumCandidates: 7,
			NumTrials:     19,
		},
	}
	settings := ResolveDSPYRuntimeSettings(spec, MutatorModeMIPROv2, 0)
	if !settings.ManualMode {
		t.Fatal("expected manual mode for explicit num_candidates+num_trials")
	}
	if settings.NumCandidates != 7 || settings.NumTrials != 19 {
		t.Fatalf("expected manual values (7,19), got (%d,%d)", settings.NumCandidates, settings.NumTrials)
	}
}

func TestResolveDSPYRuntimeSettingsMIPROAutoDefaults(t *testing.T) {
	spec := &OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[a].data.config.prompt", Type: ParamTypePrompt},
		},
		DSPY: &DSPYConfig{
			Optimizer: MutatorModeMIPROv2,
			Auto:      "light",
		},
	}
	settings := ResolveDSPYRuntimeSettings(spec, MutatorModeMIPROv2, 0)
	if settings.ManualMode {
		t.Fatal("did not expect manual mode when auto preset is configured")
	}
	if settings.NumCandidates <= 0 || settings.NumTrials <= 0 {
		t.Fatalf("expected positive auto-derived candidates/trials, got (%d,%d)", settings.NumCandidates, settings.NumTrials)
	}
}

func TestAdaptivePromptProbabilityFavorsWinningOperator(t *testing.T) {
	promptBetter := []LearningEntry{
		{MutationType: "auto:llm_prompt", Outcome: "improvement"},
		{MutationType: "auto:llm_prompt", Outcome: "improvement"},
		{MutationType: "auto:llm_prompt", Outcome: "no_change"},
		{MutationType: "auto:combinatorial", Outcome: "regression"},
		{MutationType: "auto:combinatorial", Outcome: "regression"},
	}
	promptProb := adaptivePromptProbability(0.5, promptBetter)
	if promptProb <= 0.5 {
		t.Fatalf("expected prompt probability to increase above 0.5, got %.4f", promptProb)
	}

	combinatorialBetter := []LearningEntry{
		{MutationType: "auto:llm_prompt", Outcome: "regression"},
		{MutationType: "auto:llm_prompt", Outcome: "regression"},
		{MutationType: "auto:combinatorial", Outcome: "improvement"},
		{MutationType: "auto:combinatorial", Outcome: "improvement"},
		{MutationType: "auto:combinatorial", Outcome: "no_change"},
	}
	combinatorialProb := adaptivePromptProbability(0.5, combinatorialBetter)
	if combinatorialProb >= 0.5 {
		t.Fatalf("expected prompt probability to decrease below 0.5, got %.4f", combinatorialProb)
	}
}

func TestIsFitnessBetterPrefersFeasible(t *testing.T) {
	infeasibleHigh := &Fitness{Feasible: false, CompositeScore: 100, AdjustedAccuracy: 0.95, CostPerItem: 0.01}
	feasibleLower := &Fitness{Feasible: true, CompositeScore: 10, AdjustedAccuracy: 0.80, CostPerItem: 0.10}
	if !isFitnessBetter(feasibleLower, infeasibleHigh) {
		t.Fatalf("expected feasible fitness to beat infeasible fitness")
	}
	if isFitnessBetter(infeasibleHigh, feasibleLower) {
		t.Fatalf("expected infeasible fitness not to beat feasible fitness")
	}

	first := &Fitness{Feasible: true, CompositeScore: 2.0, AdjustedAccuracy: 0.7, CostPerItem: 0.1}
	second := &Fitness{Feasible: true, CompositeScore: 2.0, AdjustedAccuracy: 0.7, CostPerItem: 0.09}
	if !isFitnessBetter(second, first) {
		t.Fatalf("expected lower cost to break ties when score and accuracy are equal")
	}
}

func TestSampleFailureCasesByTypeReturnsSingleType(t *testing.T) {
	cases := []FailureCase{
		{ItemID: "a1", Category: "reasoning"},
		{ItemID: "a2", Category: "reasoning"},
		{ItemID: "a3", Category: "reasoning"},
		{ItemID: "b1", Category: "format"},
		{ItemID: "b2", Category: "format"},
	}
	sampled := sampleFailureCasesByType(cases, 3, rand.New(rand.NewSource(7)))
	if len(sampled) == 0 || len(sampled) > 3 {
		t.Fatalf("expected sampled size in [1,3], got %d", len(sampled))
	}

	seenIDs := make(map[string]struct{}, len(sampled))
	sampledType := failureCaseType(sampled[0])
	for _, fc := range sampled {
		if _, exists := seenIDs[fc.ItemID]; exists {
			t.Fatalf("expected unique sampled failure cases, duplicate item_id=%s", fc.ItemID)
		}
		seenIDs[fc.ItemID] = struct{}{}
		if failureCaseType(fc) != sampledType {
			t.Fatalf("expected one sampled failure type, got mixed types (%s vs %s)", sampledType, failureCaseType(fc))
		}
	}
}

func TestSampleFailureCasesRandomReturnsLimitWhenAvailable(t *testing.T) {
	cases := []FailureCase{
		{ItemID: "a1", Category: "reasoning"},
		{ItemID: "a2", Category: "reasoning"},
		{ItemID: "a3", Category: "reasoning"},
		{ItemID: "b1", Category: "format"},
		{ItemID: "b2", Category: "format"},
	}
	sampled := sampleFailureCasesRandom(cases, 4, rand.New(rand.NewSource(7)))
	if got := len(sampled); got != 4 {
		t.Fatalf("expected sampled size 4, got %d", got)
	}

	seenIDs := make(map[string]struct{}, len(sampled))
	for _, fc := range sampled {
		if _, exists := seenIDs[fc.ItemID]; exists {
			t.Fatalf("expected unique sampled failure cases, duplicate item_id=%s", fc.ItemID)
		}
		seenIDs[fc.ItemID] = struct{}{}
	}
}

func TestShouldUseDSPYMinibatch(t *testing.T) {
	runtime := DSPYRuntimeSettings{UseMinibatch: true, MinibatchSize: 35}
	if shouldUseDSPYMinibatch(runtime, 0, 5) != true {
		t.Fatal("expected minibatch to be enabled when run item limit is unconstrained")
	}
	if shouldUseDSPYMinibatch(runtime, 20, 5) {
		t.Fatal("expected minibatch to be disabled when minibatch_size >= run item limit")
	}
	if shouldUseDSPYMinibatch(runtime, 100, 1) {
		t.Fatal("expected minibatch to be disabled for a single candidate")
	}
}

func TestSelectDSPYFullEvalCandidates(t *testing.T) {
	now := time.Now().UTC()
	staged := []*stagedOrganism{
		{Organism: &Organism{ID: "a", CreatedAt: now.Add(-3 * time.Minute), ParamValues: map[string]string{"nodes[a].data.config.prompt": "\"pa\""}}},
		{Organism: &Organism{ID: "b", CreatedAt: now.Add(-2 * time.Minute), ParamValues: map[string]string{"nodes[a].data.config.prompt": "\"pb\""}}},
		{Organism: &Organism{ID: "c", CreatedAt: now.Add(-1 * time.Minute), ParamValues: map[string]string{"nodes[a].data.config.prompt": "\"pc\""}}},
	}
	transient := map[string]transientEvaluatedCandidate{
		"a": {Fitness: &Fitness{Feasible: true, CompositeScore: 0.55, AdjustedAccuracy: 0.55, CostPerItem: 0.10}},
		"b": {Fitness: &Fitness{Feasible: true, CompositeScore: 0.72, AdjustedAccuracy: 0.72, CostPerItem: 0.10}},
		"c": {Fitness: &Fitness{Feasible: true, CompositeScore: 0.62, AdjustedAccuracy: 0.62, CostPerItem: 0.10}},
	}
	selected := selectDSPYFullEvalCandidates(staged, transient, DSPYRuntimeSettings{
		MinibatchFullEvalSteps: 2,
	}, nil, 50, 0)
	if len(selected) != 2 {
		t.Fatalf("expected 2 selected candidates, got %d", len(selected))
	}
	if selected[0].Organism.ID != "b" {
		t.Fatalf("expected highest-scoring candidate first, got %s", selected[0].Organism.ID)
	}
}

func TestSelectDSPYFullEvalCandidatesRespectsMetricBudget(t *testing.T) {
	now := time.Now().UTC()
	staged := []*stagedOrganism{
		{Organism: &Organism{ID: "a", CreatedAt: now.Add(-3 * time.Minute), ParamValues: map[string]string{"nodes[a].data.config.prompt": "\"pa\""}}},
		{Organism: &Organism{ID: "b", CreatedAt: now.Add(-2 * time.Minute), ParamValues: map[string]string{"nodes[a].data.config.prompt": "\"pb\""}}},
		{Organism: &Organism{ID: "c", CreatedAt: now.Add(-1 * time.Minute), ParamValues: map[string]string{"nodes[a].data.config.prompt": "\"pc\""}}},
	}
	transient := map[string]transientEvaluatedCandidate{
		"a": {Fitness: &Fitness{Feasible: true, CompositeScore: 0.55, AdjustedAccuracy: 0.55, CostPerItem: 0.10}},
		"b": {Fitness: &Fitness{Feasible: true, CompositeScore: 0.72, AdjustedAccuracy: 0.72, CostPerItem: 0.10}},
		"c": {Fitness: &Fitness{Feasible: true, CompositeScore: 0.62, AdjustedAccuracy: 0.62, CostPerItem: 0.10}},
	}
	selected := selectDSPYFullEvalCandidates(staged, transient, DSPYRuntimeSettings{
		Optimizer:              MutatorModeGEPA,
		MinibatchFullEvalSteps: 1,
		MaxMetricCalls:         100,
	}, nil, 50, 40)
	if len(selected) != 0 {
		t.Fatalf("expected budget cap to skip full eval candidates, got %d", len(selected))
	}
}

// ---------------------------------------------------------------------------
// classifyOutcome
// ---------------------------------------------------------------------------

func TestClassifyOutcome(t *testing.T) {
	tests := []struct {
		name   string
		f      *Fitness
		delta  float64
		expect string
	}{
		{"improvement", &Fitness{Feasible: true}, 0.5, "improvement"},
		{"regression", &Fitness{Feasible: true}, -0.5, "regression"},
		{"no_change", &Fitness{Feasible: true}, 0.0, "no_change"},
		{"constraint_violation", &Fitness{Feasible: false}, 0.5, "constraint_violation"},
		{"nil fitness", nil, 0.0, "no_change"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyOutcome(tc.f, tc.delta)
			if got != tc.expect {
				t.Errorf("classifyOutcome = %q, want %q", got, tc.expect)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// OptimizationRun helpers
// ---------------------------------------------------------------------------

func TestIsTerminal(t *testing.T) {
	for _, status := range []string{"completed", "failed", "cancelled"} {
		run := &OptimizationRun{Status: status}
		if !run.IsTerminal() {
			t.Errorf("IsTerminal(%q) = false, want true", status)
		}
	}
	for _, status := range []string{"pending", "running", "paused"} {
		run := &OptimizationRun{Status: status}
		if run.IsTerminal() {
			t.Errorf("IsTerminal(%q) = true, want false", status)
		}
	}
}

func TestIsOwnedAndActive(t *testing.T) {
	now := time.Now().UTC()
	recent := now.Add(-10 * time.Second)
	stale := now.Add(-60 * time.Second)

	tests := []struct {
		name string
		run  *OptimizationRun
		want bool
	}{
		{
			name: "active",
			run:  &OptimizationRun{OwnerPID: 1234, OwnerHostname: "host", LastHeartbeatAt: &recent},
			want: true,
		},
		{
			name: "stale",
			run:  &OptimizationRun{OwnerPID: 1234, OwnerHostname: "host", LastHeartbeatAt: &stale},
			want: false,
		},
		{
			name: "no heartbeat",
			run:  &OptimizationRun{OwnerPID: 1234, OwnerHostname: "host"},
			want: false,
		},
		{
			name: "no owner",
			run:  &OptimizationRun{LastHeartbeatAt: &recent},
			want: false,
		},
		{
			name: "nil run",
			run:  nil,
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.run.IsOwnedAndActive(now, 30*time.Second)
			if got != tc.want {
				t.Errorf("IsOwnedAndActive = %v, want %v", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Percentile
// ---------------------------------------------------------------------------

func TestPercentile(t *testing.T) {
	values := []float64{1, 2, 3, 4, 5}

	tests := []struct {
		p    float64
		want float64
	}{
		{0, 1.0},
		{50, 3.0},
		{100, 5.0},
		{75, 4.0},
		{25, 2.0},
	}
	for _, tc := range tests {
		got := percentile(values, tc.p)
		if math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("percentile(%v, %v) = %v, want %v", values, tc.p, got, tc.want)
		}
	}

	// Empty
	if got := percentile(nil, 50); got != 0 {
		t.Errorf("percentile(nil, 50) = %v, want 0", got)
	}

	// Single
	if got := percentile([]float64{42}, 50); got != 42 {
		t.Errorf("percentile([42], 50) = %v, want 42", got)
	}
}

// ---------------------------------------------------------------------------
// Validate param values
// ---------------------------------------------------------------------------

func TestValidateParamValue(t *testing.T) {
	tests := []struct {
		name    string
		decl    ParamDeclaration
		value   interface{}
		wantErr bool
	}{
		{
			name:    "valid model",
			decl:    ParamDeclaration{Type: ParamTypeModel, Candidates: []string{"a", "b"}},
			value:   "a",
			wantErr: false,
		},
		{
			name:    "invalid model",
			decl:    ParamDeclaration{Type: ParamTypeModel, Candidates: []string{"a", "b"}},
			value:   "c",
			wantErr: true,
		},
		{
			name:    "valid float",
			decl:    ParamDeclaration{Type: ParamTypeFloat, Range: &ParamRange{Min: 0, Max: 1, Step: 0.1}},
			value:   0.5,
			wantErr: false,
		},
		{
			name:    "float out of range",
			decl:    ParamDeclaration{Type: ParamTypeFloat, Range: &ParamRange{Min: 0, Max: 1}},
			value:   1.5,
			wantErr: true,
		},
		{
			name:    "valid prompt",
			decl:    ParamDeclaration{Type: ParamTypePrompt},
			value:   "any string",
			wantErr: false,
		},
		{
			name:    "prompt non-string",
			decl:    ParamDeclaration{Type: ParamTypePrompt},
			value:   42,
			wantErr: true,
		},
		{
			name:    "topology rejected",
			decl:    ParamDeclaration{Type: ParamTypeTopology},
			value:   nil,
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateParamValue(tc.decl, tc.value)
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func validSpec() *OptimizeSpec {
	return &OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[agent-a].data.config.model", Type: ParamTypeModel, Candidates: []string{"m1", "m2"}},
			{Path: "nodes[agent-a].data.config.temperature", Type: ParamTypeFloat, Range: &ParamRange{Min: 0, Max: 1, Step: 0.1}},
		},
		Objectives: []Objective{
			{Metric: "accuracy", Direction: "maximize", Weight: 1.0},
		},
		StopPolicy:      StopPolicy{MaxGenerations: 5, BudgetUSD: 1.0},
		PromotionPolicy: PromotionPolicy{MinAccuracyGain: 0.02},
	}
}

func testMaterializeSpec() *OptimizeSpec {
	return &OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[agent-a].data.config.model", Type: ParamTypeModel, Candidates: []string{"model-a", "model-b", "model-c"}},
			{Path: "nodes[agent-a].data.config.temperature", Type: ParamTypeFloat, Range: &ParamRange{Min: 0, Max: 1, Step: 0.1}},
		},
		Objectives: []Objective{
			{Metric: "accuracy", Direction: "maximize", Weight: 1.0},
		},
		StopPolicy:      StopPolicy{MaxGenerations: 5, BudgetUSD: 1.0},
		PromotionPolicy: PromotionPolicy{MinAccuracyGain: 0.02},
	}
}

func testWorkflowJSON() json.RawMessage {
	return json.RawMessage(`{
		"id": "test-wf",
		"name": "Test Workflow",
		"nodes": [
			{
				"id": "agent-a",
				"type": "agent",
				"data": {
					"type": "agent",
					"config": {
						"model": "model-a",
						"temperature": 0.7,
						"maxTokens": 1000,
						"timeoutSeconds": 120
					}
				}
			}
		],
		"edges": []
	}`)
}

func testSeedJSON() json.RawMessage {
	return json.RawMessage(`{
		"id": "test-seed",
		"name": "Test Seed",
		"description": "A test seed",
		"nodes": [
			{
				"id": "agent-a",
				"type": "agent",
				"data": {
					"type": "agent",
					"config": {
						"model": "model-a",
						"temperature": 0.7,
						"maxTokens": 1000,
						"timeoutSeconds": 120
					}
				}
			}
		],
		"edges": [],
		"optimize": {
			"params": [
				{"path": "nodes[agent-a].data.config.model", "type": "model", "candidates": ["model-a", "model-b"]},
				{"path": "nodes[agent-a].data.config.temperature", "type": "float", "range": {"min": 0, "max": 1, "step": 0.1}}
			],
			"locked": [],
			"objectives": [
				{"metric": "accuracy", "direction": "maximize", "weight": 1.0}
			],
			"stop_policy": {
				"max_generations": 5,
				"budget_usd": 1.0,
				"plateau_generations": 3,
				"stability_top_k": 2
			},
			"promotion_policy": {
				"min_accuracy_gain": 0.02,
				"require_generalization_check": false,
				"no_regression_on": ["parse_rate"]
			}
		}
	}`)
}
