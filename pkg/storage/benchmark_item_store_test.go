package storage

import (
	"testing"
)

func TestUpsertBenchmarkRunItem_Basic(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	runID := "run-item-basic"
	ensureBenchmarkRunForTest(t, store, runID)

	item := &BenchmarkRunItemInput{
		ItemID:        "item-1",
		Subject:       "math",
		Language:      "en",
		AnswerLabel:   "A",
		Predicted:     "A",
		ParseOK:       true,
		Correct:       true,
		LatencyMS:     100,
		TokensInput:   10,
		TokensOutput:  5,
		TotalTokens:   15,
		CostUSD:       0.01,
		RawOutput:     "output",
		OutputSource:  "final_output",
		WorkflowID:    "wf",
		BenchmarkName: "bench",
		Attempts:      1,
		AttemptDetails: []BenchmarkRunItemAttemptInput{
			{Attempt: 1, Predicted: "A", ParseOK: true, OutputSource: "final_output"},
		},
	}
	if err := store.UpsertBenchmarkRunItem(runID, item); err != nil {
		t.Fatalf("upsert failed: %v", err)
	}

	// Verify via GetBenchmarkRunItemDetail
	detail, err := store.GetBenchmarkRunItemDetail(runID, "item-1")
	if err != nil {
		t.Fatalf("get detail failed: %v", err)
	}
	if detail.Item.ItemID != "item-1" {
		t.Errorf("expected item_id=item-1, got %s", detail.Item.ItemID)
	}
	if detail.Item.RunID != runID {
		t.Errorf("expected run_id=%s, got %s", runID, detail.Item.RunID)
	}
	if !detail.Item.Correct {
		t.Error("expected correct=true")
	}
	if !detail.Item.ParseOK {
		t.Error("expected parse_ok=true")
	}
	if len(detail.Attempts) != 1 {
		t.Errorf("expected 1 attempt, got %d", len(detail.Attempts))
	}
}

func TestUpsertBenchmarkRunItem_NilIsNoOp(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	if err := store.UpsertBenchmarkRunItem("run-x", nil); err != nil {
		t.Fatalf("expected nil item to be a no-op, got error: %v", err)
	}
}

func TestGetBenchmarkRunItemDetail_NotFound(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	_, err = store.GetBenchmarkRunItemDetail("nonexistent-run", "nonexistent-item")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListBenchmarkRunItems_EmptyRun(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	runID := "run-empty"
	ensureBenchmarkRunForTest(t, store, runID)

	items, err := store.ListBenchmarkRunItems(runID, BenchmarkRunItemFilters{})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

func TestListBenchmarkRunItems_WithFilters(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	runID := "run-filters"
	ensureBenchmarkRunForTest(t, store, runID)

	// Insert a correct and an incorrect item
	correct := &BenchmarkRunItemInput{
		ItemID: "item-correct", Subject: "math", AnswerLabel: "A",
		Predicted: "A", ParseOK: true, Correct: true,
		WorkflowID: "wf", BenchmarkName: "bench", Attempts: 1,
	}
	incorrect := &BenchmarkRunItemInput{
		ItemID: "item-wrong", Subject: "science", AnswerLabel: "B",
		Predicted: "C", ParseOK: true, Correct: false,
		FailureReason: "wrong_answer",
		WorkflowID:    "wf", BenchmarkName: "bench", Attempts: 1,
	}
	if err := store.UpsertBenchmarkRunItem(runID, correct); err != nil {
		t.Fatalf("upsert correct: %v", err)
	}
	if err := store.UpsertBenchmarkRunItem(runID, incorrect); err != nil {
		t.Fatalf("upsert incorrect: %v", err)
	}

	// All items
	all, err := store.ListBenchmarkRunItems(runID, BenchmarkRunItemFilters{})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 items, got %d", len(all))
	}

	// Only incorrect
	incorrectOnly, err := store.ListBenchmarkRunItems(runID, BenchmarkRunItemFilters{OnlyIncorrect: true})
	if err != nil {
		t.Fatalf("list incorrect: %v", err)
	}
	if len(incorrectOnly) != 1 {
		t.Errorf("expected 1 incorrect item, got %d", len(incorrectOnly))
	}
	if incorrectOnly[0].ItemID != "item-wrong" {
		t.Errorf("expected item-wrong, got %s", incorrectOnly[0].ItemID)
	}

	// Filter by subject
	bySubject, err := store.ListBenchmarkRunItems(runID, BenchmarkRunItemFilters{Subject: "math"})
	if err != nil {
		t.Fatalf("list by subject: %v", err)
	}
	if len(bySubject) != 1 {
		t.Errorf("expected 1 math item, got %d", len(bySubject))
	}
}

func TestCountBenchmarkRunItems(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	runID := "run-count"
	ensureBenchmarkRunForTest(t, store, runID)

	for _, id := range []string{"a", "b", "c"} {
		item := &BenchmarkRunItemInput{
			ItemID: id, Subject: "math", AnswerLabel: "A", Predicted: "A",
			ParseOK: true, Correct: true, WorkflowID: "wf", BenchmarkName: "bench", Attempts: 1,
		}
		if err := store.UpsertBenchmarkRunItem(runID, item); err != nil {
			t.Fatalf("upsert %s: %v", id, err)
		}
	}

	count, err := store.CountBenchmarkRunItems(runID, BenchmarkRunItemFilters{})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3, got %d", count)
	}
}

func TestListFailedBenchmarkRunItemIDs(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	runID := "run-failed-ids"
	ensureBenchmarkRunForTest(t, store, runID)

	items := []*BenchmarkRunItemInput{
		{ItemID: "ok", Correct: true, ParseOK: true, WorkflowID: "wf", BenchmarkName: "bench", Attempts: 1},
		{ItemID: "wrong", Correct: false, ParseOK: true, WorkflowID: "wf", BenchmarkName: "bench", Attempts: 1},
		{ItemID: "unparsed", Correct: false, ParseOK: false, WorkflowID: "wf", BenchmarkName: "bench", Attempts: 1},
	}
	for _, it := range items {
		if err := store.UpsertBenchmarkRunItem(runID, it); err != nil {
			t.Fatalf("upsert %s: %v", it.ItemID, err)
		}
	}

	ids, err := store.ListFailedBenchmarkRunItemIDs(runID)
	if err != nil {
		t.Fatalf("list failed ids: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("expected 2 failed ids, got %d: %v", len(ids), ids)
	}
}

func TestBatchUpdateBenchmarkRunItemCosts(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	runID := "run-cost-update"
	ensureBenchmarkRunForTest(t, store, runID)

	item := &BenchmarkRunItemInput{
		ItemID: "item-cost", Correct: true, ParseOK: true,
		TotalTokens: 10, CostUSD: 0.01,
		WorkflowID: "wf", BenchmarkName: "bench", Attempts: 1,
	}
	if err := store.UpsertBenchmarkRunItem(runID, item); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Empty batch is a no-op
	if err := store.BatchUpdateBenchmarkRunItemCosts(nil); err != nil {
		t.Fatalf("empty batch: %v", err)
	}

	updates := []BenchmarkItemCostUpdate{
		{RunID: runID, ItemID: "item-cost", TotalTokens: 100, CostUSD: 0.5},
	}
	if err := store.BatchUpdateBenchmarkRunItemCosts(updates); err != nil {
		t.Fatalf("batch update: %v", err)
	}

	detail, err := store.GetBenchmarkRunItemDetail(runID, "item-cost")
	if err != nil {
		t.Fatalf("get detail: %v", err)
	}
	if detail.Item.TotalTokens != 100 {
		t.Errorf("expected 100 tokens, got %d", detail.Item.TotalTokens)
	}
	if detail.Item.CostUSD != 0.5 {
		t.Errorf("expected 0.5 cost, got %f", detail.Item.CostUSD)
	}
}

func TestGetBenchmarkRunByJobID_EdgeCases(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	// Empty job ID returns nil
	link, err := store.GetBenchmarkRunByJobID("")
	if err != nil {
		t.Fatalf("empty job id: %v", err)
	}
	if link != nil {
		t.Error("expected nil for empty job id")
	}

	// Non-existent job ID returns nil
	link, err = store.GetBenchmarkRunByJobID("nonexistent")
	if err != nil {
		t.Fatalf("nonexistent job: %v", err)
	}
	if link != nil {
		t.Error("expected nil for nonexistent job")
	}

	// Seed a benchmark run with an item that has a job_id
	runID := "run-job-link"
	ensureBenchmarkRunForTest(t, store, runID)
	item := &BenchmarkRunItemInput{
		ItemID: "item-1", JobID: "job-123", Correct: true, ParseOK: true,
		WorkflowID: "wf", BenchmarkName: "bench", Attempts: 1,
	}
	if err := store.UpsertBenchmarkRunItem(runID, item); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	link, err = store.GetBenchmarkRunByJobID("job-123")
	if err != nil {
		t.Fatalf("get by job id: %v", err)
	}
	if link == nil {
		t.Fatal("expected non-nil link")
	}
	if link.RunID != runID {
		t.Errorf("expected run_id=%s, got %s", runID, link.RunID)
	}
	if link.ItemID != "item-1" {
		t.Errorf("expected item_id=item-1, got %s", link.ItemID)
	}
}
