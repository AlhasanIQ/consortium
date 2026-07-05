package api

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/gorilla/mux"
)

func TestOpenAIModelsRequiresBearerAuth(t *testing.T) {
	api, _ := setupWorkflowAPI(t)
	router := mux.NewRouter()
	api.RegisterRoutes(router)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/models", nil))

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 body=%s", w.Code, w.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := payload["error"].(map[string]any); !ok {
		t.Fatalf("response = %#v, want OpenAI-style error object", payload)
	}
}

func TestOpenAIInvalidBearerRequestsArePreAuthRateLimited(t *testing.T) {
	api, _ := setupWorkflowAPI(t)
	api.preAuthRequestLimit = 1
	router := mux.NewRouter()
	api.RegisterRoutes(router)

	call := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		req.RemoteAddr = "203.0.113.10:12345"
		req.Header.Set("Authorization", "Bearer invalid-token")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	first := call()
	if first.Code != http.StatusUnauthorized {
		t.Fatalf("first status = %d, want 401 body=%s", first.Code, first.Body.String())
	}
	second := call()
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429 body=%s", second.Code, second.Body.String())
	}
	assertOpenAIErrorEnvelope(t, second, "rate_limit_exceeded")
}

func TestOpenAILongBearerTokenRejected(t *testing.T) {
	api, _ := setupWorkflowAPI(t)
	router := mux.NewRouter()
	api.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("x", openAIMaxBearerTokenBytes+1))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 body=%s", w.Code, w.Body.String())
	}
	assertOpenAIErrorEnvelope(t, w, "invalid_api_key")
}

func TestOpenAIKeyPrefixSupportsDotSeparatorAndLegacyUnderscore(t *testing.T) {
	if got := openAIKeyPrefix("sk-consortium-ab_cd.efgh"); got != "sk-consortium-ab_cd" {
		t.Fatalf("dot-separated prefix = %q, want sk-consortium-ab_cd", got)
	}
	if got := openAIKeyPrefix("sk-consortium-test_123"); got != "sk-consortium-test" {
		t.Fatalf("legacy underscore prefix = %q, want sk-consortium-test", got)
	}
	if got := openAIKeyPrefix("sk-consortium-no-separator"); got != "sk-consortium-no-separator" {
		t.Fatalf("prefix without separator = %q", got)
	}
}

func TestOpenAIAuthFallsBackToHashForLegacyUnderscorePrefix(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	token := "sk-consortium-ab_cd_secret"
	sum := sha256.Sum256([]byte(token))
	if err := store.CreateAPIKey(&storage.APIKey{
		ID:                "key-underscore-prefix",
		UserID:            "system",
		Name:              "underscore prefix key",
		Prefix:            "sk-consortium-ab_cd",
		KeyHash:           "sha256:" + hex.EncodeToString(sum[:]),
		RequestsPerMinute: 100,
		TokensPerMinute:   100000,
		CreatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
}

func TestOpenAIModelsRetrieve(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	upsertOpenAIDirectModelRoute(t, store)
	if err := store.UpsertAPIModelRoute(&storage.APIModelRoute{
		APIModel:      "gpt-disabled",
		Mode:          storage.APIModelRouteModeDirectModel,
		ProviderModel: "mock-model",
		Enabled:       false,
	}); err != nil {
		t.Fatalf("UpsertAPIModelRoute disabled: %v", err)
	}

	router := mux.NewRouter()
	api.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/v1/models/gpt-test", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["id"] != "gpt-test" || payload["object"] != "model" || payload["owned_by"] != "consortium" {
		t.Fatalf("payload = %#v, want model object", payload)
	}

	for _, model := range []string{"missing-model", "gpt-disabled"} {
		req := httptest.NewRequest(http.MethodGet, "/v1/models/"+model, nil)
		req.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404 body=%s", model, w.Code, w.Body.String())
		}
	}
}

func TestOpenAIReadEndpointsApplyRequestRateLimit(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	key := createOpenAITestKeyWithLimits(t, store, "sk-consortium-test_123", 1, 100000)
	upsertOpenAIDirectModelRoute(t, store)

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first read status = %d, want 200 body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/models/gpt-test", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second read status = %d, want 429 body=%s", w.Code, w.Body.String())
	}
	assertOpenAIErrorEnvelope(t, w, "rate_limit_exceeded")
}

func TestOpenAIRateLimiterPrunesExpiredKeyBuckets(t *testing.T) {
	now := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	limiter := newOpenAIRateLimiter()
	limiter.clock = func() time.Time { return now }

	if retryAfter := limiter.reserve("key-expired", 10, 0, 1, 0); retryAfter > 0 {
		t.Fatalf("initial reserve retryAfter = %v, want allowed", retryAfter)
	}
	now = now.Add(2 * time.Minute)
	if retryAfter := limiter.reserve("key-active", 10, 0, 1, 0); retryAfter > 0 {
		t.Fatalf("second reserve retryAfter = %v, want allowed", retryAfter)
	}

	if _, ok := limiter.windows["key-expired"]; ok {
		t.Fatalf("expired key bucket retained: %+v", limiter.windows)
	}
	if len(limiter.windows["key-active"]) != 1 {
		t.Fatalf("active key bucket = %+v, want one event", limiter.windows["key-active"])
	}
}

func TestOpenAIResponsesDirectModelSuccess(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	registerMockProvider(t, api, "Responses API output")
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	if err := store.UpsertAPIModelRoute(&storage.APIModelRoute{
		APIModel:      "gpt-test",
		Mode:          storage.APIModelRouteModeDirectModel,
		ProviderModel: "mock-model",
		IsDefault:     true,
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpsertAPIModelRoute: %v", err)
	}

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-test",
		"instructions":"Be terse.",
		"input":"Say hello",
		"max_output_tokens":64,
		"prompt_cache_key":"abc",
		"prompt_cache_retention":"24h"
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["object"] != "response" || payload["status"] != "completed" {
		t.Fatalf("payload = %#v, want completed response", payload)
	}
	if payload["output_text"] != "Responses API output" {
		t.Fatalf("output_text = %v, want Responses API output", payload["output_text"])
	}
	usage := payload["usage"].(map[string]any)
	if usage["total_tokens"].(float64) <= 0 {
		t.Fatalf("usage = %#v, want positive total_tokens", usage)
	}
}

func TestOpenAIPromptCacheRetentionValidation(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	registerMockProvider(t, api, "ok")
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	upsertOpenAIDirectModelRoute(t, store)

	router := mux.NewRouter()
	api.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-test",
		"input":"Say hello",
		"prompt_cache_key":"tenant-a",
		"prompt_cache_retention":"in_memory"
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("in_memory status = %d, want 200 body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-test",
		"input":"Say hello",
		"prompt_cache_retention":"bogus"
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bogus status = %d, want 400 body=%s", w.Code, w.Body.String())
	}
	assertOpenAIErrorEnvelope(t, w, "invalid_prompt_cache_retention")
}

func TestOpenAIValidationRejectsUnsupportedAndAcceptsNoopFields(t *testing.T) {
	tests := []struct {
		name       string
		endpoint   string
		body       string
		wantStatus int
		wantCode   string
		wantParam  string
	}{
		{
			name:     "chat n two rejected",
			endpoint: "/v1/chat/completions",
			body: `{
				"model":"gpt-test",
				"messages":[{"role":"user","content":"Say hello"}],
				"n":2
			}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "unsupported_n",
		},
		{
			name:     "chat n one accepted",
			endpoint: "/v1/chat/completions",
			body: `{
				"model":"gpt-test",
				"messages":[{"role":"user","content":"Say hello"}],
				"n":1
			}`,
			wantStatus: http.StatusOK,
		},
		{
			name:     "chat logprobs true rejected",
			endpoint: "/v1/chat/completions",
			body: `{
				"model":"gpt-test",
				"messages":[{"role":"user","content":"Say hello"}],
				"logprobs":true
			}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "unsupported_logprobs",
		},
		{
			name:     "chat logprobs false accepted",
			endpoint: "/v1/chat/completions",
			body: `{
				"model":"gpt-test",
				"messages":[{"role":"user","content":"Say hello"}],
				"logprobs":false,
				"parallel_tool_calls":true,
				"service_tier":"auto",
				"user":"user-123",
				"safety_identifier":"safe-123"
			}`,
			wantStatus: http.StatusOK,
		},
		{
			name:     "chat text modality accepted",
			endpoint: "/v1/chat/completions",
			body: `{
				"model":"gpt-test",
				"messages":[{"role":"user","content":"Say hello"}],
				"modalities":["text"]
			}`,
			wantStatus: http.StatusOK,
		},
		{
			name:     "chat audio modality rejected",
			endpoint: "/v1/chat/completions",
			body: `{
				"model":"gpt-test",
				"messages":[{"role":"user","content":"Say hello"}],
				"modalities":["text","audio"]
			}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "unsupported_modalities",
			wantParam:  "modalities",
		},
		{
			name:     "chat audio config rejected",
			endpoint: "/v1/chat/completions",
			body: `{
				"model":"gpt-test",
				"messages":[{"role":"user","content":"Say hello"}],
				"audio":{"voice":"alloy","format":"mp3"}
			}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "unsupported_audio",
			wantParam:  "audio",
		},
		{
			name:     "chat deprecated functions rejected",
			endpoint: "/v1/chat/completions",
			body: `{
				"model":"gpt-test",
				"messages":[{"role":"user","content":"Say hello"}],
				"functions":[{"name":"legacy","parameters":{"type":"object"}}]
			}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "unsupported_functions",
			wantParam:  "functions",
		},
		{
			name:     "chat deprecated function call rejected",
			endpoint: "/v1/chat/completions",
			body: `{
				"model":"gpt-test",
				"messages":[{"role":"user","content":"Say hello"}],
				"function_call":{"name":"legacy"}
			}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "unsupported_function_call",
			wantParam:  "function_call",
		},
		{
			name:     "chat provider routing rejected",
			endpoint: "/v1/chat/completions",
			body: `{
				"model":"gpt-test",
				"messages":[{"role":"user","content":"Say hello"}],
				"provider":{"only":["openai"],"zdr":true}
			}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "unsupported_provider_routing",
			wantParam:  "provider",
		},
		{
			name:     "chat top-level provider order rejected",
			endpoint: "/v1/chat/completions",
			body: `{
				"model":"gpt-test",
				"messages":[{"role":"user","content":"Say hello"}],
				"order":["openai"]
			}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "unsupported_provider_routing",
			wantParam:  "order",
		},
		{
			name:     "chat numeric metadata rejected",
			endpoint: "/v1/chat/completions",
			body: `{
				"model":"gpt-test",
				"messages":[{"role":"user","content":"Say hello"}],
				"metadata":{"tenant":123}
			}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_metadata",
			wantParam:  "metadata",
		},
		{
			name:     "chat invalid temperature rejected",
			endpoint: "/v1/chat/completions",
			body: `{
				"model":"gpt-test",
				"messages":[{"role":"user","content":"Say hello"}],
				"temperature":3
			}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_temperature",
			wantParam:  "temperature",
		},
		{
			name:     "chat invalid top p rejected",
			endpoint: "/v1/chat/completions",
			body: `{
				"model":"gpt-test",
				"messages":[{"role":"user","content":"Say hello"}],
				"top_p":1.5
			}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_top_p",
		},
		{
			name:     "chat negative top logprobs rejected",
			endpoint: "/v1/chat/completions",
			body: `{
				"model":"gpt-test",
				"messages":[{"role":"user","content":"Say hello"}],
				"top_logprobs":-1
			}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_top_logprobs",
			wantParam:  "top_logprobs",
		},
		{
			name:     "chat negative max tokens rejected",
			endpoint: "/v1/chat/completions",
			body: `{
				"model":"gpt-test",
				"messages":[{"role":"user","content":"Say hello"}],
				"max_completion_tokens":-1
			}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_max_completion_tokens",
		},
		{
			name:     "chat unknown message role rejected",
			endpoint: "/v1/chat/completions",
			body: `{
				"model":"gpt-test",
				"messages":[{"role":"critic","content":"Say hello"}]
			}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_message_role",
			wantParam:  "messages",
		},
		{
			name:     "responses conversation rejected",
			endpoint: "/v1/responses",
			body: `{
				"model":"gpt-test",
				"input":"Say hello",
				"conversation":"conv_123"
			}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "unsupported_conversation",
		},
		{
			name:     "responses moderation rejected",
			endpoint: "/v1/responses",
			body: `{
				"model":"gpt-test",
				"input":"Say hello",
				"moderation":{"model":"omni-moderation-latest"}
			}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "unsupported_moderation",
		},
		{
			name:     "responses background requires store",
			endpoint: "/v1/responses",
			body: `{
				"model":"gpt-test",
				"input":"Say hello",
				"background":true,
				"store":false
			}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "unsupported_background_store_false",
		},
		{
			name:     "responses provider routing rejected",
			endpoint: "/v1/responses",
			body: `{
				"model":"gpt-test",
				"input":"Say hello",
				"provider":{"data_collection":"deny","only":["openai"]}
			}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "unsupported_provider_routing",
			wantParam:  "provider",
		},
		{
			name:     "responses include rejected",
			endpoint: "/v1/responses",
			body: `{
				"model":"gpt-test",
				"input":"Say hello",
				"include":["file_search_call.results"]
			}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "unsupported_include",
			wantParam:  "include",
		},
		{
			name:     "responses truncation disabled accepted",
			endpoint: "/v1/responses",
			body: `{
				"model":"gpt-test",
				"input":"Say hello",
				"truncation":"disabled"
			}`,
			wantStatus: http.StatusOK,
		},
		{
			name:     "responses truncation auto rejected",
			endpoint: "/v1/responses",
			body: `{
				"model":"gpt-test",
				"input":"Say hello",
				"truncation":"auto"
			}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "unsupported_truncation",
			wantParam:  "truncation",
		},
		{
			name:     "responses context management rejected",
			endpoint: "/v1/responses",
			body: `{
				"model":"gpt-test",
				"input":"Say hello",
				"context_management":{"type":"auto"}
			}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "unsupported_context_management",
			wantParam:  "context_management",
		},
		{
			name:     "responses max tool calls rejected",
			endpoint: "/v1/responses",
			body: `{
				"model":"gpt-test",
				"input":"Say hello",
				"max_tool_calls":1
			}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "unsupported_max_tool_calls",
			wantParam:  "max_tool_calls",
		},
		{
			name:     "responses nested metadata rejected",
			endpoint: "/v1/responses",
			body: `{
				"model":"gpt-test",
				"input":"Say hello",
				"metadata":{"tenant":{"id":"acme"}}
			}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_metadata",
			wantParam:  "metadata",
		},
		{
			name:     "responses excessive max output tokens rejected",
			endpoint: "/v1/responses",
			body: `{
				"model":"gpt-test",
				"input":"Say hello",
				"max_output_tokens":1000001
			}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_max_output_tokens",
		},
		{
			name:     "hosted tool rejected",
			endpoint: "/v1/responses",
			body: `{
				"model":"gpt-test",
				"input":"Say hello",
				"tools":[{"type":"web_search_preview"}]
			}`,
			wantStatus: http.StatusBadRequest,
			wantCode:   "unsupported_tool_type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api, store := setupWorkflowAPI(t)
			registerMockProvider(t, api, "ok")
			key := createOpenAITestKey(t, store, "sk-consortium-test_123")
			upsertOpenAIDirectModelRoute(t, store)

			router := mux.NewRouter()
			api.RegisterRoutes(router)
			req := httptest.NewRequest(http.MethodPost, tt.endpoint, strings.NewReader(tt.body))
			req.Header.Set("Authorization", "Bearer "+key)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d body=%s", w.Code, tt.wantStatus, w.Body.String())
			}
			if tt.wantCode != "" {
				assertOpenAIErrorEnvelope(t, w, tt.wantCode)
			}
			if tt.wantParam != "" {
				var payload openAIErrorResponse
				if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
					t.Fatalf("decode error response: %v", err)
				}
				if payload.Error.Param != tt.wantParam {
					t.Fatalf("error param = %#v, want %q body=%s", payload.Error.Param, tt.wantParam, w.Body.String())
				}
			}
		})
	}
}

func TestOpenAIMetadataValidationLimits(t *testing.T) {
	validKey := strings.Repeat("k", openAIMaxMetadataKeyChars)
	validValue := strings.Repeat("v", openAIMaxMetadataValueChars)
	if validation := validateOpenAIMetadata(map[string]interface{}{validKey: validValue}); validation != nil {
		t.Fatalf("valid metadata rejected: %#v", validation)
	}

	tooMany := map[string]interface{}{}
	for i := 0; i < openAIMaxMetadataPairs+1; i++ {
		tooMany[fmt.Sprintf("k%d", i)] = "v"
	}
	if validation := validateOpenAIMetadata(tooMany); validation == nil || validation.Code != "invalid_metadata" {
		t.Fatalf("too many metadata pairs validation = %#v, want invalid_metadata", validation)
	}

	if validation := validateOpenAIMetadata(map[string]interface{}{strings.Repeat("k", openAIMaxMetadataKeyChars+1): "v"}); validation == nil || validation.Code != "invalid_metadata" {
		t.Fatalf("long metadata key validation = %#v, want invalid_metadata", validation)
	}

	if validation := validateOpenAIMetadata(map[string]interface{}{"k": strings.Repeat("v", openAIMaxMetadataValueChars+1)}); validation == nil || validation.Code != "invalid_metadata" {
		t.Fatalf("long metadata value validation = %#v, want invalid_metadata", validation)
	}

	if validation := validateOpenAIMetadata(map[string]interface{}{"k": json.Number("123")}); validation == nil || validation.Code != "invalid_metadata" {
		t.Fatalf("json.Number metadata value validation = %#v, want invalid_metadata", validation)
	}

	if validation := validateOpenAIMetadata(map[string]interface{}{"k": map[string]interface{}{"nested": "v"}}); validation == nil || validation.Code != "invalid_metadata" {
		t.Fatalf("nested metadata value validation = %#v, want invalid_metadata", validation)
	}
}

func TestOpenAIResponsesTextFormatMapsToResponseFormat(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	provider := registerMockProvider(t, api, "json")
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	upsertOpenAIDirectModelRoute(t, store)

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-test",
		"input":"Return JSON",
		"text":{"format":{"type":"json_object"}}
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	if provider.lastRequest == nil || provider.lastRequest.Extensions == nil || provider.lastRequest.Extensions.ResponseFormat == nil {
		t.Fatalf("provider request missing response_format: %+v", provider.lastRequest)
	}
	if provider.lastRequest.Extensions.ResponseFormat.Type != "json_object" {
		t.Fatalf("response_format type = %q, want json_object", provider.lastRequest.Extensions.ResponseFormat.Type)
	}
}

func TestOpenAIChatJSONSchemaRequiresProviderParameterSupport(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	provider := registerMockProvider(t, api, `{"ok":true}`)
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	upsertOpenAIDirectModelRoute(t, store)

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"gpt-test",
		"messages":[{"role":"user","content":"Return JSON"}],
		"response_format":{
			"type":"json_schema",
			"json_schema":{
				"name":"answer",
				"strict":true,
				"schema":{
					"type":"object",
					"properties":{"ok":{"type":"boolean"}},
					"required":["ok"],
					"additionalProperties":false
				}
			}
		}
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	assertJSONSchemaRequiresProviderParameters(t, provider.lastRequest)
}

func TestOpenAIResponsesJSONSchemaRequiresProviderParameterSupport(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	provider := registerMockProvider(t, api, `{"ok":true}`)
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	upsertOpenAIDirectModelRoute(t, store)

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-test",
		"input":"Return JSON",
		"text":{
			"format":{
				"type":"json_schema",
				"json_schema":{
					"name":"answer",
					"strict":true,
					"schema":{
						"type":"object",
						"properties":{"ok":{"type":"boolean"}},
						"required":["ok"],
						"additionalProperties":false
					}
				}
			}
		}
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	assertJSONSchemaRequiresProviderParameters(t, provider.lastRequest)
}

func assertJSONSchemaRequiresProviderParameters(t *testing.T, req *providers.CompletionRequest) {
	t.Helper()
	if req == nil || req.Extensions == nil || req.Extensions.ResponseFormat == nil {
		t.Fatalf("provider request missing response_format: %+v", req)
	}
	if req.Extensions.ResponseFormat.Type != "json_schema" {
		t.Fatalf("response_format type = %q, want json_schema", req.Extensions.ResponseFormat.Type)
	}
	if req.Extensions.ProviderRouting == nil || req.Extensions.ProviderRouting.RequireParameters == nil || !*req.Extensions.ProviderRouting.RequireParameters {
		t.Fatalf("provider routing = %+v, want require_parameters=true", req.Extensions.ProviderRouting)
	}
}

func TestOpenAIResponsesOfficialFunctionToolShapeMapsToProvider(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	provider := registerMockProvider(t, api, "tool-ready")
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	upsertOpenAIDirectModelRoute(t, store)

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-test",
		"input":"Use lookup",
		"tools":[{
			"type":"function",
			"name":"lookup",
			"description":"Lookup data",
			"parameters":{"type":"object","properties":{"query":{"type":"string"}}},
			"strict":true
		}],
		"tool_choice":"auto"
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	if provider.lastRequest == nil || len(provider.lastRequest.Tools) != 1 {
		t.Fatalf("provider tools = %+v", provider.lastRequest)
	}
	tool := provider.lastRequest.Tools[0]
	if tool.Type != "function" || tool.Function.Name != "lookup" || !tool.Function.Strict {
		t.Fatalf("tool = %+v, want normalized function lookup with strict=true", tool)
	}
	if tool.Function.Parameters["type"] != "object" {
		t.Fatalf("parameters = %+v", tool.Function.Parameters)
	}
	if provider.lastRequest.ToolChoice != "auto" {
		t.Fatalf("tool_choice = %#v, want auto", provider.lastRequest.ToolChoice)
	}
}

func TestOpenAIChatParallelToolCallsMapsToProvider(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	provider := registerMockProvider(t, api, "tool-ready")
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	upsertOpenAIDirectModelRoute(t, store)

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"gpt-test",
		"messages":[{"role":"user","content":"Use lookup"}],
		"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],
		"tool_choice":"auto",
		"parallel_tool_calls":false
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	if provider.lastRequest == nil || provider.lastRequest.ParallelToolCalls == nil || *provider.lastRequest.ParallelToolCalls {
		t.Fatalf("parallel_tool_calls = %+v, want false", provider.lastRequest)
	}
}

func TestOpenAIResponsesParallelToolCallsMapsToProvider(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	provider := registerMockProvider(t, api, "tool-ready")
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	upsertOpenAIDirectModelRoute(t, store)

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-test",
		"input":"Use lookup",
		"tools":[{
			"type":"function",
			"name":"lookup",
			"parameters":{"type":"object"}
		}],
		"tool_choice":"auto",
		"parallel_tool_calls":false
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	if provider.lastRequest == nil || provider.lastRequest.ParallelToolCalls == nil || *provider.lastRequest.ParallelToolCalls {
		t.Fatalf("parallel_tool_calls = %+v, want false", provider.lastRequest)
	}
}

func TestOpenAIRequestsPassSessionIDToProvider(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{
			name:   "chat",
			method: http.MethodPost,
			path:   "/v1/chat/completions",
			body:   `{"model":"gpt-test","messages":[{"role":"user","content":"Hello"}],"session_id":"sess-api-123"}`,
		},
		{
			name:   "responses",
			method: http.MethodPost,
			path:   "/v1/responses",
			body:   `{"model":"gpt-test","input":"Hello","session_id":"sess-api-123"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, store := setupWorkflowAPI(t)
			provider := registerMockProvider(t, api, "ok")
			key := createOpenAITestKey(t, store, "sk-consortium-test_123")
			upsertOpenAIDirectModelRoute(t, store)

			router := mux.NewRouter()
			api.RegisterRoutes(router)
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer "+key)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
			}
			if provider.lastRequest == nil || provider.lastRequest.Extensions == nil || provider.lastRequest.Extensions.SessionID != "sess-api-123" {
				t.Fatalf("provider request = %+v, want session_id passthrough", provider.lastRequest)
			}
		})
	}
}

func TestOpenAIResponsesRejectsMalformedFunctionTool(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	registerMockProvider(t, api, "unused")
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	upsertOpenAIDirectModelRoute(t, store)

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-test",
		"input":"Use lookup",
		"tools":[{"type":"function","parameters":{"type":"object"}}]
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body=%s", w.Code, w.Body.String())
	}
	assertOpenAIErrorEnvelope(t, w, "invalid_function_tool")
}

func TestOpenAIUsageMapsOpenRouterTokenDetails(t *testing.T) {
	usage := chatUsageFromWorkflowResult(&workflow.WorkflowResult{
		TotalInputTokens:  10,
		TotalOutputTokens: 5,
		TotalTokens:       15,
		NodeResults: []*workflow.NodeResult{
			{
				Metadata: map[string]interface{}{
					"openrouter_cached_tokens":    4,
					"openrouter_reasoning_tokens": 3,
				},
			},
		},
	}, 0, "hello")

	if usage.PromptTokensDetails.CachedTokens != 4 {
		t.Fatalf("cached_tokens = %d, want 4", usage.PromptTokensDetails.CachedTokens)
	}
	if usage.CompletionTokensDetails.ReasoningTokens != 3 {
		t.Fatalf("reasoning_tokens = %d, want 3", usage.CompletionTokensDetails.ReasoningTokens)
	}

	responsesUsage := openAIResponsesUsageMap(usage)
	inputDetails := responsesUsage["input_tokens_details"].(map[string]int)
	outputDetails := responsesUsage["output_tokens_details"].(map[string]int)
	if inputDetails["cached_tokens"] != 4 || outputDetails["reasoning_tokens"] != 3 {
		t.Fatalf("Responses usage = %#v", responsesUsage)
	}
}

func TestOpenAIChatReasoningEffortMapsToOpenRouterReasoning(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	provider := registerMockProvider(t, api, "reasoned")
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	upsertOpenAIDirectModelRoute(t, store)

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"gpt-test",
		"messages":[{"role":"user","content":"Think carefully"}],
		"reasoning_effort":"high"
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	if provider.lastRequest == nil || provider.lastRequest.Extensions == nil || provider.lastRequest.Extensions.Reasoning == nil {
		t.Fatalf("provider request missing reasoning: %+v", provider.lastRequest)
	}
	if provider.lastRequest.Extensions.Reasoning.Effort != "high" {
		t.Fatalf("reasoning effort = %q, want high", provider.lastRequest.Extensions.Reasoning.Effort)
	}
}

func TestOpenAIChatAcceptsCurrentReasoningEffortValues(t *testing.T) {
	for _, effort := range []string{"minimal", "xhigh"} {
		t.Run(effort, func(t *testing.T) {
			api, store := setupWorkflowAPI(t)
			provider := registerMockProvider(t, api, "reasoned")
			key := createOpenAITestKey(t, store, "sk-consortium-test_123")
			upsertOpenAIDirectModelRoute(t, store)

			router := mux.NewRouter()
			api.RegisterRoutes(router)
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(fmt.Sprintf(`{
				"model":"gpt-test",
				"messages":[{"role":"user","content":"Think carefully"}],
				"reasoning_effort":%q
			}`, effort)))
			req.Header.Set("Authorization", "Bearer "+key)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
			}
			if provider.lastRequest == nil || provider.lastRequest.Extensions == nil || provider.lastRequest.Extensions.Reasoning == nil {
				t.Fatalf("provider request missing reasoning: %+v", provider.lastRequest)
			}
			if provider.lastRequest.Extensions.Reasoning.Effort != effort {
				t.Fatalf("reasoning effort = %q, want %q", provider.lastRequest.Extensions.Reasoning.Effort, effort)
			}
		})
	}
}

func TestOpenAIResponsesAcceptsCurrentReasoningEffortValues(t *testing.T) {
	for _, effort := range []string{"minimal", "xhigh"} {
		t.Run(effort, func(t *testing.T) {
			api, store := setupWorkflowAPI(t)
			provider := registerMockProvider(t, api, "reasoned")
			key := createOpenAITestKey(t, store, "sk-consortium-test_123")
			upsertOpenAIDirectModelRoute(t, store)

			router := mux.NewRouter()
			api.RegisterRoutes(router)
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(fmt.Sprintf(`{
				"model":"gpt-test",
				"input":"Think carefully",
				"reasoning":{"effort":%q}
			}`, effort)))
			req.Header.Set("Authorization", "Bearer "+key)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
			}
			if provider.lastRequest == nil || provider.lastRequest.Extensions == nil || provider.lastRequest.Extensions.Reasoning == nil {
				t.Fatalf("provider request missing reasoning: %+v", provider.lastRequest)
			}
			if provider.lastRequest.Extensions.Reasoning.Effort != effort {
				t.Fatalf("reasoning effort = %q, want %q", provider.lastRequest.Extensions.Reasoning.Effort, effort)
			}
		})
	}
}

func TestOpenAIResponsesRejectsUnsupportedMultimodalInput(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-test",
		"input":[{"role":"user","content":[{"type":"input_image","image_url":"https://example.com/image.png"}]}]
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "unsupported input part type") {
		t.Fatalf("body = %s, want unsupported input part error", w.Body.String())
	}
	rows, err := store.ListAPIUsage(storage.APIUsageFilters{KeyID: "key-test", Limit: 10})
	if err != nil {
		t.Fatalf("ListAPIUsage: %v", err)
	}
	if len(rows) != 1 || rows[0].Status != storage.APIUsageStatusFailed || rows[0].ErrorCode != "invalid_input" {
		t.Fatalf("usage rows = %+v, want recorded invalid_input failure", rows)
	}
}

func TestOpenAIResponsesIdempotencyReplaysResponse(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	provider := registerMockProvider(t, api, "Responses replay output")
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	if err := store.UpsertAPIModelRoute(&storage.APIModelRoute{
		APIModel:      "gpt-test",
		Mode:          storage.APIModelRouteModeDirectModel,
		ProviderModel: "mock-model",
		IsDefault:     true,
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpsertAPIModelRoute: %v", err)
	}

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	body := `{"model":"gpt-test","input":"Say hello"}`
	call := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Idempotency-Key", "idem-responses")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	first := call()
	second := call()
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("statuses = %d/%d bodies=%s // %s", first.Code, second.Code, first.Body.String(), second.Body.String())
	}
	if first.Body.String() != second.Body.String() {
		t.Fatalf("expected byte-identical Responses replay\nfirst=%s\nsecond=%s", first.Body.String(), second.Body.String())
	}
	if provider.CallCount() != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.CallCount())
	}
	rows, err := store.ListAPIUsage(storage.APIUsageFilters{KeyID: "key-test", Limit: 10})
	if err != nil {
		t.Fatalf("ListAPIUsage: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want 1 rows=%+v", len(rows), rows)
	}
}

func TestOpenAIStoreFalseDoesNotPersistIdempotencyResponseBody(t *testing.T) {
	for _, tc := range []struct {
		name       string
		endpoint   string
		body       string
		outputText string
	}{
		{
			name:       "chat",
			endpoint:   openAIUsageEndpointChat,
			body:       `{"model":"gpt-test","messages":[{"role":"user","content":"Say hello"}],"store":false}`,
			outputText: "private chat output",
		},
		{
			name:       "responses",
			endpoint:   openAIUsageEndpointResponses,
			body:       `{"model":"gpt-test","input":"Say hello","store":false}`,
			outputText: "private responses output",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api, store := setupWorkflowAPI(t)
			registerMockProvider(t, api, tc.outputText)
			key := createOpenAITestKey(t, store, "sk-consortium-test_123")
			upsertOpenAIDirectModelRoute(t, store)

			router := mux.NewRouter()
			api.RegisterRoutes(router)
			req := httptest.NewRequest(http.MethodPost, tc.endpoint, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer "+key)
			req.Header.Set("Idempotency-Key", "store-false-"+tc.name)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), tc.outputText) {
				t.Fatalf("response body = %s, want generated output", w.Body.String())
			}

			idempotencyKey := openAIAPIIdempotencyStorageKey(tc.endpoint, "store-false-"+tc.name)
			idem, err := store.GetAPIIdempotency("key-test", idempotencyKey)
			if err == nil && strings.Contains(idem.ResponseBody, tc.outputText) {
				t.Fatalf("store:false idempotency row retained output text: %+v", idem)
			}
			if err != nil && !errors.Is(err, storage.ErrNotFound) {
				t.Fatalf("GetAPIIdempotency: %v", err)
			}
		})
	}
}

func TestOpenAIResponsesStoredObjectLifecycle(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	provider := registerMockProvider(t, api, "First output")
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	otherKey := createOpenAITestKeyWithID(t, store, "key-other", "other", "sk-consortium-other_123", 100, 100000)
	upsertOpenAIDirectModelRoute(t, store)

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	createBody := `{"model":"gpt-test","input":"Say hello","metadata":{"tenant":"acme"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	responseID, _ := created["id"].(string)
	if responseID == "" {
		t.Fatalf("missing response id: %#v", created)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/responses/"+responseID, nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("retrieve status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	var retrieved map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &retrieved); err != nil {
		t.Fatalf("decode retrieve: %v", err)
	}
	if retrieved["id"] != responseID || retrieved["output_text"] != "First output" {
		t.Fatalf("retrieved = %#v", retrieved)
	}
	if metadata, ok := retrieved["metadata"].(map[string]any); !ok || metadata["tenant"] != "acme" {
		t.Fatalf("metadata = %#v, want tenant", retrieved["metadata"])
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/responses/"+responseID+"/cancel", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("cancel completed response status = %d, want 409 body=%s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/responses/"+responseID, nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("retrieve after rejected cancel status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &retrieved); err != nil {
		t.Fatalf("decode retrieve after rejected cancel: %v", err)
	}
	if retrieved["status"] != "completed" {
		t.Fatalf("retrieve after rejected cancel = %#v, want completed", retrieved)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/responses/"+responseID+"/input_items?limit=1", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("input_items status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	var list map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode input_items: %v", err)
	}
	data := list["data"].([]any)
	if list["object"] != "list" || len(data) != 1 || list["first_id"] == "" || list["last_id"] == nil {
		t.Fatalf("input_items list = %#v", list)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/responses/"+responseID, nil)
	req.Header.Set("Authorization", "Bearer "+otherKey)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-key retrieve status = %d, want 404 body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-test","input":"Forget me","store":false}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("store false create status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	var transient map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &transient); err != nil {
		t.Fatalf("decode store false create: %v", err)
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/responses/"+transient["id"].(string), nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("store false retrieve status = %d, want 404 body=%s", w.Code, w.Body.String())
	}

	provider.response = "Second output"
	req = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-test","input":"Continue","previous_response_id":"`+responseID+`"}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("previous response status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	if provider.lastRequest == nil || len(provider.lastRequest.Messages) == 0 || !strings.Contains(provider.lastRequest.Messages[0].Content, "First output") {
		t.Fatalf("provider request missing previous output context: %+v", provider.lastRequest)
	}
	var second map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &second); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	secondID, _ := second["id"].(string)
	if secondID == "" {
		t.Fatalf("missing second response id: %#v", second)
	}

	provider.response = "Third output"
	req = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-test","input":"Continue again","previous_response_id":"`+secondID+`"}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("previous response chain status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	if provider.lastRequest == nil || len(provider.lastRequest.Messages) == 0 {
		t.Fatalf("provider request missing for chained previous response")
	}
	chainedPrompt := provider.lastRequest.Messages[0].Content
	for _, want := range []string{"First output", "Second output", "Continue again"} {
		if !strings.Contains(chainedPrompt, want) {
			t.Fatalf("chained previous response prompt missing %q:\n%s", want, chainedPrompt)
		}
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-test","input":"Steal","previous_response_id":"`+responseID+`"}`))
	req.Header.Set("Authorization", "Bearer "+otherKey)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-key previous_response_id status = %d, want 404 body=%s", w.Code, w.Body.String())
	}
}

func TestOpenAIResponsesDirectModelReturnsFunctionCallItems(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	provider := registerMockProvider(t, api, "")
	provider.toolCalls = []providers.ToolCall{
		{
			ID:   "call_lookup",
			Type: "function",
			Function: providers.ToolCallFunction{
				Name:      "lookup",
				Arguments: `{"query":"hello"}`,
			},
		},
	}
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	upsertOpenAIDirectModelRoute(t, store)

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-test",
		"input":"Call a tool",
		"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["output_text"] != "" {
		t.Fatalf("output_text = %#v, want empty for function-call-only response", payload["output_text"])
	}
	output, ok := payload["output"].([]any)
	if !ok || len(output) != 1 {
		t.Fatalf("output = %#v, want one function_call item", payload["output"])
	}
	item, ok := output[0].(map[string]any)
	if !ok {
		t.Fatalf("output item = %#v, want object", output[0])
	}
	if item["type"] != "function_call" || item["call_id"] != "call_lookup" || item["name"] != "lookup" || item["arguments"] != `{"query":"hello"}` {
		t.Fatalf("function_call item = %#v", item)
	}
	if item["status"] != "completed" || item["id"] == "" {
		t.Fatalf("function_call item status/id = %#v", item)
	}

	responseID, _ := payload["id"].(string)
	items, _, err := store.ListOpenAIObjectItems(responseID, "key-test", storage.OpenAIItemKindOutput, storage.OpenAIListPageRequest{Limit: 10, Order: storage.OpenAIListOrderAsc})
	if err != nil {
		t.Fatalf("ListOpenAIObjectItems: %v", err)
	}
	if len(items) != 1 || items[0].Role != "" || items[0].OpenAIItemID == "" || !strings.Contains(items[0].RawJSON, `"type":"function_call"`) {
		t.Fatalf("stored output items = %+v", items)
	}
}

func TestOpenAIResponsesFunctionCallOutputMustMatchPreviousCallID(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	provider := registerMockProvider(t, api, "")
	provider.toolCalls = []providers.ToolCall{
		{
			ID:   "call_lookup",
			Type: "function",
			Function: providers.ToolCallFunction{
				Name:      "lookup",
				Arguments: `{"query":"hello"}`,
			},
		},
	}
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	upsertOpenAIDirectModelRoute(t, store)

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-test",
		"input":"Call a tool",
		"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("initial status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode initial response: %v", err)
	}
	responseID, _ := created["id"].(string)
	if responseID == "" {
		t.Fatalf("missing response id: %#v", created)
	}

	provider.toolCalls = nil
	provider.response = "final"
	req = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-test",
		"previous_response_id":"`+responseID+`",
		"input":[{"type":"function_call_output","call_id":"call_missing","output":"{}"}]
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("mismatched call_id status = %d, want 400 body=%s", w.Code, w.Body.String())
	}
	assertOpenAIErrorEnvelope(t, w, "invalid_function_call_output")

	req = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-test",
		"previous_response_id":"`+responseID+`",
		"input":[{"type":"function_call_output","call_id":"call_lookup","output":"{\"answer\":\"world\"}"}]
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("matching call_id status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	if provider.lastRequest == nil || len(provider.lastRequest.Messages) == 0 {
		t.Fatalf("provider request missing for continuation")
	}
	continuationPrompt := provider.lastRequest.Messages[0].Content
	for _, want := range []string{
		"ASSISTANT_FUNCTION_CALL call_id=call_lookup name=lookup arguments={\"query\":\"hello\"}",
		"FUNCTION_CALL_OUTPUT call_id=call_lookup output={\"answer\":\"world\"}",
	} {
		if !strings.Contains(continuationPrompt, want) {
			t.Fatalf("continuation prompt missing %q:\n%s", want, continuationPrompt)
		}
	}
	var continued map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &continued); err != nil {
		t.Fatalf("decode continued response: %v", err)
	}
	continuedID, _ := continued["id"].(string)
	inputItems, _, err := store.ListOpenAIObjectItems(continuedID, "key-test", storage.OpenAIItemKindInput, storage.OpenAIListPageRequest{Limit: 10, Order: storage.OpenAIListOrderAsc})
	if err != nil {
		t.Fatalf("ListOpenAIObjectItems continued: %v", err)
	}
	if len(inputItems) != 1 || inputItems[0].Role != "" || !strings.Contains(inputItems[0].RawJSON, `"type":"function_call_output"`) || !strings.Contains(inputItems[0].RawJSON, `"call_id":"call_lookup"`) {
		t.Fatalf("continued input items = %+v", inputItems)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-test",
		"input":[{"type":"function_call_output","output":"{}"}]
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing call_id status = %d, want 400 body=%s", w.Code, w.Body.String())
	}
	assertOpenAIErrorEnvelope(t, w, "invalid_function_call_output")
}

func TestOpenAIResponsesManualFunctionCallContextIsAccepted(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	provider := registerMockProvider(t, api, "manual final")
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	upsertOpenAIDirectModelRoute(t, store)

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-test",
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"Need a lookup"}]},
			{"type":"function_call","call_id":"call_lookup","name":"lookup","arguments":"{\"query\":\"hello\"}","status":"completed"},
			{"type":"function_call_output","call_id":"call_lookup","output":"{\"answer\":\"world\"}"},
			{"type":"message","role":"user","content":"Summarize it"}
		]
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	if provider.lastRequest == nil || len(provider.lastRequest.Messages) == 0 {
		t.Fatalf("provider request missing for manual function-call context")
	}
	prompt := provider.lastRequest.Messages[0].Content
	for _, want := range []string{
		"Need a lookup",
		"ASSISTANT_FUNCTION_CALL call_id=call_lookup name=lookup arguments={\"query\":\"hello\"}",
		"FUNCTION_CALL_OUTPUT call_id=call_lookup output={\"answer\":\"world\"}",
		"Summarize it",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("manual function-call prompt missing %q:\n%s", want, prompt)
		}
	}

	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	responseID, _ := payload["id"].(string)
	inputItems, _, err := store.ListOpenAIObjectItems(responseID, "key-test", storage.OpenAIItemKindInput, storage.OpenAIListPageRequest{Limit: 10, Order: storage.OpenAIListOrderAsc})
	if err != nil {
		t.Fatalf("ListOpenAIObjectItems: %v", err)
	}
	if len(inputItems) != 4 ||
		!strings.Contains(inputItems[0].RawJSON, `"type":"message"`) ||
		!strings.Contains(inputItems[1].RawJSON, `"type":"function_call"`) ||
		!strings.Contains(inputItems[2].RawJSON, `"type":"function_call_output"`) ||
		!strings.Contains(inputItems[3].RawJSON, `"type":"message"`) {
		t.Fatalf("stored manual input items = %+v", inputItems)
	}

	provider.response = "manual follow-up"
	req = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-test",
		"previous_response_id":"`+responseID+`",
		"input":[{"type":"function_call_output","call_id":"call_lookup","output":"{\"answer\":\"again\"}"}]
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("prior manual function_call match status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
}

func TestOpenAIResponsesRejectsUnmatchedFunctionCallOutput(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	registerMockProvider(t, api, "unused")
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	upsertOpenAIDirectModelRoute(t, store)

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-test",
		"input":[{"type":"function_call_output","call_id":"call_missing","output":"{}"}]
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body=%s", w.Code, w.Body.String())
	}
	assertOpenAIErrorEnvelope(t, w, "invalid_function_call_output")
}

func TestOpenAIResponsesPreviousResponseIDRequiresCompletedResponse(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	registerMockProvider(t, api, "unused")
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	upsertOpenAIDirectModelRoute(t, store)
	now := time.Now().UTC()

	for _, tc := range []struct {
		name       string
		objectType string
		status     string
		wantStatus int
		wantCode   string
	}{
		{name: "wrong object type", objectType: storage.OpenAIObjectTypeChatCompletion, status: storage.OpenAIObjectStatusCompleted, wantStatus: http.StatusNotFound, wantCode: "previous_response_not_found"},
		{name: "in progress response", objectType: storage.OpenAIObjectTypeResponse, status: storage.OpenAIObjectStatusInProgress, wantStatus: http.StatusBadRequest, wantCode: "invalid_previous_response_id"},
		{name: "failed response", objectType: storage.OpenAIObjectTypeResponse, status: storage.OpenAIObjectStatusFailed, wantStatus: http.StatusBadRequest, wantCode: "invalid_previous_response_id"},
		{name: "cancelled response", objectType: storage.OpenAIObjectTypeResponse, status: storage.OpenAIObjectStatusCancelled, wantStatus: http.StatusBadRequest, wantCode: "invalid_previous_response_id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previousID := "resp-prev-" + strings.ReplaceAll(tc.name, " ", "-")
			if err := store.CreateOpenAIObject(&storage.OpenAIObjectRecord{
				ID:           previousID,
				ObjectType:   tc.objectType,
				KeyID:        "key-test",
				UserID:       "system",
				Endpoint:     openAIUsageEndpointResponses,
				Status:       tc.status,
				Store:        true,
				RequestJSON:  `{}`,
				ResponseJSON: `{"id":"` + previousID + `","object":"response","status":"` + tc.status + `"}`,
				MetadataJSON: `{}`,
				UsageJSON:    `{}`,
				CreatedAt:    now,
				UpdatedAt:    now,
				CompletedAt:  &now,
			}); err != nil {
				t.Fatalf("CreateOpenAIObject: %v", err)
			}

			router := mux.NewRouter()
			api.RegisterRoutes(router)
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-test","input":"Continue","previous_response_id":"`+previousID+`"}`))
			req.Header.Set("Authorization", "Bearer "+key)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d body=%s", w.Code, tc.wantStatus, w.Body.String())
			}
			assertOpenAIErrorEnvelope(t, w, tc.wantCode)
		})
	}
}

func TestOpenAIResponsesCancelScopesToKeyAndCancelsJob(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	otherKey := createOpenAITestKeyWithID(t, store, "key-other", "other", "sk-consortium-other_123", 100, 100000)
	job := createTestJob(t, store, events.JobStatusPending)
	now := time.Now().UTC()
	if err := store.CreateOpenAIObject(&storage.OpenAIObjectRecord{
		ID:             "resp-cancel-test",
		ObjectType:     storage.OpenAIObjectTypeResponse,
		KeyID:          "key-test",
		UserID:         "system",
		Endpoint:       "/v1/responses",
		JobID:          job.ID,
		RequestedModel: "gpt-test",
		Status:         storage.OpenAIObjectStatusInProgress,
		Store:          true,
		Background:     true,
		ResponseJSON:   `{"id":"resp-cancel-test","object":"response","status":"in_progress","model":"gpt-test"}`,
		CreatedAt:      now,
		UpdatedAt:      now,
	}); err != nil {
		t.Fatalf("CreateOpenAIObject: %v", err)
	}

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses/resp-cancel-test/cancel", nil)
	req.Header.Set("Authorization", "Bearer "+otherKey)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-key cancel status = %d, want 404 body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/responses/resp-cancel-test/cancel", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	got, err := store.GetExecution(job.ID)
	if err != nil {
		t.Fatalf("GetExecution: %v", err)
	}
	if got.Status != events.JobStatusCancelled {
		t.Fatalf("job status = %s, want cancelled", got.Status)
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode cancel: %v", err)
	}
	if payload["status"] != "cancelled" {
		t.Fatalf("cancel payload = %#v", payload)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/responses/resp-cancel-test/cancel", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second cancel status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode second cancel: %v", err)
	}
	if payload["status"] != "cancelled" {
		t.Fatalf("second cancel payload = %#v", payload)
	}
}

func TestOpenAIResponsesRejectsBackgroundStreamBeforeJob(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	registerMockProvider(t, api, "unused")
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	upsertOpenAIDirectModelRoute(t, store)

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-test",
		"input":"Run in background",
		"background":true,
		"stream":true
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body=%s", w.Code, w.Body.String())
	}
	assertOpenAIErrorEnvelope(t, w, "unsupported_background_stream")

	usageRows, err := store.ListAPIUsage(storage.APIUsageFilters{KeyID: "key-test", Limit: 10})
	if err != nil {
		t.Fatalf("ListAPIUsage: %v", err)
	}
	if len(usageRows) != 1 || usageRows[0].Status != storage.APIUsageStatusFailed || usageRows[0].ErrorCode != "unsupported_background_stream" {
		t.Fatalf("usage rows = %+v, want recorded validation failure", usageRows)
	}

	jobs, err := store.ListExecutions(10)
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("jobs = %+v, want no job created for unsupported background stream", jobs)
	}
}

func TestOpenAIResponsesBackgroundReturnsInProgressAndLaterRetrievesCompleted(t *testing.T) {
	db, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	defer db.Close()
	registry := providers.NewRegistry()
	registry.Register(&SlowMockProvider{
		name: "mock",
		models: []providers.Model{
			{ID: "mock-model", Provider: "mock", InputCost: 0.000001, OutputCost: 0.000002},
		},
		delay: 80 * time.Millisecond,
	})
	manager := newTestJobManager(db, registry)
	manager.StartWorkers()
	defer manager.StopWorkers(context.Background())
	api := NewWorkflowAPI(db, registry, manager)
	key := createOpenAITestKey(t, db, "sk-consortium-test_123")
	upsertOpenAIDirectModelRoute(t, db)

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	body := `{
		"model":"gpt-test",
		"input":"Run in background",
		"background":true
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Idempotency-Key", "background-test")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	responseID, _ := created["id"].(string)
	if responseID == "" || created["status"] != "in_progress" || created["background"] != true {
		t.Fatalf("created = %#v, want in_progress background response", created)
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/responses/"+responseID+"/input_items", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("initial input_items status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	var initialItems map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &initialItems); err != nil {
		t.Fatalf("decode initial input_items: %v", err)
	}
	initialData := initialItems["data"].([]any)
	if len(initialData) != 1 || initialItems["first_id"] == "" {
		t.Fatalf("initial input_items = %#v", initialItems)
	}
	initialPublicItemID, _ := initialData[0].(map[string]any)["id"].(string)
	initialStorageItemID, _ := initialItems["first_id"].(string)

	deadline := time.Now().Add(2 * time.Second)
	for {
		req := httptest.NewRequest(http.MethodGet, "/v1/responses/"+responseID, nil)
		req.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("retrieve status = %d, want 200 body=%s", w.Code, w.Body.String())
		}
		var got map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode retrieve: %v", err)
		}
		if got["status"] == "completed" {
			if got["output_text"] != "Slow response" {
				t.Fatalf("completed response = %#v", got)
			}
			req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+key)
			req.Header.Set("Idempotency-Key", "background-test")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("idempotent replay status = %d, want 200 body=%s", w.Code, w.Body.String())
			}
			var replayed map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &replayed); err != nil {
				t.Fatalf("decode idempotent replay: %v", err)
			}
			if replayed["id"] != responseID || replayed["status"] != "completed" {
				t.Fatalf("idempotent replay = %#v, want completed original response", replayed)
			}
			req = httptest.NewRequest(http.MethodGet, "/v1/responses/"+responseID+"/input_items", nil)
			req.Header.Set("Authorization", "Bearer "+key)
			w = httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("completed input_items status = %d, want 200 body=%s", w.Code, w.Body.String())
			}
			var completedItems map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &completedItems); err != nil {
				t.Fatalf("decode completed input_items: %v", err)
			}
			completedData := completedItems["data"].([]any)
			completedPublicItemID, _ := completedData[0].(map[string]any)["id"].(string)
			completedStorageItemID, _ := completedItems["first_id"].(string)
			if completedPublicItemID != initialPublicItemID || completedStorageItemID != initialStorageItemID {
				t.Fatalf("background input item IDs changed: initial public=%q storage=%q completed public=%q storage=%q",
					initialPublicItemID, initialStorageItemID, completedPublicItemID, completedStorageItemID)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("response did not complete before deadline: %s", w.Body.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestOpenAIResponsesRetrieveReconcilesTerminalBackgroundJob(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	now := time.Now().UTC()
	jobID := "job-openai-background-reconcile"
	responseID := "resp-openai-background-reconcile"
	workflowID := "wf-openai-background-reconcile"

	if err := store.CreateExecution(&storage.WorkflowExecution{
		ID:         jobID,
		Status:     events.JobStatusRunning,
		Model:      "mock-model",
		WorkflowID: workflowID,
	}); err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	if err := store.LogLLMRequestFull(&storage.LLMRequestLog{
		JobID:         jobID,
		NodeID:        "completion",
		Model:         "mock-model",
		Prompt:        "Hello",
		Response:      "Recovered background output",
		TokensIn:      3,
		TokensOut:     4,
		Status:        events.JobStatusCompleted,
		AttemptNumber: 1,
	}); err != nil {
		t.Fatalf("LogLLMRequestFull: %v", err)
	}
	if err := store.CompleteExecution(jobID, events.JobStatusCompleted, "Recovered background output", 0.01, 3, 4, 7, ""); err != nil {
		t.Fatalf("CompleteExecution: %v", err)
	}

	initialPayload := openAIResponseObject(responseID, "gpt-test", now.Unix(), storage.OpenAIObjectStatusInProgress, "", nil, nil)
	initialPayload["store"] = true
	initialPayload["background"] = true
	initialPayload["metadata"] = map[string]any{}
	initialJSON, _ := json.Marshal(initialPayload)
	requestJSON := `{"model":"gpt-test","input":"Hello","background":true}`
	if err := store.CreateOpenAIObjectWithItems(&storage.OpenAIObjectRecord{
		ID:             responseID,
		ObjectType:     storage.OpenAIObjectTypeResponse,
		KeyID:          "key-test",
		UserID:         "system",
		Endpoint:       openAIUsageEndpointResponses,
		JobID:          jobID,
		RequestedModel: "gpt-test",
		ResolvedModel:  "mock-model",
		WorkflowID:     workflowID,
		Status:         storage.OpenAIObjectStatusInProgress,
		Store:          true,
		Background:     true,
		MetadataJSON:   `{}`,
		RequestJSON:    requestJSON,
		ResponseJSON:   string(initialJSON),
		UsageJSON:      `{}`,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, openAIResponseInputStoredItems(responseID, json.RawMessage(`"Hello"`), "Hello")); err != nil {
		t.Fatalf("CreateOpenAIObjectWithItems: %v", err)
	}
	if err := store.CreateAPIUsage(&storage.APIUsageRecord{
		ID:             "usage-openai-background-reconcile",
		RequestID:      "req-openai-background-reconcile",
		KeyID:          "key-test",
		UserID:         "system",
		Endpoint:       openAIUsageEndpointResponses,
		RequestedModel: "gpt-test",
		ResolvedModel:  "mock-model",
		WorkflowID:     workflowID,
		JobID:          jobID,
		Status:         storage.APIUsageStatusRunning,
		HTTPStatus:     http.StatusOK,
		CreatedAt:      now,
	}); err != nil {
		t.Fatalf("CreateAPIUsage: %v", err)
	}
	idempotencyKey := openAIAPIIdempotencyStorageKey(openAIUsageEndpointResponses, "background-reconcile")
	if _, _, err := store.ReserveAPIIdempotency(&storage.APIIdempotencyRecord{
		ID:                 "idem-openai-background-reconcile",
		KeyID:              "key-test",
		IdempotencyKey:     idempotencyKey,
		RequestFingerprint: "fingerprint",
		JobID:              jobID,
		CreatedAt:          now,
		ExpiresAt:          now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("ReserveAPIIdempotency: %v", err)
	}

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodGet, "/v1/responses/"+responseID, nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["status"] != storage.OpenAIObjectStatusCompleted || payload["output_text"] != "Recovered background output" {
		t.Fatalf("payload = %#v, want reconciled completed response", payload)
	}
	record, err := store.GetOpenAIObject(responseID, "key-test")
	if err != nil {
		t.Fatalf("GetOpenAIObject: %v", err)
	}
	if record.Status != storage.OpenAIObjectStatusCompleted {
		t.Fatalf("stored status = %q, want completed", record.Status)
	}
	usages, err := store.ListAPIUsage(storage.APIUsageFilters{KeyID: "key-test", Endpoint: openAIUsageEndpointResponses, Limit: 10})
	if err != nil {
		t.Fatalf("ListAPIUsage: %v", err)
	}
	if len(usages) != 1 || usages[0].Status != storage.APIUsageStatusSucceeded || usages[0].JobID != jobID || usages[0].TokensTotal != 7 {
		t.Fatalf("usage rows = %+v, want reconciled successful usage", usages)
	}
	idem, err := store.GetAPIIdempotency("key-test", idempotencyKey)
	if err != nil {
		t.Fatalf("GetAPIIdempotency: %v", err)
	}
	if !strings.Contains(idem.ResponseBody, "Recovered background output") || idem.HTTPStatus != http.StatusOK {
		t.Fatalf("idempotency = %+v, want reconciled completed response replay body", idem)
	}
}

func TestOpenAIBackgroundReconcilerSweepsTerminalJobs(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	now := time.Now().UTC()
	jobID := "job-openai-background-sweep"
	responseID := "resp-openai-background-sweep"
	workflowID := "wf-openai-background-sweep"

	if err := store.CreateExecution(&storage.WorkflowExecution{
		ID:         jobID,
		Status:     events.JobStatusRunning,
		Model:      "mock-model",
		WorkflowID: workflowID,
	}); err != nil {
		t.Fatalf("CreateExecution: %v", err)
	}
	if err := store.LogLLMRequestFull(&storage.LLMRequestLog{
		JobID:         jobID,
		NodeID:        "completion",
		Model:         "mock-model",
		Prompt:        "Hello",
		Response:      "Swept background output",
		TokensIn:      2,
		TokensOut:     5,
		Status:        events.JobStatusCompleted,
		AttemptNumber: 1,
	}); err != nil {
		t.Fatalf("LogLLMRequestFull: %v", err)
	}
	if err := store.CompleteExecution(jobID, events.JobStatusCompleted, "Swept background output", 0.02, 2, 5, 7, ""); err != nil {
		t.Fatalf("CompleteExecution: %v", err)
	}

	initialPayload := openAIResponseObject(responseID, "gpt-test", now.Unix(), storage.OpenAIObjectStatusInProgress, "", nil, nil)
	initialPayload["store"] = true
	initialPayload["background"] = true
	initialPayload["metadata"] = map[string]any{}
	initialJSON, _ := json.Marshal(initialPayload)
	if err := store.CreateOpenAIObjectWithItems(&storage.OpenAIObjectRecord{
		ID:             responseID,
		ObjectType:     storage.OpenAIObjectTypeResponse,
		KeyID:          "key-test",
		UserID:         "system",
		Endpoint:       openAIUsageEndpointResponses,
		JobID:          jobID,
		RequestedModel: "gpt-test",
		ResolvedModel:  "mock-model",
		WorkflowID:     workflowID,
		Status:         storage.OpenAIObjectStatusInProgress,
		Store:          true,
		Background:     true,
		MetadataJSON:   `{}`,
		RequestJSON:    `{"model":"gpt-test","input":"Hello","background":true}`,
		ResponseJSON:   string(initialJSON),
		UsageJSON:      `{}`,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, openAIResponseInputStoredItems(responseID, json.RawMessage(`"Hello"`), "Hello")); err != nil {
		t.Fatalf("CreateOpenAIObjectWithItems: %v", err)
	}
	if err := store.CreateAPIUsage(&storage.APIUsageRecord{
		ID:             "usage-openai-background-sweep",
		RequestID:      "req-openai-background-sweep",
		KeyID:          "key-test",
		UserID:         "system",
		Endpoint:       openAIUsageEndpointResponses,
		RequestedModel: "gpt-test",
		ResolvedModel:  "mock-model",
		WorkflowID:     workflowID,
		JobID:          jobID,
		Status:         storage.APIUsageStatusRunning,
		HTTPStatus:     http.StatusOK,
		CreatedAt:      now,
	}); err != nil {
		t.Fatalf("CreateAPIUsage: %v", err)
	}
	idempotencyKey := openAIAPIIdempotencyStorageKey(openAIUsageEndpointResponses, "background-sweep")
	if _, _, err := store.ReserveAPIIdempotency(&storage.APIIdempotencyRecord{
		ID:                 "idem-openai-background-sweep",
		KeyID:              "key-test",
		IdempotencyKey:     idempotencyKey,
		RequestFingerprint: "fingerprint",
		JobID:              jobID,
		CreatedAt:          now,
		ExpiresAt:          now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("ReserveAPIIdempotency: %v", err)
	}

	reconciled, err := api.reconcileTerminalOpenAIBackgroundResponses(context.Background(), 10)
	if err != nil {
		t.Fatalf("reconcileTerminalOpenAIBackgroundResponses: %v", err)
	}
	if reconciled != 1 {
		t.Fatalf("reconciled = %d, want 1", reconciled)
	}

	record, err := store.GetOpenAIObject(responseID, "key-test")
	if err != nil {
		t.Fatalf("GetOpenAIObject: %v", err)
	}
	if record.Status != storage.OpenAIObjectStatusCompleted || !strings.Contains(record.ResponseJSON, "Swept background output") {
		t.Fatalf("record = %+v, want swept completed object", record)
	}
	usages, err := store.ListAPIUsage(storage.APIUsageFilters{KeyID: "key-test", Endpoint: openAIUsageEndpointResponses, Limit: 10})
	if err != nil {
		t.Fatalf("ListAPIUsage: %v", err)
	}
	if len(usages) != 1 || usages[0].Status != storage.APIUsageStatusSucceeded || usages[0].TokensTotal != 7 {
		t.Fatalf("usage rows = %+v, want swept successful usage", usages)
	}
	idem, err := store.GetAPIIdempotency("key-test", idempotencyKey)
	if err != nil {
		t.Fatalf("GetAPIIdempotency: %v", err)
	}
	if !strings.Contains(idem.ResponseBody, "Swept background output") || idem.HTTPStatus != http.StatusOK {
		t.Fatalf("idempotency = %+v, want swept completed response replay body", idem)
	}
}

func TestOpenAIResponsesBackgroundCancelStaysCancelled(t *testing.T) {
	db, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	defer db.Close()
	registry := providers.NewRegistry()
	registry.Register(&SlowMockProvider{
		name: "mock",
		models: []providers.Model{
			{ID: "mock-model", Provider: "mock", InputCost: 0.000001, OutputCost: 0.000002},
		},
		delay: 200 * time.Millisecond,
	})
	manager := newTestJobManager(db, registry)
	manager.StartWorkers()
	defer manager.StopWorkers(context.Background())
	api := NewWorkflowAPI(db, registry, manager)
	key := createOpenAITestKey(t, db, "sk-consortium-test_123")
	upsertOpenAIDirectModelRoute(t, db)

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-test",
		"input":"Run in background",
		"background":true
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	responseID, _ := created["id"].(string)
	if responseID == "" {
		t.Fatalf("created response missing id: %#v", created)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/responses/"+responseID+"/cancel", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	time.Sleep(350 * time.Millisecond)

	req = httptest.NewRequest(http.MethodGet, "/v1/responses/"+responseID, nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("retrieve status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode retrieve: %v", err)
	}
	if got["status"] != "cancelled" {
		t.Fatalf("response status = %#v, want cancelled", got)
	}
	usages, err := db.ListAPIUsage(storage.APIUsageFilters{KeyID: "key-test", Endpoint: openAIUsageEndpointResponses, Limit: 10})
	if err != nil {
		t.Fatalf("ListAPIUsage: %v", err)
	}
	if len(usages) != 1 || usages[0].Status != storage.APIUsageStatusCancelled {
		t.Fatalf("usage rows = %+v, want one cancelled usage row", usages)
	}
}

func TestOpenAIChatCompletionsStreamSendsFirstFrameAndDone(t *testing.T) {
	db, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	defer db.Close()
	registry := providers.NewRegistry()
	registry.Register(&SlowMockProvider{
		name: "mock",
		models: []providers.Model{
			{ID: "mock-model", Provider: "mock", InputCost: 0.000001, OutputCost: 0.000002},
		},
		delay: 80 * time.Millisecond,
	})
	manager := newTestJobManager(db, registry)
	manager.StartWorkers()
	defer manager.StopWorkers(context.Background())
	api := NewWorkflowAPI(db, registry, manager)
	key := createOpenAITestKey(t, db, "sk-consortium-test_123")
	if err := db.UpsertAPIModelRoute(&storage.APIModelRoute{
		APIModel:      "gpt-test",
		Mode:          storage.APIModelRouteModeDirectModel,
		ProviderModel: "mock-model",
		IsDefault:     true,
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpsertAPIModelRoute: %v", err)
	}

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	server := httptest.NewServer(router)
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/chat/completions", strings.NewReader(`{
		"model":"gpt-test",
		"messages":[{"role":"user","content":"Say hello"}],
		"stream":true,
		"stream_options":{"include_usage":true}
	}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200 body=%s", resp.StatusCode, string(body))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	reader := bufio.NewReader(resp.Body)
	first, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read first SSE line: %v", err)
	}
	if !strings.HasPrefix(first, "data: ") || !strings.Contains(first, `"delta":{"role":"assistant"}`) {
		t.Fatalf("first line = %q, want assistant role chunk", first)
	}
	rest, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read rest: %v", err)
	}
	text := first + string(rest)
	if !strings.Contains(text, "data: [DONE]") {
		t.Fatalf("stream missing DONE: %s", text)
	}
	if !strings.Contains(text, `"usage"`) {
		t.Fatalf("stream missing usage chunk: %s", text)
	}
}

func TestOpenAIChatCompletionsStreamEmitsToolCalls(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	provider := registerMockProvider(t, api, "")
	provider.toolCalls = []providers.ToolCall{
		{
			ID:   "call_lookup",
			Type: "function",
			Function: providers.ToolCallFunction{
				Name:      "lookup",
				Arguments: `{"query":"hello"}`,
			},
		},
	}
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	if err := store.UpsertAPIModelRoute(&storage.APIModelRoute{
		APIModel:      "gpt-test",
		Mode:          storage.APIModelRouteModeDirectModel,
		ProviderModel: "mock-model",
		IsDefault:     true,
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpsertAPIModelRoute: %v", err)
	}

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"gpt-test",
		"messages":[{"role":"user","content":"Call a tool"}],
		"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}],
		"stream":true
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	text := w.Body.String()
	if !strings.Contains(text, `"tool_calls"`) || !strings.Contains(text, `"finish_reason":"tool_calls"`) {
		t.Fatalf("stream missing tool-call delta/finish reason: %s", text)
	}
}

func TestOpenAIChatCompletionsStreamCancelMarksUsageFailed(t *testing.T) {
	db, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	defer db.Close()
	registry := providers.NewRegistry()
	registry.Register(&SlowMockProvider{
		name: "mock",
		models: []providers.Model{
			{ID: "mock-model", Provider: "mock", InputCost: 0.000001, OutputCost: 0.000002},
		},
		delay: 500 * time.Millisecond,
	})
	manager := newTestJobManager(db, registry)
	manager.StartWorkers()
	defer manager.StopWorkers(context.Background())
	api := NewWorkflowAPI(db, registry, manager)
	key := createOpenAITestKey(t, db, "sk-consortium-test_123")
	if err := db.UpsertAPIModelRoute(&storage.APIModelRoute{
		APIModel:      "gpt-test",
		Mode:          storage.APIModelRouteModeDirectModel,
		ProviderModel: "mock-model",
		IsDefault:     true,
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpsertAPIModelRoute: %v", err)
	}

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	server := httptest.NewServer(router)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/chat/completions", strings.NewReader(`{
		"model":"gpt-test",
		"messages":[{"role":"user","content":"Say hello"}],
		"stream":true
	}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	reader := bufio.NewReader(resp.Body)
	first, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read first SSE line: %v", err)
	}
	if !strings.HasPrefix(first, "data: ") {
		t.Fatalf("first line = %q, want SSE data", first)
	}
	cancel()
	_ = resp.Body.Close()

	deadline := time.Now().Add(2 * time.Second)
	var rows []storage.APIUsageRecord
	for time.Now().Before(deadline) {
		rows, err = db.ListAPIUsage(storage.APIUsageFilters{KeyID: "key-test", Limit: 10})
		if err != nil {
			t.Fatalf("ListAPIUsage: %v", err)
		}
		if len(rows) == 1 && rows[0].Status != storage.APIUsageStatusRunning {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want 1 rows=%+v", len(rows), rows)
	}
	if rows[0].Status != storage.APIUsageStatusFailed || rows[0].HTTPStatus != http.StatusRequestTimeout || rows[0].ErrorCode != "request_cancelled" {
		t.Fatalf("usage row = %+v, want cancelled failure", rows[0])
	}
}

func TestOpenAIChatCompletionsDirectModelSuccessRecordsUsage(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	registerMockProvider(t, api, "Mock API response")
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	if err := store.UpsertAPIModelRoute(&storage.APIModelRoute{
		APIModel:      "gpt-test",
		Mode:          storage.APIModelRouteModeDirectModel,
		ProviderModel: "mock-model",
		IsDefault:     true,
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpsertAPIModelRoute: %v", err)
	}

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	body := []byte(`{
		"model":"gpt-test",
		"messages":[{"role":"developer","content":"Be terse."},{"role":"user","content":"Say hello"}],
		"max_completion_tokens":64,
		"temperature":0
	}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["object"] != "chat.completion" {
		t.Fatalf("object = %v, want chat.completion response=%#v", payload["object"], payload)
	}
	choices := payload["choices"].([]any)
	message := choices[0].(map[string]any)["message"].(map[string]any)
	if message["role"] != "assistant" || message["content"] != "Mock API response" {
		t.Fatalf("message = %#v, want assistant mock response", message)
	}
	usage := payload["usage"].(map[string]any)
	if usage["total_tokens"].(float64) <= 0 {
		t.Fatalf("usage = %#v, want positive total_tokens", usage)
	}

	rows, err := store.ListAPIUsage(storage.APIUsageFilters{KeyID: "key-test", Limit: 10})
	if err != nil {
		t.Fatalf("ListAPIUsage: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want 1 rows=%+v", len(rows), rows)
	}
	if rows[0].Status != storage.APIUsageStatusSucceeded || rows[0].TokensTotal <= 0 || rows[0].JobID == "" {
		t.Fatalf("usage row = %+v, want succeeded with totals/job", rows[0])
	}
}

func TestOpenAIChatCompletionsStoredResources(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	registerMockProvider(t, api, "Stored chat response")
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	otherKey := createOpenAITestKeyWithID(t, store, "key-other", "other", "sk-consortium-other_123", 100, 100000)
	upsertOpenAIDirectModelRoute(t, store)

	router := mux.NewRouter()
	api.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"gpt-test",
		"messages":[{"role":"user","content":"Do not store"}]
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("default create status = %d, want 200 body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"gpt-test",
		"messages":[{"role":"developer","content":"Be terse."},{"role":"user","content":"Store this"}],
		"store":true,
		"metadata":{"tenant":"acme"}
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("stored create status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	completionID, _ := created["id"].(string)
	if completionID == "" {
		t.Fatalf("missing completion id: %#v", created)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/chat/completions?limit=1", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	var list map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	data := list["data"].([]any)
	if len(data) != 1 || list["object"] != "list" || list["first_id"] == "" || list["last_id"] == nil {
		t.Fatalf("list = %#v", list)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/chat/completions/"+completionID, nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("retrieve status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	var retrieved map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &retrieved); err != nil {
		t.Fatalf("decode retrieve: %v", err)
	}
	if retrieved["id"] != completionID || retrieved["object"] != "chat.completion" {
		t.Fatalf("retrieved = %#v", retrieved)
	}
	if metadata, ok := retrieved["metadata"].(map[string]any); !ok || metadata["tenant"] != "acme" {
		t.Fatalf("metadata = %#v, want tenant", retrieved["metadata"])
	}

	for _, path := range []string{
		"/v1/responses/" + completionID,
		"/v1/responses/" + completionID + "/input_items",
	} {
		req = httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+key)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404 body=%s", path, w.Code, w.Body.String())
		}
	}
	req = httptest.NewRequest(http.MethodPost, "/v1/responses/"+completionID+"/cancel", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-type response cancel status = %d, want 404 body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/chat/completions/"+completionID+"/messages?limit=10", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("messages status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	var messages map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &messages); err != nil {
		t.Fatalf("decode messages: %v", err)
	}
	msgData := messages["data"].([]any)
	if len(msgData) != 3 || messages["object"] != "list" {
		t.Fatalf("messages = %#v", messages)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/chat/completions/"+completionID, nil)
	req.Header.Set("Authorization", "Bearer "+otherKey)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-key retrieve status = %d, want 404 body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/chat/completions/"+completionID+"/messages", nil)
	req.Header.Set("Authorization", "Bearer "+otherKey)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-key messages status = %d, want 404 body=%s", w.Code, w.Body.String())
	}
}

func TestOpenAIChatCompletionsDisabledExactRouteReturnsNotFound(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	provider := registerMockProvider(t, api, "Default route response")
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	if err := store.UpsertAPIModelRoute(&storage.APIModelRoute{
		APIModel:      "default-model",
		Mode:          storage.APIModelRouteModeDirectModel,
		ProviderModel: "mock-model",
		IsDefault:     true,
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpsertAPIModelRoute default: %v", err)
	}
	if err := store.UpsertAPIModelRoute(&storage.APIModelRoute{
		APIModel:      "gpt-disabled",
		Mode:          storage.APIModelRouteModeDirectModel,
		ProviderModel: "disabled-model",
		Enabled:       false,
	}); err != nil {
		t.Fatalf("UpsertAPIModelRoute disabled: %v", err)
	}

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"gpt-disabled",
		"messages":[{"role":"user","content":"Say hello"}]
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 body=%s", w.Code, w.Body.String())
	}
	if provider.lastRequest != nil {
		t.Fatalf("provider request = %+v, want no provider call", provider.lastRequest)
	}
}

func TestOpenAIChatCompletionsPreservesProviderHTTPStatus(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	provider := registerMockProvider(t, api, "")
	provider.shouldError = true
	provider.err = &providers.ProviderError{
		Code:       providers.ErrCodeRateLimited,
		StatusCode: http.StatusTooManyRequests,
		Message:    "provider rate limited",
		Provider:   "mock",
		Model:      "mock-model",
		ErrorPhase: "http_status",
	}
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	if err := store.UpsertAPIModelRoute(&storage.APIModelRoute{
		APIModel:      "gpt-test",
		Mode:          storage.APIModelRouteModeDirectModel,
		ProviderModel: "mock-model",
		IsDefault:     true,
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpsertAPIModelRoute: %v", err)
	}

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"gpt-test",
		"messages":[{"role":"user","content":"Say hello"}]
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "rate_limited") {
		t.Fatalf("body = %s, want provider error code", w.Body.String())
	}
	rows, err := store.ListAPIUsage(storage.APIUsageFilters{KeyID: "key-test", Limit: 10})
	if err != nil {
		t.Fatalf("ListAPIUsage: %v", err)
	}
	if len(rows) != 1 || rows[0].HTTPStatus != http.StatusTooManyRequests || rows[0].ErrorCode != "rate_limited" {
		t.Fatalf("usage rows = %+v, want provider 429 failure", rows)
	}
}

func TestOpenAIChatCompletionsSanitizesProviderErrorMessage(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	provider := registerMockProvider(t, api, "")
	provider.shouldError = true
	provider.err = &providers.ProviderError{
		Code:         providers.ErrCodeRateLimited,
		StatusCode:   http.StatusTooManyRequests,
		Message:      "raw provider secret should not surface",
		Provider:     "mock",
		Model:        "mock-model",
		ErrorType:    "provider_timeout",
		NativeCode:   "native_error",
		ProviderCode: "upstream_504",
		ErrorPhase:   "http_status",
	}
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	if err := store.UpsertAPIModelRoute(&storage.APIModelRoute{
		APIModel:      "gpt-test",
		Mode:          storage.APIModelRouteModeDirectModel,
		ProviderModel: "mock-model",
		IsDefault:     true,
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpsertAPIModelRoute: %v", err)
	}

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"gpt-test",
		"messages":[{"role":"user","content":"Say hello"}]
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, forbidden := range []string{"raw provider secret", "provider_timeout", "native_error", "upstream_504", "mock-model"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("body leaked provider detail %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, "rate limit exceeded") || !strings.Contains(body, "rate_limited") {
		t.Fatalf("body = %s, want stable sanitized rate-limit error", body)
	}
}

func TestOpenAIChatStreamingSanitizesProviderErrorMessage(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	provider := registerMockProvider(t, api, "")
	provider.shouldError = true
	provider.err = &providers.ProviderError{
		Code:         providers.ErrCodeRateLimited,
		StatusCode:   http.StatusTooManyRequests,
		Message:      "raw provider secret should not surface",
		Provider:     "mock",
		Model:        "mock-model",
		ErrorType:    "provider_timeout",
		NativeCode:   "native_error",
		ProviderCode: "upstream_504",
		ErrorPhase:   "http_status",
	}
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	upsertOpenAIDirectModelRoute(t, store)

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"gpt-test",
		"messages":[{"role":"user","content":"Say hello"}],
		"stream":true
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want streaming 200 body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, forbidden := range []string{"raw provider secret", "provider_timeout", "native_error", "upstream_504", "mock-model"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("stream body leaked provider detail %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, "rate limit exceeded") || !strings.Contains(body, "rate_limited") {
		t.Fatalf("body = %s, want stable sanitized rate-limit error", body)
	}
}

func TestOpenAIResponsesStreamingSanitizesProviderErrorMessage(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	provider := registerMockProvider(t, api, "")
	provider.shouldError = true
	provider.err = &providers.ProviderError{
		Code:         providers.ErrCodeRateLimited,
		StatusCode:   http.StatusTooManyRequests,
		Message:      "raw provider secret should not surface",
		Provider:     "mock",
		Model:        "mock-model",
		ErrorType:    "provider_timeout",
		NativeCode:   "native_error",
		ProviderCode: "upstream_504",
		ErrorPhase:   "http_status",
	}
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	upsertOpenAIDirectModelRoute(t, store)

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-test",
		"input":"Say hello",
		"stream":true
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want streaming 200 body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, forbidden := range []string{"raw provider secret", "provider_timeout", "native_error", "upstream_504", "mock-model"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("stream body leaked provider detail %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, "event: response.failed") || !strings.Contains(body, "rate limit exceeded") || !strings.Contains(body, "rate_limited") {
		t.Fatalf("body = %s, want stable sanitized response.failed event", body)
	}
}

func TestOpenAIStreamingSanitizesNonProviderExecutionErrorMessage(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	provider := registerMockProvider(t, api, "")
	provider.shouldError = true
	provider.err = errors.New("raw internal workflow secret should not surface")
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	upsertOpenAIDirectModelRoute(t, store)

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-test",
		"input":"Say hello",
		"stream":true
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want streaming 200 body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, "raw internal workflow secret") {
		t.Fatalf("stream body leaked internal execution detail: %s", body)
	}
	if !strings.Contains(body, "event: response.failed") || !strings.Contains(body, "workflow execution failed") {
		t.Fatalf("body = %s, want stable sanitized response.failed event", body)
	}
}

func TestOpenAIProviderStatusFromMetadataParsesEmbeddedStatus(t *testing.T) {
	status := openAIProviderStatusFromMetadata(map[string]interface{}{
		"provider_status_code": "HTTP 429 Too Many Requests",
	})
	if status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", status)
	}
}

func TestOpenAIChatCompletionsIdempotencyDoesNotReplayTransientFailure(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	provider := registerMockProvider(t, api, "Recovered response")
	provider.failCount = 1
	provider.err = &providers.ProviderError{
		Code:       providers.ErrCodeRateLimited,
		StatusCode: http.StatusTooManyRequests,
		Message:    "provider rate limited",
		Provider:   "mock",
		Model:      "mock-model",
		ErrorPhase: "http_status",
	}
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	if err := store.UpsertAPIModelRoute(&storage.APIModelRoute{
		APIModel:      "gpt-test",
		Mode:          storage.APIModelRouteModeDirectModel,
		ProviderModel: "mock-model",
		IsDefault:     true,
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpsertAPIModelRoute: %v", err)
	}

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	body := `{"model":"gpt-test","messages":[{"role":"user","content":"Say hello"}]}`
	call := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Idempotency-Key", "idem-transient")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	first := call()
	second := call()
	if first.Code != http.StatusTooManyRequests {
		t.Fatalf("first status = %d, want 429 body=%s", first.Code, first.Body.String())
	}
	if second.Code != http.StatusOK {
		t.Fatalf("second status = %d, want 200 body=%s", second.Code, second.Body.String())
	}
	if provider.CallCount() != 2 {
		t.Fatalf("provider calls = %d, want retry to execute second call", provider.CallCount())
	}
	if !strings.Contains(second.Body.String(), "Recovered response") {
		t.Fatalf("second body = %s, want recovered response", second.Body.String())
	}
}

func TestOpenAIChatCompletionsIdempotencyReplaysResponse(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	provider := registerMockProvider(t, api, "Replay response")
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	if err := store.UpsertAPIModelRoute(&storage.APIModelRoute{
		APIModel:      "gpt-test",
		Mode:          storage.APIModelRouteModeDirectModel,
		ProviderModel: "mock-model",
		IsDefault:     true,
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpsertAPIModelRoute: %v", err)
	}
	router := mux.NewRouter()
	api.RegisterRoutes(router)
	body := `{"model":"gpt-test","messages":[{"role":"user","content":"Say hello"}]}`

	call := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Idempotency-Key", "idem-1")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	first := call()
	second := call()
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("statuses = %d/%d bodies=%s // %s", first.Code, second.Code, first.Body.String(), second.Body.String())
	}
	if first.Body.String() != second.Body.String() {
		t.Fatalf("expected byte-identical replay\nfirst=%s\nsecond=%s", first.Body.String(), second.Body.String())
	}
	rows, err := store.ListAPIUsage(storage.APIUsageFilters{KeyID: "key-test", Limit: 10})
	if err != nil {
		t.Fatalf("ListAPIUsage: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want 1 rows=%+v", len(rows), rows)
	}
	if provider.CallCount() != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.CallCount())
	}
	storageIDKey := openAIAPIIdempotencyStorageKey(openAIUsageEndpointChat, "idem-1")
	if strings.Contains(storageIDKey, "idem-1") {
		t.Fatalf("storage idempotency key leaked caller key: %s", storageIDKey)
	}
	idem, err := store.GetAPIIdempotency("key-test", storageIDKey)
	if err != nil {
		t.Fatalf("GetAPIIdempotency: %v", err)
	}
	if idem.JobID == "" {
		t.Fatalf("idempotency record missing job id: %+v", idem)
	}
	workflowIDKey := openAIWorkflowIdempotencyKey("key-test", openAIUsageEndpointChat, "idem-1")
	if strings.Contains(workflowIDKey, "idem-1") {
		t.Fatalf("workflow idempotency key leaked caller key: %s", workflowIDKey)
	}
	if _, err := store.GetAPIIdempotency("key-test", "idem-1"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("raw idempotency lookup err = %v, want ErrNotFound", err)
	}
}

func TestOpenAIChatStreamingIdempotencyCompletesReservation(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	registerMockProvider(t, api, "Streaming replay response")
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	upsertOpenAIDirectModelRoute(t, store)

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	body := `{
		"model":"gpt-test",
		"messages":[{"role":"user","content":"Say hello"}],
		"stream":true
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Idempotency-Key", "idem-stream")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
	}

	idem, err := store.GetAPIIdempotency("key-test", openAIAPIIdempotencyStorageKey(openAIUsageEndpointChat, "idem-stream"))
	if err != nil {
		t.Fatalf("GetAPIIdempotency: %v", err)
	}
	if idem.HTTPStatus != http.StatusOK {
		t.Fatalf("idempotency row not completed after stream success: %+v", idem)
	}
	if strings.TrimSpace(idem.ResponseBody) != "" {
		t.Fatalf("streaming idempotency response body = %q, want empty", idem.ResponseBody)
	}
}

func TestOpenAIIdempotencyKeyIsEndpointScoped(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	registerMockProvider(t, api, "endpoint scoped response")
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	upsertOpenAIDirectModelRoute(t, store)

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"gpt-test",
		"messages":[{"role":"user","content":"Say hello"}]
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Idempotency-Key", "shared-key")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("chat status = %d, want 200 body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-test",
		"input":"Say hello"
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Idempotency-Key", "shared-key")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("responses status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	for _, endpoint := range []string{openAIUsageEndpointChat, openAIUsageEndpointResponses} {
		if _, err := store.GetAPIIdempotency("key-test", openAIAPIIdempotencyStorageKey(endpoint, "shared-key")); err != nil {
			t.Fatalf("GetAPIIdempotency endpoint %s: %v", endpoint, err)
		}
	}
}

func TestOpenAIChatCompletionsConcurrentIdempotencyWaitsAndExecutesOnce(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	provider := registerMockProvider(t, api, "Concurrent replay response")
	provider.delay = 100 * time.Millisecond
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	if err := store.UpsertAPIModelRoute(&storage.APIModelRoute{
		APIModel:      "gpt-test",
		Mode:          storage.APIModelRouteModeDirectModel,
		ProviderModel: "mock-model",
		IsDefault:     true,
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpsertAPIModelRoute: %v", err)
	}

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	body := `{"model":"gpt-test","messages":[{"role":"user","content":"Say hello"}]}`
	type result struct {
		status int
		body   string
	}
	results := make([]result, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+key)
			req.Header.Set("Idempotency-Key", "idem-concurrent")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			results[index] = result{status: w.Code, body: w.Body.String()}
		}(i)
	}
	close(start)
	wg.Wait()

	for i, result := range results {
		if result.status != http.StatusOK {
			t.Fatalf("result %d status = %d body=%s", i, result.status, result.body)
		}
	}
	if results[0].body != results[1].body {
		t.Fatalf("expected byte-identical concurrent replay\nfirst=%s\nsecond=%s", results[0].body, results[1].body)
	}
	if provider.CallCount() != 1 {
		t.Fatalf("provider calls = %d, want 1", provider.CallCount())
	}
	rows, err := store.ListAPIUsage(storage.APIUsageFilters{KeyID: "key-test", Limit: 10})
	if err != nil {
		t.Fatalf("ListAPIUsage: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("usage rows = %d, want 1 rows=%+v", len(rows), rows)
	}
}

func TestOpenAIChatCompletionsIdempotencyConflict(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	registerMockProvider(t, api, "Replay response")
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	if err := store.UpsertAPIModelRoute(&storage.APIModelRoute{
		APIModel:      "gpt-test",
		Mode:          storage.APIModelRouteModeDirectModel,
		ProviderModel: "mock-model",
		IsDefault:     true,
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpsertAPIModelRoute: %v", err)
	}
	router := mux.NewRouter()
	api.RegisterRoutes(router)

	for i, body := range []string{
		`{"model":"gpt-test","messages":[{"role":"user","content":"One"}]}`,
		`{"model":"gpt-test","messages":[{"role":"user","content":"Two"}]}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Idempotency-Key", "idem-conflict")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if i == 0 && w.Code != http.StatusOK {
			t.Fatalf("first status = %d body=%s", w.Code, w.Body.String())
		}
		if i == 1 && w.Code != http.StatusConflict {
			t.Fatalf("second status = %d, want 409 body=%s", w.Code, w.Body.String())
		}
	}
}

func TestOpenAIChatCompletionsRejectsOversizedBodyAndRecordsUsage(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(strings.Repeat("x", openAIRequestBodyLimit+1)))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 body=%s", w.Code, w.Body.String())
	}
	rows, err := store.ListAPIUsage(storage.APIUsageFilters{KeyID: "key-test", Limit: 10})
	if err != nil {
		t.Fatalf("ListAPIUsage: %v", err)
	}
	if len(rows) != 1 || rows[0].Status != storage.APIUsageStatusFailed || rows[0].HTTPStatus != http.StatusRequestEntityTooLarge || rows[0].ErrorCode != "request_too_large" {
		t.Fatalf("usage rows = %+v, want request_too_large failure", rows)
	}
}

func TestOpenAIChatCompletionsRejectsLongIdempotencyKeyAndRecordsUsage(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	provider := registerMockProvider(t, api, "should not run")
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	if err := store.UpsertAPIModelRoute(&storage.APIModelRoute{
		APIModel:      "gpt-test",
		Mode:          storage.APIModelRouteModeDirectModel,
		ProviderModel: "mock-model",
		IsDefault:     true,
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpsertAPIModelRoute: %v", err)
	}

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"gpt-test",
		"messages":[{"role":"user","content":"Say hello"}]
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Idempotency-Key", strings.Repeat("x", openAIMaxIdempotencyKeyBytes+1))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body=%s", w.Code, w.Body.String())
	}
	if provider.CallCount() != 0 {
		t.Fatalf("provider calls = %d, want 0", provider.CallCount())
	}
	rows, err := store.ListAPIUsage(storage.APIUsageFilters{KeyID: "key-test", Limit: 10})
	if err != nil {
		t.Fatalf("ListAPIUsage: %v", err)
	}
	if len(rows) != 1 || rows[0].ErrorCode != "invalid_idempotency_key" {
		t.Fatalf("usage rows = %+v, want invalid_idempotency_key failure", rows)
	}
}

func TestOpenAIChatCompletionsRequestRateLimit(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	registerMockProvider(t, api, "Limited response")
	key := createOpenAITestKeyWithLimits(t, store, "sk-consortium-test_123", 1, 100000)
	if err := store.UpsertAPIModelRoute(&storage.APIModelRoute{
		APIModel:      "gpt-test",
		Mode:          storage.APIModelRouteModeDirectModel,
		ProviderModel: "mock-model",
		IsDefault:     true,
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpsertAPIModelRoute: %v", err)
	}
	router := mux.NewRouter()
	api.RegisterRoutes(router)
	body := `{"model":"gpt-test","messages":[{"role":"user","content":"Say hello"}]}`
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if i == 0 && w.Code != http.StatusOK {
			t.Fatalf("first status = %d body=%s", w.Code, w.Body.String())
		}
		if i == 1 {
			if w.Code != http.StatusTooManyRequests {
				t.Fatalf("second status = %d, want 429 body=%s", w.Code, w.Body.String())
			}
			if w.Header().Get("Retry-After") == "" {
				t.Fatalf("missing Retry-After header")
			}
		}
	}
}

func TestOpenAIChatCompletionsTokenRateLimit(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	registerMockProvider(t, api, "Limited response")
	key := createOpenAITestKeyWithLimits(t, store, "sk-consortium-test_123", 100, 10)
	if err := store.UpsertAPIModelRoute(&storage.APIModelRoute{
		APIModel:      "gpt-test",
		Mode:          storage.APIModelRouteModeDirectModel,
		ProviderModel: "mock-model",
		IsDefault:     true,
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpsertAPIModelRoute: %v", err)
	}

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	body := `{"model":"gpt-test","messages":[{"role":"user","content":"` + strings.Repeat("hello ", 80) + `"}],"max_completion_tokens":1}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 body=%s", w.Code, w.Body.String())
	}
	rows, err := store.ListAPIUsage(storage.APIUsageFilters{KeyID: "key-test", Limit: 10})
	if err != nil {
		t.Fatalf("ListAPIUsage: %v", err)
	}
	if len(rows) != 1 || rows[0].Status != storage.APIUsageStatusFailed || rows[0].HTTPStatus != http.StatusTooManyRequests {
		t.Fatalf("usage rows = %+v, want recorded 429 failure", rows)
	}
}

func TestOpenAIChatCompletionsTokenRateLimitUsesCompiledWorkflowRefs(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	registerMockProvider(t, api, "should not run")
	key := createOpenAITestKeyWithLimits(t, store, "sk-consortium-test_123", 100, 25)
	childDefinition := `{
		"id":"wf-child-two-prompts",
		"name":"Child two prompts",
		"nodes":[
			{"id":"a","type":"prompt","model":"mock-model","prompt":"A {{user_prompt}}","temperature":0,"max_tokens":20,"timeout_seconds":30,"retry_policy":{"max_attempts":1,"backoff_ms":0,"backoff_multiply":1,"max_backoff_ms":0}},
			{"id":"b","type":"prompt","model":"mock-model","prompt":"B {{user_prompt}}","temperature":0,"max_tokens":20,"timeout_seconds":30,"retry_policy":{"max_attempts":1,"backoff_ms":0,"backoff_multiply":1,"max_backoff_ms":0}}
		],
		"edges":[]
	}`
	if err := store.CreateWorkflow(&storage.WorkflowDefinition{
		ID:         "wf-child-two-prompts",
		Name:       "Child two prompts",
		Definition: childDefinition,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateWorkflow child: %v", err)
	}
	parentDefinition := `{
		"id":"wf-parent-ref",
		"name":"Parent ref",
		"nodes":[{"id":"group","type":"workflow_ref","workflow_ref_id":"wf-child-two-prompts"}],
		"edges":[]
	}`
	if err := store.CreateWorkflow(&storage.WorkflowDefinition{
		ID:         "wf-parent-ref",
		Name:       "Parent ref",
		Definition: parentDefinition,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateWorkflow parent: %v", err)
	}
	if err := store.UpsertAPIModelRoute(&storage.APIModelRoute{
		APIModel:   "wf-expanded",
		Mode:       storage.APIModelRouteModeWorkflow,
		WorkflowID: "wf-parent-ref",
		IsDefault:  true,
		Enabled:    true,
	}); err != nil {
		t.Fatalf("UpsertAPIModelRoute: %v", err)
	}

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"wf-expanded",
		"messages":[{"role":"user","content":"Say hello"}],
		"max_completion_tokens":1
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 body=%s", w.Code, w.Body.String())
	}
	jobs, err := store.ListExecutions(10)
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("jobs = %+v, want no job created before token rejection", jobs)
	}
}

func TestOpenAIResponsesStreamingEvents(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	registerMockProvider(t, api, "Responses stream output")
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	if err := store.UpsertAPIModelRoute(&storage.APIModelRoute{
		APIModel:      "gpt-test",
		Mode:          storage.APIModelRouteModeDirectModel,
		ProviderModel: "mock-model",
		IsDefault:     true,
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpsertAPIModelRoute: %v", err)
	}

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-test",
		"input":"Say hello",
		"stream":true
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	text := w.Body.String()
	for _, want := range []string{
		"event: response.created",
		"event: response.in_progress",
		"event: response.output_item.added",
		"event: response.content_part.added",
		"event: response.output_text.delta",
		"event: response.output_text.done",
		"event: response.content_part.done",
		"event: response.output_item.done",
		"event: response.completed",
		`"sequence_number":`,
		`"usage"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("stream missing %q:\n%s", want, text)
		}
	}
}

func TestOpenAIResponsesStreamEmitsFunctionCallEvents(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	provider := registerMockProvider(t, api, "")
	provider.toolCalls = []providers.ToolCall{
		{
			ID:   "call_lookup",
			Type: "function",
			Function: providers.ToolCallFunction{
				Name:      "lookup",
				Arguments: `{"query":"hello"}`,
			},
		},
	}
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	upsertOpenAIDirectModelRoute(t, store)

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"gpt-test",
		"input":"Call a tool",
		"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],
		"stream":true
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	text := w.Body.String()
	for _, want := range []string{
		"event: response.created",
		"event: response.output_item.added",
		"event: response.function_call_arguments.delta",
		"event: response.function_call_arguments.done",
		"event: response.output_item.done",
		"event: response.completed",
		`"type":"function_call"`,
		`"call_id":"call_lookup"`,
		`"name":"lookup"`,
		`"arguments":"{\"query\":\"hello\"}"`,
		`"output_text":""`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("stream missing %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{
		"event: response.output_text.delta",
		"event: response.output_text.done",
	} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("stream unexpectedly contains %q:\n%s", unwanted, text)
		}
	}
	doneEvents := openAITestSSEEventPayloads(t, text, "response.function_call_arguments.done")
	if len(doneEvents) != 1 {
		t.Fatalf("function_call_arguments.done events = %d, want 1\n%s", len(doneEvents), text)
	}
	done := doneEvents[0]
	if done["item_id"] == "" || done["arguments"] != `{"query":"hello"}` {
		t.Fatalf("function_call_arguments.done payload = %#v", done)
	}
	item, ok := done["item"].(map[string]any)
	if !ok || item["type"] != "function_call" || item["call_id"] != "call_lookup" {
		t.Fatalf("function_call_arguments.done item = %#v", done["item"])
	}
}

func openAITestSSEEventPayloads(t *testing.T, stream, eventName string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, block := range strings.Split(stream, "\n\n") {
		if !strings.Contains(block, "event: "+eventName+"\n") {
			continue
		}
		for _, line := range strings.Split(block, "\n") {
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload); err != nil {
				t.Fatalf("decode SSE event %s payload %q: %v", eventName, line, err)
			}
			out = append(out, payload)
		}
	}
	return out
}

func TestOpenAIWorkflowRouteAppliesTerminalPromptControls(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	provider := registerMockProvider(t, api, "Workflow route response")
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	if err := store.CreateWorkflow(&storage.WorkflowDefinition{
		ID:   "wf-api-terminal",
		Name: "API terminal prompt",
		Definition: `{
			"id":"wf-api-terminal",
			"name":"API terminal prompt",
			"nodes":[{
				"id":"answer",
				"type":"prompt",
				"model":"mock-model",
				"prompt":"{{user_prompt}}",
				"temperature":0,
				"max_tokens":64,
				"timeout_seconds":30,
				"retry_policy":{"max_attempts":1,"backoff_ms":0,"backoff_multiply":1,"max_backoff_ms":0}
			}]
		}`,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if err := store.UpsertAPIModelRoute(&storage.APIModelRoute{
		APIModel:   "wf-test",
		Mode:       storage.APIModelRouteModeWorkflow,
		WorkflowID: "wf-api-terminal",
		IsDefault:  true,
		Enabled:    true,
	}); err != nil {
		t.Fatalf("UpsertAPIModelRoute: %v", err)
	}

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	body := `{
		"model":"wf-test",
		"messages":[{"role":"user","content":"Say hello"}],
		"top_p":0.5,
		"stop":["END"],
		"tools":[{"type":"function","function":{"name":"lookup","description":"lookup","parameters":{"type":"object"}}}],
		"tool_choice":"auto",
		"parallel_tool_calls":false,
		"response_format":{"type":"json_object"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	if provider.lastRequest == nil {
		t.Fatalf("provider did not capture request")
	}
	if provider.lastRequest.TopP == nil || *provider.lastRequest.TopP != 0.5 {
		t.Fatalf("TopP = %#v, want 0.5", provider.lastRequest.TopP)
	}
	if len(provider.lastRequest.Stop) != 1 || provider.lastRequest.Stop[0] != "END" {
		t.Fatalf("Stop = %#v, want END", provider.lastRequest.Stop)
	}
	if len(provider.lastRequest.Tools) != 1 || provider.lastRequest.Extensions == nil || provider.lastRequest.Extensions.ResponseFormat == nil {
		t.Fatalf("tools/response_format not propagated: req=%+v", provider.lastRequest)
	}
	if provider.lastRequest.ParallelToolCalls == nil || *provider.lastRequest.ParallelToolCalls {
		t.Fatalf("parallel_tool_calls = %+v, want false", provider.lastRequest)
	}
}

func TestOpenAIWorkflowRouteDoesNotExposeInternalToolCalls(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	provider := registerMockProvider(t, api, "Workflow route response")
	provider.toolCalls = []providers.ToolCall{
		{
			ID:   "call_internal",
			Type: "function",
			Function: providers.ToolCallFunction{
				Name:      "internal_lookup",
				Arguments: `{"query":"hidden"}`,
			},
		},
	}
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	if err := store.CreateWorkflow(&storage.WorkflowDefinition{
		ID:   "wf-api-terminal-internal-tools",
		Name: "API terminal internal tools",
		Definition: `{
			"id":"wf-api-terminal-internal-tools",
			"name":"API terminal internal tools",
			"nodes":[{
				"id":"answer",
				"type":"prompt",
				"model":"mock-model",
				"prompt":"{{user_prompt}}",
				"temperature":0,
				"max_tokens":64,
				"timeout_seconds":30,
				"retry_policy":{"max_attempts":1,"backoff_ms":0,"backoff_multiply":1,"max_backoff_ms":0}
			}]
		}`,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if err := store.UpsertAPIModelRoute(&storage.APIModelRoute{
		APIModel:   "wf-internal-tools",
		Mode:       storage.APIModelRouteModeWorkflow,
		WorkflowID: "wf-api-terminal-internal-tools",
		IsDefault:  true,
		Enabled:    true,
	}); err != nil {
		t.Fatalf("UpsertAPIModelRoute: %v", err)
	}

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"wf-internal-tools",
		"messages":[{"role":"user","content":"Say hello"}]
	}`))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", w.Code, w.Body.String())
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	choices := payload["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})
	if choice["finish_reason"] != "stop" {
		t.Fatalf("finish_reason = %#v, want stop body=%s", choice["finish_reason"], w.Body.String())
	}
	message := choice["message"].(map[string]interface{})
	if _, ok := message["tool_calls"]; ok {
		t.Fatalf("workflow route leaked tool_calls: %s", w.Body.String())
	}
	if message["content"] != "Workflow route response" {
		t.Fatalf("content = %#v", message["content"])
	}
}

func TestOpenAIWorkflowRouteRejectsForcedToolsWithoutTerminalPrompt(t *testing.T) {
	api, store := setupWorkflowAPI(t)
	key := createOpenAITestKey(t, store, "sk-consortium-test_123")
	if err := store.CreateWorkflow(&storage.WorkflowDefinition{
		ID:   "wf-api-no-terminal",
		Name: "API no terminal prompt",
		Definition: `{
			"id":"wf-api-no-terminal",
			"name":"API no terminal prompt",
			"nodes":[{
				"id":"shape",
				"type":"operation",
				"operation_type":"extract_answer",
				"operation_config":{"text":"{{user_prompt}}"}
			}]
		}`,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if err := store.UpsertAPIModelRoute(&storage.APIModelRoute{
		APIModel:   "wf-no-terminal",
		Mode:       storage.APIModelRouteModeWorkflow,
		WorkflowID: "wf-api-no-terminal",
		IsDefault:  true,
		Enabled:    true,
	}); err != nil {
		t.Fatalf("UpsertAPIModelRoute: %v", err)
	}

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	body := `{
		"model":"wf-no-terminal",
		"messages":[{"role":"user","content":"Say hello"}],
		"tools":[{"type":"function","function":{"name":"lookup","description":"lookup","parameters":{"type":"object"}}}],
		"tool_choice":"required"
	}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "terminal prompt") {
		t.Fatalf("body = %s, want terminal prompt error", w.Body.String())
	}
}

func upsertOpenAIDirectModelRoute(t *testing.T, store *storage.Storage) {
	t.Helper()
	if err := store.UpsertAPIModelRoute(&storage.APIModelRoute{
		APIModel:      "gpt-test",
		Mode:          storage.APIModelRouteModeDirectModel,
		ProviderModel: "mock-model",
		IsDefault:     true,
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpsertAPIModelRoute: %v", err)
	}
}

func assertOpenAIErrorEnvelope(t *testing.T, w *httptest.ResponseRecorder, wantCode string) {
	t.Helper()
	var payload openAIErrorResponse
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error response: %v body=%s", err, w.Body.String())
	}
	if payload.Error.Message == "" {
		t.Fatalf("error message missing: %#v", payload)
	}
	if payload.Error.Type == "" {
		t.Fatalf("error type missing: %#v", payload)
	}
	if wantCode != "" && payload.Error.Code != wantCode {
		t.Fatalf("error code = %q, want %q body=%s", payload.Error.Code, wantCode, w.Body.String())
	}
}

func createOpenAITestKey(t *testing.T, store *storage.Storage, token string) string {
	return createOpenAITestKeyWithLimits(t, store, token, 100, 100000)
}

func createOpenAITestKeyWithLimits(t *testing.T, store *storage.Storage, token string, requestsPerMinute, tokensPerMinute int) string {
	return createOpenAITestKeyWithID(t, store, "key-test", "test", token, requestsPerMinute, tokensPerMinute)
}

func createOpenAITestKeyWithID(t *testing.T, store *storage.Storage, id, prefixSuffix, token string, requestsPerMinute, tokensPerMinute int) string {
	t.Helper()
	sum := sha256.Sum256([]byte(token))
	if err := store.CreateAPIKey(&storage.APIKey{
		ID:                id,
		UserID:            "system",
		Name:              prefixSuffix + " key",
		Prefix:            "sk-consortium-" + prefixSuffix,
		KeyHash:           "sha256:" + hex.EncodeToString(sum[:]),
		RequestsPerMinute: requestsPerMinute,
		TokensPerMinute:   tokensPerMinute,
		CreatedAt:         time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	return token
}

func TestOpenAIStringMetadataRejectsStructuredValues(t *testing.T) {
	metadata := map[string]interface{}{
		"string":     " rate_limited ",
		"structured": map[string]interface{}{"code": "rate_limited"},
		"slice":      []interface{}{"rate_limited"},
	}
	if got := openAIStringMetadata(metadata, "string"); got != "rate_limited" {
		t.Fatalf("string metadata = %q", got)
	}
	if got := openAIStringMetadata(metadata, "structured"); got != "" {
		t.Fatalf("structured metadata = %q, want empty", got)
	}
	if got := openAIStringMetadata(metadata, "slice"); got != "" {
		t.Fatalf("slice metadata = %q, want empty", got)
	}
}
