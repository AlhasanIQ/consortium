package optimize

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
)

type parsedPath struct {
	NodeID string
	Fields []string
}

// MaterializeWorkflow applies param values to a base seed JSON and returns a full variant JSON.
func MaterializeWorkflow(baseWorkflowJSON json.RawMessage, values map[string]string, spec *OptimizeSpec) (json.RawMessage, error) {
	if len(baseWorkflowJSON) == 0 {
		return nil, fmt.Errorf("base workflow JSON is required")
	}
	if spec == nil {
		return nil, fmt.Errorf("optimize spec is required")
	}

	var base map[string]interface{}
	if err := json.Unmarshal(baseWorkflowJSON, &base); err != nil {
		return nil, fmt.Errorf("parse base workflow JSON: %w", err)
	}

	// Deep copy by re-marshal to avoid mutating caller state.
	copyBytes, err := json.Marshal(base)
	if err != nil {
		return nil, fmt.Errorf("clone base workflow: %w", err)
	}
	var materialized map[string]interface{}
	if err := json.Unmarshal(copyBytes, &materialized); err != nil {
		return nil, fmt.Errorf("decode cloned workflow: %w", err)
	}

	declByPath := make(map[string]ParamDeclaration, len(spec.Params))
	for _, declaration := range spec.Params {
		declByPath[declaration.Path] = declaration
	}

	for path, encoded := range values {
		declaration, ok := declByPath[path]
		if !ok {
			return nil, fmt.Errorf("param path %q is not declared in optimize spec", path)
		}

		val, err := decodeParamValue(encoded)
		if err != nil {
			return nil, fmt.Errorf("decode value for %s: %w", path, err)
		}
		if err := validateParamValue(declaration, val); err != nil {
			return nil, fmt.Errorf("invalid value for %s: %w", path, err)
		}

		if err := setPathValue(materialized, path, val); err != nil {
			return nil, fmt.Errorf("apply %s: %w", path, err)
		}
	}

	for _, lockedPath := range spec.Locked {
		baseVal, baseFound, err := getPathValue(base, lockedPath)
		if err != nil {
			return nil, fmt.Errorf("read locked path %s in base: %w", lockedPath, err)
		}
		matVal, matFound, err := getPathValue(materialized, lockedPath)
		if err != nil {
			return nil, fmt.Errorf("read locked path %s in materialized: %w", lockedPath, err)
		}
		if baseFound != matFound || !reflect.DeepEqual(baseVal, matVal) {
			return nil, fmt.Errorf("locked path %s changed", lockedPath)
		}
	}

	out, err := json.Marshal(materialized)
	if err != nil {
		return nil, fmt.Errorf("marshal materialized workflow: %w", err)
	}
	return out, nil
}

func decodeParamValue(encoded string) (interface{}, error) {
	var val interface{}
	if err := json.Unmarshal([]byte(encoded), &val); err != nil {
		return nil, err
	}
	return val, nil
}

func validateParamValue(declaration ParamDeclaration, value interface{}) error {
	switch declaration.Type {
	case ParamTypeModel, ParamTypeEnum:
		str, ok := value.(string)
		if !ok {
			return fmt.Errorf("expected string")
		}
		for _, candidate := range declaration.Candidates {
			if candidate == str {
				return nil
			}
		}
		return fmt.Errorf("%q is not in candidates", str)
	case ParamTypePrompt:
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string")
		}
		return nil
	case ParamTypeFloat:
		n, ok := asFloat64(value)
		if !ok {
			return fmt.Errorf("expected numeric value")
		}
		if declaration.Range == nil {
			return fmt.Errorf("range is required")
		}
		if n < declaration.Range.Min || n > declaration.Range.Max {
			return fmt.Errorf("%.6f out of range [%.6f, %.6f]", n, declaration.Range.Min, declaration.Range.Max)
		}
		if declaration.Range.Step > 0 {
			offset := (n - declaration.Range.Min) / declaration.Range.Step
			if math.Abs(offset-math.Round(offset)) > 1e-9 {
				return fmt.Errorf("%.6f does not align to step %.6f from min %.6f", n, declaration.Range.Step, declaration.Range.Min)
			}
		}
		return nil
	case ParamTypeInt:
		n, ok := asFloat64(value)
		if !ok {
			return fmt.Errorf("expected numeric value")
		}
		if math.Abs(n-math.Round(n)) > 1e-9 {
			return fmt.Errorf("expected integer value, got %.6f", n)
		}
		if declaration.Range == nil {
			return fmt.Errorf("range is required")
		}
		if n < declaration.Range.Min || n > declaration.Range.Max {
			return fmt.Errorf("%.6f out of range [%.6f, %.6f]", n, declaration.Range.Min, declaration.Range.Max)
		}
		if declaration.Range.Step > 0 {
			offset := (n - declaration.Range.Min) / declaration.Range.Step
			if math.Abs(offset-math.Round(offset)) > 1e-9 {
				return fmt.Errorf("%.6f does not align to step %.6f from min %.6f", n, declaration.Range.Step, declaration.Range.Min)
			}
		}
		return nil
	case ParamTypeTopology:
		return fmt.Errorf("topology mutations are not implemented in v1")
	default:
		return fmt.Errorf("unsupported param type %q", declaration.Type)
	}
}

func asFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		parsed, err := n.Float64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func parsePath(path string) (*parsedPath, error) {
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "nodes[") {
		return nil, fmt.Errorf("path must start with nodes[<id>], got %q", path)
	}
	closeIdx := strings.Index(path, "]")
	if closeIdx < 0 {
		return nil, fmt.Errorf("path missing closing ]: %q", path)
	}
	nodeID := path[len("nodes["):closeIdx]
	if strings.TrimSpace(nodeID) == "" {
		return nil, fmt.Errorf("path node ID is empty")
	}
	rest := strings.TrimPrefix(path[closeIdx+1:], ".")
	if rest == "" {
		return nil, fmt.Errorf("path field segment is empty")
	}
	fields := strings.Split(rest, ".")
	for _, field := range fields {
		if strings.TrimSpace(field) == "" {
			return nil, fmt.Errorf("path contains empty field segment: %q", path)
		}
	}
	return &parsedPath{NodeID: nodeID, Fields: fields}, nil
}

func setPathValue(workflow map[string]interface{}, path string, value interface{}) error {
	parsed, err := parsePath(path)
	if err != nil {
		return err
	}

	node, err := findNodeByID(workflow, parsed.NodeID)
	if err != nil {
		return err
	}

	if len(parsed.Fields) < 1 {
		return fmt.Errorf("path has no fields")
	}

	current := node
	for i := 0; i < len(parsed.Fields)-1; i++ {
		field := parsed.Fields[i]
		next, ok := current[field]
		if !ok {
			return fmt.Errorf("field %q not found", strings.Join(parsed.Fields[:i+1], "."))
		}
		nextMap, ok := next.(map[string]interface{})
		if !ok {
			return fmt.Errorf("field %q is not an object", strings.Join(parsed.Fields[:i+1], "."))
		}
		current = nextMap
	}

	current[parsed.Fields[len(parsed.Fields)-1]] = value
	return nil
}

func getPathValue(workflow map[string]interface{}, path string) (interface{}, bool, error) {
	parsed, err := parsePath(path)
	if err != nil {
		return nil, false, err
	}
	current, err := findNodeByID(workflow, parsed.NodeID)
	if err != nil {
		return nil, false, err
	}
	for _, field := range parsed.Fields {
		next, ok := current[field]
		if !ok {
			return nil, false, nil
		}
		if nextMap, ok := next.(map[string]interface{}); ok {
			current = nextMap
			continue
		}
		if field != parsed.Fields[len(parsed.Fields)-1] {
			return nil, false, fmt.Errorf("field %q is not an object", field)
		}
		return next, true, nil
	}
	return current, true, nil
}

func findNodeByID(workflow map[string]interface{}, nodeID string) (map[string]interface{}, error) {
	nodesRaw, ok := workflow["nodes"]
	if !ok {
		return nil, fmt.Errorf("workflow is missing nodes array")
	}
	nodes, ok := nodesRaw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("workflow nodes is not an array")
	}
	for _, raw := range nodes {
		nodeMap, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if id, _ := nodeMap["id"].(string); id == nodeID {
			return nodeMap, nil
		}
	}
	return nil, fmt.Errorf("node %q not found", nodeID)
}

// GetWorkflowPathValue resolves a declared optimize path from workflow JSON.
func GetWorkflowPathValue(workflowJSON json.RawMessage, path string) (interface{}, bool, error) {
	var workflow map[string]interface{}
	if err := json.Unmarshal(workflowJSON, &workflow); err != nil {
		return nil, false, fmt.Errorf("parse workflow JSON: %w", err)
	}
	return getPathValue(workflow, path)
}
