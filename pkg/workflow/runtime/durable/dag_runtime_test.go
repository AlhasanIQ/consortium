package durable

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

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
	dagRuntime.sleepFn = func(context.Context, time.Duration) bool { return true }

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

	ctx, cancel := context.WithCancel(context.Background())
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
		EventCallback: func(event *runtime.ExecutionEvent) {
			if event.Type == events.EventNodeStart {
				cancel()
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

func TestDAGRuntime_Start_DiamondWaitsForAllDependenciesAndPassesTheirOutputs(t *testing.T) {
	store := newTestStore(t)
	registry := runtime.NewActivityHandlerRegistry()
	handler := &contextRecordingLLMHandler{contexts: make(map[string]map[string]interface{})}
	registry.Register(handler)
	dagRuntime := NewDAGRuntime(store, registry)

	wf := &workflow.Workflow{
		ID:   "wf-diamond-context",
		Name: "Diamond Context",
		Nodes: []*workflow.Node{
			{ID: "left", Type: workflow.NodeTypePrompt, Model: "m", Prompt: "left", RetryPolicy: &workflow.RetryPolicy{MaxAttempts: 1}},
			{ID: "right", Type: workflow.NodeTypePrompt, Model: "m", Prompt: "right", RetryPolicy: &workflow.RetryPolicy{MaxAttempts: 1}},
			{ID: "join", Type: workflow.NodeTypePrompt, Model: "m", Prompt: "join", RetryPolicy: &workflow.RetryPolicy{MaxAttempts: 1}},
		},
		Edges: []*workflow.Edge{
			{Source: "left", Target: "join"},
			{Source: "right", Target: "join"},
		},
	}
	snapshot := freezeWorkflowForDurableTest(t, wf)
	jobID := "job-diamond-context"
	createDurableJobForTest(t, store, jobID, wf, snapshot)

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
		t.Fatalf("Start failed: %v", err)
	}

	joinContext := handler.ContextFor("join")
	if joinContext == nil {
		t.Fatal("expected join activity to execute")
	}
	if got := joinContext["left"]; got != "output-left" {
		t.Fatalf("join context left = %v, want output-left", got)
	}
	if got := joinContext["right"]; got != "output-right" {
		t.Fatalf("join context right = %v, want output-right", got)
	}

	history, err := store.GetHistoryEvents(context.Background(), jobID)
	if err != nil {
		t.Fatalf("GetHistoryEvents failed: %v", err)
	}
	for _, nodeID := range []string{"left", "right", "join"} {
		if got := countHistoryByTypeAndNode(history, string(runtime.HistoryActivityStarted), nodeID); got != 1 {
			t.Errorf("%s activity_started count = %d, want 1", nodeID, got)
		}
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
	dagRuntime.sleepFn = func(context.Context, time.Duration) bool { return true }

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
	dagRuntime.sleepFn = func(context.Context, time.Duration) bool { return true }

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
// MaxParallel clamping in dispatchAndCollect
// ---------------------------------------------------------------------------

func TestDAGRuntime_Start_MaxParallelNodesRespected(t *testing.T) {
	store := newTestStore(t)
	registry := runtime.NewActivityHandlerRegistry()

	var mu sync.Mutex
	maxConcurrent := 0
	currentConcurrent := 0
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	handler := &trackingLLMHandler{
		onExecute: func() {
			mu.Lock()
			currentConcurrent++
			if currentConcurrent > maxConcurrent {
				maxConcurrent = currentConcurrent
			}
			mu.Unlock()
			started <- struct{}{}
			<-release
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
		// At least one explicit edge prevents FreezeWorkflow from injecting
		// default linear dependencies; n1, n2, and n3 are initially ready.
		Edges: []*workflow.Edge{{Source: "n1", Target: "n4"}},
	}
	snapshot := freezeWorkflowForDurableTest(t, wf)
	jobID := "job-max-parallel"
	createDurableJobForTest(t, store, jobID, wf, snapshot)

	startDone := make(chan error, 1)
	go func() {
		startDone <- dagRuntime.Start(context.Background(), &runtime.StartParams{
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
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case err := <-startDone:
			t.Fatalf("Start returned before the configured parallel activities started: %v", err)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for the configured parallel activities to start")
		}
	}
	select {
	case <-started:
		t.Fatal("a third activity started before either configured slot was released")
	default:
	}
	close(release)

	select {
	case err := <-startDone:
		if err != nil {
			t.Fatalf("Start failed: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the workflow to finish")
	}

	mu.Lock()
	observedMax := maxConcurrent
	mu.Unlock()
	if observedMax != 2 {
		t.Errorf("max concurrent = %d, expected exactly 2", observedMax)
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

type contextRecordingLLMHandler struct {
	mu       sync.Mutex
	contexts map[string]map[string]interface{}
}

func (h *contextRecordingLLMHandler) Type() runtime.ActivityType {
	return runtime.ActivityTypeLLMCall
}

func (h *contextRecordingLLMHandler) Execute(ctx context.Context, input *runtime.ActivityInput) (*runtime.ActivityOutput, error) {
	h.mu.Lock()
	contextCopy := make(map[string]interface{}, len(input.WorkflowContext))
	for key, value := range input.WorkflowContext {
		contextCopy[key] = value
	}
	h.contexts[input.NodeID] = contextCopy
	h.mu.Unlock()

	return &runtime.ActivityOutput{
		NodeID:  input.NodeID,
		Success: true,
		Output:  "output-" + input.NodeID,
	}, nil
}

func (h *contextRecordingLLMHandler) ContextFor(nodeID string) map[string]interface{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.contexts[nodeID]
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
