package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/alhasaniq/consortium/pkg/seeds"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/alhasaniq/consortium/pkg/workflow/compiler"
)

// agentNode builds a minimal workflow-file agent node map.
func agentNode(id, model string, maxTokens int) map[string]interface{} {
	return map[string]interface{}{
		"id":   id,
		"type": "agent",
		"data": map[string]interface{}{
			"type":  "agent",
			"label": id,
			"config": map[string]interface{}{
				"name":      id,
				"model":     model,
				"maxTokens": maxTokens,
			},
		},
	}
}

// resultNode builds a minimal workflow-file result node map.
func resultNode(id string) map[string]interface{} {
	return map[string]interface{}{
		"id":   id,
		"type": "result",
		"data": map[string]interface{}{
			"type":  "result",
			"label": id,
			"config": map[string]interface{}{
				"name":              id,
				"outputFormat":      "text",
				"aggregationMethod": "collect",
			},
		},
	}
}

// aggregationNode builds a workflow-file aggregation macro node map.
func aggregationNode(id, method string, config map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"id":   id,
		"type": "aggregation",
		"data": map[string]interface{}{
			"type":  "aggregation",
			"label": id,
			"config": map[string]interface{}{
				"name":              id,
				"aggregationMethod": method,
				"aggregationConfig": config,
			},
		},
	}
}

// edge builds a workflow-file edge map.
func edge(id, source, target string) map[string]interface{} {
	return map[string]interface{}{
		"id":     id,
		"source": source,
		"target": target,
	}
}

func TestWorkflowFromWorkflowFile_NilInput(t *testing.T) {
	_, err := workflowFromWorkflowFile(nil, nil)
	if err == nil {
		t.Fatal("expected error for nil workflow_file, got nil")
	}
}

func TestWorkflowFromWorkflowFile_AllEmbeddedSeedsConvertAndValidate(t *testing.T) {
	resolver := compiler.StaticResolver{}
	for _, definition := range seeds.GetAllDefinitions() {
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(definition.JSON), &raw); err != nil {
			t.Fatalf("%s: seed JSON should already be valid: %v", definition.ID, err)
		}
		wf, err := workflowFromWorkflowFile(raw, map[string]interface{}{"user_prompt": "What is 2 + 2?"})
		if err != nil {
			t.Fatalf("%s: workflowFromWorkflowFile for resolver: %v", definition.ID, err)
		}
		resolver[definition.ID] = *wf
	}

	validator := workflow.NewValidator(nil)
	for _, definition := range seeds.GetAllDefinitions() {
		t.Run(definition.ID, func(t *testing.T) {
			var raw map[string]interface{}
			if err := json.Unmarshal([]byte(definition.JSON), &raw); err != nil {
				t.Fatalf("seed JSON should already be valid: %v", err)
			}
			wf, err := workflowFromWorkflowFile(raw, map[string]interface{}{"user_prompt": "What is 2 + 2?"})
			if err != nil {
				t.Fatalf("workflowFromWorkflowFile: %v", err)
			}
			compiled, _, err := compiler.Compile(context.Background(), wf, resolver)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			result := validator.Validate(compiled)
			if !result.Valid {
				var messages []string
				for _, validationErr := range result.Errors {
					messages = append(messages, validationErr.NodeID+":"+validationErr.Field+":"+validationErr.Message)
				}
				t.Fatalf("converted seed workflow should validate: %s", strings.Join(messages, "; "))
			}
		})
	}
}

func TestWorkflowFromWorkflowFile_SimpleAgentResult(t *testing.T) {
	raw := map[string]interface{}{
		"id":   "wf-simple",
		"name": "Simple Workflow",
		"nodes": []interface{}{
			agentNode("agent-1", "openai/gpt-4o-mini", 256),
			resultNode("result-1"),
		},
		"edges": []interface{}{
			edge("e1", "agent-1", "result-1"),
		},
	}

	wf, err := workflowFromWorkflowFile(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if wf.ID != "wf-simple" {
		t.Errorf("ID = %q, want %q", wf.ID, "wf-simple")
	}
	if wf.Name != "Simple Workflow" {
		t.Errorf("Name = %q, want %q", wf.Name, "Simple Workflow")
	}
	// input nodes are skipped, result nodes are included, so 2 nodes total
	if len(wf.Nodes) != 2 {
		t.Errorf("len(Nodes) = %d, want 2", len(wf.Nodes))
	}
	if len(wf.Edges) != 1 {
		t.Errorf("len(Edges) = %d, want 1", len(wf.Edges))
	}
}

func TestWorkflowFromWorkflowFile_InputNodeIsSkipped(t *testing.T) {
	raw := map[string]interface{}{
		"id":   "wf-input",
		"name": "With Input Node",
		"nodes": []interface{}{
			map[string]interface{}{
				"id":   "input-1",
				"type": "input",
				"data": map[string]interface{}{
					"type":  "input",
					"label": "Input",
					"config": map[string]interface{}{
						"name": "Input",
					},
				},
			},
			agentNode("agent-1", "openai/gpt-4o-mini", 512),
			resultNode("result-1"),
		},
		"edges": []interface{}{
			edge("e1", "input-1", "agent-1"),
			edge("e2", "agent-1", "result-1"),
		},
	}

	inputs := map[string]interface{}{"user_prompt": "Hello"}
	wf, err := workflowFromWorkflowFile(raw, inputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only agent-1 and result-1 appear (input-1 is skipped)
	if len(wf.Nodes) != 2 {
		t.Errorf("len(Nodes) = %d, want 2 (input node should be skipped)", len(wf.Nodes))
	}
	// Context should contain input values when an input node is present
	if wf.Context["user_prompt"] != "Hello" {
		t.Errorf("Context[user_prompt] = %v, want %q", wf.Context["user_prompt"], "Hello")
	}
	// Edge from input node should be excluded (input-1 not in includedNodeIDs)
	for _, e := range wf.Edges {
		if e.Source == "input-1" || e.Target == "input-1" {
			t.Errorf("edge involving input-1 should have been removed, got %+v", *e)
		}
	}
}

func TestWorkflowFromWorkflowFile_AgentMissingModel(t *testing.T) {
	raw := map[string]interface{}{
		"id":   "wf-bad",
		"name": "Bad Workflow",
		"nodes": []interface{}{
			map[string]interface{}{
				"id":   "agent-1",
				"type": "agent",
				"data": map[string]interface{}{
					"type":  "agent",
					"label": "Agent",
					"config": map[string]interface{}{
						"name":      "Agent 1",
						"model":     "", // missing model
						"maxTokens": 256,
					},
				},
			},
		},
		"edges": []interface{}{},
	}

	_, err := workflowFromWorkflowFile(raw, nil)
	if err == nil {
		t.Fatal("expected error for missing model, got nil")
	}
}

func TestWorkflowFromWorkflowFile_AgentMissingMaxTokens(t *testing.T) {
	raw := map[string]interface{}{
		"id":   "wf-bad",
		"name": "Bad Workflow",
		"nodes": []interface{}{
			map[string]interface{}{
				"id":   "agent-1",
				"type": "agent",
				"data": map[string]interface{}{
					"type":  "agent",
					"label": "Agent",
					"config": map[string]interface{}{
						"name":  "Agent 1",
						"model": "openai/gpt-4o-mini",
						// maxTokens omitted
					},
				},
			},
		},
		"edges": []interface{}{},
	}

	_, err := workflowFromWorkflowFile(raw, nil)
	if err == nil {
		t.Fatal("expected error for missing maxTokens, got nil")
	}
}

func TestWorkflowFromWorkflowFile_UnsupportedNodeType(t *testing.T) {
	raw := map[string]interface{}{
		"id":   "wf-unsupported",
		"name": "Unsupported",
		"nodes": []interface{}{
			map[string]interface{}{
				"id":   "mystery-1",
				"type": "mystery_type",
				"data": map[string]interface{}{
					"type":   "mystery_type",
					"label":  "Mystery",
					"config": map[string]interface{}{},
				},
			},
		},
		"edges": []interface{}{},
	}

	_, err := workflowFromWorkflowFile(raw, nil)
	if err == nil {
		t.Fatal("expected error for unsupported node type, got nil")
	}
}

func TestWorkflowFromWorkflowFile_DefaultWorkflowName(t *testing.T) {
	raw := map[string]interface{}{
		// no "name" field
		"id": "wf-noname",
		"nodes": []interface{}{
			agentNode("agent-1", "openai/gpt-4o-mini", 128),
			resultNode("result-1"),
		},
		"edges": []interface{}{
			edge("e1", "agent-1", "result-1"),
		},
	}

	wf, err := workflowFromWorkflowFile(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wf.Name != "Untitled Workflow" {
		t.Errorf("Name = %q, want %q", wf.Name, "Untitled Workflow")
	}
}

func TestWorkflowFromWorkflowFile_InputValuesIgnoredWithoutInputNode(t *testing.T) {
	// When there is no input node in the workflow, inputValues should NOT
	// be injected into the workflow context.
	raw := map[string]interface{}{
		"id":   "wf-no-input-node",
		"name": "No Input Node",
		"nodes": []interface{}{
			agentNode("agent-1", "openai/gpt-4o-mini", 256),
			resultNode("result-1"),
		},
		"edges": []interface{}{
			edge("e1", "agent-1", "result-1"),
		},
	}

	inputs := map[string]interface{}{"secret": "value"}
	wf, err := workflowFromWorkflowFile(raw, inputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := wf.Context["secret"]; ok {
		t.Error("expected input values to be ignored when no input node exists")
	}
}

func TestWorkflowFromWorkflowFile_EdgeWithEmptySourceOrTarget(t *testing.T) {
	// Edges with empty source/target should be silently dropped.
	raw := map[string]interface{}{
		"id":   "wf-bad-edge",
		"name": "Bad Edge",
		"nodes": []interface{}{
			agentNode("agent-1", "openai/gpt-4o-mini", 256),
			resultNode("result-1"),
		},
		"edges": []interface{}{
			edge("e1", "agent-1", "result-1"),
			map[string]interface{}{"id": "e-bad1", "source": "", "target": "result-1"},
			map[string]interface{}{"id": "e-bad2", "source": "agent-1", "target": ""},
		},
	}

	wf, err := workflowFromWorkflowFile(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(wf.Edges) != 1 {
		t.Errorf("len(Edges) = %d, want 1 (bad edges should be dropped)", len(wf.Edges))
	}
}

func TestWorkflowFromWorkflowFile_FrontendRetryFields(t *testing.T) {
	raw := map[string]interface{}{
		"id":   "wf-frontend-retry",
		"name": "Frontend Retry",
		"nodes": []interface{}{
			map[string]interface{}{
				"id":   "agent-run-1",
				"type": "agent_run",
				"data": map[string]interface{}{
					"type":  "agent_run",
					"label": "Novomo Agent",
					"config": map[string]interface{}{
						"name":                   "Agent",
						"prompt":                 "Return exactly LIVE_OK.",
						"harness":                "claude-code",
						"timeoutSeconds":         30,
						"retryMaxAttempts":       1,
						"retryBackoffMs":         0,
						"retryBackoffMultiplier": 1.0,
						"retryMaxBackoffMs":      0,
					},
				},
			},
			map[string]interface{}{
				"id":   "result-1",
				"type": "result",
				"data": map[string]interface{}{
					"type":  "result",
					"label": "Result",
					"config": map[string]interface{}{
						"name":                   "final",
						"outputFormat":           "text",
						"aggregationMethod":      "collect",
						"retryMaxAttempts":       1,
						"retryBackoffMs":         0,
						"retryBackoffMultiplier": 1.0,
						"retryMaxBackoffMs":      0,
					},
				},
			},
		},
		"edges": []interface{}{
			edge("e1", "agent-run-1", "result-1"),
		},
	}

	wf, err := workflowFromWorkflowFile(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, node := range wf.Nodes {
		if node.RetryPolicy == nil {
			t.Fatalf("node %s retry policy is nil", node.ID)
		}
		if node.RetryPolicy.MaxAttempts != 1 || node.RetryPolicy.BackoffMs != 0 ||
			node.RetryPolicy.BackoffMultiply != 1.0 || node.RetryPolicy.MaxBackoffMs != 0 {
			t.Fatalf("node %s retry policy = %+v", node.ID, node.RetryPolicy)
		}
	}
}

func TestWorkflowFromWorkflowFile_NovoRunDefaultsSandboxToDocker(t *testing.T) {
	raw := map[string]interface{}{
		"id":   "wf-novo-default-sandbox",
		"name": "Novo Default Sandbox",
		"nodes": []interface{}{
			map[string]interface{}{
				"id":   "superagent-1",
				"type": "novo_run",
				"data": map[string]interface{}{
					"type":  "novo_run",
					"label": "Superagent",
					"config": map[string]interface{}{
						"name":           "Superagent",
						"prompt":         "Investigate",
						"timeoutSeconds": 30,
					},
				},
			},
		},
		"edges": []interface{}{},
	}

	wf, err := workflowFromWorkflowFile(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(wf.Nodes) != 1 {
		t.Fatalf("len(Nodes) = %d, want 1", len(wf.Nodes))
	}
	if wf.Nodes[0].Sandbox != "docker" {
		t.Fatalf("Sandbox = %q, want docker", wf.Nodes[0].Sandbox)
	}
}

func TestWorkflowFromWorkflowFile_ChildWorkflowNode(t *testing.T) {
	raw := map[string]interface{}{
		"id":   "wf-child",
		"name": "Child Workflow Test",
		"nodes": []interface{}{
			map[string]interface{}{
				"id":   "child-1",
				"type": "child_workflow",
				"data": map[string]interface{}{
					"type":  "child_workflow",
					"label": "Child",
					"config": map[string]interface{}{
						"name":            "Child Step",
						"childWorkflowId": "nested-wf",
						"outputKey":       "child_result",
					},
				},
			},
			resultNode("result-1"),
		},
		"edges": []interface{}{
			edge("e1", "child-1", "result-1"),
		},
	}

	wf, err := workflowFromWorkflowFile(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(wf.Nodes) != 2 {
		t.Errorf("len(Nodes) = %d, want 2", len(wf.Nodes))
	}
	childNode := wf.Nodes[0]
	if childNode.ChildWorkflowID != "nested-wf" {
		t.Errorf("ChildWorkflowID = %q, want %q", childNode.ChildWorkflowID, "nested-wf")
	}
	if childNode.ChildOutputKey != "child_result" {
		t.Errorf("ChildOutputKey = %q, want %q", childNode.ChildOutputKey, "child_result")
	}
	// Default await should be true when not specified
	if !childNode.ChildAwait {
		t.Error("ChildAwait should default to true")
	}
}

func TestWorkflowFromWorkflowFile_TemperatureSetOnNode(t *testing.T) {
	temp := 0.7
	raw := map[string]interface{}{
		"id":   "wf-temp",
		"name": "Temp Test",
		"nodes": []interface{}{
			map[string]interface{}{
				"id":   "agent-1",
				"type": "agent",
				"data": map[string]interface{}{
					"type":  "agent",
					"label": "Agent",
					"config": map[string]interface{}{
						"name":        "Agent 1",
						"model":       "openai/gpt-4o-mini",
						"maxTokens":   256,
						"temperature": temp,
					},
				},
			},
			resultNode("result-1"),
		},
		"edges": []interface{}{
			edge("e1", "agent-1", "result-1"),
		},
	}

	wf, err := workflowFromWorkflowFile(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(wf.Nodes) == 0 {
		t.Fatal("expected at least one node")
	}
	agentN := wf.Nodes[0]
	if agentN.Temperature == nil {
		t.Fatal("expected Temperature to be set on agent node")
	}
	if *agentN.Temperature != temp {
		t.Errorf("Temperature = %v, want %v", *agentN.Temperature, temp)
	}
}

func TestWorkflowFromWorkflowFile_ResultNodeAggregation(t *testing.T) {
	raw := map[string]interface{}{
		"id":   "wf-agg",
		"name": "Aggregation Test",
		"nodes": []interface{}{
			agentNode("agent-1", "openai/gpt-4o-mini", 256),
			agentNode("agent-2", "openai/gpt-4o-mini", 256),
			map[string]interface{}{
				"id":   "result-1",
				"type": "result",
				"data": map[string]interface{}{
					"type":  "result",
					"label": "Result",
					"config": map[string]interface{}{
						"name":              "final",
						"outputFormat":      "json",
						"aggregationMethod": "majority_vote",
					},
				},
			},
		},
		"edges": []interface{}{
			edge("e1", "agent-1", "result-1"),
			edge("e2", "agent-2", "result-1"),
		},
	}

	wf, err := workflowFromWorkflowFile(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find result node
	var resultN *struct{ method, format, name string }
	for _, n := range wf.Nodes {
		if n.ID == "result-1" {
			resultN = &struct{ method, format, name string }{
				method: string(n.AggregationMethod),
				format: n.OutputFormat,
				name:   n.OutputName,
			}
			break
		}
	}
	if resultN == nil {
		t.Fatal("result node not found in output")
	}
	if resultN.method != "majority_vote" {
		t.Errorf("AggregationMethod = %q, want %q", resultN.method, "majority_vote")
	}
	if resultN.format != "json" {
		t.Errorf("OutputFormat = %q, want %q", resultN.format, "json")
	}
	if resultN.name != "final" {
		t.Errorf("OutputName = %q, want %q", resultN.name, "final")
	}
}

func TestWorkflowFromWorkflowFile_AggregationMacroNode(t *testing.T) {
	raw := map[string]interface{}{
		"id":   "wf-agg-macro",
		"name": "Aggregation Macro Test",
		"nodes": []interface{}{
			agentNode("agent-1", "openai/gpt-4o-mini", 256),
			agentNode("agent-2", "anthropic/claude-sonnet-4.5", 256),
			aggregationNode("agg-judge", "judge", map[string]interface{}{
				"judge_model":   "openai/gpt-5-mini",
				"system_prompt": "Judge.",
				"prompt":        "{{responses}}",
				"temperature":   float64(0),
			}),
			map[string]interface{}{
				"id":   "final-result",
				"type": "result",
				"data": map[string]interface{}{
					"type":  "result",
					"label": "Final",
					"config": map[string]interface{}{
						"name":         "final_answer",
						"outputFormat": "text",
					},
				},
			},
		},
		"edges": []interface{}{
			edge("e1", "agent-1", "agg-judge"),
			edge("e2", "agent-2", "agg-judge"),
			edge("e3", "agg-judge", "final-result"),
		},
	}

	wf, err := workflowFromWorkflowFile(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var aggNodeIndex = -1
	for i, n := range wf.Nodes {
		if n.ID == "agg-judge" {
			aggNodeIndex = i
			break
		}
	}
	if aggNodeIndex == -1 {
		t.Fatal("compiled aggregation runtime result node not found")
	}
	aggNode := wf.Nodes[aggNodeIndex]
	if aggNode.Type != "result" {
		t.Fatalf("compiled aggregation Type = %q, want result", aggNode.Type)
	}
	if aggNode.OutputName != "final_answer" {
		t.Errorf("OutputName = %q, want final_answer", aggNode.OutputName)
	}
	if aggNode.AggregationMethod != "judge" {
		t.Errorf("AggregationMethod = %q, want judge", aggNode.AggregationMethod)
	}
	if got := aggNode.AggregationConfig["judge_model"]; got != "openai/gpt-5-mini" {
		t.Errorf("judge_model = %v, want openai/gpt-5-mini", got)
	}
	if got := aggNode.Metadata["presentation_result_id"]; got != "final-result" {
		t.Errorf("presentation_result_id = %v, want final-result", got)
	}
	inputIDs, ok := aggNode.Metadata["input_ids"].([]string)
	if !ok {
		t.Fatalf("input_ids type = %T, want []string", aggNode.Metadata["input_ids"])
	}
	if len(inputIDs) != 2 || inputIDs[0] != "agent-1" || inputIDs[1] != "agent-2" {
		t.Fatalf("input_ids = %#v, want agent-1/agent-2", inputIDs)
	}
	for _, n := range wf.Nodes {
		if n.ID == "final-result" {
			t.Fatal("presentation result node should not be included as a runtime node")
		}
	}
	for _, e := range wf.Edges {
		if e.Target == "final-result" || e.Source == "final-result" {
			t.Fatalf("runtime edge should not reference presentation result: %+v", *e)
		}
	}
}

func TestWorkflowFromWorkflowFile_AggregationMacroNodeWithWorkflowID(t *testing.T) {
	agg := aggregationNode("agg-judge", "judge", map[string]interface{}{
		"judge_model":   "openai/gpt-5-mini",
		"system_prompt": "Judge.",
		"prompt":        "{{responses}}",
		"temperature":   float64(0),
	})
	aggConfig := agg["data"].(map[string]interface{})["config"].(map[string]interface{})
	aggConfig["aggregationWorkflowId"] = "aggregation-judge"

	raw := map[string]interface{}{
		"id":   "wf-agg-ref",
		"name": "Aggregation Macro Ref Test",
		"nodes": []interface{}{
			agentNode("agent-1", "openai/gpt-4o-mini", 256),
			agentNode("agent-2", "anthropic/claude-sonnet-4.5", 256),
			agg,
			map[string]interface{}{
				"id":   "final-result",
				"type": "result",
				"data": map[string]interface{}{
					"type":  "result",
					"label": "Final",
					"config": map[string]interface{}{
						"name":         "final_answer",
						"outputFormat": "text",
					},
				},
			},
		},
		"edges": []interface{}{
			edge("e1", "agent-1", "agg-judge"),
			edge("e2", "agent-2", "agg-judge"),
			edge("e3", "agg-judge", "final-result"),
		},
	}

	wf, err := workflowFromWorkflowFile(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var aggNode *workflow.Node
	for _, n := range wf.Nodes {
		if n.ID == "agg-judge" {
			aggNode = n
		}
		if n.ID == "final-result" {
			t.Fatal("presentation result node should not be included as a runtime node")
		}
	}
	if aggNode == nil {
		t.Fatal("aggregation workflow_ref node not found")
	}
	if aggNode.Type != workflow.NodeTypeWorkflowRef {
		t.Fatalf("aggregation Type = %q, want workflow_ref", aggNode.Type)
	}
	if aggNode.WorkflowRefID != "aggregation-judge" {
		t.Fatalf("WorkflowRefID = %q, want aggregation-judge", aggNode.WorkflowRefID)
	}
	if aggNode.AggregationMethod != workflow.AggMethodJudge {
		t.Fatalf("AggregationMethod = %q, want judge", aggNode.AggregationMethod)
	}
	if got := aggNode.AggregationConfig["judge_model"]; got != "openai/gpt-5-mini" {
		t.Errorf("judge_model = %v, want openai/gpt-5-mini", got)
	}
	if got := aggNode.Metadata["presentation_result_id"]; got != "final-result" {
		t.Errorf("presentation_result_id = %v, want final-result", got)
	}
	inputIDs, ok := aggNode.Metadata["input_ids"].([]string)
	if !ok || len(inputIDs) != 2 || inputIDs[0] != "agent-1" || inputIDs[1] != "agent-2" {
		t.Fatalf("input_ids = %#v, want agent-1/agent-2", aggNode.Metadata["input_ids"])
	}
}

func TestWorkflowFromWorkflowFile_AggregationMacroMajorityVoteNormalizesTieBreaker(t *testing.T) {
	raw := map[string]interface{}{
		"id":   "wf-agg-vote",
		"name": "Aggregation Vote Test",
		"nodes": []interface{}{
			agentNode("agent-1", "openai/gpt-4o-mini", 256),
			agentNode("agent-2", "openai/gpt-4o-mini", 256),
			aggregationNode("agg-vote", "majority_vote", map[string]interface{}{
				"extraction_strategy": "regex",
				"extraction_pattern":  "Answer:\\s*([A-D])",
				"tie_breaker":         "first",
			}),
			resultNode("result-vote"),
		},
		"edges": []interface{}{
			edge("e1", "agent-1", "agg-vote"),
			edge("e2", "agent-2", "agg-vote"),
			edge("e3", "agg-vote", "result-vote"),
		},
	}

	wf, err := workflowFromWorkflowFile(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var aggNode = wf.Nodes[len(wf.Nodes)-1]
	if aggNode.ID != "agg-vote" {
		t.Fatalf("last node ID = %q, want agg-vote", aggNode.ID)
	}
	if got := aggNode.AggregationConfig["tie_breaker_method"]; got != "first" {
		t.Errorf("tie_breaker_method = %v, want first", got)
	}
	if _, exists := aggNode.AggregationConfig["tie_breaker"]; exists {
		t.Fatal("legacy tie_breaker key should be removed")
	}
}

func TestWorkflowFromWorkflowFile_ForkedAggregationConditionalBranchesStayInline(t *testing.T) {
	maxTokens := 256
	raw := map[string]interface{}{
		"id":   "wf-forked-conditional",
		"name": "Forked Conditional",
		"nodes": []interface{}{
			agentNode("candidate-a", "openai/gpt-4o-mini", maxTokens),
			map[string]interface{}{
				"id":   "fork--format-candidates",
				"type": "operation",
				"data": map[string]interface{}{
					"type":  "operation",
					"label": "format candidates",
					"config": map[string]interface{}{
						"name":                     "format candidates",
						"operationType":            "format_candidates",
						"operationConfig":          map[string]interface{}{"input_ids": []interface{}{"candidate-a"}},
						"aggregationInternalState": "forked",
						"aggregationAnchorId":      "aggregation-vote",
					},
				},
			},
			map[string]interface{}{
				"id":   "fork--tie-policy",
				"type": "conditional",
				"data": map[string]interface{}{
					"type":  "conditional",
					"label": "tie policy",
					"config": map[string]interface{}{
						"name":                     "tie policy",
						"condition":                "{{fork--count-votes.tied}} == true",
						"aggregationInternalState": "forked",
						"aggregationAnchorId":      "aggregation-vote",
					},
				},
			},
			map[string]interface{}{
				"id":   "fork--tie-policy_true",
				"type": "agent",
				"data": map[string]interface{}{
					"type":  "agent",
					"label": "tie synthesis",
					"config": map[string]interface{}{
						"name":                      "tie synthesis",
						"model":                     "openai/gpt-4o-mini",
						"systemPrompt":              "Synthesize.",
						"userPrompt":                "{{fork--format-candidates}}",
						"maxTokens":                 maxTokens,
						"aggregationInternalState":  "forked",
						"aggregationAnchorId":       "aggregation-vote",
						"aggregationBranch":         "true",
						"aggregationBranchParentId": "fork--tie-policy",
					},
				},
			},
			map[string]interface{}{
				"id":   "fork--select-winner",
				"type": "operation",
				"data": map[string]interface{}{
					"type":  "operation",
					"label": "select winner",
					"config": map[string]interface{}{
						"name":                     "select winner",
						"operationType":            "select_winner",
						"operationConfig":          map[string]interface{}{"fallback_output": "{{fork--tie-policy}}"},
						"aggregationInternalState": "forked",
						"aggregationAnchorId":      "aggregation-vote",
					},
				},
			},
			resultNode("result"),
		},
		"edges": []interface{}{
			edge("candidate-format", "candidate-a", "fork--format-candidates"),
			edge("format-tie", "fork--format-candidates", "fork--tie-policy"),
			map[string]interface{}{
				"id":           "tie-true",
				"source":       "fork--tie-policy",
				"target":       "fork--tie-policy_true",
				"sourceHandle": "true",
			},
			edge("tie-select", "fork--tie-policy", "fork--select-winner"),
			edge("select-result", "fork--select-winner", "result"),
		},
	}

	wf, err := workflowFromWorkflowFile(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodesByID := map[string]*workflow.Node{}
	for _, node := range wf.Nodes {
		nodesByID[node.ID] = node
	}
	conditional := nodesByID["fork--tie-policy"]
	if conditional == nil {
		t.Fatal("forked conditional should be included as a runtime node")
	}
	if conditional.Type != workflow.NodeTypeConditional {
		t.Fatalf("conditional Type = %q, want conditional", conditional.Type)
	}
	if conditional.TrueBranch == nil {
		t.Fatal("forked conditional true branch should be inline")
	}
	if conditional.TrueBranch.ID != "fork--tie-policy_true" {
		t.Fatalf("true branch ID = %q, want fork--tie-policy_true", conditional.TrueBranch.ID)
	}
	if conditional.TrueBranch.Type != workflow.NodeTypePrompt {
		t.Fatalf("true branch Type = %q, want prompt", conditional.TrueBranch.Type)
	}
	if _, exists := nodesByID["fork--tie-policy_true"]; exists {
		t.Fatal("forked branch prompt should not be a top-level runtime node")
	}

	edgeIDs := map[string]bool{}
	for _, edge := range wf.Edges {
		edgeIDs[edge.ID] = true
	}
	if edgeIDs["tie-true"] {
		t.Fatal("conditional branch edge should be represented inline, not as a top-level edge")
	}
	if !edgeIDs["tie-select"] {
		t.Fatal("conditional scheduling edge to downstream node should be preserved")
	}
}

func TestWorkflowFromWorkflowFile_DataTypeOverridesNodeType(t *testing.T) {
	// data.type takes precedence over the top-level node type
	raw := map[string]interface{}{
		"id":   "wf-dtype",
		"name": "DataType Override",
		"nodes": []interface{}{
			map[string]interface{}{
				"id":   "agent-1",
				"type": "something_else", // top-level type is different
				"data": map[string]interface{}{
					"type":  "agent", // data.type wins
					"label": "Agent",
					"config": map[string]interface{}{
						"name":      "Agent 1",
						"model":     "openai/gpt-4o-mini",
						"maxTokens": 256,
					},
				},
			},
			resultNode("result-1"),
		},
		"edges": []interface{}{
			edge("e1", "agent-1", "result-1"),
		},
	}

	wf, err := workflowFromWorkflowFile(raw, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(wf.Nodes) != 2 {
		t.Errorf("len(Nodes) = %d, want 2", len(wf.Nodes))
	}
}
