package jobs

import (
	"context"
	"sync"
	"sync/atomic"
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

func TestAcquireSlot_BlocksAtCapacity(t *testing.T) {
	m := newTestManager(t, 2)
	ctx := context.Background()

	// Acquire all slots
	if err := m.AcquireSlot(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.AcquireSlot(ctx); err != nil {
		t.Fatal(err)
	}

	// Third acquire should timeout
	start := time.Now()
	err := m.AcquireSlot(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected pool exhaustion error")
	}
	if !IsAdmissionError(err) {
		t.Fatalf("expected admission error, got: %v", err)
	}
	if elapsed < 900*time.Millisecond {
		t.Fatalf("expected to wait about 1s, waited %v", elapsed)
	}
}

func TestReleaseSlot_UnblocksWaiter(t *testing.T) {
	m := newTestManager(t, 1)
	ctx := context.Background()

	// Fill the pool
	if err := m.AcquireSlot(ctx); err != nil {
		t.Fatal(err)
	}

	// Start a waiter in background
	acquired := make(chan struct{})
	go func() {
		if err := m.AcquireSlot(ctx); err != nil {
			t.Errorf("waiter got error: %v", err)
			return
		}
		close(acquired)
	}()

	// Small delay then release
	time.Sleep(50 * time.Millisecond)
	m.ReleaseSlot()

	select {
	case <-acquired:
		// OK — waiter got the slot
	case <-time.After(3 * time.Second):
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

	// Cancel ctx while waiting
	cancelCtx, cancel := context.WithCancel(ctx)
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := m.AcquireSlot(cancelCtx)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}

	// Clean up
	m.ReleaseSlot()
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
	var acquired int64

	// Launch 20 goroutines competing for 5 slots
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := m.AcquireSlot(ctx); err == nil {
				atomic.AddInt64(&acquired, 1)
				time.Sleep(10 * time.Millisecond) // simulate work
				m.ReleaseSlot()
			}
		}()
	}

	wg.Wait()

	// All 20 should have eventually acquired and released
	if acquired != 20 {
		t.Fatalf("expected 20 acquisitions, got %d", acquired)
	}

	active, _ := m.PoolStats()
	if active != 0 {
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
