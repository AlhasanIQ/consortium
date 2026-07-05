package workflow

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/alhasaniq/consortium/pkg/providers"
)

// TestComplexSequentialWorkflow tests a multi-node research pipeline
func TestComplexSequentialWorkflow(t *testing.T) {
	mockProvider := NewMockProvider("mock")
	mockProvider.SetResponse("research", "Research findings: AI has grown 300% in the last 5 years.")
	mockProvider.SetResponse("300%", "Key insight: The 300% growth shows massive adoption.")
	mockProvider.SetResponse("massive adoption", "Summary: AI adoption accelerating across industries.")

	registry := providers.NewRegistry()
	registry.Register(mockProvider)

	executor := NewExecutorWithRegistry(registry)

	wf := &Workflow{
		Name: "Research Pipeline",
		Nodes: []*Node{
			{
				Type:   NodeTypePrompt,
				Model:  "mock-model",
				Prompt: "Research {{topic}} trends",
			},
			{
				Type:   NodeTypePrompt,
				Model:  "mock-model",
				Prompt: "Analyze the findings: {{research}}",
			},
			{
				Type:   NodeTypePrompt,
				Model:  "mock-model",
				Prompt: "Summarize: {{analyze}}",
			},
		},
		Context: map[string]interface{}{
			"topic": "AI",
		},
	}

	ctx := context.Background()
	result, err := executor.Execute(ctx, withStrictWorkflowDefaults(wf), nil)

	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success: %s", result.Error)
	}

	// Verify all 3 nodes executed
	if len(result.NodeResults) != 3 {
		t.Errorf("Expected 3 nodes, got %d", len(result.NodeResults))
	}

	// Verify context contains all intermediate results
	requiredKeys := []string{"node_0", "node_1", "node_2"}
	for _, key := range requiredKeys {
		if _, exists := result.Context[key]; !exists {
			t.Errorf("Expected %s in context", key)
		}
	}

	// Verify final output contains expected text
	if result.FinalOutput == "" {
		t.Error("Expected non-empty final output")
	}

	// Verify total tokens tracked (may be zero for mock)
	if result.TotalTokens < 0 {
		t.Error("Expected non-negative total tokens")
	}

	// Note: Latency may be very small in tests, so we just check it's non-negative
	if result.TotalLatency < 0 {
		t.Error("Expected non-negative latency")
	}
}

// TestMixedWorkflow tests sequential + parallel + conditional
func TestMixedWorkflow(t *testing.T) {
	mockProvider := NewMockProvider("mock")
	mockProvider.SetResponse("sentiment", "positive sentiment detected")
	mockProvider.SetResponse("technical", "Technical review: excellent code quality")
	mockProvider.SetResponse("recommendation", "Final recommendation: Approve project")

	registry := providers.NewRegistry()
	registry.Register(mockProvider)

	executor := NewExecutorWithRegistry(registry)

	wf := &Workflow{
		Nodes: []*Node{
			// Node 1: Analyze sentiment
			{
				Type:   NodeTypePrompt,
				Model:  "mock-model",
				Prompt: "Analyze sentiment of feedback",
			},
			// Node 2: Conditional on sentiment
			{
				Type:      NodeTypeConditional,
				Condition: "sentiment contains positive",
				TrueBranch: &Node{
					Type:   NodeTypePrompt,
					Model:  "mock-model",
					Prompt: "Perform technical review",
				},
			},
			// Node 3: Final recommendation
			{
				Type:   NodeTypePrompt,
				Model:  "mock-model",
				Prompt: "Based on reviews, make recommendation",
			},
		},
	}

	ctx := context.Background()
	result, err := executor.Execute(ctx, withStrictWorkflowDefaults(wf), nil)

	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success: %s", result.Error)
	}

	// Verify main workflow nodes are in context
	// Node 0: sentiment analysis
	// Node 1: conditional (with branch)
	// Node 2: final recommendation
	if _, exists := result.Context["node_0"]; !exists {
		t.Error("Expected node_0 in context")
	}
	if _, exists := result.Context["node_1"]; !exists {
		t.Error("Expected node_1 in context")
	}
	if _, exists := result.Context["node_2"]; !exists {
		t.Error("Expected node_2 in context")
	}

	// Verify final recommendation
	if !strings.Contains(result.FinalOutput, "recommendation") {
		t.Errorf("Expected recommendation in output: %s", result.FinalOutput)
	}
}

// TestEmptyWorkflow tests edge case of empty workflow
func TestEmptyWorkflow(t *testing.T) {
	registry := providers.NewRegistry()
	registry.Register(NewMockProvider("mock"))

	executor := NewExecutorWithRegistry(registry)

	wf := &Workflow{
		Nodes: []*Node{},
	}

	ctx := context.Background()
	result, err := executor.Execute(ctx, withStrictWorkflowDefaults(wf), nil)

	if err != nil {
		t.Fatalf("Empty workflow should not fail: %v", err)
	}

	if !result.Success {
		t.Error("Empty workflow should succeed")
	}

	if len(result.NodeResults) != 0 {
		t.Errorf("Expected 0 results, got %d", len(result.NodeResults))
	}
}

// TestParallelWithMixedResponses tests parallel execution using DAG
func TestParallelWithMixedResponses(t *testing.T) {
	mockProvider := NewMockProvider("mock")
	mockProvider.SetResponse("node1", "Success 1")
	mockProvider.SetResponse("node2", "Success 2")
	// node3 will get default response

	registry := providers.NewRegistry()
	registry.Register(mockProvider)

	executor := NewExecutorWithRegistry(registry)

	// Create workflow with parallel nodes using DAG (no edges = no dependencies)
	wf := &Workflow{
		Nodes: []*Node{
			{
				ID:     "node1",
				Type:   NodeTypePrompt,
				Model:  "mock-model",
				Prompt: "Execute node1",
			},
			{
				ID:     "node2",
				Type:   NodeTypePrompt,
				Model:  "mock-model",
				Prompt: "Execute node2",
			},
			{
				ID:     "node3",
				Type:   NodeTypePrompt,
				Model:  "mock-model",
				Prompt: "Execute node3",
			},
		},
		Edges: []*Edge{}, // No edges = all nodes can run in parallel
	}

	ctx := context.Background()
	result, err := executor.Execute(ctx, withStrictWorkflowDefaults(wf), nil)

	// Should succeed with default responses
	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	if !result.Success {
		t.Error("Expected success with default responses")
	}
}

// TestConditionalWithNoMatchingBranch tests conditional with no branch taken
func TestConditionalWithNoMatchingBranch(t *testing.T) {
	mockProvider := NewMockProvider("mock")
	mockProvider.SetResponse("Analyze", "neutral")

	registry := providers.NewRegistry()
	registry.Register(mockProvider)

	executor := NewExecutorWithRegistry(registry)

	wf := &Workflow{

		Nodes: []*Node{
			{
				Type:   NodeTypePrompt,
				Model:  "mock-model",
				Prompt: "Analyze",
			},
			{
				Type:      NodeTypeConditional,
				Condition: "analyze contains IMPOSSIBLE_MATCH",
				TrueBranch: &Node{
					Type:   NodeTypePrompt,
					Model:  "mock-model",
					Prompt: "True branch",
				},
				// No false branch
			},
		},
	}

	ctx := context.Background()
	result, err := executor.Execute(ctx, withStrictWorkflowDefaults(wf), nil)

	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	if !result.Success {
		t.Error("Workflow should succeed even when no branch matches")
	}

	// Final output should be from analyze node (not the conditional)
	if !strings.Contains(result.FinalOutput, "neutral") {
		t.Error("Expected neutral output when no conditional branch taken")
	}
}

// TestWorkflowContextInitialization tests that initial context is available
func TestWorkflowContextInitialization(t *testing.T) {
	mockProvider := NewMockProvider("mock")
	mockProvider.SetResponse("Alice", "Hello Alice, your role is admin")
	mockProvider.SetResponse("admin", "Admin dashboard loaded")

	registry := providers.NewRegistry()
	registry.Register(mockProvider)

	executor := NewExecutorWithRegistry(registry)

	wf := &Workflow{

		Nodes: []*Node{
			{
				Type:   NodeTypePrompt,
				Model:  "mock-model",
				Prompt: "Greet {{user}} with role {{role}}",
			},
			{
				Type:   NodeTypePrompt,
				Model:  "mock-model",
				Prompt: "Load dashboard for {{role}}",
			},
		},
		Context: map[string]interface{}{
			"user": "Alice",
			"role": "admin",
			"org":  "AcmeCorp",
		},
	}

	ctx := context.Background()
	result, err := executor.Execute(ctx, withStrictWorkflowDefaults(wf), nil)

	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	// Verify initial context preserved
	if result.Context["user"] != "Alice" {
		t.Error("Initial context user not preserved")
	}
	if result.Context["role"] != "admin" {
		t.Error("Initial context role not preserved")
	}
	if result.Context["org"] != "AcmeCorp" {
		t.Error("Initial context org not preserved")
	}

	// Verify node outputs added to context (node_0, not "personalize")
	if _, exists := result.Context["node_0"]; !exists {
		t.Error("Node output not added to context")
	}
}

// TestLargeParallelExecution tests many parallel nodes using DAG
func TestLargeParallelExecution(t *testing.T) {
	mockProvider := NewMockProvider("mock")
	for i := 0; i < 10; i++ {
		mockProvider.SetResponse("", "Response") // Default for all
	}

	registry := providers.NewRegistry()
	registry.Register(mockProvider)

	executor := NewExecutorWithRegistry(registry)

	// Create 10 parallel nodes using DAG (no edges = no dependencies = parallel)
	nodes := make([]*Node, 10)
	for i := 0; i < 10; i++ {
		nodes[i] = &Node{
			ID:     fmt.Sprintf("node_%d", i),
			Type:   NodeTypePrompt,
			Model:  "mock-model",
			Prompt: "Execute subnode",
		}
	}

	wf := &Workflow{
		Nodes: nodes,
		Edges: []*Edge{}, // No edges = all nodes can run in parallel
	}

	ctx := context.Background()
	result, err := executor.Execute(ctx, withStrictWorkflowDefaults(wf), nil)

	if err != nil {
		t.Fatalf("Large parallel workflow failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success: %s", result.Error)
	}

	// Verify all nodes executed
	if len(result.Context) < 10 {
		t.Errorf("Expected at least 10 items in context, got %d", len(result.Context))
	}
}

// TestNodeMetadataPreservation tests that metadata is preserved
func TestNodeMetadataPreservation(t *testing.T) {
	mockProvider := NewMockProvider("mock")
	mockProvider.SetResponse("", "Response")

	registry := providers.NewRegistry()
	registry.Register(mockProvider)

	executor := NewExecutorWithRegistry(registry)

	wf := &Workflow{

		Nodes: []*Node{
			{
				Type:   NodeTypePrompt,
				Model:  "mock-model",
				Prompt: "Test",
				Metadata: map[string]interface{}{
					"category": "test",
					"priority": 1,
					"tags":     []string{"unit", "integration"},
				},
			},
		},
	}

	ctx := context.Background()
	result, err := executor.Execute(ctx, withStrictWorkflowDefaults(wf), nil)

	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	// Verify metadata preserved in result
	nodeResult := result.NodeResults[0]
	if nodeResult.Metadata == nil {
		t.Fatal("Expected metadata in result")
	}

	if nodeResult.Metadata["category"] != "test" {
		t.Error("Metadata category not preserved")
	}

	if nodeResult.Metadata["priority"] != 1 {
		t.Error("Metadata priority not preserved")
	}
}

// TestWorkflowIDPropagation tests workflow ID is in result
func TestWorkflowIDPropagation(t *testing.T) {
	mockProvider := NewMockProvider("mock")
	mockProvider.SetResponse("", "Response")

	registry := providers.NewRegistry()
	registry.Register(mockProvider)

	executor := NewExecutorWithRegistry(registry)

	wf := &Workflow{
		ID: "my-unique-workflow-123",
		Nodes: []*Node{
			{
				Type:   NodeTypePrompt,
				Model:  "mock-model",
				Prompt: "Test",
			},
		},
	}

	ctx := context.Background()
	result, err := executor.Execute(ctx, withStrictWorkflowDefaults(wf), nil)

	if err != nil {
		t.Fatalf("Workflow failed: %v", err)
	}

	if result.WorkflowID != "my-unique-workflow-123" {
		t.Errorf("Expected workflow ID 'my-unique-workflow-123', got '%s'", result.WorkflowID)
	}
}
