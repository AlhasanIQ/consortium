package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"
)

// ActivityResult represents a durable, idempotent activity outcome.
// Once written with a terminal status, the result is immutable.
type ActivityResult struct {
	ID             int64      `json:"id"`
	IdempotencyKey string     `json:"idempotency_key"`
	RunID          string     `json:"run_id"`
	NodeID         string     `json:"node_id"`
	Attempt        int        `json:"attempt"`
	ActivityType   string     `json:"activity_type"`
	Status         string     `json:"status"`
	OutputPayload  string     `json:"output_payload,omitempty"`
	ErrorCode      string     `json:"error_code,omitempty"`
	ErrorMessage   string     `json:"error_message,omitempty"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	Checksum       string     `json:"checksum,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// GetActivityResult retrieves an activity result by idempotency key.
// Returns nil, nil if not found.
func (s *Storage) GetActivityResult(ctx context.Context, idempotencyKey string) (*ActivityResult, error) {
	query := `
		SELECT id, idempotency_key, run_id, node_id, attempt, activity_type, status,
		       COALESCE(output_payload, ''), COALESCE(error_code, ''), COALESCE(error_message, ''),
		       started_at, completed_at, COALESCE(checksum, ''), created_at
		FROM activity_results
		WHERE idempotency_key = ?
	`
	r := &ActivityResult{}
	err := s.db.QueryRowContext(ctx, query, idempotencyKey).Scan(
		&r.ID, &r.IdempotencyKey, &r.RunID, &r.NodeID, &r.Attempt,
		&r.ActivityType, &r.Status, &r.OutputPayload,
		&r.ErrorCode, &r.ErrorMessage,
		&r.StartedAt, &r.CompletedAt, &r.Checksum, &r.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get activity result: %w", err)
	}
	return r, nil
}

// SaveActivityResult persists an activity result. If a result with the same
// idempotency key already exists, it returns without error (idempotent write).
func (s *Storage) SaveActivityResult(ctx context.Context, result *ActivityResult) error {
	var outputPayload, errorCode, errorMessage, checksum interface{}
	if result.OutputPayload != "" {
		outputPayload = result.OutputPayload
	}
	if result.ErrorCode != "" {
		errorCode = result.ErrorCode
	}
	if result.ErrorMessage != "" {
		errorMessage = result.ErrorMessage
	}
	if result.Checksum != "" {
		checksum = result.Checksum
	}

	now := time.Now()
	res, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO activity_results
			(idempotency_key, run_id, node_id, attempt, activity_type, status,
			 output_payload, error_code, error_message, started_at, completed_at, checksum, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, result.IdempotencyKey, result.RunID, result.NodeID, result.Attempt,
		result.ActivityType, result.Status, outputPayload, errorCode, errorMessage,
		result.StartedAt, result.CompletedAt, checksum, now)
	if err != nil {
		return fmt.Errorf("failed to save activity result: %w", err)
	}

	// If INSERT OR IGNORE was a no-op (row already exists), validate that the
	// existing status matches. A mismatch indicates a potential replay divergence.
	rows, _ := res.RowsAffected()
	if rows == 0 {
		existing, fetchErr := s.GetActivityResult(ctx, result.IdempotencyKey)
		if fetchErr == nil && existing != nil && existing.Status != result.Status {
			log.Printf("Warning: activity result idempotency conflict for key %s: existing status %q, attempted status %q",
				result.IdempotencyKey, existing.Status, result.Status)
		}
	}
	return nil
}

// MarshalActivityOutput serializes an activity output to JSON for storage.
func MarshalActivityOutput(output interface{}) (string, error) {
	data, err := json.Marshal(output)
	if err != nil {
		return "", fmt.Errorf("failed to marshal activity output: %w", err)
	}
	return string(data), nil
}
