package workflow

import (
	"context"
	"testing"

	"github.com/alhasaniq/consortium/pkg/providers"
)

func TestValidateVariablesUsesEffectiveGraphAncestry(t *testing.T) {
	validator := NewValidator(nil)

	t.Run("accepts producer listed after consumer", func(t *testing.T) {
		wf := withStrictWorkflowDefaults(&Workflow{
			ID: "later-producer",
			Nodes: []*Node{
				ancestryPromptNode("consumer", "Consume {{producer}}"),
				ancestryPromptNode("producer", "Produce"),
			},
			Edges: []*Edge{{ID: "producer-consumer", Source: "producer", Target: "consumer"}},
		})

		result := validator.Validate(wf)
		if !result.Valid {
			t.Fatalf("producer ancestor was rejected because of node list order: %+v", result.Errors)
		}
	})

	t.Run("accepts transitive ancestor", func(t *testing.T) {
		wf := withStrictWorkflowDefaults(&Workflow{
			ID: "transitive-ancestor",
			Nodes: []*Node{
				ancestryPromptNode("consumer", "Consume {{producer}}"),
				ancestryPromptNode("middle", "Middle"),
				ancestryPromptNode("producer", "Produce"),
			},
			Edges: []*Edge{
				{ID: "producer-middle", Source: "producer", Target: "middle"},
				{ID: "middle-consumer", Source: "middle", Target: "consumer"},
			},
		})

		result := validator.Validate(wf)
		if !result.Valid {
			t.Fatalf("transitive producer ancestor was rejected: %+v", result.Errors)
		}
	})

	t.Run("rejects earlier node without dependency path", func(t *testing.T) {
		wf := withStrictWorkflowDefaults(&Workflow{
			ID: "parallel-reference",
			Nodes: []*Node{
				ancestryPromptNode("producer", "Produce"),
				ancestryPromptNode("consumer", "Consume {{producer}}"),
				ancestryPromptNode("sink", "Join"),
			},
			Edges: []*Edge{
				{ID: "producer-sink", Source: "producer", Target: "sink"},
				{ID: "consumer-sink", Source: "consumer", Target: "sink"},
			},
		})

		result := validator.Validate(wf)
		if result.Valid {
			t.Fatal("reference to a concurrently runnable node unexpectedly validated")
		}
		assertValidationErrorContains(t, result.Errors, "consumer", "prompt", "dependency ancestors")
	})
}

func TestValidateVariablesPreservesImplicitSequentialAncestry(t *testing.T) {
	validator := NewValidator(nil)

	t.Run("previous node remains available", func(t *testing.T) {
		wf := withStrictWorkflowDefaults(&Workflow{
			ID: "implicit-forward",
			Nodes: []*Node{
				ancestryPromptNode("producer", "Produce"),
				ancestryPromptNode("consumer", "Consume {{producer}}"),
			},
		})

		result := validator.Validate(wf)
		if !result.Valid {
			t.Fatalf("implicit sequential ancestor was rejected: %+v", result.Errors)
		}
	})

	t.Run("later node is not yet available", func(t *testing.T) {
		wf := withStrictWorkflowDefaults(&Workflow{
			ID: "implicit-backward",
			Nodes: []*Node{
				ancestryPromptNode("consumer", "Consume {{producer}}"),
				ancestryPromptNode("producer", "Produce"),
			},
		})

		result := validator.Validate(wf)
		if result.Valid {
			t.Fatal("reference to a later implicit-sequence node unexpectedly validated")
		}
		assertValidationErrorContains(t, result.Errors, "consumer", "prompt", "dependency ancestors")
	})
}

func TestValidateBranchVariablesUseEnclosingNodeAncestors(t *testing.T) {
	validator := NewValidator(nil)

	t.Run("nested branches inherit transitive top-level ancestors", func(t *testing.T) {
		wf := withStrictWorkflowDefaults(&Workflow{
			ID: "branch-ancestors",
			Nodes: []*Node{
				{
					ID:        "gate",
					Type:      NodeTypeConditional,
					Condition: "producer contains ready",
					TrueBranch: &Node{
						ID:         "nested-gate",
						Type:       NodeTypeConditional,
						Condition:  "producer not_empty",
						TrueBranch: ancestryPromptNode("nested-consumer", "Use {{producer}}"),
					},
				},
				ancestryPromptNode("middle", "Middle"),
				ancestryPromptNode("producer", "Produce"),
			},
			Edges: []*Edge{
				{ID: "producer-middle", Source: "producer", Target: "middle"},
				{ID: "middle-gate", Source: "middle", Target: "gate"},
			},
		})

		result := validator.Validate(wf)
		if !result.Valid {
			t.Fatalf("nested branch rejected an enclosing DAG ancestor: %+v", result.Errors)
		}
	})

	t.Run("branch rejects unrelated earlier top-level node", func(t *testing.T) {
		wf := withStrictWorkflowDefaults(&Workflow{
			ID: "branch-unrelated",
			Nodes: []*Node{
				ancestryPromptNode("unrelated", "Unrelated"),
				{
					ID:         "gate",
					Type:       NodeTypeConditional,
					Condition:  "producer contains ready",
					TrueBranch: ancestryPromptNode("branch-consumer", "Use {{unrelated}}"),
				},
				ancestryPromptNode("producer", "Produce"),
			},
			Edges: []*Edge{{ID: "producer-gate", Source: "producer", Target: "gate"}},
		})

		result := validator.Validate(wf)
		if result.Valid {
			t.Fatal("branch reference to unrelated top-level node unexpectedly validated")
		}
		assertValidationErrorContains(t, result.Errors, "branch-consumer", "prompt", "dependency ancestors")
	})

	t.Run("branch cannot reference enclosing conditional output before it exists", func(t *testing.T) {
		wf := withStrictWorkflowDefaults(&Workflow{
			ID: "branch-parent-output",
			Nodes: []*Node{
				ancestryPromptNode("producer", "Produce"),
				{
					ID:         "gate",
					Type:       NodeTypeConditional,
					Condition:  "producer contains ready",
					TrueBranch: ancestryPromptNode("branch-consumer", "Use {{gate}}"),
				},
			},
			Edges: []*Edge{{ID: "producer-gate", Source: "producer", Target: "gate"}},
		})

		result := validator.Validate(wf)
		if result.Valid {
			t.Fatal("branch reference to its not-yet-produced conditional output unexpectedly validated")
		}
		assertValidationErrorContains(t, result.Errors, "branch-consumer", "prompt", "dependency ancestors")
	})
}

func TestGraphAncestryValidationMatchesExecution(t *testing.T) {
	registry := providers.NewRegistry()
	provider := NewMockProvider("ancestry")
	provider.SetResponse("produce ancestor", "ancestor-value")
	provider.SetResponse("middle ancestor-value", "middle-value")
	provider.SetResponse("consume ancestor-value", "consumer-saw-ancestor")
	registry.Register(provider)

	wf := withStrictWorkflowDefaults(&Workflow{
		ID: "ancestry-execution-contract",
		Nodes: []*Node{
			ancestryPromptNode("consumer", "consume {{producer}}"),
			ancestryPromptNode("middle", "middle {{producer}}"),
			ancestryPromptNode("producer", "produce ancestor"),
		},
		Edges: []*Edge{
			{ID: "producer-middle", Source: "producer", Target: "middle"},
			{ID: "middle-consumer", Source: "middle", Target: "consumer"},
		},
	})

	validation := NewValidator(registry).Validate(wf)
	if !validation.Valid {
		t.Fatalf("runtime-safe transitive ancestry failed validation: %+v", validation.Errors)
	}

	result, err := NewExecutorWithRegistry(registry).Execute(context.Background(), wf, nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("execution failed: %+v", result)
	}
	if got := result.Context["consumer"]; got != "consumer-saw-ancestor" {
		t.Fatalf("consumer output = %#v, want proof that transitive producer output was available", got)
	}
}

func ancestryPromptNode(id, prompt string) *Node {
	return &Node{
		ID:     id,
		Type:   NodeTypePrompt,
		Model:  "mock-model",
		Prompt: prompt,
	}
}
