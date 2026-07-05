package providers

import (
	"context"
	"fmt"
	"sync"

	"github.com/alhasaniq/consortium/pkg/storage"
)

// ErrNotFound is an alias for storage.ErrNotFound so callers only need one sentinel check.
var ErrNotFound = storage.ErrNotFound

// Registry manages multiple LLM providers with concurrent access
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
	models    []Model
}

// NewRegistry creates a new provider registry
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
		models:    []Model{},
	}
}

// Register adds a provider to the registry
func (r *Registry) Register(provider Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.providers[provider.Name()] = provider

	// Add provider's models to the global model list
	r.models = append(r.models, provider.Models()...)
}

// GetProvider returns a provider by name
func (r *Registry) GetProvider(name string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	provider, exists := r.providers[name]
	if !exists {
		return nil, fmt.Errorf("provider %s not found: %w", name, ErrNotFound)
	}

	return provider, nil
}

// GetProviders returns all registered providers
func (r *Registry) GetProviders() map[string]Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Return a copy to prevent external modification
	providers := make(map[string]Provider, len(r.providers))
	for name, provider := range r.providers {
		providers[name] = provider
	}

	return providers
}

// GetModels returns all available models across all providers.
// Calls through to each provider dynamically so model caches are respected.
func (r *Registry) GetModels() []Model {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var models []Model
	for _, provider := range r.providers {
		models = append(models, provider.Models()...)
	}
	return models
}

// GetModel returns a specific model by ID.
func (r *Registry) GetModel(modelID string) (Model, error) {
	models := r.GetModels()
	for _, model := range models {
		if model.ID == modelID {
			return model, nil
		}
	}

	return Model{}, fmt.Errorf("model %s not found: %w", modelID, ErrNotFound)
}

// Complete performs a completion using the appropriate provider for the model
func (r *Registry) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	// Find the provider for this model
	model, err := r.GetModel(req.Model)
	if err != nil {
		return nil, fmt.Errorf("failed to find model: %w", err)
	}

	provider, err := r.GetProvider(model.Provider)
	if err != nil {
		return nil, fmt.Errorf("failed to find provider: %w", err)
	}

	// Perform the completion
	return provider.Complete(ctx, req)
}
