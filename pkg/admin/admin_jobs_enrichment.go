package admin

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/typeconv"
)

const nodeTypeChildWorkflow = "child_workflow"

type runHistorySummary struct {
	WorkflowStartedAt      *time.Time
	FirstActivityStartedAt *time.Time
	WorkflowCompletedAt    *time.Time
	LastEventAt            *time.Time
	EventCount             int
}

type timelineInterval struct {
	Start time.Time
	End   time.Time
}

// JobDetailComponents groups the inputs needed to build a JobDetailPayload.
type JobDetailComponents struct {
	Job                *storage.Job
	Lifecycle          JobLifecycleMetrics
	JobCostSummary     ExecutionCostSummary
	NodeExecutions     []storage.WorkflowNode
	NodesWithMetrics   []NodeWithMetrics
	PerfMetrics        PerformanceMetrics
	Logs               []storage.NodeExecutionAttempt
	AttemptBars        []AttemptWithMetrics
	OrphanAttemptBars  []AttemptWithMetrics
	AgentRuns          []AgentRunSummary
	ChildJobs          []storage.WorkflowExecution
	ChildJobMetrics    map[string][]NodeWithMetrics
	ChildCostSummaries map[string]ExecutionCostSummary
}

// JobDetailPayload is the typed response for /api/admin/jobs/{id}.
type JobDetailPayload struct {
	Job                *storage.Job                    `json:"Job"`
	Lifecycle          JobLifecycleMetrics             `json:"Lifecycle"`
	JobCostSummary     ExecutionCostSummary            `json:"JobCostSummary"`
	NodeExecutions     []storage.WorkflowNode          `json:"NodeExecutions"`
	NodesWithMetrics   []NodeWithMetrics               `json:"NodesWithMetrics"`
	PerfMetrics        PerformanceMetrics              `json:"PerfMetrics"`
	Logs               []storage.NodeExecutionAttempt  `json:"Logs"`
	Attempts           []AttemptWithMetrics            `json:"Attempts"`
	OrphanAttempts     []AttemptWithMetrics            `json:"OrphanAttempts"`
	AgentRuns          []AgentRunSummary               `json:"AgentRuns"`
	InputPrompt        string                          `json:"InputPrompt"`
	ChildJobs          []storage.WorkflowExecution     `json:"ChildJobs"`
	ChildJobMetrics    map[string][]NodeWithMetrics    `json:"ChildJobMetrics"`
	ChildCostSummaries map[string]ExecutionCostSummary `json:"ChildCostSummaries"`
	BenchmarkRun       *storage.BenchmarkRunLink       `json:"BenchmarkRun,omitempty"`
	ParentWorkflowID   string                          `json:"ParentWorkflowID,omitempty"`
	WorkflowSaved      bool                            `json:"WorkflowSaved"`
}

func buildJobDetailPayload(c JobDetailComponents) *JobDetailPayload {
	return &JobDetailPayload{
		Job:                c.Job,
		Lifecycle:          c.Lifecycle,
		JobCostSummary:     c.JobCostSummary,
		NodeExecutions:     c.NodeExecutions,
		NodesWithMetrics:   c.NodesWithMetrics,
		PerfMetrics:        c.PerfMetrics,
		Logs:               c.Logs,
		Attempts:           c.AttemptBars,
		OrphanAttempts:     c.OrphanAttemptBars,
		AgentRuns:          c.AgentRuns,
		InputPrompt:        extractInputPrompt(c.Job.RequestData),
		ChildJobs:          c.ChildJobs,
		ChildJobMetrics:    c.ChildJobMetrics,
		ChildCostSummaries: c.ChildCostSummaries,
	}
}

// applyChildWorkflowDisplayCost updates node.Cost for display when a child_workflow
// node carries child_total_cost in metadata. It also returns child_job_id if present.
func applyChildWorkflowDisplayCost(node *storage.WorkflowNode) string {
	if node == nil || node.Metadata == "" {
		return ""
	}

	var meta map[string]interface{}
	if err := json.Unmarshal([]byte(node.Metadata), &meta); err != nil {
		return ""
	}

	var childJobID string
	if cid, ok := meta["child_job_id"].(string); ok {
		childJobID = cid
	}

	if node.NodeType != nodeTypeChildWorkflow {
		return childJobID
	}

	rawCost, ok := meta["child_total_cost"]
	if !ok {
		return childJobID
	}

	if childCost, ok := typeconv.AsFloat64(rawCost); ok {
		node.Cost = childCost
	}

	return childJobID
}

func initializeJobLifecycle(job *storage.Job) (jobStartMs, totalDurationMs float64, lifecycle JobLifecycleMetrics) {
	jobStartMs = float64(job.CreatedAt.UnixMilli())
	jobEndMs := float64(job.UpdatedAt.UnixMilli())
	totalDurationMs = jobEndMs - jobStartMs
	if totalDurationMs <= 0 {
		totalDurationMs = 1 // Prevent division by zero
	}

	lifecycle = JobLifecycleMetrics{
		SubmittedAt:           job.CreatedAt,
		CompletedAt:           job.UpdatedAt,
		EndToEndDurationMs:    totalDurationMs,
		ExecutionDurationMs:   totalDurationMs,
		ExecutionWidthPercent: 100,
	}
	return jobStartMs, totalDurationMs, lifecycle
}

// computeNodeMetrics builds NodeWithMetrics for a job's nodes (used for child job inline timelines).
func computeNodeMetrics(job storage.WorkflowExecution, nodes []storage.WorkflowNode) []NodeWithMetrics {
	jobStartMs := float64(job.CreatedAt.UnixMilli())
	jobEndMs := float64(job.UpdatedAt.UnixMilli())
	totalDurationMs := jobEndMs - jobStartMs
	if totalDurationMs <= 0 {
		totalDurationMs = 1
	}

	parentSet := make(map[string]bool)
	for _, node := range nodes {
		if node.ParentNodeID != "" {
			parentSet[node.ParentNodeID] = true
		}
	}

	var result []NodeWithMetrics
	for i, node := range nodes {
		applyChildWorkflowDisplayCost(&node)

		totalTokens := node.TokensInput + node.TokensOutput
		efficiency := 0.0
		if node.LatencyMs > 0 {
			efficiency = float64(totalTokens) * 1000.0 / node.LatencyMs
		}

		nodeStartTime, nodeDurationMs := nodeTimelineParams(node)
		startOffsetPercent, widthPercent := computeTimelineBar(nodeStartTime, nodeDurationMs, jobStartMs, totalDurationMs)

		result = append(result, NodeWithMetrics{
			WorkflowNode:           node,
			TotalTokens:            totalTokens,
			EfficiencyTokensPerSec: efficiency,
			WidthPercent:           widthPercent,
			StartOffsetPercent:     startOffsetPercent,
			TimelineDurationMs:     nodeDurationMs,
			IsParentNode:           parentSet[node.NodeID],
			DisplayOrder:           fmt.Sprintf("%d", i),
		})
	}
	return result
}

func computeTimelineBar(start time.Time, durationMs, jobStartMs, totalDurationMs float64) (float64, float64) {
	startOffsetPercent := 0.0
	if !start.IsZero() {
		startOffsetPercent = ((float64(start.UnixMilli()) - jobStartMs) / totalDurationMs) * 100.0
	}
	if startOffsetPercent < 0 {
		startOffsetPercent = 0
	}
	if startOffsetPercent > 100 {
		startOffsetPercent = 100
	}

	widthPercent := (durationMs / totalDurationMs) * 100.0
	if widthPercent < 0.5 {
		widthPercent = 0.5 // Minimum visibility
	}

	return startOffsetPercent, widthPercent
}

// nodeTimelineParams computes the effective start time and duration for a node's
// timeline bar, preferring wall-clock times when available.
func nodeTimelineParams(node storage.WorkflowNode) (startTime time.Time, durationMs float64) {
	startTime = node.CreatedAt
	if node.StartedAt != nil && !node.StartedAt.IsZero() {
		startTime = *node.StartedAt
	}
	durationMs = node.LatencyMs
	if node.CompletedAt != nil && !node.CompletedAt.IsZero() {
		if wallClockMs := durationBetweenMs(startTime, *node.CompletedAt); wallClockMs > 0 {
			durationMs = wallClockMs
		}
	}
	if durationMs <= 0 {
		durationMs = 1
	}
	return startTime, durationMs
}

func durationBetweenMs(start, end time.Time) float64 {
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return 0
	}
	return float64(end.Sub(start)) / float64(time.Millisecond)
}

func timelineNodeDisplayName(node storage.WorkflowNode) string {
	if node.NodeLabel != "" {
		return node.NodeLabel
	}
	if node.NodeName != "" {
		return node.NodeName
	}
	if node.NodeType != "" {
		return node.NodeType
	}
	return node.NodeID
}

func timelineAttemptDisplayName(attempt storage.NodeExecutionAttempt) string {
	if attempt.NodeType != "" {
		return attempt.NodeType
	}
	if attempt.NodeID != "" {
		return attempt.NodeID
	}
	return "node"
}

func timelineAttemptStart(attempt storage.NodeExecutionAttempt) time.Time {
	if attempt.StartedAt != nil && !attempt.StartedAt.IsZero() {
		return *attempt.StartedAt
	}
	return attempt.CreatedAt
}

func normalizeAttemptStatusesForDisplay(attempts []storage.NodeExecutionAttempt, jobStatus string) []storage.NodeExecutionAttempt {
	if len(attempts) == 0 {
		return attempts
	}

	normalized := make([]storage.NodeExecutionAttempt, len(attempts))
	copy(normalized, attempts)

	indicesByNode := make(map[string][]int)
	for i := range normalized {
		indicesByNode[normalized[i].NodeID] = append(indicesByNode[normalized[i].NodeID], i)
	}

	terminalJob := isTerminalExecutionStatus(jobStatus)
	for _, idxs := range indicesByNode {
		if len(idxs) == 0 {
			continue
		}
		for pos, idx := range idxs {
			current := &normalized[idx]
			if current.Status != "running" {
				continue
			}

			if pos < len(idxs)-1 {
				current.Status = "retrying"
				next := normalized[idxs[pos+1]]
				nextStart := next.CreatedAt
				if next.StartedAt != nil && !next.StartedAt.IsZero() {
					nextStart = *next.StartedAt
				}
				closeAttemptForDisplay(current, nextStart)
				continue
			}

			if terminalJob {
				current.Status = "interrupted"
				closeAttemptForDisplay(current, current.UpdatedAt)
			}
		}
	}

	return normalized
}

func closeAttemptForDisplay(attempt *storage.NodeExecutionAttempt, endHint time.Time) {
	if attempt == nil {
		return
	}
	if attempt.CompletedAt != nil && !attempt.CompletedAt.IsZero() {
		return
	}

	end := endHint
	if end.IsZero() {
		end = attempt.UpdatedAt
	}
	if end.IsZero() {
		return
	}

	attempt.CompletedAt = &end
	attempt.UpdatedAt = end
	if attempt.StartedAt != nil && !attempt.StartedAt.IsZero() && end.After(*attempt.StartedAt) {
		attempt.LatencyMs = durationBetweenMs(*attempt.StartedAt, end)
	}
}

func isTerminalExecutionStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "failed", "cancelled", "archived":
		return true
	default:
		return false
	}
}

func mergedTimelineIntervalsDurationMs(intervals []timelineInterval) float64 {
	if len(intervals) == 0 {
		return 0
	}
	valid := make([]timelineInterval, 0, len(intervals))
	for _, interval := range intervals {
		if interval.Start.IsZero() || interval.End.IsZero() || !interval.End.After(interval.Start) {
			continue
		}
		valid = append(valid, interval)
	}
	if len(valid) == 0 {
		return 0
	}
	sort.Slice(valid, func(i, j int) bool {
		if valid[i].Start.Equal(valid[j].Start) {
			return valid[i].End.Before(valid[j].End)
		}
		return valid[i].Start.Before(valid[j].Start)
	})

	total := time.Duration(0)
	current := valid[0]
	for i := 1; i < len(valid); i++ {
		next := valid[i]
		if next.Start.After(current.End) {
			total += current.End.Sub(current.Start)
			current = next
			continue
		}
		if next.End.After(current.End) {
			current.End = next.End
		}
	}
	total += current.End.Sub(current.Start)
	return float64(total) / float64(time.Millisecond)
}

func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// finalizeLifecycleTiming computes queue delay, execution duration, active/idle
// breakdowns, and timeline percentages on a JobLifecycleMetrics.
func finalizeLifecycleTiming(lifecycle *JobLifecycleMetrics, jobStartMs, totalDurationMs float64, earliestAttemptStart, earliestNodeStart time.Time, attemptIntervals []timelineInterval, totalLatencyMs float64) {
	if lifecycle.ExecutionStartedAt == nil {
		if !earliestAttemptStart.IsZero() {
			start := earliestAttemptStart
			lifecycle.ExecutionStartedAt = &start
		} else if !earliestNodeStart.IsZero() {
			start := earliestNodeStart
			lifecycle.ExecutionStartedAt = &start
		}
	}
	if lifecycle.ExecutionStartedAt == nil && lifecycle.FirstActivityStartedAt != nil {
		start := *lifecycle.FirstActivityStartedAt
		lifecycle.ExecutionStartedAt = &start
	}

	if lifecycle.ExecutionStartedAt != nil && lifecycle.ExecutionStartedAt.Before(lifecycle.SubmittedAt) {
		start := lifecycle.SubmittedAt
		lifecycle.ExecutionStartedAt = &start
	}

	if lifecycle.ExecutionStartedAt != nil {
		lifecycle.QueueDelayMs = durationBetweenMs(lifecycle.SubmittedAt, *lifecycle.ExecutionStartedAt)
		lifecycle.ExecutionDurationMs = durationBetweenMs(*lifecycle.ExecutionStartedAt, lifecycle.CompletedAt)
		if lifecycle.ExecutionDurationMs <= 0 {
			lifecycle.ExecutionDurationMs = maxFloat(0, lifecycle.EndToEndDurationMs-lifecycle.QueueDelayMs)
		}
		lifecycle.ExecutionStartOffsetPercent = clampPercent((float64(lifecycle.ExecutionStartedAt.UnixMilli()) - jobStartMs) / totalDurationMs * 100)
		lifecycle.QueueWidthPercent = lifecycle.ExecutionStartOffsetPercent
		lifecycle.ExecutionWidthPercent = 100 - lifecycle.ExecutionStartOffsetPercent
	} else {
		lifecycle.ExecutionDurationMs = lifecycle.EndToEndDurationMs
		lifecycle.ExecutionWidthPercent = 100
	}

	lifecycle.ActiveAttemptDurationMs = mergedTimelineIntervalsDurationMs(attemptIntervals)
	if lifecycle.ActiveAttemptDurationMs <= 0 && totalLatencyMs > 0 {
		lifecycle.ActiveAttemptDurationMs = totalLatencyMs
	}
	if lifecycle.ExecutionDurationMs > 0 && lifecycle.ActiveAttemptDurationMs > lifecycle.ExecutionDurationMs {
		lifecycle.ActiveAttemptDurationMs = lifecycle.ExecutionDurationMs
	}
	if lifecycle.ExecutionDurationMs > lifecycle.ActiveAttemptDurationMs {
		lifecycle.IdleDurationMs = lifecycle.ExecutionDurationMs - lifecycle.ActiveAttemptDurationMs
	}
}

// reorderNodesParentFirst reorders nodes so children appear immediately after
// their parent. Also returns maps for downstream use: childrenOf (parent->children),
// childSet (node IDs that are children), parentSet (node IDs that have children).
func reorderNodesParentFirst(nodes []storage.WorkflowNode) (
	ordered []storage.WorkflowNode,
	childrenOf map[string][]storage.WorkflowNode,
	childSet map[string]bool,
	parentSet map[string]bool,
) {
	childrenOf = make(map[string][]storage.WorkflowNode)
	childSet = make(map[string]bool)
	for _, node := range nodes {
		if node.ParentNodeID != "" {
			childrenOf[node.ParentNodeID] = append(childrenOf[node.ParentNodeID], node)
			childSet[node.NodeID] = true
		}
	}
	parentSet = make(map[string]bool)
	for _, node := range nodes {
		if _, ok := childrenOf[node.NodeID]; ok {
			parentSet[node.NodeID] = true
		}
	}

	ordered = make([]storage.WorkflowNode, 0, len(nodes))
	for _, node := range nodes {
		if childSet[node.NodeID] {
			continue // Skip children; they'll be inserted after their parent
		}
		ordered = append(ordered, node)
		if children, ok := childrenOf[node.NodeID]; ok {
			ordered = append(ordered, children...)
		}
	}
	return ordered, childrenOf, childSet, parentSet
}
