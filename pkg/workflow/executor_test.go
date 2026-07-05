package workflow

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/trace"
)

type mockTraceWriter struct {
	spans []*trace.Span
}

func (m *mockTraceWriter) WriteSpan(ctx context.Context, span *trace.Span) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	m.spans = append(m.spans, span)
	return nil
}

func TestExecuteOutputNode(t *testing.T) {
	tests := []struct {
		name            string
		node            *Node
		workflowContext map[string]interface{}
		expectedOutput  string
		checkMetadata   bool // whether to verify output_value in metadata
	}{
		{
			name: "output with custom name",
			node: &Node{
				Type: NodeTypeResult,
				Metadata: map[string]interface{}{
					"output_name": "summary",
					"input_ids":   []string{"node1"},
				},
			},
			workflowContext: map[string]interface{}{"node1": "final result"},
			expectedOutput:  "final result",
		},
		{
			name: "output with OutputName field",
			node: &Node{
				Type:       NodeTypeResult,
				OutputName: "result",
				Metadata: map[string]interface{}{
					"input_ids": []string{"node1"},
				},
			},
			workflowContext: map[string]interface{}{"node1": "test output"},
			expectedOutput:  "test output",
		},
		{
			name: "output with multiple inputs",
			node: &Node{
				Type: NodeTypeResult,
				Metadata: map[string]interface{}{
					"input_ids": []interface{}{"node1", "node2"},
				},
			},
			workflowContext: map[string]interface{}{"node1": "first", "node2": "second"},
			expectedOutput:  "first\n---\nsecond",
			checkMetadata:   true,
		},
		{
			name:            "output with no inputs",
			node:            &Node{Type: NodeTypeResult},
			workflowContext: map[string]interface{}{},
			expectedOutput:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := NewTestExecutor()
			result, err := executor.executeNode(context.Background(), withStrictNodeDefaults(tt.node), "test_node", tt.workflowContext, nil, nil)
			if err != nil {
				t.Fatalf("executeOutputNode failed: %v", err)
			}
			if !result.Success {
				t.Error("expected success")
			}
			if result.Output != tt.expectedOutput {
				t.Errorf("output = %q, want %q", result.Output, tt.expectedOutput)
			}
			if tt.checkMetadata {
				if result.Metadata["output_value"] == nil {
					t.Error("expected output_value in metadata")
				}
			}
		})
	}
}

func TestNewExecutor(t *testing.T) {
	executor := NewExecutor(nil)
	if executor == nil {
		t.Fatal("expected executor")
	}
	if executor.RunnerRegistry() == nil {
		t.Fatal("expected runner registry to be initialized")
	}
}

func TestWriteSpan_UsesFallbackContextWhenCancelled(t *testing.T) {
	writer := &mockTraceWriter{}
	execCtx := &ExecutionContext{
		JobID:       "job-timeout-trace",
		TraceWriter: writer,
	}
	span := &trace.Span{
		NodeID:     "node-1",
		SpanID:     "span-1",
		Kind:       trace.KindNode,
		Status:     trace.StatusError,
		StartedAt:  time.Now(),
		Attributes: map[string]any{"error": "timeout"},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	writeSpan(ctx, execCtx, span)

	if len(writer.spans) != 1 {
		t.Fatalf("expected span to be written with fallback context, got %d", len(writer.spans))
	}
	if writer.spans[0].JobID != "job-timeout-trace" {
		t.Fatalf("expected span job_id propagation, got %q", writer.spans[0].JobID)
	}
}

func TestExecuteNode_Wrapper(t *testing.T) {
	executor := NewTestExecutor()

	ctx := context.Background()
	workflowContext := map[string]interface{}{}

	node := &Node{
		Type: NodeTypeResult,
	}

	// This tests the wrapper function that calls executeNodeWithContext
	result, err := executor.executeNode(ctx, withStrictNodeDefaults(node), "test_node", workflowContext, nil, nil)
	if err != nil {
		t.Fatalf("executeNode failed: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestExecutePromptNode_Wrapper(t *testing.T) {
	executor := NewTestExecutor()
	ctx := context.Background()
	workflowContext := map[string]interface{}{}

	node := &Node{
		Type:   NodeTypePrompt,
		Model:  "mock-model",
		Prompt: "test",
	}

	// Test the wrapper function via executeNode
	result, err := executor.executeNode(ctx, withStrictNodeDefaults(node), "test_node", workflowContext, nil, nil)
	if err != nil {
		t.Fatalf("executeNode (prompt) failed: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestExecuteConditionalNode_Wrapper(t *testing.T) {
	executor := NewTestExecutor()
	ctx := context.Background()
	workflowContext := map[string]interface{}{
		"test": "positive",
	}

	node := &Node{
		Type:        NodeTypeConditional,
		Condition:   "test contains positive",
		TrueBranch:  &Node{Type: NodeTypeResult},
		FalseBranch: &Node{Type: NodeTypeResult},
	}

	// Test the wrapper function via executeNode
	result, err := executor.executeNode(ctx, withStrictNodeDefaults(node), "test_node", workflowContext, nil, nil)
	if err != nil {
		t.Fatalf("executeNode (conditional) failed: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
}

func TestExecuteWithContext(t *testing.T) {
	executor := NewTestExecutor()

	wf := &Workflow{
		Name:  "Test",
		Nodes: []*Node{},
		Context: map[string]interface{}{
			"key": "value",
		},
	}

	ctx := context.Background()
	execCtx := &ExecutionContext{JobID: "test-job"}

	result, err := executor.Execute(ctx, withStrictWorkflowDefaults(wf), execCtx)
	if err != nil {
		t.Fatalf("ExecuteWithContext failed: %v", err)
	}
	if !result.Success {
		t.Error("expected success")
	}
	// Verify context was preserved
	if val, ok := result.Context["key"]; !ok || val != "value" {
		t.Error("expected context to be preserved")
	}
}

func TestExecutePromptNodeErrors(t *testing.T) {
	t.Run("model not found error", func(t *testing.T) {
		registry := providers.NewRegistry()
		executor := NewExecutorWithRegistry(registry)

		wf := &Workflow{
			Name: "Error Test",
			Nodes: []*Node{
				{
					Type:        NodeTypePrompt,
					Model:       "nonexistent-model",
					Prompt:      "Say 'hello'",
					MaxTokens:   50,
					Temperature: providers.Float64Ptr(0.7),
				},
			},
		}

		ctx := context.Background()
		result, err := executor.Execute(ctx, withStrictWorkflowDefaults(wf), nil)

		// Should get an error since model doesn't exist
		if err == nil {
			t.Fatal("Expected error for nonexistent model, got nil")
		}

		if result.Success {
			t.Error("Expected failure for nonexistent model")
		}
	})
}

// TestInterpolateVariables tests variable interpolation
func TestInterpolateVariables(t *testing.T) {
	ctx := map[string]interface{}{
		"name":  "Alice",
		"topic": "artificial intelligence",
		"count": 42,
	}

	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "Hello {{name}}",
			expected: "Hello Alice",
		},
		{
			input:    "The topic is {{topic}}",
			expected: "The topic is artificial intelligence",
		},
		{
			input:    "Count: {{count}}",
			expected: "Count: 42",
		},
		{
			input:    "{{name}} studies {{topic}}",
			expected: "Alice studies artificial intelligence",
		},
		{
			input:    "No variables here",
			expected: "No variables here",
		},
		{
			input:    "Missing {{unknown}} variable",
			expected: "Missing {{unknown}} variable",
		},
		{
			input:    "{{name}} {{name}} {{name}}",
			expected: "Alice Alice Alice",
		},
		{
			input:    "Input data:\n{{name}}\n{{topic}}\n{{count}}",
			expected: "Input data:\nAlice\nartificial intelligence\n42",
		},
	}

	for _, tt := range tests {
		result := InterpolateVariables(tt.input, ctx)
		if result != tt.expected {
			t.Errorf("InterpolateVariables(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// TestEvaluateCondition tests condition evaluation
func TestEvaluateCondition(t *testing.T) {

	tests := []struct {
		name      string
		condition string
		context   map[string]interface{}
		variables map[string]string
		expected  bool
		shouldErr bool
	}{
		{
			name:      "contains positive case",
			condition: "sentiment contains positive",
			context:   map[string]interface{}{"sentiment": "positive feedback"},
			variables: nil,
			expected:  true,
			shouldErr: false,
		},
		{
			name:      "contains negative case",
			condition: "sentiment contains negative",
			context:   map[string]interface{}{"sentiment": "positive feedback"},
			variables: nil,
			expected:  false,
			shouldErr: false,
		},
		{
			name:      "equals positive case",
			condition: "status equals completed",
			context:   map[string]interface{}{"status": "completed"},
			variables: nil,
			expected:  true,
			shouldErr: false,
		},
		{
			name:      "equals negative case",
			condition: "status equals pending",
			context:   map[string]interface{}{"status": "completed"},
			variables: nil,
			expected:  false,
			shouldErr: false,
		},
		{
			name:      "not_empty with value",
			condition: "name not_empty",
			context:   map[string]interface{}{"name": "Alice"},
			variables: nil,
			expected:  true,
			shouldErr: false,
		},
		{
			name:      "not_empty with empty string",
			condition: "name not_empty",
			context:   map[string]interface{}{"name": ""},
			variables: nil,
			expected:  false,
			shouldErr: false,
		},
		{
			name:      "case insensitive contains",
			condition: "result contains SUCCESS",
			context:   map[string]interface{}{"result": "Operation was successful"},
			variables: nil,
			expected:  true,
			shouldErr: false,
		},
		{
			name:      "empty condition defaults to true",
			condition: "",
			context:   map[string]interface{}{},
			variables: nil,
			expected:  true,
			shouldErr: false,
		},
		{
			name:      "multi-word search value",
			condition: "message contains hello world",
			context:   map[string]interface{}{"message": "I said hello world today"},
			variables: nil,
			expected:  true,
			shouldErr: false,
		},
		{
			name:      "variable from variables map",
			condition: "status equals active",
			context:   map[string]interface{}{"status": "active"},
			variables: map[string]string{"status": "active"},
			expected:  true,
			shouldErr: false,
		},
		{
			name:      "invalid condition format",
			condition: "invalid",
			context:   map[string]interface{}{},
			variables: nil,
			expected:  false,
			shouldErr: true,
		},
		{
			name:      "unknown operator",
			condition: "value greater_than 5",
			context:   map[string]interface{}{"value": "10"},
			variables: nil,
			expected:  false,
			shouldErr: true,
		},
		{
			name:      "contains without value",
			condition: "field contains",
			context:   map[string]interface{}{"field": "test"},
			variables: nil,
			expected:  false,
			shouldErr: true,
		},
		{
			name:      "equals without value",
			condition: "field equals",
			context:   map[string]interface{}{"field": "test"},
			variables: nil,
			expected:  false,
			shouldErr: true,
		},
		// Numeric operators
		{
			name:      "greater than true",
			condition: "count > 10",
			context:   map[string]interface{}{"count": "15"},
			expected:  true,
			shouldErr: false,
		},
		{
			name:      "greater than false",
			condition: "count > 10",
			context:   map[string]interface{}{"count": "5"},
			expected:  false,
			shouldErr: false,
		},
		{
			name:      "less than true",
			condition: "score < 0.5",
			context:   map[string]interface{}{"score": "0.3"},
			expected:  true,
			shouldErr: false,
		},
		{
			name:      "less than false",
			condition: "score < 0.5",
			context:   map[string]interface{}{"score": "0.7"},
			expected:  false,
			shouldErr: false,
		},
		{
			name:      "greater than or equal true boundary",
			condition: "value >= 10",
			context:   map[string]interface{}{"value": "10"},
			expected:  true,
			shouldErr: false,
		},
		{
			name:      "greater than or equal true above",
			condition: "value >= 10",
			context:   map[string]interface{}{"value": "15"},
			expected:  true,
			shouldErr: false,
		},
		{
			name:      "less than or equal true boundary",
			condition: "value <= 10",
			context:   map[string]interface{}{"value": "10"},
			expected:  true,
			shouldErr: false,
		},
		{
			name:      "numeric equality true",
			condition: "value == 42",
			context:   map[string]interface{}{"value": "42"},
			expected:  true,
			shouldErr: false,
		},
		{
			name:      "numeric equality false",
			condition: "value == 42",
			context:   map[string]interface{}{"value": "43"},
			expected:  false,
			shouldErr: false,
		},
		{
			name:      "numeric operator non-numeric left",
			condition: "value > 10",
			context:   map[string]interface{}{"value": "abc"},
			expected:  false,
			shouldErr: true,
		},
		{
			name:      "numeric operator non-numeric right",
			condition: "value > abc",
			context:   map[string]interface{}{"value": "10"},
			expected:  false,
			shouldErr: true,
		},
		// Regex operator
		{
			name:      "matches regex positive",
			condition: "email matches ^[a-z]+@[a-z]+\\.[a-z]+$",
			context:   map[string]interface{}{"email": "test@example.com"},
			expected:  true,
			shouldErr: false,
		},
		{
			name:      "matches regex negative",
			condition: "email matches ^[a-z]+@[a-z]+\\.[a-z]+$",
			context:   map[string]interface{}{"email": "invalid-email"},
			expected:  false,
			shouldErr: false,
		},
		{
			name:      "matches invalid regex",
			condition: "value matches [invalid",
			context:   map[string]interface{}{"value": "test"},
			expected:  false,
			shouldErr: true,
		},
		{
			name:      "matches without pattern",
			condition: "value matches",
			context:   map[string]interface{}{"value": "test"},
			expected:  false,
			shouldErr: true,
		},
		// Type checks
		{
			name:      "is_number true integer",
			condition: "value is_number",
			context:   map[string]interface{}{"value": "42"},
			expected:  true,
			shouldErr: false,
		},
		{
			name:      "is_number true float",
			condition: "value is_number",
			context:   map[string]interface{}{"value": "3.14"},
			expected:  true,
			shouldErr: false,
		},
		{
			name:      "is_number false",
			condition: "value is_number",
			context:   map[string]interface{}{"value": "not a number"},
			expected:  false,
			shouldErr: false,
		},
		{
			name:      "is_string true",
			condition: "value is_string",
			context:   map[string]interface{}{"value": "hello"},
			expected:  true,
			shouldErr: false,
		},
		{
			name:      "is_string false for missing",
			condition: "missing is_string",
			context:   map[string]interface{}{},
			expected:  false,
			shouldErr: false,
		},
		{
			name:      "is_empty true",
			condition: "value is_empty",
			context:   map[string]interface{}{"value": ""},
			expected:  true,
			shouldErr: false,
		},
		{
			name:      "is_empty false",
			condition: "value is_empty",
			context:   map[string]interface{}{"value": "not empty"},
			expected:  false,
			shouldErr: false,
		},
		// Boolean operators
		{
			name:      "AND both true",
			condition: "a not_empty AND b not_empty",
			context:   map[string]interface{}{"a": "x", "b": "y"},
			expected:  true,
			shouldErr: false,
		},
		{
			name:      "AND left false",
			condition: "a not_empty AND b not_empty",
			context:   map[string]interface{}{"a": "", "b": "y"},
			expected:  false,
			shouldErr: false,
		},
		{
			name:      "AND right false",
			condition: "a not_empty AND b not_empty",
			context:   map[string]interface{}{"a": "x", "b": ""},
			expected:  false,
			shouldErr: false,
		},
		{
			name:      "OR both true",
			condition: "a not_empty OR b not_empty",
			context:   map[string]interface{}{"a": "x", "b": "y"},
			expected:  true,
			shouldErr: false,
		},
		{
			name:      "OR left true",
			condition: "a not_empty OR b not_empty",
			context:   map[string]interface{}{"a": "x", "b": ""},
			expected:  true,
			shouldErr: false,
		},
		{
			name:      "OR both false",
			condition: "a not_empty OR b not_empty",
			context:   map[string]interface{}{"a": "", "b": ""},
			expected:  false,
			shouldErr: false,
		},
		{
			name:      "NOT true becomes false",
			condition: "NOT a not_empty",
			context:   map[string]interface{}{"a": "x"},
			expected:  false,
			shouldErr: false,
		},
		{
			name:      "NOT false becomes true",
			condition: "NOT a not_empty",
			context:   map[string]interface{}{"a": ""},
			expected:  true,
			shouldErr: false,
		},
		{
			name:      "complex AND with numeric",
			condition: "count > 5 AND status equals active",
			context:   map[string]interface{}{"count": "10", "status": "active"},
			expected:  true,
			shouldErr: false,
		},
		{
			name:      "complex OR with contains",
			condition: "result contains error OR status equals failed",
			context:   map[string]interface{}{"result": "success", "status": "failed"},
			expected:  true,
			shouldErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := EvaluateConditionExpression(tt.condition, tt.context)
			if tt.shouldErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !tt.shouldErr && result != tt.expected {
				t.Errorf("got %v, want %v", result, tt.expected)
			}
		})
	}
}

// TestWorkflowContextHandling tests input context propagation through workflow
func TestWorkflowContextHandling(t *testing.T) {

	tests := []struct {
		name            string
		workflow        *Workflow
		expectedContext map[string]interface{}
		description     string
	}{
		{
			name: "single input in context",
			workflow: &Workflow{
				Name: "Input Context Test",
				Context: map[string]interface{}{
					"bookname": "The Great Gatsby",
				},
				Nodes: []*Node{},
			},
			expectedContext: map[string]interface{}{
				"bookname": "The Great Gatsby",
			},
			description: "Should preserve single input field in context",
		},
		{
			name: "multiple inputs in context",
			workflow: &Workflow{
				Name: "Multi Input Test",
				Context: map[string]interface{}{
					"bookname": "1984",
					"author":   "George Orwell",
					"year":     "1949",
				},
				Nodes: []*Node{},
			},
			expectedContext: map[string]interface{}{
				"bookname": "1984",
				"author":   "George Orwell",
				"year":     "1949",
			},
			description: "Should preserve multiple input fields in context",
		},
		{
			name: "empty context",
			workflow: &Workflow{
				Name:    "Empty Context Test",
				Context: nil,
				Nodes:   []*Node{},
			},
			expectedContext: map[string]interface{}{},
			description:     "Should handle nil context gracefully",
		},
		{
			name: "context with various types",
			workflow: &Workflow{
				Name: "Type Context Test",
				Context: map[string]interface{}{
					"text":    "hello",
					"number":  42,
					"boolean": true,
					"float":   3.14,
				},
				Nodes: []*Node{},
			},
			expectedContext: map[string]interface{}{
				"text":    "hello",
				"number":  42,
				"boolean": true,
				"float":   3.14,
			},
			description: "Should preserve various data types in context",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := NewTestExecutor()
			ctx := context.Background()
			result, err := executor.Execute(ctx, withStrictWorkflowDefaults(tt.workflow), nil)

			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}

			if !result.Success {
				t.Fatalf("Workflow should succeed: %s", result.Error)
			}

			// Check context is preserved
			for key, expectedValue := range tt.expectedContext {
				actualValue, ok := result.Context[key]
				if !ok {
					t.Errorf("Context missing key %q", key)
					continue
				}
				if actualValue != expectedValue {
					t.Errorf("Context[%q] = %v, want %v", key, actualValue, expectedValue)
				}
			}
		})
	}
}

// TestWorkflowWithInputData tests end-to-end workflow execution with input data
func TestWorkflowWithInputData(t *testing.T) {
	executor := NewTestExecutor()

	// Simulate a workflow with input data (like from frontend input dialog)
	wf := &Workflow{
		Name: "Input Flow Test",
		Context: map[string]interface{}{
			"bookname": "The Great Gatsby",
		},
		Nodes: []*Node{
			{
				Type:  NodeTypePrompt,
				Model: "mock-model",
				// This prompt would be auto-generated by frontend converter
				Prompt:      "Input data:\n{{bookname}}\n\nYou summarize books like a professional reviewer.",
				MaxTokens:   200,
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

	// Verify context was preserved
	if bookname, ok := result.Context["bookname"]; !ok || bookname != "The Great Gatsby" {
		t.Errorf("Expected bookname in context, got %v", result.Context)
	}
}

// TestCostTracker tests the CostTracker functionality
func TestCostTracker(t *testing.T) {
	t.Run("tracks cumulative costs", func(t *testing.T) {
		tracker := NewCostTracker(&CostLimits{MaxCostUSD: 1.0})

		tracker.Add(100, 50, 0.001)
		tracker.Add(200, 100, 0.002)

		cost, tokens, inputTokens, outputTokens := tracker.GetTotals()
		if cost != 0.003 {
			t.Errorf("TotalCost = %f, want 0.003", cost)
		}
		if tokens != 450 {
			t.Errorf("TotalTokens = %d, want 450", tokens)
		}
		if inputTokens != 300 {
			t.Errorf("TotalInputTokens = %d, want 300", inputTokens)
		}
		if outputTokens != 150 {
			t.Errorf("TotalOutputTokens = %d, want 150", outputTokens)
		}
	})

	t.Run("checks cost limit", func(t *testing.T) {
		tracker := NewCostTracker(&CostLimits{MaxCostUSD: 0.001})

		tracker.Add(100, 50, 0.0005)
		err := tracker.CheckLimits("node_1")
		if err != nil {
			t.Errorf("Expected no error under limit, got: %v", err)
		}

		tracker.Add(100, 50, 0.001) // This should exceed the limit
		err = tracker.CheckLimits("node_2")
		if err == nil {
			t.Error("Expected cost limit error")
		}
		if !IsCostLimitError(err) {
			t.Errorf("Expected CostLimitError, got %T", err)
		}
	})

	t.Run("checks token limit", func(t *testing.T) {
		tracker := NewCostTracker(&CostLimits{MaxTokens: 200})

		tracker.Add(100, 50, 0.001)
		err := tracker.CheckLimits("node_1")
		if err != nil {
			t.Errorf("Expected no error under limit, got: %v", err)
		}

		tracker.Add(100, 50, 0.001) // 300 total > 200 limit
		err = tracker.CheckLimits("node_2")
		if err == nil {
			t.Error("Expected token limit error")
		}
		if !IsCostLimitError(err) {
			t.Errorf("Expected CostLimitError, got %T", err)
		}
	})

	t.Run("checks input token limit", func(t *testing.T) {
		tracker := NewCostTracker(&CostLimits{MaxInputTokens: 150})

		tracker.Add(100, 50, 0.001)
		err := tracker.CheckLimits("node_1")
		if err != nil {
			t.Errorf("Expected no error under limit, got: %v", err)
		}

		tracker.Add(100, 50, 0.001) // 200 input > 150 limit
		err = tracker.CheckLimits("node_2")
		if err == nil {
			t.Error("Expected input token limit error")
		}
	})

	t.Run("checks output token limit", func(t *testing.T) {
		tracker := NewCostTracker(&CostLimits{MaxOutputTokens: 75})

		tracker.Add(100, 50, 0.001)
		err := tracker.CheckLimits("node_1")
		if err != nil {
			t.Errorf("Expected no error under limit, got: %v", err)
		}

		tracker.Add(100, 50, 0.001) // 100 output > 75 limit
		err = tracker.CheckLimits("node_2")
		if err == nil {
			t.Error("Expected output token limit error")
		}
	})

	t.Run("nil limits allows everything", func(t *testing.T) {
		tracker := NewCostTracker(nil)

		tracker.Add(1000000, 1000000, 1000.0)
		err := tracker.CheckLimits("node_1")
		if err != nil {
			t.Errorf("Expected no error with nil limits, got: %v", err)
		}
	})
}

// TestCostTracker_ConcurrentAddAndCheck tests thread safety of CostTracker.AddAndCheck.
// Multiple goroutines concurrently call AddAndCheck, simulating parallel node execution.
func TestCostTracker_ConcurrentAddAndCheck(t *testing.T) {
	const (
		numGoroutines = 100
		costPerAdd    = 0.02 // Each goroutine adds 0.02
		costLimit     = 1.0  // Limit is 1.0, will be exceeded after ~50+ adds
	)

	tracker := NewCostTracker(&CostLimits{MaxCostUSD: costLimit})

	var wg sync.WaitGroup
	var mu sync.Mutex
	limitBreaches := 0

	// Spawn 100 goroutines, each calling AddAndCheck
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			nodeID := fmt.Sprintf("node_%d", idx)
			// Each call adds 0.02 (10 input tokens, 10 output tokens at 0.001 cost each)
			err := tracker.AddAndCheck(10, 10, costPerAdd, nodeID)
			if err != nil {
				if !IsCostLimitError(err) {
					t.Errorf("Expected CostLimitError, got %T: %v", err, err)
				}
				mu.Lock()
				limitBreaches++
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()

	// Verify final totals: 100 * 0.02 = 2.0 cost, 100 * 20 = 2000 tokens
	finalCost, finalTokens, finalInputTokens, finalOutputTokens := tracker.GetTotals()

	expectedCost := float64(numGoroutines) * costPerAdd
	expectedTokens := numGoroutines * 20
	expectedInputTokens := numGoroutines * 10
	expectedOutputTokens := numGoroutines * 10

	// Use epsilon comparison for floating point to handle rounding
	const epsilon = 0.0001
	if diff := finalCost - expectedCost; diff < -epsilon || diff > epsilon {
		t.Errorf("Final cost = %f, want %f (diff=%f)", finalCost, expectedCost, diff)
	}
	if finalTokens != expectedTokens {
		t.Errorf("Final tokens = %d, want %d", finalTokens, expectedTokens)
	}
	if finalInputTokens != expectedInputTokens {
		t.Errorf("Final input tokens = %d, want %d", finalInputTokens, expectedInputTokens)
	}
	if finalOutputTokens != expectedOutputTokens {
		t.Errorf("Final output tokens = %d, want %d", finalOutputTokens, expectedOutputTokens)
	}

	// Verify that at least one goroutine detected the limit breach
	// (total 2.0 exceeds limit of 1.0, so at least one goroutine should hit the error)
	if limitBreaches == 0 {
		t.Error("Expected at least one goroutine to detect cost limit breach, but got 0")
	}

	// Verify that the number of limit breaches is reasonable.
	// With 100 goroutines * 0.02 = 2.0 total, and limit of 1.0,
	// roughly 50+ goroutines should detect the breach (plus race conditions).
	// Allow some variance but ensure we have more than just 1 breach.
	if limitBreaches < 10 {
		t.Errorf("Expected many goroutines to detect breach, got only %d (expected ~40-60)", limitBreaches)
	}
	if limitBreaches > numGoroutines {
		t.Errorf("Unexpected number of limit breaches: %d (max: %d)", limitBreaches, numGoroutines)
	}

	t.Logf("Test completed: %d/%d goroutines detected limit breach", limitBreaches, numGoroutines)
}

// TestCostLimitErrorTypes tests the CostLimitError type creation
func TestCostLimitErrorTypes(t *testing.T) {
	t.Run("cost limit error message", func(t *testing.T) {
		err := NewCostLimitError("cost", 0.5, 0.52, "node_2")
		if err.Code != ErrCodeCostLimitExceeded {
			t.Errorf("Expected code %s, got %s", ErrCodeCostLimitExceeded, err.Code)
		}
		if err.LimitType != "cost" {
			t.Errorf("Expected limit type 'cost', got %s", err.LimitType)
		}
		if !IsCostLimitError(err) {
			t.Error("Expected IsCostLimitError to return true")
		}
	})

	t.Run("token limit error message", func(t *testing.T) {
		err := NewCostLimitError("tokens", 1000, 1200, "node_3")
		if err.Code != ErrCodeTokenLimitExceeded {
			t.Errorf("Expected code %s, got %s", ErrCodeTokenLimitExceeded, err.Code)
		}
	})

	t.Run("AsCostLimitError extracts error", func(t *testing.T) {
		err := NewCostLimitError("cost", 0.5, 0.6, "node_1")
		costErr, ok := AsCostLimitError(err)
		if !ok {
			t.Error("Expected AsCostLimitError to return true")
		}
		if costErr.LimitValue != 0.5 {
			t.Errorf("Expected limit value 0.5, got %f", costErr.LimitValue)
		}
	})
}

// TestWorkflowCostLimits tests cost limit enforcement during workflow execution
func TestWorkflowCostLimits(t *testing.T) {
	// Mock provider costs: 0.001/input token, 0.002/output token
	// Each node: 100 input * 0.001 + 50 output * 0.002 = 0.2
	promptNode := func(id, prompt string) *Node {
		return &Node{ID: id, Type: NodeTypePrompt, Model: "mock-model", Prompt: prompt}
	}

	tests := []struct {
		name             string
		nodes            []*Node
		limits           *CostLimits
		wantErr          bool
		wantSuccess      bool
		wantCostExceeded bool
		wantLimitDetails bool
		wantLimitType    string
		minNodeResults   int // 0 means don't check
	}{
		{
			name:        "workflow succeeds within cost limits",
			nodes:       []*Node{promptNode("node1", "Hello")},
			limits:      &CostLimits{MaxCostUSD: 1.0},
			wantSuccess: true,
		},
		{
			name:             "workflow fails when cost limit exceeded",
			nodes:            []*Node{promptNode("node1", "Hello")},
			limits:           &CostLimits{MaxCostUSD: 0.15},
			wantErr:          true,
			wantCostExceeded: true,
			wantLimitDetails: true,
		},
		{
			name:             "workflow fails when token limit exceeded",
			nodes:            []*Node{promptNode("node1", "Hello")},
			limits:           &CostLimits{MaxTokens: 100},
			wantErr:          true,
			wantLimitDetails: true,
			wantLimitType:    "tokens",
		},
		{
			name:        "workflow with no limits succeeds",
			nodes:       []*Node{promptNode("node1", "Hello"), promptNode("node2", "World")},
			limits:      nil,
			wantSuccess: true,
		},
		{
			name: "multi-node workflow stops at limit",
			nodes: []*Node{
				promptNode("node1", "First"),
				promptNode("node2", "Second"),
				promptNode("node3", "Third"),
			},
			limits:         &CostLimits{MaxCostUSD: 0.5},
			wantErr:        true,
			minNodeResults: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := NewTestExecutorWithCost(0.001, 0.002)
			wf := &Workflow{
				ID:     "test-workflow",
				Name:   tt.name,
				Nodes:  tt.nodes,
				Limits: tt.limits,
			}

			result, err := executor.Execute(context.Background(), withStrictWorkflowDefaults(wf), nil)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !IsCostLimitError(err) {
					t.Errorf("expected CostLimitError, got %T: %v", err, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			if tt.wantSuccess && !result.Success {
				t.Errorf("expected success, got failure: %s", result.Error)
			}
			if tt.wantCostExceeded && !result.CostLimitExceeded {
				t.Error("expected CostLimitExceeded to be true")
			}
			if tt.wantLimitDetails && result.LimitDetails == nil {
				t.Error("expected LimitDetails to be populated")
			}
			if tt.wantLimitType != "" && result.LimitDetails != nil && result.LimitDetails.LimitType != tt.wantLimitType {
				t.Errorf("expected limit type %q, got %q", tt.wantLimitType, result.LimitDetails.LimitType)
			}
			if tt.minNodeResults > 0 && len(result.NodeResults) < tt.minNodeResults {
				t.Errorf("expected at least %d node results, got %d", tt.minNodeResults, len(result.NodeResults))
			}
		})
	}
}

// timeoutProvider returns DeadlineExceeded to exercise timeout handling without waiting.
type timeoutProvider struct {
	name   string
	models []providers.Model
}

func (p *timeoutProvider) Name() string { return p.name }

func (p *timeoutProvider) Models() []providers.Model { return p.models }

func (p *timeoutProvider) Complete(ctx context.Context, req *providers.CompletionRequest) (*providers.CompletionResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (p *timeoutProvider) EstimateTokens(text string) int { return 0 }

func (p *timeoutProvider) Cost(model string, inputTokens, outputTokens int) float64 { return 0 }

func TestExecutePromptNode_Timeout(t *testing.T) {
	registry := providers.NewRegistry()
	registry.Register(&timeoutProvider{
		name: "timeout",
		models: []providers.Model{
			{
				ID:       "timeout-model",
				Name:     "Timeout Model",
				Provider: "timeout",
			},
		},
	})

	executor := NewExecutor(NewMockLLMClient(registry))
	node := &Node{
		Type:           NodeTypePrompt,
		Model:          "timeout-model",
		Prompt:         "test",
		TimeoutSeconds: 1,
		RetryPolicy: &RetryPolicy{
			MaxAttempts:     1,
			BackoffMs:       1,
			BackoffMultiply: 1.0,
		},
	}

	result, err := executor.executeNode(context.Background(), withStrictNodeDefaults(node), "node_timeout", map[string]interface{}{}, nil, nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if result.Success {
		t.Error("expected node to fail on timeout")
	}
	if result.Error != "node timeout after 1 seconds" {
		t.Errorf("unexpected error message: %q", result.Error)
	}
}

// --- Retry Test Helpers ---

// newFailingExecutor creates an executor with a FailingMockProvider that fails
// failCount times with the given error before succeeding.
func newFailingExecutor(t *testing.T, failCount int, err error) (*Executor, *FailingMockProvider) {
	t.Helper()
	provider := NewFailingMockProvider("test", failCount, err)
	registry := providers.NewRegistry()
	registry.Register(provider)
	return NewExecutor(NewMockLLMClient(registry)), provider
}

// newAlwaysFailExecutor creates an executor with an AlwaysFailProvider that
// always returns the given error.
func newAlwaysFailExecutor(t *testing.T, err error) (*Executor, *AlwaysFailProvider) {
	t.Helper()
	provider := NewAlwaysFailProvider("test", err)
	registry := providers.NewRegistry()
	registry.Register(provider)
	return NewExecutor(NewMockLLMClient(registry)), provider
}

// newRetryEventCollector returns an ExecutionContext that collects RetryEvents
// and a function to retrieve the collected events (thread-safe).
func newRetryEventCollector() (*ExecutionContext, func() []RetryEvent) {
	var events []RetryEvent
	var mu sync.Mutex
	execCtx := &ExecutionContext{
		JobID: "test-job",
		RetryCallback: func(event RetryEvent) {
			mu.Lock()
			events = append(events, event)
			mu.Unlock()
		},
	}
	getEvents := func() []RetryEvent {
		mu.Lock()
		defer mu.Unlock()
		cp := make([]RetryEvent, len(events))
		copy(cp, events)
		return cp
	}
	return execCtx, getEvents
}

// --- Retry Tests ---

// TestExecuteNode_RetryOnTransientError verifies that a transient error triggers retry
// and the node eventually succeeds on the second attempt.
func TestExecuteNode_RetryOnTransientError(t *testing.T) {
	executor, failProvider := newFailingExecutor(t, 1, fmt.Errorf("TIMEOUT: request timed out"))
	execCtx, getEvents := newRetryEventCollector()

	node := &Node{
		Type:   NodeTypePrompt,
		Model:  "mock-model",
		Prompt: "test prompt",
		RetryPolicy: &RetryPolicy{
			MaxAttempts:     3,
			BackoffMs:       1,
			BackoffMultiply: 1.0,
			RetryableErrors: []string{"TIMEOUT"},
		},
	}

	result, err := executor.executeNode(context.Background(), withStrictNodeDefaults(node), "node_1", map[string]interface{}{}, execCtx, nil)
	if err != nil {
		t.Fatalf("expected success after retry, got error: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got failure: %s", result.Error)
	}
	if result.AttemptNumber != 2 {
		t.Errorf("AttemptNumber = %d, want 2", result.AttemptNumber)
	}
	if failProvider.CallCount() != 2 {
		t.Errorf("provider called %d times, want 2", failProvider.CallCount())
	}

	events := getEvents()
	if len(events) < 2 {
		t.Fatalf("expected at least 2 retry events, got %d", len(events))
	}
	if events[0].Type != "retry_backoff" {
		t.Errorf("first event type = %q, want retry_backoff", events[0].Type)
	}
	if events[1].Type != "retry_start" {
		t.Errorf("second event type = %q, want retry_start", events[1].Type)
	}
}

// TestExecuteNode_NoRetryOnCostLimit verifies that CostLimitError stops retries immediately.
func TestExecuteNode_NoRetryOnCostLimit(t *testing.T) {
	executor := NewTestExecutorWithCost(0.001, 0.002)

	node := &Node{
		ID:     "node1",
		Type:   NodeTypePrompt,
		Model:  "mock-model",
		Prompt: "test",
		RetryPolicy: &RetryPolicy{
			MaxAttempts:     3,
			BackoffMs:       1,
			RetryableErrors: []string{"COST_LIMIT"},
		},
	}

	costTracker := NewCostTracker(&CostLimits{MaxCostUSD: 0.0001})

	_, err := executor.executeNode(context.Background(), withStrictNodeDefaults(node), "node1", map[string]interface{}{}, nil, costTracker)
	if err == nil {
		t.Fatal("expected cost limit error")
	}
	if !IsCostLimitError(err) {
		t.Errorf("expected CostLimitError, got %T: %v", err, err)
	}
}

// TestExecuteNode_NoRetryOnParentCancel verifies that a cancelled parent context stops retries.
func TestExecuteNode_NoRetryOnParentCancel(t *testing.T) {
	executor, failProvider := newAlwaysFailExecutor(t, fmt.Errorf("TIMEOUT: always fails"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	node := &Node{
		Type:   NodeTypePrompt,
		Model:  "mock-model",
		Prompt: "test",
		RetryPolicy: &RetryPolicy{
			MaxAttempts:     5,
			BackoffMs:       1,
			RetryableErrors: []string{"TIMEOUT"},
		},
	}

	_, err := executor.executeNode(ctx, withStrictNodeDefaults(node), "node_cancel", map[string]interface{}{}, nil, nil)
	if err == nil {
		t.Fatal("expected error with cancelled context")
	}

	calls := failProvider.CallCount()
	if calls > 1 {
		t.Errorf("provider called %d times, want at most 1 (should stop on cancel)", calls)
	}
}

// TestExecuteNode_RetryOnNodeTimeout verifies that a node's own timeout (not parent) IS retried.
func TestExecuteNode_RetryOnNodeTimeout(t *testing.T) {
	executor, _ := newFailingExecutor(t, 1, context.DeadlineExceeded)

	node := &Node{
		Type:   NodeTypePrompt,
		Model:  "mock-model",
		Prompt: "test",
		RetryPolicy: &RetryPolicy{
			MaxAttempts:     3,
			BackoffMs:       1,
			BackoffMultiply: 1.0,
			RetryableErrors: []string{}, // Empty = retry all errors; isolates ctx.Err() check
		},
	}

	result, err := executor.executeNode(context.Background(), withStrictNodeDefaults(node), "node_timeout", map[string]interface{}{}, nil, nil)
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success, got failure: %s", result.Error)
	}
	if result.AttemptNumber != 2 {
		t.Errorf("AttemptNumber = %d, want 2", result.AttemptNumber)
	}
}

// TestExecuteNode_MaxAttemptsExhausted verifies that all N attempts fail, returns final error,
// and emits a retry_exhausted event.
func TestExecuteNode_MaxAttemptsExhausted(t *testing.T) {
	executor, failProvider := newAlwaysFailExecutor(t, fmt.Errorf("TIMEOUT: persistent failure"))
	execCtx, getEvents := newRetryEventCollector()

	maxAttempts := 3
	node := &Node{
		Type:   NodeTypePrompt,
		Model:  "mock-model",
		Prompt: "test",
		RetryPolicy: &RetryPolicy{
			MaxAttempts:     maxAttempts,
			BackoffMs:       1,
			BackoffMultiply: 1.0,
			RetryableErrors: []string{"TIMEOUT"},
		},
	}

	_, err := executor.executeNode(context.Background(), withStrictNodeDefaults(node), "node_exhaust", map[string]interface{}{}, execCtx, nil)
	if err == nil {
		t.Fatal("expected error after exhausting all attempts")
	}

	if failProvider.CallCount() != maxAttempts {
		t.Errorf("provider called %d times, want %d", failProvider.CallCount(), maxAttempts)
	}

	events := getEvents()
	hasExhausted := false
	for _, ev := range events {
		if ev.Type == "retry_exhausted" {
			hasExhausted = true
			if ev.Attempt != maxAttempts {
				t.Errorf("retry_exhausted attempt = %d, want %d", ev.Attempt, maxAttempts)
			}
		}
	}
	if !hasExhausted {
		t.Error("expected retry_exhausted event")
	}
}

// TestExecuteNode_CancelDuringBackoff verifies that cancelling the parent context during
// backoff sleep wakes the retry loop promptly.
func TestExecuteNode_CancelDuringBackoff(t *testing.T) {
	executor, failProvider := newAlwaysFailExecutor(t, fmt.Errorf("TIMEOUT: persistent"))

	ctx, cancel := context.WithCancel(context.Background())

	node := &Node{
		Type:   NodeTypePrompt,
		Model:  "mock-model",
		Prompt: "test",
		RetryPolicy: &RetryPolicy{
			MaxAttempts:     5,
			BackoffMs:       5000, // 5 second backoff -- long enough that we'd notice if not cancelled
			BackoffMultiply: 1.0,
			RetryableErrors: []string{"TIMEOUT"},
		},
	}

	// Cancel after a short delay (during backoff sleep)
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, _ = executor.executeNode(ctx, withStrictNodeDefaults(node), "node_backoff_cancel", map[string]interface{}{}, nil, nil)
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Errorf("took %v to exit, expected prompt exit on cancel (< 2s)", elapsed)
	}

	if failProvider.CallCount() > 2 {
		t.Errorf("provider called %d times, expected at most 2 before cancel", failProvider.CallCount())
	}
}

// TestExecuteNode_NoRetryForResultNodes verifies that non-prompt nodes use NoRetryPolicy.
func TestExecuteNode_NoRetryForResultNodes(t *testing.T) {
	executor := NewTestExecutor()

	node := &Node{
		Type: NodeTypeResult,
	}

	result, err := executor.executeNode(context.Background(), withStrictNodeDefaults(node), "result_node", map[string]interface{}{}, nil, nil)
	if err != nil {
		t.Fatalf("result node should succeed: %v", err)
	}
	if !result.Success {
		t.Error("expected success for result node")
	}
	if result.AttemptNumber != 1 {
		t.Errorf("AttemptNumber = %d, want 1", result.AttemptNumber)
	}
}

func TestExecuteNode_DefaultRetryForChildWorkflowNodes(t *testing.T) {
	executor := NewTestExecutor()
	attempts := 0
	execCtx := &ExecutionContext{
		JobID: "job-child-retry",
		ExecuteChildWorkflow: func(ctx context.Context, req *ChildWorkflowRequest) (*ChildWorkflowResult, error) {
			attempts++
			if attempts == 1 {
				return &ChildWorkflowResult{
					WorkflowID: "child-wf",
					Success:    false,
					Error:      "rate limit exceeded: rate limit wait (405ms) exceeds context deadline (120ms remaining)",
				}, nil
			}
			return &ChildWorkflowResult{
				WorkflowID:  "child-wf",
				Success:     true,
				FinalOutput: "A",
			}, nil
		},
	}

	node := &Node{
		Type:            NodeTypeChildWorkflow,
		ChildWorkflowID: "child-wf",
	}

	result, err := executor.executeNode(context.Background(), withStrictNodeDefaults(node), "child-node", map[string]interface{}{
		"user_prompt": "Q",
	}, execCtx, nil)
	if err != nil {
		t.Fatalf("expected child workflow to succeed after retry, got: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success after retry, got node error: %s", result.Error)
	}
	if result.AttemptNumber != 2 {
		t.Fatalf("expected attempt number 2, got %d", result.AttemptNumber)
	}
}

func TestExecuteNode_RetriesChildWorkflowOnEmptyOutputWithConsumedTokens(t *testing.T) {
	executor := NewTestExecutor()
	attempts := 0
	execCtx := &ExecutionContext{
		JobID: "job-child-empty-output-retry",
		ExecuteChildWorkflow: func(ctx context.Context, req *ChildWorkflowRequest) (*ChildWorkflowResult, error) {
			attempts++
			if attempts == 1 {
				return &ChildWorkflowResult{
					WorkflowID:        "child-wf",
					Success:           true,
					FinalOutput:       "  ",
					TotalOutputTokens: 8,
				}, nil
			}
			return &ChildWorkflowResult{
				WorkflowID:        "child-wf",
				Success:           true,
				FinalOutput:       "A",
				TotalOutputTokens: 2,
			}, nil
		},
	}

	node := &Node{
		Type:            NodeTypeChildWorkflow,
		ChildWorkflowID: "child-wf",
	}

	result, err := executor.executeNode(context.Background(), withStrictNodeDefaults(node), "child-node", map[string]interface{}{
		"user_prompt": "Q",
	}, execCtx, nil)
	if err != nil {
		t.Fatalf("expected child workflow to succeed after retry, got: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success after retry, got node error: %s", result.Error)
	}
	if result.AttemptNumber != 2 {
		t.Fatalf("expected attempt number 2, got %d", result.AttemptNumber)
	}
}

// --- Regression Tests ---

// TestDeepCopyContext verifies that deepCopyContext produces independent copies
// so that parallel nodes sharing a context snapshot cannot cause data races.
func TestDeepCopyContext(t *testing.T) {
	t.Run("nested maps are independent", func(t *testing.T) {
		original := map[string]interface{}{
			"top": "value",
			"nested": map[string]interface{}{
				"inner_key": "inner_value",
			},
		}

		copied := DeepCopyContext(original)

		// Modify the copy's nested map
		nested := copied["nested"].(map[string]interface{})
		nested["inner_key"] = "MODIFIED"
		nested["new_key"] = "added"

		// Original must be unaffected
		origNested := original["nested"].(map[string]interface{})
		if origNested["inner_key"] != "inner_value" {
			t.Errorf("original nested map was mutated: inner_key = %v, want inner_value", origNested["inner_key"])
		}
		if _, exists := origNested["new_key"]; exists {
			t.Error("original nested map gained unexpected key 'new_key'")
		}
	})

	t.Run("slices are independent", func(t *testing.T) {
		original := map[string]interface{}{
			"items": []interface{}{"a", "b", "c"},
		}

		copied := DeepCopyContext(original)

		// Modify the copy's slice
		copiedSlice := copied["items"].([]interface{})
		copiedSlice[0] = "MODIFIED"

		// Original must be unaffected
		origSlice := original["items"].([]interface{})
		if origSlice[0] != "a" {
			t.Errorf("original slice was mutated: [0] = %v, want a", origSlice[0])
		}
	})

	t.Run("nil input returns non-nil empty map", func(t *testing.T) {
		result := DeepCopyContext(nil)
		if result == nil {
			t.Fatal("DeepCopyContext(nil) returned nil, want non-nil empty map")
		}
		if len(result) != 0 {
			t.Errorf("DeepCopyContext(nil) returned map with %d entries, want 0", len(result))
		}
	})

	t.Run("scalar values are preserved", func(t *testing.T) {
		original := map[string]interface{}{
			"str":   "hello",
			"num":   42,
			"float": 3.14,
			"bool":  true,
		}

		copied := DeepCopyContext(original)

		if copied["str"] != "hello" {
			t.Errorf("str = %v, want hello", copied["str"])
		}
		if copied["num"] != 42 {
			t.Errorf("num = %v, want 42", copied["num"])
		}
		if copied["float"] != 3.14 {
			t.Errorf("float = %v, want 3.14", copied["float"])
		}
		if copied["bool"] != true {
			t.Errorf("bool = %v, want true", copied["bool"])
		}
	})
}

// TestCostTracker_AddAndCheck verifies the atomic AddAndCheck method that combines
// cost accumulation and limit checking in a single lock acquisition.
func TestCostTracker_AddAndCheck(t *testing.T) {
	t.Run("under limit returns nil", func(t *testing.T) {
		tracker := NewCostTracker(&CostLimits{MaxCostUSD: 1.0})

		err := tracker.AddAndCheck(100, 50, 0.10, "node_1")
		if err != nil {
			t.Fatalf("expected nil error under limit, got: %v", err)
		}
	})

	t.Run("over limit returns CostLimitError", func(t *testing.T) {
		tracker := NewCostTracker(&CostLimits{MaxCostUSD: 0.05})

		// First call is under the limit
		err := tracker.AddAndCheck(100, 50, 0.03, "node_1")
		if err != nil {
			t.Fatalf("first AddAndCheck should succeed: %v", err)
		}

		// Second call pushes over the limit (0.03 + 0.05 = 0.08 > 0.05)
		err = tracker.AddAndCheck(100, 50, 0.05, "node_2")
		if err == nil {
			t.Fatal("expected CostLimitError, got nil")
		}
		if !IsCostLimitError(err) {
			t.Errorf("expected CostLimitError, got %T: %v", err, err)
		}
	})

	t.Run("totals are accumulated correctly", func(t *testing.T) {
		tracker := NewCostTracker(&CostLimits{MaxCostUSD: 10.0})

		_ = tracker.AddAndCheck(100, 50, 0.001, "node_1")
		_ = tracker.AddAndCheck(200, 75, 0.002, "node_2")
		_ = tracker.AddAndCheck(300, 125, 0.003, "node_3")

		cost, tokens, inputTokens, outputTokens := tracker.GetTotals()
		if cost != 0.006 {
			t.Errorf("TotalCost = %f, want 0.006", cost)
		}
		if tokens != 850 {
			t.Errorf("TotalTokens = %d, want 850", tokens)
		}
		if inputTokens != 600 {
			t.Errorf("TotalInputTokens = %d, want 600", inputTokens)
		}
		if outputTokens != 250 {
			t.Errorf("TotalOutputTokens = %d, want 250", outputTokens)
		}
	})

	t.Run("nil limits never returns error", func(t *testing.T) {
		tracker := NewCostTracker(nil)

		err := tracker.AddAndCheck(999999, 999999, 99999.0, "node_huge")
		if err != nil {
			t.Fatalf("nil limits should never error, got: %v", err)
		}
	})

	t.Run("token limit triggers error", func(t *testing.T) {
		tracker := NewCostTracker(&CostLimits{MaxTokens: 200})

		err := tracker.AddAndCheck(100, 50, 0.001, "node_1")
		if err != nil {
			t.Fatalf("first call should succeed: %v", err)
		}

		// 100+50 + 100+50 = 300 > 200
		err = tracker.AddAndCheck(100, 50, 0.001, "node_2")
		if err == nil {
			t.Fatal("expected token limit error")
		}
		if !IsCostLimitError(err) {
			t.Errorf("expected CostLimitError, got %T", err)
		}
	})
}

// TestParallelErrorAggregation verifies that when multiple parallel nodes fail,
// all errors are collected in the workflow result.
func TestParallelErrorAggregation(t *testing.T) {
	// Create an executor with a provider that always fails
	provider := NewAlwaysFailProvider("test", fmt.Errorf("PERMANENT: model unavailable"))
	registry := providers.NewRegistry()
	registry.Register(provider)
	executor := NewExecutor(NewMockLLMClient(registry))

	// Build a workflow with a source node (result type, succeeds immediately) feeding
	// two parallel prompt nodes via edges. This ensures node_a and node_b land in the
	// same execution level and run concurrently.
	wf := &Workflow{
		ID:   "parallel-fail-test",
		Name: "Parallel Error Test",
		Nodes: []*Node{
			{
				ID:   "source",
				Type: NodeTypeResult,
			},
			{
				ID:     "node_a",
				Type:   NodeTypePrompt,
				Model:  "mock-model",
				Prompt: "prompt A",
				RetryPolicy: &RetryPolicy{
					MaxAttempts: 1,
				},
			},
			{
				ID:     "node_b",
				Type:   NodeTypePrompt,
				Model:  "mock-model",
				Prompt: "prompt B",
				RetryPolicy: &RetryPolicy{
					MaxAttempts: 1,
				},
			},
		},
		Edges: []*Edge{
			{ID: "e1", Source: "source", Target: "node_a"},
			{ID: "e2", Source: "source", Target: "node_b"},
		},
	}

	result, err := executor.Execute(context.Background(), withStrictWorkflowDefaults(wf), nil)

	// Expect an error
	if err == nil {
		t.Fatal("expected error from parallel failures, got nil")
	}

	// The result should not be successful
	if result.Success {
		t.Error("expected workflow to fail")
	}

	// The result error should mention both node failures
	if result.Error == "" {
		t.Fatal("expected non-empty error message on result")
	}

	// The combined error should include references to both nodes
	if !containsSubstring(result.Error, "node_a") {
		t.Errorf("result.Error should mention node_a, got: %s", result.Error)
	}
	if !containsSubstring(result.Error, "node_b") {
		t.Errorf("result.Error should mention node_b, got: %s", result.Error)
	}

	// All three node results should be present (source + 2 failing prompt nodes)
	if len(result.NodeResults) < 3 {
		t.Errorf("expected at least 3 node results (source + 2 parallel), got %d", len(result.NodeResults))
	}
}

// containsSubstring checks whether s contains substr (helper for TestParallelErrorAggregation).
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}
