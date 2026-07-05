package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/gorilla/mux"
)

// nodeResponseOpts controls which optional fields are included in node response maps.
type nodeResponseOpts struct {
	IncludePrompt   bool
	IncludeMetadata bool
	IncludeTiming   bool // started_at, completed_at
	IncludeExecInfo bool // execution_uid, attempt_number
}

// mapWorkflowNodesToResponse converts storage WorkflowNodes to JSON-ready maps.
// Core fields are always included; optional fields are controlled by opts.
func mapWorkflowNodesToResponse(nodes []storage.WorkflowNode, opts nodeResponseOpts) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(nodes))
	for _, node := range nodes {
		m := map[string]interface{}{
			"node_id":       node.NodeID,
			"node_label":    node.NodeLabel,
			"node_name":     node.NodeName,
			"node_type":     node.NodeType,
			"node_order":    node.NodeOrder,
			"status":        node.Status,
			"output":        node.Output,
			"tokens_input":  node.TokensInput,
			"tokens_output": node.TokensOutput,
			"cost":          node.Cost,
			"latency_ms":    node.LatencyMs,
			"model":         node.Model,
			"error_message": node.ErrorMessage,
		}
		if opts.IncludePrompt {
			m["prompt"] = node.Prompt
		}
		if opts.IncludeMetadata {
			m["metadata"] = node.Metadata
		}
		if opts.IncludeTiming {
			m["started_at"] = node.StartedAt
			m["completed_at"] = node.CompletedAt
		}
		if opts.IncludeExecInfo {
			m["execution_uid"] = node.ExecutionUID
			m["attempt_number"] = node.AttemptNumber
		}
		if node.ParentNodeID != "" {
			m["parent_node_id"] = node.ParentNodeID
		}
		result = append(result, m)
	}
	return result
}

// handleGetJob returns job details by ID.
// TODO(v0.1-security): Job detail responses include prompts, outputs,
// workflow config, provider metadata, and costs. Require auth and per-user or
// tenant scoping before treating this as a public API.
func (api *WorkflowAPI) handleGetJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["id"]

	if jobID == "" {
		api.respondWithError(w, http.StatusBadRequest, "Missing job ID", nil)
		return
	}

	job, err := api.storage.GetExecution(jobID)
	if err != nil {
		api.respondWithError(w, http.StatusNotFound, "Job not found", err)
		return
	}

	// Fetch workflow nodes for detailed per-node metrics
	nodes, _ := api.storage.GetWorkflowNodes(jobID)

	nodeData := mapWorkflowNodesToResponse(nodes, nodeResponseOpts{
		IncludePrompt:   true,
		IncludeMetadata: true,
		IncludeTiming:   true,
		IncludeExecInfo: true,
	})

	resp := map[string]interface{}{
		"id":                job.ID,
		"workflow_id":       job.WorkflowID,
		"status":            job.Status,
		"description":       job.Description,
		"result_text":       job.ResultText,
		"tokens_input":      job.TokensInput,
		"tokens_output":     job.TokensOutput,
		"tokens_total":      job.TokensTotal,
		"cost":              job.Cost,
		"created_at":        job.CreatedAt,
		"updated_at":        job.UpdatedAt,
		"config_hash":       job.ConfigHash,
		"snapshot_sequence": job.LastEventSequence,
		"nodes":             nodeData,
	}

	// Include durable runtime metadata when present
	if job.RunNumber > 0 {
		resp["run_number"] = job.RunNumber
	}
	if job.DAGHash != "" {
		resp["dag_hash"] = job.DAGHash
	}
	if job.WorkflowExecutionID != "" {
		resp["workflow_execution_id"] = job.WorkflowExecutionID
	}

	api.respondWithJSON(w, http.StatusOK, resp)
}

// handleGetJobTrace returns trace spans for a job, grouped by node_id.
func (api *WorkflowAPI) handleGetJobTrace(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["id"]

	if jobID == "" {
		api.respondWithError(w, http.StatusBadRequest, "Missing job ID", nil)
		return
	}

	// Verify job exists
	if _, err := api.storage.GetExecution(jobID); err != nil {
		api.respondWithError(w, http.StatusNotFound, "Job not found", err)
		return
	}

	groups, err := api.storage.GetJobSpansByNode(jobID)
	if err != nil {
		api.respondWithError(w, http.StatusInternalServerError, "Failed to fetch trace spans", err)
		return
	}

	api.respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"job_id":      jobID,
		"node_groups": groups,
	})
}

// handleListJobs returns a paginated list of jobs.
// TODO(v0.1-security): List responses expose prompt/result summaries from the
// shared job table. Keep this behind the operator UI until auth scoping lands.
// Query parameters:
//   - limit: number of jobs to return (default 20, max 100)
//   - cursor: pagination cursor for next page
//   - status: filter by status (pending, running, completed, failed)
//   - workflow_id: filter by workflow ID
func (api *WorkflowAPI) handleListJobs(w http.ResponseWriter, r *http.Request) {
	// Parse query parameters
	query := r.URL.Query()

	// Parse limit
	limit := 20
	if limitStr := query.Get("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
			if limit > 100 {
				limit = 100
			}
		}
	}

	// Get cursor
	cursor := query.Get("cursor")

	// Build filters
	var filters *storage.ExecutionFilters
	status := query.Get("status")
	workflowID := query.Get("workflow_id")
	configHash := query.Get("config_hash")
	if status != "" || workflowID != "" || configHash != "" {
		filters = &storage.ExecutionFilters{
			Status:     status,
			WorkflowID: workflowID,
			ConfigHash: configHash,
		}
	}

	// Fetch paginated jobs
	result, err := api.storage.ListExecutionsPaginated(cursor, limit, filters)
	if err != nil {
		api.respondWithError(w, http.StatusInternalServerError, "Failed to list jobs", err)
		return
	}

	// Transform to API response format with summary info
	jobs := make([]map[string]interface{}, 0, len(result.Executions))
	for _, exec := range result.Executions {
		// Get node count for this job
		nodeCount, _ := api.storage.CountExecutionNodes(exec.ID)

		// Extract aggregation method from result if present
		var aggregationMethod string
		if exec.ResponseData != "" {
			var respData map[string]interface{}
			if err := json.Unmarshal([]byte(exec.ResponseData), &respData); err == nil {
				if method, ok := respData["aggregation_method"].(string); ok {
					aggregationMethod = method
				}
			}
		}

		prompt := workflow.ExtractInputPromptFromRequestData(exec.RequestData)

		jobs = append(jobs, map[string]interface{}{
			"id":                    exec.ID,
			"workflow_id":           exec.WorkflowID,
			"description":           exec.Description,
			"status":                exec.Status,
			"tokens_total":          exec.TokensTotal,
			"cost":                  exec.Cost,
			"created_at":            exec.CreatedAt,
			"node_count":            nodeCount,
			"aggregation_method":    aggregationMethod,
			"result_text":           exec.ResultText,
			"prompt":                prompt,
			"config_hash":           exec.ConfigHash,
			"run_number":            exec.RunNumber,
			"dag_hash":              exec.DAGHash,
			"workflow_execution_id": exec.WorkflowExecutionID,
		})
	}

	api.respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"jobs": jobs,
		"pagination": map[string]interface{}{
			"cursor":   result.NextCursor,
			"has_more": result.HasMore,
		},
	})
}

// handleGetJobConfig returns the normalized config for a job, computed on-demand
// from request_data with executor defaults applied.
func (api *WorkflowAPI) handleGetJobConfig(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["id"]

	if jobID == "" {
		api.respondWithError(w, http.StatusBadRequest, "Missing job ID", nil)
		return
	}

	job, err := api.storage.GetExecution(jobID)
	if err != nil {
		api.respondWithError(w, http.StatusNotFound, "Job not found", err)
		return
	}

	if job.RequestData == "" {
		api.respondWithError(w, http.StatusNotFound, "No config data available for this job", nil)
		return
	}

	var wf workflow.Workflow
	if err := json.Unmarshal([]byte(job.RequestData), &wf); err != nil {
		api.respondWithError(w, http.StatusInternalServerError, "Failed to parse job config", err)
		return
	}

	api.respondWithJSON(w, http.StatusOK, workflow.NormalizeForDisplay(&wf))
}

// handleGetJobWorkflow returns the executable workflow snapshot used by a job.
// It is intentionally separate from saved workflow definitions: ad-hoc jobs can
// have a workflow_id without a row in workflow storage.
func (api *WorkflowAPI) handleGetJobWorkflow(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["id"]

	if jobID == "" {
		api.respondWithError(w, http.StatusBadRequest, "Missing job ID", nil)
		return
	}

	job, err := api.storage.GetExecution(jobID)
	if err != nil {
		api.respondWithError(w, http.StatusNotFound, "Job not found", err)
		return
	}

	wf, source, err := workflowSnapshotForJob(job)
	if err != nil {
		api.respondWithError(w, http.StatusNotFound, "No workflow snapshot available for this job", err)
		return
	}

	workflowID := strings.TrimSpace(wf.ID)
	if workflowID == "" {
		workflowID = strings.TrimSpace(job.WorkflowID)
		wf.ID = workflowID
	}
	if strings.TrimSpace(wf.Name) == "" {
		wf.Name = workflowNameFromJob(job)
	}

	savedWorkflowExists := false
	if workflowID != "" {
		if _, err := api.storage.GetWorkflow(workflowID); err == nil {
			savedWorkflowExists = true
		} else if !errors.Is(err, storage.ErrNotFound) {
			api.respondWithError(w, http.StatusInternalServerError, "Failed to check saved workflow", err)
			return
		}
	}

	api.respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"job_id":                job.ID,
		"workflow_id":           workflowID,
		"workflow":              wf,
		"source":                source,
		"saved_workflow_exists": savedWorkflowExists,
	})
}

func workflowSnapshotForJob(job *storage.Job) (*workflow.Workflow, string, error) {
	if job == nil {
		return nil, "", storage.ErrNotFound
	}

	if wf, err := parseWorkflowSnapshot(job.RequestData); err == nil {
		return wf, "request_data", nil
	}
	if wf, err := parseWorkflowSnapshot(job.DAGSnapshot); err == nil {
		return wf, "dag_snapshot", nil
	}
	return nil, "", storage.ErrNotFound
}

func parseWorkflowSnapshot(raw string) (*workflow.Workflow, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, storage.ErrNotFound
	}
	var wf workflow.Workflow
	if err := json.Unmarshal([]byte(raw), &wf); err != nil {
		return nil, err
	}
	if len(wf.Nodes) == 0 {
		return nil, storage.ErrNotFound
	}
	return &wf, nil
}

func workflowNameFromJob(job *storage.Job) string {
	description := strings.TrimSpace(job.Description)
	if strings.HasPrefix(description, "Workflow: ") {
		description = strings.TrimSpace(strings.TrimPrefix(description, "Workflow: "))
	}
	if description != "" {
		return description
	}
	if strings.TrimSpace(job.WorkflowID) != "" {
		return job.WorkflowID
	}
	return "Ad-hoc Workflow"
}

// handleConfigDiff returns the diff between two jobs' configs.
func (api *WorkflowAPI) handleConfigDiff(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID1 := vars["id"]
	jobID2 := vars["id2"]

	if jobID1 == "" || jobID2 == "" {
		api.respondWithError(w, http.StatusBadRequest, "Missing job IDs", nil)
		return
	}

	job1, err := api.storage.GetExecution(jobID1)
	if err != nil {
		api.respondWithError(w, http.StatusNotFound, "Job not found: "+jobID1, err)
		return
	}
	job2, err := api.storage.GetExecution(jobID2)
	if err != nil {
		api.respondWithError(w, http.StatusNotFound, "Job not found: "+jobID2, err)
		return
	}

	if job1.RequestData == "" || job2.RequestData == "" {
		api.respondWithError(w, http.StatusNotFound, "Config data not available for one or both jobs", nil)
		return
	}

	var wf1, wf2 workflow.Workflow
	if err := json.Unmarshal([]byte(job1.RequestData), &wf1); err != nil {
		api.respondWithError(w, http.StatusInternalServerError, "Failed to parse config for job "+jobID1, err)
		return
	}
	if err := json.Unmarshal([]byte(job2.RequestData), &wf2); err != nil {
		api.respondWithError(w, http.StatusInternalServerError, "Failed to parse config for job "+jobID2, err)
		return
	}

	diff := workflow.DiffWorkflows(&wf1, &wf2)
	api.respondWithJSON(w, http.StatusOK, diff)
}
