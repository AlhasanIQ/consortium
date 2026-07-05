package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

type countingLLMHandler struct {
	mu    sync.Mutex
	calls int
}

func (h *countingLLMHandler) Type() runtime.ActivityType {
	return runtime.ActivityTypeLLMCall
}

func (h *countingLLMHandler) Execute(ctx context.Context, input *runtime.ActivityInput) (*runtime.ActivityOutput, error) {
	h.mu.Lock()
	h.calls++
	h.mu.Unlock()
	return &runtime.ActivityOutput{
		NodeID:  input.NodeID,
		Success: true,
		Output:  "ok",
	}, nil
}

func (h *countingLLMHandler) CallCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls
}

type scriptedLLMHandler struct {
	mu      sync.Mutex
	calls   int
	outputs map[string]runtime.ActivityOutput
}

func (h *scriptedLLMHandler) Type() runtime.ActivityType {
	return runtime.ActivityTypeLLMCall
}

func (h *scriptedLLMHandler) Execute(ctx context.Context, input *runtime.ActivityInput) (*runtime.ActivityOutput, error) {
	h.mu.Lock()
	h.calls++
	out, ok := h.outputs[input.NodeID]
	h.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("missing scripted output for node %s", input.NodeID)
	}

	clone := out
	clone.NodeID = input.NodeID
	if clone.Success && clone.Output == "" {
		clone.Output = "ok"
	}
	return &clone, nil
}

func (h *scriptedLLMHandler) CallCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.calls
}

func TestDAGRuntime_ResumesScheduledActivityFromHistory(t *testing.T) {
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
		ID:   "wf-resume",
		Name: "Resume Test",
		Nodes: []*workflow.Node{
			{ID: "n1", Type: workflow.NodeTypePrompt, Model: "mock-model", Prompt: "hello"},
		},
	}

	snapshot, err := runtime.FreezeWorkflow(
		wf.ID,
		wf.Name,
		wf.Description,
		[]runtime.NodeForFreeze{
			{
				ID:     "n1",
				Type:   string(workflow.NodeTypePrompt),
				Model:  "mock-model",
				Prompt: "hello",
			},
		},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("failed to freeze workflow: %v", err)
	}

	job := &storage.Job{
		ID:                  "job-resume",
		Description:         "Resume test job",
		Model:               "workflow",
		Status:              "running",
		WorkflowID:          wf.ID,
		WorkflowExecutionID: "job-resume",
		RunID:               "job-resume",
		RunNumber:           1,
		DAGSnapshot:         string(snapshot.Definition),
		DAGHash:             snapshot.DAGHash,
	}
	if err := store.CreateExecution(job); err != nil {
		t.Fatalf("failed to create job: %v", err)
	}

	ctx := context.Background()
	if err := store.AppendHistoryEvent(ctx, &storage.HistoryEvent{
		RunID:     "job-resume",
		Type:      string(runtime.HistoryWorkflowStarted),
		Timestamp: time.Now(),
	}); err != nil {
		t.Fatalf("failed to append workflow_started: %v", err)
	}
	if err := store.AppendHistoryEvent(ctx, &storage.HistoryEvent{
		RunID:      "job-resume",
		Type:       string(runtime.HistoryScheduleActivity),
		NodeID:     "n1",
		ActivityID: "job-resume:n1:1",
		Timestamp:  time.Now(),
		Attributes: map[string]interface{}{"attempt": float64(1)},
	}); err != nil {
		t.Fatalf("failed to append schedule_activity: %v", err)
	}

	err = dagRuntime.Start(context.Background(), &runtime.StartParams{
		Identity: &runtime.ExecutionIdentity{
			WorkflowExecutionID: "job-resume",
			RunID:               "job-resume",
			RunNumber:           1,
			DAGHash:             snapshot.DAGHash,
		},
		Snapshot: snapshot,
		Workflow: wf,
	})
	if err != nil {
		t.Fatalf("runtime start failed: %v", err)
	}

	if handler.CallCount() != 1 {
		t.Fatalf("expected resumed activity to execute exactly once, got %d", handler.CallCount())
	}

	events, err := store.GetHistoryEvents(context.Background(), "job-resume")
	if err != nil {
		t.Fatalf("failed to load history: %v", err)
	}
	hasActivityCompleted := false
	for _, e := range events {
		if e.Type == string(runtime.HistoryActivityCompleted) && e.NodeID == "n1" {
			hasActivityCompleted = true
			break
		}
	}
	if !hasActivityCompleted {
		t.Fatalf("expected resumed run history to contain activity_completed for n1")
	}
}

func TestDAGRuntime_ResumeMarksRequeuedRunningAttemptInterrupted(t *testing.T) {
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
		ID:   "wf-resume-interrupted",
		Name: "Resume Interrupted",
		Nodes: []*workflow.Node{
			{ID: "n1", Type: workflow.NodeTypePrompt, Model: "mock-model", Prompt: "hello"},
		},
	}
	snapshot, err := runtime.FreezeWorkflow(
		wf.ID,
		wf.Name,
		wf.Description,
		[]runtime.NodeForFreeze{
			{ID: "n1", Type: string(workflow.NodeTypePrompt), Model: "mock-model", Prompt: "hello"},
		},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("failed to freeze workflow: %v", err)
	}

	jobID := "job-resume-interrupted"
	job := &storage.Job{
		ID:                  jobID,
		Description:         "Resume interrupted test",
		Model:               "workflow",
		Status:              "running",
		WorkflowID:          wf.ID,
		WorkflowExecutionID: jobID,
		RunID:               jobID,
		RunNumber:           1,
		DAGSnapshot:         string(snapshot.Definition),
		DAGHash:             snapshot.DAGHash,
	}
	if err := store.CreateExecution(job); err != nil {
		t.Fatalf("failed to create job: %v", err)
	}

	ctx := context.Background()
	now := time.Now()
	history := []*storage.HistoryEvent{
		{RunID: jobID, Type: string(runtime.HistoryWorkflowStarted), Timestamp: now},
		{
			RunID:      jobID,
			Type:       string(runtime.HistoryScheduleActivity),
			NodeID:     "n1",
			ActivityID: jobID + ":n1:1",
			Timestamp:  now.Add(time.Millisecond),
			Attributes: map[string]interface{}{"attempt": float64(1)},
		},
		{
			RunID:      jobID,
			Type:       string(runtime.HistoryActivityStarted),
			NodeID:     "n1",
			ActivityID: jobID + ":n1:1",
			Timestamp:  now.Add(2 * time.Millisecond),
		},
	}
	for _, evt := range history {
		if err := store.AppendHistoryEvent(ctx, evt); err != nil {
			t.Fatalf("failed to append history event %s: %v", evt.Type, err)
		}
	}
	startedAt := now.Add(2 * time.Millisecond)
	if err := store.UpsertNodeExecutionAttempt(ctx, &storage.NodeExecutionAttempt{
		JobID:       jobID,
		ExecutionID: jobID,
		RunID:       jobID,
		NodeID:      "n1",
		NodeType:    string(workflow.NodeTypePrompt),
		Attempt:     1,
		Status:      "running",
		ActivityID:  jobID + ":n1:1",
		StartedAt:   &startedAt,
	}); err != nil {
		t.Fatalf("failed to seed stale running attempt: %v", err)
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

	if handler.CallCount() != 1 {
		t.Fatalf("expected resumed activity to execute once, got %d", handler.CallCount())
	}

	attempts, err := store.ListNodeExecutionAttemptsByJob(ctx, jobID)
	if err != nil {
		t.Fatalf("failed to list node execution attempts: %v", err)
	}
	if len(attempts) != 2 {
		t.Fatalf("expected 2 attempts (interrupted + completed), got %d", len(attempts))
	}

	byAttempt := map[int]storage.NodeExecutionAttempt{}
	for _, attempt := range attempts {
		byAttempt[attempt.Attempt] = attempt
	}

	first := byAttempt[1]
	if first.Status != "interrupted" {
		t.Fatalf("expected attempt 1 status interrupted, got %q", first.Status)
	}
	if first.CompletedAt == nil {
		t.Fatal("expected attempt 1 completed_at to be set when interrupted")
	}

	second := byAttempt[2]
	if second.Status != "completed" {
		t.Fatalf("expected attempt 2 status completed, got %q", second.Status)
	}
	if second.CompletedAt == nil {
		t.Fatal("expected attempt 2 completed_at to be set")
	}
}

func TestDAGRuntime_NoProgressFailsRun(t *testing.T) {
	store, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	registry := runtime.NewActivityHandlerRegistry()
	registry.Register(&countingLLMHandler{})
	dagRuntime := NewDAGRuntime(store, registry)

	wf := &workflow.Workflow{
		ID:   "wf-deadlock",
		Name: "Deadlock Test",
		Nodes: []*workflow.Node{
			{ID: "a", Type: workflow.NodeTypePrompt, Model: "mock-model", Prompt: "A"},
			{ID: "b", Type: workflow.NodeTypePrompt, Model: "mock-model", Prompt: "B"},
		},
		Edges: []*workflow.Edge{
			{Source: "a", Target: "b"},
			{Source: "b", Target: "a"},
		},
	}

	snapshot, err := runtime.FreezeWorkflow(
		wf.ID,
		wf.Name,
		wf.Description,
		[]runtime.NodeForFreeze{
			{ID: "a", Type: string(workflow.NodeTypePrompt), Model: "mock-model", Prompt: "A"},
			{ID: "b", Type: string(workflow.NodeTypePrompt), Model: "mock-model", Prompt: "B"},
		},
		[]runtime.EdgeForFreeze{
			{Source: "a", Target: "b"},
			{Source: "b", Target: "a"},
		},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("failed to freeze workflow: %v", err)
	}

	job := &storage.Job{
		ID:                  "job-deadlock",
		Description:         "Deadlock test job",
		Model:               "workflow",
		Status:              "pending",
		WorkflowID:          wf.ID,
		WorkflowExecutionID: "job-deadlock",
		RunID:               "job-deadlock",
		RunNumber:           1,
		DAGSnapshot:         string(snapshot.Definition),
		DAGHash:             snapshot.DAGHash,
	}
	if err := store.CreateExecution(job); err != nil {
		t.Fatalf("failed to create job: %v", err)
	}

	err = dagRuntime.Start(context.Background(), &runtime.StartParams{
		Identity: &runtime.ExecutionIdentity{
			WorkflowExecutionID: "job-deadlock",
			RunID:               "job-deadlock",
			RunNumber:           1,
			DAGHash:             snapshot.DAGHash,
		},
		Snapshot: snapshot,
		Workflow: wf,
	})
	if err == nil {
		t.Fatalf("expected runtime to fail on no-progress deadlock")
	}

	updated, err := store.GetExecution("job-deadlock")
	if err != nil {
		t.Fatalf("failed to load updated job: %v", err)
	}
	if updated.Status != "failed" {
		t.Fatalf("expected failed status for deadlock run, got %s", updated.Status)
	}

	events, err := store.GetHistoryEvents(context.Background(), "job-deadlock")
	if err != nil {
		t.Fatalf("failed to load deadlock history: %v", err)
	}
	for _, e := range events {
		if e.Type == string(runtime.HistoryWorkflowCompleted) {
			t.Fatalf("did not expect workflow_completed event for deadlock run")
		}
	}
}

func TestDAGRuntime_ResumeCompletedRunDoesNotDuplicateCompletion(t *testing.T) {
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
		ID:   "wf-resume-completed",
		Name: "Resume Completed",
		Nodes: []*workflow.Node{
			{ID: "n1", Type: workflow.NodeTypePrompt, Model: "mock-model", Prompt: "hello"},
		},
	}

	snapshot, err := runtime.FreezeWorkflow(
		wf.ID,
		wf.Name,
		wf.Description,
		[]runtime.NodeForFreeze{
			{ID: "n1", Type: string(workflow.NodeTypePrompt), Model: "mock-model", Prompt: "hello"},
		},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("failed to freeze workflow: %v", err)
	}

	jobID := "job-resume-completed"
	job := &storage.Job{
		ID:                  jobID,
		Description:         "Resume completed test",
		Model:               "workflow",
		Status:              "running",
		WorkflowID:          wf.ID,
		WorkflowExecutionID: jobID,
		RunID:               jobID,
		RunNumber:           1,
		DAGSnapshot:         string(snapshot.Definition),
		DAGHash:             snapshot.DAGHash,
	}
	if err := store.CreateExecution(job); err != nil {
		t.Fatalf("failed to create job: %v", err)
	}

	ctx := context.Background()
	now := time.Now()
	history := []*storage.HistoryEvent{
		{RunID: jobID, Type: string(runtime.HistoryWorkflowStarted), Timestamp: now},
		{
			RunID:      jobID,
			Type:       string(runtime.HistoryScheduleActivity),
			NodeID:     "n1",
			ActivityID: jobID + ":n1:1",
			Timestamp:  now.Add(time.Millisecond),
			Attributes: map[string]interface{}{"attempt": float64(1)},
		},
		{
			RunID:      jobID,
			Type:       string(runtime.HistoryActivityStarted),
			NodeID:     "n1",
			ActivityID: jobID + ":n1:1",
			Timestamp:  now.Add(2 * time.Millisecond),
		},
		{
			RunID:      jobID,
			Type:       string(runtime.HistoryActivityCompleted),
			NodeID:     "n1",
			ActivityID: jobID + ":n1:1",
			Timestamp:  now.Add(3 * time.Millisecond),
			Attributes: map[string]interface{}{
				"output":        "done",
				"tokens_input":  float64(12),
				"tokens_output": float64(18),
				"cost":          float64(0.3),
			},
		},
		{
			RunID:     jobID,
			Type:      string(runtime.HistoryWorkflowCompleted),
			Timestamp: now.Add(4 * time.Millisecond),
			Attributes: map[string]interface{}{
				"total_cost":          float64(0.3),
				"total_tokens":        float64(30),
				"total_input_tokens":  float64(12),
				"total_output_tokens": float64(18),
			},
		},
	}
	for _, evt := range history {
		if err := store.AppendHistoryEvent(ctx, evt); err != nil {
			t.Fatalf("failed to append history event %s: %v", evt.Type, err)
		}
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

	if handler.CallCount() != 0 {
		t.Fatalf("expected no activity execution for already-completed run, got %d calls", handler.CallCount())
	}

	events, err := store.GetHistoryEvents(context.Background(), jobID)
	if err != nil {
		t.Fatalf("failed to load history events: %v", err)
	}
	workflowCompletedCount := 0
	for _, evt := range events {
		if evt.Type == string(runtime.HistoryWorkflowCompleted) {
			workflowCompletedCount++
		}
	}
	if workflowCompletedCount != 1 {
		t.Fatalf("expected exactly one workflow_completed event, got %d", workflowCompletedCount)
	}

	updated, err := store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("failed to reload job: %v", err)
	}
	if updated.Status != "completed" {
		t.Fatalf("expected completed status, got %s", updated.Status)
	}
	if updated.TokensInput != 12 || updated.TokensOutput != 18 || updated.TokensTotal != 30 {
		t.Fatalf(
			"expected token totals 12/18/30, got %d/%d/%d",
			updated.TokensInput, updated.TokensOutput, updated.TokensTotal,
		)
	}
	if updated.Cost != 0.3 {
		t.Fatalf("expected cost 0.3, got %f", updated.Cost)
	}
}

func TestDAGRuntime_ResumeMergesHistoricalAndCurrentTotals(t *testing.T) {
	store, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	registry := runtime.NewActivityHandlerRegistry()
	handler := &scriptedLLMHandler{
		outputs: map[string]runtime.ActivityOutput{
			"n2": {
				Success:      true,
				Output:       "n2-output",
				TokensInput:  5,
				TokensOutput: 7,
				Cost:         0.31,
			},
		},
	}
	registry.Register(handler)
	dagRuntime := NewDAGRuntime(store, registry)

	wf := &workflow.Workflow{
		ID:   "wf-resume-merge-totals",
		Name: "Resume Merge Totals",
		Nodes: []*workflow.Node{
			{ID: "n1", Type: workflow.NodeTypePrompt, Model: "mock-model", Prompt: "first"},
			{ID: "n2", Type: workflow.NodeTypePrompt, Model: "mock-model", Prompt: "second"},
		},
		Edges: []*workflow.Edge{
			{Source: "n1", Target: "n2"},
		},
	}

	snapshot, err := runtime.FreezeWorkflow(
		wf.ID,
		wf.Name,
		wf.Description,
		[]runtime.NodeForFreeze{
			{ID: "n1", Type: string(workflow.NodeTypePrompt), Model: "mock-model", Prompt: "first"},
			{ID: "n2", Type: string(workflow.NodeTypePrompt), Model: "mock-model", Prompt: "second"},
		},
		[]runtime.EdgeForFreeze{
			{Source: "n1", Target: "n2"},
		},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("failed to freeze workflow: %v", err)
	}

	jobID := "job-resume-merge-totals"
	job := &storage.Job{
		ID:                  jobID,
		Description:         "Resume merge totals test",
		Model:               "workflow",
		Status:              "running",
		WorkflowID:          wf.ID,
		WorkflowExecutionID: jobID,
		RunID:               jobID,
		RunNumber:           1,
		DAGSnapshot:         string(snapshot.Definition),
		DAGHash:             snapshot.DAGHash,
	}
	if err := store.CreateExecution(job); err != nil {
		t.Fatalf("failed to create job: %v", err)
	}

	ctx := context.Background()
	now := time.Now()
	history := []*storage.HistoryEvent{
		{RunID: jobID, Type: string(runtime.HistoryWorkflowStarted), Timestamp: now},
		{
			RunID:      jobID,
			Type:       string(runtime.HistoryScheduleActivity),
			NodeID:     "n1",
			ActivityID: jobID + ":n1:1",
			Timestamp:  now.Add(time.Millisecond),
			Attributes: map[string]interface{}{"attempt": float64(1)},
		},
		{
			RunID:      jobID,
			Type:       string(runtime.HistoryActivityStarted),
			NodeID:     "n1",
			ActivityID: jobID + ":n1:1",
			Timestamp:  now.Add(2 * time.Millisecond),
		},
		{
			RunID:      jobID,
			Type:       string(runtime.HistoryActivityCompleted),
			NodeID:     "n1",
			ActivityID: jobID + ":n1:1",
			Timestamp:  now.Add(3 * time.Millisecond),
			Attributes: map[string]interface{}{
				"output":        "n1-output",
				"tokens_input":  float64(11),
				"tokens_output": float64(13),
				"cost":          float64(0.24),
			},
		},
	}
	for _, evt := range history {
		if err := store.AppendHistoryEvent(ctx, evt); err != nil {
			t.Fatalf("failed to append history event %s: %v", evt.Type, err)
		}
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

	if handler.CallCount() != 1 {
		t.Fatalf("expected exactly one executed node after resume, got %d", handler.CallCount())
	}

	updated, err := store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("failed to reload job: %v", err)
	}
	if updated.Status != "completed" {
		t.Fatalf("expected completed status, got %s", updated.Status)
	}

	if updated.TokensInput != 16 || updated.TokensOutput != 20 || updated.TokensTotal != 36 {
		t.Fatalf(
			"expected merged token totals 16/20/36, got %d/%d/%d",
			updated.TokensInput, updated.TokensOutput, updated.TokensTotal,
		)
	}
	if updated.Cost != 0.55 {
		t.Fatalf("expected merged cost 0.55, got %f", updated.Cost)
	}
}

func TestDAGRuntime_UsesCachedActivityAccounting(t *testing.T) {
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
		ID:   "wf-cached-accounting",
		Name: "Cached Accounting",
		Nodes: []*workflow.Node{
			{ID: "n1", Type: workflow.NodeTypePrompt, Model: "mock-model", Prompt: "hello"},
		},
	}
	snapshot, err := runtime.FreezeWorkflow(
		wf.ID,
		wf.Name,
		wf.Description,
		[]runtime.NodeForFreeze{
			{ID: "n1", Type: string(workflow.NodeTypePrompt), Model: "mock-model", Prompt: "hello"},
		},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("failed to freeze workflow: %v", err)
	}

	jobID := "job-cached-accounting"
	job := &storage.Job{
		ID:                  jobID,
		Description:         "Cached accounting test",
		Model:               "workflow",
		Status:              "pending",
		WorkflowID:          wf.ID,
		WorkflowExecutionID: jobID,
		RunID:               jobID,
		RunNumber:           1,
		DAGSnapshot:         string(snapshot.Definition),
		DAGHash:             snapshot.DAGHash,
	}
	if err := store.CreateExecution(job); err != nil {
		t.Fatalf("failed to create job: %v", err)
	}

	cachedOutput := &runtime.ActivityOutput{
		NodeID:       "n1",
		Success:      true,
		Output:       "cached-result",
		TokensInput:  7,
		TokensOutput: 13,
		Cost:         0.11,
		LatencyMs:    42,
	}
	serialized, err := json.Marshal(cachedOutput)
	if err != nil {
		t.Fatalf("failed to marshal cached output: %v", err)
	}
	completedAt := time.Now()
	if err := store.SaveActivityResult(context.Background(), &storage.ActivityResult{
		IdempotencyKey: jobID + ":n1:1",
		RunID:          jobID,
		NodeID:         "n1",
		Attempt:        1,
		ActivityType:   string(runtime.ActivityTypeLLMCall),
		Status:         "completed",
		OutputPayload:  string(serialized),
		CompletedAt:    &completedAt,
	}); err != nil {
		t.Fatalf("failed to save cached activity result: %v", err)
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

	if handler.CallCount() != 0 {
		t.Fatalf("expected cached activity result to avoid handler call, got %d", handler.CallCount())
	}

	updated, err := store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("failed to reload job: %v", err)
	}
	if updated.Status != "completed" {
		t.Fatalf("expected completed status, got %s", updated.Status)
	}
	if updated.TokensInput != 7 || updated.TokensOutput != 13 || updated.TokensTotal != 20 {
		t.Fatalf(
			"expected token totals 7/13/20, got %d/%d/%d",
			updated.TokensInput, updated.TokensOutput, updated.TokensTotal,
		)
	}
	if updated.Cost != 0.11 {
		t.Fatalf("expected cost 0.11, got %f", updated.Cost)
	}
}

// capturingLLMHandler captures the workflow context passed to it for inspection.
type capturingLLMHandler struct {
	mu              sync.Mutex
	calls           int
	capturedContext map[string]interface{}
}

func (h *capturingLLMHandler) Type() runtime.ActivityType {
	return runtime.ActivityTypeLLMCall
}

func (h *capturingLLMHandler) Execute(ctx context.Context, input *runtime.ActivityInput) (*runtime.ActivityOutput, error) {
	h.mu.Lock()
	h.calls++
	// Deep-copy the workflow context so it doesn't get mutated after capture
	h.capturedContext = make(map[string]interface{}, len(input.WorkflowContext))
	for k, v := range input.WorkflowContext {
		h.capturedContext[k] = v
	}
	h.mu.Unlock()
	return &runtime.ActivityOutput{
		NodeID:  input.NodeID,
		Success: true,
		Output:  "result-output",
	}, nil
}

func (h *capturingLLMHandler) CapturedContext() map[string]interface{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.capturedContext
}

func TestDAGRuntime_ResumeRestoresModelContextKeys(t *testing.T) {
	store, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	registry := runtime.NewActivityHandlerRegistry()
	handler := &capturingLLMHandler{}
	registry.Register(handler)
	dagRuntime := NewDAGRuntime(store, registry)

	// Two agent nodes (completed) feed into a pending third node.
	// Agent nodes have models that should be restored on resume.
	wf := &workflow.Workflow{
		ID:   "wf-model-resume",
		Name: "Model Resume Test",
		Nodes: []*workflow.Node{
			{ID: "agent-a", Type: workflow.NodeTypePrompt, Model: "x-ai/grok-4.1-fast", Prompt: "hello"},
			{ID: "agent-b", Type: workflow.NodeTypePrompt, Model: "minimax/minimax-m2.5", Prompt: "hello"},
			{ID: "n3", Type: workflow.NodeTypePrompt, Model: "mock-model", Prompt: "combine {{agent-a}} {{agent-b}}"},
		},
		Edges: []*workflow.Edge{
			{Source: "agent-a", Target: "n3"},
			{Source: "agent-b", Target: "n3"},
		},
	}

	snapshot, err := runtime.FreezeWorkflow(
		wf.ID,
		wf.Name,
		wf.Description,
		[]runtime.NodeForFreeze{
			{ID: "agent-a", Type: string(workflow.NodeTypePrompt), Model: "x-ai/grok-4.1-fast", Prompt: "hello"},
			{ID: "agent-b", Type: string(workflow.NodeTypePrompt), Model: "minimax/minimax-m2.5", Prompt: "hello"},
			{ID: "n3", Type: string(workflow.NodeTypePrompt), Model: "mock-model", Prompt: "combine {{agent-a}} {{agent-b}}"},
		},
		[]runtime.EdgeForFreeze{
			{Source: "agent-a", Target: "n3"},
			{Source: "agent-b", Target: "n3"},
		},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("failed to freeze workflow: %v", err)
	}

	jobID := "job-model-resume"
	job := &storage.Job{
		ID:                  jobID,
		Description:         "Model resume test",
		Model:               "workflow",
		Status:              "running",
		WorkflowID:          wf.ID,
		WorkflowExecutionID: jobID,
		RunID:               jobID,
		RunNumber:           1,
		DAGSnapshot:         string(snapshot.Definition),
		DAGHash:             snapshot.DAGHash,
	}
	if err := store.CreateExecution(job); err != nil {
		t.Fatalf("failed to create job: %v", err)
	}

	ctx := context.Background()
	now := time.Now()
	// Seed history: agent-a and agent-b already completed
	history := []*storage.HistoryEvent{
		{RunID: jobID, Type: string(runtime.HistoryWorkflowStarted), Timestamp: now},
		{
			RunID:      jobID,
			Type:       string(runtime.HistoryScheduleActivity),
			NodeID:     "agent-a",
			ActivityID: jobID + ":agent-a:1",
			Timestamp:  now.Add(time.Millisecond),
			Attributes: map[string]interface{}{"attempt": float64(1)},
		},
		{
			RunID:      jobID,
			Type:       string(runtime.HistoryActivityStarted),
			NodeID:     "agent-a",
			ActivityID: jobID + ":agent-a:1",
			Timestamp:  now.Add(2 * time.Millisecond),
		},
		{
			RunID:      jobID,
			Type:       string(runtime.HistoryActivityCompleted),
			NodeID:     "agent-a",
			ActivityID: jobID + ":agent-a:1",
			Timestamp:  now.Add(3 * time.Millisecond),
			Attributes: map[string]interface{}{
				"output":        "response-A",
				"tokens_input":  float64(10),
				"tokens_output": float64(15),
				"cost":          float64(0.1),
			},
		},
		{
			RunID:      jobID,
			Type:       string(runtime.HistoryScheduleActivity),
			NodeID:     "agent-b",
			ActivityID: jobID + ":agent-b:1",
			Timestamp:  now.Add(4 * time.Millisecond),
			Attributes: map[string]interface{}{"attempt": float64(1)},
		},
		{
			RunID:      jobID,
			Type:       string(runtime.HistoryActivityStarted),
			NodeID:     "agent-b",
			ActivityID: jobID + ":agent-b:1",
			Timestamp:  now.Add(5 * time.Millisecond),
		},
		{
			RunID:      jobID,
			Type:       string(runtime.HistoryActivityCompleted),
			NodeID:     "agent-b",
			ActivityID: jobID + ":agent-b:1",
			Timestamp:  now.Add(6 * time.Millisecond),
			Attributes: map[string]interface{}{
				"output":        "response-B",
				"tokens_input":  float64(10),
				"tokens_output": float64(15),
				"cost":          float64(0.1),
			},
		},
	}
	for _, evt := range history {
		if err := store.AppendHistoryEvent(ctx, evt); err != nil {
			t.Fatalf("failed to append history event %s: %v", evt.Type, err)
		}
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

	// n3 should have been executed (the only pending node after resume)
	captured := handler.CapturedContext()
	if captured == nil {
		t.Fatal("expected capturing handler to have been called with workflow context")
	}

	// Verify __model keys were restored for completed agent nodes
	modelA, ok := captured["agent-a__model"].(string)
	if !ok || modelA != "x-ai/grok-4.1-fast" {
		t.Errorf("expected agent-a__model = 'x-ai/grok-4.1-fast', got %q (ok=%v)", modelA, ok)
	}
	modelB, ok := captured["agent-b__model"].(string)
	if !ok || modelB != "minimax/minimax-m2.5" {
		t.Errorf("expected agent-b__model = 'minimax/minimax-m2.5', got %q (ok=%v)", modelB, ok)
	}

	// Verify outputs were also restored
	outputA, _ := captured["agent-a"].(string)
	if outputA != "response-A" {
		t.Errorf("expected agent-a output 'response-A', got %q", outputA)
	}
	outputB, _ := captured["agent-b"].(string)
	if outputB != "response-B" {
		t.Errorf("expected agent-b output 'response-B', got %q", outputB)
	}
}

func TestDAGRuntime_ResumeUsesFrozenExpandedAggregationSnapshot(t *testing.T) {
	store, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	registry := runtime.NewActivityHandlerRegistry()
	handler := &scriptedLLMHandler{
		outputs: map[string]runtime.ActivityOutput{
			"agg--synthesize": {Success: true, Output: "Synthesized from frozen snapshot"},
		},
	}
	registry.Register(handler)
	dagRuntime := NewDAGRuntime(store, registry)

	frozenExpanded := &workflow.Workflow{
		ID:   "wf-source-ref",
		Name: "Source Ref",
		Nodes: []*workflow.Node{
			{ID: "agent-a", Type: workflow.NodeTypePrompt, Model: "solver-a", Prompt: "hello"},
			{ID: "agent-b", Type: workflow.NodeTypePrompt, Model: "solver-b", Prompt: "hello"},
			{
				ID:     "agg--synthesize",
				Type:   workflow.NodeTypePrompt,
				Model:  "synth-model",
				Prompt: "combine {{agent-a}} {{agent-b}}",
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
			{Source: "agent-a", Target: "agg--synthesize"},
			{Source: "agent-b", Target: "agg--synthesize"},
		},
	}
	snapshot := freezeWorkflowForDurableTest(t, frozenExpanded)
	workflowWithStaleSourceRef := &workflow.Workflow{
		ID:    frozenExpanded.ID,
		Name:  frozenExpanded.Name,
		Nodes: append([]*workflow.Node{}, frozenExpanded.Nodes...),
		Edges: append([]*workflow.Edge{}, frozenExpanded.Edges...),
	}
	workflowWithStaleSourceRef.Nodes = append(workflowWithStaleSourceRef.Nodes, &workflow.Node{
		ID:            "agg",
		Type:          workflow.NodeTypeWorkflowRef,
		WorkflowRefID: "aggregation-synthesis",
	})
	workflowWithStaleSourceRef.Edges = append(workflowWithStaleSourceRef.Edges,
		&workflow.Edge{Source: "agent-a", Target: "agg"},
		&workflow.Edge{Source: "agent-b", Target: "agg"},
	)

	jobID := "job-resume-expanded-agg"
	createDurableJobForTest(t, store, jobID, frozenExpanded, snapshot)

	now := time.Now()
	for _, evt := range []*storage.HistoryEvent{
		{RunID: jobID, Type: string(runtime.HistoryWorkflowStarted), Timestamp: now},
		{
			RunID:      jobID,
			Type:       string(runtime.HistoryScheduleActivity),
			NodeID:     "agent-a",
			ActivityID: jobID + ":agent-a:1",
			Timestamp:  now.Add(time.Millisecond),
			Attributes: map[string]interface{}{"attempt": float64(1)},
		},
		{
			RunID:      jobID,
			Type:       string(runtime.HistoryActivityStarted),
			NodeID:     "agent-a",
			ActivityID: jobID + ":agent-a:1",
			Timestamp:  now.Add(2 * time.Millisecond),
		},
		{
			RunID:      jobID,
			Type:       string(runtime.HistoryActivityCompleted),
			NodeID:     "agent-a",
			ActivityID: jobID + ":agent-a:1",
			Timestamp:  now.Add(3 * time.Millisecond),
			Attributes: map[string]interface{}{"output": "Answer A"},
		},
		{
			RunID:      jobID,
			Type:       string(runtime.HistoryScheduleActivity),
			NodeID:     "agent-b",
			ActivityID: jobID + ":agent-b:1",
			Timestamp:  now.Add(4 * time.Millisecond),
			Attributes: map[string]interface{}{"attempt": float64(1)},
		},
		{
			RunID:      jobID,
			Type:       string(runtime.HistoryActivityStarted),
			NodeID:     "agent-b",
			ActivityID: jobID + ":agent-b:1",
			Timestamp:  now.Add(5 * time.Millisecond),
		},
		{
			RunID:      jobID,
			Type:       string(runtime.HistoryActivityCompleted),
			NodeID:     "agent-b",
			ActivityID: jobID + ":agent-b:1",
			Timestamp:  now.Add(6 * time.Millisecond),
			Attributes: map[string]interface{}{"output": "Answer B"},
		},
	} {
		if err := store.AppendHistoryEvent(context.Background(), evt); err != nil {
			t.Fatalf("failed to append history event %s/%s: %v", evt.Type, evt.NodeID, err)
		}
	}

	if err := dagRuntime.Start(context.Background(), &runtime.StartParams{
		Identity: &runtime.ExecutionIdentity{
			WorkflowExecutionID: jobID,
			RunID:               jobID,
			RunNumber:           1,
			DAGHash:             snapshot.DAGHash,
		},
		Snapshot: snapshot,
		Workflow: workflowWithStaleSourceRef,
	}); err != nil {
		t.Fatalf("runtime start failed: %v", err)
	}

	if handler.CallCount() != 1 {
		t.Fatalf("handler calls = %d, want 1", handler.CallCount())
	}
	history, err := store.GetHistoryEvents(context.Background(), jobID)
	if err != nil {
		t.Fatalf("failed to load history: %v", err)
	}
	if got := countHistoryByTypeAndNode(history, string(runtime.HistoryActivityStarted), "agg--synthesize"); got != 1 {
		t.Fatalf("agg--synthesize activity_started = %d, want 1", got)
	}
	if got := countHistoryByTypeAndNode(history, string(runtime.HistoryActivityStarted), "agg"); got != 0 {
		t.Fatalf("source workflow_ref node agg should not execute, activity_started = %d", got)
	}
}
