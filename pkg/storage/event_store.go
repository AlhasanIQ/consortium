package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/typeconv"
)

// Ensure Storage implements events.EventStore
var _ events.EventStore = (*Storage)(nil)

// AppendEvent atomically appends an event and increments the job's sequence counter.
// This uses a transaction to ensure atomicity of the sequence number assignment.
func (s *Storage) AppendEvent(ctx context.Context, jobID string, eventType string, payload map[string]interface{}) (*events.EventEnvelope, error) {
	return s.AppendEventWithDetails(ctx, jobID, eventType, "", "", "", "", payload)
}

// AppendEventWithDetails appends an event with additional details.
// All writes are atomic: sequence increment + event insert happen in one transaction.
func (s *Storage) AppendEventWithDetails(ctx context.Context, jobID, eventType, nodeID, message, errMsg, code string, payload map[string]interface{}) (*events.EventEnvelope, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		// Ignore rollback errors - transaction may already be committed
		_ = tx.Rollback()
	}()

	// Atomically increment last_event_sequence and get the new value
	var seq int64
	err = tx.QueryRowContext(ctx, `
		UPDATE jobs
		SET last_event_sequence = last_event_sequence + 1, updated_at = ?
		WHERE id = ?
		RETURNING last_event_sequence
	`, time.Now(), jobID).Scan(&seq)
	if err != nil {
		return nil, fmt.Errorf("failed to increment sequence: %w", err)
	}

	// Marshal payload to JSON
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	now := time.Now()

	// Extract typed correlation fields for indexed timeline queries.
	if nodeID == "" {
		if v, ok := payload["node_id"].(string); ok {
			nodeID = v
		}
	}
	executionID := typeconv.AsString(payload["execution_id"])
	runID := typeconv.AsString(payload["run_id"])
	agentRunID := typeconv.AsString(payload["agent_run_id"])
	iteration := typeconv.AsNullableInt(payload["iteration"])

	// Insert the event
	_, err = tx.ExecContext(ctx, `
		INSERT INTO job_events (
			version, job_id, sequence, event_type, execution_id, run_id, node_id, agent_run_id, iteration,
			timestamp, payload, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, events.CurrentEventVersion, jobID, seq, eventType, nullIfEmpty(executionID), nullIfEmpty(runID), nullIfEmpty(nodeID), nullIfEmpty(agentRunID), iteration, now, string(payloadJSON), now)
	if err != nil {
		return nil, fmt.Errorf("failed to insert event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Create the envelope to return
	envelope := &events.EventEnvelope{
		Version:     events.CurrentEventVersion,
		JobID:       jobID,
		Sequence:    seq,
		Type:        eventType,
		Timestamp:   now,
		Payload:     payload,
		NodeID:      nodeID,
		Message:     message,
		Error:       errMsg,
		Code:        code,
		ExecutionID: executionID,
		RunID:       runID,
		AgentRunID:  agentRunID,
	}
	if iteration != nil {
		envelope.Iteration = *iteration
	}

	return envelope, nil
}

// GetEventsAfter returns events with sequence > afterSequence, ordered by sequence ascending.
func (s *Storage) GetEventsAfter(ctx context.Context, jobID string, afterSequence int64) ([]*events.EventEnvelope, error) {
	query := `
		SELECT version, job_id, sequence, event_type,
		       COALESCE(execution_id, ''), COALESCE(run_id, ''),
		       COALESCE(node_id, ''), COALESCE(agent_run_id, ''),
		       iteration, timestamp, payload
		FROM job_events
		WHERE job_id = ? AND sequence > ?
		ORDER BY sequence ASC
	`

	rows, err := s.db.QueryContext(ctx, query, jobID, afterSequence)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var envelopes []*events.EventEnvelope
	for rows.Next() {
		var env events.EventEnvelope
		var payloadJSON string
		var executionID, runID, nodeID, agentRunID string
		var iteration sql.NullInt64
		err := rows.Scan(
			&env.Version, &env.JobID, &env.Sequence, &env.Type,
			&executionID, &runID, &nodeID, &agentRunID, &iteration,
			&env.Timestamp, &payloadJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}
		env.ExecutionID = executionID
		env.RunID = runID
		env.NodeID = nodeID
		env.AgentRunID = agentRunID
		if iteration.Valid {
			env.Iteration = int(iteration.Int64)
		}

		// Parse payload JSON
		if err := json.Unmarshal([]byte(payloadJSON), &env.Payload); err != nil {
			// If payload is invalid, use empty map
			env.Payload = make(map[string]interface{})
		}

		// Extract common fields from payload if present
		if env.NodeID == "" {
			if nodeID, ok := env.Payload["node_id"].(string); ok {
				env.NodeID = nodeID
			}
		}
		if env.ExecutionID == "" {
			if v, ok := env.Payload["execution_id"].(string); ok {
				env.ExecutionID = v
			}
		}
		if env.RunID == "" {
			if v, ok := env.Payload["run_id"].(string); ok {
				env.RunID = v
			}
		}
		if env.AgentRunID == "" {
			if v, ok := env.Payload["agent_run_id"].(string); ok {
				env.AgentRunID = v
			}
		}
		if env.Iteration == 0 {
			if iterVal := typeconv.AsNullableInt(env.Payload["iteration"]); iterVal != nil {
				env.Iteration = *iterVal
			}
		}
		if message, ok := env.Payload["message"].(string); ok {
			env.Message = message
		}
		if errMsg, ok := env.Payload["error"].(string); ok {
			env.Error = errMsg
		}
		if code, ok := env.Payload["code"].(string); ok {
			env.Code = code
		}

		envelopes = append(envelopes, &env)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating events: %w", err)
	}

	return envelopes, nil
}

func nullIfEmpty(v string) interface{} {
	if v == "" {
		return nil
	}
	return v
}

// GetLatestSequence returns the current last_event_sequence for a job.
func (s *Storage) GetLatestSequence(ctx context.Context, jobID string) (int64, error) {
	var seq int64
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(last_event_sequence, 0) FROM jobs WHERE id = ?
	`, jobID).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get latest sequence: %w", err)
	}
	return seq, nil
}

// GetEventCount returns the total number of events for a job.
func (s *Storage) GetEventCount(ctx context.Context, jobID string) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM job_events WHERE job_id = ?
	`, jobID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count events: %w", err)
	}
	return count, nil
}

// GetJobSnapshot returns a snapshot of job state for WebSocket reconnection.
// Includes job details and all nodes.
type JobSnapshot struct {
	SnapshotSequence int64              `json:"snapshot_sequence"`
	Job              *WorkflowExecution `json:"job"`
	Nodes            []WorkflowNode     `json:"nodes"`
	FinalOutput      string             `json:"final_output,omitempty"`
}

// GetJobSnapshot retrieves a complete snapshot of a job for reconnecting clients.
func (s *Storage) GetJobSnapshot(ctx context.Context, jobID string) (*JobSnapshot, error) {
	job, err := s.GetExecution(jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to get job: %w", err)
	}

	nodes, err := s.GetWorkflowNodes(jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to get nodes: %w", err)
	}

	return &JobSnapshot{
		SnapshotSequence: job.LastEventSequence,
		Job:              job,
		Nodes:            nodes,
		FinalOutput:      job.ResultText,
	}, nil
}

// --- Outbox Storage Methods ---

// OutboxEntry represents a pending side effect in the outbox
type OutboxEntry struct {
	ID            int                    `json:"id"`
	ToolCallID    string                 `json:"tool_call_id"`
	JobID         string                 `json:"job_id"`
	NodeID        string                 `json:"node_id"`
	ToolType      string                 `json:"tool_type"`
	Payload       map[string]interface{} `json:"payload"`
	Status        string                 `json:"status"`
	Attempts      int                    `json:"attempts"`
	MaxAttempts   int                    `json:"max_attempts"`
	NextAttemptAt time.Time              `json:"next_attempt_at"`
	LastError     string                 `json:"last_error"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
}

// CreateOutboxEntry inserts a new outbox entry
func (s *Storage) CreateOutboxEntry(ctx context.Context, entry *OutboxEntry) error {
	payloadJSON, err := json.Marshal(entry.Payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	now := time.Now()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO side_effect_outbox (
			tool_call_id, job_id, node_id, payload, status,
			attempts, max_attempts, next_attempt_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, 'pending', 0, ?, ?, ?, ?)
	`, entry.ToolCallID, entry.JobID, entry.NodeID, string(payloadJSON),
		entry.MaxAttempts, now, now, now)

	if err != nil {
		return fmt.Errorf("failed to insert outbox entry: %w", err)
	}

	return nil
}

// GetPendingOutboxEntries returns entries ready for processing
func (s *Storage) GetPendingOutboxEntries(ctx context.Context, limit int) ([]OutboxEntry, error) {
	query := `
		SELECT id, tool_call_id, job_id, node_id, payload, status,
		       attempts, max_attempts, next_attempt_at, COALESCE(last_error, ''),
		       created_at, updated_at
		FROM side_effect_outbox
		WHERE status = 'pending' AND next_attempt_at <= ?
		ORDER BY created_at ASC
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, time.Now(), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending outbox entries: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var entries []OutboxEntry
	for rows.Next() {
		var entry OutboxEntry
		var payloadJSON string
		err := rows.Scan(
			&entry.ID, &entry.ToolCallID, &entry.JobID, &entry.NodeID,
			&payloadJSON, &entry.Status, &entry.Attempts, &entry.MaxAttempts,
			&entry.NextAttemptAt, &entry.LastError, &entry.CreatedAt, &entry.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan outbox entry: %w", err)
		}

		// Parse payload JSON
		if err := json.Unmarshal([]byte(payloadJSON), &entry.Payload); err != nil {
			entry.Payload = make(map[string]interface{})
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// UpdateOutboxStatus updates the status of an outbox entry
func (s *Storage) UpdateOutboxStatus(ctx context.Context, id int, status, lastError string, nextAttemptAt *time.Time) error {
	var err error
	if nextAttemptAt != nil {
		_, err = s.db.ExecContext(ctx, `
			UPDATE side_effect_outbox
			SET status = ?, last_error = ?, next_attempt_at = ?, updated_at = ?
			WHERE id = ?
		`, status, lastError, *nextAttemptAt, time.Now(), id)
	} else {
		_, err = s.db.ExecContext(ctx, `
			UPDATE side_effect_outbox
			SET status = ?, last_error = ?, updated_at = ?
			WHERE id = ?
		`, status, lastError, time.Now(), id)
	}

	if err != nil {
		return fmt.Errorf("failed to update outbox status: %w", err)
	}
	return nil
}

// IncrementOutboxAttempts increments the attempt counter and updates next_attempt_at
func (s *Storage) IncrementOutboxAttempts(ctx context.Context, id int, nextAttemptAt time.Time, lastError string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE side_effect_outbox
		SET attempts = attempts + 1, status = 'pending',
		    next_attempt_at = ?, last_error = ?, updated_at = ?
		WHERE id = ?
	`, nextAttemptAt, lastError, time.Now(), id)

	if err != nil {
		return fmt.Errorf("failed to increment outbox attempts: %w", err)
	}
	return nil
}

// DeleteCompletedOutboxEntries removes completed entries older than the given duration
func (s *Storage) DeleteCompletedOutboxEntries(ctx context.Context, olderThan time.Duration) (int, error) {
	cutoff := time.Now().Add(-olderThan)
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM side_effect_outbox
		WHERE status = 'completed' AND updated_at < ?
	`, cutoff)

	if err != nil {
		return 0, fmt.Errorf("failed to delete completed outbox entries: %w", err)
	}

	count, _ := result.RowsAffected()
	return int(count), nil
}
