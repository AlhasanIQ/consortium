package benchloop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func makeLoopDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	loopDir := filepath.Join(dir, "benchmarks", "loop")
	if err := os.MkdirAll(loopDir, 0755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestStateHasCountedBenchRun(t *testing.T) {
	s := &State{}

	// Empty state never has a run counted.
	if s.HasCountedBenchRun("run1") {
		t.Error("empty state should not have any run counted")
	}
	// Empty string is always false.
	if s.HasCountedBenchRun("") {
		t.Error("empty run ID should return false")
	}

	s.CountedBenchRunIDs = []string{"run1", "run2"}
	if !s.HasCountedBenchRun("run1") {
		t.Error("run1 should be counted")
	}
	if !s.HasCountedBenchRun("run2") {
		t.Error("run2 should be counted")
	}
	if s.HasCountedBenchRun("run3") {
		t.Error("run3 should not be counted")
	}
}

func TestStateMarkBenchRunCounted(t *testing.T) {
	s := &State{}

	// Mark a new run.
	s.MarkBenchRunCounted("run1")
	if len(s.CountedBenchRunIDs) != 1 || s.CountedBenchRunIDs[0] != "run1" {
		t.Errorf("expected [run1], got %v", s.CountedBenchRunIDs)
	}

	// Marking again is a no-op.
	s.MarkBenchRunCounted("run1")
	if len(s.CountedBenchRunIDs) != 1 {
		t.Errorf("expected dedup, got %v", s.CountedBenchRunIDs)
	}

	// Empty string is a no-op.
	s.MarkBenchRunCounted("")
	if len(s.CountedBenchRunIDs) != 1 {
		t.Errorf("empty id should not be added, got %v", s.CountedBenchRunIDs)
	}

	// Mark a second distinct run.
	s.MarkBenchRunCounted("run2")
	if len(s.CountedBenchRunIDs) != 2 {
		t.Errorf("expected 2 entries, got %v", s.CountedBenchRunIDs)
	}
}

func TestReadMemoryTruncated_ShortContent(t *testing.T) {
	dir := makeLoopDir(t)
	content := "# Header\nline1\nline2\nline3"
	if err := os.WriteFile(MemoryPath(dir), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// maxLines larger than content — should return full content.
	got, err := ReadMemoryTruncated(dir, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != content {
		t.Errorf("expected full content, got %q", got)
	}
}

func TestReadMemoryTruncated_LongContent(t *testing.T) {
	dir := makeLoopDir(t)

	// Build a file with a header, separator, then many lines.
	var sb strings.Builder
	sb.WriteString("# Header\n## Session\nStarted: today\n\n---\n\n")
	for i := 0; i < 100; i++ {
		sb.WriteString("iteration line\n")
	}
	content := sb.String()
	if err := os.WriteFile(MemoryPath(dir), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadMemoryTruncated(dir, 30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should be shorter than original.
	if len(got) >= len(content) {
		t.Error("expected truncated output to be shorter than original")
	}
	// Should contain truncation notice.
	if !strings.Contains(got, "truncated") {
		t.Error("expected truncation notice in output")
	}
}

func TestReadMemoryTruncated_MissingFile(t *testing.T) {
	dir := makeLoopDir(t)
	// No memory file written — should return empty string without error.
	got, err := ReadMemoryTruncated(dir, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestAppendIteration_AcceptedEntry(t *testing.T) {
	dir := makeLoopDir(t)
	// Create memory file first.
	if err := os.WriteFile(MemoryPath(dir), []byte("# Memory\n"), 0644); err != nil {
		t.Fatal(err)
	}

	d := &Decision{
		Verdict:        VerdictContinue,
		RunID:          "run-42",
		Accuracy:       0.87,
		ParseRate:      0.99,
		CostPerItem:    0.004,
		AvgLatencyMS:   1500,
		P95LatencyMS:   3000,
		FailedItems:    2,
		TotalItems:     50,
		ChangesSummary: "Adjusted prompt",
		Reasoning:      "Better wording",
	}

	if err := AppendIteration(dir, 3, d, true); err != nil {
		t.Fatalf("append: %v", err)
	}

	data, err := os.ReadFile(MemoryPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)

	if !strings.Contains(got, "Iteration 3 — ACCEPTED") {
		t.Error("expected ACCEPTED marker")
	}
	if !strings.Contains(got, "87.0%") {
		t.Error("expected accuracy in output")
	}
	if !strings.Contains(got, "run-42") {
		t.Error("expected run ID in output")
	}
	if !strings.Contains(got, "1500ms avg") {
		t.Error("expected avg latency in output")
	}
	if !strings.Contains(got, "3000ms p95") {
		t.Error("expected p95 latency in output")
	}
}

func TestAppendIteration_RolledBackEntry(t *testing.T) {
	dir := makeLoopDir(t)
	if err := os.WriteFile(MemoryPath(dir), []byte("# Memory\n"), 0644); err != nil {
		t.Fatal(err)
	}

	d := &Decision{
		Verdict:        VerdictRollback,
		RunID:          "run-bad",
		Accuracy:       0.70,
		ParseRate:      0.95,
		ChangesSummary: "Tried new model",
		Reasoning:      "Model was worse",
	}

	if err := AppendIteration(dir, 2, d, false); err != nil {
		t.Fatalf("append: %v", err)
	}

	data, _ := os.ReadFile(MemoryPath(dir))
	got := string(data)
	if !strings.Contains(got, "ROLLED BACK") {
		t.Error("expected ROLLED BACK marker")
	}
	if !strings.Contains(got, "agent requested rollback") {
		t.Error("expected rollback reason")
	}
}

func TestAppendNote(t *testing.T) {
	dir := makeLoopDir(t)
	if err := os.WriteFile(MemoryPath(dir), []byte("# Memory\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := AppendNote(dir, 5, "Agent crashed with OOM"); err != nil {
		t.Fatalf("append note: %v", err)
	}

	data, _ := os.ReadFile(MemoryPath(dir))
	got := string(data)
	if !strings.Contains(got, "Iteration 5 — NOTE") {
		t.Error("expected note marker")
	}
	if !strings.Contains(got, "Agent crashed with OOM") {
		t.Error("expected note content")
	}
}

func TestAppendToolingRecommendations_Empty(t *testing.T) {
	dir := makeLoopDir(t)
	// No-op for empty slice.
	if err := AppendToolingRecommendations(dir, nil); err != nil {
		t.Fatalf("unexpected error for empty recs: %v", err)
	}
}

func TestAppendToolingRecommendations_WithRecs(t *testing.T) {
	dir := makeLoopDir(t)
	if err := os.WriteFile(MemoryPath(dir), []byte("# Memory\n"), 0644); err != nil {
		t.Fatal(err)
	}

	recs := []string{"Add --verbose flag", "Show p95 latency in status"}
	if err := AppendToolingRecommendations(dir, recs); err != nil {
		t.Fatalf("append recs: %v", err)
	}

	data, _ := os.ReadFile(MemoryPath(dir))
	got := string(data)
	if !strings.Contains(got, "Conctl Improvement Recommendations") {
		t.Error("expected recommendations header")
	}
	if !strings.Contains(got, "Add --verbose flag") {
		t.Error("expected first recommendation")
	}
	if !strings.Contains(got, "Show p95 latency in status") {
		t.Error("expected second recommendation")
	}
}

func TestInitMemory_CreatesFile(t *testing.T) {
	dir := makeLoopDir(t)
	state := &State{
		SessionID:       "test_sess",
		TargetWorkflows: []string{"wf-a", "wf-b"},
		Benchmark:       "global-mmlu-lite",
		Split:           "dev",
		ItemLimit:       50,
		StartedAt:       time.Now(),
	}

	if err := InitMemory(dir, state); err != nil {
		t.Fatalf("init memory: %v", err)
	}

	data, err := os.ReadFile(MemoryPath(dir))
	if err != nil {
		t.Fatalf("read memory: %v", err)
	}
	got := string(data)

	if !strings.Contains(got, "Benchmark Tuning Loop Memory") {
		t.Error("expected top-level heading")
	}
	if !strings.Contains(got, "wf-a, wf-b") {
		t.Error("expected target workflows listed")
	}
	if !strings.Contains(got, "global-mmlu-lite") {
		t.Error("expected benchmark name")
	}
}

func TestInitMemory_AppendsSessionHeader(t *testing.T) {
	dir := makeLoopDir(t)
	state := &State{
		SessionID:       "sess1",
		TargetWorkflows: []string{"wf-a"},
		Benchmark:       "bench",
		Split:           "dev",
		ItemLimit:       10,
		StartedAt:       time.Now(),
	}

	// First init creates the file.
	if err := InitMemory(dir, state); err != nil {
		t.Fatalf("first init: %v", err)
	}

	// Second init (different session) should append.
	state.SessionID = "sess2"
	if err := InitMemory(dir, state); err != nil {
		t.Fatalf("second init: %v", err)
	}

	data, _ := os.ReadFile(MemoryPath(dir))
	got := string(data)

	if !strings.Contains(got, "New Session (sess2)") {
		t.Error("expected new session separator")
	}
}
