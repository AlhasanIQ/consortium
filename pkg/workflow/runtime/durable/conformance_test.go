package durable

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

// newTestStore creates an in-memory storage instance for testing.
func newTestStore(t *testing.T) *storage.Storage {
	t.Helper()
	store, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create in-memory storage: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// ---------------------------------------------------------------------------
// 1. Activity Idempotency
// ---------------------------------------------------------------------------

func TestConformance_ActivityIdempotency(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	key := "exec-1:nodeA:1"
	now := time.Now()

	// First save — should succeed.
	result := &storage.ActivityResult{
		IdempotencyKey: key,
		RunID:          "run-1",
		NodeID:         "nodeA",
		Attempt:        1,
		ActivityType:   "llm_call",
		Status:         "completed",
		OutputPayload:  "hello world",
		CompletedAt:    &now,
	}
	if err := store.SaveActivityResult(ctx, result); err != nil {
		t.Fatalf("first save failed: %v", err)
	}

	// Read back cached result.
	cached, err := store.GetActivityResult(ctx, key)
	if err != nil {
		t.Fatalf("get activity result failed: %v", err)
	}
	if cached == nil {
		t.Fatal("expected cached result, got nil")
	}
	if cached.OutputPayload != "hello world" {
		t.Errorf("expected output 'hello world', got %q", cached.OutputPayload)
	}
	if cached.Status != "completed" {
		t.Errorf("expected status 'completed', got %q", cached.Status)
	}

	// Second save with the same idempotency key but different output.
	// INSERT OR IGNORE should be a no-op — the original result must persist.
	result2 := &storage.ActivityResult{
		IdempotencyKey: key,
		RunID:          "run-1",
		NodeID:         "nodeA",
		Attempt:        1,
		ActivityType:   "llm_call",
		Status:         "completed",
		OutputPayload:  "DIFFERENT OUTPUT",
		CompletedAt:    &now,
	}
	if err := store.SaveActivityResult(ctx, result2); err != nil {
		t.Fatalf("second save (idempotent) failed: %v", err)
	}

	// Re-read — output must still be the original.
	cached2, err := store.GetActivityResult(ctx, key)
	if err != nil {
		t.Fatalf("second get failed: %v", err)
	}
	if cached2.OutputPayload != "hello world" {
		t.Errorf("idempotency violated: expected 'hello world', got %q", cached2.OutputPayload)
	}

	// Non-existent key returns nil, nil.
	missing, err := store.GetActivityResult(ctx, "nonexistent-key")
	if err != nil {
		t.Fatalf("expected nil error for missing key, got %v", err)
	}
	if missing != nil {
		t.Errorf("expected nil for missing key, got %+v", missing)
	}
}

// ---------------------------------------------------------------------------
// 2. Timeout Enforcement
// ---------------------------------------------------------------------------

func TestConformance_TimeoutEnforcement(t *testing.T) {
	// EffectiveStartToClose with nil config and 0 legacy → default (120s).
	d := runtime.EffectiveStartToClose(nil, 0)
	if d != runtime.DefaultStartToClose {
		t.Errorf("expected default %v, got %v", runtime.DefaultStartToClose, d)
	}

	// EffectiveStartToClose with legacy seconds.
	d = runtime.EffectiveStartToClose(nil, 5)
	if d != 5*time.Second {
		t.Errorf("expected 5s, got %v", d)
	}

	// EffectiveStartToClose with explicit config overrides legacy.
	cfg := &runtime.ActivityTimeoutConfig{StartToClose: 3 * time.Second}
	d = runtime.EffectiveStartToClose(cfg, 999)
	if d != 3*time.Second {
		t.Errorf("expected 3s from config, got %v", d)
	}

	// Verify an effective timeout is observable without waiting for a wall-clock second.
	timeout := runtime.EffectiveStartToClose(&runtime.ActivityTimeoutConfig{StartToClose: time.Nanosecond}, 999)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	<-ctx.Done()
	err := ctx.Err()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// 3. Non-Retryable Error Classification
// ---------------------------------------------------------------------------

func TestConformance_NonRetryableErrors(t *testing.T) {
	// Cost limit error code.
	if !runtime.IsNonRetryable(fmt.Errorf("cost limit exceeded"), "COST_LIMIT") {
		t.Error("COST_LIMIT error code should be non-retryable")
	}

	// Cost limit in message (no explicit code).
	if !runtime.IsNonRetryable(fmt.Errorf("cost limit reached"), "") {
		t.Error("'cost limit' in message should be non-retryable")
	}

	// CostLimit in message (camelCase variant).
	if !runtime.IsNonRetryable(fmt.Errorf("CostLimit exceeded"), "") {
		t.Error("'CostLimit' in message should be non-retryable")
	}

	// Other non-retryable codes.
	for _, code := range []string{"INVALID_WORKFLOW", "INVALID_CONFIG", "INSUFFICIENT_CREDITS", "AUTH_ERROR", "AUTHENTICATION", "AUTHORIZATION"} {
		if !runtime.IsNonRetryable(fmt.Errorf("something"), code) {
			t.Errorf("%s should be non-retryable", code)
		}
	}

	// Context cancellation.
	if !runtime.IsNonRetryable(context.Canceled, "") {
		t.Error("context.Canceled should be non-retryable")
	}
	if !runtime.IsNonRetryable(context.DeadlineExceeded, "") {
		t.Error("context.DeadlineExceeded should be non-retryable")
	}

	// A transient error should be retryable.
	if runtime.IsNonRetryable(fmt.Errorf("connection reset"), "TEMPORARY") {
		t.Error("TEMPORARY error should be retryable (not non-retryable)")
	}

	// Generic error with no code should be retryable.
	if runtime.IsNonRetryable(fmt.Errorf("random failure"), "") {
		t.Error("generic error with no code should be retryable")
	}
}
