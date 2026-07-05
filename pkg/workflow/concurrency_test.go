package workflow

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/alhasaniq/consortium/pkg/providers"
)

// TestParallelNodeConcurrencySafety verifies that parallel node execution via DAG is race-free
func TestParallelNodeConcurrencySafety(t *testing.T) {
	// Create mock provider
	mockProvider := NewMockProvider("test-provider")
	mockProvider.SetResponse("Task", "Completed task")

	registry := providers.NewRegistry()
	registry.Register(mockProvider)
	executor := NewExecutorWithRegistry(registry)

	// Create a workflow with parallel nodes using DAG edges
	workflow := &Workflow{
		Context: map[string]interface{}{
			"shared_value": "initial",
		},
		Nodes: []*Node{
			{
				ID:     "task1",
				Type:   NodeTypePrompt,
				Prompt: "Task 1 with {{shared_value}}",
				Model:  "mock-model",
			},
			{
				ID:     "task2",
				Type:   NodeTypePrompt,
				Prompt: "Task 2 with {{shared_value}}",
				Model:  "mock-model",
			},
			{
				ID:     "task3",
				Type:   NodeTypePrompt,
				Prompt: "Task 3 with {{shared_value}}",
				Model:  "mock-model",
			},
			{
				ID:     "task4",
				Type:   NodeTypePrompt,
				Prompt: "Task 4 with {{shared_value}}",
				Model:  "mock-model",
			},
			{
				ID:     "task5",
				Type:   NodeTypePrompt,
				Prompt: "Task 5 with {{shared_value}}",
				Model:  "mock-model",
			},
		},
		// No edges means all nodes can run in parallel (or sequentially for no deps)
		Edges: []*Edge{},
	}

	// Run the workflow multiple times concurrently to stress test
	const numRuns = 20
	var wg sync.WaitGroup
	errors := make([]error, numRuns)

	for i := 0; i < numRuns; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := executor.Execute(context.Background(), withStrictWorkflowDefaults(workflow), nil)
			errors[idx] = err
		}(i)
	}

	wg.Wait()

	// Check for errors
	for i, err := range errors {
		if err != nil {
			t.Errorf("Run %d failed: %v", i, err)
		}
	}
}

// TestRegistryConcurrentAccess tests concurrent reads from registry
func TestRegistryConcurrentAccess(t *testing.T) {
	registry := providers.NewRegistry()

	// Register multiple providers
	for i := 0; i < 5; i++ {
		provider := NewMockProvider(fmt.Sprintf("provider-%d", i))
		registry.Register(provider)
	}

	// Perform concurrent reads
	const numReads = 100
	var wg sync.WaitGroup
	errors := make(chan error, numReads)

	for i := 0; i < numReads; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			// Test GetProviders
			providers := registry.GetProviders()
			if len(providers) != 5 {
				errors <- fmt.Errorf("expected 5 providers, got %d", len(providers))
				return
			}

			// Test GetProvider
			providerName := fmt.Sprintf("provider-%d", idx%5)
			_, err := registry.GetProvider(providerName)
			if err != nil {
				errors <- err
				return
			}

			// Test GetModels
			_ = registry.GetModels()
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("Concurrent read failed: %v", err)
	}
}

// TestWorkflowContextIsolation verifies that parallel nodes using DAG don't interfere with each other's context
func TestWorkflowContextIsolation(t *testing.T) {
	mockProvider := NewMockProvider("test-provider")
	mockProvider.SetResponse("Node", "Processed node")
	mockProvider.SetResponse("Parallel", "Processed parallel")
	mockProvider.SetResponse("Final", "Processed final")

	registry := providers.NewRegistry()
	registry.Register(mockProvider)
	executor := NewExecutorWithRegistry(registry)

	// Workflow using edges for parallelism:
	// node1 -> parallel1, parallel2, parallel3 (in parallel) -> final
	workflow := &Workflow{
		Context: map[string]interface{}{
			"base": "value",
		},
		Nodes: []*Node{
			{
				ID:     "node1",
				Type:   NodeTypePrompt,
				Prompt: "Node 1: {{base}}",
				Model:  "mock-model",
			},
			{
				ID:     "parallel1",
				Type:   NodeTypePrompt,
				Prompt: "Parallel 1 sees: {{node1}}",
				Model:  "mock-model",
			},
			{
				ID:     "parallel2",
				Type:   NodeTypePrompt,
				Prompt: "Parallel 2 sees: {{node1}}",
				Model:  "mock-model",
			},
			{
				ID:     "parallel3",
				Type:   NodeTypePrompt,
				Prompt: "Parallel 3 sees: {{node1}}",
				Model:  "mock-model",
			},
			{
				ID:     "final",
				Type:   NodeTypePrompt,
				Prompt: "Final node sees all: {{parallel1}} {{parallel2}} {{parallel3}}",
				Model:  "mock-model",
			},
		},
		Edges: []*Edge{
			{ID: "e1", Source: "node1", Target: "parallel1"},
			{ID: "e2", Source: "node1", Target: "parallel2"},
			{ID: "e3", Source: "node1", Target: "parallel3"},
			{ID: "e4", Source: "parallel1", Target: "final"},
			{ID: "e5", Source: "parallel2", Target: "final"},
			{ID: "e6", Source: "parallel3", Target: "final"},
		},
	}

	result, err := executor.Execute(context.Background(), withStrictWorkflowDefaults(workflow), nil)
	if err != nil {
		t.Fatalf("Workflow execution failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected workflow to succeed, got: %v", result.Error)
	}

	// Verify all nodes completed
	if len(result.NodeResults) != 5 {
		t.Errorf("Expected 5 node results, got %d", len(result.NodeResults))
	}

	// Verify context contains all parallel outputs
	if _, exists := result.Context["parallel1"]; !exists {
		t.Error("Context missing parallel1 output")
	}
	if _, exists := result.Context["parallel2"]; !exists {
		t.Error("Context missing parallel2 output")
	}
	if _, exists := result.Context["parallel3"]; !exists {
		t.Error("Context missing parallel3 output")
	}
}
