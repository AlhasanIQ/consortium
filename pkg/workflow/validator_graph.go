package workflow

import (
	"fmt"
	"strings"
)

func (v *Validator) validateNovomoHandoffs(wf *Workflow) []ValidationError {
	var errors []ValidationError
	if wf == nil {
		return errors
	}

	nodeByID := make(map[string]*Node, len(wf.Nodes))
	nodeIndex := make(map[string]int, len(wf.Nodes))
	for i, node := range wf.Nodes {
		if node == nil || strings.TrimSpace(node.ID) == "" {
			continue
		}
		nodeByID[node.ID] = node
		nodeIndex[node.ID] = i
	}
	graph := make(map[string][]string)
	for _, edge := range wf.Edges {
		graph[edge.Source] = append(graph[edge.Source], edge.Target)
	}

	for _, node := range wf.Nodes {
		if node == nil || (node.Type != NodeTypeAgentRun && node.Type != NodeTypeNovoRun) {
			continue
		}
		nodeID := node.ID
		explicit := NormalizeNovomoHandoff(node.InheritFrom)
		upstream := strings.TrimSpace(node.InheritFromNodeID)
		workflowTask := node.InheritFromWorkflowTask
		if CountNovomoHandoffSources(explicit != nil, upstream != "", workflowTask) > 1 {
			errors = append(errors, ValidationError{
				Field:   "inherit_from",
				NodeID:  nodeID,
				Message: "Set only one of inherit_from, inherit_from_node_id, or inherit_from_workflow_task",
			})
		}
		if err := ValidateNovomoHandoffRef("inherit_from", explicit); err != nil {
			errors = append(errors, ValidationError{
				Field:   "inherit_from",
				NodeID:  nodeID,
				Message: err.Error(),
			})
		}
		if upstream == "" {
			continue
		}
		source, ok := nodeByID[upstream]
		if !ok {
			errors = append(errors, ValidationError{
				Field:   "inherit_from_node_id",
				NodeID:  nodeID,
				Message: fmt.Sprintf("inherit_from_node_id %q does not reference an existing node", upstream),
			})
			continue
		}
		if upstream == nodeID {
			errors = append(errors, ValidationError{
				Field:   "inherit_from_node_id",
				NodeID:  nodeID,
				Message: "inherit_from_node_id cannot reference the same node",
			})
		}
		if source.Type != NodeTypeAgentRun && source.Type != NodeTypeNovoRun {
			errors = append(errors, ValidationError{
				Field:   "inherit_from_node_id",
				NodeID:  nodeID,
				Message: fmt.Sprintf("inherit_from_node_id %q must reference an agent_run or novo_run node", upstream),
			})
		}
		if !isWorkflowUpstream(nodeID, upstream, wf.Edges, graph, nodeIndex) {
			errors = append(errors, ValidationError{
				Field:   "inherit_from_node_id",
				NodeID:  nodeID,
				Message: fmt.Sprintf("inherit_from_node_id %q must be upstream of node %q", upstream, nodeID),
			})
		}
	}

	return errors
}

func isWorkflowUpstream(targetID, sourceID string, edges []*Edge, graph map[string][]string, nodeIndex map[string]int) bool {
	if sourceID == "" || targetID == "" || sourceID == targetID {
		return false
	}
	if len(edges) == 0 {
		sourceIndex, sourceOK := nodeIndex[sourceID]
		targetIndex, targetOK := nodeIndex[targetID]
		return sourceOK && targetOK && sourceIndex < targetIndex
	}
	seen := map[string]bool{}
	var walk func(string) bool
	walk = func(current string) bool {
		if current == targetID {
			return true
		}
		if seen[current] {
			return false
		}
		seen[current] = true
		for _, next := range graph[current] {
			if walk(next) {
				return true
			}
		}
		return false
	}
	return walk(sourceID)
}

// detectCycles checks for circular dependencies in the workflow graph
func (v *Validator) detectCycles(wf *Workflow) []ValidationError {
	var errors []ValidationError

	if len(wf.Edges) == 0 {
		return errors // No edges, no cycles possible
	}

	// Build adjacency list
	graph := make(map[string][]string)
	for _, edge := range wf.Edges {
		graph[edge.Source] = append(graph[edge.Source], edge.Target)
	}

	// Track visit states: 0=unvisited, 1=visiting, 2=visited
	visited := make(map[string]int)
	var cycleStack []string

	// DFS to detect cycles
	var dfs func(string) bool
	dfs = func(node string) bool {
		if visited[node] == 1 {
			// Found a cycle
			cycleStack = append(cycleStack, node)
			return true
		}
		if visited[node] == 2 {
			// Already processed
			return false
		}

		visited[node] = 1 // Mark as visiting
		cycleStack = append(cycleStack, node)

		for _, neighbor := range graph[node] {
			if dfs(neighbor) {
				return true
			}
		}

		cycleStack = cycleStack[:len(cycleStack)-1] // Pop from stack
		visited[node] = 2                           // Mark as visited
		return false
	}

	// Check all nodes for cycles
	for _, node := range wf.Nodes {
		if node.ID != "" && visited[node.ID] == 0 {
			if dfs(node.ID) {
				errors = append(errors, ValidationError{
					Field:   "edges",
					Message: fmt.Sprintf("Circular dependency detected: %s", strings.Join(cycleStack, " -> ")),
				})
				break // Report first cycle found
			}
		}
	}

	return errors
}

// validateReachability performs BFS from root nodes (those with no incoming edges)
// and warns about any nodes that are unreachable. Only applicable when edges exist.
func (v *Validator) validateReachability(wf *Workflow) []ValidationError {
	var errors []ValidationError

	if len(wf.Edges) == 0 {
		return errors // Sequential workflow — all nodes are implicitly reachable
	}

	// Build set of all node IDs
	allNodes := make(map[string]bool)
	for _, node := range wf.Nodes {
		if node.ID != "" {
			allNodes[node.ID] = true
		}
	}

	// Build adjacency list and track incoming edges
	outgoing := make(map[string][]string)
	hasIncoming := make(map[string]bool)
	for _, edge := range wf.Edges {
		outgoing[edge.Source] = append(outgoing[edge.Source], edge.Target)
		hasIncoming[edge.Target] = true
	}

	// Find root nodes (no incoming edges)
	var roots []string
	for id := range allNodes {
		if !hasIncoming[id] {
			roots = append(roots, id)
		}
	}

	if len(roots) == 0 {
		// All nodes have incoming edges — likely a cycle (caught by detectCycles)
		return errors
	}

	// BFS from roots
	visited := make(map[string]bool)
	queue := make([]string, len(roots))
	copy(queue, roots)
	for _, r := range roots {
		visited[r] = true
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, neighbor := range outgoing[current] {
			if !visited[neighbor] {
				visited[neighbor] = true
				queue = append(queue, neighbor)
			}
		}
	}

	// Check for unreachable nodes
	for id := range allNodes {
		if !visited[id] {
			errors = append(errors, ValidationError{
				Field:   "nodes",
				NodeID:  id,
				Message: fmt.Sprintf("Node '%s' is unreachable from any root node", id),
			})
		}
	}

	return errors
}
