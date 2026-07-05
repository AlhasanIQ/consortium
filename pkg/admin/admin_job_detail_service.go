package admin

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
)

// nodeMetricsResult holds the output of buildNodesWithMetrics.
type nodeMetricsResult struct {
	nodeExecutions    []storage.WorkflowNode
	nodesWithMetrics  []NodeWithMetrics
	perfMetrics       PerformanceMetrics
	earliestNodeStart time.Time
	nodeDisplayOrder  map[string]string
	nodeDisplayName   map[string]string
	nodeRenderOrder   map[string]int
	nodeTimelineStart map[string]time.Time
}

// attemptProcessingResult holds the output of processAttemptData.
type attemptProcessingResult struct {
	logs                       []storage.NodeExecutionAttempt
	attemptBars                []AttemptWithMetrics
	attemptIntervals           []timelineInterval
	attemptIntervalsByNode     map[string][]timelineInterval
	attemptCountByNode         map[string]int
	earliestAttemptStartByNode map[string]time.Time
	earliestAttemptStart       time.Time
	retriedNodeCount           int
	totalRetryAttempts         int
}

// buildJobDetailData assembles the payload returned by /api/admin/jobs/{id}.
// It intentionally keeps transport concerns (HTTP status/serialization) out of
// this path so the detail-building logic can evolve and be tested independently.
func (s *Server) buildJobDetailData(ctx context.Context, jobID string) (*JobDetailPayload, error) {
	job, err := s.storage.GetExecution(jobID)
	if err != nil {
		return nil, err
	}
	workflowContext := extractWorkflowContext(job.RequestData)
	snapshot := extractFromSnapshot(job.DAGSnapshot)

	jobStartMs, totalDurationMs, lifecycle := initializeJobLifecycle(job)

	runID := strings.TrimSpace(job.RunID)
	if runID == "" {
		runID = job.ID
	}
	if historySummary, historyErr := s.loadRunHistorySummary(runID); historyErr != nil {
		log.Printf("Warning: Failed to load history summary for run %s: %v", runID, historyErr)
	} else {
		lifecycle.HistoryEventCount = historySummary.EventCount
		lifecycle.ExecutionStartedAt = historySummary.WorkflowStartedAt
		lifecycle.FirstActivityStartedAt = historySummary.FirstActivityStartedAt
		lifecycle.LastHistoryEventAt = historySummary.LastEventAt
		if historySummary.WorkflowCompletedAt != nil && historySummary.WorkflowCompletedAt.After(lifecycle.SubmittedAt) {
			lifecycle.CompletedAt = *historySummary.WorkflowCompletedAt
			totalDurationMs = float64(lifecycle.CompletedAt.UnixMilli()) - jobStartMs
			if totalDurationMs <= 0 {
				totalDurationMs = 1
			}
			lifecycle.EndToEndDurationMs = totalDurationMs
		}
	}

	// Load and compute per-node metrics.
	nm := buildNodesWithMetrics(s.storage, jobID, workflowContext, snapshot, jobStartMs, totalDurationMs, job)

	// Load and process execution attempt data.
	var ap attemptProcessingResult
	if execLogs, err := s.storage.GetNodeExecutionAttempts(jobID); err == nil {
		ap = processAttemptData(execLogs, job.Status, nm, jobStartMs, totalDurationMs)
		nm.perfMetrics.NodesRetried = ap.retriedNodeCount
		nm.perfMetrics.TotalRetryAttempts = ap.totalRetryAttempts
		nm.perfMetrics.TotalNodes = len(nm.nodeExecutions)
	} else {
		log.Printf("Warning: Failed to fetch execution logs for job %s: %v", jobID, err)
	}

	// Assign attempt bars to their parent nodes and compute active/wait durations.
	var orphanAttemptBars []AttemptWithMetrics
	if len(ap.attemptBars) > 0 {
		orphanAttemptBars = assignAttemptsToNodes(nm.nodesWithMetrics, ap, nm.nodeRenderOrder, nm.nodeTimelineStart)
		computeNodeActiveDurations(nm.nodesWithMetrics, ap.attemptIntervalsByNode)
	}

	finalizeLifecycleTiming(&lifecycle, jobStartMs, totalDurationMs, ap.earliestAttemptStart, nm.earliestNodeStart, ap.attemptIntervals, nm.perfMetrics.TotalLatencyMs)

	// Fetch child jobs, compute inline timelines, and cost summaries.
	childJobs, childJobMetrics, childCostSummaries, jobCostSummary := s.loadChildJobData(
		jobID, job, nm.nodesWithMetrics, jobStartMs, totalDurationMs,
	)
	agentRunRows, err := s.storage.ListAgentRunsByJob(ctx, jobID)
	if err != nil {
		log.Printf("Warning: Failed to fetch agent runs for job %s: %v", jobID, err)
	}
	agentRuns := enrichAgentRunsWithNovomoURLs(agentRunRows, novomoFrontendV2URL())

	data := buildJobDetailPayload(JobDetailComponents{
		Job:                job,
		Lifecycle:          lifecycle,
		JobCostSummary:     jobCostSummary,
		NodeExecutions:     nm.nodeExecutions,
		NodesWithMetrics:   nm.nodesWithMetrics,
		PerfMetrics:        nm.perfMetrics,
		Logs:               ap.logs,
		AttemptBars:        ap.attemptBars,
		OrphanAttemptBars:  orphanAttemptBars,
		AgentRuns:          agentRuns,
		ChildJobs:          childJobs,
		ChildJobMetrics:    childJobMetrics,
		ChildCostSummaries: childCostSummaries,
	})
	if strings.TrimSpace(job.WorkflowID) != "" {
		if _, err := s.storage.GetWorkflow(job.WorkflowID); err == nil {
			data.WorkflowSaved = true
		} else if !errors.Is(err, storage.ErrNotFound) {
			return nil, fmt.Errorf("check workflow %q: %w", job.WorkflowID, err)
		}
	}

	attachBenchmarkAndParentLinks(s, data, jobID, job)

	return data, nil
}

// buildNodesWithMetrics fetches workflow nodes for a job and computes per-node
// timeline metrics, display ordering, and aggregate performance stats.
func buildNodesWithMetrics(
	store *storage.Storage,
	jobID string,
	workflowContext map[string]interface{},
	snapshot snapshotExtract,
	jobStartMs, totalDurationMs float64,
	job *storage.Job,
) nodeMetricsResult {
	result := nodeMetricsResult{
		nodeDisplayOrder:  make(map[string]string),
		nodeDisplayName:   make(map[string]string),
		nodeRenderOrder:   make(map[string]int),
		nodeTimelineStart: make(map[string]time.Time),
	}

	nodes, err := store.GetWorkflowNodes(jobID)
	if err != nil {
		log.Printf("Warning: Failed to fetch workflow nodes for job %s: %v", jobID, err)
		return result
	}

	nodes, childrenOf, _, parentSet := reorderNodesParentFirst(nodes)
	result.nodeExecutions = nodes

	// Build full context: initial request vars + node outputs for prompt interpolation.
	workflowContext = buildFullWorkflowContext(workflowContext, nodes)

	// First pass: compute totals (skip parent nodes that have children — metrics are on children)
	totalLatency := 0.0
	metricsNodeCount := 0
	for _, node := range nodes {
		if parentSet[node.NodeID] {
			continue
		}
		totalLatency += node.LatencyMs
		metricsNodeCount++
	}

	// Second pass: compute metrics for each node
	cumulative := 0
	displayIndex := 0                      // Sequential index for non-child nodes
	parentDisplayIndex := map[string]int{} // nodeID -> display index (for child labels)
	childCounter := map[string]int{}       // parentNodeID -> next child number
	for _, node := range nodes {
		childJobID := applyChildWorkflowDisplayCost(&node)
		node.Prompt = workflow.InterpolateVariables(node.Prompt, workflowContext)
		nodeConfig := resolveNodeConfig(node.NodeID, node.Metadata, snapshot.Configs)
		systemPrompt := snapshot.SystemPrompts[node.NodeID]

		totalTokens := node.TokensInput + node.TokensOutput
		cumulative += totalTokens

		efficiency := 0.0
		if node.LatencyMs > 0 {
			efficiency = float64(totalTokens) * 1000.0 / node.LatencyMs
		}

		// Compute display order label (e.g. "0", "1", "2", "2.1")
		displayOrder := ""
		if node.ParentNodeID != "" {
			childCounter[node.ParentNodeID]++
			parentIdx := parentDisplayIndex[node.ParentNodeID]
			displayOrder = fmt.Sprintf("%d.%d", parentIdx, childCounter[node.ParentNodeID])
		} else {
			displayOrder = fmt.Sprintf("%d", displayIndex)
			parentDisplayIndex[node.NodeID] = displayIndex
			displayIndex++
		}

		// Calculate timeline position from wall-clock start/end with latency fallback.
		nodeStartTime, nodeDurationMs := nodeTimelineParams(node)
		// Parent aggregation nodes may have null started_at - use earliest child start.
		if node.StartedAt == nil && parentSet[node.NodeID] {
			if children, ok := childrenOf[node.NodeID]; ok && len(children) > 0 {
				earliest := children[0].CreatedAt
				for _, c := range children {
					if c.StartedAt != nil && !c.StartedAt.IsZero() && c.StartedAt.Before(earliest) {
						earliest = *c.StartedAt
					}
				}
				nodeStartTime = earliest
			}
		}
		if result.earliestNodeStart.IsZero() || nodeStartTime.Before(result.earliestNodeStart) {
			result.earliestNodeStart = nodeStartTime
		}

		startOffsetPercent, widthPercent := computeTimelineBar(nodeStartTime, nodeDurationMs, jobStartMs, totalDurationMs)
		displayName := timelineNodeDisplayName(node)
		result.nodeDisplayOrder[node.NodeID] = displayOrder
		result.nodeDisplayName[node.NodeID] = displayName
		result.nodeTimelineStart[node.NodeID] = nodeStartTime

		result.nodesWithMetrics = append(result.nodesWithMetrics, NodeWithMetrics{
			WorkflowNode:           node,
			TotalTokens:            totalTokens,
			CumulativeTokens:       cumulative,
			EfficiencyTokensPerSec: efficiency,
			WidthPercent:           widthPercent,
			StartOffsetPercent:     startOffsetPercent,
			TimelineDurationMs:     nodeDurationMs,
			IsChildNode:            node.ParentNodeID != "",
			IsParentNode:           parentSet[node.NodeID],
			DisplayOrder:           displayOrder,
			ChildJobID:             childJobID,
			NodeConfig:             nodeConfig,
			SystemPrompt:           systemPrompt,
		})
		result.nodeRenderOrder[node.NodeID] = len(result.nodesWithMetrics) - 1
	}

	// Compute performance metrics
	result.perfMetrics.TotalLatencyMs = totalLatency
	if metricsNodeCount > 0 {
		result.perfMetrics.AvgNodeLatencyMs = totalLatency / float64(metricsNodeCount)
	}
	if totalLatency > 0 {
		result.perfMetrics.ThroughputTokensPerSec = float64(job.TokensTotal) * 1000.0 / totalLatency
	}
	if job.TokensTotal > 0 {
		result.perfMetrics.CostPerToken = job.Cost / float64(job.TokensTotal)
	}

	return result
}

// processAttemptData normalizes execution attempts, computes timeline bars,
// tracks retry statistics, and collects interval data for active/wait analysis.
func processAttemptData(
	execLogs []storage.NodeExecutionAttempt,
	jobStatus string,
	nm nodeMetricsResult,
	jobStartMs, totalDurationMs float64,
) attemptProcessingResult {
	result := attemptProcessingResult{
		attemptIntervalsByNode:     make(map[string][]timelineInterval),
		attemptCountByNode:         make(map[string]int),
		earliestAttemptStartByNode: make(map[string]time.Time),
	}

	// Keep all attempts for the table and compute retry stats.
	result.logs = normalizeAttemptStatusesForDisplay(execLogs, jobStatus)
	retriedNodes := make(map[string]bool)

	for _, lg := range result.logs {
		result.attemptCountByNode[lg.NodeID]++
		if lg.Attempt > 1 {
			result.totalRetryAttempts++
			if lg.NodeID != "" {
				retriedNodes[lg.NodeID] = true
			}
		}

		attemptStart := lg.CreatedAt
		if lg.StartedAt != nil && !lg.StartedAt.IsZero() {
			attemptStart = *lg.StartedAt
		}
		if result.earliestAttemptStart.IsZero() || attemptStart.Before(result.earliestAttemptStart) {
			result.earliestAttemptStart = attemptStart
		}
		if earliest, ok := result.earliestAttemptStartByNode[lg.NodeID]; !ok || attemptStart.Before(earliest) {
			result.earliestAttemptStartByNode[lg.NodeID] = attemptStart
		}
		attemptEnd := lg.UpdatedAt
		if lg.CompletedAt != nil && !lg.CompletedAt.IsZero() {
			attemptEnd = *lg.CompletedAt
		}
		if attemptEnd.After(attemptStart) {
			interval := timelineInterval{Start: attemptStart, End: attemptEnd}
			result.attemptIntervals = append(result.attemptIntervals, interval)
			result.attemptIntervalsByNode[lg.NodeID] = append(result.attemptIntervalsByNode[lg.NodeID], interval)
		}

		attemptDurationMs := lg.LatencyMs
		if wallClockMs := durationBetweenMs(attemptStart, attemptEnd); wallClockMs > 0 {
			attemptDurationMs = wallClockMs
		}
		if attemptDurationMs <= 0 {
			attemptDurationMs = 1
		}

		startOffsetPercent, widthPercent := computeTimelineBar(attemptStart, attemptDurationMs, jobStartMs, totalDurationMs)
		order := nm.nodeDisplayOrder[lg.NodeID]
		if order == "" {
			order = "?"
		}
		name := nm.nodeDisplayName[lg.NodeID]
		if name == "" {
			name = timelineAttemptDisplayName(lg)
		}

		result.attemptBars = append(result.attemptBars, AttemptWithMetrics{
			NodeExecutionAttempt: lg,
			NodeDisplayOrder:     order,
			NodeDisplayName:      name,
			StartOffsetPercent:   startOffsetPercent,
			WidthPercent:         widthPercent,
			TimelineDurationMs:   attemptDurationMs,
		})
	}
	result.retriedNodeCount = len(retriedNodes)

	// Sort all attempt bars by timeline position.
	sort.SliceStable(result.attemptBars, func(i, j int) bool {
		iStart := timelineAttemptStart(result.attemptBars[i].NodeExecutionAttempt)
		jStart := timelineAttemptStart(result.attemptBars[j].NodeExecutionAttempt)
		if !iStart.Equal(jStart) {
			return iStart.Before(jStart)
		}

		iOrder, iKnown := nm.nodeRenderOrder[result.attemptBars[i].NodeID]
		jOrder, jKnown := nm.nodeRenderOrder[result.attemptBars[j].NodeID]
		if !iKnown {
			iOrder = 1<<30 - 1
		}
		if !jKnown {
			jOrder = 1<<30 - 1
		}
		if iOrder != jOrder {
			return iOrder < jOrder
		}
		if result.attemptBars[i].Attempt != result.attemptBars[j].Attempt {
			return result.attemptBars[i].Attempt < result.attemptBars[j].Attempt
		}
		return result.attemptBars[i].NodeID < result.attemptBars[j].NodeID
	})

	return result
}

// assignAttemptsToNodes distributes timeline attempt bars to their corresponding
// nodes (AttemptsAbove / AttemptsBelow) and returns any orphan attempt bars that
// don't match a known node.
func assignAttemptsToNodes(
	nodesWithMetrics []NodeWithMetrics,
	ap attemptProcessingResult,
	nodeRenderOrder map[string]int,
	nodeTimelineStart map[string]time.Time,
) []AttemptWithMetrics {
	// Filter timeline-only bars to retried nodes (single-attempt bars
	// duplicate the node bar on the timeline). The full sorted attemptBars
	// is preserved for the Attempts response table.
	var timelineAttemptBars []AttemptWithMetrics
	if ap.totalRetryAttempts > 0 {
		for _, ab := range ap.attemptBars {
			if ap.attemptCountByNode[ab.NodeID] > 1 {
				timelineAttemptBars = append(timelineAttemptBars, ab)
			}
		}
	}

	nodeIndexByID := make(map[string]int, len(nodesWithMetrics))
	for i := range nodesWithMetrics {
		nodeIndexByID[nodesWithMetrics[i].NodeID] = i
	}

	var orphanAttemptBars []AttemptWithMetrics
	attemptsByNode := make(map[string][]AttemptWithMetrics)
	for _, attemptBar := range timelineAttemptBars {
		if _, ok := nodeIndexByID[attemptBar.NodeID]; !ok {
			orphanAttemptBars = append(orphanAttemptBars, attemptBar)
			continue
		}
		attemptsByNode[attemptBar.NodeID] = append(attemptsByNode[attemptBar.NodeID], attemptBar)
	}

	for nodeID, nodeAttempts := range attemptsByNode {
		sort.SliceStable(nodeAttempts, func(i, j int) bool {
			iStart := timelineAttemptStart(nodeAttempts[i].NodeExecutionAttempt)
			jStart := timelineAttemptStart(nodeAttempts[j].NodeExecutionAttempt)
			if !iStart.Equal(jStart) {
				return iStart.Before(jStart)
			}
			if nodeAttempts[i].Attempt != nodeAttempts[j].Attempt {
				return nodeAttempts[i].Attempt < nodeAttempts[j].Attempt
			}
			return nodeAttempts[i].ID < nodeAttempts[j].ID
		})
		nodeIdx := nodeIndexByID[nodeID]
		nodeStart := nodeTimelineStart[nodeID]
		if earliest, ok := ap.earliestAttemptStartByNode[nodeID]; ok && (nodeStart.IsZero() || earliest.Before(nodeStart)) {
			nodeStart = earliest
		}

		for _, nodeAttempt := range nodeAttempts {
			attemptStart := timelineAttemptStart(nodeAttempt.NodeExecutionAttempt)
			if !nodeStart.IsZero() && attemptStart.Before(nodeStart) {
				nodesWithMetrics[nodeIdx].AttemptsAbove = append(nodesWithMetrics[nodeIdx].AttemptsAbove, nodeAttempt)
				continue
			}
			nodesWithMetrics[nodeIdx].AttemptsBelow = append(nodesWithMetrics[nodeIdx].AttemptsBelow, nodeAttempt)
		}
	}

	return orphanAttemptBars
}

// computeNodeActiveDurations calculates active and wait durations for each node
// by merging attempt intervals against the node's total timeline span.
func computeNodeActiveDurations(nodesWithMetrics []NodeWithMetrics, attemptIntervalsByNode map[string][]timelineInterval) {
	for i := range nodesWithMetrics {
		nodeID := nodesWithMetrics[i].NodeID
		activeMs := mergedTimelineIntervalsDurationMs(attemptIntervalsByNode[nodeID])
		if activeMs <= 0 {
			continue
		}
		if activeMs > nodesWithMetrics[i].TimelineDurationMs {
			activeMs = nodesWithMetrics[i].TimelineDurationMs
		}
		nodesWithMetrics[i].ActiveDurationMs = activeMs
		waitMs := nodesWithMetrics[i].TimelineDurationMs - activeMs
		if waitMs > 0 {
			nodesWithMetrics[i].WaitDurationMs = waitMs
		}
	}
}

// attachBenchmarkAndParentLinks resolves benchmark run and parent workflow
// references for the job detail payload.
func attachBenchmarkAndParentLinks(s *Server, data *JobDetailPayload, jobID string, job *storage.Job) {
	// Benchmark reverse-lookup: check this job, then walk up to parent.
	if benchLink, err := s.storage.GetBenchmarkRunByJobID(jobID); err != nil {
		log.Printf("Warning: benchmark lookup for job %s: %v", jobID, err)
	} else if benchLink != nil {
		data.BenchmarkRun = benchLink
	} else if job.ParentExecutionID != "" {
		if benchLink, err := s.storage.GetBenchmarkRunByJobID(job.ParentExecutionID); err != nil {
			log.Printf("Warning: benchmark lookup for parent %s: %v", job.ParentExecutionID, err)
		} else if benchLink != nil {
			data.BenchmarkRun = benchLink
		}
	}

	// Parent workflow: resolve the parent job's workflow ID for navigation.
	if job.ParentExecutionID != "" {
		if parentJob, err := s.storage.GetExecution(job.ParentExecutionID); err != nil {
			log.Printf("Warning: parent job lookup for %s: %v", job.ParentExecutionID, err)
		} else if parentJob.WorkflowID != "" {
			data.ParentWorkflowID = parentJob.WorkflowID
		}
	}
}
