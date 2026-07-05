package jobs

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/trace"
	"github.com/alhasaniq/consortium/pkg/workflow"
)

// buildResultFromDB constructs a WorkflowExecutionResult from persisted job + node data.
func (m *Manager) buildResultFromDB(job *storage.WorkflowExecution, workflowID string) (*WorkflowExecutionResult, error) {
	nodes, err := m.storage.GetWorkflowNodes(job.ID)
	if err != nil {
		return nil, fmt.Errorf("get nodes for job %s: %w", job.ID, err)
	}
	failedErrorCode := ""
	callTraceMeta := make(map[string]map[string]interface{})
	if spans, spanErr := m.storage.GetJobSpans(job.ID); spanErr == nil {
		for _, span := range spans {
			if span.Kind != trace.KindCall || span.NodeID == "" || len(span.Attributes) == 0 {
				continue
			}

			dest := callTraceMeta[span.NodeID]
			if dest == nil {
				dest = make(map[string]interface{})
			}
			for _, key := range []string{
				"finish_reason",
				"rate_limit_wait_ms",
				"max_tokens",
				"tokens_input",
				"tokens_output",
				"messages_count",
				"call_type",
				"retry_layer",
				"error_phase",
				"provider_error_code",
				"provider_status_code",
				"provider_request_id",
				"provider",
			} {
				if v, ok := span.Attributes[key]; ok {
					dest[key] = v
				}
			}
			callTraceMeta[span.NodeID] = dest
		}
	}

	wfResult := &workflow.WorkflowResult{
		WorkflowID:        workflowID,
		Success:           job.Status == events.JobStatusCompleted,
		FinalOutput:       job.ResultText,
		TotalTokens:       job.TokensTotal,
		TotalInputTokens:  job.TokensInput,
		TotalOutputTokens: job.TokensOutput,
		TotalCost:         job.Cost,
		Context:           make(map[string]interface{}),
		Outputs:           make(map[string]interface{}),
	}

	if job.Status == events.JobStatusFailed {
		wfResult.Error = job.ErrorMessage
	}

	var totalLatency float64
	for _, n := range nodes {
		totalLatency += n.LatencyMs
		var nodeMeta map[string]interface{}
		if n.Metadata != "" {
			if err := json.Unmarshal([]byte(n.Metadata), &nodeMeta); err != nil {
				nodeMeta = nil
			}
		}
		if traceMeta, ok := callTraceMeta[n.NodeID]; ok {
			if nodeMeta == nil {
				nodeMeta = make(map[string]interface{})
			}
			for k, v := range traceMeta {
				nodeMeta[k] = v
			}
		}

		nr := &workflow.NodeResult{
			NodeID:       n.NodeID,
			Success:      n.Status == events.JobStatusCompleted,
			Output:       n.Output,
			TokensInput:  n.TokensInput,
			TokensOutput: n.TokensOutput,
			Cost:         n.Cost,
			LatencyMs:    n.LatencyMs,
		}
		if len(nodeMeta) > 0 {
			nr.Metadata = nodeMeta
		}
		if n.Status == events.JobStatusFailed {
			nr.Error = n.ErrorMessage
			if failedErrorCode == "" && strings.TrimSpace(n.ErrorCode) != "" {
				failedErrorCode = strings.TrimSpace(n.ErrorCode)
			}
		}
		wfResult.NodeResults = append(wfResult.NodeResults, nr)

		// Populate context map (node_id -> output) for parent/child forwarding.
		if n.Output != "" {
			wfResult.Context[n.NodeID] = n.Output
			wfResult.Outputs[n.NodeID] = n.Output
		}

		if len(nodeMeta) > 0 {
			if outputName, ok := nodeMeta["output_name"].(string); ok && outputName != "" {
				// Preserve named output keys for output/result nodes even when value is empty.
				// Benchmark contract grading depends on canonical key presence.
				if n.NodeType == string(workflow.NodeTypeResult) || n.Output != "" {
					wfResult.Outputs[outputName] = n.Output
				}
			}
		}
	}
	wfResult.TotalLatency = totalLatency

	if job.Status == events.JobStatusFailed && strings.TrimSpace(wfResult.Error) == "" {
		for _, n := range nodes {
			if strings.EqualFold(n.Status, events.JobStatusFailed) && strings.TrimSpace(n.ErrorMessage) != "" {
				wfResult.Error = strings.TrimSpace(n.ErrorMessage)
				break
			}
		}
	}

	// Use FinalOutput from last completed node if job-level is empty.
	if wfResult.FinalOutput == "" && len(nodes) > 0 {
		for i := len(nodes) - 1; i >= 0; i-- {
			if nodes[i].Output != "" {
				wfResult.FinalOutput = nodes[i].Output
				break
			}
		}
	}

	execResult := &WorkflowExecutionResult{
		JobID:      job.ID,
		WorkflowID: workflowID,
		Success:    wfResult.Success,
		Result:     wfResult,
	}
	if !wfResult.Success {
		execResult.Error = wfResult.Error
		execResult.ErrorCode = failedErrorCode
	}
	return execResult, nil
}
