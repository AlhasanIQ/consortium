package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestAppendHistoryEvent_AssignsMonotonicSequencePerRun(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	evA1 := &HistoryEvent{RunID: "run-a", Type: "workflow_started"}
	evB1 := &HistoryEvent{RunID: "run-b", Type: "workflow_started"}
	evA2 := &HistoryEvent{RunID: "run-a", Type: "activity_started", NodeID: "n1"}
	evB2 := &HistoryEvent{RunID: "run-b", Type: "activity_started", NodeID: "n1"}
	evA3 := &HistoryEvent{RunID: "run-a", Type: "activity_completed", NodeID: "n1"}

	for _, ev := range []*HistoryEvent{evA1, evB1, evA2, evB2, evA3} {
		if err := store.AppendHistoryEvent(ctx, ev); err != nil {
			t.Fatalf("append history event failed: %v", err)
		}
	}

	if evA1.Sequence != 1 || evA2.Sequence != 2 || evA3.Sequence != 3 {
		t.Fatalf("expected run-a sequences [1,2,3], got [%d,%d,%d]", evA1.Sequence, evA2.Sequence, evA3.Sequence)
	}
	if evB1.Sequence != 1 || evB2.Sequence != 2 {
		t.Fatalf("expected run-b sequences [1,2], got [%d,%d]", evB1.Sequence, evB2.Sequence)
	}

	runAEvents, err := store.GetHistoryEvents(ctx, "run-a")
	if err != nil {
		t.Fatalf("GetHistoryEvents(run-a) failed: %v", err)
	}
	if len(runAEvents) != 3 {
		t.Fatalf("expected 3 run-a events, got %d", len(runAEvents))
	}
	if runAEvents[0].Sequence != 1 || runAEvents[1].Sequence != 2 || runAEvents[2].Sequence != 3 {
		t.Fatalf("expected run-a sequences [1,2,3], got [%d,%d,%d]", runAEvents[0].Sequence, runAEvents[1].Sequence, runAEvents[2].Sequence)
	}

	runBEvents, err := store.GetHistoryEvents(ctx, "run-b")
	if err != nil {
		t.Fatalf("GetHistoryEvents(run-b) failed: %v", err)
	}
	if len(runBEvents) != 2 {
		t.Fatalf("expected 2 run-b events, got %d", len(runBEvents))
	}
	if runBEvents[0].Sequence != 1 || runBEvents[1].Sequence != 2 {
		t.Fatalf("expected run-b sequences [1,2], got [%d,%d]", runBEvents[0].Sequence, runBEvents[1].Sequence)
	}
}

func TestGetHistoryEventsAfter_ReturnsStrictlyGreaterSequences(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	for i := 0; i < 4; i++ {
		if err := store.AppendHistoryEvent(ctx, &HistoryEvent{
			RunID: "after-run",
			Type:  "status",
			NodeID: func() string {
				return "n" + string(rune('1'+i))
			}(),
		}); err != nil {
			t.Fatalf("append event %d failed: %v", i+1, err)
		}
	}

	events, err := store.GetHistoryEventsAfter(ctx, "after-run", 2)
	if err != nil {
		t.Fatalf("GetHistoryEventsAfter failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events after sequence 2, got %d", len(events))
	}
	if events[0].Sequence != 3 || events[1].Sequence != 4 {
		t.Fatalf("expected sequences [3,4], got [%d,%d]", events[0].Sequence, events[1].Sequence)
	}
}

func TestHistoryEvent_RoundTripTimestampAndAttributes(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	explicitTS := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
	attrs := map[string]interface{}{
		"attempt":  float64(2),
		"message":  "done",
		"metadata": map[string]interface{}{"source": "unit-test"},
	}

	if err := store.AppendHistoryEvent(ctx, &HistoryEvent{
		RunID:      "roundtrip-run",
		Type:       "activity_completed",
		NodeID:     "node-1",
		ActivityID: "act-1",
		Timestamp:  explicitTS,
		Attributes: attrs,
	}); err != nil {
		t.Fatalf("append event failed: %v", err)
	}

	events, err := store.GetHistoryEvents(ctx, "roundtrip-run")
	if err != nil {
		t.Fatalf("GetHistoryEvents failed: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	ev := events[0]
	if ev.NodeID != "node-1" || ev.ActivityID != "act-1" {
		t.Fatalf("unexpected correlation fields: node=%q activity=%q", ev.NodeID, ev.ActivityID)
	}
	if !ev.Timestamp.Equal(explicitTS) {
		t.Fatalf("expected timestamp %v, got %v", explicitTS, ev.Timestamp)
	}
	if ev.Attributes["message"] != "done" {
		t.Fatalf("expected message attribute 'done', got %v", ev.Attributes["message"])
	}
	if attempt, ok := ev.Attributes["attempt"].(float64); !ok || attempt != 2 {
		t.Fatalf("expected attempt attribute 2, got %v", ev.Attributes["attempt"])
	}

	count, err := store.GetHistoryEventCount(ctx, "roundtrip-run")
	if err != nil {
		t.Fatalf("GetHistoryEventCount failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected count 1, got %d", count)
	}

	size, err := store.GetHistorySize(ctx, "roundtrip-run")
	if err != nil {
		t.Fatalf("GetHistorySize failed: %v", err)
	}
	if size <= 2 {
		t.Fatalf("expected non-trivial serialized attribute size, got %d", size)
	}
}

func TestAppendHistoryEvent_ConcurrentRuns_NoTransactionNestingError(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage failed: %v", err)
	}
	defer store.Close()

	const workers = 40
	ctx := context.Background()

	var wg sync.WaitGroup
	errCh := make(chan error, workers)

	for i := 0; i < workers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			runID := fmt.Sprintf("run-%d", i)
			if err := store.AppendHistoryEvent(ctx, &HistoryEvent{
				RunID: runID,
				Type:  "workflow_started",
			}); err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("unexpected append error: %v", err)
	}
}

func TestAppendHistoryEvent_ConcurrentSameRun_AssignsUniqueMonotonicSequence(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage failed: %v", err)
	}
	defer store.Close()

	const workers = 50
	const runID = "same-run"
	ctx := context.Background()

	var wg sync.WaitGroup
	errCh := make(chan error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.AppendHistoryEvent(ctx, &HistoryEvent{
				RunID: runID,
				Type:  "activity_completed",
			}); err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("unexpected append error: %v", err)
	}

	events, err := store.GetHistoryEvents(ctx, runID)
	if err != nil {
		t.Fatalf("GetHistoryEvents failed: %v", err)
	}
	if len(events) != workers {
		t.Fatalf("expected %d events, got %d", workers, len(events))
	}

	for i, ev := range events {
		want := int64(i + 1)
		if ev.Sequence != want {
			t.Fatalf("expected sequence %d at index %d, got %d", want, i, ev.Sequence)
		}
	}
}

func TestAppendHistoryEventBatch(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	t.Run("batch of 3 events gets monotonic sequences", func(t *testing.T) {
		events := []*HistoryEvent{
			{RunID: "batch-run", Type: "workflow_started"},
			{RunID: "batch-run", Type: "activity_started", NodeID: "n1"},
			{RunID: "batch-run", Type: "activity_completed", NodeID: "n1"},
		}

		if err := store.AppendHistoryEventBatch(ctx, events); err != nil {
			t.Fatalf("AppendHistoryEventBatch failed: %v", err)
		}

		// Verify sequences assigned to the event structs
		for i, ev := range events {
			want := int64(i + 1)
			if ev.Sequence != want {
				t.Fatalf("expected sequence %d for event %d, got %d", want, i, ev.Sequence)
			}
		}

		// Verify events are persisted and retrievable
		stored, err := store.GetHistoryEvents(ctx, "batch-run")
		if err != nil {
			t.Fatalf("GetHistoryEvents failed: %v", err)
		}
		if len(stored) != 3 {
			t.Fatalf("expected 3 stored events, got %d", len(stored))
		}
		for i, ev := range stored {
			want := int64(i + 1)
			if ev.Sequence != want {
				t.Fatalf("expected stored sequence %d at index %d, got %d", want, i, ev.Sequence)
			}
		}
	})

	t.Run("single event batch delegates correctly", func(t *testing.T) {
		ev := &HistoryEvent{RunID: "single-batch-run", Type: "workflow_started"}
		if err := store.AppendHistoryEventBatch(ctx, []*HistoryEvent{ev}); err != nil {
			t.Fatalf("AppendHistoryEventBatch (single) failed: %v", err)
		}
		if ev.Sequence != 1 {
			t.Fatalf("expected sequence 1 for single-event batch, got %d", ev.Sequence)
		}

		stored, err := store.GetHistoryEvents(ctx, "single-batch-run")
		if err != nil {
			t.Fatalf("GetHistoryEvents failed: %v", err)
		}
		if len(stored) != 1 {
			t.Fatalf("expected 1 stored event, got %d", len(stored))
		}
	})

	t.Run("empty batch is a no-op", func(t *testing.T) {
		if err := store.AppendHistoryEventBatch(ctx, []*HistoryEvent{}); err != nil {
			t.Fatalf("AppendHistoryEventBatch (empty) should be no-op, got error: %v", err)
		}
		if err := store.AppendHistoryEventBatch(ctx, nil); err != nil {
			t.Fatalf("AppendHistoryEventBatch (nil) should be no-op, got error: %v", err)
		}
	})
}

func TestAppendHistoryEventBatch_RollsBackOnLaterEventFailure(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage failed: %v", err)
	}
	defer store.Close()

	events := []*HistoryEvent{
		{RunID: "atomic-batch", Type: "workflow_started"},
		{
			RunID: "atomic-batch",
			Type:  "activity_started",
			Attributes: map[string]interface{}{
				"unmarshalable": make(chan int),
			},
		},
	}
	if err := store.AppendHistoryEventBatch(context.Background(), events); err == nil {
		t.Fatal("AppendHistoryEventBatch accepted an event with unmarshalable attributes")
	}

	stored, err := store.GetHistoryEvents(context.Background(), "atomic-batch")
	if err != nil {
		t.Fatalf("GetHistoryEvents after failed batch: %v", err)
	}
	if len(stored) != 0 {
		t.Fatalf("failed batch partially persisted %d event(s): %+v", len(stored), stored)
	}
}

func TestRetryHistoryBatch(t *testing.T) {
	t.Run("retries transient begin transaction errors", func(t *testing.T) {
		attempts := 0
		err := retryHistoryBatch(context.Background(), func() (bool, error) {
			attempts++
			if attempts == 1 {
				return true, errors.New("failed to begin batch history tx: cannot start a transaction within a transaction")
			}
			return false, nil
		})
		if err != nil {
			t.Fatalf("retryHistoryBatch returned error: %v", err)
		}
		if attempts != 2 {
			t.Fatalf("expected 2 attempts, got %d", attempts)
		}
	})

	t.Run("does not retry non-retryable attempts", func(t *testing.T) {
		attempts := 0
		wantErr := errors.New("failed to commit batch history tx: database is locked")
		err := retryHistoryBatch(context.Background(), func() (bool, error) {
			attempts++
			return false, wantErr
		})
		if !errors.Is(err, wantErr) {
			t.Fatalf("expected %v, got %v", wantErr, err)
		}
		if attempts != 1 {
			t.Fatalf("expected 1 attempt, got %d", attempts)
		}
	})
}
