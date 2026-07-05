package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/alhasaniq/consortium/pkg/providers"
)

func debateJudgeConfig() map[string]interface{} {
	return withDebateDecideConfig(map[string]interface{}{
		"judge_model":       "mock-model",
		"max_tokens":        -1,
		"repair_max_tokens": 256,
	})
}

func TestDebateDecideAggregatorName(t *testing.T) {
	agg := &DebateDecideAggregator{}
	if agg.Name() != AggMethodDebateDecide {
		t.Errorf("Expected name %s, got %s", AggMethodDebateDecide, agg.Name())
	}
}

func TestDebateDecideAggregatorSingle(t *testing.T) {
	agg := &DebateDecideAggregator{}

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: B\nBecause it's correct."},
	}

	ctx := context.Background()
	result, err := agg.Aggregate(ctx, inputs, debateJudgeConfig(), nil, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result.Winner != "agent-1" {
		t.Errorf("Expected winner agent-1, got %q", result.Winner)
	}
	if result.ConsensusAnswer != "B" {
		t.Errorf("Expected consensus B, got %q", result.ConsensusAnswer)
	}
}

func TestDebateDecideAggregatorUnanimous(t *testing.T) {
	agg := &DebateDecideAggregator{}

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: C"},
		{AgentID: "agent-2", Output: "Final answer: C"},
		{AgentID: "agent-3", Output: "Final answer: C"},
	}

	ctx := context.Background()
	result, err := agg.Aggregate(ctx, inputs, debateJudgeConfig(), nil, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Unanimous — should short-circuit with zero LLM cost
	if result.Winner != "agent-1" {
		t.Errorf("Expected winner agent-1 (first in camp), got %q", result.Winner)
	}
	if result.ConsensusAnswer != "C" {
		t.Errorf("Expected consensus C, got %q", result.ConsensusAnswer)
	}
	if result.AgreementRatio != 1.0 {
		t.Errorf("Expected agreement 1.0, got %f", result.AgreementRatio)
	}
	if result.TokensUsed != 0 {
		t.Errorf("Expected zero tokens (short-circuit), got %d", result.TokensUsed)
	}
	if !strings.Contains(result.Reasoning, "Unanimous") {
		t.Errorf("Expected reasoning to mention unanimous, got %q", result.Reasoning)
	}
}

func TestDebateDecideAggregatorSplitCamps(t *testing.T) {
	agg := &DebateDecideAggregator{}

	registry := providers.NewRegistry()
	mock := NewMockProvider("test")
	mock.SetResponse("impartial judge", "Camp A's arguments are more convincing because they address the core issue.\n\nWINNER: A")
	registry.Register(mock)
	client := NewMockLLMClient(registry)

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: A\nBecause of X."},
		{AgentID: "agent-2", Output: "Final answer: B\nBecause of Y."},
		{AgentID: "agent-3", Output: "Final answer: A\nBecause of Z."},
	}

	config := debateJudgeConfig()

	ctx := context.Background()
	result, err := agg.Aggregate(ctx, inputs, config, client, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.ConsensusAnswer != "A" {
		t.Errorf("Expected consensus A (camp A won), got %q", result.ConsensusAnswer)
	}
	if result.Winner != "agent-1" {
		t.Errorf("Expected winner agent-1 (first in winning camp), got %q", result.Winner)
	}
	if result.Scores["agent-1"] != 1.0 {
		t.Errorf("Expected score 1.0 for agent-1 (winning camp), got %f", result.Scores["agent-1"])
	}
	if result.Scores["agent-2"] != 0.0 {
		t.Errorf("Expected score 0.0 for agent-2 (losing camp), got %f", result.Scores["agent-2"])
	}
	if len(result.DissentingIDs) != 1 || result.DissentingIDs[0] != "agent-2" {
		t.Errorf("Expected dissenting [agent-2], got %v", result.DissentingIDs)
	}
}

func TestDebateDecideAggregatorJudgePicksCampB(t *testing.T) {
	agg := &DebateDecideAggregator{}

	registry := providers.NewRegistry()
	mock := NewMockProvider("test")
	// Judge explicitly picks camp B (the minority camp) over camp A
	mock.SetResponse("impartial judge", "Camp B's argument is more rigorous and identifies a subtle flaw in camp A's reasoning.\n\nWINNER: B")
	registry.Register(mock)
	client := NewMockLLMClient(registry)

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: A\nBecause of X."},
		{AgentID: "agent-2", Output: "Final answer: B\nBecause of Y."},
		{AgentID: "agent-3", Output: "Final answer: A\nBecause of Z."},
	}

	config := debateJudgeConfig()

	ctx := context.Background()
	result, err := agg.Aggregate(ctx, inputs, config, client, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Judge picked camp B — agent-2 should win even though camp A had majority
	if result.ConsensusAnswer != "B" {
		t.Errorf("Expected consensus B (camp B won), got %q", result.ConsensusAnswer)
	}
	if result.Winner != "agent-2" {
		t.Errorf("Expected winner agent-2 (only member of winning camp), got %q", result.Winner)
	}
	if result.Scores["agent-2"] != 1.0 {
		t.Errorf("Expected score 1.0 for agent-2 (winning camp), got %f", result.Scores["agent-2"])
	}
	if result.Scores["agent-1"] != 0.0 {
		t.Errorf("Expected score 0.0 for agent-1 (losing camp), got %f", result.Scores["agent-1"])
	}
	// Agreement ratio should be 1/3 (only 1 of 3 extracted agents in winning camp)
	if result.AgreementRatio < 0.33 || result.AgreementRatio > 0.34 {
		t.Errorf("Expected agreement ~0.33, got %f", result.AgreementRatio)
	}
	if len(result.DissentingIDs) != 2 {
		t.Errorf("Expected 2 dissenting agents, got %v", result.DissentingIDs)
	}
}

func TestDebateDecideAggregatorParseFailureReturnsError(t *testing.T) {
	agg := &DebateDecideAggregator{}

	registry := providers.NewRegistry()
	mock := NewMockProvider("test")
	// No parseable winner marker in either judge or repair call.
	// Avoid words "a" or "b" as standalone tokens since parseWinner matches single-letter labels.
	mock.SetResponse("impartial judge", "The camps present equally compelling points. No clear verdict.")
	mock.SetResponse("return ONLY the answer option letter", "It is impossible to pick one.")
	registry.Register(mock)
	client := NewMockLLMClient(registry)

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: A\nBecause of X."},
		{AgentID: "agent-2", Output: "Final answer: B\nBecause of Y."},
		{AgentID: "agent-3", Output: "Final answer: A\nBecause of Z."},
	}

	config := debateJudgeConfig()

	_, err := agg.Aggregate(context.Background(), inputs, config, client, nil)
	if err == nil {
		t.Fatal("Expected error when winner cannot be parsed after repair call")
	}
	if !strings.Contains(err.Error(), "could not parse winning camp after repair call") {
		t.Fatalf("Expected parse-failure error, got: %v", err)
	}
	var retryErr *RetryableError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected RetryableError, got %T: %v", err, err)
	}
	if retryErr.Code != RetryCodeAggParseFailure {
		t.Fatalf("expected code %s, got %s", RetryCodeAggParseFailure, retryErr.Code)
	}
}

func TestDebateDecideAggregatorNilLLMMultipleCamps(t *testing.T) {
	agg := &DebateDecideAggregator{}

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: A"},
		{AgentID: "agent-2", Output: "Final answer: B"},
	}

	ctx := context.Background()
	_, err := agg.Aggregate(ctx, inputs, debateJudgeConfig(), nil, nil)
	if err == nil {
		t.Fatal("Expected error when no LLM client provided for multiple camps")
	}
	if !strings.Contains(err.Error(), "LLM client is required") {
		t.Errorf("Expected 'LLM client is required' error, got: %s", err.Error())
	}
}

func TestDebateDecideAggregatorEmptyContentWithOutputTokensIsRetryable(t *testing.T) {
	agg := &DebateDecideAggregator{}

	registry := providers.NewRegistry()
	registry.Register(&fixedUsageProvider{
		content:          "",
		promptTokens:     12,
		completionTokens: 8,
	})
	client := NewMockLLMClient(registry)

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: A"},
		{AgentID: "agent-2", Output: "Final answer: B"},
	}

	config := debateJudgeConfig()

	_, err := agg.Aggregate(context.Background(), inputs, config, client, nil)
	if err == nil {
		t.Fatal("expected retryable empty-content error")
	}
	var retryErr *RetryableError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected RetryableError, got %T: %v", err, err)
	}
	if retryErr.Code != RetryCodeOutputTruncatedEmpty {
		t.Fatalf("expected retry code %s, got %s", RetryCodeOutputTruncatedEmpty, retryErr.Code)
	}
}

func TestDebateDecideAggregatorNoExtraction(t *testing.T) {
	agg := &DebateDecideAggregator{}

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "I have no idea."},
		{AgentID: "agent-2", Output: "Cannot determine."},
	}

	ctx := context.Background()
	_, err := agg.Aggregate(ctx, inputs, debateJudgeConfig(), nil, nil)
	if err == nil {
		t.Fatal("Expected error when no discrete answers can be extracted")
	}
	if !strings.Contains(err.Error(), "no discrete answers could be extracted") {
		t.Errorf("Expected extraction-failure error, got: %v", err)
	}
}

func TestDebateDecideAggregatorEmpty(t *testing.T) {
	agg := &DebateDecideAggregator{}

	ctx := context.Background()
	_, err := agg.Aggregate(ctx, []AgentOutput{}, nil, nil, nil)
	if err == nil {
		t.Fatal("Expected error for empty inputs")
	}
}

func TestDebateDecideAggregatorSupportsMoreThanEightCamps(t *testing.T) {
	agg := &DebateDecideAggregator{}

	registry := providers.NewRegistry()
	mock := NewMockProvider("test")
	mock.SetResponse("impartial judge", "After comparing all camps, the strongest answer is I.\n\nWINNER: I")
	registry.Register(mock)
	client := NewMockLLMClient(registry)

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: A"},
		{AgentID: "agent-2", Output: "Final answer: B"},
		{AgentID: "agent-3", Output: "Final answer: C"},
		{AgentID: "agent-4", Output: "Final answer: D"},
		{AgentID: "agent-5", Output: "Final answer: E"},
		{AgentID: "agent-6", Output: "Final answer: F"},
		{AgentID: "agent-7", Output: "Final answer: G"},
		{AgentID: "agent-8", Output: "Final answer: H"},
		{AgentID: "agent-9", Output: "Final answer: I"},
	}

	result, err := agg.Aggregate(context.Background(), inputs, debateJudgeConfig(), client, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result.Winner != "agent-9" {
		t.Fatalf("Expected winner agent-9 for answer I, got %q", result.Winner)
	}
	if result.ConsensusAnswer != "I" {
		t.Fatalf("Expected consensus answer I, got %q", result.ConsensusAnswer)
	}
	if !strings.Contains(result.Reasoning, "Camp9") {
		t.Fatalf("Expected reasoning to include fallback Camp9 label, got %q", result.Reasoning)
	}
}

type debateMaxTokensRecorderProvider struct {
	callMaxTokens []int
}

func (p *debateMaxTokensRecorderProvider) Name() string {
	return "debate-max-tokens-recorder"
}

func (p *debateMaxTokensRecorderProvider) Models() []providers.Model {
	return []providers.Model{{
		ID:         "mock-model",
		Name:       "Mock Model",
		Provider:   p.Name(),
		ContextLen: 8192,
		InputCost:  0,
		OutputCost: 0,
		MaxTokens:  8192,
		Available:  true,
	}}
}

func (p *debateMaxTokensRecorderProvider) Complete(_ context.Context, req *providers.CompletionRequest) (*providers.CompletionResponse, error) {
	p.callMaxTokens = append(p.callMaxTokens, req.MaxTokens)
	content := "No final verdict provided."
	if len(p.callMaxTokens) > 1 {
		content = "B"
	}
	return &providers.CompletionResponse{
		ID:      fmt.Sprintf("debate-max-token-call-%d", len(p.callMaxTokens)),
		Model:   req.Model,
		Content: content,
		Usage: providers.Usage{
			PromptTokens:     10,
			CompletionTokens: 3,
			TotalTokens:      13,
		},
		Finish: "stop",
	}, nil
}

func (p *debateMaxTokensRecorderProvider) EstimateTokens(text string) int {
	return len(text) / 4
}

func (p *debateMaxTokensRecorderProvider) Cost(model string, inputTokens, outputTokens int) float64 {
	return 0
}

func TestDebateDecideAggregatorUsesConfiguredRepairMaxTokens(t *testing.T) {
	agg := &DebateDecideAggregator{}

	registry := providers.NewRegistry()
	recorder := &debateMaxTokensRecorderProvider{}
	registry.Register(recorder)
	client := NewMockLLMClient(registry)

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: A"},
		{AgentID: "agent-2", Output: "Final answer: B"},
		{AgentID: "agent-3", Output: "Final answer: A"},
	}

	config := debateJudgeConfig()
	config["repair_max_tokens"] = 321

	result, err := agg.Aggregate(context.Background(), inputs, config, client, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result.Winner != "agent-2" {
		t.Fatalf("Expected winner agent-2 from repaired answer B, got %q", result.Winner)
	}
	if len(recorder.callMaxTokens) != 2 {
		t.Fatalf("Expected two LLM calls (judge + repair), got %d", len(recorder.callMaxTokens))
	}
	if recorder.callMaxTokens[0] != -1 {
		t.Fatalf("Expected initial judge max_tokens=-1, got %d", recorder.callMaxTokens[0])
	}
	if recorder.callMaxTokens[1] != 321 {
		t.Fatalf("Expected repair max_tokens=321, got %d", recorder.callMaxTokens[1])
	}
}

func TestDebateDecideRegistered(t *testing.T) {
	registry := NewAggregatorRegistry()
	agg, ok := registry.Get(AggMethodDebateDecide)
	if !ok {
		t.Fatal("Expected debate_decide to be registered")
	}
	if agg.Name() != AggMethodDebateDecide {
		t.Errorf("Expected name debate_decide, got %s", agg.Name())
	}
}
