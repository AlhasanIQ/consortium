package conctl

import (
	"bytes"
	"strings"
	"testing"
)

func TestWorkflowGetTableRendersAPISuccessRateAsPercent(t *testing.T) {
	data := map[string]interface{}{
		"Workflow": map[string]interface{}{
			"id":             "wf-test",
			"name":           "Workflow Test",
			"NodeCount":      float64(3),
			"ExecutionCount": float64(10),
			"SuccessRate":    float64(40),
			"TotalCost":      float64(1.23),
		},
	}

	var buf bytes.Buffer
	workflowGetTable(&buf, data, "md")
	got := buf.String()

	if !strings.Contains(got, "Success: 40.0%") {
		t.Fatalf("expected success rate to render as 40.0%%, got:\n%s", got)
	}
	if strings.Contains(got, "4000.0%") {
		t.Fatalf("success rate was multiplied twice:\n%s", got)
	}
}

func TestWorkflowsTableRendersAPISuccessRateAsPercent(t *testing.T) {
	data := map[string]interface{}{
		"Workflows": []interface{}{
			map[string]interface{}{
				"id":             "wf-test",
				"name":           "Workflow Test",
				"ExecutionCount": float64(10),
				"SuccessRate":    float64(40),
				"TotalCost":      float64(1.23),
				"NodeCount":      float64(3),
			},
		},
	}

	var buf bytes.Buffer
	workflowsTable(&buf, data, "md")
	got := buf.String()

	if !strings.Contains(got, "40.0%") {
		t.Fatalf("expected success rate to render as 40.0%%, got:\n%s", got)
	}
	if strings.Contains(got, "4000.0%") {
		t.Fatalf("success rate was multiplied twice:\n%s", got)
	}
}
