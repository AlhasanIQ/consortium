package jobs

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
)

// ---------------------------------------------------------------------------
// Shared mock provider
// ---------------------------------------------------------------------------

// mockProvider implements providers.Provider for testing.
type mockProvider struct {
	name         string
	models       []providers.Model
	response     *providers.CompletionResponse
	err          error
	completeFunc func(context.Context, *providers.CompletionRequest) (*providers.CompletionResponse, error)
}

func (m *mockProvider) Name() string                   { return m.name }
func (m *mockProvider) Models() []providers.Model      { return m.models }
func (m *mockProvider) EstimateTokens(text string) int { return len(text) / 4 }
func (m *mockProvider) Cost(model string, input, output int) float64 {
	return float64(input)*0.001 + float64(output)*0.002
}
func (m *mockProvider) Complete(ctx context.Context, req *providers.CompletionRequest) (*providers.CompletionResponse, error) {
	if m.completeFunc != nil {
		return m.completeFunc(ctx, req)
	}
	return m.response, m.err
}

// ---------------------------------------------------------------------------
// Mock provider constructors
// ---------------------------------------------------------------------------

// defaultMockModel returns the standard mock model definition reused across tests.
func defaultMockModel() providers.Model {
	return providers.Model{
		ID:         "mock-model",
		Name:       "Mock Model",
		Provider:   "mock",
		ContextLen: 4096,
		InputCost:  0.001,
		OutputCost: 0.002,
		MaxTokens:  2048,
		Available:  true,
	}
}

// defaultMockResponse returns the standard mock completion response.
func defaultMockResponse() *providers.CompletionResponse {
	return &providers.CompletionResponse{
		Content: "Mock response",
		Usage: providers.Usage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}
}

// newDefaultMockProvider returns the standard "mock" provider with a fixed response.
func newDefaultMockProvider() *mockProvider {
	return &mockProvider{
		name:     "mock",
		models:   []providers.Model{defaultMockModel()},
		response: defaultMockResponse(),
	}
}

// newSlowMockProvider returns a provider that blocks for duration or until ctx is cancelled.
func newSlowMockProvider(name string, modelID string, duration time.Duration) *mockProvider {
	return &mockProvider{
		name:   name,
		models: []providers.Model{{ID: modelID, Provider: name}},
		completeFunc: func(ctx context.Context, _ *providers.CompletionRequest) (*providers.CompletionResponse, error) {
			select {
			case <-time.After(duration):
				return &providers.CompletionResponse{Content: "Completed"}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}
}

// newBlockingMockProvider returns a provider that waits until ctx cancellation.
// Useful for deterministic cancellation tests.
func newBlockingMockProvider(name string, modelID string) *mockProvider {
	return &mockProvider{
		name:   name,
		models: []providers.Model{{ID: modelID, Provider: name}},
		completeFunc: func(ctx context.Context, _ *providers.CompletionRequest) (*providers.CompletionResponse, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
}

// newFailingMockProvider returns a provider that always returns the given error.
func newFailingMockProvider(name string, modelID string, err error) *mockProvider {
	return &mockProvider{
		name:   name,
		models: []providers.Model{{Provider: name, ID: modelID}},
		err:    err,
	}
}

// newConcurrencyTrackingMockProvider returns a provider that tracks the maximum
// number of concurrent in-flight calls and sleeps for the given duration.
func newConcurrencyTrackingMockProvider(name string, modelID string, sleep time.Duration) (*mockProvider, *int, *sync.Mutex) {
	var mu sync.Mutex
	current := 0
	maxSeen := 0
	mock := &mockProvider{
		name:   name,
		models: []providers.Model{{ID: modelID, Provider: name}},
		completeFunc: func(_ context.Context, _ *providers.CompletionRequest) (*providers.CompletionResponse, error) {
			mu.Lock()
			current++
			if current > maxSeen {
				maxSeen = current
			}
			mu.Unlock()

			time.Sleep(sleep)

			mu.Lock()
			current--
			mu.Unlock()

			return &providers.CompletionResponse{
				Content: "ok",
				Usage:   providers.Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
			}, nil
		},
	}
	return mock, &maxSeen, &mu
}

// newRendezvousMockProvider returns a provider that forces two prompt calls to
// overlap. The first call waits for the second call to start, failing fast if
// that never happens.
func newRendezvousMockProvider(name string, modelID string, waitForSecond time.Duration) (*mockProvider, *int, *sync.Mutex) {
	var mu sync.Mutex
	calls := 0
	secondStarted := make(chan struct{})

	mock := &mockProvider{
		name:   name,
		models: []providers.Model{{ID: modelID, Provider: name}},
		completeFunc: func(ctx context.Context, _ *providers.CompletionRequest) (*providers.CompletionResponse, error) {
			mu.Lock()
			calls++
			callNum := calls
			mu.Unlock()

			switch callNum {
			case 1:
				select {
				case <-secondStarted:
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(waitForSecond):
					return nil, fmt.Errorf("parallel rendezvous timeout after %s", waitForSecond)
				}
			case 2:
				select {
				case <-secondStarted:
				default:
					close(secondStarted)
				}
			}

			return &providers.CompletionResponse{
				Content: "ok",
				Usage:   providers.Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
			}, nil
		},
	}

	return mock, &calls, &mu
}

// ---------------------------------------------------------------------------
// Test setup helpers
// ---------------------------------------------------------------------------

// setupManagerTest creates a Manager backed by an in-memory store with the
// default mock provider registered. Suitable for most tests.
func setupManagerTest(t *testing.T) (*Manager, *storage.Storage) {
	t.Helper()
	store, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	registry := providers.NewRegistry()
	registry.Register(newDefaultMockProvider())

	cfg := DefaultManagerConfig()
	cfg.MaxConcurrentWorkflows = 4
	cfg.WorkerCount = 4
	cfg.WorkerPollInterval = 2 * time.Millisecond

	manager := NewManagerWithConfig(store, registry, cfg)
	return manager, store
}

// setupManagerWithProvider creates a Manager with a custom provider.
func setupManagerWithProvider(t *testing.T, provider *mockProvider) (*Manager, *storage.Storage) {
	t.Helper()
	store, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	registry := providers.NewRegistry()
	registry.Register(provider)

	cfg := DefaultManagerConfig()
	cfg.MaxConcurrentWorkflows = 4
	cfg.WorkerCount = 4
	cfg.WorkerPollInterval = 2 * time.Millisecond

	manager := NewManagerWithConfig(store, registry, cfg)
	return manager, store
}

// setupManagerWithConfig creates a Manager with a custom config and the default mock provider.
func setupManagerWithConfig(t *testing.T, cfg *ManagerConfig) (*Manager, *storage.Storage) {
	t.Helper()
	store, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	registry := providers.NewRegistry()
	registry.Register(newDefaultMockProvider())

	manager := NewManagerWithConfig(store, registry, cfg)
	return manager, store
}

// startWorkers starts background workers on a manager and registers cleanup.
func startWorkers(t *testing.T, m *Manager) {
	t.Helper()
	m.StartWorkers()
	t.Cleanup(func() { m.StopWorkers(context.Background()) })
}

// ---------------------------------------------------------------------------
// Workflow constructors
// ---------------------------------------------------------------------------

// simpleWorkflow returns a single-node prompt workflow.
func simpleWorkflow(name, model, prompt string) *workflow.Workflow {
	defaultTemp := 0.0
	return &workflow.Workflow{
		Name: name,
		Nodes: []*workflow.Node{{
			Type:           workflow.NodeTypePrompt,
			Model:          model,
			Prompt:         prompt,
			Temperature:    &defaultTemp,
			MaxTokens:      256,
			TimeoutSeconds: 30,
			RetryPolicy:    workflow.DefaultRetryPolicy(),
		}},
	}
}

// simpleWorkflowWithID returns a single-node prompt workflow with an explicit ID.
func simpleWorkflowWithID(id, name, model, prompt string) *workflow.Workflow {
	wf := simpleWorkflow(name, model, prompt)
	wf.ID = id
	return wf
}

// strictPromptNode returns a prompt node with all runtime-required fields explicit.
func strictPromptNode(id, model, prompt string) *workflow.Node {
	temperature := 0.0
	return &workflow.Node{
		ID:             id,
		Type:           workflow.NodeTypePrompt,
		Model:          model,
		Prompt:         prompt,
		Temperature:    &temperature,
		MaxTokens:      256,
		TimeoutSeconds: 30,
		RetryPolicy:    workflow.DefaultRetryPolicy(),
	}
}

// strictResultNode returns a result node with explicit aggregation and retry policy.
func strictResultNode(id string, inputIDs []string, outputName string) *workflow.Node {
	metadata := map[string]interface{}{
		"input_ids": toAnySlice(inputIDs),
	}
	if outputName != "" {
		metadata["output_name"] = outputName
	}
	return &workflow.Node{
		ID:                id,
		Type:              workflow.NodeTypeResult,
		AggregationMethod: workflow.AggMethodCollect,
		Metadata:          metadata,
		RetryPolicy:       workflow.DefaultRetryPolicy(),
	}
}

func toAnySlice(values []string) []interface{} {
	out := make([]interface{}, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}
