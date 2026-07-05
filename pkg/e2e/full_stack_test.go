package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/admin"
	"github.com/alhasaniq/consortium/pkg/api"
	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/jobs"
	"github.com/alhasaniq/consortium/pkg/middleware"
	"github.com/alhasaniq/consortium/pkg/optimize"
	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	os.Exit(m.Run())
}

type e2eApp struct {
	handler http.Handler
	manager *jobs.Manager
	store   *storage.Storage
}

type deterministicProvider struct {
	name              string
	models            []providers.Model
	response          string
	failuresRemaining map[string]int
	responseFunc      func(req *providers.CompletionRequest) (string, error)
	mu                sync.Mutex
}

func (p *deterministicProvider) Name() string { return p.name }

func (p *deterministicProvider) Models() []providers.Model { return p.models }

func (p *deterministicProvider) EstimateTokens(text string) int {
	return len(text)/4 + 1
}

func (p *deterministicProvider) Cost(_ string, inputTokens, outputTokens int) float64 {
	return float64(inputTokens)*0.000001 + float64(outputTokens)*0.000002
}

func (p *deterministicProvider) Complete(ctx context.Context, req *providers.CompletionRequest) (*providers.CompletionResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	if p.failuresRemaining != nil {
		p.mu.Lock()
		remaining := p.failuresRemaining[req.Model]
		if remaining > 0 {
			p.failuresRemaining[req.Model] = remaining - 1
			p.mu.Unlock()
			return nil, fmt.Errorf("temporary e2e provider failure for %s", req.Model)
		}
		p.mu.Unlock()
	}

	lastMessage := ""
	if len(req.Messages) > 0 {
		lastMessage = req.Messages[len(req.Messages)-1].Content
	}

	if p.responseFunc != nil {
		content, err := p.responseFunc(req)
		if err != nil {
			return nil, err
		}
		return &providers.CompletionResponse{
			Content: content,
			Model:   req.Model,
			Usage: providers.Usage{
				PromptTokens:     12,
				CompletionTokens: 18,
				TotalTokens:      30,
			},
		}, nil
	}

	content := fmt.Sprintf("%s: %s\nAnswer: A\nFinal answer: A\n{\"score\": 9, \"scores\": {\"Correctness\": 9, \"Clarity\": 8}}", p.response, lastMessage)
	return &providers.CompletionResponse{
		Content: content,
		Model:   req.Model,
		Usage: providers.Usage{
			PromptTokens:     12,
			CompletionTokens: 18,
			TotalTokens:      30,
		},
	}, nil
}

func newE2EApp(t *testing.T) *e2eApp {
	return newE2EAppWithWorkers(t, true)
}

func newE2EAppWithWorkers(t *testing.T, startWorkers bool) *e2eApp {
	return newE2EAppWithWorkersAndProvider(t, startWorkers, &deterministicProvider{
		name:     "mock",
		models:   e2eProviderModels(),
		response: "deterministic",
	})
}

func newE2EAppWithWorkersAndProvider(t *testing.T, startWorkers bool, provider providers.Provider) *e2eApp {
	t.Helper()

	store, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("create in-memory storage: %v", err)
	}

	registry := providers.NewRegistry()
	registry.Register(provider)

	cfg := jobs.DefaultManagerConfig()
	cfg.MaxConcurrentWorkflows = 4
	cfg.WorkerCount = 4
	cfg.WorkerPollInterval = 2 * time.Millisecond
	manager := jobs.NewManagerWithConfig(store, registry, cfg)
	if startWorkers {
		manager.StartWorkers()
	}

	router := mux.NewRouter()
	router.Use(middleware.Recovery)
	router.Use(middleware.RequestID)
	router.Use(middleware.Logger)
	router.Use(middleware.CORS)

	workdir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workdir, "benchmarks", "results"), 0o700); err != nil {
		t.Fatalf("create benchmark results fixture dir: %v", err)
	}

	admin.NewServer(store, store.DB(), registry, manager, workdir).RegisterRoutes(router)
	api.NewWorkflowAPI(store, registry, manager).RegisterRoutes(router)
	registerSystemRoutes(router, manager)

	app := &e2eApp{
		handler: middleware.TrimTrailingSlash(router),
		manager: manager,
		store:   store,
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		manager.StopWorkers(ctx)
		if err := store.Close(); err != nil {
			t.Fatalf("close storage: %v", err)
		}
	})

	return app
}

func e2eProviderModels() []providers.Model {
	modelIDs := []string{
		"mock/fast",
		"mock/slow",
		"mock/flaky",
		"mock/judge",
		"mock/debate",
		"mock/scorer",
		"deepseek/deepseek-v4-flash",
		"deepseek/deepseek-v4-pro",
		"minimax/minimax-m3",
		"xiaomi/mimo-v2.5",
	}
	models := make([]providers.Model, 0, len(modelIDs))
	for _, id := range modelIDs {
		models = append(models, providers.Model{
			ID:         id,
			Provider:   "mock",
			Name:       id,
			ContextLen: 128000,
			InputCost:  0.000001,
			OutputCost: 0.000002,
			MaxTokens:  4096,
			Available:  true,
		})
	}
	return models
}

func registerSystemRoutes(router *mux.Router, manager *jobs.Manager) {
	router.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}).Methods("GET")

	router.HandleFunc("/system/readiness", func(w http.ResponseWriter, _ *http.Request) {
		active, capacity := manager.PoolStats()
		utilization := float64(0)
		if capacity > 0 {
			utilization = float64(active) / float64(capacity)
		}
		admissionState := "accepting"
		admissionPaused, pauseReason := manager.AdmissionState()
		if admissionPaused {
			admissionState = "paused"
		} else if active >= capacity {
			admissionState = "full"
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":                 "ready",
			"active_workflows":       active,
			"pool_capacity":          capacity,
			"pool_utilization":       utilization,
			"admission_state":        admissionState,
			"admission_pause_reason": pauseReason,
		})
	}).Methods("GET")
}

func (app *e2eApp) request(method, path string, body interface{}) *httptest.ResponseRecorder {
	var req *http.Request
	if body == nil {
		req = httptest.NewRequest(method, path, nil)
	} else {
		payload, err := json.Marshal(body)
		if err != nil {
			panic(err)
		}
		req = httptest.NewRequest(method, path, bytesReader(payload))
		req.Header.Set("Content-Type", "application/json")
	}

	rec := httptest.NewRecorder()
	app.handler.ServeHTTP(rec, req)
	return rec
}

func (app *e2eApp) formRequest(method, path string, values map[string]string) *httptest.ResponseRecorder {
	form := url.Values{}
	for key, value := range values {
		form.Set(key, value)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	app.handler.ServeHTTP(rec, req)
	return rec
}

func bytesReader(payload []byte) *bytes.Reader {
	return bytes.NewReader(payload)
}

func decodeResponse(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode response body %q: %v", rec.Body.String(), err)
	}
	return out
}

func requireStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, want, rec.Body.String())
	}
}

func retryPolicyMap() map[string]interface{} {
	return map[string]interface{}{
		"max_attempts":     1,
		"backoff_ms":       0,
		"backoff_multiply": 1.0,
		"max_backoff_ms":   0,
	}
}

func runtimeWorkflow(id, prompt string) map[string]interface{} {
	return map[string]interface{}{
		"id":   id,
		"name": "Runtime " + id,
		"nodes": []interface{}{
			map[string]interface{}{
				"id":              "prompt-1",
				"type":            "prompt",
				"model":           "mock/fast",
				"prompt":          prompt,
				"temperature":     0.0,
				"max_tokens":      128,
				"timeout_seconds": 30,
				"retry_policy":    retryPolicyMap(),
			},
		},
	}
}

func workflowFile(id string) map[string]interface{} {
	return map[string]interface{}{
		"id":          id,
		"version":     "1.0.0",
		"name":        "Builder " + id,
		"description": "e2e builder workflow",
		"created_at":  "2026-06-14T00:00:00Z",
		"updated_at":  "2026-06-14T00:00:00Z",
		"nodes": []interface{}{
			map[string]interface{}{
				"id":   "input-1",
				"type": "input",
				"data": map[string]interface{}{
					"type":  "input",
					"label": "Input",
					"config": map[string]interface{}{
						"name": "Input",
					},
				},
			},
			map[string]interface{}{
				"id":   "agent-1",
				"type": "agent",
				"data": map[string]interface{}{
					"type":  "agent",
					"label": "Agent",
					"config": map[string]interface{}{
						"name":           "Agent",
						"model":          "mock/fast",
						"systemPrompt":   "You answer deterministically.",
						"userPrompt":     "Question: {{user_prompt}}",
						"temperature":    0.0,
						"maxTokens":      128,
						"timeoutSeconds": 30,
						"retryPolicy":    retryPolicyMap(),
					},
				},
			},
			map[string]interface{}{
				"id":   "result-1",
				"type": "result",
				"data": map[string]interface{}{
					"type":  "result",
					"label": "Result",
					"config": map[string]interface{}{
						"name":              "final_answer",
						"outputFormat":      "text",
						"aggregationMethod": "collect",
						"timeoutSeconds":    30,
						"retryPolicy":       retryPolicyMap(),
					},
				},
			},
		},
		"edges": []interface{}{
			map[string]interface{}{"id": "edge-input-agent", "source": "input-1", "target": "agent-1"},
			map[string]interface{}{"id": "edge-agent-result", "source": "agent-1", "target": "result-1"},
		},
	}
}

func aggregationWorkflowFile(id, method string, agentPrompts []string, aggregationConfig map[string]interface{}) map[string]interface{} {
	nodes := []interface{}{
		map[string]interface{}{
			"id":   "input-1",
			"type": "input",
			"data": map[string]interface{}{
				"type":  "input",
				"label": "Input",
				"config": map[string]interface{}{
					"name": "Input",
				},
			},
		},
	}
	edges := []interface{}{}

	for index, prompt := range agentPrompts {
		agentID := fmt.Sprintf("agent-%d", index+1)
		nodes = append(nodes, map[string]interface{}{
			"id":   agentID,
			"type": "agent",
			"data": map[string]interface{}{
				"type":  "agent",
				"label": agentID,
				"config": map[string]interface{}{
					"name":           agentID,
					"model":          "mock/fast",
					"systemPrompt":   "You answer deterministically.",
					"userPrompt":     prompt,
					"temperature":    0.0,
					"maxTokens":      128,
					"timeoutSeconds": 30,
					"retryPolicy":    retryPolicyMap(),
				},
			},
		})
		edges = append(edges, map[string]interface{}{
			"id":     fmt.Sprintf("edge-input-%s", agentID),
			"source": "input-1",
			"target": agentID,
		})
		edges = append(edges, map[string]interface{}{
			"id":     fmt.Sprintf("edge-%s-agg", agentID),
			"source": agentID,
			"target": "agg-1",
		})
	}

	nodes = append(nodes,
		map[string]interface{}{
			"id":   "agg-1",
			"type": "aggregation",
			"data": map[string]interface{}{
				"type":  "aggregation",
				"label": "Aggregation",
				"config": map[string]interface{}{
					"name":              "Aggregation",
					"aggregationMethod": method,
					"aggregationConfig": aggregationConfig,
					"retryPolicy":       retryPolicyMap(),
				},
			},
		},
		map[string]interface{}{
			"id":   "result-1",
			"type": "result",
			"data": map[string]interface{}{
				"type":  "result",
				"label": "Result",
				"config": map[string]interface{}{
					"name":         "final_answer",
					"outputFormat": "text",
				},
			},
		},
	)
	edges = append(edges, map[string]interface{}{"id": "edge-agg-result", "source": "agg-1", "target": "result-1"})

	return map[string]interface{}{
		"id":          id,
		"version":     "1.0.0",
		"name":        "Aggregation " + method,
		"description": "e2e aggregation workflow",
		"created_at":  "2026-06-14T00:00:00Z",
		"updated_at":  "2026-06-14T00:00:00Z",
		"nodes":       nodes,
		"edges":       edges,
	}
}

func aggregationE2EConfig(method string) map[string]interface{} {
	rubric := []interface{}{
		map[string]interface{}{"name": "Correctness", "weight": 0.7, "description": "Correct answer"},
		map[string]interface{}{"name": "Clarity", "weight": 0.3, "description": "Clear answer"},
	}
	extraction := map[string]interface{}{
		"extraction_strategy": "regex",
		"extraction_pattern":  `Answer:\s*([A-D])`,
	}

	switch method {
	case "collect":
		return map[string]interface{}{"separator": " | "}
	case "majority_vote":
		return map[string]interface{}{
			"extraction_strategy": "regex",
			"extraction_pattern":  `Answer:\s*([A-D])`,
			"tie_breaker_method":  "first",
		}
	case "debate_decide":
		cfg := map[string]interface{}{
			"judge_model":       "mock/fast",
			"system_prompt":     "Judge competing answer camps.",
			"prompt":            "Question: {{prompt}}\n\n{{camps}}\n\nPick a winner.",
			"temperature":       0.0,
			"max_tokens":        128,
			"repair_max_tokens": 64,
		}
		for k, v := range extraction {
			cfg[k] = v
		}
		return cfg
	case "judge":
		cfg := map[string]interface{}{
			"judge_model":       "mock/fast",
			"system_prompt":     "Judge competing responses.",
			"prompt":            "Question: {{prompt}}\n\n{{responses}}\n\nPick a winner.",
			"temperature":       0.0,
			"max_tokens":        128,
			"repair_max_tokens": 64,
		}
		for k, v := range extraction {
			cfg[k] = v
		}
		return cfg
	case "scoring":
		cfg := map[string]interface{}{
			"scoring_model": "mock/fast",
			"system_prompt": "Score responses against the rubric.",
			"prompt":        "Question: {{prompt}}\n\nResponse: {{response}}\n\nRubric:\n{{rubric}}",
			"temperature":   0.0,
			"max_tokens":    128,
			"rubric":        rubric,
		}
		for k, v := range extraction {
			cfg[k] = v
		}
		return cfg
	case "synthesis":
		return map[string]interface{}{
			"model":         "mock/fast",
			"system_prompt": "Synthesize responses.",
			"prompt":        "Question: {{prompt}}\n\n{{responses}}\n\nSynthesize.",
			"temperature":   0.0,
			"max_tokens":    128,
		}
	case "peer_matrix":
		cfg := map[string]interface{}{
			"rubric_mode":        "static",
			"eval_system_prompt": "Score peer responses.",
			"eval_prompt":        "Question: {{question}}\n\nResponse: {{response}}\n\n{{rubric}}",
			"temperature":        0.0,
			"max_tokens":         128,
			"normalization":      "none",
			"max_parallel":       2,
			"rubric":             rubric,
		}
		for k, v := range extraction {
			cfg[k] = v
		}
		return cfg
	default:
		panic("unknown aggregation method " + method)
	}
}

func aggregationE2EAgentPrompts(method string) []string {
	if method == "majority_vote" {
		return []string{"Answer: B\nReasoning one.", "Answer: B\nReasoning two.", "Answer: A\nReasoning three."}
	}
	return []string{"Answer: A\nReasoning one.", "Answer: A\nReasoning two."}
}

func waitForJobStatus(t *testing.T, app *e2eApp, jobID string, want string) map[string]interface{} {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	var last map[string]interface{}
	for time.Now().Before(deadline) {
		rec := app.request(http.MethodGet, "/api/jobs/"+jobID, nil)
		requireStatus(t, rec, http.StatusOK)
		last = decodeResponse(t, rec)
		if status, _ := last["status"].(string); status == want {
			return last
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach %s; last=%v", jobID, want, last)
	return nil
}

func seedBenchmarkRun(t *testing.T, app *e2eApp) string {
	t.Helper()
	return seedBenchmarkRunVariant(t, app, "benchmark-e2e-workflow", "Benchmark E2E Workflow", true, false)
}

func seedBenchmarkRunVariant(t *testing.T, app *e2eApp, workflowID, workflowName string, row1Correct, row2Correct bool) string {
	t.Helper()

	row1Predicted := "A"
	row1Failure := ""
	if !row1Correct {
		row1Predicted = "B"
		row1Failure = "wrong_answer"
	}
	row2Predicted := "B"
	row2Failure := ""
	if !row2Correct {
		row2Predicted = "C"
		row2Failure = "wrong_answer"
	}
	correctItems := 0
	failureCounts := map[string]int{}
	if row1Correct {
		correctItems++
	} else {
		failureCounts["wrong_answer"]++
	}
	if row2Correct {
		correctItems++
	} else {
		failureCounts["wrong_answer"]++
	}

	runID := "bench-e2e-" + uuid.NewString()
	started := time.Now().Add(-2 * time.Second).UTC()
	completed := time.Now().UTC()
	datasetPath := filepath.Join(t.TempDir(), "e2e-benchmark.jsonl")
	var dataset bytes.Buffer
	for _, row := range []map[string]interface{}{
		{
			"id":           "row-1",
			"benchmark":    "global-mmlu-lite",
			"split":        "dev",
			"subject":      "math",
			"language":     "en",
			"question":     "What field does row 1 cover?",
			"choices":      []string{"math", "science"},
			"answer_label": "A",
		},
		{
			"id":           "row-2",
			"benchmark":    "global-mmlu-lite",
			"split":        "dev",
			"subject":      "science",
			"language":     "en",
			"question":     "What field does row 2 cover?",
			"choices":      []string{"math", "science", "history"},
			"answer_label": "B",
		},
	} {
		if err := json.NewEncoder(&dataset).Encode(row); err != nil {
			t.Fatalf("encode benchmark dataset row: %v", err)
		}
	}
	if err := os.WriteFile(datasetPath, dataset.Bytes(), 0o600); err != nil {
		t.Fatalf("write benchmark dataset fixture: %v", err)
	}
	result := &storage.BenchmarkRunResultInput{
		Summary: storage.BenchmarkRunSummaryInput{
			RunID:                   runID,
			Benchmark:               "e2e-benchmark",
			Split:                   "dev",
			ItemLimit:               2,
			WorkflowID:              workflowID,
			WorkflowName:            workflowName,
			DatasetPath:             datasetPath,
			TotalItems:              2,
			CompletedItems:          2,
			ParsedItems:             2,
			CorrectItems:            correctItems,
			Accuracy:                float64(correctItems) / 2,
			ParseRate:               1,
			TotalAttempts:           2,
			FailureReasonCounts:     failureCounts,
			AllAttemptFailureCounts: failureCounts,
			TotalLatencyMS:          30,
			AvgLatencyMS:            15,
			P50LatencyMS:            15,
			P95LatencyMS:            25,
			P99LatencyMS:            25,
			TotalTokensInput:        20,
			TotalTokensOutput:       30,
			TotalTokens:             50,
			AvgTokensPerItem:        25,
			TotalCostUSD:            0.002,
			StartedAt:               started,
			CompletedAt:             completed,
			ElapsedSeconds:          completed.Sub(started).Seconds(),
			ExecutionEngine:         "e2e",
		},
		Items: []storage.BenchmarkRunItemInput{
			{
				ItemID:        "row-1",
				Subject:       "math",
				Language:      "en",
				AnswerLabel:   "A",
				Predicted:     row1Predicted,
				ParseOK:       true,
				Correct:       row1Correct,
				JobID:         "bench-job-row-1",
				LatencyMS:     10,
				TokensInput:   10,
				TokensOutput:  10,
				TotalTokens:   20,
				CostUSD:       0.001,
				RawOutput:     row1Predicted,
				FailureReason: row1Failure,
				OutputSource:  "parent",
				WorkflowID:    workflowID,
				BenchmarkName: "e2e-benchmark",
				Attempts:      1,
				AttemptDetails: []storage.BenchmarkRunItemAttemptInput{
					{Attempt: 1, JobID: "bench-job-row-1", ParseOK: true, Predicted: row1Predicted, FailureReason: row1Failure, OutputSource: "parent"},
				},
			},
			{
				ItemID:        "row-2",
				Subject:       "science",
				Language:      "en",
				AnswerLabel:   "B",
				Predicted:     row2Predicted,
				ParseOK:       true,
				Correct:       row2Correct,
				JobID:         "bench-job-row-2",
				LatencyMS:     20,
				TokensInput:   10,
				TokensOutput:  20,
				TotalTokens:   30,
				CostUSD:       0.001,
				RawOutput:     row2Predicted,
				FailureReason: row2Failure,
				OutputSource:  "parent",
				WorkflowID:    workflowID,
				BenchmarkName: "e2e-benchmark",
				Attempts:      1,
				AttemptDetails: []storage.BenchmarkRunItemAttemptInput{
					{Attempt: 1, JobID: "bench-job-row-2", ParseOK: true, Predicted: row2Predicted, FailureReason: row2Failure, OutputSource: "parent"},
				},
			},
		},
	}
	if err := app.store.UpsertBenchmarkRunResult(result, storage.BenchmarkRunPersistMeta{
		Status: "completed",
		Source: "manual",
	}); err != nil {
		t.Fatalf("seed benchmark run: %v", err)
	}
	return runID
}

func optimizeSpec() *optimize.OptimizeSpec {
	return &optimize.OptimizeSpec{
		Params: []optimize.ParamDeclaration{
			{
				Path:       "nodes[agent-1].data.config.model",
				Type:       optimize.ParamTypeModel,
				Candidates: []string{"mock/fast", "mock/slow"},
			},
		},
		Objectives: []optimize.Objective{
			{Metric: "accuracy", Direction: "maximize", Weight: 1},
		},
		StopPolicy: optimize.StopPolicy{
			MaxGenerations: 2,
			BudgetUSD:      1,
		},
		PromotionPolicy: optimize.PromotionPolicy{
			MinAccuracyGain: 0.01,
		},
	}
}

func fitness(score float64) *optimize.Fitness {
	return &optimize.Fitness{
		Accuracy:         score,
		AdjustedAccuracy: score,
		ParseRate:        1,
		TotalCost:        0.01,
		CostPerItem:      0.005,
		AvgLatencyMS:     10,
		P95LatencyMS:     20,
		TotalItems:       2,
		CompositeScore:   score,
		Feasible:         true,
	}
}

func TestE2EWorkflowCRUDValidateExecuteAndAdminVisibility(t *testing.T) {
	t.Parallel()
	app := newE2EApp(t)

	health := app.request(http.MethodGet, "/health", nil)
	requireStatus(t, health, http.StatusOK)
	if strings.TrimSpace(health.Body.String()) != "OK" {
		t.Fatalf("health body=%q want OK", health.Body.String())
	}

	readiness := app.request(http.MethodGet, "/system/readiness", nil)
	requireStatus(t, readiness, http.StatusOK)
	readinessBody := decodeResponse(t, readiness)
	if readinessBody["status"] != "ready" {
		t.Fatalf("unexpected readiness response: %v", readinessBody)
	}
	state, ok := readinessBody["admission_state"].(string)
	if !ok || (state != "accepting" && state != "full") {
		t.Fatalf("unexpected readiness admission state: %v", readinessBody)
	}
	active, ok := readinessBody["active_workflows"].(float64)
	if !ok || active < 0 {
		t.Fatalf("unexpected active workflow count: %v", readinessBody)
	}
	capacity, ok := readinessBody["pool_capacity"].(float64)
	if !ok || capacity != 4 {
		t.Fatalf("unexpected readiness capacity: %v", readinessBody)
	}
	if active > capacity {
		t.Fatalf("readiness active workflows exceeded capacity: %v", readinessBody)
	}

	models := app.request(http.MethodGet, "/api/models?provider=mock", nil)
	requireStatus(t, models, http.StatusOK)
	modelsBody := decodeResponse(t, models)
	if got := len(modelsBody["models"].([]interface{})); got < 2 {
		t.Fatalf("mock model count=%d want at least 2", got)
	}

	unknownModels := app.request(http.MethodGet, "/api/models?provider=missing", nil)
	requireStatus(t, unknownModels, http.StatusOK)
	if got := len(decodeResponse(t, unknownModels)["models"].([]interface{})); got != 0 {
		t.Fatalf("unknown provider model count=%d want 0", got)
	}

	seeds := app.request(http.MethodGet, "/api/workflows/seeds", nil)
	requireStatus(t, seeds, http.StatusOK)
	if got := len(decodeResponse(t, seeds)["seeds"].([]interface{})); got == 0 {
		t.Fatal("expected seed workflow templates")
	}

	legacyWS := app.request(http.MethodPost, "/api/workflows/execute/ws", runtimeWorkflow("legacy-ws", "legacy"))
	requireStatus(t, legacyWS, http.StatusGone)

	workflowID := "e2e-crud-" + uuid.NewString()
	create := app.request(http.MethodPost, "/api/workflows", workflowFile(workflowID))
	requireStatus(t, create, http.StatusCreated)
	createBody := decodeResponse(t, create)
	if createBody["id"] != workflowID {
		t.Fatalf("created workflow id=%v want %s", createBody["id"], workflowID)
	}

	list := app.request(http.MethodGet, "/api/workflows", nil)
	requireStatus(t, list, http.StatusOK)
	listBody := decodeResponse(t, list)
	if workflows := listBody["workflows"].([]interface{}); len(workflows) == 0 {
		t.Fatal("expected at least one workflow after create")
	}

	get := app.request(http.MethodGet, "/api/workflows/"+workflowID, nil)
	requireStatus(t, get, http.StatusOK)
	getBody := decodeResponse(t, get)
	if getBody["id"] != workflowID || getBody["name"] != "Builder "+workflowID {
		t.Fatalf("unexpected get workflow body: %v", getBody)
	}

	updated := workflowFile("body-id-must-not-win")
	updated["name"] = "Renamed By URL"
	update := app.request(http.MethodPut, "/api/workflows/"+workflowID, updated)
	requireStatus(t, update, http.StatusOK)
	updateBody := decodeResponse(t, update)
	updatedWorkflow := updateBody["workflow"].(map[string]interface{})
	if updatedWorkflow["id"] != workflowID || updatedWorkflow["name"] != "Renamed By URL" {
		t.Fatalf("update did not preserve URL id/name: %v", updatedWorkflow)
	}

	adminWorkflowDetail := app.request(http.MethodGet, "/api/admin/workflows/"+workflowID, nil)
	requireStatus(t, adminWorkflowDetail, http.StatusOK)
	adminWorkflowDetailBody := decodeResponse(t, adminWorkflowDetail)
	if adminWorkflowDetailBody["Workflow"].(map[string]interface{})["id"] != workflowID {
		t.Fatalf("admin workflow detail mismatch: %v", adminWorkflowDetailBody)
	}

	adminUpdated := workflowFile("admin-body-id-must-not-win")
	adminUpdated["name"] = "Admin Updated"
	adminUpdate := app.request(http.MethodPut, "/api/admin/workflows/"+workflowID, adminUpdated)
	requireStatus(t, adminUpdate, http.StatusOK)
	if decodeResponse(t, adminUpdate)["id"] != workflowID {
		t.Fatalf("admin workflow update did not preserve URL id: %s", adminUpdate.Body.String())
	}

	valid := app.request(http.MethodPost, "/api/workflows/validate", runtimeWorkflow("validate-ok", "hello"))
	requireStatus(t, valid, http.StatusOK)
	validBody := decodeResponse(t, valid)
	if validBody["valid"] != true {
		t.Fatalf("valid workflow was rejected: %v", validBody)
	}

	invalid := runtimeWorkflow("validate-bad", "hello")
	invalid["nodes"].([]interface{})[0].(map[string]interface{})["model"] = "missing/model"
	invalidRec := app.request(http.MethodPost, "/api/workflows/validate", invalid)
	requireStatus(t, invalidRec, http.StatusOK)
	invalidBody := decodeResponse(t, invalidRec)
	if invalidBody["valid"] != false {
		t.Fatalf("invalid workflow was accepted: %v", invalidBody)
	}

	exec := app.request(http.MethodPost, "/api/workflows/execute", runtimeWorkflow("sync-"+workflowID, "sync prompt"))
	requireStatus(t, exec, http.StatusOK)
	execBody := decodeResponse(t, exec)
	if execBody["success"] != true || execBody["job_id"] == "" {
		t.Fatalf("unexpected execute response: %v", execBody)
	}
	jobID := execBody["job_id"].(string)

	job := app.request(http.MethodGet, "/api/jobs/"+jobID, nil)
	requireStatus(t, job, http.StatusOK)
	jobBody := decodeResponse(t, job)
	if jobBody["status"] != events.JobStatusCompleted {
		t.Fatalf("job status=%v want completed", jobBody["status"])
	}
	if nodes := jobBody["nodes"].([]interface{}); len(nodes) == 0 {
		t.Fatalf("expected executed job nodes, got %v", jobBody)
	}

	publicJobs := app.request(http.MethodGet, "/api/jobs?status=completed&limit=1", nil)
	requireStatus(t, publicJobs, http.StatusOK)
	publicJobsBody := decodeResponse(t, publicJobs)
	if jobsRows := publicJobsBody["jobs"].([]interface{}); len(jobsRows) != 1 {
		t.Fatalf("public job list should return one completed job with limit=1: %v", publicJobsBody)
	}

	config := app.request(http.MethodGet, "/api/jobs/"+jobID+"/config", nil)
	requireStatus(t, config, http.StatusOK)
	configBody := decodeResponse(t, config)
	if configBody["id"] == "" || configBody["nodes"] == nil {
		t.Fatalf("unexpected config response: %v", configBody)
	}

	snapshot := app.request(http.MethodGet, "/api/jobs/"+jobID+"/workflow", nil)
	requireStatus(t, snapshot, http.StatusOK)
	snapshotBody := decodeResponse(t, snapshot)
	if snapshotBody["source"] != "request_data" || snapshotBody["saved_workflow_exists"] != false {
		t.Fatalf("unexpected job workflow snapshot: %v", snapshotBody)
	}

	adminOverview := app.request(http.MethodGet, "/api/admin/overview", nil)
	requireStatus(t, adminOverview, http.StatusOK)
	overviewBody := decodeResponse(t, adminOverview)
	stats := overviewBody["Stats"].(map[string]interface{})
	if stats["CompletedJobs"].(float64) < 1 {
		t.Fatalf("admin overview did not observe completed job: %v", overviewBody)
	}

	adminJobs := app.request(http.MethodGet, "/api/admin/jobs?status=completed&show=all", nil)
	requireStatus(t, adminJobs, http.StatusOK)
	adminJobsBody := decodeResponse(t, adminJobs)
	if jobsRows := adminJobsBody["Jobs"].([]interface{}); len(jobsRows) == 0 {
		t.Fatalf("admin jobs did not include completed jobs: %v", adminJobsBody)
	}

	adminJobDetail := app.request(http.MethodGet, "/api/admin/jobs/"+jobID, nil)
	requireStatus(t, adminJobDetail, http.StatusOK)
	adminJobBody := decodeResponse(t, adminJobDetail)
	if adminJobBody["Job"].(map[string]interface{})["id"] != jobID {
		t.Fatalf("admin job detail mismatch: %v", adminJobBody)
	}

	adminWorkflows := app.request(http.MethodGet, "/api/admin/workflows", nil)
	requireStatus(t, adminWorkflows, http.StatusOK)
	adminWorkflowsBody := decodeResponse(t, adminWorkflows)
	if adminWorkflowsBody["TotalWorkflows"].(float64) < 1 {
		t.Fatalf("admin workflows did not observe created workflow: %v", adminWorkflowsBody)
	}

	exported := app.request(http.MethodGet, "/api/admin/workflows/"+workflowID+"/export", nil)
	requireStatus(t, exported, http.StatusOK)
	exportBody := decodeResponse(t, exported)
	if exportBody["id"] != workflowID || exportBody["name"] != "Admin Updated" {
		t.Fatalf("admin workflow export id mismatch: %v", exportBody)
	}

	deleteRec := app.request(http.MethodDelete, "/api/workflows/"+workflowID, nil)
	requireStatus(t, deleteRec, http.StatusOK)
	missing := app.request(http.MethodGet, "/api/workflows/"+workflowID, nil)
	requireStatus(t, missing, http.StatusNotFound)
}

func TestE2ESubmitIdempotencyStreamReplayAndBulkControls(t *testing.T) {
	t.Parallel()
	app := newE2EApp(t)

	payload := map[string]interface{}{
		"workflow_file": workflowFile("e2e-async-" + uuid.NewString()),
		"input_values": map[string]interface{}{
			"user_prompt": "What is covered by async e2e?",
		},
		"idempotency_key": "idem-" + uuid.NewString(),
	}

	first := app.request(http.MethodPost, "/api/workflows/submit", payload)
	requireStatus(t, first, http.StatusCreated)
	firstBody := decodeResponse(t, first)
	firstJobID := firstBody["job_id"].(string)
	if firstBody["duplicate"] != false || firstBody["status"] == "" {
		t.Fatalf("unexpected first submit response: %v", firstBody)
	}

	second := app.request(http.MethodPost, "/api/workflows/submit", payload)
	requireStatus(t, second, http.StatusOK)
	secondBody := decodeResponse(t, second)
	if secondBody["job_id"] != firstJobID || secondBody["duplicate"] != true {
		t.Fatalf("idempotency did not return original job: first=%v second=%v", firstBody, secondBody)
	}

	completed := waitForJobStatus(t, app, firstJobID, events.JobStatusCompleted)
	if completed["tokens_total"].(float64) <= 0 {
		t.Fatalf("completed job missing token accounting: %v", completed)
	}

	terminalStream := app.request(http.MethodGet, "/api/jobs/"+firstJobID+"/stream", nil)
	requireStatus(t, terminalStream, http.StatusOK)
	streamSnapshot := decodeResponse(t, terminalStream)
	if streamSnapshot["complete"] != true || streamSnapshot["status"] != events.JobStatusCompleted {
		t.Fatalf("terminal stream snapshot mismatch: %v", streamSnapshot)
	}

	trace := app.request(http.MethodGet, "/api/jobs/"+firstJobID+"/trace", nil)
	requireStatus(t, trace, http.StatusOK)
	traceBody := decodeResponse(t, trace)
	if traceBody["job_id"] != firstJobID {
		t.Fatalf("trace job id mismatch: %v", traceBody)
	}

	forcePayload := map[string]interface{}{}
	for k, v := range payload {
		forcePayload[k] = v
	}
	forcePayload["force_new_run"] = true
	forced := app.request(http.MethodPost, "/api/workflows/submit", forcePayload)
	requireStatus(t, forced, http.StatusCreated)
	forcedBody := decodeResponse(t, forced)
	forcedJobID := forcedBody["job_id"].(string)
	if forcedJobID == firstJobID || forcedBody["dedup_bypassed"] != true {
		t.Fatalf("force_new_run did not bypass dedup: %v", forcedBody)
	}
	waitForJobStatus(t, app, forcedJobID, events.JobStatusCompleted)

	diff := app.request(http.MethodGet, "/api/jobs/"+firstJobID+"/diff/"+forcedJobID, nil)
	requireStatus(t, diff, http.StatusOK)

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopCancel()
	app.manager.StopWorkers(stopCtx)

	pendingID := "bulk-pending-" + uuid.NewString()
	pausedID := "bulk-paused-" + uuid.NewString()
	for _, seeded := range []*storage.Job{
		{ID: pendingID, Description: "bulk pending", Model: "workflow", Status: events.JobStatusPending},
		{ID: pausedID, Description: "bulk paused", Model: "workflow", Status: events.JobStatusPaused},
	} {
		if err := app.store.CreateExecution(seeded); err != nil {
			t.Fatalf("seed %s job: %v", seeded.Status, err)
		}
	}

	pauseAll := app.request(http.MethodPost, "/api/jobs/pause-all", nil)
	requireStatus(t, pauseAll, http.StatusOK)
	pausedJob := app.request(http.MethodGet, "/api/jobs/"+pendingID, nil)
	requireStatus(t, pausedJob, http.StatusOK)
	if status := decodeResponse(t, pausedJob)["status"]; status != events.JobStatusPaused {
		t.Fatalf("pending job should be paused, got %v", status)
	}

	resumeAll := app.request(http.MethodPost, "/api/jobs/resume-all", nil)
	requireStatus(t, resumeAll, http.StatusOK)
	resumedJob := app.request(http.MethodGet, "/api/jobs/"+pendingID, nil)
	requireStatus(t, resumedJob, http.StatusOK)
	if status := decodeResponse(t, resumedJob)["status"]; status != events.JobStatusPending {
		t.Fatalf("paused job should resume to pending, got %v", status)
	}

	cancelAll := app.request(http.MethodPost, "/api/jobs/cancel-all", nil)
	requireStatus(t, cancelAll, http.StatusOK)
	cancelledPending := app.request(http.MethodGet, "/api/jobs/"+pendingID, nil)
	requireStatus(t, cancelledPending, http.StatusOK)
	if status := decodeResponse(t, cancelledPending)["status"]; status != events.JobStatusCancelled {
		t.Fatalf("pending job should cancel, got %v", status)
	}
	cancelledPaused := app.request(http.MethodGet, "/api/jobs/"+pausedID, nil)
	requireStatus(t, cancelledPaused, http.StatusOK)
	if status := decodeResponse(t, cancelledPaused)["status"]; status != events.JobStatusCancelled {
		t.Fatalf("paused job should cancel, got %v", status)
	}

	cancelMissing := app.request(http.MethodPost, "/api/jobs/not-a-real-job/cancel", nil)
	requireStatus(t, cancelMissing, http.StatusNotFound)

	singlePausedID := "single-paused-" + uuid.NewString()
	if err := app.store.CreateExecution(&storage.Job{
		ID:          singlePausedID,
		Description: "single resume",
		Model:       "workflow",
		Status:      events.JobStatusPaused,
	}); err != nil {
		t.Fatalf("seed single paused job: %v", err)
	}
	resumeSingle := app.request(http.MethodPost, "/api/jobs/"+singlePausedID+"/resume", nil)
	requireStatus(t, resumeSingle, http.StatusOK)
	resumedSingle := app.request(http.MethodGet, "/api/jobs/"+singlePausedID, nil)
	requireStatus(t, resumedSingle, http.StatusOK)
	if status := decodeResponse(t, resumedSingle)["status"]; status != events.JobStatusPending {
		t.Fatalf("single paused job should resume to pending, got %v", status)
	}
}

func TestE2EAggregationMacroMethodsSubmitAndComplete(t *testing.T) {
	app := newE2EApp(t)

	tests := []struct {
		method      string
		wantContain string
	}{
		{method: "collect", wantContain: " | "},
		{method: "majority_vote", wantContain: "Answer: B"},
		{method: "debate_decide", wantContain: "Answer: A"},
		{method: "judge", wantContain: "Answer: A"},
		{method: "scoring", wantContain: "Answer: A"},
		{method: "synthesis", wantContain: "deterministic:"},
		{method: "peer_matrix", wantContain: "Answer: A"},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			payload := map[string]interface{}{
				"workflow_file": aggregationWorkflowFile(
					"e2e-agg-"+tt.method+"-"+uuid.NewString(),
					tt.method,
					aggregationE2EAgentPrompts(tt.method),
					aggregationE2EConfig(tt.method),
				),
				"input_values": map[string]interface{}{
					"user_prompt": "Which answer should win?",
				},
				"idempotency_key": "agg-" + tt.method + "-" + uuid.NewString(),
			}

			submit := app.request(http.MethodPost, "/api/workflows/submit", payload)
			requireStatus(t, submit, http.StatusCreated)
			body := decodeResponse(t, submit)
			jobID := body["job_id"].(string)

			waitForJobStatus(t, app, jobID, events.JobStatusCompleted)
			job, err := app.store.GetExecution(jobID)
			if err != nil {
				t.Fatalf("load completed job: %v", err)
			}
			if !strings.Contains(job.ResultText, tt.wantContain) {
				t.Fatalf("result_text for %s = %q, want to contain %q", tt.method, job.ResultText, tt.wantContain)
			}
		})
	}
}

func TestE2ELiveWebSocketReplayAndCloseCode(t *testing.T) {
	t.Parallel()
	app := newE2EAppWithWorkers(t, false)

	jobID := "e2e-ws-" + uuid.NewString()
	if err := app.store.CreateExecution(&storage.Job{
		ID:                  jobID,
		Description:         "websocket replay e2e",
		Model:               "workflow",
		Status:              events.JobStatusRunning,
		WorkflowID:          "wf-" + jobID,
		WorkflowExecutionID: jobID,
		RunID:               jobID,
		RunNumber:           1,
		DAGSnapshot:         `{"id":"wf-` + jobID + `","name":"WS","nodes":[{"id":"n1","type":"prompt","model":"mock/fast","prompt":"hello","max_tokens":64,"timeout_seconds":30,"retry_policy":{"max_attempts":1}}]}`,
		DAGHash:             "hash-" + jobID,
	}); err != nil {
		t.Fatalf("create running ws job: %v", err)
	}

	ctx := context.Background()
	for _, event := range []struct {
		typ     string
		nodeID  string
		message string
		output  string
	}{
		{typ: events.EventStatus, message: "queued"},
		{typ: events.EventNodeStart, nodeID: "n1", message: "started"},
		{typ: events.EventNodeComplete, nodeID: "n1", message: "completed", output: "ok"},
	} {
		payload := map[string]interface{}{"message": event.message}
		if event.output != "" {
			payload["output"] = event.output
		}
		if _, err := app.store.AppendEventWithDetails(ctx, jobID, event.typ, event.nodeID, event.message, "", "", payload); err != nil {
			t.Fatalf("append event %s: %v", event.typ, err)
		}
	}

	server := httptest.NewServer(app.handler)
	defer server.Close()

	completeAfterSnapshot := make(chan struct{})
	completionDone := make(chan struct{})
	releaseCompletion := func() {
		select {
		case <-completeAfterSnapshot:
		default:
			close(completeAfterSnapshot)
		}
	}
	defer func() {
		releaseCompletion()
		<-completionDone
	}()

	go func() {
		defer close(completionDone)
		<-completeAfterSnapshot
		_, _ = app.store.AppendEventWithDetails(context.Background(), jobID, events.EventComplete, "", "done", "", "", map[string]interface{}{"message": "done"})
		_ = app.store.UpdateExecutionStatus(jobID, events.JobStatusCompleted)
	}()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/jobs/" + jobID + "/stream?resume_from=1"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		if resp != nil {
			t.Fatalf("dial stream status=%d: %v", resp.StatusCode, err)
		}
		t.Fatalf("dial stream: %v", err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var snapshot map[string]interface{}
	if err := conn.ReadJSON(&snapshot); err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if snapshot["type"] != "snapshot" {
		t.Fatalf("first websocket message was not snapshot: %v", snapshot)
	}
	releaseCompletion()

	var replaySequences []int
	closeCode := 0
	for {
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var msg map[string]interface{}
		err := conn.ReadJSON(&msg)
		if err != nil {
			if closeErr, ok := err.(*websocket.CloseError); ok {
				closeCode = closeErr.Code
				break
			}
			t.Fatalf("read websocket event: %v", err)
		}
		if data, ok := msg["data"].(map[string]interface{}); ok {
			if seq, ok := data["sequence"].(float64); ok {
				replaySequences = append(replaySequences, int(seq))
			}
		}
	}

	if closeCode != events.CloseNormal {
		t.Fatalf("close code=%d want %d", closeCode, events.CloseNormal)
	}
	if len(replaySequences) < 3 || replaySequences[0] != 2 || replaySequences[1] != 3 {
		t.Fatalf("expected replay to skip sequence 1 and include newer events, got %v", replaySequences)
	}
}

func TestE2EAdminBenchmarkRunAndDatasetFlagLifecycle(t *testing.T) {
	t.Parallel()
	app := newE2EApp(t)
	runID := seedBenchmarkRun(t, app)

	list := app.request(http.MethodGet, "/api/admin/benchmarks?benchmark=e2e-benchmark&split=dev", nil)
	requireStatus(t, list, http.StatusOK)
	listBody := decodeResponse(t, list)
	runs := listBody["Runs"].([]interface{})
	if len(runs) != 1 || runs[0].(map[string]interface{})["id"] != runID {
		t.Fatalf("benchmark list mismatch: %v", listBody)
	}

	detail := app.request(http.MethodGet, "/api/admin/benchmarks/"+runID+"?incorrect=1", nil)
	requireStatus(t, detail, http.StatusOK)
	detailBody := decodeResponse(t, detail)
	if detailBody["TotalItems"].(float64) != 1 || detailBody["IncorrectCount"].(float64) != 1 {
		t.Fatalf("benchmark detail incorrect filter mismatch: %v", detailBody)
	}

	item := app.request(http.MethodGet, "/api/admin/benchmarks/"+runID+"/items/row-2", nil)
	requireStatus(t, item, http.StatusOK)
	itemBody := decodeResponse(t, item)
	detailPayload := itemBody["Detail"].(map[string]interface{})
	if detailPayload["item"].(map[string]interface{})["item_id"] != "row-2" {
		t.Fatalf("benchmark item detail mismatch: %v", itemBody)
	}
	datasetItem := itemBody["DatasetItem"].(map[string]interface{})
	if datasetItem["question"] != "What field does row 2 cover?" || itemBody["NotFoundInSet"].(bool) {
		t.Fatalf("benchmark dataset item was not resolved: %v", itemBody)
	}

	analysis := app.request(http.MethodGet, "/api/admin/benchmarks/"+runID+"/analysis?top=2&page=1&page_size=1", nil)
	requireStatus(t, analysis, http.StatusOK)
	analysisBody := decodeResponse(t, analysis)
	if analysisBody["run_id"] != runID || analysisBody["total_items"].(float64) != 1 {
		t.Fatalf("benchmark analysis mismatch: %v", analysisBody)
	}

	createFlag := app.request(http.MethodPost, "/api/admin/benchmarks/dataset-flags", map[string]interface{}{
		"flags": []interface{}{
			map[string]interface{}{
				"benchmark":       "e2e-benchmark",
				"split":           "dev",
				"item_id":         "row-2",
				"dataset_version": "e2e-v1",
				"reason":          "Gold answer is intentionally suspect in this fixture",
				"source":          "e2e",
			},
		},
	})
	requireStatus(t, createFlag, http.StatusOK)
	flagBody := decodeResponse(t, createFlag)
	flags := flagBody["flags"].([]interface{})
	if len(flags) != 1 {
		t.Fatalf("expected one created flag, got %v", flagBody)
	}
	flagID := int(flags[0].(map[string]interface{})["id"].(float64))

	listFlags := app.request(http.MethodGet, "/api/admin/benchmarks/dataset-flags?benchmark=e2e-benchmark&split=dev", nil)
	requireStatus(t, listFlags, http.StatusOK)
	listFlagsBody := decodeResponse(t, listFlags)
	if listFlagsBody["total"].(float64) != 1 {
		t.Fatalf("expected one active flag, got %v", listFlagsBody)
	}

	resolve := app.request(http.MethodPatch, fmt.Sprintf("/api/admin/benchmarks/dataset-flags/%d/resolve", flagID), map[string]interface{}{
		"resolved_by":     "e2e",
		"resolved_reason": "fixture complete",
	})
	requireStatus(t, resolve, http.StatusOK)
	if decodeResponse(t, resolve)["resolved"] != true {
		t.Fatalf("flag was not resolved: %s", resolve.Body.String())
	}

	afterResolve := app.request(http.MethodGet, "/api/admin/benchmarks/dataset-flags?benchmark=e2e-benchmark&split=dev", nil)
	requireStatus(t, afterResolve, http.StatusOK)
	if decodeResponse(t, afterResolve)["total"].(float64) != 0 {
		t.Fatalf("resolved flag should be hidden by active default: %s", afterResolve.Body.String())
	}

	allFlags := app.request(http.MethodGet, "/api/admin/benchmarks/dataset-flags?benchmark=e2e-benchmark&split=dev&active_only=false", nil)
	requireStatus(t, allFlags, http.StatusOK)
	if decodeResponse(t, allFlags)["total"].(float64) != 1 {
		t.Fatalf("resolved flag should appear with active_only=false: %s", allFlags.Body.String())
	}

	deleteFlag := app.request(http.MethodDelete, fmt.Sprintf("/api/admin/benchmarks/dataset-flags/%d", flagID), nil)
	requireStatus(t, deleteFlag, http.StatusOK)
	if decodeResponse(t, deleteFlag)["deleted"] != true {
		t.Fatalf("flag was not deleted: %s", deleteFlag.Body.String())
	}
}

func TestE2EAdminOperationsAndBenchmarkEdges(t *testing.T) {
	t.Parallel()
	app := newE2EApp(t)
	ctx := context.Background()

	dbDiag := app.request(http.MethodGet, "/api/admin/db-diagnostics?tables=true", nil)
	requireStatus(t, dbDiag, http.StatusOK)
	dbDiagBody := decodeResponse(t, dbDiag)
	if dbDiagBody["Database"] == nil || dbDiagBody["Workers"] == nil {
		t.Fatalf("db diagnostics should include database and worker state: %v", dbDiagBody)
	}

	admission := app.request(http.MethodGet, "/api/admin/admission", nil)
	requireStatus(t, admission, http.StatusOK)
	if decodeResponse(t, admission)["paused"] != false {
		t.Fatalf("new app should accept admission: %s", admission.Body.String())
	}
	pauseAdmission := app.formRequest(http.MethodPost, "/api/admin/admission/pause", map[string]string{
		"message": "e2e maintenance window",
	})
	requireStatus(t, pauseAdmission, http.StatusOK)
	pauseBody := decodeResponse(t, pauseAdmission)
	if pauseBody["paused"] != true || pauseBody["state"] != "paused" {
		t.Fatalf("admission pause mismatch: %v", pauseBody)
	}
	readiness := app.request(http.MethodGet, "/system/readiness", nil)
	requireStatus(t, readiness, http.StatusOK)
	if decodeResponse(t, readiness)["admission_state"] != "paused" {
		t.Fatalf("readiness should expose paused admission: %s", readiness.Body.String())
	}
	resumeAdmission := app.formRequest(http.MethodPost, "/api/admin/admission/resume", nil)
	requireStatus(t, resumeAdmission, http.StatusOK)
	if decodeResponse(t, resumeAdmission)["paused"] != false {
		t.Fatalf("admission resume mismatch: %s", resumeAdmission.Body.String())
	}

	testWorkflow := app.request(http.MethodPost, "/api/admin/test/workflow", map[string]interface{}{
		"workflow": map[string]interface{}{
			"id":   "admin-test-workflow-" + uuid.NewString(),
			"name": "Admin Test Workflow",
			"nodes": []map[string]interface{}{
				{
					"id":              "test-prompt",
					"type":            "prompt",
					"model":           "mock/fast",
					"prompt":          "Admin test prompt",
					"temperature":     0.0,
					"max_tokens":      32,
					"timeout_seconds": 30,
					"retry_policy":    retryPolicyMap(),
				},
			},
		},
	})
	requireStatus(t, testWorkflow, http.StatusOK)
	if decodeResponse(t, testWorkflow)["success"] != true {
		t.Fatalf("admin test workflow should succeed: %s", testWorkflow.Body.String())
	}

	stopCtx, stopCancel := context.WithTimeout(ctx, 5*time.Second)
	defer stopCancel()
	app.manager.StopWorkers(stopCtx)

	adminPendingID := "admin-bulk-pending-" + uuid.NewString()
	adminPausedID := "admin-bulk-paused-" + uuid.NewString()
	for _, seeded := range []*storage.Job{
		{ID: adminPendingID, Description: "admin bulk pending", Model: "workflow", Status: events.JobStatusPending},
		{ID: adminPausedID, Description: "admin bulk paused", Model: "workflow", Status: events.JobStatusPaused},
	} {
		if err := app.store.CreateExecution(seeded); err != nil {
			t.Fatalf("seed admin bulk %s job: %v", seeded.Status, err)
		}
	}
	adminPauseAll := app.request(http.MethodPost, "/api/admin/jobs/pause-all", nil)
	requireStatus(t, adminPauseAll, http.StatusOK)
	if decodeResponse(t, app.request(http.MethodGet, "/api/jobs/"+adminPendingID, nil))["status"] != events.JobStatusPaused {
		t.Fatalf("admin pause-all should pause pending job")
	}
	adminResumeAll := app.request(http.MethodPost, "/api/admin/jobs/resume-all", nil)
	requireStatus(t, adminResumeAll, http.StatusOK)
	if decodeResponse(t, app.request(http.MethodGet, "/api/jobs/"+adminPendingID, nil))["status"] != events.JobStatusPending {
		t.Fatalf("admin resume-all should resume paused job")
	}
	adminCancelAll := app.request(http.MethodPost, "/api/admin/jobs/cancel-all", nil)
	requireStatus(t, adminCancelAll, http.StatusOK)
	if decodeResponse(t, app.request(http.MethodGet, "/api/jobs/"+adminPendingID, nil))["status"] != events.JobStatusCancelled {
		t.Fatalf("admin cancel-all should cancel queued pending job")
	}

	adminSinglePausedID := "admin-single-paused-" + uuid.NewString()
	if err := app.store.CreateExecution(&storage.Job{
		ID:          adminSinglePausedID,
		Description: "admin single paused",
		Model:       "workflow",
		Status:      events.JobStatusPaused,
	}); err != nil {
		t.Fatalf("seed admin single paused job: %v", err)
	}
	adminResumeJob := app.request(http.MethodPost, "/api/admin/jobs/"+adminSinglePausedID+"/resume", nil)
	requireStatus(t, adminResumeJob, http.StatusOK)
	if decodeResponse(t, app.request(http.MethodGet, "/api/jobs/"+adminSinglePausedID, nil))["status"] != events.JobStatusPending {
		t.Fatalf("admin resume job should move paused job to pending")
	}
	adminCancelID := "admin-single-running-" + uuid.NewString()
	if err := app.store.CreateExecution(&storage.Job{
		ID:          adminCancelID,
		Description: "admin single running",
		Model:       "workflow",
		Status:      events.JobStatusRunning,
	}); err != nil {
		t.Fatalf("seed admin running job: %v", err)
	}
	adminCancelJob := app.request(http.MethodPost, "/api/admin/jobs/"+adminCancelID+"/cancel", nil)
	requireStatus(t, adminCancelJob, http.StatusConflict)

	jobID := "admin-e2e-" + uuid.NewString()
	if err := app.store.CreateExecution(&storage.Job{
		ID:          jobID,
		Description: "admin operations e2e",
		Model:       "workflow",
		Status:      events.JobStatusCompleted,
		WorkflowID:  "wf-" + jobID,
	}); err != nil {
		t.Fatalf("seed completed admin job: %v", err)
	}
	completedAt := time.Now().UTC()
	if err := app.store.UpsertNodeExecutionAttempt(ctx, &storage.NodeExecutionAttempt{
		JobID:        jobID,
		ExecutionID:  jobID,
		RunID:        jobID,
		NodeID:       "agent-1",
		NodeType:     "prompt",
		Attempt:      1,
		Status:       "completed",
		CompletedAt:  &completedAt,
		LatencyMs:    12,
		TokensInput:  3,
		TokensOutput: 4,
		Cost:         0.0002,
		Metadata:     map[string]interface{}{"model": "mock/fast"},
	}); err != nil {
		t.Fatalf("seed node execution attempt: %v", err)
	}
	agentRunID := "agent-run-" + uuid.NewString()
	if err := app.store.UpsertAgentRun(ctx, &storage.AgentRun{
		ID:            agentRunID,
		JobID:         jobID,
		ExecutionID:   jobID,
		RunID:         jobID,
		NodeID:        "agent-run-1",
		Attempt:       1,
		RunKind:       "agent_run",
		ExternalRunID: "novomo-run-" + uuid.NewString(),
		Harness:       "claude-code",
		Status:        "completed",
		Output:        "done",
		StartedAt:     &completedAt,
		FinishedAt:    &completedAt,
	}); err != nil {
		t.Fatalf("seed agent run: %v", err)
	}

	nodes := app.request(http.MethodGet, "/api/admin/jobs/"+jobID+"/nodes?type=prompt&limit=1", nil)
	requireStatus(t, nodes, http.StatusOK)
	if decodeResponse(t, nodes)["Total"].(float64) != 1 {
		t.Fatalf("admin nodes should include seeded prompt node: %s", nodes.Body.String())
	}
	attempts := app.request(http.MethodGet, "/api/admin/jobs/"+jobID+"/node-execution-attempts", nil)
	requireStatus(t, attempts, http.StatusOK)
	if len(decodeResponse(t, attempts)["node_execution_attempts"].([]interface{})) != 1 {
		t.Fatalf("admin attempts should include seeded node attempt: %s", attempts.Body.String())
	}
	agentRuns := app.request(http.MethodGet, "/api/admin/jobs/"+jobID+"/agent-runs", nil)
	requireStatus(t, agentRuns, http.StatusOK)
	if len(decodeResponse(t, agentRuns)["agent_runs"].([]interface{})) != 1 {
		t.Fatalf("admin agent runs should include seeded agent run: %s", agentRuns.Body.String())
	}
	stopTerminalAgentRun := app.request(http.MethodPost, "/api/admin/jobs/"+jobID+"/agent-runs/"+agentRunID+"/stop", nil)
	requireStatus(t, stopTerminalAgentRun, http.StatusConflict)

	runningID := "admin-running-" + uuid.NewString()
	if err := app.store.CreateExecution(&storage.Job{
		ID:          runningID,
		Description: "running archive conflict",
		Model:       "workflow",
		Status:      events.JobStatusRunning,
	}); err != nil {
		t.Fatalf("seed running admin job: %v", err)
	}
	archiveRunning := app.request(http.MethodPost, "/api/admin/jobs/"+runningID+"/archive", nil)
	requireStatus(t, archiveRunning, http.StatusConflict)
	archiveCompleted := app.request(http.MethodPost, "/api/admin/jobs/"+jobID+"/archive", nil)
	requireStatus(t, archiveCompleted, http.StatusOK)
	archiveAgain := app.request(http.MethodPost, "/api/admin/jobs/"+jobID+"/archive", nil)
	requireStatus(t, archiveAgain, http.StatusOK)
	if decodeResponse(t, archiveAgain)["status"] != "already_archived" {
		t.Fatalf("second archive should be idempotent: %s", archiveAgain.Body.String())
	}
	unarchive := app.request(http.MethodPost, "/api/admin/jobs/"+jobID+"/unarchive", nil)
	requireStatus(t, unarchive, http.StatusOK)
	if decodeResponse(t, unarchive)["status"] != events.JobStatusCompleted {
		t.Fatalf("completed job should unarchive to completed: %s", unarchive.Body.String())
	}
	unarchiveAgain := app.request(http.MethodPost, "/api/admin/jobs/"+jobID+"/unarchive", nil)
	requireStatus(t, unarchiveAgain, http.StatusConflict)

	retryWorkflowID := "retry-wf-" + uuid.NewString()
	retrySnapshot, err := json.Marshal(map[string]interface{}{
		"id":   retryWorkflowID,
		"name": "Retry E2E",
		"nodes": []map[string]interface{}{
			{
				"id":              "retry-prompt",
				"type":            "prompt",
				"model":           "mock/fast",
				"prompt":          "Retry {{topic}}",
				"temperature":     0.0,
				"max_tokens":      32,
				"timeout_seconds": 30,
				"retry_policy":    retryPolicyMap(),
			},
		},
		"context": map[string]interface{}{"topic": "admin"},
	})
	if err != nil {
		t.Fatalf("marshal retry workflow snapshot: %v", err)
	}
	retryJobID := "admin-retry-" + uuid.NewString()
	if err := app.store.CreateExecution(&storage.Job{
		ID:          retryJobID,
		Description: "retry source",
		Model:       "workflow",
		Status:      events.JobStatusFailed,
		WorkflowID:  retryWorkflowID,
		RequestData: string(retrySnapshot),
	}); err != nil {
		t.Fatalf("seed failed retry job: %v", err)
	}
	pauseRetryAdmission := app.formRequest(http.MethodPost, "/api/admin/admission/pause", map[string]string{"message": "retry gate"})
	requireStatus(t, pauseRetryAdmission, http.StatusOK)
	retryBlocked := app.formRequest(http.MethodPost, "/api/admin/jobs/"+retryJobID+"/retry", nil)
	requireStatus(t, retryBlocked, http.StatusServiceUnavailable)
	if decodeResponse(t, retryBlocked)["code"] != "ADMISSION_PAUSED" {
		t.Fatalf("retry should expose admission pause code: %s", retryBlocked.Body.String())
	}
	retryBypass := app.formRequest(http.MethodPost, "/api/admin/jobs/"+retryJobID+"/retry", map[string]string{"admission_bypass": "1"})
	requireStatus(t, retryBypass, http.StatusOK)
	retryBody := decodeResponse(t, retryBypass)
	if retryBody["source_job_id"] != retryJobID || retryBody["job_id"] == "" || retryBody["admission_bypass"] != true {
		t.Fatalf("retry bypass response mismatch: %v", retryBody)
	}
	requireStatus(t, app.formRequest(http.MethodPost, "/api/admin/admission/resume", nil), http.StatusOK)

	baseRunID := seedBenchmarkRunVariant(t, app, "control-workflow", "Control Workflow", true, false)
	candidateRunID := seedBenchmarkRunVariant(t, app, "candidate-workflow", "Candidate Workflow", false, true)
	compare := app.request(http.MethodGet, "/api/admin/benchmarks/compare?benchmark=e2e-benchmark&split=dev&control=control-workflow", nil)
	requireStatus(t, compare, http.StatusOK)
	compareBody := decodeResponse(t, compare)
	if compareBody["control_id"] != baseRunID || len(compareBody["metrics"].([]interface{})) != 2 {
		t.Fatalf("benchmark comparison mismatch: %v", compareBody)
	}
	compareItems := app.request(http.MethodGet, "/api/admin/benchmarks/compare-items?base="+baseRunID+"&candidate="+candidateRunID+"&limit=1", nil)
	requireStatus(t, compareItems, http.StatusOK)
	compareItemsBody := decodeResponse(t, compareItems)
	summary := compareItemsBody["summary"].(map[string]interface{})
	if summary["total_compared"].(float64) != 2 || summary["regressed"].(float64) != 1 || summary["improved"].(float64) != 1 {
		t.Fatalf("benchmark compare-items summary mismatch: %v", compareItemsBody)
	}
	if compareItemsBody["returned_regressed"].(float64) != 1 || compareItemsBody["returned_improved"].(float64) != 1 {
		t.Fatalf("benchmark compare-items limit mismatch: %v", compareItemsBody)
	}
	runnerStatus := app.request(http.MethodGet, "/api/admin/benchmarks/runner-status", nil)
	requireStatus(t, runnerStatus, http.StatusOK)
	if decodeResponse(t, runnerStatus)["running"] == true {
		t.Fatalf("runner should be idle before start: %s", runnerStatus.Body.String())
	}
	importBenchmarks := app.request(http.MethodPost, "/api/admin/benchmarks/import", nil)
	requireStatus(t, importBenchmarks, http.StatusOK)
	importBody := decodeResponse(t, importBenchmarks)
	if importBody["imported_runs"].(float64) != 0 || importBody["failed_files"].(float64) != 0 {
		t.Fatalf("empty benchmark import should be a no-op: %v", importBody)
	}
	badBenchmarkStart := app.formRequest(http.MethodPost, "/api/admin/benchmarks/run", map[string]string{
		"benchmarks": "bad benchmark",
		"workflows":  "wf",
	})
	requireStatus(t, badBenchmarkStart, http.StatusBadRequest)
	cancelIdleRunner := app.request(http.MethodPost, "/api/admin/benchmarks/run/cancel", nil)
	requireStatus(t, cancelIdleRunner, http.StatusConflict)
	badRerunItem := app.formRequest(http.MethodPost, "/api/admin/benchmarks/"+baseRunID+"/rerun-failures", map[string]string{"item": "bad item"})
	requireStatus(t, badRerunItem, http.StatusBadRequest)
	badReplayMode := app.formRequest(http.MethodPost, "/api/admin/benchmarks/"+baseRunID+"/replay-items", map[string]string{
		"items": "row-1",
		"mode":  "impossible",
	})
	requireStatus(t, badReplayMode, http.StatusBadRequest)

	benchloopStatus := app.request(http.MethodGet, "/api/admin/benchloop/status", nil)
	requireStatus(t, benchloopStatus, http.StatusOK)
	benchloopStatusBody := decodeResponse(t, benchloopStatus)
	if benchloopStatusBody["active"] != false || len(benchloopStatusBody["sessions"].([]interface{})) != 0 {
		t.Fatalf("empty benchloop status mismatch: %v", benchloopStatusBody)
	}
	benchloopMemory := app.request(http.MethodGet, "/api/admin/benchloop/memory", nil)
	requireStatus(t, benchloopMemory, http.StatusOK)
	if decodeResponse(t, benchloopMemory)["content"] != "" {
		t.Fatalf("empty benchloop memory mismatch: %s", benchloopMemory.Body.String())
	}
	requireStatus(t, app.request(http.MethodGet, "/api/admin/benchloop/transcript", nil), http.StatusBadRequest)
	requireStatus(t, app.request(http.MethodGet, "/api/admin/benchloop/log", nil), http.StatusBadRequest)
	requireStatus(t, app.request(http.MethodGet, "/api/admin/benchloop/archive/missing-session", nil), http.StatusNotFound)
}

func TestE2EAdminOptimizationLifecycle(t *testing.T) {
	t.Parallel()
	app := newE2EApp(t)

	workflowID := "opt-e2e-wf-" + uuid.NewString()
	createWorkflow := app.request(http.MethodPost, "/api/workflows", workflowFile(workflowID))
	requireStatus(t, createWorkflow, http.StatusCreated)

	createRun := app.request(http.MethodPost, "/api/admin/optimize/runs", map[string]interface{}{
		"workflow_id":     workflowID,
		"benchmark":       "e2e-benchmark",
		"split":           "dev",
		"item_limit":      2,
		"concurrency":     1,
		"population_size": 2,
		"strategy":        "evolutionary",
		"mutator_mode":    "auto",
		"budget_usd":      1,
		"spec":            optimizeSpec(),
	})
	requireStatus(t, createRun, http.StatusCreated)
	runBody := decodeResponse(t, createRun)
	runID := runBody["id"].(string)
	if runBody["status"] != "pending" {
		t.Fatalf("created optimize run should be pending: %v", runBody)
	}

	heartbeat := app.request(http.MethodPatch, "/api/admin/optimize/runs/"+runID+"/heartbeat", map[string]interface{}{
		"owner_pid":      4242,
		"owner_hostname": "e2e-host",
	})
	requireStatus(t, heartbeat, http.StatusOK)

	resume := app.request(http.MethodPost, "/api/admin/optimize/runs/"+runID+"/resume", nil)
	requireStatus(t, resume, http.StatusOK)
	if decodeResponse(t, resume)["status"] != "running" {
		t.Fatalf("resume response mismatch: %s", resume.Body.String())
	}

	seedOrgID := "org-seed-e2e-" + uuid.NewString()
	createSeedOrg := app.request(http.MethodPost, "/api/admin/optimize/runs/"+runID+"/organisms", map[string]interface{}{
		"id":            seedOrgID,
		"generation":    0,
		"param_values":  map[string]string{"nodes[agent-1].data.config.model": "mock/fast"},
		"workflow_json": workflowFile(workflowID),
		"mutation_type": "seed",
		"fitness":       fitness(0.5),
	})
	requireStatus(t, createSeedOrg, http.StatusOK)

	orgID := "org-candidate-e2e-" + uuid.NewString()
	createCandidateOrg := app.request(http.MethodPost, "/api/admin/optimize/runs/"+runID+"/organisms", map[string]interface{}{
		"id":            orgID,
		"generation":    1,
		"parent_ids":    []string{seedOrgID},
		"param_values":  map[string]string{"nodes[agent-1].data.config.model": "mock/slow"},
		"workflow_json": workflowFile(workflowID),
		"mutation_type": "model",
		"fitness":       fitness(0.7),
	})
	requireStatus(t, createCandidateOrg, http.StatusOK)

	fitnessPatch := app.request(http.MethodPatch, "/api/admin/optimize/organisms/"+orgID+"/fitness", map[string]interface{}{
		"bench_run_id": "bench-linked-e2e",
		"fitness":      fitness(0.75),
	})
	requireStatus(t, fitnessPatch, http.StatusOK)

	progress := app.request(http.MethodPatch, "/api/admin/optimize/runs/"+runID+"/progress", map[string]interface{}{
		"generation":             1,
		"best_organism_id":       orgID,
		"best_fitness":           fitness(0.75),
		"spent_usd":              0.1,
		"total_organisms":        1,
		"dspy_metric_calls_used": 0,
	})
	requireStatus(t, progress, http.StatusOK)

	statusPatch := app.request(http.MethodPatch, "/api/admin/optimize/runs/"+runID+"/status", map[string]interface{}{"status": "running"})
	requireStatus(t, statusPatch, http.StatusOK)
	if decodeResponse(t, statusPatch)["status"] != "running" {
		t.Fatalf("status patch response mismatch: %s", statusPatch.Body.String())
	}

	listRuns := app.request(http.MethodGet, "/api/admin/optimize/runs?status=running&workflow="+workflowID, nil)
	requireStatus(t, listRuns, http.StatusOK)
	if got := len(decodeResponse(t, listRuns)["runs"].([]interface{})); got != 1 {
		t.Fatalf("optimize run filter count=%d want 1", got)
	}

	detail := app.request(http.MethodGet, "/api/admin/optimize/runs/"+runID, nil)
	requireStatus(t, detail, http.StatusOK)
	detailBody := decodeResponse(t, detail)
	if detailBody["best_organism_id"] != orgID || detailBody["generation"].(float64) != 1 {
		t.Fatalf("optimize run detail did not include progress: %v", detailBody)
	}

	organisms := app.request(http.MethodGet, "/api/admin/optimize/runs/"+runID+"/organisms?best=1&limit=1", nil)
	requireStatus(t, organisms, http.StatusOK)
	organismsBody := decodeResponse(t, organisms)
	if organismsBody["total"].(float64) != 1 ||
		organismsBody["organisms"].([]interface{})[0].(map[string]interface{})["id"] != orgID {
		t.Fatalf("expected one best organism: %s", organisms.Body.String())
	}

	orgDetail := app.request(http.MethodGet, "/api/admin/optimize/runs/"+runID+"/organisms/"+orgID, nil)
	requireStatus(t, orgDetail, http.StatusOK)
	if decodeResponse(t, orgDetail)["organism"].(map[string]interface{})["id"] != orgID {
		t.Fatalf("organism detail mismatch: %s", orgDetail.Body.String())
	}

	globalOrgDetail := app.request(http.MethodGet, "/api/admin/optimize/organisms/"+orgID, nil)
	requireStatus(t, globalOrgDetail, http.StatusOK)
	if decodeResponse(t, globalOrgDetail)["organism"].(map[string]interface{})["id"] != orgID {
		t.Fatalf("global organism detail mismatch: %s", globalOrgDetail.Body.String())
	}

	lineage := app.request(http.MethodGet, "/api/admin/optimize/runs/"+runID+"/lineage", nil)
	requireStatus(t, lineage, http.StatusOK)
	if len(decodeResponse(t, lineage)["nodes"].([]interface{})) != 2 {
		t.Fatalf("lineage should include organism: %s", lineage.Body.String())
	}

	globalLineage := app.request(http.MethodGet, "/api/admin/optimize/organisms/"+orgID+"/lineage", nil)
	requireStatus(t, globalLineage, http.StatusOK)
	if len(decodeResponse(t, globalLineage)["organisms"].([]interface{})) != 2 {
		t.Fatalf("global lineage should include parent and child: %s", globalLineage.Body.String())
	}

	changes := app.request(http.MethodPost, "/api/admin/optimize/organisms/"+orgID+"/param-changes", map[string]interface{}{
		"changes": []interface{}{
			map[string]interface{}{"path": "nodes[agent-1].data.config.model", "old_value": "mock/slow", "new_value": "mock/fast", "reason": "seed"},
		},
	})
	requireStatus(t, changes, http.StatusOK)
	getChanges := app.request(http.MethodGet, "/api/admin/optimize/organisms/"+orgID+"/param-changes", nil)
	requireStatus(t, getChanges, http.StatusOK)
	if len(decodeResponse(t, getChanges)["changes"].([]interface{})) != 1 {
		t.Fatalf("param changes were not persisted: %s", getChanges.Body.String())
	}

	artifacts := app.request(http.MethodPost, "/api/admin/optimize/organisms/"+orgID+"/mutation-artifacts", map[string]interface{}{
		"artifacts": []interface{}{
			map[string]interface{}{"input_prompt_hash": "in", "input_prompt": "prompt", "raw_output_hash": "out", "raw_output": "response", "claude_model": "e2e"},
		},
	})
	requireStatus(t, artifacts, http.StatusOK)
	getArtifacts := app.request(http.MethodGet, "/api/admin/optimize/organisms/"+orgID+"/mutation-artifacts", nil)
	requireStatus(t, getArtifacts, http.StatusOK)
	if len(decodeResponse(t, getArtifacts)["artifacts"].([]interface{})) != 1 {
		t.Fatalf("mutation artifacts were not persisted: %s", getArtifacts.Body.String())
	}

	learn := app.request(http.MethodPost, "/api/admin/optimize/runs/"+runID+"/learning-log", map[string]interface{}{
		"generation":    1,
		"organism_id":   orgID,
		"parent_id":     orgID,
		"mutation_type": "model",
		"description":   "e2e learned",
		"outcome":       "improvement",
		"fitness_delta": 0.25,
	})
	requireStatus(t, learn, http.StatusOK)
	learningLog := app.request(http.MethodGet, "/api/admin/optimize/runs/"+runID+"/learning-log?limit=10", nil)
	requireStatus(t, learningLog, http.StatusOK)
	if len(decodeResponse(t, learningLog)["entries"].([]interface{})) != 1 {
		t.Fatalf("learning log missing entry: %s", learningLog.Body.String())
	}

	secondRun := app.request(http.MethodPost, "/api/admin/optimize/runs", map[string]interface{}{
		"workflow_id":     workflowID,
		"benchmark":       "e2e-benchmark",
		"split":           "dev",
		"item_limit":      2,
		"concurrency":     1,
		"population_size": 2,
		"strategy":        "evolutionary",
		"mutator_mode":    "auto",
		"budget_usd":      1,
		"spec":            optimizeSpec(),
	})
	requireStatus(t, secondRun, http.StatusCreated)
	secondRunID := decodeResponse(t, secondRun)["id"].(string)
	compare := app.request(http.MethodGet, "/api/admin/optimize/compare?ids="+runID+","+secondRunID, nil)
	requireStatus(t, compare, http.StatusOK)
	if len(decodeResponse(t, compare)["runs"].([]interface{})) != 2 {
		t.Fatalf("optimize compare should include two runs: %s", compare.Body.String())
	}

	promote := app.request(http.MethodPost, "/api/admin/optimize/runs/"+runID+"/promote", nil)
	requireStatus(t, promote, http.StatusOK)
	promoteBody := decodeResponse(t, promote)
	if promoteBody["promoted"] != true || promoteBody["organism_id"] != orgID {
		t.Fatalf("promote response mismatch: %v", promoteBody)
	}

	pause := app.request(http.MethodPost, "/api/admin/optimize/runs/"+runID+"/pause", nil)
	requireStatus(t, pause, http.StatusOK)
	if decodeResponse(t, pause)["status"] != "paused" {
		t.Fatalf("pause response mismatch: %s", pause.Body.String())
	}

	cancel := app.request(http.MethodPost, "/api/admin/optimize/runs/"+runID+"/cancel", nil)
	requireStatus(t, cancel, http.StatusOK)
	if decodeResponse(t, cancel)["status"] != "cancelled" {
		t.Fatalf("cancel response mismatch: %s", cancel.Body.String())
	}
}
