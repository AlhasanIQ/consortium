package optimize

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
)

type CombinatorialMutator struct {
	Rand *rand.Rand
}

func (m *CombinatorialMutator) Mutate(_ context.Context, req *MutationRequest) ([]*MutationResult, error) {
	count, rng, err := prepareMutation(req, m.Rand)
	if err != nil {
		return nil, err
	}

	mutable := make([]ParamDeclaration, 0)
	for _, declaration := range req.Spec.Params {
		switch declaration.Type {
		case ParamTypeModel, ParamTypeEnum, ParamTypeFloat, ParamTypeInt:
			mutable = append(mutable, declaration)
		}
	}
	if len(mutable) == 0 {
		return nil, ErrNoCombinatorialParams
	}

	baseValues, err := buildParentParamValues(req.Parent, req.Spec)
	if err != nil {
		return nil, err
	}

	results := make([]*MutationResult, 0, count)
	for i := 0; i < count; i++ {
		childValues := copyStringMap(baseValues)
		changes := make([]ParamChange, 0)

		selected := mutable[rng.Intn(len(mutable))]
		grouped := collectMutationGroup(selected, mutable)
		for _, declaration := range grouped {
			oldEncoded, oldExists := childValues[declaration.Path]
			oldVal, _ := decodeIfPresent(oldEncoded, oldExists)

			newVal, err := m.sampleNewValue(declaration, oldVal, rng)
			if err != nil {
				return nil, fmt.Errorf("sample value for %s: %w", declaration.Path, err)
			}
			newEncoded, err := encodeJSONValue(newVal)
			if err != nil {
				return nil, fmt.Errorf("encode value for %s: %w", declaration.Path, err)
			}
			childValues[declaration.Path] = newEncoded

			changes = append(changes, ParamChange{
				Path:     declaration.Path,
				OldValue: oldEncoded,
				NewValue: newEncoded,
				Reason:   "combinatorial search",
			})
		}

		sort.Slice(changes, func(i, j int) bool {
			return changes[i].Path < changes[j].Path
		})

		reason := fmt.Sprintf("Mutated %d declarative parameter(s) via combinatorial sampling", len(changes))
		organism := newChildOrganism(req.Parent, req.Generation, "combinatorial", reason, childValues)
		results = append(results, &MutationResult{
			Organism:  organism,
			Changes:   changes,
			Reasoning: reason,
		})
	}
	return results, nil
}

func decodeIfPresent(encoded string, exists bool) (interface{}, error) {
	if !exists || strings.TrimSpace(encoded) == "" {
		return nil, nil
	}
	var v interface{}
	if err := json.Unmarshal([]byte(encoded), &v); err != nil {
		return nil, err
	}
	return v, nil
}

func collectMutationGroup(selected ParamDeclaration, all []ParamDeclaration) []ParamDeclaration {
	if strings.TrimSpace(selected.Group) == "" {
		return []ParamDeclaration{selected}
	}
	group := make([]ParamDeclaration, 0)
	for _, declaration := range all {
		if declaration.Group == selected.Group {
			group = append(group, declaration)
		}
	}
	if len(group) == 0 {
		return []ParamDeclaration{selected}
	}
	return group
}

func (m *CombinatorialMutator) sampleNewValue(declaration ParamDeclaration, old interface{}, rng *rand.Rand) (interface{}, error) {
	switch declaration.Type {
	case ParamTypeModel, ParamTypeEnum:
		if len(declaration.Candidates) == 0 {
			return nil, ErrMissingCandidates
		}
		candidates := append([]string(nil), declaration.Candidates...)
		rng.Shuffle(len(candidates), func(i, j int) {
			candidates[i], candidates[j] = candidates[j], candidates[i]
		})
		for _, candidate := range candidates {
			if oldStr, ok := old.(string); ok && oldStr == candidate && len(candidates) > 1 {
				continue
			}
			return candidate, nil
		}
		return candidates[0], nil
	case ParamTypeFloat:
		if declaration.Range == nil {
			return nil, ErrMissingRange
		}
		if declaration.Range.Step > 0 {
			nSteps := int(math.Round((declaration.Range.Max - declaration.Range.Min) / declaration.Range.Step))
			if nSteps < 0 {
				nSteps = 0
			}
			candidates := make([]float64, 0, nSteps+1)
			for i := 0; i <= nSteps; i++ {
				candidates = append(candidates, declaration.Range.Min+(float64(i)*declaration.Range.Step))
			}
			rng.Shuffle(len(candidates), func(i, j int) {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			})
			for _, candidate := range candidates {
				if oldFloat, ok := asFloat64(old); ok && math.Abs(oldFloat-candidate) < 1e-9 && len(candidates) > 1 {
					continue
				}
				return candidate, nil
			}
			return candidates[0], nil
		}
		candidate := declaration.Range.Min + (rng.Float64() * (declaration.Range.Max - declaration.Range.Min))
		if oldFloat, ok := asFloat64(old); ok && math.Abs(oldFloat-candidate) < 1e-6 {
			candidate = declaration.Range.Min
		}
		return candidate, nil
	case ParamTypeInt:
		if declaration.Range == nil {
			return nil, ErrMissingRange
		}
		step := declaration.Range.Step
		if step <= 0 {
			step = 1
		}
		min := int(math.Round(declaration.Range.Min))
		max := int(math.Round(declaration.Range.Max))
		if max < min {
			return nil, ErrInvalidIntRange
		}
		inc := int(math.Round(step))
		if inc < 1 {
			inc = 1
		}
		candidates := make([]int, 0, ((max-min)/inc)+1)
		for v := min; v <= max; v += inc {
			candidates = append(candidates, v)
		}
		if len(candidates) == 0 {
			candidates = append(candidates, min)
		}
		rng.Shuffle(len(candidates), func(i, j int) {
			candidates[i], candidates[j] = candidates[j], candidates[i]
		})
		for _, candidate := range candidates {
			if oldFloat, ok := asFloat64(old); ok && int(math.Round(oldFloat)) == candidate && len(candidates) > 1 {
				continue
			}
			return candidate, nil
		}
		return candidates[0], nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedCombinatorialType, declaration.Type)
	}
}
