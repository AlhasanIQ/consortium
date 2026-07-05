package admin

import (
	"github.com/alhasaniq/consortium/pkg/bench"
	"github.com/alhasaniq/consortium/pkg/storage"
)

// --- Struct conversion helpers (bench <-> storage) ---

func convertAttemptDetailToInput(a bench.AttemptDetail) storage.BenchmarkRunItemAttemptInput {
	return storage.BenchmarkRunItemAttemptInput{
		Attempt:              a.Attempt,
		JobID:                a.JobID,
		LatencyMS:            a.LatencyMS,
		TokensInput:          a.TokensInput,
		TokensOutput:         a.TokensOutput,
		TotalTokens:          a.TotalTokens,
		CostUSD:              a.CostUSD,
		RawOutput:            a.RawOutput,
		Predicted:            a.Predicted,
		ParseOK:              a.ParseOK,
		Error:                a.Error,
		FailureReason:        a.FailureReason,
		OutputSource:         a.OutputSource,
		ContractNodeID:       a.ContractNodeID,
		ContractModel:        a.ContractModel,
		ContractFinishReason: a.ContractFinishReason,
		ContractTokensOutput: a.ContractTokensOutput,
		ContractMaxTokens:    a.ContractMaxTokens,
		ContractDiagnostic:   a.ContractDiagnostic,
	}
}

func convertItemResultToInput(item bench.ItemResult) storage.BenchmarkRunItemInput {
	attempts := make([]storage.BenchmarkRunItemAttemptInput, 0, len(item.AttemptDetails))
	for _, a := range item.AttemptDetails {
		attempts = append(attempts, convertAttemptDetailToInput(a))
	}
	return storage.BenchmarkRunItemInput{
		ItemID:           item.ItemID,
		Subject:          item.Subject,
		Language:         item.Language,
		AnswerLabel:      item.AnswerLabel,
		Predicted:        item.Predicted,
		ParseOK:          item.ParseOK,
		Correct:          item.Correct,
		JobID:            item.JobID,
		LatencyMS:        item.LatencyMS,
		TokensInput:      item.TokensInput,
		TokensOutput:     item.TokensOutput,
		TotalTokens:      item.TotalTokens,
		CostUSD:          item.CostUSD,
		RawOutput:        item.RawOutput,
		Error:            item.Error,
		FailureReason:    item.FailureReason,
		OutputSource:     item.OutputSource,
		WorkflowID:       item.WorkflowID,
		BenchmarkName:    item.BenchmarkName,
		Attempts:         item.Attempts,
		NonLetterRetries: item.NonLetterRetries,
		AttemptDetails:   attempts,
	}
}

// dbAttemptToInput strips the DB-only fields (RunID, ItemID) from a
// storage.BenchmarkRunItemAttempt, yielding the package-agnostic AttemptInput
// that convertAttemptDetailToInput also targets.
func dbAttemptToInput(a storage.BenchmarkRunItemAttempt) storage.BenchmarkRunItemAttemptInput {
	return storage.BenchmarkRunItemAttemptInput{
		Attempt:              a.Attempt,
		JobID:                a.JobID,
		LatencyMS:            a.LatencyMS,
		TokensInput:          a.TokensInput,
		TokensOutput:         a.TokensOutput,
		TotalTokens:          a.TotalTokens,
		CostUSD:              a.CostUSD,
		RawOutput:            a.RawOutput,
		Predicted:            a.Predicted,
		ParseOK:              a.ParseOK,
		Error:                a.Error,
		FailureReason:        a.FailureReason,
		OutputSource:         a.OutputSource,
		ContractNodeID:       a.ContractNodeID,
		ContractModel:        a.ContractModel,
		ContractFinishReason: a.ContractFinishReason,
		ContractTokensOutput: a.ContractTokensOutput,
		ContractMaxTokens:    a.ContractMaxTokens,
		ContractDiagnostic:   a.ContractDiagnostic,
	}
}

// attemptInputToBenchDetail is the reverse of convertAttemptDetailToInput:
// storage.BenchmarkRunItemAttemptInput -> bench.AttemptDetail.
func attemptInputToBenchDetail(a storage.BenchmarkRunItemAttemptInput) bench.AttemptDetail {
	return bench.AttemptDetail{
		Attempt:              a.Attempt,
		JobID:                a.JobID,
		LatencyMS:            a.LatencyMS,
		TokensInput:          a.TokensInput,
		TokensOutput:         a.TokensOutput,
		TotalTokens:          a.TotalTokens,
		CostUSD:              a.CostUSD,
		RawOutput:            a.RawOutput,
		Predicted:            a.Predicted,
		ParseOK:              a.ParseOK,
		Error:                a.Error,
		FailureReason:        a.FailureReason,
		OutputSource:         a.OutputSource,
		ContractNodeID:       a.ContractNodeID,
		ContractModel:        a.ContractModel,
		ContractFinishReason: a.ContractFinishReason,
		ContractTokensOutput: a.ContractTokensOutput,
		ContractMaxTokens:    a.ContractMaxTokens,
		ContractDiagnostic:   a.ContractDiagnostic,
	}
}

func convertDBItemToBenchResult(item storage.BenchmarkRunItem, attempts []storage.BenchmarkRunItemAttempt) bench.ItemResult {
	details := make([]bench.AttemptDetail, 0, len(attempts))
	for _, a := range attempts {
		details = append(details, attemptInputToBenchDetail(dbAttemptToInput(a)))
	}
	return bench.ItemResult{
		ItemID:           item.ItemID,
		Subject:          item.Subject,
		Language:         item.Language,
		AnswerLabel:      item.AnswerLabel,
		Predicted:        item.Predicted,
		ParseOK:          item.ParseOK,
		Correct:          item.Correct,
		JobID:            item.JobID,
		LatencyMS:        item.LatencyMS,
		TokensInput:      item.TokensInput,
		TokensOutput:     item.TokensOutput,
		TotalTokens:      item.TotalTokens,
		CostUSD:          item.CostUSD,
		RawOutput:        item.RawOutput,
		Error:            item.Error,
		FailureReason:    item.FailureReason,
		OutputSource:     item.OutputSource,
		WorkflowID:       item.WorkflowID,
		BenchmarkName:    item.BenchmarkName,
		Attempts:         item.Attempts,
		NonLetterRetries: item.NonLetterRetries,
		AttemptDetails:   details,
	}
}

func convertBenchRunResult(input bench.RunResult) storage.BenchmarkRunResultInput {
	items := make([]storage.BenchmarkRunItemInput, 0, len(input.Items))
	for _, item := range input.Items {
		items = append(items, convertItemResultToInput(item))
	}

	return storage.BenchmarkRunResultInput{
		Summary: storage.BenchmarkRunSummaryInput{
			RunID:                     input.Summary.RunID,
			Benchmark:                 input.Summary.Benchmark,
			Split:                     input.Summary.Split,
			ItemLimit:                 0,
			WorkflowID:                input.Summary.WorkflowID,
			WorkflowName:              input.Summary.WorkflowName,
			DatasetPath:               input.Summary.DatasetPath,
			TotalItems:                input.Summary.TotalItems,
			CompletedItems:            input.Summary.CompletedItems,
			FailedItems:               input.Summary.FailedItems,
			ParsedItems:               input.Summary.ParsedItems,
			CorrectItems:              input.Summary.CorrectItems,
			Accuracy:                  input.Summary.Accuracy,
			ParseRate:                 input.Summary.ParseRate,
			RetriedItems:              input.Summary.RetriedItems,
			TotalAttempts:             input.Summary.TotalAttempts,
			TotalNonLetterRetries:     input.Summary.TotalNonLetterRetries,
			AdmissionRetries:          input.Summary.AdmissionRetries,
			ItemsWithAdmissionRetries: input.Summary.ItemsWithAdmissionRetries,
			FailureReasonCounts:       input.Summary.FailureReasonCounts,
			AllAttemptFailureCounts:   input.Summary.AllAttemptFailureCounts,
			TotalLatencyMS:            input.Summary.TotalLatencyMS,
			AvgLatencyMS:              input.Summary.AvgLatencyMS,
			P50LatencyMS:              input.Summary.P50LatencyMS,
			P95LatencyMS:              input.Summary.P95LatencyMS,
			P99LatencyMS:              input.Summary.P99LatencyMS,
			TotalTokensInput:          input.Summary.TotalTokensInput,
			TotalTokensOutput:         input.Summary.TotalTokensOutput,
			TotalTokens:               input.Summary.TotalTokens,
			AvgTokensPerItem:          input.Summary.AvgTokensPerItem,
			TotalCostUSD:              input.Summary.TotalCostUSD,
			AvgCostUSDPerItem:         input.Summary.AvgCostUSDPerItem,
			StartedAt:                 input.Summary.StartedAt,
			CompletedAt:               input.Summary.CompletedAt,
			ElapsedSeconds:            input.Summary.ElapsedSeconds,
			ExecutionEngine:           input.Summary.ExecutionEngine,
			ExecutionEngineNotes:      input.Summary.ExecutionEngineNotes,
		},
		Items: items,
	}
}

func convertSingleBenchmarkItem(runID string, item bench.ItemResult) *storage.BenchmarkRunItemInput {
	result := convertItemResultToInput(item)
	return &result
}

// convertDBItemsToBenchResults converts storage items + attempts back to
// bench.ItemResult slice suitable for BuildSummary / SaveRunResult.
func convertDBItemsToBenchResults(
	items []storage.BenchmarkRunItem,
	attempts []storage.BenchmarkRunItemAttempt,
) []bench.ItemResult {
	attemptsByItem := make(map[string][]storage.BenchmarkRunItemAttempt, len(items))
	for _, a := range attempts {
		attemptsByItem[a.ItemID] = append(attemptsByItem[a.ItemID], a)
	}

	out := make([]bench.ItemResult, 0, len(items))
	for _, item := range items {
		out = append(out, convertDBItemToBenchResult(item, attemptsByItem[item.ItemID]))
	}
	return out
}
