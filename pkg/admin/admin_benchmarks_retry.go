package admin

import (
	"log"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/pkg/bench"
	"github.com/alhasaniq/consortium/pkg/jobs"
	workflowruntime "github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

// --- Retry logic ---

func benchmarkExecutionFailureMessage(execResult *jobs.WorkflowExecutionResult) string {
	if execResult == nil {
		return "workflow execution failed"
	}
	base := strings.TrimSpace(execResult.Error)
	if base == "" {
		base = "workflow execution failed"
	}
	code := strings.ToUpper(strings.TrimSpace(execResult.ErrorCode))
	if code == "" || strings.Contains(strings.ToUpper(base), code) {
		return base
	}
	return code + ": " + base
}

func shouldRetryBenchmarkContractFailure(reason string) bool {
	return reason == bench.FailureReasonEmptyFinalOutput ||
		reason == bench.FailureReasonInvalidContract ||
		reason == bench.FailureReasonContractTruncated
}

func shouldRetryTransientExecutionFailure(reason, errMsg, errorCode string) bool {
	if code := strings.TrimSpace(errorCode); code != "" && workflowruntime.IsRetryableCode(strings.ToUpper(code)) {
		return true
	}
	switch reason {
	case bench.FailureReasonProviderFailure:
		return true
	case bench.FailureReasonToolRuntimeFailure:
		lower := strings.ToLower(strings.TrimSpace(errMsg))
		if lower == "" {
			return false
		}
		transientMarkers := []string{
			"context deadline exceeded",
			"timeout",
			"timed out",
			"failed to read response",
			"stream error",
			"database is locked",
			"sqlite_busy",
			"connection reset",
			"broken pipe",
			"temporarily unavailable",
			"internal_error",
			"eof",
		}
		for _, marker := range transientMarkers {
			if strings.Contains(lower, marker) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func shouldRetryAdmissionExhaustion(execResult *jobs.WorkflowExecutionResult, errMsg string) bool {
	if execResult != nil && strings.EqualFold(strings.TrimSpace(execResult.ErrorCode), "POOL_EXHAUSTED") {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(errMsg))
	return strings.Contains(lower, "admission pool exhausted") || strings.Contains(lower, "server at capacity")
}

func admissionRetryDelay(attempt int) time.Duration {
	const maxDelay = 30 * time.Second
	delay := 250 * time.Millisecond
	for i := 0; i < attempt; i++ {
		delay *= 4
		if delay >= maxDelay {
			return maxDelay
		}
	}
	return delay
}

func transientRetryDelay(retry int) time.Duration {
	delay := 400 * time.Millisecond
	for i := 1; i < retry; i++ {
		delay *= 2
		if delay >= 3*time.Second {
			return 3 * time.Second
		}
	}
	return delay
}

// --- Error classification ---

// classifyBenchmarkFatalCause determines whether an execution error is
// a hard-fatal (immediately pause) or a soft candidate (pause after
// repeated occurrences of the same signature).
func classifyBenchmarkFatalCause(errorCode, failureReason, errMsg string) (benchmarkFatalCause, bool) {
	normalizedCode := strings.ToUpper(strings.TrimSpace(errorCode))
	message := strings.TrimSpace(errMsg)
	if message == "" {
		message = "workflow execution failed"
	}
	if admissionReason, ok := jobs.ClassifyAdmissionPauseReason(normalizedCode, message); ok {
		code := normalizedCode
		if code == "" {
			code = strings.ToUpper(strings.TrimSpace(admissionReason.Code))
		}
		signature := code
		if signature == "" {
			signature = "ADMISSION_PAUSE"
		}
		return benchmarkFatalCause{
			Code:          code,
			Message:       message,
			Reason:        admissionReason.Reason,
			FailureReason: failureReason,
			Signature:     signature,
			Hard:          true,
		}, true
	}

	if reason, ok := hardFatalReasonForCode(normalizedCode); ok {
		return benchmarkFatalCause{
			Code:          normalizedCode,
			Message:       message,
			Reason:        reason,
			FailureReason: failureReason,
			Signature:     normalizedCode,
			Hard:          true,
		}, true
	}
	if inferredCode, reason, ok := hardFatalReasonForMessage(message); ok {
		if normalizedCode == "" {
			normalizedCode = inferredCode
		}
		signature := normalizedCode
		if signature == "" {
			signature = inferredCode
		}
		return benchmarkFatalCause{
			Code:          normalizedCode,
			Message:       message,
			Reason:        reason,
			FailureReason: failureReason,
			Signature:     signature,
			Hard:          true,
		}, true
	}

	if normalizedCode == "" {
		return benchmarkFatalCause{}, false
	}
	if failureReason != bench.FailureReasonProviderFailure && failureReason != bench.FailureReasonToolRuntimeFailure {
		return benchmarkFatalCause{}, false
	}
	if isRetryableExecutionCode(normalizedCode) {
		return benchmarkFatalCause{}, false
	}

	return benchmarkFatalCause{
		Code:          normalizedCode,
		Message:       message,
		Reason:        "repeated_non_retryable_signature",
		FailureReason: failureReason,
		Signature:     benchmarkFailureSignature(normalizedCode, message),
		Hard:          false,
	}, true
}

func hardFatalReasonForCode(code string) (string, bool) {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "ADMISSION_PAUSED":
		return "admission_paused", true
	case "INSUFFICIENT_CREDITS":
		return "insufficient_credits", true
	case "AUTH_ERROR", "AUTHENTICATION", "AUTHORIZATION":
		return "auth_or_access_denied", true
	case "MODEL_NOT_FOUND":
		return "model_not_found", true
	case "BAD_REQUEST", "INVALID_WORKFLOW", "INVALID_CONFIG":
		return "invalid_request", true
	case "COST_LIMIT":
		return "cost_limit_exceeded", true
	default:
		return "", false
	}
}

var fatalMessagePatterns = []struct {
	code    string
	reason  string
	markers []string
}{
	{"ADMISSION_PAUSED", "admission_paused", []string{"admission paused", "admission is paused", "server admission paused"}},
	{"INSUFFICIENT_CREDITS", "insufficient_credits", []string{"insufficient credits", "payment required", "no credits left", "insufficient balance", "quota exhausted"}},
	{"AUTH_ERROR", "auth_or_access_denied", []string{"invalid api key", "authentication failed", "auth error", "unauthorized", "forbidden", "access denied", "not authorized", "account suspended", "account disabled", "country, region, or territory", "unsupported country", "geo restriction", "geographical restriction"}},
	{"MODEL_NOT_FOUND", "model_not_found", []string{"model not found", "no such model"}},
	{"BAD_REQUEST", "invalid_request", []string{"bad request", "invalid request", "invalid workflow", "invalid config", "unsupported parameter"}},
	{"COST_LIMIT", "cost_limit_exceeded", []string{"cost limit"}},
}

func hardFatalReasonForMessage(errMsg string) (code string, reason string, ok bool) {
	lower := strings.ToLower(strings.TrimSpace(errMsg))
	if lower == "" {
		return "", "", false
	}
	for _, p := range fatalMessagePatterns {
		for _, marker := range p.markers {
			if strings.Contains(lower, marker) {
				return p.code, p.reason, true
			}
		}
	}
	return "", "", false
}

func isRetryableExecutionCode(code string) bool {
	normalized := strings.ToUpper(strings.TrimSpace(code))
	if normalized == "" {
		return false
	}
	if normalized == "POOL_EXHAUSTED" {
		return true
	}
	return workflowruntime.IsRetryableCode(normalized)
}

func benchmarkFailureSignature(code, errMsg string) string {
	normalizedCode := strings.ToUpper(strings.TrimSpace(code))
	normalizedMsg := strings.ToLower(strings.TrimSpace(errMsg))
	normalizedMsg = strings.Join(strings.Fields(normalizedMsg), " ")
	if len(normalizedMsg) > 160 {
		normalizedMsg = normalizedMsg[:160]
	}
	if normalizedMsg == "" {
		normalizedMsg = "no_message"
	}
	if normalizedCode == "" {
		normalizedCode = "UNKNOWN"
	}
	return normalizedCode + "|" + normalizedMsg
}

func maybeTriggerBenchmarkFatalGuard(
	guard *benchmarkFatalGuard,
	benchmarkName string,
	workflowID string,
	itemID string,
	attemptDetail bench.AttemptDetail,
	execResult *jobs.WorkflowExecutionResult,
) {
	if guard == nil || guard.isTriggered() {
		return
	}

	errorCode := ""
	if execResult != nil {
		errorCode = strings.TrimSpace(execResult.ErrorCode)
	}
	cause, candidate := classifyBenchmarkFatalCause(errorCode, attemptDetail.FailureReason, attemptDetail.Error)
	if !candidate {
		return
	}

	cause.Benchmark = benchmarkName
	cause.WorkflowID = workflowID
	cause.ItemID = itemID
	cause.JobID = attemptDetail.JobID
	cause.Attempt = attemptDetail.Attempt
	triggered, occurrences := guard.note(cause)
	if triggered {
		if cause.Occurrences == 0 {
			cause.Occurrences = occurrences
		}
		log.Printf(
			"PAUSING benchmark due to fatal execution error code=%s reason=%s signature=%s occurrences=%d benchmark=%s workflow=%s item_id=%s job_id=%s attempt=%d error=%s",
			cause.Code,
			cause.Reason,
			cause.Signature,
			cause.Occurrences,
			cause.Benchmark,
			cause.WorkflowID,
			cause.ItemID,
			cause.JobID,
			cause.Attempt,
			cause.Message,
		)
	}
}
