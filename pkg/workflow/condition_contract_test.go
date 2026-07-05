package workflow

import "testing"

func TestConditionValidationMatchesEvaluation(t *testing.T) {
	v := NewValidator(nil)

	valid := []string{
		"score > 10",
		"name contains john",
		"result matches ^ok.*",
		"NOT output is_empty",
		"count >= 2 AND verdict not_empty",
		"left not_empty OR right not_empty",
	}
	invalid := []string{
		"score ??? 10",
		"value >",
		"text matches [",
		"broken",
	}

	ctx := map[string]interface{}{
		"score":   20,
		"name":    "john smith",
		"result":  "ok",
		"output":  "x",
		"count":   3,
		"verdict": "pass",
		"left":    "",
		"right":   "x",
		"value":   1,
		"text":    "abc",
	}

	for _, expr := range valid {
		if errs := v.validateConditionExpression(expr, "n1"); len(errs) > 0 {
			t.Fatalf("expected valid expression %q, got errors: %+v", expr, errs)
		}
		if _, err := EvaluateConditionExpression(expr, ctx); err != nil {
			t.Fatalf("expected evaluable expression %q, got error: %v", expr, err)
		}
	}

	for _, expr := range invalid {
		if errs := v.validateConditionExpression(expr, "n1"); len(errs) == 0 {
			t.Fatalf("expected invalid expression %q to fail validation", expr)
		}
		if _, err := EvaluateConditionExpression(expr, ctx); err == nil {
			t.Fatalf("expected invalid expression %q to fail evaluation", expr)
		}
	}
}

func TestExtractConditionVariablesHandlesCompoundExpressions(t *testing.T) {
	got := extractConditionVariables("NOT a is_empty AND b contains foo OR c >= 2")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("unexpected variable count: got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected variable at index %d: got=%q want=%q", i, got[i], want[i])
		}
	}
}
