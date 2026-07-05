package jobs

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/alhasaniq/consortium/pkg/events"
)

// waitForCompletion polls the DB until the job reaches a terminal state,
// then builds a WorkflowExecutionResult from the persisted data.
func (m *Manager) waitForCompletion(ctx context.Context, jobID, workflowID string) (*WorkflowExecutionResult, error) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	terminalCh, unregister := m.registerCompletionWaiter(jobID)
	defer unregister()

	for {
		job, err := m.storage.GetExecution(jobID)
		if err != nil {
			return nil, fmt.Errorf("poll job %s: %w", jobID, err)
		}

		switch job.Status {
		case events.JobStatusCompleted, events.JobStatusFailed, events.JobStatusCancelled:
			return m.buildResultFromDB(job, workflowID)
		}
		// Still pending or running — keep polling.
		select {
		case <-ctx.Done():
			// Cancellation of the submitted durable job is best-effort and may race
			// with worker claim/registration. Run it asynchronously so ExecuteWorkflow
			// returns promptly with context cancellation.
			go m.cancelJobOnCallerAbort(jobID)
			return nil, ctx.Err()
		case <-terminalCh:
			terminalCh = nil
		case <-ticker.C:
		}
	}
}

// WaitForCompletion polls the persisted job until it reaches a terminal state,
// then returns the same execution result used by synchronous workflow execution.
func (m *Manager) WaitForCompletion(ctx context.Context, jobID, workflowID string) (*WorkflowExecutionResult, error) {
	return m.waitForCompletion(ctx, jobID, workflowID)
}

func (m *Manager) registerCompletionWaiter(jobID string) (<-chan struct{}, func()) {
	jobID = strings.TrimSpace(jobID)
	ch := make(chan struct{})
	if jobID == "" {
		close(ch)
		return ch, func() {}
	}

	m.completionMu.Lock()
	if m.completionWaiters == nil {
		m.completionWaiters = make(map[string][]chan struct{})
	}
	m.completionWaiters[jobID] = append(m.completionWaiters[jobID], ch)
	m.completionMu.Unlock()

	var once sync.Once
	unregister := func() {
		once.Do(func() {
			m.completionMu.Lock()
			defer m.completionMu.Unlock()
			waiters := m.completionWaiters[jobID]
			for i, waiter := range waiters {
				if waiter == ch {
					waiters = append(waiters[:i], waiters[i+1:]...)
					break
				}
			}
			if len(waiters) == 0 {
				delete(m.completionWaiters, jobID)
			} else {
				m.completionWaiters[jobID] = waiters
			}
		})
	}
	return ch, unregister
}

func (m *Manager) notifyCompletionWaiters(jobID string) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return
	}

	m.completionMu.Lock()
	waiters := m.completionWaiters[jobID]
	delete(m.completionWaiters, jobID)
	m.completionMu.Unlock()

	for _, waiter := range waiters {
		close(waiter)
	}
}

func (m *Manager) cancelJobOnCallerAbort(jobID string) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return
	}

	// Small retry window to bridge claim-time races:
	// job can be marked running in DB before runningJobs registration is visible.
	deadline := time.Now().Add(1500 * time.Millisecond)
	for {
		job, err := m.storage.GetExecution(jobID)
		if err != nil {
			log.Printf("⚠️ [ExecuteWorkflow] caller aborted but job lookup failed for %s: %v", jobID, err)
			return
		}
		if events.IsTerminalStatus(job.Status) {
			return
		}

		if err := m.CancelJob(jobID); err == nil {
			return
		} else {
			// Ignore races where job reached a terminal state between lookup and cancel.
			refreshed, refreshErr := m.storage.GetExecution(jobID)
			if refreshErr == nil && refreshed != nil && events.IsTerminalStatus(refreshed.Status) {
				return
			}

			// Pending/paused/running transitions can briefly race with worker claim.
			if !isCallerAbortCancelableStatus(job.Status) || time.Now().After(deadline) {
				log.Printf("⚠️ [ExecuteWorkflow] caller aborted but job cancel failed for %s: %v", jobID, err)
				return
			}
		}

		time.Sleep(20 * time.Millisecond)
	}
}

func isCallerAbortCancelableStatus(status string) bool {
	switch status {
	case events.JobStatusPending, events.JobStatusPaused, events.JobStatusRunning:
		return true
	default:
		return false
	}
}
