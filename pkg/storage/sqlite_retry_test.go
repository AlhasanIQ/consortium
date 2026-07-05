package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestIsRetryableSQLiteError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"generic error", errors.New("something broke"), false},
		{"database is locked", errors.New("database is locked"), true},
		{"database table is locked", errors.New("database table is locked"), true},
		{"database schema is locked", errors.New("database schema is locked"), true},
		{"database is busy", errors.New("database is busy"), true},
		{"sqlite_busy", errors.New("sqlite_busy"), true},
		{"transaction within transaction", errors.New("cannot start a transaction within a transaction"), true},
		{"case insensitive", errors.New("DATABASE IS LOCKED"), true},
		{"embedded message", errors.New("error: database is locked (code 5)"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryableSQLiteError(tt.err); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsUniqueConstraintError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"generic error", errors.New("something broke"), false},
		{"unique constraint failed", errors.New("UNIQUE constraint failed: table.col"), true},
		{"unique constraint", errors.New("unique constraint violation"), true},
		{"case insensitive", errors.New("UNIQUE CONSTRAINT FAILED"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUniqueConstraintError(tt.err); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRetrySQLiteBusy_SuccessFirstAttempt(t *testing.T) {
	calls := 0
	err := retrySQLiteBusy(func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestRetrySQLiteBusy_NonRetryableError(t *testing.T) {
	calls := 0
	testErr := errors.New("syntax error")
	err := retrySQLiteBusy(func() error {
		calls++
		return testErr
	})
	if !errors.Is(err, testErr) {
		t.Errorf("expected original error, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call (no retry for non-retryable), got %d", calls)
	}
}

func TestRetrySQLiteBusy_RetriesThenSucceeds(t *testing.T) {
	calls := 0
	err := retrySQLiteBusy(func() error {
		calls++
		if calls < 3 {
			return errors.New("database is locked")
		}
		return nil
	})
	if err != nil {
		t.Errorf("expected nil error after retries, got %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestRetrySQLiteBusy_ExhaustsRetries(t *testing.T) {
	calls := 0
	err := retrySQLiteBusy(func() error {
		calls++
		return errors.New("database is locked")
	})
	if err == nil {
		t.Error("expected error after exhausting retries")
	}
	if calls != 6 {
		t.Errorf("expected 6 calls (max attempts), got %d", calls)
	}
}

func TestSleepWithContext_NormalSleep(t *testing.T) {
	ctx := context.Background()
	ok := sleepWithContext(ctx, 1*time.Millisecond)
	if !ok {
		t.Error("expected true for normal sleep")
	}
}

func TestSleepWithContext_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	ok := sleepWithContext(ctx, 1*time.Second)
	if ok {
		t.Error("expected false for cancelled context")
	}
}

func TestSleepWithContext_ZeroDuration(t *testing.T) {
	ctx := context.Background()
	ok := sleepWithContext(ctx, 0)
	if !ok {
		t.Error("expected true for zero duration")
	}
}

func TestSleepWithContext_NegativeDuration(t *testing.T) {
	ctx := context.Background()
	ok := sleepWithContext(ctx, -1*time.Second)
	if !ok {
		t.Error("expected true for negative duration")
	}
}
