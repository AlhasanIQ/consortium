package optimize

import (
	"context"
	"encoding/json"
	"math/rand"
	"strings"
)

func (l *Loop) fetchParentFailures(ctx context.Context, run *OptimizationRun, parent *Organism, limit int) ([]FailureCase, error) {
	if parent == nil || strings.TrimSpace(parent.BenchRunID) == "" {
		return nil, nil
	}
	fetchLimit := limit
	if fetchLimit <= 0 {
		fetchLimit = 200
	} else {
		fetchLimit = max(limit*4, limit+8)
		if fetchLimit > 200 {
			fetchLimit = 200
		}
	}
	cases, err := l.Evaluator.GetFailureCases(ctx, parent.BenchRunID, fetchLimit)
	if err != nil {
		return nil, err
	}
	filtered := make([]FailureCase, 0, len(cases))
	for _, failure := range cases {
		if !l.IncludeFlaggedFailures && failure.Flagged {
			continue
		}
		if strings.TrimSpace(failure.Diagnosis) == "" {
			failure.Diagnosis = failureCaseDiagnosis(failure)
		}
		filtered = append(filtered, failure)
	}
	if limit <= 0 || len(filtered) <= limit {
		if enricher, ok := l.Evaluator.(FailureCaseEnricher); ok {
			if enriched, err := enricher.EnrichFailureCases(ctx, parent.BenchRunID, filtered); err == nil {
				filtered = enriched
			}
		}
		return filtered, nil
	}
	if run != nil && NormalizeOptimizeStrategy(run.Strategy) == OptimizeStrategyDSPY {
		// DSPy-style optimizers use random minibatches rather than failure-type bucketing.
		filtered = sampleFailureCasesRandom(filtered, limit, l.loopRand())
	} else {
		// Evolutionary/darwinian path benefits from a tighter, single-failure-mode
		// minibatch to preserve mutation focus.
		filtered = sampleFailureCasesByType(filtered, limit, l.loopRand())
	}
	if enricher, ok := l.Evaluator.(FailureCaseEnricher); ok {
		if enriched, err := enricher.EnrichFailureCases(ctx, parent.BenchRunID, filtered); err == nil {
			filtered = enriched
		}
	}
	return filtered, nil
}

// fetchParentSuccesses retrieves correctly-answered benchmark items from the
// parent's benchmark run for use as positive grounding context.
func (l *Loop) fetchParentSuccesses(ctx context.Context, parent *Organism, limit int) []SuccessExample {
	if parent == nil || strings.TrimSpace(parent.BenchRunID) == "" || limit <= 0 {
		return nil
	}
	provider, ok := l.Evaluator.(SuccessCaseProvider)
	if !ok {
		return nil
	}
	cases, err := provider.GetSuccessCases(ctx, parent.BenchRunID, limit)
	if err != nil {
		return nil
	}
	return cases
}

// buildProposalContext assembles enriched context for instruction proposals,
// inspired by DSPy's GroundedProposer which provides dataset summary,
// program/module descriptions, success examples, and scored instruction history.
func (l *Loop) buildProposalContext(
	ctx context.Context,
	run *OptimizationRun,
	parent *Organism,
	baseWorkflowJSON json.RawMessage,
	componentKey string,
	failures []FailureCase,
	learning []LearningEntry,
	rng *rand.Rand,
) *ProposalContext {
	pc := &ProposalContext{}

	// R7: Random tip diversity
	pc.Tip = randomProposalTip(rng)

	// R5: Include improving entries alongside non-improving ones
	pc.ImprovingEntries = selectImprovingLearningEntries(learning, 5)

	// R2: Success examples as positive grounding
	successes := l.fetchParentSuccesses(ctx, parent, 5)
	pc.SuccessExamples = successes

	// R1: Dataset summary from observed examples
	pc.DatasetSummary = buildDatasetSummary(failures, successes)

	// R6: Workflow structure and node role
	pc.WorkflowDescription = buildWorkflowDescription(baseWorkflowJSON, componentKey)

	return pc
}

func classifyOutcome(f *Fitness, delta float64) string {
	if f == nil {
		return "no_change"
	}
	if !f.Feasible {
		return "constraint_violation"
	}
	switch {
	case delta > 1e-9:
		return "improvement"
	case delta < -1e-9:
		return "regression"
	default:
		return "no_change"
	}
}

func sampleFailureCasesByType(cases []FailureCase, limit int, rng *rand.Rand) []FailureCase {
	if len(cases) == 0 || limit <= 0 || len(cases) <= limit {
		return append([]FailureCase(nil), cases...)
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(rand.Int63())) //nolint:gosec // non-cryptographic sampling
	}

	grouped := make(map[string][]FailureCase)
	keys := make([]string, 0)
	for _, failure := range cases {
		key := failureCaseType(failure)
		if _, exists := grouped[key]; !exists {
			keys = append(keys, key)
		}
		grouped[key] = append(grouped[key], failure)
	}
	weights := make([]float64, 0, len(keys))
	totalWeight := 0.0
	for _, key := range keys {
		weight := float64(len(grouped[key]))
		weights = append(weights, weight)
		totalWeight += weight
	}
	if totalWeight <= 0 {
		return append([]FailureCase(nil), cases[:limit]...)
	}

	target := rng.Float64() * totalWeight
	cumulative := 0.0
	chosenType := keys[0]
	for i, key := range keys {
		cumulative += weights[i]
		if target <= cumulative {
			chosenType = key
			break
		}
	}

	bucket := grouped[chosenType]
	n := min(limit, len(bucket))
	permutation := rng.Perm(len(bucket))
	sampled := make([]FailureCase, 0, n)
	for i := 0; i < n; i++ {
		sampled = append(sampled, bucket[permutation[i]])
	}
	return sampled
}

func sampleFailureCasesRandom(cases []FailureCase, limit int, rng *rand.Rand) []FailureCase {
	if len(cases) == 0 || limit <= 0 || len(cases) <= limit {
		return append([]FailureCase(nil), cases...)
	}
	if rng == nil {
		rng = rand.New(rand.NewSource(rand.Int63())) //nolint:gosec // non-cryptographic sampling
	}
	perm := rng.Perm(len(cases))
	sampled := make([]FailureCase, 0, limit)
	for i := 0; i < limit; i++ {
		sampled = append(sampled, cases[perm[i]])
	}
	return sampled
}

func failureCaseType(failure FailureCase) string {
	if category := strings.TrimSpace(strings.ToLower(failure.Category)); category != "" {
		return category
	}
	if reason := strings.TrimSpace(strings.ToLower(failure.FailureReason)); reason != "" {
		return reason
	}
	return "uncategorized"
}
