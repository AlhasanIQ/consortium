package workflow

import "testing"

func TestParseOpenRouterProviderConfig_SnakeCase(t *testing.T) {
	cfg := map[string]interface{}{
		"openrouter_provider": map[string]interface{}{
			"only":            []interface{}{"OpenAI"},
			"allow_fallbacks": false,
		},
	}

	routing := parseOpenRouterProviderConfig(cfg)
	if routing == nil {
		t.Fatal("expected routing to be parsed")
	}
	if len(routing.Only) != 1 || routing.Only[0] != "OpenAI" {
		t.Fatalf("expected only=[OpenAI], got %+v", routing.Only)
	}
	if routing.AllowFallbacks == nil || *routing.AllowFallbacks {
		t.Fatalf("expected allow_fallbacks=false, got %+v", routing.AllowFallbacks)
	}
}

func TestParseOpenRouterProviderConfig_CamelCase(t *testing.T) {
	cfg := map[string]interface{}{
		"openRouterProvider": map[string]interface{}{
			"order": []interface{}{"Anthropic"},
		},
	}

	routing := parseOpenRouterProviderConfig(cfg)
	if routing == nil {
		t.Fatal("expected routing to be parsed")
	}
	if len(routing.Order) != 1 || routing.Order[0] != "Anthropic" {
		t.Fatalf("expected order=[Anthropic], got %+v", routing.Order)
	}
}

func TestParseOpenRouterProviderConfig_Empty(t *testing.T) {
	cfg := map[string]interface{}{
		"openrouter_provider": map[string]interface{}{},
	}

	if routing := parseOpenRouterProviderConfig(cfg); routing != nil {
		t.Fatalf("expected nil routing for empty config, got %+v", routing)
	}
}

func TestParseOpenRouterProviderConfig_NewFieldsAreNotDropped(t *testing.T) {
	cfg := map[string]interface{}{
		"openrouter_provider": map[string]interface{}{
			"max_price": map[string]interface{}{
				"prompt":     0.1,
				"completion": "0.2",
			},
			"zdr":             true,
			"data_collection": "deny",
		},
	}

	routing := parseOpenRouterProviderConfig(cfg)
	if routing == nil {
		t.Fatal("expected routing with only expanded fields to be parsed")
	}
	if routing.MaxPrice == nil || routing.MaxPrice.Prompt != 0.1 || routing.MaxPrice.Completion != "0.2" {
		t.Fatalf("max_price = %+v", routing.MaxPrice)
	}
	if routing.ZDR == nil || *routing.ZDR != true {
		t.Fatalf("zdr = %+v", routing.ZDR)
	}
	if routing.DataCollection != "deny" {
		t.Fatalf("data_collection = %q", routing.DataCollection)
	}
}
