package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/alhasaniq/consortium/pkg/jobs"
	"github.com/alhasaniq/consortium/pkg/novomo"
	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/gorilla/mux"
)

type Server struct {
	storage    *storage.Storage
	db         *sql.DB
	registry   *providers.Registry
	jobManager *jobs.Manager
	workdir    string

	benchmarkRunnerMu  sync.Mutex
	benchmarkRunner    *benchmarkRunnerState
	benchmarkCancel    context.CancelFunc
	benchmarkSessionID string
}

// NodeWithMetrics extends WorkflowNode with computed metrics
type NodeWithMetrics struct {
	storage.WorkflowNode
	TotalTokens            int
	CumulativeTokens       int
	EfficiencyTokensPerSec float64
	WidthPercent           float64 // Timeline bar width (% of job duration)
	StartOffsetPercent     float64 // Timeline bar start offset (% of job duration)
	TimelineDurationMs     float64 // Displayed duration for timeline bars
	ActiveDurationMs       float64 // Union of attempt active windows for this node
	WaitDurationMs         float64 // Wall-clock node span minus active attempt windows
	IsChildNode            bool    // True if this node has a parent (aggregation subnode)
	IsParentNode           bool    // True if this node has child aggregation nodes
	DisplayOrder           string  // Display label: "0", "1", "2", "2.1" (child of node 2)
	ChildJobID             string  // Child job ID extracted from metadata (for child_workflow nodes)
	NodeConfig             string  // Node config from frozen dag_snapshot (pretty JSON)
	SystemPrompt           string  // System prompt from frozen dag_snapshot
	AttemptsAbove          []AttemptWithMetrics
	AttemptsBelow          []AttemptWithMetrics
	ChildJobNodes          []NodeWithMetrics // Inline substeps from child workflow job
}

// NodeExecutionView is used by filtered workflow-node partial rendering.
type NodeExecutionView struct {
	storage.WorkflowNode
	NodeConfig string
}

// JobLifecycleMetrics captures submit/start/active/wait timing for job detail views.
type JobLifecycleMetrics struct {
	SubmittedAt                 time.Time
	ExecutionStartedAt          *time.Time
	FirstActivityStartedAt      *time.Time
	LastHistoryEventAt          *time.Time
	CompletedAt                 time.Time
	HistoryEventCount           int
	QueueDelayMs                float64
	EndToEndDurationMs          float64
	ExecutionDurationMs         float64
	ActiveAttemptDurationMs     float64
	IdleDurationMs              float64
	ExecutionStartOffsetPercent float64
	QueueWidthPercent           float64
	ExecutionWidthPercent       float64
}

// AttemptWithMetrics extends attempt telemetry with timeline bar positioning and labels.
type AttemptWithMetrics struct {
	storage.NodeExecutionAttempt
	NodeDisplayOrder   string
	NodeDisplayName    string
	StartOffsetPercent float64 // Timeline bar start offset (% of job duration)
	WidthPercent       float64 // Timeline bar width (% of job duration)
	TimelineDurationMs float64 // Displayed duration for timeline bars
}

// ExecutionCostSummary describes direct and descendant usage totals for one execution.
type ExecutionCostSummary struct {
	DirectTokens    int
	ChildTokens     int
	TotalTokens     int
	DirectCost      float64
	ChildCost       float64
	TotalCost       float64
	DirectLatencyMs float64
	ChildLatencyMs  float64
	TotalLatencyMs  float64
	DescendantCount int
}

// PerformanceMetrics holds computed performance stats
type PerformanceMetrics struct {
	TotalLatencyMs         float64
	AvgNodeLatencyMs       float64
	ThroughputTokensPerSec float64
	CostPerToken           float64
	NodesRetried           int // Number of nodes that were retried
	TotalRetryAttempts     int // Total retry attempts across all nodes
	TotalNodes             int // Total number of nodes
}

// WorkflowWithStats extends WorkflowDefinition with computed statistics
type WorkflowWithStats struct {
	storage.WorkflowDefinition
	Layer         string // L0/L1/L2/L3 derived from ID prefix, or "" if uncategorized
	NodeCount     int
	NodeTypes     map[string]int // e.g., {"prompt": 3, "conditional": 1}
	ModelsUsed    []string
	HasCostLimits bool

	AggregationSourceIDs            []string
	WorkflowRefIDs                  []string
	ChildWorkflowIDs                []string
	ReferencesL0DirectlyAsBenchmark bool

	// Execution stats from jobs table
	ExecutionCount int
	SuccessCount   int
	SuccessRate    float64
	TotalCost      float64
	TotalTokens    int64
	LastRunAt      *time.Time
	LastRunStatus  string
}

func NewServer(store *storage.Storage, db *sql.DB, registry *providers.Registry, manager *jobs.Manager, workdir string) *Server {
	s := &Server{
		storage:    store,
		db:         db,
		registry:   registry,
		jobManager: manager,
		workdir:    workdir,
	}

	if abandoned, err := s.storage.AbandonStaleRunnerSessions(); err != nil {
		log.Printf("Warning: failed to abandon stale benchmark sessions: %v", err)
	} else if abandoned > 0 {
		log.Printf("Abandoned %d stale benchmark runner sessions from previous server run", abandoned)
	}

	if failed, err := s.storage.FailStaleBenchmarkRuns(); err != nil {
		log.Printf("Warning: failed to recover stale benchmark runs: %v", err)
	} else if failed > 0 {
		log.Printf("Marked %d stale benchmark runs as failed from previous server run", failed)
	}

	return s
}

// extractInputPrompt extracts the user's input prompt from persisted workflow request data.
func extractInputPrompt(requestData string) string {
	return workflow.ExtractInputPromptFromRequestData(requestData)
}

// extractWorkflowContext returns request context values from request_data payload.
func extractWorkflowContext(requestData string) map[string]interface{} {
	return workflow.ExtractContextFromRequestData(requestData)
}

// buildFullWorkflowContext merges the initial request context with node outputs
// (keyed by node_id) to reconstruct the full variable scope that was available
// during execution. This allows renderPromptTemplate to resolve both initial
// variables like {{user_prompt}} and inter-node references like {{analyze}}.
func buildFullWorkflowContext(requestContext map[string]interface{}, nodes []storage.WorkflowNode) map[string]interface{} {
	full := make(map[string]interface{}, len(requestContext)+len(nodes))
	for k, v := range requestContext {
		full[k] = v
	}
	for _, node := range nodes {
		if node.Output != "" {
			full[node.NodeID] = node.Output
		}
	}
	return full
}

// snapshotExtract holds node configs and system prompts extracted from the frozen dag_snapshot.
type snapshotExtract struct {
	Configs       map[string]string // node_id -> pretty config JSON (excludes id, prompt, system_prompt)
	SystemPrompts map[string]string // node_id -> system_prompt text
}

// extractFromSnapshot extracts node configs and system prompts from dag_snapshot.
func extractFromSnapshot(dagSnapshot string) snapshotExtract {
	result := snapshotExtract{
		Configs:       make(map[string]string),
		SystemPrompts: make(map[string]string),
	}
	if strings.TrimSpace(dagSnapshot) == "" {
		return result
	}

	var snapshot struct {
		Nodes []map[string]interface{} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(dagSnapshot), &snapshot); err != nil || len(snapshot.Nodes) == 0 {
		return result
	}

	for _, node := range snapshot.Nodes {
		nodeID, ok := node["id"].(string)
		if !ok || strings.TrimSpace(nodeID) == "" {
			continue
		}

		if sp, ok := node["system_prompt"].(string); ok && sp != "" {
			result.SystemPrompts[nodeID] = sp
		}

		config := make(map[string]interface{}, len(node))
		for key, value := range node {
			// Prompt and system_prompt are displayed separately in node cards.
			if key == "id" || key == "prompt" || key == "system_prompt" {
				continue
			}
			config[key] = value
		}
		if len(config) == 0 {
			continue
		}

		data, err := json.MarshalIndent(config, "", "  ")
		if err != nil {
			continue
		}
		result.Configs[nodeID] = string(data)
	}

	return result
}

func resolveNodeConfig(nodeID, runtimeMetadata string, snapshotConfigs map[string]string) string {
	if cfg, ok := snapshotConfigs[nodeID]; ok && cfg != "" {
		return cfg
	}
	return prettyJSONString(runtimeMetadata)
}

func prettyJSONString(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}

	var payload interface{}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return raw
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return raw
	}
	return string(data)
}

func (s *Server) RegisterRoutes(r *mux.Router) {
	// JSON admin API routes used by the React admin panel.
	r.HandleFunc("/api/admin/overview", s.handleOverview).Methods("GET")
	r.HandleFunc("/api/admin/db-diagnostics", s.handleDBDiagnostics).Methods("GET")
	r.HandleFunc("/api/admin/jobs", s.handleJobs).Methods("GET")
	r.HandleFunc("/api/admin/jobs/{id}", s.handleJobDetail).Methods("GET")
	r.HandleFunc("/api/admin/jobs/{id}/nodes", s.handleJobNodes).Methods("GET")
	r.HandleFunc("/api/admin/workflows", s.handleWorkflows).Methods("GET")
	r.HandleFunc("/api/admin/workflows/{id}", s.handleWorkflowDetail).Methods("GET")
	r.HandleFunc("/api/admin/workflows/{id}/export", s.handleWorkflowExport).Methods("GET")
	r.HandleFunc("/api/admin/workflows/{id}", s.handleWorkflowUpdate).Methods("PUT")
	r.HandleFunc("/api/admin/api-keys", s.handleAPIKeysList).Methods("GET")
	r.HandleFunc("/api/admin/api-keys", s.handleAPIKeysCreate).Methods("POST")
	r.HandleFunc("/api/admin/api-keys/{id}", s.handleAPIKeyRevoke).Methods("DELETE")
	r.HandleFunc("/api/admin/api-usage", s.handleAPIUsageList).Methods("GET")
	r.HandleFunc("/api/admin/api-usage/export", s.handleAPIUsageExport).Methods("GET")
	r.HandleFunc("/api/admin/api-metrics", s.handleAPIMetrics).Methods("GET")
	r.HandleFunc("/api/admin/model-routes", s.handleAPIModelRoutesList).Methods("GET")
	r.HandleFunc("/api/admin/model-routes", s.handleAPIModelRouteUpsert).Methods("POST")
	r.HandleFunc("/api/admin/model-routes/{model}", s.handleAPIModelRouteUpsert).Methods("PUT")
	r.HandleFunc("/api/admin/model-routes/{model}", s.handleAPIModelRouteDelete).Methods("DELETE")
	r.HandleFunc("/api/admin/benchmarks", s.handleBenchmarks).Methods("GET")
	r.HandleFunc("/api/admin/benchmarks/compare", s.handleBenchmarkComparison).Methods("GET")
	r.HandleFunc("/api/admin/benchmarks/compare-items", s.handleBenchmarkCompareItems).Methods("GET")
	r.HandleFunc("/api/admin/benchmarks/runner-status", s.handleBenchmarkRunnerStatus).Methods("GET")
	r.HandleFunc("/api/admin/benchmarks/import", s.handleBenchmarkImport).Methods("POST")
	r.HandleFunc("/api/admin/benchmarks/run", s.handleBenchmarkRunStart).Methods("POST")
	r.HandleFunc("/api/admin/benchmarks/run/cancel", s.handleBenchmarkRunCancel).Methods("POST")
	r.HandleFunc("/api/admin/benchmarks/dataset-flags", s.handleListDatasetFlags).Methods("GET")
	r.HandleFunc("/api/admin/benchmarks/dataset-flags", s.handleCreateDatasetFlags).Methods("POST")
	r.HandleFunc("/api/admin/benchmarks/dataset-flags/{id}/resolve", s.handleResolveDatasetFlag).Methods("PATCH")
	r.HandleFunc("/api/admin/benchmarks/dataset-flags/{id}", s.handleDeleteDatasetFlag).Methods("DELETE")
	r.HandleFunc("/api/admin/benchmarks/{id}", s.handleBenchmarkDetail).Methods("GET")
	r.HandleFunc("/api/admin/benchmarks/{id}/analysis", s.handleBenchmarkWrongAnswerAnalysis).Methods("GET")
	r.HandleFunc("/api/admin/benchmarks/{id}/items", s.handleBenchmarkItemDetail).Methods("GET")
	r.HandleFunc("/api/admin/benchmarks/{id}/items/{itemID}", s.handleBenchmarkItemDetail).Methods("GET")
	r.HandleFunc("/api/admin/benchmarks/{id}/rerun-failures", s.handleBenchmarkRerunFailures).Methods("POST")
	r.HandleFunc("/api/admin/benchmarks/{id}/replay-items", s.handleBenchmarkReplayItems).Methods("POST")
	r.HandleFunc("/api/admin/optimize/runs", s.handleOptimizeRuns).Methods("GET")
	r.HandleFunc("/api/admin/optimize/runs", s.handleOptimizeRunCreate).Methods("POST")
	r.HandleFunc("/api/admin/optimize/runs/{id}", s.handleOptimizeRunDetail).Methods("GET")
	r.HandleFunc("/api/admin/optimize/runs/{id}/pause", s.handleOptimizeRunPause).Methods("POST")
	r.HandleFunc("/api/admin/optimize/runs/{id}/resume", s.handleOptimizeRunResume).Methods("POST")
	r.HandleFunc("/api/admin/optimize/runs/{id}/cancel", s.handleOptimizeRunCancel).Methods("POST")
	r.HandleFunc("/api/admin/optimize/runs/{id}/heartbeat", s.handleOptimizeRunHeartbeat).Methods("PATCH")
	r.HandleFunc("/api/admin/optimize/runs/{id}/progress", s.handleOptimizeRunProgress).Methods("PATCH")
	r.HandleFunc("/api/admin/optimize/runs/{id}/status", s.handleOptimizeRunStatusPatch).Methods("PATCH")
	r.HandleFunc("/api/admin/optimize/runs/{id}/organisms", s.handleOptimizeRunOrganisms).Methods("GET", "POST")
	r.HandleFunc("/api/admin/optimize/runs/{id}/organisms/{orgID}", s.handleOptimizeRunOrganismDetail).Methods("GET")
	r.HandleFunc("/api/admin/optimize/runs/{id}/lineage", s.handleOptimizeRunLineage).Methods("GET")
	r.HandleFunc("/api/admin/optimize/runs/{id}/learning-log", s.handleOptimizeRunLearningLog).Methods("GET", "POST")
	r.HandleFunc("/api/admin/optimize/runs/{id}/promote", s.handleOptimizeRunPromote).Methods("POST")
	r.HandleFunc("/api/admin/optimize/compare", s.handleOptimizeCompare).Methods("GET")
	r.HandleFunc("/api/admin/optimize/organisms/{orgID}", s.handleOptimizeOrganismDetail).Methods("GET")
	r.HandleFunc("/api/admin/optimize/organisms/{orgID}/fitness", s.handleOptimizeOrganismFitnessPatch).Methods("PATCH")
	r.HandleFunc("/api/admin/optimize/organisms/{orgID}/lineage", s.handleOptimizeOrganismLineage).Methods("GET")
	r.HandleFunc("/api/admin/optimize/organisms/{orgID}/param-changes", s.handleOptimizeOrganismParamChanges).Methods("GET", "POST")
	r.HandleFunc("/api/admin/optimize/organisms/{orgID}/mutation-artifacts", s.handleOptimizeOrganismMutationArtifacts).Methods("GET", "POST")
	r.HandleFunc("/api/admin/test/workflow", s.handleTestWorkflow).Methods("POST")
	r.HandleFunc("/api/admin/admission", s.handleAdmissionStatus).Methods("GET")
	r.HandleFunc("/api/admin/admission/pause", s.handleAdmissionPause).Methods("POST")
	r.HandleFunc("/api/admin/admission/resume", s.handleAdmissionResume).Methods("POST")
	r.HandleFunc("/api/admin/jobs/pause-all", s.handlePauseAllJobs).Methods("POST")
	r.HandleFunc("/api/admin/jobs/resume-all", s.handleResumeAllJobs).Methods("POST")
	r.HandleFunc("/api/admin/jobs/cancel-all", s.handleCancelAllJobs).Methods("POST")
	r.HandleFunc("/api/admin/jobs/{id}/cancel", s.handleCancelJob).Methods("POST")
	r.HandleFunc("/api/admin/jobs/{id}/resume", s.handleResumeJob).Methods("POST")
	r.HandleFunc("/api/admin/jobs/{id}/retry", s.handleRetryJob).Methods("POST")
	r.HandleFunc("/api/admin/jobs/{id}/archive", s.handleArchiveJob).Methods("POST")
	r.HandleFunc("/api/admin/jobs/{id}/unarchive", s.handleUnarchiveJob).Methods("POST")
	r.HandleFunc("/api/admin/jobs/{id}/node-execution-attempts", s.handleJobNodeExecutionAttempts).Methods("GET")
	r.HandleFunc("/api/admin/jobs/{id}/agent-runs", s.handleJobAgentRuns).Methods("GET")
	r.HandleFunc("/api/admin/jobs/{id}/agent-runs/{agentRunID}/stop", s.handleStopJobAgentRun).Methods("POST")

	// Benchloop observability
	r.HandleFunc("/api/admin/benchloop/status", s.handleBenchloopStatus).Methods("GET")
	r.HandleFunc("/api/admin/benchloop/transcript", s.handleBenchloopTranscript).Methods("GET")
	r.HandleFunc("/api/admin/benchloop/log", s.handleBenchloopLog).Methods("GET")
	r.HandleFunc("/api/admin/benchloop/memory", s.handleBenchloopMemory).Methods("GET")
	r.HandleFunc("/api/admin/benchloop/archive/{sessionID}", s.handleBenchloopArchive).Methods("GET")
}

func writeJSONError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": message}); err != nil {
		log.Printf("Error encoding JSON error response: %v", err)
	}
}

func writeJSONResponse(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("Error encoding JSON response: %v", err)
		writeJSONError(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["id"]

	if err := s.jobManager.CancelJob(jobID); err != nil {
		statusCode := http.StatusInternalServerError
		if errors.Is(err, storage.ErrNotFound) {
			statusCode = http.StatusNotFound
		} else if errors.Is(err, jobs.ErrJobStateConflict) {
			statusCode = http.StatusConflict
		}
		writeJSONError(w, err.Error(), statusCode)
		return
	}

	log.Printf("Job %s cancelled via admin panel", jobID)
	writeJSONResponse(w, map[string]string{"message": "Job cancelled"})
}

func (s *Server) handleResumeJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["id"]

	if err := s.jobManager.ResumeJob(jobID); err != nil {
		statusCode := http.StatusInternalServerError
		if errors.Is(err, storage.ErrNotFound) {
			statusCode = http.StatusNotFound
		} else if errors.Is(err, jobs.ErrJobStateConflict) {
			statusCode = http.StatusConflict
		}
		writeJSONError(w, err.Error(), statusCode)
		return
	}

	log.Printf("Job %s resumed via admin panel", jobID)
	writeJSONResponse(w, map[string]string{"message": "Job resumed"})
}

func (s *Server) handlePauseAllJobs(w http.ResponseWriter, r *http.Request) {
	pausedCount, err := s.jobManager.PauseAllPendingJobs(r.Context())
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("Paused %d pending jobs via admin panel", pausedCount)
	writeJSONResponse(w, map[string]any{
		"success":      true,
		"paused_count": pausedCount,
		"message":      "Pending jobs paused",
	})
}

func (s *Server) handleResumeAllJobs(w http.ResponseWriter, r *http.Request) {
	resumedCount, err := s.jobManager.ResumeAllPausedJobs(r.Context())
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("Resumed %d paused jobs via admin panel", resumedCount)
	writeJSONResponse(w, map[string]any{
		"success":       true,
		"resumed_count": resumedCount,
		"message":       "Paused jobs resumed",
	})
}

func (s *Server) handleCancelAllJobs(w http.ResponseWriter, r *http.Request) {
	result, err := s.jobManager.CancelAllActiveJobs(r.Context())
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf(
		"Cancel-all via admin panel: running_cancelled=%d/%d queued_cancelled=%d failed=%d",
		result.RunningCancelled, result.RunningMatched, result.QueuedCancelled, len(result.FailedJobIDs),
	)
	writeJSONResponse(w, map[string]any{
		"success":           true,
		"running_matched":   result.RunningMatched,
		"running_cancelled": result.RunningCancelled,
		"queued_cancelled":  result.QueuedCancelled,
		"failed_job_ids":    result.FailedJobIDs,
	})
}

func (s *Server) handleArchiveJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["id"]

	job, err := s.storage.GetExecution(jobID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeJSONError(w, "Job not found", http.StatusNotFound)
			return
		}
		writeJSONError(w, "Failed to fetch job", http.StatusInternalServerError)
		return
	}

	if job.Status == "archived" {
		writeJSONResponse(w, map[string]any{"success": true, "status": "already_archived"})
		return
	}

	if job.Status == "running" || job.Status == "pending" {
		writeJSONError(w, "Cannot archive a running or pending job", http.StatusConflict)
		return
	}

	if err := s.storage.ArchiveExecution(jobID); err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("Job %s archived via admin panel", jobID)
	writeJSONResponse(w, map[string]any{"success": true})
}

func (s *Server) handleUnarchiveJob(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["id"]

	job, err := s.storage.GetExecution(jobID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeJSONError(w, "Job not found", http.StatusNotFound)
			return
		}
		writeJSONError(w, "Failed to fetch job", http.StatusInternalServerError)
		return
	}

	if job.Status != "archived" {
		writeJSONError(w, "Job is not archived", http.StatusConflict)
		return
	}

	restoreStatus := "completed"
	if job.ErrorMessage != "" {
		restoreStatus = "failed"
	}

	if err := s.storage.UnarchiveExecution(jobID, restoreStatus); err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("Job %s unarchived via admin panel (restored to %s)", jobID, restoreStatus)
	writeJSONResponse(w, map[string]any{"success": true, "status": restoreStatus})
}

func (s *Server) handleJobNodeExecutionAttempts(w http.ResponseWriter, r *http.Request) {
	jobID := mux.Vars(r)["id"]
	ctx := r.Context()
	rows, err := s.storage.ListNodeExecutionAttemptsByJob(ctx, jobID)
	if err != nil {
		writeJSONError(w, "Failed to query node execution attempts", http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, map[string]interface{}{
		"job_id":                  jobID,
		"node_execution_attempts": rows,
	})
}

func (s *Server) handleJobAgentRuns(w http.ResponseWriter, r *http.Request) {
	jobID := mux.Vars(r)["id"]
	ctx := r.Context()
	rows, err := s.storage.ListAgentRunsByJob(ctx, jobID)
	if err != nil {
		writeJSONError(w, "Failed to query agent runs", http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, map[string]interface{}{
		"job_id":     jobID,
		"agent_runs": enrichAgentRunsWithNovomoURLs(rows, novomoFrontendV2URL()),
	})
}

func (s *Server) handleStopJobAgentRun(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	jobID := vars["id"]
	agentRunID := vars["agentRunID"]

	row, err := s.jobManager.StopExternalAgentRun(r.Context(), jobID, agentRunID)
	if err != nil {
		statusCode := http.StatusInternalServerError
		switch {
		case errors.Is(err, storage.ErrNotFound):
			statusCode = http.StatusNotFound
		case errors.Is(err, jobs.ErrJobStateConflict):
			statusCode = http.StatusConflict
		default:
			if nerr, ok := novomo.AsError(err); ok {
				switch nerr.StatusCode {
				case http.StatusNotFound:
					statusCode = http.StatusNotFound
				case http.StatusConflict:
					statusCode = http.StatusConflict
				default:
					if nerr.Retryable {
						statusCode = http.StatusBadGateway
					}
				}
			}
		}
		writeJSONError(w, err.Error(), statusCode)
		return
	}

	log.Printf("Novomo-backed agent run %s stopped via admin panel", agentRunID)
	writeJSONResponse(w, map[string]any{
		"success":   true,
		"agent_run": row,
		"message":   "Agent run stop requested",
	})
}

type overviewStats struct {
	TotalJobs      int64
	CompletedJobs  int64
	FailedJobs     int64
	TotalCost      float64
	TotalTokens    int64
	TotalWorkflows int64
}

func (s *Server) loadOverviewStats() overviewStats {
	var stats overviewStats

	if err := s.db.QueryRow(`
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(cost), 0),
		       COALESCE(SUM(tokens_total), 0)
		FROM jobs
	`).Scan(&stats.TotalJobs, &stats.CompletedJobs, &stats.FailedJobs, &stats.TotalCost, &stats.TotalTokens); err != nil {
		log.Printf("Error getting job stats: %v", err)
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM workflows").Scan(&stats.TotalWorkflows); err != nil {
		log.Printf("Error getting total workflows: %v", err)
	}

	return stats
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	stats := s.loadOverviewStats()
	writeJSONResponse(w, map[string]interface{}{
		"Stats": stats,
	})
}

// JobWithPrompt extends Job with extracted input prompt for display
type JobWithPrompt struct {
	storage.Job
	InputPrompt       string
	IsChild           bool
	ParentExecutionID string
	DirectTokens      int
	ChildTokens       int
	DisplayTokens     int
	DirectCost        float64
	ChildCost         float64
	DisplayCost       float64
	DescendantCount   int
}

// Pagination holds pagination state for paginated API responses.
type Pagination struct {
	Page       int
	Limit      int
	TotalItems int
	TotalPages int
	HasPrev    bool
	HasNext    bool
	StartItem  int
	EndItem    int
	Pages      []int // Page numbers to show
}

func (s *Server) handleTestWorkflow(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Workflow workflow.Workflow `json:"workflow"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Execute workflow via JobManager
	result, err := s.jobManager.ExecuteWorkflow(r.Context(), &req.Workflow)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		log.Printf("Error encoding test workflow result: %v", err)
		writeJSONError(w, "Failed to encode response", http.StatusInternalServerError)
	}
}
