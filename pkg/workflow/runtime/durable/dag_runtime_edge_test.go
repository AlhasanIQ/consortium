package durable

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

func TestDAGRuntime_Start_ValidatesRequiredFields(t *testing.T) {
	store := newTestStore(t)
	dagRuntime := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())

	wf := &workflow.Workflow{
		ID:   "wf-required-fields",
		Name: "Required Fields",
		Nodes: []*workflow.Node{
			{ID: "n1", Type: workflow.NodeTypePrompt, Model: "mock-model", Prompt: "hello"},
		},
	}
	snapshot := freezeWorkflowForDurableTest(t, wf)
	identity := &runtime.ExecutionIdentity{
		WorkflowExecutionID: "exec-1",
		RunID:               "run-1",
		RunNumber:           1,
		DAGHash:             snapshot.DAGHash,
	}

	tests := []struct {
		name   string
		params *runtime.StartParams
	}{
		{
			name: "missing identity",
			params: &runtime.StartParams{
				Snapshot: snapshot,
				Workflow: wf,
			},
		},
		{
			name: "missing snapshot",
			params: &runtime.StartParams{
				Identity: identity,
				Workflow: wf,
			},
		},
		{
			name: "missing workflow",
			params: &runtime.StartParams{
				Identity: identity,
				Snapshot: snapshot,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := dagRuntime.Start(context.Background(), tt.params)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), "required") {
				t.Fatalf("expected required-field error, got %v", err)
			}
		})
	}
}

func TestBuildDeps_InvalidSnapshotDefinitionReturnsError(t *testing.T) {
	_, _, err := BuildDeps(&runtime.FrozenSnapshot{
		DAGHash:    "invalid",
		Definition: []byte("{not-json"),
	})
	if err == nil {
		t.Fatal("expected error for invalid snapshot JSON")
	}
}

func TestReplay_WorkflowContextRestoredFromHistory(t *testing.T) {
	events := []*runtime.HistoryEvent{
		{
			Sequence: 1,
			Type:     runtime.HistoryWorkflowStarted,
			Timestamp: time.Date(
				2025, time.January, 1, 0, 0, 0, 0, time.UTC,
			),
			Attributes: map[string]interface{}{
				"context": map[string]interface{}{
					"topic": "durability",
					"depth": "strict",
				},
			},
		},
	}

	state := Replay(events, []string{"n1"})
	if state.WorkflowContext == nil {
		t.Fatal("expected workflow context to be restored")
	}
	if got := state.WorkflowContext["topic"]; got != "durability" {
		t.Fatalf("expected topic=durability, got %v", got)
	}
	if got := state.WorkflowContext["depth"]; got != "strict" {
		t.Fatalf("expected depth=strict, got %v", got)
	}
}

func TestDAGRuntime_ResumeDoesNotDuplicateWorkflowStarted(t *testing.T) {
	store, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	registry := runtime.NewActivityHandlerRegistry()
	handler := &countingLLMHandler{}
	registry.Register(handler)
	dagRuntime := NewDAGRuntime(store, registry)

	wf := &workflow.Workflow{
		ID:   "wf-resume-no-duplicate-start",
		Name: "Resume No Duplicate Start",
		Nodes: []*workflow.Node{
			{ID: "n1", Type: workflow.NodeTypePrompt, Model: "mock-model", Prompt: "hello"},
		},
	}
	snapshot := freezeWorkflowForDurableTest(t, wf)
	jobID := "job-resume-no-duplicate-start"
	createDurableJobForTest(t, store, jobID, wf, snapshot)

	if err := store.AppendHistoryEvent(context.Background(), &storage.HistoryEvent{
		RunID:     jobID,
		Type:      string(runtime.HistoryWorkflowStarted),
		Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("append workflow_started failed: %v", err)
	}
	if err := store.AppendHistoryEvent(context.Background(), &storage.HistoryEvent{
		RunID:     jobID,
		Type:      string(runtime.HistoryWorkflowCompleted),
		Timestamp: time.Now().Add(time.Millisecond),
		Attributes: map[string]interface{}{
			"total_cost":          float64(0),
			"total_tokens":        float64(0),
			"total_input_tokens":  float64(0),
			"total_output_tokens": float64(0),
		},
	}); err != nil {
		t.Fatalf("append workflow_completed failed: %v", err)
	}

	if err := dagRuntime.Start(context.Background(), &runtime.StartParams{
		Identity: &runtime.ExecutionIdentity{
			WorkflowExecutionID: jobID,
			RunID:               jobID,
			RunNumber:           1,
			DAGHash:             snapshot.DAGHash,
		},
		Snapshot: snapshot,
		Workflow: wf,
	}); err != nil {
		t.Fatalf("runtime start failed: %v", err)
	}

	history, err := store.GetHistoryEvents(context.Background(), jobID)
	if err != nil {
		t.Fatalf("GetHistoryEvents failed: %v", err)
	}

	workflowStartedCount := 0
	for _, evt := range history {
		if evt.Type == string(runtime.HistoryWorkflowStarted) {
			workflowStartedCount++
		}
	}
	if workflowStartedCount != 1 {
		t.Fatalf("expected exactly one workflow_started event, got %d", workflowStartedCount)
	}
	if handler.CallCount() != 0 {
		t.Fatalf("expected no activity execution for terminal run, got %d calls", handler.CallCount())
	}
}

func TestDAGRuntime_UsesCachedFailedActivityResultWithoutReExecutingHandler(t *testing.T) {
	store := newTestStore(t)
	registry := runtime.NewActivityHandlerRegistry()
	handler := newScriptedLLMHandler(scriptedNode{success: true, output: "fresh"})
	registry.Register(handler)
	dagRuntime := NewDAGRuntime(store, registry)

	wf := &workflow.Workflow{
		ID:   "wf-cached-failed-activity",
		Name: "Cached Failed Activity",
		Nodes: []*workflow.Node{
			{
				ID:     "n1",
				Type:   workflow.NodeTypePrompt,
				Model:  "mock-model",
				Prompt: "hello",
				RetryPolicy: &workflow.RetryPolicy{
					MaxAttempts:     5,
					BackoffMs:       1,
					BackoffMultiply: 1.0,
					RetryableErrors: []string{"AUTHENTICATION", "TIMEOUT"},
				},
			},
		},
	}
	snapshot := freezeWorkflowForDurableTest(t, wf)
	jobID := "job-cached-failed-activity"
	createDurableJobForTest(t, store, jobID, wf, snapshot)

	cachedOutput := &runtime.ActivityOutput{
		NodeID:    "n1",
		Success:   false,
		Error:     "cached authentication failure",
		ErrorCode: "AUTHENTICATION",
	}
	serialized, err := json.Marshal(cachedOutput)
	if err != nil {
		t.Fatalf("marshal cached output failed: %v", err)
	}
	completedAt := time.Now()
	if err := store.SaveActivityResult(context.Background(), &storage.ActivityResult{
		IdempotencyKey: jobID + ":n1:1",
		RunID:          jobID,
		NodeID:         "n1",
		Attempt:        1,
		ActivityType:   string(runtime.ActivityTypeLLMCall),
		Status:         "failed",
		OutputPayload:  string(serialized),
		ErrorCode:      "AUTHENTICATION",
		ErrorMessage:   "cached authentication failure",
		CompletedAt:    &completedAt,
	}); err != nil {
		t.Fatalf("failed to seed cached failed activity result: %v", err)
	}

	var streamTypes []string
	if err := dagRuntime.Start(context.Background(), &runtime.StartParams{
		Identity: &runtime.ExecutionIdentity{
			WorkflowExecutionID: jobID,
			RunID:               jobID,
			RunNumber:           1,
			DAGHash:             snapshot.DAGHash,
		},
		Snapshot: snapshot,
		Workflow: wf,
		EventCallback: func(event *runtime.ExecutionEvent) {
			streamTypes = append(streamTypes, event.Type)
		},
	}); err != nil {
		t.Fatalf("runtime start failed: %v", err)
	}

	if handler.CallCount() != 0 {
		t.Fatalf("expected cached failed result to bypass handler execution, got %d calls", handler.CallCount())
	}
	if countEventType(streamTypes, events.EventNodeRetryBackoff) != 0 {
		t.Fatalf("expected no retry_backoff events, got %d", countEventType(streamTypes, events.EventNodeRetryBackoff))
	}
	if countEventType(streamTypes, events.EventNodeRetryStart) != 0 {
		t.Fatalf("expected no retry_start events, got %d", countEventType(streamTypes, events.EventNodeRetryStart))
	}

	job, err := store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("GetJob failed: %v", err)
	}
	if job.Status != events.JobStatusFailed {
		t.Fatalf("expected failed status, got %s", job.Status)
	}

	history, err := store.GetHistoryEvents(context.Background(), jobID)
	if err != nil {
		t.Fatalf("GetHistoryEvents failed: %v", err)
	}
	if countHistoryByType(history, string(runtime.HistoryActivityFailed)) != 1 {
		t.Fatalf("expected exactly one activity_failed event, got %d", countHistoryByType(history, string(runtime.HistoryActivityFailed)))
	}
	if !hasHistoryType(history, string(runtime.HistoryNodeFailed)) {
		t.Fatal("expected node_failed event in history")
	}
	if !hasHistoryType(history, string(runtime.HistoryWorkflowFailed)) {
		t.Fatal("expected workflow_failed event in history")
	}
}
