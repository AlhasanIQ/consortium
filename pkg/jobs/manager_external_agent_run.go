package jobs

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/pkg/novomo"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
)

const (
	defaultAgentRunPollInterval  = 2 * time.Second
	agentRunPollGrace            = 10 * time.Second
	agentRunTerminalWriteTimeout = 15 * time.Second
)

type externalAgentRunConfig struct {
	RunKind           string
	Harness           string
	InheritFrom       *workflow.NovomoHandoffRef
	TaskID            string
	ParentJobID       string
	ParentExecutionID string
	ParentRunID       string
	ParentNodeID      string
	Attempt           int
	TimeoutSeconds    int
	GraceSeconds      int
	Submit            func(context.Context) (*externalAgentRunSnapshot, error)
	Get               func(context.Context, string) (*externalAgentRunSnapshot, error)
	Stop              func(context.Context, string) error
}

type externalAgentRunSnapshot struct {
	ExternalRunID    string
	ExternalJobRunID string
	ExternalTaskID   string
	Harness          string
	Status           string
	Output           string
	TokensInput      int
	TokensOutput     int
	CostUSD          float64
	ErrorCode        string
	ErrorMessage     string
	StartedAt        *time.Time
	FinishedAt       *time.Time
}

// StopExternalAgentRun asks Novomo to stop a persisted active agent_run or
// Superagent run, then marks the local row cancelled for immediate operator
// feedback. The running workflow poller will still reconcile the final Novomo
// terminal state on its next poll.
func (m *Manager) StopExternalAgentRun(ctx context.Context, jobID, agentRunID string) (*storage.AgentRun, error) {
	jobID = strings.TrimSpace(jobID)
	agentRunID = strings.TrimSpace(agentRunID)
	if jobID == "" || agentRunID == "" {
		return nil, fmt.Errorf("job id and agent run id are required: %w", ErrJobStateConflict)
	}

	row, err := m.storage.GetAgentRunByID(ctx, jobID, agentRunID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, storage.ErrNotFound
	}
	if strings.TrimSpace(row.ExternalRunID) == "" {
		return nil, fmt.Errorf("agent run %s has no external run id: %w", agentRunID, ErrJobStateConflict)
	}
	if !isRunningAgentRunStatus(row.Status) {
		return nil, fmt.Errorf("agent run %s is not active (status %q): %w", agentRunID, row.Status, ErrJobStateConflict)
	}

	factory := m.novomoStopClientFactory
	if factory == nil {
		factory = func() (novomoStopClient, error) {
			return novomo.NewClientFromEnv()
		}
	}

	client, err := factory()
	if err != nil {
		return nil, classifyNovomoClientError(err)
	}
	if client == nil {
		return nil, fmt.Errorf("novomo stop client is nil: %w", ErrJobStateConflict)
	}

	stopCtx, cancel := context.WithTimeout(ctx, agentRunTerminalWriteTimeout)
	defer cancel()
	switch strings.TrimSpace(row.RunKind) {
	case "novo_run":
		err = client.StopNovoRun(stopCtx, row.ExternalRunID)
	default:
		err = client.StopRun(stopCtx, row.ExternalRunID)
	}
	if err != nil {
		return nil, classifyNovomoClientError(err)
	}

	updated := *row
	updated.Status = "cancelled"
	updated.ErrorCode = "CANCELLED"
	updated.ErrorMessage = "Stop requested from Consortium admin"
	now := time.Now().UTC()
	updated.FinishedAt = &now
	persisted, err := m.storage.UpdateAgentRunIfNonTerminal(ctx, &updated)
	if err != nil {
		return nil, fmt.Errorf("mark agent run stopped: %w", err)
	}
	if !persisted {
		latest, latestErr := m.storage.GetAgentRunByID(ctx, row.JobID, row.ID)
		if latestErr != nil {
			return nil, latestErr
		}
		if latest == nil {
			return nil, storage.ErrNotFound
		}
		if isTerminalAgentRunStatus(latest.Status) {
			return latest, fmt.Errorf("agent run %s is already terminal (status %q): %w", agentRunID, latest.Status, ErrJobStateConflict)
		}
		return latest, fmt.Errorf("agent run %s changed before stop persisted: %w", agentRunID, ErrJobStateConflict)
	}
	return &updated, nil
}

func (m *Manager) executeExternalAgentRunWithConfig(ctx context.Context, cfg externalAgentRunConfig, pollInterval time.Duration) (*workflow.AgentRunResult, error) {
	if cfg.Submit == nil || cfg.Get == nil {
		return nil, workflow.NewNonRetryableError(fmt.Errorf("external agent run callbacks are nil"), "INVALID_CONFIG")
	}
	if pollInterval <= 0 {
		pollInterval = defaultAgentRunPollInterval
	}

	attempt := cfg.Attempt
	if attempt < 1 {
		attempt = 1
	}

	existing, err := m.storage.GetAgentRunByExecutionAttempt(ctx, cfg.ParentJobID, cfg.ParentRunID, cfg.ParentNodeID, attempt)
	if err != nil {
		return nil, workflow.NewRetryableError(err, "STORAGE_ERROR")
	}
	if existing != nil {
		if isTerminalAgentRunStatus(existing.Status) {
			return agentRunResultFromStorage(existing), nil
		}
		return m.pollExternalAgentRun(ctx, cfg, existing, pollInterval)
	}

	submit, err := cfg.Submit(ctx)
	if err != nil {
		return nil, classifyNovomoClientError(err)
	}
	if submit == nil || strings.TrimSpace(submit.ExternalRunID) == "" {
		return nil, workflow.NewRetryableError(fmt.Errorf("novomo returned empty external run id"), "NOVOMO_MALFORMED_RESPONSE")
	}

	row := &storage.AgentRun{
		ID:               agentRunRowID(cfg.ParentJobID, cfg.ParentRunID, cfg.ParentNodeID, attempt),
		JobID:            cfg.ParentJobID,
		ExecutionID:      fallbackString(cfg.ParentExecutionID, cfg.ParentJobID),
		RunID:            cfg.ParentRunID,
		NodeID:           cfg.ParentNodeID,
		Attempt:          attempt,
		RunKind:          cfg.RunKind,
		ExternalRunID:    submit.ExternalRunID,
		ExternalJobRunID: submit.ExternalJobRunID,
		ExternalTaskID:   submit.ExternalTaskID,
		InheritFromJSON:  novomoHandoffJSON(cfg.InheritFrom),
		Harness:          coalesceString(submit.Harness, cfg.Harness),
		Status:           runningStatusIfEmpty(submit.Status),
	}
	if err := m.storage.UpsertAgentRun(ctx, row); err != nil {
		return nil, workflow.NewRetryableError(fmt.Errorf("persist agent run: %w", err), "STORAGE_ERROR")
	}

	return m.pollExternalAgentRun(ctx, cfg, row, pollInterval)
}

func (m *Manager) pollExternalAgentRun(ctx context.Context, cfg externalAgentRunConfig, row *storage.AgentRun, pollInterval time.Duration) (*workflow.AgentRunResult, error) {
	if row == nil {
		return nil, workflow.NewNonRetryableError(fmt.Errorf("agent run row is nil"), "INVALID_CONFIG")
	}

	grace := time.Duration(cfg.GraceSeconds) * time.Second
	if grace < 0 {
		grace = 0
	}
	deadline := time.Now().Add(time.Duration(cfg.TimeoutSeconds)*time.Second + grace)
	for {
		if ctx.Err() != nil {
			m.stopExternalAgentRun(row, cfg)
			return m.markAgentRunContextDone(row, ctx.Err())
		}
		if time.Now().After(deadline) {
			m.stopExternalAgentRun(row, cfg)
			return m.markAgentRunUnresponsive(row, fmt.Errorf("novomo run %s did not reach terminal status", row.ExternalRunID))
		}

		run, err := cfg.Get(ctx, row.ExternalRunID)
		if err != nil {
			classified := classifyNovomoClientError(err)
			if workflow.IsRetryable(classified) {
				if !sleepWithContext(ctx, pollInterval) {
					m.stopExternalAgentRun(row, cfg)
					return m.markAgentRunContextDone(row, ctx.Err())
				}
				continue
			}
			failed := *row
			failed.Status = "failed"
			failed.ErrorCode = workflow.GetErrorCode(classified)
			failed.ErrorMessage = classified.Error()
			now := time.Now().UTC()
			failed.FinishedAt = &now
			persisted, err := m.updateAgentRunTerminalIfNonTerminal(&failed)
			if err != nil {
				log.Printf("warning: failed to mark agent run %s failed: %v", failed.ID, err)
			} else if persisted != nil {
				return agentRunResultFromStorage(persisted), nil
			}
			return agentRunResultFromStorage(&failed), nil
		}
		if run == nil {
			m.stopExternalAgentRun(row, cfg)
			return m.markAgentRunUnresponsive(row, fmt.Errorf("novomo returned nil run"))
		}

		updated := *row
		updated.Status = runningStatusIfEmpty(run.Status)
		if strings.TrimSpace(run.ExternalJobRunID) != "" {
			updated.ExternalJobRunID = run.ExternalJobRunID
		}
		if strings.TrimSpace(run.ExternalTaskID) != "" {
			updated.ExternalTaskID = run.ExternalTaskID
		}
		if strings.TrimSpace(run.Harness) != "" {
			updated.Harness = run.Harness
		}
		if isRunningAgentRunStatus(updated.Status) {
			if run.StartedAt != nil {
				updated.StartedAt = run.StartedAt
			}
			persisted, err := m.storage.UpdateAgentRunIfNonTerminal(ctx, &updated)
			if err != nil {
				log.Printf("warning: failed to update running agent run %s: %v", row.ID, err)
			} else if !persisted {
				latest, getErr := m.storage.GetAgentRunByID(ctx, row.JobID, row.ID)
				if getErr != nil {
					log.Printf("warning: failed to refresh skipped agent run %s update: %v", row.ID, getErr)
				} else if latest != nil && isTerminalAgentRunStatus(latest.Status) {
					return agentRunResultFromStorage(latest), nil
				}
			}
			row = &updated
			if !sleepWithContext(ctx, pollInterval) {
				m.stopExternalAgentRun(row, cfg)
				return m.markAgentRunContextDone(row, ctx.Err())
			}
			continue
		}

		updated.Output = run.Output
		updated.TokensInput = run.TokensInput
		updated.TokensOutput = run.TokensOutput
		updated.CostUSD = run.CostUSD
		updated.ErrorCode = run.ErrorCode
		updated.ErrorMessage = run.ErrorMessage
		if run.StartedAt != nil {
			updated.StartedAt = run.StartedAt
		}
		if run.FinishedAt != nil {
			updated.FinishedAt = run.FinishedAt
		} else {
			now := time.Now().UTC()
			updated.FinishedAt = &now
		}
		if err := m.storage.UpsertAgentRun(ctx, &updated); err != nil {
			return nil, workflow.NewRetryableError(fmt.Errorf("update agent run: %w", err), "STORAGE_ERROR")
		}
		return agentRunResultFromStorage(&updated), nil
	}
}

func (m *Manager) stopExternalAgentRun(row *storage.AgentRun, cfg externalAgentRunConfig) {
	if cfg.Stop == nil || row == nil {
		return
	}
	externalRunID := strings.TrimSpace(row.ExternalRunID)
	if externalRunID == "" {
		return
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), agentRunTerminalWriteTimeout)
	defer cancel()
	if err := cfg.Stop(stopCtx, externalRunID); err != nil {
		if nerr, ok := novomo.AsError(err); ok && strings.EqualFold(nerr.Code, "NOT_STOPPABLE") {
			return
		}
		log.Printf("warning: failed to stop Novomo %s %s: %v", cfg.RunKind, externalRunID, err)
	}
}

func (m *Manager) markAgentRunContextDone(row *storage.AgentRun, cause error) (*workflow.AgentRunResult, error) {
	if errors.Is(cause, context.Canceled) {
		return m.markAgentRunCancelled(row)
	}
	return m.markAgentRunUnresponsive(row, cause)
}

func (m *Manager) markAgentRunCancelled(row *storage.AgentRun) (*workflow.AgentRunResult, error) {
	updated := *row
	updated.Status = "cancelled"
	updated.ErrorCode = "CANCELLED"
	updated.ErrorMessage = "Agent run cancelled by parent workflow"
	now := time.Now().UTC()
	updated.FinishedAt = &now
	persisted, err := m.updateAgentRunTerminalIfNonTerminal(&updated)
	if err != nil {
		log.Printf("warning: failed to mark agent run %s cancelled: %v", updated.ID, err)
	} else if persisted != nil {
		return agentRunResultFromStorage(persisted), nil
	}
	return agentRunResultFromStorage(&updated), nil
}

func (m *Manager) markAgentRunUnresponsive(row *storage.AgentRun, cause error) (*workflow.AgentRunResult, error) {
	updated := *row
	updated.Status = "failed"
	updated.ErrorCode = workflow.AgentRunErrorNovomoUnresponsive
	updated.ErrorMessage = "Novomo did not return a terminal status before the Consortium polling deadline"
	if cause != nil && !errors.Is(cause, context.Canceled) && !errors.Is(cause, context.DeadlineExceeded) {
		updated.ErrorMessage = cause.Error()
	}
	now := time.Now().UTC()
	updated.FinishedAt = &now
	persisted, err := m.updateAgentRunTerminalIfNonTerminal(&updated)
	if err != nil {
		log.Printf("warning: failed to mark agent run %s unresponsive: %v", updated.ID, err)
	} else if persisted != nil {
		return agentRunResultFromStorage(persisted), nil
	}
	return agentRunResultFromStorage(&updated), nil
}

func (m *Manager) updateAgentRunTerminalIfNonTerminal(row *storage.AgentRun) (*storage.AgentRun, error) {
	dbCtx, cancel := context.WithTimeout(context.Background(), agentRunTerminalWriteTimeout)
	defer cancel()

	var lastErr error
	backoff := 100 * time.Millisecond
	for {
		persisted, err := m.storage.UpdateAgentRunIfNonTerminal(dbCtx, row)
		if err == nil {
			if persisted {
				return row, nil
			}
			latest, getErr := m.storage.GetAgentRunByID(dbCtx, row.JobID, row.ID)
			if getErr != nil {
				return nil, getErr
			}
			if latest == nil {
				return nil, storage.ErrNotFound
			}
			return latest, nil
		}
		lastErr = err

		if dbCtx.Err() != nil {
			return nil, lastErr
		}
		if !sleepWithContext(dbCtx, backoff) {
			return nil, lastErr
		}
		if backoff < time.Second {
			backoff *= 2
		}
	}
}

func agentRunResultFromStorage(row *storage.AgentRun) *workflow.AgentRunResult {
	if row == nil {
		return nil
	}
	success := strings.EqualFold(row.Status, "completed")
	return &workflow.AgentRunResult{
		ExternalRunID:    row.ExternalRunID,
		ExternalRunKind:  row.RunKind,
		ExternalTaskID:   row.ExternalTaskID,
		ExternalJobRunID: row.ExternalJobRunID,
		InheritFrom:      novomoHandoffFromJSON(row.InheritFromJSON),
		Harness:          row.Harness,
		Status:           row.Status,
		Success:          success,
		Output:           row.Output,
		Error:            row.ErrorMessage,
		ErrorCode:        row.ErrorCode,
		TokensInput:      row.TokensInput,
		TokensOutput:     row.TokensOutput,
		Cost:             row.CostUSD,
		StartedAt:        row.StartedAt,
		FinishedAt:       row.FinishedAt,
	}
}

func classifyNovomoClientError(err error) error {
	if err == nil {
		return nil
	}
	if code := workflow.GetErrorCode(err); code != "" {
		return err
	}
	if nerr, ok := novomo.AsError(err); ok {
		code := nerr.Code
		if code == "" {
			code = "NOVOMO_ERROR"
		}
		if nerr.Retryable {
			return workflow.NewRetryableError(err, code)
		}
		return workflow.NewNonRetryableError(err, code)
	}
	if errors.Is(err, context.Canceled) {
		return workflow.NewNonRetryableError(err, "CANCELLED")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return workflow.NewRetryableError(err, workflow.RetryCodeTimeout)
	}
	return err
}

func isTerminalAgentRunStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "failed", "cancelled", "canceled", "timeout", "startup_failed", "paused", "crashed":
		return true
	default:
		return false
	}
}

func isRunningAgentRunStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "pending", "running", "started":
		return true
	default:
		return false
	}
}

func runningStatusIfEmpty(status string) string {
	if strings.TrimSpace(status) == "" {
		return "running"
	}
	return strings.TrimSpace(status)
}

func coalesceString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
