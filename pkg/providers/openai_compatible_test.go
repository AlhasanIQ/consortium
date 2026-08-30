package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func newCompatibleTestProvider(serverURL string, apiKey string, models ...string) *OpenAICompatibleProvider {
	return NewOpenAICompatibleProvider(OpenAICompatibleConfig{
		BaseURL: serverURL,
		APIKey:  apiKey,
		Timeout: 5 * time.Second,
		Models:  models,
	})
}

func TestOpenAICompatibleProviderFallbackModelsAreNamespaced(t *testing.T) {
	p := newCompatibleTestProvider("http://127.0.0.1:1/v1", "", "qwen3:8b", "compatible/llama3.2:3b", "qwen3:8b", " ")
	models := p.Models()
	got := make([]string, 0, len(models))
	for _, model := range models {
		got = append(got, model.ID)
		if model.Provider != openAICompatibleProviderName {
			t.Fatalf("provider = %q, want %q", model.Provider, openAICompatibleProviderName)
		}
	}
	want := []string{"compatible/llama3.2:3b", "compatible/qwen3:8b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("model IDs = %#v, want %#v", got, want)
	}
}

func TestOpenAICompatibleProviderRefreshModelsMergesFallbackAndUsesAuth(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %q, want /v1/models", r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": "remote-model", "object": "model"},
				{"id": "qwen3:8b", "object": "model"},
			},
		})
	}))
	defer srv.Close()

	p := newCompatibleTestProvider(srv.URL+"/v1/", "secret", "qwen3:8b", "manual-only")
	if err := p.RefreshModels(context.Background()); err != nil {
		t.Fatalf("RefreshModels: %v", err)
	}
	if auth != "Bearer secret" {
		t.Fatalf("Authorization = %q, want Bearer secret", auth)
	}

	models := p.Models()
	got := make([]string, 0, len(models))
	for _, model := range models {
		got = append(got, model.ID)
	}
	want := []string{"compatible/manual-only", "compatible/qwen3:8b", "compatible/remote-model"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("model IDs = %#v, want %#v", got, want)
	}
}

func TestOpenAICompatibleProviderRefreshModelsDoesNotSendEmptyAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization = %q, want empty", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "local-model"}},
		})
	}))
	defer srv.Close()

	p := newCompatibleTestProvider(srv.URL, "")
	if err := p.RefreshModels(context.Background()); err != nil {
		t.Fatalf("RefreshModels: %v", err)
	}
}

func TestOpenAICompatibleProviderRefreshFailurePreservesFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not supported", http.StatusNotFound)
	}))
	defer srv.Close()

	p := newCompatibleTestProvider(srv.URL, "", "manual-model")
	if err := p.RefreshModels(context.Background()); err == nil {
		t.Fatal("expected refresh error")
	}
	models := p.Models()
	if len(models) != 1 || models[0].ID != "compatible/manual-model" {
		t.Fatalf("fallback models after failed refresh = %#v", models)
	}
}

func TestOpenAICompatibleProviderCompleteUsesUpstreamModelAndStandardFields(t *testing.T) {
	var gotBody map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %q, want /v1/chat/completions", r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("X-Request-ID", "req-local-1")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":           "chatcmpl-local",
			"model":        "qwen3:8b",
			"service_tier": "default",
			"choices": []map[string]any{{
				"finish_reason": "tool_calls",
				"message": map[string]any{
					"content": "ignored free-form content",
					"tool_calls": []map[string]any{{
						"id":   "call-1",
						"type": "function",
						"function": map[string]any{
							"name":      "ANSWER_B",
							"arguments": `{}`,
						},
					}},
				},
			}},
			"usage": map[string]any{
				"prompt_tokens":     11,
				"completion_tokens": 3,
				"total_tokens":      14,
				"cost":              0.002,
			},
		})
	}))
	defer srv.Close()

	p := newCompatibleTestProvider(srv.URL+"/v1", "local-secret", "qwen3:8b")
	parallel := true
	resp, err := p.Complete(context.Background(), &CompletionRequest{
		Model:       "compatible/qwen3:8b",
		Messages:    []Message{{Role: "user", Content: "Pick one"}},
		MaxTokens:   128,
		Temperature: Float64Ptr(0),
		TopP:        Float64Ptr(0.9),
		Stop:        []string{"END"},
		Extensions: &ProviderExtensions{
			ResponseFormat:     &ResponseFormat{Type: "json_object"},
			Seed:               IntPtr(7),
			ProviderRouting:    &ProviderRouting{Only: []string{"should-not-leak"}},
			Reasoning:          &ReasoningConfig{Effort: "high"},
			SessionID:          "should-not-leak",
			OpenRouterMetadata: BoolPtr(true),
		},
		Tools: []ToolDefinition{{
			Type: "function",
			Function: ToolFunctionDefinition{
				Name:       "ANSWER_B",
				Parameters: map[string]interface{}{"type": "object"},
			},
		}},
		ToolChoice:        "auto",
		ParallelToolCalls: &parallel,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if auth != "Bearer local-secret" {
		t.Fatalf("Authorization = %q, want Bearer local-secret", auth)
	}
	if gotBody["model"] != "qwen3:8b" {
		t.Fatalf("upstream model = %#v, want qwen3:8b", gotBody["model"])
	}
	for _, key := range []string{"max_tokens", "temperature", "top_p", "stop", "response_format", "seed", "tools", "tool_choice", "parallel_tool_calls"} {
		if _, ok := gotBody[key]; !ok {
			t.Errorf("request missing standard field %q", key)
		}
	}
	for _, key := range []string{"provider", "reasoning", "session_id", "openrouter_metadata"} {
		if _, ok := gotBody[key]; ok {
			t.Errorf("OpenRouter-specific field %q leaked into generic request", key)
		}
	}
	if resp.Content != "B" {
		t.Fatalf("Content = %q, want extracted tool answer B", resp.Content)
	}
	if resp.Model != "compatible/qwen3:8b" {
		t.Fatalf("Model = %q, want namespaced model", resp.Model)
	}
	if resp.RequestID != "req-local-1" {
		t.Fatalf("RequestID = %q, want req-local-1", resp.RequestID)
	}
	if resp.Usage.TotalTokens != 14 || resp.Usage.Cost == nil || *resp.Usage.Cost != 0.002 {
		t.Fatalf("Usage = %#v", resp.Usage)
	}
}

func TestOpenAICompatibleProviderCompleteWithoutAPIKeyOmitsAuthorization(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization = %q, want empty", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "local",
			"model": "local-model",
			"choices": []map[string]any{{
				"message":       map[string]any{"content": "hello"},
				"finish_reason": "stop",
			}},
		})
	}))
	defer srv.Close()

	p := newCompatibleTestProvider(srv.URL, "", "local-model")
	resp, err := p.Complete(context.Background(), &CompletionRequest{
		Model:    "compatible/local-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "hello" {
		t.Fatalf("Content = %q, want hello", resp.Content)
	}
}

func TestOpenAICompatibleProviderRejectsUnprefixedModelBeforeHTTP(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := newCompatibleTestProvider(srv.URL, "", "local-model")
	_, err := p.Complete(context.Background(), &CompletionRequest{
		Model:    "local-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	pe, ok := AsProviderError(err)
	if !ok {
		t.Fatalf("error = %T %v, want ProviderError", err, err)
	}
	if pe.Code != ErrCodeBadRequest || pe.Retryable {
		t.Fatalf("ProviderError = %#v", pe)
	}
	if attempts != 0 {
		t.Fatalf("HTTP attempts = %d, want 0", attempts)
	}
}

func TestOpenAICompatibleProviderErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		code      string
		retryable bool
	}{
		{name: "bad request", status: http.StatusBadRequest, code: ErrCodeBadRequest, retryable: false},
		{name: "auth", status: http.StatusUnauthorized, code: ErrCodeAuthError, retryable: false},
		{name: "missing model", status: http.StatusNotFound, code: ErrCodeModelNotFound, retryable: false},
		{name: "rate limited", status: http.StatusTooManyRequests, code: ErrCodeRateLimited, retryable: true},
		{name: "bad gateway", status: http.StatusBadGateway, code: ErrCodeUpstreamError, retryable: true},
		{name: "gateway timeout", status: http.StatusGatewayTimeout, code: ErrCodeUpstreamTimeout, retryable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.status == http.StatusTooManyRequests {
					w.Header().Set("Retry-After", "2")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":{"code":"native","message":"endpoint detail"}}`))
			}))
			defer srv.Close()

			p := newCompatibleTestProvider(srv.URL, "", "m")
			_, err := p.Complete(context.Background(), &CompletionRequest{Model: "compatible/m", Messages: []Message{{Role: "user", Content: "hi"}}})
			pe, ok := AsProviderError(err)
			if !ok {
				t.Fatalf("error = %T %v, want ProviderError", err, err)
			}
			if pe.Code != tc.code || pe.Retryable != tc.retryable {
				t.Fatalf("ProviderError code/retryable = %q/%v, want %q/%v", pe.Code, pe.Retryable, tc.code, tc.retryable)
			}
			if pe.Provider != openAICompatibleProviderName {
				t.Fatalf("Provider = %q", pe.Provider)
			}
			if !strings.Contains(pe.Message, "endpoint detail") {
				t.Fatalf("Message = %q, want endpoint detail", pe.Message)
			}
			if tc.status == http.StatusTooManyRequests && pe.RetryAfter != 2*time.Second {
				t.Fatalf("RetryAfter = %v, want 2s", pe.RetryAfter)
			}
		})
	}
}

func TestParseOpenAICompatibleModelsRejectsMalformedJSON(t *testing.T) {
	if _, err := parseOpenAICompatibleModels([]byte(`{"data":`)); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestParseRetryAfterSupportsSeconds(t *testing.T) {
	if got := parseRetryAfter("3"); got != 3*time.Second {
		t.Fatalf("parseRetryAfter(3) = %v, want 3s", got)
	}
	if got := parseRetryAfter("invalid"); got != 0 {
		t.Fatalf("parseRetryAfter(invalid) = %v, want 0", got)
	}
}
