package workflow

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const novoRunDefaultGraceSeconds = 10

// NovoRunNodeRunner executes novo_run nodes by launching or adopting a Novomo
// wake and consuming the terminal NovoRun summary.
type NovoRunNodeRunner struct{}

func (r *NovoRunNodeRunner) NodeType() NodeType { return NodeTypeNovoRun }

func (r *NovoRunNodeRunner) Execute(sc *NodeContext) (*NodeResult, error) {
	startTime := time.Now()

	if sc.ExecCtx == nil || sc.ExecCtx.ExecuteNovoRun == nil {
		err := NewNonRetryableError(fmt.Errorf("novo_run executor is not configured"), "INVALID_CONFIG")
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}
	if sc.Node == nil {
		err := NewNonRetryableError(fmt.Errorf("novo_run node is nil"), "INVALID_CONFIG")
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}

	goal := strings.TrimSpace(InterpolateVariables(sc.Node.Prompt, sc.WorkflowContext))
	taskID := strings.TrimSpace(sc.Node.TaskID)
	if goal == "" && taskID == "" {
		err := NewNonRetryableError(fmt.Errorf("novo_run prompt or task_id is required"), "INVALID_CONFIG")
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}
	timeoutSeconds := sc.Node.TimeoutSeconds
	if timeoutSeconds <= 0 {
		err := NewNonRetryableError(fmt.Errorf("novo_run timeout_seconds is required"), "INVALID_CONFIG")
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}
	sandbox := NormalizeNovomoSandbox(sc.Node.Sandbox)
	if !IsSupportedNovomoSandbox(sandbox) {
		err := NewNonRetryableError(fmt.Errorf("novo_run sandbox %q is not supported", sandbox), "INVALID_CONFIG")
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}
	if sc.Node.GraceSeconds < 0 {
		err := NewNonRetryableError(fmt.Errorf("novo_run grace_seconds must be non-negative"), "INVALID_CONFIG")
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}
	inheritFrom := NormalizeNovomoHandoff(sc.Node.InheritFrom)
	inheritFromNodeID := strings.TrimSpace(sc.Node.InheritFromNodeID)
	inheritFromPolicy := strings.TrimSpace(sc.Node.InheritFromPolicy)
	inheritFromWorkflowTask := sc.Node.InheritFromWorkflowTask
	if CountNovomoHandoffSources(inheritFrom != nil, inheritFromNodeID != "", inheritFromWorkflowTask) > 1 {
		err := NewNonRetryableError(fmt.Errorf("novo_run can set only one inheritance source"), "INVALID_CONFIG")
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}
	if validationErr := ValidateNovomoHandoffRef("inherit_from", inheritFrom); validationErr != nil {
		err := NewNonRetryableError(fmt.Errorf("novo_run %s", validationErr.Error()), "INVALID_CONFIG")
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}
	graceSeconds := sc.Node.GraceSeconds
	if graceSeconds == 0 {
		graceSeconds = novoRunDefaultGraceSeconds
	}

	nodeCtx, cancel := context.WithTimeout(sc.Ctx, time.Duration(timeoutSeconds+graceSeconds)*time.Second)
	defer cancel()

	req := &NovoRunRequest{
		ParentJobID:             sc.ExecCtx.JobID,
		ParentExecutionID:       fallbackString(sc.ExecCtx.WorkflowExecutionID, sc.ExecCtx.JobID),
		ParentRunID:             sc.ExecCtx.RunID,
		ParentWorkflowID:        sc.ExecCtx.WorkflowID,
		ParentNodeID:            sc.NodeID,
		Attempt:                 sc.Attempt,
		Goal:                    goal,
		TaskID:                  taskID,
		TaskSummary:             strings.TrimSpace(sc.Node.TaskSummary),
		Identity:                strings.TrimSpace(sc.Node.Identity),
		Image:                   strings.TrimSpace(sc.Node.Image),
		Sandbox:                 sandbox,
		RuntimeURL:              strings.TrimSpace(sc.Node.RuntimeURL),
		TimeoutSeconds:          timeoutSeconds,
		GraceSeconds:            graceSeconds,
		RepoSpecs:               sc.Node.RepoSpecs,
		WorkSource:              sc.Node.WorkSource,
		InheritFrom:             inheritFrom,
		InheritFromNodeID:       inheritFromNodeID,
		InheritFromPolicy:       inheritFromPolicy,
		InheritFromWorkflowTask: inheritFromWorkflowTask,
		IdempotencyKey:          NodeExecutionID(fallbackString(sc.ExecCtx.WorkflowExecutionID, sc.ExecCtx.JobID), sc.NodeID, sc.Attempt),
	}

	agentResult, err := sc.ExecCtx.ExecuteNovoRun(nodeCtx, req)
	latency := float64(time.Since(startTime).Milliseconds())
	if err != nil {
		classified := classifyAgentRunGoError(err)
		return newErrorResult(sc.NodeID, err.Error(), latency, agentRunMetadata(sc.Node.Metadata, nil)), classified
	}
	if agentResult == nil {
		err := NewNonRetryableError(fmt.Errorf("novo_run returned nil result"), AgentRunErrorNovomoUnresponsive)
		return newErrorResult(sc.NodeID, err.Error(), latency, agentRunMetadata(sc.Node.Metadata, nil)), err
	}
	if agentResult.ExternalRunKind == "" {
		agentResult.ExternalRunKind = "novo_run"
	}

	metadata := agentRunMetadata(sc.Node.Metadata, agentResult)
	if !agentResult.Success {
		errMsg := strings.TrimSpace(agentResult.Error)
		if errMsg == "" {
			errMsg = "novo_run failed"
		}
		err := classifyAgentRunTerminalFailure(agentResult.ErrorCode, errMsg)
		return &NodeResult{
			NodeID:       sc.NodeID,
			Success:      false,
			Error:        errMsg,
			TokensInput:  agentResult.TokensInput,
			TokensOutput: agentResult.TokensOutput,
			Cost:         agentResult.Cost,
			LatencyMs:    latency,
			Metadata:     metadata,
		}, err
	}

	return &NodeResult{
		NodeID:       sc.NodeID,
		Success:      true,
		Output:       agentResult.Output,
		TokensInput:  agentResult.TokensInput,
		TokensOutput: agentResult.TokensOutput,
		Cost:         agentResult.Cost,
		LatencyMs:    latency,
		Metadata:     metadata,
	}, nil
}
