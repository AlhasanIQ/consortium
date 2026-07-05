package storage

import (
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/optimize"
	"github.com/google/uuid"
)

func TestOptimizationStore_RunRoundTrip(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage failed: %v", err)
	}
	defer store.Close()

	runID := uuid.NewString()
	run := &optimize.OptimizationRun{
		ID:             runID,
		WorkflowID:     "reasoning-judge-pick-cheap",
		Benchmark:      "global-mmlu-lite",
		Split:          "dev",
		ItemLimit:      10,
		Concurrency:    5,
		Spec:           &optimize.OptimizeSpec{Params: []optimize.ParamDeclaration{{Path: "nodes[a].data.config.model", Type: optimize.ParamTypeModel, Candidates: []string{"m1"}}}, Objectives: []optimize.Objective{{Metric: "accuracy", Direction: "maximize", Weight: 1}}, StopPolicy: optimize.StopPolicy{MaxGenerations: 2, BudgetUSD: 1, PlateauGenerations: 1, StabilityTopK: 1}, PromotionPolicy: optimize.PromotionPolicy{}},
		Strategy:       "evolutionary",
		PopulationSize: 3,
		ClaudeModel:    "opus",
		TotalBudgetUSD: 1,
		SpentUSD:       0.2,
		Status:         "running",
		Generation:     1,
		TotalOrganisms: 2,
	}

	if err := store.CreateOptimizationRun(run); err != nil {
		t.Fatalf("CreateOptimizationRun failed: %v", err)
	}
	loaded, err := store.GetOptimizationRun(runID)
	if err != nil {
		t.Fatalf("GetOptimizationRun failed: %v", err)
	}
	if loaded.ID != runID {
		t.Fatalf("expected run id %s, got %s", runID, loaded.ID)
	}
	if loaded.Spec == nil || len(loaded.Spec.Params) != 1 {
		t.Fatalf("expected spec params to round-trip")
	}

	if err := store.UpdateOptimizationRunProgress(runID, 2, "org-best", &optimize.Fitness{CompositeScore: 1.23}, 0.9, 9, 42); err != nil {
		t.Fatalf("UpdateOptimizationRunProgress failed: %v", err)
	}
	loaded, err = store.GetOptimizationRun(runID)
	if err != nil {
		t.Fatalf("GetOptimizationRun failed: %v", err)
	}
	if loaded.Generation != 2 || loaded.TotalOrganisms != 9 {
		t.Fatalf("unexpected run progress: %+v", loaded)
	}
	if loaded.BestOrganismID != "org-best" {
		t.Fatalf("expected best organism id org-best, got %s", loaded.BestOrganismID)
	}
	if loaded.DSPYMetricCallsUsed != 42 {
		t.Fatalf("expected dspy metric calls used 42, got %d", loaded.DSPYMetricCallsUsed)
	}
}

func TestOptimizationStore_OrganismRoundTrip(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage failed: %v", err)
	}
	defer store.Close()

	runID := uuid.NewString()
	run := &optimize.OptimizationRun{
		ID:             runID,
		WorkflowID:     "wf",
		Benchmark:      "bench",
		Split:          "dev",
		Spec:           &optimize.OptimizeSpec{Params: []optimize.ParamDeclaration{{Path: "nodes[a].data.config.model", Type: optimize.ParamTypeModel, Candidates: []string{"m1"}}}, Objectives: []optimize.Objective{{Metric: "accuracy", Direction: "maximize", Weight: 1}}, StopPolicy: optimize.StopPolicy{MaxGenerations: 2, BudgetUSD: 1, PlateauGenerations: 1, StabilityTopK: 1}, PromotionPolicy: optimize.PromotionPolicy{}},
		Strategy:       "evolutionary",
		PopulationSize: 2,
		TotalBudgetUSD: 1,
		Status:         "running",
	}
	if err := store.CreateOptimizationRun(run); err != nil {
		t.Fatalf("CreateOptimizationRun failed: %v", err)
	}

	orgID := uuid.NewString()
	org := &optimize.Organism{
		ID:           orgID,
		OptRunID:     runID,
		Generation:   0,
		ParentIDs:    []string{},
		ParamValues:  map[string]string{"nodes[a].data.config.model": `"m1"`},
		WorkflowJSON: []byte(`{"id":"wf"}`),
		MutationType: "seed",
		MutationLog:  "baseline",
		CreatedAt:    time.Now().UTC(),
	}
	if err := store.CreateOptimizationOrganism(org); err != nil {
		t.Fatalf("CreateOptimizationOrganism failed: %v", err)
	}
	changes := []optimize.ParamChange{{Path: "nodes[a].data.config.model", OldValue: `"m0"`, NewValue: `"m1"`, Reason: "test"}}
	if err := store.CreateOptimizationParamChanges(orgID, changes); err != nil {
		t.Fatalf("CreateOptimizationParamChanges failed: %v", err)
	}

	fitness := &optimize.Fitness{AdjustedAccuracy: 0.8, ParseRate: 0.9, CostPerItem: 0.01, CompositeScore: 2.5, Feasible: true}
	if err := store.UpdateOptimizationOrganismFitness(orgID, "bench-run-1", fitness); err != nil {
		t.Fatalf("UpdateOptimizationOrganismFitness failed: %v", err)
	}
	loaded, err := store.GetOptimizationOrganism(orgID)
	if err != nil {
		t.Fatalf("GetOptimizationOrganism failed: %v", err)
	}
	if loaded.BenchRunID != "bench-run-1" {
		t.Fatalf("expected bench run id bench-run-1, got %s", loaded.BenchRunID)
	}
	if loaded.Fitness == nil || loaded.Fitness.CompositeScore != 2.5 {
		t.Fatalf("expected fitness to round-trip, got %+v", loaded.Fitness)
	}

	loadedChanges, err := store.GetOptimizationParamChanges(orgID)
	if err != nil {
		t.Fatalf("GetOptimizationParamChanges failed: %v", err)
	}
	if len(loadedChanges) != 1 {
		t.Fatalf("expected 1 param change, got %d", len(loadedChanges))
	}

	entry := &optimize.LearningEntry{Generation: 1, OrganismID: orgID, ParentID: orgID, MutationType: "hybrid", Description: "test", Outcome: "improvement", FitnessDelta: 0.5}
	if err := store.AppendOptimizationLearningEntry(runID, entry); err != nil {
		t.Fatalf("AppendOptimizationLearningEntry failed: %v", err)
	}
	learning, err := store.GetOptimizationLearningLog(runID, 10)
	if err != nil {
		t.Fatalf("GetOptimizationLearningLog failed: %v", err)
	}
	if len(learning) != 1 {
		t.Fatalf("expected 1 learning entry, got %d", len(learning))
	}
}

func TestOptimizationStore_GetBestOrganismsPrefersFeasible(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage failed: %v", err)
	}
	defer store.Close()

	runID := uuid.NewString()
	run := &optimize.OptimizationRun{
		ID:             runID,
		WorkflowID:     "wf",
		Benchmark:      "bench",
		Split:          "dev",
		Spec:           &optimize.OptimizeSpec{Params: []optimize.ParamDeclaration{{Path: "nodes[a].data.config.model", Type: optimize.ParamTypeModel, Candidates: []string{"m1"}}}, Objectives: []optimize.Objective{{Metric: "accuracy", Direction: "maximize", Weight: 1}}, StopPolicy: optimize.StopPolicy{MaxGenerations: 2, BudgetUSD: 1, PlateauGenerations: 1, StabilityTopK: 1}, PromotionPolicy: optimize.PromotionPolicy{}},
		Strategy:       "evolutionary",
		PopulationSize: 2,
		TotalBudgetUSD: 1,
		Status:         "running",
	}
	if err := store.CreateOptimizationRun(run); err != nil {
		t.Fatalf("CreateOptimizationRun failed: %v", err)
	}

	now := time.Now().UTC()
	infeasibleID := uuid.NewString()
	feasibleID := uuid.NewString()

	infeasible := &optimize.Organism{
		ID:           infeasibleID,
		OptRunID:     runID,
		Generation:   1,
		ParentIDs:    []string{},
		ParamValues:  map[string]string{"nodes[a].data.config.model": `"m1"`},
		WorkflowJSON: []byte(`{"id":"wf"}`),
		MutationType: "combinatorial",
		MutationLog:  "infeasible candidate",
		CreatedAt:    now,
	}
	feasible := &optimize.Organism{
		ID:           feasibleID,
		OptRunID:     runID,
		Generation:   1,
		ParentIDs:    []string{},
		ParamValues:  map[string]string{"nodes[a].data.config.model": `"m1"`},
		WorkflowJSON: []byte(`{"id":"wf"}`),
		MutationType: "combinatorial",
		MutationLog:  "feasible candidate",
		CreatedAt:    now.Add(1 * time.Second),
	}
	if err := store.CreateOptimizationOrganism(infeasible); err != nil {
		t.Fatalf("CreateOptimizationOrganism infeasible failed: %v", err)
	}
	if err := store.CreateOptimizationOrganism(feasible); err != nil {
		t.Fatalf("CreateOptimizationOrganism feasible failed: %v", err)
	}

	if err := store.UpdateOptimizationOrganismFitness(infeasibleID, "bench-run-infeasible", &optimize.Fitness{
		AdjustedAccuracy: 0.95,
		ParseRate:        0.90,
		CostPerItem:      0.05,
		CompositeScore:   100,
		Feasible:         false,
	}); err != nil {
		t.Fatalf("UpdateOptimizationOrganismFitness infeasible failed: %v", err)
	}
	if err := store.UpdateOptimizationOrganismFitness(feasibleID, "bench-run-feasible", &optimize.Fitness{
		AdjustedAccuracy: 0.80,
		ParseRate:        0.90,
		CostPerItem:      0.05,
		CompositeScore:   10,
		Feasible:         true,
	}); err != nil {
		t.Fatalf("UpdateOptimizationOrganismFitness feasible failed: %v", err)
	}

	best, err := store.GetBestOptimizationOrganisms(runID, 1)
	if err != nil {
		t.Fatalf("GetBestOptimizationOrganisms failed: %v", err)
	}
	if len(best) != 1 {
		t.Fatalf("expected 1 best organism, got %d", len(best))
	}
	if best[0].ID != feasibleID {
		t.Fatalf("expected feasible organism %s to be selected, got %s", feasibleID, best[0].ID)
	}
}
