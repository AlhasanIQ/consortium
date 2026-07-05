package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"

	"github.com/alhasaniq/consortium/pkg/providers"
)

const maxOpenRouterMetadataBytes = 16 * 1024

var attemptObservabilityKeys = []string{
	"retry_layer",
	"error_phase",
	"provider",
	"provider_error_code",
	"provider_status_code",
	"provider_request_id",
	"provider_response_id",
	"provider_generation_id",
	"provider_error_type",
	"provider_native_code",
	"provider_code",
	"openrouter_service_tier",
	"openrouter_cached_tokens",
	"openrouter_cache_write_tokens",
	"openrouter_reasoning_tokens",
	"openrouter_is_byok",
	"openrouter_metadata_truncated",
	"openrouter_metadata_bytes",
	"scoring_subcall_attempts",
	"scoring_subcall_max_attempts",
	"scoring_subcall_agent",
	"agreement_ratio",
	"consensus_answer",
	"dissenting_ids",
}

// BuildErrorObservabilityMetadata extracts retry/error telemetry from errors so
// attempt rows can expose where failures happened.
func BuildErrorObservabilityMetadata(err error) map[string]interface{} {
	if err == nil {
		return nil
	}

	meta := make(map[string]interface{})
	if pe, ok := providers.AsProviderError(err); ok {
		meta["retry_layer"] = "provider"
		if pe.ErrorPhase != "" {
			meta["error_phase"] = pe.ErrorPhase
		}
		if pe.Provider != "" {
			meta["provider"] = pe.Provider
		}
		if pe.Code != "" {
			meta["provider_error_code"] = pe.Code
		}
		if pe.StatusCode > 0 {
			meta["provider_status_code"] = pe.StatusCode
		}
		if pe.RequestID != "" {
			meta["provider_request_id"] = pe.RequestID
		}
		if pe.GenerationID != "" {
			meta["provider_generation_id"] = pe.GenerationID
		}
		if pe.ErrorType != "" {
			meta["provider_error_type"] = pe.ErrorType
		}
		if pe.NativeCode != "" {
			meta["provider_native_code"] = pe.NativeCode
		}
		if pe.ProviderCode != "" {
			meta["provider_code"] = pe.ProviderCode
		}
		return meta
	}

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		meta["retry_layer"] = "node_runtime"
		meta["error_phase"] = "node_timeout"
		return meta
	case errors.Is(err, context.Canceled):
		meta["retry_layer"] = "node_runtime"
		meta["error_phase"] = "node_cancelled"
		return meta
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		meta["retry_layer"] = "network"
		meta["error_phase"] = "transport_timeout"
		return meta
	}

	lower := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(lower, "connection reset"),
		strings.Contains(lower, "broken pipe"),
		strings.Contains(lower, "failed to read response"),
		strings.Contains(lower, "unexpected eof"):
		meta["retry_layer"] = "network"
		meta["error_phase"] = "transport_read"
	case strings.Contains(lower, "timeout"),
		strings.Contains(lower, "timed out"):
		meta["retry_layer"] = "network"
		meta["error_phase"] = "transport_timeout"
	default:
		meta["retry_layer"] = "runtime"
		meta["error_phase"] = "execution"
	}
	return meta
}

// BuildCallObservabilityMetadata captures request/response correlation IDs on
// successful provider calls for per-attempt tracing.
func BuildCallObservabilityMetadata(providerName, responseID, requestID string) map[string]interface{} {
	meta := make(map[string]interface{})
	if providerName != "" {
		meta["provider"] = providerName
	}
	if requestID != "" {
		meta["provider_request_id"] = requestID
	}
	if responseID != "" {
		meta["provider_response_id"] = responseID
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

func BuildProviderResponseObservabilityMetadata(providerName string, resp *providers.CompletionResponse) map[string]interface{} {
	if resp == nil {
		return nil
	}
	meta := BuildCallObservabilityMetadata(providerName, resp.ID, resp.RequestID)
	if meta == nil {
		meta = make(map[string]interface{})
	}
	if resp.GenerationID != "" {
		meta["provider_generation_id"] = resp.GenerationID
	}
	if resp.ServiceTier != "" {
		meta["openrouter_service_tier"] = resp.ServiceTier
	}
	if resp.Usage.PromptTokensDetails != nil {
		if resp.Usage.PromptTokensDetails.CachedTokens > 0 {
			meta["openrouter_cached_tokens"] = resp.Usage.PromptTokensDetails.CachedTokens
		}
		if resp.Usage.PromptTokensDetails.CacheWriteTokens > 0 {
			meta["openrouter_cache_write_tokens"] = resp.Usage.PromptTokensDetails.CacheWriteTokens
		}
	}
	if resp.Usage.CompletionTokensDetails != nil && resp.Usage.CompletionTokensDetails.ReasoningTokens > 0 {
		meta["openrouter_reasoning_tokens"] = resp.Usage.CompletionTokensDetails.ReasoningTokens
	}
	if resp.Usage.IsBYOK != nil {
		meta["openrouter_is_byok"] = *resp.Usage.IsBYOK
	}
	// TODO(v0.1-security): Raw provider metadata can contain provider-specific
	// routing or account details. Keep it opt-in and add an allowlist/redaction
	// pass before enabling it for shared or customer-visible deployments.
	if len(resp.OpenRouterMetadata) > maxOpenRouterMetadataBytes {
		meta["openrouter_metadata_truncated"] = true
		meta["openrouter_metadata_bytes"] = len(resp.OpenRouterMetadata)
	} else if decoded, ok := decodeProviderRawJSON(resp.OpenRouterMetadata); ok {
		meta["openrouter_metadata"] = decoded
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}

func decodeProviderRawJSON(raw json.RawMessage) (interface{}, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var decoded interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, false
	}
	return decoded, true
}

// MergeMetadata returns a shallow merged map of base+extra.
func MergeMetadata(base map[string]interface{}, extra map[string]interface{}) map[string]interface{} {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// SelectAttemptObservabilityMetadata copies retry/error observability keys from
// a node output metadata map into attempt metadata.
func SelectAttemptObservabilityMetadata(outputMeta map[string]interface{}) map[string]interface{} {
	if len(outputMeta) == 0 {
		return nil
	}
	meta := make(map[string]interface{})
	for _, key := range attemptObservabilityKeys {
		if value, ok := outputMeta[key]; ok {
			meta[key] = value
		}
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
}
