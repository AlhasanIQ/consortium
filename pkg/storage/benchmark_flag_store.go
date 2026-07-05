package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// DatasetFlag represents a suspected-wrong-gold-label flag for a benchmark item.
type DatasetFlag struct {
	ID             int64      `json:"id"`
	Benchmark      string     `json:"benchmark"`
	Split          string     `json:"split"`
	ItemID         string     `json:"item_id"`
	DatasetVersion string     `json:"dataset_version"`
	Reason         string     `json:"reason"`
	Source         string     `json:"source"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	ResolvedBy     string     `json:"resolved_by,omitempty"`
	ResolvedReason string     `json:"resolved_reason,omitempty"`
}

const datasetFlagColumns = `id, benchmark, split, item_id, dataset_version, reason, source, created_at, updated_at, resolved_at, resolved_by, resolved_reason`

func scanDatasetFlag(row interface{ Scan(...interface{}) error }) (*DatasetFlag, error) {
	var f DatasetFlag
	var resolvedAt sql.NullTime
	var resolvedBy, resolvedReason sql.NullString
	err := row.Scan(
		&f.ID, &f.Benchmark, &f.Split, &f.ItemID, &f.DatasetVersion,
		&f.Reason, &f.Source, &f.CreatedAt, &f.UpdatedAt,
		&resolvedAt, &resolvedBy, &resolvedReason,
	)
	if err != nil {
		return nil, err
	}
	if resolvedAt.Valid {
		f.ResolvedAt = &resolvedAt.Time
	}
	if resolvedBy.Valid {
		f.ResolvedBy = resolvedBy.String
	}
	if resolvedReason.Valid {
		f.ResolvedReason = resolvedReason.String
	}
	return &f, nil
}

func scanDatasetFlags(rows *sql.Rows) ([]DatasetFlag, error) {
	var flags []DatasetFlag
	for rows.Next() {
		f, err := scanDatasetFlag(rows)
		if err != nil {
			return nil, err
		}
		flags = append(flags, *f)
	}
	return flags, rows.Err()
}

// UpsertActiveFlags creates or updates active (unresolved) flags.
// If an active flag already exists for the same (benchmark, split, item_id),
// its reason/source/dataset_version are updated. Returns the resulting flags.
func (s *Storage) UpsertActiveFlags(flags []DatasetFlag) ([]DatasetFlag, error) {
	if len(flags) == 0 {
		return nil, nil
	}

	const maxAttempts = 5
	backoff := 50 * time.Millisecond
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err := s.upsertActiveFlagsOnce(flags)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if !IsRetryableSQLiteError(lastErr) || attempt == maxAttempts {
			break
		}
		time.Sleep(backoff)
		if backoff < time.Second {
			backoff *= 2
		}
	}
	return nil, lastErr
}

func (s *Storage) upsertActiveFlagsOnce(flags []DatasetFlag) ([]DatasetFlag, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	now := sqlTime(time.Now())
	results := make([]DatasetFlag, 0, len(flags))

	for _, f := range flags {
		benchmark := strings.TrimSpace(f.Benchmark)
		split := strings.TrimSpace(f.Split)
		itemID := strings.TrimSpace(f.ItemID)
		reason := strings.TrimSpace(f.Reason)
		if benchmark == "" || split == "" || itemID == "" || reason == "" {
			return nil, fmt.Errorf("benchmark, split, item_id, and reason are required")
		}

		// INSERT with ON CONFLICT on the partial unique index (active flags only).
		// SQLite does not support ON CONFLICT on partial indexes directly, so we
		// use a two-step approach: try to find an existing active flag and update it,
		// or insert a new one.
		var existingID sql.NullInt64
		err := tx.QueryRow(
			`SELECT id FROM benchmark_dataset_flags
			 WHERE benchmark = ? AND split = ? AND item_id = ? AND resolved_at IS NULL`,
			benchmark, split, itemID,
		).Scan(&existingID)

		var flagID int64
		if err == nil && existingID.Valid {
			// Update existing active flag
			_, execErr := tx.Exec(
				`UPDATE benchmark_dataset_flags
				 SET reason = ?, source = ?, dataset_version = ?, updated_at = ?
				 WHERE id = ?`,
				reason, f.Source, f.DatasetVersion, now, existingID.Int64,
			)
			if execErr != nil {
				return nil, fmt.Errorf("update flag %d: %w", existingID.Int64, execErr)
			}
			flagID = existingID.Int64
		} else if errors.Is(err, sql.ErrNoRows) {
			// Insert new flag
			res, execErr := tx.Exec(
				`INSERT INTO benchmark_dataset_flags
				 (benchmark, split, item_id, dataset_version, reason, source, created_at, updated_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				benchmark, split, itemID, f.DatasetVersion, reason, f.Source, now, now,
			)
			if execErr != nil {
				return nil, fmt.Errorf("insert flag: %w", execErr)
			}
			flagID, _ = res.LastInsertId()
		} else {
			return nil, fmt.Errorf("check existing flag: %w", err)
		}

		// Re-read the flag to return consistent data
		row := tx.QueryRow(
			`SELECT `+datasetFlagColumns+` FROM benchmark_dataset_flags WHERE id = ?`,
			flagID,
		)
		result, scanErr := scanDatasetFlag(row)
		if scanErr != nil {
			return nil, fmt.Errorf("read back flag %d: %w", flagID, scanErr)
		}
		results = append(results, *result)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return results, nil
}

// ResolveFlag marks a flag as resolved by its primary key ID.
func (s *Storage) ResolveFlag(id int64, resolvedBy, resolvedReason string) error {
	resolvedReason = strings.TrimSpace(resolvedReason)
	if resolvedReason == "" {
		return fmt.Errorf("resolved_reason is required")
	}

	now := sqlTime(time.Now())
	res, err := s.db.Exec(
		`UPDATE benchmark_dataset_flags
		 SET resolved_at = ?, resolved_by = ?, resolved_reason = ?, updated_at = ?
		 WHERE id = ? AND resolved_at IS NULL`,
		now, resolvedBy, resolvedReason, now, id,
	)
	if err != nil {
		return fmt.Errorf("resolve flag %d: %w", id, err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("flag %d not found or already resolved: %w", id, ErrNotFound)
	}
	return nil
}

// ResolveFlagByItem resolves the active flag matching the natural key (benchmark, split, item_id).
func (s *Storage) ResolveFlagByItem(benchmark, split, itemID, resolvedBy, resolvedReason string) error {
	resolvedReason = strings.TrimSpace(resolvedReason)
	if resolvedReason == "" {
		return fmt.Errorf("resolved_reason is required")
	}
	benchmark = strings.TrimSpace(benchmark)
	split = strings.TrimSpace(split)
	itemID = strings.TrimSpace(itemID)
	if benchmark == "" || split == "" || itemID == "" {
		return fmt.Errorf("benchmark, split, and item_id are required")
	}

	now := sqlTime(time.Now())
	res, err := s.db.Exec(
		`UPDATE benchmark_dataset_flags
		 SET resolved_at = ?, resolved_by = ?, resolved_reason = ?, updated_at = ?
		 WHERE benchmark = ? AND split = ? AND item_id = ? AND resolved_at IS NULL`,
		now, resolvedBy, resolvedReason, now, benchmark, split, itemID,
	)
	if err != nil {
		return fmt.Errorf("resolve flag by item: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("no active flag found for %s/%s/%s: %w", benchmark, split, itemID, ErrNotFound)
	}
	return nil
}

// ListFlags lists flags for a benchmark/split. If activeOnly is true, only unresolved flags are returned.
func (s *Storage) ListFlags(benchmark, split string, activeOnly bool) ([]DatasetFlag, error) {
	query := `SELECT ` + datasetFlagColumns + ` FROM benchmark_dataset_flags WHERE 1=1`
	args := []interface{}{}

	if benchmark != "" {
		query += ` AND benchmark = ?`
		args = append(args, benchmark)
	}
	if split != "" {
		query += ` AND split = ?`
		args = append(args, split)
	}
	if activeOnly {
		query += ` AND resolved_at IS NULL`
	}
	query += ` ORDER BY created_at DESC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list flags: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	return scanDatasetFlags(rows)
}

// ListFlagsByItems returns active flags for a batch of item IDs within a benchmark/split.
// Used to enrich analysis responses with flag data.
func (s *Storage) ListFlagsByItems(benchmark, split string, itemIDs []string) ([]DatasetFlag, error) {
	if len(itemIDs) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(itemIDs))
	args := []interface{}{benchmark, split}
	for i, id := range itemIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}

	query := fmt.Sprintf(
		`SELECT %s FROM benchmark_dataset_flags
		 WHERE benchmark = ? AND split = ? AND item_id IN (%s) AND resolved_at IS NULL
		 ORDER BY item_id`,
		datasetFlagColumns, strings.Join(placeholders, ","),
	)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list flags by items: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	return scanDatasetFlags(rows)
}

// DeleteFlag permanently removes a flag by ID. Admin cleanup only.
func (s *Storage) DeleteFlag(id int64) error {
	res, err := s.db.Exec(`DELETE FROM benchmark_dataset_flags WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete flag %d: %w", id, err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("flag %d not found: %w", id, ErrNotFound)
	}
	return nil
}
