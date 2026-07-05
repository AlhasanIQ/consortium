package workflow

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/trace"
	"github.com/google/uuid"
)

const (
	// DefaultNodeTimeoutSeconds is the default timeout for node execution (2 minutes)
	// This prevents nodes from hanging indefinitely due to network issues or slow LLM responses
	DefaultNodeTimeoutSeconds = 120
)

// varRegex matches {{variable}} placeholders for context interpolation.
// Compiled once at package init to avoid per-call overhead.
var varRegex = regexp.MustCompile(`\{\{([^}]+)\}\}`)

// Executor executes workflows by orchestrating node execution and managing context.
// It handles sequential, parallel, and conditional node execution with full observability.
// Node-type-specific logic is delegated to NodeRunner implementations via RunnerRegistry.
type Executor struct {
	llmClient          *providers.Client   // Required - enforces accounting and logging
	aggregatorRegistry *AggregatorRegistry // Registry for aggregation strategies
	runnerRegistry     *RunnerRegistry     // Registry for node type runners
}

// RunnerRegistry returns the executor's runner registry for external use (e.g., durable runtime adapter).
func (e *Executor) RunnerRegistry() *RunnerRegistry {
	return e.runnerRegistry
}

// NewExecutor creates a new workflow executor with LLM client.
// All LLM requests will be logged to storage for observability and cost tracking.
// Node-type runners are registered automatically; adding a new node type requires
// only implementing NodeRunner and registering it here.
func NewExecutor(client *providers.Client) *Executor {
	aggRegistry := NewAggregatorRegistry()

	runnerRegistry := NewRunnerRegistry()

	e := &Executor{
		llmClient:          client,
		aggregatorRegistry: aggRegistry,
		runnerRegistry:     runnerRegistry,
	}

	// Register node type runners
	llmRunner := &LLMCallNodeRunner{llmClient: client}
	runnerRegistry.Register(llmRunner)
	runnerRegistry.Register(&ConditionalNodeRunner{executor: e})
	runnerRegistry.Register(&ResultNodeRunner{
		llmClient:          client,
		aggregatorRegistry: aggRegistry,
	})
	runnerRegistry.Register(&ChildWorkflowNodeRunner{})
	runnerRegistry.Register(&OperationNodeRunner{})
	runnerRegistry.Register(&ContractExtractNodeRunner{llmRunner: llmRunner})
	runnerRegistry.Register(&AgentRunNodeRunner{})
	runnerRegistry.Register(&NovoRunNodeRunner{})

	return e
}

// RetryEvent carries information about a retry decision for observability.
// Emitted via ExecutionContext.RetryCallback when a node is retried.
type RetryEvent struct {
	Type        string // "retry_start", "retry_backoff", "retry_exhausted"
	NodeID      string
	Attempt     int
	MaxAttempts int
	Error       error
	ErrorCode   string
	BackoffMs   int64
}

// ExecutionContext holds context for workflow execution
type ExecutionContext struct {
	JobID                string                 // Parent job ID for tracking
	WorkflowExecutionID  string                 // Stable logical execution identity
	WorkflowID           string                 // Parent workflow ID
	RunID                string                 // Durable per-run identity (optional)
	TraceWriter          trace.Writer           // Optional trace span writer (nil = no tracing)
	RetryCallback        func(event RetryEvent) // Optional retry event callback (nil = no retry events)
	MaxParallelNodes     int                    // Max goroutines per DAG level (0 = unlimited)
	ExecuteChildWorkflow func(ctx context.Context, req *ChildWorkflowRequest) (*ChildWorkflowResult, error)
	ExecuteAgentRun      func(ctx context.Context, req *AgentRunRequest) (*AgentRunResult, error)
	ExecuteNovoRun       func(ctx context.Context, req *NovoRunRequest) (*AgentRunResult, error)
}

// ChildWorkflowRequest describes a native child workflow call from a parent node.
type ChildWorkflowRequest struct {
	ParentJobID      string
	ParentRunID      string
	ParentWorkflowID string
	ParentNodeID     string
	ChildWorkflowID  string
	InputValues      map[string]interface{}
}

// ChildWorkflowResult describes the terminal outcome of a child workflow execution.
type ChildWorkflowResult struct {
	JobID             string
	WorkflowID        string
	Success           bool
	FinalOutput       string
	Outputs           map[string]interface{}
	Error             string
	TotalTokens       int
	TotalInputTokens  int
	TotalOutputTokens int
	TotalCost         float64
	TotalLatency      float64
}

// AgentRunRequest describes a Novomo-backed external agent run from a workflow node.
type AgentRunRequest struct {
	ParentJobID             string
	ParentExecutionID       string
	ParentRunID             string
	ParentWorkflowID        string
	ParentNodeID            string
	Attempt                 int
	Prompt                  string
	Harness                 string
	Sandbox                 string
	TaskID                  string
	TimeoutSeconds          int
	InheritFrom             *NovomoHandoffRef
	InheritFromNodeID       string
	InheritFromPolicy       string
	InheritFromWorkflowTask bool
	IdempotencyKey          string
}

// NovoRunRequest describes a Novomo wake from a workflow Superagent node.
type NovoRunRequest struct {
	ParentJobID             string
	ParentExecutionID       string
	ParentRunID             string
	ParentWorkflowID        string
	ParentNodeID            string
	Attempt                 int
	Goal                    string
	TaskID                  string
	TaskSummary             string
	Identity                string
	Image                   string
	Sandbox                 string
	RuntimeURL              string
	TimeoutSeconds          int
	GraceSeconds            int
	RepoSpecs               []map[string]interface{}
	WorkSource              map[string]interface{}
	InheritFrom             *NovomoHandoffRef
	InheritFromNodeID       string
	InheritFromPolicy       string
	InheritFromWorkflowTask bool
	IdempotencyKey          string
}

// AgentRunResult describes the terminal outcome of a Novomo-backed agent run.
type AgentRunResult struct {
	ExternalRunID    string
	ExternalRunKind  string
	ExternalTaskID   string
	ExternalJobRunID string
	InheritFrom      *NovomoHandoffRef
	Harness          string
	Status           string
	Success          bool
	Output           string
	Error            string
	ErrorCode        string
	TokensInput      int
	TokensOutput     int
	Cost             float64
	StartedAt        *time.Time
	FinishedAt       *time.Time
}

// CostTracker tracks accumulated costs during workflow execution.
// It is thread-safe for concurrent access from parallel nodes.
type CostTracker struct {
	TotalCost         float64
	TotalTokens       int
	TotalInputTokens  int
	TotalOutputTokens int
	Limits            *CostLimits
	mu                sync.Mutex
}

// NewCostTracker creates a new cost tracker with optional limits.
// If limits is nil, no cost enforcement is performed.
func NewCostTracker(limits *CostLimits) *CostTracker {
	return &CostTracker{
		Limits: limits,
	}
}

// Add records tokens and cost from a node execution.
// This method is thread-safe.
func (ct *CostTracker) Add(inputTokens, outputTokens int, cost float64) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.TotalCost += cost
	ct.TotalInputTokens += inputTokens
	ct.TotalOutputTokens += outputTokens
	ct.TotalTokens += inputTokens + outputTokens
}

// AddAndCheck atomically records tokens/cost and checks limits in a single lock acquisition.
// Returns a CostLimitError if any limit is exceeded after the update, nil otherwise.
func (ct *CostTracker) AddAndCheck(inputTokens, outputTokens int, cost float64, nodeID string) error {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.TotalCost += cost
	ct.TotalInputTokens += inputTokens
	ct.TotalOutputTokens += outputTokens
	ct.TotalTokens += inputTokens + outputTokens
	return ct.checkLimitsLocked(nodeID)
}

// CheckLimits verifies no limits are exceeded.
// Returns a CostLimitError if any limit is exceeded, nil otherwise.
func (ct *CostTracker) CheckLimits(nodeID string) error {
	if ct.Limits == nil {
		return nil
	}
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return ct.checkLimitsLocked(nodeID)
}

// checkLimitsLocked checks all limit thresholds. Caller must hold ct.mu.
func (ct *CostTracker) checkLimitsLocked(nodeID string) error {
	if ct.Limits == nil {
		return nil
	}
	if ct.Limits.MaxCostUSD > 0 && ct.TotalCost > ct.Limits.MaxCostUSD {
		return NewCostLimitError("cost", ct.Limits.MaxCostUSD, ct.TotalCost, nodeID)
	}
	if ct.Limits.MaxTokens > 0 && ct.TotalTokens > ct.Limits.MaxTokens {
		return NewCostLimitError("tokens", float64(ct.Limits.MaxTokens), float64(ct.TotalTokens), nodeID)
	}
	if ct.Limits.MaxInputTokens > 0 && ct.TotalInputTokens > ct.Limits.MaxInputTokens {
		return NewCostLimitError("input_tokens", float64(ct.Limits.MaxInputTokens), float64(ct.TotalInputTokens), nodeID)
	}
	if ct.Limits.MaxOutputTokens > 0 && ct.TotalOutputTokens > ct.Limits.MaxOutputTokens {
		return NewCostLimitError("output_tokens", float64(ct.Limits.MaxOutputTokens), float64(ct.TotalOutputTokens), nodeID)
	}
	return nil
}

// GetTotals returns the current totals (thread-safe)
func (ct *CostTracker) GetTotals() (cost float64, tokens, inputTokens, outputTokens int) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return ct.TotalCost, ct.TotalTokens, ct.TotalInputTokens, ct.TotalOutputTokens
}

// Execute runs a workflow with execution context for job tracking.
// Nodes can reference previous outputs via {{node_0}}, {{node_1}}, etc. syntax in prompts.
// Returns aggregated results including tokens, costs, and latency for all nodes.
// If workflow has edges, nodes are executed in parallel based on dependency graph.
// If workflow has Limits defined, execution stops when any limit is exceeded.
func (e *Executor) Execute(ctx context.Context, workflow *Workflow, execCtx *ExecutionContext) (*WorkflowResult, error) {
	result := &WorkflowResult{
		WorkflowID:  workflow.ID,
		Success:     true,
		NodeResults: make([]*NodeResult, 0),
		Context:     make(map[string]interface{}),
		Outputs:     make(map[string]interface{}),
	}

	// Initialize cost tracker with workflow limits
	costTracker := NewCostTracker(workflow.Limits)

	// Initialize context with workflow context
	if workflow.Context != nil {
		for k, v := range workflow.Context {
			result.Context[k] = v
		}
	}

	// Build execution plan from edges
	executionPlan, err := BuildExecutionPlan(workflow)
	if err != nil {
		result.Success = false
		result.Error = fmt.Sprintf("failed to build execution plan: %v", err)
		return result, err
	}

	// Execute each level in parallel
	for levelIdx, level := range executionPlan {
		log.Printf("⚡ Executing level %d with %d parallel nodes", levelIdx, len(level.Nodes))

		if len(level.Nodes) == 1 {
			// Single node - execute directly (no goroutine overhead)
			node := level.Nodes[0]
			nodeID := node.ID
			if nodeID == "" {
				nodeID = fmt.Sprintf("node_%d", levelIdx)
			}

			nodeResult, err := e.executeNode(ctx, node, nodeID, result.Context, execCtx, costTracker)

			if nf := e.processNodeResult(result, node, nodeID, nodeResult, err); nf != nil {
				applyFailure(result, nf)
				if err != nil {
					return result, err
				}
				return result, fmt.Errorf("node failed: %s", nodeResult.Error)
			}
		} else {
			// Multiple nodes - execute in parallel
			var wg sync.WaitGroup
			nodeResults := make([]*NodeResult, len(level.Nodes))
			nodeErrors := make([]error, len(level.Nodes))

			// Create deep context snapshot for parallel execution to prevent data races
			contextSnapshot := DeepCopyContext(result.Context)

			// Determine fan-out limit for this level
			fanOut := len(level.Nodes)
			if execCtx != nil && execCtx.MaxParallelNodes > 0 && execCtx.MaxParallelNodes < fanOut {
				fanOut = execCtx.MaxParallelNodes
			}
			sem := make(chan struct{}, fanOut)

			for i, node := range level.Nodes {
				wg.Add(1)
				sem <- struct{}{} // acquire semaphore slot
				go func(idx int, s *Node) {
					defer func() { <-sem }() // release semaphore slot
					defer wg.Done()

					nodeID := s.ID
					if nodeID == "" {
						nodeID = fmt.Sprintf("node_%d_%d", levelIdx, idx)
					}

					nodeResult, err := e.executeNode(ctx, s, nodeID, contextSnapshot, execCtx, costTracker)
					nodeResults[idx] = nodeResult
					nodeErrors[idx] = err
				}(i, node)
			}

			wg.Wait()

			// Process results and collect failures
			var failures []*nodeFailure
			var firstErr error
			for i, nodeResult := range nodeResults {
				nodeID := level.Nodes[i].ID
				nf := e.processNodeResult(result, level.Nodes[i], nodeID, nodeResult, nodeErrors[i])
				if nf != nil {
					failures = append(failures, nf)
					if firstErr == nil {
						if nodeErrors[i] != nil {
							firstErr = nodeErrors[i]
						} else {
							firstErr = nf.err
						}
					}
				}
			}

			// Merge __output_* keys from parallel node metadata into context.
			// Result nodes write __output_<name> to their local context snapshot,
			// but in parallel execution that snapshot is discarded. Re-apply
			// from NodeResult.Metadata so downstream nodes can reference outputs.
			for _, nodeResult := range nodeResults {
				if nodeResult == nil || nodeResult.Metadata == nil {
					continue
				}
				if outName, ok := nodeResult.Metadata["output_name"].(string); ok {
					if outVal, ok := nodeResult.Metadata["output_value"]; ok {
						outputKey := fmt.Sprintf("__output_%s", outName)
						result.Context[outputKey] = outVal
					}
				}
			}

			if len(failures) > 0 {
				collectedErrs := make([]error, len(failures))
				for i, nf := range failures {
					applyFailure(result, nf)
					collectedErrs[i] = nf.err
				}
				combined := errors.Join(collectedErrs...)
				result.Error = combined.Error()
				return result, firstErr
			}
		}
	}

	return result, nil
}

// nodeFailure describes a single node's failure for post-execution processing.
type nodeFailure struct {
	nodeID  string
	err     error // the execution error or synthetic error from !Success
	costErr *CostLimitError
}

// processNodeResult checks a node's execution outcome and returns a nodeFailure
// if the node failed, or nil if it succeeded. It always calls updateResultWithNodeResult
// when nodeResult is non-nil.
func (e *Executor) processNodeResult(result *WorkflowResult, node *Node, nodeID string, nodeResult *NodeResult, execErr error) *nodeFailure {
	if nodeResult != nil {
		e.updateResultWithNodeResult(result, node, nodeResult)
	}

	if execErr != nil {
		nf := &nodeFailure{nodeID: nodeID, err: fmt.Errorf("node %s failed: %w", nodeID, execErr)}
		if costErr, ok := AsCostLimitError(execErr); ok {
			nf.costErr = costErr
		}
		return nf
	}
	if nodeResult != nil && !nodeResult.Success {
		return &nodeFailure{
			nodeID: nodeID,
			err:    fmt.Errorf("node %s failed: %s", nodeID, nodeResult.Error),
		}
	}
	return nil
}

// applyFailure sets failure fields on the workflow result from a nodeFailure.
func applyFailure(result *WorkflowResult, nf *nodeFailure) {
	result.Success = false
	result.Error = nf.err.Error()
	if nf.costErr != nil && !result.CostLimitExceeded {
		result.CostLimitExceeded = true
		result.LimitDetails = &CostLimitDetails{
			LimitType:    nf.costErr.LimitType,
			LimitValue:   nf.costErr.LimitValue,
			CurrentValue: nf.costErr.CurrentValue,
			ExceededAt:   nf.costErr.NodeID,
		}
	}
}

// updateResultWithNodeResult updates the workflow result with a node result
func (e *Executor) updateResultWithNodeResult(result *WorkflowResult, node *Node, nodeResult *NodeResult) {
	result.NodeResults = append(result.NodeResults, nodeResult)
	result.TotalInputTokens += nodeResult.TokensInput
	result.TotalOutputTokens += nodeResult.TokensOutput
	result.TotalTokens += nodeResult.TokensInput + nodeResult.TokensOutput
	result.TotalCost += nodeResult.Cost
	result.TotalLatency += nodeResult.LatencyMs

	// Update context with node output
	result.Context[nodeResult.NodeID] = nodeResult.Output

	// Only update FinalOutput if node produced output
	if nodeResult.Output != "" {
		result.FinalOutput = nodeResult.Output
	}

	// Collect named outputs from output nodes
	if node.Type == NodeTypeResult && nodeResult.Metadata != nil {
		if outputName, ok := nodeResult.Metadata["output_name"].(string); ok {
			if outputValue, ok := nodeResult.Metadata["output_value"]; ok {
				result.Outputs[outputName] = outputValue
			}
		}
	}
}

// emitRetryEvent calls the RetryCallback if available. Nil-safe.
func emitRetryEvent(execCtx *ExecutionContext, event RetryEvent) {
	if execCtx == nil || execCtx.RetryCallback == nil {
		return
	}
	execCtx.RetryCallback(event)
}

// getJobID extracts the JobID from ExecutionContext. Returns "" if nil.
func getJobID(execCtx *ExecutionContext) string {
	if execCtx == nil {
		return ""
	}
	return execCtx.JobID
}

// writeSpan is a helper that writes a trace span if a TraceWriter is available.
// Errors are logged but never propagated - tracing must not break execution.
func writeSpan(ctx context.Context, execCtx *ExecutionContext, span *trace.Span) {
	if span == nil {
		return
	}
	if execCtx == nil || execCtx.TraceWriter == nil {
		return
	}
	if span.JobID == "" {
		span.JobID = execCtx.JobID
	}
	if span.JobID == "" {
		log.Printf("WARNING: TraceWriter configured but span missing job_id; skipping span write (node_id=%s span_id=%s)", span.NodeID, span.SpanID)
		return
	}
	traceCtx := ctx
	traceCancel := func() {}
	if traceCtx == nil || traceCtx.Err() != nil {
		// Preserve observability for timeout/cancel paths where the execution
		// context is already done by the time we emit a span.
		traceCtx, traceCancel = context.WithTimeout(context.Background(), 2*time.Second)
	}
	defer traceCancel()

	if err := execCtx.TraceWriter.WriteSpan(traceCtx, span); err != nil {
		log.Printf("WARNING: Failed to write trace span: %v", err)
	}
}

// executeNode executes a single workflow node with retry support.
//
// The retry loop wraps the node dispatch (prompt/conditional/result/child/contract_extract).
// Retry policy is explicit on every node; implicit type-based defaults are disallowed.
//
// On each failed attempt the loop checks (in order):
//  1. Parent context done → stop (user cancel / shutdown)
//  2. CostLimitError → stop (budget exceeded, retrying wastes money)
//  3. IsRetryable / ShouldRetry → decide whether to retry
//
// Per-attempt timeout (context.DeadlineExceeded from executePromptNode's own
// context.WithTimeout) IS retryable — each attempt gets a fresh timeout.
func (e *Executor) executeNode(ctx context.Context, node *Node, nodeID string, workflowContext map[string]interface{}, execCtx *ExecutionContext, costTracker *CostTracker) (*NodeResult, error) {
	startTime := time.Now()
	spanID := uuid.NewString()

	// Extract label from metadata
	nodeLabel := node.DisplayMeta().Label

	// Retry policy is explicit on every retryable node. Deterministic operation
	// nodes are zero-cost and intentionally run once when no policy is present.
	policy := node.RetryPolicy
	if policy == nil {
		if node.Type == NodeTypeOperation {
			policy = NoRetryPolicy()
		} else {
			err := NewNonRetryableError(fmt.Errorf("node retry_policy is required"), "INVALID_CONFIG")
			return &NodeResult{
				NodeID:    nodeID,
				Success:   false,
				Error:     err.Error(),
				LatencyMs: float64(time.Since(startTime).Milliseconds()),
			}, err
		}
	}

	// Generate a stable job ID for execution UIDs
	jobID := getJobID(execCtx)

	var result *NodeResult
	var execErr error
	var attemptCount int
	consecutiveAdaptiveFailures := 0

retryLoop:
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		attemptCount = attempt
		execUID := NodeExecutionID(jobID, nodeID, attempt)
		nodeForAttempt, adaptiveEffort, adapted := EffectiveNodeForAttempt(node, policy, consecutiveAdaptiveFailures)

		// Execute the node via runner registry
		runner, ok := e.runnerRegistry.Get(nodeForAttempt.Type)
		if !ok {
			return nil, fmt.Errorf("unknown node type: %s", nodeForAttempt.Type)
		}
		result, execErr = runner.Execute(&NodeContext{
			Ctx:             ctx,
			Node:            nodeForAttempt,
			NodeID:          nodeID,
			Attempt:         attempt,
			ParentSpanID:    spanID,
			WorkflowContext: workflowContext,
			ExecCtx:         execCtx,
			CostTracker:     costTracker,
		})

		// Stamp attempt tracking on result
		if result != nil {
			result.AttemptNumber = attempt
			result.ExecutionUID = execUID
		}

		// Centralized cost limit enforcement — catches accumulated spend
		// across all node types without requiring each runner to check.
		if execErr == nil && costTracker != nil {
			if limitErr := costTracker.CheckLimits(nodeID); limitErr != nil {
				if result != nil {
					result.Success = false
					result.Error = limitErr.Error()
				}
				execErr = limitErr
			}
		}

		// Success → done
		if execErr == nil && (result == nil || result.Success) {
			break
		}

		// Determine the error to evaluate for retry
		retryErr := execErr
		if retryErr == nil && result != nil && !result.Success {
			retryErr = fmt.Errorf("%s", result.Error)
		}
		retryCode := GetErrorCode(retryErr)
		if policy.IsAdaptiveReasoningTrigger(retryCode) {
			consecutiveAdaptiveFailures++
		} else {
			consecutiveAdaptiveFailures = 0
		}

		// 1. Parent context done → stop immediately (user cancel / shutdown)
		if ctx.Err() != nil {
			break
		}

		// 2. CostLimitError → stop (don't waste more money)
		if IsCostLimitError(retryErr) {
			break
		}

		// 3. Check retry policy
		if !policy.ShouldRetry(retryErr, attempt) {
			// Exhausted or non-retryable → emit exhausted event and stop
			if attempt >= policy.MaxAttempts {
				emitRetryEvent(execCtx, RetryEvent{
					Type:        "retry_exhausted",
					NodeID:      nodeID,
					Attempt:     attempt,
					MaxAttempts: policy.MaxAttempts,
					Error:       retryErr,
					ErrorCode:   GetErrorCode(retryErr),
				})
			}
			break
		}

		// Will retry — compute backoff and emit events
		backoff := policy.GetBackoffDuration(attempt)
		backoffMs := backoff.Milliseconds()

		// Write a trace span for the failed attempt
		if execCtx != nil {
			failedSpanAttrs := map[string]any{
				"node_type":       node.Type,
				"node_label":      nodeLabel,
				"attempt":         attempt,
				"max_attempts":    policy.MaxAttempts,
				"error":           retryErr.Error(),
				"error_code":      retryCode,
				"next_backoff_ms": backoffMs,
			}
			if adapted {
				failedSpanAttrs["adaptive_reasoning_effort"] = adaptiveEffort
			}
			writeSpan(ctx, execCtx, &trace.Span{
				NodeID:       nodeID,
				SpanID:       uuid.NewString(),
				ParentSpanID: spanID,
				Kind:         trace.KindNode,
				Status:       trace.StatusError,
				StartedAt:    startTime,
				DurationMs:   float64(time.Since(startTime).Milliseconds()),
				Attributes:   failedSpanAttrs,
			})
		}

		// Emit retry_backoff event
		emitRetryEvent(execCtx, RetryEvent{
			Type:        "retry_backoff",
			NodeID:      nodeID,
			Attempt:     attempt,
			MaxAttempts: policy.MaxAttempts,
			Error:       retryErr,
			ErrorCode:   retryCode,
			BackoffMs:   backoffMs,
		})

		// Context-aware sleep: use timer + select so we wake on parent cancel
		if backoff > 0 {
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				// Parent cancelled during backoff — stop retrying
				break retryLoop
			case <-timer.C:
			}
		}

		// Emit retry_start event for the next attempt
		emitRetryEvent(execCtx, RetryEvent{
			Type:        "retry_start",
			NodeID:      nodeID,
			Attempt:     attempt + 1,
			MaxAttempts: policy.MaxAttempts,
		})

		// Reset start time for the new attempt
		startTime = time.Now()
	}

	// Write final node span covering the last attempt
	status := trace.StatusOK
	attrs := map[string]any{
		"node_type":  node.Type,
		"node_label": nodeLabel,
	}
	if attemptCount > 1 {
		attrs["attempt"] = attemptCount
		attrs["max_attempts"] = policy.MaxAttempts
	}
	if execErr != nil {
		status = trace.StatusError
		attrs["error"] = execErr.Error()
		if errors.Is(execErr, context.DeadlineExceeded) {
			status = trace.StatusTimeout
		} else if errors.Is(execErr, context.Canceled) {
			status = trace.StatusCancelled
		}
	} else if result != nil && !result.Success {
		status = trace.StatusError
		attrs["error"] = result.Error
	}

	if execCtx != nil {
		writeSpan(ctx, execCtx, &trace.Span{
			NodeID:     nodeID,
			SpanID:     spanID,
			Kind:       trace.KindNode,
			Status:     status,
			StartedAt:  startTime,
			DurationMs: float64(time.Since(startTime).Milliseconds()),
			Attributes: attrs,
		})
	}

	return result, execErr
}

// ExecuteNode is the exported entry point for node execution, satisfying the
// NodeExecutor interface. It delegates to the unexported executeNode method.
func (e *Executor) ExecuteNode(ctx context.Context, node *Node, nodeID string, workflowContext map[string]interface{}, execCtx *ExecutionContext, costTracker *CostTracker) (*NodeResult, error) {
	return e.executeNode(ctx, node, nodeID, workflowContext, execCtx, costTracker)
}

// DeepCopyContext performs a recursive deep copy of a workflow context map.
// This prevents data races when parallel nodes share a context snapshot.
// Exported so that other packages (durable runtime, bench, etc.) can reuse it
// instead of maintaining duplicate shallow-copy helpers.
func DeepCopyContext(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return make(map[string]interface{})
	}
	dst := make(map[string]interface{}, len(src))
	for k, v := range src {
		dst[k] = deepCopyValue(v)
	}
	return dst
}

// deepCopyValue recursively copies a value. Nested maps and slices are cloned;
// scalar types (string, int, float64, bool, nil) are returned as-is.
func deepCopyValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		return DeepCopyContext(val)
	case []interface{}:
		cp := make([]interface{}, len(val))
		for i, elem := range val {
			cp[i] = deepCopyValue(elem)
		}
		return cp
	default:
		return v
	}
}

// InterpolateVariables replaces {{variable}} placeholders with values from context.
func InterpolateVariables(text string, context map[string]interface{}) string {
	return varRegex.ReplaceAllStringFunc(text, func(match string) string {
		// Extract variable name
		varName := strings.TrimSpace(match[2 : len(match)-2])

		// Look up value in context
		if val, ok := context[varName]; ok {
			return fmt.Sprintf("%v", val)
		}
		return match // Return original if not found
	})
}
