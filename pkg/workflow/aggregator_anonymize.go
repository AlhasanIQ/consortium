package workflow

// AnonymizationMap maps letter labels to real agent IDs and back.
// Used to present LLM-facing prompts without model names to eliminate name bias.
type AnonymizationMap struct {
	LabelToID map[string]string // "A" -> "agent-grok"
	IDToLabel map[string]string // "agent-grok" -> "A"
	Labels    []string          // ["A", "B", "C"] in input order
}

// BuildAnonymizationMap creates a deterministic letter-label mapping for agents.
// Labels follow input slice order: first agent = A, second = B, etc.
func BuildAnonymizationMap(agentIDs []string) *AnonymizationMap {
	m := &AnonymizationMap{
		LabelToID: make(map[string]string, len(agentIDs)),
		IDToLabel: make(map[string]string, len(agentIDs)),
		Labels:    make([]string, len(agentIDs)),
	}
	for i, id := range agentIDs {
		label := CandidateLabel(i)
		m.LabelToID[label] = id
		m.IDToLabel[id] = label
		m.Labels[i] = label
	}
	return m
}

// ResolveLabel maps a letter label back to a real agent ID.
// Returns empty string if label is not found.
func (m *AnonymizationMap) ResolveLabel(label string) string {
	return m.LabelToID[label]
}
