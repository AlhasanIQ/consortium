package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// UpsertExecutionReplayRequest persists an optional replay request JSON payload
// for a job. Empty payloads are ignored.
func (s *Storage) UpsertExecutionReplayRequest(ctx context.Context, jobID, replayJSON string) error {
	jobID = strings.TrimSpace(jobID)
	replayJSON = strings.TrimSpace(replayJSON)
	if jobID == "" {
		return fmt.Errorf("job_id is required")
	}
	if replayJSON == "" {
		return nil
	}

	now := time.Now()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO execution_replay_requests (job_id, replay_request, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(job_id) DO UPDATE SET
			replay_request = excluded.replay_request,
			updated_at = excluded.updated_at
	`, jobID, replayJSON, now, now)
	if err != nil {
		return fmt.Errorf("upsert execution replay request: %w", err)
	}
	return nil
}

// GetExecutionReplayRequest returns the replay request JSON payload for a job.
// Returns empty string and nil when no replay request exists.
func (s *Storage) GetExecutionReplayRequest(ctx context.Context, jobID string) (string, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return "", fmt.Errorf("job_id is required")
	}

	var replayJSON string
	err := s.db.QueryRowContext(ctx, `
		SELECT replay_request
		FROM execution_replay_requests
		WHERE job_id = ?
	`, jobID).Scan(&replayJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get execution replay request: %w", err)
	}
	return replayJSON, nil
}
