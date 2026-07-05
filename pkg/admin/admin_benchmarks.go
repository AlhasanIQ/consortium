package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/pkg/bench"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	workflowruntime "github.com/alhasaniq/consortium/pkg/workflow/runtime"
	"github.com/gorilla/mux"
)

type benchmarkRunPageData struct {
	Runs                []storage.BenchmarkRun
	Runner              benchmarkRunnerState
	Benchmarks          []string
	Workflows           []string
	Statuses            []string
	Splits              []string
	FilterBenchmark     string
	FilterWorkflow      string
	FilterStatus        string
	FilterSplit         string
	FilterIncludeOpt    bool
	ChartSeries         []benchmarkChartSeries
	AvailableBenchmarks []string
	AvailableWorkflows  []string
}

type benchmarkDetailPageData struct {
	Run                      *storage.BenchmarkRun
	Optimization             *benchmarkOptimizationContext
	Items                    []storage.BenchmarkRunItem
	TotalItems               int
	Page                     int
	PageSize                 int
	HasPrev                  bool
	HasNext                  bool
	OnlyIncorrect            bool
	Subject                  string
	FailureReason            string
	FailureLabels            []string
	FailureSeries            []int
	FinalFailureLabels       []string
	FinalFailureSeries       []int
	AllAttemptsFailureLabels []string
	AllAttemptsFailureSeries []int
	CorrectCount             int
	IncorrectCount           int
	TopSubjectLabels         []string
	TopSubjectIncorrect      []int
	FlaggedItems             int     `json:"flagged_items"`
	AdjustedAccuracy         float64 `json:"adjusted_accuracy"`
}

type benchmarkOptimizationContext struct {
	RunID      string `json:"run_id"`
	OrganismID string `json:"organism_id"`
}

type benchmarkItemDetailPageData struct {
	Run           *storage.BenchmarkRun
	Detail        *storage.BenchmarkRunItemDetail
	DatasetItem   *bench.DatasetItem
	JobSummaries  []benchmarkItemJobSummary
	NotFoundInSet bool
	Flag          *storage.DatasetFlag `json:"flag,omitempty"`
}

type benchmarkItemJobSummary struct {
	JobID          string
	Job            *storage.Job
	Nodes          []storage.WorkflowNode
	Attempts       []storage.NodeExecutionAttempt
	TotalTokens    int
	TotalCost      float64
	DescendantJobs int
	ChildJobs      []benchmarkItemChildJob
}

type benchmarkItemChildJob struct {
	JobID string
	Job   *storage.Job
	Nodes []storage.WorkflowNode
}

var safeCSVValue = regexp.MustCompile(`^[A-Za-z0-9,_\-]+$`)
var safeSplitValue = regexp.MustCompile(`^[A-Za-z0-9_\-]+$`)

func wantsLiveUsageEnrichment(r *http.Request) bool {
	flag := strings.TrimSpace(r.URL.Query().Get("enrich_usage"))
	if flag == "1" {
		return true
	}
	return strings.EqualFold(flag, "true")
}

func (s *Server) handleBenchmarks(w http.ResponseWriter, r *http.Request) {
	benchmarkFilter := strings.TrimSpace(r.URL.Query().Get("benchmark"))
	workflowFilter := strings.TrimSpace(r.URL.Query().Get("workflow"))
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	splitFilter := strings.TrimSpace(r.URL.Query().Get("split"))
	includeOptimizer := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_optimizer")), "true") || strings.TrimSpace(r.URL.Query().Get("include_optimizer")) == "1"
	limit := parseIntDefault(r.URL.Query().Get("limit"), 300)
	if limit <= 0 || limit > 500 {
		limit = 300
	}

	runs, err := s.storage.ListBenchmarkRuns(limit, benchmarkFilter, workflowFilter, statusFilter, splitFilter, includeOptimizer)
	if err != nil {
		writeJSONError(w, "Failed to list benchmark runs", http.StatusInternalServerError)
		return
	}
	// If DB was reset, bootstrap from artifacts automatically for admin UX.
	if benchmarkFilter == "" && workflowFilter == "" && statusFilter == "" && splitFilter == "" {
		allRuns, countErr := s.storage.ListBenchmarkRuns(1, "", "", "", "", true)
		if countErr != nil {
			writeJSONError(w, "Failed to list benchmark runs", http.StatusInternalServerError)
			return
		}
		if len(allRuns) == 0 {
			if imported, failed, importErr := s.importBenchmarkArtifacts(s.benchmarkResultsDir()); importErr == nil {
				if imported > 0 || failed > 0 {
					runs, _ = s.storage.ListBenchmarkRuns(300, "", "", "", "", includeOptimizer)
				}
			}
		}
	}
	// Keep benchmark list requests cheap: serve persisted totals only.
	// Inclusive usage enrichment is applied in detail paths and when saving runs.

	data := benchmarkRunPageData{
		Runs:                runs,
		FilterBenchmark:     benchmarkFilter,
		FilterWorkflow:      workflowFilter,
		FilterStatus:        statusFilter,
		FilterSplit:         splitFilter,
		FilterIncludeOpt:    includeOptimizer,
		AvailableBenchmarks: []string{bench.BenchmarkGlobalMMLULite, bench.BenchmarkGlobalMMLU, bench.BenchmarkMMLUPro, bench.BenchmarkMath500},
	}
	data.Runner = s.currentBenchmarkRunnerState()
	data.Benchmarks, data.Workflows, data.Statuses, data.Splits = collectBenchmarkFilters(runs)
	data.ChartSeries = buildRunChartSeries(runs)

	// Fetch benchmark workflow IDs for the run form selector.
	if allWorkflows, err := s.storage.ListWorkflows(500); err == nil {
		for _, wf := range allWorkflows {
			if strings.HasPrefix(wf.ID, "benchmark-") {
				data.AvailableWorkflows = append(data.AvailableWorkflows, wf.ID)
			}
		}
		sort.Strings(data.AvailableWorkflows)
	}

	writeJSONResponse(w, data)
}

func (s *Server) handleBenchmarkDetail(w http.ResponseWriter, r *http.Request) {
	runID := mux.Vars(r)["id"]
	if strings.TrimSpace(runID) == "" {
		writeJSONError(w, "Missing run ID", http.StatusBadRequest)
		return
	}

	run, err := s.storage.GetBenchmarkRun(runID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeJSONError(w, "Benchmark run not found", http.StatusNotFound)
			return
		}
		writeJSONError(w, "Failed to load benchmark run", http.StatusInternalServerError)
		return
	}

	page := parseIntDefault(r.URL.Query().Get("page"), 1)
	pageSize := parseIntDefault(r.URL.Query().Get("page_size"), 100)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 100
	}
	if pageSize > 500 {
		pageSize = 500
	}
	offset := (page - 1) * pageSize

	filters := storage.BenchmarkRunItemFilters{
		OnlyIncorrect: strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("incorrect")), "1"),
		Subject:       strings.TrimSpace(r.URL.Query().Get("subject")),
		FailureReason: strings.TrimSpace(r.URL.Query().Get("failure_reason")),
		Limit:         pageSize,
		Offset:        offset,
	}

	totalItems, err := s.storage.CountBenchmarkRunItems(runID, filters)
	if err != nil {
		writeJSONError(w, "Failed to count benchmark items", http.StatusInternalServerError)
		return
	}
	items, err := s.storage.ListBenchmarkRunItems(runID, filters)
	if err != nil {
		writeJSONError(w, "Failed to list benchmark items", http.StatusInternalServerError)
		return
	}

	allItems, err := s.loadAllBenchmarkRunItems(runID)
	if err != nil {
		writeJSONError(w, "Failed to load benchmark analytics", http.StatusInternalServerError)
		return
	}

	// Default to persisted usage/cost/latency for fast reads. Live inclusive
	// enrichment is available as a legacy fallback for older runs.
	if wantsLiveUsageEnrichment(r) {
		usageIDs := collectBenchmarkItemJobIDs(allItems)
		if len(usageIDs) == 0 {
			usageIDs = collectBenchmarkItemJobIDs(items)
		}
		if len(usageIDs) > 0 {
			if usageByJob, usageErr := s.loadExecutionCostSummaries(usageIDs); usageErr != nil {
				log.Printf("Warning: failed to load benchmark execution usage for run %s: %v", runID, usageErr)
			} else {
				existingItemCosts := make(map[string]storage.BenchmarkItemCostUpdate, len(allItems))
				for _, it := range allItems {
					existingItemCosts[it.ItemID] = storage.BenchmarkItemCostUpdate{
						RunID:       runID,
						ItemID:      it.ItemID,
						TotalTokens: it.TotalTokens,
						CostUSD:     it.CostUSD,
					}
				}
				existingRunTokens := run.TotalTokens
				existingRunCost := run.TotalCostUSD

				_, _, _ = applyExecutionUsageToBenchmarkItems(items, usageByJob)
				totalTokens, totalCost, totalLatency := applyExecutionUsageToBenchmarkItems(allItems, usageByJob)
				if totalTokens > 0 || totalCost > 0 || totalLatency > 0 {
					run.TotalTokens = totalTokens
					run.TotalCostUSD = totalCost
					run.TotalLatencyMS = totalLatency
					if run.TotalItems > 0 {
						run.AvgTokensPerItem = float64(totalTokens) / float64(run.TotalItems)
						run.AvgCostUSDPerItem = totalCost / float64(run.TotalItems)
						run.AvgLatencyMS = totalLatency / float64(run.TotalItems)
					}
					latenciesMS := make([]float64, 0, len(allItems))
					for _, item := range allItems {
						latenciesMS = append(latenciesMS, item.LatencyMS)
					}
					if len(latenciesMS) > 0 {
						run.P50LatencyMS = percentileFloat64(latenciesMS, 50)
						run.P95LatencyMS = percentileFloat64(latenciesMS, 95)
						run.P99LatencyMS = percentileFloat64(latenciesMS, 99)
					}
				}

				// Avoid cost writeback during active benchmark execution; the extra write
				// load can contend with benchmark item workflow writes and trigger SQLite
				// transaction contention.
				s.benchmarkRunnerMu.Lock()
				benchmarkRunnerActive := s.benchmarkRunner != nil && s.benchmarkRunner.Running
				s.benchmarkRunnerMu.Unlock()

				if !benchmarkRunnerActive {
					// Write enriched costs back to DB so they survive job deletion.
					if run.TotalTokens != existingRunTokens || !almostEqualFloat(run.TotalCostUSD, existingRunCost) {
						if err := s.storage.UpdateBenchmarkRunCosts(runID, run.TotalTokens, run.TotalCostUSD); err != nil {
							log.Printf("Warning: failed to persist run costs for %s: %v", runID, err)
						}
					}
					costUpdates := make([]storage.BenchmarkItemCostUpdate, 0, len(allItems))
					for _, it := range allItems {
						prev := existingItemCosts[it.ItemID]
						if prev.ItemID == "" {
							prev = storage.BenchmarkItemCostUpdate{RunID: runID, ItemID: it.ItemID}
						}
						if it.TotalTokens == prev.TotalTokens && almostEqualFloat(it.CostUSD, prev.CostUSD) {
							continue
						}
						// Skip no-op zero rows to avoid churn.
						if it.TotalTokens == 0 && prev.TotalTokens == 0 && almostEqualFloat(it.CostUSD, 0) && almostEqualFloat(prev.CostUSD, 0) {
							continue
						}
						costUpdates = append(costUpdates, storage.BenchmarkItemCostUpdate{
							RunID:       runID,
							ItemID:      it.ItemID,
							TotalTokens: it.TotalTokens,
							CostUSD:     it.CostUSD,
						})
					}
					if len(costUpdates) > 0 {
						if err := s.storage.BatchUpdateBenchmarkRunItemCosts(costUpdates); err != nil {
							log.Printf("Warning: failed to persist item costs for run %s: %v", runID, err)
						}
					}
				}
			}
		}
	}

	failureLabels, failureSeries := buildFailureChartData(run, allItems, failureSourceAllAttempts)
	finalLabels, finalSeries := buildFailureChartData(run, allItems, failureSourceFinalOnly)
	attemptLabels, attemptSeries := buildFailureChartData(run, allItems, failureSourceAllAttempts)
	topSubjectLabels, topSubjectIncorrect := topIncorrectSubjects(allItems, 10)

	flagMap := s.loadDatasetFlagMap(run.Benchmark, run.Split, allItems)
	flaggedCount := len(flagMap)
	adjustedAccuracy := run.Accuracy
	if flaggedCount > 0 && len(allItems) > flaggedCount {
		correctExcluding := 0
		for _, it := range allItems {
			if _, flagged := flagMap[it.ItemID]; !flagged && it.Correct {
				correctExcluding++
			}
		}
		adjustedAccuracy = float64(correctExcluding) / float64(len(allItems)-flaggedCount)
	}

	var optimizeCtx *benchmarkOptimizationContext
	optRunID := strings.TrimSpace(run.OptRunID)
	optOrgID := strings.TrimSpace(run.OptOrganismID)
	if optRunID == "" || optOrgID == "" {
		if linkedRunID, linkedOrgID, err := s.storage.GetOptimizationLinkByBenchmarkRunID(run.ID); err == nil {
			if optRunID == "" {
				optRunID = linkedRunID
			}
			if optOrgID == "" {
				optOrgID = linkedOrgID
			}
		}
	}
	if optRunID != "" {
		optimizeCtx = &benchmarkOptimizationContext{
			RunID:      optRunID,
			OrganismID: optOrgID,
		}
	}

	data := benchmarkDetailPageData{
		Run:                      run,
		Optimization:             optimizeCtx,
		Items:                    items,
		TotalItems:               totalItems,
		Page:                     page,
		PageSize:                 pageSize,
		HasPrev:                  page > 1,
		HasNext:                  offset+len(items) < totalItems,
		OnlyIncorrect:            filters.OnlyIncorrect,
		Subject:                  filters.Subject,
		FailureReason:            filters.FailureReason,
		FailureLabels:            failureLabels,
		FailureSeries:            failureSeries,
		FinalFailureLabels:       finalLabels,
		FinalFailureSeries:       finalSeries,
		AllAttemptsFailureLabels: attemptLabels,
		AllAttemptsFailureSeries: attemptSeries,
		CorrectCount:             run.CorrectItems,
		IncorrectCount:           max(run.TotalItems-run.CorrectItems, 0),
		TopSubjectLabels:         topSubjectLabels,
		TopSubjectIncorrect:      topSubjectIncorrect,
		FlaggedItems:             flaggedCount,
		AdjustedAccuracy:         adjustedAccuracy,
	}

	writeJSONResponse(w, data)
}

func almostEqualFloat(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func (s *Server) handleBenchmarkItemDetail(w http.ResponseWriter, r *http.Request) {
	runID := mux.Vars(r)["id"]
	itemID := mux.Vars(r)["itemID"]
	if strings.TrimSpace(itemID) == "" {
		itemID = r.URL.Query().Get("item_id")
	}
	if strings.TrimSpace(itemID) == "" {
		itemID = r.URL.Query().Get("itemID")
	}
	if strings.TrimSpace(runID) == "" || strings.TrimSpace(itemID) == "" {
		writeJSONError(w, "Missing run/item ID", http.StatusBadRequest)
		return
	}

	run, err := s.storage.GetBenchmarkRun(runID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeJSONError(w, "Benchmark run not found", http.StatusNotFound)
			return
		}
		writeJSONError(w, "Failed to load benchmark run", http.StatusInternalServerError)
		return
	}

	detail, err := s.storage.GetBenchmarkRunItemDetail(runID, itemID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeJSONError(w, "Benchmark item not found", http.StatusNotFound)
			return
		}
		writeJSONError(w, "Failed to load benchmark item detail", http.StatusInternalServerError)
		return
	}

	datasetItem, found, err := loadDatasetItemByID(run.DatasetPath, itemID)
	if err != nil {
		writeJSONError(w, "Failed to resolve dataset item", http.StatusInternalServerError)
		return
	}

	jobIDs := uniqueAttemptJobIDs(detail.Attempts)
	usageByJob := make(map[string]ExecutionCostSummary)
	if wantsLiveUsageEnrichment(r) && len(jobIDs) > 0 {
		if summaries, usageErr := s.loadExecutionCostSummaries(jobIDs); usageErr != nil {
			log.Printf("Warning: failed to load benchmark item execution usage for run=%s item=%s: %v", runID, itemID, usageErr)
		} else {
			usageByJob = summaries
		}
	}

	itemTokens := 0
	itemCost := 0.0
	itemLatency := 0.0
	for i := range detail.Attempts {
		attempt := &detail.Attempts[i]
		jobID := strings.TrimSpace(attempt.JobID)
		if summary, ok := usageByJob[jobID]; ok {
			attempt.TotalTokens = summary.TotalTokens
			attempt.CostUSD = summary.TotalCost
			if summary.TotalLatencyMs > 0 {
				attempt.LatencyMS = summary.TotalLatencyMs
			}
		}
		itemTokens += attempt.TotalTokens
		itemCost += attempt.CostUSD
		itemLatency += attempt.LatencyMS
	}
	detail.Item.TotalTokens = itemTokens
	detail.Item.CostUSD = itemCost
	detail.Item.LatencyMS = itemLatency

	jobSummaries := make([]benchmarkItemJobSummary, 0, len(jobIDs))
	for _, jobID := range jobIDs {
		job, err := s.storage.GetExecution(jobID)
		if err != nil {
			continue
		}
		nodes, _ := s.storage.GetWorkflowNodes(jobID)
		for i := range nodes {
			applyChildWorkflowDisplayCost(&nodes[i])
		}
		// Interpolate prompt templates for display (same as admin job detail).
		if job != nil {
			reqCtx := extractWorkflowContext(job.RequestData)
			wfCtx := buildFullWorkflowContext(reqCtx, nodes)
			for i := range nodes {
				nodes[i].Prompt = workflow.InterpolateVariables(nodes[i].Prompt, wfCtx)
			}
		}
		attempts, _ := s.storage.GetNodeExecutionAttempts(jobID)
		tokens := job.TokensTotal
		cost := job.Cost
		descendants := 0
		if summary, ok := usageByJob[jobID]; ok {
			tokens = summary.TotalTokens
			cost = summary.TotalCost
			descendants = summary.DescendantCount
		}

		// Fetch child job nodes for the execution drilldown.
		var childJobs []benchmarkItemChildJob
		if children, childErr := s.storage.GetChildExecutions(jobID); childErr == nil {
			for _, child := range children {
				childJob, childJobErr := s.storage.GetExecution(child.ID)
				if childJobErr != nil {
					continue
				}
				childNodes, _ := s.storage.GetWorkflowNodes(child.ID)
				// Interpolate child prompt templates for display.
				if childJob != nil {
					childReqCtx := extractWorkflowContext(childJob.RequestData)
					childWfCtx := buildFullWorkflowContext(childReqCtx, childNodes)
					for i := range childNodes {
						childNodes[i].Prompt = workflow.InterpolateVariables(childNodes[i].Prompt, childWfCtx)
					}
				}
				childJobs = append(childJobs, benchmarkItemChildJob{
					JobID: child.ID,
					Job:   childJob,
					Nodes: childNodes,
				})
			}
		}

		jobSummaries = append(jobSummaries, benchmarkItemJobSummary{
			JobID:          jobID,
			Job:            job,
			Nodes:          nodes,
			Attempts:       attempts,
			TotalTokens:    tokens,
			TotalCost:      cost,
			DescendantJobs: descendants,
			ChildJobs:      childJobs,
		})
	}

	// Lookup active dataset flag for this item.
	var itemFlag *storage.DatasetFlag
	if flags, flagErr := s.storage.ListFlagsByItems(run.Benchmark, run.Split, []string{itemID}); flagErr == nil && len(flags) > 0 {
		itemFlag = &flags[0]
	}

	data := benchmarkItemDetailPageData{
		Run:           run,
		Detail:        detail,
		DatasetItem:   datasetItem,
		JobSummaries:  jobSummaries,
		NotFoundInSet: !found,
		Flag:          itemFlag,
	}

	writeJSONResponse(w, data)
}

func (s *Server) handleBenchmarkWrongAnswerAnalysis(w http.ResponseWriter, r *http.Request) {
	runID := strings.TrimSpace(mux.Vars(r)["id"])
	if runID == "" {
		writeJSONError(w, "Missing run ID", http.StatusBadRequest)
		return
	}

	run, err := s.storage.GetBenchmarkRun(runID)
	if err != nil {
		writeJSONError(w, "Benchmark run not found", http.StatusNotFound)
		return
	}

	allItems, err := s.loadAllBenchmarkRunItems(runID)
	if err != nil {
		writeJSONError(w, "Failed to load benchmark items", http.StatusInternalServerError)
		return
	}

	format := bench.FormatForBenchmark(run.Benchmark)

	// Load active dataset flags for enrichment
	flagMap := s.loadDatasetFlagMap(run.Benchmark, run.Split, allItems)

	topN := parseIntDefault(r.URL.Query().Get("top"), 10)
	if topN < 1 {
		topN = 10
	}
	if topN > 50 {
		topN = 50
	}
	page := parseIntDefault(r.URL.Query().Get("page"), 0)
	pageSize := parseIntDefault(r.URL.Query().Get("page_size"), 0)
	if page < 0 {
		page = 0
	}
	if pageSize < 0 {
		pageSize = 0
	}
	if pageSize > 500 {
		pageSize = 500
	}

	var incorrectItems []storage.BenchmarkRunItem
	for _, item := range allItems {
		if !item.Correct {
			incorrectItems = append(incorrectItems, item)
		}
	}

	performanceCollector := newBenchmarkPerformanceCollector(format, len(allItems))
	diagnosticsCollector := newBenchmarkAnalysisDiagnosticsCollector(topN)
	parentJobIDs := collectBenchmarkItemJobIDs(allItems)
	parentAttemptsByJob := make(map[string][]storage.NodeExecutionAttempt, len(parentJobIDs))
	parentNodesByJob := make(map[string][]storage.WorkflowNode, len(parentJobIDs))
	if len(parentJobIDs) > 0 {
		if rows, err := s.storage.ListNodeExecutionAttemptsByJobIDs(parentJobIDs); err != nil {
			log.Printf("Warning: failed to bulk load parent node attempts for benchmark analysis run=%s: %v", runID, err)
		} else {
			parentAttemptsByJob = rows
		}
		if rows, err := s.storage.ListWorkflowNodesByJobIDs(parentJobIDs); err != nil {
			log.Printf("Warning: failed to bulk load parent workflow nodes for benchmark analysis run=%s: %v", runID, err)
		} else {
			parentNodesByJob = rows
		}
	}
	childDataByParentJobID, err := s.loadBenchmarkChildDataByParentJobIDs(parentJobIDs)
	if err != nil {
		log.Printf("Warning: failed to bulk load child workflow data for benchmark analysis run=%s: %v", runID, err)
		childDataByParentJobID = map[string]benchmarkChildAnalysisData{}
	}
	childJobIDs := make([]string, 0, len(childDataByParentJobID))
	for _, childData := range childDataByParentJobID {
		childJobIDs = append(childJobIDs, childData.jobID)
	}
	childAttemptsByJob := make(map[string][]storage.NodeExecutionAttempt, len(childJobIDs))
	if len(childJobIDs) > 0 {
		if rows, err := s.storage.ListNodeExecutionAttemptsByJobIDs(childJobIDs); err != nil {
			log.Printf("Warning: failed to bulk load child node attempts for benchmark analysis run=%s: %v", runID, err)
		} else {
			childAttemptsByJob = rows
		}
	}

	for _, item := range allItems {
		parentJobID := strings.TrimSpace(item.JobID)
		diag := benchmarkAnalysisItemDiagnostics{
			ItemID:      item.ItemID,
			Subject:     item.Subject,
			ParentJobID: parentJobID,
		}

		if parentJobID != "" {
			parentAttempts := parentAttemptsByJob[parentJobID]
			parentUsage := summarizeNodeAttemptUsage(parentAttempts)
			if len(parentUsage) > 0 {
				diag.ParentLatency, diag.ParentRetries = summarizeNodeUsageTotals(parentUsage)
			} else {
				diag.ParentLatency = item.LatencyMS
			}

			if childData, ok := childDataByParentJobID[parentJobID]; ok {
				childAttempts := childAttemptsByJob[childData.jobID]

				diag.ChildJobID = childData.jobID
				childUsage := summarizeNodeAttemptUsage(childAttempts)
				if len(childUsage) > 0 {
					diag.ChildLatency, diag.ChildRetries = summarizeNodeUsageTotals(childUsage)
				} else {
					diag.ChildLatency, diag.ChildRetries = summarizeNodeFallbackTotals(childData.nodes)
				}

				performanceCollector.ingestItem(item.AnswerLabel, childData.nodes, childAttempts, true)
			} else {
				performanceCollector.ingestItem(item.AnswerLabel, parentNodesByJob[parentJobID], parentAttempts, false)
			}
		}

		diag.TotalLatency = diag.ParentLatency + diag.ChildLatency
		diag.TotalRetries = diag.ParentRetries + diag.ChildRetries
		diagnosticsCollector.add(diag)
	}

	analysisItems := make([]wrongAnswerAnalysisItem, 0, len(incorrectItems))
	var summary wrongAnswerSummary
	summary.TotalIncorrect = len(incorrectItems)

	for _, item := range incorrectItems {
		ai := wrongAnswerAnalysisItem{
			ItemID:          item.ItemID,
			Subject:         item.Subject,
			AnswerLabel:     item.AnswerLabel,
			ParentPredicted: item.Predicted,
			ParentJobID:     item.JobID,
			AgentAnswers:    []agentNodeAnswer{},
			Category:        categoryUnclassified,
		}
		if flag, ok := flagMap[item.ItemID]; ok {
			ai.Flagged = true
			ai.FlagReason = flag.Reason
			summary.FlaggedItems++
		}

		if strings.TrimSpace(item.JobID) == "" {
			summary.Unclassified++
			analysisItems = append(analysisItems, ai)
			continue
		}

		childData, ok := childDataByParentJobID[strings.TrimSpace(item.JobID)]
		if !ok {
			summary.Unclassified++
			analysisItems = append(analysisItems, ai)
			continue
		}

		ai.ChildJobID = childData.jobID
		agentAnswers, childResultAnswer, childResultParseOK := extractChildWorkflowAnswers(format, childData.nodes, item.AnswerLabel)

		ai.AgentAnswers = agentAnswers
		ai.ChildPredicted = childResultAnswer

		category := classifyWrongAnswer(format, item.AnswerLabel, childResultAnswer, childResultParseOK, agentAnswers)
		ai.Category = category

		switch category {
		case categoryAllStepsWrong:
			summary.AllStepsWrong++
		case categorySomeRightChildWrong:
			summary.SomeRightChildWrong++
		case categoryAllRightChildWrong:
			summary.AllRightChildWrong++
		case categoryChildRightParentWrong:
			summary.ChildRightParentWrong++
		default:
			summary.Unclassified++
		}

		analysisItems = append(analysisItems, ai)
	}

	totalIncorrectItems := len(analysisItems)
	pagedItems := analysisItems
	hasMore := false
	if page > 0 && pageSize > 0 {
		start := (page - 1) * pageSize
		if start < 0 {
			start = 0
		}
		if start >= len(analysisItems) {
			pagedItems = []wrongAnswerAnalysisItem{}
		} else {
			end := start + pageSize
			if end < len(analysisItems) {
				hasMore = true
			}
			if end > len(analysisItems) {
				end = len(analysisItems)
			}
			pagedItems = analysisItems[start:end]
		}
	}

	// Compute raw and adjusted accuracy.
	// FlaggedItems counts all flagged items in the run (not just incorrect ones).
	totalFlagged := len(flagMap)
	summary.FlaggedItems = totalFlagged
	summary.RawAccuracy = run.Accuracy
	totalAll := len(allItems)
	if totalAll > totalFlagged && totalFlagged > 0 {
		// Adjusted = correct (excluding flagged) / total (excluding flagged)
		correctExcludingFlagged := 0
		for _, item := range allItems {
			if _, isFlagged := flagMap[item.ItemID]; !isFlagged && item.Correct {
				correctExcludingFlagged++
			}
		}
		summary.AdjustedAccuracy = float64(correctExcludingFlagged) / float64(totalAll-totalFlagged)
	} else {
		summary.AdjustedAccuracy = run.Accuracy
	}

	writeJSONResponse(w, wrongAnswerAnalysisResponse{
		RunID:       run.ID,
		Benchmark:   run.Benchmark,
		Split:       run.Split,
		WorkflowID:  run.WorkflowID,
		Summary:     summary,
		Items:       pagedItems,
		TotalItems:  totalIncorrectItems,
		Page:        page,
		PageSize:    pageSize,
		HasMore:     hasMore,
		Performance: performanceCollector.toView(),
		Diagnostics: diagnosticsCollector.toView(),
	})
}

func (s *Server) handleBenchmarkImport(w http.ResponseWriter, r *http.Request) {
	resultsDir := s.benchmarkResultsDir()
	imported, failed, err := s.importBenchmarkArtifacts(resultsDir)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Import failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"imported_runs": imported,
		"failed_files":  failed,
		"results_dir":   resultsDir,
	})
}

func (s *Server) benchmarkResultsDir() string {
	root := strings.TrimSpace(s.workdir)
	if root == "" {
		root = "."
	}
	return filepath.Join(root, "benchmarks", "results")
}

func (s *Server) handleBenchmarkRerunFailures(w http.ResponseWriter, r *http.Request) {
	runID := mux.Vars(r)["id"]
	if strings.TrimSpace(runID) == "" {
		writeJSONError(w, "Missing run ID", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSONError(w, "Invalid form payload", http.StatusBadRequest)
		return
	}
	targetItemID := strings.TrimSpace(r.FormValue("item"))
	if targetItemID != "" && !safeSplitValue.MatchString(targetItemID) {
		writeJSONError(w, "item must be a safe item ID", http.StatusBadRequest)
		return
	}
	admissionBypass := parseAdminBool(r.FormValue("admission_bypass"))

	// Guard: runner must be idle.
	s.benchmarkRunnerMu.Lock()
	if s.benchmarkRunner != nil && s.benchmarkRunner.Running {
		s.benchmarkRunnerMu.Unlock()
		writeJSONError(w, "A benchmark run is already active", http.StatusConflict)
		return
	}
	s.benchmarkRunnerMu.Unlock()

	run, err := s.storage.GetBenchmarkRun(runID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeJSONError(w, "Benchmark run not found", http.StatusNotFound)
			return
		}
		writeJSONError(w, "Failed to load benchmark run", http.StatusInternalServerError)
		return
	}

	failedIDs, err := s.storage.ListFailedBenchmarkRunItemIDs(runID)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to list failed items: %v", err), http.StatusInternalServerError)
		return
	}
	if len(failedIDs) == 0 {
		writeJSONError(w, "No failed items in this run", http.StatusBadRequest)
		return
	}

	wf, err := s.storage.GetWorkflow(run.WorkflowID)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to load workflow: %v", err), http.StatusBadRequest)
		return
	}

	items, err := bench.LoadDataset(run.DatasetPath, 0)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to load dataset: %v", err), http.StatusBadRequest)
		return
	}

	failedSet := make(map[string]struct{}, len(failedIDs))
	for _, id := range failedIDs {
		failedSet[id] = struct{}{}
	}
	if targetItemID != "" {
		if _, ok := failedSet[targetItemID]; !ok {
			writeJSONError(w, "requested item is not a failed item in this run", http.StatusBadRequest)
			return
		}
	}
	filtered := make([]bench.DatasetItem, 0, len(failedIDs))
	for _, item := range items {
		if _, ok := failedSet[item.ID]; !ok {
			continue
		}
		if targetItemID != "" && item.ID != targetItemID {
			continue
		}
		filtered = append(filtered, item)
	}
	if len(filtered) == 0 {
		writeJSONError(w, "Failed items not found in dataset", http.StatusBadRequest)
		return
	}
	if !s.ensureBenchmarkAdmission(w, admissionBypass, len(filtered)) {
		return
	}

	concurrency := max(parseIntDefault(r.FormValue("concurrency"), 20), 1)
	maxNonLetterRetries := max(parseIntDefault(r.FormValue("max_non_letter_retries"), 2), 0)
	maxTransientRetries := max(parseIntDefault(r.FormValue("max_transient_retries"), 3), 0)

	startedAt := time.Now()

	plans := []benchmarkRunPlan{{
		Benchmark:          run.Benchmark,
		Split:              run.Split,
		DatasetPath:        run.DatasetPath,
		ItemLimit:          len(filtered),
		Source:             "manual",
		WorkflowID:         run.WorkflowID,
		WorkflowName:       wf.Name,
		WorkflowDefinition: wf.Definition,
		RunID:              runID, // reuse original run ID — results merge in-place
		MergeIntoRunID:     runID,
		StartedAt:          startedAt,
		Items:              filtered,
		ItemResults:        make([]bench.ItemResult, len(filtered)),
		AdmissionBypass:    admissionBypass,
	}}
	if err := s.preflightValidateBenchmarkWorkflow(wf, filtered[0]); err != nil {
		writeJSONError(w, fmt.Sprintf("Workflow validation failed before benchmark rerun: %v", err), http.StatusBadRequest)
		return
	}

	cfg := benchmarkRunRequest{
		Benchmarks:          []string{run.Benchmark},
		Workflows:           []string{run.WorkflowID},
		Split:               run.Split,
		Limit:               len(filtered),
		Source:              "manual",
		Concurrency:         concurrency,
		MaxNonLetterRetries: maxNonLetterRetries,
		MaxTransientRetries: maxTransientRetries,
		AdmissionBypass:     admissionBypass,
	}

	if err := s.launchBenchmarkRun("rerun-failures", 1, len(filtered), cfg, plans); err != nil {
		writeJSONError(w, err.Error(), http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"started":           true,
		"failed_item_count": len(filtered),
		"run_id":            runID,
		"item":              targetItemID,
		"admission_bypass":  admissionBypass,
	})
}

func (s *Server) handleBenchmarkReplayItems(w http.ResponseWriter, r *http.Request) {
	baseRunID := mux.Vars(r)["id"]
	if strings.TrimSpace(baseRunID) == "" {
		writeJSONError(w, "Missing run ID", http.StatusBadRequest)
		return
	}

	s.benchmarkRunnerMu.Lock()
	if s.benchmarkRunner != nil && s.benchmarkRunner.Running {
		s.benchmarkRunnerMu.Unlock()
		writeJSONError(w, "A benchmark run is already active", http.StatusConflict)
		return
	}
	s.benchmarkRunnerMu.Unlock()

	if err := r.ParseForm(); err != nil {
		writeJSONError(w, "Invalid form payload", http.StatusBadRequest)
		return
	}

	itemsRaw := strings.TrimSpace(r.FormValue("items"))
	if itemsRaw == "" || !safeCSVValue.MatchString(itemsRaw) {
		writeJSONError(w, "items is required and must be a safe comma-separated list", http.StatusBadRequest)
		return
	}
	itemIDs := dedupeOrderedStrings(bench.ParseCSVValues(itemsRaw))
	if len(itemIDs) == 0 {
		writeJSONError(w, "at least one item is required", http.StatusBadRequest)
		return
	}
	admissionBypass := parseAdminBool(r.FormValue("admission_bypass"))

	replayMode, err := parseReplayModeForm(r.FormValue("mode"))
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	baseRun, err := s.storage.GetBenchmarkRun(baseRunID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeJSONError(w, "Benchmark run not found", http.StatusNotFound)
			return
		}
		writeJSONError(w, "Failed to load benchmark run", http.StatusInternalServerError)
		return
	}

	workflowID := strings.TrimSpace(r.FormValue("workflow"))
	if workflowID == "" {
		workflowID = baseRun.WorkflowID
	}
	workflowDef, err := s.storage.GetWorkflow(workflowID)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to load workflow: %v", err), http.StatusBadRequest)
		return
	}

	changedWorkflowIDs := make(map[string]struct{})
	changedRaw := strings.TrimSpace(r.FormValue("changed_workflows"))
	if changedRaw != "" {
		if !safeCSVValue.MatchString(changedRaw) {
			writeJSONError(w, "changed_workflows must be a safe comma-separated list", http.StatusBadRequest)
			return
		}
		for _, wfID := range dedupeOrderedStrings(bench.ParseCSVValues(changedRaw)) {
			changedWorkflowIDs[wfID] = struct{}{}
		}
	}

	items, err := bench.LoadDataset(baseRun.DatasetPath, 0)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to load dataset: %v", err), http.StatusBadRequest)
		return
	}
	requested := make(map[string]struct{}, len(itemIDs))
	for _, id := range itemIDs {
		requested[id] = struct{}{}
	}

	filtered := make([]bench.DatasetItem, 0, len(itemIDs))
	for _, item := range items {
		if _, ok := requested[item.ID]; ok {
			filtered = append(filtered, item)
		}
	}
	if len(filtered) == 0 {
		writeJSONError(w, "requested items were not found in dataset", http.StatusBadRequest)
		return
	}
	if len(filtered) != len(requested) {
		found := make(map[string]struct{}, len(filtered))
		for _, item := range filtered {
			found[item.ID] = struct{}{}
		}
		missing := make([]string, 0)
		for itemID := range requested {
			if _, ok := found[itemID]; !ok {
				missing = append(missing, itemID)
			}
		}
		sort.Strings(missing)
		writeJSONError(w, fmt.Sprintf("requested items not found in dataset: %s", strings.Join(missing, ",")), http.StatusBadRequest)
		return
	}
	if !s.ensureBenchmarkAdmission(w, admissionBypass, len(filtered)) {
		return
	}

	sourceParentJobByItem := make(map[string]string, len(filtered))
	for _, item := range filtered {
		detail, err := s.storage.GetBenchmarkRunItemDetail(baseRunID, item.ID)
		if err != nil {
			writeJSONError(w, fmt.Sprintf("failed to load benchmark item detail for %s: %v", item.ID, err), http.StatusBadRequest)
			return
		}
		sourceParentJobID := latestBenchmarkAttemptJobID(detail)
		if strings.TrimSpace(sourceParentJobID) == "" {
			writeJSONError(w, fmt.Sprintf("item %s has no source job_id in baseline run", item.ID), http.StatusBadRequest)
			return
		}
		sourceParentJobByItem[item.ID] = sourceParentJobID
	}

	concurrency := max(parseIntDefault(r.FormValue("concurrency"), 1), 1)
	maxNonLetterRetries := max(parseIntDefault(r.FormValue("max_non_letter_retries"), 1), 0)
	maxTransientRetries := max(parseIntDefault(r.FormValue("max_transient_retries"), 1), 0)

	startedAt := time.Now()
	replayRunID := fmt.Sprintf("replay-%s-%d", baseRunID, startedAt.UnixNano())
	plans := []benchmarkRunPlan{{
		Benchmark:          baseRun.Benchmark,
		Split:              baseRun.Split,
		DatasetPath:        baseRun.DatasetPath,
		ItemLimit:          len(filtered),
		Source:             "replay",
		WorkflowID:         workflowID,
		WorkflowName:       workflowDef.Name,
		WorkflowDefinition: workflowDef.Definition,
		RunID:              replayRunID,
		StartedAt:          startedAt,
		Items:              filtered,
		ItemResults:        make([]bench.ItemResult, len(filtered)),
		AdmissionBypass:    admissionBypass,
		Replay: &benchmarkReplaySpec{
			Mode:                  replayMode,
			BaseRunID:             baseRunID,
			ChangedWorkflowIDs:    changedWorkflowIDs,
			SourceParentJobByItem: sourceParentJobByItem,
		},
	}}
	if err := s.preflightValidateBenchmarkWorkflow(workflowDef, filtered[0]); err != nil {
		writeJSONError(w, fmt.Sprintf("Workflow validation failed before replay run: %v", err), http.StatusBadRequest)
		return
	}

	cfg := benchmarkRunRequest{
		Benchmarks:          []string{baseRun.Benchmark},
		Workflows:           []string{workflowID},
		Split:               baseRun.Split,
		Limit:               len(filtered),
		Source:              "replay",
		Concurrency:         concurrency,
		MaxNonLetterRetries: maxNonLetterRetries,
		MaxTransientRetries: maxTransientRetries,
		AdmissionBypass:     admissionBypass,
	}

	if err := s.launchBenchmarkRun("replay-items", 1, len(filtered), cfg, plans); err != nil {
		writeJSONError(w, err.Error(), http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"started":              true,
		"run_id":               replayRunID,
		"base_run_id":          baseRunID,
		"workflow_id":          workflowID,
		"item_count":           len(filtered),
		"replay_mode":          replayMode,
		"changed_workflow_ids": keysFromSet(changedWorkflowIDs),
		"admission_bypass":     admissionBypass,
	})
}

func parseReplayModeForm(raw string) (workflowruntime.ReplayMode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "best_effort", "best-effort":
		return workflowruntime.ReplayModeBestEffort, nil
	case "required", "strict":
		return workflowruntime.ReplayModeRequired, nil
	case "off", "none":
		return workflowruntime.ReplayModeOff, nil
	default:
		return "", fmt.Errorf("invalid replay mode %q (supported: best_effort, required, off)", raw)
	}
}

func latestBenchmarkAttemptJobID(detail *storage.BenchmarkRunItemDetail) string {
	if detail == nil {
		return ""
	}
	for i := len(detail.Attempts) - 1; i >= 0; i-- {
		jobID := strings.TrimSpace(detail.Attempts[i].JobID)
		if jobID != "" {
			return jobID
		}
	}
	return strings.TrimSpace(detail.Item.JobID)
}
