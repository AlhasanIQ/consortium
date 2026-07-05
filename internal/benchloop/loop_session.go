package benchloop

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

func (l *Loop) persistAgentSession(phase string, iteration, attempt int, result *AgentResult) {
	if result == nil {
		return
	}

	record := &SessionRecord{
		LoopSessionID:   l.state.SessionID,
		Phase:           phase,
		Iteration:       iteration,
		Attempt:         attempt,
		ClaudeSessionID: result.ClaudeSessionID,
		TranscriptPath:  result.TranscriptPath,
		LogPath:         result.LogPath,
		ExitCode:        result.ExitCode,
		StartedAt:       result.StartedAt,
		FinishedAt:      result.FinishedAt,
	}
	if err := AppendSessionRecord(l.cfg.Workdir, record); err != nil {
		log.Printf("Warning: failed to append session index: %v", err)
	}

	l.state.LastClaudeSessionID = result.ClaudeSessionID
	l.state.LastTranscriptPath = result.TranscriptPath

	if result.ClaudeSessionID != "" {
		fmt.Fprintf(os.Stderr, "Claude session: %s\n", result.ClaudeSessionID)
	}
	if result.TranscriptPath != "" {
		fmt.Fprintf(os.Stderr, "Transcript: %s\n", result.TranscriptPath)
	}
}

func (l *Loop) updateLiveStatus(phase, step string, iteration, attempt int, message string) {
	if l == nil || l.state == nil {
		return
	}

	now := time.Now()
	if l.state.Live == nil ||
		l.state.Live.Iteration != iteration ||
		l.state.Live.Attempt != attempt {
		l.state.Live = &LiveStatus{
			StartedAt: &now,
		}
	}
	live := l.state.Live
	live.Phase = phase
	live.Step = step
	live.Message = message
	live.Iteration = iteration
	live.Attempt = attempt
	live.UpdatedAt = &now

	if err := l.state.Save(l.cfg.Workdir); err != nil {
		log.Printf("Warning: failed to save live status: %v", err)
	}
}

func (l *Loop) setLiveAgent(start *AgentResult) {
	if l == nil || l.state == nil || start == nil {
		return
	}

	if l.state.Live == nil {
		now := time.Now()
		l.state.Live = &LiveStatus{StartedAt: &now}
	}
	live := l.state.Live
	live.AgentPID = start.ProcessPID
	live.AgentSessionID = start.ClaudeSessionID
	live.AgentLogPath = start.LogPath
	live.TranscriptPath = start.TranscriptPath
	now := time.Now()
	live.UpdatedAt = &now

	if err := l.state.Save(l.cfg.Workdir); err != nil {
		log.Printf("Warning: failed to save live agent status: %v", err)
	}
}

func (l *Loop) clearLiveStatus() {
	if l == nil || l.state == nil {
		return
	}
	l.state.Live = nil
}

func (l *Loop) appendLiveLogLine(format string, args ...interface{}) {
	if l == nil || l.state == nil || l.state.Live == nil {
		return
	}
	path := strings.TrimSpace(l.state.Live.AgentLogPath)
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, format, args...)
}

// printSummary prints the final loop summary.
func (l *Loop) printSummary() {
	fmt.Println("\n=== Benchloop Complete ===")
	fmt.Printf("Iterations run:     %d\n", l.state.Iteration)
	fmt.Printf("Status:             %s\n", l.state.Status)

	if l.state.BaselineRunID != "" {
		fmt.Printf("Initial baseline:   %.1f%% (run: %s)\n", l.state.BaselineAccuracy*100, l.state.BaselineRunID)
	}

	if l.state.CurrentRunID != "" && l.state.CurrentRunID != l.state.BaselineRunID {
		delta := (l.state.CurrentAccuracy - l.state.BaselineAccuracy) * 100
		sign := "+"
		if delta < 0 {
			sign = ""
		}
		fmt.Printf("Final accuracy:     %.1f%% (%s%.1f%%) (run: %s)\n",
			l.state.CurrentAccuracy*100, sign, delta, l.state.CurrentRunID)
	} else if l.state.CurrentRunID != "" {
		fmt.Printf("Final accuracy:     %.1f%% (unchanged from baseline)\n", l.state.CurrentAccuracy*100)
	}

	if l.state.CurrentAvgLatencyMS > 0 {
		fmt.Printf("Avg latency:        %.0fms (p95: %.0fms)\n", l.state.CurrentAvgLatencyMS, l.state.CurrentP95LatencyMS)
	}
	fmt.Printf("Agent cost:         $%.2f\n", l.state.TotalAgentCostUSD)
	fmt.Printf("Benchmark cost:     $%.2f\n", l.state.TotalBenchCostUSD)
	fmt.Printf("Total spend:        $%.2f\n", l.state.TotalAgentCostUSD+l.state.TotalBenchCostUSD)
	fmt.Printf("Plateau count:      %d\n", l.state.PlateauCount)

	if l.state.CurrentAccuracy > l.state.BaselineAccuracy {
		fmt.Println("\nImprovement detected. Consider running a full benchmark for validation:")
		conctlBin := conctlBinaryPath(l.cfg.Workdir)
		if l.conctl != nil && strings.TrimSpace(l.conctl.BinPath()) != "" {
			conctlBin = l.conctl.BinPath()
		}
		fmt.Printf("  %q benchmarks run --yes --benchmarks %s --workflows %s --run-set full\n",
			conctlBin, l.state.Benchmark, strings.Join(l.lock.WorkflowOrder, ","))
	}
}

// accumulateUXRecommendations saves non-empty tooling feedback from each decision
// into state so they can be consolidated at session end.
func (l *Loop) accumulateUXRecommendations(decision *Decision) {
	if decision == nil {
		return
	}
	rec := strings.TrimSpace(decision.UXRecommendations)
	if rec == "" {
		return
	}
	// Skip generic "no issues" responses.
	lower := strings.ToLower(rec)
	if strings.Contains(lower, "no issues") && strings.Contains(lower, "sufficient") {
		return
	}
	l.state.UXRecommendations = append(l.state.UXRecommendations, rec)
}
