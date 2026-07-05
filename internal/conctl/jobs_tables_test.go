package conctl

import (
	"bytes"
	"strings"
	"testing"
)

// --- sortFloat64s tests ---

func TestSortFloat64s_Empty(t *testing.T) {
	var a []float64
	sortFloat64s(a) // must not panic
}

func TestSortFloat64s_AlreadySorted(t *testing.T) {
	a := []float64{1.0, 2.0, 3.0}
	sortFloat64s(a)
	if a[0] != 1.0 || a[1] != 2.0 || a[2] != 3.0 {
		t.Errorf("already-sorted slice changed: %v", a)
	}
}

func TestSortFloat64s_ReverseSorted(t *testing.T) {
	a := []float64{3.0, 2.0, 1.0}
	sortFloat64s(a)
	if a[0] != 1.0 || a[1] != 2.0 || a[2] != 3.0 {
		t.Errorf("sort failed: %v", a)
	}
}

func TestSortFloat64s_Duplicates(t *testing.T) {
	a := []float64{5.0, 1.0, 3.0, 1.0, 5.0}
	sortFloat64s(a)
	for i := 1; i < len(a); i++ {
		if a[i] < a[i-1] {
			t.Errorf("not sorted at index %d: %v", i, a)
		}
	}
}

func TestSortFloat64s_SingleElement(t *testing.T) {
	a := []float64{42.0}
	sortFloat64s(a)
	if a[0] != 42.0 {
		t.Errorf("single element changed: %v", a)
	}
}

// --- enrichJobDetail tests ---

func TestEnrichJobDetail_LatencyMetrics(t *testing.T) {
	data := map[string]interface{}{
		"NodesWithMetrics": []interface{}{
			map[string]interface{}{"latency_ms": 100.0, "node_id": "n1"},
			map[string]interface{}{"latency_ms": 200.0, "node_id": "n2"},
			map[string]interface{}{"latency_ms": 300.0, "node_id": "n3"},
		},
	}

	enrichJobDetail(data)

	computed, ok := data["computed"].(map[string]interface{})
	if !ok {
		t.Fatal("expected computed map")
	}

	avg, ok := computed["avg_node_latency_ms"].(float64)
	if !ok {
		t.Fatal("expected avg_node_latency_ms")
	}
	if avg != 200.0 {
		t.Errorf("avg latency = %v, want 200.0", avg)
	}

	p95, ok := computed["p95_node_latency_ms"].(float64)
	if !ok {
		t.Fatal("expected p95_node_latency_ms")
	}
	if p95 != 300.0 {
		t.Errorf("p95 latency = %v, want 300.0", p95)
	}
}

func TestEnrichJobDetail_ZeroLatencyNodesIgnored(t *testing.T) {
	// Nodes with latency_ms == 0 should be excluded from the calculation.
	data := map[string]interface{}{
		"NodesWithMetrics": []interface{}{
			map[string]interface{}{"latency_ms": 0.0, "node_id": "n1"},
			map[string]interface{}{"latency_ms": 400.0, "node_id": "n2"},
		},
	}

	enrichJobDetail(data)

	computed := data["computed"].(map[string]interface{})
	avg := computed["avg_node_latency_ms"].(float64)
	if avg != 400.0 {
		t.Errorf("expected avg 400.0 (only non-zero nodes), got %v", avg)
	}
}

func TestEnrichJobDetail_NoNodes_NoLatencyKeys(t *testing.T) {
	data := map[string]interface{}{}
	enrichJobDetail(data)

	computed, ok := data["computed"].(map[string]interface{})
	if !ok {
		t.Fatal("expected computed map")
	}
	if _, ok := computed["avg_node_latency_ms"]; ok {
		t.Error("should not have avg_node_latency_ms when no nodes")
	}
}

func TestEnrichJobDetail_AttemptCounting(t *testing.T) {
	data := map[string]interface{}{
		"Attempts": []interface{}{
			map[string]interface{}{"attempt": 1.0, "node_id": "n1"},
			map[string]interface{}{"attempt": 2.0, "node_id": "n1"}, // retry
			map[string]interface{}{"attempt": 1.0, "node_id": "n2"},
		},
	}

	enrichJobDetail(data)

	computed := data["computed"].(map[string]interface{})
	total := computed["total_attempts"].(int)
	retries := computed["retry_attempts"].(int)

	if total != 3 {
		t.Errorf("total_attempts = %d, want 3", total)
	}
	if retries != 1 {
		t.Errorf("retry_attempts = %d, want 1", retries)
	}
}

func TestEnrichJobDetail_LifecycleTiming(t *testing.T) {
	data := map[string]interface{}{
		"Lifecycle": map[string]interface{}{
			"ExecutionDurationMs":     800.0,
			"QueueDelayMs":            200.0,
			"ActiveAttemptDurationMs": 600.0,
			"IdleDurationMs":          200.0,
		},
	}

	enrichJobDetail(data)

	computed := data["computed"].(map[string]interface{})

	e2e := computed["end_to_end_ms"].(float64)
	if e2e != 1000.0 {
		t.Errorf("end_to_end_ms = %v, want 1000.0", e2e)
	}

	activeShare := computed["active_share_pct"].(float64)
	// 600 / (600+200) * 100 = 75%
	if activeShare != 75.0 {
		t.Errorf("active_share_pct = %v, want 75.0", activeShare)
	}

	queueDelay := computed["queue_delay_ms"].(float64)
	if queueDelay != 200.0 {
		t.Errorf("queue_delay_ms = %v, want 200.0", queueDelay)
	}
}

// --- jobsTable render tests ---

func TestJobsTable_EmptyData(t *testing.T) {
	var buf bytes.Buffer
	// Non-map data — should not panic.
	jobsTable(&buf, nil, "table")
	jobsTable(&buf, "string", "table")
}

func TestJobsTable_WithJobs(t *testing.T) {
	data := map[string]interface{}{
		"Jobs": []interface{}{
			map[string]interface{}{
				"id":          "job-123",
				"status":      "completed",
				"model":       "openai/gpt-4o",
				"DisplayCost": 0.0123,
				"created_at":  "2024-01-01T00:00:00Z",
			},
		},
	}

	var buf bytes.Buffer
	jobsTable(&buf, data, "table")
	out := buf.String()

	if !strings.Contains(out, "job-123") {
		t.Error("expected job ID in table output")
	}
	if !strings.Contains(out, "completed") {
		t.Error("expected status in table output")
	}
}

func TestJobAgentRunsTableSlimRows(t *testing.T) {
	data := map[string]interface{}{
		"agent_runs": []interface{}{
			map[string]interface{}{
				"node_id":             "agent-a",
				"attempt":             2.0,
				"run_kind":            "novo_run",
				"status":              "completed",
				"harness":             "claude-code",
				"external_run_id":     "novomo-run-123456789012345",
				"external_job_run_id": "jobrun-123456789012345",
				"external_task_id":    "task-123456789012345",
				"inherit_from_json":   `{"kind":"job_run","id":"jobrun-parent","policy":"latest"}`,
				"tokens_input":        10.0,
				"tokens_output":       5.0,
				"cost_usd":            0.12,
			},
		},
	}
	var buf bytes.Buffer
	jobAgentRunsTable(&buf, data, "table")
	out := buf.String()
	for _, want := range []string{"agent-a", "novo_run", "task-123456789012345", "jobrun-123456789012345", "novomo-run-123456789012345", "job_run:jobrun-parent", "claude-code", "completed", "15", "$0.1200"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "...") {
		t.Fatalf("expected Novomo IDs to be untruncated, got:\n%s", out)
	}
}

// --- jobTraceTable render tests ---

func TestJobTraceTable_EmptyGroups(t *testing.T) {
	data := map[string]interface{}{
		"job_id":      "job-abc",
		"node_groups": []interface{}{},
	}

	var buf bytes.Buffer
	jobTraceTable(&buf, data, "table")
	out := buf.String()

	if !strings.Contains(out, "job-abc") {
		t.Error("expected job ID in trace header")
	}
	if !strings.Contains(out, "no trace spans") {
		t.Error("expected empty trace message")
	}
}

func TestJobTraceTable_WithSpans(t *testing.T) {
	data := map[string]interface{}{
		"job_id": "job-xyz",
		"node_groups": []interface{}{
			map[string]interface{}{
				"node_id": "node-1",
				"spans": []interface{}{
					map[string]interface{}{
						"kind":        "call",
						"status":      "ok",
						"duration_ms": 123.0,
						"attributes":  map[string]interface{}{"model": "gpt-4o"},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	jobTraceTable(&buf, data, "table")
	out := buf.String()

	if !strings.Contains(out, "node-1") {
		t.Error("expected node ID in trace output")
	}
	if !strings.Contains(out, "123ms") {
		t.Error("expected duration in trace output")
	}
}

// --- jobChildrenTable render tests ---

func TestJobChildrenTable_NoChildren(t *testing.T) {
	data := map[string]interface{}{
		"Jobs": []interface{}{},
	}

	var buf bytes.Buffer
	jobChildrenTable(&buf, data, "table")
	out := buf.String()

	if !strings.Contains(out, "no child jobs") {
		t.Error("expected empty children message")
	}
}
