package workflow

import (
	"fmt"
	"time"

	"github.com/alhasaniq/consortium/pkg/trace"
	"github.com/google/uuid"
)

// ConditionalNodeRunner evaluates conditions and delegates to branch execution.
type ConditionalNodeRunner struct {
	executor NodeExecutor
}

// NodeType returns NodeTypeConditional ("conditional").
func (r *ConditionalNodeRunner) NodeType() NodeType { return NodeTypeConditional }

// Execute evaluates the condition and executes the appropriate branch.
func (r *ConditionalNodeRunner) Execute(sc *NodeContext) (*NodeResult, error) {
	// Evaluate condition
	conditionMet, err := EvaluateConditionExpression(sc.Node.Condition, sc.WorkflowContext)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate condition: %w", err)
	}

	// Execute appropriate branch
	var branchNode *Node
	var branchName string
	if conditionMet && sc.Node.TrueBranch != nil {
		branchNode = sc.Node.TrueBranch
		branchName = "true"
	} else if !conditionMet && sc.Node.FalseBranch != nil {
		branchNode = sc.Node.FalseBranch
		branchName = "false"
	}

	// Write decision span for the condition evaluation
	if sc.ExecCtx != nil {
		writeSpan(sc.Ctx, sc.ExecCtx, &trace.Span{
			NodeID:       sc.NodeID,
			SpanID:       uuid.NewString(),
			ParentSpanID: sc.ParentSpanID,
			Kind:         trace.KindDecision,
			Status:       trace.StatusOK,
			StartedAt:    time.Now(),
			Attributes: map[string]any{
				"decision_type": "condition",
				"condition":     sc.Node.Condition,
				"result":        conditionMet,
				"branch_taken":  branchName,
			},
		})
	}

	if branchNode == nil {
		// No branch to execute, return empty result
		return &NodeResult{
			NodeID:  sc.NodeID,
			Success: true,
			Output:  "",
		}, nil
	}

	// Generate branch node ID
	branchNodeID := fmt.Sprintf("%s_%s", sc.NodeID, branchName)

	// Execute the branch through the full node executor (with retry/spans)
	result, err := r.executor.ExecuteNode(sc.Ctx, branchNode, branchNodeID, sc.WorkflowContext, sc.ExecCtx, sc.CostTracker)
	if err != nil {
		return result, err
	}

	if result.Metadata == nil {
		result.Metadata = map[string]interface{}{}
	}
	result.Metadata["conditional_branch_node_id"] = branchNodeID
	result.Metadata["conditional_branch_name"] = branchName

	if compiledAggregationGroupParentNodeID(sc.NodeID, sc.Node.Metadata) != "" {
		result.TokensInput = 0
		result.TokensOutput = 0
		result.Cost = 0
		result.LatencyMs = 0
	}

	// Update node ID to parent conditional node
	result.NodeID = sc.NodeID
	return result, nil
}
