package storage

import (
	"context"
	"testing"
	"time"
)

func setupAgentStoreTest(t *testing.T) *Storage {
	t.Helper()
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func createAgentTestJob(t *testing.T, store *Storage, jobID string) {
	t.Helper()
	err := store.CreateExecution(&Job{
		ID:                  jobID,
		Description:         "agent test job",
		Model:               "workflow",
		Status:              "running",
		WorkflowID:          "wf-agent-test",
		WorkflowExecutionID: jobID,
		RunID:               jobID,
		RunNumber:           1,
		DAGSnapshot:         `{"id":"wf-agent-test","nodes":[{"id":"n1"}],"edges":[]}`,
		DAGHash:             "agent-test-hash",
	})
	if err != nil {
		t.Fatalf("failed to create test job: %v", err)
	}
}

func TestNodeExecutionAttemptLifecycle(t *testing.T) {
	store := setupAgentStoreTest(t)
	jobID := "node-exec-job"
	createAgentTestJob(t, store, jobID)
	ctx := context.Background()

	started := time.Now().UTC()
	if err := store.UpsertNodeExecutionAttempt(ctx, &NodeExecutionAttempt{
		JobID:       jobID,
		ExecutionID: jobID,
		RunID:       jobID,
		NodeID:      "n1",
		NodeType:    "prompt",
		Attempt:     1,
		Status:      "running",
		ActivityID:  "act-1",
		StartedAt:   &started,
	}); err != nil {
		t.Fatalf("failed to insert node execution start: %v", err)
	}

	completed := time.Now().UTC()
	if err := store.UpsertNodeExecutionAttempt(ctx, &NodeExecutionAttempt{
		JobID:        jobID,
		ExecutionID:  jobID,
		RunID:        jobID,
		NodeID:       "n1",
		NodeType:     "prompt",
		Attempt:      1,
		Status:       "completed",
		ActivityID:   "act-1",
		CompletedAt:  &completed,
		LatencyMs:    123.4,
		TokensInput:  10,
		TokensOutput: 15,
		Cost:         0.02,
	}); err != nil {
		t.Fatalf("failed to upsert node execution completion: %v", err)
	}

	rows, err := store.ListNodeExecutionAttemptsByJob(ctx, jobID)
	if err != nil {
		t.Fatalf("failed to list node executions: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 node execution row, got %d", len(rows))
	}
	row := rows[0]
	if row.Status != "completed" {
		t.Errorf("expected status completed, got %s", row.Status)
	}
	if row.TokensInput != 10 || row.TokensOutput != 15 {
		t.Errorf("unexpected token metrics: in=%d out=%d", row.TokensInput, row.TokensOutput)
	}
	if row.Cost != 0.02 {
		t.Errorf("unexpected cost: %f", row.Cost)
	}
}

func TestNodeExecutionAttemptRowsAreRunScoped(t *testing.T) {
	store := setupAgentStoreTest(t)
	jobID := "node-run-scope-job"
	createAgentTestJob(t, store, jobID)
	ctx := context.Background()

	if err := store.UpsertNodeExecutionAttempt(ctx, &NodeExecutionAttempt{
		JobID:       jobID,
		ExecutionID: "exec-1",
		RunID:       "run-1",
		NodeID:      "n1",
		NodeType:    "prompt",
		Attempt:     1,
		Status:      "completed",
		ActivityID:  "act-1",
	}); err != nil {
		t.Fatalf("failed to insert run-1 node execution: %v", err)
	}

	if err := store.UpsertNodeExecutionAttempt(ctx, &NodeExecutionAttempt{
		JobID:       jobID,
		ExecutionID: "exec-1",
		RunID:       "run-2",
		NodeID:      "n1",
		NodeType:    "prompt",
		Attempt:     1,
		Status:      "failed",
		ActivityID:  "act-2",
	}); err != nil {
		t.Fatalf("failed to insert run-2 node execution: %v", err)
	}

	rows, err := store.ListNodeExecutionAttemptsByJob(ctx, jobID)
	if err != nil {
		t.Fatalf("failed to list node executions: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 run-scoped rows, got %d", len(rows))
	}

	statusByRun := map[string]string{}
	for _, row := range rows {
		statusByRun[row.RunID] = row.Status
	}
	if statusByRun["run-1"] != "completed" {
		t.Fatalf("expected run-1 status completed, got %q", statusByRun["run-1"])
	}
	if statusByRun["run-2"] != "failed" {
		t.Fatalf("expected run-2 status failed, got %q", statusByRun["run-2"])
	}
}

func TestMarkRunningNodeAttemptsCancelled(t *testing.T) {
	store := setupAgentStoreTest(t)
	jobID := "node-cancel-job"
	createAgentTestJob(t, store, jobID)
	ctx := context.Background()

	mustUpsert := func(node *NodeExecutionAttempt) {
		t.Helper()
		if err := store.UpsertNodeExecutionAttempt(ctx, node); err != nil {
			t.Fatalf("failed to upsert node execution: %v", err)
		}
	}

	mustUpsert(&NodeExecutionAttempt{
		JobID:       jobID,
		ExecutionID: "exec-1",
		RunID:       "run-1",
		NodeID:      "n1",
		NodeType:    "prompt",
		Attempt:     1,
		Status:      "running",
	})
	mustUpsert(&NodeExecutionAttempt{
		JobID:       jobID,
		ExecutionID: "exec-1",
		RunID:       "run-1",
		NodeID:      "n2",
		NodeType:    "prompt",
		Attempt:     1,
		Status:      "completed",
	})
	mustUpsert(&NodeExecutionAttempt{
		JobID:       jobID,
		ExecutionID: "exec-1",
		RunID:       "run-2",
		NodeID:      "n1",
		NodeType:    "prompt",
		Attempt:     1,
		Status:      "running",
	})

	count, err := store.MarkRunningNodeExecutionAttemptsCancelled(ctx, jobID, "run-1")
	if err != nil {
		t.Fatalf("failed to cancel running rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 cancelled running row, got %d", count)
	}

	rows, err := store.ListNodeExecutionAttemptsByJob(ctx, jobID)
	if err != nil {
		t.Fatalf("failed to list node executions: %v", err)
	}

	type key struct {
		RunID  string
		NodeID string
	}
	byKey := map[key]NodeExecutionAttempt{}
	for _, row := range rows {
		byKey[key{RunID: row.RunID, NodeID: row.NodeID}] = row
	}

	row := byKey[key{RunID: "run-1", NodeID: "n1"}]
	if row.Status != "cancelled" {
		t.Fatalf("expected run-1/n1 to be cancelled, got %q", row.Status)
	}
	if row.CompletedAt == nil {
		t.Fatal("expected run-1/n1 completed_at to be set on cancellation")
	}

	if byKey[key{RunID: "run-1", NodeID: "n2"}].Status != "completed" {
		t.Fatal("expected run-1/n2 status to remain completed")
	}
	if byKey[key{RunID: "run-2", NodeID: "n1"}].Status != "running" {
		t.Fatal("expected run-2/n1 status to remain running")
	}
}

func TestMarkRunningNodeAttemptsInterrupted(t *testing.T) {
	store := setupAgentStoreTest(t)
	jobID := "node-interrupt-job"
	createAgentTestJob(t, store, jobID)
	ctx := context.Background()

	mustUpsert := func(node *NodeExecutionAttempt) {
		t.Helper()
		if err := store.UpsertNodeExecutionAttempt(ctx, node); err != nil {
			t.Fatalf("failed to upsert node execution: %v", err)
		}
	}

	mustUpsert(&NodeExecutionAttempt{
		JobID:       jobID,
		ExecutionID: "exec-1",
		RunID:       "run-1",
		NodeID:      "n1",
		NodeType:    "prompt",
		Attempt:     1,
		Status:      "running",
	})
	mustUpsert(&NodeExecutionAttempt{
		JobID:       jobID,
		ExecutionID: "exec-1",
		RunID:       "run-1",
		NodeID:      "n2",
		NodeType:    "prompt",
		Attempt:     1,
		Status:      "completed",
	})
	mustUpsert(&NodeExecutionAttempt{
		JobID:       jobID,
		ExecutionID: "exec-1",
		RunID:       "run-2",
		NodeID:      "n1",
		NodeType:    "prompt",
		Attempt:     1,
		Status:      "running",
	})

	count, err := store.MarkRunningNodeExecutionAttemptsInterrupted(ctx, jobID, "run-1")
	if err != nil {
		t.Fatalf("failed to interrupt running rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 interrupted running row, got %d", count)
	}

	rows, err := store.ListNodeExecutionAttemptsByJob(ctx, jobID)
	if err != nil {
		t.Fatalf("failed to list node executions: %v", err)
	}

	type key struct {
		RunID  string
		NodeID string
	}
	byKey := map[key]NodeExecutionAttempt{}
	for _, row := range rows {
		byKey[key{RunID: row.RunID, NodeID: row.NodeID}] = row
	}

	row := byKey[key{RunID: "run-1", NodeID: "n1"}]
	if row.Status != "interrupted" {
		t.Fatalf("expected run-1/n1 to be interrupted, got %q", row.Status)
	}
	if row.CompletedAt == nil {
		t.Fatal("expected run-1/n1 completed_at to be set on interruption")
	}

	if byKey[key{RunID: "run-1", NodeID: "n2"}].Status != "completed" {
		t.Fatal("expected run-1/n2 status to remain completed")
	}
	if byKey[key{RunID: "run-2", NodeID: "n1"}].Status != "running" {
		t.Fatal("expected run-2/n1 status to remain running")
	}
}

func TestListAgentRunsByJob(t *testing.T) {
	store := setupAgentStoreTest(t)
	ctx := context.Background()

	jobID := "agent-runs-job"
	createAgentTestJob(t, store, jobID)

	otherJobID := "agent-runs-other-job"
	createAgentTestJob(t, store, otherJobID)

	if err := store.UpsertAgentRun(ctx, &AgentRun{
		ID:            "run-1",
		JobID:         jobID,
		ExecutionID:   jobID,
		RunID:         jobID,
		NodeID:        "node-a",
		Attempt:       1,
		ExternalRunID: "novomo-1",
		Harness:       "claude-code",
		Status:        "running",
	}); err != nil {
		t.Fatalf("failed to upsert first agent run: %v", err)
	}
	finished := time.Now().UTC()
	if err := store.UpsertAgentRun(ctx, &AgentRun{
		ID:            "run-2",
		JobID:         jobID,
		ExecutionID:   jobID,
		RunID:         jobID,
		NodeID:        "node-b",
		Attempt:       1,
		ExternalRunID: "novomo-2",
		Harness:       "claude-code",
		Status:        "completed",
		Output:        "done",
		TokensInput:   12,
		TokensOutput:  5,
		CostUSD:       0.17,
		FinishedAt:    &finished,
	}); err != nil {
		t.Fatalf("failed to upsert second agent run: %v", err)
	}
	if err := store.UpsertAgentRun(ctx, &AgentRun{
		ID:            "run-other",
		JobID:         otherJobID,
		ExecutionID:   otherJobID,
		RunID:         otherJobID,
		NodeID:        "node-x",
		Attempt:       1,
		ExternalRunID: "novomo-other",
		Harness:       "claude-code",
		Status:        "running",
	}); err != nil {
		t.Fatalf("failed to upsert other-job run: %v", err)
	}

	runs, err := store.ListAgentRunsByJob(ctx, jobID)
	if err != nil {
		t.Fatalf("ListAgentRunsByJob failed: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 agent runs for job %s, got %d", jobID, len(runs))
	}

	byID := map[string]AgentRun{}
	for _, run := range runs {
		byID[run.ID] = run
	}
	if byID["run-1"].NodeID != "node-a" {
		t.Fatalf("expected run-1 node-a, got %q", byID["run-1"].NodeID)
	}
	if byID["run-2"].Status != "completed" {
		t.Fatalf("expected run-2 status completed, got %q", byID["run-2"].Status)
	}
	if byID["run-2"].ExternalRunID != "novomo-2" || byID["run-2"].Harness != "claude-code" {
		t.Fatalf("expected Novomo fields, got %+v", byID["run-2"])
	}
	if byID["run-2"].RunKind != "agent_run" {
		t.Fatalf("expected legacy agent_run kind, got %+v", byID["run-2"])
	}
	if byID["run-2"].Output != "done" || byID["run-2"].TokensInput != 12 || byID["run-2"].TokensOutput != 5 || byID["run-2"].CostUSD != 0.17 {
		t.Fatalf("expected usage/output fields, got %+v", byID["run-2"])
	}
}

func TestUpsertAgentRunAllowsNovoRunWithoutHarness(t *testing.T) {
	store := setupAgentStoreTest(t)
	ctx := context.Background()
	jobID := "novo-run-no-harness-job"
	createAgentTestJob(t, store, jobID)

	if err := store.UpsertAgentRun(ctx, &AgentRun{
		ID:             "novo-row-1",
		JobID:          jobID,
		ExecutionID:    jobID,
		RunID:          jobID,
		NodeID:         "superagent-node",
		Attempt:        1,
		RunKind:        "novo_run",
		ExternalRunID:  "nr-1",
		ExternalTaskID: "task-1",
		Status:         "running",
	}); err != nil {
		t.Fatalf("failed to upsert novo_run without harness: %v", err)
	}

	rows, err := store.ListAgentRunsByJob(ctx, jobID)
	if err != nil {
		t.Fatalf("ListAgentRunsByJob failed: %v", err)
	}
	if len(rows) != 1 || rows[0].RunKind != "novo_run" || rows[0].Harness != "" {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestUpsertAgentRunRejectsAgentRunWithoutHarness(t *testing.T) {
	store := setupAgentStoreTest(t)
	ctx := context.Background()
	jobID := "agent-run-no-harness-job"
	createAgentTestJob(t, store, jobID)

	err := store.UpsertAgentRun(ctx, &AgentRun{
		ID:            "agent-row-1",
		JobID:         jobID,
		ExecutionID:   jobID,
		RunID:         jobID,
		NodeID:        "agent-node",
		Attempt:       1,
		RunKind:       "agent_run",
		ExternalRunID: "run-1",
		Status:        "running",
	})
	if err == nil {
		t.Fatal("expected agent_run without harness to be rejected")
	}
}

func TestUpdateAgentRunIfNonTerminalPreservesTerminalRows(t *testing.T) {
	store := setupAgentStoreTest(t)
	ctx := context.Background()
	jobID := "agent-run-conditional-update-job"
	createAgentTestJob(t, store, jobID)

	started := time.Now().UTC()
	finished := started.Add(time.Second)
	if err := store.UpsertAgentRun(ctx, &AgentRun{
		ID:            "agent-row-1",
		JobID:         jobID,
		ExecutionID:   jobID,
		RunID:         jobID,
		NodeID:        "agent-node",
		Attempt:       1,
		RunKind:       "agent_run",
		ExternalRunID: "novomo-1",
		Harness:       "claude-code",
		Status:        "cancelled",
		ErrorCode:     "CANCELLED",
		ErrorMessage:  "Stop requested",
		StartedAt:     &started,
		FinishedAt:    &finished,
	}); err != nil {
		t.Fatalf("failed to insert terminal agent run: %v", err)
	}

	persisted, err := store.UpdateAgentRunIfNonTerminal(ctx, &AgentRun{
		ID:               "agent-row-1",
		JobID:            jobID,
		ExecutionID:      jobID,
		RunID:            jobID,
		NodeID:           "agent-node",
		Attempt:          1,
		RunKind:          "agent_run",
		ExternalRunID:    "novomo-1",
		ExternalJobRunID: "job-run-1",
		Harness:          "claude-code",
		Status:           "running",
		StartedAt:        &started,
	})
	if err != nil {
		t.Fatalf("UpdateAgentRunIfNonTerminal failed: %v", err)
	}
	if persisted {
		t.Fatal("expected terminal row update to be skipped")
	}

	got, err := store.GetAgentRunByID(ctx, jobID, "agent-row-1")
	if err != nil {
		t.Fatalf("GetAgentRunByID failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected agent run row")
	}
	if got.Status != "cancelled" || got.ErrorCode != "CANCELLED" || got.ErrorMessage != "Stop requested" {
		t.Fatalf("terminal fields were overwritten: %+v", got)
	}
	if got.ExternalJobRunID != "" {
		t.Fatalf("expected skipped update to preserve empty job run id, got %q", got.ExternalJobRunID)
	}
}

func TestUpdateAgentRunIfNonTerminalUpdatesActiveRows(t *testing.T) {
	store := setupAgentStoreTest(t)
	ctx := context.Background()
	jobID := "agent-run-active-update-job"
	createAgentTestJob(t, store, jobID)

	started := time.Now().UTC()
	if err := store.UpsertAgentRun(ctx, &AgentRun{
		ID:            "agent-row-1",
		JobID:         jobID,
		ExecutionID:   jobID,
		RunID:         jobID,
		NodeID:        "agent-node",
		Attempt:       1,
		RunKind:       "agent_run",
		ExternalRunID: "novomo-1",
		Harness:       "claude-code",
		Status:        "pending",
	}); err != nil {
		t.Fatalf("failed to insert active agent run: %v", err)
	}

	persisted, err := store.UpdateAgentRunIfNonTerminal(ctx, &AgentRun{
		ID:               "agent-row-1",
		JobID:            jobID,
		ExecutionID:      jobID,
		RunID:            jobID,
		NodeID:           "agent-node",
		Attempt:          1,
		RunKind:          "agent_run",
		ExternalRunID:    "novomo-1",
		ExternalJobRunID: "job-run-1",
		Harness:          "claude-code",
		Status:           "running",
		StartedAt:        &started,
	})
	if err != nil {
		t.Fatalf("UpdateAgentRunIfNonTerminal failed: %v", err)
	}
	if !persisted {
		t.Fatal("expected active row update to persist")
	}

	got, err := store.GetAgentRunByID(ctx, jobID, "agent-row-1")
	if err != nil {
		t.Fatalf("GetAgentRunByID failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected agent run row")
	}
	if got.Status != "running" || got.ExternalJobRunID != "job-run-1" || got.StartedAt == nil {
		t.Fatalf("active row was not updated: %+v", got)
	}
}

func TestGetAgentRunByExecutionAttempt(t *testing.T) {
	store := setupAgentStoreTest(t)
	ctx := context.Background()
	jobID := "agent-run-lookup-job"
	createAgentTestJob(t, store, jobID)

	if err := store.UpsertAgentRun(ctx, &AgentRun{
		ID:            "agent-row-1",
		JobID:         jobID,
		ExecutionID:   jobID,
		RunID:         "run-1",
		NodeID:        "agent-node",
		Attempt:       2,
		ExternalRunID: "novomo-lookup",
		Harness:       "claude-code",
		Status:        "running",
	}); err != nil {
		t.Fatalf("failed to upsert agent run: %v", err)
	}

	got, err := store.GetAgentRunByExecutionAttempt(ctx, jobID, "run-1", "agent-node", 2)
	if err != nil {
		t.Fatalf("lookup failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected row")
	}
	if got.ID != "agent-row-1" || got.ExternalRunID != "novomo-lookup" {
		t.Fatalf("unexpected lookup row: %+v", got)
	}

	missing, err := store.GetAgentRunByExecutionAttempt(ctx, jobID, "run-1", "agent-node", 3)
	if err != nil {
		t.Fatalf("missing lookup failed: %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil missing row, got %+v", missing)
	}
}

func TestAgentRunPersistsNovoRunKindAndTaskID(t *testing.T) {
	store := setupAgentStoreTest(t)
	ctx := context.Background()
	jobID := "novo-run-job"
	createAgentTestJob(t, store, jobID)

	if err := store.UpsertAgentRun(ctx, &AgentRun{
		ID:             "novo-row-1",
		JobID:          jobID,
		ExecutionID:    jobID,
		RunID:          "run-1",
		NodeID:         "superagent-node",
		Attempt:        1,
		RunKind:        "novo_run",
		ExternalRunID:  "nr-lookup",
		ExternalTaskID: "task-lookup",
		Harness:        "claude-code",
		Status:         "running",
	}); err != nil {
		t.Fatalf("failed to upsert novo run row: %v", err)
	}

	got, err := store.GetAgentRunByExecutionAttempt(ctx, jobID, "run-1", "superagent-node", 1)
	if err != nil {
		t.Fatalf("lookup failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected row")
	}
	if got.RunKind != "novo_run" || got.ExternalRunID != "nr-lookup" || got.ExternalTaskID != "task-lookup" {
		t.Fatalf("unexpected novo row: %+v", got)
	}
}

func TestAgentRunPersistsHandoffFieldsAndLatestNodeLookup(t *testing.T) {
	store := setupAgentStoreTest(t)
	ctx := context.Background()
	jobID := "agent-run-handoff-job"
	createAgentTestJob(t, store, jobID)

	if err := store.UpsertAgentRun(ctx, &AgentRun{
		ID:               "agent-row-1",
		JobID:            jobID,
		ExecutionID:      jobID,
		RunID:            "run-1",
		NodeID:           "agent-node",
		Attempt:          1,
		RunKind:          "agent_run",
		ExternalRunID:    "job-older",
		ExternalJobRunID: "jobrun-older",
		InheritFromJSON:  `{"kind":"novo_run","id":"nr-parent"}`,
		Harness:          "claude-code",
		Status:           "completed",
	}); err != nil {
		t.Fatalf("failed to upsert first agent run: %v", err)
	}
	if err := store.UpsertAgentRun(ctx, &AgentRun{
		ID:               "agent-row-2",
		JobID:            jobID,
		ExecutionID:      jobID,
		RunID:            "run-1",
		NodeID:           "agent-node",
		Attempt:          2,
		RunKind:          "agent_run",
		ExternalRunID:    "job-latest",
		ExternalJobRunID: "jobrun-latest",
		InheritFromJSON:  `{"kind":"job_run","id":"jobrun-upstream","policy":"latest"}`,
		Harness:          "claude-code",
		Status:           "running",
	}); err != nil {
		t.Fatalf("failed to upsert second agent run: %v", err)
	}

	got, err := store.GetLatestAgentRunByNode(ctx, jobID, "run-1", "agent-node")
	if err != nil {
		t.Fatalf("latest lookup failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected latest row")
	}
	if got.ID != "agent-row-2" || got.ExternalJobRunID != "jobrun-latest" {
		t.Fatalf("unexpected latest row: %+v", got)
	}
	if got.InheritFromJSON != `{"kind":"job_run","id":"jobrun-upstream","policy":"latest"}` {
		t.Fatalf("inherit_from_json was not persisted: %+v", got)
	}

	missing, err := store.GetLatestAgentRunByNode(ctx, jobID, "run-1", "missing")
	if err != nil {
		t.Fatalf("missing lookup failed: %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil for missing node, got %+v", missing)
	}
}

func TestAgentRunSchemaDropsLegacyDetailTables(t *testing.T) {
	store := setupAgentStoreTest(t)
	ctx := context.Background()

	for _, table := range []string{"agent_events", "agent_tool_calls"} {
		var count int
		err := store.db.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM sqlite_master
			WHERE type = 'table' AND name = ?
		`, table).Scan(&count)
		if err != nil {
			t.Fatalf("query sqlite_master: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected legacy table %s to be absent", table)
		}
	}
}
