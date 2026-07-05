package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

// appendHistory writes a history event to the store.
func (r *DAGRuntime) appendHistory(ctx context.Context, runID string, eventType runtime.HistoryEventType, nodeID, activityID string, attrs map[string]interface{}) error {
	event := &storage.HistoryEvent{
		RunID:      runID,
		Type:       string(eventType),
		NodeID:     nodeID,
		ActivityID: activityID,
		Timestamp:  time.Now(),
		Attributes: attrs,
	}
	if err := r.store.AppendHistoryEvent(ctx, event); err != nil {
		log.Printf("Warning: failed to append history event %s: %v", eventType, err)
		return err
	}
	return nil
}

// historyBatchEntry describes a single event within a batch write.
type historyBatchEntry struct {
	eventType  runtime.HistoryEventType
	nodeID     string
	activityID string
	attrs      map[string]interface{}
}

// appendHistoryBatch writes multiple history events atomically in a single transaction.
func (r *DAGRuntime) appendHistoryBatch(ctx context.Context, runID string, entries []historyBatchEntry) error {
	histEvents := make([]*storage.HistoryEvent, len(entries))
	for i, e := range entries {
		histEvents[i] = &storage.HistoryEvent{
			RunID:      runID,
			Type:       string(e.eventType),
			NodeID:     e.nodeID,
			ActivityID: e.activityID,
			Timestamp:  time.Now(),
			Attributes: e.attrs,
		}
	}
	if err := r.store.AppendHistoryEventBatch(ctx, histEvents); err != nil {
		log.Printf("Warning: failed to append history batch: %v", err)
		return err
	}
	return nil
}

// emitEvent sends a streaming event via the callback if set.
func (r *DAGRuntime) emitEvent(params *runtime.StartParams, event *runtime.ExecutionEvent) {
	if event == nil {
		return
	}
	if event.Payload == nil {
		event.Payload = make(map[string]interface{})
	}
	if params != nil && params.Identity != nil {
		if _, ok := event.Payload["execution_id"]; !ok {
			event.Payload["execution_id"] = params.Identity.WorkflowExecutionID
		}
		if _, ok := event.Payload["run_id"]; !ok {
			event.Payload["run_id"] = params.Identity.RunID
		}
	}
	if params != nil && params.EventCallback != nil {
		params.EventCallback(event)
	}
}

func (r *DAGRuntime) markRunningNodesCancelled(jobID, runID string) {
	if r.store == nil || jobID == "" || runID == "" {
		return
	}
	count, err := r.store.MarkRunningNodeExecutionAttemptsCancelled(context.Background(), jobID, runID)
	if err != nil {
		log.Printf("Warning: failed to mark running node execution attempts cancelled for job %s run %s: %v", jobID, runID, err)
		return
	}
	if count > 0 {
		log.Printf("Marked %d running node execution attempts cancelled for job %s run %s", count, jobID, runID)
	}
}

func (r *DAGRuntime) markRunningNodesInterrupted(jobID, runID string) {
	if r.store == nil || jobID == "" || runID == "" {
		return
	}
	count, err := r.store.MarkRunningNodeExecutionAttemptsInterrupted(context.Background(), jobID, runID)
	if err != nil {
		log.Printf("Warning: failed to mark running node execution attempts interrupted for job %s run %s: %v", jobID, runID, err)
		return
	}
	if count > 0 {
		log.Printf("Marked %d running node execution attempts interrupted for job %s run %s", count, jobID, runID)
	}
}

func ptrTime(t time.Time) *time.Time {
	return &t
}

// emitNodeStart sends a node_start streaming event matching the existing format.
func (r *DAGRuntime) emitNodeStart(params *runtime.StartParams, node *workflow.Node, nodeIndex, totalNodes int, jobID string) {
	label := extractLabel(node)
	meta := node.DisplayMeta()

	nodeData := map[string]interface{}{
		"node_index": nodeIndex,
		"node_total": totalNodes,
		"node_type":  node.Type,
		"node_label": label,
	}
	if meta.Name != "" {
		nodeData["node_name"] = meta.Name
	}
	if meta.Description != "" {
		nodeData["node_description"] = meta.Description
	}
	if node.Type == workflow.NodeTypeResult && node.AggregationMethod != "" {
		nodeData["aggregation_method"] = string(node.AggregationMethod)
	}

	msg := fmt.Sprintf("Executing %s (%d/%d)", label, nodeIndex, totalNodes)
	if meta.Description != "" {
		msg = fmt.Sprintf("Executing %s: %s (%d/%d)", label, meta.Description, nodeIndex, totalNodes)
	}

	r.emitEvent(params, &runtime.ExecutionEvent{
		Type: events.EventNodeStart,
		Payload: map[string]interface{}{
			"job_id":  jobID,
			"node_id": node.ID,
			"message": msg,
			"data":    nodeData,
		},
	})
}

// emitNodeComplete sends a node_complete streaming event matching the existing format.
func (r *DAGRuntime) emitNodeComplete(params *runtime.StartParams, node *workflow.Node, output *runtime.ActivityOutput, jobID string) {
	label := extractLabel(node)
	meta := node.DisplayMeta()

	completeData := map[string]interface{}{
		"tokens_input":  output.TokensInput,
		"tokens_output": output.TokensOutput,
		"cost":          output.Cost,
		"latency_ms":    output.LatencyMs,
		"node_label":    label,
		"node_type":     string(node.Type),
	}
	if meta.Name != "" {
		completeData["node_name"] = meta.Name
	}
	if meta.Description != "" {
		completeData["node_description"] = meta.Description
	}

	// Propagate aggregation metadata
	if output.Metadata != nil {
		for _, key := range []string{"aggregation_method", "aggregation_winner", "aggregation_scores", "aggregation_reasoning", "eval_matrix"} {
			if v, ok := output.Metadata[key]; ok {
				completeData[key] = v
			}
		}
	}

	r.emitEvent(params, &runtime.ExecutionEvent{
		Type: events.EventNodeComplete,
		Payload: map[string]interface{}{
			"job_id":  jobID,
			"node_id": output.NodeID,
			"message": fmt.Sprintf("%s completed", label),
			"output":  output.Output,
			"data":    completeData,
		},
	})
}

// emitRetryEvents sends retry-related streaming events.
func (r *DAGRuntime) emitRetryEvents(params *runtime.StartParams, node *workflow.Node, attempt int, policy *workflow.RetryPolicy, jobID, errMsg string) {
	backoff := policy.GetBackoffDuration(attempt)

	// node_retry_backoff
	r.emitEvent(params, &runtime.ExecutionEvent{
		Type: events.EventNodeRetryBackoff,
		Payload: map[string]interface{}{
			"job_id":       jobID,
			"node_id":      node.ID,
			"message":      fmt.Sprintf("Retrying %s in %s (attempt %d/%d)", node.ID, backoff, attempt+1, policy.MaxAttempts),
			"attempt":      attempt,
			"max_attempts": policy.MaxAttempts,
			"backoff_ms":   backoff.Milliseconds(),
			"error":        errMsg,
		},
	})

	// node_retry_start (after backoff)
	r.emitEvent(params, &runtime.ExecutionEvent{
		Type: events.EventNodeRetryStart,
		Payload: map[string]interface{}{
			"job_id":       jobID,
			"node_id":      node.ID,
			"message":      fmt.Sprintf("Retry attempt %d/%d for %s", attempt+1, policy.MaxAttempts, node.ID),
			"attempt":      attempt + 1,
			"max_attempts": policy.MaxAttempts,
		},
	})
}

// saveWorkflowNode persists a completed node execution projection row.
func (r *DAGRuntime) saveWorkflowNode(jobID, runID, executionID string, node *workflow.Node, output *runtime.ActivityOutput, attempt int, activityID string, nodeIndex, totalNodes int) {
	label := extractLabel(node)
	meta := node.DisplayMeta()

	metadata := mergedProjectionMetadata(node, output.Metadata)
	metadataJSON := marshalProjectionMetadata(metadata)
	tokensInput, tokensOutput := output.TokensInput, output.TokensOutput
	cost, latencyMs := output.Cost, output.LatencyMs
	if isCompiledConditionalBranchProjection(node, output.Metadata) {
		tokensInput, tokensOutput = 0, 0
		cost, latencyMs = 0, 0
	}

	wfNode := &storage.WorkflowNode{
		ExecutionID:   executionID,
		RunID:         runID,
		NodeID:        node.ID,
		NodeType:      string(node.Type),
		NodeOrder:     nodeIndex,
		Status:        "completed",
		NodeLabel:     label,
		NodeName:      meta.Name,
		Prompt:        node.Prompt,
		Model:         node.Model,
		Output:        output.Output,
		TokensInput:   tokensInput,
		TokensOutput:  tokensOutput,
		Cost:          cost,
		LatencyMs:     latencyMs,
		Metadata:      metadataJSON,
		ParentNodeID:  compiledAggregationParentNodeID(node.ID, node.Metadata),
		ExecutionUID:  fmt.Sprintf("%s:%s:%d", jobID, node.ID, attempt),
		AttemptNumber: attempt,
		ActivityID:    activityID,
	}
	if err := r.store.AddWorkflowNode(wfNode); err != nil {
		log.Printf("Warning: failed to save workflow node %s: %v", node.ID, err)
	}
}

// saveFailedNode persists a failed node execution projection row.
func (r *DAGRuntime) saveFailedNode(jobID, runID, executionID string, node *workflow.Node, errMsg, errCode string, outputMetadata map[string]interface{}, attempt int, activityID string, nodeIndex, totalNodes int) {
	label := extractLabel(node)
	meta := node.DisplayMeta()
	metadata := mergedProjectionMetadata(node, outputMetadata)

	wfNode := &storage.WorkflowNode{
		ExecutionID:   executionID,
		RunID:         runID,
		NodeID:        node.ID,
		NodeType:      string(node.Type),
		NodeOrder:     nodeIndex,
		Status:        "failed",
		NodeLabel:     label,
		NodeName:      meta.Name,
		Prompt:        node.Prompt,
		Model:         node.Model,
		ErrorMessage:  errMsg,
		ErrorCode:     errCode,
		Metadata:      marshalProjectionMetadata(metadata),
		ParentNodeID:  compiledAggregationParentNodeID(node.ID, node.Metadata),
		ExecutionUID:  fmt.Sprintf("%s:%s:%d", jobID, node.ID, attempt),
		AttemptNumber: attempt,
		ActivityID:    activityID,
	}
	if err := r.store.AddWorkflowNode(wfNode); err != nil {
		log.Printf("Warning: failed to save failed workflow node %s: %v", node.ID, err)
	}
}

func mergedProjectionMetadata(node *workflow.Node, outputMetadata map[string]interface{}) map[string]interface{} {
	var merged map[string]interface{}
	if node != nil && len(node.Metadata) > 0 {
		merged = make(map[string]interface{}, len(node.Metadata)+len(outputMetadata))
		for k, v := range node.Metadata {
			merged[k] = v
		}
	}
	if len(outputMetadata) > 0 {
		if merged == nil {
			merged = make(map[string]interface{}, len(outputMetadata))
		}
		for k, v := range outputMetadata {
			merged[k] = v
		}
	}
	if node != nil && len(node.Metadata) > 0 {
		preserveProjectionProvenanceMetadata(merged, node.Metadata)
	}
	return merged
}

func preserveProjectionProvenanceMetadata(merged, nodeMetadata map[string]interface{}) {
	if len(merged) == 0 || len(nodeMetadata) == 0 {
		return
	}
	for _, key := range []string{
		"aggregation_group_node_id",
		"aggregation_anchor_id",
		"aggregation_method",
		"source_workflow_id",
		"source_workflow_hash",
		"source_node_id",
		"source_parent_node_id",
	} {
		if value, ok := nodeMetadata[key]; ok {
			merged[key] = value
		}
	}
}

func isCompiledConditionalBranchProjection(node *workflow.Node, outputMetadata map[string]interface{}) bool {
	if node == nil || node.Type != workflow.NodeTypeConditional || len(outputMetadata) == 0 {
		return false
	}
	if compiledAggregationParentNodeID(node.ID, node.Metadata) == "" {
		return false
	}
	branchID, _ := outputMetadata["conditional_branch_node_id"].(string)
	return strings.TrimSpace(branchID) != ""
}

func marshalProjectionMetadata(metadata map[string]interface{}) string {
	if len(metadata) == 0 {
		return ""
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return ""
	}
	return string(data)
}

func compiledAggregationParentNodeID(nodeID string, metadata map[string]interface{}) string {
	if len(metadata) == 0 {
		return ""
	}
	groupID, _ := metadata["aggregation_group_node_id"].(string)
	groupID = strings.TrimSpace(groupID)
	if groupID == "" || groupID == nodeID {
		return ""
	}
	return groupID
}

// extractLabel gets a display label for a node.
func extractLabel(node *workflow.Node) string {
	meta := node.DisplayMeta()
	if meta.Label != "" {
		return meta.Label
	}
	if node.ID != "" {
		return node.ID
	}
	return string(node.Type)
}
