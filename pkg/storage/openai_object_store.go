package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	OpenAIObjectTypeResponse       = "response"
	OpenAIObjectTypeChatCompletion = "chat.completion"

	OpenAIObjectStatusInProgress = "in_progress"
	OpenAIObjectStatusCompleted  = "completed"
	OpenAIObjectStatusFailed     = "failed"
	OpenAIObjectStatusCancelled  = "cancelled"

	OpenAIItemKindInput   = "input"
	OpenAIItemKindOutput  = "output"
	OpenAIItemKindMessage = "message"

	OpenAIListOrderAsc  = "asc"
	OpenAIListOrderDesc = "desc"
)

type OpenAIObjectRecord struct {
	ID                 string     `json:"id"`
	ObjectType         string     `json:"object_type"`
	KeyID              string     `json:"key_id"`
	UserID             string     `json:"user_id"`
	Endpoint           string     `json:"endpoint"`
	JobID              string     `json:"job_id,omitempty"`
	RequestedModel     string     `json:"requested_model,omitempty"`
	ResolvedModel      string     `json:"resolved_model,omitempty"`
	WorkflowID         string     `json:"workflow_id,omitempty"`
	Status             string     `json:"status"`
	Store              bool       `json:"store"`
	Background         bool       `json:"background"`
	MetadataJSON       string     `json:"metadata_json,omitempty"`
	RequestJSON        string     `json:"request_json,omitempty"`
	ResponseJSON       string     `json:"response_json,omitempty"`
	UsageJSON          string     `json:"usage_json,omitempty"`
	ErrorCode          string     `json:"error_code,omitempty"`
	ErrorMessage       string     `json:"error_message,omitempty"`
	PreviousResponseID string     `json:"previous_response_id,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
}

type OpenAIObjectItem struct {
	ID           string    `json:"id"`
	ObjectID     string    `json:"object_id"`
	ItemKind     string    `json:"item_kind"`
	ItemIndex    int       `json:"item_index"`
	OpenAIItemID string    `json:"openai_item_id,omitempty"`
	Role         string    `json:"role,omitempty"`
	ContentJSON  string    `json:"content_json,omitempty"`
	RawJSON      string    `json:"raw_json,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type OpenAIListPageRequest struct {
	Limit int
	After string
	Order string
}

type OpenAIListPage struct {
	HasMore bool
	FirstID string
	LastID  string
}

type OpenAIObjectListFilters struct {
	KeyID      string
	ObjectType string
	Status     string
	Limit      int
	After      string
	Order      string
}

type OpenAIObjectCompletion struct {
	JobID        string
	Status       string
	ResponseJSON string
	UsageJSON    string
	ErrorCode    string
	ErrorMessage string
	CompletedAt  time.Time
}

type sqlExecutor interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

func (s *Storage) CreateOpenAIObject(record *OpenAIObjectRecord) error {
	return insertOpenAIObject(s.db, record)
}

func (s *Storage) CreateOpenAIObjectWithItems(record *OpenAIObjectRecord, items []OpenAIObjectItem) error {
	if record == nil {
		return fmt.Errorf("openai object record is required")
	}
	cleanOpenAIObject(record)
	if record.ID == "" || record.ObjectType == "" || record.KeyID == "" || record.Endpoint == "" || record.Status == "" {
		return fmt.Errorf("id, object_type, key_id, endpoint, and status are required")
	}
	now := time.Now().UTC()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	}
	return retrySQLiteBusy(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("begin create openai object with items: %w", err)
		}
		defer tx.Rollback() //nolint:errcheck

		if err := insertOpenAIObject(tx, record); err != nil {
			return err
		}
		if err := replaceOpenAIObjectItemsTx(tx, record.ID, record.KeyID, items); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit create openai object with items: %w", err)
		}
		return nil
	})
}

func (s *Storage) GetOpenAIObject(id, keyID string) (*OpenAIObjectRecord, error) {
	id = strings.TrimSpace(id)
	keyID = strings.TrimSpace(keyID)
	if id == "" || keyID == "" {
		return nil, ErrNotFound
	}
	return scanOpenAIObject(s.db.QueryRow(openAIObjectSelectSQL()+` WHERE id = ? AND key_id = ?`, id, keyID))
}

func (s *Storage) ListOpenAIObjects(filters OpenAIObjectListFilters) ([]OpenAIObjectRecord, OpenAIListPage, error) {
	filters.KeyID = strings.TrimSpace(filters.KeyID)
	filters.ObjectType = strings.TrimSpace(filters.ObjectType)
	filters.Status = strings.TrimSpace(filters.Status)
	filters.After = strings.TrimSpace(filters.After)
	order := normalizeOpenAIListOrder(filters.Order)
	limit := normalizeOpenAIListLimit(filters.Limit)
	if filters.KeyID == "" || filters.ObjectType == "" {
		return nil, OpenAIListPage{}, fmt.Errorf("key_id and object_type are required")
	}

	clauses := []string{"key_id = ?", "object_type = ?"}
	args := []interface{}{filters.KeyID, filters.ObjectType}
	if filters.Status != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, filters.Status)
	}
	if filters.After != "" {
		after, err := s.getOpenAIObjectForCursor(filters.After, filters.KeyID, filters.ObjectType)
		if err != nil {
			return nil, OpenAIListPage{}, err
		}
		if order == OpenAIListOrderAsc {
			clauses = append(clauses, "(created_at > ? OR (created_at = ? AND id > ?))")
		} else {
			clauses = append(clauses, "(created_at < ? OR (created_at = ? AND id < ?))")
		}
		args = append(args, sqlTime(after.CreatedAt), sqlTime(after.CreatedAt), after.ID)
	}
	orderSQL := "created_at DESC, id DESC"
	if order == OpenAIListOrderAsc {
		orderSQL = "created_at ASC, id ASC"
	}
	args = append(args, limit+1)

	rows, err := s.db.Query(openAIObjectSelectSQL()+`
		WHERE `+strings.Join(clauses, " AND ")+`
		ORDER BY `+orderSQL+`
		LIMIT ?
	`, args...)
	if err != nil {
		return nil, OpenAIListPage{}, fmt.Errorf("list openai objects: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []OpenAIObjectRecord
	for rows.Next() {
		record, err := scanOpenAIObject(rows)
		if err != nil {
			return nil, OpenAIListPage{}, err
		}
		out = append(out, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, OpenAIListPage{}, err
	}
	return trimOpenAIObjectPage(out, limit)
}

func (s *Storage) ListInProgressOpenAIBackgroundObjects(limit int) ([]OpenAIObjectRecord, error) {
	limit = normalizeOpenAIListLimit(limit)
	rows, err := s.db.Query(openAIObjectSelectSQL()+`
		WHERE object_type = ?
		  AND background = 1
		  AND status = ?
		  AND COALESCE(job_id, '') != ''
		ORDER BY updated_at ASC, created_at ASC, id ASC
		LIMIT ?
	`, OpenAIObjectTypeResponse, OpenAIObjectStatusInProgress, limit)
	if err != nil {
		return nil, fmt.Errorf("list in-progress openai background objects: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []OpenAIObjectRecord
	for rows.Next() {
		record, err := scanOpenAIObject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Storage) UpdateOpenAIObjectCompletion(id, keyID string, update OpenAIObjectCompletion) error {
	return updateOpenAIObjectCompletion(s.db, id, keyID, update)
}

func (s *Storage) CompleteOpenAIObjectWithItemsUsageAndIdempotency(
	id, keyID string,
	objectUpdate OpenAIObjectCompletion,
	items []OpenAIObjectItem,
	usageID string,
	usageUpdate APIUsageCompletion,
	idempotencyID string,
	idempotencyKey string,
	idempotencyResponseBody string,
	idempotencyHTTPStatus int,
) error {
	id = strings.TrimSpace(id)
	keyID = strings.TrimSpace(keyID)
	usageID = strings.TrimSpace(usageID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if id == "" || keyID == "" || usageID == "" {
		return ErrNotFound
	}
	return retrySQLiteBusy(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("begin complete openai object: %w", err)
		}
		defer tx.Rollback() //nolint:errcheck

		if err := updateOpenAIObjectCompletion(tx, id, keyID, objectUpdate); err != nil {
			return err
		}
		if err := replaceOpenAIObjectItemsTx(tx, id, keyID, items); err != nil {
			return err
		}
		if err := updateAPIUsageCompletion(tx, usageID, usageUpdate); err != nil {
			return err
		}
		if strings.TrimSpace(idempotencyID) != "" {
			if err := completeAPIIdempotencyByID(tx, idempotencyID, keyID, idempotencyResponseBody, idempotencyHTTPStatus); err != nil {
				return err
			}
		} else if idempotencyKey != "" {
			if err := completeAPIIdempotency(tx, keyID, idempotencyKey, idempotencyResponseBody, idempotencyHTTPStatus); err != nil {
				return err
			}
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit complete openai object: %w", err)
		}
		return nil
	})
}

func (s *Storage) CompleteOpenAIObjectWithItemsUsageAndIdempotencyByJob(
	id, keyID string,
	objectUpdate OpenAIObjectCompletion,
	items []OpenAIObjectItem,
	usageEndpoint string,
	jobID string,
	usageUpdate APIUsageCompletion,
	idempotencyResponseBody string,
	idempotencyHTTPStatus int,
) error {
	id = strings.TrimSpace(id)
	keyID = strings.TrimSpace(keyID)
	usageEndpoint = strings.TrimSpace(usageEndpoint)
	jobID = strings.TrimSpace(jobID)
	if id == "" || keyID == "" || usageEndpoint == "" || jobID == "" {
		return ErrNotFound
	}
	return retrySQLiteBusy(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("begin complete openai object by job: %w", err)
		}
		defer tx.Rollback() //nolint:errcheck

		if err := updateOpenAIObjectCompletion(tx, id, keyID, objectUpdate); err != nil {
			return err
		}
		if err := replaceOpenAIObjectItemsTx(tx, id, keyID, items); err != nil {
			return err
		}
		if err := updateAPIUsageCompletionByJob(tx, keyID, usageEndpoint, jobID, usageUpdate); err != nil {
			return err
		}
		if err := completeAPIIdempotencyByJob(tx, keyID, jobID, idempotencyResponseBody, idempotencyHTTPStatus); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit complete openai object by job: %w", err)
		}
		return nil
	})
}

func (s *Storage) AttachOpenAIObjectJob(id, keyID, jobID string) error {
	id = strings.TrimSpace(id)
	keyID = strings.TrimSpace(keyID)
	jobID = strings.TrimSpace(jobID)
	if id == "" || keyID == "" || jobID == "" {
		return ErrNotFound
	}
	result, err := s.db.Exec(`
		UPDATE api_openai_objects
		SET job_id = ?, updated_at = ?
		WHERE id = ? AND key_id = ?
	`, jobID, sqlTime(time.Now().UTC()), id, keyID)
	if err != nil {
		return fmt.Errorf("attach openai object job: %w", err)
	}
	return requireRowsAffected(result, ErrNotFound)
}

func (s *Storage) ReplaceOpenAIObjectItems(objectID, keyID string, items []OpenAIObjectItem) error {
	objectID = strings.TrimSpace(objectID)
	keyID = strings.TrimSpace(keyID)
	if objectID == "" || keyID == "" {
		return ErrNotFound
	}
	return retrySQLiteBusy(func() error {
		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("begin replace openai items: %w", err)
		}
		defer tx.Rollback() //nolint:errcheck

		if err := replaceOpenAIObjectItemsTx(tx, objectID, keyID, items); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit replace openai items: %w", err)
		}
		return nil
	})
}

func insertOpenAIObject(exec sqlExecutor, record *OpenAIObjectRecord) error {
	if record == nil {
		return fmt.Errorf("openai object record is required")
	}
	cleanOpenAIObject(record)
	if record.ID == "" || record.ObjectType == "" || record.KeyID == "" || record.Endpoint == "" || record.Status == "" {
		return fmt.Errorf("id, object_type, key_id, endpoint, and status are required")
	}
	now := time.Now().UTC()
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	}
	_, err := exec.Exec(`
			INSERT INTO api_openai_objects (
				id, object_type, key_id, user_id, endpoint, job_id,
				requested_model, resolved_model, workflow_id, status, store,
				background, metadata_json, request_json, response_json, usage_json,
				error_code, error_message, previous_response_id,
				created_at, updated_at, completed_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, record.ID, record.ObjectType, record.KeyID, record.UserID, record.Endpoint,
		nullIfEmpty(record.JobID), nullIfEmpty(record.RequestedModel), nullIfEmpty(record.ResolvedModel),
		nullIfEmpty(record.WorkflowID), record.Status, boolInt(record.Store), boolInt(record.Background),
		defaultJSON(record.MetadataJSON), defaultJSON(record.RequestJSON), defaultJSON(record.ResponseJSON),
		defaultJSON(record.UsageJSON), nullIfEmpty(record.ErrorCode), nullIfEmpty(record.ErrorMessage),
		nullIfEmpty(record.PreviousResponseID), sqlTime(record.CreatedAt), sqlTime(record.UpdatedAt),
		sqlTimePtrFromPointer(record.CompletedAt))
	if err != nil {
		return fmt.Errorf("create openai object: %w", err)
	}
	return nil
}

func updateOpenAIObjectCompletion(exec sqlExecutor, id, keyID string, update OpenAIObjectCompletion) error {
	id = strings.TrimSpace(id)
	keyID = strings.TrimSpace(keyID)
	if id == "" || keyID == "" {
		return ErrNotFound
	}
	update.Status = strings.TrimSpace(update.Status)
	if update.Status == "" {
		update.Status = OpenAIObjectStatusCompleted
	}
	if update.CompletedAt.IsZero() && isTerminalOpenAIObjectStatus(update.Status) {
		update.CompletedAt = time.Now().UTC()
	}
	updatedAt := time.Now().UTC()
	result, err := exec.Exec(`
			UPDATE api_openai_objects
			SET job_id = COALESCE(NULLIF(?, ''), job_id),
			    status = ?,
			    response_json = ?,
			    usage_json = ?,
			    error_code = ?,
			    error_message = ?,
			    updated_at = ?,
			    completed_at = ?
			WHERE id = ? AND key_id = ?
			  AND (status != ? OR ? = ?)
		`, strings.TrimSpace(update.JobID), update.Status, defaultJSON(update.ResponseJSON),
		defaultJSON(update.UsageJSON), nullIfEmpty(update.ErrorCode), nullIfEmpty(update.ErrorMessage),
		sqlTime(updatedAt), sqlTimePtrFromValue(update.CompletedAt), id, keyID,
		OpenAIObjectStatusCancelled, update.Status, OpenAIObjectStatusCancelled)
	if err != nil {
		return fmt.Errorf("update openai object completion: %w", err)
	}
	return requireRowsAffected(result, ErrNotFound)
}

func replaceOpenAIObjectItemsTx(tx *sql.Tx, objectID, keyID string, items []OpenAIObjectItem) error {
	objectID = strings.TrimSpace(objectID)
	keyID = strings.TrimSpace(keyID)
	if objectID == "" || keyID == "" {
		return ErrNotFound
	}
	var existingID string
	if err := tx.QueryRow(`SELECT id FROM api_openai_objects WHERE id = ? AND key_id = ?`, objectID, keyID).Scan(&existingID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("lookup openai object for items: %w", err)
	}

	if _, err := tx.Exec(`DELETE FROM api_openai_items WHERE object_id = ?`, objectID); err != nil {
		return fmt.Errorf("delete openai items: %w", err)
	}
	stmt, err := tx.Prepare(`
		INSERT INTO api_openai_items (
			id, object_id, item_kind, item_index, openai_item_id,
			role, content_json, raw_json, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare insert openai items: %w", err)
	}
	defer stmt.Close() //nolint:errcheck

	now := time.Now().UTC()
	for i := range items {
		cleanOpenAIObjectItem(&items[i])
		if items[i].ObjectID == "" {
			items[i].ObjectID = objectID
		}
		if items[i].ID == "" || items[i].ObjectID != objectID || items[i].ItemKind == "" {
			return fmt.Errorf("id, matching object_id, and item_kind are required")
		}
		if items[i].CreatedAt.IsZero() {
			items[i].CreatedAt = now
		}
		if _, err := stmt.Exec(
			items[i].ID, items[i].ObjectID, items[i].ItemKind, items[i].ItemIndex,
			nullIfEmpty(items[i].OpenAIItemID), nullIfEmpty(items[i].Role),
			defaultJSON(items[i].ContentJSON), defaultJSON(items[i].RawJSON),
			sqlTime(items[i].CreatedAt),
		); err != nil {
			return fmt.Errorf("insert openai item: %w", err)
		}
	}
	return nil
}

func (s *Storage) ListOpenAIObjectItems(objectID, keyID, itemKind string, pageRequests ...OpenAIListPageRequest) ([]OpenAIObjectItem, OpenAIListPage, error) {
	objectID = strings.TrimSpace(objectID)
	keyID = strings.TrimSpace(keyID)
	itemKind = strings.TrimSpace(itemKind)
	if objectID == "" || keyID == "" || itemKind == "" {
		return nil, OpenAIListPage{}, ErrNotFound
	}
	if _, err := s.GetOpenAIObject(objectID, keyID); err != nil {
		return nil, OpenAIListPage{}, err
	}
	pageReq := OpenAIListPageRequest{}
	if len(pageRequests) > 0 {
		pageReq = pageRequests[0]
	}
	pageReq.After = strings.TrimSpace(pageReq.After)
	order := normalizeOpenAIListOrder(pageReq.Order)
	limit := normalizeOpenAIListLimit(pageReq.Limit)

	clauses := []string{"object_id = ?", "item_kind = ?"}
	args := []interface{}{objectID, itemKind}
	if pageReq.After != "" {
		after, err := s.getOpenAIObjectItemForCursor(objectID, itemKind, pageReq.After)
		if err != nil {
			return nil, OpenAIListPage{}, err
		}
		if order == OpenAIListOrderDesc {
			clauses = append(clauses, "(item_index < ? OR (item_index = ? AND id < ?))")
		} else {
			clauses = append(clauses, "(item_index > ? OR (item_index = ? AND id > ?))")
		}
		args = append(args, after.ItemIndex, after.ItemIndex, after.ID)
	}
	orderSQL := "item_index ASC, id ASC"
	if order == OpenAIListOrderDesc {
		orderSQL = "item_index DESC, id DESC"
	}
	args = append(args, limit+1)

	rows, err := s.db.Query(`
		SELECT id, object_id, item_kind, item_index, COALESCE(openai_item_id, ''),
		       COALESCE(role, ''), COALESCE(content_json, '{}'), COALESCE(raw_json, '{}'), created_at
		FROM api_openai_items
		WHERE `+strings.Join(clauses, " AND ")+`
		ORDER BY `+orderSQL+`
		LIMIT ?
	`, args...)
	if err != nil {
		return nil, OpenAIListPage{}, fmt.Errorf("list openai object items: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var out []OpenAIObjectItem
	for rows.Next() {
		item, err := scanOpenAIObjectItem(rows)
		if err != nil {
			return nil, OpenAIListPage{}, err
		}
		out = append(out, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, OpenAIListPage{}, err
	}
	return trimOpenAIItemPage(out, limit)
}

func (s *Storage) getOpenAIObjectForCursor(id, keyID, objectType string) (*OpenAIObjectRecord, error) {
	return scanOpenAIObject(s.db.QueryRow(openAIObjectSelectSQL()+`
		WHERE id = ? AND key_id = ? AND object_type = ?
	`, id, keyID, objectType))
}

func (s *Storage) getOpenAIObjectItemForCursor(objectID, itemKind, id string) (*OpenAIObjectItem, error) {
	return scanOpenAIObjectItem(s.db.QueryRow(`
		SELECT id, object_id, item_kind, item_index, COALESCE(openai_item_id, ''),
		       COALESCE(role, ''), COALESCE(content_json, '{}'), COALESCE(raw_json, '{}'), created_at
		FROM api_openai_items
		WHERE object_id = ? AND item_kind = ? AND id = ?
	`, objectID, itemKind, id))
}

func openAIObjectSelectSQL() string {
	return `
		SELECT id, object_type, key_id, user_id, endpoint, COALESCE(job_id, ''),
		       COALESCE(requested_model, ''), COALESCE(resolved_model, ''),
		       COALESCE(workflow_id, ''), status, store, background,
		       COALESCE(metadata_json, '{}'), COALESCE(request_json, '{}'),
		       COALESCE(response_json, '{}'), COALESCE(usage_json, '{}'),
		       COALESCE(error_code, ''), COALESCE(error_message, ''),
		       COALESCE(previous_response_id, ''), created_at, updated_at, completed_at
		FROM api_openai_objects
	`
}

func scanOpenAIObject(row interface{ Scan(...interface{}) error }) (*OpenAIObjectRecord, error) {
	var record OpenAIObjectRecord
	var store, background int
	var completedAt sql.NullTime
	err := row.Scan(
		&record.ID, &record.ObjectType, &record.KeyID, &record.UserID, &record.Endpoint,
		&record.JobID, &record.RequestedModel, &record.ResolvedModel, &record.WorkflowID,
		&record.Status, &store, &background, &record.MetadataJSON, &record.RequestJSON,
		&record.ResponseJSON, &record.UsageJSON, &record.ErrorCode, &record.ErrorMessage,
		&record.PreviousResponseID, &record.CreatedAt, &record.UpdatedAt, &completedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan openai object: %w", err)
	}
	record.Store = store != 0
	record.Background = background != 0
	if completedAt.Valid {
		record.CompletedAt = &completedAt.Time
	}
	return &record, nil
}

func scanOpenAIObjectItem(row interface{ Scan(...interface{}) error }) (*OpenAIObjectItem, error) {
	var item OpenAIObjectItem
	err := row.Scan(
		&item.ID, &item.ObjectID, &item.ItemKind, &item.ItemIndex, &item.OpenAIItemID,
		&item.Role, &item.ContentJSON, &item.RawJSON, &item.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan openai object item: %w", err)
	}
	return &item, nil
}

func cleanOpenAIObject(record *OpenAIObjectRecord) {
	record.ID = strings.TrimSpace(record.ID)
	record.ObjectType = strings.TrimSpace(record.ObjectType)
	record.KeyID = strings.TrimSpace(record.KeyID)
	record.UserID = defaultSystemUser(strings.TrimSpace(record.UserID))
	record.Endpoint = strings.TrimSpace(record.Endpoint)
	record.JobID = strings.TrimSpace(record.JobID)
	record.RequestedModel = strings.TrimSpace(record.RequestedModel)
	record.ResolvedModel = strings.TrimSpace(record.ResolvedModel)
	record.WorkflowID = strings.TrimSpace(record.WorkflowID)
	record.Status = strings.TrimSpace(record.Status)
	record.ErrorCode = strings.TrimSpace(record.ErrorCode)
	record.ErrorMessage = strings.TrimSpace(record.ErrorMessage)
	record.PreviousResponseID = strings.TrimSpace(record.PreviousResponseID)
}

func cleanOpenAIObjectItem(item *OpenAIObjectItem) {
	item.ID = strings.TrimSpace(item.ID)
	item.ObjectID = strings.TrimSpace(item.ObjectID)
	item.ItemKind = strings.TrimSpace(item.ItemKind)
	item.OpenAIItemID = strings.TrimSpace(item.OpenAIItemID)
	item.Role = strings.TrimSpace(item.Role)
}

func normalizeOpenAIListLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func normalizeOpenAIListOrder(order string) string {
	if strings.EqualFold(strings.TrimSpace(order), OpenAIListOrderAsc) {
		return OpenAIListOrderAsc
	}
	return OpenAIListOrderDesc
}

func trimOpenAIObjectPage(rows []OpenAIObjectRecord, limit int) ([]OpenAIObjectRecord, OpenAIListPage, error) {
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	page := OpenAIListPage{HasMore: hasMore}
	if len(rows) > 0 {
		page.FirstID = rows[0].ID
		page.LastID = rows[len(rows)-1].ID
	}
	return rows, page, nil
}

func trimOpenAIItemPage(rows []OpenAIObjectItem, limit int) ([]OpenAIObjectItem, OpenAIListPage, error) {
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	page := OpenAIListPage{HasMore: hasMore}
	if len(rows) > 0 {
		page.FirstID = rows[0].ID
		page.LastID = rows[len(rows)-1].ID
	}
	return rows, page, nil
}

func defaultJSON(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "{}"
	}
	return value
}

func sqlTimePtrFromValue(t time.Time) interface{} {
	if t.IsZero() {
		return nil
	}
	return sqlTime(t)
}

func isTerminalOpenAIObjectStatus(status string) bool {
	switch status {
	case OpenAIObjectStatusCompleted, OpenAIObjectStatusFailed, OpenAIObjectStatusCancelled:
		return true
	default:
		return false
	}
}
