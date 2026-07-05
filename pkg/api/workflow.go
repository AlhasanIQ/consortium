package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/alhasaniq/consortium/pkg/jobs"
	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/gorilla/mux"
)

type WorkflowAPI struct {
	storage             *storage.Storage
	registry            *providers.Registry
	jobManager          *jobs.Manager
	rateLimiter         *openAIRateLimiter
	preAuthRequestLimit int
}

// NewWorkflowAPI creates a new workflow API handler. The manager parameter is
// the shared jobs.Manager instance — all execution goes through this single
// manager so admission gating is consistent across the process.
func NewWorkflowAPI(storage *storage.Storage, registry *providers.Registry, manager *jobs.Manager) *WorkflowAPI {
	return &WorkflowAPI{
		storage:             storage,
		registry:            registry,
		jobManager:          manager,
		rateLimiter:         newOpenAIRateLimiter(),
		preAuthRequestLimit: openAIPreAuthRequestsPerMinute,
	}
}

// decodeWorkflow decodes a workflow from the request body
// Returns true if successful; on error, writes error response and returns false
func (api *WorkflowAPI) decodeWorkflow(w http.ResponseWriter, r *http.Request) (*workflow.Workflow, bool) {
	var wf workflow.Workflow
	if err := json.NewDecoder(r.Body).Decode(&wf); err != nil {
		api.respondWithError(w, http.StatusBadRequest, "Invalid JSON payload", err)
		return nil, false
	}
	return &wf, true
}

// respondWithError sends a standardized error response
func (api *WorkflowAPI) respondWithError(w http.ResponseWriter, statusCode int, message string, err error) {
	errResp := APIError{
		Error: message,
	}

	if err != nil {
		errResp.Details = err.Error()
	}

	// Log error
	if err != nil {
		log.Printf("API Error [%d]: %s - %v", statusCode, message, err)
	} else {
		log.Printf("API Error [%d]: %s", statusCode, message)
	}

	// Marshal JSON before writing headers to avoid partial writes
	respJSON, encErr := json.Marshal(errResp)
	if encErr != nil {
		log.Printf("Failed to marshal error response: %v", encErr)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		if _, err := w.Write([]byte(`{"error":"Internal server error","code":"INTERNAL_ERROR"}`)); err != nil {
			log.Printf("Error writing error response: %v", err)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if _, err := w.Write(respJSON); err != nil {
		log.Printf("Error writing response: %v", err)
	}
}

// respondWithJSON sends a standardized JSON response
func (api *WorkflowAPI) respondWithJSON(w http.ResponseWriter, statusCode int, payload interface{}) {
	// Marshal JSON before writing headers to avoid partial writes
	respJSON, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Failed to marshal JSON response: %v", err)
		api.respondWithError(w, http.StatusInternalServerError, "Failed to encode response", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if _, err := w.Write(respJSON); err != nil {
		log.Printf("Error writing JSON response: %v", err)
	}
}

func (api *WorkflowAPI) RegisterRoutes(r *mux.Router) {
	api.registerOpenAIRoutes(r)

	r.HandleFunc("/api/models", api.handleListModels).Methods("GET")

	// TODO(v0.1-security): These builder/operator routes are intentionally
	// documented as trusted-local APIs for v0.1. Add auth/tenant scoping before
	// exposing /api/workflows or /api/jobs directly to untrusted clients.
	r.HandleFunc("/api/workflows", api.handleListWorkflows).Methods("GET")
	r.HandleFunc("/api/workflows", api.handleCreateWorkflow).Methods("POST")
	r.HandleFunc("/api/workflows/execute", api.handleExecuteWorkflow).Methods("POST")
	r.HandleFunc("/api/workflows/execute/ws", api.handleExecuteWSGone)
	r.HandleFunc("/api/workflows/validate", api.handleValidateWorkflow).Methods("POST")
	r.HandleFunc("/api/workflows/seeds", api.handleGetSeedWorkflows).Methods("GET")
	r.HandleFunc("/api/workflows/compile-preview", api.handleCompilePreview).Methods("POST")
	r.HandleFunc("/api/workflows/{id}", api.handleGetWorkflow).Methods("GET")
	r.HandleFunc("/api/workflows/{id}", api.handleUpdateWorkflow).Methods("PUT")
	r.HandleFunc("/api/workflows/{id}", api.handleDeleteWorkflow).Methods("DELETE")

	// New idempotent submission endpoint (submit workflow, get job ID)
	r.HandleFunc("/api/workflows/submit", api.handleSubmitWorkflow).Methods("POST")

	// Job endpoints - more specific routes first
	r.HandleFunc("/api/jobs/pause-all", api.handlePauseAllJobs).Methods("POST")
	r.HandleFunc("/api/jobs/resume-all", api.handleResumeAllJobs).Methods("POST")
	r.HandleFunc("/api/jobs/cancel-all", api.handleCancelAllJobs).Methods("POST")
	r.HandleFunc("/api/jobs/{id}/cancel", api.handleCancelJob).Methods("POST")
	r.HandleFunc("/api/jobs/{id}/resume", api.handleResumeJob).Methods("POST")
	r.HandleFunc("/api/jobs/{id}/stream", api.handleJobStream) // WebSocket for streaming job events
	r.HandleFunc("/api/jobs/{id}/trace", api.handleGetJobTrace).Methods("GET")
	r.HandleFunc("/api/jobs/{id}/config", api.handleGetJobConfig).Methods("GET")
	r.HandleFunc("/api/jobs/{id}/workflow", api.handleGetJobWorkflow).Methods("GET")
	r.HandleFunc("/api/jobs/{id}/diff/{id2}", api.handleConfigDiff).Methods("GET")
	r.HandleFunc("/api/jobs/{id}", api.handleGetJob).Methods("GET")
	r.HandleFunc("/api/jobs", api.handleListJobs).Methods("GET")
}

// handleExecuteWSGone returns 410 Gone for the removed WebSocket execute endpoint.
// Clients should use POST /api/workflows/submit followed by WS /api/jobs/{id}/stream.
func (api *WorkflowAPI) handleExecuteWSGone(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusGone)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": "This endpoint has been removed. Use POST /api/workflows/submit followed by WS /api/jobs/{id}/stream.",
	})
}

func (api *WorkflowAPI) handleListModels(w http.ResponseWriter, r *http.Request) {
	providerFilter := r.URL.Query().Get("provider")
	allModels := api.registry.GetModels()

	var models []providers.Model
	if providerFilter != "" {
		for _, m := range allModels {
			if m.Provider == providerFilter {
				models = append(models, m)
			}
		}
		if models == nil {
			models = []providers.Model{}
		}
	} else {
		models = allModels
	}

	api.respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"models": models,
	})
}

func (api *WorkflowAPI) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	workflows, err := api.storage.ListWorkflows(100)
	if err != nil {
		api.respondWithError(w, http.StatusInternalServerError, "Failed to list workflows", err)
		return
	}

	// Ensure we return empty array instead of null
	if workflows == nil {
		workflows = []storage.WorkflowDefinition{}
	}

	api.respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"workflows": workflows,
	})
}

// handleGetSeedWorkflows returns metadata about available seed workflows
func (api *WorkflowAPI) handleGetSeedWorkflows(w http.ResponseWriter, r *http.Request) {
	seeds, err := api.storage.GetSeedWorkflows()
	if err != nil {
		api.respondWithError(w, http.StatusInternalServerError, "Failed to get seed workflows", err)
		return
	}

	api.respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"seeds": seeds,
	})
}

func (api *WorkflowAPI) handleCreateWorkflow(w http.ResponseWriter, r *http.Request) {
	// Expect the complete workflow file format (nodes, edges, metadata)
	var workflowFile map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&workflowFile); err != nil {
		api.respondWithError(w, http.StatusBadRequest, "Invalid JSON payload", err)
		return
	}

	// Normalize all IDs (workflow, nodes, edges)
	if err := workflow.NormalizeWorkflowFileIDs(workflowFile); err != nil {
		api.respondWithError(w, http.StatusInternalServerError, "Failed to normalize IDs", err)
		return
	}

	// Extract metadata
	id := workflowFile["id"].(string)
	name, _ := workflowFile["name"].(string)
	if name == "" {
		name = "Untitled Workflow"
	}

	description, _ := workflowFile["description"].(string)

	// Marshal the complete workflow definition
	definitionJSON, err := json.Marshal(workflowFile)
	if err != nil {
		api.respondWithError(w, http.StatusInternalServerError, "Failed to marshal workflow", err)
		return
	}

	// Save to database
	wfDef := &storage.WorkflowDefinition{
		ID:          id,
		Name:        name,
		Description: description,
		Definition:  string(definitionJSON),
	}

	if err := api.storage.CreateWorkflow(wfDef); err != nil {
		api.respondWithError(w, http.StatusInternalServerError, "Failed to save workflow", err)
		return
	}

	api.respondWithJSON(w, http.StatusCreated, map[string]interface{}{
		"id":       id,
		"message":  "Workflow created successfully",
		"workflow": workflowFile, // Return normalized workflow with all IDs assigned
	})
}

func (api *WorkflowAPI) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	if id == "" {
		api.respondWithError(w, http.StatusBadRequest, "Missing workflow ID", nil)
		return
	}

	wfDef, err := api.storage.GetWorkflow(id)
	if err != nil {
		api.respondWithError(w, http.StatusNotFound, "Workflow not found", err)
		return
	}

	// Parse the definition JSON
	var workflowFile map[string]interface{}
	if err := json.Unmarshal([]byte(wfDef.Definition), &workflowFile); err != nil {
		api.respondWithError(w, http.StatusInternalServerError, "Failed to parse workflow definition", err)
		return
	}

	api.respondWithJSON(w, http.StatusOK, workflowFile)
}

func (api *WorkflowAPI) handleUpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	if id == "" {
		api.respondWithError(w, http.StatusBadRequest, "Missing workflow ID", nil)
		return
	}

	var workflowFile map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&workflowFile); err != nil {
		api.respondWithError(w, http.StatusBadRequest, "Invalid JSON payload", err)
		return
	}

	// Ensure ID matches URL
	workflowFile["id"] = id

	// Normalize all IDs (nodes, edges) - preserving existing workflow ID
	if err := workflow.NormalizeWorkflowFileIDs(workflowFile); err != nil {
		api.respondWithError(w, http.StatusInternalServerError, "Failed to normalize IDs", err)
		return
	}

	name, _ := workflowFile["name"].(string)
	if name == "" {
		name = "Untitled Workflow"
	}

	description, _ := workflowFile["description"].(string)

	// Marshal the complete workflow definition
	definitionJSON, err := json.Marshal(workflowFile)
	if err != nil {
		api.respondWithError(w, http.StatusInternalServerError, "Failed to marshal workflow", err)
		return
	}

	// Update in database
	wfDef := &storage.WorkflowDefinition{
		ID:          id,
		Name:        name,
		Description: description,
		Definition:  string(definitionJSON),
	}

	if err := api.storage.UpdateWorkflow(wfDef); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			api.respondWithError(w, http.StatusNotFound, "Workflow not found", err)
			return
		}
		api.respondWithError(w, http.StatusInternalServerError, "Failed to update workflow", err)
		return
	}

	api.respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"id":       id,
		"message":  "Workflow updated successfully",
		"workflow": workflowFile, // Return normalized workflow with all IDs
	})
}

func (api *WorkflowAPI) handleDeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	if id == "" {
		api.respondWithError(w, http.StatusBadRequest, "Missing workflow ID", nil)
		return
	}

	if err := api.storage.DeleteWorkflow(id); err != nil {
		api.respondWithError(w, http.StatusInternalServerError, "Failed to delete workflow", err)
		return
	}

	api.respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Workflow deleted successfully",
	})
}

// handleValidateWorkflow validates a workflow without executing it
func (api *WorkflowAPI) handleValidateWorkflow(w http.ResponseWriter, r *http.Request) {
	wf, ok := api.decodeWorkflow(w, r)
	if !ok {
		return
	}

	// Create validator
	validator := workflow.NewValidator(api.registry)

	// Validate workflow
	result := validator.Validate(wf)

	api.respondWithJSON(w, http.StatusOK, result)
}
