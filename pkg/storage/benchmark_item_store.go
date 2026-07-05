package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// BenchmarkRunItemAttemptInput is a storage-facing attempt payload.
type BenchmarkRunItemAttemptInput struct {
	Attempt              int
	JobID                string
	LatencyMS            float64
	TokensInput          int
	TokensOutput         int
	TotalTokens          int
	CostUSD              float64
	RawOutput            string
	Predicted            string
	ParseOK              bool
	Error                string
	FailureReason        string
	OutputSource         string
	ContractNodeID       string
	ContractModel        string
	ContractFinishReason string
	ContractTokensOutput int
	ContractMaxTokens    int
	ContractDiagnostic   string
}

// BenchmarkRunItemInput is a storage-facing item payload.
type BenchmarkRunItemInput struct {
	ItemID           string
	Subject          string
	Language         string
	AnswerLabel      string
	Predicted        string
	ParseOK          bool
	Correct          bool
	JobID            string
	LatencyMS        float64
	TokensInput      int
	TokensOutput     int
	TotalTokens      int
	CostUSD          float64
	RawOutput        string
	Error            string
	FailureReason    string
	OutputSource     string
	WorkflowID       string
	BenchmarkName    string
	Attempts         int
	NonLetterRetries int
	AttemptDetails   []BenchmarkRunItemAttemptInput
}

// BenchmarkRunItem is a persisted benchmark item outcome.
type BenchmarkRunItem struct {
	RunID            string  `json:"run_id"`
	ItemID           string  `json:"item_id"`
	Subject          string  `json:"subject"`
	Language         string  `json:"language"`
	AnswerLabel      string  `json:"answer_label"`
	Predicted        string  `json:"predicted"`
	ParseOK          bool    `json:"parse_ok"`
	Correct          bool    `json:"correct"`
	JobID            string  `json:"job_id"`
	LatencyMS        float64 `json:"latency_ms"`
	TokensInput      int     `json:"tokens_input"`
	TokensOutput     int     `json:"tokens_output"`
	TotalTokens      int     `json:"total_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	RawOutput        string  `json:"raw_output"`
	Error            string  `json:"error"`
	FailureReason    string  `json:"failure_reason"`
	OutputSource     string  `json:"output_source"`
	WorkflowID       string  `json:"workflow_id"`
	BenchmarkName    string  `json:"benchmark_name"`
	Attempts         int     `json:"attempts"`
	NonLetterRetries int     `json:"non_letter_retries"`
}

// BenchmarkRunItemAttempt is a persisted per-attempt benchmark item execution.
type BenchmarkRunItemAttempt struct {
	RunID                string  `json:"run_id"`
	ItemID               string  `json:"item_id"`
	Attempt              int     `json:"attempt"`
	JobID                string  `json:"job_id"`
	LatencyMS            float64 `json:"latency_ms"`
	TokensInput          int     `json:"tokens_input"`
	TokensOutput         int     `json:"tokens_output"`
	TotalTokens          int     `json:"total_tokens"`
	CostUSD              float64 `json:"cost_usd"`
	RawOutput            string  `json:"raw_output"`
	Predicted            string  `json:"predicted"`
	ParseOK              bool    `json:"parse_ok"`
	Error                string  `json:"error"`
	FailureReason        string  `json:"failure_reason"`
	OutputSource         string  `json:"output_source"`
	ContractNodeID       string  `json:"contract_node_id"`
	ContractModel        string  `json:"contract_model"`
	ContractFinishReason string  `json:"contract_finish_reason"`
	ContractTokensOutput int     `json:"contract_tokens_output"`
	ContractMaxTokens    int     `json:"contract_max_tokens"`
	ContractDiagnostic   string  `json:"contract_diagnostic"`
}

// BenchmarkRunItemDetail includes item-level outcome plus attempt history.
type BenchmarkRunItemDetail struct {
	Item     BenchmarkRunItem          `json:"item"`
	Attempts []BenchmarkRunItemAttempt `json:"attempts"`
}

// BenchmarkRunItemFilters controls run item list filtering.
type BenchmarkRunItemFilters struct {
	OnlyIncorrect bool
	Subject       string
	FailureReason string
	Limit         int
	Offset        int
}

// BenchmarkItemCostUpdate holds cost/token data for a single benchmark item.
type BenchmarkItemCostUpdate struct {
	RunID       string
	ItemID      string
	TotalTokens int
	CostUSD     float64
}

// BenchmarkRunLink holds the minimal info needed to link a job back to a benchmark run.
type BenchmarkRunLink struct {
	RunID     string `json:"run_id"`
	Benchmark string `json:"benchmark"`
	ItemID    string `json:"item_id,omitempty"`
}

// CountBenchmarkRunItems counts items under one run after optional filtering.
func (s *Storage) CountBenchmarkRunItems(runID string, filters BenchmarkRunItemFilters) (int, error) {
	conditions, args := buildBenchmarkRunItemWhere(runID, filters)
	query := "SELECT COUNT(*) FROM benchmark_run_items WHERE " + strings.Join(conditions, " AND ")
	var count int
	if err := s.db.QueryRow(query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count benchmark run items: %w", err)
	}
	return count, nil
}

// ListBenchmarkRunItems lists benchmark items for a run with basic filtering.
func (s *Storage) ListBenchmarkRunItems(runID string, filters BenchmarkRunItemFilters) ([]BenchmarkRunItem, error) {
	if filters.Limit <= 0 {
		filters.Limit = 50
	}
	if filters.Limit > 500 {
		filters.Limit = 500
	}
	if filters.Offset < 0 {
		filters.Offset = 0
	}

	conditions, args := buildBenchmarkRunItemWhere(runID, filters)
	query := `
		SELECT run_id, item_id, COALESCE(subject, ''), COALESCE(language, ''),
		       COALESCE(answer_label, ''), COALESCE(predicted, ''),
		       parse_ok, correct, COALESCE(job_id, ''),
		       latency_ms, tokens_input, tokens_output, total_tokens, cost_usd,
		       COALESCE(raw_output, ''), COALESCE(error, ''), COALESCE(failure_reason, ''),
		       COALESCE(output_source, ''), COALESCE(workflow_id, ''), COALESCE(benchmark_name, ''),
		       attempts, non_letter_retries
		FROM benchmark_run_items
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY item_id ASC
		LIMIT ? OFFSET ?
	`
	args = append(args, filters.Limit, filters.Offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list benchmark run items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]BenchmarkRunItem, 0)
	for rows.Next() {
		var item BenchmarkRunItem
		var parseOK, correct int
		if err := rows.Scan(
			&item.RunID, &item.ItemID, &item.Subject, &item.Language,
			&item.AnswerLabel, &item.Predicted,
			&parseOK, &correct, &item.JobID,
			&item.LatencyMS, &item.TokensInput, &item.TokensOutput, &item.TotalTokens, &item.CostUSD,
			&item.RawOutput, &item.Error, &item.FailureReason,
			&item.OutputSource, &item.WorkflowID, &item.BenchmarkName,
			&item.Attempts, &item.NonLetterRetries,
		); err != nil {
			return nil, fmt.Errorf("scan benchmark run item: %w", err)
		}
		item.ParseOK = parseOK == 1
		item.Correct = correct == 1
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate benchmark run items: %w", err)
	}
	return out, nil
}

// GetBenchmarkRunItemDetail fetches one item and all attempts for a run.
func (s *Storage) GetBenchmarkRunItemDetail(runID, itemID string) (*BenchmarkRunItemDetail, error) {
	var item BenchmarkRunItem
	var parseOK, correct int
	err := s.db.QueryRow(`
		SELECT run_id, item_id, COALESCE(subject, ''), COALESCE(language, ''),
		       COALESCE(answer_label, ''), COALESCE(predicted, ''),
		       parse_ok, correct, COALESCE(job_id, ''),
		       latency_ms, tokens_input, tokens_output, total_tokens, cost_usd,
		       COALESCE(raw_output, ''), COALESCE(error, ''), COALESCE(failure_reason, ''),
		       COALESCE(output_source, ''), COALESCE(workflow_id, ''), COALESCE(benchmark_name, ''),
		       attempts, non_letter_retries
		FROM benchmark_run_items
		WHERE run_id = ? AND item_id = ?
	`, runID, itemID).Scan(
		&item.RunID, &item.ItemID, &item.Subject, &item.Language,
		&item.AnswerLabel, &item.Predicted,
		&parseOK, &correct, &item.JobID,
		&item.LatencyMS, &item.TokensInput, &item.TokensOutput, &item.TotalTokens, &item.CostUSD,
		&item.RawOutput, &item.Error, &item.FailureReason,
		&item.OutputSource, &item.WorkflowID, &item.BenchmarkName,
		&item.Attempts, &item.NonLetterRetries,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get benchmark run item: %w", err)
	}
	item.ParseOK = parseOK == 1
	item.Correct = correct == 1

	rows, err := s.db.Query(`
		SELECT run_id, item_id, attempt_number, COALESCE(job_id, ''), latency_ms,
		       tokens_input, tokens_output, total_tokens, cost_usd, COALESCE(raw_output, ''),
		       COALESCE(predicted, ''), parse_ok, COALESCE(error, ''), COALESCE(failure_reason, ''),
		       COALESCE(output_source, ''), COALESCE(contract_node_id, ''), COALESCE(contract_model, ''),
		       COALESCE(contract_finish_reason, ''), contract_tokens_output, contract_max_tokens,
		       COALESCE(contract_diagnostic, '')
		FROM benchmark_run_item_attempts
		WHERE run_id = ? AND item_id = ?
		ORDER BY attempt_number ASC
	`, runID, itemID)
	if err != nil {
		return nil, fmt.Errorf("list benchmark run item attempts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	attempts := make([]BenchmarkRunItemAttempt, 0)
	for rows.Next() {
		var attempt BenchmarkRunItemAttempt
		var parseOKInt int
		if err := rows.Scan(
			&attempt.RunID, &attempt.ItemID, &attempt.Attempt, &attempt.JobID, &attempt.LatencyMS,
			&attempt.TokensInput, &attempt.TokensOutput, &attempt.TotalTokens, &attempt.CostUSD, &attempt.RawOutput,
			&attempt.Predicted, &parseOKInt, &attempt.Error, &attempt.FailureReason,
			&attempt.OutputSource, &attempt.ContractNodeID, &attempt.ContractModel,
			&attempt.ContractFinishReason, &attempt.ContractTokensOutput, &attempt.ContractMaxTokens,
			&attempt.ContractDiagnostic,
		); err != nil {
			return nil, fmt.Errorf("scan benchmark run item attempt: %w", err)
		}
		attempt.ParseOK = parseOKInt == 1
		attempts = append(attempts, attempt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate benchmark run item attempts: %w", err)
	}

	return &BenchmarkRunItemDetail{
		Item:     item,
		Attempts: attempts,
	}, nil
}

func buildBenchmarkRunItemWhere(runID string, filters BenchmarkRunItemFilters) ([]string, []interface{}) {
	conditions := []string{"run_id = ?"}
	args := []interface{}{runID}

	if filters.OnlyIncorrect {
		conditions = append(conditions, "correct = 0")
	}
	if subject := strings.TrimSpace(filters.Subject); subject != "" {
		conditions = append(conditions, "subject = ?")
		args = append(args, subject)
	}
	if reason := strings.TrimSpace(filters.FailureReason); reason != "" {
		conditions = append(conditions, "failure_reason = ?")
		args = append(args, reason)
	}
	return conditions, args
}

// BatchUpdateBenchmarkRunItemCosts persists inclusive cost/token data for multiple items.
func (s *Storage) BatchUpdateBenchmarkRunItemCosts(updates []BenchmarkItemCostUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	const maxAttempts = 8
	backoff := 10 * time.Millisecond
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := s.batchUpdateBenchmarkRunItemCostsOnce(updates); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if !IsRetryableSQLiteError(lastErr) || attempt == maxAttempts {
			break
		}
		time.Sleep(backoff)
		if backoff < 200*time.Millisecond {
			backoff *= 2
		}
	}
	return lastErr
}

func (s *Storage) batchUpdateBenchmarkRunItemCostsOnce(updates []BenchmarkItemCostUpdate) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin item cost update tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		UPDATE benchmark_run_items
		SET total_tokens = ?, cost_usd = ?, updated_at = CURRENT_TIMESTAMP
		WHERE run_id = ? AND item_id = ?`)
	if err != nil {
		return fmt.Errorf("prepare item cost update: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, u := range updates {
		if _, err := stmt.Exec(u.TotalTokens, u.CostUSD, u.RunID, u.ItemID); err != nil {
			return fmt.Errorf("update item cost %s/%s: %w", u.RunID, u.ItemID, err)
		}
	}
	return tx.Commit()
}

// ListFailedBenchmarkRunItemIDs returns item_id values for items that are
// incorrect or unparseable in the given benchmark run.
func (s *Storage) ListFailedBenchmarkRunItemIDs(runID string) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT item_id FROM benchmark_run_items WHERE run_id = ? AND (correct = 0 OR parse_ok = 0) ORDER BY item_id`,
		runID,
	)
	if err != nil {
		return nil, fmt.Errorf("list failed benchmark run item ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan failed benchmark item id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate failed benchmark item ids: %w", err)
	}
	return ids, nil
}

// ListAllBenchmarkRunItemAttempts returns every attempt row for a given run,
// ordered by item_id then attempt_number. Callers typically group by ItemID.
func (s *Storage) ListAllBenchmarkRunItemAttempts(runID string) ([]BenchmarkRunItemAttempt, error) {
	rows, err := s.db.Query(`
		SELECT run_id, item_id, attempt_number, COALESCE(job_id, ''),
		       latency_ms, tokens_input, tokens_output, total_tokens, cost_usd,
		       COALESCE(raw_output, ''), COALESCE(predicted, ''), parse_ok,
		       COALESCE(error, ''), COALESCE(failure_reason, ''), COALESCE(output_source, ''),
		       COALESCE(contract_node_id, ''), COALESCE(contract_model, ''),
		       COALESCE(contract_finish_reason, ''), contract_tokens_output, contract_max_tokens,
		       COALESCE(contract_diagnostic, '')
		FROM benchmark_run_item_attempts
		WHERE run_id = ?
		ORDER BY item_id, attempt_number`, runID)
	if err != nil {
		return nil, fmt.Errorf("list benchmark run item attempts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []BenchmarkRunItemAttempt
	for rows.Next() {
		var a BenchmarkRunItemAttempt
		var parseOK int
		if err := rows.Scan(
			&a.RunID, &a.ItemID, &a.Attempt, &a.JobID,
			&a.LatencyMS, &a.TokensInput, &a.TokensOutput, &a.TotalTokens, &a.CostUSD,
			&a.RawOutput, &a.Predicted, &parseOK,
			&a.Error, &a.FailureReason, &a.OutputSource,
			&a.ContractNodeID, &a.ContractModel,
			&a.ContractFinishReason, &a.ContractTokensOutput, &a.ContractMaxTokens,
			&a.ContractDiagnostic,
		); err != nil {
			return nil, fmt.Errorf("scan benchmark attempt row: %w", err)
		}
		a.ParseOK = parseOK != 0
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpsertBenchmarkRunItem persists a single benchmark item and its attempts.
func (s *Storage) UpsertBenchmarkRunItem(runID string, item *BenchmarkRunItemInput) error {
	if item == nil {
		return nil
	}
	return retrySQLiteBusy(func() error {
		return s.upsertBenchmarkRunItemOnce(runID, item)
	})
}

func (s *Storage) upsertBenchmarkRunItemOnce(runID string, item *BenchmarkRunItemInput) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin upsert benchmark item tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := upsertBenchmarkRunItemSummaryReplaceTx(tx, runID, item); err != nil {
		return fmt.Errorf("upsert benchmark item %s/%s: %w", runID, item.ItemID, err)
	}

	// Delete and re-insert attempts for this item
	if _, err := tx.Exec(`DELETE FROM benchmark_run_item_attempts WHERE run_id = ? AND item_id = ?`, runID, item.ItemID); err != nil {
		return fmt.Errorf("delete benchmark attempts %s/%s: %w", runID, item.ItemID, err)
	}
	if err := insertBenchmarkRunItemAttemptsTx(tx, runID, item.ItemID, item.AttemptDetails, 0); err != nil {
		return fmt.Errorf("insert benchmark attempts %s/%s: %w", runID, item.ItemID, err)
	}

	return tx.Commit()
}

// AppendBenchmarkRunItemResult appends rerun attempts to an existing item's
// history and updates the item summary to reflect the latest attempt outcome.
// Attempt numbers are auto-offset from the current max for that item.
// Item-level metrics (attempts, non_letter_retries, latency, tokens, cost)
// are accumulated rather than replaced.
func (s *Storage) AppendBenchmarkRunItemResult(runID string, item *BenchmarkRunItemInput) error {
	if item == nil {
		return nil
	}
	return retrySQLiteBusy(func() error {
		return s.appendBenchmarkRunItemResultOnce(runID, item)
	})
}

func (s *Storage) appendBenchmarkRunItemResultOnce(runID string, item *BenchmarkRunItemInput) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin append benchmark item tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Get the current max attempt number for this item.
	var maxAttempt int
	err = tx.QueryRow(
		`SELECT COALESCE(MAX(attempt_number), 0) FROM benchmark_run_item_attempts WHERE run_id = ? AND item_id = ?`,
		runID, item.ItemID,
	).Scan(&maxAttempt)
	if err != nil {
		return fmt.Errorf("query max attempt for %s/%s: %w", runID, item.ItemID, err)
	}

	// Update item summary: latest-wins for result fields, accumulate for metrics.
	if err := upsertBenchmarkRunItemSummaryAccumulateTx(tx, runID, item); err != nil {
		return fmt.Errorf("upsert benchmark item %s/%s: %w", runID, item.ItemID, err)
	}

	// Append new attempts with offset numbering (don't delete old ones).
	if err := insertBenchmarkRunItemAttemptsTx(tx, runID, item.ItemID, item.AttemptDetails, maxAttempt); err != nil {
		return fmt.Errorf("insert benchmark attempts %s/%s: %w", runID, item.ItemID, err)
	}

	return tx.Commit()
}

func upsertBenchmarkRunItemSummaryReplaceTx(tx *sql.Tx, runID string, item *BenchmarkRunItemInput) error {
	_, err := tx.Exec(`
		INSERT INTO benchmark_run_items
		(run_id, item_id, subject, language, answer_label, predicted,
		 parse_ok, correct, job_id, latency_ms,
		 tokens_input, tokens_output, total_tokens, cost_usd,
		 raw_output, error, failure_reason, output_source,
		 workflow_id, benchmark_name, attempts, non_letter_retries)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(run_id, item_id) DO UPDATE SET
		 predicted = excluded.predicted,
		 parse_ok = excluded.parse_ok,
		 correct = excluded.correct,
		 job_id = excluded.job_id,
		 latency_ms = excluded.latency_ms,
		 tokens_input = excluded.tokens_input,
		 tokens_output = excluded.tokens_output,
		 total_tokens = excluded.total_tokens,
		 cost_usd = excluded.cost_usd,
		 raw_output = excluded.raw_output,
		 error = excluded.error,
		 failure_reason = excluded.failure_reason,
		 output_source = excluded.output_source,
		 attempts = excluded.attempts,
		 non_letter_retries = excluded.non_letter_retries,
		 updated_at = CURRENT_TIMESTAMP`,
		runID, item.ItemID, item.Subject, item.Language, item.AnswerLabel, item.Predicted,
		boolToInt(item.ParseOK), boolToInt(item.Correct), nullIfEmpty(item.JobID), item.LatencyMS,
		item.TokensInput, item.TokensOutput, item.TotalTokens, item.CostUSD,
		item.RawOutput, nullIfEmpty(item.Error), nullIfEmpty(item.FailureReason), item.OutputSource,
		item.WorkflowID, item.BenchmarkName, item.Attempts, item.NonLetterRetries)
	return err
}

func upsertBenchmarkRunItemSummaryAccumulateTx(tx *sql.Tx, runID string, item *BenchmarkRunItemInput) error {
	_, err := tx.Exec(`
		INSERT INTO benchmark_run_items
		(run_id, item_id, subject, language, answer_label, predicted,
		 parse_ok, correct, job_id, latency_ms,
		 tokens_input, tokens_output, total_tokens, cost_usd,
		 raw_output, error, failure_reason, output_source,
		 workflow_id, benchmark_name, attempts, non_letter_retries)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(run_id, item_id) DO UPDATE SET
		 predicted = excluded.predicted,
		 parse_ok = excluded.parse_ok,
		 correct = excluded.correct,
		 job_id = excluded.job_id,
		 raw_output = excluded.raw_output,
		 error = excluded.error,
		 failure_reason = excluded.failure_reason,
		 output_source = excluded.output_source,
		 latency_ms = benchmark_run_items.latency_ms + excluded.latency_ms,
		 tokens_input = benchmark_run_items.tokens_input + excluded.tokens_input,
		 tokens_output = benchmark_run_items.tokens_output + excluded.tokens_output,
		 total_tokens = benchmark_run_items.total_tokens + excluded.total_tokens,
		 cost_usd = benchmark_run_items.cost_usd + excluded.cost_usd,
		 attempts = benchmark_run_items.attempts + excluded.attempts,
		 non_letter_retries = benchmark_run_items.non_letter_retries + excluded.non_letter_retries,
		 updated_at = CURRENT_TIMESTAMP`,
		runID, item.ItemID, item.Subject, item.Language, item.AnswerLabel, item.Predicted,
		boolToInt(item.ParseOK), boolToInt(item.Correct), nullIfEmpty(item.JobID), item.LatencyMS,
		item.TokensInput, item.TokensOutput, item.TotalTokens, item.CostUSD,
		item.RawOutput, nullIfEmpty(item.Error), nullIfEmpty(item.FailureReason), item.OutputSource,
		item.WorkflowID, item.BenchmarkName, item.Attempts, item.NonLetterRetries)
	return err
}

func insertBenchmarkRunItemAttemptsTx(tx *sql.Tx, runID, itemID string, attempts []BenchmarkRunItemAttemptInput, attemptOffset int) error {
	for _, attempt := range attempts {
		attemptNumber := attempt.Attempt + attemptOffset
		_, err := tx.Exec(`
			INSERT INTO benchmark_run_item_attempts
			(run_id, item_id, attempt_number, job_id,
			 latency_ms, tokens_input, tokens_output, total_tokens, cost_usd,
			 raw_output, predicted, parse_ok, error, failure_reason, output_source,
			 contract_node_id, contract_model, contract_finish_reason,
			 contract_tokens_output, contract_max_tokens, contract_diagnostic)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			runID, itemID, attemptNumber, nullIfEmpty(attempt.JobID),
			attempt.LatencyMS, attempt.TokensInput, attempt.TokensOutput, attempt.TotalTokens, attempt.CostUSD,
			attempt.RawOutput, attempt.Predicted, boolToInt(attempt.ParseOK), nullIfEmpty(attempt.Error), nullIfEmpty(attempt.FailureReason), attempt.OutputSource,
			nullIfEmpty(attempt.ContractNodeID), nullIfEmpty(attempt.ContractModel), nullIfEmpty(attempt.ContractFinishReason),
			attempt.ContractTokensOutput, attempt.ContractMaxTokens, nullIfEmpty(attempt.ContractDiagnostic))
		if err != nil {
			return fmt.Errorf("insert attempt %s/%s/%d: %w", runID, itemID, attemptNumber, err)
		}
	}
	return nil
}

// GetBenchmarkRunByJobID returns the benchmark run that created the given job,
// or nil if the job was not created by a benchmark.
func (s *Storage) GetBenchmarkRunByJobID(jobID string) (*BenchmarkRunLink, error) {
	if jobID == "" {
		return nil, nil
	}
	var link BenchmarkRunLink
	err := s.db.QueryRow(`
		SELECT bri.run_id, br.benchmark, bri.item_id
		FROM benchmark_run_items bri
		JOIN benchmark_runs br ON br.id = bri.run_id
		WHERE bri.job_id = ?
		LIMIT 1
	`, jobID).Scan(&link.RunID, &link.Benchmark, &link.ItemID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetBenchmarkRunByJobID: %w", err)
	}
	return &link, nil
}
