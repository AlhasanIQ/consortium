package workflow

import (
	"fmt"
	"reflect"
)

// FieldDiff represents a single changed field between two workflow configs.
type FieldDiff struct {
	Path     string `json:"path"`     // e.g. "nodes.claude-1.temperature"
	Left     any    `json:"left"`     // value in run A (nil if absent)
	Right    any    `json:"right"`    // value in run B (nil if absent)
	Category string `json:"category"` // "structure", "model", "prompt", "decoding", "aggregation", "limits"
}

// ConfigDiff represents all differences between two workflow configurations.
type ConfigDiff struct {
	LeftHash  string      `json:"left_hash"`
	RightHash string      `json:"right_hash"`
	Identical bool        `json:"identical"`
	Diffs     []FieldDiff `json:"diffs"`
}

// DiffWorkflows compares two workflow definitions and returns categorized field diffs.
// Nodes are matched by node_id. Missing explicit values are treated as distinct
// from explicitly set values.
func DiffWorkflows(a, b *Workflow) *ConfigDiff {
	na, hashA := normalizeAndHash(a)
	nb, hashB := normalizeAndHash(b)

	diff := &ConfigDiff{
		LeftHash:  hashA,
		RightHash: hashB,
	}

	if hashA == hashB {
		diff.Identical = true
		return diff
	}

	// Build node maps for matching
	nodesA := make(map[string]normalizedNode)
	for _, s := range na.Nodes {
		nodesA[s.NodeID] = s
	}
	nodesB := make(map[string]normalizedNode)
	for _, s := range nb.Nodes {
		nodesB[s.NodeID] = s
	}

	// Compare matched nodes
	seen := make(map[string]bool)
	for id, sa := range nodesA {
		seen[id] = true
		sb, ok := nodesB[id]
		if !ok {
			diff.Diffs = append(diff.Diffs, FieldDiff{
				Path:     fmt.Sprintf("nodes.%s", id),
				Left:     sa.NodeType,
				Right:    nil,
				Category: "structure",
			})
			continue
		}
		diff.Diffs = append(diff.Diffs, diffNodes(id, sa, sb)...)
	}

	// Nodes only in B
	for id, sb := range nodesB {
		if seen[id] {
			continue
		}
		diff.Diffs = append(diff.Diffs, FieldDiff{
			Path:     fmt.Sprintf("nodes.%s", id),
			Left:     nil,
			Right:    sb.NodeType,
			Category: "structure",
		})
	}

	// Compare edges
	diff.Diffs = append(diff.Diffs, diffEdges(na.Edges, nb.Edges)...)

	// Compare limits
	diff.Diffs = append(diff.Diffs, diffLimits(na.Limits, nb.Limits)...)

	return diff
}

// diffNodes compares two normalized nodes field-by-field.
func diffNodes(nodeID string, a, b normalizedNode) []FieldDiff {
	var diffs []FieldDiff
	prefix := fmt.Sprintf("nodes.%s", nodeID)

	if a.NodeType != b.NodeType {
		diffs = append(diffs, FieldDiff{
			Path: prefix + ".node_type", Left: a.NodeType, Right: b.NodeType, Category: "structure",
		})
	}
	if a.Model != b.Model {
		diffs = append(diffs, FieldDiff{
			Path: prefix + ".model", Left: a.Model, Right: b.Model, Category: "model",
		})
	}
	if a.Prompt != b.Prompt {
		diffs = append(diffs, FieldDiff{
			Path: prefix + ".prompt", Left: a.Prompt, Right: b.Prompt, Category: "prompt",
		})
	}
	if a.SystemPrompt != b.SystemPrompt {
		diffs = append(diffs, FieldDiff{
			Path: prefix + ".system_prompt", Left: a.SystemPrompt, Right: b.SystemPrompt, Category: "prompt",
		})
	}
	if a.TemperatureSet != b.TemperatureSet || a.Temperature != b.Temperature {
		var left, right any
		if a.TemperatureSet {
			left = a.Temperature
		}
		if b.TemperatureSet {
			right = b.Temperature
		}
		diffs = append(diffs, FieldDiff{
			Path: prefix + ".temperature", Left: left, Right: right, Category: "decoding",
		})
	}
	if a.MaxTokensSet != b.MaxTokensSet || a.MaxTokens != b.MaxTokens {
		var left, right any
		if a.MaxTokensSet {
			left = a.MaxTokens
		}
		if b.MaxTokensSet {
			right = b.MaxTokens
		}
		diffs = append(diffs, FieldDiff{
			Path: prefix + ".max_tokens", Left: left, Right: right, Category: "decoding",
		})
	}
	if a.TimeoutSecondsSet != b.TimeoutSecondsSet || a.TimeoutSeconds != b.TimeoutSeconds {
		var left, right any
		if a.TimeoutSecondsSet {
			left = a.TimeoutSeconds
		}
		if b.TimeoutSecondsSet {
			right = b.TimeoutSeconds
		}
		diffs = append(diffs, FieldDiff{
			Path: prefix + ".timeout_seconds", Left: left, Right: right, Category: "decoding",
		})
	}
	if a.Condition != b.Condition {
		diffs = append(diffs, FieldDiff{
			Path: prefix + ".condition", Left: a.Condition, Right: b.Condition, Category: "structure",
		})
	}
	if a.OperationType != b.OperationType {
		diffs = append(diffs, FieldDiff{
			Path: prefix + ".operation_type", Left: a.OperationType, Right: b.OperationType, Category: "operation",
		})
	}
	if !reflect.DeepEqual(a.OperationConfig, b.OperationConfig) {
		diffs = append(diffs, FieldDiff{
			Path: prefix + ".operation_config", Left: a.OperationConfig, Right: b.OperationConfig, Category: "operation",
		})
	}
	if a.AggregationMethod != b.AggregationMethod {
		diffs = append(diffs, FieldDiff{
			Path: prefix + ".aggregation_method", Left: a.AggregationMethod, Right: b.AggregationMethod, Category: "aggregation",
		})
	}
	if a.SourceWorkflowHash != b.SourceWorkflowHash {
		diffs = append(diffs, FieldDiff{
			Path: prefix + ".source_workflow_hash", Left: a.SourceWorkflowHash, Right: b.SourceWorkflowHash, Category: "composition",
		})
	}

	return diffs
}

// diffEdges compares two sorted edge lists.
func diffEdges(a, b []normalizedEdge) []FieldDiff {
	var diffs []FieldDiff

	setA := make(map[string]bool)
	for _, e := range a {
		setA[e.Source+"→"+e.Target] = true
	}
	setB := make(map[string]bool)
	for _, e := range b {
		setB[e.Source+"→"+e.Target] = true
	}

	for key := range setA {
		if !setB[key] {
			diffs = append(diffs, FieldDiff{
				Path: "edges." + key, Left: key, Right: nil, Category: "structure",
			})
		}
	}
	for key := range setB {
		if !setA[key] {
			diffs = append(diffs, FieldDiff{
				Path: "edges." + key, Left: nil, Right: key, Category: "structure",
			})
		}
	}

	return diffs
}

// diffLimits compares cost limits between two configs.
func diffLimits(a, b *CostLimits) []FieldDiff {
	var diffs []FieldDiff

	// Normalize nil to zero-value
	if a == nil {
		a = &CostLimits{}
	}
	if b == nil {
		b = &CostLimits{}
	}

	if a.MaxCostUSD != b.MaxCostUSD {
		diffs = append(diffs, FieldDiff{
			Path: "limits.max_cost_usd", Left: a.MaxCostUSD, Right: b.MaxCostUSD, Category: "limits",
		})
	}
	if a.MaxTokens != b.MaxTokens {
		diffs = append(diffs, FieldDiff{
			Path: "limits.max_tokens", Left: a.MaxTokens, Right: b.MaxTokens, Category: "limits",
		})
	}
	if a.MaxInputTokens != b.MaxInputTokens {
		diffs = append(diffs, FieldDiff{
			Path: "limits.max_input_tokens", Left: a.MaxInputTokens, Right: b.MaxInputTokens, Category: "limits",
		})
	}
	if a.MaxOutputTokens != b.MaxOutputTokens {
		diffs = append(diffs, FieldDiff{
			Path: "limits.max_output_tokens", Left: a.MaxOutputTokens, Right: b.MaxOutputTokens, Category: "limits",
		})
	}

	return diffs
}
