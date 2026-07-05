package storage

import (
	"testing"
	"time"
)

func TestCreateBenchmarkRunnerSession_Basic(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	sess := &BenchmarkRunnerSession{
		ID:         "sess-1",
		Status:     "running",
		Command:    "benchmark run --workflow test",
		TotalRuns:  3,
		TotalItems: 100,
		StartedAt:  time.Now().UTC(),
	}
	if err := store.CreateBenchmarkRunnerSession(sess); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Verify via GetActiveBenchmarkRunnerSession
	active, err := store.GetActiveBenchmarkRunnerSession()
	if err != nil {
		t.Fatalf("get active: %v", err)
	}
	if active == nil {
		t.Fatal("expected active session, got nil")
	}
	if active.ID != "sess-1" {
		t.Errorf("expected id=sess-1, got %s", active.ID)
	}
	if active.Status != "running" {
		t.Errorf("expected status=running, got %s", active.Status)
	}
	if active.TotalRuns != 3 {
		t.Errorf("expected total_runs=3, got %d", active.TotalRuns)
	}
	if active.TotalItems != 100 {
		t.Errorf("expected total_items=100, got %d", active.TotalItems)
	}
}

func TestGetActiveBenchmarkRunnerSession_NoRunning(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	active, err := store.GetActiveBenchmarkRunnerSession()
	if err != nil {
		t.Fatalf("get active: %v", err)
	}
	if active != nil {
		t.Errorf("expected nil for no running sessions, got %+v", active)
	}
}

func TestUpdateBenchmarkRunnerSession(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	sess := &BenchmarkRunnerSession{
		ID:        "sess-update",
		Status:    "running",
		Command:   "run",
		StartedAt: time.Now().UTC(),
	}
	if err := store.CreateBenchmarkRunnerSession(sess); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Update with some fields
	completedRuns := 2
	currentBench := "mmlu"
	if err := store.UpdateBenchmarkRunnerSession("sess-update", BenchmarkRunnerSessionUpdate{
		CompletedRuns:    &completedRuns,
		CurrentBenchmark: &currentBench,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	active, err := store.GetActiveBenchmarkRunnerSession()
	if err != nil {
		t.Fatalf("get active: %v", err)
	}
	if active.CompletedRuns != 2 {
		t.Errorf("expected completed_runs=2, got %d", active.CompletedRuns)
	}
	if active.CurrentBenchmark != "mmlu" {
		t.Errorf("expected current_benchmark=mmlu, got %s", active.CurrentBenchmark)
	}
}

func TestUpdateBenchmarkRunnerSession_EmptyUpdateIsNoOp(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	// Empty update should not error even if session doesn't exist
	if err := store.UpdateBenchmarkRunnerSession("nonexistent", BenchmarkRunnerSessionUpdate{}); err != nil {
		t.Fatalf("empty update: %v", err)
	}
}

func TestAbandonStaleRunnerSessions(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	// Create two running sessions
	for _, id := range []string{"sess-a", "sess-b"} {
		sess := &BenchmarkRunnerSession{
			ID:        id,
			Status:    "running",
			Command:   "run",
			StartedAt: time.Now().UTC(),
		}
		if err := store.CreateBenchmarkRunnerSession(sess); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}

	n, err := store.AbandonStaleRunnerSessions()
	if err != nil {
		t.Fatalf("abandon: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 abandoned, got %d", n)
	}

	// No more active sessions
	active, err := store.GetActiveBenchmarkRunnerSession()
	if err != nil {
		t.Fatalf("get active: %v", err)
	}
	if active != nil {
		t.Error("expected no active sessions after abandon")
	}

	// Running abandon again should affect 0
	n2, err := store.AbandonStaleRunnerSessions()
	if err != nil {
		t.Fatalf("second abandon: %v", err)
	}
	if n2 != 0 {
		t.Errorf("expected 0 on second abandon, got %d", n2)
	}
}
