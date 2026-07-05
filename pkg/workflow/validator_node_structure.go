package workflow

import (
	"fmt"
	"regexp"
	"strings"
)

func validateNodeStructure(wf *Workflow, node *Node, nodeID string) []ValidationError {
	var errors []ValidationError

	if node.Type != NodeTypeOperation && node.Type != NodeTypeWorkflowRef {
		if node.RetryPolicy == nil {
			errors = append(errors, ValidationError{
				Field:   "retry_policy",
				NodeID:  nodeID,
				Message: "Node requires explicit retry_policy (no implicit defaults)",
			})
		} else if msg := validateRetryPolicy(node.RetryPolicy); msg != "" {
			errors = append(errors, ValidationError{
				Field:   "retry_policy",
				NodeID:  nodeID,
				Message: msg,
			})
		}
	}

	switch node.Type {
	case NodeTypePrompt:
		errors = append(errors, validatePromptBackedNodeStructure(node, nodeID)...)
	case NodeTypeContractExtract:
		errors = append(errors, validatePromptBackedNodeStructure(node, nodeID)...)
		errors = append(errors, validateContractExtractNodeStructure(node, nodeID)...)
	case NodeTypeChildWorkflow:
		errors = append(errors, validateChildWorkflowNodeStructure(wf, node, nodeID)...)
	case NodeTypeAgentRun:
		errors = append(errors, validateAgentRunNodeStructure(node, nodeID)...)
	case NodeTypeNovoRun:
		errors = append(errors, validateNovoRunNodeStructure(node, nodeID)...)
	case NodeTypeResult:
		errors = append(errors, validateResultNodeStructure(node, nodeID)...)
	case NodeTypeOperation:
		errors = append(errors, validateOperationNodeStructure(node, nodeID)...)
	case NodeTypeWorkflowRef:
		errors = append(errors, ValidationError{
			Field:   "type",
			NodeID:  nodeID,
			Message: "workflow_ref nodes must be compiled before runtime validation",
		})
	}

	return errors
}

func validateOperationNodeStructure(node *Node, nodeID string) []ValidationError {
	var errors []ValidationError
	required := map[string][][]string{
		OperationCollectInputs:    {{"input_ids"}, {"candidates"}},
		OperationFormatCandidates: {{"input_ids"}, {"candidates"}},
		OperationExtractAnswer:    {{"text"}},
		OperationCountVotes:       {{"answers"}},
		OperationGroupAnswerCamps: {{"answers"}, {"items"}},
		OperationSelectWinner:     {{"scores"}, {"selection", "outputs"}, {"winner_answer", "items"}, {"vote_result", "items"}},
		OperationReduceScores:     {{"scores"}},
		OperationParseJSONField:   {{"text", "field"}},
		OperationParseSelection:   {{"text", "labels"}},
	}

	requiredGroups, ok := required[node.OperationType]
	if strings.TrimSpace(node.OperationType) == "" {
		errors = append(errors, ValidationError{
			Field:   "operation_type",
			NodeID:  nodeID,
			Message: "Operation node requires operation_type",
		})
		return errors
	}
	if !ok {
		errors = append(errors, ValidationError{
			Field:   "operation_type",
			NodeID:  nodeID,
			Message: fmt.Sprintf("Unsupported operation_type %q", node.OperationType),
		})
		return errors
	}
	if !hasRequiredOperationConfigGroup(node.OperationConfig, requiredGroups) {
		errors = append(errors, ValidationError{
			Field:   "operation_config",
			NodeID:  nodeID,
			Message: fmt.Sprintf("Operation %q %s", node.OperationType, describeOperationConfigRequirement(requiredGroups)),
		})
	}
	return errors
}

func hasRequiredOperationConfigGroup(config map[string]interface{}, groups [][]string) bool {
	for _, group := range groups {
		matched := true
		for _, key := range group {
			if !hasOperationConfigKey(config, key) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func hasOperationConfigKey(config map[string]interface{}, key string) bool {
	value, ok := config[key]
	if !ok || value == nil {
		return false
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) != ""
	}
	return true
}

func describeOperationConfigRequirement(groups [][]string) string {
	if len(groups) == 1 && len(groups[0]) > 1 {
		return fmt.Sprintf("requires: %s", strings.Join(groups[0], ", "))
	}
	parts := make([]string, 0, len(groups))
	for _, group := range groups {
		parts = append(parts, strings.Join(group, ", "))
	}
	return fmt.Sprintf("requires one of: %s", strings.Join(parts, "; "))
}

func validatePromptBackedNodeStructure(node *Node, nodeID string) []ValidationError {
	var errors []ValidationError
	if node.Prompt == "" {
		errors = append(errors, ValidationError{
			Field:   "prompt",
			NodeID:  nodeID,
			Message: "Prompt node requires a prompt text",
		})
	}
	if node.Model == "" {
		errors = append(errors, ValidationError{
			Field:   "model",
			NodeID:  nodeID,
			Message: "Prompt node requires a model",
		})
	}
	if node.Temperature == nil {
		errors = append(errors, ValidationError{
			Field:   "temperature",
			NodeID:  nodeID,
			Message: "Prompt node requires explicit temperature (no implicit default)",
		})
	}
	if node.MaxTokens == 0 {
		errors = append(errors, ValidationError{
			Field:   "max_tokens",
			NodeID:  nodeID,
			Message: "Prompt node requires explicit max_tokens (no implicit default)",
		})
	}
	if node.TimeoutSeconds <= 0 {
		errors = append(errors, ValidationError{
			Field:   "timeout_seconds",
			NodeID:  nodeID,
			Message: "Prompt node requires explicit timeout_seconds (no implicit default)",
		})
	}
	return errors
}

func validateContractExtractNodeStructure(node *Node, nodeID string) []ValidationError {
	var errors []ValidationError
	sourceVar, _ := node.Metadata["source_variable"].(string)
	if strings.TrimSpace(sourceVar) == "" {
		errors = append(errors, ValidationError{
			Field:   "source_variable",
			NodeID:  nodeID,
			Message: "Contract extract node requires metadata source_variable",
		})
	}

	if _, ok := node.Metadata["extraction_patterns"]; ok {
		patterns := parseExtractionPatterns(node.Metadata)
		if len(patterns) == 0 {
			errors = append(errors, ValidationError{
				Field:   "extraction_patterns",
				NodeID:  nodeID,
				Message: "Contract extract node has invalid extraction_patterns metadata",
			})
		} else {
			for idx, pattern := range patterns {
				if _, err := regexp.Compile(pattern); err != nil {
					errors = append(errors, ValidationError{
						Field:   "extraction_patterns",
						NodeID:  nodeID,
						Message: fmt.Sprintf("Invalid extraction pattern at index %d: %v", idx, err),
					})
				}
			}
		}
	}
	return errors
}

func validateChildWorkflowNodeStructure(wf *Workflow, node *Node, nodeID string) []ValidationError {
	var errors []ValidationError
	if strings.TrimSpace(node.ChildWorkflowID) == "" {
		errors = append(errors, ValidationError{
			Field:   "child_workflow_id",
			NodeID:  nodeID,
			Message: "Child workflow node requires child_workflow_id",
		})
	}
	if wf.ID != "" && strings.TrimSpace(node.ChildWorkflowID) == strings.TrimSpace(wf.ID) {
		errors = append(errors, ValidationError{
			Field:   "child_workflow_id",
			NodeID:  nodeID,
			Message: "Child workflow cannot reference itself",
		})
	}
	if node.TimeoutSeconds <= 0 {
		errors = append(errors, ValidationError{
			Field:   "timeout_seconds",
			NodeID:  nodeID,
			Message: "Child workflow node requires explicit timeout_seconds (no implicit default)",
		})
	}
	return errors
}

func validateAgentRunNodeStructure(node *Node, nodeID string) []ValidationError {
	var errors []ValidationError
	if strings.TrimSpace(node.Prompt) == "" {
		errors = append(errors, ValidationError{
			Field:   "prompt",
			NodeID:  nodeID,
			Message: "Agent run node requires a prompt text",
		})
	}
	if strings.TrimSpace(node.Harness) == "" {
		errors = append(errors, ValidationError{
			Field:   "harness",
			NodeID:  nodeID,
			Message: "Agent run node requires a harness",
		})
	} else if !isSupportedAgentRunHarness(node.Harness) {
		errors = append(errors, ValidationError{
			Field:   "harness",
			NodeID:  nodeID,
			Message: fmt.Sprintf("Unsupported agent run harness '%s'", node.Harness),
		})
	}
	if node.TimeoutSeconds <= 0 {
		errors = append(errors, ValidationError{
			Field:   "timeout_seconds",
			NodeID:  nodeID,
			Message: "Agent run node requires explicit timeout_seconds (no implicit default)",
		})
	}
	sandbox := strings.TrimSpace(node.Sandbox)
	if sandbox != "" && !IsSupportedNovomoSandbox(sandbox) {
		errors = append(errors, ValidationError{
			Field:   "sandbox",
			NodeID:  nodeID,
			Message: fmt.Sprintf("Unsupported agent run sandbox '%s'", sandbox),
		})
	}
	return errors
}

func validateNovoRunNodeStructure(node *Node, nodeID string) []ValidationError {
	var errors []ValidationError
	if strings.TrimSpace(node.Prompt) == "" && strings.TrimSpace(node.TaskID) == "" {
		errors = append(errors, ValidationError{
			Field:   "prompt",
			NodeID:  nodeID,
			Message: "Novo run node requires prompt text or task_id",
		})
	}
	if node.TimeoutSeconds <= 0 {
		errors = append(errors, ValidationError{
			Field:   "timeout_seconds",
			NodeID:  nodeID,
			Message: "Novo run node requires explicit timeout_seconds (no implicit default)",
		})
	}
	sandbox := strings.TrimSpace(node.Sandbox)
	if sandbox != "" && !IsSupportedNovomoSandbox(sandbox) {
		errors = append(errors, ValidationError{
			Field:   "sandbox",
			NodeID:  nodeID,
			Message: fmt.Sprintf("Unsupported novo run sandbox '%s'", sandbox),
		})
	}
	if node.GraceSeconds < 0 {
		errors = append(errors, ValidationError{
			Field:   "grace_seconds",
			NodeID:  nodeID,
			Message: "Novo run node grace_seconds must be non-negative",
		})
	}
	return errors
}

func validateResultNodeStructure(node *Node, nodeID string) []ValidationError {
	if node.AggregationMethod != "" {
		return nil
	}
	return []ValidationError{{
		Field:   "aggregation_method",
		NodeID:  nodeID,
		Message: "Result node requires explicit aggregation_method (no implicit default)",
	}}
}

func isSupportedAgentRunHarness(harness string) bool {
	switch strings.TrimSpace(harness) {
	case "claude-code", "codex":
		return true
	default:
		return false
	}
}

func validateRetryPolicy(policy *RetryPolicy) string {
	if policy == nil {
		return "retry_policy is required"
	}
	if policy.MaxAttempts < 1 {
		return "retry_policy.max_attempts must be >= 1"
	}
	if policy.BackoffMs < 0 {
		return "retry_policy.backoff_ms must be >= 0"
	}
	if policy.BackoffMultiply < 0 {
		return "retry_policy.backoff_multiply must be >= 0"
	}
	if policy.MaxBackoffMs < 0 {
		return "retry_policy.max_backoff_ms must be >= 0"
	}
	return ""
}
