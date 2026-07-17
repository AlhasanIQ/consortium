package providers

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/trace"
)

type mockCostTracker struct {
	input  int
	output int
	cost   float64
}

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

func (m *mockCostTracker) Add(inputTokens, outputTokens int, cost float64) {
	m.input += inputTokens
	m.output += outputTokens
	m.cost += cost
}

// mockNodeLogger records calls for test verification.
type mockNodeLogger struct {
	nodes []mockLoggedNode
}

type mockLoggedNode struct {
	JobID     string
	NodeID    string
	Model     string
	Status    string
	ErrorMsg  string
	Response  string
	TokensIn  int
	TokensOut int
}

func (m *mockNodeLogger) LogLLMRequestFull(l *storage.LLMRequestLog) error {
	// Track latest state per nodeID (upsert semantics like real storage).
	for i, n := range m.nodes {
		if n.JobID == l.JobID && n.NodeID == l.NodeID {
			m.nodes[i] = mockLoggedNode{
				JobID: l.JobID, NodeID: l.NodeID, Model: l.Model,
				Status: l.Status, ErrorMsg: l.ErrMsg, Response: l.Response,
				TokensIn: l.TokensIn, TokensOut: l.TokensOut,
			}
			return nil
		}
	}
	m.nodes = append(m.nodes, mockLoggedNode{
		JobID: l.JobID, NodeID: l.NodeID, Model: l.Model,
		Status: l.Status, ErrorMsg: l.ErrMsg, Response: l.Response,
		TokensIn: l.TokensIn, TokensOut: l.TokensOut,
	})
	return nil
}

func (m *mockNodeLogger) AddSubNode(_ SubNodeRecord) error {
	return nil
}

func (m *mockNodeLogger) findNode(jobID, nodeID string) *mockLoggedNode {
	for _, n := range m.nodes {
		if n.JobID == jobID && n.NodeID == nodeID {
			return &n
		}
	}
	return nil
}

func setupTestClient(t *testing.T) (*Client, *mockNodeLogger) {
	logger := &mockNodeLogger{}

	registry := NewRegistry()
	mock := &mockProvider{
		name: "test-provider",
		models: []Model{
			{ID: "test-model", Provider: "test-provider", InputCost: 0.001, OutputCost: 0.002},
		},
		response: &CompletionResponse{
			ID:      "test-123",
			Model:   "test-model",
			Content: "Hello from test",
			Usage: Usage{
				PromptTokens:     10,
				CompletionTokens: 20,
				TotalTokens:      30,
			},
			Finish: "stop",
		},
	}
	registry.Register(mock)

	client := NewClient(registry, logger)
	return client, logger
}

func TestClient(t *testing.T) {
	t.Run("complete with accounting", func(t *testing.T) {
		client, _ := setupTestClient(t)

		req := &ClientRequest{
			Model:    "test-model",
			Messages: []Message{{Role: "user", Content: "Hello"}},
		}

		ctx := &CompletionContext{JobID: "job-123", NodeID: "node-1"}
		resp, err := client.Complete(context.Background(), req, ctx)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Content != "Hello from test" {
			t.Errorf("expected 'Hello from test', got %s", resp.Content)
		}
		if resp.Usage.TotalTokens != 30 {
			t.Errorf("expected 30 tokens, got %d", resp.Usage.TotalTokens)
		}
	})

	t.Run("complete without job context", func(t *testing.T) {
		client, _ := setupTestClient(t)

		req := &ClientRequest{
			Model:    "test-model",
			Messages: []Message{{Role: "user", Content: "Hello"}},
		}

		// No context provided - should still work
		resp, err := client.Complete(context.Background(), req, nil)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Content != "Hello from test" {
			t.Errorf("expected 'Hello from test', got %s", resp.Content)
		}
	})

	t.Run("complete updates shared cost tracker", func(t *testing.T) {
		client, _ := setupTestClient(t)
		tracker := &mockCostTracker{}

		req := &ClientRequest{
			Model:    "test-model",
			Messages: []Message{{Role: "user", Content: "Hello"}},
		}

		resp, err := client.Complete(context.Background(), req, &CompletionContext{CostTracker: tracker})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected response")
		}
		if tracker.input != 10 || tracker.output != 20 {
			t.Fatalf("unexpected tracked tokens in=%d out=%d", tracker.input, tracker.output)
		}
		// cost = 10*0.001 + 20*0.002
		if math.Abs(tracker.cost-0.05) > 1e-12 {
			t.Fatalf("unexpected tracked cost: %f", tracker.cost)
		}
	})

	t.Run("complete uses provider-reported cost", func(t *testing.T) {
		providerCost := 0.1234
		registry := NewRegistry()
		registry.Register(&mockProvider{
			name:   "reported-cost-provider",
			models: []Model{{ID: "reported-cost-model", Provider: "reported-cost-provider", InputCost: 0.001, OutputCost: 0.002}},
			response: &CompletionResponse{
				Content: "priced response",
				Usage: Usage{
					PromptTokens:     10,
					CompletionTokens: 20,
					TotalTokens:      30,
					Cost:             &providerCost,
				},
			},
		})
		tracker := &mockCostTracker{}
		client := NewClient(registry, nil)

		_, err := client.Complete(context.Background(), &ClientRequest{Model: "reported-cost-model"}, &CompletionContext{CostTracker: tracker})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tracker.input != 10 || tracker.output != 20 || tracker.cost != providerCost {
			t.Fatalf("tracker = in:%d out:%d cost:%v, want provider-reported usage", tracker.input, tracker.output, tracker.cost)
		}
	})

	t.Run("complete does not account failed provider calls", func(t *testing.T) {
		registry := NewRegistry()
		registry.Register(&mockProvider{
			name:   "failed-provider",
			models: []Model{{ID: "failed-model", Provider: "failed-provider", InputCost: 0.001, OutputCost: 0.002}},
			err:    errors.New("provider unavailable"),
			response: &CompletionResponse{Usage: Usage{
				PromptTokens:     10,
				CompletionTokens: 20,
				TotalTokens:      30,
			}},
		})
		tracker := &mockCostTracker{}
		client := NewClient(registry, nil)

		_, err := client.Complete(context.Background(), &ClientRequest{Model: "failed-model"}, &CompletionContext{CostTracker: tracker})
		if err == nil {
			t.Fatal("expected provider error")
		}
		if tracker.input != 0 || tracker.output != 0 || tracker.cost != 0 {
			t.Fatalf("failed call changed tracker = in:%d out:%d cost:%v", tracker.input, tracker.output, tracker.cost)
		}
	})

	t.Run("get model", func(t *testing.T) {
		client, _ := setupTestClient(t)

		model, err := client.GetModel("test-model")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if model.ID != "test-model" {
			t.Errorf("expected test-model, got %s", model.ID)
		}
	})

	t.Run("get all models", func(t *testing.T) {
		client, _ := setupTestClient(t)

		models := client.GetModels()
		if len(models) != 1 {
			t.Fatalf("expected 1 model, got %d", len(models))
		}
		if models[0].ID != "test-model" {
			t.Errorf("expected test-model, got %s", models[0].ID)
		}
	})

	t.Run("complete with node logging", func(t *testing.T) {
		client, logger := setupTestClient(t)

		req := &ClientRequest{
			Model:    "test-model",
			Messages: []Message{{Role: "user", Content: "Hello"}},
		}

		// Context with both JobID and NodeID
		ctx := &CompletionContext{
			JobID:  "job-456",
			NodeID: "node-1",
		}
		resp, err := client.Complete(context.Background(), req, ctx)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Content != "Hello from test" {
			t.Errorf("expected 'Hello from test', got %s", resp.Content)
		}

		// Verify node was logged
		node := logger.findNode("job-456", "node-1")
		if node == nil {
			t.Fatal("expected node to be logged")
		}
		if node.Status != "completed" {
			t.Errorf("expected status 'completed', got %s", node.Status)
		}
	})

	t.Run("complete with provider error", func(t *testing.T) {
		logger := &mockNodeLogger{}
		registry := NewRegistry()
		mock := &mockProvider{
			name: "test-provider",
			models: []Model{
				{ID: "test-model", Provider: "test-provider", InputCost: 0.001, OutputCost: 0.002},
			},
			err: context.DeadlineExceeded, // Simulate timeout error
		}
		registry.Register(mock)

		client := NewClient(registry, logger)

		req := &ClientRequest{
			Model:    "test-model",
			Messages: []Message{{Role: "user", Content: "Hello"}},
		}

		ctx := &CompletionContext{
			JobID:  "job-error",
			NodeID: "node-error",
		}
		_, err := client.Complete(context.Background(), req, ctx)

		if err == nil {
			t.Fatal("expected error, got nil")
		}

		// Verify node was logged with error status
		node := logger.findNode("job-error", "node-error")
		if node == nil {
			t.Fatal("expected node to be logged")
		}
		if node.Status != "failed" {
			t.Errorf("expected status 'failed', got %s", node.Status)
		}
	})

	t.Run("writes trace span even when request context is cancelled", func(t *testing.T) {
		logger := &mockNodeLogger{}
		registry := NewRegistry()
		mock := &mockProvider{
			name: "test-provider",
			models: []Model{
				{ID: "test-model", Provider: "test-provider", InputCost: 0.001, OutputCost: 0.002},
			},
			err: context.DeadlineExceeded,
		}
		registry.Register(mock)

		client := NewClient(registry, logger)
		traceWriter := &mockTraceWriter{}
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Simulate timeout/cancelled node context.

		req := &ClientRequest{
			Model:    "test-model",
			Messages: []Message{{Role: "user", Content: "Hello"}},
		}
		compCtx := &CompletionContext{
			JobID:       "job-trace-timeout",
			NodeID:      "node-trace-timeout",
			TraceWriter: traceWriter,
		}

		_, err := client.Complete(ctx, req, compCtx)
		if err == nil {
			t.Fatal("expected provider error, got nil")
		}
		if len(traceWriter.spans) != 1 {
			t.Fatalf("expected 1 written span, got %d", len(traceWriter.spans))
		}
		if traceWriter.spans[0].Status != trace.StatusError {
			t.Fatalf("expected trace status error, got %q", traceWriter.spans[0].Status)
		}
	})

	t.Run("complete without logger", func(t *testing.T) {
		registry := NewRegistry()
		mock := &mockProvider{
			name: "test-provider",
			models: []Model{
				{ID: "test-model", Provider: "test-provider", InputCost: 0.001, OutputCost: 0.002},
			},
			response: &CompletionResponse{
				ID:      "test-123",
				Model:   "test-model",
				Content: "Hello from test",
				Usage: Usage{
					PromptTokens:     10,
					CompletionTokens: 20,
					TotalTokens:      30,
				},
				Finish: "stop",
			},
		}
		registry.Register(mock)

		// Create client without logger
		client := NewClient(registry, nil)

		req := &ClientRequest{
			Model:    "test-model",
			Messages: []Message{{Role: "user", Content: "Hello"}},
		}

		ctx := &CompletionContext{
			JobID:  "job-123",
			NodeID: "node-1",
		}
		resp, err := client.Complete(context.Background(), req, ctx)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Content != "Hello from test" {
			t.Errorf("expected 'Hello from test', got %s", resp.Content)
		}
	})
}
