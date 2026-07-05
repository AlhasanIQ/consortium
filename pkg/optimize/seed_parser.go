package optimize

import (
	"encoding/json"
	"fmt"
)

// ParsedSeed captures seed workflow JSON with optimize annotations extracted.
type ParsedSeed struct {
	OptimizeSpec  *OptimizeSpec
	WorkflowJSON  json.RawMessage
	WorkflowID    string
	WorkflowName  string
	WorkflowDescr string
}

// ParseSeedOptimizeSpec extracts and validates top-level optimize declaration.
func ParseSeedOptimizeSpec(seedJSON []byte) (*ParsedSeed, error) {
	if len(seedJSON) == 0 {
		return nil, fmt.Errorf("seed JSON is required")
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(seedJSON, &raw); err != nil {
		return nil, fmt.Errorf("parse seed JSON: %w", err)
	}

	workflowID, _ := raw["id"].(string)
	workflowName, _ := raw["name"].(string)
	workflowDescr, _ := raw["description"].(string)
	if workflowID == "" {
		return nil, fmt.Errorf("seed workflow id is required")
	}

	optimizeRaw, ok := raw["optimize"]
	if !ok {
		return nil, fmt.Errorf("seed %s does not declare optimize spec", workflowID)
	}
	optimizeBytes, err := json.Marshal(optimizeRaw)
	if err != nil {
		return nil, fmt.Errorf("marshal optimize spec: %w", err)
	}

	var spec OptimizeSpec
	if err := json.Unmarshal(optimizeBytes, &spec); err != nil {
		return nil, fmt.Errorf("parse optimize spec: %w", err)
	}
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("invalid optimize spec: %w", err)
	}

	delete(raw, "optimize")
	workflowBytes, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal workflow without optimize key: %w", err)
	}

	var workflowObj map[string]interface{}
	if err := json.Unmarshal(workflowBytes, &workflowObj); err != nil {
		return nil, fmt.Errorf("parse stripped workflow: %w", err)
	}

	for _, declaration := range spec.Params {
		if _, found, err := getPathValue(workflowObj, declaration.Path); err != nil {
			return nil, fmt.Errorf("param path %s: %w", declaration.Path, err)
		} else if !found {
			return nil, fmt.Errorf("param path %s not found in workflow", declaration.Path)
		}
	}
	for _, lockedPath := range spec.Locked {
		if _, found, err := getPathValue(workflowObj, lockedPath); err != nil {
			return nil, fmt.Errorf("locked path %s: %w", lockedPath, err)
		} else if !found {
			return nil, fmt.Errorf("locked path %s not found in workflow", lockedPath)
		}
	}

	return &ParsedSeed{
		OptimizeSpec:  &spec,
		WorkflowJSON:  workflowBytes,
		WorkflowID:    workflowID,
		WorkflowName:  workflowName,
		WorkflowDescr: workflowDescr,
	}, nil
}
