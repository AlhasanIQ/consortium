package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/alhasaniq/consortium/pkg/jobs"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/alhasaniq/consortium/pkg/workflow/compiler"
	"github.com/google/uuid"
)

type CompilePreviewRequest struct {
	Workflow     workflow.Workflow      `json:"workflow"`
	WorkflowFile map[string]interface{} `json:"workflow_file,omitempty"`
	InputValues  map[string]interface{} `json:"input_values,omitempty"`
}

type CompilePreviewResponse struct {
	WorkflowID        string                   `json:"workflow_id"`
	Nodes             []*workflow.Node         `json:"nodes"`
	Edges             []*workflow.Edge         `json:"edges"`
	AggregationGroups []CompilePreviewAggGroup `json:"aggregation_groups"`
}

type CompilePreviewAggGroup struct {
	AnchorNodeID           string                         `json:"anchor_node_id"`
	Method                 string                         `json:"method"`
	SourceWorkflowID       string                         `json:"source_workflow_id"`
	TerminalNodeID         string                         `json:"terminal_node_id"`
	PresentationResultID   string                         `json:"presentation_result_id,omitempty"`
	InputNodeIDs           []string                       `json:"input_node_ids"`
	NodeIDs                []string                       `json:"node_ids"`
	LLMJobCount            int                            `json:"llm_job_count"`
	TopLevelLLMJobCount    int                            `json:"top_level_llm_job_count"`
	ConditionalLLMJobCount int                            `json:"conditional_llm_job_count"`
	ConditionalLLMJobs     []CompilePreviewConditionalJob `json:"conditional_llm_jobs"`
	OperationCount         int                            `json:"operation_count"`
}

type CompilePreviewConditionalJob struct {
	ID             string                `json:"id"`
	ParentNodeID   string                `json:"parent_node_id"`
	Branch         string                `json:"branch"`
	Type           string                `json:"type"`
	Model          string                `json:"model,omitempty"`
	SystemPrompt   string                `json:"system_prompt,omitempty"`
	Prompt         string                `json:"prompt,omitempty"`
	Temperature    *float64              `json:"temperature,omitempty"`
	MaxTokens      int                   `json:"max_tokens,omitempty"`
	TimeoutSeconds int                   `json:"timeout_seconds,omitempty"`
	RetryPolicy    *workflow.RetryPolicy `json:"retry_policy,omitempty"`
	Label          string                `json:"label,omitempty"`
}

func (api *WorkflowAPI) handleCompilePreview(w http.ResponseWriter, r *http.Request) {
	var req CompilePreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.respondWithJSON(w, http.StatusBadRequest, NewAPIError("Invalid JSON payload", ErrCodeInvalidJSON).WithDetails(err.Error()))
		return
	}
	if len(req.WorkflowFile) == 0 && req.Workflow.ID == "" && req.Workflow.Name == "" && len(req.Workflow.Nodes) == 0 && len(req.Workflow.Edges) == 0 {
		api.respondWithJSON(w, http.StatusBadRequest, NewAPIError("workflow or workflow_file is required", ErrCodeInvalidWorkflow))
		return
	}

	var previewWorkflow *workflow.Workflow
	var presentationResultByAnchor map[string]string
	if len(req.WorkflowFile) > 0 {
		if err := workflow.NormalizeWorkflowFileIDs(req.WorkflowFile); err != nil {
			api.respondWithJSON(w, http.StatusBadRequest, NewAPIError("Invalid workflow file", ErrCodeInvalidWorkflow).WithDetails(err.Error()))
			return
		}
		presentationResultByAnchor = compilePreviewPresentationResultIDsFromWorkflowFile(req.WorkflowFile)
		convertedWorkflow, err := workflowFromWorkflowFile(req.WorkflowFile, req.InputValues)
		if err != nil {
			api.respondWithJSON(w, http.StatusBadRequest, NewAPIError("Failed to convert workflow file", ErrCodeInvalidWorkflow).WithDetails(err.Error()))
			return
		}
		previewWorkflow = convertedWorkflow
	} else {
		previewWorkflow = &req.Workflow
		presentationResultByAnchor = compilePreviewPresentationResultIDsFromWorkflow(previewWorkflow)
	}
	if previewWorkflow.ID == "" {
		previewWorkflow.ID = uuid.New().String()
	}

	compiledWorkflow, report, err := compiler.Compile(r.Context(), previewWorkflow, jobs.NewWorkflowDefinitionResolver(api.storage))
	if err != nil {
		api.respondWithJSON(w, http.StatusBadRequest, NewAPIError("Workflow validation failed", ErrCodeInvalidWorkflow).WithDetails(err.Error()))
		return
	}
	ensureCompilePreviewContext(compiledWorkflow)
	// Compile preview is a structural dry-run. Model availability is validated on
	// real execution paths, so nil keeps this endpoint usable without provider state.
	if result := workflow.NewValidator(nil).Validate(compiledWorkflow); !result.Valid {
		api.respondWithJSON(w, http.StatusBadRequest, NewAPIError("Workflow validation failed", ErrCodeInvalidWorkflow).WithDetails(result.Errors))
		return
	}

	api.respondWithJSON(w, http.StatusOK, CompilePreviewResponse{
		WorkflowID:        compiledWorkflow.ID,
		Nodes:             compiledWorkflow.Nodes,
		Edges:             compiledWorkflow.Edges,
		AggregationGroups: compilePreviewAggregationGroups(compiledWorkflow, report, presentationResultByAnchor),
	})
}

func ensureCompilePreviewContext(wf *workflow.Workflow) {
	if wf == nil {
		return
	}
	if wf.Context == nil {
		wf.Context = map[string]interface{}{}
	}
	if _, ok := wf.Context["user_prompt"]; !ok {
		wf.Context["user_prompt"] = "__compile_preview_user_prompt__"
	}
}

func compilePreviewAggregationGroups(compiled *workflow.Workflow, report *compiler.Report, presentationResultByAnchor map[string]string) []CompilePreviewAggGroup {
	if compiled == nil || report == nil {
		return []CompilePreviewAggGroup{}
	}
	nodesByID := make(map[string]*workflow.Node, len(compiled.Nodes))
	for _, node := range compiled.Nodes {
		nodesByID[node.ID] = node
	}

	groups := make([]CompilePreviewAggGroup, 0, len(report.ExpandedRefs))
	for _, ref := range report.ExpandedRefs {
		groupNodeIDs := make([]string, 0, len(ref.ExpandedNodeIDs))
		method := ""
		terminalNodeID := ""
		topLevelLLMJobCount := 0
		conditionalJobs := []CompilePreviewConditionalJob{}
		operationCount := 0
		for _, id := range ref.ExpandedNodeIDs {
			node := nodesByID[id]
			if node == nil {
				continue
			}
			if anchor := metadataStringForPreview(node.Metadata, "aggregation_anchor_id"); anchor != ref.NodeID {
				continue
			}
			groupNodeIDs = append(groupNodeIDs, id)
			if method == "" {
				method = metadataStringForPreview(node.Metadata, "aggregation_method")
			}
			if node.Type == workflow.NodeTypePrompt {
				topLevelLLMJobCount++
			}
			conditionalJobs = append(conditionalJobs, compilePreviewConditionalJobs(node)...)
			if node.Type == workflow.NodeTypeOperation {
				operationCount++
			}
			if node.Type == workflow.NodeTypeResult {
				terminalNodeID = node.ID
			}
		}
		sort.Strings(groupNodeIDs)
		if method == "" || len(groupNodeIDs) == 0 {
			continue
		}
		groups = append(groups, CompilePreviewAggGroup{
			AnchorNodeID:           ref.NodeID,
			Method:                 method,
			SourceWorkflowID:       ref.WorkflowID,
			TerminalNodeID:         terminalNodeID,
			PresentationResultID:   presentationResultByAnchor[ref.NodeID],
			InputNodeIDs:           compilePreviewInputIDs(compiled, ref.NodeID),
			NodeIDs:                groupNodeIDs,
			LLMJobCount:            topLevelLLMJobCount + len(conditionalJobs),
			TopLevelLLMJobCount:    topLevelLLMJobCount,
			ConditionalLLMJobCount: len(conditionalJobs),
			ConditionalLLMJobs:     conditionalJobs,
			OperationCount:         operationCount,
		})
	}
	return groups
}

func compilePreviewInputIDs(wf *workflow.Workflow, anchorID string) []string {
	seen := map[string]struct{}{}
	for _, edge := range wf.Edges {
		// Aggregation compiler outputs anchor-prefixed node IDs via {anchor}--{source};
		// external parent edges into that namespace are the candidate inputs.
		if strings.HasPrefix(edge.Target, anchorID+"--") && !strings.HasPrefix(edge.Source, anchorID+"--") {
			seen[edge.Source] = struct{}{}
		}
	}

	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func compilePreviewConditionalJobs(node *workflow.Node) []CompilePreviewConditionalJob {
	if node == nil {
		return nil
	}
	return compilePreviewConditionalJobsForID(node.ID, node)
}

func compilePreviewConditionalJobsForID(parentID string, node *workflow.Node) []CompilePreviewConditionalJob {
	out := make([]CompilePreviewConditionalJob, 0)
	if node.TrueBranch != nil {
		out = append(out, compilePreviewConditionalBranchJobs(parentID, "true", node.TrueBranch)...)
	}
	if node.FalseBranch != nil {
		out = append(out, compilePreviewConditionalBranchJobs(parentID, "false", node.FalseBranch)...)
	}
	return out
}

func compilePreviewConditionalBranchJobs(parentID, branch string, node *workflow.Node) []CompilePreviewConditionalJob {
	if node == nil {
		return nil
	}
	id := parentID + "_" + branch

	out := make([]CompilePreviewConditionalJob, 0)
	if node.Type == workflow.NodeTypePrompt {
		out = append(out, CompilePreviewConditionalJob{
			ID:             id,
			ParentNodeID:   parentID,
			Branch:         branch,
			Type:           string(node.Type),
			Model:          node.Model,
			SystemPrompt:   node.SystemPrompt,
			Prompt:         node.Prompt,
			Temperature:    node.Temperature,
			MaxTokens:      node.MaxTokens,
			TimeoutSeconds: node.TimeoutSeconds,
			RetryPolicy:    node.RetryPolicy.Clone(),
			Label:          metadataStringForPreview(node.Metadata, "label"),
		})
	}
	out = append(out, compilePreviewConditionalJobsForID(id, node)...)
	return out
}

func compilePreviewPresentationResultIDsFromWorkflowFile(file map[string]interface{}) map[string]string {
	nodeTypes := map[string]string{}
	for _, raw := range interfaceSliceForPreview(file["nodes"]) {
		node, _ := raw.(map[string]interface{})
		id, _ := node["id"].(string)
		nodeType, _ := node["type"].(string)
		if data, _ := node["data"].(map[string]interface{}); data != nil {
			if dataType, _ := data["type"].(string); dataType != "" {
				nodeType = dataType
			}
		}
		if id != "" {
			nodeTypes[id] = nodeType
		}
	}

	out := map[string]string{}
	for _, raw := range interfaceSliceForPreview(file["edges"]) {
		edge, _ := raw.(map[string]interface{})
		source, _ := edge["source"].(string)
		target, _ := edge["target"].(string)
		if source != "" && target != "" && nodeTypes[source] == "aggregation" && nodeTypes[target] == "result" {
			out[source] = target
		}
	}
	return out
}

func compilePreviewPresentationResultIDsFromWorkflow(wf *workflow.Workflow) map[string]string {
	if wf == nil {
		return map[string]string{}
	}
	resultIDs := map[string]struct{}{}
	for _, node := range wf.Nodes {
		if node.Type == workflow.NodeTypeResult {
			resultIDs[node.ID] = struct{}{}
		}
	}

	out := map[string]string{}
	for _, edge := range wf.Edges {
		// Direct workflow payloads may use workflow_ref aggregation anchors rather
		// than builder-only aggregation nodes, so key every edge into a result.
		if _, ok := resultIDs[edge.Target]; ok {
			out[edge.Source] = edge.Target
		}
	}
	return out
}

func interfaceSliceForPreview(value interface{}) []interface{} {
	if values, ok := value.([]interface{}); ok {
		return values
	}
	return nil
}

func metadataStringForPreview(metadata map[string]interface{}, key string) string {
	if metadata == nil {
		return ""
	}
	value, _ := metadata[key].(string)
	return value
}
