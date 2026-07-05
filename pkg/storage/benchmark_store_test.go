package storage

import (
	"testing"
	"time"
)

func TestUpsertBenchmarkRunItem_ReplacesAttemptsAndSummary(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	runID := "run-replace-item"
	ensureBenchmarkRunForTest(t, store, runID)

	first := &BenchmarkRunItemInput{
		ItemID:        "item-1",
		Subject:       "math",
		Language:      "en",
		AnswerLabel:   "A",
		Predicted:     "A",
		ParseOK:       true,
		Correct:       true,
		LatencyMS:     120,
		TokensInput:   10,
		TokensOutput:  14,
		TotalTokens:   24,
		CostUSD:       0.12,
		RawOutput:     "first",
		OutputSource:  "final_output",
		WorkflowID:    "wf",
		BenchmarkName: "bench",
		Attempts:      2,
		AttemptDetails: []BenchmarkRunItemAttemptInput{
			{Attempt: 1, Predicted: "A", ParseOK: true, OutputSource: "final_output"},
			{Attempt: 2, Predicted: "A", ParseOK: true, OutputSource: "final_output"},
		},
	}
	if err := store.UpsertBenchmarkRunItem(runID, first); err != nil {
		t.Fatalf("first upsert failed: %v", err)
	}

	second := &BenchmarkRunItemInput{
		ItemID:        "item-1",
		Subject:       "math",
		Language:      "en",
		AnswerLabel:   "A",
		Predicted:     "B",
		ParseOK:       false,
		Correct:       false,
		LatencyMS:     50,
		TokensInput:   4,
		TokensOutput:  3,
		TotalTokens:   7,
		CostUSD:       0.03,
		RawOutput:     "second",
		Error:         "bad parse",
		FailureReason: "invalid_contract",
		OutputSource:  "contract",
		WorkflowID:    "wf",
		BenchmarkName: "bench",
		Attempts:      1,
		AttemptDetails: []BenchmarkRunItemAttemptInput{
			{Attempt: 1, Predicted: "B", ParseOK: false, Error: "bad parse", FailureReason: "invalid_contract", OutputSource: "contract"},
		},
	}
	if err := store.UpsertBenchmarkRunItem(runID, second); err != nil {
		t.Fatalf("second upsert failed: %v", err)
	}

	detail, err := store.GetBenchmarkRunItemDetail(runID, "item-1")
	if err != nil {
		t.Fatalf("failed to load benchmark item detail: %v", err)
	}
	if detail.Item.Predicted != "B" || detail.Item.ParseOK || detail.Item.Correct {
		t.Fatalf("expected replacement summary from second upsert, got %+v", detail.Item)
	}
	if detail.Item.TotalTokens != 7 || detail.Item.Attempts != 1 {
		t.Fatalf("expected replacement metrics total_tokens=7 attempts=1, got %+v", detail.Item)
	}
	if len(detail.Attempts) != 1 || detail.Attempts[0].Attempt != 1 {
		t.Fatalf("expected attempts to be replaced with a single attempt #1, got %+v", detail.Attempts)
	}
}

func TestAppendBenchmarkRunItemResult_AppendsAttemptsAndAccumulatesMetrics(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	runID := "run-append-item"
	ensureBenchmarkRunForTest(t, store, runID)

	base := &BenchmarkRunItemInput{
		ItemID:           "item-2",
		Subject:          "science",
		Language:         "en",
		AnswerLabel:      "C",
		Predicted:        "C",
		ParseOK:          true,
		Correct:          true,
		LatencyMS:        200,
		TokensInput:      12,
		TokensOutput:     18,
		TotalTokens:      30,
		CostUSD:          0.2,
		RawOutput:        "base",
		OutputSource:     "final_output",
		WorkflowID:       "wf",
		BenchmarkName:    "bench",
		Attempts:         1,
		NonLetterRetries: 1,
		AttemptDetails:   []BenchmarkRunItemAttemptInput{{Attempt: 1, Predicted: "C", ParseOK: true, OutputSource: "final_output"}},
	}
	if err := store.UpsertBenchmarkRunItem(runID, base); err != nil {
		t.Fatalf("base upsert failed: %v", err)
	}

	appendResult := &BenchmarkRunItemInput{
		ItemID:           "item-2",
		Subject:          "science",
		Language:         "en",
		AnswerLabel:      "C",
		Predicted:        "D",
		ParseOK:          false,
		Correct:          false,
		LatencyMS:        40,
		TokensInput:      3,
		TokensOutput:     5,
		TotalTokens:      8,
		CostUSD:          0.04,
		RawOutput:        "append",
		Error:            "contract parse failed",
		FailureReason:    "invalid_contract",
		OutputSource:     "contract",
		WorkflowID:       "wf",
		BenchmarkName:    "bench",
		Attempts:         1,
		NonLetterRetries: 2,
		AttemptDetails:   []BenchmarkRunItemAttemptInput{{Attempt: 1, Predicted: "D", ParseOK: false, FailureReason: "invalid_contract", OutputSource: "contract"}},
	}
	if err := store.AppendBenchmarkRunItemResult(runID, appendResult); err != nil {
		t.Fatalf("append failed: %v", err)
	}

	detail, err := store.GetBenchmarkRunItemDetail(runID, "item-2")
	if err != nil {
		t.Fatalf("failed to load benchmark item detail: %v", err)
	}
	if detail.Item.Predicted != "D" || detail.Item.ParseOK || detail.Item.Correct {
		t.Fatalf("expected latest result fields from append, got %+v", detail.Item)
	}
	if detail.Item.TotalTokens != 38 {
		t.Fatalf("expected accumulated total_tokens=38, got %+v", detail.Item)
	}
	if detail.Item.CostUSD < 0.2399 || detail.Item.CostUSD > 0.2401 {
		t.Fatalf("expected accumulated cost near 0.24, got %+v", detail.Item)
	}
	if detail.Item.Attempts != 2 || detail.Item.NonLetterRetries != 3 {
		t.Fatalf("expected accumulated attempts/retries 2/3, got %+v", detail.Item)
	}
	if len(detail.Attempts) != 2 || detail.Attempts[0].Attempt != 1 || detail.Attempts[1].Attempt != 2 {
		t.Fatalf("expected appended attempt numbers [1,2], got %+v", detail.Attempts)
	}
}

func TestUpsertBenchmarkRunResult_ReplacesItemRows(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	runID := "run-upsert-result"
	first := BenchmarkRunResultInput{
		Summary: benchmarkSummaryForTest(runID, 1),
		Items: []BenchmarkRunItemInput{
			{
				ItemID:        "item-3",
				Subject:       "history",
				Language:      "en",
				AnswerLabel:   "A",
				Predicted:     "A",
				ParseOK:       true,
				Correct:       true,
				WorkflowID:    "wf",
				BenchmarkName: "bench",
				Attempts:      1,
				AttemptDetails: []BenchmarkRunItemAttemptInput{
					{Attempt: 1, Predicted: "A", ParseOK: true, OutputSource: "final_output"},
				},
			},
		},
	}
	if err := store.UpsertBenchmarkRunResult(&first, BenchmarkRunPersistMeta{
		Status:       "imported",
		Source:       "imported",
		ArtifactPath: "/tmp/first.json",
	}); err != nil {
		t.Fatalf("first run upsert failed: %v", err)
	}

	second := BenchmarkRunResultInput{
		Summary: benchmarkSummaryForTest(runID, 1),
		Items: []BenchmarkRunItemInput{
			{
				ItemID:        "item-3",
				Subject:       "history",
				Language:      "en",
				AnswerLabel:   "A",
				Predicted:     "B",
				ParseOK:       false,
				Correct:       false,
				Error:         "changed",
				FailureReason: "invalid_contract",
				WorkflowID:    "wf",
				BenchmarkName: "bench",
				Attempts:      1,
				AttemptDetails: []BenchmarkRunItemAttemptInput{
					{Attempt: 1, Predicted: "B", ParseOK: false, Error: "changed", FailureReason: "invalid_contract", OutputSource: "contract"},
				},
			},
		},
	}
	if err := store.UpsertBenchmarkRunResult(&second, BenchmarkRunPersistMeta{
		Status:       "imported",
		Source:       "imported",
		ArtifactPath: "/tmp/second.json",
	}); err != nil {
		t.Fatalf("second run upsert failed: %v", err)
	}

	detail, err := store.GetBenchmarkRunItemDetail(runID, "item-3")
	if err != nil {
		t.Fatalf("failed to load benchmark item detail: %v", err)
	}
	if detail.Item.Predicted != "B" || detail.Item.ParseOK || detail.Item.Correct {
		t.Fatalf("expected run-level replacement from second upsert, got %+v", detail.Item)
	}
	if len(detail.Attempts) != 1 || detail.Attempts[0].Predicted != "B" {
		t.Fatalf("expected attempt rows replaced by second payload, got %+v", detail.Attempts)
	}
}

func TestBatchUpdateBenchmarkRunItemCosts_UpdatesSelectedRows(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	runID := "run-batch-cost-update"
	ensureBenchmarkRunForTest(t, store, runID)

	itemA := &BenchmarkRunItemInput{
		ItemID:        "item-a",
		Subject:       "math",
		Language:      "en",
		AnswerLabel:   "A",
		Predicted:     "A",
		ParseOK:       true,
		Correct:       true,
		WorkflowID:    "wf",
		BenchmarkName: "bench",
		Attempts:      1,
	}
	itemB := &BenchmarkRunItemInput{
		ItemID:        "item-b",
		Subject:       "science",
		Language:      "en",
		AnswerLabel:   "B",
		Predicted:     "B",
		ParseOK:       true,
		Correct:       true,
		WorkflowID:    "wf",
		BenchmarkName: "bench",
		Attempts:      1,
	}
	if err := store.UpsertBenchmarkRunItem(runID, itemA); err != nil {
		t.Fatalf("failed to insert item-a: %v", err)
	}
	if err := store.UpsertBenchmarkRunItem(runID, itemB); err != nil {
		t.Fatalf("failed to insert item-b: %v", err)
	}

	updates := []BenchmarkItemCostUpdate{
		{
			RunID:       runID,
			ItemID:      "item-a",
			TotalTokens: 321,
			CostUSD:     0.456,
		},
	}
	if err := store.BatchUpdateBenchmarkRunItemCosts(updates); err != nil {
		t.Fatalf("batch update failed: %v", err)
	}

	detailA, err := store.GetBenchmarkRunItemDetail(runID, "item-a")
	if err != nil {
		t.Fatalf("failed to load item-a detail: %v", err)
	}
	if detailA.Item.TotalTokens != 321 {
		t.Fatalf("expected item-a tokens 321, got %d", detailA.Item.TotalTokens)
	}
	if detailA.Item.CostUSD < 0.4559 || detailA.Item.CostUSD > 0.4561 {
		t.Fatalf("expected item-a cost near 0.456, got %f", detailA.Item.CostUSD)
	}

	detailB, err := store.GetBenchmarkRunItemDetail(runID, "item-b")
	if err != nil {
		t.Fatalf("failed to load item-b detail: %v", err)
	}
	if detailB.Item.TotalTokens != 0 {
		t.Fatalf("expected item-b tokens unchanged at 0, got %d", detailB.Item.TotalTokens)
	}
	if detailB.Item.CostUSD != 0 {
		t.Fatalf("expected item-b cost unchanged at 0, got %f", detailB.Item.CostUSD)
	}
}

func TestGetBenchmarkRunByJobID(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	runID := "run-reverse-lookup"
	ensureBenchmarkRunForTest(t, store, runID)

	// Seed an item with a job_id.
	item := &BenchmarkRunItemInput{
		ItemID:        "item-1",
		Subject:       "math",
		AnswerLabel:   "A",
		Predicted:     "A",
		ParseOK:       true,
		Correct:       true,
		JobID:         "job-abc",
		WorkflowID:    "wf",
		BenchmarkName: "bench",
		Attempts:      1,
	}
	if err := store.UpsertBenchmarkRunItem(runID, item); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	// Direct match.
	link, err := store.GetBenchmarkRunByJobID("job-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if link == nil {
		t.Fatal("expected link, got nil")
	}
	if link.RunID != runID {
		t.Fatalf("expected run_id %s, got %s", runID, link.RunID)
	}
	if link.Benchmark != "bench" {
		t.Fatalf("expected benchmark 'bench', got %s", link.Benchmark)
	}
	if link.ItemID != "item-1" {
		t.Fatalf("expected item_id 'item-1', got %s", link.ItemID)
	}

	// No match.
	link, err = store.GetBenchmarkRunByJobID("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if link != nil {
		t.Fatalf("expected nil, got %+v", link)
	}

	// Empty string.
	link, err = store.GetBenchmarkRunByJobID("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if link != nil {
		t.Fatalf("expected nil for empty string, got %+v", link)
	}
}

func ensureBenchmarkRunForTest(t *testing.T, store *Storage, runID string) {
	t.Helper()
	if err := store.EnsureBenchmarkRunExists(runID, "bench", "dev", "wf", "workflow", "dataset.json", 1, 1, "manual", "", ""); err != nil {
		t.Fatalf("failed to seed benchmark run: %v", err)
	}
}

func benchmarkSummaryForTest(runID string, totalItems int) BenchmarkRunSummaryInput {
	now := time.Now().UTC()
	return BenchmarkRunSummaryInput{
		RunID:           runID,
		Benchmark:       "bench",
		Split:           "dev",
		WorkflowID:      "wf",
		WorkflowName:    "workflow",
		DatasetPath:     "dataset.json",
		TotalItems:      totalItems,
		CompletedItems:  totalItems,
		FailedItems:     0,
		ParsedItems:     totalItems,
		CorrectItems:    totalItems,
		Accuracy:        1,
		ParseRate:       1,
		StartedAt:       now,
		CompletedAt:     now,
		ElapsedSeconds:  0.01,
		ExecutionEngine: "backend",
	}
}
