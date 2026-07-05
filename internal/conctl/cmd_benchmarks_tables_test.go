package conctl

import (
	"bytes"
	"strings"
	"testing"
)

func TestBenchmarkAnalysisTableIncludesPerformanceSections(t *testing.T) {
	data := map[string]interface{}{
		"workflow_id": "benchmark-judge-pick-cheap",
		"benchmark":   "global-mmlu-lite",
		"split":       "dev",
		"summary": map[string]interface{}{
			"total_incorrect":          1.0,
			"all_steps_wrong":          1.0,
			"some_right_child_wrong":   0.0,
			"all_right_child_wrong":    0.0,
			"child_right_parent_wrong": 0.0,
			"unclassified":             0.0,
		},
		"items": []interface{}{
			map[string]interface{}{
				"item_id":          "row-17",
				"subject":          "college_chemistry",
				"answer_label":     "A",
				"parent_predicted": "C",
				"child_predicted":  "C",
				"category":         "all_steps_wrong",
				"agent_answers": []interface{}{
					map[string]interface{}{
						"model":    "model-a",
						"answer":   "C",
						"correct":  false,
						"parse_ok": true,
					},
				},
			},
		},
		"performance": map[string]interface{}{
			"total_items":           10.0,
			"items_with_child_data": 8.0,
			"agent_models": []interface{}{
				map[string]interface{}{
					"model":          "model-a",
					"samples":        8.0,
					"accuracy":       0.5,
					"parse_rate":     1.0,
					"total_retries":  3.0,
					"avg_latency_ms": 120.0,
					"p95_latency_ms": 200.0,
					"total_cost_usd": 1.23,
					"avg_cost_usd":   0.15375,
				},
			},
			"aggregation_nodes": []interface{}{
				map[string]interface{}{
					"node_id":        "result-judge",
					"models":         []interface{}{"judge-model"},
					"samples":        8.0,
					"accuracy":       0.625,
					"parse_rate":     0.875,
					"total_retries":  2.0,
					"avg_latency_ms": 900.0,
					"p95_latency_ms": 1400.0,
					"total_cost_usd": 0.55,
					"avg_cost_usd":   0.06875,
				},
			},
		},
		"diagnostics": map[string]interface{}{
			"top_n": 2.0,
			"slowest_items": []interface{}{
				map[string]interface{}{
					"item_id":           "row-17",
					"subject":           "college_chemistry",
					"total_latency_ms":  2200.0,
					"parent_latency_ms": 600.0,
					"child_latency_ms":  1600.0,
					"parent_retries":    1.0,
					"child_retries":     2.0,
					"total_retries":     3.0,
				},
			},
			"most_retries_items": []interface{}{
				map[string]interface{}{
					"item_id":          "row-17",
					"subject":          "college_chemistry",
					"total_latency_ms": 2200.0,
					"parent_retries":   1.0,
					"child_retries":    2.0,
					"total_retries":    3.0,
				},
			},
		},
	}

	var buf bytes.Buffer
	benchmarkAnalysisTable(&buf, data, "md")
	out := buf.String()

	assertContains := func(substr string) {
		t.Helper()
		if !strings.Contains(out, substr) {
			t.Fatalf("expected output to contain %q\n\nOutput:\n%s", substr, out)
		}
	}

	assertContains("Model/Node Performance (all items with child data: 8/10)")
	assertContains("Agent Model Performance:")
	assertContains("Aggregation Node Performance:")
	assertContains("Slowest Items (top 2):")
	assertContains("Most Retries (top 2):")
	assertContains("result-judge")
	assertContains("model-a")
	assertContains("judge-model")
	assertContains("row-17")
}

func TestBenchmarkAnalysisTableStillShowsPerformanceWithoutIncorrectItems(t *testing.T) {
	data := map[string]interface{}{
		"workflow_id": "benchmark-judge-pick-cheap",
		"benchmark":   "global-mmlu-lite",
		"split":       "dev",
		"summary": map[string]interface{}{
			"total_incorrect":          0.0,
			"all_steps_wrong":          0.0,
			"some_right_child_wrong":   0.0,
			"all_right_child_wrong":    0.0,
			"child_right_parent_wrong": 0.0,
			"unclassified":             0.0,
		},
		"items": []interface{}{},
		"performance": map[string]interface{}{
			"total_items":           1.0,
			"items_with_child_data": 1.0,
			"agent_models": []interface{}{
				map[string]interface{}{
					"model":          "model-a",
					"samples":        1.0,
					"accuracy":       1.0,
					"parse_rate":     1.0,
					"total_retries":  0.0,
					"avg_latency_ms": 100.0,
					"p95_latency_ms": 100.0,
					"total_cost_usd": 0.01,
					"avg_cost_usd":   0.01,
				},
			},
		},
		"diagnostics": map[string]interface{}{
			"top_n": 1.0,
			"slowest_items": []interface{}{
				map[string]interface{}{
					"item_id":           "row-1",
					"subject":           "math",
					"total_latency_ms":  300.0,
					"parent_latency_ms": 100.0,
					"child_latency_ms":  200.0,
					"parent_retries":    0.0,
					"child_retries":     0.0,
					"total_retries":     0.0,
				},
			},
			"most_retries_items": []interface{}{
				map[string]interface{}{
					"item_id":          "row-1",
					"subject":          "math",
					"total_retries":    0.0,
					"parent_retries":   0.0,
					"child_retries":    0.0,
					"total_latency_ms": 300.0,
				},
			},
		},
	}

	var buf bytes.Buffer
	benchmarkAnalysisTable(&buf, data, "md")
	out := buf.String()

	assertContains := func(substr string) {
		t.Helper()
		if !strings.Contains(out, substr) {
			t.Fatalf("expected output to contain %q\n\nOutput:\n%s", substr, out)
		}
	}

	assertContains("(no incorrect items)")
	assertContains("Model/Node Performance (all items with child data: 1/1)")
	assertContains("Slowest Items (top 1):")
	assertContains("Most Retries (top 1):")
}
