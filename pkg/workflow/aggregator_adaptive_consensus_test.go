package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func adaptiveJudgeConfig(threshold float64) map[string]interface{} {
	return map[string]interface{}{
		"extraction_strategy":    "regex",
		"extraction_pattern":     DefaultExtractionPattern,
		"short_circuit_threshold": threshold,
		"short_circuit_min_votes":  3,
	}
}

func TestJudgeAdaptiveConsensusSkipsLLMCall(t *testing.T) {
	inputs := []AgentOutput{
		{AgentID: "a", Output: "Final answer: B"},
		{AgentID: "b", Output: "The answer is B."},
		{AgentID: "c", Output: "Answer: B"},
		{AgentID: "d", Output: "Final answer: C"},
	}

	// nil llmClient is intentional: reaching the normal judge path would fail.
	result, err := (&JudgeAggregator{}).Aggregate(
		context.Background(), inputs, adaptiveJudgeConfig(0.75), nil, nil,
	)
	if err != nil {
		t.Fatalf("expected deterministic quorum to skip the judge call: %v", err)
	}
	if result.Winner != "a" || result.ConsensusAnswer != "B" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.AgreementRatio != 0.75 {
		t.Fatalf("expected 0.75 agreement, got %.3f", result.AgreementRatio)
	}
	if result.TokensUsed != 0 || result.Cost != 0 {
		t.Fatalf("short-circuit must not charge aggregation tokens/cost: %+v", result)
	}
	if len(result.DissentingIDs) != 1 || result.DissentingIDs[0] != "d" {
		t.Fatalf("unexpected dissenters: %v", result.DissentingIDs)
	}
	if !strings.Contains(result.Reasoning, "skipped judge LLM call") {
		t.Fatalf("reasoning should expose the optimization: %q", result.Reasoning)
	}
}

func TestJudgeAdaptiveConsensusFallsThroughBelowThreshold(t *testing.T) {
	inputs := []AgentOutput{
		{AgentID: "a", Output: "Final answer: B"},
		{AgentID: "b", Output: "Final answer: B"},
		{AgentID: "c", Output: "Final answer: C"},
		{AgentID: "d", Output: "Final answer: D"},
	}

	_, err := (&JudgeAggregator{}).Aggregate(
		context.Background(), inputs, adaptiveJudgeConfig(0.75), nil, nil,
	)
	if err == nil {
		t.Fatal("below-threshold consensus must continue to the normal judge path")
	}
	if !strings.Contains(err.Error(), "judge_model") {
		t.Fatalf("expected normal judge configuration error after fallthrough, got: %v", err)
	}
}

func TestJudgeAdaptiveConsensusInvalidConfigFailsClosed(t *testing.T) {
	config := adaptiveJudgeConfig(0.75)
	config["short_circuit_threshold"] = 0.2
	inputs := []AgentOutput{
		{AgentID: "a", Output: "Final answer: A"},
		{AgentID: "b", Output: "Final answer: A"},
		{AgentID: "c", Output: "Final answer: A"},
	}

	_, err := (&JudgeAggregator{}).Aggregate(context.Background(), inputs, config, nil, nil)
	if err == nil {
		t.Fatal("invalid quorum config should be rejected")
	}
	if !errors.Is(err, ErrAggregationConfig) {
		t.Fatalf("expected ErrAggregationConfig, got: %v", err)
	}
}

func TestLegacyScoringFastPathRemainsUnanimousOnly(t *testing.T) {
	config := adaptiveJudgeConfig(0.75)
	inputs := []AgentOutput{
		{AgentID: "a", Output: "Final answer: B"},
		{AgentID: "b", Output: "Final answer: B"},
		{AgentID: "c", Output: "Final answer: B"},
		{AgentID: "d", Output: "Final answer: C"},
	}

	if decision, ok := maybeUnanimousAnswerDecision(inputs, config); ok || decision != nil {
		t.Fatal("legacy unanimous helper must stay strict until each evaluator opts into quorum-aware metadata")
	}
}
