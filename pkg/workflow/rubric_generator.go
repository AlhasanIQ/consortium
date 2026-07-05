package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"

	"github.com/alhasaniq/consortium/pkg/providers"
)

// RubricGeneratorSystemPrompt instructs the LLM to produce task-specific criteria.
const RubricGeneratorSystemPrompt = `You are an evaluation expert. Given a question or task, generate 3-5 scoring criteria tailored to that specific task. Each criterion must have a name, weight (decimal, all weights must sum to 1.0), and a one-sentence description.

Respond with ONLY valid JSON in this exact format:
{"rubric": [{"name": "CriterionName", "weight": 0.3, "description": "What this measures"}]}`

// GenerateDynamicRubric calls an LLM to produce a task-specific scoring rubric.
// Returns the rubric, token count from the generation call, and any error.
// Falls back to the provided static rubric on any failure (with 0 tokens).
func GenerateDynamicRubric(
	ctx context.Context,
	originalPrompt string,
	model string,
	llmClient *providers.Client,
	aggCtx *AggregationContext,
	staticFallback []RubricCriterion,
) ([]RubricCriterion, int, error) {
	if llmClient == nil || originalPrompt == "" {
		return staticFallback, 0, nil
	}

	messages := []providers.Message{
		{Role: "system", Content: RubricGeneratorSystemPrompt},
		{Role: "user", Content: fmt.Sprintf("Generate scoring criteria for evaluating responses to this question:\n\n%s", originalPrompt)},
	}

	req := &providers.ClientRequest{
		Model:       model,
		Messages:    messages,
		MaxTokens:   512,
		Temperature: providers.Float64Ptr(0.0),
	}
	// Disable extended reasoning for rubric generation — fast and cheap.
	req.Reasoning = &providers.ReasoningConfig{Effort: "none"}

	compCtx := &providers.CompletionContext{
		NodeLabel: "Rubric Generator",
		NodeName:  "Generate Dynamic Rubric",
	}
	if aggCtx != nil {
		compCtx.JobID = aggCtx.JobID
		compCtx.RunID = aggCtx.RunID
		compCtx.ParentNodeID = aggCtx.NodeID
		compCtx.CostTracker = aggCtx.CostTracker
		compCtx.NodeID = fmt.Sprintf("%s__agg__rubric_gen", aggCtx.NodeID)
	}

	resp, err := llmClient.Complete(ctx, req, compCtx)
	if err != nil {
		log.Printf("Dynamic rubric generation failed (LLM error), falling back to static: %v", err)
		return staticFallback, 0, nil
	}

	rubric, err := parseDynamicRubric(resp.Content)
	if err != nil {
		log.Printf("Dynamic rubric generation failed (parse error), falling back to static: %v", err)
		return staticFallback, 0, nil
	}

	return rubric, resp.Usage.TotalTokens, nil
}

// parseDynamicRubric extracts and validates a rubric from LLM JSON output.
func parseDynamicRubric(response string) ([]RubricCriterion, error) {
	// Find JSON object in response
	jsonRegex := regexp.MustCompile(`\{[\s\S]*"rubric"[\s\S]*\}`)
	match := jsonRegex.FindString(response)
	if match == "" {
		return nil, fmt.Errorf("no JSON rubric found in response")
	}

	var parsed struct {
		Rubric []RubricCriterion `json:"rubric"`
	}
	if err := json.Unmarshal([]byte(match), &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse rubric JSON: %w", err)
	}

	if len(parsed.Rubric) < 2 || len(parsed.Rubric) > 10 {
		return nil, fmt.Errorf("rubric must have 2-10 criteria, got %d", len(parsed.Rubric))
	}

	// Validate and normalize weights
	var totalWeight float64
	for _, c := range parsed.Rubric {
		if c.Name == "" {
			return nil, fmt.Errorf("rubric criterion missing name")
		}
		if c.Weight <= 0 {
			return nil, fmt.Errorf("rubric criterion %q has non-positive weight", c.Name)
		}
		totalWeight += c.Weight
	}

	// Normalize weights to sum to 1.0 if they don't already
	if totalWeight > 0 && (totalWeight < 0.95 || totalWeight > 1.05) {
		for i := range parsed.Rubric {
			parsed.Rubric[i].Weight = parsed.Rubric[i].Weight / totalWeight
		}
	}

	return parsed.Rubric, nil
}
