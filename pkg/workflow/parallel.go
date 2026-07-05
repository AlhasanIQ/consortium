package workflow

import (
	"fmt"
)

// ExecutionLevel represents a group of nodes that can be executed in parallel
type ExecutionLevel struct {
	Nodes []*Node
}

// BuildExecutionPlan analyzes workflow edges and creates execution levels
// Each level contains nodes that can run in parallel (have same dependencies)
func BuildExecutionPlan(workflow *Workflow) ([]*ExecutionLevel, error) {
	// If no edges, execute sequentially as before
	if len(workflow.Edges) == 0 {
		levels := make([]*ExecutionLevel, len(workflow.Nodes))
		for i, node := range workflow.Nodes {
			levels[i] = &ExecutionLevel{Nodes: []*Node{node}}
		}
		return levels, nil
	}

	// Build node map by ID for quick lookup
	nodeByID := make(map[string]*Node)
	for _, node := range workflow.Nodes {
		if node.ID == "" {
			return nil, fmt.Errorf("node ID is required when using edges")
		}
		nodeByID[node.ID] = node
	}

	// Build dependency graph
	// dependencies[nodeID] = list of node IDs that must complete before this node
	dependencies := make(map[string][]string)
	// dependents[nodeID] = list of node IDs that depend on this node
	dependents := make(map[string][]string)

	// Initialize all nodes
	for _, node := range workflow.Nodes {
		dependencies[node.ID] = []string{}
		dependents[node.ID] = []string{}
	}

	// Build dependency graph from edges
	for _, edge := range workflow.Edges {
		// edge: source -> target means target depends on source
		dependencies[edge.Target] = append(dependencies[edge.Target], edge.Source)
		dependents[edge.Source] = append(dependents[edge.Source], edge.Target)
	}

	// Build execution levels using topological sort
	var levels []*ExecutionLevel
	completed := make(map[string]bool)

	for len(completed) < len(workflow.Nodes) {
		// Find all nodes that have all dependencies completed
		var readyNodes []*Node
		for _, node := range workflow.Nodes {
			if completed[node.ID] {
				continue
			}

			// Check if all dependencies are completed
			allDepsCompleted := true
			for _, depID := range dependencies[node.ID] {
				if !completed[depID] {
					allDepsCompleted = false
					break
				}
			}

			if allDepsCompleted {
				readyNodes = append(readyNodes, node)
			}
		}

		if len(readyNodes) == 0 {
			// No nodes ready - likely a cycle in the graph
			// Debug: print remaining nodes and their dependencies
			var remaining []string
			for _, node := range workflow.Nodes {
				if !completed[node.ID] {
					deps := dependencies[node.ID]
					remaining = append(remaining, fmt.Sprintf("%s (deps: %v)", node.ID, deps))
				}
			}
			return nil, fmt.Errorf("circular dependency detected or invalid graph. Remaining nodes: %v", remaining)
		}

		// Create execution level with all ready nodes
		levels = append(levels, &ExecutionLevel{Nodes: readyNodes})

		// Mark these nodes as completed
		for _, node := range readyNodes {
			completed[node.ID] = true
		}
	}

	return levels, nil
}
