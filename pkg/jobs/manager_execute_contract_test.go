package jobs

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/alhasaniq/consortium/pkg/bench"
	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/workflow"
)

func TestExecuteWorkflow_PreservesEmptyNamedResultOutput(t *testing.T) {
	provider := &mockProvider{
		name:   "mock",
		models: []providers.Model{defaultMockModel()},
		completeFunc: func(_ context.Context, req *providers.CompletionRequest) (*providers.CompletionResponse, error) {
			userPrompt := ""
			if len(req.Messages) > 0 {
				userPrompt = req.Messages[len(req.Messages)-1].Content
			}

			content := "CHILD OUTPUT"
			completionTokens := 32
			if strings.Contains(userPrompt, "contract-step") {
				content = ""
				completionTokens = 512
			}

			return &providers.CompletionResponse{
				Content: content,
				Finish:  "stop",
				Usage: providers.Usage{
					PromptTokens:     120,
					CompletionTokens: completionTokens,
					TotalTokens:      120 + completionTokens,
				},
			}, nil
		},
	}

	manager, _ := setupManagerWithProvider(t, provider)
	startWorkers(t, manager)

	wf := &workflow.Workflow{
		ID:   "wf-empty-contract-output",
		Name: "Empty Contract Output",
		Nodes: []*workflow.Node{
			strictPromptNode("child", "mock-model", "child-step"),
			strictPromptNode("contract", "mock-model", "contract-step"),
			strictResultNode("result", []string{"contract"}, "benchmark_answer"),
		},
		Edges: []*workflow.Edge{
			{ID: "edge-child-contract", Source: "child", Target: "contract"},
			{ID: "edge-contract-result", Source: "contract", Target: "result"},
		},
	}

	execResult, err := manager.ExecuteWorkflow(context.Background(), wf)
	if err != nil {
		t.Fatalf("ExecuteWorkflow returned error: %v", err)
	}
	if !execResult.Success {
		t.Fatalf("expected successful execution, got error=%q", execResult.Error)
	}
	if execResult.Result == nil {
		t.Fatal("expected workflow result")
	}
	if execResult.Result.FinalOutput != "CHILD OUTPUT" {
		t.Fatalf("expected final_output to remain child output, got %q", execResult.Result.FinalOutput)
	}

	value, exists := execResult.Result.Outputs["benchmark_answer"]
	if !exists {
		t.Fatalf("expected outputs to include benchmark_answer key, got keys=%v", keys(execResult.Result.Outputs))
	}
	if got, _ := value.(string); got != "" {
		t.Fatalf("expected benchmark_answer to be empty string, got %#v", value)
	}

	canonical, source, present := bench.ExtractCanonicalBenchmarkAnswer(execResult.Result.Outputs, execResult.Result.FinalOutput)
	if canonical != "" {
		t.Fatalf("expected canonical output to be empty, got %q", canonical)
	}
	if source != bench.OutputSourceBenchmarkAnswer {
		t.Fatalf("expected source=%q, got %q", bench.OutputSourceBenchmarkAnswer, source)
	}
	if !present {
		t.Fatal("expected canonical benchmark answer key to be present")
	}
}

func TestExecuteWorkflow_PersistsProviderTelemetryMetadata(t *testing.T) {
	byok := true
	provider := &mockProvider{
		name:   "mock",
		models: []providers.Model{defaultMockModel()},
		response: &providers.CompletionResponse{
			ID:           "resp-provider-1",
			RequestID:    "req-provider-1",
			GenerationID: "gen-provider-1",
			Model:        "mock-model",
			Content:      "provider output",
			Finish:       "stop",
			ServiceTier:  "default",
			Usage: providers.Usage{
				PromptTokens:     12,
				CompletionTokens: 7,
				TotalTokens:      19,
				PromptTokensDetails: &providers.PromptTokensDetails{
					CachedTokens:     5,
					CacheWriteTokens: 2,
				},
				CompletionTokensDetails: &providers.CompletionTokensDetails{
					ReasoningTokens: 3,
				},
				IsBYOK: &byok,
			},
		},
	}

	manager, store := setupManagerWithProvider(t, provider)
	startWorkers(t, manager)

	wf := &workflow.Workflow{
		ID:   "wf-provider-telemetry",
		Name: "Provider Telemetry",
		Nodes: []*workflow.Node{
			strictPromptNode("call", "mock-model", "call provider"),
			strictResultNode("result", []string{"call"}, "final"),
		},
		Edges: []*workflow.Edge{
			{ID: "edge-call-result", Source: "call", Target: "result"},
		},
	}

	execResult, err := manager.ExecuteWorkflow(context.Background(), wf)
	if err != nil {
		t.Fatalf("ExecuteWorkflow returned error: %v", err)
	}
	if !execResult.Success {
		t.Fatalf("execution failed: %s", execResult.Error)
	}

	nodes, err := store.GetWorkflowNodes(execResult.JobID)
	if err != nil {
		t.Fatalf("GetWorkflowNodes: %v", err)
	}
	var promptMeta map[string]interface{}
	for _, node := range nodes {
		if node.NodeID != "call" {
			continue
		}
		if err := json.Unmarshal([]byte(node.Metadata), &promptMeta); err != nil {
			t.Fatalf("decode prompt metadata: %v metadata=%s", err, node.Metadata)
		}
		break
	}
	if promptMeta == nil {
		t.Fatalf("prompt node metadata not found in nodes: %+v", nodes)
	}
	if promptMeta["provider_generation_id"] != "gen-provider-1" {
		t.Fatalf("metadata = %#v", promptMeta)
	}
	if promptMeta["openrouter_cached_tokens"] != float64(5) || promptMeta["openrouter_reasoning_tokens"] != float64(3) {
		t.Fatalf("OpenRouter token metadata = %#v", promptMeta)
	}
	if promptMeta["openrouter_service_tier"] != "default" || promptMeta["openrouter_is_byok"] != true {
		t.Fatalf("OpenRouter scalar metadata = %#v", promptMeta)
	}
}

func keys(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
