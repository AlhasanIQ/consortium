package workflow

import (
	"context"
	"testing"
)

func TestNovoRunNodeRunnerExecutesAndReturnsUsage(t *testing.T) {
	runner := &NovoRunNodeRunner{}
	var captured *NovoRunRequest

	result, err := runner.Execute(&NodeContext{
		Ctx:     context.Background(),
		NodeID:  "superagent-a",
		Attempt: 2,
		Node: &Node{
			ID:             "superagent-a",
			Type:           NodeTypeNovoRun,
			Prompt:         "Investigate {{topic}}",
			TaskID:         "task-existing",
			TaskSummary:    "brief",
			Identity:       "sde-novo",
			Sandbox:        "host",
			RuntimeURL:     "http://127.0.0.1:18082",
			TimeoutSeconds: 30,
			GraceSeconds:   5,
			RepoSpecs: []map[string]interface{}{{
				"name": "app",
				"source": map[string]interface{}{
					"type":      "host_path",
					"host_path": map[string]interface{}{"path": "/tmp/app"},
				},
			}},
			WorkSource: map[string]interface{}{
				"type":         "gitea_branch",
				"gitea_branch": map[string]interface{}{"branch_ref": "main"},
			},
		},
		WorkflowContext: map[string]interface{}{"topic": "durability"},
		ExecCtx: &ExecutionContext{
			JobID:               "job-1",
			WorkflowExecutionID: "exec-1",
			WorkflowID:          "wf-1",
			RunID:               "run-1",
			ExecuteNovoRun: func(ctx context.Context, req *NovoRunRequest) (*AgentRunResult, error) {
				captured = req
				return &AgentRunResult{
					ExternalRunID:   "nr-1",
					ExternalRunKind: "novo_run",
					ExternalTaskID:  "task-existing",
					Harness:         "claude-code",
					Status:          "completed",
					Success:         true,
					Output:          "done\n",
					TokensInput:     11,
					TokensOutput:    7,
					Cost:            0.23,
				}, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if captured == nil {
		t.Fatal("ExecuteNovoRun was not called")
	}
	if captured.ParentJobID != "job-1" || captured.ParentExecutionID != "exec-1" || captured.ParentRunID != "run-1" || captured.ParentWorkflowID != "wf-1" {
		t.Fatalf("unexpected parent context: %+v", captured)
	}
	if captured.ParentNodeID != "superagent-a" || captured.Attempt != 2 {
		t.Fatalf("unexpected node attempt: %+v", captured)
	}
	if captured.Goal != "Investigate durability" || captured.TaskID != "task-existing" || captured.TaskSummary != "brief" {
		t.Fatalf("unexpected task fields: %+v", captured)
	}
	if captured.Identity != "sde-novo" || captured.Sandbox != "host" || captured.RuntimeURL != "http://127.0.0.1:18082" {
		t.Fatalf("unexpected wake config: %+v", captured)
	}
	if captured.TimeoutSeconds != 30 || captured.GraceSeconds != 5 {
		t.Fatalf("unexpected timeout config: %+v", captured)
	}
	if len(captured.RepoSpecs) != 1 || captured.WorkSource["type"] != "gitea_branch" {
		t.Fatalf("repo/work source not propagated: %+v", captured)
	}
	if captured.IdempotencyKey != "exec-1:superagent-a:2" {
		t.Fatalf("IdempotencyKey = %q, want exec-1:superagent-a:2", captured.IdempotencyKey)
	}
	if !result.Success || result.Output != "done\n" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Metadata["external_run_id"] != "nr-1" ||
		result.Metadata["external_run_kind"] != "novo_run" ||
		result.Metadata["external_task_id"] != "task-existing" {
		t.Fatalf("metadata missing NovoRun fields: %+v", result.Metadata)
	}
}

func TestNovoRunNodeRunnerAllowsExistingTaskWithoutGoal(t *testing.T) {
	runner := &NovoRunNodeRunner{}
	called := false
	_, err := runner.Execute(&NodeContext{
		Ctx:    context.Background(),
		NodeID: "superagent-a",
		Node: &Node{
			ID:             "superagent-a",
			Type:           NodeTypeNovoRun,
			TaskID:         "task-existing",
			TimeoutSeconds: 30,
		},
		ExecCtx: &ExecutionContext{
			ExecuteNovoRun: func(ctx context.Context, req *NovoRunRequest) (*AgentRunResult, error) {
				called = true
				return &AgentRunResult{ExternalRunID: "nr-1", ExternalRunKind: "novo_run", ExternalTaskID: "task-existing", Status: "completed", Success: true}, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !called {
		t.Fatal("ExecuteNovoRun was not called")
	}
}

func TestNovoRunNodeRunnerDefaultsMissingSandboxToDocker(t *testing.T) {
	runner := &NovoRunNodeRunner{}
	var captured *NovoRunRequest

	_, err := runner.Execute(&NodeContext{
		Ctx:    context.Background(),
		NodeID: "superagent-a",
		Node: &Node{
			ID:             "superagent-a",
			Type:           NodeTypeNovoRun,
			Prompt:         "Investigate",
			TimeoutSeconds: 30,
		},
		ExecCtx: &ExecutionContext{
			ExecuteNovoRun: func(ctx context.Context, req *NovoRunRequest) (*AgentRunResult, error) {
				captured = req
				return &AgentRunResult{ExternalRunID: "nr-1", ExternalRunKind: "novo_run", Status: "completed", Success: true}, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if captured == nil {
		t.Fatal("ExecuteNovoRun was not called")
	}
	if captured.Sandbox != "docker" {
		t.Fatalf("Sandbox = %q, want docker", captured.Sandbox)
	}
}

func TestNovoRunNodeRunnerRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		node *Node
		code string
	}{
		{name: "missing goal and task", node: &Node{Type: NodeTypeNovoRun, TimeoutSeconds: 30}, code: "INVALID_CONFIG"},
		{name: "missing timeout", node: &Node{Type: NodeTypeNovoRun, Prompt: "do"}, code: "INVALID_CONFIG"},
		{name: "invalid sandbox", node: &Node{Type: NodeTypeNovoRun, Prompt: "do", Sandbox: "vm", TimeoutSeconds: 30}, code: "INVALID_CONFIG"},
		{name: "invalid grace", node: &Node{Type: NodeTypeNovoRun, Prompt: "do", TimeoutSeconds: 30, GraceSeconds: -1}, code: "INVALID_CONFIG"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &NovoRunNodeRunner{}
			_, err := runner.Execute(&NodeContext{
				Ctx:     context.Background(),
				NodeID:  "superagent-a",
				Node:    tt.node,
				ExecCtx: &ExecutionContext{ExecuteNovoRun: func(context.Context, *NovoRunRequest) (*AgentRunResult, error) { return nil, nil }},
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
