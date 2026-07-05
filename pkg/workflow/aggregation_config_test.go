package workflow

import (
	"encoding/json"
	"math"
	"testing"
)

func TestAggregationTemperature(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]interface{}
		want   float64
		found  bool
	}{
		{"nil config", nil, 0, false},
		{"empty config", map[string]interface{}{}, 0, false},
		{"float64 value", map[string]interface{}{"temperature": 0.7}, 0.7, true},
		{"int value", map[string]interface{}{"temperature": 1}, 1.0, true},
		{"NaN rejected", map[string]interface{}{"temperature": math.NaN()}, 0, false},
		{"Inf rejected", map[string]interface{}{"temperature": math.Inf(1)}, 0, false},
		{"string ignored", map[string]interface{}{"temperature": "hot"}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := aggregationTemperature(tt.config)
			if found != tt.found {
				t.Errorf("found = %v, want %v", found, tt.found)
			}
			if found && *got != tt.want {
				t.Errorf("value = %v, want %v", *got, tt.want)
			}
		})
	}
}

func TestAggregationMaxTokens(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]interface{}
		want   int
		found  bool
	}{
		{"nil config", nil, 0, false},
		{"empty config", map[string]interface{}{}, 0, false},
		{"max_tokens key", map[string]interface{}{"max_tokens": 1024}, 1024, true},
		{"maxTokens key", map[string]interface{}{"maxTokens": 512}, 512, true},
		{"unlimited (-1)", map[string]interface{}{"max_tokens": -1}, -1, true},
		{"zero rejected", map[string]interface{}{"max_tokens": 0}, 0, false},
		{"negative rejected", map[string]interface{}{"max_tokens": -2}, 0, false},
		{"float64 value", map[string]interface{}{"max_tokens": float64(256)}, 256, true},
		{"json.Number", map[string]interface{}{"max_tokens": json.Number("2048")}, 2048, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := aggregationMaxTokens(tt.config)
			if found != tt.found {
				t.Errorf("found = %v, want %v", found, tt.found)
			}
			if got != tt.want {
				t.Errorf("value = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestAggregationRepairMaxTokens(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]interface{}
		want   int
		found  bool
	}{
		{"nil config", nil, 0, false},
		{"repair_max_tokens key", map[string]interface{}{"repair_max_tokens": 256}, 256, true},
		{"repairMaxTokens key", map[string]interface{}{"repairMaxTokens": 128}, 128, true},
		{"zero rejected", map[string]interface{}{"repair_max_tokens": 0}, 0, false},
		{"negative rejected", map[string]interface{}{"repair_max_tokens": -1}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := aggregationRepairMaxTokens(tt.config)
			if found != tt.found {
				t.Errorf("found = %v, want %v", found, tt.found)
			}
			if got != tt.want {
				t.Errorf("value = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestNumericToFloat64(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
		want  float64
		ok    bool
	}{
		{"float64", float64(3.14), 3.14, true},
		{"float32", float32(2.5), 2.5, true},
		{"int", 42, 42.0, true},
		{"int64", int64(100), 100.0, true},
		{"uint", uint(10), 10.0, true},
		{"json.Number", json.Number("1.5"), 1.5, true},
		{"json.Number invalid", json.Number("abc"), 0, false},
		{"NaN", math.NaN(), 0, false},
		{"Inf", math.Inf(1), 0, false},
		{"-Inf", math.Inf(-1), 0, false},
		{"string", "hello", 0, false},
		{"nil", nil, 0, false},
		{"bool", true, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := numericToFloat64(tt.input)
			if ok != tt.ok {
				t.Errorf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("value = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNumericToInt(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
		want  int
		ok    bool
	}{
		{"int", 42, 42, true},
		{"int32", int32(10), 10, true},
		{"int64", int64(100), 100, true},
		{"uint", uint(5), 5, true},
		{"float64", float64(7.9), 7, true},
		{"float64 NaN", math.NaN(), 0, false},
		{"float64 Inf", math.Inf(1), 0, false},
		{"json.Number int", json.Number("42"), 42, true},
		{"json.Number float", json.Number("3.14"), 3, true},
		{"json.Number invalid", json.Number("abc"), 0, false},
		{"string", "hello", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := numericToInt(tt.input)
			if ok != tt.ok {
				t.Errorf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("value = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMergeAggregationConfig(t *testing.T) {
	t.Run("all nil", func(t *testing.T) {
		result := MergeAggregationConfig(nil, nil, nil)
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})

	t.Run("base only", func(t *testing.T) {
		base := map[string]interface{}{"model": "gpt-4o"}
		result := MergeAggregationConfig(base, nil, nil)
		if result["model"] != "gpt-4o" {
			t.Errorf("expected model gpt-4o, got %v", result["model"])
		}
	})

	t.Run("normalizes legacy majority vote tie breaker", func(t *testing.T) {
		base := map[string]interface{}{"tie_breaker": "first"}
		result := MergeAggregationConfig(base, nil, nil)
		if result["tie_breaker_method"] != "first" {
			t.Errorf("expected tie_breaker_method first, got %v", result["tie_breaker_method"])
		}
		if _, exists := result["tie_breaker"]; exists {
			t.Error("expected legacy tie_breaker key to be removed")
		}
	})

	t.Run("preserves explicit majority vote tie breaker method", func(t *testing.T) {
		base := map[string]interface{}{"tie_breaker": "first", "tie_breaker_method": "error"}
		result := MergeAggregationConfig(base, nil, nil)
		if result["tie_breaker_method"] != "error" {
			t.Errorf("expected tie_breaker_method error, got %v", result["tie_breaker_method"])
		}
		if _, exists := result["tie_breaker"]; exists {
			t.Error("expected legacy tie_breaker key to be removed")
		}
	})

	t.Run("merges provider when not present", func(t *testing.T) {
		base := map[string]interface{}{"model": "gpt-4o"}
		provider := map[string]interface{}{"order": []string{"Azure"}}
		result := MergeAggregationConfig(base, provider, nil)
		if result["openRouterProvider"] == nil {
			t.Error("expected openRouterProvider to be merged")
		}
	})

	t.Run("skips provider when base has provider key", func(t *testing.T) {
		base := map[string]interface{}{"openRouterProvider": map[string]interface{}{"order": []string{"GCP"}}}
		provider := map[string]interface{}{"order": []string{"Azure"}}
		result := MergeAggregationConfig(base, provider, nil)
		existing := result["openRouterProvider"].(map[string]interface{})
		if existing["order"].([]string)[0] != "GCP" {
			t.Error("expected original provider config preserved")
		}
	})

	t.Run("merges reasoning when not present", func(t *testing.T) {
		base := map[string]interface{}{"model": "gpt-4o"}
		reasoning := map[string]interface{}{"effort": "high"}
		result := MergeAggregationConfig(base, nil, reasoning)
		if result["openRouterReasoning"] == nil {
			t.Error("expected openRouterReasoning to be merged")
		}
	})

	t.Run("skips reasoning when base has reasoning key", func(t *testing.T) {
		base := map[string]interface{}{"openRouterReasoning": map[string]interface{}{"effort": "low"}}
		reasoning := map[string]interface{}{"effort": "high"}
		result := MergeAggregationConfig(base, nil, reasoning)
		existing := result["openRouterReasoning"].(map[string]interface{})
		if existing["effort"] != "low" {
			t.Error("expected original reasoning config preserved")
		}
	})
}

func TestHasAnyAggregationConfigKey(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]interface{}
		keys   []string
		want   bool
	}{
		{"nil config", nil, []string{"a"}, false},
		{"empty config", map[string]interface{}{}, []string{"a"}, false},
		{"key present", map[string]interface{}{"a": 1}, []string{"a", "b"}, true},
		{"key absent", map[string]interface{}{"c": 1}, []string{"a", "b"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasAnyAggregationConfigKey(tt.config, tt.keys...)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
