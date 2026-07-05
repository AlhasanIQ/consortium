package admin

import (
	"fmt"
	"strings"

	"github.com/alhasaniq/consortium/pkg/bench"
	"github.com/alhasaniq/consortium/pkg/typeconv"
	"github.com/alhasaniq/consortium/pkg/workflow"
)

// --- Contract helpers ---

// applyPerOptionToolcallContract scans the workflow for nodes that declare
// tool_choice but have no tools defined, and injects per-option tool
// definitions built from the benchmark item's choices. This is data-driven:
// any workflow whose definition includes toolChoice on a node automatically
// gets tools injected — no workflow-ID matching required.
func applyPerOptionToolcallContract(execWorkflow *workflow.Workflow, item bench.DatasetItem) error {
	if execWorkflow == nil {
		return nil
	}

	for _, node := range execWorkflow.Nodes {
		if node == nil || node.Metadata == nil {
			continue
		}
		if _, hasToolChoice := node.Metadata["tool_choice"]; !hasToolChoice {
			continue
		}
		if node.Metadata["tools"] != nil {
			continue // already has tools defined
		}
		tools := buildOptionTools(item.Choices)
		if len(tools) == 0 {
			return fmt.Errorf("node %s has tool_choice but question has no options to build tools from", node.ID)
		}
		node.Metadata["tools"] = tools
	}
	return nil
}

func buildOptionTools(choices []string) []map[string]interface{} {
	tools := make([]map[string]interface{}, 0, len(choices))
	for i := 0; i < len(choices); i++ {
		label := bench.LabelForIndex(i)
		if label == "" {
			break
		}
		description := fmt.Sprintf("Select option %s as the final answer.", label)
		choiceText := strings.TrimSpace(choices[i])
		if choiceText != "" {
			description = fmt.Sprintf("Select option %s: %s", label, choiceText)
		}
		tools = append(tools, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        fmt.Sprintf("choose_%s", strings.ToLower(label)),
				"description": description,
				"parameters": map[string]interface{}{
					"type":                 "object",
					"properties":           map[string]interface{}{},
					"additionalProperties": false,
				},
				"strict": true,
			},
		})
	}
	return tools
}

type contractNodeDiagnostics struct {
	NodeID           string
	Model            string
	FinishReason     string
	MaxTokens        int
	TokensOutput     int
	ExtractionMethod string
}

func extractContractNodeDiagnostics(nodes []*workflow.NodeResult) contractNodeDiagnostics {
	for _, node := range nodes {
		if node == nil || !strings.Contains(strings.ToLower(node.NodeID), "contract") {
			continue
		}
		diag := contractNodeDiagnostics{
			NodeID:       node.NodeID,
			TokensOutput: node.TokensOutput,
		}
		if node.Metadata != nil {
			if v, ok := node.Metadata["model"]; ok {
				diag.Model = typeconv.AsString(v)
			}
			if v, ok := node.Metadata["finish_reason"]; ok {
				diag.FinishReason = typeconv.AsString(v)
			}
			if v, ok := node.Metadata["max_tokens"]; ok {
				diag.MaxTokens = typeconv.AsInt(v)
			}
			if v, ok := node.Metadata["extraction_method"]; ok {
				diag.ExtractionMethod = typeconv.AsString(v)
			}
		}
		return diag
	}
	return contractNodeDiagnostics{}
}

func applyContractNodeDiagnostics(attempt *bench.AttemptDetail, diag contractNodeDiagnostics) {
	if attempt == nil || diag.NodeID == "" {
		return
	}
	attempt.ContractNodeID = diag.NodeID
	attempt.ContractModel = diag.Model
	attempt.ContractFinishReason = diag.FinishReason
	attempt.ContractTokensOutput = diag.TokensOutput
	attempt.ContractMaxTokens = diag.MaxTokens
	attempt.ContractExtractionMethod = diag.ExtractionMethod
}

func enrichContractFailureDetails(attempt *bench.AttemptDetail) {
	if attempt == nil || attempt.FailureReason == "" || attempt.ContractNodeID == "" {
		return
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("contract_node=%s", attempt.ContractNodeID))
	if attempt.ContractExtractionMethod != "" {
		parts = append(parts, fmt.Sprintf("extraction=%s", attempt.ContractExtractionMethod))
	}
	if attempt.ContractModel != "" {
		parts = append(parts, fmt.Sprintf("model=%s", attempt.ContractModel))
	}
	if attempt.ContractFinishReason != "" {
		parts = append(parts, fmt.Sprintf("finish_reason=%s", attempt.ContractFinishReason))
	}
	if attempt.ContractTokensOutput > 0 {
		parts = append(parts, fmt.Sprintf("tokens_output=%d", attempt.ContractTokensOutput))
	}
	if attempt.ContractMaxTokens > 0 {
		parts = append(parts, fmt.Sprintf("max_tokens=%d", attempt.ContractMaxTokens))
	}
	if len(parts) == 0 {
		return
	}
	attempt.ContractDiagnostic = strings.Join(parts, " ")
	if strings.TrimSpace(attempt.Error) == "" &&
		(attempt.FailureReason == bench.FailureReasonEmptyFinalOutput ||
			attempt.FailureReason == bench.FailureReasonInvalidContract) {
		attempt.Error = attempt.ContractDiagnostic
	}
}
