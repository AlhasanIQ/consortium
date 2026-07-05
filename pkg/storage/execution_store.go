package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	sqlite "modernc.org/sqlite"
)

// sqliteConstraintUnique is the SQLite extended error code for UNIQUE constraint
// violations (SQLITE_CONSTRAINT_UNIQUE = 2067). Defined here to avoid importing
// the heavy modernc.org/sqlite/lib package.
const sqliteConstraintUnique = 2067

// executionColumns is the canonical SELECT column list for WorkflowExecution.
// Every query that returns a WorkflowExecution must use this constant
// paired with scanExecution to keep the column list and Scan call in sync.
const executionColumns = `id, query, model, status,
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
	COALESCE(dag_hash, '')`

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...interface{}) error
}

// scanExecution scans a single row into a WorkflowExecution.
// The row must have been selected using executionColumns.
func scanExecution(row rowScanner) (*WorkflowExecution, error) {
	exec := &WorkflowExecution{}
	err := row.Scan(
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
	)
	return exec, err
}

// scanExecutionRows scans all rows into a slice of WorkflowExecution.
// The rows must have been selected using executionColumns.
func scanExecutionRows(rows *sql.Rows) ([]WorkflowExecution, error) {
	var executions []WorkflowExecution
	for rows.Next() {
		exec, err := scanExecution(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan execution: %w", err)
		}
		executions = append(executions, *exec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating executions: %w", err)
	}
	return executions, nil
}

// CreateExecution creates a new workflow execution
func (s *Storage) CreateExecution(exec *WorkflowExecution) error {
	query := `
		INSERT INTO jobs (id, query, model, status, request_data, workflow_id, parent_execution_id,
		                  idempotency_key, request_hash, config_hash, user_id,
		                  workflow_execution_id, run_id, run_number, previous_run_id, dag_snapshot, dag_hash,
		                  created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	now := time.Now()

	// Convert empty strings to nil for proper NULL handling
	var idempotencyKey, requestHash, configHash, userID interface{}
	if exec.IdempotencyKey != "" {
		idempotencyKey = exec.IdempotencyKey
	}
	if exec.RequestHash != "" {
		requestHash = exec.RequestHash
	}
	if exec.ConfigHash != "" {
		configHash = exec.ConfigHash
	}
	if exec.UserID != "" {
		userID = exec.UserID
	}

	// Durable runtime fields (nullable for non-durable/older rows).
	var workflowExecID, runID, previousRunID, dagSnapshot, dagHash interface{}
	if exec.WorkflowExecutionID != "" {
		workflowExecID = exec.WorkflowExecutionID
	}
	if exec.RunID != "" {
		runID = exec.RunID
	}
	if exec.PreviousRunID != "" {
		previousRunID = exec.PreviousRunID
	}
	if exec.DAGSnapshot != "" {
		dagSnapshot = exec.DAGSnapshot
	}
	if exec.DAGHash != "" {
		dagHash = exec.DAGHash
	}
	runNumber := exec.RunNumber
	if runNumber == 0 {
		runNumber = 1
	}

	_, err := s.db.Exec(query,
		exec.ID, exec.Description, exec.Model, exec.Status, exec.RequestData, exec.WorkflowID, exec.ParentExecutionID,
		idempotencyKey, requestHash, configHash, userID,
		workflowExecID, runID, runNumber, previousRunID, dagSnapshot, dagHash,
		now, now)
	if err != nil {
		return fmt.Errorf("failed to create execution: %w", err)
	}
	return nil
}

// CreateExecutionAtomic attempts to create a job. If a UNIQUE constraint violation
// occurs on idempotency_key, it returns the existing eligible job instead of an error.
// Returns (true, nil) if created, (false, existing) if dedup hit, or error.
func (s *Storage) CreateExecutionAtomic(exec *WorkflowExecution) (bool, *WorkflowExecution, error) {
	err := s.CreateExecution(exec)
	if err == nil {
		return true, nil, nil
	}

	// Check for UNIQUE constraint violation (idempotency key collision)
	var sqliteErr *sqlite.Error
	if exec.IdempotencyKey != "" && errors.As(err, &sqliteErr) && sqliteErr.Code() == sqliteConstraintUnique {
		existing, lookupErr := s.GetEligibleExecutionByIdempotencyKey(exec.IdempotencyKey, exec.UserID)
		if lookupErr != nil {
			return false, nil, fmt.Errorf("failed to look up existing job after collision: %w", lookupErr)
		}
		if existing != nil {
			return false, existing, nil
		}
		// Collision with a non-eligible job (failed/cancelled) — treat as no dedup hit,
		// but we can't create because the key is taken. Generate a new job without the key.
		return false, nil, nil
	}

	return false, nil, err
}

// GetExecution retrieves a workflow execution by ID
func (s *Storage) GetExecution(id string) (*WorkflowExecution, error) {
	query := `SELECT ` + executionColumns + ` FROM jobs WHERE id = ?`
	exec, err := scanExecution(s.db.QueryRow(query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get execution: %w", err)
	}
	return exec, nil
}

// UpdateExecution updates an execution's status and result
func (s *Storage) UpdateExecution(exec *WorkflowExecution) error {
	query := `
		UPDATE jobs
		SET status = ?, response_data = ?, result_text = ?,
		    tokens_input = ?, tokens_output = ?, tokens_total = ?,
		    cost = ?, error_message = ?, retry_count = ?, updated_at = ?
		WHERE id = ?
	`
	_, err := s.db.Exec(query,
		exec.Status, exec.ResponseData, exec.ResultText,
		exec.TokensInput, exec.TokensOutput, exec.TokensTotal,
		exec.Cost, exec.ErrorMessage, exec.RetryCount, time.Now(),
		exec.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update execution: %w", err)
	}
	return nil
}

// UpdateExecutionStatus updates only the status field of an execution.
func (s *Storage) UpdateExecutionStatus(id, status string) error {
	_, err := s.db.Exec(`UPDATE jobs SET status = ?, updated_at = ? WHERE id = ?`, status, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to update execution status: %w", err)
	}
	return nil
}

// UpdateExecutionResult updates the result fields of a completed execution.
func (s *Storage) UpdateExecutionResult(id, resultText string, cost float64, inputTokens, outputTokens, totalTokens int) error {
	_, err := s.db.Exec(`
		UPDATE jobs
		SET result_text = ?,
		    cost = ?,
		    tokens_input = ?,
		    tokens_output = ?,
		    tokens_total = ?,
		    updated_at = ?
		WHERE id = ?
	`, resultText, cost, inputTokens, outputTokens, totalTokens, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to update execution result: %w", err)
	}
	return nil
}

// CompleteExecution atomically sets the final status and all result fields of a
// completed, failed, or cancelled execution in a single UPDATE. This avoids the
// window between separate UpdateExecutionStatus/UpdateExecutionResult/UpdateExecutionError
// calls where the row is partially updated.
func (s *Storage) CompleteExecution(id, status, resultText string, cost float64, inputTokens, outputTokens, totalTokens int, errMsg string) error {
	_, err := s.db.Exec(`
		UPDATE jobs
		SET status = ?,
		    result_text = ?,
		    cost = ?,
		    tokens_input = ?,
		    tokens_output = ?,
		    tokens_total = ?,
		    error_message = ?,
		    updated_at = ?
		WHERE id = ?
	`, status, resultText, cost, inputTokens, outputTokens, totalTokens, errMsg, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to complete execution: %w", err)
	}
	return nil
}

// UpdateExecutionError updates the error message of a failed execution.
func (s *Storage) UpdateExecutionError(id, errMsg string) error {
	_, err := s.db.Exec(`UPDATE jobs SET error_message = ?, updated_at = ? WHERE id = ?`, errMsg, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to update execution error: %w", err)
	}
	return nil
}

// ListExecutions retrieves recent workflow executions
func (s *Storage) ListExecutions(limit int) ([]WorkflowExecution, error) {
	query := `SELECT ` + executionColumns + `
		FROM jobs
		WHERE status != 'archived'
		ORDER BY created_at DESC
		LIMIT ?`
	rows, err := s.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list executions: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("Error closing rows: %v", err)
		}
	}()
	return scanExecutionRows(rows)
}

// ListExecutionsByStatus retrieves executions filtered by status
func (s *Storage) ListExecutionsByStatus(status string, limit int) ([]WorkflowExecution, error) {
	query := `SELECT ` + executionColumns + `
		FROM jobs
		WHERE status = ?
		ORDER BY created_at DESC
		LIMIT ?`
	rows, err := s.db.Query(query, status, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list executions by status: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("Error closing rows: %v", err)
		}
	}()
	return scanExecutionRows(rows)
}

// ListExecutionsPaginated retrieves executions with cursor-based pagination
// Cursor format: base64(created_at_unix_nano:job_id) for stable pagination
func (s *Storage) ListExecutionsPaginated(cursor string, limit int, filters *ExecutionFilters) (*PaginatedExecutions, error) {
	// Parse cursor if provided
	var cursorTime time.Time
	var cursorID string
	if cursor != "" {
		decoded, err := decodeCursor(cursor)
		if err != nil {
			return nil, fmt.Errorf("invalid cursor: %w", err)
		}
		cursorTime = decoded.Time
		cursorID = decoded.ID
	}

	// Build query with optional filters
	var args []interface{}
	query := `SELECT ` + executionColumns + `
		FROM jobs
		WHERE status != 'archived'
	`

	// Apply filters
	if filters != nil {
		if filters.Status != "" {
			query += ` AND status = ?`
			args = append(args, filters.Status)
		}
		if filters.WorkflowID != "" {
			query += ` AND workflow_id = ?`
			args = append(args, filters.WorkflowID)
		}
		if filters.ConfigHash != "" {
			query += ` AND config_hash = ?`
			args = append(args, filters.ConfigHash)
		}
	}

	// Apply cursor for pagination (keyset pagination)
	if cursor != "" {
		query += ` AND (created_at < ? OR (created_at = ? AND id < ?))`
		args = append(args, cursorTime, cursorTime, cursorID)
	}

	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit+1) // Fetch one extra to check for more

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list executions: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("Error closing rows: %v", err)
		}
	}()

	executions, err := scanExecutionRows(rows)
	if err != nil {
		return nil, err
	}

	// Check if there are more results
	hasMore := len(executions) > limit
	if hasMore {
		executions = executions[:limit] // Remove the extra row
	}

	// Generate next cursor
	var nextCursor string
	if hasMore && len(executions) > 0 {
		lastExec := executions[len(executions)-1]
		nextCursor = encodeCursor(lastExec.CreatedAt, lastExec.ID)
	}

	return &PaginatedExecutions{
		Executions: executions,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

// CountExecutionNodes returns the number of executed nodes for a given execution.
func (s *Storage) CountExecutionNodes(jobID string) (int, error) {
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM workflow_node_executions
		WHERE job_id = ?
		  AND (
			run_id = COALESCE(NULLIF((SELECT run_id FROM jobs WHERE id = ?), ''), ?)
		  )
	`, jobID, jobID, jobID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count nodes: %w", err)
	}
	return count, nil
}

// ArchiveExecution archives an execution
func (s *Storage) ArchiveExecution(executionID string) error {
	query := `
		UPDATE jobs
		SET status = 'archived', archived_at = ?, updated_at = ?
		WHERE id = ?
	`
	now := time.Now()
	_, err := s.db.Exec(query, now, now, executionID)
	if err != nil {
		return fmt.Errorf("failed to archive execution: %w", err)
	}
	return nil
}

// UnarchiveExecution restores an archived execution to its previous status
func (s *Storage) UnarchiveExecution(executionID string, newStatus string) error {
	query := `
		UPDATE jobs
		SET status = ?, archived_at = NULL, updated_at = ?
		WHERE id = ?
	`
	_, err := s.db.Exec(query, newStatus, time.Now(), executionID)
	if err != nil {
		return fmt.Errorf("failed to unarchive execution: %w", err)
	}
	return nil
}

// GetChildExecutions retrieves all child executions for a given parent execution ID.
// Used to track parent-child relationships between executions.
func (s *Storage) GetChildExecutions(parentExecutionID string) ([]WorkflowExecution, error) {
	query := `SELECT ` + executionColumns + `
		FROM jobs
		WHERE parent_execution_id = ?
		ORDER BY created_at ASC`
	rows, err := s.db.Query(query, parentExecutionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get child executions: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Printf("Error closing rows: %v", err)
		}
	}()
	return scanExecutionRows(rows)
}

// ListLatestChildExecutionsByParentIDs returns the newest child execution for each
// parent execution ID. Parents with no children are omitted from the map.
func (s *Storage) ListLatestChildExecutionsByParentIDs(parentExecutionIDs []string) (map[string]WorkflowExecution, error) {
	parents := normalizeNonEmptyIDs(parentExecutionIDs)
	out := make(map[string]WorkflowExecution, len(parents))
	if len(parents) == 0 {
		return out, nil
	}

	const maxIDsPerChunk = 300
	for start := 0; start < len(parents); start += maxIDsPerChunk {
		end := start + maxIDsPerChunk
		if end > len(parents) {
			end = len(parents)
		}
		chunk := parents[start:end]

		placeholders := inClausePlaceholders(len(chunk))
		query := `SELECT ` + executionColumns + `
			FROM jobs
			WHERE parent_execution_id IN (` + placeholders + `)
			ORDER BY parent_execution_id ASC, created_at DESC, id DESC`

		args := make([]interface{}, len(chunk))
		for i := range chunk {
			args[i] = chunk[i]
		}

		rows, err := s.db.Query(query, args...)
		if err != nil {
			return nil, fmt.Errorf("failed to list latest child executions: %w", err)
		}

		for rows.Next() {
			exec, err := scanExecution(rows)
			if err != nil {
				return nil, fmt.Errorf("failed to scan child execution: %w", err)
			}
			parentID := strings.TrimSpace(exec.ParentExecutionID)
			if parentID == "" {
				continue
			}
			// Rows are ordered newest-first per parent, so first row wins.
			if _, exists := out[parentID]; !exists {
				out[parentID] = *exec
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("error iterating child executions: %w", err)
		}
		if err := rows.Close(); err != nil {
			log.Printf("Error closing rows: %v", err)
		}
	}

	return out, nil
}

// getExecutionChainRecursive recursively retrieves all child executions
func (s *Storage) getExecutionChainRecursive(parentExecutionID string) ([]WorkflowExecution, error) {
	children, err := s.GetChildExecutions(parentExecutionID)
	if err != nil {
		return nil, err
	}

	var allChildren []WorkflowExecution
	for _, child := range children {
		allChildren = append(allChildren, child)

		// Recursively get this child's children
		grandchildren, err := s.getExecutionChainRecursive(child.ID)
		if err != nil {
			return nil, err
		}
		allChildren = append(allChildren, grandchildren...)
	}

	return allChildren, nil
}

// GetExecutionChain retrieves the full parent/child execution tree starting from a parent
// Returns executions in execution order (parent first, then children in chronological order)
func (s *Storage) GetExecutionChain(rootExecutionID string) ([]WorkflowExecution, error) {
	var allExecutions []WorkflowExecution

	// Get the root execution
	rootExec, err := s.GetExecution(rootExecutionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get root execution: %w", err)
	}
	allExecutions = append(allExecutions, *rootExec)

	// Get all children recursively
	children, err := s.getExecutionChainRecursive(rootExecutionID)
	if err != nil {
		return nil, err
	}
	allExecutions = append(allExecutions, children...)

	return allExecutions, nil
}

// GetEligibleExecutionByIdempotencyKey retrieves an eligible execution by idempotency key,
// optionally scoped to a user. Only returns dedup-eligible statuses.
func (s *Storage) GetEligibleExecutionByIdempotencyKey(idempotencyKey, userID string) (*WorkflowExecution, error) {
	if idempotencyKey == "" {
		return nil, nil
	}
	query := `SELECT ` + executionColumns + `
		FROM jobs WHERE idempotency_key = ?
		  AND status IN ('pending', 'running', 'paused', 'completed')`
	args := []interface{}{idempotencyKey}
	if userID != "" {
		query += ` AND user_id = ?`
		args = append(args, userID)
	}
	exec, err := scanExecution(s.db.QueryRow(query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // Not found is not an error for idempotency checks
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get execution by idempotency key: %w", err)
	}
	return exec, nil
}

// ReconcileRunningJobs handles jobs left in 'running' state from a previous server session.
// Legacy jobs (no dag_hash) are failed since they cannot be resumed.
// Durable jobs (have dag_hash) are left as-is for background workers to resume.
// Returns (legacyFailed, durableRunning).
func (s *Storage) ReconcileRunningJobs() (int64, int, error) {
	result, err := s.db.Exec(`
		UPDATE jobs
		SET status='failed',
		    error_message='Server restart - workflow was interrupted',
		    updated_at=datetime('now')
		WHERE status='running'
		  AND (dag_hash IS NULL OR dag_hash = '')
	`)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to reconcile legacy running jobs: %w", err)
	}
	legacyFailed, _ := result.RowsAffected()

	var durableCount int
	err = s.db.QueryRow(durableJobCountSQL("running", durableQueueScopeAny)).Scan(&durableCount)
	if err != nil {
		return legacyFailed, 0, fmt.Errorf("failed to count durable running jobs: %w", err)
	}

	return legacyFailed, durableCount, nil
}

// FindRecentEligibleExecutionByRequestHash finds an eligible execution by request hash
// within a time window, optionally scoped to a user.
func (s *Storage) FindRecentEligibleExecutionByRequestHash(requestHash string, windowSeconds int, userID string) (*WorkflowExecution, error) {
	if requestHash == "" {
		return nil, nil
	}
	query := `SELECT ` + executionColumns + `
		FROM jobs
		WHERE request_hash = ?
		  AND created_at > datetime('now', '-' || ? || ' seconds')
		  AND status IN ('pending', 'running', 'paused', 'completed')`
	args := []interface{}{requestHash, windowSeconds}
	if userID != "" {
		query += ` AND user_id = ?`
		args = append(args, userID)
	}
	query += ` ORDER BY created_at DESC LIMIT 1`
	exec, err := scanExecution(s.db.QueryRow(query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // Not found is not an error for deduplication checks
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find recent execution by request hash: %w", err)
	}
	return exec, nil
}
