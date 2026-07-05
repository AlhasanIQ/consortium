package workflow

import (
	"encoding/json"

	"github.com/alhasaniq/consortium/pkg/providers"
)

var openRouterProviderConfigKeys = []string{
	"openrouter_provider",
	"openRouterProvider",
	"provider_routing",
	"providerRouting",
}

// parseOpenRouterProviderConfig extracts OpenRouter routing preferences from a config/metadata map.
func parseOpenRouterProviderConfig(cfg map[string]interface{}) *providers.ProviderRouting {
	if cfg == nil {
		return nil
	}
	for _, key := range openRouterProviderConfigKeys {
		if routing := parseProviderRouting(cfg[key]); routing != nil {
			return routing
		}
	}
	return nil
}

func parseProviderRouting(raw interface{}) *providers.ProviderRouting {
	if raw == nil {
		return nil
	}

	serialized, err := json.Marshal(raw)
	if err != nil {
		return nil
	}

	var routing providers.ProviderRouting
	if err := json.Unmarshal(serialized, &routing); err != nil {
		return nil
	}

	if len(routing.Order) == 0 &&
		len(routing.Only) == 0 &&
		len(routing.Ignore) == 0 &&
		routing.AllowFallbacks == nil &&
		routing.RequireParameters == nil &&
		routing.Sort == nil &&
		len(routing.Quantizations) == 0 &&
		routing.MaxPrice == nil &&
		routing.DataCollection == "" &&
		routing.ZDR == nil &&
		routing.PreferredMaxLatency == nil &&
		routing.PreferredMinThroughput == nil &&
		routing.EnforceDistillableText == nil {
		return nil
	}

	return &routing
}
