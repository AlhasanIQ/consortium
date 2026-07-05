// Package api provides HTTP and WebSocket API handlers for the Consortium platform.
package api

import (
	"github.com/alhasaniq/consortium/pkg/events"
)

// Re-export event type constants for convenience within the api package.
// These are defined in pkg/events for shared use across packages.
const (
	EventStatus       = events.EventStatus
	EventJobCreated   = events.EventJobCreated
	EventNodeStart    = events.EventNodeStart
	EventNodeComplete = events.EventNodeComplete
	EventNodeFailed   = events.EventNodeFailed
	EventComplete     = events.EventComplete
	EventError        = events.EventError
	EventCancelled    = events.EventCancelled

	EventAgentPlanCreated        = events.EventAgentPlanCreated
	EventAgentIterationStarted   = events.EventAgentIterationStarted
	EventAgentToolCalled         = events.EventAgentToolCalled
	EventAgentToolResult         = events.EventAgentToolResult
	EventAgentIterationCompleted = events.EventAgentIterationCompleted
	EventAgentBranchCreated      = events.EventAgentBranchCreated
	EventAgentBranchPruned       = events.EventAgentBranchPruned
	EventAgentBranchSelected     = events.EventAgentBranchSelected
	EventAgentTerminated         = events.EventAgentTerminated
	EventAgentFailed             = events.EventAgentFailed

	EventMemoryRead          = events.EventMemoryRead
	EventMemoryWrite         = events.EventMemoryWrite
	EventRetrievalExecuted   = events.EventRetrievalExecuted
	EventRetrievalResultUsed = events.EventRetrievalResultUsed
)

// Re-export job status constants for convenience.
const (
	JobStatusPending   = events.JobStatusPending
	JobStatusRunning   = events.JobStatusRunning
	JobStatusPaused    = events.JobStatusPaused
	JobStatusCompleted = events.JobStatusCompleted
	JobStatusFailed    = events.JobStatusFailed
	JobStatusCancelled = events.JobStatusCancelled
)

// Re-export helper functions.
var (
	IsTerminalStatus = events.IsTerminalStatus
	IsTerminalEvent  = events.IsTerminalEvent
)
