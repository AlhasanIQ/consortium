package workflow

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/alhasaniq/consortium/pkg/providers"
)

// campNames are human-readable camp labels used for presentation in the judge
// prompt only. They are deliberately distinct from MCQA answer option letters
// (A, B, C, D) to avoid collision. The judge picks the answer letter directly,
// not the camp name.
var campNames = []string{
	"CampAlpha", "CampBeta", "CampGamma", "CampDelta",
	"CampEpsilon", "CampZeta", "CampEta", "CampTheta",
}

func campPresentationName(index int) string {
	if index < len(campNames) {
		return campNames[index]
	}
	return fmt.Sprintf("Camp%d", index+1)
}

// DebateDecideAggregator groups agents by extracted answer into "camps",
// then uses a single judge call to pick the winning camp.
// Short-circuits when all agents agree (zero LLM cost).
type DebateDecideAggregator struct{}

// Name returns the aggregation method name
func (d *DebateDecideAggregator) Name() AggregationMethod {
	return AggMethodDebateDecide
}

// DefaultDebateDecideSystemPrompt is the system prompt for the camp judge.
const DefaultDebateDecideSystemPrompt = `You are an impartial judge evaluating competing positions on a question. Multiple groups of AI agents have provided arguments for different answers. Your task is to determine which camp's arguments are most convincing.

Evaluate based on:
1. Quality and depth of reasoning
2. Strength of evidence cited
3. Logical consistency

After your analysis, you MUST end your response with exactly one line in this format:
WINNER: [answer]

Where [answer] is the answer option letter (e.g., A, B, C, D) of the camp with the strongest arguments.`

// DefaultDebateDecidePrompt is the user prompt template for camp adjudication.
const DefaultDebateDecidePrompt = `Original Question: {{prompt}}

{{camps}}

Analyze each camp's arguments and select the camp with the strongest reasoning.`

// Aggregate groups agents by extracted answer and adjudicates between camps.
func (d *DebateDecideAggregator) Aggregate(ctx context.Context, inputs []AgentOutput, config map[string]interface{}, llmClient *providers.Client, aggCtx *AggregationContext) (*AggregationResult, error) {
	if single, err := checkSingleInput(inputs, AggMethodDebateDecide); err != nil {
		return nil, err
	} else if single != nil {
		cfg := ParseExtractorConfig(config)
		single.Result.AgreementRatio = 1.0
		single.Result.ConsensusAnswer = ExtractAnswer(inputs[0].Output, cfg)
		return single.Result, nil
	}

	// Extract answers and group into camps
	cfg := ParseExtractorConfig(config)
	camps := make(map[string][]AgentOutput) // answer -> agents in that camp
	campOrder := []string{}                 // preserve insertion order
	var unextracted []AgentOutput

	for _, input := range inputs {
		answer := ExtractAnswer(input.Output, cfg)
		if answer == "" {
			unextracted = append(unextracted, input)
			continue
		}
		if _, exists := camps[answer]; !exists {
			campOrder = append(campOrder, answer)
		}
		camps[answer] = append(camps[answer], input)
	}

	// If no answers extracted, fall back to synthesis (same pattern as majority_vote).
	// This handles free-form answer domains (e.g. math) where the extraction regex
	// is tuned for MCQA single-letter answers and won't match expressions like "27".
	if len(camps) == 0 {
		if llmClient != nil {
			return d.fallbackSynthesis(ctx, inputs, config, llmClient, aggCtx,
				"No discrete answers could be extracted from any agent; delegating to synthesis")
		}
		return nil, fmt.Errorf("debate_decide aggregation failed: no discrete answers could be extracted from any agent")
	}

	// Single camp: unanimous agreement — short-circuit with zero LLM cost
	if len(camps) == 1 {
		answer := campOrder[0]
		members := camps[answer]
		winner := members[0]
		scores := make(map[string]float64)
		for _, m := range members {
			scores[m.AgentID] = 1.0
		}
		for _, u := range unextracted {
			scores[u.AgentID] = 0.0
		}
		return &AggregationResult{
			Output:          winner.Output,
			Method:          AggMethodDebateDecide,
			Winner:          winner.AgentID,
			Scores:          scores,
			Reasoning:       fmt.Sprintf("Unanimous agreement on %q — no judge needed", answer),
			AgreementRatio:  1.0,
			ConsensusAnswer: answer,
		}, nil
	}

	// Multiple camps: build camp briefs and run judge
	if llmClient == nil {
		return nil, fmt.Errorf("LLM client is required for debate_decide with multiple camps")
	}

	// Build camp summaries using presentation-only camp names (CampAlpha, CampBeta, ...)
	// to avoid collision with MCQA answer option letters (A, B, C, D).
	// The judge picks the answer letter directly, validated against campOrder.
	var campsBuilder strings.Builder
	for i, answer := range campOrder {
		name := campPresentationName(i)
		members := camps[answer]
		fmt.Fprintf(&campsBuilder, "### %s (Answer: %s, %d agent(s)):\n", name, answer, len(members))
		for j, member := range members {
			fmt.Fprintf(&campsBuilder, "--- Agent %d argument ---\n%s\n\n", j+1, member.Output)
		}
	}

	// Required configuration.
	model, _ := config["judge_model"].(string)
	if model == "" {
		return nil, fmt.Errorf("debate_decide aggregation requires aggregationConfig.judge_model: %w", ErrAggregationConfig)
	}

	systemPrompt, _ := config["system_prompt"].(string)
	if systemPrompt == "" {
		return nil, fmt.Errorf("debate_decide aggregation requires aggregationConfig.system_prompt: %w", ErrAggregationConfig)
	}

	promptTemplate, _ := config["prompt"].(string)
	if promptTemplate == "" {
		return nil, fmt.Errorf("debate_decide aggregation requires aggregationConfig.prompt: %w", ErrAggregationConfig)
	}

	originalPrompt, _ := config["original_prompt"].(string)

	temperature, ok := aggregationTemperature(config)
	if !ok {
		return nil, fmt.Errorf("debate_decide aggregation requires numeric aggregationConfig.temperature: %w", ErrAggregationConfig)
	}
	maxTokens, ok := aggregationMaxTokens(config)
	if !ok {
		return nil, fmt.Errorf("debate_decide aggregation requires aggregationConfig.max_tokens: %w", ErrAggregationConfig)
	}

	// Build prompt
	finalPrompt := promptTemplate
	finalPrompt = strings.ReplaceAll(finalPrompt, "{{prompt}}", originalPrompt)
	finalPrompt = strings.ReplaceAll(finalPrompt, "{{camps}}", campsBuilder.String())

	resp, err := callAggregatorLLM(ctx, aggLLMCall{
		Scope:        "debate_decide",
		Model:        model,
		SystemPrompt: systemPrompt,
		UserPrompt:   finalPrompt,
		MaxTokens:    maxTokens,
		Temperature:  temperature,
		Config:       config,
		NodeLabel:    "Debate Judge",
		NodeName:     "Debate Camp Adjudication",
		SubNodeID:    "debate_judge",
	}, llmClient, aggCtx)
	if err != nil {
		return nil, err
	}

	// Track cumulative token usage across initial + repair calls
	totalTokensUsed := resp.Usage.TotalTokens

	// Parse winning answer — the judge outputs the answer letter directly (e.g., "B"),
	// validated against campOrder which contains the actual answer options.
	winnerAnswer := parseWinner(resp.Content, campOrder)
	resolution := "direct_parse"

	// If initial parse failed, attempt a repair call (same pattern as judge aggregator)
	if winnerAnswer == "" {
		repairMaxTokens, ok := aggregationRepairMaxTokens(config)
		if !ok {
			return nil, fmt.Errorf("debate_decide aggregation requires aggregationConfig.repair_max_tokens: %w", ErrAggregationConfig)
		}
		repairMessages := []providers.Message{
			{Role: "user", Content: fmt.Sprintf(
				"You previously judged competing camps of arguments. Here is your judgment:\n\n%s\n\n"+
					"Based on your judgment above, return ONLY the answer option letter of the winning camp. "+
					"Valid answers are: %s\n\nReply with nothing else.",
				resp.Content, strings.Join(campOrder, ", "))},
		}

		repairResp, repairErr := callAggregatorLLM(ctx, aggLLMCall{
			Scope:       "debate_decide_repair",
			Model:       model,
			Messages:    repairMessages,
			MaxTokens:   repairMaxTokens,
			Temperature: temperature,
			Config:      config,
			NodeLabel:   "Debate-Repair",
			NodeName:    "Debate Camp Adjudication Repair",
			SubNodeID:   "debate_repair",
		}, llmClient, aggCtx)
		if repairErr == nil {
			totalTokensUsed += responseTotalTokens(repairResp)
			winnerAnswer = parseWinner(repairResp.Content, campOrder)
			if winnerAnswer == "" {
				// Repair response might be just the raw answer letter
				trimmed := strings.TrimSpace(repairResp.Content)
				for _, answer := range campOrder {
					if strings.EqualFold(trimmed, answer) {
						winnerAnswer = answer
						break
					}
				}
			}
			if winnerAnswer != "" {
				resolution = "repair_call"
			}
		} else {
			log.Printf("[debate-decide] repair call failed: %v", repairErr)
		}
	}

	// Hard failure: if both parse and repair failed, return an error so the
	// node retry policy gets a clean signal instead of silently picking the
	// largest camp (which biases toward majority without judge endorsement).
	if winnerAnswer == "" {
		return nil, NewRetryableError(
			fmt.Errorf("debate_decide aggregation failed: could not parse winning camp after repair call (valid answers: %s)", strings.Join(campOrder, ", ")),
			RetryCodeAggParseFailure,
		)
	}

	winningMembers := camps[winnerAnswer]
	winner := winningMembers[0]

	// Build scores: winning camp = 1.0, others = 0.0
	scores := make(map[string]float64)
	for _, input := range inputs {
		scores[input.AgentID] = 0.0
	}
	for _, m := range winningMembers {
		scores[m.AgentID] = 1.0
	}

	// Agreement metadata
	totalExtracted := 0
	for _, members := range camps {
		totalExtracted += len(members)
	}
	ratio := float64(len(winningMembers)) / float64(totalExtracted)
	var dissenting []string
	for _, answer := range campOrder {
		if answer == winnerAnswer {
			continue
		}
		for _, m := range camps[answer] {
			dissenting = append(dissenting, m.AgentID)
		}
	}

	// Build camp name list for reasoning text
	campNameList := make([]string, len(campOrder))
	for i := range campOrder {
		campNameList[i] = campPresentationName(i)
	}

	reasoning := fmt.Sprintf("Debate: %d camps identified (%s). Judge selected answer %s.\n\n%s\n\n[winner_resolution: %s]",
		len(camps),
		strings.Join(campNameList, ", "),
		winnerAnswer,
		resp.Content, resolution)

	return &AggregationResult{
		Output:          winner.Output,
		Method:          AggMethodDebateDecide,
		Winner:          winner.AgentID,
		Scores:          scores,
		TokensUsed:      totalTokensUsed,
		Reasoning:       reasoning,
		AgreementRatio:  ratio,
		ConsensusAnswer: winnerAnswer,
		DissentingIDs:   dissenting,
	}, nil
}

// fallbackSynthesis delegates to the synthesis aggregator when extraction fails.
// Uses default synthesis prompts because the debate_decide system_prompt/prompt
// are for camp adjudication and won't produce a valid synthesis.
func (d *DebateDecideAggregator) fallbackSynthesis(ctx context.Context, inputs []AgentOutput, config map[string]interface{}, llmClient *providers.Client, aggCtx *AggregationContext, reason string) (*AggregationResult, error) {
	synth := &SynthesisAggregator{}
	synthConfig := map[string]interface{}{
		"model":           config["judge_model"],
		"temperature":     config["temperature"],
		"max_tokens":      config["max_tokens"],
		"system_prompt":   DefaultSynthesisSystemPrompt,
		"prompt":          DefaultSynthesisPrompt,
		"original_prompt": config["original_prompt"],
	}
	// Propagate provider routing if present.
	if routing, ok := config["openRouterReasoning"]; ok {
		synthConfig["openRouterReasoning"] = routing
	}
	if provider, ok := config["openRouterProvider"]; ok {
		synthConfig["openRouterProvider"] = provider
	}

	result, err := synth.Aggregate(ctx, inputs, synthConfig, llmClient, aggCtx)
	if err != nil {
		return nil, fmt.Errorf("debate_decide synthesis fallback failed: %w", err)
	}
	result.Method = AggMethodDebateDecide
	result.Reasoning = reason + " — synthesis fallback: " + result.Reasoning
	return result, nil
}
