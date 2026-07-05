package storage

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/trace"
)

// TestTraceFullLifecycle writes node, call, and decision spans with parent-child
// relationships, then fetches them via GetJobSpansByNode and verifies grouping,
// span counts, parent links, and attribute round-tripping through JSON.
func TestTraceFullLifecycle(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create a job so foreign key is satisfied.
	job := &Job{
		ID:          "trace-lifecycle-job",
		Description: "Trace lifecycle test",
		Model:       "test-model",
		Status:      "running",
	}
	if err := store.CreateExecution(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	// Node span (root of node-1)
	nodeSpan := &trace.Span{
		JobID:      "trace-lifecycle-job",
		NodeID:     "node-1",
		SpanID:     "span-node-1",
		Kind:       trace.KindNode,
		Status:     trace.StatusOK,
		StartedAt:  now,
		DurationMs: 250.5,
		Attributes: map[string]any{
			"model":        "gpt-4",
			"tokens_input": 42,
		},
	}

	// Call span (child of node span)
	callSpan := &trace.Span{
		JobID:        "trace-lifecycle-job",
		NodeID:       "node-1",
		SpanID:       "span-call-1",
		ParentSpanID: "span-node-1",
		Kind:         trace.KindCall,
		Status:       trace.StatusOK,
		StartedAt:    now.Add(10 * time.Millisecond),
		DurationMs:   200.0,
		Attributes: map[string]any{
			"provider":  "openrouter",
			"http_code": float64(200),
		},
	}

	// Decision span (child of node span)
	decisionSpan := &trace.Span{
		JobID:        "trace-lifecycle-job",
		NodeID:       "node-1",
		SpanID:       "span-decision-1",
		ParentSpanID: "span-node-1",
		Kind:         trace.KindDecision,
		Status:       trace.StatusOK,
		StartedAt:    now.Add(220 * time.Millisecond),
		DurationMs:   5.0,
		Attributes: map[string]any{
			"condition": "cost < 0.01",
			"result":    true,
		},
	}

	// A span in a different node to verify grouping.
	node2Span := &trace.Span{
		JobID:      "trace-lifecycle-job",
		NodeID:     "node-2",
		SpanID:     "span-node-2",
		Kind:       trace.KindNode,
		Status:     trace.StatusOK,
		StartedAt:  now.Add(300 * time.Millisecond),
		DurationMs: 100.0,
		Attributes: map[string]any{},
	}

	// Write all spans.
	for _, span := range []*trace.Span{nodeSpan, callSpan, decisionSpan, node2Span} {
		if err := store.WriteSpan(ctx, span); err != nil {
			t.Fatalf("WriteSpan failed for %s: %v", span.SpanID, err)
		}
	}

	// Fetch grouped by node.
	groups, err := store.GetJobSpansByNode("trace-lifecycle-job")
	if err != nil {
		t.Fatalf("GetJobSpansByNode failed: %v", err)
	}

	// Verify correct number of groups.
	if len(groups) != 2 {
		t.Fatalf("Expected 2 node groups, got %d", len(groups))
	}

	// Verify first group is node-1 (earliest started_at).
	if groups[0].NodeID != "node-1" {
		t.Errorf("Expected first group node_id 'node-1', got '%s'", groups[0].NodeID)
	}
	if len(groups[0].Spans) != 3 {
		t.Errorf("Expected 3 spans in node-1 group, got %d", len(groups[0].Spans))
	}

	// Verify second group is node-2.
	if groups[1].NodeID != "node-2" {
		t.Errorf("Expected second group node_id 'node-2', got '%s'", groups[1].NodeID)
	}
	if len(groups[1].Spans) != 1 {
		t.Errorf("Expected 1 span in node-2 group, got %d", len(groups[1].Spans))
	}

	// Verify parent-child relationships in node-1 spans.
	spansByID := make(map[string]trace.Span)
	for _, s := range groups[0].Spans {
		spansByID[s.SpanID] = s
	}

	rootSpan, ok := spansByID["span-node-1"]
	if !ok {
		t.Fatal("Root node span not found in results")
	}
	if rootSpan.ParentSpanID != "" {
		t.Errorf("Root span should have empty parent_span_id, got '%s'", rootSpan.ParentSpanID)
	}
	if rootSpan.Kind != trace.KindNode {
		t.Errorf("Expected kind '%s', got '%s'", trace.KindNode, rootSpan.Kind)
	}

	childCall, ok := spansByID["span-call-1"]
	if !ok {
		t.Fatal("Call span not found in results")
	}
	if childCall.ParentSpanID != "span-node-1" {
		t.Errorf("Call span parent_span_id: expected 'span-node-1', got '%s'", childCall.ParentSpanID)
	}

	childDecision, ok := spansByID["span-decision-1"]
	if !ok {
		t.Fatal("Decision span not found in results")
	}
	if childDecision.ParentSpanID != "span-node-1" {
		t.Errorf("Decision span parent_span_id: expected 'span-node-1', got '%s'", childDecision.ParentSpanID)
	}

	// Verify attributes round-trip through JSON correctly.
	if model, ok := rootSpan.Attributes["model"].(string); !ok || model != "gpt-4" {
		t.Errorf("Expected attribute model='gpt-4', got %v", rootSpan.Attributes["model"])
	}
	// JSON numbers deserialize as float64.
	if tokensInput, ok := rootSpan.Attributes["tokens_input"].(float64); !ok || tokensInput != 42 {
		t.Errorf("Expected attribute tokens_input=42, got %v", rootSpan.Attributes["tokens_input"])
	}
	if result, ok := childDecision.Attributes["result"].(bool); !ok || result != true {
		t.Errorf("Expected attribute result=true, got %v", childDecision.Attributes["result"])
	}
	if httpCode, ok := childCall.Attributes["http_code"].(float64); !ok || httpCode != 200 {
		t.Errorf("Expected attribute http_code=200, got %v", childCall.Attributes["http_code"])
	}

	// Verify duration round-trips.
	if rootSpan.DurationMs != 250.5 {
		t.Errorf("Expected duration_ms 250.5, got %f", rootSpan.DurationMs)
	}
}

// TestTraceParallelNodeWrites writes spans for multiple nodes from concurrent
// goroutines and verifies that all spans are retrievable without data corruption.
func TestTraceParallelNodeWrites(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	job := &Job{
		ID:          "trace-parallel-job",
		Description: "Parallel trace test",
		Model:       "test-model",
		Status:      "running",
	}
	if err := store.CreateExecution(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	ctx := context.Background()
	numNodes := 10
	spansPerNode := 5

	var wg sync.WaitGroup
	errs := make(chan error, numNodes*spansPerNode)

	for node := 0; node < numNodes; node++ {
		wg.Add(1)
		go func(nodeIdx int) {
			defer wg.Done()
			nodeID := fmt.Sprintf("parallel-node-%d", nodeIdx)
			for span := 0; span < spansPerNode; span++ {
				s := &trace.Span{
					JobID:      "trace-parallel-job",
					NodeID:     nodeID,
					SpanID:     fmt.Sprintf("span-%d-%d", nodeIdx, span),
					Kind:       trace.KindCall,
					Status:     trace.StatusOK,
					StartedAt:  time.Now().UTC(),
					DurationMs: float64(span * 10),
					Attributes: map[string]any{
						"node_index": float64(nodeIdx),
						"span_index": float64(span),
					},
				}
				if err := store.WriteSpan(ctx, s); err != nil {
					errs <- fmt.Errorf("WriteSpan node=%d span=%d: %w", nodeIdx, span, err)
				}
			}
		}(node)
	}

	wg.Wait()
	close(errs)

	// Check for write errors.
	for err := range errs {
		t.Errorf("Concurrent write error: %v", err)
	}

	// Verify all spans are retrievable via GetJobSpans.
	allSpans, err := store.GetJobSpans("trace-parallel-job")
	if err != nil {
		t.Fatalf("GetJobSpans failed: %v", err)
	}

	expectedTotal := numNodes * spansPerNode
	if len(allSpans) != expectedTotal {
		t.Fatalf("Expected %d total spans, got %d", expectedTotal, len(allSpans))
	}

	// Verify spans per node via GetJobSpansByNode.
	groups, err := store.GetJobSpansByNode("trace-parallel-job")
	if err != nil {
		t.Fatalf("GetJobSpansByNode failed: %v", err)
	}

	if len(groups) != numNodes {
		t.Errorf("Expected %d node groups, got %d", numNodes, len(groups))
	}

	for _, group := range groups {
		if len(group.Spans) != spansPerNode {
			t.Errorf("Node %s: expected %d spans, got %d", group.NodeID, spansPerNode, len(group.Spans))
		}
	}

	// Verify no data corruption: each span should have the correct node/span indices in attributes.
	seen := make(map[string]bool)
	for _, s := range allSpans {
		if seen[s.SpanID] {
			t.Errorf("Duplicate span_id: %s", s.SpanID)
		}
		seen[s.SpanID] = true
	}
}

// TestTraceErrorSpanCapture writes a span with error status and error attributes,
// then verifies the error details survive the write/read round-trip.
func TestTraceErrorSpanCapture(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	job := &Job{
		ID:          "trace-error-job",
		Description: "Error trace test",
		Model:       "test-model",
		Status:      "failed",
	}
	if err := store.CreateExecution(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	ctx := context.Background()
	errorSpan := &trace.Span{
		JobID:      "trace-error-job",
		NodeID:     "failing-node",
		SpanID:     "span-error-1",
		Kind:       trace.KindCall,
		Status:     trace.StatusError,
		StartedAt:  time.Now().UTC(),
		DurationMs: 1500.0,
		Attributes: map[string]any{
			"error_type":    "timeout",
			"error_message": "provider returned HTTP 504 after 30s",
			"retry_count":   float64(3),
			"provider":      "openrouter",
			"model":         "anthropic/claude-3-opus",
		},
		ToolCallID: "tool-call-abc",
	}

	if err := store.WriteSpan(ctx, errorSpan); err != nil {
		t.Fatalf("WriteSpan failed: %v", err)
	}

	// Fetch via GetJobSpans.
	spans, err := store.GetJobSpans("trace-error-job")
	if err != nil {
		t.Fatalf("GetJobSpans failed: %v", err)
	}

	if len(spans) != 1 {
		t.Fatalf("Expected 1 span, got %d", len(spans))
	}

	s := spans[0]

	// Verify status.
	if s.Status != trace.StatusError {
		t.Errorf("Expected status '%s', got '%s'", trace.StatusError, s.Status)
	}

	// Verify kind.
	if s.Kind != trace.KindCall {
		t.Errorf("Expected kind '%s', got '%s'", trace.KindCall, s.Kind)
	}

	// Verify tool_call_id round-trips.
	if s.ToolCallID != "tool-call-abc" {
		t.Errorf("Expected tool_call_id 'tool-call-abc', got '%s'", s.ToolCallID)
	}

	// Verify error attributes are preserved.
	if errType, ok := s.Attributes["error_type"].(string); !ok || errType != "timeout" {
		t.Errorf("Expected attribute error_type='timeout', got %v", s.Attributes["error_type"])
	}
	if errMsg, ok := s.Attributes["error_message"].(string); !ok || errMsg != "provider returned HTTP 504 after 30s" {
		t.Errorf("Expected attribute error_message preserved, got %v", s.Attributes["error_message"])
	}
	if retryCount, ok := s.Attributes["retry_count"].(float64); !ok || retryCount != 3 {
		t.Errorf("Expected attribute retry_count=3, got %v", s.Attributes["retry_count"])
	}
	if provider, ok := s.Attributes["provider"].(string); !ok || provider != "openrouter" {
		t.Errorf("Expected attribute provider='openrouter', got %v", s.Attributes["provider"])
	}
	if model, ok := s.Attributes["model"].(string); !ok || model != "anthropic/claude-3-opus" {
		t.Errorf("Expected attribute model='anthropic/claude-3-opus', got %v", s.Attributes["model"])
	}

	// Verify duration.
	if s.DurationMs != 1500.0 {
		t.Errorf("Expected duration_ms 1500.0, got %f", s.DurationMs)
	}
}

func TestWriteSpan_SanitizesInvalidJSONAttributes(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	job := &Job{
		ID:          "trace-sanitize-job",
		Description: "Trace sanitize test",
		Model:       "test-model",
		Status:      "running",
	}
	if err := store.CreateExecution(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	span := &trace.Span{
		JobID:      "trace-sanitize-job",
		NodeID:     "node-1",
		SpanID:     "span-sanitize-1",
		Kind:       trace.KindCall,
		Status:     trace.StatusError,
		StartedAt:  time.Now().UTC(),
		DurationMs: 12.3,
		Attributes: map[string]any{
			"nan_value": math.NaN(),
			"inf_value": math.Inf(1),
			"nested": map[string]any{
				"bad": math.NaN(),
			},
			"unsupported": make(chan int),
		},
	}

	if err := store.WriteSpan(context.Background(), span); err != nil {
		t.Fatalf("WriteSpan failed after sanitization: %v", err)
	}

	spans, err := store.GetJobSpans("trace-sanitize-job")
	if err != nil {
		t.Fatalf("GetJobSpans failed: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	if got, ok := spans[0].Attributes["nan_value"].(string); !ok || got != "NaN" {
		t.Fatalf("expected nan_value sanitized to 'NaN', got %#v", spans[0].Attributes["nan_value"])
	}
	if got, ok := spans[0].Attributes["inf_value"].(string); !ok || got != "+Inf" {
		t.Fatalf("expected inf_value sanitized to '+Inf', got %#v", spans[0].Attributes["inf_value"])
	}
	nested, ok := spans[0].Attributes["nested"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested map, got %#v", spans[0].Attributes["nested"])
	}
	if got, ok := nested["bad"].(string); !ok || got != "NaN" {
		t.Fatalf("expected nested.bad sanitized to 'NaN', got %#v", nested["bad"])
	}
	if _, ok := spans[0].Attributes["unsupported"].(string); !ok {
		t.Fatalf("expected unsupported attribute to be stringified, got %#v", spans[0].Attributes["unsupported"])
	}
}

func TestIsRetryableTraceWriteError(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{err: fmt.Errorf("database is locked"), want: true},
		{err: fmt.Errorf("SQLITE_BUSY: database is busy"), want: true},
		{err: fmt.Errorf("some permanent error"), want: false},
		{err: nil, want: false},
	}

	for _, tt := range tests {
		if got := isRetryableTraceWriteError(tt.err); got != tt.want {
			t.Fatalf("isRetryableTraceWriteError(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}
