package workflow

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/alhasaniq/consortium/pkg/providers"
)

// --- RunnerRegistry tests ---

func TestRunnerRegistry_RegisterAndGet(t *testing.T) {
	registry := NewRunnerRegistry()

	runner := &ResultNodeRunner{
		aggregatorRegistry: NewAggregatorRegistry(),
	}
	registry.Register(runner)

	got, ok := registry.Get(NodeTypeResult)
	if !ok {
		t.Fatal("expected runner to be found")
	}
	if got.NodeType() != NodeTypeResult {
		t.Errorf("NodeType() = %q, want %q", got.NodeType(), NodeTypeResult)
	}
}

func TestRunnerRegistry_GetUnknown(t *testing.T) {
	registry := NewRunnerRegistry()

	_, ok := registry.Get(NodeType("unknown"))
	if ok {
		t.Error("expected unknown node type to return false")
	}
}

func TestRunnerRegistry_OverwriteRegistration(t *testing.T) {
	registry := NewRunnerRegistry()

	runner1 := &ResultNodeRunner{aggregatorRegistry: NewAggregatorRegistry()}
	runner2 := &ResultNodeRunner{aggregatorRegistry: NewAggregatorRegistry()}

	registry.Register(runner1)
	registry.Register(runner2)

	got, ok := registry.Get(NodeTypeResult)
	if !ok {
		t.Fatal("expected runner to be found")
	}
	// Should be the second registration
	if got != runner2 {
		t.Error("expected second registration to overwrite first")
	}
}

func TestNewExecutorRegistersOperationRunner(t *testing.T) {
	executor := NewExecutor(nil)

	runner, ok := executor.RunnerRegistry().Get(NodeTypeOperation)
	if !ok {
		t.Fatal("expected operation runner to be registered")
	}
	if _, ok := runner.(*OperationNodeRunner); !ok {
		t.Fatalf("expected *OperationNodeRunner, got %T", runner)
	}
}

// --- LLMCallNodeRunner tests ---

func TestLLMCallNodeRunner_NodeType(t *testing.T) {
	runner := &LLMCallNodeRunner{}
	if runner.NodeType() != NodeTypePrompt {
		t.Errorf("NodeType() = %q, want %q", runner.NodeType(), NodeTypePrompt)
	}
}

func TestLLMCallNodeRunner_Execute(t *testing.T) {
	registry := newMockRegistry()
	client := NewMockLLMClient(registry.registry)
	runner := &LLMCallNodeRunner{llmClient: client}

	sc := &NodeContext{
		Ctx:             context.Background(),
		Node:            &Node{Type: NodeTypePrompt, Model: "mock-model", Prompt: "hello"},
		NodeID:          "step_0",
		WorkflowContext: map[string]interface{}{},
	}

	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	if result.NodeID != "step_0" {
		t.Errorf("NodeID = %q, want %q", result.NodeID, "step_0")
	}
	if result.Output == "" {
		t.Error("expected non-empty output")
	}
}

func TestLLMCallNodeRunner_InterpolatesVariables(t *testing.T) {
	registry := newMockRegistry()
	provider := registry.providers["test"]
	provider.SetResponse("world", "Hello world response")
	client := NewMockLLMClient(registry.registry)
	runner := &LLMCallNodeRunner{llmClient: client}

	sc := &NodeContext{
		Ctx:             context.Background(),
		Node:            &Node{Type: NodeTypePrompt, Model: "mock-model", Prompt: "Say {{greeting}}"},
		NodeID:          "step_0",
		WorkflowContext: map[string]interface{}{"greeting": "world"},
	}

	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestLLMCallNodeRunner_CostTracking(t *testing.T) {
	registry := newMockRegistryWithCost(0.001, 0.002)
	client := NewMockLLMClient(registry)
	runner := &LLMCallNodeRunner{llmClient: client}

	tracker := NewCostTracker(&CostLimits{MaxCostUSD: 10.0})

	sc := &NodeContext{
		Ctx:             context.Background(),
		Node:            &Node{Type: NodeTypePrompt, Model: "mock-model", Prompt: "test"},
		NodeID:          "step_0",
		WorkflowContext: map[string]interface{}{},
		CostTracker:     tracker,
	}

	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}

	cost, tokens, _, _ := tracker.GetTotals()
	if cost <= 0 {
		t.Error("expected cost > 0")
	}
	if tokens <= 0 {
		t.Error("expected tokens > 0")
	}
}

func TestLLMCallNodeRunner_CostLimitExceeded(t *testing.T) {
	// Cost limit checking is centralized in the executor (A3).
	// The runner still adds cost, so the tracker accumulates spend.
	// Verify the tracker exceeds the limit after execution.
	registry := newMockRegistryWithCost(0.001, 0.002)
	client := NewMockLLMClient(registry)
	runner := &LLMCallNodeRunner{llmClient: client}

	tracker := NewCostTracker(&CostLimits{MaxCostUSD: 0.0001})

	sc := &NodeContext{
		Ctx:             context.Background(),
		Node:            &Node{Type: NodeTypePrompt, Model: "mock-model", Prompt: "test"},
		NodeID:          "step_0",
		WorkflowContext: map[string]interface{}{},
		CostTracker:     tracker,
	}

	_, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("Execute should succeed (cost checking is in executor): %v", err)
	}
	// After execution, the tracker should have accumulated cost beyond the limit.
	if limErr := tracker.CheckLimits("step_0"); limErr == nil {
		t.Fatal("expected cost tracker to exceed limit after LLM call")
	}
}

func TestLLMCallNodeRunner_OpenRouterProviderRouting(t *testing.T) {
	reg := providers.NewRegistry()
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
	}
	reg.Register(provider)

	client := NewMockLLMClient(reg)
	runner := &LLMCallNodeRunner{llmClient: client}

	sc := &NodeContext{
		Ctx: context.Background(),
		Node: &Node{
			Type:   NodeTypePrompt,
			Model:  "mock-model",
			Prompt: "hello",
			Metadata: map[string]interface{}{
				"openrouter_provider": map[string]interface{}{
					"order":           []string{"OpenAI"},
					"allow_fallbacks": false,
				},
			},
		},
		NodeID:          "step_0",
		WorkflowContext: map[string]interface{}{},
	}

	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}

	req := provider.lastRequest()
	if req == nil || req.Extensions == nil || req.Extensions.ProviderRouting == nil {
		t.Fatalf("expected ProviderRouting on provider request, got %+v", req)
	}
	if len(req.Extensions.ProviderRouting.Order) != 1 || req.Extensions.ProviderRouting.Order[0] != "OpenAI" {
		t.Fatalf("expected order=[OpenAI], got %+v", req.Extensions.ProviderRouting.Order)
	}
	if req.Extensions.ProviderRouting.AllowFallbacks == nil || *req.Extensions.ProviderRouting.AllowFallbacks {
		t.Fatalf("expected allow_fallbacks=false, got %+v", req.Extensions.ProviderRouting.AllowFallbacks)
	}
}

func TestLLMCallNodeRunner_OpenRouterReasoning(t *testing.T) {
	reg := providers.NewRegistry()
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
	}
	reg.Register(provider)

	client := NewMockLLMClient(reg)
	runner := &LLMCallNodeRunner{llmClient: client}

	sc := &NodeContext{
		Ctx: context.Background(),
		Node: &Node{
			Type:   NodeTypePrompt,
			Model:  "mock-model",
			Prompt: "hello",
			Metadata: map[string]interface{}{
				"openrouter_reasoning": map[string]interface{}{
					"effort": "high",
				},
			},
		},
		NodeID:          "step_0",
		WorkflowContext: map[string]interface{}{},
	}

	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}

	req := provider.lastRequest()
	if req == nil || req.Extensions == nil || req.Extensions.Reasoning == nil {
		t.Fatalf("expected Reasoning on provider request, got %+v", req)
	}
	if req.Extensions.Reasoning.Effort != "high" {
		t.Fatalf("expected effort=high, got %+v", req.Extensions.Reasoning.Effort)
	}
}

func TestLLMCallNodeRunner_TruncationReturnsRetryableCode(t *testing.T) {
	reg := providers.NewRegistry()
	provider := &capturingProvider{
		name: "capture",
		models: []providers.Model{
			{
				ID:         "mock-model",
				Name:       "Mock Model",
				Provider:   "capture",
				ContextLen: 4096,
				MaxTokens:  4096,
				Available:  true,
			},
		},
		responseContent: " ",
		finishReason:    "length",
	}
	reg.Register(provider)

	client := NewMockLLMClient(reg)
	runner := &LLMCallNodeRunner{llmClient: client}

	sc := &NodeContext{
		Ctx: context.Background(),
		Node: &Node{
			Type:      NodeTypePrompt,
			Model:     "mock-model",
			Prompt:    "hello",
			MaxTokens: 4096,
		},
		NodeID:          "step_0",
		WorkflowContext: map[string]interface{}{},
	}

	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err == nil {
		t.Fatalf("expected truncation error")
	}
	if result.Success {
		t.Fatalf("expected failed result for truncation")
	}
	retryErr, ok := err.(*RetryableError)
	if !ok {
		t.Fatalf("expected RetryableError, got %T", err)
	}
	if retryErr.Code != RetryCodeOutputTruncatedEmpty {
		t.Fatalf("expected retry code %s, got %s", RetryCodeOutputTruncatedEmpty, retryErr.Code)
	}
}

func TestLLMCallNodeRunner_ClassifiesRateLimitWaitDeadline(t *testing.T) {
	reg := providers.NewRegistry()
	provider := &capturingProvider{
		name: "capture",
		models: []providers.Model{
			{
				ID:         "mock-model",
				Name:       "Mock Model",
				Provider:   "capture",
				ContextLen: 4096,
				MaxTokens:  4096,
				Available:  true,
			},
		},
		callErr: fmt.Errorf("rate limit exceeded: rate limit wait (405ms) exceeds context deadline (120ms remaining)"),
	}
	reg.Register(provider)

	client := NewMockLLMClient(reg)
	runner := &LLMCallNodeRunner{llmClient: client}

	sc := &NodeContext{
		Ctx: context.Background(),
		Node: &Node{
			Type:   NodeTypePrompt,
			Model:  "mock-model",
			Prompt: "hello",
		},
		NodeID:          "step_0",
		WorkflowContext: map[string]interface{}{},
	}

	_, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err == nil {
		t.Fatalf("expected rate-limit deadline error")
	}
	retryErr, ok := err.(*RetryableError)
	if !ok {
		t.Fatalf("expected RetryableError, got %T", err)
	}
	if retryErr.Code != RetryCodeRateLimitWaitDeadline {
		t.Fatalf("expected retry code %s, got %s", RetryCodeRateLimitWaitDeadline, retryErr.Code)
	}
}

func TestLLMCallNodeRunner_PreservesUpstreamTimeoutMessage(t *testing.T) {
	reg := providers.NewRegistry()
	provider := &capturingProvider{
		name: "capture",
		models: []providers.Model{
			{
				ID:         "mock-model",
				Name:       "Mock Model",
				Provider:   "capture",
				ContextLen: 4096,
				MaxTokens:  4096,
				Available:  true,
			},
		},
		callErr: fmt.Errorf("failed to read response: %w", context.DeadlineExceeded),
	}
	reg.Register(provider)

	client := NewMockLLMClient(reg)
	runner := &LLMCallNodeRunner{llmClient: client}

	sc := &NodeContext{
		Ctx: context.Background(),
		Node: &Node{
			Type:           NodeTypePrompt,
			Model:          "mock-model",
			Prompt:         "hello",
			TimeoutSeconds: 360,
		},
		NodeID:          "step_0",
		WorkflowContext: map[string]interface{}{},
	}

	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if result.Success {
		t.Fatalf("expected failed result")
	}
	if result.Error != "failed to read response: context deadline exceeded" {
		t.Fatalf("expected upstream timeout message, got %q", result.Error)
	}
	retryErr, ok := err.(*RetryableError)
	if !ok {
		t.Fatalf("expected RetryableError, got %T", err)
	}
	if retryErr.Code != "TIMEOUT" {
		t.Fatalf("expected retry code TIMEOUT, got %s", retryErr.Code)
	}
}

func TestParseResponseFormat_Nil(t *testing.T) {
	if rf := parseResponseFormat(nil); rf != nil {
		t.Fatalf("expected nil for nil input, got %+v", rf)
	}
}

func TestParseResponseFormat_JsonSchema(t *testing.T) {
	input := map[string]interface{}{
		"type": "json_schema",
		"json_schema": map[string]interface{}{
			"name":   "benchmark_answer",
			"strict": true,
			"schema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"answer": map[string]interface{}{
						"type": "string",
					},
				},
				"required":             []interface{}{"answer"},
				"additionalProperties": false,
			},
		},
	}
	rf := parseResponseFormat(input)
	if rf == nil {
		t.Fatal("expected non-nil ResponseFormat")
	}
	if rf.Type != "json_schema" {
		t.Fatalf("expected type json_schema, got %s", rf.Type)
	}
	if rf.JsonSchema == nil {
		t.Fatal("expected non-nil JsonSchema")
	}
	if rf.JsonSchema.Name != "benchmark_answer" {
		t.Fatalf("expected schema name benchmark_answer, got %s", rf.JsonSchema.Name)
	}
	if !rf.JsonSchema.Strict {
		t.Fatal("expected strict=true")
	}
	if len(rf.JsonSchema.Schema) == 0 {
		t.Fatal("expected non-empty schema")
	}
}

func TestParseResponseFormat_JsonObject(t *testing.T) {
	input := map[string]interface{}{
		"type": "json_object",
	}
	rf := parseResponseFormat(input)
	if rf == nil {
		t.Fatal("expected non-nil ResponseFormat")
	}
	if rf.Type != "json_object" {
		t.Fatalf("expected type json_object, got %s", rf.Type)
	}
	if rf.JsonSchema != nil {
		t.Fatalf("expected nil JsonSchema for json_object, got %+v", rf.JsonSchema)
	}
}

func TestParseResponseFormat_EmptyType(t *testing.T) {
	input := map[string]interface{}{
		"type": "",
	}
	if rf := parseResponseFormat(input); rf != nil {
		t.Fatalf("expected nil for empty type, got %+v", rf)
	}
}

func TestParseResponseFormat_InvalidInput(t *testing.T) {
	if rf := parseResponseFormat("not a map"); rf != nil {
		t.Fatalf("expected nil for string input, got %+v", rf)
	}
}

func TestLLMCallNodeRunner_ResponseFormat(t *testing.T) {
	reg := providers.NewRegistry()
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
	}
	reg.Register(provider)

	client := NewMockLLMClient(reg)
	runner := &LLMCallNodeRunner{llmClient: client}

	sc := &NodeContext{
		Ctx: context.Background(),
		Node: &Node{
			Type:   NodeTypePrompt,
			Model:  "mock-model",
			Prompt: "hello",
			Metadata: map[string]interface{}{
				"response_format": map[string]interface{}{
					"type": "json_schema",
					"json_schema": map[string]interface{}{
						"name":   "test_schema",
						"strict": true,
						"schema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"answer": map[string]interface{}{"type": "string"},
							},
							"required":             []interface{}{"answer"},
							"additionalProperties": false,
						},
					},
				},
			},
		},
		NodeID:          "step_0",
		WorkflowContext: map[string]interface{}{},
	}

	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}

	req := provider.lastRequest()
	if req == nil || req.Extensions == nil || req.Extensions.ResponseFormat == nil {
		t.Fatalf("expected ResponseFormat on provider request, got %+v", req)
	}
	if req.Extensions.ResponseFormat.Type != "json_schema" {
		t.Fatalf("expected type=json_schema, got %s", req.Extensions.ResponseFormat.Type)
	}
	if req.Extensions.ResponseFormat.JsonSchema == nil {
		t.Fatal("expected non-nil JsonSchema")
	}
	if req.Extensions.ResponseFormat.JsonSchema.Name != "test_schema" {
		t.Fatalf("expected schema name test_schema, got %s", req.Extensions.ResponseFormat.JsonSchema.Name)
	}
}

// --- ConditionalNodeRunner tests ---

func TestConditionalNodeRunner_NodeType(t *testing.T) {
	runner := &ConditionalNodeRunner{}
	if runner.NodeType() != NodeTypeConditional {
		t.Errorf("NodeType() = %q, want %q", runner.NodeType(), NodeTypeConditional)
	}
}

func TestConditionalNodeRunner_TrueBranch(t *testing.T) {
	// Mock node executor that returns a fixed result
	mockExecutor := NodeExecutorFunc(func(ctx context.Context, node *Node, nodeID string, wfCtx map[string]interface{}, execCtx *ExecutionContext, ct *CostTracker) (*NodeResult, error) {
		return &NodeResult{NodeID: nodeID, Success: true, Output: "branch executed"}, nil
	})

	runner := &ConditionalNodeRunner{executor: mockExecutor}

	sc := &NodeContext{
		Ctx: context.Background(),
		Node: &Node{
			Type:       NodeTypeConditional,
			Condition:  "status contains active",
			TrueBranch: &Node{Type: NodeTypeResult},
		},
		NodeID:          "cond_0",
		WorkflowContext: map[string]interface{}{"status": "active"},
	}

	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if result.Output != "branch executed" {
		t.Errorf("Output = %q, want %q", result.Output, "branch executed")
	}
	// NodeID should be set to the parent conditional node
	if result.NodeID != "cond_0" {
		t.Errorf("NodeID = %q, want %q", result.NodeID, "cond_0")
	}
}

func TestConditionalNodeRunner_FalseBranch(t *testing.T) {
	mockExecutor := NodeExecutorFunc(func(ctx context.Context, node *Node, nodeID string, wfCtx map[string]interface{}, execCtx *ExecutionContext, ct *CostTracker) (*NodeResult, error) {
		return &NodeResult{NodeID: nodeID, Success: true, Output: "false branch"}, nil
	})

	runner := &ConditionalNodeRunner{executor: mockExecutor}

	sc := &NodeContext{
		Ctx: context.Background(),
		Node: &Node{
			Type:        NodeTypeConditional,
			Condition:   "status equals active",
			FalseBranch: &Node{Type: NodeTypeResult},
		},
		NodeID:          "cond_0",
		WorkflowContext: map[string]interface{}{"status": "inactive"},
	}

	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Output != "false branch" {
		t.Errorf("Output = %q, want %q", result.Output, "false branch")
	}
}

func TestConditionalNodeRunner_NoBranch(t *testing.T) {
	runner := &ConditionalNodeRunner{
		executor: NodeExecutorFunc(func(ctx context.Context, node *Node, nodeID string, wfCtx map[string]interface{}, execCtx *ExecutionContext, ct *CostTracker) (*NodeResult, error) {
			t.Fatal("nodeExecutor should not be called when no branch matches")
			return nil, nil
		}),
	}

	sc := &NodeContext{
		Ctx: context.Background(),
		Node: &Node{
			Type:      NodeTypeConditional,
			Condition: "status contains active",
			// No TrueBranch, no FalseBranch
		},
		NodeID:          "cond_0",
		WorkflowContext: map[string]interface{}{"status": "active"},
	}

	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if result.Output != "" {
		t.Errorf("expected empty output, got %q", result.Output)
	}
}

func TestConditionalNodeRunner_BranchError(t *testing.T) {
	mockExecutor := NodeExecutorFunc(func(ctx context.Context, node *Node, nodeID string, wfCtx map[string]interface{}, execCtx *ExecutionContext, ct *CostTracker) (*NodeResult, error) {
		return nil, fmt.Errorf("branch execution failed")
	})

	runner := &ConditionalNodeRunner{executor: mockExecutor}

	sc := &NodeContext{
		Ctx: context.Background(),
		Node: &Node{
			Type:       NodeTypeConditional,
			Condition:  "status contains active",
			TrueBranch: &Node{Type: NodeTypePrompt},
		},
		NodeID:          "cond_0",
		WorkflowContext: map[string]interface{}{"status": "active"},
	}

	_, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err == nil {
		t.Fatal("expected error from branch execution")
	}
}

// --- ResultNodeRunner tests ---

func TestResultNodeRunner_NodeType(t *testing.T) {
	runner := &ResultNodeRunner{}
	if runner.NodeType() != NodeTypeResult {
		t.Errorf("NodeType() = %q, want %q", runner.NodeType(), NodeTypeResult)
	}
}

func TestResultNodeRunner_SingleInput(t *testing.T) {
	runner := &ResultNodeRunner{
		aggregatorRegistry: NewAggregatorRegistry(),
	}

	sc := &NodeContext{
		Ctx: context.Background(),
		Node: &Node{
			Type: NodeTypeResult,
			Metadata: map[string]interface{}{
				"output_name": "summary",
				"input_ids":   []string{"step1"},
			},
		},
		NodeID:          "output_0",
		WorkflowContext: map[string]interface{}{"step1": "the result"},
	}

	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	if result.Output != "the result" {
		t.Errorf("Output = %q, want %q", result.Output, "the result")
	}
}

func TestResultNodeRunner_MultipleInputsCollect(t *testing.T) {
	runner := &ResultNodeRunner{
		aggregatorRegistry: NewAggregatorRegistry(),
	}

	sc := &NodeContext{
		Ctx: context.Background(),
		Node: &Node{
			Type: NodeTypeResult,
			Metadata: map[string]interface{}{
				"input_ids": []string{"a", "b"},
			},
		},
		NodeID:          "output_0",
		WorkflowContext: map[string]interface{}{"a": "first", "b": "second"},
	}

	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Output != "first\n---\nsecond" {
		t.Errorf("Output = %q, want %q", result.Output, "first\n---\nsecond")
	}
}

func TestResultNodeRunner_NoInputs(t *testing.T) {
	runner := &ResultNodeRunner{
		aggregatorRegistry: NewAggregatorRegistry(),
	}

	sc := &NodeContext{
		Ctx:             context.Background(),
		Node:            &Node{Type: NodeTypeResult},
		NodeID:          "output_0",
		WorkflowContext: map[string]interface{}{},
	}

	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Output != "" {
		t.Errorf("expected empty output, got %q", result.Output)
	}
}

func TestResultNodeRunner_StoresOutputInContext(t *testing.T) {
	runner := &ResultNodeRunner{
		aggregatorRegistry: NewAggregatorRegistry(),
	}

	wfCtx := map[string]interface{}{"step1": "value"}
	sc := &NodeContext{
		Ctx: context.Background(),
		Node: &Node{
			Type:       NodeTypeResult,
			OutputName: "my_output",
			Metadata: map[string]interface{}{
				"input_ids": []string{"step1"},
			},
		},
		NodeID:          "output_0",
		WorkflowContext: wfCtx,
	}

	_, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if val, ok := wfCtx["__output_my_output"]; !ok || val != "value" {
		t.Errorf("expected __output_my_output = %q in workflow context, got %v", "value", val)
	}
}

func TestResultNodeRunner_AggregationTracksCost(t *testing.T) {
	reg := providers.NewRegistry()
	mp := NewMockProviderWithCost("test", 0.001, 0.002)
	mp.SetResponse("impartial judge", "Response B is better.\n\nWINNER: B")
	reg.Register(mp)
	client := NewMockLLMClient(reg)
	runner := &ResultNodeRunner{
		llmClient:          client,
		aggregatorRegistry: NewAggregatorRegistry(),
	}

	tracker := NewCostTracker(&CostLimits{MaxCostUSD: 10.0})
	sc := &NodeContext{
		Ctx: context.Background(),
		Node: &Node{
			Type:              NodeTypeResult,
			AggregationMethod: AggMethodJudge,
			AggregationConfig: map[string]interface{}{
				"judge_model": "mock-model",
			},
			Metadata: map[string]interface{}{
				"input_ids": []string{"a", "b"},
			},
		},
		NodeID:          "output_agg",
		WorkflowContext: map[string]interface{}{"a": "first", "b": "second"},
		CostTracker:     tracker,
	}

	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("expected successful aggregation result, got %+v", result)
	}

	cost, tokens, inputTokens, outputTokens := tracker.GetTotals()
	if cost <= 0 {
		t.Fatalf("expected aggregation cost to be tracked, got %f", cost)
	}
	if tokens <= 0 || inputTokens <= 0 || outputTokens <= 0 {
		t.Fatalf("expected aggregation token usage to be tracked, got total=%d in=%d out=%d", tokens, inputTokens, outputTokens)
	}
}

func TestResultNodeRunner_AggregationCostLimitExceeded(t *testing.T) {
	// Cost limit checking is centralized in the executor (A3).
	// The runner still adds cost, so the tracker accumulates spend.
	reg := providers.NewRegistry()
	mp := NewMockProviderWithCost("test", 0.001, 0.002)
	mp.SetResponse("impartial judge", "Response B is better.\n\nWINNER: B")
	reg.Register(mp)
	client := NewMockLLMClient(reg)
	runner := &ResultNodeRunner{
		llmClient:          client,
		aggregatorRegistry: NewAggregatorRegistry(),
	}

	tracker := NewCostTracker(&CostLimits{MaxCostUSD: 0.0001})
	sc := &NodeContext{
		Ctx: context.Background(),
		Node: &Node{
			Type:              NodeTypeResult,
			AggregationMethod: AggMethodJudge,
			AggregationConfig: map[string]interface{}{
				"judge_model": "mock-model",
			},
			Metadata: map[string]interface{}{
				"input_ids": []string{"a", "b"},
			},
		},
		NodeID:          "output_agg_limit",
		WorkflowContext: map[string]interface{}{"a": "first", "b": "second"},
		CostTracker:     tracker,
	}

	_, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("Execute should succeed (cost checking is in executor): %v", err)
	}
	// After execution, the tracker should have accumulated cost beyond the limit.
	if limErr := tracker.CheckLimits("output_agg_limit"); limErr == nil {
		t.Fatal("expected cost tracker to exceed limit after aggregation")
	}
}

// --- Test helpers ---

type mockRegistryWithProvider struct {
	registry  *providers.Registry
	providers map[string]*MockProvider
}

type capturingProvider struct {
	name            string
	models          []providers.Model
	responseContent string
	finishReason    string
	callErr         error

	mu   sync.Mutex
	last *providers.CompletionRequest
}

func (p *capturingProvider) Name() string { return p.name }

func (p *capturingProvider) Models() []providers.Model { return p.models }

func (p *capturingProvider) Complete(_ context.Context, req *providers.CompletionRequest) (*providers.CompletionResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.last = req
	if p.callErr != nil {
		return nil, p.callErr
	}
	content := p.responseContent
	if content == "" {
		content = "ok"
	}
	return &providers.CompletionResponse{
		ID:      "capture-response",
		Model:   req.Model,
		Content: content,
		Usage: providers.Usage{
			PromptTokens:     1,
			CompletionTokens: 1,
			TotalTokens:      2,
		},
		Finish: p.finishReason,
	}, nil
}

func (p *capturingProvider) EstimateTokens(text string) int { return len(text) / 4 }

func (p *capturingProvider) Cost(_ string, _ int, _ int) float64 { return 0 }

func (p *capturingProvider) lastRequest() *providers.CompletionRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.last
}

func newMockRegistry() *mockRegistryWithProvider {
	reg := providers.NewRegistry()
	mp := NewMockProvider("test")
	reg.Register(mp)
	return &mockRegistryWithProvider{
		registry:  reg,
		providers: map[string]*MockProvider{"test": mp},
	}
}

func newMockRegistryWithCost(inputCost, outputCost float64) *providers.Registry {
	reg := providers.NewRegistry()
	mp := NewMockProviderWithCost("test", inputCost, outputCost)
	reg.Register(mp)
	return reg
}
