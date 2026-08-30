package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/alhasaniq/consortium/pkg/providers"
)

func clearProviderEnv(t *testing.T) {
	t.Helper()
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_COMPATIBLE_BASE_URL", "")
	t.Setenv("OPENAI_COMPATIBLE_API_KEY", "")
	t.Setenv("OPENAI_COMPATIBLE_MODELS", "")
}

func TestRegisterConfiguredProvidersRequiresAtLeastOneProvider(t *testing.T) {
	clearProviderEnv(t)
	registry := providers.NewRegistry()
	if err := registerConfiguredProviders(context.Background(), registry); err == nil {
		t.Fatal("expected error when no provider is configured")
	}
}

func TestRegisterConfiguredProvidersOpenRouterOnly(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("OPENROUTER_API_KEY", "test-openrouter-key")
	registry := providers.NewRegistry()
	if err := registerConfiguredProviders(context.Background(), registry); err != nil {
		t.Fatalf("registerConfiguredProviders: %v", err)
	}
	if _, err := registry.GetProvider("openrouter"); err != nil {
		t.Fatalf("OpenRouter provider not registered: %v", err)
	}
}

func TestRegisterConfiguredProvidersCompatibleOnlyDiscoversModels(t *testing.T) {
	clearProviderEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %q, want /v1/models", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "qwen3:8b"}, {"id": "llama3.2:3b"}},
		})
	}))
	defer srv.Close()

	t.Setenv("OPENAI_COMPATIBLE_BASE_URL", srv.URL+"/v1")
	registry := providers.NewRegistry()
	if err := registerConfiguredProviders(context.Background(), registry); err != nil {
		t.Fatalf("registerConfiguredProviders: %v", err)
	}
	if _, err := registry.GetProvider("openai-compatible"); err != nil {
		t.Fatalf("compatible provider not registered: %v", err)
	}
	models := registry.GetModels()
	got := make([]string, 0, len(models))
	for _, model := range models {
		got = append(got, model.ID)
	}
	want := []string{"compatible/llama3.2:3b", "compatible/qwen3:8b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("models = %#v, want %#v", got, want)
	}
}

func TestRegisterConfiguredProvidersCompatibleFallbackAllowsMissingCatalog(t *testing.T) {
	clearProviderEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "models endpoint disabled", http.StatusNotFound)
	}))
	defer srv.Close()

	t.Setenv("OPENAI_COMPATIBLE_BASE_URL", srv.URL+"/v1")
	t.Setenv("OPENAI_COMPATIBLE_MODELS", " qwen3:8b, llama3.2:3b, qwen3:8b ")
	registry := providers.NewRegistry()
	if err := registerConfiguredProviders(context.Background(), registry); err != nil {
		t.Fatalf("registerConfiguredProviders: %v", err)
	}
	models := registry.GetModels()
	if len(models) != 2 {
		t.Fatalf("model count = %d, want 2", len(models))
	}
}

func TestRegisterConfiguredProvidersCompatibleWithoutFallbackFailsClosed(t *testing.T) {
	clearProviderEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "models endpoint disabled", http.StatusNotFound)
	}))
	defer srv.Close()

	t.Setenv("OPENAI_COMPATIBLE_BASE_URL", srv.URL+"/v1")
	registry := providers.NewRegistry()
	if err := registerConfiguredProviders(context.Background(), registry); err == nil {
		t.Fatal("expected model discovery failure without fallback models")
	}
}

func TestRegisterConfiguredProvidersCanRegisterBothBackends(t *testing.T) {
	clearProviderEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": "local-model"}}})
	}))
	defer srv.Close()

	t.Setenv("OPENROUTER_API_KEY", "or-key")
	t.Setenv("OPENAI_COMPATIBLE_BASE_URL", srv.URL)
	registry := providers.NewRegistry()
	if err := registerConfiguredProviders(context.Background(), registry); err != nil {
		t.Fatalf("registerConfiguredProviders: %v", err)
	}
	if got := len(registry.GetProviders()); got != 2 {
		t.Fatalf("provider count = %d, want 2", got)
	}
}

func TestParseCompatibleModelList(t *testing.T) {
	got := parseCompatibleModelList("qwen3:8b, llama3.2:3b\nqwen3:8b\r\n  deepseek-r1:7b ")
	want := []string{"qwen3:8b", "llama3.2:3b", "deepseek-r1:7b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseCompatibleModelList = %#v, want %#v", got, want)
	}
}
