package workflow

import (
	"context"
	"sync"
)

// NodeContext bundles all parameters needed to execute a single node.
// Passed to NodeRunner.Execute as a single argument.
type NodeContext struct {
	Ctx             context.Context
	Node            *Node
	NodeID          string
	Attempt         int
	ParentSpanID    string
	WorkflowContext map[string]interface{}
	ExecCtx         *ExecutionContext
	CostTracker     *CostTracker
}

// NodeRunner executes a single node type. Implementations are registered
// by NodeType in RunnerRegistry and dispatched by the executor.
//
// Runners contain only node-type-specific logic. Retry, timeout, trace spans,
// and attempt tracking are handled centrally by the executor's executeNode method.
type NodeRunner interface {
	// NodeType returns the type of node this runner handles.
	NodeType() NodeType
	// Execute runs the node logic and returns a result.
	Execute(sc *NodeContext) (*NodeResult, error)
}

// NodeExecutor abstracts the full node executor (with retry/spans).
// Implemented by Executor so that ConditionalNodeRunner can invoke branch
// execution without a direct dependency on the concrete Executor type.
type NodeExecutor interface {
	ExecuteNode(ctx context.Context, node *Node, nodeID string, workflowContext map[string]interface{}, execCtx *ExecutionContext, costTracker *CostTracker) (*NodeResult, error)
}

// NodeExecutorFunc is the function signature for the full node executor.
// It satisfies NodeExecutor so it can be used in tests and as an adapter.
type NodeExecutorFunc func(ctx context.Context, node *Node, nodeID string, workflowContext map[string]interface{}, execCtx *ExecutionContext, costTracker *CostTracker) (*NodeResult, error)

// ExecuteNode implements NodeExecutor.
func (f NodeExecutorFunc) ExecuteNode(ctx context.Context, node *Node, nodeID string, workflowContext map[string]interface{}, execCtx *ExecutionContext, costTracker *CostTracker) (*NodeResult, error) {
	return f(ctx, node, nodeID, workflowContext, execCtx, costTracker)
}

// RunnerRegistry maps NodeType to NodeRunner. Thread-safe for concurrent reads.
type RunnerRegistry struct {
	mu      sync.RWMutex
	runners map[NodeType]NodeRunner
}

// NewRunnerRegistry creates an empty runner registry.
func NewRunnerRegistry() *RunnerRegistry {
	return &RunnerRegistry{
		runners: make(map[NodeType]NodeRunner),
	}
}

// Register adds a runner to the registry, keyed by its NodeType.
func (r *RunnerRegistry) Register(runner NodeRunner) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runners[runner.NodeType()] = runner
}

// Get retrieves a runner by node type.
func (r *RunnerRegistry) Get(nodeType NodeType) (NodeRunner, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	runner, ok := r.runners[nodeType]
	return runner, ok
}
