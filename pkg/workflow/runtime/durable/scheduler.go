// Package durable implements the durable execution runtime with deterministic replay.
package durable

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

// NodeState represents the state of a node in the scheduler.
type NodeState string

const (
	NodeStatePending   NodeState = "pending"
	NodeStateScheduled NodeState = "scheduled"
	NodeStateRunning   NodeState = "running"
	NodeStateCompleted NodeState = "completed"
	NodeStateFailed    NodeState = "failed"
	NodeStateSkipped   NodeState = "skipped"
)

// SchedulerState is the deterministic state machine for workflow execution.
// It is reconstructed from history events via Replay and produces commands
// (ready nodes to schedule) based on the current DAG and node states.
type SchedulerState struct {
	Nodes             map[string]NodeState   `json:"nodes"`
	NodeOutputs       map[string]interface{} `json:"node_outputs"`
	NodeErrors        map[string]string      `json:"node_errors"`
	PendingActivities map[string]string      `json:"pending_activities"` // activity_id → node_id
	Completed         bool                   `json:"completed"`
	Failed            bool                   `json:"failed"`
	Cancelled         bool                   `json:"cancelled"`
	ActivityAttempts  map[string]int         `json:"activity_attempts"` // node_id → current attempt
	AdaptiveFailures  map[string]int         `json:"adaptive_failures"` // node_id → consecutive trigger failures
	WorkflowContext   map[string]interface{} `json:"workflow_context,omitempty"`
}

// NewSchedulerState creates a fresh scheduler state with all nodes pending.
func NewSchedulerState(nodeIDs []string) *SchedulerState {
	s := &SchedulerState{
		Nodes:             make(map[string]NodeState, len(nodeIDs)),
		NodeOutputs:       make(map[string]interface{}),
		NodeErrors:        make(map[string]string),
		PendingActivities: make(map[string]string),
		ActivityAttempts:  make(map[string]int),
		AdaptiveFailures:  make(map[string]int),
	}
	for _, id := range nodeIDs {
		s.Nodes[id] = NodeStatePending
	}
	return s
}

// IsTerminal returns true if the workflow has reached a terminal state.
func (s *SchedulerState) IsTerminal() bool {
	return s.Completed || s.Failed || s.Cancelled
}

// ReadySet returns the set of node IDs whose dependencies are all completed
// and that are currently pending. Results are sorted by ID for determinism.
//
// deps maps each node ID to its set of dependency (predecessor) node IDs.
func (s *SchedulerState) ReadySet(deps map[string][]string) []string {
	if s.IsTerminal() {
		return nil
	}

	var ready []string
	for nodeID, state := range s.Nodes {
		if state != NodeStatePending {
			continue
		}
		allDepsComplete := true
		for _, dep := range deps[nodeID] {
			if s.Nodes[dep] != NodeStateCompleted {
				allDepsComplete = false
				break
			}
		}
		if allDepsComplete {
			ready = append(ready, nodeID)
		}
	}

	// Deterministic ordering for replay safety
	sort.Strings(ready)
	return ready
}

// AllNodesTerminal returns true if every node is in a terminal state (completed, failed, or skipped).
func (s *SchedulerState) AllNodesTerminal() bool {
	for _, state := range s.Nodes {
		if state != NodeStateCompleted && state != NodeStateFailed && state != NodeStateSkipped {
			return false
		}
	}
	return true
}

// HasFailedNodes returns true if any node is in failed state.
func (s *SchedulerState) HasFailedNodes() bool {
	for _, state := range s.Nodes {
		if state == NodeStateFailed {
			return true
		}
	}
	return false
}

// RequeueInFlightNodes resets scheduled/running nodes to pending so they can be
// deterministically re-dispatched after process restart.
func (s *SchedulerState) RequeueInFlightNodes() int {
	requeued := 0
	for nodeID, state := range s.Nodes {
		if state == NodeStateScheduled || state == NodeStateRunning {
			s.Nodes[nodeID] = NodeStatePending
			requeued++
		}
	}
	if requeued > 0 {
		s.PendingActivities = make(map[string]string)
	}
	return requeued
}

// BuildDeps constructs the dependency map from a frozen snapshot's edges.
// Returns map[nodeID] → []dependencyNodeIDs.
func BuildDeps(snapshot *runtime.FrozenSnapshot) (map[string][]string, []string, error) {
	var canonical runtime.CanonicalWorkflow
	if err := json.Unmarshal(snapshot.Definition, &canonical); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal frozen snapshot: %w", err)
	}

	// Collect node IDs in order
	nodeIDs := make([]string, len(canonical.Nodes))
	for i, n := range canonical.Nodes {
		nodeIDs[i] = n.ID
	}

	// Build dependency map: for each edge Source→Target, Target depends on Source
	deps := make(map[string][]string, len(nodeIDs))
	for _, id := range nodeIDs {
		deps[id] = nil // ensure every node has an entry
	}
	for _, edge := range canonical.Edges {
		deps[edge.Target] = append(deps[edge.Target], edge.Source)
	}

	// Detect cycles: build forward adjacency (source → targets) from deps and run DFS.
	forward := make(map[string][]string, len(nodeIDs))
	for target, sources := range deps {
		for _, src := range sources {
			forward[src] = append(forward[src], target)
		}
	}
	if cycle := detectCycle(forward, nodeIDs); cycle != nil {
		return nil, nil, fmt.Errorf("cycle detected in workflow DAG: %s", strings.Join(cycle, " -> "))
	}

	return deps, nodeIDs, nil
}

// detectCycle uses DFS with white/gray/black coloring to find back edges.
// Returns the cycle path if found, or nil if the graph is acyclic.
func detectCycle(forward map[string][]string, nodeIDs []string) []string {
	const (
		white = 0 // unvisited
		gray  = 1 // in current DFS path
		black = 2 // fully explored
	)

	color := make(map[string]int, len(nodeIDs))
	parent := make(map[string]string, len(nodeIDs))

	for _, id := range nodeIDs {
		color[id] = white
	}

	var dfs func(node string) []string
	dfs = func(node string) []string {
		color[node] = gray
		for _, next := range forward[node] {
			if color[next] == gray {
				// Back edge found — reconstruct cycle path.
				path := []string{next, node}
				cur := node
				for cur != next {
					cur = parent[cur]
					path = append(path, cur)
				}
				// Reverse to get cycle in forward order.
				for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
					path[i], path[j] = path[j], path[i]
				}
				return path
			}
			if color[next] == white {
				parent[next] = node
				if cycle := dfs(next); cycle != nil {
					return cycle
				}
			}
		}
		color[node] = black
		return nil
	}

	// Sort nodeIDs for deterministic cycle reporting.
	sorted := make([]string, len(nodeIDs))
	copy(sorted, nodeIDs)
	sort.Strings(sorted)

	for _, id := range sorted {
		if color[id] == white {
			if cycle := dfs(id); cycle != nil {
				return cycle
			}
		}
	}
	return nil
}

// Replay reconstructs a SchedulerState from a sequence of history events.
// This is the core determinism guarantee: same history → same state.
func Replay(events []*runtime.HistoryEvent, nodeIDs []string) *SchedulerState {
	state := NewSchedulerState(nodeIDs)

	for _, event := range events {
		switch event.Type {
		case runtime.HistoryWorkflowStarted:
			// Initialize context if present in attributes
			if ctx, ok := event.Attributes["context"]; ok {
				if ctxMap, ok := ctx.(map[string]interface{}); ok {
					state.WorkflowContext = ctxMap
				}
			}

		case runtime.HistoryScheduleActivity:
			if event.NodeID != "" {
				state.Nodes[event.NodeID] = NodeStateScheduled
			}
			if event.ActivityID != "" {
				state.PendingActivities[event.ActivityID] = event.NodeID
			}
			// Track attempt number
			if attempt, ok := event.Attributes["attempt"]; ok {
				if attemptNum, ok := attempt.(float64); ok {
					state.ActivityAttempts[event.NodeID] = int(attemptNum)
				}
			}

		case runtime.HistoryActivityStarted:
			if event.NodeID != "" {
				state.Nodes[event.NodeID] = NodeStateRunning
			}

		case runtime.HistoryActivityCompleted:
			if event.NodeID != "" {
				state.Nodes[event.NodeID] = NodeStateCompleted
				state.AdaptiveFailures[event.NodeID] = 0
			}
			if event.ActivityID != "" {
				delete(state.PendingActivities, event.ActivityID)
			}
			// Store output
			if output, ok := event.Attributes["output"]; ok {
				state.NodeOutputs[event.NodeID] = output
			}

		case runtime.HistoryActivityFailed:
			// Note: a failed activity doesn't necessarily mean the node is failed.
			// The scheduler may retry. Only mark as failed if explicitly decided.
			if event.ActivityID != "" {
				delete(state.PendingActivities, event.ActivityID)
			}
			// Store error
			if errMsg, ok := event.Attributes["error"]; ok {
				if s, ok := errMsg.(string); ok {
					state.NodeErrors[event.NodeID] = s
				}
			}
			if code, ok := event.Attributes["error_code"].(string); ok {
				if strings.EqualFold(strings.TrimSpace(code), workflow.RetryCodeOutputTruncatedEmpty) {
					state.AdaptiveFailures[event.NodeID]++
				} else {
					state.AdaptiveFailures[event.NodeID] = 0
				}
			} else if event.NodeID != "" {
				state.AdaptiveFailures[event.NodeID] = 0
			}
			// If no retry is scheduled, the node remains in its current state
			// until a node_failed or re-schedule event arrives.

		case runtime.HistoryNodeFailed:
			if event.NodeID != "" {
				state.Nodes[event.NodeID] = NodeStateFailed
			}

		case runtime.HistoryNodeComplete:
			if event.NodeID != "" {
				state.Nodes[event.NodeID] = NodeStateCompleted
			}

		case runtime.HistoryWorkflowCompleted:
			state.Completed = true

		case runtime.HistoryWorkflowFailed:
			state.Failed = true

		case runtime.HistoryWorkflowCancelled:
			state.Cancelled = true

		case runtime.HistoryWorkflowTimedOut:
			state.Failed = true
		}
	}

	return state
}
