package providers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestProvider creates an OpenRouterProvider pointed at a test server.
func newTestProvider(serverURL string) *OpenRouterProvider {
	return NewOpenRouterProvider(ProviderConfig{
		APIKey:  "test-key",
		BaseURL: serverURL,
		Timeout: 10 * time.Second,
	})
}

func TestStaticModelsIncludesDeepSeekV4FlashDefault(t *testing.T) {
	for _, model := range staticModels() {
		if model.ID != "deepseek/deepseek-v4-flash" {
			continue
		}
		if model.Name != "DeepSeek V4 Flash" {
			t.Fatalf("Name = %q, want DeepSeek V4 Flash", model.Name)
		}
		if model.Provider != "openrouter" {
			t.Fatalf("Provider = %q, want openrouter", model.Provider)
		}
		if model.ContextLen != 1000000 {
			t.Fatalf("ContextLen = %d, want 1000000", model.ContextLen)
		}
		if model.InputCost != 0.09/1000000 {
			t.Fatalf("InputCost = %v, want %v", model.InputCost, 0.09/1000000)
		}
		if model.OutputCost != 0.18/1000000 {
			t.Fatalf("OutputCost = %v, want %v", model.OutputCost, 0.18/1000000)
		}
		if !model.Available {
			t.Fatal("expected model to be available")
		}
		return
	}
	t.Fatal("static models missing deepseek/deepseek-v4-flash")
}

func TestProviderError_HTTP402(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "no credits left"},
		})
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	_, err := p.Complete(context.Background(), &CompletionRequest{
		Model:    "openai/gpt-4o-mini",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 402")
	}

	provErr, ok := AsProviderError(err)
	if !ok {
		t.Fatalf("expected ProviderError, got %T: %v", err, err)
	}
	if provErr.Code != ErrCodeInsufficientCredits {
		t.Errorf("expected code %q, got %q", ErrCodeInsufficientCredits, provErr.Code)
	}
	if provErr.Retryable {
		t.Error("402 should not be retryable")
	}
	if provErr.StatusCode != 402 {
		t.Errorf("expected status 402, got %d", provErr.StatusCode)
	}
}

func TestProviderError_HTTP401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "invalid api key"},
		})
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	_, err := p.Complete(context.Background(), &CompletionRequest{
		Model:    "openai/gpt-4o-mini",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})

	provErr, ok := AsProviderError(err)
	if !ok {
		t.Fatalf("expected ProviderError, got %T: %v", err, err)
	}
	if provErr.Code != ErrCodeAuthError {
		t.Errorf("expected code %q, got %q", ErrCodeAuthError, provErr.Code)
	}
	if provErr.Retryable {
		t.Error("401 should not be retryable")
	}
}

func TestProviderError_HTTP429_SingleAttempt(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.Header().Set("Retry-After", "0") // Don't actually wait in tests
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "rate limited"},
		})
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	_, err := p.Complete(context.Background(), &CompletionRequest{
		Model:    "openai/gpt-4o-mini",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})

	provErr, ok := AsProviderError(err)
	if !ok {
		t.Fatalf("expected ProviderError, got %T: %v", err, err)
	}
	if provErr.Code != ErrCodeRateLimited {
		t.Errorf("expected code %q, got %q", ErrCodeRateLimited, provErr.Code)
	}
	if attempts != 1 {
		t.Errorf("expected single provider attempt, got %d", attempts)
	}
}

func TestProviderError_OpenRouterMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", "req-err-1")
		w.Header().Set("X-Generation-ID", "gen-err-1")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    "provider_error",
				"message": "upstream failed",
				"metadata": map[string]any{
					"error_type":    "provider_timeout",
					"provider_code": "upstream_504",
				},
			},
		})
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	_, err := p.Complete(context.Background(), &CompletionRequest{
		Model:    "openai/gpt-4o-mini",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})

	provErr, ok := AsProviderError(err)
	if !ok {
		t.Fatalf("expected ProviderError, got %T: %v", err, err)
	}
	if provErr.Code != ErrCodeUpstreamError {
		t.Fatalf("public code = %q, want %q", provErr.Code, ErrCodeUpstreamError)
	}
	if provErr.NativeCode != "provider_error" {
		t.Fatalf("native code = %q, want provider_error", provErr.NativeCode)
	}
	if provErr.ErrorType != "provider_timeout" || provErr.ProviderCode != "upstream_504" {
		t.Fatalf("provider metadata = type:%q code:%q", provErr.ErrorType, provErr.ProviderCode)
	}
	if provErr.RequestID != "req-err-1" || provErr.GenerationID != "gen-err-1" {
		t.Fatalf("ids = request:%q generation:%q", provErr.RequestID, provErr.GenerationID)
	}
}

func TestProviderError_HTTP502(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`{"error":{"message":"upstream failed"}}`))
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	_, err := p.Complete(context.Background(), &CompletionRequest{
		Model:    "openai/gpt-4o-mini",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})

	provErr, ok := AsProviderError(err)
	if !ok {
		t.Fatalf("expected ProviderError, got %T: %v", err, err)
	}
	if provErr.Code != ErrCodeUpstreamError {
		t.Errorf("expected code %q, got %q", ErrCodeUpstreamError, provErr.Code)
	}
}

func TestProviderError_OpenRouterStatusClassification(t *testing.T) {
	for _, tc := range []struct {
		name          string
		status        int
		wantCode      string
		wantRetryable bool
	}{
		{name: "request timeout", status: http.StatusRequestTimeout, wantCode: ErrCodeUpstreamTimeout, wantRetryable: true},
		{name: "request too large", status: http.StatusRequestEntityTooLarge, wantCode: ErrCodeBadRequest, wantRetryable: false},
		{name: "unprocessable entity", status: http.StatusUnprocessableEntity, wantCode: ErrCodeBadRequest, wantRetryable: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(`{"error":{"message":"provider rejected request"}}`))
			}))
			defer srv.Close()

			p := newTestProvider(srv.URL)
			_, err := p.Complete(context.Background(), &CompletionRequest{
				Model:    "openai/gpt-4o-mini",
				Messages: []Message{{Role: "user", Content: "hi"}},
			})
			provErr, ok := AsProviderError(err)
			if !ok {
				t.Fatalf("expected ProviderError, got %T: %v", err, err)
			}
			if provErr.Code != tc.wantCode {
				t.Fatalf("code = %q, want %q", provErr.Code, tc.wantCode)
			}
			if provErr.Retryable != tc.wantRetryable {
				t.Fatalf("retryable = %v, want %v", provErr.Retryable, tc.wantRetryable)
			}
		})
	}
}

func TestProviderError_HTTP200_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":    "resp-1",
			"model": "openai/gpt-4o-mini",
			"choices": []map[string]any{
				{"message": map[string]string{"content": "Hello!"}, "finish_reason": "stop"},
			},
			"usage": map[string]int{"prompt_tokens": 5, "completion_tokens": 2, "total_tokens": 7},
		})
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	resp, err := p.Complete(context.Background(), &CompletionRequest{
		Model:    "openai/gpt-4o-mini",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello!" {
		t.Errorf("expected content 'Hello!', got %q", resp.Content)
	}
}

func TestOpenRouterRequestHeadersAndControlSerialization(t *testing.T) {
	var captured map[string]any
	var capturedHeader http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Clone()
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id":    "resp-control-1",
			"model": "openai/gpt-4o-mini",
			"choices": []map[string]any{
				{"message": map[string]string{"content": "ok"}, "finish_reason": "stop"},
			},
			"usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	defer srv.Close()

	enableMetadata := true
	allowFallbacks := false
	requireParameters := true
	zdr := true
	reasoningTokens := 256
	reasoningEnabled := true
	parallelToolCalls := false
	p := newTestProvider(srv.URL)
	_, err := p.Complete(context.Background(), &CompletionRequest{
		Model:             "openai/gpt-4o-mini",
		Messages:          []Message{{Role: "user", Content: "hi"}},
		ParallelToolCalls: &parallelToolCalls,
		Extensions: &ProviderExtensions{
			SessionID:          "sess_123",
			OpenRouterMetadata: &enableMetadata,
			ProviderRouting: &ProviderRouting{
				Order:             []string{"anthropic", "openai"},
				Only:              []string{"OpenAI"},
				AllowFallbacks:    &allowFallbacks,
				RequireParameters: &requireParameters,
				Sort:              "price",
				MaxPrice:          &ProviderMaxPrice{Prompt: 0.1, Completion: "0.2"},
				ZDR:               &zdr,
				DataCollection:    "deny",
			},
			Reasoning: &ReasoningConfig{
				Effort:    "minimal",
				MaxTokens: &reasoningTokens,
				Enabled:   &reasoningEnabled,
				Summary:   "auto",
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedHeader.Get("X-Title") != "Consortium" {
		t.Fatalf("X-Title = %q, want Consortium", capturedHeader.Get("X-Title"))
	}
	if capturedHeader.Get("X-OpenRouter-Title") != "Consortium" {
		t.Fatalf("X-OpenRouter-Title = %q, want Consortium", capturedHeader.Get("X-OpenRouter-Title"))
	}
	if capturedHeader.Get("X-OpenRouter-Metadata") != "enabled" {
		t.Fatalf("X-OpenRouter-Metadata = %q, want enabled", capturedHeader.Get("X-OpenRouter-Metadata"))
	}
	if captured["session_id"] != "sess_123" {
		t.Fatalf("session_id = %#v", captured["session_id"])
	}
	if captured["parallel_tool_calls"] != false {
		t.Fatalf("parallel_tool_calls = %#v, want false", captured["parallel_tool_calls"])
	}
	provider, ok := captured["provider"].(map[string]any)
	if !ok {
		t.Fatalf("provider body = %#v", captured["provider"])
	}
	if provider["sort"] != "price" || provider["data_collection"] != "deny" || provider["zdr"] != true {
		t.Fatalf("provider body = %#v", provider)
	}
	maxPrice, ok := provider["max_price"].(map[string]any)
	if !ok || maxPrice["prompt"] != 0.1 || maxPrice["completion"] != "0.2" {
		t.Fatalf("max_price = %#v", provider["max_price"])
	}
	reasoning, ok := captured["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "minimal" || reasoning["max_tokens"] != float64(256) || reasoning["enabled"] != true || reasoning["summary"] != "auto" {
		t.Fatalf("reasoning = %#v", captured["reasoning"])
	}
}

func TestOpenRouterParsesTelemetryAndGenerationID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", "req-success-1")
		w.Header().Set("X-Generation-ID", "gen-success-1")
		json.NewEncoder(w).Encode(map[string]any{
			"id":           "resp-success-1",
			"model":        "openai/gpt-4o-mini",
			"service_tier": "default",
			"openrouter_metadata": map[string]any{
				"provider_name": "openai",
				"region":        "us",
			},
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content":   "Hello!",
						"reasoning": "short rationale",
						"reasoning_details": []map[string]any{
							{"type": "summary", "text": "why"},
						},
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     10,
				"completion_tokens": 5,
				"total_tokens":      15,
				"cost":              0.001,
				"prompt_tokens_details": map[string]any{
					"cached_tokens":      4,
					"cache_write_tokens": 2,
				},
				"completion_tokens_details": map[string]any{
					"reasoning_tokens": 3,
				},
				"cost_details": map[string]any{
					"upstream_inference_cost": 0.0007,
				},
				"is_byok": true,
			},
		})
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	resp, err := p.Complete(context.Background(), &CompletionRequest{
		Model:    "openai/gpt-4o-mini",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.RequestID != "req-success-1" || resp.GenerationID != "gen-success-1" {
		t.Fatalf("ids = request:%q generation:%q", resp.RequestID, resp.GenerationID)
	}
	if resp.ServiceTier != "default" {
		t.Fatalf("service tier = %q", resp.ServiceTier)
	}
	if resp.Reasoning != "short rationale" || !strings.Contains(string(resp.ReasoningDetails), `"summary"`) {
		t.Fatalf("reasoning = %q details=%s", resp.Reasoning, string(resp.ReasoningDetails))
	}
	if !strings.Contains(string(resp.OpenRouterMetadata), `"provider_name"`) {
		t.Fatalf("openrouter metadata = %s", string(resp.OpenRouterMetadata))
	}
	if resp.Usage.PromptTokensDetails == nil || resp.Usage.PromptTokensDetails.CachedTokens != 4 || resp.Usage.PromptTokensDetails.CacheWriteTokens != 2 {
		t.Fatalf("prompt token details = %#v", resp.Usage.PromptTokensDetails)
	}
	if resp.Usage.CompletionTokensDetails == nil || resp.Usage.CompletionTokensDetails.ReasoningTokens != 3 {
		t.Fatalf("completion token details = %#v", resp.Usage.CompletionTokensDetails)
	}
	if resp.Usage.IsBYOK == nil || *resp.Usage.IsBYOK != true {
		t.Fatalf("is_byok = %#v", resp.Usage.IsBYOK)
	}
	if len(resp.Usage.CostDetails) == 0 || resp.Usage.CostDetails["upstream_inference_cost"] != 0.0007 {
		t.Fatalf("cost details = %#v", resp.Usage.CostDetails)
	}
	if !strings.Contains(string(resp.Usage.RawJSON), `"prompt_tokens"`) {
		t.Fatalf("raw usage = %s", string(resp.Usage.RawJSON))
	}
}

func TestProviderError_HTTP200_NoChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "resp-2",
			"model":   "openai/gpt-4o-mini",
			"choices": []map[string]any{},
			"usage":   map[string]int{"prompt_tokens": 1, "completion_tokens": 0, "total_tokens": 1},
		})
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	_, err := p.Complete(context.Background(), &CompletionRequest{
		Model:    "openai/gpt-4o-mini",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatalf("expected no-choices error")
	}
	provErr, ok := AsProviderError(err)
	if !ok {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if provErr.Code != ErrCodeNoChoices {
		t.Fatalf("expected error code %s, got %s", ErrCodeNoChoices, provErr.Code)
	}
	if !provErr.Retryable {
		t.Fatalf("expected no-choices error to be retryable")
	}
}

func TestToolCallRoundTrip_UsesArgumentsWhenContentEmpty(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		json.NewEncoder(w).Encode(map[string]any{
			"id":    "resp-tool-1",
			"model": "openai/gpt-4o-mini",
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": "",
						"tool_calls": []map[string]any{
							{
								"id":   "call_1",
								"type": "function",
								"function": map[string]any{
									"name":      "submit_answer",
									"arguments": `{"answer":"B"}`,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
			"usage": map[string]int{"prompt_tokens": 5, "completion_tokens": 2, "total_tokens": 7},
		})
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	resp, err := p.Complete(context.Background(), &CompletionRequest{
		Model:    "openai/gpt-4o-mini",
		Messages: []Message{{Role: "user", Content: "hi"}},
		Tools: []ToolDefinition{
			{
				Type: "function",
				Function: ToolFunctionDefinition{
					Name: "submit_answer",
					Parameters: map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"answer": map[string]interface{}{
								"type": "string",
							},
						},
						"required": []string{"answer"},
					},
				},
			},
		},
		ToolChoice: "required",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "B" {
		t.Fatalf("expected content from tool args to be B, got %q", resp.Content)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected one raw tool call, got %+v", resp.ToolCalls)
	}
	if resp.ToolCalls[0].ID != "call_1" || resp.ToolCalls[0].Function.Name != "submit_answer" {
		t.Fatalf("unexpected tool call payload: %+v", resp.ToolCalls[0])
	}

	if _, ok := captured["tools"]; !ok {
		t.Fatalf("expected request to include tools, got body: %#v", captured)
	}
	if gotChoice, ok := captured["tool_choice"].(string); !ok || gotChoice != "required" {
		t.Fatalf("expected tool_choice=required, got %#v", captured["tool_choice"])
	}
}

func TestToolCallRoundTrip_UsesOptionFunctionNameWhenNoArguments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":    "resp-tool-2",
			"model": "openai/gpt-4o-mini",
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": "",
						"tool_calls": []map[string]any{
							{
								"id":   "call_2",
								"type": "function",
								"function": map[string]any{
									"name":      "select_option_D",
									"arguments": "{}",
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
			"usage": map[string]int{"prompt_tokens": 5, "completion_tokens": 2, "total_tokens": 7},
		})
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	resp, err := p.Complete(context.Background(), &CompletionRequest{
		Model:      "openai/gpt-4o-mini",
		Messages:   []Message{{Role: "user", Content: "hi"}},
		ToolChoice: "required",
		Tools: []ToolDefinition{
			{
				Type: "function",
				Function: ToolFunctionDefinition{
					Name: "select_option_D",
					Parameters: map[string]interface{}{
						"type":                 "object",
						"properties":           map[string]interface{}{},
						"additionalProperties": false,
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "D" {
		t.Fatalf("expected content from option function name to be D, got %q", resp.Content)
	}
}

func TestToolCallRoundTrip_PrefersToolSelectionOverTextContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"id":    "resp-tool-3",
			"model": "openai/gpt-4o-mini",
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": "I choose B",
						"tool_calls": []map[string]any{
							{
								"id":   "call_3",
								"type": "function",
								"function": map[string]any{
									"name":      "select_option_E",
									"arguments": "{}",
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
			"usage": map[string]int{"prompt_tokens": 5, "completion_tokens": 2, "total_tokens": 7},
		})
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	resp, err := p.Complete(context.Background(), &CompletionRequest{
		Model:      "openai/gpt-4o-mini",
		Messages:   []Message{{Role: "user", Content: "hi"}},
		ToolChoice: "required",
		Tools: []ToolDefinition{
			{
				Type: "function",
				Function: ToolFunctionDefinition{
					Name: "select_option_E",
					Parameters: map[string]interface{}{
						"type":                 "object",
						"properties":           map[string]interface{}{},
						"additionalProperties": false,
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "E" {
		t.Fatalf("expected tool-selected answer E, got %q", resp.Content)
	}
}

func TestZeroValueSerialization(t *testing.T) {
	tests := []struct {
		name        string
		temperature *float64
		expectKey   bool
		expectValue float64
	}{
		{"nil temperature omitted", nil, false, 0},
		{"zero temperature included", Float64Ptr(0.0), true, 0.0},
		{"non-zero temperature included", Float64Ptr(0.7), true, 0.7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewDecoder(r.Body).Decode(&captured)
				// Return valid response
				json.NewEncoder(w).Encode(map[string]any{
					"id":    "resp-1",
					"model": "openai/gpt-4o-mini",
					"choices": []map[string]any{
						{"message": map[string]string{"content": "ok"}, "finish_reason": "stop"},
					},
					"usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
				})
			}))
			defer srv.Close()

			p := newTestProvider(srv.URL)
			_, err := p.Complete(context.Background(), &CompletionRequest{
				Model:       "openai/gpt-4o-mini",
				Messages:    []Message{{Role: "user", Content: "hi"}},
				Temperature: tt.temperature,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			_, hasKey := captured["temperature"]
			if hasKey != tt.expectKey {
				t.Errorf("temperature key present=%v, want %v (body: %v)", hasKey, tt.expectKey, captured)
			}
			if tt.expectKey {
				val, ok := captured["temperature"].(float64)
				if !ok {
					t.Errorf("temperature not a float64: %T", captured["temperature"])
				} else if val != tt.expectValue {
					t.Errorf("temperature=%v, want %v", val, tt.expectValue)
				}
			}
		})
	}
}

func TestReasoningSerialization(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&captured)
		json.NewEncoder(w).Encode(map[string]any{
			"id":    "resp-1",
			"model": "xiaomi/mimo-v2-flash",
			"choices": []map[string]any{
				{"message": map[string]string{"content": "ok"}, "finish_reason": "stop"},
			},
			"usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
		})
	}))
	defer srv.Close()

	p := newTestProvider(srv.URL)
	_, err := p.Complete(context.Background(), &CompletionRequest{
		Model:    "xiaomi/mimo-v2-flash",
		Messages: []Message{{Role: "user", Content: "hi"}},
		Extensions: &ProviderExtensions{
			Reasoning: &ReasoningConfig{
				Effort: "high",
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reasoningRaw, ok := captured["reasoning"]
	if !ok {
		t.Fatalf("expected request to include reasoning, got body: %#v", captured)
	}
	reasoning, ok := reasoningRaw.(map[string]any)
	if !ok {
		t.Fatalf("expected reasoning to be object, got %T", reasoningRaw)
	}
	if got, _ := reasoning["effort"].(string); got != "high" {
		t.Fatalf("expected reasoning.effort=high, got %#v", reasoning["effort"])
	}
}

func TestIsTransientNetworkError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"connection reset", errors.New("read tcp: connection reset by peer"), true},
		{"broken pipe", errors.New("write: broken pipe"), true},
		{"EOF", errors.New("unexpected EOF"), true},
		{"connection refused", errors.New("dial tcp: connection refused"), true},
		{"context canceled", context.Canceled, false},
		{"context deadline exceeded", context.DeadlineExceeded, false},
		{"regular error", errors.New("some failure"), false},
		{"nil error", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isTransientNetworkError(tt.err)
			if got != tt.want {
				t.Errorf("isTransientNetworkError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestEstimateTokens(t *testing.T) {
	p := newTestProvider("http://unused")

	tests := []struct {
		name string
		text string
		want int
	}{
		{"empty string", "", 0},
		{"single char", "a", 1},
		{"five chars", "hello", 2},
		{"100 chars", strings.Repeat("x", 100), 25},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.EstimateTokens(tt.text)
			if got != tt.want {
				t.Errorf("EstimateTokens(%q) = %d, want %d", tt.text, got, tt.want)
			}
		})
	}
}

func TestStringifyOpenRouterValueRejectsStructuredValues(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{"string", " native ", "native"},
		{"integer float", 42.0, "42"},
		{"fractional float", 42.5, "42.5"},
		{"bool", true, "true"},
		{"json number", json.Number("429"), "429"},
		{"map", map[string]any{"code": "x"}, ""},
		{"slice", []any{"x"}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stringifyOpenRouterValue(tt.value); got != tt.want {
				t.Fatalf("stringifyOpenRouterValue(%T) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}
