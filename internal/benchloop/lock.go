package benchloop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// RunLockMetadata is persisted to benchmarks/loop/.lock while a loop is active.
type RunLockMetadata struct {
	PID       int       `json:"pid"`
	CreatedAt time.Time `json:"created_at"`
	SessionID string    `json:"session_id,omitempty"`
	Workdir   string    `json:"workdir"`
	Resume    bool      `json:"resume"`
}

// RunLock represents an acquired loop lock.
type RunLock struct {
	path string
	meta RunLockMetadata
}

// LockPath returns the path to the benchloop lock file.
func LockPath(workdir string) string {
	return filepath.Join(workdir, "benchmarks", "loop", ".lock")
}

// AcquireRunLock acquires an exclusive loop lock, removing stale locks when possible.
func AcquireRunLock(workdir string, resume bool) (*RunLock, error) {
	path := LockPath(workdir)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}

	meta := RunLockMetadata{
		PID:       os.Getpid(),
		CreatedAt: time.Now(),
		Workdir:   workdir,
		Resume:    resume,
	}

	// Try once; if a stale lock exists, remove it and retry once.
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err == nil {
			enc := json.NewEncoder(f)
			enc.SetIndent("", "  ")
			writeErr := enc.Encode(meta)
			closeErr := f.Close()
			if writeErr != nil {
				_ = os.Remove(path)
				return nil, fmt.Errorf("write lock file: %w", writeErr)
			}
			if closeErr != nil {
				_ = os.Remove(path)
				return nil, fmt.Errorf("close lock file: %w", closeErr)
			}
			return &RunLock{path: path, meta: meta}, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("acquire lock: %w", err)
		}

		existing, readErr := readRunLockMetadata(path)
		if readErr == nil && existing.PID > 0 && processExists(existing.PID) {
			return nil, fmt.Errorf(
				"benchloop is already running (pid=%d session=%q lock=%s)",
				existing.PID,
				existing.SessionID,
				path,
			)
		}

		// Stale or unreadable lock file: best-effort cleanup and retry once.
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return nil, fmt.Errorf("remove stale lock %s: %w", path, removeErr)
		}
	}

	return nil, fmt.Errorf("failed to acquire lock %s", path)
}

// SetSessionID updates the lock metadata with the resolved loop session id.
func (l *RunLock) SetSessionID(sessionID string) error {
	if l == nil {
		return nil
	}
	l.meta.SessionID = sessionID
	data, err := json.MarshalIndent(l.meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal lock metadata: %w", err)
	}
	if err := os.WriteFile(l.path, data, 0644); err != nil {
		return fmt.Errorf("update lock metadata: %w", err)
	}
	return nil
}

// Release removes the lock file.
func (l *RunLock) Release() error {
	if l == nil {
		return nil
	}
	if err := os.Remove(l.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("release lock %s: %w", l.path, err)
	}
	return nil
}

func readRunLockMetadata(path string) (*RunLockMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var meta RunLockMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// ReadRunLockMetadata reads the active run lock metadata from benchmarks/loop/.lock.
func ReadRunLockMetadata(workdir string) (*RunLockMetadata, error) {
	return readRunLockMetadata(LockPath(workdir))
}

// IsRunLockProcessAlive reports whether the lock PID is currently alive.
func IsRunLockProcessAlive(meta *RunLockMetadata) bool {
	if meta == nil {
		return false
	}
	return processExists(meta.PID)
}
