package benchloop

import "testing"

func TestCollectModelValuesFromJSON(t *testing.T) {
	raw := []byte(`{
  "nodes": [
    {"data": {"config": {"model": "a/model-1", "temperature": 0.2}}},
    {"data": {"config": {"aggregationConfig": {"judge_model": "b/judge-1"}}}}
  ]
}`)
	models, err := collectModelValuesFromJSON(raw)
	if err != nil {
		t.Fatalf("collect models: %v", err)
	}
	if got := models["$.nodes[0].data.config.model"]; got != "a/model-1" {
		t.Fatalf("unexpected model value: %q", got)
	}
	if got := models["$.nodes[1].data.config.aggregationConfig.judge_model"]; got != "b/judge-1" {
		t.Fatalf("unexpected judge_model value: %q", got)
	}
}

func TestCollectModelValuesFromJSON_IgnoresNonModelKeys(t *testing.T) {
	raw := []byte(`{"config": {"name": "x", "provider": "openrouter", "openRouterProvider": {"order": ["a", "b"]}}}`)
	models, err := collectModelValuesFromJSON(raw)
	if err != nil {
		t.Fatalf("collect models: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("expected no model keys, got %+v", models)
	}
}
