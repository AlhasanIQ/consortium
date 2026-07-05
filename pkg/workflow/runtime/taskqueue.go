package runtime

import (
	"fmt"
	"sync"
)

// TaskQueue provides partitioned, bounded dispatch for activities.
// This is a single-process implementation using Go channels.
type TaskQueue struct {
	mu       sync.RWMutex
	queues   map[string]chan *TaskItem
	capacity int
	closed   bool
}

// TaskItem represents a unit of work dispatched through the queue.
type TaskItem struct {
	ID          string
	PartitionID string
	Payload     interface{}
}

// NewTaskQueue creates a task queue with the given per-partition capacity.
func NewTaskQueue(capacity int) *TaskQueue {
	if capacity <= 0 {
		capacity = 100
	}
	return &TaskQueue{
		queues:   make(map[string]chan *TaskItem),
		capacity: capacity,
	}
}

// Enqueue adds a task item to the specified partition.
// Returns an error if the partition is at capacity (backpressure).
func (q *TaskQueue) Enqueue(partitionID string, item *TaskItem) error {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return fmt.Errorf("task queue is closed")
	}
	ch, ok := q.queues[partitionID]
	if !ok {
		ch = make(chan *TaskItem, q.capacity)
		q.queues[partitionID] = ch
	}
	q.mu.Unlock()

	item.PartitionID = partitionID
	select {
	case ch <- item:
		return nil
	default:
		return fmt.Errorf("task queue partition %q at capacity (%d)", partitionID, q.capacity)
	}
}

// Dequeue takes the next item from a partition. Blocks until available.
// Returns nil if the queue is closed.
func (q *TaskQueue) Dequeue(partitionID string) *TaskItem {
	q.mu.RLock()
	ch, ok := q.queues[partitionID]
	q.mu.RUnlock()
	if !ok {
		return nil
	}
	item, ok := <-ch
	if !ok {
		return nil
	}
	return item
}

// Len returns the number of items in a partition.
func (q *TaskQueue) Len(partitionID string) int {
	q.mu.RLock()
	ch, ok := q.queues[partitionID]
	q.mu.RUnlock()
	if !ok {
		return 0
	}
	return len(ch)
}

// Close shuts down the task queue.
func (q *TaskQueue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	for _, ch := range q.queues {
		close(ch)
	}
}

// Pending operation budgets — scheduler checks these before issuing commands.
const (
	MaxPendingActivities = 2000
	MaxPendingChildRuns  = 1000
	MaxPendingSignals    = 2000
	MaxPendingUpdates    = 2000
)
