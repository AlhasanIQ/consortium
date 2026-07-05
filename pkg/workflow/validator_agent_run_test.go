package workflow

import "testing"

func TestValidatorAgentRunRequiresExplicitConfig(t *testing.T) {
	validator := NewValidator(nil)

	wf := &Workflow{
		ID: "wf-agent",
		Nodes: []*Node{
			{ID: "agent-a", Type: NodeTypeAgentRun},
		},
	}

	res := validator.Validate(wf)
	if res.Valid {
		t.Fatal("expected workflow to be invalid")
	}
	assertValidationError(t, res, "agent-a", "retry_policy")
	assertValidationError(t, res, "agent-a", "prompt")
	assertValidationError(t, res, "agent-a", "harness")
	assertValidationError(t, res, "agent-a", "timeout_seconds")
}

func TestValidatorAgentRunValidatesVariablesAndHarness(t *testing.T) {
	validator := NewValidator(nil)
	res := validator.Validate(&Workflow{
		ID:      "wf-agent",
		Context: map[string]interface{}{"topic": "durability"},
		Nodes: []*Node{
			{
				ID:             "agent-a",
				Type:           NodeTypeAgentRun,
				Prompt:         "Investigate {{missing}}",
				Harness:        "weird",
				TimeoutSeconds: 30,
				RetryPolicy:    NoRetryPolicy(),
			},
		},
	})
	if res.Valid {
		t.Fatal("expected invalid workflow")
	}
	assertValidationError(t, res, "agent-a", "prompt")
	assertValidationError(t, res, "agent-a", "harness")
}

func TestValidatorAgentRunValidatesSandbox(t *testing.T) {
	validator := NewValidator(nil)

	res := validator.Validate(&Workflow{
		ID: "wf-agent",
		Nodes: []*Node{
			{
				ID:             "agent-a",
				Type:           NodeTypeAgentRun,
				Prompt:         "Investigate durability",
				Harness:        "claude-code",
				Sandbox:        " vm ",
				TimeoutSeconds: 30,
				RetryPolicy:    NoRetryPolicy(),
			},
		},
	})
	if res.Valid {
		t.Fatal("expected invalid workflow")
	}
	assertValidationError(t, res, "agent-a", "sandbox")
}

func TestValidatorAgentRunValidWithoutModel(t *testing.T) {
	validator := NewValidator(nil)
	res := validator.Validate(&Workflow{
		ID:      "wf-agent",
		Context: map[string]interface{}{"topic": "durability"},
		Nodes: []*Node{
			{
				ID:             "agent-a",
				Type:           NodeTypeAgentRun,
				Prompt:         "Investigate {{topic}}",
				Harness:        "claude-code",
				TimeoutSeconds: 30,
				RetryPolicy:    NoRetryPolicy(),
			},
		},
	})
	if !res.Valid {
		t.Fatalf("expected valid workflow, got errors: %+v", res.Errors)
	}
}

func TestValidatorAgentRunAcceptsCodexHarness(t *testing.T) {
	validator := NewValidator(nil)
	res := validator.Validate(&Workflow{
		ID: "wf-agent",
		Nodes: []*Node{
			{
				ID:             "agent-a",
				Type:           NodeTypeAgentRun,
				Prompt:         "Investigate durability",
				Harness:        "codex",
				TimeoutSeconds: 30,
				RetryPolicy:    NoRetryPolicy(),
			},
		},
	})
	if !res.Valid {
		t.Fatalf("expected valid workflow, got errors: %+v", res.Errors)
	}
}

func TestValidatorNovomoHandoffRejectsInvalidExplicitHandle(t *testing.T) {
	validator := NewValidator(nil)

	res := validator.Validate(&Workflow{
		ID: "wf-agent",
		Nodes: []*Node{
			{
				ID:             "agent-a",
				Type:           NodeTypeAgentRun,
				Prompt:         "Investigate durability",
				Harness:        "claude-code",
				TimeoutSeconds: 30,
				RetryPolicy:    NoRetryPolicy(),
				InheritFrom:    &NovomoHandoffRef{Kind: "workspace", ID: "opaque"},
			},
		},
	})
	if res.Valid {
		t.Fatal("expected invalid workflow")
	}
	assertValidationError(t, res, "agent-a", "inherit_from")
}

func TestValidatorNovomoHandoffAllowsUpstreamAgentOrSuperagent(t *testing.T) {
	validator := NewValidator(nil)

	res := validator.Validate(&Workflow{
		ID: "wf-agent-chain",
		Nodes: []*Node{
			{
				ID:             "agent-a",
				Type:           NodeTypeAgentRun,
				Prompt:         "First",
				Harness:        "claude-code",
				TimeoutSeconds: 30,
				RetryPolicy:    NoRetryPolicy(),
			},
			{
				ID:                "superagent-b",
				Type:              NodeTypeNovoRun,
				Prompt:            "Continue",
				TimeoutSeconds:    30,
				RetryPolicy:       NoRetryPolicy(),
				InheritFromNodeID: "agent-a",
				InheritFromPolicy: "latest",
			},
		},
		Edges: []*Edge{{Source: "agent-a", Target: "superagent-b"}},
	})
	if !res.Valid {
		t.Fatalf("expected valid workflow, got errors: %+v", res.Errors)
	}
}

func TestValidatorNovomoHandoffRequiresUpstreamNode(t *testing.T) {
	validator := NewValidator(nil)

	res := validator.Validate(&Workflow{
		ID: "wf-agent-chain",
		Nodes: []*Node{
			{
				ID:                "superagent-b",
				Type:              NodeTypeNovoRun,
				Prompt:            "Continue",
				TimeoutSeconds:    30,
				RetryPolicy:       NoRetryPolicy(),
				InheritFromNodeID: "agent-a",
			},
			{
				ID:             "agent-a",
				Type:           NodeTypeAgentRun,
				Prompt:         "Later",
				Harness:        "claude-code",
				TimeoutSeconds: 30,
				RetryPolicy:    NoRetryPolicy(),
			},
		},
		Edges: []*Edge{{Source: "superagent-b", Target: "agent-a"}},
	})
	if res.Valid {
		t.Fatal("expected invalid workflow")
	}
	assertValidationError(t, res, "superagent-b", "inherit_from_node_id")
}

func TestValidatorNovoRunRequiresGoalOrTaskAndTimeout(t *testing.T) {
	validator := NewValidator(nil)

	res := validator.Validate(&Workflow{
		ID: "wf-superagent",
		Nodes: []*Node{
			{ID: "superagent-a", Type: NodeTypeNovoRun},
		},
	})
	if res.Valid {
		t.Fatal("expected workflow to be invalid")
	}
	assertValidationError(t, res, "superagent-a", "prompt")
	assertValidationError(t, res, "superagent-a", "timeout_seconds")
	assertValidationError(t, res, "superagent-a", "retry_policy")
}

func TestValidatorNovoRunValidWithTaskIDWithoutGoal(t *testing.T) {
	validator := NewValidator(nil)

	res := validator.Validate(&Workflow{
		ID: "wf-superagent",
		Nodes: []*Node{
			{
				ID:             "superagent-a",
				Type:           NodeTypeNovoRun,
				TaskID:         "task-existing",
				TimeoutSeconds: 30,
				RetryPolicy:    NoRetryPolicy(),
			},
		},
	})
	if !res.Valid {
		t.Fatalf("expected valid workflow, got errors: %+v", res.Errors)
	}
}

func TestValidatorNovoRunValidatesVariablesAndSandbox(t *testing.T) {
	validator := NewValidator(nil)

	res := validator.Validate(&Workflow{
		ID:      "wf-superagent",
		Context: map[string]interface{}{"topic": "durability"},
		Nodes: []*Node{
			{
				ID:             "superagent-a",
				Type:           NodeTypeNovoRun,
				Prompt:         "Investigate {{missing}}",
				Sandbox:        "vm",
				TimeoutSeconds: 30,
				RetryPolicy:    NoRetryPolicy(),
			},
		},
	})
	if res.Valid {
		t.Fatal("expected invalid workflow")
	}
	assertValidationError(t, res, "superagent-a", "prompt")
	assertValidationError(t, res, "superagent-a", "sandbox")
}

func assertValidationError(t *testing.T, res *ValidationResult, nodeID, field string) {
	t.Helper()
	for _, err := range res.Errors {
		if err.NodeID == nodeID && err.Field == field {
			return
		}
	}
	t.Fatalf("missing validation error node=%s field=%s in %+v", nodeID, field, res.Errors)
}
