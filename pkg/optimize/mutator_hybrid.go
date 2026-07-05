package optimize

import (
	"context"
	"math/rand"
	"strings"
)

// AdaptiveMixMutator combines combinatorial and prompt operators and adapts
// the operator mix based on recent learning outcomes.
type AdaptiveMixMutator struct {
	Combinatorial             Mutator
	LLM                       Mutator
	PromptMutationProbability float64
	Rand                      *rand.Rand
}

func (m *AdaptiveMixMutator) Mutate(ctx context.Context, req *MutationRequest) ([]*MutationResult, error) {
	if req == nil {
		return nil, ErrMutationRequestRequired
	}
	count := req.Count
	if count < 1 {
		count = 1
	}
	rng := m.Rand
	if rng == nil {
		rng = rand.New(rand.NewSource(rand.Int63())) //nolint:gosec // non-cryptographic sampling
	}
	prob := m.PromptMutationProbability
	if prob < 0 {
		prob = 0.5
	}
	if prob > 1 {
		prob = 1
	}
	hasPrompt := requestHasPromptParams(req)
	hasCombinatorial := requestHasCombinatorialParams(req)
	if hasPrompt && hasCombinatorial {
		prob = adaptivePromptProbability(prob, req.LearningLog)
	}

	results := make([]*MutationResult, 0, count)
	for i := 0; i < count; i++ {
		selected := m.Combinatorial
		usePrompt := false
		switch {
		case hasPrompt && hasCombinatorial:
			usePrompt = rng.Float64() < prob
			if usePrompt {
				selected = m.LLM
			}
		case hasPrompt:
			usePrompt = true
			selected = m.LLM
		case hasCombinatorial:
			selected = m.Combinatorial
		default:
			return nil, ErrHybridMutatorIncompatible
		}
		if selected == nil {
			return nil, ErrHybridMutatorNotConfigured
		}

		childReq := *req
		childReq.Count = 1
		mutations, err := selected.Mutate(ctx, &childReq)
		if err != nil {
			alternate := firstAvailableAlternateMutator(usePrompt, hasCombinatorial, hasPrompt, m.Combinatorial, m.LLM)
			if alternate != nil {
				selected = alternate
				mutations, err = selected.Mutate(ctx, &childReq)
			}
			if err != nil {
				return nil, err
			}
		}
		results = append(results, mutations...)
	}
	for _, result := range results {
		if result.Organism != nil {
			// Preserve provenance: record which sub-mutator was used (e.g., "auto:llm_prompt").
			mutationType := strings.TrimSpace(result.Organism.MutationType)
			if mutationType != "" && !strings.HasPrefix(mutationType, "auto:") {
				result.Organism.MutationType = "auto:" + mutationType
			}
		}
	}
	return results, nil
}

func requestHasPromptParams(req *MutationRequest) bool {
	if req == nil || req.Spec == nil {
		return false
	}
	for _, declaration := range req.Spec.Params {
		if declaration.Type == ParamTypePrompt {
			return true
		}
	}
	return false
}

func requestHasCombinatorialParams(req *MutationRequest) bool {
	if req == nil || req.Spec == nil {
		return false
	}
	for _, declaration := range req.Spec.Params {
		switch declaration.Type {
		case ParamTypeModel, ParamTypeEnum, ParamTypeFloat, ParamTypeInt:
			return true
		}
	}
	return false
}

func firstAvailableAlternateMutator(usePrompt bool, hasCombinatorial bool, hasPrompt bool, combinatorial Mutator, llm Mutator) Mutator {
	if usePrompt {
		if !hasCombinatorial {
			return nil
		}
		return combinatorial
	}
	if !hasPrompt {
		return nil
	}
	return llm
}

func adaptivePromptProbability(base float64, learning []LearningEntry) float64 {
	if base < 0 {
		base = 0.5
	}
	if base > 1 {
		base = 1
	}
	if len(learning) == 0 {
		return base
	}

	type operatorStats struct {
		wins   float64
		losses float64
	}
	stats := map[string]*operatorStats{
		"prompt":        {},
		"combinatorial": {},
	}

	seen := 0
	for i := len(learning) - 1; i >= 0 && seen < 60; i-- {
		family := mutationTypeFamily(learning[i].MutationType)
		if family == "" {
			continue
		}
		win, loss := outcomeWeights(learning[i].Outcome)
		stats[family].wins += win
		stats[family].losses += loss
		seen++
	}
	if seen == 0 {
		return base
	}

	prompt := stats["prompt"]
	combinatorial := stats["combinatorial"]
	promptRate := (prompt.wins + 1.0) / (prompt.wins + prompt.losses + 2.0)
	combinatorialRate := (combinatorial.wins + 1.0) / (combinatorial.wins + combinatorial.losses + 2.0)
	learned := promptRate / (promptRate + combinatorialRate)

	evidence := float64(seen) / 20.0
	if evidence > 1.0 {
		evidence = 1.0
	}
	mixed := (1.0-evidence)*base + evidence*learned
	if mixed < 0.1 {
		return 0.1
	}
	if mixed > 0.9 {
		return 0.9
	}
	return mixed
}

func mutationTypeFamily(mutationType string) string {
	normalized := strings.ToLower(strings.TrimSpace(mutationType))
	switch {
	case strings.Contains(normalized, "combinatorial"):
		return "combinatorial"
	case strings.Contains(normalized, "prompt"):
		return "prompt"
	default:
		return ""
	}
}

func outcomeWeights(outcome string) (wins float64, losses float64) {
	switch strings.ToLower(strings.TrimSpace(outcome)) {
	case "improvement":
		return 1.0, 0.0
	case "no_change":
		return 0.15, 0.35
	case "regression", "constraint_violation", "verify_error":
		return 0.0, 1.0
	default:
		return 0.0, 0.5
	}
}
