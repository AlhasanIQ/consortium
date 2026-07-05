package conctl

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/alhasaniq/consortium/pkg/failureinsights"
)

// benchmarkRunnerStatusTable renders runner status with optional diagnostics.
func benchmarkRunnerStatusTable(w io.Writer, data interface{}, format string) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return
	}

	progress := fmt.Sprintf("%.0f/%.0f", floatVal(m, "completed_items"), floatVal(m, "total_items"))
	accuracy := "-"
	completed := floatVal(m, "completed_items")
	if completed > 0 {
		accuracy = fmt.Sprintf("%.1f%%", (floatVal(m, "correct_items")/completed)*100)
	}

	headers := []string{"RUNNING", "RUN", "WORKFLOW", "ITEM", "PROGRESS", "ACCURACY", "UPDATED"}
	rows := [][]string{{
		fmt.Sprintf("%t", boolVal(m, "running")),
		stringVal(m, "current_run_id"),
		stringVal(m, "current_workflow"),
		stringVal(m, "current_item_id"),
		progress,
		accuracy,
		truncate(stringVal(m, "last_update_at"), 24),
	}}
	printTableAuto(w, headers, rows, format)

	jobs, ok := m["_running_jobs"].([]interface{})
	if !ok || len(jobs) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Active Jobs:")
	jobHeaders := []string{"JOB", "WORKFLOW", "STATUS", "CREATED"}
	jobRows := make([][]string, 0, len(jobs))
	for _, job := range jobs {
		jm, ok := job.(map[string]interface{})
		if !ok {
			continue
		}
		workflow := stringVal(jm, "workflow_id")
		if workflow == "" {
			workflow = truncate(stringVal(jm, "description"), 30)
		}
		jobRows = append(jobRows, []string{
			stringVal(jm, "id"),
			workflow,
			stringVal(jm, "status"),
			truncate(stringVal(jm, "created_at"), 24),
		})
	}
	printTableAuto(w, jobHeaders, jobRows, format)
}

// benchmarkDrillTable renders the drill-down investigation view.
func benchmarkDrillTable(w io.Writer, data interface{}, format string) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return
	}

	// Question section.
	dsItem, _ := m["DatasetItem"].(map[string]interface{})
	renderQuestion(w, dsItem, 200)

	// Flag banner when item is flagged.
	if flg, ok := m["flag"].(map[string]interface{}); ok && flg != nil {
		reason := stringVal(flg, "reason")
		source := stringVal(flg, "source")
		createdAt := stringVal(flg, "created_at")
		if createdAt != "" && len(createdAt) > 10 {
			createdAt = createdAt[:10]
		}
		fmt.Fprintf(w, "\n!! DATASET FLAG: %s", reason)
		if source != "" || createdAt != "" {
			fmt.Fprintf(w, " (source: %s, %s)", source, createdAt)
		}
		fmt.Fprintln(w)
	}

	// Verdict section (with category from analysis).
	detail, _ := m["Detail"].(map[string]interface{})
	renderItemOutcome(w, detail, stringVal(m, "_category"))

	// Delegate to the standard item table for execution trace (inherits _showOutput/_showPrompts).
	benchmarkItemTraceSection(w, m, format)
}

// benchmarkItemTraceSection renders just the execution trace part (shared by item and drill).
func benchmarkItemTraceSection(w io.Writer, m map[string]interface{}, format string) {
	showOutput := boolVal(m, "_showOutput")
	showPrompts := boolVal(m, "_showPrompts")
	expected := stringVal(m, "_expected_answer")

	jobSummaries, ok := m["JobSummaries"].([]interface{})
	if !ok || len(jobSummaries) == 0 {
		return
	}

	for attemptIdx, js := range jobSummaries {
		jsm, ok := js.(map[string]interface{})
		if !ok {
			continue
		}
		if len(jobSummaries) > 1 {
			fmt.Fprintf(w, "--- Attempt %d ---\n", attemptIdx+1)
		}
		fmt.Fprintf(w, "Parent Job: %s  Tokens: %.0f  Cost: %s\n",
			stringVal(jsm, "JobID"),
			floatVal(jsm, "TotalTokens"),
			formatCostVal(floatVal(jsm, "TotalCost")))

		if nodes, ok := jsm["Nodes"].([]interface{}); ok && len(nodes) > 0 {
			if rows := renderNodeRows(nodes, expected); len(rows) > 0 {
				printTableAuto(w, nodeTableHeaders, rows, format)
			}
			if showPrompts || showOutput {
				printNodeDetails(w, nodes, showPrompts, showOutput)
			}
		}

		if childJobs, ok := jsm["ChildJobs"].([]interface{}); ok {
			for _, cj := range childJobs {
				cjm, ok := cj.(map[string]interface{})
				if !ok {
					continue
				}
				fmt.Fprintf(w, "\n  Child Job: %s\n", stringVal(cjm, "JobID"))
				if childNodes, ok := cjm["Nodes"].([]interface{}); ok && len(childNodes) > 0 {
					if rows := renderNodeRows(childNodes, expected); len(rows) > 0 {
						printTableAuto(w, nodeTableHeaders, rows, format)
					}
					if showPrompts || showOutput {
						printNodeDetails(w, childNodes, showPrompts, showOutput)
					}
				}
			}
		}
		fmt.Fprintln(w)
	}
}

// renderExplainNarrative builds a structured narrative of why an item was incorrect.
func renderExplainNarrative(w io.Writer, itemData map[string]interface{}, analysisItem map[string]interface{}) {
	dsItem, _ := itemData["DatasetItem"].(map[string]interface{})
	detail, _ := itemData["Detail"].(map[string]interface{})
	var item map[string]interface{}
	if detail != nil {
		item, _ = detail["item"].(map[string]interface{})
	}

	// Header.
	subject := ""
	if item != nil {
		subject = stringVal(item, "subject")
	}
	itemID := ""
	if item != nil {
		itemID = stringVal(item, "item_id")
	}
	fmt.Fprintf(w, "## Item: %s (%s)\n\n", itemID, subject)

	// Question.
	if dsItem != nil {
		fmt.Fprintf(w, "Question: %s\n", stringVal(dsItem, "question"))
		expectedLabel := stringVal(dsItem, "answer_label")
		if choices, ok := dsItem["choices"].([]interface{}); ok {
			for i, c := range choices {
				if i >= 26 {
					break
				}
				label := string(rune('A' + i))
				if label == expectedLabel {
					fmt.Fprintf(w, "Correct Answer: %s - %v\n", expectedLabel, c)
					break
				}
			}
		}
		fmt.Fprintln(w)
	}

	// Walk through job summaries to build the narrative.
	jobSummaries, _ := itemData["JobSummaries"].([]interface{})
	if len(jobSummaries) == 0 {
		fmt.Fprintln(w, "(no execution data available)")
		return
	}

	jsm, ok := jobSummaries[len(jobSummaries)-1].(map[string]interface{})
	if !ok {
		return
	}

	expectedLabel := ""
	if dsItem != nil {
		expectedLabel = stringVal(dsItem, "answer_label")
	}

	// Child workflow section.
	if childJobs, ok := jsm["ChildJobs"].([]interface{}); ok && len(childJobs) > 0 {
		cjm, _ := childJobs[0].(map[string]interface{})
		if cjm != nil {
			fmt.Fprintf(w, "### Child Workflow\n")
			if childNodes, ok := cjm["Nodes"].([]interface{}); ok {
				for _, cn := range childNodes {
					cnm, ok := cn.(map[string]interface{})
					if !ok || stringVal(cnm, "parent_node_id") != "" {
						continue
					}
					nodeType := stringVal(cnm, "node_type")
					if nodeType == "aggregator" || nodeType == "result" {
						answer := extractAnswer(stringVal(cnm, "output"))
						mark := "?"
						if answer == expectedLabel {
							mark = "correct"
						} else if answer != "-" {
							mark = "wrong"
						}
						fmt.Fprintf(w, "  -> Child synthesized: %s (%s)\n", answer, mark)
					} else {
						model := truncateModel(stringVal(cnm, "model"))
						answer := extractAnswer(stringVal(cnm, "output"))
						mark := "?"
						if answer == expectedLabel {
							mark = "correct"
						} else if answer != "-" {
							mark = "wrong"
						}
						fmt.Fprintf(w, "  %s (%s): answered %s (%s)\n",
							stringVal(cnm, "node_id"), model, answer, mark)
					}
				}
			}
			fmt.Fprintln(w)
		}
	}

	// Parent workflow section.
	fmt.Fprintf(w, "### Parent Workflow\n")
	if nodes, ok := jsm["Nodes"].([]interface{}); ok {
		for _, n := range nodes {
			nm, ok := n.(map[string]interface{})
			if !ok || stringVal(nm, "parent_node_id") != "" {
				continue
			}
			nodeType := stringVal(nm, "node_type")
			model := truncateModel(stringVal(nm, "model"))
			answer := extractAnswer(stringVal(nm, "output"))
			mark := "?"
			if answer == expectedLabel {
				mark = "correct"
			} else if answer != "-" {
				mark = "wrong"
			}

			switch nodeType {
			case "aggregator", "result":
				fmt.Fprintf(w, "  -> Parent synthesized: %s (%s)\n", answer, mark)
			case "child_workflow":
				fmt.Fprintf(w, "  %s (child workflow): returned %s (%s)\n",
					stringVal(nm, "node_id"), answer, mark)
			default:
				fmt.Fprintf(w, "  %s (%s): answered %s (%s)\n",
					stringVal(nm, "node_id"), model, answer, mark)
			}
		}
	}
	fmt.Fprintln(w)

	// Diagnosis.
	fmt.Fprintf(w, "### Diagnosis\n")
	if analysisItem != nil {
		category := stringVal(analysisItem, "category")
		fmt.Fprintf(w, "Category: %s\n", category)
		fmt.Fprintln(w, failureinsights.WrongAnswerCategoryExplanation(category))
	} else {
		if item != nil && boolVal(item, "correct") {
			fmt.Fprintln(w, "This item was answered correctly.")
		} else {
			fmt.Fprintln(w, "Analysis data not available for detailed diagnosis.")
		}
	}
}

// compareItemsTable renders a table for benchmark item comparison.
func compareItemsTable(w io.Writer, data interface{}, format string) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return
	}

	// Summary.
	fmt.Fprintf(w, "Base:      %s (%s)\n", stringVal(m, "base_run_id"), stringVal(m, "base_workflow_id"))
	fmt.Fprintf(w, "Candidate: %s (%s)\n", stringVal(m, "candidate_run_id"), stringVal(m, "candidate_workflow_id"))
	if summary, ok := m["summary"].(map[string]interface{}); ok {
		fmt.Fprintf(w, "Compared: %.0f  Regressed: %.0f  Improved: %.0f  Unchanged correct: %.0f  Unchanged wrong: %.0f\n",
			floatVal(summary, "total_compared"),
			floatVal(summary, "regressed"),
			floatVal(summary, "improved"),
			floatVal(summary, "unchanged_correct"),
			floatVal(summary, "unchanged_wrong"))
	}
	fmt.Fprintln(w)

	itemHeaders := []string{"ITEM_ID", "SUBJECT", "EXPECTED", "BASE", "CANDIDATE", "BASE_FAIL", "CAND_FAIL"}

	// Regressions.
	if regressions, ok := m["regressions"].([]interface{}); ok && len(regressions) > 0 {
		fmt.Fprintln(w, "Regressions:")
		printTableAuto(w, itemHeaders, buildCompareItemRows(regressions), format)
		fmt.Fprintln(w)
	}

	// Improvements.
	if improvements, ok := m["improvements"].([]interface{}); ok && len(improvements) > 0 {
		fmt.Fprintln(w, "Improvements:")
		printTableAuto(w, itemHeaders, buildCompareItemRows(improvements), format)
	}
}

// buildCompareItemRows builds table rows for compare-items sections (regressions/improvements).
func buildCompareItemRows(items []interface{}) [][]string {
	var rows [][]string
	for _, r := range items {
		rm, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		baseFail := stringVal(rm, "base_failure_reason")
		if baseFail == "" {
			baseFail = "-"
		}
		candFail := stringVal(rm, "candidate_failure_reason")
		if candFail == "" {
			candFail = "-"
		}
		rows = append(rows, []string{
			stringVal(rm, "item_id"),
			truncate(stringVal(rm, "subject"), 20),
			stringVal(rm, "answer_label"),
			stringVal(rm, "base_predicted"),
			stringVal(rm, "candidate_predicted"),
			truncate(baseFail, 15),
			truncate(candFail, 15),
		})
	}
	return rows
}

// benchmarksTable renders a table for benchmark run list.
func benchmarksTable(w io.Writer, data interface{}, format string) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return
	}
	runs, ok := m["Runs"].([]interface{})
	if !ok {
		return
	}
	headers := []string{"ID", "BENCHMARK", "WORKFLOW", "STATUS", "ACCURACY", "COST", "ITEMS"}
	var rows [][]string
	for _, r := range runs {
		rm, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		rows = append(rows, []string{
			stringVal(rm, "id"),
			stringVal(rm, "benchmark"),
			stringVal(rm, "workflow_id"),
			stringVal(rm, "status"),
			fmt.Sprintf("%.1f%%", floatVal(rm, "accuracy")*100),
			fmt.Sprintf("$%.4f", floatVal(rm, "total_cost_usd")),
			fmt.Sprintf("%.0f", floatVal(rm, "total_items")),
		})
	}
	printTableAuto(w, headers, rows, format)
}

// compareTable renders a comparison table.
func compareTable(w io.Writer, data interface{}, format string) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return
	}
	metrics, ok := m["metrics"].([]interface{})
	if !ok {
		return
	}
	headers := []string{"WORKFLOW", "ACCURACY", "PARSE%", "COST", "ITEMS", "ACC_DELTA", "COST_DELTA%"}
	var rows [][]string
	for _, metric := range metrics {
		mm, ok := metric.(map[string]interface{})
		if !ok {
			continue
		}
		run, _ := mm["run"].(map[string]interface{})
		accDelta := ""
		if v, ok := mm["accuracy_delta"]; ok && v != nil {
			accDelta = fmt.Sprintf("%+.2f%%", floatVal(mm, "accuracy_delta")*100)
		}
		costDelta := ""
		if v, ok := mm["cost_delta_pct"]; ok && v != nil {
			costDelta = fmt.Sprintf("%+.1f%%", floatVal(mm, "cost_delta_pct"))
		}
		control := ""
		if boolVal(mm, "is_control") {
			control = " *"
		}
		rows = append(rows, []string{
			stringVal(run, "workflow_id") + control,
			fmt.Sprintf("%.1f%%", floatVal(run, "accuracy")*100),
			fmt.Sprintf("%.1f%%", floatVal(run, "parse_rate")*100),
			fmt.Sprintf("$%.4f", floatVal(run, "total_cost_usd")),
			fmt.Sprintf("%.0f", floatVal(run, "total_items")),
			accDelta,
			costDelta,
		})
	}
	printTableAuto(w, headers, rows, format)
}

// enrichCompareData performs client-side delta recomputation for selected runs.
func enrichCompareData(data map[string]interface{}, selectedRuns []string, controlWorkflow string) {
	metrics, ok := data["metrics"].([]interface{})
	if !ok {
		return
	}

	selectedSet := make(map[string]bool)
	for _, id := range selectedRuns {
		selectedSet[id] = true
	}

	// Find control run.
	var controlRun map[string]interface{}
	bestAcc := -1.0
	for _, m := range metrics {
		mm, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		run, _ := mm["run"].(map[string]interface{})
		runID := stringVal(run, "id")
		if !selectedSet[runID] {
			continue
		}
		if controlWorkflow != "" && stringVal(run, "workflow_id") == controlWorkflow {
			controlRun = mm
			break
		}
		acc := floatVal(run, "accuracy")
		if acc > bestAcc {
			bestAcc = acc
			controlRun = mm
		}
	}

	if controlRun == nil {
		return
	}

	controlRunData, _ := controlRun["run"].(map[string]interface{})
	controlAcc := floatVal(controlRunData, "accuracy")
	controlCost := floatVal(controlRunData, "total_cost_usd")
	controlPR := floatVal(controlRunData, "parse_rate")

	controlRunData2, _ := controlRun["run"].(map[string]interface{})
	controlRunID := stringVal(controlRunData2, "id")

	for _, m := range metrics {
		mm, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		run, _ := mm["run"].(map[string]interface{})
		runID := stringVal(run, "id")

		isCtrl := (runID == controlRunID)
		mm["is_control"] = isCtrl

		if !isCtrl {
			acc := floatVal(run, "accuracy")
			cost := floatVal(run, "total_cost_usd")
			pr := floatVal(run, "parse_rate")
			mm["accuracy_delta"] = acc - controlAcc
			mm["parse_rate_delta"] = pr - controlPR
			if controlCost > 0 {
				mm["cost_delta_pct"] = ((cost - controlCost) / controlCost) * 100
			}
		} else {
			mm["accuracy_delta"] = nil
			mm["cost_delta_pct"] = nil
			mm["parse_rate_delta"] = nil
		}

		_ = runID // used in selectedSet check above
	}
}

// benchmarkGetTable renders a table for benchmark run detail (get command).
func benchmarkGetTable(w io.Writer, data interface{}, format string) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return
	}

	// Run summary header.
	run, _ := m["Run"].(map[string]interface{})
	if run != nil {
		fmt.Fprintf(w, "Run:       %s\n", stringVal(run, "id"))
		fmt.Fprintf(w, "Benchmark: %s  Split: %s\n", stringVal(run, "benchmark"), stringVal(run, "split"))
		fmt.Fprintf(w, "Workflow:  %s  Status: %s\n", stringVal(run, "workflow_id"), stringVal(run, "status"))

		flaggedItems := int(floatVal(m, "flagged_items"))
		if flaggedItems > 0 {
			adjAcc := floatVal(m, "adjusted_accuracy")
			fmt.Fprintf(w, "Accuracy:  %.1f%% raw | %.1f%% adjusted (%d flagged excluded)  Parse: %.1f%%  Cost: %s\n",
				floatVal(run, "accuracy")*100,
				adjAcc*100,
				flaggedItems,
				floatVal(run, "parse_rate")*100,
				formatCostVal(floatVal(run, "total_cost_usd")))
		} else {
			fmt.Fprintf(w, "Accuracy:  %.1f%%  Parse: %.1f%%  Cost: %s\n",
				floatVal(run, "accuracy")*100,
				floatVal(run, "parse_rate")*100,
				formatCostVal(floatVal(run, "total_cost_usd")))
		}
		fmt.Fprintf(w, "Items:     %.0f total, %.0f correct, %.0f incorrect\n",
			floatVal(run, "total_items"),
			floatVal(m, "CorrectCount"),
			floatVal(m, "IncorrectCount"))
		fmt.Fprintln(w)
	}

	// Items table.
	items, ok := m["Items"].([]interface{})
	if !ok || len(items) == 0 {
		fmt.Fprintln(w, "(no items)")
		return
	}

	showCategory := boolVal(m, "_showCategory")
	headers := []string{"ITEM_ID", "SUBJECT", "CORRECT", "PREDICTED", "EXPECTED", "LATENCY", "COST", "FAILURE", "JOB"}
	if showCategory {
		headers = append(headers, "CATEGORY")
	}
	var rows [][]string
	for _, item := range items {
		im, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		correct := "N"
		if boolVal(im, "correct") {
			correct = "Y"
		}
		failure := stringVal(im, "failure_reason")
		if failure == "" {
			failure = "-"
		}
		row := []string{
			stringVal(im, "item_id"),
			truncate(stringVal(im, "subject"), 25),
			correct,
			stringVal(im, "predicted"),
			stringVal(im, "answer_label"),
			fmt.Sprintf("%.0fms", floatVal(im, "latency_ms")),
			formatCostVal(floatVal(im, "cost_usd")),
			truncate(failure, 20),
			stringVal(im, "job_id"),
		}
		if showCategory {
			cat := stringVal(im, "_category")
			if cat == "" {
				cat = "-"
			}
			row = append(row, truncate(cat, 28))
		}
		rows = append(rows, row)
	}
	printTableAuto(w, headers, rows, format)

	// Pagination hint.
	page := int(floatVal(m, "Page"))
	if boolVal(m, "HasNext") {
		fmt.Fprintf(w, "\nPage %d (more available: --page %d)\n", page, page+1)
	}
}

// benchmarkAnalysisTable renders a table for wrong-answer analysis.
func benchmarkAnalysisTable(w io.Writer, data interface{}, format string) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return
	}

	// Summary section.
	summary, _ := m["summary"].(map[string]interface{})
	if summary != nil {
		fmt.Fprintf(w, "Wrong Answer Analysis: %s (%s/%s)\n",
			stringVal(m, "workflow_id"), stringVal(m, "benchmark"), stringVal(m, "split"))

		flaggedItems := int(floatVal(summary, "flagged_items"))
		rawAcc := floatVal(summary, "raw_accuracy")
		adjAcc := floatVal(summary, "adjusted_accuracy")
		if flaggedItems > 0 {
			fmt.Fprintf(w, "ACCURACY: %.1f%% raw | %.1f%% adjusted (%d flagged items excluded)\n",
				rawAcc*100, adjAcc*100, flaggedItems)
		}

		fmt.Fprintf(w, "Total Incorrect: %.0f\n", floatVal(summary, "total_incorrect"))
		fmt.Fprintln(w)

		catHeaders := []string{"CATEGORY", "COUNT"}
		catRows := [][]string{
			{"all_steps_wrong", fmt.Sprintf("%.0f", floatVal(summary, "all_steps_wrong"))},
			{"some_right_child_wrong", fmt.Sprintf("%.0f", floatVal(summary, "some_right_child_wrong"))},
			{"all_right_child_wrong", fmt.Sprintf("%.0f", floatVal(summary, "all_right_child_wrong"))},
			{"child_right_parent_wrong", fmt.Sprintf("%.0f", floatVal(summary, "child_right_parent_wrong"))},
			{"unclassified", fmt.Sprintf("%.0f", floatVal(summary, "unclassified"))},
		}
		printTableAuto(w, catHeaders, catRows, format)
		fmt.Fprintln(w)
	}

	// Items table.
	items, ok := m["items"].([]interface{})
	if ok && len(items) > 0 {
		headers := []string{"ITEM_ID", "SUBJECT", "EXPECTED", "PARENT", "CHILD", "CATEGORY", "FLAGGED", "AGENTS"}
		var rows [][]string
		for _, item := range items {
			im, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			// Compact agent summary: "model:D:N model:C:Y" format.
			agentSummary := ""
			if agents, ok := im["agent_answers"].([]interface{}); ok {
				var parts []string
				for _, ag := range agents {
					am, ok := ag.(map[string]interface{})
					if !ok {
						continue
					}
					mark := "X" // parse failure
					if boolVal(am, "correct") {
						mark = "Y"
					} else if boolVal(am, "parse_ok") {
						mark = "N"
					}
					model := truncateModelShort(stringVal(am, "model"))
					if model != "" {
						parts = append(parts, model+":"+stringVal(am, "answer")+":"+mark)
					} else {
						parts = append(parts, stringVal(am, "answer")+":"+mark)
					}
				}
				agentSummary = strings.Join(parts, " ")
			}
			flagCol := ""
			if boolVal(im, "flagged") {
				flagCol = "FLAGGED"
				if reason := stringVal(im, "flag_reason"); reason != "" {
					flagCol = "FLAGGED: " + truncate(reason, 40)
				}
			}
			rows = append(rows, []string{
				stringVal(im, "item_id"),
				truncate(stringVal(im, "subject"), 20),
				stringVal(im, "answer_label"),
				stringVal(im, "parent_predicted"),
				stringVal(im, "child_predicted"),
				stringVal(im, "category"),
				flagCol,
				agentSummary,
			})
		}
		printTableAuto(w, headers, rows, format)
	} else {
		fmt.Fprintln(w, "(no incorrect items)")
	}

	// Per-model accuracy summary.
	type modelStats struct {
		correct int
		total   int
	}
	modelAccuracy := make(map[string]*modelStats)
	for _, item := range items {
		im, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		agents, ok := im["agent_answers"].([]interface{})
		if !ok {
			continue
		}
		for _, ag := range agents {
			am, ok := ag.(map[string]interface{})
			if !ok {
				continue
			}
			model := truncateModelShort(stringVal(am, "model"))
			if model == "" {
				continue
			}
			if _, exists := modelAccuracy[model]; !exists {
				modelAccuracy[model] = &modelStats{}
			}
			modelAccuracy[model].total++
			if boolVal(am, "correct") {
				modelAccuracy[model].correct++
			}
		}
	}
	if len(modelAccuracy) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Per-Model Accuracy (incorrect items only):")
		modelHeaders := []string{"MODEL", "CORRECT", "TOTAL", "ACCURACY"}
		var modelNames []string
		for name := range modelAccuracy {
			modelNames = append(modelNames, name)
		}
		sort.Strings(modelNames)
		var modelRows [][]string
		for _, name := range modelNames {
			stats := modelAccuracy[name]
			acc := 0.0
			if stats.total > 0 {
				acc = float64(stats.correct) / float64(stats.total) * 100
			}
			modelRows = append(modelRows, []string{
				name,
				fmt.Sprintf("%d", stats.correct),
				fmt.Sprintf("%d", stats.total),
				fmt.Sprintf("%.1f%%", acc),
			})
		}
		printTableAuto(w, modelHeaders, modelRows, format)
	}

	performance, _ := m["performance"].(map[string]interface{})
	if performance == nil {
		return
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "Model/Node Performance (all items with child data: %.0f/%.0f)\n",
		floatVal(performance, "items_with_child_data"), floatVal(performance, "total_items"))

	if agentModels, ok := performance["agent_models"].([]interface{}); ok && len(agentModels) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Agent Model Performance:")
		headers := []string{"MODEL", "SAMPLES", "ACCURACY", "PARSE", "RETRIES", "AVG_LAT", "P95_LAT", "TOTAL_COST", "AVG_COST"}
		var rows [][]string
		for _, entry := range agentModels {
			em, ok := entry.(map[string]interface{})
			if !ok {
				continue
			}
			rows = append(rows, []string{
				truncateModelShort(stringVal(em, "model")),
				fmt.Sprintf("%.0f", floatVal(em, "samples")),
				fmt.Sprintf("%.1f%%", floatVal(em, "accuracy")*100),
				fmt.Sprintf("%.1f%%", floatVal(em, "parse_rate")*100),
				fmt.Sprintf("%.0f", floatVal(em, "total_retries")),
				fmt.Sprintf("%.0fms", floatVal(em, "avg_latency_ms")),
				fmt.Sprintf("%.0fms", floatVal(em, "p95_latency_ms")),
				formatCostVal(floatVal(em, "total_cost_usd")),
				formatCostVal(floatVal(em, "avg_cost_usd")),
			})
		}
		printTableAuto(w, headers, rows, format)
	}

	if aggregationNodes, ok := performance["aggregation_nodes"].([]interface{}); ok && len(aggregationNodes) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Aggregation Node Performance:")
		headers := []string{"NODE", "MODELS", "SAMPLES", "ACCURACY", "PARSE", "RETRIES", "AVG_LAT", "P95_LAT", "TOTAL_COST", "AVG_COST"}
		var rows [][]string
		for _, entry := range aggregationNodes {
			em, ok := entry.(map[string]interface{})
			if !ok {
				continue
			}
			rows = append(rows, []string{
				stringVal(em, "node_id"),
				truncate(analysisModelsLabel(em), 28),
				fmt.Sprintf("%.0f", floatVal(em, "samples")),
				fmt.Sprintf("%.1f%%", floatVal(em, "accuracy")*100),
				fmt.Sprintf("%.1f%%", floatVal(em, "parse_rate")*100),
				fmt.Sprintf("%.0f", floatVal(em, "total_retries")),
				fmt.Sprintf("%.0fms", floatVal(em, "avg_latency_ms")),
				fmt.Sprintf("%.0fms", floatVal(em, "p95_latency_ms")),
				formatCostVal(floatVal(em, "total_cost_usd")),
				formatCostVal(floatVal(em, "avg_cost_usd")),
			})
		}
		printTableAuto(w, headers, rows, format)
	}

	diagnostics, _ := m["diagnostics"].(map[string]interface{})
	if diagnostics == nil {
		return
	}

	if slowest, ok := diagnostics["slowest_items"].([]interface{}); ok && len(slowest) > 0 {
		topN := int(floatVal(diagnostics, "top_n"))
		if topN <= 0 {
			topN = len(slowest)
		}
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Slowest Items (top %d):\n", topN)
		headers := []string{"ITEM_ID", "SUBJECT", "TOTAL_LAT", "PARENT_LAT", "CHILD_LAT", "PARENT_RET", "CHILD_RET", "TOTAL_RET"}
		rows := make([][]string, 0, len(slowest))
		for _, entry := range slowest {
			em, ok := entry.(map[string]interface{})
			if !ok {
				continue
			}
			rows = append(rows, []string{
				stringVal(em, "item_id"),
				truncate(stringVal(em, "subject"), 20),
				fmt.Sprintf("%.0fms", floatVal(em, "total_latency_ms")),
				fmt.Sprintf("%.0fms", floatVal(em, "parent_latency_ms")),
				fmt.Sprintf("%.0fms", floatVal(em, "child_latency_ms")),
				fmt.Sprintf("%.0f", floatVal(em, "parent_retries")),
				fmt.Sprintf("%.0f", floatVal(em, "child_retries")),
				fmt.Sprintf("%.0f", floatVal(em, "total_retries")),
			})
		}
		printTableAuto(w, headers, rows, format)
	}

	if mostRetries, ok := diagnostics["most_retries_items"].([]interface{}); ok && len(mostRetries) > 0 {
		topN := int(floatVal(diagnostics, "top_n"))
		if topN <= 0 {
			topN = len(mostRetries)
		}
		fmt.Fprintln(w)
		fmt.Fprintf(w, "Most Retries (top %d):\n", topN)
		headers := []string{"ITEM_ID", "SUBJECT", "TOTAL_RET", "PARENT_RET", "CHILD_RET", "TOTAL_LAT"}
		rows := make([][]string, 0, len(mostRetries))
		for _, entry := range mostRetries {
			em, ok := entry.(map[string]interface{})
			if !ok {
				continue
			}
			rows = append(rows, []string{
				stringVal(em, "item_id"),
				truncate(stringVal(em, "subject"), 20),
				fmt.Sprintf("%.0f", floatVal(em, "total_retries")),
				fmt.Sprintf("%.0f", floatVal(em, "parent_retries")),
				fmt.Sprintf("%.0f", floatVal(em, "child_retries")),
				fmt.Sprintf("%.0fms", floatVal(em, "total_latency_ms")),
			})
		}
		printTableAuto(w, headers, rows, format)
	}
}

func analysisModelsLabel(entry map[string]interface{}) string {
	models, ok := entry["models"].([]interface{})
	if !ok || len(models) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(models))
	for _, m := range models {
		if ms, ok := m.(string); ok && ms != "" {
			parts = append(parts, truncateModelShort(ms))
		}
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}

// benchmarkItemTable renders a table for benchmark item detail with execution trace.
func benchmarkItemTable(w io.Writer, data interface{}, format string) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return
	}

	// Question section.
	dsItem, _ := m["DatasetItem"].(map[string]interface{})
	renderQuestion(w, dsItem, 120)

	// Item outcome.
	detail, _ := m["Detail"].(map[string]interface{})
	renderItemOutcome(w, detail, "")

	// Delegate to shared trace renderer.
	benchmarkItemTraceSection(w, m, format)
}
