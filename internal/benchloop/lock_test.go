package benchloop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunLockAcquireRelease(t *testing.T) {
	dir := t.TempDir()

	lock, err := AcquireRunLock(dir, false)
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	if lock.path == "" {
		t.Fatal("expected lock path")
	}

	_, err = AcquireRunLock(dir, false)
	if err == nil {
		t.Fatal("expected second acquire to fail while lock is held")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("unexpected second acquire error: %v", err)
	}

	if err := lock.SetSessionID("sess-1"); err != nil {
		t.Fatalf("set session id: %v", err)
	}

	data, err := os.ReadFile(LockPath(dir))
	if err != nil {
		t.Fatalf("read lock file: %v", err)
	}
	var meta RunLockMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("parse lock metadata: %v", err)
	}
	if meta.SessionID != "sess-1" {
		t.Fatalf("unexpected session id in lock metadata: %q", meta.SessionID)
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("release lock: %v", err)
	}

	// Should be acquirable again after release.
	lock2, err := AcquireRunLock(dir, false)
	if err != nil {
		t.Fatalf("re-acquire lock after release: %v", err)
	}
	_ = lock2.Release()
}

func TestRunLockAcquireRemovesStaleLock(t *testing.T) {
	dir := t.TempDir()
	lockPath := LockPath(dir)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		t.Fatalf("mkdir lock dir: %v", err)
	}

	// PID 999999 should not exist in normal test environments.
	stale := RunLockMetadata{
		PID:       999999,
		CreatedAt: time.Now().Add(-1 * time.Hour),
		Workdir:   dir,
		Resume:    false,
	}
	data, _ := json.Marshal(stale)
	if err := os.WriteFile(lockPath, data, 0644); err != nil {
		t.Fatalf("write stale lock: %v", err)
	}

	lock, err := AcquireRunLock(dir, false)
	if err != nil {
		t.Fatalf("acquire lock after stale lock: %v", err)
	}
	_ = lock.Release()
}
