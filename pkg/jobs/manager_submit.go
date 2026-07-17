package jobs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/alhasaniq/consortium/pkg/workflow/compiler"
	"github.com/alhasaniq/consortium/pkg/workflow/runtime"
	"github.com/google/uuid"
)

// SubmitWorkflowRequest contains parameters for workflow submission with deduplication
type SubmitWorkflowRequest struct {
	Workflow       *workflow.Workflow
	IdempotencyKey string // Client-provided key to prevent duplicates
	ForceNewRun    bool   // Bypass all dedup mechanisms
	// DisableRequestHashDedup bypasses implicit content-hash dedup while
	// preserving explicit IdempotencyKey deduplication.
	DisableRequestHashDedup bool
	UserID                  string // For user-scoped dedup (empty = system-wide)
	ParentExecutionID       string // Parent job ID for parent/child observability
	AdmissionBypass         bool   // Admin/operator-only bypass while admission is paused (root submissions)
	Replay                  *runtime.ReplayRequest
}

// SubmitWorkflowResponse is returned when a workflow is submitted
type SubmitWorkflowResponse struct {
	JobID             string      `json:"job_id"`
	WorkflowID        string      `json:"workflow_id"`
	Duplicate         bool        `json:"duplicate"`                     // True if returning existing job
	Status            string      `json:"status"`                        // Current job status
	DedupReason       DedupReason `json:"dedup_reason,omitempty"`        // Why this was deduplicated
	DedupSourceJobID  string      `json:"dedup_source_job_id,omitempty"` // ID of the dedup source job
	DedupSourceStatus string      `json:"dedup_source_status,omitempty"` // Status of the dedup source job
	DedupBypassed     bool        `json:"dedup_bypassed"`                // True if force_new_run was used
}

// computeRequestHash generates a SHA256 hash of the workflow request for deduplication.
// It combines the config hash (semantic nodes, graph ordering, limits, and execution controls)
// with context variables and workflow name to produce a complete execution-shape hash.
// It also returns the configHash to avoid redundant recomputation by the caller.
func computeRequestHash(wf *workflow.Workflow) (requestHash, configHash string) {
	configHash = workflow.ComputeConfigHash(wf)
	hashData := struct {
		ConfigHash string                 `json:"config_hash"`
		Context    map[string]interface{} `json:"context"`
		Name       string                 `json:"name"`
	}{
		ConfigHash: configHash,
		Context:    wf.Context,
		Name:       wf.Name,
	}

	data, err := json.Marshal(hashData)
	if err != nil {
		log.Printf("WARNING: computeRequestHash marshal failed (dedup disabled): %v", err)
		return "", configHash
	}

	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), configHash
}

// freezeWorkflowForSubmit converts a workflow into a frozen snapshot for durable execution.
func freezeWorkflowForSubmit(wf *workflow.Workflow) (*runtime.FrozenSnapshot, error) {
	return runtime.FreezeWorkflowDefinition(wf)
}

// SubmitWorkflow submits a workflow for execution with idempotency and deduplication.
// Dedup eligibility: only pending, running, paused, or completed jobs qualify as cache hits.
// Failed, cancelled, and archived jobs are never returned as dedup hits.
func (m *Manager) SubmitWorkflow(ctx context.Context, req *SubmitWorkflowRequest) (*SubmitWorkflowResponse, error) {
	wf := req.Workflow
	replayRequested := req.Replay != nil && req.Replay.Mode != runtime.ReplayModeOff

	// Ensure workflow has an ID
	if wf.ID == "" {
		wf.ID = "ad-hoc-" + uuid.New().String()
	}

	compiledWorkflow, _, err := compiler.Compile(ctx, wf, workflowDefinitionResolver{store: m.storage})
	if err != nil {
		return nil, NewWorkflowSubmitValidationError("compile workflow references", err)
	}
	wf = compiledWorkflow

	// Enforce DAG validation for every submission path (API, benchmarks, optimize,
	// and internal child workflows). This prevents runtime-only config failures.
	if err := validateWorkflowForSubmit(wf, m.registry); err != nil {
		return nil, err
	}

	// Admission pause gates root submissions. Child submissions remain allowed
	// so already-admitted parent workflows can continue making progress.
	// AdmissionBypass is an explicit operator escape hatch for single-job probes.
	if paused, reason := m.AdmissionState(); paused {
		if req.ParentExecutionID == "" && !req.AdmissionBypass {
			if reason == nil {
				reason = &AdmissionPauseReason{
					Code:        "ADMISSION_PAUSED",
					Reason:      "admission_paused",
					Message:     "Admission is paused",
					TriggeredAt: time.Now(),
				}
			}
			return nil, &AdmissionPausedError{Reason: *reason}
		}
	}

	// Compute request hash and config hash once — both used for dedup check and job creation.
	requestHash, configHash := computeRequestHash(wf)

	// If force_new_run, skip all dedup checks
	if !req.ForceNewRun && !replayRequested {
		// 1. Check idempotency key first (exact duplicate detection)
		if req.IdempotencyKey != "" {
			existingJob, err := m.storage.GetEligibleExecutionByIdempotencyKey(req.IdempotencyKey, req.UserID)
			if err != nil {
				return nil, fmt.Errorf("failed to check idempotency key: %w", err)
			}
			if existingJob != nil {
				log.Printf("🔄 [Dedup] Duplicate detected - reason: idempotency_key, job_id: %s, key: %s, status: %s",
					existingJob.ID, req.IdempotencyKey, existingJob.Status)
				return &SubmitWorkflowResponse{
					JobID:             existingJob.ID,
					WorkflowID:        existingJob.WorkflowID,
					Duplicate:         true,
					Status:            existingJob.Status,
					DedupReason:       DedupReasonIdempotencyKey,
					DedupSourceJobID:  existingJob.ID,
					DedupSourceStatus: existingJob.Status,
				}, nil
			}
		}

		// 2. Check request hash for deduplication window (prevents rapid duplicate submissions)
		if requestHash != "" && !req.DisableRequestHashDedup {
			recentJob, err := m.storage.FindRecentEligibleExecutionByRequestHash(requestHash, DeduplicationWindowSeconds, req.UserID)
			if err != nil {
				log.Printf("Warning: Failed to check request hash: %v", err)
				// Continue - dedup check failure shouldn't block submission
			} else if recentJob != nil {
				log.Printf("🔄 [Dedup] Duplicate detected - reason: request_hash, job_id: %s, hash: %s..., window: %ds, status: %s",
					recentJob.ID, requestHash[:12], DeduplicationWindowSeconds, recentJob.Status)
				return &SubmitWorkflowResponse{
					JobID:             recentJob.ID,
					WorkflowID:        recentJob.WorkflowID,
					Duplicate:         true,
					Status:            recentJob.Status,
					DedupReason:       DedupReasonRequestHash,
					DedupSourceJobID:  recentJob.ID,
					DedupSourceStatus: recentJob.Status,
				}, nil
			}
		}
	} else {
		log.Printf("🔓 [Dedup] Bypassed - force_new_run=%v replay=%v", req.ForceNewRun, replayRequested)
	}

	// Enforce admission/backpressure at submit time.
	// Child workflow submissions bypass admission — the parent already holds a slot,
	// and blocking children would cause worker starvation deadlock.
	if req.ParentExecutionID == "" {
		activeBacklog, err := m.storage.CountDurableActiveJobs(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to check active durable backlog: %w", err)
		}
		if activeBacklog >= m.config.MaxConcurrentWorkflows {
			return nil, ErrPoolExhausted
		}
	}

	// 3. No duplicate found (or bypassed) - create new job
	jobID := uuid.New().String()

	// When force_new_run is set, don't carry the idempotency key to avoid collisions
	idempotencyKey := req.IdempotencyKey
	if req.ForceNewRun || replayRequested {
		idempotencyKey = ""
	}

	// Freeze workflow snapshot/hash for durable execution.
	snapshot, err := freezeWorkflowForSubmit(wf)
	if err != nil {
		return nil, fmt.Errorf("failed to freeze workflow: %w", err)
	}

	// Replay submissions are created as paused, then activated after replay
	// payload persistence to avoid workers claiming the job before replay data exists.
	initialStatus := events.JobStatusPending
	if replayRequested {
		initialStatus = events.JobStatusPaused
	}

	job := &storage.Job{
		ID:             jobID,
		Description:    fmt.Sprintf("Workflow: %s", wf.Name),
		Model:          "workflow",
		Status:         initialStatus,
		WorkflowID:     wf.ID,
		IdempotencyKey: idempotencyKey,
		RequestHash:    requestHash,
		ConfigHash:     configHash,
		UserID:         req.UserID,
		// Initialize durable runtime identity fields.
		WorkflowExecutionID: jobID,
		RunID:               jobID,
		RunNumber:           1,
		DAGSnapshot:         string(snapshot.Definition),
		DAGHash:             snapshot.DAGHash,
		ParentExecutionID:   req.ParentExecutionID,
	}

	if reqData, err := json.Marshal(wf); err == nil {
		job.RequestData = string(reqData)
	}

	// Use atomic create to handle concurrent same-key submissions
	created, existing, err := m.storage.CreateExecutionAtomic(job)
	if err != nil {
		return nil, fmt.Errorf("failed to create job record: %w", err)
	}

	if !created && existing != nil {
		// Race condition: another request created a job with the same idempotency key
		log.Printf("🔄 [Dedup] Race-safe duplicate detected - job_id: %s, key: %s, status: %s",
			existing.ID, req.IdempotencyKey, existing.Status)
		return &SubmitWorkflowResponse{
			JobID:             existing.ID,
			WorkflowID:        existing.WorkflowID,
			Duplicate:         true,
			Status:            existing.Status,
			DedupReason:       DedupReasonIdempotencyKey,
			DedupSourceJobID:  existing.ID,
			DedupSourceStatus: existing.Status,
		}, nil
	}

	if !created && existing == nil {
		// Collision with non-eligible job (failed/cancelled) with same idempotency key.
		// Retry without idempotency key to allow a fresh job.
		job.IdempotencyKey = ""
		job.ID = uuid.New().String()
		job.WorkflowExecutionID = job.ID
		job.RunID = job.ID
		if err := m.storage.CreateExecution(job); err != nil {
			return nil, fmt.Errorf("failed to create job record (retry without key): %w", err)
		}
		jobID = job.ID
	}

	if replayRequested {
		replayJSON, err := json.Marshal(req.Replay)
		if err != nil {
			_ = m.storage.CompleteExecution(jobID, events.JobStatusFailed, "", 0, 0, 0, 0, fmt.Sprintf("marshal replay request: %v", err))
			return nil, fmt.Errorf("marshal replay request for job %s: %w", jobID, err)
		}
		if err := m.storage.UpsertExecutionReplayRequest(ctx, jobID, string(replayJSON)); err != nil {
			_ = m.storage.CompleteExecution(jobID, events.JobStatusFailed, "", 0, 0, 0, 0, fmt.Sprintf("persist replay request: %v", err))
			return nil, fmt.Errorf("persist replay request for job %s: %w", jobID, err)
		}
		if err := m.storage.UpdateExecutionStatus(jobID, events.JobStatusPending); err != nil {
			_ = m.storage.CompleteExecution(jobID, events.JobStatusFailed, "", 0, 0, 0, 0, fmt.Sprintf("activate replay submission: %v", err))
			return nil, fmt.Errorf("activate replay submission for job %s: %w", jobID, err)
		}
	}

	log.Printf("✅ [Submit] Created new job %s for workflow %s (idempotency_key=%s, force_new_run=%v, dag_hash=%s)",
		jobID, wf.ID, req.IdempotencyKey, req.ForceNewRun, snapshot.DAGHash[:12])

	return &SubmitWorkflowResponse{
		JobID:         jobID,
		WorkflowID:    wf.ID,
		Duplicate:     false,
		Status:        events.JobStatusPending,
		DedupBypassed: req.ForceNewRun,
	}, nil
}

func validateWorkflowForSubmit(wf *workflow.Workflow, registry *providers.Registry) error {
	validator := workflow.NewValidator(registry)
	result := validator.Validate(wf)
	if result.Valid {
		return nil
	}

	const maxErrors = 3
	parts := make([]string, 0, min(maxErrors, len(result.Errors)))
	for i, validationErr := range result.Errors {
		if i >= maxErrors {
			break
		}
		prefix := validationErr.Field
		if strings.TrimSpace(validationErr.NodeID) != "" {
			prefix = fmt.Sprintf("%s[%s]", validationErr.Field, validationErr.NodeID)
		}
		parts = append(parts, fmt.Sprintf("%s: %s", prefix, validationErr.Message))
	}
	if len(result.Errors) > maxErrors {
		parts = append(parts, fmt.Sprintf("... and %d more validation error(s)", len(result.Errors)-maxErrors))
	}
	return NewWorkflowSubmitValidationError("workflow validation failed", fmt.Errorf("%s", strings.Join(parts, "; ")))
}
