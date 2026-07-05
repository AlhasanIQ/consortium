package benchloop

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
)

// AgentResult holds the outcome of a single agent session.
type AgentResult struct {
	ExitCode        int
	ProcessPID      int
	LogPath         string
	ClaudeSessionID string
	TranscriptPath  string
	StartedAt       time.Time
	FinishedAt      time.Time
}

// IterationsDir returns the path to the iterations log directory.
func IterationsDir(workdir, sessionID string) string {
	if strings.TrimSpace(sessionID) == "" {
		// Backward-compatible fallback for legacy paths.
		return filepath.Join(workdir, "benchmarks", "loop", "iterations")
	}
	return filepath.Join(workdir, "benchmarks", "loop", "sessions", sessionID, "iterations")
}

// RunAgent spawns a Claude Code agent session and captures its output.
// The user prompt is passed via stdin, and stdout is captured to iterations/{iteration}.log.
// The system prompt is passed via --append-system-prompt (callers choose base vs iteration prompt).
// When verbose is true, stdout is also tee'd to the terminal.
func RunAgent(
	ctx context.Context,
	cfg *Config,
	sessionID string,
	iteration int,
	systemPrompt, userPrompt string,
	onStart func(*AgentResult),
) (*AgentResult, error) {
	logDir := IterationsDir(cfg.Workdir, sessionID)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("create iterations dir: %w", err)
	}

	logPath := filepath.Join(logDir, fmt.Sprintf("%d.log", iteration))
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, fmt.Errorf("create log file: %w", err)
	}
	defer func() { _ = logFile.Close() }()

	claudeSessionID := uuid.NewString()
	transcriptPath, err := ClaudeTranscriptPath(cfg.Workdir, claudeSessionID)
	if err != nil {
		return nil, fmt.Errorf("resolve transcript path: %w", err)
	}

	result := &AgentResult{
		LogPath:         logPath,
		ClaudeSessionID: claudeSessionID,
		TranscriptPath:  transcriptPath,
		StartedAt:       time.Now(),
	}
	_, _ = fmt.Fprintf(logFile, "[benchloop] attempt=%d session=%s started=%s model=%s output_format=%s\n",
		iteration,
		claudeSessionID,
		result.StartedAt.Format(time.RFC3339),
		cfg.Model,
		cfg.EffectiveAgentOutputFormat(),
	)

	args := buildClaudeArgs(cfg, claudeSessionID, systemPrompt)
	cmd := exec.CommandContext(ctx, cfg.ClaudeBin, args...)
	cmd.Dir = cfg.Workdir
	cmd.Stdin = bytes.NewReader([]byte(userPrompt))

	// Stdout/stderr routing.
	// Non-verbose: connect directly to the log file (*os.File, no pipe).
	// This eliminates SIGPIPE risk — if benchloop crashes, the agent can
	// still write to the file since the FD is dup2'd into the child.
	// Verbose: pipe through MultiWriter for terminal tee (SIGPIPE risk
	// accepted since verbose is only used for interactive debugging).
	var stdoutBuf *bytes.Buffer
	if cfg.Verbose {
		stdoutBuf = &bytes.Buffer{}
		cmd.Stdout = io.MultiWriter(logFile, stdoutBuf, os.Stdout)
		cmd.Stderr = io.MultiWriter(logFile, os.Stderr)
	} else {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	// Build clean environment: strip CLAUDECODE to allow nested sessions,
	// and add benchloop-specific metadata.
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "CLAUDECODE=") {
			env = append(env, e)
		}
	}
	pathValue := os.Getenv("PATH")
	if strings.TrimSpace(cfg.Workdir) != "" {
		pathValue = filepath.Join(cfg.Workdir, "bin") + string(os.PathListSeparator) + pathValue
	}
	env = upsertEnv(env, "PATH", pathValue)
	env = upsertEnv(env, "CONCTL_BIN", conctlBinaryPath(cfg.Workdir))
	env = upsertEnv(env, "BENCHLOOP_WORKDIR", cfg.Workdir)
	env = upsertEnv(env, "BENCHLOOP_ITERATION", fmt.Sprintf("%d", iteration))
	env = upsertEnv(env, "BENCHLOOP_CLAUDE_SESSION_ID", claudeSessionID)
	cmd.Env = env

	// Process group isolation: agent runs in its own group so we can
	// kill the entire tree on context cancellation or crash cleanup.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = 3 * time.Second

	// Start process and record PID for crash recovery.
	if err := cmd.Start(); err != nil {
		return result, fmt.Errorf("start claude agent: %w", err)
	}
	result.ProcessPID = cmd.Process.Pid
	writeAgentPID(cfg.Workdir, cmd.Process.Pid)
	_, _ = fmt.Fprintf(logFile, "[benchloop] pid=%d transcript=%s\n", result.ProcessPID, result.TranscriptPath)
	if onStart != nil {
		startSnapshot := *result
		onStart(&startSnapshot)
	}

	err = cmd.Wait()
	clearAgentPID(cfg.Workdir)
	result.FinishedAt = time.Now()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			return result, fmt.Errorf("run claude agent: %w", err)
		}
	}
	duration := result.FinishedAt.Sub(result.StartedAt).Round(time.Second)
	_, _ = fmt.Fprintf(logFile, "[benchloop] finished=%s exit_code=%d duration=%s\n",
		result.FinishedAt.Format(time.RFC3339), result.ExitCode, duration)

	return result, nil
}

// buildClaudeArgs constructs the CLI arguments for the claude command.
func buildClaudeArgs(cfg *Config, sessionID, systemPrompt string) []string {
	args := []string{
		"-p",
		"--session-id", sessionID,
		"--model", cfg.Model,
		"--max-turns", "200",
	}

	if cfg.AgentBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", cfg.AgentBudgetUSD))
	}

	// TODO(v0.1-security): Benchloop bypass mode is powerful and should remain
	// an explicit operator-only experimental path, not part of unattended CI.
	// Benchloop always runs in bypass mode to avoid permission/escalation dead paths.
	args = append(args, "--permission-mode", "bypassPermissions", "--dangerously-skip-permissions")

	args = append(args, "--output-format", cfg.EffectiveAgentOutputFormat())
	args = append(args, "--append-system-prompt", systemPrompt)

	return args
}

func upsertEnv(env []string, key, value string) []string {
	prefix := key + "="
	entry := prefix + value
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = entry
			return env
		}
	}
	return append(env, entry)
}

// agentPIDPath returns the path to the agent PID file.
func agentPIDPath(workdir string) string {
	return filepath.Join(workdir, "benchmarks", "loop", ".agent.pid")
}

// writeAgentPID persists the agent process PID so it can be cleaned up on crash.
func writeAgentPID(workdir string, pid int) {
	_ = os.WriteFile(agentPIDPath(workdir), []byte(fmt.Sprintf("%d", pid)), 0644)
}

// clearAgentPID removes the agent PID file.
func clearAgentPID(workdir string) {
	_ = os.Remove(agentPIDPath(workdir))
}

// KillStaleAgent reads the agent PID file and kills the process group if still alive.
// Safe to call when no stale agent exists (no-op).
func KillStaleAgent(workdir string) {
	path := agentPIDPath(workdir)
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err != nil || pid <= 0 {
		_ = os.Remove(path)
		return
	}

	// Check if process exists.
	if err := syscall.Kill(pid, 0); err != nil {
		_ = os.Remove(path)
		return
	}

	// Guard against PID reuse: our agents run with Setpgid (PGID == PID).
	// If the PID was recycled by an unrelated process, its PGID will differ.
	pgid, err := syscall.Getpgid(pid)
	if err != nil || pgid != pid {
		log.Printf("Stale agent PID %d has PGID %d (expected %d) — PID was reused, skipping kill", pid, pgid, pid)
		_ = os.Remove(path)
		return
	}

	log.Printf("Killing stale agent process group (pid %d)", pid)
	_ = syscall.Kill(-pid, syscall.SIGKILL) // Kill entire process group.
	_ = syscall.Kill(pid, syscall.SIGKILL)  // Fallback: kill process directly.
	_ = os.Remove(path)
}
