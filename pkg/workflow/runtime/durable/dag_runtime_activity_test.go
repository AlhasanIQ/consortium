package durable

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

// ---------------------------------------------------------------------------
// decodeCachedActivityOutput
// ---------------------------------------------------------------------------

func TestActivityDecodeCachedOutput_NilCached(t *testing.T) {
	out := decodeCachedActivityOutput("n1", nil)
	if out.Success {
		t.Error("expected Success=false for nil cached result")
	}
	if out.NodeID != "n1" {
		t.Errorf("NodeID = %q, want %q", out.NodeID, "n1")
	}
	if out.Error == "" {
		t.Error("expected non-empty error for nil cached result")
	}
}

func TestActivityDecodeCachedOutput_CompletedJSON(t *testing.T) {
	payload := &runtime.ActivityOutput{
		NodeID:       "n1",
		Success:      true,
		Output:       "result text",
		TokensInput:  100,
		TokensOutput: 50,
		Cost:         0.005,
	}
	data, _ := json.Marshal(payload)

	cached := &storage.ActivityResult{
		Status:        "completed",
		OutputPayload: string(data),
		NodeID:        "n1",
	}
	out := decodeCachedActivityOutput("n1", cached)
	if !out.Success {
		t.Error("expected Success=true")
	}
	if out.Output != "result text" {
		t.Errorf("Output = %q, want %q", out.Output, "result text")
	}
	if out.TokensInput != 100 {
		t.Errorf("TokensInput = %d, want 100", out.TokensInput)
	}
	if out.TokensOutput != 50 {
		t.Errorf("TokensOutput = %d, want 50", out.TokensOutput)
	}
	if out.Cost != 0.005 {
		t.Errorf("Cost = %f, want 0.005", out.Cost)
	}
}

func TestActivityDecodeCachedOutput_FailedJSON(t *testing.T) {
	payload := &runtime.ActivityOutput{
		NodeID:  "n1",
		Success: false,
		Error:   "timeout",
	}
	data, _ := json.Marshal(payload)

	cached := &storage.ActivityResult{
		Status:        "failed",
		OutputPayload: string(data),
		ErrorCode:     "TIMEOUT",
		ErrorMessage:  "timeout",
	}
	out := decodeCachedActivityOutput("n1", cached)
	if out.Success {
		t.Error("expected Success=false for failed result")
	}
	if out.ErrorCode != "TIMEOUT" {
		t.Errorf("ErrorCode = %q, want %q", out.ErrorCode, "TIMEOUT")
	}
}

func TestActivityDecodeCachedOutput_LegacyRawText(t *testing.T) {
	// Legacy rows stored raw output text, not JSON.
	cached := &storage.ActivityResult{
		Status:        "completed",
		OutputPayload: "plain text output that is not JSON",
		NodeID:        "n1",
	}
	out := decodeCachedActivityOutput("n1", cached)
	if !out.Success {
		t.Error("expected Success=true for completed status")
	}
	if out.Output != "plain text output that is not JSON" {
		t.Errorf("Output = %q, want raw text", out.Output)
	}
	if out.NodeID != "n1" {
		t.Errorf("NodeID = %q, want %q", out.NodeID, "n1")
	}
}

func TestActivityDecodeCachedOutput_EmptyPayload(t *testing.T) {
	cached := &storage.ActivityResult{
		Status:       "completed",
		ErrorMessage: "",
	}
	out := decodeCachedActivityOutput("n2", cached)
	if !out.Success {
		t.Error("expected Success=true")
	}
	if out.NodeID != "n2" {
		t.Errorf("NodeID = %q, want %q", out.NodeID, "n2")
	}
}

func TestActivityDecodeCachedOutput_ErrorFieldsFallback(t *testing.T) {
	// When the JSON payload has no error fields, they should fall back to cached fields.
	payload := &runtime.ActivityOutput{
		NodeID:  "n1",
		Success: false,
	}
	data, _ := json.Marshal(payload)

	cached := &storage.ActivityResult{
		Status:        "failed",
		OutputPayload: string(data),
		ErrorCode:     "COST_LIMIT",
		ErrorMessage:  "cost limit exceeded",
	}
	out := decodeCachedActivityOutput("n1", cached)
	if out.ErrorCode != "COST_LIMIT" {
		t.Errorf("ErrorCode = %q, want %q", out.ErrorCode, "COST_LIMIT")
	}
	if out.Error != "cost limit exceeded" {
		t.Errorf("Error = %q, want %q", out.Error, "cost limit exceeded")
	}
}

// ---------------------------------------------------------------------------
// heartbeatTimeoutFromNode
// ---------------------------------------------------------------------------

func TestActivityHeartbeatTimeoutFromNode(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]interface{}
		want     time.Duration
	}{
		{"nil node", nil, 0},
		{"no metadata key", map[string]interface{}{"other": 10}, 0},
		{"heartbeat_timeout_ms int", map[string]interface{}{"heartbeat_timeout_ms": 500}, 500 * time.Millisecond},
		{"heartbeat_timeout_ms float64", map[string]interface{}{"heartbeat_timeout_ms": 1500.0}, 1500 * time.Millisecond},
		{"heartbeat_timeout_seconds int", map[string]interface{}{"heartbeat_timeout_seconds": 30}, 30 * time.Second},
		{"heartbeat_timeout_seconds float64", map[string]interface{}{"heartbeat_timeout_seconds": 5.0}, 5 * time.Second},
		{"ms takes precedence over seconds", map[string]interface{}{
			"heartbeat_timeout_ms":      2000,
			"heartbeat_timeout_seconds": 10,
		}, 2 * time.Second},
		{"zero ms falls through to seconds", map[string]interface{}{
			"heartbeat_timeout_ms":      0,
			"heartbeat_timeout_seconds": 7,
		}, 7 * time.Second},
		{"negative ms falls through to seconds", map[string]interface{}{
			"heartbeat_timeout_ms":      -1,
			"heartbeat_timeout_seconds": 3,
		}, 3 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var node *workflow.Node
			if tt.metadata != nil {
				node = &workflow.Node{Metadata: tt.metadata}
			}
			got := heartbeatTimeoutFromNode(node)
			if got != tt.want {
				t.Errorf("heartbeatTimeoutFromNode = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// asPositiveInt
// ---------------------------------------------------------------------------

func TestActivityAsPositiveInt(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		wantVal int
		wantOK  bool
	}{
		{"int positive", 42, 42, true},
		{"int zero", 0, 0, false},
		{"int negative", -5, -5, false},
		{"int32 positive", int32(10), 10, true},
		{"int64 positive", int64(100), 100, true},
		{"float32 positive", float32(7.9), 7, true},
		{"float64 positive", 3.14, 3, true},
		{"float64 zero", 0.0, 0, false},
		{"float64 negative", -1.5, 0, false},
		{"float64 NaN", math.NaN(), 0, false},
		{"float64 +Inf", math.Inf(1), 0, false},
		{"string", "10", 0, false},
		{"nil", nil, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, ok := asPositiveInt(tt.input)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if val != tt.wantVal {
				t.Errorf("val = %d, want %d", val, tt.wantVal)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// sleepWithContext
// ---------------------------------------------------------------------------

func TestActivitySleepWithContext_ZeroDuration(t *testing.T) {
	if !sleepWithContext(t.Context(), 0) {
		t.Error("expected true for zero duration")
	}
	if !sleepWithContext(t.Context(), -1*time.Second) {
		t.Error("expected true for negative duration")
	}
}

// ---------------------------------------------------------------------------
// totalsFromHistory
// ---------------------------------------------------------------------------

func TestActivityTotalsFromHistory_WorkflowCompletedPreferred(t *testing.T) {
	events := []*runtime.HistoryEvent{
		{
			Type: runtime.HistoryActivityCompleted,
			Attributes: map[string]interface{}{
				"cost":          0.001,
				"tokens_input":  10,
				"tokens_output": 5,
			},
		},
		{
			Type: runtime.HistoryWorkflowCompleted,
			Attributes: map[string]interface{}{
				"total_cost":          0.010,
				"total_tokens":        200,
				"total_input_tokens":  120,
				"total_output_tokens": 80,
			},
		},
	}

	cost, total, input, output := totalsFromHistory(events)
	if cost != 0.010 {
		t.Errorf("cost = %f, want 0.010", cost)
	}
	if total != 200 {
		t.Errorf("total = %d, want 200", total)
	}
	if input != 120 {
		t.Errorf("input = %d, want 120", input)
	}
	if output != 80 {
		t.Errorf("output = %d, want 80", output)
	}
}

func TestActivityTotalsFromHistory_FallbackToActivitySum(t *testing.T) {
	events := []*runtime.HistoryEvent{
		{
			Type: runtime.HistoryActivityCompleted,
			Attributes: map[string]interface{}{
				"cost":          0.002,
				"tokens_input":  50,
				"tokens_output": 30,
			},
		},
		{
			Type: runtime.HistoryActivityCompleted,
			Attributes: map[string]interface{}{
				"cost":          0.003,
				"tokens_input":  40,
				"tokens_output": 20,
			},
		},
	}

	cost, total, input, output := totalsFromHistory(events)
	if cost != 0.005 {
		t.Errorf("cost = %f, want 0.005", cost)
	}
	if input != 90 {
		t.Errorf("input = %d, want 90", input)
	}
	if output != 50 {
		t.Errorf("output = %d, want 50", output)
	}
	if total != 140 {
		t.Errorf("total = %d, want 140 (input+output)", total)
	}
}

func TestActivityTotalsFromHistory_EmptyHistory(t *testing.T) {
	cost, total, input, output := totalsFromHistory(nil)
	if cost != 0 || total != 0 || input != 0 || output != 0 {
		t.Errorf("expected all zeros, got cost=%f total=%d input=%d output=%d", cost, total, input, output)
	}
}

// ---------------------------------------------------------------------------
// coalesceExecutionTotals
// ---------------------------------------------------------------------------

func TestActivityCoalesceExecutionTotals(t *testing.T) {
	tests := []struct {
		name                             string
		hCost                            float64
		hTotal, hInput, hOutput          int
		tCost                            float64
		tTotal, tInput, tOutput          int
		wantCost                         float64
		wantTotal, wantInput, wantOutput int
	}{
		{
			name:  "history empty, tracker wins",
			tCost: 0.01, tTotal: 100, tInput: 60, tOutput: 40,
			wantCost: 0.01, wantTotal: 100, wantInput: 60, wantOutput: 40,
		},
		{
			name:  "tracker empty, history wins",
			hCost: 0.02, hTotal: 200, hInput: 120, hOutput: 80,
			wantCost: 0.02, wantTotal: 200, wantInput: 120, wantOutput: 80,
		},
		{
			name:  "history dominates all dimensions",
			hCost: 0.05, hTotal: 500, hInput: 300, hOutput: 200,
			tCost: 0.03, tTotal: 300, tInput: 200, tOutput: 100,
			wantCost: 0.05, wantTotal: 500, wantInput: 300, wantOutput: 200,
		},
		{
			name:  "tracker dominates all dimensions",
			hCost: 0.01, hTotal: 100, hInput: 60, hOutput: 40,
			tCost: 0.05, tTotal: 500, tInput: 300, tOutput: 200,
			wantCost: 0.05, wantTotal: 500, wantInput: 300, wantOutput: 200,
		},
		{
			name:  "mixed dominance uses max per dimension",
			hCost: 0.03, hTotal: 200, hInput: 150, hOutput: 50,
			tCost: 0.02, tTotal: 300, tInput: 100, tOutput: 200,
			wantCost: 0.03, wantTotal: 350, wantInput: 150, wantOutput: 200, // total adjusted to input+output
		},
		{
			name:     "both empty",
			wantCost: 0, wantTotal: 0, wantInput: 0, wantOutput: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost, total, input, output := coalesceExecutionTotals(
				tt.hCost, tt.hTotal, tt.hInput, tt.hOutput,
				tt.tCost, tt.tTotal, tt.tInput, tt.tOutput,
			)
			if cost != tt.wantCost {
				t.Errorf("cost = %f, want %f", cost, tt.wantCost)
			}
			if total != tt.wantTotal {
				t.Errorf("total = %d, want %d", total, tt.wantTotal)
			}
			if input != tt.wantInput {
				t.Errorf("input = %d, want %d", input, tt.wantInput)
			}
			if output != tt.wantOutput {
				t.Errorf("output = %d, want %d", output, tt.wantOutput)
			}
		})
	}
}

func TestActivityCoalesceExecutionTotals_InputOutputExceedsTotal(t *testing.T) {
	// When mixed dominance leads to input+output > total, total should be adjusted.
	cost, total, input, output := coalesceExecutionTotals(
		0.01, 100, 200, 10, // history: high input
		0.01, 100, 10, 200, // tracker: high output
	)
	_ = cost
	if input != 200 {
		t.Errorf("input = %d, want 200", input)
	}
	if output != 200 {
		t.Errorf("output = %d, want 200", output)
	}
	if total < input+output {
		t.Errorf("total = %d should be >= input+output = %d", total, input+output)
	}
}

// ---------------------------------------------------------------------------
// resolveExecutionTotals
// ---------------------------------------------------------------------------

func TestActivityResolveExecutionTotals_NilTracker(t *testing.T) {
	events := []*runtime.HistoryEvent{
		{
			Type: runtime.HistoryWorkflowCompleted,
			Attributes: map[string]interface{}{
				"total_cost":          0.05,
				"total_tokens":        500,
				"total_input_tokens":  300,
				"total_output_tokens": 200,
			},
		},
	}

	cost, total, input, output := resolveExecutionTotals(nil, events)
	if cost != 0.05 {
		t.Errorf("cost = %f, want 0.05", cost)
	}
	if total != 500 {
		t.Errorf("total = %d, want 500", total)
	}
	if input != 300 {
		t.Errorf("input = %d, want 300", input)
	}
	if output != 200 {
		t.Errorf("output = %d, want 200", output)
	}
}
