package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/alhasaniq/consortium/pkg/bench"
	"github.com/alhasaniq/consortium/pkg/workflow"
)

// --- Outcome determination ---

func determineBenchmarkRunOutcome(ctx context.Context, guard *benchmarkFatalGuard, plans []benchmarkRunPlan) benchmarkRunOutcome {
	outcome := benchmarkRunOutcome{
		Status:              "completed",
		FatalGuardTriggered: guard.isTriggered(),
	}

	cancelRequested := errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded)
	outcome.CancelEffective = (cancelRequested || outcome.FatalGuardTriggered) && hasPausedBenchmarkItems(plans)
	if outcome.CancelEffective {
		// Use "failed" for fatal guard triggers — the error field carries
		// the specific cause. DB CHECK constraints only allow known statuses.
		if outcome.FatalGuardTriggered {
			outcome.Status = "failed"
		} else {
			outcome.Status = "cancelled"
		}
	}
	return outcome
}

func benchmarkSessionStatus(runErr error, fatalGuardTriggered, cancelEffective bool) string {
	switch {
	case runErr != nil:
		return "failed"
	case fatalGuardTriggered:
		return "failed"
	case cancelEffective:
		return "cancelled"
	default:
		return "completed"
	}
}

func benchmarkRunError(saveErrors []string) error {
	if len(saveErrors) == 0 {
		return nil
	}
	return fmt.Errorf("%d save/upsert errors: %s", len(saveErrors), strings.Join(saveErrors, "; "))
}

func benchmarkRunnerError(runErr error, guard *benchmarkFatalGuard, outcome benchmarkRunOutcome) string {
	switch {
	case runErr != nil:
		return runErr.Error()
	case outcome.FatalGuardTriggered:
		return guard.snapshotMessage()
	case outcome.CancelEffective:
		return "cancelled by user"
	default:
		return ""
	}
}

func hasPausedBenchmarkItems(plans []benchmarkRunPlan) bool {
	for _, run := range plans {
		for _, item := range run.ItemResults {
			if item.FailureReason == bench.FailureReasonBenchmarkPaused {
				return true
			}
		}
	}
	return false
}

// --- Item result creation and finalization ---

func newBenchmarkItemResult(run *benchmarkRunPlan, item bench.DatasetItem) bench.ItemResult {
	return bench.ItemResult{
		ItemID:        item.ID,
		Subject:       item.Subject,
		Language:      item.Language,
		AnswerLabel:   item.AnswerLabel,
		OutputSource:  bench.OutputSourceNone,
		WorkflowID:    run.WorkflowID,
		BenchmarkName: run.Benchmark,
	}
}

func finalizeBenchmarkAttemptErrorResult(result bench.ItemResult, item bench.DatasetItem, attempt int, errMsg string) bench.ItemResult {
	attemptDetail := bench.AttemptDetail{
		Attempt:      attempt,
		OutputSource: bench.OutputSourceNone,
	}
	classifyAttemptError(&attemptDetail, errMsg)
	result.AttemptDetails = append(result.AttemptDetails, attemptDetail)
	finalizeBenchmarkItemFromAttempt(&result, item, attemptDetail)
	return result
}

func pausedBenchmarkItemResultIfCancelled(
	ctx context.Context,
	guard *benchmarkFatalGuard,
	run *benchmarkRunPlan,
	item bench.DatasetItem,
) (bench.ItemResult, bool) {
	if ctx.Err() == nil {
		return bench.ItemResult{}, false
	}
	msg := "benchmark cancelled"
	if guard != nil && guard.isTriggered() {
		msg = guard.snapshotMessage()
	}
	return pausedBenchmarkItemResult(run.Benchmark, run.WorkflowID, item, msg), true
}

func pausedBenchmarkItemResult(benchmarkName, workflowID string, item bench.DatasetItem, message string) bench.ItemResult {
	msg := strings.TrimSpace(message)
	if msg == "" {
		msg = "benchmark paused"
	}
	result := bench.ItemResult{
		ItemID:        item.ID,
		Subject:       item.Subject,
		Language:      item.Language,
		AnswerLabel:   item.AnswerLabel,
		OutputSource:  bench.OutputSourceNone,
		WorkflowID:    workflowID,
		BenchmarkName: benchmarkName,
	}
	attempt := bench.AttemptDetail{
		Attempt:       1,
		ParseOK:       false,
		Error:         msg,
		FailureReason: bench.FailureReasonBenchmarkPaused,
		OutputSource:  bench.OutputSourceNone,
	}
	result.AttemptDetails = append(result.AttemptDetails, attempt)
	finalizeBenchmarkItemFromAttempt(&result, item, attempt)
	return result
}

func finalizeBenchmarkItemFromAttempt(result *bench.ItemResult, item bench.DatasetItem, attempt bench.AttemptDetail) {
	result.JobID = attempt.JobID
	result.RawOutput = attempt.RawOutput
	result.Predicted = attempt.Predicted
	result.ParseOK = attempt.ParseOK
	result.Error = attempt.Error
	result.FailureReason = attempt.FailureReason
	if attempt.OutputSource == "" {
		result.OutputSource = bench.OutputSourceNone
	} else {
		result.OutputSource = attempt.OutputSource
	}
	correct := bench.IsCorrectPrediction(item, attempt.Predicted, attempt.ParseOK)
	result.Correct = correct
	if n := len(result.AttemptDetails); n > 0 {
		result.AttemptDetails[n-1].Correct = correct
	}
	result.Attempts = len(result.AttemptDetails)
}

func accumulateBenchmarkAttemptMetrics(result *bench.ItemResult, attemptDetail *bench.AttemptDetail, wfResult *workflow.WorkflowResult) {
	if result == nil || attemptDetail == nil || wfResult == nil {
		return
	}
	attemptDetail.LatencyMS = wfResult.TotalLatency
	attemptDetail.TokensInput = wfResult.TotalInputTokens
	attemptDetail.TokensOutput = wfResult.TotalOutputTokens
	attemptDetail.TotalTokens = wfResult.TotalTokens
	attemptDetail.CostUSD = wfResult.TotalCost

	result.LatencyMS += attemptDetail.LatencyMS
	result.TokensInput += attemptDetail.TokensInput
	result.TokensOutput += attemptDetail.TokensOutput
	result.TotalTokens += attemptDetail.TotalTokens
	result.CostUSD += attemptDetail.CostUSD
}

// --- Answer classification ---

// classifyAttemptError applies failure classification to an attempt from an error message.
func classifyAttemptError(attempt *bench.AttemptDetail, errMsg string) {
	attempt.Error = errMsg
	classification := bench.ClassifyFailure(bench.FailureClassificationInput{Err: errMsg})
	attempt.FailureReason = classification.Reason
	attempt.Predicted = classification.Predicted
}

func classifyMathAttemptOutput(canonicalOutput string, canonicalPresent bool) bench.FailureClassification {
	trimmed := strings.TrimSpace(canonicalOutput)
	if !canonicalPresent || trimmed == "" {
		return bench.FailureClassification{Reason: bench.FailureReasonEmptyFinalOutput}
	}

	predicted, ok := bench.ParseMathAnswer(trimmed)
	if !ok {
		return bench.FailureClassification{
			Reason:    bench.FailureReasonInvalidContract,
			Predicted: bench.NormalizeMathAnswer(trimmed),
		}
	}
	return bench.FailureClassification{Predicted: predicted}
}
