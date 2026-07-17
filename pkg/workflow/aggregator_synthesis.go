package workflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/alhasaniq/consortium/pkg/providers"
)

// SynthesisAggregator uses an LLM to synthesize multiple responses into one
type SynthesisAggregator struct{}

// Name returns the aggregation method name
func (s *SynthesisAggregator) Name() AggregationMethod {
	return AggMethodSynthesis
}

// DefaultSynthesisSystemPrompt is the default system prompt for the synthesis persona
const DefaultSynthesisSystemPrompt = `You are a synthesis agent that has received responses from multiple AI advisors. Your job is to deliver the definitive answer — informed by all responses but relying on your own judgment.

Rules:
1. Analyze each response for correctness, completeness, and reasoning quality.
2. Construct your answer using the strongest reasoning and evidence across all responses.
3. Where responses conflict, use your judgment to determine which reasoning is more sound — do not default to the majority or to any single response.
4. If responses use a structured answer format (e.g. "Final answer: X"), preserve that format in your synthesis.
5. End with a clear, definitive conclusion.`

// DefaultSynthesisPrompt is the default user prompt template for synthesis (contains the data)
const DefaultSynthesisPrompt = `Original Question: {{prompt}}

{{responses}}

Please provide a synthesized response that captures the best insights from all models.`

// Aggregate synthesizes all inputs using an LLM
func (s *SynthesisAggregator) Aggregate(ctx context.Context, inputs []AgentOutput, config map[string]interface{}, llmClient *providers.Client, aggCtx *AggregationContext) (*AggregationResult, error) {
	if single, err := checkSingleInput(inputs, AggMethodSynthesis); err != nil {
		return nil, err
	} else if single != nil {
		// Synthesis doesn't pick a "winner" — clear winner-specific fields.
		single.Result.Winner = ""
		single.Result.Scores = nil
		single.Result.Reasoning = ""
		return single.Result, nil
	}

	// Required configuration.
	model, _ := config["model"].(string)
	if model == "" {
		return nil, fmt.Errorf("synthesis aggregation requires aggregationConfig.model: %w", ErrAggregationConfig)
	}

	systemPrompt, _ := config["system_prompt"].(string)
	if systemPrompt == "" {
		return nil, fmt.Errorf("synthesis aggregation requires aggregationConfig.system_prompt: %w", ErrAggregationConfig)
	}

	promptTemplate, _ := config["prompt"].(string)
	if promptTemplate == "" {
		return nil, fmt.Errorf("synthesis aggregation requires aggregationConfig.prompt: %w", ErrAggregationConfig)
	}

	// Get original prompt from config if available
	originalPrompt, _ := config["original_prompt"].(string)

	temperature, ok := aggregationTemperature(config)
	if !ok {
		return nil, fmt.Errorf("synthesis aggregation requires numeric aggregationConfig.temperature: %w", ErrAggregationConfig)
	}
	maxTokens, ok := aggregationMaxTokens(config)
	if !ok {
		return nil, fmt.Errorf("synthesis aggregation requires aggregationConfig.max_tokens: %w", ErrAggregationConfig)
	}

	// Build the responses section with anonymized letter labels
	agentIDs := make([]string, 0, len(inputs))
	for _, input := range inputs {
		agentIDs = append(agentIDs, input.AgentID)
	}
	anonMap := BuildAnonymizationMap(agentIDs)

	var responsesBuilder strings.Builder
	for _, input := range inputs {
		label := anonMap.IDToLabel[input.AgentID]
		fmt.Fprintf(&responsesBuilder, "### Response %s:\n%s\n\n", label, input.Output)
	}

	// Build the final user prompt
	finalPrompt := promptTemplate
	finalPrompt = strings.ReplaceAll(finalPrompt, "{{prompt}}", originalPrompt)
	finalPrompt = strings.ReplaceAll(finalPrompt, "{{responses}}", responsesBuilder.String())

	// If the prompt still has {{...}} placeholders, they might be agent IDs
	// Replace them with actual outputs
	for _, input := range inputs {
		placeholder := fmt.Sprintf("{{%s}}", input.AgentID)
		finalPrompt = strings.ReplaceAll(finalPrompt, placeholder, input.Output)
	}

	// Build messages with system prompt, optional few-shot examples, and user prompt.
	var messages []providers.Message
	if systemPrompt != "" {
		messages = append(messages, providers.Message{Role: "system", Content: systemPrompt})
	}
	// Inject few-shot examples as user/assistant turn pairs before the real prompt.
	if examples, ok := config["examples"].([]interface{}); ok {
		for _, ex := range examples {
			if exMap, ok := ex.(map[string]interface{}); ok {
				if u, _ := exMap["user"].(string); u != "" {
					messages = append(messages, providers.Message{Role: "user", Content: u})
				}
				if a, _ := exMap["assistant"].(string); a != "" {
					messages = append(messages, providers.Message{Role: "assistant", Content: a})
				}
			}
		}
	}
	messages = append(messages, providers.Message{Role: "user", Content: finalPrompt})

	resp, err := callAggregatorLLM(ctx, aggLLMCall{
		Scope:       "synthesis",
		Model:       model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: temperature,
		Config:      config,
		NodeLabel:   "Synthesis",
		NodeName:    "Synthesis Aggregation",
		SubNodeID:   "synthesis",
	}, llmClient, aggCtx)
	if err != nil {
		return nil, err
	}

	// Best-effort agreement metadata — extraction config is intentionally optional here.
	// Synthesis uses LLM-generated combined answer, not extracted answers. Extraction only
	// feeds diagnostic ComputeAgreement metrics; missing config degrades to no-op extractor.
	extractCfg := ParseExtractorConfig(config)
	agreeRatio, consensus, dissenting := ComputeAgreement(inputs, extractCfg)

	return &AggregationResult{
		Output:          resp.Content,
		Method:          AggMethodSynthesis,
		TokensUsed:      resp.Usage.TotalTokens,
		Cost:            0, // Cost is tracked by providers.Client
		Reasoning:       "Synthesized from " + fmt.Sprintf("%d", len(inputs)) + " agent responses",
		AgreementRatio:  agreeRatio,
		ConsensusAnswer: consensus,
		DissentingIDs:   dissenting,
	}, nil
}

// RegisterSynthesisAggregator registers the synthesis aggregator with the registry
func RegisterSynthesisAggregator(registry *AggregatorRegistry) {
	registry.Register(&SynthesisAggregator{})
}
