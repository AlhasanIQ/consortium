package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// TestIntegration_SubmitStreamComplete tests the full submit -> stream -> complete flow
func TestIntegration_SubmitStreamComplete(t *testing.T) {
	// Setup
	db, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	registry := providers.NewRegistry()
	mockProvider := &MockExecuteProvider{
		name: "mock",
		models: []providers.Model{
			{ID: "mock-model", Provider: "mock", InputCost: 0.001, OutputCost: 0.002},
		},
		response: "Test response from mock model",
	}
	registry.Register(mockProvider)

	api := NewWorkflowAPI(db, registry, newTestJobManager(db, registry))

	t.Run("submit returns job_id and workflow_id", func(t *testing.T) {
		router := mux.NewRouter()
		api.RegisterRoutes(router)

		submitReq := map[string]interface{}{
			"workflow": map[string]interface{}{
				"name": "Test Workflow",
				"nodes": []interface{}{
					strictPromptNodeMap("node1", "mock-model", "Hello world"),
				},
			},
			"idempotency_key": "test-key-" + uuid.New().String(),
		}

		body, _ := json.Marshal(submitReq)
		req := httptest.NewRequest("POST", "/api/workflows/submit", bytes.NewReader(body))
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
		if _, ok := response["status"].(string); !ok {
			t.Error("Expected status field in response")
		}
	})

	t.Run("idempotency prevents duplicate submissions", func(t *testing.T) {
		router := mux.NewRouter()
		api.RegisterRoutes(router)

		idempotencyKey := "idempotent-key-" + uuid.New().String()

		submitReq := map[string]interface{}{
			"workflow": map[string]interface{}{
				"name": "Idempotent Workflow",
				"nodes": []interface{}{
					strictPromptNodeMap("node1", "mock-model", "Test prompt"),
				},
			},
			"idempotency_key": idempotencyKey,
		}

		body, _ := json.Marshal(submitReq)

		// First submission
		req1 := httptest.NewRequest("POST", "/api/workflows/submit", bytes.NewReader(body))
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)

		if w1.Code != http.StatusCreated {
			t.Fatalf("First submission failed: %d - %s", w1.Code, w1.Body.String())
		}

		var response1 map[string]interface{}
		json.NewDecoder(w1.Body).Decode(&response1)
		jobID1 := response1["job_id"].(string)

		// Second submission with same idempotency key
		req2 := httptest.NewRequest("POST", "/api/workflows/submit", bytes.NewReader(body))
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)

		// For duplicate submissions, the API returns 200 OK (idempotent)
		if w2.Code != http.StatusOK && w2.Code != http.StatusCreated {
			t.Fatalf("Second submission failed: %d - %s", w2.Code, w2.Body.String())
		}

		var response2 map[string]interface{}
		json.NewDecoder(w2.Body).Decode(&response2)
		jobID2 := response2["job_id"].(string)

		// Should return same job ID
		if jobID1 != jobID2 {
			t.Errorf("Expected same job ID for duplicate submission, got %s and %s", jobID1, jobID2)
		}

		// Should have duplicate flag
		if duplicate, ok := response2["duplicate"].(bool); !ok || !duplicate {
			t.Error("Expected duplicate flag to be true on second submission")
		}
	})
}

// TestIntegration_SubmitCancel tests the submit -> cancel flow
func TestIntegration_SubmitCancel(t *testing.T) {
	db, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	registry := providers.NewRegistry()

	// Create a slow provider to give time for cancellation
	slowProvider := &SlowMockProvider{
		name: "slow",
		models: []providers.Model{
			{ID: "slow-model", Provider: "slow", InputCost: 0.001, OutputCost: 0.002},
		},
		delay: 2 * time.Second,
	}
	registry.Register(slowProvider)

	api := NewWorkflowAPI(db, registry, newTestJobManager(db, registry))
	router := mux.NewRouter()
	api.RegisterRoutes(router)

	t.Run("cancel pending job returns success", func(t *testing.T) {
		// Submit a workflow (creates a pending job)
		// Note: Submit only creates a pending job - execution starts when connecting to stream
		submitReq := map[string]interface{}{
			"workflow": map[string]interface{}{
				"name": "Pending Workflow",
				"nodes": []interface{}{
					strictPromptNodeMap("node1", "slow-model", "This won't run yet"),
				},
			},
			"idempotency_key": "cancel-pending-test-" + uuid.New().String(),
		}

		body, _ := json.Marshal(submitReq)
		submitReqHTTP := httptest.NewRequest("POST", "/api/workflows/submit", bytes.NewReader(body))
		submitW := httptest.NewRecorder()
		router.ServeHTTP(submitW, submitReqHTTP)

		if submitW.Code != http.StatusCreated {
			t.Fatalf("Submit failed: %d - %s", submitW.Code, submitW.Body.String())
		}

		var submitResp map[string]interface{}
		json.NewDecoder(submitW.Body).Decode(&submitResp)
		jobID := submitResp["job_id"].(string)

		// Cancel the pending job - should succeed immediately.
		cancelReq := httptest.NewRequest("POST", "/api/jobs/"+jobID+"/cancel", nil)
		cancelW := httptest.NewRecorder()
		router.ServeHTTP(cancelW, cancelReq)

		if cancelW.Code != http.StatusOK {
			t.Errorf("Expected cancel status %d for pending job, got %d. Body: %s", http.StatusOK, cancelW.Code, cancelW.Body.String())
		}

		var cancelResp map[string]interface{}
		if err := json.NewDecoder(cancelW.Body).Decode(&cancelResp); err != nil {
			t.Fatalf("Failed to decode cancel response: %v", err)
		}
		if success, ok := cancelResp["success"].(bool); !ok || !success {
			t.Errorf("Expected success=true, got response: %+v", cancelResp)
		}
	})

	t.Run("cancel non-existent job returns 404", func(t *testing.T) {
		cancelReq := httptest.NewRequest("POST", "/api/jobs/nonexistent-job-id/cancel", nil)
		cancelW := httptest.NewRecorder()
		router.ServeHTTP(cancelW, cancelReq)

		if cancelW.Code != http.StatusNotFound {
			t.Errorf("Expected status %d, got %d", http.StatusNotFound, cancelW.Code)
		}

		var errorResp map[string]interface{}
		json.NewDecoder(cancelW.Body).Decode(&errorResp)

		if code, ok := errorResp["code"].(string); !ok || code != ErrCodeJobNotFound {
			t.Errorf("Expected error code %s, got %v", ErrCodeJobNotFound, errorResp["code"])
		}
	})

	t.Run("cancel completed job returns 409", func(t *testing.T) {
		// Create a completed job directly in database
		job := &storage.Job{
			ID:     "completed-job-" + uuid.New().String(),
			Status: "completed",
		}
		db.CreateExecution(job)

		cancelReq := httptest.NewRequest("POST", "/api/jobs/"+job.ID+"/cancel", nil)
		cancelW := httptest.NewRecorder()
		router.ServeHTTP(cancelW, cancelReq)

		if cancelW.Code != http.StatusConflict {
			t.Errorf("Expected status %d, got %d", http.StatusConflict, cancelW.Code)
		}

		var errorResp map[string]interface{}
		json.NewDecoder(cancelW.Body).Decode(&errorResp)

		if code, ok := errorResp["code"].(string); !ok || code != ErrCodeJobNotRunning {
			t.Errorf("Expected error code %s, got %v", ErrCodeJobNotRunning, errorResp["code"])
		}
	})
}

// TestIntegration_EventOrdering tests that events are emitted in correct order
func TestIntegration_EventOrdering(t *testing.T) {
	db, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	registry := providers.NewRegistry()
	mockProvider := &MockExecuteProvider{
		name: "mock",
		models: []providers.Model{
			{ID: "mock-model", Provider: "mock"},
		},
		response: "Test response",
	}
	registry.Register(mockProvider)

	manager := newTestJobManager(db, registry)
	manager.StartWorkers()
	t.Cleanup(func() { manager.StopWorkers(context.Background()) })

	t.Run("events follow canonical order", func(t *testing.T) {
		wf := &workflow.Workflow{
			ID:   "event-order-test",
			Name: "Event Ordering Test",
			Nodes: []*workflow.Node{
				strictPromptNode("node1", "mock-model", "Test prompt"),
			},
		}

		result, err := manager.ExecuteWorkflow(context.Background(), wf)
		if err != nil {
			t.Fatalf("Execution failed: %v", err)
		}

		// Verify persisted events
		persistedEvents, err := db.GetEventsAfter(context.Background(), result.JobID, 0)
		if err != nil {
			t.Fatalf("GetEventsAfter failed: %v", err)
		}

		if len(persistedEvents) == 0 {
			t.Fatal("Expected to receive events")
		}

		// Verify event order: status should come first
		firstEventType := persistedEvents[0].Type
		if firstEventType != events.EventStatus && firstEventType != events.EventJobCreated {
			t.Errorf("Expected first event to be status or job_created, got %s", firstEventType)
		}

		// Verify last event is complete or error
		lastEventType := persistedEvents[len(persistedEvents)-1].Type
		if lastEventType != events.EventComplete && lastEventType != events.EventError {
			t.Errorf("Expected last event to be complete or error, got %s", lastEventType)
		}

		// Verify node_start comes before node_complete for same node
		nodeStartIndex := -1
		nodeCompleteIndex := -1
		for i, event := range persistedEvents {
			if event.Type == events.EventNodeStart && event.NodeID == "node1" {
				nodeStartIndex = i
			}
			if event.Type == events.EventNodeComplete && event.NodeID == "node1" {
				nodeCompleteIndex = i
			}
		}

		if nodeStartIndex >= 0 && nodeCompleteIndex >= 0 {
			if nodeStartIndex > nodeCompleteIndex {
				t.Error("node_start should come before node_complete for same node")
			}
		}
	})

	t.Run("timestamps are monotonically increasing", func(t *testing.T) {
		wf := &workflow.Workflow{
			ID:   "timestamp-test",
			Name: "Timestamp Test",
			Nodes: []*workflow.Node{
				strictPromptNode("node1", "mock-model", "First node"),
				strictPromptNode("node2", "mock-model", "Second node"),
			},
		}

		result, err := manager.ExecuteWorkflow(context.Background(), wf)
		if err != nil {
			t.Fatalf("Execution failed: %v", err)
		}

		persistedEvents, err := db.GetEventsAfter(context.Background(), result.JobID, 0)
		if err != nil {
			t.Fatalf("GetEventsAfter failed: %v", err)
		}

		// Verify timestamps are non-decreasing
		for i := 1; i < len(persistedEvents); i++ {
			prev := persistedEvents[i-1].Timestamp
			curr := persistedEvents[i].Timestamp

			if curr.Before(prev) {
				t.Errorf("Timestamp at index %d (%v) is before timestamp at index %d (%v)",
					i, curr, i-1, prev)
			}
		}
	})

	t.Run("no duplicate events for same node", func(t *testing.T) {
		wf := &workflow.Workflow{
			ID:   "no-duplicate-test",
			Name: "No Duplicate Test",
			Nodes: []*workflow.Node{
				strictPromptNode("node1", "mock-model", "Test"),
			},
		}

		result, err := manager.ExecuteWorkflow(context.Background(), wf)
		if err != nil {
			t.Fatalf("Execution failed: %v", err)
		}

		persistedEvents, err := db.GetEventsAfter(context.Background(), result.JobID, 0)
		if err != nil {
			t.Fatalf("GetEventsAfter failed: %v", err)
		}

		// Count node_complete events for node1
		nodeCompleteCount := 0
		for _, event := range persistedEvents {
			if event.Type == events.EventNodeComplete && event.NodeID == "node1" {
				nodeCompleteCount++
			}
		}

		if nodeCompleteCount > 1 {
			t.Errorf("Expected at most 1 node_complete for node1, got %d", nodeCompleteCount)
		}
	})
}

// TestIntegration_ValidationAtEntrypoints tests that invalid workflows are rejected
func TestIntegration_ValidationAtEntrypoints(t *testing.T) {
	db, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	registry := providers.NewRegistry()
	mockProvider := &MockExecuteProvider{
		name: "mock",
		models: []providers.Model{
			{ID: "mock-model", Provider: "mock"},
		},
		response: "Test",
	}
	registry.Register(mockProvider)

	api := NewWorkflowAPI(db, registry, newTestJobManager(db, registry))
	router := mux.NewRouter()
	api.RegisterRoutes(router)

	t.Run("submit rejects workflow with invalid model", func(t *testing.T) {
		submitReq := map[string]interface{}{
			"workflow": map[string]interface{}{
				"name": "Invalid Model Workflow",
				"nodes": []interface{}{
					strictPromptNodeMap("node1", "nonexistent-model", "Test"),
				},
			},
			"idempotency_key": "invalid-model-" + uuid.New().String(),
		}

		body, _ := json.Marshal(submitReq)
		req := httptest.NewRequest("POST", "/api/workflows/submit", bytes.NewReader(body))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d. Body: %s", http.StatusBadRequest, w.Code, w.Body.String())
		}

		var errorResp map[string]interface{}
		json.NewDecoder(w.Body).Decode(&errorResp)

		if code, ok := errorResp["code"].(string); !ok || code != ErrCodeInvalidWorkflow {
			t.Errorf("Expected error code %s, got %v", ErrCodeInvalidWorkflow, errorResp["code"])
		}
	})

	t.Run("execute rejects workflow with circular dependencies", func(t *testing.T) {
		wf := map[string]interface{}{
			"name": "Circular Workflow",
			"nodes": []interface{}{
				strictPromptNodeMap("node1", "mock-model", "Test"),
				strictPromptNodeMap("node2", "mock-model", "Test"),
			},
			"edges": []interface{}{
				map[string]interface{}{"source": "node1", "target": "node2"},
				map[string]interface{}{"source": "node2", "target": "node1"}, // Creates cycle
			},
		}

		body, _ := json.Marshal(wf)
		req := httptest.NewRequest("POST", "/api/workflows/execute", bytes.NewReader(body))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d. Body: %s", http.StatusBadRequest, w.Code, w.Body.String())
		}

		var errorResp map[string]interface{}
		json.NewDecoder(w.Body).Decode(&errorResp)

		// Should have error code INVALID_WORKFLOW or CYCLE_DETECTED
		code, _ := errorResp["code"].(string)
		if code != ErrCodeInvalidWorkflow && code != ErrCodeCycleDetected {
			t.Errorf("Expected error code %s or %s, got %s", ErrCodeInvalidWorkflow, ErrCodeCycleDetected, code)
		}
	})

	t.Run("submit rejects empty workflow", func(t *testing.T) {
		submitReq := map[string]interface{}{
			"workflow": map[string]interface{}{
				"name":  "Empty Workflow",
				"nodes": []interface{}{},
			},
			"idempotency_key": "empty-" + uuid.New().String(),
		}

		body, _ := json.Marshal(submitReq)
		req := httptest.NewRequest("POST", "/api/workflows/submit", bytes.NewReader(body))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d. Body: %s", http.StatusBadRequest, w.Code, w.Body.String())
		}
	})
}

// TestIntegration_ErrorCodes tests that error codes are returned correctly
func TestIntegration_ErrorCodes(t *testing.T) {
	db, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	registry := providers.NewRegistry()
	api := NewWorkflowAPI(db, registry, newTestJobManager(db, registry))
	router := mux.NewRouter()
	api.RegisterRoutes(router)

	tests := []struct {
		name         string
		method       string
		path         string
		body         interface{}
		expectedCode string
		expectedHTTP int
	}{
		{
			name:         "invalid JSON returns INVALID_JSON",
			method:       "POST",
			path:         "/api/workflows/submit",
			body:         "{invalid json",
			expectedCode: ErrCodeInvalidJSON,
			expectedHTTP: http.StatusBadRequest,
		},
		{
			name:         "job not found returns JOB_NOT_FOUND",
			method:       "POST",
			path:         "/api/jobs/nonexistent/cancel",
			body:         nil,
			expectedCode: ErrCodeJobNotFound,
			expectedHTTP: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bodyReader *bytes.Reader
			if tt.body != nil {
				if str, ok := tt.body.(string); ok {
					bodyReader = bytes.NewReader([]byte(str))
				} else {
					body, _ := json.Marshal(tt.body)
					bodyReader = bytes.NewReader(body)
				}
			}

			var req *http.Request
			if bodyReader != nil {
				req = httptest.NewRequest(tt.method, tt.path, bodyReader)
			} else {
				req = httptest.NewRequest(tt.method, tt.path, nil)
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tt.expectedHTTP {
				t.Errorf("Expected HTTP %d, got %d. Body: %s", tt.expectedHTTP, w.Code, w.Body.String())
			}

			var errorResp map[string]interface{}
			if err := json.NewDecoder(w.Body).Decode(&errorResp); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if code, ok := errorResp["code"].(string); !ok || code != tt.expectedCode {
				t.Errorf("Expected error code %s, got %v", tt.expectedCode, errorResp["code"])
			}
		})
	}
}

// TestIntegration_ExecuteWSGone verifies /api/workflows/execute/ws returns 410 Gone.
// Clients should use POST /api/workflows/submit + WS /api/jobs/{id}/stream instead.
func TestIntegration_ExecuteWSGone(t *testing.T) {
	db, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	registry := providers.NewRegistry()
	api := NewWorkflowAPI(db, registry, newTestJobManager(db, registry))
	router := mux.NewRouter()
	api.RegisterRoutes(router)

	req := httptest.NewRequest("GET", "/api/workflows/execute/ws", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusGone {
		t.Errorf("Expected 410 Gone, got %d", w.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if body["error"] == "" {
		t.Error("Expected error message in response body")
	}
}

// TestIntegration_JobListEndpoint tests the GET /api/jobs endpoint
func TestIntegration_JobListEndpoint(t *testing.T) {
	db, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	registry := providers.NewRegistry()
	api := NewWorkflowAPI(db, registry, newTestJobManager(db, registry))
	router := mux.NewRouter()
	api.RegisterRoutes(router)

	// Create some test jobs
	for i := 0; i < 5; i++ {
		job := &storage.Job{
			ID:          uuid.New().String(),
			Description: "Test job " + string(rune('A'+i)),
			Model:       "mock-model",
			Status:      "completed",
			WorkflowID:  "test-workflow-1",
			TokensTotal: 100 * (i + 1),
			Cost:        0.001 * float64(i+1),
		}
		if i == 3 {
			job.Status = "failed"
		}
		if i == 4 {
			job.WorkflowID = "test-workflow-2"
		}
		db.CreateExecution(job)
		// Small delay to ensure different timestamps for ordering
		time.Sleep(10 * time.Millisecond)
	}

	t.Run("list jobs returns paginated results", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/jobs?limit=3", nil)
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

		if len(jobs) != 3 {
			t.Errorf("Expected 3 jobs, got %d", len(jobs))
		}

		pagination, ok := response["pagination"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected pagination object in response")
		}

		if hasMore, ok := pagination["has_more"].(bool); !ok || !hasMore {
			t.Error("Expected has_more to be true")
		}

		if cursor, ok := pagination["cursor"].(string); !ok || cursor == "" {
			t.Error("Expected non-empty cursor")
		}
	})

	t.Run("cursor pagination works", func(t *testing.T) {
		// First request
		req1 := httptest.NewRequest("GET", "/api/jobs?limit=2", nil)
		w1 := httptest.NewRecorder()
		router.ServeHTTP(w1, req1)

		var response1 map[string]interface{}
		json.NewDecoder(w1.Body).Decode(&response1)
		cursor := response1["pagination"].(map[string]interface{})["cursor"].(string)
		jobs1 := response1["jobs"].([]interface{})

		// Second request with cursor
		req2 := httptest.NewRequest("GET", "/api/jobs?limit=2&cursor="+cursor, nil)
		w2 := httptest.NewRecorder()
		router.ServeHTTP(w2, req2)

		var response2 map[string]interface{}
		json.NewDecoder(w2.Body).Decode(&response2)
		jobs2 := response2["jobs"].([]interface{})

		// Verify no overlap between pages
		ids1 := make(map[string]bool)
		for _, j := range jobs1 {
			job := j.(map[string]interface{})
			ids1[job["id"].(string)] = true
		}

		for _, j := range jobs2 {
			job := j.(map[string]interface{})
			if ids1[job["id"].(string)] {
				t.Error("Found overlapping job IDs between pages")
			}
		}
	})

	t.Run("status filter works", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/jobs?status=failed", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		var response map[string]interface{}
		json.NewDecoder(w.Body).Decode(&response)
		jobs := response["jobs"].([]interface{})

		if len(jobs) != 1 {
			t.Errorf("Expected 1 failed job, got %d", len(jobs))
		}

		if len(jobs) > 0 {
			job := jobs[0].(map[string]interface{})
			if job["status"] != "failed" {
				t.Errorf("Expected status 'failed', got '%s'", job["status"])
			}
		}
	})

	t.Run("workflow_id filter works", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/jobs?workflow_id=test-workflow-2", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		var response map[string]interface{}
		json.NewDecoder(w.Body).Decode(&response)
		jobs := response["jobs"].([]interface{})

		if len(jobs) != 1 {
			t.Errorf("Expected 1 job for workflow-2, got %d", len(jobs))
		}

		if len(jobs) > 0 {
			job := jobs[0].(map[string]interface{})
			if job["workflow_id"] != "test-workflow-2" {
				t.Errorf("Expected workflow_id 'test-workflow-2', got '%s'", job["workflow_id"])
			}
		}
	})

	t.Run("list jobs includes prompt and result_text", func(t *testing.T) {
		wf := workflow.Workflow{
			ID:    "prompt-workflow",
			Name:  "Prompt Workflow",
			Nodes: []*workflow.Node{},
			Context: map[string]interface{}{
				"user_prompt": "Why is the sky blue?",
			},
		}
		wfJSON, err := json.Marshal(wf)
		if err != nil {
			t.Fatalf("Failed to marshal workflow: %v", err)
		}

		job := &storage.Job{
			ID:          uuid.New().String(),
			Description: "Prompt job",
			Model:       "mock-model",
			Status:      "completed",
			WorkflowID:  "test-workflow-prompt",
			RequestData: string(wfJSON),
			ResultText:  "Rayleigh scattering",
		}
		if err := db.CreateExecution(job); err != nil {
			t.Fatalf("Failed to create job: %v", err)
		}
		if err := db.UpdateExecution(job); err != nil {
			t.Fatalf("Failed to update job: %v", err)
		}

		req := httptest.NewRequest("GET", "/api/jobs?limit=1", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		var response map[string]interface{}
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		jobs, ok := response["jobs"].([]interface{})
		if !ok || len(jobs) == 0 {
			t.Fatalf("Expected jobs array with at least one entry")
		}

		jobResp, ok := jobs[0].(map[string]interface{})
		if !ok {
			t.Fatalf("Expected job to be an object")
		}

		if prompt, _ := jobResp["prompt"].(string); prompt != "Why is the sky blue?" {
			t.Errorf("Expected prompt to be 'Why is the sky blue?', got '%v'", jobResp["prompt"])
		}
		if resultText, _ := jobResp["result_text"].(string); resultText != "Rayleigh scattering" {
			t.Errorf("Expected result_text to be 'Rayleigh scattering', got '%v'", jobResp["result_text"])
		}
	})

	t.Run("job detail includes node error_message", func(t *testing.T) {
		jobID := uuid.New().String()
		job := &storage.Job{
			ID:          jobID,
			Description: "Job with failed node",
			Model:       "mock-model",
			Status:      "failed",
			WorkflowID:  "test-workflow-error",
		}
		if err := db.CreateExecution(job); err != nil {
			t.Fatalf("Failed to create job: %v", err)
		}

		node := &storage.WorkflowNode{
			ExecutionID:  jobID,
			NodeID:       "node-1",
			NodeType:     "prompt",
			NodeOrder:    1,
			Status:       "failed",
			ErrorMessage: "boom",
		}
		if err := db.AddWorkflowNode(node); err != nil {
			t.Fatalf("Failed to add workflow node: %v", err)
		}

		req := httptest.NewRequest("GET", "/api/jobs/"+jobID, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		var response map[string]interface{}
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		nodes, ok := response["nodes"].([]interface{})
		if !ok || len(nodes) == 0 {
			t.Fatalf("Expected nodes array with at least one entry")
		}

		nodeResp, ok := nodes[0].(map[string]interface{})
		if !ok {
			t.Fatalf("Expected node to be an object")
		}

		if msg, _ := nodeResp["error_message"].(string); msg != "boom" {
			t.Errorf("Expected error_message to be 'boom', got '%v'", nodeResp["error_message"])
		}
	})

	t.Run("job detail includes node metadata", func(t *testing.T) {
		// Create a job with nodes
		jobID := uuid.New().String()
		job := &storage.Job{
			ID:          jobID,
			Description: "Job with nodes",
			Model:       "mock-model",
			Status:      "completed",
			WorkflowID:  "node-test-workflow",
		}
		db.CreateExecution(job)

		// Add a node with label and name
		node := &storage.WorkflowNode{
			ExecutionID:  jobID,
			NodeID:       "test-node-1",
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
		db.AddWorkflowNode(node)

		req := httptest.NewRequest("GET", "/api/jobs/"+jobID, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
			return
		}

		var response map[string]interface{}
		json.NewDecoder(w.Body).Decode(&response)

		nodes, ok := response["nodes"].([]interface{})
		if !ok || len(nodes) == 0 {
			t.Fatal("Expected nodes array with at least one node")
		}

		nodeData := nodes[0].(map[string]interface{})

		// Verify node metadata is present
		if nodeData["node_label"] != "Claude" {
			t.Errorf("Expected node_label 'Claude', got '%v'", nodeData["node_label"])
		}
		if nodeData["node_name"] != "Claude Response" {
			t.Errorf("Expected node_name 'Claude Response', got '%v'", nodeData["node_name"])
		}
		if nodeData["node_type"] != "prompt" {
			t.Errorf("Expected node_type 'prompt', got '%v'", nodeData["node_type"])
		}
	})
}

// ---------------------------------------------------------------------------
// P0 Gate Tests — Durable Worker Cutover
// These tests define target behavior for Phases 1-4 of the durable worker
// cutover. They are expected to FAIL until the corresponding phase is
// implemented, following a red-green approach.
// ---------------------------------------------------------------------------

// Test_SubmitExecutesWithoutStream verifies that a submitted job reaches
// terminal state via background workers, without any WebSocket connection.
// Passes after: Phase 1 (background worker).
func Test_SubmitExecutesWithoutStream(t *testing.T) {
	db, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	registry := providers.NewRegistry()
	registry.Register(&MockExecuteProvider{
		name:     "mock",
		models:   []providers.Model{{ID: "mock-model", Provider: "mock"}},
		response: "Background execution response",
	})

	manager := newTestJobManager(db, registry)
	manager.StartWorkers()
	t.Cleanup(func() { manager.StopWorkers(context.Background()) })

	api := NewWorkflowAPI(db, registry, manager)
	router := mux.NewRouter()
	api.RegisterRoutes(router)

	// Submit via HTTP API
	submitBody, _ := json.Marshal(map[string]interface{}{
		"workflow": map[string]interface{}{
			"name": "BG Exec Test",
			"nodes": []interface{}{
				strictPromptNodeMap("node1", "mock-model", "Say hello"),
			},
		},
		"force_new_run": true,
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", "/api/workflows/submit", bytes.NewReader(submitBody)))
	if w.Code != http.StatusCreated {
		t.Fatalf("Submit failed: %d - %s", w.Code, w.Body.String())
	}

	var submitResp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&submitResp)
	jobID := submitResp["job_id"].(string)

	// Do NOT open WebSocket. Poll GET /api/jobs/{id} until terminal.
	var finalStatus string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		gw := httptest.NewRecorder()
		router.ServeHTTP(gw, httptest.NewRequest("GET", "/api/jobs/"+jobID, nil))
		var jr map[string]interface{}
		json.NewDecoder(gw.Body).Decode(&jr)
		finalStatus, _ = jr["status"].(string)
		if finalStatus == "completed" || finalStatus == "failed" || finalStatus == "cancelled" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if finalStatus == "pending" || finalStatus == "running" {
		t.Fatalf("submitted job did not reach terminal state — got '%s'", finalStatus)
	}
	if finalStatus != "completed" {
		t.Fatalf("expected 'completed', got '%s'", finalStatus)
	}
}

// Test_StreamDoesNotTriggerExecution verifies that workers execute jobs
// independently of WS connections, and that events are persisted to job_events
// for stream replay.
// Passes after: Phase 2 (event persistence) + Phase 3 (subscribe-only stream).
func Test_StreamDoesNotTriggerExecution(t *testing.T) {
	db, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	registry := providers.NewRegistry()
	registry.Register(&MockExecuteProvider{
		name:     "mock",
		models:   []providers.Model{{ID: "mock-model", Provider: "mock"}},
		response: "Test response",
	})

	manager := newTestJobManager(db, registry)
	manager.StartWorkers()
	t.Cleanup(func() { manager.StopWorkers(context.Background()) })

	wf := &workflow.Workflow{
		ID:   "wf-stream-test",
		Name: "Stream Test",
		Nodes: []*workflow.Node{
			strictPromptNode("node1", "mock-model", "Hello"),
		},
	}

	// Execute via workers (no WS/callback involved)
	result, err := manager.ExecuteWorkflow(context.Background(), wf)
	if err != nil {
		t.Fatalf("ExecuteWorkflow failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("execution failed: %s", result.Error)
	}

	// Verify events were persisted to job_events for stream replay.
	evts, err := db.GetEventsAfter(context.Background(), result.JobID, 0)
	if err != nil {
		t.Fatalf("GetEventsAfter failed: %v", err)
	}
	if len(evts) == 0 {
		t.Fatal("no events in job_events — stream cannot replay without persisted events")
	}

	// Verify key lifecycle events exist
	typeSet := make(map[string]bool)
	for _, e := range evts {
		typeSet[e.Type] = true
	}
	if !typeSet[events.EventNodeComplete] {
		t.Error("expected node_complete event in job_events")
	}
	if !typeSet[events.EventComplete] {
		t.Error("expected complete event in job_events")
	}
}

// Test_NoDuplicateExecutionOnReconnect verifies that after a job completes,
// events in job_events have monotonically increasing sequences and activity
// results are cached for idempotency.
// Passes after: Phase 2 (event persistence).
func Test_NoDuplicateExecutionOnReconnect(t *testing.T) {
	db, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	registry := providers.NewRegistry()
	registry.Register(&MockExecuteProvider{
		name:     "mock",
		models:   []providers.Model{{ID: "mock-model", Provider: "mock"}},
		response: "Dedup response",
	})

	manager := newTestJobManager(db, registry)
	manager.StartWorkers()
	t.Cleanup(func() { manager.StopWorkers(context.Background()) })

	wf := &workflow.Workflow{
		ID:   "wf-no-dup",
		Name: "No Dup Test",
		Nodes: []*workflow.Node{
			strictPromptNode("node1", "mock-model", "Hello"),
		},
	}

	ctx := context.Background()

	// Execute via workers
	result, err := manager.ExecuteWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("execution not successful: %s", result.Error)
	}

	// Check persisted events have monotonic sequences
	persistedEvents, err := db.GetEventsAfter(ctx, result.JobID, 0)
	if err != nil {
		t.Fatalf("GetEventsAfter failed: %v", err)
	}
	if len(persistedEvents) == 0 {
		t.Fatal("no events persisted to job_events — required for reconnect replay")
	}
	for i := 1; i < len(persistedEvents); i++ {
		if persistedEvents[i].Sequence <= persistedEvents[i-1].Sequence {
			t.Errorf("event sequences not monotonic: [%d]=%d, [%d]=%d",
				i-1, persistedEvents[i-1].Sequence, i, persistedEvents[i].Sequence)
		}
	}

	// Verify activity_results for this run have exactly one entry per node
	actKey := result.JobID + ":node1:1"
	actResult, err := db.GetActivityResult(ctx, actKey)
	if err != nil {
		t.Fatalf("GetActivityResult failed: %v", err)
	}
	if actResult == nil {
		t.Error("expected activity result to be cached for node1")
	}
}
