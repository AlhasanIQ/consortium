package workflow

import (
	"fmt"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/trace"
	"github.com/google/uuid"
)

// ResultNodeRunner executes output/aggregation nodes.
type ResultNodeRunner struct {
	llmClient          *providers.Client
	aggregatorRegistry *AggregatorRegistry
}

// NodeType returns NodeTypeResult ("result").
func (r *ResultNodeRunner) NodeType() NodeType { return NodeTypeResult }

// Execute runs the output node, collecting inputs and optionally aggregating them.
func (r *ResultNodeRunner) Execute(sc *NodeContext) (*NodeResult, error) {
	startTime := time.Now()

	// Get output name - check metadata first, then OutputName field, then use nodeID
	nodeMeta := sc.Node.DisplayMeta()
	outputName := sc.NodeID
	if nodeMeta.OutputName != "" {
		outputName = nodeMeta.OutputName
	}
	if sc.Node.OutputName != "" {
		outputName = sc.Node.OutputName
	}

	// Collect inputs from all incoming edges
	var collectedInputs []AgentOutput
	inputIDs := nodeMeta.InputIDs

	// Resolve input_ids to collected inputs
	if len(inputIDs) > 0 {
		for _, id := range inputIDs {
			if val, exists := sc.WorkflowContext[id]; exists {
				model, _ := sc.WorkflowContext[id+"__model"].(string)
				collectedInputs = append(collectedInputs, AgentOutput{
					AgentID: id,
					Model:   model,
					Output:  fmt.Sprintf("%v", val),
				})
			}
		}
	}

	// Aggregation method is required and validated at submit time.
	method := sc.Node.AggregationMethod
	if method == "" {
		err := NewNonRetryableError(fmt.Errorf("result node aggregation_method is required"), "INVALID_CONFIG")
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}

	// Get or create aggregation config
	aggConfig := make(map[string]interface{}, len(sc.Node.AggregationConfig)+1)
	for k, v := range sc.Node.AggregationConfig {
		aggConfig[k] = v
	}

	// Add original prompt to config for synthesis
	if userPrompt, ok := sc.WorkflowContext["user_prompt"]; ok {
		aggConfig["original_prompt"] = fmt.Sprintf("%v", userPrompt)
	}

	var outputStr string
	var aggMetadata map[string]interface{}

	// Check if we should use an aggregator
	if len(collectedInputs) > 1 {
		// Get aggregator from registry
		aggregator, ok := r.aggregatorRegistry.Get(method)
		if !ok {
			return nil, fmt.Errorf("unknown aggregation method: %s", method)
		}

		// Build aggregation context
		aggCtx := &AggregationContext{
			NodeID:      sc.NodeID,
			CostTracker: sc.CostTracker,
		}
		if sc.ExecCtx != nil {
			aggCtx.JobID = sc.ExecCtx.JobID
			aggCtx.RunID = sc.ExecCtx.RunID
		}

		// Execute aggregation
		aggStartTime := time.Now()
		result, err := aggregator.Aggregate(sc.Ctx, collectedInputs, aggConfig, r.llmClient, aggCtx)
		aggDuration := float64(time.Since(aggStartTime).Milliseconds())
		if err != nil {
			failureMeta := map[string]interface{}{
				"aggregation_method": string(method),
			}
			if method != AggMethodCollect {
				failureMeta["legacy_result_aggregation"] = true
			}
			for k, v := range BuildErrorObservabilityMetadata(err) {
				failureMeta[k] = v
			}
			if subcallErr, ok := asScoringSubcallError(err); ok {
				for k, v := range subcallErr.metadata() {
					failureMeta[k] = v
				}
			}
			writeSpan(sc.Ctx, sc.ExecCtx, &trace.Span{
				NodeID: sc.NodeID, SpanID: uuid.NewString(), ParentSpanID: sc.ParentSpanID,
				Kind: trace.KindCall, Status: trace.StatusError, StartedAt: aggStartTime, DurationMs: aggDuration,
				Attributes: map[string]any{"call_type": "aggregation", "method": string(method), "input_count": len(collectedInputs), "error": err.Error()},
			})
			return newErrorResult(sc.NodeID, fmt.Sprintf("aggregation failed: %v", err), float64(time.Since(startTime).Milliseconds()), failureMeta), err
		}

		// Write aggregation call span
		writeSpan(sc.Ctx, sc.ExecCtx, &trace.Span{
			NodeID: sc.NodeID, SpanID: uuid.NewString(), ParentSpanID: sc.ParentSpanID,
			Kind: trace.KindCall, Status: trace.StatusOK, StartedAt: aggStartTime, DurationMs: aggDuration,
			Attributes: map[string]any{"call_type": "aggregation", "method": string(result.Method), "input_count": len(collectedInputs), "winner": result.Winner},
		})

		outputStr = result.Output
		aggMetadata = map[string]interface{}{
			"aggregation_method":    string(result.Method),
			"aggregation_winner":    result.Winner,
			"aggregation_scores":    result.Scores,
			"aggregation_reasoning": result.Reasoning,
		}
		if method != AggMethodCollect {
			aggMetadata["legacy_result_aggregation"] = true
		}
		// Include evaluation matrix for peer_matrix aggregation
		if result.EvalMatrix != nil {
			aggMetadata["eval_matrix"] = map[string]interface{}{
				"raw_scores":        result.EvalMatrix.RawScores,
				"normalized_scores": result.EvalMatrix.NormalizedScores,
				"final_scores":      result.EvalMatrix.FinalScores,
				"reviewer_bias":     result.EvalMatrix.ReviewerBias,
				"invalid_count":     result.EvalMatrix.InvalidCount,
			}
		}
		// Keep explicit zero so observability can distinguish "0" from missing.
		aggMetadata["agreement_ratio"] = result.AgreementRatio

		// Save a visible "decision" child node for multi-call aggregators
		// so the timeline shows the winner selection step.
		if (result.Method == AggMethodScoring || result.Method == AggMethodPeerMatrix) && aggCtx.JobID != "" {
			decisionOutput := formatDecisionOutput(result)
			decideNodeID := fmt.Sprintf("%s__agg__decide", sc.NodeID)
			r.llmClient.LogSubNode(
				aggCtx.JobID, aggCtx.RunID,
				decideNodeID, sc.NodeID,
				"result", "Aggregator",
				fmt.Sprintf("Winner: %s", result.Winner),
				decisionOutput, 0,
			)
		}
	} else {
		// Empty or single input - preserve historical passthrough behavior.
		if len(collectedInputs) == 0 {
			outputStr = ""
		} else if len(collectedInputs) == 1 {
			outputStr = collectedInputs[0].Output
		}
		aggMetadata = map[string]interface{}{
			"aggregation_method": string(AggMethodCollect),
		}
	}

	// Store in context with special key for outputs
	outputKey := fmt.Sprintf("__output_%s", outputName)
	sc.WorkflowContext[outputKey] = outputStr

	// Build result metadata
	resultMetadata := map[string]interface{}{
		"output_name":  outputName,
		"output_value": outputStr,
		"input_ids":    inputIDs,
	}
	for k, v := range aggMetadata {
		resultMetadata[k] = v
	}

	// Cost limit enforcement is handled centrally by executeNode after runner returns.

	return &NodeResult{
		NodeID:       sc.NodeID,
		Success:      true,
		Output:       outputStr,
		TokensInput:  0, // Real metrics tracked on child aggregation node(s)
		TokensOutput: 0,
		Cost:         0,
		LatencyMs:    float64(time.Since(startTime).Milliseconds()),
		Metadata:     resultMetadata,
	}, nil
}

// formatDecisionOutput builds a human-readable summary of the aggregation decision.
func formatDecisionOutput(result *AggregationResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Winner: %s\n\nScores:\n", result.Winner)
	for agentID, score := range result.Scores {
		marker := ""
		if agentID == result.Winner {
			marker = " ← winner"
		}
		fmt.Fprintf(&b, "  %s: %.2f%s\n", agentID, score, marker)
	}
	if result.Output != "" {
		b.WriteString("\n--- Winning Answer ---\n")
		b.WriteString(result.Output)
	}
	return b.String()
}
