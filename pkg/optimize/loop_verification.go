package optimize

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (l *Loop) verifyMutationsIfEnabled(ctx context.Context, run *OptimizationRun, staged []*stagedOrganism) ([]*stagedOrganism, error) {
	if !l.VerifyMutations {
		return staged, nil
	}
	mode := normalizeVerifyMode(l.VerifyMode)
	replayMode := normalizeReplayMode(l.VerifyReplayMode)

	survivors := make([]*stagedOrganism, 0, len(staged))
	for _, item := range staged {
		if item == nil || item.Organism == nil {
			continue
		}
		parent := item.Parent
		if parent == nil || strings.TrimSpace(parent.BenchRunID) == "" {
			survivors = append(survivors, item)
			continue
		}

		if mode == "replay" {
			passed, err := l.verifyViaReplay(ctx, run, item, replayMode)
			if err != nil {
				return nil, err
			}
			if passed {
				survivors = append(survivors, item)
			}
			continue
		}

		passed, err := l.verifyViaFullBenchmark(ctx, run, item)
		if err != nil {
			return nil, err
		}
		if passed {
			survivors = append(survivors, item)
		}
	}
	return survivors, nil
}

func (l *Loop) verifyViaReplay(ctx context.Context, run *OptimizationRun, item *stagedOrganism, replayMode string) (bool, error) {
	if item == nil || item.Organism == nil || item.Parent == nil {
		return true, nil
	}
	limit := l.QuickCheckItems
	if limit < 0 {
		limit = 0
	}
	failures, err := l.fetchParentFailures(ctx, run, item.Parent, limit)
	if err != nil {
		if replayMode == "best_effort" {
			return l.verifyViaFullBenchmark(ctx, run, item)
		}
		entry := &LearningEntry{
			Generation:   item.Organism.Generation,
			OrganismID:   item.Organism.ID,
			ParentID:     item.Parent.ID,
			MutationType: item.Organism.MutationType,
			Description:  coalesceString(item.Organism.MutationLog, "replay verification failed"),
			Outcome:      "verify_error",
			FitnessDelta: 0,
			VerifyMethod: "replay",
			CreatedAt:    time.Now().UTC(),
		}
		_ = l.Store.AppendLearningEntry(ctx, run.ID, entry)
		return false, nil
	}
	if len(failures) == 0 {
		return true, nil
	}

	itemIDs := make([]string, 0, len(failures))
	for _, failure := range failures {
		if id := strings.TrimSpace(failure.ItemID); id != "" {
			itemIDs = append(itemIDs, id)
		}
	}
	if len(itemIDs) == 0 {
		return true, nil
	}

	runID, err := l.Evaluator.ReplayBenchmark(ctx, ReplayBenchmarkRequest{
		BaseRunID:        item.Parent.BenchRunID,
		WorkflowID:       item.EvaluationWorkflow,
		Items:            itemIDs,
		Mode:             replayMode,
		Concurrency:      run.Concurrency,
		ChangedWorkflows: []string{item.CandidateWorkflowID},
	})
	if err != nil {
		if replayMode == "best_effort" {
			return l.verifyViaFullBenchmark(ctx, run, item)
		}
		entry := &LearningEntry{
			Generation:   item.Organism.Generation,
			OrganismID:   item.Organism.ID,
			ParentID:     item.Parent.ID,
			MutationType: item.Organism.MutationType,
			Description:  coalesceString(item.Organism.MutationLog, "replay verification failed"),
			Outcome:      "verify_error",
			FitnessDelta: 0,
			VerifyMethod: "replay",
			CreatedAt:    time.Now().UTC(),
		}
		_ = l.Store.AppendLearningEntry(ctx, run.ID, entry)
		return false, nil
	}
	if err := l.Evaluator.WaitForCompletion(ctx); err != nil {
		return false, fmt.Errorf("wait replay verification completion: %w", err)
	}
	fitness, err := l.Evaluator.GetRunSummary(ctx, runID)
	if err != nil {
		return false, fmt.Errorf("replay verification summary: %w", err)
	}
	fitness.CompositeScore = ComputeCompositeScore(fitness, run.Spec.Objectives)
	fitness.Feasible, fitness.ConstraintViolations = CheckConstraints(fitness, run.Spec.Constraints)
	item.ReplayFitness = fitness
	if fitness.AdjustedAccuracy <= 0 {
		entry := &LearningEntry{
			Generation:   item.Organism.Generation,
			OrganismID:   item.Organism.ID,
			ParentID:     item.Parent.ID,
			MutationType: item.Organism.MutationType,
			Description:  coalesceString(item.Organism.MutationLog, "no replay improvement on parent failures"),
			Outcome:      "no_change",
			FitnessDelta: fitness.AdjustedAccuracy,
			VerifyMethod: "replay",
			CreatedAt:    time.Now().UTC(),
		}
		_ = l.Store.AppendLearningEntry(ctx, run.ID, entry)
		return false, nil
	}
	return true, nil
}

func (l *Loop) verifyViaFullBenchmark(ctx context.Context, run *OptimizationRun, item *stagedOrganism) (bool, error) {
	if item == nil || item.Organism == nil {
		return true, nil
	}
	quickItems := l.QuickCheckItems
	if quickItems <= 0 {
		quickItems = 5
	}
	runID, err := l.Evaluator.RunBenchmark(ctx, RunBenchmarkRequest{
		WorkflowID:  item.EvaluationWorkflow,
		Benchmark:   run.Benchmark,
		Split:       run.Split,
		ItemLimit:   quickItems,
		Concurrency: run.Concurrency,
		Meta: map[string]string{
			"source":          "optimizer",
			"opt_run_id":      run.ID,
			"opt_organism_id": item.Organism.ID,
		},
	})
	if err != nil {
		return false, fmt.Errorf("start quick verification benchmark: %w", err)
	}
	if err := l.Evaluator.WaitForCompletion(ctx); err != nil {
		return false, fmt.Errorf("wait quick verification completion: %w", err)
	}
	fitness, err := l.Evaluator.GetRunSummary(ctx, runID)
	if err != nil {
		return false, fmt.Errorf("quick verification summary: %w", err)
	}
	fitness.CompositeScore = ComputeCompositeScore(fitness, run.Spec.Objectives)
	fitness.Feasible, fitness.ConstraintViolations = CheckConstraints(fitness, run.Spec.Constraints)
	item.ReplayFitness = fitness
	if item.Parent != nil && item.Parent.Fitness != nil && fitness.AdjustedAccuracy < (item.Parent.Fitness.AdjustedAccuracy-0.20) {
		delta := fitness.AdjustedAccuracy - item.Parent.Fitness.AdjustedAccuracy
		entry := &LearningEntry{
			Generation:   item.Organism.Generation,
			OrganismID:   item.Organism.ID,
			ParentID:     item.Parent.ID,
			MutationType: item.Organism.MutationType,
			Description:  coalesceString(item.Organism.MutationLog, "quick-check regression"),
			Outcome:      "regression",
			FitnessDelta: delta,
			VerifyMethod: "full",
			CreatedAt:    time.Now().UTC(),
		}
		_ = l.Store.AppendLearningEntry(ctx, run.ID, entry)
		return false, nil
	}
	return true, nil
}

func normalizeVerifyMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "replay":
		return "replay"
	case "full":
		return "full"
	default:
		return "replay"
	}
}

func normalizeReplayMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "best_effort", "best-effort":
		return "best_effort"
	case "required", "strict":
		return "required"
	default:
		return "best_effort"
	}
}
