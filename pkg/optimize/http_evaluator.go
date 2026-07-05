package optimize

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type HTTPBenchmarkEvaluator struct {
	Client       *HTTPClient
	PollInterval time.Duration

	mu                 sync.Mutex
	lastWorkflowID     string
	lastRequestedRunID string
}

func NewHTTPBenchmarkEvaluator(baseURL string, timeout time.Duration) *HTTPBenchmarkEvaluator {
	return &HTTPBenchmarkEvaluator{
		Client:       NewHTTPClient(baseURL, timeout),
		PollInterval: 2 * time.Second,
	}
}

func (e *HTTPBenchmarkEvaluator) RunBenchmark(ctx context.Context, req RunBenchmarkRequest) (string, error) {
	if e.Client == nil {
		return "", fmt.Errorf("http client is nil")
	}
	if strings.TrimSpace(req.WorkflowID) == "" {
		return "", fmt.Errorf("workflow_id is required")
	}
	if strings.TrimSpace(req.Benchmark) == "" {
		return "", fmt.Errorf("benchmark is required")
	}
	if req.Concurrency < 1 {
		req.Concurrency = 1
	}

	if err := e.waitForRunnerSlot(ctx); err != nil {
		return "", err
	}

	form := url.Values{}
	form.Set("benchmarks", req.Benchmark)
	form.Set("workflows", req.WorkflowID)
	form.Set("run_set", "custom")
	form.Set("split", defaultSplit(req.Split))
	form.Set("limit", strconv.Itoa(max(req.ItemLimit, 0)))
	form.Set("concurrency", strconv.Itoa(req.Concurrency))
	form.Set("max_non_letter_retries", "1")
	form.Set("max_transient_retries", "1")
	form.Set("pause_on_fatal", "false")
	if req.Meta != nil {
		if source := strings.TrimSpace(req.Meta["source"]); source != "" {
			form.Set("source", source)
		}
		if optRunID := strings.TrimSpace(req.Meta["opt_run_id"]); optRunID != "" {
			form.Set("opt_run_id", optRunID)
		}
		if optOrganismID := strings.TrimSpace(req.Meta["opt_organism_id"]); optOrganismID != "" {
			form.Set("opt_organism_id", optOrganismID)
		}
		if organismID := strings.TrimSpace(req.Meta["organism_id"]); organismID != "" {
			form.Set("opt_organism_id", organismID)
		}
		if scope := strings.TrimSpace(req.Meta["opt_scope"]); scope != "" {
			form.Set("opt_scope", scope)
		}
		if sampleMode := strings.TrimSpace(req.Meta["sample_mode"]); sampleMode != "" {
			form.Set("sample_mode", sampleMode)
		}
		if sampleSeed := strings.TrimSpace(req.Meta["sample_seed"]); sampleSeed != "" {
			form.Set("sample_seed", sampleSeed)
		}
	}

	var startResp map[string]interface{}
	if err := e.Client.PostForm(ctx, "/api/admin/benchmarks/run", form, &startResp); err != nil {
		return "", err
	}

	runID := ""
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		state, err := e.runnerStatus(ctx)
		if err == nil {
			running := boolFromMap(state, "running")
			currentWorkflow := stringFromMap(state, "current_workflow")
			currentRunID := stringFromMap(state, "current_run_id")
			if running && strings.TrimSpace(currentRunID) != "" {
				if currentWorkflow == "" || strings.EqualFold(currentWorkflow, req.WorkflowID) {
					runID = currentRunID
					break
				}
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(e.pollInterval()):
		}
	}

	e.mu.Lock()
	e.lastWorkflowID = req.WorkflowID
	e.lastRequestedRunID = runID
	e.mu.Unlock()
	return runID, nil
}

func (e *HTTPBenchmarkEvaluator) RunBatchBenchmark(ctx context.Context, req RunBatchBenchmarkRequest) (map[string]string, error) {
	if e.Client == nil {
		return nil, fmt.Errorf("http client is nil")
	}
	if strings.TrimSpace(req.Benchmark) == "" {
		return nil, fmt.Errorf("benchmark is required")
	}
	workflowIDs := dedupeTrimmed(req.WorkflowIDs)
	if len(workflowIDs) == 0 {
		return nil, fmt.Errorf("at least one workflow_id is required")
	}
	if req.Concurrency < 1 {
		req.Concurrency = 1
	}

	if err := e.waitForRunnerSlot(ctx); err != nil {
		return nil, err
	}

	before := make(map[string]string, len(workflowIDs))
	for _, workflowID := range workflowIDs {
		runID, err := e.latestRunIDForWorkflow(ctx, workflowID, req.Benchmark, 3)
		if err == nil && strings.TrimSpace(runID) != "" {
			before[workflowID] = runID
		}
	}

	form := url.Values{}
	form.Set("benchmarks", req.Benchmark)
	form.Set("workflows", strings.Join(workflowIDs, ","))
	form.Set("run_set", "custom")
	form.Set("split", defaultSplit(req.Split))
	form.Set("limit", strconv.Itoa(max(req.ItemLimit, 0)))
	form.Set("concurrency", strconv.Itoa(req.Concurrency))
	form.Set("max_non_letter_retries", "1")
	form.Set("max_transient_retries", "1")
	form.Set("pause_on_fatal", "false")
	if req.Meta != nil {
		if source := strings.TrimSpace(req.Meta["source"]); source != "" {
			form.Set("source", source)
		}
		if optRunID := strings.TrimSpace(req.Meta["opt_run_id"]); optRunID != "" {
			form.Set("opt_run_id", optRunID)
		}
		if optOrganismID := strings.TrimSpace(req.Meta["opt_organism_id"]); optOrganismID != "" {
			form.Set("opt_organism_id", optOrganismID)
		}
		if organismID := strings.TrimSpace(req.Meta["organism_id"]); organismID != "" {
			form.Set("opt_organism_id", organismID)
		}
		if scope := strings.TrimSpace(req.Meta["opt_scope"]); scope != "" {
			form.Set("opt_scope", scope)
		}
		if sampleMode := strings.TrimSpace(req.Meta["sample_mode"]); sampleMode != "" {
			form.Set("sample_mode", sampleMode)
		}
		if sampleSeed := strings.TrimSpace(req.Meta["sample_seed"]); sampleSeed != "" {
			form.Set("sample_seed", sampleSeed)
		}
	}

	var startResp map[string]interface{}
	if err := e.Client.PostForm(ctx, "/api/admin/benchmarks/run", form, &startResp); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(45 * time.Second)
	resolved := make(map[string]string, len(workflowIDs))
	for len(resolved) < len(workflowIDs) && time.Now().Before(deadline) {
		for _, workflowID := range workflowIDs {
			if _, ok := resolved[workflowID]; ok {
				continue
			}
			latest, err := e.latestRunIDForWorkflow(ctx, workflowID, req.Benchmark, 5)
			if err != nil || strings.TrimSpace(latest) == "" {
				continue
			}
			if previous := strings.TrimSpace(before[workflowID]); previous != "" && previous == latest {
				continue
			}
			resolved[workflowID] = latest
		}
		if len(resolved) == len(workflowIDs) {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(e.pollInterval()):
		}
	}
	if len(resolved) != len(workflowIDs) {
		missing := make([]string, 0, len(workflowIDs)-len(resolved))
		for _, workflowID := range workflowIDs {
			if _, ok := resolved[workflowID]; !ok {
				missing = append(missing, workflowID)
			}
		}
		return nil, fmt.Errorf("failed to resolve benchmark run IDs for workflows: %s", strings.Join(missing, ", "))
	}
	return resolved, nil
}

func (e *HTTPBenchmarkEvaluator) ReplayBenchmark(ctx context.Context, req ReplayBenchmarkRequest) (string, error) {
	if e.Client == nil {
		return "", fmt.Errorf("http client is nil")
	}
	baseRunID := strings.TrimSpace(req.BaseRunID)
	if baseRunID == "" {
		return "", fmt.Errorf("base_run_id is required")
	}
	workflowID := strings.TrimSpace(req.WorkflowID)
	if workflowID == "" {
		return "", fmt.Errorf("workflow_id is required")
	}
	items := dedupeTrimmed(req.Items)
	if len(items) == 0 {
		return "", fmt.Errorf("at least one replay item is required")
	}
	if req.Concurrency < 1 {
		req.Concurrency = 1
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = "best_effort"
	}
	if err := e.waitForRunnerSlot(ctx); err != nil {
		return "", err
	}

	form := url.Values{}
	form.Set("items", strings.Join(items, ","))
	form.Set("workflow", workflowID)
	form.Set("mode", mode)
	form.Set("concurrency", strconv.Itoa(req.Concurrency))
	if changed := dedupeTrimmed(req.ChangedWorkflows); len(changed) > 0 {
		form.Set("changed_workflows", strings.Join(changed, ","))
	}

	path := "/api/admin/benchmarks/" + url.PathEscape(baseRunID) + "/replay-items"
	var response map[string]interface{}
	if err := e.Client.PostForm(ctx, path, form, &response); err != nil {
		return "", err
	}
	runID := strings.TrimSpace(stringFromMap(response, "run_id", "RunID"))
	if runID == "" {
		return "", fmt.Errorf("replay benchmark response missing run_id")
	}
	e.mu.Lock()
	e.lastWorkflowID = workflowID
	e.lastRequestedRunID = runID
	e.mu.Unlock()
	return runID, nil
}

func (e *HTTPBenchmarkEvaluator) WaitForCompletion(ctx context.Context) error {
	for {
		state, err := e.runnerStatus(ctx)
		if err != nil {
			return err
		}
		if !boolFromMap(state, "running") {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(e.pollInterval()):
		}
	}
}

func (e *HTTPBenchmarkEvaluator) GetRunSummary(ctx context.Context, runID string) (*Fitness, error) {
	resolvedID, err := e.resolveRunID(ctx, runID)
	if err != nil {
		return nil, err
	}
	var detail map[string]interface{}
	if err := e.Client.GetJSON(ctx, "/api/admin/benchmarks/"+url.PathEscape(resolvedID), nil, &detail); err != nil {
		return nil, err
	}

	runRaw := mapFromMap(detail, "run", "Run")
	if runRaw == nil {
		return nil, fmt.Errorf("benchmark detail response missing run payload")
	}

	flagged := IntFromMap(detail, "flagged_items", "FlaggedItems")
	adjusted := FloatFromMap(detail, "adjusted_accuracy", "AdjustedAccuracy")
	if adjusted == 0 {
		adjusted = FloatFromMap(runRaw, "accuracy", "Accuracy")
	}

	totalItems := IntFromMap(runRaw, "total_items", "TotalItems")
	totalCost := FloatFromMap(runRaw, "total_cost_usd", "TotalCostUSD")
	if totalCost <= 0 {
		totalCost = FloatFromMap(detail, "total_cost_usd", "total_cost", "TotalCostUSD")
	}
	costPerItem := 0.0
	if totalItems > 0 {
		costPerItem = totalCost / float64(totalItems)
	}

	fitness := &Fitness{
		Accuracy:         FloatFromMap(runRaw, "accuracy", "Accuracy"),
		AdjustedAccuracy: adjusted,
		ParseRate:        FloatFromMap(runRaw, "parse_rate", "ParseRate"),
		TotalCost:        totalCost,
		CostPerItem:      costPerItem,
		AvgLatencyMS:     FloatFromMap(runRaw, "avg_latency_ms", "AvgLatencyMS"),
		P95LatencyMS:     FloatFromMap(runRaw, "p95_latency_ms", "P95LatencyMS"),
		FailedItems:      IntFromMap(runRaw, "failed_items", "FailedItems"),
		TotalItems:       totalItems,
		FlaggedItems:     flagged,
	}
	return fitness, nil
}

func (e *HTTPBenchmarkEvaluator) GetFailureCases(ctx context.Context, runID string, limit int) ([]FailureCase, error) {
	resolvedID, err := e.resolveRunID(ctx, runID)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	if limit > 0 {
		query.Set("page_size", strconv.Itoa(limit))
	}
	var response map[string]interface{}
	if err := e.Client.GetJSON(ctx, "/api/admin/benchmarks/"+url.PathEscape(resolvedID)+"/analysis", query, &response); err != nil {
		return nil, err
	}
	items := listFromMap(response, "items", "Items")
	cases := make([]FailureCase, 0, len(items))
	for _, raw := range items {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		analysisCategory := failureCaseCategory(stringFromMap(item, "category", "Category"))
		failure := FailureCase{
			ItemID:         stringFromMap(item, "item_id", "ItemID"),
			Question:       stringFromMap(item, "question", "Question"),
			CorrectAnswer:  stringFromMap(item, "answer_label", "answer", "CorrectAnswer"),
			Predicted:      stringFromMap(item, "parent_predicted", "predicted", "Predicted"),
			ChildPredicted: stringFromMap(item, "child_predicted", "ChildPredicted"),
			FailureReason:  stringFromMap(item, "failure_reason", "FailureReason"),
			Category:       analysisCategory,
			RawOutput:      stringFromMap(item, "raw_output", "RawOutput"),
			ParentJobID:    stringFromMap(item, "parent_job_id", "ParentJobID"),
			ChildJobID:     stringFromMap(item, "child_job_id", "ChildJobID"),
			Flagged:        boolFromMapKeys(item, "flagged", "Flagged"),
			FlagReason:     stringFromMap(item, "flag_reason", "FlagReason"),
			AgentAnswers:   parseFailureAgentAnswers(listFromMap(item, "agent_answers", "AgentAnswers")),
		}
		if strings.TrimSpace(failure.FailureReason) == "" {
			failure.FailureReason = analysisCategory
		}
		failure.Diagnosis = failureCaseDiagnosis(failure)
		cases = append(cases, failure)
		if limit > 0 && len(cases) >= limit {
			break
		}
	}
	return cases, nil
}

func (e *HTTPBenchmarkEvaluator) EnrichFailureCases(ctx context.Context, runID string, cases []FailureCase) ([]FailureCase, error) {
	if len(cases) == 0 {
		return nil, nil
	}
	resolvedID, err := e.resolveRunID(ctx, runID)
	if err != nil {
		return append([]FailureCase(nil), cases...), err
	}
	enriched := append([]FailureCase(nil), cases...)
	for i := range enriched {
		itemID := strings.TrimSpace(enriched[i].ItemID)
		if itemID == "" {
			continue
		}
		detail, err := e.fetchBenchmarkItemDetail(ctx, resolvedID, itemID)
		if err != nil {
			continue
		}
		mergeFailureCaseWithItemDetail(&enriched[i], detail)
		if strings.TrimSpace(enriched[i].Diagnosis) == "" {
			enriched[i].Diagnosis = failureCaseDiagnosis(enriched[i])
		}
	}
	return enriched, nil
}

// GetSuccessCases implements SuccessCaseProvider by fetching correctly-answered
// benchmark items from the run detail endpoint.
func (e *HTTPBenchmarkEvaluator) GetSuccessCases(ctx context.Context, runID string, limit int) ([]SuccessExample, error) {
	resolvedID, err := e.resolveRunID(ctx, runID)
	if err != nil {
		return nil, err
	}
	fetchLimit := limit * 3 // fetch more to filter for correct items
	if fetchLimit < 20 {
		fetchLimit = 20
	}
	query := url.Values{}
	query.Set("page_size", strconv.Itoa(fetchLimit))
	var response map[string]interface{}
	if err := e.Client.GetJSON(ctx, "/api/admin/benchmarks/"+url.PathEscape(resolvedID), query, &response); err != nil {
		return nil, err
	}
	items := listFromMap(response, "Items", "items")
	cases := make([]SuccessExample, 0, limit)
	for _, raw := range items {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if !boolFromMapKeys(item, "Correct", "correct") {
			continue
		}
		question := stringFromMap(item, "Question", "question")
		if strings.TrimSpace(question) == "" {
			continue
		}
		cases = append(cases, SuccessExample{
			ItemID:        stringFromMap(item, "ItemID", "item_id"),
			Question:      question,
			CorrectAnswer: stringFromMap(item, "AnswerLabel", "answer_label", "answer"),
			Predicted:     stringFromMap(item, "Predicted", "predicted"),
		})
		if limit > 0 && len(cases) >= limit {
			break
		}
	}
	return cases, nil
}

func (e *HTTPBenchmarkEvaluator) waitForRunnerSlot(ctx context.Context) error {
	backoff := time.Second
	for {
		state, err := e.runnerStatus(ctx)
		if err != nil {
			return err
		}
		if !boolFromMap(state, "running") {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff = time.Duration(math.Min(float64(backoff*2), float64(30*time.Second)))
	}
}

func (e *HTTPBenchmarkEvaluator) latestRunIDForWorkflow(ctx context.Context, workflowID string, benchmark string, limit int) (string, error) {
	if strings.TrimSpace(workflowID) == "" {
		return "", fmt.Errorf("workflow_id is required")
	}
	if limit < 1 {
		limit = 1
	}
	query := url.Values{}
	query.Set("workflow", workflowID)
	query.Set("limit", strconv.Itoa(limit))
	query.Set("include_optimizer", "true")
	if strings.TrimSpace(benchmark) != "" {
		query.Set("benchmark", benchmark)
	}
	var response map[string]interface{}
	if err := e.Client.GetJSON(ctx, "/api/admin/benchmarks", query, &response); err != nil {
		return "", err
	}
	runs := listFromMap(response, "runs", "Runs")
	if len(runs) == 0 {
		return "", nil
	}
	first, ok := runs[0].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("unexpected benchmark run payload type")
	}
	return stringFromMap(first, "id", "ID"), nil
}

func (e *HTTPBenchmarkEvaluator) resolveRunID(ctx context.Context, runID string) (string, error) {
	if strings.TrimSpace(runID) != "" {
		return runID, nil
	}
	e.mu.Lock()
	workflowID := e.lastWorkflowID
	cachedRunID := e.lastRequestedRunID
	e.mu.Unlock()

	if strings.TrimSpace(cachedRunID) != "" {
		return cachedRunID, nil
	}
	if strings.TrimSpace(workflowID) == "" {
		return "", fmt.Errorf("benchmark run id is unknown")
	}
	query := url.Values{}
	query.Set("workflow", workflowID)
	query.Set("limit", "1")
	query.Set("include_optimizer", "true")
	var response map[string]interface{}
	if err := e.Client.GetJSON(ctx, "/api/admin/benchmarks", query, &response); err != nil {
		return "", err
	}
	runs := listFromMap(response, "runs", "Runs")
	if len(runs) == 0 {
		return "", fmt.Errorf("no benchmark runs found for workflow %s", workflowID)
	}
	first, ok := runs[0].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("unexpected benchmark run payload type")
	}
	resolved := stringFromMap(first, "id", "ID")
	if strings.TrimSpace(resolved) == "" {
		return "", fmt.Errorf("benchmark run payload missing id")
	}
	e.mu.Lock()
	e.lastRequestedRunID = resolved
	e.mu.Unlock()
	return resolved, nil
}

func (e *HTTPBenchmarkEvaluator) runnerStatus(ctx context.Context) (map[string]interface{}, error) {
	var status map[string]interface{}
	if err := e.Client.GetJSON(ctx, "/api/admin/benchmarks/runner-status", nil, &status); err != nil {
		return nil, err
	}
	return status, nil
}

func (e *HTTPBenchmarkEvaluator) pollInterval() time.Duration {
	if e.PollInterval <= 0 {
		return 2 * time.Second
	}
	return e.PollInterval
}

func defaultSplit(split string) string {
	split = strings.TrimSpace(split)
	if split == "" {
		return "dev"
	}
	return split
}

func dedupeTrimmed(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func parseFailureAgentAnswers(items []interface{}) []FailureAgentAnswer {
	if len(items) == 0 {
		return nil
	}
	answers := make([]FailureAgentAnswer, 0, len(items))
	for _, raw := range items {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		answers = append(answers, FailureAgentAnswer{
			NodeID:  stringFromMap(item, "node_id", "NodeID"),
			Model:   stringFromMap(item, "model", "Model"),
			Answer:  stringFromMap(item, "answer", "Answer"),
			Correct: boolFromMapKeys(item, "correct", "Correct"),
			ParseOK: boolFromMapKeys(item, "parse_ok", "ParseOK"),
		})
	}
	return answers
}

func (e *HTTPBenchmarkEvaluator) fetchBenchmarkItemDetail(ctx context.Context, runID string, itemID string) (map[string]interface{}, error) {
	query := url.Values{}
	query.Set("item_id", itemID)
	path := "/api/admin/benchmarks/" + url.PathEscape(runID) + "/items"
	var detail map[string]interface{}
	if err := e.Client.GetJSON(ctx, path, query, &detail); err != nil {
		return nil, err
	}
	return detail, nil
}

func mergeFailureCaseWithItemDetail(failure *FailureCase, detail map[string]interface{}) {
	if failure == nil || detail == nil {
		return
	}
	if datasetItem := mapFromMap(detail, "DatasetItem", "dataset_item"); datasetItem != nil {
		if strings.TrimSpace(failure.Question) == "" {
			failure.Question = stringFromMap(datasetItem, "question", "Question")
		}
	}
	if itemDetail := mapFromMap(detail, "Detail", "detail"); itemDetail != nil {
		if itemSummary := mapFromMap(itemDetail, "item", "Item"); itemSummary != nil {
			if strings.TrimSpace(failure.CorrectAnswer) == "" {
				failure.CorrectAnswer = stringFromMap(itemSummary, "answer_label", "AnswerLabel")
			}
			if strings.TrimSpace(failure.Predicted) == "" {
				failure.Predicted = stringFromMap(itemSummary, "predicted", "Predicted")
			}
			if reason := strings.TrimSpace(stringFromMap(itemSummary, "failure_reason", "FailureReason")); reason != "" {
				current := strings.TrimSpace(failure.FailureReason)
				if current == "" || current == failureCaseCategory(failure.Category) {
					failure.FailureReason = reason
				}
			}
			if strings.TrimSpace(failure.RawOutput) == "" {
				failure.RawOutput = stringFromMap(itemSummary, "raw_output", "RawOutput")
			}
		}
		attempts := listFromMap(itemDetail, "attempts", "Attempts")
		if n := len(attempts); n > 0 {
			if last, ok := attempts[n-1].(map[string]interface{}); ok {
				if reason := strings.TrimSpace(stringFromMap(last, "failure_reason", "FailureReason")); reason != "" {
					current := strings.TrimSpace(failure.FailureReason)
					if current == "" || current == failureCaseCategory(failure.Category) {
						failure.FailureReason = reason
					}
				}
				if strings.TrimSpace(failure.RawOutput) == "" {
					failure.RawOutput = stringFromMap(last, "raw_output", "RawOutput")
				}
			}
		}
	}
	if flag := mapFromMap(detail, "flag", "Flag"); flag != nil {
		failure.Flagged = true
		if strings.TrimSpace(failure.FlagReason) == "" {
			failure.FlagReason = stringFromMap(flag, "reason", "Reason")
		}
	}
	if strings.TrimSpace(failure.FailureReason) == "" {
		failure.FailureReason = failureCaseCategory(failure.Category)
	}

	// R9: Extract per-node execution traces from job summaries (DSPy-style trace).
	if len(failure.NodeTraces) == 0 {
		failure.NodeTraces = extractNodeTracesFromDetail(detail)
	}
}

// extractNodeTracesFromDetail pulls per-node intermediate outputs from the
// benchmark item detail response, giving the LLM proposer DSPy-style execution
// trace visibility into what each workflow node produced.
func extractNodeTracesFromDetail(detail map[string]interface{}) []FailureNodeTrace {
	jobs := listFromMap(detail, "JobSummaries", "job_summaries")
	if len(jobs) == 0 {
		return nil
	}
	var traces []FailureNodeTrace
	seen := map[string]bool{}
	for _, rawJob := range jobs {
		jobData, ok := rawJob.(map[string]interface{})
		if !ok {
			continue
		}
		nodes := listFromMap(jobData, "Nodes", "nodes")
		for _, rawNode := range nodes {
			node, ok := rawNode.(map[string]interface{})
			if !ok {
				continue
			}
			nodeID := stringFromMap(node, "node_id", "NodeID")
			if nodeID == "" || seen[nodeID] {
				continue
			}
			output := stringFromMap(node, "output", "Output", "result", "Result")
			if strings.TrimSpace(output) == "" {
				continue
			}
			seen[nodeID] = true
			// Trim long outputs to keep prompt context manageable.
			if len(output) > 500 {
				output = output[:500]
			}
			traces = append(traces, FailureNodeTrace{
				NodeID: nodeID,
				Model:  stringFromMap(node, "model", "Model"),
				Output: output,
			})
		}
	}
	return traces
}

func mapFromMap(m map[string]interface{}, keys ...string) map[string]interface{} {
	for _, key := range keys {
		if raw, ok := m[key]; ok {
			if value, ok := raw.(map[string]interface{}); ok {
				return value
			}
		}
	}
	return nil
}

func listFromMap(m map[string]interface{}, keys ...string) []interface{} {
	for _, key := range keys {
		if raw, ok := m[key]; ok {
			if value, ok := raw.([]interface{}); ok {
				return value
			}
		}
	}
	return nil
}

func stringFromMap(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if raw, ok := m[key]; ok {
			switch value := raw.(type) {
			case string:
				return value
			}
		}
	}
	return ""
}

func boolFromMap(m map[string]interface{}, key string) bool {
	raw, ok := m[key]
	if !ok {
		return false
	}
	value, ok := raw.(bool)
	return ok && value
}

func boolFromMapKeys(m map[string]interface{}, keys ...string) bool {
	for _, key := range keys {
		if boolFromMap(m, key) {
			return true
		}
	}
	return false
}

// IntFromMap looks up keys in a map and returns the first found value as int.
func IntFromMap(m map[string]interface{}, keys ...string) int {
	for _, key := range keys {
		if raw, ok := m[key]; ok {
			switch value := raw.(type) {
			case float64:
				return int(value)
			case int:
				return value
			case int64:
				return int(value)
			}
		}
	}
	return 0
}

// FloatFromMap looks up keys in a map and returns the first found value as float64.
func FloatFromMap(m map[string]interface{}, keys ...string) float64 {
	for _, key := range keys {
		if raw, ok := m[key]; ok {
			switch value := raw.(type) {
			case float64:
				return value
			case int:
				return float64(value)
			case int64:
				return float64(value)
			}
		}
	}
	return 0
}
