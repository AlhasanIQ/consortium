package api

import (
	"encoding/json"
	"fmt"

	"github.com/alhasaniq/consortium/pkg/jobs"
	"github.com/alhasaniq/consortium/pkg/workflow"
)

// workflowFromWorkflowFile converts frontend workflow-file payloads into runtime workflows.
// Conversion is centralized in jobs.WorkflowFromDefinition so API submit and backend
// workflow execution share one implementation path.
func workflowFromWorkflowFile(raw map[string]interface{}, inputValues map[string]interface{}) (*workflow.Workflow, error) {
	if raw == nil {
		return nil, fmt.Errorf("workflow_file is required")
	}
	definition, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal workflow_file: %w", err)
	}
	return jobs.WorkflowFromDefinition(string(definition), inputValues)
}
