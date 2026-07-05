package jobs

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	workflowruntime "github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

func TestSubmitWorkflow_FailsEarlyOnInvalidAggregationConfig(t *testing.T) {
	manager, _ := setupManagerTest(t)

	wf := &workflow.Workflow{
		ID:   "wf-invalid-synthesis-config",
		Name: "Invalid Synthesis Config",
		Nodes: []*workflow.Node{
			strictPromptNode("agent-a", "mock-model", "Solve the question"),
			{
				ID:                "result",
				Type:              workflow.NodeTypeResult,
				AggregationMethod: workflow.AggMethodSynthesis,
				AggregationConfig: map[string]interface{}{
					"model":         "mock-model",
					"system_prompt": "Synthesize the best final answer.",
					// Missing prompt on purpose.
					"temperature": 0.0,
					"max_tokens":  -1,
				},
				RetryPolicy: workflow.NoRetryPolicy(),
				Metadata: map[string]interface{}{
					"input_ids": []interface{}{"agent-a"},
				},
			},
		},
		Edges: []*workflow.Edge{
			{ID: "edge-agent-result", Source: "agent-a", Target: "result"},
		},
	}

	_, err := manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
		Workflow:    wf,
		ForceNewRun: true,
	})
	if err == nil {
		t.Fatal("expected validation error for missing synthesis prompt")
	}
	if !IsWorkflowSubmitValidationError(err) {
		t.Fatalf("expected typed workflow submit validation error, got: %T %v", err, err)
	}
	if !strings.Contains(err.Error(), "workflow validation failed") {
		t.Fatalf("expected workflow validation failure, got: %v", err)
	}
	if !strings.Contains(err.Error(), "synthesis requires prompt") {
		t.Fatalf("expected synthesis prompt validation failure, got: %v", err)
	}
}

func TestSubmitWorkflow_CompilesWorkflowRefBeforeFreeze(t *testing.T) {
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

	parent := &workflow.Workflow{
		ID:   "parent-with-ref",
		Name: "Parent With Ref",
		Nodes: []*workflow.Node{{
			ID:            "agg",
			Type:          workflow.NodeTypeWorkflowRef,
			WorkflowRefID: "aggregation-fixture-collect",
		}},
	}
	resp, err := manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
		Workflow:    parent,
		ForceNewRun: true,
	})
	if err != nil {
		t.Fatalf("SubmitWorkflow failed: %v", err)
	}

	job, err := store.GetExecution(resp.JobID)
	if err != nil {
		t.Fatalf("GetExecution: %v", err)
	}
	var frozen workflowruntime.CanonicalWorkflow
	if err := json.Unmarshal([]byte(job.DAGSnapshot), &frozen); err != nil {
		t.Fatalf("unmarshal dag snapshot: %v", err)
	}
	if len(frozen.Nodes) != 1 {
		t.Fatalf("frozen nodes = %+v", frozen.Nodes)
	}
	if frozen.Nodes[0].Type == string(workflow.NodeTypeWorkflowRef) {
		t.Fatalf("workflow_ref reached frozen snapshot: %+v", frozen.Nodes[0])
	}
	if frozen.Nodes[0].ID != "agg--join" {
		t.Fatalf("expanded node ID = %q", frozen.Nodes[0].ID)
	}
	if frozen.Nodes[0].Metadata["source_workflow_id"] != "aggregation-fixture-collect" {
		t.Fatalf("missing source metadata: %+v", frozen.Nodes[0].Metadata)
	}
	if frozen.Nodes[0].Metadata["source_workflow_hash"] == "" {
		t.Fatalf("missing source hash: %+v", frozen.Nodes[0].Metadata)
	}
}

func TestSubmitWorkflow_UnresolvedWorkflowRefFailsBeforeJobCreation(t *testing.T) {
	manager, store := setupManagerTest(t)

	parent := &workflow.Workflow{
		ID:   "parent-with-missing-ref",
		Name: "Parent With Missing Ref",
		Nodes: []*workflow.Node{{
			ID:            "agg",
			Type:          workflow.NodeTypeWorkflowRef,
			WorkflowRefID: "missing-workflow",
		}},
	}
	_, err := manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
		Workflow:    parent,
		ForceNewRun: true,
	})
	if err == nil {
		t.Fatal("expected unresolved workflow_ref submit error")
	}
	if !IsWorkflowSubmitValidationError(err) {
		t.Fatalf("expected typed workflow submit validation error, got: %T %v", err, err)
	}
	if !strings.Contains(err.Error(), "missing-workflow") {
		t.Fatalf("expected missing ref in error, got %v", err)
	}
	jobs, listErr := store.ListExecutions(10)
	if listErr != nil {
		t.Fatalf("ListExecutions: %v", listErr)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected no job rows after unresolved workflow_ref, got %+v", jobs)
	}
}
