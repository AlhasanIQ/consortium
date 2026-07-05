package workflow

import (
	"errors"
	"fmt"
)

// Error codes for cost limit violations
const (
	ErrCodeCostLimitExceeded        = "COST_LIMIT_EXCEEDED"
	ErrCodeTokenLimitExceeded       = "TOKEN_LIMIT_EXCEEDED"
	ErrCodeInputTokenLimitExceeded  = "INPUT_TOKEN_LIMIT_EXCEEDED"
	ErrCodeOutputTokenLimitExceeded = "OUTPUT_TOKEN_LIMIT_EXCEEDED"
)

// CostLimitError represents a cost or token limit violation during workflow execution
type CostLimitError struct {
	Code         string  // Error code (ErrCodeCostLimitExceeded, etc.)
	LimitType    string  // Human-readable limit type: "cost", "tokens", "input_tokens", "output_tokens"
	LimitValue   float64 // The configured limit
	CurrentValue float64 // Value when limit was exceeded
	NodeID       string  // Node where limit was exceeded
	Message      string  // Detailed error message
}

func (e *CostLimitError) Error() string {
	return e.Message
}

// NewCostLimitError creates a new cost limit exceeded error
func NewCostLimitError(limitType string, limit, current float64, nodeID string) *CostLimitError {
	var code, msg string

	switch limitType {
	case "cost":
		code = ErrCodeCostLimitExceeded
		msg = fmt.Sprintf("cost limit exceeded: $%.6f of $%.6f budget used at node %s",
			current, limit, nodeID)
	case "tokens":
		code = ErrCodeTokenLimitExceeded
		msg = fmt.Sprintf("token limit exceeded: %d of %d tokens used at node %s",
			int(current), int(limit), nodeID)
	case "input_tokens":
		code = ErrCodeInputTokenLimitExceeded
		msg = fmt.Sprintf("input token limit exceeded: %d of %d tokens used at node %s",
			int(current), int(limit), nodeID)
	case "output_tokens":
		code = ErrCodeOutputTokenLimitExceeded
		msg = fmt.Sprintf("output token limit exceeded: %d of %d tokens used at node %s",
			int(current), int(limit), nodeID)
	default:
		code = ErrCodeCostLimitExceeded
		msg = fmt.Sprintf("limit exceeded: %.2f of %.2f at node %s", current, limit, nodeID)
	}

	return &CostLimitError{
		Code:         code,
		LimitType:    limitType,
		LimitValue:   limit,
		CurrentValue: current,
		NodeID:       nodeID,
		Message:      msg,
	}
}

// IsCostLimitError checks if an error is a cost limit error.
func IsCostLimitError(err error) bool {
	var e *CostLimitError
	return errors.As(err, &e)
}

// AsCostLimitError attempts to extract a CostLimitError from an error.
func AsCostLimitError(err error) (*CostLimitError, bool) {
	var e *CostLimitError
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}
