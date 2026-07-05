package optimize

import (
	"math/rand"
	"testing"
)

func TestPrepareMutation_NilRequest(t *testing.T) {
	count, rng, err := prepareMutation(nil, nil)
	if err == nil {
		t.Fatal("expected error for nil request")
	}
	if count != 0 || rng != nil {
		t.Errorf("expected zero count and nil rng on error, got count=%d rng=%v", count, rng)
	}
}

func TestPrepareMutation_NilParent(t *testing.T) {
	req := &MutationRequest{Spec: &OptimizeSpec{}}
	_, _, err := prepareMutation(req, nil)
	if err == nil {
		t.Fatal("expected error for nil parent")
	}
}

func TestPrepareMutation_NilSpec(t *testing.T) {
	req := &MutationRequest{Parent: &Organism{}}
	_, _, err := prepareMutation(req, nil)
	if err == nil {
		t.Fatal("expected error for nil spec")
	}
}

func TestPrepareMutation_CountClamped(t *testing.T) {
	tests := []struct {
		name      string
		count     int
		wantCount int
	}{
		{"zero count becomes 1", 0, 1},
		{"negative count becomes 1", -5, 1},
		{"positive count preserved", 3, 3},
		{"one preserved", 1, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := &MutationRequest{
				Parent: &Organism{},
				Spec:   &OptimizeSpec{},
				Count:  tc.count,
			}
			count, _, err := prepareMutation(req, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if count != tc.wantCount {
				t.Errorf("count = %d, want %d", count, tc.wantCount)
			}
		})
	}
}

func TestPrepareMutation_ReusesProvidedRng(t *testing.T) {
	req := &MutationRequest{
		Parent: &Organism{},
		Spec:   &OptimizeSpec{},
		Count:  1,
	}
	provided := rand.New(rand.NewSource(42))
	_, returned, err := prepareMutation(req, provided)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if returned != provided {
		t.Error("expected returned rng to be the same pointer as provided")
	}
}

func TestPrepareMutation_GeneratesRngWhenNil(t *testing.T) {
	req := &MutationRequest{
		Parent: &Organism{},
		Spec:   &OptimizeSpec{},
		Count:  1,
	}
	_, rng, err := prepareMutation(req, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rng == nil {
		t.Error("expected a non-nil rng when none was provided")
	}
}
