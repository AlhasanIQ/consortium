package benchloop

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// promoteSeedsIfImproved exports the current workflow definitions from the database
// and overwrites the corresponding seed files if improvements were made during the loop.
// This is called after the loop completes (Phase 2), so the agent's "never edit seeds"
// rule is preserved — only the controller promotes seeds.
func (l *Loop) promoteSeedsIfImproved(ctx context.Context) {
	if l.state.CurrentAccuracy <= l.state.BaselineAccuracy {
		return // no improvement, nothing to promote
	}
	if l.lock == nil || len(l.lock.TargetWorkflows) == 0 {
		return
	}

	seedsDir := filepath.Join(l.cfg.Workdir, "pkg", "seeds", "data")
	if _, err := os.Stat(seedsDir); os.IsNotExist(err) {
		log.Printf("Warning: seeds directory %s not found, skipping promotion", seedsDir)
		return
	}

	fmt.Fprintf(os.Stderr, "\n=== Seed Promotion ===\n")
	fmt.Fprintf(os.Stderr, "Accuracy improved %.1f%% → %.1f%%. Checking seed files...\n",
		l.state.BaselineAccuracy*100, l.state.CurrentAccuracy*100)

	updated := 0
	for _, wfID := range l.lock.TargetWorkflows {
		seedPath := filepath.Join(seedsDir, wfID+".json")
		if _, err := os.Stat(seedPath); os.IsNotExist(err) {
			continue // not a seed workflow, skip
		}

		// Export current definition via conctl.
		tmpPath := filepath.Join(os.TempDir(), "benchloop-seed-"+wfID+".json")
		_, err := l.conctl.Run(ctx, "workflows", "export", "--id", wfID, "--output", tmpPath)
		if err != nil {
			log.Printf("Warning: failed to export %s for seed promotion: %v", wfID, err)
			continue
		}

		// Compare with existing seed.
		exported, err := os.ReadFile(tmpPath)
		if err != nil {
			log.Printf("Warning: failed to read exported %s: %v", tmpPath, err)
			continue
		}
		existing, err := os.ReadFile(seedPath)
		if err != nil {
			log.Printf("Warning: failed to read seed %s: %v", seedPath, err)
			continue
		}
		if bytes.Equal(bytes.TrimSpace(exported), bytes.TrimSpace(existing)) {
			continue // no changes
		}

		// Overwrite seed file.
		if err := os.WriteFile(seedPath, exported, 0644); err != nil {
			log.Printf("Warning: failed to write seed %s: %v", seedPath, err)
			continue
		}
		updated++
		fmt.Fprintf(os.Stderr, "  Promoted %s → %s\n", wfID, seedPath)
	}

	if updated > 0 {
		fmt.Fprintf(os.Stderr, "\n%d seed file(s) updated. Review and commit:\n", updated)
		fmt.Fprintf(os.Stderr, "  git diff pkg/seeds/data/\n")
		fmt.Fprintf(os.Stderr, "  git add pkg/seeds/data/ && git commit -m \"chore: promote benchloop improvements to seeds\"\n")
	} else {
		fmt.Fprintf(os.Stderr, "No seed files changed (workflow definitions match).\n")
	}
}
