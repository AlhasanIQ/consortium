package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	AgentRunErrorNovomoUnresponsive = "NOVOMO_UNRESPONSIVE"
	agentRunNodeContextGrace        = 10 * time.Second
)

// AgentRunNodeRunner executes agent_run nodes by delegating to the configured
// external agent runtime callback.
type AgentRunNodeRunner struct{}

func (r *AgentRunNodeRunner) NodeType() NodeType { return NodeTypeAgentRun }

func (r *AgentRunNodeRunner) Execute(sc *NodeContext) (*NodeResult, error) {
	startTime := time.Now()

	if sc.ExecCtx == nil || sc.ExecCtx.ExecuteAgentRun == nil {
		err := NewNonRetryableError(fmt.Errorf("agent run executor is not configured"), "INVALID_CONFIG")
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}
	if sc.Node == nil {
		err := NewNonRetryableError(fmt.Errorf("agent_run node is nil"), "INVALID_CONFIG")
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}

	prompt := strings.TrimSpace(InterpolateVariables(sc.Node.Prompt, sc.WorkflowContext))
	if prompt == "" {
		err := NewNonRetryableError(fmt.Errorf("agent_run prompt is required"), "INVALID_CONFIG")
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}
	harness := strings.TrimSpace(sc.Node.Harness)
	if harness == "" {
		err := NewNonRetryableError(fmt.Errorf("agent_run harness is required"), "INVALID_CONFIG")
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}
	if !isSupportedAgentRunHarness(harness) {
		err := NewNonRetryableError(fmt.Errorf("agent_run harness %q is not supported", harness), "INVALID_CONFIG")
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}
	sandbox := NormalizeNovomoSandbox(sc.Node.Sandbox)
	if !IsSupportedNovomoSandbox(sandbox) {
		err := NewNonRetryableError(fmt.Errorf("agent_run sandbox %q is not supported", sandbox), "INVALID_CONFIG")
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}
	timeoutSeconds := sc.Node.TimeoutSeconds
	if timeoutSeconds <= 0 {
		err := NewNonRetryableError(fmt.Errorf("agent_run timeout_seconds is required"), "INVALID_CONFIG")
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}
	inheritFrom := NormalizeNovomoHandoff(sc.Node.InheritFrom)
	inheritFromNodeID := strings.TrimSpace(sc.Node.InheritFromNodeID)
	inheritFromPolicy := strings.TrimSpace(sc.Node.InheritFromPolicy)
	inheritFromWorkflowTask := sc.Node.InheritFromWorkflowTask
	if CountNovomoHandoffSources(inheritFrom != nil, inheritFromNodeID != "", inheritFromWorkflowTask) > 1 {
		err := NewNonRetryableError(fmt.Errorf("agent_run can set only one inheritance source"), "INVALID_CONFIG")
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}
	if validationErr := ValidateNovomoHandoffRef("inherit_from", inheritFrom); validationErr != nil {
		err := NewNonRetryableError(fmt.Errorf("agent_run %s", validationErr.Error()), "INVALID_CONFIG")
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}

	nodeCtx, cancel := context.WithTimeout(sc.Ctx, time.Duration(timeoutSeconds)*time.Second+agentRunNodeContextGrace)
	defer cancel()

	req := &AgentRunRequest{
		ParentJobID:             sc.ExecCtx.JobID,
		ParentExecutionID:       fallbackString(sc.ExecCtx.WorkflowExecutionID, sc.ExecCtx.JobID),
		ParentRunID:             sc.ExecCtx.RunID,
		ParentWorkflowID:        sc.ExecCtx.WorkflowID,
		ParentNodeID:            sc.NodeID,
		Attempt:                 sc.Attempt,
		Prompt:                  prompt,
		Harness:                 harness,
		Sandbox:                 sandbox,
		TaskID:                  strings.TrimSpace(sc.Node.TaskID),
		TimeoutSeconds:          timeoutSeconds,
		InheritFrom:             inheritFrom,
		InheritFromNodeID:       inheritFromNodeID,
		InheritFromPolicy:       inheritFromPolicy,
		InheritFromWorkflowTask: inheritFromWorkflowTask,
		IdempotencyKey:          NodeExecutionID(fallbackString(sc.ExecCtx.WorkflowExecutionID, sc.ExecCtx.JobID), sc.NodeID, sc.Attempt),
	}

	agentResult, err := sc.ExecCtx.ExecuteAgentRun(nodeCtx, req)
	latency := float64(time.Since(startTime).Milliseconds())
	if err != nil {
		classified := classifyAgentRunGoError(err)
		return newErrorResult(sc.NodeID, err.Error(), latency, agentRunMetadata(sc.Node.Metadata, nil)), classified
	}
	if agentResult == nil {
		err := NewNonRetryableError(fmt.Errorf("agent_run returned nil result"), AgentRunErrorNovomoUnresponsive)
		return newErrorResult(sc.NodeID, err.Error(), latency, agentRunMetadata(sc.Node.Metadata, nil)), err
	}

	metadata := agentRunMetadata(sc.Node.Metadata, agentResult)
	if !agentResult.Success {
		errMsg := strings.TrimSpace(agentResult.Error)
		if errMsg == "" {
			errMsg = "agent_run failed"
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

// CountNovomoHandoffSources returns how many of the given handoff-source flags
// are set, used to enforce that at most one inheritance source is configured.
func CountNovomoHandoffSources(sources ...bool) int {
	count := 0
	for _, source := range sources {
		if source {
			count++
		}
	}
	return count
}

func agentRunMetadata(base map[string]interface{}, result *AgentRunResult) map[string]interface{} {
	extra := map[string]interface{}{}
	if result != nil {
		if result.ExternalRunID != "" {
			extra["external_run_id"] = result.ExternalRunID
		}
		if result.ExternalRunKind != "" {
			extra["external_run_kind"] = result.ExternalRunKind
		}
		if result.ExternalTaskID != "" {
			extra["external_task_id"] = result.ExternalTaskID
		}
		if result.ExternalJobRunID != "" {
			extra["external_job_run_id"] = result.ExternalJobRunID
		}
		if result.InheritFrom != nil {
			extra["inherit_from"] = result.InheritFrom
		}
		if result.Harness != "" {
			extra["harness"] = result.Harness
		}
		if result.Status != "" {
			extra["status"] = result.Status
		}
		if result.ErrorCode != "" {
			extra["error_code"] = result.ErrorCode
		}
	}
	return MergeMetadata(base, extra)
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func classifyAgentRunGoError(err error) error {
	if err == nil {
		return nil
	}
	if code := GetErrorCode(err); code != "" {
		return err
	}
	if errors.Is(err, context.Canceled) {
		return NewNonRetryableError(err, "CANCELLED")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return NewRetryableError(err, RetryCodeTimeout)
	}
	return err
}

func classifyAgentRunTerminalFailure(code, message string) error {
	code = strings.TrimSpace(code)
	if code == "" {
		code = "unknown"
	}
	err := fmt.Errorf("%s", message)
	switch strings.ToLower(code) {
	case "timeout", "crashed", "startup_failed":
		return NewRetryableError(err, code)
	case "bad_request", "auth", "unsupported_harness", "invalid_request",
		"not_found", "novomo_error", "novomo_http_error", "novomo_malformed_response",
		"novomo_url_missing", "novomo_url_invalid", "cancelled",
		strings.ToLower(AgentRunErrorNovomoUnresponsive):
		return NewNonRetryableError(err, code)
	default:
		return NewNonRetryableError(err, code)
	}
}
