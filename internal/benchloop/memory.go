package benchloop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MemoryPath returns the path to loop_memory.md.
func MemoryPath(workdir string) string {
	return filepath.Join(workdir, "benchmarks", "loop", "loop_memory.md")
}

// InitMemory creates the memory file or appends a new session header if it already exists.
// Memory accumulates across sessions so the agent can learn from prior tuning attempts.
func InitMemory(workdir string, state *State) error {
	path := MemoryPath(workdir)

	itemDesc := fmt.Sprintf("%d items", state.ItemLimit)
	if state.ItemLimit == 0 {
		itemDesc = "all items"
	}
	header := fmt.Sprintf(`## Session
- Target: %s
- Benchmark: %s (%s split, %s)
- Started: %s
- Baseline: %s
`,
		strings.Join(state.TargetWorkflows, ", "),
		state.Benchmark,
		state.Split,
		itemDesc,
		state.StartedAt.Format(time.RFC3339),
		formatBaseline(state),
	)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		// First session ever — create with top-level heading.
		content := "# Benchmark Tuning Loop Memory\n\n" + header
		return os.WriteFile(path, []byte(content), 0644)
	}

	// File exists from a prior session — append a session separator.
	separator := fmt.Sprintf("\n---\n\n# New Session (%s)\n\n", state.SessionID)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open memory for new session: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.WriteString(separator + header); err != nil {
		return fmt.Errorf("append session header: %w", err)
	}
	return nil
}

// AppendIteration appends a canonical iteration summary to the memory file.
func AppendIteration(workdir string, iteration int, decision *Decision, accepted bool) error {
	path := MemoryPath(workdir)

	status := "ACCEPTED"
	if !accepted {
		status = "ROLLED BACK"
		switch decision.Verdict {
		case VerdictStopNoProgress:
			status += " (no progress)"
		case VerdictRollback:
			status += " (agent requested rollback)"
		default:
			status += " (gate rejected)"
		}
	}

	var b strings.Builder
	b.WriteString("\n---\n\n")
	b.WriteString(fmt.Sprintf("## Iteration %d — %s\n", iteration, status))
	b.WriteString(fmt.Sprintf("**Accuracy:** %.1f%%", decision.Accuracy*100))
	b.WriteString(fmt.Sprintf(" | Parse: %.0f%%", decision.ParseRate*100))
	b.WriteString(fmt.Sprintf(" | Cost/item: $%.4f", decision.CostPerItem))
	if decision.AvgLatencyMS > 0 {
		b.WriteString(fmt.Sprintf(" | Latency: %.0fms avg", decision.AvgLatencyMS))
	}
	if decision.P95LatencyMS > 0 {
		b.WriteString(fmt.Sprintf(" / %.0fms p95", decision.P95LatencyMS))
	}
	b.WriteString(fmt.Sprintf(" | Failed: %d", decision.FailedItems))
	if decision.TotalItems > 0 {
		b.WriteString(fmt.Sprintf(" | Items: %d", decision.TotalItems))
	}
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("**Run:** %s\n", decision.RunID))
	if decision.TargetedReplayRunID != "" {
		replayStatus := "failed"
		if decision.TargetedReplayPass {
			replayStatus = "passed"
		}
		if decision.TargetedReplayItem != "" {
			b.WriteString(fmt.Sprintf("**Targeted replay:** %s (item %s, run %s)\n", replayStatus, decision.TargetedReplayItem, decision.TargetedReplayRunID))
		} else {
			b.WriteString(fmt.Sprintf("**Targeted replay:** %s (run %s)\n", replayStatus, decision.TargetedReplayRunID))
		}
	}
	b.WriteString(fmt.Sprintf("**Changes:** %s\n", decision.ChangesSummary))
	b.WriteString(fmt.Sprintf("**Reasoning:** %s\n", decision.Reasoning))
	if decision.FailureAnalysis != "" {
		b.WriteString(fmt.Sprintf("**Failures:** %s\n", decision.FailureAnalysis))
	}
	if decision.UXRecommendations != "" {
		b.WriteString(fmt.Sprintf("**UX recommendations:** %s\n", decision.UXRecommendations))
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("open memory file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.WriteString(b.String()); err != nil {
		return fmt.Errorf("append to memory: %w", err)
	}
	return nil
}

// AppendNote appends a free-form note to the memory file (e.g., for crashes, matrix changes).
func AppendNote(workdir string, iteration int, note string) error {
	path := MemoryPath(workdir)
	entry := fmt.Sprintf("\n---\n\n## Iteration %d — NOTE\n%s\n", iteration, note)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("open memory file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.WriteString(entry); err != nil {
		return fmt.Errorf("append note to memory: %w", err)
	}
	return nil
}

// AppendToolingRecommendations writes a consolidated "Conctl Improvement Recommendations"
// section to the memory file at session end. Only called when there are accumulated
// recommendations from the session's iterations.
func AppendToolingRecommendations(workdir string, recommendations []string) error {
	if len(recommendations) == 0 {
		return nil
	}
	path := MemoryPath(workdir)

	var b strings.Builder
	b.WriteString("\n---\n\n")
	b.WriteString("## Conctl Improvement Recommendations\n")
	b.WriteString("Consolidated from agent feedback across this session's iterations.\n\n")
	for i, rec := range recommendations {
		b.WriteString(fmt.Sprintf("%d. %s\n", i+1, rec))
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return fmt.Errorf("open memory for tooling recommendations: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.WriteString(b.String()); err != nil {
		return fmt.Errorf("append tooling recommendations: %w", err)
	}
	return nil
}

// ReadMemory returns the current contents of the memory file.
func ReadMemory(workdir string) (string, error) {
	data, err := os.ReadFile(MemoryPath(workdir))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read memory: %w", err)
	}
	return string(data), nil
}

// ReadMemoryTruncated returns memory contents, truncated to maxLines if needed.
// When truncated, it keeps the session header (lines before the first "---" separator)
// and the last iterations, prepending a truncation notice.
func ReadMemoryTruncated(workdir string, maxLines int) (string, error) {
	content, err := ReadMemory(workdir)
	if err != nil || content == "" {
		return content, err
	}

	lines := strings.Split(content, "\n")
	if len(lines) <= maxLines {
		return content, nil
	}

	// Find the end of the session header (first "---" separator).
	headerEnd := 0
	for i, line := range lines {
		if strings.TrimSpace(line) == "---" {
			headerEnd = i
			break
		}
	}
	if headerEnd == 0 {
		// No separator found — just take the last maxLines.
		headerEnd = 0
	}

	header := lines[:headerEnd]
	remaining := lines[headerEnd:]

	// How many lines can we keep from the tail?
	// Reserve space for header + truncation notice (2 lines).
	tailBudget := maxLines - len(header) - 2
	if tailBudget < 10 {
		tailBudget = 10
	}
	if tailBudget >= len(remaining) {
		return content, nil // fits after all
	}

	truncated := len(remaining) - tailBudget
	tail := remaining[len(remaining)-tailBudget:]

	var b strings.Builder
	for _, line := range header {
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf("\n[... %d earlier lines truncated ...]\n", truncated))
	b.WriteString(strings.Join(tail, "\n"))

	return b.String(), nil
}

func formatBaseline(state *State) string {
	if state.BaselineRunID != "" {
		return fmt.Sprintf("%.1f%% (run: %s)", state.BaselineAccuracy*100, state.BaselineRunID)
	}
	return "(will be established in iteration 1)"
}
