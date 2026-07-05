package trace

import (
	"context"
	"time"
)

// Span kinds - generic categories, specifics live in Attributes.
const (
	KindNode     = "node"     // Workflow node execution
	KindCall     = "call"     // External call (LLM, tool, aggregation judge)
	KindDecision = "decision" // Branch/evaluation point (condition, cost check)
)

// Span status values.
const (
	StatusOK        = "ok"
	StatusError     = "error"
	StatusTimeout   = "timeout"
	StatusCancelled = "cancelled"
)

// Span represents a single trace span within a node execution.
type Span struct {
	ID           int64          `json:"id,omitempty"`
	JobID        string         `json:"job_id"`
	NodeID       string         `json:"node_id"`
	SpanID       string         `json:"span_id"`
	ParentSpanID string         `json:"parent_span_id,omitempty"`
	Kind         string         `json:"kind"`
	Status       string         `json:"status"`
	StartedAt    time.Time      `json:"started_at"`
	DurationMs   float64        `json:"duration_ms,omitempty"`
	Attributes   map[string]any `json:"attributes"`
	ToolCallID   string         `json:"tool_call_id,omitempty"`
}

// NodeGroup groups trace spans by node_id for API responses.
type NodeGroup struct {
	NodeID string `json:"node_id"`
	Spans  []Span `json:"spans"`
}

// Writer persists trace spans. Implementations must be safe for concurrent use.
type Writer interface {
	WriteSpan(ctx context.Context, span *Span) error
}
