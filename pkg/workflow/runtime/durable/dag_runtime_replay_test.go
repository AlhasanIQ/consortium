package durable

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

func TestDAGRuntime_ReplaySeedsSkipReusedNodes(t *testing.T) {
	store := newTestStore(t)
	registry := runtime.NewActivityHandlerRegistry()
	handler := newScriptedLLMHandler(scriptedNode{success: true, output: "fresh-c"})
	registry.Register(handler)
	dagRuntime := NewDAGRuntime(store, registry)

	wf := &workflow.Workflow{
		ID:   "wf-replay-seed",
		Name: "Replay Seed Test",
		Nodes: []*workflow.Node{
			{ID: "a", Type: workflow.NodeTypePrompt, Model: "mock-model", Prompt: "A"},
			{ID: "b", Type: workflow.NodeTypePrompt, Model: "mock-model", Prompt: "B"},
			{ID: "c", Type: workflow.NodeTypePrompt, Model: "mock-model", Prompt: "C"},
		},
		Edges: []*workflow.Edge{
			{Source: "a", Target: "b"},
			{Source: "b", Target: "c"},
		},
	}
	snapshot := freezeWorkflowForDurableTest(t, wf)
	jobID := "job-replay-seed"
	createDurableJobForTest(t, store, jobID, wf, snapshot)

	err := dagRuntime.Start(context.Background(), &runtime.StartParams{
		Identity: &runtime.ExecutionIdentity{
			WorkflowExecutionID: jobID,
			RunID:               jobID,
			RunNumber:           1,
			DAGHash:             snapshot.DAGHash,
		},
		Snapshot: snapshot,
		Workflow: wf,
		Replay: &runtime.ReplayRequest{
			Mode: runtime.ReplayModeRequired,
			Plan: &runtime.ReplayPlan{
				BaseDAGHash:      "base-hash",
				CandidateDAGHash: snapshot.DAGHash,
				ReuseNodeIDs:     []string{"a", "b"},
				ExecuteNodeIDs:   []string{"c"},
			},
			SeedNodes: map[string]runtime.ReplaySeedNode{
				"a": {
					NodeID:       "a",
					Output:       "cached-a",
					TokensInput:  10,
					TokensOutput: 5,
					Cost:         0.001,
					SourceJobID:  "source-parent-job",
				},
				"b": {
					NodeID:       "b",
					Output:       "cached-b",
					TokensInput:  8,
					TokensOutput: 4,
					Cost:         0.0012,
					SourceJobID:  "source-parent-job",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("runtime start failed: %v", err)
	}

	if handler.CallCount() != 1 {
		t.Fatalf("expected only one handler call for node c, got %d", handler.CallCount())
	}

	nodes, err := store.GetWorkflowNodes(jobID)
	if err != nil {
		t.Fatalf("GetWorkflowNodes failed: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("expected 3 workflow nodes, got %d", len(nodes))
	}

	nodeByID := map[string]storage.WorkflowNode{}
	for _, node := range nodes {
		nodeByID[node.NodeID] = node
	}
	if nodeByID["a"].Output != "cached-a" {
		t.Fatalf("node a output = %q, want %q", nodeByID["a"].Output, "cached-a")
	}
	if nodeByID["b"].Output != "cached-b" {
		t.Fatalf("node b output = %q, want %q", nodeByID["b"].Output, "cached-b")
	}
	if nodeByID["c"].Output != "fresh-c" {
		t.Fatalf("node c output = %q, want %q", nodeByID["c"].Output, "fresh-c")
	}

	// Replayed nodes must have zero cost/tokens.
	for _, replayedID := range []string{"a", "b"} {
		n := nodeByID[replayedID]
		if n.Cost != 0 {
			t.Errorf("replayed node %s cost = %f, want 0", replayedID, n.Cost)
		}
		if n.TokensInput != 0 {
			t.Errorf("replayed node %s tokens_input = %d, want 0", replayedID, n.TokensInput)
		}
		if n.TokensOutput != 0 {
			t.Errorf("replayed node %s tokens_output = %d, want 0", replayedID, n.TokensOutput)
		}

		// Metadata must contain replayed flag.
		var meta map[string]interface{}
		if err := json.Unmarshal([]byte(n.Metadata), &meta); err != nil {
			t.Fatalf("replayed node %s metadata parse error: %v", replayedID, err)
		}
		if replayed, _ := meta["replayed"].(bool); !replayed {
			t.Errorf("replayed node %s metadata missing replayed=true, got %v", replayedID, meta)
		}
		if sourceJob, _ := meta["source_job_id"].(string); sourceJob != "source-parent-job" {
			t.Errorf("replayed node %s metadata source_job_id = %q, want %q", replayedID, sourceJob, "source-parent-job")
		}
	}

	// Non-replayed node (c) must NOT have replayed metadata.
	freshNode := nodeByID["c"]
	if freshNode.Metadata != "" {
		var meta map[string]interface{}
		if err := json.Unmarshal([]byte(freshNode.Metadata), &meta); err == nil {
			if _, hasReplayed := meta["replayed"]; hasReplayed {
				t.Errorf("fresh node c should not have replayed metadata")
			}
		}
	}
}

func TestDAGRuntime_ReplayRequiredCandidateHashMismatchFails(t *testing.T) {
	store := newTestStore(t)
	registry := runtime.NewActivityHandlerRegistry()
	registry.Register(newScriptedLLMHandler(scriptedNode{success: true, output: "ok"}))
	dagRuntime := NewDAGRuntime(store, registry)

	wf := &workflow.Workflow{
		ID:   "wf-replay-required-mismatch",
		Name: "Replay Required Mismatch",
		Nodes: []*workflow.Node{
			{ID: "n1", Type: workflow.NodeTypePrompt, Model: "mock-model", Prompt: "hello"},
		},
	}
	snapshot := freezeWorkflowForDurableTest(t, wf)
	jobID := "job-replay-required-mismatch"
	createDurableJobForTest(t, store, jobID, wf, snapshot)

	err := dagRuntime.Start(context.Background(), &runtime.StartParams{
		Identity: &runtime.ExecutionIdentity{
			WorkflowExecutionID: jobID,
			RunID:               jobID,
			RunNumber:           1,
			DAGHash:             snapshot.DAGHash,
		},
		Snapshot: snapshot,
		Workflow: wf,
		Replay: &runtime.ReplayRequest{
			Mode: runtime.ReplayModeRequired,
			Plan: &runtime.ReplayPlan{
				CandidateDAGHash: "wrong-dag-hash",
			},
			SeedNodes: map[string]runtime.ReplaySeedNode{
				"n1": {NodeID: "n1", Output: "cached"},
			},
		},
	})
	if err == nil {
		t.Fatal("expected replay required hash mismatch to fail")
	}
}
