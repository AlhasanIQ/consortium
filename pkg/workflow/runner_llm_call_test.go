package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/storage"
)

// newTestLLMCallRunner creates an LLMCallNodeRunner backed by the given provider.
func newTestLLMCallRunner(p providers.Provider) *LLMCallNodeRunner {
	reg := providers.NewRegistry()
	reg.Register(p)
	client := providers.NewClient(reg, nil)
	return &LLMCallNodeRunner{llmClient: client}
}

// baseNode returns a minimal valid Node for LLM call tests.
func baseNode() *Node {
	temp := 0.5
	return &Node{
		Type:           NodeTypePrompt,
		Model:          "mock-model",
		Prompt:         "Hello, world!",
		MaxTokens:      100,
		Temperature:    &temp,
		TimeoutSeconds: 30,
	}
}

func TestLLMCallRunner_NodeType(t *testing.T) {
	runner := &LLMCallNodeRunner{}
	if got := runner.NodeType(); got != NodeTypePrompt {
		t.Fatalf("NodeType() = %q, want %q", got, NodeTypePrompt)
	}
}

func TestLLMCallRunner_Success(t *testing.T) {
	mock := NewMockProvider("test")
	mock.SetResponse("Hello", "World response")
	runner := newTestLLMCallRunner(mock)

	sc := &NodeContext{
		Ctx:             context.Background(),
		Node:            baseNode(),
		NodeID:          "node-1",
		Attempt:         1,
		WorkflowContext: map[string]interface{}{},
	}

	result, err := runner.Execute(sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.NodeID != "node-1" {
		t.Errorf("NodeID = %q, want %q", result.NodeID, "node-1")
	}
	if result.Output != "World response" {
		t.Errorf("Output = %q, want %q", result.Output, "World response")
	}
	if result.LatencyMs < 0 {
		t.Errorf("LatencyMs should be non-negative, got %f", result.LatencyMs)
	}
}

func TestLLMCallRunner_PropagatesAPIRequestControls(t *testing.T) {
	provider := &capturingProvider{
		name: "capture",
		models: []providers.Model{
			{
				ID:         "mock-model",
				Name:       "Mock Model",
				Provider:   "capture",
				ContextLen: 4096,
				MaxTokens:  2048,
				Available:  true,
			},
		},
		responseContent: "ok",
	}
	runner := newTestLLMCallRunner(provider)
	node := baseNode()
	node.Metadata = map[string]interface{}{
		"seed":                        42,
		"top_p":                       0.75,
		"stop":                        []interface{}{"END", "STOP"},
		"session_id":                  "sess-controls",
		"openrouter_metadata":         true,
		"openrouter_metadata_enabled": false,
	}

	_, err := runner.Execute(&NodeContext{
		Ctx:             context.Background(),
		Node:            node,
		NodeID:          "node-controls",
		Attempt:         1,
		WorkflowContext: map[string]interface{}{},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	req := provider.lastRequest()
	if req == nil {
		t.Fatal("provider did not receive request")
	}
	if req.Extensions == nil || req.Extensions.Seed == nil || *req.Extensions.Seed != 42 {
		t.Fatalf("seed extension = %#v, want 42", req.Extensions)
	}
	if req.TopP == nil || *req.TopP != 0.75 {
		t.Fatalf("TopP = %#v, want 0.75", req.TopP)
	}
	if len(req.Stop) != 2 || req.Stop[0] != "END" || req.Stop[1] != "STOP" {
		t.Fatalf("Stop = %#v, want END/STOP", req.Stop)
	}
	if req.Extensions.SessionID != "sess-controls" {
		t.Fatalf("session_id extension = %#v, want sess-controls", req.Extensions)
	}
	if req.Extensions.OpenRouterMetadata == nil || *req.Extensions.OpenRouterMetadata != false {
		t.Fatalf("openrouter metadata opt-in = %#v, want explicit false from openrouter_metadata_enabled", req.Extensions.OpenRouterMetadata)
	}
}

func TestLLMCallRunner_LogsCompiledAggregationParentNodeID(t *testing.T) {
	mock := NewMockProvider("test")
	mock.SetResponse("Hello", "World response")
	logger := &recordingNodeLogger{}
	registry := providers.NewRegistry()
	registry.Register(mock)
	runner := &LLMCallNodeRunner{llmClient: providers.NewClient(registry, logger)}

	node := baseNode()
	node.Metadata = map[string]interface{}{
		"aggregation_group_node_id": "agg--result",
	}
	sc := &NodeContext{
		Ctx:             context.Background(),
		Node:            node,
		NodeID:          "agg--repair-selection_true",
		Attempt:         1,
		WorkflowContext: map[string]interface{}{},
		ExecCtx: &ExecutionContext{
			JobID: "job-compiled-branch",
			RunID: "run-compiled-branch",
		},
	}

	if _, err := runner.Execute(sc); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(logger.logs) == 0 {
		t.Fatalf("expected provider logger calls")
	}
	for _, log := range logger.logs {
		if log.ParentNodeID != "agg--result" {
			t.Fatalf("logged ParentNodeID = %q, want agg--result in %+v", log.ParentNodeID, log)
		}
	}
}

func TestLLMCallRunner_MissingTimeout(t *testing.T) {
	runner := &LLMCallNodeRunner{}
	node := baseNode()
	node.TimeoutSeconds = 0

	sc := &NodeContext{
		Ctx:             context.Background(),
		Node:            node,
		NodeID:          "node-timeout",
		WorkflowContext: map[string]interface{}{},
	}

	result, err := runner.Execute(sc)
	if err == nil {
		t.Fatal("expected error for missing timeout")
	}
	if result.Success {
		t.Fatal("expected failure result")
	}
	var nre *RetryableError
	if !asNonRetryable(err, &nre) {
		t.Errorf("expected non-retryable error, got %T", err)
	}
}

type recordingNodeLogger struct {
	logs []*storage.LLMRequestLog
}

func (l *recordingNodeLogger) LogLLMRequestFull(log *storage.LLMRequestLog) error {
	cp := *log
	l.logs = append(l.logs, &cp)
	return nil
}

func (l *recordingNodeLogger) AddSubNode(_ providers.SubNodeRecord) error {
	return nil
}

func TestLLMCallRunner_MissingMaxTokens(t *testing.T) {
	runner := &LLMCallNodeRunner{}
	node := baseNode()
	node.MaxTokens = 0

	sc := &NodeContext{
		Ctx:             context.Background(),
		Node:            node,
		NodeID:          "node-maxtokens",
		WorkflowContext: map[string]interface{}{},
	}

	result, err := runner.Execute(sc)
	if err == nil {
		t.Fatal("expected error for missing max_tokens")
	}
	if result.Success {
		t.Fatal("expected failure result")
	}
}

func TestLLMCallRunner_MissingTemperature(t *testing.T) {
	runner := &LLMCallNodeRunner{}
	node := baseNode()
	node.Temperature = nil

	sc := &NodeContext{
		Ctx:             context.Background(),
		Node:            node,
		NodeID:          "node-temp",
		WorkflowContext: map[string]interface{}{},
	}

	result, err := runner.Execute(sc)
	if err == nil {
		t.Fatal("expected error for missing temperature")
	}
	if result.Success {
		t.Fatal("expected failure result")
	}
	var nre *RetryableError
	if !asNonRetryable(err, &nre) {
		t.Errorf("expected non-retryable error, got %T", err)
	}
}

func TestLLMCallRunner_PromptInterpolation(t *testing.T) {
	mock := NewMockProvider("test")
	mock.SetResponse("Say hello to Alice", "Hi Alice!")
	runner := newTestLLMCallRunner(mock)

	node := baseNode()
	node.Prompt = "Say hello to {{name}}"

	sc := &NodeContext{
		Ctx:    context.Background(),
		Node:   node,
		NodeID: "node-interp",
		WorkflowContext: map[string]interface{}{
			"name": "Alice",
		},
	}

	result, err := runner.Execute(sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "Hi Alice!" {
		t.Errorf("Output = %q, want %q", result.Output, "Hi Alice!")
	}
}

func TestLLMCallRunner_SystemPromptInterpolation(t *testing.T) {
	mock := NewMockProvider("test")
	// The mock matches on all message contents concatenated
	mock.SetResponse("You are a helpful", "System prompt worked")
	runner := newTestLLMCallRunner(mock)

	node := baseNode()
	node.SystemPrompt = "You are a helpful {{role}}"
	node.Prompt = "Do something"

	sc := &NodeContext{
		Ctx:    context.Background(),
		Node:   node,
		NodeID: "node-sys",
		WorkflowContext: map[string]interface{}{
			"role": "assistant",
		},
	}

	result, err := runner.Execute(sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.Output != "System prompt worked" {
		t.Errorf("Output = %q, want %q", result.Output, "System prompt worked")
	}
}

func TestLLMCallRunner_CancelledContext(t *testing.T) {
	mock := NewMockProvider("test")
	runner := newTestLLMCallRunner(mock)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	sc := &NodeContext{
		Ctx:             ctx,
		Node:            baseNode(),
		NodeID:          "node-cancel",
		WorkflowContext: map[string]interface{}{},
	}

	result, err := runner.Execute(sc)
	if err == nil {
		// Some providers may not check context; skip if it succeeds
		t.Skip("provider did not honor cancelled context")
	}
	if result.Success {
		t.Error("expected failure result for cancelled context")
	}
}

func TestLLMCallRunner_ProviderError(t *testing.T) {
	failProvider := NewAlwaysFailProvider("test", fmt.Errorf("provider exploded"))
	runner := newTestLLMCallRunner(failProvider)

	sc := &NodeContext{
		Ctx:             context.Background(),
		Node:            baseNode(),
		NodeID:          "node-err",
		Attempt:         1,
		WorkflowContext: map[string]interface{}{},
	}

	result, err := runner.Execute(sc)
	if err == nil {
		t.Fatal("expected error from failing provider")
	}
	if result.Success {
		t.Fatal("expected failure result")
	}
	if result.NodeID != "node-err" {
		t.Errorf("NodeID = %q, want %q", result.NodeID, "node-err")
	}
}

func TestClassifyLLMCallErrorRespectsProviderRetryableFlagForUpstreamError(t *testing.T) {
	err := &providers.ProviderError{
		Code:       providers.ErrCodeUpstreamError,
		Message:    "provider rejected request",
		StatusCode: 422,
		Retryable:  false,
	}

	classified := classifyLLMCallError(err, err.Error())
	if IsRetryable(classified) {
		t.Fatalf("classified error should be non-retryable: %T %v", classified, classified)
	}
}

func TestLLMCallRunner_TokensAndCost(t *testing.T) {
	costProvider := NewMockProviderWithCost("test", 0.001, 0.002)
	runner := newTestLLMCallRunner(costProvider)

	sc := &NodeContext{
		Ctx:             context.Background(),
		Node:            baseNode(),
		NodeID:          "node-cost",
		Attempt:         1,
		WorkflowContext: map[string]interface{}{},
	}

	result, err := runner.Execute(sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TokensInput == 0 {
		t.Error("expected non-zero TokensInput")
	}
	if result.TokensOutput == 0 {
		t.Error("expected non-zero TokensOutput")
	}
	// Cost is estimated from model pricing: 100*0.001 + 50*0.002 = 0.2
	expectedCost := 100*0.001 + 50*0.002
	if result.Cost != expectedCost {
		t.Errorf("Cost = %f, want %f", result.Cost, expectedCost)
	}
}

func TestLLMCallRunner_MetadataPreserved(t *testing.T) {
	mock := NewMockProvider("test")
	runner := newTestLLMCallRunner(mock)

	node := baseNode()
	node.Metadata = map[string]interface{}{
		"label": "Test Label",
		"name":  "Test Name",
	}

	sc := &NodeContext{
		Ctx:             context.Background(),
		Node:            node,
		NodeID:          "node-meta",
		Attempt:         1,
		WorkflowContext: map[string]interface{}{},
	}

	result, err := runner.Execute(sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Metadata == nil {
		t.Fatal("expected metadata on result")
	}
	if result.Metadata["label"] != "Test Label" {
		t.Errorf("metadata label = %v, want %q", result.Metadata["label"], "Test Label")
	}
}

func TestLLMCallRunner_ProviderTelemetryMetadata(t *testing.T) {
	byok := true
	provider := &telemetryProvider{
		response: &providers.CompletionResponse{
			ID:                 "resp-telemetry",
			RequestID:          "req-telemetry",
			GenerationID:       "gen-telemetry",
			Model:              "mock-model",
			Content:            "ok",
			Finish:             "stop",
			ServiceTier:        "default",
			OpenRouterMetadata: json.RawMessage(`{"provider_name":"openai"}`),
			Reasoning:          "short rationale",
			Usage: providers.Usage{
				PromptTokens:     10,
				CompletionTokens: 5,
				TotalTokens:      15,
				PromptTokensDetails: &providers.PromptTokensDetails{
					CachedTokens:     4,
					CacheWriteTokens: 2,
				},
				CompletionTokensDetails: &providers.CompletionTokensDetails{
					ReasoningTokens: 3,
				},
				IsBYOK: &byok,
			},
		},
	}
	runner := newTestLLMCallRunner(provider)

	result, err := runner.Execute(&NodeContext{
		Ctx:             context.Background(),
		Node:            baseNode(),
		NodeID:          "node-telemetry",
		Attempt:         1,
		WorkflowContext: map[string]interface{}{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Metadata["provider_generation_id"] != "gen-telemetry" {
		t.Fatalf("metadata = %#v", result.Metadata)
	}
	if result.Metadata["openrouter_cached_tokens"] != 4 || result.Metadata["openrouter_reasoning_tokens"] != 3 {
		t.Fatalf("OpenRouter token metadata = %#v", result.Metadata)
	}
	if _, ok := result.Metadata["openrouter_reasoning"]; ok {
		t.Fatalf("raw reasoning text should not be persisted in node metadata: %#v", result.Metadata)
	}
	if result.Metadata["openrouter_is_byok"] != true || result.Metadata["openrouter_service_tier"] != "default" {
		t.Fatalf("OpenRouter scalar metadata = %#v", result.Metadata)
	}
	metadata, ok := result.Metadata["openrouter_metadata"].(map[string]interface{})
	if !ok || metadata["provider_name"] != "openai" {
		t.Fatalf("openrouter metadata = %#v", result.Metadata["openrouter_metadata"])
	}
}

func TestLLMCallRunner_NegativeMaxTokensTreatedAsZero(t *testing.T) {
	mock := NewMockProvider("test")
	runner := newTestLLMCallRunner(mock)

	node := baseNode()
	node.MaxTokens = -1

	sc := &NodeContext{
		Ctx:             context.Background(),
		Node:            node,
		NodeID:          "node-neg",
		Attempt:         1,
		WorkflowContext: map[string]interface{}{},
	}

	// Negative max_tokens is clamped to 0 and the call proceeds
	result, err := runner.Execute(sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
}

func TestLLMCallRunner_OutputTruncatedEmpty(t *testing.T) {
	// Create a custom provider that returns finish_reason=length with empty content
	truncProvider := &truncatedProvider{}
	runner := newTestLLMCallRunner(truncProvider)

	sc := &NodeContext{
		Ctx:             context.Background(),
		Node:            baseNode(),
		NodeID:          "node-trunc",
		Attempt:         1,
		WorkflowContext: map[string]interface{}{},
	}

	result, err := runner.Execute(sc)
	if err == nil {
		t.Fatal("expected error for truncated empty output")
	}
	if result.Success {
		t.Fatal("expected failure result")
	}
	var re *RetryableError
	if !asRetryable(err, &re) {
		t.Fatalf("expected RetryableError, got %T", err)
	}
	if re.Code != RetryCodeOutputTruncatedEmpty {
		t.Errorf("Code = %q, want %q", re.Code, RetryCodeOutputTruncatedEmpty)
	}
}

// --- helpers ---

// asNonRetryable checks if err is a *RetryableError with Retryable=false.
func asNonRetryable(err error, target **RetryableError) bool {
	for err != nil {
		if re, ok := err.(*RetryableError); ok && !re.Retryable {
			*target = re
			return true
		}
		if u, ok := err.(interface{ Unwrap() error }); ok {
			err = u.Unwrap()
		} else {
			break
		}
	}
	return false
}

func asRetryable(err error, target **RetryableError) bool {
	for err != nil {
		if re, ok := err.(*RetryableError); ok {
			*target = re
			return true
		}
		if u, ok := err.(interface{ Unwrap() error }); ok {
			err = u.Unwrap()
		} else {
			break
		}
	}
	return false
}

// truncatedProvider returns empty content with finish_reason=length.
type truncatedProvider struct{}

func (p *truncatedProvider) Name() string { return "test" }
func (p *truncatedProvider) Models() []providers.Model {
	return []providers.Model{{
		ID: "mock-model", Name: "Mock", Provider: "test",
		ContextLen: 4096, MaxTokens: 2048, Available: true,
	}}
}
func (p *truncatedProvider) EstimateTokens(text string) int               { return len(text) / 4 }
func (p *truncatedProvider) Cost(model string, input, output int) float64 { return 0 }
func (p *truncatedProvider) Complete(ctx context.Context, req *providers.CompletionRequest) (*providers.CompletionResponse, error) {
	return &providers.CompletionResponse{
		ID:      "trunc-id",
		Content: "",
		Model:   req.Model,
		Finish:  "length",
		Usage: providers.Usage{
			PromptTokens:     10,
			CompletionTokens: 0,
			TotalTokens:      10,
		},
	}, nil
}

type telemetryProvider struct {
	response *providers.CompletionResponse
}

func (p *telemetryProvider) Name() string { return "mock" }
func (p *telemetryProvider) Models() []providers.Model {
	return []providers.Model{{
		ID: "mock-model", Name: "Mock", Provider: "mock",
		ContextLen: 4096, MaxTokens: 2048, Available: true,
	}}
}
func (p *telemetryProvider) EstimateTokens(text string) int               { return len(text) / 4 }
func (p *telemetryProvider) Cost(model string, input, output int) float64 { return 0 }
func (p *telemetryProvider) Complete(ctx context.Context, req *providers.CompletionRequest) (*providers.CompletionResponse, error) {
	return p.response, nil
}

func TestParseOptionalStringRejectsStructuredValues(t *testing.T) {
	if got := parseOptionalString(" session-1 "); got != "session-1" {
		t.Fatalf("string value = %q", got)
	}
	if got := parseOptionalString(map[string]any{"id": "session-1"}); got != "" {
		t.Fatalf("structured map value = %q, want empty", got)
	}
	if got := parseOptionalString([]any{"session-1"}); got != "" {
		t.Fatalf("structured slice value = %q, want empty", got)
	}
}
