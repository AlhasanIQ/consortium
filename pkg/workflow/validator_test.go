package workflow

import (
	"strings"
	"testing"

	"github.com/alhasaniq/consortium/pkg/providers"
)

func TestValidateStructure(t *testing.T) {
	registry := providers.NewRegistry()
	mockProvider := NewMockProvider("test")
	registry.Register(mockProvider)
	validator := NewValidator(registry)

	tests := []struct {
		name        string
		workflow    *Workflow
		expectValid bool
		// If non-empty, at least one error must have this exact Message.
		expectErrorMsg string
	}{
		{
			name: "empty workflow",
			workflow: &Workflow{
				ID:    "test",
				Nodes: []*Node{},
			},
			expectValid: false,
		},
		{
			name: "duplicate node IDs",
			workflow: &Workflow{
				ID: "test",
				Nodes: []*Node{
					{ID: "step1", Type: NodeTypePrompt, Model: "mock-model", Prompt: "test"},
					{ID: "step1", Type: NodeTypePrompt, Model: "mock-model", Prompt: "test"},
				},
			},
			expectValid:    false,
			expectErrorMsg: "Duplicate node ID: 'step1'",
		},
		{
			name: "node ID with double underscore rejected",
			workflow: &Workflow{
				ID: "test",
				Nodes: []*Node{
					{ID: "foo__bar", Type: NodeTypePrompt, Model: "mock-model", Prompt: "test"},
				},
			},
			expectValid:    false,
			expectErrorMsg: "Node ID 'foo__bar' must not contain '__' (reserved separator for context metadata)",
		},
		{
			name: "prompt node missing prompt",
			workflow: &Workflow{
				ID: "test",
				Nodes: []*Node{
					{ID: "node1", Type: NodeTypePrompt, Model: "mock-model", Prompt: ""},
				},
			},
			expectValid: false,
		},
		{
			name: "prompt node missing model",
			workflow: &Workflow{
				ID: "test",
				Nodes: []*Node{
					{ID: "node1", Type: NodeTypePrompt, Model: "", Prompt: "test"},
				},
			},
			expectValid: false,
		},
		{
			name: "contract_extract node missing source_variable",
			workflow: &Workflow{
				ID: "test",
				Nodes: []*Node{
					{
						ID:     "contract",
						Type:   NodeTypeContractExtract,
						Model:  "mock-model",
						Prompt: "Extract: {{source}}",
						Metadata: map[string]interface{}{
							"extraction_patterns": []interface{}{`^\s*([A-Za-z])\s*$`},
						},
					},
				},
			},
			expectValid:    false,
			expectErrorMsg: "Contract extract node requires metadata source_variable",
		},
		{
			name: "contract_extract node invalid extraction pattern",
			workflow: &Workflow{
				ID: "test",
				Nodes: []*Node{
					{
						ID:     "contract",
						Type:   NodeTypeContractExtract,
						Model:  "mock-model",
						Prompt: "Extract: {{source}}",
						Metadata: map[string]interface{}{
							"source_variable":     "source",
							"extraction_patterns": []interface{}{"[invalid"},
						},
					},
				},
				Context: map[string]interface{}{"source": "Final answer: B"},
			},
			expectValid:    false,
			expectErrorMsg: "Invalid extraction pattern at index 0: error parsing regexp: missing closing ]: `[invalid`",
		},
		{
			name: "valid workflow",
			workflow: &Workflow{
				ID: "test",
				Nodes: []*Node{
					{ID: "step1", Type: NodeTypePrompt, Model: "mock-model", Prompt: "test"},
				},
			},
			expectValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf := tt.workflow
			if tt.expectValid {
				wf = withStrictWorkflowDefaults(wf)
			}
			result := validator.Validate(wf)
			if result.Valid != tt.expectValid {
				if tt.expectValid {
					t.Errorf("Expected validation to pass, got errors: %v", result.Errors)
				} else {
					t.Errorf("Expected validation to fail for %s", tt.name)
				}
			}
			if !tt.expectValid && len(result.Errors) == 0 && tt.name == "empty workflow" {
				t.Error("Expected validation errors")
			}
			if tt.expectErrorMsg != "" {
				found := false
				for _, err := range result.Errors {
					if err.Message == tt.expectErrorMsg {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected error message %q, got: %v", tt.expectErrorMsg, result.Errors)
				}
			}
		})
	}
}

func TestValidateOperationNodeStructure(t *testing.T) {
	registry := providers.NewRegistry()
	registry.Register(NewMockProvider("test"))
	validator := NewValidator(registry)

	t.Run("valid operation does not require retry policy", func(t *testing.T) {
		wf := &Workflow{
			ID: "test",
			Nodes: []*Node{{
				ID:              "count_votes",
				Type:            NodeTypeOperation,
				OperationType:   OperationCountVotes,
				OperationConfig: map[string]interface{}{"answers": []interface{}{"A", "B", "A"}},
			}},
		}

		result := validator.Validate(wf)
		if !result.Valid {
			t.Fatalf("expected operation node without retry policy to validate, got %v", result.Errors)
		}
	})

	tests := []struct {
		name        string
		node        *Node
		wantMessage string
	}{
		{
			name: "missing operation type",
			node: &Node{
				ID:              "op",
				Type:            NodeTypeOperation,
				OperationConfig: map[string]interface{}{"answers": []interface{}{"A"}},
			},
			wantMessage: "Operation node requires operation_type",
		},
		{
			name: "unknown operation type",
			node: &Node{
				ID:              "op",
				Type:            NodeTypeOperation,
				OperationType:   "unsupported",
				OperationConfig: map[string]interface{}{"answers": []interface{}{"A"}},
			},
			wantMessage: `Unsupported operation_type "unsupported"`,
		},
		{
			name: "missing required config",
			node: &Node{
				ID:              "op",
				Type:            NodeTypeOperation,
				OperationType:   OperationCountVotes,
				OperationConfig: map[string]interface{}{},
			},
			wantMessage: `Operation "count_votes" requires one of: answers`,
		},
		{
			name: "missing one required config key",
			node: &Node{
				ID:              "op",
				Type:            NodeTypeOperation,
				OperationType:   OperationParseJSONField,
				OperationConfig: map[string]interface{}{"text": "{{judge_output}}"},
			},
			wantMessage: `Operation "parse_json_field" requires: text, field`,
		},
		{
			name: "uncompiled workflow reference",
			node: &Node{
				ID:            "ref",
				Type:          NodeTypeWorkflowRef,
				WorkflowRefID: "aggregation-synthesis",
			},
			wantMessage: "workflow_ref nodes must be compiled before runtime validation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.Validate(&Workflow{ID: "test", Nodes: []*Node{tt.node}})
			if result.Valid {
				t.Fatal("expected validation to fail")
			}
			for _, err := range result.Errors {
				if err.Message == tt.wantMessage {
					return
				}
			}
			t.Fatalf("expected error %q, got %v", tt.wantMessage, result.Errors)
		})
	}
}

func TestValidateConditionalBranchesRecursively(t *testing.T) {
	temp := 0.2
	validator := NewValidator(nil)

	t.Run("branch prompt structure is validated", func(t *testing.T) {
		wf := &Workflow{
			ID: "branch-structure",
			Nodes: []*Node{
				{
					ID:             "source",
					Type:           NodeTypePrompt,
					Model:          "mock-model",
					Prompt:         "Seed",
					Temperature:    &temp,
					MaxTokens:      64,
					TimeoutSeconds: 30,
					RetryPolicy:    testNoRetryPolicy(),
				},
				{
					ID:          "gate",
					Type:        NodeTypeConditional,
					Condition:   "source contains yes",
					RetryPolicy: testNoRetryPolicy(),
					TrueBranch: &Node{
						ID:             "branch-prompt",
						Type:           NodeTypePrompt,
						Model:          "mock-model",
						Prompt:         "Repair {{source}}",
						Temperature:    &temp,
						TimeoutSeconds: 30,
						RetryPolicy:    testNoRetryPolicy(),
					},
				},
			},
			Edges: []*Edge{{ID: "source-gate", Source: "source", Target: "gate"}},
		}

		result := validator.Validate(wf)
		if result.Valid {
			t.Fatalf("expected nested branch prompt without max_tokens to fail validation")
		}
		assertValidationErrorContains(t, result.Errors, "branch-prompt", "max_tokens", "Prompt node requires explicit max_tokens")
	})

	t.Run("branch prompt variables use parent workflow context", func(t *testing.T) {
		wf := &Workflow{
			ID: "branch-vars",
			Nodes: []*Node{
				{
					ID:             "source",
					Type:           NodeTypePrompt,
					Model:          "mock-model",
					Prompt:         "Seed",
					Temperature:    &temp,
					MaxTokens:      64,
					TimeoutSeconds: 30,
					RetryPolicy:    testNoRetryPolicy(),
				},
				{
					ID:          "gate",
					Type:        NodeTypeConditional,
					Condition:   "source contains yes",
					RetryPolicy: testNoRetryPolicy(),
					TrueBranch: &Node{
						ID:             "branch-prompt",
						Type:           NodeTypePrompt,
						Model:          "mock-model",
						Prompt:         "Repair {{missing_branch_input}}",
						Temperature:    &temp,
						MaxTokens:      64,
						TimeoutSeconds: 30,
						RetryPolicy:    testNoRetryPolicy(),
					},
				},
			},
			Edges: []*Edge{{ID: "source-gate", Source: "source", Target: "gate"}},
		}

		result := validator.Validate(wf)
		if result.Valid {
			t.Fatalf("expected missing variable in nested branch prompt to fail validation")
		}
		assertValidationErrorContains(t, result.Errors, "branch-prompt", "prompt", "Variable '{{missing_branch_input}}' not found")
	})

	t.Run("nested branch conditionals are validated", func(t *testing.T) {
		wf := &Workflow{
			ID: "branch-condition",
			Nodes: []*Node{
				{
					ID:             "source",
					Type:           NodeTypePrompt,
					Model:          "mock-model",
					Prompt:         "Seed",
					Temperature:    &temp,
					MaxTokens:      64,
					TimeoutSeconds: 30,
					RetryPolicy:    testNoRetryPolicy(),
				},
				{
					ID:          "gate",
					Type:        NodeTypeConditional,
					Condition:   "source contains yes",
					RetryPolicy: testNoRetryPolicy(),
					TrueBranch: &Node{
						ID:          "nested-gate",
						Type:        NodeTypeConditional,
						RetryPolicy: testNoRetryPolicy(),
						TrueBranch: &Node{
							ID:             "nested-branch-prompt",
							Type:           NodeTypePrompt,
							Model:          "mock-model",
							Prompt:         "Nested {{source}}",
							Temperature:    &temp,
							MaxTokens:      64,
							TimeoutSeconds: 30,
							RetryPolicy:    testNoRetryPolicy(),
						},
					},
				},
			},
			Edges: []*Edge{{ID: "source-gate", Source: "source", Target: "gate"}},
		}

		result := validator.Validate(wf)
		if result.Valid {
			t.Fatalf("expected nested conditional without condition to fail validation")
		}
		assertValidationErrorContains(t, result.Errors, "nested-gate", "condition", "Conditional node requires a condition expression")
	})
}

func assertValidationErrorContains(t *testing.T, errors []ValidationError, nodeID, field, messageContains string) {
	t.Helper()
	for _, err := range errors {
		if err.NodeID == nodeID && err.Field == field && strings.Contains(err.Message, messageContains) {
			return
		}
	}
	t.Fatalf("expected validation error node=%q field=%q containing %q, got %v", nodeID, field, messageContains, errors)
}

func TestDetectCycles(t *testing.T) {
	registry := providers.NewRegistry()
	mockProvider := NewMockProvider("test")
	registry.Register(mockProvider)
	validator := NewValidator(registry)

	tests := []struct {
		name        string
		nodes       []*Node
		edges       []*Edge
		expectValid bool
		// If true, check that at least one error has Field="edges" and Message contains "Circular dependency".
		expectCycleError bool
	}{
		{
			name: "no cycles",
			nodes: []*Node{
				{ID: "step1", Type: NodeTypePrompt, Model: "mock-model", Prompt: "test"},
				{ID: "step2", Type: NodeTypePrompt, Model: "mock-model", Prompt: "test"},
				{ID: "step3", Type: NodeTypePrompt, Model: "mock-model", Prompt: "test"},
			},
			edges: []*Edge{
				{ID: "e1", Source: "step1", Target: "step2"},
				{ID: "e2", Source: "step2", Target: "step3"},
			},
			expectValid: true,
		},
		{
			name: "simple cycle",
			nodes: []*Node{
				{ID: "step1", Type: NodeTypePrompt, Model: "mock-model", Prompt: "test"},
				{ID: "step2", Type: NodeTypePrompt, Model: "mock-model", Prompt: "test"},
			},
			edges: []*Edge{
				{ID: "e1", Source: "step1", Target: "step2"},
				{ID: "e2", Source: "step2", Target: "step1"}, // Cycle
			},
			expectValid:      false,
			expectCycleError: true,
		},
		{
			name: "self loop",
			nodes: []*Node{
				{ID: "step1", Type: NodeTypePrompt, Model: "mock-model", Prompt: "test"},
			},
			edges: []*Edge{
				{ID: "e1", Source: "step1", Target: "step1"}, // Self loop
			},
			expectValid: false,
		},
		{
			name: "complex cycle",
			nodes: []*Node{
				{ID: "step1", Type: NodeTypePrompt, Model: "mock-model", Prompt: "test"},
				{ID: "step2", Type: NodeTypePrompt, Model: "mock-model", Prompt: "test"},
				{ID: "step3", Type: NodeTypePrompt, Model: "mock-model", Prompt: "test"},
				{ID: "step4", Type: NodeTypePrompt, Model: "mock-model", Prompt: "test"},
			},
			edges: []*Edge{
				{ID: "e1", Source: "step1", Target: "step2"},
				{ID: "e2", Source: "step2", Target: "step3"},
				{ID: "e3", Source: "step3", Target: "step4"},
				{ID: "e4", Source: "step4", Target: "step2"}, // Cycle
			},
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf := &Workflow{
				ID:    "test",
				Nodes: tt.nodes,
				Edges: tt.edges,
			}
			if tt.expectValid {
				withStrictWorkflowDefaults(wf)
			}
			result := validator.Validate(wf)
			if result.Valid != tt.expectValid {
				if tt.expectValid {
					t.Errorf("Expected no cycles, got errors: %v", result.Errors)
				} else {
					t.Errorf("Expected validation to fail for %s", tt.name)
				}
			}
			if tt.expectCycleError {
				found := false
				for _, err := range result.Errors {
					if err.Field == "edges" && strings.Contains(err.Message, "Circular dependency") {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected cycle detection error, got: %v", result.Errors)
				}
			}
		})
	}
}

func TestValidateVariables(t *testing.T) {
	registry := providers.NewRegistry()
	mockProvider := NewMockProvider("test")
	registry.Register(mockProvider)
	validator := NewValidator(registry)

	tests := []struct {
		name        string
		workflow    *Workflow
		expectValid bool
		// If non-empty, at least one error Message must contain all these substrings.
		expectMsgContains []string
	}{
		{
			name: "valid variable from context",
			workflow: &Workflow{
				ID: "test",
				Context: map[string]interface{}{
					"user_input": "hello",
				},
				Nodes: []*Node{
					{ID: "step1", Type: NodeTypePrompt, Model: "mock-model", Prompt: "Process: {{user_input}}"},
				},
			},
			expectValid: true,
		},
		{
			name: "valid variable from previous node",
			workflow: &Workflow{
				ID: "test",
				Nodes: []*Node{
					{ID: "node1", Type: NodeTypePrompt, Model: "mock-model", Prompt: "First node"},
					{ID: "node2", Type: NodeTypePrompt, Model: "mock-model", Prompt: "Process: {{node1}}"},
				},
			},
			expectValid: true,
		},
		{
			name: "undefined variable",
			workflow: &Workflow{
				ID: "test",
				Nodes: []*Node{
					{ID: "step1", Type: NodeTypePrompt, Model: "mock-model", Prompt: "Process: {{nonexistent}}"},
				},
			},
			expectValid:       false,
			expectMsgContains: []string{"nonexistent", "not found"},
		},
		{
			name: "multiple variables",
			workflow: &Workflow{
				ID: "test",
				Context: map[string]interface{}{
					"var1": "value1",
				},
				Nodes: []*Node{
					{ID: "step1", Type: NodeTypePrompt, Model: "mock-model", Prompt: "First: {{var1}}"},
					{ID: "step2", Type: NodeTypePrompt, Model: "mock-model", Prompt: "Second: {{step1}} and {{var1}}"},
				},
			},
			expectValid: true,
		},
		{
			name: "condition variable validation",
			workflow: &Workflow{
				ID: "test",
				Nodes: []*Node{
					{ID: "step1", Type: NodeTypePrompt, Model: "mock-model", Prompt: "test"},
					{
						ID:        "step2",
						Type:      NodeTypeConditional,
						Condition: "nonexistent contains value",
						TrueBranch: &Node{
							Type:   NodeTypePrompt,
							Model:  "mock-model",
							Prompt: "true",
						},
					},
				},
			},
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf := tt.workflow
			if tt.expectValid {
				wf = withStrictWorkflowDefaults(wf)
			}
			result := validator.Validate(wf)
			if result.Valid != tt.expectValid {
				if tt.expectValid {
					t.Errorf("Expected validation to pass, got errors: %v", result.Errors)
				} else {
					t.Errorf("Expected validation to fail for %s", tt.name)
				}
			}
			if len(tt.expectMsgContains) > 0 {
				found := false
				for _, err := range result.Errors {
					allMatch := true
					for _, substr := range tt.expectMsgContains {
						if !strings.Contains(err.Message, substr) {
							allMatch = false
							break
						}
					}
					if allMatch {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected undefined variable error, got: %v", result.Errors)
				}
			}
		})
	}
}

func TestValidateModels(t *testing.T) {
	registry := providers.NewRegistry()
	// Add test models via mock provider
	mockProvider := NewMockProvider("test")
	mockProvider.models = []providers.Model{
		{
			ID:         "gpt-4",
			Name:       "GPT-4",
			Provider:   "test",
			InputCost:  0.03,
			OutputCost: 0.06,
		},
	}
	registry.Register(mockProvider)
	validator := NewValidator(registry)

	tests := []struct {
		name              string
		model             string
		expectValid       bool
		expectMsgContains []string
	}{
		{
			name:        "valid model",
			model:       "gpt-4",
			expectValid: true,
		},
		{
			name:              "invalid model",
			model:             "nonexistent-model",
			expectValid:       false,
			expectMsgContains: []string{"nonexistent-model", "not found"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf := &Workflow{
				ID: "test",
				Nodes: []*Node{
					{ID: "step1", Type: NodeTypePrompt, Model: tt.model, Prompt: "test"},
				},
			}
			if tt.expectValid {
				withStrictWorkflowDefaults(wf)
			}
			result := validator.Validate(wf)
			if result.Valid != tt.expectValid {
				if tt.expectValid {
					t.Errorf("Expected validation to pass, got errors: %v", result.Errors)
				} else {
					t.Error("Expected validation to fail for nonexistent model")
				}
			}
			if len(tt.expectMsgContains) > 0 {
				found := false
				for _, err := range result.Errors {
					allMatch := true
					for _, substr := range tt.expectMsgContains {
						if !strings.Contains(err.Message, substr) {
							allMatch = false
							break
						}
					}
					if allMatch {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected model not found error, got: %v", result.Errors)
				}
			}
		})
	}
}

func TestValidateConditions(t *testing.T) {
	registry := providers.NewRegistry()
	mockProvider := NewMockProvider("test")
	registry.Register(mockProvider)
	validator := NewValidator(registry)

	tests := []struct {
		name        string
		context     map[string]interface{}
		condition   string
		trueBranch  *Node
		expectValid bool
	}{
		{
			name:      "valid condition with contains",
			context:   map[string]interface{}{"result": "success"},
			condition: "result contains success",
			trueBranch: &Node{
				Type: NodeTypePrompt, Model: "mock-model", Prompt: "Success!",
			},
			expectValid: true,
		},
		{
			name:      "valid condition with equals",
			context:   map[string]interface{}{"status": "done"},
			condition: "status equals done",
			trueBranch: &Node{
				Type: NodeTypePrompt, Model: "mock-model", Prompt: "Done!",
			},
			expectValid: true,
		},
		{
			name:      "valid condition with not_empty",
			context:   map[string]interface{}{"value": "something"},
			condition: "value not_empty",
			trueBranch: &Node{
				Type: NodeTypePrompt, Model: "mock-model", Prompt: "Has value!",
			},
			expectValid: true,
		},
		{
			name:      "missing condition",
			condition: "",
			trueBranch: &Node{
				Type: NodeTypePrompt, Model: "mock-model", Prompt: "test",
			},
			expectValid: false,
		},
		{
			name:      "invalid condition format",
			condition: "invalid",
			trueBranch: &Node{
				Type: NodeTypePrompt, Model: "mock-model", Prompt: "test",
			},
			expectValid: false,
		},
		{
			name:      "unknown operator",
			context:   map[string]interface{}{"value": "test"},
			condition: "value unknown_op test",
			trueBranch: &Node{
				Type: NodeTypePrompt, Model: "mock-model", Prompt: "test",
			},
			expectValid: false,
		},
		{
			name:      "contains without value",
			context:   map[string]interface{}{"value": "test"},
			condition: "value contains",
			trueBranch: &Node{
				Type: NodeTypePrompt, Model: "mock-model", Prompt: "test",
			},
			expectValid: false,
		},
		{
			name:        "conditional without branches",
			context:     map[string]interface{}{"value": "test"},
			condition:   "value not_empty",
			trueBranch:  nil,
			expectValid: false,
		},
		{
			name:      "valid numeric greater than",
			context:   map[string]interface{}{"count": "15"},
			condition: "count > 10",
			trueBranch: &Node{
				Type: NodeTypePrompt, Model: "mock-model", Prompt: "High count",
			},
			expectValid: true,
		},
		{
			name:      "valid numeric less than or equal",
			context:   map[string]interface{}{"score": "0.3"},
			condition: "score <= 0.5",
			trueBranch: &Node{
				Type: NodeTypePrompt, Model: "mock-model", Prompt: "Low score",
			},
			expectValid: true,
		},
		{
			name:      "valid matches regex",
			context:   map[string]interface{}{"email": "test@example"},
			condition: "email matches ^[a-z]+@[a-z]+$",
			trueBranch: &Node{
				Type: NodeTypePrompt, Model: "mock-model", Prompt: "Valid email",
			},
			expectValid: true,
		},
		{
			name:      "invalid regex pattern",
			context:   map[string]interface{}{"value": "test"},
			condition: "value matches [invalid",
			trueBranch: &Node{
				Type: NodeTypePrompt, Model: "mock-model", Prompt: "test",
			},
			expectValid: false,
		},
		{
			name:      "valid is_number type check",
			context:   map[string]interface{}{"value": "42"},
			condition: "value is_number",
			trueBranch: &Node{
				Type: NodeTypePrompt, Model: "mock-model", Prompt: "Is numeric",
			},
			expectValid: true,
		},
		{
			name:      "valid is_empty type check",
			context:   map[string]interface{}{"value": ""},
			condition: "value is_empty",
			trueBranch: &Node{
				Type: NodeTypePrompt, Model: "mock-model", Prompt: "Is empty",
			},
			expectValid: true,
		},
		{
			name:      "valid AND compound condition",
			context:   map[string]interface{}{"a": "x", "b": "y"},
			condition: "a not_empty AND b not_empty",
			trueBranch: &Node{
				Type: NodeTypePrompt, Model: "mock-model", Prompt: "Both set",
			},
			expectValid: true,
		},
		{
			name:      "valid OR compound condition",
			context:   map[string]interface{}{"a": "x", "b": "y"},
			condition: "a not_empty OR b not_empty",
			trueBranch: &Node{
				Type: NodeTypePrompt, Model: "mock-model", Prompt: "At least one set",
			},
			expectValid: true,
		},
		{
			name:      "valid NOT prefix condition",
			context:   map[string]interface{}{"a": "x"},
			condition: "NOT a is_empty",
			trueBranch: &Node{
				Type: NodeTypePrompt, Model: "mock-model", Prompt: "Not empty",
			},
			expectValid: true,
		},
		{
			name:      "valid complex compound condition",
			context:   map[string]interface{}{"count": "15", "status": "active"},
			condition: "count > 10 AND status equals active",
			trueBranch: &Node{
				Type: NodeTypePrompt, Model: "mock-model", Prompt: "Active with high count",
			},
			expectValid: true,
		},
		{
			name:      "numeric operator without value",
			context:   map[string]interface{}{"count": "10"},
			condition: "count >",
			trueBranch: &Node{
				Type: NodeTypePrompt, Model: "mock-model", Prompt: "test",
			},
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf := &Workflow{
				ID:      "test",
				Context: tt.context,
				Nodes: []*Node{
					{
						ID:         "step1",
						Type:       NodeTypeConditional,
						Condition:  tt.condition,
						TrueBranch: tt.trueBranch,
					},
				},
			}
			if tt.expectValid {
				withStrictWorkflowDefaults(wf)
			}
			result := validator.Validate(wf)
			if result.Valid != tt.expectValid {
				if tt.expectValid {
					t.Errorf("Expected validation to pass, got errors: %v", result.Errors)
				} else {
					t.Errorf("Expected validation to fail for %s", tt.name)
				}
			}
		})
	}
}

func TestValidateEdges(t *testing.T) {
	registry := providers.NewRegistry()
	mockProvider := NewMockProvider("test")
	registry.Register(mockProvider)
	validator := NewValidator(registry)

	tests := []struct {
		name        string
		nodes       []*Node
		edges       []*Edge
		expectValid bool
	}{
		{
			name: "valid edges",
			nodes: []*Node{
				{ID: "step1", Type: NodeTypePrompt, Model: "mock-model", Prompt: "test"},
				{ID: "step2", Type: NodeTypePrompt, Model: "mock-model", Prompt: "test"},
			},
			edges: []*Edge{
				{ID: "e1", Source: "step1", Target: "step2"},
			},
			expectValid: true,
		},
		{
			name: "edge with nonexistent source",
			nodes: []*Node{
				{ID: "step1", Type: NodeTypePrompt, Model: "mock-model", Prompt: "test"},
			},
			edges: []*Edge{
				{ID: "e1", Source: "nonexistent", Target: "step1"},
			},
			expectValid: false,
		},
		{
			name: "edge with nonexistent target",
			nodes: []*Node{
				{ID: "step1", Type: NodeTypePrompt, Model: "mock-model", Prompt: "test"},
			},
			edges: []*Edge{
				{ID: "e1", Source: "step1", Target: "nonexistent"},
			},
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf := &Workflow{
				ID:    "test",
				Nodes: tt.nodes,
				Edges: tt.edges,
			}
			if tt.expectValid {
				withStrictWorkflowDefaults(wf)
			}
			result := validator.Validate(wf)
			if result.Valid != tt.expectValid {
				if tt.expectValid {
					t.Errorf("Expected validation to pass, got errors: %v", result.Errors)
				} else {
					t.Errorf("Expected validation to fail for %s", tt.name)
				}
			}
		})
	}
}

func TestValidateLimits(t *testing.T) {
	registry := providers.NewRegistry()
	mockProvider := NewMockProvider("test")
	registry.Register(mockProvider)
	validator := NewValidator(registry)

	tests := []struct {
		name        string
		limits      *CostLimits
		expectValid bool
		// If non-empty, at least one error must have this Field value.
		expectField string
	}{
		{
			name: "valid limits",
			limits: &CostLimits{
				MaxCostUSD: 10.0,
				MaxTokens:  10000,
			},
			expectValid: true,
		},
		{
			name:        "nil limits is valid",
			limits:      nil,
			expectValid: true,
		},
		{
			name: "negative cost limit",
			limits: &CostLimits{
				MaxCostUSD: -1.0,
			},
			expectValid: false,
			expectField: "limits.max_cost_usd",
		},
		{
			name: "negative token limit",
			limits: &CostLimits{
				MaxTokens: -100,
			},
			expectValid: false,
		},
		{
			name: "negative input token limit",
			limits: &CostLimits{
				MaxInputTokens: -50,
			},
			expectValid: false,
		},
		{
			name: "negative output token limit",
			limits: &CostLimits{
				MaxOutputTokens: -25,
			},
			expectValid: false,
		},
		{
			name: "very low cost limit warning",
			limits: &CostLimits{
				MaxCostUSD: 0.00001, // Very low
			},
			expectValid: false,
		},
		{
			name: "very low token limit warning",
			limits: &CostLimits{
				MaxTokens: 50, // Very low
			},
			expectValid: false,
		},
		{
			name: "zero limits are valid (means no limit)",
			limits: &CostLimits{
				MaxCostUSD:      0,
				MaxTokens:       0,
				MaxInputTokens:  0,
				MaxOutputTokens: 0,
			},
			expectValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf := &Workflow{
				ID: "test",
				Nodes: []*Node{
					{ID: "step1", Type: NodeTypePrompt, Model: "mock-model", Prompt: "test"},
				},
				Limits: tt.limits,
			}
			if tt.expectValid {
				withStrictWorkflowDefaults(wf)
			}
			result := validator.Validate(wf)
			if result.Valid != tt.expectValid {
				if tt.expectValid {
					t.Errorf("Expected validation to pass, got errors: %v", result.Errors)
				} else {
					t.Errorf("Expected validation to fail for %s", tt.name)
				}
			}
			if tt.expectField != "" {
				found := false
				for _, err := range result.Errors {
					if err.Field == tt.expectField {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected error for %s", tt.expectField)
				}
			}
		})
	}
}

func TestValidateReachability(t *testing.T) {
	registry := providers.NewRegistry()
	mockProvider := NewMockProvider("test")
	registry.Register(mockProvider)
	validator := NewValidator(registry)

	tests := []struct {
		name                string
		nodes               []*Node
		edges               []*Edge
		expectValid         bool
		expectReachWarnings int // number of reachability warnings expected
	}{
		{
			name: "unreachable node produces warning",
			// Create a disconnected cycle (c1 <-> c2) separate from the main chain.
			// c1 and c2 have incoming edges so they are not roots, and they are
			// unreachable from the only root ("root"). The cycle itself is caught
			// by detectCycles (making Valid=false), but validateReachability still
			// runs and appends its own warnings.
			nodes: []*Node{
				{ID: "root", Type: NodeTypePrompt, Model: "mock-model", Prompt: "start"},
				{ID: "connected", Type: NodeTypePrompt, Model: "mock-model", Prompt: "middle"},
				{ID: "c1", Type: NodeTypePrompt, Model: "mock-model", Prompt: "cycle1"},
				{ID: "c2", Type: NodeTypePrompt, Model: "mock-model", Prompt: "cycle2"},
			},
			edges: []*Edge{
				{ID: "e1", Source: "root", Target: "connected"},
				{ID: "e2", Source: "c1", Target: "c2"},
				{ID: "e3", Source: "c2", Target: "c1"},
			},
			// Valid=false because the cycle is detected, but reachability warnings
			// are still emitted for c1 and c2.
			expectValid:         false,
			expectReachWarnings: 2,
		},
		{
			name: "fully connected DAG has no reachability warnings",
			nodes: []*Node{
				{ID: "a", Type: NodeTypePrompt, Model: "mock-model", Prompt: "start"},
				{ID: "b", Type: NodeTypePrompt, Model: "mock-model", Prompt: "mid"},
				{ID: "c", Type: NodeTypePrompt, Model: "mock-model", Prompt: "end"},
			},
			edges: []*Edge{
				{ID: "e1", Source: "a", Target: "b"},
				{ID: "e2", Source: "b", Target: "c"},
			},
			expectValid:         true,
			expectReachWarnings: 0,
		},
		{
			name: "no edges skips reachability check",
			nodes: []*Node{
				{ID: "step1", Type: NodeTypePrompt, Model: "mock-model", Prompt: "first"},
				{ID: "step2", Type: NodeTypePrompt, Model: "mock-model", Prompt: "second"},
			},
			edges:               nil,
			expectValid:         true,
			expectReachWarnings: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf := &Workflow{
				ID:    "test",
				Nodes: tt.nodes,
				Edges: tt.edges,
			}
			if tt.expectValid {
				withStrictWorkflowDefaults(wf)
			}
			result := validator.Validate(wf)
			if result.Valid != tt.expectValid {
				if tt.expectValid {
					t.Errorf("Expected validation to pass, got errors: %v", result.Errors)
				} else {
					t.Errorf("Expected validation to fail for %s", tt.name)
				}
			}

			// Count reachability warnings (field="nodes", message contains "unreachable")
			reachWarnings := 0
			for _, err := range result.Errors {
				if err.Field == "nodes" && strings.Contains(err.Message, "unreachable") {
					reachWarnings++
				}
			}
			if reachWarnings != tt.expectReachWarnings {
				t.Errorf("Expected %d reachability warnings, got %d; errors: %v",
					tt.expectReachWarnings, reachWarnings, result.Errors)
			}
		})
	}
}

func TestValidateResultInputs(t *testing.T) {
	registry := providers.NewRegistry()
	mockProvider := NewMockProvider("test")
	registry.Register(mockProvider)
	validator := NewValidator(registry)

	tests := []struct {
		name           string
		nodes          []*Node
		expectValid    bool
		expectInputErr bool // expect at least one error with Field="input_ids"
	}{
		{
			name: "result node referencing non-existent input IDs produces error",
			nodes: []*Node{
				{ID: "step1", Type: NodeTypePrompt, Model: "mock-model", Prompt: "hello"},
				{
					ID:   "result1",
					Type: NodeTypeResult,
					Metadata: map[string]interface{}{
						"input_ids": []interface{}{"step1", "ghost_node"},
					},
				},
			},
			expectValid:    false,
			expectInputErr: true,
		},
		{
			name: "result node with valid input_ids is valid",
			nodes: []*Node{
				{ID: "step1", Type: NodeTypePrompt, Model: "mock-model", Prompt: "hello"},
				{ID: "step2", Type: NodeTypePrompt, Model: "mock-model", Prompt: "world"},
				{
					ID:   "result1",
					Type: NodeTypeResult,
					Metadata: map[string]interface{}{
						"input_ids": []interface{}{"step1", "step2"},
					},
				},
			},
			expectValid:    true,
			expectInputErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf := &Workflow{
				ID:    "test",
				Nodes: tt.nodes,
			}
			if tt.expectValid {
				withStrictWorkflowDefaults(wf)
			}
			result := validator.Validate(wf)
			if result.Valid != tt.expectValid {
				if tt.expectValid {
					t.Errorf("Expected validation to pass, got errors: %v", result.Errors)
				} else {
					t.Errorf("Expected validation to fail for %s", tt.name)
				}
			}

			hasInputErr := false
			for _, err := range result.Errors {
				if err.Field == "input_ids" {
					hasInputErr = true
					break
				}
			}
			if hasInputErr != tt.expectInputErr {
				t.Errorf("Expected input_ids error=%v, got=%v; errors: %v",
					tt.expectInputErr, hasInputErr, result.Errors)
			}
		})
	}
}

func TestValidateAggregationConfigsMajorityVote(t *testing.T) {
	registry := providers.NewRegistry()
	mockProvider := NewMockProvider("test")
	registry.Register(mockProvider)
	validator := NewValidator(registry)

	wfMissingTieBreaker := &Workflow{
		ID: "test-missing-tie-breaker",
		Nodes: []*Node{
			{ID: "step1", Type: NodeTypePrompt, Model: "mock-model", Prompt: "hello"},
			{
				ID:                "result1",
				Type:              NodeTypeResult,
				AggregationMethod: AggMethodMajorityVote,
				Metadata: map[string]interface{}{
					"input_ids": []interface{}{"step1"},
				},
			},
		},
	}
	withStrictNodeDefaults(wfMissingTieBreaker.Nodes[0])
	withStrictNodeDefaults(wfMissingTieBreaker.Nodes[1])
	if wfMissingTieBreaker.Nodes[1].AggregationConfig != nil {
		delete(wfMissingTieBreaker.Nodes[1].AggregationConfig, "tie_breaker_method")
	}
	result := validator.Validate(wfMissingTieBreaker)
	if result.Valid {
		t.Fatalf("Expected validation failure when tie_breaker_method is missing")
	}
	foundAggConfigErr := false
	for _, err := range result.Errors {
		if err.Field == "aggregation_config" && strings.Contains(err.Message, "tie_breaker_method") {
			foundAggConfigErr = true
			break
		}
	}
	if !foundAggConfigErr {
		t.Fatalf("Expected aggregation_config error mentioning tie_breaker_method, got: %v", result.Errors)
	}

	wfWithExplicitTieBreaker := &Workflow{
		ID: "test-explicit-tie-breaker",
		Nodes: []*Node{
			{ID: "step1", Type: NodeTypePrompt, Model: "mock-model", Prompt: "hello"},
			{
				ID:                "result1",
				Type:              NodeTypeResult,
				AggregationMethod: AggMethodMajorityVote,
				AggregationConfig: map[string]interface{}{
					"tie_breaker_method": "first",
				},
				Metadata: map[string]interface{}{
					"input_ids": []interface{}{"step1"},
				},
			},
		},
	}
	withStrictWorkflowDefaults(wfWithExplicitTieBreaker)
	result = validator.Validate(wfWithExplicitTieBreaker)
	if !result.Valid {
		t.Fatalf("Expected validation success with explicit tie_breaker_method, got errors: %v", result.Errors)
	}
}

func TestValidateAggregationConfigsRequireTokenFields(t *testing.T) {
	registry := providers.NewRegistry()
	mockProvider := NewMockProvider("test")
	registry.Register(mockProvider)
	validator := NewValidator(registry)

	tests := []struct {
		name               string
		method             AggregationMethod
		aggregationConfig  map[string]interface{}
		expectValid        bool
		expectErrorContain string
	}{
		{
			name:               "judge requires prompt",
			method:             AggMethodJudge,
			aggregationConfig:  withJudgeConfig(map[string]interface{}{"prompt": ""}),
			expectValid:        false,
			expectErrorContain: "judge requires prompt",
		},
		{
			name:               "judge requires max_tokens",
			method:             AggMethodJudge,
			aggregationConfig:  withJudgeConfig(map[string]interface{}{"max_tokens": nil, "repair_max_tokens": 256}),
			expectValid:        false,
			expectErrorContain: "judge requires max_tokens",
		},
		{
			name:               "judge requires repair_max_tokens",
			method:             AggMethodJudge,
			aggregationConfig:  withJudgeConfig(map[string]interface{}{"repair_max_tokens": nil, "max_tokens": -1}),
			expectValid:        false,
			expectErrorContain: "judge requires repair_max_tokens",
		},
		{
			name:               "debate_decide requires prompt",
			method:             AggMethodDebateDecide,
			aggregationConfig:  withDebateDecideConfig(map[string]interface{}{"prompt": ""}),
			expectValid:        false,
			expectErrorContain: "debate_decide requires prompt",
		},
		{
			name:               "debate_decide requires repair_max_tokens",
			method:             AggMethodDebateDecide,
			aggregationConfig:  withDebateDecideConfig(map[string]interface{}{"repair_max_tokens": nil, "max_tokens": -1}),
			expectValid:        false,
			expectErrorContain: "debate_decide requires repair_max_tokens",
		},
		{
			name:               "scoring requires prompt",
			method:             AggMethodScoring,
			aggregationConfig:  withScoringConfig(map[string]interface{}{"prompt": ""}),
			expectValid:        false,
			expectErrorContain: "scoring requires prompt",
		},
		{
			name:               "scoring requires max_tokens",
			method:             AggMethodScoring,
			aggregationConfig:  withScoringConfig(map[string]interface{}{"max_tokens": nil}),
			expectValid:        false,
			expectErrorContain: "scoring requires max_tokens",
		},
		{
			name:               "synthesis requires prompt",
			method:             AggMethodSynthesis,
			aggregationConfig:  withSynthesisConfig(map[string]interface{}{"prompt": ""}),
			expectValid:        false,
			expectErrorContain: "synthesis requires prompt",
		},
		{
			name:               "synthesis requires max_tokens",
			method:             AggMethodSynthesis,
			aggregationConfig:  withSynthesisConfig(map[string]interface{}{"max_tokens": nil}),
			expectValid:        false,
			expectErrorContain: "synthesis requires max_tokens",
		},
		{
			name:               "peer_matrix requires eval_prompt",
			method:             AggMethodPeerMatrix,
			aggregationConfig:  withPeerMatrixConfig(map[string]interface{}{"eval_prompt": ""}),
			expectValid:        false,
			expectErrorContain: "peer_matrix requires eval_prompt",
		},
		{
			name:              "judge accepts max_tokens and repair_max_tokens",
			method:            AggMethodJudge,
			aggregationConfig: withJudgeConfig(map[string]interface{}{"max_tokens": -1, "repair_max_tokens": 256}),
			expectValid:       true,
		},
		{
			name:              "debate_decide accepts max_tokens and repair_max_tokens",
			method:            AggMethodDebateDecide,
			aggregationConfig: withDebateDecideConfig(map[string]interface{}{"max_tokens": -1, "repair_max_tokens": 256}),
			expectValid:       true,
		},
		{
			name:              "synthesis accepts max_tokens",
			method:            AggMethodSynthesis,
			aggregationConfig: withSynthesisConfig(map[string]interface{}{"max_tokens": -1}),
			expectValid:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf := &Workflow{
				ID: "aggregation-token-validation",
				Nodes: []*Node{
					{
						ID:             "step1",
						Type:           NodeTypePrompt,
						Model:          "mock-model",
						Prompt:         "a",
						Temperature:    providers.Float64Ptr(0.7),
						MaxTokens:      100,
						TimeoutSeconds: 120,
						RetryPolicy:    DefaultRetryPolicy(),
					},
					{
						ID:             "step2",
						Type:           NodeTypePrompt,
						Model:          "mock-model",
						Prompt:         "b",
						Temperature:    providers.Float64Ptr(0.7),
						MaxTokens:      100,
						TimeoutSeconds: 120,
						RetryPolicy:    DefaultRetryPolicy(),
					},
					{
						ID:                "result1",
						Type:              NodeTypeResult,
						AggregationMethod: tt.method,
						AggregationConfig: tt.aggregationConfig,
						RetryPolicy:       NoRetryPolicy(),
						Metadata: map[string]interface{}{
							"input_ids": []interface{}{"step1", "step2"},
						},
					},
				},
			}

			result := validator.Validate(wf)
			if result.Valid != tt.expectValid {
				t.Fatalf("Expected valid=%v, got valid=%v errors=%v", tt.expectValid, result.Valid, result.Errors)
			}
			if tt.expectErrorContain == "" {
				return
			}
			found := false
			for _, err := range result.Errors {
				if strings.Contains(err.Message, tt.expectErrorContain) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("Expected error containing %q, got %v", tt.expectErrorContain, result.Errors)
			}
		})
	}
}

func TestValidateAggregationConfigsPeerMatrix(t *testing.T) {
	registry := providers.NewRegistry()
	mockProvider := NewMockProvider("test")
	registry.Register(mockProvider)
	validator := NewValidator(registry)

	tests := []struct {
		name               string
		aggregationConfig  map[string]interface{}
		step2Model         string
		expectValid        bool
		expectErrorContain string
	}{
		{
			name:               "rejects judge_model in peer_matrix config",
			aggregationConfig:  withPeerMatrixConfig(map[string]interface{}{"judge_model": "openai/gpt-4o-mini"}),
			step2Model:         "mock-model",
			expectValid:        false,
			expectErrorContain: "does not support judge_model",
		},
		{
			name:               "requires rubric_model in dynamic rubric mode",
			aggregationConfig:  withPeerMatrixConfig(map[string]interface{}{"rubric_mode": "dynamic"}),
			step2Model:         "mock-model",
			expectValid:        false,
			expectErrorContain: "requires rubric_model",
		},
		{
			name:               "requires model on every peer_matrix input node",
			aggregationConfig:  withPeerMatrixConfig(map[string]interface{}{}),
			step2Model:         "",
			expectValid:        false,
			expectErrorContain: "must set model for per-agent evaluator routing",
		},
		{
			name: "accepts valid per-model routing config",
			aggregationConfig: withPeerMatrixConfig(map[string]interface{}{
				"eval_prompt":  "review {{rubric}}",
				"rubric_mode":  "dynamic",
				"rubric_model": "z-ai/glm-5",
			}),
			step2Model:  "mock-model",
			expectValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wf := &Workflow{
				ID: "peer-matrix-validation",
				Nodes: []*Node{
					{
						ID:             "step1",
						Type:           NodeTypePrompt,
						Model:          "mock-model",
						Prompt:         "a",
						Temperature:    providers.Float64Ptr(0.7),
						MaxTokens:      100,
						TimeoutSeconds: 120,
						RetryPolicy:    DefaultRetryPolicy(),
					},
					{
						ID:             "step2",
						Type:           NodeTypePrompt,
						Model:          tt.step2Model,
						Prompt:         "b",
						Temperature:    providers.Float64Ptr(0.7),
						MaxTokens:      100,
						TimeoutSeconds: 120,
						RetryPolicy:    DefaultRetryPolicy(),
					},
					{
						ID:                "result1",
						Type:              NodeTypeResult,
						AggregationMethod: AggMethodPeerMatrix,
						AggregationConfig: tt.aggregationConfig,
						RetryPolicy:       NoRetryPolicy(),
						Metadata: map[string]interface{}{
							"input_ids": []interface{}{"step1", "step2"},
						},
					},
				},
			}
			result := validator.Validate(wf)
			if result.Valid != tt.expectValid {
				t.Fatalf("Expected valid=%v, got valid=%v errors=%v", tt.expectValid, result.Valid, result.Errors)
			}
			if tt.expectErrorContain == "" {
				return
			}
			found := false
			for _, err := range result.Errors {
				if strings.Contains(err.Message, tt.expectErrorContain) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("Expected error containing %q, got %v", tt.expectErrorContain, result.Errors)
			}
		})
	}
}

func TestValidateVariables_ContractExtractSourceVariableReference(t *testing.T) {
	registry := providers.NewRegistry()
	mockProvider := NewMockProvider("test")
	registry.Register(mockProvider)
	validator := NewValidator(registry)

	wf := &Workflow{
		ID: "test",
		Nodes: []*Node{
			{
				ID:     "contract",
				Type:   NodeTypeContractExtract,
				Model:  "mock-model",
				Prompt: "Extract answer",
				Metadata: map[string]interface{}{
					"source_variable": "missing_source",
				},
			},
		},
		Context: map[string]interface{}{},
	}

	result := validator.Validate(wf)
	if result.Valid {
		t.Fatalf("expected validation error for missing source_variable reference")
	}

	found := false
	for _, err := range result.Errors {
		if err.Field == "source_variable" &&
			strings.Contains(err.Message, "Source variable 'missing_source' not found in context or previous nodes") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected source_variable reference validation error, got: %+v", result.Errors)
	}
}
