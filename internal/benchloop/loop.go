package benchloop

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

// Loop is the main benchmark tuning loop orchestrator.
type Loop struct {
	cfg      *Config
	state    *State
	conctl   *ConctlRunner
	lock     *MatrixLock
	runLock  *RunLock
	lockHeld bool
}

// NewLoop creates a new Loop from config.
func NewLoop(cfg *Config) *Loop {
	return &Loop{cfg: cfg}
}

// Run executes the full benchmark tuning loop.
func (l *Loop) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Println("Received shutdown signal, finishing current iteration...")
		cancel()
	}()

	// Ensure loop directory exists.
	loopDir := filepath.Join(l.cfg.Workdir, "benchmarks", "loop")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		return fmt.Errorf("create loop dir: %w", err)
	}

	// Enforce single active benchloop run in this workdir.
	runLock, err := AcquireRunLock(l.cfg.Workdir, l.cfg.Resume)
	if err != nil {
		return err
	}
	l.runLock = runLock
	l.lockHeld = true
	defer func() {
		if l.lockHeld {
			if releaseErr := l.runLock.Release(); releaseErr != nil {
				log.Printf("Warning: failed to release run lock: %v", releaseErr)
			}
		}
	}()

	// Setup.
	fmt.Fprintf(os.Stderr, "\n╔══════════════════════════════════╗\n")
	fmt.Fprintf(os.Stderr, "║       BENCHLOOP — SETUP          ║\n")
	fmt.Fprintf(os.Stderr, "╚══════════════════════════════════╝\n\n")
	if err := l.setup(ctx); err != nil {
		return fmt.Errorf("setup: %w", err)
	}

	// Phase 1: Iteration loop.
	fmt.Fprintf(os.Stderr, "\n╔══════════════════════════════════╗\n")
	fmt.Fprintf(os.Stderr, "║     BENCHLOOP — ITERATING        ║\n")
	fmt.Fprintf(os.Stderr, "╚══════════════════════════════════╝\n\n")
	if err := l.iterate(ctx); err != nil {
		return err
	}

	// Phase 2: Completion summary.
	fmt.Fprintf(os.Stderr, "\n╔══════════════════════════════════╗\n")
	fmt.Fprintf(os.Stderr, "║     BENCHLOOP — COMPLETE         ║\n")
	fmt.Fprintf(os.Stderr, "╚══════════════════════════════════╝\n\n")
	l.printSummary()

	// Consolidate tooling recommendations from all iterations into loop memory.
	if len(l.state.UXRecommendations) > 0 {
		if err := AppendToolingRecommendations(l.cfg.Workdir, l.state.UXRecommendations); err != nil {
			log.Printf("Warning: failed to write tooling recommendations: %v", err)
		} else {
			fmt.Fprintf(os.Stderr, "\nWrote %d conctl improvement recommendation(s) to loop memory.\n", len(l.state.UXRecommendations))
			fmt.Fprintf(os.Stderr, "Review: cat %s | grep -A 999 'Conctl Improvement Recommendations'\n", MemoryPath(l.cfg.Workdir))
		}
	}

	// Phase 3: Promote improved workflows back to seed files.
	l.promoteSeedsIfImproved(ctx)

	return nil
}

// setup builds conctl, loads/creates state, and establishes matrix lock.
func (l *Loop) setup(ctx context.Context) error {
	// Build conctl binary.
	log.Println("Building conctl binary...")
	conctl, err := BuildConctl(l.cfg.Workdir)
	if err != nil {
		return fmt.Errorf("build conctl: %w", err)
	}
	l.conctl = conctl
	log.Printf("conctl binary ready at %s", conctl.BinPath())

	// Kill any stale agent left behind by a previous crash.
	KillStaleAgent(l.cfg.Workdir)

	// Load or create state.
	if l.cfg.Resume {
		state, err := LoadState(l.cfg.Workdir)
		if err != nil {
			return fmt.Errorf("resume: %w", err)
		}
		l.state = state
		log.Printf("Resumed from state: iteration %d, accuracy %.1f%%", state.Iteration, state.CurrentAccuracy*100)
	} else {
		// Fresh run — archive previous session artifacts if they exist.
		archivedSession, err := ArchivePreviousSession(l.cfg.Workdir)
		if err != nil {
			log.Printf("Warning: failed to archive previous session: %v", err)
		} else if archivedSession != "" {
			log.Printf("Archived previous session %s to benchmarks/loop/archive/%s/", archivedSession, archivedSession)
		}
		l.state = NewState(l.cfg)
	}
	if l.runLock != nil {
		if err := l.runLock.SetSessionID(l.state.SessionID); err != nil {
			return fmt.Errorf("update run lock: %w", err)
		}
	}

	// Populate baseline from --best-run-id if provided.
	if l.cfg.BestRunID != "" && l.state.BaselineRunID == "" {
		if err := l.bootstrapBaseline(ctx); err != nil {
			return fmt.Errorf("bootstrap baseline: %w", err)
		}
	}

	// Matrix lock setup.
	if l.cfg.Resume {
		lock, err := ReadMatrixLock(l.cfg.Workdir)
		if err != nil {
			return fmt.Errorf("resume matrix lock: %w", err)
		}
		lock.Normalize()
		if lock.MigrateLegacyRunSetSplit() {
			if err := UpdateMatrix(l.cfg.Workdir, lock); err != nil {
				return fmt.Errorf("persist migrated matrix lock: %w", err)
			}
			log.Printf("Migrated legacy matrix lock to run_set=custom for split=%q", lock.Split)
		}
		if err := l.validateResumeInputs(lock); err != nil {
			return err
		}
		l.lock = lock
		log.Println("Loaded existing matrix lock")
	} else {
		if err := l.autoGenerateMatrix(); err != nil {
			return err
		}
	}
	if err := l.lock.ValidateRunSetSplit(); err != nil {
		return err
	}

	// Sync state with the approved matrix lock values.
	// State was created before the lock was established, so fields like
	// ItemLimit, Split, and Concurrency may be 0/"" if not explicitly set by the user.
	l.state.Live = nil
	l.state.Split = l.lock.Split
	l.state.ItemLimit = l.lock.ItemLimit
	l.state.Concurrency = l.lock.Concurrency
	l.state.TargetWorkflows = l.lock.TargetWorkflows

	// Validate baseline item count matches locked item_limit.
	// A baseline with fewer items than the session requires is not a valid match.
	// When item_limit is 0 (all items), skip this check since "all" varies by split size.
	if l.lock.ItemLimit > 0 && l.state.BaselineTotalItems > 0 && l.state.BaselineTotalItems < l.lock.ItemLimit {
		return fmt.Errorf("baseline run %s has %d items but session requires %d — use a baseline with at least %d items or omit --best-run-id to bootstrap fresh",
			l.state.BaselineRunID, l.state.BaselineTotalItems, l.lock.ItemLimit, l.lock.ItemLimit)
	}

	// Initialize memory file.
	if err := InitMemory(l.cfg.Workdir, l.state); err != nil {
		return fmt.Errorf("init memory: %w", err)
	}

	l.state.Status = "running"
	return l.state.Save(l.cfg.Workdir)
}
