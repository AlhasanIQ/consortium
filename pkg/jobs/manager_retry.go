package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/workflow"
)

// RetryJobOptions controls retry behavior for an existing job.
type RetryJobOptions struct {
	// AdmissionBypass allows an explicit one-off retry while admission is paused.
	AdmissionBypass bool
}

// RetryJob submits a fresh run for an existing terminal job using its stored
// request_data snapshot. A new job ID is always created (force_new_run=true).
func (m *Manager) RetryJob(ctx context.Context, jobID string, opts RetryJobOptions) (*SubmitWorkflowResponse, error) {
	trimmedID := strings.TrimSpace(jobID)
	if trimmedID == "" {
		return nil, fmt.Errorf("job id is required")
	}

	job, err := m.storage.GetExecution(trimmedID)
	if err != nil {
		return nil, fmt.Errorf("job %s not found: %w", trimmedID, err)
	}
	if job.Status != events.JobStatusFailed && job.Status != events.JobStatusCancelled {
		return nil, fmt.Errorf("cannot retry job %s: status is %s (must be 'failed' or 'cancelled'): %w", trimmedID, job.Status, ErrJobStateConflict)
	}

	requestData := strings.TrimSpace(job.RequestData)
	if requestData == "" {
		return nil, fmt.Errorf("cannot retry job %s: missing request_data snapshot", trimmedID)
	}

	var wf workflow.Workflow
	if err := json.Unmarshal([]byte(requestData), &wf); err != nil {
		return nil, fmt.Errorf("cannot retry job %s: parse request_data: %w", trimmedID, err)
	}
	if strings.TrimSpace(wf.ID) == "" {
		wf.ID = strings.TrimSpace(job.WorkflowID)
	}

	resp, err := m.SubmitWorkflow(ctx, &SubmitWorkflowRequest{
		Workflow:          &wf,
		ForceNewRun:       true,
		UserID:            strings.TrimSpace(job.UserID),
		ParentExecutionID: strings.TrimSpace(job.ParentExecutionID),
		AdmissionBypass:   opts.AdmissionBypass,
	})
	if err != nil {
		return nil, fmt.Errorf("retry job %s: %w", trimmedID, err)
	}
	return resp, nil
}
