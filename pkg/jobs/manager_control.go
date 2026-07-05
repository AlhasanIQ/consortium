package jobs

import (
	"context"
	"fmt"

	"github.com/alhasaniq/consortium/pkg/events"
)

const bulkCancelMessage = "Job cancelled by bulk user request"

// CancelAllResult reports how many jobs were affected by a bulk cancel request.
type CancelAllResult struct {
	RunningMatched   int      `json:"running_matched"`
	RunningCancelled int      `json:"running_cancelled"`
	QueuedCancelled  int      `json:"queued_cancelled"`
	FailedJobIDs     []string `json:"failed_job_ids,omitempty"`
}

// PauseAllPendingJobs pauses every pending job so workers stop claiming new work.
// Running jobs are intentionally left untouched.
func (m *Manager) PauseAllPendingJobs(ctx context.Context) (int, error) {
	pausedBefore, _ := m.AdmissionState()
	newlyPaused := false
	if !pausedBefore {
		newlyPaused = m.pauseAdmission(m.manualPauseReason("Admission paused by operator request"))
	}
	updated, err := m.storage.TransitionExecutionStatuses(
		ctx,
		[]string{events.JobStatusPending},
		events.JobStatusPaused,
		nil,
	)
	if err != nil {
		if newlyPaused {
			m.clearAdmissionPause()
		}
		return 0, fmt.Errorf("pause all pending jobs: %w", err)
	}
	return updated, nil
}

// ResumeAllPausedJobs resumes every paused job by returning it to pending.
func (m *Manager) ResumeAllPausedJobs(ctx context.Context) (int, error) {
	updated, err := m.storage.TransitionExecutionStatuses(
		ctx,
		[]string{events.JobStatusPaused},
		events.JobStatusPending,
		nil,
	)
	if err != nil {
		return 0, fmt.Errorf("resume all paused jobs: %w", err)
	}
	return updated, nil
}

// ResumeJob resumes a single paused job by transitioning it back to pending.
func (m *Manager) ResumeJob(jobID string) error {
	job, err := m.storage.GetExecution(jobID)
	if err != nil {
		return fmt.Errorf("job %s not found: %w", jobID, err)
	}
	if job.Status != events.JobStatusPaused {
		return fmt.Errorf("cannot resume job %s: status is %s (must be 'paused'): %w", jobID, job.Status, ErrJobStateConflict)
	}
	job.Status = events.JobStatusPending
	if err := m.storage.UpdateExecution(job); err != nil {
		return fmt.Errorf("failed to resume job %s: %w", jobID, err)
	}
	return nil
}

// CancelAllActiveJobs cancels all running jobs and marks queued jobs as cancelled.
// Queued includes both pending and paused jobs.
func (m *Manager) CancelAllActiveJobs(ctx context.Context) (*CancelAllResult, error) {
	result := &CancelAllResult{}

	runningIDs, err := m.storage.ListExecutionIDsByStatuses(ctx, []string{events.JobStatusRunning})
	if err != nil {
		return nil, fmt.Errorf("list running jobs for cancel-all: %w", err)
	}
	result.RunningMatched = len(runningIDs)

	for _, jobID := range runningIDs {
		if err := m.CancelJob(jobID); err != nil {
			// If the job changed status after listing, treat it as a race instead of a hard failure.
			job, lookupErr := m.storage.GetExecution(jobID)
			if lookupErr == nil && job.Status != events.JobStatusRunning {
				continue
			}
			result.FailedJobIDs = append(result.FailedJobIDs, jobID)
			continue
		}
		result.RunningCancelled++
	}

	result.QueuedCancelled, err = m.storage.TransitionExecutionStatuses(
		ctx,
		[]string{events.JobStatusPending, events.JobStatusPaused},
		events.JobStatusCancelled,
		strPtr(bulkCancelMessage),
	)
	if err != nil {
		return nil, fmt.Errorf("cancel queued jobs: %w", err)
	}

	return result, nil
}

func strPtr(v string) *string {
	return &v
}
