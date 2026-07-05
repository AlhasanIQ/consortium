package optimize

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---- helpers ----------------------------------------------------------------

func newTestServer(t *testing.T, mux *http.ServeMux) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// ---- HTTPClient error mapping -----------------------------------------------

func TestHTTPClient_ErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantSubstr string
	}{
		{"400 bad request", 400, "invalid input", "http 400"},
		{"404 not found", 404, "not found", "http 404"},
		{"409 conflict", 409, "conflict", "http 409"},
		{"500 server error", 500, "internal error", "http 500"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, tc.body, tc.statusCode)
			})
			srv := newTestServer(t, mux)
			client := NewHTTPClient(srv.URL, 5*time.Second)

			var dst map[string]interface{}
			err := client.GetJSON(context.Background(), "/test", nil, &dst)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantSubstr)
			}
		})
	}
}

func TestHTTPClient_SuccessfulJSON(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/test", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"key": "value"})
	})
	srv := newTestServer(t, mux)
	client := NewHTTPClient(srv.URL, 5*time.Second)

	var dst map[string]string
	if err := client.GetJSON(context.Background(), "/api/test", nil, &dst); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dst["key"] != "value" {
		t.Errorf("got %q, want %q", dst["key"], "value")
	}
}

func TestHTTPClient_MalformedJSON(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/bad", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("not-json"))
	})
	srv := newTestServer(t, mux)
	client := NewHTTPClient(srv.URL, 5*time.Second)

	var dst map[string]interface{}
	err := client.GetJSON(context.Background(), "/api/bad", nil, &dst)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
	if !strings.Contains(err.Error(), "decode response JSON") {
		t.Errorf("error %q does not mention decode: %v", err.Error(), err)
	}
}

// ---- HTTPBenchmarkEvaluator: GetRunSummary ----------------------------------

func TestHTTPBenchmarkEvaluator_GetRunSummary_FullPayload(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/admin/benchmarks/run-abc", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]interface{}{
			"run": map[string]interface{}{
				"accuracy":       0.82,
				"parse_rate":     0.99,
				"total_cost_usd": 1.50,
				"avg_latency_ms": 320.5,
				"p95_latency_ms": 750.0,
				"failed_items":   3,
				"total_items":    100,
			},
			"adjusted_accuracy": 0.80,
			"flagged_items":     2,
		})
	})
	srv := newTestServer(t, mux)

	eval := NewHTTPBenchmarkEvaluator(srv.URL, 5*time.Second)
	eval.PollInterval = 10 * time.Millisecond

	fitness, err := eval.GetRunSummary(context.Background(), "run-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fitness.Accuracy != 0.82 {
		t.Errorf("Accuracy: got %.4f, want 0.82", fitness.Accuracy)
	}
	if fitness.AdjustedAccuracy != 0.80 {
		t.Errorf("AdjustedAccuracy: got %.4f, want 0.80", fitness.AdjustedAccuracy)
	}
	if fitness.ParseRate != 0.99 {
		t.Errorf("ParseRate: got %.4f, want 0.99", fitness.ParseRate)
	}
	if fitness.TotalCost != 1.50 {
		t.Errorf("TotalCost: got %.4f, want 1.50", fitness.TotalCost)
	}
	if fitness.CostPerItem != 0.015 {
		t.Errorf("CostPerItem: got %.6f, want 0.015", fitness.CostPerItem)
	}
	if fitness.AvgLatencyMS != 320.5 {
		t.Errorf("AvgLatencyMS: got %.4f, want 320.5", fitness.AvgLatencyMS)
	}
	if fitness.P95LatencyMS != 750.0 {
		t.Errorf("P95LatencyMS: got %.4f, want 750.0", fitness.P95LatencyMS)
	}
	if fitness.FailedItems != 3 {
		t.Errorf("FailedItems: got %d, want 3", fitness.FailedItems)
	}
	if fitness.TotalItems != 100 {
		t.Errorf("TotalItems: got %d, want 100", fitness.TotalItems)
	}
	if fitness.FlaggedItems != 2 {
		t.Errorf("FlaggedItems: got %d, want 2", fitness.FlaggedItems)
	}
}

func TestHTTPBenchmarkEvaluator_GetRunSummary_MissingRunKey(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/admin/benchmarks/run-missing", func(w http.ResponseWriter, r *http.Request) {
		// Response has no "run" key.
		writeJSON(w, 200, map[string]interface{}{"other": "data"})
	})
	srv := newTestServer(t, mux)

	eval := NewHTTPBenchmarkEvaluator(srv.URL, 5*time.Second)
	_, err := eval.GetRunSummary(context.Background(), "run-missing")
	if err == nil {
		t.Fatal("expected error when run payload is missing")
	}
	if !strings.Contains(err.Error(), "missing run payload") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestHTTPBenchmarkEvaluator_GetRunSummary_AdjustedAccuracyFallback(t *testing.T) {
	// When the top-level adjusted_accuracy is 0, it should fall back to run.accuracy.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/admin/benchmarks/run-fallback", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]interface{}{
			"run": map[string]interface{}{
				"accuracy":     0.75,
				"total_items":  50,
				"failed_items": 0,
			},
			// adjusted_accuracy omitted (defaults to 0)
		})
	})
	srv := newTestServer(t, mux)

	eval := NewHTTPBenchmarkEvaluator(srv.URL, 5*time.Second)
	fitness, err := eval.GetRunSummary(context.Background(), "run-fallback")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should fall back to run.accuracy.
	if fitness.AdjustedAccuracy != 0.75 {
		t.Errorf("AdjustedAccuracy fallback: got %.4f, want 0.75", fitness.AdjustedAccuracy)
	}
}

// ---- HTTPBenchmarkEvaluator: GetFailureCases --------------------------------

func TestHTTPBenchmarkEvaluator_GetFailureCases_ParsesAllFields(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/admin/benchmarks/run-fc/analysis", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]interface{}{
			"items": []interface{}{
				map[string]interface{}{
					"item_id":        "item-1",
					"question":       "What is 2+2?",
					"answer_label":   "4",
					"predicted":      "5",
					"failure_reason": "wrong arithmetic",
					"category":       "wrong_answer",
					"raw_output":     "five",
					"parent_job_id":  "job-p1",
					"child_job_id":   "job-c1",
					"flagged":        true,
					"flag_reason":    "suspicious",
					"agent_answers": []interface{}{
						map[string]interface{}{
							"node_id":  "node-a",
							"model":    "gpt-4",
							"answer":   "5",
							"correct":  false,
							"parse_ok": true,
						},
					},
				},
			},
		})
	})
	srv := newTestServer(t, mux)

	eval := NewHTTPBenchmarkEvaluator(srv.URL, 5*time.Second)
	cases, err := eval.GetFailureCases(context.Background(), "run-fc", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("got %d cases, want 1", len(cases))
	}

	fc := cases[0]
	if fc.ItemID != "item-1" {
		t.Errorf("ItemID: got %q, want %q", fc.ItemID, "item-1")
	}
	if fc.Question != "What is 2+2?" {
		t.Errorf("Question: got %q", fc.Question)
	}
	if fc.CorrectAnswer != "4" {
		t.Errorf("CorrectAnswer: got %q, want %q", fc.CorrectAnswer, "4")
	}
	if fc.Predicted != "5" {
		t.Errorf("Predicted: got %q, want %q", fc.Predicted, "5")
	}
	if fc.FailureReason != "wrong arithmetic" {
		t.Errorf("FailureReason: got %q", fc.FailureReason)
	}
	if fc.RawOutput != "five" {
		t.Errorf("RawOutput: got %q", fc.RawOutput)
	}
	if fc.ParentJobID != "job-p1" {
		t.Errorf("ParentJobID: got %q", fc.ParentJobID)
	}
	if fc.ChildJobID != "job-c1" {
		t.Errorf("ChildJobID: got %q", fc.ChildJobID)
	}
	if !fc.Flagged {
		t.Error("Flagged: want true")
	}
	if fc.FlagReason != "suspicious" {
		t.Errorf("FlagReason: got %q", fc.FlagReason)
	}
	if len(fc.AgentAnswers) != 1 {
		t.Fatalf("AgentAnswers: got %d, want 1", len(fc.AgentAnswers))
	}
	aa := fc.AgentAnswers[0]
	if aa.NodeID != "node-a" {
		t.Errorf("AgentAnswer.NodeID: got %q", aa.NodeID)
	}
	if aa.Model != "gpt-4" {
		t.Errorf("AgentAnswer.Model: got %q", aa.Model)
	}
	if aa.Correct {
		t.Error("AgentAnswer.Correct: want false")
	}
	if !aa.ParseOK {
		t.Error("AgentAnswer.ParseOK: want true")
	}
}

func TestHTTPBenchmarkEvaluator_GetFailureCases_EmptyItems(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/admin/benchmarks/run-empty/analysis", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]interface{}{"items": []interface{}{}})
	})
	srv := newTestServer(t, mux)

	eval := NewHTTPBenchmarkEvaluator(srv.URL, 5*time.Second)
	cases, err := eval.GetFailureCases(context.Background(), "run-empty", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cases) != 0 {
		t.Errorf("got %d cases, want 0", len(cases))
	}
}

func TestHTTPBenchmarkEvaluator_GetFailureCases_LimitEnforced(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/admin/benchmarks/run-limit/analysis", func(w http.ResponseWriter, r *http.Request) {
		items := make([]interface{}, 10)
		for i := range items {
			items[i] = map[string]interface{}{
				"item_id":      "item",
				"question":     "q",
				"answer_label": "a",
				"predicted":    "b",
				"category":     "wrong_answer",
			}
		}
		writeJSON(w, 200, map[string]interface{}{"items": items})
	})
	srv := newTestServer(t, mux)

	eval := NewHTTPBenchmarkEvaluator(srv.URL, 5*time.Second)
	// Pass limit=3; even though server returns 10, client should truncate.
	cases, err := eval.GetFailureCases(context.Background(), "run-limit", 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cases) != 3 {
		t.Errorf("got %d cases, want 3 (limit enforced)", len(cases))
	}
}

// ---- HTTPBenchmarkEvaluator: GetSuccessCases --------------------------------

func TestHTTPBenchmarkEvaluator_GetSuccessCases_FiltersCorrect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/admin/benchmarks/run-sc", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]interface{}{
			"Items": []interface{}{
				map[string]interface{}{
					"ItemID":      "item-1",
					"Question":    "What color is the sky?",
					"AnswerLabel": "blue",
					"Predicted":   "blue",
					"Correct":     true,
				},
				map[string]interface{}{
					"ItemID":      "item-2",
					"Question":    "What is 1+1?",
					"AnswerLabel": "2",
					"Predicted":   "3",
					"Correct":     false, // should be filtered out
				},
			},
		})
	})
	srv := newTestServer(t, mux)

	eval := NewHTTPBenchmarkEvaluator(srv.URL, 5*time.Second)
	cases, err := eval.GetSuccessCases(context.Background(), "run-sc", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("got %d success cases, want 1 (incorrect items filtered)", len(cases))
	}
	if cases[0].ItemID != "item-1" {
		t.Errorf("ItemID: got %q, want %q", cases[0].ItemID, "item-1")
	}
	if cases[0].Question != "What color is the sky?" {
		t.Errorf("Question: got %q", cases[0].Question)
	}
	if cases[0].CorrectAnswer != "blue" {
		t.Errorf("CorrectAnswer: got %q", cases[0].CorrectAnswer)
	}
}

// ---- HTTPBenchmarkEvaluator: ReplayBenchmark --------------------------------

func TestHTTPBenchmarkEvaluator_ReplayBenchmark_ParsesRunID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/admin/benchmarks/base-run-1/replay-items", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, 200, map[string]interface{}{"run_id": "new-run-42"})
	})
	// runnerStatus — returns not running so waitForRunnerSlot proceeds immediately.
	mux.HandleFunc("/api/admin/benchmarks/runner-status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]interface{}{"running": false})
	})
	srv := newTestServer(t, mux)

	eval := NewHTTPBenchmarkEvaluator(srv.URL, 5*time.Second)
	eval.PollInterval = 10 * time.Millisecond

	runID, err := eval.ReplayBenchmark(context.Background(), ReplayBenchmarkRequest{
		BaseRunID:   "base-run-1",
		WorkflowID:  "wf-test",
		Items:       []string{"item-a", "item-b"},
		Mode:        "best_effort",
		Concurrency: 2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if runID != "new-run-42" {
		t.Errorf("runID: got %q, want %q", runID, "new-run-42")
	}
}

func TestHTTPBenchmarkEvaluator_ReplayBenchmark_MissingRunIDInResponse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/admin/benchmarks/base-run-2/replay-items", func(w http.ResponseWriter, r *http.Request) {
		// Response missing run_id.
		writeJSON(w, 200, map[string]interface{}{"status": "ok"})
	})
	mux.HandleFunc("/api/admin/benchmarks/runner-status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]interface{}{"running": false})
	})
	srv := newTestServer(t, mux)

	eval := NewHTTPBenchmarkEvaluator(srv.URL, 5*time.Second)
	eval.PollInterval = 10 * time.Millisecond

	_, err := eval.ReplayBenchmark(context.Background(), ReplayBenchmarkRequest{
		BaseRunID:  "base-run-2",
		WorkflowID: "wf-test",
		Items:      []string{"item-a"},
		Mode:       "best_effort",
	})
	if err == nil {
		t.Fatal("expected error when run_id missing from replay response")
	}
	if !strings.Contains(err.Error(), "missing run_id") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHTTPBenchmarkEvaluator_ReplayBenchmark_ValidationErrors(t *testing.T) {
	srv := newTestServer(t, http.NewServeMux())
	eval := NewHTTPBenchmarkEvaluator(srv.URL, 5*time.Second)

	tests := []struct {
		name       string
		req        ReplayBenchmarkRequest
		wantSubstr string
	}{
		{
			name:       "missing base_run_id",
			req:        ReplayBenchmarkRequest{WorkflowID: "wf", Items: []string{"x"}},
			wantSubstr: "base_run_id is required",
		},
		{
			name:       "missing workflow_id",
			req:        ReplayBenchmarkRequest{BaseRunID: "r1", Items: []string{"x"}},
			wantSubstr: "workflow_id is required",
		},
		{
			name:       "missing items",
			req:        ReplayBenchmarkRequest{BaseRunID: "r1", WorkflowID: "wf"},
			wantSubstr: "at least one replay item is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := eval.ReplayBenchmark(context.Background(), tc.req)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantSubstr)
			}
		})
	}
}

// ---- HTTPPopulationStore: GetRun round-trip ---------------------------------

func TestHTTPPopulationStore_GetRun_RoundTrip(t *testing.T) {
	want := OptimizationRun{
		ID:         "run-123",
		WorkflowID: "wf-abc",
		Benchmark:  "global-mmlu",
		Status:     "running",
		Generation: 3,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/admin/optimize/runs/run-123", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "not GET", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, 200, want)
	})
	srv := newTestServer(t, mux)

	store := NewHTTPPopulationStore(srv.URL, 5*time.Second)
	got, err := store.GetRun(context.Background(), "run-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != want.ID {
		t.Errorf("ID: got %q, want %q", got.ID, want.ID)
	}
	if got.WorkflowID != want.WorkflowID {
		t.Errorf("WorkflowID: got %q, want %q", got.WorkflowID, want.WorkflowID)
	}
	if got.Benchmark != want.Benchmark {
		t.Errorf("Benchmark: got %q, want %q", got.Benchmark, want.Benchmark)
	}
	if got.Status != want.Status {
		t.Errorf("Status: got %q, want %q", got.Status, want.Status)
	}
	if got.Generation != want.Generation {
		t.Errorf("Generation: got %d, want %d", got.Generation, want.Generation)
	}
}

func TestHTTPPopulationStore_GetRun_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/admin/optimize/runs/run-nope", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", 404)
	})
	srv := newTestServer(t, mux)

	store := NewHTTPPopulationStore(srv.URL, 5*time.Second)
	_, err := store.GetRun(context.Background(), "run-nope")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !strings.Contains(err.Error(), "http 404") {
		t.Errorf("expected http 404 error, got: %v", err)
	}
}

// ---- HTTPPopulationStore: ListRuns ------------------------------------------

func TestHTTPPopulationStore_ListRuns_ParsesSlice(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/admin/optimize/runs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "not GET", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, 200, map[string]interface{}{
			"runs": []interface{}{
				map[string]interface{}{"id": "run-1", "status": "running"},
				map[string]interface{}{"id": "run-2", "status": "completed"},
			},
		})
	})
	srv := newTestServer(t, mux)

	store := NewHTTPPopulationStore(srv.URL, 5*time.Second)
	runs, err := store.ListRuns(context.Background(), "running", "", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("got %d runs, want 2", len(runs))
	}
	if runs[0].ID != "run-1" {
		t.Errorf("runs[0].ID: got %q, want %q", runs[0].ID, "run-1")
	}
	if runs[1].Status != "completed" {
		t.Errorf("runs[1].Status: got %q, want %q", runs[1].Status, "completed")
	}
}

// ---- HTTPPopulationStore: GetOrganism round-trip ----------------------------

func TestHTTPPopulationStore_GetOrganism_RoundTrip(t *testing.T) {
	wantOrg := Organism{
		ID:           "org-abc",
		OptRunID:     "run-123",
		Generation:   2,
		MutationType: "prompt",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/admin/optimize/organisms/org-abc", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]interface{}{"organism": wantOrg})
	})
	srv := newTestServer(t, mux)

	store := NewHTTPPopulationStore(srv.URL, 5*time.Second)
	got, err := store.GetOrganism(context.Background(), "org-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != wantOrg.ID {
		t.Errorf("ID: got %q, want %q", got.ID, wantOrg.ID)
	}
	if got.Generation != wantOrg.Generation {
		t.Errorf("Generation: got %d, want %d", got.Generation, wantOrg.Generation)
	}
	if got.MutationType != wantOrg.MutationType {
		t.Errorf("MutationType: got %q, want %q", got.MutationType, wantOrg.MutationType)
	}
}

// ---- HTTPPopulationStore: UpdateRunStatus routing ---------------------------

func TestHTTPPopulationStore_UpdateRunStatus_RoutesCorrectly(t *testing.T) {
	tests := []struct {
		status     string
		wantSuffix string
		wantMethod string
	}{
		{"paused", "/pause", http.MethodPost},
		{"running", "/resume", http.MethodPost},
		{"cancelled", "/cancel", http.MethodPost},
		{"failed", "/status", http.MethodPatch},
	}

	for _, tc := range tests {
		t.Run(tc.status, func(t *testing.T) {
			var gotPath, gotMethod string
			mux := http.NewServeMux()
			mux.HandleFunc("/api/admin/optimize/runs/run-x/", func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotMethod = r.Method
				writeJSON(w, 200, map[string]interface{}{})
			})
			mux.HandleFunc("/api/admin/optimize/runs/run-x", func(w http.ResponseWriter, r *http.Request) {
				// catch untrailed path if needed
				gotPath = r.URL.Path
				gotMethod = r.Method
				writeJSON(w, 200, map[string]interface{}{})
			})
			srv := newTestServer(t, mux)

			store := NewHTTPPopulationStore(srv.URL, 5*time.Second)
			err := store.UpdateRunStatus(context.Background(), "run-x", tc.status)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.HasSuffix(gotPath, tc.wantSuffix) {
				t.Errorf("path %q does not end with %q", gotPath, tc.wantSuffix)
			}
			if gotMethod != tc.wantMethod {
				t.Errorf("method: got %q, want %q", gotMethod, tc.wantMethod)
			}
		})
	}
}

// ---- HTTPPopulationStore: GetParamChanges round-trip ------------------------

func TestHTTPPopulationStore_GetParamChanges_RoundTrip(t *testing.T) {
	wantChanges := []ParamChange{
		{Path: "nodes[n1].data.config.model", OldValue: "gpt-3.5", NewValue: "gpt-4", Reason: "improve accuracy"},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/admin/optimize/organisms/org-1/param-changes", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]interface{}{"changes": wantChanges})
	})
	srv := newTestServer(t, mux)

	store := NewHTTPPopulationStore(srv.URL, 5*time.Second)
	changes, err := store.GetParamChanges(context.Background(), "org-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("got %d changes, want 1", len(changes))
	}
	if changes[0].Path != wantChanges[0].Path {
		t.Errorf("Path: got %q, want %q", changes[0].Path, wantChanges[0].Path)
	}
	if changes[0].NewValue != wantChanges[0].NewValue {
		t.Errorf("NewValue: got %q, want %q", changes[0].NewValue, wantChanges[0].NewValue)
	}
	if changes[0].Reason != wantChanges[0].Reason {
		t.Errorf("Reason: got %q, want %q", changes[0].Reason, wantChanges[0].Reason)
	}
}

// ---- HTTPWorkflowManager ---------------------------------------------------

func TestHTTPWorkflowManager_CreateAndDelete(t *testing.T) {
	var createdID string
	var deletedID string

	mux := http.NewServeMux()
	mux.HandleFunc("/api/workflows", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "not POST", http.StatusMethodNotAllowed)
			return
		}
		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "bad body", 400)
			return
		}
		createdID, _ = payload["id"].(string)
		writeJSON(w, 200, map[string]interface{}{"id": createdID})
	})
	mux.HandleFunc("/api/workflows/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "not DELETE", http.StatusMethodNotAllowed)
			return
		}
		deletedID = strings.TrimPrefix(r.URL.Path, "/api/workflows/")
		writeJSON(w, 200, map[string]interface{}{})
	})
	srv := newTestServer(t, mux)

	mgr := NewHTTPWorkflowManager(srv.URL, 5*time.Second)
	wfJSON := json.RawMessage(`{"nodes":[],"edges":[]}`)
	if err := mgr.CreateTemporaryWorkflow(context.Background(), "wf-tmp-1", wfJSON, "Test WF", "desc"); err != nil {
		t.Fatalf("CreateTemporaryWorkflow: unexpected error: %v", err)
	}
	if createdID != "wf-tmp-1" {
		t.Errorf("created workflow id: got %q, want %q", createdID, "wf-tmp-1")
	}

	if err := mgr.DeleteTemporaryWorkflow(context.Background(), "wf-tmp-1"); err != nil {
		t.Fatalf("DeleteTemporaryWorkflow: unexpected error: %v", err)
	}
	if deletedID != "wf-tmp-1" {
		t.Errorf("deleted workflow id: got %q, want %q", deletedID, "wf-tmp-1")
	}
}

func TestHTTPWorkflowManager_CreateWorkflow_InvalidJSON(t *testing.T) {
	srv := newTestServer(t, http.NewServeMux())
	mgr := NewHTTPWorkflowManager(srv.URL, 5*time.Second)

	err := mgr.CreateTemporaryWorkflow(context.Background(), "wf-bad", json.RawMessage(`not-json`), "x", "")
	if err == nil {
		t.Fatal("expected error for invalid workflow JSON")
	}
	if !strings.Contains(err.Error(), "parse workflow JSON") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHTTPWorkflowManager_Delete_ServerError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/workflows/wf-err", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})
	srv := newTestServer(t, mux)

	mgr := NewHTTPWorkflowManager(srv.URL, 5*time.Second)
	err := mgr.DeleteTemporaryWorkflow(context.Background(), "wf-err")
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
	if !strings.Contains(err.Error(), "http 403") {
		t.Errorf("expected http 403 error, got: %v", err)
	}
}

// ---- extractNodeTracesFromDetail -------------------------------------------

func TestExtractNodeTracesFromDetail_ParsesJobSummaries(t *testing.T) {
	detail := map[string]interface{}{
		"JobSummaries": []interface{}{
			map[string]interface{}{
				"Nodes": []interface{}{
					map[string]interface{}{
						"node_id": "captain",
						"model":   "gpt-4",
						"output":  "The answer is 42",
					},
					map[string]interface{}{
						"node_id": "judge",
						"model":   "claude-3",
						"output":  "I agree with the captain",
					},
				},
			},
		},
	}

	traces := extractNodeTracesFromDetail(detail)
	if len(traces) != 2 {
		t.Fatalf("got %d traces, want 2", len(traces))
	}
	if traces[0].NodeID != "captain" {
		t.Errorf("traces[0].NodeID: got %q, want %q", traces[0].NodeID, "captain")
	}
	if traces[0].Model != "gpt-4" {
		t.Errorf("traces[0].Model: got %q", traces[0].Model)
	}
	if traces[1].NodeID != "judge" {
		t.Errorf("traces[1].NodeID: got %q", traces[1].NodeID)
	}
}

func TestExtractNodeTracesFromDetail_SkipsDuplicateNodes(t *testing.T) {
	detail := map[string]interface{}{
		"JobSummaries": []interface{}{
			map[string]interface{}{
				"Nodes": []interface{}{
					map[string]interface{}{"node_id": "n1", "output": "first"},
				},
			},
			map[string]interface{}{
				"Nodes": []interface{}{
					map[string]interface{}{"node_id": "n1", "output": "duplicate — should be skipped"},
				},
			},
		},
	}

	traces := extractNodeTracesFromDetail(detail)
	if len(traces) != 1 {
		t.Fatalf("got %d traces, want 1 (duplicate node_id skipped)", len(traces))
	}
	if traces[0].Output != "first" {
		t.Errorf("expected first occurrence, got %q", traces[0].Output)
	}
}

func TestExtractNodeTracesFromDetail_TruncatesLongOutput(t *testing.T) {
	longOutput := strings.Repeat("x", 600)
	detail := map[string]interface{}{
		"job_summaries": []interface{}{
			map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{"node_id": "n1", "output": longOutput},
				},
			},
		},
	}

	traces := extractNodeTracesFromDetail(detail)
	if len(traces) != 1 {
		t.Fatalf("got %d traces, want 1", len(traces))
	}
	if len(traces[0].Output) != 500 {
		t.Errorf("output length: got %d, want 500 (truncated)", len(traces[0].Output))
	}
}

func TestExtractNodeTracesFromDetail_SkipsEmptyOutput(t *testing.T) {
	detail := map[string]interface{}{
		"job_summaries": []interface{}{
			map[string]interface{}{
				"nodes": []interface{}{
					map[string]interface{}{"node_id": "n1", "output": "  "},
					map[string]interface{}{"node_id": "n2", "output": "real output"},
				},
			},
		},
	}

	traces := extractNodeTracesFromDetail(detail)
	if len(traces) != 1 {
		t.Fatalf("got %d traces, want 1 (empty output skipped)", len(traces))
	}
	if traces[0].NodeID != "n2" {
		t.Errorf("expected n2, got %q", traces[0].NodeID)
	}
}
