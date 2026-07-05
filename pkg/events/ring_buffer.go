package events

import (
	"sync"
)

// RingBuffer provides a bounded, thread-safe in-memory cache for events per job.
// Used as a fast lookup cache before falling back to database for event replay.
type RingBuffer struct {
	capacity int
	events   []*EventEnvelope
	head     int  // Index where next event will be written
	size     int  // Current number of events in buffer
	full     bool // Whether buffer has wrapped around
	mu       sync.RWMutex
}

// NewRingBuffer creates a new ring buffer with the specified capacity.
// Capacity should be >= 1.
func NewRingBuffer(capacity int) *RingBuffer {
	if capacity < 1 {
		capacity = 1
	}
	return &RingBuffer{
		capacity: capacity,
		events:   make([]*EventEnvelope, capacity),
	}
}

// Push adds an event to the buffer.
// If the buffer is full, the oldest event is overwritten.
func (rb *RingBuffer) Push(event *EventEnvelope) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.events[rb.head] = event
	rb.head = (rb.head + 1) % rb.capacity

	if rb.size < rb.capacity {
		rb.size++
	} else {
		rb.full = true
	}
}

// GetAfter returns all events with sequence > afterSequence, ordered by sequence ascending.
// Returns an empty slice if no matching events are found.
func (rb *RingBuffer) GetAfter(afterSequence int64) []*EventEnvelope {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.size == 0 {
		return nil
	}

	// Collect all events that match the criteria
	var result []*EventEnvelope

	// Calculate the starting index (oldest event)
	var startIdx int
	if rb.full {
		startIdx = rb.head // When full, head points to oldest
	} else {
		startIdx = 0 // When not full, oldest is at 0
	}

	// Iterate through all events in order
	for i := 0; i < rb.size; i++ {
		idx := (startIdx + i) % rb.capacity
		event := rb.events[idx]
		if event != nil && event.Sequence > afterSequence {
			result = append(result, event)
		}
	}

	return result
}

// GetAll returns all events in the buffer, ordered by sequence ascending.
func (rb *RingBuffer) GetAll() []*EventEnvelope {
	return rb.GetAfter(-1) // -1 ensures all events are included
}

// Size returns the current number of events in the buffer.
func (rb *RingBuffer) Size() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.size
}

// Capacity returns the maximum capacity of the buffer.
func (rb *RingBuffer) Capacity() int {
	return rb.capacity
}

// MinSequence returns the minimum sequence number in the buffer, or -1 if empty.
func (rb *RingBuffer) MinSequence() int64 {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.size == 0 {
		return -1
	}

	// Find the oldest event
	var startIdx int
	if rb.full {
		startIdx = rb.head
	} else {
		startIdx = 0
	}

	if rb.events[startIdx] != nil {
		return rb.events[startIdx].Sequence
	}
	return -1
}

// MaxSequence returns the maximum sequence number in the buffer, or -1 if empty.
func (rb *RingBuffer) MaxSequence() int64 {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.size == 0 {
		return -1
	}

	// The newest event is at head-1 (with wraparound)
	newestIdx := (rb.head - 1 + rb.capacity) % rb.capacity
	if rb.events[newestIdx] != nil {
		return rb.events[newestIdx].Sequence
	}
	return -1
}

// Contains checks if an event with the given sequence exists in the buffer.
func (rb *RingBuffer) Contains(sequence int64) bool {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	for i := 0; i < rb.size; i++ {
		var startIdx int
		if rb.full {
			startIdx = rb.head
		} else {
			startIdx = 0
		}
		idx := (startIdx + i) % rb.capacity
		if rb.events[idx] != nil && rb.events[idx].Sequence == sequence {
			return true
		}
	}
	return false
}

// Clear removes all events from the buffer.
func (rb *RingBuffer) Clear() {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.events = make([]*EventEnvelope, rb.capacity)
	rb.head = 0
	rb.size = 0
	rb.full = false
}

// RingBufferCache manages ring buffers for multiple jobs.
type RingBufferCache struct {
	buffers    map[string]*RingBuffer
	capacity   int // Capacity for each buffer
	maxBuffers int // Maximum number of buffers to keep
	mu         sync.RWMutex
}

// NewRingBufferCache creates a new cache for managing per-job ring buffers.
// capacity is the size of each ring buffer, maxBuffers limits memory usage.
func NewRingBufferCache(capacity, maxBuffers int) *RingBufferCache {
	return &RingBufferCache{
		buffers:    make(map[string]*RingBuffer),
		capacity:   capacity,
		maxBuffers: maxBuffers,
	}
}

// GetOrCreate returns the ring buffer for a job, creating one if it doesn't exist.
func (c *RingBufferCache) GetOrCreate(jobID string) *RingBuffer {
	c.mu.Lock()
	defer c.mu.Unlock()

	if rb, exists := c.buffers[jobID]; exists {
		return rb
	}

	// Evict oldest buffer if at capacity
	// Simple strategy: just remove any one buffer (could be improved with LRU)
	if len(c.buffers) >= c.maxBuffers {
		for oldJobID := range c.buffers {
			delete(c.buffers, oldJobID)
			break
		}
	}

	rb := NewRingBuffer(c.capacity)
	c.buffers[jobID] = rb
	return rb
}

// Get returns the ring buffer for a job, or nil if it doesn't exist.
func (c *RingBufferCache) Get(jobID string) *RingBuffer {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.buffers[jobID]
}

// Remove removes the ring buffer for a job.
func (c *RingBufferCache) Remove(jobID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.buffers, jobID)
}

// Size returns the number of buffers in the cache.
func (c *RingBufferCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.buffers)
}
