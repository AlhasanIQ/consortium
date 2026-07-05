package workflow

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/alhasaniq/consortium/pkg/providers"
)

func TestAggregatorRegistry(t *testing.T) {
	registry := NewAggregatorRegistry()

	// Test that all standard aggregators are registered
	methods := []AggregationMethod{
		AggMethodCollect,
		AggMethodSynthesis,
		AggMethodJudge,
		AggMethodScoring,
		AggMethodPeerMatrix,
		AggMethodMajorityVote,
		AggMethodDebateDecide,
	}

	for _, method := range methods {
		agg, ok := registry.Get(method)
		if !ok {
			t.Errorf("Expected aggregator for method %s to be registered", method)
		}
		if agg == nil {
			t.Errorf("Expected non-nil aggregator for method %s", method)
		}
		if agg.Name() != method {
			t.Errorf("Expected aggregator name %s, got %s", method, agg.Name())
		}
	}

	// Test unknown method
	_, ok := registry.Get("unknown")
	if ok {
		t.Error("Expected unknown method to not be registered")
	}
}

func TestCollectAggregatorDefault(t *testing.T) {
	agg := &CollectAggregator{}

	if agg.Name() != AggMethodCollect {
		t.Errorf("Expected name %s, got %s", AggMethodCollect, agg.Name())
	}

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Response 1"},
		{AgentID: "agent-2", Output: "Response 2"},
		{AgentID: "agent-3", Output: "Response 3"},
	}

	ctx := context.Background()
	result, err := agg.Aggregate(ctx, inputs, nil, nil, nil)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Method != AggMethodCollect {
		t.Errorf("Expected method %s, got %s", AggMethodCollect, result.Method)
	}

	// Default behavior joins all outputs with separator
	for _, input := range inputs {
		if !strings.Contains(result.Output, input.Output) {
			t.Errorf("Expected output to contain %q", input.Output)
		}
	}
}

func TestCollectAggregatorCustomSeparator(t *testing.T) {
	agg := &CollectAggregator{}

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "A"},
		{AgentID: "agent-2", Output: "B"},
	}

	config := map[string]interface{}{
		"separator": " | ",
	}

	ctx := context.Background()
	result, err := agg.Aggregate(ctx, inputs, config, nil, nil)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Output != "A | B" {
		t.Errorf("Expected 'A | B', got %q", result.Output)
	}
}

func TestCollectAggregatorEmpty(t *testing.T) {
	agg := &CollectAggregator{}

	ctx := context.Background()
	_, err := agg.Aggregate(ctx, []AgentOutput{}, nil, nil, nil)

	if err == nil {
		t.Fatal("Expected error for empty inputs, got nil")
	}
}

func TestCollectAggregatorSingle(t *testing.T) {
	agg := &CollectAggregator{}

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Only response"},
	}

	ctx := context.Background()
	result, err := agg.Aggregate(ctx, inputs, nil, nil, nil)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Output != "Only response" {
		t.Errorf("Expected 'Only response', got %q", result.Output)
	}
}

func TestJudgeAggregatorNoClient(t *testing.T) {
	agg := &JudgeAggregator{}

	if agg.Name() != AggMethodJudge {
		t.Errorf("Expected name %s, got %s", AggMethodJudge, agg.Name())
	}

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Response 1"},
		{AgentID: "agent-2", Output: "Response 2"},
	}

	ctx := context.Background()
	_, err := agg.Aggregate(ctx, inputs, nil, nil, nil)

	// Should fail without LLM client
	if err == nil {
		t.Error("Expected error when no LLM client provided for judge")
	}
}

func TestJudgeAggregatorShortCircuitsOnUnanimousAnswer(t *testing.T) {
	agg := &JudgeAggregator{}

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: B\nReasoning: one"},
		{AgentID: "agent-2", Output: "Final answer: B\nReasoning: two"},
	}
	config := withJudgeConfig(map[string]interface{}{
		"extraction_strategy": "regex",
		"max_tokens":          -1,
		"repair_max_tokens":   256,
	})

	result, err := agg.Aggregate(context.Background(), inputs, config, nil, nil)
	if err != nil {
		t.Fatalf("expected unanimous short-circuit to succeed without LLM client, got: %v", err)
	}
	if result.Winner != "agent-1" {
		t.Fatalf("expected first agent to win deterministic short-circuit, got %q", result.Winner)
	}
	if result.ConsensusAnswer != "B" {
		t.Fatalf("expected consensus answer B, got %q", result.ConsensusAnswer)
	}
	if result.AgreementRatio != 1.0 {
		t.Fatalf("expected agreement ratio 1.0, got %f", result.AgreementRatio)
	}
	if !strings.Contains(result.Reasoning, "short-circuited judge aggregation") {
		t.Fatalf("expected short-circuit reasoning, got %q", result.Reasoning)
	}
}

func TestJudgeAggregatorEmptyContentWithOutputTokensIsRetryable(t *testing.T) {
	agg := &JudgeAggregator{}

	registry := providers.NewRegistry()
	registry.Register(&fixedUsageProvider{
		content:          "",
		promptTokens:     12,
		completionTokens: 7,
	})
	client := NewMockLLMClient(registry)

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Response 1"},
		{AgentID: "agent-2", Output: "Response 2"},
	}
	config := withJudgeConfig(map[string]interface{}{
		"judge_model":       "mock-model",
		"max_tokens":        -1,
		"repair_max_tokens": 256,
	})

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

func TestJudgeAggregatorDirectParse(t *testing.T) {
	agg := &JudgeAggregator{}

	// Mock returns WINNER: B (anonymized label). Agent-1=A, Agent-2=B.
	registry := providers.NewRegistry()
	mock := NewMockProvider("test")
	mock.SetResponse("impartial judge", "After careful analysis, Response B provides a better answer.\n\nWINNER: B")
	registry.Register(mock)
	client := NewMockLLMClient(registry)

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Response 1", Metadata: map[string]interface{}{}},
		{AgentID: "agent-2", Output: "Response 2", Metadata: map[string]interface{}{}},
	}

	config := withJudgeConfig(map[string]interface{}{
		"judge_model":       "mock-model",
		"max_tokens":        -1,
		"repair_max_tokens": 256,
	})

	ctx := context.Background()
	result, err := agg.Aggregate(ctx, inputs, config, client, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Winner != "agent-2" {
		t.Errorf("Expected winner agent-2 (label B), got %q", result.Winner)
	}
	if result.Scores["agent-2"] != 1.0 {
		t.Errorf("Expected winning score 1.0, got %f", result.Scores["agent-2"])
	}
	if result.Scores["agent-1"] != 0.0 {
		t.Errorf("Expected losing score 0.0, got %f", result.Scores["agent-1"])
	}
	if !strings.Contains(result.Reasoning, "[winner_resolution: direct_parse]") {
		t.Errorf("Expected reasoning to contain direct_parse resolution, got %q", result.Reasoning)
	}
}

func TestJudgeAggregatorFailsOnUnparseableWinner(t *testing.T) {
	agg := &JudgeAggregator{}

	// Mock returns gibberish (no WINNER line, no labels). Both parse and repair fail.
	registry := providers.NewRegistry()
	mock := NewMockProvider("test")
	mock.SetResponse("impartial judge", "I cannot decide, all responses are equally good.")
	mock.SetResponse("return ONLY the letter label", "I really cannot pick one.")
	registry.Register(mock)
	client := NewMockLLMClient(registry)

	inputs := []AgentOutput{
		{AgentID: "charlie", Output: "Response C", Metadata: map[string]interface{}{}},
		{AgentID: "alpha", Output: "Response A", Metadata: map[string]interface{}{}},
		{AgentID: "bravo", Output: "Response B", Metadata: map[string]interface{}{}},
	}

	config := withJudgeConfig(map[string]interface{}{
		"judge_model":       "mock-model",
		"max_tokens":        -1,
		"repair_max_tokens": 256,
	})

	ctx := context.Background()
	_, err := agg.Aggregate(ctx, inputs, config, client, nil)
	if err == nil {
		t.Fatal("Expected error when winner cannot be parsed after repair, got nil")
	}
	if !strings.Contains(err.Error(), "could not parse winner after repair call") {
		t.Errorf("Expected parse-failure error message, got: %v", err)
	}
	var retryErr *RetryableError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected RetryableError, got %T: %v", err, err)
	}
	if retryErr.Code != RetryCodeAggParseFailure {
		t.Fatalf("expected code %s, got %s", RetryCodeAggParseFailure, retryErr.Code)
	}
}

func TestJudgeAggregatorRepairCallSuccess(t *testing.T) {
	agg := &JudgeAggregator{}

	// Mock: initial judge call matches "impartial judge" -> gibberish.
	// Repair call matches "return ONLY the letter label" -> raw label "B".
	registry := providers.NewRegistry()
	mock := NewMockProvider("test")
	mock.SetResponse("impartial judge", "Both responses are excellent. I find it very hard to choose.")
	mock.SetResponse("return ONLY the letter label", "B")
	registry.Register(mock)
	client := NewMockLLMClient(registry)

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Response 1", Metadata: map[string]interface{}{}},
		{AgentID: "agent-2", Output: "Response 2", Metadata: map[string]interface{}{}},
	}

	config := withDebateDecideConfig(map[string]interface{}{
		"judge_model":       "mock-model",
		"max_tokens":        -1,
		"repair_max_tokens": 256,
	})

	ctx := context.Background()
	result, err := agg.Aggregate(ctx, inputs, config, client, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Winner != "agent-2" {
		t.Errorf("Expected winner agent-2 (label B) via repair call, got %q", result.Winner)
	}
	if result.Output != "Response 2" {
		t.Errorf("Expected output 'Response 2', got %q", result.Output)
	}
	if !strings.Contains(result.Reasoning, "[winner_resolution: repair_call]") {
		t.Errorf("Expected reasoning to contain repair_call resolution, got %q", result.Reasoning)
	}
}

func TestJudgeAggregatorUsesConfiguredRepairMaxTokens(t *testing.T) {
	agg := &JudgeAggregator{}

	registry := providers.NewRegistry()
	recorder := &maxTokensRecorderProvider{
		responses: []string{
			"I cannot decide from these responses.",
			"B",
		},
	}
	registry.Register(recorder)
	client := NewMockLLMClient(registry)

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Response 1", Metadata: map[string]interface{}{}},
		{AgentID: "agent-2", Output: "Response 2", Metadata: map[string]interface{}{}},
	}

	config := withJudgeConfig(map[string]interface{}{
		"judge_model":       "mock-model",
		"max_tokens":        -1,
		"repair_max_tokens": 321,
	})

	result, err := agg.Aggregate(context.Background(), inputs, config, client, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	t.Logf("peer-matrix debug: method=%s winner=%s output=%q scores=%v eval_nil=%v reasoning=%s",
		result.Method, result.Winner, result.Output, result.Scores, result.EvalMatrix == nil, result.Reasoning)
	if result.Winner != "agent-2" {
		t.Fatalf("Expected winner agent-2 from repair response B, got %q", result.Winner)
	}
	if len(recorder.calls) != 2 {
		t.Fatalf("Expected two LLM calls (judge + repair), got %d", len(recorder.calls))
	}
	if recorder.calls[0] != -1 {
		t.Fatalf("Expected initial judge max_tokens=-1, got %d", recorder.calls[0])
	}
	if recorder.calls[1] != 321 {
		t.Fatalf("Expected repair max_tokens=321, got %d", recorder.calls[1])
	}
}

func TestScoringAggregatorSingle(t *testing.T) {
	agg := &ScoringAggregator{}

	if agg.Name() != AggMethodScoring {
		t.Errorf("Expected name %s, got %s", AggMethodScoring, agg.Name())
	}

	// Single input wins by default without requiring LLM
	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Response 1"},
	}

	ctx := context.Background()
	result, err := agg.Aggregate(ctx, inputs, nil, nil, nil)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Winner != "agent-1" {
		t.Errorf("Expected winner 'agent-1', got %q", result.Winner)
	}
}

func TestScoringAggregatorMultipleNeedsLLM(t *testing.T) {
	agg := &ScoringAggregator{}

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Response 1"},
		{AgentID: "agent-2", Output: "Response 2"},
	}

	ctx := context.Background()
	_, err := agg.Aggregate(ctx, inputs, nil, nil, nil)

	// Should fail without LLM client for multiple inputs
	if err == nil {
		t.Error("Expected error when no LLM client provided for scoring multiple inputs")
	}
}

func TestScoringAggregatorShortCircuitsOnUnanimousAnswer(t *testing.T) {
	agg := &ScoringAggregator{}

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: C\nReasoning: one"},
		{AgentID: "agent-2", Output: "Final answer: C\nReasoning: two"},
		{AgentID: "agent-3", Output: "Final answer: C\nReasoning: three"},
	}
	config := withScoringConfig(map[string]interface{}{
		"extraction_strategy": "regex",
		"max_tokens":          -1,
	})

	result, err := agg.Aggregate(context.Background(), inputs, config, nil, nil)
	if err != nil {
		t.Fatalf("expected unanimous short-circuit to succeed without LLM client, got: %v", err)
	}
	if result.Winner != "agent-1" {
		t.Fatalf("expected first agent to win deterministic short-circuit, got %q", result.Winner)
	}
	if result.ConsensusAnswer != "C" {
		t.Fatalf("expected consensus answer C, got %q", result.ConsensusAnswer)
	}
	if result.AgreementRatio != 1.0 {
		t.Fatalf("expected agreement ratio 1.0, got %f", result.AgreementRatio)
	}
	for _, input := range inputs {
		if result.Scores[input.AgentID] != 1.0 {
			t.Fatalf("expected short-circuit score 1.0 for %s, got %f", input.AgentID, result.Scores[input.AgentID])
		}
	}
	if !strings.Contains(result.Reasoning, "short-circuited scoring aggregation") {
		t.Fatalf("expected short-circuit reasoning, got %q", result.Reasoning)
	}
}

type flakyScoringProvider struct {
	failuresBeforeSuccess map[string]int
	attemptsByKey         map[string]int
}

func newFlakyScoringProvider(failures map[string]int) *flakyScoringProvider {
	return &flakyScoringProvider{
		failuresBeforeSuccess: failures,
		attemptsByKey:         make(map[string]int),
	}
}

func (p *flakyScoringProvider) Name() string { return "flaky-scoring" }

func (p *flakyScoringProvider) Models() []providers.Model {
	return []providers.Model{{
		ID:         "mock-model",
		Name:       "Mock Model",
		Provider:   "flaky-scoring",
		ContextLen: 8192,
		InputCost:  0,
		OutputCost: 0,
		MaxTokens:  1024,
		Available:  true,
	}}
}

func (p *flakyScoringProvider) Complete(_ context.Context, req *providers.CompletionRequest) (*providers.CompletionResponse, error) {
	var joined strings.Builder
	for _, m := range req.Messages {
		joined.WriteString(m.Content)
		joined.WriteByte('\n')
	}
	key := "default"
	content := joined.String()
	if strings.Contains(content, "Response to evaluate:\nResponse 1") {
		key = "agent-1"
	}
	if strings.Contains(content, "Response to evaluate:\nResponse 2") {
		key = "agent-2"
	}

	p.attemptsByKey[key]++
	if p.attemptsByKey[key] <= p.failuresBeforeSuccess[key] {
		return nil, &providers.ProviderError{
			Code:       providers.ErrCodeUpstreamError,
			Message:    "simulated transient upstream failure",
			Provider:   "openrouter",
			Model:      req.Model,
			Retryable:  true,
			ErrorPhase: "http_read",
		}
	}

	response := `{"scores":{"accuracy":8,"completeness":7,"clarity":7,"relevance":8}}`
	if key == "agent-2" {
		response = `{"scores":{"accuracy":9,"completeness":8,"clarity":8,"relevance":9}}`
	}

	return &providers.CompletionResponse{
		ID:      fmt.Sprintf("resp-%s-%d", key, p.attemptsByKey[key]),
		Model:   req.Model,
		Content: response,
		Usage: providers.Usage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
		Finish: "stop",
	}, nil
}

func (p *flakyScoringProvider) EstimateTokens(text string) int {
	return len(text) / 4
}

func (p *flakyScoringProvider) Cost(model string, inputTokens, outputTokens int) float64 {
	return 0
}

type fixedUsageProvider struct {
	name             string
	content          string
	promptTokens     int
	completionTokens int
}

func (p *fixedUsageProvider) Name() string {
	if p.name == "" {
		return "fixed-usage"
	}
	return p.name
}

func (p *fixedUsageProvider) Models() []providers.Model {
	return []providers.Model{{
		ID:         "mock-model",
		Name:       "Mock Model",
		Provider:   p.Name(),
		ContextLen: 8192,
		InputCost:  0,
		OutputCost: 0,
		MaxTokens:  1024,
		Available:  true,
	}}
}

func (p *fixedUsageProvider) Complete(_ context.Context, req *providers.CompletionRequest) (*providers.CompletionResponse, error) {
	promptTokens := p.promptTokens
	if promptTokens <= 0 {
		promptTokens = 10
	}
	completionTokens := p.completionTokens
	if completionTokens < 0 {
		completionTokens = 0
	}

	return &providers.CompletionResponse{
		ID:      "fixed-usage-response",
		Model:   req.Model,
		Content: p.content,
		Usage: providers.Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
		Finish: "stop",
	}, nil
}

func (p *fixedUsageProvider) EstimateTokens(text string) int {
	return len(text) / 4
}

func (p *fixedUsageProvider) Cost(model string, inputTokens, outputTokens int) float64 {
	return 0
}

type maxTokensRecorderProvider struct {
	responses []string
	calls     []int
}

func (p *maxTokensRecorderProvider) Name() string { return "max-tokens-recorder" }

func (p *maxTokensRecorderProvider) Models() []providers.Model {
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

func (p *maxTokensRecorderProvider) Complete(_ context.Context, req *providers.CompletionRequest) (*providers.CompletionResponse, error) {
	p.calls = append(p.calls, req.MaxTokens)
	content := "No winner."
	if len(p.responses) > 0 {
		idx := len(p.calls) - 1
		if idx >= len(p.responses) {
			idx = len(p.responses) - 1
		}
		content = p.responses[idx]
	}
	return &providers.CompletionResponse{
		ID:      fmt.Sprintf("max-token-call-%d", len(p.calls)),
		Model:   req.Model,
		Content: content,
		Usage: providers.Usage{
			PromptTokens:     10,
			CompletionTokens: 4,
			TotalTokens:      14,
		},
		Finish: "stop",
	}, nil
}

func (p *maxTokensRecorderProvider) EstimateTokens(text string) int {
	return len(text) / 4
}

func (p *maxTokensRecorderProvider) Cost(model string, inputTokens, outputTokens int) float64 {
	return 0
}

type scriptedScoringStep struct {
	content          string
	completionTokens int
}

type scriptedScoringProvider struct {
	scripts       map[string][]scriptedScoringStep
	attemptsByKey map[string]int
}

func newScriptedScoringProvider(scripts map[string][]scriptedScoringStep) *scriptedScoringProvider {
	return &scriptedScoringProvider{
		scripts:       scripts,
		attemptsByKey: make(map[string]int),
	}
}

func (p *scriptedScoringProvider) Name() string { return "scripted-scoring" }

func (p *scriptedScoringProvider) Models() []providers.Model {
	return []providers.Model{{
		ID:         "mock-model",
		Name:       "Mock Model",
		Provider:   "scripted-scoring",
		ContextLen: 8192,
		InputCost:  0,
		OutputCost: 0,
		MaxTokens:  1024,
		Available:  true,
	}}
}

func (p *scriptedScoringProvider) Complete(_ context.Context, req *providers.CompletionRequest) (*providers.CompletionResponse, error) {
	var joined strings.Builder
	for _, m := range req.Messages {
		joined.WriteString(m.Content)
		joined.WriteByte('\n')
	}
	key := "default"
	content := joined.String()
	if strings.Contains(content, "Response to evaluate:\nResponse 1") {
		key = "agent-1"
	}
	if strings.Contains(content, "Response to evaluate:\nResponse 2") {
		key = "agent-2"
	}

	p.attemptsByKey[key]++

	steps := p.scripts[key]
	if len(steps) == 0 {
		steps = []scriptedScoringStep{{content: `{"scores":{"accuracy":8,"completeness":8,"clarity":8,"relevance":8}}`, completionTokens: 20}}
	}
	idx := p.attemptsByKey[key] - 1
	if idx >= len(steps) {
		idx = len(steps) - 1
	}
	step := steps[idx]

	promptTokens := 10
	completionTokens := step.completionTokens
	if completionTokens < 0 {
		completionTokens = 0
	}

	return &providers.CompletionResponse{
		ID:      fmt.Sprintf("scripted-%s-%d", key, p.attemptsByKey[key]),
		Model:   req.Model,
		Content: step.content,
		Usage: providers.Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
		Finish: "stop",
	}, nil
}

func (p *scriptedScoringProvider) EstimateTokens(text string) int {
	return len(text) / 4
}

func (p *scriptedScoringProvider) Cost(model string, inputTokens, outputTokens int) float64 {
	return 0
}

func TestScoringAggregatorRetriesSubcallsAndSucceeds(t *testing.T) {
	agg := &ScoringAggregator{}

	registry := providers.NewRegistry()
	flaky := newFlakyScoringProvider(map[string]int{
		"agent-1": 1,
		"agent-2": 1,
	})
	registry.Register(flaky)
	client := NewMockLLMClient(registry)

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Response 1"},
		{AgentID: "agent-2", Output: "Response 2"},
	}
	config := withScoringConfig(map[string]interface{}{
		"scoring_model":                "mock-model",
		"max_tokens":                   -1,
		"subcall_retry_max_attempts":   float64(3),
		"subcall_retry_backoff_ms":     float64(1),
		"subcall_retry_max_backoff_ms": float64(2),
	})

	result, err := agg.Aggregate(context.Background(), inputs, config, client, nil)
	if err != nil {
		t.Fatalf("expected success after subcall retries, got error: %v", err)
	}
	if result.Winner != "agent-2" {
		t.Fatalf("expected agent-2 winner after scoring, got %q", result.Winner)
	}
	if flaky.attemptsByKey["agent-1"] != 2 || flaky.attemptsByKey["agent-2"] != 2 {
		t.Fatalf("expected each scorer call to run twice (retry once), got attempts: %+v", flaky.attemptsByKey)
	}
}

func TestScoringAggregatorFailsWhenSubcallRetriesExhausted(t *testing.T) {
	agg := &ScoringAggregator{}

	registry := providers.NewRegistry()
	flaky := newFlakyScoringProvider(map[string]int{
		"agent-1": 5, // exceed retry budget
	})
	registry.Register(flaky)
	client := NewMockLLMClient(registry)

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Response 1"},
		{AgentID: "agent-2", Output: "Response 2"},
	}
	config := withScoringConfig(map[string]interface{}{
		"scoring_model":                "mock-model",
		"max_tokens":                   -1,
		"subcall_retry_max_attempts":   float64(2),
		"subcall_retry_backoff_ms":     float64(1),
		"subcall_retry_max_backoff_ms": float64(2),
	})

	_, err := agg.Aggregate(context.Background(), inputs, config, client, nil)
	if err == nil {
		t.Fatal("expected failure when subcall retry budget exhausted")
	}
	if !strings.Contains(err.Error(), "after 2/2 attempts") {
		t.Fatalf("expected exhausted-attempts error, got: %v", err)
	}
}

func TestScoringAggregatorRetriesEmptyContentWhenOutputTokensConsumed(t *testing.T) {
	agg := &ScoringAggregator{}

	registry := providers.NewRegistry()
	scripted := newScriptedScoringProvider(map[string][]scriptedScoringStep{
		"agent-1": {
			{content: "", completionTokens: 9},
			{content: `{"scores":{"accuracy":8,"completeness":7,"clarity":8,"relevance":7}}`, completionTokens: 20},
		},
		"agent-2": {
			{content: `{"scores":{"accuracy":9,"completeness":9,"clarity":8,"relevance":9}}`, completionTokens: 20},
		},
	})
	registry.Register(scripted)
	client := NewMockLLMClient(registry)

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Response 1"},
		{AgentID: "agent-2", Output: "Response 2"},
	}
	config := withScoringConfig(map[string]interface{}{
		"scoring_model":                "mock-model",
		"max_tokens":                   -1,
		"subcall_retry_max_attempts":   float64(3),
		"subcall_retry_backoff_ms":     float64(1),
		"subcall_retry_max_backoff_ms": float64(2),
	})

	result, err := agg.Aggregate(context.Background(), inputs, config, client, nil)
	if err != nil {
		t.Fatalf("expected empty-content retry to recover, got error: %v", err)
	}
	if result.Winner != "agent-2" {
		t.Fatalf("expected agent-2 winner after retry, got %q", result.Winner)
	}
	if scripted.attemptsByKey["agent-1"] != 2 {
		t.Fatalf("expected agent-1 scoring call to retry once, got attempts: %+v", scripted.attemptsByKey)
	}
	if !strings.Contains(result.Reasoning, "(agent-1 scoring call retries: 1)") {
		t.Fatalf("expected retry note in reasoning, got: %s", result.Reasoning)
	}
}

func TestScoringAggregatorAllowsEmptyContentWhenNoOutputTokensConsumed(t *testing.T) {
	agg := &ScoringAggregator{}

	registry := providers.NewRegistry()
	scripted := newScriptedScoringProvider(map[string][]scriptedScoringStep{
		"agent-1": {
			{content: "", completionTokens: 0},
		},
		"agent-2": {
			{content: `{"scores":{"accuracy":9,"completeness":8,"clarity":8,"relevance":9}}`, completionTokens: 20},
		},
	})
	registry.Register(scripted)
	client := NewMockLLMClient(registry)

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Response 1"},
		{AgentID: "agent-2", Output: "Response 2"},
	}
	config := withScoringConfig(map[string]interface{}{
		"scoring_model":                "mock-model",
		"max_tokens":                   -1,
		"subcall_retry_max_attempts":   float64(3),
		"subcall_retry_backoff_ms":     float64(1),
		"subcall_retry_max_backoff_ms": float64(2),
	})

	result, err := agg.Aggregate(context.Background(), inputs, config, client, nil)
	if err != nil {
		t.Fatalf("expected no error when empty completion had zero output tokens, got: %v", err)
	}
	if result.Winner != "agent-2" {
		t.Fatalf("expected agent-2 winner, got %q", result.Winner)
	}
	if scripted.attemptsByKey["agent-1"] != 1 {
		t.Fatalf("expected no retry for zero-output-token empty response, got attempts: %+v", scripted.attemptsByKey)
	}
	if result.Scores["agent-1"] != 0.0 {
		t.Fatalf("expected agent-1 parse-failure score 0.0, got %f", result.Scores["agent-1"])
	}
}

func TestSynthesisAggregatorEmptyContentWithOutputTokensIsRetryable(t *testing.T) {
	agg := &SynthesisAggregator{}

	registry := providers.NewRegistry()
	registry.Register(&fixedUsageProvider{
		content:          "",
		promptTokens:     12,
		completionTokens: 8,
	})
	client := NewMockLLMClient(registry)

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Response 1"},
		{AgentID: "agent-2", Output: "Response 2"},
	}
	config := withSynthesisConfig(map[string]interface{}{
		"model":      "mock-model",
		"max_tokens": -1,
	})

	_, err := agg.Aggregate(context.Background(), inputs, config, client, nil)
	if err == nil {
		t.Fatal("expected retryable error for empty synthesis output with consumed tokens")
	}
	var retryErr *RetryableError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected RetryableError, got %T: %v", err, err)
	}
	if !retryErr.Retryable {
		t.Fatalf("expected retryable=true, got false")
	}
	if retryErr.Code != RetryCodeOutputTruncatedEmpty {
		t.Fatalf("expected retry code %s, got %s", RetryCodeOutputTruncatedEmpty, retryErr.Code)
	}
}

func TestSynthesisAggregatorAllowsEmptyContentWhenNoOutputTokensConsumed(t *testing.T) {
	agg := &SynthesisAggregator{}

	registry := providers.NewRegistry()
	registry.Register(&fixedUsageProvider{
		content:          "",
		promptTokens:     12,
		completionTokens: 0,
	})
	client := NewMockLLMClient(registry)

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Response 1"},
		{AgentID: "agent-2", Output: "Response 2"},
	}
	config := withSynthesisConfig(map[string]interface{}{
		"model":      "mock-model",
		"max_tokens": -1,
	})

	result, err := agg.Aggregate(context.Background(), inputs, config, client, nil)
	if err != nil {
		t.Fatalf("expected no error for empty synthesis output with zero output tokens, got: %v", err)
	}
	if result.Output != "" {
		t.Fatalf("expected empty output, got %q", result.Output)
	}
}

func TestSynthesisAggregatorSingle(t *testing.T) {
	agg := &SynthesisAggregator{}

	if agg.Name() != AggMethodSynthesis {
		t.Errorf("Expected name %s, got %s", AggMethodSynthesis, agg.Name())
	}

	// Single input returns it directly without requiring LLM
	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Response 1"},
	}

	ctx := context.Background()
	result, err := agg.Aggregate(ctx, inputs, nil, nil, nil)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Output != "Response 1" {
		t.Errorf("Expected 'Response 1', got %q", result.Output)
	}
}

func TestSynthesisAggregatorMultipleNeedsLLM(t *testing.T) {
	agg := &SynthesisAggregator{}

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Response 1"},
		{AgentID: "agent-2", Output: "Response 2"},
	}

	ctx := context.Background()
	_, err := agg.Aggregate(ctx, inputs, nil, nil, nil)

	// Should fail without LLM client for multiple inputs
	if err == nil {
		t.Error("Expected error when no LLM client provided for synthesis of multiple inputs")
	}
}

func TestAggregatorRegistryCustom(t *testing.T) {
	registry := NewAggregatorRegistry()

	// Test registering a custom aggregator
	custom := &CollectAggregator{}
	registry.Register(custom)

	// Should still be retrievable
	agg, ok := registry.Get(AggMethodCollect)
	if !ok {
		t.Error("Expected collect aggregator to be registered")
	}
	if agg == nil {
		t.Error("Expected non-nil aggregator")
	}
}

func TestValidateAggregationConfig(t *testing.T) {
	// collect validates with nil config (no LLM call)
	if err := ValidateAggregationConfig(AggMethodCollect, nil); err != nil {
		t.Errorf("Unexpected error validating collect with nil config: %v", err)
	}

	// Methods requiring max_tokens should fail with nil config
	for _, method := range []AggregationMethod{AggMethodJudge, AggMethodScoring, AggMethodSynthesis, AggMethodPeerMatrix, AggMethodDebateDecide} {
		if err := ValidateAggregationConfig(method, nil); err == nil {
			t.Errorf("Expected %s to fail validation with nil config (missing max_tokens)", method)
		}
	}

	// Methods requiring repair_max_tokens should fail without it.
	// Keep all other required fields present to ensure we fail for the intended reason.
	for _, method := range []AggregationMethod{AggMethodJudge, AggMethodDebateDecide} {
		if err := ValidateAggregationConfig(method, map[string]interface{}{
			"judge_model":         "mock-model",
			"system_prompt":       "judge",
			"prompt":              "score",
			"temperature":         0.0,
			"max_tokens":          -1,
			"extraction_strategy": "regex",
			"extraction_pattern":  "(?i)answer\\s*[:=]\\s*([A-Z])",
		}); err == nil {
			t.Errorf("Expected %s to fail validation without repair_max_tokens", method)
		}
	}

	// judge/debate_decide pass with both max_tokens and repair_max_tokens
	for _, method := range []AggregationMethod{AggMethodJudge, AggMethodDebateDecide} {
		if err := ValidateAggregationConfig(method, map[string]interface{}{
			"judge_model":         "mock-model",
			"system_prompt":       "judge",
			"prompt":              "score",
			"temperature":         0.0,
			"max_tokens":          -1,
			"repair_max_tokens":   256,
			"extraction_strategy": "regex",
			"extraction_pattern":  "(?i)answer\\s*[:=]\\s*([A-Z])",
		}); err != nil {
			t.Errorf("Expected %s to validate with max_tokens + repair_max_tokens: %v", method, err)
		}
	}

	// scoring/synthesis pass with required explicit fields.
	for _, method := range []AggregationMethod{AggMethodScoring, AggMethodSynthesis} {
		cfg := map[string]interface{}{
			"system_prompt": "system",
			"prompt":        "prompt",
			"temperature":   0.0,
			"max_tokens":    -1,
		}
		if method == AggMethodScoring {
			cfg["scoring_model"] = "mock-model"
		}
		if method == AggMethodSynthesis {
			cfg["model"] = "mock-model"
		}
		if err := ValidateAggregationConfig(method, map[string]interface{}{
			"system_prompt": cfg["system_prompt"],
			"prompt":        cfg["prompt"],
			"temperature":   cfg["temperature"],
			"max_tokens":    cfg["max_tokens"],
			"scoring_model": cfg["scoring_model"],
			"model":         cfg["model"],
		}); err != nil {
			t.Errorf("Expected %s to validate with max_tokens: %v", method, err)
		}
	}

	// max_tokens: -1 (unlimited) is valid when all required fields are present.
	if err := ValidateAggregationConfig(AggMethodScoring, map[string]interface{}{
		"scoring_model": "mock-model",
		"system_prompt": "system",
		"prompt":        "prompt",
		"temperature":   0.0,
		"max_tokens":    -1,
	}); err != nil {
		t.Fatalf("Expected max_tokens: -1 to be valid: %v", err)
	}

	// max_tokens: 0 is invalid
	if err := ValidateAggregationConfig(AggMethodScoring, map[string]interface{}{
		"scoring_model": "mock-model",
		"system_prompt": "system",
		"prompt":        "prompt",
		"temperature":   0.0,
		"max_tokens":    0,
	}); err == nil {
		t.Fatal("Expected max_tokens: 0 to fail validation")
	}

	if err := ValidateAggregationConfig(AggMethodScoring, map[string]interface{}{
		"scoring_model": "mock-model",
		"system_prompt": "system",
		"prompt":        "score {{response}}",
		"temperature":   0.0,
		"max_tokens":    -1,
		"rubric_mode":   "dynamic",
	}); err == nil {
		t.Fatalf("Expected scoring dynamic rubric validation to require prompt {{rubric}} placeholder")
	}

	if err := ValidateAggregationConfig(AggMethodScoring, map[string]interface{}{
		"scoring_model": "mock-model",
		"system_prompt": "system",
		"prompt":        "score {{response}} with {{rubric}}",
		"temperature":   0.0,
		"max_tokens":    -1,
		"rubric_mode":   "dynamic",
	}); err != nil {
		t.Fatalf("Expected scoring dynamic rubric config with prompt placeholder to validate: %v", err)
	}

	if err := ValidateAggregationConfig(AggMethodMajorityVote, nil); err == nil {
		t.Fatalf("Expected majority_vote to require explicit tie_breaker_method config")
	}

	if err := ValidateAggregationConfig(AggMethodMajorityVote, map[string]interface{}{
		"extraction_strategy": "regex",
		"extraction_pattern":  "(?i)answer\\s*[:=]\\s*([A-Z])",
		"tie_breaker_method":  "first",
	}); err != nil {
		t.Fatalf("Expected majority_vote tie_breaker_method=first to validate: %v", err)
	}

	if err := ValidateAggregationConfig(AggMethodMajorityVote, map[string]interface{}{
		"extraction_strategy":     "regex",
		"extraction_pattern":      "(?i)answer\\s*[:=]\\s*([A-Z])",
		"tie_breaker_method":      "synthesis",
		"tie_breaker_model":       "mock-model",
		"tie_breaker_temperature": 0.0,
		"system_prompt":           "You are a synthesis agent.",
		"prompt":                  "{{prompt}}\n\n{{responses}}",
		"max_tokens":              -1,
	}); err != nil {
		t.Fatalf("Expected majority_vote tie_breaker_method=synthesis with full config to validate: %v", err)
	}

	// synthesis tie-breaker without system_prompt/prompt/max_tokens should fail early
	if err := ValidateAggregationConfig(AggMethodMajorityVote, map[string]interface{}{
		"extraction_strategy":     "regex",
		"extraction_pattern":      "(?i)answer\\s*[:=]\\s*([A-Z])",
		"tie_breaker_method":      "synthesis",
		"tie_breaker_model":       "mock-model",
		"tie_breaker_temperature": 0.0,
	}); err == nil {
		t.Fatalf("Expected majority_vote tie_breaker_method=synthesis without system_prompt to fail validation")
	}

	if err := ValidateAggregationConfig(AggMethodMajorityVote, map[string]interface{}{
		"tie_breaker_method": "synth",
	}); err == nil {
		t.Fatalf("Expected majority_vote validation to fail on unsupported tie_breaker_method")
	}

	if err := ValidateAggregationConfig(AggMethodPeerMatrix, map[string]interface{}{
		"eval_system_prompt": "evaluate",
		"eval_prompt":        "review",
		"normalization":      "none",
		"temperature":        0.0,
		"max_tokens":         -1,
		"max_parallel":       2,
		"rubric": []interface{}{
			map[string]interface{}{"name": "accuracy", "weight": 1.0},
		},
		"judge_model": "openai/gpt-4o-mini",
	}); err == nil {
		t.Fatalf("Expected peer_matrix validation to reject judge_model")
	}

	if err := ValidateAggregationConfig(AggMethodPeerMatrix, map[string]interface{}{
		"eval_system_prompt": "evaluate",
		"eval_prompt":        "review",
		"normalization":      "none",
		"temperature":        0.0,
		"max_tokens":         -1,
		"max_parallel":       2,
		"rubric": []interface{}{
			map[string]interface{}{"name": "accuracy", "weight": 1.0},
		},
		"rubric_mode": "dynamic",
	}); err == nil {
		t.Fatalf("Expected peer_matrix validation to require rubric_model when rubric_mode=dynamic")
	}

	if err := ValidateAggregationConfig(AggMethodPeerMatrix, map[string]interface{}{
		"eval_system_prompt": "evaluate",
		"eval_prompt":        "review {{response}}",
		"normalization":      "none",
		"temperature":        0.0,
		"max_tokens":         -1,
		"max_parallel":       2,
		"rubric": []interface{}{
			map[string]interface{}{"name": "accuracy", "weight": 1.0},
		},
		"rubric_mode":  "dynamic",
		"rubric_model": "z-ai/glm-5",
	}); err == nil {
		t.Fatalf("Expected peer_matrix dynamic rubric validation to require eval_prompt {{rubric}} placeholder")
	}

	if err := ValidateAggregationConfig(AggMethodPeerMatrix, map[string]interface{}{
		"eval_system_prompt": "evaluate",
		"eval_prompt":        "review {{rubric}}",
		"normalization":      "none",
		"temperature":        0.0,
		"max_tokens":         -1,
		"max_parallel":       2,
		"rubric": []interface{}{
			map[string]interface{}{"name": "accuracy", "weight": 1.0},
		},
		"rubric_mode":  "dynamic",
		"rubric_model": "z-ai/glm-5",
	}); err != nil {
		t.Fatalf("Expected peer_matrix dynamic rubric config with rubric_model to validate: %v", err)
	}
}

func TestPeerMatrixAggregatorSingle(t *testing.T) {
	agg := &PeerMatrixAggregator{}

	if agg.Name() != AggMethodPeerMatrix {
		t.Errorf("Expected name %s, got %s", AggMethodPeerMatrix, agg.Name())
	}

	// Single input wins by default without requiring LLM
	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Response 1"},
	}

	ctx := context.Background()
	result, err := agg.Aggregate(ctx, inputs, nil, nil, nil)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Winner != "agent-1" {
		t.Errorf("Expected winner 'agent-1', got %q", result.Winner)
	}
}

func TestPeerMatrixAggregatorMultipleNeedsLLM(t *testing.T) {
	agg := &PeerMatrixAggregator{}

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Response 1"},
		{AgentID: "agent-2", Output: "Response 2"},
	}

	ctx := context.Background()
	_, err := agg.Aggregate(ctx, inputs, nil, nil, nil)

	// Should fail without LLM client for multiple inputs
	if err == nil {
		t.Error("Expected error when no LLM client provided for peer_matrix with multiple inputs")
	}
}

func TestPeerMatrixAggregatorShortCircuitsOnUnanimousAnswer(t *testing.T) {
	agg := &PeerMatrixAggregator{}

	inputs := []AgentOutput{
		{AgentID: "agent-a", Output: "Final answer: D\nReasoning: one"},
		{AgentID: "agent-b", Output: "Final answer: D\nReasoning: two"},
	}
	config := withPeerMatrixConfig(map[string]interface{}{
		"extraction_strategy": "regex",
		"max_tokens":          -1,
	})

	result, err := agg.Aggregate(context.Background(), inputs, config, nil, nil)
	if err != nil {
		t.Fatalf("expected unanimous short-circuit to succeed without LLM client, got: %v", err)
	}
	if result.Winner != "agent-a" {
		t.Fatalf("expected first agent to win deterministic short-circuit, got %q", result.Winner)
	}
	if result.ConsensusAnswer != "D" {
		t.Fatalf("expected consensus answer D, got %q", result.ConsensusAnswer)
	}
	if result.AgreementRatio != 1.0 {
		t.Fatalf("expected agreement ratio 1.0, got %f", result.AgreementRatio)
	}
	if result.EvalMatrix != nil {
		t.Fatalf("expected no evaluation matrix when short-circuiting, got %+v", result.EvalMatrix)
	}
	if result.Scores["agent-a"] != 10.0 || result.Scores["agent-b"] != 10.0 {
		t.Fatalf("expected short-circuit peer scores of 10.0, got %+v", result.Scores)
	}
	if !strings.Contains(result.Reasoning, "short-circuited peer_matrix aggregation") {
		t.Fatalf("expected short-circuit reasoning, got %q", result.Reasoning)
	}
}

type candidateScoreProvider struct{}

func (p *candidateScoreProvider) Name() string { return "candidate-score" }

func (p *candidateScoreProvider) Models() []providers.Model {
	return []providers.Model{{
		ID:         "mock-model",
		Name:       "Mock Model",
		Provider:   "candidate-score",
		ContextLen: 8192,
		InputCost:  0,
		OutputCost: 0,
		MaxTokens:  4096,
		Available:  true,
	}}
}

func (p *candidateScoreProvider) Complete(_ context.Context, req *providers.CompletionRequest) (*providers.CompletionResponse, error) {
	var joined strings.Builder
	for _, msg := range req.Messages {
		joined.WriteString(msg.Content)
		joined.WriteByte('\n')
	}
	prompt := joined.String()

	score := 5
	candidate := ""
	if idx := strings.Index(prompt, "Response to evaluate:\n"); idx >= 0 {
		rest := prompt[idx+len("Response to evaluate:\n"):]
		candidate = strings.TrimSpace(strings.SplitN(rest, "\n", 2)[0])
	}
	switch candidate {
	case "CAND_A":
		score = 9
	case "CAND_B":
		score = 6
	case "CAND_C":
		score = 4
	}

	content := fmt.Sprintf(`{"accuracy":{"reasoning":"ok","score":%d},"clarity":{"reasoning":"ok","score":%d}}`, score, score)
	return &providers.CompletionResponse{
		ID:      "candidate-score-response",
		Model:   req.Model,
		Content: content,
		Usage: providers.Usage{
			PromptTokens:     12,
			CompletionTokens: 8,
			TotalTokens:      20,
		},
		Finish: "stop",
	}, nil
}

func (p *candidateScoreProvider) EstimateTokens(text string) int { return len(text) / 4 }

func (p *candidateScoreProvider) Cost(model string, inputTokens, outputTokens int) float64 { return 0 }

func TestPeerMatrixAggregatorSelectsWinnerFromValidEvaluations(t *testing.T) {
	agg := &PeerMatrixAggregator{}

	registry := providers.NewRegistry()
	registry.Register(&candidateScoreProvider{})
	client := NewMockLLMClient(registry)

	inputs := []AgentOutput{
		{AgentID: "agent-a", Model: "mock-model", Output: "CAND_A"},
		{AgentID: "agent-b", Model: "mock-model", Output: "CAND_B"},
		{AgentID: "agent-c", Model: "mock-model", Output: "CAND_C"},
	}

	config := withPeerMatrixConfig(map[string]interface{}{
		"max_parallel": float64(2),
		"rubric": []interface{}{
			map[string]interface{}{"name": "Accuracy", "weight": 0.7, "description": "Correctness"},
			map[string]interface{}{"name": "Clarity", "weight": 0.3, "description": "Communication quality"},
		},
	})

	result, err := agg.Aggregate(context.Background(), inputs, config, client, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result.Winner != "agent-a" {
		t.Fatalf("Expected winner agent-a, got %q", result.Winner)
	}
	if result.Output != "CAND_A" {
		t.Fatalf("Expected winner output CAND_A, got %q", result.Output)
	}
	approxEqual := func(a, b float64) bool {
		diff := a - b
		if diff < 0 {
			diff = -diff
		}
		return diff < 0.01
	}
	if !approxEqual(result.Scores["agent-a"], 9) {
		t.Fatalf("Expected score ~9 for agent-a, got %v", result.Scores["agent-a"])
	}
	if !approxEqual(result.Scores["agent-b"], 6) {
		t.Fatalf("Expected score ~6 for agent-b, got %v", result.Scores["agent-b"])
	}
	if !approxEqual(result.Scores["agent-c"], 4) {
		t.Fatalf("Expected score ~4 for agent-c, got %v", result.Scores["agent-c"])
	}
	if result.EvalMatrix == nil {
		t.Fatal("Expected eval matrix in result")
	}
	if result.EvalMatrix.InvalidCount != 0 {
		t.Fatalf("Expected zero invalid evaluations, got %d", result.EvalMatrix.InvalidCount)
	}
	if result.TokensUsed <= 0 {
		t.Fatalf("Expected positive token usage, got %d", result.TokensUsed)
	}
}

func TestPeerMatrixAggregatorAllInvalidEvaluationsFallbackFirst(t *testing.T) {
	agg := &PeerMatrixAggregator{}

	registry := providers.NewRegistry()
	registry.Register(NewMockProvider("test"))
	client := NewMockLLMClient(registry)

	inputs := []AgentOutput{
		{AgentID: "agent-a", Model: "mock-model", Output: "A"},
		{AgentID: "agent-b", Model: "mock-model", Output: "B"},
	}

	config := withPeerMatrixConfig(map[string]interface{}{})

	result, err := agg.Aggregate(context.Background(), inputs, config, client, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.Winner != "agent-a" {
		t.Fatalf("Expected fallback winner agent-a, got %q", result.Winner)
	}
	if result.Output != "A" {
		t.Fatalf("Expected fallback output A, got %q", result.Output)
	}
	if len(result.Scores) != 0 {
		t.Fatalf("Expected no final scores when all evaluations are invalid, got %+v", result.Scores)
	}
	if result.EvalMatrix == nil {
		t.Fatal("Expected eval matrix in result")
	}
	if result.EvalMatrix.InvalidCount != 2 {
		t.Fatalf("Expected 2 invalid evaluations, got %d", result.EvalMatrix.InvalidCount)
	}
	if !strings.Contains(result.Reasoning, "(2 invalid)") {
		t.Fatalf("Expected reasoning to report invalid count, got %q", result.Reasoning)
	}
}

func TestPeerMatrixAggregatorMissingReviewerModelFails(t *testing.T) {
	agg := &PeerMatrixAggregator{}

	registry := providers.NewRegistry()
	registry.Register(NewMockProvider("test"))
	client := NewMockLLMClient(registry)

	inputs := []AgentOutput{
		{AgentID: "agent-a", Model: "mock-model", Output: "A"},
		{AgentID: "agent-b", Output: "B"},
	}

	_, err := agg.Aggregate(context.Background(), inputs, withPeerMatrixConfig(map[string]interface{}{}), client, nil)
	if err == nil {
		t.Fatal("Expected error when a peer_matrix reviewer has no model")
	}
	if !strings.Contains(err.Error(), "missing model") {
		t.Fatalf("Expected missing-model error, got: %v", err)
	}
}

func TestPeerMatrixAggregatorRejectsJudgeModelConfig(t *testing.T) {
	agg := &PeerMatrixAggregator{}

	registry := providers.NewRegistry()
	registry.Register(NewMockProvider("test"))
	client := NewMockLLMClient(registry)

	inputs := []AgentOutput{
		{AgentID: "agent-a", Model: "mock-model", Output: "A"},
		{AgentID: "agent-b", Model: "mock-model", Output: "B"},
	}

	_, err := agg.Aggregate(context.Background(), inputs, map[string]interface{}{
		"judge_model": "openai/gpt-4o-mini",
	}, client, nil)
	if err == nil {
		t.Fatal("Expected error when peer_matrix config includes judge_model")
	}
	if !strings.Contains(err.Error(), "does not support judge_model") {
		t.Fatalf("Expected judge_model rejection error, got: %v", err)
	}
}

func TestExecuteSingleEvaluation_PerAgentModel(t *testing.T) {
	agg := &PeerMatrixAggregator{}
	provider := &capturingProvider{
		name:            "capture",
		responseContent: `{"scores":{"logical_soundness":8}}`,
		models: []providers.Model{
			{
				ID:         "mock-model",
				Name:       "Mock Model",
				Provider:   "capture",
				ContextLen: 4096,
				MaxTokens:  2048,
				Available:  true,
			},
			{
				ID:         "reviewer-model",
				Name:       "Reviewer Model",
				Provider:   "capture",
				ContextLen: 4096,
				MaxTokens:  2048,
				Available:  true,
			},
		},
	}
	registry := providers.NewRegistry()
	registry.Register(provider)
	client := NewMockLLMClient(registry)

	cfg, err := parsePeerMatrixConfig(map[string]interface{}{
		"eval_system_prompt": "score",
		"eval_prompt":        "evaluate {{candidate_response}}",
		"normalization":      "none",
		"temperature":        0.0,
		"max_parallel":       2,
		"max_tokens":         float64(64),
		"rubric": []interface{}{
			map[string]interface{}{"name": "logical_soundness", "weight": 1.0},
		},
	})
	if err != nil {
		t.Fatalf("parsePeerMatrixConfig failed: %v", err)
	}
	task := evalTask{
		ReviewerID:     "agent-a",
		ReviewerModel:  "reviewer-model",
		CandidateID:    "agent-b",
		Response:       "candidate output",
		ReviewerAnswer: "reviewer output",
	}

	result := agg.executeSingleEvaluation(
		context.Background(),
		task,
		cfg,
		DefaultPeerEvalPrompt,
		"What is 2+2?",
		client,
		nil,
	)

	if !result.Valid {
		t.Fatalf("Expected valid evaluation result, got %+v", result)
	}
	req := provider.lastRequest()
	if req == nil {
		t.Fatal("Expected provider to receive a completion request")
	}
	if req.Model != "reviewer-model" {
		t.Fatalf("Expected reviewer model to be used, got %q", req.Model)
	}
}

func TestBuildEvalTasks(t *testing.T) {
	inputs := []AgentOutput{
		{AgentID: "a", Model: "model-a", Output: "A"},
		{AgentID: "b", Model: "model-b", Output: "B"},
		{AgentID: "c", Model: "model-c", Output: "C"},
	}

	tasks := buildEvalTasks(inputs)

	// N=3 agents, each evaluates 2 others = 6 tasks
	if len(tasks) != 6 {
		t.Errorf("Expected 6 tasks for 3 agents, got %d", len(tasks))
	}

	// Verify no self-evaluation
	for _, task := range tasks {
		if task.ReviewerID == task.CandidateID {
			t.Errorf("Self-evaluation found: reviewer=%s, candidate=%s", task.ReviewerID, task.CandidateID)
		}
	}

	// Verify ReviewerAnswer and ReviewerModel are populated from the reviewer's AgentOutput
	for _, task := range tasks {
		var expectedAnswer string
		var expectedModel string
		for _, input := range inputs {
			if input.AgentID == task.ReviewerID {
				expectedAnswer = input.Output
				expectedModel = input.Model
				break
			}
		}
		if task.ReviewerAnswer != expectedAnswer {
			t.Errorf("Expected ReviewerAnswer=%q for reviewer %s, got %q",
				expectedAnswer, task.ReviewerID, task.ReviewerAnswer)
		}
		if task.ReviewerModel != expectedModel {
			t.Errorf("Expected ReviewerModel=%q for reviewer %s, got %q",
				expectedModel, task.ReviewerID, task.ReviewerModel)
		}
	}
}

func TestBuildEvalTasksNoModel(t *testing.T) {
	inputs := []AgentOutput{
		{AgentID: "a", Output: "A"},
		{AgentID: "b", Output: "B"},
	}

	tasks := buildEvalTasks(inputs)

	// N=2 agents, each evaluates 1 other = 2 tasks
	if len(tasks) != 2 {
		t.Errorf("Expected 2 tasks for 2 agents, got %d", len(tasks))
	}

	// When AgentOutput.Model is empty, ReviewerModel should also be empty
	for _, task := range tasks {
		if task.ReviewerModel != "" {
			t.Errorf("Expected empty ReviewerModel when AgentOutput.Model is empty, got %q", task.ReviewerModel)
		}
	}
}

func TestBuildEvaluationMatrixNoNormalization(t *testing.T) {
	// With "none" normalization, raw scores should pass through unchanged
	inputs := []AgentOutput{
		{AgentID: "a", Output: "A"},
		{AgentID: "b", Output: "B"},
	}
	results := []evalResult{
		{ReviewerID: "a", CandidateID: "b", Score: 8.0, Valid: true},
		{ReviewerID: "b", CandidateID: "a", Score: 4.0, Valid: true},
	}

	matrix := buildEvaluationMatrix(results, inputs, "none")

	// Raw scores should equal normalized scores
	if matrix.NormalizedScores["a"]["b"] != 8.0 {
		t.Errorf("Expected normalized score 8.0, got %f", matrix.NormalizedScores["a"]["b"])
	}
	if matrix.NormalizedScores["b"]["a"] != 4.0 {
		t.Errorf("Expected normalized score 4.0, got %f", matrix.NormalizedScores["b"]["a"])
	}

	// Final scores: a gets average of scores from others = 4.0, b gets 8.0
	if matrix.FinalScores["a"] != 4.0 {
		t.Errorf("Expected final score 4.0 for a, got %f", matrix.FinalScores["a"])
	}
	if matrix.FinalScores["b"] != 8.0 {
		t.Errorf("Expected final score 8.0 for b, got %f", matrix.FinalScores["b"])
	}
}

func TestSelectWinnerDeterministicTieAndMissingOutput(t *testing.T) {
	inputs := []AgentOutput{
		{AgentID: "agent-a", Output: "A"},
		{AgentID: "agent-b", Output: "B"},
	}

	winner, output := selectWinner(map[string]float64{
		"agent-b": 7.0,
		"agent-a": 7.0,
	}, inputs)
	if winner != "agent-a" {
		t.Fatalf("Expected alphabetical tie-break winner agent-a, got %q", winner)
	}
	if output != "A" {
		t.Fatalf("Expected output A for winner agent-a, got %q", output)
	}

	winner, output = selectWinner(map[string]float64{
		"agent-z": 10.0,
	}, inputs)
	if winner != "agent-z" {
		t.Fatalf("Expected winner agent-z, got %q", winner)
	}
	if output != "" {
		t.Fatalf("Expected empty output when winner ID is not in inputs, got %q", output)
	}
}

func TestParseWinner(t *testing.T) {
	valid := []string{"agent-1", "agent-2"}

	winner := parseWinner("Analysis...\nWINNER: agent-2\n", valid)
	if winner != "agent-2" {
		t.Errorf("Expected winner agent-2, got %q", winner)
	}

	winner = parseWinner("winner: Agent-1 (best response)", valid)
	if winner != "agent-1" {
		t.Errorf("Expected winner agent-1, got %q", winner)
	}

	winner = parseWinner("WINNER: agent-3", valid)
	if winner != "" {
		t.Errorf("Expected no winner, got %q", winner)
	}
}

func TestParseWinnerSingleLetterLabels(t *testing.T) {
	labels := []string{"A", "B", "C"}

	// Exact match on WINNER: line
	if w := parseWinner("WINNER: B", labels); w != "B" {
		t.Errorf("Exact match failed: expected B, got %q", w)
	}

	// Word-boundary match: "Camp A" should match A, not be confused by other letters
	if w := parseWinner("WINNER: Camp A", labels); w != "A" {
		t.Errorf("Camp label match failed: expected A, got %q", w)
	}

	// Key regression: "Based on analysis, B" should match B, NOT A.
	// Before the fix, strings.Contains("based on analysis, b", "a") matched A first.
	if w := parseWinner("WINNER: Based on analysis, B is the best", labels); w != "B" {
		t.Errorf("Substring bias regression: expected B, got %q", w)
	}

	// Pass 2 fallback: line contains "winner" somewhere
	if w := parseWinner("The clear winner is C based on evidence", labels); w != "C" {
		t.Errorf("Pass 2 fallback failed: expected C, got %q", w)
	}

	// Pass 2 should NOT match letters inside words: "winner: amazing" should not match A
	if w := parseWinner("winner: amazing response overall", labels); w != "" {
		t.Errorf("False positive on letters-in-words: expected empty, got %q", w)
	}

	// No valid label in response
	if w := parseWinner("WINNER: D", labels); w != "" {
		t.Errorf("Invalid label should return empty, got %q", w)
	}
}

func TestContainsIDAsWord(t *testing.T) {
	// Single letter should only match as standalone word
	if containsIDAsWord("amazing", "A") {
		t.Error("Should not match 'A' inside 'amazing'")
	}
	if !containsIDAsWord("Response A wins", "A") {
		t.Error("Should match standalone 'A'")
	}

	// Multi-char IDs still work
	if !containsIDAsWord("The winner is agent-pro", "agent-pro") {
		t.Error("Should match 'agent-pro' as a word")
	}
	if containsIDAsWord("agent-provider is different", "agent-pro") {
		t.Error("Should not match 'agent-pro' as prefix of 'agent-provider'")
	}
}

func TestParseScores(t *testing.T) {
	scores := parseScores(`Some text {"scores": {"accuracy": 8, "clarity": 6}}`)
	if scores["accuracy"] != 8 || scores["clarity"] != 6 {
		t.Errorf("Expected JSON scores to parse, got %+v", scores)
	}

	scores = parseScores("Accuracy: 9\nClarity: 7/10")
	if scores["accuracy"] != 9 || scores["clarity"] != 7 {
		t.Errorf("Expected fallback scores to parse, got %+v", scores)
	}
}

func TestCalculateWeightedScore(t *testing.T) {
	rubric := []RubricCriterion{
		{Name: "Accuracy", Weight: 0.5},
		{Name: "Clarity", Weight: 0.5},
	}

	score, matched := calculateWeightedScore(map[string]float64{
		"accuracy": 8,
		"clarity":  6,
	}, rubric)
	if score != 7.0 {
		t.Errorf("Expected weighted score 7.0, got %f", score)
	}
	if !matched {
		t.Error("Expected matched=true when criteria are present")
	}

	// Empty scores should return 0.0 and matched=false (parse failure)
	score, matched = calculateWeightedScore(map[string]float64{}, rubric)
	if score != 0.0 {
		t.Errorf("Expected default score 0.0 for parse failure, got %f", score)
	}
	if matched {
		t.Error("Expected matched=false when no criteria are parsed")
	}

	// Scores that don't match rubric criteria should return strict failure
	score, matched = calculateWeightedScore(map[string]float64{
		"unknown_criterion": 6.0,
	}, rubric)
	if score != 0.0 {
		t.Errorf("Expected score 0.0 for unmatched criteria, got %f", score)
	}
	if matched {
		t.Error("Expected matched=false when no rubric criteria match")
	}
}

func TestScoringAggregatorAllParseFailures(t *testing.T) {
	agg := &ScoringAggregator{}

	// Create a mock provider that returns gibberish (no parseable scores)
	registry := providers.NewRegistry()
	mock := NewMockProvider("test")
	mock.SetResponse("expert evaluator", "This response is interesting but I cannot provide structured scores.")
	registry.Register(mock)
	client := NewMockLLMClient(registry)

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Response 1"},
		{AgentID: "agent-2", Output: "Response 2"},
		{AgentID: "agent-3", Output: "Response 3"},
	}

	config := withScoringConfig(map[string]interface{}{
		"scoring_model": "mock-model",
		"max_tokens":    -1,
	})

	ctx := context.Background()
	_, err := agg.Aggregate(ctx, inputs, config, client, nil)

	// Should fail when ALL responses fail to parse scores
	if err == nil {
		t.Fatal("Expected error when all score evaluations fail to parse, got nil")
	}
	if !strings.Contains(err.Error(), "all 3 responses failed to parse scores") {
		t.Errorf("Expected error message about all parse failures, got: %s", err.Error())
	}
}

func TestScoringAggregatorPartialParseFailure(t *testing.T) {
	agg := &ScoringAggregator{}

	// Create a mock provider where response depends on the user prompt content
	registry := providers.NewRegistry()
	mock := NewMockProvider("test")
	// The system prompt contains "expert evaluator" — this is the default match
	// Return unparseable response by default
	mock.SetResponse("expert evaluator", "This response is interesting but I cannot provide structured scores.")
	// For agent-2, the user prompt will contain "Response to evaluate:\nResponse 2" — use a keyword
	// longer than "expert evaluator" (16 chars) so it wins the longest-match tiebreak
	mock.SetResponse("Response to evaluate:\nResponse 2", `Great response! {"scores": {"accuracy": 8, "completeness": 7, "clarity": 9, "relevance": 8}}`)
	registry.Register(mock)
	client := NewMockLLMClient(registry)

	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Response 1"},
		{AgentID: "agent-2", Output: "Response 2"},
	}

	config := withScoringConfig(map[string]interface{}{
		"scoring_model": "mock-model",
		"max_tokens":    -1,
	})

	ctx := context.Background()
	result, err := agg.Aggregate(ctx, inputs, config, client, nil)

	// Should NOT error — only partial failure
	if err != nil {
		t.Fatalf("Unexpected error for partial parse failure: %v", err)
	}

	// agent-2 should win (has real scores), agent-1 should have 0.0
	if result.Winner != "agent-2" {
		t.Errorf("Expected winner agent-2 (parsed scores), got %q", result.Winner)
	}
	if result.Scores["agent-1"] != 0.0 {
		t.Errorf("Expected agent-1 score 0.0 (parse failure), got %f", result.Scores["agent-1"])
	}
	if result.Scores["agent-2"] <= 0.0 {
		t.Errorf("Expected agent-2 to have positive score, got %f", result.Scores["agent-2"])
	}
	// Should contain warning about parse failures
	if !strings.Contains(result.Reasoning, "Warning") {
		t.Errorf("Expected reasoning to contain parse failure warning, got: %s", result.Reasoning)
	}
}

func TestCalculateWeightedScorePartialMatch(t *testing.T) {
	rubric := []RubricCriterion{
		{Name: "Accuracy", Weight: 0.5},
		{Name: "Clarity", Weight: 0.5},
	}

	// Only one criterion matches — should still be considered matched
	score, matched := calculateWeightedScore(map[string]float64{
		"accuracy": 8.0,
	}, rubric)
	if !matched {
		t.Error("Expected matched=true when at least one rubric criterion is found")
	}
	// Weighted score with partial match: 8.0 * 0.5 / 0.5 = 8.0
	if score != 8.0 {
		t.Errorf("Expected score 8.0 for partial match, got %f", score)
	}
}

func TestNormalizeRubricKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Accuracy", "accuracy"},
		{"Logical Soundness", "logical_soundness"},
		{"Evidence Analysis", "evidence_analysis"},
		{"evidence_analysis", "evidence_analysis"}, // already normalized
		{"Evidence & Analysis", "evidence_analysis"},
		{"Clarity/Structure", "clarity_structure"},
		{"  Completeness  ", "completeness"}, // leading/trailing spaces
		{"Multi--Hyphen", "multi_hyphen"},
		{"UPPER CASE NAME", "upper_case_name"},
		{"a.b.c", "a_b_c"},
	}
	for _, tt := range tests {
		got := normalizeRubricKey(tt.input)
		if got != tt.want {
			t.Errorf("normalizeRubricKey(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCalculateWeightedScore_NormalizedKeys(t *testing.T) {
	// Rubric uses Title Case, scores use snake_case — both sides should normalize
	rubric := []RubricCriterion{
		{Name: "Logical Soundness", Weight: 0.4},
		{Name: "Evidence Analysis", Weight: 0.3},
		{Name: "Completeness", Weight: 0.2},
		{Name: "Clarity", Weight: 0.1},
	}

	scores := map[string]float64{
		"logical_soundness": 8,
		"evidence_analysis": 7,
		"completeness":      6,
		"clarity":           9,
	}

	score, matched := calculateWeightedScore(scores, rubric)
	if !matched {
		t.Fatal("Expected matched=true when all criteria match")
	}
	// 8*0.4 + 7*0.3 + 6*0.2 + 9*0.1 = 3.2 + 2.1 + 1.2 + 0.9 = 7.4
	expected := 7.4
	if score < expected-0.01 || score > expected+0.01 {
		t.Errorf("Expected weighted score %.2f, got %.2f", expected, score)
	}
}

func TestCalculateWeightedScore_StrictMatch(t *testing.T) {
	rubric := []RubricCriterion{
		{Name: "Accuracy", Weight: 0.5},
		{Name: "Clarity", Weight: 0.5},
	}

	// Completely unrelated keys — should fail strict matching
	score, matched := calculateWeightedScore(map[string]float64{
		"speed":      8.0,
		"creativity": 7.0,
	}, rubric)
	if matched {
		t.Error("Expected matched=false when no rubric criteria match")
	}
	if score != 0.0 {
		t.Errorf("Expected score 0.0 for strict failure, got %f", score)
	}
}

func TestParseCriterionScores(t *testing.T) {
	// Valid per-criterion format
	response := `{"logical_soundness": {"reasoning": "consistent chain of logic", "score": 8}, "evidence_analysis": {"reasoning": "options well eliminated", "score": 7}, "completeness": {"reasoning": "missed edge case", "score": 6}, "clarity": {"reasoning": "well structured", "score": 9}}`

	scores, ok := parseCriterionScores(response)
	if !ok {
		t.Fatal("Expected ok=true for valid per-criterion response")
	}
	if len(scores) != 4 {
		t.Fatalf("Expected 4 criterion scores, got %d", len(scores))
	}
	if scores["logical_soundness"] != 8 {
		t.Errorf("Expected logical_soundness=8, got %f", scores["logical_soundness"])
	}
	if scores["evidence_analysis"] != 7 {
		t.Errorf("Expected evidence_analysis=7, got %f", scores["evidence_analysis"])
	}
	if scores["completeness"] != 6 {
		t.Errorf("Expected completeness=6, got %f", scores["completeness"])
	}
	if scores["clarity"] != 9 {
		t.Errorf("Expected clarity=9, got %f", scores["clarity"])
	}
}

func TestParseCriterionScores_WithSurroundingText(t *testing.T) {
	response := `Here is my evaluation:
{"logical_soundness": {"reasoning": "good logic", "score": 7}, "clarity": {"reasoning": "clear", "score": 8}}
That is my assessment.`

	scores, ok := parseCriterionScores(response)
	if !ok {
		t.Fatal("Expected ok=true for response with surrounding text")
	}
	if len(scores) != 2 {
		t.Fatalf("Expected 2 criterion scores, got %d", len(scores))
	}
}

func TestParseCriterionScores_InvalidFormats(t *testing.T) {
	// Single score format should NOT be parsed as per-criterion
	_, ok := parseCriterionScores(`{"score": 8, "reasoning": "Good"}`)
	if ok {
		t.Error("Expected ok=false for single-score format")
	}

	// Empty response
	_, ok = parseCriterionScores("")
	if ok {
		t.Error("Expected ok=false for empty response")
	}

	// No JSON
	_, ok = parseCriterionScores("This is just text without JSON")
	if ok {
		t.Error("Expected ok=false for text-only response")
	}

	// Out of range scores (score > 10)
	_, ok = parseCriterionScores(`{"clarity": {"reasoning": "good", "score": 15}}`)
	if ok {
		t.Error("Expected ok=false for out-of-range score")
	}
}

func TestParseCriterionScores_KeyNormalization(t *testing.T) {
	// Keys with spaces and mixed case should be normalized
	response := `{"Logical Soundness": {"reasoning": "good", "score": 8}, "Evidence Analysis": {"reasoning": "ok", "score": 7}}`

	scores, ok := parseCriterionScores(response)
	if !ok {
		t.Fatal("Expected ok=true")
	}
	if _, exists := scores["logical_soundness"]; !exists {
		t.Error("Expected normalized key 'logical_soundness' in scores")
	}
	if _, exists := scores["evidence_analysis"]; !exists {
		t.Error("Expected normalized key 'evidence_analysis' in scores")
	}
}

func TestParseScores_MultiWordCriteria(t *testing.T) {
	// Multi-word criteria like "Logical Soundness: 8" should be captured and normalized
	scores := parseScores("Logical Soundness: 8\nEvidence Analysis: 7/10\nCompleteness: 9")
	if scores["logical_soundness"] != 8 {
		t.Errorf("Expected logical_soundness=8, got %v (keys: %v)", scores["logical_soundness"], scores)
	}
	if scores["evidence_analysis"] != 7 {
		t.Errorf("Expected evidence_analysis=7, got %v (keys: %v)", scores["evidence_analysis"], scores)
	}
	if scores["completeness"] != 9 {
		t.Errorf("Expected completeness=9, got %v (keys: %v)", scores["completeness"], scores)
	}
}

func TestCalculateWeightedScore_PartialMatch(t *testing.T) {
	// When 3 of 4 criteria match, weighted score uses only matched weight (renormalized)
	rubric := []RubricCriterion{
		{Name: "Logical Soundness", Weight: 0.4},
		{Name: "Evidence Analysis", Weight: 0.3},
		{Name: "Completeness", Weight: 0.2},
		{Name: "Clarity", Weight: 0.1},
	}
	scores := map[string]float64{
		"logical_soundness": 10,
		"evidence_analysis": 10,
		"completeness":      10,
		// Missing "clarity"
	}
	weighted, matched := calculateWeightedScore(scores, rubric)
	if !matched {
		t.Fatal("Expected matched=true for partial match (3/4 criteria)")
	}
	// totalWeight = 0.4+0.3+0.2 = 0.9; weightedSum = 10*0.9 = 9.0; score = 9.0/0.9 ≈ 10.0
	if math.Abs(weighted-10.0) > 0.01 {
		t.Errorf("Expected weighted score ~10.0 (renormalized from 3 matching criteria), got %f", weighted)
	}
}

func TestParseCriterionScores_RejectsLegacySingleScore(t *testing.T) {
	// Legacy single-score format {"score": 8, "reasoning": "Good"} should NOT be parsed
	// as a criterion score (the old parseEvalResponse bug)
	response := `{"score": 8, "reasoning": "Good response"}`
	scores, ok := parseCriterionScores(response)
	// "score" key would try to unmarshal 8 (number) as {"reasoning":"...","score":N} — should fail
	if ok && len(scores) > 0 {
		t.Errorf("Expected parseCriterionScores to reject legacy single-score format, got scores: %v", scores)
	}
}

func TestParseCriterionScores_RejectsNestedCriterionAsTopLevel(t *testing.T) {
	// Per-criterion format: the inner {"reasoning":"...","score":N} objects should NOT
	// be confused with the top-level structure. This verifies parseCriterionScores
	// correctly handles the full per-criterion format without false-matching.
	response := `{
		"logical_soundness": {"reasoning": "consistent chain of logic", "score": 8},
		"evidence_analysis": {"reasoning": "options well eliminated", "score": 7}
	}`
	scores, ok := parseCriterionScores(response)
	if !ok {
		t.Fatal("Expected ok=true for valid per-criterion format")
	}
	if scores["logical_soundness"] != 8 {
		t.Errorf("Expected logical_soundness=8, got %v", scores["logical_soundness"])
	}
	if scores["evidence_analysis"] != 7 {
		t.Errorf("Expected evidence_analysis=7, got %v", scores["evidence_analysis"])
	}
}

func TestAggregationTemperatureAndMaxTokens(t *testing.T) {
	if _, found := aggregationTemperature(nil); found {
		t.Fatal("expected temperature not found with nil config")
	}
	// nil config → not found
	if _, found := aggregationMaxTokens(nil); found {
		t.Fatal("expected max_tokens not found with nil config")
	}

	cfg := map[string]interface{}{
		"temperature": 0.65,
		"max_tokens":  float64(2048),
	}
	gotTemp, found := aggregationTemperature(cfg)
	if !found {
		t.Fatal("expected temperature to be found")
	}
	if got := *gotTemp; got != 0.65 {
		t.Fatalf("expected overridden temperature 0.65, got %f", got)
	}
	if got, found := aggregationMaxTokens(cfg); !found || got != 2048 {
		t.Fatalf("expected max tokens 2048, got %d (found=%v)", got, found)
	}

	cfg["maxTokens"] = float64(4096)
	delete(cfg, "max_tokens")
	if got, found := aggregationMaxTokens(cfg); !found || got != 4096 {
		t.Fatalf("expected camelCase maxTokens override 4096, got %d (found=%v)", got, found)
	}

	// max_tokens: -1 (unlimited) is valid
	if got, found := aggregationMaxTokens(map[string]interface{}{"max_tokens": -1}); !found || got != -1 {
		t.Fatalf("expected max_tokens -1 (unlimited), got %d (found=%v)", got, found)
	}

	// repair_max_tokens
	if _, found := aggregationRepairMaxTokens(nil); found {
		t.Fatal("expected repair_max_tokens not found with nil config")
	}
	if got, found := aggregationRepairMaxTokens(map[string]interface{}{"repair_max_tokens": 256}); !found || got != 256 {
		t.Fatalf("expected repair_max_tokens 256, got %d (found=%v)", got, found)
	}
	if got, found := aggregationRepairMaxTokens(map[string]interface{}{"repairMaxTokens": 128}); !found || got != 128 {
		t.Fatalf("expected camelCase repairMaxTokens 128, got %d (found=%v)", got, found)
	}
}

func TestMergeAggregationConfigAddsResultLevelOpenRouterConfig(t *testing.T) {
	base := map[string]interface{}{
		"judge_model": "openai/gpt-4o-mini",
	}
	provider := map[string]interface{}{
		"only":            []string{"OpenAI"},
		"allow_fallbacks": false,
	}
	reasoning := map[string]interface{}{
		"effort": "high",
	}

	merged := MergeAggregationConfig(base, provider, reasoning)
	if merged["judge_model"] != "openai/gpt-4o-mini" {
		t.Fatalf("expected judge_model to be preserved, got %+v", merged["judge_model"])
	}
	if merged["openRouterProvider"] == nil {
		t.Fatalf("expected openRouterProvider to be merged, got %+v", merged)
	}
	if merged["openRouterReasoning"] == nil {
		t.Fatalf("expected openRouterReasoning to be merged, got %+v", merged)
	}
}

func TestMergeAggregationConfigPreservesExplicitOpenRouterConfig(t *testing.T) {
	base := map[string]interface{}{
		"openrouter_provider":  map[string]interface{}{"only": []string{"Anthropic"}},
		"openrouter_reasoning": map[string]interface{}{"effort": "low"},
	}
	provider := map[string]interface{}{
		"only": []string{"OpenAI"},
	}
	reasoning := map[string]interface{}{
		"effort": "high",
	}

	merged := MergeAggregationConfig(base, provider, reasoning)
	if merged["openrouter_provider"] == nil {
		t.Fatalf("expected explicit openrouter_provider to be preserved, got %+v", merged)
	}
	if merged["openrouter_reasoning"] == nil {
		t.Fatalf("expected explicit openrouter_reasoning to be preserved, got %+v", merged)
	}
	if _, exists := merged["openRouterProvider"]; exists {
		t.Fatalf("expected no injected openRouterProvider when explicit provider key exists, got %+v", merged)
	}
	if _, exists := merged["openRouterReasoning"]; exists {
		t.Fatalf("expected no injected openRouterReasoning when explicit reasoning key exists, got %+v", merged)
	}
}

func TestDebateDecideFailsOnUnparseableWinner(t *testing.T) {
	agg := &DebateDecideAggregator{}

	// Mock returns gibberish — both judge call and repair call fail to produce a label.
	// Avoid words "a" or "b" as standalone tokens since parseWinner matches single-letter labels.
	registry := providers.NewRegistry()
	mock := NewMockProvider("test")
	mock.SetResponse("impartial judge", "The camps present equally compelling points. No clear verdict.")
	mock.SetResponse("return ONLY the answer option letter", "It is impossible to pick one.")
	registry.Register(mock)
	client := NewMockLLMClient(registry)

	// 3 agents with extractable but different answers → 2 camps
	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: A\n\nBecause reasons."},
		{AgentID: "agent-2", Output: "Final answer: B\n\nBecause other reasons."},
		{AgentID: "agent-3", Output: "Final answer: A\n\nBecause more reasons."},
	}

	config := withDebateDecideConfig(map[string]interface{}{
		"judge_model":       "mock-model",
		"max_tokens":        -1,
		"repair_max_tokens": 256,
	})

	ctx := context.Background()
	_, err := agg.Aggregate(ctx, inputs, config, client, nil)
	if err == nil {
		t.Fatal("Expected error when winner cannot be parsed after repair, got nil")
	}
	if !strings.Contains(err.Error(), "could not parse winning camp after repair call") {
		t.Errorf("Expected parse-failure error message, got: %v", err)
	}
	var retryErr *RetryableError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected RetryableError, got %T: %v", err, err)
	}
	if retryErr.Code != RetryCodeAggParseFailure {
		t.Fatalf("expected code %s, got %s", RetryCodeAggParseFailure, retryErr.Code)
	}
}

func TestDebateDecideRepairCallSuccess(t *testing.T) {
	agg := &DebateDecideAggregator{}

	// Mock: initial judge call → gibberish. Repair call → raw label "B".
	registry := providers.NewRegistry()
	mock := NewMockProvider("test")
	mock.SetResponse("impartial judge", "Both camps present compelling arguments. Camp A has more supporters but Camp B's reasoning is more rigorous.")
	mock.SetResponse("return ONLY the answer option letter", "B")
	registry.Register(mock)
	client := NewMockLLMClient(registry)

	// 3 agents: 2 answer A, 1 answers B → camp A is majority, camp B is minority
	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: A\n\nBecause reasons."},
		{AgentID: "agent-2", Output: "Final answer: B\n\nBecause better reasons."},
		{AgentID: "agent-3", Output: "Final answer: A\n\nBecause more reasons."},
	}

	config := withDebateDecideConfig(map[string]interface{}{
		"judge_model":       "mock-model",
		"max_tokens":        -1,
		"repair_max_tokens": 256,
	})

	ctx := context.Background()
	result, err := agg.Aggregate(ctx, inputs, config, client, nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// B wins via repair — the minority camp should be selected (judge overrides majority)
	if result.Winner != "agent-2" {
		t.Errorf("Expected winner agent-2 (camp B) via repair call, got %q", result.Winner)
	}
	if !strings.Contains(result.Reasoning, "[winner_resolution: repair_call]") {
		t.Errorf("Expected reasoning to contain repair_call resolution, got %q", result.Reasoning)
	}
	// Agreement ratio: winning camp has 1 of 3 extracted agents
	expectedRatio := 1.0 / 3.0
	if math.Abs(result.AgreementRatio-expectedRatio) > 0.01 {
		t.Errorf("Expected agreement ratio ~%.2f, got %.2f", expectedRatio, result.AgreementRatio)
	}
	// Dissenting IDs should be agents from the losing camp
	if len(result.DissentingIDs) != 2 {
		t.Errorf("Expected 2 dissenting agents, got %d: %v", len(result.DissentingIDs), result.DissentingIDs)
	}
}

func TestSelectAttemptObservabilityMetadata_AgreementKeys(t *testing.T) {
	outputMeta := map[string]interface{}{
		"agreement_ratio":  0.67,
		"consensus_answer": "A",
		"dissenting_ids":   []string{"agent-3"},
		"unrelated_key":    "should not pass through",
	}

	result := SelectAttemptObservabilityMetadata(outputMeta)

	if result["agreement_ratio"] != 0.67 {
		t.Errorf("Expected agreement_ratio=0.67, got %v", result["agreement_ratio"])
	}
	if result["consensus_answer"] != "A" {
		t.Errorf("Expected consensus_answer=A, got %v", result["consensus_answer"])
	}
	if result["dissenting_ids"] == nil {
		t.Error("Expected dissenting_ids to pass through")
	}
	if _, exists := result["unrelated_key"]; exists {
		t.Error("Expected unrelated_key to be filtered out")
	}
}

func TestParsePeerMatrixConfigRubricModel(t *testing.T) {
	base := map[string]interface{}{
		"eval_system_prompt": "evaluate",
		"eval_prompt":        "review",
		"normalization":      "none",
		"temperature":        0.0,
		"max_parallel":       2,
		"max_tokens":         256,
		"rubric": []interface{}{
			map[string]interface{}{"name": "accuracy", "weight": 1.0},
		},
	}

	// rubric_model explicitly set
	cfg, err := parsePeerMatrixConfig(map[string]interface{}{
		"eval_system_prompt": "evaluate",
		"eval_prompt":        "review",
		"normalization":      "none",
		"temperature":        0.0,
		"max_parallel":       2,
		"max_tokens":         256,
		"rubric": []interface{}{
			map[string]interface{}{"name": "accuracy", "weight": 1.0},
		},
		"rubric_model": "z-ai/glm-5",
	})
	if err != nil {
		t.Fatalf("parsePeerMatrixConfig failed: %v", err)
	}
	if cfg.RubricModel != "z-ai/glm-5" {
		t.Errorf("Expected RubricModel=z-ai/glm-5, got %q", cfg.RubricModel)
	}

	// rubric_model absent — should be empty
	cfg, err = parsePeerMatrixConfig(base)
	if err != nil {
		t.Fatalf("parsePeerMatrixConfig failed: %v", err)
	}
	if cfg.RubricModel != "" {
		t.Errorf("Expected empty RubricModel when not set, got %q", cfg.RubricModel)
	}

	// judge_model is ignored by parsePeerMatrixConfig (rejected later by ValidateAggregationConfig)
	cfg, err = parsePeerMatrixConfig(map[string]interface{}{
		"eval_system_prompt": "evaluate",
		"eval_prompt":        "review",
		"normalization":      "none",
		"temperature":        0.0,
		"max_parallel":       2,
		"max_tokens":         256,
		"rubric": []interface{}{
			map[string]interface{}{"name": "accuracy", "weight": 1.0},
		},
		"judge_model": "openai/gpt-4o-mini",
	})
	if err != nil {
		t.Fatalf("parsePeerMatrixConfig failed: %v", err)
	}
	if cfg.RubricModel != "" {
		t.Errorf("Expected rubric model to stay empty when only judge_model is set, got %q", cfg.RubricModel)
	}
}
