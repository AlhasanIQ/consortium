package durable

import (
	"context"
	"errors"
	"testing"

	"github.com/alhasaniq/consortium/pkg/trace"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

type mockNodeRunner struct {
	nodeType workflow.NodeType
	result   *workflow.NodeResult
	err      error
	seen     *workflow.NodeContext
}

type mockActivityTraceWriter struct {
	spans []*trace.Span
}

func (m *mockActivityTraceWriter) WriteSpan(_ context.Context, span *trace.Span) error {
	m.spans = append(m.spans, span)
	return nil
}

func (m *mockNodeRunner) NodeType() workflow.NodeType {
	return m.nodeType
}

func (m *mockNodeRunner) Execute(sc *workflow.NodeContext) (*workflow.NodeResult, error) {
	m.seen = sc
	return m.result, m.err
}

func TestNewNodeRunnerAdapter_Type(t *testing.T) {
	runner := &mockNodeRunner{nodeType: workflow.NodeTypePrompt}
	adapter := NewNodeRunnerAdapter(runner)
	if adapter.Type() != runtime.ActivityTypeLLMCall {
		t.Fatalf("expected activity type %s, got %s", runtime.ActivityTypeLLMCall, adapter.Type())
	}
}

func TestNodeRunnerAdapter_ExecuteSuccess(t *testing.T) {
	runner := &mockNodeRunner{
		nodeType: workflow.NodeTypePrompt,
		result: &workflow.NodeResult{
			NodeID:       "n1",
			Success:      true,
			Output:       "ok",
			TokensInput:  11,
			TokensOutput: 22,
			Cost:         0.12,
			LatencyMs:    9.5,
			Metadata: map[string]interface{}{
				"source": "test",
			},
		},
	}
	adapter := NewNodeRunnerAdapter(runner)

	input := &runtime.ActivityInput{
		NodeID:          "n1",
		Node:            &workflow.Node{ID: "n1", Type: workflow.NodeTypePrompt, Prompt: "hello"},
		WorkflowContext: map[string]interface{}{"topic": "x"},
	}
	out, err := adapter.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !out.Success {
		t.Fatalf("expected success output")
	}
	if out.Output != "ok" {
		t.Fatalf("expected output 'ok', got %q", out.Output)
	}
	if out.TokensInput != 11 || out.TokensOutput != 22 {
		t.Fatalf("unexpected token accounting: in=%d out=%d", out.TokensInput, out.TokensOutput)
	}
	if runner.seen == nil || runner.seen.Node == nil || runner.seen.Node.ID != "n1" {
		t.Fatalf("runner did not receive expected NodeContext")
	}
}

func TestNodeRunnerAdapter_ExecuteSuccessWritesNodeSpan(t *testing.T) {
	runner := &mockNodeRunner{
		nodeType: workflow.NodeTypePrompt,
		result: &workflow.NodeResult{
			NodeID:       "n1",
			Success:      true,
			Output:       "ok",
			TokensInput:  11,
			TokensOutput: 22,
			Cost:         0.12,
			LatencyMs:    9.5,
		},
	}
	adapter := NewNodeRunnerAdapter(runner)
	traceWriter := &mockActivityTraceWriter{}

	out, err := adapter.Execute(context.Background(), &runtime.ActivityInput{
		ActivityID: "job-1:n1:1",
		NodeID:     "n1",
		Node:       &workflow.Node{ID: "n1", Type: workflow.NodeTypePrompt},
		Attempt:    1,
		ExecCtx: &workflow.ExecutionContext{
			JobID:       "job-1",
			TraceWriter: traceWriter,
		},
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !out.Success {
		t.Fatalf("expected success output")
	}
	if len(traceWriter.spans) != 1 {
		t.Fatalf("expected 1 trace span, got %d", len(traceWriter.spans))
	}
	span := traceWriter.spans[0]
	if span.JobID != "job-1" {
		t.Fatalf("expected span job_id job-1, got %q", span.JobID)
	}
	if span.NodeID != "n1" {
		t.Fatalf("expected span node_id n1, got %q", span.NodeID)
	}
	if span.Kind != trace.KindNode {
		t.Fatalf("expected span kind %q, got %q", trace.KindNode, span.Kind)
	}
	if span.Status != trace.StatusOK {
		t.Fatalf("expected span status %q, got %q", trace.StatusOK, span.Status)
	}
}

func TestNodeRunnerAdapter_ExecuteRunnerError(t *testing.T) {
	runner := &mockNodeRunner{
		nodeType: workflow.NodeTypePrompt,
		err:      errors.New("boom"),
	}
	adapter := NewNodeRunnerAdapter(runner)
	out, err := adapter.Execute(context.Background(), &runtime.ActivityInput{
		NodeID: "n1",
		Node:   &workflow.Node{ID: "n1", Type: workflow.NodeTypePrompt},
	})
	if err != nil {
		t.Fatalf("expected nil Go error, got %v", err)
	}
	if out.Success {
		t.Fatalf("expected failed output")
	}
	if out.Error != "boom" {
		t.Fatalf("expected boom error, got %q", out.Error)
	}
}

func TestNodeRunnerAdapter_ExecuteRunnerErrorWritesCancelledSpanWhenContextDone(t *testing.T) {
	runner := &mockNodeRunner{
		nodeType: workflow.NodeTypePrompt,
		err:      context.Canceled,
	}
	adapter := NewNodeRunnerAdapter(runner)
	traceWriter := &mockActivityTraceWriter{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	out, err := adapter.Execute(ctx, &runtime.ActivityInput{
		ActivityID: "job-1:n1:1",
		NodeID:     "n1",
		Node:       &workflow.Node{ID: "n1", Type: workflow.NodeTypePrompt},
		Attempt:    1,
		ExecCtx: &workflow.ExecutionContext{
			JobID:       "job-1",
			TraceWriter: traceWriter,
		},
	})
	if err != nil {
		t.Fatalf("expected nil Go error, got %v", err)
	}
	if out.Success {
		t.Fatalf("expected failed output")
	}
	if len(traceWriter.spans) != 1 {
		t.Fatalf("expected 1 trace span, got %d", len(traceWriter.spans))
	}
	if traceWriter.spans[0].Status != trace.StatusCancelled {
		t.Fatalf("expected cancelled span status, got %q", traceWriter.spans[0].Status)
	}
}

func TestNodeRunnerAdapter_ExecuteRunnerErrorWithPartialResult(t *testing.T) {
	runner := &mockNodeRunner{
		nodeType: workflow.NodeTypePrompt,
		result: &workflow.NodeResult{
			NodeID:       "n1",
			Success:      false,
			Output:       "partial output",
			Error:        "cost limit exceeded",
			TokensInput:  10,
			TokensOutput: 5,
			Cost:         0.42,
		},
		err: errors.New("cost limit exceeded"),
	}
	adapter := NewNodeRunnerAdapter(runner)

	out, err := adapter.Execute(context.Background(), &runtime.ActivityInput{
		NodeID: "n1",
		Node:   &workflow.Node{ID: "n1", Type: workflow.NodeTypePrompt},
	})
	if err != nil {
		t.Fatalf("expected nil Go error, got %v", err)
	}
	if out.Success {
		t.Fatalf("expected failed output")
	}
	if out.Output != "partial output" {
		t.Fatalf("expected partial output to be preserved, got %q", out.Output)
	}
	if out.Error != "cost limit exceeded" {
		t.Fatalf("expected node error to be preserved, got %q", out.Error)
	}
	if out.TokensInput != 10 || out.TokensOutput != 5 {
		t.Fatalf("expected token accounting to be preserved, got in=%d out=%d", out.TokensInput, out.TokensOutput)
	}
	if out.Cost != 0.42 {
		t.Fatalf("expected cost to be preserved, got %f", out.Cost)
	}
}

func TestNodeRunnerAdapter_ExecuteNilResult(t *testing.T) {
	runner := &mockNodeRunner{nodeType: workflow.NodeTypePrompt}
	adapter := NewNodeRunnerAdapter(runner)
	out, err := adapter.Execute(context.Background(), &runtime.ActivityInput{
		NodeID: "n1",
		Node:   &workflow.Node{ID: "n1", Type: workflow.NodeTypePrompt},
	})
	if err != nil {
		t.Fatalf("expected nil Go error, got %v", err)
	}
	if out.Success {
		t.Fatalf("expected failed output")
	}
	if out.Error != "runner returned nil result" {
		t.Fatalf("unexpected error message: %q", out.Error)
	}
}

func TestNodeRunnerAdapter_ExecuteNilNodeInput(t *testing.T) {
	runner := &mockNodeRunner{nodeType: workflow.NodeTypePrompt}
	adapter := NewNodeRunnerAdapter(runner)
	_, err := adapter.Execute(context.Background(), &runtime.ActivityInput{NodeID: "n1"})
	if err == nil {
		t.Fatal("expected error for nil node")
	}
}

func TestRegisterNodeRunners_RegistersKnownTypes(t *testing.T) {
	runnerRegistry := workflow.NewRunnerRegistry()
	runnerRegistry.Register(&mockNodeRunner{nodeType: workflow.NodeTypePrompt, result: &workflow.NodeResult{Success: true}})
	runnerRegistry.Register(&mockNodeRunner{nodeType: workflow.NodeTypeResult, result: &workflow.NodeResult{Success: true}})
	runnerRegistry.Register(&mockNodeRunner{nodeType: workflow.NodeTypeOperation, result: &workflow.NodeResult{Success: true}})
	runnerRegistry.Register(&mockNodeRunner{nodeType: workflow.NodeTypeAgentRun, result: &workflow.NodeResult{Success: true}})

	activityRegistry := runtime.NewActivityHandlerRegistry()
	RegisterNodeRunners(activityRegistry, runnerRegistry)

	if _, err := activityRegistry.Get(runtime.ActivityTypeLLMCall); err != nil {
		t.Fatalf("expected llm_call handler to be registered: %v", err)
	}
	if _, err := activityRegistry.Get(runtime.ActivityTypeResult); err != nil {
		t.Fatalf("expected result handler to be registered: %v", err)
	}
	if _, err := activityRegistry.Get(runtime.ActivityType(workflow.NodeTypeOperation)); err != nil {
		t.Fatalf("expected operation handler to be registered: %v", err)
	}
	if _, err := activityRegistry.Get(runtime.ActivityTypeAgentRun); err != nil {
		t.Fatalf("expected agent_run handler to be registered: %v", err)
	}
	if _, err := activityRegistry.Get(runtime.ActivityTypeConditional); err == nil {
		t.Fatalf("did not expect conditional handler when no conditional runner is registered")
	}
}
