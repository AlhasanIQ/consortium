package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/alhasaniq/consortium/pkg/jobs"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

func (api *WorkflowAPI) handleExecuteWorkflow(w http.ResponseWriter, r *http.Request) {
	wf, ok := api.decodeWorkflow(w, r)
	if !ok {
		return
	}

	// Normalize workflow ID
	if wf.ID == "" {
		wf.ID = uuid.New().String()
	}
	// Note: Node IDs are auto-generated during execution, not from user input

	// Validate workflow before execution
	validator := workflow.NewValidator(api.registry)
	validationResult := validator.Validate(wf)
	if !validationResult.Valid {
		api.respondWithJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error":   "Workflow validation failed",
			"code":    ErrCodeInvalidWorkflow,
			"details": validationResult.Errors,
		})
		return
	}

	// Execute workflow through durable submit+wait.
	ctx := r.Context()
	execResult, err := api.jobManager.ExecuteWorkflow(ctx, wf)
	if err != nil {
		if jobs.IsAdmissionPausedError(err) {
			w.Header().Set("Retry-After", "30")
			payload := map[string]interface{}{
				"error": "Admission paused",
				"code":  ErrCodeAdmissionPaused,
			}
			if reason, ok := jobs.AdmissionPauseFromError(err); ok && reason != nil {
				payload["details"] = reason
			}
			api.respondWithJSON(w, http.StatusServiceUnavailable, payload)
			return
		}
		if jobs.IsAdmissionError(err) {
			w.Header().Set("Retry-After", "1")
			api.respondWithJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
				"error":   "Server at capacity",
				"code":    ErrCodePoolExhausted,
				"details": err.Error(),
			})
			return
		}
		api.respondWithError(w, http.StatusInternalServerError, "Failed to execute workflow", err)
		return
	}

	if !execResult.Success {
		api.respondWithJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success":     false,
			"job_id":      execResult.JobID,
			"workflow_id": execResult.WorkflowID,
			"error":       execResult.Error,
			"code":        execResult.ErrorCode,
		})
		return
	}

	api.respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"job_id":      execResult.JobID,
		"workflow_id": execResult.WorkflowID,
		"result":      execResult.Result,
	})
}

// SubmitWorkflowRequest represents the request body for workflow submission
type SubmitWorkflowRequest struct {
	Workflow       workflow.Workflow      `json:"workflow"`
	WorkflowFile   map[string]interface{} `json:"workflow_file,omitempty"`
	InputValues    map[string]interface{} `json:"input_values,omitempty"`
	IdempotencyKey string                 `json:"idempotency_key,omitempty"`
	ForceNewRun    bool                   `json:"force_new_run,omitempty"`
}

// handleSubmitWorkflow handles workflow submission with idempotency
// This creates a durable queued job; background workers execute it.
// /api/jobs/{id}/stream is subscribe/replay only.
func (api *WorkflowAPI) handleSubmitWorkflow(w http.ResponseWriter, r *http.Request) {
	var req SubmitWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("API Error [400]: Invalid JSON payload - %v", err)
		api.respondWithJSON(w, http.StatusBadRequest, NewAPIError("Invalid JSON payload", ErrCodeInvalidJSON).WithDetails(err.Error()))
		return
	}

	var submitWorkflow *workflow.Workflow
	if len(req.WorkflowFile) > 0 {
		if err := workflow.NormalizeWorkflowFileIDs(req.WorkflowFile); err != nil {
			api.respondWithError(w, http.StatusBadRequest, "Invalid workflow file", err)
			return
		}
		convertedWorkflow, err := workflowFromWorkflowFile(req.WorkflowFile, req.InputValues)
		if err != nil {
			api.respondWithError(w, http.StatusBadRequest, "Failed to convert workflow file", err)
			return
		}
		submitWorkflow = convertedWorkflow
	} else {
		submitWorkflow = &req.Workflow
	}

	// Normalize workflow ID
	if submitWorkflow.ID == "" {
		submitWorkflow.ID = uuid.New().String()
	}

	// Submit workflow with idempotency check
	ctx := r.Context()
	submitReq := &jobs.SubmitWorkflowRequest{
		Workflow:       submitWorkflow,
		IdempotencyKey: req.IdempotencyKey,
		ForceNewRun:    req.ForceNewRun,
	}

	resp, err := api.jobManager.SubmitWorkflow(ctx, submitReq)
	if err != nil {
		if jobs.IsAdmissionPausedError(err) {
			w.Header().Set("Retry-After", "30")
			payload := map[string]interface{}{
				"error": "Admission paused",
				"code":  ErrCodeAdmissionPaused,
			}
			if reason, ok := jobs.AdmissionPauseFromError(err); ok && reason != nil {
				payload["details"] = reason
			}
			api.respondWithJSON(w, http.StatusServiceUnavailable, payload)
			return
		}
		if jobs.IsAdmissionError(err) {
			w.Header().Set("Retry-After", "30")
			api.respondWithJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
				"error": "Server at capacity",
				"code":  ErrCodePoolExhausted,
			})
			return
		}
		if jobs.IsWorkflowSubmitValidationError(err) {
			api.respondWithJSON(w, http.StatusBadRequest, map[string]interface{}{
				"error":   "Workflow validation failed",
				"code":    ErrCodeInvalidWorkflow,
				"details": err.Error(),
			})
			return
		}
		api.respondWithError(w, http.StatusInternalServerError, "Failed to submit workflow", err)
		return
	}

	// Return appropriate status code
	statusCode := http.StatusCreated
	if resp.Duplicate {
		statusCode = http.StatusOK // Return 200 for duplicate (idempotent)
	}

	api.respondWithJSON(w, statusCode, map[string]interface{}{
		"job_id":              resp.JobID,
		"workflow_id":         resp.WorkflowID,
		"duplicate":           resp.Duplicate,
		"status":              resp.Status,
		"dedup_reason":        resp.DedupReason,
		"dedup_source_job_id": resp.DedupSourceJobID,
		"dedup_source_status": resp.DedupSourceStatus,
		"dedup_bypassed":      resp.DedupBypassed,
	})
}

// handleCancelJob cancels a pending or running workflow job
func (api *WorkflowAPI) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["id"]

	if jobID == "" {
		api.respondWithJSON(w, http.StatusBadRequest, APIError{
			Error: "Missing job ID",
			Code:  ErrCodeInvalidJSON,
		})
		return
	}

	// Attempt to cancel the job via JobManager
	if err := api.jobManager.CancelJob(jobID); err != nil {
		statusCode := http.StatusInternalServerError
		errorCode := ErrCodeInternalError
		errorMessage := "Failed to cancel job"

		switch {
		case errors.Is(err, storage.ErrNotFound):
			statusCode = http.StatusNotFound
			errorCode = ErrCodeJobNotFound
			errorMessage = "Job not found"
		case errors.Is(err, jobs.ErrJobStateConflict):
			statusCode = http.StatusConflict
			errorCode = ErrCodeJobNotRunning
			errorMessage = "Job is not in running state"
		}

		api.respondWithJSON(w, statusCode, APIError{
			Error:   errorMessage,
			Code:    errorCode,
			Details: err.Error(),
		})
		return
	}

	api.respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"message":    "Job cancellation requested successfully",
		"job_id":     jobID,
		"event_type": EventCancelled,
	})
}

func (api *WorkflowAPI) handleResumeJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["id"]

	if jobID == "" {
		api.respondWithJSON(w, http.StatusBadRequest, APIError{
			Error: "Missing job ID",
			Code:  ErrCodeInvalidJSON,
		})
		return
	}

	if err := api.jobManager.ResumeJob(jobID); err != nil {
		statusCode := http.StatusInternalServerError
		errorCode := ErrCodeInternalError
		errorMessage := "Failed to resume job"

		switch {
		case errors.Is(err, storage.ErrNotFound):
			statusCode = http.StatusNotFound
			errorCode = ErrCodeJobNotFound
			errorMessage = "Job not found"
		case errors.Is(err, jobs.ErrJobStateConflict):
			statusCode = http.StatusConflict
			errorCode = ErrCodeJobNotPaused
			errorMessage = "Job is not in paused state"
		}

		api.respondWithJSON(w, statusCode, APIError{
			Error:   errorMessage,
			Code:    errorCode,
			Details: err.Error(),
		})
		return
	}

	api.respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Job resumed successfully",
		"job_id":  jobID,
	})
}

func (api *WorkflowAPI) handlePauseAllJobs(w http.ResponseWriter, r *http.Request) {
	pausedCount, err := api.jobManager.PauseAllPendingJobs(r.Context())
	if err != nil {
		api.respondWithJSON(w, http.StatusInternalServerError, APIError{
			Error:   "Failed to pause pending jobs",
			Code:    ErrCodeInternalError,
			Details: err.Error(),
		})
		return
	}

	api.respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"paused_count": pausedCount,
		"message":      "Pending jobs paused",
	})
}

func (api *WorkflowAPI) handleResumeAllJobs(w http.ResponseWriter, r *http.Request) {
	resumedCount, err := api.jobManager.ResumeAllPausedJobs(r.Context())
	if err != nil {
		api.respondWithJSON(w, http.StatusInternalServerError, APIError{
			Error:   "Failed to resume paused jobs",
			Code:    ErrCodeInternalError,
			Details: err.Error(),
		})
		return
	}

	api.respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"success":       true,
		"resumed_count": resumedCount,
		"message":       "Paused jobs resumed",
	})
}

func (api *WorkflowAPI) handleCancelAllJobs(w http.ResponseWriter, r *http.Request) {
	result, err := api.jobManager.CancelAllActiveJobs(r.Context())
	if err != nil {
		api.respondWithJSON(w, http.StatusInternalServerError, APIError{
			Error:   "Failed to cancel active jobs",
			Code:    ErrCodeInternalError,
			Details: err.Error(),
		})
		return
	}

	api.respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"success":             true,
		"running_matched":     result.RunningMatched,
		"running_cancelled":   result.RunningCancelled,
		"queued_cancelled":    result.QueuedCancelled,
		"failed_job_ids":      result.FailedJobIDs,
		"partial_failures":    len(result.FailedJobIDs),
		"event_type":          EventCancelled,
		"terminal_job_status": JobStatusCancelled,
	})
}
