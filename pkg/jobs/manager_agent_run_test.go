package jobs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/novomo"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
)

type fakeNovomoRunClient struct {
	mu          sync.Mutex
	submitCalls int
	getCalls    int
	stopCalls   int
	submitResp  *novomo.SubmitRunResponse
	getScript   []*novomo.Run
	submitReqs  []novomo.SubmitRunRequest
	stoppedIDs  []string
	stopErr     error
}

type fakeNovoRunClient struct {
	mu          sync.Mutex
	submitCalls int
	getCalls    int
	stopCalls   int
	submitResp  *novomo.SubmitNovoRunResponse
	getScript   []*novomo.NovoRun
	baseURL     string
	submitReqs  []novomo.SubmitNovoRunRequest
	stoppedIDs  []string
	stopErr     error
}

type fakeNovomoStopClient struct {
	mu             sync.Mutex
	stopRunCalls   int
	stopNovoCalls  int
	stoppedRunIDs  []string
	stoppedNovoIDs []string
	stopRunErr     error
	stopNovoErr    error
	onStopRun      func()
}

func (f *fakeNovomoStopClient) StopRun(ctx context.Context, runID string) error {
	f.mu.Lock()
	f.stopRunCalls++
	f.stoppedRunIDs = append(f.stoppedRunIDs, runID)
	err := f.stopRunErr
	onStopRun := f.onStopRun
	f.mu.Unlock()
	if onStopRun != nil {
		onStopRun()
	}
	return err
}

func (f *fakeNovomoStopClient) StopNovoRun(ctx context.Context, runID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopNovoCalls++
	f.stoppedNovoIDs = append(f.stoppedNovoIDs, runID)
	return f.stopNovoErr
}

func (f *fakeNovoRunClient) SubmitNovoRun(ctx context.Context, req novomo.SubmitNovoRunRequest) (*novomo.SubmitNovoRunResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.submitCalls++
	f.submitReqs = append(f.submitReqs, req)
	if f.submitResp != nil {
		return f.submitResp, nil
	}
	return &novomo.SubmitNovoRunResponse{NovoRunID: "nr-1", TaskID: "task-1", Status: novomo.StatusRunning}, nil
}

func (f *fakeNovoRunClient) BaseURL() string {
	return f.baseURL
}

func (f *fakeNovoRunClient) GetNovoRun(ctx context.Context, runID string) (*novomo.NovoRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls++
	if len(f.getScript) == 0 {
		return &novomo.NovoRun{RunID: runID, Status: novomo.StatusRunning}, nil
	}
	next := f.getScript[0]
	f.getScript = f.getScript[1:]
	return next, nil
}

func (f *fakeNovoRunClient) StopNovoRun(ctx context.Context, runID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCalls++
	f.stoppedIDs = append(f.stoppedIDs, runID)
	return f.stopErr
}

func (f *fakeNovomoRunClient) SubmitRun(ctx context.Context, req novomo.SubmitRunRequest) (*novomo.SubmitRunResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.submitCalls++
	f.submitReqs = append(f.submitReqs, req)
	if f.submitResp != nil {
		return f.submitResp, nil
	}
	return &novomo.SubmitRunResponse{RunID: "novomo-1", Status: novomo.StatusRunning}, nil
}

func (f *fakeNovomoRunClient) GetRun(ctx context.Context, runID string) (*novomo.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls++
	if len(f.getScript) == 0 {
		return &novomo.Run{RunID: runID, Status: novomo.StatusRunning}, nil
	}
	next := f.getScript[0]
	f.getScript = f.getScript[1:]
	return next, nil
}

func (f *fakeNovomoRunClient) StopRun(ctx context.Context, runID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCalls++
	f.stoppedIDs = append(f.stoppedIDs, runID)
	return f.stopErr
}

type parallelNovomoRunClient struct {
	mu          sync.Mutex
	submitCalls int
	submitted   map[string]string
}

func (f *parallelNovomoRunClient) SubmitRun(ctx context.Context, req novomo.SubmitRunRequest) (*novomo.SubmitRunResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.submitCalls++
	if f.submitted == nil {
		f.submitted = make(map[string]string)
	}
	runID := fmt.Sprintf("novomo-%d", f.submitCalls)
	f.submitted[req.IdempotencyKey] = runID
	return &novomo.SubmitRunResponse{RunID: runID, Status: novomo.StatusRunning}, nil
}

func (f *parallelNovomoRunClient) GetRun(ctx context.Context, runID string) (*novomo.Run, error) {
	return &novomo.Run{
		RunID:        runID,
		Status:       novomo.StatusCompleted,
		Output:       "done " + runID,
		TokensInput:  1,
		TokensOutput: 2,
		CostUSD:      0.01,
	}, nil
}

func TestManagerExecuteAgentRunSubmitsPollsAndPersists(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-agent")

	client := &fakeNovomoRunClient{
		submitResp: &novomo.SubmitRunResponse{RunID: "novomo-1", Status: novomo.StatusRunning},
		getScript: []*novomo.Run{
			{RunID: "novomo-1", Status: novomo.StatusRunning},
			{
				RunID:        "novomo-1",
				Status:       novomo.StatusCompleted,
				Output:       "agent output",
				TokensInput:  9,
				TokensOutput: 4,
				CostUSD:      0.19,
			},
		},
	}

	result, err := manager.executeAgentRunWithClient(context.Background(), &workflow.AgentRunRequest{
		ParentJobID:       "job-agent",
		ParentExecutionID: "exec-agent",
		ParentRunID:       "run-agent",
		ParentWorkflowID:  "wf-agent",
		ParentNodeID:      "agent-node",
		Attempt:           1,
		Prompt:            "do work",
		Harness:           "claude-code",
		Sandbox:           "docker",
		TimeoutSeconds:    30,
		IdempotencyKey:    "job-agent:agent-node:1",
	}, client, time.Millisecond)
	if err != nil {
		t.Fatalf("executeAgentRunWithClient failed: %v", err)
	}
	if !result.Success || result.Output != "agent output" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.TokensInput != 9 || result.TokensOutput != 4 || result.Cost != 0.19 {
		t.Fatalf("usage not returned: %+v", result)
	}
	if client.submitCalls != 1 || client.getCalls != 2 {
		t.Fatalf("unexpected client calls submit=%d get=%d", client.submitCalls, client.getCalls)
	}
	if len(client.submitReqs) != 1 || client.submitReqs[0].Sandbox != "docker" {
		t.Fatalf("sandbox not submitted to novomo agent run: %+v", client.submitReqs)
	}

	rows, err := store.ListAgentRunsByJob(context.Background(), "job-agent")
	if err != nil {
		t.Fatalf("ListAgentRunsByJob failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 agent run row, got %d", len(rows))
	}
	row := rows[0]
	if row.ExternalRunID != "novomo-1" || row.Status != "completed" || row.Output != "agent output" {
		t.Fatalf("unexpected persisted row: %+v", row)
	}
	if row.ExecutionID != "exec-agent" {
		t.Fatalf("ExecutionID = %q, want exec-agent", row.ExecutionID)
	}
}

func TestManagerExecuteAgentRunSubmitsExplicitInheritFromAndPersistsJobRunID(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-agent")

	client := &fakeNovomoRunClient{
		submitResp: &novomo.SubmitRunResponse{RunID: "novomo-1", Status: novomo.StatusRunning},
		getScript: []*novomo.Run{{
			RunID:       "novomo-1",
			Status:      novomo.StatusCompleted,
			Output:      "continued",
			RawJobRunID: "jobrun-created",
		}},
	}

	result, err := manager.executeAgentRunWithClient(context.Background(), &workflow.AgentRunRequest{
		ParentJobID:       "job-agent",
		ParentExecutionID: "exec-agent",
		ParentRunID:       "run-agent",
		ParentWorkflowID:  "wf-agent",
		ParentNodeID:      "agent-node",
		Attempt:           1,
		Prompt:            "continue work",
		Harness:           "claude-code",
		Sandbox:           "docker",
		TimeoutSeconds:    30,
		InheritFrom:       &workflow.NovomoHandoffRef{Kind: "job_run", ID: "jobrun-prior", Policy: "latest"},
		IdempotencyKey:    "job-agent:agent-node:1",
	}, client, time.Millisecond)
	if err != nil {
		t.Fatalf("executeAgentRunWithClient failed: %v", err)
	}
	if result.ExternalJobRunID != "jobrun-created" || result.InheritFrom == nil || result.InheritFrom.ID != "jobrun-prior" {
		t.Fatalf("expected job run id and inherited ref in result, got %+v", result)
	}
	if len(client.submitReqs) != 1 || client.submitReqs[0].InheritFrom == nil {
		t.Fatalf("inherit_from was not submitted: %+v", client.submitReqs)
	}
	if client.submitReqs[0].InheritFrom.Kind != "job_run" || client.submitReqs[0].InheritFrom.ID != "jobrun-prior" {
		t.Fatalf("unexpected submitted inherit_from: %+v", client.submitReqs[0].InheritFrom)
	}

	rows, err := store.ListAgentRunsByJob(context.Background(), "job-agent")
	if err != nil {
		t.Fatalf("ListAgentRunsByJob failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 agent run row, got %d", len(rows))
	}
	if rows[0].ExternalJobRunID != "jobrun-created" || rows[0].InheritFromJSON == "" {
		t.Fatalf("handoff fields were not persisted: %+v", rows[0])
	}
}

func TestManagerExecuteAgentRunSubmitsWorkflowTaskHandoffForFanInAuto(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-agent")

	client := &fakeNovomoRunClient{
		getScript: []*novomo.Run{{
			RunID:       "novomo-qa",
			TaskID:      "consortium-run-agent",
			Status:      novomo.StatusCompleted,
			Output:      "reviewed fan-in",
			RawJobRunID: "jobrun-qa",
		}},
	}

	result, err := manager.executeAgentRunWithClient(context.Background(), &workflow.AgentRunRequest{
		ParentJobID:             "job-agent",
		ParentExecutionID:       "exec-agent",
		ParentRunID:             "run-agent",
		ParentWorkflowID:        "wf-agent",
		ParentNodeID:            "qa",
		Attempt:                 1,
		Prompt:                  "review both branches",
		Harness:                 "claude-code",
		Sandbox:                 "docker",
		TimeoutSeconds:          30,
		InheritFromWorkflowTask: true,
		IdempotencyKey:          "job-agent:qa:1",
	}, client, time.Millisecond)
	if err != nil {
		t.Fatalf("executeAgentRunWithClient failed: %v", err)
	}
	if !result.Success || result.InheritFrom == nil || result.InheritFrom.Kind != "task" || result.InheritFrom.ID != "consortium-run-agent" {
		t.Fatalf("workflow task handoff not reflected in result: %+v", result)
	}
	if len(client.submitReqs) != 1 {
		t.Fatalf("expected 1 submit, got %d", len(client.submitReqs))
	}
	submitted := client.submitReqs[0]
	if submitted.TaskID != "consortium-run-agent" {
		t.Fatalf("task_id = %q, want consortium-run-agent", submitted.TaskID)
	}
	if submitted.InheritFrom == nil || submitted.InheritFrom.Kind != "task" || submitted.InheritFrom.ID != "consortium-run-agent" {
		t.Fatalf("unexpected submitted inherit_from: %+v", submitted.InheritFrom)
	}
}

func TestManagerExecuteAgentRunRejectsInvalidSandbox(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-agent")

	client := &fakeNovomoRunClient{}
	_, err := manager.executeAgentRunWithClient(context.Background(), &workflow.AgentRunRequest{
		ParentJobID:       "job-agent",
		ParentExecutionID: "exec-agent",
		ParentRunID:       "run-agent",
		ParentWorkflowID:  "wf-agent",
		ParentNodeID:      "agent-node",
		Attempt:           1,
		Prompt:            "do work",
		Harness:           "claude-code",
		Sandbox:           "vm",
		TimeoutSeconds:    30,
		IdempotencyKey:    "job-agent:agent-node:1",
	}, client, time.Millisecond)
	if err == nil {
		t.Fatal("expected invalid sandbox error")
	}
	if client.submitCalls != 0 {
		t.Fatalf("expected no Novomo submit for invalid sandbox, got %d", client.submitCalls)
	}
}

func TestManagerExecuteAgentRunPersistsDistinctRowsForParallelNodes(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-agent")

	client := &parallelNovomoRunClient{}
	nodeIDs := []string{"agent-a", "agent-b"}
	errs := make(chan error, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		nodeID := nodeID
		go func() {
			result, err := manager.executeAgentRunWithClient(context.Background(), &workflow.AgentRunRequest{
				ParentJobID:       "job-agent",
				ParentExecutionID: "exec-agent",
				ParentRunID:       "run-agent",
				ParentWorkflowID:  "wf-agent",
				ParentNodeID:      nodeID,
				Attempt:           1,
				Prompt:            "do " + nodeID,
				Harness:           "claude-code",
				TimeoutSeconds:    30,
				IdempotencyKey:    "job-agent:" + nodeID + ":1",
			}, client, time.Millisecond)
			if err != nil {
				errs <- err
				return
			}
			if !result.Success || result.ExternalRunID == "" {
				errs <- fmt.Errorf("unexpected result for %s: %+v", nodeID, result)
				return
			}
			errs <- nil
		}()
	}

	for range nodeIDs {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}

	rows, err := store.ListAgentRunsByJob(context.Background(), "job-agent")
	if err != nil {
		t.Fatalf("ListAgentRunsByJob failed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 agent run rows, got %d: %+v", len(rows), rows)
	}
	seenNodes := map[string]bool{}
	seenRuns := map[string]bool{}
	for _, row := range rows {
		seenNodes[row.NodeID] = true
		if row.ExternalRunID == "" {
			t.Fatalf("missing external run id: %+v", row)
		}
		if seenRuns[row.ExternalRunID] {
			t.Fatalf("duplicate external run id %q in rows: %+v", row.ExternalRunID, rows)
		}
		seenRuns[row.ExternalRunID] = true
	}
	for _, nodeID := range nodeIDs {
		if !seenNodes[nodeID] {
			t.Fatalf("missing row for node %s: %+v", nodeID, rows)
		}
	}
}

func TestManagerExecuteAgentRunPersistsDistinctRowsForDifferentRuns(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-agent")

	client := &parallelNovomoRunClient{}
	for _, runID := range []string{"run-a", "run-b"} {
		result, err := manager.executeAgentRunWithClient(context.Background(), &workflow.AgentRunRequest{
			ParentJobID:       "job-agent",
			ParentExecutionID: "exec-agent",
			ParentRunID:       runID,
			ParentWorkflowID:  "wf-agent",
			ParentNodeID:      "agent-node",
			Attempt:           1,
			Prompt:            "do work",
			Harness:           "claude-code",
			TimeoutSeconds:    30,
			IdempotencyKey:    "exec-agent:agent-node:1",
		}, client, time.Millisecond)
		if err != nil {
			t.Fatalf("executeAgentRunWithClient(%s) failed: %v", runID, err)
		}
		if !result.Success {
			t.Fatalf("expected success for %s: %+v", runID, result)
		}
	}

	rows, err := store.ListAgentRunsByJob(context.Background(), "job-agent")
	if err != nil {
		t.Fatalf("ListAgentRunsByJob failed: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows for distinct run ids, got %d: %+v", len(rows), rows)
	}
	seenRunIDs := map[string]bool{}
	for _, row := range rows {
		seenRunIDs[row.RunID] = true
	}
	for _, runID := range []string{"run-a", "run-b"} {
		if !seenRunIDs[runID] {
			t.Fatalf("missing row for run id %s: %+v", runID, rows)
		}
	}
}

func TestManagerExecuteAgentRunResumesExistingExternalRun(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-agent")

	if err := store.UpsertAgentRun(context.Background(), &storage.AgentRun{
		ID:            agentRunRowID("job-agent", "run-agent", "agent-node", 1),
		JobID:         "job-agent",
		ExecutionID:   "exec-agent",
		RunID:         "run-agent",
		NodeID:        "agent-node",
		Attempt:       1,
		ExternalRunID: "novomo-existing",
		Harness:       "claude-code",
		Status:        "running",
	}); err != nil {
		t.Fatalf("seed agent run row: %v", err)
	}

	client := &fakeNovomoRunClient{
		getScript: []*novomo.Run{{
			RunID:        "novomo-existing",
			Status:       novomo.StatusCompleted,
			Output:       "resumed",
			TokensInput:  1,
			TokensOutput: 2,
			CostUSD:      0.03,
		}},
	}

	result, err := manager.executeAgentRunWithClient(context.Background(), &workflow.AgentRunRequest{
		ParentJobID:       "job-agent",
		ParentExecutionID: "exec-agent",
		ParentRunID:       "run-agent",
		ParentWorkflowID:  "wf-agent",
		ParentNodeID:      "agent-node",
		Attempt:           1,
		Prompt:            "do work",
		Harness:           "claude-code",
		TimeoutSeconds:    30,
		IdempotencyKey:    "job-agent:agent-node:1",
	}, client, time.Millisecond)
	if err != nil {
		t.Fatalf("executeAgentRunWithClient failed: %v", err)
	}
	if !result.Success || result.ExternalRunID != "novomo-existing" || result.Output != "resumed" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if client.submitCalls != 0 {
		t.Fatalf("expected no submit on resume, got %d", client.submitCalls)
	}
	if client.getCalls != 1 {
		t.Fatalf("expected one poll, got %d", client.getCalls)
	}
}

func TestPollExternalAgentRunMarksCancelledOnContextCancel(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-agent")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	row := &storage.AgentRun{
		ID:            agentRunRowID("job-agent", "run-agent", "agent-node", 1),
		JobID:         "job-agent",
		ExecutionID:   "exec-agent",
		RunID:         "run-agent",
		NodeID:        "agent-node",
		Attempt:       1,
		RunKind:       "agent_run",
		ExternalRunID: "novomo-active",
		Harness:       "claude-code",
		Status:        "running",
	}

	result, err := manager.pollExternalAgentRun(ctx, externalAgentRunConfig{
		RunKind:        "agent_run",
		TimeoutSeconds: 30,
		Get: func(context.Context, string) (*externalAgentRunSnapshot, error) {
			t.Fatal("Get should not run after context cancellation")
			return nil, nil
		},
	}, row, time.Millisecond)
	if err != nil {
		t.Fatalf("poll returned error: %v", err)
	}
	if result == nil || result.Status != "cancelled" {
		t.Fatalf("expected cancelled result, got %+v", result)
	}
}

func TestPollExternalAgentRunStopsNovomoOnContextCancel(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-agent")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stoppedID := ""
	row := &storage.AgentRun{
		ID:            agentRunRowID("job-agent", "run-agent", "agent-node", 1),
		JobID:         "job-agent",
		ExecutionID:   "exec-agent",
		RunID:         "run-agent",
		NodeID:        "agent-node",
		Attempt:       1,
		RunKind:       "agent_run",
		ExternalRunID: "novomo-active",
		Harness:       "claude-code",
		Status:        "running",
	}

	result, err := manager.pollExternalAgentRun(ctx, externalAgentRunConfig{
		RunKind:        "agent_run",
		TimeoutSeconds: 30,
		Get: func(context.Context, string) (*externalAgentRunSnapshot, error) {
			t.Fatal("Get should not run after context cancellation")
			return nil, nil
		},
		Stop: func(ctx context.Context, externalRunID string) error {
			stoppedID = externalRunID
			return nil
		},
	}, row, time.Millisecond)
	if err != nil {
		t.Fatalf("poll returned error: %v", err)
	}
	if stoppedID != "novomo-active" {
		t.Fatalf("expected Novomo stop for novomo-active, got %q", stoppedID)
	}
	if result == nil || result.Status != "cancelled" {
		t.Fatalf("expected cancelled result, got %+v", result)
	}
}

func TestPollExternalAgentRunStopsNovomoOnPollingDeadline(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-agent")

	stoppedID := ""
	row := &storage.AgentRun{
		ID:            agentRunRowID("job-agent", "run-agent", "agent-node", 1),
		JobID:         "job-agent",
		ExecutionID:   "exec-agent",
		RunID:         "run-agent",
		NodeID:        "agent-node",
		Attempt:       1,
		RunKind:       "agent_run",
		ExternalRunID: "novomo-active",
		Harness:       "claude-code",
		Status:        "running",
	}
	if err := store.UpsertAgentRun(context.Background(), row); err != nil {
		t.Fatalf("upsert running row: %v", err)
	}

	result, err := manager.pollExternalAgentRun(context.Background(), externalAgentRunConfig{
		RunKind:        "agent_run",
		TimeoutSeconds: -1,
		Get: func(context.Context, string) (*externalAgentRunSnapshot, error) {
			t.Fatal("Get should not run after polling deadline")
			return nil, nil
		},
		Stop: func(ctx context.Context, externalRunID string) error {
			stoppedID = externalRunID
			return nil
		},
	}, row, time.Millisecond)
	if err != nil {
		t.Fatalf("poll returned error: %v", err)
	}
	if stoppedID != "novomo-active" {
		t.Fatalf("expected Novomo stop for novomo-active, got %q", stoppedID)
	}
	if result == nil || result.Status != "failed" || result.ErrorCode != workflow.AgentRunErrorNovomoUnresponsive {
		t.Fatalf("expected unresponsive result, got %+v", result)
	}
}

func TestPollExternalAgentRunPreservesManualStopOnPollingDeadline(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-agent")

	row := &storage.AgentRun{
		ID:            agentRunRowID("job-agent", "run-agent", "agent-node", 1),
		JobID:         "job-agent",
		ExecutionID:   "exec-agent",
		RunID:         "run-agent",
		NodeID:        "agent-node",
		Attempt:       1,
		RunKind:       "agent_run",
		ExternalRunID: "novomo-active",
		Harness:       "claude-code",
		Status:        "running",
	}
	if err := store.UpsertAgentRun(context.Background(), row); err != nil {
		t.Fatalf("upsert running row: %v", err)
	}

	result, err := manager.pollExternalAgentRun(context.Background(), externalAgentRunConfig{
		RunKind:        "agent_run",
		TimeoutSeconds: -1,
		Get: func(context.Context, string) (*externalAgentRunSnapshot, error) {
			t.Fatal("Get should not run after polling deadline")
			return nil, nil
		},
		Stop: func(ctx context.Context, externalRunID string) error {
			manualStop := *row
			manualStop.Status = "cancelled"
			manualStop.ErrorCode = "CANCELLED"
			manualStop.ErrorMessage = "manual stop won"
			now := time.Now().UTC()
			manualStop.FinishedAt = &now
			if err := store.UpsertAgentRun(context.Background(), &manualStop); err != nil {
				t.Fatalf("upsert manual stop row: %v", err)
			}
			return nil
		},
	}, row, time.Millisecond)
	if err != nil {
		t.Fatalf("poll returned error: %v", err)
	}
	if result == nil || result.Status != "cancelled" || result.ErrorCode != "CANCELLED" {
		t.Fatalf("expected manual cancelled result, got %+v", result)
	}

	latest, err := store.GetAgentRunByID(context.Background(), "job-agent", row.ID)
	if err != nil {
		t.Fatalf("get latest row: %v", err)
	}
	if latest.Status != "cancelled" || latest.ErrorMessage != "manual stop won" {
		t.Fatalf("poller overwrote manual stop state: %+v", latest)
	}
}

func TestPollExternalAgentRunStopsNovomoOnNilRun(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-agent")

	stoppedID := ""
	row := &storage.AgentRun{
		ID:            agentRunRowID("job-agent", "run-agent", "agent-node", 1),
		JobID:         "job-agent",
		ExecutionID:   "exec-agent",
		RunID:         "run-agent",
		NodeID:        "agent-node",
		Attempt:       1,
		RunKind:       "agent_run",
		ExternalRunID: "novomo-active",
		Harness:       "claude-code",
		Status:        "running",
	}
	if err := store.UpsertAgentRun(context.Background(), row); err != nil {
		t.Fatalf("upsert running row: %v", err)
	}

	result, err := manager.pollExternalAgentRun(context.Background(), externalAgentRunConfig{
		RunKind:        "agent_run",
		TimeoutSeconds: 30,
		Get: func(context.Context, string) (*externalAgentRunSnapshot, error) {
			return nil, nil
		},
		Stop: func(ctx context.Context, externalRunID string) error {
			stoppedID = externalRunID
			return nil
		},
	}, row, time.Millisecond)
	if err != nil {
		t.Fatalf("poll returned error: %v", err)
	}
	if stoppedID != "novomo-active" {
		t.Fatalf("expected Novomo stop for novomo-active, got %q", stoppedID)
	}
	if result == nil || result.Status != "failed" || result.ErrorCode != workflow.AgentRunErrorNovomoUnresponsive {
		t.Fatalf("expected unresponsive result, got %+v", result)
	}
}

func TestPollExternalAgentRunPreservesManualStopState(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-agent")

	row := &storage.AgentRun{
		ID:            agentRunRowID("job-agent", "run-agent", "agent-node", 1),
		JobID:         "job-agent",
		ExecutionID:   "exec-agent",
		RunID:         "run-agent",
		NodeID:        "agent-node",
		Attempt:       1,
		RunKind:       "agent_run",
		ExternalRunID: "novomo-active",
		Harness:       "claude-code",
		Status:        "running",
	}
	if err := store.UpsertAgentRun(context.Background(), row); err != nil {
		t.Fatalf("upsert running row: %v", err)
	}

	result, err := manager.pollExternalAgentRun(context.Background(), externalAgentRunConfig{
		RunKind:        "agent_run",
		TimeoutSeconds: 30,
		Get: func(context.Context, string) (*externalAgentRunSnapshot, error) {
			manualStop := *row
			manualStop.Status = "cancelled"
			manualStop.ErrorCode = "CANCELLED"
			if err := store.UpsertAgentRun(context.Background(), &manualStop); err != nil {
				t.Fatalf("upsert manual stop row: %v", err)
			}
			return &externalAgentRunSnapshot{ExternalRunID: "novomo-active", Status: "running"}, nil
		},
	}, row, time.Millisecond)
	if err != nil {
		t.Fatalf("poll returned error: %v", err)
	}
	if result == nil || result.Status != "cancelled" || result.ErrorCode != "CANCELLED" {
		t.Fatalf("expected manual cancelled result, got %+v", result)
	}

	latest, err := store.GetAgentRunByID(context.Background(), "job-agent", row.ID)
	if err != nil {
		t.Fatalf("get latest row: %v", err)
	}
	if latest.Status != "cancelled" {
		t.Fatalf("poller overwrote manual stop state: %+v", latest)
	}
}

func TestPollExternalAgentRunPreservesManualStopOnNonRetryableGetError(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-agent")

	row := &storage.AgentRun{
		ID:            agentRunRowID("job-agent", "run-agent", "agent-node", 1),
		JobID:         "job-agent",
		ExecutionID:   "exec-agent",
		RunID:         "run-agent",
		NodeID:        "agent-node",
		Attempt:       1,
		RunKind:       "agent_run",
		ExternalRunID: "novomo-active",
		Harness:       "claude-code",
		Status:        "running",
	}
	if err := store.UpsertAgentRun(context.Background(), row); err != nil {
		t.Fatalf("upsert running row: %v", err)
	}

	result, err := manager.pollExternalAgentRun(context.Background(), externalAgentRunConfig{
		RunKind:        "agent_run",
		TimeoutSeconds: 30,
		Get: func(context.Context, string) (*externalAgentRunSnapshot, error) {
			manualStop := *row
			manualStop.Status = "cancelled"
			manualStop.ErrorCode = "CANCELLED"
			manualStop.ErrorMessage = "manual stop won"
			now := time.Now().UTC()
			manualStop.FinishedAt = &now
			if err := store.UpsertAgentRun(context.Background(), &manualStop); err != nil {
				t.Fatalf("upsert manual stop row: %v", err)
			}
			return nil, &novomo.Error{Code: "NOVOMO_BAD_RESPONSE", Message: "bad response"}
		},
	}, row, time.Millisecond)
	if err != nil {
		t.Fatalf("poll returned error: %v", err)
	}
	if result == nil || result.Status != "cancelled" || result.ErrorCode != "CANCELLED" {
		t.Fatalf("expected manual cancelled result, got %+v", result)
	}

	latest, err := store.GetAgentRunByID(context.Background(), "job-agent", row.ID)
	if err != nil {
		t.Fatalf("get latest row: %v", err)
	}
	if latest.Status != "cancelled" || latest.ErrorMessage != "manual stop won" {
		t.Fatalf("poller overwrote manual stop state: %+v", latest)
	}
}

func TestStopExternalAgentRunSuppressesNotStoppable(t *testing.T) {
	manager, _ := setupManagerTest(t)
	var logOutput bytes.Buffer
	previousOutput := log.Writer()
	log.SetOutput(&logOutput)
	defer log.SetOutput(previousOutput)

	called := false
	manager.stopExternalAgentRun(&storage.AgentRun{
		RunKind:       "agent_run",
		ExternalRunID: "novomo-terminal",
	}, externalAgentRunConfig{
		RunKind: "agent_run",
		Stop: func(context.Context, string) error {
			called = true
			return &novomo.Error{Code: "not_stoppable", Message: "already terminal"}
		},
	})
	if !called {
		t.Fatal("expected stop callback to be called")
	}
	if logOutput.Len() != 0 {
		t.Fatalf("expected not_stoppable stop error to be suppressed, got log %q", logOutput.String())
	}
}

func TestManagerStopExternalAgentRunStopsAgentRun(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-agent")

	row := &storage.AgentRun{
		ID:            agentRunRowID("job-agent", "run-agent", "agent-node", 1),
		JobID:         "job-agent",
		ExecutionID:   "exec-agent",
		RunID:         "run-agent",
		NodeID:        "agent-node",
		Attempt:       1,
		RunKind:       "agent_run",
		ExternalRunID: "novomo-active",
		Harness:       "claude-code",
		Status:        "running",
	}
	if err := store.UpsertAgentRun(context.Background(), row); err != nil {
		t.Fatalf("upsert agent run: %v", err)
	}

	stopClient := &fakeNovomoStopClient{}
	manager.novomoStopClientFactory = func() (novomoStopClient, error) {
		return stopClient, nil
	}

	updated, err := manager.StopExternalAgentRun(context.Background(), "job-agent", row.ID)
	if err != nil {
		t.Fatalf("StopExternalAgentRun failed: %v", err)
	}
	if stopClient.stopRunCalls != 1 || len(stopClient.stoppedRunIDs) != 1 || stopClient.stoppedRunIDs[0] != "novomo-active" {
		t.Fatalf("unexpected stop calls: %+v", stopClient)
	}
	if updated.Status != "cancelled" || updated.ErrorCode != "CANCELLED" {
		t.Fatalf("expected local cancelled status, got %+v", updated)
	}
}

func TestManagerStopExternalAgentRunReturnsConflictWhenRemoteNotStoppable(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-agent")

	row := &storage.AgentRun{
		ID:            agentRunRowID("job-agent", "run-agent", "agent-node", 1),
		JobID:         "job-agent",
		ExecutionID:   "exec-agent",
		RunID:         "run-agent",
		NodeID:        "agent-node",
		Attempt:       1,
		RunKind:       "agent_run",
		ExternalRunID: "novomo-terminal",
		Harness:       "claude-code",
		Status:        "running",
	}
	if err := store.UpsertAgentRun(context.Background(), row); err != nil {
		t.Fatalf("upsert agent run: %v", err)
	}

	stopClient := &fakeNovomoStopClient{stopRunErr: &novomo.Error{Code: "NOT_STOPPABLE", Message: "already terminal"}}
	manager.novomoStopClientFactory = func() (novomoStopClient, error) {
		return stopClient, nil
	}

	updated, err := manager.StopExternalAgentRun(context.Background(), "job-agent", row.ID)
	if err == nil {
		t.Fatal("expected remote not_stoppable conflict")
	}
	if updated != nil {
		t.Fatalf("expected no local update on remote not_stoppable, got %+v", updated)
	}
	if code := workflow.GetErrorCode(err); code != "NOT_STOPPABLE" {
		t.Fatalf("expected NOT_STOPPABLE error code, got %q from %v", code, err)
	}
	latest, err := store.GetAgentRunByID(context.Background(), "job-agent", row.ID)
	if err != nil {
		t.Fatalf("get latest row: %v", err)
	}
	if latest.Status != "running" {
		t.Fatalf("expected local row to remain running until poll reconciliation, got %+v", latest)
	}
}

func TestManagerStopExternalAgentRunPreservesConcurrentTerminalPollResult(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-agent")

	row := &storage.AgentRun{
		ID:            agentRunRowID("job-agent", "run-agent", "agent-node", 1),
		JobID:         "job-agent",
		ExecutionID:   "exec-agent",
		RunID:         "run-agent",
		NodeID:        "agent-node",
		Attempt:       1,
		RunKind:       "agent_run",
		ExternalRunID: "novomo-active",
		Harness:       "claude-code",
		Status:        "running",
	}
	if err := store.UpsertAgentRun(context.Background(), row); err != nil {
		t.Fatalf("upsert agent run: %v", err)
	}

	finished := time.Now().UTC()
	stopClient := &fakeNovomoStopClient{
		onStopRun: func() {
			completed := *row
			completed.Status = "completed"
			completed.Output = "poll result"
			completed.TokensInput = 11
			completed.TokensOutput = 7
			completed.CostUSD = 0.25
			completed.FinishedAt = &finished
			if err := store.UpsertAgentRun(context.Background(), &completed); err != nil {
				t.Errorf("concurrent terminal upsert: %v", err)
			}
		},
	}
	manager.novomoStopClientFactory = func() (novomoStopClient, error) {
		return stopClient, nil
	}

	updated, err := manager.StopExternalAgentRun(context.Background(), "job-agent", row.ID)
	if !errors.Is(err, ErrJobStateConflict) {
		t.Fatalf("expected state conflict after concurrent completion, got %v", err)
	}
	if updated == nil {
		t.Fatal("expected latest terminal row to be returned")
	}
	if updated.Status != "completed" || updated.Output != "poll result" || updated.TokensInput != 11 || updated.TokensOutput != 7 {
		t.Fatalf("expected concurrent completed result to win, got %+v", updated)
	}

	latest, err := store.GetAgentRunByID(context.Background(), "job-agent", row.ID)
	if err != nil {
		t.Fatalf("get latest row: %v", err)
	}
	if latest.Status != "completed" || latest.Output != "poll result" || latest.CostUSD != 0.25 {
		t.Fatalf("operator stop overwrote concurrent terminal row: %+v", latest)
	}
}

func TestManagerStopExternalAgentRunStopsNovoRun(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-superagent")

	row := &storage.AgentRun{
		ID:             agentRunRowID("job-superagent", "run-agent", "superagent-node", 1),
		JobID:          "job-superagent",
		ExecutionID:    "exec-agent",
		RunID:          "run-agent",
		NodeID:         "superagent-node",
		Attempt:        1,
		RunKind:        "novo_run",
		ExternalRunID:  "nr-active",
		ExternalTaskID: "task-active",
		Status:         "running",
	}
	if err := store.UpsertAgentRun(context.Background(), row); err != nil {
		t.Fatalf("upsert agent run: %v", err)
	}

	stopClient := &fakeNovomoStopClient{}
	manager.novomoStopClientFactory = func() (novomoStopClient, error) {
		return stopClient, nil
	}

	_, err := manager.StopExternalAgentRun(context.Background(), "job-superagent", row.ID)
	if err != nil {
		t.Fatalf("StopExternalAgentRun failed: %v", err)
	}
	if stopClient.stopNovoCalls != 1 || len(stopClient.stoppedNovoIDs) != 1 || stopClient.stoppedNovoIDs[0] != "nr-active" {
		t.Fatalf("unexpected stop calls: %+v", stopClient)
	}
}

func TestManagerStopExternalAgentRunRejectsTerminalLocalRun(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-agent")

	row := &storage.AgentRun{
		ID:            agentRunRowID("job-agent", "run-agent", "agent-node", 1),
		JobID:         "job-agent",
		ExecutionID:   "exec-agent",
		RunID:         "run-agent",
		NodeID:        "agent-node",
		Attempt:       1,
		RunKind:       "agent_run",
		ExternalRunID: "novomo-terminal",
		Harness:       "claude-code",
		Status:        "completed",
	}
	if err := store.UpsertAgentRun(context.Background(), row); err != nil {
		t.Fatalf("upsert agent run: %v", err)
	}

	_, err := manager.StopExternalAgentRun(context.Background(), "job-agent", row.ID)
	if !errors.Is(err, ErrJobStateConflict) {
		t.Fatalf("expected state conflict, got %v", err)
	}
}

func TestManagerExecuteNovoRunSubmitsPollsAndPersists(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-superagent")

	client := &fakeNovoRunClient{
		baseURL:    "http://127.0.0.1:8080",
		submitResp: &novomo.SubmitNovoRunResponse{NovoRunID: "nr-1", TaskID: "task-1", Status: novomo.StatusRunning},
		getScript: []*novomo.NovoRun{
			{RunID: "nr-1", TaskID: "task-1", Status: novomo.StatusRunning},
			{
				RunID:        "nr-1",
				TaskID:       "task-1",
				Status:       novomo.StatusCompleted,
				Output:       "superagent output",
				TokensInput:  13,
				TokensOutput: 8,
				CostUSD:      0.29,
			},
		},
	}

	result, err := manager.executeNovoRunWithClient(context.Background(), &workflow.NovoRunRequest{
		ParentJobID:       "job-superagent",
		ParentExecutionID: "exec-superagent",
		ParentRunID:       "run-superagent",
		ParentWorkflowID:  "wf-superagent",
		ParentNodeID:      "superagent-node",
		Attempt:           1,
		Goal:              "wake up",
		TaskSummary:       "brief",
		Identity:          "sde-novo",
		Sandbox:           "host",
		TimeoutSeconds:    30,
		IdempotencyKey:    "job-superagent:superagent-node:1",
	}, client, time.Millisecond)
	if err != nil {
		t.Fatalf("executeNovoRunWithClient failed: %v", err)
	}
	if !result.Success || result.Output != "superagent output" || result.ExternalRunKind != "novo_run" || result.ExternalTaskID != "task-1" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if client.submitCalls != 1 || client.getCalls != 2 {
		t.Fatalf("unexpected client calls submit=%d get=%d", client.submitCalls, client.getCalls)
	}
	if len(client.submitReqs) != 1 || client.submitReqs[0].RuntimeURL != "http://127.0.0.1:8080" {
		t.Fatalf("expected default runtime URL from client, got %+v", client.submitReqs)
	}

	rows, err := store.ListAgentRunsByJob(context.Background(), "job-superagent")
	if err != nil {
		t.Fatalf("ListAgentRunsByJob failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 agent run row, got %d", len(rows))
	}
	row := rows[0]
	if row.RunKind != "novo_run" || row.ExternalRunID != "nr-1" || row.ExternalTaskID != "task-1" || row.Status != "completed" {
		t.Fatalf("unexpected persisted row: %+v", row)
	}
}

func TestManagerExecuteNovoRunInheritsFromUpstreamSuperagent(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-superagent")

	if err := store.UpsertAgentRun(context.Background(), &storage.AgentRun{
		ID:             agentRunRowID("job-superagent", "run-superagent", "upstream-superagent", 1),
		JobID:          "job-superagent",
		ExecutionID:    "exec-superagent",
		RunID:          "run-superagent",
		NodeID:         "upstream-superagent",
		Attempt:        1,
		RunKind:        "novo_run",
		ExternalRunID:  "nr-upstream",
		ExternalTaskID: "task-upstream",
		Status:         "completed",
	}); err != nil {
		t.Fatalf("seed upstream superagent row: %v", err)
	}

	client := &fakeNovoRunClient{
		baseURL:    "http://127.0.0.1:8090",
		submitResp: &novomo.SubmitNovoRunResponse{NovoRunID: "nr-downstream", TaskID: "task-downstream", Status: novomo.StatusRunning},
		getScript: []*novomo.NovoRun{{
			RunID:  "nr-downstream",
			TaskID: "task-downstream",
			Status: novomo.StatusCompleted,
			Output: "downstream",
		}},
	}

	result, err := manager.executeNovoRunWithClient(context.Background(), &workflow.NovoRunRequest{
		ParentJobID:       "job-superagent",
		ParentExecutionID: "exec-superagent",
		ParentRunID:       "run-superagent",
		ParentWorkflowID:  "wf-superagent",
		ParentNodeID:      "downstream-superagent",
		Attempt:           1,
		Goal:              "continue",
		Sandbox:           "docker",
		TimeoutSeconds:    30,
		InheritFromNodeID: "upstream-superagent",
		InheritFromPolicy: "latest",
		IdempotencyKey:    "job-superagent:downstream-superagent:1",
	}, client, time.Millisecond)
	if err != nil {
		t.Fatalf("executeNovoRunWithClient failed: %v", err)
	}
	if !result.Success || result.InheritFrom == nil || result.InheritFrom.Kind != "novo_run" || result.InheritFrom.ID != "nr-upstream" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(client.submitReqs) != 1 || client.submitReqs[0].InheritFrom == nil {
		t.Fatalf("inherit_from was not submitted: %+v", client.submitReqs)
	}
	if client.submitReqs[0].InheritFrom.Kind != "novo_run" || client.submitReqs[0].InheritFrom.ID != "nr-upstream" || client.submitReqs[0].InheritFrom.Policy != "latest" {
		t.Fatalf("unexpected submitted inherit_from: %+v", client.submitReqs[0].InheritFrom)
	}
}

func TestManagerExecuteNovoRunRejectsUnfinishedUpstreamHandoff(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-superagent")

	if err := store.UpsertAgentRun(context.Background(), &storage.AgentRun{
		ID:            agentRunRowID("job-superagent", "run-superagent", "upstream-agent", 1),
		JobID:         "job-superagent",
		ExecutionID:   "exec-superagent",
		RunID:         "run-superagent",
		NodeID:        "upstream-agent",
		Attempt:       1,
		RunKind:       "agent_run",
		ExternalRunID: "job-upstream",
		Harness:       "claude-code",
		Status:        "running",
	}); err != nil {
		t.Fatalf("seed upstream row: %v", err)
	}

	client := &fakeNovoRunClient{baseURL: "http://127.0.0.1:8090"}
	_, err := manager.executeNovoRunWithClient(context.Background(), &workflow.NovoRunRequest{
		ParentJobID:       "job-superagent",
		ParentExecutionID: "exec-superagent",
		ParentRunID:       "run-superagent",
		ParentNodeID:      "downstream-superagent",
		Attempt:           1,
		Goal:              "continue",
		Sandbox:           "docker",
		TimeoutSeconds:    30,
		InheritFromNodeID: "upstream-agent",
		IdempotencyKey:    "job-superagent:downstream-superagent:1",
	}, client, time.Millisecond)
	if err == nil {
		t.Fatal("expected unfinished upstream handoff error")
	}
	if client.submitCalls != 0 {
		t.Fatalf("expected no submit for unfinished upstream, got %d", client.submitCalls)
	}
}

func TestManagerExecuteNovoRunRewritesDefaultLocalRuntimeURLForDocker(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-superagent-docker")

	client := &fakeNovoRunClient{
		baseURL:    "http://localhost:8090",
		submitResp: &novomo.SubmitNovoRunResponse{NovoRunID: "nr-docker", TaskID: "task-docker", Status: novomo.StatusRunning},
		getScript: []*novomo.NovoRun{{
			RunID:        "nr-docker",
			TaskID:       "task-docker",
			Status:       novomo.StatusCompleted,
			Output:       "done",
			TokensInput:  1,
			TokensOutput: 1,
		}},
	}

	_, err := manager.executeNovoRunWithClient(context.Background(), &workflow.NovoRunRequest{
		ParentJobID:       "job-superagent-docker",
		ParentExecutionID: "exec-superagent-docker",
		ParentRunID:       "run-superagent-docker",
		ParentWorkflowID:  "wf-superagent",
		ParentNodeID:      "superagent-node",
		Attempt:           1,
		Goal:              "wake up",
		Sandbox:           "docker",
		TimeoutSeconds:    30,
		IdempotencyKey:    "job-superagent-docker:superagent-node:1",
	}, client, time.Millisecond)
	if err != nil {
		t.Fatalf("executeNovoRunWithClient failed: %v", err)
	}
	if len(client.submitReqs) != 1 || client.submitReqs[0].RuntimeURL != "http://host.docker.internal:8090" {
		t.Fatalf("expected docker-reachable default runtime URL, got %+v", client.submitReqs)
	}
}

func TestManagerExecuteNovoRunKeepsExplicitRuntimeURL(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-superagent-explicit")

	client := &fakeNovoRunClient{
		baseURL:    "http://127.0.0.1:8080",
		submitResp: &novomo.SubmitNovoRunResponse{NovoRunID: "nr-explicit", TaskID: "task-explicit", Status: novomo.StatusRunning},
		getScript: []*novomo.NovoRun{{
			RunID:        "nr-explicit",
			TaskID:       "task-explicit",
			Status:       novomo.StatusCompleted,
			Output:       "done",
			TokensInput:  1,
			TokensOutput: 1,
		}},
	}

	_, err := manager.executeNovoRunWithClient(context.Background(), &workflow.NovoRunRequest{
		ParentJobID:       "job-superagent-explicit",
		ParentExecutionID: "exec-superagent-explicit",
		ParentRunID:       "run-superagent-explicit",
		ParentWorkflowID:  "wf-superagent",
		ParentNodeID:      "superagent-node",
		Attempt:           1,
		Goal:              "wake up",
		Sandbox:           "docker",
		RuntimeURL:        "http://host.docker.internal:8080",
		TimeoutSeconds:    30,
		IdempotencyKey:    "job-superagent-explicit:superagent-node:1",
	}, client, time.Millisecond)
	if err != nil {
		t.Fatalf("executeNovoRunWithClient failed: %v", err)
	}
	if len(client.submitReqs) != 1 || client.submitReqs[0].RuntimeURL != "http://host.docker.internal:8080" {
		t.Fatalf("expected explicit runtime URL to win, got %+v", client.submitReqs)
	}
}

func TestManagerExecuteNovoRunKeepsExplicitLocalRuntimeURLForDocker(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-superagent-explicit-local")

	client := &fakeNovoRunClient{
		baseURL:    "http://127.0.0.1:8090",
		submitResp: &novomo.SubmitNovoRunResponse{NovoRunID: "nr-explicit-local", TaskID: "task-explicit-local", Status: novomo.StatusRunning},
		getScript: []*novomo.NovoRun{{
			RunID:        "nr-explicit-local",
			TaskID:       "task-explicit-local",
			Status:       novomo.StatusCompleted,
			Output:       "done",
			TokensInput:  1,
			TokensOutput: 1,
		}},
	}

	_, err := manager.executeNovoRunWithClient(context.Background(), &workflow.NovoRunRequest{
		ParentJobID:       "job-superagent-explicit-local",
		ParentExecutionID: "exec-superagent-explicit-local",
		ParentRunID:       "run-superagent-explicit-local",
		ParentWorkflowID:  "wf-superagent",
		ParentNodeID:      "superagent-node",
		Attempt:           1,
		Goal:              "wake up",
		Sandbox:           "docker",
		RuntimeURL:        "http://localhost:8090",
		TimeoutSeconds:    30,
		IdempotencyKey:    "job-superagent-explicit-local:superagent-node:1",
	}, client, time.Millisecond)
	if err != nil {
		t.Fatalf("executeNovoRunWithClient failed: %v", err)
	}
	if len(client.submitReqs) != 1 || client.submitReqs[0].RuntimeURL != "http://localhost:8090" {
		t.Fatalf("expected explicit local runtime URL to win, got %+v", client.submitReqs)
	}
}

func TestManagerExecuteNovoRunRejectsInvalidTiming(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-superagent-invalid")
	client := &fakeNovoRunClient{baseURL: "http://127.0.0.1:8080"}

	tests := []struct {
		name string
		req  *workflow.NovoRunRequest
	}{
		{
			name: "missing timeout",
			req: &workflow.NovoRunRequest{
				ParentJobID:       "job-superagent-invalid",
				ParentExecutionID: "exec-superagent-invalid",
				ParentRunID:       "run-superagent-invalid",
				ParentNodeID:      "superagent-node-timeout",
				Attempt:           1,
				Goal:              "wake up",
				IdempotencyKey:    "invalid-timeout",
			},
		},
		{
			name: "negative grace",
			req: &workflow.NovoRunRequest{
				ParentJobID:       "job-superagent-invalid",
				ParentExecutionID: "exec-superagent-invalid",
				ParentRunID:       "run-superagent-invalid",
				ParentNodeID:      "superagent-node-grace",
				Attempt:           1,
				Goal:              "wake up",
				TimeoutSeconds:    30,
				GraceSeconds:      -1,
				IdempotencyKey:    "invalid-grace",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := manager.executeNovoRunWithClient(context.Background(), tt.req, client, time.Millisecond)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if client.submitCalls != 0 {
				t.Fatalf("expected no submit for invalid request, got %d", client.submitCalls)
			}
		})
	}
}

func TestManagerExecuteNovoRunResumesExistingNovoRun(t *testing.T) {
	manager, store := setupManagerTest(t)
	createAgentRunManagerJob(t, store, "job-superagent")

	if err := store.UpsertAgentRun(context.Background(), &storage.AgentRun{
		ID:             agentRunRowID("job-superagent", "run-superagent", "superagent-node", 1),
		JobID:          "job-superagent",
		ExecutionID:    "exec-superagent",
		RunID:          "run-superagent",
		NodeID:         "superagent-node",
		Attempt:        1,
		RunKind:        "novo_run",
		ExternalRunID:  "nr-existing",
		ExternalTaskID: "task-existing",
		Harness:        "claude-code",
		Status:         "running",
	}); err != nil {
		t.Fatalf("seed agent run row: %v", err)
	}

	client := &fakeNovoRunClient{
		getScript: []*novomo.NovoRun{{
			RunID:        "nr-existing",
			TaskID:       "task-existing",
			Status:       novomo.StatusCompleted,
			Output:       "resumed superagent",
			TokensInput:  1,
			TokensOutput: 2,
			CostUSD:      0.03,
		}},
	}

	result, err := manager.executeNovoRunWithClient(context.Background(), &workflow.NovoRunRequest{
		ParentJobID:       "job-superagent",
		ParentExecutionID: "exec-superagent",
		ParentRunID:       "run-superagent",
		ParentWorkflowID:  "wf-superagent",
		ParentNodeID:      "superagent-node",
		Attempt:           1,
		Goal:              "wake up",
		TimeoutSeconds:    30,
		IdempotencyKey:    "job-superagent:superagent-node:1",
	}, client, time.Millisecond)
	if err != nil {
		t.Fatalf("executeNovoRunWithClient failed: %v", err)
	}
	if !result.Success || result.ExternalRunID != "nr-existing" || result.Output != "resumed superagent" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if client.submitCalls != 0 {
		t.Fatalf("expected no submit on resume, got %d", client.submitCalls)
	}
}

func createAgentRunManagerJob(t *testing.T, store *storage.Storage, jobID string) {
	t.Helper()
	if err := store.CreateExecution(&storage.Job{
		ID:                  jobID,
		Description:         "agent run manager test",
		Model:               "workflow",
		Status:              "running",
		WorkflowID:          "wf-agent",
		WorkflowExecutionID: jobID,
		RunID:               "run-agent",
		RunNumber:           1,
		DAGSnapshot:         `{"id":"wf-agent","nodes":[{"id":"agent-node"}],"edges":[]}`,
		DAGHash:             "agent-run-hash",
	}); err != nil {
		t.Fatalf("create execution: %v", err)
	}
}
