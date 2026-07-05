package optimize

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseSeedOptimizeSpec_ValidFull(t *testing.T) {
	seed := map[string]interface{}{
		"id":   "test-workflow",
		"name": "Test Workflow",
		"nodes": []interface{}{
			map[string]interface{}{
				"id": "agent-a",
				"data": map[string]interface{}{
					"config": map[string]interface{}{
						"model":  "gpt-4",
						"prompt": "You are helpful.",
					},
				},
			},
		},
		"optimize": map[string]interface{}{
			"params": []interface{}{
				map[string]interface{}{
					"path":       "nodes[agent-a].data.config.model",
					"type":       "model",
					"candidates": []interface{}{"gpt-4", "claude-3"},
				},
			},
			"objectives": []interface{}{
				map[string]interface{}{
					"metric":    "accuracy",
					"direction": "maximize",
					"weight":    1,
				},
			},
			"stop_policy": map[string]interface{}{
				"max_generations": 10,
				"budget_usd":      5.0,
			},
			"promotion_policy": map[string]interface{}{},
		},
	}

	seedJSON, err := json.Marshal(seed)
	if err != nil {
		t.Fatalf("marshal seed: %v", err)
	}

	parsed, err := ParseSeedOptimizeSpec(seedJSON)
	if err != nil {
		t.Fatalf("ParseSeedOptimizeSpec error: %v", err)
	}

	if parsed.WorkflowID != "test-workflow" {
		t.Errorf("WorkflowID = %q, want %q", parsed.WorkflowID, "test-workflow")
	}
	if parsed.WorkflowName != "Test Workflow" {
		t.Errorf("WorkflowName = %q, want %q", parsed.WorkflowName, "Test Workflow")
	}
	if parsed.OptimizeSpec == nil {
		t.Fatal("OptimizeSpec is nil")
	}
	if len(parsed.OptimizeSpec.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(parsed.OptimizeSpec.Params))
	}

	// Verify optimize key is stripped from workflow JSON.
	var wfObj map[string]interface{}
	if err := json.Unmarshal(parsed.WorkflowJSON, &wfObj); err != nil {
		t.Fatalf("unmarshal workflow JSON: %v", err)
	}
	if _, exists := wfObj["optimize"]; exists {
		t.Error("optimize key should be stripped from workflow JSON")
	}
}

func TestParseSeedOptimizeSpec_EmptyJSON(t *testing.T) {
	_, err := ParseSeedOptimizeSpec(nil)
	if err == nil {
		t.Fatal("expected error for empty JSON")
	}
}

func TestParseSeedOptimizeSpec_NoID(t *testing.T) {
	seedJSON := []byte(`{"name": "test", "optimize": {}}`)
	_, err := ParseSeedOptimizeSpec(seedJSON)
	if err == nil {
		t.Fatal("expected error for missing id")
	}
	if !strings.Contains(err.Error(), "id is required") {
		t.Errorf("error should mention id, got: %v", err)
	}
}

func TestParseSeedOptimizeSpec_NoOptimizeKey(t *testing.T) {
	seedJSON := []byte(`{"id": "test-wf", "nodes": []}`)
	_, err := ParseSeedOptimizeSpec(seedJSON)
	if err == nil {
		t.Fatal("expected error for missing optimize key")
	}
	if !strings.Contains(err.Error(), "does not declare optimize") {
		t.Errorf("error should mention 'does not declare optimize', got: %v", err)
	}
}

func TestParseSeedOptimizeSpec_InvalidSpec(t *testing.T) {
	seed := map[string]interface{}{
		"id":    "test-wf",
		"nodes": []interface{}{},
		"optimize": map[string]interface{}{
			"params":     []interface{}{},
			"objectives": []interface{}{},
		},
	}
	seedJSON, _ := json.Marshal(seed)
	_, err := ParseSeedOptimizeSpec(seedJSON)
	if err == nil {
		t.Fatal("expected error for invalid spec (no params)")
	}
}

func TestParseSeedOptimizeSpec_ParamPathNotInWorkflow(t *testing.T) {
	seed := map[string]interface{}{
		"id":    "test-wf",
		"nodes": []interface{}{},
		"optimize": map[string]interface{}{
			"params": []interface{}{
				map[string]interface{}{
					"path":       "nodes[missing].data.config.model",
					"type":       "model",
					"candidates": []interface{}{"a", "b"},
				},
			},
			"objectives": []interface{}{
				map[string]interface{}{"metric": "accuracy", "direction": "maximize", "weight": 1},
			},
			"stop_policy": map[string]interface{}{
				"max_generations": 1,
				"budget_usd":      1.0,
			},
			"promotion_policy": map[string]interface{}{},
		},
	}
	seedJSON, _ := json.Marshal(seed)
	_, err := ParseSeedOptimizeSpec(seedJSON)
	if err == nil {
		t.Fatal("expected error when param path not found in workflow")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestParseSeedOptimizeSpec_LockedPathNotInWorkflow(t *testing.T) {
	seed := map[string]interface{}{
		"id": "test-wf",
		"nodes": []interface{}{
			map[string]interface{}{
				"id": "a",
				"data": map[string]interface{}{
					"config": map[string]interface{}{"model": "x"},
				},
			},
		},
		"optimize": map[string]interface{}{
			"params": []interface{}{
				map[string]interface{}{
					"path":       "nodes[a].data.config.model",
					"type":       "model",
					"candidates": []interface{}{"x", "y"},
				},
			},
			"locked": []interface{}{"nodes[a].data.config.missing_locked"},
			"objectives": []interface{}{
				map[string]interface{}{"metric": "accuracy", "direction": "maximize", "weight": 1},
			},
			"stop_policy": map[string]interface{}{
				"max_generations": 1,
				"budget_usd":      1.0,
			},
			"promotion_policy": map[string]interface{}{},
		},
	}
	seedJSON, _ := json.Marshal(seed)
	_, err := ParseSeedOptimizeSpec(seedJSON)
	if err == nil {
		t.Fatal("expected error when locked path not found in workflow")
	}
}
