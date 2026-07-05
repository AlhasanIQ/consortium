package trace

import (
	"testing"
	"time"
)

func TestSpanKindConstants(t *testing.T) {
	// Verify kind constants have expected values
	kinds := map[string]string{
		"KindNode":     KindNode,
		"KindCall":     KindCall,
		"KindDecision": KindDecision,
	}
	expected := map[string]string{
		"KindNode":     "node",
		"KindCall":     "call",
		"KindDecision": "decision",
	}
	for name, got := range kinds {
		if got != expected[name] {
			t.Errorf("%s: expected %q, got %q", name, expected[name], got)
		}
	}
}

func TestSpanStatusConstants(t *testing.T) {
	statuses := map[string]string{
		"StatusOK":        StatusOK,
		"StatusError":     StatusError,
		"StatusTimeout":   StatusTimeout,
		"StatusCancelled": StatusCancelled,
	}
	expected := map[string]string{
		"StatusOK":        "ok",
		"StatusError":     "error",
		"StatusTimeout":   "timeout",
		"StatusCancelled": "cancelled",
	}
	for name, got := range statuses {
		if got != expected[name] {
			t.Errorf("%s: expected %q, got %q", name, expected[name], got)
		}
	}
}

func TestSpanFieldAssignment(t *testing.T) {
	now := time.Now()
	span := Span{
		JobID:        "job-123",
		NodeID:       "node-a",
		SpanID:       "span-1",
		ParentSpanID: "span-0",
		Kind:         KindCall,
		Status:       StatusOK,
		StartedAt:    now,
		DurationMs:   42.5,
		Attributes:   map[string]any{"model": "gpt-4", "tokens": 100},
		ToolCallID:   "tc-1",
	}

	if span.JobID != "job-123" {
		t.Errorf("expected JobID job-123, got %s", span.JobID)
	}
	if span.NodeID != "node-a" {
		t.Errorf("expected NodeID node-a, got %s", span.NodeID)
	}
	if span.SpanID != "span-1" {
		t.Errorf("expected SpanID span-1, got %s", span.SpanID)
	}
	if span.ParentSpanID != "span-0" {
		t.Errorf("expected ParentSpanID span-0, got %s", span.ParentSpanID)
	}
	if span.Kind != KindCall {
		t.Errorf("expected Kind %s, got %s", KindCall, span.Kind)
	}
	if span.Status != StatusOK {
		t.Errorf("expected Status %s, got %s", StatusOK, span.Status)
	}
	if !span.StartedAt.Equal(now) {
		t.Errorf("expected StartedAt %v, got %v", now, span.StartedAt)
	}
	if span.DurationMs != 42.5 {
		t.Errorf("expected DurationMs 42.5, got %f", span.DurationMs)
	}
	if span.Attributes["model"] != "gpt-4" {
		t.Errorf("expected model attribute gpt-4, got %v", span.Attributes["model"])
	}
	if span.Attributes["tokens"] != 100 {
		t.Errorf("expected tokens attribute 100, got %v", span.Attributes["tokens"])
	}
	if span.ToolCallID != "tc-1" {
		t.Errorf("expected ToolCallID tc-1, got %s", span.ToolCallID)
	}
}

func TestSpanZeroValue(t *testing.T) {
	var span Span

	if span.ID != 0 {
		t.Errorf("expected zero ID, got %d", span.ID)
	}
	if span.JobID != "" {
		t.Errorf("expected empty JobID, got %s", span.JobID)
	}
	if span.Kind != "" {
		t.Errorf("expected empty Kind, got %s", span.Kind)
	}
	if span.Attributes != nil {
		t.Errorf("expected nil Attributes, got %v", span.Attributes)
	}
	if span.DurationMs != 0 {
		t.Errorf("expected zero DurationMs, got %f", span.DurationMs)
	}
}

func TestNodeGroupStructure(t *testing.T) {
	spans := []Span{
		{SpanID: "s1", NodeID: "node-a", Kind: KindNode, Status: StatusOK},
		{SpanID: "s2", NodeID: "node-a", Kind: KindCall, Status: StatusOK},
	}

	group := NodeGroup{
		NodeID: "node-a",
		Spans:  spans,
	}

	if group.NodeID != "node-a" {
		t.Errorf("expected NodeID node-a, got %s", group.NodeID)
	}
	if len(group.Spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(group.Spans))
	}
	if group.Spans[0].SpanID != "s1" {
		t.Errorf("expected first span s1, got %s", group.Spans[0].SpanID)
	}
}
