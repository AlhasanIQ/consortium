// Package events defines canonical event types and job statuses used throughout
// the Consortium platform. This package is imported by both api and jobs packages
// to ensure consistent event handling.
package events

// Event type taxonomy:
//   - Workflow lifecycle: status, job_created, complete, error, cancelled, paused
//   - Node lifecycle:     node_start, node_complete, node_failed (node-level; distinct from error which is workflow-level)
//   - Node retry:         node_retry_backoff, node_retry_start, node_retry_exhausted
//   - Agent lifecycle:    agent_plan_created, agent_iteration_*, agent_tool_*, agent_branch_*, agent_terminated, agent_failed
//   - Memory/retrieval:   memory_read, memory_write, retrieval_executed, retrieval_result_used
//
// These are the ONLY event types that should be emitted over WebSocket connections.
const (
	EventStatus     = "status"
	EventJobCreated = "job_created"

	EventNodeStart          = "node_start"
	EventNodeComplete       = "node_complete"
	EventNodeFailed         = "node_failed" // node-level failure; use EventError for workflow-level errors
	EventNodeRetryBackoff   = "node_retry_backoff"
	EventNodeRetryStart     = "node_retry_start"
	EventNodeRetryExhausted = "node_retry_exhausted"

	EventComplete  = "complete"
	EventError     = "error" // workflow-level error, not node-specific
	EventCancelled = "cancelled"
	EventPaused    = "paused"

	EventAgentPlanCreated        = "agent_plan_created"
	EventAgentIterationStarted   = "agent_iteration_started"
	EventAgentToolCalled         = "agent_tool_called"
	EventAgentToolResult         = "agent_tool_result"
	EventAgentIterationCompleted = "agent_iteration_completed"
	EventAgentBranchCreated      = "agent_branch_created"
	EventAgentBranchPruned       = "agent_branch_pruned"
	EventAgentBranchSelected     = "agent_branch_selected"
	EventAgentTerminated         = "agent_terminated"
	EventAgentFailed             = "agent_failed"

	EventMemoryRead          = "memory_read"
	EventMemoryWrite         = "memory_write"
	EventRetrievalExecuted   = "retrieval_executed"
	EventRetrievalResultUsed = "retrieval_result_used"
)

// Job status constants — the ONLY status values that jobs can have.
const (
	JobStatusPending   = "pending"
	JobStatusRunning   = "running"
	JobStatusCompleted = "completed"
	JobStatusFailed    = "failed"
	JobStatusCancelled = "cancelled"
	JobStatusPaused    = "paused"
)

// IsTerminalStatus returns true if the status is a terminal state (completed, failed, or cancelled)
func IsTerminalStatus(status string) bool {
	return status == JobStatusCompleted || status == JobStatusFailed || status == JobStatusCancelled
}

// IsTerminalEvent returns true if the event type indicates workflow termination
func IsTerminalEvent(eventType string) bool {
	return eventType == EventComplete || eventType == EventError || eventType == EventCancelled
}
