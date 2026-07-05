package admin

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/bench"
	"github.com/alhasaniq/consortium/pkg/jobs"
	"github.com/alhasaniq/consortium/pkg/optimize"
	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/gorilla/mux"
)

// setupTestDB creates an in-memory SQLite database for testing
func setupTestDB(t *testing.T) (*storage.Storage, *sql.DB, func()) {
	t.Helper()

	store, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	db := store.DB()

	cleanup := func() {
		store.Close()
	}

	return store, db, cleanup
}

// setupTestServer creates a test admin server
func setupTestServer(t *testing.T) (*Server, *mux.Router, func()) {
	t.Helper()

	store, db, cleanup := setupTestDB(t)
	registry := providers.NewRegistry()

	mgr := newTestJobManager(store, registry)
	mgr.StartWorkers()

	server := NewServer(store, db, registry, mgr, "")
	router := mux.NewRouter()
	server.RegisterRoutes(router)

	return server, router, func() {
		mgr.StopWorkers(context.Background())
		cleanup()
	}
}

func newTestJobManager(store *storage.Storage, registry *providers.Registry) *jobs.Manager {
	cfg := jobs.DefaultManagerConfig()
	cfg.MaxConcurrentWorkflows = 4
	cfg.WorkerCount = 4
	cfg.WorkerPollInterval = 2 * time.Millisecond
	return jobs.NewManagerWithConfig(store, registry, cfg)
}

func TestHandleOverview(t *testing.T) {
	_, router, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/admin/overview", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Expected valid JSON response: %v", err)
	}
}

func TestLoadOverviewStats_EmptyJobsDoesNotLogError(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	var logBuf bytes.Buffer
	origWriter := log.Writer()
	origPrefix := log.Prefix()
	origFlags := log.Flags()
	log.SetOutput(&logBuf)
	log.SetPrefix("")
	log.SetFlags(0)
	defer func() {
		log.SetOutput(origWriter)
		log.SetPrefix(origPrefix)
		log.SetFlags(origFlags)
	}()

	stats := server.loadOverviewStats()
	if stats.TotalJobs != 0 || stats.CompletedJobs != 0 || stats.FailedJobs != 0 {
		t.Fatalf("expected zero job counters, got total=%d completed=%d failed=%d", stats.TotalJobs, stats.CompletedJobs, stats.FailedJobs)
	}
	if strings.Contains(logBuf.String(), "Error getting job stats") {
		t.Fatalf("expected no overview scan error log, got: %s", logBuf.String())
	}
}

func TestOverviewStatsQueryUsesCoveringIndex(t *testing.T) {
	_, db, cleanup := setupTestDB(t)
	defer cleanup()

	rows, err := db.Query(`
		EXPLAIN QUERY PLAN
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(cost), 0),
		       COALESCE(SUM(tokens_total), 0)
		FROM jobs
	`)
	if err != nil {
		t.Fatalf("explain overview stats query: %v", err)
	}
	defer func() { _ = rows.Close() }()

	var planParts []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("scan query plan row: %v", err)
		}
		planParts = append(planParts, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate query plan rows: %v", err)
	}

	plan := strings.Join(planParts, "\n")
	if !strings.Contains(plan, "COVERING INDEX idx_jobs_overview_stats_cover") {
		t.Fatalf("overview stats query should use covering index, got plan:\n%s", plan)
	}
}

func TestHandleJobs(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	// Create a test job
	job := &storage.Job{
		ID:          "test-job-1",
		Description: "Test query",
		Model:       "test-model",
		Status:      "completed",
		TokensTotal: 100,
		Cost:        0.01,
	}
	if err := server.storage.CreateExecution(job); err != nil {
		t.Fatalf("Failed to create test job: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/admin/jobs", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Expected valid JSON response: %v", err)
	}
}

func TestHandleBulkJobControls(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	t.Run("pause all pending", func(t *testing.T) {
		if err := server.storage.CreateExecution(&storage.Job{
			ID:          "pause-pending-1",
			Description: "pending 1",
			Model:       "workflow",
			Status:      "pending",
		}); err != nil {
			t.Fatalf("Failed to create pending job: %v", err)
		}
		if err := server.storage.CreateExecution(&storage.Job{
			ID:          "pause-pending-2",
			Description: "pending 2",
			Model:       "workflow",
			Status:      "pending",
		}); err != nil {
			t.Fatalf("Failed to create pending job: %v", err)
		}

		req := httptest.NewRequest("POST", "/api/admin/jobs/pause-all", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		for _, jobID := range []string{"pause-pending-1", "pause-pending-2"} {
			job, err := server.storage.GetExecution(jobID)
			if err != nil {
				t.Fatalf("Failed to reload %s: %v", jobID, err)
			}
			if job.Status != "paused" {
				t.Fatalf("Expected %s paused, got %s", jobID, job.Status)
			}
		}
	})

	t.Run("resume all paused", func(t *testing.T) {
		if err := server.storage.CreateExecution(&storage.Job{
			ID:          "resume-paused-1",
			Description: "paused 1",
			Model:       "workflow",
			Status:      "paused",
		}); err != nil {
			t.Fatalf("Failed to create paused job: %v", err)
		}
		if err := server.storage.CreateExecution(&storage.Job{
			ID:          "resume-paused-2",
			Description: "paused 2",
			Model:       "workflow",
			Status:      "paused",
		}); err != nil {
			t.Fatalf("Failed to create paused job: %v", err)
		}

		req := httptest.NewRequest("POST", "/api/admin/jobs/resume-all", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		for _, jobID := range []string{"resume-paused-1", "resume-paused-2"} {
			job, err := server.storage.GetExecution(jobID)
			if err != nil {
				t.Fatalf("Failed to reload %s: %v", jobID, err)
			}
			if job.Status != "pending" {
				t.Fatalf("Expected %s pending, got %s", jobID, job.Status)
			}
		}
	})

	t.Run("cancel all queued", func(t *testing.T) {
		if err := server.storage.CreateExecution(&storage.Job{
			ID:          "cancel-pending-1",
			Description: "pending",
			Model:       "workflow",
			Status:      "pending",
		}); err != nil {
			t.Fatalf("Failed to create pending job: %v", err)
		}
		if err := server.storage.CreateExecution(&storage.Job{
			ID:          "cancel-paused-1",
			Description: "paused",
			Model:       "workflow",
			Status:      "paused",
		}); err != nil {
			t.Fatalf("Failed to create paused job: %v", err)
		}

		req := httptest.NewRequest("POST", "/api/admin/jobs/cancel-all", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		for _, jobID := range []string{"cancel-pending-1", "cancel-paused-1"} {
			job, err := server.storage.GetExecution(jobID)
			if err != nil {
				t.Fatalf("Failed to reload %s: %v", jobID, err)
			}
			if job.Status != "cancelled" {
				t.Fatalf("Expected %s cancelled, got %s", jobID, job.Status)
			}
			if strings.TrimSpace(job.ErrorMessage) == "" {
				t.Fatalf("Expected %s to include cancellation message", jobID)
			}
		}
	})
}

func TestHandleStopJobAgentRun(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	if err := server.storage.CreateExecution(&storage.Job{
		ID:          "job-agent-stop",
		Description: "running agent job",
		Model:       "workflow",
		Status:      "running",
	}); err != nil {
		t.Fatalf("create job: %v", err)
	}

	row := &storage.AgentRun{
		ID:            "job-agent-stop:run-1:agent-node:1",
		JobID:         "job-agent-stop",
		ExecutionID:   "job-agent-stop",
		RunID:         "run-1",
		NodeID:        "agent-node",
		Attempt:       1,
		RunKind:       "agent_run",
		ExternalRunID: "novomo-active",
		Harness:       "claude-code",
		Status:        "running",
	}
	if err := server.storage.UpsertAgentRun(context.Background(), row); err != nil {
		t.Fatalf("upsert agent run: %v", err)
	}

	var gotPath string
	novomoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run_id":"novomo-active","status":"failed"}`))
	}))
	defer novomoServer.Close()
	t.Setenv("NOVOMO_URL", novomoServer.URL)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/admin/jobs/job-agent-stop/agent-runs/"+url.PathEscape(row.ID)+"/stop",
		nil,
	)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}
	if gotPath != "/v1/runs/novomo-active/stop" {
		t.Fatalf("unexpected Novomo stop path %q", gotPath)
	}
	updated, err := server.storage.GetAgentRunByID(context.Background(), "job-agent-stop", row.ID)
	if err != nil || updated == nil {
		t.Fatalf("reload agent run: row=%+v err=%v", updated, err)
	}
	if updated.Status != "cancelled" {
		t.Fatalf("expected cancelled local row, got %+v", updated)
	}
}

func TestHandleStopJobAgentRunMapsNotStoppableConflict(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	if err := server.storage.CreateExecution(&storage.Job{
		ID:          "job-agent-stop-conflict",
		Description: "running agent job",
		Model:       "workflow",
		Status:      "running",
	}); err != nil {
		t.Fatalf("create job: %v", err)
	}

	row := &storage.AgentRun{
		ID:            "job-agent-stop-conflict:run-1:agent-node:1",
		JobID:         "job-agent-stop-conflict",
		ExecutionID:   "job-agent-stop-conflict",
		RunID:         "run-1",
		NodeID:        "agent-node",
		Attempt:       1,
		RunKind:       "agent_run",
		ExternalRunID: "novomo-terminal",
		Harness:       "claude-code",
		Status:        "running",
	}
	if err := server.storage.UpsertAgentRun(context.Background(), row); err != nil {
		t.Fatalf("upsert agent run: %v", err)
	}

	novomoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"not_stoppable"}`))
	}))
	defer novomoServer.Close()
	t.Setenv("NOVOMO_URL", novomoServer.URL)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/admin/jobs/job-agent-stop-conflict/agent-runs/"+url.PathEscape(row.ID)+"/stop",
		nil,
	)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d. Body: %s", http.StatusConflict, w.Code, w.Body.String())
	}
}

func TestHandleStopJobAgentRunReturnsNotFoundForMissingRow(t *testing.T) {
	_, router, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/admin/jobs/job-missing/agent-runs/missing-run/stop",
		nil,
	)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d. Body: %s", http.StatusNotFound, w.Code, w.Body.String())
	}
}

func TestExtractInputPrompt(t *testing.T) {
	requestData := `{"context":{"user_prompt":"Solve this","prompt":"fallback"}}`
	if got := extractInputPrompt(requestData); got != "Solve this" {
		t.Fatalf("expected user_prompt to be preferred, got %q", got)
	}
}

func TestRenderPromptTemplate(t *testing.T) {
	context := map[string]interface{}{
		"user_prompt": "Explain retries",
	}
	got := workflow.InterpolateVariables("Task: {{user_prompt}} :: {{missing}}", context)
	if got != "Task: Explain retries :: {{missing}}" {
		t.Fatalf("unexpected render result: %q", got)
	}
}

func TestExtractNodeConfigsFromSnapshot(t *testing.T) {
	snapshot := `{
		"nodes": [
			{
				"id": "step_0",
				"type": "prompt",
				"model": "openrouter/test",
				"prompt": "Hello {{user_prompt}}",
				"max_tokens": 256,
				"temperature": 0.2
			}
		]
	}`

	configs := extractFromSnapshot(snapshot).Configs
	cfg := configs["step_0"]
	if cfg == "" {
		t.Fatalf("expected config for step_0")
	}
	if !strings.Contains(cfg, `"max_tokens": 256`) {
		t.Fatalf("expected max_tokens in config, got: %s", cfg)
	}
	if strings.Contains(cfg, `"prompt":`) {
		t.Fatalf("did not expect prompt in extracted config, got: %s", cfg)
	}
}

func TestHandleJobDetail(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	t.Run("existing job", func(t *testing.T) {
		// Create a test job
		job := &storage.Job{
			ID:          "test-job-detail",
			Description: "Test query",
			Model:       "test-model",
			Status:      "completed",
			TokensTotal: 100,
			Cost:        0.01,
		}
		if err := server.storage.CreateExecution(job); err != nil {
			t.Fatalf("Failed to create test job: %v", err)
		}

		req := httptest.NewRequest("GET", "/api/admin/jobs/test-job-detail", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("non-existent job", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/jobs/non-existent-job", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
		}
	})

	t.Run("child workflow node shows metadata cost", func(t *testing.T) {
		job := &storage.Job{
			ID:          "test-job-detail-child-cost",
			Description: "Child workflow display cost",
			Model:       "test-model",
			Status:      "completed",
			TokensTotal: 10,
			Cost:        0.01,
		}
		if err := server.storage.CreateExecution(job); err != nil {
			t.Fatalf("Failed to create job: %v", err)
		}

		node := &storage.WorkflowNode{
			ExecutionID: "test-job-detail-child-cost",
			NodeID:      "child_step_0",
			NodeType:    "child_workflow",
			NodeOrder:   0,
			Status:      "completed",
			Cost:        0.0,
			LatencyMs:   42,
			Metadata:    `{"child_job_id":"child-job-xyz","child_total_cost":0.6543}`,
		}
		if err := server.storage.AddWorkflowNode(node); err != nil {
			t.Fatalf("Failed to add node: %v", err)
		}

		req := httptest.NewRequest("GET", "/api/admin/jobs/test-job-detail-child-cost", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
		if !strings.Contains(w.Body.String(), "0.6543") {
			t.Fatalf("Expected child metadata cost in job detail response")
		}
	})

	t.Run("interpolates node prompts with request context", func(t *testing.T) {
		job := &storage.Job{
			ID:          "test-job-detail-prompt-template",
			Description: "Prompt interpolation",
			Model:       "test-model",
			Status:      "completed",
			TokensTotal: 1,
			Cost:        0.0,
			RequestData: `{"context":{"user_prompt":"What is 2+2?\nShow your work."}}`,
		}
		if err := server.storage.CreateExecution(job); err != nil {
			t.Fatalf("Failed to create job: %v", err)
		}

		node := &storage.WorkflowNode{
			ExecutionID:  "test-job-detail-prompt-template",
			NodeID:       "step_0",
			NodeType:     "prompt",
			NodeOrder:    0,
			Status:       "completed",
			Prompt:       "Question:\n{{user_prompt}}",
			Model:        "test-model",
			Output:       "4",
			TokensInput:  1,
			TokensOutput: 1,
			LatencyMs:    5,
		}
		if err := server.storage.AddWorkflowNode(node); err != nil {
			t.Fatalf("Failed to add node: %v", err)
		}

		req := httptest.NewRequest("GET", "/api/admin/jobs/test-job-detail-prompt-template", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Expected valid JSON response: %v", err)
		}
		// Verify that NodesWithMetrics contains the interpolated prompt
		nodesRaw, ok := resp["NodesWithMetrics"].([]interface{})
		if !ok || len(nodesRaw) == 0 {
			t.Fatalf("Expected NodesWithMetrics in response")
		}
		firstNode := nodesRaw[0].(map[string]interface{})
		prompt, _ := firstNode["prompt"].(string)
		if !strings.Contains(prompt, "What is 2+2?") {
			t.Fatalf("Expected interpolated prompt, got: %s", prompt)
		}
		if strings.Contains(prompt, "{{user_prompt}}") {
			t.Fatalf("Did not expect unresolved {{user_prompt}} in NodesWithMetrics prompt")
		}
	})

	t.Run("interpolates inter-node references in prompts", func(t *testing.T) {
		job := &storage.Job{
			ID:          "test-job-detail-internode-ref",
			Description: "Inter-node interpolation",
			Model:       "test-model",
			Status:      "completed",
			TokensTotal: 2,
			Cost:        0.0,
			RequestData: `{"context":{"user_prompt":"Summarize gravity"}}`,
		}
		if err := server.storage.CreateExecution(job); err != nil {
			t.Fatalf("Failed to create job: %v", err)
		}

		// First node produces output that the second node references.
		node1 := &storage.WorkflowNode{
			ExecutionID:  "test-job-detail-internode-ref",
			NodeID:       "analyze",
			NodeType:     "prompt",
			NodeOrder:    0,
			Status:       "completed",
			Prompt:       "Analyze: {{user_prompt}}",
			Model:        "test-model",
			Output:       "Gravity is a fundamental force.",
			TokensInput:  1,
			TokensOutput: 1,
			LatencyMs:    5,
		}
		node2 := &storage.WorkflowNode{
			ExecutionID:  "test-job-detail-internode-ref",
			NodeID:       "refine",
			NodeType:     "prompt",
			NodeOrder:    1,
			Status:       "completed",
			Prompt:       "Refine this analysis: {{analyze}}",
			Model:        "test-model",
			Output:       "Refined output",
			TokensInput:  1,
			TokensOutput: 1,
			LatencyMs:    5,
		}
		if err := server.storage.AddWorkflowNode(node1); err != nil {
			t.Fatalf("Failed to add node: %v", err)
		}
		if err := server.storage.AddWorkflowNode(node2); err != nil {
			t.Fatalf("Failed to add node: %v", err)
		}

		req := httptest.NewRequest("GET", "/api/admin/jobs/test-job-detail-internode-ref", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Expected valid JSON response: %v", err)
		}
		nodesRaw, ok := resp["NodesWithMetrics"].([]interface{})
		if !ok || len(nodesRaw) < 2 {
			t.Fatalf("Expected at least 2 nodes in NodesWithMetrics, got %d", len(nodesRaw))
		}
		// Second node's prompt should have {{analyze}} resolved to the first node's output.
		secondNode := nodesRaw[1].(map[string]interface{})
		prompt, _ := secondNode["prompt"].(string)
		if !strings.Contains(prompt, "Gravity is a fundamental force.") {
			t.Fatalf("Expected inter-node reference resolved in prompt, got: %s", prompt)
		}
		if strings.Contains(prompt, "{{analyze}}") {
			t.Fatalf("Did not expect unresolved {{analyze}} in prompt")
		}
	})

	t.Run("shows node config from dag snapshot", func(t *testing.T) {
		job := &storage.Job{
			ID:                  "test-job-detail-node-config",
			Description:         "Node config rendering",
			Model:               "test-model",
			Status:              "completed",
			TokensTotal:         1,
			Cost:                0.0,
			DAGSnapshot:         `{"nodes":[{"id":"step_0","type":"prompt","model":"openrouter/test","max_tokens":512,"temperature":0.42,"prompt":"Hello"}]}`,
			DAGHash:             "testhash1",
			WorkflowExecutionID: "test-job-detail-node-config",
			RunID:               "test-job-detail-node-config",
		}
		if err := server.storage.CreateExecution(job); err != nil {
			t.Fatalf("Failed to create job: %v", err)
		}

		node := &storage.WorkflowNode{
			ExecutionID:  "test-job-detail-node-config",
			NodeID:       "step_0",
			NodeType:     "prompt",
			NodeOrder:    0,
			Status:       "completed",
			Prompt:       "Hello",
			Model:        "openrouter/test",
			Output:       "ok",
			TokensInput:  1,
			TokensOutput: 1,
			LatencyMs:    2,
		}
		if err := server.storage.AddWorkflowNode(node); err != nil {
			t.Fatalf("Failed to add node: %v", err)
		}

		req := httptest.NewRequest("GET", "/api/admin/jobs/test-job-detail-node-config", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
		body := w.Body.String()
		if !strings.Contains(body, "max_tokens") || !strings.Contains(body, "0.42") {
			t.Fatalf("Expected dag snapshot config values in response body")
		}
	})

	t.Run("shows lifecycle timing from durable history", func(t *testing.T) {
		const jobID = "test-job-detail-lifecycle"
		job := &storage.Job{
			ID:                  jobID,
			Description:         "Lifecycle timing test",
			Model:               "workflow",
			Status:              "completed",
			TokensTotal:         42,
			Cost:                0.02,
			RunID:               jobID,
			WorkflowExecutionID: jobID,
			RunNumber:           2,
			DAGHash:             "lifecycle-hash",
			DAGSnapshot:         `{"nodes":[]}`,
		}
		if err := server.storage.CreateExecution(job); err != nil {
			t.Fatalf("Failed to create lifecycle job: %v", err)
		}

		submittedAt := time.Date(2026, 2, 11, 9, 0, 0, 0, time.UTC)
		completedAt := submittedAt.Add(30 * time.Second)
		if _, err := server.db.Exec(`
				UPDATE jobs
				SET created_at = ?, updated_at = ?, run_id = ?, workflow_execution_id = ?, run_number = ?
				WHERE id = ?
			`, submittedAt, completedAt, jobID, jobID, 2, jobID); err != nil {
			t.Fatalf("Failed to normalize lifecycle job timestamps: %v", err)
		}

		events := []struct {
			seq        int
			eventType  string
			nodeID     interface{}
			activityID interface{}
			ts         time.Time
		}{
			{seq: 1, eventType: "workflow_started", nodeID: nil, activityID: nil, ts: submittedAt.Add(5 * time.Second)},
			{seq: 2, eventType: "activity_started", nodeID: "step-0", activityID: "step-0:1", ts: submittedAt.Add(8 * time.Second)},
			{seq: 3, eventType: "activity_completed", nodeID: "step-0", activityID: "step-0:1", ts: submittedAt.Add(18 * time.Second)},
			{seq: 4, eventType: "workflow_completed", nodeID: nil, activityID: nil, ts: completedAt},
		}
		for _, ev := range events {
			if _, err := server.db.Exec(`
					INSERT INTO execution_history (run_id, sequence, event_type, node_id, activity_id, timestamp, attributes, created_at)
					VALUES (?, ?, ?, ?, ?, ?, '{}', ?)
				`, jobID, ev.seq, ev.eventType, ev.nodeID, ev.activityID, ev.ts, ev.ts); err != nil {
				t.Fatalf("Failed to insert history event %d: %v", ev.seq, err)
			}
		}

		if _, err := server.db.Exec(`
				INSERT INTO workflow_node_execution_attempts (
					job_id, execution_id, run_id, node_id, node_type, attempt_number, status,
					started_at, completed_at, latency_ms, tokens_input, tokens_output, cost,
					error_code, error_message, metadata, execution_uid, created_at, updated_at
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '{}', ?, ?, ?)
			`, jobID, jobID, jobID, "step-0", "prompt", 1, "completed",
			submittedAt.Add(8*time.Second), submittedAt.Add(18*time.Second), 10000.0, 24, 18, 0.02,
			"", "", jobID+":step-0:1", submittedAt.Add(8*time.Second), submittedAt.Add(18*time.Second)); err != nil {
			t.Fatalf("Failed to insert node execution attempt: %v", err)
		}

		req := httptest.NewRequest("GET", "/api/admin/jobs/"+jobID, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Expected valid JSON response: %v", err)
		}
		lifecycle, ok := resp["Lifecycle"].(map[string]interface{})
		if !ok {
			t.Fatalf("Expected Lifecycle in response")
		}
		queueDelay, _ := lifecycle["QueueDelayMs"].(float64)
		if queueDelay < 4000 || queueDelay > 6000 {
			t.Fatalf("Expected QueueDelayMs ~5000, got %f", queueDelay)
		}
	})

	t.Run("normalizes stale running attempts for terminal jobs", func(t *testing.T) {
		const jobID = "test-job-detail-stale-running"
		job := &storage.Job{
			ID:                  jobID,
			Description:         "Stale running attempt normalization",
			Model:               "workflow",
			Status:              "completed",
			TokensTotal:         10,
			Cost:                0.01,
			RunID:               jobID,
			WorkflowExecutionID: jobID,
			DAGHash:             "stale-hash",
			DAGSnapshot:         `{"nodes":[]}`,
		}
		if err := server.storage.CreateExecution(job); err != nil {
			t.Fatalf("Failed to create stale-running job: %v", err)
		}

		base := time.Date(2026, 2, 11, 10, 0, 0, 0, time.UTC)
		if _, err := server.db.Exec(`
				UPDATE jobs
				SET created_at = ?, updated_at = ?, run_id = ?, workflow_execution_id = ?
				WHERE id = ?
			`, base, base.Add(2*time.Minute), jobID, jobID, jobID); err != nil {
			t.Fatalf("Failed to normalize stale-running job timestamps: %v", err)
		}

		// Attempt 1 is left as running, then attempt 2 completed.
		if _, err := server.db.Exec(`
				INSERT INTO workflow_node_execution_attempts (
					job_id, execution_id, run_id, node_id, node_type, attempt_number, status,
					started_at, completed_at, latency_ms, tokens_input, tokens_output, cost,
					error_code, error_message, metadata, execution_uid, created_at, updated_at
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '{}', ?, ?, ?)
			`, jobID, jobID, jobID, "stale-node", "child_workflow", 1, "running",
			base.Add(5*time.Second), nil, 0.0, 0, 0, 0.0,
			"", "", jobID+":stale-node:1", base.Add(5*time.Second), base.Add(5*time.Second)); err != nil {
			t.Fatalf("Failed to insert stale running attempt: %v", err)
		}
		if _, err := server.db.Exec(`
				INSERT INTO workflow_node_execution_attempts (
					job_id, execution_id, run_id, node_id, node_type, attempt_number, status,
					started_at, completed_at, latency_ms, tokens_input, tokens_output, cost,
					error_code, error_message, metadata, execution_uid, created_at, updated_at
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '{}', ?, ?, ?)
			`, jobID, jobID, jobID, "stale-node", "child_workflow", 2, "completed",
			base.Add(70*time.Second), base.Add(95*time.Second), 25000.0, 0, 0, 0.0,
			"", "", jobID+":stale-node:2", base.Add(70*time.Second), base.Add(95*time.Second)); err != nil {
			t.Fatalf("Failed to insert completed retry attempt: %v", err)
		}

		req := httptest.NewRequest("GET", "/api/admin/jobs/"+jobID, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
		body := w.Body.String()
		if !strings.Contains(body, `"retrying"`) {
			t.Fatalf("Expected stale running attempt to be normalized to retrying in JSON")
		}
	})
}

func TestHandleJobNodes(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	// Create a test job with workflow nodes
	job := &storage.Job{
		ID:                  "test-job-nodes",
		Description:         "Test query",
		Model:               "test-model",
		Status:              "completed",
		TokensTotal:         100,
		Cost:                0.01,
		RequestData:         `{"context":{"user_prompt":"node filter prompt"}}`,
		DAGSnapshot:         `{"nodes":[{"id":"step_0","type":"prompt","model":"test-model","max_tokens":128,"temperature":0.5,"prompt":"ignored here"},{"id":"step_1","type":"child_workflow","child_workflow_id":"child-id"}]}`,
		DAGHash:             "testhash2",
		WorkflowExecutionID: "test-job-nodes",
		RunID:               "test-job-nodes",
	}
	if err := server.storage.CreateExecution(job); err != nil {
		t.Fatalf("Failed to create test job: %v", err)
	}

	// Add workflow nodes
	node := &storage.WorkflowNode{
		ExecutionID:  "test-job-nodes",
		NodeID:       "step_0",
		NodeType:     "prompt",
		NodeOrder:    0,
		Status:       "completed",
		Prompt:       "Test prompt:\n{{user_prompt}}",
		Model:        "test-model",
		Output:       "Test output",
		TokensInput:  50,
		TokensOutput: 50,
		Cost:         0.01,
		LatencyMs:    100,
	}
	if err := server.storage.AddWorkflowNode(node); err != nil {
		t.Fatalf("Failed to add workflow node: %v", err)
	}

	childWorkflowNode := &storage.WorkflowNode{
		ExecutionID: "test-job-nodes",
		NodeID:      "step_1",
		NodeType:    "child_workflow",
		NodeOrder:   1,
		Status:      "completed",
		Metadata:    `{"child_job_id":"child-job-1","child_total_cost":0.4321}`,
		Cost:        0.0,
		LatencyMs:   50,
	}
	if err := server.storage.AddWorkflowNode(childWorkflowNode); err != nil {
		t.Fatalf("Failed to add child workflow node: %v", err)
	}

	t.Run("get all nodes", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/jobs/test-job-nodes/nodes", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "0.4321") {
			t.Fatalf("Expected child workflow display cost from metadata, body: %s", body)
		}
	})

	t.Run("filter by status", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/jobs/test-job-nodes/nodes?status=completed", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("search filter", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/jobs/test-job-nodes/nodes?search=test", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("type filter", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/jobs/test-job-nodes/nodes?type=prompt", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("renders interpolated prompt", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/jobs/test-job-nodes/nodes", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "node filter prompt") {
			t.Fatalf("Expected interpolated prompt in node list response")
		}
		if strings.Contains(body, "{{user_prompt}}") {
			t.Fatalf("Did not expect unresolved {{user_prompt}} placeholder in node list response")
		}
	})

	t.Run("renders node config", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/jobs/test-job-nodes/nodes", nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "max_tokens") || !strings.Contains(body, "128") {
			t.Fatalf("Expected node config values from dag snapshot in node list response")
		}
	})
}

func TestApplyChildWorkflowDisplayCost(t *testing.T) {
	t.Run("uses child_total_cost from metadata for child_workflow node", func(t *testing.T) {
		node := storage.WorkflowNode{
			NodeType: "child_workflow",
			Cost:     0.0,
			Metadata: `{"child_job_id":"child-123","child_total_cost":1.2345}`,
		}

		childJobID := applyChildWorkflowDisplayCost(&node)

		if childJobID != "child-123" {
			t.Fatalf("expected child job id child-123, got %q", childJobID)
		}
		if node.Cost != 1.2345 {
			t.Fatalf("expected display cost 1.2345, got %f", node.Cost)
		}
	})

	t.Run("does not overwrite cost for non-child nodes", func(t *testing.T) {
		node := storage.WorkflowNode{
			NodeType: "prompt",
			Cost:     0.09,
			Metadata: `{"child_total_cost":1.2345}`,
		}

		_ = applyChildWorkflowDisplayCost(&node)

		if node.Cost != 0.09 {
			t.Fatalf("expected cost to remain 0.09, got %f", node.Cost)
		}
	})
}

func TestHandleTestWorkflow(t *testing.T) {
	_, router, cleanup := setupTestServer(t)
	defer cleanup()

	t.Run("invalid request body", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/admin/test/workflow", bytes.NewBufferString("invalid json"))
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
		}
	})

	t.Run("valid workflow request", func(t *testing.T) {
		wf := workflow.Workflow{
			ID:   "test-workflow",
			Name: "Test Workflow",
			Nodes: []*workflow.Node{
				{
					ID:          "step_0",
					Type:        workflow.NodeTypePrompt,
					Prompt:      "Hello",
					Model:       "test/mock-model",
					RetryPolicy: &workflow.RetryPolicy{MaxAttempts: 1},
				},
			},
		}

		reqBody := struct {
			Workflow workflow.Workflow `json:"workflow"`
		}{
			Workflow: wf,
		}

		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/api/admin/test/workflow", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		// This will fail because no provider is registered for "test/mock-model"
		// but the request parsing should succeed
		if w.Code == http.StatusBadRequest {
			t.Error("Request body should have been parsed successfully")
		}
	})
}

func TestNodeWithMetrics(t *testing.T) {
	node := NodeWithMetrics{
		WorkflowNode: storage.WorkflowNode{
			ExecutionID:  "job-1",
			NodeID:       "step_0",
			TokensInput:  100,
			TokensOutput: 50,
			LatencyMs:    500,
		},
		TotalTokens:            150,
		CumulativeTokens:       150,
		EfficiencyTokensPerSec: 300,
		WidthPercent:           50,
	}

	if node.TotalTokens != 150 {
		t.Errorf("Expected TotalTokens 150, got %d", node.TotalTokens)
	}
	if node.EfficiencyTokensPerSec != 300 {
		t.Errorf("Expected EfficiencyTokensPerSec 300, got %f", node.EfficiencyTokensPerSec)
	}
}

func TestPerformanceMetrics(t *testing.T) {
	metrics := PerformanceMetrics{
		TotalLatencyMs:         1000,
		AvgNodeLatencyMs:       500,
		ThroughputTokensPerSec: 150,
		CostPerToken:           0.0001,
	}

	if metrics.TotalLatencyMs != 1000 {
		t.Errorf("Expected TotalLatencyMs 1000, got %f", metrics.TotalLatencyMs)
	}
	if metrics.ThroughputTokensPerSec != 150 {
		t.Errorf("Expected ThroughputTokensPerSec 150, got %f", metrics.ThroughputTokensPerSec)
	}
}

func TestNewServer(t *testing.T) {
	store, db, cleanup := setupTestDB(t)
	defer cleanup()

	registry := providers.NewRegistry()
	server := NewServer(store, db, registry, newTestJobManager(store, registry), "")

	if server == nil {
		t.Fatal("Expected server to be non-nil")
	}
	if server.storage != store {
		t.Error("Expected storage to match")
	}
	if server.db != db {
		t.Error("Expected db to match")
	}
	if server.registry != registry {
		t.Error("Expected registry to match")
	}
	if server.jobManager == nil {
		t.Error("Expected jobManager to be non-nil")
	}
}

func TestRegisterRoutes(t *testing.T) {
	_, router, cleanup := setupTestServer(t)
	defer cleanup()

	routes := []struct {
		method string
		path   string
	}{
		{"GET", "/api/admin/overview"},
		{"GET", "/api/admin/jobs"},
		{"GET", "/api/admin/jobs/{id}"},
		{"GET", "/api/admin/jobs/{id}/nodes"},
		{"POST", "/api/admin/test/workflow"},
		{"GET", "/api/admin/workflows"},
		{"GET", "/api/admin/benchmarks"},
		{"GET", "/api/admin/db-diagnostics"},
	}

	for _, route := range routes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			match := &mux.RouteMatch{}
			req := httptest.NewRequest(route.method, route.path, nil)
			if !router.Match(req, match) {
				t.Errorf("Route %s %s should be registered", route.method, route.path)
			}
		})
	}
}

func TestDBDiagnosticsEndpoint(t *testing.T) {
	_, router, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/admin/db-diagnostics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected valid JSON response: %v", err)
	}

	database, ok := resp["Database"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected Database object in response: %#v", resp)
	}
	if _, ok := database["Pool"].(map[string]interface{}); !ok {
		t.Fatalf("expected Database.Pool object in response: %#v", database)
	}
	if _, ok := database["Queue"].(map[string]interface{}); !ok {
		t.Fatalf("expected Database.Queue object in response: %#v", database)
	}
	if _, ok := database["Tables"]; ok {
		t.Fatalf("expected Database.Tables to be omitted by default: %#v", database["Tables"])
	}

	workers, ok := resp["Workers"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected Workers object in response: %#v", resp)
	}
	if got := int(workers["WorkerMax"].(float64)); got != 4 {
		t.Fatalf("expected WorkerMax 4 from test manager, got %d", got)
	}
}

func TestDBDiagnosticsEndpointIncludesTablesWhenRequested(t *testing.T) {
	_, router, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/admin/db-diagnostics?tables=true", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected valid JSON response: %v", err)
	}
	database := resp["Database"].(map[string]interface{})
	tables, ok := database["Tables"].([]interface{})
	if !ok || len(tables) == 0 {
		t.Fatalf("expected table diagnostics when requested: %#v", database["Tables"])
	}
}

func TestJobDetailWithNodeExecutions(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	// Create a workflow job with multiple nodes
	job := &storage.Job{
		ID:           "workflow-job-1",
		Description:  "Multi-node workflow",
		Model:        "test-model",
		Status:       "completed",
		TokensInput:  200,
		TokensOutput: 100,
		TokensTotal:  300,
		Cost:         0.05,
	}
	if err := server.storage.CreateExecution(job); err != nil {
		t.Fatalf("Failed to create test job: %v", err)
	}

	// Add multiple workflow nodes
	nodes := []*storage.WorkflowNode{
		{
			ExecutionID:  "workflow-job-1",
			NodeID:       "step_0",
			NodeType:     "prompt",
			NodeOrder:    0,
			Status:       "completed",
			TokensInput:  100,
			TokensOutput: 50,
			Cost:         0.02,
			LatencyMs:    200,
		},
		{
			ExecutionID:  "workflow-job-1",
			NodeID:       "step_1",
			NodeType:     "prompt",
			NodeOrder:    1,
			Status:       "completed",
			TokensInput:  100,
			TokensOutput: 50,
			Cost:         0.03,
			LatencyMs:    300,
		},
	}

	for _, node := range nodes {
		if err := server.storage.AddWorkflowNode(node); err != nil {
			t.Fatalf("Failed to add workflow node: %v", err)
		}
	}

	req := httptest.NewRequest("GET", "/api/admin/jobs/workflow-job-1", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestOverviewStats(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	// Create jobs with different statuses
	jobs := []*storage.Job{
		{ID: "job-1", Status: "completed", TokensTotal: 100, Cost: 0.01},
		{ID: "job-2", Status: "completed", TokensTotal: 200, Cost: 0.02},
		{ID: "job-3", Status: "failed", TokensTotal: 50, Cost: 0.005},
	}

	for _, job := range jobs {
		job.Description = "test"
		job.Model = "test-model"
		if err := server.storage.CreateExecution(job); err != nil {
			t.Fatalf("Failed to create test job: %v", err)
		}
	}

	req := httptest.NewRequest("GET", "/api/admin/overview", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Expected valid JSON response: %v", err)
	}
	stats, ok := resp["Stats"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected Stats in response")
	}
	totalJobs, _ := stats["TotalJobs"].(float64)
	if totalJobs != 3 {
		t.Errorf("Expected TotalJobs 3, got %v", totalJobs)
	}
}

// MockProvider for testing workflow execution
type MockProvider struct {
	name   string
	models []providers.Model
}

func NewMockProvider(name string) *MockProvider {
	return &MockProvider{
		name: name,
		models: []providers.Model{
			{
				ID:         "mock-model",
				Name:       "Mock Model",
				Provider:   name,
				ContextLen: 4096,
				InputCost:  0.0,
				OutputCost: 0.0,
				MaxTokens:  2048,
				Available:  true,
			},
		},
	}
}

func (m *MockProvider) Name() string {
	return m.name
}

func (m *MockProvider) Models() []providers.Model {
	return m.models
}

func (m *MockProvider) Complete(ctx context.Context, req *providers.CompletionRequest) (*providers.CompletionResponse, error) {
	return &providers.CompletionResponse{
		ID:      "mock-response",
		Content: "Mock response",
		Model:   req.Model,
		Usage: providers.Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}, nil
}

func (m *MockProvider) Cost(model string, inputTokens, outputTokens int) float64 {
	return 0.0
}

func (m *MockProvider) EstimateTokens(text string) int {
	return len(text) / 4
}

func TestWorkflowExecutionWithMockProvider(t *testing.T) {
	store, db, cleanup := setupTestDB(t)
	defer cleanup()

	registry := providers.NewRegistry()
	mockProvider := NewMockProvider("test")
	registry.Register(mockProvider)

	mgr := newTestJobManager(store, registry)
	mgr.StartWorkers()
	defer mgr.StopWorkers(context.Background())

	server := NewServer(store, db, registry, mgr, "")
	router := mux.NewRouter()
	server.RegisterRoutes(router)

	wf := workflow.Workflow{
		ID:   "test-workflow",
		Name: "Test Workflow",
		Nodes: []*workflow.Node{
			{
				ID:             "step_0",
				Type:           workflow.NodeTypePrompt,
				Prompt:         "Hello",
				Model:          "mock-model",
				Temperature:    providers.Float64Ptr(0.0),
				MaxTokens:      128,
				TimeoutSeconds: 30,
				RetryPolicy:    &workflow.RetryPolicy{MaxAttempts: 1},
			},
		},
	}

	reqBody := struct {
		Workflow workflow.Workflow `json:"workflow"`
	}{
		Workflow: wf,
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/api/admin/test/workflow", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	// Verify response is valid JSON
	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Errorf("Response should be valid JSON: %v", err)
	}
}

// --- Regression tests for admin_jobs.go node reordering and handleJobDetail ---

// TestHandleJobNodes_ReorderParentChild verifies that child nodes appear
// immediately after their parent, regardless of insertion order.
func TestHandleJobNodes_ReorderParentChild(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	// Create a job
	job := &storage.Job{
		ID:          "reorder-test",
		Description: "Test reordering",
		Model:       "test-model",
		Status:      "completed",
	}
	if err := server.storage.CreateExecution(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	// Insert nodes out of order: child before parent
	// child-1 has parent_node_id = "parent-1"
	nodes := []storage.WorkflowNode{
		{
			ExecutionID:  "reorder-test",
			NodeID:       "child-1",
			NodeType:     "prompt",
			NodeOrder:    0,
			Status:       "completed",
			ParentNodeID: "parent-1",
			Model:        "model-a",
			Output:       "child output",
		},
		{
			ExecutionID: "reorder-test",
			NodeID:      "standalone",
			NodeType:    "prompt",
			NodeOrder:   1,
			Status:      "completed",
			Model:       "model-b",
			Output:      "standalone output",
		},
		{
			ExecutionID: "reorder-test",
			NodeID:      "parent-1",
			NodeType:    "prompt",
			NodeOrder:   2,
			Status:      "completed",
			Model:       "model-c",
			Output:      "parent output",
		},
	}
	for i := range nodes {
		if err := server.storage.AddWorkflowNode(&nodes[i]); err != nil {
			t.Fatalf("Failed to add node %s: %v", nodes[i].NodeID, err)
		}
	}

	req := httptest.NewRequest("GET", "/api/admin/jobs/reorder-test/nodes", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Nodes []struct {
			NodeID       string `json:"node_id"`
			ParentNodeID string `json:"parent_node_id"`
		} `json:"Nodes"`
		Total int `json:"Total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp.Total != 3 {
		t.Fatalf("Expected 3 nodes, got %d", resp.Total)
	}

	// Find parent-1's index
	parentIdx := -1
	childIdx := -1
	for i, n := range resp.Nodes {
		switch n.NodeID {
		case "parent-1":
			parentIdx = i
		case "child-1":
			childIdx = i
		}
	}

	if parentIdx == -1 || childIdx == -1 {
		t.Fatal("parent-1 or child-1 not found in response")
	}

	// Child must appear immediately after parent
	if childIdx != parentIdx+1 {
		nodeIDs := make([]string, len(resp.Nodes))
		for i, n := range resp.Nodes {
			nodeIDs[i] = n.NodeID
		}
		t.Errorf("Expected child-1 immediately after parent-1, got order: %v", nodeIDs)
	}
}

// TestHandleJobDetail_ResponseStructure verifies the response shape of the
// job detail endpoint. This is a regression test for decomposing handleJobDetail.
func TestHandleJobDetail_ResponseStructure(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	// Create a completed job with nodes
	job := &storage.Job{
		ID:           "detail-test",
		Description:  "Detail structure test",
		Model:        "test-model",
		Status:       "completed",
		TokensInput:  100,
		TokensOutput: 50,
		TokensTotal:  150,
		Cost:         0.01,
		ResultText:   "some result",
	}
	if err := server.storage.CreateExecution(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	// Add a node
	node := &storage.WorkflowNode{
		ExecutionID:  "detail-test",
		NodeID:       "step-1",
		NodeType:     "prompt",
		NodeOrder:    0,
		Status:       "completed",
		Model:        "test-model",
		Output:       "node output",
		TokensInput:  50,
		TokensOutput: 25,
		Cost:         0.005,
		LatencyMs:    200.0,
	}
	if err := server.storage.AddWorkflowNode(node); err != nil {
		t.Fatalf("Failed to add node: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/admin/jobs/detail-test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Verify all expected top-level keys exist
	expectedKeys := []string{
		"Job", "Lifecycle", "JobCostSummary",
		"NodeExecutions", "NodesWithMetrics", "PerfMetrics",
		"Logs", "Attempts", "OrphanAttempts",
		"InputPrompt", "ChildJobs", "ChildJobMetrics", "ChildCostSummaries",
	}
	for _, key := range expectedKeys {
		if _, ok := resp[key]; !ok {
			t.Errorf("Missing expected key %q in response", key)
		}
	}

	// Verify Job fields
	var jobData map[string]interface{}
	if err := json.Unmarshal(resp["Job"], &jobData); err != nil {
		t.Fatalf("Failed to parse Job: %v", err)
	}
	if jobData["id"] != "detail-test" {
		t.Errorf("Job.id = %v, want detail-test", jobData["id"])
	}
	if jobData["status"] != "completed" {
		t.Errorf("Job.status = %v, want completed", jobData["status"])
	}

	// Verify NodesWithMetrics is non-empty array
	var nodesMetrics []map[string]interface{}
	if err := json.Unmarshal(resp["NodesWithMetrics"], &nodesMetrics); err != nil {
		t.Fatalf("Failed to parse NodesWithMetrics: %v", err)
	}
	if len(nodesMetrics) == 0 {
		t.Error("Expected at least 1 node in NodesWithMetrics")
	}

	// Verify Lifecycle has expected fields
	var lifecycle map[string]interface{}
	if err := json.Unmarshal(resp["Lifecycle"], &lifecycle); err != nil {
		t.Fatalf("Failed to parse Lifecycle: %v", err)
	}
	if _, ok := lifecycle["EndToEndDurationMs"]; !ok {
		t.Error("Lifecycle missing EndToEndDurationMs")
	}
}

// --- Regression tests for admin_benchmarks.go ---

func TestFailureChartData(t *testing.T) {
	t.Run("FromStoredAllAttemptCounts", func(t *testing.T) {
		run := &storage.BenchmarkRun{
			AllAttemptFailureCounts: map[string]int{"timeout": 3, "rate_limit": 1},
			FailureReasonCounts:     map[string]int{"timeout": 2},
		}
		keys, vals := buildFailureChartData(run, nil, failureSourceAllAttempts)
		// Should use AllAttemptFailureCounts first
		if len(keys) != 2 {
			t.Fatalf("expected 2 keys, got %d", len(keys))
		}
		// Keys should be sorted
		if keys[0] != "rate_limit" || keys[1] != "timeout" {
			t.Errorf("keys = %v, want [rate_limit timeout]", keys)
		}
		if vals[0] != 1 || vals[1] != 3 {
			t.Errorf("vals = %v, want [1 3]", vals)
		}
	})

	t.Run("FallbackToItemCounts", func(t *testing.T) {
		run := &storage.BenchmarkRun{
			FailureReasonCounts: map[string]int{"parse_error": 5},
		}
		keys, vals := buildFailureChartData(run, nil, failureSourceAllAttempts)
		if len(keys) != 1 || keys[0] != "parse_error" {
			t.Errorf("expected [parse_error], got %v", keys)
		}
		if vals[0] != 5 {
			t.Errorf("vals[0] = %d, want 5", vals[0])
		}
	})

	t.Run("RecomputeFromItems", func(t *testing.T) {
		run := &storage.BenchmarkRun{}
		items := []storage.BenchmarkRunItem{
			{FailureReason: "timeout"},
			{FailureReason: "timeout"},
			{FailureReason: "rate_limit"},
		}
		keys, vals := buildFailureChartData(run, items, failureSourceAllAttempts)
		if len(keys) != 2 {
			t.Fatalf("expected 2 keys, got %d", len(keys))
		}
		if keys[0] != "rate_limit" || keys[1] != "timeout" {
			t.Errorf("keys = %v", keys)
		}
		if vals[1] != 2 {
			t.Errorf("timeout count = %d, want 2", vals[1])
		}
	})

	t.Run("WrongAnswerCounted", func(t *testing.T) {
		run := &storage.BenchmarkRun{FailureReasonCounts: map[string]int{"timeout": 1}}
		items := []storage.BenchmarkRunItem{
			{Correct: false, ParseOK: true, FailureReason: ""},
			{Correct: false, ParseOK: true, FailureReason: ""},
			{Correct: true, ParseOK: true, FailureReason: ""},
		}
		keys, vals := buildFailureChartData(run, items, failureSourceAllAttempts)
		// Should have both "timeout" and "wrong_answer"
		found := false
		for i, k := range keys {
			if k == "wrong_answer" {
				if vals[i] != 2 {
					t.Errorf("wrong_answer count = %d, want 2", vals[i])
				}
				found = true
			}
		}
		if !found {
			t.Error("expected wrong_answer in keys")
		}
	})
}

func TestFinalFailureChartData(t *testing.T) {
	run := &storage.BenchmarkRun{
		AllAttemptFailureCounts: map[string]int{"timeout": 5},
		FailureReasonCounts:     map[string]int{"timeout": 2},
	}
	keys, vals := buildFailureChartData(run, nil, failureSourceFinalOnly)
	// Should use FailureReasonCounts, NOT AllAttemptFailureCounts
	if len(keys) != 1 || keys[0] != "timeout" {
		t.Errorf("keys = %v, want [timeout]", keys)
	}
	if vals[0] != 2 {
		t.Errorf("vals[0] = %d, want 2", vals[0])
	}
}

func TestAllAttemptsFailureChartData(t *testing.T) {
	run := &storage.BenchmarkRun{
		AllAttemptFailureCounts: map[string]int{"timeout": 5, "rate_limit": 3},
	}
	keys, vals := buildFailureChartData(run, nil, failureSourceAllAttempts)
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
	if keys[0] != "rate_limit" || keys[1] != "timeout" {
		t.Errorf("keys = %v", keys)
	}
	if vals[0] != 3 || vals[1] != 5 {
		t.Errorf("vals = %v, want [3 5]", vals)
	}
}

func TestUniqueJobIDCollectors(t *testing.T) {
	t.Run("UniqueAttemptJobIDs", func(t *testing.T) {
		attempts := []storage.BenchmarkRunItemAttempt{
			{JobID: "job-1"},
			{JobID: "job-2"},
			{JobID: "job-1"}, // duplicate
			{JobID: ""},      // empty
			{JobID: "job-3"},
		}
		ids := uniqueAttemptJobIDs(attempts)
		if len(ids) != 3 {
			t.Fatalf("expected 3 unique IDs, got %d: %v", len(ids), ids)
		}
		// Verify order preserved (first occurrence)
		if ids[0] != "job-1" || ids[1] != "job-2" || ids[2] != "job-3" {
			t.Errorf("ids = %v, want [job-1 job-2 job-3]", ids)
		}
	})

	t.Run("CollectBenchmarkItemJobIDs", func(t *testing.T) {
		items := []storage.BenchmarkRunItem{
			{JobID: "a"},
			{JobID: "b"},
			{JobID: "a"}, // duplicate
			{JobID: "c"},
		}
		ids := collectBenchmarkItemJobIDs(items)
		if len(ids) != 3 {
			t.Fatalf("expected 3 unique IDs, got %d: %v", len(ids), ids)
		}
		if ids[0] != "a" || ids[1] != "b" || ids[2] != "c" {
			t.Errorf("ids = %v, want [a b c]", ids)
		}
	})

	t.Run("CollectBenchmarkRunAttemptJobIDs", func(t *testing.T) {
		results := []bench.ItemResult{
			{
				AttemptDetails: []bench.AttemptDetail{
					{JobID: "j1"},
					{JobID: "j2"},
				},
			},
			{
				AttemptDetails: []bench.AttemptDetail{
					{JobID: "j1"}, // duplicate across items
					{JobID: "j3"},
				},
			},
		}
		ids := collectBenchmarkRunAttemptJobIDs(results)
		if len(ids) != 3 {
			t.Fatalf("expected 3 unique IDs, got %d: %v", len(ids), ids)
		}
	})
}

func TestConvertBenchRunResult_RoundTrip(t *testing.T) {
	input := bench.RunResult{
		Summary: bench.RunSummary{
			RunID:      "run-1",
			Benchmark:  "mmlu",
			Split:      "test",
			WorkflowID: "wf-1",
			TotalItems: 10,
			Accuracy:   0.8,
		},
		Items: []bench.ItemResult{
			{
				ItemID:      "item-1",
				Subject:     "math",
				Language:    "en",
				AnswerLabel: "A",
				Predicted:   "A",
				ParseOK:     true,
				Correct:     true,
				JobID:       "job-1",
				LatencyMS:   150.0,
				AttemptDetails: []bench.AttemptDetail{
					{
						Attempt:              1,
						JobID:                "job-1",
						LatencyMS:            150.0,
						TokensInput:          100,
						TokensOutput:         50,
						TotalTokens:          150,
						CostUSD:              0.01,
						RawOutput:            "A",
						Predicted:            "A",
						ParseOK:              true,
						ContractNodeID:       "agent",
						ContractModel:        "gpt-4",
						ContractFinishReason: "stop",
					},
				},
			},
		},
	}

	result := convertBenchRunResult(input)

	// Verify summary fields
	if result.Summary.RunID != "run-1" {
		t.Errorf("Summary.RunID = %q", result.Summary.RunID)
	}
	if result.Summary.Accuracy != 0.8 {
		t.Errorf("Summary.Accuracy = %f", result.Summary.Accuracy)
	}

	// Verify item fields
	if len(result.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result.Items))
	}
	item := result.Items[0]
	if item.ItemID != "item-1" {
		t.Errorf("item.ItemID = %q", item.ItemID)
	}
	if item.Subject != "math" {
		t.Errorf("item.Subject = %q", item.Subject)
	}
	if !item.Correct {
		t.Error("item.Correct should be true")
	}

	// Verify attempt fields
	if len(item.AttemptDetails) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(item.AttemptDetails))
	}
	attempt := item.AttemptDetails[0]
	if attempt.Attempt != 1 {
		t.Errorf("attempt.Attempt = %d", attempt.Attempt)
	}
	if attempt.ContractNodeID != "agent" {
		t.Errorf("attempt.ContractNodeID = %q", attempt.ContractNodeID)
	}
	if attempt.ContractModel != "gpt-4" {
		t.Errorf("attempt.ContractModel = %q", attempt.ContractModel)
	}
}

func TestConvertSingleBenchmarkItem(t *testing.T) {
	item := bench.ItemResult{
		ItemID:      "item-42",
		Subject:     "physics",
		AnswerLabel: "B",
		Predicted:   "C",
		Correct:     false,
		ParseOK:     true,
		WorkflowID:  "wf-bench",
		AttemptDetails: []bench.AttemptDetail{
			{Attempt: 1, JobID: "j1", FailureReason: "timeout"},
			{Attempt: 2, JobID: "j2", Predicted: "C", ParseOK: true},
		},
	}

	result := convertSingleBenchmarkItem("run-x", item)
	if result.ItemID != "item-42" {
		t.Errorf("ItemID = %q", result.ItemID)
	}
	if result.Subject != "physics" {
		t.Errorf("Subject = %q", result.Subject)
	}
	if result.WorkflowID != "wf-bench" {
		t.Errorf("WorkflowID = %q", result.WorkflowID)
	}
	if len(result.AttemptDetails) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(result.AttemptDetails))
	}
	if result.AttemptDetails[0].FailureReason != "timeout" {
		t.Errorf("attempt[0].FailureReason = %q", result.AttemptDetails[0].FailureReason)
	}
}

func TestConvertDBItemsToBenchResults(t *testing.T) {
	items := []storage.BenchmarkRunItem{
		{
			ItemID:      "item-1",
			Subject:     "bio",
			AnswerLabel: "D",
			Predicted:   "D",
			Correct:     true,
			ParseOK:     true,
			JobID:       "job-100",
		},
	}
	attempts := []storage.BenchmarkRunItemAttempt{
		{
			ItemID:       "item-1",
			Attempt:      1,
			JobID:        "job-100",
			TokensInput:  200,
			TokensOutput: 80,
			TotalTokens:  280,
			Predicted:    "D",
			ParseOK:      true,
		},
	}

	results := convertDBItemsToBenchResults(items, attempts)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.ItemID != "item-1" {
		t.Errorf("ItemID = %q", r.ItemID)
	}
	if r.Subject != "bio" {
		t.Errorf("Subject = %q", r.Subject)
	}
	if !r.Correct {
		t.Error("Correct should be true")
	}
	if len(r.AttemptDetails) != 1 {
		t.Fatalf("expected 1 attempt detail, got %d", len(r.AttemptDetails))
	}
	if r.AttemptDetails[0].TokensInput != 200 {
		t.Errorf("attempt.TokensInput = %d", r.AttemptDetails[0].TokensInput)
	}
}

func TestIsBetterPromotionCandidate(t *testing.T) {
	baseline := &optimize.Organism{
		ID: "baseline",
		Fitness: &optimize.Fitness{
			CompositeScore:   2.0,
			AdjustedAccuracy: 0.75,
			CostPerItem:      0.10,
			Feasible:         true,
		},
		CreatedAt: time.Now().UTC(),
	}
	higherScore := &optimize.Organism{
		ID: "higher-score",
		Fitness: &optimize.Fitness{
			CompositeScore:   2.2,
			AdjustedAccuracy: 0.70,
			CostPerItem:      0.20,
			Feasible:         true,
		},
		CreatedAt: baseline.CreatedAt.Add(1 * time.Second),
	}
	if !isBetterPromotionCandidate(higherScore, baseline) {
		t.Fatalf("expected higher composite score to win")
	}
}

func TestSelectBestPromotableOrganismFallsBackToFeasibleCandidate(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	run := &optimize.OptimizationRun{
		ID:              "opt-run-promote-fallback",
		WorkflowID:      "wf-promote",
		WorkflowVersion: 1,
		Benchmark:       "bench",
		Split:           "dev",
		Concurrency:     1,
		Spec: &optimize.OptimizeSpec{
			Params: []optimize.ParamDeclaration{{
				Path:       "nodes[a].data.config.model",
				Type:       optimize.ParamTypeModel,
				Candidates: []string{"m1", "m2"},
			}},
			Objectives:      []optimize.Objective{{Metric: "accuracy", Direction: "maximize", Weight: 1}},
			StopPolicy:      optimize.StopPolicy{MaxGenerations: 1, BudgetUSD: 1, PlateauGenerations: 1, StabilityTopK: 1},
			PromotionPolicy: optimize.PromotionPolicy{},
		},
		Strategy:       "evolutionary",
		PopulationSize: 2,
		TotalBudgetUSD: 1,
		Status:         "completed",
		BestOrganismID: "org-infeasible",
	}
	if err := server.storage.CreateOptimizationRun(run); err != nil {
		t.Fatalf("CreateOptimizationRun failed: %v", err)
	}

	infeasible := &optimize.Organism{
		ID:           "org-infeasible",
		OptRunID:     run.ID,
		Generation:   1,
		ParentIDs:    []string{},
		ParamValues:  map[string]string{"nodes[a].data.config.model": `"m1"`},
		WorkflowJSON: []byte(`{"id":"wf-promote"}`),
		MutationType: "combinatorial",
		MutationLog:  "infeasible",
		CreatedAt:    time.Now().UTC(),
	}
	feasible := &optimize.Organism{
		ID:           "org-feasible",
		OptRunID:     run.ID,
		Generation:   1,
		ParentIDs:    []string{},
		ParamValues:  map[string]string{"nodes[a].data.config.model": `"m2"`},
		WorkflowJSON: []byte(`{"id":"wf-promote"}`),
		MutationType: "combinatorial",
		MutationLog:  "feasible",
		CreatedAt:    time.Now().UTC().Add(1 * time.Second),
	}
	if err := server.storage.CreateOptimizationOrganism(infeasible); err != nil {
		t.Fatalf("CreateOptimizationOrganism infeasible failed: %v", err)
	}
	if err := server.storage.CreateOptimizationOrganism(feasible); err != nil {
		t.Fatalf("CreateOptimizationOrganism feasible failed: %v", err)
	}
	if err := server.storage.UpdateOptimizationOrganismFitness(infeasible.ID, "bench-infeasible", &optimize.Fitness{
		AdjustedAccuracy: 0.95,
		ParseRate:        0.9,
		CostPerItem:      0.05,
		CompositeScore:   100,
		Feasible:         false,
	}); err != nil {
		t.Fatalf("UpdateOptimizationOrganismFitness infeasible failed: %v", err)
	}
	if err := server.storage.UpdateOptimizationOrganismFitness(feasible.ID, "bench-feasible", &optimize.Fitness{
		AdjustedAccuracy: 0.80,
		ParseRate:        0.9,
		CostPerItem:      0.05,
		CompositeScore:   10,
		Feasible:         true,
	}); err != nil {
		t.Fatalf("UpdateOptimizationOrganismFitness feasible failed: %v", err)
	}

	selected, err := server.selectBestPromotableOrganism(run)
	if err != nil {
		t.Fatalf("selectBestPromotableOrganism failed: %v", err)
	}
	if selected == nil {
		t.Fatal("expected a promotable organism")
	}
	if selected.ID != feasible.ID {
		t.Fatalf("expected feasible fallback organism %s, got %s", feasible.ID, selected.ID)
	}
}
