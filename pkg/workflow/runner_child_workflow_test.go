package workflow

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
)

func TestChildWorkflowNodeRunnerSuccess(t *testing.T) {
	runner := &ChildWorkflowNodeRunner{}

	result, err := runner.Execute(withStrictNodeContextDefaults(&NodeContext{
		Ctx:    context.Background(),
		NodeID: "child-node",
		Node: &Node{
			ID:                 "child-node",
			Type:               NodeTypeChildWorkflow,
			ChildWorkflowID:    "reasoning-informed-captain-synthesis",
			ChildOutputKey:     "benchmark_answer",
			TimeoutSeconds:     5,
			ChildInputTemplate: map[string]string{"user_prompt": "{{user_prompt}}"},
		},
		WorkflowContext: map[string]interface{}{
			"user_prompt": "Q",
		},
		ExecCtx: &ExecutionContext{
			JobID:      "parent-job",
			WorkflowID: "parent-wf",
			RunID:      "parent-run",
			ExecuteChildWorkflow: func(ctx context.Context, req *ChildWorkflowRequest) (*ChildWorkflowResult, error) {
				if req.ChildWorkflowID != "reasoning-informed-captain-synthesis" {
					t.Fatalf("unexpected child workflow id: %s", req.ChildWorkflowID)
				}
				return &ChildWorkflowResult{
					JobID:             "child-job",
					WorkflowID:        req.ChildWorkflowID,
					Success:           true,
					FinalOutput:       "B",
					Outputs:           map[string]interface{}{"benchmark_answer": "A"},
					TotalInputTokens:  10,
					TotalOutputTokens: 3,
					TotalTokens:       13,
					TotalCost:         0.01,
				}, nil
			},
		},
	}))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("expected successful result, got %+v", result)
	}
	if result.Output != "A" {
		t.Fatalf("expected output A, got %q", result.Output)
	}
	if result.Cost != 0 {
		t.Fatalf("expected parent node cost to exclude child cost, got %f", result.Cost)
	}
	if result.TokensInput != 0 || result.TokensOutput != 0 {
		t.Fatalf("expected parent node tokens to exclude child usage, got in=%d out=%d", result.TokensInput, result.TokensOutput)
	}
	if result.Metadata["child_total_cost"] != 0.01 {
		t.Fatalf("expected child_total_cost metadata to be preserved, got %v", result.Metadata["child_total_cost"])
	}
	if result.Metadata["child_total_tokens"] != 13 {
		t.Fatalf("expected child_total_tokens metadata to be preserved, got %v", result.Metadata["child_total_tokens"])
	}
}

func TestChildWorkflowNodeRunnerMissingExecutor(t *testing.T) {
	runner := &ChildWorkflowNodeRunner{}
	_, err := runner.Execute(withStrictNodeContextDefaults(&NodeContext{
		Ctx:    context.Background(),
		NodeID: "child-node",
		Node: &Node{
			ID:              "child-node",
			Type:            NodeTypeChildWorkflow,
			ChildWorkflowID: "reasoning-informed-captain-synthesis",
		},
	}))
	if err == nil {
		t.Fatalf("expected error when child executor is missing")
	}
}

func TestChildWorkflowNodeRunnerTransientFailureWrapsRetryable(t *testing.T) {
	runner := &ChildWorkflowNodeRunner{}
	_, err := runner.Execute(withStrictNodeContextDefaults(&NodeContext{
		Ctx:    context.Background(),
		NodeID: "child-node",
		Node: &Node{
			ID:              "child-node",
			Type:            NodeTypeChildWorkflow,
			ChildWorkflowID: "reasoning-informed-captain-synthesis",
		},
		ExecCtx: &ExecutionContext{
			ExecuteChildWorkflow: func(ctx context.Context, req *ChildWorkflowRequest) (*ChildWorkflowResult, error) {
				return &ChildWorkflowResult{
					Success: false,
					Error:   "rate limit exceeded: rate limit wait (405ms) exceeds context deadline (120ms remaining)",
				}, nil
			},
		},
	}))
	if err == nil {
		t.Fatalf("expected child workflow error")
	}
	retryErr, ok := err.(*RetryableError)
	if !ok {
		t.Fatalf("expected RetryableError, got %T", err)
	}
	if !retryErr.Retryable {
		t.Fatalf("expected transient child failure to be retryable")
	}
	if retryErr.Code != RetryCodeChildWorkflowTransient {
		t.Fatalf("expected retry code %s, got %s", RetryCodeChildWorkflowTransient, retryErr.Code)
	}
}

func TestChildWorkflowNodeRunnerEmptyOutputRetryableWhenChildConsumedTokens(t *testing.T) {
	runner := &ChildWorkflowNodeRunner{}
	result, err := runner.Execute(withStrictNodeContextDefaults(&NodeContext{
		Ctx:    context.Background(),
		NodeID: "child-node",
		Node: &Node{
			ID:              "child-node",
			Type:            NodeTypeChildWorkflow,
			ChildWorkflowID: "reasoning-informed-captain-synthesis",
		},
		ExecCtx: &ExecutionContext{
			ExecuteChildWorkflow: func(ctx context.Context, req *ChildWorkflowRequest) (*ChildWorkflowResult, error) {
				return &ChildWorkflowResult{
					WorkflowID:        req.ChildWorkflowID,
					Success:           true,
					FinalOutput:       "   ",
					TotalInputTokens:  10,
					TotalOutputTokens: 6,
					TotalTokens:       16,
				}, nil
			},
		},
	}))
	if err == nil {
		t.Fatalf("expected error for empty child output")
	}
	if result == nil || result.Success {
		t.Fatalf("expected failed node result, got %+v", result)
	}
	var retryErr *RetryableError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected RetryableError, got %T: %v", err, err)
	}
	if !retryErr.Retryable {
		t.Fatalf("expected retryable empty-output error")
	}
	if retryErr.Code != RetryCodeOutputTruncatedEmpty {
		t.Fatalf("expected retry code %s, got %s", RetryCodeOutputTruncatedEmpty, retryErr.Code)
	}
	if got := result.Metadata["child_total_output_tokens"]; got != 6 {
		t.Fatalf("expected child_total_output_tokens metadata = 6, got %v", got)
	}
}

func TestChildWorkflowNodeRunnerEmptyOutputNonRetryableWhenNoOutputTokens(t *testing.T) {
	runner := &ChildWorkflowNodeRunner{}
	result, err := runner.Execute(withStrictNodeContextDefaults(&NodeContext{
		Ctx:    context.Background(),
		NodeID: "child-node",
		Node: &Node{
			ID:              "child-node",
			Type:            NodeTypeChildWorkflow,
			ChildWorkflowID: "reasoning-informed-captain-synthesis",
		},
		ExecCtx: &ExecutionContext{
			ExecuteChildWorkflow: func(ctx context.Context, req *ChildWorkflowRequest) (*ChildWorkflowResult, error) {
				return &ChildWorkflowResult{
					WorkflowID:        req.ChildWorkflowID,
					Success:           true,
					FinalOutput:       "",
					TotalInputTokens:  10,
					TotalOutputTokens: 0,
					TotalTokens:       10,
				}, nil
			},
		},
	}))
	if err == nil {
		t.Fatalf("expected error for empty child output")
	}
	if result == nil || result.Success {
		t.Fatalf("expected failed node result, got %+v", result)
	}
	var retryErr *RetryableError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected RetryableError wrapper, got %T: %v", err, err)
	}
	if retryErr.Retryable {
		t.Fatalf("expected non-retryable empty-output error when child produced zero output tokens")
	}
	if retryErr.Code != "EMPTY_CHILD_OUTPUT" {
		t.Fatalf("expected error code EMPTY_CHILD_OUTPUT, got %s", retryErr.Code)
	}
}

// mockNetError implements net.Error for testing network timeout classification.
type mockNetError struct {
	timeout bool
}

func (e *mockNetError) Error() string   { return "mock net error" }
func (e *mockNetError) Timeout() bool   { return e.timeout }
func (e *mockNetError) Temporary() bool { return false }

// Verify mockNetError satisfies net.Error at compile time.
var _ net.Error = (*mockNetError)(nil)

func TestClassifyChildWorkflowGoError(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		wantNil       bool
		wantRetryable *bool  // nil means expect unwrapped (no RetryableError)
		wantCode      string // only checked when wantRetryable != nil
	}{
		{
			name:    "nil returns nil",
			err:     nil,
			wantNil: true,
		},
		{
			name:          "context.Canceled is non-retryable CANCELLED",
			err:           context.Canceled,
			wantRetryable: boolPtr(false),
			wantCode:      "CANCELLED",
		},
		{
			name:          "context.DeadlineExceeded is retryable TIMEOUT",
			err:           context.DeadlineExceeded,
			wantRetryable: boolPtr(true),
			wantCode:      "TIMEOUT",
		},
		{
			name:          "net.Error with Timeout()=true is retryable TIMEOUT",
			err:           &mockNetError{timeout: true},
			wantRetryable: boolPtr(true),
			wantCode:      "TIMEOUT",
		},
		{
			name:          "auth error is non-retryable",
			err:           fmt.Errorf("auth error: invalid credentials"),
			wantRetryable: boolPtr(false),
			wantCode:      "CHILD_WORKFLOW_FATAL",
		},
		{
			name:          "rate limit is retryable transient",
			err:           fmt.Errorf("rate limit exceeded"),
			wantRetryable: boolPtr(true),
			wantCode:      RetryCodeChildWorkflowTransient,
		},
		{
			name:          "generic unknown error returned as-is",
			err:           fmt.Errorf("something unexpected"),
			wantRetryable: nil, // no wrapping
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := classifyChildWorkflowGoError(tc.err)

			if tc.wantNil {
				if result != nil {
					t.Fatalf("expected nil, got %v", result)
				}
				return
			}

			if result == nil {
				t.Fatalf("expected non-nil error, got nil")
			}

			if tc.wantRetryable == nil {
				// Expect the error returned as-is, not wrapped in RetryableError
				var retryErr *RetryableError
				if errors.As(result, &retryErr) {
					t.Fatalf("expected unwrapped error, got RetryableError with code=%s retryable=%v", retryErr.Code, retryErr.Retryable)
				}
				if result.Error() != tc.err.Error() {
					t.Fatalf("expected error message %q, got %q", tc.err.Error(), result.Error())
				}
				return
			}

			var retryErr *RetryableError
			if !errors.As(result, &retryErr) {
				t.Fatalf("expected RetryableError, got %T: %v", result, result)
			}
			if retryErr.Retryable != *tc.wantRetryable {
				t.Fatalf("expected Retryable=%v, got %v", *tc.wantRetryable, retryErr.Retryable)
			}
			if retryErr.Code != tc.wantCode {
				t.Fatalf("expected Code=%q, got %q", tc.wantCode, retryErr.Code)
			}
		})
	}
}

func TestIsNonRetryableChildWorkflowError(t *testing.T) {
	tests := []struct {
		errMsg string
		want   bool
	}{
		{"auth error", true},
		{"invalid api key", true},
		{"model not found", true},
		{"rate limit", false},
		{"", false},
		{"random failure", false},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("%q", tc.errMsg), func(t *testing.T) {
			got := isNonRetryableChildWorkflowError(tc.errMsg)
			if got != tc.want {
				t.Fatalf("isNonRetryableChildWorkflowError(%q) = %v, want %v", tc.errMsg, got, tc.want)
			}
		})
	}
}

func boolPtr(b bool) *bool { return &b }
