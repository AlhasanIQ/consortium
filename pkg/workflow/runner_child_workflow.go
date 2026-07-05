package workflow

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// ChildWorkflowNodeRunner executes child_workflow nodes.
// It delegates child execution to ExecutionContext.ExecuteChildWorkflow.
type ChildWorkflowNodeRunner struct{}

// NodeType returns NodeTypeChildWorkflow ("child_workflow").
func (r *ChildWorkflowNodeRunner) NodeType() NodeType { return NodeTypeChildWorkflow }

// Execute submits a child workflow and waits for terminal completion.
func (r *ChildWorkflowNodeRunner) Execute(sc *NodeContext) (*NodeResult, error) {
	startTime := time.Now()

	if sc.ExecCtx == nil || sc.ExecCtx.ExecuteChildWorkflow == nil {
		err := fmt.Errorf("child workflow executor is not configured")
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}

	childWorkflowID := strings.TrimSpace(sc.Node.ChildWorkflowID)
	if childWorkflowID == "" {
		err := NewNonRetryableError(fmt.Errorf("child_workflow_id is required"), "INVALID_CHILD_WORKFLOW")
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}
	timeoutSeconds := sc.Node.TimeoutSeconds
	if timeoutSeconds <= 0 {
		err := NewNonRetryableError(fmt.Errorf("node timeout_seconds is required"), "INVALID_CONFIG")
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}
	nodeCtx, cancel := context.WithTimeout(sc.Ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	inputValues := make(map[string]interface{}, len(sc.Node.ChildInputTemplate))
	for key, template := range sc.Node.ChildInputTemplate {
		inputValues[key] = InterpolateVariables(template, sc.WorkflowContext)
	}
	if len(inputValues) == 0 {
		if userPrompt, ok := sc.WorkflowContext["user_prompt"]; ok {
			inputValues["user_prompt"] = userPrompt
		}
	}

	req := &ChildWorkflowRequest{
		ParentJobID:      sc.ExecCtx.JobID,
		ParentRunID:      sc.ExecCtx.RunID,
		ParentWorkflowID: sc.ExecCtx.WorkflowID,
		ParentNodeID:     sc.NodeID,
		ChildWorkflowID:  childWorkflowID,
		InputValues:      inputValues,
	}

	childResult, err := sc.ExecCtx.ExecuteChildWorkflow(nodeCtx, req)
	if err != nil {
		classified := classifyChildWorkflowGoError(err)
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), classified
	}
	if childResult == nil {
		err := fmt.Errorf("child workflow returned nil result")
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}
	if !childResult.Success {
		errMsg := strings.TrimSpace(childResult.Error)
		if errMsg == "" {
			errMsg = "child workflow failed"
		}
		err := fmt.Errorf("%s", errMsg)
		if isTransientChildWorkflowError(errMsg) {
			err = NewRetryableError(err, RetryCodeChildWorkflowTransient)
		}
		return newErrorResult(sc.NodeID, errMsg, float64(time.Since(startTime).Milliseconds())), err
	}

	output := childResult.FinalOutput
	outputSource := "final_output"
	if key := strings.TrimSpace(sc.Node.ChildOutputKey); key != "" {
		if childResult.Outputs != nil {
			if value, ok := childResult.Outputs[key]; ok {
				output = fmt.Sprintf("%v", value)
				outputSource = "named_output:" + key
			}
		}
	}
	metadata := buildChildWorkflowMetadata(childResult, outputSource)
	trimmedOutput := strings.TrimSpace(output)

	latency := float64(time.Since(startTime).Milliseconds())
	if trimmedOutput == "" {
		err := classifyEmptyChildWorkflowOutputError(childResult, outputSource)
		return newErrorResult(sc.NodeID, err.Error(), latency, metadata), err
	}

	return &NodeResult{
		NodeID:  sc.NodeID,
		Success: true,
		Output:  trimmedOutput,
		// Parent job accounting excludes child workflow usage to avoid double-counting
		// when aggregating costs/tokens across all jobs.
		TokensInput:  0,
		TokensOutput: 0,
		Cost:         0,
		LatencyMs:    latency,
		Metadata:     metadata,
	}, nil
}

func buildChildWorkflowMetadata(childResult *ChildWorkflowResult, outputSource string) map[string]interface{} {
	if childResult == nil {
		return map[string]interface{}{
			"child_output_source": outputSource,
		}
	}

	return map[string]interface{}{
		"child_job_id":               childResult.JobID,
		"child_workflow_id":          childResult.WorkflowID,
		"child_output_source":        outputSource,
		"child_total_tokens":         childResult.TotalTokens,
		"child_total_input_tokens":   childResult.TotalInputTokens,
		"child_total_output_tokens":  childResult.TotalOutputTokens,
		"child_total_cost":           childResult.TotalCost,
		"child_accounting_excluded":  true,
		"child_accounting_exclusion": "parent_job_totals",
	}
}

func classifyEmptyChildWorkflowOutputError(childResult *ChildWorkflowResult, outputSource string) error {
	childWorkflowID := ""
	childOutputTokens := 0
	if childResult != nil {
		childWorkflowID = strings.TrimSpace(childResult.WorkflowID)
		childOutputTokens = childResult.TotalOutputTokens
	}

	scope := "child workflow"
	if childWorkflowID != "" {
		scope = fmt.Sprintf("child workflow %q", childWorkflowID)
	}
	if childOutputTokens > 0 {
		return NewRetryableError(
			fmt.Errorf("%s returned empty %s after consuming %d output tokens", scope, outputSource, childOutputTokens),
			RetryCodeOutputTruncatedEmpty,
		)
	}
	return NewNonRetryableError(
		fmt.Errorf("%s returned empty %s", scope, outputSource),
		"EMPTY_CHILD_OUTPUT",
	)
}

// classifyChildWorkflowGoError wraps a Go error from ExecuteChildWorkflow
// with structured retry classification based on the error type.
func classifyChildWorkflowGoError(err error) error {
	if err == nil {
		return nil
	}

	// context.Canceled — user abort / shutdown, non-retryable
	if errors.Is(err, context.Canceled) {
		return NewNonRetryableError(err, "CANCELLED")
	}

	// context.DeadlineExceeded — retryable timeout (next attempt gets fresh deadline)
	if errors.Is(err, context.DeadlineExceeded) {
		return NewRetryableError(err, RetryCodeTimeout)
	}

	// net.Error with Timeout() — retryable network timeout
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return NewRetryableError(err, RetryCodeTimeout)
	}

	// Check string-based non-retryable markers
	if isNonRetryableChildWorkflowError(err.Error()) {
		return NewNonRetryableError(err, "CHILD_WORKFLOW_FATAL")
	}

	// Check string-based transient markers
	if isTransientChildWorkflowError(err.Error()) {
		return NewRetryableError(err, RetryCodeChildWorkflowTransient)
	}

	// Unknown errors default to retryable (policy will further filter)
	return err
}

// isNonRetryableChildWorkflowError checks whether a child workflow error message
// indicates a permanent failure that should not be retried.
func isNonRetryableChildWorkflowError(errMsg string) bool {
	lower := strings.ToLower(strings.TrimSpace(errMsg))
	if lower == "" {
		return false
	}
	markers := []string{
		"auth error",
		"invalid api key",
		"insufficient credits",
		"model not found",
		"bad request",
		"content filtered",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func isTransientChildWorkflowError(errMsg string) bool {
	lower := strings.ToLower(strings.TrimSpace(errMsg))
	if lower == "" {
		return false
	}
	markers := []string{
		"rate limit",
		"timeout",
		"timed out",
		"context deadline",
		"no choices returned",
		"temporarily unavailable",
		"upstream timeout",
		"upstream unavailable",
		"database is locked",
		"sqlite_busy",
		"connection reset",
		"broken pipe",
		"server at capacity",
		"admission pool exhausted",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
