package benchloop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSessionLogPath_PrefersExistingArchivePath(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "benchmarks", "loop", "archive", "sess-a", "iterations", "2.log")
	if err := os.MkdirAll(filepath.Dir(archivePath), 0755); err != nil {
		t.Fatalf("mkdir archive path: %v", err)
	}
	if err := os.WriteFile(archivePath, []byte("archive-log"), 0644); err != nil {
		t.Fatalf("write archive log: %v", err)
	}

	record := SessionRecord{
		LoopSessionID: "sess-a",
		Attempt:       2,
		LogPath:       filepath.Join(dir, "benchmarks", "loop", "iterations", "2.log"), // stale
	}

	got := ResolveSessionLogPath(dir, record)
	if got != archivePath {
		t.Fatalf("resolved path mismatch:\n got: %s\nwant: %s", got, archivePath)
	}
}

func TestResolveSessionLogPath_UsesCurrentSessionPath(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "benchmarks", "loop", "sessions", "sess-b", "iterations", "5.log")
	if err := os.MkdirAll(filepath.Dir(currentPath), 0755); err != nil {
		t.Fatalf("mkdir session path: %v", err)
	}
	if err := os.WriteFile(currentPath, []byte("current-log"), 0644); err != nil {
		t.Fatalf("write current log: %v", err)
	}

	record := SessionRecord{
		LoopSessionID: "sess-b",
		Attempt:       5,
		LogPath:       "",
	}
	got := ResolveSessionLogPath(dir, record)
	if got != currentPath {
		t.Fatalf("resolved path mismatch:\n got: %s\nwant: %s", got, currentPath)
	}
}

func TestResolveSessionLogPath_UsesArchivedSessionScopedPath(t *testing.T) {
	dir := t.TempDir()
	archivedPath := filepath.Join(dir, "benchmarks", "loop", "archive", "sess-c", "sessions", "iterations", "3.log")
	if err := os.MkdirAll(filepath.Dir(archivedPath), 0755); err != nil {
		t.Fatalf("mkdir archived session path: %v", err)
	}
	if err := os.WriteFile(archivedPath, []byte("archived-session-log"), 0644); err != nil {
		t.Fatalf("write archived session log: %v", err)
	}

	record := SessionRecord{
		LoopSessionID: "sess-c",
		Attempt:       3,
	}
	got := ResolveSessionLogPath(dir, record)
	if got != archivedPath {
		t.Fatalf("resolved path mismatch:\n got: %s\nwant: %s", got, archivedPath)
	}
}
