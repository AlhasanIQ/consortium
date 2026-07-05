package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/alhasaniq/consortium/pkg/providers"
)

func TestMajorityVoteAggregatorName(t *testing.T) {
	agg := &MajorityVoteAggregator{}
	if agg.Name() != AggMethodMajorityVote {
		t.Errorf("Expected name %s, got %s", AggMethodMajorityVote, agg.Name())
	}
}

func TestMajorityVoteAggregatorSingle(t *testing.T) {
	agg := &MajorityVoteAggregator{}

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: B\nBecause it's correct."},
	}
	config := map[string]interface{}{
		"tie_breaker_method": "error",
	}
	config = withMajorityVoteConfig(config)

	ctx := context.Background()
	result, err := agg.Aggregate(ctx, inputs, config, nil, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Winner != "agent-1" {
		t.Errorf("Expected winner agent-1, got %q", result.Winner)
	}
	if result.AgreementRatio != 1.0 {
		t.Errorf("Expected agreement 1.0, got %f", result.AgreementRatio)
	}
	if result.ConsensusAnswer != "B" {
		t.Errorf("Expected consensus B, got %q", result.ConsensusAnswer)
	}
}

func TestMajorityVoteAggregatorUnanimous(t *testing.T) {
	agg := &MajorityVoteAggregator{}

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "After analysis, Final answer: C"},
		{AgentID: "agent-2", Output: "The answer is C"},
		{AgentID: "agent-3", Output: "Final answer: C"},
	}
	config := map[string]interface{}{
		"tie_breaker_method": "error",
	}
	config = withMajorityVoteConfig(config)

	ctx := context.Background()
	result, err := agg.Aggregate(ctx, inputs, config, nil, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.AgreementRatio != 1.0 {
		t.Errorf("Expected agreement 1.0, got %f", result.AgreementRatio)
	}
	if result.ConsensusAnswer != "C" {
		t.Errorf("Expected consensus C, got %q", result.ConsensusAnswer)
	}
	if len(result.DissentingIDs) != 0 {
		t.Errorf("Expected no dissenters, got %v", result.DissentingIDs)
	}
	// Winner should be first agent in input order that voted C
	if result.Winner != "agent-1" {
		t.Errorf("Expected winner agent-1 (first with majority answer), got %q", result.Winner)
	}
}

func TestMajorityVoteAggregatorMajority(t *testing.T) {
	agg := &MajorityVoteAggregator{}

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: A"},
		{AgentID: "agent-2", Output: "Final answer: B"},
		{AgentID: "agent-3", Output: "Final answer: A"},
	}
	config := map[string]interface{}{
		"tie_breaker_method": "error",
	}
	config = withMajorityVoteConfig(config)

	ctx := context.Background()
	result, err := agg.Aggregate(ctx, inputs, config, nil, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.ConsensusAnswer != "A" {
		t.Errorf("Expected consensus A, got %q", result.ConsensusAnswer)
	}
	if result.Winner != "agent-1" {
		t.Errorf("Expected winner agent-1 (first voter for A), got %q", result.Winner)
	}
	if len(result.DissentingIDs) != 1 || result.DissentingIDs[0] != "agent-2" {
		t.Errorf("Expected dissenting [agent-2], got %v", result.DissentingIDs)
	}
	// Check scores: A voters get 1.0, B voter gets 0.0
	if result.Scores["agent-1"] != 1.0 {
		t.Errorf("Expected score 1.0 for agent-1, got %f", result.Scores["agent-1"])
	}
	if result.Scores["agent-2"] != 0.0 {
		t.Errorf("Expected score 0.0 for agent-2, got %f", result.Scores["agent-2"])
	}
}

func TestMajorityVoteAggregatorExtractionFailureFallbackFirst(t *testing.T) {
	agg := &MajorityVoteAggregator{}

	// No extractable answers
	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "I have no idea what the answer is."},
		{AgentID: "agent-2", Output: "Cannot determine the correct option."},
	}

	config := map[string]interface{}{
		"tie_breaker_method": "first",
	}
	config = withMajorityVoteConfig(config)

	ctx := context.Background()
	result, err := agg.Aggregate(ctx, inputs, config, nil, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Winner != "agent-1" {
		t.Errorf("Expected fallback winner agent-1, got %q", result.Winner)
	}
}

func TestMajorityVoteAggregatorExtractionFailureError(t *testing.T) {
	agg := &MajorityVoteAggregator{}

	// No extractable answers, strict error mode
	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "I have no idea"},
		{AgentID: "agent-2", Output: "Cannot determine"},
	}

	config := map[string]interface{}{
		"tie_breaker_method": "error",
	}
	config = withMajorityVoteConfig(config)

	ctx := context.Background()
	_, err := agg.Aggregate(ctx, inputs, config, nil, nil)
	if err == nil {
		t.Fatal("Expected error when no answers can be extracted with error tie_breaker_method")
	}
}

func TestMajorityVoteAggregatorPartialExtraction(t *testing.T) {
	agg := &MajorityVoteAggregator{}

	// 3 agents: 2 extractable (both say A), 1 unextractable
	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: A\nBecause of X."},
		{AgentID: "agent-2", Output: "I have no idea what the answer is."},
		{AgentID: "agent-3", Output: "Final answer: A\nBecause of Z."},
	}
	config := map[string]interface{}{
		"tie_breaker_method": "error",
	}
	config = withMajorityVoteConfig(config)

	ctx := context.Background()
	result, err := agg.Aggregate(ctx, inputs, config, nil, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.ConsensusAnswer != "A" {
		t.Errorf("Expected consensus A, got %q", result.ConsensusAnswer)
	}
	// Agreement ratio based on extracted answers only (2/2 = 1.0)
	if result.AgreementRatio != 1.0 {
		t.Errorf("Expected agreement 1.0 (both extracted answers agree), got %f", result.AgreementRatio)
	}
	// agent-2 can't be extracted → score 0.0
	if result.Scores["agent-2"] != 0.0 {
		t.Errorf("Expected score 0.0 for unextractable agent-2, got %f", result.Scores["agent-2"])
	}
	if result.Winner != "agent-1" {
		t.Errorf("Expected winner agent-1 (first extracted A voter), got %q", result.Winner)
	}
}

func TestMajorityVoteAggregatorMissingTieBreaker(t *testing.T) {
	agg := &MajorityVoteAggregator{}

	// A and B tie at 1 vote each, missing tie_breaker_method should fail validation.
	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: B"},
		{AgentID: "agent-2", Output: "Final answer: A"},
	}

	ctx := context.Background()
	_, err := agg.Aggregate(ctx, inputs, nil, nil, nil)
	if err == nil {
		t.Fatal("Expected error when tie_breaker_method is not set")
	}
	if !strings.Contains(err.Error(), "tie_breaker_method") {
		t.Fatalf("Expected error to mention tie_breaker_method, got: %v", err)
	}
}

func TestMajorityVoteAggregatorTieErrorBreaker(t *testing.T) {
	agg := &MajorityVoteAggregator{}

	// A and B tie at 1 vote each
	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: A"},
		{AgentID: "agent-2", Output: "Final answer: B"},
	}

	config := map[string]interface{}{
		"tie_breaker_method": "error",
	}
	config = withMajorityVoteConfig(config)

	ctx := context.Background()
	_, err := agg.Aggregate(ctx, inputs, config, nil, nil)
	if err == nil {
		t.Fatal("Expected error when tie_breaker_method is 'error' and votes are tied")
	}
	if !strings.Contains(err.Error(), "tie") {
		t.Errorf("Expected error message to mention 'tie', got: %v", err)
	}
}

func TestMajorityVoteAggregatorTieFirstBreaker(t *testing.T) {
	agg := &MajorityVoteAggregator{}

	// A and B tie at 1 vote each
	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: B"},
		{AgentID: "agent-2", Output: "Final answer: A"},
	}

	config := map[string]interface{}{
		"tie_breaker_method": "first",
	}
	config = withMajorityVoteConfig(config)

	ctx := context.Background()
	result, err := agg.Aggregate(ctx, inputs, config, nil, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	// "first" should pick agent-1 (first in input order)
	if result.Winner != "agent-1" {
		t.Errorf("Expected winner agent-1 (first input), got %q", result.Winner)
	}
}

func TestMajorityVoteAggregatorEmpty(t *testing.T) {
	agg := &MajorityVoteAggregator{}

	ctx := context.Background()
	_, err := agg.Aggregate(ctx, []AgentOutput{}, nil, nil, nil)
	if err == nil {
		t.Fatal("Expected error for empty inputs")
	}
}

func TestMajorityVoteAggregatorTieSynthesisFallback(t *testing.T) {
	agg := &MajorityVoteAggregator{}

	// A and B tie at 1 vote each
	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: A"},
		{AgentID: "agent-2", Output: "Final answer: B"},
	}

	// Provide an LLM client so synthesis fallback can work
	registry := providers.NewRegistry()
	mock := NewMockProvider("test")
	mock.SetResponse("synthesis agent", "After careful review, the answer is A.")
	registry.Register(mock)
	client := NewMockLLMClient(registry)

	config := map[string]interface{}{
		"tie_breaker_method":      "synthesis",
		"tie_breaker_model":       "mock-model",
		"tie_breaker_temperature": 0.0,
	}
	config = withMajorityVoteConfig(config)

	ctx := context.Background()
	result, err := agg.Aggregate(ctx, inputs, config, client, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Method != AggMethodMajorityVote {
		t.Errorf("Expected method majority_vote, got %s", result.Method)
	}
	if !strings.Contains(result.Reasoning, "synthesis fallback") {
		t.Errorf("Expected reasoning to mention synthesis fallback, got %q", result.Reasoning)
	}
}

func TestMajorityVoteAggregatorTieSynthesisMissingModelOrTemp(t *testing.T) {
	agg := &MajorityVoteAggregator{}
	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: A"},
		{AgentID: "agent-2", Output: "Final answer: B"},
	}

	ctx := context.Background()

	_, err := agg.Aggregate(ctx, inputs, map[string]interface{}{
		"tie_breaker_method":      "synthesis",
		"tie_breaker_temperature": 0.0,
	}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "aggregationConfig.tie_breaker_model") {
		t.Fatalf("Expected missing model validation error, got: %v", err)
	}

	_, err = agg.Aggregate(ctx, inputs, map[string]interface{}{
		"tie_breaker_method": "synthesis",
		"tie_breaker_model":  "mock-model",
	}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "aggregationConfig.tie_breaker_temperature") {
		t.Fatalf("Expected missing temperature validation error, got: %v", err)
	}
}

func TestJudgeAggregatorAgreementMetadata(t *testing.T) {
	agg := &JudgeAggregator{}

	registry := providers.NewRegistry()
	mock := NewMockProvider("test")
	mock.SetResponse("impartial judge", "Response B is clearly better.\n\nWINNER: B")
	registry.Register(mock)
	client := NewMockLLMClient(registry)

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: A"},
		{AgentID: "agent-2", Output: "Final answer: A"},
		{AgentID: "agent-3", Output: "Final answer: B"},
	}

	config := withJudgeConfig(map[string]interface{}{
		"judge_model": "mock-model",
	})

	ctx := context.Background()
	result, err := agg.Aggregate(ctx, inputs, config, client, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Agreement should reflect the agents' answers (2/3 say A), not the judge decision
	if result.AgreementRatio < 0.66 || result.AgreementRatio > 0.67 {
		t.Errorf("Expected agreement ~0.67, got %f", result.AgreementRatio)
	}
	if result.ConsensusAnswer != "A" {
		t.Errorf("Expected consensus A, got %q", result.ConsensusAnswer)
	}
}

func TestMajorityVoteRegistered(t *testing.T) {
	registry := NewAggregatorRegistry()
	agg, ok := registry.Get(AggMethodMajorityVote)
	if !ok {
		t.Fatal("Expected majority_vote to be registered")
	}
	if agg.Name() != AggMethodMajorityVote {
		t.Errorf("Expected name majority_vote, got %s", agg.Name())
	}
}
