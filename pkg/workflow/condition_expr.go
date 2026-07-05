package workflow

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type conditionOperatorInfo struct {
	requiresValue bool
}

var supportedConditionOperators = map[string]conditionOperatorInfo{
	"contains":  {requiresValue: true},
	"equals":    {requiresValue: true},
	"not_empty": {requiresValue: false},
	"is_empty":  {requiresValue: false},
	"matches":   {requiresValue: true},
	">":         {requiresValue: true},
	"<":         {requiresValue: true},
	">=":        {requiresValue: true},
	"<=":        {requiresValue: true},
	"==":        {requiresValue: true},
	"is_number": {requiresValue: false},
	"is_string": {requiresValue: false},
}

// EvaluateConditionExpression evaluates a condition against runtime context values.
func EvaluateConditionExpression(condition string, context map[string]interface{}) (bool, error) {
	if condition == "" {
		return true, nil
	}
	condition = strings.TrimSpace(condition)

	if trimmed, ok := trimNotPrefix(condition); ok {
		result, err := EvaluateConditionExpression(trimmed, context)
		if err != nil {
			return false, err
		}
		return !result, nil
	}

	if idx := findBooleanOperatorIndex(condition, " AND "); idx != -1 {
		left, err := EvaluateConditionExpression(condition[:idx], context)
		if err != nil {
			return false, err
		}
		right, err := EvaluateConditionExpression(condition[idx+5:], context)
		if err != nil {
			return false, err
		}
		return left && right, nil
	}

	if idx := findBooleanOperatorIndex(condition, " OR "); idx != -1 {
		left, err := EvaluateConditionExpression(condition[:idx], context)
		if err != nil {
			return false, err
		}
		right, err := EvaluateConditionExpression(condition[idx+4:], context)
		if err != nil {
			return false, err
		}
		return left || right, nil
	}

	parts := strings.Fields(condition)
	if len(parts) < 2 {
		return false, fmt.Errorf("invalid condition format: %s", condition)
	}

	varName := parts[0]
	operator := parts[1]
	rawValue, hasValue := context[varName]
	value := ""
	if hasValue {
		value = fmt.Sprintf("%v", rawValue)
	}

	switch operator {
	case "contains":
		if len(parts) < 3 {
			return false, fmt.Errorf("contains operator requires a value")
		}
		searchStr := strings.Join(parts[2:], " ")
		return strings.Contains(strings.ToLower(value), strings.ToLower(searchStr)), nil
	case "equals":
		if len(parts) < 3 {
			return false, fmt.Errorf("equals operator requires a value")
		}
		return value == strings.Join(parts[2:], " "), nil
	case "not_empty":
		return value != "", nil
	case "is_empty":
		return value == "", nil
	case "matches":
		if len(parts) < 3 {
			return false, fmt.Errorf("matches operator requires a regex pattern")
		}
		re, err := regexp.Compile(strings.Join(parts[2:], " "))
		if err != nil {
			return false, fmt.Errorf("invalid regex pattern: %w", err)
		}
		return re.MatchString(value), nil
	case ">", "<", ">=", "<=", "==":
		if len(parts) < 3 {
			return false, fmt.Errorf("%s operator requires a value", operator)
		}
		return evaluateNumericOperands(value, operator, parts[2])
	case "is_number":
		_, err := strconv.ParseFloat(value, 64)
		return err == nil, nil
	case "is_string":
		return hasValue, nil
	default:
		return false, fmt.Errorf("unknown operator: %s", operator)
	}
}

// ValidateConditionExpression checks condition syntax/operator validity.
func ValidateConditionExpression(condition string) error {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return fmt.Errorf("condition is required")
	}

	if trimmed, ok := trimNotPrefix(condition); ok {
		return ValidateConditionExpression(trimmed)
	}

	if idx := findBooleanOperatorIndex(strings.ToUpper(condition), " AND "); idx != -1 {
		if err := ValidateConditionExpression(strings.TrimSpace(condition[:idx])); err != nil {
			return err
		}
		return ValidateConditionExpression(strings.TrimSpace(condition[idx+5:]))
	}
	if idx := findBooleanOperatorIndex(strings.ToUpper(condition), " OR "); idx != -1 {
		if err := ValidateConditionExpression(strings.TrimSpace(condition[:idx])); err != nil {
			return err
		}
		return ValidateConditionExpression(strings.TrimSpace(condition[idx+4:]))
	}

	parts := strings.Fields(condition)
	if len(parts) < 2 {
		return fmt.Errorf("invalid condition format: expected 'variable operator [value]'")
	}

	op := parts[1]
	info, ok := supportedConditionOperators[op]
	if !ok {
		return fmt.Errorf("unknown operator: %s", op)
	}
	if info.requiresValue && len(parts) < 3 {
		return fmt.Errorf("operator %q requires a comparison value", op)
	}
	if op == "matches" && len(parts) >= 3 {
		if _, err := regexp.Compile(strings.Join(parts[2:], " ")); err != nil {
			return fmt.Errorf("invalid regex pattern: %w", err)
		}
	}
	return nil
}

// ExtractConditionVariables returns unique variables referenced by a condition.
func ExtractConditionVariables(condition string) []string {
	seen := make(map[string]bool)
	var vars []string

	isReserved := func(s string) bool {
		u := strings.ToUpper(s)
		return u == "AND" || u == "OR" || u == "NOT"
	}

	parts := splitConditionByBoolean(condition)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if trimmed, ok := trimNotPrefix(part); ok {
			part = trimmed
		}
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}
		candidate := fields[0]
		if isReserved(candidate) || seen[candidate] {
			continue
		}
		seen[candidate] = true
		vars = append(vars, candidate)
	}
	return vars
}

func splitConditionByBoolean(condition string) []string {
	replaced := regexp.MustCompile(`(?i)\s+AND\s+`).ReplaceAllString(condition, "|SPLIT|")
	replaced = regexp.MustCompile(`(?i)\s+OR\s+`).ReplaceAllString(replaced, "|SPLIT|")
	return strings.Split(replaced, "|SPLIT|")
}

func trimNotPrefix(condition string) (string, bool) {
	trimmed := strings.TrimSpace(condition)
	upper := strings.ToUpper(trimmed)
	if !strings.HasPrefix(upper, "NOT ") {
		return trimmed, false
	}
	return strings.TrimSpace(trimmed[4:]), true
}

func findBooleanOperatorIndex(condition, op string) int {
	return strings.Index(strings.ToUpper(condition), strings.ToUpper(op))
}

func evaluateNumericOperands(leftStr, operator, rightStr string) (bool, error) {
	left, err := strconv.ParseFloat(leftStr, 64)
	if err != nil {
		return false, fmt.Errorf("left operand is not a number: %s", leftStr)
	}
	right, err := strconv.ParseFloat(rightStr, 64)
	if err != nil {
		return false, fmt.Errorf("right operand is not a number: %s", rightStr)
	}
	switch operator {
	case ">":
		return left > right, nil
	case "<":
		return left < right, nil
	case ">=":
		return left >= right, nil
	case "<=":
		return left <= right, nil
	case "==":
		return left == right, nil
	default:
		return false, fmt.Errorf("unknown numeric operator: %s", operator)
	}
}
