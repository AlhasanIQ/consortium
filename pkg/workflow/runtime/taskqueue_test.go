package runtime

import (
	"testing"
)

func TestNewTaskQueue(t *testing.T) {
	t.Run("positive capacity", func(t *testing.T) {
		q := NewTaskQueue(50)
		if q == nil {
			t.Fatal("NewTaskQueue returned nil")
		}
		if q.capacity != 50 {
			t.Errorf("capacity = %d, want 50", q.capacity)
		}
	})

	t.Run("zero capacity defaults to 100", func(t *testing.T) {
		q := NewTaskQueue(0)
		if q.capacity != 100 {
			t.Errorf("capacity = %d, want 100", q.capacity)
		}
	})

	t.Run("negative capacity defaults to 100", func(t *testing.T) {
		q := NewTaskQueue(-5)
		if q.capacity != 100 {
			t.Errorf("capacity = %d, want 100", q.capacity)
		}
	})
}

func TestTaskQueue_EnqueueDequeue(t *testing.T) {
	q := NewTaskQueue(10)

	item := &TaskItem{ID: "task-1", Payload: "data"}
	if err := q.Enqueue("partition-a", item); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	if item.PartitionID != "partition-a" {
		t.Errorf("PartitionID = %q, want %q", item.PartitionID, "partition-a")
	}

	got := q.Dequeue("partition-a")
	if got == nil {
		t.Fatal("Dequeue returned nil")
	}
	if got.ID != "task-1" {
		t.Errorf("got ID = %q, want %q", got.ID, "task-1")
	}
	if got.Payload != "data" {
		t.Errorf("got Payload = %v, want %q", got.Payload, "data")
	}
}

func TestTaskQueue_Len(t *testing.T) {
	q := NewTaskQueue(10)

	if n := q.Len("nonexistent"); n != 0 {
		t.Errorf("Len of nonexistent partition = %d, want 0", n)
	}

	q.Enqueue("p1", &TaskItem{ID: "a"})
	q.Enqueue("p1", &TaskItem{ID: "b"})
	q.Enqueue("p2", &TaskItem{ID: "c"})

	if n := q.Len("p1"); n != 2 {
		t.Errorf("Len(p1) = %d, want 2", n)
	}
	if n := q.Len("p2"); n != 1 {
		t.Errorf("Len(p2) = %d, want 1", n)
	}

	q.Dequeue("p1")
	if n := q.Len("p1"); n != 1 {
		t.Errorf("after dequeue, Len(p1) = %d, want 1", n)
	}
}

func TestTaskQueue_Backpressure(t *testing.T) {
	q := NewTaskQueue(2)

	q.Enqueue("p", &TaskItem{ID: "1"})
	q.Enqueue("p", &TaskItem{ID: "2"})

	// Third enqueue should fail (capacity = 2)
	err := q.Enqueue("p", &TaskItem{ID: "3"})
	if err == nil {
		t.Fatal("expected backpressure error when at capacity")
	}

	// Dequeue one, then enqueue should succeed
	q.Dequeue("p")
	if err := q.Enqueue("p", &TaskItem{ID: "3"}); err != nil {
		t.Fatalf("enqueue after dequeue should succeed: %v", err)
	}
}

func TestTaskQueue_DequeueNonexistentPartition(t *testing.T) {
	q := NewTaskQueue(10)
	got := q.Dequeue("does-not-exist")
	if got != nil {
		t.Errorf("Dequeue from nonexistent partition should return nil, got %+v", got)
	}
}

func TestTaskQueue_Close(t *testing.T) {
	q := NewTaskQueue(10)
	q.Enqueue("p", &TaskItem{ID: "1"})

	q.Close()

	// Enqueue after close should fail
	err := q.Enqueue("p", &TaskItem{ID: "2"})
	if err == nil {
		t.Fatal("expected error on enqueue after Close")
	}

	// Dequeue from closed channel returns nil (channel is closed)
	got := q.Dequeue("p")
	// First item is already buffered, so we may get it
	// After draining, next dequeue returns nil
	if got != nil {
		// drain the buffered item, next should be nil
		got2 := q.Dequeue("p")
		if got2 != nil {
			t.Error("expected nil after draining closed queue")
		}
	}
}

func TestTaskQueue_MultiplePartitions(t *testing.T) {
	q := NewTaskQueue(10)

	q.Enqueue("alpha", &TaskItem{ID: "a1"})
	q.Enqueue("beta", &TaskItem{ID: "b1"})
	q.Enqueue("alpha", &TaskItem{ID: "a2"})

	got := q.Dequeue("alpha")
	if got.ID != "a1" {
		t.Errorf("expected a1, got %s", got.ID)
	}

	got = q.Dequeue("beta")
	if got.ID != "b1" {
		t.Errorf("expected b1, got %s", got.ID)
	}

	got = q.Dequeue("alpha")
	if got.ID != "a2" {
		t.Errorf("expected a2, got %s", got.ID)
	}
}

func TestTaskQueue_Constants(t *testing.T) {
	if MaxPendingActivities != 2000 {
		t.Errorf("MaxPendingActivities = %d, want 2000", MaxPendingActivities)
	}
	if MaxPendingChildRuns != 1000 {
		t.Errorf("MaxPendingChildRuns = %d, want 1000", MaxPendingChildRuns)
	}
	if MaxPendingSignals != 2000 {
		t.Errorf("MaxPendingSignals = %d, want 2000", MaxPendingSignals)
	}
	if MaxPendingUpdates != 2000 {
		t.Errorf("MaxPendingUpdates = %d, want 2000", MaxPendingUpdates)
	}
}
