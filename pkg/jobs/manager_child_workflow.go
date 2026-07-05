package jobs

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/workflow"
	workflowruntime "github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

func (m *Manager) executeChildWorkflowWithParentReplay(
	ctx context.Context,
	req *workflow.ChildWorkflowRequest,
	parentReplay *workflowruntime.ReplayRequest,
) (*workflow.ChildWorkflowResult, error) {
	if req == nil {
		return nil, fmt.Errorf("child workflow request is nil")
	}
	if !m.workersStarted.Load() {
		return nil, ErrWorkersNotStarted
	}

	childWorkflowID := strings.TrimSpace(req.ChildWorkflowID)
	if childWorkflowID == "" {
		return nil, fmt.Errorf("child workflow id is required")
	}

	definition, err := m.storage.GetWorkflow(childWorkflowID)
	if err != nil {
		return nil, fmt.Errorf("load child workflow %q: %w", childWorkflowID, err)
	}

	inputValues := req.InputValues
	if inputValues == nil {
		inputValues = make(map[string]interface{})
	}

	childWorkflow, err := WorkflowFromDefinition(definition.Definition, inputValues)
	if err != nil {
		return nil, fmt.Errorf("convert child workflow %q: %w", childWorkflowID, err)
	}
	childWorkflow.ID = definition.ID
	childWorkflow.Name = definition.Name
	childWorkflow.Description = definition.Description

	childReplay, replayErr := m.resolveChildReplayRequest(ctx, req, childWorkflow, parentReplay)
	if replayErr != nil {
		return nil, fmt.Errorf("resolve child replay for %q: %w", childWorkflowID, replayErr)
	}
	if childReplay != nil {
		log.Printf("🔁 [Replay] child workflow %s receives replay request (parent=%s node=%s)",
			childWorkflowID, req.ParentJobID, req.ParentNodeID)
	}

	submitResp, err := m.SubmitWorkflow(ctx, &SubmitWorkflowRequest{
		Workflow:          childWorkflow,
		ForceNewRun:       true,
		ParentExecutionID: req.ParentJobID,
		Replay:            childReplay,
	})
	if err != nil {
		return nil, fmt.Errorf("submit child workflow %q: %w", childWorkflowID, err)
	}

	execResult, err := m.waitForCompletion(ctx, submitResp.JobID, childWorkflow.ID)
	if err != nil {
		return nil, fmt.Errorf("wait for child workflow %q: %w", childWorkflowID, err)
	}
	if execResult == nil {
		return nil, fmt.Errorf("nil child execution result")
	}

	childResult := &workflow.ChildWorkflowResult{
		JobID:      execResult.JobID,
		WorkflowID: childWorkflow.ID,
		Success:    execResult.Success,
		Error:      execResult.Error,
	}
	if execResult.Result == nil {
		if !execResult.Success && childResult.Error == "" {
			childResult.Error = "child workflow failed"
		}
		return childResult, nil
	}

	childResult.FinalOutput = execResult.Result.FinalOutput
	childResult.Outputs = execResult.Result.Outputs
	childResult.TotalTokens = execResult.Result.TotalTokens
	childResult.TotalInputTokens = execResult.Result.TotalInputTokens
	childResult.TotalOutputTokens = execResult.Result.TotalOutputTokens
	childResult.TotalCost = execResult.Result.TotalCost
	childResult.TotalLatency = execResult.Result.TotalLatency
	if !execResult.Success && childResult.Error == "" {
		childResult.Error = execResult.Result.Error
	}
	if !execResult.Success && strings.TrimSpace(childResult.Error) == "" {
		if nodeErr := m.firstFailedNodeError(ctx, execResult.JobID); nodeErr != "" {
			childResult.Error = nodeErr
		}
	}
	if !execResult.Success && strings.TrimSpace(childResult.Error) == "" {
		childResult.Error = "child workflow failed"
	}

	return childResult, nil
}

func (m *Manager) firstFailedNodeError(ctx context.Context, jobID string) string {
	nodes, err := m.storage.ListNodeExecutionAttemptsByJob(ctx, jobID)
	if err == nil {
		for _, node := range nodes {
			if strings.EqualFold(node.Status, events.JobStatusFailed) && strings.TrimSpace(node.ErrorMessage) != "" {
				return strings.TrimSpace(node.ErrorMessage)
			}
		}
	}

	workflowNodes, err := m.storage.GetWorkflowNodes(jobID)
	if err != nil {
		return ""
	}
	for _, node := range workflowNodes {
		if strings.EqualFold(node.Status, events.JobStatusFailed) && strings.TrimSpace(node.ErrorMessage) != "" {
			return strings.TrimSpace(node.ErrorMessage)
		}
	}
	return ""
}
