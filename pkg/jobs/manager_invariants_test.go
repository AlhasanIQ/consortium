package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
)

func TestSubmitWorkflow_ConcurrentIdempotencyKeyCreatesOneExecution(t *testing.T) {
	manager, store := setupManagerTest(t)
	wf := simpleWorkflowWithID("wf-concurrent-idempotency", "Concurrent Idempotency", "mock-model", "same request")

	const submitters = 16
	start := make(chan struct{})
	type outcome struct {
		resp *SubmitWorkflowResponse
		err  error
	}
	results := make(chan outcome, submitters)
	for i := 0; i < submitters; i++ {
		go func() {
			<-start
			resp, err := manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
				Workflow:                wf,
				IdempotencyKey:          "same-idempotency-key",
				DisableRequestHashDedup: true,
				UserID:                  "user-1",
			})
			results <- outcome{resp: resp, err: err}
		}()
	}
	close(start)

	var jobID string
	created := 0
	duplicates := 0
	for i := 0; i < submitters; i++ {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent submit failed: %v", result.err)
		}
		if result.resp == nil || result.resp.JobID == "" {
			t.Fatalf("concurrent submit returned incomplete response: %+v", result.resp)
		}
		if jobID == "" {
			jobID = result.resp.JobID
		} else if result.resp.JobID != jobID {
			t.Fatalf("idempotent requests returned different job ids: %q and %q", jobID, result.resp.JobID)
		}
		if result.resp.Duplicate {
			duplicates++
			if result.resp.DedupReason != DedupReasonIdempotencyKey {
				t.Fatalf("duplicate response reason = %q, want %q", result.resp.DedupReason, DedupReasonIdempotencyKey)
			}
		} else {
			created++
		}
	}

	if created != 1 || duplicates != submitters-1 {
		t.Fatalf("expected one created response and %d duplicates, got created=%d duplicates=%d", submitters-1, created, duplicates)
	}
	jobs, err := store.ListExecutions(submitters + 1)
	if err != nil {
		t.Fatalf("ListExecutions failed: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != jobID {
		t.Fatalf("expected exactly one persisted execution %q, got %+v", jobID, jobs)
	}
}

func TestSubmitWorkflow_RetryPolicyChangeDoesNotDeduplicate(t *testing.T) {
	manager, store := setupManagerTest(t)

	makeWorkflow := func(maxAttempts int) *workflow.Workflow {
		node := strictPromptNode("retry-sensitive-node", "mock-model", "same prompt")
		node.RetryPolicy = &workflow.RetryPolicy{
			MaxAttempts:     maxAttempts,
			BackoffMs:       10,
			RetryableErrors: []string{"RATE_LIMIT"},
		}
		return &workflow.Workflow{
			ID:    "wf-retry-sensitive-dedup",
			Name:  "Retry-sensitive dedup",
			Nodes: []*workflow.Node{node},
		}
	}

	first, err := manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
		Workflow: makeWorkflow(1),
	})
	if err != nil {
		t.Fatalf("submit one-attempt workflow: %v", err)
	}
	second, err := manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
		Workflow: makeWorkflow(3),
	})
	if err != nil {
		t.Fatalf("submit three-attempt workflow: %v", err)
	}

	if second.Duplicate || second.JobID == first.JobID {
		t.Fatalf("retry-policy change was deduplicated: first=%+v second=%+v", first, second)
	}
	firstJob, err := store.GetExecution(first.JobID)
	if err != nil {
		t.Fatalf("load first job: %v", err)
	}
	secondJob, err := store.GetExecution(second.JobID)
	if err != nil {
		t.Fatalf("load second job: %v", err)
	}
	if firstJob.ConfigHash == secondJob.ConfigHash || firstJob.RequestHash == secondJob.RequestHash {
		t.Fatalf("retry-policy change did not alter execution identity: first config=%q request=%q second config=%q request=%q",
			firstJob.ConfigHash, firstJob.RequestHash, secondJob.ConfigHash, secondJob.RequestHash)
	}
}

func TestSubmitWorkflow_IdempotencyKeyIsScopedByUser(t *testing.T) {
	manager, store := setupManagerTest(t)
	wf := simpleWorkflowWithID("wf-user-scoped-idempotency", "User-scoped idempotency", "mock-model", "same request")
	const key = "shared-logical-key"

	submit := func(t *testing.T, userID string) *SubmitWorkflowResponse {
		t.Helper()
		resp, err := manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
			Workflow:                wf,
			IdempotencyKey:          key,
			DisableRequestHashDedup: true,
			UserID:                  userID,
		})
		if err != nil {
			t.Fatalf("submit for %s: %v", userID, err)
		}
		return resp
	}

	userAFirst := submit(t, "user-a")
	userBFirst := submit(t, "user-b")
	if userAFirst.Duplicate || userBFirst.Duplicate || userAFirst.JobID == userBFirst.JobID {
		t.Fatalf("different users did not receive independent executions: user-a=%+v user-b=%+v", userAFirst, userBFirst)
	}

	userASecond := submit(t, "user-a")
	userBSecond := submit(t, "user-b")
	if !userASecond.Duplicate || userASecond.JobID != userAFirst.JobID {
		t.Fatalf("user A did not deduplicate to its own execution: first=%+v second=%+v", userAFirst, userASecond)
	}
	if !userBSecond.Duplicate || userBSecond.JobID != userBFirst.JobID {
		t.Fatalf("user B did not deduplicate to its own execution: first=%+v second=%+v", userBFirst, userBSecond)
	}

	jobs, err := store.ListExecutions(10)
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("persisted executions = %d, want 2: %+v", len(jobs), jobs)
	}
}

func TestRetryJobUsesPersistedRequestSnapshotAndParentIdentity(t *testing.T) {
	manager, store := setupManagerTest(t)
	original := simpleWorkflowWithID("wf-retry-snapshot", "Retry Snapshot", "mock-model", "original prompt")
	original.Context = map[string]interface{}{"input": "persisted-value"}

	submitted, err := manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
		Workflow:          original,
		ForceNewRun:       true,
		UserID:            "user-42",
		ParentExecutionID: "parent-execution-42",
	})
	if err != nil {
		t.Fatalf("initial submit failed: %v", err)
	}
	source, err := store.GetExecution(submitted.JobID)
	if err != nil {
		t.Fatalf("load source execution: %v", err)
	}
	source.Status = events.JobStatusFailed
	source.ErrorMessage = "provider failed"
	if err := store.UpdateExecution(source); err != nil {
		t.Fatalf("mark source execution failed: %v", err)
	}

	retry, err := manager.RetryJob(context.Background(), submitted.JobID, RetryJobOptions{})
	if err != nil {
		t.Fatalf("RetryJob failed: %v", err)
	}
	if retry == nil || retry.JobID == "" || retry.JobID == submitted.JobID {
		t.Fatalf("RetryJob did not create a fresh execution: %+v", retry)
	}

	retried, err := store.GetExecution(retry.JobID)
	if err != nil {
		t.Fatalf("load retried execution: %v", err)
	}
	if retried.Status != events.JobStatusPending {
		t.Fatalf("retried execution status = %q, want pending", retried.Status)
	}
	if retried.UserID != source.UserID || retried.ParentExecutionID != source.ParentExecutionID {
		t.Fatalf("retry identity not preserved: user=%q parent=%q, want user=%q parent=%q", retried.UserID, retried.ParentExecutionID, source.UserID, source.ParentExecutionID)
	}
	if retried.IdempotencyKey != "" {
		t.Fatalf("retry unexpectedly reused idempotency key %q", retried.IdempotencyKey)
	}
	if retried.DAGHash != source.DAGHash {
		t.Fatalf("retry changed frozen workflow hash: source=%q retry=%q", source.DAGHash, retried.DAGHash)
	}

	var retriedWorkflow workflow.Workflow
	if err := json.Unmarshal([]byte(retried.RequestData), &retriedWorkflow); err != nil {
		t.Fatalf("decode retry request_data: %v", err)
	}
	if retriedWorkflow.ID != original.ID || retriedWorkflow.Context["input"] != "persisted-value" {
		t.Fatalf("retry did not use persisted workflow snapshot: %+v", retriedWorkflow)
	}
}

func TestExecuteWorkflow_RetryPreservesAttemptsAndFinalResult(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	provider := &mockProvider{
		name:   "mock",
		models: []providers.Model{defaultMockModel()},
		completeFunc: func(_ context.Context, _ *providers.CompletionRequest) (*providers.CompletionResponse, error) {
			mu.Lock()
			calls++
			call := calls
			mu.Unlock()
			if call == 1 {
				return nil, &providers.ProviderError{
					Code:      providers.ErrCodeRateLimited,
					Message:   "try again",
					Retryable: true,
				}
			}
			return &providers.CompletionResponse{
				Content: "retry-success",
				Usage: providers.Usage{
					PromptTokens:     3,
					CompletionTokens: 4,
					TotalTokens:      7,
				},
			}, nil
		},
	}
	manager, store := setupManagerWithProvider(t, provider)
	startWorkers(t, manager)

	node := strictPromptNode("retry-node", "mock-model", "retry me")
	node.RetryPolicy = &workflow.RetryPolicy{
		MaxAttempts:     2,
		RetryableErrors: []string{"RATE_LIMIT"},
	}
	result, err := manager.ExecuteWorkflow(context.Background(), &workflow.Workflow{
		ID:    "wf-manager-retry",
		Name:  "Manager Retry",
		Nodes: []*workflow.Node{node},
	})
	if err != nil {
		t.Fatalf("ExecuteWorkflow returned error: %v", err)
	}
	if result == nil || !result.Success || result.Result == nil || result.Result.FinalOutput != "retry-success" {
		t.Fatalf("retry result = %+v, want successful persisted second-attempt output", result)
	}
	mu.Lock()
	observedCalls := calls
	mu.Unlock()
	if observedCalls != 2 {
		t.Fatalf("provider calls = %d, want exactly two attempts", observedCalls)
	}

	attempts, err := store.GetNodeExecutionAttemptsForNode(result.JobID, "retry-node")
	if err != nil {
		t.Fatalf("load retry attempts: %v", err)
	}
	if len(attempts) != 2 {
		t.Fatalf("persisted attempts = %d, want 2: %+v", len(attempts), attempts)
	}
	if attempts[0].Attempt != 1 || attempts[0].Status != events.JobStatusFailed || attempts[0].ErrorCode != "RATE_LIMIT" {
		t.Fatalf("first attempt did not preserve retry failure: %+v", attempts[0])
	}
	if attempts[1].Attempt != 2 || attempts[1].Status != events.JobStatusCompleted {
		t.Fatalf("second attempt did not complete: %+v", attempts[1])
	}

	nodes, err := store.GetWorkflowNodes(result.JobID)
	if err != nil {
		t.Fatalf("load current node projection: %v", err)
	}
	if len(nodes) != 1 || nodes[0].AttemptNumber != 2 || nodes[0].Status != events.JobStatusCompleted || nodes[0].Output != "retry-success" {
		t.Fatalf("current node projection lost retry result: %+v", nodes)
	}
}

func TestExecuteWorkflow_TracksChildExecutionAndPreservesChildOutput(t *testing.T) {
	manager, store := setupManagerTest(t)
	startWorkers(t, manager)

	child := simpleWorkflowWithID("wf-child-tracked", "Tracked Child", "mock-model", "child prompt")
	childJSON, err := json.Marshal(child)
	if err != nil {
		t.Fatalf("marshal child workflow: %v", err)
	}
	if err := store.CreateWorkflow(&storage.WorkflowDefinition{
		ID:         child.ID,
		Name:       child.Name,
		Definition: string(childJSON),
	}); err != nil {
		t.Fatalf("store child workflow definition: %v", err)
	}

	parent := &workflow.Workflow{
		ID:   "wf-parent-tracks-child",
		Name: "Parent Tracks Child",
		Nodes: []*workflow.Node{
			{
				ID:              "child-node",
				Type:            workflow.NodeTypeChildWorkflow,
				ChildWorkflowID: child.ID,
				TimeoutSeconds:  5,
				RetryPolicy:     workflow.NoRetryPolicy(),
			},
			strictResultNode("result", []string{"child-node"}, "final"),
		},
		Edges: []*workflow.Edge{{Source: "child-node", Target: "result"}},
	}
	result, err := manager.ExecuteWorkflow(context.Background(), parent)
	if err != nil {
		t.Fatalf("parent ExecuteWorkflow returned error: %v", err)
	}
	if result == nil || !result.Success || result.Result == nil {
		t.Fatalf("parent result = %+v, want success", result)
	}
	if got := result.Result.Outputs["final"]; got != "Mock response" {
		t.Fatalf("parent named output = %#v, want child output", got)
	}

	executions, err := store.ListExecutions(10)
	if err != nil {
		t.Fatalf("list parent and child executions: %v", err)
	}
	var childExecution *storage.WorkflowExecution
	for i := range executions {
		if executions[i].WorkflowID == child.ID {
			childExecution = &executions[i]
			break
		}
	}
	if childExecution == nil {
		t.Fatalf("child execution was not persisted: %+v", executions)
	}
	if childExecution.Status != events.JobStatusCompleted || childExecution.ParentExecutionID != result.JobID {
		t.Fatalf("child execution lifecycle/linkage = status=%q parent=%q, want completed/%q", childExecution.Status, childExecution.ParentExecutionID, result.JobID)
	}
	if childExecution.ResultText != "Mock response" {
		t.Fatalf("child result_text = %q, want preserved child output", childExecution.ResultText)
	}
}

func TestSubmitWorkflow_AdmissionIsReusableAfterExecutionCompletes(t *testing.T) {
	cfg := DefaultManagerConfig()
	cfg.MaxConcurrentWorkflows = 1
	cfg.WorkerCount = 1
	cfg.WorkerInitialCount = 1
	cfg.WorkerPollInterval = time.Millisecond
	manager, store := setupManagerWithConfig(t, cfg)
	startWorkers(t, manager)

	for i := 1; i <= 2; i++ {
		wf := simpleWorkflowWithID(fmt.Sprintf("wf-admission-reuse-%d", i), "Admission Reuse", "mock-model", fmt.Sprintf("prompt-%d", i))
		resp, err := manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
			Workflow:    wf,
			ForceNewRun: true,
		})
		if err != nil {
			t.Fatalf("submit %d failed: %v", i, err)
		}
		waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		result, err := manager.WaitForCompletion(waitCtx, resp.JobID, wf.ID)
		cancel()
		if err != nil {
			t.Fatalf("wait for execution %d failed: %v", i, err)
		}
		if result == nil || !result.Success {
			t.Fatalf("execution %d did not complete successfully: %+v", i, result)
		}
		job, err := store.GetExecution(resp.JobID)
		if err != nil {
			t.Fatalf("load execution %d: %v", i, err)
		}
		if job.Status != events.JobStatusCompleted {
			t.Fatalf("execution %d status = %q, want completed", i, job.Status)
		}
	}
}

func TestStopWorkers_CancelsActiveExecutionAndLeavesNoRunningWorker(t *testing.T) {
	started := make(chan struct{})
	var startOnce sync.Once
	provider := &mockProvider{
		name:   "blocking",
		models: []providers.Model{{ID: "blocking-model", Provider: "blocking"}},
		completeFunc: func(ctx context.Context, _ *providers.CompletionRequest) (*providers.CompletionResponse, error) {
			startOnce.Do(func() { close(started) })
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	store, err := storage.NewStorage(filepath.Join(t.TempDir(), "shutdown.db"))
	if err != nil {
		t.Fatalf("create shutdown test storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	registry := providers.NewRegistry()
	registry.Register(provider)
	cfg := DefaultManagerConfig()
	cfg.MaxConcurrentWorkflows = 1
	cfg.WorkerCount = 1
	cfg.WorkerInitialCount = 1
	cfg.WorkerPollInterval = time.Millisecond
	manager := NewManagerWithConfig(store, registry, cfg)
	t.Cleanup(func() { manager.StopWorkers(context.Background()) })

	wf := simpleWorkflowWithID("wf-shutdown-cancel", "Shutdown Cancel", "blocking-model", "wait")
	resp, err := manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
		Workflow:    wf,
		ForceNewRun: true,
	})
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	manager.StartWorkers()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("worker never entered the active provider call")
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	manager.StopWorkers(stopCtx)
	cancel()

	job, err := store.GetExecution(resp.JobID)
	if err != nil {
		t.Fatalf("load shutdown execution: %v", err)
	}
	if job.Status != events.JobStatusCancelled {
		t.Fatalf("shutdown execution status = %q, want cancelled", job.Status)
	}
	if manager.IsRunning(resp.JobID) {
		t.Fatal("shutdown left execution registered as running")
	}
	if stats := manager.WorkerStats(); stats.ActiveWorkers != 0 || stats.BusyWorkers != 0 {
		t.Fatalf("shutdown left worker counters active: %+v", stats)
	}

	_, err = manager.ExecuteWorkflow(context.Background(), simpleWorkflow("after shutdown", "blocking-model", "not submitted"))
	if !errors.Is(err, ErrWorkersNotStarted) {
		t.Fatalf("ExecuteWorkflow after shutdown error = %v, want ErrWorkersNotStarted", err)
	}
}
