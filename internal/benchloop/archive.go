package benchloop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type archiveOptions struct {
	includeMemory        bool
	includeSessionIndex  bool
	requireValidDecision bool
}

// ArchivePreviousSession moves prior session artifacts into archive/{session_id}/
// so a fresh run starts clean without losing history. The memory file is preserved
// in place (not archived) since it normally accumulates across sessions.
func ArchivePreviousSession(workdir string) (string, error) {
	return archiveSession(workdir, archiveOptions{})
}

// ArchiveCleanSlate archives all loop runtime state (including loop memory and
// session index) so the next run starts from a blank working state.
//
// By default it validates decision.json before archiving when present. Set force
// to true to bypass decision validation.
func ArchiveCleanSlate(workdir string, force bool) (string, error) {
	return archiveSession(workdir, archiveOptions{
		includeMemory:        true,
		includeSessionIndex:  true,
		requireValidDecision: !force,
	})
}

func archiveSession(workdir string, opts archiveOptions) (string, error) {
	loopDir := filepath.Join(workdir, "benchmarks", "loop")

	// Read existing state to get the session ID for the archive directory name.
	stateData, err := os.ReadFile(filepath.Join(loopDir, "state.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil // no previous session
		}
		return "", fmt.Errorf("read previous state: %w", err)
	}

	var prevState struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(stateData, &prevState); err != nil {
		return "", fmt.Errorf("parse previous state: %w", err)
	}
	if prevState.SessionID == "" {
		return "", nil
	}
	if opts.requireValidDecision {
		if err := ensureDecisionValidForArchive(workdir); err != nil {
			return "", err
		}
	}

	archiveDir := filepath.Join(loopDir, "archive", prevState.SessionID)
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		return "", fmt.Errorf("create archive dir: %w", err)
	}

	artifacts := []string{
		"state.json",
		"matrix.lock.json",
		"decision.json",
		"baselines",
		"baselines.prev",
	}
	if opts.includeMemory {
		artifacts = append(artifacts, "loop_memory.md")
	}
	if opts.includeSessionIndex {
		artifacts = append(artifacts, "session_index.jsonl")
	}

	for _, name := range artifacts {
		src := filepath.Join(loopDir, name)
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue
		}
		dst := filepath.Join(archiveDir, name)
		if err := os.Rename(src, dst); err != nil {
			return "", fmt.Errorf("archive %s: %w", name, err)
		}
	}

	// Archive per-session logs if present.
	sessionLogs := filepath.Join(loopDir, "sessions", prevState.SessionID)
	if _, err := os.Stat(sessionLogs); err == nil {
		dst := filepath.Join(archiveDir, "sessions")
		_ = os.RemoveAll(dst)
		if err := os.Rename(sessionLogs, dst); err != nil {
			return "", fmt.Errorf("archive session logs: %w", err)
		}
	} else {
		// Backward compatibility: pre-sessionized runs stored logs in loop/iterations.
		legacyIterations := filepath.Join(loopDir, "iterations")
		if _, legacyErr := os.Stat(legacyIterations); legacyErr == nil {
			dst := filepath.Join(archiveDir, "iterations")
			_ = os.RemoveAll(dst)
			if err := os.Rename(legacyIterations, dst); err != nil {
				return "", fmt.Errorf("archive legacy iterations: %w", err)
			}
		}
	}

	return prevState.SessionID, nil
}

func ensureDecisionValidForArchive(workdir string) error {
	path := DecisionPath(workdir)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat decision.json: %w", err)
	}
	if _, err := ReadDecision(workdir); err != nil {
		return fmt.Errorf("decision.json validation failed: %w", err)
	}
	return nil
}
