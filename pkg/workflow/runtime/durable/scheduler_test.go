package durable

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

// Helper to build deps map from edges
func depsFromEdges(nodeIDs []string, edges []runtime.CanonicalEdge) map[string][]string {
	deps := make(map[string][]string, len(nodeIDs))
	for _, id := range nodeIDs {
		deps[id] = nil
	}
	for _, e := range edges {
		deps[e.Target] = append(deps[e.Target], e.Source)
	}
	return deps
}

func TestReadySet_LinearWorkflow(t *testing.T) {
	// A → B → C (sequential)
	nodeIDs := []string{"A", "B", "C"}
	deps := depsFromEdges(nodeIDs, []runtime.CanonicalEdge{
		{Source: "A", Target: "B"},
		{Source: "B", Target: "C"},
	})

	state := NewSchedulerState(nodeIDs)

	// Initially only A is ready (no deps)
	ready := state.ReadySet(deps)
	if len(ready) != 1 || ready[0] != "A" {
		t.Errorf("Expected [A], got %v", ready)
	}

	// After A completes, B becomes ready
	state.Nodes["A"] = NodeStateCompleted
	ready = state.ReadySet(deps)
	if len(ready) != 1 || ready[0] != "B" {
		t.Errorf("Expected [B], got %v", ready)
	}

	// After B completes, C becomes ready
	state.Nodes["B"] = NodeStateCompleted
	ready = state.ReadySet(deps)
	if len(ready) != 1 || ready[0] != "C" {
		t.Errorf("Expected [C], got %v", ready)
	}

	// After C completes, nothing is ready
	state.Nodes["C"] = NodeStateCompleted
	ready = state.ReadySet(deps)
	if len(ready) != 0 {
		t.Errorf("Expected empty, got %v", ready)
	}
}

func TestReadyQueue_AdvancesChainIncrementally(t *testing.T) {
	nodeIDs := []string{"A", "B", "C"}
	deps := map[string][]string{
		"A": nil,
		"B": {"A"},
		"C": {"B"},
	}
	state := NewSchedulerState(nodeIDs)
	queue := newReadyQueue(state, deps, nodeIDs)

	for _, want := range nodeIDs {
		ready := queue.ReadySet()
		if len(ready) != 1 || ready[0] != want {
			t.Fatalf("ready = %v, want [%s]", ready, want)
		}
		state.Nodes[want] = NodeStateCompleted
	}
	if ready := queue.ReadySet(); len(ready) != 0 {
		t.Fatalf("ready after completion = %v, want empty", ready)
	}
}

func TestReadyQueue_RetryStaysReady(t *testing.T) {
	nodeIDs := []string{"A", "B"}
	deps := map[string][]string{"A": nil, "B": {"A"}}
	state := NewSchedulerState(nodeIDs)
	queue := newReadyQueue(state, deps, nodeIDs)

	if ready := queue.ReadySet(); len(ready) != 1 || ready[0] != "A" {
		t.Fatalf("initial ready = %v, want [A]", ready)
	}
	state.Nodes["A"] = NodeStateRunning
	state.Nodes["A"] = NodeStatePending
	if ready := queue.ReadySet(); len(ready) != 1 || ready[0] != "A" {
		t.Fatalf("retry ready = %v, want [A]", ready)
	}
	if state.Nodes["B"] != NodeStatePending {
		t.Fatalf("dependent state = %s, want pending", state.Nodes["B"])
	}
}

func TestReadyQueue_InitializesFromReplayedCompletion(t *testing.T) {
	nodeIDs := []string{"A", "B", "C"}
	deps := map[string][]string{"A": nil, "B": {"A"}, "C": {"B"}}
	state := NewSchedulerState(nodeIDs)
	state.Nodes["A"] = NodeStateCompleted
	queue := newReadyQueue(state, deps, nodeIDs)

	if ready := queue.ReadySet(); len(ready) != 1 || ready[0] != "B" {
		t.Fatalf("ready after replay = %v, want [B]", ready)
	}
}

func TestReadyQueue_MatchesReadySetAcrossDeterministicRandomDAGs(t *testing.T) {
	rng := rand.New(rand.NewSource(42))

	for graph := 0; graph < 100; graph++ {
		nodeCount := 1 + rng.Intn(40)
		nodeIDs := make([]string, nodeCount)
		deps := make(map[string][]string, nodeCount)
		for i := 0; i < nodeCount; i++ {
			nodeIDs[i] = fmt.Sprintf("node-%02d", i)
			for predecessor := 0; predecessor < i; predecessor++ {
				if rng.Intn(7) == 0 {
					deps[nodeIDs[i]] = append(deps[nodeIDs[i]], nodeIDs[predecessor])
				}
			}
		}

		state := NewSchedulerState(nodeIDs)
		queue := newReadyQueue(state, deps, nodeIDs)
		retried := false

		for step := 0; step <= nodeCount; step++ {
			want := state.ReadySet(deps)
			got := queue.ReadySet()
			if !slices.Equal(got, want) {
				t.Fatalf("graph %d step %d ready = %v, want %v", graph, step, got, want)
			}
			if len(got) == 0 {
				if !state.AllNodesTerminal() {
					t.Fatalf("graph %d made no progress with non-terminal state: %+v", graph, state.Nodes)
				}
				break
			}

			// Exercise retry semantics once per graph: a pending ready node must
			// remain ready without advancing any of its dependents.
			if !retried {
				retried = true
				wantRetry := state.ReadySet(deps)
				gotRetry := queue.ReadySet()
				if !slices.Equal(gotRetry, wantRetry) {
					t.Fatalf("graph %d retry ready = %v, want %v", graph, gotRetry, wantRetry)
				}
			}

			for _, nodeID := range got {
				state.Nodes[nodeID] = NodeStateCompleted
			}
		}
	}
}

func TestReadySet_DiamondDAG(t *testing.T) {
	// A → B, A → C, B → D, C → D (diamond)
	nodeIDs := []string{"A", "B", "C", "D"}
	deps := depsFromEdges(nodeIDs, []runtime.CanonicalEdge{
		{Source: "A", Target: "B"},
		{Source: "A", Target: "C"},
		{Source: "B", Target: "D"},
		{Source: "C", Target: "D"},
	})

	state := NewSchedulerState(nodeIDs)

	// Initially only A
	ready := state.ReadySet(deps)
	if len(ready) != 1 || ready[0] != "A" {
		t.Errorf("Expected [A], got %v", ready)
	}

	// After A: B and C ready (sorted)
	state.Nodes["A"] = NodeStateCompleted
	ready = state.ReadySet(deps)
	if len(ready) != 2 || ready[0] != "B" || ready[1] != "C" {
		t.Errorf("Expected [B, C], got %v", ready)
	}

	// After B only: D not ready yet (C still pending)
	state.Nodes["B"] = NodeStateCompleted
	ready = state.ReadySet(deps)
	if len(ready) != 1 || ready[0] != "C" {
		t.Errorf("Expected [C], got %v", ready)
	}

	// After both B and C: D ready
	state.Nodes["C"] = NodeStateCompleted
	ready = state.ReadySet(deps)
	if len(ready) != 1 || ready[0] != "D" {
		t.Errorf("Expected [D], got %v", ready)
	}
}

func TestReadySet_ParallelNoDeps(t *testing.T) {
	// All independent (no edges)
	nodeIDs := []string{"X", "Y", "Z"}
	deps := depsFromEdges(nodeIDs, nil)

	state := NewSchedulerState(nodeIDs)
	ready := state.ReadySet(deps)
	if len(ready) != 3 || ready[0] != "X" || ready[1] != "Y" || ready[2] != "Z" {
		t.Errorf("Expected [X, Y, Z], got %v", ready)
	}
}

func TestReadySet_SingleNode(t *testing.T) {
	nodeIDs := []string{"solo"}
	deps := depsFromEdges(nodeIDs, nil)

	state := NewSchedulerState(nodeIDs)
	ready := state.ReadySet(deps)
	if len(ready) != 1 || ready[0] != "solo" {
		t.Errorf("Expected [solo], got %v", ready)
	}

	state.Nodes["solo"] = NodeStateCompleted
	ready = state.ReadySet(deps)
	if len(ready) != 0 {
		t.Errorf("Expected empty, got %v", ready)
	}
}

func TestReadySet_TerminalStateReturnsNil(t *testing.T) {
	nodeIDs := []string{"A", "B"}
	deps := depsFromEdges(nodeIDs, nil)

	state := NewSchedulerState(nodeIDs)
	state.Completed = true
	ready := state.ReadySet(deps)
	if ready != nil {
		t.Errorf("Expected nil for terminal state, got %v", ready)
	}
}

func TestReadySet_Deterministic(t *testing.T) {
	// Run ReadySet multiple times — must produce same ordering
	nodeIDs := []string{"Z", "A", "M", "B"}
	deps := depsFromEdges(nodeIDs, nil)

	state := NewSchedulerState(nodeIDs)
	for i := 0; i < 10; i++ {
		ready := state.ReadySet(deps)
		expected := []string{"A", "B", "M", "Z"}
		if len(ready) != len(expected) {
			t.Fatalf("Iteration %d: expected %v, got %v", i, expected, ready)
		}
		for j, id := range ready {
			if id != expected[j] {
				t.Fatalf("Iteration %d: expected %v, got %v", i, expected, ready)
			}
		}
	}
}

func TestReplay_EmptyHistory(t *testing.T) {
	state := Replay(nil, []string{"A", "B"})
	if state.IsTerminal() {
		t.Error("Empty history should not be terminal")
	}
	if state.Nodes["A"] != NodeStatePending || state.Nodes["B"] != NodeStatePending {
		t.Errorf("All nodes should be pending: %v", state.Nodes)
	}
}

func TestReplay_CompletedWorkflow(t *testing.T) {
	now := time.Now()
	events := []*runtime.HistoryEvent{
		{Sequence: 1, Type: runtime.HistoryWorkflowStarted, Timestamp: now},
		{Sequence: 2, Type: runtime.HistoryScheduleActivity, NodeID: "A", ActivityID: "act-1", Timestamp: now,
			Attributes: map[string]interface{}{"attempt": float64(1)}},
		{Sequence: 3, Type: runtime.HistoryActivityStarted, NodeID: "A", ActivityID: "act-1", Timestamp: now},
		{Sequence: 4, Type: runtime.HistoryActivityCompleted, NodeID: "A", ActivityID: "act-1", Timestamp: now,
			Attributes: map[string]interface{}{"output": "result-A"}},
		{Sequence: 5, Type: runtime.HistoryScheduleActivity, NodeID: "B", ActivityID: "act-2", Timestamp: now,
			Attributes: map[string]interface{}{"attempt": float64(1)}},
		{Sequence: 6, Type: runtime.HistoryActivityStarted, NodeID: "B", ActivityID: "act-2", Timestamp: now},
		{Sequence: 7, Type: runtime.HistoryActivityCompleted, NodeID: "B", ActivityID: "act-2", Timestamp: now,
			Attributes: map[string]interface{}{"output": "result-B"}},
		{Sequence: 8, Type: runtime.HistoryWorkflowCompleted, Timestamp: now},
	}

	state := Replay(events, []string{"A", "B"})
	if !state.Completed {
		t.Error("Expected completed=true")
	}
	if state.Nodes["A"] != NodeStateCompleted || state.Nodes["B"] != NodeStateCompleted {
		t.Errorf("Expected both completed: %v", state.Nodes)
	}
	if state.NodeOutputs["A"] != "result-A" {
		t.Errorf("Expected output result-A, got %v", state.NodeOutputs["A"])
	}
	if len(state.PendingActivities) != 0 {
		t.Errorf("Expected no pending activities: %v", state.PendingActivities)
	}
}

func TestReplay_Determinism(t *testing.T) {
	now := time.Now()
	events := []*runtime.HistoryEvent{
		{Sequence: 1, Type: runtime.HistoryWorkflowStarted, Timestamp: now},
		{Sequence: 2, Type: runtime.HistoryScheduleActivity, NodeID: "A", ActivityID: "a1", Timestamp: now,
			Attributes: map[string]interface{}{"attempt": float64(1)}},
		{Sequence: 3, Type: runtime.HistoryActivityCompleted, NodeID: "A", ActivityID: "a1", Timestamp: now,
			Attributes: map[string]interface{}{"output": "ok"}},
	}

	state1 := Replay(events, []string{"A", "B"})
	state2 := Replay(events, []string{"A", "B"})

	// Same history → same state
	s1, _ := json.Marshal(state1)
	s2, _ := json.Marshal(state2)
	if string(s1) != string(s2) {
		t.Errorf("Replay not deterministic:\n  state1: %s\n  state2: %s", s1, s2)
	}
}

func TestReplay_FailedWorkflow(t *testing.T) {
	now := time.Now()
	events := []*runtime.HistoryEvent{
		{Sequence: 1, Type: runtime.HistoryWorkflowStarted, Timestamp: now},
		{Sequence: 2, Type: runtime.HistoryScheduleActivity, NodeID: "A", ActivityID: "a1", Timestamp: now},
		{Sequence: 3, Type: runtime.HistoryActivityFailed, NodeID: "A", ActivityID: "a1", Timestamp: now,
			Attributes: map[string]interface{}{"error": "API timeout"}},
		{Sequence: 4, Type: runtime.HistoryNodeFailed, NodeID: "A", Timestamp: now},
		{Sequence: 5, Type: runtime.HistoryWorkflowFailed, Timestamp: now},
	}

	state := Replay(events, []string{"A", "B"})
	if !state.Failed {
		t.Error("Expected failed=true")
	}
	if state.Nodes["A"] != NodeStateFailed {
		t.Errorf("Expected A failed, got %s", state.Nodes["A"])
	}
	if state.NodeErrors["A"] != "API timeout" {
		t.Errorf("Expected error 'API timeout', got %s", state.NodeErrors["A"])
	}
}

func TestReplay_CancelledWorkflow(t *testing.T) {
	events := []*runtime.HistoryEvent{
		{Sequence: 1, Type: runtime.HistoryWorkflowStarted, Timestamp: time.Now()},
		{Sequence: 2, Type: runtime.HistoryWorkflowCancelled, Timestamp: time.Now()},
	}

	state := Replay(events, []string{"A"})
	if !state.Cancelled {
		t.Error("Expected cancelled=true")
	}
}

func TestReplay_ActivityRetry(t *testing.T) {
	now := time.Now()
	events := []*runtime.HistoryEvent{
		{Sequence: 1, Type: runtime.HistoryWorkflowStarted, Timestamp: now},
		// First attempt fails
		{Sequence: 2, Type: runtime.HistoryScheduleActivity, NodeID: "A", ActivityID: "a1", Timestamp: now,
			Attributes: map[string]interface{}{"attempt": float64(1)}},
		{Sequence: 3, Type: runtime.HistoryActivityFailed, NodeID: "A", ActivityID: "a1", Timestamp: now,
			Attributes: map[string]interface{}{"error": "transient"}},
		// Second attempt succeeds
		{Sequence: 4, Type: runtime.HistoryScheduleActivity, NodeID: "A", ActivityID: "a2", Timestamp: now,
			Attributes: map[string]interface{}{"attempt": float64(2)}},
		{Sequence: 5, Type: runtime.HistoryActivityCompleted, NodeID: "A", ActivityID: "a2", Timestamp: now,
			Attributes: map[string]interface{}{"output": "success"}},
		{Sequence: 6, Type: runtime.HistoryWorkflowCompleted, Timestamp: now},
	}

	state := Replay(events, []string{"A"})
	if !state.Completed {
		t.Error("Expected completed")
	}
	if state.Nodes["A"] != NodeStateCompleted {
		t.Errorf("Expected A completed, got %s", state.Nodes["A"])
	}
	if state.ActivityAttempts["A"] != 2 {
		t.Errorf("Expected attempt 2, got %d", state.ActivityAttempts["A"])
	}
}

func TestBuildDeps(t *testing.T) {
	// Create a frozen snapshot with known structure
	nodes := []runtime.NodeForFreeze{
		{ID: "n1", Type: "prompt"},
		{ID: "n2", Type: "prompt"},
		{ID: "n3", Type: "result"},
	}
	edges := []runtime.EdgeForFreeze{
		{Source: "n1", Target: "n3"},
		{Source: "n2", Target: "n3"},
	}
	snapshot, err := runtime.FreezeWorkflow("wf", "test", "", nodes, edges, nil, nil)
	if err != nil {
		t.Fatalf("FreezeWorkflow failed: %v", err)
	}

	deps, nodeIDs, err := BuildDeps(snapshot)
	if err != nil {
		t.Fatalf("BuildDeps failed: %v", err)
	}

	if len(nodeIDs) != 3 {
		t.Fatalf("Expected 3 nodes, got %d", len(nodeIDs))
	}

	// n1 and n2 have no deps, n3 depends on both
	if len(deps["n1"]) != 0 {
		t.Errorf("n1 should have no deps: %v", deps["n1"])
	}
	if len(deps["n2"]) != 0 {
		t.Errorf("n2 should have no deps: %v", deps["n2"])
	}
	if len(deps["n3"]) != 2 {
		t.Errorf("n3 should have 2 deps: %v", deps["n3"])
	}
}

func TestBuildDeps_NoEdges_LinearDepsInjected(t *testing.T) {
	// When frozen with no edges, FreezeWorkflow injects linear deps
	nodes := []runtime.NodeForFreeze{
		{ID: "s1", Type: "prompt"},
		{ID: "s2", Type: "prompt"},
		{ID: "s3", Type: "prompt"},
	}
	snapshot, err := runtime.FreezeWorkflow("wf", "test", "", nodes, nil, nil, nil)
	if err != nil {
		t.Fatalf("FreezeWorkflow failed: %v", err)
	}

	deps, nodeIDs, err := BuildDeps(snapshot)
	if err != nil {
		t.Fatalf("BuildDeps failed: %v", err)
	}

	// Should be s1 → s2 → s3
	if len(deps["s1"]) != 0 {
		t.Errorf("s1 should have no deps: %v", deps["s1"])
	}
	if len(deps["s2"]) != 1 || deps["s2"][0] != "s1" {
		t.Errorf("s2 should depend on s1: %v", deps["s2"])
	}
	if len(deps["s3"]) != 1 || deps["s3"][0] != "s2" {
		t.Errorf("s3 should depend on s2: %v", deps["s3"])
	}

	// Verify sequential scheduling: only one ready at a time
	state := NewSchedulerState(nodeIDs)
	ready := state.ReadySet(deps)
	if len(ready) != 1 || ready[0] != "s1" {
		t.Errorf("Expected [s1], got %v", ready)
	}

	state.Nodes["s1"] = NodeStateCompleted
	ready = state.ReadySet(deps)
	if len(ready) != 1 || ready[0] != "s2" {
		t.Errorf("Expected [s2], got %v", ready)
	}
}

func TestBuildDeps_RejectsCyclesBeforeScheduling(t *testing.T) {
	snapshot, err := runtime.FreezeWorkflow(
		"wf-cycle",
		"Cycle",
		"",
		[]runtime.NodeForFreeze{
			{ID: "a", Type: "prompt"},
			{ID: "b", Type: "prompt"},
		},
		[]runtime.EdgeForFreeze{
			{Source: "a", Target: "b"},
			{Source: "b", Target: "a"},
		},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("FreezeWorkflow failed: %v", err)
	}

	_, _, err = BuildDeps(snapshot)
	if err == nil {
		t.Fatal("expected cyclic snapshot to be rejected")
	}
	if !strings.Contains(err.Error(), "cycle detected") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestAllNodesTerminal(t *testing.T) {
	state := NewSchedulerState([]string{"A", "B", "C"})
	if state.AllNodesTerminal() {
		t.Error("Not all nodes should be terminal initially")
	}

	state.Nodes["A"] = NodeStateCompleted
	state.Nodes["B"] = NodeStateFailed
	state.Nodes["C"] = NodeStateSkipped
	if !state.AllNodesTerminal() {
		t.Error("All nodes should be terminal")
	}
}

func TestHasFailedNodes(t *testing.T) {
	state := NewSchedulerState([]string{"A", "B"})
	if state.HasFailedNodes() {
		t.Error("No nodes should be failed initially")
	}

	state.Nodes["A"] = NodeStateFailed
	if !state.HasFailedNodes() {
		t.Error("Should detect failed node")
	}
}
