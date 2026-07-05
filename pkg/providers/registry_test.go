package providers

import (
	"context"
	"testing"
)

// MockProvider for testing registry
type mockProvider struct {
	name     string
	models   []Model
	response *CompletionResponse
	err      error
}

func (m *mockProvider) Name() string                                 { return m.name }
func (m *mockProvider) Models() []Model                              { return m.models }
func (m *mockProvider) EstimateTokens(text string) int               { return len(text) / 4 }
func (m *mockProvider) Cost(model string, input, output int) float64 { return 0.001 }
func (m *mockProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	return m.response, m.err
}

func TestRegistry(t *testing.T) {
	t.Run("register and get provider", func(t *testing.T) {
		registry := NewRegistry()
		mock := &mockProvider{
			name: "test-provider",
			models: []Model{
				{ID: "test-model", Name: "Test Model", Provider: "test-provider", InputCost: 0.001, OutputCost: 0.002},
			},
		}

		registry.Register(mock)

		provider, err := registry.GetProvider("test-provider")
		if err != nil {
			t.Fatalf("expected provider, got error: %v", err)
		}
		if provider.Name() != "test-provider" {
			t.Errorf("expected test-provider, got %s", provider.Name())
		}
	})

	t.Run("get unknown provider returns error", func(t *testing.T) {
		registry := NewRegistry()

		_, err := registry.GetProvider("unknown")
		if err == nil {
			t.Fatal("expected error for unknown provider")
		}
	})

	t.Run("get models from registry", func(t *testing.T) {
		registry := NewRegistry()
		mock := &mockProvider{
			name: "provider1",
			models: []Model{
				{ID: "model1", Name: "Model 1", Provider: "provider1"},
				{ID: "model2", Name: "Model 2", Provider: "provider1"},
			},
		}

		registry.Register(mock)

		models := registry.GetModels()
		if len(models) != 2 {
			t.Fatalf("expected 2 models, got %d", len(models))
		}
	})

	t.Run("get specific model", func(t *testing.T) {
		registry := NewRegistry()
		mock := &mockProvider{
			name: "test",
			models: []Model{
				{ID: "target-model", Name: "Target", Provider: "test"},
			},
		}

		registry.Register(mock)

		model, err := registry.GetModel("target-model")
		if err != nil {
			t.Fatalf("expected model, got error: %v", err)
		}
		if model.ID != "target-model" {
			t.Errorf("expected target-model, got %s", model.ID)
		}
	})

	t.Run("get unknown model returns error", func(t *testing.T) {
		registry := NewRegistry()

		_, err := registry.GetModel("unknown")
		if err == nil {
			t.Fatal("expected error for unknown model")
		}
	})

	t.Run("complete delegates to provider", func(t *testing.T) {
		registry := NewRegistry()
		mock := &mockProvider{
			name: "test",
			models: []Model{
				{ID: "test-model", Provider: "test"},
			},
			response: &CompletionResponse{
				Content: "test response",
				Usage:   Usage{TotalTokens: 100},
			},
		}

		registry.Register(mock)

		resp, err := registry.Complete(context.Background(), &CompletionRequest{
			Model:    "test-model",
			Messages: []Message{{Role: "user", Content: "hello"}},
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Content != "test response" {
			t.Errorf("expected test response, got %s", resp.Content)
		}
	})

	t.Run("concurrent access is thread-safe", func(t *testing.T) {
		registry := NewRegistry()
		mock := &mockProvider{
			name:   "concurrent",
			models: []Model{{ID: "model1", Provider: "concurrent"}},
		}

		registry.Register(mock)

		// Read from cache concurrently
		done := make(chan bool, 100)
		for i := 0; i < 100; i++ {
			go func() {
				models := registry.GetModels()
				_ = models
				done <- true
			}()
		}

		for i := 0; i < 100; i++ {
			<-done
		}
	})

	t.Run("GetProviders returns all registered", func(t *testing.T) {
		registry := NewRegistry()
		mock1 := &mockProvider{name: "provider1", models: []Model{{ID: "m1", Provider: "provider1"}}}
		mock2 := &mockProvider{name: "provider2", models: []Model{{ID: "m2", Provider: "provider2"}}}

		registry.Register(mock1)
		registry.Register(mock2)

		providers := registry.GetProviders()
		if len(providers) != 2 {
			t.Fatalf("expected 2 providers, got %d", len(providers))
		}

		names := make(map[string]bool)
		for _, p := range providers {
			names[p.Name()] = true
		}
		if !names["provider1"] || !names["provider2"] {
			t.Error("missing expected providers")
		}
	})

	t.Run("Complete returns error for unknown model", func(t *testing.T) {
		registry := NewRegistry()
		_, err := registry.Complete(context.Background(), &CompletionRequest{
			Model:    "unknown-model",
			Messages: []Message{{Role: "user", Content: "test"}},
		})
		if err == nil {
			t.Error("expected error for unknown model")
		}
	})
}
