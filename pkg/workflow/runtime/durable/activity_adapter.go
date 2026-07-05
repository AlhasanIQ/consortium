package durable

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/alhasaniq/consortium/pkg/trace"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/alhasaniq/consortium/pkg/workflow/runtime"
	"github.com/google/uuid"
)

// NodeRunnerAdapter wraps an existing workflow.NodeRunner as a runtime.ActivityHandler.
// This allows the durable runtime to execute activities using the existing runner infrastructure.
type NodeRunnerAdapter struct {
	runner  workflow.NodeRunner
	actType runtime.ActivityType
}

// NewNodeRunnerAdapter creates an adapter for a NodeRunner.
func NewNodeRunnerAdapter(runner workflow.NodeRunner) *NodeRunnerAdapter {
	return &NodeRunnerAdapter{
		runner:  runner,
		actType: runtime.NodeTypeToActivityType(runner.NodeType()),
	}
}

// Type returns the activity type this adapter handles.
func (a *NodeRunnerAdapter) Type() runtime.ActivityType {
	return a.actType
}

// Execute runs the underlying NodeRunner with a constructed NodeContext.
func (a *NodeRunnerAdapter) Execute(ctx context.Context, input *runtime.ActivityInput) (*runtime.ActivityOutput, error) {
	if input.Node == nil {
		return nil, fmt.Errorf("activity input has nil node for %s", input.NodeID)
	}

	start := time.Now()

	sc := &workflow.NodeContext{
		Ctx:             ctx,
		Node:            input.Node,
		NodeID:          input.NodeID,
		Attempt:         input.Attempt,
		WorkflowContext: input.WorkflowContext,
		ExecCtx:         input.ExecCtx,
		CostTracker:     input.CostTracker,
	}

	result, err := a.runner.Execute(sc)
	latency := time.Since(start).Seconds() * 1000 // ms

	if err != nil {
		errorCode := workflow.GetErrorCode(err)
		if errorCode == "" && errors.Is(err, context.DeadlineExceeded) {
			errorCode = "TIMEOUT"
		}
		if result != nil {
			out := &runtime.ActivityOutput{
				NodeID:       input.NodeID,
				Success:      false,
				Output:       result.Output,
				Error:        result.Error,
				ErrorCode:    errorCode,
				TokensInput:  result.TokensInput,
				TokensOutput: result.TokensOutput,
				Cost:         result.Cost,
				LatencyMs:    result.LatencyMs,
				Metadata:     result.Metadata,
			}
			if out.Error == "" {
				out.Error = err.Error()
			}
			if out.LatencyMs == 0 {
				out.LatencyMs = latency
			}
			writeActivityNodeSpan(ctx, input, start, out, err)
			return out, nil
		}
		out := &runtime.ActivityOutput{
			NodeID:    input.NodeID,
			Success:   false,
			Error:     err.Error(),
			ErrorCode: errorCode,
			LatencyMs: latency,
		}
		writeActivityNodeSpan(ctx, input, start, out, err)
		return out, nil // Return output with error info, not a Go error
	}

	if result == nil {
		out := &runtime.ActivityOutput{
			NodeID:    input.NodeID,
			Success:   false,
			Error:     "runner returned nil result",
			LatencyMs: latency,
		}
		writeActivityNodeSpan(ctx, input, start, out, nil)
		return out, nil
	}

	out := &runtime.ActivityOutput{
		NodeID:       input.NodeID,
		Success:      result.Success,
		Output:       result.Output,
		Error:        result.Error,
		TokensInput:  result.TokensInput,
		TokensOutput: result.TokensOutput,
		Cost:         result.Cost,
		LatencyMs:    result.LatencyMs,
		Metadata:     result.Metadata,
	}
	writeActivityNodeSpan(ctx, input, start, out, nil)
	return out, nil
}

func writeActivityNodeSpan(ctx context.Context, input *runtime.ActivityInput, startedAt time.Time, out *runtime.ActivityOutput, runErr error) {
	if input == nil || input.ExecCtx == nil || input.ExecCtx.TraceWriter == nil {
		return
	}
	if input.ExecCtx.JobID == "" {
		log.Printf("WARNING: TraceWriter configured but activity span missing job_id; skipping span write (node_id=%s activity_id=%s)", input.NodeID, input.ActivityID)
		return
	}

	status := trace.StatusOK
	if out != nil && !out.Success {
		status = trace.StatusError
	}
	if runErr != nil {
		status = trace.StatusError
	}
	if errors.Is(runErr, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		status = trace.StatusTimeout
	} else if errors.Is(runErr, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		status = trace.StatusCancelled
	}

	durationMs := float64(time.Since(startedAt).Milliseconds())
	if out != nil && out.LatencyMs > 0 {
		durationMs = out.LatencyMs
	}

	nodeType := string(input.Type)
	if input.Node != nil {
		nodeType = string(input.Node.Type)
	}
	attrs := map[string]any{
		"node_type":   nodeType,
		"attempt":     input.Attempt,
		"activity_id": input.ActivityID,
	}
	if out != nil {
		attrs["success"] = out.Success
		if out.Error != "" {
			attrs["error"] = out.Error
		}
		if out.ErrorCode != "" {
			attrs["error_code"] = out.ErrorCode
		}
		if out.TokensInput > 0 {
			attrs["tokens_input"] = out.TokensInput
		}
		if out.TokensOutput > 0 {
			attrs["tokens_output"] = out.TokensOutput
		}
		if out.Cost > 0 {
			attrs["cost"] = out.Cost
		}
	}
	if runErr != nil && (out == nil || out.Error == "") {
		attrs["error"] = runErr.Error()
	}

	span := &trace.Span{
		JobID:      input.ExecCtx.JobID,
		NodeID:     input.NodeID,
		SpanID:     uuid.NewString(),
		Kind:       trace.KindNode,
		Status:     status,
		StartedAt:  startedAt,
		DurationMs: durationMs,
		Attributes: attrs,
	}

	traceCtx := ctx
	traceCancel := func() {}
	if traceCtx == nil || traceCtx.Err() != nil {
		traceCtx, traceCancel = context.WithTimeout(context.Background(), 2*time.Second)
	}
	defer traceCancel()

	if err := input.ExecCtx.TraceWriter.WriteSpan(traceCtx, span); err != nil {
		log.Printf("WARNING: Failed to write durable activity trace span: %v", err)
	}
}

// RegisterNodeRunners creates activity handlers from all registered NodeRunners
// and registers them in the activity handler registry.
func RegisterNodeRunners(registry *runtime.ActivityHandlerRegistry, runnerRegistry *workflow.RunnerRegistry) {
	// Register adapters for known node types
	for _, nodeType := range []workflow.NodeType{
		workflow.NodeTypePrompt,
		workflow.NodeTypeConditional,
		workflow.NodeTypeResult,
		workflow.NodeTypeOperation,
		workflow.NodeTypeChildWorkflow,
		workflow.NodeTypeContractExtract,
		workflow.NodeTypeAgentRun,
		workflow.NodeTypeNovoRun,
	} {
		runner, ok := runnerRegistry.Get(nodeType)
		if ok {
			adapter := NewNodeRunnerAdapter(runner)
			registry.Register(adapter)
		}
	}
}
