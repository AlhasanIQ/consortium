package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	APIModelRouteModeWorkflow    = "workflow"
	APIModelRouteModeDirectModel = "direct_model"

	APIUsageStatusRunning   = "running"
	APIUsageStatusSucceeded = "succeeded"
	APIUsageStatusFailed    = "failed"
	APIUsageStatusCancelled = "cancelled"
)

type APIKey struct {
	ID                string     `json:"id"`
	UserID            string     `json:"user_id"`
	Name              string     `json:"name"`
	Prefix            string     `json:"prefix"`
	KeyHash           string     `json:"-"`
	WorkflowID        string     `json:"workflow_id,omitempty"`
	RequestsPerMinute int        `json:"requests_per_minute"`
	TokensPerMinute   int        `json:"tokens_per_minute"`
	CreatedAt         time.Time  `json:"created_at"`
	LastUsedAt        *time.Time `json:"last_used_at,omitempty"`
	RevokedAt         *time.Time `json:"revoked_at,omitempty"`
}

type APIModelRoute struct {
	APIModel      string    `json:"api_model"`
	Mode          string    `json:"mode"`
	WorkflowID    string    `json:"workflow_id,omitempty"`
	ProviderModel string    `json:"provider_model,omitempty"`
	Description   string    `json:"description,omitempty"`
	IsDefault     bool      `json:"is_default"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type APIUsageRecord struct {
	ID             string     `json:"id"`
	RequestID      string     `json:"request_id"`
	KeyID          string     `json:"key_id"`
	UserID         string     `json:"user_id"`
	Endpoint       string     `json:"endpoint"`
	RequestedModel string     `json:"requested_model,omitempty"`
	ResolvedModel  string     `json:"resolved_model,omitempty"`
	WorkflowID     string     `json:"workflow_id,omitempty"`
	JobID          string     `json:"job_id,omitempty"`
	Status         string     `json:"status"`
	HTTPStatus     int        `json:"http_status,omitempty"`
	Stream         bool       `json:"stream"`
	TokensInput    int        `json:"tokens_input"`
	TokensOutput   int        `json:"tokens_output"`
	TokensTotal    int        `json:"tokens_total"`
	Cost           float64    `json:"cost"`
	LatencyMs      float64    `json:"latency_ms"`
	ErrorCode      string     `json:"error_code,omitempty"`
	ErrorMessage   string     `json:"error_message,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
}

type APIUsageCompletion struct {
	JobID        string
	Status       string
	HTTPStatus   int
	TokensInput  int
	TokensOutput int
	TokensTotal  int
	Cost         float64
	LatencyMs    float64
	ErrorCode    string
	ErrorMessage string
	CompletedAt  time.Time
}

type APIUsageFilters struct {
	From           *time.Time
	To             *time.Time
	KeyID          string
	RequestedModel string
	Endpoint       string
	Status         string
	Limit          int
}

type APIUsageSummary struct {
	Requests     int     `json:"requests"`
	TokensInput  int     `json:"tokens_input"`
	TokensOutput int     `json:"tokens_output"`
	TokensTotal  int     `json:"tokens_total"`
	Cost         float64 `json:"cost"`
}

type OpenAIAPIMetrics struct {
	GeneratedAt              time.Time       `json:"generated_at"`
	StaleBefore              time.Time       `json:"stale_before"`
	Usage                    APIUsageSummary `json:"usage"`
	AvgLatencyMs             float64         `json:"avg_latency_ms"`
	RequestsByStatus         map[string]int  `json:"requests_by_status"`
	RequestsByEndpoint       map[string]int  `json:"requests_by_endpoint"`
	HTTPStatusClasses        map[string]int  `json:"http_status_classes"`
	StaleRunningUsage        int             `json:"stale_running_usage"`
	StaleBackgroundResponses int             `json:"stale_background_responses"`
	PendingIdempotency       int             `json:"pending_idempotency"`
}

type APIIdempotencyRecord struct {
	ID                 string    `json:"id"`
	KeyID              string    `json:"key_id"`
	IdempotencyKey     string    `json:"idempotency_key"`
	RequestFingerprint string    `json:"request_fingerprint"`
	JobID              string    `json:"job_id,omitempty"`
	ResponseBody       string    `json:"response_body,omitempty"`
	HTTPStatus         int       `json:"http_status,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	ExpiresAt          time.Time `json:"expires_at"`
}

func (s *Storage) CreateAPIKey(key *APIKey) error {
	if key == nil {
		return fmt.Errorf("api key is required")
	}
	key.ID = strings.TrimSpace(key.ID)
	key.UserID = defaultSystemUser(strings.TrimSpace(key.UserID))
	key.Name = strings.TrimSpace(key.Name)
	key.Prefix = strings.TrimSpace(key.Prefix)
	key.KeyHash = strings.TrimSpace(key.KeyHash)
	key.WorkflowID = strings.TrimSpace(key.WorkflowID)
	if key.ID == "" || key.Name == "" || key.Prefix == "" || key.KeyHash == "" {
		return fmt.Errorf("id, name, prefix, and key_hash are required")
	}
	if key.WorkflowID != "" {
		if _, err := s.GetWorkflow(key.WorkflowID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return ErrNotFound
			}
			return err
		}
	}
	if key.RequestsPerMinute <= 0 {
		key.RequestsPerMinute = 60
	}
	if key.TokensPerMinute <= 0 {
		key.TokensPerMinute = 120000
	}
	if key.CreatedAt.IsZero() {
		key.CreatedAt = time.Now().UTC()
	}

	_, err := s.db.Exec(`
		INSERT INTO api_keys (
			id, user_id, name, prefix, key_hash, workflow_id,
			requests_per_minute, tokens_per_minute, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, key.ID, key.UserID, key.Name, key.Prefix, key.KeyHash, nullIfEmpty(key.WorkflowID),
		key.RequestsPerMinute, key.TokensPerMinute, sqlTime(key.CreatedAt))
	if err != nil {
		return fmt.Errorf("create api key: %w", err)
	}
	return nil
}

func (s *Storage) GetAPIKeyByPrefix(prefix string) (*APIKey, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil, ErrNotFound
	}
	return scanAPIKey(s.db.QueryRow(`
		SELECT id, user_id, name, prefix, key_hash, COALESCE(workflow_id, ''),
		       requests_per_minute, tokens_per_minute, created_at, last_used_at, revoked_at
		FROM api_keys
		WHERE prefix = ?
	`, prefix))
}

func (s *Storage) GetAPIKeyByHash(keyHash string) (*APIKey, error) {
	keyHash = strings.TrimSpace(keyHash)
	if keyHash == "" {
		return nil, ErrNotFound
	}
	return scanAPIKey(s.db.QueryRow(`
		SELECT id, user_id, name, prefix, key_hash, COALESCE(workflow_id, ''),
		       requests_per_minute, tokens_per_minute, created_at, last_used_at, revoked_at
		FROM api_keys
		WHERE key_hash = ?
	`, keyHash))
}

func (s *Storage) ListAPIKeys(userID string, includeRevoked bool) ([]APIKey, error) {
	userID = strings.TrimSpace(userID)
	clauses := []string{"1=1"}
	args := []interface{}{}
	if userID != "" {
		clauses = append(clauses, "user_id = ?")
		args = append(args, userID)
	}
	if !includeRevoked {
		clauses = append(clauses, "revoked_at IS NULL")
	}
	rows, err := s.db.Query(`
		SELECT id, user_id, name, prefix, key_hash, COALESCE(workflow_id, ''),
		       requests_per_minute, tokens_per_minute, created_at, last_used_at, revoked_at
		FROM api_keys
		WHERE `+strings.Join(clauses, " AND ")+`
		ORDER BY created_at DESC, id DESC
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []APIKey
	for rows.Next() {
		key, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *key)
	}
	return out, rows.Err()
}

func (s *Storage) TouchAPIKeyLastUsed(id string, at time.Time) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrNotFound
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	result, err := s.db.Exec(`UPDATE api_keys SET last_used_at = ? WHERE id = ?`, sqlTime(at), id)
	if err != nil {
		return fmt.Errorf("touch api key: %w", err)
	}
	return requireRowsAffected(result, ErrNotFound)
}

func (s *Storage) RevokeAPIKey(id string, at time.Time) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrNotFound
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	result, err := s.db.Exec(`UPDATE api_keys SET revoked_at = ? WHERE id = ?`, sqlTime(at), id)
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	return requireRowsAffected(result, ErrNotFound)
}

func (s *Storage) UpsertAPIModelRoute(route *APIModelRoute) error {
	if route == nil {
		return fmt.Errorf("api model route is required")
	}
	cleanAPIModelRoute(route)
	if route.APIModel == "" {
		return fmt.Errorf("api_model is required")
	}
	switch route.Mode {
	case APIModelRouteModeWorkflow:
		if route.WorkflowID == "" {
			return fmt.Errorf("workflow_id is required for workflow routes")
		}
		if _, err := s.GetWorkflow(route.WorkflowID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return ErrNotFound
			}
			return err
		}
		route.ProviderModel = ""
	case APIModelRouteModeDirectModel:
		if route.ProviderModel == "" {
			return fmt.Errorf("provider_model is required for direct_model routes")
		}
		route.WorkflowID = ""
	default:
		return fmt.Errorf("unsupported route mode %q", route.Mode)
	}

	now := time.Now().UTC()
	if route.CreatedAt.IsZero() {
		route.CreatedAt = now
	}
	if route.UpdatedAt.IsZero() {
		route.UpdatedAt = now
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin route upsert: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if route.IsDefault && route.Enabled {
		if _, err := tx.Exec(`UPDATE api_model_routes SET is_default = 0, updated_at = ? WHERE is_default = 1`, sqlTime(route.UpdatedAt)); err != nil {
			return fmt.Errorf("clear default routes: %w", err)
		}
	}
	if _, err := tx.Exec(`
		INSERT INTO api_model_routes (
			api_model, mode, workflow_id, provider_model, description,
			is_default, enabled, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(api_model) DO UPDATE SET
			mode = excluded.mode,
			workflow_id = excluded.workflow_id,
			provider_model = excluded.provider_model,
			description = excluded.description,
			is_default = excluded.is_default,
			enabled = excluded.enabled,
			updated_at = excluded.updated_at
	`, route.APIModel, route.Mode, nullIfEmpty(route.WorkflowID), nullIfEmpty(route.ProviderModel),
		route.Description, boolInt(route.IsDefault), boolInt(route.Enabled),
		sqlTime(route.CreatedAt), sqlTime(route.UpdatedAt)); err != nil {
		return fmt.Errorf("upsert api model route: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit route upsert: %w", err)
	}
	return nil
}

func (s *Storage) GetAPIModelRoute(apiModel string) (*APIModelRoute, error) {
	apiModel = strings.TrimSpace(apiModel)
	if apiModel == "" {
		return nil, ErrNotFound
	}
	return scanAPIModelRoute(s.db.QueryRow(`
		SELECT api_model, mode, COALESCE(workflow_id, ''), COALESCE(provider_model, ''),
		       COALESCE(description, ''), is_default, enabled, created_at, updated_at
		FROM api_model_routes
		WHERE api_model = ?
	`, apiModel))
}

func (s *Storage) GetDefaultAPIModelRoute() (*APIModelRoute, error) {
	return scanAPIModelRoute(s.db.QueryRow(`
		SELECT api_model, mode, COALESCE(workflow_id, ''), COALESCE(provider_model, ''),
		       COALESCE(description, ''), is_default, enabled, created_at, updated_at
		FROM api_model_routes
		WHERE is_default = 1 AND enabled = 1
		ORDER BY updated_at DESC, api_model ASC
		LIMIT 1
	`))
}

func (s *Storage) ListAPIModelRoutes(includeDisabled bool) ([]APIModelRoute, error) {
	query := `
		SELECT api_model, mode, COALESCE(workflow_id, ''), COALESCE(provider_model, ''),
		       COALESCE(description, ''), is_default, enabled, created_at, updated_at
		FROM api_model_routes
	`
	if !includeDisabled {
		query += ` WHERE enabled = 1`
	}
	query += ` ORDER BY is_default DESC, api_model ASC`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("list api model routes: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var out []APIModelRoute
	for rows.Next() {
		route, err := scanAPIModelRoute(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *route)
	}
	return out, rows.Err()
}

func (s *Storage) DeleteAPIModelRoute(apiModel string) error {
	apiModel = strings.TrimSpace(apiModel)
	if apiModel == "" {
		return ErrNotFound
	}
	result, err := s.db.Exec(`DELETE FROM api_model_routes WHERE api_model = ?`, apiModel)
	if err != nil {
		return fmt.Errorf("delete api model route: %w", err)
	}
	return requireRowsAffected(result, ErrNotFound)
}

func (s *Storage) CreateAPIUsage(record *APIUsageRecord) error {
	if record == nil {
		return fmt.Errorf("api usage record is required")
	}
	cleanAPIUsageRecord(record)
	if record.ID == "" || record.RequestID == "" || record.KeyID == "" || record.Endpoint == "" {
		return fmt.Errorf("id, request_id, key_id, and endpoint are required")
	}
	if record.Status == "" {
		record.Status = APIUsageStatusRunning
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.Exec(`
		INSERT INTO api_usage (
			id, request_id, key_id, user_id, endpoint, requested_model, resolved_model,
			workflow_id, job_id, status, http_status, stream, tokens_input,
			tokens_output, tokens_total, cost, latency_ms, error_code,
			error_message, created_at, completed_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, record.ID, record.RequestID, record.KeyID, record.UserID, record.Endpoint,
		nullIfEmpty(record.RequestedModel), nullIfEmpty(record.ResolvedModel),
		nullIfEmpty(record.WorkflowID), nullIfEmpty(record.JobID), record.Status,
		nullableInt(record.HTTPStatus), boolInt(record.Stream), record.TokensInput,
		record.TokensOutput, record.TokensTotal, record.Cost, record.LatencyMs,
		nullIfEmpty(record.ErrorCode), nullIfEmpty(record.ErrorMessage),
		sqlTime(record.CreatedAt), sqlTimePtrFromPointer(record.CompletedAt))
	if err != nil {
		return fmt.Errorf("create api usage: %w", err)
	}
	return nil
}

func (s *Storage) UpdateAPIUsageCompletion(id string, update APIUsageCompletion) error {
	return updateAPIUsageCompletion(s.db, id, update)
}

func (s *Storage) UpdateAPIUsageCompletionByJob(keyID, endpoint, jobID string, update APIUsageCompletion) error {
	return updateAPIUsageCompletionByJob(s.db, keyID, endpoint, jobID, update)
}

func (s *Storage) AttachAPIUsageJob(id, jobID string) error {
	id = strings.TrimSpace(id)
	jobID = strings.TrimSpace(jobID)
	if id == "" || jobID == "" {
		return ErrNotFound
	}
	result, err := s.db.Exec(`
		UPDATE api_usage
		SET job_id = ?
		WHERE id = ?
	`, jobID, id)
	if err != nil {
		return fmt.Errorf("attach api usage job: %w", err)
	}
	return requireRowsAffected(result, ErrNotFound)
}

func updateAPIUsageCompletion(exec sqlExecutor, id string, update APIUsageCompletion) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ErrNotFound
	}
	if update.Status == "" {
		update.Status = APIUsageStatusSucceeded
	}
	if update.CompletedAt.IsZero() {
		update.CompletedAt = time.Now().UTC()
	}
	result, err := exec.Exec(`
		UPDATE api_usage
		SET job_id = COALESCE(NULLIF(?, ''), job_id),
		    status = ?,
		    http_status = ?,
		    tokens_input = ?,
		    tokens_output = ?,
		    tokens_total = ?,
		    cost = ?,
		    latency_ms = ?,
		    error_code = ?,
		    error_message = ?,
		    completed_at = ?
		WHERE id = ?
	`, nullIfEmpty(update.JobID), update.Status, nullableInt(update.HTTPStatus),
		update.TokensInput, update.TokensOutput, update.TokensTotal, update.Cost,
		update.LatencyMs, nullIfEmpty(update.ErrorCode), nullIfEmpty(update.ErrorMessage),
		sqlTime(update.CompletedAt), id)
	if err != nil {
		return fmt.Errorf("update api usage: %w", err)
	}
	return requireRowsAffected(result, ErrNotFound)
}

func updateAPIUsageCompletionByJob(exec sqlExecutor, keyID, endpoint, jobID string, update APIUsageCompletion) error {
	keyID = strings.TrimSpace(keyID)
	endpoint = strings.TrimSpace(endpoint)
	jobID = strings.TrimSpace(jobID)
	if keyID == "" || endpoint == "" || jobID == "" {
		return ErrNotFound
	}
	if update.Status == "" {
		update.Status = APIUsageStatusSucceeded
	}
	if update.JobID == "" {
		update.JobID = jobID
	}
	if update.CompletedAt.IsZero() {
		update.CompletedAt = time.Now().UTC()
	}
	result, err := exec.Exec(`
		UPDATE api_usage
		SET job_id = COALESCE(NULLIF(?, ''), job_id),
		    status = ?,
		    http_status = ?,
		    tokens_input = ?,
		    tokens_output = ?,
		    tokens_total = ?,
		    cost = ?,
		    latency_ms = ?,
		    error_code = ?,
		    error_message = ?,
		    completed_at = ?
		WHERE key_id = ? AND endpoint = ? AND job_id = ?
	`, nullIfEmpty(update.JobID), update.Status, nullableInt(update.HTTPStatus),
		update.TokensInput, update.TokensOutput, update.TokensTotal, update.Cost,
		update.LatencyMs, nullIfEmpty(update.ErrorCode), nullIfEmpty(update.ErrorMessage),
		sqlTime(update.CompletedAt), keyID, endpoint, jobID)
	if err != nil {
		return fmt.Errorf("update api usage by job: %w", err)
	}
	return requireRowsAffected(result, ErrNotFound)
}

func (s *Storage) ListAPIUsage(filters APIUsageFilters) ([]APIUsageRecord, error) {
	query, args := buildAPIUsageQuery(filters, false)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list api usage: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	var out []APIUsageRecord
	for rows.Next() {
		record, err := scanAPIUsage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *record)
	}
	return out, rows.Err()
}

func (s *Storage) SummarizeAPIUsage(filters APIUsageFilters) (APIUsageSummary, error) {
	where, args := apiUsageWhere(filters)
	var summary APIUsageSummary
	err := s.db.QueryRow(`
		SELECT COUNT(*),
		       COALESCE(SUM(tokens_input), 0),
		       COALESCE(SUM(tokens_output), 0),
		       COALESCE(SUM(tokens_total), 0),
		       COALESCE(SUM(cost), 0)
		FROM api_usage
		WHERE `+where, args...).Scan(
		&summary.Requests, &summary.TokensInput, &summary.TokensOutput,
		&summary.TokensTotal, &summary.Cost,
	)
	if err != nil {
		return APIUsageSummary{}, fmt.Errorf("summarize api usage: %w", err)
	}
	return summary, nil
}

func (s *Storage) GetOpenAIAPIMetrics(staleBefore, now time.Time) (OpenAIAPIMetrics, error) {
	if staleBefore.IsZero() {
		staleBefore = time.Now().UTC().Add(-15 * time.Minute)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	metrics := OpenAIAPIMetrics{
		GeneratedAt:        now.UTC(),
		StaleBefore:        staleBefore.UTC(),
		RequestsByStatus:   map[string]int{},
		RequestsByEndpoint: map[string]int{},
		HTTPStatusClasses:  map[string]int{},
	}

	err := s.db.QueryRow(`
		SELECT COUNT(*),
		       COALESCE(SUM(tokens_input), 0),
		       COALESCE(SUM(tokens_output), 0),
		       COALESCE(SUM(tokens_total), 0),
		       COALESCE(SUM(cost), 0),
		       COALESCE(AVG(CASE WHEN latency_ms > 0 THEN latency_ms END), 0)
		FROM api_usage
		WHERE endpoint LIKE '/v1/%'
	`).Scan(
		&metrics.Usage.Requests,
		&metrics.Usage.TokensInput,
		&metrics.Usage.TokensOutput,
		&metrics.Usage.TokensTotal,
		&metrics.Usage.Cost,
		&metrics.AvgLatencyMs,
	)
	if err != nil {
		return OpenAIAPIMetrics{}, fmt.Errorf("summarize openai api metrics: %w", err)
	}

	if metrics.RequestsByStatus, err = queryStringIntMap(s.db, `
		SELECT status, COUNT(*)
		FROM api_usage
		WHERE endpoint LIKE '/v1/%'
		GROUP BY status
	`); err != nil {
		return OpenAIAPIMetrics{}, fmt.Errorf("openai api metrics by status: %w", err)
	}
	if metrics.RequestsByEndpoint, err = queryStringIntMap(s.db, `
		SELECT endpoint, COUNT(*)
		FROM api_usage
		WHERE endpoint LIKE '/v1/%'
		GROUP BY endpoint
	`); err != nil {
		return OpenAIAPIMetrics{}, fmt.Errorf("openai api metrics by endpoint: %w", err)
	}
	if metrics.HTTPStatusClasses, err = queryStringIntMap(s.db, `
		SELECT CASE
		         WHEN http_status BETWEEN 200 AND 299 THEN '2xx'
		         WHEN http_status BETWEEN 300 AND 399 THEN '3xx'
		         WHEN http_status BETWEEN 400 AND 499 THEN '4xx'
		         WHEN http_status BETWEEN 500 AND 599 THEN '5xx'
		         ELSE 'unknown'
		       END AS class,
		       COUNT(*)
		FROM api_usage
		WHERE endpoint LIKE '/v1/%'
		GROUP BY class
	`); err != nil {
		return OpenAIAPIMetrics{}, fmt.Errorf("openai api metrics by http status: %w", err)
	}

	if err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM api_usage
		WHERE endpoint LIKE '/v1/%'
		  AND status = ?
		  AND created_at < ?
	`, APIUsageStatusRunning, sqlTime(staleBefore)).Scan(&metrics.StaleRunningUsage); err != nil {
		return OpenAIAPIMetrics{}, fmt.Errorf("count stale openai api usage: %w", err)
	}
	if err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM api_openai_objects
		WHERE object_type = ?
		  AND background = 1
		  AND status = ?
		  AND created_at < ?
	`, OpenAIObjectTypeResponse, OpenAIObjectStatusInProgress, sqlTime(staleBefore)).Scan(&metrics.StaleBackgroundResponses); err != nil {
		return OpenAIAPIMetrics{}, fmt.Errorf("count stale openai background responses: %w", err)
	}
	if err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM api_idempotency
		WHERE COALESCE(response_body, '') = ''
		  AND expires_at > ?
	`, sqlTime(now)).Scan(&metrics.PendingIdempotency); err != nil {
		return OpenAIAPIMetrics{}, fmt.Errorf("count pending api idempotency: %w", err)
	}

	return metrics, nil
}

func queryStringIntMap(db *sql.DB, query string, args ...interface{}) (map[string]int, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck

	out := map[string]int{}
	for rows.Next() {
		var key string
		var value int
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		out[key] = value
	}
	return out, rows.Err()
}

func (s *Storage) ReserveAPIIdempotency(record *APIIdempotencyRecord) (bool, *APIIdempotencyRecord, error) {
	if record == nil {
		return false, nil, fmt.Errorf("api idempotency record is required")
	}
	cleanAPIIdempotencyRecord(record)
	if record.ID == "" || record.KeyID == "" || record.IdempotencyKey == "" || record.RequestFingerprint == "" || record.ExpiresAt.IsZero() {
		return false, nil, fmt.Errorf("id, key_id, idempotency_key, request_fingerprint, and expires_at are required")
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}

	created := false
	var existing *APIIdempotencyRecord
	err := retrySQLiteBusy(func() error {
		created = false
		existing = nil
		if _, err := s.db.Exec(`
			DELETE FROM api_idempotency
			WHERE expires_at < ?
		`, sqlTime(record.CreatedAt)); err != nil {
			return fmt.Errorf("delete expired api idempotency: %w", err)
		}

		_, err := s.db.Exec(`
			INSERT INTO api_idempotency (
				id, key_id, idempotency_key, request_fingerprint, job_id,
				response_body, http_status, created_at, expires_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, record.ID, record.KeyID, record.IdempotencyKey, record.RequestFingerprint,
			nullIfEmpty(record.JobID), nullIfEmpty(record.ResponseBody), nullableInt(record.HTTPStatus),
			sqlTime(record.CreatedAt), sqlTime(record.ExpiresAt))
		if err == nil {
			created = true
			existing = record
			return nil
		}
		if !isUniqueConstraintError(err) {
			return fmt.Errorf("insert api idempotency: %w", err)
		}
		loaded, err := s.GetAPIIdempotency(record.KeyID, record.IdempotencyKey)
		if err != nil {
			return fmt.Errorf("lookup existing api idempotency after collision: %w", err)
		}
		existing = loaded
		if existing.RequestFingerprint != record.RequestFingerprint {
			return ErrConflict
		}
		return nil
	})
	if err != nil {
		return false, existing, err
	}
	return created, existing, nil
}

func (s *Storage) CompleteAPIIdempotency(keyID, idempotencyKey, responseBody string, httpStatus int) error {
	return completeAPIIdempotency(s.db, keyID, idempotencyKey, responseBody, httpStatus)
}

func (s *Storage) CompleteAPIIdempotencyByID(id, keyID, responseBody string, httpStatus int) error {
	return completeAPIIdempotencyByID(s.db, id, keyID, responseBody, httpStatus)
}

func (s *Storage) CompleteAPIIdempotencyByJob(keyID, jobID, responseBody string, httpStatus int) error {
	return completeAPIIdempotencyByJob(s.db, keyID, jobID, responseBody, httpStatus)
}

func completeAPIIdempotency(exec sqlExecutor, keyID, idempotencyKey, responseBody string, httpStatus int) error {
	keyID = strings.TrimSpace(keyID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if keyID == "" || idempotencyKey == "" {
		return ErrNotFound
	}
	result, err := exec.Exec(`
		UPDATE api_idempotency
		SET response_body = ?, http_status = ?
		WHERE key_id = ? AND idempotency_key = ?
	`, responseBody, nullableInt(httpStatus), keyID, idempotencyKey)
	if err != nil {
		return fmt.Errorf("complete api idempotency: %w", err)
	}
	return requireRowsAffected(result, ErrNotFound)
}

func completeAPIIdempotencyByID(exec sqlExecutor, id, keyID, responseBody string, httpStatus int) error {
	id = strings.TrimSpace(id)
	keyID = strings.TrimSpace(keyID)
	if id == "" || keyID == "" {
		return ErrNotFound
	}
	result, err := exec.Exec(`
		UPDATE api_idempotency
		SET response_body = ?, http_status = ?
		WHERE id = ? AND key_id = ?
	`, responseBody, nullableInt(httpStatus), id, keyID)
	if err != nil {
		return fmt.Errorf("complete api idempotency by id: %w", err)
	}
	return requireRowsAffected(result, ErrNotFound)
}

func completeAPIIdempotencyByJob(exec sqlExecutor, keyID, jobID, responseBody string, httpStatus int) error {
	keyID = strings.TrimSpace(keyID)
	jobID = strings.TrimSpace(jobID)
	if keyID == "" || jobID == "" {
		return ErrNotFound
	}
	result, err := exec.Exec(`
		UPDATE api_idempotency
		SET response_body = ?, http_status = ?
		WHERE key_id = ? AND job_id = ?
	`, responseBody, nullableInt(httpStatus), keyID, jobID)
	if err != nil {
		return fmt.Errorf("complete api idempotency by job: %w", err)
	}
	return requireRowsAffected(result, ErrNotFound)
}

func (s *Storage) DeleteAPIIdempotency(keyID, idempotencyKey string) error {
	keyID = strings.TrimSpace(keyID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if keyID == "" || idempotencyKey == "" {
		return ErrNotFound
	}
	result, err := s.db.Exec(`DELETE FROM api_idempotency WHERE key_id = ? AND idempotency_key = ?`, keyID, idempotencyKey)
	if err != nil {
		return fmt.Errorf("delete api idempotency: %w", err)
	}
	return requireRowsAffected(result, ErrNotFound)
}

func (s *Storage) DeleteAPIIdempotencyByID(id, keyID string) error {
	id = strings.TrimSpace(id)
	keyID = strings.TrimSpace(keyID)
	if id == "" || keyID == "" {
		return ErrNotFound
	}
	result, err := s.db.Exec(`DELETE FROM api_idempotency WHERE id = ? AND key_id = ?`, id, keyID)
	if err != nil {
		return fmt.Errorf("delete api idempotency by id: %w", err)
	}
	return requireRowsAffected(result, ErrNotFound)
}

func (s *Storage) AttachAPIIdempotencyJob(keyID, idempotencyKey, jobID string) error {
	keyID = strings.TrimSpace(keyID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	jobID = strings.TrimSpace(jobID)
	if keyID == "" || idempotencyKey == "" || jobID == "" {
		return ErrNotFound
	}
	result, err := s.db.Exec(`
		UPDATE api_idempotency
		SET job_id = ?
		WHERE key_id = ? AND idempotency_key = ?
	`, jobID, keyID, idempotencyKey)
	if err != nil {
		return fmt.Errorf("attach api idempotency job: %w", err)
	}
	return requireRowsAffected(result, ErrNotFound)
}

func (s *Storage) AttachAPIIdempotencyJobByID(id, keyID, jobID string) error {
	id = strings.TrimSpace(id)
	keyID = strings.TrimSpace(keyID)
	jobID = strings.TrimSpace(jobID)
	if id == "" || keyID == "" || jobID == "" {
		return ErrNotFound
	}
	result, err := s.db.Exec(`
		UPDATE api_idempotency
		SET job_id = ?
		WHERE id = ? AND key_id = ?
	`, jobID, id, keyID)
	if err != nil {
		return fmt.Errorf("attach api idempotency job by id: %w", err)
	}
	return requireRowsAffected(result, ErrNotFound)
}

func (s *Storage) GetAPIIdempotency(keyID, idempotencyKey string) (*APIIdempotencyRecord, error) {
	keyID = strings.TrimSpace(keyID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if keyID == "" || idempotencyKey == "" {
		return nil, ErrNotFound
	}
	return scanAPIIdempotency(s.db.QueryRow(`
		SELECT id, key_id, idempotency_key, request_fingerprint, COALESCE(job_id, ''),
		       COALESCE(response_body, ''), COALESCE(http_status, 0), created_at, expires_at
		FROM api_idempotency
		WHERE key_id = ? AND idempotency_key = ?
	`, keyID, idempotencyKey))
}

func (s *Storage) seedDefaultAPIModelRoutes() error {
	defaults := []APIModelRoute{
		{
			APIModel:    "consortium-default",
			Mode:        APIModelRouteModeWorkflow,
			WorkflowID:  "reasoning-informed-captain-synthesis-cheap",
			Description: "Default Consortium ensemble workflow",
			IsDefault:   true,
			Enabled:     true,
		},
		{
			APIModel:    "consortium-majority-cheap",
			Mode:        APIModelRouteModeWorkflow,
			WorkflowID:  "reasoning-majority-pick-cheap",
			Description: "Cheap majority-pick Consortium workflow",
			Enabled:     true,
		},
		{
			APIModel:    "consortium-judge-cheap",
			Mode:        APIModelRouteModeWorkflow,
			WorkflowID:  "reasoning-judge-pick-cheap",
			Description: "Cheap judge-pick Consortium workflow",
			Enabled:     true,
		},
		{
			APIModel:      "gpt-4o-mini",
			Mode:          APIModelRouteModeDirectModel,
			ProviderModel: "openai/gpt-4o-mini",
			Description:   "OpenAI-compatible direct model alias via OpenRouter",
			Enabled:       true,
		},
		{
			APIModel:      "gpt-5-mini",
			Mode:          APIModelRouteModeDirectModel,
			ProviderModel: "openai/gpt-5-mini",
			Description:   "OpenAI-compatible direct model alias via OpenRouter",
			Enabled:       true,
		},
		{
			APIModel:      "gpt-5.2",
			Mode:          APIModelRouteModeDirectModel,
			ProviderModel: "openai/gpt-5.2",
			Description:   "OpenAI-compatible direct model alias via OpenRouter",
			Enabled:       true,
		},
	}
	for i := range defaults {
		if _, err := s.GetAPIModelRoute(defaults[i].APIModel); err == nil {
			continue
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
		if defaults[i].Mode == APIModelRouteModeWorkflow {
			if _, err := s.GetWorkflow(defaults[i].WorkflowID); err != nil {
				if errors.Is(err, ErrNotFound) {
					continue
				}
				return err
			}
		}
		if err := s.UpsertAPIModelRoute(&defaults[i]); err != nil {
			return err
		}
	}
	return nil
}

func scanAPIKey(row interface{ Scan(...interface{}) error }) (*APIKey, error) {
	var key APIKey
	var workflowID sql.NullString
	var lastUsedAt, revokedAt sql.NullTime
	err := row.Scan(
		&key.ID, &key.UserID, &key.Name, &key.Prefix, &key.KeyHash, &workflowID,
		&key.RequestsPerMinute, &key.TokensPerMinute, &key.CreatedAt, &lastUsedAt, &revokedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan api key: %w", err)
	}
	if workflowID.Valid {
		key.WorkflowID = workflowID.String
	}
	if lastUsedAt.Valid {
		key.LastUsedAt = &lastUsedAt.Time
	}
	if revokedAt.Valid {
		key.RevokedAt = &revokedAt.Time
	}
	return &key, nil
}

func scanAPIModelRoute(row interface{ Scan(...interface{}) error }) (*APIModelRoute, error) {
	var route APIModelRoute
	var isDefault, enabled int
	err := row.Scan(
		&route.APIModel, &route.Mode, &route.WorkflowID, &route.ProviderModel,
		&route.Description, &isDefault, &enabled, &route.CreatedAt, &route.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan api model route: %w", err)
	}
	route.IsDefault = isDefault != 0
	route.Enabled = enabled != 0
	return &route, nil
}

func scanAPIUsage(row interface{ Scan(...interface{}) error }) (*APIUsageRecord, error) {
	var record APIUsageRecord
	var requestedModel, resolvedModel, workflowID, jobID sql.NullString
	var errorCode, errorMessage sql.NullString
	var httpStatus, stream int
	var completedAt sql.NullTime
	err := row.Scan(
		&record.ID, &record.RequestID, &record.KeyID, &record.UserID, &record.Endpoint,
		&requestedModel, &resolvedModel, &workflowID, &jobID, &record.Status,
		&httpStatus, &stream, &record.TokensInput, &record.TokensOutput,
		&record.TokensTotal, &record.Cost, &record.LatencyMs, &errorCode,
		&errorMessage, &record.CreatedAt, &completedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan api usage: %w", err)
	}
	record.RequestedModel = requestedModel.String
	record.ResolvedModel = resolvedModel.String
	record.WorkflowID = workflowID.String
	record.JobID = jobID.String
	record.HTTPStatus = httpStatus
	record.Stream = stream != 0
	record.ErrorCode = errorCode.String
	record.ErrorMessage = errorMessage.String
	if completedAt.Valid {
		record.CompletedAt = &completedAt.Time
	}
	return &record, nil
}

func scanAPIIdempotency(row interface{ Scan(...interface{}) error }) (*APIIdempotencyRecord, error) {
	var record APIIdempotencyRecord
	err := row.Scan(
		&record.ID, &record.KeyID, &record.IdempotencyKey, &record.RequestFingerprint,
		&record.JobID, &record.ResponseBody, &record.HTTPStatus, &record.CreatedAt, &record.ExpiresAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan api idempotency: %w", err)
	}
	return &record, nil
}

func buildAPIUsageQuery(filters APIUsageFilters, count bool) (string, []interface{}) {
	where, args := apiUsageWhere(filters)
	limit := filters.Limit
	if limit <= 0 {
		limit = 100
	}
	selectClause := `
		SELECT id, request_id, key_id, user_id, endpoint, requested_model, resolved_model,
		       workflow_id, job_id, status, COALESCE(http_status, 0), stream,
		       tokens_input, tokens_output, tokens_total, cost, latency_ms,
		       error_code, error_message, created_at, completed_at
		FROM api_usage
	`
	if count {
		selectClause = `SELECT COUNT(*) FROM api_usage`
	}
	return selectClause + ` WHERE ` + where + ` ORDER BY created_at DESC, id DESC LIMIT ?`, append(args, limit)
}

func apiUsageWhere(filters APIUsageFilters) (string, []interface{}) {
	clauses := []string{"1=1"}
	args := []interface{}{}
	if filters.From != nil {
		clauses = append(clauses, "created_at >= ?")
		args = append(args, sqlTime(*filters.From))
	}
	if filters.To != nil {
		clauses = append(clauses, "created_at <= ?")
		args = append(args, sqlTime(*filters.To))
	}
	if strings.TrimSpace(filters.KeyID) != "" {
		clauses = append(clauses, "key_id = ?")
		args = append(args, strings.TrimSpace(filters.KeyID))
	}
	if strings.TrimSpace(filters.RequestedModel) != "" {
		clauses = append(clauses, "requested_model = ?")
		args = append(args, strings.TrimSpace(filters.RequestedModel))
	}
	if strings.TrimSpace(filters.Endpoint) != "" {
		clauses = append(clauses, "endpoint = ?")
		args = append(args, strings.TrimSpace(filters.Endpoint))
	}
	if strings.TrimSpace(filters.Status) != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, strings.TrimSpace(filters.Status))
	}
	return strings.Join(clauses, " AND "), args
}

func cleanAPIModelRoute(route *APIModelRoute) {
	route.APIModel = strings.TrimSpace(route.APIModel)
	route.Mode = strings.TrimSpace(route.Mode)
	route.WorkflowID = strings.TrimSpace(route.WorkflowID)
	route.ProviderModel = strings.TrimSpace(route.ProviderModel)
	route.Description = strings.TrimSpace(route.Description)
}

func cleanAPIUsageRecord(record *APIUsageRecord) {
	record.ID = strings.TrimSpace(record.ID)
	record.RequestID = strings.TrimSpace(record.RequestID)
	record.KeyID = strings.TrimSpace(record.KeyID)
	record.UserID = defaultSystemUser(strings.TrimSpace(record.UserID))
	record.Endpoint = strings.TrimSpace(record.Endpoint)
	record.RequestedModel = strings.TrimSpace(record.RequestedModel)
	record.ResolvedModel = strings.TrimSpace(record.ResolvedModel)
	record.WorkflowID = strings.TrimSpace(record.WorkflowID)
	record.JobID = strings.TrimSpace(record.JobID)
	record.Status = strings.TrimSpace(record.Status)
	record.ErrorCode = strings.TrimSpace(record.ErrorCode)
	record.ErrorMessage = strings.TrimSpace(record.ErrorMessage)
}

func cleanAPIIdempotencyRecord(record *APIIdempotencyRecord) {
	record.ID = strings.TrimSpace(record.ID)
	record.KeyID = strings.TrimSpace(record.KeyID)
	record.IdempotencyKey = strings.TrimSpace(record.IdempotencyKey)
	record.RequestFingerprint = strings.TrimSpace(record.RequestFingerprint)
	record.JobID = strings.TrimSpace(record.JobID)
}

func defaultSystemUser(userID string) string {
	if userID == "" {
		return "system"
	}
	return userID
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func sqlTimePtrFromPointer(t *time.Time) interface{} {
	if t == nil || t.IsZero() {
		return nil
	}
	return sqlTime(*t)
}

func requireRowsAffected(result sql.Result, missing error) error {
	rows, err := result.RowsAffected()
	if err == nil && rows == 0 {
		return missing
	}
	return err
}
