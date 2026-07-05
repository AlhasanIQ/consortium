package durable

import (
	"context"
	"sync"
	"time"
)

// HeartbeatReporter allows activities to report liveness during long-running operations.
// The runtime monitors heartbeats and cancels activities that miss their deadline.
type HeartbeatReporter struct {
	mu         sync.Mutex
	lastBeat   time.Time
	details    map[string]interface{}
	activityID string
	nodeID     string
	timeout    time.Duration
	cancelFn   context.CancelFunc
	stopped    bool
}

// NewHeartbeatReporter creates a heartbeat reporter for an activity.
func NewHeartbeatReporter(activityID, nodeID string, timeout time.Duration, cancelFn context.CancelFunc) *HeartbeatReporter {
	return &HeartbeatReporter{
		activityID: activityID,
		nodeID:     nodeID,
		timeout:    timeout,
		cancelFn:   cancelFn,
		lastBeat:   time.Now(),
	}
}

// Beat records a heartbeat with optional detail data.
func (h *HeartbeatReporter) Beat(details map[string]interface{}) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastBeat = time.Now()
	h.details = details
}

// LastBeat returns the time of the last heartbeat.
func (h *HeartbeatReporter) LastBeat() time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastBeat
}

// Details returns the last heartbeat details.
func (h *HeartbeatReporter) Details() map[string]interface{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.details
}

// Stop ends the heartbeat monitor.
func (h *HeartbeatReporter) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopped = true
}

// IsStopped returns whether the reporter has been stopped.
func (h *HeartbeatReporter) IsStopped() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stopped
}

// MonitorHeartbeats watches for missed heartbeats and cancels the activity context.
// This should be run as a goroutine. It exits when the context is cancelled or
// the reporter is stopped.
func MonitorHeartbeats(ctx context.Context, reporter *HeartbeatReporter, onTimeout func()) {
	if reporter.timeout <= 0 {
		return // No heartbeat monitoring configured
	}

	ticker := time.NewTicker(reporter.timeout / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if reporter.IsStopped() {
				return
			}
			since := time.Since(reporter.LastBeat())
			if since > reporter.timeout {
				if onTimeout != nil {
					onTimeout()
				}
				reporter.cancelFn()
				return
			}
		}
	}
}

// heartbeatContextKey is used to store the HeartbeatReporter in context.
type heartbeatContextKey struct{}

// ContextWithHeartbeat returns a new context containing the heartbeat reporter.
func ContextWithHeartbeat(ctx context.Context, reporter *HeartbeatReporter) context.Context {
	return context.WithValue(ctx, heartbeatContextKey{}, reporter)
}

// HeartbeatFromContext extracts the heartbeat reporter from context.
// Returns nil if no reporter is present. Activities can use this to send beats.
func HeartbeatFromContext(ctx context.Context) *HeartbeatReporter {
	r, _ := ctx.Value(heartbeatContextKey{}).(*HeartbeatReporter)
	return r
}
