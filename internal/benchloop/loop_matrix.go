package benchloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"slices"
	"sort"
	"strings"
	"time"
)

// ErrItemCountMismatch is returned when a run's item count doesn't match the baseline.
var ErrItemCountMismatch = errors.New("item count mismatch")

// ErrMatrixScopeMismatch is returned when a run falls outside the frozen matrix lock.
var ErrMatrixScopeMismatch = errors.New("run outside approved matrix")

// autoGenerateMatrix creates a matrix lock directly from operator-provided CLI flags.
// For fresh runs, split/item_limit/concurrency are required in Config.Validate.
func (l *Loop) autoGenerateMatrix() error {
	log.Println("Generating matrix lock from operator CLI flags")

	// Derive workflow_order and target_workflows from the --workflows flag.
	// benchmark-* workflows go into workflow_order; their reasoning-* children
	// are inferred and added to target_workflows.
	var workflowOrder []string
	targetSet := make(map[string]struct{})
	for _, wf := range l.cfg.Workflows {
		targetSet[wf] = struct{}{}
		if strings.HasPrefix(wf, "benchmark-") {
			workflowOrder = append(workflowOrder, wf)
			// Infer the child reasoning workflow.
			child := "reasoning-" + strings.TrimPrefix(wf, "benchmark-")
			targetSet[child] = struct{}{}
		}
	}
	var targetWorkflows []string
	for wf := range targetSet {
		targetWorkflows = append(targetWorkflows, wf)
	}
	sort.Strings(targetWorkflows)

	proposal := &MatrixProposal{
		Benchmark:       l.cfg.Benchmark,
		RunSet:          l.cfg.RunSet,
		Split:           l.cfg.Split,
		ItemLimit:       l.cfg.ItemLimit,
		Concurrency:     l.cfg.Concurrency,
		WorkflowOrder:   workflowOrder,
		TargetWorkflows: targetWorkflows,
		Rationale:       "Operator-defined matrix from benchloop run flags",
	}

	if err := proposal.Validate(); err != nil {
		return fmt.Errorf("auto-generated matrix invalid: %w", err)
	}

	if err := ApproveMatrix(l.cfg.Workdir, proposal, l.state.SessionID); err != nil {
		return fmt.Errorf("write matrix lock: %w", err)
	}

	lock, err := ReadMatrixLock(l.cfg.Workdir)
	if err != nil {
		return fmt.Errorf("read matrix lock: %w", err)
	}
	l.lock = lock

	lockJSON, _ := json.MarshalIndent(lock, "", "  ")
	fmt.Fprintf(os.Stderr, "Matrix auto-generated and locked:\n%s\n", string(lockJSON))
	return nil
}

func (l *Loop) validateResumeInputs(lock *MatrixLock) error {
	if l == nil || !l.cfg.Resume || lock == nil {
		return nil
	}

	if l.cfg.IsExplicit("benchmark") && l.cfg.Benchmark != lock.Benchmark {
		return fmt.Errorf("--benchmark=%q does not match resumed matrix benchmark=%q; omit --benchmark or start a fresh run", l.cfg.Benchmark, lock.Benchmark)
	}
	if l.cfg.IsExplicit("run-set") && l.cfg.RunSet != lock.RunSet {
		return fmt.Errorf("--run-set=%q does not match resumed matrix run_set=%q; omit --run-set or start a fresh run", l.cfg.RunSet, lock.RunSet)
	}
	if l.cfg.IsExplicit("split") && l.cfg.Split != lock.Split {
		return fmt.Errorf("--split=%q does not match resumed matrix split=%q; omit --split or start a fresh run", l.cfg.Split, lock.Split)
	}
	if l.cfg.IsExplicit("item-limit") && l.cfg.ItemLimit != lock.ItemLimit {
		return fmt.Errorf("--item-limit=%d does not match resumed matrix item_limit=%d; omit --item-limit or start a fresh run", l.cfg.ItemLimit, lock.ItemLimit)
	}
	if l.cfg.IsExplicit("concurrency") && l.cfg.Concurrency != lock.Concurrency {
		return fmt.Errorf("--concurrency=%d does not match resumed matrix concurrency=%d; omit --concurrency or start a fresh run", l.cfg.Concurrency, lock.Concurrency)
	}
	if l.cfg.IsExplicit("workflows") {
		requested := dedupeNonEmpty(append([]string(nil), l.cfg.Workflows...))
		order := dedupeNonEmpty(append([]string(nil), lock.WorkflowOrder...))
		target := dedupeNonEmpty(append([]string(nil), lock.TargetWorkflows...))
		sort.Strings(requested)
		sort.Strings(order)
		sort.Strings(target)
		if !slices.Equal(requested, order) && !slices.Equal(requested, target) {
			return fmt.Errorf("--workflows=%v does not match resumed matrix scope (workflow_order=%v target_workflows=%v); omit --workflows or pass one of the locked sets", requested, order, target)
		}
	}

	return nil
}

// validateRunScope checks the decision's run against the locked matrix.
// Benchmark/workflow/split mismatches and item count mismatches are fatal.
func (l *Loop) validateRunScope(ctx context.Context, decision *Decision) error {
	// Fetch the run's metadata to check benchmark/workflow/split/items.
	out, err := l.conctl.RunJSON(ctx, "benchmarks", "get", "--id", decision.RunID)
	if err != nil {
		return fmt.Errorf("fetch run %s: %w", decision.RunID, err)
	}

	var envelope struct {
		Run struct {
			Benchmark  string `json:"benchmark"`
			WorkflowID string `json:"workflow_id"`
			Split      string `json:"split"`
			TotalItems int    `json:"total_items"`
		} `json:"Run"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		return fmt.Errorf("parse run metadata: %w", err)
	}
	runMeta := envelope.Run

	if !l.lock.IsInMatrix(runMeta.Benchmark, runMeta.WorkflowID, runMeta.Split) {
		return fmt.Errorf("%w: run %s (bench=%s wf=%s split=%s) not in matrix (bench=%s workflows=%v split=%s)",
			ErrMatrixScopeMismatch,
			decision.RunID,
			runMeta.Benchmark,
			runMeta.WorkflowID,
			runMeta.Split,
			l.lock.Benchmark,
			l.lock.WorkflowOrder,
			l.lock.Split,
		)
	}

	// Item count must match baseline — comparing metrics across different sample sizes is invalid.
	if l.state.BaselineTotalItems > 0 && runMeta.TotalItems != l.state.BaselineTotalItems {
		return fmt.Errorf("%w: run %s has %d items but baseline has %d",
			ErrItemCountMismatch, decision.RunID, runMeta.TotalItems, l.state.BaselineTotalItems)
	}

	return nil
}

// handleMatrixChangeRequest processes a matrix expansion request.
func (l *Loop) handleMatrixChangeRequest(ctx context.Context, iterNum int, decision *Decision) (bool, error) {
	log.Println("Agent requested matrix change; rejecting (operator-defined matrix is immutable during a run)")

	// Rollback any changes made during this iteration.
	_ = RestoreWorkflows(ctx, l.conctl, l.cfg.Workdir, l.lock.TargetWorkflows)

	log.Println("Matrix change rejected by controller policy")
	l.state.PlateauCount++
	l.state.Iteration++
	now := time.Now()
	l.state.LastIterationAt = &now
	_ = l.state.Save(l.cfg.Workdir)
	_ = AppendIteration(l.cfg.Workdir, iterNum, decision, false)

	note := "Matrix change rejected: matrix is immutable during a run; start a fresh run with the desired matrix flags"
	if decision.MatrixChangeRequest != nil {
		desc := strings.TrimSpace(decision.MatrixChangeRequest.Description)
		if desc != "" {
			note = fmt.Sprintf("%s (%s)", note, desc)
		}
	}
	_ = AppendNote(l.cfg.Workdir, iterNum, note)

	fmt.Fprintln(os.Stderr, "Matrix change requests are disabled during a run. Start a fresh benchloop run with new matrix flags.")
	return false, nil
}
