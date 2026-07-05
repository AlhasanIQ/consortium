package benchloop

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// BaselinesDir returns the path to the baselines directory.
func BaselinesDir(workdir string) string {
	return filepath.Join(workdir, "benchmarks", "loop", "baselines")
}

// BaselinesPrevDir returns the path to the previous baselines directory.
func BaselinesPrevDir(workdir string) string {
	return filepath.Join(workdir, "benchmarks", "loop", "baselines.prev")
}

// SnapshotWorkflows exports all target workflows to the baselines directory.
func SnapshotWorkflows(ctx context.Context, conctl *ConctlRunner, workdir string, workflows []string) error {
	dir := BaselinesDir(workdir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create baselines dir: %w", err)
	}

	for _, wfID := range workflows {
		outPath := filepath.Join(dir, wfID+".json")
		_, err := conctl.Run(ctx, "workflows", "export", "--id", wfID, "--output", outPath)
		if err != nil {
			return fmt.Errorf("snapshot workflow %s: %w", wfID, err)
		}
	}
	return nil
}

// RestoreWorkflows restores all target workflows from the baselines directory.
func RestoreWorkflows(ctx context.Context, conctl *ConctlRunner, workdir string, workflows []string) error {
	dir := BaselinesDir(workdir)
	missing := make([]string, 0)

	for _, wfID := range workflows {
		srcPath := filepath.Join(dir, wfID+".json")
		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			missing = append(missing, wfID)
			continue
		}
		_, err := conctl.Run(ctx, "workflows", "update", "--id", wfID, "--file", srcPath, "--yes")
		if err != nil {
			return fmt.Errorf("restore workflow %s: %w", wfID, err)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("restore aborted: missing baseline snapshots for %v", missing)
	}
	return nil
}

// RotateBaselines moves current baselines to baselines.prev and exports fresh baselines.
func RotateBaselines(ctx context.Context, conctl *ConctlRunner, workdir string, workflows []string) error {
	baseDir := BaselinesDir(workdir)
	prevDir := BaselinesPrevDir(workdir)

	// Remove old prev.
	if err := os.RemoveAll(prevDir); err != nil {
		return fmt.Errorf("remove old baselines.prev: %w", err)
	}

	// Move current → prev.
	if _, err := os.Stat(baseDir); err == nil {
		if err := os.Rename(baseDir, prevDir); err != nil {
			return fmt.Errorf("rotate baselines: %w", err)
		}
	}

	// Export fresh baselines from the now-accepted workflows.
	return SnapshotWorkflows(ctx, conctl, workdir, workflows)
}
