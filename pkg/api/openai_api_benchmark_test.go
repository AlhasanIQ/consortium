package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/jobs"
	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/gorilla/mux"
)

const benchmarkOpenAIKeyPrefix = "sk-consortium-bench"

func benchmarkAuthFixture() string {
	return benchmarkOpenAIKeyPrefix + "_123"
}

func benchmarkOpenAIAPI(b *testing.B) (*WorkflowAPI, *storage.Storage, *mux.Router) {
	b.Helper()
	logOutput := log.Writer()
	log.SetOutput(io.Discard)
	b.Cleanup(func() { log.SetOutput(logOutput) })

	store, err := storage.NewStorage(":memory:")
	if err != nil {
		b.Fatalf("NewStorage: %v", err)
	}
	b.Cleanup(func() { _ = store.Close() })

	registry := providers.NewRegistry()
	registry.Register(&MockExecuteProvider{
		name: "mock",
		models: []providers.Model{
			{ID: "mock-model", Provider: "mock", InputCost: 0.000001, OutputCost: 0.000002},
		},
		response: "benchmark response",
	})

	cfg := jobs.DefaultManagerConfig()
	cfg.MaxConcurrentWorkflows = 64
	cfg.WorkerCount = 64
	cfg.WorkerPollInterval = time.Millisecond
	manager := jobs.NewManagerWithConfig(store, registry, cfg)
	manager.StartWorkers()
	b.Cleanup(func() { manager.StopWorkers(context.Background()) })

	api := NewWorkflowAPI(store, registry, manager)
	createBenchmarkOpenAIKey(b, store)
	if err := store.UpsertAPIModelRoute(&storage.APIModelRoute{
		APIModel:      "gpt-test",
		Mode:          storage.APIModelRouteModeDirectModel,
		ProviderModel: "mock-model",
		IsDefault:     true,
		Enabled:       true,
	}); err != nil {
		b.Fatalf("UpsertAPIModelRoute: %v", err)
	}

	router := mux.NewRouter()
	api.RegisterRoutes(router)
	return api, store, router
}

func createBenchmarkOpenAIKey(b *testing.B, store *storage.Storage) {
	b.Helper()
	sum := sha256.Sum256([]byte(benchmarkAuthFixture()))
	if err := store.CreateAPIKey(&storage.APIKey{
		ID:                "key-bench",
		UserID:            "system",
		Name:              "benchmark key",
		Prefix:            benchmarkOpenAIKeyPrefix,
		KeyHash:           "sha256:" + hex.EncodeToString(sum[:]),
		RequestsPerMinute: 1_000_000_000,
		TokensPerMinute:   1_000_000_000,
		CreatedAt:         time.Now().UTC(),
	}); err != nil {
		b.Fatalf("CreateAPIKey: %v", err)
	}
}

func benchmarkOpenAIRequest(router *mux.Router, method, path, body string) int {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+benchmarkAuthFixture())
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w.Code
}

func BenchmarkOpenAIChatCompletionsDirectModelStoreFalse(b *testing.B) {
	_, _, router := benchmarkOpenAIAPI(b)
	body := `{"model":"gpt-test","messages":[{"role":"user","content":"Say hello"}],"store":false,"max_completion_tokens":32}`

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if code := benchmarkOpenAIRequest(router, http.MethodPost, "/v1/chat/completions", body); code != http.StatusOK {
			b.Fatalf("status = %d, want 200", code)
		}
	}
}

func BenchmarkOpenAIResponsesDirectModelStoreFalse(b *testing.B) {
	_, _, router := benchmarkOpenAIAPI(b)
	body := `{"model":"gpt-test","input":"Say hello","store":false,"max_output_tokens":32}`

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if code := benchmarkOpenAIRequest(router, http.MethodPost, "/v1/responses", body); code != http.StatusOK {
			b.Fatalf("status = %d, want 200", code)
		}
	}
}

func BenchmarkOpenAIResponseRetrieveStored(b *testing.B) {
	_, _, router := benchmarkOpenAIAPI(b)
	body := `{"model":"gpt-test","input":"Say hello","store":true,"max_output_tokens":32}`
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+benchmarkAuthFixture())
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		b.Fatalf("create response status = %d body=%s", w.Code, w.Body.String())
	}
	responseID := "resp-"
	if idx := strings.Index(w.Body.String(), `"id":"resp-`); idx >= 0 {
		start := idx + len(`"id":"`)
		end := strings.Index(w.Body.String()[start:], `"`)
		if end >= 0 {
			responseID = w.Body.String()[start : start+end]
		}
	}
	if responseID == "resp-" {
		b.Fatalf("failed to extract response id from %s", w.Body.String())
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if code := benchmarkOpenAIRequest(router, http.MethodGet, "/v1/responses/"+responseID, ""); code != http.StatusOK {
			b.Fatalf("status = %d, want 200", code)
		}
	}
}
