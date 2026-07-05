package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// GetNodeExecutionAttempts retrieves all execution attempts for a job (current run).
func (s *Storage) GetNodeExecutionAttempts(jobID string) ([]NodeExecutionAttempt, error) {
	rows, err := s.db.Query(`
		SELECT id, job_id, execution_id, run_id, node_id, node_type, attempt_number, status,
		       COALESCE(activity_id, ''), started_at, completed_at, COALESCE(latency_ms, 0),
		       COALESCE(tokens_input, 0), COALESCE(tokens_output, 0), COALESCE(cost, 0),
		       COALESCE(error_code, ''), COALESCE(error_message, ''), COALESCE(metadata, '{}'),
		       created_at, updated_at
		FROM workflow_node_execution_attempts
		WHERE job_id = ?
		  AND run_id = COALESCE(NULLIF((SELECT run_id FROM jobs WHERE id = ?), ''), ?)
		ORDER BY node_id ASC, attempt_number ASC, id ASC
	`, jobID, jobID, jobID)
	if err != nil {
		return nil, fmt.Errorf("query node execution attempts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanNodeExecutionAttempts(rows)
}

// ListNodeExecutionAttemptsByJobIDs returns attempt history grouped by job ID for
// the current run of each job.
func (s *Storage) ListNodeExecutionAttemptsByJobIDs(jobIDs []string) (map[string][]NodeExecutionAttempt, error) {
	ids := normalizeNonEmptyIDs(jobIDs)
	out := make(map[string][]NodeExecutionAttempt, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	const maxIDsPerChunk = 300
	for start := 0; start < len(ids); start += maxIDsPerChunk {
		end := start + maxIDsPerChunk
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		placeholders := inClausePlaceholders(len(chunk))

		query := `
			SELECT id, job_id, execution_id, run_id, node_id, node_type, attempt_number, status,
			       COALESCE(activity_id, ''), started_at, completed_at, COALESCE(latency_ms, 0),
			       COALESCE(tokens_input, 0), COALESCE(tokens_output, 0), COALESCE(cost, 0),
			       COALESCE(error_code, ''), COALESCE(error_message, ''), COALESCE(metadata, '{}'),
			       created_at, updated_at
			FROM workflow_node_execution_attempts
			WHERE job_id IN (` + placeholders + `)
			  AND run_id = COALESCE(
				NULLIF((SELECT run_id FROM jobs WHERE id = workflow_node_execution_attempts.job_id), ''),
				workflow_node_execution_attempts.job_id
			  )
			ORDER BY job_id ASC, node_id ASC, attempt_number ASC, id ASC
		`
		args := make([]interface{}, len(chunk))
		for i := range chunk {
			args[i] = chunk[i]
		}

		rows, err := s.db.Query(query, args...)
		if err != nil {
			return nil, fmt.Errorf("query node execution attempts by job ids: %w", err)
		}

		attempts, err := scanNodeExecutionAttempts(rows)
		closeErr := rows.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			log.Printf("Error closing rows: %v", closeErr)
		}

		for _, attempt := range attempts {
			out[attempt.JobID] = append(out[attempt.JobID], attempt)
		}
	}

	return out, nil
}

// GetNodeExecutionAttemptsForNode retrieves execution attempts for a specific node in a job.
func (s *Storage) GetNodeExecutionAttemptsForNode(jobID, nodeID string) ([]NodeExecutionAttempt, error) {
	rows, err := s.db.Query(`
		SELECT id, job_id, execution_id, run_id, node_id, node_type, attempt_number, status,
		       COALESCE(activity_id, ''), started_at, completed_at, COALESCE(latency_ms, 0),
		       COALESCE(tokens_input, 0), COALESCE(tokens_output, 0), COALESCE(cost, 0),
		       COALESCE(error_code, ''), COALESCE(error_message, ''), COALESCE(metadata, '{}'),
		       created_at, updated_at
		FROM workflow_node_execution_attempts
		WHERE job_id = ?
		  AND run_id = COALESCE(NULLIF((SELECT run_id FROM jobs WHERE id = ?), ''), ?)
		  AND node_id = ?
		ORDER BY attempt_number ASC, id ASC
	`, jobID, jobID, jobID, nodeID)
	if err != nil {
		return nil, fmt.Errorf("query node execution attempts for node: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanNodeExecutionAttempts(rows)
}

// scanNodeExecutionAttempts scans rows into NodeExecutionAttempt slice.
func scanNodeExecutionAttempts(rows *sql.Rows) ([]NodeExecutionAttempt, error) {
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

// AddWorkflowNode upserts the node-level execution projection for a run.
func (s *Storage) AddWorkflowNode(node *WorkflowNode) error {
	if node == nil {
		return fmt.Errorf("workflow node is nil")
	}
	if node.ExecutionID == "" || node.NodeID == "" || node.NodeType == "" || node.Status == "" {
		return fmt.Errorf("missing required workflow node fields")
	}
	if node.AttemptNumber < 1 {
		node.AttemptNumber = 1
	}
	runID := s.resolveRunID(node.ExecutionID, node.RunID)
	execUID := node.ExecutionUID
	if execUID == "" {
		execUID = fmt.Sprintf("%s:%s:%d", node.ExecutionID, node.NodeID, node.AttemptNumber)
	}
	meta := node.Metadata
	if strings.TrimSpace(meta) == "" {
		meta = "{}"
	}
	now := time.Now()
	var startedAt, completedAt, parentNodeID interface{}
	if node.StartedAt != nil {
		startedAt = *node.StartedAt
	}
	if node.CompletedAt != nil {
		completedAt = *node.CompletedAt
	} else if node.Status == "completed" || node.Status == "failed" || node.Status == "cancelled" {
		completedAt = now
	}
	if node.ParentNodeID != "" {
		parentNodeID = node.ParentNodeID
	}

	// The COALESCE(MAX(node_order), -1) + 1 subquery auto-assigns a monotonic
	// node_order when the caller does not provide one (node_order=0). This is
	// safe under SQLite's single-writer model (only one write transaction can
	// execute at a time). A multi-writer database would need an explicit
	// sequence counter or advisory lock.
	query := `
		INSERT INTO workflow_node_executions (
			job_id, execution_id, run_id, node_id, node_type, node_order, status,
			node_label, node_name, prompt, model, output, tokens_input, tokens_output,
			cost, latency_ms, error_message, error_code, metadata, execution_uid,
			attempt_number, activity_id, started_at, completed_at, parent_node_id, created_at, updated_at
		)
		VALUES (
			?, ?, ?, ?, ?,
			COALESCE(NULLIF(?, 0), (SELECT COALESCE(MAX(node_order), -1) + 1 FROM workflow_node_executions WHERE job_id = ? AND run_id = ?)),
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		)
		ON CONFLICT(job_id, run_id, node_id) DO UPDATE SET
			status = excluded.status,
			node_label = COALESCE(NULLIF(excluded.node_label, ''), workflow_node_executions.node_label),
			node_name = COALESCE(NULLIF(excluded.node_name, ''), workflow_node_executions.node_name),
			prompt = COALESCE(NULLIF(excluded.prompt, ''), workflow_node_executions.prompt),
			model = COALESCE(NULLIF(excluded.model, ''), workflow_node_executions.model),
			output = COALESCE(NULLIF(excluded.output, ''), workflow_node_executions.output),
			tokens_input = excluded.tokens_input,
			tokens_output = excluded.tokens_output,
			cost = excluded.cost,
			latency_ms = excluded.latency_ms,
			error_message = excluded.error_message,
			error_code = COALESCE(NULLIF(excluded.error_code, ''), workflow_node_executions.error_code),
			metadata = COALESCE(NULLIF(excluded.metadata, ''), workflow_node_executions.metadata),
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
			parent_node_id = COALESCE(excluded.parent_node_id, workflow_node_executions.parent_node_id),
			updated_at = excluded.updated_at
	`
	_, err := s.db.Exec(query,
		node.ExecutionID, node.ExecutionID, runID, node.NodeID, node.NodeType,
		node.NodeOrder, node.ExecutionID, runID,
		node.Status, node.NodeLabel, node.NodeName, node.Prompt, node.Model, node.Output,
		node.TokensInput, node.TokensOutput, node.Cost, node.LatencyMs,
		node.ErrorMessage, node.ErrorCode, meta, execUID,
		node.AttemptNumber, nullIfEmpty(node.ActivityID), startedAt, completedAt, parentNodeID, now, now,
	)
	if err != nil {
		return fmt.Errorf("failed to add workflow node: %w", err)
	}
	return nil
}

// UpdateWorkflowNode updates an existing workflow node execution projection row.
func (s *Storage) UpdateWorkflowNode(jobID, nodeID, status, output, errMsg string, tokensIn, tokensOut int, cost, latency float64) error {
	now := time.Now()
	query := `
		UPDATE workflow_node_executions
		SET status = ?, output = ?, tokens_input = ?, tokens_output = ?,
		    cost = ?, latency_ms = ?, error_message = ?, updated_at = ?
		WHERE id = (
			SELECT id
			FROM workflow_node_executions
			WHERE job_id = ? AND node_id = ?
			  AND run_id = COALESCE(NULLIF((SELECT run_id FROM jobs WHERE id = ?), ''), ?)
			ORDER BY attempt_number DESC, updated_at DESC, id DESC
			LIMIT 1
		)
	`
	_, err := s.db.Exec(query,
		status, output, tokensIn, tokensOut, cost, latency, errMsg, now,
		jobID, nodeID, jobID, jobID)
	if err != nil {
		return fmt.Errorf("failed to update workflow node: %w", err)
	}

	return nil
}

// GetWorkflowNodes retrieves node execution projection rows for the current run.
func (s *Storage) GetWorkflowNodes(jobID string) ([]WorkflowNode, error) {
	query := `
		SELECT id, job_id, COALESCE(run_id, ''), node_id, node_type, node_order, status,
		       COALESCE(node_label, ''), COALESCE(node_name, ''),
		       COALESCE(prompt, ''), COALESCE(model, ''), COALESCE(output, ''),
		       COALESCE(tokens_input, 0), COALESCE(tokens_output, 0), COALESCE(cost, 0), COALESCE(latency_ms, 0),
		       COALESCE(error_message, ''), COALESCE(error_code, ''), COALESCE(metadata, ''),
		       created_at, updated_at,
		       COALESCE(execution_uid, ''), COALESCE(attempt_number, 1),
		       COALESCE(activity_id, ''), started_at, completed_at,
		       COALESCE(parent_node_id, '')
		FROM workflow_node_executions
		WHERE job_id = ?
		  AND run_id = COALESCE(NULLIF((SELECT run_id FROM jobs WHERE id = ?), ''), ?)
		ORDER BY node_order ASC
	`
	rows, err := s.db.Query(query, jobID, jobID, jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow nodes: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("Error closing rows: %v", err)
		}
	}()

	var nodes []WorkflowNode
	for rows.Next() {
		var node WorkflowNode
		err := rows.Scan(
			&node.ID, &node.ExecutionID, &node.RunID, &node.NodeID, &node.NodeType, &node.NodeOrder, &node.Status,
			&node.NodeLabel, &node.NodeName,
			&node.Prompt, &node.Model, &node.Output, &node.TokensInput, &node.TokensOutput,
			&node.Cost, &node.LatencyMs, &node.ErrorMessage, &node.ErrorCode, &node.Metadata,
			&node.CreatedAt, &node.UpdatedAt,
			&node.ExecutionUID, &node.AttemptNumber, &node.ActivityID, &node.StartedAt, &node.CompletedAt,
			&node.ParentNodeID,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan workflow node: %w", err)
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating workflow nodes: %w", err)
	}
	return nodes, nil
}

// ListWorkflowNodesByJobIDs returns workflow node projection rows grouped by job
// ID for the current run of each job.
func (s *Storage) ListWorkflowNodesByJobIDs(jobIDs []string) (map[string][]WorkflowNode, error) {
	ids := normalizeNonEmptyIDs(jobIDs)
	out := make(map[string][]WorkflowNode, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	const maxIDsPerChunk = 300
	for start := 0; start < len(ids); start += maxIDsPerChunk {
		end := start + maxIDsPerChunk
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		placeholders := inClausePlaceholders(len(chunk))

		query := `
			SELECT id, job_id, COALESCE(run_id, ''), node_id, node_type, node_order, status,
			       COALESCE(node_label, ''), COALESCE(node_name, ''),
			       COALESCE(prompt, ''), COALESCE(model, ''), COALESCE(output, ''),
			       COALESCE(tokens_input, 0), COALESCE(tokens_output, 0), COALESCE(cost, 0), COALESCE(latency_ms, 0),
			       COALESCE(error_message, ''), COALESCE(error_code, ''), COALESCE(metadata, ''),
			       created_at, updated_at,
			       COALESCE(execution_uid, ''), COALESCE(attempt_number, 1),
			       COALESCE(activity_id, ''), started_at, completed_at,
			       COALESCE(parent_node_id, '')
			FROM workflow_node_executions
			WHERE job_id IN (` + placeholders + `)
			  AND run_id = COALESCE(
				NULLIF((SELECT run_id FROM jobs WHERE id = workflow_node_executions.job_id), ''),
				workflow_node_executions.job_id
			  )
			ORDER BY job_id ASC, node_order ASC
		`

		args := make([]interface{}, len(chunk))
		for i := range chunk {
			args[i] = chunk[i]
		}

		rows, err := s.db.Query(query, args...)
		if err != nil {
			return nil, fmt.Errorf("failed to list workflow nodes by job IDs: %w", err)
		}

		for rows.Next() {
			var node WorkflowNode
			if err := rows.Scan(
				&node.ID, &node.ExecutionID, &node.RunID, &node.NodeID, &node.NodeType, &node.NodeOrder, &node.Status,
				&node.NodeLabel, &node.NodeName,
				&node.Prompt, &node.Model, &node.Output, &node.TokensInput, &node.TokensOutput,
				&node.Cost, &node.LatencyMs, &node.ErrorMessage, &node.ErrorCode, &node.Metadata,
				&node.CreatedAt, &node.UpdatedAt,
				&node.ExecutionUID, &node.AttemptNumber, &node.ActivityID, &node.StartedAt, &node.CompletedAt,
				&node.ParentNodeID,
			); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("failed to scan workflow node: %w", err)
			}
			out[node.ExecutionID] = append(out[node.ExecutionID], node)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("error iterating workflow nodes: %w", err)
		}
		if err := rows.Close(); err != nil {
			log.Printf("Error closing rows: %v", err)
		}
	}

	return out, nil
}

// LLMRequestLog contains all fields for logging an LLM request.
type LLMRequestLog struct {
	JobID         string
	NodeID        string
	Model         string
	Prompt        string
	Response      string
	TokensIn      int
	TokensOut     int
	Cost          float64
	Latency       float64
	Status        string
	ErrMsg        string
	NodeLabel     string
	NodeName      string
	AttemptNumber int
	ExecutionUID  string
	ParentNodeID  string
	RunID         string
}

// LogLLMRequest logs an LLM request as a workflow node for accounting purposes
// This is called by LLM client to ensure all LLM calls are tracked
// Note: nodeID is auto-generated by the workflow executor and should already be unique
func (s *Storage) LogLLMRequest(jobID, nodeID, model, prompt, response string, tokensIn, tokensOut int, cost, latency float64, status, errMsg string) error {
	return s.LogLLMRequestFull(&LLMRequestLog{
		JobID:     jobID,
		NodeID:    nodeID,
		Model:     model,
		Prompt:    prompt,
		Response:  response,
		TokensIn:  tokensIn,
		TokensOut: tokensOut,
		Cost:      cost,
		Latency:   latency,
		Status:    status,
		ErrMsg:    errMsg,
	})
}

// LogLLMRequestFull logs an LLM request with all fields including attempt tracking
func (s *Storage) LogLLMRequestFull(l *LLMRequestLog) error {
	// If no node_id provided, generate one (fallback for non-workflow LLM calls)
	if l.NodeID == "" {
		l.NodeID = fmt.Sprintf("llm_%d", time.Now().UnixNano())
	}

	now := time.Now()

	// Default attempt number to 1
	if l.AttemptNumber == 0 {
		l.AttemptNumber = 1
	}
	l.RunID = s.resolveRunID(l.JobID, l.RunID)
	if strings.TrimSpace(l.ExecutionUID) == "" {
		l.ExecutionUID = fmt.Sprintf("%s:%s:%d", l.JobID, l.NodeID, l.AttemptNumber)
	}

	var completedAt interface{}
	if l.Status == "completed" || l.Status == "failed" || l.Status == "cancelled" {
		completedAt = now
	}

	// Attempt-level write: immutable retry history keyed by attempt.
	attemptQuery := `
		INSERT INTO workflow_node_execution_attempts (
			job_id, execution_id, run_id, node_id, node_type, attempt_number, status,
			activity_id, started_at, completed_at, latency_ms, tokens_input, tokens_output,
			cost, error_code, error_message, metadata, execution_uid, node_label, node_name,
			prompt, model, output, parent_node_id, created_at, updated_at
		) VALUES (
			?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, NULL, ?, '{}', ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?
		)
		ON CONFLICT(job_id, run_id, node_id, attempt_number) DO UPDATE SET
			status = excluded.status,
			started_at = COALESCE(workflow_node_execution_attempts.started_at, excluded.started_at),
			completed_at = COALESCE(excluded.completed_at, workflow_node_execution_attempts.completed_at),
			latency_ms = excluded.latency_ms,
			tokens_input = excluded.tokens_input,
			tokens_output = excluded.tokens_output,
			cost = excluded.cost,
			error_message = excluded.error_message,
			execution_uid = COALESCE(NULLIF(excluded.execution_uid, ''), workflow_node_execution_attempts.execution_uid),
			node_label = COALESCE(NULLIF(excluded.node_label, ''), workflow_node_execution_attempts.node_label),
			node_name = COALESCE(NULLIF(excluded.node_name, ''), workflow_node_execution_attempts.node_name),
			prompt = COALESCE(NULLIF(excluded.prompt, ''), workflow_node_execution_attempts.prompt),
			model = COALESCE(NULLIF(excluded.model, ''), workflow_node_execution_attempts.model),
			output = COALESCE(NULLIF(excluded.output, ''), workflow_node_execution_attempts.output),
			parent_node_id = COALESCE(excluded.parent_node_id, workflow_node_execution_attempts.parent_node_id),
			updated_at = excluded.updated_at
	`
	if _, err := s.db.Exec(attemptQuery,
		l.JobID, l.JobID, l.RunID, l.NodeID, "prompt", l.AttemptNumber, l.Status,
		now, completedAt, l.Latency, l.TokensIn, l.TokensOut, l.Cost, l.ErrMsg,
		l.ExecutionUID, l.NodeLabel, l.NodeName, l.Prompt, l.Model, l.Response, l.ParentNodeID, now, now,
	); err != nil {
		return fmt.Errorf("failed to upsert workflow node execution attempt: %w", err)
	}

	wfNode := &WorkflowNode{
		ExecutionID:   l.JobID,
		RunID:         l.RunID,
		NodeID:        l.NodeID,
		NodeType:      "prompt",
		Status:        l.Status,
		NodeLabel:     l.NodeLabel,
		NodeName:      l.NodeName,
		Prompt:        l.Prompt,
		Model:         l.Model,
		Output:        l.Response,
		TokensInput:   l.TokensIn,
		TokensOutput:  l.TokensOut,
		Cost:          l.Cost,
		LatencyMs:     l.Latency,
		ErrorMessage:  l.ErrMsg,
		Metadata:      "{}",
		ExecutionUID:  l.ExecutionUID,
		AttemptNumber: l.AttemptNumber,
		StartedAt:     &now,
	}
	if completedAt != nil {
		completed := now
		wfNode.CompletedAt = &completed
	}
	if l.ParentNodeID != "" {
		wfNode.ParentNodeID = l.ParentNodeID
	}
	if err := s.AddWorkflowNode(wfNode); err != nil {
		return fmt.Errorf("failed to upsert workflow node execution: %w", err)
	}

	log.Printf("LogLLMRequest UPSERT: job_id=%s node_id=%s status=%s attempt=%d run_id=%s",
		l.JobID, l.NodeID, l.Status, l.AttemptNumber, l.RunID)

	return nil
}
