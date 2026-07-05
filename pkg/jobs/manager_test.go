package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	workflowruntime "github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

func TestManager(t *testing.T) {
	t.Run("execute workflow creates job record", func(t *testing.T) {
		manager, store := setupManagerTest(t)
		startWorkers(t, manager)

		wf := simpleWorkflowWithID("wf-123", "Test Workflow", "mock-model", "Say hello")

		result, err := manager.ExecuteWorkflow(context.Background(), wf)
		if err != nil {
			t.Fatalf("execution failed: %v", err)
		}

		if !result.Success {
			t.Errorf("expected success, got failure")
		}

		// Verify job was created and updated
		job, err := store.GetExecution(result.JobID)
		if err != nil {
			t.Fatalf("job not found: %v", err)
		}

		if job.Status != "completed" {
			t.Errorf("expected completed status, got %s", job.Status)
		}
		if job.WorkflowID != "wf-123" {
			t.Errorf("expected workflow ID wf-123, got %s", job.WorkflowID)
		}
	})

	t.Run("execute workflow without ID generates one", func(t *testing.T) {
		manager, _ := setupManagerTest(t)
		startWorkers(t, manager)

		wf := simpleWorkflow("No ID Workflow", "mock-model", "Test")

		result, err := manager.ExecuteWorkflow(context.Background(), wf)
		if err != nil {
			t.Fatalf("execution failed: %v", err)
		}

		if result.WorkflowID == "" {
			t.Error("expected workflow ID to be generated")
		}
		if !strings.HasPrefix(result.WorkflowID, "ad-hoc-") {
			t.Errorf("expected ad-hoc prefix, got %s", result.WorkflowID)
		}
	})

	t.Run("concurrent workflow executions are isolated", func(t *testing.T) {
		manager, store := setupManagerTest(t)
		startWorkers(t, manager)

		wf1 := simpleWorkflow("First", "mock-model", "Test")
		wf2 := simpleWorkflow("Second", "mock-model", "Test")

		done := make(chan *WorkflowExecutionResult, 2)

		go func() {
			result, _ := manager.ExecuteWorkflow(context.Background(), wf1)
			done <- result
		}()

		go func() {
			result, _ := manager.ExecuteWorkflow(context.Background(), wf2)
			done <- result
		}()

		// Collect results
		results := make([]*WorkflowExecutionResult, 2)
		for i := 0; i < 2; i++ {
			select {
			case r := <-done:
				results[i] = r
			case <-time.After(5 * time.Second):
				t.Fatal("timeout waiting for executions")
			}
		}

		// Verify both jobs exist and are independent
		job1, err := store.GetExecution(results[0].JobID)
		if err != nil {
			t.Fatalf("job1 not found: %v", err)
		}

		job2, err := store.GetExecution(results[1].JobID)
		if err != nil {
			t.Fatalf("job2 not found: %v", err)
		}

		if job1.ID == job2.ID {
			t.Error("expected different job IDs")
		}
	})

	t.Run("workflow execution failure updates job status", func(t *testing.T) {
		manager, store := setupManagerWithProvider(t,
			newFailingMockProvider("fail", "fail-model", fmt.Errorf("mock error")))
		startWorkers(t, manager)

		temp := 0.0
		wf := &workflow.Workflow{
			Name: "Failing Workflow",
			Nodes: []*workflow.Node{
				{Type: workflow.NodeTypePrompt, Model: "fail-model", Prompt: "test",
					Temperature: &temp, MaxTokens: 256, TimeoutSeconds: 30,
					RetryPolicy: &workflow.RetryPolicy{MaxAttempts: 1}},
			},
		}

		result, err := manager.ExecuteWorkflow(context.Background(), wf)
		if err != nil {
			t.Fatalf("ExecuteWorkflow should not return error: %v", err)
		}

		if result.Success {
			t.Error("expected workflow to fail")
		}

		// Verify job was marked as failed
		job, err := store.GetExecution(result.JobID)
		if err != nil {
			t.Fatalf("job not found: %v", err)
		}

		if job.Status != "failed" {
			t.Errorf("expected failed status, got %s", job.Status)
		}
	})

	t.Run("execution with context cancellation cancels submitted job", func(t *testing.T) {
		manager, store := setupManagerWithProvider(t,
			newBlockingMockProvider("slow", "slow-model"))
		startWorkers(t, manager)

		wf := simpleWorkflow("Cancelled Workflow", "slow-model", "test")

		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan error, 1)
		go func() {
			_, err := manager.ExecuteWorkflow(ctx, wf)
			done <- err
		}()

		var jobID string
		deadline := time.Now().Add(1500 * time.Millisecond)
		for time.Now().Before(deadline) {
			jobs, err := store.ListExecutions(5)
			if err == nil && len(jobs) > 0 {
				jobID = jobs[0].ID
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if jobID == "" {
			t.Fatal("expected job submission before cancellation")
		}

		cancel()

		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("expected context canceled error, got %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for ExecuteWorkflow cancellation")
		}

		pollDeadline := time.Now().Add(1500 * time.Millisecond)
		for time.Now().Before(pollDeadline) {
			job, err := store.GetExecution(jobID)
			if err != nil {
				t.Fatalf("failed to get cancelled job %s: %v", jobID, err)
			}
			if job.Status == events.JobStatusCancelled {
				return
			}
			if events.IsTerminalStatus(job.Status) && job.Status != events.JobStatusCancelled {
				t.Fatalf("expected cancelled status after caller abort, got %s", job.Status)
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("job %s was not cancelled before timeout", jobID)
	})

	t.Run("CancelJob scenarios", func(t *testing.T) {
		tests := []struct {
			name        string
			setup       func(t *testing.T, manager *Manager, store *storage.Storage) string // returns jobID
			expectErr   bool
			errContains string
			wantStatus  string
		}{
			{
				name: "cancels running workflow",
				setup: func(t *testing.T, _ *Manager, _ *storage.Storage) string {
					t.Helper()
					// This test needs its own slow provider, so it's handled specially below.
					return ""
				},
			},
			{
				name: "returns error for non-existent job",
				setup: func(t *testing.T, _ *Manager, _ *storage.Storage) string {
					t.Helper()
					return "non-existent-job-id"
				},
				expectErr:   true,
				errContains: "not found",
			},
			{
				name: "returns error for completed job",
				setup: func(t *testing.T, manager *Manager, store *storage.Storage) string {
					t.Helper()
					startWorkers(t, manager)
					wf := simpleWorkflow("Completed Workflow", "mock-model", "test")
					result, err := manager.ExecuteWorkflow(context.Background(), wf)
					if err != nil {
						t.Fatalf("execution failed: %v", err)
					}
					job, err := store.GetExecution(result.JobID)
					if err != nil {
						t.Fatalf("job not found: %v", err)
					}
					if job.Status != "completed" {
						t.Fatalf("expected completed status, got %s", job.Status)
					}
					return result.JobID
				},
				expectErr:   true,
				errContains: "running",
			},
			{
				name: "cancels pending workflow",
				setup: func(t *testing.T, manager *Manager, _ *storage.Storage) string {
					t.Helper()
					wf := simpleWorkflowWithID("wf-pending-cancel", "Pending Cancel", "mock-model", "hello")
					wf.Nodes[0].ID = "n1"
					resp, err := manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
						Workflow:    wf,
						ForceNewRun: true,
					})
					if err != nil {
						t.Fatalf("SubmitWorkflow failed: %v", err)
					}
					return resp.JobID
				},
				wantStatus: events.JobStatusCancelled,
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				// Special case: "cancels running workflow" needs its own slow provider
				if tc.name == "cancels running workflow" {
					manager, store := setupManagerWithProvider(t,
						newBlockingMockProvider("slow", "slow-model"))
					startWorkers(t, manager)

					wf := simpleWorkflow("Slow Workflow", "slow-model", "test")

					execCtx, execCancel := context.WithCancel(context.Background())
					defer execCancel()
					done := make(chan error, 1)
					go func() {
						_, err := manager.ExecuteWorkflow(execCtx, wf)
						done <- err
					}()

					// Poll until worker picks up and starts the job
					var jobID string
					deadline := time.Now().Add(2 * time.Second)
					for time.Now().Before(deadline) {
						jobsList, err := store.ListExecutions(10)
						if err == nil && len(jobsList) > 0 && jobsList[0].Status == "running" {
							candidateID := jobsList[0].ID
							if manager.IsRunning(candidateID) {
								jobID = candidateID
								break
							}
						}
						time.Sleep(10 * time.Millisecond)
					}
					if jobID == "" {
						t.Fatal("timed out waiting for job to reach running state and be tracked")
					}

					if err := manager.CancelJob(jobID); err != nil {
						t.Fatalf("CancelJob failed: %v", err)
					}

					// Ensure ExecuteWorkflow unblocks promptly even if durable
					// cancellation finalization is still in progress.
					execCancel()

					finalStatus := ""
					finalError := ""
					pollDeadline := time.Now().Add(3 * time.Second)
					for time.Now().Before(pollDeadline) {
						job, err := store.GetExecution(jobID)
						if err != nil {
							t.Fatalf("job not found: %v", err)
						}
						if job.Status == events.JobStatusCancelled || job.Status == events.JobStatusFailed {
							finalStatus = job.Status
							finalError = job.ErrorMessage
							break
						}
						time.Sleep(10 * time.Millisecond)
					}
					if finalStatus == "" {
						t.Fatal("expected terminal status in storage after cancellation")
					}
					if finalStatus == events.JobStatusFailed {
						lowerErr := strings.ToLower(finalError)
						if !strings.Contains(lowerErr, "cancel") && !strings.Contains(lowerErr, "context canceled") {
							t.Fatalf("expected cancellation-related failure, got status=%s error=%q", finalStatus, finalError)
						}
					}

					select {
					case err := <-done:
						if err != nil && !errors.Is(err, context.Canceled) {
							t.Fatalf("expected nil or context canceled from ExecuteWorkflow, got %v", err)
						}
					case <-time.After(2 * time.Second):
						t.Fatal("timeout waiting for ExecuteWorkflow to exit")
					}
					return
				}

				// Standard table-driven path
				manager, store := setupManagerTest(t)
				jobID := tc.setup(t, manager, store)

				err := manager.CancelJob(jobID)
				if tc.expectErr {
					if err == nil {
						t.Error("expected error")
					} else if !strings.Contains(err.Error(), tc.errContains) {
						t.Errorf("expected error containing %q, got: %v", tc.errContains, err)
					}
					return
				}

				if err != nil {
					t.Fatalf("CancelJob failed: %v", err)
				}

				if tc.wantStatus != "" {
					job, err := store.GetExecution(jobID)
					if err != nil {
						t.Fatalf("GetJob failed: %v", err)
					}
					if job.Status != tc.wantStatus {
						t.Fatalf("expected %s status, got %s", tc.wantStatus, job.Status)
					}
				}
			})
		}
	})

	t.Run("bulk pause/resume/cancel controls", func(t *testing.T) {
		manager, store := setupManagerTest(t)
		createJob := func(id, status string) {
			t.Helper()
			if err := store.CreateExecution(&storage.Job{
				ID:          id,
				Description: "bulk control test",
				Model:       "workflow",
				Status:      status,
			}); err != nil {
				t.Fatalf("failed to create job %s: %v", id, err)
			}
		}

		createJob("bulk-pending-1", events.JobStatusPending)
		createJob("bulk-pending-2", events.JobStatusPending)
		createJob("bulk-running-1", events.JobStatusRunning)

		paused, err := manager.PauseAllPendingJobs(context.Background())
		if err != nil {
			t.Fatalf("PauseAllPendingJobs failed: %v", err)
		}
		if paused != 2 {
			t.Fatalf("expected 2 paused jobs, got %d", paused)
		}
		if admissionPaused, reason := manager.AdmissionState(); !admissionPaused || reason == nil || reason.Reason != "manual_pause" {
			t.Fatalf("expected manual admission pause state, got paused=%v reason=%+v", admissionPaused, reason)
		}

		for _, id := range []string{"bulk-pending-1", "bulk-pending-2"} {
			job, err := store.GetExecution(id)
			if err != nil {
				t.Fatalf("GetJob(%s) failed: %v", id, err)
			}
			if job.Status != events.JobStatusPaused {
				t.Fatalf("expected %s to be paused, got %s", id, job.Status)
			}
		}

		resumed, err := manager.ResumeAllPausedJobs(context.Background())
		if err != nil {
			t.Fatalf("ResumeAllPausedJobs failed: %v", err)
		}
		if resumed != 2 {
			t.Fatalf("expected 2 resumed jobs, got %d", resumed)
		}
		if admissionPaused, reason := manager.AdmissionState(); !admissionPaused || reason == nil || reason.Reason != "manual_pause" {
			t.Fatalf("expected manual admission pause to remain after resume-all, got paused=%v reason=%+v", admissionPaused, reason)
		}

		cancelResult, err := manager.CancelAllActiveJobs(context.Background())
		if err != nil {
			t.Fatalf("CancelAllActiveJobs failed: %v", err)
		}
		if cancelResult.QueuedCancelled != 2 {
			t.Fatalf("expected 2 queued cancellations, got %d", cancelResult.QueuedCancelled)
		}
		if cancelResult.RunningMatched != 1 {
			t.Fatalf("expected 1 running match, got %d", cancelResult.RunningMatched)
		}
		if cancelResult.RunningCancelled != 0 {
			t.Fatalf("expected 0 running cancelled (no in-memory runner), got %d", cancelResult.RunningCancelled)
		}
		if len(cancelResult.FailedJobIDs) != 1 || cancelResult.FailedJobIDs[0] != "bulk-running-1" {
			t.Fatalf("expected running job failure to be reported, got %+v", cancelResult.FailedJobIDs)
		}

		for _, id := range []string{"bulk-pending-1", "bulk-pending-2"} {
			job, err := store.GetExecution(id)
			if err != nil {
				t.Fatalf("GetJob(%s) failed: %v", id, err)
			}
			if job.Status != events.JobStatusCancelled {
				t.Fatalf("expected %s to be cancelled, got %s", id, job.Status)
			}
			if strings.TrimSpace(job.ErrorMessage) == "" {
				t.Fatalf("expected cancellation message for %s", id)
			}
		}
	})

	t.Run("auto admission pause pauses pending roots and blocks only new roots", func(t *testing.T) {
		manager, store := setupManagerTest(t)
		rootPending := &storage.Job{
			ID:          "root-pending-auto-pause",
			Description: "root pending",
			Model:       "workflow",
			Status:      events.JobStatusPending,
		}
		if err := store.CreateExecution(rootPending); err != nil {
			t.Fatalf("failed to create root pending job: %v", err)
		}
		childPending := &storage.Job{
			ID:                "child-pending-auto-pause",
			Description:       "child pending",
			Model:             "workflow",
			Status:            events.JobStatusPending,
			ParentExecutionID: "parent-123",
		}
		if err := store.CreateExecution(childPending); err != nil {
			t.Fatalf("failed to create child pending job: %v", err)
		}

		manager.maybePauseAdmissionForFailure("failed-job-1", "AUTH_ERROR", "OpenRouter authentication failed: invalid api key")

		paused, reason := manager.AdmissionState()
		if !paused {
			t.Fatal("expected admission to be paused")
		}
		if reason == nil || reason.Reason != "auth_or_access_denied" || reason.TriggerJobID != "failed-job-1" {
			t.Fatalf("unexpected pause reason: %+v", reason)
		}

		rootAfter, err := store.GetExecution(rootPending.ID)
		if err != nil {
			t.Fatalf("failed to reload root pending job: %v", err)
		}
		if rootAfter.Status != events.JobStatusPaused {
			t.Fatalf("expected root pending job to be paused, got %s", rootAfter.Status)
		}
		childAfter, err := store.GetExecution(childPending.ID)
		if err != nil {
			t.Fatalf("failed to reload child pending job: %v", err)
		}
		if childAfter.Status != events.JobStatusPending {
			t.Fatalf("expected child pending job to remain pending, got %s", childAfter.Status)
		}

		rootWorkflow := simpleWorkflowWithID("wf-admission-paused-root", "Root Blocked", "mock-model", "hello")
		rootWorkflow.Nodes[0].ID = "n1"
		_, err = manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
			Workflow:    rootWorkflow,
			ForceNewRun: true,
		})
		if !IsAdmissionPausedError(err) {
			t.Fatalf("expected admission paused error for root submission, got %v", err)
		}

		childWorkflow := simpleWorkflowWithID("wf-admission-paused-child", "Child Allowed", "mock-model", "hello")
		childWorkflow.Nodes[0].ID = "n1"
		childResp, err := manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
			Workflow:          childWorkflow,
			ForceNewRun:       true,
			ParentExecutionID: "parent-123",
		})
		if err != nil {
			t.Fatalf("expected child submission to bypass admission pause, got %v (resp=%+v)", err, childResp)
		}
		if childResp == nil || childResp.JobID == "" {
			t.Fatalf("expected child response with job id, got %+v", childResp)
		}
	})

	t.Run("manual admission pause still allows child submissions", func(t *testing.T) {
		manager, _ := setupManagerTest(t)
		manager.pauseAdmission(manager.manualPauseReason("operator pause"))

		rootWorkflow := simpleWorkflowWithID("wf-manual-paused-root", "Root Blocked", "mock-model", "hello")
		rootWorkflow.Nodes[0].ID = "n1"
		_, err := manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
			Workflow:    rootWorkflow,
			ForceNewRun: true,
		})
		if !IsAdmissionPausedError(err) {
			t.Fatalf("expected admission paused error for root submission, got %v", err)
		}

		childWorkflow := simpleWorkflowWithID("wf-manual-paused-child", "Child Allowed", "mock-model", "hello")
		childWorkflow.Nodes[0].ID = "n1"
		childResp, err := manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
			Workflow:          childWorkflow,
			ForceNewRun:       true,
			ParentExecutionID: "manual-parent-1",
		})
		if err != nil {
			t.Fatalf("expected child submission to bypass manual admission pause, got %v", err)
		}
		if childResp == nil || childResp.JobID == "" {
			t.Fatalf("expected child submission response with job id, got %+v", childResp)
		}
	})

	t.Run("resume operations do not implicitly reopen admission", func(t *testing.T) {
		manager, store := setupManagerTest(t)
		manager.pauseAdmission(manager.manualPauseReason("operator pause"))

		pausedJob := &storage.Job{
			ID:          "paused-for-resume",
			Description: "paused job",
			Model:       "workflow",
			Status:      events.JobStatusPaused,
		}
		if err := store.CreateExecution(pausedJob); err != nil {
			t.Fatalf("failed to create paused job: %v", err)
		}

		if err := manager.ResumeJob(pausedJob.ID); err != nil {
			t.Fatalf("ResumeJob failed: %v", err)
		}
		if admissionPaused, reason := manager.AdmissionState(); !admissionPaused || reason == nil {
			t.Fatalf("expected admission to remain paused after ResumeJob, got paused=%v reason=%+v", admissionPaused, reason)
		}

		if changed := manager.ResumeAdmission(); !changed {
			t.Fatal("expected ResumeAdmission to clear paused gate")
		}
		if admissionPaused, reason := manager.AdmissionState(); admissionPaused || reason != nil {
			t.Fatalf("expected admission to be accepting after explicit resume, got paused=%v reason=%+v", admissionPaused, reason)
		}
	})

	t.Run("admission bypass allows one-off root probe", func(t *testing.T) {
		manager, _ := setupManagerTest(t)
		manager.pauseAdmission(manager.manualPauseReason("operator pause"))

		wf := simpleWorkflowWithID("wf-probe", "Probe", "mock-model", "hello")
		wf.Nodes[0].ID = "n1"
		if _, err := manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
			Workflow:    wf,
			ForceNewRun: true,
		}); !IsAdmissionPausedError(err) {
			t.Fatalf("expected admission paused error without bypass, got %v", err)
		}

		resp, err := manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
			Workflow:        wf,
			ForceNewRun:     true,
			AdmissionBypass: true,
		})
		if err != nil {
			t.Fatalf("expected submission success with admission bypass, got %v", err)
		}
		if resp == nil || resp.JobID == "" {
			t.Fatalf("expected bypass submission response with job id, got %+v", resp)
		}
	})

	t.Run("RetryJob resubmits with optional admission bypass", func(t *testing.T) {
		manager, store := setupManagerTest(t)

		wf := simpleWorkflowWithID("wf-retry", "Retry Source", "mock-model", "hello")
		wf.Nodes[0].ID = "n1"
		submitResp, err := manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
			Workflow:    wf,
			ForceNewRun: true,
		})
		if err != nil {
			t.Fatalf("initial submit failed: %v", err)
		}

		job, err := store.GetExecution(submitResp.JobID)
		if err != nil {
			t.Fatalf("failed to load submitted job: %v", err)
		}
		job.Status = events.JobStatusFailed
		job.ErrorMessage = "simulated failure"
		if err := store.UpdateExecution(job); err != nil {
			t.Fatalf("failed to mark source job as failed: %v", err)
		}

		manager.pauseAdmission(manager.manualPauseReason("operator pause"))
		if _, err := manager.RetryJob(context.Background(), submitResp.JobID, RetryJobOptions{}); !IsAdmissionPausedError(err) {
			t.Fatalf("expected admission paused error without bypass, got %v", err)
		}

		retryResp, err := manager.RetryJob(context.Background(), submitResp.JobID, RetryJobOptions{
			AdmissionBypass: true,
		})
		if err != nil {
			t.Fatalf("RetryJob with admission bypass failed: %v", err)
		}
		if retryResp == nil || retryResp.JobID == "" {
			t.Fatalf("expected retry response with job id, got %+v", retryResp)
		}
		if retryResp.JobID == submitResp.JobID {
			t.Fatalf("expected retry to create a new job id, got same %s", retryResp.JobID)
		}
	})

	t.Run("SubmitWorkflow returns ErrPoolExhausted when backlog is full", func(t *testing.T) {
		cfg := DefaultManagerConfig()
		cfg.MaxConcurrentWorkflows = 1
		manager, _ := setupManagerWithConfig(t, cfg)

		wf1 := simpleWorkflowWithID("wf-overload-1", "Overload 1", "mock-model", "hello")
		wf1.Nodes[0].ID = "n1"
		if _, err := manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
			Workflow:    wf1,
			ForceNewRun: true,
		}); err != nil {
			t.Fatalf("first SubmitWorkflow failed: %v", err)
		}

		wf2 := simpleWorkflowWithID("wf-overload-2", "Overload 2", "mock-model", "hello again")
		wf2.Nodes[0].ID = "n1"
		_, err := manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
			Workflow:    wf2,
			ForceNewRun: true,
		})
		if !errors.Is(err, ErrPoolExhausted) {
			t.Fatalf("expected ErrPoolExhausted, got %v", err)
		}
	})

	t.Run("SubmitWorkflow bypasses admission for child submissions", func(t *testing.T) {
		cfg := DefaultManagerConfig()
		cfg.MaxConcurrentWorkflows = 1
		manager, _ := setupManagerWithConfig(t, cfg)

		parent := simpleWorkflowWithID("wf-parent-active", "Parent Active", "mock-model", "hello")
		parent.Nodes[0].ID = "n1"
		parentResp, err := manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
			Workflow:    parent,
			ForceNewRun: true,
		})
		if err != nil {
			t.Fatalf("parent submit failed: %v", err)
		}
		if parentResp.Duplicate {
			t.Fatal("expected parent submit to create a fresh job")
		}

		child := simpleWorkflowWithID("wf-child-bypass", "Child Bypass", "mock-model", "hello child")
		child.Nodes[0].ID = "n1"
		childResp, err := manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
			Workflow:          child,
			ForceNewRun:       true,
			ParentExecutionID: parentResp.JobID,
		})
		if err != nil {
			t.Fatalf("child submit should bypass admission, got error: %v", err)
		}
		if childResp.Duplicate {
			t.Fatal("expected child submit to create a fresh job")
		}
		if childResp.JobID == parentResp.JobID {
			t.Fatal("expected child and parent to have different job IDs")
		}
	})

	t.Run("node timeout works correctly", func(t *testing.T) {
		manager, store := setupManagerWithProvider(t,
			newSlowMockProvider("timeout-test", "timeout-model", 2*time.Second))
		startWorkers(t, manager)

		temp := 0.0
		wf := &workflow.Workflow{
			Name: "Timeout Test",
			Nodes: []*workflow.Node{
				{
					Type:           workflow.NodeTypePrompt,
					Model:          "timeout-model",
					Prompt:         "test",
					Temperature:    &temp,
					MaxTokens:      256,
					TimeoutSeconds: 1,
					RetryPolicy:    &workflow.RetryPolicy{MaxAttempts: 1},
				},
			},
		}

		startTime := time.Now()
		result, _ := manager.ExecuteWorkflow(context.Background(), wf)
		duration := time.Since(startTime)

		if duration > 3*time.Second {
			t.Errorf("timeout took too long: %v (expected under 3s)", duration)
		}

		job, err := store.GetExecution(result.JobID)
		if err != nil {
			t.Fatalf("job not found: %v", err)
		}

		if job.Status != "failed" {
			t.Errorf("expected failed status, got %s", job.Status)
		}

		if job.ErrorMessage == "" {
			t.Error("expected error message to be set")
		}
		if !strings.Contains(job.ErrorMessage, "timeout") && !strings.Contains(job.ErrorMessage, "context deadline exceeded") {
			t.Logf("Note: Error message doesn't explicitly mention timeout: %s", job.ErrorMessage)
		}
	})

	t.Run("parallel nodes via edges execute concurrently", func(t *testing.T) {
		mock, calls, mu := newRendezvousMockProvider("mock", "mock-model", 2*time.Second)

		store, err := storage.NewStorage(":memory:")
		if err != nil {
			t.Fatalf("failed to create storage: %v", err)
		}
		registry := providers.NewRegistry()
		registry.Register(mock)

		cfg := DefaultManagerConfig()
		cfg.WorkerCount = 4
		cfg.MaxConcurrentWorkflows = 4
		cfg.WorkerPollInterval = 1 * time.Millisecond
		cfg.MaxParallelNodesPerWF = 2
		manager := NewManagerWithConfig(store, registry, cfg)
		startWorkers(t, manager)

		wf := &workflow.Workflow{
			ID:   "parallel-test",
			Name: "Parallel Test",
			Nodes: []*workflow.Node{
				strictPromptNode("agent1", "mock-model", "Agent 1 task"),
				strictPromptNode("agent2", "mock-model", "Agent 2 task"),
				strictResultNode("result", []string{"agent1", "agent2"}, "final"),
			},
			Edges: []*workflow.Edge{
				{ID: "e1", Source: "agent1", Target: "result"},
				{ID: "e2", Source: "agent2", Target: "result"},
			},
		}

		result, err := manager.ExecuteWorkflow(context.Background(), wf)
		if err != nil {
			t.Fatalf("execution failed: %v", err)
		}
		if !result.Success {
			t.Errorf("expected success, got error: %s", result.Error)
		}

		// Verify events were persisted
		evts, err := store.GetEventsAfter(context.Background(), result.JobID, 0)
		if err != nil {
			t.Fatalf("failed to get events: %v", err)
		}
		nodeStartCount := 0
		nodeCompleteCount := 0
		for _, e := range evts {
			if e.Type == "node_start" {
				nodeStartCount++
			}
			if e.Type == "node_complete" {
				nodeCompleteCount++
			}
		}
		if nodeStartCount < 3 {
			t.Errorf("expected at least 3 node_start events, got %d", nodeStartCount)
		}
		if nodeCompleteCount < 3 {
			t.Errorf("expected at least 3 node_complete events, got %d", nodeCompleteCount)
		}

		// Verify provider saw both prompt calls; rendezvous provider guarantees
		// this only succeeds when both prompt nodes overlap in-flight.
		mu.Lock()
		observed := *calls
		mu.Unlock()
		if observed < 2 {
			t.Fatalf("expected at least 2 prompt calls, got %d", observed)
		}
	})

	t.Run("durable execution writes trace spans via ExecCtx", func(t *testing.T) {
		manager, store := setupManagerTest(t)
		startWorkers(t, manager)

		wf := simpleWorkflowWithID("wf-trace-wiring", "Trace Wiring", "mock-model", "trace me")
		wf.Nodes[0].ID = "n1"

		result, err := manager.ExecuteWorkflow(context.Background(), wf)
		if err != nil {
			t.Fatalf("ExecuteWorkflow failed: %v", err)
		}
		if !result.Success {
			t.Fatalf("expected successful execution, got error: %s", result.Error)
		}

		spans, err := store.GetJobSpans(result.JobID)
		if err != nil {
			t.Fatalf("GetJobSpans failed: %v", err)
		}
		if len(spans) == 0 {
			t.Fatal("expected at least one trace span for durable execution")
		}
	})

	t.Run("MaxParallelNodesPerWF limits fan-out per workflow", func(t *testing.T) {
		mock, maxSeen, mu := newConcurrencyTrackingMockProvider("limited", "limited-model", 60*time.Millisecond)

		store, err := storage.NewStorage(":memory:")
		if err != nil {
			t.Fatalf("failed to create storage: %v", err)
		}
		registry := providers.NewRegistry()
		registry.Register(mock)

		cfg := DefaultManagerConfig()
		cfg.MaxParallelNodesPerWF = 1
		cfg.MaxConcurrentWorkflows = 4
		cfg.WorkerCount = 4
		cfg.WorkerPollInterval = 1 * time.Millisecond
		manager := NewManagerWithConfig(store, registry, cfg)
		startWorkers(t, manager)

		wf := &workflow.Workflow{
			ID:   "wf-parallel-limit",
			Name: "Parallel Limit",
			Nodes: []*workflow.Node{
				strictPromptNode("a", "limited-model", "A"),
				strictPromptNode("b", "limited-model", "B"),
			},
		}

		result, err := manager.ExecuteWorkflow(context.Background(), wf)
		if err != nil {
			t.Fatalf("ExecuteWorkflow failed: %v", err)
		}
		if !result.Success {
			t.Fatalf("expected success, got error: %s", result.Error)
		}

		mu.Lock()
		observed := *maxSeen
		mu.Unlock()
		if observed > 1 {
			t.Fatalf("expected max in-flight node executions <= 1, got %d", observed)
		}
	})
}

func TestCancelJobOnCallerAbortRetriesRunningMapRace(t *testing.T) {
	manager, store := setupManagerWithProvider(t,
		newBlockingMockProvider("slow", "slow-model"))
	startWorkers(t, manager)

	wf := simpleWorkflow("Caller Abort Retry", "slow-model", "test")

	execCtx, execCancel := context.WithCancel(context.Background())
	defer execCancel()

	done := make(chan error, 1)
	go func() {
		_, err := manager.ExecuteWorkflow(execCtx, wf)
		done <- err
	}()

	var jobID string
	var cancelFunc interface{}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		jobs, err := store.ListExecutions(5)
		if err == nil && len(jobs) > 0 {
			candidate := jobs[0].ID
			job, getErr := store.GetExecution(candidate)
			if getErr == nil && job.Status == events.JobStatusRunning {
				if cf, ok := manager.runningJobs.Load(candidate); ok {
					jobID = candidate
					cancelFunc = cf
					break
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if jobID == "" || cancelFunc == nil {
		t.Fatal("timed out waiting for running job with registered cancel function")
	}

	// Simulate the transient race where DB status is running but runningJobs map
	// registration is not yet visible.
	manager.runningJobs.Delete(jobID)
	go func() {
		time.Sleep(80 * time.Millisecond)
		manager.runningJobs.Store(jobID, cancelFunc)
	}()

	manager.cancelJobOnCallerAbort(jobID)

	pollDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(pollDeadline) {
		job, err := store.GetExecution(jobID)
		if err != nil {
			t.Fatalf("failed to load job %s: %v", jobID, err)
		}
		if job.Status == events.JobStatusCancelled {
			break
		}
		if events.IsTerminalStatus(job.Status) && job.Status != events.JobStatusCancelled {
			t.Fatalf("expected cancelled status, got terminal status %s", job.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}

	job, err := store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("failed to load final job %s: %v", jobID, err)
	}
	if job.Status != events.JobStatusCancelled {
		t.Fatalf("expected final status cancelled, got %s", job.Status)
	}

	// Ensure ExecuteWorkflow goroutine exits.
	execCancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for ExecuteWorkflow to return")
	}
}

func TestExecuteWorkflow_FailsWithoutWorkers(t *testing.T) {
	manager, _ := setupManagerTest(t)
	// Intentionally do NOT call startWorkers(t, manager)

	wf := simpleWorkflow("Should Fail Fast", "mock-model", "test")

	_, err := manager.ExecuteWorkflow(context.Background(), wf)
	if err == nil {
		t.Fatal("expected error when workers not started")
	}
	if err != ErrWorkersNotStarted {
		t.Errorf("expected ErrWorkersNotStarted, got: %v", err)
	}

}

func TestExecuteWorkflowDoesNotWaitForCompletionPollInterval(t *testing.T) {
	manager, _ := setupManagerTest(t)
	startWorkers(t, manager)

	wf := &workflow.Workflow{
		ID:   "wf-fast-completion",
		Name: "Fast completion",
		Nodes: []*workflow.Node{
			strictPromptNode("n1", "mock-model", "Hello"),
		},
	}

	started := time.Now()
	result, err := manager.ExecuteWorkflow(context.Background(), wf)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("ExecuteWorkflow failed: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("ExecuteWorkflow result = %+v, want success", result)
	}
	if elapsed >= 45*time.Millisecond {
		t.Fatalf("ExecuteWorkflow took %s, want below completion poll interval", elapsed)
	}
}

// ---------------------------------------------------------------------------
// SubmitWorkflow identity & snapshot tests (table-driven)
// ---------------------------------------------------------------------------

func TestSubmitWorkflow_IdentityAndSnapshot(t *testing.T) {
	tests := []struct {
		name   string
		verify func(t *testing.T, manager *Manager, store *storage.Storage)
	}{
		{
			name: "stores snapshot and identity",
			verify: func(t *testing.T, manager *Manager, store *storage.Storage) {
				t.Helper()
				wf := &workflow.Workflow{
					ID:   "wf-submit-test",
					Name: "Submit Test",
					Nodes: []*workflow.Node{
						strictPromptNode("node1", "mock-model", "Hello"),
						strictPromptNode("node2", "mock-model", "World"),
					},
				}

				resp, err := manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
					Workflow: wf,
				})
				if err != nil {
					t.Fatalf("SubmitWorkflow failed: %v", err)
				}
				if resp.Duplicate {
					t.Fatal("Expected new job, got duplicate")
				}

				job, err := store.GetExecution(resp.JobID)
				if err != nil {
					t.Fatalf("Failed to get job: %v", err)
				}

				if job.WorkflowExecutionID != resp.JobID {
					t.Errorf("Expected workflow_execution_id = job_id, got %s", job.WorkflowExecutionID)
				}
				if job.RunID != resp.JobID {
					t.Errorf("Expected run_id = job_id, got %s", job.RunID)
				}
				if job.RunNumber != 1 {
					t.Errorf("Expected run_number 1, got %d", job.RunNumber)
				}
				if job.DAGHash == "" {
					t.Error("Expected dag_hash to be set")
				}
				if job.DAGSnapshot == "" {
					t.Error("Expected dag_snapshot to be set")
				}
			},
		},
		{
			name: "freezes deterministically",
			verify: func(t *testing.T, manager *Manager, store *storage.Storage) {
				t.Helper()
				makeWF := func() *workflow.Workflow {
					return &workflow.Workflow{
						ID:   "wf-determ",
						Name: "Deterministic Test",
						Nodes: []*workflow.Node{
							strictPromptNode("a", "mock-model", "Node A"),
							strictPromptNode("b", "mock-model", "Node B"),
						},
					}
				}

				resp1, err := manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
					Workflow:    makeWF(),
					ForceNewRun: true,
				})
				if err != nil {
					t.Fatalf("First submit failed: %v", err)
				}

				resp2, err := manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
					Workflow:    makeWF(),
					ForceNewRun: true,
				})
				if err != nil {
					t.Fatalf("Second submit failed: %v", err)
				}

				job1, _ := store.GetExecution(resp1.JobID)
				job2, _ := store.GetExecution(resp2.JobID)

				if job1.DAGHash != job2.DAGHash {
					t.Errorf("Same workflow should produce same dag_hash: %s vs %s", job1.DAGHash, job2.DAGHash)
				}
			},
		},
		{
			name: "freezes novo run config",
			verify: func(t *testing.T, manager *Manager, store *storage.Storage) {
				t.Helper()
				wf := &workflow.Workflow{
					ID:   "wf-superagent-freeze",
					Name: "Superagent Freeze Test",
					Nodes: []*workflow.Node{{
						ID:             "superagent",
						Type:           workflow.NodeTypeNovoRun,
						Prompt:         "wake",
						TaskID:         "task-a",
						TaskSummary:    "brief",
						Identity:       "sde-novo",
						Image:          "novomo/novo:dev",
						Sandbox:        "host",
						RuntimeURL:     "http://127.0.0.1:8080",
						TimeoutSeconds: 30,
						GraceSeconds:   5,
						RetryPolicy:    &workflow.RetryPolicy{MaxAttempts: 1},
						RepoSpecs: []map[string]interface{}{{
							"name": "app",
						}},
						WorkSource: map[string]interface{}{
							"type": "gitea_branch",
						},
					}},
				}

				resp, err := manager.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
					Workflow:    wf,
					ForceNewRun: true,
				})
				if err != nil {
					t.Fatalf("SubmitWorkflow failed: %v", err)
				}
				job, err := store.GetExecution(resp.JobID)
				if err != nil {
					t.Fatalf("Failed to get job: %v", err)
				}

				var frozen workflowruntime.CanonicalWorkflow
				if err := json.Unmarshal([]byte(job.DAGSnapshot), &frozen); err != nil {
					t.Fatalf("unmarshal dag snapshot: %v", err)
				}
				got := frozen.Nodes[0]
				if got.TaskID != "task-a" || got.RuntimeURL != "http://127.0.0.1:8080" || got.GraceSeconds != 5 || got.RepoSpecs[0]["name"] != "app" {
					t.Fatalf("expected novo_run fields in submitted snapshot, got %+v", got)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manager, store := setupManagerTest(t)
			tc.verify(t, manager, store)
		})
	}
}

func TestSubmitWorkflow_DedupEdgeCases(t *testing.T) {
	manager, store := setupManagerTest(t)
	ctx := context.Background()

	makeWF := func(suffix string) *workflow.Workflow {
		return &workflow.Workflow{
			ID:   "wf-dedup-edge-" + suffix,
			Name: "Dedup Edge Case " + suffix,
			Nodes: []*workflow.Node{
				strictPromptNode("n1", "mock-model", "Hello "+suffix),
			},
		}
	}

	t.Run("request hash dedup returns existing job", func(t *testing.T) {
		suffix := fmt.Sprintf("reqhash-%d", time.Now().UnixNano())
		resp1, err := manager.SubmitWorkflow(ctx, &SubmitWorkflowRequest{
			Workflow: makeWF(suffix),
		})
		if err != nil {
			t.Fatalf("first submit failed: %v", err)
		}
		resp2, err := manager.SubmitWorkflow(ctx, &SubmitWorkflowRequest{
			Workflow: makeWF(suffix),
		})
		if err != nil {
			t.Fatalf("second submit failed: %v", err)
		}

		if !resp2.Duplicate {
			t.Fatal("expected duplicate=true on second request-hash submit")
		}
		if resp2.DedupReason != DedupReasonRequestHash {
			t.Fatalf("expected dedup reason %s, got %s", DedupReasonRequestHash, resp2.DedupReason)
		}
		if resp2.JobID != resp1.JobID {
			t.Fatalf("expected same job id for request-hash dedup, got %s and %s", resp1.JobID, resp2.JobID)
		}
	})

	t.Run("failed idempotency-key collision falls back to fresh run", func(t *testing.T) {
		key := fmt.Sprintf("dedup-failed-collision-%d", time.Now().UnixNano())
		failedJobID := fmt.Sprintf("failed-job-%d", time.Now().UnixNano())
		wfSuffix := fmt.Sprintf("failed-collision-%d", time.Now().UnixNano())

		err := store.CreateExecution(&storage.Job{
			ID:                  failedJobID,
			Description:         "failed prior run",
			Model:               "workflow",
			Status:              events.JobStatusFailed,
			WorkflowID:          "wf-failed-prior",
			IdempotencyKey:      key,
			WorkflowExecutionID: failedJobID,
			RunID:               failedJobID,
			RunNumber:           1,
			DAGSnapshot:         "{}",
			DAGHash:             "hash-failed-prior",
		})
		if err != nil {
			t.Fatalf("failed to seed failed job: %v", err)
		}

		resp, err := manager.SubmitWorkflow(ctx, &SubmitWorkflowRequest{
			Workflow:       makeWF(wfSuffix),
			IdempotencyKey: key,
		})
		if err != nil {
			t.Fatalf("submit failed: %v", err)
		}
		if resp.Duplicate {
			t.Fatal("expected fresh run, got duplicate response")
		}
		if resp.JobID == failedJobID {
			t.Fatalf("expected a new job id, got old failed job id %s", failedJobID)
		}

		job, err := store.GetExecution(resp.JobID)
		if err != nil {
			t.Fatalf("failed to load fallback job: %v", err)
		}
		if job.IdempotencyKey != "" {
			t.Fatalf("expected fallback job to be created without idempotency key, got %q", job.IdempotencyKey)
		}
	})

	t.Run("force new run bypasses idempotency dedup", func(t *testing.T) {
		key := fmt.Sprintf("dedup-force-new-%d", time.Now().UnixNano())
		wfSuffix := fmt.Sprintf("force-new-%d", time.Now().UnixNano())
		resp1, err := manager.SubmitWorkflow(ctx, &SubmitWorkflowRequest{
			Workflow:       makeWF(wfSuffix),
			IdempotencyKey: key,
		})
		if err != nil {
			t.Fatalf("first submit failed: %v", err)
		}

		resp2, err := manager.SubmitWorkflow(ctx, &SubmitWorkflowRequest{
			Workflow:       makeWF(wfSuffix),
			IdempotencyKey: key,
			ForceNewRun:    true,
		})
		if err != nil {
			t.Fatalf("force_new_run submit failed: %v", err)
		}

		if resp2.Duplicate {
			t.Fatal("expected force_new_run submit to bypass dedup")
		}
		if !resp2.DedupBypassed {
			t.Fatal("expected dedup_bypassed=true for force_new_run submit")
		}
		if resp1.JobID == resp2.JobID {
			t.Fatalf("expected different job ids when force_new_run=true, got %s", resp2.JobID)
		}

		job, err := store.GetExecution(resp2.JobID)
		if err != nil {
			t.Fatalf("failed to load force_new_run job: %v", err)
		}
		if job.IdempotencyKey != "" {
			t.Fatalf("expected force_new_run job to be stored without idempotency key, got %q", job.IdempotencyKey)
		}
	})

	t.Run("disable request hash dedup creates fresh same-body jobs", func(t *testing.T) {
		manager, _ := setupManagerTest(t)
		wfSuffix := fmt.Sprintf("api-no-idem-%d", time.Now().UnixNano())
		resp1, err := manager.SubmitWorkflow(ctx, &SubmitWorkflowRequest{
			Workflow:                makeWF(wfSuffix),
			DisableRequestHashDedup: true,
			UserID:                  "api-key:key-1",
		})
		if err != nil {
			t.Fatalf("first submit failed: %v", err)
		}
		resp2, err := manager.SubmitWorkflow(ctx, &SubmitWorkflowRequest{
			Workflow:                makeWF(wfSuffix),
			DisableRequestHashDedup: true,
			UserID:                  "api-key:key-1",
		})
		if err != nil {
			t.Fatalf("second submit failed: %v", err)
		}

		if resp2.Duplicate {
			t.Fatal("expected repeated API submit without idempotency key to create a fresh job")
		}
		if resp1.JobID == resp2.JobID {
			t.Fatalf("expected different job ids, got %s", resp1.JobID)
		}
	})

	t.Run("disable request hash dedup preserves explicit idempotency dedup", func(t *testing.T) {
		manager, _ := setupManagerTest(t)
		key := fmt.Sprintf("api-idem-%d", time.Now().UnixNano())
		wfSuffix := fmt.Sprintf("api-idem-%d", time.Now().UnixNano())
		resp1, err := manager.SubmitWorkflow(ctx, &SubmitWorkflowRequest{
			Workflow:                makeWF(wfSuffix),
			IdempotencyKey:          key,
			DisableRequestHashDedup: true,
			UserID:                  "api-key:key-1",
		})
		if err != nil {
			t.Fatalf("first submit failed: %v", err)
		}
		resp2, err := manager.SubmitWorkflow(ctx, &SubmitWorkflowRequest{
			Workflow:                makeWF(wfSuffix),
			IdempotencyKey:          key,
			DisableRequestHashDedup: true,
			UserID:                  "api-key:key-1",
		})
		if err != nil {
			t.Fatalf("second submit failed: %v", err)
		}

		if !resp2.Duplicate {
			t.Fatal("expected explicit idempotency key to dedup")
		}
		if resp2.DedupReason != DedupReasonIdempotencyKey {
			t.Fatalf("dedup reason = %s, want %s", resp2.DedupReason, DedupReasonIdempotencyKey)
		}
		if resp1.JobID != resp2.JobID {
			t.Fatalf("expected same job id for idempotency dedup, got %s and %s", resp1.JobID, resp2.JobID)
		}
	})
}

func TestManager_StartupRecoveryFailsResumableJobWithInvalidRequestData(t *testing.T) {
	store, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	cfg := DefaultManagerConfig()
	cfg.MaxConcurrentWorkflows = 4
	cfg.WorkerCount = 4
	cfg.WorkerPollInterval = 2 * time.Millisecond
	manager := NewManagerWithConfig(store, providers.NewRegistry(), cfg)

	jobID := fmt.Sprintf("bad-request-%d", time.Now().UnixNano())
	if err := store.CreateExecution(&storage.Job{
		ID:                  jobID,
		Description:         "invalid request_data",
		Model:               "workflow",
		Status:              events.JobStatusRunning,
		RequestData:         "{not-json",
		WorkflowID:          "wf-bad-request-data",
		WorkflowExecutionID: jobID,
		RunID:               jobID,
		RunNumber:           1,
		DAGSnapshot:         "{}",
		DAGHash:             "hash-bad-request",
	}); err != nil {
		t.Fatalf("failed to seed running durable job: %v", err)
	}

	startWorkers(t, manager)

	deadline := time.Now().Add(3 * time.Second)
	var updated *storage.WorkflowExecution
	for time.Now().Before(deadline) {
		updated, err = store.GetExecution(jobID)
		if err != nil {
			t.Fatalf("failed to get job: %v", err)
		}
		if updated.Status == events.JobStatusFailed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if updated == nil || updated.Status != events.JobStatusFailed {
		t.Fatalf("expected running resumable job with invalid request_data to fail, got %v", updated.Status)
	}
	if !strings.Contains(updated.ErrorMessage, "worker:") || !strings.Contains(updated.ErrorMessage, "unmarshal request_data") {
		t.Fatalf("expected worker parse failure message, got: %s", updated.ErrorMessage)
	}
}

func TestManager_DoesNotContinuouslyScanRunningDurableJobsAfterStartupRecovery(t *testing.T) {
	store, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	cfg := DefaultManagerConfig()
	cfg.MaxConcurrentWorkflows = 4
	cfg.WorkerCount = 4
	cfg.WorkerPollInterval = 2 * time.Millisecond
	manager := NewManagerWithConfig(store, providers.NewRegistry(), cfg)
	startWorkers(t, manager)

	// Allow startup recovery pass to drain and disable running-row scans.
	time.Sleep(100 * time.Millisecond)

	jobID := fmt.Sprintf("post-startup-running-%d", time.Now().UnixNano())
	if err := store.CreateExecution(&storage.Job{
		ID:                  jobID,
		Description:         "inserted after startup recovery",
		Model:               "workflow",
		Status:              events.JobStatusRunning,
		RequestData:         "{not-json",
		WorkflowID:          "wf-post-startup-running",
		WorkflowExecutionID: jobID,
		RunID:               jobID,
		RunNumber:           1,
		DAGSnapshot:         "{}",
		DAGHash:             "hash-post-startup-running",
	}); err != nil {
		t.Fatalf("failed to seed post-startup running durable job: %v", err)
	}

	// Workers should not pick this up because resumable recovery is startup-only.
	time.Sleep(250 * time.Millisecond)

	updated, err := store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("failed to load post-startup running job: %v", err)
	}
	if updated.Status != events.JobStatusRunning {
		t.Fatalf("expected startup-only recovery behavior (status should stay running), got %s", updated.Status)
	}
}

// ---------------------------------------------------------------------------
// P0 Gate Tests -- Durable Worker Cutover
// ---------------------------------------------------------------------------

func Test_EventsPersistWithoutActiveWS(t *testing.T) {
	manager, store := setupManagerTest(t)
	startWorkers(t, manager)

	wf := simpleWorkflowWithID("wf-event-persist", "Event Persistence Test", "mock-model", "Hello")
	wf.Nodes[0].ID = "node1"

	result, err := manager.ExecuteWorkflow(context.Background(), wf)
	if err != nil {
		t.Fatalf("ExecuteWorkflow failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("execution failed: %s", result.Error)
	}

	ctx := context.Background()
	evts, err := store.GetEventsAfter(ctx, result.JobID, 0)
	if err != nil {
		t.Fatalf("GetEventsAfter failed: %v", err)
	}
	if len(evts) == 0 {
		t.Fatal("no events in job_events -- events must persist regardless of WS callback")
	}

	// Check for specific lifecycle events
	types := make(map[string]bool)
	for _, e := range evts {
		types[e.Type] = true
	}
	if !types["node_start"] && !types["status"] {
		t.Error("expected status or node_start event")
	}
	if !types["node_complete"] {
		t.Error("expected node_complete event")
	}
	if !types["complete"] {
		t.Error("expected complete event")
	}

	// Verify sequences are monotonic
	for i := 1; i < len(evts); i++ {
		if evts[i].Sequence <= evts[i-1].Sequence {
			t.Errorf("sequences not monotonic at index %d: %d <= %d",
				i, evts[i].Sequence, evts[i-1].Sequence)
		}
	}
}

func Test_ManagerResumesDurableRunningJobsOnStartup(t *testing.T) {
	store, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}

	registry := providers.NewRegistry()
	registry.Register(&mockProvider{
		name: "mock",
		models: []providers.Model{
			{ID: "mock-model", Name: "Mock", Provider: "mock", Available: true},
		},
		response: &providers.CompletionResponse{
			Content: "Resumed response",
			Usage:   providers.Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
		},
	})

	// First manager: submit a job
	cfg := DefaultManagerConfig()
	cfg.MaxConcurrentWorkflows = 4
	cfg.WorkerCount = 4
	cfg.WorkerPollInterval = 2 * time.Millisecond
	manager1 := NewManagerWithConfig(store, registry, cfg)
	temp := 0.0
	wf := &workflow.Workflow{
		ID:   "wf-resume",
		Name: "Resume Test",
		Nodes: []*workflow.Node{
			{
				ID:             "node1",
				Type:           workflow.NodeTypePrompt,
				Model:          "mock-model",
				Prompt:         "Hello",
				Temperature:    &temp,
				MaxTokens:      256,
				TimeoutSeconds: 30,
				RetryPolicy:    workflow.DefaultRetryPolicy(),
			},
		},
	}

	resp, err := manager1.SubmitWorkflow(context.Background(), &SubmitWorkflowRequest{
		Workflow: wf, ForceNewRun: true,
	})
	if err != nil {
		t.Fatalf("SubmitWorkflow failed: %v", err)
	}

	// Simulate server crash: mark job as running in DB
	job, err := store.GetExecution(resp.JobID)
	if err != nil {
		t.Fatalf("GetJob failed: %v", err)
	}
	job.Status = "running"
	if err := store.UpdateExecution(job); err != nil {
		t.Fatalf("UpdateJob failed: %v", err)
	}

	// Create NEW manager (simulating server restart).
	manager2 := NewManagerWithConfig(store, registry, cfg)
	startWorkers(t, manager2)

	// Poll for job completion
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, _ = store.GetExecution(resp.JobID)
		if job.Status != "running" {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	if job.Status == "running" {
		t.Fatal("running durable job should have been resumed by new manager -- still stuck in 'running'")
	}
	if job.Status != "completed" {
		t.Fatalf("expected resumed job to complete, got status '%s' (error: %s)", job.Status, job.ErrorMessage)
	}
}

func TestDurableEventPayloadPassthrough(t *testing.T) {
	manager, store := setupManagerTest(t)
	startWorkers(t, manager)

	wf := simpleWorkflowWithID("wf-event-payload", "Payload Passthrough", "mock-model", "Hello")
	wf.Nodes[0].ID = "node1"

	result, err := manager.ExecuteWorkflow(context.Background(), wf)
	if err != nil {
		t.Fatalf("ExecuteWorkflow failed: %v", err)
	}

	evts, err := store.GetEventsAfter(context.Background(), result.JobID, 0)
	if err != nil {
		t.Fatalf("GetEventsAfter failed: %v", err)
	}

	var completeEvt *events.EventEnvelope
	for _, e := range evts {
		if e.Type == "complete" {
			completeEvt = e
			break
		}
	}
	if completeEvt == nil {
		t.Fatalf("expected complete event in persisted events")
	}
	if completeEvt.Payload == nil {
		t.Fatalf("expected complete event payload to be populated")
	}
	if _, ok := completeEvt.Payload["final_output"]; !ok {
		t.Fatalf("expected final_output in complete event payload")
	}
	if _, ok := completeEvt.Payload["total_tokens"]; !ok {
		t.Fatalf("expected total_tokens in complete event payload")
	}
}

// ---------------------------------------------------------------------------
// RunConcurrently tests
// ---------------------------------------------------------------------------

func TestRunConcurrently(t *testing.T) {
	t.Run("progress and concurrency cap", func(t *testing.T) {
		const totalTasks = 12
		const concurrency = 3

		var mu sync.Mutex
		currentRunning := 0
		maxRunning := 0
		progressCalls := 0
		seenCompleted := make(map[int]int, totalTasks)
		callbackErrors := make([]string, 0)

		tasks := make([]func(), 0, totalTasks)
		for range totalTasks {
			tasks = append(tasks, func() {
				mu.Lock()
				currentRunning++
				if currentRunning > maxRunning {
					maxRunning = currentRunning
				}
				mu.Unlock()

				time.Sleep(20 * time.Millisecond)

				mu.Lock()
				currentRunning--
				mu.Unlock()
			})
		}

		RunConcurrently(tasks, concurrency, func(completed, total int) {
			mu.Lock()
			defer mu.Unlock()
			progressCalls++
			if total != totalTasks {
				callbackErrors = append(callbackErrors, fmt.Sprintf("expected total=%d in progress callback, got %d", totalTasks, total))
				return
			}
			if completed < 1 || completed > totalTasks {
				callbackErrors = append(callbackErrors, fmt.Sprintf("expected completed in [1,%d], got %d", totalTasks, completed))
				return
			}
			seenCompleted[completed]++
		})

		mu.Lock()
		defer mu.Unlock()
		if len(callbackErrors) > 0 {
			t.Fatalf("progress callback validation errors: %v", callbackErrors)
		}
		if progressCalls != totalTasks {
			t.Fatalf("expected %d progress callbacks, got %d", totalTasks, progressCalls)
		}
		if len(seenCompleted) != totalTasks {
			t.Fatalf("expected %d unique completion indexes, got %d", totalTasks, len(seenCompleted))
		}
		for i := 1; i <= totalTasks; i++ {
			if seenCompleted[i] != 1 {
				t.Fatalf("expected completion index %d to appear once, got %d", i, seenCompleted[i])
			}
		}
		if maxRunning > concurrency {
			t.Fatalf("expected max running <= %d, got %d", concurrency, maxRunning)
		}
	})

	t.Run("normalizes invalid concurrency", func(t *testing.T) {
		var mu sync.Mutex
		completed := 0

		tasks := []func(){
			func() { mu.Lock(); completed++; mu.Unlock() },
			func() { mu.Lock(); completed++; mu.Unlock() },
		}

		RunConcurrently(tasks, 0, nil)

		mu.Lock()
		defer mu.Unlock()
		if completed != len(tasks) {
			t.Fatalf("expected %d completed tasks, got %d", len(tasks), completed)
		}
	})
}

func TestManager_IsRunningAndConfig(t *testing.T) {
	manager, _ := setupManagerTest(t)

	cfg := manager.Config()
	if cfg == nil {
		t.Fatal("expected non-nil manager config")
	}
	if cfg.MaxConcurrentWorkflows <= 0 {
		t.Fatalf("expected positive MaxConcurrentWorkflows, got %d", cfg.MaxConcurrentWorkflows)
	}

	const jobID = "running-check-job"
	if manager.IsRunning(jobID) {
		t.Fatal("expected IsRunning=false before storing job")
	}

	manager.runningJobs.Store(jobID, context.CancelFunc(func() {}))
	t.Cleanup(func() { manager.runningJobs.Delete(jobID) })

	if !manager.IsRunning(jobID) {
		t.Fatal("expected IsRunning=true after storing running job")
	}
}
