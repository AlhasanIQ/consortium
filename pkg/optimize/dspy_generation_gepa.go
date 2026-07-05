package optimize

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"time"
)

func (l *Loop) runDSPYGEPAGeneration(
	ctx context.Context,
	run *OptimizationRun,
	baseWorkflowJSON json.RawMessage,
	generation int,
	parent *Organism,
	runtime DSPYRuntimeSettings,
	metricCallsUsed *int,
) (int, error) {
	if run == nil || run.Spec == nil || parent == nil {
		return 0, fmt.Errorf("run, spec, and parent are required")
	}
	rng := l.loopRand()
	trialCount := max(runtime.NumTrials, 1)
	if runtime.MaxMetricCalls > 0 {
		// GEPA in DSPy is budget-driven (max_metric_calls), not trial-capped.
		// Keep a large safety cap to avoid unbounded loops if verification keeps
		// rejecting candidates without consuming metric budget.
		trialCount = max(runtime.MaxMetricCalls+max(runtime.MinibatchFullEvalSteps, 1)*8, 64)
	}
	fullEvalSteps := max(runtime.MinibatchFullEvalSteps, 1)
	reflectionLimit := max(runtime.ReflectionMinibatchSize, max(12, runtime.MinibatchSize))
	baseFailures, _ := l.fetchParentFailures(ctx, run, parent, reflectionLimit)

	gepa := &GEPAMutator{
		Model: strings.TrimSpace(run.ClaudeModel),
		Rand:  rng,
	}
	if existing, ok := l.Mutator.(*GEPAMutator); ok && existing != nil {
		gepa = existing
		if strings.TrimSpace(gepa.Model) == "" {
			gepa.Model = strings.TrimSpace(run.ClaudeModel)
		}
		if gepa.Rand == nil {
			gepa.Rand = rng
		}
	}

	states := make([]*dspyCandidateState, 0, min(trialCount+1, 1024))
	states = append(states, &dspyCandidateState{
		organism: parent,
		score:    parentScore(parent),
		fullEval: true,
	})

	createdCount := 0
	noMetricProgress := 0
	batchSize := max(runtime.BatchSize, 1)

	for trial := 0; trial < trialCount; {
		if err := ctx.Err(); err != nil {
			return createdCount, err
		}
		if run.Status != "running" || run.SpentUSD >= run.TotalBudgetUSD {
			break
		}
		if runtime.MaxMetricCalls > 0 && metricCallsUsed != nil && *metricCallsUsed >= runtime.MaxMetricCalls {
			break
		}
		callsBefore := 0
		if metricCallsUsed != nil {
			callsBefore = *metricCallsUsed
		}

		// Determine chunk size for this iteration.
		chunkEnd := min(trial+batchSize, trialCount)
		chunkSize := chunkEnd - trial

		// 1. Generate N mutations, selecting parents from current Pareto frontier.
		batchOrganisms := make([]*Organism, 0, chunkSize)
		parentsByChild := make(map[string]*Organism, chunkSize)

		for i := 0; i < chunkSize; i++ {
			if runtime.MaxMetricCalls > 0 && metricCallsUsed != nil && *metricCallsUsed >= runtime.MaxMetricCalls {
				break
			}

			mutationParent := selectGEPAMutationParent(states, runtime, run.Spec, rng)
			if mutationParent == nil || mutationParent.organism == nil {
				mutationParent = &dspyCandidateState{organism: parent, score: parentScore(parent), fullEval: true}
			}

			failures := baseFailures
			if mutationParent.organism != nil && strings.TrimSpace(mutationParent.organism.BenchRunID) != "" {
				if sampled, err := l.fetchParentFailures(ctx, run, mutationParent.organism, reflectionLimit); err == nil && len(sampled) > 0 {
					failures = sampled
				}
			}
			learning, _ := l.Store.GetLearningLog(ctx, run.ID, 300)
			pc := l.buildProposalContext(ctx, run, mutationParent.organism, baseWorkflowJSON, "", failures, learning, rng)
			mutations, err := gepa.Mutate(ctx, &MutationRequest{
				Parent:          mutationParent.organism,
				Spec:            run.Spec,
				FailureCases:    failures,
				LearningLog:     learning,
				Generation:      generation + trial + i,
				Count:           1,
				ProposalContext: pc,
			})
			if err != nil {
				return createdCount, fmt.Errorf("gepa dspy mutate trial %d: %w", trial+i+1, err)
			}
			if len(mutations) == 0 || mutations[0] == nil || mutations[0].Organism == nil {
				continue
			}
			mutation := mutations[0]
			mutation.Organism.OptRunID = run.ID
			mutation.Organism.Generation = generation
			if err := l.persistCandidateOrganism(ctx, run, mutation.Organism, mutation.Changes, mutation.Artifacts); err != nil {
				return createdCount, err
			}
			createdCount++

			batchOrganisms = append(batchOrganisms, mutation.Organism)
			parentsByChild[mutation.Organism.ID] = mutationParent.organism
		}

		if len(batchOrganisms) == 0 {
			trial = chunkEnd
			continue
		}

		// 2. Stage all N at once.
		staged, err := l.stageOrganisms(ctx, run, baseWorkflowJSON, batchOrganisms, parentsByChild)
		if err != nil {
			return createdCount, err
		}

		// 3. Verify batch → survivors.
		survivors, err := l.verifyMutationsIfEnabled(ctx, run, staged)
		if err != nil {
			_ = l.cleanupStaged(ctx, staged)
			return createdCount, err
		}

		// 4. Score survivors that don't already have ReplayFitness.
		needsEval := filterStagedWithoutReplayFitness(survivors)
		var batchResults map[string]transientEvaluatedCandidate
		if len(needsEval) > 0 {
			evalLimit := runtime.MinibatchSize
			isFullEval := false
			if !runtime.UseMinibatch {
				evalLimit = run.ItemLimit
				isFullEval = true
			}
			if canSpendDSPYMetricBudget(runtime, metricCallsUsed, max(evalLimit, 1)*len(needsEval)) {
				var metricCalls int
				batchResults, metricCalls, err = l.evaluateStagedBatchTransient(ctx, run, needsEval, evalLimit)
				if err != nil {
					_ = l.cleanupStaged(ctx, staged)
					return createdCount, err
				}
				accumulateDSPYMetricCallsRaw(metricCallsUsed, metricCalls)
				// For full-eval (non-minibatch), persist fitness from batch results.
				if isFullEval {
					for _, item := range needsEval {
						if res, ok := batchResults[item.Organism.ID]; ok && res.Fitness != nil {
							item.Organism.Fitness = res.Fitness
							item.Organism.BenchRunID = res.RunID
							now := time.Now().UTC()
							item.Organism.EvaluatedAt = &now
							_ = l.Store.UpdateOrganismFitness(ctx, item.Organism.ID, res.RunID, res.Fitness)
							mp := parentsByChild[item.Organism.ID]
							_ = l.appendLearningForEvaluatedOrganism(ctx, run, item.Organism, mp)
						}
					}
				}
			}
		}

		// 5. Update states for all survivors.
		for _, item := range survivors {
			score := pickTrialScore(item, batchResults)
			isFullEval := !runtime.UseMinibatch && item.Organism.EvaluatedAt != nil
			states = append(states, &dspyCandidateState{
				organism: item.Organism,
				score:    score,
				fullEval: isFullEval,
			})
		}

		// 6. Cleanup all staged.
		_ = l.cleanupStaged(ctx, staged)

		// 7. Periodic full eval.
		if runtime.UseMinibatch && shouldRunDSPYPeriodicFullEval(chunkEnd-1, trialCount, fullEvalSteps) {
			target := selectGEPAFullEvalTarget(states, runtime, run.Spec, rng)
			if target != nil && target.organism != nil && target.organism.EvaluatedAt == nil {
				if canSpendDSPYMetricBudget(runtime, metricCallsUsed, max(run.ItemLimit, 1)) {
					fitness, err := l.evaluateOrganismSingle(ctx, run, baseWorkflowJSON, target.organism, run.ItemLimit)
					if err != nil {
						return createdCount, err
					}
					accumulateDSPYMetricCalls(metricCallsUsed, fitness, run.ItemLimit)
					target.score = fitness.AdjustedAccuracy
					target.fullEval = true
					evalParent := resolveDSPYCandidateParent(states, target.organism, parent)
					if err := l.appendLearningForEvaluatedOrganism(ctx, run, target.organism, evalParent); err != nil {
						return createdCount, err
					}
				}
			}
			if metricCallsUsed != nil && *metricCallsUsed == callsBefore {
				noMetricProgress++
			} else {
				noMetricProgress = 0
			}
			if runtime.MaxMetricCalls > 0 && noMetricProgress >= max(fullEvalSteps*8, 32) {
				break
			}
		}

		trial = chunkEnd
	}

	if runtime.UseMinibatch {
		target := selectGEPAFullEvalTarget(states, runtime, run.Spec, rng)
		if target != nil && target.organism != nil && target.organism.EvaluatedAt == nil {
			if !canSpendDSPYMetricBudget(runtime, metricCallsUsed, max(run.ItemLimit, 1)) {
				return createdCount, nil
			}
			fitness, err := l.evaluateOrganismSingle(ctx, run, baseWorkflowJSON, target.organism, run.ItemLimit)
			if err != nil {
				return createdCount, err
			}
			accumulateDSPYMetricCalls(metricCallsUsed, fitness, run.ItemLimit)
			target.score = fitness.AdjustedAccuracy
			target.fullEval = true
			evalParent := resolveDSPYCandidateParent(states, target.organism, parent)
			if err := l.appendLearningForEvaluatedOrganism(ctx, run, target.organism, evalParent); err != nil {
				return createdCount, err
			}
		}
	}

	return createdCount, nil
}

func selectGEPAMutationParent(states []*dspyCandidateState, runtime DSPYRuntimeSettings, spec *OptimizeSpec, rng *rand.Rand) *dspyCandidateState {
	if len(states) == 0 {
		return nil
	}
	candidates := make([]*dspyCandidateState, 0, len(states))
	for _, state := range states {
		if state == nil || state.organism == nil {
			continue
		}
		candidates = append(candidates, state)
	}
	if len(candidates) == 0 {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(runtime.CandidateSelectionStrategy)) {
	case "current_best":
		return bestDSPYCandidateState(candidates)
	default:
		frontier := gepaParetoFrontier(candidates, spec)
		if len(frontier) == 0 {
			return bestDSPYCandidateState(candidates)
		}
		if rng == nil {
			rng = rand.New(rand.NewSource(rand.Int63())) //nolint:gosec
		}
		return frontier[rng.Intn(len(frontier))]
	}
}

func selectGEPAFullEvalTarget(states []*dspyCandidateState, runtime DSPYRuntimeSettings, spec *OptimizeSpec, rng *rand.Rand) *dspyCandidateState {
	pending := make([]*dspyCandidateState, 0, len(states))
	for _, state := range states {
		if state == nil || state.organism == nil || state.fullEval {
			continue
		}
		pending = append(pending, state)
	}
	if len(pending) == 0 {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(runtime.CandidateSelectionStrategy)) {
	case "current_best":
		return bestDSPYCandidateState(pending)
	default:
		frontier := gepaParetoFrontier(pending, spec)
		if len(frontier) == 0 {
			return bestDSPYCandidateState(pending)
		}
		if rng == nil {
			rng = rand.New(rand.NewSource(rand.Int63())) //nolint:gosec
		}
		return frontier[rng.Intn(len(frontier))]
	}
}

func bestDSPYCandidateState(states []*dspyCandidateState) *dspyCandidateState {
	var best *dspyCandidateState
	for _, state := range states {
		if state == nil || state.organism == nil {
			continue
		}
		if best == nil {
			best = state
			continue
		}
		if state.fullEval && !best.fullEval {
			best = state
			continue
		}
		if state.score > best.score+1e-9 {
			best = state
			continue
		}
		if math.Abs(state.score-best.score) <= 1e-9 && state.organism.CreatedAt.Before(best.organism.CreatedAt) {
			best = state
		}
	}
	return best
}

func gepaParetoFrontier(states []*dspyCandidateState, spec *OptimizeSpec) []*dspyCandidateState {
	if len(states) <= 1 {
		return states
	}
	frontier := make([]*dspyCandidateState, 0, len(states))
	for i := range states {
		a := states[i]
		if a == nil || a.organism == nil || a.organism.Fitness == nil {
			continue
		}
		dominated := false
		for j := range states {
			if i == j {
				continue
			}
			b := states[j]
			if b == nil || b.organism == nil || b.organism.Fitness == nil {
				continue
			}
			if fitnessDominates(b.organism.Fitness, a.organism.Fitness, spec) {
				dominated = true
				break
			}
		}
		if !dominated {
			frontier = append(frontier, a)
		}
	}
	if len(frontier) == 0 {
		return states
	}
	return frontier
}
