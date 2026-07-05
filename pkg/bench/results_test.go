package bench

import (
	"testing"
	"time"
)

func TestBuildSummaryFailureReasonCounts(t *testing.T) {
	start := time.Date(2026, time.February, 9, 10, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Second)

	items := []ItemResult{
		{
			ItemID:        "1",
			Subject:       "math",
			Language:      "en",
			Correct:       true,
			ParseOK:       true,
			Attempts:      1,
			OutputSource:  OutputSourceBenchmarkAnswer,
			FailureReason: "",
		},
		{
			ItemID:        "2",
			Subject:       "math",
			Language:      "en",
			Correct:       false,
			ParseOK:       false,
			Attempts:      3,
			OutputSource:  OutputSourceMissingBenchmarkAnswer,
			FailureReason: FailureReasonEmptyFinalOutput,
		},
		{
			ItemID:        "3",
			Subject:       "history",
			Language:      "en",
			Correct:       false,
			ParseOK:       false,
			Attempts:      1,
			OutputSource:  OutputSourceNone,
			FailureReason: FailureReasonProviderFailure,
			Error:         "OpenRouter API error",
		},
	}

	summary := BuildSummary(
		"run-id",
		BenchmarkGlobalMMLULite,
		"test",
		"benchmark-synthesis",
		"Benchmark Synthesis",
		"benchmarks/data/global_mmlu_lite/test.jsonl",
		start,
		end,
		items,
	)

	if summary.TotalItems != 3 {
		t.Fatalf("expected total_items=3, got %d", summary.TotalItems)
	}
	if summary.CorrectItems != 1 {
		t.Fatalf("expected correct_items=1, got %d", summary.CorrectItems)
	}
	if summary.ParsedItems != 1 {
		t.Fatalf("expected parsed_items=1, got %d", summary.ParsedItems)
	}

	if summary.FailureReasonCounts[FailureReasonEmptyFinalOutput] != 1 {
		t.Fatalf("expected empty_final_output count=1, got %d", summary.FailureReasonCounts[FailureReasonEmptyFinalOutput])
	}
	if summary.FailureReasonCounts[FailureReasonProviderFailure] != 1 {
		t.Fatalf("expected provider_failure count=1, got %d", summary.FailureReasonCounts[FailureReasonProviderFailure])
	}
	if summary.FailureReasonCounts[FailureReasonNoLetter] != 0 {
		t.Fatalf("expected no_letter count=0, got %d", summary.FailureReasonCounts[FailureReasonNoLetter])
	}
}

func TestBuildSummaryAdmissionRetryVisibility(t *testing.T) {
	start := time.Date(2026, time.February, 10, 10, 0, 0, 0, time.UTC)
	end := start.Add(5 * time.Second)

	items := []ItemResult{
		{
			ItemID:   "1",
			Subject:  "math",
			Language: "en",
			Correct:  true,
			ParseOK:  true,
			Attempts: 4,
			AttemptDetails: []AttemptDetail{
				{Attempt: 1, FailureReason: FailureReasonToolRuntimeFailure, Error: "admission pool exhausted"},
				{Attempt: 2, FailureReason: FailureReasonToolRuntimeFailure, Error: "admission pool exhausted"},
				{Attempt: 3, FailureReason: FailureReasonToolRuntimeFailure, Error: "admission pool exhausted"},
				{Attempt: 4, ParseOK: true, OutputSource: OutputSourceBenchmarkAnswer},
			},
			OutputSource: OutputSourceBenchmarkAnswer,
		},
		{
			ItemID:   "2",
			Subject:  "math",
			Language: "en",
			Correct:  false,
			ParseOK:  true,
			Attempts: 1,
			AttemptDetails: []AttemptDetail{
				{Attempt: 1, ParseOK: true, OutputSource: OutputSourceBenchmarkAnswer},
			},
			OutputSource: OutputSourceBenchmarkAnswer,
		},
		{
			ItemID:        "3",
			Subject:       "history",
			Language:      "en",
			Correct:       false,
			ParseOK:       false,
			Attempts:      6,
			FailureReason: FailureReasonEmptyFinalOutput,
			AttemptDetails: []AttemptDetail{
				{Attempt: 1, FailureReason: FailureReasonToolRuntimeFailure, Error: "admission pool exhausted"},
				{Attempt: 2, FailureReason: FailureReasonToolRuntimeFailure, Error: "admission pool exhausted"},
				{Attempt: 3, FailureReason: FailureReasonToolRuntimeFailure, Error: "admission pool exhausted"},
				{Attempt: 4, FailureReason: FailureReasonToolRuntimeFailure, Error: "admission pool exhausted"},
				{Attempt: 5, FailureReason: FailureReasonToolRuntimeFailure, Error: "admission pool exhausted"},
				{Attempt: 6, FailureReason: FailureReasonEmptyFinalOutput},
			},
			OutputSource: OutputSourceMissingBenchmarkAnswer,
		},
	}

	summary := BuildSummary(
		"run-id",
		BenchmarkGlobalMMLULite,
		"test",
		"benchmark-synthesis",
		"Benchmark Synthesis",
		"benchmarks/data/global_mmlu_lite/test.jsonl",
		start,
		end,
		items,
	)

	// Final-attempt counts: item 1 succeeded (no failure), item 2 succeeded, item 3 has empty_final_output
	if summary.FailureReasonCounts[FailureReasonToolRuntimeFailure] != 0 {
		t.Fatalf("expected final failure_reason_counts[tool/runtime_failure]=0, got %d",
			summary.FailureReasonCounts[FailureReasonToolRuntimeFailure])
	}
	if summary.FailureReasonCounts[FailureReasonEmptyFinalOutput] != 1 {
		t.Fatalf("expected final failure_reason_counts[empty_final_output]=1, got %d",
			summary.FailureReasonCounts[FailureReasonEmptyFinalOutput])
	}

	// All-attempt counts: 8 tool/runtime_failure across items 1 and 3, 1 empty_final_output
	if summary.AllAttemptFailureCounts[FailureReasonToolRuntimeFailure] != 8 {
		t.Fatalf("expected all_attempt_failure_counts[tool/runtime_failure]=8, got %d",
			summary.AllAttemptFailureCounts[FailureReasonToolRuntimeFailure])
	}
	if summary.AllAttemptFailureCounts[FailureReasonEmptyFinalOutput] != 1 {
		t.Fatalf("expected all_attempt_failure_counts[empty_final_output]=1, got %d",
			summary.AllAttemptFailureCounts[FailureReasonEmptyFinalOutput])
	}

	// Admission retry totals
	if summary.AdmissionRetries != 8 {
		t.Fatalf("expected admission_retries=8, got %d", summary.AdmissionRetries)
	}
	if summary.ItemsWithAdmissionRetries != 2 {
		t.Fatalf("expected items_with_admission_retries=2, got %d", summary.ItemsWithAdmissionRetries)
	}
}

func TestBuildSummaryInitializesAllFailureReasons(t *testing.T) {
	start := time.Date(2026, time.February, 9, 10, 0, 0, 0, time.UTC)
	end := start.Add(1 * time.Second)

	summary := BuildSummary(
		"run-id",
		BenchmarkGlobalMMLULite,
		"test",
		"benchmark-synthesis",
		"Benchmark Synthesis",
		"benchmarks/data/global_mmlu_lite/test.jsonl",
		start,
		end,
		nil,
	)

	for _, reason := range FailureReasonPrecedence() {
		if _, ok := summary.FailureReasonCounts[reason]; !ok {
			t.Fatalf("expected reason %q to be initialized in FailureReasonCounts", reason)
		}
		if _, ok := summary.AllAttemptFailureCounts[reason]; !ok {
			t.Fatalf("expected reason %q to be initialized in AllAttemptFailureCounts", reason)
		}
	}
}
