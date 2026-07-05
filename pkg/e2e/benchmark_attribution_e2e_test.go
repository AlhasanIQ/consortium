package e2e_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/storage"
	workflowruntime "github.com/alhasaniq/consortium/pkg/workflow/runtime"
	"github.com/google/uuid"
)

var e2eBenchmarkWrapperIDs = []string{
	"benchmark-informed-captain-synthesis-cheap",
	"benchmark-judge-pick-cheap",
	"benchmark-judge-score-pick-cheap",
	"benchmark-peer-score-pick-cheap",
	"benchmark-majority-pick-cheap",
	"benchmark-self-consistency-majority-pick-cheap",
	"benchmark-camp-split-judge-pick-cheap",
	"benchmark-adversarial-defense-judge-pick-cheap",
	"benchmark-multi-round-majority-pick-cheap",
}

var e2eBaselineBenchmarkIDs = []string{
	"benchmark-baseline-deepseek-v4-flash-cheap",
	"benchmark-baseline-mimo-cheap",
	"benchmark-baseline-minimax-cheap",
	"benchmark-math-baseline-deepseek-v4-flash-cheap",
	"benchmark-math-baseline-mimo-cheap",
	"benchmark-math-baseline-minimax-cheap",
}

func TestE2EBenchmarkAttributionForL3Wrappers(t *testing.T) {
	app := newE2EApp(t)
	for _, workflowID := range e2eBenchmarkWrapperIDs {
		t.Run(workflowID, func(t *testing.T) {
			parentJobID := submitStoredWorkflowAndWait(t, app, workflowID, e2eBenchmarkInput())
			children := requireChildExecutions(t, app, parentJobID)
			childFrozen := loadFrozenDAG(t, app, children[0].ID)
			requireFrozenContainsAnyAggregationSource(t, childFrozen)

			runID := seedBenchmarkResultForJob(t, app, workflowID, parentJobID, "A")
			analysis := benchmarkAnalysis(t, app, runID)
			performance := objectField(analysis, "performance")
			if got := numberField(performance, "items_with_child_data"); got < 1 {
				t.Fatalf("items_with_child_data = %v, want >= 1: %v", got, analysis)
			}
			if got := len(arrayField(performance, "agent_models")); got == 0 {
				t.Fatalf("agent_models empty for %s: %v", workflowID, analysis)
			}
			if got := len(arrayField(performance, "aggregation_nodes")); got == 0 {
				t.Fatalf("aggregation_nodes empty for %s: %v", workflowID, analysis)
			}
		})
	}
}

func TestE2EBenchmarkAttributionForDirectBaselines(t *testing.T) {
	app := newE2EApp(t)
	for _, workflowID := range e2eBaselineBenchmarkIDs {
		t.Run(workflowID, func(t *testing.T) {
			parentJobID := submitStoredWorkflowAndWait(t, app, workflowID, e2eBenchmarkInput())
			requireNoChildExecutions(t, app, parentJobID)

			runID := seedBenchmarkResultForJob(t, app, workflowID, parentJobID, "A")
			analysis := benchmarkAnalysis(t, app, runID)
			performance := objectField(analysis, "performance")
			if got := len(arrayField(performance, "agent_models")); got == 0 {
				t.Fatalf("baseline agent_models empty for %s: %v", workflowID, analysis)
			}
			if got := len(arrayField(performance, "aggregation_nodes")); got != 0 {
				t.Fatalf("baseline aggregation_nodes len = %d, want 0 for benchmark packaging-only collect: %v", got, analysis)
			}
		})
	}
}

func seedBenchmarkResultForJob(t *testing.T, app *e2eApp, workflowID, jobID, predicted string) string {
	t.Helper()
	// This deterministic e2e validates benchmark analysis attribution over the
	// real workflow node rows produced above. The live conctl smoke script covers
	// the benchmark runner's item-field preservation path.
	runID := "bench-e2e-" + uuid.NewString()
	datasetPath := writeBenchmarkDatasetFixture(t)
	started := time.Now().Add(-time.Second).UTC()
	completed := time.Now().UTC()
	result := &storage.BenchmarkRunResultInput{
		Summary: storage.BenchmarkRunSummaryInput{
			RunID:               runID,
			Benchmark:           "global-mmlu-lite",
			Split:               "dev",
			ItemLimit:           1,
			WorkflowID:          workflowID,
			WorkflowName:        workflowID,
			DatasetPath:         datasetPath,
			TotalItems:          1,
			CompletedItems:      1,
			ParsedItems:         1,
			CorrectItems:        1,
			Accuracy:            1,
			ParseRate:           1,
			TotalAttempts:       1,
			TotalLatencyMS:      10,
			AvgLatencyMS:        10,
			P50LatencyMS:        10,
			P95LatencyMS:        10,
			P99LatencyMS:        10,
			TotalTokensInput:    10,
			TotalTokensOutput:   10,
			TotalTokens:         20,
			AvgTokensPerItem:    20,
			TotalCostUSD:        0.001,
			StartedAt:           started,
			CompletedAt:         completed,
			ElapsedSeconds:      completed.Sub(started).Seconds(),
			ExecutionEngine:     "e2e",
			FailureReasonCounts: map[string]int{},
		},
		Items: []storage.BenchmarkRunItemInput{{
			ItemID:        "row-1",
			Subject:       "e2e",
			Language:      "en",
			AnswerLabel:   "A",
			Predicted:     predicted,
			ParseOK:       true,
			Correct:       true,
			JobID:         jobID,
			LatencyMS:     10,
			TokensInput:   10,
			TokensOutput:  10,
			TotalTokens:   20,
			CostUSD:       0.001,
			RawOutput:     predicted,
			OutputSource:  "parent",
			WorkflowID:    workflowID,
			BenchmarkName: "global-mmlu-lite",
			Attempts:      1,
			AttemptDetails: []storage.BenchmarkRunItemAttemptInput{{
				Attempt:      1,
				JobID:        jobID,
				ParseOK:      true,
				Predicted:    predicted,
				OutputSource: "parent",
			}},
		}},
	}
	if err := app.store.UpsertBenchmarkRunResult(result, storage.BenchmarkRunPersistMeta{
		Status: "completed",
		Source: "e2e",
	}); err != nil {
		t.Fatalf("UpsertBenchmarkRunResult: %v", err)
	}
	return runID
}

func writeBenchmarkDatasetFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "global-mmlu-lite.jsonl")
	var dataset bytes.Buffer
	row := map[string]interface{}{
		"id":           "row-1",
		"benchmark":    "global-mmlu-lite",
		"split":        "dev",
		"subject":      "e2e",
		"language":     "en",
		"question":     "Which option is correct?",
		"choices":      []string{"Alpha", "Beta", "Gamma", "Delta"},
		"answer_label": "A",
	}
	if err := json.NewEncoder(&dataset).Encode(row); err != nil {
		t.Fatalf("encode dataset row: %v", err)
	}
	if err := os.WriteFile(path, dataset.Bytes(), 0o600); err != nil {
		t.Fatalf("write dataset fixture: %v", err)
	}
	return path
}

func benchmarkAnalysis(t *testing.T, app *e2eApp, runID string) map[string]interface{} {
	t.Helper()
	rec := app.request(http.MethodGet, "/api/admin/benchmarks/"+runID+"/analysis?top=5", nil)
	requireStatus(t, rec, http.StatusOK)
	return decodeResponse(t, rec)
}

func requireFrozenContainsAnyAggregationSource(t *testing.T, frozen workflowruntime.CanonicalWorkflow) {
	t.Helper()
	for _, node := range frozen.Nodes {
		if source := metadataString(node.Metadata, "source_workflow_id"); strings.HasPrefix(source, "aggregation-") {
			return
		}
	}
	t.Fatalf("frozen DAG missing compiled aggregation source metadata: %v", frozenNodeIDs(frozen))
}

func objectField(body map[string]interface{}, key string) map[string]interface{} {
	value, _ := body[key].(map[string]interface{})
	return value
}

func arrayField(body map[string]interface{}, key string) []interface{} {
	values, _ := body[key].([]interface{})
	return values
}

func numberField(body map[string]interface{}, key string) float64 {
	value, _ := body[key].(float64)
	return value
}
