package storage

import (
	"context"
	"strings"
	"time"
)

// IsRetryableSQLiteError reports whether err is a transient SQLite contention
// error that should be retried with backoff.
func IsRetryableSQLiteError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database table is locked") ||
		strings.Contains(msg, "database schema is locked") ||
		strings.Contains(msg, "database is busy") ||
		strings.Contains(msg, "sqlite_busy") ||
		strings.Contains(msg, "cannot start a transaction within a transaction")
}

// retrySQLiteBusy retries fn up to 6 times with exponential backoff when
// SQLite returns a transient contention error.
func retrySQLiteBusy(fn func() error) error {
	const maxAttempts = 6
	backoff := 10 * time.Millisecond
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := fn(); err == nil {
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

// isUniqueConstraintError reports whether err is a SQLite UNIQUE constraint
// violation. This is separate from IsRetryableSQLiteError because UNIQUE
// violations are only retryable in specific contexts (e.g. sequence assignment
// where a retry will recompute the next sequence).
func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint failed") ||
		strings.Contains(msg, "unique constraint")
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
