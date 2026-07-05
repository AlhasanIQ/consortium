package admin

import (
	"math"
	"testing"

	"github.com/alhasaniq/consortium/pkg/bench"
	"github.com/alhasaniq/consortium/pkg/storage"
)

func TestLoadExecutionCostSummaries_IncludesAttemptLatencyAcrossDescendants(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	parent := &storage.Job{
		ID:          "latency-parent",
		Description: "parent query",
		Model:       "test-model",
		Status:      "completed",
		TokensTotal: 100,
		Cost:        1.5,
	}
	child := &storage.Job{
		ID:                "latency-child",
		Description:       "child query",
		Model:             "test-model",
		Status:            "completed",
		TokensTotal:       50,
		Cost:              0.7,
		ParentExecutionID: parent.ID,
	}
	if err := server.storage.CreateExecution(parent); err != nil {
		t.Fatalf("CreateExecution(parent): %v", err)
	}
	if err := server.storage.CreateExecution(child); err != nil {
		t.Fatalf("CreateExecution(child): %v", err)
	}
	if _, err := server.db.Exec(`UPDATE jobs SET tokens_total = ?, cost = ? WHERE id = ?`, 100, 1.5, parent.ID); err != nil {
		t.Fatalf("update parent usage: %v", err)
	}
	if _, err := server.db.Exec(`UPDATE jobs SET tokens_total = ?, cost = ? WHERE id = ?`, 50, 0.7, child.ID); err != nil {
		t.Fatalf("update child usage: %v", err)
	}
	if err := server.storage.LogLLMRequestFull(&storage.LLMRequestLog{
		JobID: parent.ID, NodeID: "node-parent", Model: "test-model", Prompt: "p", Response: "o",
		Latency: 120, Status: "completed", AttemptNumber: 1,
	}); err != nil {
		t.Fatalf("LogLLMRequestFull(parent): %v", err)
	}
	if err := server.storage.LogLLMRequestFull(&storage.LLMRequestLog{
		JobID: child.ID, NodeID: "node-child", Model: "test-model", Prompt: "p", Response: "o",
		Latency: 80, Status: "completed", AttemptNumber: 1,
	}); err != nil {
		t.Fatalf("LogLLMRequestFull(child attempt1): %v", err)
	}
	if err := server.storage.LogLLMRequestFull(&storage.LLMRequestLog{
		JobID: child.ID, NodeID: "node-child", Model: "test-model", Prompt: "p", Response: "o",
		Latency: 40, Status: "completed", AttemptNumber: 2,
	}); err != nil {
		t.Fatalf("LogLLMRequestFull(child attempt2): %v", err)
	}

	summaries, err := server.loadExecutionCostSummaries([]string{parent.ID, child.ID})
	if err != nil {
		t.Fatalf("loadExecutionCostSummaries(): %v", err)
	}

	parentSummary := summaries[parent.ID]
	if parentSummary.DirectTokens != 100 || parentSummary.ChildTokens != 50 || parentSummary.TotalTokens != 150 {
		t.Fatalf("unexpected parent token totals: %+v", parentSummary)
	}
	if !almostEqual(parentSummary.DirectCost, 1.5) || !almostEqual(parentSummary.ChildCost, 0.7) || !almostEqual(parentSummary.TotalCost, 2.2) {
		t.Fatalf("unexpected parent cost totals: %+v", parentSummary)
	}
	if !almostEqual(parentSummary.DirectLatencyMs, 120) || !almostEqual(parentSummary.ChildLatencyMs, 120) || !almostEqual(parentSummary.TotalLatencyMs, 240) {
		t.Fatalf("unexpected parent latency totals: %+v", parentSummary)
	}

	childSummary := summaries[child.ID]
	if !almostEqual(childSummary.DirectLatencyMs, 120) || !almostEqual(childSummary.ChildLatencyMs, 0) || !almostEqual(childSummary.TotalLatencyMs, 120) {
		t.Fatalf("unexpected child latency totals: %+v", childSummary)
	}
}

func TestApplyExecutionUsageToBenchmarkItems_EnrichesLatency(t *testing.T) {
	items := []storage.BenchmarkRunItem{
		{
			ItemID:      "item-1",
			JobID:       "job-a",
			LatencyMS:   15,
			TotalTokens: 1,
			CostUSD:     0.1,
		},
		{
			ItemID:      "item-2",
			JobID:       "job-b",
			LatencyMS:   25,
			TotalTokens: 2,
			CostUSD:     0.2,
		},
	}
	usageByJob := map[string]ExecutionCostSummary{
		"job-a": {
			TotalTokens:    10,
			TotalCost:      1.2,
			TotalLatencyMs: 55,
		},
		"job-b": {
			TotalTokens:    20,
			TotalCost:      2.4,
			TotalLatencyMs: 0,
		},
	}

	totalTokens, totalCost, totalLatency := applyExecutionUsageToBenchmarkItems(items, usageByJob)
	if totalTokens != 30 {
		t.Fatalf("totalTokens=%d, want 30", totalTokens)
	}
	if !almostEqual(totalCost, 3.6) {
		t.Fatalf("totalCost=%.6f, want 3.6", totalCost)
	}
	if !almostEqual(totalLatency, 80) {
		t.Fatalf("totalLatency=%.6f, want 80", totalLatency)
	}

	if !almostEqual(items[0].LatencyMS, 55) || items[0].TotalTokens != 10 || !almostEqual(items[0].CostUSD, 1.2) {
		t.Fatalf("unexpected item-1 enrichment: %+v", items[0])
	}
	if !almostEqual(items[1].LatencyMS, 25) || items[1].TotalTokens != 20 || !almostEqual(items[1].CostUSD, 2.4) {
		t.Fatalf("unexpected item-2 enrichment: %+v", items[1])
	}
}

func TestApplyExecutionUsageToRunResults_UpdatesAttemptAndItemLatency(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	parent := &storage.Job{
		ID:          "run-latency-parent",
		Description: "parent query",
		Model:       "test-model",
		Status:      "completed",
		TokensTotal: 10,
		Cost:        0.1,
	}
	child := &storage.Job{
		ID:                "run-latency-child",
		Description:       "child query",
		Model:             "test-model",
		Status:            "completed",
		TokensTotal:       20,
		Cost:              0.2,
		ParentExecutionID: parent.ID,
	}
	if err := server.storage.CreateExecution(parent); err != nil {
		t.Fatalf("CreateExecution(parent): %v", err)
	}
	if err := server.storage.CreateExecution(child); err != nil {
		t.Fatalf("CreateExecution(child): %v", err)
	}
	if _, err := server.db.Exec(`UPDATE jobs SET tokens_total = ?, cost = ? WHERE id = ?`, 10, 0.1, parent.ID); err != nil {
		t.Fatalf("update parent usage: %v", err)
	}
	if _, err := server.db.Exec(`UPDATE jobs SET tokens_total = ?, cost = ? WHERE id = ?`, 20, 0.2, child.ID); err != nil {
		t.Fatalf("update child usage: %v", err)
	}
	if err := server.storage.LogLLMRequestFull(&storage.LLMRequestLog{
		JobID: parent.ID, NodeID: "node-parent", Model: "test-model", Prompt: "p", Response: "o",
		TokensIn: 10, Cost: 0.1, Latency: 100, Status: "completed", AttemptNumber: 1,
	}); err != nil {
		t.Fatalf("LogLLMRequestFull(parent): %v", err)
	}
	if err := server.storage.LogLLMRequestFull(&storage.LLMRequestLog{
		JobID: child.ID, NodeID: "node-child", Model: "test-model", Prompt: "p", Response: "o",
		TokensIn: 20, Cost: 0.2, Latency: 60, Status: "completed", AttemptNumber: 1,
	}); err != nil {
		t.Fatalf("LogLLMRequestFull(child): %v", err)
	}

	run := &benchmarkRunPlan{
		RunID: "test-run",
		ItemResults: []bench.ItemResult{
			{
				ItemID: "row-1",
				AttemptDetails: []bench.AttemptDetail{
					{Attempt: 1, JobID: parent.ID, LatencyMS: 1, TotalTokens: 1, CostUSD: 0.01},
				},
			},
		},
	}

	server.applyExecutionUsageToRunResults(run)
	attempt := run.ItemResults[0].AttemptDetails[0]
	if !almostEqual(attempt.LatencyMS, 160) {
		t.Fatalf("attempt.LatencyMS=%.6f, want 160", attempt.LatencyMS)
	}
	if attempt.TotalTokens != 30 || !almostEqual(attempt.CostUSD, 0.3) {
		t.Fatalf("attempt enrichment mismatch: %+v", attempt)
	}
	if !almostEqual(run.ItemResults[0].LatencyMS, 160) {
		t.Fatalf("item latency mismatch: %+v", run.ItemResults[0])
	}
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}
