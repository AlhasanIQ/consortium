package storage

import (
	"context"
	"testing"
)

func TestExecutionReplayRequestStore(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage failed: %v", err)
	}
	defer store.Close()

	job := &WorkflowExecution{
		ID:          "job-replay-store",
		Description: "replay store test",
		Model:       "workflow",
		Status:      "pending",
		WorkflowID:  "wf-replay-store",
	}
	if err := store.CreateExecution(job); err != nil {
		t.Fatalf("CreateExecution failed: %v", err)
	}

	ctx := context.Background()
	initial := `{"mode":"best_effort","seed_nodes":{"n1":{"output":"x"}}}`
	if err := store.UpsertExecutionReplayRequest(ctx, job.ID, initial); err != nil {
		t.Fatalf("UpsertExecutionReplayRequest failed: %v", err)
	}

	got, err := store.GetExecutionReplayRequest(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetExecutionReplayRequest failed: %v", err)
	}
	if got != initial {
		t.Fatalf("replay payload = %q, want %q", got, initial)
	}

	updated := `{"mode":"required"}`
	if err := store.UpsertExecutionReplayRequest(ctx, job.ID, updated); err != nil {
		t.Fatalf("UpsertExecutionReplayRequest update failed: %v", err)
	}
	got, err = store.GetExecutionReplayRequest(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetExecutionReplayRequest (updated) failed: %v", err)
	}
	if got != updated {
		t.Fatalf("updated replay payload = %q, want %q", got, updated)
	}

	if err := store.UpsertExecutionReplayRequest(ctx, job.ID, "   "); err != nil {
		t.Fatalf("empty replay payload should be ignored: %v", err)
	}
	got, err = store.GetExecutionReplayRequest(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetExecutionReplayRequest after empty update: %v", err)
	}
	if got != updated {
		t.Fatalf("empty replay update cleared the stored payload: got %q, want %q", got, updated)
	}

	if err := store.UpsertExecutionReplayRequest(ctx, "   ", updated); err == nil {
		t.Fatal("empty job ID should be rejected")
	}

	missing, err := store.GetExecutionReplayRequest(ctx, "missing-job")
	if err != nil {
		t.Fatalf("GetExecutionReplayRequest missing failed: %v", err)
	}
	if missing != "" {
		t.Fatalf("expected empty replay payload for missing job, got %q", missing)
	}
}
