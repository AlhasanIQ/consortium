package workflow

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/alhasaniq/consortium/pkg/providers"
)

// JudgeAggregator uses a single LLM judge to select the best response.
type JudgeAggregator struct{}

// Name returns the aggregation method name
func (j *JudgeAggregator) Name() AggregationMethod {
	return AggMethodJudge
}

// DefaultJudgeSystemPrompt is the default system prompt for the judge persona
const DefaultJudgeSystemPrompt = `You are an impartial judge evaluating AI responses. Your task is to analyze responses and determine which one provides the best answer.

Evaluate each response based on:
1. Accuracy and correctness
2. Completeness
3. Clarity and coherence
4. Relevance to the question

After your analysis, you MUST end your response with exactly one line in this format:
WINNER: [letter]

Where [letter] is the label of the response with the best answer (e.g., A, B, C).`

// DefaultJudgePrompt is the default user prompt template for judging (contains the data)
const DefaultJudgePrompt = `Original Question: {{prompt}}

{{responses}}

Analyze each response and select the best one.`

// Aggregate selects the best response using a single LLM judge
func (j *JudgeAggregator) Aggregate(ctx context.Context, inputs []AgentOutput, config map[string]interface{}, llmClient *providers.Client, aggCtx *AggregationContext) (*AggregationResult, error) {
	if single, err := checkSingleInput(inputs, AggMethodJudge); err != nil {
		return nil, err
	} else if single != nil {
		return single.Result, nil
	}

	// Optional fast path: if all agents produced the same extractable answer,
	// skip judge LLM calls entirely.
	if decision, ok := maybeUnanimousAnswerDecision(inputs, config); ok {
		scores := make(map[string]float64, len(inputs))
		for _, input := range inputs {
			if input.AgentID == decision.WinnerAgentID {
				scores[input.AgentID] = 1.0
			} else {
				scores[input.AgentID] = 0.0
			}
		}
		return &AggregationResult{
			Output:          decision.WinningOutput,
			Method:          AggMethodJudge,
			Winner:          decision.WinnerAgentID,
			Scores:          scores,
			Reasoning:       fmt.Sprintf("Unanimous extracted answer %q — short-circuited judge aggregation", decision.Answer),
			AgreementRatio:  1.0,
			ConsensusAnswer: decision.Answer,
		}, nil
	}

	// Required configuration.
	model, _ := config["judge_model"].(string)
	if model == "" {
		return nil, fmt.Errorf("judge aggregation requires aggregationConfig.judge_model: %w", ErrAggregationConfig)
	}

	systemPrompt, _ := config["system_prompt"].(string)
	if systemPrompt == "" {
		return nil, fmt.Errorf("judge aggregation requires aggregationConfig.system_prompt: %w", ErrAggregationConfig)
	}

	promptTemplate, _ := config["prompt"].(string)
	if promptTemplate == "" {
		return nil, fmt.Errorf("judge aggregation requires aggregationConfig.prompt: %w", ErrAggregationConfig)
	}

	// Get original prompt from config if available
	originalPrompt, _ := config["original_prompt"].(string)

	temperature, ok := aggregationTemperature(config)
	if !ok {
		return nil, fmt.Errorf("judge aggregation requires numeric aggregationConfig.temperature: %w", ErrAggregationConfig)
	}
	maxTokens, ok := aggregationMaxTokens(config)
	if !ok {
		return nil, fmt.Errorf("judge aggregation requires aggregationConfig.max_tokens: %w", ErrAggregationConfig)
	}

	// Build anonymization map and responses section using letter labels
	agentIDs := make([]string, 0, len(inputs))
	for _, input := range inputs {
		agentIDs = append(agentIDs, input.AgentID)
	}
	anonMap := BuildAnonymizationMap(agentIDs)

	var responsesBuilder strings.Builder
	for _, input := range inputs {
		label := anonMap.IDToLabel[input.AgentID]
		responsesBuilder.WriteString(fmt.Sprintf("### Response %s:\n%s\n\n", label, input.Output))
	}

	// Build the final user prompt
	finalPrompt := promptTemplate
	finalPrompt = strings.ReplaceAll(finalPrompt, "{{prompt}}", originalPrompt)
	finalPrompt = strings.ReplaceAll(finalPrompt, "{{responses}}", responsesBuilder.String())

	resp, err := callAggregatorLLM(ctx, aggLLMCall{
		Scope:        "judge",
		Model:        model,
		SystemPrompt: systemPrompt,
		UserPrompt:   finalPrompt,
		MaxTokens:    maxTokens,
		Temperature:  temperature,
		Config:       config,
		NodeLabel:    "Judge",
		NodeName:     "Judge Aggregation",
		SubNodeID:    "judge",
	}, llmClient, aggCtx)
	if err != nil {
		return nil, err
	}

	// Track total token usage across initial + potential repair call
	totalTokensInput := responseTokensInput(resp)
	totalTokensOutput := responseTokensOutput(resp)
	totalTokensUsed := responseTotalTokens(resp)
	totalCost := responseCost(resp)

	// Parse the winner label from the response (using letter labels, not real IDs)
	winnerLabel := parseWinner(resp.Content, anonMap.Labels)
	resolution := "direct_parse"

	// If initial parse failed, attempt a repair call
	if winnerLabel == "" {
		repairMaxTokens, ok := aggregationRepairMaxTokens(config)
		if !ok {
			return nil, fmt.Errorf("judge aggregation requires aggregationConfig.repair_max_tokens: %w", ErrAggregationConfig)
		}
		repairMessages := []providers.Message{
			{Role: "user", Content: fmt.Sprintf(
				"You previously evaluated several AI responses. Here is your evaluation:\n\n%s\n\n"+
					"Based on your evaluation above, return ONLY the letter label of the winning response. "+
					"Valid labels are: %s\n\nReply with nothing else.",
				resp.Content, strings.Join(anonMap.Labels, ", "))},
		}

		repairResp, repairErr := callAggregatorLLM(ctx, aggLLMCall{
			Scope:       "judge_repair",
			Model:       model,
			Messages:    repairMessages,
			MaxTokens:   repairMaxTokens,
			Temperature: temperature,
			Config:      config,
			NodeLabel:   "Judge-Repair",
			NodeName:    "Judge Aggregation Repair",
			SubNodeID:   "judge_repair",
		}, llmClient, aggCtx)
		if repairErr == nil {
			totalTokensInput += responseTokensInput(repairResp)
			totalTokensOutput += responseTokensOutput(repairResp)
			totalTokensUsed += responseTotalTokens(repairResp)
			totalCost += responseCost(repairResp)

			winnerLabel = parseWinner(repairResp.Content, anonMap.Labels)
			if winnerLabel == "" {
				// Repair response might be just the raw label
				trimmed := strings.TrimSpace(repairResp.Content)
				for _, label := range anonMap.Labels {
					if strings.EqualFold(trimmed, label) {
						winnerLabel = label
						break
					}
				}
			}
			if winnerLabel != "" {
				resolution = "repair_call"
			}
		} else {
			log.Printf("[judge-aggregator] repair call failed: %v", repairErr)
		}
	}

	// Hard failure: if both parse and repair failed, return an error so the
	// node retry policy gets a clean signal instead of silently picking the
	// first agent (which would systematically bias toward array position).
	if winnerLabel == "" {
		return nil, NewRetryableError(
			fmt.Errorf("judge aggregation failed: could not parse winner after repair call (valid labels: %s)", strings.Join(anonMap.Labels, ", ")),
			RetryCodeAggParseFailure,
		)
	}

	// Resolve anonymized label back to real agent ID
	winner := anonMap.ResolveLabel(winnerLabel)

	// Find the winning response and build scores
	var winningOutput string
	scores := make(map[string]float64)
	for _, input := range inputs {
		if input.AgentID == winner {
			winningOutput = input.Output
			scores[input.AgentID] = 1.0
		} else {
			scores[input.AgentID] = 0.0
		}
	}

	// Surface resolution method in reasoning
	reasoning := resp.Content
	reasoning += fmt.Sprintf("\n\n[winner_resolution: %s]", resolution)

	// Best-effort agreement metadata — extraction config is intentionally optional here.
	// Judge uses LLM-selected WINNER, not extracted answers. Extraction only feeds
	// diagnostic ComputeAgreement metrics; missing config degrades to no-op extractor.
	extractCfg := ParseExtractorConfig(config)
	agreeRatio, consensus, dissenting := ComputeAgreement(inputs, extractCfg)

	return &AggregationResult{
		Output:          winningOutput,
		Method:          AggMethodJudge,
		Winner:          winner,
		Scores:          scores,
		TokensInput:     totalTokensInput,
		TokensOutput:    totalTokensOutput,
		TokensUsed:      totalTokensUsed,
		Cost:            totalCost,
		Reasoning:       reasoning,
		AgreementRatio:  agreeRatio,
		ConsensusAnswer: consensus,
		DissentingIDs:   dissenting,
	}, nil
}

// parseWinner extracts the winner agent ID from the LLM response.
// Returns empty string if no valid winner can be identified.
func parseWinner(response string, validAgentIDs []string) string {
	lines := strings.Split(response, "\n")

	// Pass 1: case-insensitive match for "WINNER:" prefix followed by agent ID
	for _, line := range lines {
		line = strings.TrimSpace(line)
		upperLine := strings.ToUpper(line)
		if strings.HasPrefix(upperLine, "WINNER:") {
			winnerPart := strings.TrimSpace(line[7:]) // After "WINNER:"
			// Try exact match first (most reliable)
			for _, agentID := range validAgentIDs {
				if strings.EqualFold(winnerPart, agentID) {
					return agentID
				}
			}
			// Try word-boundary match (handles "Camp A", "Response B wins", etc.)
			for _, agentID := range validAgentIDs {
				if containsIDAsWord(winnerPart, agentID) {
					return agentID
				}
			}
		}
	}

	// Pass 2: look for any line containing "winner" followed by an agent ID
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(strings.ToLower(trimmed), "winner") {
			for _, agentID := range validAgentIDs {
				if containsIDAsWord(trimmed, agentID) {
					return agentID
				}
			}
		}
	}

	return ""
}

// containsIDAsWord checks if text contains agentID as a standalone word/token.
// Uses word-boundary matching to avoid false positives (e.g. single-letter labels
// like "A" matching inside words like "analysis").
func containsIDAsWord(text string, agentID string) bool {
	pattern := `(?i)\b` + regexp.QuoteMeta(agentID) + `\b`
	re, err := regexp.Compile(pattern)
	if err != nil {
		return strings.Contains(strings.ToLower(text), strings.ToLower(agentID))
	}
	return re.MatchString(text)
}
