package admin

import (
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/alhasaniq/consortium/pkg/storage"
)

func (s *Server) loadRunHistorySummary(runID string) (runHistorySummary, error) {
	summary := runHistorySummary{}
	var workflowStartedRaw sql.NullString
	var firstActivityStartedRaw sql.NullString
	var workflowCompletedRaw sql.NullString
	var lastEventRaw sql.NullString
	var eventCount int

	err := s.db.QueryRow(`
		SELECT
			MIN(CASE WHEN event_type = 'workflow_started' THEN timestamp END) AS workflow_started_at,
			MIN(CASE WHEN event_type = 'activity_started' THEN timestamp END) AS first_activity_started_at,
			MAX(CASE WHEN event_type = 'workflow_completed' THEN timestamp END) AS workflow_completed_at,
			MAX(timestamp) AS last_event_at,
			COUNT(*) AS event_count
		FROM execution_history
		WHERE run_id = ?
	`, runID).Scan(&workflowStartedRaw, &firstActivityStartedRaw, &workflowCompletedRaw, &lastEventRaw, &eventCount)
	if err != nil {
		return summary, fmt.Errorf("query run history summary for run_id=%s: %w", runID, err)
	}

	if workflowStartedRaw.Valid {
		summary.WorkflowStartedAt = parseNullableAdminTimestamp(workflowStartedRaw.String)
	}
	if firstActivityStartedRaw.Valid {
		summary.FirstActivityStartedAt = parseNullableAdminTimestamp(firstActivityStartedRaw.String)
	}
	if workflowCompletedRaw.Valid {
		summary.WorkflowCompletedAt = parseNullableAdminTimestamp(workflowCompletedRaw.String)
	}
	if lastEventRaw.Valid {
		summary.LastEventAt = parseNullableAdminTimestamp(lastEventRaw.String)
	}
	summary.EventCount = eventCount
	return summary, nil
}

func (s *Server) loadExecutionCostSummaries(executionIDs []string) (map[string]ExecutionCostSummary, error) {
	summaries := make(map[string]ExecutionCostSummary)
	if len(executionIDs) == 0 {
		return summaries, nil
	}

	seen := make(map[string]struct{}, len(executionIDs))
	ids := make([]string, 0, len(executionIDs))
	for _, id := range executionIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return summaries, nil
	}

	const maxIDsPerChunk = 300
	for start := 0; start < len(ids); start += maxIDsPerChunk {
		end := start + maxIDsPerChunk
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]

		query, args := buildExecutionCostSummariesQuery(chunk)

		rows, err := s.db.Query(query, args...)
		if err != nil {
			return nil, fmt.Errorf("query execution usage summaries: %w", err)
		}

		for rows.Next() {
			var rootID string
			var summary ExecutionCostSummary
			if err := rows.Scan(
				&rootID,
				&summary.DirectTokens,
				&summary.ChildTokens,
				&summary.TotalTokens,
				&summary.DirectCost,
				&summary.ChildCost,
				&summary.TotalCost,
				&summary.DirectLatencyMs,
				&summary.ChildLatencyMs,
				&summary.TotalLatencyMs,
				&summary.DescendantCount,
			); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("scan execution usage summary: %w", err)
			}
			summaries[rootID] = summary
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("iterate execution usage summary rows: %w", err)
		}
		_ = rows.Close()
	}

	for _, id := range ids {
		if _, ok := summaries[id]; !ok {
			summaries[id] = ExecutionCostSummary{}
		}
	}

	return summaries, nil
}

func buildExecutionCostSummariesQuery(executionIDs []string) (string, []interface{}) {
	values := make([]string, len(executionIDs))
	args := make([]interface{}, len(executionIDs))
	for i, id := range executionIDs {
		values[i] = "(?)"
		args[i] = id
	}

	query := fmt.Sprintf(`
		WITH RECURSIVE seed(id) AS (
			VALUES %s
		),
		tree(root_id, id, tokens_total, cost, depth) AS (
			SELECT j.id, j.id, COALESCE(j.tokens_total, 0), COALESCE(j.cost, 0), 0
			FROM jobs j
			JOIN seed s ON s.id = j.id
			UNION ALL
			SELECT tree.root_id, child.id, COALESCE(child.tokens_total, 0), COALESCE(child.cost, 0), tree.depth + 1
			FROM jobs child
			JOIN tree ON child.parent_execution_id = tree.id
			WHERE tree.depth < 32
		),
		tree_jobs(id) AS (
			SELECT DISTINCT id
			FROM tree
		),
		attempt_latency(job_id, latency_ms) AS (
			SELECT tj.id, COALESCE(SUM(a.latency_ms), 0)
			FROM tree_jobs tj
			CROSS JOIN workflow_node_execution_attempts a INDEXED BY idx_workflow_node_execution_attempts_job_node_attempt
			WHERE a.job_id = tj.id
			GROUP BY tj.id
		),
		tree_usage(root_id, id, tokens_total, cost, latency_ms) AS (
			SELECT tree.root_id,
			       tree.id,
			       tree.tokens_total,
			       tree.cost,
			       COALESCE(attempt_latency.latency_ms, 0) AS latency_ms
			FROM tree
			LEFT JOIN attempt_latency ON attempt_latency.job_id = tree.id
		)
		SELECT root_id,
		       COALESCE(SUM(CASE WHEN id = root_id THEN tokens_total ELSE 0 END), 0) AS direct_tokens,
		       COALESCE(SUM(CASE WHEN id != root_id THEN tokens_total ELSE 0 END), 0) AS child_tokens,
		       COALESCE(SUM(tokens_total), 0) AS total_tokens,
		       COALESCE(SUM(CASE WHEN id = root_id THEN cost ELSE 0 END), 0) AS direct_cost,
		       COALESCE(SUM(CASE WHEN id != root_id THEN cost ELSE 0 END), 0) AS child_cost,
		       COALESCE(SUM(cost), 0) AS total_cost,
		       COALESCE(SUM(CASE WHEN id = root_id THEN latency_ms ELSE 0 END), 0) AS direct_latency_ms,
		       COALESCE(SUM(CASE WHEN id != root_id THEN latency_ms ELSE 0 END), 0) AS child_latency_ms,
		       COALESCE(SUM(latency_ms), 0) AS total_latency_ms,
		       COALESCE(SUM(CASE WHEN id != root_id THEN 1 ELSE 0 END), 0) AS descendant_count
		FROM tree_usage
		GROUP BY root_id
	`, strings.Join(values, ", "))
	return query, args
}

// loadChildJobData fetches child jobs, computes inline timeline metrics, attaches
// child nodes to parent nodesWithMetrics, and loads cost summaries.
func (s *Server) loadChildJobData(
	jobID string, job *storage.Job, nodesWithMetrics []NodeWithMetrics,
	jobStartMs, totalDurationMs float64,
) ([]storage.WorkflowExecution, map[string][]NodeWithMetrics, map[string]ExecutionCostSummary, ExecutionCostSummary) {
	var childJobs []storage.WorkflowExecution
	childJobMetrics := make(map[string][]NodeWithMetrics)
	if children, err := s.storage.GetChildExecutions(jobID); err == nil && len(children) > 0 {
		childJobs = children
		for _, child := range children {
			if childNodes, err := s.storage.GetWorkflowNodes(child.ID); err == nil && len(childNodes) > 0 {
				childJobMetrics[child.ID] = computeNodeMetrics(child, childNodes)
			}
		}
	}

	// Attach child job substeps to child_workflow nodes for inline display.
	if len(childJobMetrics) > 0 {
		for i := range nodesWithMetrics {
			if nodesWithMetrics[i].ChildJobID == "" {
				continue
			}
			childNodes, ok := childJobMetrics[nodesWithMetrics[i].ChildJobID]
			if !ok {
				continue
			}
			remapped := make([]NodeWithMetrics, len(childNodes))
			copy(remapped, childNodes)
			for j := range remapped {
				cn := &remapped[j]
				nodeStart := cn.CreatedAt
				if cn.StartedAt != nil && !cn.StartedAt.IsZero() {
					nodeStart = *cn.StartedAt
				}
				cn.StartOffsetPercent, cn.WidthPercent = computeTimelineBar(
					nodeStart, cn.TimelineDurationMs, jobStartMs, totalDurationMs,
				)
			}
			nodesWithMetrics[i].ChildJobNodes = remapped
		}
	}

	jobCostSummary := ExecutionCostSummary{
		DirectTokens: job.TokensTotal,
		TotalTokens:  job.TokensTotal,
		DirectCost:   job.Cost,
		TotalCost:    job.Cost,
	}
	childCostSummaries := make(map[string]ExecutionCostSummary)
	costSummaryIDs := make([]string, 0, len(childJobs)+1)
	costSummaryIDs = append(costSummaryIDs, jobID)
	for _, child := range childJobs {
		costSummaryIDs = append(costSummaryIDs, child.ID)
	}
	if costSummaries, err := s.loadExecutionCostSummaries(costSummaryIDs); err == nil {
		if summary, ok := costSummaries[jobID]; ok {
			jobCostSummary = summary
		}
		for i := range childJobs {
			summary := ExecutionCostSummary{
				DirectTokens: childJobs[i].TokensTotal,
				TotalTokens:  childJobs[i].TokensTotal,
				DirectCost:   childJobs[i].Cost,
				TotalCost:    childJobs[i].Cost,
			}
			if candidate, ok := costSummaries[childJobs[i].ID]; ok {
				summary = candidate
				childJobs[i].Cost = candidate.TotalCost
			}
			childCostSummaries[childJobs[i].ID] = summary
		}
	} else {
		log.Printf("Warning: Failed to compute execution cost summaries for job %s: %v", jobID, err)
		for _, child := range childJobs {
			childCostSummaries[child.ID] = ExecutionCostSummary{
				DirectTokens: child.TokensTotal,
				TotalTokens:  child.TokensTotal,
				DirectCost:   child.Cost,
				TotalCost:    child.Cost,
			}
		}
	}

	return childJobs, childJobMetrics, childCostSummaries, jobCostSummary
}
