package storage

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"
)

// --- Field fidelity helpers (regression tests for execution scan refactoring) ---

// withAllFields populates every field on a WorkflowExecution that is writable
// via CreateExecution. Used to verify that query+scan functions round-trip all
// 28 columns correctly.
func withAllFields() func(*Job) {
	return func(j *Job) {
		j.Description = "full-field test query"
		j.Model = "test-model-v2"
		j.RequestData = `{"prompt":"hello"}`
		j.WorkflowID = "wf-full"
		j.ParentExecutionID = "parent-exec-1"
		j.IdempotencyKey = "idem-" + j.ID
		j.RequestHash = "hash-" + j.ID
		j.ConfigHash = "cfg-" + j.ID
		j.UserID = "user-full"
		j.WorkflowExecutionID = j.ID
		j.RunID = j.ID
		j.RunNumber = 3
		j.PreviousRunID = "prev-" + j.ID
		j.DAGSnapshot = `{"nodes":[{"id":"n1"}]}`
		j.DAGHash = "daghash-" + j.ID
	}
}

// assertExecutionFields checks all writable fields round-tripped correctly.
// responseData, resultText, errorMessage, tokens, cost, retryCount, archivedAt
// are set after creation via Update/Complete, so we only check fields set at create time.
func assertExecutionFields(t *testing.T, label string, got *WorkflowExecution, wantID string) {
	t.Helper()
	if got.ID != wantID {
		t.Errorf("%s: ID = %q, want %q", label, got.ID, wantID)
	}
	if got.Description != "full-field test query" {
		t.Errorf("%s: Description = %q, want %q", label, got.Description, "full-field test query")
	}
	if got.Model != "test-model-v2" {
		t.Errorf("%s: Model = %q, want %q", label, got.Model, "test-model-v2")
	}
	if got.RequestData != `{"prompt":"hello"}` {
		t.Errorf("%s: RequestData = %q, want JSON", label, got.RequestData)
	}
	if got.WorkflowID != "wf-full" {
		t.Errorf("%s: WorkflowID = %q, want %q", label, got.WorkflowID, "wf-full")
	}
	if got.ParentExecutionID != "parent-exec-1" {
		t.Errorf("%s: ParentExecutionID = %q, want %q", label, got.ParentExecutionID, "parent-exec-1")
	}
	if got.IdempotencyKey != "idem-"+wantID {
		t.Errorf("%s: IdempotencyKey = %q, want %q", label, got.IdempotencyKey, "idem-"+wantID)
	}
	if got.RequestHash != "hash-"+wantID {
		t.Errorf("%s: RequestHash = %q, want %q", label, got.RequestHash, "hash-"+wantID)
	}
	if got.ConfigHash != "cfg-"+wantID {
		t.Errorf("%s: ConfigHash = %q, want %q", label, got.ConfigHash, "cfg-"+wantID)
	}
	if got.UserID != "user-full" {
		t.Errorf("%s: UserID = %q, want %q", label, got.UserID, "user-full")
	}
	if got.WorkflowExecutionID != wantID {
		t.Errorf("%s: WorkflowExecutionID = %q, want %q", label, got.WorkflowExecutionID, wantID)
	}
	if got.RunID != wantID {
		t.Errorf("%s: RunID = %q, want %q", label, got.RunID, wantID)
	}
	if got.RunNumber != 3 {
		t.Errorf("%s: RunNumber = %d, want 3", label, got.RunNumber)
	}
	if got.PreviousRunID != "prev-"+wantID {
		t.Errorf("%s: PreviousRunID = %q, want %q", label, got.PreviousRunID, "prev-"+wantID)
	}
	if got.DAGSnapshot != `{"nodes":[{"id":"n1"}]}` {
		t.Errorf("%s: DAGSnapshot = %q", label, got.DAGSnapshot)
	}
	if got.DAGHash != "daghash-"+wantID {
		t.Errorf("%s: DAGHash = %q, want %q", label, got.DAGHash, "daghash-"+wantID)
	}
	if got.CreatedAt.IsZero() {
		t.Errorf("%s: CreatedAt is zero", label)
	}
	if got.UpdatedAt.IsZero() {
		t.Errorf("%s: UpdatedAt is zero", label)
	}
}

// --- Shared helpers ---

// newTestStore creates an in-memory storage instance for testing.
func newTestStore(t *testing.T) *Storage {
	t.Helper()
	s, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestDBQueryDiagnosticsLogsWhenEnabled(t *testing.T) {
	t.Setenv("DB_QUERY_LOG_ENABLED", "true")
	t.Setenv("DB_QUERY_LOG_ALL", "true")
	t.Setenv("DB_SLOW_QUERY_THRESHOLD_MS", "1")

	var logBuf bytes.Buffer
	oldFlags := log.Flags()
	log.SetOutput(&logBuf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetFlags(oldFlags)
	})

	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage failed: %v", err)
	}
	defer store.Close()

	if _, err := store.DB().ExecContext(context.Background(), "SELECT 1"); err != nil {
		t.Fatalf("diagnostic query failed: %v", err)
	}

	logs := logBuf.String()
	if !strings.Contains(logs, "[DB query]") {
		t.Fatalf("expected diagnostic DB query log, got: %s", logs)
	}
	if !strings.Contains(logs, "operation=exec") {
		t.Fatalf("expected DB log to include operation, got: %s", logs)
	}
}

func TestDBQueryDiagnosticsLogsRowsWhenClosed(t *testing.T) {
	t.Setenv("DB_QUERY_LOG_ENABLED", "true")
	t.Setenv("DB_QUERY_LOG_ALL", "true")
	t.Setenv("DB_SLOW_QUERY_THRESHOLD_MS", "1")

	var logBuf bytes.Buffer
	oldFlags := log.Flags()
	log.SetOutput(&logBuf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(os.Stderr)
		log.SetFlags(oldFlags)
	})

	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage failed: %v", err)
	}
	defer store.Close()

	rows, err := store.DB().QueryContext(context.Background(), "SELECT 1")
	if err != nil {
		t.Fatalf("diagnostic query failed: %v", err)
	}
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan failed: %v", err)
		}
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close rows failed: %v", err)
	}

	logs := logBuf.String()
	if !strings.Contains(logs, "operation=rows") {
		t.Fatalf("expected DB row lifetime log, got: %s", logs)
	}
}

func TestDBDiagnosticsReportsConfiguredBusyTimeout(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage failed: %v", err)
	}
	defer store.Close()

	diag := store.DBDiagnostics(context.Background())
	if diag.SQLite.BusyTimeoutMs != 5000 {
		t.Fatalf("expected SQLite busy_timeout 5000ms, got %d", diag.SQLite.BusyTimeoutMs)
	}
	if diag.SQLite.JournalMode == "" {
		t.Fatal("expected SQLite journal mode diagnostic to be populated")
	}
}

// createJobWithStatus creates a minimal job in the store and fails the test on error.
// Named to avoid collision with createTestJob in event_contract_test.go.
func createJobWithStatus(t *testing.T, s *Storage, id, status string) {
	t.Helper()
	job := &Job{
		ID:          id,
		Description: "test",
		Model:       "gpt-4",
		Status:      status,
	}
	if err := s.CreateExecution(job); err != nil {
		t.Fatalf("Failed to create job %s: %v", id, err)
	}
}

// createTestExecution creates a workflow execution with optional overrides and
// fails the test on error. Supports IdempotencyKey, RequestHash, UserID, and
// durable runtime fields.
func createTestExecution(t *testing.T, s *Storage, id, status string, opts ...func(*Job)) {
	t.Helper()
	job := &Job{
		ID:          id,
		Description: "test",
		Model:       "workflow",
		Status:      status,
		WorkflowID:  "wf-1",
	}
	for _, fn := range opts {
		fn(job)
	}
	if err := s.CreateExecution(job); err != nil {
		t.Fatalf("Failed to create execution %s: %v", id, err)
	}
}

func withIdempotencyKey(key string) func(*Job) {
	return func(j *Job) { j.IdempotencyKey = key }
}

func withRequestHash(hash string) func(*Job) {
	return func(j *Job) { j.RequestHash = hash }
}

func withUserID(uid string) func(*Job) {
	return func(j *Job) { j.UserID = uid }
}

func withDurableFields(execID, runID, dagHash, dagSnapshot string, runNumber int) func(*Job) {
	return func(j *Job) {
		j.WorkflowExecutionID = execID
		j.RunID = runID
		j.DAGHash = dagHash
		j.DAGSnapshot = dagSnapshot
		j.RunNumber = runNumber
	}
}

// --- Tests ---

// TestCreateAndGetJob tests basic job CRUD operations
func TestCreateAndGetJob(t *testing.T) {
	store := newTestStore(t)

	job := &Job{
		ID:          "test-123",
		Description: "What is AI?",
		Model:       "gpt-4",
		Status:      "pending",
	}
	if err := store.CreateExecution(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	retrieved, err := store.GetExecution("test-123")
	if err != nil {
		t.Fatalf("Failed to get job: %v", err)
	}

	if retrieved.ID != job.ID {
		t.Errorf("Expected ID %s, got %s", job.ID, retrieved.ID)
	}
	if retrieved.Description != job.Description {
		t.Errorf("Expected query %s, got %s", job.Description, retrieved.Description)
	}
	if retrieved.Model != job.Model {
		t.Errorf("Expected model %s, got %s", job.Model, retrieved.Model)
	}
	if retrieved.Status != job.Status {
		t.Errorf("Expected status %s, got %s", job.Status, retrieved.Status)
	}
}

// TestGetNonExistentJob tests error handling for missing jobs
func TestGetNonExistentJob(t *testing.T) {
	store := newTestStore(t)

	_, err := store.GetExecution("does-not-exist")
	if err != ErrNotFound {
		t.Errorf("Expected ErrNotFound, got: %v", err)
	}
}

// TestUpdateJob tests job updates
func TestUpdateJob(t *testing.T) {
	store := newTestStore(t)

	job := &Job{
		ID:          "update-test",
		Description: "Test query",
		Model:       "gpt-4",
		Status:      "pending",
	}
	store.CreateExecution(job)

	job.Status = "completed"
	job.ResultText = "Test result"
	job.TokensInput = 10
	job.TokensOutput = 20
	job.TokensTotal = 30
	job.Cost = 0.005

	if err := store.UpdateExecution(job); err != nil {
		t.Fatalf("Failed to update job: %v", err)
	}

	updated, _ := store.GetExecution("update-test")
	if updated.Status != "completed" {
		t.Errorf("Expected status completed, got %s", updated.Status)
	}
	if updated.ResultText != "Test result" {
		t.Errorf("Expected result text, got %s", updated.ResultText)
	}
	if updated.TokensTotal != 30 {
		t.Errorf("Expected 30 tokens, got %d", updated.TokensTotal)
	}
	if updated.Cost != 0.005 {
		t.Errorf("Expected cost 0.005, got %f", updated.Cost)
	}
}

// TestListJobs tests job listing
func TestListJobs(t *testing.T) {
	store := newTestStore(t)

	for i := 1; i <= 5; i++ {
		job := &Job{
			ID:          fmt.Sprintf("job-%d", i),
			Description: fmt.Sprintf("Query %d", i),
			Model:       "gpt-4",
			Status:      "completed",
		}
		store.CreateExecution(job)
		time.Sleep(time.Millisecond) // Ensure different timestamps
	}

	jobs, err := store.ListExecutions(3)
	if err != nil {
		t.Fatalf("Failed to list jobs: %v", err)
	}

	if len(jobs) != 3 {
		t.Errorf("Expected 3 jobs, got %d", len(jobs))
	}

	// Verify order (most recent first)
	if jobs[0].ID != "job-5" {
		t.Errorf("Expected most recent job first, got %s", jobs[0].ID)
	}
}

// TestListJobsByStatus tests filtering by status
func TestListJobsByStatus(t *testing.T) {
	store := newTestStore(t)

	statuses := []string{"completed", "failed", "completed", "pending", "completed"}
	for i, status := range statuses {
		job := &Job{
			ID:          fmt.Sprintf("job-%d", i),
			Description: "Test",
			Model:       "gpt-4",
			Status:      status,
		}
		store.CreateExecution(job)
	}

	completed, err := store.ListExecutionsByStatus("completed", 10)
	if err != nil {
		t.Fatalf("Failed to list completed jobs: %v", err)
	}

	if len(completed) != 3 {
		t.Errorf("Expected 3 completed jobs, got %d", len(completed))
	}

	for _, job := range completed {
		if job.Status != "completed" {
			t.Errorf("Expected completed status, got %s", job.Status)
		}
	}

	failed, _ := store.ListExecutionsByStatus("failed", 10)
	if len(failed) != 1 {
		t.Errorf("Expected 1 failed job, got %d", len(failed))
	}
}

// TestNodeExecutionAttempts merges two previously separate tests:
// - Basic attempt tracking (upsert, ordering, status, error message, tokens)
// - Node-level fields (node_id, error_code, per-node query, non-existent node query)
func TestNodeExecutionAttempts(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	createJobWithStatus(t, store, "log-test", "running")

	// Upsert attempt 1 (failed, with error code)
	if err := store.UpsertNodeExecutionAttempt(ctx, &NodeExecutionAttempt{
		JobID:        "log-test",
		ExecutionID:  "log-test",
		RunID:        "log-test",
		NodeID:       "node-A",
		NodeType:     "prompt",
		Attempt:      1,
		Status:       "failed",
		LatencyMs:    150.5,
		TokensInput:  100,
		TokensOutput: 0,
		ErrorMessage: "rate limit exceeded",
		ErrorCode:    "RATE_LIMIT",
	}); err != nil {
		t.Fatalf("Failed to upsert attempt 1: %v", err)
	}

	// Upsert attempt 2 (completed)
	if err := store.UpsertNodeExecutionAttempt(ctx, &NodeExecutionAttempt{
		JobID:        "log-test",
		ExecutionID:  "log-test",
		RunID:        "log-test",
		NodeID:       "node-A",
		NodeType:     "prompt",
		Attempt:      2,
		Status:       "completed",
		LatencyMs:    200.3,
		TokensInput:  100,
		TokensOutput: 50,
	}); err != nil {
		t.Fatalf("Failed to upsert attempt 2: %v", err)
	}

	t.Run("GetAll", func(t *testing.T) {
		attempts, err := store.GetNodeExecutionAttempts("log-test")
		if err != nil {
			t.Fatalf("Failed to get attempts: %v", err)
		}
		if len(attempts) != 2 {
			t.Fatalf("Expected 2 attempts, got %d", len(attempts))
		}

		// Verify ordering by attempt number
		if attempts[0].Attempt != 1 {
			t.Error("Expected attempts ordered by attempt number")
		}

		// First attempt: failed with error details
		a0 := attempts[0]
		if a0.Status != "failed" {
			t.Errorf("attempts[0].Status = %q, want %q", a0.Status, "failed")
		}
		if a0.ErrorMessage != "rate limit exceeded" {
			t.Errorf("attempts[0].ErrorMessage = %q, want %q", a0.ErrorMessage, "rate limit exceeded")
		}
		if a0.ErrorCode != "RATE_LIMIT" {
			t.Errorf("attempts[0].ErrorCode = %q, want %q", a0.ErrorCode, "RATE_LIMIT")
		}
		if a0.NodeID != "node-A" {
			t.Errorf("attempts[0].NodeID = %q, want %q", a0.NodeID, "node-A")
		}

		// Second attempt: completed with output tokens
		a1 := attempts[1]
		if a1.Attempt != 2 {
			t.Errorf("attempts[1].Attempt = %d, want 2", a1.Attempt)
		}
		if a1.Status != "completed" {
			t.Errorf("attempts[1].Status = %q, want %q", a1.Status, "completed")
		}
		if a1.TokensOutput != 50 {
			t.Errorf("attempts[1].TokensOutput = %d, want 50", a1.TokensOutput)
		}
	})

	t.Run("GetForNode", func(t *testing.T) {
		nodeAttempts, err := store.GetNodeExecutionAttemptsForNode("log-test", "node-A")
		if err != nil {
			t.Fatalf("Failed to get node attempts: %v", err)
		}
		if len(nodeAttempts) != 2 {
			t.Errorf("expected 2 node attempts, got %d", len(nodeAttempts))
		}
	})

	t.Run("GetForNonExistentNode", func(t *testing.T) {
		emptyAttempts, err := store.GetNodeExecutionAttemptsForNode("log-test", "node-Z")
		if err != nil {
			t.Fatalf("Failed to get empty attempts: %v", err)
		}
		if len(emptyAttempts) != 0 {
			t.Errorf("expected 0 attempts for non-existent node, got %d", len(emptyAttempts))
		}
	})
}

// TestWorkflowFields tests workflow-specific fields
func TestWorkflowFields(t *testing.T) {
	store := newTestStore(t)

	job := &Job{
		ID:          "wf-123",
		Description: "Workflow query",
		Model:       "gpt-4",
		Status:      "pending",
		WorkflowID:  "my-workflow",
	}
	if err := store.CreateExecution(job); err != nil {
		t.Fatalf("Failed to create workflow job: %v", err)
	}

	retrieved, _ := store.GetExecution("wf-123")
	if retrieved.WorkflowID != "my-workflow" {
		t.Errorf("Expected workflow ID 'my-workflow', got %s", retrieved.WorkflowID)
	}
}

// TestNodeExecutions tests workflow node execution tracking
func TestNodeExecutions(t *testing.T) {
	store := newTestStore(t)

	createJobWithStatus(t, store, "wf-nodes", "pending")

	node1 := &WorkflowNode{
		ExecutionID:  "wf-nodes",
		NodeID:       "research",
		NodeType:     "prompt",
		NodeOrder:    0,
		Status:       "completed",
		Prompt:       "Research topic",
		Model:        "gpt-4",
		Output:       "Research results",
		TokensInput:  10,
		TokensOutput: 20,
		Cost:         0.001,
		LatencyMs:    150.5,
	}
	store.AddWorkflowNode(node1)

	node2 := &WorkflowNode{
		ExecutionID:  "wf-nodes",
		NodeID:       "summarize",
		NodeType:     "prompt",
		NodeOrder:    1,
		Status:       "completed",
		Prompt:       "Summarize research",
		Model:        "gpt-4",
		Output:       "Summary",
		TokensInput:  15,
		TokensOutput: 25,
		Cost:         0.002,
		LatencyMs:    180.0,
	}
	store.AddWorkflowNode(node2)

	nodes, err := store.GetWorkflowNodes("wf-nodes")
	if err != nil {
		t.Fatalf("Failed to get workflow nodes: %v", err)
	}

	if len(nodes) != 2 {
		t.Errorf("Expected 2 nodes, got %d", len(nodes))
	}
	if nodes[0].NodeID != "research" {
		t.Error("Expected nodes ordered by node_order")
	}
	if nodes[0].TokensInput != 10 {
		t.Errorf("Expected 10 input tokens, got %d", nodes[0].TokensInput)
	}
	if nodes[1].Cost != 0.002 {
		t.Errorf("Expected cost 0.002, got %f", nodes[1].Cost)
	}
}

// TestArchiveAndUnarchiveJob tests job archival and restoration as two phases
// of a single lifecycle. Previously split across two separate tests.
func TestArchiveAndUnarchiveJob(t *testing.T) {
	store := newTestStore(t)

	job := &Job{
		ID:          "archive-test",
		Description: "Test",
		Model:       "gpt-4",
		Status:      "completed",
	}
	store.CreateExecution(job)

	// --- Phase 1: Archive ---
	t.Run("Archive", func(t *testing.T) {
		if err := store.ArchiveExecution("archive-test"); err != nil {
			t.Fatalf("Failed to archive job: %v", err)
		}

		archived, _ := store.GetExecution("archive-test")
		if archived.Status != "archived" {
			t.Errorf("Expected archived status, got %s", archived.Status)
		}
		if archived.ArchivedAt == nil {
			t.Error("Expected archived_at timestamp")
		}

		// Verify not in default list
		jobs, _ := store.ListExecutions(10)
		for _, j := range jobs {
			if j.ID == "archive-test" {
				t.Error("Archived job should not appear in default list")
			}
		}

		// Verify in archived list
		archivedJobs, _ := store.ListExecutionsByStatus("archived", 10)
		found := false
		for _, j := range archivedJobs {
			if j.ID == "archive-test" {
				found = true
				break
			}
		}
		if !found {
			t.Error("Archived job should appear in archived list")
		}
	})

	// --- Phase 2: Unarchive ---
	t.Run("Unarchive", func(t *testing.T) {
		if err := store.UnarchiveExecution("archive-test", "completed"); err != nil {
			t.Fatalf("Failed to unarchive job: %v", err)
		}

		restored, _ := store.GetExecution("archive-test")
		if restored.Status != "completed" {
			t.Errorf("Expected completed status, got %s", restored.Status)
		}
		if restored.ArchivedAt != nil {
			t.Error("Expected archived_at to be NULL after unarchive")
		}

		// Verify appears in default list again
		jobs, _ := store.ListExecutions(10)
		found := false
		for _, j := range jobs {
			if j.ID == "archive-test" {
				found = true
				break
			}
		}
		if !found {
			t.Error("Unarchived job should appear in default list")
		}
	})
}

// TestConcurrentAccess tests basic concurrent operations
func TestConcurrentAccess(t *testing.T) {
	store := newTestStore(t)

	done := make(chan bool)
	for i := 0; i < 5; i++ {
		go func(id int) {
			job := &Job{
				ID:          fmt.Sprintf("concurrent-%d", id),
				Description: "Test",
				Model:       "gpt-4",
				Status:      "pending",
			}
			store.CreateExecution(job)
			done <- true
		}(i)
	}

	for i := 0; i < 5; i++ {
		<-done
	}

	jobs, _ := store.ListExecutions(10)
	if len(jobs) < 5 {
		t.Errorf("Expected at least 5 jobs, got %d", len(jobs))
	}
}

// TestLogLLMRequest_AttemptFields verifies that attempt_number and execution_uid
// are written and readable on workflow_node_executions. Prevents silent field omission in UPSERT.
func TestLogLLMRequest_AttemptFields(t *testing.T) {
	store := newTestStore(t)

	createJobWithStatus(t, store, "job-attempt-1", "running")

	// Log initial attempt (running)
	if err := store.LogLLMRequestFull(&LLMRequestLog{
		JobID: "job-attempt-1", NodeID: "node-1", Model: "mock-model",
		Prompt:    "prompt",
		Status:    "running",
		NodeLabel: "Node 1", NodeName: "Node One",
		AttemptNumber: 1, ExecutionUID: "job-attempt-1:node-1:1", RunID: "job-attempt-1",
	}); err != nil {
		t.Fatalf("Failed to log initial attempt: %v", err)
	}

	// Log completion with attempt 2 (simulating retry success)
	if err := store.LogLLMRequestFull(&LLMRequestLog{
		JobID: "job-attempt-1", NodeID: "node-1", Model: "mock-model",
		Prompt: "prompt", Response: "response text",
		TokensIn: 100, TokensOut: 50, Cost: 0.05, Latency: 250.0,
		Status:    "completed",
		NodeLabel: "Node 1", NodeName: "Node One",
		AttemptNumber: 2, ExecutionUID: "job-attempt-1:node-1:2", RunID: "job-attempt-1",
	}); err != nil {
		t.Fatalf("Failed to log retry completion: %v", err)
	}

	nodes, err := store.GetWorkflowNodes("job-attempt-1")
	if err != nil {
		t.Fatalf("Failed to get workflow nodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node (UPSERT), got %d", len(nodes))
	}

	node := nodes[0]
	if node.Status != "completed" {
		t.Errorf("node status = %q, want %q", node.Status, "completed")
	}
	if node.AttemptNumber != 2 {
		t.Errorf("node attempt_number = %d, want 2", node.AttemptNumber)
	}
	if node.ExecutionUID != "job-attempt-1:node-1:2" {
		t.Errorf("node execution_uid = %q, want %q", node.ExecutionUID, "job-attempt-1:node-1:2")
	}
	if node.Output != "response text" {
		t.Errorf("node output = %q, want %q", node.Output, "response text")
	}
}

// --- Dedup Eligibility Tests ---

func TestGetEligibleExecutionByIdempotencyKey_EligibilityMatrix(t *testing.T) {
	store := newTestStore(t)

	tests := []struct {
		status   string
		eligible bool
	}{
		{"pending", true},
		{"running", true},
		{"paused", true},
		{"completed", true},
		{"failed", false},
		{"cancelled", false},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			key := "key-" + tt.status
			createTestExecution(t, store, "job-"+tt.status, tt.status, withIdempotencyKey(key))

			result, err := store.GetEligibleExecutionByIdempotencyKey(key, "")
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if tt.eligible && result == nil {
				t.Errorf("Expected eligible job for status %q, got nil", tt.status)
			}
			if !tt.eligible && result != nil {
				t.Errorf("Expected nil for status %q, got job %s", tt.status, result.ID)
			}
		})
	}
}

func TestFindRecentEligibleByRequestHash_StatusFiltering(t *testing.T) {
	store := newTestStore(t)

	tests := []struct {
		status   string
		eligible bool
	}{
		{"completed", true},
		{"running", true},
		{"pending", true},
		{"paused", true},
		{"failed", false},
		{"cancelled", false},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			uniqueHash := "test-hash-abc123-" + tt.status
			createTestExecution(t, store, "hash-job-"+tt.status, tt.status, withRequestHash(uniqueHash))

			result, err := store.FindRecentEligibleExecutionByRequestHash(uniqueHash, 3600, "")
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if tt.eligible && result == nil {
				t.Errorf("Expected eligible job for status %q, got nil", tt.status)
			}
			if !tt.eligible && result != nil {
				t.Errorf("Expected nil for status %q, got job %s", tt.status, result.ID)
			}
		})
	}
}

func TestGetEligibleExecutionByIdempotencyKey_UserScoping(t *testing.T) {
	store := newTestStore(t)

	createTestExecution(t, store, "user-scope-job", "completed",
		withIdempotencyKey("shared-key"), withUserID("user-A"))

	tests := []struct {
		name    string
		userID  string
		wantHit bool
	}{
		{"same user", "user-A", true},
		{"different user", "user-B", false},
		{"no user scope (system-wide)", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := store.GetEligibleExecutionByIdempotencyKey("shared-key", tt.userID)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if tt.wantHit && result == nil {
				t.Fatalf("Expected to find job for userID=%q", tt.userID)
			}
			if !tt.wantHit && result != nil {
				t.Errorf("Expected nil for userID=%q, got job %s", tt.userID, result.ID)
			}
		})
	}
}

func TestFindRecentEligibleByRequestHash_UserScoping(t *testing.T) {
	store := newTestStore(t)

	createTestExecution(t, store, "hash-scope-job", "completed",
		withRequestHash("scoped-hash"), withUserID("user-X"))

	tests := []struct {
		name    string
		userID  string
		wantHit bool
	}{
		{"same user", "user-X", true},
		{"different user", "user-Y", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := store.FindRecentEligibleExecutionByRequestHash("scoped-hash", 3600, tt.userID)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if tt.wantHit && result == nil {
				t.Fatalf("Expected to find job for userID=%q", tt.userID)
			}
			if !tt.wantHit && result != nil {
				t.Errorf("Expected nil for userID=%q, got job %s", tt.userID, result.ID)
			}
		})
	}
}

// TestCreateExecutionAtomic uses table-driven subtests to cover all four scenarios:
// new creation, eligible collision, collision preserving durable fields, and
// collision with a failed (ineligible) job.
func TestCreateExecutionAtomic(t *testing.T) {
	tests := []struct {
		name string
		// setup creates any pre-existing jobs before the atomic attempt
		setup func(t *testing.T, s *Storage)
		// job is the job passed to CreateExecutionAtomic
		job *Job
		// expectations
		wantCreated     bool
		wantExistingNil bool
		// verify runs additional assertions on the returned existing job (if non-nil)
		verify func(t *testing.T, existing *Job)
	}{
		{
			name:  "Success_NewJob",
			setup: nil,
			job: &Job{
				ID:             "atomic-new",
				Description:    "test",
				Model:          "workflow",
				Status:         "pending",
				IdempotencyKey: "atomic-key",
			},
			wantCreated:     true,
			wantExistingNil: true,
		},
		{
			name: "Collision_EligibleJob",
			setup: func(t *testing.T, s *Storage) {
				createTestExecution(t, s, "atomic-first", "completed", withIdempotencyKey("collision-key"))
			},
			job: &Job{
				ID:             "atomic-second",
				Description:    "test",
				Model:          "workflow",
				Status:         "pending",
				IdempotencyKey: "collision-key",
			},
			wantCreated:     false,
			wantExistingNil: false,
			verify: func(t *testing.T, existing *Job) {
				if existing.ID != "atomic-first" {
					t.Errorf("Expected existing job ID atomic-first, got %s", existing.ID)
				}
			},
		},
		{
			name: "Collision_PreservesDurableFields",
			setup: func(t *testing.T, s *Storage) {
				createTestExecution(t, s, "durable-original", "running",
					withIdempotencyKey("durable-collision-key"),
					withDurableFields(
						"durable-original", "durable-original",
						"abc123deadbeef", `{"nodes":[{"id":"n1"}]}`, 1,
					),
				)
			},
			job: &Job{
				ID:             "durable-duplicate",
				Description:    "test",
				Model:          "workflow",
				Status:         "pending",
				IdempotencyKey: "durable-collision-key",
			},
			wantCreated:     false,
			wantExistingNil: false,
			verify: func(t *testing.T, existing *Job) {
				if existing.DAGHash != "abc123deadbeef" {
					t.Errorf("DAGHash lost in collision path: got %q, want %q", existing.DAGHash, "abc123deadbeef")
				}
				if existing.DAGSnapshot != `{"nodes":[{"id":"n1"}]}` {
					t.Errorf("DAGSnapshot lost in collision path: got %q", existing.DAGSnapshot)
				}
				if existing.WorkflowExecutionID != "durable-original" {
					t.Errorf("WorkflowExecutionID lost: got %q, want %q", existing.WorkflowExecutionID, "durable-original")
				}
				if existing.RunID != "durable-original" {
					t.Errorf("RunID lost: got %q, want %q", existing.RunID, "durable-original")
				}
				if existing.RunNumber != 1 {
					t.Errorf("RunNumber lost: got %d, want 1", existing.RunNumber)
				}
			},
		},
		{
			name: "Collision_FailedJobNotEligible",
			setup: func(t *testing.T, s *Storage) {
				createTestExecution(t, s, "failed-key-job", "failed", withIdempotencyKey("failed-collision-key"))
			},
			job: &Job{
				ID:             "new-key-job",
				Description:    "test",
				Model:          "workflow",
				Status:         "pending",
				IdempotencyKey: "failed-collision-key",
			},
			wantCreated:     false,
			wantExistingNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestStore(t)

			if tt.setup != nil {
				tt.setup(t, s)
			}

			created, existing, err := s.CreateExecutionAtomic(tt.job)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if created != tt.wantCreated {
				t.Errorf("created = %v, want %v", created, tt.wantCreated)
			}
			if tt.wantExistingNil && existing != nil {
				t.Errorf("Expected existing=nil, got job %s", existing.ID)
			}
			if !tt.wantExistingNil && existing == nil {
				t.Fatal("Expected non-nil existing job")
			}
			if tt.verify != nil && existing != nil {
				tt.verify(t, existing)
			}
		})
	}
}

// --- Regression tests: execution query field fidelity ---
// These tests verify that every query function that returns WorkflowExecution
// correctly round-trips all 28 scanned columns. This is a safety net for the
// planned extraction of shared SELECT+Scan helpers.

func TestGetExecution_FieldFidelity(t *testing.T) {
	s := newTestStore(t)
	createTestExecution(t, s, "fid-get", "completed", withAllFields())

	got, err := s.GetExecution("fid-get")
	if err != nil {
		t.Fatalf("GetExecution: %v", err)
	}
	assertExecutionFields(t, "GetExecution", got, "fid-get")
	if got.Status != "completed" {
		t.Errorf("Status = %q, want %q", got.Status, "completed")
	}
}

func TestListExecutions_FieldFidelity(t *testing.T) {
	s := newTestStore(t)
	createTestExecution(t, s, "fid-list", "completed", withAllFields())

	results, err := s.ListExecutions(10)
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 execution, got %d", len(results))
	}
	assertExecutionFields(t, "ListExecutions", &results[0], "fid-list")
}

func TestListExecutionsByStatus_FieldFidelity(t *testing.T) {
	s := newTestStore(t)
	createTestExecution(t, s, "fid-status", "running", withAllFields())

	results, err := s.ListExecutionsByStatus("running", 10)
	if err != nil {
		t.Fatalf("ListExecutionsByStatus: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 execution, got %d", len(results))
	}
	assertExecutionFields(t, "ListExecutionsByStatus", &results[0], "fid-status")
	if results[0].Status != "running" {
		t.Errorf("Status = %q, want %q", results[0].Status, "running")
	}
}

func TestListExecutionsPaginated_FieldFidelity(t *testing.T) {
	s := newTestStore(t)
	createTestExecution(t, s, "fid-page", "pending", withAllFields())

	result, err := s.ListExecutionsPaginated("", 10, nil)
	if err != nil {
		t.Fatalf("ListExecutionsPaginated: %v", err)
	}
	if len(result.Executions) != 1 {
		t.Fatalf("expected 1 execution, got %d", len(result.Executions))
	}
	assertExecutionFields(t, "ListExecutionsPaginated", &result.Executions[0], "fid-page")
	if result.HasMore {
		t.Error("HasMore should be false for single result")
	}
}

func TestListExecutionsPaginated_Filters(t *testing.T) {
	s := newTestStore(t)
	createTestExecution(t, s, "page-a", "completed", withAllFields())
	time.Sleep(time.Millisecond) // distinct timestamps
	createTestExecution(t, s, "page-b", "running", func(j *Job) {
		withAllFields()(j)
		j.IdempotencyKey = "idem-page-b"
		j.RequestHash = "hash-page-b"
		j.ConfigHash = "cfg-match"
		j.WorkflowExecutionID = "page-b"
		j.RunID = "page-b"
		j.PreviousRunID = "prev-page-b"
		j.DAGHash = "daghash-page-b"
	})

	t.Run("FilterByStatus", func(t *testing.T) {
		result, err := s.ListExecutionsPaginated("", 10, &ExecutionFilters{Status: "running"})
		if err != nil {
			t.Fatalf("ListExecutionsPaginated with status filter: %v", err)
		}
		if len(result.Executions) != 1 {
			t.Fatalf("expected 1 execution, got %d", len(result.Executions))
		}
		if result.Executions[0].ID != "page-b" {
			t.Errorf("expected page-b, got %s", result.Executions[0].ID)
		}
	})

	t.Run("FilterByWorkflowID", func(t *testing.T) {
		result, err := s.ListExecutionsPaginated("", 10, &ExecutionFilters{WorkflowID: "wf-full"})
		if err != nil {
			t.Fatalf("ListExecutionsPaginated with workflow filter: %v", err)
		}
		if len(result.Executions) != 2 {
			t.Fatalf("expected 2 executions, got %d", len(result.Executions))
		}
	})

	t.Run("FilterByConfigHash", func(t *testing.T) {
		result, err := s.ListExecutionsPaginated("", 10, &ExecutionFilters{ConfigHash: "cfg-match"})
		if err != nil {
			t.Fatalf("ListExecutionsPaginated with config hash filter: %v", err)
		}
		if len(result.Executions) != 1 {
			t.Fatalf("expected 1 execution, got %d", len(result.Executions))
		}
		if result.Executions[0].ID != "page-b" {
			t.Errorf("expected page-b, got %s", result.Executions[0].ID)
		}
	})

	t.Run("Pagination", func(t *testing.T) {
		// Fetch 1 at a time
		page1, err := s.ListExecutionsPaginated("", 1, nil)
		if err != nil {
			t.Fatalf("page 1: %v", err)
		}
		if len(page1.Executions) != 1 {
			t.Fatalf("page 1: expected 1, got %d", len(page1.Executions))
		}
		if !page1.HasMore {
			t.Error("page 1: HasMore should be true")
		}
		if page1.NextCursor == "" {
			t.Fatal("page 1: expected non-empty cursor")
		}

		page2, err := s.ListExecutionsPaginated(page1.NextCursor, 1, nil)
		if err != nil {
			t.Fatalf("page 2: %v", err)
		}
		if len(page2.Executions) != 1 {
			t.Fatalf("page 2: expected 1, got %d", len(page2.Executions))
		}
		if page2.HasMore {
			t.Error("page 2: HasMore should be false")
		}

		// Ensure different IDs on each page
		if page1.Executions[0].ID == page2.Executions[0].ID {
			t.Error("pages returned same execution")
		}
	})
}

func TestGetChildExecutions_FieldFidelity(t *testing.T) {
	s := newTestStore(t)

	// Create parent
	createTestExecution(t, s, "parent-fid", "completed", withAllFields())

	// Create two children with parent_execution_id set
	for i, childID := range []string{"child-fid-1", "child-fid-2"} {
		createTestExecution(t, s, childID, "completed", func(j *Job) {
			withAllFields()(j)
			j.ParentExecutionID = "parent-fid"
			j.IdempotencyKey = fmt.Sprintf("idem-%s", childID)
			j.RequestHash = fmt.Sprintf("hash-%s", childID)
			j.ConfigHash = fmt.Sprintf("cfg-%s", childID)
			j.WorkflowExecutionID = childID
			j.RunID = childID
			j.PreviousRunID = fmt.Sprintf("prev-%s", childID)
			j.DAGHash = fmt.Sprintf("daghash-%s", childID)
		})
		_ = i
		time.Sleep(time.Millisecond) // ensure distinct created_at for ASC ordering
	}

	children, err := s.GetChildExecutions("parent-fid")
	if err != nil {
		t.Fatalf("GetChildExecutions: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(children))
	}

	// Verify ordering is ASC by created_at
	if children[0].ID != "child-fid-1" {
		t.Errorf("expected child-fid-1 first, got %s", children[0].ID)
	}
	if children[1].ID != "child-fid-2" {
		t.Errorf("expected child-fid-2 second, got %s", children[1].ID)
	}

	// Verify full field fidelity on first child
	c := children[0]
	if c.ParentExecutionID != "parent-fid" {
		t.Errorf("ParentExecutionID = %q, want %q", c.ParentExecutionID, "parent-fid")
	}
	if c.Description != "full-field test query" {
		t.Errorf("Description = %q", c.Description)
	}
	if c.Model != "test-model-v2" {
		t.Errorf("Model = %q", c.Model)
	}
	if c.WorkflowID != "wf-full" {
		t.Errorf("WorkflowID = %q", c.WorkflowID)
	}
	if c.DAGSnapshot != `{"nodes":[{"id":"n1"}]}` {
		t.Errorf("DAGSnapshot = %q", c.DAGSnapshot)
	}
	if c.RunNumber != 3 {
		t.Errorf("RunNumber = %d, want 3", c.RunNumber)
	}

	// No children for non-existent parent
	none, err := s.GetChildExecutions("non-existent-parent")
	if err != nil {
		t.Fatalf("GetChildExecutions(non-existent): %v", err)
	}
	if len(none) != 0 {
		t.Errorf("expected 0 children for non-existent parent, got %d", len(none))
	}
}

func TestGetEligibleExecutionByIdempotencyKey_FieldFidelity(t *testing.T) {
	s := newTestStore(t)
	createTestExecution(t, s, "fid-idem", "completed", withAllFields())

	got, err := s.GetEligibleExecutionByIdempotencyKey("idem-fid-idem", "user-full")
	if err != nil {
		t.Fatalf("GetEligibleExecutionByIdempotencyKey: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	assertExecutionFields(t, "GetEligibleByIdempotencyKey", got, "fid-idem")
}

func TestFindRecentEligibleByRequestHash_FieldFidelity(t *testing.T) {
	s := newTestStore(t)
	createTestExecution(t, s, "fid-hash", "completed", withAllFields())

	got, err := s.FindRecentEligibleExecutionByRequestHash("hash-fid-hash", 3600, "user-full")
	if err != nil {
		t.Fatalf("FindRecentEligibleExecutionByRequestHash: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	assertExecutionFields(t, "FindRecentByRequestHash", got, "fid-hash")
}

func TestReconcileRunningJobs(t *testing.T) {
	s := newTestStore(t)

	// Legacy running job (no durable fields) — should be failed
	createJobWithStatus(t, s, "legacy-running", "running")

	// Durable running job — should be left as-is
	createTestExecution(t, s, "durable-running", "running",
		withDurableFields("durable-running", "durable-running", "abc123", `{"nodes":[]}`, 1),
	)

	// Non-running jobs — should be untouched
	createJobWithStatus(t, s, "completed-job", "completed")
	createJobWithStatus(t, s, "pending-job", "pending")

	legacyFailed, durableCount, err := s.ReconcileRunningJobs()
	if err != nil {
		t.Fatalf("ReconcileRunningJobs: %v", err)
	}
	if legacyFailed != 1 {
		t.Errorf("legacyFailed = %d, want 1", legacyFailed)
	}
	if durableCount != 1 {
		t.Errorf("durableCount = %d, want 1", durableCount)
	}

	// Verify legacy job was failed
	legacy, err := s.GetExecution("legacy-running")
	if err != nil {
		t.Fatalf("GetExecution(legacy): %v", err)
	}
	if legacy.Status != "failed" {
		t.Errorf("legacy status = %q, want %q", legacy.Status, "failed")
	}
	if legacy.ErrorMessage == "" {
		t.Error("legacy error message should be set")
	}

	// Verify durable job still running
	durable, err := s.GetExecution("durable-running")
	if err != nil {
		t.Fatalf("GetExecution(durable): %v", err)
	}
	if durable.Status != "running" {
		t.Errorf("durable status = %q, want %q", durable.Status, "running")
	}

	// Verify others untouched
	comp, _ := s.GetExecution("completed-job")
	if comp.Status != "completed" {
		t.Errorf("completed job status changed to %q", comp.Status)
	}
	pend, _ := s.GetExecution("pending-job")
	if pend.Status != "pending" {
		t.Errorf("pending job status changed to %q", pend.Status)
	}
}

func TestGetExecutionChain_FieldFidelity(t *testing.T) {
	s := newTestStore(t)

	// Create root → child1 → grandchild
	createTestExecution(t, s, "root-chain", "completed", withAllFields())
	time.Sleep(time.Millisecond)

	createTestExecution(t, s, "child-chain", "completed", func(j *Job) {
		withAllFields()(j)
		j.ParentExecutionID = "root-chain"
		j.IdempotencyKey = "idem-child-chain"
		j.RequestHash = "hash-child-chain"
		j.ConfigHash = "cfg-child-chain"
		j.WorkflowExecutionID = "child-chain"
		j.RunID = "child-chain"
		j.PreviousRunID = "prev-child-chain"
		j.DAGHash = "daghash-child-chain"
	})
	time.Sleep(time.Millisecond)

	createTestExecution(t, s, "grandchild-chain", "completed", func(j *Job) {
		withAllFields()(j)
		j.ParentExecutionID = "child-chain"
		j.IdempotencyKey = "idem-grandchild-chain"
		j.RequestHash = "hash-grandchild-chain"
		j.ConfigHash = "cfg-grandchild-chain"
		j.WorkflowExecutionID = "grandchild-chain"
		j.RunID = "grandchild-chain"
		j.PreviousRunID = "prev-grandchild-chain"
		j.DAGHash = "daghash-grandchild-chain"
	})

	chain, err := s.GetExecutionChain("root-chain")
	if err != nil {
		t.Fatalf("GetExecutionChain: %v", err)
	}
	if len(chain) != 3 {
		t.Fatalf("expected 3 executions in chain, got %d", len(chain))
	}
	if chain[0].ID != "root-chain" {
		t.Errorf("chain[0] = %q, want root-chain", chain[0].ID)
	}
	if chain[1].ID != "child-chain" {
		t.Errorf("chain[1] = %q, want child-chain", chain[1].ID)
	}
	if chain[2].ID != "grandchild-chain" {
		t.Errorf("chain[2] = %q, want grandchild-chain", chain[2].ID)
	}

	// Field fidelity on root
	assertExecutionFields(t, "chain-root", &chain[0], "root-chain")
}
