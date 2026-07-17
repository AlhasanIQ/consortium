package durable

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

type parallelFailureQuiescenceHandler struct {
	mu             sync.Mutex
	started        int
	allStarted     chan struct{}
	cancelObserved chan struct{}
	permitExit     chan struct{}
	permitExitOnce sync.Once
	siblingExited  chan struct{}
	downstreamRuns int
}

func newParallelFailureQuiescenceHandler() *parallelFailureQuiescenceHandler {
	return &parallelFailureQuiescenceHandler{
		allStarted:     make(chan struct{}),
		cancelObserved: make(chan struct{}),
		permitExit:     make(chan struct{}),
		siblingExited:  make(chan struct{}),
	}
}

func (h *parallelFailureQuiescenceHandler) Type() runtime.ActivityType {
	return runtime.ActivityTypeLLMCall
}

func (h *parallelFailureQuiescenceHandler) Execute(ctx context.Context, input *runtime.ActivityInput) (*runtime.ActivityOutput, error) {
	switch input.NodeID {
	case "fails":
		h.markReadySiblingStarted()
		<-h.allStarted
		return &runtime.ActivityOutput{
			NodeID:    input.NodeID,
			Success:   false,
			Error:     "authentication rejected",
			ErrorCode: "AUTHENTICATION",
		}, nil
	case "blocked":
		h.markReadySiblingStarted()
		<-h.allStarted
		<-ctx.Done()
		close(h.cancelObserved)
		<-h.permitExit
		close(h.siblingExited)
		return &runtime.ActivityOutput{
			NodeID:    input.NodeID,
			Success:   false,
			Error:     ctx.Err().Error(),
			ErrorCode: "CANCELLED",
		}, nil
	case "downstream":
		h.mu.Lock()
		h.downstreamRuns++
		h.mu.Unlock()
		return &runtime.ActivityOutput{NodeID: input.NodeID, Success: true, Output: "unexpected"}, nil
	default:
		return &runtime.ActivityOutput{NodeID: input.NodeID, Success: true, Output: "ok"}, nil
	}
}

func (h *parallelFailureQuiescenceHandler) markReadySiblingStarted() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.started++
	if h.started == 2 {
		close(h.allStarted)
	}
}

func (h *parallelFailureQuiescenceHandler) downstreamCallCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.downstreamRuns
}

func (h *parallelFailureQuiescenceHandler) releaseSibling() {
	h.permitExitOnce.Do(func() { close(h.permitExit) })
}

func TestDAGRuntime_ParallelFailureQuiescesInFlightSibling(t *testing.T) {
	store := newTestStore(t)
	handler := newParallelFailureQuiescenceHandler()
	registry := runtime.NewActivityHandlerRegistry()
	registry.Register(handler)
	dagRuntime := NewDAGRuntime(store, registry)

	wf := &workflow.Workflow{
		ID:   "wf-parallel-failure-quiescence",
		Name: "Parallel Failure Quiescence",
		Nodes: []*workflow.Node{
			{ID: "blocked", Type: workflow.NodeTypePrompt, Model: "mock-model", Prompt: "block"},
			{ID: "fails", Type: workflow.NodeTypePrompt, Model: "mock-model", Prompt: "fail"},
			{ID: "downstream", Type: workflow.NodeTypePrompt, Model: "mock-model", Prompt: "must not run"},
		},
		Edges: []*workflow.Edge{
			{Source: "blocked", Target: "downstream"},
			{Source: "fails", Target: "downstream"},
		},
	}
	snapshot := freezeWorkflowForDurableTest(t, wf)
	jobID := "job-parallel-failure-quiescence"
	createDurableJobForTest(t, store, jobID, wf, snapshot)

	testCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	t.Cleanup(handler.releaseSibling)
	startDone := make(chan error, 1)
	go func() {
		startDone <- dagRuntime.Start(testCtx, &runtime.StartParams{
			Identity: &runtime.ExecutionIdentity{
				WorkflowExecutionID: jobID,
				RunID:               jobID,
				RunNumber:           1,
				DAGHash:             snapshot.DAGHash,
			},
			Snapshot: snapshot,
			Workflow: wf,
		})
	}()

	select {
	case <-handler.allStarted:
	case <-testCtx.Done():
		t.Fatal("ready siblings did not both start")
	}
	select {
	case <-handler.cancelObserved:
	case <-testCtx.Done():
		t.Fatal("terminal sibling failure did not cancel the in-flight sibling")
	}
	jobWhileQuiescing, err := store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("load job while sibling is exiting: %v", err)
	}
	if jobWhileQuiescing.Status != events.JobStatusRunning {
		t.Fatalf("job finalized as %q before the cancelled sibling exited", jobWhileQuiescing.Status)
	}

	select {
	case err := <-startDone:
		t.Fatalf("Start returned before the cancelled sibling exited: %v", err)
	default:
	}
	handler.releaseSibling()

	select {
	case <-handler.siblingExited:
	case <-testCtx.Done():
		t.Fatal("cancelled sibling did not exit")
	}
	select {
	case err := <-startDone:
		if err != nil {
			t.Fatalf("Start returned unexpected runtime error: %v", err)
		}
	case <-testCtx.Done():
		t.Fatal("Start did not return after every launched activity exited")
	}

	job, err := store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("load failed job: %v", err)
	}
	if job.Status != events.JobStatusFailed {
		t.Fatalf("job status = %q, want failed", job.Status)
	}
	if handler.downstreamCallCount() != 0 {
		t.Fatalf("downstream node executed %d times after an upstream failure", handler.downstreamCallCount())
	}

	attempts, err := store.ListNodeExecutionAttemptsByJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("list node attempts: %v", err)
	}
	if len(attempts) != 2 {
		t.Fatalf("node attempts = %d, want exactly the two ready siblings: %+v", len(attempts), attempts)
	}
	statuses := make(map[string]string, len(attempts))
	for _, attempt := range attempts {
		statuses[attempt.NodeID] = attempt.Status
		if attempt.Status == events.JobStatusRunning {
			t.Fatalf("terminal workflow retained running attempt: %+v", attempt)
		}
	}
	if statuses["fails"] != events.JobStatusFailed {
		t.Fatalf("failing attempt status = %q, want failed", statuses["fails"])
	}
	if statuses["blocked"] != events.JobStatusCancelled {
		t.Fatalf("quiesced sibling status = %q, want cancelled", statuses["blocked"])
	}
	if _, ok := statuses["downstream"]; ok {
		t.Fatalf("downstream attempt was persisted despite failed dependency: %+v", attempts)
	}

	history, err := store.GetHistoryEvents(context.Background(), jobID)
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	terminalFailures := 0
	otherTerminals := 0
	for _, event := range history {
		switch runtime.HistoryEventType(event.Type) {
		case runtime.HistoryWorkflowFailed:
			terminalFailures++
		case runtime.HistoryWorkflowCompleted, runtime.HistoryWorkflowCancelled, runtime.HistoryWorkflowTimedOut:
			otherTerminals++
		}
	}
	if terminalFailures != 1 || otherTerminals != 0 {
		t.Fatalf("terminal history failures=%d other_terminals=%d, want exactly one workflow failure", terminalFailures, otherTerminals)
	}
}
