package durable

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

// ---------------------------------------------------------------------------
// extractLabel
// ---------------------------------------------------------------------------

func TestEmit_ExtractLabel_UsesMetadataLabel(t *testing.T) {
	node := &workflow.Node{
		ID:   "node-1",
		Type: workflow.NodeTypePrompt,
		Metadata: map[string]interface{}{
			"label": "My Custom Label",
		},
	}
	got := extractLabel(node)
	if got != "My Custom Label" {
		t.Errorf("extractLabel() = %q, want %q", got, "My Custom Label")
	}
}

func TestEmit_ExtractLabel_FallsBackToID(t *testing.T) {
	node := &workflow.Node{
		ID:   "node-fallback",
		Type: workflow.NodeTypePrompt,
	}
	got := extractLabel(node)
	if got != "node-fallback" {
		t.Errorf("extractLabel() = %q, want %q", got, "node-fallback")
	}
}

func TestEmit_ExtractLabel_FallsBackToType(t *testing.T) {
	node := &workflow.Node{
		Type: workflow.NodeTypeResult,
	}
	got := extractLabel(node)
	if got != string(workflow.NodeTypeResult) {
		t.Errorf("extractLabel() = %q, want %q", got, string(workflow.NodeTypeResult))
	}
}

// ---------------------------------------------------------------------------
// ptrTime
// ---------------------------------------------------------------------------

func TestEmit_PtrTime(t *testing.T) {
	now := ptrTime(time.Now())
	if now == nil {
		t.Fatal("ptrTime returned nil")
	}
}

// ---------------------------------------------------------------------------
// emitEvent
// ---------------------------------------------------------------------------

func TestEmit_EmitEvent_NilEventIsNoOp(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())
	// Should not panic.
	r.emitEvent(nil, nil)
}

func TestEmit_EmitEvent_NilPayloadInitialized(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())

	var captured *runtime.ExecutionEvent
	params := &runtime.StartParams{
		EventCallback: func(e *runtime.ExecutionEvent) { captured = e },
	}
	event := &runtime.ExecutionEvent{Type: "test"}
	r.emitEvent(params, event)

	if captured == nil {
		t.Fatal("callback was not invoked")
	}
	if captured.Payload == nil {
		t.Fatal("payload should be initialized when nil")
	}
}

func TestEmit_EmitEvent_InjectsIdentityFields(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())

	var captured *runtime.ExecutionEvent
	params := &runtime.StartParams{
		Identity: &runtime.ExecutionIdentity{
			WorkflowExecutionID: "exec-42",
			RunID:               "run-42",
		},
		EventCallback: func(e *runtime.ExecutionEvent) { captured = e },
	}
	event := &runtime.ExecutionEvent{Type: "test"}
	r.emitEvent(params, event)

	if captured == nil {
		t.Fatal("callback was not invoked")
	}
	if captured.Payload["execution_id"] != "exec-42" {
		t.Errorf("execution_id = %v, want exec-42", captured.Payload["execution_id"])
	}
	if captured.Payload["run_id"] != "run-42" {
		t.Errorf("run_id = %v, want run-42", captured.Payload["run_id"])
	}
}

func TestEmit_EmitEvent_DoesNotOverrideExistingIdentity(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())

	var captured *runtime.ExecutionEvent
	params := &runtime.StartParams{
		Identity: &runtime.ExecutionIdentity{
			WorkflowExecutionID: "exec-new",
			RunID:               "run-new",
		},
		EventCallback: func(e *runtime.ExecutionEvent) { captured = e },
	}
	event := &runtime.ExecutionEvent{
		Type: "test",
		Payload: map[string]interface{}{
			"execution_id": "exec-existing",
			"run_id":       "run-existing",
		},
	}
	r.emitEvent(params, event)

	if captured.Payload["execution_id"] != "exec-existing" {
		t.Errorf("execution_id = %v, want exec-existing (should not be overridden)", captured.Payload["execution_id"])
	}
	if captured.Payload["run_id"] != "run-existing" {
		t.Errorf("run_id = %v, want run-existing (should not be overridden)", captured.Payload["run_id"])
	}
}

func TestEmit_EmitEvent_NilCallbackDoesNotPanic(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())
	params := &runtime.StartParams{} // no callback set
	event := &runtime.ExecutionEvent{Type: "test"}
	// Should not panic.
	r.emitEvent(params, event)
}

// ---------------------------------------------------------------------------
// emitNodeStart
// ---------------------------------------------------------------------------

func TestEmit_EmitNodeStart(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())

	var captured *runtime.ExecutionEvent
	params := &runtime.StartParams{
		EventCallback: func(e *runtime.ExecutionEvent) { captured = e },
	}

	node := &workflow.Node{
		ID:   "prompt-1",
		Type: workflow.NodeTypePrompt,
		Metadata: map[string]interface{}{
			"label":       "GPT-4",
			"name":        "GPT-4 Response",
			"description": "Generate answer",
		},
	}
	r.emitNodeStart(params, node, 1, 3, "job-1")

	if captured == nil {
		t.Fatal("callback was not invoked")
	}
	if captured.Type != events.EventNodeStart {
		t.Errorf("type = %q, want %q", captured.Type, events.EventNodeStart)
	}
	if captured.Payload["job_id"] != "job-1" {
		t.Errorf("job_id = %v, want job-1", captured.Payload["job_id"])
	}
	if captured.Payload["node_id"] != "prompt-1" {
		t.Errorf("node_id = %v, want prompt-1", captured.Payload["node_id"])
	}

	data, ok := captured.Payload["data"].(map[string]interface{})
	if !ok {
		t.Fatal("data not a map")
	}
	if data["node_index"] != 1 {
		t.Errorf("node_index = %v, want 1", data["node_index"])
	}
	if data["node_total"] != 3 {
		t.Errorf("node_total = %v, want 3", data["node_total"])
	}
	if data["node_type"] != workflow.NodeTypePrompt {
		t.Errorf("node_type = %v, want %v", data["node_type"], workflow.NodeTypePrompt)
	}
	if data["node_label"] != "GPT-4" {
		t.Errorf("node_label = %v, want GPT-4", data["node_label"])
	}
	if data["node_name"] != "GPT-4 Response" {
		t.Errorf("node_name = %v, want 'GPT-4 Response'", data["node_name"])
	}
	if data["node_description"] != "Generate answer" {
		t.Errorf("node_description = %v, want 'Generate answer'", data["node_description"])
	}
}

func TestEmit_EmitNodeStart_ResultNodeIncludesAggregation(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())

	var captured *runtime.ExecutionEvent
	params := &runtime.StartParams{
		EventCallback: func(e *runtime.ExecutionEvent) { captured = e },
	}

	node := &workflow.Node{
		ID:                "result-1",
		Type:              workflow.NodeTypeResult,
		AggregationMethod: workflow.AggMethodSynthesis,
	}
	r.emitNodeStart(params, node, 2, 5, "job-2")

	data := captured.Payload["data"].(map[string]interface{})
	if data["aggregation_method"] != string(workflow.AggMethodSynthesis) {
		t.Errorf("aggregation_method = %v, want %q", data["aggregation_method"], workflow.AggMethodSynthesis)
	}
}

func TestEmit_EmitNodeStart_MessageWithoutDescription(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())

	var captured *runtime.ExecutionEvent
	params := &runtime.StartParams{
		EventCallback: func(e *runtime.ExecutionEvent) { captured = e },
	}

	node := &workflow.Node{ID: "n1", Type: workflow.NodeTypePrompt}
	r.emitNodeStart(params, node, 1, 1, "job-3")

	msg, _ := captured.Payload["message"].(string)
	if msg != "Executing n1 (1/1)" {
		t.Errorf("message = %q, want 'Executing n1 (1/1)'", msg)
	}
}

// ---------------------------------------------------------------------------
// emitNodeComplete
// ---------------------------------------------------------------------------

func TestEmit_EmitNodeComplete(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())

	var captured *runtime.ExecutionEvent
	params := &runtime.StartParams{
		EventCallback: func(e *runtime.ExecutionEvent) { captured = e },
	}

	node := &workflow.Node{
		ID:   "prompt-1",
		Type: workflow.NodeTypePrompt,
		Metadata: map[string]interface{}{
			"label": "Claude",
			"name":  "Claude Response",
		},
	}
	output := &runtime.ActivityOutput{
		NodeID:       "prompt-1",
		Output:       "Hello world",
		TokensInput:  100,
		TokensOutput: 50,
		Cost:         0.01,
		LatencyMs:    250.0,
		Metadata: map[string]interface{}{
			"aggregation_method": "synthesis",
			"aggregation_winner": "model-a",
		},
	}

	r.emitNodeComplete(params, node, output, "job-1")

	if captured == nil {
		t.Fatal("callback was not invoked")
	}
	if captured.Type != events.EventNodeComplete {
		t.Errorf("type = %q, want %q", captured.Type, events.EventNodeComplete)
	}
	if captured.Payload["output"] != "Hello world" {
		t.Errorf("output = %v, want 'Hello world'", captured.Payload["output"])
	}
	if captured.Payload["node_id"] != "prompt-1" {
		t.Errorf("node_id = %v, want prompt-1", captured.Payload["node_id"])
	}

	data := captured.Payload["data"].(map[string]interface{})
	if data["tokens_input"] != 100 {
		t.Errorf("tokens_input = %v, want 100", data["tokens_input"])
	}
	if data["cost"] != 0.01 {
		t.Errorf("cost = %v, want 0.01", data["cost"])
	}
	if data["aggregation_method"] != "synthesis" {
		t.Errorf("aggregation_method = %v, want synthesis", data["aggregation_method"])
	}
	if data["aggregation_winner"] != "model-a" {
		t.Errorf("aggregation_winner = %v, want model-a", data["aggregation_winner"])
	}
}

func TestEmit_EmitNodeComplete_NoMetadata(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())

	var captured *runtime.ExecutionEvent
	params := &runtime.StartParams{
		EventCallback: func(e *runtime.ExecutionEvent) { captured = e },
	}

	node := &workflow.Node{ID: "n1", Type: workflow.NodeTypePrompt}
	output := &runtime.ActivityOutput{NodeID: "n1", Output: "ok"}

	r.emitNodeComplete(params, node, output, "job-2")

	data := captured.Payload["data"].(map[string]interface{})
	// Aggregation keys should not be present when metadata is nil.
	if _, ok := data["aggregation_method"]; ok {
		t.Error("aggregation_method should not be set when metadata is nil")
	}
}

// ---------------------------------------------------------------------------
// emitRetryEvents
// ---------------------------------------------------------------------------

func TestEmit_EmitRetryEvents(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())

	var captured []*runtime.ExecutionEvent
	params := &runtime.StartParams{
		EventCallback: func(e *runtime.ExecutionEvent) {
			cp := *e
			cp.Payload = copyMap(e.Payload)
			captured = append(captured, &cp)
		},
	}

	node := &workflow.Node{ID: "retry-node", Type: workflow.NodeTypePrompt}
	policy := &workflow.RetryPolicy{
		MaxAttempts: 3,
		BackoffMs:   1000,
	}

	r.emitRetryEvents(params, node, 1, policy, "job-retry", "timeout error")

	if len(captured) != 2 {
		t.Fatalf("expected 2 events, got %d", len(captured))
	}

	// First event: retry backoff
	if captured[0].Type != events.EventNodeRetryBackoff {
		t.Errorf("first event type = %q, want %q", captured[0].Type, events.EventNodeRetryBackoff)
	}
	if captured[0].Payload["error"] != "timeout error" {
		t.Errorf("error = %v, want 'timeout error'", captured[0].Payload["error"])
	}
	if captured[0].Payload["attempt"] != 1 {
		t.Errorf("attempt = %v, want 1", captured[0].Payload["attempt"])
	}
	if captured[0].Payload["max_attempts"] != 3 {
		t.Errorf("max_attempts = %v, want 3", captured[0].Payload["max_attempts"])
	}

	// Second event: retry start
	if captured[1].Type != events.EventNodeRetryStart {
		t.Errorf("second event type = %q, want %q", captured[1].Type, events.EventNodeRetryStart)
	}
	if captured[1].Payload["attempt"] != 2 {
		t.Errorf("attempt = %v, want 2 (attempt+1)", captured[1].Payload["attempt"])
	}
}

// ---------------------------------------------------------------------------
// appendHistory / appendHistoryBatch
// ---------------------------------------------------------------------------

func TestEmit_AppendHistory(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())

	createDurableJob(t, store, "job-hist", "run-hist")

	ctx := context.Background()
	err := r.appendHistory(ctx, "run-hist", runtime.HistoryEventType("workflow_started"), "", "", map[string]interface{}{"foo": "bar"})
	if err != nil {
		t.Fatalf("appendHistory failed: %v", err)
	}

	// Verify the event was stored.
	events, err := store.GetHistoryEvents(ctx, "run-hist")
	if err != nil {
		t.Fatalf("GetHistoryEvents failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "workflow_started" {
		t.Errorf("event type = %q, want workflow_started", events[0].Type)
	}
}

func TestEmit_AppendHistoryBatch(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())

	createDurableJob(t, store, "job-batch", "run-batch")

	ctx := context.Background()
	entries := []historyBatchEntry{
		{eventType: "schedule_activity", nodeID: "n1", activityID: "a1"},
		{eventType: "activity_started", nodeID: "n1", activityID: "a1"},
	}
	err := r.appendHistoryBatch(ctx, "run-batch", entries)
	if err != nil {
		t.Fatalf("appendHistoryBatch failed: %v", err)
	}

	events, err := store.GetHistoryEvents(ctx, "run-batch")
	if err != nil {
		t.Fatalf("GetHistoryEvents failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
}

// ---------------------------------------------------------------------------
// markRunningNodesCancelled / markRunningNodesInterrupted
// ---------------------------------------------------------------------------

func TestEmit_MarkRunningNodesCancelled_NilStoreNoOp(t *testing.T) {
	r := &DAGRuntime{} // nil store
	// Should not panic.
	r.markRunningNodesCancelled("job-1", "run-1")
}

func TestEmit_MarkRunningNodesCancelled_EmptyIDsNoOp(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())
	// Empty jobID or runID should return early.
	r.markRunningNodesCancelled("", "run-1")
	r.markRunningNodesCancelled("job-1", "")
}

func TestEmit_MarkRunningNodesInterrupted_NilStoreNoOp(t *testing.T) {
	r := &DAGRuntime{} // nil store
	r.markRunningNodesInterrupted("job-1", "run-1")
}

func TestEmit_MarkRunningNodesInterrupted_EmptyIDsNoOp(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())
	r.markRunningNodesInterrupted("", "run-1")
	r.markRunningNodesInterrupted("job-1", "")
}

// ---------------------------------------------------------------------------
// saveWorkflowNode / saveFailedNode
// ---------------------------------------------------------------------------

func TestEmit_SaveWorkflowNode(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())

	createDurableJob(t, store, "job-save", "run-save")

	node := &workflow.Node{
		ID:    "prompt-1",
		Type:  workflow.NodeTypePrompt,
		Model: "gpt-4",
		Metadata: map[string]interface{}{
			"label": "GPT-4",
			"name":  "GPT-4 Response",
		},
	}
	output := &runtime.ActivityOutput{
		NodeID:       "prompt-1",
		Output:       "test output",
		TokensInput:  200,
		TokensOutput: 100,
		Cost:         0.05,
		LatencyMs:    500.0,
		Metadata:     map[string]interface{}{"key": "value"},
	}

	r.saveWorkflowNode("job-save", "run-save", "job-save", node, output, 1, "act-1", 0, 1)

	nodes, err := store.GetWorkflowNodes("job-save")
	if err != nil {
		t.Fatalf("GetWorkflowNodes failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].Status != "completed" {
		t.Errorf("status = %q, want completed", nodes[0].Status)
	}
	if nodes[0].Output != "test output" {
		t.Errorf("output = %q, want 'test output'", nodes[0].Output)
	}
	if nodes[0].TokensInput != 200 {
		t.Errorf("tokens_input = %d, want 200", nodes[0].TokensInput)
	}
	if nodes[0].Cost != 0.05 {
		t.Errorf("cost = %f, want 0.05", nodes[0].Cost)
	}
	if nodes[0].ActivityID != "act-1" {
		t.Errorf("activity_id = %q, want act-1", nodes[0].ActivityID)
	}
}

func TestEmit_SaveWorkflowNodePersistsCompiledAggregationGroupMetadata(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())

	createDurableJob(t, store, "job-compiled-agg", "run-compiled-agg")

	node := &workflow.Node{
		ID:    "agg--judge",
		Type:  workflow.NodeTypePrompt,
		Model: "judge-model",
		Metadata: map[string]interface{}{
			"aggregation_group_node_id": "agg--result",
			"aggregation_anchor_id":     "agg",
			"aggregation_method":        "judge",
			"source_workflow_id":        "aggregation-judge",
			"source_workflow_hash":      "sha-source",
			"source_parent_node_id":     "agg",
			"summary":                   "static",
		},
	}
	output := &runtime.ActivityOutput{
		NodeID:      "agg--judge",
		Output:      "A",
		TokensInput: 10,
		Cost:        0.01,
		Metadata: map[string]interface{}{
			"activity_metadata":         "kept",
			"aggregation_group_node_id": "wrong--result",
			"aggregation_anchor_id":     "wrong",
			"aggregation_method":        "wrong-method",
			"source_workflow_id":        "wrong-source",
			"source_workflow_hash":      "wrong-hash",
			"source_parent_node_id":     "wrong-parent",
			"summary":                   "runtime",
		},
	}

	r.saveWorkflowNode("job-compiled-agg", "run-compiled-agg", "job-compiled-agg", node, output, 1, "act-compiled", 0, 2)

	nodes, err := store.GetWorkflowNodes("job-compiled-agg")
	if err != nil {
		t.Fatalf("GetWorkflowNodes failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].ParentNodeID != "agg--result" {
		t.Fatalf("ParentNodeID = %q, want agg--result", nodes[0].ParentNodeID)
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(nodes[0].Metadata), &metadata); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	for _, key := range []string{"aggregation_anchor_id", "aggregation_method", "source_workflow_id", "source_workflow_hash", "activity_metadata"} {
		if metadata[key] == nil || metadata[key] == "" {
			t.Fatalf("metadata missing %s: %+v", key, metadata)
		}
	}
	if metadata["aggregation_group_node_id"] != "agg--result" || metadata["aggregation_anchor_id"] != "agg" || metadata["aggregation_method"] != "judge" || metadata["source_workflow_id"] != "aggregation-judge" || metadata["source_workflow_hash"] != "sha-source" || metadata["source_parent_node_id"] != "agg" {
		t.Fatalf("compiled provenance metadata was overwritten: %+v", metadata)
	}
	if metadata["summary"] != "runtime" {
		t.Fatalf("runtime metadata should win for non-provenance keys, got %+v", metadata)
	}
}

func TestEmit_SaveWorkflowNodeZerosCompiledConditionalBranchProjectionAccounting(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())

	createDurableJob(t, store, "job-compiled-conditional", "run-compiled-conditional")

	node := &workflow.Node{
		ID:   "agg--repair-selection",
		Type: workflow.NodeTypeConditional,
		Metadata: map[string]interface{}{
			"aggregation_group_node_id": "agg--result",
			"aggregation_anchor_id":     "agg",
			"aggregation_method":        "judge",
		},
	}
	output := &runtime.ActivityOutput{
		NodeID:       "agg--repair-selection",
		Output:       "A",
		TokensInput:  10,
		TokensOutput: 5,
		Cost:         0.07,
		LatencyMs:    123,
		Metadata: map[string]interface{}{
			"conditional_branch_node_id": "agg--repair-selection_true",
			"conditional_branch_name":    "true",
		},
	}

	r.saveWorkflowNode("job-compiled-conditional", "run-compiled-conditional", "job-compiled-conditional", node, output, 1, "act-conditional", 0, 2)

	nodes, err := store.GetWorkflowNodes("job-compiled-conditional")
	if err != nil {
		t.Fatalf("GetWorkflowNodes failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].ParentNodeID != "agg--result" {
		t.Fatalf("ParentNodeID = %q, want agg--result", nodes[0].ParentNodeID)
	}
	if nodes[0].TokensInput != 0 || nodes[0].TokensOutput != 0 || nodes[0].Cost != 0 || nodes[0].LatencyMs != 0 {
		t.Fatalf("conditional carrier accounting = tokens %d/%d cost %f latency %f, want zero", nodes[0].TokensInput, nodes[0].TokensOutput, nodes[0].Cost, nodes[0].LatencyMs)
	}
	if nodes[0].Output != "A" {
		t.Fatalf("Output = %q, want A", nodes[0].Output)
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(nodes[0].Metadata), &metadata); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if metadata["conditional_branch_node_id"] != "agg--repair-selection_true" {
		t.Fatalf("conditional branch metadata missing: %+v", metadata)
	}
}

func TestEmit_SaveFailedNode(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())

	createDurableJob(t, store, "job-fail", "run-fail")

	node := &workflow.Node{
		ID:   "prompt-2",
		Type: workflow.NodeTypePrompt,
	}

	r.saveFailedNode("job-fail", "run-fail", "job-fail", node, "something broke", "ERR_TIMEOUT", nil, 2, "act-2", 1, 3)

	nodes, err := store.GetWorkflowNodes("job-fail")
	if err != nil {
		t.Fatalf("GetWorkflowNodes failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].Status != "failed" {
		t.Errorf("status = %q, want failed", nodes[0].Status)
	}
	if nodes[0].ErrorMessage != "something broke" {
		t.Errorf("error_message = %q, want 'something broke'", nodes[0].ErrorMessage)
	}
	if nodes[0].ErrorCode != "ERR_TIMEOUT" {
		t.Errorf("error_code = %q, want ERR_TIMEOUT", nodes[0].ErrorCode)
	}
	if nodes[0].AttemptNumber != 2 {
		t.Errorf("attempt_number = %d, want 2", nodes[0].AttemptNumber)
	}
}

func TestEmit_SaveFailedNodePersistsCompiledAggregationGroupMetadata(t *testing.T) {
	store := newTestStore(t)
	r := NewDAGRuntime(store, runtime.NewActivityHandlerRegistry())

	createDurableJob(t, store, "job-failed-compiled-agg", "run-failed-compiled-agg")

	node := &workflow.Node{
		ID:    "agg--judge",
		Type:  workflow.NodeTypePrompt,
		Model: "judge-model",
		Metadata: map[string]interface{}{
			"aggregation_group_node_id": "agg--result",
			"aggregation_anchor_id":     "agg",
			"aggregation_method":        "judge",
			"source_workflow_id":        "aggregation-judge",
			"source_workflow_hash":      "sha-source",
		},
	}

	r.saveFailedNode("job-failed-compiled-agg", "run-failed-compiled-agg", "job-failed-compiled-agg", node, "failed", "TIMEOUT", nil, 1, "act-failed", 0, 2)

	nodes, err := store.GetWorkflowNodes("job-failed-compiled-agg")
	if err != nil {
		t.Fatalf("GetWorkflowNodes failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].ParentNodeID != "agg--result" {
		t.Fatalf("ParentNodeID = %q, want agg--result", nodes[0].ParentNodeID)
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(nodes[0].Metadata), &metadata); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	for _, key := range []string{"aggregation_anchor_id", "aggregation_method", "source_workflow_id", "source_workflow_hash"} {
		if metadata[key] == nil || metadata[key] == "" {
			t.Fatalf("metadata missing %s: %+v", key, metadata)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func copyMap(m map[string]interface{}) map[string]interface{} {
	cp := make(map[string]interface{}, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

// createDurableJob creates a job with all four required durable fields set.
func createDurableJob(t *testing.T, store *storage.Storage, jobID, runID string) {
	t.Helper()
	job := &storage.Job{
		ID:                  jobID,
		Description:         "test",
		Model:               "workflow",
		Status:              "running",
		WorkflowID:          "wf-test",
		WorkflowExecutionID: jobID,
		RunID:               runID,
		RunNumber:           1,
		DAGHash:             "hash-" + jobID,
		DAGSnapshot:         `{"nodes":[]}`,
	}
	if err := store.CreateExecution(job); err != nil {
		t.Fatalf("failed to create test job: %v", err)
	}
}
