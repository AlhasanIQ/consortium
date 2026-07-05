package workflow

import "testing"

func TestBuildAnonymizationMap(t *testing.T) {
	ids := []string{"agent-grok", "agent-mimo", "agent-gemini"}
	m := BuildAnonymizationMap(ids)

	// Labels follow input order
	if m.Labels[0] != "A" || m.Labels[1] != "B" || m.Labels[2] != "C" {
		t.Errorf("Expected labels [A B C], got %v", m.Labels)
	}

	// Forward mapping
	if m.LabelToID["A"] != "agent-grok" {
		t.Errorf("Expected A -> agent-grok, got %q", m.LabelToID["A"])
	}
	if m.LabelToID["B"] != "agent-mimo" {
		t.Errorf("Expected B -> agent-mimo, got %q", m.LabelToID["B"])
	}
	if m.LabelToID["C"] != "agent-gemini" {
		t.Errorf("Expected C -> agent-gemini, got %q", m.LabelToID["C"])
	}

	// Reverse mapping
	if m.IDToLabel["agent-grok"] != "A" {
		t.Errorf("Expected agent-grok -> A, got %q", m.IDToLabel["agent-grok"])
	}

	// ResolveLabel
	if m.ResolveLabel("B") != "agent-mimo" {
		t.Errorf("Expected ResolveLabel(B) = agent-mimo, got %q", m.ResolveLabel("B"))
	}
	if m.ResolveLabel("Z") != "" {
		t.Errorf("Expected ResolveLabel(Z) = empty, got %q", m.ResolveLabel("Z"))
	}
}

func TestBuildAnonymizationMapSingle(t *testing.T) {
	m := BuildAnonymizationMap([]string{"only-agent"})

	if len(m.Labels) != 1 || m.Labels[0] != "A" {
		t.Errorf("Expected single label [A], got %v", m.Labels)
	}
	if m.ResolveLabel("A") != "only-agent" {
		t.Errorf("Expected A -> only-agent, got %q", m.ResolveLabel("A"))
	}
}
