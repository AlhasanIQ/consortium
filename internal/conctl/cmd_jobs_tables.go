package conctl

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// jobsTable renders a minimal table for jobs list.
func jobsTable(w io.Writer, data interface{}, format string) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return
	}
	jobs, ok := m["Jobs"].([]interface{})
	if !ok {
		return
	}
	headers := []string{"ID", "STATUS", "MODEL", "COST", "CREATED"}
	var rows [][]string
	for _, j := range jobs {
		jm, ok := j.(map[string]interface{})
		if !ok {
			continue
		}
		rows = append(rows, []string{
			stringVal(jm, "id"),
			stringVal(jm, "status"),
			stringVal(jm, "model"),
			fmt.Sprintf("$%.4f", floatVal(jm, "DisplayCost")),
			stringVal(jm, "created_at"),
		})
	}
	printTableAuto(w, headers, rows, format)
}

// enrichJobDetail adds computed metrics to a job detail response.
func enrichJobDetail(data map[string]interface{}) {
	computed := map[string]interface{}{}

	// Compute avg/p95 latency from NodesWithMetrics.
	if nodes, ok := data["NodesWithMetrics"].([]interface{}); ok && len(nodes) > 0 {
		var latencies []float64
		for _, n := range nodes {
			nm, ok := n.(map[string]interface{})
			if !ok {
				continue
			}
			if l := floatVal(nm, "latency_ms"); l > 0 {
				latencies = append(latencies, l)
			}
		}
		if len(latencies) > 0 {
			sum := 0.0
			for _, l := range latencies {
				sum += l
			}
			computed["avg_node_latency_ms"] = sum / float64(len(latencies))
			// Sort for p95.
			sorted := make([]float64, len(latencies))
			copy(sorted, latencies)
			sortFloat64s(sorted)
			idx := int(float64(len(sorted))*0.95+0.5) - 1
			if idx < 0 {
				idx = 0
			}
			if idx >= len(sorted) {
				idx = len(sorted) - 1
			}
			computed["p95_node_latency_ms"] = sorted[idx]
		}
	}

	// Attempt tracking.
	if attempts, ok := data["Attempts"].([]interface{}); ok {
		total := len(attempts)
		retries := 0
		for _, a := range attempts {
			am, ok := a.(map[string]interface{})
			if !ok {
				continue
			}
			if floatVal(am, "attempt") > 1 {
				retries++
			}
		}
		computed["total_attempts"] = total
		computed["retry_attempts"] = retries
	}

	// Lifecycle timing.
	if lc, ok := data["Lifecycle"].(map[string]interface{}); ok {
		execMs := floatVal(lc, "ExecutionDurationMs")
		queueMs := floatVal(lc, "QueueDelayMs")
		activeMs := floatVal(lc, "ActiveAttemptDurationMs")
		idleMs := floatVal(lc, "IdleDurationMs")

		computed["end_to_end_ms"] = execMs + queueMs
		if activeMs+idleMs > 0 {
			computed["active_share_pct"] = (activeMs / (activeMs + idleMs)) * 100
		}
		computed["queue_delay_ms"] = queueMs
	}

	data["computed"] = computed
}

// sortFloat64s sorts a float64 slice in place (insertion sort, fine for small N).
func sortFloat64s(a []float64) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j-1] > a[j]; j-- {
			a[j-1], a[j] = a[j], a[j-1]
		}
	}
}

// jobGetTable renders a compact table for job detail (get command).
func jobGetTable(w io.Writer, data interface{}, format string) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return
	}

	// Job header.
	job, _ := m["Job"].(map[string]interface{})
	if job != nil {
		fmt.Fprintf(w, "Job:      %s\n", stringVal(job, "id"))
		fmt.Fprintf(w, "Status:   %s  Workflow: %s\n", stringVal(job, "status"), stringVal(job, "workflow_id"))
		fmt.Fprintf(w, "Model:    %s\n", stringVal(job, "model"))
		fmt.Fprintf(w, "Cost:     %s  Tokens: %.0f (in: %.0f, out: %.0f)\n",
			formatCostVal(floatVal(job, "cost")),
			floatVal(job, "tokens_total"),
			floatVal(job, "tokens_input"),
			floatVal(job, "tokens_output"))
		fmt.Fprintf(w, "Created:  %s\n", stringVal(job, "created_at"))
		if pid := stringVal(job, "parent_execution_id"); pid != "" {
			fmt.Fprintf(w, "Parent:   %s\n", pid)
		}
	}
	if pwf := stringVal(m, "ParentWorkflowID"); pwf != "" {
		fmt.Fprintf(w, "Parent WF: %s\n", pwf)
	}
	if bench, ok := m["BenchmarkRun"].(map[string]interface{}); ok && bench != nil {
		fmt.Fprintf(w, "Bench:    %s (run: %s)\n", stringVal(bench, "benchmark"), stringVal(bench, "run_id"))
	}

	// Lifecycle.
	if lc, _ := m["Lifecycle"].(map[string]interface{}); lc != nil {
		fmt.Fprintf(w, "Queue:    %.0fms  Execution: %.0fms  Active: %.1f%%\n",
			floatVal(lc, "QueueDelayMs"),
			floatVal(lc, "ExecutionDurationMs"),
			floatVal(lc, "ActiveAttemptDurationMs")/(floatVal(lc, "ActiveAttemptDurationMs")+floatVal(lc, "IdleDurationMs")+0.001)*100)
	}

	// Cost summary.
	if cs, _ := m["JobCostSummary"].(map[string]interface{}); cs != nil {
		desc := int(floatVal(cs, "DescendantCount"))
		if desc > 0 {
			fmt.Fprintf(w, "Total:    %s (incl %d child jobs)\n",
				formatCostVal(floatVal(cs, "TotalCost")), desc)
		}
	}
	fmt.Fprintln(w)

	// Nodes table.
	nodes, _ := m["NodesWithMetrics"].([]interface{})
	if len(nodes) == 0 {
		return
	}

	headers := []string{"NODE", "TYPE", "MODEL", "STATUS", "LATENCY", "TOKENS", "COST"}
	var rows [][]string
	for _, n := range nodes {
		nm, ok := n.(map[string]interface{})
		if !ok {
			continue
		}
		rows = append(rows, []string{
			stringVal(nm, "node_id"),
			stringVal(nm, "node_type"),
			formatModelWithLabel(stringVal(nm, "model"), stringVal(nm, "node_name")),
			stringVal(nm, "status"),
			fmt.Sprintf("%.0fms", floatVal(nm, "latency_ms")),
			fmt.Sprintf("%.0f", floatVal(nm, "TotalTokens")),
			formatCostVal(floatVal(nm, "cost")),
		})
	}
	printTableAuto(w, headers, rows, format)
}

// jobNodesTable renders a table for job nodes.
func jobNodesTable(w io.Writer, data interface{}, format string) {
	// Response is a list of WorkflowNode objects.
	nodes, ok := data.([]interface{})
	if !ok {
		// Try nested in a map.
		if m, ok := data.(map[string]interface{}); ok {
			if nl, ok := m["Nodes"].([]interface{}); ok {
				nodes = nl
			}
		}
	}
	if len(nodes) == 0 {
		return
	}

	rows := renderNodeRowsOpts(nodes, nodeRowOpts{FullAnswer: true, IncludeTokens: true})
	printTableAuto(w, nodeTableHeadersWithTokens, rows, format)
}

// jobAttemptsTable renders a table for node execution attempts.
func jobAttemptsTable(w io.Writer, data interface{}, format string) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return
	}
	attempts, ok := m["node_execution_attempts"].([]interface{})
	if !ok || len(attempts) == 0 {
		fmt.Fprintln(w, "(no attempts)")
		return
	}

	headers := []string{"NODE", "ATTEMPT", "STATUS", "LATENCY", "TOKENS_IN", "TOKENS_OUT", "COST", "LAYER", "PHASE", "CODE", "REQ_ID", "ERROR"}
	var rows [][]string
	for _, a := range attempts {
		am, ok := a.(map[string]interface{})
		if !ok {
			continue
		}
		meta, _ := am["metadata"].(map[string]interface{})
		errMsg := stringVal(am, "error_message")
		if errMsg == "" {
			errMsg = "-"
		}
		layer := stringVal(meta, "retry_layer")
		if layer == "" {
			layer = "-"
		}
		phase := stringVal(meta, "error_phase")
		if phase == "" {
			phase = "-"
		}
		code := stringVal(am, "error_code")
		if code == "" {
			code = stringVal(meta, "provider_error_code")
		}
		if code == "" {
			code = "-"
		}
		reqID := stringVal(meta, "provider_request_id")
		if reqID == "" {
			reqID = stringVal(meta, "provider_response_id")
		}
		if reqID == "" {
			reqID = "-"
		}
		status := stringVal(am, "status")
		if isNodeReplayed(am) {
			status += " [replay]"
		}
		rows = append(rows, []string{
			stringVal(am, "node_id"),
			fmt.Sprintf("%.0f", floatVal(am, "attempt")),
			status,
			fmt.Sprintf("%.0fms", floatVal(am, "latency_ms")),
			fmt.Sprintf("%.0f", floatVal(am, "tokens_input")),
			fmt.Sprintf("%.0f", floatVal(am, "tokens_output")),
			formatCostVal(floatVal(am, "cost")),
			truncate(layer, 14),
			truncate(phase, 16),
			truncate(code, 18),
			truncate(reqID, 14),
			truncate(errMsg, 50),
		})
	}
	printTableAuto(w, headers, rows, format)
}

// jobAgentRunsTable renders a table for agent run summaries.
func jobAgentRunsTable(w io.Writer, data interface{}, format string) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return
	}
	runs, ok := m["agent_runs"].([]interface{})
	if !ok || len(runs) == 0 {
		fmt.Fprintln(w, "(no agent runs)")
		return
	}

	headers := []string{"NODE", "ATTEMPT", "KIND", "STATUS", "HARNESS", "EXTERNAL_RUN", "JOB_RUN", "TASK", "HANDOFF", "TOKENS", "COST", "ERROR"}
	var rows [][]string
	for _, r := range runs {
		rm, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		errMsg := stringVal(rm, "error_message")
		if errMsg == "" {
			errMsg = stringVal(rm, "error_code")
		}
		if errMsg == "" {
			errMsg = "-"
		}
		kind := stringVal(rm, "run_kind")
		if kind == "" {
			kind = "agent_run"
		}
		taskID := stringVal(rm, "external_task_id")
		if taskID == "" {
			taskID = "-"
		}
		jobRunID := stringVal(rm, "external_job_run_id")
		if jobRunID == "" {
			jobRunID = "-"
		}
		rows = append(rows, []string{
			stringVal(rm, "node_id"),
			fmt.Sprintf("%.0f", floatVal(rm, "attempt")),
			kind,
			stringVal(rm, "status"),
			stringVal(rm, "harness"),
			stringVal(rm, "external_run_id"),
			jobRunID,
			taskID,
			truncate(formatAgentRunHandoff(rm), 24),
			fmt.Sprintf("%.0f", floatVal(rm, "tokens_input")+floatVal(rm, "tokens_output")),
			formatCostVal(floatVal(rm, "cost_usd")),
			truncate(errMsg, 40),
		})
	}
	printTableAuto(w, headers, rows, format)
}

func formatAgentRunHandoff(row map[string]interface{}) string {
	if ref, ok := row["inherit_from"].(map[string]interface{}); ok {
		return compactHandoffRef(ref)
	}
	raw := stringVal(row, "inherit_from_json")
	if raw == "" {
		return "-"
	}
	var ref map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &ref); err != nil {
		return raw
	}
	return compactHandoffRef(ref)
}

func compactHandoffRef(ref map[string]interface{}) string {
	kind := stringVal(ref, "kind")
	id := stringVal(ref, "id")
	if kind == "" && id == "" {
		return "-"
	}
	if kind == "" {
		return id
	}
	if id == "" {
		return kind
	}
	return kind + ":" + id
}

// jobTraceTable renders trace spans grouped by node.
func jobTraceTable(w io.Writer, data interface{}, format string) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return
	}
	fmt.Fprintf(w, "Job: %s\n\n", stringVal(m, "job_id"))

	groups, ok := m["node_groups"].([]interface{})
	if !ok || len(groups) == 0 {
		fmt.Fprintln(w, "(no trace spans)")
		return
	}

	for _, g := range groups {
		gm, ok := g.(map[string]interface{})
		if !ok {
			continue
		}
		fmt.Fprintf(w, "--- Node: %s ---\n", stringVal(gm, "node_id"))

		spans, ok := gm["spans"].([]interface{})
		if !ok || len(spans) == 0 {
			fmt.Fprintln(w, "(no spans)")
			continue
		}

		headers := []string{"KIND", "STATUS", "DURATION", "ATTRIBUTES"}
		var rows [][]string
		for _, s := range spans {
			sm, ok := s.(map[string]interface{})
			if !ok {
				continue
			}
			// Format attributes as key=value pairs.
			attrStr := "-"
			if attrs, ok := sm["attributes"].(map[string]interface{}); ok && len(attrs) > 0 {
				var parts []string
				for k, v := range attrs {
					parts = append(parts, fmt.Sprintf("%s=%v", k, v))
				}
				attrStr = truncate(strings.Join(parts, ", "), 60)
			}
			rows = append(rows, []string{
				stringVal(sm, "kind"),
				stringVal(sm, "status"),
				fmt.Sprintf("%.0fms", floatVal(sm, "duration_ms")),
				attrStr,
			})
		}
		printTableAuto(w, headers, rows, format)
		fmt.Fprintln(w)
	}
}

// jobChildrenTable renders a table for child jobs of a parent.
func jobChildrenTable(w io.Writer, data interface{}, format string) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return
	}
	jobs, ok := m["Jobs"].([]interface{})
	if !ok || len(jobs) == 0 {
		fmt.Fprintln(w, "(no child jobs)")
		return
	}

	headers := []string{"ID", "STATUS", "DESCRIPTION", "COST", "TOKENS", "CREATED"}
	var rows [][]string
	for _, j := range jobs {
		jm, ok := j.(map[string]interface{})
		if !ok {
			continue
		}
		rows = append(rows, []string{
			stringVal(jm, "id"),
			stringVal(jm, "status"),
			truncate(stringVal(jm, "description"), 30),
			formatCostVal(floatVal(jm, "cost")),
			fmt.Sprintf("%.0f", floatVal(jm, "tokens_total")),
			truncate(stringVal(jm, "created_at"), 19),
		})
	}
	printTableAuto(w, headers, rows, format)
}
