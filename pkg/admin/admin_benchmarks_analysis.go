package admin

import (
	"encoding/json"
	"log"
	"math"
	"sort"
	"strings"

	"github.com/alhasaniq/consortium/pkg/bench"
	"github.com/alhasaniq/consortium/pkg/storage"
)

// wrongAnswerCategory classifies where an incorrect answer went wrong in the pipeline.
type wrongAnswerCategory string

const (
	categoryAllStepsWrong         wrongAnswerCategory = "all_steps_wrong"
	categorySomeRightChildWrong   wrongAnswerCategory = "some_right_child_wrong"
	categoryAllRightChildWrong    wrongAnswerCategory = "all_right_child_wrong"
	categoryChildRightParentWrong wrongAnswerCategory = "child_right_parent_wrong"
	categoryUnclassified          wrongAnswerCategory = "unclassified"
)

type agentNodeAnswer struct {
	NodeID  string `json:"node_id"`
	Model   string `json:"model,omitempty"`
	Answer  string `json:"answer"`
	Correct bool   `json:"correct"`
	ParseOK bool   `json:"parse_ok"`
}

type wrongAnswerAnalysisItem struct {
	ItemID          string              `json:"item_id"`
	Subject         string              `json:"subject"`
	AnswerLabel     string              `json:"answer_label"`
	ParentPredicted string              `json:"parent_predicted"`
	ChildPredicted  string              `json:"child_predicted"`
	AgentAnswers    []agentNodeAnswer   `json:"agent_answers"`
	Category        wrongAnswerCategory `json:"category"`
	ParentJobID     string              `json:"parent_job_id,omitempty"`
	ChildJobID      string              `json:"child_job_id,omitempty"`
	Flagged         bool                `json:"flagged"`
	FlagReason      string              `json:"flag_reason,omitempty"`
}

type wrongAnswerSummary struct {
	TotalIncorrect        int     `json:"total_incorrect"`
	AllStepsWrong         int     `json:"all_steps_wrong"`
	SomeRightChildWrong   int     `json:"some_right_child_wrong"`
	AllRightChildWrong    int     `json:"all_right_child_wrong"`
	ChildRightParentWrong int     `json:"child_right_parent_wrong"`
	Unclassified          int     `json:"unclassified"`
	FlaggedItems          int     `json:"flagged_items"`
	RawAccuracy           float64 `json:"raw_accuracy"`
	AdjustedAccuracy      float64 `json:"adjusted_accuracy"`
}

type wrongAnswerAnalysisResponse struct {
	RunID       string                           `json:"run_id"`
	Benchmark   string                           `json:"benchmark"`
	Split       string                           `json:"split"`
	WorkflowID  string                           `json:"workflow_id"`
	Summary     wrongAnswerSummary               `json:"summary"`
	Items       []wrongAnswerAnalysisItem        `json:"items"`
	TotalItems  int                              `json:"total_items,omitempty"`
	Page        int                              `json:"page,omitempty"`
	PageSize    int                              `json:"page_size,omitempty"`
	HasMore     bool                             `json:"has_more,omitempty"`
	Performance benchmarkPerformanceView         `json:"performance"`
	Diagnostics benchmarkAnalysisDiagnosticsView `json:"diagnostics"`
}

type benchmarkNodePerformanceStats struct {
	Samples          int     `json:"samples"`
	ParseOK          int     `json:"parse_ok"`
	Correct          int     `json:"correct"`
	Accuracy         float64 `json:"accuracy"`
	ParseRate        float64 `json:"parse_rate"`
	TotalRetries     int     `json:"total_retries"`
	ItemsWithRetries int     `json:"items_with_retries"`
	AvgRetries       float64 `json:"avg_retries"`
	TotalLatencyMS   float64 `json:"total_latency_ms"`
	AvgLatencyMS     float64 `json:"avg_latency_ms"`
	P95LatencyMS     float64 `json:"p95_latency_ms"`
	TotalCostUSD     float64 `json:"total_cost_usd"`
	AvgCostUSD       float64 `json:"avg_cost_usd"`
}

type benchmarkAgentModelPerformance struct {
	Model string `json:"model"`
	benchmarkNodePerformanceStats
}

type benchmarkAggregationNodePerformance struct {
	NodeID string   `json:"node_id"`
	Models []string `json:"models,omitempty"`
	benchmarkNodePerformanceStats
}

type benchmarkPerformanceView struct {
	TotalItems         int                                   `json:"total_items"`
	ItemsWithChildData int                                   `json:"items_with_child_data"`
	AgentModels        []benchmarkAgentModelPerformance      `json:"agent_models"`
	AggregationNodes   []benchmarkAggregationNodePerformance `json:"aggregation_nodes"`
}

type benchmarkAnalysisItemDiagnostics struct {
	ItemID        string  `json:"item_id"`
	Subject       string  `json:"subject"`
	ParentJobID   string  `json:"parent_job_id,omitempty"`
	ChildJobID    string  `json:"child_job_id,omitempty"`
	ParentLatency float64 `json:"parent_latency_ms"`
	ChildLatency  float64 `json:"child_latency_ms"`
	TotalLatency  float64 `json:"total_latency_ms"`
	ParentRetries int     `json:"parent_retries"`
	ChildRetries  int     `json:"child_retries"`
	TotalRetries  int     `json:"total_retries"`
}

type benchmarkAnalysisDiagnosticsView struct {
	TopN           int                                `json:"top_n"`
	SlowestItems   []benchmarkAnalysisItemDiagnostics `json:"slowest_items"`
	MostRetryItems []benchmarkAnalysisItemDiagnostics `json:"most_retries_items"`
}

type benchmarkNodeAttemptUsage struct {
	totalLatency float64
	totalCost    float64
	maxAttempt   int
	hasAttempts  bool
}

type benchmarkChildAnalysisData struct {
	jobID string
	nodes []storage.WorkflowNode
}

type benchmarkNodePerfAccumulator struct {
	samples          int
	parseOK          int
	correct          int
	totalRetries     int
	itemsWithRetries int
	totalLatencyMS   float64
	totalCostUSD     float64
	latenciesMS      []float64
}

func (a *benchmarkNodePerfAccumulator) addSample(parseOK, correct bool, retries int, latencyMS, costUSD float64) {
	a.samples++
	if parseOK {
		a.parseOK++
	}
	if correct {
		a.correct++
	}
	if retries > 0 {
		a.totalRetries += retries
		a.itemsWithRetries++
	}
	a.totalLatencyMS += latencyMS
	a.totalCostUSD += costUSD
	a.latenciesMS = append(a.latenciesMS, latencyMS)
}

func (a *benchmarkNodePerfAccumulator) toView() benchmarkNodePerformanceStats {
	view := benchmarkNodePerformanceStats{
		Samples:          a.samples,
		ParseOK:          a.parseOK,
		Correct:          a.correct,
		TotalRetries:     a.totalRetries,
		ItemsWithRetries: a.itemsWithRetries,
		TotalLatencyMS:   a.totalLatencyMS,
		TotalCostUSD:     a.totalCostUSD,
	}
	if a.samples > 0 {
		view.Accuracy = float64(a.correct) / float64(a.samples)
		view.ParseRate = float64(a.parseOK) / float64(a.samples)
		view.AvgRetries = float64(a.totalRetries) / float64(a.samples)
		view.AvgLatencyMS = a.totalLatencyMS / float64(a.samples)
		view.AvgCostUSD = a.totalCostUSD / float64(a.samples)
		view.P95LatencyMS = percentileFloat64(a.latenciesMS, 95)
	}
	return view
}

type benchmarkAggregationPerfAccumulator struct {
	benchmarkNodePerfAccumulator
	models map[string]struct{}
}

type benchmarkPerformanceCollector struct {
	format             bench.BenchmarkFormat
	totalItems         int
	itemsWithChildData int
	agentModels        map[string]*benchmarkNodePerfAccumulator
	aggregationNodes   map[string]*benchmarkAggregationPerfAccumulator
}

type benchmarkAnalysisDiagnosticsCollector struct {
	topN  int
	items []benchmarkAnalysisItemDiagnostics
}

func newBenchmarkPerformanceCollector(format bench.BenchmarkFormat, totalItems int) *benchmarkPerformanceCollector {
	return &benchmarkPerformanceCollector{
		format:           format,
		totalItems:       totalItems,
		agentModels:      make(map[string]*benchmarkNodePerfAccumulator),
		aggregationNodes: make(map[string]*benchmarkAggregationPerfAccumulator),
	}
}

func summarizeNodeAttemptUsage(attempts []storage.NodeExecutionAttempt) map[string]benchmarkNodeAttemptUsage {
	usage := make(map[string]benchmarkNodeAttemptUsage)
	for _, attempt := range attempts {
		entry := usage[attempt.NodeID]
		entry.hasAttempts = true
		entry.totalLatency += attempt.LatencyMs
		entry.totalCost += attempt.Cost
		if attempt.Attempt > entry.maxAttempt {
			entry.maxAttempt = attempt.Attempt
		}
		usage[attempt.NodeID] = entry
	}
	return usage
}

func summarizeNodeUsageTotals(usage map[string]benchmarkNodeAttemptUsage) (latencyMS float64, retries int) {
	for _, summary := range usage {
		if !summary.hasAttempts {
			continue
		}
		latencyMS += summary.totalLatency
		retries += maxNodeRetries(summary.maxAttempt-1, 0)
	}
	return latencyMS, retries
}

func summarizeNodeFallbackTotals(nodes []storage.WorkflowNode) (latencyMS float64, retries int) {
	for _, node := range nodes {
		latencyMS += node.LatencyMs
		retries += maxNodeRetries(node.AttemptNumber-1, 0)
	}
	return latencyMS, retries
}

func nodeUsageFor(node storage.WorkflowNode, usage map[string]benchmarkNodeAttemptUsage) (latencyMS, costUSD float64, retries int) {
	if summary, ok := usage[node.NodeID]; ok && summary.hasAttempts {
		return summary.totalLatency, summary.totalCost, maxNodeRetries(summary.maxAttempt-1, 0)
	}
	return node.LatencyMs, node.Cost, maxNodeRetries(node.AttemptNumber-1, 0)
}

func newBenchmarkAnalysisDiagnosticsCollector(topN int) *benchmarkAnalysisDiagnosticsCollector {
	if topN < 1 {
		topN = 10
	}
	return &benchmarkAnalysisDiagnosticsCollector{
		topN:  topN,
		items: []benchmarkAnalysisItemDiagnostics{},
	}
}

func (c *benchmarkAnalysisDiagnosticsCollector) add(item benchmarkAnalysisItemDiagnostics) {
	c.items = append(c.items, item)
}

func (c *benchmarkAnalysisDiagnosticsCollector) toView() benchmarkAnalysisDiagnosticsView {
	slowest := append([]benchmarkAnalysisItemDiagnostics(nil), c.items...)
	sort.SliceStable(slowest, func(i, j int) bool {
		if slowest[i].TotalLatency != slowest[j].TotalLatency {
			return slowest[i].TotalLatency > slowest[j].TotalLatency
		}
		if slowest[i].TotalRetries != slowest[j].TotalRetries {
			return slowest[i].TotalRetries > slowest[j].TotalRetries
		}
		return slowest[i].ItemID < slowest[j].ItemID
	})

	mostRetries := append([]benchmarkAnalysisItemDiagnostics(nil), c.items...)
	sort.SliceStable(mostRetries, func(i, j int) bool {
		if mostRetries[i].TotalRetries != mostRetries[j].TotalRetries {
			return mostRetries[i].TotalRetries > mostRetries[j].TotalRetries
		}
		if mostRetries[i].ChildRetries != mostRetries[j].ChildRetries {
			return mostRetries[i].ChildRetries > mostRetries[j].ChildRetries
		}
		if mostRetries[i].ParentRetries != mostRetries[j].ParentRetries {
			return mostRetries[i].ParentRetries > mostRetries[j].ParentRetries
		}
		if mostRetries[i].TotalLatency != mostRetries[j].TotalLatency {
			return mostRetries[i].TotalLatency > mostRetries[j].TotalLatency
		}
		return mostRetries[i].ItemID < mostRetries[j].ItemID
	})

	limit := c.topN
	if limit > len(slowest) {
		limit = len(slowest)
	}
	slowest = slowest[:limit]

	if c.topN > len(mostRetries) {
		limit = len(mostRetries)
	} else {
		limit = c.topN
	}
	mostRetries = mostRetries[:limit]

	return benchmarkAnalysisDiagnosticsView{
		TopN:           c.topN,
		SlowestItems:   slowest,
		MostRetryItems: mostRetries,
	}
}

func (c *benchmarkPerformanceCollector) ingestItem(answerLabel string, childNodes []storage.WorkflowNode, childAttempts []storage.NodeExecutionAttempt, countChildData bool) {
	if len(childNodes) == 0 {
		return
	}
	if countChildData {
		c.itemsWithChildData++
	}
	attemptUsage := summarizeNodeAttemptUsage(childAttempts)

	// Agent nodes are top-level prompt nodes in the child workflow.
	for _, node := range childNodes {
		if node.ParentNodeID != "" || node.NodeType != "prompt" {
			continue
		}
		answer, parseOK := parseAnswer(c.format, node.Output)
		correct := parseOK && answersMatch(c.format, answer, answerLabel)
		latencyMS, costUSD, retries := nodeUsageFor(node, attemptUsage)
		model := strings.TrimSpace(node.Model)
		if model == "" {
			model = "(unknown)"
		}
		acc := c.agentModels[model]
		if acc == nil {
			acc = &benchmarkNodePerfAccumulator{}
			c.agentModels[model] = acc
		}
		acc.addSample(parseOK, correct, retries, latencyMS, costUSD)
	}

	// Aggregation nodes are top-level result/aggregator nodes with their child
	// aggregation call nodes (parent_node_id references the top-level node).
	for _, node := range childNodes {
		if node.ParentNodeID != "" || (node.NodeType != "result" && node.NodeType != "aggregator") {
			continue
		}
		if isBenchmarkOutputPackagingNode(node) {
			continue
		}
		answer, parseOK := parseAnswer(c.format, node.Output)
		correct := parseOK && answersMatch(c.format, answer, answerLabel)
		totalLatencyMS := 0.0
		totalCostUSD := 0.0
		totalRetries := 0

		acc := c.aggregationNodes[node.NodeID]
		if acc == nil {
			acc = &benchmarkAggregationPerfAccumulator{
				models: make(map[string]struct{}),
			}
			c.aggregationNodes[node.NodeID] = acc
		}

		for _, component := range childNodes {
			if component.NodeID != node.NodeID && component.ParentNodeID != node.NodeID {
				continue
			}
			latencyMS, costUSD, retries := nodeUsageFor(component, attemptUsage)
			totalLatencyMS += latencyMS
			totalCostUSD += costUSD
			totalRetries += retries
			if model := strings.TrimSpace(component.Model); model != "" {
				acc.models[model] = struct{}{}
			}
		}
		acc.addSample(parseOK, correct, totalRetries, totalLatencyMS, totalCostUSD)
	}
}

func isBenchmarkOutputPackagingNode(node storage.WorkflowNode) bool {
	if strings.TrimSpace(node.Metadata) == "" {
		return false
	}
	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(node.Metadata), &metadata); err != nil {
		return false
	}
	packaging, _ := metadata["benchmark_output_packaging"].(bool)
	return packaging
}

func (c *benchmarkPerformanceCollector) toView() benchmarkPerformanceView {
	view := benchmarkPerformanceView{
		TotalItems:         c.totalItems,
		ItemsWithChildData: c.itemsWithChildData,
	}

	agentModels := make([]string, 0, len(c.agentModels))
	for model := range c.agentModels {
		agentModels = append(agentModels, model)
	}
	sort.Strings(agentModels)
	for _, model := range agentModels {
		view.AgentModels = append(view.AgentModels, benchmarkAgentModelPerformance{
			Model:                         model,
			benchmarkNodePerformanceStats: c.agentModels[model].toView(),
		})
	}

	aggregationNodeIDs := make([]string, 0, len(c.aggregationNodes))
	for nodeID := range c.aggregationNodes {
		aggregationNodeIDs = append(aggregationNodeIDs, nodeID)
	}
	sort.Strings(aggregationNodeIDs)
	for _, nodeID := range aggregationNodeIDs {
		acc := c.aggregationNodes[nodeID]
		models := keysFromSet(acc.models)
		view.AggregationNodes = append(view.AggregationNodes, benchmarkAggregationNodePerformance{
			NodeID:                        nodeID,
			Models:                        models,
			benchmarkNodePerformanceStats: acc.toView(),
		})
	}
	return view
}

func percentileFloat64(values []float64, p int) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	rank := int(math.Ceil(float64(p)/100.0*float64(len(sorted)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}

func maxNodeRetries(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func keysFromSet(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// classifyWrongAnswer determines where in the pipeline an incorrect answer went wrong.
func classifyWrongAnswer(format bench.BenchmarkFormat, goldLabel string, childResultAnswer string, childResultParseOK bool, agentAnswers []agentNodeAnswer) wrongAnswerCategory {
	// Category 4: Child workflow returned the correct answer but parent extracted wrong.
	if childResultParseOK && answersMatch(format, childResultAnswer, goldLabel) {
		return categoryChildRightParentWrong
	}

	agentCorrectCount := 0
	agentParsedCount := 0
	for _, a := range agentAnswers {
		if a.ParseOK {
			agentParsedCount++
			if a.Correct {
				agentCorrectCount++
			}
		}
	}

	if agentParsedCount == 0 {
		return categoryUnclassified
	}

	// Category 3: All agents right but aggregation returned wrong.
	if agentCorrectCount == agentParsedCount {
		return categoryAllRightChildWrong
	}

	// Category 2: Some agents right but aggregation picked wrong.
	if agentCorrectCount > 0 {
		return categorySomeRightChildWrong
	}

	// Category 1: Every agent answered wrong.
	return categoryAllStepsWrong
}

func (s *Server) loadBenchmarkChildDataByParentJobIDs(parentJobIDs []string) (map[string]benchmarkChildAnalysisData, error) {
	childByParent := make(map[string]benchmarkChildAnalysisData)
	if len(parentJobIDs) == 0 {
		return childByParent, nil
	}

	latestChildren, err := s.storage.ListLatestChildExecutionsByParentIDs(parentJobIDs)
	if err != nil {
		return nil, err
	}
	if len(latestChildren) == 0 {
		return childByParent, nil
	}

	seenChildJobs := make(map[string]struct{}, len(latestChildren))
	childJobIDs := make([]string, 0, len(latestChildren))
	for parentJobID, child := range latestChildren {
		childByParent[parentJobID] = benchmarkChildAnalysisData{jobID: child.ID}
		if _, exists := seenChildJobs[child.ID]; exists {
			continue
		}
		seenChildJobs[child.ID] = struct{}{}
		childJobIDs = append(childJobIDs, child.ID)
	}

	childNodesByJob, err := s.storage.ListWorkflowNodesByJobIDs(childJobIDs)
	if err != nil {
		return nil, err
	}
	for parentJobID, data := range childByParent {
		data.nodes = childNodesByJob[data.jobID]
		childByParent[parentJobID] = data
	}
	return childByParent, nil
}

// parseAnswer parses an answer string using the appropriate parser for the benchmark format.
func parseAnswer(format bench.BenchmarkFormat, raw string) (string, bool) {
	if format == bench.BenchmarkFormatMathAnswer {
		return bench.ParseMathAnswer(strings.TrimSpace(raw))
	}
	return bench.ParseAnswerLabel(strings.TrimSpace(raw))
}

// answersMatch checks whether a predicted answer matches the gold answer
// using the appropriate comparison for the benchmark format.
func answersMatch(format bench.BenchmarkFormat, predicted, gold string) bool {
	if format == bench.BenchmarkFormatMathAnswer {
		return bench.MathAnswersEquivalent(predicted, gold)
	}
	return strings.EqualFold(predicted, gold)
}

type itemComparisonResult struct {
	ItemID                 string              `json:"item_id"`
	Subject                string              `json:"subject"`
	AnswerLabel            string              `json:"answer_label"`
	BasePredicted          string              `json:"base_predicted,omitempty"`
	CandidatePredicted     string              `json:"candidate_predicted,omitempty"`
	BaseFailureReason      string              `json:"base_failure_reason,omitempty"`
	CandidateFailureReason string              `json:"candidate_failure_reason,omitempty"`
	BaseCategory           wrongAnswerCategory `json:"base_category,omitempty"`
	CandidateCategory      wrongAnswerCategory `json:"candidate_category,omitempty"`
}

// extractChildWorkflowAnswers scans child workflow nodes and returns agent answers
// and the child result node's parsed answer. Skips sub-nodes (aggregation children).
func extractChildWorkflowAnswers(format bench.BenchmarkFormat, childNodes []storage.WorkflowNode, answerLabel string) (agentAnswers []agentNodeAnswer, childResultAnswer string, childResultParseOK bool) {
	for _, node := range childNodes {
		if node.ParentNodeID != "" {
			continue
		}
		switch node.NodeType {
		case "prompt":
			answer, parseOK := parseAnswer(format, node.Output)
			agentAnswers = append(agentAnswers, agentNodeAnswer{
				NodeID:  node.NodeID,
				Model:   node.Model,
				Answer:  answer,
				Correct: parseOK && answersMatch(format, answer, answerLabel),
				ParseOK: parseOK,
			})
		case "result":
			childResultAnswer, childResultParseOK = parseAnswer(format, node.Output)
		}
	}
	return
}

func (s *Server) classifyBenchmarkItemWithChildData(format bench.BenchmarkFormat, item storage.BenchmarkRunItem, childDataByParent map[string]benchmarkChildAnalysisData) wrongAnswerCategory {
	if strings.TrimSpace(item.FailureReason) != "" {
		return categoryUnclassified
	}
	parentJobID := strings.TrimSpace(item.JobID)
	if parentJobID == "" {
		return categoryUnclassified
	}

	if childDataByParent != nil {
		childData, ok := childDataByParent[parentJobID]
		if !ok || len(childData.nodes) == 0 {
			return categoryUnclassified
		}
		agentAnswers, childResultAnswer, childResultParseOK := extractChildWorkflowAnswers(format, childData.nodes, item.AnswerLabel)
		return classifyWrongAnswer(format, item.AnswerLabel, childResultAnswer, childResultParseOK, agentAnswers)
	}

	children, err := s.storage.GetChildExecutions(parentJobID)
	if err != nil || len(children) == 0 {
		return categoryUnclassified
	}

	child := children[len(children)-1]
	childNodes, _ := s.storage.GetWorkflowNodes(child.ID)
	agentAnswers, childResultAnswer, childResultParseOK := extractChildWorkflowAnswers(format, childNodes, item.AnswerLabel)
	return classifyWrongAnswer(format, item.AnswerLabel, childResultAnswer, childResultParseOK, agentAnswers)
}

// loadDatasetFlagMap returns a map of item_id -> DatasetFlag for active flags
// matching the given benchmark/split and item list.
func (s *Server) loadDatasetFlagMap(benchmark, split string, items []storage.BenchmarkRunItem) map[string]storage.DatasetFlag {
	if len(items) == 0 {
		return nil
	}
	itemIDs := make([]string, len(items))
	for i, it := range items {
		itemIDs[i] = it.ItemID
	}
	flags, err := s.storage.ListFlagsByItems(benchmark, split, itemIDs)
	if err != nil {
		log.Printf("Warning: failed to load dataset flags for %s/%s: %v", benchmark, split, err)
		return nil
	}
	m := make(map[string]storage.DatasetFlag, len(flags))
	for _, f := range flags {
		m[f.ItemID] = f
	}
	return m
}
