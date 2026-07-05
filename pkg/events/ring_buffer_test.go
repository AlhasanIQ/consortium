package events

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func makeEvent(seq int64) *EventEnvelope {
	return &EventEnvelope{
		Version:   1,
		JobID:     "job-1",
		Sequence:  seq,
		Type:      "test",
		Timestamp: time.Now(),
	}
}

// --- RingBuffer tests ---

func TestNewRingBuffer_MinCapacity(t *testing.T) {
	rb := NewRingBuffer(0)
	if rb.Capacity() != 1 {
		t.Fatalf("expected capacity clamped to 1, got %d", rb.Capacity())
	}
	rb2 := NewRingBuffer(-5)
	if rb2.Capacity() != 1 {
		t.Fatalf("expected capacity clamped to 1, got %d", rb2.Capacity())
	}
}

func TestRingBuffer_EmptyBuffer(t *testing.T) {
	rb := NewRingBuffer(10)

	if rb.Size() != 0 {
		t.Fatalf("expected size 0, got %d", rb.Size())
	}
	if rb.MinSequence() != -1 {
		t.Fatalf("expected MinSequence -1, got %d", rb.MinSequence())
	}
	if rb.MaxSequence() != -1 {
		t.Fatalf("expected MaxSequence -1, got %d", rb.MaxSequence())
	}
	if events := rb.GetAll(); events != nil {
		t.Fatalf("expected nil from GetAll on empty buffer, got %v", events)
	}
	if events := rb.GetAfter(0); events != nil {
		t.Fatalf("expected nil from GetAfter on empty buffer, got %v", events)
	}
	if rb.Contains(1) {
		t.Fatal("expected Contains to return false on empty buffer")
	}
}

func TestRingBuffer_SinglePushGet(t *testing.T) {
	rb := NewRingBuffer(5)
	ev := makeEvent(1)
	rb.Push(ev)

	if rb.Size() != 1 {
		t.Fatalf("expected size 1, got %d", rb.Size())
	}
	if rb.MinSequence() != 1 {
		t.Fatalf("expected MinSequence 1, got %d", rb.MinSequence())
	}
	if rb.MaxSequence() != 1 {
		t.Fatalf("expected MaxSequence 1, got %d", rb.MaxSequence())
	}
	if !rb.Contains(1) {
		t.Fatal("expected Contains(1) to be true")
	}

	all := rb.GetAll()
	if len(all) != 1 || all[0].Sequence != 1 {
		t.Fatalf("expected GetAll to return [seq=1], got %v", all)
	}
}

func TestRingBuffer_PushToCapacity(t *testing.T) {
	cap := 5
	rb := NewRingBuffer(cap)
	for i := int64(1); i <= int64(cap); i++ {
		rb.Push(makeEvent(i))
	}

	if rb.Size() != cap {
		t.Fatalf("expected size %d, got %d", cap, rb.Size())
	}
	if rb.MinSequence() != 1 {
		t.Fatalf("expected MinSequence 1, got %d", rb.MinSequence())
	}
	if rb.MaxSequence() != int64(cap) {
		t.Fatalf("expected MaxSequence %d, got %d", cap, rb.MaxSequence())
	}

	all := rb.GetAll()
	if len(all) != cap {
		t.Fatalf("expected %d events, got %d", cap, len(all))
	}
	for i, ev := range all {
		if ev.Sequence != int64(i+1) {
			t.Fatalf("event %d: expected seq %d, got %d", i, i+1, ev.Sequence)
		}
	}
}

func TestRingBuffer_WrapAround(t *testing.T) {
	rb := NewRingBuffer(3)
	// Push 5 events into a buffer of capacity 3 => events 1,2 evicted
	for i := int64(1); i <= 5; i++ {
		rb.Push(makeEvent(i))
	}

	if rb.Size() != 3 {
		t.Fatalf("expected size 3 after wrap, got %d", rb.Size())
	}
	if rb.MinSequence() != 3 {
		t.Fatalf("expected MinSequence 3, got %d", rb.MinSequence())
	}
	if rb.MaxSequence() != 5 {
		t.Fatalf("expected MaxSequence 5, got %d", rb.MaxSequence())
	}

	all := rb.GetAll()
	if len(all) != 3 {
		t.Fatalf("expected 3 events, got %d", len(all))
	}
	for i, expected := range []int64{3, 4, 5} {
		if all[i].Sequence != expected {
			t.Fatalf("event %d: expected seq %d, got %d", i, expected, all[i].Sequence)
		}
	}

	// Evicted events should not be found
	if rb.Contains(1) {
		t.Fatal("expected evicted event seq=1 to not be found")
	}
	if rb.Contains(2) {
		t.Fatal("expected evicted event seq=2 to not be found")
	}
	if !rb.Contains(3) {
		t.Fatal("expected seq=3 to be found")
	}
}

func TestRingBuffer_GetAfter_Various(t *testing.T) {
	rb := NewRingBuffer(5)
	for i := int64(10); i <= 14; i++ {
		rb.Push(makeEvent(i))
	}
	// Buffer: [10, 11, 12, 13, 14]

	tests := []struct {
		name           string
		afterSeq       int64
		expectedCount  int
		expectedFirstS int64
	}{
		{"before all", 5, 5, 10},
		{"at start", 10, 4, 11},
		{"middle", 12, 2, 13},
		{"at end", 14, 0, 0},
		{"beyond end", 100, 0, 0},
		{"negative", -1, 5, 10},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := rb.GetAfter(tc.afterSeq)
			if len(result) != tc.expectedCount {
				t.Fatalf("GetAfter(%d): expected %d events, got %d", tc.afterSeq, tc.expectedCount, len(result))
			}
			if tc.expectedCount > 0 && result[0].Sequence != tc.expectedFirstS {
				t.Fatalf("GetAfter(%d): expected first seq %d, got %d", tc.afterSeq, tc.expectedFirstS, result[0].Sequence)
			}
		})
	}
}

func TestRingBuffer_GetAfter_WrappedBuffer(t *testing.T) {
	rb := NewRingBuffer(3)
	// Push 6 events => only 4,5,6 remain
	for i := int64(1); i <= 6; i++ {
		rb.Push(makeEvent(i))
	}

	result := rb.GetAfter(4)
	if len(result) != 2 {
		t.Fatalf("expected 2 events after seq 4, got %d", len(result))
	}
	if result[0].Sequence != 5 || result[1].Sequence != 6 {
		t.Fatalf("expected [5,6], got [%d,%d]", result[0].Sequence, result[1].Sequence)
	}
}

func TestRingBuffer_Clear(t *testing.T) {
	rb := NewRingBuffer(5)
	for i := int64(1); i <= 3; i++ {
		rb.Push(makeEvent(i))
	}
	rb.Clear()

	if rb.Size() != 0 {
		t.Fatalf("expected size 0 after clear, got %d", rb.Size())
	}
	if rb.MinSequence() != -1 {
		t.Fatalf("expected MinSequence -1 after clear, got %d", rb.MinSequence())
	}
	if rb.GetAll() != nil {
		t.Fatal("expected nil from GetAll after clear")
	}
}

func TestRingBuffer_ConcurrentPushRead(t *testing.T) {
	rb := NewRingBuffer(100)
	var wg sync.WaitGroup

	// 10 writers, each pushing 100 events
	for w := 0; w < 10; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				seq := int64(writerID*1000 + i)
				rb.Push(makeEvent(seq))
			}
		}(w)
	}

	// 5 concurrent readers
	for r := 0; r < 5; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_ = rb.GetAll()
				_ = rb.GetAfter(0)
				_ = rb.Size()
				_ = rb.MinSequence()
				_ = rb.MaxSequence()
			}
		}()
	}

	wg.Wait()

	// After all writes, buffer should be at capacity (1000 events into cap 100)
	if rb.Size() != 100 {
		t.Fatalf("expected size 100, got %d", rb.Size())
	}
}

func TestRingBuffer_CapacityOne(t *testing.T) {
	rb := NewRingBuffer(1)

	rb.Push(makeEvent(1))
	if rb.Size() != 1 {
		t.Fatalf("expected size 1, got %d", rb.Size())
	}
	if rb.MinSequence() != 1 || rb.MaxSequence() != 1 {
		t.Fatal("expected min=max=1")
	}

	rb.Push(makeEvent(2))
	if rb.Size() != 1 {
		t.Fatalf("expected size 1 after wrap, got %d", rb.Size())
	}
	if rb.MinSequence() != 2 || rb.MaxSequence() != 2 {
		t.Fatal("expected min=max=2 after overwrite")
	}
	if rb.Contains(1) {
		t.Fatal("expected seq=1 evicted")
	}
}

// --- RingBufferCache tests ---

func TestRingBufferCache_GetOrCreate(t *testing.T) {
	cache := NewRingBufferCache(10, 5)

	rb1 := cache.GetOrCreate("job-a")
	if rb1 == nil {
		t.Fatal("expected non-nil buffer")
	}
	if rb1.Capacity() != 10 {
		t.Fatalf("expected capacity 10, got %d", rb1.Capacity())
	}

	// Same job returns same buffer
	rb2 := cache.GetOrCreate("job-a")
	if rb1 != rb2 {
		t.Fatal("expected same buffer for same job ID")
	}

	if cache.Size() != 1 {
		t.Fatalf("expected cache size 1, got %d", cache.Size())
	}
}

func TestRingBufferCache_Get(t *testing.T) {
	cache := NewRingBufferCache(10, 5)

	if cache.Get("nonexistent") != nil {
		t.Fatal("expected nil for nonexistent job")
	}

	cache.GetOrCreate("job-a")
	if cache.Get("job-a") == nil {
		t.Fatal("expected non-nil for existing job")
	}
}

func TestRingBufferCache_Remove(t *testing.T) {
	cache := NewRingBufferCache(10, 5)
	cache.GetOrCreate("job-a")
	cache.Remove("job-a")

	if cache.Size() != 0 {
		t.Fatalf("expected size 0 after remove, got %d", cache.Size())
	}
	if cache.Get("job-a") != nil {
		t.Fatal("expected nil after remove")
	}
}

func TestRingBufferCache_Eviction(t *testing.T) {
	cache := NewRingBufferCache(10, 3) // Max 3 buffers

	cache.GetOrCreate("job-1")
	cache.GetOrCreate("job-2")
	cache.GetOrCreate("job-3")

	if cache.Size() != 3 {
		t.Fatalf("expected 3 buffers, got %d", cache.Size())
	}

	// Adding a 4th should evict one
	cache.GetOrCreate("job-4")
	if cache.Size() != 3 {
		t.Fatalf("expected 3 buffers after eviction, got %d", cache.Size())
	}

	// job-4 must exist
	if cache.Get("job-4") == nil {
		t.Fatal("expected job-4 to exist after creation")
	}
}

func TestRingBufferCache_ConcurrentAccess(t *testing.T) {
	cache := NewRingBufferCache(10, 100)
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			jobID := fmt.Sprintf("job-%d", id%10)
			rb := cache.GetOrCreate(jobID)
			rb.Push(makeEvent(int64(id)))
			_ = cache.Get(jobID)
			_ = cache.Size()
		}(i)
	}

	wg.Wait()

	if cache.Size() > 10 {
		t.Fatalf("expected at most 10 buffers, got %d", cache.Size())
	}
}
