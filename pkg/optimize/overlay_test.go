package optimize

import "testing"

func TestApplySpecOverlay_IncludeExclude(t *testing.T) {
	spec := testOverlaySpec()
	out, err := ApplySpecOverlay(spec, SpecOverlayOptions{
		IncludeSelectors: []string{"type:model", "glob:**.temperature"},
		ExcludeSelectors: []string{"path:nodes[agent-a].data.config.model"},
	})
	if err != nil {
		t.Fatalf("ApplySpecOverlay returned error: %v", err)
	}
	if len(out.Params) != 1 {
		t.Fatalf("expected 1 param after include/exclude, got %d", len(out.Params))
	}
	if got := out.Params[0].Path; got != "nodes[agent-a].data.config.temperature" {
		t.Fatalf("unexpected remaining param path %q", got)
	}
}

func TestApplySpecOverlay_SetThenFreeze(t *testing.T) {
	spec := testOverlaySpec()
	out, err := ApplySpecOverlay(spec, SpecOverlayOptions{
		SetOperations:   []string{"glob:**.temperature=0"},
		FreezeSelectors: []string{"glob:**.temperature"},
	})
	if err != nil {
		t.Fatalf("ApplySpecOverlay returned error: %v", err)
	}
	if len(out.Params) != 1 {
		t.Fatalf("expected temperature to be removed from mutable params, got %d params", len(out.Params))
	}
	if out.Params[0].Path != "nodes[agent-a].data.config.model" {
		t.Fatalf("expected model param to remain mutable, got %q", out.Params[0].Path)
	}
	if _, ok := out.InitialValues["nodes[agent-a].data.config.temperature"]; !ok {
		t.Fatal("expected initial value override for temperature")
	}
	locked := false
	for _, path := range out.Locked {
		if path == "nodes[agent-a].data.config.temperature" {
			locked = true
			break
		}
	}
	if !locked {
		t.Fatal("expected frozen temperature path to be added to locked set")
	}
}

func TestApplySpecOverlay_SetRejectsTypeMismatch(t *testing.T) {
	spec := testOverlaySpec()
	_, err := ApplySpecOverlay(spec, SpecOverlayOptions{
		SetOperations: []string{"type:model=0"},
	})
	if err == nil {
		t.Fatal("expected type mismatch error for model set-knob")
	}
}

func TestApplySpecOverlay_RejectsRemovingAllParams(t *testing.T) {
	spec := testOverlaySpec()
	_, err := ApplySpecOverlay(spec, SpecOverlayOptions{
		ExcludeSelectors: []string{"glob:nodes[*].data.config.*"},
	})
	if err == nil {
		t.Fatal("expected error when overlay removes all params")
	}
}

func testOverlaySpec() *OptimizeSpec {
	return &OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[agent-a].data.config.model", Type: ParamTypeModel, Candidates: []string{"model-a", "model-b"}},
			{Path: "nodes[agent-a].data.config.temperature", Type: ParamTypeFloat, Range: &ParamRange{Min: 0, Max: 1, Step: 0.1}},
		},
		Locked:     []string{"nodes[result].data.config.aggregationMethod"},
		Objectives: []Objective{{Metric: "accuracy", Direction: "maximize", Weight: 1}},
		StopPolicy: StopPolicy{
			MaxGenerations:     2,
			BudgetUSD:          1,
			PlateauGenerations: 1,
			StabilityTopK:      1,
		},
		PromotionPolicy: PromotionPolicy{MinAccuracyGain: 0},
	}
}
