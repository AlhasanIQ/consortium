package runtime

import (
	"context"
	"errors"
	"strings"
)

// NonRetryableErrors is the set of error patterns that should not trigger retries.
var NonRetryableErrors = []string{
	"COST_LIMIT",
	"INVALID_WORKFLOW",
	"INVALID_CONFIG",
	"INSUFFICIENT_CREDITS",
	"AUTH_ERROR",
	"AUTHENTICATION",
	"AUTHORIZATION",
}

// IsNonRetryable returns true if the error should not be retried.
// Checks against built-in non-retryable patterns and context cancellation.
func IsNonRetryable(err error, errorCode string) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	errStr := ""
	if err != nil {
		errStr = err.Error()
	}

	// Check error code
	for _, pattern := range NonRetryableErrors {
		if strings.EqualFold(errorCode, pattern) {
			return true
		}
	}

	// Check error message for cost limit patterns
	if strings.Contains(errStr, "cost limit") || strings.Contains(errStr, "CostLimit") {
		return true
	}

	return false
}

// IsRetryableCode checks if an error code matches known retryable patterns.
func IsRetryableCode(errorCode string) bool {
	retryable := []string{
		"RATE_LIMIT",
		"RATE_LIMIT_WAIT_DEADLINE",
		"TIMEOUT",
		"TEMPORARY",
		"UNAVAILABLE",
		"CONNECTION",
		"5xx",
		"PROVIDER_NO_CHOICES",
		"OUTPUT_TRUNCATED_EMPTY",
		"CHILD_WORKFLOW_TRANSIENT",
	}
	for _, code := range retryable {
		if strings.EqualFold(errorCode, code) {
			return true
		}
	}
	return false
}
