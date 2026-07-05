package admin

import (
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/bench"
	"github.com/alhasaniq/consortium/pkg/storage"
)

func TestClassifyBenchmarkFatalCause(t *testing.T) {
	tests := []struct {
		name          string
		code          string
		failure       string
		errMsg        string
		wantCandidate bool
		wantHard      bool
		wantReason    string
	}{
		{
			name:          "admission paused code is hard fatal",
			code:          "ADMISSION_PAUSED",
			failure:       bench.FailureReasonToolRuntimeFailure,
			errMsg:        "Admission paused by operator request",
			wantCandidate: true,
			wantHard:      true,
			wantReason:    "admission_paused",
		},
		{
			name:          "insufficient credits code is hard fatal",
			code:          "INSUFFICIENT_CREDITS",
			failure:       bench.FailureReasonProviderFailure,
			errMsg:        "payment required",
			wantCandidate: true,
			wantHard:      true,
			wantReason:    "insufficient_credits",
		},
		{
			name:          "auth error code is hard fatal",
			code:          "AUTH_ERROR",
			failure:       bench.FailureReasonProviderFailure,
			errMsg:        "unauthorized",
			wantCandidate: true,
			wantHard:      true,
			wantReason:    "auth_or_access_denied",
		},
		{
			name:          "model not found code is hard fatal",
			code:          "MODEL_NOT_FOUND",
			failure:       bench.FailureReasonProviderFailure,
			errMsg:        "model not found",
			wantCandidate: true,
			wantHard:      true,
			wantReason:    "model_not_found",
		},
		{
			name:          "bad request code is hard fatal",
			code:          "BAD_REQUEST",
			failure:       bench.FailureReasonToolRuntimeFailure,
			errMsg:        "invalid config",
			wantCandidate: true,
			wantHard:      true,
			wantReason:    "invalid_request",
		},
		{
			name:          "cost limit code is hard fatal",
			code:          "COST_LIMIT",
			failure:       bench.FailureReasonProviderFailure,
			errMsg:        "cost limit exceeded",
			wantCandidate: true,
			wantHard:      true,
			wantReason:    "cost_limit_exceeded",
		},
		{
			name:          "auth marker in message is hard fatal even without code",
			code:          "",
			failure:       bench.FailureReasonProviderFailure,
			errMsg:        "OpenRouter authentication failed: invalid api key",
			wantCandidate: true,
			wantHard:      true,
			wantReason:    "auth_or_access_denied",
		},
		{
			name:          "region blocked message is hard fatal",
			code:          "",
			failure:       bench.FailureReasonProviderFailure,
			errMsg:        "Provider returned 403: access denied from your country, region, or territory",
			wantCandidate: true,
			wantHard:      true,
			wantReason:    "auth_or_access_denied",
		},
		{
			name:          "insufficient credits in message without code",
			code:          "",
			failure:       bench.FailureReasonProviderFailure,
			errMsg:        "payment required: insufficient credits",
			wantCandidate: true,
			wantHard:      true,
			wantReason:    "insufficient_credits",
		},
		{
			name:          "cost limit in message without code",
			code:          "",
			failure:       bench.FailureReasonProviderFailure,
			errMsg:        "cost limit exceeded for this workflow",
			wantCandidate: true,
			wantHard:      true,
			wantReason:    "cost_limit_exceeded",
		},
		{
			name:          "retryable code (RATE_LIMIT) is not a fatal candidate",
			code:          "RATE_LIMIT",
			failure:       bench.FailureReasonProviderFailure,
			errMsg:        "rate limit",
			wantCandidate: false,
		},
		{
			name:          "pool exhausted is retryable, not a fatal candidate",
			code:          "POOL_EXHAUSTED",
			failure:       bench.FailureReasonToolRuntimeFailure,
			errMsg:        "admission pool exhausted",
			wantCandidate: false,
		},
		{
			name:          "non provider/runtime failure reason with unknown code is not a candidate",
			code:          "SOME_UNKNOWN",
			failure:       bench.FailureReasonInvalidContract,
			errMsg:        "invalid contract output",
			wantCandidate: false,
		},
		{
			name:          "empty code and no message match is not a candidate",
			code:          "",
			failure:       bench.FailureReasonToolRuntimeFailure,
			errMsg:        "generic error with no markers",
			wantCandidate: false,
		},
		{
			name:          "unknown non-retryable code with provider failure is soft candidate",
			code:          "FOO_UNKNOWN",
			failure:       bench.FailureReasonProviderFailure,
			errMsg:        "foo unknown deterministic",
			wantCandidate: true,
			wantHard:      false,
			wantReason:    "repeated_non_retryable_signature",
		},
		{
			name:          "unknown non-retryable code with runtime failure is soft candidate",
			code:          "FOO_UNKNOWN",
			failure:       bench.FailureReasonToolRuntimeFailure,
			errMsg:        "foo unknown deterministic",
			wantCandidate: true,
			wantHard:      false,
			wantReason:    "repeated_non_retryable_signature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := classifyBenchmarkFatalCause(tt.code, tt.failure, tt.errMsg)
			if ok != tt.wantCandidate {
				t.Fatalf("expected candidate=%v, got %v", tt.wantCandidate, ok)
			}
			if !ok {
				return
			}
			if got.Hard != tt.wantHard {
				t.Fatalf("expected hard=%v, got %v", tt.wantHard, got.Hard)
			}
			if got.Reason != tt.wantReason {
				t.Fatalf("expected reason=%q, got %q", tt.wantReason, got.Reason)
			}
			if got.Code == "" {
				t.Fatal("expected non-empty code")
			}
			if got.Signature == "" {
				t.Fatal("expected non-empty signature")
			}
		})
	}
}

func TestBenchmarkFatalGuard_ThresholdTrigger(t *testing.T) {
	guard := &benchmarkFatalGuard{
		signatureCounts: make(map[string]int),
		repeatThreshold: 3,
	}

	cause := benchmarkFatalCause{
		Code:      "FOO_UNKNOWN",
		Message:   "foo unknown deterministic",
		Reason:    "repeated_non_retryable_signature",
		Signature: benchmarkFailureSignature("FOO_UNKNOWN", "foo unknown deterministic"),
		Hard:      false,
	}

	// First two notes should not trigger.
	if triggered, count := guard.note(cause); triggered || count != 1 {
		t.Fatalf("first note: expected triggered=false count=1, got triggered=%v count=%d", triggered, count)
	}
	if guard.isTriggered() {
		t.Fatal("guard should not be triggered after first note")
	}

	if triggered, count := guard.note(cause); triggered || count != 2 {
		t.Fatalf("second note: expected triggered=false count=2, got triggered=%v count=%d", triggered, count)
	}
	if guard.isTriggered() {
		t.Fatal("guard should not be triggered after second note")
	}

	// Third note hits the threshold.
	if triggered, count := guard.note(cause); !triggered || count != 3 {
		t.Fatalf("third note: expected triggered=true count=3, got triggered=%v count=%d", triggered, count)
	}
	if !guard.isTriggered() {
		t.Fatal("guard should be triggered after threshold hit")
	}

	// Subsequent notes should be no-ops.
	if triggered, _ := guard.note(cause); triggered {
		t.Fatal("expected subsequent note to be a no-op")
	}
}

func TestBenchmarkFatalGuard_HardTriggerImmediate(t *testing.T) {
	guard := &benchmarkFatalGuard{
		signatureCounts: make(map[string]int),
		repeatThreshold: 5,
	}

	cause := benchmarkFatalCause{
		Code:      "AUTH_ERROR",
		Message:   "authentication failed",
		Reason:    "auth_or_access_denied",
		Signature: "AUTH_ERROR",
		Hard:      true,
	}

	if triggered, count := guard.note(cause); !triggered || count != 1 {
		t.Fatalf("hard note: expected triggered=true count=1, got triggered=%v count=%d", triggered, count)
	}
	if !guard.isTriggered() {
		t.Fatal("guard should be triggered immediately for hard fatal")
	}
}

func TestBenchmarkFatalGuard_CancelCalledOnTrigger(t *testing.T) {
	cancelCalled := false
	guard := &benchmarkFatalGuard{
		cancel:          func() { cancelCalled = true },
		signatureCounts: make(map[string]int),
		repeatThreshold: 1,
	}

	cause := benchmarkFatalCause{
		Code:      "SOME_CODE",
		Message:   "some error",
		Reason:    "repeated_non_retryable_signature",
		Signature: "SOME_CODE|some error",
		Hard:      false,
	}

	guard.note(cause)
	if !cancelCalled {
		t.Fatal("expected cancel function to be called when guard triggers")
	}
}

func TestBenchmarkAnalysisDiagnosticsCollector(t *testing.T) {
	collector := newBenchmarkAnalysisDiagnosticsCollector(2)
	collector.add(benchmarkAnalysisItemDiagnostics{
		ItemID:        "row-1",
		TotalLatency:  1800,
		ParentRetries: 1,
		ChildRetries:  0,
		TotalRetries:  1,
	})
	collector.add(benchmarkAnalysisItemDiagnostics{
		ItemID:        "row-2",
		TotalLatency:  900,
		ParentRetries: 3,
		ChildRetries:  1,
		TotalRetries:  4,
	})
	collector.add(benchmarkAnalysisItemDiagnostics{
		ItemID:        "row-3",
		TotalLatency:  2500,
		ParentRetries: 0,
		ChildRetries:  4,
		TotalRetries:  4,
	})

	view := collector.toView()
	if view.TopN != 2 {
		t.Fatalf("expected TopN=2, got %d", view.TopN)
	}
	if len(view.SlowestItems) != 2 {
		t.Fatalf("expected 2 slowest items, got %d", len(view.SlowestItems))
	}
	if len(view.MostRetryItems) != 2 {
		t.Fatalf("expected 2 most-retries items, got %d", len(view.MostRetryItems))
	}

	if view.SlowestItems[0].ItemID != "row-3" || view.SlowestItems[1].ItemID != "row-1" {
		t.Fatalf("unexpected slowest order: %s, %s", view.SlowestItems[0].ItemID, view.SlowestItems[1].ItemID)
	}
	// row-3 and row-2 have equal total retries (4); row-3 wins tie-break on child retries.
	if view.MostRetryItems[0].ItemID != "row-3" || view.MostRetryItems[1].ItemID != "row-2" {
		t.Fatalf("unexpected most-retries order: %s, %s", view.MostRetryItems[0].ItemID, view.MostRetryItems[1].ItemID)
	}
}

func TestBenchmarkFatalGuard_SnapshotMessage(t *testing.T) {
	guard := &benchmarkFatalGuard{
		signatureCounts: make(map[string]int),
		repeatThreshold: 1,
	}

	// Before trigger, should return generic message.
	if msg := guard.snapshotMessage(); msg != "benchmark paused due to fatal execution error" {
		t.Fatalf("expected generic message before trigger, got %q", msg)
	}

	cause := benchmarkFatalCause{
		Code:      "AUTH_ERROR",
		Message:   "invalid api key",
		Reason:    "auth_or_access_denied",
		Signature: "AUTH_ERROR",
		Hard:      true,
	}
	guard.note(cause)

	msg := guard.snapshotMessage()
	if msg == "" {
		t.Fatal("expected non-empty snapshot message after trigger")
	}
	if !contains(msg, "AUTH_ERROR") || !contains(msg, "auth_or_access_denied") {
		t.Fatalf("expected snapshot message to include code and reason, got %q", msg)
	}
}

func TestBenchmarkFatalGuard_NilSafe(t *testing.T) {
	var guard *benchmarkFatalGuard

	if guard.isTriggered() {
		t.Fatal("nil guard should not be triggered")
	}
	if msg := guard.snapshotMessage(); msg == "" {
		t.Fatal("nil guard snapshot message should not be empty")
	}

	triggered, count := guard.note(benchmarkFatalCause{Hard: true})
	if triggered || count != 0 {
		t.Fatalf("nil guard note should be no-op, got triggered=%v count=%d", triggered, count)
	}
}

func TestBenchmarkFatalGuard_DifferentSignaturesIndependent(t *testing.T) {
	guard := &benchmarkFatalGuard{
		signatureCounts: make(map[string]int),
		repeatThreshold: 3,
	}

	causeA := benchmarkFatalCause{
		Code: "ERR_A", Message: "error a", Reason: "repeated_non_retryable_signature",
		Signature: "ERR_A|error a", Hard: false,
	}
	causeB := benchmarkFatalCause{
		Code: "ERR_B", Message: "error b", Reason: "repeated_non_retryable_signature",
		Signature: "ERR_B|error b", Hard: false,
	}

	// Interleave two signatures — neither should trigger at threshold 3.
	guard.note(causeA) // A=1
	guard.note(causeB) // B=1
	guard.note(causeA) // A=2
	guard.note(causeB) // B=2

	if guard.isTriggered() {
		t.Fatal("guard should not be triggered with interleaved signatures below threshold")
	}

	// Third occurrence of A triggers.
	if triggered, _ := guard.note(causeA); !triggered {
		t.Fatal("expected guard to trigger on third occurrence of signature A")
	}
}

func TestBenchmarkFailureSignature(t *testing.T) {
	tests := []struct {
		name    string
		code    string
		errMsg  string
		wantSig string
	}{
		{
			name:    "normal",
			code:    "AUTH_ERROR",
			errMsg:  "Invalid API key",
			wantSig: "AUTH_ERROR|invalid api key",
		},
		{
			name:    "empty code",
			code:    "",
			errMsg:  "some error",
			wantSig: "UNKNOWN|some error",
		},
		{
			name:    "empty message",
			code:    "SOME_CODE",
			errMsg:  "",
			wantSig: "SOME_CODE|no_message",
		},
		{
			name:    "both empty",
			code:    "",
			errMsg:  "",
			wantSig: "UNKNOWN|no_message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := benchmarkFailureSignature(tt.code, tt.errMsg)
			if got != tt.wantSig {
				t.Fatalf("expected %q, got %q", tt.wantSig, got)
			}
		})
	}
}

func TestHardFatalReasonForCode(t *testing.T) {
	cases := []struct {
		code       string
		wantReason string
		wantOK     bool
	}{
		{"ADMISSION_PAUSED", "admission_paused", true},
		{"INSUFFICIENT_CREDITS", "insufficient_credits", true},
		{"AUTH_ERROR", "auth_or_access_denied", true},
		{"AUTHENTICATION", "auth_or_access_denied", true},
		{"AUTHORIZATION", "auth_or_access_denied", true},
		{"MODEL_NOT_FOUND", "model_not_found", true},
		{"BAD_REQUEST", "invalid_request", true},
		{"INVALID_WORKFLOW", "invalid_request", true},
		{"INVALID_CONFIG", "invalid_request", true},
		{"COST_LIMIT", "cost_limit_exceeded", true},
		{"RATE_LIMIT", "", false},
		{"POOL_EXHAUSTED", "", false},
		{"", "", false},
	}

	for _, tc := range cases {
		reason, ok := hardFatalReasonForCode(tc.code)
		if ok != tc.wantOK {
			t.Fatalf("code=%q: expected ok=%v, got %v", tc.code, tc.wantOK, ok)
		}
		if reason != tc.wantReason {
			t.Fatalf("code=%q: expected reason=%q, got %q", tc.code, tc.wantReason, reason)
		}
	}
}

func TestHardFatalReasonForMessage(t *testing.T) {
	cases := []struct {
		msg        string
		wantCode   string
		wantReason string
		wantOK     bool
	}{
		{"admission paused", "ADMISSION_PAUSED", "admission_paused", true},
		{"server admission paused", "ADMISSION_PAUSED", "admission_paused", true},
		{"payment required", "INSUFFICIENT_CREDITS", "insufficient_credits", true},
		{"quota exhausted", "INSUFFICIENT_CREDITS", "insufficient_credits", true},
		{"invalid api key", "AUTH_ERROR", "auth_or_access_denied", true},
		{"forbidden", "AUTH_ERROR", "auth_or_access_denied", true},
		{"account suspended", "AUTH_ERROR", "auth_or_access_denied", true},
		{"unsupported country", "AUTH_ERROR", "auth_or_access_denied", true},
		{"model not found", "MODEL_NOT_FOUND", "model_not_found", true},
		{"bad request", "BAD_REQUEST", "invalid_request", true},
		{"cost limit hit", "COST_LIMIT", "cost_limit_exceeded", true},
		{"generic error without markers", "", "", false},
		{"", "", "", false},
	}

	for _, tc := range cases {
		code, reason, ok := hardFatalReasonForMessage(tc.msg)
		if ok != tc.wantOK {
			t.Fatalf("msg=%q: expected ok=%v, got %v", tc.msg, tc.wantOK, ok)
		}
		if code != tc.wantCode {
			t.Fatalf("msg=%q: expected code=%q, got %q", tc.msg, tc.wantCode, code)
		}
		if reason != tc.wantReason {
			t.Fatalf("msg=%q: expected reason=%q, got %q", tc.msg, tc.wantReason, reason)
		}
	}
}

func TestIsRetryableExecutionCode(t *testing.T) {
	cases := []struct {
		code string
		want bool
	}{
		{"POOL_EXHAUSTED", true},
		{"RATE_LIMIT", true},
		{"TIMEOUT", true},
		{"AUTH_ERROR", false},
		{"ADMISSION_PAUSED", false},
		{"COST_LIMIT", false},
		{"", false},
	}

	for _, tc := range cases {
		if got := isRetryableExecutionCode(tc.code); got != tc.want {
			t.Fatalf("code=%q: expected %v, got %v", tc.code, tc.want, got)
		}
	}
}

func TestAdmissionRetryDelay(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 0, want: 250 * time.Millisecond},
		{attempt: 1, want: 1 * time.Second},
		{attempt: 2, want: 4 * time.Second},
		{attempt: 3, want: 16 * time.Second},
		{attempt: 4, want: 30 * time.Second}, // capped
		{attempt: 10, want: 30 * time.Second},
	}

	for _, tt := range tests {
		got := admissionRetryDelay(tt.attempt)
		if got != tt.want {
			t.Fatalf("attempt %d: expected %v, got %v", tt.attempt, tt.want, got)
		}
	}
}

func TestTransientRetryDelay(t *testing.T) {
	tests := []struct {
		retry int
		want  time.Duration
	}{
		{retry: 1, want: 400 * time.Millisecond},
		{retry: 2, want: 800 * time.Millisecond},
		{retry: 3, want: 1600 * time.Millisecond},
		{retry: 4, want: 3 * time.Second}, // capped
		{retry: 10, want: 3 * time.Second},
	}

	for _, tt := range tests {
		got := transientRetryDelay(tt.retry)
		if got != tt.want {
			t.Fatalf("retry %d: expected %v, got %v", tt.retry, tt.want, got)
		}
	}
}

func TestMaybeTriggerBenchmarkFatalGuard_NilGuardSafe(t *testing.T) {
	// Should not panic with nil guard.
	maybeTriggerBenchmarkFatalGuard(
		nil,
		"global-mmlu-lite",
		"wf-test",
		"item-1",
		bench.AttemptDetail{
			Attempt:       1,
			Error:         "auth error",
			FailureReason: bench.FailureReasonProviderFailure,
		},
		nil,
	)
}

func TestMaybeTriggerBenchmarkFatalGuard_AlreadyTriggered(t *testing.T) {
	guard := &benchmarkFatalGuard{
		signatureCounts: make(map[string]int),
		repeatThreshold: 1,
	}
	// Pre-trigger the guard.
	guard.triggered.Store(true)

	// Should be a no-op — no panic, no additional cause set.
	maybeTriggerBenchmarkFatalGuard(
		guard,
		"global-mmlu-lite",
		"wf-test",
		"item-1",
		bench.AttemptDetail{
			Attempt:       1,
			Error:         "ADMISSION_PAUSED: server paused",
			FailureReason: bench.FailureReasonToolRuntimeFailure,
		},
		nil,
	)
}

func TestBenchmarkPerformanceCollectorToView(t *testing.T) {
	collector := newBenchmarkPerformanceCollector(bench.BenchmarkFormatMCQA, 2)

	item1Nodes := []storage.WorkflowNode{
		{NodeID: "agent-a", NodeType: "prompt", Model: "model-a", Output: "A", LatencyMs: 10, Cost: 0.10},
		{NodeID: "agent-b", NodeType: "prompt", Model: "model-b", Output: "B", LatencyMs: 20, Cost: 0.20},
		{NodeID: "result-judge", NodeType: "result", Output: "A", LatencyMs: 5},
		{NodeID: "result-judge__agg__judge", ParentNodeID: "result-judge", NodeType: "prompt", Model: "judge-model", LatencyMs: 7, Cost: 0.05},
	}
	item1Attempts := []storage.NodeExecutionAttempt{
		{NodeID: "agent-a", Attempt: 1, LatencyMs: 10, Cost: 0.10},
		{NodeID: "agent-b", Attempt: 1, LatencyMs: 15, Cost: 0.00},
		{NodeID: "agent-b", Attempt: 2, LatencyMs: 20, Cost: 0.20},
		{NodeID: "result-judge", Attempt: 1, LatencyMs: 5, Cost: 0.00},
		{NodeID: "result-judge__agg__judge", Attempt: 1, LatencyMs: 7, Cost: 0.05},
	}
	collector.ingestItem("A", item1Nodes, item1Attempts, true)

	item2Nodes := []storage.WorkflowNode{
		{NodeID: "agent-a", NodeType: "prompt", Model: "model-a", Output: "C", LatencyMs: 12, Cost: 0.11},
		{NodeID: "agent-b", NodeType: "prompt", Model: "model-b", Output: "B", LatencyMs: 18, Cost: 0.21},
		{NodeID: "result-judge", NodeType: "result", Output: "B", LatencyMs: 6},
		{NodeID: "result-judge__agg__judge", ParentNodeID: "result-judge", NodeType: "prompt", Model: "judge-model", LatencyMs: 8, Cost: 0.06},
	}
	item2Attempts := []storage.NodeExecutionAttempt{
		{NodeID: "agent-a", Attempt: 1, LatencyMs: 12, Cost: 0.11},
		{NodeID: "agent-b", Attempt: 1, LatencyMs: 18, Cost: 0.21},
		{NodeID: "result-judge", Attempt: 1, LatencyMs: 6, Cost: 0.00},
		{NodeID: "result-judge__agg__judge", Attempt: 1, LatencyMs: 8, Cost: 0.06},
	}
	collector.ingestItem("B", item2Nodes, item2Attempts, true)

	view := collector.toView()
	if view.TotalItems != 2 || view.ItemsWithChildData != 2 {
		t.Fatalf("unexpected coverage totals: %+v", view)
	}
	if len(view.AgentModels) != 2 {
		t.Fatalf("expected 2 agent models, got %d", len(view.AgentModels))
	}

	modelA := view.AgentModels[0]
	modelB := view.AgentModels[1]
	if modelA.Model != "model-a" || modelB.Model != "model-b" {
		t.Fatalf("unexpected model ordering/content: %+v", view.AgentModels)
	}
	if modelA.Samples != 2 || modelA.Correct != 1 || modelA.TotalRetries != 0 {
		t.Fatalf("unexpected model-a stats: %+v", modelA)
	}
	if modelA.Accuracy != 0.5 || modelA.ParseRate != 1.0 {
		t.Fatalf("unexpected model-a accuracy/parse: %+v", modelA)
	}
	if modelA.TotalLatencyMS != 22 {
		t.Fatalf("unexpected model-a cost/latency totals: %+v", modelA)
	}
	if modelA.TotalCostUSD < 0.2099 || modelA.TotalCostUSD > 0.2101 {
		t.Fatalf("unexpected model-a cost total: %+v", modelA)
	}

	if modelB.Samples != 2 || modelB.Correct != 1 {
		t.Fatalf("unexpected model-b stats: %+v", modelB)
	}
	if modelB.TotalRetries != 1 || modelB.ItemsWithRetries != 1 {
		t.Fatalf("expected model-b retries to include one extra attempt: %+v", modelB)
	}
	if modelB.TotalLatencyMS != 53 {
		t.Fatalf("expected model-b latency to include retry attempt latency, got %+v", modelB)
	}

	if len(view.AggregationNodes) != 1 {
		t.Fatalf("expected one aggregation node, got %+v", view.AggregationNodes)
	}
	agg := view.AggregationNodes[0]
	if agg.NodeID != "result-judge" {
		t.Fatalf("unexpected aggregation node id: %+v", agg)
	}
	if agg.Samples != 2 || agg.Correct != 2 || agg.Accuracy != 1.0 {
		t.Fatalf("unexpected aggregation accuracy stats: %+v", agg)
	}
	if agg.TotalLatencyMS != 26 {
		t.Fatalf("unexpected aggregation totals (must include child aggregation call node): %+v", agg)
	}
	if agg.TotalCostUSD < 0.1099 || agg.TotalCostUSD > 0.1101 {
		t.Fatalf("unexpected aggregation cost total: %+v", agg)
	}
	if len(agg.Models) != 1 || agg.Models[0] != "judge-model" {
		t.Fatalf("unexpected aggregation model set: %+v", agg.Models)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsImpl(s, substr))
}

func containsImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
