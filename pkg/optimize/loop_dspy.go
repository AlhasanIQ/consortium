package optimize

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// executeDSPYStyle runs a non-Darwinian optimizer path where each generation mutates
// only the current incumbent (best organism), matching a DSPy-style incumbent search loop.
func (l *Loop) executeDSPYStyle(ctx context.Context, run *OptimizationRun, baseWorkflowJSON json.RawMessage) error {
	mode, err := ResolveDSPYOptimizerMode(run.MutatorMode, run.Spec)
	if err != nil {
		return fmt.Errorf("resolve dspy optimizer mode: %w", err)
	}
	runtime := ResolveDSPYRuntimeSettings(run.Spec, mode, run.ItemLimit)
	metricCallsUsed, err := l.loadDSPYMetricCallsUsed(ctx, run)
	if err != nil {
		return err
	}
	run.DSPYMetricCallsUsed = metricCallsUsed

	withoutImprovement := 0
	var lastBestFitness *Fitness
	if run.BestFitness != nil {
		snapshot := *run.BestFitness
		lastBestFitness = &snapshot
	}

	for generation := run.Generation + 1; generation <= run.Spec.StopPolicy.MaxGenerations; generation++ {
		if err := ctx.Err(); err != nil {
			_ = l.Store.UpdateRunStatus(context.Background(), run.ID, "paused")
			return err
		}
		if runtime.Optimizer == MutatorModeGEPA && runtime.MaxMetricCalls > 0 && metricCallsUsed >= runtime.MaxMetricCalls {
			break
		}
		if run.SpentUSD >= run.TotalBudgetUSD {
			break
		}
		if run.Status != "running" {
			break
		}

		existing, err := l.Store.GetOrganismsByGeneration(ctx, run.ID, generation, 100000)
		if err != nil {
			return fmt.Errorf("load generation organisms: %w", err)
		}
		createdCount := 0
		if len(existing) == 0 {
			parent, err := l.currentBest(ctx, run)
			if err != nil {
				return err
			}
			if parent == nil {
				return fmt.Errorf("dspy strategy requires an incumbent parent organism")
			}
			createdCount, err = l.runDSPYGeneration(
				ctx,
				run,
				baseWorkflowJSON,
				mode,
				generation,
				parent,
				runtime,
				&metricCallsUsed,
			)
			if err != nil {
				return err
			}
		} else {
			// Resume compatibility: if a generation already has pre-created organisms,
			// evaluate remaining unevaluated candidates with the previous flow.
			parentsByChild, err := l.loadParentMap(ctx, run.ID)
			if err != nil {
				return err
			}
			unevaluated := make([]*Organism, 0, len(existing))
			for _, organism := range existing {
				if organism == nil {
					continue
				}
				if organism.Fitness != nil && organism.EvaluatedAt != nil {
					continue
				}
				unevaluated = append(unevaluated, organism)
			}
			if len(unevaluated) > 0 {
				staged, err := l.stageOrganisms(ctx, run, baseWorkflowJSON, unevaluated, parentsByChild)
				if err != nil {
					return err
				}
				defer func() {
					_ = l.cleanupStaged(context.Background(), staged)
				}()

				survivors, err := l.verifyMutationsIfEnabled(ctx, run, staged)
				if err != nil {
					return err
				}
				if len(survivors) > 0 {
					useMinibatch := shouldUseDSPYMinibatch(runtime, run.ItemLimit, len(survivors))
					if !useMinibatch {
						if err := l.evaluateStagedBatch(ctx, run, survivors); err != nil {
							return err
						}
						metricCallsUsed += countMetricCallsForStaged(survivors, run.ItemLimit)
					} else {
						transient, transientMetricCalls, err := l.evaluateStagedBatchTransient(ctx, run, survivors, runtime.MinibatchSize)
						if err != nil {
							return err
						}
						metricCallsUsed += transientMetricCalls
						remainingMetricCalls := 0
						if runtime.Optimizer == MutatorModeGEPA && runtime.MaxMetricCalls > 0 {
							remainingMetricCalls = runtime.MaxMetricCalls - metricCallsUsed
						}
						fullEvalCandidates := selectDSPYFullEvalCandidates(
							survivors,
							transient,
							runtime,
							run.Spec,
							run.ItemLimit,
							remainingMetricCalls,
						)
						if len(fullEvalCandidates) > 0 {
							if err := l.evaluateStagedBatch(ctx, run, fullEvalCandidates); err != nil {
								return err
							}
							metricCallsUsed += countMetricCallsForStaged(fullEvalCandidates, run.ItemLimit)
						}
					}
				}
				if err := l.cleanupStaged(ctx, staged); err != nil {
					return err
				}
			}
		}

		best, err := l.currentBest(ctx, run)
		if err != nil {
			return err
		}
		improved := false
		if best != nil {
			if isFitnessBetter(best.Fitness, lastBestFitness) {
				improved = true
				snapshot := *best.Fitness
				lastBestFitness = &snapshot
			}
			run.BestOrganismID = best.ID
			run.BestFitness = best.Fitness
		}
		if improved {
			withoutImprovement = 0
		} else {
			withoutImprovement++
		}

		all, err := l.Store.GetAllOrganisms(ctx, run.ID)
		if err != nil {
			return fmt.Errorf("load all organisms for progress: %w", err)
		}
		run.Generation = generation
		run.TotalOrganisms = len(all)
		run.DSPYMetricCallsUsed = metricCallsUsed
		if createdCount > 0 && run.TotalOrganisms < createdCount {
			run.TotalOrganisms = createdCount
		}
		if err := l.Store.UpdateRunProgress(ctx, run.ID, run.Generation, run.BestOrganismID, run.BestFitness, run.SpentUSD, run.TotalOrganisms, run.DSPYMetricCallsUsed); err != nil {
			return fmt.Errorf("update run progress: %w", err)
		}

		if run.Spec.StopPolicy.PlateauGenerations > 0 && withoutImprovement >= run.Spec.StopPolicy.PlateauGenerations {
			break
		}
	}

	run.Status = "completed"
	completedAt := time.Now().UTC()
	run.CompletedAt = &completedAt
	if err := l.Store.UpdateRunStatus(ctx, run.ID, "completed"); err != nil {
		return fmt.Errorf("set run completed: %w", err)
	}
	return nil
}

func shouldUseDSPYMinibatch(runtime DSPYRuntimeSettings, runItemLimit int, candidateCount int) bool {
	if candidateCount <= 1 {
		return false
	}
	if !runtime.UseMinibatch {
		return false
	}
	if runtime.MinibatchSize <= 0 {
		return false
	}
	if runItemLimit > 0 && runtime.MinibatchSize >= runItemLimit {
		return false
	}
	return true
}

func selectDSPYFullEvalCandidates(
	staged []*stagedOrganism,
	transient map[string]transientEvaluatedCandidate,
	runtime DSPYRuntimeSettings,
	spec *OptimizeSpec,
	fullEvalItemLimit int,
	remainingMetricCalls int,
) []*stagedOrganism {
	if len(staged) == 0 || len(transient) == 0 {
		return nil
	}
	scored := make([]scoredCandidate, 0, len(staged))
	for _, item := range staged {
		if item == nil || item.Organism == nil {
			continue
		}
		result, ok := transient[item.Organism.ID]
		if !ok || result.Fitness == nil {
			continue
		}
		scored = append(scored, scoredCandidate{
			item:    item,
			fitness: result.Fitness,
		})
	}
	if len(scored) == 0 {
		return nil
	}

	byCombo := make(map[string][]scoredCandidate)
	for _, candidate := range scored {
		key := dspyParamComboKey(candidate.item.Organism)
		byCombo[key] = append(byCombo[key], candidate)
	}
	comboRanked := make([]comboAggregate, 0, len(byCombo))
	for key, candidates := range byCombo {
		if len(candidates) == 0 {
			continue
		}
		sum := 0.0
		best := candidates[0]
		for _, candidate := range candidates {
			sum += candidate.fitness.CompositeScore
			if isFitnessBetter(candidate.fitness, best.fitness) {
				best = candidate
			}
		}
		comboRanked = append(comboRanked, comboAggregate{
			key:       key,
			meanScore: sum / float64(len(candidates)),
			best:      best,
		})
	}
	sort.SliceStable(comboRanked, func(i, j int) bool {
		if comboRanked[i].meanScore > comboRanked[j].meanScore+1e-9 {
			return true
		}
		if comboRanked[i].meanScore < comboRanked[j].meanScore-1e-9 {
			return false
		}
		if isFitnessBetter(comboRanked[i].best.fitness, comboRanked[j].best.fitness) {
			return true
		}
		if isFitnessBetter(comboRanked[j].best.fitness, comboRanked[i].best.fitness) {
			return false
		}
		return comboRanked[i].best.item.Organism.CreatedAt.Before(comboRanked[j].best.item.Organism.CreatedAt)
	})
	if len(comboRanked) == 0 {
		return nil
	}

	steps := max(runtime.MinibatchFullEvalSteps, 1)
	fullEvalCount := max(len(comboRanked)/steps, 1)
	if len(comboRanked)%steps != 0 {
		fullEvalCount++
	}
	if runtime.MaxFullEvals > 0 && fullEvalCount > runtime.MaxFullEvals {
		fullEvalCount = runtime.MaxFullEvals
	}
	if runtime.Optimizer == MutatorModeGEPA && runtime.MaxMetricCalls > 0 && remainingMetricCalls >= 0 && fullEvalItemLimit > 0 {
		maxAllowed := remainingMetricCalls / fullEvalItemLimit
		if maxAllowed < fullEvalCount {
			fullEvalCount = maxAllowed
		}
	}
	if fullEvalCount > len(comboRanked) {
		fullEvalCount = len(comboRanked)
	}
	if fullEvalCount <= 0 {
		return nil
	}

	ordered := comboRanked
	if runtime.Optimizer == MutatorModeGEPA && strings.EqualFold(strings.TrimSpace(runtime.CandidateSelectionStrategy), "pareto") {
		ordered = prioritizeParetoCombos(comboRanked, spec)
	}

	selected := make([]*stagedOrganism, 0, fullEvalCount)
	for i := 0; i < fullEvalCount; i++ {
		selected = append(selected, ordered[i].best.item)
	}
	return selected
}

func prioritizeParetoCombos(combos []comboAggregate, spec *OptimizeSpec) []comboAggregate {
	if len(combos) <= 1 {
		return combos
	}
	frontier := make([]comboAggregate, 0, len(combos))
	rest := make([]comboAggregate, 0, len(combos))
	for i := range combos {
		dominated := false
		for j := range combos {
			if i == j {
				continue
			}
			if fitnessDominates(combos[j].best.fitness, combos[i].best.fitness, spec) {
				dominated = true
				break
			}
		}
		if dominated {
			rest = append(rest, combos[i])
		} else {
			frontier = append(frontier, combos[i])
		}
	}
	out := make([]comboAggregate, 0, len(combos))
	out = append(out, frontier...)
	out = append(out, rest...)
	return out
}

func fitnessDominates(a *Fitness, b *Fitness, spec *OptimizeSpec) bool {
	if a == nil || b == nil {
		return false
	}
	objectives := defaultObjectives(spec)
	betterOrEqualAll := true
	strictlyBetter := false
	for _, objective := range objectives {
		av := a.MetricValue(objective.Metric)
		bv := b.MetricValue(objective.Metric)
		switch objective.Direction {
		case "minimize":
			if av > bv+1e-9 {
				betterOrEqualAll = false
			}
			if av+1e-9 < bv {
				strictlyBetter = true
			}
		default:
			if av+1e-9 < bv {
				betterOrEqualAll = false
			}
			if av > bv+1e-9 {
				strictlyBetter = true
			}
		}
		if !betterOrEqualAll {
			return false
		}
	}
	return betterOrEqualAll && strictlyBetter
}

func defaultObjectives(spec *OptimizeSpec) []Objective {
	if spec != nil && len(spec.Objectives) > 0 {
		return spec.Objectives
	}
	return []Objective{
		{Metric: "accuracy", Direction: "maximize"},
		{Metric: "cost_per_item", Direction: "minimize"},
	}
}

func dspyParamComboKey(org *Organism) string {
	if org == nil || len(org.ParamValues) == 0 {
		return ""
	}
	keys := make([]string, 0, len(org.ParamValues))
	for path := range org.ParamValues {
		keys = append(keys, path)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(org.ParamValues[key])
		b.WriteString(";")
	}
	return b.String()
}

func metricCallsFromFitness(fitness *Fitness, fallback int) int {
	if fitness != nil && fitness.TotalItems > 0 {
		return fitness.TotalItems
	}
	if fallback > 0 {
		return fallback
	}
	return 0
}

func countMetricCallsForStaged(staged []*stagedOrganism, fallback int) int {
	total := 0
	for _, item := range staged {
		if item == nil || item.Organism == nil {
			continue
		}
		total += metricCallsFromFitness(item.Organism.Fitness, fallback)
	}
	return total
}

func (l *Loop) loadDSPYMetricCallsUsed(ctx context.Context, run *OptimizationRun) (int, error) {
	if run == nil {
		return 0, nil
	}
	if run.DSPYMetricCallsUsed > 0 {
		return run.DSPYMetricCallsUsed, nil
	}
	all, err := l.Store.GetAllOrganisms(ctx, run.ID)
	if err != nil {
		return 0, fmt.Errorf("load organisms for dspy metric call accounting: %w", err)
	}
	total := 0
	for _, organism := range all {
		if organism == nil || organism.Fitness == nil || organism.EvaluatedAt == nil {
			continue
		}
		total += metricCallsFromFitness(organism.Fitness, run.ItemLimit)
	}
	return total, nil
}
