package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alhasaniq/consortium/pkg/workflow"
)

// --- parseBenchmarkRunRequest ---

func TestBenchmarkRunnerParseBenchmarkRunRequestDefaults(t *testing.T) {
	form := "benchmarks=global-mmlu-lite&workflows=reasoning-judge-pick&run_set=custom&split=dev"
	r := httptest.NewRequest(http.MethodPost, "/api/admin/benchmarks/run", strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	req, err := parseBenchmarkRunRequest(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(req.Benchmarks) != 1 || req.Benchmarks[0] != "global-mmlu-lite" {
		t.Fatalf("Benchmarks = %v, want [global-mmlu-lite]", req.Benchmarks)
	}
	if len(req.Workflows) != 1 || req.Workflows[0] != "reasoning-judge-pick" {
		t.Fatalf("Workflows = %v, want [reasoning-judge-pick]", req.Workflows)
	}
	if req.Concurrency < 1 {
		t.Fatalf("Concurrency = %d, want >= 1", req.Concurrency)
	}
	if req.SampleMode != benchmarkSampleModeHead {
		t.Fatalf("SampleMode = %q, want %q", req.SampleMode, benchmarkSampleModeHead)
	}
	if req.Source != "manual" {
		t.Fatalf("Source = %q, want manual", req.Source)
	}
	if !req.PauseOnFatal {
		t.Fatal("PauseOnFatal should default to true")
	}
	if req.AdmissionBypass {
		t.Fatal("AdmissionBypass should default to false")
	}
}

func TestBenchmarkRunnerParseBenchmarkRunRequestMultipleCSV(t *testing.T) {
	form := "benchmarks=global-mmlu-lite,math-500&workflows=wf-a,wf-b,wf-c&run_set=custom&split=dev"
	r := httptest.NewRequest(http.MethodPost, "/api/admin/benchmarks/run", strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	req, err := parseBenchmarkRunRequest(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(req.Benchmarks) != 2 {
		t.Fatalf("Benchmarks len = %d, want 2", len(req.Benchmarks))
	}
	if len(req.Workflows) != 3 {
		t.Fatalf("Workflows len = %d, want 3", len(req.Workflows))
	}
}

func TestBenchmarkRunnerParseBenchmarkRunRequestDeduplicates(t *testing.T) {
	form := "benchmarks=global-mmlu-lite,global-mmlu-lite&workflows=wf-a,wf-a&run_set=custom&split=dev"
	r := httptest.NewRequest(http.MethodPost, "/api/admin/benchmarks/run", strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	req, err := parseBenchmarkRunRequest(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(req.Benchmarks) != 1 {
		t.Fatalf("expected deduplicated benchmarks, got %v", req.Benchmarks)
	}
	if len(req.Workflows) != 1 {
		t.Fatalf("expected deduplicated workflows, got %v", req.Workflows)
	}
}

func TestBenchmarkRunnerParseBenchmarkRunRequestEmptyBenchmarks(t *testing.T) {
	form := "benchmarks=&workflows=wf-a&run_set=dev"
	r := httptest.NewRequest(http.MethodPost, "/api/admin/benchmarks/run", strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, err := parseBenchmarkRunRequest(r)
	if err == nil {
		t.Fatal("expected error for empty benchmarks")
	}
	if !strings.Contains(err.Error(), "benchmarks") {
		t.Fatalf("expected benchmarks error, got: %v", err)
	}
}

func TestBenchmarkRunnerParseBenchmarkRunRequestEmptyWorkflows(t *testing.T) {
	form := "benchmarks=global-mmlu-lite&workflows=&run_set=dev"
	r := httptest.NewRequest(http.MethodPost, "/api/admin/benchmarks/run", strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, err := parseBenchmarkRunRequest(r)
	if err == nil {
		t.Fatal("expected error for empty workflows")
	}
	if !strings.Contains(err.Error(), "workflows") {
		t.Fatalf("expected workflows error, got: %v", err)
	}
}

func TestBenchmarkRunnerParseBenchmarkRunRequestInvalidBenchmarkName(t *testing.T) {
	form := "benchmarks=not-a-real-benchmark&workflows=wf-a&run_set=custom&split=dev"
	r := httptest.NewRequest(http.MethodPost, "/api/admin/benchmarks/run", strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, err := parseBenchmarkRunRequest(r)
	if err == nil {
		t.Fatal("expected error for invalid benchmark name")
	}
	if !strings.Contains(err.Error(), "invalid benchmark") {
		t.Fatalf("expected invalid benchmark error, got: %v", err)
	}
}

func TestBenchmarkRunnerParseBenchmarkRunRequestInvalidRunSet(t *testing.T) {
	form := "benchmarks=global-mmlu-lite&workflows=wf-a&run_set=bogus"
	r := httptest.NewRequest(http.MethodPost, "/api/admin/benchmarks/run", strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, err := parseBenchmarkRunRequest(r)
	if err == nil {
		t.Fatal("expected error for invalid run_set")
	}
	if !strings.Contains(err.Error(), "run_set") {
		t.Fatalf("expected run_set error, got: %v", err)
	}
}

func TestBenchmarkRunnerParseBenchmarkRunRequestCustomRunSetRequiresSplit(t *testing.T) {
	form := "benchmarks=global-mmlu-lite&workflows=wf-a&run_set=custom"
	r := httptest.NewRequest(http.MethodPost, "/api/admin/benchmarks/run", strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, err := parseBenchmarkRunRequest(r)
	if err == nil {
		t.Fatal("expected error for custom run_set without split")
	}
	if !strings.Contains(err.Error(), "split") {
		t.Fatalf("expected split error, got: %v", err)
	}
}

func TestBenchmarkRunnerParseBenchmarkRunRequestCustomRunSetWithSplit(t *testing.T) {
	form := "benchmarks=global-mmlu-lite&workflows=wf-a&run_set=custom&split=dev"
	r := httptest.NewRequest(http.MethodPost, "/api/admin/benchmarks/run", strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	req, err := parseBenchmarkRunRequest(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Split == "" {
		t.Fatal("expected split to be set")
	}
}

func TestBenchmarkRunnerParseBenchmarkRunRequestInvalidSampleMode(t *testing.T) {
	form := "benchmarks=global-mmlu-lite&workflows=wf-a&run_set=dev&sample_mode=bogus"
	r := httptest.NewRequest(http.MethodPost, "/api/admin/benchmarks/run", strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, err := parseBenchmarkRunRequest(r)
	if err == nil {
		t.Fatal("expected error for invalid sample_mode")
	}
	if !strings.Contains(err.Error(), "sample_mode") {
		t.Fatalf("expected sample_mode error, got: %v", err)
	}
}

func TestBenchmarkRunnerParseBenchmarkRunRequestRandomSampleMode(t *testing.T) {
	form := "benchmarks=global-mmlu-lite&workflows=wf-a&run_set=custom&split=dev&sample_mode=random&sample_seed=42"
	r := httptest.NewRequest(http.MethodPost, "/api/admin/benchmarks/run", strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	req, err := parseBenchmarkRunRequest(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.SampleMode != benchmarkSampleModeRandom {
		t.Fatalf("SampleMode = %q, want random", req.SampleMode)
	}
	if req.SampleSeed != 42 {
		t.Fatalf("SampleSeed = %d, want 42", req.SampleSeed)
	}
}

func TestBenchmarkRunnerParseBenchmarkRunRequestInvalidSampleSeed(t *testing.T) {
	form := "benchmarks=global-mmlu-lite&workflows=wf-a&run_set=dev&sample_seed=notanumber"
	r := httptest.NewRequest(http.MethodPost, "/api/admin/benchmarks/run", strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, err := parseBenchmarkRunRequest(r)
	if err == nil {
		t.Fatal("expected error for invalid sample_seed")
	}
	if !strings.Contains(err.Error(), "sample_seed") {
		t.Fatalf("expected sample_seed error, got: %v", err)
	}
}

func TestBenchmarkRunnerParseBenchmarkRunRequestSourceValues(t *testing.T) {
	validSources := []string{"manual", "benchloop", "optimizer", "imported", "replay"}
	for _, src := range validSources {
		form := "benchmarks=global-mmlu-lite&workflows=wf-a&run_set=custom&split=dev&source=" + src
		r := httptest.NewRequest(http.MethodPost, "/api/admin/benchmarks/run", strings.NewReader(form))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		req, err := parseBenchmarkRunRequest(r)
		if err != nil {
			t.Fatalf("unexpected error for source=%s: %v", src, err)
		}
		if req.Source != src {
			t.Fatalf("Source = %q, want %q", req.Source, src)
		}
	}
}

func TestBenchmarkRunnerParseBenchmarkRunRequestInvalidSource(t *testing.T) {
	form := "benchmarks=global-mmlu-lite&workflows=wf-a&run_set=custom&split=dev&source=invalid"
	r := httptest.NewRequest(http.MethodPost, "/api/admin/benchmarks/run", strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, err := parseBenchmarkRunRequest(r)
	if err == nil {
		t.Fatal("expected error for invalid source")
	}
	if !strings.Contains(err.Error(), "source must be") {
		t.Fatalf("expected source validation error, got: %v", err)
	}
}

func TestBenchmarkRunnerParseBenchmarkRunRequestPauseOnFatalFalse(t *testing.T) {
	form := "benchmarks=global-mmlu-lite&workflows=wf-a&run_set=custom&split=dev&pause_on_fatal=false"
	r := httptest.NewRequest(http.MethodPost, "/api/admin/benchmarks/run", strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	req, err := parseBenchmarkRunRequest(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.PauseOnFatal {
		t.Fatal("PauseOnFatal should be false when pause_on_fatal=false")
	}
}

func TestBenchmarkRunnerParseBenchmarkRunRequestAdmissionBypass(t *testing.T) {
	form := "benchmarks=global-mmlu-lite&workflows=wf-a&run_set=custom&split=dev&admission_bypass=true"
	r := httptest.NewRequest(http.MethodPost, "/api/admin/benchmarks/run", strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	req, err := parseBenchmarkRunRequest(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !req.AdmissionBypass {
		t.Fatal("AdmissionBypass should be true when admission_bypass=true")
	}
}

func TestBenchmarkRunnerParseBenchmarkRunRequestOptimizerDspyMinibatchForcesSampleRandom(t *testing.T) {
	form := "benchmarks=global-mmlu-lite&workflows=wf-a&run_set=custom&split=dev&source=optimizer&opt_scope=dspy_minibatch"
	r := httptest.NewRequest(http.MethodPost, "/api/admin/benchmarks/run", strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	req, err := parseBenchmarkRunRequest(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.SampleMode != benchmarkSampleModeRandom {
		t.Fatalf("SampleMode = %q, want random for optimizer dspy_minibatch", req.SampleMode)
	}
	if req.SampleSeed == 0 {
		t.Fatal("SampleSeed should be auto-generated for dspy_minibatch when not provided")
	}
}

func TestBenchmarkRunnerParseBenchmarkRunRequestOptOrganismIDFallback(t *testing.T) {
	form := "benchmarks=global-mmlu-lite&workflows=wf-a&run_set=custom&split=dev&organism_id=org-123"
	r := httptest.NewRequest(http.MethodPost, "/api/admin/benchmarks/run", strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	req, err := parseBenchmarkRunRequest(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.OptOrganismID != "org-123" {
		t.Fatalf("OptOrganismID = %q, want org-123 (fallback from organism_id)", req.OptOrganismID)
	}
}

func TestBenchmarkRunnerParseBenchmarkRunRequestConcurrencyClampedToMin1(t *testing.T) {
	form := "benchmarks=global-mmlu-lite&workflows=wf-a&run_set=custom&split=dev&concurrency=0"
	r := httptest.NewRequest(http.MethodPost, "/api/admin/benchmarks/run", strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	req, err := parseBenchmarkRunRequest(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Concurrency < 1 {
		t.Fatalf("Concurrency = %d, want >= 1", req.Concurrency)
	}
}

func TestBenchmarkRunnerParseBenchmarkRunRequestFatalRepeatThresholdClampedToMin1(t *testing.T) {
	form := "benchmarks=global-mmlu-lite&workflows=wf-a&run_set=custom&split=dev&fatal_repeat_threshold=0"
	r := httptest.NewRequest(http.MethodPost, "/api/admin/benchmarks/run", strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	req, err := parseBenchmarkRunRequest(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.FatalRepeatThreshold < 1 {
		t.Fatalf("FatalRepeatThreshold = %d, want >= 1", req.FatalRepeatThreshold)
	}
}

func TestBenchmarkRunnerParseBenchmarkRunRequestUnsafeCSVBenchmarks(t *testing.T) {
	form := "benchmarks=foo;DROP TABLE&workflows=wf-a&run_set=dev"
	r := httptest.NewRequest(http.MethodPost, "/api/admin/benchmarks/run", strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, err := parseBenchmarkRunRequest(r)
	if err == nil {
		t.Fatal("expected error for unsafe benchmarks value")
	}
}

func TestBenchmarkRunnerParseBenchmarkRunRequestUnsafeCSVWorkflows(t *testing.T) {
	form := "benchmarks=global-mmlu-lite&workflows=wf a&run_set=dev"
	r := httptest.NewRequest(http.MethodPost, "/api/admin/benchmarks/run", strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, err := parseBenchmarkRunRequest(r)
	if err == nil {
		t.Fatal("expected error for unsafe workflows value")
	}
}

// --- summarizeValidationErrors ---

func TestBenchmarkRunnerSummarizeValidationErrorsEmpty(t *testing.T) {
	result := summarizeValidationErrors(nil, 5)
	if result != "unknown validation error" {
		t.Fatalf("got %q, want 'unknown validation error'", result)
	}
}

func TestBenchmarkRunnerSummarizeValidationErrorsSingleError(t *testing.T) {
	errors := []workflow.ValidationError{
		{Field: "model", NodeID: "agent-a", Message: "model is required"},
	}
	result := summarizeValidationErrors(errors, 5)
	if !strings.Contains(result, "model[agent-a]") {
		t.Fatalf("expected field[nodeID] format, got %q", result)
	}
	if !strings.Contains(result, "model is required") {
		t.Fatalf("expected message in output, got %q", result)
	}
}

func TestBenchmarkRunnerSummarizeValidationErrorsTruncation(t *testing.T) {
	errors := []workflow.ValidationError{
		{Field: "a", Message: "err1"},
		{Field: "b", Message: "err2"},
		{Field: "c", Message: "err3"},
		{Field: "d", Message: "err4"},
	}
	result := summarizeValidationErrors(errors, 2)
	if !strings.Contains(result, "err1") || !strings.Contains(result, "err2") {
		t.Fatalf("expected first 2 errors, got %q", result)
	}
	if strings.Contains(result, "err3") || strings.Contains(result, "err4") {
		t.Fatalf("expected errors 3 and 4 to be truncated, got %q", result)
	}
	if !strings.Contains(result, "2 more") {
		t.Fatalf("expected '... and 2 more' hint, got %q", result)
	}
}

func TestBenchmarkRunnerSummarizeValidationErrorsEmptyFieldAndMessage(t *testing.T) {
	errors := []workflow.ValidationError{
		{Field: "", NodeID: "", Message: ""},
	}
	result := summarizeValidationErrors(errors, 5)
	if !strings.Contains(result, "workflow") {
		t.Fatalf("expected 'workflow' fallback for empty field, got %q", result)
	}
	if !strings.Contains(result, "validation failed") {
		t.Fatalf("expected 'validation failed' fallback for empty message, got %q", result)
	}
}

func TestBenchmarkRunnerSummarizeValidationErrorsMaxErrorsZero(t *testing.T) {
	errors := []workflow.ValidationError{
		{Field: "a", Message: "err1"},
		{Field: "b", Message: "err2"},
	}
	// maxErrors < 1 should be clamped to 1
	result := summarizeValidationErrors(errors, 0)
	if !strings.Contains(result, "err1") {
		t.Fatalf("expected at least first error, got %q", result)
	}
	if strings.Contains(result, "err2") {
		t.Fatalf("expected second error truncated with maxErrors=0, got %q", result)
	}
}

func TestBenchmarkRunnerSummarizeValidationErrorsFieldWithoutNodeID(t *testing.T) {
	errors := []workflow.ValidationError{
		{Field: "timeout", Message: "must be positive"},
	}
	result := summarizeValidationErrors(errors, 5)
	// Field without NodeID should not have brackets
	if strings.Contains(result, "[") {
		t.Fatalf("expected no brackets when NodeID is empty, got %q", result)
	}
	if !strings.Contains(result, "timeout: must be positive") {
		t.Fatalf("expected 'timeout: must be positive', got %q", result)
	}
}

// --- launchBenchmarkRun ---

func TestBenchmarkRunnerLaunchRejectsWhenAlreadyActive(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	// Simulate an active benchmark runner
	server.benchmarkRunnerMu.Lock()
	server.benchmarkRunner = &benchmarkRunnerState{Running: true}
	server.benchmarkRunnerMu.Unlock()

	err := server.launchBenchmarkRun("test", 1, 10, benchmarkRunRequest{}, nil)
	if err == nil {
		t.Fatal("expected error when benchmark run is already active")
	}
	if !strings.Contains(err.Error(), "already active") {
		t.Fatalf("expected 'already active' error, got: %v", err)
	}
}

func TestBenchmarkRunnerLaunchAllowsWhenNotActive(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	// No active runner, but plans is empty so the goroutine will finish quickly
	err := server.launchBenchmarkRun("test", 0, 0, benchmarkRunRequest{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	server.benchmarkRunnerMu.Lock()
	running := server.benchmarkRunner != nil && server.benchmarkRunner.Running
	sessionID := server.benchmarkSessionID
	server.benchmarkRunnerMu.Unlock()

	if !running {
		t.Fatal("expected runner to be running after launch")
	}
	if sessionID == "" {
		t.Fatal("expected session ID to be set after launch")
	}
}

// --- forcedDirtyParentNodeIDs edge cases ---

func TestBenchmarkRunnerForcedDirtyParentNodeIDsNilWorkflow(t *testing.T) {
	got := forcedDirtyParentNodeIDs(nil, map[string]struct{}{"wf": {}})
	if len(got) != 0 {
		t.Fatalf("expected empty for nil workflow, got %v", got)
	}
}

func TestBenchmarkRunnerForcedDirtyParentNodeIDsEmptyChanged(t *testing.T) {
	wf := &workflow.Workflow{
		Nodes: []*workflow.Node{
			{ID: "child-a", Type: workflow.NodeTypeChildWorkflow, ChildWorkflowID: "wf-1"},
		},
	}
	got := forcedDirtyParentNodeIDs(wf, nil)
	if len(got) != 0 {
		t.Fatalf("expected empty for nil changedWorkflowIDs, got %v", got)
	}
}

func TestBenchmarkRunnerForcedDirtyParentNodeIDsDeduplicates(t *testing.T) {
	wf := &workflow.Workflow{
		Nodes: []*workflow.Node{
			{ID: "child-a", Type: workflow.NodeTypeChildWorkflow, ChildWorkflowID: "wf-1"},
			{ID: "child-a", Type: workflow.NodeTypeChildWorkflow, ChildWorkflowID: "wf-1"}, // duplicate
			{ID: "child-b", Type: workflow.NodeTypeChildWorkflow, ChildWorkflowID: "wf-1"},
		},
	}
	changed := map[string]struct{}{"wf-1": {}}
	got := forcedDirtyParentNodeIDs(wf, changed)
	if len(got) != 2 {
		t.Fatalf("expected 2 unique node IDs, got %v", got)
	}
}

func TestBenchmarkRunnerForcedDirtyParentNodeIDsSkipsNonChildNodes(t *testing.T) {
	wf := &workflow.Workflow{
		Nodes: []*workflow.Node{
			{ID: "prompt-a", Type: workflow.NodeTypePrompt},
			{ID: "result", Type: workflow.NodeTypeResult},
			nil, // nil node should be handled
		},
	}
	changed := map[string]struct{}{"wf-1": {}}
	got := forcedDirtyParentNodeIDs(wf, changed)
	if len(got) != 0 {
		t.Fatalf("expected empty for non-child-workflow nodes, got %v", got)
	}
}

func TestBenchmarkRunnerForcedDirtyParentNodeIDsSorted(t *testing.T) {
	wf := &workflow.Workflow{
		Nodes: []*workflow.Node{
			{ID: "z-child", Type: workflow.NodeTypeChildWorkflow, ChildWorkflowID: "wf-1"},
			{ID: "a-child", Type: workflow.NodeTypeChildWorkflow, ChildWorkflowID: "wf-1"},
			{ID: "m-child", Type: workflow.NodeTypeChildWorkflow, ChildWorkflowID: "wf-1"},
		},
	}
	changed := map[string]struct{}{"wf-1": {}}
	got := forcedDirtyParentNodeIDs(wf, changed)
	if len(got) != 3 {
		t.Fatalf("expected 3 node IDs, got %v", got)
	}
	if got[0] != "a-child" || got[1] != "m-child" || got[2] != "z-child" {
		t.Fatalf("expected sorted [a-child, m-child, z-child], got %v", got)
	}
}

// --- benchmarkRunnerState JSON ---

func TestBenchmarkRunnerStateCancelRequested(t *testing.T) {
	state := &benchmarkRunnerState{
		Running:         true,
		CancelRequested: true,
	}
	if !state.CancelRequested {
		t.Fatal("expected CancelRequested to be true")
	}
}

// --- buildBenchmarkTasks ---

func TestBenchmarkRunnerBuildBenchmarkTasksCount(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	plans := []benchmarkRunPlan{
		{RunID: "r1"},
		{RunID: "r2"},
	}
	workItems := []benchmarkWorkItem{
		{RunIndex: 0, ItemIndex: 0},
		{RunIndex: 1, ItemIndex: 0},
		{RunIndex: 0, ItemIndex: 1},
	}

	cfg := benchmarkRunRequest{Concurrency: 2}
	guard := &benchmarkFatalGuard{
		signatureCounts: make(map[string]int),
		repeatThreshold: 3,
	}

	tasks := server.buildBenchmarkTasks(context.Background(), cfg, guard, plans, workItems)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
}
