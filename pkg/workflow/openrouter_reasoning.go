package workflow

import (
	"encoding/json"
	"strings"

	"github.com/alhasaniq/consortium/pkg/providers"
)

var openRouterReasoningConfigKeys = []string{
	"openrouter_reasoning",
	"openRouterReasoning",
}

// parseOpenRouterReasoningConfig extracts OpenRouter reasoning config from a metadata/config map.
// Absence means provider default behavior (no explicit reasoning override).
func parseOpenRouterReasoningConfig(cfg map[string]interface{}) *providers.ReasoningConfig {
	if cfg == nil {
		return nil
	}
	for _, key := range openRouterReasoningConfigKeys {
		if reasoning := parseReasoningConfig(cfg[key]); reasoning != nil {
			return reasoning
		}
	}
	return nil
}

func parseReasoningConfig(raw interface{}) *providers.ReasoningConfig {
	if raw == nil {
		return nil
	}

	// Convenience form: "high"
	if effort, ok := raw.(string); ok {
		raw = map[string]interface{}{"effort": effort}
	}

	serialized, err := json.Marshal(raw)
	if err != nil {
		return nil
	}

	var reasoning providers.ReasoningConfig
	if err := json.Unmarshal(serialized, &reasoning); err != nil {
		return nil
	}

	effort := strings.ToLower(strings.TrimSpace(reasoning.Effort))
	if effort == "" {
		effort = "none"
	}
	switch effort {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max":
		reasoning.Effort = effort
		return &reasoning
	default:
		return nil
	}
}
