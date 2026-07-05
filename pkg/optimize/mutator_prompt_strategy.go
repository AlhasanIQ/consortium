package optimize

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"sort"
	"strings"
)

type promptMutationStrategy struct {
	MutationType  string
	DefaultReason string
	BuildPrompt   func(currentPrompt string, failures []FailureCase, learning []LearningEntry, spec *OptimizeSpec, pc *ProposalContext) string
}

func mutatePromptWithStrategy(
	ctx context.Context,
	model string,
	rng *rand.Rand,
	req *MutationRequest,
	strategy promptMutationStrategy,
) ([]*MutationResult, error) {
	if strategy.BuildPrompt == nil {
		return nil, ErrStrategyMissingBuilder
	}
	count, rng, err := prepareMutation(req, rng)
	if err != nil {
		return nil, err
	}

	promptParams := make([]ParamDeclaration, 0)
	for _, declaration := range req.Spec.Params {
		if declaration.Type == ParamTypePrompt {
			promptParams = append(promptParams, declaration)
		}
	}
	if len(promptParams) == 0 {
		return nil, ErrNoPromptParams
	}

	workflowID := extractWorkflowID(req.Parent.WorkflowJSON)
	baseValues, err := buildParentParamValues(req.Parent, req.Spec)
	if err != nil {
		return nil, err
	}

	mutationType := strings.TrimSpace(strategy.MutationType)
	if mutationType == "" {
		mutationType = "llm_prompt"
	}
	defaultReason := strings.TrimSpace(strategy.DefaultReason)
	if defaultReason == "" {
		defaultReason = "LLM revised prompt based on sampled failures"
	}

	results := make([]*MutationResult, 0, count)
	for i := 0; i < count; i++ {
		selected := promptParams[rng.Intn(len(promptParams))]
		grouped := collectMutationGroup(selected, promptParams)

		currentPrompt, err := decodePromptValue(baseValues[selected.Path])
		if err != nil {
			return nil, fmt.Errorf("decode current prompt %s: %w", selected.Path, err)
		}

		prompt := strategy.BuildPrompt(currentPrompt, req.FailureCases, req.LearningLog, req.Spec, req.ProposalContext)
		mutation, err := InvokeClaudePromptMutation(ctx, model, prompt)
		if err != nil {
			return nil, err
		}
		if err := ValidatePromptIsolation(workflowID, selected.Path, mutation.Parsed.RevisedPrompt); err != nil {
			return nil, err
		}

		childValues := copyStringMap(baseValues)
		changes := make([]ParamChange, 0, len(grouped))
		for _, declaration := range grouped {
			oldEncoded := childValues[declaration.Path]
			newEncoded, err := encodeJSONValue(mutation.Parsed.RevisedPrompt)
			if err != nil {
				return nil, fmt.Errorf("encode revised prompt: %w", err)
			}
			childValues[declaration.Path] = newEncoded
			changes = append(changes, ParamChange{
				Path:     declaration.Path,
				OldValue: oldEncoded,
				NewValue: newEncoded,
				Reason:   coalesceString(mutation.Parsed.ChangesSummary, mutationType),
			})
		}
		sort.Slice(changes, func(i, j int) bool {
			return changes[i].Path < changes[j].Path
		})

		reason := strings.TrimSpace(mutation.Parsed.Reasoning)
		if reason == "" {
			reason = defaultReason
		}
		artifacts := []MutationArtifact{{
			InputPromptHash:  sha256Hex(prompt),
			InputPrompt:      prompt,
			RawOutputHash:    sha256Hex(mutation.RawOutput),
			RawOutput:        mutation.RawOutput,
			ClaudeModel:      coalesceString(mutation.Model, model),
			ClaudeCLIVersion: mutation.CLIVersion,
		}}
		organism := newChildOrganism(req.Parent, req.Generation, mutationType, reason, childValues)
		results = append(results, &MutationResult{
			Organism:  organism,
			Changes:   changes,
			Artifacts: artifacts,
			Reasoning: reason,
		})
	}
	return results, nil
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
