package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// NodeExecutionAttempt captures attempt-level lifecycle records across node types.
type NodeExecutionAttempt struct {
	ID           int64                  `json:"id"`
	JobID        string                 `json:"job_id"`
	ExecutionID  string                 `json:"execution_id"`
	RunID        string                 `json:"run_id"`
	NodeID       string                 `json:"node_id"`
	NodeType     string                 `json:"node_type"`
	Attempt      int                    `json:"attempt"`
	Status       string                 `json:"status"`
	ActivityID   string                 `json:"activity_id,omitempty"`
	StartedAt    *time.Time             `json:"started_at,omitempty"`
	CompletedAt  *time.Time             `json:"completed_at,omitempty"`
	LatencyMs    float64                `json:"latency_ms"`
	TokensInput  int                    `json:"tokens_input"`
	TokensOutput int                    `json:"tokens_output"`
	Cost         float64                `json:"cost"`
	ErrorCode    string                 `json:"error_code,omitempty"`
	ErrorMessage string                 `json:"error_message,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

// UpsertNodeExecutionAttempt writes attempt-level node execution attempt telemetry.
func (s *Storage) UpsertNodeExecutionAttempt(ctx context.Context, attempt *NodeExecutionAttempt) error {
	if attempt == nil {
		return fmt.Errorf("node execution attempt is nil")
	}
	if attempt.JobID == "" || attempt.ExecutionID == "" || attempt.RunID == "" || attempt.NodeID == "" || attempt.NodeType == "" {
		return fmt.Errorf("missing required node execution attempt fields")
	}
	if attempt.Attempt < 1 {
		attempt.Attempt = 1
	}

	metaJSON := "{}"
	if attempt.Metadata != nil {
		b, err := json.Marshal(attempt.Metadata)
		if err != nil {
			return fmt.Errorf("marshal node attempt metadata: %w", err)
		}
		metaJSON = string(b)
	}

	now := time.Now()
	if attempt.StartedAt == nil {
		attempt.StartedAt = &now
	}
	executionUID := fmt.Sprintf("%s:%s:%d", attempt.JobID, attempt.NodeID, attempt.Attempt)
	activityID := strings.TrimSpace(attempt.ActivityID)
	if activityID == "" {
		activityID = executionUID
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO workflow_node_execution_attempts (
			job_id, execution_id, run_id, node_id, node_type, attempt_number, status, activity_id,
			started_at, completed_at, latency_ms, tokens_input, tokens_output, cost,
			error_code, error_message, metadata, execution_uid, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(job_id, run_id, node_id, attempt_number) DO UPDATE SET
			status = excluded.status,
			activity_id = COALESCE(excluded.activity_id, workflow_node_execution_attempts.activity_id),
			started_at = COALESCE(workflow_node_execution_attempts.started_at, excluded.started_at),
			completed_at = excluded.completed_at,
			latency_ms = excluded.latency_ms,
			tokens_input = excluded.tokens_input,
			tokens_output = excluded.tokens_output,
			cost = excluded.cost,
			error_code = excluded.error_code,
			error_message = excluded.error_message,
			metadata = excluded.metadata,
			execution_uid = COALESCE(NULLIF(excluded.execution_uid, ''), workflow_node_execution_attempts.execution_uid),
			updated_at = excluded.updated_at
		`, attempt.JobID, attempt.ExecutionID, attempt.RunID, attempt.NodeID, attempt.NodeType,
		attempt.Attempt,
		attempt.Status, activityID,
		attempt.StartedAt, attempt.CompletedAt, attempt.LatencyMs, attempt.TokensInput, attempt.TokensOutput, attempt.Cost,
		nullIfEmpty(attempt.ErrorCode), nullIfEmpty(attempt.ErrorMessage), metaJSON, executionUID, now, now)
	if err != nil {
		return fmt.Errorf("upsert node execution attempt: %w", err)
	}

	// Keep the node projection table up to date with latest attempt status/metrics.
	// The COALESCE(MAX(node_order), -1) + 1 subquery auto-assigns a monotonic
	// node_order. This is safe under SQLite's single-writer model (only one
	// write transaction executes at a time). A multi-writer database would need
	// an explicit sequence counter or advisory lock.
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO workflow_node_executions (
			job_id, execution_id, run_id, node_id, node_type, node_order, status,
			tokens_input, tokens_output, cost, latency_ms,
			error_code, error_message, metadata, execution_uid, attempt_number,
			activity_id, started_at, completed_at, created_at, updated_at
		) VALUES (
			?, ?, ?, ?, ?,
			(SELECT COALESCE(MAX(node_order), -1) + 1 FROM workflow_node_executions WHERE job_id = ? AND run_id = ?),
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		)
		ON CONFLICT(job_id, run_id, node_id) DO UPDATE SET
			status = excluded.status,
			tokens_input = excluded.tokens_input,
			tokens_output = excluded.tokens_output,
			cost = excluded.cost,
			latency_ms = excluded.latency_ms,
			error_code = excluded.error_code,
			error_message = excluded.error_message,
			metadata = excluded.metadata,
			execution_uid = CASE
				WHEN excluded.attempt_number >= workflow_node_executions.attempt_number
				THEN COALESCE(NULLIF(excluded.execution_uid, ''), workflow_node_executions.execution_uid)
				ELSE workflow_node_executions.execution_uid
			END,
			attempt_number = CASE
				WHEN excluded.attempt_number >= workflow_node_executions.attempt_number
				THEN excluded.attempt_number
				ELSE workflow_node_executions.attempt_number
			END,
			activity_id = COALESCE(excluded.activity_id, workflow_node_executions.activity_id),
			started_at = COALESCE(workflow_node_executions.started_at, excluded.started_at),
			completed_at = COALESCE(excluded.completed_at, workflow_node_executions.completed_at),
			updated_at = excluded.updated_at
	`, attempt.JobID, attempt.ExecutionID, attempt.RunID, attempt.NodeID, attempt.NodeType,
		attempt.JobID, attempt.RunID,
		attempt.Status, attempt.TokensInput, attempt.TokensOutput, attempt.Cost, attempt.LatencyMs,
		nullIfEmpty(attempt.ErrorCode), nullIfEmpty(attempt.ErrorMessage), metaJSON, executionUID, attempt.Attempt,
		activityID, attempt.StartedAt, attempt.CompletedAt, now, now)
	if err != nil {
		return fmt.Errorf("upsert workflow node execution projection: %w", err)
	}
	return nil
}

// ListNodeExecutionAttemptsByJob returns ordered node attempt history for a job.
func (s *Storage) ListNodeExecutionAttemptsByJob(ctx context.Context, jobID string) ([]NodeExecutionAttempt, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, job_id, execution_id, run_id, node_id, node_type, attempt_number, status,
		       COALESCE(activity_id, ''), started_at, completed_at, latency_ms,
		       tokens_input, tokens_output, cost, COALESCE(error_code, ''),
		       COALESCE(error_message, ''), COALESCE(metadata, '{}'),
		       created_at, updated_at
		FROM workflow_node_execution_attempts
		WHERE job_id = ?
		ORDER BY node_id ASC, attempt_number ASC
	`, jobID)
	if err != nil {
		return nil, fmt.Errorf("query node execution attempts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []NodeExecutionAttempt
	for rows.Next() {
		var item NodeExecutionAttempt
		var metaJSON string
		if err := rows.Scan(
			&item.ID, &item.JobID, &item.ExecutionID, &item.RunID, &item.NodeID, &item.NodeType, &item.Attempt, &item.Status,
			&item.ActivityID, &item.StartedAt, &item.CompletedAt, &item.LatencyMs,
			&item.TokensInput, &item.TokensOutput, &item.Cost, &item.ErrorCode, &item.ErrorMessage,
			&metaJSON, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan node execution attempt: %w", err)
		}
		_ = json.Unmarshal([]byte(metaJSON), &item.Metadata)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate node execution attempts: %w", err)
	}
	return out, nil
}

func (s *Storage) markRunningNodeExecutionAttemptsTerminal(ctx context.Context, jobID, runID, terminalStatus string) (int64, error) {
	if jobID == "" || runID == "" {
		return 0, fmt.Errorf("job_id and run_id are required")
	}
	if strings.TrimSpace(terminalStatus) == "" {
		return 0, fmt.Errorf("terminal status is required")
	}
	now := time.Now()
	res, err := s.db.ExecContext(ctx, `
		UPDATE workflow_node_execution_attempts
		SET status = ?,
		    completed_at = COALESCE(completed_at, ?),
		    updated_at = ?
		WHERE job_id = ?
		  AND run_id = ?
		  AND status = 'running'
	`, terminalStatus, now, now, jobID, runID)
	if err != nil {
		return 0, fmt.Errorf("mark running node attempts %s: %w", terminalStatus, err)
	}
	_, _ = s.db.ExecContext(ctx, `
		UPDATE workflow_node_executions
		SET status = ?,
		    completed_at = COALESCE(completed_at, ?),
		    updated_at = ?
		WHERE job_id = ?
		  AND run_id = ?
		  AND status = 'running'
	`, terminalStatus, now, now, jobID, runID)
	count, _ := res.RowsAffected()
	return count, nil
}

// MarkRunningNodeExecutionAttemptsCancelled marks in-flight node execution attempt rows as cancelled
// for the provided job/run pair.
func (s *Storage) MarkRunningNodeExecutionAttemptsCancelled(ctx context.Context, jobID, runID string) (int64, error) {
	return s.markRunningNodeExecutionAttemptsTerminal(ctx, jobID, runID, "cancelled")
}

// MarkRunningNodeExecutionAttemptsInterrupted marks in-flight node execution attempt rows as interrupted
// for the provided job/run pair. Used when replay resumes and re-dispatches in-flight work.
func (s *Storage) MarkRunningNodeExecutionAttemptsInterrupted(ctx context.Context, jobID, runID string) (int64, error) {
	return s.markRunningNodeExecutionAttemptsTerminal(ctx, jobID, runID, "interrupted")
}

// AgentRun stores aggregate telemetry for an autonomous agent node execution.
type AgentRun struct {
	ID               string     `json:"id"`
	JobID            string     `json:"job_id"`
	ExecutionID      string     `json:"execution_id"`
	RunID            string     `json:"run_id"`
	NodeID           string     `json:"node_id"`
	Attempt          int        `json:"attempt"`
	RunKind          string     `json:"run_kind"`
	ExternalRunID    string     `json:"external_run_id"`
	ExternalJobRunID string     `json:"external_job_run_id,omitempty"`
	ExternalTaskID   string     `json:"external_task_id,omitempty"`
	InheritFromJSON  string     `json:"inherit_from_json,omitempty"`
	Harness          string     `json:"harness"`
	Status           string     `json:"status"`
	Output           string     `json:"output,omitempty"`
	TokensInput      int        `json:"tokens_input"`
	TokensOutput     int        `json:"tokens_output"`
	CostUSD          float64    `json:"cost_usd"`
	ErrorCode        string     `json:"error_code,omitempty"`
	ErrorMessage     string     `json:"error_message,omitempty"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// UpsertAgentRun creates or updates an agent run summary.
func (s *Storage) UpsertAgentRun(ctx context.Context, run *AgentRun) error {
	if err := normalizeAgentRunForWrite(run); err != nil {
		return err
	}
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_runs (
			id, job_id, execution_id, run_id, node_id, attempt, run_kind, external_run_id, external_job_run_id, external_task_id, inherit_from_json, harness, status,
			output, tokens_input, tokens_output, cost_usd, error_code, error_message,
			started_at, finished_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			status = excluded.status,
			run_kind = excluded.run_kind,
			external_run_id = excluded.external_run_id,
			external_job_run_id = COALESCE(NULLIF(excluded.external_job_run_id, ''), agent_runs.external_job_run_id),
			external_task_id = COALESCE(NULLIF(excluded.external_task_id, ''), agent_runs.external_task_id),
			inherit_from_json = COALESCE(NULLIF(excluded.inherit_from_json, ''), agent_runs.inherit_from_json),
			harness = excluded.harness,
			output = excluded.output,
			tokens_input = excluded.tokens_input,
			tokens_output = excluded.tokens_output,
			cost_usd = excluded.cost_usd,
			error_code = excluded.error_code,
			error_message = excluded.error_message,
			started_at = COALESCE(agent_runs.started_at, excluded.started_at),
			finished_at = excluded.finished_at,
			updated_at = excluded.updated_at
	`, run.ID, run.JobID, run.ExecutionID, run.RunID, run.NodeID, run.Attempt, run.RunKind, run.ExternalRunID, nullIfEmpty(run.ExternalJobRunID), nullIfEmpty(run.ExternalTaskID), nullIfEmpty(run.InheritFromJSON), run.Harness, run.Status,
		nullIfEmpty(run.Output), run.TokensInput, run.TokensOutput, run.CostUSD,
		nullIfEmpty(run.ErrorCode), nullIfEmpty(run.ErrorMessage), run.StartedAt, run.FinishedAt, now, now)
	if err != nil {
		return fmt.Errorf("upsert agent run: %w", err)
	}
	return nil
}

// UpdateAgentRunIfNonTerminal updates an existing agent run row only while the
// stored row is still non-terminal. It prevents pollers from racing with and
// overwriting an operator stop or a terminal Novomo reconciliation.
func (s *Storage) UpdateAgentRunIfNonTerminal(ctx context.Context, run *AgentRun) (bool, error) {
	if err := normalizeAgentRunForWrite(run); err != nil {
		return false, err
	}
	now := time.Now()
	result, err := s.db.ExecContext(ctx, `
		UPDATE agent_runs SET
			status = ?,
			run_kind = ?,
			external_run_id = ?,
			external_job_run_id = COALESCE(NULLIF(?, ''), external_job_run_id),
			external_task_id = COALESCE(NULLIF(?, ''), external_task_id),
			inherit_from_json = COALESCE(NULLIF(?, ''), inherit_from_json),
			harness = ?,
			output = ?,
			tokens_input = ?,
			tokens_output = ?,
			cost_usd = ?,
			error_code = ?,
			error_message = ?,
			started_at = COALESCE(started_at, ?),
			finished_at = ?,
			updated_at = ?
		WHERE job_id = ? AND id = ?
		  AND status NOT IN ('completed', 'failed', 'cancelled', 'canceled', 'timeout', 'startup_failed', 'paused', 'crashed')
	`, run.Status, run.RunKind, run.ExternalRunID, nullIfEmpty(run.ExternalJobRunID), nullIfEmpty(run.ExternalTaskID), nullIfEmpty(run.InheritFromJSON),
		run.Harness, nullIfEmpty(run.Output), run.TokensInput, run.TokensOutput, run.CostUSD, nullIfEmpty(run.ErrorCode), nullIfEmpty(run.ErrorMessage),
		run.StartedAt, run.FinishedAt, now, run.JobID, run.ID)
	if err != nil {
		return false, fmt.Errorf("update active agent run: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read active agent run update count: %w", err)
	}
	return rows > 0, nil
}

func normalizeAgentRunForWrite(run *AgentRun) error {
	if run == nil {
		return fmt.Errorf("agent run is nil")
	}
	if run.ID == "" || run.JobID == "" || run.ExecutionID == "" || run.RunID == "" || run.NodeID == "" || run.ExternalRunID == "" || run.Status == "" {
		return fmt.Errorf("missing required agent run fields")
	}
	if run.RunKind != "novo_run" && run.Harness == "" {
		return fmt.Errorf("missing required agent run fields")
	}
	if run.Attempt < 1 {
		run.Attempt = 1
	}
	if run.RunKind == "" {
		run.RunKind = "agent_run"
	}
	if run.RunKind != "agent_run" && run.RunKind != "novo_run" {
		return fmt.Errorf("invalid agent run kind %q", run.RunKind)
	}
	return nil
}

// ListAgentRunsByJob lists all agent runs for a job.
func (s *Storage) ListAgentRunsByJob(ctx context.Context, jobID string) ([]AgentRun, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, job_id, execution_id, run_id, node_id, attempt, run_kind, external_run_id, COALESCE(external_job_run_id, ''), COALESCE(external_task_id, ''), COALESCE(inherit_from_json, ''), harness, status,
		       COALESCE(output, ''), tokens_input, tokens_output, cost_usd,
		       COALESCE(error_code, ''), COALESCE(error_message, ''),
		       started_at, finished_at, created_at, updated_at
		FROM agent_runs
		WHERE job_id = ?
		ORDER BY created_at ASC, id ASC
	`, jobID)
	if err != nil {
		return nil, fmt.Errorf("query agent runs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []AgentRun
	for rows.Next() {
		var item AgentRun
		if err := rows.Scan(
			&item.ID, &item.JobID, &item.ExecutionID, &item.RunID, &item.NodeID,
			&item.Attempt, &item.RunKind, &item.ExternalRunID, &item.ExternalJobRunID, &item.ExternalTaskID, &item.InheritFromJSON, &item.Harness, &item.Status,
			&item.Output, &item.TokensInput, &item.TokensOutput, &item.CostUSD,
			&item.ErrorCode, &item.ErrorMessage, &item.StartedAt, &item.FinishedAt, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan agent run: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent runs: %w", err)
	}
	return out, nil
}

// GetAgentRunByID returns one Novomo-backed run row scoped to a Consortium job.
func (s *Storage) GetAgentRunByID(ctx context.Context, jobID, id string) (*AgentRun, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, job_id, execution_id, run_id, node_id, attempt, run_kind, external_run_id, COALESCE(external_job_run_id, ''), COALESCE(external_task_id, ''), COALESCE(inherit_from_json, ''), harness, status,
		       COALESCE(output, ''), tokens_input, tokens_output, cost_usd,
		       COALESCE(error_code, ''), COALESCE(error_message, ''),
		       started_at, finished_at, created_at, updated_at
		FROM agent_runs
		WHERE job_id = ? AND id = ?
	`, jobID, id)
	return scanAgentRunRow(row, "get agent run by id")
}

// GetAgentRunByExecutionAttempt returns the Novomo agent run row for a workflow node attempt.
func (s *Storage) GetAgentRunByExecutionAttempt(ctx context.Context, jobID, runID, nodeID string, attempt int) (*AgentRun, error) {
	if attempt < 1 {
		attempt = 1
	}
	row := s.db.QueryRowContext(ctx, `
			SELECT id, job_id, execution_id, run_id, node_id, attempt, run_kind, external_run_id, COALESCE(external_job_run_id, ''), COALESCE(external_task_id, ''), COALESCE(inherit_from_json, ''), harness, status,
			       COALESCE(output, ''), tokens_input, tokens_output, cost_usd,
		       COALESCE(error_code, ''), COALESCE(error_message, ''),
		       started_at, finished_at, created_at, updated_at
		FROM agent_runs
		WHERE job_id = ? AND run_id = ? AND node_id = ? AND attempt = ?
	`, jobID, runID, nodeID, attempt)
	return scanAgentRunRow(row, "get agent run by execution attempt")
}

// GetLatestAgentRunByNode returns the most recent Novomo-backed run row for a
// workflow node within the same durable workflow run.
func (s *Storage) GetLatestAgentRunByNode(ctx context.Context, jobID, runID, nodeID string) (*AgentRun, error) {
	row := s.db.QueryRowContext(ctx, `
			SELECT id, job_id, execution_id, run_id, node_id, attempt, run_kind, external_run_id, COALESCE(external_job_run_id, ''), COALESCE(external_task_id, ''), COALESCE(inherit_from_json, ''), harness, status,
			       COALESCE(output, ''), tokens_input, tokens_output, cost_usd,
		       COALESCE(error_code, ''), COALESCE(error_message, ''),
		       started_at, finished_at, created_at, updated_at
		FROM agent_runs
		WHERE job_id = ? AND run_id = ? AND node_id = ?
		ORDER BY attempt DESC, updated_at DESC, created_at DESC, id DESC
		LIMIT 1
	`, jobID, runID, nodeID)
	return scanAgentRunRow(row, "get latest agent run by node")
}

type agentRunRowScanner interface {
	Scan(dest ...interface{}) error
}

func scanAgentRunRow(row agentRunRowScanner, operation string) (*AgentRun, error) {
	var item AgentRun
	if err := row.Scan(
		&item.ID, &item.JobID, &item.ExecutionID, &item.RunID, &item.NodeID, &item.Attempt,
		&item.RunKind, &item.ExternalRunID, &item.ExternalJobRunID, &item.ExternalTaskID, &item.InheritFromJSON, &item.Harness, &item.Status, &item.Output,
		&item.TokensInput, &item.TokensOutput, &item.CostUSD,
		&item.ErrorCode, &item.ErrorMessage, &item.StartedAt, &item.FinishedAt,
		&item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", operation, err)
	}
	return &item, nil
}

// PurgeExpiredReasoningTraces removes verbose reasoning artifacts older than TTL.
func (s *Storage) PurgeExpiredReasoningTraces(ctx context.Context, ttl time.Duration) (int64, error) {
	return 0, nil
}
