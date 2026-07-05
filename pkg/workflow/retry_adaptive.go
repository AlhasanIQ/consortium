package workflow

import "strings"

// NodeReasoningEffort returns the configured OpenRouter reasoning effort for a
// node. Empty/missing config defaults to "none".
func NodeReasoningEffort(node *Node) string {
	if node == nil {
		return "none"
	}
	reasoning := parseOpenRouterReasoningConfig(node.Metadata)
	if reasoning == nil {
		return "none"
	}
	effort := strings.ToLower(strings.TrimSpace(reasoning.Effort))
	if effort == "" {
		return "none"
	}
	return effort
}

// EffectiveNodeForAttempt applies adaptive reasoning overrides (if configured)
// and returns the node used for the current attempt.
func EffectiveNodeForAttempt(node *Node, policy *RetryPolicy, consecutiveTriggerFailures int) (*Node, string, bool) {
	baseEffort := NodeReasoningEffort(node)
	if policy == nil {
		return node, baseEffort, false
	}
	overrideEffort, ok := policy.AdaptiveReasoningEffort(baseEffort, consecutiveTriggerFailures)
	if !ok {
		return node, baseEffort, false
	}
	return cloneNodeWithReasoningEffort(node, overrideEffort), overrideEffort, true
}

func cloneNodeWithReasoningEffort(node *Node, effort string) *Node {
	if node == nil {
		return nil
	}
	clone := *node

	metaCopy := make(map[string]interface{}, len(node.Metadata)+1)
	for k, v := range node.Metadata {
		metaCopy[k] = v
	}
	metaCopy["openrouter_reasoning"] = map[string]interface{}{"effort": effort}
	if _, ok := node.Metadata["openRouterReasoning"]; ok {
		metaCopy["openRouterReasoning"] = map[string]interface{}{"effort": effort}
	}
	clone.Metadata = metaCopy
	return &clone
}
