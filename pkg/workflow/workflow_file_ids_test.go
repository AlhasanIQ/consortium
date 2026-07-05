package workflow

import (
	"testing"
)

func TestNormalizeWorkflowFileIDsWorkflowIDGeneration(t *testing.T) {
	tests := []struct {
		name           string
		input          map[string]interface{}
		shouldGenerate bool
	}{
		{
			name:           "missing workflow id generates new one",
			input:          map[string]interface{}{},
			shouldGenerate: true,
		},
		{
			name: "empty workflow id generates new one",
			input: map[string]interface{}{
				"id": "",
			},
			shouldGenerate: true,
		},
		{
			name: "non-string workflow id generates new one",
			input: map[string]interface{}{
				"id": 123,
			},
			shouldGenerate: true,
		},
		{
			name: "existing workflow id is preserved",
			input: map[string]interface{}{
				"id": "existing-id",
			},
			shouldGenerate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NormalizeWorkflowFileIDs(tt.input)
			if err != nil {
				t.Errorf("NormalizeWorkflowFileIDs() returned error: %v", err)
			}

			id, ok := tt.input["id"].(string)
			if !ok {
				t.Fatalf("workflow id not a string")
			}

			if id == "" {
				t.Errorf("workflow id is empty")
			}

			if !tt.shouldGenerate {
				if id != "existing-id" {
					t.Errorf("workflow id: got %q, want %q", id, "existing-id")
				}
			} else {
				// Generated IDs should be valid UUIDs
				if len(id) == 0 {
					t.Errorf("generated workflow id is empty")
				}
			}
		})
	}
}

func TestNormalizeWorkflowFileIDsNodeIDGeneration(t *testing.T) {
	tests := []struct {
		name          string
		input         map[string]interface{}
		expectedCount int
	}{
		{
			name: "missing node ids generate new ones",
			input: map[string]interface{}{
				"id": "wf1",
				"nodes": []interface{}{
					map[string]interface{}{
						"type":  "prompt",
						"model": "gpt-4",
					},
					map[string]interface{}{
						"id":   "existing-node",
						"type": "result",
					},
				},
			},
			expectedCount: 2,
		},
		{
			name: "empty node ids generate new ones",
			input: map[string]interface{}{
				"id": "wf1",
				"nodes": []interface{}{
					map[string]interface{}{
						"id":   "",
						"type": "prompt",
					},
					map[string]interface{}{
						"id":   "node2",
						"type": "result",
					},
				},
			},
			expectedCount: 2,
		},
		{
			name: "non-string node ids generate new ones",
			input: map[string]interface{}{
				"id": "wf1",
				"nodes": []interface{}{
					map[string]interface{}{
						"id":   123,
						"type": "prompt",
					},
				},
			},
			expectedCount: 1,
		},
		{
			name: "no nodes",
			input: map[string]interface{}{
				"id": "wf1",
			},
			expectedCount: 0,
		},
		{
			name: "invalid node type in array is left as-is",
			input: map[string]interface{}{
				"id": "wf1",
				"nodes": []interface{}{
					"not a map",
					map[string]interface{}{
						"type": "prompt",
					},
				},
			},
			expectedCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NormalizeWorkflowFileIDs(tt.input)
			if err != nil {
				t.Errorf("NormalizeWorkflowFileIDs() returned error: %v", err)
			}

			nodes, ok := tt.input["nodes"].([]interface{})
			if !ok {
				if tt.expectedCount == 0 {
					return
				}
				t.Fatalf("nodes not an array")
			}

			if len(nodes) != tt.expectedCount {
				t.Errorf("nodes count: got %d, want %d", len(nodes), tt.expectedCount)
			}

			for i, nodeInterface := range nodes {
				node, ok := nodeInterface.(map[string]interface{})
				if !ok {
					continue
				}

				id, ok := node["id"].(string)
				if !ok {
					t.Errorf("node[%d] id is not a string", i)
					continue
				}

				if id == "" {
					t.Errorf("node[%d] id is empty", i)
				}
			}
		})
	}
}

func TestNormalizeWorkflowFileIDsEdgeIDGeneration(t *testing.T) {
	tests := []struct {
		name          string
		input         map[string]interface{}
		expectedCount int
	}{
		{
			name: "missing edge ids generate new ones",
			input: map[string]interface{}{
				"id": "wf1",
				"edges": []interface{}{
					map[string]interface{}{
						"source": "node1",
						"target": "node2",
					},
					map[string]interface{}{
						"id":     "existing-edge",
						"source": "node2",
						"target": "node3",
					},
				},
			},
			expectedCount: 2,
		},
		{
			name: "empty edge ids generate new ones",
			input: map[string]interface{}{
				"id": "wf1",
				"edges": []interface{}{
					map[string]interface{}{
						"id":     "",
						"source": "node1",
						"target": "node2",
					},
				},
			},
			expectedCount: 1,
		},
		{
			name: "non-string edge ids generate new ones",
			input: map[string]interface{}{
				"id": "wf1",
				"edges": []interface{}{
					map[string]interface{}{
						"id":     456,
						"source": "node1",
						"target": "node2",
					},
				},
			},
			expectedCount: 1,
		},
		{
			name: "no edges",
			input: map[string]interface{}{
				"id": "wf1",
			},
			expectedCount: 0,
		},
		{
			name: "invalid edge type in array is left as-is",
			input: map[string]interface{}{
				"id": "wf1",
				"edges": []interface{}{
					"not a map",
					map[string]interface{}{
						"source": "node1",
						"target": "node2",
					},
				},
			},
			expectedCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NormalizeWorkflowFileIDs(tt.input)
			if err != nil {
				t.Errorf("NormalizeWorkflowFileIDs() returned error: %v", err)
			}

			edges, ok := tt.input["edges"].([]interface{})
			if !ok {
				if tt.expectedCount == 0 {
					return
				}
				t.Fatalf("edges not an array")
			}

			if len(edges) != tt.expectedCount {
				t.Errorf("edges count: got %d, want %d", len(edges), tt.expectedCount)
			}

			for i, edgeInterface := range edges {
				edge, ok := edgeInterface.(map[string]interface{})
				if !ok {
					continue
				}

				id, ok := edge["id"].(string)
				if !ok {
					t.Errorf("edge[%d] id is not a string", i)
					continue
				}

				if id == "" {
					t.Errorf("edge[%d] id is empty", i)
				}
			}
		})
	}
}

func TestNormalizeWorkflowFileIDsComplexWorkflow(t *testing.T) {
	input := map[string]interface{}{
		"name": "Test Workflow",
		"nodes": []interface{}{
			map[string]interface{}{
				"type":  "prompt",
				"model": "gpt-4",
			},
			map[string]interface{}{
				"id":   "existing-node",
				"type": "result",
			},
			map[string]interface{}{
				"id":   "",
				"type": "conditional",
			},
		},
		"edges": []interface{}{
			map[string]interface{}{
				"source": "node1",
				"target": "node2",
			},
			map[string]interface{}{
				"id":     "edge2",
				"source": "node2",
				"target": "node3",
			},
		},
	}

	err := NormalizeWorkflowFileIDs(input)
	if err != nil {
		t.Fatalf("NormalizeWorkflowFileIDs() returned error: %v", err)
	}

	// Check workflow ID
	workflowID, ok := input["id"].(string)
	if !ok || workflowID == "" {
		t.Errorf("workflow id not generated")
	}

	// Check nodes
	nodes, ok := input["nodes"].([]interface{})
	if !ok || len(nodes) != 3 {
		t.Fatalf("nodes malformed")
	}

	// First node should have generated ID
	node1, ok := nodes[0].(map[string]interface{})
	if !ok {
		t.Fatalf("node[0] is not a map")
	}
	id1, ok := node1["id"].(string)
	if !ok || id1 == "" {
		t.Errorf("node[0] id not generated")
	}

	// Second node should keep existing ID
	node2, ok := nodes[1].(map[string]interface{})
	if !ok {
		t.Fatalf("node[1] is not a map")
	}
	id2, _ := node2["id"].(string)
	if id2 != "existing-node" {
		t.Errorf("node[1] id: got %q, want %q", id2, "existing-node")
	}

	// Third node should have generated ID (was empty)
	node3, ok := nodes[2].(map[string]interface{})
	if !ok {
		t.Fatalf("node[2] is not a map")
	}
	id3, ok := node3["id"].(string)
	if !ok || id3 == "" {
		t.Errorf("node[2] id not generated")
	}

	// Check edges
	edges, ok := input["edges"].([]interface{})
	if !ok || len(edges) != 2 {
		t.Fatalf("edges malformed")
	}

	// First edge should have generated ID
	edge1, ok := edges[0].(map[string]interface{})
	if !ok {
		t.Fatalf("edge[0] is not a map")
	}
	eid1, ok := edge1["id"].(string)
	if !ok || eid1 == "" {
		t.Errorf("edge[0] id not generated")
	}

	// Second edge should keep existing ID
	edge2, ok := edges[1].(map[string]interface{})
	if !ok {
		t.Fatalf("edge[1] is not a map")
	}
	eid2, _ := edge2["id"].(string)
	if eid2 != "edge2" {
		t.Errorf("edge[1] id: got %q, want %q", eid2, "edge2")
	}
}

func TestNormalizeWorkflowFileIDsPreservesOtherFields(t *testing.T) {
	input := map[string]interface{}{
		"id":          "wf1",
		"name":        "Test Workflow",
		"description": "A test workflow",
		"context": map[string]interface{}{
			"topic": "AI",
		},
		"nodes": []interface{}{
			map[string]interface{}{
				"id":       "node1",
				"type":     "prompt",
				"model":    "gpt-4",
				"prompt":   "test prompt",
				"metadata": map[string]interface{}{"custom": "value"},
			},
		},
	}

	err := NormalizeWorkflowFileIDs(input)
	if err != nil {
		t.Fatalf("NormalizeWorkflowFileIDs() returned error: %v", err)
	}

	// Check that other fields are preserved
	if input["name"] != "Test Workflow" {
		t.Errorf("name not preserved")
	}

	if input["description"] != "A test workflow" {
		t.Errorf("description not preserved")
	}

	context, ok := input["context"].(map[string]interface{})
	if !ok || context["topic"] != "AI" {
		t.Errorf("context not preserved")
	}

	nodes, _ := input["nodes"].([]interface{})
	node, _ := nodes[0].(map[string]interface{})
	if node["model"] != "gpt-4" {
		t.Errorf("node model not preserved")
	}

	if node["prompt"] != "test prompt" {
		t.Errorf("node prompt not preserved")
	}
}

func TestNormalizeWorkflowFileIDsIdempotent(t *testing.T) {
	input := map[string]interface{}{
		"name": "Test",
		"nodes": []interface{}{
			map[string]interface{}{
				"type": "prompt",
			},
		},
		"edges": []interface{}{
			map[string]interface{}{
				"source": "n1",
				"target": "n2",
			},
		},
	}

	// First normalization
	err1 := NormalizeWorkflowFileIDs(input)
	if err1 != nil {
		t.Fatalf("first NormalizeWorkflowFileIDs() returned error: %v", err1)
	}

	// Extract IDs
	id1, _ := input["id"].(string)
	nodes1 := input["nodes"].([]interface{})
	node1, _ := nodes1[0].(map[string]interface{})
	nodeID1, _ := node1["id"].(string)
	edges1 := input["edges"].([]interface{})
	edge1, _ := edges1[0].(map[string]interface{})
	edgeID1, _ := edge1["id"].(string)

	// Second normalization (should not change IDs)
	err2 := NormalizeWorkflowFileIDs(input)
	if err2 != nil {
		t.Fatalf("second NormalizeWorkflowFileIDs() returned error: %v", err2)
	}

	// Extract IDs again
	id2, _ := input["id"].(string)
	nodes2 := input["nodes"].([]interface{})
	node2, _ := nodes2[0].(map[string]interface{})
	nodeID2, _ := node2["id"].(string)
	edges2 := input["edges"].([]interface{})
	edge2, _ := edges2[0].(map[string]interface{})
	edgeID2, _ := edge2["id"].(string)

	if id1 != id2 {
		t.Errorf("workflow id changed on second normalization: %q -> %q", id1, id2)
	}

	if nodeID1 != nodeID2 {
		t.Errorf("node id changed on second normalization: %q -> %q", nodeID1, nodeID2)
	}

	if edgeID1 != edgeID2 {
		t.Errorf("edge id changed on second normalization: %q -> %q", edgeID1, edgeID2)
	}
}
