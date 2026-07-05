package benchloop

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ConctlRunner manages a pre-built conctl binary for controller-side invocations.
type ConctlRunner struct {
	binPath string
	workdir string
}

// BuildConctl compiles the conctl binary once and returns a runner.
// The binary is placed at <workdir>/bin/conctl.
func BuildConctl(workdir string) (*ConctlRunner, error) {
	binPath := conctlBinaryPath(workdir)

	cmd := exec.Command("make", "conctl-build")
	cmd.Dir = workdir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("build conctl: %w\n%s", err, stderr.String())
	}
	if _, err := os.Stat(binPath); err != nil {
		return nil, fmt.Errorf("build conctl: expected binary at %s: %w", binPath, err)
	}

	return &ConctlRunner{binPath: binPath, workdir: workdir}, nil
}

// Run executes a conctl command and returns its combined stdout/stderr output.
func (r *ConctlRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, r.binPath, args...)
	cmd.Dir = r.workdir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("conctl %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return out, nil
}

// RunJSON executes a conctl command with --format json and returns only stdout.
// Stderr (hints, diagnostics) is captured separately so it cannot corrupt JSON parsing.
// Global flags must precede the resource/command in conctl's arg parser.
func (r *ConctlRunner) RunJSON(ctx context.Context, args ...string) ([]byte, error) {
	full := make([]string, 0, len(args)+2)
	full = append(full, "--format", "json")
	full = append(full, args...)

	cmd := exec.CommandContext(ctx, r.binPath, full...)
	cmd.Dir = r.workdir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.Bytes(), fmt.Errorf("conctl %s: %w\n%s", strings.Join(full, " "), err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// BinPath returns the path to the compiled conctl binary.
func (r *ConctlRunner) BinPath() string {
	return r.binPath
}

func conctlBinaryPath(workdir string) string {
	return filepath.Join(workdir, "bin", "conctl")
}
