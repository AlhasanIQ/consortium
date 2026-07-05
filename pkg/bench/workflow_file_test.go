package bench

import (
	"strings"
	"testing"
)

func TestParseWorkflowFile_ValidWorkflow(t *testing.T) {
	raw := `{
		"id": "wf-001",
		"name": "Test Workflow",
		"description": "A test workflow",
		"nodes": [
			{
				"id": "node-1",
				"type": "llm_call",
				"data": {
					"type": "llm_call",
					"label": "Node One",
					"config": {
						"model": "gpt-4",
						"systemPrompt": "You are helpful.",
						"userPrompt": "Hello",
						"temperature": 0.7,
						"maxTokens": 512
					}
				}
			}
		],
		"edges": []
	}`

	wf, err := ParseWorkflowFile(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wf.ID != "wf-001" {
		t.Errorf("ID = %q, want %q", wf.ID, "wf-001")
	}
	if wf.Name != "Test Workflow" {
		t.Errorf("Name = %q, want %q", wf.Name, "Test Workflow")
	}
	if wf.Description != "A test workflow" {
		t.Errorf("Description = %q, want %q", wf.Description, "A test workflow")
	}
	if len(wf.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(wf.Nodes))
	}
	if wf.Nodes[0].ID != "node-1" {
		t.Errorf("node ID = %q, want %q", wf.Nodes[0].ID, "node-1")
	}
	if wf.Nodes[0].Data.Config.Model != "gpt-4" {
		t.Errorf("node model = %q, want %q", wf.Nodes[0].Data.Config.Model, "gpt-4")
	}
	if wf.Nodes[0].Data.Config.Temperature != 0.7 {
		t.Errorf("node temperature = %v, want %v", wf.Nodes[0].Data.Config.Temperature, 0.7)
	}
	if wf.Nodes[0].Data.Config.MaxTokens != 512 {
		t.Errorf("node maxTokens = %d, want %d", wf.Nodes[0].Data.Config.MaxTokens, 512)
	}
}

func TestParseWorkflowFile_WithEdges(t *testing.T) {
	raw := `{
		"id": "wf-edges",
		"name": "Workflow With Edges",
		"nodes": [
			{"id": "n1", "type": "llm_call", "data": {"type": "llm_call", "config": {}}},
			{"id": "n2", "type": "result", "data": {"type": "result", "config": {}}}
		],
		"edges": [
			{"id": "e1", "source": "n1", "target": "n2"}
		]
	}`

	wf, err := ParseWorkflowFile(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(wf.Edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(wf.Edges))
	}
	if wf.Edges[0].Source != "n1" || wf.Edges[0].Target != "n2" {
		t.Errorf("edge source/target = %q/%q, want n1/n2", wf.Edges[0].Source, wf.Edges[0].Target)
	}
}

func TestParseWorkflowFile_MultipleNodes(t *testing.T) {
	raw := `{
		"id": "wf-multi",
		"name": "Multi Node",
		"nodes": [
			{"id": "n1", "type": "llm_call", "data": {"type": "llm_call", "config": {"model": "a"}}},
			{"id": "n2", "type": "llm_call", "data": {"type": "llm_call", "config": {"model": "b"}}},
			{"id": "n3", "type": "result",   "data": {"type": "result",   "config": {}}}
		],
		"edges": []
	}`

	wf, err := ParseWorkflowFile(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(wf.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(wf.Nodes))
	}
}

func TestParseWorkflowFile_AggregationConfig(t *testing.T) {
	raw := `{
		"id": "wf-agg",
		"name": "Aggregation Workflow",
		"nodes": [
			{
				"id": "agg",
				"type": "result",
				"data": {
					"type": "result",
					"config": {
						"aggregationMethod": "majority_vote",
						"aggregationConfig": {"threshold": 0.6}
					}
				}
			}
		],
		"edges": []
	}`

	wf, err := ParseWorkflowFile(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg := wf.Nodes[0].Data.Config
	if cfg.AggregationMethod != "majority_vote" {
		t.Errorf("aggregationMethod = %q, want %q", cfg.AggregationMethod, "majority_vote")
	}
	if cfg.AggregationConfig == nil {
		t.Fatal("expected non-nil aggregationConfig")
	}
	threshold, ok := cfg.AggregationConfig["threshold"]
	if !ok {
		t.Fatal("missing threshold in aggregationConfig")
	}
	if threshold != 0.6 {
		t.Errorf("threshold = %v, want 0.6", threshold)
	}
}

func TestParseWorkflowFile_NoNodes_Error(t *testing.T) {
	raw := `{"id": "empty", "name": "Empty", "nodes": [], "edges": []}`
	_, err := ParseWorkflowFile(raw)
	if err == nil {
		t.Fatal("expected error for workflow with no nodes")
	}
	if !strings.Contains(err.Error(), "no nodes") {
		t.Errorf("error message %q does not contain 'no nodes'", err.Error())
	}
}

func TestParseWorkflowFile_NullNodes_Error(t *testing.T) {
	raw := `{"id": "null-nodes", "name": "Null Nodes"}`
	_, err := ParseWorkflowFile(raw)
	if err == nil {
		t.Fatal("expected error for workflow with null/missing nodes")
	}
}

func TestParseWorkflowFile_InvalidJSON_Error(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"empty string", ""},
		{"plain text", "not json at all"},
		{"truncated json", `{"id": "wf", "nodes": [`},
		{"invalid value type", `{"id": 123, "nodes": [{"id":"n1","type":"t","data":{"type":"t","config":{}}}]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseWorkflowFile(tt.raw)
			if err == nil {
				t.Fatalf("expected error for input %q, got nil", tt.raw)
			}
		})
	}
}

func TestParseWorkflowFile_OptionalFieldsAbsent(t *testing.T) {
	// Description and edge IDs are optional
	raw := `{
		"id": "wf-minimal",
		"name": "Minimal",
		"nodes": [
			{"id": "n1", "type": "llm_call", "data": {"type": "llm_call", "config": {}}}
		],
		"edges": [
			{"source": "n1", "target": "n1"}
		]
	}`

	wf, err := ParseWorkflowFile(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wf.Description != "" {
		t.Errorf("expected empty description, got %q", wf.Description)
	}
	if wf.Edges[0].ID != "" {
		t.Errorf("expected empty edge ID, got %q", wf.Edges[0].ID)
	}
}
