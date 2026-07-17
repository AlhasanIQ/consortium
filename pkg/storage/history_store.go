package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// HistoryEvent mirrors runtime.HistoryEvent for the storage layer.
type HistoryEvent struct {
	ID         int64                  `json:"id"`
	RunID      string                 `json:"run_id"`
	Sequence   int64                  `json:"sequence"`
	Type       string                 `json:"type"`
	NodeID     string                 `json:"node_id,omitempty"`
	ActivityID string                 `json:"activity_id,omitempty"`
	Timestamp  time.Time              `json:"timestamp"`
	Attributes map[string]interface{} `json:"attributes,omitempty"`
}

// AppendHistoryEvent atomically assigns a monotonic sequence number and inserts
// a history event for the given run using one SQL statement.
//
// The sequence is computed as MAX(sequence)+1 within the run, starting at 1.
// A retry loop handles transient SQLite errors (busy/locked) and UNIQUE
// constraint violations on the (run_id, sequence) pair that can occur under
// high concurrency.
func (s *Storage) AppendHistoryEvent(ctx context.Context, event *HistoryEvent) error {
	// Marshal attributes
	attrsJSON := "{}"
	if event.Attributes != nil {
		data, err := json.Marshal(event.Attributes)
		if err != nil {
			return fmt.Errorf("failed to marshal attributes: %w", err)
		}
		attrsJSON = string(data)
	}

	var nodeID, activityID interface{}
	if event.NodeID != "" {
		nodeID = event.NodeID
	}
	if event.ActivityID != "" {
		activityID = event.ActivityID
	}

	now := time.Now()
	if event.Timestamp.IsZero() {
		event.Timestamp = now
	}

	// Avoid explicit BEGIN/COMMIT here. Under high-concurrency durable workloads
	// with a single SQLite connection, starting many transactions can trigger
	// "cannot start a transaction within a transaction". This single-statement
	// INSERT computes next per-run sequence atomically.
	//
	// Retry up to 3 times on transient SQLite errors or UNIQUE constraint
	// violations (sequence collision under concurrent appends).
	const maxAttempts = 3
	backoffs := []time.Duration{5 * time.Millisecond, 10 * time.Millisecond}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		err := s.db.QueryRowContext(ctx, `
			INSERT INTO execution_history (run_id, sequence, event_type, node_id, activity_id, timestamp, attributes, created_at)
			SELECT ?, COALESCE(MAX(sequence), 0) + 1, ?, ?, ?, ?, ?, ?
			FROM execution_history
			WHERE run_id = ?
			RETURNING sequence
		`, event.RunID, string(event.Type), nodeID, activityID, event.Timestamp, attrsJSON, now, event.RunID).Scan(&event.Sequence)
		if err == nil {
			return nil
		}

		retryable := IsRetryableSQLiteError(err) || isUniqueConstraintError(err)
		if !retryable || attempt >= maxAttempts-1 || ctx.Err() != nil {
			return fmt.Errorf("failed to insert history event: %w", err)
		}
		if !sleepWithContext(ctx, backoffs[attempt]) {
			return fmt.Errorf("failed to insert history event: %w", ctx.Err())
		}
	}

	return fmt.Errorf("failed to insert history event after %d attempts", maxAttempts)
}

// AppendHistoryEventBatch atomically appends multiple history events in a single
// transaction, ensuring monotonic sequence assignment across the batch.
// If len(events) <= 1, delegates to AppendHistoryEvent for the simpler path.
func (s *Storage) AppendHistoryEventBatch(ctx context.Context, events []*HistoryEvent) error {
	if len(events) == 0 {
		return nil
	}
	if len(events) == 1 {
		return s.AppendHistoryEvent(ctx, events[0])
	}

	return retryHistoryBatch(ctx, func() (bool, error) {
		return s.appendHistoryEventBatchOnce(ctx, events)
	})
}

func retryHistoryBatch(ctx context.Context, attempt func() (bool, error)) error {
	const maxAttempts = 3
	backoffs := []time.Duration{5 * time.Millisecond, 10 * time.Millisecond}

	for i := 0; i < maxAttempts; i++ {
		retryable, err := attempt()
		if err == nil {
			return nil
		}
		if !retryable || i >= maxAttempts-1 || ctx.Err() != nil {
			return err
		}
		if !sleepWithContext(ctx, backoffs[i]) {
			return ctx.Err()
		}
	}
	return nil
}

func (s *Storage) appendHistoryEventBatchOnce(ctx context.Context, events []*HistoryEvent) (bool, error) {
	const maxSingleStatementBatchSize = 400
	if len(events) > maxSingleStatementBatchSize {
		return s.appendHistoryEventBatchTransaction(ctx, events)
	}
	if events[0] == nil {
		return false, fmt.Errorf("history batch event 0 is nil")
	}
	runID := events[0].RunID
	var query strings.Builder
	query.Grow(256 + len(events)*128)
	query.WriteString(`
		WITH next_sequence AS (
			SELECT COALESCE(MAX(sequence), 0) AS base
			FROM execution_history
			WHERE run_id = ?
		)
		INSERT INTO execution_history
			(run_id, sequence, event_type, node_id, activity_id, timestamp, attributes, created_at)
	`)
	args := make([]interface{}, 0, 1+len(events)*8)
	args = append(args, runID)

	for i, event := range events {
		if event == nil {
			return false, fmt.Errorf("history batch event %d is nil", i)
		}
		if event.RunID != runID {
			return false, fmt.Errorf("history batch contains multiple run IDs: %q and %q", runID, event.RunID)
		}

		attrsJSON := "{}"
		if event.Attributes != nil {
			data, marshalErr := json.Marshal(event.Attributes)
			if marshalErr != nil {
				return false, fmt.Errorf("failed to marshal attributes: %w", marshalErr)
			}
			attrsJSON = string(data)
		}

		var nodeID, activityID interface{}
		if event.NodeID != "" {
			nodeID = event.NodeID
		}
		if event.ActivityID != "" {
			activityID = event.ActivityID
		}

		now := time.Now()
		if event.Timestamp.IsZero() {
			event.Timestamp = now
		}

		if i > 0 {
			query.WriteString(" UNION ALL ")
		}
		query.WriteString("SELECT ?, base + ?, ?, ?, ?, ?, ?, ? FROM next_sequence")
		args = append(args,
			event.RunID, i+1, string(event.Type), nodeID, activityID,
			event.Timestamp, attrsJSON, now,
		)
	}
	query.WriteString(" RETURNING sequence")

	rows, err := s.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		retryable := IsRetryableSQLiteError(err) || isUniqueConstraintError(err)
		return retryable, fmt.Errorf("failed to insert history batch: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var firstSequence int64
	count := 0
	for rows.Next() {
		var sequence int64
		if err := rows.Scan(&sequence); err != nil {
			return false, fmt.Errorf("failed to scan inserted history sequence: %w", err)
		}
		if count == 0 || sequence < firstSequence {
			firstSequence = sequence
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return IsRetryableSQLiteError(err), fmt.Errorf("failed to insert history batch: %w", err)
	}
	if count != len(events) {
		return false, fmt.Errorf("inserted %d history events, expected %d", count, len(events))
	}
	for i, event := range events {
		event.Sequence = firstSequence + int64(i)
	}

	return false, nil
}

func (s *Storage) appendHistoryEventBatchTransaction(ctx context.Context, events []*HistoryEvent) (bool, error) {
	if events[0] == nil {
		return false, fmt.Errorf("history batch event 0 is nil")
	}
	runID := events[0].RunID
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return IsRetryableSQLiteError(err), fmt.Errorf("failed to begin batch history tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for i, event := range events {
		if event == nil {
			return false, fmt.Errorf("history batch event %d is nil", i)
		}
		if event.RunID != runID {
			return false, fmt.Errorf("history batch contains multiple run IDs: %q and %q", runID, event.RunID)
		}

		attrsJSON := "{}"
		if event.Attributes != nil {
			data, marshalErr := json.Marshal(event.Attributes)
			if marshalErr != nil {
				return false, fmt.Errorf("failed to marshal attributes: %w", marshalErr)
			}
			attrsJSON = string(data)
		}

		var nodeID, activityID interface{}
		if event.NodeID != "" {
			nodeID = event.NodeID
		}
		if event.ActivityID != "" {
			activityID = event.ActivityID
		}

		now := time.Now()
		if event.Timestamp.IsZero() {
			event.Timestamp = now
		}

		err := tx.QueryRowContext(ctx, `
			INSERT INTO execution_history (run_id, sequence, event_type, node_id, activity_id, timestamp, attributes, created_at)
			SELECT ?, COALESCE(MAX(sequence), 0) + 1, ?, ?, ?, ?, ?, ?
			FROM execution_history
			WHERE run_id = ?
			RETURNING sequence
		`, event.RunID, string(event.Type), nodeID, activityID, event.Timestamp, attrsJSON, now, event.RunID).Scan(&event.Sequence)
		if err != nil {
			retryable := IsRetryableSQLiteError(err) || isUniqueConstraintError(err)
			return retryable, fmt.Errorf("failed to insert batch history event: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return IsRetryableSQLiteError(err), fmt.Errorf("failed to commit batch history tx: %w", err)
	}
	return false, nil
}

// GetHistoryEvents retrieves all history events for a run, ordered by sequence.
func (s *Storage) GetHistoryEvents(ctx context.Context, runID string) ([]*HistoryEvent, error) {
	return s.GetHistoryEventsAfter(ctx, runID, 0)
}

// GetHistoryEventsAfter retrieves history events with sequence > afterSeq.
func (s *Storage) GetHistoryEventsAfter(ctx context.Context, runID string, afterSeq int64) ([]*HistoryEvent, error) {
	query := `
		SELECT id, run_id, sequence, event_type,
		       COALESCE(node_id, ''), COALESCE(activity_id, ''),
		       timestamp, COALESCE(attributes, '{}')
		FROM execution_history
		WHERE run_id = ? AND sequence > ?
		ORDER BY sequence ASC
	`
	rows, err := s.db.QueryContext(ctx, query, runID, afterSeq)
	if err != nil {
		return nil, fmt.Errorf("failed to query history events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var events []*HistoryEvent
	for rows.Next() {
		e := &HistoryEvent{}
		var attrsJSON string
		err := rows.Scan(&e.ID, &e.RunID, &e.Sequence, &e.Type,
			&e.NodeID, &e.ActivityID, &e.Timestamp, &attrsJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to scan history event: %w", err)
		}
		if attrsJSON != "" && attrsJSON != "{}" {
			if err := json.Unmarshal([]byte(attrsJSON), &e.Attributes); err != nil {
				return nil, fmt.Errorf("failed to unmarshal attributes: %w", err)
			}
		}
		events = append(events, e)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating history events: %w", err)
	}
	return events, nil
}

// GetHistoryEventCount returns the total number of events for a run.
func (s *Storage) GetHistoryEventCount(ctx context.Context, runID string) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM execution_history WHERE run_id = ?
	`, runID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count history events: %w", err)
	}
	return count, nil
}

// GetHistorySize returns the total bytes of attributes JSON for a run (for budget tracking).
func (s *Storage) GetHistorySize(ctx context.Context, runID string) (int64, error) {
	var size sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT SUM(LENGTH(attributes)) FROM execution_history WHERE run_id = ?
	`, runID).Scan(&size)
	if err != nil {
		return 0, fmt.Errorf("failed to get history size: %w", err)
	}
	if !size.Valid {
		return 0, nil
	}
	return size.Int64, nil
}
