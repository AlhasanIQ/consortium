// Package runtime defines the execution runtime abstraction and durable execution primitives.
package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// ExecutionIdentity holds the stable identity for a workflow execution.
type ExecutionIdentity struct {
	WorkflowExecutionID string `json:"workflow_execution_id"` // stable logical identity
	RunID               string `json:"run_id"`                // per-run, changes on rollover
	RunNumber           int    `json:"run_number"`            // 1-based within execution
	DAGHash             string `json:"dag_hash"`              // SHA256 of frozen snapshot
}

// StartConflictPolicy defines behavior when a duplicate execution ID is submitted.
type StartConflictPolicy string

const (
	ConflictPolicyReject           StartConflictPolicy = "reject"
	ConflictPolicyAllowNewRun      StartConflictPolicy = "allow_new_run"
	ConflictPolicyIdempotentReturn StartConflictPolicy = "idempotent_return"
)

// FrozenSnapshot holds the immutable workflow definition and its hash.
type FrozenSnapshot struct {
	DAGHash    string `json:"dag_hash"`
	Definition []byte `json:"definition"` // canonical JSON
}

// CanonicalNode is the normalized representation of a node for canonical hashing.
// Only fields that affect execution semantics are included.
type CanonicalNode struct {
	ID                      string                     `json:"id"`
	Type                    string                     `json:"type"`
	Model                   string                     `json:"model,omitempty"`
	Prompt                  string                     `json:"prompt,omitempty"`
	Harness                 string                     `json:"harness,omitempty"`
	SystemPrompt            string                     `json:"system_prompt,omitempty"`
	Temperature             *float64                   `json:"temperature,omitempty"`
	MaxTokens               int                        `json:"max_tokens,omitempty"`
	TimeoutSeconds          int                        `json:"timeout_seconds,omitempty"`
	RetryPolicy             interface{}                `json:"retry_policy,omitempty"`
	TrueBranch              interface{}                `json:"true_branch,omitempty"`
	FalseBranch             interface{}                `json:"false_branch,omitempty"`
	OutputFormat            string                     `json:"output_format,omitempty"`
	TaskID                  string                     `json:"task_id,omitempty"`
	TaskSummary             string                     `json:"task_summary,omitempty"`
	Identity                string                     `json:"identity,omitempty"`
	Image                   string                     `json:"image,omitempty"`
	Sandbox                 string                     `json:"sandbox,omitempty"`
	RuntimeURL              string                     `json:"runtime_url,omitempty"`
	GraceSeconds            int                        `json:"grace_seconds,omitempty"`
	RepoSpecs               []map[string]interface{}   `json:"repo_specs,omitempty"`
	WorkSource              map[string]interface{}     `json:"work_source,omitempty"`
	InheritFrom             *CanonicalNovomoHandoffRef `json:"inherit_from,omitempty"`
	InheritFromNodeID       string                     `json:"inherit_from_node_id,omitempty"`
	InheritFromPolicy       string                     `json:"inherit_from_policy,omitempty"`
	InheritFromWorkflowTask bool                       `json:"inherit_from_workflow_task,omitempty"`
	Condition               string                     `json:"condition,omitempty"`
	OutputName              string                     `json:"output_name,omitempty"`
	OperationType           string                     `json:"operation_type,omitempty"`
	OperationConfig         map[string]interface{}     `json:"operation_config,omitempty"`
	AggregationMethod       string                     `json:"aggregation_method,omitempty"`
	AggregationConfig       map[string]interface{}     `json:"aggregation_config,omitempty"`
	WorkflowRefID           string                     `json:"workflow_ref_id,omitempty"`
	InputTemplate           map[string]string          `json:"input_template,omitempty"`
	OutputKey               string                     `json:"output_key,omitempty"`
	ChildWorkflowID         string                     `json:"child_workflow_id,omitempty"`
	ChildInputTemplate      map[string]string          `json:"child_input_template,omitempty"`
	ChildAwait              bool                       `json:"child_await,omitempty"`
	ChildOutputKey          string                     `json:"child_output_key,omitempty"`
	Metadata                map[string]interface{}     `json:"metadata,omitempty"`
}

// CanonicalNovomoHandoffRef is the runtime package's copy of the opaque
// Consortium-to-Novomo handoff envelope. Keeping it here avoids importing the
// workflow package into the durable runtime primitives.
type CanonicalNovomoHandoffRef struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Policy string `json:"policy,omitempty"`
}

// CanonicalEdge is the normalized representation of an edge.
type CanonicalEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

// CanonicalWorkflow is the normalized, deterministic representation of a workflow
// used for hashing and frozen storage.
type CanonicalWorkflow struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Nodes       []CanonicalNode        `json:"nodes"`
	Edges       []CanonicalEdge        `json:"edges"`
	Context     map[string]interface{} `json:"context,omitempty"`
	Limits      interface{}            `json:"limits,omitempty"`
	FrozenAt    time.Time              `json:"frozen_at"`
}

// WorkflowFreezer is the interface for workflow types that can be frozen.
// This avoids importing the workflow package directly.
type WorkflowFreezer interface {
	GetID() string
	GetName() string
	GetDescription() string
	GetNodes() []NodeForFreeze
	GetEdges() []EdgeForFreeze
	GetContext() map[string]interface{}
	GetLimits() interface{}
}

// NodeForFreeze provides the fields needed to freeze a node.
type NodeForFreeze struct {
	ID                      string
	Type                    string
	Model                   string
	Prompt                  string
	Harness                 string
	SystemPrompt            string
	Temperature             *float64
	MaxTokens               int
	TimeoutSeconds          int
	RetryPolicy             interface{}
	TrueBranch              interface{}
	FalseBranch             interface{}
	OutputFormat            string
	TaskID                  string
	TaskSummary             string
	Identity                string
	Image                   string
	Sandbox                 string
	RuntimeURL              string
	GraceSeconds            int
	RepoSpecs               []map[string]interface{}
	WorkSource              map[string]interface{}
	InheritFrom             *CanonicalNovomoHandoffRef
	InheritFromNodeID       string
	InheritFromPolicy       string
	InheritFromWorkflowTask bool
	Condition               string
	OutputName              string
	OperationType           string
	OperationConfig         map[string]interface{}
	AggregationMethod       string
	AggregationConfig       map[string]interface{}
	WorkflowRefID           string
	InputTemplate           map[string]string
	OutputKey               string
	ChildWorkflowID         string
	ChildInputTemplate      map[string]string
	ChildAwait              bool
	ChildOutputKey          string
	Metadata                map[string]interface{}
}

// EdgeForFreeze provides the fields needed to freeze an edge.
type EdgeForFreeze struct {
	Source string
	Target string
}

// FreezeWorkflow canonicalizes and hashes a workflow for immutable storage.
// If the workflow has no edges, linear dependencies are injected by node list order
// to preserve sequential execution semantics in the durable runtime.
func FreezeWorkflow(
	id, name, description string,
	nodes []NodeForFreeze,
	edges []EdgeForFreeze,
	context map[string]interface{},
	limits interface{},
) (*FrozenSnapshot, error) {
	if len(nodes) == 0 {
		return nil, fmt.Errorf("cannot freeze workflow with no nodes")
	}

	// Build canonical nodes (preserve original order for sequential semantics)
	canonicalNodes := make([]CanonicalNode, len(nodes))
	for i, n := range nodes {
		canonicalNodes[i] = CanonicalNode(n)
	}

	// Build canonical edges
	var canonicalEdges []CanonicalEdge
	if len(edges) == 0 {
		// No edges: inject linear dependencies by node list order.
		// For nodes [A, B, C], synthesize edges A→B, B→C.
		// This ensures the durable scheduler produces one ready node at a time.
		for i := 0; i < len(nodes)-1; i++ {
			canonicalEdges = append(canonicalEdges, CanonicalEdge{
				Source: nodes[i].ID,
				Target: nodes[i+1].ID,
			})
		}
	} else {
		// Explicit graph dependencies make serialized node order non-semantic.
		sort.Slice(canonicalNodes, func(i, j int) bool {
			return canonicalNodes[i].ID < canonicalNodes[j].ID
		})
		// Explicit edges: normalize to deterministic order (sorted by source, then target)
		canonicalEdges = make([]CanonicalEdge, len(edges))
		for i, e := range edges {
			canonicalEdges[i] = CanonicalEdge(e)
		}
		sort.Slice(canonicalEdges, func(i, j int) bool {
			if canonicalEdges[i].Source != canonicalEdges[j].Source {
				return canonicalEdges[i].Source < canonicalEdges[j].Source
			}
			return canonicalEdges[i].Target < canonicalEdges[j].Target
		})
	}

	canonical := CanonicalWorkflow{
		ID:          id,
		Name:        name,
		Description: description,
		Nodes:       canonicalNodes,
		Edges:       canonicalEdges,
		Context:     context,
		Limits:      limits,
	}

	// Compute hash from semantic content only (excludes FrozenAt for determinism)
	hashData, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal canonical workflow: %w", err)
	}
	dagHash := ComputeDAGHash(hashData)

	// Add timestamp to stored definition (metadata, not part of hash)
	canonical.FrozenAt = time.Now().UTC()
	data, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal canonical workflow with timestamp: %w", err)
	}

	return &FrozenSnapshot{
		DAGHash:    dagHash,
		Definition: data,
	}, nil
}

// ComputeDAGHash produces a SHA256 hash of the canonical workflow JSON.
func ComputeDAGHash(workflowJSON []byte) string {
	h := sha256.Sum256(workflowJSON)
	return hex.EncodeToString(h[:])
}
