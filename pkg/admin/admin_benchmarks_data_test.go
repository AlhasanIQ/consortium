package admin

import (
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/bench"
	"github.com/alhasaniq/consortium/pkg/storage"
)

// ---------------------------------------------------------------------------
// dedupeOrderedStrings
// ---------------------------------------------------------------------------

func TestBenchmarkData_DedupeOrderedStrings(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{name: "nil input", input: nil, want: []string{}},
		{name: "empty slice", input: []string{}, want: []string{}},
		{name: "no duplicates", input: []string{"a", "b", "c"}, want: []string{"a", "b", "c"}},
		{name: "with duplicates", input: []string{"a", "b", "a", "c", "b"}, want: []string{"a", "b", "c"}},
		{name: "preserves order", input: []string{"z", "a", "m"}, want: []string{"z", "a", "m"}},
		{name: "trims whitespace", input: []string{" a ", "a", "  b"}, want: []string{"a", "b"}},
		{name: "skips empty and whitespace-only", input: []string{"", " ", "a", "", "b"}, want: []string{"a", "b"}},
		{name: "all empty", input: []string{"", " ", "  "}, want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dedupeOrderedStrings(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d; got %v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("index %d: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// sortedMapKeys
// ---------------------------------------------------------------------------

func TestBenchmarkData_SortedMapKeys(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]struct{}
		want  []string
	}{
		{name: "nil map", input: nil, want: []string{}},
		{name: "empty map", input: map[string]struct{}{}, want: []string{}},
		{name: "single key", input: map[string]struct{}{"x": {}}, want: []string{"x"}},
		{name: "sorted output", input: map[string]struct{}{"c": {}, "a": {}, "b": {}}, want: []string{"a", "b", "c"}},
		{name: "skips empty key", input: map[string]struct{}{"b": {}, "": {}, "a": {}}, want: []string{"a", "b"}},
		{name: "skips whitespace-only key", input: map[string]struct{}{" ": {}, "z": {}}, want: []string{"z"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sortedMapKeys(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d; got %v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("index %d: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseIntDefault
// ---------------------------------------------------------------------------

func TestBenchmarkData_ParseIntDefault(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		fallback int
		want     int
	}{
		{name: "valid int", raw: "42", fallback: 0, want: 42},
		{name: "empty uses fallback", raw: "", fallback: 10, want: 10},
		{name: "whitespace uses fallback", raw: "  ", fallback: 5, want: 5},
		{name: "invalid uses fallback", raw: "abc", fallback: 7, want: 7},
		{name: "negative int", raw: "-3", fallback: 0, want: -3},
		{name: "zero", raw: "0", fallback: 99, want: 0},
		{name: "trimmed whitespace", raw: " 15 ", fallback: 0, want: 15},
		{name: "float uses fallback", raw: "3.14", fallback: 1, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseIntDefault(tt.raw, tt.fallback)
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// strPtr
// ---------------------------------------------------------------------------

func TestBenchmarkData_StrPtr(t *testing.T) {
	got := strPtr("hello")
	if got == nil {
		t.Fatal("expected non-nil pointer")
	}
	if *got != "hello" {
		t.Errorf("got %q, want %q", *got, "hello")
	}
	got2 := strPtr("")
	if *got2 != "" {
		t.Errorf("got %q, want empty string", *got2)
	}
}

// ---------------------------------------------------------------------------
// collectBenchmarkFilters
// ---------------------------------------------------------------------------

func TestBenchmarkData_CollectBenchmarkFilters(t *testing.T) {
	runs := []storage.BenchmarkRun{
		{Benchmark: "mmlu", WorkflowID: "wf-a", Status: "completed", Split: "dev"},
		{Benchmark: "gpqa", WorkflowID: "wf-b", Status: "running", Split: "test"},
		{Benchmark: "mmlu", WorkflowID: "wf-a", Status: "completed", Split: "dev"},
		{Benchmark: "gpqa", WorkflowID: "wf-c", Status: "completed", Split: ""},
	}
	benchmarks, workflows, statuses, splits := collectBenchmarkFilters(runs)

	assertStringSlice(t, "benchmarks", benchmarks, []string{"gpqa", "mmlu"})
	assertStringSlice(t, "workflows", workflows, []string{"wf-a", "wf-b", "wf-c"})
	assertStringSlice(t, "statuses", statuses, []string{"completed", "running"})
	assertStringSlice(t, "splits", splits, []string{"dev", "test"})
}

func TestBenchmarkData_CollectBenchmarkFilters_Empty(t *testing.T) {
	benchmarks, workflows, statuses, splits := collectBenchmarkFilters(nil)
	assertStringSlice(t, "benchmarks", benchmarks, []string{})
	assertStringSlice(t, "workflows", workflows, []string{})
	assertStringSlice(t, "statuses", statuses, []string{})
	assertStringSlice(t, "splits", splits, []string{})
}

// ---------------------------------------------------------------------------
// buildRunChartSeries
// ---------------------------------------------------------------------------

func TestBenchmarkData_BuildRunChartSeries(t *testing.T) {
	now := time.Now().UTC()
	started := now.Add(-time.Hour)

	runs := []storage.BenchmarkRun{
		// Good dev run
		{ID: "r1", WorkflowID: "wf", Benchmark: "mmlu", Status: "completed", Split: "dev", TotalItems: 100, Accuracy: 0.85, TotalCostUSD: 1.5, CreatedAt: now, StartedAt: &started},
		// Good test run
		{ID: "r2", WorkflowID: "wf", Benchmark: "mmlu", Status: "completed", Split: "test", TotalItems: 100, Accuracy: 0.80, TotalCostUSD: 2.0, CreatedAt: now},
		// Excluded: not completed
		{ID: "r3", WorkflowID: "wf", Benchmark: "mmlu", Status: "running", Split: "dev", TotalItems: 100},
		// Excluded: optimizer source
		{ID: "r4", WorkflowID: "wf", Benchmark: "mmlu", Status: "completed", Split: "dev", TotalItems: 100, Source: "optimizer"},
		// Excluded: has item limit
		{ID: "r5", WorkflowID: "wf", Benchmark: "mmlu", Status: "completed", Split: "dev", TotalItems: 50, ItemLimit: 50},
		// Excluded: wrong split
		{ID: "r6", WorkflowID: "wf", Benchmark: "mmlu", Status: "completed", Split: "train", TotalItems: 100},
	}

	series := buildRunChartSeries(runs)

	// Should get test first, then dev (ordered slice)
	if len(series) != 2 {
		t.Fatalf("expected 2 series, got %d", len(series))
	}
	if series[0].Split != "test" {
		t.Errorf("first series split = %q, want %q", series[0].Split, "test")
	}
	if series[1].Split != "dev" {
		t.Errorf("second series split = %q, want %q", series[1].Split, "dev")
	}

	// test series should have r2 only
	if len(series[0].Points) != 1 {
		t.Fatalf("test series: expected 1 point, got %d", len(series[0].Points))
	}
	if series[0].Points[0].RunID != "r2" {
		t.Errorf("test point RunID = %q, want %q", series[0].Points[0].RunID, "r2")
	}

	// dev series should have r1 only (using StartedAt for date)
	if len(series[1].Points) != 1 {
		t.Fatalf("dev series: expected 1 point, got %d", len(series[1].Points))
	}
	devPt := series[1].Points[0]
	if devPt.RunID != "r1" {
		t.Errorf("dev point RunID = %q, want %q", devPt.RunID, "r1")
	}
	if devPt.Accuracy != 0.85 {
		t.Errorf("dev point Accuracy = %f, want 0.85", devPt.Accuracy)
	}
	if devPt.Date != started.Format(time.RFC3339) {
		t.Errorf("dev point Date = %q, want StartedAt-based date %q", devPt.Date, started.Format(time.RFC3339))
	}
}

func TestBenchmarkData_BuildRunChartSeries_Empty(t *testing.T) {
	series := buildRunChartSeries(nil)
	if len(series) != 0 {
		t.Errorf("expected 0 series, got %d", len(series))
	}
}

func TestBenchmarkData_BuildRunChartSeries_FilterPartialRuns(t *testing.T) {
	now := time.Now().UTC()
	runs := []storage.BenchmarkRun{
		{ID: "full", WorkflowID: "wf", Benchmark: "mmlu", Status: "completed", Split: "dev", TotalItems: 100, CreatedAt: now},
		{ID: "partial", WorkflowID: "wf", Benchmark: "mmlu", Status: "completed", Split: "dev", TotalItems: 50, CreatedAt: now},
	}
	series := buildRunChartSeries(runs)
	if len(series) != 1 {
		t.Fatalf("expected 1 series, got %d", len(series))
	}
	if len(series[0].Points) != 1 {
		t.Fatalf("expected 1 point, got %d", len(series[0].Points))
	}
	if series[0].Points[0].RunID != "full" {
		t.Errorf("expected full run, got %q", series[0].Points[0].RunID)
	}
}

func TestBenchmarkData_BuildRunChartSeries_MaxPoints(t *testing.T) {
	now := time.Now().UTC()
	runs := make([]storage.BenchmarkRun, 35)
	for i := range runs {
		runs[i] = storage.BenchmarkRun{
			ID:         "r" + time.Duration(i).String(),
			WorkflowID: "wf",
			Benchmark:  "mmlu",
			Status:     "completed",
			Split:      "dev",
			TotalItems: 100,
			CreatedAt:  now.Add(time.Duration(i) * time.Minute),
		}
	}
	series := buildRunChartSeries(runs)
	if len(series) != 1 {
		t.Fatalf("expected 1 series, got %d", len(series))
	}
	if len(series[0].Points) != 30 {
		t.Errorf("expected max 30 points, got %d", len(series[0].Points))
	}
}

// ---------------------------------------------------------------------------
// sortedChartData
// ---------------------------------------------------------------------------

func TestBenchmarkData_SortedChartData(t *testing.T) {
	tests := []struct {
		name       string
		counts     map[string]int
		wantLabels []string
		wantValues []int
	}{
		{name: "nil map", counts: nil, wantLabels: []string{}, wantValues: []int{}},
		{name: "empty map", counts: map[string]int{}, wantLabels: []string{}, wantValues: []int{}},
		{
			name:       "sorted alphabetically",
			counts:     map[string]int{"timeout": 3, "auth_error": 1, "parse_fail": 5},
			wantLabels: []string{"auth_error", "parse_fail", "timeout"},
			wantValues: []int{1, 5, 3},
		},
		{
			name:       "single entry",
			counts:     map[string]int{"only": 42},
			wantLabels: []string{"only"},
			wantValues: []int{42},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labels, values := sortedChartData(tt.counts)
			assertStringSlice(t, "labels", labels, tt.wantLabels)
			assertIntSlice(t, "values", values, tt.wantValues)
		})
	}
}

// ---------------------------------------------------------------------------
// buildFailureChartData
// ---------------------------------------------------------------------------

func TestBenchmarkData_BuildFailureChartData_AllAttempts(t *testing.T) {
	run := &storage.BenchmarkRun{
		AllAttemptFailureCounts: map[string]int{"timeout": 5, "auth_error": 2},
		FailureReasonCounts:     map[string]int{"timeout": 3},
	}
	labels, values := buildFailureChartData(run, nil, failureSourceAllAttempts)
	assertStringSlice(t, "labels", labels, []string{"auth_error", "timeout"})
	assertIntSlice(t, "values", values, []int{2, 5})
}

func TestBenchmarkData_BuildFailureChartData_AllAttemptsFallsBackToFinalOnly(t *testing.T) {
	run := &storage.BenchmarkRun{
		AllAttemptFailureCounts: map[string]int{},
		FailureReasonCounts:     map[string]int{"parse_fail": 4},
	}
	labels, values := buildFailureChartData(run, nil, failureSourceAllAttempts)
	assertStringSlice(t, "labels", labels, []string{"parse_fail"})
	assertIntSlice(t, "values", values, []int{4})
}

func TestBenchmarkData_BuildFailureChartData_FinalOnly(t *testing.T) {
	run := &storage.BenchmarkRun{
		AllAttemptFailureCounts: map[string]int{"timeout": 10},
		FailureReasonCounts:     map[string]int{"timeout": 3},
	}
	labels, values := buildFailureChartData(run, nil, failureSourceFinalOnly)
	// Should use FailureReasonCounts, not AllAttemptFailureCounts
	assertStringSlice(t, "labels", labels, []string{"timeout"})
	assertIntSlice(t, "values", values, []int{3})
}

func TestBenchmarkData_BuildFailureChartData_FallsBackToItems(t *testing.T) {
	run := &storage.BenchmarkRun{}
	items := []storage.BenchmarkRunItem{
		{FailureReason: "timeout"},
		{FailureReason: "timeout"},
		{FailureReason: "auth_error"},
		{FailureReason: ""},
	}
	labels, values := buildFailureChartData(run, items, failureSourceAllAttempts)
	assertStringSlice(t, "labels", labels, []string{"auth_error", "timeout"})
	assertIntSlice(t, "values", values, []int{1, 2})
}

func TestBenchmarkData_BuildFailureChartData_WrongAnswer(t *testing.T) {
	run := &storage.BenchmarkRun{
		FailureReasonCounts: map[string]int{"timeout": 1},
	}
	items := []storage.BenchmarkRunItem{
		{Correct: false, ParseOK: true, FailureReason: ""},        // wrong_answer
		{Correct: false, ParseOK: true, FailureReason: ""},        // wrong_answer
		{Correct: true, ParseOK: true, FailureReason: ""},         // correct, skip
		{Correct: false, ParseOK: false, FailureReason: ""},       // parse failed, skip
		{Correct: false, ParseOK: true, FailureReason: "timeout"}, // has failure reason, skip
	}
	labels, values := buildFailureChartData(run, items, failureSourceFinalOnly)
	assertStringSlice(t, "labels", labels, []string{"timeout", "wrong_answer"})
	assertIntSlice(t, "values", values, []int{1, 2})
}

func TestBenchmarkData_BuildFailureChartData_SkipsZeroCounts(t *testing.T) {
	run := &storage.BenchmarkRun{
		AllAttemptFailureCounts: map[string]int{"timeout": 3, "zero_entry": 0},
	}
	labels, values := buildFailureChartData(run, nil, failureSourceAllAttempts)
	assertStringSlice(t, "labels", labels, []string{"timeout"})
	assertIntSlice(t, "values", values, []int{3})
}

// ---------------------------------------------------------------------------
// topIncorrectSubjects
// ---------------------------------------------------------------------------

func TestBenchmarkData_TopIncorrectSubjects(t *testing.T) {
	items := []storage.BenchmarkRunItem{
		{Correct: false, Subject: "math"},
		{Correct: false, Subject: "math"},
		{Correct: false, Subject: "math"},
		{Correct: false, Subject: "physics"},
		{Correct: false, Subject: "physics"},
		{Correct: false, Subject: "history"},
		{Correct: true, Subject: "math"}, // correct, skip
		{Correct: false, Subject: ""},    // unknown
		{Correct: false, Subject: "  "},  // whitespace → unknown
	}

	labels, values := topIncorrectSubjects(items, 3)
	// math=3, physics=2, unknown=2 (empty + whitespace)
	if len(labels) != 3 {
		t.Fatalf("expected 3 subjects, got %d: %v", len(labels), labels)
	}
	if labels[0] != "math" || values[0] != 3 {
		t.Errorf("first: got %q=%d, want math=3", labels[0], values[0])
	}
	// physics and unknown both have 2, alphabetical tiebreak
	if labels[1] != "physics" || values[1] != 2 {
		t.Errorf("second: got %q=%d, want physics=2", labels[1], values[1])
	}
	if labels[2] != "unknown" || values[2] != 2 {
		t.Errorf("third: got %q=%d, want unknown=2", labels[2], values[2])
	}
}

func TestBenchmarkData_TopIncorrectSubjects_LimitTruncation(t *testing.T) {
	items := []storage.BenchmarkRunItem{
		{Correct: false, Subject: "a"},
		{Correct: false, Subject: "b"},
		{Correct: false, Subject: "c"},
		{Correct: false, Subject: "d"},
	}
	labels, values := topIncorrectSubjects(items, 2)
	if len(labels) != 2 {
		t.Fatalf("expected 2 subjects, got %d", len(labels))
	}
	if len(values) != 2 {
		t.Fatalf("expected 2 values, got %d", len(values))
	}
}

func TestBenchmarkData_TopIncorrectSubjects_ZeroLimit(t *testing.T) {
	items := []storage.BenchmarkRunItem{
		{Correct: false, Subject: "a"},
		{Correct: false, Subject: "b"},
	}
	labels, _ := topIncorrectSubjects(items, 0)
	// limit=0 means no limit
	if len(labels) != 2 {
		t.Errorf("expected 2 subjects with limit=0, got %d", len(labels))
	}
}

func TestBenchmarkData_TopIncorrectSubjects_Empty(t *testing.T) {
	labels, values := topIncorrectSubjects(nil, 10)
	if len(labels) != 0 || len(values) != 0 {
		t.Errorf("expected empty slices, got labels=%v values=%v", labels, values)
	}
}

// ---------------------------------------------------------------------------
// collectUniqueJobIDs (generic)
// ---------------------------------------------------------------------------

func TestBenchmarkData_CollectUniqueJobIDs(t *testing.T) {
	items := []string{"job-1", "job-2", "job-1", "", "  ", "job-3", "job-2"}
	got := collectUniqueJobIDs(items, func(s string) string { return s })
	assertStringSlice(t, "jobIDs", got, []string{"job-1", "job-2", "job-3"})
}

func TestBenchmarkData_CollectUniqueJobIDs_Empty(t *testing.T) {
	got := collectUniqueJobIDs([]string{}, func(s string) string { return s })
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// uniqueAttemptJobIDs
// ---------------------------------------------------------------------------

func TestBenchmarkData_UniqueAttemptJobIDs(t *testing.T) {
	attempts := []storage.BenchmarkRunItemAttempt{
		{JobID: "j1"},
		{JobID: "j2"},
		{JobID: "j1"},
		{JobID: ""},
		{JobID: "j3"},
	}
	got := uniqueAttemptJobIDs(attempts)
	assertStringSlice(t, "jobIDs", got, []string{"j1", "j2", "j3"})
}

// ---------------------------------------------------------------------------
// collectBenchmarkItemJobIDs
// ---------------------------------------------------------------------------

func TestBenchmarkData_CollectBenchmarkItemJobIDs(t *testing.T) {
	items := []storage.BenchmarkRunItem{
		{JobID: "j-a"},
		{JobID: "j-b"},
		{JobID: "j-a"},
		{JobID: ""},
	}
	got := collectBenchmarkItemJobIDs(items)
	assertStringSlice(t, "jobIDs", got, []string{"j-a", "j-b"})
}

// ---------------------------------------------------------------------------
// collectBenchmarkRunAttemptJobIDs
// ---------------------------------------------------------------------------

func TestBenchmarkData_CollectBenchmarkRunAttemptJobIDs(t *testing.T) {
	results := []bench.ItemResult{
		{
			AttemptDetails: []bench.AttemptDetail{
				{JobID: "j1"},
				{JobID: "j2"},
			},
		},
		{
			AttemptDetails: []bench.AttemptDetail{
				{JobID: "j1"},
				{JobID: ""},
				{JobID: "j3"},
			},
		},
	}
	got := collectBenchmarkRunAttemptJobIDs(results)
	assertStringSlice(t, "jobIDs", got, []string{"j1", "j2", "j3"})
}

// ---------------------------------------------------------------------------
// applyExecutionUsageToBenchmarkItems
// ---------------------------------------------------------------------------

func TestBenchmarkData_ApplyExecutionUsageToBenchmarkItems(t *testing.T) {
	items := []storage.BenchmarkRunItem{
		{JobID: "j1", TotalTokens: 0, CostUSD: 0, LatencyMS: 0},
		{JobID: "j2", TotalTokens: 10, CostUSD: 0.01, LatencyMS: 50},
		{JobID: "j-missing"},
		{JobID: ""},
	}
	usageByJob := map[string]ExecutionCostSummary{
		"j1": {TotalTokens: 100, TotalCost: 0.50, TotalLatencyMs: 200},
		"j2": {TotalTokens: 200, TotalCost: 1.00, TotalLatencyMs: 300},
	}

	totalTokens, totalCost, totalLatency := applyExecutionUsageToBenchmarkItems(items, usageByJob)

	// j1 should be enriched
	if items[0].TotalTokens != 100 {
		t.Errorf("j1 tokens = %d, want 100", items[0].TotalTokens)
	}
	if items[0].CostUSD != 0.50 {
		t.Errorf("j1 cost = %f, want 0.50", items[0].CostUSD)
	}
	if items[0].LatencyMS != 200 {
		t.Errorf("j1 latency = %f, want 200", items[0].LatencyMS)
	}

	// j2 should be overwritten
	if items[1].TotalTokens != 200 {
		t.Errorf("j2 tokens = %d, want 200", items[1].TotalTokens)
	}

	// j-missing keeps zeros
	if items[2].TotalTokens != 0 {
		t.Errorf("j-missing tokens = %d, want 0", items[2].TotalTokens)
	}

	// Totals
	if totalTokens != 300 {
		t.Errorf("totalTokens = %d, want 300", totalTokens)
	}
	if totalCost != 1.50 {
		t.Errorf("totalCost = %f, want 1.50", totalCost)
	}
	if totalLatency != 500 {
		t.Errorf("totalLatency = %f, want 500", totalLatency)
	}
}

func TestBenchmarkData_ApplyExecutionUsageToBenchmarkItems_ZeroLatencyPreserved(t *testing.T) {
	items := []storage.BenchmarkRunItem{
		{JobID: "j1", LatencyMS: 99},
	}
	usageByJob := map[string]ExecutionCostSummary{
		"j1": {TotalTokens: 10, TotalCost: 0.01, TotalLatencyMs: 0},
	}
	applyExecutionUsageToBenchmarkItems(items, usageByJob)
	// Zero latency from usage should NOT overwrite existing latency
	if items[0].LatencyMS != 99 {
		t.Errorf("latency = %f, want 99 (should preserve when usage latency is 0)", items[0].LatencyMS)
	}
}

// ---------------------------------------------------------------------------
// convertAttemptDetailToInput (round-trip with attemptInputToBenchDetail)
// ---------------------------------------------------------------------------

func TestBenchmarkData_ConvertAttemptDetailRoundTrip(t *testing.T) {
	original := bench.AttemptDetail{
		Attempt:              2,
		JobID:                "job-123",
		LatencyMS:            150.5,
		TokensInput:          100,
		TokensOutput:         50,
		TotalTokens:          150,
		CostUSD:              0.025,
		RawOutput:            "The answer is B",
		Predicted:            "B",
		ParseOK:              true,
		Error:                "",
		FailureReason:        "timeout",
		OutputSource:         "openrouter",
		ContractNodeID:       "node-1",
		ContractModel:        "gpt-4",
		ContractFinishReason: "stop",
		ContractTokensOutput: 45,
		ContractMaxTokens:    1024,
		ContractDiagnostic:   "ok",
	}

	input := convertAttemptDetailToInput(original)
	roundTripped := attemptInputToBenchDetail(input)

	// Compare all fields
	if roundTripped.Attempt != original.Attempt {
		t.Errorf("Attempt: got %d, want %d", roundTripped.Attempt, original.Attempt)
	}
	if roundTripped.JobID != original.JobID {
		t.Errorf("JobID: got %q, want %q", roundTripped.JobID, original.JobID)
	}
	if roundTripped.LatencyMS != original.LatencyMS {
		t.Errorf("LatencyMS: got %f, want %f", roundTripped.LatencyMS, original.LatencyMS)
	}
	if roundTripped.TokensInput != original.TokensInput {
		t.Errorf("TokensInput: got %d, want %d", roundTripped.TokensInput, original.TokensInput)
	}
	if roundTripped.TokensOutput != original.TokensOutput {
		t.Errorf("TokensOutput: got %d, want %d", roundTripped.TokensOutput, original.TokensOutput)
	}
	if roundTripped.TotalTokens != original.TotalTokens {
		t.Errorf("TotalTokens: got %d, want %d", roundTripped.TotalTokens, original.TotalTokens)
	}
	if roundTripped.CostUSD != original.CostUSD {
		t.Errorf("CostUSD: got %f, want %f", roundTripped.CostUSD, original.CostUSD)
	}
	if roundTripped.Predicted != original.Predicted {
		t.Errorf("Predicted: got %q, want %q", roundTripped.Predicted, original.Predicted)
	}
	if roundTripped.ParseOK != original.ParseOK {
		t.Errorf("ParseOK: got %v, want %v", roundTripped.ParseOK, original.ParseOK)
	}
	if roundTripped.FailureReason != original.FailureReason {
		t.Errorf("FailureReason: got %q, want %q", roundTripped.FailureReason, original.FailureReason)
	}
	if roundTripped.ContractNodeID != original.ContractNodeID {
		t.Errorf("ContractNodeID: got %q, want %q", roundTripped.ContractNodeID, original.ContractNodeID)
	}
	if roundTripped.ContractModel != original.ContractModel {
		t.Errorf("ContractModel: got %q, want %q", roundTripped.ContractModel, original.ContractModel)
	}
	if roundTripped.ContractMaxTokens != original.ContractMaxTokens {
		t.Errorf("ContractMaxTokens: got %d, want %d", roundTripped.ContractMaxTokens, original.ContractMaxTokens)
	}
}

// ---------------------------------------------------------------------------
// dbAttemptToInput
// ---------------------------------------------------------------------------

func TestBenchmarkData_DbAttemptToInput(t *testing.T) {
	attempt := storage.BenchmarkRunItemAttempt{
		RunID:     "run-1",
		ItemID:    "item-1",
		Attempt:   3,
		JobID:     "job-x",
		CostUSD:   0.05,
		ParseOK:   true,
		RawOutput: "answer",
	}
	input := dbAttemptToInput(attempt)
	if input.Attempt != 3 {
		t.Errorf("Attempt: got %d, want 3", input.Attempt)
	}
	if input.JobID != "job-x" {
		t.Errorf("JobID: got %q, want %q", input.JobID, "job-x")
	}
	if input.CostUSD != 0.05 {
		t.Errorf("CostUSD: got %f, want 0.05", input.CostUSD)
	}
}

// ---------------------------------------------------------------------------
// convertItemResultToInput
// ---------------------------------------------------------------------------

func TestBenchmarkData_ConvertItemResultToInput(t *testing.T) {
	item := bench.ItemResult{
		ItemID:      "item-42",
		Subject:     "math",
		Language:    "en",
		AnswerLabel: "C",
		Predicted:   "C",
		ParseOK:     true,
		Correct:     true,
		JobID:       "job-7",
		Attempts:    2,
		AttemptDetails: []bench.AttemptDetail{
			{Attempt: 1, JobID: "job-6", Predicted: "A"},
			{Attempt: 2, JobID: "job-7", Predicted: "C"},
		},
	}
	input := convertItemResultToInput(item)
	if input.ItemID != "item-42" {
		t.Errorf("ItemID: got %q, want %q", input.ItemID, "item-42")
	}
	if input.Subject != "math" {
		t.Errorf("Subject: got %q, want %q", input.Subject, "math")
	}
	if !input.Correct {
		t.Error("Correct: expected true")
	}
	if len(input.AttemptDetails) != 2 {
		t.Fatalf("AttemptDetails: got %d, want 2", len(input.AttemptDetails))
	}
	if input.AttemptDetails[0].Predicted != "A" {
		t.Errorf("attempt[0].Predicted: got %q, want %q", input.AttemptDetails[0].Predicted, "A")
	}
	if input.AttemptDetails[1].Predicted != "C" {
		t.Errorf("attempt[1].Predicted: got %q, want %q", input.AttemptDetails[1].Predicted, "C")
	}
}

// ---------------------------------------------------------------------------
// convertDBItemToBenchResult
// ---------------------------------------------------------------------------

func TestBenchmarkData_ConvertDBItemToBenchResult(t *testing.T) {
	item := storage.BenchmarkRunItem{
		RunID:       "run-1",
		ItemID:      "item-1",
		Subject:     "chemistry",
		AnswerLabel: "D",
		Predicted:   "D",
		ParseOK:     true,
		Correct:     true,
		Attempts:    1,
	}
	attempts := []storage.BenchmarkRunItemAttempt{
		{RunID: "run-1", ItemID: "item-1", Attempt: 1, JobID: "job-1", Predicted: "D"},
	}

	result := convertDBItemToBenchResult(item, attempts)
	if result.ItemID != "item-1" {
		t.Errorf("ItemID: got %q, want %q", result.ItemID, "item-1")
	}
	if result.Subject != "chemistry" {
		t.Errorf("Subject: got %q, want %q", result.Subject, "chemistry")
	}
	if !result.Correct {
		t.Error("Correct: expected true")
	}
	if len(result.AttemptDetails) != 1 {
		t.Fatalf("AttemptDetails: got %d, want 1", len(result.AttemptDetails))
	}
	if result.AttemptDetails[0].Predicted != "D" {
		t.Errorf("attempt.Predicted: got %q, want %q", result.AttemptDetails[0].Predicted, "D")
	}
}

// ---------------------------------------------------------------------------
// convertDBItemsToBenchResults
// ---------------------------------------------------------------------------

func TestBenchmarkData_ConvertDBItemsToBenchResults(t *testing.T) {
	items := []storage.BenchmarkRunItem{
		{RunID: "r1", ItemID: "i1", Correct: true, Predicted: "A"},
		{RunID: "r1", ItemID: "i2", Correct: false, Predicted: "B"},
	}
	attempts := []storage.BenchmarkRunItemAttempt{
		{RunID: "r1", ItemID: "i1", Attempt: 1, JobID: "j1"},
		{RunID: "r1", ItemID: "i2", Attempt: 1, JobID: "j2"},
		{RunID: "r1", ItemID: "i2", Attempt: 2, JobID: "j3"},
	}

	results := convertDBItemsToBenchResults(items, attempts)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if len(results[0].AttemptDetails) != 1 {
		t.Errorf("item i1: expected 1 attempt, got %d", len(results[0].AttemptDetails))
	}
	if len(results[1].AttemptDetails) != 2 {
		t.Errorf("item i2: expected 2 attempts, got %d", len(results[1].AttemptDetails))
	}
}

func TestBenchmarkData_ConvertDBItemsToBenchResults_NoAttempts(t *testing.T) {
	items := []storage.BenchmarkRunItem{
		{RunID: "r1", ItemID: "i1", Correct: true},
	}
	results := convertDBItemsToBenchResults(items, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if len(results[0].AttemptDetails) != 0 {
		t.Errorf("expected 0 attempts, got %d", len(results[0].AttemptDetails))
	}
}

// ---------------------------------------------------------------------------
// convertBenchRunResult
// ---------------------------------------------------------------------------

func TestBenchmarkData_ConvertBenchRunResult(t *testing.T) {
	input := bench.RunResult{
		Summary: bench.RunSummary{
			RunID:        "run-abc",
			Benchmark:    "mmlu",
			Split:        "dev",
			WorkflowID:   "wf-1",
			WorkflowName: "test-wf",
			TotalItems:   100,
			CorrectItems: 85,
			Accuracy:     0.85,
		},
		Items: []bench.ItemResult{
			{ItemID: "q1", Correct: true, Predicted: "A"},
			{ItemID: "q2", Correct: false, Predicted: "B"},
		},
	}

	converted := convertBenchRunResult(input)
	if converted.Summary.RunID != "run-abc" {
		t.Errorf("RunID: got %q, want %q", converted.Summary.RunID, "run-abc")
	}
	if converted.Summary.Accuracy != 0.85 {
		t.Errorf("Accuracy: got %f, want 0.85", converted.Summary.Accuracy)
	}
	if len(converted.Items) != 2 {
		t.Fatalf("Items: got %d, want 2", len(converted.Items))
	}
	if converted.Items[0].ItemID != "q1" {
		t.Errorf("Items[0].ItemID: got %q, want %q", converted.Items[0].ItemID, "q1")
	}
}

// ---------------------------------------------------------------------------
// convertSingleBenchmarkItem
// ---------------------------------------------------------------------------

func TestBenchmarkData_ConvertSingleBenchmarkItem(t *testing.T) {
	item := bench.ItemResult{
		ItemID:    "item-99",
		Predicted: "X",
		Correct:   false,
	}
	result := convertSingleBenchmarkItem("run-1", item)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.ItemID != "item-99" {
		t.Errorf("ItemID: got %q, want %q", result.ItemID, "item-99")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func assertStringSlice(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: len = %d, want %d; got %v", label, len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s[%d]: got %q, want %q", label, i, got[i], want[i])
		}
	}
}

func assertIntSlice(t *testing.T, label string, got, want []int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: len = %d, want %d; got %v", label, len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s[%d]: got %d, want %d", label, i, got[i], want[i])
		}
	}
}
