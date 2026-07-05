package runtime

import (
	"testing"

	"github.com/alhasaniq/consortium/pkg/workflow"
)

func TestNodeTypeToActivityTypeChildWorkflow(t *testing.T) {
	got := NodeTypeToActivityType(workflow.NodeTypeChildWorkflow)
	if got != ActivityTypeChildWorkflow {
		t.Fatalf("expected %q, got %q", ActivityTypeChildWorkflow, got)
	}
}
