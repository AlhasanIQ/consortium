package workflow

import (
	"encoding/json"
	"math"

	"github.com/alhasaniq/consortium/pkg/providers"
)

func aggregationTemperature(config map[string]interface{}) (*float64, bool) {
	if config != nil {
		if raw, ok := config["temperature"]; ok {
			if value, ok := numericToFloat64(raw); ok {
				return providers.Float64Ptr(value), true
			}
		}
	}
	return nil, false
}

// aggregationMaxTokens returns the configured max_tokens value and whether it was found.
// Valid values are -1 (unlimited) or > 0. The caller (validator) ensures this is set.
func aggregationMaxTokens(config map[string]interface{}) (int, bool) {
	if config != nil {
		for _, key := range []string{"max_tokens", "maxTokens"} {
			if raw, ok := config[key]; ok {
				if value, ok := numericToInt(raw); ok && (value == -1 || value > 0) {
					return value, true
				}
			}
		}
	}
	return 0, false
}

// aggregationRepairMaxTokens returns the configured repair_max_tokens value and whether it was found.
// Only required for aggregation methods that have repair calls (judge, debate_decide).
func aggregationRepairMaxTokens(config map[string]interface{}) (int, bool) {
	if config != nil {
		for _, key := range []string{"repair_max_tokens", "repairMaxTokens"} {
			if raw, ok := config[key]; ok {
				if value, ok := numericToInt(raw); ok && value > 0 {
					return value, true
				}
			}
		}
	}
	return 0, false
}

func numericToFloat64(raw interface{}) (float64, bool) {
	switch v := raw.(type) {
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, false
		}
		return v, true
	case float32:
		value := float64(v)
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return 0, false
		}
		return value, true
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case json.Number:
		parsed, err := v.Float64()
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func numericToInt(raw interface{}) (int, bool) {
	switch v := raw.(type) {
	case int:
		return v, true
	case int8:
		return int(v), true
	case int16:
		return int(v), true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case uint:
		return int(v), true
	case uint8:
		return int(v), true
	case uint16:
		return int(v), true
	case uint32:
		return int(v), true
	case uint64:
		return int(v), true
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, false
		}
		return int(v), true
	case float32:
		value := float64(v)
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return 0, false
		}
		return int(value), true
	case json.Number:
		if parsedInt, err := v.Int64(); err == nil {
			return int(parsedInt), true
		}
		parsedFloat, err := v.Float64()
		if err != nil || math.IsNaN(parsedFloat) || math.IsInf(parsedFloat, 0) {
			return 0, false
		}
		return int(parsedFloat), true
	default:
		return 0, false
	}
}

// MergeAggregationConfig combines result-level OpenRouter config with explicit
// aggregation config, preserving explicit aggregation keys when both are set.
// It only normalizes fields that were provided; builder defaults and validator
// checks remain responsible for required method-specific aggregation settings.
func MergeAggregationConfig(
	base map[string]interface{},
	openRouterProvider map[string]interface{},
	openRouterReasoning map[string]interface{},
) map[string]interface{} {
	if len(base) == 0 && len(openRouterProvider) == 0 && len(openRouterReasoning) == 0 {
		return nil
	}

	merged := make(map[string]interface{}, len(base)+2)
	for key, value := range base {
		merged[key] = value
	}
	if tieBreaker, ok := merged["tie_breaker"]; ok {
		if _, exists := merged["tie_breaker_method"]; !exists {
			merged["tie_breaker_method"] = tieBreaker
		}
		delete(merged, "tie_breaker")
	}

	if len(openRouterProvider) > 0 && !hasAnyAggregationConfigKey(merged, openRouterProviderConfigKeys...) {
		merged["openRouterProvider"] = openRouterProvider
	}
	if len(openRouterReasoning) > 0 && !hasAnyAggregationConfigKey(merged, openRouterReasoningConfigKeys...) {
		merged["openRouterReasoning"] = openRouterReasoning
	}

	return merged
}

func hasAnyAggregationConfigKey(config map[string]interface{}, keys ...string) bool {
	if len(config) == 0 {
		return false
	}
	for _, key := range keys {
		if _, exists := config[key]; exists {
			return true
		}
	}
	return false
}
