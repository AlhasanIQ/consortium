package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"sync/atomic"
	"time"

	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/typeconv"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

// activityResult bundles the result of a dispatched activity.
type activityResult struct {
	nodeID     string
	activityID string
	attempt    int
	output     *runtime.ActivityOutput
	nodeIndex  int
	node       *workflow.Node
	startedAt  time.Time
}

// executeActivity dispatches an activity to the appropriate handler.
// It checks for cached idempotent results before executing and applies
// start-to-close timeout enforcement.
func (r *DAGRuntime) executeActivity(
	ctx context.Context,
	params *runtime.StartParams,
	nodeID string,
	node *workflow.Node,
	activityID string,
	attempt int,
	workflowCtx map[string]interface{},
) *runtime.ActivityOutput {
	actType := runtime.NodeTypeToActivityType(node.Type)
	jobID := params.Identity.WorkflowExecutionID
	idempotencyKey := fmt.Sprintf("%s:%s:%d", jobID, nodeID, attempt)

	// Check for cached idempotent result (activity_results table)
	cached, err := r.store.GetActivityResult(ctx, idempotencyKey)
	if err != nil {
		log.Printf("Warning: failed to check activity result cache: %v", err)
	}
	if cached != nil && (cached.Status == "completed" || cached.Status == "failed") {
		cachedOut := decodeCachedActivityOutput(nodeID, cached)
		if cachedOut.Success && params.CostTracker != nil {
			params.CostTracker.Add(cachedOut.TokensInput, cachedOut.TokensOutput, cachedOut.Cost)
		}
		return cachedOut
	}

	handler, err := r.handlers.Get(actType)
	if err != nil {
		return &runtime.ActivityOutput{
			NodeID:  nodeID,
			Success: false,
			Error:   fmt.Sprintf("no handler for activity type %s: %v", actType, err),
		}
	}

	// Apply start-to-close timeout
	timeout := runtime.EffectiveStartToClose(nil, node.TimeoutSeconds)
	if node.Type == workflow.NodeTypeAgentRun || node.Type == workflow.NodeTypeNovoRun {
		timeout += 10 * time.Second
	}
	actCtx, actCancel := context.WithTimeout(ctx, timeout)
	defer actCancel()

	heartbeatTimeout := heartbeatTimeoutFromNode(node)
	var heartbeatTimedOut atomic.Bool
	var reporter *HeartbeatReporter
	if heartbeatTimeout > 0 {
		reporter = NewHeartbeatReporter(activityID, nodeID, heartbeatTimeout, actCancel)
		actCtx = ContextWithHeartbeat(actCtx, reporter)
		go MonitorHeartbeats(actCtx, reporter, func() {
			heartbeatTimedOut.Store(true)
		})
	}

	input := &runtime.ActivityInput{
		ActivityID:      activityID,
		NodeID:          nodeID,
		Type:            actType,
		Node:            node,
		WorkflowContext: workflowCtx,
		Attempt:         attempt,
		IdempotencyKey:  idempotencyKey,
		ExecCtx:         params.ExecCtx,
		CostTracker:     params.CostTracker,
	}

	output, err := handler.Execute(actCtx, input)

	// Stop heartbeat monitor BEFORE checking heartbeatTimedOut to avoid a race
	// where the monitor fires between Execute returning and the flag check.
	if reporter != nil {
		reporter.Stop()
	}
	if err != nil {
		errorCode := ""
		if errors.Is(actCtx.Err(), context.DeadlineExceeded) {
			errorCode = "TIMEOUT"
		}
		output = &runtime.ActivityOutput{
			NodeID:    nodeID,
			Success:   false,
			Error:     err.Error(),
			ErrorCode: errorCode,
		}
	}
	if output == nil {
		output = &runtime.ActivityOutput{
			NodeID:  nodeID,
			Success: false,
			Error:   "activity handler returned nil output",
		}
	}
	if heartbeatTimedOut.Load() {
		output.Success = false
		if output.Error == "" || output.Error == context.Canceled.Error() {
			output.Error = fmt.Sprintf("heartbeat timeout after %s", heartbeatTimeout)
		}
		if output.ErrorCode == "" {
			output.ErrorCode = "TIMEOUT"
		}
	}
	if !output.Success && output.ErrorCode == "" && errors.Is(actCtx.Err(), context.DeadlineExceeded) {
		output.ErrorCode = "TIMEOUT"
	}

	// Persist idempotent result to activity_results table
	now := time.Now()
	status := "completed"
	if !output.Success {
		status = "failed"
	}
	outputPayload := output.Output
	if marshaled, marshalErr := json.Marshal(output); marshalErr == nil {
		outputPayload = string(marshaled)
	} else {
		log.Printf("Warning: failed to marshal activity output for %s: %v", idempotencyKey, marshalErr)
	}
	result := &storage.ActivityResult{
		IdempotencyKey: idempotencyKey,
		RunID:          params.Identity.RunID,
		NodeID:         nodeID,
		Attempt:        attempt,
		ActivityType:   string(actType),
		Status:         status,
		OutputPayload:  outputPayload,
		ErrorCode:      output.ErrorCode,
		ErrorMessage:   output.Error,
		CompletedAt:    &now,
	}
	if saveErr := r.store.SaveActivityResult(ctx, result); saveErr != nil {
		log.Printf("Warning: failed to save activity result for %s: %v", idempotencyKey, saveErr)
	}

	return output
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func heartbeatTimeoutFromNode(node *workflow.Node) time.Duration {
	if node == nil || node.Metadata == nil {
		return 0
	}

	if raw, ok := node.Metadata["heartbeat_timeout_ms"]; ok {
		if ms, ok := asPositiveInt(raw); ok {
			return time.Duration(ms) * time.Millisecond
		}
	}
	if raw, ok := node.Metadata["heartbeat_timeout_seconds"]; ok {
		if sec, ok := asPositiveInt(raw); ok {
			return time.Duration(sec) * time.Second
		}
	}
	return 0
}

func asPositiveInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, n > 0
	case int32:
		i := int(n)
		return i, i > 0
	case int64:
		i := int(n)
		return i, i > 0
	case float32:
		f := float64(n)
		if f <= 0 || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return int(f), int(f) > 0
	case float64:
		if n <= 0 || math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, false
		}
		return int(n), int(n) > 0
	default:
		return 0, false
	}
}

func decodeCachedActivityOutput(nodeID string, cached *storage.ActivityResult) *runtime.ActivityOutput {
	if cached == nil {
		return &runtime.ActivityOutput{
			NodeID:  nodeID,
			Success: false,
			Error:   "cached activity result missing",
		}
	}

	parsed := &runtime.ActivityOutput{}
	if cached.OutputPayload != "" {
		if err := json.Unmarshal([]byte(cached.OutputPayload), parsed); err != nil {
			// Backward compatibility for legacy rows that stored raw output text.
			parsed = &runtime.ActivityOutput{
				NodeID:  nodeID,
				Success: cached.Status == "completed",
				Output:  cached.OutputPayload,
			}
		}
	}

	if parsed.NodeID == "" {
		parsed.NodeID = nodeID
	}
	parsed.Success = cached.Status == "completed"
	if parsed.Error == "" {
		parsed.Error = cached.ErrorMessage
	}
	if parsed.ErrorCode == "" {
		parsed.ErrorCode = cached.ErrorCode
	}

	return parsed
}

func resolveExecutionTotals(costTracker *workflow.CostTracker, historyEvents []*runtime.HistoryEvent) (float64, int, int, int) {
	historyCost, historyTotal, historyInput, historyOutput := totalsFromHistory(historyEvents)

	if costTracker != nil {
		trackerCost, trackerTotal, trackerInput, trackerOutput := costTracker.GetTotals()
		return coalesceExecutionTotals(
			historyCost, historyTotal, historyInput, historyOutput,
			trackerCost, trackerTotal, trackerInput, trackerOutput,
		)
	}

	return historyCost, historyTotal, historyInput, historyOutput
}

func coalesceExecutionTotals(
	historyCost float64,
	historyTotal, historyInput, historyOutput int,
	trackerCost float64,
	trackerTotal, trackerInput, trackerOutput int,
) (float64, int, int, int) {
	historyEmpty := historyCost == 0 && historyTotal == 0 && historyInput == 0 && historyOutput == 0
	trackerEmpty := trackerCost == 0 && trackerTotal == 0 && trackerInput == 0 && trackerOutput == 0

	if historyEmpty {
		return trackerCost, trackerTotal, trackerInput, trackerOutput
	}
	if trackerEmpty {
		return historyCost, historyTotal, historyInput, historyOutput
	}

	// Prefer whichever source dominates all dimensions. This avoids double counting
	// on resume while still allowing tracker totals to win when history is sparse.
	if historyCost >= trackerCost &&
		historyInput >= trackerInput &&
		historyOutput >= trackerOutput &&
		historyTotal >= trackerTotal {
		return historyCost, historyTotal, historyInput, historyOutput
	}
	if trackerCost >= historyCost &&
		trackerInput >= historyInput &&
		trackerOutput >= historyOutput &&
		trackerTotal >= historyTotal {
		return trackerCost, trackerTotal, trackerInput, trackerOutput
	}

	cost := math.Max(historyCost, trackerCost)
	input := max(historyInput, trackerInput)
	output := max(historyOutput, trackerOutput)
	total := max(historyTotal, trackerTotal)
	if input+output > total {
		total = input + output
	}
	return cost, total, input, output
}

func totalsFromHistory(historyEvents []*runtime.HistoryEvent) (float64, int, int, int) {
	// Prefer the terminal workflow_completed aggregate when present.
	for i := len(historyEvents) - 1; i >= 0; i-- {
		e := historyEvents[i]
		if e.Type != runtime.HistoryWorkflowCompleted {
			continue
		}
		totalCost, _ := typeconv.AsFloat64(e.Attributes["total_cost"])
		totalTokens, _ := typeconv.AsIntOK(e.Attributes["total_tokens"])
		totalInput, _ := typeconv.AsIntOK(e.Attributes["total_input_tokens"])
		totalOutput, _ := typeconv.AsIntOK(e.Attributes["total_output_tokens"])
		return totalCost, totalTokens, totalInput, totalOutput
	}

	// Fallback: sum activity_completed events.
	var totalCost float64
	var totalInput, totalOutput int
	for _, e := range historyEvents {
		if e.Type != runtime.HistoryActivityCompleted {
			continue
		}
		if v, ok := typeconv.AsFloat64(e.Attributes["cost"]); ok {
			totalCost += v
		}
		if v, ok := typeconv.AsIntOK(e.Attributes["tokens_input"]); ok {
			totalInput += v
		}
		if v, ok := typeconv.AsIntOK(e.Attributes["tokens_output"]); ok {
			totalOutput += v
		}
	}
	return totalCost, totalInput + totalOutput, totalInput, totalOutput
}
