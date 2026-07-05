package workflow

import (
	"fmt"
	"strings"

	"github.com/alhasaniq/consortium/pkg/providers"
)

// ValidationError represents a single validation error
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	NodeID  string `json:"node_id,omitempty"`
}

// ValidationResult holds the result of workflow validation
type ValidationResult struct {
	Valid  bool              `json:"valid"`
	Errors []ValidationError `json:"errors,omitempty"`
}

// Validator validates workflows before execution
type Validator struct {
	registry *providers.Registry
}

// NewValidator creates a new workflow validator
func NewValidator(registry *providers.Registry) *Validator {
	return &Validator{
		registry: registry,
	}
}

// Validate performs comprehensive workflow validation
func (v *Validator) Validate(wf *Workflow) *ValidationResult {
	result := &ValidationResult{
		Valid:  true,
		Errors: make([]ValidationError, 0),
	}

	// 1. Check for cycles in the workflow graph
	if cycleErrors := v.detectCycles(wf); len(cycleErrors) > 0 {
		result.Errors = append(result.Errors, cycleErrors...)
		result.Valid = false
	}

	// 2. Validate variable references
	if varErrors := v.validateVariables(wf); len(varErrors) > 0 {
		result.Errors = append(result.Errors, varErrors...)
		result.Valid = false
	}

	// 3. Validate model existence
	if v.registry != nil {
		if modelErrors := v.validateModels(wf); len(modelErrors) > 0 {
			result.Errors = append(result.Errors, modelErrors...)
			result.Valid = false
		}
	}

	// 4. Validate condition syntax
	if condErrors := v.validateConditions(wf); len(condErrors) > 0 {
		result.Errors = append(result.Errors, condErrors...)
		result.Valid = false
	}

	// 5. Validate basic structure
	if structErrors := v.validateStructure(wf); len(structErrors) > 0 {
		result.Errors = append(result.Errors, structErrors...)
		result.Valid = false
	}

	// 6. Validate cost limits
	if limitErrors := v.validateLimits(wf); len(limitErrors) > 0 {
		result.Errors = append(result.Errors, limitErrors...)
		result.Valid = false
	}

	// 7. Validate aggregation config
	if aggErrors := v.validateAggregationConfigs(wf); len(aggErrors) > 0 {
		result.Errors = append(result.Errors, aggErrors...)
		result.Valid = false
	}

	// 8. Validate result node input_ids reference existing nodes
	if inputErrors := v.validateResultInputs(wf); len(inputErrors) > 0 {
		result.Errors = append(result.Errors, inputErrors...)
		result.Valid = false
	}

	// 9. Validate Novomo handoff references.
	if handoffErrors := v.validateNovomoHandoffs(wf); len(handoffErrors) > 0 {
		result.Errors = append(result.Errors, handoffErrors...)
		result.Valid = false
	}

	// 10. Validate all nodes are reachable from root nodes
	if reachErrors := v.validateReachability(wf); len(reachErrors) > 0 {
		result.Errors = append(result.Errors, reachErrors...)
		// Unreachable nodes are warnings — don't set Valid=false
	}

	return result
}

// validateVariables checks that all {{variable}} references are valid
func (v *Validator) validateVariables(wf *Workflow) []ValidationError {
	var errors []ValidationError
	// Collect available variables (from context and previous nodes)
	availableVars := make(map[string]bool)
	if wf.Context != nil {
		for k := range wf.Context {
			availableVars[k] = true
		}
	}

	// For each node, check variable references
	for i, node := range wf.Nodes {
		nodeID := node.ID
		if nodeID == "" {
			nodeID = fmt.Sprintf("node_%d", i)
		}

		errors = append(errors, validateSingleNodeVariables(node, nodeID, availableVars)...)

		// Add this node's ID to available variables
		if nodeID != "" {
			availableVars[nodeID] = true
		}
		branchVars := cloneAvailableVars(availableVars)
		errors = append(errors, validateBranchVariables(nodeID, "true", node.TrueBranch, branchVars)...)
		errors = append(errors, validateBranchVariables(nodeID, "false", node.FalseBranch, branchVars)...)

	}

	return errors
}

func validateBranchVariables(parentID, branch string, node *Node, availableVars map[string]bool) []ValidationError {
	if node == nil {
		return nil
	}
	nodeID := branchNodeID(parentID, branch, node)
	var errors []ValidationError
	errors = append(errors, validateSingleNodeVariables(node, nodeID, availableVars)...)

	branchVars := cloneAvailableVars(availableVars)
	if nodeID != "" {
		branchVars[nodeID] = true
	}
	errors = append(errors, validateBranchVariables(nodeID, "true", node.TrueBranch, branchVars)...)
	errors = append(errors, validateBranchVariables(nodeID, "false", node.FalseBranch, branchVars)...)
	return errors
}

func validateSingleNodeVariables(node *Node, nodeID string, availableVars map[string]bool) []ValidationError {
	if node == nil {
		return nil
	}
	var errors []ValidationError

	if (node.Type == NodeTypePrompt || node.Type == NodeTypeContractExtract || node.Type == NodeTypeAgentRun || node.Type == NodeTypeNovoRun) && node.Prompt != "" {
		matches := varRegex.FindAllStringSubmatch(node.Prompt, -1)
		for _, match := range matches {
			varName := strings.TrimSpace(match[1])
			if !availableVars[varName] {
				errors = append(errors, ValidationError{
					Field:   "prompt",
					NodeID:  nodeID,
					Message: fmt.Sprintf("Variable '{{%s}}' not found in context or previous nodes", varName),
				})
			}
		}
	}
	if node.Type == NodeTypeContractExtract {
		sourceVar, _ := node.Metadata["source_variable"].(string)
		sourceVar = strings.TrimSpace(sourceVar)
		if sourceVar != "" && !availableVars[sourceVar] {
			errors = append(errors, ValidationError{
				Field:   "source_variable",
				NodeID:  nodeID,
				Message: fmt.Sprintf("Source variable '%s' not found in context or previous nodes", sourceVar),
			})
		}
	}

	if node.Type == NodeTypeConditional && node.Condition != "" {
		varNames := extractConditionVariables(node.Condition)
		for _, varName := range varNames {
			if !availableVars[varName] {
				errors = append(errors, ValidationError{
					Field:   "condition",
					NodeID:  nodeID,
					Message: fmt.Sprintf("Condition variable '%s' not found in context or previous nodes", varName),
				})
			}
		}
	}

	if node.Type == NodeTypeChildWorkflow && len(node.ChildInputTemplate) > 0 {
		for _, template := range node.ChildInputTemplate {
			matches := varRegex.FindAllStringSubmatch(template, -1)
			for _, match := range matches {
				varName := strings.TrimSpace(match[1])
				if !availableVars[varName] {
					errors = append(errors, ValidationError{
						Field:   "child_input_template",
						NodeID:  nodeID,
						Message: fmt.Sprintf("Variable '{{%s}}' not found in context or previous nodes", varName),
					})
				}
			}
		}
	}
	return errors
}

func cloneAvailableVars(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func branchNodeID(parentID, branch string, node *Node) string {
	if node != nil && strings.TrimSpace(node.ID) != "" {
		return node.ID
	}
	return parentID + "_" + branch
}

// extractConditionVariables extracts all variable names from a condition expression.
// It handles compound conditions (AND, OR, NOT) and returns unique variable names.
func extractConditionVariables(condition string) []string {
	return ExtractConditionVariables(condition)
}

func (v *Validator) validateAggregationConfigs(wf *Workflow) []ValidationError {
	var errors []ValidationError
	nodesByID := make(map[string]*Node, len(wf.Nodes))
	for _, n := range wf.Nodes {
		if n.ID != "" {
			nodesByID[n.ID] = n
		}
	}

	for i, node := range wf.Nodes {
		if node.Type != NodeTypeResult {
			continue
		}

		nodeID := node.ID
		if nodeID == "" {
			nodeID = fmt.Sprintf("node_%d", i)
		}

		method := node.AggregationMethod
		if method == "" {
			errors = append(errors, ValidationError{
				Field:   "aggregation_method",
				NodeID:  nodeID,
				Message: "Result node requires an explicit aggregation_method (no implicit default)",
			})
			continue
		}
		if err := ValidateAggregationConfig(method, node.AggregationConfig); err != nil {
			errors = append(errors, ValidationError{
				Field:   "aggregation_config",
				NodeID:  nodeID,
				Message: err.Error(),
			})
		}
		if method == AggMethodPeerMatrix {
			errors = append(errors, validatePeerMatrixInputModels(nodeID, node.DisplayMeta().InputIDs, nodesByID)...)
		}
	}

	return errors
}

func validatePeerMatrixInputModels(resultNodeID string, inputIDs []string, nodesByID map[string]*Node) []ValidationError {
	var errors []ValidationError
	for _, inputID := range inputIDs {
		upstream, ok := nodesByID[inputID]
		if !ok {
			// Missing node IDs are handled by validateResultInputs.
			continue
		}
		if strings.TrimSpace(upstream.Model) == "" {
			errors = append(errors, ValidationError{
				Field:   "aggregation_config",
				NodeID:  resultNodeID,
				Message: fmt.Sprintf("peer_matrix input node '%s' must set model for per-agent evaluator routing", inputID),
			})
		}
	}
	return errors
}

// validateModels checks that all referenced models exist in the registry
func (v *Validator) validateModels(wf *Workflow) []ValidationError {
	var errors []ValidationError

	for i, node := range wf.Nodes {
		nodeID := node.ID
		if nodeID == "" {
			nodeID = fmt.Sprintf("node_%d", i)
		}
		errors = append(errors, v.validateNodeModelRecursive(node, nodeID)...)
	}

	return errors
}

func (v *Validator) validateNodeModelRecursive(node *Node, nodeID string) []ValidationError {
	if node == nil {
		return nil
	}
	var errors []ValidationError
	if (node.Type == NodeTypePrompt || node.Type == NodeTypeContractExtract) && node.Model != "" {
		if _, err := v.registry.GetModel(node.Model); err != nil {
			errors = append(errors, ValidationError{
				Field:   "model",
				NodeID:  nodeID,
				Message: fmt.Sprintf("Model '%s' not found in registry", node.Model),
			})
		}
	}
	errors = append(errors, v.validateNodeModelRecursive(node.TrueBranch, branchNodeID(nodeID, "true", node.TrueBranch))...)
	errors = append(errors, v.validateNodeModelRecursive(node.FalseBranch, branchNodeID(nodeID, "false", node.FalseBranch))...)
	return errors
}

// validateConditions checks that all conditional expressions are valid
func (v *Validator) validateConditions(wf *Workflow) []ValidationError {
	var errors []ValidationError

	for i, node := range wf.Nodes {
		nodeID := node.ID
		if nodeID == "" {
			nodeID = fmt.Sprintf("node_%d", i)
		}
		errors = append(errors, v.validateNodeConditionRecursive(node, nodeID)...)
	}

	return errors
}

func (v *Validator) validateNodeConditionRecursive(node *Node, nodeID string) []ValidationError {
	if node == nil {
		return nil
	}
	var errors []ValidationError
	if node.Type == NodeTypeConditional {
		if node.Condition == "" {
			errors = append(errors, ValidationError{
				Field:   "condition",
				NodeID:  nodeID,
				Message: "Conditional node requires a condition expression",
			})
		} else if condErrors := v.validateConditionExpression(node.Condition, nodeID); len(condErrors) > 0 {
			errors = append(errors, condErrors...)
		}

		if node.TrueBranch == nil && node.FalseBranch == nil {
			errors = append(errors, ValidationError{
				Field:   "condition",
				NodeID:  nodeID,
				Message: "Conditional node must have at least one branch (true_branch or false_branch)",
			})
		}
	}
	errors = append(errors, v.validateNodeConditionRecursive(node.TrueBranch, branchNodeID(nodeID, "true", node.TrueBranch))...)
	errors = append(errors, v.validateNodeConditionRecursive(node.FalseBranch, branchNodeID(nodeID, "false", node.FalseBranch))...)
	return errors
}

// validateConditionExpression validates a single or compound condition expression
func (v *Validator) validateConditionExpression(condition string, nodeID string) []ValidationError {
	if err := ValidateConditionExpression(strings.TrimSpace(condition)); err != nil {
		return []ValidationError{{
			Field:   "condition",
			NodeID:  nodeID,
			Message: err.Error(),
		}}
	}
	return nil
}

// validateStructure checks basic workflow structure requirements
func (v *Validator) validateStructure(wf *Workflow) []ValidationError {
	var errors []ValidationError

	// Check workflow has nodes
	if len(wf.Nodes) == 0 {
		errors = append(errors, ValidationError{
			Field:   "nodes",
			Message: "Workflow must have at least one node",
		})
		return errors
	}

	// Check for duplicate node IDs and reserved separator
	nodeIDs := make(map[string]bool)
	for i, node := range wf.Nodes {
		if node.ID != "" {
			if strings.Contains(node.ID, "__") {
				errors = append(errors, ValidationError{
					Field:   "nodes",
					NodeID:  node.ID,
					Message: fmt.Sprintf("Node ID '%s' must not contain '__' (reserved separator for context metadata)", node.ID),
				})
			}
			if nodeIDs[node.ID] {
				errors = append(errors, ValidationError{
					Field:   "nodes",
					NodeID:  node.ID,
					Message: fmt.Sprintf("Duplicate node ID: '%s'", node.ID),
				})
			}
			nodeIDs[node.ID] = true
		}

		// Validate node-specific requirements
		nodeID := node.ID
		if nodeID == "" {
			nodeID = fmt.Sprintf("node_%d", i)
		}

		errors = append(errors, validateNodeStructure(wf, node, nodeID)...)
		errors = append(errors, validateBranchStructure(wf, nodeID, "true", node.TrueBranch)...)
		errors = append(errors, validateBranchStructure(wf, nodeID, "false", node.FalseBranch)...)
	}

	// Validate edges reference existing nodes and check for self-edges
	if len(wf.Edges) > 0 {
		for _, edge := range wf.Edges {
			if edge.Source == edge.Target {
				errors = append(errors, ValidationError{
					Field:   "edges",
					Message: fmt.Sprintf("Self-edge detected: node '%s' connects to itself", edge.Source),
				})
			}
			sourceExists := false
			targetExists := false
			for _, node := range wf.Nodes {
				if node.ID == edge.Source {
					sourceExists = true
				}
				if node.ID == edge.Target {
					targetExists = true
				}
			}
			if !sourceExists {
				errors = append(errors, ValidationError{
					Field:   "edges",
					Message: fmt.Sprintf("Edge source '%s' does not exist in workflow nodes", edge.Source),
				})
			}
			if !targetExists {
				errors = append(errors, ValidationError{
					Field:   "edges",
					Message: fmt.Sprintf("Edge target '%s' does not exist in workflow nodes", edge.Target),
				})
			}
		}
	}

	return errors
}

func validateBranchStructure(wf *Workflow, parentID, branch string, node *Node) []ValidationError {
	if node == nil {
		return nil
	}
	nodeID := branchNodeID(parentID, branch, node)
	var errors []ValidationError
	errors = append(errors, validateNodeStructure(wf, node, nodeID)...)
	errors = append(errors, validateBranchStructure(wf, nodeID, "true", node.TrueBranch)...)
	errors = append(errors, validateBranchStructure(wf, nodeID, "false", node.FalseBranch)...)
	return errors
}

// validateLimits checks that cost limit values are valid
func (v *Validator) validateLimits(wf *Workflow) []ValidationError {
	var errors []ValidationError

	if wf.Limits == nil {
		return errors // No limits specified, valid
	}

	if wf.Limits.MaxCostUSD < 0 {
		errors = append(errors, ValidationError{
			Field:   "limits.max_cost_usd",
			Message: "MaxCostUSD cannot be negative",
		})
	}

	if wf.Limits.MaxTokens < 0 {
		errors = append(errors, ValidationError{
			Field:   "limits.max_tokens",
			Message: "MaxTokens cannot be negative",
		})
	}

	if wf.Limits.MaxInputTokens < 0 {
		errors = append(errors, ValidationError{
			Field:   "limits.max_input_tokens",
			Message: "MaxInputTokens cannot be negative",
		})
	}

	if wf.Limits.MaxOutputTokens < 0 {
		errors = append(errors, ValidationError{
			Field:   "limits.max_output_tokens",
			Message: "MaxOutputTokens cannot be negative",
		})
	}

	// Warn if limits seem too low (may cause immediate failures)
	if wf.Limits.MaxCostUSD > 0 && wf.Limits.MaxCostUSD < 0.0001 {
		errors = append(errors, ValidationError{
			Field:   "limits.max_cost_usd",
			Message: "MaxCostUSD is very low (< $0.0001), workflow may fail immediately",
		})
	}

	if wf.Limits.MaxTokens > 0 && wf.Limits.MaxTokens < 100 {
		errors = append(errors, ValidationError{
			Field:   "limits.max_tokens",
			Message: "MaxTokens is very low (< 100), workflow may fail immediately",
		})
	}

	return errors
}

// validateResultInputs checks that result nodes' input_ids reference existing node IDs.
func (v *Validator) validateResultInputs(wf *Workflow) []ValidationError {
	var errors []ValidationError

	// Build set of all node IDs
	nodeIDs := make(map[string]bool)
	for _, node := range wf.Nodes {
		if node.ID != "" {
			nodeIDs[node.ID] = true
		}
	}

	for i, node := range wf.Nodes {
		if node.Type != NodeTypeResult {
			continue
		}
		nodeID := node.ID
		if nodeID == "" {
			nodeID = fmt.Sprintf("node_%d", i)
		}

		meta := node.DisplayMeta()
		for _, inputID := range meta.InputIDs {
			if !nodeIDs[inputID] {
				errors = append(errors, ValidationError{
					Field:   "input_ids",
					NodeID:  nodeID,
					Message: fmt.Sprintf("Result node references non-existent input node '%s'", inputID),
				})
			}
		}
	}

	return errors
}
