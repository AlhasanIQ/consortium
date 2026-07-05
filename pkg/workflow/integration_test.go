package workflow

import (
	"context"
	"strings"
	"testing"

	"github.com/alhasaniq/consortium/pkg/providers"
)

// TestSequentialWorkflowWithMock tests sequential execution with deterministic responses
func TestSequentialWorkflowWithMock(t *testing.T) {
	// Setup mock provider
	mockProvider := NewMockProvider("mock")
	mockProvider.SetResponse("hello", "Hello! How are you?")
	mockProvider.SetResponse("How are you", "I'm doing great, thanks for asking!")

	registry := providers.NewRegistry()
	registry.Register(mockProvider)

	executor := NewExecutorWithRegistry(registry)

	wf := &Workflow{
		Name: "Sequential Test",
		Nodes: []*Node{
			{
				Type:        NodeTypePrompt,
				Model:       "mock-model",
				Prompt:      "Say hello",
				MaxTokens:   50,
				Temperature: providers.Float64Ptr(0.7),
			},
			{
				Type:        NodeTypePrompt,
				Model:       "mock-model",
				Prompt:      "Reply to: {{node_0}}",
				MaxTokens:   50,
				Temperature: providers.Float64Ptr(0.7),
			},
		},
	}

	ctx := context.Background()
	result, err := executor.Execute(ctx, withStrictWorkflowDefaults(wf), nil)

	if err != nil {
		t.Fatalf("Workflow execution failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got failure: %s", result.Error)
	}

	if len(result.NodeResults) != 2 {
		t.Errorf("Expected 2 node results, got %d", len(result.NodeResults))
	}

	// Verify first node output
	node1Result := result.NodeResults[0]
	if node1Result.NodeID != "node_0" {
		t.Errorf("Expected node_0, got %s", node1Result.NodeID)
	}
	if !strings.Contains(node1Result.Output, "Hello") {
		t.Errorf("Expected 'Hello' in output, got: %s", node1Result.Output)
	}

	// Verify second node got context from first
	node2Result := result.NodeResults[1]
	if node2Result.NodeID != "node_1" {
		t.Errorf("Expected node_1, got %s", node2Result.NodeID)
	}
	if !strings.Contains(node2Result.Output, "great") {
		t.Errorf("Expected 'great' in output, got: %s", node2Result.Output)
	}

	// Verify context
	if _, exists := result.Context["node_0"]; !exists {
		t.Error("Expected node_0 in context")
	}
	if _, exists := result.Context["node_1"]; !exists {
		t.Error("Expected node_1 in context")
	}
}

// TestParallelWorkflowWithMock tests parallel execution using DAG edges
func TestParallelWorkflowWithMock(t *testing.T) {
	mockProvider := NewMockProvider("mock")
	mockProvider.SetResponse("technical", "Technical analysis: Advanced implementation details...")
	mockProvider.SetResponse("business", "Business analysis: Strong market opportunity...")
	mockProvider.SetResponse("ethical", "Ethical analysis: Important considerations...")

	registry := providers.NewRegistry()
	registry.Register(mockProvider)

	executor := NewExecutorWithRegistry(registry)

	// Create workflow with parallel nodes using DAG (no edges = no dependencies = parallel)
	wf := &Workflow{
		Nodes: []*Node{
			{
				ID:     "technical",
				Type:   NodeTypePrompt,
				Model:  "mock-model",
				Prompt: "technical analysis",
			},
			{
				ID:     "business",
				Type:   NodeTypePrompt,
				Model:  "mock-model",
				Prompt: "business analysis",
			},
			{
				ID:     "ethical",
				Type:   NodeTypePrompt,
				Model:  "mock-model",
				Prompt: "ethical analysis",
			},
		},
		Edges: []*Edge{}, // No edges = all nodes can run in parallel
	}

	ctx := context.Background()
	result, err := executor.Execute(ctx, withStrictWorkflowDefaults(wf), nil)

	if err != nil {
		t.Fatalf("Workflow execution failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got failure: %s", result.Error)
	}

	// Check that all node outputs are in context
	if _, exists := result.Context["technical"]; !exists {
		t.Error("Expected technical in context")
	}
	if _, exists := result.Context["business"]; !exists {
		t.Error("Expected business in context")
	}
	if _, exists := result.Context["ethical"]; !exists {
		t.Error("Expected ethical in context")
	}
}

// TestConditionalWorkflowWithMock tests conditional branching
func TestConditionalWorkflowWithMock(t *testing.T) {
	mockProvider := NewMockProvider("mock")
	mockProvider.SetResponse("sentiment", "positive")
	mockProvider.SetResponse("positive feedback", "Thank you for the positive feedback!")
	mockProvider.SetResponse("concern", "We apologize for the inconvenience.")

	registry := providers.NewRegistry()
	registry.Register(mockProvider)

	executor := NewExecutorWithRegistry(registry)

	// Test positive branch
	wf := &Workflow{

		Nodes: []*Node{
			{
				Type:   NodeTypePrompt,
				Model:  "mock-model",
				Prompt: "Analyze sentiment",
			},
			{
				Type:      NodeTypeConditional,
				Condition: "node_0 contains positive",
				TrueBranch: &Node{
					Type:   NodeTypePrompt,
					Model:  "mock-model",
					Prompt: "Generate positive feedback response",
				},
				FalseBranch: &Node{
					Type:   NodeTypePrompt,
					Model:  "mock-model",
					Prompt: "Generate concern response",
				},
			},
		},
	}

	ctx := context.Background()
	result, err := executor.Execute(ctx, withStrictWorkflowDefaults(wf), nil)

	if err != nil {
		t.Fatalf("Workflow execution failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got failure: %s", result.Error)
	}

	// Should have taken positive branch
	if !strings.Contains(result.FinalOutput, "Thank you") {
		t.Errorf("Expected positive response, got: %s", result.FinalOutput)
	}
}

// TestContextPropagation tests variable interpolation across nodes
func TestContextPropagation(t *testing.T) {
	mockProvider := NewMockProvider("mock")
	mockProvider.SetResponse("Alice", "Hello Alice! Let's talk about AI.")
	mockProvider.SetResponse("AI", "Artificial Intelligence is fascinating.")

	registry := providers.NewRegistry()
	registry.Register(mockProvider)

	executor := NewExecutorWithRegistry(registry)

	wf := &Workflow{

		Nodes: []*Node{
			{
				Type:   NodeTypePrompt,
				Model:  "mock-model",
				Prompt: "Greet {{name}} and mention {{topic}}",
			},
			{
				Type:   NodeTypePrompt,
				Model:  "mock-model",
				Prompt: "Elaborate on {{topic}} from: {{node_0}}",
			},
		},
		Context: map[string]interface{}{
			"name":  "Alice",
			"topic": "AI",
		},
	}

	ctx := context.Background()
	result, err := executor.Execute(ctx, withStrictWorkflowDefaults(wf), nil)

	if err != nil {
		t.Fatalf("Workflow execution failed: %v", err)
	}

	// Verify initial context was used
	node1 := result.NodeResults[0]
	if !strings.Contains(node1.Output, "Alice") {
		t.Error("Expected name interpolation in node 1")
	}

	// Verify node output was added to context (should be node_0, not "greet")
	if greet, exists := result.Context["node_0"]; !exists {
		t.Error("Expected node_0 in context")
	} else {
		greetStr := greet.(string)
		if !strings.Contains(greetStr, "Alice") {
			t.Error("Expected Alice in node_0 output")
		}
	}
}

// TestWorkflowWithMultipleBranches tests nested conditionals
func TestNestedConditionals(t *testing.T) {
	mockProvider := NewMockProvider("mock")
	mockProvider.SetResponse("", "neutral")

	registry := providers.NewRegistry()
	registry.Register(mockProvider)

	executor := NewExecutorWithRegistry(registry)

	wf := &Workflow{

		Nodes: []*Node{
			{
				Type:   NodeTypePrompt,
				Model:  "mock-model",
				Prompt: "Analyze this",
			},
			{
				Type:      NodeTypeConditional,
				Condition: "analyze contains positive",
				TrueBranch: &Node{
					Type:   NodeTypePrompt,
					Model:  "mock-model",
					Prompt: "Positive response",
				},
				FalseBranch: &Node{
					Type:      NodeTypeConditional,
					Condition: "analyze contains negative",
					TrueBranch: &Node{
						Type:   NodeTypePrompt,
						Model:  "mock-model",
						Prompt: "Negative response",
					},
					FalseBranch: &Node{
						Type:   NodeTypePrompt,
						Model:  "mock-model",
						Prompt: "Neutral response",
					},
				},
			},
		},
	}

	ctx := context.Background()
	result, err := executor.Execute(ctx, withStrictWorkflowDefaults(wf), nil)

	if err != nil {
		t.Fatalf("Workflow execution failed: %v", err)
	}

	// Should reach neutral branch
	if !result.Success {
		t.Error("Expected successful execution")
	}

	// Verify we got output (neutral branch executed)
	if result.FinalOutput == "" {
		t.Error("Expected non-empty output from neutral branch")
	}
}
