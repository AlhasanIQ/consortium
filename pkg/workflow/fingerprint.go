package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// normalizedNode is a canonical representation of a node's config for hashing.
// Only execution-relevant fields are included. Cosmetic metadata is excluded.
type normalizedNode struct {
	NodeID                  string                   `json:"node_id"`
	NodeType                string                   `json:"node_type"`
	Model                   string                   `json:"model"`
	Prompt                  string                   `json:"prompt"`
	Harness                 string                   `json:"harness,omitempty"`
	SystemPrompt            string                   `json:"system_prompt"`
	Temperature             float64                  `json:"temperature"`
	TemperatureSet          bool                     `json:"temperature_set"`
	MaxTokens               int                      `json:"max_tokens"`
	MaxTokensSet            bool                     `json:"max_tokens_set"`
	TimeoutSeconds          int                      `json:"timeout_seconds"`
	TimeoutSecondsSet       bool                     `json:"timeout_seconds_set"`
	RetryPolicy             *RetryPolicy             `json:"retry_policy,omitempty"`
	TrueBranch              *normalizedNode          `json:"true_branch,omitempty"`
	FalseBranch             *normalizedNode          `json:"false_branch,omitempty"`
	OutputName              string                   `json:"output_name,omitempty"`
	OutputFormat            string                   `json:"output_format,omitempty"`
	TaskID                  string                   `json:"task_id,omitempty"`
	TaskSummary             string                   `json:"task_summary,omitempty"`
	Identity                string                   `json:"identity,omitempty"`
	Image                   string                   `json:"image,omitempty"`
	Sandbox                 string                   `json:"sandbox,omitempty"`
	RuntimeURL              string                   `json:"runtime_url,omitempty"`
	GraceSeconds            int                      `json:"grace_seconds,omitempty"`
	GraceSecondsSet         bool                     `json:"grace_seconds_set,omitempty"`
	RepoSpecs               []map[string]interface{} `json:"repo_specs,omitempty"`
	WorkSource              map[string]interface{}   `json:"work_source,omitempty"`
	InheritFrom             *NovomoHandoffRef        `json:"inherit_from,omitempty"`
	InheritFromNodeID       string                   `json:"inherit_from_node_id,omitempty"`
	InheritFromPolicy       string                   `json:"inherit_from_policy,omitempty"`
	InheritFromWorkflowTask bool                     `json:"inherit_from_workflow_task,omitempty"`
	Condition               string                   `json:"condition,omitempty"`
	OperationType           string                   `json:"operation_type,omitempty"`
	OperationConfig         map[string]interface{}   `json:"operation_config,omitempty"`
	AggregationMethod       string                   `json:"aggregation_method,omitempty"`
	AggregationConfig       map[string]interface{}   `json:"aggregation_config,omitempty"`
	WorkflowRefID           string                   `json:"workflow_ref_id,omitempty"`
	InputTemplate           map[string]string        `json:"input_template,omitempty"`
	OutputKey               string                   `json:"output_key,omitempty"`
	ChildWorkflowID         string                   `json:"child_workflow_id,omitempty"`
	ChildInputTemplate      map[string]string        `json:"child_input_template,omitempty"`
	ChildAwait              bool                     `json:"child_await,omitempty"`
	ChildOutputKey          string                   `json:"child_output_key,omitempty"`
	SourceWorkflowHash      string                   `json:"source_workflow_hash,omitempty"`
	Metadata                map[string]interface{}   `json:"metadata,omitempty"`
}

// normalizedEdge is a canonical edge for hashing.
type normalizedEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

// normalizedConfig is the canonical representation hashed to produce config_hash.
type normalizedConfig struct {
	Nodes  []normalizedNode `json:"nodes"`
	Edges  []normalizedEdge `json:"edges"`
	Limits *CostLimits      `json:"limits,omitempty"`
}

// ComputeConfigHash produces a deterministic fingerprint of a workflow's
// effective configuration. It hashes only explicitly provided node values,
// preserves node order when it defines an implicit sequence, normalizes nodes
// for explicit graphs and edges by source+target, and excludes context variables
// and cosmetic fields (name, description, workflow ID and display metadata).
func ComputeConfigHash(wf *Workflow) string {
	_, hash := normalizeAndHash(wf)
	return hash
}

// normalizeAndHash builds the canonical representation and computes the hash
// in a single pass, returning both to avoid redundant normalize() calls.
func normalizeAndHash(wf *Workflow) (*normalizedConfig, string) {
	nc := normalize(wf)
	data, err := json.Marshal(nc)
	if err != nil {
		return nc, ""
	}
	h := sha256.Sum256(data)
	return nc, hex.EncodeToString(h[:])
}

// normalize builds the canonical representation of a workflow's config.
func normalize(wf *Workflow) *normalizedConfig {
	nc := &normalizedConfig{
		Limits: wf.Limits,
	}

	// Node order defines implicit sequential edges when no graph is supplied.
	// With explicit edges, serialization order is not semantic and is normalized.
	for _, s := range wf.Nodes {
		nc.Nodes = append(nc.Nodes, normalizeNode(s))
	}
	if len(wf.Edges) > 0 {
		sort.Slice(nc.Nodes, func(i, j int) bool {
			return nc.Nodes[i].NodeID < nc.Nodes[j].NodeID
		})
	}

	// Normalize and sort edges by source, then target
	for _, e := range wf.Edges {
		nc.Edges = append(nc.Edges, normalizedEdge{
			Source: e.Source,
			Target: e.Target,
		})
	}
	sort.Slice(nc.Edges, func(i, j int) bool {
		if nc.Edges[i].Source == nc.Edges[j].Source {
			return nc.Edges[i].Target < nc.Edges[j].Target
		}
		return nc.Edges[i].Source < nc.Edges[j].Source
	})

	return nc
}

// DisplayConfig is the JSON-friendly representation returned by the /config endpoint.
type DisplayConfig struct {
	WorkflowID string        `json:"workflow_id"`
	ConfigHash string        `json:"config_hash"`
	Nodes      []DisplayNode `json:"nodes"`
	Edges      []DisplayEdge `json:"edges"`
	Limits     *CostLimits   `json:"limits,omitempty"`
}

// DisplayNode is a single node's effective config for display.
type DisplayNode struct {
	NodeID            string  `json:"node_id"`
	NodeType          string  `json:"node_type"`
	Model             string  `json:"model,omitempty"`
	Harness           string  `json:"harness,omitempty"`
	Sandbox           string  `json:"sandbox,omitempty"`
	Temperature       float64 `json:"temperature"`
	MaxTokens         int     `json:"max_tokens"`
	TimeoutSeconds    int     `json:"timeout_seconds"`
	SystemPrompt      string  `json:"system_prompt,omitempty"`
	Prompt            string  `json:"prompt,omitempty"`
	AggregationMethod string  `json:"aggregation_method,omitempty"`
}

// DisplayEdge is an edge for display.
type DisplayEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

// NormalizeForDisplay builds a display-friendly normalized config from a workflow.
// It reuses the normalize() result to avoid double-walking the node list.
func NormalizeForDisplay(wf *Workflow) *DisplayConfig {
	nc, hash := normalizeAndHash(wf)

	dc := &DisplayConfig{
		WorkflowID: wf.ID,
		ConfigHash: hash,
		Limits:     wf.Limits,
	}

	for _, ns := range nc.Nodes {
		dc.Nodes = append(dc.Nodes, DisplayNode{
			NodeID:            ns.NodeID,
			NodeType:          ns.NodeType,
			Model:             ns.Model,
			Harness:           ns.Harness,
			Sandbox:           ns.Sandbox,
			Temperature:       ns.Temperature,
			MaxTokens:         ns.MaxTokens,
			TimeoutSeconds:    ns.TimeoutSeconds,
			SystemPrompt:      ns.SystemPrompt,
			Prompt:            ns.Prompt,
			AggregationMethod: ns.AggregationMethod,
		})
	}

	for _, e := range nc.Edges {
		dc.Edges = append(dc.Edges, DisplayEdge(e))
	}

	return dc
}

// normalizeNode extracts config-relevant fields without applying implicit defaults.
func normalizeNode(s *Node) normalizedNode {
	temp := 0.0
	tempSet := false
	if s.Temperature != nil {
		temp = *s.Temperature
		tempSet = true
	}
	maxTokens := s.MaxTokens
	maxTokensSet := s.MaxTokens != 0
	timeout := s.TimeoutSeconds
	timeoutSet := s.TimeoutSeconds != 0
	graceSeconds := s.GraceSeconds
	graceSecondsSet := s.GraceSeconds != 0

	return normalizedNode{
		NodeID:                  s.ID,
		NodeType:                string(s.Type),
		Model:                   s.Model,
		Prompt:                  s.Prompt,
		Harness:                 s.Harness,
		SystemPrompt:            s.SystemPrompt,
		Temperature:             temp,
		TemperatureSet:          tempSet,
		MaxTokens:               maxTokens,
		MaxTokensSet:            maxTokensSet,
		TimeoutSeconds:          timeout,
		TimeoutSecondsSet:       timeoutSet,
		RetryPolicy:             s.RetryPolicy.Clone(),
		TrueBranch:              normalizeOptionalNode(s.TrueBranch),
		FalseBranch:             normalizeOptionalNode(s.FalseBranch),
		OutputName:              s.OutputName,
		OutputFormat:            s.OutputFormat,
		TaskID:                  s.TaskID,
		TaskSummary:             s.TaskSummary,
		Identity:                s.Identity,
		Image:                   s.Image,
		Sandbox:                 s.Sandbox,
		RuntimeURL:              s.RuntimeURL,
		GraceSeconds:            graceSeconds,
		GraceSecondsSet:         graceSecondsSet,
		RepoSpecs:               s.RepoSpecs,
		WorkSource:              s.WorkSource,
		InheritFrom:             NormalizeNovomoHandoff(s.InheritFrom),
		InheritFromNodeID:       s.InheritFromNodeID,
		InheritFromPolicy:       s.InheritFromPolicy,
		InheritFromWorkflowTask: s.InheritFromWorkflowTask,
		Condition:               s.Condition,
		OperationType:           s.OperationType,
		OperationConfig:         s.OperationConfig,
		AggregationMethod:       string(s.AggregationMethod),
		AggregationConfig:       s.AggregationConfig,
		WorkflowRefID:           s.WorkflowRefID,
		InputTemplate:           s.InputTemplate,
		OutputKey:               s.OutputKey,
		ChildWorkflowID:         s.ChildWorkflowID,
		ChildInputTemplate:      s.ChildInputTemplate,
		ChildAwait:              s.ChildAwait,
		ChildOutputKey:          s.ChildOutputKey,
		SourceWorkflowHash:      metadataString(s.Metadata, "source_workflow_hash"),
		Metadata:                semanticNodeMetadata(s.Metadata),
	}
}

func normalizeOptionalNode(node *Node) *normalizedNode {
	if node == nil {
		return nil
	}
	normalized := normalizeNode(node)
	return &normalized
}

func semanticNodeMetadata(metadata map[string]interface{}) map[string]interface{} {
	if len(metadata) == 0 {
		return nil
	}
	semantic := make(map[string]interface{}, len(metadata))
	for key, value := range metadata {
		switch key {
		case "label", "name", "description":
			continue
		default:
			semantic[key] = value
		}
	}
	if len(semantic) == 0 {
		return nil
	}
	return semantic
}

func metadataString(metadata map[string]interface{}, key string) string {
	if metadata == nil {
		return ""
	}
	value, _ := metadata[key].(string)
	return value
}
