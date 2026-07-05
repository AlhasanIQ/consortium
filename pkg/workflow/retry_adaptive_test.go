package workflow

import "testing"

func TestEffectiveNodeForAttempt_AppliesReasoningOverride(t *testing.T) {
	node := &Node{
		ID:   "n1",
		Type: NodeTypePrompt,
		Metadata: map[string]interface{}{
			"openrouter_reasoning": map[string]interface{}{"effort": "high"},
		},
	}
	policy := DefaultRetryPolicy()

	effective, effort, applied := EffectiveNodeForAttempt(node, policy, 2)
	if !applied {
		t.Fatalf("expected adaptive reasoning override to apply")
	}
	if effort != "medium" {
		t.Fatalf("expected effective effort medium, got %s", effort)
	}
	if effective == node {
		t.Fatalf("expected cloned node when override applied")
	}
	reasoning := parseOpenRouterReasoningConfig(effective.Metadata)
	if reasoning == nil || reasoning.Effort != "medium" {
		t.Fatalf("expected effective node metadata effort=medium, got %#v", effective.Metadata["openrouter_reasoning"])
	}
}

func TestEffectiveNodeForAttempt_NoOverrideWhenNotTriggered(t *testing.T) {
	node := &Node{
		ID:   "n1",
		Type: NodeTypePrompt,
		Metadata: map[string]interface{}{
			"openrouter_reasoning": map[string]interface{}{"effort": "high"},
		},
	}
	policy := DefaultRetryPolicy()

	effective, _, applied := EffectiveNodeForAttempt(node, policy, 1)
	if applied {
		t.Fatalf("did not expect adaptive override below activation threshold")
	}
	if effective != node {
		t.Fatalf("expected original node pointer when override not applied")
	}
}
