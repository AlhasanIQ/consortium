package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/pkg/events"
)

type durableQueueScope string

const (
	durableQueueScopeAny   durableQueueScope = "any"
	durableQueueScopeRoot  durableQueueScope = "root"
	durableQueueScopeChild durableQueueScope = "child"
)

// CountDurableActiveJobs returns the number of durable root executions that are
// currently pending or running.
//
// Child executions are excluded because parent jobs already account for
// admission capacity and children bypass submission-time admission.
func (s *Storage) CountDurableActiveJobs(ctx context.Context) (int, error) {
	pending, err := s.countDurableJobs(ctx, events.JobStatusPending, durableQueueScopeRoot)
	if err != nil {
		return 0, fmt.Errorf("count durable active jobs: %w", err)
	}
	running, err := s.countDurableJobs(ctx, events.JobStatusRunning, durableQueueScopeRoot)
	if err != nil {
		return 0, fmt.Errorf("count durable active jobs: %w", err)
	}
	return pending + running, nil
}

// ClaimPendingDurableJob atomically sets the status of the oldest pending
// durable job (has dag_hash) to 'running' and returns it. Returns nil if no
// jobs are available. Single-process safe via SQLite transaction serialization.
func (s *Storage) ClaimPendingDurableJob(ctx context.Context) (*WorkflowExecution, error) {
	return s.claimPendingDurableJob(ctx, durableQueueScopeAny)
}

// ClaimPendingDurableRootJob atomically claims the oldest pending durable root
// job (no parent_execution_id).
func (s *Storage) ClaimPendingDurableRootJob(ctx context.Context) (*WorkflowExecution, error) {
	return s.claimPendingDurableJob(ctx, durableQueueScopeRoot)
}

// ClaimPendingDurableChildJob atomically claims the oldest pending durable child
// job (has parent_execution_id).
func (s *Storage) ClaimPendingDurableChildJob(ctx context.Context) (*WorkflowExecution, error) {
	return s.claimPendingDurableJob(ctx, durableQueueScopeChild)
}

func (s *Storage) claimPendingDurableJob(ctx context.Context, scope durableQueueScope) (*WorkflowExecution, error) {
	const maxClaimAttempts = 4
	backoff := 10 * time.Millisecond

	for attempt := 1; attempt <= maxClaimAttempts; attempt++ {
		var id string
		err := s.db.QueryRowContext(ctx, pendingDurableJobSelectSQL(scope)).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("select pending durable job: %w", err)
		}

		var claimedID string
		err = s.db.QueryRowContext(ctx, `
			UPDATE jobs
			SET status = 'running', updated_at = ?
			WHERE id = ? AND status = 'pending'
			RETURNING id
		`, time.Now(), id).Scan(&claimedID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			if attempt < maxClaimAttempts && IsRetryableSQLiteError(err) && ctx.Err() == nil {
				if !sleepWithContext(ctx, backoff) {
					return nil, ctx.Err()
				}
				backoff *= 2
				continue
			}
			return nil, fmt.Errorf("claim pending durable job: %w", err)
		}

		return s.GetExecution(claimedID)
	}

	return nil, nil
}

// CountPendingDurableJobs returns the number of pending durable jobs
// (both root and child).
func (s *Storage) CountPendingDurableJobs(ctx context.Context) (int, error) {
	root, err := s.countDurableJobs(ctx, events.JobStatusPending, durableQueueScopeRoot)
	if err != nil {
		return 0, fmt.Errorf("count pending durable jobs: %w", err)
	}
	child, err := s.countDurableJobs(ctx, events.JobStatusPending, durableQueueScopeChild)
	if err != nil {
		return 0, fmt.Errorf("count pending durable jobs: %w", err)
	}
	return root + child, nil
}

// ListResumableDurableRunningJobs returns running durable jobs (have dag_hash)
// that are not in the excludeIDs set. Used for startup recovery — jobs left
// running after a crash can be resumed by a new manager instance.
//
// Single-process assumption: the excludeIDs represent jobs currently held by
// in-process workers. Multi-process would need advisory locks or external
// coordination.
func (s *Storage) ListResumableDurableRunningJobs(ctx context.Context, excludeIDs []string, limit int) ([]*WorkflowExecution, error) {
	query := `
		SELECT id, query, model, status,
		       COALESCE(request_data, ''), COALESCE(response_data, ''), COALESCE(result_text, ''),
		       tokens_input, tokens_output, tokens_total, cost, COALESCE(error_message, ''),
		       retry_count, COALESCE(workflow_id, ''), COALESCE(parent_execution_id, ''),
		       COALESCE(idempotency_key, ''), COALESCE(request_hash, ''),
		       created_at, updated_at, archived_at,
		       COALESCE(last_event_sequence, 0),
		       COALESCE(config_hash, ''),
		       COALESCE(user_id, ''),
		       COALESCE(workflow_execution_id, ''),
		       COALESCE(run_id, ''),
		       COALESCE(run_number, 1),
		       COALESCE(previous_run_id, ''),
		       COALESCE(dag_snapshot, ''),
		       COALESCE(dag_hash, '')
		FROM jobs INDEXED BY idx_jobs_running_durable_created
		WHERE status = 'running' AND dag_hash IS NOT NULL AND dag_hash != ''
	`
	args := []interface{}{}

	if len(excludeIDs) > 0 {
		placeholders := strings.Repeat("?,", len(excludeIDs))
		placeholders = placeholders[:len(placeholders)-1]
		query += " AND id NOT IN (" + placeholders + ")"
		for _, id := range excludeIDs {
			args = append(args, id)
		}
	}

	query += " ORDER BY created_at ASC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query resumable: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []*WorkflowExecution
	for rows.Next() {
		exec := &WorkflowExecution{}
		if err := rows.Scan(
			&exec.ID, &exec.Description, &exec.Model, &exec.Status, &exec.RequestData,
			&exec.ResponseData, &exec.ResultText, &exec.TokensInput, &exec.TokensOutput,
			&exec.TokensTotal, &exec.Cost, &exec.ErrorMessage, &exec.RetryCount,
			&exec.WorkflowID, &exec.ParentExecutionID,
			&exec.IdempotencyKey, &exec.RequestHash,
			&exec.CreatedAt, &exec.UpdatedAt, &exec.ArchivedAt,
			&exec.LastEventSequence,
			&exec.ConfigHash,
			&exec.UserID,
			&exec.WorkflowExecutionID,
			&exec.RunID,
			&exec.RunNumber,
			&exec.PreviousRunID,
			&exec.DAGSnapshot,
			&exec.DAGHash,
		); err != nil {
			return nil, fmt.Errorf("scan resumable execution: %w", err)
		}
		results = append(results, exec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate resumable executions: %w", err)
	}

	return results, nil
}

func pendingDurableJobSelectSQL(scope durableQueueScope) string {
	return fmt.Sprintf(`
		SELECT id
		FROM jobs INDEXED BY %s
		WHERE %s
		ORDER BY created_at ASC, id ASC
		LIMIT 1
	`, durableQueueIndex(events.JobStatusPending, scope), durableQueueWhere(events.JobStatusPending, scope))
}

func durableJobCountSQL(status string, scope durableQueueScope) string {
	return fmt.Sprintf(`
		SELECT COUNT(*)
		FROM jobs INDEXED BY %s
		WHERE %s
	`, durableQueueIndex(status, scope), durableQueueWhere(status, scope))
}

func (s *Storage) countDurableJobs(ctx context.Context, status string, scope durableQueueScope) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, durableJobCountSQL(status, scope)).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func durableQueueIndex(status string, scope durableQueueScope) string {
	switch status {
	case events.JobStatusPending:
		switch scope {
		case durableQueueScopeRoot:
			return "idx_jobs_pending_durable_root_created"
		case durableQueueScopeChild:
			return "idx_jobs_pending_durable_child_created"
		default:
			return "idx_jobs_pending_durable_created"
		}
	case events.JobStatusRunning:
		return "idx_jobs_running_durable_created"
	default:
		return "idx_jobs_status"
	}
}

func durableQueueWhere(status string, scope durableQueueScope) string {
	parts := []string{
		fmt.Sprintf("status = '%s'", status),
		"dag_hash IS NOT NULL",
		"dag_hash != ''",
	}
	switch scope {
	case durableQueueScopeRoot:
		parts = append(parts, "(parent_execution_id IS NULL OR parent_execution_id = '')")
	case durableQueueScopeChild:
		parts = append(parts, "parent_execution_id IS NOT NULL", "parent_execution_id != ''")
	}
	return strings.Join(parts, " AND ")
}

// ListExecutionIDsByStatuses returns execution IDs whose status is in the provided set.
// Results are ordered oldest-first to preserve queue semantics for bulk operations.
func (s *Storage) ListExecutionIDsByStatuses(ctx context.Context, statuses []string) ([]string, error) {
	clean := sanitizeStatuses(statuses)
	if len(clean) == 0 {
		return []string{}, nil
	}

	query, args := buildStatusQueryArgs(`
		SELECT id
		FROM jobs
		WHERE status IN (%s)
		ORDER BY created_at ASC
	`, clean)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list execution ids by statuses: %w", err)
	}
	defer func() {
		_ = rows.Close()
	}()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan execution id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate execution ids: %w", err)
	}

	return ids, nil
}

// TransitionExecutionStatuses atomically moves jobs from one set of statuses to another.
// When errorMessage is non-nil, it overwrites error_message for matched jobs.
func (s *Storage) TransitionExecutionStatuses(ctx context.Context, fromStatuses []string, toStatus string, errorMessage *string) (int, error) {
	clean := sanitizeStatuses(fromStatuses)
	if len(clean) == 0 {
		return 0, nil
	}

	const maxAttempts = 4
	backoff := 10 * time.Millisecond
	now := time.Now()

	statusClause := strings.TrimRight(strings.Repeat("?,", len(clean)), ",")
	setClause := "status = ?, updated_at = ?"
	args := make([]interface{}, 0, len(clean)+4)
	args = append(args, toStatus, now)
	if errorMessage != nil {
		setClause += ", error_message = ?"
		args = append(args, *errorMessage)
	}
	for _, status := range clean {
		args = append(args, status)
	}

	query := fmt.Sprintf(`UPDATE jobs SET %s WHERE status IN (%s)`, setClause, statusClause)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		res, err := s.db.ExecContext(ctx, query, args...)
		if err != nil {
			if attempt < maxAttempts && IsRetryableSQLiteError(err) && ctx.Err() == nil {
				if !sleepWithContext(ctx, backoff) {
					return 0, ctx.Err()
				}
				backoff *= 2
				continue
			}
			return 0, fmt.Errorf("transition execution statuses: %w", err)
		}

		rows, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("rows affected for status transition: %w", err)
		}
		return int(rows), nil
	}

	return 0, nil
}

// TransitionPendingRootExecutions transitions only pending root jobs
// (parent_execution_id is null/empty) to the provided status.
func (s *Storage) TransitionPendingRootExecutions(ctx context.Context, toStatus string, errorMessage *string) (int, error) {
	const maxAttempts = 4
	backoff := 10 * time.Millisecond
	now := time.Now()

	setClause := "status = ?, updated_at = ?"
	args := []interface{}{toStatus, now}
	if errorMessage != nil {
		setClause += ", error_message = ?"
		args = append(args, *errorMessage)
	}
	args = append(args, events.JobStatusPending)

	query := fmt.Sprintf(`
		UPDATE jobs
		SET %s
		WHERE status = ?
		  AND (parent_execution_id IS NULL OR parent_execution_id = '')
	`, setClause)

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		res, err := s.db.ExecContext(ctx, query, args...)
		if err != nil {
			if attempt < maxAttempts && IsRetryableSQLiteError(err) && ctx.Err() == nil {
				if !sleepWithContext(ctx, backoff) {
					return 0, ctx.Err()
				}
				backoff *= 2
				continue
			}
			return 0, fmt.Errorf("transition pending root executions: %w", err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return 0, fmt.Errorf("rows affected for root transition: %w", err)
		}
		return int(rows), nil
	}

	return 0, nil
}

func sanitizeStatuses(statuses []string) []string {
	clean := make([]string, 0, len(statuses))
	seen := make(map[string]struct{}, len(statuses))
	for _, status := range statuses {
		trimmed := strings.TrimSpace(status)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		clean = append(clean, trimmed)
	}
	return clean
}

func buildStatusQueryArgs(template string, statuses []string) (string, []interface{}) {
	placeholders := strings.TrimRight(strings.Repeat("?,", len(statuses)), ",")
	query := fmt.Sprintf(template, placeholders)
	args := make([]interface{}, 0, len(statuses))
	for _, status := range statuses {
		args = append(args, status)
	}
	return query, args
}
