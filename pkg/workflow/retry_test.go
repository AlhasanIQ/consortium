package workflow

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"
)

func TestShouldRetry_CostLimitError(t *testing.T) {
	policy := DefaultRetryPolicy()
	err := NewCostLimitError("cost", 0.5, 0.6, "node_1")

	if policy.ShouldRetry(err, 1) {
		t.Error("CostLimitError should never be retryable — retrying spends more money")
	}
}

func TestShouldRetry_NonRetryableError(t *testing.T) {
	policy := DefaultRetryPolicy()
	err := NewNonRetryableError(fmt.Errorf("permanent failure"), "PERMANENT")

	if policy.ShouldRetry(err, 1) {
		t.Error("Explicitly non-retryable errors should not be retried")
	}
}

func TestShouldRetry_ContextCanceled(t *testing.T) {
	policy := DefaultRetryPolicy()

	if policy.ShouldRetry(context.Canceled, 1) {
		t.Error("context.Canceled should not be retried (user abort)")
	}
}

func TestShouldRetry_TransientPatterns(t *testing.T) {
	policy := DefaultRetryPolicy()

	tests := []struct {
		name    string
		errMsg  string
		retryOK bool
	}{
		{"rate_limit", "RATE_LIMIT exceeded", true},
		{"timeout", "request TIMEOUT after 30s", true},
		{"5xx error", "server returned 5xx", true},
		{"temporary", "TEMPORARY network issue", true},
		{"unavailable", "service UNAVAILABLE", true},
		{"connection error", "CONNECTION refused", true},
		{"unknown error", "something completely unexpected", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := fmt.Errorf("%s", tt.errMsg)
			got := policy.ShouldRetry(err, 1)
			if got != tt.retryOK {
				t.Errorf("ShouldRetry(%q) = %v, want %v", tt.errMsg, got, tt.retryOK)
			}
		})
	}
}

func TestShouldRetry_MatchesRetryableErrorCode(t *testing.T) {
	policy := &RetryPolicy{
		MaxAttempts:     4,
		RetryableErrors: []string{RetryCodeOutputTruncatedEmpty},
	}
	err := NewRetryableError(fmt.Errorf("output truncated"), RetryCodeOutputTruncatedEmpty)
	if !policy.ShouldRetry(err, 1) {
		t.Fatalf("expected retry for matching retryable error code")
	}
}

func TestShouldRetry_AggParseFailureRetried(t *testing.T) {
	policy := DefaultRetryPolicy()
	err := NewRetryableError(fmt.Errorf("judge aggregation failed: could not parse winner"), RetryCodeAggParseFailure)
	if !policy.ShouldRetry(err, 1) {
		t.Fatal("expected AGG_PARSE_FAILURE to be retried by default policy")
	}
}

func TestAdaptiveReasoningEffortProgression(t *testing.T) {
	policy := DefaultRetryPolicy()
	base := "high"

	if _, ok := policy.AdaptiveReasoningEffort(base, 1); ok {
		t.Fatalf("expected no adaptive override before activation threshold")
	}

	effort, ok := policy.AdaptiveReasoningEffort(base, 2)
	if !ok || effort != "medium" {
		t.Fatalf("expected medium at second consecutive trigger, got effort=%q ok=%v", effort, ok)
	}
	effort, ok = policy.AdaptiveReasoningEffort(base, 3)
	if !ok || effort != "low" {
		t.Fatalf("expected low at third consecutive trigger, got effort=%q ok=%v", effort, ok)
	}
	effort, ok = policy.AdaptiveReasoningEffort(base, 10)
	if !ok || effort != "none" {
		t.Fatalf("expected none at high consecutive trigger count, got effort=%q ok=%v", effort, ok)
	}
}

func TestShouldRetry_NilPolicy(t *testing.T) {
	var policy *RetryPolicy
	err := fmt.Errorf("some error")

	if policy.ShouldRetry(err, 1) {
		t.Error("nil policy should never retry")
	}
}

func TestShouldRetry_MaxAttemptsReached(t *testing.T) {
	policy := &RetryPolicy{MaxAttempts: 3}
	err := fmt.Errorf("TIMEOUT")

	if policy.ShouldRetry(err, 3) {
		t.Error("should not retry when attempt == MaxAttempts")
	}
	if policy.ShouldRetry(err, 4) {
		t.Error("should not retry when attempt > MaxAttempts")
	}
}

func TestShouldRetry_NoRetryPolicy(t *testing.T) {
	policy := NoRetryPolicy()
	err := fmt.Errorf("TIMEOUT")

	if policy.ShouldRetry(err, 1) {
		t.Error("NoRetryPolicy should never retry")
	}
}

func TestShouldRetry_EmptyRetryableErrors(t *testing.T) {
	// If RetryableErrors is empty, all non-excluded errors should be retried
	policy := &RetryPolicy{
		MaxAttempts:     3,
		RetryableErrors: []string{},
	}

	err := fmt.Errorf("any random error")
	if !policy.ShouldRetry(err, 1) {
		t.Error("empty RetryableErrors list should retry all errors")
	}
}

func TestShouldRetry_NilRetryableErrorsUsesDefaultList(t *testing.T) {
	policy := &RetryPolicy{MaxAttempts: 3}

	if !policy.ShouldRetry(fmt.Errorf("TIMEOUT"), 1) {
		t.Error("nil RetryableErrors should inherit default patterns")
	}

	if policy.ShouldRetry(fmt.Errorf("totally custom error"), 1) {
		t.Error("nil RetryableErrors should not retry errors outside default patterns")
	}
}

func TestRetryPolicyCloneDeepCopiesMutableConfiguration(t *testing.T) {
	original := &RetryPolicy{
		MaxAttempts:     4,
		RetryableErrors: []string{"TIMEOUT"},
		AdaptiveReasoning: &AdaptiveReasoningPolicy{
			TriggerErrorCodes:        []string{RetryCodeTimeout},
			ActivateAfterConsecutive: 2,
			Ladder:                   []string{"high", "none"},
		},
	}

	clone := original.Clone()
	if clone == nil {
		t.Fatal("Clone returned nil for a non-nil policy")
	}
	clone.RetryableErrors[0] = "CONNECTION"
	clone.AdaptiveReasoning.TriggerErrorCodes[0] = RetryCodeAggParseFailure
	clone.AdaptiveReasoning.Ladder[0] = "low"

	if original.RetryableErrors[0] != "TIMEOUT" {
		t.Fatalf("RetryableErrors alias changed original: %+v", original.RetryableErrors)
	}
	if original.AdaptiveReasoning.TriggerErrorCodes[0] != RetryCodeTimeout {
		t.Fatalf("TriggerErrorCodes alias changed original: %+v", original.AdaptiveReasoning.TriggerErrorCodes)
	}
	if original.AdaptiveReasoning.Ladder[0] != "high" {
		t.Fatalf("Ladder alias changed original: %+v", original.AdaptiveReasoning.Ladder)
	}
}

func TestRetryPolicyClonePreservesNilReceiver(t *testing.T) {
	var policy *RetryPolicy
	if clone := policy.Clone(); clone != nil {
		t.Fatalf("nil policy Clone() = %+v, want nil", clone)
	}
}

func TestGetBackoffDuration(t *testing.T) {
	policy := &RetryPolicy{
		BackoffMs:       1000,
		BackoffMultiply: 2.0,
		MaxBackoffMs:    10000,
	}

	// Attempt 1: 1000ms
	d1 := policy.GetBackoffDuration(1)
	if d1.Milliseconds() != 1000 {
		t.Errorf("attempt 1 backoff = %dms, want 1000ms", d1.Milliseconds())
	}

	// Attempt 2: 2000ms
	d2 := policy.GetBackoffDuration(2)
	if d2.Milliseconds() != 2000 {
		t.Errorf("attempt 2 backoff = %dms, want 2000ms", d2.Milliseconds())
	}

	// Attempt 3: 4000ms
	d3 := policy.GetBackoffDuration(3)
	if d3.Milliseconds() != 4000 {
		t.Errorf("attempt 3 backoff = %dms, want 4000ms", d3.Milliseconds())
	}

	// Attempt 5: would be 16000ms but capped at 10000ms
	d5 := policy.GetBackoffDuration(5)
	if d5.Milliseconds() != 10000 {
		t.Errorf("attempt 5 backoff = %dms, want 10000ms (capped)", d5.Milliseconds())
	}
}

func TestNodeExecutionID(t *testing.T) {
	id := NodeExecutionID("job-123", "node-1", 2)
	if id != "job-123:node-1:2" {
		t.Errorf("NodeExecutionID = %q, want %q", id, "job-123:node-1:2")
	}

}

func TestGetBackoffDuration_OverflowProtection(t *testing.T) {
	t.Run("ExtremAttempt_NoMaxBackoff", func(t *testing.T) {
		policy := &RetryPolicy{
			BackoffMs:       1000,
			BackoffMultiply: 2.0,
			MaxBackoffMs:    0, // unlimited
		}

		d := policy.GetBackoffDuration(1000)

		// Must not be negative (overflow) and must not panic
		if d < 0 {
			t.Fatalf("backoff duration overflowed to negative: %v", d)
		}

		// Should be capped at math.MaxInt32 milliseconds
		maxDuration := time.Duration(math.MaxInt32) * time.Millisecond
		if d != maxDuration {
			t.Errorf("expected duration capped at MaxInt32 ms (%v), got %v", maxDuration, d)
		}
	})

	t.Run("HighAttempt_WithMaxBackoff", func(t *testing.T) {
		policy := &RetryPolicy{
			BackoffMs:       1000,
			BackoffMultiply: 2.0,
			MaxBackoffMs:    30000,
		}

		d := policy.GetBackoffDuration(100)

		if d.Milliseconds() != 30000 {
			t.Errorf("expected backoff capped at MaxBackoffMs=30000ms, got %dms", d.Milliseconds())
		}
	})
}
