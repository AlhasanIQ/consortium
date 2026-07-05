package optimize

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
)

func (l *Loop) selectParents(ctx context.Context, run *OptimizationRun) ([]*Organism, error) {
	population, err := l.Store.GetBestOrganisms(ctx, run.ID, run.PopulationSize*3)
	if err != nil {
		return nil, fmt.Errorf("load selection population: %w", err)
	}
	all, err := l.Store.GetAllOrganisms(ctx, run.ID)
	if err != nil {
		return nil, fmt.Errorf("load population: %w", err)
	}
	if len(population) == 0 {
		population = all
	}
	if len(population) == 0 {
		return nil, fmt.Errorf("no organisms available for parent selection")
	}
	eligible := filterEligibleParents(population)
	if len(eligible) == 0 {
		eligible = filterEligibleParents(all)
	}
	if len(eligible) == 0 {
		return nil, fmt.Errorf("no evaluated organisms available for parent selection")
	}
	if feasible := filterFeasibleParents(eligible); len(feasible) > 0 {
		eligible = feasible
	}
	if hasPromptParams(run.Spec) {
		if withFailures := filterParentsWithFailures(eligible); len(withFailures) > 0 {
			eligible = withFailures
		}
	}

	childCounts := make(map[string]int)
	for _, organism := range all {
		for _, parentID := range organism.ParentIDs {
			childCounts[parentID]++
		}
	}

	rng := l.Rand
	if rng == nil {
		rng = rand.New(rand.NewSource(rand.Int63())) //nolint:gosec // non-cryptographic sampling
	}
	return SelectParents(eligible, childCounts, run.PopulationSize, l.Selection, rng)
}

func (l *Loop) currentBest(ctx context.Context, run *OptimizationRun) (*Organism, error) {
	all, err := l.Store.GetAllOrganisms(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	var best *Organism
	for _, organism := range all {
		if !isEligibleParent(organism) {
			continue
		}
		if best == nil || isOrganismBetter(organism, best) {
			best = organism
		}
	}
	if best == nil {
		return nil, nil
	}
	return best, nil
}

func (l *Loop) childrenPerParent(run *OptimizationRun, withoutImprovement int) int {
	if run == nil {
		return 1
	}
	children := run.ChildrenPerParent
	if children < 1 {
		children = 1
	}
	if run.AdaptiveFanout {
		if run.TotalBudgetUSD > 0 {
			remainingRatio := (run.TotalBudgetUSD - run.SpentUSD) / run.TotalBudgetUSD
			if remainingRatio > 0.30 && run.Spec != nil && run.Spec.StopPolicy.PlateauGenerations > 0 &&
				withoutImprovement >= run.Spec.StopPolicy.PlateauGenerations && children < 2 {
				children = 2
			}
		}
	}
	if maxChildren := run.MaxChildrenPerGeneration; maxChildren > 0 && children > maxChildren {
		children = maxChildren
	}
	return children
}

func (l *Loop) loadParentMap(ctx context.Context, runID string) (map[string]*Organism, error) {
	all, err := l.Store.GetAllOrganisms(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("load organisms for parent map: %w", err)
	}
	byID := make(map[string]*Organism, len(all))
	for _, organism := range all {
		if organism == nil {
			continue
		}
		byID[organism.ID] = organism
	}
	parentsByChild := make(map[string]*Organism)
	for _, organism := range all {
		if organism == nil || len(organism.ParentIDs) == 0 {
			continue
		}
		if parent := byID[organism.ParentIDs[0]]; parent != nil {
			parentsByChild[organism.ID] = parent
		}
	}
	return parentsByChild, nil
}

func filterEligibleParents(population []*Organism) []*Organism {
	eligible := make([]*Organism, 0, len(population))
	for _, organism := range population {
		if isEligibleParent(organism) {
			eligible = append(eligible, organism)
		}
	}
	return eligible
}

func filterFeasibleParents(population []*Organism) []*Organism {
	feasible := make([]*Organism, 0, len(population))
	for _, organism := range population {
		if organism != nil && organism.Fitness != nil && organism.Fitness.Feasible {
			feasible = append(feasible, organism)
		}
	}
	return feasible
}

func filterParentsWithFailures(population []*Organism) []*Organism {
	withFailures := make([]*Organism, 0, len(population))
	for _, organism := range population {
		if organism != nil && organism.Fitness != nil && organism.Fitness.FailedItems > 0 {
			withFailures = append(withFailures, organism)
		}
	}
	return withFailures
}

func hasPromptParams(spec *OptimizeSpec) bool {
	if spec == nil {
		return false
	}
	for _, declaration := range spec.Params {
		if declaration.Type == ParamTypePrompt {
			return true
		}
	}
	return false
}

func isEligibleParent(organism *Organism) bool {
	if organism == nil || organism.Fitness == nil {
		return false
	}
	if organism.EvaluatedAt == nil {
		return false
	}
	return strings.TrimSpace(organism.BenchRunID) != ""
}

func isOrganismBetter(candidate *Organism, incumbent *Organism) bool {
	if candidate == nil || candidate.Fitness == nil {
		return false
	}
	if incumbent == nil || incumbent.Fitness == nil {
		return true
	}
	if isFitnessBetter(candidate.Fitness, incumbent.Fitness) {
		return true
	}
	if isFitnessBetter(incumbent.Fitness, candidate.Fitness) {
		return false
	}
	return candidate.CreatedAt.Before(incumbent.CreatedAt)
}

func isFitnessBetter(candidate *Fitness, incumbent *Fitness) bool {
	if candidate == nil {
		return false
	}
	if incumbent == nil {
		return true
	}
	if candidate.Feasible != incumbent.Feasible {
		return candidate.Feasible
	}
	if candidate.CompositeScore > incumbent.CompositeScore+1e-9 {
		return true
	}
	if candidate.CompositeScore < incumbent.CompositeScore-1e-9 {
		return false
	}
	if candidate.AdjustedAccuracy > incumbent.AdjustedAccuracy+1e-9 {
		return true
	}
	if candidate.AdjustedAccuracy < incumbent.AdjustedAccuracy-1e-9 {
		return false
	}
	if candidate.CostPerItem+1e-12 < incumbent.CostPerItem {
		return true
	}
	return false
}
