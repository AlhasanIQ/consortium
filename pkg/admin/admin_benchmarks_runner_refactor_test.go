package admin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/bench"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	workflowruntime "github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

func TestPreflightValidateBenchmarkWorkflowFailsEarlyOnInvalidAggregationConfig(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	wf := &workflow.Workflow{
		ID:   "wf-invalid-synthesis-preflight",
		Name: "Invalid Synthesis Preflight",
		Nodes: []*workflow.Node{
			{
				ID:             "agent-a",
				Type:           workflow.NodeTypePrompt,
				Model:          "mock-model",
				Prompt:         "Solve {{user_prompt}}",
				Temperature:    float64Ptr(0.0),
				MaxTokens:      256,
				TimeoutSeconds: 30,
				RetryPolicy:    workflow.DefaultRetryPolicy(),
			},
			{
				ID:                "result",
				Type:              workflow.NodeTypeResult,
				AggregationMethod: workflow.AggMethodSynthesis,
				AggregationConfig: map[string]interface{}{
					"model":         "mock-model",
					"system_prompt": "Synthesize answers.",
					"temperature":   0.0,
					"max_tokens":    -1,
				},
				RetryPolicy: workflow.NoRetryPolicy(),
				Metadata: map[string]interface{}{
					"input_ids": []interface{}{"agent-a"},
				},
			},
		},
		Edges: []*workflow.Edge{
			{ID: "edge-agent-result", Source: "agent-a", Target: "result"},
		},
	}

	raw, err := json.Marshal(wf)
	if err != nil {
		t.Fatalf("marshal workflow: %v", err)
	}

	sample := bench.DatasetItem{
		ID:          "item-1",
		Benchmark:   bench.BenchmarkGlobalMMLULite,
		Question:    "What is 2 + 2?",
		Choices:     []string{"3", "4", "5", "6"},
		AnswerLabel: "B",
	}

	err = server.preflightValidateBenchmarkWorkflow(&storage.WorkflowDefinition{
		ID:         wf.ID,
		Name:       wf.Name,
		Definition: string(raw),
	}, sample)
	if err == nil {
		t.Fatal("expected preflight DAG validation failure")
	}
	if !strings.Contains(err.Error(), "workflow DAG validation failed") {
		t.Fatalf("expected DAG validation failure, got: %v", err)
	}
	if !strings.Contains(err.Error(), "synthesis requires prompt") {
		t.Fatalf("expected synthesis prompt validation error, got: %v", err)
	}
}

func float64Ptr(v float64) *float64 {
	return &v
}

func TestDetermineBenchmarkRunOutcome(t *testing.T) {
	pausedItem := bench.ItemResult{FailureReason: bench.FailureReasonBenchmarkPaused}
	pausedPlans := []benchmarkRunPlan{{ItemResults: []bench.ItemResult{pausedItem}}}
	activePlans := []benchmarkRunPlan{{ItemResults: []bench.ItemResult{{FailureReason: bench.FailureReasonProviderFailure}}}}

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	triggeredGuard := &benchmarkFatalGuard{repeatThreshold: 3}
	triggeredGuard.triggered.Store(true)

	tests := []struct {
		name      string
		ctx       context.Context
		guard     *benchmarkFatalGuard
		plans     []benchmarkRunPlan
		wantState benchmarkRunOutcome
	}{
		{
			name:      "completed when no cancellation and no fatal guard",
			ctx:       context.Background(),
			plans:     activePlans,
			wantState: benchmarkRunOutcome{Status: "completed"},
		},
		{
			name:      "cancelled when context canceled and paused items exist",
			ctx:       cancelledCtx,
			plans:     pausedPlans,
			wantState: benchmarkRunOutcome{Status: "cancelled", CancelEffective: true},
		},
		{
			name:      "completed when context canceled but no paused items",
			ctx:       cancelledCtx,
			plans:     activePlans,
			wantState: benchmarkRunOutcome{Status: "completed"},
		},
		{
			name:      "failed when fatal guard triggered and paused items exist",
			ctx:       context.Background(),
			guard:     triggeredGuard,
			plans:     pausedPlans,
			wantState: benchmarkRunOutcome{Status: "failed", FatalGuardTriggered: true, CancelEffective: true},
		},
		{
			name:      "completed when fatal guard triggered but no paused items",
			ctx:       context.Background(),
			guard:     triggeredGuard,
			plans:     activePlans,
			wantState: benchmarkRunOutcome{Status: "completed", FatalGuardTriggered: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := determineBenchmarkRunOutcome(tt.ctx, tt.guard, tt.plans)
			if got != tt.wantState {
				t.Fatalf("determineBenchmarkRunOutcome() = %#v, want %#v", got, tt.wantState)
			}
		})
	}
}

func TestBenchmarkSessionStatus(t *testing.T) {
	tests := []struct {
		name            string
		runErr          error
		fatalTriggered  bool
		cancelEffective bool
		wantStatus      string
	}{
		{name: "run error overrides everything", runErr: errors.New("save failed"), fatalTriggered: true, cancelEffective: true, wantStatus: "failed"},
		{name: "fatal trigger maps to failed", fatalTriggered: true, cancelEffective: true, wantStatus: "failed"},
		{name: "cancel maps to cancelled", cancelEffective: true, wantStatus: "cancelled"},
		{name: "default completed", wantStatus: "completed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := benchmarkSessionStatus(tt.runErr, tt.fatalTriggered, tt.cancelEffective); got != tt.wantStatus {
				t.Fatalf("benchmarkSessionStatus() = %q, want %q", got, tt.wantStatus)
			}
		})
	}
}

func TestBuildInterleavedBenchmarkWorkRoundRobin(t *testing.T) {
	plans := []benchmarkRunPlan{
		{RunID: "r1", Items: make([]bench.DatasetItem, 2)},
		{RunID: "r2", Items: make([]bench.DatasetItem, 3)},
		{RunID: "r3", Items: make([]bench.DatasetItem, 1)},
	}
	got := buildInterleavedBenchmarkWork(plans)

	want := []benchmarkWorkItem{
		{RunIndex: 0, ItemIndex: 0},
		{RunIndex: 1, ItemIndex: 0},
		{RunIndex: 2, ItemIndex: 0},
		{RunIndex: 0, ItemIndex: 1},
		{RunIndex: 1, ItemIndex: 1},
		{RunIndex: 1, ItemIndex: 2},
	}

	if len(got) != len(want) {
		t.Fatalf("buildInterleavedBenchmarkWork() len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("buildInterleavedBenchmarkWork()[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestHasPausedBenchmarkItems(t *testing.T) {
	plans := []benchmarkRunPlan{
		{RunID: "run-a", ItemResults: []bench.ItemResult{{FailureReason: bench.FailureReasonProviderFailure}}},
		{RunID: "run-b", ItemResults: []bench.ItemResult{{FailureReason: bench.FailureReasonBenchmarkPaused}}},
	}
	if !hasPausedBenchmarkItems(plans) {
		t.Fatal("expected paused benchmark items to be detected")
	}

	if hasPausedBenchmarkItems([]benchmarkRunPlan{{RunID: "run-c", ItemResults: nil}}) {
		t.Fatal("did not expect paused benchmark items")
	}
}

func TestPausedBenchmarkItemResultMessageDefault(t *testing.T) {
	item := bench.DatasetItem{ID: "q1", AnswerLabel: "A", Subject: "math", Language: "en"}
	result := pausedBenchmarkItemResult("mmlu", "wf-1", item, "  ")
	if result.FailureReason != bench.FailureReasonBenchmarkPaused {
		t.Fatalf("FailureReason = %q, want %q", result.FailureReason, bench.FailureReasonBenchmarkPaused)
	}
	if result.Error != "benchmark paused" {
		t.Fatalf("Error = %q, want %q", result.Error, "benchmark paused")
	}
	if result.Attempts != 1 {
		t.Fatalf("Attempts = %d, want 1", result.Attempts)
	}
}

func TestPausedBenchmarkItemResultPreservesIdentifiers(t *testing.T) {
	item := bench.DatasetItem{ID: "q9", AnswerLabel: "C", Subject: "physics", Language: "english"}
	result := pausedBenchmarkItemResult("mmlu", "workflow-x", item, "cancelled")
	if result.ItemID != item.ID {
		t.Fatalf("ItemID = %q, want %q", result.ItemID, item.ID)
	}
	if result.WorkflowID != "workflow-x" {
		t.Fatalf("WorkflowID = %q, want workflow-x", result.WorkflowID)
	}
	if result.BenchmarkName != "mmlu" {
		t.Fatalf("BenchmarkName = %q, want mmlu", result.BenchmarkName)
	}
}

func TestBuildInterleavedBenchmarkWorkEmpty(t *testing.T) {
	if got := buildInterleavedBenchmarkWork(nil); len(got) != 0 {
		t.Fatalf("expected empty work for nil plans, got %d", len(got))
	}
	if got := buildInterleavedBenchmarkWork([]benchmarkRunPlan{{Items: nil}}); len(got) != 0 {
		t.Fatalf("expected empty work for empty plan items, got %d", len(got))
	}
}

func TestDetermineBenchmarkRunOutcomeCanceledContextDeadline(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
	defer cancel()
	pausedPlans := []benchmarkRunPlan{{ItemResults: []bench.ItemResult{{FailureReason: bench.FailureReasonBenchmarkPaused}}}}

	got := determineBenchmarkRunOutcome(ctx, nil, pausedPlans)
	if got.Status != "cancelled" || !got.CancelEffective {
		t.Fatalf("outcome = %#v, want cancelled with CancelEffective", got)
	}
}

func TestPausedBenchmarkItemResultIfCancelledHandlesNilGuard(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	run := &benchmarkRunPlan{Benchmark: "mmlu", WorkflowID: "wf-1"}
	item := bench.DatasetItem{ID: "q1", AnswerLabel: "A", Subject: "math", Language: "en"}

	result, cancelled := pausedBenchmarkItemResultIfCancelled(ctx, nil, run, item)
	if !cancelled {
		t.Fatal("expected cancelled=true")
	}
	if result.FailureReason != bench.FailureReasonBenchmarkPaused {
		t.Fatalf("FailureReason = %q, want %q", result.FailureReason, bench.FailureReasonBenchmarkPaused)
	}
	if result.Error != "benchmark cancelled" {
		t.Fatalf("Error = %q, want %q", result.Error, "benchmark cancelled")
	}
}

func TestFinalizeBenchmarkAttemptErrorResult(t *testing.T) {
	run := &benchmarkRunPlan{Benchmark: "mmlu", WorkflowID: "wf-2"}
	item := bench.DatasetItem{ID: "q2", AnswerLabel: "B", Subject: "history", Language: "en"}

	result := newBenchmarkItemResult(run, item)
	result = finalizeBenchmarkAttemptErrorResult(result, item, 2, "boom")

	if result.Attempts != 1 {
		t.Fatalf("Attempts = %d, want 1", result.Attempts)
	}
	if len(result.AttemptDetails) != 1 || result.AttemptDetails[0].Attempt != 2 {
		t.Fatalf("AttemptDetails = %#v, want one attempt #2", result.AttemptDetails)
	}
	if result.Error != "boom" {
		t.Fatalf("Error = %q, want %q", result.Error, "boom")
	}
}

func TestFinalizeBenchmarkItemFromAttemptMath500(t *testing.T) {
	item := bench.DatasetItem{
		ID:          "m1",
		Benchmark:   bench.BenchmarkMath500,
		GoldAnswer:  `\frac{14}{3}`,
		AnswerLabel: `\frac{14}{3}`,
	}
	result := bench.ItemResult{
		AttemptDetails: []bench.AttemptDetail{{
			Attempt:   1,
			ParseOK:   true,
			Predicted: "14/3",
		}},
	}
	attempt := result.AttemptDetails[0]
	finalizeBenchmarkItemFromAttempt(&result, item, attempt)

	if !result.Correct {
		t.Fatalf("result.Correct = %v, want true", result.Correct)
	}
	if len(result.AttemptDetails) != 1 || !result.AttemptDetails[0].Correct {
		t.Fatalf("attempt correct not propagated: %#v", result.AttemptDetails)
	}
}

func TestAccumulateBenchmarkAttemptMetrics(t *testing.T) {
	result := &bench.ItemResult{LatencyMS: 10, TokensInput: 1, TokensOutput: 2, TotalTokens: 3, CostUSD: 0.1}
	attempt := &bench.AttemptDetail{}
	wfResult := &workflow.WorkflowResult{
		TotalLatency:      20,
		TotalInputTokens:  5,
		TotalOutputTokens: 7,
		TotalTokens:       12,
		TotalCost:         0.25,
	}

	accumulateBenchmarkAttemptMetrics(result, attempt, wfResult)

	if attempt.TotalTokens != 12 || attempt.CostUSD != 0.25 {
		t.Fatalf("attempt metrics not populated: %+v", attempt)
	}
	if result.LatencyMS != 30 || result.TokensInput != 6 || result.TokensOutput != 9 || result.TotalTokens != 15 {
		t.Fatalf("result totals not accumulated: %+v", result)
	}
	if result.CostUSD < 0.3499 || result.CostUSD > 0.3501 {
		t.Fatalf("result cost not accumulated: %f", result.CostUSD)
	}
}

func TestForcedDirtyParentNodeIDs(t *testing.T) {
	wf := &workflow.Workflow{
		Nodes: []*workflow.Node{
			{ID: "child-a", Type: workflow.NodeTypeChildWorkflow, ChildWorkflowID: "reasoning-judge-pick-cheap"},
			{ID: "child-b", Type: workflow.NodeTypeChildWorkflow, ChildWorkflowID: "reasoning-informed-captain-synthesis-cheap"},
			{ID: "contract", Type: workflow.NodeTypePrompt},
		},
	}

	changed := map[string]struct{}{
		"reasoning-judge-pick-cheap": {},
	}
	got := forcedDirtyParentNodeIDs(wf, changed)
	if len(got) != 1 || got[0] != "child-a" {
		t.Fatalf("forcedDirtyParentNodeIDs() = %v, want [child-a]", got)
	}
}

func TestLatestBenchmarkAttemptJobIDPrefersLastAttempt(t *testing.T) {
	detail := &storage.BenchmarkRunItemDetail{
		Item: storage.BenchmarkRunItem{JobID: "top-level-job"},
		Attempts: []storage.BenchmarkRunItemAttempt{
			{Attempt: 1, JobID: "job-1"},
			{Attempt: 2, JobID: "job-2"},
		},
	}
	if got := latestBenchmarkAttemptJobID(detail); got != "job-2" {
		t.Fatalf("latestBenchmarkAttemptJobID() = %q, want %q", got, "job-2")
	}
}

func TestParseAnswerFormatDispatch(t *testing.T) {
	tests := []struct {
		name   string
		format bench.BenchmarkFormat
		raw    string
		want   string
		wantOK bool
	}{
		{"mcqa letter", bench.BenchmarkFormatMCQA, "The answer is A", "A", true},
		{"mcqa no letter", bench.BenchmarkFormatMCQA, `\frac{14}{3}`, "", false},
		{"math expression", bench.BenchmarkFormatMathAnswer, "Final answer: \\frac{14}{3}", `\frac{14}{3}`, true},
		{"math short", bench.BenchmarkFormatMathAnswer, "42", "42", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseAnswer(tt.format, tt.raw)
			if ok != tt.wantOK {
				t.Fatalf("parseAnswer ok=%v, want %v (got=%q)", ok, tt.wantOK, got)
			}
			if tt.wantOK && got != tt.want {
				t.Fatalf("parseAnswer=%q, want %q", got, tt.want)
			}
		})
	}
}

func TestAnswersMatchFormatDispatch(t *testing.T) {
	tests := []struct {
		name      string
		format    bench.BenchmarkFormat
		predicted string
		gold      string
		want      bool
	}{
		{"mcqa equal", bench.BenchmarkFormatMCQA, "A", "A", true},
		{"mcqa case insensitive", bench.BenchmarkFormatMCQA, "a", "A", true},
		{"mcqa different", bench.BenchmarkFormatMCQA, "A", "B", false},
		{"math equivalent fractions", bench.BenchmarkFormatMathAnswer, `\frac{14}{3}`, "14/3", true},
		{"math different values", bench.BenchmarkFormatMathAnswer, "5", "3", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := answersMatch(tt.format, tt.predicted, tt.gold); got != tt.want {
				t.Fatalf("answersMatch=%v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractChildWorkflowAnswersMathFormat(t *testing.T) {
	nodes := []storage.WorkflowNode{
		{NodeID: "agent-a", NodeType: "prompt", Model: "model-a", Output: "Final answer: \\frac{14}{3}"},
		{NodeID: "agent-b", NodeType: "prompt", Model: "model-b", Output: "Final answer: 5"},
		{NodeID: "result", NodeType: "result", Output: "Final answer: \\frac{14}{3}"},
	}
	goldAnswer := `\frac{14}{3}`

	agents, childResult, childParseOK := extractChildWorkflowAnswers(bench.BenchmarkFormatMathAnswer, nodes, goldAnswer)

	if len(agents) != 2 {
		t.Fatalf("expected 2 agent answers, got %d", len(agents))
	}
	if !agents[0].ParseOK || !agents[0].Correct {
		t.Fatalf("agent-a should be parsed and correct: %+v", agents[0])
	}
	if !agents[1].ParseOK || agents[1].Correct {
		t.Fatalf("agent-b should be parsed but incorrect: %+v", agents[1])
	}
	if !childParseOK {
		t.Fatalf("child result should parse, got parseOK=%v result=%q", childParseOK, childResult)
	}
}

func TestClassifyWrongAnswerMathFormat(t *testing.T) {
	// Child returned correct answer but parent (benchmark wrapper) got wrong -> categoryChildRightParentWrong
	agents := []agentNodeAnswer{
		{NodeID: "a", Answer: `\frac{14}{3}`, Correct: true, ParseOK: true},
	}
	cat := classifyWrongAnswer(bench.BenchmarkFormatMathAnswer, `\frac{14}{3}`, `\frac{14}{3}`, true, agents)
	if cat != categoryChildRightParentWrong {
		t.Fatalf("expected categoryChildRightParentWrong, got %s", cat)
	}

	// All agents wrong -> categoryAllStepsWrong
	wrongAgents := []agentNodeAnswer{
		{NodeID: "a", Answer: "5", Correct: false, ParseOK: true},
	}
	cat = classifyWrongAnswer(bench.BenchmarkFormatMathAnswer, `\frac{14}{3}`, "5", true, wrongAgents)
	if cat != categoryAllStepsWrong {
		t.Fatalf("expected categoryAllStepsWrong, got %s", cat)
	}
}

func TestBenchmarkPerformanceCollectorMathFormat(t *testing.T) {
	collector := newBenchmarkPerformanceCollector(bench.BenchmarkFormatMathAnswer, 1)

	nodes := []storage.WorkflowNode{
		{NodeID: "agent-a", NodeType: "prompt", Model: "model-a", Output: "Final answer: \\frac{14}{3}", LatencyMs: 10, Cost: 0.10},
		{NodeID: "result", NodeType: "result", Output: "Final answer: \\frac{14}{3}", LatencyMs: 5},
	}
	attempts := []storage.NodeExecutionAttempt{
		{NodeID: "agent-a", Attempt: 1, LatencyMs: 10, Cost: 0.10},
		{NodeID: "result", Attempt: 1, LatencyMs: 5, Cost: 0.00},
	}

	collector.ingestItem(`\frac{14}{3}`, nodes, attempts, true)

	view := collector.toView()
	if len(view.AgentModels) != 1 {
		t.Fatalf("expected 1 agent model, got %d", len(view.AgentModels))
	}
	if view.AgentModels[0].Correct != 1 {
		t.Fatalf("expected agent to be correct for math answer, got correct=%d", view.AgentModels[0].Correct)
	}
	if view.AgentModels[0].ParseOK != 1 {
		t.Fatalf("expected agent parse to succeed for math answer, got parseOK=%d", view.AgentModels[0].ParseOK)
	}
}

func TestParseReplayModeForm(t *testing.T) {
	tests := []struct {
		in      string
		want    workflowruntime.ReplayMode
		wantErr bool
	}{
		{in: "", want: workflowruntime.ReplayModeBestEffort},
		{in: "best_effort", want: workflowruntime.ReplayModeBestEffort},
		{in: "required", want: workflowruntime.ReplayModeRequired},
		{in: "off", want: workflowruntime.ReplayModeOff},
		{in: "invalid", wantErr: true},
	}

	for _, tt := range tests {
		got, err := parseReplayModeForm(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("parseReplayModeForm(%q) expected error", tt.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseReplayModeForm(%q) returned error: %v", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("parseReplayModeForm(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
