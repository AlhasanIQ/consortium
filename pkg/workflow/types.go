// Package workflow provides DirectPrompt composition patterns for multi-node LLM workflows.
//
// The workflow system enables complex AI pipelines through composition patterns:
//   - Sequential: Chain prompts where each node uses output from previous nodes
//   - Parallel: Use edges to define a DAG; nodes at the same level execute concurrently
//   - Conditional: Branch execution based on previous results
//   - Output with Aggregation: Combine multiple inputs using various strategies
//
// Example sequential workflow:
//
//	wf := &workflow.Workflow{
//	    ID: "research-pipeline",
//	    Nodes: []*workflow.Node{
//	        {ID: "research", Type: workflow.NodeTypePrompt, Prompt: "Research {{topic}}"},
//	        {ID: "summarize", Type: workflow.NodeTypePrompt, Prompt: "Summarize: {{research}}"},
//	    },
//	    Context: map[string]interface{}{"topic": "AI"},
//	}
//
// Example parallel workflow using edges:
//
//	wf := &workflow.Workflow{
//	    Nodes: []*workflow.Node{
//	        {ID: "technical", Type: workflow.NodeTypePrompt, Prompt: "Technical analysis"},
//	        {ID: "business", Type: workflow.NodeTypePrompt, Prompt: "Business analysis"},
//	    },
//	    Edges: []*workflow.Edge{
//	        {Source: "input", Target: "technical"},
//	        {Source: "input", Target: "business"},
//	    },
//	}
//
// Example conditional workflow:
//
//	{
//	    Type: workflow.NodeTypeConditional,
//	    Condition: "sentiment contains positive",
//	    TrueBranch: &workflow.Node{Prompt: "Thank you message"},
//	    FalseBranch: &workflow.Node{Prompt: "Address concern"},
//	}
//
// Context variables use {{variable}} syntax for interpolation.
// Supported condition operators: contains, equals, not_empty
package workflow

import (
	"github.com/alhasaniq/consortium/pkg/providers"
)

// NodeType defines the type of workflow node
type NodeType string

const (
	NodeTypePrompt          NodeType = "prompt"
	NodeTypeConditional     NodeType = "conditional"
	NodeTypeResult          NodeType = "result"
	NodeTypeOperation       NodeType = "operation"
	NodeTypeWorkflowRef     NodeType = "workflow_ref"
	NodeTypeChildWorkflow   NodeType = "child_workflow"
	NodeTypeContractExtract NodeType = "contract_extract"
	NodeTypeAgentRun        NodeType = "agent_run"
	NodeTypeNovoRun         NodeType = "novo_run"
)

const (
	// DefaultNodeMaxTokens is kept for legacy backfill/migration reference only.
	// Runtime execution requires explicit max_tokens values.
	DefaultNodeMaxTokens = 1000

	// DefaultNovomoSandbox is the sandbox used by Novomo-backed nodes when no
	// sandbox is specified by the workflow.
	DefaultNovomoSandbox = "docker"
)

// AggregationMethod defines how multiple inputs are combined in an output node
type AggregationMethod string

const (
	// AggMethodCollect joins all inputs with a configurable separator (default)
	AggMethodCollect AggregationMethod = "collect"
	// AggMethodJudge uses a single LLM judge to select the best response
	AggMethodJudge AggregationMethod = "judge"
	// AggMethodScoring evaluates responses against a rubric and selects highest-scoring
	AggMethodScoring AggregationMethod = "scoring"
	// AggMethodSynthesis uses an LLM to synthesize all responses into one
	AggMethodSynthesis AggregationMethod = "synthesis"
	// AggMethodPeerMatrix has each agent score every other agent's response
	AggMethodPeerMatrix AggregationMethod = "peer_matrix"
	// AggMethodMajorityVote extracts discrete answers and picks the majority winner
	AggMethodMajorityVote AggregationMethod = "majority_vote"
	// AggMethodDebateDecide groups agents by answer into camps and adjudicates
	AggMethodDebateDecide AggregationMethod = "debate_decide"
)

// PeerMatrixConfig configures peer matrix aggregation
type PeerMatrixConfig struct {
	RubricModel         string                     `json:"rubric_model,omitempty"`         // Model for dynamic rubric generation (required when rubric_mode=dynamic)
	EvalSystemPrompt    string                     `json:"eval_system_prompt,omitempty"`   // Custom system prompt for evaluator persona
	EvalPrompt          string                     `json:"eval_prompt,omitempty"`          // Custom user prompt template
	Temperature         float64                    `json:"temperature,omitempty"`          // Evaluator temperature (default: 0.3)
	MaxTokens           int                        `json:"max_tokens,omitempty"`           // Per-evaluation max tokens (default: 1024)
	OpenRouterProvider  *providers.ProviderRouting `json:"openrouter_provider,omitempty"`  // Optional OpenRouter upstream routing
	OpenRouterReasoning *providers.ReasoningConfig `json:"openrouter_reasoning,omitempty"` // Optional OpenRouter reasoning controls
	Normalization       string                     `json:"normalization,omitempty"`        // "none" (default) | future custom strategies
	MaxParallel         int                        `json:"max_parallel,omitempty"`         // Concurrent evals (default: 6)
	Rubric              []RubricCriterion          `json:"rubric,omitempty"`               // Custom scoring rubric (optional)
}

// Node represents a single node in a workflow
// Frontend can optionally provide node IDs which will be preserved during execution
type Node struct {
	ID           string                 `json:"id,omitempty"` // Optional: Frontend-provided node ID
	Type         NodeType               `json:"type"`
	Model        string                 `json:"model,omitempty"`
	Prompt       string                 `json:"prompt,omitempty"`
	Harness      string                 `json:"harness,omitempty"`
	SystemPrompt string                 `json:"system_prompt,omitempty"` // System message for LLM context
	Temperature  *float64               `json:"temperature,omitempty"`
	MaxTokens    int                    `json:"max_tokens,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`

	// For Novomo-backed nodes.
	Sandbox string `json:"sandbox,omitempty"`
	// InheritFrom is an explicit opaque Novomo handoff handle. Consortium does
	// not interpret the handle beyond basic validation; Novomo resolves it into
	// workspace, memory, trunk, or context state.
	InheritFrom *NovomoHandoffRef `json:"inherit_from,omitempty"`
	// InheritFromNodeID asks Consortium to derive an opaque handoff handle from
	// a prior Novomo-producing workflow node in the same job/run.
	InheritFromNodeID string `json:"inherit_from_node_id,omitempty"`
	// InheritFromPolicy is passed through when deriving a handle from an
	// upstream node. Empty means Novomo's default policy.
	InheritFromPolicy string `json:"inherit_from_policy,omitempty"`
	// InheritFromWorkflowTask asks the runtime to derive a handoff from the
	// Novomo task shared by all agent_run nodes in this workflow execution.
	InheritFromWorkflowTask bool `json:"inherit_from_workflow_task,omitempty"`

	// For novo_run nodes (Superagent / Novomo wake)
	TaskID       string                   `json:"task_id,omitempty"`
	TaskSummary  string                   `json:"task_summary,omitempty"`
	Identity     string                   `json:"identity,omitempty"`
	Image        string                   `json:"image,omitempty"`
	RuntimeURL   string                   `json:"runtime_url,omitempty"`
	GraceSeconds int                      `json:"grace_seconds,omitempty"`
	RepoSpecs    []map[string]interface{} `json:"repo_specs,omitempty"`
	WorkSource   map[string]interface{}   `json:"work_source,omitempty"`

	// Timeout configuration
	// TimeoutSeconds specifies the maximum execution time for this node in seconds.
	// This field is required for node types that perform external calls.
	// Timeout applies to the entire node execution including LLM API calls.
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`

	// Retry configuration
	// RetryPolicy specifies retry behavior when a node fails with a transient error.
	// This field is explicit per node; runtime fallback defaults are not applied.
	RetryPolicy *RetryPolicy `json:"retry_policy,omitempty"`

	// For conditional nodes
	Condition   string `json:"condition,omitempty"`    // Expression to evaluate
	TrueBranch  *Node  `json:"true_branch,omitempty"`  // If condition is true
	FalseBranch *Node  `json:"false_branch,omitempty"` // If condition is false

	// For output nodes
	OutputName   string `json:"output_name,omitempty"`   // Name for this output
	OutputFormat string `json:"output_format,omitempty"` // json, text, structured

	// For deterministic operation nodes
	OperationType   string                 `json:"operation_type,omitempty"`
	OperationConfig map[string]interface{} `json:"operation_config,omitempty"`

	// Aggregation configuration (for output nodes)
	// AggregationMethod specifies how multiple inputs are combined
	// If empty or "collect", inputs are collected without transformation
	AggregationMethod AggregationMethod      `json:"aggregation_method,omitempty"`
	AggregationConfig map[string]interface{} `json:"aggregation_config,omitempty"`

	// For workflow_ref nodes. These are source-level composition references that
	// compile into the parent DAG before runtime execution.
	WorkflowRefID string            `json:"workflow_ref_id,omitempty"`
	InputTemplate map[string]string `json:"input_template,omitempty"`
	OutputKey     string            `json:"output_key,omitempty"`

	// For child_workflow nodes
	ChildWorkflowID    string            `json:"child_workflow_id,omitempty"`
	ChildInputTemplate map[string]string `json:"child_input_template,omitempty"`
	ChildAwait         bool              `json:"child_await,omitempty"`
	ChildOutputKey     string            `json:"child_output_key,omitempty"`
}

// NovomoHandoffRef is the public, opaque handoff shape Consortium passes to
// Novomo. Kind/id/policy are intentionally the whole contract: Consortium does
// not inspect Novomo workspace sources, memory episodes, branches, trunks, or
// manifests.
type NovomoHandoffRef struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Policy string `json:"policy,omitempty"`
}

// NodeDisplayMeta holds typed display/identity fields extracted from Node.Metadata.
// Use Node.DisplayMeta() to safely parse these from the raw map.
type NodeDisplayMeta struct {
	Label       string   `json:"label,omitempty"`
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	InputIDs    []string `json:"input_ids,omitempty"`
	OutputName  string   `json:"output_name,omitempty"`
}

// DisplayMeta extracts typed display fields from the node's Metadata map.
func (n *Node) DisplayMeta() NodeDisplayMeta {
	meta := NodeDisplayMeta{}
	if n.Metadata == nil {
		return meta
	}
	if v, ok := n.Metadata["label"].(string); ok {
		meta.Label = v
	}
	if v, ok := n.Metadata["name"].(string); ok {
		meta.Name = v
	}
	if v, ok := n.Metadata["description"].(string); ok {
		meta.Description = v
	}
	if v, ok := n.Metadata["output_name"].(string); ok {
		meta.OutputName = v
	}
	// input_ids can be []interface{} (from JSON) or []string
	if ids, ok := n.Metadata["input_ids"].([]interface{}); ok {
		for _, id := range ids {
			if s, ok := id.(string); ok {
				meta.InputIDs = append(meta.InputIDs, s)
			}
		}
	} else if ids, ok := n.Metadata["input_ids"].([]string); ok {
		meta.InputIDs = ids
	}
	return meta
}

// Edge represents a connection between two nodes in the workflow graph
type Edge struct {
	ID     string `json:"id"`
	Source string `json:"source"` // Source node ID
	Target string `json:"target"` // Target node ID
}

// CostLimits defines budget constraints for workflow execution.
// If any limit is exceeded during execution, the workflow stops immediately.
// Zero values mean no limit for that field.
type CostLimits struct {
	MaxCostUSD      float64 `json:"max_cost_usd,omitempty"`      // Max total cost in USD
	MaxTokens       int     `json:"max_tokens,omitempty"`        // Max total tokens (input + output)
	MaxInputTokens  int     `json:"max_input_tokens,omitempty"`  // Max input tokens
	MaxOutputTokens int     `json:"max_output_tokens,omitempty"` // Max output tokens
}

// Workflow represents a complete workflow
type Workflow struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Nodes       []*Node                `json:"nodes"`
	Edges       []*Edge                `json:"edges,omitempty"`   // Graph edges for parallel execution
	Context     map[string]interface{} `json:"context,omitempty"` // Shared context across nodes
	Limits      *CostLimits            `json:"limits,omitempty"`  // Budget constraints
}

// NodeResult represents the result of executing a single node
type NodeResult struct {
	NodeID        string                        `json:"node_id"`
	Success       bool                          `json:"success"`
	Output        string                        `json:"output,omitempty"`
	Response      *providers.CompletionResponse `json:"response,omitempty"`
	Error         string                        `json:"error,omitempty"`
	TokensInput   int                           `json:"tokens_input"`
	TokensOutput  int                           `json:"tokens_output"`
	Cost          float64                       `json:"cost"`
	LatencyMs     float64                       `json:"latency_ms"`
	Metadata      map[string]interface{}        `json:"metadata,omitempty"`
	AttemptNumber int                           `json:"attempt_number,omitempty"`
	ExecutionUID  string                        `json:"execution_uid,omitempty"`
}

// newErrorResult builds a failed NodeResult with the given error message and latency.
// Optional metadata is merged if provided (first map wins).
func newErrorResult(nodeID string, errMsg string, latencyMs float64, metadata ...map[string]interface{}) *NodeResult {
	r := &NodeResult{
		NodeID:    nodeID,
		Success:   false,
		Error:     errMsg,
		LatencyMs: latencyMs,
	}
	if len(metadata) > 0 && metadata[0] != nil {
		r.Metadata = metadata[0]
	}
	return r
}

// CostLimitDetails provides information when a cost limit is exceeded
type CostLimitDetails struct {
	LimitType    string  `json:"limit_type"`       // "cost", "tokens", "input_tokens", "output_tokens"
	LimitValue   float64 `json:"limit_value"`      // The configured limit
	CurrentValue float64 `json:"current_value"`    // Value when limit was hit
	ExceededAt   string  `json:"exceeded_at_node"` // Node ID where limit was exceeded
}

// WorkflowResult represents the result of executing a complete workflow
type WorkflowResult struct {
	WorkflowID        string                 `json:"workflow_id"`
	Success           bool                   `json:"success"`
	NodeResults       []*NodeResult          `json:"node_results"`
	FinalOutput       string                 `json:"final_output,omitempty"`
	Outputs           map[string]interface{} `json:"outputs,omitempty"` // Named outputs from output nodes
	TotalTokens       int                    `json:"total_tokens"`
	TotalInputTokens  int                    `json:"total_input_tokens"`
	TotalOutputTokens int                    `json:"total_output_tokens"`
	TotalCost         float64                `json:"total_cost"`
	TotalLatency      float64                `json:"total_latency_ms"`
	Context           map[string]interface{} `json:"context,omitempty"`
	Error             string                 `json:"error,omitempty"`
	CostLimitExceeded bool                   `json:"cost_limit_exceeded,omitempty"` // True if execution stopped due to cost limit
	LimitDetails      *CostLimitDetails      `json:"limit_details,omitempty"`       // Details when cost limit exceeded
}
