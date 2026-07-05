package optimize

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type dspyCandidateState struct {
	organism *Organism
	score    float64
	fullEval bool
}

func (l *Loop) runDSPYGeneration(
	ctx context.Context,
	run *OptimizationRun,
	baseWorkflowJSON json.RawMessage,
	mode string,
	generation int,
	parent *Organism,
	runtime DSPYRuntimeSettings,
	metricCallsUsed *int,
) (int, error) {
	switch mode {
	case MutatorModeMIPROv2:
		return l.runDSPYMIPROGeneration(ctx, run, baseWorkflowJSON, generation, parent, runtime, metricCallsUsed)
	case MutatorModeGEPA:
		return l.runDSPYGEPAGeneration(ctx, run, baseWorkflowJSON, generation, parent, runtime, metricCallsUsed)
	default:
		return 0, fmt.Errorf("unsupported dspy optimizer mode %q", mode)
	}
}

func (l *Loop) persistCandidateOrganism(ctx context.Context, run *OptimizationRun, organism *Organism, changes []ParamChange, artifacts []MutationArtifact) error {
	if organism == nil {
		return fmt.Errorf("organism is nil")
	}
	if err := l.Store.CreateOrganism(ctx, organism); err != nil {
		return fmt.Errorf("create child organism: %w", err)
	}
	if len(changes) > 0 {
		if err := l.Store.CreateParamChanges(ctx, organism.ID, changes); err != nil {
			return fmt.Errorf("persist child param changes: %w", err)
		}
	}
	if len(artifacts) > 0 {
		if err := l.Store.CreateMutationArtifacts(ctx, organism.ID, artifacts, run.CompactArtifacts); err != nil {
			return fmt.Errorf("persist mutation artifacts: %w", err)
		}
	}
	return nil
}

func (l *Loop) appendLearningForEvaluatedOrganism(ctx context.Context, run *OptimizationRun, organism *Organism, parent *Organism) error {
	if run == nil || organism == nil || organism.Fitness == nil {
		return nil
	}
	parentID := ""
	delta := 0.0
	if parent != nil {
		parentID = parent.ID
		if parent.Fitness != nil {
			delta = organism.Fitness.CompositeScore - parent.Fitness.CompositeScore
		}
	}
	entry := &LearningEntry{
		Generation:   organism.Generation,
		OrganismID:   organism.ID,
		ParentID:     parentID,
		MutationType: organism.MutationType,
		Description:  organism.MutationLog,
		Outcome:      classifyOutcome(organism.Fitness, delta),
		FitnessDelta: delta,
		CreatedAt:    time.Now().UTC(),
	}
	return l.Store.AppendLearningEntry(ctx, run.ID, entry)
}

func accumulateDSPYMetricCalls(metricCallsUsed *int, fitness *Fitness, fallback int) {
	if metricCallsUsed == nil {
		return
	}
	*metricCallsUsed += metricCallsFromFitness(fitness, fallback)
}

func accumulateDSPYMetricCallsRaw(metricCallsUsed *int, calls int) {
	if metricCallsUsed == nil {
		return
	}
	*metricCallsUsed += calls
}

func filterStagedWithoutReplayFitness(staged []*stagedOrganism) []*stagedOrganism {
	out := make([]*stagedOrganism, 0, len(staged))
	for _, item := range staged {
		if item != nil && item.ReplayFitness == nil {
			out = append(out, item)
		}
	}
	return out
}

func pickTrialScore(item *stagedOrganism, batchResults map[string]transientEvaluatedCandidate) float64 {
	if item == nil || item.Organism == nil {
		return 0
	}
	if item.ReplayFitness != nil {
		return item.ReplayFitness.AdjustedAccuracy
	}
	if res, ok := batchResults[item.Organism.ID]; ok && res.Fitness != nil {
		return res.Fitness.AdjustedAccuracy
	}
	return 0
}

func canSpendDSPYMetricBudget(runtime DSPYRuntimeSettings, metricCallsUsed *int, estimatedCalls int) bool {
	if runtime.Optimizer != MutatorModeGEPA || runtime.MaxMetricCalls <= 0 || metricCallsUsed == nil {
		return true
	}
	if estimatedCalls <= 0 {
		estimatedCalls = 1
	}
	return (*metricCallsUsed + estimatedCalls) <= runtime.MaxMetricCalls
}

func shouldRunDSPYPeriodicFullEval(trial int, trialCount int, fullEvalSteps int) bool {
	if trialCount <= 0 {
		return false
	}
	if trial == trialCount-1 {
		return true
	}
	interval := max(fullEvalSteps+1, 1)
	return (trial+1)%interval == 0
}

func parentScore(parent *Organism) float64 {
	if parent == nil || parent.Fitness == nil {
		return 0
	}
	return parent.Fitness.AdjustedAccuracy
}

func copyIntMap(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func resolveDSPYCandidateParent(states []*dspyCandidateState, organism *Organism, fallback *Organism) *Organism {
	if organism == nil || len(organism.ParentIDs) == 0 {
		return fallback
	}
	parentID := strings.TrimSpace(organism.ParentIDs[0])
	if parentID == "" {
		return fallback
	}
	for _, state := range states {
		if state == nil || state.organism == nil {
			continue
		}
		if state.organism.ID == parentID {
			return state.organism
		}
	}
	return fallback
}
