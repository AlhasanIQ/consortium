package workflow

import (
	"testing"
)

func TestEvaluateConditionExpression_EmptyCondition(t *testing.T) {
	result, err := EvaluateConditionExpression("", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("empty condition should return true")
	}
}

func TestEvaluateConditionExpression_Contains(t *testing.T) {
	ctx := map[string]interface{}{"output": "Hello World"}

	tests := []struct {
		name      string
		condition string
		want      bool
	}{
		{"match", "output contains hello", true},
		{"case insensitive", "output contains HELLO", true},
		{"no match", "output contains goodbye", false},
		{"multi-word value", "output contains hello world", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EvaluateConditionExpression(tc.condition, ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestEvaluateConditionExpression_Equals(t *testing.T) {
	ctx := map[string]interface{}{"status": "success"}

	got, err := EvaluateConditionExpression("status equals success", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("expected true for matching equals")
	}

	got, err = EvaluateConditionExpression("status equals failure", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("expected false for non-matching equals")
	}
}

func TestEvaluateConditionExpression_NotEmpty(t *testing.T) {
	ctx := map[string]interface{}{"x": "value", "y": ""}

	got, err := EvaluateConditionExpression("x not_empty", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("expected true for non-empty variable")
	}

	got, err = EvaluateConditionExpression("y not_empty", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("expected false for empty variable")
	}
}

func TestEvaluateConditionExpression_IsEmpty(t *testing.T) {
	ctx := map[string]interface{}{"x": "value"}

	got, err := EvaluateConditionExpression("x is_empty", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("expected false for non-empty variable")
	}

	got, err = EvaluateConditionExpression("missing is_empty", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("expected true for missing variable")
	}
}

func TestEvaluateConditionExpression_Matches(t *testing.T) {
	ctx := map[string]interface{}{"val": "abc123"}

	got, err := EvaluateConditionExpression(`val matches ^[a-z]+\d+$`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("expected regex to match")
	}

	got, err = EvaluateConditionExpression(`val matches ^[0-9]+$`, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("expected regex not to match")
	}
}

func TestEvaluateConditionExpression_InvalidRegex(t *testing.T) {
	ctx := map[string]interface{}{"val": "test"}
	_, err := EvaluateConditionExpression(`val matches [invalid`, ctx)
	if err == nil {
		t.Error("expected error for invalid regex")
	}
}

func TestEvaluateConditionExpression_NumericOperators(t *testing.T) {
	ctx := map[string]interface{}{"score": "75"}

	tests := []struct {
		condition string
		want      bool
	}{
		{"score > 50", true},
		{"score > 100", false},
		{"score < 100", true},
		{"score < 50", false},
		{"score >= 75", true},
		{"score >= 76", false},
		{"score <= 75", true},
		{"score <= 74", false},
		{"score == 75", true},
		{"score == 76", false},
	}
	for _, tc := range tests {
		t.Run(tc.condition, func(t *testing.T) {
			got, err := EvaluateConditionExpression(tc.condition, ctx)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestEvaluateConditionExpression_NumericNonNumber(t *testing.T) {
	ctx := map[string]interface{}{"val": "abc"}
	_, err := EvaluateConditionExpression("val > 10", ctx)
	if err == nil {
		t.Error("expected error for non-numeric left operand")
	}
}

func TestEvaluateConditionExpression_IsNumber(t *testing.T) {
	ctx := map[string]interface{}{"num": "42.5", "str": "hello"}

	got, err := EvaluateConditionExpression("num is_number", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("expected true for numeric string")
	}

	got, err = EvaluateConditionExpression("str is_number", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("expected false for non-numeric string")
	}
}

func TestEvaluateConditionExpression_IsString(t *testing.T) {
	ctx := map[string]interface{}{"x": "val"}

	got, err := EvaluateConditionExpression("x is_string", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("expected true for existing variable")
	}

	got, err = EvaluateConditionExpression("missing is_string", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("expected false for missing variable")
	}
}

func TestEvaluateConditionExpression_NOT(t *testing.T) {
	ctx := map[string]interface{}{"x": "value"}

	got, err := EvaluateConditionExpression("NOT x is_empty", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("NOT x is_empty should be true when x has value")
	}
}

func TestEvaluateConditionExpression_AND(t *testing.T) {
	ctx := map[string]interface{}{"a": "1", "b": "2"}

	got, err := EvaluateConditionExpression("a not_empty AND b not_empty", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("expected true: both non-empty")
	}

	got, err = EvaluateConditionExpression("a not_empty AND c not_empty", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("expected false: c is missing")
	}
}

func TestEvaluateConditionExpression_OR(t *testing.T) {
	ctx := map[string]interface{}{"a": "1"}

	got, err := EvaluateConditionExpression("a not_empty OR b not_empty", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got {
		t.Error("expected true: a is non-empty")
	}

	got, err = EvaluateConditionExpression("c not_empty OR d not_empty", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got {
		t.Error("expected false: both missing")
	}
}

func TestEvaluateConditionExpression_UnknownOperator(t *testing.T) {
	ctx := map[string]interface{}{"x": "val"}
	_, err := EvaluateConditionExpression("x foobar val", ctx)
	if err == nil {
		t.Error("expected error for unknown operator")
	}
}

func TestEvaluateConditionExpression_InvalidFormat(t *testing.T) {
	_, err := EvaluateConditionExpression("onlyonepart", nil)
	if err == nil {
		t.Error("expected error for single-part condition")
	}
}

func TestValidateConditionExpression_Valid(t *testing.T) {
	valids := []string{
		"x contains hello",
		"x equals foo",
		"x not_empty",
		"x is_empty",
		"x matches ^[a-z]+$",
		"x > 5",
		"x < 10",
		"x >= 5",
		"x <= 10",
		"x == 5",
		"x is_number",
		"x is_string",
		"x not_empty AND y not_empty",
		"NOT x is_empty",
	}
	for _, cond := range valids {
		t.Run(cond, func(t *testing.T) {
			if err := ValidateConditionExpression(cond); err != nil {
				t.Errorf("expected valid, got error: %v", err)
			}
		})
	}
}

func TestValidateConditionExpression_Invalid(t *testing.T) {
	invalids := []string{
		"",
		"x",
		"x unknownop val",
		`x matches [invalid`,
		"x contains",
	}
	for _, cond := range invalids {
		t.Run(cond, func(t *testing.T) {
			if err := ValidateConditionExpression(cond); err == nil {
				t.Error("expected error for invalid condition")
			}
		})
	}
}

func TestExtractConditionVariables(t *testing.T) {
	tests := []struct {
		condition string
		wantVars  []string
	}{
		{"x contains hello", []string{"x"}},
		{"x not_empty AND y not_empty", []string{"x", "y"}},
		{"a > 5 OR b < 10", []string{"a", "b"}},
		{"NOT x is_empty", []string{"x"}},
	}
	for _, tc := range tests {
		t.Run(tc.condition, func(t *testing.T) {
			got := ExtractConditionVariables(tc.condition)
			if len(got) != len(tc.wantVars) {
				t.Fatalf("expected %d vars, got %d: %v", len(tc.wantVars), len(got), got)
			}
			for i, v := range tc.wantVars {
				if got[i] != v {
					t.Errorf("var[%d]: expected %q, got %q", i, v, got[i])
				}
			}
		})
	}
}
