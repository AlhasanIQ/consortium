package admin

import (
	"log"
	"net/http"
	"strings"

	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/gorilla/mux"
)

func (s *Server) handleJobNodes(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["id"]

	// Get filter parameters
	search := strings.ToLower(r.URL.Query().Get("search"))
	statusFilter := r.URL.Query().Get("status")
	typeFilter := strings.ToLower(r.URL.Query().Get("type"))
	limit := parseIntDefault(r.URL.Query().Get("limit"), 0)
	offset := parseIntDefault(r.URL.Query().Get("offset"), 0)
	if limit < 0 {
		limit = 0
	}
	if limit > 1000 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}

	nodes, err := s.storage.GetWorkflowNodes(jobID)
	if err != nil {
		writeJSONError(w, "Failed to get workflow nodes", http.StatusInternalServerError)
		return
	}
	workflowContext := map[string]interface{}{}
	nodeConfigs := map[string]string{}
	if job, jobErr := s.storage.GetExecution(jobID); jobErr == nil {
		workflowContext = extractWorkflowContext(job.RequestData)
		nodeConfigs = extractFromSnapshot(job.DAGSnapshot).Configs
	} else {
		log.Printf("Warning: Failed to fetch job %s while rendering nodes: %v", jobID, jobErr)
	}

	// Build full context: initial request vars + node outputs for prompt interpolation.
	workflowContext = buildFullWorkflowContext(workflowContext, nodes)

	// Reorder nodes so children appear after their parent
	nodes, _, _, _ = reorderNodesParentFirst(nodes)

	// Show child workflow cost from metadata in the filtered timeline view.
	for i := range nodes {
		applyChildWorkflowDisplayCost(&nodes[i])
	}

	// Filter nodes
	var filteredNodes []NodeExecutionView
	for _, node := range nodes {
		node.Prompt = workflow.InterpolateVariables(node.Prompt, workflowContext)
		nodeConfig := resolveNodeConfig(node.NodeID, node.Metadata, nodeConfigs)

		// Search filter - check prompt, output, and node type
		if search != "" {
			searchMatch := strings.Contains(strings.ToLower(node.Prompt), search) ||
				strings.Contains(strings.ToLower(node.Output), search) ||
				strings.Contains(strings.ToLower(node.NodeType), search) ||
				strings.Contains(strings.ToLower(nodeConfig), search)
			if !searchMatch {
				continue
			}
		}

		// Status filter
		if statusFilter != "" && node.Status != statusFilter {
			continue
		}

		// Type filter
		if typeFilter != "" && strings.ToLower(node.NodeType) != typeFilter {
			continue
		}

		filteredNodes = append(filteredNodes, NodeExecutionView{
			WorkflowNode: node,
			NodeConfig:   nodeConfig,
		})
	}

	totalNodes := len(filteredNodes)
	hasMore := false
	if limit > 0 {
		if offset >= len(filteredNodes) {
			filteredNodes = []NodeExecutionView{}
		} else {
			end := offset + limit
			if end < len(filteredNodes) {
				hasMore = true
			}
			if end > len(filteredNodes) {
				end = len(filteredNodes)
			}
			filteredNodes = filteredNodes[offset:end]
		}
	}

	data := map[string]interface{}{
		"Nodes":   filteredNodes,
		"Total":   totalNodes,
		"Limit":   limit,
		"Offset":  offset,
		"HasMore": hasMore,
	}

	writeJSONResponse(w, data)
}
