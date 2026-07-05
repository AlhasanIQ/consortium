package workflow

import "testing"

func TestValidatorChildWorkflowRequiresID(t *testing.T) {
	validator := NewValidator(nil)
	wf := &Workflow{
		ID:   "parent-wf",
		Name: "Parent",
		Nodes: []*Node{
			{ID: "child", Type: NodeTypeChildWorkflow},
		},
	}

	res := validator.Validate(wf)
	if res.Valid {
		t.Fatalf("expected validation to fail for missing child_workflow_id")
	}
}

func TestValidatorChildWorkflowRejectsSelfReference(t *testing.T) {
	validator := NewValidator(nil)
	wf := &Workflow{
		ID:   "parent-wf",
		Name: "Parent",
		Nodes: []*Node{
			{ID: "child", Type: NodeTypeChildWorkflow, ChildWorkflowID: "parent-wf"},
		},
	}

	res := validator.Validate(wf)
	if res.Valid {
		t.Fatalf("expected validation to fail for self-referenced child workflow")
	}
}
