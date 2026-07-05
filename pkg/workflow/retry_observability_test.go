package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/alhasaniq/consortium/pkg/providers"
)

func TestBuildErrorObservabilityMetadata_Nil(t *testing.T) {
	meta := BuildErrorObservabilityMetadata(nil)
	if meta != nil {
		t.Error("nil error should return nil metadata")
	}
}

func TestBuildErrorObservabilityMetadata_ProviderError(t *testing.T) {
	err := &providers.ProviderError{
		Code:         providers.ErrCodeRateLimited,
		StatusCode:   429,
		Message:      "rate limited",
		Provider:     "openrouter",
		RequestID:    "req-123",
		GenerationID: "gen-123",
		ErrorType:    "provider_timeout",
		ProviderCode: "upstream_504",
		NativeCode:   "rate_limit_exceeded",
		ErrorPhase:   "http_status",
	}
	meta := BuildErrorObservabilityMetadata(err)
	if meta == nil {
		t.Fatal("expected non-nil metadata")
	}
	if meta["retry_layer"] != "provider" {
		t.Errorf("expected retry_layer=provider, got %v", meta["retry_layer"])
	}
	if meta["error_phase"] != "http_status" {
		t.Errorf("expected error_phase=http_status, got %v", meta["error_phase"])
	}
	if meta["provider"] != "openrouter" {
		t.Errorf("expected provider=openrouter, got %v", meta["provider"])
	}
	if meta["provider_error_code"] != providers.ErrCodeRateLimited {
		t.Errorf("expected provider_error_code=RATE_LIMITED, got %v", meta["provider_error_code"])
	}
	if meta["provider_status_code"] != 429 {
		t.Errorf("expected provider_status_code=429, got %v", meta["provider_status_code"])
	}
	if meta["provider_request_id"] != "req-123" {
		t.Errorf("expected provider_request_id=req-123, got %v", meta["provider_request_id"])
	}
	if meta["provider_generation_id"] != "gen-123" {
		t.Errorf("expected provider_generation_id=gen-123, got %v", meta["provider_generation_id"])
	}
	if meta["provider_error_type"] != "provider_timeout" {
		t.Errorf("expected provider_error_type=provider_timeout, got %v", meta["provider_error_type"])
	}
	if meta["provider_code"] != "upstream_504" {
		t.Errorf("expected provider_code=upstream_504, got %v", meta["provider_code"])
	}
	if meta["provider_native_code"] != "rate_limit_exceeded" {
		t.Errorf("expected provider_native_code=rate_limit_exceeded, got %v", meta["provider_native_code"])
	}
}

func TestBuildErrorObservabilityMetadata_WrappedProviderError(t *testing.T) {
	inner := &providers.ProviderError{
		Code:     providers.ErrCodeUpstreamError,
		Provider: "openrouter",
	}
	err := fmt.Errorf("wrapped: %w", inner)
	meta := BuildErrorObservabilityMetadata(err)
	if meta == nil {
		t.Fatal("expected non-nil metadata for wrapped provider error")
	}
	if meta["retry_layer"] != "provider" {
		t.Errorf("expected retry_layer=provider, got %v", meta["retry_layer"])
	}
}

func TestBuildErrorObservabilityMetadata_DeadlineExceeded(t *testing.T) {
	meta := BuildErrorObservabilityMetadata(context.DeadlineExceeded)
	if meta == nil {
		t.Fatal("expected non-nil metadata")
	}
	if meta["retry_layer"] != "node_runtime" {
		t.Errorf("expected retry_layer=node_runtime, got %v", meta["retry_layer"])
	}
	if meta["error_phase"] != "node_timeout" {
		t.Errorf("expected error_phase=node_timeout, got %v", meta["error_phase"])
	}
}

func TestBuildErrorObservabilityMetadata_Canceled(t *testing.T) {
	meta := BuildErrorObservabilityMetadata(context.Canceled)
	if meta == nil {
		t.Fatal("expected non-nil metadata")
	}
	if meta["retry_layer"] != "node_runtime" {
		t.Errorf("expected retry_layer=node_runtime, got %v", meta["retry_layer"])
	}
	if meta["error_phase"] != "node_cancelled" {
		t.Errorf("expected error_phase=node_cancelled, got %v", meta["error_phase"])
	}
}

func TestBuildErrorObservabilityMetadata_NetworkStrings(t *testing.T) {
	tests := []struct {
		msg   string
		phase string
	}{
		{"connection reset by peer", "transport_read"},
		{"broken pipe", "transport_read"},
		{"failed to read response body", "transport_read"},
		{"unexpected EOF", "transport_read"},
		{"request timeout exceeded", "transport_timeout"},
		{"operation timed out", "transport_timeout"},
	}
	for _, tc := range tests {
		t.Run(tc.msg, func(t *testing.T) {
			meta := BuildErrorObservabilityMetadata(errors.New(tc.msg))
			if meta == nil {
				t.Fatal("expected non-nil metadata")
			}
			if meta["retry_layer"] != "network" {
				t.Errorf("expected retry_layer=network, got %v", meta["retry_layer"])
			}
			if meta["error_phase"] != tc.phase {
				t.Errorf("expected error_phase=%s, got %v", tc.phase, meta["error_phase"])
			}
		})
	}
}

func TestBuildErrorObservabilityMetadata_GenericError(t *testing.T) {
	meta := BuildErrorObservabilityMetadata(errors.New("some random error"))
	if meta == nil {
		t.Fatal("expected non-nil metadata")
	}
	if meta["retry_layer"] != "runtime" {
		t.Errorf("expected retry_layer=runtime, got %v", meta["retry_layer"])
	}
	if meta["error_phase"] != "execution" {
		t.Errorf("expected error_phase=execution, got %v", meta["error_phase"])
	}
}

func TestBuildCallObservabilityMetadata(t *testing.T) {
	meta := BuildCallObservabilityMetadata("openrouter", "resp-1", "req-1")
	if meta == nil {
		t.Fatal("expected non-nil metadata")
	}
	if meta["provider"] != "openrouter" {
		t.Errorf("expected provider=openrouter, got %v", meta["provider"])
	}
	if meta["provider_response_id"] != "resp-1" {
		t.Errorf("expected provider_response_id=resp-1, got %v", meta["provider_response_id"])
	}
	if meta["provider_request_id"] != "req-1" {
		t.Errorf("expected provider_request_id=req-1, got %v", meta["provider_request_id"])
	}
}

func TestBuildCallObservabilityMetadata_AllEmpty(t *testing.T) {
	meta := BuildCallObservabilityMetadata("", "", "")
	if meta != nil {
		t.Error("all-empty inputs should return nil")
	}
}

func TestBuildCallObservabilityMetadata_Partial(t *testing.T) {
	meta := BuildCallObservabilityMetadata("openai", "", "")
	if meta == nil {
		t.Fatal("expected non-nil metadata with provider set")
	}
	if meta["provider"] != "openai" {
		t.Errorf("expected provider=openai, got %v", meta["provider"])
	}
	if _, ok := meta["provider_request_id"]; ok {
		t.Error("empty request ID should not be in metadata")
	}
}

func TestBuildProviderResponseObservabilityMetadata(t *testing.T) {
	byok := true
	resp := &providers.CompletionResponse{
		ID:                 "resp-1",
		RequestID:          "req-1",
		GenerationID:       "gen-1",
		ServiceTier:        "default",
		OpenRouterMetadata: []byte(`{"provider_name":"openai"}`),
		Reasoning:          "short rationale",
		Usage: providers.Usage{
			PromptTokensDetails: &providers.PromptTokensDetails{
				CachedTokens:     4,
				CacheWriteTokens: 2,
			},
			CompletionTokensDetails: &providers.CompletionTokensDetails{
				ReasoningTokens: 3,
			},
			IsBYOK: &byok,
		},
	}

	meta := BuildProviderResponseObservabilityMetadata("openrouter", resp)
	if meta["provider_generation_id"] != "gen-1" {
		t.Fatalf("provider_generation_id = %v", meta["provider_generation_id"])
	}
	if meta["openrouter_service_tier"] != "default" {
		t.Fatalf("service tier = %v", meta["openrouter_service_tier"])
	}
	if meta["openrouter_cached_tokens"] != 4 || meta["openrouter_cache_write_tokens"] != 2 {
		t.Fatalf("cache token metadata = %#v", meta)
	}
	if meta["openrouter_reasoning_tokens"] != 3 {
		t.Fatalf("reasoning metadata = %#v", meta)
	}
	if _, ok := meta["openrouter_reasoning"]; ok {
		t.Fatalf("raw reasoning text should not be persisted in metadata: %#v", meta)
	}
	if meta["openrouter_is_byok"] != true {
		t.Fatalf("is_byok metadata = %#v", meta["openrouter_is_byok"])
	}
	if metadata, ok := meta["openrouter_metadata"].(map[string]interface{}); !ok || metadata["provider_name"] != "openai" {
		t.Fatalf("openrouter_metadata = %#v", meta["openrouter_metadata"])
	}
}

func TestBuildProviderResponseObservabilityMetadataCapsOpenRouterMetadata(t *testing.T) {
	resp := &providers.CompletionResponse{
		ID:                 "resp-1",
		OpenRouterMetadata: []byte(`{"provider_name":"openai","payload":"` + strings.Repeat("x", 20*1024) + `"}`),
	}

	meta := BuildProviderResponseObservabilityMetadata("openrouter", resp)
	if _, ok := meta["openrouter_metadata"]; ok {
		t.Fatalf("oversized openrouter_metadata should not be persisted: %#v", meta["openrouter_metadata"])
	}
	if meta["openrouter_metadata_truncated"] != true {
		t.Fatalf("openrouter_metadata_truncated = %#v, want true in %#v", meta["openrouter_metadata_truncated"], meta)
	}
	if bytes, ok := meta["openrouter_metadata_bytes"].(int); !ok || bytes <= 16*1024 {
		t.Fatalf("openrouter_metadata_bytes = %#v, want original byte length", meta["openrouter_metadata_bytes"])
	}
}

func TestMergeMetadata(t *testing.T) {
	base := map[string]interface{}{"a": 1, "b": 2}
	extra := map[string]interface{}{"b": 3, "c": 4}
	merged := MergeMetadata(base, extra)
	if merged["a"] != 1 {
		t.Errorf("expected a=1, got %v", merged["a"])
	}
	if merged["b"] != 3 {
		t.Errorf("expected b=3 (overridden), got %v", merged["b"])
	}
	if merged["c"] != 4 {
		t.Errorf("expected c=4, got %v", merged["c"])
	}
}

func TestMergeMetadata_BothEmpty(t *testing.T) {
	merged := MergeMetadata(nil, nil)
	if merged != nil {
		t.Error("merging two nils should return nil")
	}
}

func TestMergeMetadata_OneEmpty(t *testing.T) {
	base := map[string]interface{}{"a": 1}
	merged := MergeMetadata(base, nil)
	if merged == nil || merged["a"] != 1 {
		t.Error("expected base data preserved")
	}
}

func TestSelectAttemptObservabilityMetadata(t *testing.T) {
	outputMeta := map[string]interface{}{
		"retry_layer":                 "provider",
		"error_phase":                 "http_status",
		"provider":                    "openrouter",
		"provider_generation_id":      "gen-1",
		"provider_error_type":         "provider_timeout",
		"provider_native_code":        "provider_error",
		"provider_code":               "upstream_504",
		"openrouter_cached_tokens":    4,
		"openrouter_reasoning_tokens": 3,
		"unrelated_key":               "should be excluded",
		"agreement_ratio":             0.75,
		"consensus_answer":            "B",
	}
	meta := SelectAttemptObservabilityMetadata(outputMeta)
	if meta == nil {
		t.Fatal("expected non-nil metadata")
	}
	if meta["retry_layer"] != "provider" {
		t.Errorf("expected retry_layer=provider, got %v", meta["retry_layer"])
	}
	if _, ok := meta["unrelated_key"]; ok {
		t.Error("unrelated_key should not be selected")
	}
	if meta["agreement_ratio"] != 0.75 {
		t.Errorf("expected agreement_ratio=0.75, got %v", meta["agreement_ratio"])
	}
	if meta["provider_generation_id"] != "gen-1" || meta["provider_error_type"] != "provider_timeout" || meta["provider_native_code"] != "provider_error" || meta["provider_code"] != "upstream_504" {
		t.Errorf("provider attempt metadata not selected: %#v", meta)
	}
	if meta["openrouter_cached_tokens"] != 4 || meta["openrouter_reasoning_tokens"] != 3 {
		t.Errorf("OpenRouter attempt metadata not selected: %#v", meta)
	}
}

func TestSelectAttemptObservabilityMetadata_Empty(t *testing.T) {
	meta := SelectAttemptObservabilityMetadata(nil)
	if meta != nil {
		t.Error("nil input should return nil")
	}
}

func TestSelectAttemptObservabilityMetadata_NoMatchingKeys(t *testing.T) {
	outputMeta := map[string]interface{}{
		"foo": "bar",
		"baz": 42,
	}
	meta := SelectAttemptObservabilityMetadata(outputMeta)
	if meta != nil {
		t.Error("no matching keys should return nil")
	}
}
