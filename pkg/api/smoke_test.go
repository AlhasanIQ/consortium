package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// Smoke tests validate core user flows work correctly.
// Run with: go test -run TestSmoke_ ./pkg/api/...
//
// These tests focus on the critical paths a new developer would use:
// 1. Health check
// 2. Model listing
// 3. Submit workflow
// 4. Get job details
// 5. Idempotency
// 6. Cancel job
// 7. Invalid workflow rejection

// setupSmokeTest creates a test environment with mock provider
func setupSmokeTest(t *testing.T) (*WorkflowAPI, *mux.Router, *storage.Storage) {
	t.Helper()

	db, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	registry := providers.NewRegistry()
	mockProvider := &MockExecuteProvider{
		name: "mock",
		models: []providers.Model{
			{ID: "mock-model", Provider: "mock", Name: "Mock Model", InputCost: 0.001, OutputCost: 0.002},
		},
		response: "Test response from mock model",
	}
	registry.Register(mockProvider)

	api := NewWorkflowAPI(db, registry, newTestJobManager(db, registry))
	router := mux.NewRouter()
	api.RegisterRoutes(router)

	// Add health endpoint (mirrors cmd/server/main.go)
	router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}).Methods("GET")

	return api, router, db
}

func smokePromptNode(id, model, prompt string) map[string]interface{} {
	return map[string]interface{}{
		"id":              id,
		"type":            "prompt",
		"model":           model,
		"prompt":          prompt,
		"temperature":     0.0,
		"max_tokens":      256,
		"timeout_seconds": 30,
		"retry_policy": map[string]interface{}{
			"max_attempts":     1,
			"backoff_ms":       0,
			"backoff_multiply": 1.0,
			"max_backoff_ms":   0,
		},
	}
}

// TestSmoke_HealthEndpoint verifies the health check endpoint returns OK
func TestSmoke_HealthEndpoint(t *testing.T) {
	_, router, _ := setupSmokeTest(t)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	if w.Body.String() != "OK" {
		t.Errorf("Expected body 'OK', got '%s'", w.Body.String())
	}
}

// TestSmoke_ModelsEndpoint verifies the models endpoint returns available models
func TestSmoke_ModelsEndpoint(t *testing.T) {
	_, router, _ := setupSmokeTest(t)

	req := httptest.NewRequest("GET", "/api/models", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		return
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	models, ok := response["models"].([]interface{})
	if !ok {
		t.Fatal("Expected models array in response")
	}

	if len(models) == 0 {
		t.Error("Expected at least one model")
	}

	// Verify model has required fields
	model := models[0].(map[string]interface{})
	if _, ok := model["id"].(string); !ok {
		t.Error("Expected model to have 'id' field")
	}
	if _, ok := model["provider"].(string); !ok {
		t.Error("Expected model to have 'provider' field")
	}
}

// TestSmoke_SubmitWorkflow verifies workflow submission returns job_id
func TestSmoke_SubmitWorkflow(t *testing.T) {
	_, router, _ := setupSmokeTest(t)

	submitReq := map[string]interface{}{
		"workflow": map[string]interface{}{
			"name": "Smoke Test Workflow",
			"nodes": []interface{}{
				smokePromptNode("node1", "mock-model", "Hello world"),
			},
		},
		"idempotency_key": "smoke-test-" + uuid.New().String(),
	}

	body, _ := json.Marshal(submitReq)
	req := httptest.NewRequest("POST", "/api/workflows/submit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusCreated, w.Code, w.Body.String())
		return
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify required fields
	if _, ok := response["job_id"].(string); !ok {
		t.Error("Expected job_id field in response")
	}
	if _, ok := response["workflow_id"].(string); !ok {
		t.Error("Expected workflow_id field in response")
	}
	if status, ok := response["status"].(string); !ok || status != "pending" {
		t.Errorf("Expected status 'pending', got '%v'", response["status"])
	}
}

// TestSmoke_JobDetails verifies job details endpoint returns job with nodes
func TestSmoke_JobDetails(t *testing.T) {
	_, router, db := setupSmokeTest(t)

	// Create a completed job with nodes
	jobID := uuid.New().String()
	job := &storage.Job{
		ID:           jobID,
		Description:  "Smoke test job",
		Model:        "mock-model",
		Status:       "completed",
		WorkflowID:   "smoke-workflow",
		TokensInput:  50,
		TokensOutput: 100,
		TokensTotal:  150,
		Cost:         0.005,
		ResultText:   "Test result",
	}
	if err := db.CreateExecution(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	// Add a node
	node := &storage.WorkflowNode{
		ExecutionID:  jobID,
		NodeID:       "node-1",
		NodeType:     "prompt",
		NodeOrder:    0,
		Status:       "completed",
		NodeLabel:    "Claude",
		NodeName:     "Claude Response",
		Model:        "mock-model",
		Output:       "Test output",
		TokensInput:  50,
		TokensOutput: 100,
		Cost:         0.005,
		LatencyMs:    500.0,
	}
	if err := db.AddWorkflowNode(node); err != nil {
		t.Fatalf("Failed to add node: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/jobs/"+jobID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		return
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify job fields
	if response["id"] != jobID {
		t.Errorf("Expected job id '%s', got '%v'", jobID, response["id"])
	}
	if response["status"] != "completed" {
		t.Errorf("Expected status 'completed', got '%v'", response["status"])
	}

	// Verify nodes are included
	nodes, ok := response["nodes"].([]interface{})
	if !ok || len(nodes) == 0 {
		t.Error("Expected nodes array with at least one node")
		return
	}

	nodeData := nodes[0].(map[string]interface{})
	if nodeData["node_label"] != "Claude" {
		t.Errorf("Expected node_label 'Claude', got '%v'", nodeData["node_label"])
	}
}

// TestSmoke_IdempotentSubmit verifies duplicate submissions return same job
func TestSmoke_IdempotentSubmit(t *testing.T) {
	_, router, _ := setupSmokeTest(t)

	idempotencyKey := "idempotent-smoke-" + uuid.New().String()
	submitReq := map[string]interface{}{
		"workflow": map[string]interface{}{
			"name": "Idempotent Smoke Test",
			"nodes": []interface{}{
				smokePromptNode("node1", "mock-model", "Test"),
			},
		},
		"idempotency_key": idempotencyKey,
	}

	body, _ := json.Marshal(submitReq)

	// First submission
	req1 := httptest.NewRequest("POST", "/api/workflows/submit", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)

	if w1.Code != http.StatusCreated {
		t.Fatalf("First submission failed: %d - %s", w1.Code, w1.Body.String())
	}

	var response1 map[string]interface{}
	json.NewDecoder(w1.Body).Decode(&response1)
	jobID1 := response1["job_id"].(string)

	// Second submission with same idempotency key
	body2, _ := json.Marshal(submitReq)
	req2 := httptest.NewRequest("POST", "/api/workflows/submit", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	// Should return OK (not Created) for duplicate
	if w2.Code != http.StatusOK && w2.Code != http.StatusCreated {
		t.Fatalf("Second submission failed: %d - %s", w2.Code, w2.Body.String())
	}

	var response2 map[string]interface{}
	json.NewDecoder(w2.Body).Decode(&response2)
	jobID2 := response2["job_id"].(string)

	// Should return same job ID
	if jobID1 != jobID2 {
		t.Errorf("Expected same job ID, got '%s' and '%s'", jobID1, jobID2)
	}

	// Should have duplicate flag
	if duplicate, ok := response2["duplicate"].(bool); !ok || !duplicate {
		t.Error("Expected duplicate flag to be true")
	}
}

// TestSmoke_CancelJobNotActivelyRunning verifies cancel returns error for a
// "running" DB job that is not actively owned by this process.
// Pending jobs are cancellable directly; this test covers the running-path
// ownership guard.
func TestSmoke_CancelJobNotActivelyRunning(t *testing.T) {
	_, router, db := setupSmokeTest(t)

	// Create a job with running status in database
	// However, it's not tracked in the job manager's running map
	jobID := uuid.New().String()
	job := &storage.Job{
		ID:     jobID,
		Status: "running",
	}
	if err := db.CreateExecution(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	// Cancel the job - should return error since it's not actively running
	req := httptest.NewRequest("POST", "/api/jobs/"+jobID+"/cancel", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Expect 409 because job exists but is in a conflicting state (running in DB, not in process map)
	if w.Code != http.StatusConflict {
		t.Errorf("Expected status %d for job not in running map, got %d. Body: %s",
			http.StatusConflict, w.Code, w.Body.String())
		return
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Should have JOB_NOT_RUNNING error code (job exists in DB but not in process map)
	if code, ok := response["code"].(string); !ok || code != ErrCodeJobNotRunning {
		t.Errorf("Expected error code '%s', got '%v'", ErrCodeJobNotRunning, response["code"])
	}
}

// TestSmoke_CancelNonExistentJob verifies 404 for non-existent job
func TestSmoke_CancelNonExistentJob(t *testing.T) {
	_, router, _ := setupSmokeTest(t)

	req := httptest.NewRequest("POST", "/api/jobs/nonexistent-job-id/cancel", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
	}

	var response map[string]interface{}
	json.NewDecoder(w.Body).Decode(&response)

	if code, ok := response["code"].(string); !ok || code != ErrCodeJobNotFound {
		t.Errorf("Expected error code '%s', got '%v'", ErrCodeJobNotFound, response["code"])
	}
}

// TestSmoke_InvalidWorkflowRejected verifies invalid workflows are rejected
func TestSmoke_InvalidWorkflowRejected(t *testing.T) {
	_, router, _ := setupSmokeTest(t)

	t.Run("rejects workflow with invalid model", func(t *testing.T) {
		submitReq := map[string]interface{}{
			"workflow": map[string]interface{}{
				"name": "Invalid Model Workflow",
				"nodes": []interface{}{
					smokePromptNode("node1", "nonexistent-model", "Test"),
				},
			},
			"idempotency_key": "invalid-model-" + uuid.New().String(),
		}

		body, _ := json.Marshal(submitReq)
		req := httptest.NewRequest("POST", "/api/workflows/submit", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d. Body: %s", http.StatusBadRequest, w.Code, w.Body.String())
		}

		var response map[string]interface{}
		json.NewDecoder(w.Body).Decode(&response)

		if code, ok := response["code"].(string); !ok || code != ErrCodeInvalidWorkflow {
			t.Errorf("Expected error code '%s', got '%v'", ErrCodeInvalidWorkflow, response["code"])
		}
	})

	t.Run("rejects empty workflow", func(t *testing.T) {
		submitReq := map[string]interface{}{
			"workflow": map[string]interface{}{
				"name":  "Empty Workflow",
				"nodes": []interface{}{},
			},
			"idempotency_key": "empty-" + uuid.New().String(),
		}

		body, _ := json.Marshal(submitReq)
		req := httptest.NewRequest("POST", "/api/workflows/submit", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d. Body: %s", http.StatusBadRequest, w.Code, w.Body.String())
		}
	})

	t.Run("rejects invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/workflows/submit", bytes.NewReader([]byte("{invalid json")))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
		}

		var response map[string]interface{}
		json.NewDecoder(w.Body).Decode(&response)

		if code, ok := response["code"].(string); !ok || code != ErrCodeInvalidJSON {
			t.Errorf("Expected error code '%s', got '%v'", ErrCodeInvalidJSON, response["code"])
		}
	})
}

// TestSmoke_JobListEndpoint verifies the job list endpoint works
func TestSmoke_JobListEndpoint(t *testing.T) {
	_, router, db := setupSmokeTest(t)

	// Create a few test jobs
	for i := 0; i < 3; i++ {
		job := &storage.Job{
			ID:          uuid.New().String(),
			Description: "Test job",
			Status:      "completed",
		}
		db.CreateExecution(job)
		time.Sleep(5 * time.Millisecond) // Ensure different timestamps
	}

	req := httptest.NewRequest("GET", "/api/jobs?limit=2", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		return
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	jobs, ok := response["jobs"].([]interface{})
	if !ok {
		t.Fatal("Expected jobs array in response")
	}

	if len(jobs) != 2 {
		t.Errorf("Expected 2 jobs, got %d", len(jobs))
	}

	// Verify pagination info
	pagination, ok := response["pagination"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected pagination object")
	}

	if hasMore, ok := pagination["has_more"].(bool); !ok || !hasMore {
		t.Error("Expected has_more to be true")
	}
}

// TestSmoke_DedupSkipsFailedJobs verifies that failed jobs are not returned as dedup hits
func TestSmoke_DedupSkipsFailedJobs(t *testing.T) {
	_, router, db := setupSmokeTest(t)

	idempotencyKey := "dedup-failed-" + uuid.New().String()
	submitReq := map[string]interface{}{
		"workflow": map[string]interface{}{
			"name": "Dedup Failed Test",
			"nodes": []interface{}{
				smokePromptNode("node1", "mock-model", "Test"),
			},
		},
		"idempotency_key": idempotencyKey,
	}

	body, _ := json.Marshal(submitReq)

	// First submission
	req1 := httptest.NewRequest("POST", "/api/workflows/submit", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)

	if w1.Code != http.StatusCreated {
		t.Fatalf("First submission failed: %d - %s", w1.Code, w1.Body.String())
	}

	var response1 map[string]interface{}
	json.NewDecoder(w1.Body).Decode(&response1)
	jobID1 := response1["job_id"].(string)

	// Mark the job as failed
	failedJob, err := db.GetExecution(jobID1)
	if err != nil {
		t.Fatalf("Failed to get job: %v", err)
	}
	failedJob.Status = "failed"
	if err := db.UpdateExecution(failedJob); err != nil {
		t.Fatalf("Failed to update job status: %v", err)
	}

	// Resubmit with same idempotency key — should create NEW job (not return failed one)
	body2, _ := json.Marshal(submitReq)
	req2 := httptest.NewRequest("POST", "/api/workflows/submit", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	var response2 map[string]interface{}
	json.NewDecoder(w2.Body).Decode(&response2)
	jobID2 := response2["job_id"].(string)

	// Should be a new job, not the failed one
	if jobID1 == jobID2 {
		t.Errorf("Expected new job ID after failed job, got same ID: %s", jobID1)
	}

	// Should not be marked as duplicate
	if dup, ok := response2["duplicate"].(bool); ok && dup {
		t.Error("Expected duplicate=false for resubmit after failure")
	}
}

// TestSmoke_ForceNewRunBypass verifies force_new_run bypasses all dedup
func TestSmoke_ForceNewRunBypass(t *testing.T) {
	_, router, _ := setupSmokeTest(t)

	idempotencyKey := "force-new-" + uuid.New().String()
	submitReq := map[string]interface{}{
		"workflow": map[string]interface{}{
			"name": "Force New Test",
			"nodes": []interface{}{
				smokePromptNode("node1", "mock-model", "Test"),
			},
		},
		"idempotency_key": idempotencyKey,
	}

	body, _ := json.Marshal(submitReq)

	// First submission
	req1 := httptest.NewRequest("POST", "/api/workflows/submit", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)

	if w1.Code != http.StatusCreated {
		t.Fatalf("First submission failed: %d - %s", w1.Code, w1.Body.String())
	}

	var response1 map[string]interface{}
	json.NewDecoder(w1.Body).Decode(&response1)
	jobID1 := response1["job_id"].(string)

	// Second submission with force_new_run=true
	submitReq["force_new_run"] = true
	body2, _ := json.Marshal(submitReq)
	req2 := httptest.NewRequest("POST", "/api/workflows/submit", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusCreated {
		t.Fatalf("Force new submission failed: %d - %s", w2.Code, w2.Body.String())
	}

	var response2 map[string]interface{}
	json.NewDecoder(w2.Body).Decode(&response2)
	jobID2 := response2["job_id"].(string)

	// Should be a different job
	if jobID1 == jobID2 {
		t.Errorf("Expected different job ID with force_new_run, got same: %s", jobID1)
	}

	// Should not be duplicate
	if dup, ok := response2["duplicate"].(bool); ok && dup {
		t.Error("Expected duplicate=false with force_new_run")
	}

	// Should have dedup_bypassed=true
	if bypassed, ok := response2["dedup_bypassed"].(bool); !ok || !bypassed {
		t.Error("Expected dedup_bypassed=true")
	}
}

// TestSmoke_DedupResponseContract verifies full dedup metadata in response
func TestSmoke_DedupResponseContract(t *testing.T) {
	_, router, _ := setupSmokeTest(t)

	idempotencyKey := "contract-" + uuid.New().String()
	submitReq := map[string]interface{}{
		"workflow": map[string]interface{}{
			"name": "Contract Test",
			"nodes": []interface{}{
				smokePromptNode("node1", "mock-model", "Test"),
			},
		},
		"idempotency_key": idempotencyKey,
	}

	body, _ := json.Marshal(submitReq)

	// First submission — verify new job response fields
	req1 := httptest.NewRequest("POST", "/api/workflows/submit", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)

	var resp1 map[string]interface{}
	json.NewDecoder(w1.Body).Decode(&resp1)

	// New job: all dedup fields should be in response
	requiredFields := []string{"job_id", "workflow_id", "duplicate", "status", "dedup_bypassed"}
	for _, field := range requiredFields {
		if _, ok := resp1[field]; !ok {
			t.Errorf("Missing field %q in new job response", field)
		}
	}

	// Second submission — verify dedup response fields
	body2, _ := json.Marshal(submitReq)
	req2 := httptest.NewRequest("POST", "/api/workflows/submit", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	var resp2 map[string]interface{}
	json.NewDecoder(w2.Body).Decode(&resp2)

	// Verify dedup metadata
	if resp2["duplicate"] != true {
		t.Error("Expected duplicate=true")
	}
	if resp2["dedup_reason"] == nil || resp2["dedup_reason"] == "" {
		t.Error("Expected dedup_reason to be set")
	}
	if resp2["dedup_source_job_id"] == nil || resp2["dedup_source_job_id"] == "" {
		t.Error("Expected dedup_source_job_id to be set")
	}
	if resp2["dedup_source_status"] == nil || resp2["dedup_source_status"] == "" {
		t.Error("Expected dedup_source_status to be set")
	}
}
