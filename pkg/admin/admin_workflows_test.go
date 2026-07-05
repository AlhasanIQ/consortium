package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/alhasaniq/consortium/pkg/storage"
)

func TestHandleWorkflowUpdateNormalizesIDs(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	seed := &storage.WorkflowDefinition{
		ID:          "wf-admin-update",
		Name:        "Before",
		Description: "before",
		Definition:  `{"id":"wf-admin-update","name":"Before","nodes":[],"edges":[]}`,
	}
	if err := server.storage.CreateWorkflow(seed); err != nil {
		t.Fatalf("create workflow: %v", err)
	}

	payload := map[string]interface{}{
		"name": "After",
		"nodes": []interface{}{
			map[string]interface{}{
				"type": "prompt",
				"data": map[string]interface{}{
					"type": "prompt",
					"config": map[string]interface{}{
						"userPrompt": "hi",
					},
				},
			},
		},
		"edges": []interface{}{
			map[string]interface{}{
				"source": "node-a",
				"target": "node-b",
			},
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/workflows/wf-admin-update", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	updated, err := server.storage.GetWorkflow("wf-admin-update")
	if err != nil {
		t.Fatalf("get updated workflow: %v", err)
	}

	var wf map[string]interface{}
	if err := json.Unmarshal([]byte(updated.Definition), &wf); err != nil {
		t.Fatalf("unmarshal updated definition: %v", err)
	}

	if id, _ := wf["id"].(string); id != "wf-admin-update" {
		t.Fatalf("expected workflow id wf-admin-update, got %q", id)
	}
	nodes, _ := wf["nodes"].([]interface{})
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	node, _ := nodes[0].(map[string]interface{})
	if id, _ := node["id"].(string); id == "" {
		t.Fatal("expected node id to be normalized")
	}
	edges, _ := wf["edges"].([]interface{})
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
	edge, _ := edges[0].(map[string]interface{})
	if id, _ := edge["id"].(string); id == "" {
		t.Fatal("expected edge id to be normalized")
	}
}

func TestHandleWorkflowUpdateNotFound(t *testing.T) {
	_, router, cleanup := setupTestServer(t)
	defer cleanup()

	payload := map[string]interface{}{
		"name": "Missing",
		"nodes": []interface{}{
			map[string]interface{}{"id": "node-1", "type": "prompt"},
		},
		"edges": []interface{}{},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/workflows/wf-missing", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestParseWorkflowDefinitionStats_InitializesCollections(t *testing.T) {
	wf := &storage.WorkflowDefinition{
		ID:         "wf-invalid-json",
		Name:       "Invalid",
		Definition: "not-json",
	}

	stats := parseWorkflowDefinitionStats(wf)
	if stats.NodeTypes == nil {
		t.Fatal("expected NodeTypes to be initialized, got nil")
	}
	if stats.ModelsUsed == nil {
		t.Fatal("expected ModelsUsed to be initialized, got nil")
	}
	if len(stats.NodeTypes) != 0 {
		t.Fatalf("expected empty NodeTypes for invalid definition, got %v", stats.NodeTypes)
	}
	if len(stats.ModelsUsed) != 0 {
		t.Fatalf("expected empty ModelsUsed for invalid definition, got %v", stats.ModelsUsed)
	}
}

func TestParseWorkflowDefinitionStats_SortsModelsUsed(t *testing.T) {
	definition := map[string]any{
		"id":   "wf-model-order",
		"name": "Model order",
		"nodes": []map[string]any{
			{
				"type": "prompt",
				"data": map[string]any{
					"type": "prompt",
					"config": map[string]any{
						"model": "z-model",
					},
				},
			},
			{
				"type": "prompt",
				"data": map[string]any{
					"type": "prompt",
					"config": map[string]any{
						"model": "a-model",
					},
				},
			},
		},
		"edges": []map[string]any{},
	}

	payload, err := json.Marshal(definition)
	if err != nil {
		t.Fatalf("marshal workflow definition: %v", err)
	}

	wf := &storage.WorkflowDefinition{
		ID:         "wf-model-order",
		Name:       "Model order",
		Definition: string(payload),
	}
	stats := parseWorkflowDefinitionStats(wf)

	expected := []string{"a-model", "z-model"}
	if !reflect.DeepEqual(stats.ModelsUsed, expected) {
		t.Fatalf("expected sorted ModelsUsed %v, got %v", expected, stats.ModelsUsed)
	}
}

func TestParseWorkflowDefinitionStats_ExtractsSourceRelationships(t *testing.T) {
	definition := map[string]any{
		"id":   "reasoning-source-demo",
		"name": "Reasoning Source Demo",
		"nodes": []map[string]any{
			{
				"id":   "aggregation",
				"type": "aggregation",
				"data": map[string]any{
					"type": "aggregation",
					"config": map[string]any{
						"aggregationMethod":     "judge",
						"aggregationWorkflowId": "aggregation-judge",
					},
				},
			},
			{
				"id":   "phase-a",
				"type": "workflow_ref",
				"data": map[string]any{
					"type": "workflow_ref",
					"config": map[string]any{
						"workflowId": "reasoning-judge-pick",
					},
				},
			},
			{
				"id":   "phase-b",
				"type": "workflow_ref",
				"data": map[string]any{
					"type": "workflow_ref",
					"config": map[string]any{
						"workflowRefId": "reasoning-informed-captain-synthesis",
					},
				},
			},
			{
				"id":   "child",
				"type": "child_workflow",
				"data": map[string]any{
					"type": "child_workflow",
					"config": map[string]any{
						"childWorkflowId": "reasoning-majority-pick",
					},
				},
			},
		},
		"edges": []map[string]any{},
	}
	payload, err := json.Marshal(definition)
	if err != nil {
		t.Fatalf("marshal workflow definition: %v", err)
	}

	stats := parseWorkflowDefinitionStats(&storage.WorkflowDefinition{
		ID:         "reasoning-source-demo",
		Name:       "Reasoning Source Demo",
		Definition: string(payload),
	})

	if !reflect.DeepEqual(stats.AggregationSourceIDs, []string{"aggregation-judge"}) {
		t.Fatalf("AggregationSourceIDs = %v", stats.AggregationSourceIDs)
	}
	if !reflect.DeepEqual(stats.WorkflowRefIDs, []string{"reasoning-informed-captain-synthesis", "reasoning-judge-pick"}) {
		t.Fatalf("WorkflowRefIDs = %v", stats.WorkflowRefIDs)
	}
	if !reflect.DeepEqual(stats.ChildWorkflowIDs, []string{"reasoning-majority-pick"}) {
		t.Fatalf("ChildWorkflowIDs = %v", stats.ChildWorkflowIDs)
	}
	if stats.ReferencesL0DirectlyAsBenchmark {
		t.Fatalf("non-benchmark workflow should not warn about direct L0 references")
	}
}

func TestParseWorkflowDefinitionStats_FlagsBenchmarkDirectL0References(t *testing.T) {
	definition := map[string]any{
		"id":   "benchmark-direct-l0",
		"name": "Benchmark Direct L0",
		"nodes": []map[string]any{
			{
				"id":   "bad-ref",
				"type": "workflow_ref",
				"data": map[string]any{
					"type": "workflow_ref",
					"config": map[string]any{
						"workflowId": "aggregation-synthesis",
					},
				},
			},
		},
		"edges": []map[string]any{},
	}
	payload, err := json.Marshal(definition)
	if err != nil {
		t.Fatalf("marshal workflow definition: %v", err)
	}

	stats := parseWorkflowDefinitionStats(&storage.WorkflowDefinition{
		ID:         "benchmark-direct-l0",
		Name:       "Benchmark Direct L0",
		Definition: string(payload),
	})

	if !stats.ReferencesL0DirectlyAsBenchmark {
		t.Fatalf("expected direct L3 -> L0 warning for workflow refs: %+v", stats)
	}
}

func TestParseWorkflowDefinitionStats_FlagsBenchmarkChildWorkflowDirectL0References(t *testing.T) {
	definition := map[string]any{
		"id":   "benchmark-child-direct-l0",
		"name": "Benchmark Child Direct L0",
		"nodes": []map[string]any{
			{
				"id":   "bad-child",
				"type": "child_workflow",
				"data": map[string]any{
					"type": "child_workflow",
					"config": map[string]any{
						"childWorkflowId": "aggregation-synthesis",
					},
				},
			},
		},
		"edges": []map[string]any{},
	}
	payload, err := json.Marshal(definition)
	if err != nil {
		t.Fatalf("marshal workflow definition: %v", err)
	}

	stats := parseWorkflowDefinitionStats(&storage.WorkflowDefinition{
		ID:         "benchmark-child-direct-l0",
		Name:       "Benchmark Child Direct L0",
		Definition: string(payload),
	})

	if !stats.ReferencesL0DirectlyAsBenchmark {
		t.Fatalf("expected direct L3 child_workflow -> L0 warning for workflow refs: %+v", stats)
	}
}

func TestParseWorkflowDefinitionStats_AllowsBenchmarkOutputPackagingAggregationSource(t *testing.T) {
	definition := map[string]any{
		"id":   "benchmark-packaging-l0",
		"name": "Benchmark Packaging L0",
		"nodes": []map[string]any{
			{
				"id":   "child",
				"type": "child_workflow",
				"data": map[string]any{
					"type": "child_workflow",
					"config": map[string]any{
						"childWorkflowId": "reasoning-judge-pick",
					},
				},
			},
			{
				"id":   "packaging",
				"type": "aggregation",
				"data": map[string]any{
					"type": "aggregation",
					"config": map[string]any{
						"aggregationMethod":        "collect",
						"aggregationWorkflowId":    "aggregation-collect",
						"benchmarkOutputPackaging": true,
					},
				},
			},
		},
		"edges": []map[string]any{},
	}
	payload, err := json.Marshal(definition)
	if err != nil {
		t.Fatalf("marshal workflow definition: %v", err)
	}

	stats := parseWorkflowDefinitionStats(&storage.WorkflowDefinition{
		ID:         "benchmark-packaging-l0",
		Name:       "Benchmark Packaging L0",
		Definition: string(payload),
	})

	if stats.ReferencesL0DirectlyAsBenchmark {
		t.Fatalf("benchmark output packaging aggregation should not warn: %+v", stats)
	}
	if !reflect.DeepEqual(stats.AggregationSourceIDs, []string{"aggregation-collect"}) {
		t.Fatalf("AggregationSourceIDs = %v", stats.AggregationSourceIDs)
	}
}

func TestParseWorkflowDefinitionStats_ExtractsBackendWorkflowReferences(t *testing.T) {
	definition := map[string]any{
		"id":   "composite-backend-shape",
		"name": "Composite Backend Shape",
		"nodes": []map[string]any{
			{
				"id":              "phase-a",
				"type":            "workflow_ref",
				"workflow_ref_id": "reasoning-judge-pick",
			},
			{
				"id":                "child-a",
				"type":              "child_workflow",
				"child_workflow_id": "reasoning-informed-captain-synthesis",
			},
		},
	}
	payload, err := json.Marshal(definition)
	if err != nil {
		t.Fatalf("marshal workflow definition: %v", err)
	}

	stats := parseWorkflowDefinitionStats(&storage.WorkflowDefinition{
		ID:         "composite-backend-shape",
		Name:       "Composite Backend Shape",
		Definition: string(payload),
	})

	if !reflect.DeepEqual(stats.WorkflowRefIDs, []string{"reasoning-judge-pick"}) {
		t.Fatalf("WorkflowRefIDs = %v", stats.WorkflowRefIDs)
	}
	if !reflect.DeepEqual(stats.ChildWorkflowIDs, []string{"reasoning-informed-captain-synthesis"}) {
		t.Fatalf("ChildWorkflowIDs = %v", stats.ChildWorkflowIDs)
	}
}

func TestParseWorkflowDefinitionStats_DoesNotFlagBenchmarkChildL1Reference(t *testing.T) {
	definition := map[string]any{
		"id":   "benchmark-child-l1",
		"name": "Benchmark Child L1",
		"nodes": []map[string]any{
			{
				"id":   "child",
				"type": "child_workflow",
				"data": map[string]any{
					"type": "child_workflow",
					"config": map[string]any{
						"childWorkflowId": "reasoning-judge-pick",
					},
				},
			},
		},
		"edges": []map[string]any{},
	}
	payload, err := json.Marshal(definition)
	if err != nil {
		t.Fatalf("marshal workflow definition: %v", err)
	}

	stats := parseWorkflowDefinitionStats(&storage.WorkflowDefinition{
		ID:         "benchmark-child-l1",
		Name:       "Benchmark Child L1",
		Definition: string(payload),
	})

	if stats.ReferencesL0DirectlyAsBenchmark {
		t.Fatalf("benchmark child_workflow to L1 should not warn: %+v", stats)
	}
	if !reflect.DeepEqual(stats.ChildWorkflowIDs, []string{"reasoning-judge-pick"}) {
		t.Fatalf("ChildWorkflowIDs = %v", stats.ChildWorkflowIDs)
	}
}

func TestHandleWorkflowsReturnsNonNullCollections(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	wf := &storage.WorkflowDefinition{
		ID:         "wf-non-null-collections",
		Name:       "Non Null",
		Definition: "not-json",
	}
	if err := server.storage.CreateWorkflow(wf); err != nil {
		t.Fatalf("create workflow: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/workflows", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	rawWorkflows, ok := response["Workflows"].([]any)
	if !ok {
		t.Fatalf("expected Workflows array, got %T", response["Workflows"])
	}

	var found map[string]any
	for _, item := range rawWorkflows {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if row["id"] == wf.ID {
			found = row
			break
		}
	}
	if found == nil {
		t.Fatalf("workflow %q not found in response", wf.ID)
	}

	if _, ok := found["ModelsUsed"].([]any); !ok {
		t.Fatalf("expected ModelsUsed array, got %T (%v)", found["ModelsUsed"], found["ModelsUsed"])
	}
	if _, ok := found["NodeTypes"].(map[string]any); !ok {
		t.Fatalf("expected NodeTypes object, got %T (%v)", found["NodeTypes"], found["NodeTypes"])
	}
	if _, ok := found["AggregationSourceIDs"].([]any); !ok {
		t.Fatalf("expected AggregationSourceIDs array, got %T (%v)", found["AggregationSourceIDs"], found["AggregationSourceIDs"])
	}
	if _, ok := found["WorkflowRefIDs"].([]any); !ok {
		t.Fatalf("expected WorkflowRefIDs array, got %T (%v)", found["WorkflowRefIDs"], found["WorkflowRefIDs"])
	}
	if _, ok := found["ChildWorkflowIDs"].([]any); !ok {
		t.Fatalf("expected ChildWorkflowIDs array, got %T (%v)", found["ChildWorkflowIDs"], found["ChildWorkflowIDs"])
	}
}
