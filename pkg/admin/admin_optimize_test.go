package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/optimize"
	"github.com/alhasaniq/consortium/pkg/storage"
)

// validTestSpec returns a minimal valid OptimizeSpec for test use.
func validTestSpec() *optimize.OptimizeSpec {
	return &optimize.OptimizeSpec{
		Params: []optimize.ParamDeclaration{
			{
				Path:       "nodes[agent-a].data.config.model",
				Type:       optimize.ParamTypeModel,
				Candidates: []string{"model-a", "model-b"},
			},
		},
		Objectives: []optimize.Objective{
			{Metric: "accuracy", Direction: "maximize", Weight: 1.0},
		},
		StopPolicy: optimize.StopPolicy{
			MaxGenerations: 5,
			BudgetUSD:      10,
		},
		PromotionPolicy: optimize.PromotionPolicy{
			MinAccuracyGain: 0.01,
		},
	}
}

// createTestOptRun inserts a test optimization run directly into storage.
func createTestOptRun(t *testing.T, store *storage.Storage, id, workflowID, status string) *optimize.OptimizationRun {
	t.Helper()
	now := time.Now().UTC()
	run := &optimize.OptimizationRun{
		ID:              id,
		WorkflowID:      workflowID,
		WorkflowVersion: 1,
		Benchmark:       "test-bench",
		Split:           "dev",
		ItemLimit:       50,
		Concurrency:     1,
		Spec:            validTestSpec(),
		Strategy:        optimize.OptimizeStrategyEvolutionary,
		PopulationSize:  5,
		ClaudeModel:     "opus",
		MutatorMode:     optimize.MutatorModeAuto,
		TotalBudgetUSD:  10,
		Status:          status,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := store.CreateOptimizationRun(run); err != nil {
		t.Fatalf("Failed to create optimization run: %v", err)
	}
	return run
}

// createTestWorkflow inserts a minimal workflow definition into storage.
func createTestWorkflow(t *testing.T, store *storage.Storage, id string) {
	t.Helper()
	wf := &storage.WorkflowDefinition{
		ID:         id,
		Name:       id,
		Definition: `{"id":"` + id + `","nodes":[]}`,
	}
	if err := store.CreateWorkflow(wf); err != nil {
		t.Fatalf("Failed to create workflow: %v", err)
	}
}

// createTestOrganism inserts a test organism into storage for the given run.
func createTestOrganism(t *testing.T, store *storage.Storage, orgID, runID string, generation int) *optimize.Organism {
	t.Helper()
	org := &optimize.Organism{
		ID:           orgID,
		OptRunID:     runID,
		Generation:   generation,
		ParamValues:  map[string]string{"nodes[agent-a].data.config.model": "model-a"},
		WorkflowJSON: json.RawMessage(`{"id":"test-wf","nodes":[]}`),
		MutationType: "seed",
		CreatedAt:    time.Now().UTC(),
	}
	if err := store.CreateOptimizationOrganism(org); err != nil {
		t.Fatalf("Failed to create organism: %v", err)
	}
	return org
}

func TestOptimizeListRuns_Empty(t *testing.T) {
	_, router, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/admin/optimize/runs", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Expected valid JSON response: %v", err)
	}
	runs, ok := resp["runs"].([]interface{})
	if !ok {
		t.Fatalf("Expected runs array in response, got %T", resp["runs"])
	}
	if len(runs) != 0 {
		t.Fatalf("Expected 0 runs, got %d", len(runs))
	}
}

func TestOptimizeListRuns_WithData(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	createTestWorkflow(t, server.storage, "wf-opt-list")
	createTestOptRun(t, server.storage, "opt-run-1", "wf-opt-list", "running")
	createTestOptRun(t, server.storage, "opt-run-2", "wf-opt-list", "completed")

	t.Run("list all", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/optimize/runs", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Expected valid JSON: %v", err)
		}
		runs := resp["runs"].([]interface{})
		if len(runs) != 2 {
			t.Fatalf("Expected 2 runs, got %d", len(runs))
		}
	})

	t.Run("filter by status", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/optimize/runs?status=running", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Expected valid JSON: %v", err)
		}
		runs := resp["runs"].([]interface{})
		if len(runs) != 1 {
			t.Fatalf("Expected 1 run with status=running, got %d", len(runs))
		}
	})

	t.Run("filter by workflow", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/optimize/runs?workflow=wf-opt-list", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Expected valid JSON: %v", err)
		}
		runs := resp["runs"].([]interface{})
		if len(runs) != 2 {
			t.Fatalf("Expected 2 runs for workflow, got %d", len(runs))
		}
	})
}

func TestOptimizeCreateRun(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	createTestWorkflow(t, server.storage, "wf-create-opt")

	t.Run("success with JSON body", func(t *testing.T) {
		body := map[string]interface{}{
			"workflow_id":     "wf-create-opt",
			"benchmark":       "test-bench",
			"split":           "dev",
			"item_limit":      50,
			"population_size": 5,
			"strategy":        "evolutionary",
			"budget_usd":      10,
			"spec": map[string]interface{}{
				"params": []map[string]interface{}{
					{
						"path":       "nodes[agent-a].data.config.model",
						"type":       "model",
						"candidates": []string{"model-a", "model-b"},
					},
				},
				"objectives": []map[string]interface{}{
					{"metric": "accuracy", "direction": "maximize", "weight": 1.0},
				},
				"stop_policy": map[string]interface{}{
					"max_generations": 5,
					"budget_usd":      10,
				},
				"promotion_policy": map[string]interface{}{
					"min_accuracy_gain": 0.01,
				},
			},
		}
		payload, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/admin/optimize/runs", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusCreated, w.Code, w.Body.String())
		}

		var resp optimize.OptimizationRun
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Expected valid JSON: %v", err)
		}
		if resp.ID == "" {
			t.Fatal("Expected non-empty run ID")
		}
		if resp.WorkflowID != "wf-create-opt" {
			t.Fatalf("Expected workflow_id=wf-create-opt, got %s", resp.WorkflowID)
		}
		if resp.Status != "pending" {
			t.Fatalf("Expected status=pending, got %s", resp.Status)
		}
	})

	t.Run("missing required fields", func(t *testing.T) {
		body := map[string]interface{}{
			"workflow_id": "wf-create-opt",
			// missing benchmark
		}
		payload, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/admin/optimize/runs", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusBadRequest, w.Code, w.Body.String())
		}
	})

	t.Run("workflow not found", func(t *testing.T) {
		body := map[string]interface{}{
			"workflow_id": "nonexistent-wf",
			"benchmark":   "test-bench",
			"spec": map[string]interface{}{
				"params": []map[string]interface{}{
					{
						"path":       "nodes[agent-a].data.config.model",
						"type":       "model",
						"candidates": []string{"model-a"},
					},
				},
				"objectives": []map[string]interface{}{
					{"metric": "accuracy", "direction": "maximize", "weight": 1.0},
				},
				"stop_policy": map[string]interface{}{
					"max_generations": 5,
					"budget_usd":      10,
				},
				"promotion_policy": map[string]interface{}{
					"min_accuracy_gain": 0.01,
				},
			},
		}
		payload, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/admin/optimize/runs", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusNotFound, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Workflow not found") {
			t.Fatalf("Expected 'Workflow not found' in response body, got: %s", w.Body.String())
		}
	})

	t.Run("invalid strategy", func(t *testing.T) {
		body := map[string]interface{}{
			"workflow_id": "wf-create-opt",
			"benchmark":   "test-bench",
			"strategy":    "invalid_strategy",
			"spec": map[string]interface{}{
				"params": []map[string]interface{}{
					{
						"path":       "nodes[agent-a].data.config.model",
						"type":       "model",
						"candidates": []string{"model-a"},
					},
				},
				"objectives": []map[string]interface{}{
					{"metric": "accuracy", "direction": "maximize", "weight": 1.0},
				},
				"stop_policy": map[string]interface{}{
					"max_generations": 5,
					"budget_usd":      10,
				},
				"promotion_policy": map[string]interface{}{
					"min_accuracy_gain": 0.01,
				},
			},
		}
		payload, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/admin/optimize/runs", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusBadRequest, w.Code, w.Body.String())
		}
	})
}

func TestOptimizeRunDetail(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	createTestWorkflow(t, server.storage, "wf-detail")
	createTestOptRun(t, server.storage, "opt-detail-1", "wf-detail", "running")

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/optimize/runs/opt-detail-1", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		var resp optimize.OptimizationRun
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Expected valid JSON: %v", err)
		}
		if resp.ID != "opt-detail-1" {
			t.Fatalf("Expected ID=opt-detail-1, got %s", resp.ID)
		}
		if resp.Status != "running" {
			t.Fatalf("Expected status=running, got %s", resp.Status)
		}
	})

	t.Run("not found", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/optimize/runs/nonexistent", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected status %d, got %d", http.StatusNotFound, w.Code)
		}
	})
}

func TestOptimizeRunPauseResumeCancel(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	createTestWorkflow(t, server.storage, "wf-lifecycle")
	createTestOptRun(t, server.storage, "opt-lifecycle-1", "wf-lifecycle", "running")

	t.Run("pause", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/admin/optimize/runs/opt-lifecycle-1/pause", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Expected valid JSON: %v", err)
		}
		if resp["status"] != "paused" {
			t.Fatalf("Expected status=paused, got %v", resp["status"])
		}

		// Verify in storage
		run, err := server.storage.GetOptimizationRun("opt-lifecycle-1")
		if err != nil {
			t.Fatalf("Failed to reload run: %v", err)
		}
		if run.Status != "paused" {
			t.Fatalf("Expected stored status=paused, got %s", run.Status)
		}
	})

	t.Run("resume", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/admin/optimize/runs/opt-lifecycle-1/resume", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Expected valid JSON: %v", err)
		}
		if resp["status"] != "running" {
			t.Fatalf("Expected status=running, got %v", resp["status"])
		}

		run, err := server.storage.GetOptimizationRun("opt-lifecycle-1")
		if err != nil {
			t.Fatalf("Failed to reload run: %v", err)
		}
		if run.Status != "running" {
			t.Fatalf("Expected stored status=running, got %s", run.Status)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/admin/optimize/runs/opt-lifecycle-1/cancel", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Expected valid JSON: %v", err)
		}
		if resp["status"] != "cancelled" {
			t.Fatalf("Expected status=cancelled, got %v", resp["status"])
		}

		run, err := server.storage.GetOptimizationRun("opt-lifecycle-1")
		if err != nil {
			t.Fatalf("Failed to reload run: %v", err)
		}
		if run.Status != "cancelled" {
			t.Fatalf("Expected stored status=cancelled, got %s", run.Status)
		}
	})
}

func TestOptimizeRunOrganisms_List(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	createTestWorkflow(t, server.storage, "wf-orgs")
	createTestOptRun(t, server.storage, "opt-orgs-1", "wf-orgs", "running")
	createTestOrganism(t, server.storage, "org-1", "opt-orgs-1", 0)
	createTestOrganism(t, server.storage, "org-2", "opt-orgs-1", 1)

	t.Run("list all organisms", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/optimize/runs/opt-orgs-1/organisms", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Expected valid JSON: %v", err)
		}
		organisms := resp["organisms"].([]interface{})
		if len(organisms) != 2 {
			t.Fatalf("Expected 2 organisms, got %d", len(organisms))
		}
		total := resp["total"].(float64)
		if int(total) != 2 {
			t.Fatalf("Expected total=2, got %v", total)
		}
	})

	t.Run("filter by generation", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/optimize/runs/opt-orgs-1/organisms?generation=0", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Expected valid JSON: %v", err)
		}
		organisms := resp["organisms"].([]interface{})
		if len(organisms) != 1 {
			t.Fatalf("Expected 1 organism for generation=0, got %d", len(organisms))
		}
	})
}

func TestOptimizeRunOrganisms_Create(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	createTestWorkflow(t, server.storage, "wf-org-create")
	createTestOptRun(t, server.storage, "opt-org-create-1", "wf-org-create", "running")

	body := map[string]interface{}{
		"id":            "org-new-1",
		"generation":    0,
		"param_values":  map[string]string{"nodes[agent-a].data.config.model": "model-b"},
		"workflow_json": map[string]interface{}{"id": "wf-org-create", "nodes": []interface{}{}},
		"mutation_type": "seed",
	}
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/admin/optimize/runs/opt-org-create-1/organisms", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Expected valid JSON: %v", err)
	}
	if resp["created"] != true {
		t.Fatalf("Expected created=true, got %v", resp["created"])
	}
	if resp["organism_id"] != "org-new-1" {
		t.Fatalf("Expected organism_id=org-new-1, got %v", resp["organism_id"])
	}

	// Verify organism exists in storage
	org, err := server.storage.GetOptimizationOrganism("org-new-1")
	if err != nil {
		t.Fatalf("Failed to get created organism: %v", err)
	}
	if org.OptRunID != "opt-org-create-1" {
		t.Fatalf("Expected opt_run_id=opt-org-create-1, got %s", org.OptRunID)
	}
}

func TestOptimizeRunOrganismDetail(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	createTestWorkflow(t, server.storage, "wf-org-detail")
	createTestOptRun(t, server.storage, "opt-org-detail-1", "wf-org-detail", "running")
	createTestOrganism(t, server.storage, "org-detail-1", "opt-org-detail-1", 0)

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/optimize/runs/opt-org-detail-1/organisms/org-detail-1", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Expected valid JSON: %v", err)
		}
		if resp["organism"] == nil {
			t.Fatal("Expected organism in response")
		}
	})

	t.Run("organism not found", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/optimize/runs/opt-org-detail-1/organisms/nonexistent", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected status %d, got %d", http.StatusNotFound, w.Code)
		}
	})

	t.Run("organism in wrong run returns 404", func(t *testing.T) {
		createTestOptRun(t, server.storage, "opt-org-detail-other", "wf-org-detail", "running")
		req := httptest.NewRequest("GET", "/api/admin/optimize/runs/opt-org-detail-other/organisms/org-detail-1", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected status %d for organism in wrong run, got %d", http.StatusNotFound, w.Code)
		}
	})
}

func TestOptimizeRunStatusPatch(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	createTestWorkflow(t, server.storage, "wf-status-patch")
	createTestOptRun(t, server.storage, "opt-status-1", "wf-status-patch", "running")

	t.Run("success", func(t *testing.T) {
		body := map[string]interface{}{"status": "completed"}
		payload, _ := json.Marshal(body)
		req := httptest.NewRequest("PATCH", "/api/admin/optimize/runs/opt-status-1/status", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Expected valid JSON: %v", err)
		}
		if resp["status"] != "completed" {
			t.Fatalf("Expected status=completed, got %v", resp["status"])
		}
	})

	t.Run("empty status rejected", func(t *testing.T) {
		body := map[string]interface{}{"status": ""}
		payload, _ := json.Marshal(body)
		req := httptest.NewRequest("PATCH", "/api/admin/optimize/runs/opt-status-1/status", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
		}
	})
}

func TestOptimizeRunProgress(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	createTestWorkflow(t, server.storage, "wf-progress")
	createTestOptRun(t, server.storage, "opt-progress-1", "wf-progress", "running")

	body := map[string]interface{}{
		"generation":       2,
		"best_organism_id": "org-best",
		"best_fitness": map[string]interface{}{
			"accuracy":          0.85,
			"adjusted_accuracy": 0.83,
			"composite_score":   0.80,
			"feasible":          true,
		},
		"spent_usd":       1.50,
		"total_organisms": 10,
	}
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest("PATCH", "/api/admin/optimize/runs/opt-progress-1/progress", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Expected valid JSON: %v", err)
	}
	if resp["updated"] != true {
		t.Fatalf("Expected updated=true, got %v", resp["updated"])
	}

	// Verify progress was stored
	run, err := server.storage.GetOptimizationRun("opt-progress-1")
	if err != nil {
		t.Fatalf("Failed to reload run: %v", err)
	}
	if run.Generation != 2 {
		t.Fatalf("Expected generation=2, got %d", run.Generation)
	}
	if run.BestOrganismID != "org-best" {
		t.Fatalf("Expected best_organism_id=org-best, got %s", run.BestOrganismID)
	}
	if run.TotalOrganisms != 10 {
		t.Fatalf("Expected total_organisms=10, got %d", run.TotalOrganisms)
	}
}

func TestOptimizeRunLineage(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	createTestWorkflow(t, server.storage, "wf-lineage")
	createTestOptRun(t, server.storage, "opt-lineage-1", "wf-lineage", "running")
	createTestOrganism(t, server.storage, "org-lin-1", "opt-lineage-1", 0)
	createTestOrganism(t, server.storage, "org-lin-2", "opt-lineage-1", 1)

	t.Run("with organisms", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/optimize/runs/opt-lineage-1/lineage", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Expected valid JSON: %v", err)
		}
		nodes := resp["nodes"].([]interface{})
		if len(nodes) != 2 {
			t.Fatalf("Expected 2 lineage nodes, got %d", len(nodes))
		}
		if resp["edges"] == nil {
			t.Fatal("Expected edges key in lineage response")
		}
	})

	t.Run("empty lineage", func(t *testing.T) {
		createTestOptRun(t, server.storage, "opt-lineage-empty", "wf-lineage", "pending")
		req := httptest.NewRequest("GET", "/api/admin/optimize/runs/opt-lineage-empty/lineage", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d", http.StatusOK, w.Code)
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Expected valid JSON: %v", err)
		}
		nodes := resp["nodes"].([]interface{})
		if len(nodes) != 0 {
			t.Fatalf("Expected 0 lineage nodes for empty run, got %d", len(nodes))
		}
	})
}

func TestOptimizeRunLearningLog(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	createTestWorkflow(t, server.storage, "wf-learning")
	createTestOptRun(t, server.storage, "opt-learning-1", "wf-learning", "running")

	t.Run("append entry", func(t *testing.T) {
		body := map[string]interface{}{
			"generation":    1,
			"organism_id":   "org-learn-1",
			"parent_id":     "org-learn-0",
			"mutation_type": "llm",
			"description":   "Changed model from A to B",
			"outcome":       "improved",
			"fitness_delta": 0.05,
		}
		payload, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/admin/optimize/runs/opt-learning-1/learning-log", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}
	})

	t.Run("read entries", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/optimize/runs/opt-learning-1/learning-log", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Expected valid JSON: %v", err)
		}
		entries := resp["entries"].([]interface{})
		if len(entries) != 1 {
			t.Fatalf("Expected 1 learning entry, got %d", len(entries))
		}
	})
}

func TestOptimizeRunHeartbeat(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	createTestWorkflow(t, server.storage, "wf-heartbeat")
	createTestOptRun(t, server.storage, "opt-heartbeat-1", "wf-heartbeat", "running")

	t.Run("acquire lease", func(t *testing.T) {
		body := map[string]interface{}{
			"owner_pid":      12345,
			"owner_hostname": "test-host",
		}
		payload, _ := json.Marshal(body)
		req := httptest.NewRequest("PATCH", "/api/admin/optimize/runs/opt-heartbeat-1/heartbeat", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Expected valid JSON: %v", err)
		}
		if resp["ok"] != true {
			t.Fatalf("Expected ok=true, got %v", resp["ok"])
		}
	})

	t.Run("clear lease", func(t *testing.T) {
		body := map[string]interface{}{"clear": true}
		payload, _ := json.Marshal(body)
		req := httptest.NewRequest("PATCH", "/api/admin/optimize/runs/opt-heartbeat-1/heartbeat", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Expected valid JSON: %v", err)
		}
		if resp["cleared"] != true {
			t.Fatalf("Expected cleared=true, got %v", resp["cleared"])
		}
	})

	t.Run("missing required fields", func(t *testing.T) {
		body := map[string]interface{}{"owner_pid": 0}
		payload, _ := json.Marshal(body)
		req := httptest.NewRequest("PATCH", "/api/admin/optimize/runs/opt-heartbeat-1/heartbeat", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusBadRequest, w.Code, w.Body.String())
		}
	})

	t.Run("not found", func(t *testing.T) {
		body := map[string]interface{}{
			"owner_pid":      99999,
			"owner_hostname": "other-host",
		}
		payload, _ := json.Marshal(body)
		req := httptest.NewRequest("PATCH", "/api/admin/optimize/runs/nonexistent/heartbeat", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected status %d, got %d", http.StatusNotFound, w.Code)
		}
	})
}

func TestOptimizeOrganismDetail(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	createTestWorkflow(t, server.storage, "wf-org-global")
	createTestOptRun(t, server.storage, "opt-org-global-1", "wf-org-global", "running")
	createTestOrganism(t, server.storage, "org-global-1", "opt-org-global-1", 0)

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/optimize/organisms/org-global-1", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Expected valid JSON: %v", err)
		}
		if resp["organism"] == nil {
			t.Fatal("Expected organism in response")
		}
	})

	t.Run("not found", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/optimize/organisms/nonexistent", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected status %d, got %d", http.StatusNotFound, w.Code)
		}
	})
}

func TestOptimizeOrganismFitnessPatch(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	createTestWorkflow(t, server.storage, "wf-fitness")
	createTestOptRun(t, server.storage, "opt-fitness-1", "wf-fitness", "running")
	createTestOrganism(t, server.storage, "org-fitness-1", "opt-fitness-1", 0)

	t.Run("success", func(t *testing.T) {
		body := map[string]interface{}{
			"bench_run_id": "bench-run-1",
			"fitness": map[string]interface{}{
				"accuracy":          0.90,
				"adjusted_accuracy": 0.88,
				"parse_rate":        1.0,
				"total_cost":        0.50,
				"cost_per_item":     0.01,
				"composite_score":   0.85,
				"feasible":          true,
				"total_items":       50,
			},
		}
		payload, _ := json.Marshal(body)
		req := httptest.NewRequest("PATCH", "/api/admin/optimize/organisms/org-fitness-1/fitness", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Expected valid JSON: %v", err)
		}
		if resp["updated"] != true {
			t.Fatalf("Expected updated=true, got %v", resp["updated"])
		}
	})

	t.Run("missing fitness", func(t *testing.T) {
		body := map[string]interface{}{"bench_run_id": "bench-run-2"}
		payload, _ := json.Marshal(body)
		req := httptest.NewRequest("PATCH", "/api/admin/optimize/organisms/org-fitness-1/fitness", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
		}
	})
}

func TestOptimizeOrganismParamChanges(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	createTestWorkflow(t, server.storage, "wf-param-changes")
	createTestOptRun(t, server.storage, "opt-param-1", "wf-param-changes", "running")
	createTestOrganism(t, server.storage, "org-param-1", "opt-param-1", 0)

	t.Run("create param changes", func(t *testing.T) {
		body := map[string]interface{}{
			"changes": []map[string]interface{}{
				{
					"path":      "nodes[agent-a].data.config.model",
					"old_value": "model-a",
					"new_value": "model-b",
					"reason":    "better accuracy",
				},
			},
		}
		payload, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/admin/optimize/organisms/org-param-1/param-changes", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}
	})

	t.Run("read param changes", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/optimize/organisms/org-param-1/param-changes", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Expected valid JSON: %v", err)
		}
		changes := resp["changes"].([]interface{})
		if len(changes) != 1 {
			t.Fatalf("Expected 1 param change, got %d", len(changes))
		}
	})
}

func TestOptimizeOrganismMutationArtifacts(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	createTestWorkflow(t, server.storage, "wf-artifacts")
	createTestOptRun(t, server.storage, "opt-artifacts-1", "wf-artifacts", "running")
	createTestOrganism(t, server.storage, "org-artifacts-1", "opt-artifacts-1", 0)

	t.Run("create mutation artifacts", func(t *testing.T) {
		body := map[string]interface{}{
			"artifacts": []map[string]interface{}{
				{
					"input_prompt_hash": "abc123",
					"input_prompt":      "Optimize the model selection",
					"raw_output_hash":   "def456",
					"raw_output":        "Selected model-b for better performance",
					"claude_model":      "opus",
				},
			},
		}
		payload, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/api/admin/optimize/organisms/org-artifacts-1/mutation-artifacts", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}
	})

	t.Run("read mutation artifacts", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/optimize/organisms/org-artifacts-1/mutation-artifacts", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Expected valid JSON: %v", err)
		}
		artifacts := resp["artifacts"].([]interface{})
		if len(artifacts) != 1 {
			t.Fatalf("Expected 1 mutation artifact, got %d", len(artifacts))
		}
	})
}

func TestOptimizeCompare(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	createTestWorkflow(t, server.storage, "wf-compare")
	createTestOptRun(t, server.storage, "opt-cmp-1", "wf-compare", "completed")
	createTestOptRun(t, server.storage, "opt-cmp-2", "wf-compare", "completed")

	t.Run("compare two runs", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/optimize/compare?ids=opt-cmp-1,opt-cmp-2", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
		}

		var resp optimizeCompareResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("Expected valid JSON: %v", err)
		}
		if len(resp.Runs) != 2 {
			t.Fatalf("Expected 2 runs in comparison, got %d", len(resp.Runs))
		}
	})

	t.Run("missing ids", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/optimize/compare", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
		}
	})

	t.Run("single id rejected", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/optimize/compare?ids=opt-cmp-1", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("Expected status %d, got %d", http.StatusBadRequest, w.Code)
		}
	})

	t.Run("nonexistent run", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/admin/optimize/compare?ids=opt-cmp-1,nonexistent", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("Expected status %d, got %d", http.StatusNotFound, w.Code)
		}
	})
}

func TestOptimizeOrganismLineage(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	createTestWorkflow(t, server.storage, "wf-org-lineage")
	createTestOptRun(t, server.storage, "opt-org-lineage-1", "wf-org-lineage", "running")
	createTestOrganism(t, server.storage, "org-lineage-root", "opt-org-lineage-1", 0)

	req := httptest.NewRequest("GET", "/api/admin/optimize/organisms/org-lineage-root/lineage", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Expected valid JSON: %v", err)
	}
	if resp["organisms"] == nil {
		t.Fatal("Expected organisms key in lineage response")
	}
}

func TestOptimizeHeartbeat_Conflict(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	createTestWorkflow(t, server.storage, "wf-hb-conflict")
	createTestOptRun(t, server.storage, "opt-hb-conflict", "wf-hb-conflict", "running")

	// First, acquire a lease
	body := map[string]interface{}{
		"owner_pid":      11111,
		"owner_hostname": "host-a",
	}
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest("PATCH", "/api/admin/optimize/runs/opt-hb-conflict/heartbeat", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Expected initial lease to succeed, got %d", w.Code)
	}

	// Different owner tries to acquire - should conflict
	body2 := map[string]interface{}{
		"owner_pid":      22222,
		"owner_hostname": "host-b",
	}
	payload2, _ := json.Marshal(body2)
	req2 := httptest.NewRequest("PATCH", "/api/admin/optimize/runs/opt-hb-conflict/heartbeat", bytes.NewReader(payload2))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusConflict {
		t.Fatalf("Expected status %d for conflicting lease, got %d. Body: %s", http.StatusConflict, w2.Code, w2.Body.String())
	}
}

func TestOptimizeBuildGenerationAggregates(t *testing.T) {
	run := &optimize.OptimizationRun{ItemLimit: 50}
	organisms := []*optimize.Organism{
		{
			Generation: 0,
			Fitness:    &optimize.Fitness{AdjustedAccuracy: 0.80, CostPerItem: 0.01, TotalItems: 50},
		},
		{
			Generation: 0,
			Fitness:    &optimize.Fitness{AdjustedAccuracy: 0.75, CostPerItem: 0.02, TotalItems: 50},
		},
		{
			Generation: 1,
			Fitness:    &optimize.Fitness{AdjustedAccuracy: 0.85, CostPerItem: 0.015, TotalItems: 50},
		},
		{
			Generation: 1,
			Fitness:    nil, // should be skipped
		},
	}

	aggregates := buildOptimizeGenerationAggregates(run, organisms)
	if len(aggregates) != 2 {
		t.Fatalf("Expected 2 generation aggregates, got %d", len(aggregates))
	}

	gen0 := aggregates[0]
	if gen0.Generation != 0 {
		t.Fatalf("Expected first aggregate generation=0, got %d", gen0.Generation)
	}
	if gen0.BestAccuracy != 0.80 {
		t.Fatalf("Expected gen0 best=0.80, got %f", gen0.BestAccuracy)
	}
	if gen0.WorstAccuracy != 0.75 {
		t.Fatalf("Expected gen0 worst=0.75, got %f", gen0.WorstAccuracy)
	}
	expectedMean := (0.80 + 0.75) / 2
	if fmt.Sprintf("%.4f", gen0.MeanAccuracy) != fmt.Sprintf("%.4f", expectedMean) {
		t.Fatalf("Expected gen0 mean=%.4f, got %.4f", expectedMean, gen0.MeanAccuracy)
	}

	gen1 := aggregates[1]
	if gen1.Generation != 1 {
		t.Fatalf("Expected second aggregate generation=1, got %d", gen1.Generation)
	}
	if gen1.BestAccuracy != 0.85 {
		t.Fatalf("Expected gen1 best=0.85, got %f", gen1.BestAccuracy)
	}
	// Cumulative cost should include both generations
	if gen1.CumulativeCost <= gen0.CumulativeCost {
		t.Fatalf("Expected gen1 cumulative cost > gen0, got gen0=%f gen1=%f", gen0.CumulativeCost, gen1.CumulativeCost)
	}
}
