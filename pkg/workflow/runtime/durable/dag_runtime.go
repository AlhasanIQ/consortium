package durable

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

// DAGRuntime implements the ExecutionRuntime interface using deterministic
// event-driven scheduling with durable history.
type DAGRuntime struct {
	store    *storage.Storage
	handlers *runtime.ActivityHandlerRegistry
	sleepFn  func(ctx context.Context, d time.Duration) bool

	// Running executions for cancellation support
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

type startRunContext struct {
	execID           string
	runID            string
	jobID            string
	deps             map[string][]string
	nodeIDs          []string
	nodeMap          map[string]*workflow.Node
	totalNodes       int
	historyEvents    []*runtime.HistoryEvent
	state            *SchedulerState
	alreadyCompleted bool
	workflowCtx      map[string]interface{}
	nodeIndex        int
}

// NewDAGRuntime creates a new durable DAG runtime.
func NewDAGRuntime(store *storage.Storage, handlers *runtime.ActivityHandlerRegistry) *DAGRuntime {
	return &DAGRuntime{
		store:    store,
		handlers: handlers,
		sleepFn:  sleepWithContext,
		cancels:  make(map[string]context.CancelFunc),
	}
}

// resultAction signals how the main loop should proceed after processing a result.
type resultAction int

const (
	resultContinue resultAction = iota // keep processing results
	resultBreak                        // break out of the result loop (terminal for this level)
	resultAbort                        // break out of mainLoop entirely (runtimeErr set)
)

// Start begins execution of a workflow. It blocks until the workflow completes,
// fails, or is cancelled.
func (r *DAGRuntime) Start(ctx context.Context, params *runtime.StartParams) error {
	if err := prepareStartParams(params); err != nil {
		return err
	}

	execID := params.Identity.WorkflowExecutionID

	// Set up cancellation tracking
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	r.mu.Lock()
	r.cancels[execID] = cancel
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.cancels, execID)
		r.mu.Unlock()
	}()

	startCtx, err := r.prepareRunContext(ctx, params)
	if err != nil {
		return err
	}

	runID := startCtx.runID
	jobID := startCtx.jobID
	deps := startCtx.deps
	nodeMap := startCtx.nodeMap
	totalNodes := startCtx.totalNodes
	state := startCtx.state
	workflowCtx := startCtx.workflowCtx
	nodeIndex := startCtx.nodeIndex
	readyQueue := newReadyQueue(state, deps, startCtx.nodeIDs)

	var runtimeErr error

mainLoop:
	for !state.IsTerminal() {
		// Check for context cancellation.
		if ctx.Err() != nil {
			runtimeErr = r.handleCancellation(params, state, jobID, runID, nil)
			break mainLoop
		}

		// Compute ready set.
		ready := readyQueue.ReadySet()
		if len(ready) == 0 {
			if state.AllNodesTerminal() {
				break mainLoop
			}

			runtimeErr = fmt.Errorf("scheduler made no progress for run %s", runID)
			if histErr := r.appendHistory(ctx, runID, runtime.HistoryWorkflowFailed, "", "", map[string]interface{}{
				"error":  runtimeErr.Error(),
				"reason": "no_progress",
			}); histErr != nil {
				runtimeErr = fmt.Errorf("%w (additionally failed to append workflow_failed: %v)", runtimeErr, histErr)
			}
			state.Failed = true
			r.emitEvent(params, &runtime.ExecutionEvent{
				Type: events.EventError,
				Payload: map[string]interface{}{
					"job_id":  jobID,
					"message": runtimeErr.Error(),
					"error":   runtimeErr.Error(),
				},
			})
			break mainLoop
		}

		// Dispatch ready nodes and collect results.
		nodeIndex, runtimeErr = r.dispatchAndCollect(ctx, &dispatchRequest{
			params:      params,
			state:       state,
			ready:       ready,
			nodeMap:     nodeMap,
			workflowCtx: workflowCtx,
			jobID:       jobID,
			runID:       runID,
			totalNodes:  totalNodes,
			nodeIndex:   nodeIndex,
		})
		if runtimeErr != nil {
			break mainLoop
		}
	}

	startCtx.state = state
	startCtx.workflowCtx = workflowCtx
	startCtx.nodeIndex = nodeIndex
	return r.finalizeRun(ctx, params, startCtx, runtimeErr)
}

// handleCancellation records cancellation in history, marks state, and emits
// the cancellation event. It returns a non-nil error only if appending the
// history event fails. extraAttrs are merged into the history attributes.
func (r *DAGRuntime) handleCancellation(params *runtime.StartParams, state *SchedulerState, jobID, runID string, extraAttrs map[string]interface{}) error {
	r.markRunningNodesCancelled(jobID, runID)
	var runtimeErr error
	if histErr := r.appendHistory(context.Background(), runID, runtime.HistoryWorkflowCancelled, "", "", extraAttrs); histErr != nil {
		runtimeErr = fmt.Errorf("failed to append workflow_cancelled: %w", histErr)
	}
	state.Cancelled = true
	r.emitEvent(params, &runtime.ExecutionEvent{
		Type: events.EventCancelled,
		Payload: map[string]interface{}{
			"job_id":  jobID,
			"message": "Workflow cancelled",
		},
	})
	return runtimeErr
}

// dispatchRequest holds the parameters for dispatchAndCollect.
type dispatchRequest struct {
	params      *runtime.StartParams
	state       *SchedulerState
	ready       []string
	nodeMap     map[string]*workflow.Node
	workflowCtx map[string]interface{}
	jobID       string
	runID       string
	totalNodes  int
	nodeIndex   int
}

// dispatchAndCollect dispatches all ready nodes as goroutines, collects their
// results, and processes each one. It returns the updated nodeIndex and a
// non-nil error if the loop should abort.
func (r *DAGRuntime) dispatchAndCollect(ctx context.Context, req *dispatchRequest) (int, error) {
	params := req.params
	nodeIndex := req.nodeIndex
	batchCtx, cancelBatch := context.WithCancel(ctx)
	defer cancelBatch()

	var wg sync.WaitGroup
	resultCh := make(chan *activityResult, len(req.ready))
	maxParallel := len(req.ready)
	if params.ExecCtx != nil && params.ExecCtx.MaxParallelNodes > 0 && params.ExecCtx.MaxParallelNodes < maxParallel {
		maxParallel = params.ExecCtx.MaxParallelNodes
	}
	if maxParallel < 1 {
		maxParallel = 1
	}
	sem := make(chan struct{}, maxParallel)

	var runtimeErr error

dispatchLoop:
	for _, nodeID := range req.ready {
		node := req.nodeMap[nodeID]
		if node == nil {
			errMsg := fmt.Sprintf("frozen snapshot node %s missing from workflow definition", nodeID)
			runtimeErr = errors.New(errMsg)
			if histErr := r.appendHistory(ctx, req.runID, runtime.HistoryWorkflowFailed, "", "", map[string]interface{}{
				"error":   errMsg,
				"reason":  "snapshot_node_missing",
				"node_id": nodeID,
			}); histErr != nil {
				runtimeErr = fmt.Errorf("%w (additionally failed to append workflow_failed: %v)", runtimeErr, histErr)
			}
			req.state.Failed = true
			r.emitEvent(params, &runtime.ExecutionEvent{
				Type: events.EventError,
				Payload: map[string]interface{}{
					"job_id":  req.jobID,
					"message": errMsg,
					"error":   errMsg,
					"node_id": nodeID,
				},
			})
			break dispatchLoop
		}

		nodeIndex++
		attempt := req.state.ActivityAttempts[nodeID] + 1
		retryPolicy := resolveNodeRetryPolicy(node)
		effectiveNode, adaptiveEffort, adaptiveApplied := workflow.EffectiveNodeForAttempt(node, retryPolicy, req.state.AdaptiveFailures[nodeID])
		activityID := fmt.Sprintf("%s:%s:%d", req.jobID, nodeID, attempt)

		scheduleAttrs := map[string]interface{}{
			"attempt":       float64(attempt),
			"activity_type": string(runtime.NodeTypeToActivityType(effectiveNode.Type)),
		}
		if adaptiveApplied {
			scheduleAttrs["adaptive_reasoning_effort"] = adaptiveEffort
		}
		if err := r.appendHistoryBatch(ctx, req.runID, []historyBatchEntry{
			{eventType: runtime.HistoryScheduleActivity, nodeID: nodeID, activityID: activityID, attrs: scheduleAttrs},
			{eventType: runtime.HistoryActivityStarted, nodeID: nodeID, activityID: activityID, attrs: map[string]interface{}{
				"attempt": float64(attempt),
			}},
		}); err != nil {
			runtimeErr = fmt.Errorf("failed to append schedule+started for %s: %w", nodeID, err)
			req.state.Failed = true
			break dispatchLoop
		}
		req.state.Nodes[nodeID] = NodeStateRunning
		req.state.ActivityAttempts[nodeID] = attempt
		// Attempt upserts use context.Background() intentionally: they are best-effort
		// observability records and must not be cancelled when the workflow context is done.
		// History appends (the source of truth) use the parent ctx and abort on failure.
		if uErr := r.store.UpsertNodeExecutionAttempt(context.Background(), &storage.NodeExecutionAttempt{
			JobID:       req.jobID,
			ExecutionID: params.Identity.WorkflowExecutionID,
			RunID:       req.runID,
			NodeID:      nodeID,
			NodeType:    string(effectiveNode.Type),
			Attempt:     attempt,
			Status:      "running",
			ActivityID:  activityID,
			StartedAt:   ptrTime(time.Now()),
			Metadata:    buildAttemptMeta(effectiveNode, nil),
		}); uErr != nil {
			log.Printf("dag_runtime: failed to upsert node attempt (job=%s node=%s): %v", req.jobID, nodeID, uErr)
		}

		r.emitNodeStart(params, effectiveNode, nodeIndex, req.totalNodes, req.jobID)

		// Dispatch with a per-activity context snapshot to avoid concurrent map access.
		activityCtx := workflow.DeepCopyContext(req.workflowCtx)
		activityStartedAt := time.Now()
		sem <- struct{}{}
		wg.Add(1)
		go func(nID string, n *workflow.Node, aID string, att int, idx int, wfCtx map[string]interface{}, startedAt time.Time) {
			defer wg.Done()
			defer func() { <-sem }()
			output := r.executeActivity(batchCtx, params, nID, n, aID, att, wfCtx)
			resultCh <- &activityResult{
				nodeID:     nID,
				activityID: aID,
				attempt:    att,
				output:     output,
				nodeIndex:  idx,
				node:       n,
				startedAt:  startedAt,
			}
		}(nodeID, effectiveNode, activityID, attempt, nodeIndex, activityCtx, activityStartedAt)
	}

	if runtimeErr != nil {
		cancelBatch()
		wg.Wait()
		r.markRunningNodesCancelled(req.jobID, req.runID)
		return nodeIndex, runtimeErr
	}

	go func() {
		wg.Wait()
		close(resultCh)
	}()

	quiescing := false
	for res := range resultCh {
		// A terminal result already won this batch. Drain every launched activity so
		// Start cannot return while sibling work is still running, but do not process
		// sibling cancellation as another node/workflow failure.
		if quiescing {
			continue
		}

		// Cancellation takes precedence over node failure classification.
		if ctx.Err() != nil {
			runtimeErr = r.handleCancellation(params, req.state, req.jobID, req.runID, map[string]interface{}{
				"reason": "context_cancelled",
			})
			quiescing = true
			cancelBatch()
			continue
		}

		var action resultAction
		if res.output.Success {
			action, runtimeErr = r.processActivitySuccess(ctx, params, req.state, res, req.workflowCtx, req.jobID, req.runID, req.totalNodes)
		} else {
			action, runtimeErr = r.processActivityFailure(ctx, params, req.state, res, req.jobID, req.runID, req.totalNodes)
		}
		if action == resultBreak || action == resultAbort {
			quiescing = true
			cancelBatch()
		}
	}

	if quiescing && req.state.Failed {
		// The winning failure terminalized its own attempt. Any rows still marked
		// running belong to siblings cancelled solely to quiesce this failed batch.
		r.markRunningNodesCancelled(req.jobID, req.runID)
	}

	return nodeIndex, runtimeErr
}

// processActivitySuccess handles a successful activity result: records the
// completion in history and state, updates workflow context, persists the node,
// and checks cost limits. It returns a resultAction indicating whether the
// result loop should continue, break, or abort.
func (r *DAGRuntime) processActivitySuccess(
	ctx context.Context,
	params *runtime.StartParams,
	state *SchedulerState,
	res *activityResult,
	workflowCtx map[string]interface{},
	jobID, runID string,
	totalNodes int,
) (resultAction, error) {
	state.AdaptiveFailures[res.nodeID] = 0
	now := time.Now()
	if uErr := r.store.UpsertNodeExecutionAttempt(context.Background(), &storage.NodeExecutionAttempt{
		JobID:        jobID,
		ExecutionID:  params.Identity.WorkflowExecutionID,
		RunID:        runID,
		NodeID:       res.nodeID,
		NodeType:     string(res.node.Type),
		Attempt:      res.attempt,
		Status:       "completed",
		ActivityID:   res.activityID,
		StartedAt:    ptrTime(res.startedAt),
		CompletedAt:  &now,
		LatencyMs:    res.output.LatencyMs,
		TokensInput:  res.output.TokensInput,
		TokensOutput: res.output.TokensOutput,
		Cost:         res.output.Cost,
		Metadata:     buildAttemptMeta(res.node, res.output.Metadata),
	}); uErr != nil {
		log.Printf("dag_runtime: failed to upsert completed attempt (job=%s node=%s): %v", jobID, res.nodeID, uErr)
	}

	if err := r.appendHistory(ctx, runID, runtime.HistoryActivityCompleted, res.nodeID, res.activityID, map[string]interface{}{
		"output":        res.output.Output,
		"tokens_input":  res.output.TokensInput,
		"tokens_output": res.output.TokensOutput,
		"cost":          res.output.Cost,
		"latency_ms":    res.output.LatencyMs,
	}); err != nil {
		state.Failed = true
		return resultAbort, fmt.Errorf("failed to append activity_completed for %s: %w", res.nodeID, err)
	}
	state.Nodes[res.nodeID] = NodeStateCompleted
	state.NodeOutputs[res.nodeID] = res.output.Output
	delete(state.PendingActivities, res.activityID)

	workflowCtx[res.nodeID] = res.output.Output
	if res.node != nil && res.node.Model != "" {
		workflowCtx[res.nodeID+"__model"] = res.node.Model
	}
	if res.output.Metadata != nil {
		if outputName, ok := res.output.Metadata["output_name"].(string); ok && outputName != "" {
			if outputValue, ok := res.output.Metadata["output_value"].(string); ok {
				workflowCtx[fmt.Sprintf("__output_%s", outputName)] = outputValue
			}
		}
	}

	// NOTE: CostTracker.Add is called by providers.Client for successful LLM calls.
	// Do not add again here or we'll double count.

	r.emitNodeComplete(params, res.node, res.output, jobID)
	r.saveWorkflowNode(jobID, runID, params.Identity.WorkflowExecutionID, res.node, res.output, res.attempt, res.activityID, res.nodeIndex, totalNodes)

	if params.CostTracker != nil {
		if limitErr := params.CostTracker.CheckLimits(res.nodeID); limitErr != nil {
			state.NodeErrors[res.nodeID] = limitErr.Error()
			var runtimeErr error
			if err := r.appendHistory(ctx, runID, runtime.HistoryWorkflowFailed, "", "", map[string]interface{}{
				"error":  limitErr.Error(),
				"reason": "cost_limit_exceeded",
			}); err != nil {
				runtimeErr = fmt.Errorf("cost limit exceeded and failed to append workflow_failed: %w", err)
			}
			state.Failed = true
			r.emitEvent(params, &runtime.ExecutionEvent{
				Type: events.EventError,
				Payload: map[string]interface{}{
					"job_id":  jobID,
					"message": limitErr.Error(),
					"error":   limitErr.Error(),
				},
			})
			return resultBreak, runtimeErr
		}
	}
	return resultContinue, nil
}

// processActivityFailure handles a failed activity result: determines whether
// to retry or terminally fail the node, records appropriate history, and emits
// events. It returns a resultAction indicating how the result loop should proceed.
func (r *DAGRuntime) processActivityFailure(
	ctx context.Context,
	params *runtime.StartParams,
	state *SchedulerState,
	res *activityResult,
	jobID, runID string,
	totalNodes int,
) (resultAction, error) {
	errMsg := res.output.Error
	if errMsg == "" {
		errMsg = "activity failed"
	}
	retryPolicy := resolveNodeRetryPolicy(res.node)
	if retryPolicy != nil && retryPolicy.IsAdaptiveReasoningTrigger(res.output.ErrorCode) {
		state.AdaptiveFailures[res.nodeID]++
	} else {
		state.AdaptiveFailures[res.nodeID] = 0
	}
	now := time.Now()
	if uErr := r.store.UpsertNodeExecutionAttempt(context.Background(), &storage.NodeExecutionAttempt{
		JobID:        jobID,
		ExecutionID:  params.Identity.WorkflowExecutionID,
		RunID:        runID,
		NodeID:       res.nodeID,
		NodeType:     string(res.node.Type),
		Attempt:      res.attempt,
		Status:       "failed",
		ActivityID:   res.activityID,
		StartedAt:    ptrTime(res.startedAt),
		CompletedAt:  &now,
		LatencyMs:    res.output.LatencyMs,
		TokensInput:  res.output.TokensInput,
		TokensOutput: res.output.TokensOutput,
		Cost:         res.output.Cost,
		ErrorCode:    res.output.ErrorCode,
		ErrorMessage: errMsg,
		Metadata:     buildAttemptMeta(res.node, res.output.Metadata),
	}); uErr != nil {
		log.Printf("dag_runtime: failed to upsert failed attempt (job=%s node=%s): %v", jobID, res.nodeID, uErr)
	}
	nonRetryable := runtime.IsNonRetryable(fmt.Errorf("%s", errMsg), res.output.ErrorCode)
	willRetry := retryPolicy != nil && res.attempt < retryPolicy.MaxAttempts && !nonRetryable

	if willRetry {
		return r.processActivityRetry(ctx, params, state, res, retryPolicy, jobID, runID, errMsg)
	}
	return r.processActivityTerminalFailure(ctx, params, state, res, jobID, runID, totalNodes, errMsg)
}

// processActivityRetry handles the retry path for a failed activity: records
// activity_failed in history, resets the node to pending, emits retry events,
// and sleeps for the backoff duration.
func (r *DAGRuntime) processActivityRetry(
	ctx context.Context,
	params *runtime.StartParams,
	state *SchedulerState,
	res *activityResult,
	retryPolicy *workflow.RetryPolicy,
	jobID, runID, errMsg string,
) (resultAction, error) {
	if err := r.appendHistory(ctx, runID, runtime.HistoryActivityFailed, res.nodeID, res.activityID, map[string]interface{}{
		"error":      errMsg,
		"error_code": res.output.ErrorCode,
		"attempt":    float64(res.attempt),
	}); err != nil {
		state.Failed = true
		return resultAbort, fmt.Errorf("failed to append activity_failed for %s: %w", res.nodeID, err)
	}
	state.Nodes[res.nodeID] = NodeStatePending
	r.emitRetryEvents(params, res.node, res.attempt, retryPolicy, jobID, errMsg)

	backoff := retryPolicy.GetBackoffDuration(res.attempt)
	r.sleepFn(ctx, backoff)
	return resultContinue, nil
}

// processActivityTerminalFailure handles a terminal (non-retryable or exhausted)
// activity failure: records activity_failed + node_failed atomically, emits
// failure events, and marks the workflow as failed.
func (r *DAGRuntime) processActivityTerminalFailure(
	ctx context.Context,
	params *runtime.StartParams,
	state *SchedulerState,
	res *activityResult,
	jobID, runID string,
	totalNodes int,
	errMsg string,
) (resultAction, error) {
	if err := r.appendHistoryBatch(ctx, runID, []historyBatchEntry{
		{eventType: runtime.HistoryActivityFailed, nodeID: res.nodeID, activityID: res.activityID, attrs: map[string]interface{}{
			"error":      errMsg,
			"error_code": res.output.ErrorCode,
			"attempt":    float64(res.attempt),
		}},
		{eventType: runtime.HistoryNodeFailed, nodeID: res.nodeID, attrs: map[string]interface{}{
			"error":    errMsg,
			"attempts": float64(res.attempt),
		}},
	}); err != nil {
		state.Failed = true
		return resultAbort, fmt.Errorf("failed to append activity_failed+node_failed for %s: %w", res.nodeID, err)
	}
	state.Nodes[res.nodeID] = NodeStateFailed
	state.NodeErrors[res.nodeID] = errMsg

	r.saveFailedNode(jobID, runID, params.Identity.WorkflowExecutionID, res.node, errMsg, res.output.ErrorCode, res.output.Metadata, res.attempt, res.activityID, res.nodeIndex, totalNodes)

	r.emitEvent(params, &runtime.ExecutionEvent{
		Type: events.EventNodeFailed,
		Payload: map[string]interface{}{
			"job_id":     jobID,
			"node_id":    res.nodeID,
			"message":    fmt.Sprintf("Node %s failed: %s", res.nodeID, errMsg),
			"error":      errMsg,
			"node_label": extractLabel(res.node),
		},
	})

	if res.attempt > 1 {
		r.emitEvent(params, &runtime.ExecutionEvent{
			Type: events.EventNodeRetryExhausted,
			Payload: map[string]interface{}{
				"job_id":         jobID,
				"node_id":        res.nodeID,
				"message":        fmt.Sprintf("All %d retry attempts exhausted for %s", res.attempt, res.nodeID),
				"total_attempts": res.attempt,
			},
		})
	}

	var runtimeErr error
	if err := r.appendHistory(ctx, runID, runtime.HistoryWorkflowFailed, "", "", map[string]interface{}{
		"error":       errMsg,
		"failed_node": res.nodeID,
	}); err != nil {
		runtimeErr = fmt.Errorf("failed to append workflow_failed for %s: %w", res.nodeID, err)
	}
	state.Failed = true

	r.emitEvent(params, &runtime.ExecutionEvent{
		Type: events.EventError,
		Payload: map[string]interface{}{
			"job_id":  jobID,
			"message": fmt.Sprintf("Workflow failed: %s", errMsg),
			"error":   errMsg,
		},
	})

	return resultBreak, runtimeErr
}

func prepareStartParams(params *runtime.StartParams) error {
	if params == nil || params.Identity == nil || params.Snapshot == nil || params.Workflow == nil {
		return fmt.Errorf("identity, snapshot, and workflow are required")
	}
	if params.CostTracker == nil {
		params.CostTracker = workflow.NewCostTracker(params.Workflow.Limits)
	}
	return nil
}

func buildNodeMap(nodes []*workflow.Node) map[string]*workflow.Node {
	nodeMap := make(map[string]*workflow.Node, len(nodes))
	for _, n := range nodes {
		if n == nil {
			continue
		}
		nodeMap[n.ID] = n
	}
	return nodeMap
}

func (r *DAGRuntime) prepareRunContext(ctx context.Context, params *runtime.StartParams) (*startRunContext, error) {
	execID := params.Identity.WorkflowExecutionID
	runID := params.Identity.RunID
	jobID := execID // job_id == workflow_execution_id in Phase 1

	// Build dependency map from frozen snapshot.
	deps, nodeIDs, err := BuildDeps(params.Snapshot)
	if err != nil {
		// Fail the job so callers can observe the terminal state.
		if stErr := r.store.UpdateExecutionStatus(jobID, events.JobStatusFailed); stErr != nil {
			log.Printf("dag_runtime: failed to update execution status for job %s: %v", jobID, stErr)
		}
		if stErr := r.store.UpdateExecutionError(jobID, err.Error()); stErr != nil {
			log.Printf("dag_runtime: failed to update execution error for job %s: %v", jobID, stErr)
		}
		return nil, fmt.Errorf("failed to build deps from snapshot: %w", err)
	}

	totalNodes := len(nodeIDs)
	nodeMap := buildNodeMap(params.Workflow.Nodes)

	// Load existing history for replay (supports resume).
	historyEvents, err := r.loadRuntimeHistoryEvents(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to load history: %w", err)
	}

	// If fresh run, record workflow_started.
	freshRun := len(historyEvents) == 0
	if freshRun {
		if err := r.appendHistory(ctx, runID, runtime.HistoryWorkflowStarted, "", "", map[string]interface{}{
			"workflow_id": params.Workflow.ID,
			"total_nodes": totalNodes,
			"dag_hash":    params.Identity.DAGHash,
		}); err != nil {
			return nil, fmt.Errorf("failed to record workflow_started: %w", err)
		}

		seeded, replayErr := r.applyReplaySeeds(ctx, params, runID, jobID, nodeIDs, nodeMap, totalNodes)
		if replayErr != nil {
			return nil, replayErr
		}
		if seeded > 0 {
			log.Printf("Replay seeded %d node(s) for run %s", seeded, runID)
		}

		// Refresh history after workflow_started and optional replay seeding.
		historyEvents, err = r.loadRuntimeHistoryEvents(ctx, runID)
		if err != nil {
			return nil, fmt.Errorf("failed to refresh history after replay seeding: %w", err)
		}
	}

	// Replay to reconstruct state.
	state := Replay(historyEvents, nodeIDs)
	alreadyCompleted := state.Completed
	if requeued := state.RequeueInFlightNodes(); requeued > 0 {
		r.markRunningNodesInterrupted(jobID, runID)
		r.emitEvent(params, &runtime.ExecutionEvent{
			Type: events.EventStatus,
			Payload: map[string]interface{}{
				"job_id":  jobID,
				"message": fmt.Sprintf("Resuming workflow: re-dispatching %d in-flight node(s)", requeued),
			},
		})
	}

	// Ensure DB status reflects active execution.
	if err := r.store.UpdateExecutionStatus(jobID, events.JobStatusRunning); err != nil {
		log.Printf("Warning: failed to update job status to running: %v", err)
	}

	// Emit status event for fresh runs.
	if freshRun {
		r.emitEvent(params, &runtime.ExecutionEvent{
			Type: events.EventStatus,
			Payload: map[string]interface{}{
				"job_id":  jobID,
				"message": fmt.Sprintf("Starting workflow: %s", params.Workflow.Name),
			},
		})
	}

	workflowCtx := buildWorkflowContext(params.Workflow.Context, state.NodeOutputs)
	applyReplayWorkflowContext(workflowCtx, params.Replay)

	// Restore __model keys for completed nodes from the frozen workflow definition.
	// Uses state.NodeOutputs (not workflowCtx) to avoid false positives from initial context keys.
	for _, node := range params.Workflow.Nodes {
		if node.Model != "" {
			if _, completed := state.NodeOutputs[node.ID]; completed {
				workflowCtx[node.ID+"__model"] = node.Model
			}
		}
	}

	nodeIndex := completedNodeCount(nodeIDs, state)

	return &startRunContext{
		execID:           execID,
		runID:            runID,
		jobID:            jobID,
		deps:             deps,
		nodeIDs:          nodeIDs,
		nodeMap:          nodeMap,
		totalNodes:       totalNodes,
		historyEvents:    historyEvents,
		state:            state,
		alreadyCompleted: alreadyCompleted,
		workflowCtx:      workflowCtx,
		nodeIndex:        nodeIndex,
	}, nil
}

func (r *DAGRuntime) finalizeRun(ctx context.Context, params *runtime.StartParams, startCtx *startRunContext, runtimeErr error) error {
	if startCtx == nil || startCtx.state == nil {
		return runtimeErr
	}

	runID := startCtx.runID
	jobID := startCtx.jobID
	nodeIDs := startCtx.nodeIDs
	state := startCtx.state

	// Refresh history before finalization so totals can include events appended
	// during this runtime invocation (including resumed executions).
	finalHistoryEvents := startCtx.historyEvents
	if refreshedHistory, err := r.loadRuntimeHistoryEvents(ctx, runID); err != nil {
		log.Printf("Warning: failed to refresh history for totals on run %s: %v", runID, err)
	} else {
		finalHistoryEvents = refreshedHistory
	}

	finalStatus := events.JobStatusCompleted
	switch {
	case state.Cancelled:
		finalStatus = events.JobStatusCancelled
	case runtimeErr != nil || state.Failed:
		finalStatus = events.JobStatusFailed
	case startCtx.alreadyCompleted:
		// Terminal completion already exists in durable history (resume path).
		// Avoid duplicate workflow_completed append/emission.
	default:
		cost, tokens, inputTokens, outputTokens := resolveExecutionTotals(params.CostTracker, finalHistoryEvents)
		if err := r.appendHistory(ctx, runID, runtime.HistoryWorkflowCompleted, "", "", map[string]interface{}{
			"total_cost":          cost,
			"total_tokens":        tokens,
			"total_input_tokens":  inputTokens,
			"total_output_tokens": outputTokens,
		}); err != nil {
			runtimeErr = fmt.Errorf("failed to append workflow_completed: %w", err)
			finalStatus = events.JobStatusFailed
			state.Failed = true
		} else {
			finalOutput := resolveFinalOutput(nodeIDs, state.NodeOutputs)
			r.emitEvent(params, &runtime.ExecutionEvent{
				Type: events.EventComplete,
				Payload: map[string]interface{}{
					"job_id":       jobID,
					"message":      "Workflow completed successfully",
					"final_output": finalOutput,
					"total_cost":   cost,
					"total_tokens": tokens,
				},
			})
		}
	}

	// Atomically finalize the job with status, result, and error in a single write.
	var finalOutput, errMsg string
	var cost float64
	var inputTokens, outputTokens, totalTokens int

	switch finalStatus {
	case events.JobStatusCompleted:
		finalOutput = resolveFinalOutput(nodeIDs, state.NodeOutputs)
		cost, totalTokens, inputTokens, outputTokens = resolveExecutionTotals(params.CostTracker, finalHistoryEvents)
	case events.JobStatusFailed:
		errMsg = resolveFinalError(runtimeErr, nodeIDs, state.NodeErrors)
	}

	if err := r.store.CompleteExecution(jobID, finalStatus, finalOutput, cost, inputTokens, outputTokens, totalTokens, errMsg); err != nil {
		log.Printf("Warning: failed to complete execution for %s: %v", jobID, err)
	}

	if runtimeErr != nil {
		return runtimeErr
	}
	return nil
}

func (r *DAGRuntime) loadRuntimeHistoryEvents(ctx context.Context, runID string) ([]*runtime.HistoryEvent, error) {
	existingEvents, err := r.store.GetHistoryEvents(ctx, runID)
	if err != nil {
		return nil, err
	}
	return toRuntimeHistoryEvents(existingEvents), nil
}

func toRuntimeHistoryEvents(events []*storage.HistoryEvent) []*runtime.HistoryEvent {
	historyEvents := make([]*runtime.HistoryEvent, len(events))
	for i, e := range events {
		historyEvents[i] = &runtime.HistoryEvent{
			ID:         e.ID,
			RunID:      e.RunID,
			Sequence:   e.Sequence,
			Type:       runtime.HistoryEventType(e.Type),
			NodeID:     e.NodeID,
			ActivityID: e.ActivityID,
			Timestamp:  e.Timestamp,
			Attributes: e.Attributes,
		}
	}
	return historyEvents
}

func buildWorkflowContext(initial map[string]interface{}, nodeOutputs map[string]interface{}) map[string]interface{} {
	workflowCtx := make(map[string]interface{}, len(initial)+len(nodeOutputs))
	for k, v := range initial {
		workflowCtx[k] = v
	}
	for nodeID, output := range nodeOutputs {
		if s, ok := output.(string); ok {
			workflowCtx[nodeID] = s
		}
	}
	return workflowCtx
}

func completedNodeCount(nodeIDs []string, state *SchedulerState) int {
	if state == nil {
		return 0
	}
	completed := 0
	for _, id := range nodeIDs {
		if state.Nodes[id] == NodeStateCompleted {
			completed++
		}
	}
	return completed
}

func resolveFinalOutput(nodeIDs []string, nodeOutputs map[string]interface{}) string {
	for i := len(nodeIDs) - 1; i >= 0; i-- {
		if out, ok := nodeOutputs[nodeIDs[i]]; ok {
			if s, ok := out.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func resolveFinalError(runtimeErr error, nodeIDs []string, nodeErrors map[string]string) string {
	if runtimeErr != nil {
		return runtimeErr.Error()
	}
	for _, nodeID := range nodeIDs {
		if e, ok := nodeErrors[nodeID]; ok && e != "" {
			return e
		}
	}
	return ""
}

// Cancel cancels a running execution.
func (r *DAGRuntime) Cancel(ctx context.Context, execID string) error {
	r.mu.Lock()
	cancelFn, ok := r.cancels[execID]
	r.mu.Unlock()
	if ok {
		cancelFn()
	}
	return nil
}

func resolveNodeRetryPolicy(node *workflow.Node) *workflow.RetryPolicy {
	if node == nil {
		return nil
	}
	return node.RetryPolicy
}

// buildAttemptMeta constructs per-attempt metadata from the node's reasoning
// config and (optionally) the activity output metadata (e.g. child_job_id).
func buildAttemptMeta(node *workflow.Node, outputMeta map[string]interface{}) map[string]interface{} {
	meta := make(map[string]interface{})

	if effort := workflow.NodeReasoningEffort(node); effort != "" && effort != "none" {
		meta["reasoning_effort"] = effort
	}
	if cid, ok := outputMeta["child_job_id"].(string); ok && cid != "" {
		meta["child_job_id"] = cid
	}
	for key, value := range workflow.SelectAttemptObservabilityMetadata(outputMeta) {
		meta[key] = value
	}

	if len(meta) == 0 {
		return nil
	}
	return meta
}

// Ensure DAGRuntime implements ExecutionRuntime
var _ runtime.ExecutionRuntime = (*DAGRuntime)(nil)
