package workflow

import (
	"context"
	"fmt"
	"testing"

	"github.com/alhasaniq/consortium/pkg/providers"
)

func TestResultNodeRunner_PopulatesAgentModel(t *testing.T) {
	capture := &captureInputsAggregator{}
	registry := NewAggregatorRegistry()
	registry.Register(capture)

	runner := &ResultNodeRunner{
		aggregatorRegistry: registry,
	}

	node := &Node{
		ID:                "result1",
		Type:              NodeTypeResult,
		OutputName:        "final_output",
		AggregationMethod: AggMethodPeerMatrix,
		Metadata: map[string]interface{}{
			"input_ids": []interface{}{"agent-a", "agent-b"},
		},
	}

	workflowCtx := map[string]interface{}{
		"agent-a":        "Response from A",
		"agent-a__model": "x-ai/grok-4.1-fast",
		"agent-b":        "Response from B",
		"agent-b__model": "minimax/minimax-m2.5",
	}

	sc := &NodeContext{
		Ctx:             context.Background(),
		NodeID:          "result1",
		Node:            node,
		WorkflowContext: workflowCtx,
	}

	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}
	if !capture.called {
		t.Fatal("Expected aggregator to be called for peer_matrix result node")
	}
	if len(capture.inputs) != 2 {
		t.Fatalf("Expected 2 aggregated inputs, got %d", len(capture.inputs))
	}
	if capture.modelForAgent("agent-a") != "x-ai/grok-4.1-fast" {
		t.Fatalf("Expected model for agent-a to propagate, got %q", capture.modelForAgent("agent-a"))
	}
	if capture.modelForAgent("agent-b") != "minimax/minimax-m2.5" {
		t.Fatalf("Expected model for agent-b to propagate, got %q", capture.modelForAgent("agent-b"))
	}
}

func TestResultNodeRunner_ModelAbsentGracefullyHandled(t *testing.T) {
	capture := &captureInputsAggregator{}
	registry := NewAggregatorRegistry()
	registry.Register(capture)

	runner := &ResultNodeRunner{
		aggregatorRegistry: registry,
	}

	node := &Node{
		ID:                "result1",
		Type:              NodeTypeResult,
		OutputName:        "final_output",
		AggregationMethod: AggMethodPeerMatrix,
		Metadata: map[string]interface{}{
			"input_ids": []interface{}{"agent-a", "agent-b"},
		},
	}

	// No __model key — simulates old workflows or non-agent nodes
	workflowCtx := map[string]interface{}{
		"agent-a": "Response from A",
		"agent-b": "Response from B",
	}

	sc := &NodeContext{
		Ctx:             context.Background(),
		NodeID:          "result1",
		Node:            node,
		WorkflowContext: workflowCtx,
	}

	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}
	if !capture.called {
		t.Fatal("Expected aggregator to be called for peer_matrix result node")
	}
	if capture.modelForAgent("agent-a") != "" {
		t.Fatalf("Expected empty model for agent-a when __model key is missing, got %q", capture.modelForAgent("agent-a"))
	}
	if capture.modelForAgent("agent-b") != "" {
		t.Fatalf("Expected empty model for agent-b when __model key is missing, got %q", capture.modelForAgent("agent-b"))
	}
}

func TestResultNodeRunner_AgreementRatioZeroPersisted(t *testing.T) {
	aggWithZeroRatio := &agreementRatioZeroAggregator{}
	registry := NewAggregatorRegistry()
	registry.Register(aggWithZeroRatio)

	runner := &ResultNodeRunner{
		aggregatorRegistry: registry,
	}

	node := &Node{
		ID:                "result1",
		Type:              NodeTypeResult,
		OutputName:        "final_output",
		AggregationMethod: AggMethodMajorityVote,
		Metadata: map[string]interface{}{
			"input_ids": []interface{}{"agent-a", "agent-b"},
		},
	}

	workflowCtx := map[string]interface{}{
		"agent-a": "The answer is A.",
		"agent-b": "The answer is B.",
	}

	sc := &NodeContext{
		Ctx:             context.Background(),
		NodeID:          "result1",
		Node:            node,
		WorkflowContext: workflowCtx,
	}

	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("Expected success, got error: %s", result.Error)
	}

	meta := result.Metadata
	val, ok := meta["agreement_ratio"]
	if !ok {
		t.Fatal("Expected agreement_ratio key to exist even when ratio is 0")
	}
	ratio, ok := val.(float64)
	if !ok {
		t.Fatalf("Expected agreement_ratio to be float64, got %T", val)
	}
	if ratio != 0 {
		t.Errorf("Expected agreement_ratio=0, got %v", ratio)
	}
}

// agreementRatioZeroAggregator is a test aggregator that returns explicit zero agreement.
type agreementRatioZeroAggregator struct{}

func (a *agreementRatioZeroAggregator) Name() AggregationMethod { return AggMethodMajorityVote }

func (a *agreementRatioZeroAggregator) Aggregate(_ context.Context, inputs []AgentOutput, _ map[string]interface{}, _ *providers.Client, _ *AggregationContext) (*AggregationResult, error) {
	return &AggregationResult{
		Output:         inputs[0].Output,
		Method:         AggMethodMajorityVote,
		Winner:         inputs[0].AgentID,
		Scores:         map[string]float64{inputs[0].AgentID: 1},
		AgreementRatio: 0,
	}, nil
}

type captureInputsAggregator struct {
	called bool
	inputs []AgentOutput
}

func (a *captureInputsAggregator) Name() AggregationMethod { return AggMethodPeerMatrix }

func (a *captureInputsAggregator) Aggregate(_ context.Context, inputs []AgentOutput, _ map[string]interface{}, _ *providers.Client, _ *AggregationContext) (*AggregationResult, error) {
	a.called = true
	a.inputs = append(a.inputs[:0], inputs...)
	if len(inputs) == 0 {
		return nil, fmt.Errorf("expected at least one input")
	}
	scores := map[string]float64{}
	for _, in := range inputs {
		scores[in.AgentID] = 1
	}
	return &AggregationResult{
		Output: inputs[0].Output,
		Method: AggMethodPeerMatrix,
		Winner: inputs[0].AgentID,
		Scores: scores,
	}, nil
}

func (a *captureInputsAggregator) modelForAgent(agentID string) string {
	for _, in := range a.inputs {
		if in.AgentID == agentID {
			return in.Model
		}
	}
	return ""
}
