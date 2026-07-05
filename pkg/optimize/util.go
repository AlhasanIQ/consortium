package optimize

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

func copyStringMap(src map[string]string) map[string]string {
	if src == nil {
		return map[string]string{}
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func encodeJSONValue(v interface{}) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func buildParentParamValues(parent *Organism, spec *OptimizeSpec) (map[string]string, error) {
	if parent == nil {
		return nil, fmt.Errorf("parent organism is required")
	}
	values := copyStringMap(parent.ParamValues)
	if len(values) == len(spec.Params) {
		return values, nil
	}

	var workflowObj map[string]interface{}
	if len(parent.WorkflowJSON) > 0 {
		if err := json.Unmarshal(parent.WorkflowJSON, &workflowObj); err != nil {
			return nil, fmt.Errorf("parse parent workflow JSON: %w", err)
		}
	}

	for _, declaration := range spec.Params {
		if _, ok := values[declaration.Path]; ok {
			continue
		}
		if workflowObj == nil {
			continue
		}
		raw, found, err := getPathValue(workflowObj, declaration.Path)
		if err != nil || !found {
			continue
		}
		encoded, err := encodeJSONValue(raw)
		if err != nil {
			continue
		}
		values[declaration.Path] = encoded
	}
	return values, nil
}

func newChildOrganism(parent *Organism, generation int, mutationType string, mutationLog string, paramValues map[string]string) *Organism {
	now := time.Now().UTC()
	parentIDs := []string{}
	optRunID := ""
	if parent != nil {
		parentIDs = append(parentIDs, parent.ID)
		optRunID = parent.OptRunID
	}
	return &Organism{
		ID:           uuid.NewString(),
		OptRunID:     optRunID,
		Generation:   generation,
		ParentIDs:    parentIDs,
		ParamValues:  paramValues,
		MutationType: mutationType,
		MutationLog:  strings.TrimSpace(mutationLog),
		CreatedAt:    now,
	}
}
