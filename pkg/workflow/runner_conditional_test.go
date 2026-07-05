package workflow

import (
	"context"
	"testing"
)

// Tests in this file cover conditional runner scenarios NOT already in runner_test.go:
// - Context variable interpolation in conditions
// - Branch node ID format verification
// - Both branches nil (false condition, no false branch)
// - Executor receives correct branch node

func TestConditionalNodeRunner_ContextVarCondition(t *testing.T) {
	// Test condition that uses context variable interpolation
	var receivedNodeID string
	mockExecutor := NodeExecutorFunc(func(ctx context.Context, node *Node, nodeID string, wfCtx map[string]interface{}, execCtx *ExecutionContext, ct *CostTracker) (*NodeResult, error) {
		receivedNodeID = nodeID
		return &NodeResult{NodeID: nodeID, Success: true, Output: "ok"}, nil
	})
	runner := &ConditionalNodeRunner{executor: mockExecutor}

	sc := &NodeContext{
		Ctx:    context.Background(),
		NodeID: "cond-var",
		Node: &Node{
			Type:      NodeTypeConditional,
			Condition: `answer equals yes`,
			TrueBranch: &Node{
				Type: NodeTypeResult,
			},
		},
		WorkflowContext: map[string]interface{}{
			"answer": "yes",
		},
	}

	result, err := runner.Execute(sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedNodeID != "cond-var_true" {
		t.Errorf("branch nodeID = %q, want %q", receivedNodeID, "cond-var_true")
	}
	// Verify NodeID is remapped to parent
	if result.NodeID != "cond-var" {
		t.Errorf("result NodeID = %q, want %q", result.NodeID, "cond-var")
	}
}

func TestConditionalNodeRunner_FalseConditionNoFalseBranch(t *testing.T) {
	// Condition evaluates to false, but there is no false branch.
	called := false
	mockExecutor := NodeExecutorFunc(func(ctx context.Context, node *Node, nodeID string, wfCtx map[string]interface{}, execCtx *ExecutionContext, ct *CostTracker) (*NodeResult, error) {
		called = true
		return &NodeResult{NodeID: nodeID, Success: true}, nil
	})
	runner := &ConditionalNodeRunner{executor: mockExecutor}

	sc := &NodeContext{
		Ctx:    context.Background(),
		NodeID: "cond-nofb",
		Node: &Node{
			Type:      NodeTypeConditional,
			Condition: "status equals active",
			TrueBranch: &Node{
				Type: NodeTypeResult,
			},
			// No FalseBranch
		},
		WorkflowContext: map[string]interface{}{
			"status": "inactive",
		},
	}

	result, err := runner.Execute(sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("executor should not be called when condition is false and no false branch")
	}
	if !result.Success {
		t.Error("expected success")
	}
	if result.Output != "" {
		t.Errorf("expected empty output, got %q", result.Output)
	}
}

func TestConditionalNodeRunner_BranchNodeReceived(t *testing.T) {
	// Verify the correct branch *node* is passed to the executor
	var receivedNode *Node
	mockExecutor := NodeExecutorFunc(func(ctx context.Context, node *Node, nodeID string, wfCtx map[string]interface{}, execCtx *ExecutionContext, ct *CostTracker) (*NodeResult, error) {
		receivedNode = node
		return &NodeResult{NodeID: nodeID, Success: true, Output: "done"}, nil
	})
	runner := &ConditionalNodeRunner{executor: mockExecutor}

	falseBranch := &Node{
		Type:  NodeTypePrompt,
		Model: "false-model",
	}
	sc := &NodeContext{
		Ctx:    context.Background(),
		NodeID: "cond-check",
		Node: &Node{
			Type:      NodeTypeConditional,
			Condition: "x equals y",
			TrueBranch: &Node{
				Type:  NodeTypePrompt,
				Model: "true-model",
			},
			FalseBranch: falseBranch,
		},
		WorkflowContext: map[string]interface{}{
			"x": "no",
		},
	}

	_, err := runner.Execute(sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedNode != falseBranch {
		t.Error("executor should receive the false branch node")
	}
	if receivedNode.Model != "false-model" {
		t.Errorf("branch model = %q, want %q", receivedNode.Model, "false-model")
	}
}

func TestConditionalNodeRunner_ResultPropagated(t *testing.T) {
	// Verify that result fields from branch execution are propagated through
	// (except NodeID which is overwritten).
	mockExecutor := NodeExecutorFunc(func(ctx context.Context, node *Node, nodeID string, wfCtx map[string]interface{}, execCtx *ExecutionContext, ct *CostTracker) (*NodeResult, error) {
		return &NodeResult{
			NodeID:       nodeID,
			Success:      true,
			Output:       "detailed output",
			TokensInput:  100,
			TokensOutput: 50,
			Cost:         0.01,
		}, nil
	})
	runner := &ConditionalNodeRunner{executor: mockExecutor}

	sc := &NodeContext{
		Ctx:    context.Background(),
		NodeID: "cond-prop",
		Node: &Node{
			Type:       NodeTypeConditional,
			Condition:  "status contains active",
			TrueBranch: &Node{Type: NodeTypePrompt},
		},
		WorkflowContext: map[string]interface{}{"status": "active"},
	}

	result, err := runner.Execute(sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NodeID != "cond-prop" {
		t.Errorf("NodeID = %q, want %q", result.NodeID, "cond-prop")
	}
	if result.Output != "detailed output" {
		t.Errorf("Output = %q, want %q", result.Output, "detailed output")
	}
	if result.TokensInput != 100 {
		t.Errorf("TokensInput = %d, want 100", result.TokensInput)
	}
	if result.TokensOutput != 50 {
		t.Errorf("TokensOutput = %d, want 50", result.TokensOutput)
	}
	if result.Cost != 0.01 {
		t.Errorf("Cost = %f, want 0.01", result.Cost)
	}
}

func TestConditionalNodeRunner_CompiledAggregationBranchCarrierClearsAccounting(t *testing.T) {
	mockExecutor := NodeExecutorFunc(func(ctx context.Context, node *Node, nodeID string, wfCtx map[string]interface{}, execCtx *ExecutionContext, ct *CostTracker) (*NodeResult, error) {
		return &NodeResult{
			NodeID:       nodeID,
			Success:      true,
			Output:       "winner",
			TokensInput:  100,
			TokensOutput: 50,
			Cost:         0.04,
			LatencyMs:    250,
		}, nil
	})
	runner := &ConditionalNodeRunner{executor: mockExecutor}

	sc := &NodeContext{
		Ctx:    context.Background(),
		NodeID: "agg--repair-selection",
		Node: &Node{
			Type:       NodeTypeConditional,
			Condition:  "selection is_empty",
			TrueBranch: &Node{Type: NodeTypePrompt},
			Metadata: map[string]interface{}{
				"aggregation_group_node_id": "agg--result",
			},
		},
		WorkflowContext: map[string]interface{}{"selection": ""},
	}

	result, err := runner.Execute(sc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NodeID != "agg--repair-selection" {
		t.Fatalf("NodeID = %q, want agg--repair-selection", result.NodeID)
	}
	if result.TokensInput != 0 || result.TokensOutput != 0 || result.Cost != 0 || result.LatencyMs != 0 {
		t.Fatalf("compiled conditional carrier accounting = tokens %d/%d cost %f latency %f, want zero", result.TokensInput, result.TokensOutput, result.Cost, result.LatencyMs)
	}
	if result.Output != "winner" {
		t.Fatalf("Output = %q, want winner", result.Output)
	}
	if result.Metadata["conditional_branch_node_id"] != "agg--repair-selection_true" {
		t.Fatalf("conditional branch metadata missing: %+v", result.Metadata)
	}
}
