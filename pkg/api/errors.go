// Package api provides HTTP and WebSocket API handlers for the Consortium platform.
package api

// Error code constants - machine-readable error codes for structured error handling.
// These codes are used in API error responses and WebSocket error events.
const (
	// Validation errors
	ErrCodeInvalidWorkflow = "INVALID_WORKFLOW" // Workflow validation failed
	ErrCodeCycleDetected   = "CYCLE_DETECTED"   // Workflow contains circular dependencies
	ErrCodeInvalidModel    = "INVALID_MODEL"    // Unknown or unavailable model
	ErrCodeInvalidJSON     = "INVALID_JSON"     // Invalid JSON payload

	// Execution errors
	ErrCodeExecutionFailed  = "EXECUTION_FAILED"  // General execution failure
	ErrCodeNodeFailed       = "NODE_FAILED"       // Node execution failed
	ErrCodeExecutionTimeout = "EXECUTION_TIMEOUT" // Execution exceeded time limit
	// Cost/token limit codes: use workflow.ErrCodeCostLimitExceeded, workflow.ErrCodeTokenLimitExceeded

	// Cancellation
	ErrCodeCancelled = "CANCELLED" // Cancelled by user request

	// Job errors
	ErrCodeJobNotFound     = "JOB_NOT_FOUND"    // Job ID not found
	ErrCodeJobNotRunning   = "JOB_NOT_RUNNING"  // Cannot cancel non-running job
	ErrCodeJobNotPending   = "JOB_NOT_PENDING"  // Job is not in pending state
	ErrCodeJobNotPaused    = "JOB_NOT_PAUSED"   // Job is not in paused state
	ErrCodePoolExhausted   = "POOL_EXHAUSTED"   // Server at concurrency/backlog capacity
	ErrCodeAdmissionPaused = "ADMISSION_PAUSED" // Admission paused due to systemic terminal failure

	// Connection errors
	ErrCodeConnectionLost    = "CONNECTION_LOST"    // WebSocket connection lost
	ErrCodeConnectionTimeout = "CONNECTION_TIMEOUT" // Connection timed out

	// Provider errors: use providers.ErrCode* for these codes
	// (ErrCodeInsufficientCredits, ErrCodeRateLimited, ErrCodeUpstreamError, ErrCodeUpstreamTimeout)

	// Internal errors
	ErrCodeInternalError = "INTERNAL_ERROR" // Internal server error
	ErrCodeDatabaseError = "DATABASE_ERROR" // Database operation failed
)

// APIError represents a standardized error response for API endpoints.
// This is the canonical error format for all HTTP error responses.
type APIError struct {
	// Error is a human-readable error message
	Error string `json:"error"`

	// Code is a machine-readable error code (one of ErrCode* constants)
	Code string `json:"code"`

	// Details provides additional context about the error
	Details interface{} `json:"details,omitempty"`

	// NodeID identifies the node where the error occurred (for node-related errors)
	NodeID string `json:"node_id,omitempty"`
}

// NewAPIError creates a new API error with the given message and code.
func NewAPIError(message, code string) *APIError {
	return &APIError{
		Error: message,
		Code:  code,
	}
}

// WithDetails adds details to an API error.
func (e *APIError) WithDetails(details interface{}) *APIError {
	e.Details = details
	return e
}

// WithNodeID adds a node ID to an API error.
func (e *APIError) WithNodeID(nodeID string) *APIError {
	e.NodeID = nodeID
	return e
}
