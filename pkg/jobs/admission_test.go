package jobs

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/storage"
)

func newTestManager(t *testing.T, maxConcurrent int) *Manager {
	t.Helper()
	db, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	registry := providers.NewRegistry()
	return NewManagerWithConfig(db, registry, &ManagerConfig{
		MaxConcurrentWorkflows:  maxConcurrent,
		MaxParallelNodesPerWF:   32,
		AdmissionTimeoutSeconds: 1, // short timeout for tests
	})
}

func TestAcquireSlot_ReturnsPoolExhaustedAtCapacity(t *testing.T) {
	m := newTestManager(t, 2)
	ctx := context.Background()

	// Acquire all slots
	if err := m.AcquireSlot(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.AcquireSlot(ctx); err != nil {
		t.Fatal(err)
	}

	// Third acquire should fail without taking a slot.
	err := m.AcquireSlot(ctx)
	if !IsAdmissionError(err) {
		t.Fatalf("expected admission error, got: %v", err)
	}
	if active, capacity := m.PoolStats(); active != 2 || capacity != 2 {
		t.Fatalf("exhausted acquire changed pool state: active=%d capacity=%d", active, capacity)
	}
	m.ReleaseSlot()
	m.ReleaseSlot()
}

func TestReleaseSlot_UnblocksWaiter(t *testing.T) {
	m := newTestManager(t, 1)
	ctx := context.Background()

	// Fill the pool
	if err := m.AcquireSlot(ctx); err != nil {
		t.Fatal(err)
	}

	// Start a waiter in background. The readiness signal is sent before the
	// acquire, so the test never needs a sleep to arrange the blocked state.
	started := make(chan struct{})
	acquired := make(chan struct{})
	go func() {
		close(started)
		if err := m.AcquireSlot(ctx); err != nil {
			t.Errorf("waiter got error: %v", err)
			return
		}
		close(acquired)
	}()

	<-started
	m.ReleaseSlot()

	select {
	case <-acquired:
		// OK — waiter got the slot
	case <-time.After(time.Second):
		t.Fatal("waiter did not acquire slot after release")
	}

	// Clean up
	m.ReleaseSlot()
}

func TestAcquireSlot_ContextCancellation(t *testing.T) {
	m := newTestManager(t, 1)
	ctx := context.Background()

	// Fill the pool
	if err := m.AcquireSlot(ctx); err != nil {
		t.Fatal(err)
	}

	// Cancel ctx while waiting. The readiness signal removes the need for a
	// timing-based delay before cancellation.
	cancelCtx, cancel := context.WithCancel(ctx)
	started := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		close(started)
		errCh <- m.AcquireSlot(cancelCtx)
	}()
	<-started
	cancel()

	select {
	case err := <-errCh:
		if err != context.Canceled {
			t.Fatalf("expected context.Canceled, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled waiter did not return")
	}

	// Clean up
	m.ReleaseSlot()
}

func TestClassifyAdmissionPauseReason(t *testing.T) {
	tests := []struct {
		name        string
		code        string
		message     string
		wantPause   bool
		wantCode    string
		wantReason  string
		wantMessage string
	}{
		{
			name:        "explicit auth code",
			code:        " auth_error ",
			wantPause:   true,
			wantCode:    "AUTH_ERROR",
			wantReason:  "auth_or_access_denied",
			wantMessage: "Provider authentication/access denied; admission paused",
		},
		{
			name:        "message infers credits",
			message:     "provider reports quota exhausted",
			wantPause:   true,
			wantCode:    "INSUFFICIENT_CREDITS",
			wantReason:  "insufficient_credits",
			wantMessage: "provider reports quota exhausted",
		},
		{
			name:        "message infers auth while preserving supplied code",
			code:        "UPSTREAM_FAILURE",
			message:     "request forbidden by provider",
			wantPause:   true,
			wantCode:    "UPSTREAM_FAILURE",
			wantReason:  "auth_or_access_denied",
			wantMessage: "request forbidden by provider",
		},
		{
			name:      "ordinary transient failure does not pause",
			code:      "5xx",
			message:   "temporary upstream failure",
			wantPause: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, paused := ClassifyAdmissionPauseReason(tt.code, tt.message)
			if paused != tt.wantPause {
				t.Fatalf("paused = %v, want %v (reason=%+v)", paused, tt.wantPause, got)
			}
			if !tt.wantPause {
				return
			}
			if got.Code != tt.wantCode || got.Reason != tt.wantReason || got.Message != tt.wantMessage {
				t.Fatalf("reason = %+v, want code=%q reason=%q message=%q", got, tt.wantCode, tt.wantReason, tt.wantMessage)
			}
		})
	}
}

func TestPoolStats(t *testing.T) {
	m := newTestManager(t, 5)

	active, capacity := m.PoolStats()
	if active != 0 {
		t.Fatalf("expected 0 active, got %d", active)
	}
	if capacity != 5 {
		t.Fatalf("expected capacity 5, got %d", capacity)
	}

	// Acquire two slots
	ctx := context.Background()
	_ = m.AcquireSlot(ctx)
	_ = m.AcquireSlot(ctx)

	active, capacity = m.PoolStats()
	if active != 2 {
		t.Fatalf("expected 2 active, got %d", active)
	}
	if capacity != 5 {
		t.Fatalf("expected capacity 5, got %d", capacity)
	}

	// Release one
	m.ReleaseSlot()
	active, _ = m.PoolStats()
	if active != 1 {
		t.Fatalf("expected 1 active, got %d", active)
	}

	m.ReleaseSlot()
}

func TestPoolConcurrentAccess(t *testing.T) {
	m := newTestManager(t, 5)
	ctx := context.Background()

	var wg sync.WaitGroup
	var mu sync.Mutex
	current := 0
	maxCurrent := 0
	acquired := make(chan struct{}, 20)
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })

	// Hold every acquired slot until all five capacity holders have arrived.
	// This proves the cap without a sleep and then releases all waiters in one
	// controlled transition.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := m.AcquireSlot(ctx); err != nil {
				t.Errorf("AcquireSlot failed: %v", err)
				return
			}
			mu.Lock()
			current++
			if current > maxCurrent {
				maxCurrent = current
			}
			mu.Unlock()
			acquired <- struct{}{}
			<-release
			mu.Lock()
			current--
			mu.Unlock()
			m.ReleaseSlot()
		}()
	}

	for i := 0; i < 5; i++ {
		select {
		case <-acquired:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for capacity holder %d", i+1)
		}
	}
	if active, capacity := m.PoolStats(); active != 5 || capacity != 5 {
		t.Fatalf("expected all capacity slots to be occupied, active=%d capacity=%d", active, capacity)
	}
	releaseOnce.Do(func() { close(release) })
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if maxCurrent != 5 {
		t.Fatalf("expected max concurrent holders 5, got %d", maxCurrent)
	}
	if current != 0 {
		t.Fatalf("expected 0 holders after all released, got %d", current)
	}
	if active, _ := m.PoolStats(); active != 0 {
		t.Fatalf("expected 0 active after all released, got %d", active)
	}
}

func TestDefaultManagerConfig(t *testing.T) {
	cfg := DefaultManagerConfig()
	if cfg.MaxConcurrentWorkflows != 150 {
		t.Fatalf("expected default 150, got %d", cfg.MaxConcurrentWorkflows)
	}
	if cfg.WorkerCount != 300 {
		t.Fatalf("expected default worker count 300, got %d", cfg.WorkerCount)
	}
	if cfg.WorkerInitialCount != 10 {
		t.Fatalf("expected default initial worker count 10, got %d", cfg.WorkerInitialCount)
	}
	if cfg.MaxParallelNodesPerWF != 32 {
		t.Fatalf("expected default 32, got %d", cfg.MaxParallelNodesPerWF)
	}
	if cfg.AdmissionTimeoutSeconds != 30 {
		t.Fatalf("expected default 30, got %d", cfg.AdmissionTimeoutSeconds)
	}
}

func TestNewManager_DefaultConfig(t *testing.T) {
	db, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	registry := providers.NewRegistry()

	// NewManager should apply default config values.
	m := NewManager(db, registry)
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	if m.config == nil {
		t.Fatal("config should not be nil")
	}
	_, capacity := m.PoolStats()
	if capacity != 150 {
		t.Fatalf("expected default capacity 150, got %d", capacity)
	}
}
