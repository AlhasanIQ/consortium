package workflow

import (
	"encoding/json"
	"testing"
)

func TestNodeDisplayMetaExtraction(t *testing.T) {
	tests := []struct {
		name     string
		node     *Node
		expected NodeDisplayMeta
	}{
		{
			name:     "nil metadata returns empty display meta",
			node:     &Node{ID: "node1"},
			expected: NodeDisplayMeta{},
		},
		{
			name: "empty metadata returns empty display meta",
			node: &Node{
				ID:       "node1",
				Metadata: map[string]interface{}{},
			},
			expected: NodeDisplayMeta{},
		},
		{
			name: "all fields present",
			node: &Node{
				ID: "node1",
				Metadata: map[string]interface{}{
					"label":       "My Node",
					"name":        "node_name",
					"description": "Node description",
					"output_name": "result",
					"input_ids":   []interface{}{"input1", "input2"},
				},
			},
			expected: NodeDisplayMeta{
				Label:       "My Node",
				Name:        "node_name",
				Description: "Node description",
				OutputName:  "result",
				InputIDs:    []string{"input1", "input2"},
			},
		},
		{
			name: "partial fields",
			node: &Node{
				ID: "node1",
				Metadata: map[string]interface{}{
					"label": "Test Node",
					"name":  "test",
				},
			},
			expected: NodeDisplayMeta{
				Label: "Test Node",
				Name:  "test",
			},
		},
		{
			name: "input_ids as []string",
			node: &Node{
				ID: "node1",
				Metadata: map[string]interface{}{
					"input_ids": []string{"a", "b", "c"},
				},
			},
			expected: NodeDisplayMeta{
				InputIDs: []string{"a", "b", "c"},
			},
		},
		{
			name: "input_ids with non-string elements ignored",
			node: &Node{
				ID: "node1",
				Metadata: map[string]interface{}{
					"input_ids": []interface{}{"valid", 123, "also_valid", nil},
				},
			},
			expected: NodeDisplayMeta{
				InputIDs: []string{"valid", "also_valid"},
			},
		},
		{
			name: "wrong type fields ignored",
			node: &Node{
				ID: "node1",
				Metadata: map[string]interface{}{
					"label":       123, // wrong type
					"name":        []string{"not", "string"},
					"description": "valid description",
				},
			},
			expected: NodeDisplayMeta{
				Description: "valid description",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.node.DisplayMeta()

			if result.Label != tt.expected.Label {
				t.Errorf("Label: got %q, want %q", result.Label, tt.expected.Label)
			}

			if result.Name != tt.expected.Name {
				t.Errorf("Name: got %q, want %q", result.Name, tt.expected.Name)
			}

			if result.Description != tt.expected.Description {
				t.Errorf("Description: got %q, want %q", result.Description, tt.expected.Description)
			}

			if result.OutputName != tt.expected.OutputName {
				t.Errorf("OutputName: got %q, want %q", result.OutputName, tt.expected.OutputName)
			}

			if len(result.InputIDs) != len(tt.expected.InputIDs) {
				t.Errorf("InputIDs length: got %d, want %d", len(result.InputIDs), len(tt.expected.InputIDs))
			} else {
				for i, id := range result.InputIDs {
					if id != tt.expected.InputIDs[i] {
						t.Errorf("InputIDs[%d]: got %q, want %q", i, id, tt.expected.InputIDs[i])
					}
				}
			}
		})
	}
}

func TestWorkflowRefNodeJSONRoundTrip(t *testing.T) {
	node := Node{
		ID:            "ref",
		Type:          NodeTypeWorkflowRef,
		WorkflowRefID: "aggregation-synthesis",
		InputTemplate: map[string]string{"user_prompt": "{{user_prompt}}"},
		OutputKey:     "result",
	}

	data, err := json.Marshal(node)
	if err != nil {
		t.Fatalf("marshal workflow_ref node: %v", err)
	}

	var roundTrip Node
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("unmarshal workflow_ref node: %v", err)
	}

	if roundTrip.Type != NodeTypeWorkflowRef {
		t.Fatalf("Type = %q, want %q", roundTrip.Type, NodeTypeWorkflowRef)
	}
	if roundTrip.WorkflowRefID != "aggregation-synthesis" {
		t.Fatalf("WorkflowRefID = %q", roundTrip.WorkflowRefID)
	}
	if got := roundTrip.InputTemplate["user_prompt"]; got != "{{user_prompt}}" {
		t.Fatalf("InputTemplate[user_prompt] = %q", got)
	}
	if roundTrip.OutputKey != "result" {
		t.Fatalf("OutputKey = %q", roundTrip.OutputKey)
	}
	if roundTrip.ChildWorkflowID != "" || roundTrip.ChildInputTemplate != nil || roundTrip.ChildOutputKey != "" {
		t.Fatalf("workflow_ref JSON should not populate child_workflow fields: %+v", roundTrip)
	}
}

func TestNewErrorResult(t *testing.T) {
	tests := []struct {
		name           string
		nodeID         string
		errMsg         string
		latencyMs      float64
		metadata       []map[string]interface{}
		expectedResult *NodeResult
	}{
		{
			name:      "basic error result without metadata",
			nodeID:    "node1",
			errMsg:    "test error",
			latencyMs: 100.5,
			metadata:  nil,
			expectedResult: &NodeResult{
				NodeID:    "node1",
				Success:   false,
				Error:     "test error",
				LatencyMs: 100.5,
			},
		},
		{
			name:      "error result with metadata",
			nodeID:    "node2",
			errMsg:    "failed to process",
			latencyMs: 50.0,
			metadata: []map[string]interface{}{
				{
					"retry_count": 3,
					"reason":      "timeout",
				},
			},
			expectedResult: &NodeResult{
				NodeID:    "node2",
				Success:   false,
				Error:     "failed to process",
				LatencyMs: 50.0,
				Metadata: map[string]interface{}{
					"retry_count": 3,
					"reason":      "timeout",
				},
			},
		},
		{
			name:      "error result with empty metadata map",
			nodeID:    "node3",
			errMsg:    "empty metadata error",
			latencyMs: 75.0,
			metadata: []map[string]interface{}{
				nil,
			},
			expectedResult: &NodeResult{
				NodeID:    "node3",
				Success:   false,
				Error:     "empty metadata error",
				LatencyMs: 75.0,
			},
		},
		{
			name:      "zero latency",
			nodeID:    "node4",
			errMsg:    "instant error",
			latencyMs: 0,
			metadata:  nil,
			expectedResult: &NodeResult{
				NodeID:    "node4",
				Success:   false,
				Error:     "instant error",
				LatencyMs: 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := newErrorResult(tt.nodeID, tt.errMsg, tt.latencyMs, tt.metadata...)

			if result.NodeID != tt.expectedResult.NodeID {
				t.Errorf("NodeID: got %q, want %q", result.NodeID, tt.expectedResult.NodeID)
			}

			if result.Success != tt.expectedResult.Success {
				t.Errorf("Success: got %v, want %v", result.Success, tt.expectedResult.Success)
			}

			if result.Error != tt.expectedResult.Error {
				t.Errorf("Error: got %q, want %q", result.Error, tt.expectedResult.Error)
			}

			if result.LatencyMs != tt.expectedResult.LatencyMs {
				t.Errorf("LatencyMs: got %v, want %v", result.LatencyMs, tt.expectedResult.LatencyMs)
			}

			// Check metadata
			if tt.expectedResult.Metadata == nil {
				if result.Metadata != nil {
					t.Errorf("Metadata: got %v, want nil", result.Metadata)
				}
			} else {
				if result.Metadata == nil {
					t.Errorf("Metadata: got nil, want %v", tt.expectedResult.Metadata)
				} else {
					for k, v := range tt.expectedResult.Metadata {
						if result.Metadata[k] != v {
							t.Errorf("Metadata[%q]: got %v, want %v", k, result.Metadata[k], v)
						}
					}
				}
			}

			// Check that other fields are zero values
			if result.Output != "" {
				t.Errorf("Output should be empty, got %q", result.Output)
			}

			if result.TokensInput != 0 {
				t.Errorf("TokensInput should be 0, got %d", result.TokensInput)
			}

			if result.TokensOutput != 0 {
				t.Errorf("TokensOutput should be 0, got %d", result.TokensOutput)
			}

			if result.Cost != 0 {
				t.Errorf("Cost should be 0, got %f", result.Cost)
			}

			if result.AttemptNumber != 0 {
				t.Errorf("AttemptNumber should be 0, got %d", result.AttemptNumber)
			}
		})
	}
}

func TestNodeTypes(t *testing.T) {
	// Verify that node type constants are defined and have expected values
	if NodeTypePrompt != "prompt" {
		t.Errorf("NodeTypePrompt = %q, want %q", NodeTypePrompt, "prompt")
	}

	if NodeTypeConditional != "conditional" {
		t.Errorf("NodeTypeConditional = %q, want %q", NodeTypeConditional, "conditional")
	}

	if NodeTypeResult != "result" {
		t.Errorf("NodeTypeResult = %q, want %q", NodeTypeResult, "result")
	}

	if NodeTypeChildWorkflow != "child_workflow" {
		t.Errorf("NodeTypeChildWorkflow = %q, want %q", NodeTypeChildWorkflow, "child_workflow")
	}

	if NodeTypeContractExtract != "contract_extract" {
		t.Errorf("NodeTypeContractExtract = %q, want %q", NodeTypeContractExtract, "contract_extract")
	}
}

func TestAggregationMethods(t *testing.T) {
	// Verify that aggregation method constants are defined and have expected values
	expectedMethods := map[AggregationMethod]string{
		AggMethodCollect:      "collect",
		AggMethodJudge:        "judge",
		AggMethodScoring:      "scoring",
		AggMethodSynthesis:    "synthesis",
		AggMethodPeerMatrix:   "peer_matrix",
		AggMethodMajorityVote: "majority_vote",
		AggMethodDebateDecide: "debate_decide",
	}

	for method, expectedStr := range expectedMethods {
		if string(method) != expectedStr {
			t.Errorf("AggregationMethod %v = %q, want %q", method, string(method), expectedStr)
		}
	}
}

func TestCostLimits(t *testing.T) {
	tests := []struct {
		name        string
		limits      *CostLimits
		expectedStr string
	}{
		{
			name:        "nil cost limits",
			limits:      nil,
			expectedStr: "<nil>",
		},
		{
			name: "zero cost limits",
			limits: &CostLimits{
				MaxCostUSD:      0,
				MaxTokens:       0,
				MaxInputTokens:  0,
				MaxOutputTokens: 0,
			},
			expectedStr: "unlimited",
		},
		{
			name: "with cost limit",
			limits: &CostLimits{
				MaxCostUSD:      10.50,
				MaxTokens:       5000,
				MaxInputTokens:  2000,
				MaxOutputTokens: 3000,
			},
			expectedStr: "cost=10.50",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.limits == nil {
				if tt.limits != nil {
					t.Errorf("Expected nil limits")
				}
				return
			}

			// Verify fields can be accessed
			_ = tt.limits.MaxCostUSD
			_ = tt.limits.MaxTokens
			_ = tt.limits.MaxInputTokens
			_ = tt.limits.MaxOutputTokens
		})
	}
}

func TestWorkflowResult(t *testing.T) {
	result := &WorkflowResult{
		WorkflowID:        "wf1",
		Success:           true,
		TotalTokens:       1000,
		TotalInputTokens:  500,
		TotalOutputTokens: 500,
		TotalCost:         1.50,
		TotalLatency:      5000.0,
	}

	if result.WorkflowID != "wf1" {
		t.Errorf("WorkflowID: got %q, want %q", result.WorkflowID, "wf1")
	}

	if !result.Success {
		t.Errorf("Success: got %v, want true", result.Success)
	}

	if result.TotalTokens != 1000 {
		t.Errorf("TotalTokens: got %d, want 1000", result.TotalTokens)
	}

	if result.TotalCost != 1.50 {
		t.Errorf("TotalCost: got %f, want 1.50", result.TotalCost)
	}
}

func TestNodeStruct(t *testing.T) {
	temp := 0.7
	node := &Node{
		ID:             "node1",
		Type:           NodeTypePrompt,
		Model:          "gpt-4",
		Prompt:         "test prompt",
		SystemPrompt:   "system message",
		Temperature:    &temp,
		MaxTokens:      1000,
		TimeoutSeconds: 30,
	}

	if node.ID != "node1" {
		t.Errorf("ID: got %q, want %q", node.ID, "node1")
	}

	if node.Type != NodeTypePrompt {
		t.Errorf("Type: got %q, want %q", node.Type, NodeTypePrompt)
	}

	if node.Model != "gpt-4" {
		t.Errorf("Model: got %q, want %q", node.Model, "gpt-4")
	}

	if *node.Temperature != 0.7 {
		t.Errorf("Temperature: got %f, want 0.7", *node.Temperature)
	}

	if node.TimeoutSeconds != 30 {
		t.Errorf("TimeoutSeconds: got %d, want 30", node.TimeoutSeconds)
	}
}

func TestEdgeStruct(t *testing.T) {
	edge := &Edge{
		ID:     "edge1",
		Source: "node1",
		Target: "node2",
	}

	if edge.ID != "edge1" {
		t.Errorf("ID: got %q, want %q", edge.ID, "edge1")
	}

	if edge.Source != "node1" {
		t.Errorf("Source: got %q, want %q", edge.Source, "node1")
	}

	if edge.Target != "node2" {
		t.Errorf("Target: got %q, want %q", edge.Target, "node2")
	}
}

func TestWorkflowStruct(t *testing.T) {
	workflow := &Workflow{
		ID:          "wf1",
		Name:        "Test Workflow",
		Description: "A test workflow",
		Nodes: []*Node{
			{ID: "node1", Type: NodeTypePrompt},
			{ID: "node2", Type: NodeTypeResult},
		},
		Edges: []*Edge{
			{ID: "edge1", Source: "node1", Target: "node2"},
		},
		Context: map[string]interface{}{
			"topic": "AI",
		},
	}

	if workflow.ID != "wf1" {
		t.Errorf("ID: got %q, want %q", workflow.ID, "wf1")
	}

	if workflow.Name != "Test Workflow" {
		t.Errorf("Name: got %q, want %q", workflow.Name, "Test Workflow")
	}

	if len(workflow.Nodes) != 2 {
		t.Errorf("Nodes length: got %d, want 2", len(workflow.Nodes))
	}

	if len(workflow.Edges) != 1 {
		t.Errorf("Edges length: got %d, want 1", len(workflow.Edges))
	}
}
