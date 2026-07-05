package workflow

import (
	"testing"
)

func TestBuildExecutionPlan_Sequential(t *testing.T) {
	// Test workflow with no edges (should execute sequentially)
	wf := &Workflow{
		ID:   "test-seq",
		Name: "Sequential Test",
		Nodes: []*Node{
			{ID: "node1", Type: NodeTypePrompt},
			{ID: "node2", Type: NodeTypePrompt},
			{ID: "node3", Type: NodeTypePrompt},
		},
		Edges: []*Edge{}, // No edges
	}

	plan, err := BuildExecutionPlan(wf)
	if err != nil {
		t.Fatalf("BuildExecutionPlan failed: %v", err)
	}

	// Should have 3 levels (one for each node)
	if len(plan) != 3 {
		t.Errorf("Expected 3 levels, got %d", len(plan))
	}

	// Each level should have 1 node
	for i, level := range plan {
		if len(level.Nodes) != 1 {
			t.Errorf("Level %d: expected 1 node, got %d", i, len(level.Nodes))
		}
	}
}

func TestBuildExecutionPlan_Parallel(t *testing.T) {
	// Test workflow with parallel execution:
	// Input -> Agent1 (parallel)
	//       -> Agent2 (parallel)
	// Both -> Output
	wf := &Workflow{
		ID:   "test-parallel",
		Name: "Parallel Test",
		Nodes: []*Node{
			{ID: "input", Type: NodeTypePrompt},
			{ID: "agent1", Type: NodeTypePrompt},
			{ID: "agent2", Type: NodeTypePrompt},
			{ID: "output", Type: NodeTypeResult},
		},
		Edges: []*Edge{
			{ID: "e1", Source: "input", Target: "agent1"},
			{ID: "e2", Source: "input", Target: "agent2"},
			{ID: "e3", Source: "agent1", Target: "output"},
			{ID: "e4", Source: "agent2", Target: "output"},
		},
	}

	plan, err := BuildExecutionPlan(wf)
	if err != nil {
		t.Fatalf("BuildExecutionPlan failed: %v", err)
	}

	// Should have 3 levels:
	// Level 0: input (1 node)
	// Level 1: agent1, agent2 (2 nodes in parallel)
	// Level 2: output (1 node)
	if len(plan) != 3 {
		t.Fatalf("Expected 3 levels, got %d", len(plan))
	}

	// Level 0: input
	if len(plan[0].Nodes) != 1 || plan[0].Nodes[0].ID != "input" {
		t.Errorf("Level 0: expected input node, got %v", plan[0].Nodes)
	}

	// Level 1: agent1 and agent2 (parallel)
	if len(plan[1].Nodes) != 2 {
		t.Fatalf("Level 1: expected 2 nodes (parallel), got %d", len(plan[1].Nodes))
	}

	// Check that both agents are in level 1
	nodeIDs := make(map[string]bool)
	for _, node := range plan[1].Nodes {
		nodeIDs[node.ID] = true
	}
	if !nodeIDs["agent1"] || !nodeIDs["agent2"] {
		t.Errorf("Level 1: expected agent1 and agent2, got %v", nodeIDs)
	}

	// Level 2: output
	if len(plan[2].Nodes) != 1 || plan[2].Nodes[0].ID != "output" {
		t.Errorf("Level 2: expected output node, got %v", plan[2].Nodes)
	}
}

func TestBuildExecutionPlan_Complex(t *testing.T) {
	// Test more complex workflow:
	// Input -> A (parallel)
	//       -> B (parallel)
	// A -> C
	// B -> D
	// C, D -> Output (both need to complete)
	wf := &Workflow{
		ID:   "test-complex",
		Name: "Complex Test",
		Nodes: []*Node{
			{ID: "input", Type: NodeTypePrompt},
			{ID: "a", Type: NodeTypePrompt},
			{ID: "b", Type: NodeTypePrompt},
			{ID: "c", Type: NodeTypePrompt},
			{ID: "d", Type: NodeTypePrompt},
			{ID: "output", Type: NodeTypeResult},
		},
		Edges: []*Edge{
			{ID: "e1", Source: "input", Target: "a"},
			{ID: "e2", Source: "input", Target: "b"},
			{ID: "e3", Source: "a", Target: "c"},
			{ID: "e4", Source: "b", Target: "d"},
			{ID: "e5", Source: "c", Target: "output"},
			{ID: "e6", Source: "d", Target: "output"},
		},
	}

	plan, err := BuildExecutionPlan(wf)
	if err != nil {
		t.Fatalf("BuildExecutionPlan failed: %v", err)
	}

	// Should have 4 levels:
	// Level 0: input
	// Level 1: a, b (parallel)
	// Level 2: c, d (parallel)
	// Level 3: output
	if len(plan) != 4 {
		t.Fatalf("Expected 4 levels, got %d", len(plan))
	}

	// Check level 0
	if len(plan[0].Nodes) != 1 || plan[0].Nodes[0].ID != "input" {
		t.Errorf("Level 0: expected input, got %v", plan[0].Nodes)
	}

	// Check level 1: a, b
	if len(plan[1].Nodes) != 2 {
		t.Fatalf("Level 1: expected 2 nodes, got %d", len(plan[1].Nodes))
	}

	// Check level 2: c, d
	if len(plan[2].Nodes) != 2 {
		t.Fatalf("Level 2: expected 2 nodes, got %d", len(plan[2].Nodes))
	}

	// Check level 3: output
	if len(plan[3].Nodes) != 1 || plan[3].Nodes[0].ID != "output" {
		t.Errorf("Level 3: expected output, got %v", plan[3].Nodes)
	}
}

func TestBuildExecutionPlan_MissingNodeID(t *testing.T) {
	wf := &Workflow{
		ID:   "test-missing-id",
		Name: "Missing ID Test",
		Nodes: []*Node{
			{Type: NodeTypePrompt}, // Missing ID
		},
		Edges: []*Edge{
			{ID: "e1", Source: "node1", Target: "node2"},
		},
	}

	_, err := BuildExecutionPlan(wf)
	if err == nil {
		t.Error("Expected error for missing node ID, got nil")
	}
}

func TestBuildExecutionPlan_CircularDependency(t *testing.T) {
	wf := &Workflow{
		ID:   "test-circular",
		Name: "Circular Test",
		Nodes: []*Node{
			{ID: "a", Type: NodeTypePrompt},
			{ID: "b", Type: NodeTypePrompt},
		},
		Edges: []*Edge{
			{ID: "e1", Source: "a", Target: "b"},
			{ID: "e2", Source: "b", Target: "a"}, // Circular!
		},
	}

	_, err := BuildExecutionPlan(wf)
	if err == nil {
		t.Error("Expected error for circular dependency, got nil")
	}
}
