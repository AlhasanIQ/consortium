package runtime

import (
	"context"

	"github.com/alhasaniq/consortium/pkg/workflow"
)

// ExecutionEvent is an event emitted during execution for streaming to clients.
// This maps to the existing WebSocket event format for compatibility.
type ExecutionEvent struct {
	Type    string                 `json:"type"`
	Payload map[string]interface{} `json:"payload,omitempty"`
}

// StartParams holds all parameters needed to start a durable execution.
type StartParams struct {
	Identity      *ExecutionIdentity
	Snapshot      *FrozenSnapshot
	Workflow      *workflow.Workflow          // Original workflow for node lookup
	EventCallback func(event *ExecutionEvent) // Streaming callback (existing contract)
	ExecCtx       *workflow.ExecutionContext  // Execution context (tracing, etc.)
	CostTracker   *workflow.CostTracker       // Shared cost tracker
	Replay        *ReplayRequest              // Optional replay plan + seed data for targeted re-execution
}

// ExecutionRuntime is the interface for workflow execution engines.
// The durable runtime is the only implementation in this milestone.
type ExecutionRuntime interface {
	// Start begins execution of a workflow. Blocks until completion, failure, or cancellation.
	Start(ctx context.Context, params *StartParams) error

	// Cancel requests cancellation of a running execution.
	Cancel(ctx context.Context, execID string) error
}
