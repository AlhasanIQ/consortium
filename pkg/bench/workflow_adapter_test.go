package bench

import (
	"testing"

	"github.com/alhasaniq/consortium/pkg/workflow"
)

func TestBuildExecutionWorkflow(t *testing.T) {
	file := &WorkflowFile{
		ID:   "reasoning-informed-captain-synthesis",
		Name: "Reasoning Synthesis",
		Nodes: []WorkflowNode{
			{
				ID:   "input",
				Type: "input",
				Data: WorkflowNodeData{
					Type: "input",
				},
			},
			{
				ID:   "agent-a",
				Type: "agent",
				Data: WorkflowNodeData{
					Type: "agent",
					Config: WorkflowNodeConfig{
						Model:        "openai/gpt-5-mini",
						SystemPrompt: "System {{user_prompt}}",
						UserPrompt:   "{{user_prompt}}",
						Temperature:  0.2,
						MaxTokens:    200,
					},
				},
			},
			{
				ID:   "agent-b",
				Type: "agent",
				Data: WorkflowNodeData{
					Type: "agent",
					Config: WorkflowNodeConfig{
						Model:      "google/gemini-2.5-flash",
						UserPrompt: "{{prompt}}",
					},
				},
			},
			{
				ID:   "result-node",
				Type: "result",
				Data: WorkflowNodeData{
					Type: "result",
					Config: WorkflowNodeConfig{
						Name:              "final_answer",
						OutputFormat:      "text",
						AggregationMethod: "peer_matrix",
						AggregationConfig: map[string]interface{}{
							"judge_model": "openai/gpt-5-mini",
						},
					},
				},
			},
		},
	}

	wf, err := BuildExecutionWorkflow(file, "What is 2+2?")
	if err != nil {
		t.Fatalf("BuildExecutionWorkflow failed: %v", err)
	}

	if len(wf.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(wf.Nodes))
	}
	if len(wf.Edges) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(wf.Edges))
	}

	agentNode := wf.Nodes[0]
	if agentNode.Temperature == nil || *agentNode.Temperature != 0.2 {
		t.Fatalf("expected agent temperature=0.2, got %v", agentNode.Temperature)
	}
	if agentNode.MaxTokens != 200 {
		t.Fatalf("expected agent max_tokens=200, got %d", agentNode.MaxTokens)
	}

	resultNode := wf.Nodes[len(wf.Nodes)-1]
	if resultNode.Type != workflow.NodeTypeResult {
		t.Fatalf("expected last node type=result, got %s", resultNode.Type)
	}
	if resultNode.AggregationMethod != workflow.AggMethodPeerMatrix {
		t.Fatalf("expected peer_matrix aggregation, got %s", resultNode.AggregationMethod)
	}
	if resultNode.OutputName != "final_answer" {
		t.Fatalf("expected output_name=final_answer, got %q", resultNode.OutputName)
	}
	if resultNode.AggregationConfig["original_prompt"] == nil {
		t.Fatalf("expected aggregation config to include original_prompt")
	}
	if _, ok := resultNode.Metadata["input_ids"]; !ok {
		t.Fatalf("expected metadata.input_ids")
	}
}

func TestBuildExecutionWorkflowRejectsMissingModel(t *testing.T) {
	file := &WorkflowFile{
		ID:   "bad-workflow",
		Name: "Bad",
		Nodes: []WorkflowNode{
			{
				ID:   "agent-1",
				Type: "agent",
				Data: WorkflowNodeData{
					Type: "agent",
				},
			},
		},
	}

	if _, err := BuildExecutionWorkflow(file, "test"); err == nil {
		t.Fatalf("expected error for missing agent model")
	}
}
