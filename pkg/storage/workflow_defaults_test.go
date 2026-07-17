package storage

import (
	"encoding/json"
	"testing"
)

func TestApplyLegacyWorkflowDefaults_PromptNode(t *testing.T) {
	def := `{"nodes":[{"type":"prompt","data":{"type":"prompt","config":{}}}]}`
	result, changed, err := ApplyLegacyWorkflowDefaults(def)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Error("expected changed=true for bare prompt node")
	}

	var wf map[string]interface{}
	if err := json.Unmarshal([]byte(result), &wf); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	nodes := wf["nodes"].([]interface{})
	node := nodes[0].(map[string]interface{})
	config := node["data"].(map[string]interface{})["config"].(map[string]interface{})

	if config["temperature"] != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", config["temperature"])
	}
	if config["maxTokens"] != float64(1000) {
		t.Errorf("expected maxTokens 1000, got %v", config["maxTokens"])
	}
	if config["timeoutSeconds"] != float64(120) {
		t.Errorf("expected timeoutSeconds 120, got %v", config["timeoutSeconds"])
	}
	if _, ok := config["retryPolicy"]; !ok {
		t.Error("expected retryPolicy to be set")
	}
}

func TestApplyLegacyWorkflowDefaults_ResultNode(t *testing.T) {
	def := `{"nodes":[{"type":"result","data":{"type":"result","config":{}}}]}`
	result, changed, err := ApplyLegacyWorkflowDefaults(def)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Error("expected changed=true for bare result node")
	}

	var wf map[string]interface{}
	if err := json.Unmarshal([]byte(result), &wf); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	nodes := wf["nodes"].([]interface{})
	node := nodes[0].(map[string]interface{})
	config := node["data"].(map[string]interface{})["config"].(map[string]interface{})

	if config["aggregationMethod"] != "collect" {
		t.Errorf("expected default aggregationMethod 'collect', got %v", config["aggregationMethod"])
	}
}

func TestApplyLegacyWorkflowDefaults_NoNodes(t *testing.T) {
	def := `{"name":"empty"}`
	result, changed, err := ApplyLegacyWorkflowDefaults(def)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Error("expected no changes for definition without nodes")
	}
	if result != def {
		t.Error("expected original definition returned unchanged")
	}
}

func TestApplyLegacyWorkflowDefaults_InvalidJSON(t *testing.T) {
	_, _, err := ApplyLegacyWorkflowDefaults("not json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestApplyLegacyWorkflowDefaults_AlreadyHasDefaults(t *testing.T) {
	def := `{"nodes":[{"type":"prompt","data":{"type":"prompt","config":{"temperature":0.5,"maxTokens":500,"timeoutSeconds":60,"retryPolicy":{"max_attempts":3}}}}]}`
	_, changed, err := ApplyLegacyWorkflowDefaults(def)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if changed {
		t.Error("expected no changes when defaults already present")
	}
}

func TestApplyLegacyWorkflowDefaults_AggregationModelDefaults(t *testing.T) {
	tests := []struct {
		method string
		field  string
	}{
		{method: "judge", field: "judge_model"},
		{method: "synthesis", field: "model"},
		{method: "scoring", field: "scoring_model"},
		{method: "debate_decide", field: "judge_model"},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			def := `{"nodes":[{"type":"result","data":{"type":"result","config":{"aggregationMethod":"` + tt.method + `"}}}]}`
			result, changed, err := ApplyLegacyWorkflowDefaults(def)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !changed {
				t.Fatalf("expected changed=true for %s without config", tt.method)
			}

			var wf map[string]interface{}
			if err := json.Unmarshal([]byte(result), &wf); err != nil {
				t.Fatalf("unmarshal result: %v", err)
			}
			nodes := wf["nodes"].([]interface{})
			node := nodes[0].(map[string]interface{})
			config := node["data"].(map[string]interface{})["config"].(map[string]interface{})
			aggConfig := config["aggregationConfig"].(map[string]interface{})

			if aggConfig[tt.field] != "deepseek/deepseek-v4-flash" {
				t.Errorf("expected %s deepseek/deepseek-v4-flash, got %v", tt.field, aggConfig[tt.field])
			}
		})
	}
}

func TestApplyLegacyWorkflowDefaults_JudgeAggregationExtractionDefaults(t *testing.T) {
	def := `{"nodes":[{"type":"result","data":{"type":"result","config":{"aggregationMethod":"judge"}}}]}`
	result, changed, err := ApplyLegacyWorkflowDefaults(def)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Error("expected changed=true for judge without config")
	}

	var wf map[string]interface{}
	if err := json.Unmarshal([]byte(result), &wf); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	nodes := wf["nodes"].([]interface{})
	node := nodes[0].(map[string]interface{})
	config := node["data"].(map[string]interface{})["config"].(map[string]interface{})
	aggConfig := config["aggregationConfig"].(map[string]interface{})

	if aggConfig["extraction_strategy"] != "regex" {
		t.Errorf("expected extraction_strategy regex, got %v", aggConfig["extraction_strategy"])
	}
}

func TestApplyLegacyWorkflowDefaults_ChildWorkflowNode(t *testing.T) {
	def := `{"nodes":[{"type":"child_workflow","data":{"type":"child_workflow","config":{}}}]}`
	result, changed, err := ApplyLegacyWorkflowDefaults(def)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Error("expected changed=true")
	}

	var wf map[string]interface{}
	if err := json.Unmarshal([]byte(result), &wf); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	nodes := wf["nodes"].([]interface{})
	node := nodes[0].(map[string]interface{})
	config := node["data"].(map[string]interface{})["config"].(map[string]interface{})

	if config["timeoutSeconds"] != float64(120) {
		t.Errorf("expected timeoutSeconds 120, got %v", config["timeoutSeconds"])
	}
	if _, ok := config["retryPolicy"]; !ok {
		t.Error("expected retryPolicy to be set")
	}
}

func TestSeedWorkflows_Integration(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	// NewStorage seeds workflows automatically; verify they exist
	seeds, err := store.GetSeedWorkflows()
	if err != nil {
		t.Fatalf("GetSeedWorkflows: %v", err)
	}
	if len(seeds) == 0 {
		t.Fatal("expected at least one seed workflow")
	}

	// Verify seed workflows are in the database
	for _, seed := range seeds {
		wf, err := store.GetWorkflow(seed.ID)
		if err != nil {
			t.Errorf("seed workflow %s not found in DB: %v", seed.ID, err)
			continue
		}
		if wf.Name == "" {
			t.Errorf("seed workflow %s has empty name", seed.ID)
		}
	}
}

func TestGetSeedWorkflowJSONByID(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	seeds, err := store.GetSeedWorkflows()
	if err != nil {
		t.Fatalf("GetSeedWorkflows: %v", err)
	}
	if len(seeds) == 0 {
		t.Fatal("embedded seed workflows are unexpectedly empty")
	}

	// Test with valid ID
	jsonStr, err := store.GetSeedWorkflowJSONByID(seeds[0].ID)
	if err != nil {
		t.Fatalf("GetSeedWorkflowJSONByID: %v", err)
	}
	if jsonStr == "" {
		t.Error("expected non-empty JSON")
	}

	// Verify it's valid JSON
	var parsed interface{}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Errorf("seed JSON is not valid JSON: %v", err)
	}

	// Test with invalid ID
	_, err = store.GetSeedWorkflowJSONByID("nonexistent-seed")
	if err == nil {
		t.Error("expected error for nonexistent seed ID")
	}
}
