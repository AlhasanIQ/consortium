package workflow

import (
	"context"
	"testing"
	"time"
)

func TestAgentRunNodeRunnerExecutesAndReturnsUsage(t *testing.T) {
	runner := &AgentRunNodeRunner{}
	var captured *AgentRunRequest

	result, err := runner.Execute(&NodeContext{
		Ctx:     context.Background(),
		NodeID:  "agent-a",
		Attempt: 2,
		Node: &Node{
			ID:             "agent-a",
			Type:           NodeTypeAgentRun,
			Prompt:         "Investigate {{topic}}",
			Harness:        "claude-code",
			Sandbox:        "host",
			TimeoutSeconds: 30,
		},
		WorkflowContext: map[string]interface{}{"topic": "durability"},
		ExecCtx: &ExecutionContext{
			JobID:               "job-1",
			WorkflowExecutionID: "exec-1",
			WorkflowID:          "wf-1",
			RunID:               "run-1",
			ExecuteAgentRun: func(ctx context.Context, req *AgentRunRequest) (*AgentRunResult, error) {
				captured = req
				return &AgentRunResult{
					ExternalRunID: "novomo-1",
					Harness:       "claude-code",
					Status:        "completed",
					Success:       true,
					Output:        "done\n",
					TokensInput:   11,
					TokensOutput:  7,
					Cost:          0.23,
				}, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if captured == nil {
		t.Fatal("ExecuteAgentRun was not called")
	}
	if captured.ParentJobID != "job-1" || captured.ParentExecutionID != "exec-1" || captured.ParentRunID != "run-1" || captured.ParentWorkflowID != "wf-1" {
		t.Fatalf("unexpected parent context: %+v", captured)
	}
	if captured.ParentNodeID != "agent-a" || captured.Attempt != 2 {
		t.Fatalf("unexpected node attempt: %+v", captured)
	}
	if captured.Prompt != "Investigate durability" {
		t.Fatalf("Prompt = %q", captured.Prompt)
	}
	if captured.Harness != "claude-code" || captured.Sandbox != "host" || captured.TimeoutSeconds != 30 {
		t.Fatalf("unexpected runtime config: %+v", captured)
	}
	if captured.IdempotencyKey != "exec-1:agent-a:2" {
		t.Fatalf("IdempotencyKey = %q, want exec-1:agent-a:2", captured.IdempotencyKey)
	}
	if !result.Success || result.Output != "done\n" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.TokensInput != 11 || result.TokensOutput != 7 || result.Cost != 0.23 {
		t.Fatalf("usage not propagated: %+v", result)
	}
	if result.Metadata["external_run_id"] != "novomo-1" || result.Metadata["harness"] != "claude-code" {
		t.Fatalf("metadata missing external run fields: %+v", result.Metadata)
	}
}

func TestAgentRunNodeRunnerDefaultsMissingSandboxToDocker(t *testing.T) {
	runner := &AgentRunNodeRunner{}
	var captured *AgentRunRequest

	_, err := runner.Execute(&NodeContext{
		Ctx:     context.Background(),
		NodeID:  "agent-a",
		Attempt: 1,
		Node: &Node{
			ID:             "agent-a",
			Type:           NodeTypeAgentRun,
			Prompt:         "Do work",
			Harness:        "claude-code",
			TimeoutSeconds: 30,
		},
		ExecCtx: &ExecutionContext{
			ExecuteAgentRun: func(ctx context.Context, req *AgentRunRequest) (*AgentRunResult, error) {
				captured = req
				return &AgentRunResult{ExternalRunID: "novomo-1", Harness: "claude-code", Status: "completed", Success: true, Output: "done"}, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if captured == nil {
		t.Fatal("ExecuteAgentRun was not called")
	}
	if captured.Sandbox != "docker" {
		t.Fatalf("Sandbox = %q, want docker", captured.Sandbox)
	}
}

func TestAgentRunNodeRunnerUsesPollGraceContext(t *testing.T) {
	runner := &AgentRunNodeRunner{}

	_, err := runner.Execute(&NodeContext{
		Ctx:     context.Background(),
		NodeID:  "agent-a",
		Attempt: 1,
		Node: &Node{
			ID:             "agent-a",
			Type:           NodeTypeAgentRun,
			Prompt:         "Do work",
			Harness:        "claude-code",
			TimeoutSeconds: 1,
		},
		ExecCtx: &ExecutionContext{
			JobID:      "job-1",
			WorkflowID: "wf-1",
			RunID:      "run-1",
			ExecuteAgentRun: func(ctx context.Context, req *AgentRunRequest) (*AgentRunResult, error) {
				deadline, ok := ctx.Deadline()
				if !ok {
					t.Fatal("expected callback context deadline")
				}
				remaining := time.Until(deadline)
				if remaining < 10*time.Second || remaining > 12*time.Second {
					t.Fatalf("callback deadline remaining = %s, want timeout plus grace", remaining)
				}
				return &AgentRunResult{ExternalRunID: "novomo-1", Harness: "claude-code", Status: "completed", Success: true, Output: "done"}, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
}

func TestAgentRunNodeRunnerClassifiesTerminalFailures(t *testing.T) {
	tests := []struct {
		name      string
		code      string
		retryable bool
	}{
		{name: "timeout", code: "timeout", retryable: true},
		{name: "crashed", code: "crashed", retryable: true},
		{name: "startup failed", code: "startup_failed", retryable: true},
		{name: "bad request", code: "bad_request", retryable: false},
		{name: "cancelled", code: "CANCELLED", retryable: false},
		{name: "novomo error", code: "NOVOMO_ERROR", retryable: false},
		{name: "unknown", code: "unknown", retryable: false},
		{name: "unresponsive", code: "NOVOMO_UNRESPONSIVE", retryable: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &AgentRunNodeRunner{}
			result, err := runner.Execute(&NodeContext{
				Ctx:    context.Background(),
				NodeID: "agent-a",
				Node: &Node{
					ID:             "agent-a",
					Type:           NodeTypeAgentRun,
					Prompt:         "Do work",
					Harness:        "claude-code",
					TimeoutSeconds: 30,
				},
				ExecCtx: &ExecutionContext{
					ExecuteAgentRun: func(ctx context.Context, req *AgentRunRequest) (*AgentRunResult, error) {
						return &AgentRunResult{
							ExternalRunID: "novomo-1",
							Harness:       "claude-code",
							Status:        "failed",
							Success:       false,
							ErrorCode:     tt.code,
							Error:         "agent failed",
						}, nil
					},
				},
			})
			if result == nil || result.Success {
				t.Fatalf("expected failed result, got %+v", result)
			}
			if err == nil {
				t.Fatal("expected classified error")
			}
			if IsRetryable(err) != tt.retryable {
				t.Fatalf("retryable = %v, want %v, err=%v", IsRetryable(err), tt.retryable, err)
			}
			if GetErrorCode(err) != tt.code {
				t.Fatalf("code = %q, want %q", GetErrorCode(err), tt.code)
			}
		})
	}
}

func TestAgentRunNodeRunnerRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		node *Node
		code string
	}{
		{name: "missing prompt", node: &Node{Type: NodeTypeAgentRun, Harness: "claude-code", TimeoutSeconds: 30}, code: "INVALID_CONFIG"},
		{name: "missing harness", node: &Node{Type: NodeTypeAgentRun, Prompt: "do", TimeoutSeconds: 30}, code: "INVALID_CONFIG"},
		{name: "unsupported harness", node: &Node{Type: NodeTypeAgentRun, Prompt: "do", Harness: "weird", TimeoutSeconds: 30}, code: "INVALID_CONFIG"},
		{name: "invalid sandbox", node: &Node{Type: NodeTypeAgentRun, Prompt: "do", Harness: "claude-code", Sandbox: "vm", TimeoutSeconds: 30}, code: "INVALID_CONFIG"},
		{name: "missing timeout", node: &Node{Type: NodeTypeAgentRun, Prompt: "do", Harness: "claude-code"}, code: "INVALID_CONFIG"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &AgentRunNodeRunner{}
			_, err := runner.Execute(&NodeContext{
				Ctx:     context.Background(),
				NodeID:  "agent-a",
				Node:    tt.node,
				ExecCtx: &ExecutionContext{ExecuteAgentRun: func(context.Context, *AgentRunRequest) (*AgentRunResult, error) { return nil, nil }},
			})
			if err == nil {
				t.Fatal("expected error")
			}
			if GetErrorCode(err) != tt.code {
				t.Fatalf("code = %q, want %q", GetErrorCode(err), tt.code)
			}
		})
	}
}
