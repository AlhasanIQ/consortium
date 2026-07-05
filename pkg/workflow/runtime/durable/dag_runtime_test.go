package durable

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

// ---------------------------------------------------------------------------
// prepareStartParams
// ---------------------------------------------------------------------------

func TestDAGRuntime_PrepareStartParams_NilParams(t *testing.T) {
	err := prepareStartParams(nil)
	if err == nil {
		t.Fatal("expected error for nil params")
	}
}

func TestDAGRuntime_PrepareStartParams_MissingIdentity(t *testing.T) {
	err := prepareStartParams(&runtime.StartParams{
		Snapshot: &runtime.FrozenSnapshot{},
		Workflow: &workflow.Workflow{},
	})
	if err == nil {
		t.Fatal("expected error for nil identity")
	}
}

func TestDAGRuntime_PrepareStartParams_MissingSnapshot(t *testing.T) {
	err := prepareStartParams(&runtime.StartParams{
		Identity: &runtime.ExecutionIdentity{},
		Workflow: &workflow.Workflow{},
	})
	if err == nil {
		t.Fatal("expected error for nil snapshot")
	}
}

func TestDAGRuntime_PrepareStartParams_MissingWorkflow(t *testing.T) {
	err := prepareStartParams(&runtime.StartParams{
		Identity: &runtime.ExecutionIdentity{},
		Snapshot: &runtime.FrozenSnapshot{},
	})
	if err == nil {
		t.Fatal("expected error for nil workflow")
	}
}

func TestDAGRuntime_PrepareStartParams_InitializesCostTracker(t *testing.T) {
	params := &runtime.StartParams{
		Identity: &runtime.ExecutionIdentity{},
		Snapshot: &runtime.FrozenSnapshot{},
		Workflow: &workflow.Workflow{
			Limits: &workflow.CostLimits{MaxCostUSD: 1.0},
		},
	}
	if err := prepareStartParams(params); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if params.CostTracker == nil {
		t.Fatal("expected CostTracker to be initialized")
	}
}

func TestDAGRuntime_PrepareStartParams_PreservesCostTracker(t *testing.T) {
	existing := workflow.NewCostTracker(&workflow.CostLimits{MaxCostUSD: 5.0})
	params := &runtime.StartParams{
		Identity:    &runtime.ExecutionIdentity{},
		Snapshot:    &runtime.FrozenSnapshot{},
		Workflow:    &workflow.Workflow{},
		CostTracker: existing,
	}
	if err := prepareStartParams(params); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if params.CostTracker != existing {
		t.Fatal("expected existing CostTracker to be preserved")
	}
}

// ---------------------------------------------------------------------------
// buildNodeMap
// ---------------------------------------------------------------------------

func TestDAGRuntime_BuildNodeMap(t *testing.T) {
	nodes := []*workflow.Node{
		{ID: "a", Type: workflow.NodeTypePrompt},
		{ID: "b", Type: workflow.NodeTypeResult},
		nil, // should be skipped
		{ID: "c", Type: workflow.NodeTypePrompt},
	}
	m := buildNodeMap(nodes)
	if len(m) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(m))
	}
	if m["a"] == nil || m["b"] == nil || m["c"] == nil {
		t.Error("expected all non-nil nodes in map")
	}
}

func TestDAGRuntime_BuildNodeMap_Empty(t *testing.T) {
	m := buildNodeMap(nil)
	if len(m) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(m))
	}
}

func TestDAGRuntime_BuildNodeMap_DuplicateIDsLastWins(t *testing.T) {
	nodes := []*workflow.Node{
		{ID: "dup", Type: workflow.NodeTypePrompt, Model: "first"},
		{ID: "dup", Type: workflow.NodeTypePrompt, Model: "second"},
	}
	m := buildNodeMap(nodes)
	if len(m) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(m))
	}
	if m["dup"].Model != "second" {
		t.Errorf("expected last node to win, got model=%q", m["dup"].Model)
	}
}

// ---------------------------------------------------------------------------
// buildWorkflowContext
// ---------------------------------------------------------------------------

func TestDAGRuntime_BuildWorkflowContext_MergesInitialAndOutputs(t *testing.T) {
	initial := map[string]interface{}{
		"prompt": "What is 2+2?",
	}
	outputs := map[string]interface{}{
		"node-1": "four",
		"node-2": 42, // non-string should be skipped
	}
	ctx := buildWorkflowContext(initial, outputs)
	if ctx["prompt"] != "What is 2+2?" {
		t.Errorf("prompt = %v, want 'What is 2+2?'", ctx["prompt"])
	}
	if ctx["node-1"] != "four" {
		t.Errorf("node-1 = %v, want 'four'", ctx["node-1"])
	}
	if _, ok := ctx["node-2"]; ok {
		t.Error("non-string outputs should not be included")
	}
}

func TestDAGRuntime_BuildWorkflowContext_NilInputs(t *testing.T) {
	ctx := buildWorkflowContext(nil, nil)
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	if len(ctx) != 0 {
		t.Errorf("expected empty context, got %d entries", len(ctx))
	}
}

func TestDAGRuntime_BuildWorkflowContext_OutputOverridesInitial(t *testing.T) {
	initial := map[string]interface{}{"key": "initial-value"}
	outputs := map[string]interface{}{"key": "output-value"}
	ctx := buildWorkflowContext(initial, outputs)
	if ctx["key"] != "output-value" {
		t.Errorf("expected output to override initial, got %v", ctx["key"])
	}
}

// ---------------------------------------------------------------------------
// completedNodeCount
// ---------------------------------------------------------------------------

func TestDAGRuntime_CompletedNodeCount(t *testing.T) {
	state := &SchedulerState{
		Nodes: map[string]NodeState{
			"a": NodeStateCompleted,
			"b": NodeStateFailed,
			"c": NodeStateCompleted,
			"d": NodeStatePending,
		},
	}
	count := completedNodeCount([]string{"a", "b", "c", "d"}, state)
	if count != 2 {
		t.Errorf("expected 2 completed, got %d", count)
	}
}

func TestDAGRuntime_CompletedNodeCount_NilState(t *testing.T) {
	count := completedNodeCount([]string{"a"}, nil)
	if count != 0 {
		t.Errorf("expected 0 for nil state, got %d", count)
	}
}

func TestDAGRuntime_CompletedNodeCount_AllCompleted(t *testing.T) {
	state := &SchedulerState{
		Nodes: map[string]NodeState{
			"a": NodeStateCompleted,
			"b": NodeStateCompleted,
		},
	}
	count := completedNodeCount([]string{"a", "b"}, state)
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}
}

func TestDAGRuntime_CompletedNodeCount_NoneCompleted(t *testing.T) {
	state := &SchedulerState{
		Nodes: map[string]NodeState{
			"a": NodeStatePending,
			"b": NodeStateRunning,
		},
	}
	count := completedNodeCount([]string{"a", "b"}, state)
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// resolveFinalOutput
// ---------------------------------------------------------------------------

func TestDAGRuntime_ResolveFinalOutput_LastNodeWithOutput(t *testing.T) {
	nodeIDs := []string{"a", "b", "c"}
	outputs := map[string]interface{}{
		"a": "first",
		"c": "last",
	}
	result := resolveFinalOutput(nodeIDs, outputs)
	if result != "last" {
		t.Errorf("expected 'last', got %q", result)
	}
}

func TestDAGRuntime_ResolveFinalOutput_SkipsEmptyStrings(t *testing.T) {
	nodeIDs := []string{"a", "b"}
	outputs := map[string]interface{}{
		"a": "result",
		"b": "", // empty, should skip
	}
	result := resolveFinalOutput(nodeIDs, outputs)
	if result != "result" {
		t.Errorf("expected 'result', got %q", result)
	}
}

func TestDAGRuntime_ResolveFinalOutput_NoOutputs(t *testing.T) {
	result := resolveFinalOutput([]string{"a", "b"}, map[string]interface{}{})
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestDAGRuntime_ResolveFinalOutput_NonStringOutput(t *testing.T) {
	nodeIDs := []string{"a", "b"}
	outputs := map[string]interface{}{
		"a": "valid",
		"b": 42, // non-string
	}
	result := resolveFinalOutput(nodeIDs, outputs)
	if result != "valid" {
		t.Errorf("expected 'valid', got %q", result)
	}
}

// ---------------------------------------------------------------------------
// resolveFinalError
// ---------------------------------------------------------------------------

func TestDAGRuntime_ResolveFinalError_RuntimeError(t *testing.T) {
	err := fmt.Errorf("runtime error")
	result := resolveFinalError(err, []string{"a"}, map[string]string{"a": "node error"})
	if result != "runtime error" {
		t.Errorf("expected runtime error to take precedence, got %q", result)
	}
}

func TestDAGRuntime_ResolveFinalError_NodeError(t *testing.T) {
	result := resolveFinalError(nil, []string{"a", "b"}, map[string]string{
		"b": "node b failed",
	})
	if result != "node b failed" {
		t.Errorf("expected 'node b failed', got %q", result)
	}
}

func TestDAGRuntime_ResolveFinalError_NoErrors(t *testing.T) {
	result := resolveFinalError(nil, []string{"a"}, map[string]string{})
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestDAGRuntime_ResolveFinalError_FirstNodeErrorByOrder(t *testing.T) {
	result := resolveFinalError(nil, []string{"x", "y", "z"}, map[string]string{
		"y": "y failed",
		"z": "z failed",
	})
	if result != "y failed" {
		t.Errorf("expected first error in node order, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// resolveNodeRetryPolicy
// ---------------------------------------------------------------------------

func TestDAGRuntime_ResolveNodeRetryPolicy_NilNode(t *testing.T) {
	p := resolveNodeRetryPolicy(nil)
	if p != nil {
		t.Error("expected nil for nil node")
	}
}

func TestDAGRuntime_ResolveNodeRetryPolicy_NoPolicy(t *testing.T) {
	p := resolveNodeRetryPolicy(&workflow.Node{})
	if p != nil {
		t.Error("expected nil when node has no retry policy")
	}
}

func TestDAGRuntime_ResolveNodeRetryPolicy_ReturnsPolicy(t *testing.T) {
	policy := &workflow.RetryPolicy{MaxAttempts: 3}
	p := resolveNodeRetryPolicy(&workflow.Node{RetryPolicy: policy})
	if p != policy {
		t.Error("expected same policy pointer")
	}
}

// ---------------------------------------------------------------------------
// buildAttemptMeta
// ---------------------------------------------------------------------------

func TestDAGRuntime_BuildAttemptMeta_EmptyResult(t *testing.T) {
	node := &workflow.Node{Type: workflow.NodeTypePrompt}
	meta := buildAttemptMeta(node, nil)
	if meta != nil {
		t.Errorf("expected nil for node with no reasoning config and nil output meta, got %v", meta)
	}
}

func TestDAGRuntime_BuildAttemptMeta_ChildJobID(t *testing.T) {
	node := &workflow.Node{Type: workflow.NodeTypePrompt}
	outputMeta := map[string]interface{}{
		"child_job_id": "child-123",
	}
	meta := buildAttemptMeta(node, outputMeta)
	if meta == nil {
		t.Fatal("expected non-nil meta")
	}
	if meta["child_job_id"] != "child-123" {
		t.Errorf("child_job_id = %v, want child-123", meta["child_job_id"])
	}
}

func TestDAGRuntime_BuildAttemptMeta_EmptyChildJobIDIgnored(t *testing.T) {
	node := &workflow.Node{Type: workflow.NodeTypePrompt}
	outputMeta := map[string]interface{}{
		"child_job_id": "",
	}
	meta := buildAttemptMeta(node, outputMeta)
	// empty child_job_id should not be included
	if meta != nil {
		if _, ok := meta["child_job_id"]; ok {
			t.Error("empty child_job_id should not be included")
		}
	}
}

// ---------------------------------------------------------------------------
// Cancel
// ---------------------------------------------------------------------------

func TestDAGRuntime_Cancel_UnknownExecID(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())
	err := r.Cancel(context.Background(), "unknown-exec-id")
	if err != nil {
		t.Fatalf("expected no error for unknown exec ID, got %v", err)
	}
}

func TestDAGRuntime_Cancel_CallsCancelFunc(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())

	cancelled := false
	r.mu.Lock()
	r.cancels["exec-1"] = func() { cancelled = true }
	r.mu.Unlock()

	err := r.Cancel(context.Background(), "exec-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cancelled {
		t.Error("expected cancel function to be called")
	}
}

// ---------------------------------------------------------------------------
// handleCancellation
// ---------------------------------------------------------------------------

func TestDAGRuntime_HandleCancellation_SetsStateAndEmitsEvent(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())

	createDurableJob(t, store, "job-cancel-test", "run-cancel-test")

	state := NewSchedulerState([]string{"n1"})
	var captured *runtime.ExecutionEvent
	params := &runtime.StartParams{
		Identity: &runtime.ExecutionIdentity{
			WorkflowExecutionID: "job-cancel-test",
			RunID:               "run-cancel-test",
		},
		EventCallback: func(e *runtime.ExecutionEvent) { captured = e },
	}

	runtimeErr := r.handleCancellation(params, state, "job-cancel-test", "run-cancel-test", nil)
	if runtimeErr != nil {
		t.Fatalf("unexpected error: %v", runtimeErr)
	}
	if !state.Cancelled {
		t.Error("expected state.Cancelled to be true")
	}
	if captured == nil {
		t.Fatal("expected cancellation event")
	}
	if captured.Type != events.EventCancelled {
		t.Errorf("event type = %q, want %q", captured.Type, events.EventCancelled)
	}
}

// ---------------------------------------------------------------------------
// toRuntimeHistoryEvents
// ---------------------------------------------------------------------------

func TestDAGRuntime_ToRuntimeHistoryEvents(t *testing.T) {
	now := time.Now()
	storageEvents := []*storage.HistoryEvent{
		{
			ID:         1,
			RunID:      "run-1",
			Sequence:   1,
			Type:       "workflow_started",
			NodeID:     "",
			ActivityID: "",
			Timestamp:  now,
			Attributes: map[string]interface{}{"key": "value"},
		},
		{
			ID:         2,
			RunID:      "run-1",
			Sequence:   2,
			Type:       "schedule_activity",
			NodeID:     "n1",
			ActivityID: "a1",
			Timestamp:  now.Add(time.Second),
		},
	}

	result := toRuntimeHistoryEvents(storageEvents)
	if len(result) != 2 {
		t.Fatalf("expected 2 events, got %d", len(result))
	}
	if result[0].ID != 1 {
		t.Errorf("first event ID = %d, want 1", result[0].ID)
	}
	if result[0].Type != runtime.HistoryWorkflowStarted {
		t.Errorf("first event type = %q, want workflow_started", result[0].Type)
	}
	if result[0].Attributes["key"] != "value" {
		t.Errorf("first event attributes missing key")
	}
	if result[1].NodeID != "n1" {
		t.Errorf("second event NodeID = %q, want n1", result[1].NodeID)
	}
	if result[1].ActivityID != "a1" {
		t.Errorf("second event ActivityID = %q, want a1", result[1].ActivityID)
	}
}

func TestDAGRuntime_ToRuntimeHistoryEvents_Empty(t *testing.T) {
	result := toRuntimeHistoryEvents(nil)
	if len(result) != 0 {
		t.Errorf("expected 0 events, got %d", len(result))
	}
}

// ---------------------------------------------------------------------------
// End-to-end: Start with a simple single-node workflow
// ---------------------------------------------------------------------------

func TestDAGRuntime_Start_SingleNodeSuccess(t *testing.T) {
	store := newTestStore(t)
	registry := runtime.NewActivityHandlerRegistry()
	handler := newScriptedLLMHandler(scriptedNode{success: true, output: "hello world"})
	registry.Register(handler)
	dagRuntime := NewDAGRuntime(store, registry)

	wf := &workflow.Workflow{
		ID:   "wf-single",
		Name: "Single Node",
		Nodes: []*workflow.Node{
			{
				ID:          "n1",
				Type:        workflow.NodeTypePrompt,
				Model:       "test-model",
				Prompt:      "say hello",
				RetryPolicy: &workflow.RetryPolicy{MaxAttempts: 1},
			},
		},
	}
	snapshot := freezeWorkflowForDurableTest(t, wf)
	jobID := "job-single-success"
	createDurableJobForTest(t, store, jobID, wf, snapshot)

	var mu sync.Mutex
	var eventTypes []string

	err := dagRuntime.Start(context.Background(), &runtime.StartParams{
		Identity: &runtime.ExecutionIdentity{
			WorkflowExecutionID: jobID,
			RunID:               jobID,
			RunNumber:           1,
			DAGHash:             snapshot.DAGHash,
		},
		Snapshot: snapshot,
		Workflow: wf,
		EventCallback: func(e *runtime.ExecutionEvent) {
			mu.Lock()
			eventTypes = append(eventTypes, e.Type)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	job, err := store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("GetExecution failed: %v", err)
	}
	if job.Status != events.JobStatusCompleted {
		t.Errorf("status = %q, want completed", job.Status)
	}
	if job.ResultText != "hello world" {
		t.Errorf("result = %q, want 'hello world'", job.ResultText)
	}

	mu.Lock()
	defer mu.Unlock()
	if countEventType(eventTypes, events.EventNodeStart) != 1 {
		t.Errorf("expected 1 node_start event, got %d", countEventType(eventTypes, events.EventNodeStart))
	}
	if countEventType(eventTypes, events.EventNodeComplete) != 1 {
		t.Errorf("expected 1 node_complete event, got %d", countEventType(eventTypes, events.EventNodeComplete))
	}
	if countEventType(eventTypes, events.EventComplete) != 1 {
		t.Errorf("expected 1 workflow_complete event, got %d", countEventType(eventTypes, events.EventComplete))
	}
}

func TestDAGRuntime_Start_SingleNodeFailure(t *testing.T) {
	store := newTestStore(t)
	registry := runtime.NewActivityHandlerRegistry()
	handler := newScriptedLLMHandler(scriptedNode{
		success:   false,
		err:       "something went wrong",
		errorCode: "AUTHENTICATION", // non-retryable
	})
	registry.Register(handler)
	dagRuntime := NewDAGRuntime(store, registry)

	wf := &workflow.Workflow{
		ID:   "wf-fail",
		Name: "Fail",
		Nodes: []*workflow.Node{
			{
				ID:          "n1",
				Type:        workflow.NodeTypePrompt,
				Model:       "test-model",
				Prompt:      "fail please",
				RetryPolicy: &workflow.RetryPolicy{MaxAttempts: 3, BackoffMs: 1},
			},
		},
	}
	snapshot := freezeWorkflowForDurableTest(t, wf)
	jobID := "job-single-fail"
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
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Non-retryable error means only 1 attempt
	if handler.CallCount() != 1 {
		t.Errorf("expected 1 call for non-retryable, got %d", handler.CallCount())
	}

	job, err := store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("GetExecution failed: %v", err)
	}
	if job.Status != events.JobStatusFailed {
		t.Errorf("status = %q, want failed", job.Status)
	}
	if !strings.Contains(job.ErrorMessage, "something went wrong") {
		t.Errorf("error = %q, want to contain 'something went wrong'", job.ErrorMessage)
	}
}

// ---------------------------------------------------------------------------
// Context cancellation during Start
// ---------------------------------------------------------------------------

func TestDAGRuntime_Start_ContextCancellation(t *testing.T) {
	store := newTestStore(t)
	registry := runtime.NewActivityHandlerRegistry()
	handler := newScriptedLLMHandler(scriptedNode{blockUntilCancel: true})
	registry.Register(handler)
	dagRuntime := NewDAGRuntime(store, registry)

	wf := &workflow.Workflow{
		ID:   "wf-ctx-cancel",
		Name: "Context Cancel",
		Nodes: []*workflow.Node{
			{
				ID:             "n1",
				Type:           workflow.NodeTypePrompt,
				Model:          "test-model",
				Prompt:         "block",
				TimeoutSeconds: 30,
				RetryPolicy:    &workflow.RetryPolicy{MaxAttempts: 1},
			},
		},
	}
	snapshot := freezeWorkflowForDurableTest(t, wf)
	jobID := "job-ctx-cancel"
	createDurableJobForTest(t, store, jobID, wf, snapshot)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := dagRuntime.Start(ctx, &runtime.StartParams{
		Identity: &runtime.ExecutionIdentity{
			WorkflowExecutionID: jobID,
			RunID:               jobID,
			RunNumber:           1,
			DAGHash:             snapshot.DAGHash,
		},
		Snapshot: snapshot,
		Workflow: wf,
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	job, err := store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("GetExecution failed: %v", err)
	}
	if job.Status != events.JobStatusCancelled {
		t.Errorf("status = %q, want cancelled", job.Status)
	}
}

// ---------------------------------------------------------------------------
// Cost tracking: cost limit triggers failure
// ---------------------------------------------------------------------------

func TestDAGRuntime_Start_CostLimitTriggersFailure(t *testing.T) {
	store := newTestStore(t)
	registry := runtime.NewActivityHandlerRegistry()
	handler := &costInjectingLLMHandler{cost: 5.0}
	registry.Register(handler)
	dagRuntime := NewDAGRuntime(store, registry)

	wf := &workflow.Workflow{
		ID:   "wf-cost",
		Name: "Cost Test",
		Nodes: []*workflow.Node{
			{
				ID:          "n1",
				Type:        workflow.NodeTypePrompt,
				Model:       "test-model",
				Prompt:      "expensive",
				RetryPolicy: &workflow.RetryPolicy{MaxAttempts: 1},
			},
		},
		Limits: &workflow.CostLimits{MaxCostUSD: 1.0},
	}
	snapshot := freezeWorkflowForDurableTest(t, wf)
	jobID := "job-cost-fail"
	createDurableJobForTest(t, store, jobID, wf, snapshot)

	var mu sync.Mutex
	var errorEvents []string

	err := dagRuntime.Start(context.Background(), &runtime.StartParams{
		Identity: &runtime.ExecutionIdentity{
			WorkflowExecutionID: jobID,
			RunID:               jobID,
			RunNumber:           1,
			DAGHash:             snapshot.DAGHash,
		},
		Snapshot:    snapshot,
		Workflow:    wf,
		CostTracker: workflow.NewCostTracker(wf.Limits),
		EventCallback: func(e *runtime.ExecutionEvent) {
			if e.Type == events.EventError {
				mu.Lock()
				if msg, ok := e.Payload["message"].(string); ok {
					errorEvents = append(errorEvents, msg)
				}
				mu.Unlock()
			}
		},
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	job, err := store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("GetExecution failed: %v", err)
	}
	if job.Status != events.JobStatusFailed {
		t.Errorf("status = %q, want failed", job.Status)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(errorEvents) == 0 {
		t.Error("expected at least one error event for cost limit")
	}
	foundCostError := false
	for _, msg := range errorEvents {
		if strings.Contains(strings.ToLower(msg), "cost") {
			foundCostError = true
			break
		}
	}
	if !foundCostError {
		t.Errorf("expected cost-related error event, got: %v", errorEvents)
	}
}

// ---------------------------------------------------------------------------
// Multi-node DAG: sequential dependencies
// ---------------------------------------------------------------------------

func TestDAGRuntime_Start_MultiNodeSequential(t *testing.T) {
	store := newTestStore(t)
	registry := runtime.NewActivityHandlerRegistry()
	handler := newScriptedLLMHandler(
		scriptedNode{success: true, output: "first-output"},
		scriptedNode{success: true, output: "second-output"},
	)
	registry.Register(handler)
	dagRuntime := NewDAGRuntime(store, registry)

	wf := &workflow.Workflow{
		ID:   "wf-multi-seq",
		Name: "Multi Sequential",
		Nodes: []*workflow.Node{
			{
				ID:          "n1",
				Type:        workflow.NodeTypePrompt,
				Model:       "test-model",
				Prompt:      "first",
				RetryPolicy: &workflow.RetryPolicy{MaxAttempts: 1},
			},
			{
				ID:          "n2",
				Type:        workflow.NodeTypePrompt,
				Model:       "test-model",
				Prompt:      "second",
				RetryPolicy: &workflow.RetryPolicy{MaxAttempts: 1},
			},
		},
		Edges: []*workflow.Edge{
			{Source: "n1", Target: "n2"},
		},
	}
	snapshot := freezeWorkflowForDurableTest(t, wf)
	jobID := "job-multi-seq"
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
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if handler.CallCount() != 2 {
		t.Errorf("expected 2 handler calls, got %d", handler.CallCount())
	}

	job, err := store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("GetExecution failed: %v", err)
	}
	if job.Status != events.JobStatusCompleted {
		t.Errorf("status = %q, want completed", job.Status)
	}
	// Final output should be from the last node
	if job.ResultText != "second-output" {
		t.Errorf("result = %q, want 'second-output'", job.ResultText)
	}

	history, err := store.GetHistoryEvents(context.Background(), jobID)
	if err != nil {
		t.Fatalf("GetHistoryEvents failed: %v", err)
	}
	completedCount := countHistoryByType(history, string(runtime.HistoryActivityCompleted))
	if completedCount != 2 {
		t.Errorf("expected 2 activity_completed events, got %d", completedCount)
	}
}

// ---------------------------------------------------------------------------
// Retry with eventual success
// ---------------------------------------------------------------------------

func TestDAGRuntime_Start_RetryThenSuccess(t *testing.T) {
	store := newTestStore(t)
	registry := runtime.NewActivityHandlerRegistry()
	handler := newScriptedLLMHandler(
		scriptedNode{success: false, err: "temporary failure", errorCode: "TIMEOUT"},
		scriptedNode{success: true, output: "recovered"},
	)
	registry.Register(handler)
	dagRuntime := NewDAGRuntime(store, registry)

	wf := &workflow.Workflow{
		ID:   "wf-retry-success",
		Name: "Retry Then Success",
		Nodes: []*workflow.Node{
			{
				ID:     "n1",
				Type:   workflow.NodeTypePrompt,
				Model:  "test-model",
				Prompt: "retry me",
				RetryPolicy: &workflow.RetryPolicy{
					MaxAttempts:     3,
					BackoffMs:       1,
					BackoffMultiply: 1.0,
					RetryableErrors: []string{"TIMEOUT"},
				},
			},
		},
	}
	snapshot := freezeWorkflowForDurableTest(t, wf)
	jobID := "job-retry-success"
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
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if handler.CallCount() != 2 {
		t.Errorf("expected 2 calls (1 fail + 1 success), got %d", handler.CallCount())
	}

	job, err := store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("GetExecution failed: %v", err)
	}
	if job.Status != events.JobStatusCompleted {
		t.Errorf("status = %q, want completed", job.Status)
	}
	if job.ResultText != "recovered" {
		t.Errorf("result = %q, want 'recovered'", job.ResultText)
	}
}

func TestDAGRuntime_Start_CompiledAggregationRetryDoesNotRerunUpstreamAgents(t *testing.T) {
	store := newTestStore(t)
	registry := runtime.NewActivityHandlerRegistry()
	handler := newScriptedLLMHandler(
		scriptedNode{success: true, output: "Answer: A"},
		scriptedNode{success: true, output: "Answer: B"},
		scriptedNode{success: false, err: "temporary judge failure", errorCode: "TIMEOUT"},
		scriptedNode{success: true, output: "Synthesized answer"},
	)
	registry.Register(handler)
	dagRuntime := NewDAGRuntime(store, registry)

	wf := &workflow.Workflow{
		ID:   "wf-compiled-agg-retry",
		Name: "Compiled Aggregation Retry",
		Nodes: []*workflow.Node{
			{
				ID:          "agent-a",
				Type:        workflow.NodeTypePrompt,
				Model:       "solver-a",
				Prompt:      "answer A",
				RetryPolicy: &workflow.RetryPolicy{MaxAttempts: 1},
			},
			{
				ID:          "agent-b",
				Type:        workflow.NodeTypePrompt,
				Model:       "solver-b",
				Prompt:      "answer B",
				RetryPolicy: &workflow.RetryPolicy{MaxAttempts: 1},
			},
			{
				ID:          "agg--synthesize",
				Type:        workflow.NodeTypePrompt,
				Model:       "judge-model",
				Prompt:      "synthesize {{agent-a}} {{agent-b}}",
				RetryPolicy: &workflow.RetryPolicy{MaxAttempts: 2, BackoffMs: 1, BackoffMultiply: 1, RetryableErrors: []string{"TIMEOUT"}},
				Metadata: map[string]interface{}{
					"aggregation_group_node_id": "agg--result",
					"aggregation_anchor_id":     "agg",
					"aggregation_method":        "synthesis",
					"source_workflow_id":        "aggregation-synthesis",
					"source_workflow_hash":      "source-hash-v1",
				},
			},
		},
		Edges: []*workflow.Edge{
			{ID: "a-agg", Source: "agent-a", Target: "agg--synthesize"},
			{ID: "b-agg", Source: "agent-b", Target: "agg--synthesize"},
		},
	}
	snapshot := freezeWorkflowForDurableTest(t, wf)
	jobID := "job-compiled-agg-retry"
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
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if handler.CallCount() != 4 {
		t.Fatalf("handler calls = %d, want 4", handler.CallCount())
	}
	history, err := store.GetHistoryEvents(context.Background(), jobID)
	if err != nil {
		t.Fatalf("GetHistoryEvents failed: %v", err)
	}
	if got := countHistoryByTypeAndNode(history, string(runtime.HistoryActivityStarted), "agent-a"); got != 1 {
		t.Fatalf("agent-a activity_started = %d, want 1", got)
	}
	if got := countHistoryByTypeAndNode(history, string(runtime.HistoryActivityStarted), "agent-b"); got != 1 {
		t.Fatalf("agent-b activity_started = %d, want 1", got)
	}
	if got := countHistoryByTypeAndNode(history, string(runtime.HistoryActivityStarted), "agg--synthesize"); got != 2 {
		t.Fatalf("agg--synthesize activity_started = %d, want 2", got)
	}
}

// ---------------------------------------------------------------------------
// Workflow context enrichment (model key)
// ---------------------------------------------------------------------------

func TestDAGRuntime_Start_WorkflowContextEnrichedWithModel(t *testing.T) {
	store := newTestStore(t)
	registry := runtime.NewActivityHandlerRegistry()
	handler := newScriptedLLMHandler(scriptedNode{success: true, output: "out"})
	registry.Register(handler)
	dagRuntime := NewDAGRuntime(store, registry)

	wf := &workflow.Workflow{
		ID:   "wf-model-ctx",
		Name: "Model Context",
		Nodes: []*workflow.Node{
			{
				ID:          "n1",
				Type:        workflow.NodeTypePrompt,
				Model:       "gpt-4o",
				Prompt:      "test",
				RetryPolicy: &workflow.RetryPolicy{MaxAttempts: 1},
			},
		},
	}
	snapshot := freezeWorkflowForDurableTest(t, wf)
	jobID := "job-model-ctx"
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
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	job, err := store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("GetExecution failed: %v", err)
	}
	if job.Status != events.JobStatusCompleted {
		t.Errorf("status = %q, want completed", job.Status)
	}
}

// ---------------------------------------------------------------------------
// finalizeRun with nil startCtx
// ---------------------------------------------------------------------------

func TestDAGRuntime_FinalizeRun_NilStartCtx(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())

	err := r.finalizeRun(context.Background(), &runtime.StartParams{}, nil, nil)
	if err != nil {
		t.Fatalf("expected no error for nil startCtx, got %v", err)
	}
}

func TestDAGRuntime_FinalizeRun_NilState(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())

	err := r.finalizeRun(context.Background(), &runtime.StartParams{}, &startRunContext{}, nil)
	if err != nil {
		t.Fatalf("expected no error for nil state, got %v", err)
	}
}

func TestDAGRuntime_FinalizeRun_PropagatesRuntimeErr(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())

	runtimeErr := fmt.Errorf("something broke")
	err := r.finalizeRun(context.Background(), &runtime.StartParams{}, nil, runtimeErr)
	if err != runtimeErr {
		t.Fatalf("expected runtimeErr to be returned, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// MaxParallel clamping in dispatchAndCollect
// ---------------------------------------------------------------------------

func TestDAGRuntime_Start_MaxParallelNodesRespected(t *testing.T) {
	store := newTestStore(t)
	registry := runtime.NewActivityHandlerRegistry()

	var mu sync.Mutex
	maxConcurrent := 0
	currentConcurrent := 0
	handler := &trackingLLMHandler{
		onExecute: func() {
			mu.Lock()
			currentConcurrent++
			if currentConcurrent > maxConcurrent {
				maxConcurrent = currentConcurrent
			}
			mu.Unlock()
			time.Sleep(50 * time.Millisecond) // ensure overlap window
			mu.Lock()
			currentConcurrent--
			mu.Unlock()
		},
	}
	registry.Register(handler)
	dagRuntime := NewDAGRuntime(store, registry)

	// 4 independent nodes but max parallel = 2
	wf := &workflow.Workflow{
		ID:   "wf-max-parallel",
		Name: "Max Parallel",
		Nodes: []*workflow.Node{
			{ID: "n1", Type: workflow.NodeTypePrompt, Model: "m", Prompt: "p", RetryPolicy: &workflow.RetryPolicy{MaxAttempts: 1}},
			{ID: "n2", Type: workflow.NodeTypePrompt, Model: "m", Prompt: "p", RetryPolicy: &workflow.RetryPolicy{MaxAttempts: 1}},
			{ID: "n3", Type: workflow.NodeTypePrompt, Model: "m", Prompt: "p", RetryPolicy: &workflow.RetryPolicy{MaxAttempts: 1}},
			{ID: "n4", Type: workflow.NodeTypePrompt, Model: "m", Prompt: "p", RetryPolicy: &workflow.RetryPolicy{MaxAttempts: 1}},
		},
		// No edges: all independent
	}
	snapshot := freezeWorkflowForDurableTest(t, wf)
	jobID := "job-max-parallel"
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
		ExecCtx: &workflow.ExecutionContext{
			MaxParallelNodes: 2,
		},
	})
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	mu.Lock()
	observedMax := maxConcurrent
	mu.Unlock()
	if observedMax > 2 {
		t.Errorf("max concurrent = %d, expected <= 2", observedMax)
	}

	job, err := store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("GetExecution failed: %v", err)
	}
	if job.Status != events.JobStatusCompleted {
		t.Errorf("status = %q, want completed", job.Status)
	}
}

// trackingLLMHandler is a test handler that executes a callback during activity processing.
type trackingLLMHandler struct {
	onExecute func()
}

func (h *trackingLLMHandler) Type() runtime.ActivityType {
	return runtime.ActivityTypeLLMCall
}

func (h *trackingLLMHandler) Execute(ctx context.Context, input *runtime.ActivityInput) (*runtime.ActivityOutput, error) {
	if h.onExecute != nil {
		h.onExecute()
	}
	return &runtime.ActivityOutput{
		NodeID:  input.NodeID,
		Success: true,
		Output:  "ok",
	}, nil
}

// ---------------------------------------------------------------------------
// processActivitySuccess: output_name metadata propagation
// ---------------------------------------------------------------------------

func TestDAGRuntime_ProcessActivitySuccess_OutputNamePropagation(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())

	createDurableJob(t, store, "job-output-name", "run-output-name")

	state := NewSchedulerState([]string{"n1"})
	state.Nodes["n1"] = NodeStateRunning
	state.PendingActivities["act-1"] = "n1"

	workflowCtx := make(map[string]interface{})
	res := &activityResult{
		nodeID:     "n1",
		activityID: "act-1",
		attempt:    1,
		node:       &workflow.Node{ID: "n1", Type: workflow.NodeTypePrompt},
		output: &runtime.ActivityOutput{
			NodeID:  "n1",
			Success: true,
			Output:  "42",
			Metadata: map[string]interface{}{
				"output_name":  "answer",
				"output_value": "42",
			},
		},
		startedAt: time.Now(),
	}
	params := &runtime.StartParams{
		Identity: &runtime.ExecutionIdentity{
			WorkflowExecutionID: "job-output-name",
			RunID:               "run-output-name",
		},
	}

	action, runtimeErr := r.processActivitySuccess(
		context.Background(), params, state, res, workflowCtx, "job-output-name", "run-output-name", 1,
	)
	if runtimeErr != nil {
		t.Fatalf("unexpected error: %v", runtimeErr)
	}
	if action != resultContinue {
		t.Errorf("action = %d, want resultContinue", action)
	}
	if workflowCtx["n1"] != "42" {
		t.Errorf("workflowCtx[n1] = %v, want '42'", workflowCtx["n1"])
	}
	if workflowCtx["__output_answer"] != "42" {
		t.Errorf("workflowCtx[__output_answer] = %v, want '42'", workflowCtx["__output_answer"])
	}
}

// ---------------------------------------------------------------------------
// processActivitySuccess: cost limit check
// ---------------------------------------------------------------------------

func TestDAGRuntime_ProcessActivitySuccess_CostLimitCheck(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())

	createDurableJob(t, store, "job-cost-check", "run-cost-check")

	state := NewSchedulerState([]string{"n1"})
	state.Nodes["n1"] = NodeStateRunning
	state.PendingActivities["act-1"] = "n1"

	costTracker := workflow.NewCostTracker(&workflow.CostLimits{MaxCostUSD: 0.01})
	costTracker.Add(100, 100, 1.0) // blow past the limit

	workflowCtx := make(map[string]interface{})
	res := &activityResult{
		nodeID:     "n1",
		activityID: "act-1",
		attempt:    1,
		node:       &workflow.Node{ID: "n1", Type: workflow.NodeTypePrompt},
		output: &runtime.ActivityOutput{
			NodeID:  "n1",
			Success: true,
			Output:  "expensive",
			Cost:    1.0,
		},
		startedAt: time.Now(),
	}
	params := &runtime.StartParams{
		Identity: &runtime.ExecutionIdentity{
			WorkflowExecutionID: "job-cost-check",
			RunID:               "run-cost-check",
		},
		CostTracker: costTracker,
	}

	action, _ := r.processActivitySuccess(
		context.Background(), params, state, res, workflowCtx, "job-cost-check", "run-cost-check", 1,
	)
	if action != resultBreak {
		t.Errorf("action = %d, want resultBreak (cost limit exceeded)", action)
	}
	if !state.Failed {
		t.Error("expected state.Failed after cost limit exceeded")
	}
}
