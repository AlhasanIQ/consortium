package benchloop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// iterate runs Phase 1: the main iteration loop.
func (l *Loop) iterate(ctx context.Context) error {
	for l.state.Iteration < l.state.MaxIterations {
		if ctx.Err() != nil {
			log.Println("Context cancelled, stopping loop")
			break
		}

		iterNum := l.state.Iteration + 1
		l.state.TotalAttempts++
		attemptNum := l.state.TotalAttempts
		retryLabel := ""
		if l.state.AgentCrashCount > 0 {
			retryLabel = fmt.Sprintf(" (retry %d/3)", l.state.AgentCrashCount)
		}

		fmt.Fprintf(os.Stderr, "\n========== Iteration %d/%d%s [attempt %d] ==========\n",
			iterNum, l.state.MaxIterations, retryLabel, attemptNum)

		// Run one iteration. Use attemptNum for log file naming to avoid overwrites on retry.
		stop, err := l.runIteration(ctx, iterNum, attemptNum)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n!!! Iteration %d FAILED: %v\n\n", iterNum, err)
			l.state.AgentCrashCount++

			// Kill the agent process group in case it's still alive.
			KillStaleAgent(l.cfg.Workdir)

			// Cancel any running benchmark run left behind by the crashed agent.
			// Without this, the next attempt's preflight will block on a busy runner.
			l.cancelRunningBenchmark(ctx)

			// Record the crash in memory with attempt number for uniqueness.
			_ = AppendNote(l.cfg.Workdir, iterNum, fmt.Sprintf("Attempt %d crash: %v", attemptNum, err))
			_ = l.state.Save(l.cfg.Workdir)

			if l.state.AgentCrashCount >= 3 {
				l.state.Status = "failed"
				_ = l.state.Save(l.cfg.Workdir)
				fmt.Fprintf(os.Stderr, "FATAL: 3 consecutive failures, halting loop.\n")
				fmt.Fprintf(os.Stderr, "Check benchmarks/loop/loop_memory.md for error details.\n")
				return fmt.Errorf("3 consecutive failures, halting loop")
			}
			fmt.Fprintf(os.Stderr, "Retrying... (crash %d/3)\n", l.state.AgentCrashCount)
			continue
		}

		// Reset crash counter on successful iteration.
		// Note: state.Iteration++ and state.Save() are done atomically inside
		// acceptBootstrap/acceptIteration/rollbackIteration BEFORE appending to
		// memory, so crash-recovery won't re-log the same iteration number.
		l.state.AgentCrashCount = 0

		if stop {
			break
		}

		// Check stop conditions.
		if l.shouldStop() {
			break
		}
	}

	l.state.Status = "completed"
	l.clearLiveStatus()
	return l.state.Save(l.cfg.Workdir)
}

// runIteration executes a single iteration. Returns (shouldStop, error).
func (l *Loop) runIteration(ctx context.Context, iterNum, attemptNum int) (bool, error) {
	// Step 0: Kill any stale agent from a previous crash/retry.
	KillStaleAgent(l.cfg.Workdir)

	// Step 1: Preflight checks.
	l.updateLiveStatus("iteration", "preflight", iterNum, attemptNum, "Checking benchmark runner status")
	if err := l.preflight(ctx); err != nil {
		return false, fmt.Errorf("preflight: %w", err)
	}

	// Step 2: Snapshot current workflows.
	l.updateLiveStatus("iteration", "snapshot", iterNum, attemptNum, "Snapshotting target workflows")
	log.Println("Snapshotting workflows...")
	if err := SnapshotWorkflows(ctx, l.conctl, l.cfg.Workdir, l.lock.TargetWorkflows); err != nil {
		return false, fmt.Errorf("snapshot: %w", err)
	}

	// Step 3: Remove stale decision.
	l.updateLiveStatus("iteration", "reset_decision", iterNum, attemptNum, "Clearing stale decision file")
	if err := RemoveDecision(l.cfg.Workdir); err != nil {
		return false, fmt.Errorf("remove stale decision: %w", err)
	}

	// Step 4: Build prompt and invoke agent.
	memory, err := ReadMemoryTruncated(l.cfg.Workdir, 150)
	if err != nil {
		log.Printf("Warning: failed to read memory: %v", err)
	}
	iterPrompt := BuildIterationPrompt(l.cfg, l.state, l.lock, memory)

	iterCtx, iterCancel := context.WithTimeout(ctx, l.cfg.IterationTimeout)
	defer iterCancel()

	fmt.Fprintf(os.Stderr, "Spawning agent session (model: %s, timeout: %s)...\n", l.cfg.Model, l.cfg.IterationTimeout)
	l.updateLiveStatus("iteration", "agent_running", iterNum, attemptNum, "Agent is executing")

	// Heartbeat: print elapsed time periodically when not in verbose mode.
	agentStart := time.Now()
	var heartbeatOnce sync.Once
	heartbeatDone := make(chan struct{})
	if !l.cfg.Verbose {
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					elapsed := time.Since(agentStart).Round(time.Second)
					fmt.Fprintf(os.Stderr, "  Agent running... (%s elapsed)\n", elapsed)
					l.updateLiveStatus("iteration", "agent_running", iterNum, attemptNum, fmt.Sprintf("Agent is executing (%s elapsed)", elapsed))
					l.appendLiveLogLine("[benchloop] heartbeat: agent running (%s elapsed)\n", elapsed)
				case <-heartbeatDone:
					return
				}
			}
		}()
	}
	stopHeartbeat := func() { heartbeatOnce.Do(func() { close(heartbeatDone) }) }
	defer stopHeartbeat()

	iterSystemPrompt := BuildIterationSystemPrompt(l.cfg)
	result, err := RunAgent(iterCtx, l.cfg, l.state.SessionID, attemptNum, iterSystemPrompt, iterPrompt, func(start *AgentResult) {
		l.setLiveAgent(start)
		fmt.Fprintf(os.Stderr, "Agent started (pid: %d)\n", start.ProcessPID)
		fmt.Fprintf(os.Stderr, "  Session:    %s\n", start.ClaudeSessionID)
		fmt.Fprintf(os.Stderr, "  Log file:   %s\n", start.LogPath)
		fmt.Fprintf(os.Stderr, "  Transcript: %s\n", start.TranscriptPath)
	})
	stopHeartbeat()
	elapsed := time.Since(agentStart).Round(time.Second)
	l.updateLiveStatus("iteration", "agent_finished", iterNum, attemptNum, fmt.Sprintf("Agent finished in %s", elapsed))
	l.persistAgentSession("iteration", iterNum, attemptNum, result)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Agent error after %s: %v — rolling back workflows\n", elapsed, err)
		_ = RestoreWorkflows(ctx, l.conctl, l.cfg.Workdir, l.lock.TargetWorkflows)
		return false, err
	}

	fmt.Fprintf(os.Stderr, "Agent session completed in %s (exit code: %d, log: %s)\n", elapsed, result.ExitCode, result.LogPath)
	if result.ExitCode != 0 {
		fmt.Fprintf(os.Stderr, "Agent exited with non-zero code — rolling back\n")
		_ = RestoreWorkflows(ctx, l.conctl, l.cfg.Workdir, l.lock.TargetWorkflows)
		return false, fmt.Errorf("agent exit code %d", result.ExitCode)
	}

	// Step 5a: Wait for runner idle (agent may have left a run in progress).
	l.updateLiveStatus("iteration", "post_agent_preflight", iterNum, attemptNum, "Waiting for benchmark runner to become idle")
	if err := l.preflight(ctx); err != nil {
		log.Printf("Warning: post-agent runner check failed: %v", err)
	}

	// Step 5b: Read and validate decision.
	l.updateLiveStatus("iteration", "read_decision", iterNum, attemptNum, "Reading agent decision")
	decision, err := ReadDecision(l.cfg.Workdir)
	if err != nil {
		log.Printf("Failed to read decision: %v — rolling back", err)
		_ = RestoreWorkflows(ctx, l.conctl, l.cfg.Workdir, l.lock.TargetWorkflows)
		return false, fmt.Errorf("read decision: %w", err)
	}

	// Warn if sanity check was not passed.
	if !decision.SanityPassed {
		log.Println("Warning: agent did not pass sanity check — reviewing results with caution")
	}
	l.recordDecisionCosts(ctx, decision)
	l.accumulateUXRecommendations(decision)

	if !l.cfg.AllowModelSwaps {
		swaps, detectErr := DetectModelSwaps(ctx, l.conctl, l.cfg.Workdir, l.lock.TargetWorkflows)
		if detectErr != nil {
			return false, fmt.Errorf("detect model swaps: %w", detectErr)
		}
		if len(swaps) > 0 {
			fmt.Fprintf(os.Stderr, "ROLLED BACK: model swaps are disabled for this loop\n")
			for _, swap := range swaps {
				fmt.Fprintf(os.Stderr, "  - %s: %s (%q -> %q)\n", swap.WorkflowID, swap.Path, swap.Before, swap.After)
			}
			if strings.TrimSpace(decision.ChangesSummary) == "" {
				decision.ChangesSummary = "Controller rollback: model swaps are disabled"
			} else {
				decision.ChangesSummary += "; controller rollback: model swaps are disabled"
			}
			decision.Verdict = VerdictRollback
			return l.rollbackIteration(ctx, iterNum, decision, "model swaps are disabled")
		}
	}

	// Print decision summary.
	fmt.Fprintf(os.Stderr, "\n┌─ Decision ─────────────────────────────────────\n")
	fmt.Fprintf(os.Stderr, "│ Verdict:    %s\n", decision.Verdict)
	fmt.Fprintf(os.Stderr, "│ Accuracy:   %.1f%%\n", decision.Accuracy*100)
	fmt.Fprintf(os.Stderr, "│ Parse rate: %.0f%%\n", decision.ParseRate*100)
	fmt.Fprintf(os.Stderr, "│ Cost/item:  $%.4f\n", decision.CostPerItem)
	if decision.AvgLatencyMS > 0 {
		fmt.Fprintf(os.Stderr, "│ Latency:    %.0fms avg / %.0fms p95\n", decision.AvgLatencyMS, decision.P95LatencyMS)
	}
	fmt.Fprintf(os.Stderr, "│ Failed:     %d\n", decision.FailedItems)
	if decision.TotalItems > 0 {
		fmt.Fprintf(os.Stderr, "│ Items:      %d\n", decision.TotalItems)
	}
	fmt.Fprintf(os.Stderr, "│ Run:        %s\n", decision.RunID)
	fmt.Fprintf(os.Stderr, "│ Changes:    %s\n", decision.ChangesSummary)
	if l.state.CurrentRunID != "" {
		delta := (decision.Accuracy - l.state.CurrentAccuracy) * 100
		sign := "+"
		if delta < 0 {
			sign = ""
		}
		fmt.Fprintf(os.Stderr, "│ Delta:      %s%.1f%% vs baseline\n", sign, delta)
	}
	fmt.Fprintf(os.Stderr, "└────────────────────────────────────────────────\n\n")

	// Step 6: Validate run is within the approved matrix.
	l.updateLiveStatus("iteration", "validate_scope", iterNum, attemptNum, "Validating run scope against matrix lock")
	if err := l.validateRunScope(ctx, decision); err != nil {
		if errors.Is(err, ErrItemCountMismatch) || errors.Is(err, ErrMatrixScopeMismatch) {
			decision.Verdict = VerdictRollback
			return l.rollbackIteration(ctx, iterNum, decision, err.Error())
		}
		log.Printf("Warning: run scope validation: %v", err)
	}

	// Handle special verdicts early.
	if decision.Verdict == VerdictRequestMatrixChange {
		l.updateLiveStatus("iteration", "matrix_change_request", iterNum, attemptNum, "Processing requested matrix change")
		return l.handleMatrixChangeRequest(ctx, iterNum, decision)
	}

	// Bootstrap iteration: if no baseline yet, accept as the baseline.
	if l.state.CurrentRunID == "" {
		l.updateLiveStatus("iteration", "accept_bootstrap", iterNum, attemptNum, "Establishing first baseline")
		return l.acceptBootstrap(ctx, iterNum, decision)
	}

	// Step 8: Evaluate promotion gate.
	gate := EvaluateGate(decision, l.state)
	if gate.Accept {
		fmt.Fprintf(os.Stderr, "Gate: PASS (%s)\n", gate.Reason)
	} else {
		fmt.Fprintf(os.Stderr, "Gate: FAIL (%s)\n", gate.Reason)
	}
	if gate.Override {
		fmt.Fprintf(os.Stderr, "Gate OVERRODE agent verdict %q\n", decision.Verdict)
	}

	// Step 9: Accept or rollback.
	if gate.Accept {
		l.updateLiveStatus("iteration", "accept", iterNum, attemptNum, "Promotion gate passed; accepting iteration")
		return l.acceptIteration(ctx, iterNum, decision)
	}
	l.updateLiveStatus("iteration", "rollback", iterNum, attemptNum, "Promotion gate failed; rolling back iteration")
	return l.rollbackIteration(ctx, iterNum, decision, gate.Reason)
}

// preflight verifies backend health and runner availability.
func (l *Loop) preflight(ctx context.Context) error {
	log.Println("Preflight: checking benchmark runner status...")
	for attempt := 0; attempt < 30; attempt++ {
		out, err := l.conctl.RunJSON(ctx, "benchmarks", "runner-status")
		if err != nil {
			if attempt == 0 {
				return fmt.Errorf("runner status check failed: %w", err)
			}
			fmt.Fprintf(os.Stderr, "  Runner check failed (attempt %d/30): %v\n", attempt+1, err)
			time.Sleep(10 * time.Second)
			continue
		}

		var status struct {
			Running bool `json:"running"`
		}
		if err := json.Unmarshal(out, &status); err != nil {
			return fmt.Errorf("parse runner status (raw: %s): %w", string(out), err)
		}

		if !status.Running {
			log.Println("Preflight: runner idle, ready to proceed")
			return nil
		}

		if attempt >= 29 {
			return fmt.Errorf("benchmark runner still busy after 5 minutes, cannot proceed")
		}

		fmt.Fprintf(os.Stderr, "  Runner busy, waiting... (%d/30)\n", attempt+1)
		if l.state != nil && l.state.Live != nil {
			l.updateLiveStatus(
				l.state.Live.Phase,
				l.state.Live.Step,
				l.state.Live.Iteration,
				l.state.Live.Attempt,
				fmt.Sprintf("Runner busy; waiting (%d/30)", attempt+1),
			)
			l.appendLiveLogLine("[benchloop] waiting for runner idle (%d/30)\n", attempt+1)
		}
		time.Sleep(10 * time.Second)
	}
	return nil
}

// cancelRunningBenchmark cancels any active benchmark run and waits for the runner to become idle.
// Called when an agent crashes mid-iteration to free the runner for the next attempt.
func (l *Loop) cancelRunningBenchmark(ctx context.Context) {
	out, err := l.conctl.RunJSON(ctx, "benchmarks", "runner-status")
	if err != nil {
		return
	}
	var status struct {
		Running bool `json:"running"`
	}
	if err := json.Unmarshal(out, &status); err != nil || !status.Running {
		return
	}

	fmt.Fprintf(os.Stderr, "  Cancelling stuck benchmark run...\n")
	if _, err := l.conctl.Run(ctx, "benchmarks", "cancel-run", "--yes"); err != nil {
		log.Printf("Warning: failed to cancel benchmark run: %v", err)
	}

	// Also cancel all running jobs to kill in-flight provider requests that would
	// otherwise keep the runner busy until their timeouts expire.
	fmt.Fprintf(os.Stderr, "  Cancelling in-flight jobs...\n")
	if _, err := l.conctl.Run(ctx, "jobs", "cancel-all", "--yes"); err != nil {
		log.Printf("Warning: failed to cancel jobs: %v", err)
	}

	// Poll until the runner is actually idle (up to 2 minutes).
	for i := 0; i < 24; i++ {
		time.Sleep(5 * time.Second)
		out, err := l.conctl.RunJSON(ctx, "benchmarks", "runner-status")
		if err != nil {
			continue
		}
		if err := json.Unmarshal(out, &status); err != nil {
			continue
		}
		if !status.Running {
			fmt.Fprintf(os.Stderr, "  Benchmark runner idle\n")
			return
		}
		fmt.Fprintf(os.Stderr, "  Waiting for runner to idle... (%d/24)\n", i+1)
	}
	fmt.Fprintf(os.Stderr, "  Warning: runner still busy after 2 minutes\n")
}

// bootstrapBaseline fetches metrics from the specified baseline run.
func (l *Loop) bootstrapBaseline(ctx context.Context) error {
	log.Printf("Fetching baseline metrics from run %s...", l.cfg.BestRunID)

	out, err := l.conctl.RunJSON(ctx, "benchmarks", "get", "--id", l.cfg.BestRunID)
	if err != nil {
		return fmt.Errorf("fetch baseline run: %w", err)
	}

	var envelope struct {
		Run struct {
			Accuracy       float64 `json:"accuracy"`
			ParseRate      float64 `json:"parse_rate"`
			FailedItems    int     `json:"failed_items"`
			AvgCostPerItem float64 `json:"avg_cost_usd_per_item"`
			TotalItems     int     `json:"total_items"`
			AvgLatencyMS   float64 `json:"avg_latency_ms"`
			P95LatencyMS   float64 `json:"p95_latency_ms"`
		} `json:"Run"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		return fmt.Errorf("parse baseline run data: %w", err)
	}
	runData := envelope.Run

	l.state.BaselineAccuracy = runData.Accuracy
	l.state.BaselineParseRate = runData.ParseRate
	l.state.BaselineCostPerItem = runData.AvgCostPerItem
	l.state.BaselineFailedItems = runData.FailedItems
	l.state.BaselineRunID = l.cfg.BestRunID
	l.state.BaselineTotalItems = runData.TotalItems
	l.state.BaselineAvgLatencyMS = runData.AvgLatencyMS
	l.state.BaselineP95LatencyMS = runData.P95LatencyMS
	l.state.CurrentAccuracy = runData.Accuracy
	l.state.CurrentParseRate = runData.ParseRate
	l.state.CurrentCostPerItem = runData.AvgCostPerItem
	l.state.CurrentFailedItems = runData.FailedItems
	l.state.CurrentRunID = l.cfg.BestRunID
	l.state.CurrentAvgLatencyMS = runData.AvgLatencyMS
	l.state.CurrentP95LatencyMS = runData.P95LatencyMS

	log.Printf("Baseline: accuracy=%.1f%%, parse_rate=%.0f%%, cost/item=$%.4f, items=%d, avg_latency=%.0fms",
		runData.Accuracy*100, runData.ParseRate*100, runData.AvgCostPerItem, runData.TotalItems, runData.AvgLatencyMS)
	return nil
}

// acceptBootstrap accepts the first iteration as the baseline (no comparison needed).
func (l *Loop) acceptBootstrap(ctx context.Context, iterNum int, decision *Decision) (bool, error) {
	fmt.Fprintf(os.Stderr, "BASELINE ESTABLISHED: accuracy=%.1f%% parse=%.0f%% cost/item=$%.4f",
		decision.Accuracy*100, decision.ParseRate*100, decision.CostPerItem)
	if decision.AvgLatencyMS > 0 {
		fmt.Fprintf(os.Stderr, " latency=%.0fms", decision.AvgLatencyMS)
	}
	fmt.Fprintln(os.Stderr)

	l.state.BaselineAccuracy = decision.Accuracy
	l.state.BaselineParseRate = decision.ParseRate
	l.state.BaselineCostPerItem = decision.CostPerItem
	l.state.BaselineFailedItems = decision.FailedItems
	l.state.BaselineRunID = decision.RunID
	l.state.BaselineAvgLatencyMS = decision.AvgLatencyMS
	l.state.BaselineP95LatencyMS = decision.P95LatencyMS
	l.state.CurrentAccuracy = decision.Accuracy
	l.state.CurrentParseRate = decision.ParseRate
	l.state.CurrentCostPerItem = decision.CostPerItem
	l.state.CurrentFailedItems = decision.FailedItems
	l.state.CurrentRunID = decision.RunID
	l.state.CurrentAvgLatencyMS = decision.AvgLatencyMS
	l.state.CurrentP95LatencyMS = decision.P95LatencyMS

	// Fetch actual total_items from the run to record baseline item count.
	if out, err := l.conctl.RunJSON(ctx, "benchmarks", "get", "--id", decision.RunID); err == nil {
		var envelope struct {
			Run struct {
				TotalItems int `json:"total_items"`
			} `json:"Run"`
		}
		if err := json.Unmarshal(out, &envelope); err == nil {
			l.state.BaselineTotalItems = envelope.Run.TotalItems
		}
	}

	_ = RotateBaselines(ctx, l.conctl, l.cfg.Workdir, l.lock.TargetWorkflows)

	// Increment and save state BEFORE appending to memory, so crash-recovery
	// won't re-log the same iteration number.
	l.state.Iteration++
	now := time.Now()
	l.state.LastIterationAt = &now
	_ = l.state.Save(l.cfg.Workdir)

	_ = AppendIteration(l.cfg.Workdir, iterNum, decision, true)
	return false, nil
}

// acceptIteration promotes a successful iteration.
func (l *Loop) acceptIteration(ctx context.Context, iterNum int, decision *Decision) (bool, error) {
	delta := (decision.Accuracy - l.state.CurrentAccuracy) * 100
	fmt.Fprintf(os.Stderr, "ACCEPTED: %.1f%% -> %.1f%% (+%.1f%%)\n",
		l.state.CurrentAccuracy*100, decision.Accuracy*100, delta)

	// Rotate baselines and export fresh snapshots.
	if err := RotateBaselines(ctx, l.conctl, l.cfg.Workdir, l.lock.TargetWorkflows); err != nil {
		log.Printf("Warning: failed to rotate baselines: %v", err)
	}

	// Update state.
	l.state.CurrentAccuracy = decision.Accuracy
	l.state.CurrentParseRate = decision.ParseRate
	l.state.CurrentCostPerItem = decision.CostPerItem
	l.state.CurrentFailedItems = decision.FailedItems
	l.state.CurrentRunID = decision.RunID
	l.state.CurrentAvgLatencyMS = decision.AvgLatencyMS
	l.state.CurrentP95LatencyMS = decision.P95LatencyMS
	l.state.PlateauCount = 0

	// Increment and save state BEFORE appending to memory.
	l.state.Iteration++
	now := time.Now()
	l.state.LastIterationAt = &now
	_ = l.state.Save(l.cfg.Workdir)

	_ = AppendIteration(l.cfg.Workdir, iterNum, decision, true)

	if decision.RecommendFullRun {
		fmt.Fprintf(os.Stderr, "Agent recommends a full benchmark run for validation\n")
	}

	// Stop if agent says stop_improved.
	if decision.Verdict == VerdictStopImproved {
		fmt.Fprintf(os.Stderr, "Agent recommends stopping — improvement achieved\n")
		return true, nil
	}

	return false, nil
}

// rollbackIteration restores workflows and increments plateau counter.
func (l *Loop) rollbackIteration(ctx context.Context, iterNum int, decision *Decision, reason string) (bool, error) {
	fmt.Fprintf(os.Stderr, "ROLLED BACK: %s\n", reason)

	if err := RestoreWorkflows(ctx, l.conctl, l.cfg.Workdir, l.lock.TargetWorkflows); err != nil {
		return false, fmt.Errorf("rollback restore: %w", err)
	}

	// Increment plateau for no-progress, not for safety-floor rejections.
	if decision.Verdict == VerdictStopNoProgress || decision.Verdict == VerdictContinue || decision.Verdict == VerdictRollback {
		l.state.PlateauCount++
	}

	// Increment and save state BEFORE appending to memory.
	l.state.Iteration++
	now := time.Now()
	l.state.LastIterationAt = &now
	_ = l.state.Save(l.cfg.Workdir)

	_ = AppendIteration(l.cfg.Workdir, iterNum, decision, false)
	return false, nil
}

// shouldStop checks non-iteration stop conditions.
func (l *Loop) shouldStop() bool {
	if l.state.PlateauCount >= l.cfg.StopAfterPlateau {
		log.Printf("Plateau limit reached (%d consecutive no-progress iterations)", l.state.PlateauCount)
		return true
	}

	if l.cfg.TotalBudgetUSD > 0 {
		totalSpent := l.state.TotalAgentCostUSD + l.state.TotalBenchCostUSD
		remainingBudget := l.cfg.TotalBudgetUSD - totalSpent
		if remainingBudget <= 0 {
			log.Println("Total budget exhausted")
			return true
		}
	}

	return false
}

func (l *Loop) recordDecisionCosts(ctx context.Context, decision *Decision) {
	if decision == nil {
		return
	}
	if err := l.recordBenchmarkRunCost(ctx, decision.RunID); err != nil {
		log.Printf("Warning: failed to record benchmark cost for %s: %v", decision.RunID, err)
	}
	if decision.TargetedReplayRunID != "" {
		if err := l.recordBenchmarkRunCost(ctx, decision.TargetedReplayRunID); err != nil {
			log.Printf("Warning: failed to record targeted replay cost for %s: %v", decision.TargetedReplayRunID, err)
		}
	}
	if decision.SanityRunID != "" {
		if err := l.recordBenchmarkRunCost(ctx, decision.SanityRunID); err != nil {
			log.Printf("Warning: failed to record sanity benchmark cost for %s: %v", decision.SanityRunID, err)
		}
	}
}

func (l *Loop) recordBenchmarkRunCost(ctx context.Context, runID string) error {
	if strings.TrimSpace(runID) == "" || l.state.HasCountedBenchRun(runID) {
		return nil
	}

	out, err := l.conctl.RunJSON(ctx, "benchmarks", "get", "--id", runID)
	if err != nil {
		return err
	}
	var envelope struct {
		Run struct {
			TotalCostUSD float64 `json:"total_cost_usd"`
		} `json:"Run"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		return fmt.Errorf("parse run cost: %w", err)
	}

	l.state.TotalBenchCostUSD += envelope.Run.TotalCostUSD
	l.state.MarkBenchRunCounted(runID)
	if saveErr := l.state.Save(l.cfg.Workdir); saveErr != nil {
		return fmt.Errorf("save state after cost accounting: %w", saveErr)
	}
	return nil
}
