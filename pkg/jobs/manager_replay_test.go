package jobs

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	workflowruntime "github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

func TestManagerBuildReplayRequest(t *testing.T) {
	manager, _ := setupManagerTest(t)
	startWorkers(t, manager)

	baseWorkflow := &workflow.Workflow{
		ID:   "wf-replay-base",
		Name: "Replay Base",
		Nodes: []*workflow.Node{
			strictPromptNode("n1", "mock-model", "step 1"),
			strictPromptNode("n2", "mock-model", "step 2"),
		},
		Edges: []*workflow.Edge{
			{Source: "n1", Target: "n2"},
		},
	}
	baseResult, err := manager.ExecuteWorkflow(context.Background(), baseWorkflow)
	if err != nil {
		t.Fatalf("ExecuteWorkflow(base) failed: %v", err)
	}
	if !baseResult.Success {
		t.Fatalf("expected base workflow success, got %+v", baseResult)
	}

	candidate := &workflow.Workflow{
		ID:   "wf-replay-base",
		Name: "Replay Candidate",
		Nodes: []*workflow.Node{
			strictPromptNode("n1", "mock-model", "step 1"),
			strictPromptNode("n2", "mock-model", "step 2 changed"),
		},
		Edges: []*workflow.Edge{
			{Source: "n1", Target: "n2"},
		},
	}

	replayReq, err := manager.BuildReplayRequest(context.Background(), baseResult.JobID, candidate, workflowruntime.ReplayModeBestEffort, nil)
	if err != nil {
		t.Fatalf("BuildReplayRequest failed: %v", err)
	}
	if replayReq == nil || replayReq.Plan == nil {
		t.Fatal("expected replay request with plan")
	}
	if len(replayReq.Plan.ReuseNodeIDs) != 1 || replayReq.Plan.ReuseNodeIDs[0] != "n1" {
		t.Fatalf("unexpected reuse nodes: %v", replayReq.Plan.ReuseNodeIDs)
	}
	if len(replayReq.Plan.ExecuteNodeIDs) != 1 || replayReq.Plan.ExecuteNodeIDs[0] != "n2" {
		t.Fatalf("unexpected execute nodes: %v", replayReq.Plan.ExecuteNodeIDs)
	}
	if _, ok := replayReq.SeedNodes["n1"]; !ok {
		t.Fatalf("expected seed for n1, got keys: %v", replayReq.SeedNodes)
	}
}

func TestManagerBuildReplayRequestCompilesWorkflowRefCandidate(t *testing.T) {
	manager, store := setupManagerTest(t)

	child := &workflow.Workflow{
		ID:    "aggregation-fixture-collect",
		Name:  "Aggregation Fixture Collect",
		Nodes: []*workflow.Node{strictPromptNode("join", "mock-model", "Collect")},
	}
	childData, err := json.Marshal(child)
	if err != nil {
		t.Fatalf("marshal child workflow: %v", err)
	}
	if err := store.CreateWorkflow(&storage.WorkflowDefinition{
		ID:         child.ID,
		Name:       child.Name,
		Definition: string(childData),
	}); err != nil {
		t.Fatalf("CreateWorkflow child: %v", err)
	}

	baseWorkflow := &workflow.Workflow{
		ID:   "parent-with-ref",
		Name: "Parent With Ref",
		Nodes: []*workflow.Node{{
			ID:            "agg",
			Type:          workflow.NodeTypeWorkflowRef,
			WorkflowRefID: "aggregation-fixture-collect",
		}},
	}
	baseResult, err := manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
		Workflow:    baseWorkflow,
		ForceNewRun: true,
	})
	if err != nil {
		t.Fatalf("SubmitWorkflow(base) failed: %v", err)
	}

	candidate := &workflow.Workflow{
		ID:   "parent-with-ref",
		Name: "Parent With Ref",
		Nodes: []*workflow.Node{{
			ID:            "agg",
			Type:          workflow.NodeTypeWorkflowRef,
			WorkflowRefID: "aggregation-fixture-collect",
		}},
	}
	replayReq, err := manager.BuildReplayRequest(context.Background(), baseResult.JobID, candidate, workflowruntime.ReplayModeBestEffort, nil)
	if err != nil {
		t.Fatalf("BuildReplayRequest failed: %v", err)
	}
	if replayReq == nil || replayReq.Plan == nil {
		t.Fatal("expected replay request with plan")
	}
	if len(replayReq.Plan.ReuseNodeIDs) != 1 || replayReq.Plan.ReuseNodeIDs[0] != "agg--join" {
		t.Fatalf("expected compiled node reuse, got %+v", replayReq.Plan)
	}
	if len(replayReq.Plan.ExecuteNodeIDs) != 0 {
		t.Fatalf("expected no execute nodes for unchanged compiled candidate, got %+v", replayReq.Plan.ExecuteNodeIDs)
	}
}

func TestExecuteWorkflowWithReplayPersistsAndAppliesSeeds(t *testing.T) {
	manager, store := setupManagerTest(t)
	startWorkers(t, manager)

	baseWorkflow := &workflow.Workflow{
		ID:   "wf-replay-apply",
		Name: "Replay Apply Base",
		Nodes: []*workflow.Node{
			strictPromptNode("n1", "mock-model", "first"),
			strictPromptNode("n2", "mock-model", "second"),
		},
		Edges: []*workflow.Edge{
			{Source: "n1", Target: "n2"},
		},
	}
	baseResult, err := manager.ExecuteWorkflow(context.Background(), baseWorkflow)
	if err != nil {
		t.Fatalf("ExecuteWorkflow(base) failed: %v", err)
	}
	if !baseResult.Success {
		t.Fatalf("expected base workflow success, got %+v", baseResult)
	}

	candidate := &workflow.Workflow{
		ID:   "wf-replay-apply",
		Name: "Replay Apply Candidate",
		Nodes: []*workflow.Node{
			strictPromptNode("n1", "mock-model", "first"),
			strictPromptNode("n2", "mock-model", "second changed"),
		},
		Edges: []*workflow.Edge{
			{Source: "n1", Target: "n2"},
		},
	}
	replayReq, err := manager.BuildReplayRequest(context.Background(), baseResult.JobID, candidate, workflowruntime.ReplayModeRequired, nil)
	if err != nil {
		t.Fatalf("BuildReplayRequest failed: %v", err)
	}

	result, err := manager.ExecuteWorkflowWithReplay(context.Background(), candidate, replayReq)
	if err != nil {
		t.Fatalf("ExecuteWorkflowWithReplay failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected replay workflow success, got %+v", result)
	}

	history, err := store.GetHistoryEvents(context.Background(), result.JobID)
	if err != nil {
		t.Fatalf("GetHistoryEvents failed: %v", err)
	}
	foundReplayedCompletion := false
	for _, evt := range history {
		if evt.NodeID != "n1" {
			continue
		}
		if evt.Type == string(workflowruntime.HistoryActivityCompleted) {
			if replayed, ok := evt.Attributes["replayed"].(bool); ok && replayed {
				foundReplayedCompletion = true
				break
			}
		}
	}
	if !foundReplayedCompletion {
		t.Fatalf("expected replayed activity_completed event for n1 in run %s", result.JobID)
	}
}
