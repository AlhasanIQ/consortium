package events

import (
	"context"
	"time"
)

// EventEnvelope is the stable event format for persistence and streaming.
// All events are wrapped in this envelope for consistent handling.
type EventEnvelope struct {
	Version     int                    `json:"version"`                // Schema version (currently 1)
	JobID       string                 `json:"job_id"`                 // Job this event belongs to
	Sequence    int64                  `json:"sequence"`               // Monotonically increasing per-job sequence
	Type        string                 `json:"type"`                   // One of Event* constants
	Timestamp   time.Time              `json:"timestamp"`              // When the event occurred
	Payload     map[string]interface{} `json:"payload"`                // Event-specific data
	NodeID      string                 `json:"node_id,omitempty"`      // Optional: node this event relates to
	Message     string                 `json:"message,omitempty"`      // Optional: human-readable message
	Error       string                 `json:"error,omitempty"`        // Optional: error message
	Code        string                 `json:"code,omitempty"`         // Optional: machine-readable error code
	ExecutionID string                 `json:"execution_id,omitempty"` // Durable logical execution identity
	RunID       string                 `json:"run_id,omitempty"`       // Durable per-run identity
	AgentRunID  string                 `json:"agent_run_id,omitempty"` // Agent loop correlation
	Iteration   int                    `json:"iteration,omitempty"`    // Agent loop iteration
}

// CurrentEventVersion is the current version of the event envelope schema
const CurrentEventVersion = 1

// EventStore defines the interface for persisting and retrieving events
type EventStore interface {
	// AppendEvent atomically appends an event and increments the job's sequence counter.
	// Returns the created envelope with assigned sequence number.
	AppendEvent(ctx context.Context, jobID string, eventType string, payload map[string]interface{}) (*EventEnvelope, error)

	// AppendEventWithDetails appends an event with additional details (node_id, message, error, code)
	AppendEventWithDetails(ctx context.Context, jobID, eventType, nodeID, message, errMsg, code string, payload map[string]interface{}) (*EventEnvelope, error)

	// GetEventsAfter returns events with sequence > afterSequence, ordered by sequence ascending.
	// Used for replay after reconnection.
	GetEventsAfter(ctx context.Context, jobID string, afterSequence int64) ([]*EventEnvelope, error)

	// GetLatestSequence returns the current last_event_sequence for a job.
	// Returns 0 if job has no events.
	GetLatestSequence(ctx context.Context, jobID string) (int64, error)

	// GetEventCount returns the total number of events for a job
	GetEventCount(ctx context.Context, jobID string) (int64, error)
}

// NewEventEnvelope creates a new event envelope with default values
func NewEventEnvelope(jobID, eventType string, payload map[string]interface{}) *EventEnvelope {
	if payload == nil {
		payload = make(map[string]interface{})
	}
	return &EventEnvelope{
		Version:   CurrentEventVersion,
		JobID:     jobID,
		Type:      eventType,
		Timestamp: time.Now(),
		Payload:   payload,
	}
}

// WithNodeID adds node information to the envelope
func (e *EventEnvelope) WithNodeID(nodeID string) *EventEnvelope {
	e.NodeID = nodeID
	return e
}

// WithMessage adds a human-readable message to the envelope
func (e *EventEnvelope) WithMessage(message string) *EventEnvelope {
	e.Message = message
	return e
}

// WithError adds error information to the envelope
func (e *EventEnvelope) WithError(err, code string) *EventEnvelope {
	e.Error = err
	e.Code = code
	return e
}

// WebSocket close codes for diagnostics.
const (
	// CloseNormal indicates normal workflow completion (code 4000)
	CloseNormal = 4000

	// CloseFailed indicates the job failed (code 4001)
	CloseFailed = 4001

	// CloseCancelled indicates the job was cancelled (code 4002)
	CloseCancelled = 4002

	// CloseServerShutdown indicates the server is shutting down (code 4003)
	CloseServerShutdown = 4003

	// CloseReconnectRequired indicates the client should reconnect (code 4004)
	// Used for backpressure when client is too slow
	CloseReconnectRequired = 4004

	// ClosePoolExhausted indicates the admission pool is full (code 4005)
	// The client should retry with backoff
	ClosePoolExhausted = 4005
)

// CloseCodeToReason returns a human-readable reason for a close code
func CloseCodeToReason(code int) string {
	switch code {
	case CloseNormal:
		return "workflow completed"
	case CloseFailed:
		return "workflow failed"
	case CloseCancelled:
		return "workflow cancelled"
	case CloseServerShutdown:
		return "server shutdown"
	case CloseReconnectRequired:
		return "reconnect required"
	case ClosePoolExhausted:
		return "server at capacity"
	default:
		return "unknown"
	}
}

// StatusToCloseCode converts a job status to the appropriate WebSocket close code
func StatusToCloseCode(status string) int {
	switch status {
	case JobStatusCompleted:
		return CloseNormal
	case JobStatusFailed:
		return CloseFailed
	case JobStatusCancelled:
		return CloseCancelled
	default:
		return CloseNormal
	}
}
