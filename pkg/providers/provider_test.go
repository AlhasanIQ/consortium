package providers

import (
	"encoding/json"
	"testing"
)

func TestProviderJSONContracts(t *testing.T) {
	cost := 0.005
	modelJSON, err := json.Marshal(Model{
		ID:         "gpt-4",
		Name:       "GPT-4",
		Provider:   "openai",
		ContextLen: 128000,
		InputCost:  0.00003,
		OutputCost: 0.00006,
		MaxTokens:  4096,
		Available:  true,
	})
	if err != nil {
		t.Fatalf("marshal model: %v", err)
	}
	var model map[string]any
	if err := json.Unmarshal(modelJSON, &model); err != nil {
		t.Fatalf("decode model JSON: %v", err)
	}
	for key, want := range map[string]any{
		"id":                    "gpt-4",
		"provider":              "openai",
		"context_length":        float64(128000),
		"input_cost_per_token":  float64(0.00003),
		"output_cost_per_token": float64(0.00006),
		"max_tokens":            float64(4096),
		"available":             true,
	} {
		if model[key] != want {
			t.Fatalf("model[%q] = %#v, want %#v; JSON=%s", key, model[key], want, modelJSON)
		}
	}

	usageJSON, err := json.Marshal(Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		Cost:             &cost,
	})
	if err != nil {
		t.Fatalf("marshal usage: %v", err)
	}
	var usage map[string]any
	if err := json.Unmarshal(usageJSON, &usage); err != nil {
		t.Fatalf("decode usage JSON: %v", err)
	}
	if usage["prompt_tokens"] != float64(100) || usage["completion_tokens"] != float64(50) || usage["total_tokens"] != float64(150) {
		t.Fatalf("usage token fields = %#v", usage)
	}
	if usage["cost"] != cost {
		t.Fatalf("usage cost = %#v, want %v", usage["cost"], cost)
	}

	withoutCost, err := json.Marshal(Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3})
	if err != nil {
		t.Fatalf("marshal usage without cost: %v", err)
	}
	var usageWithoutCost map[string]any
	if err := json.Unmarshal(withoutCost, &usageWithoutCost); err != nil {
		t.Fatalf("decode usage without cost JSON: %v", err)
	}
	if _, ok := usageWithoutCost["cost"]; ok {
		t.Fatalf("usage without provider cost serialized cost: %s", withoutCost)
	}
}

func TestCompletionRequestJSONPreservesExplicitZeroAndExtensions(t *testing.T) {
	requestJSON, err := json.Marshal(CompletionRequest{
		Model:       "test-model",
		Messages:    []Message{{Role: "user", Content: "hello"}},
		Temperature: Float64Ptr(0),
		Extensions: &ProviderExtensions{
			Reasoning: &ReasoningConfig{Effort: "high"},
			Seed:      IntPtr(42),
			ProviderRouting: &ProviderRouting{
				Only:           []string{"openai"},
				AllowFallbacks: BoolPtr(false),
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal completion request: %v", err)
	}
	var request map[string]any
	if err := json.Unmarshal(requestJSON, &request); err != nil {
		t.Fatalf("decode completion request JSON: %v", err)
	}
	if request["temperature"] != float64(0) {
		t.Fatalf("temperature = %#v, want explicit zero; JSON=%s", request["temperature"], requestJSON)
	}
	extensions, ok := request["extensions"].(map[string]any)
	if !ok {
		t.Fatalf("extensions = %#v, want object", request["extensions"])
	}
	if extensions["seed"] != float64(42) {
		t.Fatalf("extensions.seed = %#v, want 42", extensions["seed"])
	}
	reasoning, ok := extensions["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "high" {
		t.Fatalf("extensions.reasoning = %#v, want effort=high", extensions["reasoning"])
	}
	routing, ok := extensions["provider"].(map[string]any)
	if !ok || routing["allow_fallbacks"] != false {
		t.Fatalf("extensions.provider = %#v, want allow_fallbacks=false", extensions["provider"])
	}
}
