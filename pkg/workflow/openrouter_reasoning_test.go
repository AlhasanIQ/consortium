package workflow

import "testing"

func TestParseOpenRouterReasoningConfig_SnakeCase(t *testing.T) {
	cfg := map[string]interface{}{
		"openrouter_reasoning": map[string]interface{}{
			"effort": "high",
		},
	}

	reasoning := parseOpenRouterReasoningConfig(cfg)
	if reasoning == nil {
		t.Fatal("expected reasoning to be parsed")
	}
	if reasoning.Effort != "high" {
		t.Fatalf("expected effort=high, got %q", reasoning.Effort)
	}
}

func TestParseOpenRouterReasoningConfig_CamelCase(t *testing.T) {
	cfg := map[string]interface{}{
		"openRouterReasoning": map[string]interface{}{
			"effort": "Medium",
		},
	}

	reasoning := parseOpenRouterReasoningConfig(cfg)
	if reasoning == nil {
		t.Fatal("expected reasoning to be parsed")
	}
	if reasoning.Effort != "medium" {
		t.Fatalf("expected effort=medium, got %q", reasoning.Effort)
	}
}

func TestParseOpenRouterReasoningConfig_StringShorthand(t *testing.T) {
	cfg := map[string]interface{}{
		"openRouterReasoning": "low",
	}

	reasoning := parseOpenRouterReasoningConfig(cfg)
	if reasoning == nil {
		t.Fatal("expected reasoning to be parsed")
	}
	if reasoning.Effort != "low" {
		t.Fatalf("expected effort=low, got %q", reasoning.Effort)
	}
}

func TestParseOpenRouterReasoningConfig_DefaultsToNone(t *testing.T) {
	cfg := map[string]interface{}{
		"openrouter_reasoning": map[string]interface{}{},
	}

	reasoning := parseOpenRouterReasoningConfig(cfg)
	if reasoning == nil {
		t.Fatal("expected reasoning to be parsed")
	}
	if reasoning.Effort != "none" {
		t.Fatalf("expected effort=none, got %q", reasoning.Effort)
	}
}

func TestParseOpenRouterReasoningConfig_InvalidEffort(t *testing.T) {
	cfg := map[string]interface{}{
		"openrouter_reasoning": map[string]interface{}{
			"effort": "exclude",
		},
	}

	if reasoning := parseOpenRouterReasoningConfig(cfg); reasoning != nil {
		t.Fatalf("expected nil reasoning for invalid effort, got %+v", reasoning)
	}
}

func TestParseOpenRouterReasoningConfig_ExpandedFields(t *testing.T) {
	cfg := map[string]interface{}{
		"openrouter_reasoning": map[string]interface{}{
			"effort":     "minimal",
			"max_tokens": 256,
			"enabled":    true,
			"summary":    "auto",
		},
	}

	reasoning := parseOpenRouterReasoningConfig(cfg)
	if reasoning == nil {
		t.Fatal("expected expanded reasoning config to be parsed")
	}
	if reasoning.Effort != "minimal" {
		t.Fatalf("effort = %q, want minimal", reasoning.Effort)
	}
	if reasoning.MaxTokens == nil || *reasoning.MaxTokens != 256 {
		t.Fatalf("max_tokens = %+v", reasoning.MaxTokens)
	}
	if reasoning.Enabled == nil || *reasoning.Enabled != true {
		t.Fatalf("enabled = %+v", reasoning.Enabled)
	}
	if reasoning.Summary != "auto" {
		t.Fatalf("summary = %q", reasoning.Summary)
	}
}
