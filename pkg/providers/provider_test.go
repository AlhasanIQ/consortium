package providers

import (
	"testing"
)

func TestFloat64Ptr(t *testing.T) {
	v := 0.7
	p := Float64Ptr(v)
	if p == nil {
		t.Fatal("expected non-nil pointer")
	}
	if *p != v {
		t.Errorf("expected %f, got %f", v, *p)
	}
}

func TestFloat64Ptr_Zero(t *testing.T) {
	p := Float64Ptr(0.0)
	if p == nil {
		t.Fatal("expected non-nil pointer for zero value")
	}
	if *p != 0.0 {
		t.Errorf("expected 0.0, got %f", *p)
	}
}

func TestIntPtr(t *testing.T) {
	v := 42
	p := IntPtr(v)
	if p == nil {
		t.Fatal("expected non-nil pointer")
	}
	if *p != v {
		t.Errorf("expected %d, got %d", v, *p)
	}
}

func TestBoolPtr(t *testing.T) {
	pTrue := BoolPtr(true)
	pFalse := BoolPtr(false)
	if pTrue == nil || !*pTrue {
		t.Error("BoolPtr(true) should return pointer to true")
	}
	if pFalse == nil || *pFalse {
		t.Error("BoolPtr(false) should return pointer to false")
	}
}

func TestModel_JSONFields(t *testing.T) {
	m := Model{
		ID:         "gpt-4",
		Name:       "GPT-4",
		Provider:   "openai",
		ContextLen: 128000,
		InputCost:  0.00003,
		OutputCost: 0.00006,
		MaxTokens:  4096,
		Available:  true,
	}
	if m.ID != "gpt-4" {
		t.Errorf("unexpected ID: %s", m.ID)
	}
	if !m.Available {
		t.Error("expected Available to be true")
	}
	if m.ContextLen != 128000 {
		t.Errorf("unexpected ContextLen: %d", m.ContextLen)
	}
}

func TestCompletionRequest_Extensions(t *testing.T) {
	req := CompletionRequest{
		Model: "test-model",
		Messages: []Message{
			{Role: "user", Content: "hello"},
		},
		Extensions: &ProviderExtensions{
			Reasoning: &ReasoningConfig{Effort: "high"},
			Seed:      IntPtr(42),
			ProviderRouting: &ProviderRouting{
				Only:           []string{"openai"},
				AllowFallbacks: BoolPtr(false),
			},
		},
	}
	if req.Extensions == nil {
		t.Fatal("expected Extensions to be set")
	}
	if req.Extensions.Reasoning.Effort != "high" {
		t.Errorf("expected reasoning effort 'high', got %q", req.Extensions.Reasoning.Effort)
	}
	if *req.Extensions.Seed != 42 {
		t.Errorf("expected seed 42, got %d", *req.Extensions.Seed)
	}
	if req.Extensions.ProviderRouting.Only[0] != "openai" {
		t.Errorf("unexpected provider routing")
	}
}

func TestUsage_NilCost(t *testing.T) {
	u := Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}
	if u.Cost != nil {
		t.Error("expected nil cost when not set")
	}

	cost := 0.005
	u.Cost = &cost
	if *u.Cost != 0.005 {
		t.Errorf("expected cost 0.005, got %f", *u.Cost)
	}
}

func TestProviderConfig_Fields(t *testing.T) {
	cfg := ProviderConfig{
		Name:   "openrouter",
		APIKey: "sk-test",
		Metadata: map[string]string{
			"env": "test",
		},
	}
	if cfg.Name != "openrouter" {
		t.Errorf("unexpected name: %s", cfg.Name)
	}
	if cfg.Metadata["env"] != "test" {
		t.Error("unexpected metadata")
	}
}
