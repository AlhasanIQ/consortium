package admin

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/internal/appenv"
	"github.com/alhasaniq/consortium/pkg/seeds"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/gorilla/mux"
)

// countNodesRecursive counts nodes and their types recursively
func countNodesRecursive(nodes []*workflow.Node) (int, map[string]int) {
	count := 0
	types := make(map[string]int)

	for _, node := range nodes {
		count++
		types[string(node.Type)]++

		// Recurse into conditional branches
		if node.TrueBranch != nil {
			subCount, subTypes := countNodesRecursive([]*workflow.Node{node.TrueBranch})
			count += subCount
			for k, v := range subTypes {
				types[k] += v
			}
		}
		if node.FalseBranch != nil {
			subCount, subTypes := countNodesRecursive([]*workflow.Node{node.FalseBranch})
			count += subCount
			for k, v := range subTypes {
				types[k] += v
			}
		}
	}

	return count, types
}

// extractModelsFromNodesRecursive extracts unique models from workflow nodes
func extractModelsFromNodesRecursive(nodes []*workflow.Node, modelSet map[string]bool) {
	for _, node := range nodes {
		if node.Model != "" {
			modelSet[node.Model] = true
		}

		// Recurse into conditional branches
		if node.TrueBranch != nil {
			extractModelsFromNodesRecursive([]*workflow.Node{node.TrueBranch}, modelSet)
		}
		if node.FalseBranch != nil {
			extractModelsFromNodesRecursive([]*workflow.Node{node.FalseBranch}, modelSet)
		}
	}
}

// WorkflowFileFormat represents the frontend workflow file format with nodes
type WorkflowFileFormat struct {
	ID          string                   `json:"id"`
	Name        string                   `json:"name"`
	Description string                   `json:"description,omitempty"`
	Nodes       []map[string]interface{} `json:"nodes"`
	Edges       []map[string]interface{} `json:"edges"`
}

type workflowExecutionStats struct {
	ExecutionCount int
	SuccessCount   int
	TotalCost      float64
	TotalTokens    int64
	LastRunAt      *time.Time
	LastRunStatus  string
}

func parseWorkflowDefinitionStats(wf *storage.WorkflowDefinition) WorkflowWithStats {
	stats := WorkflowWithStats{WorkflowDefinition: *wf}
	stats.Layer = seeds.Layer(wf.ID)
	stats.NodeTypes = make(map[string]int)
	stats.ModelsUsed = make([]string, 0)
	stats.AggregationSourceIDs = make([]string, 0)
	stats.WorkflowRefIDs = make([]string, 0)
	stats.ChildWorkflowIDs = make([]string, 0)
	warningAggregationSourceIDs := make([]string, 0)

	// Try parsing as frontend workflow file format (with nodes)
	var fileFormat WorkflowFileFormat
	if err := json.Unmarshal([]byte(wf.Definition), &fileFormat); err == nil && len(fileFormat.Nodes) > 0 {
		stats.NodeCount = len(fileFormat.Nodes)
		modelSet := make(map[string]bool)
		var aggregationSourceIDs, workflowRefIDs, childWorkflowIDs []string

		for _, node := range fileFormat.Nodes {
			nodeType := workflowFileNodeType(node)
			if config := workflowFileNodeConfig(node); config != nil {
				// Extract model from data.config.model
				if model, ok := config["model"].(string); ok && model != "" {
					modelSet[model] = true
				}
				if id := stringValue(config, "aggregationWorkflowId"); id != "" {
					aggregationSourceIDs = append(aggregationSourceIDs, id)
					if !boolValue(config, "benchmarkOutputPackaging") {
						warningAggregationSourceIDs = append(warningAggregationSourceIDs, id)
					}
				}
				if nodeType == string(workflow.NodeTypeWorkflowRef) {
					if id := firstNonEmptyString(stringValue(config, "workflowId"), stringValue(config, "workflowRefId"), stringValue(config, "workflow_ref_id")); id != "" {
						workflowRefIDs = append(workflowRefIDs, id)
					}
				}
				if nodeType == string(workflow.NodeTypeChildWorkflow) {
					if id := firstNonEmptyString(stringValue(config, "childWorkflowId"), stringValue(config, "child_workflow_id")); id != "" {
						childWorkflowIDs = append(childWorkflowIDs, id)
					}
				}
			}
			if nodeType != "" {
				stats.NodeTypes[nodeType]++
			}
		}

		for m := range modelSet {
			stats.ModelsUsed = append(stats.ModelsUsed, m)
		}
		stats.AggregationSourceIDs = sortedUniqueStrings(aggregationSourceIDs)
		stats.WorkflowRefIDs = sortedUniqueStrings(workflowRefIDs)
		stats.ChildWorkflowIDs = sortedUniqueStrings(childWorkflowIDs)
	} else {
		// Try parsing as backend workflow format (with nodes)
		var wfDef workflow.Workflow
		if err := json.Unmarshal([]byte(wf.Definition), &wfDef); err == nil {
			stats.NodeCount, stats.NodeTypes = countNodesRecursive(wfDef.Nodes)

			modelSet := make(map[string]bool)
			extractModelsFromNodesRecursive(wfDef.Nodes, modelSet)
			for m := range modelSet {
				stats.ModelsUsed = append(stats.ModelsUsed, m)
			}
			refs := collectBackendWorkflowRefs(wfDef.Nodes)
			stats.WorkflowRefIDs = refs.WorkflowRefIDs
			stats.ChildWorkflowIDs = refs.ChildWorkflowIDs

			stats.HasCostLimits = wfDef.Limits != nil
		}
	}

	sort.Strings(stats.ModelsUsed)
	stats.ReferencesL0DirectlyAsBenchmark = stats.Layer == seeds.LayerL3 && referencesLayerL0(warningAggregationSourceIDs, stats.WorkflowRefIDs, stats.ChildWorkflowIDs)

	return stats
}

func workflowFileNodeType(node map[string]interface{}) string {
	if data, ok := node["data"].(map[string]interface{}); ok {
		if t, ok := data["type"].(string); ok {
			return t
		}
	}
	if t, ok := node["type"].(string); ok {
		return t
	}
	return ""
}

func workflowFileNodeConfig(node map[string]interface{}) map[string]interface{} {
	data, _ := node["data"].(map[string]interface{})
	if data == nil {
		return node
	}
	config, _ := data["config"].(map[string]interface{})
	if config == nil {
		return node
	}
	return config
}

func stringValue(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return strings.TrimSpace(v)
}

func boolValue(m map[string]interface{}, key string) bool {
	v, _ := m[key].(bool)
	return v
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func sortedUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean == "" {
			continue
		}
		if _, exists := seen[clean]; exists {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	sort.Strings(out)
	return out
}

type workflowReferenceSummary struct {
	WorkflowRefIDs   []string
	ChildWorkflowIDs []string
}

func collectBackendWorkflowRefs(nodes []*workflow.Node) workflowReferenceSummary {
	var workflowRefIDs, childWorkflowIDs []string
	var walk func([]*workflow.Node)
	walk = func(nodes []*workflow.Node) {
		for _, node := range nodes {
			if node == nil {
				continue
			}
			if id := strings.TrimSpace(node.WorkflowRefID); id != "" {
				workflowRefIDs = append(workflowRefIDs, id)
			}
			if id := strings.TrimSpace(node.ChildWorkflowID); id != "" {
				childWorkflowIDs = append(childWorkflowIDs, id)
			}
			if node.TrueBranch != nil {
				walk([]*workflow.Node{node.TrueBranch})
			}
			if node.FalseBranch != nil {
				walk([]*workflow.Node{node.FalseBranch})
			}
		}
	}
	walk(nodes)
	return workflowReferenceSummary{
		WorkflowRefIDs:   sortedUniqueStrings(workflowRefIDs),
		ChildWorkflowIDs: sortedUniqueStrings(childWorkflowIDs),
	}
}

func referencesLayerL0(groups ...[]string) bool {
	for _, group := range groups {
		for _, id := range group {
			if seeds.Layer(id) == seeds.LayerL0 {
				return true
			}
		}
	}
	return false
}

func applyWorkflowExecutionStats(stats *WorkflowWithStats, execution workflowExecutionStats) {
	if stats == nil {
		return
	}
	stats.ExecutionCount = execution.ExecutionCount
	stats.SuccessCount = execution.SuccessCount
	stats.TotalCost = execution.TotalCost
	stats.TotalTokens = execution.TotalTokens
	stats.LastRunAt = execution.LastRunAt
	stats.LastRunStatus = execution.LastRunStatus
	if execution.ExecutionCount > 0 {
		stats.SuccessRate = float64(execution.SuccessCount) / float64(execution.ExecutionCount) * 100
	}
}

// loadWorkflowExecutionStats batches execution-stat lookups for workflows to
// avoid per-workflow query fanout on list endpoints.
func (s *Server) loadWorkflowExecutionStats(workflowIDs []string) (map[string]workflowExecutionStats, error) {
	seen := make(map[string]struct{}, len(workflowIDs))
	ids := make([]string, 0, len(workflowIDs))
	for _, id := range workflowIDs {
		clean := strings.TrimSpace(id)
		if clean == "" {
			continue
		}
		if _, exists := seen[clean]; exists {
			continue
		}
		seen[clean] = struct{}{}
		ids = append(ids, clean)
	}

	out := make(map[string]workflowExecutionStats, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	const maxIDsPerChunk = 300
	for start := 0; start < len(ids); start += maxIDsPerChunk {
		end := start + maxIDsPerChunk
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		query, args := buildWorkflowExecutionStatsQuery(chunk)

		rows, err := s.db.Query(query, args...)
		if err != nil {
			return nil, fmt.Errorf("load workflow execution stats: %w", err)
		}

		for rows.Next() {
			var workflowID string
			var row workflowExecutionStats
			var lastRunRaw sql.NullString
			var lastStatus sql.NullString
			if err := rows.Scan(
				&workflowID,
				&row.ExecutionCount,
				&row.SuccessCount,
				&row.TotalCost,
				&row.TotalTokens,
				&lastRunRaw,
				&lastStatus,
			); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("scan workflow execution stats: %w", err)
			}
			if lastRunRaw.Valid && strings.TrimSpace(lastRunRaw.String) != "" {
				if ts, err := parseAdminTimestamp(lastRunRaw.String); err == nil {
					row.LastRunAt = &ts
				}
			}
			if lastStatus.Valid {
				row.LastRunStatus = lastStatus.String
			}
			out[workflowID] = row
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("iterate workflow execution stats: %w", err)
		}
		if err := rows.Close(); err != nil {
			log.Printf("Error closing rows: %v", err)
		}
	}

	return out, nil
}

func buildWorkflowExecutionStatsQuery(workflowIDs []string) (string, []interface{}) {
	placeholders := strings.TrimRight(strings.Repeat("?,", len(workflowIDs)), ",")
	args := make([]interface{}, len(workflowIDs))
	for i, id := range workflowIDs {
		args[i] = id
	}

	query := fmt.Sprintf(`
		WITH agg AS (
			SELECT
				j.workflow_id,
				COUNT(*) AS total,
				COALESCE(SUM(CASE WHEN j.status = 'completed' THEN 1 ELSE 0 END), 0) AS success,
				COALESCE(SUM(j.cost), 0) AS total_cost,
				COALESCE(SUM(j.tokens_total), 0) AS total_tokens
			FROM jobs j
			WHERE j.workflow_id IN (%s)
			GROUP BY j.workflow_id
		)
		SELECT
			agg.workflow_id,
			agg.total,
			agg.success,
			agg.total_cost,
			agg.total_tokens,
			(
				SELECT updated_at
				FROM jobs j2
				WHERE j2.workflow_id = agg.workflow_id
				ORDER BY j2.updated_at DESC, j2.id DESC
				LIMIT 1
			) AS last_run_at,
			(
				SELECT status
				FROM jobs j2
				WHERE j2.workflow_id = agg.workflow_id
				ORDER BY j2.updated_at DESC, j2.id DESC
				LIMIT 1
			) AS last_status
		FROM agg
	`, placeholders)
	return query, args
}

// computeWorkflowStats computes statistics for a workflow definition.
func (s *Server) computeWorkflowStats(wf *storage.WorkflowDefinition) WorkflowWithStats {
	stats := parseWorkflowDefinitionStats(wf)
	execStatsByWorkflow, err := s.loadWorkflowExecutionStats([]string{wf.ID})
	if err != nil {
		log.Printf("Error querying execution stats for workflow %s: %v", wf.ID, err)
		return stats
	}
	if execStats, ok := execStatsByWorkflow[wf.ID]; ok {
		applyWorkflowExecutionStats(&stats, execStats)
	}
	return stats
}

// getFrontendURL returns the frontend URL from environment or default
func getFrontendURL() string {
	return appenv.FrontendURL()
}

func (s *Server) handleWorkflows(w http.ResponseWriter, r *http.Request) {
	// Fetch workflow definitions from storage
	limit := parseIntDefault(r.URL.Query().Get("limit"), 100)
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	workflows, err := s.storage.ListWorkflows(limit)
	if err != nil {
		log.Printf("Error listing workflows: %v", err)
		writeJSONError(w, "Failed to list workflows", http.StatusInternalServerError)
		return
	}

	workflowIDs := make([]string, 0, len(workflows))
	for _, wf := range workflows {
		workflowIDs = append(workflowIDs, wf.ID)
	}
	execStatsByWorkflow, err := s.loadWorkflowExecutionStats(workflowIDs)
	if err != nil {
		log.Printf("Error loading workflow execution stats: %v", err)
		writeJSONError(w, "Failed to load workflow stats", http.StatusInternalServerError)
		return
	}

	// Compute stats for each workflow
	workflowsWithStats := make([]WorkflowWithStats, 0, len(workflows))
	var totalExecutions int
	var activeWorkflows int
	sevenDaysAgo := time.Now().AddDate(0, 0, -7)

	for _, wf := range workflows {
		stats := parseWorkflowDefinitionStats(&wf)
		if execStats, ok := execStatsByWorkflow[wf.ID]; ok {
			applyWorkflowExecutionStats(&stats, execStats)
		}
		workflowsWithStats = append(workflowsWithStats, stats)

		totalExecutions += stats.ExecutionCount
		if stats.LastRunAt != nil && stats.LastRunAt.After(sevenDaysAgo) {
			activeWorkflows++
		}
	}

	data := map[string]interface{}{
		"Workflows":       workflowsWithStats,
		"TotalWorkflows":  len(workflows),
		"ActiveWorkflows": activeWorkflows,
		"TotalExecutions": totalExecutions,
		"FrontendURL":     getFrontendURL(),
	}

	writeJSONResponse(w, data)
}

func (s *Server) handleWorkflowExport(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workflowID := vars["id"]

	wf, err := s.storage.GetWorkflow(workflowID)
	if err != nil {
		writeJSONError(w, "Workflow not found", http.StatusNotFound)
		return
	}

	// Return the raw workflow file format (same as core API GET /api/workflows/{id}).
	var workflowFile map[string]interface{}
	if err := json.Unmarshal([]byte(wf.Definition), &workflowFile); err != nil {
		writeJSONError(w, "Failed to parse workflow definition", http.StatusInternalServerError)
		return
	}

	writeJSONResponse(w, workflowFile)
}

func (s *Server) handleWorkflowUpdate(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workflowID := vars["id"]

	var workflowFile map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&workflowFile); err != nil {
		writeJSONError(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	// Ensure ID matches URL.
	workflowFile["id"] = workflowID

	// Normalize IDs to match core workflow update semantics.
	if err := workflow.NormalizeWorkflowFileIDs(workflowFile); err != nil {
		writeJSONError(w, "Failed to normalize IDs", http.StatusInternalServerError)
		return
	}

	name, _ := workflowFile["name"].(string)
	if name == "" {
		name = "Untitled Workflow"
	}
	description, _ := workflowFile["description"].(string)

	definitionJSON, err := json.Marshal(workflowFile)
	if err != nil {
		writeJSONError(w, "Failed to marshal workflow", http.StatusInternalServerError)
		return
	}

	wfDef := &storage.WorkflowDefinition{
		ID:          workflowID,
		Name:        name,
		Description: description,
		Definition:  string(definitionJSON),
	}

	if err := s.storage.UpdateWorkflow(wfDef); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeJSONError(w, "Workflow not found", http.StatusNotFound)
			return
		}
		writeJSONError(w, fmt.Sprintf("Failed to update workflow: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSONResponse(w, map[string]interface{}{
		"id":      workflowID,
		"message": "Workflow updated successfully",
	})
}

func (s *Server) handleWorkflowDetail(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workflowID := vars["id"]

	wf, err := s.storage.GetWorkflow(workflowID)
	if err != nil {
		writeJSONError(w, "Workflow not found", http.StatusNotFound)
		return
	}

	stats := s.computeWorkflowStats(wf)

	// Parse the definition for display
	var wfDef workflow.Workflow
	if err := json.Unmarshal([]byte(wf.Definition), &wfDef); err != nil {
		log.Printf("Error parsing workflow definition: %v", err)
	}

	// Get recent executions
	rows, err := s.db.Query(`
		SELECT id, query, status, cost, tokens_total, created_at, updated_at
		FROM jobs
		WHERE workflow_id = ?
		ORDER BY created_at DESC
		LIMIT 10
	`, workflowID)
	if err != nil {
		log.Printf("Error querying recent executions: %v", err)
	}

	var recentExecutions []storage.Job
	if rows != nil {
		defer func() {
			if err := rows.Close(); err != nil {
				log.Printf("Error closing rows: %v", err)
			}
		}()

		for rows.Next() {
			var j storage.Job
			if err := rows.Scan(&j.ID, &j.Description, &j.Status, &j.Cost, &j.TokensTotal, &j.CreatedAt, &j.UpdatedAt); err != nil {
				log.Printf("Error scanning job row: %v", err)
				continue
			}
			j.WorkflowID = workflowID
			recentExecutions = append(recentExecutions, j)
		}
	}

	data := map[string]interface{}{
		"Workflow":         stats,
		"Definition":       wfDef,
		"RecentExecutions": recentExecutions,
		"DefinitionJSON":   wf.Definition,
		"FrontendURL":      getFrontendURL(),
	}

	writeJSONResponse(w, data)
}
