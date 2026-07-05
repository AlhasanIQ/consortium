package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/gorilla/mux"
)

func TestHandleCompilePreviewExpandsScoringAndPeerMatrixJobs(t *testing.T) {
	tests := []struct {
		name             string
		method           string
		sourceWorkflowID string
		wantPromptIDs    []string
		wantGroupID      string
		wantPresentation string
	}{
		{
			name:             "scoring creates one scorer per upstream candidate",
			method:           "scoring",
			sourceWorkflowID: "aggregation-scoring",
			wantPromptIDs: []string{
				"agg--score-agent-a",
				"agg--score-agent-b",
				"agg--score-agent-c",
			},
			wantGroupID:      "agg--result",
			wantPresentation: "final",
		},
		{
			name:             "peer matrix creates reviewer candidate cross product without self review",
			method:           "peer_matrix",
			sourceWorkflowID: "aggregation-peer-matrix",
			wantPromptIDs: []string{
				"agg--review-agent-a-agent-b",
				"agg--review-agent-a-agent-c",
				"agg--review-agent-b-agent-a",
				"agg--review-agent-b-agent-c",
				"agg--review-agent-c-agent-a",
				"agg--review-agent-c-agent-b",
			},
			wantGroupID:      "agg--result",
			wantPresentation: "final",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api, _ := setupWorkflowAPI(t)

			router := mux.NewRouter()
			api.RegisterRoutes(router)

			w := serveHTTP(t, router, "POST", "/api/workflows/compile-preview", map[string]interface{}{
				"workflow_file": builderAggregationWorkflowFile(tt.method, tt.sourceWorkflowID),
			})
			if w.Code != http.StatusOK {
				t.Fatalf("compile preview status = %d body=%s", w.Code, w.Body.String())
			}

			resp := decodeJSON(t, w)
			nodes := responseArray(t, resp, "nodes")
			edges := responseArray(t, resp, "edges")
			groups := responseArray(t, resp, "aggregation_groups")

			for _, id := range tt.wantPromptIDs {
				node := findResponseNode(t, nodes, id)
				if got := node["type"]; got != "prompt" {
					t.Fatalf("%s type = %v, want prompt", id, got)
				}
				metadata := responseMap(t, node, "metadata")
				if metadata["aggregation_anchor_id"] != "agg" {
					t.Fatalf("%s metadata aggregation_anchor_id = %v", id, metadata["aggregation_anchor_id"])
				}
				if metadata["aggregation_group_node_id"] != tt.wantGroupID {
					t.Fatalf("%s metadata aggregation_group_node_id = %v", id, metadata["aggregation_group_node_id"])
				}
			}

			if tt.method == "peer_matrix" {
				for _, illegal := range []string{
					"agg--review-agent-a-agent-a",
					"agg--review-agent-b-agent-b",
					"agg--review-agent-c-agent-c",
				} {
					if hasResponseNode(nodes, illegal) {
						t.Fatalf("peer matrix preview included self-review node %q", illegal)
					}
				}
			}

			assertPreviewEdge(t, edges, "agent-a", firstExpectedRoot(tt.method, "agent-a"))
			assertPreviewEdge(t, edges, "agent-b", firstExpectedRoot(tt.method, "agent-b"))
			assertPreviewEdge(t, edges, "agent-c", firstExpectedRoot(tt.method, "agent-c"))

			group := findResponseGroup(t, groups, "agg")
			if group["method"] != tt.method {
				t.Fatalf("group method = %v, want %s", group["method"], tt.method)
			}
			if group["terminal_node_id"] != tt.wantGroupID {
				t.Fatalf("group terminal_node_id = %v, want %s", group["terminal_node_id"], tt.wantGroupID)
			}
			if group["presentation_result_id"] != tt.wantPresentation {
				t.Fatalf("presentation_result_id = %v, want %s", group["presentation_result_id"], tt.wantPresentation)
			}
			if got := int(group["llm_job_count"].(float64)); got != len(tt.wantPromptIDs) {
				t.Fatalf("llm_job_count = %d, want %d", got, len(tt.wantPromptIDs))
			}
			if got := responseStringSlice(t, group, "input_node_ids"); strings.Join(got, ",") != "agent-a,agent-b,agent-c" {
				t.Fatalf("input_node_ids = %v", got)
			}
		})
	}
}

func TestHandleCompilePreviewReturnsBadRequestForMissingAggregationSource(t *testing.T) {
	api, _ := setupWorkflowAPI(t)
	router := mux.NewRouter()
	api.RegisterRoutes(router)

	w := serveHTTP(t, router, "POST", "/api/workflows/compile-preview", map[string]interface{}{
		"workflow_file": builderAggregationWorkflowFile("scoring", "aggregation-does-not-exist"),
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("compile preview status = %d body=%s, want 400", w.Code, w.Body.String())
	}
	resp := decodeJSON(t, w)
	if resp["code"] != ErrCodeInvalidWorkflow {
		t.Fatalf("code = %v, want %s", resp["code"], ErrCodeInvalidWorkflow)
	}
}

func TestHandleCompilePreviewOmitsEmptyGroupsForPlainWorkflowRefs(t *testing.T) {
	api, _ := setupWorkflowAPI(t)
	router := mux.NewRouter()
	api.RegisterRoutes(router)

	w := serveHTTP(t, router, "POST", "/api/workflows/compile-preview", map[string]interface{}{
		"input_values": map[string]interface{}{"user_prompt": "Summarize the task."},
		"workflow_file": map[string]interface{}{
			"version": "1.0.0",
			"id":      "plain-ref-preview",
			"name":    "Plain Ref Preview",
			"nodes": []interface{}{
				map[string]interface{}{
					"id":       "delegate",
					"type":     "workflow_ref",
					"position": map[string]interface{}{"x": 260, "y": 160},
					"data": map[string]interface{}{
						"type":  "workflow_ref",
						"label": "Delegate",
						"config": map[string]interface{}{
							"workflowId": "agent-run-novomo-basic",
							"inputTemplate": map[string]interface{}{
								"user_prompt": "{{user_prompt}}",
							},
							"outputKey": "agent_output",
						},
					},
				},
				map[string]interface{}{
					"id":       "final",
					"type":     "result",
					"position": map[string]interface{}{"x": 620, "y": 160},
					"data": map[string]interface{}{
						"type":  "result",
						"label": "Final",
						"config": map[string]interface{}{
							"name":              "final_answer",
							"outputFormat":      "text",
							"aggregationMethod": "collect",
							"retryPolicy": map[string]interface{}{
								"max_attempts": 1,
							},
						},
					},
				},
			},
			"edges": []interface{}{
				map[string]interface{}{"id": "delegate-final", "source": "delegate", "target": "final"},
			},
		},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("compile preview status = %d body=%s", w.Code, w.Body.String())
	}

	resp := decodeJSON(t, w)
	groups := responseArray(t, resp, "aggregation_groups")
	if len(groups) != 0 {
		t.Fatalf("aggregation_groups = %v, want none for plain workflow_ref expansion", groups)
	}
}

func TestHandleCompilePreviewReturnsBadRequestForEmptyPayload(t *testing.T) {
	api, _ := setupWorkflowAPI(t)
	router := mux.NewRouter()
	api.RegisterRoutes(router)

	w := serveHTTP(t, router, "POST", "/api/workflows/compile-preview", map[string]interface{}{})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("compile preview status = %d body=%s, want 400", w.Code, w.Body.String())
	}
	resp := decodeJSON(t, w)
	if resp["code"] != ErrCodeInvalidWorkflow {
		t.Fatalf("code = %v, want %s", resp["code"], ErrCodeInvalidWorkflow)
	}
}

func TestHandleCompilePreviewReturnsBadRequestForContentlessWorkflowFile(t *testing.T) {
	api, _ := setupWorkflowAPI(t)
	router := mux.NewRouter()
	api.RegisterRoutes(router)

	w := serveHTTP(t, router, "POST", "/api/workflows/compile-preview", map[string]interface{}{
		"workflow_file": map[string]interface{}{"version": "1.0.0"},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("compile preview status = %d body=%s, want 400", w.Code, w.Body.String())
	}
	resp := decodeJSON(t, w)
	if resp["code"] != ErrCodeInvalidWorkflow {
		t.Fatalf("code = %v, want %s", resp["code"], ErrCodeInvalidWorkflow)
	}
}

func TestHandleCompilePreviewValidatesCompiledWorkflow(t *testing.T) {
	api, _ := setupWorkflowAPI(t)
	router := mux.NewRouter()
	api.RegisterRoutes(router)

	w := serveHTTP(t, router, "POST", "/api/workflows/compile-preview", map[string]interface{}{
		"workflow": map[string]interface{}{
			"id":   "invalid-preview",
			"name": "Invalid Preview",
			"nodes": []interface{}{
				map[string]interface{}{
					"id":     "prompt-without-model",
					"type":   "prompt",
					"prompt": "Answer.",
				},
			},
		},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("compile preview status = %d body=%s, want 400", w.Code, w.Body.String())
	}
	resp := decodeJSON(t, w)
	if resp["code"] != ErrCodeInvalidWorkflow {
		t.Fatalf("code = %v, want %s", resp["code"], ErrCodeInvalidWorkflow)
	}
}

func TestHandleCompilePreviewCountsConditionalLLMJobs(t *testing.T) {
	api, _ := setupWorkflowAPI(t)

	router := mux.NewRouter()
	api.RegisterRoutes(router)

	w := serveHTTP(t, router, "POST", "/api/workflows/compile-preview", map[string]interface{}{
		"workflow_file": builderAggregationWorkflowFile("judge", "aggregation-judge"),
	})
	if w.Code != http.StatusOK {
		t.Fatalf("compile preview status = %d body=%s", w.Code, w.Body.String())
	}

	resp := decodeJSON(t, w)
	groups := responseArray(t, resp, "aggregation_groups")
	group := findResponseGroup(t, groups, "agg")
	if got := int(group["top_level_llm_job_count"].(float64)); got != 1 {
		t.Fatalf("top_level_llm_job_count = %d, want 1", got)
	}
	if got := int(group["conditional_llm_job_count"].(float64)); got != 1 {
		t.Fatalf("conditional_llm_job_count = %d, want 1", got)
	}
	if got := int(group["llm_job_count"].(float64)); got != 2 {
		t.Fatalf("llm_job_count = %d, want 2", got)
	}
	conditionalJobs := responseArray(t, group, "conditional_llm_jobs")
	if len(conditionalJobs) != 1 {
		t.Fatalf("conditional_llm_jobs length = %d, want 1", len(conditionalJobs))
	}
	job := conditionalJobs[0].(map[string]interface{})
	if job["id"] != "agg--repair-selection_true" {
		t.Fatalf("conditional job id = %v, want runtime branch carrier id agg--repair-selection_true", job["id"])
	}
	if job["parent_node_id"] != "agg--repair-selection" || job["branch"] != "true" {
		t.Fatalf("conditional job = %+v, want true branch of agg--repair-selection", job)
	}
	if job["type"] != "prompt" {
		t.Fatalf("conditional job type = %v, want prompt", job["type"])
	}
	if job["model"] != "judge-model" {
		t.Fatalf("conditional job model = %v, want judge-model", job["model"])
	}
	if prompt, _ := job["prompt"].(string); !strings.Contains(prompt, "Return only the winning response label") {
		t.Fatalf("conditional job prompt = %q, want repair prompt", prompt)
	}
	if maxTokens, _ := job["max_tokens"].(float64); maxTokens <= 0 {
		t.Fatalf("conditional job max_tokens = %v, want positive max_tokens", job["max_tokens"])
	}
}

func TestCompilePreviewConditionalJobsUsesRuntimeIDsForNestedBranches(t *testing.T) {
	jobs := compilePreviewConditionalJobs(&workflow.Node{
		ID:   "outer-condition",
		Type: workflow.NodeTypeConditional,
		TrueBranch: &workflow.Node{
			ID:        "source-inner-condition",
			Type:      workflow.NodeTypeConditional,
			Condition: "needs_repair equals yes",
			FalseBranch: &workflow.Node{
				ID:    "source-repair-call",
				Type:  workflow.NodeTypePrompt,
				Model: "repair-model",
			},
		},
	})
	if len(jobs) != 1 {
		t.Fatalf("conditional jobs length = %d, want 1: %+v", len(jobs), jobs)
	}
	job := jobs[0]
	if job.ID != "outer-condition_true_false" {
		t.Fatalf("conditional job id = %q, want runtime nested branch carrier id outer-condition_true_false", job.ID)
	}
	if job.ParentNodeID != "outer-condition_true" || job.Branch != "false" {
		t.Fatalf("conditional job parent/branch = %q/%q, want outer-condition_true/false", job.ParentNodeID, job.Branch)
	}
}

func TestHandleCompilePreviewCountsDynamicRubricLLMJobs(t *testing.T) {
	tests := []struct {
		name             string
		method           string
		sourceWorkflowID string
		wantLLMJobs      int
	}{
		{
			name:             "scoring adds one dynamic rubric job to candidate scorers",
			method:           "scoring",
			sourceWorkflowID: "aggregation-scoring",
			wantLLMJobs:      4,
		},
		{
			name:             "peer matrix adds one dynamic rubric job to reviewer cross product",
			method:           "peer_matrix",
			sourceWorkflowID: "aggregation-peer-matrix",
			wantLLMJobs:      7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api, _ := setupWorkflowAPI(t)
			router := mux.NewRouter()
			api.RegisterRoutes(router)

			file := builderAggregationWorkflowFile(tt.method, tt.sourceWorkflowID)
			setBuilderAggregationConfig(file, map[string]interface{}{
				"rubric_mode":  "dynamic",
				"rubric_model": "judge-model",
			})
			w := serveHTTP(t, router, "POST", "/api/workflows/compile-preview", map[string]interface{}{
				"workflow_file": file,
			})
			if w.Code != http.StatusOK {
				t.Fatalf("compile preview status = %d body=%s", w.Code, w.Body.String())
			}

			resp := decodeJSON(t, w)
			groups := responseArray(t, resp, "aggregation_groups")
			group := findResponseGroup(t, groups, "agg")
			if got := int(group["llm_job_count"].(float64)); got != tt.wantLLMJobs {
				t.Fatalf("llm_job_count = %d, want %d", got, tt.wantLLMJobs)
			}
			if got := int(group["top_level_llm_job_count"].(float64)); got != tt.wantLLMJobs {
				t.Fatalf("top_level_llm_job_count = %d, want %d", got, tt.wantLLMJobs)
			}
			if got := int(group["conditional_llm_job_count"].(float64)); got != 0 {
				t.Fatalf("conditional_llm_job_count = %d, want 0", got)
			}
		})
	}
}

func builderAggregationWorkflowFile(method, sourceWorkflowID string) map[string]interface{} {
	agent := func(id, model string, x, y int) map[string]interface{} {
		return map[string]interface{}{
			"id":       id,
			"type":     "agent",
			"position": map[string]interface{}{"x": x, "y": y},
			"data": map[string]interface{}{
				"type":  "agent",
				"label": id,
				"config": map[string]interface{}{
					"model":          model,
					"temperature":    0,
					"maxTokens":      256,
					"timeoutSeconds": 30,
					"systemPrompt":   "Answer carefully.",
					"userPrompt":     "{{user_prompt}}",
					"retryPolicy": map[string]interface{}{
						"max_attempts": 1,
					},
				},
			},
		}
	}
	aggregationConfig := map[string]interface{}{
		"scoring_model":       "judge-model",
		"judge_model":         "judge-model",
		"system_prompt":       "Score.",
		"eval_system_prompt":  "Review.",
		"eval_prompt":         "{{candidate}}\n{{rubric}}",
		"prompt":              aggregationPreviewPromptTemplate(method),
		"rubric":              []interface{}{map[string]interface{}{"name": "correctness", "weight": 1.0, "description": "Correctness"}},
		"normalization":       "none",
		"max_parallel":        6,
		"max_tokens":          256,
		"temperature":         0,
		"repair_max_tokens":   128,
		"extraction_strategy": "regex",
		"extraction_pattern":  `Answer:\s*([A-D])`,
	}
	if method == "peer_matrix" {
		delete(aggregationConfig, "judge_model")
	}
	return map[string]interface{}{
		"version":    "1.0.0",
		"id":         "preview-test",
		"name":       "Preview Test",
		"created_at": "2026-06-23T00:00:00Z",
		"updated_at": "2026-06-23T00:00:00Z",
		"nodes": []interface{}{
			agent("agent-a", "model-a", 100, 80),
			agent("agent-b", "model-b", 100, 240),
			agent("agent-c", "model-c", 100, 400),
			map[string]interface{}{
				"id":       "agg",
				"type":     "aggregation",
				"position": map[string]interface{}{"x": 420, "y": 240},
				"data": map[string]interface{}{
					"type":  "aggregation",
					"label": "Aggregation",
					"config": map[string]interface{}{
						"aggregationMethod":     method,
						"aggregationWorkflowId": sourceWorkflowID,
						"aggregationConfig":     aggregationConfig,
						"retryPolicy": map[string]interface{}{
							"max_attempts": 1,
						},
					},
				},
			},
			map[string]interface{}{
				"id":       "final",
				"type":     "result",
				"position": map[string]interface{}{"x": 720, "y": 240},
				"data": map[string]interface{}{
					"type":   "result",
					"label":  "Final",
					"config": map[string]interface{}{"name": "final_answer", "outputFormat": "text"},
				},
			},
		},
		"edges": []interface{}{
			map[string]interface{}{"id": "a-agg", "source": "agent-a", "target": "agg"},
			map[string]interface{}{"id": "b-agg", "source": "agent-b", "target": "agg"},
			map[string]interface{}{"id": "c-agg", "source": "agent-c", "target": "agg"},
			map[string]interface{}{"id": "agg-final", "source": "agg", "target": "final"},
		},
	}
}

func aggregationPreviewPromptTemplate(method string) string {
	switch method {
	case "debate_decide":
		return "{{camps}}"
	case "scoring":
		return "{{candidate}}\n{{rubric}}"
	default:
		return "{{responses}}"
	}
}

func setBuilderAggregationConfig(file map[string]interface{}, values map[string]interface{}) {
	nodes := file["nodes"].([]interface{})
	for _, raw := range nodes {
		node := raw.(map[string]interface{})
		if node["id"] != "agg" {
			continue
		}
		data := node["data"].(map[string]interface{})
		config := data["config"].(map[string]interface{})
		aggConfig := config["aggregationConfig"].(map[string]interface{})
		for key, value := range values {
			aggConfig[key] = value
		}
		return
	}
}

func firstExpectedRoot(method, agentID string) string {
	if method == "scoring" || method == "peer_matrix" {
		return "agg--extract-answer-" + agentID
	}
	return "agg--format-candidates"
}

func responseArray(t *testing.T, m map[string]interface{}, key string) []interface{} {
	t.Helper()
	values, ok := m[key].([]interface{})
	if !ok {
		t.Fatalf("%s = %T, want []interface{}", key, m[key])
	}
	return values
}

func responseMap(t *testing.T, m map[string]interface{}, key string) map[string]interface{} {
	t.Helper()
	values, ok := m[key].(map[string]interface{})
	if !ok {
		t.Fatalf("%s = %T, want map[string]interface{}", key, m[key])
	}
	return values
}

func findResponseNode(t *testing.T, nodes []interface{}, id string) map[string]interface{} {
	t.Helper()
	for _, raw := range nodes {
		node := raw.(map[string]interface{})
		if node["id"] == id {
			return node
		}
	}
	t.Fatalf("node %q not found in %v", id, nodes)
	return nil
}

func hasResponseNode(nodes []interface{}, id string) bool {
	for _, raw := range nodes {
		node := raw.(map[string]interface{})
		if node["id"] == id {
			return true
		}
	}
	return false
}

func findResponseGroup(t *testing.T, groups []interface{}, anchorID string) map[string]interface{} {
	t.Helper()
	for _, raw := range groups {
		group := raw.(map[string]interface{})
		if group["anchor_node_id"] == anchorID {
			return group
		}
	}
	t.Fatalf("group %q not found in %v", anchorID, groups)
	return nil
}

func assertPreviewEdge(t *testing.T, edges []interface{}, source, target string) {
	t.Helper()
	for _, raw := range edges {
		edge := raw.(map[string]interface{})
		if edge["source"] == source && edge["target"] == target {
			return
		}
	}
	t.Fatalf("edge %s -> %s not found in %v", source, target, edges)
}

func responseStringSlice(t *testing.T, m map[string]interface{}, key string) []string {
	t.Helper()
	raw, ok := m[key].([]interface{})
	if !ok {
		t.Fatalf("%s = %T, want []interface{}", key, m[key])
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		out = append(out, item.(string))
	}
	return out
}
