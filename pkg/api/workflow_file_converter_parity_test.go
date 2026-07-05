package api

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/alhasaniq/consortium/pkg/jobs"
	"github.com/alhasaniq/consortium/pkg/workflow"
)

func TestWorkflowFileConverterParityWithJobsConverter(t *testing.T) {
	raw := map[string]interface{}{
		"id":          "wf-parity",
		"name":        "Parity Workflow",
		"description": "converter parity test",
		"nodes": []interface{}{
			map[string]interface{}{
				"id":   "input-1",
				"type": "input",
				"data": map[string]interface{}{
					"type":  "input",
					"label": "Input",
					"config": map[string]interface{}{
						"name": "Input",
					},
				},
			},
			map[string]interface{}{
				"id":   "agent-1",
				"type": "agent",
				"data": map[string]interface{}{
					"type":  "agent",
					"label": "Agent",
					"config": map[string]interface{}{
						"name":           "Agent 1",
						"model":          "openai/gpt-4o-mini",
						"systemPrompt":   "You are helpful",
						"userPrompt":     "Question: {{user_prompt}}",
						"temperature":    0.0,
						"maxTokens":      256,
						"timeoutSeconds": 30,
						"retryPolicy": map[string]interface{}{
							"max_attempts":     1,
							"backoff_ms":       0,
							"backoff_multiply": 1.0,
							"max_backoff_ms":   0,
						},
					},
				},
			},
			map[string]interface{}{
				"id":   "result-1",
				"type": "result",
				"data": map[string]interface{}{
					"type":  "result",
					"label": "Result",
					"config": map[string]interface{}{
						"name":              "final_answer",
						"outputFormat":      "text",
						"aggregationMethod": "collect",
						"timeoutSeconds":    30,
						"retryPolicy": map[string]interface{}{
							"max_attempts":     1,
							"backoff_ms":       0,
							"backoff_multiply": 1.0,
							"max_backoff_ms":   0,
						},
					},
				},
			},
		},
		"edges": []interface{}{
			map[string]interface{}{"id": "e1", "source": "input-1", "target": "agent-1"},
			map[string]interface{}{"id": "e2", "source": "agent-1", "target": "result-1"},
		},
	}

	inputs := map[string]interface{}{
		"user_prompt": "What is 2+2?",
	}

	apiWF, err := workflowFromWorkflowFile(raw, inputs)
	if err != nil {
		t.Fatalf("api workflow conversion failed: %v", err)
	}

	definition, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal definition failed: %v", err)
	}
	jobsWF, err := jobs.WorkflowFromDefinition(string(definition), inputs)
	if err != nil {
		t.Fatalf("jobs workflow conversion failed: %v", err)
	}

	assertWorkflowParity(t, apiWF, jobsWF)
}

func TestWorkflowFileConverterParityWithAggregationMacro(t *testing.T) {
	raw := map[string]interface{}{
		"id":          "wf-parity-aggregation",
		"name":        "Parity Aggregation Workflow",
		"description": "aggregation macro converter parity test",
		"nodes": []interface{}{
			map[string]interface{}{
				"id":   "agent-a",
				"type": "agent",
				"data": map[string]interface{}{
					"type":  "agent",
					"label": "Agent A",
					"config": map[string]interface{}{
						"name":           "Agent A",
						"model":          "openai/gpt-4o-mini",
						"systemPrompt":   "Answer A",
						"temperature":    0.0,
						"maxTokens":      256,
						"timeoutSeconds": 30,
					},
				},
			},
			map[string]interface{}{
				"id":   "agent-b",
				"type": "agent",
				"data": map[string]interface{}{
					"type":  "agent",
					"label": "Agent B",
					"config": map[string]interface{}{
						"name":           "Agent B",
						"model":          "anthropic/claude-sonnet-4.5",
						"systemPrompt":   "Answer B",
						"temperature":    0.0,
						"maxTokens":      256,
						"timeoutSeconds": 30,
					},
				},
			},
			map[string]interface{}{
				"id":   "agg-vote",
				"type": "aggregation",
				"data": map[string]interface{}{
					"type":  "aggregation",
					"label": "Vote",
					"config": map[string]interface{}{
						"name":              "Vote",
						"aggregationMethod": "majority_vote",
						"aggregationConfig": map[string]interface{}{
							"extraction_strategy": "regex",
							"extraction_pattern":  "Answer:\\s*([A-D])",
							"tie_breaker":         "first",
						},
					},
				},
			},
			map[string]interface{}{
				"id":   "result-vote",
				"type": "result",
				"data": map[string]interface{}{
					"type":  "result",
					"label": "Vote Result",
					"config": map[string]interface{}{
						"name":         "voted_answer",
						"outputFormat": "text",
					},
				},
			},
		},
		"edges": []interface{}{
			map[string]interface{}{"id": "e1", "source": "agent-a", "target": "agg-vote"},
			map[string]interface{}{"id": "e2", "source": "agent-b", "target": "agg-vote"},
			map[string]interface{}{"id": "e3", "source": "agg-vote", "target": "result-vote"},
		},
	}

	apiWF, err := workflowFromWorkflowFile(raw, nil)
	if err != nil {
		t.Fatalf("api workflow conversion failed: %v", err)
	}

	definition, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal definition failed: %v", err)
	}
	jobsWF, err := jobs.WorkflowFromDefinition(string(definition), nil)
	if err != nil {
		t.Fatalf("jobs workflow conversion failed: %v", err)
	}

	assertWorkflowParity(t, apiWF, jobsWF)
}

func assertWorkflowParity(t *testing.T, got, want *workflow.Workflow) {
	t.Helper()
	if got == nil || want == nil {
		t.Fatalf("unexpected nil workflows: got=%v want=%v", got, want)
	}
	if got.ID != want.ID {
		t.Fatalf("workflow id mismatch: got=%q want=%q", got.ID, want.ID)
	}
	if got.Name != want.Name {
		t.Fatalf("workflow name mismatch: got=%q want=%q", got.Name, want.Name)
	}
	if got.Description != want.Description {
		t.Fatalf("workflow description mismatch: got=%q want=%q", got.Description, want.Description)
	}
	if len(got.Nodes) != len(want.Nodes) {
		t.Fatalf("workflow node count mismatch: got=%d want=%d", len(got.Nodes), len(want.Nodes))
	}
	if len(got.Edges) != len(want.Edges) {
		t.Fatalf("workflow edge count mismatch: got=%d want=%d", len(got.Edges), len(want.Edges))
	}
	if len(got.Context) != len(want.Context) {
		t.Fatalf("workflow context size mismatch: got=%d want=%d", len(got.Context), len(want.Context))
	}
	for key, wantVal := range want.Context {
		if got.Context[key] != wantVal {
			t.Fatalf("workflow context mismatch for key %q: got=%v want=%v", key, got.Context[key], wantVal)
		}
	}

	for i := range got.Nodes {
		g := got.Nodes[i]
		w := want.Nodes[i]
		if g.ID != w.ID || g.Type != w.Type || g.Model != w.Model || g.Prompt != w.Prompt || g.SystemPrompt != w.SystemPrompt {
			t.Fatalf("node mismatch at index=%d: got=%+v want=%+v", i, *g, *w)
		}
		if g.AggregationMethod != w.AggregationMethod || g.OutputFormat != w.OutputFormat || g.OutputName != w.OutputName {
			t.Fatalf("result node config mismatch at index=%d: got=%+v want=%+v", i, *g, *w)
		}
		if !reflect.DeepEqual(g.AggregationConfig, w.AggregationConfig) {
			t.Fatalf("aggregation config mismatch at index=%d: got=%+v want=%+v", i, g.AggregationConfig, w.AggregationConfig)
		}
		if !reflect.DeepEqual(g.Metadata, w.Metadata) {
			t.Fatalf("metadata mismatch at index=%d: got=%+v want=%+v", i, g.Metadata, w.Metadata)
		}
	}

	for i := range got.Edges {
		g := got.Edges[i]
		w := want.Edges[i]
		if g.ID != w.ID || g.Source != w.Source || g.Target != w.Target {
			t.Fatalf("edge mismatch at index=%d: got=%+v want=%+v", i, *g, *w)
		}
	}
}
