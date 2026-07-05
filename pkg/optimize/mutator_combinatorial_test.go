package optimize

import (
	"context"
	"encoding/json"
	"math/rand"
	"strings"
	"testing"
)

// --- decodeIfPresent ---

func TestDecodeIfPresent(t *testing.T) {
	tests := []struct {
		name    string
		encoded string
		exists  bool
		wantNil bool
		wantVal interface{}
		wantErr bool
	}{
		{
			name:    "not present returns nil",
			encoded: "",
			exists:  false,
			wantNil: true,
		},
		{
			name:    "empty string with exists=true returns nil",
			encoded: "   ",
			exists:  true,
			wantNil: true,
		},
		{
			name:    "valid JSON string",
			encoded: `"hello"`,
			exists:  true,
			wantVal: "hello",
		},
		{
			name:    "valid JSON number",
			encoded: `3.14`,
			exists:  true,
			wantVal: 3.14,
		},
		{
			name:    "invalid JSON returns error",
			encoded: `not-json`,
			exists:  true,
			wantErr: true,
		},
		{
			name:    "null JSON value",
			encoded: `null`,
			exists:  true,
			wantNil: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			val, err := decodeIfPresent(tc.encoded, tc.exists)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantNil {
				if val != nil {
					t.Errorf("expected nil, got %v", val)
				}
				return
			}
			if val != tc.wantVal {
				t.Errorf("val = %v (%T), want %v (%T)", val, val, tc.wantVal, tc.wantVal)
			}
		})
	}
}

// --- collectMutationGroup ---

func TestCollectMutationGroup_NoGroup(t *testing.T) {
	selected := ParamDeclaration{Path: "nodes[a].data.config.model", Type: ParamTypeModel}
	all := []ParamDeclaration{
		selected,
		{Path: "nodes[b].data.config.temp", Type: ParamTypeFloat},
	}
	group := collectMutationGroup(selected, all)
	if len(group) != 1 {
		t.Fatalf("expected 1 member (no group), got %d", len(group))
	}
	if group[0].Path != selected.Path {
		t.Errorf("expected selected param, got %q", group[0].Path)
	}
}

func TestCollectMutationGroup_WhitespaceGroupTreatedAsEmpty(t *testing.T) {
	selected := ParamDeclaration{Path: "nodes[a].data.config.model", Type: ParamTypeModel, Group: "   "}
	all := []ParamDeclaration{selected}
	group := collectMutationGroup(selected, all)
	if len(group) != 1 {
		t.Fatalf("expected 1 (whitespace group treated as empty), got %d", len(group))
	}
}

func TestCollectMutationGroup_WithGroup(t *testing.T) {
	all := []ParamDeclaration{
		{Path: "nodes[a].data.config.model", Type: ParamTypeModel, Group: "alpha"},
		{Path: "nodes[b].data.config.model", Type: ParamTypeModel, Group: "alpha"},
		{Path: "nodes[c].data.config.temp", Type: ParamTypeFloat, Group: "beta"},
	}
	selected := all[0]
	group := collectMutationGroup(selected, all)
	if len(group) != 2 {
		t.Fatalf("expected 2 members in group 'alpha', got %d", len(group))
	}
	for _, m := range group {
		if m.Group != "alpha" {
			t.Errorf("unexpected group member: %q (group=%q)", m.Path, m.Group)
		}
	}
}

func TestCollectMutationGroup_GroupNotFoundFallsBack(t *testing.T) {
	// Selected has a group but no other param shares it → returns just selected.
	selected := ParamDeclaration{Path: "nodes[a].data.config.model", Type: ParamTypeModel, Group: "solo"}
	all := []ParamDeclaration{
		selected,
		{Path: "nodes[b].data.config.temp", Type: ParamTypeFloat, Group: "other"},
	}
	group := collectMutationGroup(selected, all)
	if len(group) != 1 {
		t.Fatalf("expected 1 fallback member, got %d", len(group))
	}
}

// --- sampleNewValue ---

func TestSampleNewValue_EnumAvoidsCurrent(t *testing.T) {
	m := &CombinatorialMutator{}
	rng := rand.New(rand.NewSource(42))
	decl := ParamDeclaration{
		Type:       ParamTypeEnum,
		Candidates: []string{"a", "b", "c"},
	}
	old := "a"
	// Run many times — should never return "a" since alternatives exist.
	for i := 0; i < 50; i++ {
		val, err := m.sampleNewValue(decl, old, rng)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val == "a" {
			t.Errorf("sampleNewValue returned same value as old (%q)", old)
		}
	}
}

func TestSampleNewValue_EnumSingleCandidate(t *testing.T) {
	m := &CombinatorialMutator{}
	rng := rand.New(rand.NewSource(1))
	decl := ParamDeclaration{
		Type:       ParamTypeEnum,
		Candidates: []string{"only"},
	}
	val, err := m.sampleNewValue(decl, "only", rng)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "only" {
		t.Errorf("expected %q, got %v", "only", val)
	}
}

func TestSampleNewValue_EnumNoCandidatesError(t *testing.T) {
	m := &CombinatorialMutator{}
	rng := rand.New(rand.NewSource(1))
	decl := ParamDeclaration{Type: ParamTypeEnum, Candidates: nil}
	_, err := m.sampleNewValue(decl, nil, rng)
	if err == nil {
		t.Fatal("expected error for empty candidates")
	}
}

func TestSampleNewValue_FloatNoRange(t *testing.T) {
	m := &CombinatorialMutator{}
	rng := rand.New(rand.NewSource(1))
	decl := ParamDeclaration{Type: ParamTypeFloat}
	_, err := m.sampleNewValue(decl, nil, rng)
	if err == nil {
		t.Fatal("expected error for missing range")
	}
}

func TestSampleNewValue_FloatWithStep(t *testing.T) {
	m := &CombinatorialMutator{}
	rng := rand.New(rand.NewSource(7))
	decl := ParamDeclaration{
		Type:  ParamTypeFloat,
		Range: &ParamRange{Min: 0.0, Max: 1.0, Step: 0.25},
	}
	allowed := map[float64]bool{0.0: true, 0.25: true, 0.5: true, 0.75: true, 1.0: true}
	for i := 0; i < 20; i++ {
		val, err := m.sampleNewValue(decl, nil, rng)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		f, ok := val.(float64)
		if !ok {
			t.Fatalf("expected float64, got %T", val)
		}
		inAllowed := false
		for a := range allowed {
			if abs64(f-a) < 1e-9 {
				inAllowed = true
				break
			}
		}
		if !inAllowed {
			t.Errorf("sampled value %v not in allowed set %v", f, allowed)
		}
	}
}

func TestSampleNewValue_FloatContinuous(t *testing.T) {
	m := &CombinatorialMutator{}
	rng := rand.New(rand.NewSource(99))
	decl := ParamDeclaration{
		Type:  ParamTypeFloat,
		Range: &ParamRange{Min: 0.0, Max: 1.0},
	}
	for i := 0; i < 20; i++ {
		val, err := m.sampleNewValue(decl, nil, rng)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		f, ok := val.(float64)
		if !ok {
			t.Fatalf("expected float64, got %T", val)
		}
		if f < 0.0 || f > 1.0 {
			t.Errorf("sampled value %v out of range [0,1]", f)
		}
	}
}

func TestSampleNewValue_IntNoRange(t *testing.T) {
	m := &CombinatorialMutator{}
	rng := rand.New(rand.NewSource(1))
	decl := ParamDeclaration{Type: ParamTypeInt}
	_, err := m.sampleNewValue(decl, nil, rng)
	if err == nil {
		t.Fatal("expected error for missing range")
	}
}

func TestSampleNewValue_IntWithStep(t *testing.T) {
	m := &CombinatorialMutator{}
	rng := rand.New(rand.NewSource(3))
	decl := ParamDeclaration{
		Type:  ParamTypeInt,
		Range: &ParamRange{Min: 0, Max: 10, Step: 5},
	}
	// Candidates: 0, 5, 10
	allowed := map[int]bool{0: true, 5: true, 10: true}
	for i := 0; i < 20; i++ {
		val, err := m.sampleNewValue(decl, nil, rng)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		n, ok := val.(int)
		if !ok {
			t.Fatalf("expected int, got %T (%v)", val, val)
		}
		if !allowed[n] {
			t.Errorf("sampled int %v not in allowed set %v", n, allowed)
		}
	}
}

func TestSampleNewValue_IntAvoidsCurrent(t *testing.T) {
	m := &CombinatorialMutator{}
	rng := rand.New(rand.NewSource(42))
	decl := ParamDeclaration{
		Type:  ParamTypeInt,
		Range: &ParamRange{Min: 1, Max: 3, Step: 1},
	}
	// old = 2, candidates = [1,2,3]; should never return 2 since alternatives exist.
	for i := 0; i < 50; i++ {
		val, err := m.sampleNewValue(decl, float64(2), rng)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val.(int) == 2 {
			t.Error("sampleNewValue returned same int value as old")
		}
	}
}

func TestSampleNewValue_IntInvalidRange(t *testing.T) {
	m := &CombinatorialMutator{}
	rng := rand.New(rand.NewSource(1))
	decl := ParamDeclaration{
		Type:  ParamTypeInt,
		Range: &ParamRange{Min: 10, Max: 5},
	}
	_, err := m.sampleNewValue(decl, nil, rng)
	if err == nil {
		t.Fatal("expected error for max < min")
	}
}

func TestSampleNewValue_UnsupportedType(t *testing.T) {
	m := &CombinatorialMutator{}
	rng := rand.New(rand.NewSource(1))
	decl := ParamDeclaration{Type: ParamTypePrompt}
	_, err := m.sampleNewValue(decl, nil, rng)
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
	if !strings.Contains(err.Error(), "unsupported combinatorial type") {
		t.Errorf("error %q should mention 'unsupported combinatorial type'", err.Error())
	}
}

// --- CombinatorialMutator.Mutate integration ---

func TestCombinatorialMutator_Mutate_Basic(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	m := &CombinatorialMutator{Rand: rng}

	parent := &Organism{
		ID:          "parent",
		ParamValues: map[string]string{"nodes[a].data.config.model": `"gpt-4"`},
	}
	spec := &OptimizeSpec{
		Params: []ParamDeclaration{
			{
				Path:       "nodes[a].data.config.model",
				Type:       ParamTypeModel,
				Candidates: []string{"gpt-4", "claude-3"},
			},
		},
	}
	req := &MutationRequest{Parent: parent, Spec: spec, Count: 3}
	results, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Organism == nil {
			t.Error("result organism should not be nil")
		}
		if len(r.Changes) == 0 {
			t.Error("expected at least one change per result")
		}
	}
}

func TestCombinatorialMutator_Mutate_NoParams(t *testing.T) {
	m := &CombinatorialMutator{}
	parent := &Organism{ID: "parent", ParamValues: map[string]string{}}
	spec := &OptimizeSpec{
		Params: []ParamDeclaration{
			{Path: "nodes[a].data.config.prompt", Type: ParamTypePrompt},
		},
	}
	req := &MutationRequest{Parent: parent, Spec: spec, Count: 1}
	_, err := m.Mutate(context.Background(), req)
	if err == nil {
		t.Fatal("expected error when no combinatorial params exist")
	}
	if !strings.Contains(err.Error(), "no combinatorial params") {
		t.Errorf("error %q should mention 'no combinatorial params'", err.Error())
	}
}

func TestCombinatorialMutator_Mutate_ChangesAreSorted(t *testing.T) {
	// Use grouped params to force multiple changes and verify sorting.
	rng := rand.New(rand.NewSource(1))
	m := &CombinatorialMutator{Rand: rng}

	parent := &Organism{
		ID: "parent",
		ParamValues: map[string]string{
			"nodes[a].data.config.model": `"gpt-4"`,
			"nodes[b].data.config.model": `"gpt-4"`,
		},
	}
	spec := &OptimizeSpec{
		Params: []ParamDeclaration{
			{
				Path:       "nodes[a].data.config.model",
				Type:       ParamTypeModel,
				Candidates: []string{"gpt-4", "claude-3"},
				Group:      "models",
			},
			{
				Path:       "nodes[b].data.config.model",
				Type:       ParamTypeModel,
				Candidates: []string{"gpt-4", "claude-3"},
				Group:      "models",
			},
		},
	}
	req := &MutationRequest{Parent: parent, Spec: spec, Count: 1}
	results, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	changes := results[0].Changes
	for i := 1; i < len(changes); i++ {
		if changes[i].Path < changes[i-1].Path {
			t.Errorf("changes not sorted at index %d: %q < %q", i, changes[i].Path, changes[i-1].Path)
		}
	}
}

func TestCombinatorialMutator_Mutate_NilRequest(t *testing.T) {
	m := &CombinatorialMutator{}
	_, err := m.Mutate(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil request")
	}
}

// --- JSON encoding round-trip for new values ---

func TestCombinatorialMutator_NewValueJSONEncoded(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	m := &CombinatorialMutator{Rand: rng}

	parent := &Organism{
		ID:          "parent",
		ParamValues: map[string]string{"nodes[x].data.config.temp": "0.5"},
	}
	spec := &OptimizeSpec{
		Params: []ParamDeclaration{
			{
				Path:  "nodes[x].data.config.temp",
				Type:  ParamTypeFloat,
				Range: &ParamRange{Min: 0.0, Max: 1.0, Step: 0.5},
			},
		},
	}
	results, err := m.Mutate(context.Background(), &MutationRequest{Parent: parent, Spec: spec, Count: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected a result")
	}
	newEncoded := results[0].Changes[0].NewValue
	var parsed float64
	if err := json.Unmarshal([]byte(newEncoded), &parsed); err != nil {
		t.Errorf("new value %q is not valid JSON float: %v", newEncoded, err)
	}
}

func abs64(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
