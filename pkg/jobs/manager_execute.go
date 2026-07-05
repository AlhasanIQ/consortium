package jobs

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

// WorkflowExecutionResult contains the result of a workflow execution
type WorkflowExecutionResult struct {
	JobID      string                   `json:"job_id"`
	WorkflowID string                   `json:"workflow_id"`
	Success    bool                     `json:"success"`
	Result     *workflow.WorkflowResult `json:"result,omitempty"`
	Error      string                   `json:"error,omitempty"`
	ErrorCode  string                   `json:"error_code,omitempty"`
	Duplicate  bool                     `json:"duplicate,omitempty"` // True if this was a duplicate request
}

// ExecuteWorkflowOptions controls runtime submission behavior for sync execute paths.
type ExecuteWorkflowOptions struct {
	// AdmissionBypass allows an explicit one-off root submission while admission
	// is paused. Intended for operator probes and administrative workflows.
	AdmissionBypass bool
}

// StreamingCallback is called for each workflow execution event
type StreamingCallback func(event *ExecutionEvent)

// ExecutionEvent represents a workflow execution event.
// Type should be one of the canonical event constants from pkg/events:
// EventStatus, EventJobCreated, EventNodeStart, EventNodeComplete, EventNodeFailed,
// EventComplete, EventError, EventCancelled.
type ExecutionEvent struct {
	Type      string                 `json:"type"` // One of events.Event* constants
	JobID     string                 `json:"job_id"`
	NodeID    string                 `json:"node_id,omitempty"`
	Message   string                 `json:"message"`
	Output    string                 `json:"output,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Code      string                 `json:"code,omitempty"` // Machine-readable error code
	Timestamp time.Time              `json:"timestamp"`      // Actual event timestamp
	Data      map[string]interface{} `json:"data,omitempty"`
}

// executeDurable runs a workflow through the durable runtime.
func (m *Manager) executeDurable(ctx context.Context, job *storage.WorkflowExecution, wf *workflow.Workflow, callback StreamingCallback, replay *runtime.ReplayRequest) (*WorkflowExecutionResult, error) {
	jobID := job.ID

	identity := &runtime.ExecutionIdentity{
		WorkflowExecutionID: job.WorkflowExecutionID,
		RunID:               job.RunID,
		RunNumber:           job.RunNumber,
		DAGHash:             job.DAGHash,
	}

	snapshot := &runtime.FrozenSnapshot{
		DAGHash:    job.DAGHash,
		Definition: []byte(job.DAGSnapshot),
	}

	costTracker := workflow.NewCostTracker(wf.Limits)

	// Build event callback that always persists to job_events, then
	// optionally forwards to the WS streaming callback.
	eventCb := func(event *runtime.ExecutionEvent) {
		// Extract structured fields from payload for persistence.
		var nodeID, message, errMsg, code string
		if p := event.Payload; p != nil {
			if v, ok := p["node_id"].(string); ok {
				nodeID = v
			}
			if v, ok := p["message"].(string); ok {
				message = v
			}
			if v, ok := p["error"].(string); ok {
				errMsg = v
			}
			if v, ok := p["code"].(string); ok {
				code = v
			}
		}

		// Persist to job_events (always, regardless of WS callback).
		envelope, persistErr := m.storage.AppendEventWithDetails(
			context.Background(), jobID, event.Type,
			nodeID, message, errMsg, code, event.Payload,
		)
		if persistErr != nil {
			log.Printf("⚠️ [Durable] failed to persist event for job %s: %v", jobID, persistErr)
		}

		// Forward to optional WS callback.
		if callback != nil {
			execEvent := &ExecutionEvent{
				Type:      event.Type,
				JobID:     jobID,
				Timestamp: time.Now(),
			}
			data := make(map[string]interface{})
			if envelope != nil {
				data["sequence"] = envelope.Sequence
				if envelope.ExecutionID != "" {
					data["execution_id"] = envelope.ExecutionID
				}
				if envelope.RunID != "" {
					data["run_id"] = envelope.RunID
				}
				if envelope.AgentRunID != "" {
					data["agent_run_id"] = envelope.AgentRunID
				}
				if envelope.Iteration > 0 {
					data["iteration"] = envelope.Iteration
				}
			}
			if p := event.Payload; p != nil {
				if rawData, ok := p["data"].(map[string]interface{}); ok {
					for k, v := range rawData {
						data[k] = v
					}
				}
				for k, v := range p {
					switch k {
					case "job_id":
						// job_id is sourced from the enclosing execution context.
					case "message":
						execEvent.Message = fmt.Sprintf("%v", v)
					case "node_id":
						execEvent.NodeID = fmt.Sprintf("%v", v)
					case "output":
						execEvent.Output = fmt.Sprintf("%v", v)
					case "error":
						execEvent.Error = fmt.Sprintf("%v", v)
					case "code":
						execEvent.Code = fmt.Sprintf("%v", v)
					case "data":
						// Already merged above.
					default:
						data[k] = v
					}
				}
			}
			if len(data) > 0 {
				execEvent.Data = data
			}
			if execEvent.Message == "" {
				switch execEvent.Type {
				case events.EventComplete:
					execEvent.Message = "Workflow completed successfully"
				case events.EventCancelled:
					execEvent.Message = "Workflow cancelled"
				}
			}
			callback(execEvent)
		}
	}

	execCtx := &workflow.ExecutionContext{
		JobID:               jobID,
		WorkflowExecutionID: job.WorkflowExecutionID,
		WorkflowID:          wf.ID,
		RunID:               job.RunID,
		TraceWriter:         m.storage,
		MaxParallelNodes:    m.config.MaxParallelNodesPerWF,
		ExecuteChildWorkflow: func(childCtx context.Context, req *workflow.ChildWorkflowRequest) (*workflow.ChildWorkflowResult, error) {
			return m.executeChildWorkflowWithParentReplay(childCtx, req, replay)
		},
		ExecuteAgentRun: func(agentCtx context.Context, req *workflow.AgentRunRequest) (*workflow.AgentRunResult, error) {
			return m.executeAgentRun(agentCtx, req)
		},
		ExecuteNovoRun: func(novoCtx context.Context, req *workflow.NovoRunRequest) (*workflow.AgentRunResult, error) {
			return m.executeNovoRun(novoCtx, req)
		},
	}

	params := &runtime.StartParams{
		Identity:      identity,
		Snapshot:      snapshot,
		Workflow:      wf,
		EventCallback: eventCb,
		ExecCtx:       execCtx,
		CostTracker:   costTracker,
		Replay:        replay,
	}

	err := m.durableRuntime.Start(ctx, params)

	// Build result
	result := &WorkflowExecutionResult{
		JobID:      jobID,
		WorkflowID: wf.ID,
	}

	// Get updated job state
	updatedJob, lookupErr := m.storage.GetExecution(jobID)
	if lookupErr == nil && updatedJob != nil {
		result.Success = updatedJob.Status == events.JobStatusCompleted
		if updatedJob.Status == events.JobStatusFailed {
			result.Error = updatedJob.ErrorMessage
		}
	}

	if err != nil {
		result.Success = false
		if result.Error == "" {
			result.Error = err.Error()
		}
	}

	return result, nil
}

// ExecuteWorkflow executes a workflow with automatic job tracking.
// Requires StartWorkers() to have been called first.
func (m *Manager) ExecuteWorkflow(ctx context.Context, wf *workflow.Workflow) (*WorkflowExecutionResult, error) {
	return m.ExecuteWorkflowWithReplay(ctx, wf, nil)
}

// ExecuteWorkflowWithReplay executes a workflow with optional replay seeding.
// Replay is only applied on fresh durable runs (no existing history).
func (m *Manager) ExecuteWorkflowWithReplay(ctx context.Context, wf *workflow.Workflow, replay *runtime.ReplayRequest) (*WorkflowExecutionResult, error) {
	return m.ExecuteWorkflowWithReplayOptions(ctx, wf, replay, ExecuteWorkflowOptions{})
}

// ExecuteWorkflowWithReplayOptions executes a workflow with optional replay seeding
// and submission options.
func (m *Manager) ExecuteWorkflowWithReplayOptions(
	ctx context.Context,
	wf *workflow.Workflow,
	replay *runtime.ReplayRequest,
	opts ExecuteWorkflowOptions,
) (*WorkflowExecutionResult, error) {
	if !m.workersStarted.Load() {
		return nil, ErrWorkersNotStarted
	}
	resp, err := m.SubmitWorkflow(ctx, &SubmitWorkflowRequest{
		Workflow:        wf,
		ForceNewRun:     true,
		AdmissionBypass: opts.AdmissionBypass,
		Replay:          replay,
	})
	if err != nil {
		return nil, err
	}
	return m.waitForCompletion(ctx, resp.JobID, wf.ID)
}
