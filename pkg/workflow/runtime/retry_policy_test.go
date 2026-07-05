package runtime

import (
	"context"
	"fmt"
	"testing"
)

func TestIsNonRetryable(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		errorCode string
		want      bool
	}{
		// Context errors
		{
			name: "context canceled",
			err:  context.Canceled,
			want: true,
		},
		{
			name: "context deadline exceeded",
			err:  context.DeadlineExceeded,
			want: true,
		},
		{
			name: "wrapped context canceled",
			err:  fmt.Errorf("operation failed: %w", context.Canceled),
			want: true,
		},

		// Non-retryable error codes
		{
			name:      "COST_LIMIT code",
			err:       fmt.Errorf("some error"),
			errorCode: "COST_LIMIT",
			want:      true,
		},
		{
			name:      "cost_limit lowercase",
			err:       fmt.Errorf("some error"),
			errorCode: "cost_limit",
			want:      true,
		},
		{
			name:      "INVALID_WORKFLOW code",
			err:       fmt.Errorf("some error"),
			errorCode: "INVALID_WORKFLOW",
			want:      true,
		},
		{
			name:      "AUTH_ERROR code",
			err:       fmt.Errorf("some error"),
			errorCode: "AUTH_ERROR",
			want:      true,
		},
		{
			name:      "AUTHENTICATION code",
			err:       fmt.Errorf("some error"),
			errorCode: "AUTHENTICATION",
			want:      true,
		},
		{
			name:      "AUTHORIZATION code",
			err:       fmt.Errorf("some error"),
			errorCode: "AUTHORIZATION",
			want:      true,
		},
		{
			name:      "INVALID_CONFIG code",
			err:       fmt.Errorf("some error"),
			errorCode: "INVALID_CONFIG",
			want:      true,
		},
		{
			name:      "INSUFFICIENT_CREDITS code",
			err:       fmt.Errorf("some error"),
			errorCode: "INSUFFICIENT_CREDITS",
			want:      true,
		},

		// Error message patterns
		{
			name: "cost limit in message",
			err:  fmt.Errorf("exceeded cost limit for this job"),
			want: true,
		},
		{
			name: "CostLimit in message",
			err:  fmt.Errorf("CostLimit reached"),
			want: true,
		},

		// Retryable cases
		{
			name:      "retryable error",
			err:       fmt.Errorf("connection reset"),
			errorCode: "TIMEOUT",
			want:      false,
		},
		{
			name:      "no code, generic error",
			err:       fmt.Errorf("something went wrong"),
			errorCode: "",
			want:      false,
		},
		{
			name:      "nil error with empty code",
			err:       nil,
			errorCode: "",
			want:      false,
		},
		{
			name:      "unknown code",
			err:       fmt.Errorf("error"),
			errorCode: "UNKNOWN_CODE",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsNonRetryable(tt.err, tt.errorCode)
			if got != tt.want {
				t.Errorf("IsNonRetryable(%v, %q) = %v, want %v", tt.err, tt.errorCode, got, tt.want)
			}
		})
	}
}

func TestIsRetryableCode(t *testing.T) {
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
		if !IsRetryableCode(code) {
			t.Errorf("IsRetryableCode(%q) = false, want true", code)
		}
	}

	// Case-insensitive
	if !IsRetryableCode("rate_limit") {
		t.Error("IsRetryableCode should be case-insensitive")
	}
	if !IsRetryableCode("Timeout") {
		t.Error("IsRetryableCode should be case-insensitive")
	}

	// Non-retryable codes
	nonRetryable := []string{
		"COST_LIMIT",
		"AUTH_ERROR",
		"INVALID_WORKFLOW",
		"",
		"UNKNOWN",
	}
	for _, code := range nonRetryable {
		if IsRetryableCode(code) {
			t.Errorf("IsRetryableCode(%q) = true, want false", code)
		}
	}
}
