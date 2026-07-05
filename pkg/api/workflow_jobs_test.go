package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	workflowruntime "github.com/alhasaniq/consortium/pkg/workflow/runtime"
	"github.com/gorilla/mux"
)

// ---------------------------------------------------------------------------
// mapWorkflowNodesToResponse
// ---------------------------------------------------------------------------

func TestMapWorkflowNodesToResponse_EmptyInput(t *testing.T) {
	result := mapWorkflowNodesToResponse(nil, nodeResponseOpts{})
	if result == nil {
		t.Fatal("expected non-nil slice for nil input")
	}
	if len(result) != 0 {
		t.Fatalf("expected empty slice, got %d elements", len(result))
	}
}

func TestMapWorkflowNodesToResponse_CoreFieldsAlwaysPresent(t *testing.T) {
	now := time.Now()
	nodes := []storage.WorkflowNode{
		{
			NodeID:        "n1",
			NodeLabel:     "My Label",
			NodeName:      "my-name",
			NodeType:      "prompt",
			NodeOrder:     2,
			Status:        "completed",
			Output:        "hello output",
			TokensInput:   10,
			TokensOutput:  20,
			Cost:          0.0015,
			LatencyMs:     123.4,
			Model:         "gpt-4o",
			ErrorMessage:  "",
			Prompt:        "the prompt text",
			Metadata:      `{"key":"val"}`,
			ExecutionUID:  "exec-uid-001",
			AttemptNumber: 1,
			StartedAt:     &now,
			CompletedAt:   &now,
		},
	}

	result := mapWorkflowNodesToResponse(nodes, nodeResponseOpts{})

	if len(result) != 1 {
		t.Fatalf("expected 1 element, got %d", len(result))
	}
	m := result[0]

	coreFields := []string{
		"node_id", "node_label", "node_name", "node_type",
		"node_order", "status", "output", "tokens_input",
		"tokens_output", "cost", "latency_ms", "model", "error_message",
	}
	for _, f := range coreFields {
		if _, ok := m[f]; !ok {
			t.Errorf("core field %q missing from response map", f)
		}
	}

	if m["node_id"] != "n1" {
		t.Errorf("node_id = %v, want n1", m["node_id"])
	}
	if m["status"] != "completed" {
		t.Errorf("status = %v, want completed", m["status"])
	}
	if m["model"] != "gpt-4o" {
		t.Errorf("model = %v, want gpt-4o", m["model"])
	}
}

func TestMapWorkflowNodesToResponse_OptionalFields(t *testing.T) {
	now := time.Now()
	nodes := []storage.WorkflowNode{
		{
			NodeID:        "n1",
			Prompt:        "some prompt",
			Metadata:      `{"x":1}`,
			ExecutionUID:  "exec-001",
			AttemptNumber: 3,
			StartedAt:     &now,
			CompletedAt:   &now,
		},
	}

	tests := []struct {
		name         string
		opts         nodeResponseOpts
		wantPrompt   bool
		wantMetadata bool
		wantTiming   bool
		wantExecInfo bool
	}{
		{
			name:         "no optional fields",
			opts:         nodeResponseOpts{},
			wantPrompt:   false,
			wantMetadata: false,
			wantTiming:   false,
			wantExecInfo: false,
		},
		{
			name:         "only prompt",
			opts:         nodeResponseOpts{IncludePrompt: true},
			wantPrompt:   true,
			wantMetadata: false,
			wantTiming:   false,
			wantExecInfo: false,
		},
		{
			name:         "only metadata",
			opts:         nodeResponseOpts{IncludeMetadata: true},
			wantPrompt:   false,
			wantMetadata: true,
			wantTiming:   false,
			wantExecInfo: false,
		},
		{
			name:         "only timing",
			opts:         nodeResponseOpts{IncludeTiming: true},
			wantPrompt:   false,
			wantMetadata: false,
			wantTiming:   true,
			wantExecInfo: false,
		},
		{
			name:         "only exec info",
			opts:         nodeResponseOpts{IncludeExecInfo: true},
			wantPrompt:   false,
			wantMetadata: false,
			wantTiming:   false,
			wantExecInfo: true,
		},
		{
			name:         "all optional fields",
			opts:         nodeResponseOpts{IncludePrompt: true, IncludeMetadata: true, IncludeTiming: true, IncludeExecInfo: true},
			wantPrompt:   true,
			wantMetadata: true,
			wantTiming:   true,
			wantExecInfo: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapWorkflowNodesToResponse(nodes, tt.opts)
			if len(result) != 1 {
				t.Fatalf("expected 1 element, got %d", len(result))
			}
			m := result[0]

			_, hasPrompt := m["prompt"]
			_, hasMetadata := m["metadata"]
			_, hasStartedAt := m["started_at"]
			_, hasCompletedAt := m["completed_at"]
			_, hasExecUID := m["execution_uid"]
			_, hasAttempt := m["attempt_number"]

			if hasPrompt != tt.wantPrompt {
				t.Errorf("prompt present=%v, want %v", hasPrompt, tt.wantPrompt)
			}
			if hasMetadata != tt.wantMetadata {
				t.Errorf("metadata present=%v, want %v", hasMetadata, tt.wantMetadata)
			}
			if hasStartedAt != tt.wantTiming || hasCompletedAt != tt.wantTiming {
				t.Errorf("timing fields present=%v/%v, want %v", hasStartedAt, hasCompletedAt, tt.wantTiming)
			}
			if hasExecUID != tt.wantExecInfo || hasAttempt != tt.wantExecInfo {
				t.Errorf("exec info fields present=%v/%v, want %v", hasExecUID, hasAttempt, tt.wantExecInfo)
			}
		})
	}
}

func TestMapWorkflowNodesToResponse_ParentNodeID(t *testing.T) {
	t.Run("parent_node_id included when non-empty", func(t *testing.T) {
		nodes := []storage.WorkflowNode{
			{NodeID: "child", ParentNodeID: "parent-x"},
		}
		result := mapWorkflowNodesToResponse(nodes, nodeResponseOpts{})
		if len(result) == 0 {
			t.Fatal("expected 1 result")
		}
		if v, ok := result[0]["parent_node_id"]; !ok || v != "parent-x" {
			t.Errorf("parent_node_id = %v, want parent-x", result[0]["parent_node_id"])
		}
	})

	t.Run("parent_node_id omitted when empty", func(t *testing.T) {
		nodes := []storage.WorkflowNode{
			{NodeID: "orphan", ParentNodeID: ""},
		}
		result := mapWorkflowNodesToResponse(nodes, nodeResponseOpts{})
		if len(result) == 0 {
			t.Fatal("expected 1 result")
		}
		if _, ok := result[0]["parent_node_id"]; ok {
			t.Error("parent_node_id should be omitted when empty")
		}
	})
}

func TestMapWorkflowNodesToResponse_PreservesOrder(t *testing.T) {
	nodes := []storage.WorkflowNode{
		{NodeID: "first"},
		{NodeID: "second"},
		{NodeID: "third"},
	}
	result := mapWorkflowNodesToResponse(nodes, nodeResponseOpts{})
	if len(result) != 3 {
		t.Fatalf("expected 3, got %d", len(result))
	}
	for i, want := range []string{"first", "second", "third"} {
		if result[i]["node_id"] != want {
			t.Errorf("result[%d].node_id = %v, want %s", i, result[i]["node_id"], want)
		}
	}
}

// ---------------------------------------------------------------------------
// handleGetJob
// ---------------------------------------------------------------------------

func TestHandleGetJob_NotFound(t *testing.T) {
	api, _ := setupWorkflowAPI(t)
	router := mux.NewRouter()
	router.HandleFunc("/api/jobs/{id}", api.handleGetJob).Methods("GET")

	w := serveHTTP(t, router, "GET", "/api/jobs/nonexistent-job-id", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleGetJob_ReturnsJobFields(t *testing.T) {
	api, db := setupWorkflowAPI(t)
	router := mux.NewRouter()
	router.HandleFunc("/api/jobs/{id}", api.handleGetJob).Methods("GET")

	job := createTestJob(t, db, "completed", func(j *storage.Job) {
		j.Description = "my job description"
		j.WorkflowID = "wf-abc"
		j.TokensInput = 100
		j.TokensOutput = 200
		j.TokensTotal = 300
		j.Cost = 0.0045
	})

	w := serveHTTP(t, router, "GET", "/api/jobs/"+job.ID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := decodeJSON(t, w)

	checkField := func(key string, wantVal interface{}) {
		t.Helper()
		val, ok := resp[key]
		if !ok {
			t.Errorf("field %q missing from response", key)
			return
		}
		switch want := wantVal.(type) {
		case string:
			if got, ok := val.(string); !ok || got != want {
				t.Errorf("%s = %v, want %v", key, val, want)
			}
		case float64:
			if got, ok := val.(float64); !ok || got != want {
				t.Errorf("%s = %v, want %v", key, val, want)
			}
		}
	}

	checkField("id", job.ID)
	checkField("status", "completed")
	checkField("description", "my job description")
	checkField("workflow_id", "wf-abc")

	if resp["nodes"] == nil {
		t.Error("nodes field missing from response")
	}
}

func TestHandleGetJob_DurableMetadataConditional(t *testing.T) {
	api, db := setupWorkflowAPI(t)
	router := mux.NewRouter()
	router.HandleFunc("/api/jobs/{id}", api.handleGetJob).Methods("GET")

	t.Run("dag_hash and workflow_execution_id absent when empty", func(t *testing.T) {
		// CreateExecution always stores run_number=1 (defaulted from 0),
		// so run_number=1 will be present. But dag_hash and workflow_execution_id
		// are only included when non-empty.
		job := createTestJob(t, db, "completed")

		w := serveHTTP(t, router, "GET", "/api/jobs/"+job.ID, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
		resp := decodeJSON(t, w)

		if _, ok := resp["dag_hash"]; ok {
			t.Error("dag_hash should be absent when empty")
		}
		if _, ok := resp["workflow_execution_id"]; ok {
			t.Error("workflow_execution_id should be absent when empty")
		}
	})

	t.Run("durable fields present when set", func(t *testing.T) {
		job := createTestJob(t, db, "running", func(j *storage.Job) {
			j.RunNumber = 3
			j.DAGHash = "deadbeef123"
			j.DAGSnapshot = `{"nodes":[],"edges":[]}`
			j.WorkflowExecutionID = "exec-durable-001"
			j.RunID = "run-durable-001"
		})

		w := serveHTTP(t, router, "GET", "/api/jobs/"+job.ID, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
		}
		resp := decodeJSON(t, w)

		if got, ok := resp["run_number"].(float64); !ok || int(got) != 3 {
			t.Errorf("run_number = %v, want 3", resp["run_number"])
		}
		if got, ok := resp["dag_hash"].(string); !ok || got != "deadbeef123" {
			t.Errorf("dag_hash = %v, want deadbeef123", resp["dag_hash"])
		}
		if got, ok := resp["workflow_execution_id"].(string); !ok || got != "exec-durable-001" {
			t.Errorf("workflow_execution_id = %v, want exec-durable-001", resp["workflow_execution_id"])
		}
	})
}

func TestHandleGetJob_IncludesNodesWithAllOptionalFields(t *testing.T) {
	api, db := setupWorkflowAPI(t)
	router := mux.NewRouter()
	router.HandleFunc("/api/jobs/{id}", api.handleGetJob).Methods("GET")

	job := createTestJob(t, db, "completed")

	// Add a workflow node directly
	now := time.Now()
	node := &storage.WorkflowNode{
		ExecutionID:   job.ID,
		NodeID:        "node-test",
		NodeType:      "prompt",
		Status:        "completed",
		NodeLabel:     "Test Node",
		NodeName:      "test-node",
		Output:        "node output",
		Prompt:        "node prompt",
		Metadata:      `{"info":"test"}`,
		ExecutionUID:  job.ID + ":node-test:1",
		AttemptNumber: 1,
		StartedAt:     &now,
		CompletedAt:   &now,
	}
	if err := db.AddWorkflowNode(node); err != nil {
		t.Fatalf("AddWorkflowNode: %v", err)
	}

	w := serveHTTP(t, router, "GET", "/api/jobs/"+job.ID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := decodeJSON(t, w)
	nodesList, ok := resp["nodes"].([]interface{})
	if !ok || len(nodesList) == 0 {
		t.Fatalf("expected at least 1 node, got %v", resp["nodes"])
	}

	n := nodesList[0].(map[string]interface{})

	// handleGetJob passes all optional opts as true
	for _, field := range []string{"prompt", "metadata", "started_at", "completed_at", "execution_uid", "attempt_number"} {
		if _, ok := n[field]; !ok {
			t.Errorf("expected field %q to be present in node response", field)
		}
	}
}

// ---------------------------------------------------------------------------
// handleGetJobWorkflow
// ---------------------------------------------------------------------------

func TestHandleGetJobWorkflow(t *testing.T) {
	api, db := setupWorkflowAPI(t)
	router := mux.NewRouter()
	router.HandleFunc("/api/jobs/{id}/workflow", api.handleGetJobWorkflow).Methods("GET")

	wf := workflow.Workflow{
		ID:   "manual-agent-live-novomo",
		Name: "Manual Agent Live Novomo",
		Nodes: []*workflow.Node{
			{
				ID:             "agent1",
				Type:           workflow.NodeTypeAgentRun,
				Prompt:         "Return exactly LIVE_OK.",
				Harness:        "claude-code",
				TimeoutSeconds: 30,
			},
			{
				ID:                "result1",
				Type:              workflow.NodeTypeResult,
				OutputName:        "final",
				AggregationMethod: workflow.AggMethodCollect,
				Metadata:          map[string]interface{}{"input_ids": []interface{}{"agent1"}},
			},
		},
		Edges: []*workflow.Edge{{ID: "e1", Source: "agent1", Target: "result1"}},
	}
	requestData, _ := json.Marshal(wf)
	job := createTestJob(t, db, "completed", func(j *storage.Job) {
		j.WorkflowID = wf.ID
		j.RequestData = string(requestData)
	})

	t.Run("returns ad-hoc job workflow snapshot", func(t *testing.T) {
		w := serveHTTP(t, router, "GET", "/api/jobs/"+job.ID+"/workflow", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
		}

		resp := decodeJSON(t, w)
		if resp["job_id"] != job.ID {
			t.Fatalf("job_id = %v, want %s", resp["job_id"], job.ID)
		}
		if resp["workflow_id"] != wf.ID {
			t.Fatalf("workflow_id = %v, want %s", resp["workflow_id"], wf.ID)
		}
		if resp["saved_workflow_exists"] != false {
			t.Fatalf("saved_workflow_exists = %v, want false", resp["saved_workflow_exists"])
		}
		workflowObj, ok := resp["workflow"].(map[string]interface{})
		if !ok {
			t.Fatalf("workflow missing or wrong type: %#v", resp["workflow"])
		}
		if workflowObj["id"] != wf.ID || workflowObj["name"] != wf.Name {
			t.Fatalf("workflow identity = (%v, %v), want (%s, %s)", workflowObj["id"], workflowObj["name"], wf.ID, wf.Name)
		}
	})

	t.Run("reports when workflow is already saved", func(t *testing.T) {
		if err := db.CreateWorkflow(&storage.WorkflowDefinition{
			ID:         wf.ID,
			Name:       wf.Name,
			Definition: string(requestData),
		}); err != nil {
			t.Fatalf("create workflow: %v", err)
		}

		w := serveHTTP(t, router, "GET", "/api/jobs/"+job.ID+"/workflow", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
		}

		resp := decodeJSON(t, w)
		if resp["saved_workflow_exists"] != true {
			t.Fatalf("saved_workflow_exists = %v, want true", resp["saved_workflow_exists"])
		}
	})

	t.Run("falls back to frozen dag snapshot", func(t *testing.T) {
		frozen, err := workflowruntime.FreezeWorkflow(
			"dag-only-workflow",
			"DAG Only Workflow",
			"",
			[]workflowruntime.NodeForFreeze{
				{
					ID:             "agent-from-dag",
					Type:           string(workflow.NodeTypeAgentRun),
					Prompt:         "Return exactly DAG_OK.",
					Harness:        "claude-code",
					TimeoutSeconds: 30,
				},
			},
			nil,
			nil,
			nil,
		)
		if err != nil {
			t.Fatalf("freeze workflow: %v", err)
		}

		dagJob := createTestJob(t, db, "completed", func(j *storage.Job) {
			j.WorkflowID = "dag-only-workflow"
			j.WorkflowExecutionID = "dag-only-workflow-exec"
			j.RunID = "dag-only-workflow-run"
			j.RequestData = ""
			j.DAGSnapshot = string(frozen.Definition)
			j.DAGHash = frozen.DAGHash
		})

		w := serveHTTP(t, router, "GET", "/api/jobs/"+dagJob.ID+"/workflow", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
		}

		resp := decodeJSON(t, w)
		if resp["source"] != "dag_snapshot" {
			t.Fatalf("source = %v, want dag_snapshot", resp["source"])
		}
		workflowObj, ok := resp["workflow"].(map[string]interface{})
		if !ok {
			t.Fatalf("workflow missing or wrong type: %#v", resp["workflow"])
		}
		nodes, ok := workflowObj["nodes"].([]interface{})
		if !ok || len(nodes) != 1 {
			t.Fatalf("workflow nodes = %#v, want one node", workflowObj["nodes"])
		}
		node := nodes[0].(map[string]interface{})
		if node["id"] != "agent-from-dag" {
			t.Fatalf("node id = %v, want agent-from-dag", node["id"])
		}
	})

	t.Run("not found when job has no workflow snapshot", func(t *testing.T) {
		emptyJob := createTestJob(t, db, "completed")
		w := serveHTTP(t, router, "GET", "/api/jobs/"+emptyJob.ID+"/workflow", nil)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d. Body: %s", w.Code, http.StatusNotFound, w.Body.String())
		}
	})
}

// ---------------------------------------------------------------------------
// handleGetJobTrace
// ---------------------------------------------------------------------------

func TestHandleGetJobTrace_NotFound(t *testing.T) {
	api, _ := setupWorkflowAPI(t)
	router := mux.NewRouter()
	router.HandleFunc("/api/jobs/{id}/trace", api.handleGetJobTrace).Methods("GET")

	w := serveHTTP(t, router, "GET", "/api/jobs/nonexistent/trace", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleGetJobTrace_ReturnsStructure(t *testing.T) {
	api, db := setupWorkflowAPI(t)
	router := mux.NewRouter()
	router.HandleFunc("/api/jobs/{id}/trace", api.handleGetJobTrace).Methods("GET")

	job := createTestJob(t, db, "completed")

	w := serveHTTP(t, router, "GET", "/api/jobs/"+job.ID+"/trace", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := decodeJSON(t, w)

	if gotID, ok := resp["job_id"].(string); !ok || gotID != job.ID {
		t.Errorf("job_id = %v, want %s", resp["job_id"], job.ID)
	}
	if _, ok := resp["node_groups"]; !ok {
		t.Error("node_groups field missing from trace response")
	}
}

func TestHandleGetJobTrace_ContentType(t *testing.T) {
	api, db := setupWorkflowAPI(t)
	router := mux.NewRouter()
	router.HandleFunc("/api/jobs/{id}/trace", api.handleGetJobTrace).Methods("GET")

	job := createTestJob(t, db, "completed")

	w := serveHTTP(t, router, "GET", "/api/jobs/"+job.ID+"/trace", nil)
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

// ---------------------------------------------------------------------------
// handleListJobs
// ---------------------------------------------------------------------------

func TestHandleListJobs_Empty(t *testing.T) {
	api, _ := setupWorkflowAPI(t)
	router := mux.NewRouter()
	router.HandleFunc("/api/jobs", api.handleListJobs).Methods("GET")

	w := serveHTTP(t, router, "GET", "/api/jobs", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := decodeJSON(t, w)

	jobs, ok := resp["jobs"].([]interface{})
	if !ok {
		t.Fatalf("jobs field is not an array: %T", resp["jobs"])
	}
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs in empty DB, got %d", len(jobs))
	}

	pagination, ok := resp["pagination"].(map[string]interface{})
	if !ok {
		t.Fatal("pagination field missing from response")
	}
	if pagination["has_more"] != false {
		t.Errorf("has_more = %v, want false for empty DB", pagination["has_more"])
	}
}

func TestHandleListJobs_ReturnsJobs(t *testing.T) {
	api, db := setupWorkflowAPI(t)
	router := mux.NewRouter()
	router.HandleFunc("/api/jobs", api.handleListJobs).Methods("GET")

	for i := 0; i < 3; i++ {
		createTestJob(t, db, "completed")
	}

	w := serveHTTP(t, router, "GET", "/api/jobs", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	resp := decodeJSON(t, w)
	jobs, ok := resp["jobs"].([]interface{})
	if !ok {
		t.Fatalf("jobs field is not an array")
	}
	if len(jobs) < 3 {
		t.Errorf("expected at least 3 jobs, got %d", len(jobs))
	}
}

func TestHandleListJobs_JobFieldsPresent(t *testing.T) {
	api, db := setupWorkflowAPI(t)
	router := mux.NewRouter()
	router.HandleFunc("/api/jobs", api.handleListJobs).Methods("GET")

	createTestJob(t, db, "running", func(j *storage.Job) {
		j.WorkflowID = "wf-field-test"
		j.Description = "field test job"
	})

	w := serveHTTP(t, router, "GET", "/api/jobs", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	resp := decodeJSON(t, w)
	jobs, _ := resp["jobs"].([]interface{})
	if len(jobs) == 0 {
		t.Fatal("expected at least one job")
	}

	j := jobs[0].(map[string]interface{})
	requiredFields := []string{
		"id", "workflow_id", "description", "status",
		"tokens_total", "cost", "created_at", "node_count",
		"aggregation_method", "result_text", "prompt",
		"config_hash", "run_number", "dag_hash", "workflow_execution_id",
	}
	for _, f := range requiredFields {
		if _, ok := j[f]; !ok {
			t.Errorf("field %q missing from job list entry", f)
		}
	}
}

func TestHandleListJobs_LimitParameter(t *testing.T) {
	api, db := setupWorkflowAPI(t)
	router := mux.NewRouter()
	router.HandleFunc("/api/jobs", api.handleListJobs).Methods("GET")

	for i := 0; i < 5; i++ {
		createTestJob(t, db, "completed")
	}

	tests := []struct {
		limitParam string
		maxExpect  int
	}{
		{"2", 2},
		{"3", 3},
		{"1", 1},
	}

	for _, tt := range tests {
		t.Run("limit="+tt.limitParam, func(t *testing.T) {
			w := serveHTTP(t, router, "GET", "/api/jobs?limit="+tt.limitParam, nil)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
			}
			resp := decodeJSON(t, w)
			jobs, _ := resp["jobs"].([]interface{})
			if len(jobs) > tt.maxExpect {
				t.Errorf("got %d jobs with limit=%s, expected at most %d", len(jobs), tt.limitParam, tt.maxExpect)
			}
		})
	}
}

func TestHandleListJobs_LimitCappedAt100(t *testing.T) {
	api, db := setupWorkflowAPI(t)
	router := mux.NewRouter()
	router.HandleFunc("/api/jobs", api.handleListJobs).Methods("GET")

	// Create 5 jobs to have something to test with
	for i := 0; i < 5; i++ {
		createTestJob(t, db, "completed")
	}

	// Requesting more than 100 should not error and should still return data
	w := serveHTTP(t, router, "GET", "/api/jobs?limit=500", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	resp := decodeJSON(t, w)
	if _, ok := resp["jobs"].([]interface{}); !ok {
		t.Fatal("jobs field missing from response")
	}
}

func TestHandleListJobs_InvalidLimitFallsBackToDefault(t *testing.T) {
	api, db := setupWorkflowAPI(t)
	router := mux.NewRouter()
	router.HandleFunc("/api/jobs", api.handleListJobs).Methods("GET")

	createTestJob(t, db, "completed")

	w := serveHTTP(t, router, "GET", "/api/jobs?limit=notanumber", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	// Should succeed with default limit of 20
	resp := decodeJSON(t, w)
	if _, ok := resp["jobs"].([]interface{}); !ok {
		t.Fatal("jobs field missing from response with invalid limit")
	}
}

func TestHandleListJobs_StatusFilter(t *testing.T) {
	api, db := setupWorkflowAPI(t)
	router := mux.NewRouter()
	router.HandleFunc("/api/jobs", api.handleListJobs).Methods("GET")

	createTestJob(t, db, "completed")
	createTestJob(t, db, "completed")
	createTestJob(t, db, "pending")

	w := serveHTTP(t, router, "GET", "/api/jobs?status=completed", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. Body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := decodeJSON(t, w)
	jobs, _ := resp["jobs"].([]interface{})
	for _, jRaw := range jobs {
		j := jRaw.(map[string]interface{})
		if j["status"] != "completed" {
			t.Errorf("got job with status=%v, want completed", j["status"])
		}
	}
}

func TestHandleListJobs_WorkflowIDFilter(t *testing.T) {
	api, db := setupWorkflowAPI(t)
	router := mux.NewRouter()
	router.HandleFunc("/api/jobs", api.handleListJobs).Methods("GET")

	createTestJob(t, db, "completed", func(j *storage.Job) { j.WorkflowID = "wf-target" })
	createTestJob(t, db, "completed", func(j *storage.Job) { j.WorkflowID = "wf-target" })
	createTestJob(t, db, "completed", func(j *storage.Job) { j.WorkflowID = "wf-other" })

	w := serveHTTP(t, router, "GET", "/api/jobs?workflow_id=wf-target", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	resp := decodeJSON(t, w)
	jobs, _ := resp["jobs"].([]interface{})
	if len(jobs) != 2 {
		t.Errorf("expected 2 jobs with wf-target, got %d", len(jobs))
	}
	for _, jRaw := range jobs {
		j := jRaw.(map[string]interface{})
		if j["workflow_id"] != "wf-target" {
			t.Errorf("got job with workflow_id=%v, want wf-target", j["workflow_id"])
		}
	}
}

func TestHandleListJobs_ExtractsPromptFromRequestData(t *testing.T) {
	api, db := setupWorkflowAPI(t)
	router := mux.NewRouter()
	router.HandleFunc("/api/jobs", api.handleListJobs).Methods("GET")

	tests := []struct {
		name           string
		requestDataKey string
		promptValue    string
	}{
		{
			name:           "user_prompt key",
			requestDataKey: "user_prompt",
			promptValue:    "What is the meaning of life?",
		},
		{
			name:           "prompt key",
			requestDataKey: "prompt",
			promptValue:    "Explain quantum computing",
		},
		{
			name:           "query key",
			requestDataKey: "query",
			promptValue:    "Search for relevant documents",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestData, _ := json.Marshal(map[string]interface{}{
				"nodes":   []interface{}{},
				"context": map[string]interface{}{tt.requestDataKey: tt.promptValue},
			})
			job := createTestJob(t, db, "completed", func(j *storage.Job) {
				j.RequestData = string(requestData)
			})

			w := serveHTTP(t, router, "GET", "/api/jobs?limit=100", nil)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
			}

			resp := decodeJSON(t, w)
			jobs, _ := resp["jobs"].([]interface{})

			var found map[string]interface{}
			for _, jRaw := range jobs {
				j := jRaw.(map[string]interface{})
				if j["id"] == job.ID {
					found = j
					break
				}
			}
			if found == nil {
				t.Fatalf("job %s not found in listing", job.ID)
			}
			if found["prompt"] != tt.promptValue {
				t.Errorf("prompt = %v, want %q", found["prompt"], tt.promptValue)
			}
		})
	}
}

func TestHandleListJobs_ExtractsPromptFromWorkflowNodes(t *testing.T) {
	api, db := setupWorkflowAPI(t)
	router := mux.NewRouter()
	router.HandleFunc("/api/jobs", api.handleListJobs).Methods("GET")

	requestData, _ := json.Marshal(map[string]interface{}{
		"id":          "990edebb-5e92-4484-a2d6-4f63614177b8",
		"name":        "finding neemo - Novomo handoff chain",
		"description": "Three-step Novomo Agent workflow",
		"nodes": []interface{}{
			map[string]interface{}{
				"id":     "repo",
				"type":   "agent_run",
				"prompt": "Create a small static web app for a fish tank scene.",
			},
		},
		"edges": []interface{}{},
	})
	job := createTestJob(t, db, "completed", func(j *storage.Job) {
		j.RequestData = string(requestData)
	})

	w := serveHTTP(t, router, "GET", "/api/jobs?limit=100", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	resp := decodeJSON(t, w)
	jobs, _ := resp["jobs"].([]interface{})

	var found map[string]interface{}
	for _, jRaw := range jobs {
		j := jRaw.(map[string]interface{})
		if j["id"] == job.ID {
			found = j
			break
		}
	}
	if found == nil {
		t.Fatalf("job %s not found in listing", job.ID)
	}
	if found["prompt"] != "Create a small static web app for a fish tank scene." {
		t.Errorf("prompt = %v, want node prompt", found["prompt"])
	}
}

func TestHandleListJobs_ExtractsAggregationMethod(t *testing.T) {
	api, db := setupWorkflowAPI(t)
	router := mux.NewRouter()
	router.HandleFunc("/api/jobs", api.handleListJobs).Methods("GET")

	// CreateExecution does not store response_data; we must update it separately.
	job := createTestJob(t, db, "completed")

	responseData, _ := json.Marshal(map[string]interface{}{
		"aggregation_method": "majority_vote",
		"result":             "the answer",
	})
	job.ResponseData = string(responseData)
	if err := db.UpdateExecution(job); err != nil {
		t.Fatalf("UpdateExecution: %v", err)
	}

	w := serveHTTP(t, router, "GET", "/api/jobs?limit=100", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	resp := decodeJSON(t, w)
	jobs, _ := resp["jobs"].([]interface{})

	var found map[string]interface{}
	for _, jRaw := range jobs {
		j := jRaw.(map[string]interface{})
		if j["id"] == job.ID {
			found = j
			break
		}
	}
	if found == nil {
		t.Fatalf("job %s not found in listing", job.ID)
	}
	if found["aggregation_method"] != "majority_vote" {
		t.Errorf("aggregation_method = %v, want majority_vote", found["aggregation_method"])
	}
}

func TestHandleListJobs_PaginationCursor(t *testing.T) {
	api, db := setupWorkflowAPI(t)
	router := mux.NewRouter()
	router.HandleFunc("/api/jobs", api.handleListJobs).Methods("GET")

	for i := 0; i < 5; i++ {
		createTestJob(t, db, "completed")
	}

	// First page: limit 2
	w := serveHTTP(t, router, "GET", "/api/jobs?limit=2", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	resp := decodeJSON(t, w)
	pagination, ok := resp["pagination"].(map[string]interface{})
	if !ok {
		t.Fatal("pagination missing")
	}

	// With 5 jobs and limit 2, there should be more pages
	if pagination["has_more"] != true {
		t.Errorf("has_more = %v, want true when more jobs exist", pagination["has_more"])
	}

	cursor, ok := pagination["cursor"].(string)
	if !ok || cursor == "" {
		t.Fatal("cursor should be non-empty when has_more=true")
	}

	// Second page using cursor
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/jobs?limit=2&cursor="+cursor, nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("second page status = %d, want %d", rec.Code, http.StatusOK)
	}

	resp2 := decodeJSON(t, rec)
	jobs2, _ := resp2["jobs"].([]interface{})
	if len(jobs2) == 0 {
		t.Error("expected jobs on second page")
	}
}

// ---------------------------------------------------------------------------
// handleResumeJob
// ---------------------------------------------------------------------------

func TestHandleResumeJob(t *testing.T) {
	api, db := setupWorkflowAPI(t)

	router := mux.NewRouter()
	router.HandleFunc("/api/jobs/{id}/resume", api.handleResumeJob).Methods("POST")

	tests := []struct {
		name           string
		setupJob       func() string
		expectedStatus int
		checkAfter     func(t *testing.T, jobID string)
	}{
		{
			name:           "resume non-existent job",
			setupJob:       func() string { return "nonexistent-job" },
			expectedStatus: http.StatusNotFound,
		},
		{
			name: "resume non-paused job",
			setupJob: func() string {
				return createTestJob(t, db, "completed").ID
			},
			expectedStatus: http.StatusConflict,
		},
		{
			name: "successful resume",
			setupJob: func() string {
				return createTestJob(t, db, "paused").ID
			},
			expectedStatus: http.StatusOK,
			checkAfter: func(t *testing.T, jobID string) {
				updated, err := db.GetExecution(jobID)
				if err != nil {
					t.Fatalf("Failed to reload job: %v", err)
				}
				if updated.Status != "pending" {
					t.Fatalf("Expected pending status, got %s", updated.Status)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobID := tt.setupJob()
			w := serveHTTP(t, router, "POST", "/api/jobs/"+jobID+"/resume", nil)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d. Body: %s", tt.expectedStatus, w.Code, w.Body.String())
			}
			if tt.checkAfter != nil {
				tt.checkAfter(t, jobID)
			}
		})
	}

	t.Run("missing job ID", func(t *testing.T) {
		w := httptest.NewRecorder()
		api.handleResumeJob(w, httptest.NewRequest("POST", "/api/jobs//resume", nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
		}
	})
}

func TestHandleListJobs_ContentType(t *testing.T) {
	api, _ := setupWorkflowAPI(t)
	router := mux.NewRouter()
	router.HandleFunc("/api/jobs", api.handleListJobs).Methods("GET")

	w := serveHTTP(t, router, "GET", "/api/jobs", nil)
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}
