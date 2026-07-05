package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// BenchmarkRunnerSession represents a persistent benchmark runner session.
type BenchmarkRunnerSession struct {
	ID               string
	Status           string
	Command          string
	Error            string
	TotalRuns        int
	CompletedRuns    int
	ImportedRuns     int
	TotalItems       int
	CompletedItems   int
	CorrectItems     int
	IncorrectItems   int
	CurrentRunID     string
	CurrentBenchmark string
	CurrentWorkflow  string
	CurrentItemID    string
	CancelRequested  bool
	StartedAt        time.Time
	FinishedAt       *time.Time
	LastHeartbeatAt  time.Time
}

// BenchmarkRunnerSessionUpdate holds optional fields for updating a session.
// nil pointers mean "don't update this field".
type BenchmarkRunnerSessionUpdate struct {
	Status           *string
	Error            *string
	CompletedRuns    *int
	ImportedRuns     *int
	CompletedItems   *int
	CorrectItems     *int
	IncorrectItems   *int
	CurrentRunID     *string
	CurrentBenchmark *string
	CurrentWorkflow  *string
	CurrentItemID    *string
	CancelRequested  *bool
	FinishedAt       *time.Time
}

// CreateBenchmarkRunnerSession inserts a new benchmark runner session record.
func (s *Storage) CreateBenchmarkRunnerSession(session *BenchmarkRunnerSession) error {
	_, err := s.db.Exec(`
		INSERT INTO benchmark_runner_sessions
		(id, status, command, total_runs, total_items, started_at, last_heartbeat_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		session.ID, session.Status, session.Command,
		session.TotalRuns, session.TotalItems,
		sqlTime(session.StartedAt), sqlTime(session.StartedAt))
	if err != nil {
		return fmt.Errorf("create benchmark runner session: %w", err)
	}
	return nil
}

// UpdateBenchmarkRunnerSession builds a dynamic UPDATE from non-nil fields.
func (s *Storage) UpdateBenchmarkRunnerSession(id string, update BenchmarkRunnerSessionUpdate) error {
	sets := []string{}
	args := []interface{}{}

	if update.Status != nil {
		sets = append(sets, "status = ?")
		args = append(args, *update.Status)
	}
	if update.Error != nil {
		sets = append(sets, "error = ?")
		args = append(args, *update.Error)
	}
	if update.CompletedRuns != nil {
		sets = append(sets, "completed_runs = ?")
		args = append(args, *update.CompletedRuns)
	}
	if update.ImportedRuns != nil {
		sets = append(sets, "imported_runs = ?")
		args = append(args, *update.ImportedRuns)
	}
	if update.CompletedItems != nil {
		sets = append(sets, "completed_items = ?")
		args = append(args, *update.CompletedItems)
	}
	if update.CorrectItems != nil {
		sets = append(sets, "correct_items = ?")
		args = append(args, *update.CorrectItems)
	}
	if update.IncorrectItems != nil {
		sets = append(sets, "incorrect_items = ?")
		args = append(args, *update.IncorrectItems)
	}
	if update.CurrentRunID != nil {
		sets = append(sets, "current_run_id = ?")
		args = append(args, *update.CurrentRunID)
	}
	if update.CurrentBenchmark != nil {
		sets = append(sets, "current_benchmark = ?")
		args = append(args, *update.CurrentBenchmark)
	}
	if update.CurrentWorkflow != nil {
		sets = append(sets, "current_workflow = ?")
		args = append(args, *update.CurrentWorkflow)
	}
	if update.CurrentItemID != nil {
		sets = append(sets, "current_item_id = ?")
		args = append(args, *update.CurrentItemID)
	}
	if update.CancelRequested != nil {
		sets = append(sets, "cancel_requested = ?")
		args = append(args, boolToInt(*update.CancelRequested))
	}
	if update.FinishedAt != nil {
		sets = append(sets, "finished_at = ?")
		args = append(args, sqlTime(*update.FinishedAt))
	}

	if len(sets) == 0 {
		return nil
	}

	now := sqlTime(time.Now())
	sets = append(sets, "last_heartbeat_at = ?", "updated_at = ?")
	args = append(args, now, now)
	args = append(args, id)

	_, err := s.db.Exec(
		"UPDATE benchmark_runner_sessions SET "+strings.Join(sets, ", ")+" WHERE id = ?",
		args...)
	if err != nil {
		return fmt.Errorf("update benchmark runner session %s: %w", id, err)
	}
	return nil
}

// GetActiveBenchmarkRunnerSession returns the most recent running session, or nil.
func (s *Storage) GetActiveBenchmarkRunnerSession() (*BenchmarkRunnerSession, error) {
	row := s.db.QueryRow(`
		SELECT id, status, command, COALESCE(error, ''),
		       total_runs, completed_runs, imported_runs,
		       total_items, completed_items, correct_items, incorrect_items,
		       COALESCE(current_run_id, ''), COALESCE(current_benchmark, ''),
		       COALESCE(current_workflow, ''), COALESCE(current_item_id, ''),
		       cancel_requested, started_at, finished_at, last_heartbeat_at
		FROM benchmark_runner_sessions
		WHERE status = 'running'
		ORDER BY started_at DESC
		LIMIT 1`)

	sess := &BenchmarkRunnerSession{}
	var cancelReq int
	var finishedAt *time.Time
	err := row.Scan(
		&sess.ID, &sess.Status, &sess.Command, &sess.Error,
		&sess.TotalRuns, &sess.CompletedRuns, &sess.ImportedRuns,
		&sess.TotalItems, &sess.CompletedItems, &sess.CorrectItems, &sess.IncorrectItems,
		&sess.CurrentRunID, &sess.CurrentBenchmark,
		&sess.CurrentWorkflow, &sess.CurrentItemID,
		&cancelReq, &sess.StartedAt, &finishedAt, &sess.LastHeartbeatAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get active benchmark runner session: %w", err)
	}
	sess.CancelRequested = cancelReq == 1
	sess.FinishedAt = finishedAt
	return sess, nil
}

// AbandonStaleRunnerSessions marks all running sessions as abandoned (e.g. after server restart).
func (s *Storage) AbandonStaleRunnerSessions() (int, error) {
	result, err := s.db.Exec(`
		UPDATE benchmark_runner_sessions
		SET status = 'abandoned', error = 'server restarted', updated_at = ?
		WHERE status = 'running'`, sqlTime(time.Now()))
	if err != nil {
		return 0, fmt.Errorf("abandon stale runner sessions: %w", err)
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}
