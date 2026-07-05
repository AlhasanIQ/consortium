package optimize

import (
	"math/rand"
	"testing"
)

func TestSelectParents_KLessThanOne(t *testing.T) {
	pop := []*Organism{{ID: "a", Fitness: &Fitness{CompositeScore: 1.0}}}
	_, err := SelectParents(pop, nil, 0, DefaultSelectionConfig(), nil)
	if err == nil {
		t.Fatal("expected error for k < 1")
	}
}

func TestSelectParents_SelectsOnlyAvailable(t *testing.T) {
	org := &Organism{ID: "only", Fitness: &Fitness{CompositeScore: 0.5}}
	rng := rand.New(rand.NewSource(42))
	parents, err := SelectParents([]*Organism{org}, nil, 3, DefaultSelectionConfig(), rng)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(parents) != 3 {
		t.Fatalf("expected 3 parents, got %d", len(parents))
	}
	for i, p := range parents {
		if p.ID != "only" {
			t.Errorf("parent[%d] = %q, want %q", i, p.ID, "only")
		}
	}
}

func TestSelectParents_HighScoreBias(t *testing.T) {
	low := &Organism{ID: "low", Fitness: &Fitness{CompositeScore: 0.0}}
	high := &Organism{ID: "high", Fitness: &Fitness{CompositeScore: 1.0}}

	cfg := SelectionConfig{Sharpness: 50, MidpointPercentile: 50, NoveltyWeight: 0}
	rng := rand.New(rand.NewSource(99))

	parents, err := SelectParents([]*Organism{low, high}, nil, 100, cfg, rng)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	highCount := 0
	for _, p := range parents {
		if p.ID == "high" {
			highCount++
		}
	}
	if highCount < 80 {
		t.Errorf("expected strong bias toward high-scoring organism, got %d/100 high selections", highCount)
	}
}

func TestSelectParents_NoveltyPenalty(t *testing.T) {
	a := &Organism{ID: "fresh", Fitness: &Fitness{CompositeScore: 0.5}}
	b := &Organism{ID: "overused", Fitness: &Fitness{CompositeScore: 0.5}}

	childCounts := map[string]int{
		"fresh":    0,
		"overused": 20,
	}

	cfg := SelectionConfig{Sharpness: 10, MidpointPercentile: 50, NoveltyWeight: 5.0}
	rng := rand.New(rand.NewSource(42))

	parents, err := SelectParents([]*Organism{a, b}, childCounts, 200, cfg, rng)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	freshCount := 0
	for _, p := range parents {
		if p.ID == "fresh" {
			freshCount++
		}
	}
	if freshCount < 100 {
		t.Errorf("expected novelty to favor 'fresh' organism, got %d/200 fresh selections", freshCount)
	}
}

func TestSelectParents_NilFitnessHandled(t *testing.T) {
	org := &Organism{ID: "no-fitness"}
	rng := rand.New(rand.NewSource(1))
	parents, err := SelectParents([]*Organism{org}, nil, 1, DefaultSelectionConfig(), rng)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(parents) != 1 || parents[0].ID != "no-fitness" {
		t.Fatalf("expected to select the only available organism")
	}
}

func TestSelectParents_NilOrganismInPopulation(t *testing.T) {
	org := &Organism{ID: "valid", Fitness: &Fitness{CompositeScore: 1.0}}
	rng := rand.New(rand.NewSource(1))
	parents, err := SelectParents([]*Organism{nil, org}, nil, 5, DefaultSelectionConfig(), rng)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(parents) != 5 {
		t.Fatalf("expected 5 parents, got %d", len(parents))
	}
}

func TestSelectParents_DefaultsForBadConfig(t *testing.T) {
	org := &Organism{ID: "a", Fitness: &Fitness{CompositeScore: 0.5}}
	cfg := SelectionConfig{Sharpness: -5, MidpointPercentile: 200, NoveltyWeight: -1}
	rng := rand.New(rand.NewSource(42))

	parents, err := SelectParents([]*Organism{org}, nil, 1, cfg, rng)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(parents) != 1 {
		t.Fatalf("expected 1 parent, got %d", len(parents))
	}
}

func TestSelectParents_DeterministicWithSeed(t *testing.T) {
	pop := make([]*Organism, 10)
	for i := range pop {
		pop[i] = &Organism{
			ID:      string(rune('a' + i)),
			Fitness: &Fitness{CompositeScore: float64(i) / 10.0},
		}
	}

	cfg := DefaultSelectionConfig()
	rng1 := rand.New(rand.NewSource(777))
	rng2 := rand.New(rand.NewSource(777))

	parents1, _ := SelectParents(pop, nil, 20, cfg, rng1)
	parents2, _ := SelectParents(pop, nil, 20, cfg, rng2)

	for i := range parents1 {
		if parents1[i].ID != parents2[i].ID {
			t.Fatalf("selection not deterministic at index %d: %q vs %q", i, parents1[i].ID, parents2[i].ID)
		}
	}
}
