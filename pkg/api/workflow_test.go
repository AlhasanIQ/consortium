package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/jobs"
	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/gorilla/mux"
)

func TestNewWorkflowAPI(t *testing.T) {
	api, _ := setupWorkflowAPI(t)

	if api.storage == nil {
		t.Error("Expected storage to be set")
	}
	if api.registry == nil {
		t.Error("Expected registry to be set")
	}
}

func TestRegisterRoutes(t *testing.T) {
	api, _ := setupWorkflowAPI(t)
	router := mux.NewRouter()
	api.RegisterRoutes(router)

	routes := []struct {
		method string
		path   string
	}{
		{"GET", "/api/workflows"},
		{"POST", "/api/workflows"},
		{"POST", "/api/workflows/execute"},
		{"GET", "/api/workflows/{id}"},
		{"PUT", "/api/workflows/{id}"},
		{"DELETE", "/api/workflows/{id}"},
	}

	for _, route := range routes {
		req := httptest.NewRequest(route.method, route.path, nil)
		match := &mux.RouteMatch{}
		if !router.Match(req, match) {
			t.Errorf("Route %s %s not registered", route.method, route.path)
		}
	}
}

func TestHandleListWorkflows(t *testing.T) {
	api, _ := setupWorkflowAPI(t)

	w := httptest.NewRecorder()
	api.handleListWorkflows(w, httptest.NewRequest("GET", "/api/workflows", nil))

	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", ct)
	}

	resp := decodeJSON(t, w)
	if _, ok := resp["workflows"].([]interface{}); !ok {
		t.Fatalf("Expected workflows field to be an array, got %T", resp["workflows"])
	}
}

func TestHandleCreateWorkflow(t *testing.T) {
	api, _ := setupWorkflowAPI(t)

	tests := []struct {
		name           string
		body           interface{}
		expectedStatus int
		checkResponse  func(t *testing.T, response map[string]interface{})
	}{
		{
			name: "valid workflow",
			body: workflow.Workflow{
				Name: "Test Workflow", Description: "Test Description",
				Nodes: []*workflow.Node{{Type: workflow.NodeTypePrompt, Prompt: "Test prompt"}},
			},
			expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, response map[string]interface{}) {
				if _, ok := response["id"].(string); !ok {
					t.Error("Expected id field in response")
				}
				if msg, ok := response["message"].(string); !ok || msg == "" {
					t.Error("Expected message field in response")
				}
			},
		},
		{
			name: "workflow with custom ID",
			body: map[string]interface{}{
				"id": "custom-id-123", "name": "Custom ID Workflow",
				"nodes": []interface{}{
					map[string]interface{}{"id": "node-1", "type": "prompt", "data": map[string]interface{}{"prompt": "Test prompt"}},
				},
				"edges": []interface{}{},
			},
			expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, response map[string]interface{}) {
				if id, ok := response["id"].(string); !ok || id != "custom-id-123" {
					t.Errorf("Expected id to be custom-id-123, got %v", response["id"])
				}
			},
		},
		{
			name:           "invalid JSON",
			body:           "invalid json",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			api.handleCreateWorkflow(w, jsonRequest(t, "POST", "/api/workflows", tt.body))

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
			if tt.checkResponse != nil && w.Code == http.StatusCreated {
				tt.checkResponse(t, decodeJSON(t, w))
			}
		})
	}
}

func TestHandleGetWorkflow(t *testing.T) {
	api, _ := setupWorkflowAPI(t)

	router := mux.NewRouter()
	router.HandleFunc("/api/workflows/{id}", api.handleGetWorkflow).Methods("GET")
	router.HandleFunc("/api/workflows", api.handleCreateWorkflow).Methods("POST")

	createBody := map[string]interface{}{
		"id": "test-id-123", "name": "Test Workflow",
		"nodes": []map[string]interface{}{{"id": "node1", "type": "prompt", "prompt": "test"}},
	}
	w := serveHTTP(t, router, "POST", "/api/workflows", createBody)
	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create workflow: %d %s", w.Code, w.Body.String())
	}

	w = serveHTTP(t, router, "GET", "/api/workflows/test-id-123", nil)
	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", ct)
	}

	resp := decodeJSON(t, w)
	if id, ok := resp["id"].(string); !ok || id != "test-id-123" {
		t.Errorf("Expected id test-id-123, got %v", resp["id"])
	}
	if _, ok := resp["name"]; !ok {
		t.Error("Expected name field in response")
	}
}

func TestHandleUpdateWorkflow(t *testing.T) {
	api, _ := setupWorkflowAPI(t)

	router := mux.NewRouter()
	router.HandleFunc("/api/workflows/{id}", api.handleUpdateWorkflow).Methods("PUT")

	tests := []struct {
		name           string
		workflowID     string
		body           interface{}
		expectedStatus int
		checkResponse  func(t *testing.T, response map[string]interface{})
	}{
		{
			name:       "valid update",
			workflowID: "test-id-123",
			body: workflow.Workflow{
				Name: "Updated Workflow", Description: "Updated Description",
				Nodes: []*workflow.Node{{Type: workflow.NodeTypePrompt, Prompt: "Updated prompt"}},
			},
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, response map[string]interface{}) {
				if msg, ok := response["message"].(string); !ok || msg == "" {
					t.Error("Expected message field in response")
				}
			},
		},
		{
			name:           "invalid JSON",
			workflowID:     "test-id",
			body:           "invalid json",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:       "missing workflow returns 404",
			workflowID: "workflow-does-not-exist",
			body: workflow.Workflow{
				Name: "Will Not Exist",
				Nodes: []*workflow.Node{
					{Type: workflow.NodeTypePrompt, Prompt: "No-op"},
				},
			},
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.expectedStatus == http.StatusOK {
				err := api.storage.CreateWorkflow(&storage.WorkflowDefinition{
					ID:         tt.workflowID,
					Name:       "Existing",
					Definition: `{"id":"` + tt.workflowID + `","name":"Existing","nodes":[],"edges":[]}`,
				})
				if err != nil {
					t.Fatalf("failed to seed workflow %s: %v", tt.workflowID, err)
				}
			}

			w := serveHTTP(t, router, "PUT", "/api/workflows/"+tt.workflowID, tt.body)
			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
			if tt.checkResponse != nil && w.Code == http.StatusOK {
				tt.checkResponse(t, decodeJSON(t, w))
			}
		})
	}
}

func TestHandleDeleteWorkflow(t *testing.T) {
	api, _ := setupWorkflowAPI(t)

	router := mux.NewRouter()
	router.HandleFunc("/api/workflows/{id}", api.handleDeleteWorkflow).Methods("DELETE")

	w := serveHTTP(t, router, "DELETE", "/api/workflows/delete-test-123", nil)
	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", ct)
	}

	resp := decodeJSON(t, w)
	if msg, ok := resp["message"].(string); !ok || msg == "" {
		t.Error("Expected message field in response")
	}
}

func TestHandleExecuteWorkflowInvalidJSON(t *testing.T) {
	api, _ := setupWorkflowAPI(t)

	router := mux.NewRouter()
	router.HandleFunc("/api/workflows/{id}/execute", api.handleExecuteWorkflow).Methods("POST")

	w := serveHTTP(t, router, "POST", "/api/workflows/test-id/execute", "invalid json")
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestNormalizeWorkflowFileIDs(t *testing.T) {
	tests := []struct {
		name  string
		file  map[string]interface{}
		check func(t *testing.T, file map[string]interface{})
	}{
		{
			name: "generates workflow ID if missing",
			file: map[string]interface{}{"name": "Test"},
			check: func(t *testing.T, file map[string]interface{}) {
				if id, ok := file["id"].(string); !ok || id == "" {
					t.Error("Expected workflow ID to be generated")
				}
			},
		},
		{
			name: "preserves existing workflow ID",
			file: map[string]interface{}{"id": "existing-123", "name": "Test"},
			check: func(t *testing.T, file map[string]interface{}) {
				if file["id"].(string) != "existing-123" {
					t.Error("Expected ID to be preserved")
				}
			},
		},
		{
			name: "generates node IDs if missing",
			file: map[string]interface{}{
				"id": "workflow-1", "name": "Test",
				"nodes": []interface{}{
					map[string]interface{}{"type": "start"},
					map[string]interface{}{"type": "llm"},
				},
			},
			check: func(t *testing.T, file map[string]interface{}) {
				for i, n := range file["nodes"].([]interface{}) {
					if id, ok := n.(map[string]interface{})["id"].(string); !ok || id == "" {
						t.Errorf("Expected node %d to have ID", i)
					}
				}
			},
		},
		{
			name: "preserves existing node IDs",
			file: map[string]interface{}{
				"id": "workflow-1", "name": "Test",
				"nodes": []interface{}{
					map[string]interface{}{"id": "node-1", "type": "start"},
					map[string]interface{}{"id": "node-2", "type": "llm"},
				},
			},
			check: func(t *testing.T, file map[string]interface{}) {
				nodes := file["nodes"].([]interface{})
				if nodes[0].(map[string]interface{})["id"].(string) != "node-1" {
					t.Error("Expected node 0 ID to be preserved")
				}
				if nodes[1].(map[string]interface{})["id"].(string) != "node-2" {
					t.Error("Expected node 1 ID to be preserved")
				}
			},
		},
		{
			name: "generates edge IDs if missing",
			file: map[string]interface{}{
				"id": "workflow-1", "name": "Test",
				"edges": []interface{}{
					map[string]interface{}{"source": "node-1", "target": "node-2"},
				},
			},
			check: func(t *testing.T, file map[string]interface{}) {
				edge := file["edges"].([]interface{})[0].(map[string]interface{})
				if id, ok := edge["id"].(string); !ok || id == "" {
					t.Error("Expected edge to have ID")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := workflow.NormalizeWorkflowFileIDs(tt.file); err != nil {
				t.Fatalf("normalizeWorkflowFileIDs failed: %v", err)
			}
			tt.check(t, tt.file)
		})
	}
}

func TestErrorHandling(t *testing.T) {
	tests := []struct {
		name           string
		handler        func(w http.ResponseWriter, r *http.Request)
		request        *http.Request
		expectedStatus int
		checkError     func(t *testing.T, response APIError)
	}{
		{
			name: "handleGetWorkflow - missing ID",
			handler: func(w http.ResponseWriter, r *http.Request) {
				api, _ := setupWorkflowAPI(t)
				api.handleGetWorkflow(w, r)
			},
			request:        httptest.NewRequest("GET", "/api/workflows/", nil),
			expectedStatus: http.StatusBadRequest,
			checkError: func(t *testing.T, response APIError) {
				if response.Error != "Missing workflow ID" {
					t.Errorf("Expected 'Missing workflow ID' error, got: %s", response.Error)
				}
			},
		},
		{
			name: "handleGetWorkflow - not found",
			handler: func(w http.ResponseWriter, r *http.Request) {
				api, _ := setupWorkflowAPI(t)
				router := mux.NewRouter()
				router.HandleFunc("/api/workflows/{id}", api.handleGetWorkflow).Methods("GET")
				router.ServeHTTP(w, r)
			},
			request:        httptest.NewRequest("GET", "/api/workflows/nonexistent-id", nil),
			expectedStatus: http.StatusNotFound,
			checkError: func(t *testing.T, response APIError) {
				if response.Error != "Workflow not found" {
					t.Errorf("Expected 'Workflow not found' error, got: %s", response.Error)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tt.handler(w, tt.request)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
			if tt.checkError != nil {
				var response APIError
				if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
					t.Fatalf("Failed to decode error response: %v. Body: %s", err, w.Body.String())
				}
				tt.checkError(t, response)
			}
		})
	}
}

func TestRespondWithJSON(t *testing.T) {
	api, _ := setupWorkflowAPI(t)

	tests := []struct {
		name           string
		statusCode     int
		payload        interface{}
		expectedStatus int
		checkResponse  func(t *testing.T, w *httptest.ResponseRecorder)
	}{
		{
			name: "successful response", statusCode: http.StatusOK,
			payload: map[string]string{"message": "success"}, expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var response map[string]string
				if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if response["message"] != "success" {
					t.Errorf("Expected message 'success', got: %s", response["message"])
				}
			},
		},
		{
			name: "created response", statusCode: http.StatusCreated,
			payload: map[string]interface{}{"id": "123", "created": true}, expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				if w.Header().Get("Content-Type") != "application/json" {
					t.Error("Expected Content-Type to be application/json")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			api.respondWithJSON(w, tt.statusCode, tt.payload)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}

func TestHandleExecuteWorkflowWithMockProvider(t *testing.T) {
	api, db := setupWorkflowAPI(t)
	mockProvider := registerMockProvider(t, api, "Mock LLM response")

	t.Run("successful workflow execution", func(t *testing.T) {
		wf := workflow.Workflow{
			Name:  "Test Execute",
			Nodes: []*workflow.Node{strictPromptNode("node-0", "mock-model", "Test prompt")},
		}
		w := httptest.NewRecorder()
		api.handleExecuteWorkflow(w, jsonRequest(t, "POST", "/api/workflows/execute", wf))

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		resp := decodeJSON(t, w)
		if success, ok := resp["success"].(bool); !ok || !success {
			t.Error("Expected success to be true")
		}
		if _, ok := resp["job_id"].(string); !ok {
			t.Error("Expected job_id field in response")
		}

		jobsList, _ := db.ListExecutions(100)
		if len(jobsList) == 0 {
			t.Error("Expected job to be created in database")
		}
	})

	t.Run("workflow execution with error", func(t *testing.T) {
		mockProvider.shouldError = true
		mockProvider.errorMsg = "Mock provider error"
		defer func() { mockProvider.shouldError = false }()

		wf := workflow.Workflow{
			Name: "Test Execute Failure",
			Nodes: []*workflow.Node{
				strictPromptNode("node-0", "mock-model", "This will fail"),
			},
		}
		w := httptest.NewRecorder()
		api.handleExecuteWorkflow(w, jsonRequest(t, "POST", "/api/workflows/execute", wf))

		if w.Code != http.StatusInternalServerError {
			t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, w.Code)
		}

		resp := decodeJSON(t, w)
		if success, ok := resp["success"].(bool); !ok || success {
			t.Error("Expected success to be false")
		}
		if _, ok := resp["error"]; !ok {
			t.Error("Expected error field in response")
		}
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		w := httptest.NewRecorder()
		api.handleExecuteWorkflow(w, jsonRequest(t, "POST", "/api/workflows/execute", "{invalid json"))

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
		}

		var response APIError
		json.NewDecoder(w.Body).Decode(&response)
		if response.Error != "Invalid JSON payload" {
			t.Errorf("Expected 'Invalid JSON payload' error, got: %s", response.Error)
		}
	})
}

func TestRespondWithError(t *testing.T) {
	api, _ := setupWorkflowAPI(t)

	tests := []struct {
		name         string
		statusCode   int
		message      string
		err          error
		checkDetails bool
	}{
		{"error with details", http.StatusInternalServerError, "Internal server error", fmt.Errorf("database connection failed"), true},
		{"error without details", http.StatusBadRequest, "Bad request", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			api.respondWithError(w, tt.statusCode, tt.message, tt.err)

			if w.Code != tt.statusCode {
				t.Errorf("Expected status %d, got %d", tt.statusCode, w.Code)
			}

			var response APIError
			json.NewDecoder(w.Body).Decode(&response)

			if response.Error != tt.message {
				t.Errorf("Expected error message '%s', got: %s", tt.message, response.Error)
			}
			if tt.checkDetails && response.Details == nil {
				t.Error("Expected details field to be populated")
			}
			if !tt.checkDetails && response.Details != nil {
				t.Errorf("Expected no details, got: %v", response.Details)
			}
		})
	}
}

func TestHandleDeleteWorkflowEdgeCases(t *testing.T) {
	api, db := setupWorkflowAPI(t)

	router := mux.NewRouter()
	router.HandleFunc("/api/workflows/{id}", api.handleDeleteWorkflow).Methods("DELETE")

	t.Run("delete non-existent workflow succeeds (idempotent)", func(t *testing.T) {
		w := serveHTTP(t, router, "DELETE", "/api/workflows/non-existent-id", nil)
		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("delete existing workflow", func(t *testing.T) {
		db.CreateWorkflow(&storage.WorkflowDefinition{Name: "To Be Deleted"})

		w := serveHTTP(t, router, "DELETE", "/api/workflows/delete-test-123", nil)
		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		if _, err := db.GetWorkflow("delete-test-123"); err == nil {
			t.Error("Expected workflow to be deleted")
		}
	})
}

func TestHandleUpdateWorkflowEdgeCases(t *testing.T) {
	api, db := setupWorkflowAPI(t)

	router := mux.NewRouter()
	router.HandleFunc("/api/workflows/{id}", api.handleUpdateWorkflow).Methods("PUT")

	t.Run("update with invalid JSON", func(t *testing.T) {
		w := serveHTTP(t, router, "PUT", "/api/workflows/test-id", "{invalid")
		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
		}
	})

	t.Run("update existing workflow", func(t *testing.T) {
		db.CreateWorkflow(&storage.WorkflowDefinition{
			ID: "update-test-123", Name: "Original Name",
			Definition: `{"id":"update-test-123","name":"Original Name","nodes":[],"edges":[]}`,
		})

		updateData := map[string]interface{}{
			"id": "update-test-123", "name": "Updated Name", "description": "New description",
			"nodes": []interface{}{}, "edges": []interface{}{},
		}
		w := serveHTTP(t, router, "PUT", "/api/workflows/update-test-123", updateData)
		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		updated, err := db.GetWorkflow("update-test-123")
		if err != nil {
			t.Fatalf("Failed to get updated workflow: %v", err)
		}
		if updated.Name != "Updated Name" {
			t.Errorf("Expected name to be updated to 'Updated Name', got: %s", updated.Name)
		}
	})
}

func TestHandleCreateWorkflowEdgeCases(t *testing.T) {
	api, _ := setupWorkflowAPI(t)

	t.Run("create with invalid JSON", func(t *testing.T) {
		w := httptest.NewRecorder()
		api.handleCreateWorkflow(w, jsonRequest(t, "POST", "/api/workflows", "{invalid json"))
		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
		}
	})

	t.Run("create workflow with auto-generated IDs", func(t *testing.T) {
		workflowData := map[string]interface{}{
			"name": "New Workflow",
			"nodes": []interface{}{
				map[string]interface{}{"type": "start"},
				map[string]interface{}{"type": "llm"},
			},
		}
		w := httptest.NewRecorder()
		api.handleCreateWorkflow(w, jsonRequest(t, "POST", "/api/workflows", workflowData))

		if w.Code != http.StatusCreated {
			t.Errorf("Expected status %d, got %d. Body: %s", http.StatusCreated, w.Code, w.Body.String())
		}

		resp := decodeJSON(t, w)
		wf := resp["workflow"].(map[string]interface{})
		if _, ok := wf["id"]; !ok {
			t.Error("Expected workflow to have auto-generated ID")
		}
		for i, n := range wf["nodes"].([]interface{}) {
			if _, ok := n.(map[string]interface{})["id"]; !ok {
				t.Errorf("Expected node %d to have auto-generated ID", i)
			}
		}
	})
}

func TestHandleValidateWorkflow(t *testing.T) {
	api, _ := setupWorkflowAPI(t)
	registerMockProvider(t, api, "")

	tests := []struct {
		name           string
		body           interface{}
		expectedStatus int
		wantValid      *bool
	}{
		{
			name: "valid workflow",
			body: workflow.Workflow{
				ID: "test-validation", Name: "Valid Workflow",
				Nodes: []*workflow.Node{strictPromptNode("node1", "mock-model", "Test prompt")},
			},
			expectedStatus: http.StatusOK,
			wantValid:      boolPtr(true),
		},
		{
			name: "invalid workflow with missing model",
			body: workflow.Workflow{
				ID: "test-invalid", Name: "Invalid Workflow",
				Nodes: []*workflow.Node{strictPromptNode("node1", "nonexistent-model", "Test prompt")},
			},
			expectedStatus: http.StatusOK,
			wantValid:      boolPtr(false),
		},
		{
			name:           "invalid JSON",
			body:           "{invalid",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			api.handleValidateWorkflow(w, jsonRequest(t, "POST", "/api/workflows/validate", tt.body))

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d. Body: %s", tt.expectedStatus, w.Code, w.Body.String())
			}
			if tt.wantValid != nil {
				var result workflow.ValidationResult
				if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}
				if result.Valid != *tt.wantValid {
					t.Errorf("Expected valid=%v, got valid=%v (errors: %v)", *tt.wantValid, result.Valid, result.Errors)
				}
			}
		})
	}
}

func TestHandleCancelJob(t *testing.T) {
	api, db := setupWorkflowAPI(t)

	router := mux.NewRouter()
	router.HandleFunc("/api/jobs/{id}/cancel", api.handleCancelJob).Methods("POST")

	tests := []struct {
		name           string
		setupJob       func() string
		expectedStatus int
		checkAfter     func(t *testing.T, jobID string)
	}{
		{
			name:           "cancel non-existent job",
			setupJob:       func() string { return "nonexistent-job" },
			expectedStatus: http.StatusNotFound,
		},
		{
			name: "cancel completed job",
			setupJob: func() string {
				return createTestJob(t, db, "completed").ID
			},
			expectedStatus: http.StatusConflict,
		},
		{
			name: "cancel pending job",
			setupJob: func() string {
				return createTestJob(t, db, "pending").ID
			},
			expectedStatus: http.StatusOK,
			checkAfter: func(t *testing.T, jobID string) {
				updated, err := db.GetExecution(jobID)
				if err != nil {
					t.Fatalf("Failed to reload job: %v", err)
				}
				if updated.Status != "cancelled" {
					t.Fatalf("Expected cancelled status, got %s", updated.Status)
				}
			},
		},
		{
			name: "cancel job not tracked by manager",
			setupJob: func() string {
				return createTestJob(t, db, "running").ID
			},
			expectedStatus: http.StatusConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobID := tt.setupJob()
			w := serveHTTP(t, router, "POST", "/api/jobs/"+jobID+"/cancel", nil)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d. Body: %s", tt.expectedStatus, w.Code, w.Body.String())
			}
			if tt.checkAfter != nil {
				tt.checkAfter(t, jobID)
			}
		})
	}

	t.Run("missing job ID", func(t *testing.T) {
		w := httptest.NewRecorder()
		api.handleCancelJob(w, httptest.NewRequest("POST", "/api/jobs//cancel", nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
		}
	})
}

func TestHandleBulkJobControls(t *testing.T) {
	setup := func(t *testing.T) (*WorkflowAPI, *storage.Storage, *mux.Router) {
		t.Helper()
		api, db := setupWorkflowAPI(t)
		router := mux.NewRouter()
		router.HandleFunc("/api/jobs/pause-all", api.handlePauseAllJobs).Methods("POST")
		router.HandleFunc("/api/jobs/resume-all", api.handleResumeAllJobs).Methods("POST")
		router.HandleFunc("/api/jobs/cancel-all", api.handleCancelAllJobs).Methods("POST")
		return api, db, router
	}

	t.Run("pause all pending jobs", func(t *testing.T) {
		_, db, router := setup(t)
		pendingA := createTestJob(t, db, "pending")
		pendingB := createTestJob(t, db, "pending")
		createTestJob(t, db, "running")

		w := serveHTTP(t, router, "POST", "/api/jobs/pause-all", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		resp := decodeJSON(t, w)
		if pausedCount, ok := resp["paused_count"].(float64); !ok || pausedCount != 2 {
			t.Fatalf("Expected paused_count=2, got %#v", resp["paused_count"])
		}

		updatedA, err := db.GetExecution(pendingA.ID)
		if err != nil {
			t.Fatalf("Failed to reload pendingA: %v", err)
		}
		updatedB, err := db.GetExecution(pendingB.ID)
		if err != nil {
			t.Fatalf("Failed to reload pendingB: %v", err)
		}
		if updatedA.Status != "paused" || updatedB.Status != "paused" {
			t.Fatalf("Expected both pending jobs to be paused, got %s and %s", updatedA.Status, updatedB.Status)
		}
	})

	t.Run("resume all paused jobs", func(t *testing.T) {
		_, db, router := setup(t)
		pausedA := createTestJob(t, db, "paused")
		pausedB := createTestJob(t, db, "paused")
		createTestJob(t, db, "running")

		w := serveHTTP(t, router, "POST", "/api/jobs/resume-all", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		resp := decodeJSON(t, w)
		if resumedCount, ok := resp["resumed_count"].(float64); !ok || resumedCount != 2 {
			t.Fatalf("Expected resumed_count=2, got %#v", resp["resumed_count"])
		}

		updatedA, err := db.GetExecution(pausedA.ID)
		if err != nil {
			t.Fatalf("Failed to reload pausedA: %v", err)
		}
		updatedB, err := db.GetExecution(pausedB.ID)
		if err != nil {
			t.Fatalf("Failed to reload pausedB: %v", err)
		}
		if updatedA.Status != "pending" || updatedB.Status != "pending" {
			t.Fatalf("Expected paused jobs to return to pending, got %s and %s", updatedA.Status, updatedB.Status)
		}
	})

	t.Run("cancel all cancels pending and paused jobs", func(t *testing.T) {
		_, db, router := setup(t)
		pendingJob := createTestJob(t, db, "pending")
		pausedJob := createTestJob(t, db, "paused")

		w := serveHTTP(t, router, "POST", "/api/jobs/cancel-all", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		resp := decodeJSON(t, w)
		if queuedCancelled, ok := resp["queued_cancelled"].(float64); !ok || queuedCancelled != 2 {
			t.Fatalf("Expected queued_cancelled=2, got %#v", resp["queued_cancelled"])
		}

		updatedPending, err := db.GetExecution(pendingJob.ID)
		if err != nil {
			t.Fatalf("Failed to reload pending job: %v", err)
		}
		updatedPaused, err := db.GetExecution(pausedJob.ID)
		if err != nil {
			t.Fatalf("Failed to reload paused job: %v", err)
		}

		if updatedPending.Status != "cancelled" || updatedPaused.Status != "cancelled" {
			t.Fatalf("Expected pending+paused jobs to be cancelled, got %s and %s", updatedPending.Status, updatedPaused.Status)
		}
		if updatedPending.ErrorMessage == "" || updatedPaused.ErrorMessage == "" {
			t.Fatal("Expected bulk-cancelled jobs to have error messages")
		}
	})
}

func TestHandleSubmitWorkflow_PoolExhausted(t *testing.T) {
	db, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create in-memory storage: %v", err)
	}

	registry := providers.NewRegistry()
	registry.Register(&MockExecuteProvider{
		name:     "mock",
		models:   []providers.Model{{ID: "mock-model", Provider: "mock", InputCost: 0.001, OutputCost: 0.002}},
		response: "ok",
	})

	cfg := jobs.DefaultManagerConfig()
	cfg.MaxConcurrentWorkflows = 1
	manager := jobs.NewManagerWithConfig(db, registry, cfg)
	api := NewWorkflowAPI(db, registry, manager)

	router := mux.NewRouter()
	router.HandleFunc("/api/workflows/submit", api.handleSubmitWorkflow).Methods("POST")

	submit := func(wfID string) *httptest.ResponseRecorder {
		payload := map[string]interface{}{
			"workflow": map[string]interface{}{
				"id": wfID, "name": "Pool Test " + wfID,
				"nodes": []interface{}{
					strictPromptNodeMap("n1", "mock-model", "hello"),
				},
			},
			"force_new_run": true,
		}
		return serveHTTP(t, router, "POST", "/api/workflows/submit", payload)
	}

	first := submit("wf-pool-1")
	if first.Code != http.StatusCreated {
		t.Fatalf("Expected first submit status %d, got %d. Body: %s", http.StatusCreated, first.Code, first.Body.String())
	}

	second := submit("wf-pool-2")
	if second.Code != http.StatusServiceUnavailable {
		t.Fatalf("Expected second submit status %d, got %d. Body: %s", http.StatusServiceUnavailable, second.Code, second.Body.String())
	}

	resp := decodeJSON(t, second)
	if resp["code"] != ErrCodePoolExhausted {
		t.Fatalf("Expected error code %s, got %v", ErrCodePoolExhausted, resp["code"])
	}
}

func TestHandleSubmitWorkflow_AdmissionPaused(t *testing.T) {
	api, _ := setupWorkflowAPI(t)
	registerMockProvider(t, api, "ok")

	if _, err := api.jobManager.PauseAllPendingJobs(context.Background()); err != nil {
		t.Fatalf("failed to pause admission: %v", err)
	}

	router := mux.NewRouter()
	router.HandleFunc("/api/workflows/submit", api.handleSubmitWorkflow).Methods("POST")

	payload := map[string]interface{}{
		"workflow": map[string]interface{}{
			"id":   "wf-submit-paused",
			"name": "Submit Paused",
			"nodes": []interface{}{
				strictPromptNodeMap("n1", "mock-model", "hello"),
			},
		},
		"force_new_run": true,
	}
	w := serveHTTP(t, router, "POST", "/api/workflows/submit", payload)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d. body=%s", http.StatusServiceUnavailable, w.Code, w.Body.String())
	}
	resp := decodeJSON(t, w)
	if resp["code"] != ErrCodeAdmissionPaused {
		t.Fatalf("expected error code %s, got %v", ErrCodeAdmissionPaused, resp["code"])
	}
	if details, ok := resp["details"].(map[string]interface{}); !ok || details["reason"] == "" {
		t.Fatalf("expected structured pause details, got %v", resp["details"])
	}
}

func TestHandleExecuteWorkflow_PoolExhausted(t *testing.T) {
	db, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create in-memory storage: %v", err)
	}

	registry := providers.NewRegistry()
	registry.Register(&MockExecuteProvider{
		name:     "mock",
		models:   []providers.Model{{ID: "mock-model", Provider: "mock", InputCost: 0.001, OutputCost: 0.002}},
		response: "ok",
	})

	cfg := jobs.DefaultManagerConfig()
	cfg.MaxConcurrentWorkflows = 1
	cfg.WorkerCount = 1
	cfg.WorkerPollInterval = 10 * time.Second
	manager := jobs.NewManagerWithConfig(db, registry, cfg)

	// Seed one pending durable root job before workers start so admission is full.
	_, err = manager.SubmitWorkflow(context.Background(), &jobs.SubmitWorkflowRequest{
		Workflow: &workflow.Workflow{
			ID:   "wf-pending",
			Name: "Pending",
			Nodes: []*workflow.Node{
				strictPromptNode("n1", "mock-model", "hello"),
			},
		},
		ForceNewRun: true,
	})
	if err != nil {
		t.Fatalf("Failed to seed pending workflow: %v", err)
	}

	manager.StartWorkers()
	t.Cleanup(func() { manager.StopWorkers(context.Background()) })

	api := NewWorkflowAPI(db, registry, manager)

	wf := workflow.Workflow{
		ID:   "wf-exec",
		Name: "Execute",
		Nodes: []*workflow.Node{
			strictPromptNode("n1", "mock-model", "hello"),
		},
	}

	w := httptest.NewRecorder()
	api.handleExecuteWorkflow(w, jsonRequest(t, "POST", "/api/workflows/execute", wf))

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusServiceUnavailable, w.Code, w.Body.String())
	}

	resp := decodeJSON(t, w)
	if resp["code"] != ErrCodePoolExhausted {
		t.Fatalf("Expected error code %s, got %v", ErrCodePoolExhausted, resp["code"])
	}
}

func TestHandleExecuteWorkflow_AdmissionPaused(t *testing.T) {
	api, _ := setupWorkflowAPI(t)
	registerMockProvider(t, api, "ok")

	if _, err := api.jobManager.PauseAllPendingJobs(context.Background()); err != nil {
		t.Fatalf("failed to pause admission: %v", err)
	}

	router := mux.NewRouter()
	router.HandleFunc("/api/workflows/execute", api.handleExecuteWorkflow).Methods("POST")

	payload := map[string]interface{}{
		"id":   "wf-execute-paused",
		"name": "Execute Paused",
		"nodes": []interface{}{
			strictPromptNodeMap("n1", "mock-model", "hello"),
		},
	}
	w := serveHTTP(t, router, "POST", "/api/workflows/execute", payload)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d. body=%s", http.StatusServiceUnavailable, w.Code, w.Body.String())
	}
	resp := decodeJSON(t, w)
	if resp["code"] != ErrCodeAdmissionPaused {
		t.Fatalf("expected error code %s, got %v", ErrCodeAdmissionPaused, resp["code"])
	}
}

func TestHandleSubmitWorkflow_WorkflowFileConversion(t *testing.T) {
	api, db := setupWorkflowAPI(t)
	registerMockProvider(t, api, "ok")

	router := mux.NewRouter()
	router.HandleFunc("/api/workflows/submit", api.handleSubmitWorkflow).Methods("POST")

	payload := map[string]interface{}{
		"workflow_file": map[string]interface{}{
			"name": "File Conversion Workflow",
			"nodes": []interface{}{
				map[string]interface{}{
					"id": "input-1", "type": "input",
					"data": map[string]interface{}{"type": "input", "label": "Input", "config": map[string]interface{}{"name": "query"}},
				},
				map[string]interface{}{
					"id": "agent-1", "type": "agent",
					"data": map[string]interface{}{"type": "agent", "label": "Agent", "config": map[string]interface{}{
						"name":           "writer",
						"model":          "mock-model",
						"systemPrompt":   "Write an answer",
						"temperature":    0.0,
						"maxTokens":      256,
						"timeoutSeconds": 30,
						"retryPolicy": map[string]interface{}{
							"max_attempts":     1,
							"backoff_ms":       0,
							"backoff_multiply": 1.0,
							"max_backoff_ms":   0,
						},
						"openRouterProvider": map[string]interface{}{
							"only":            []string{"OpenAI"},
							"allow_fallbacks": false,
						},
						"openRouterReasoning": map[string]interface{}{
							"effort": "high",
						},
					}},
				},
				map[string]interface{}{
					"id": "result-1", "type": "result",
					"data": map[string]interface{}{"type": "result", "label": "Result", "config": map[string]interface{}{
						"name":              "final",
						"outputFormat":      "text",
						"aggregationMethod": "judge",
						"timeoutSeconds":    30,
						"retryPolicy": map[string]interface{}{
							"max_attempts":     1,
							"backoff_ms":       0,
							"backoff_multiply": 1.0,
							"max_backoff_ms":   0,
						},
						"aggregationConfig": map[string]interface{}{
							"judge_model":       "mock-model",
							"system_prompt":     "You are an impartial judge evaluating candidate responses.",
							"prompt":            "Question: {{question}}\n\nCandidate responses:\n{{responses_json}}",
							"temperature":       0.0,
							"max_tokens":        -1,
							"repair_max_tokens": 256,
						},
						"openRouterProvider": map[string]interface{}{
							"only":            []string{"OpenAI"},
							"allow_fallbacks": false,
						},
						"openRouterReasoning": map[string]interface{}{
							"effort": "high",
						},
					}},
				},
			},
			"edges": []interface{}{
				map[string]interface{}{"id": "e1", "source": "input-1", "target": "agent-1"},
				map[string]interface{}{"id": "e2", "source": "agent-1", "target": "result-1"},
			},
		},
		"input_values":  map[string]interface{}{"query": "What is AI?"},
		"force_new_run": true,
	}

	w := serveHTTP(t, router, "POST", "/api/workflows/submit", payload)
	if w.Code != http.StatusCreated {
		t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusCreated, w.Code, w.Body.String())
	}

	resp := decodeJSON(t, w)
	jobID, ok := resp["job_id"].(string)
	if !ok || jobID == "" {
		t.Fatalf("Expected non-empty job_id in response, got: %v", resp["job_id"])
	}

	job, err := db.GetExecution(jobID)
	if err != nil {
		t.Fatalf("Failed to fetch created job: %v", err)
	}

	var persisted workflow.Workflow
	if err := json.Unmarshal([]byte(job.RequestData), &persisted); err != nil {
		t.Fatalf("Failed to parse persisted workflow: %v", err)
	}

	if len(persisted.Nodes) != 2 {
		t.Fatalf("Expected 2 executable nodes (agent + result), got %d", len(persisted.Nodes))
	}
	if persisted.Nodes[0].Type != workflow.NodeTypePrompt {
		t.Fatalf("Expected first node type %s, got %s", workflow.NodeTypePrompt, persisted.Nodes[0].Type)
	}
	if persisted.Nodes[1].Type != workflow.NodeTypeResult {
		t.Fatalf("Expected second node type %s, got %s", workflow.NodeTypeResult, persisted.Nodes[1].Type)
	}
	if persisted.Context["query"] != "What is AI?" {
		t.Fatalf("Expected input_values to become workflow context, got %v", persisted.Context["query"])
	}
	if !strings.Contains(persisted.Nodes[0].Prompt, "{{query}}") {
		t.Fatalf("Expected converted prompt to include input placeholder, got %q", persisted.Nodes[0].Prompt)
	}
	if persisted.Nodes[0].Metadata["openrouter_provider"] == nil {
		t.Fatalf("Expected converted agent metadata to include openrouter_provider, got %+v", persisted.Nodes[0].Metadata)
	}
	if persisted.Nodes[0].Metadata["openrouter_reasoning"] == nil {
		t.Fatalf("Expected converted agent metadata to include openrouter_reasoning, got %+v", persisted.Nodes[0].Metadata)
	}
	if persisted.Nodes[1].AggregationConfig == nil {
		t.Fatalf("Expected result aggregation config to be present")
	}
	if persisted.Nodes[1].AggregationConfig["openRouterReasoning"] == nil {
		t.Fatalf("Expected converted result aggregation config to include openRouterReasoning, got %+v", persisted.Nodes[1].AggregationConfig)
	}
	if persisted.Nodes[1].AggregationConfig["openRouterProvider"] == nil {
		t.Fatalf("Expected converted result aggregation config to include openRouterProvider, got %+v", persisted.Nodes[1].AggregationConfig)
	}
}

func TestHandleGetJobConfig(t *testing.T) {
	api, db := setupWorkflowAPI(t)

	router := mux.NewRouter()
	router.HandleFunc("/api/jobs/{id}/config", api.handleGetJobConfig).Methods("GET")

	wf := workflow.Workflow{
		ID: "test-wf-config", Name: "Config Test",
		Nodes: []*workflow.Node{
			{ID: "s1", Type: workflow.NodeTypePrompt, Model: "gpt-4o", Prompt: "Hello", Temperature: providers.Float64Ptr(0.5), MaxTokens: 2000},
		},
		Edges: []*workflow.Edge{},
	}
	requestData, _ := json.Marshal(wf)
	jobWithConfig := createTestJob(t, db, "completed", func(j *storage.Job) {
		j.Model = "gpt-4o"
		j.RequestData = string(requestData)
		j.WorkflowID = "test-wf-config"
	})

	t.Run("returns normalized config", func(t *testing.T) {
		w := serveHTTP(t, router, "GET", "/api/jobs/"+jobWithConfig.ID+"/config", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		resp := decodeJSON(t, w)
		if _, ok := resp["config_hash"].(string); !ok {
			t.Error("Expected config_hash in response")
		}
		nodes, ok := resp["nodes"].([]interface{})
		if !ok || len(nodes) == 0 {
			t.Fatal("Expected non-empty nodes array")
		}
		node := nodes[0].(map[string]interface{})
		if node["model"] != "gpt-4o" {
			t.Errorf("Expected model gpt-4o, got %v", node["model"])
		}
		if node["temperature"] != 0.5 {
			t.Errorf("Expected temperature 0.5, got %v", node["temperature"])
		}
	})

	t.Run("not found for missing job", func(t *testing.T) {
		w := serveHTTP(t, router, "GET", "/api/jobs/nonexistent/config", nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
		}
	})

	t.Run("not found when request_data is empty", func(t *testing.T) {
		emptyJob := createTestJob(t, db, "completed")
		w := serveHTTP(t, router, "GET", "/api/jobs/"+emptyJob.ID+"/config", nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
		}
	})
}

func TestHandleConfigDiff(t *testing.T) {
	api, db := setupWorkflowAPI(t)

	router := mux.NewRouter()
	router.HandleFunc("/api/jobs/{id}/diff/{id2}", api.handleConfigDiff).Methods("GET")

	wf1 := workflow.Workflow{
		ID:    "wf1",
		Nodes: []*workflow.Node{{ID: "s1", Type: workflow.NodeTypePrompt, Model: "gpt-4o", Temperature: providers.Float64Ptr(0.5)}},
	}
	wf2 := workflow.Workflow{
		ID:    "wf2",
		Nodes: []*workflow.Node{{ID: "s1", Type: workflow.NodeTypePrompt, Model: "claude-3.5-sonnet", Temperature: providers.Float64Ptr(0.9)}},
	}
	reqData1, _ := json.Marshal(wf1)
	reqData2, _ := json.Marshal(wf2)

	job1 := createTestJob(t, db, "completed", func(j *storage.Job) {
		j.Model = "gpt-4o"
		j.RequestData = string(reqData1)
	})
	job2 := createTestJob(t, db, "completed", func(j *storage.Job) {
		j.Model = "claude-3.5-sonnet"
		j.RequestData = string(reqData2)
	})

	t.Run("returns diffs for different configs", func(t *testing.T) {
		w := serveHTTP(t, router, "GET", "/api/jobs/"+job1.ID+"/diff/"+job2.ID, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		resp := decodeJSON(t, w)
		if resp["identical"] == true {
			t.Error("Expected identical=false for different configs")
		}

		diffs, ok := resp["diffs"].([]interface{})
		if !ok || len(diffs) == 0 {
			t.Fatal("Expected non-empty diffs array")
		}

		foundModel, foundTemp := false, false
		for _, d := range diffs {
			path := d.(map[string]interface{})["path"].(string)
			if path == "nodes.s1.model" {
				foundModel = true
			}
			if path == "nodes.s1.temperature" {
				foundTemp = true
			}
		}
		if !foundModel {
			t.Error("Expected model diff")
		}
		if !foundTemp {
			t.Error("Expected temperature diff")
		}
	})

	t.Run("identical configs returns identical=true", func(t *testing.T) {
		w := serveHTTP(t, router, "GET", "/api/jobs/"+job1.ID+"/diff/"+job1.ID, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		resp := decodeJSON(t, w)
		if resp["identical"] != true {
			t.Error("Expected identical=true when diffing a job against itself")
		}
	})

	t.Run("not found for missing job", func(t *testing.T) {
		w := serveHTTP(t, router, "GET", "/api/jobs/nonexistent/diff/"+job2.ID, nil)
		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
		}
	})
}

func TestHandleGetJob_ConfigHash(t *testing.T) {
	api, db := setupWorkflowAPI(t)

	router := mux.NewRouter()
	router.HandleFunc("/api/jobs/{id}", api.handleGetJob).Methods("GET")

	job := createTestJob(t, db, "completed", func(j *storage.Job) {
		j.ConfigHash = "abc123deadbeef"
	})

	w := serveHTTP(t, router, "GET", "/api/jobs/"+job.ID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	resp := decodeJSON(t, w)
	if resp["config_hash"] != "abc123deadbeef" {
		t.Errorf("Expected config_hash 'abc123deadbeef', got %v", resp["config_hash"])
	}
}

func TestHandleListJobs_ConfigHashFilter(t *testing.T) {
	api, db := setupWorkflowAPI(t)

	router := mux.NewRouter()
	router.HandleFunc("/api/jobs", api.handleListJobs).Methods("GET")

	for i, hash := range []string{"hash-aaa", "hash-aaa", "hash-bbb"} {
		createTestJob(t, db, "completed", func(j *storage.Job) {
			j.ID = fmt.Sprintf("job-filter-%d-%s", i, j.ID)
			j.ConfigHash = hash
		})
	}

	w := serveHTTP(t, router, "GET", "/api/jobs?config_hash=hash-aaa", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	resp := decodeJSON(t, w)
	jobsList, ok := resp["jobs"].([]interface{})
	if !ok {
		t.Fatal("Expected jobs array in response")
	}

	for _, j := range jobsList {
		if j.(map[string]interface{})["config_hash"] != "hash-aaa" {
			t.Errorf("Expected config_hash 'hash-aaa', got %v", j.(map[string]interface{})["config_hash"])
		}
	}
	if len(jobsList) != 2 {
		t.Errorf("Expected 2 jobs with hash-aaa, got %d", len(jobsList))
	}
}

func TestHandleListJobs_IncludesDurableMetadata(t *testing.T) {
	api, db := setupWorkflowAPI(t)

	router := mux.NewRouter()
	router.HandleFunc("/api/jobs", api.handleListJobs).Methods("GET")

	createTestJob(t, db, "completed", func(j *storage.Job) {
		j.Description = "durable metadata test"
		j.RunNumber = 2
		j.DAGHash = "abc123def456"
		j.DAGSnapshot = `{"nodes":[],"edges":[]}`
		j.WorkflowExecutionID = "exec-123"
		j.RunID = "run-123"
	})

	w := serveHTTP(t, router, "GET", "/api/jobs?limit=1", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	resp := decodeJSON(t, w)
	jobsList, ok := resp["jobs"].([]interface{})
	if !ok || len(jobsList) == 0 {
		t.Fatalf("Expected at least one job in response")
	}

	jobMap := jobsList[0].(map[string]interface{})
	if got, ok := jobMap["run_number"].(float64); !ok || int(got) != 2 {
		t.Fatalf("Expected run_number=2, got %v", jobMap["run_number"])
	}
	if got, ok := jobMap["dag_hash"].(string); !ok || got != "abc123def456" {
		t.Fatalf("Expected dag_hash=abc123def456, got %v", jobMap["dag_hash"])
	}
	if got, ok := jobMap["workflow_execution_id"].(string); !ok || got != "exec-123" {
		t.Fatalf("Expected workflow_execution_id=exec-123, got %v", jobMap["workflow_execution_id"])
	}
}

func boolPtr(b bool) *bool { return &b }
