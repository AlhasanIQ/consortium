package workflow

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

const (
	RetryCodeRateLimitWaitDeadline  = "RATE_LIMIT_WAIT_DEADLINE"
	RetryCodeProviderNoChoices      = "PROVIDER_NO_CHOICES"
	RetryCodeOutputTruncatedEmpty   = "OUTPUT_TRUNCATED_EMPTY"
	RetryCodeChildWorkflowTransient = "CHILD_WORKFLOW_TRANSIENT"
	RetryCodeTimeout                = "TIMEOUT"
	RetryCodeAggParseFailure        = "AGG_PARSE_FAILURE"
)

// AdaptiveReasoningPolicy controls progressive reasoning-effort fallback when
// retrying repeated truncation failures.
type AdaptiveReasoningPolicy struct {
	TriggerErrorCodes        []string `json:"trigger_error_codes,omitempty"`
	ActivateAfterConsecutive int      `json:"activate_after_consecutive,omitempty"`
	Ladder                   []string `json:"ladder,omitempty"` // Ordered high->...->none
}

// RetryPolicy defines retry behavior for workflow nodes
type RetryPolicy struct {
	MaxAttempts       int                      `json:"max_attempts"`                 // Maximum number of attempts (including first try)
	BackoffMs         int                      `json:"backoff_ms"`                   // Initial backoff in milliseconds
	BackoffMultiply   float64                  `json:"backoff_multiply,omitempty"`   // Multiplier for exponential backoff (default 2.0)
	MaxBackoffMs      int                      `json:"max_backoff_ms,omitempty"`     // Maximum backoff in milliseconds
	RetryableErrors   []string                 `json:"retryable_errors,omitempty"`   // Error codes/patterns to retry. Nil (omitted) = DefaultRetryPolicy list. Empty slice = retry all retryable errors.
	AdaptiveReasoning *AdaptiveReasoningPolicy `json:"adaptive_reasoning,omitempty"` // Optional progressive reasoning downgrade policy
}

// defaultRetryableErrors is the cached default retryable error pattern list,
// avoiding a fresh allocation on every ShouldRetry call.
var defaultRetryableErrors = []string{
	"RATE_LIMIT",
	RetryCodeRateLimitWaitDeadline,
	RetryCodeTimeout,
	"TEMPORARY",
	"UNAVAILABLE",
	"CONNECTION",
	"5xx",
	RetryCodeProviderNoChoices,
	RetryCodeOutputTruncatedEmpty,
	RetryCodeChildWorkflowTransient,
	RetryCodeAggParseFailure,
}

// DefaultRetryPolicy returns the default retry policy
func DefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxAttempts:     5,
		BackoffMs:       1000,
		BackoffMultiply: 2.0,
		MaxBackoffMs:    30000,
		RetryableErrors: slices.Clone(defaultRetryableErrors),
		AdaptiveReasoning: &AdaptiveReasoningPolicy{
			TriggerErrorCodes:        []string{RetryCodeOutputTruncatedEmpty, RetryCodeTimeout},
			ActivateAfterConsecutive: 2,
			Ladder:                   []string{"high", "medium", "low", "none"},
		},
	}
}

// NoRetryPolicy returns a policy that never retries
func NoRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		MaxAttempts: 1,
	}
}

// Clone returns a deep copy of the retry policy, preserving nil receivers.
func (p *RetryPolicy) Clone() *RetryPolicy {
	if p == nil {
		return nil
	}
	cloned := *p
	if p.RetryableErrors != nil {
		cloned.RetryableErrors = slices.Clone(p.RetryableErrors)
	}
	if p.AdaptiveReasoning != nil {
		adaptive := *p.AdaptiveReasoning
		if p.AdaptiveReasoning.TriggerErrorCodes != nil {
			adaptive.TriggerErrorCodes = slices.Clone(p.AdaptiveReasoning.TriggerErrorCodes)
		}
		if p.AdaptiveReasoning.Ladder != nil {
			adaptive.Ladder = slices.Clone(p.AdaptiveReasoning.Ladder)
		}
		cloned.AdaptiveReasoning = &adaptive
	}
	return &cloned
}

// ShouldRetry determines if an error should be retried based on the policy.
//
// Non-retryable errors (always returns false):
//   - CostLimitError: budget exceeded — retrying would spend more money
//   - Explicitly marked non-retryable errors (NewNonRetryableError)
//   - context.Canceled: parent context cancelled (user abort / shutdown)
//
// Note: context.DeadlineExceeded is NOT checked here. The caller must check
// ctx.Err() on the parent context first. If the parent is alive, a
// DeadlineExceeded came from the node's own per-attempt timeout and IS retryable.
func (p *RetryPolicy) ShouldRetry(err error, attempt int) bool {
	if p == nil || p.MaxAttempts <= 1 {
		return false
	}

	if attempt >= p.MaxAttempts {
		return false
	}

	if err == nil {
		return false
	}

	// CostLimitError is never retryable — retrying spends more against exceeded budget
	if IsCostLimitError(err) {
		return false
	}

	// Explicitly marked non-retryable errors
	if !IsRetryable(err) {
		return false
	}

	// Parent context cancelled — user abort or shutdown
	if errors.Is(err, context.Canceled) {
		return false
	}

	patterns := p.RetryableErrors

	// Omitted retryable_errors inherits the default curated retry list.
	if patterns == nil {
		patterns = defaultRetryableErrors
	}

	// Explicit empty retryable_errors means retry all remaining errors.
	if len(patterns) == 0 {
		return true
	}

	errStr := strings.ToLower(err.Error())
	errCode := strings.ToLower(strings.TrimSpace(GetErrorCode(err)))

	// Check if error matches any retryable patterns
	for _, pattern := range patterns {
		pattern = strings.ToLower(pattern)
		if errCode != "" && errCode == pattern {
			return true
		}
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// IsAdaptiveReasoningTrigger reports whether an error code should advance the
// adaptive reasoning fallback ladder.
func (p *RetryPolicy) IsAdaptiveReasoningTrigger(errorCode string) bool {
	cfg := p.AdaptiveReasoningConfig()
	if cfg == nil {
		return false
	}
	code := strings.TrimSpace(errorCode)
	if code == "" {
		return false
	}
	for _, trigger := range cfg.TriggerErrorCodes {
		if strings.EqualFold(strings.TrimSpace(trigger), code) {
			return true
		}
	}
	return false
}

// AdaptiveReasoningConfig returns a sanitized adaptive reasoning config or nil
// when disabled.
func (p *RetryPolicy) AdaptiveReasoningConfig() *AdaptiveReasoningPolicy {
	if p == nil || p.AdaptiveReasoning == nil {
		return nil
	}
	cfg := *p.AdaptiveReasoning

	if cfg.ActivateAfterConsecutive < 1 {
		cfg.ActivateAfterConsecutive = 2
	}
	if len(cfg.TriggerErrorCodes) == 0 {
		cfg.TriggerErrorCodes = []string{RetryCodeOutputTruncatedEmpty, RetryCodeTimeout}
	}

	ladder := make([]string, 0, len(cfg.Ladder))
	for _, effort := range cfg.Ladder {
		effort = strings.ToLower(strings.TrimSpace(effort))
		switch effort {
		case "high", "medium", "low", "none":
			if !slices.Contains(ladder, effort) {
				ladder = append(ladder, effort)
			}
		}
	}
	if len(ladder) == 0 {
		ladder = []string{"high", "medium", "low", "none"}
	}
	cfg.Ladder = ladder
	return &cfg
}

// AdaptiveReasoningEffort resolves the effort override for the next attempt
// based on consecutive trigger failures. The returned bool is false when no
// override should be applied.
func (p *RetryPolicy) AdaptiveReasoningEffort(baseEffort string, consecutiveTriggerFailures int) (string, bool) {
	cfg := p.AdaptiveReasoningConfig()
	if cfg == nil || consecutiveTriggerFailures < cfg.ActivateAfterConsecutive {
		return "", false
	}

	base := strings.ToLower(strings.TrimSpace(baseEffort))
	if base == "" {
		base = "none"
	}

	ladder := cfg.Ladder
	baseIdx := 0
	for i := range ladder {
		if ladder[i] == base {
			baseIdx = i
			break
		}
	}

	shift := consecutiveTriggerFailures - cfg.ActivateAfterConsecutive + 1
	if shift < 1 {
		shift = 1
	}
	targetIdx := baseIdx + shift
	if targetIdx >= len(ladder) {
		targetIdx = len(ladder) - 1
	}
	if targetIdx <= baseIdx {
		return "", false
	}
	return ladder[targetIdx], true
}

// GetBackoffDuration calculates the backoff duration for a given attempt
func (p *RetryPolicy) GetBackoffDuration(attempt int) time.Duration {
	if p == nil || p.BackoffMs <= 0 {
		return 0
	}

	multiplier := p.BackoffMultiply
	if multiplier <= 0 {
		multiplier = 2.0
	}

	// Calculate exponential backoff with early exit to prevent overflow
	maxBackoff := p.MaxBackoffMs
	backoffMs := float64(p.BackoffMs)
	for i := 1; i < attempt; i++ {
		backoffMs *= multiplier
		// Break early when we've already hit the cap to avoid float64 overflow
		if maxBackoff > 0 && backoffMs >= float64(maxBackoff) {
			backoffMs = float64(maxBackoff)
			break
		}
	}

	// Apply max backoff limit
	if maxBackoff > 0 && backoffMs > float64(maxBackoff) {
		backoffMs = float64(maxBackoff)
	}

	// Safety clamp: when MaxBackoffMs is 0 (unlimited), cap at MaxInt32 to
	// prevent overflow when converting to time.Duration.
	if backoffMs > float64(math.MaxInt32) {
		backoffMs = float64(math.MaxInt32)
	}

	return time.Duration(backoffMs) * time.Millisecond
}

// NodeExecutionID generates a deterministic execution ID for a node attempt
// Format: {job_id}:{node_id}:{attempt}
func NodeExecutionID(jobID, nodeID string, attempt int) string {
	return fmt.Sprintf("%s:%s:%d", jobID, nodeID, attempt)
}

// RetryableError wraps an error with retry information
type RetryableError struct {
	Err       error
	Retryable bool
	Code      string
}

func (e *RetryableError) Error() string {
	return e.Err.Error()
}

func (e *RetryableError) Unwrap() error {
	return e.Err
}

// NewRetryableError creates a new retryable error
func NewRetryableError(err error, code string) *RetryableError {
	return &RetryableError{
		Err:       err,
		Retryable: true,
		Code:      code,
	}
}

// NewNonRetryableError creates a new non-retryable error
func NewNonRetryableError(err error, code string) *RetryableError {
	return &RetryableError{
		Err:       err,
		Retryable: false,
		Code:      code,
	}
}

// IsRetryable checks if an error is explicitly marked as retryable
func IsRetryable(err error) bool {
	var re *RetryableError
	if errors.As(err, &re) {
		return re.Retryable
	}
	// Default to retryable for unknown errors (policy will further filter)
	return true
}

// GetErrorCode extracts the error code from a RetryableError
func GetErrorCode(err error) string {
	var re *RetryableError
	if errors.As(err, &re) {
		return re.Code
	}
	return ""
}
