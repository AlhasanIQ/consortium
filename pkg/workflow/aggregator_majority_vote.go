package workflow

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/alhasaniq/consortium/pkg/providers"
)

// MajorityVoteAggregator selects the answer with the most votes across agents.
// Uses the shared answer extraction infrastructure to parse discrete answers
// from each agent's output, then picks the majority/plurality winner.
// Zero LLM cost unless tie fallback triggers synthesis.
type MajorityVoteAggregator struct{}

// Name returns the aggregation method name
func (m *MajorityVoteAggregator) Name() AggregationMethod {
	return AggMethodMajorityVote
}

// Aggregate extracts discrete answers from all inputs and returns the majority winner.
func (m *MajorityVoteAggregator) Aggregate(ctx context.Context, inputs []AgentOutput, config map[string]interface{}, llmClient *providers.Client, aggCtx *AggregationContext) (*AggregationResult, error) {
	if single, err := checkSingleInput(inputs, AggMethodMajorityVote); err != nil {
		return nil, err
	} else if single != nil {
		cfg := ParseExtractorConfig(config)
		single.Result.AgreementRatio = 1.0
		single.Result.ConsensusAnswer = ExtractAnswer(inputs[0].Output, cfg)
		return single.Result, nil
	}
	tieBreaker, err := parseMajorityVoteTieBreaker(config)
	if err != nil {
		return nil, fmt.Errorf("majority_vote config invalid: %w", err)
	}

	// Parse extraction config
	cfg := ParseExtractorConfig(config)

	// Extract answers from all agents
	answers := make(map[string]string)          // agentID -> extracted answer
	answerToAgents := make(map[string][]string) // answer -> []agentID
	for _, input := range inputs {
		answer := ExtractAnswer(input.Output, cfg)
		if answer != "" {
			answers[input.AgentID] = answer
			answerToAgents[answer] = append(answerToAgents[answer], input.AgentID)
		}
	}

	// If no answers could be extracted, fall back to synthesis or error
	if len(answers) == 0 {
		if tieBreaker == "synthesis" && llmClient != nil {
			return m.fallbackSynthesis(ctx, inputs, config, llmClient, aggCtx,
				"No discrete answers could be extracted from any agent response")
		}
		if tieBreaker == "first" {
			return m.fallbackFirst(inputs, "No discrete answers could be extracted; falling back to first response")
		}
		return nil, fmt.Errorf("majority_vote: no discrete answers could be extracted from %d responses", len(inputs))
	}

	// Sort answers by vote count descending, then alphabetically for determinism
	type answerVote struct {
		answer string
		count  int
	}
	sortedAnswers := make([]answerVote, 0, len(answerToAgents))
	for answer, agents := range answerToAgents {
		sortedAnswers = append(sortedAnswers, answerVote{answer, len(agents)})
	}
	sort.Slice(sortedAnswers, func(i, j int) bool {
		if sortedAnswers[i].count != sortedAnswers[j].count {
			return sortedAnswers[i].count > sortedAnswers[j].count
		}
		return sortedAnswers[i].answer < sortedAnswers[j].answer
	})

	bestAnswer := sortedAnswers[0].answer
	bestCount := sortedAnswers[0].count
	tied := len(sortedAnswers) > 1 && sortedAnswers[1].count == bestCount

	// Handle tie
	if tied {
		switch tieBreaker {
		case "synthesis":
			if llmClient != nil {
				return m.fallbackSynthesis(ctx, inputs, config, llmClient, aggCtx,
					"Tie detected between answers; delegating to synthesis")
			}
		case "error":
			return nil, fmt.Errorf("majority_vote: tie between %d answers with %d votes each", len(sortedAnswers), bestCount)
		case "first":
			return m.fallbackFirst(inputs, "Tie detected; falling back to first response")
		}
		// Default: deterministic — bestAnswer is the alphabetically first among tied answers (from sort above)
	}

	// Build scores and find the winning agent's output
	scores := make(map[string]float64)
	var winnerAgent string
	var winningOutput string
	for _, input := range inputs {
		answer, ok := answers[input.AgentID]
		if !ok {
			scores[input.AgentID] = 0.0
			continue
		}
		if answer == bestAnswer {
			scores[input.AgentID] = 1.0
			if winnerAgent == "" {
				winnerAgent = input.AgentID
				winningOutput = input.Output
			}
		} else {
			scores[input.AgentID] = 0.0
		}
	}

	// Build agreement metadata
	ratio := float64(bestCount) / float64(len(answers))
	var dissenting []string
	for _, input := range inputs {
		answer, ok := answers[input.AgentID]
		if !ok {
			continue
		}
		if answer != bestAnswer {
			dissenting = append(dissenting, input.AgentID)
		}
	}

	reasoning := fmt.Sprintf("Majority vote: %d/%d agents answered %q", bestCount, len(answers), bestAnswer)
	if len(dissenting) > 0 {
		reasoning += fmt.Sprintf(" (dissenting: %v)", dissenting)
	}

	return &AggregationResult{
		Output:          winningOutput,
		Method:          AggMethodMajorityVote,
		Winner:          winnerAgent,
		Scores:          scores,
		Reasoning:       reasoning,
		AgreementRatio:  ratio,
		ConsensusAnswer: bestAnswer,
		DissentingIDs:   dissenting,
	}, nil
}

func parseMajorityVoteTieBreaker(config map[string]interface{}) (string, error) {
	if config == nil {
		return "", fmt.Errorf("missing tie_breaker_method; must be one of: synthesis, first, error")
	}

	raw, ok := config["tie_breaker_method"]
	if !ok {
		return "", fmt.Errorf("missing tie_breaker_method; must be one of: synthesis, first, error")
	}
	tieBreaker, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("tie_breaker_method must be a string")
	}
	tieBreaker = strings.ToLower(strings.TrimSpace(tieBreaker))
	if tieBreaker == "" {
		return "", fmt.Errorf("tie_breaker_method must be one of: synthesis, first, error")
	}

	switch tieBreaker {
	case "synthesis", "first", "error":
	default:
		return "", fmt.Errorf("unsupported tie_breaker_method %q; must be one of: synthesis, first, error", tieBreaker)
	}

	if tieBreaker == "synthesis" {
		model, ok := config["tie_breaker_model"].(string)
		if !ok || strings.TrimSpace(model) == "" {
			return "", fmt.Errorf("tie_breaker_method=synthesis requires explicit non-empty aggregationConfig.tie_breaker_model")
		}
		tempRaw, exists := config["tie_breaker_temperature"]
		if !exists {
			return "", fmt.Errorf("tie_breaker_method=synthesis requires explicit aggregationConfig.tie_breaker_temperature")
		}
		if _, ok := numericToFloat64(tempRaw); !ok {
			return "", fmt.Errorf("tie_breaker_method=synthesis requires numeric aggregationConfig.tie_breaker_temperature")
		}
	}

	return tieBreaker, nil
}

// fallbackSynthesis delegates to the synthesis aggregator when tie/extraction fails.
func (m *MajorityVoteAggregator) fallbackSynthesis(ctx context.Context, inputs []AgentOutput, config map[string]interface{}, llmClient *providers.Client, aggCtx *AggregationContext, reason string) (*AggregationResult, error) {
	synth := &SynthesisAggregator{}
	synthConfig := make(map[string]interface{}, len(config)+2)
	for k, v := range config {
		synthConfig[k] = v
	}
	// Map explicit majority-vote tie-breaker synthesis keys into synthesis config keys.
	synthConfig["model"] = config["tie_breaker_model"]
	synthConfig["temperature"] = config["tie_breaker_temperature"]

	result, err := synth.Aggregate(ctx, inputs, synthConfig, llmClient, aggCtx)
	if err != nil {
		return nil, fmt.Errorf("majority_vote synthesis fallback failed: %w", err)
	}
	result.Method = AggMethodMajorityVote
	result.Reasoning = reason + " — synthesis fallback: " + result.Reasoning
	return result, nil
}

// fallbackFirst returns the first agent's response.
func (m *MajorityVoteAggregator) fallbackFirst(inputs []AgentOutput, reason string) (*AggregationResult, error) {
	return &AggregationResult{
		Output:    inputs[0].Output,
		Method:    AggMethodMajorityVote,
		Winner:    inputs[0].AgentID,
		Scores:    map[string]float64{inputs[0].AgentID: 1.0},
		Reasoning: reason,
	}, nil
}
