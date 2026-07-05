package storage

import (
	"context"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/events"
)

func TestEventStore_GetLatestSequenceAndCount(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	jobID := "event-seq-job"
	createTestJob(t, store, jobID)

	if _, err := store.AppendEvent(ctx, jobID, events.EventStatus, map[string]interface{}{"message": "queued"}); err != nil {
		t.Fatalf("append event 1 failed: %v", err)
	}
	if _, err := store.AppendEventWithDetails(ctx, jobID, events.EventNodeStart, "n1", "start", "", "", map[string]interface{}{
		"node_id": "n1",
	}); err != nil {
		t.Fatalf("append event 2 failed: %v", err)
	}
	if _, err := store.AppendEventWithDetails(ctx, jobID, events.EventNodeComplete, "n1", "done", "", "", map[string]interface{}{
		"node_id": "n1",
		"output":  "ok",
	}); err != nil {
		t.Fatalf("append event 3 failed: %v", err)
	}

	seq, err := store.GetLatestSequence(ctx, jobID)
	if err != nil {
		t.Fatalf("GetLatestSequence failed: %v", err)
	}
	if seq != 3 {
		t.Fatalf("expected latest sequence 3, got %d", seq)
	}

	count, err := store.GetEventCount(ctx, jobID)
	if err != nil {
		t.Fatalf("GetEventCount failed: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected event count 3, got %d", count)
	}
}

func TestEventStore_GetJobSnapshotIncludesNodesAndResult(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	jobID := "event-snapshot-job"
	createTestJob(t, store, jobID)

	if _, err := store.AppendEvent(ctx, jobID, events.EventStatus, map[string]interface{}{"message": "queued"}); err != nil {
		t.Fatalf("append event failed: %v", err)
	}

	if err := store.UpdateExecutionResult(jobID, "final output", 0.2, 12, 8, 20); err != nil {
		t.Fatalf("UpdateExecutionResult failed: %v", err)
	}
	if err := store.UpdateExecutionStatus(jobID, events.JobStatusCompleted); err != nil {
		t.Fatalf("UpdateExecutionStatus failed: %v", err)
	}

	if err := store.AddWorkflowNode(&WorkflowNode{
		ExecutionID:  jobID,
		NodeID:       "n1",
		NodeType:     "prompt",
		NodeOrder:    1,
		Status:       "completed",
		Output:       "node output",
		TokensInput:  5,
		TokensOutput: 7,
	}); err != nil {
		t.Fatalf("AddWorkflowNode failed: %v", err)
	}

	snapshot, err := store.GetJobSnapshot(ctx, jobID)
	if err != nil {
		t.Fatalf("GetJobSnapshot failed: %v", err)
	}

	if snapshot.Job == nil || snapshot.Job.ID != jobID {
		t.Fatalf("expected snapshot job %s, got %+v", jobID, snapshot.Job)
	}
	if snapshot.SnapshotSequence != 1 {
		t.Fatalf("expected snapshot sequence 1, got %d", snapshot.SnapshotSequence)
	}
	if snapshot.FinalOutput != "final output" {
		t.Fatalf("expected final output 'final output', got %q", snapshot.FinalOutput)
	}
	if len(snapshot.Nodes) != 1 || snapshot.Nodes[0].NodeID != "n1" {
		t.Fatalf("expected one node n1, got %+v", snapshot.Nodes)
	}
}

func TestEventStore_OutboxLifecycle(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	jobID := "event-outbox-job"
	createTestJob(t, store, jobID)

	if err := store.CreateOutboxEntry(ctx, &OutboxEntry{
		ToolCallID:  "tool-1",
		JobID:       jobID,
		NodeID:      "n1",
		ToolType:    "webhook",
		Payload:     map[string]interface{}{"url": "https://example.com"},
		MaxAttempts: 3,
	}); err != nil {
		t.Fatalf("CreateOutboxEntry failed: %v", err)
	}

	pending, err := store.GetPendingOutboxEntries(ctx, 10)
	if err != nil {
		t.Fatalf("GetPendingOutboxEntries failed: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending entry, got %d", len(pending))
	}
	entry := pending[0]
	if entry.ToolCallID != "tool-1" || entry.JobID != jobID {
		t.Fatalf("unexpected outbox entry %+v", entry)
	}

	nextAttempt := time.Now().Add(2 * time.Hour)
	if err := store.UpdateOutboxStatus(ctx, entry.ID, "pending", "retry later", &nextAttempt); err != nil {
		t.Fatalf("UpdateOutboxStatus(with nextAttempt) failed: %v", err)
	}
	pending, err = store.GetPendingOutboxEntries(ctx, 10)
	if err != nil {
		t.Fatalf("GetPendingOutboxEntries after deferral failed: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected 0 ready entries after deferral, got %d", len(pending))
	}

	if err := store.IncrementOutboxAttempts(ctx, entry.ID, time.Now().Add(-time.Minute), "transient"); err != nil {
		t.Fatalf("IncrementOutboxAttempts failed: %v", err)
	}

	pending, err = store.GetPendingOutboxEntries(ctx, 10)
	if err != nil {
		t.Fatalf("GetPendingOutboxEntries after increment failed: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 ready entry after increment, got %d", len(pending))
	}
	if pending[0].Attempts != 1 {
		t.Fatalf("expected attempts=1, got %d", pending[0].Attempts)
	}
	if pending[0].LastError != "transient" {
		t.Fatalf("expected last_error transient, got %q", pending[0].LastError)
	}

	if err := store.UpdateOutboxStatus(ctx, entry.ID, "completed", "", nil); err != nil {
		t.Fatalf("UpdateOutboxStatus(completed) failed: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `
		UPDATE side_effect_outbox SET updated_at = ? WHERE id = ?
	`, time.Now().Add(-3*time.Hour), entry.ID); err != nil {
		t.Fatalf("failed to age completed entry: %v", err)
	}

	deleted, err := store.DeleteCompletedOutboxEntries(ctx, time.Hour)
	if err != nil {
		t.Fatalf("DeleteCompletedOutboxEntries failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deleted completed entry, got %d", deleted)
	}

	remaining, err := store.GetPendingOutboxEntries(ctx, 10)
	if err != nil {
		t.Fatalf("GetPendingOutboxEntries final check failed: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected no remaining pending entries, got %d", len(remaining))
	}
}
