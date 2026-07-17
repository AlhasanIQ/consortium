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

type scriptedNode struct {
	success          bool
	output           string
	err              string
	errorCode        string
	delay            time.Duration
	blockUntilCancel bool
	heartbeatEvery   time.Duration
}

type scriptedActivityHandler struct {
	mu    sync.Mutex
	calls int
	nodes []scriptedNode
}

func newScriptedLLMHandler(nodes ...scriptedNode) *scriptedActivityHandler {
	return &scriptedActivityHandler{nodes: nodes}
}

func (h *scriptedActivityHandler) Type() runtime.ActivityType {
	return runtime.ActivityTypeLLMCall
}

func (h *scriptedActivityHandler) Execute(ctx context.Context, input *runtime.ActivityInput) (*runtime.ActivityOutput, error) {
	h.mu.Lock()
	h.calls++
	callNum := h.calls
	h.mu.Unlock()

	scripted := scriptedNode{success: true, output: "ok"}
	if len(h.nodes) > 0 {
		idx := callNum - 1
		if idx >= len(h.nodes) {
			idx = len(h.nodes) - 1
		}
		scripted = h.nodes[idx]
	}

	if scripted.heartbeatEvery > 0 {
		reporter := HeartbeatFromContext(ctx)
		if reporter != nil {
			stop := make(chan struct{})
			defer close(stop)
			go func() {
				ticker := time.NewTicker(scripted.heartbeatEvery)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-stop:
						return
					case <-ticker.C:
						reporter.Beat(map[string]interface{}{"call": callNum})
					}
				}
			}()
		}
	}

	if scripted.blockUntilCancel {
		<-ctx.Done()
		return &runtime.ActivityOutput{
			NodeID:    input.NodeID,
			Success:   false,
			Error:     ctx.Err().Error(),
			ErrorCode: "CANCELLED",
		}, nil
	}

	if scripted.delay > 0 {
		select {
		case <-time.After(scripted.delay):
		case <-ctx.Done():
			return &runtime.ActivityOutput{
				NodeID:    input.NodeID,
				Success:   false,
				Error:     ctx.Err().Error(),
				ErrorCode: "CANCELLED",
			}, nil
		}
	}

	if scripted.success {
		return &runtime.ActivityOutput{
			NodeID:  input.NodeID,
			Success: true,
			Output:  scripted.output,
		}, nil
	}

	errMsg := scripted.err
	if errMsg == "" {
		errMsg = "scripted failure"
	}
	return &runtime.ActivityOutput{
		NodeID:    input.NodeID,
		Success:   false,
		Error:     errMsg,
		ErrorCode: scripted.errorCode,
	}, nil
}

func (h *scriptedActivityHandler) CallCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls
}

type costInjectingLLMHandler struct {
	mu    sync.Mutex
	calls int
	cost  float64
}

func (h *costInjectingLLMHandler) Type() runtime.ActivityType {
	return runtime.ActivityTypeLLMCall
}

func (h *costInjectingLLMHandler) Execute(ctx context.Context, input *runtime.ActivityInput) (*runtime.ActivityOutput, error) {
	h.mu.Lock()
	h.calls++
	h.mu.Unlock()

	if input.CostTracker != nil {
		input.CostTracker.Add(10, 10, h.cost)
	}

	return &runtime.ActivityOutput{
		NodeID:       input.NodeID,
		Success:      true,
		Output:       "expensive output",
		TokensInput:  10,
		TokensOutput: 10,
		Cost:         h.cost,
	}, nil
}

func (h *costInjectingLLMHandler) CallCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls
}

func freezeWorkflowForDurableTest(t *testing.T, wf *workflow.Workflow) *runtime.FrozenSnapshot {
	t.Helper()

	nodes := make([]runtime.NodeForFreeze, len(wf.Nodes))
	for i, n := range wf.Nodes {
		nodes[i] = runtime.NodeForFreeze{
			ID:                n.ID,
			Type:              string(n.Type),
			Model:             n.Model,
			Prompt:            n.Prompt,
			SystemPrompt:      n.SystemPrompt,
			Temperature:       n.Temperature,
			MaxTokens:         n.MaxTokens,
			TimeoutSeconds:    n.TimeoutSeconds,
			TaskID:            n.TaskID,
			TaskSummary:       n.TaskSummary,
			Identity:          n.Identity,
			Image:             n.Image,
			Sandbox:           n.Sandbox,
			RuntimeURL:        n.RuntimeURL,
			GraceSeconds:      n.GraceSeconds,
			RepoSpecs:         n.RepoSpecs,
			WorkSource:        n.WorkSource,
			Condition:         n.Condition,
			OutputName:        n.OutputName,
			AggregationMethod: string(n.AggregationMethod),
			AggregationConfig: n.AggregationConfig,
			Metadata:          n.Metadata,
		}
	}

	edges := make([]runtime.EdgeForFreeze, len(wf.Edges))
	for i, e := range wf.Edges {
		edges[i] = runtime.EdgeForFreeze{Source: e.Source, Target: e.Target}
	}

	snapshot, err := runtime.FreezeWorkflow(wf.ID, wf.Name, wf.Description, nodes, edges, wf.Context, wf.Limits)
	if err != nil {
		t.Fatalf("freeze workflow failed: %v", err)
	}
	return snapshot
}

func createDurableJobForTest(t *testing.T, store *storage.Storage, jobID string, wf *workflow.Workflow, snapshot *runtime.FrozenSnapshot) {
	t.Helper()

	job := &storage.Job{
		ID:                  jobID,
		Description:         fmt.Sprintf("Test job %s", jobID),
		Model:               "workflow",
		Status:              events.JobStatusPending,
		WorkflowID:          wf.ID,
		WorkflowExecutionID: jobID,
		RunID:               jobID,
		RunNumber:           1,
		DAGSnapshot:         string(snapshot.Definition),
		DAGHash:             snapshot.DAGHash,
	}
	if err := store.CreateExecution(job); err != nil {
		t.Fatalf("create job failed: %v", err)
	}
}

func countHistoryByType(events []*storage.HistoryEvent, eventType string) int {
	count := 0
	for _, e := range events {
		if e.Type == eventType {
			count++
		}
	}
	return count
}

func countHistoryByTypeAndNode(events []*storage.HistoryEvent, eventType, nodeID string) int {
	count := 0
	for _, e := range events {
		if e.Type == eventType && e.NodeID == nodeID {
			count++
		}
	}
	return count
}

func hasHistoryType(events []*storage.HistoryEvent, eventType string) bool {
	for _, e := range events {
		if e.Type == eventType {
			return true
		}
	}
	return false
}

func countEventType(types []string, typ string) int {
	count := 0
	for _, t := range types {
		if t == typ {
			count++
		}
	}
	return count
}

func TestDAGRuntime_RetryExhaustionEmitsEventsAndFails(t *testing.T) {
	store := newTestStore(t)
	registry := runtime.NewActivityHandlerRegistry()
	handler := newScriptedLLMHandler(
		scriptedNode{success: false, err: "TIMEOUT: first failure", errorCode: "TIMEOUT"},
		scriptedNode{success: false, err: "TIMEOUT: second failure", errorCode: "TIMEOUT"},
		scriptedNode{success: false, err: "TIMEOUT: third failure", errorCode: "TIMEOUT"},
	)
	registry.Register(handler)
	dagRuntime := NewDAGRuntime(store, registry)
	dagRuntime.sleepFn = func(context.Context, time.Duration) bool { return true }

	wf := &workflow.Workflow{
		ID:   "wf-retry-exhaust",
		Name: "Retry Exhaustion",
		Nodes: []*workflow.Node{
			{
				ID:     "n1",
				Type:   workflow.NodeTypePrompt,
				Model:  "mock-model",
				Prompt: "hello",
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
	jobID := "job-retry-exhaust"
	createDurableJobForTest(t, store, jobID, wf, snapshot)

	var mu sync.Mutex
	var streamTypes []string

	err := dagRuntime.Start(context.Background(), &runtime.StartParams{
		Identity: &runtime.ExecutionIdentity{
			WorkflowExecutionID: jobID,
			RunID:               jobID,
			RunNumber:           1,
			DAGHash:             snapshot.DAGHash,
		},
		Snapshot: snapshot,
		Workflow: wf,
		EventCallback: func(event *runtime.ExecutionEvent) {
			mu.Lock()
			streamTypes = append(streamTypes, event.Type)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("runtime start failed: %v", err)
	}

	if handler.CallCount() != 3 {
		t.Fatalf("expected 3 attempts, got %d", handler.CallCount())
	}

	job, err := store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("failed to get job: %v", err)
	}
	if job.Status != events.JobStatusFailed {
		t.Fatalf("expected failed status, got %s", job.Status)
	}

	mu.Lock()
	if countEventType(streamTypes, events.EventNodeRetryBackoff) != 2 {
		t.Errorf("expected 2 retry_backoff events, got %d", countEventType(streamTypes, events.EventNodeRetryBackoff))
	}
	if countEventType(streamTypes, events.EventNodeRetryStart) != 2 {
		t.Errorf("expected 2 retry_start events, got %d", countEventType(streamTypes, events.EventNodeRetryStart))
	}
	if countEventType(streamTypes, events.EventNodeRetryExhausted) != 1 {
		t.Errorf("expected 1 retry_exhausted event, got %d", countEventType(streamTypes, events.EventNodeRetryExhausted))
	}
	mu.Unlock()

	history, err := store.GetHistoryEvents(context.Background(), jobID)
	if err != nil {
		t.Fatalf("failed to get history events: %v", err)
	}
	if countHistoryByType(history, string(runtime.HistoryActivityStarted)) != 3 {
		t.Errorf("expected 3 activity_started events, got %d", countHistoryByType(history, string(runtime.HistoryActivityStarted)))
	}
	if countHistoryByType(history, string(runtime.HistoryActivityFailed)) != 3 {
		t.Errorf("expected 3 activity_failed events, got %d", countHistoryByType(history, string(runtime.HistoryActivityFailed)))
	}
	if !hasHistoryType(history, string(runtime.HistoryNodeFailed)) {
		t.Error("expected node_failed in history")
	}
	if !hasHistoryType(history, string(runtime.HistoryWorkflowFailed)) {
		t.Error("expected workflow_failed in history")
	}
}

func TestDAGRuntime_CancelDuringBackoffViaRuntimeCancel(t *testing.T) {
	store := newTestStore(t)
	registry := runtime.NewActivityHandlerRegistry()
	handler := newScriptedLLMHandler(scriptedNode{success: false, err: "TIMEOUT: fail", errorCode: "TIMEOUT"})
	registry.Register(handler)
	dagRuntime := NewDAGRuntime(store, registry)
	backoffEntered := make(chan struct{}, 1)
	dagRuntime.sleepFn = func(ctx context.Context, _ time.Duration) bool {
		backoffEntered <- struct{}{}
		<-ctx.Done()
		return false
	}

	wf := &workflow.Workflow{
		ID:   "wf-backoff-cancel",
		Name: "Backoff Cancel",
		Nodes: []*workflow.Node{
			{
				ID:     "n1",
				Type:   workflow.NodeTypePrompt,
				Model:  "mock-model",
				Prompt: "hello",
				RetryPolicy: &workflow.RetryPolicy{
					MaxAttempts:     5,
					BackoffMs:       5000,
					BackoffMultiply: 1.0,
					RetryableErrors: []string{"TIMEOUT"},
				},
			},
		},
	}
	snapshot := freezeWorkflowForDurableTest(t, wf)
	jobID := "job-backoff-cancel"
	createDurableJobForTest(t, store, jobID, wf, snapshot)

	var cancelOnce sync.Once
	err := dagRuntime.Start(context.Background(), &runtime.StartParams{
		Identity: &runtime.ExecutionIdentity{
			WorkflowExecutionID: jobID,
			RunID:               jobID,
			RunNumber:           1,
			DAGHash:             snapshot.DAGHash,
		},
		Snapshot: snapshot,
		Workflow: wf,
		EventCallback: func(event *runtime.ExecutionEvent) {
			if event.Type == events.EventNodeRetryBackoff {
				cancelOnce.Do(func() {
					_ = dagRuntime.Cancel(context.Background(), jobID)
				})
			}
		},
	})
	if err != nil {
		t.Fatalf("runtime start failed: %v", err)
	}

	select {
	case <-backoffEntered:
	default:
		t.Fatal("expected retry backoff to be entered before cancellation")
	}

	if handler.CallCount() != 1 {
		t.Fatalf("expected only first attempt before cancellation, got %d", handler.CallCount())
	}

	job, err := store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("failed to get job: %v", err)
	}
	if job.Status != events.JobStatusCancelled {
		t.Fatalf("expected cancelled status, got %s", job.Status)
	}

	history, err := store.GetHistoryEvents(context.Background(), jobID)
	if err != nil {
		t.Fatalf("failed to get history: %v", err)
	}
	if !hasHistoryType(history, string(runtime.HistoryWorkflowCancelled)) {
		t.Fatal("expected workflow_cancelled in history")
	}
}

func TestDAGRuntime_StuckActivityCancelsCleanly(t *testing.T) {
	store := newTestStore(t)
	registry := runtime.NewActivityHandlerRegistry()
	handler := newScriptedLLMHandler(scriptedNode{blockUntilCancel: true})
	registry.Register(handler)
	dagRuntime := NewDAGRuntime(store, registry)

	wf := &workflow.Workflow{
		ID:   "wf-stuck-cancel",
		Name: "Stuck Cancel",
		Nodes: []*workflow.Node{
			{
				ID:          "n1",
				Type:        workflow.NodeTypePrompt,
				Model:       "mock-model",
				Prompt:      "hello",
				RetryPolicy: &workflow.RetryPolicy{MaxAttempts: 1},
			},
		},
	}
	snapshot := freezeWorkflowForDurableTest(t, wf)
	jobID := "job-stuck-cancel"
	createDurableJobForTest(t, store, jobID, wf, snapshot)

	var cancelOnce sync.Once
	err := dagRuntime.Start(context.Background(), &runtime.StartParams{
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
				cancelOnce.Do(func() {
					_ = dagRuntime.Cancel(context.Background(), jobID)
				})
			}
		},
	})
	if err != nil {
		t.Fatalf("runtime start failed: %v", err)
	}

	if handler.CallCount() != 1 {
		t.Fatalf("expected single stuck attempt, got %d", handler.CallCount())
	}

	job, err := store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("failed to get job: %v", err)
	}
	if job.Status != events.JobStatusCancelled {
		t.Fatalf("expected cancelled status, got %s", job.Status)
	}

	nodeRows, err := store.ListNodeExecutionAttemptsByJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("failed to get node executions: %v", err)
	}
	var foundCancelled bool
	for _, row := range nodeRows {
		if row.RunID == jobID && row.NodeID == "n1" && row.Status == "cancelled" {
			foundCancelled = true
			break
		}
	}
	if !foundCancelled {
		t.Fatalf("expected cancelled node execution row for %s, got %+v", jobID, nodeRows)
	}
}

func TestDAGRuntime_HeartbeatTimeoutFailsActivity(t *testing.T) {
	store := newTestStore(t)
	registry := runtime.NewActivityHandlerRegistry()
	handler := newScriptedLLMHandler(scriptedNode{blockUntilCancel: true})
	registry.Register(handler)
	dagRuntime := NewDAGRuntime(store, registry)

	wf := &workflow.Workflow{
		ID:   "wf-heartbeat-timeout",
		Name: "Heartbeat Timeout",
		Nodes: []*workflow.Node{
			{
				ID:             "n1",
				Type:           workflow.NodeTypePrompt,
				Model:          "mock-model",
				Prompt:         "hello",
				TimeoutSeconds: 2,
				RetryPolicy:    &workflow.RetryPolicy{MaxAttempts: 1},
				Metadata: map[string]interface{}{
					"heartbeat_timeout_ms": 30,
				},
			},
		},
	}
	snapshot := freezeWorkflowForDurableTest(t, wf)
	jobID := "job-heartbeat-timeout"
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
		t.Fatalf("runtime start failed: %v", err)
	}

	job, err := store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("failed to get job: %v", err)
	}
	if job.Status != events.JobStatusFailed {
		t.Fatalf("expected failed status, got %s", job.Status)
	}
	if !strings.Contains(strings.ToLower(job.ErrorMessage), "heartbeat timeout") {
		t.Fatalf("expected heartbeat timeout error, got: %s", job.ErrorMessage)
	}
}

func TestDAGRuntime_HeartbeatBeatsAllowCompletion(t *testing.T) {
	store := newTestStore(t)
	registry := runtime.NewActivityHandlerRegistry()
	handler := newScriptedLLMHandler(scriptedNode{
		success:        true,
		output:         "ok",
		delay:          150 * time.Millisecond,
		heartbeatEvery: 10 * time.Millisecond,
	})
	registry.Register(handler)
	dagRuntime := NewDAGRuntime(store, registry)

	wf := &workflow.Workflow{
		ID:   "wf-heartbeat-success",
		Name: "Heartbeat Success",
		Nodes: []*workflow.Node{
			{
				ID:             "n1",
				Type:           workflow.NodeTypePrompt,
				Model:          "mock-model",
				Prompt:         "hello",
				TimeoutSeconds: 2,
				RetryPolicy:    &workflow.RetryPolicy{MaxAttempts: 1},
				Metadata: map[string]interface{}{
					"heartbeat_timeout_ms": 30,
				},
			},
		},
	}
	snapshot := freezeWorkflowForDurableTest(t, wf)
	jobID := "job-heartbeat-success"
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
		t.Fatalf("runtime start failed: %v", err)
	}

	job, err := store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("failed to get job: %v", err)
	}
	if job.Status != events.JobStatusCompleted {
		t.Fatalf("expected completed status, got %s", job.Status)
	}
}

func TestDAGRuntime_FailsWhenSnapshotNodeMissingFromWorkflow(t *testing.T) {
	store := newTestStore(t)
	registry := runtime.NewActivityHandlerRegistry()
	registry.Register(newScriptedLLMHandler(scriptedNode{success: true, output: "ok"}))
	dagRuntime := NewDAGRuntime(store, registry)

	wf := &workflow.Workflow{
		ID:   "wf-snapshot-mismatch",
		Name: "Snapshot Mismatch",
		Nodes: []*workflow.Node{
			{ID: "different-node", Type: workflow.NodeTypePrompt, Model: "mock-model", Prompt: "hello"},
		},
	}

	snapshot, err := runtime.FreezeWorkflow(
		wf.ID,
		wf.Name,
		wf.Description,
		[]runtime.NodeForFreeze{{ID: "snapshot-node", Type: string(workflow.NodeTypePrompt), Model: "mock-model", Prompt: "hello"}},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("failed to freeze mismatch snapshot: %v", err)
	}

	jobID := "job-snapshot-mismatch"
	createDurableJobForTest(t, store, jobID, wf, snapshot)

	err = dagRuntime.Start(context.Background(), &runtime.StartParams{
		Identity: &runtime.ExecutionIdentity{
			WorkflowExecutionID: jobID,
			RunID:               jobID,
			RunNumber:           1,
			DAGHash:             snapshot.DAGHash,
		},
		Snapshot: snapshot,
		Workflow: wf,
	})
	if err == nil {
		t.Fatal("expected runtime error for snapshot/workflow mismatch")
	}
	if !strings.Contains(err.Error(), "missing from workflow definition") {
		t.Fatalf("unexpected error: %v", err)
	}

	job, err := store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("failed to get job: %v", err)
	}
	if job.Status != events.JobStatusFailed {
		t.Fatalf("expected failed status, got %s", job.Status)
	}

	history, err := store.GetHistoryEvents(context.Background(), jobID)
	if err != nil {
		t.Fatalf("failed to read history: %v", err)
	}
	if !hasHistoryType(history, string(runtime.HistoryWorkflowFailed)) {
		t.Fatal("expected workflow_failed history event")
	}
}

func TestDAGRuntime_UsesCachedActivityResultWithoutReExecutingHandler(t *testing.T) {
	store := newTestStore(t)
	registry := runtime.NewActivityHandlerRegistry()
	handler := newScriptedLLMHandler(scriptedNode{success: true, output: "fresh"})
	registry.Register(handler)
	dagRuntime := NewDAGRuntime(store, registry)

	wf := &workflow.Workflow{
		ID:   "wf-cached-activity",
		Name: "Cached Activity",
		Nodes: []*workflow.Node{
			{
				ID:          "n1",
				Type:        workflow.NodeTypePrompt,
				Model:       "mock-model",
				Prompt:      "hello",
				RetryPolicy: &workflow.RetryPolicy{MaxAttempts: 1},
			},
		},
	}
	snapshot := freezeWorkflowForDurableTest(t, wf)
	jobID := "job-cached-activity"
	createDurableJobForTest(t, store, jobID, wf, snapshot)

	now := time.Now()
	if err := store.SaveActivityResult(context.Background(), &storage.ActivityResult{
		IdempotencyKey: fmt.Sprintf("%s:%s:%d", jobID, "n1", 1),
		RunID:          jobID,
		NodeID:         "n1",
		Attempt:        1,
		ActivityType:   string(runtime.ActivityTypeLLMCall),
		Status:         "completed",
		OutputPayload:  "cached output",
		CompletedAt:    &now,
	}); err != nil {
		t.Fatalf("failed to seed cached activity result: %v", err)
	}

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
		t.Fatalf("runtime start failed: %v", err)
	}

	if handler.CallCount() != 0 {
		t.Fatalf("expected cached result to bypass handler execution, got %d calls", handler.CallCount())
	}

	job, err := store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("failed to get job: %v", err)
	}
	if job.Status != events.JobStatusCompleted {
		t.Fatalf("expected completed status, got %s", job.Status)
	}
	if job.ResultText != "cached output" {
		t.Fatalf("expected cached final output, got %q", job.ResultText)
	}
}

func TestDAGRuntime_NonRetryableErrorSkipsRetryEvenWithPolicy(t *testing.T) {
	store := newTestStore(t)
	registry := runtime.NewActivityHandlerRegistry()
	handler := newScriptedLLMHandler(scriptedNode{
		success:   false,
		err:       "authentication failed",
		errorCode: "AUTHENTICATION",
	})
	registry.Register(handler)
	dagRuntime := NewDAGRuntime(store, registry)

	wf := &workflow.Workflow{
		ID:   "wf-nonretryable",
		Name: "NonRetryable",
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
	jobID := "job-nonretryable"
	createDurableJobForTest(t, store, jobID, wf, snapshot)

	var mu sync.Mutex
	var streamTypes []string
	err := dagRuntime.Start(context.Background(), &runtime.StartParams{
		Identity: &runtime.ExecutionIdentity{
			WorkflowExecutionID: jobID,
			RunID:               jobID,
			RunNumber:           1,
			DAGHash:             snapshot.DAGHash,
		},
		Snapshot: snapshot,
		Workflow: wf,
		EventCallback: func(event *runtime.ExecutionEvent) {
			mu.Lock()
			streamTypes = append(streamTypes, event.Type)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatalf("runtime start failed: %v", err)
	}

	if handler.CallCount() != 1 {
		t.Fatalf("expected exactly one attempt for non-retryable error, got %d", handler.CallCount())
	}

	mu.Lock()
	retryBackoffs := countEventType(streamTypes, events.EventNodeRetryBackoff)
	retryStarts := countEventType(streamTypes, events.EventNodeRetryStart)
	mu.Unlock()
	if retryBackoffs != 0 || retryStarts != 0 {
		t.Fatalf("expected no retry events, got backoff=%d start=%d", retryBackoffs, retryStarts)
	}

	job, err := store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("failed to get job: %v", err)
	}
	if job.Status != events.JobStatusFailed {
		t.Fatalf("expected failed status, got %s", job.Status)
	}
}

func TestDAGRuntime_CostLimitExceededFailsWorkflow(t *testing.T) {
	store := newTestStore(t)
	registry := runtime.NewActivityHandlerRegistry()
	handler := &costInjectingLLMHandler{cost: 2.0}
	registry.Register(handler)
	dagRuntime := NewDAGRuntime(store, registry)

	wf := &workflow.Workflow{
		ID:   "wf-cost-limit",
		Name: "Cost Limit",
		Nodes: []*workflow.Node{
			{
				ID:     "n1",
				Type:   workflow.NodeTypePrompt,
				Model:  "mock-model",
				Prompt: "hello",
				RetryPolicy: &workflow.RetryPolicy{
					MaxAttempts: 1,
				},
			},
		},
		Limits: &workflow.CostLimits{
			MaxCostUSD: 0.5,
		},
	}
	snapshot := freezeWorkflowForDurableTest(t, wf)
	jobID := "job-cost-limit"
	createDurableJobForTest(t, store, jobID, wf, snapshot)

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
	})
	if err != nil {
		t.Fatalf("runtime start failed: %v", err)
	}

	if handler.CallCount() != 1 {
		t.Fatalf("expected one activity call, got %d", handler.CallCount())
	}

	job, err := store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("failed to get job: %v", err)
	}
	if job.Status != events.JobStatusFailed {
		t.Fatalf("expected failed status, got %s", job.Status)
	}
	if !strings.Contains(strings.ToLower(job.ErrorMessage), "cost") {
		t.Fatalf("expected cost-related error, got %q", job.ErrorMessage)
	}

	history, err := store.GetHistoryEvents(context.Background(), jobID)
	if err != nil {
		t.Fatalf("failed to get history events: %v", err)
	}
	foundCostLimitFailure := false
	for _, e := range history {
		if e.Type != string(runtime.HistoryWorkflowFailed) || e.Attributes == nil {
			continue
		}
		if reason, ok := e.Attributes["reason"].(string); ok && reason == "cost_limit_exceeded" {
			foundCostLimitFailure = true
			break
		}
	}
	if !foundCostLimitFailure {
		t.Fatal("expected workflow_failed history event with reason=cost_limit_exceeded")
	}
}
