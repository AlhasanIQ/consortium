package workflow

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/alhasaniq/consortium/pkg/providers"
)

func TestResultNodeRunner_ExecutesEveryAggregationMethod(t *testing.T) {
	tests := []struct {
		name       string
		method     AggregationMethod
		inputs     map[string]string
		models     map[string]string
		config     map[string]interface{}
		mock       func(*MockProvider)
		wantOutput string
		wantWinner string
	}{
		{
			name:       "collect honors configured separator",
			method:     AggMethodCollect,
			inputs:     map[string]string{"agent-a": "first", "agent-b": "second"},
			config:     map[string]interface{}{"separator": " | "},
			wantOutput: "first | second",
		},
		{
			name:   "majority_vote selects most common extracted answer",
			method: AggMethodMajorityVote,
			inputs: map[string]string{
				"agent-a": "Answer: B\nReasoning from A.",
				"agent-b": "Answer: B\nReasoning from B.",
				"agent-c": "Answer: A\nReasoning from C.",
			},
			config: map[string]interface{}{
				"extraction_strategy": "regex",
				"extraction_pattern":  `Answer:\s*([A-D])`,
				"tie_breaker_method":  "first",
			},
			wantOutput: "Answer: B\nReasoning from A.",
			wantWinner: "agent-a",
		},
		{
			name:   "debate_decide groups camps and applies judge decision",
			method: AggMethodDebateDecide,
			inputs: map[string]string{
				"agent-a": "Answer: A\nArgument from A.",
				"agent-b": "Answer: B\nArgument from B.",
			},
			config: map[string]interface{}{
				"judge_model":                "mock-model",
				"system_prompt":              "You are an impartial judge evaluating competing camps.",
				"prompt":                     "Original Question: {{prompt}}\n\n{{camps}}\n\nPick the winning answer.",
				"extraction_strategy":        "regex",
				"extraction_pattern":         `Answer:\s*([A-D])`,
				"temperature":                0.0,
				"max_tokens":                 256,
				"repair_max_tokens":          64,
				"subcall_retry_max_attempts": 1,
			},
			mock: func(provider *MockProvider) {
				provider.SetResponse("competing camps", "Camp B is more rigorous.\n\nWINNER: B")
			},
			wantOutput: "Answer: B\nArgument from B.",
			wantWinner: "agent-b",
		},
		{
			name:   "judge anonymizes responses and selects winner",
			method: AggMethodJudge,
			inputs: map[string]string{
				"agent-a": "Weak answer.",
				"agent-b": "Strong answer.",
			},
			config: map[string]interface{}{
				"judge_model":                "mock-model",
				"system_prompt":              "You are an impartial judge evaluating responses.",
				"prompt":                     "Original Question: {{prompt}}\n\n{{responses}}\n\nPick the best response.",
				"temperature":                0.0,
				"max_tokens":                 256,
				"repair_max_tokens":          64,
				"subcall_retry_max_attempts": 1,
			},
			mock: func(provider *MockProvider) {
				provider.SetResponse("Pick the best response", "Response B is better.\n\nWINNER: B")
			},
			wantOutput: "Strong answer.",
			wantWinner: "agent-b",
		},
		{
			name:   "scoring scores each response and selects highest weighted score",
			method: AggMethodScoring,
			inputs: map[string]string{
				"agent-a": "Response 1: incomplete.",
				"agent-b": "Response 2: complete and clear.",
			},
			config: map[string]interface{}{
				"scoring_model":              "mock-model",
				"system_prompt":              "You score responses against the rubric.",
				"prompt":                     "Original Question: {{prompt}}\n\nResponse to evaluate:\n{{response}}\n\nScoring Rubric:\n{{rubric}}",
				"temperature":                0.0,
				"max_tokens":                 256,
				"subcall_retry_max_attempts": 1,
				"rubric": []interface{}{
					map[string]interface{}{"name": "Correctness", "weight": 0.7, "description": "Correct answer"},
					map[string]interface{}{"name": "Clarity", "weight": 0.3, "description": "Clear answer"},
				},
			},
			mock: func(provider *MockProvider) {
				provider.SetResponse("Response 1", `{"scores": {"correctness": 4, "clarity": 5}}`)
				provider.SetResponse("Response 2", `{"scores": {"correctness": 9, "clarity": 8}}`)
			},
			wantOutput: "Response 2: complete and clear.",
			wantWinner: "agent-b",
		},
		{
			name:   "synthesis returns synthesized answer",
			method: AggMethodSynthesis,
			inputs: map[string]string{
				"agent-a": "Partial answer.",
				"agent-b": "Complementary answer.",
			},
			config: map[string]interface{}{
				"model":         "mock-model",
				"system_prompt": "You synthesize responses.",
				"prompt":        "Original Question: {{prompt}}\n\n{{responses}}\n\nSynthesize.",
				"temperature":   0.0,
				"max_tokens":    256,
			},
			mock: func(provider *MockProvider) {
				provider.SetResponse("Synthesize", "Synthesized answer.")
			},
			wantOutput: "Synthesized answer.",
		},
		{
			name:   "peer_matrix uses upstream reviewer models",
			method: AggMethodPeerMatrix,
			inputs: map[string]string{
				"agent-a": "Candidate A.",
				"agent-b": "Candidate B.",
			},
			models: map[string]string{
				"agent-a": "mock-model",
				"agent-b": "mock-model",
			},
			config: map[string]interface{}{
				"rubric_mode":        "static",
				"eval_system_prompt": "You are a peer evaluator.",
				"eval_prompt":        "Question: {{question}}\n\nResponse to evaluate:\n{{response}}\n\n{{rubric}}",
				"normalization":      "none",
				"temperature":        0.0,
				"max_tokens":         256,
				"max_parallel":       1,
				"rubric": []interface{}{
					map[string]interface{}{"name": "Correctness", "weight": 0.7, "description": "Correct answer"},
					map[string]interface{}{"name": "Clarity", "weight": 0.3, "description": "Clear answer"},
				},
			},
			mock: func(provider *MockProvider) {
				provider.SetResponse("Candidate A", `{"correctness": {"reasoning": "good", "score": 8}, "clarity": {"reasoning": "ok", "score": 7}}`)
				provider.SetResponse("Candidate B", `{"correctness": {"reasoning": "best", "score": 9}, "clarity": {"reasoning": "clear", "score": 9}}`)
			},
			wantOutput: "Candidate B.",
			wantWinner: "agent-b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := NewMockProvider("test")
			if tt.mock != nil {
				tt.mock(provider)
			}
			registry := providers.NewRegistry()
			registry.Register(provider)

			result, err := executeAggregationResultNode(t, tt.method, tt.inputs, tt.models, tt.config, NewMockLLMClient(registry))
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}
			if !result.Success {
				t.Fatalf("expected success, got error %q", result.Error)
			}
			if result.Output != tt.wantOutput {
				t.Fatalf("Output = %q, want %q", result.Output, tt.wantOutput)
			}
			if tt.wantWinner != "" && result.Metadata["aggregation_winner"] != tt.wantWinner {
				t.Fatalf("aggregation_winner = %v, want %q", result.Metadata["aggregation_winner"], tt.wantWinner)
			}
			if result.Metadata["aggregation_method"] != string(tt.method) {
				t.Fatalf("aggregation_method = %v, want %q", result.Metadata["aggregation_method"], tt.method)
			}
			if tt.method == AggMethodCollect {
				if _, exists := result.Metadata["legacy_result_aggregation"]; exists {
					t.Fatalf("collect result aggregation should not be marked legacy hidden path: %+v", result.Metadata)
				}
			} else if result.Metadata["legacy_result_aggregation"] != true {
				t.Fatalf("legacy_result_aggregation = %#v, want true for %s metadata=%+v", result.Metadata["legacy_result_aggregation"], tt.method, result.Metadata)
			}
		})
	}
}

func TestResultNodeRunner_LegacyMarkerDoesNotMutateNodeDefinition(t *testing.T) {
	provider := NewMockProvider("test")
	provider.SetResponse("Pick the best response", "Response B is better.\n\nWINNER: B")
	registry := providers.NewRegistry()
	registry.Register(provider)

	inputIDs := []interface{}{"agent-a", "agent-b"}
	node := &Node{
		ID:                "result1",
		Type:              NodeTypeResult,
		OutputName:        "final_output",
		AggregationMethod: AggMethodJudge,
		AggregationConfig: map[string]interface{}{
			"judge_model":                "mock-model",
			"prompt":                     "Original Question: {{prompt}}\n\n{{responses}}\n\nPick the best response.",
			"repair_max_tokens":          64,
			"subcall_retry_max_attempts": 1,
		},
		Metadata: map[string]interface{}{
			"input_ids": inputIDs,
		},
	}
	wf := &Workflow{ID: "legacy-runtime-marker", Name: "Legacy Runtime Marker", Nodes: []*Node{node}}

	runner := &ResultNodeRunner{
		llmClient:          NewMockLLMClient(registry),
		aggregatorRegistry: NewAggregatorRegistry(),
	}
	sc := withStrictNodeContextDefaults(&NodeContext{
		Ctx:    context.Background(),
		Node:   node,
		NodeID: "result1",
		WorkflowContext: map[string]interface{}{
			"user_prompt": "Which candidate is best?",
			"agent-a":     "Weak answer.",
			"agent-b":     "Strong answer.",
		},
		CostTracker: NewCostTracker(&CostLimits{MaxCostUSD: 10.0}),
	})
	beforeHash := ComputeConfigHash(wf)

	result, err := runner.Execute(sc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Metadata["legacy_result_aggregation"] != true {
		t.Fatalf("runtime metadata missing legacy marker: %+v", result.Metadata)
	}
	if node.Metadata["legacy_result_aggregation"] == true {
		t.Fatalf("node definition metadata was mutated: %+v", node.Metadata)
	}
	if afterHash := ComputeConfigHash(wf); afterHash != beforeHash {
		t.Fatalf("config hash changed after runtime legacy marker: before=%s after=%s", beforeHash, afterHash)
	}
}

func TestResultNodeRunner_FailedLegacyAggregationIncludesRuntimeMarker(t *testing.T) {
	registry := providers.NewRegistry()
	registry.Register(NewFailingMockProvider("test", 10, errors.New("provider unavailable")))

	result, err := executeAggregationResultNode(
		t,
		AggMethodSynthesis,
		map[string]string{
			"agent-a": "First answer.",
			"agent-b": "Second answer.",
		},
		nil,
		map[string]interface{}{
			"model":        "mock-model",
			"prompt":       "{{responses}}\n\nSynthesize.",
			"temperature":  0.0,
			"max_tokens":   256,
			"retry_policy": map[string]interface{}{"max_attempts": 1},
		},
		NewMockLLMClient(registry),
	)
	if err == nil {
		t.Fatalf("Execute succeeded, want aggregation error")
	}
	if result == nil {
		t.Fatalf("result is nil, want error result with metadata")
	}
	if result.Success {
		t.Fatalf("result.Success = true, want false")
	}
	if result.Metadata["aggregation_method"] != string(AggMethodSynthesis) {
		t.Fatalf("aggregation_method = %v, want %q", result.Metadata["aggregation_method"], AggMethodSynthesis)
	}
	if result.Metadata["legacy_result_aggregation"] != true {
		t.Fatalf("legacy_result_aggregation = %#v, want true metadata=%+v", result.Metadata["legacy_result_aggregation"], result.Metadata)
	}
}

func executeAggregationResultNode(
	t *testing.T,
	method AggregationMethod,
	inputs map[string]string,
	models map[string]string,
	config map[string]interface{},
	llmClient *providers.Client,
) (*NodeResult, error) {
	t.Helper()

	inputIDs := make([]interface{}, 0, len(inputs))
	workflowCtx := map[string]interface{}{
		"user_prompt": "Which candidate is best?",
	}
	agentIDs := make([]string, 0, len(inputs))
	for agentID := range inputs {
		agentIDs = append(agentIDs, agentID)
	}
	sort.Strings(agentIDs)
	for _, agentID := range agentIDs {
		output := inputs[agentID]
		inputIDs = append(inputIDs, agentID)
		workflowCtx[agentID] = output
		if model := strings.TrimSpace(models[agentID]); model != "" {
			workflowCtx[agentID+"__model"] = model
		}
	}

	runner := &ResultNodeRunner{
		llmClient:          llmClient,
		aggregatorRegistry: NewAggregatorRegistry(),
	}
	sc := &NodeContext{
		Ctx: context.Background(),
		Node: &Node{
			ID:                "result1",
			Type:              NodeTypeResult,
			OutputName:        "final_output",
			AggregationMethod: method,
			AggregationConfig: config,
			Metadata: map[string]interface{}{
				"input_ids": inputIDs,
			},
		},
		NodeID:          "result1",
		WorkflowContext: workflowCtx,
		CostTracker:     NewCostTracker(&CostLimits{MaxCostUSD: 10.0}),
	}
	return runner.Execute(withStrictNodeContextDefaults(sc))
}
