package benchloop

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// SessionRecord maps a benchloop attempt to a Claude session transcript.
type SessionRecord struct {
	LoopSessionID   string    `json:"loop_session_id"`
	Phase           string    `json:"phase"` // currently "iteration"
	Iteration       int       `json:"iteration"`
	Attempt         int       `json:"attempt"`
	ClaudeSessionID string    `json:"claude_session_id"`
	TranscriptPath  string    `json:"transcript_path"`
	LogPath         string    `json:"log_path"`
	ExitCode        int       `json:"exit_code"`
	StartedAt       time.Time `json:"started_at"`
	FinishedAt      time.Time `json:"finished_at"`
	InFlight        bool      `json:"in_flight,omitempty"`
}

// SessionIndexPath returns the append-only session index path.
func SessionIndexPath(workdir string) string {
	return filepath.Join(workdir, "benchmarks", "loop", "session_index.jsonl")
}

// AppendSessionRecord appends one session record to the loop's session index.
func AppendSessionRecord(workdir string, record *SessionRecord) error {
	path := SessionIndexPath(workdir)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create session index dir: %w", err)
	}

	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal session record: %w", err)
	}
	line = append(line, '\n')

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open session index: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("append session index: %w", err)
	}
	return nil
}

// ReadSessionRecords reads all session records from the session index.
func ReadSessionRecords(workdir string) ([]SessionRecord, error) {
	path := SessionIndexPath(workdir)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open session index: %w", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 8*1024*1024)

	records := make([]SessionRecord, 0, 64)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var r SessionRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			log.Printf("Warning: skipping malformed session index line %d: %v", lineNum, err)
			continue
		}
		records = append(records, r)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan session index: %w", err)
	}
	return records, nil
}

// ReadLiveSessionRecord returns the currently running agent session from state.live, if present.
// It is used as a best-effort fallback while an attempt is still in-flight and has not yet
// been persisted into session_index.jsonl.
func ReadLiveSessionRecord(workdir string) (*SessionRecord, error) {
	if _, err := os.Stat(StatePath(workdir)); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat state file: %w", err)
	}

	state, err := LoadState(workdir)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(state.Status) != "running" {
		return nil, nil
	}
	if lockMeta, lockErr := ReadRunLockMetadata(workdir); lockErr == nil {
		if !IsRunLockProcessAlive(lockMeta) {
			return nil, nil
		}
		if lockSession := strings.TrimSpace(lockMeta.SessionID); lockSession != "" && lockSession != strings.TrimSpace(state.SessionID) {
			return nil, nil
		}
	}
	if state.Live == nil || strings.TrimSpace(state.Live.AgentSessionID) == "" {
		return nil, nil
	}

	record := &SessionRecord{
		LoopSessionID:   state.SessionID,
		Phase:           strings.TrimSpace(state.Live.Phase),
		Iteration:       state.Live.Iteration,
		Attempt:         state.Live.Attempt,
		ClaudeSessionID: strings.TrimSpace(state.Live.AgentSessionID),
		TranscriptPath:  strings.TrimSpace(state.Live.TranscriptPath),
		LogPath:         strings.TrimSpace(state.Live.AgentLogPath),
		ExitCode:        -1,
		InFlight:        true,
	}
	if record.Phase == "" {
		record.Phase = "iteration"
	}
	if state.Live.StartedAt != nil {
		record.StartedAt = *state.Live.StartedAt
	}
	return record, nil
}

// MergeLiveSessionRecord appends a live record unless it is already represented
// in the persisted session index.
func MergeLiveSessionRecord(records []SessionRecord, live *SessionRecord) []SessionRecord {
	if live == nil {
		return records
	}
	for _, r := range records {
		if strings.TrimSpace(r.ClaudeSessionID) != "" && r.ClaudeSessionID == live.ClaudeSessionID {
			return records
		}
		if r.LoopSessionID == live.LoopSessionID && r.Attempt == live.Attempt && r.Attempt > 0 {
			return records
		}
	}
	return append(records, *live)
}

// CurrentLoopSessionID returns the active benchloop session ID from state.json, if present.
func CurrentLoopSessionID(workdir string) (string, error) {
	if _, err := os.Stat(StatePath(workdir)); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("stat state file: %w", err)
	}

	state, err := LoadState(workdir)
	if err != nil {
		return "", err
	}
	return state.SessionID, nil
}

// ResolveSessionLogPath resolves the most likely existing log path for a session record.
// It preserves backward compatibility across legacy paths and archived sessions.
func ResolveSessionLogPath(workdir string, record SessionRecord) string {
	try := make([]string, 0, 5)
	attempt := strconv.Itoa(record.Attempt) + ".log"
	if record.LoopSessionID != "" {
		try = append(try,
			filepath.Join(workdir, "benchmarks", "loop", "sessions", record.LoopSessionID, "iterations", attempt),
			filepath.Join(workdir, "benchmarks", "loop", "archive", record.LoopSessionID, "sessions", "iterations", attempt),
			filepath.Join(workdir, "benchmarks", "loop", "archive", record.LoopSessionID, "iterations", attempt),
		)
	}
	// Legacy global iterations path.
	try = append(try, filepath.Join(workdir, "benchmarks", "loop", "iterations", attempt))
	if strings.TrimSpace(record.LogPath) != "" {
		try = append(try, record.LogPath)
	}

	for _, path := range try {
		if strings.TrimSpace(path) == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return record.LogPath
}
