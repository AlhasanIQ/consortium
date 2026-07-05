package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/trace"
	"github.com/gorilla/mux"
)

// TestTraceAPIIntegration sets up real storage with trace spans, calls
// GET /api/jobs/{id}/trace via httptest, and verifies the response shape
// and grouping by node_id.
func TestTraceAPIIntegration(t *testing.T) {
	db, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	registry := providers.NewRegistry()
	api := NewWorkflowAPI(db, registry, newTestJobManager(db, registry))
	router := mux.NewRouter()
	api.RegisterRoutes(router)

	// Create a job.
	job := &storage.Job{
		ID:          "trace-api-job",
		Description: "Trace API test",
		Model:       "test-model",
		Status:      "completed",
	}
	if err := db.CreateExecution(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	// Write spans across two nodes.
	ctx := context.Background()
	now := time.Now().UTC()

	spans := []*trace.Span{
		{
			JobID:      "trace-api-job",
			NodeID:     "node-alpha",
			SpanID:     "span-a1",
			Kind:       trace.KindNode,
			Status:     trace.StatusOK,
			StartedAt:  now,
			DurationMs: 100.0,
			Attributes: map[string]any{"model": "gpt-4"},
		},
		{
			JobID:        "trace-api-job",
			NodeID:       "node-alpha",
			SpanID:       "span-a2",
			ParentSpanID: "span-a1",
			Kind:         trace.KindCall,
			Status:       trace.StatusOK,
			StartedAt:    now.Add(5 * time.Millisecond),
			DurationMs:   80.0,
			Attributes:   map[string]any{"provider": "openrouter"},
		},
		{
			JobID:      "trace-api-job",
			NodeID:     "node-beta",
			SpanID:     "span-b1",
			Kind:       trace.KindNode,
			Status:     trace.StatusError,
			StartedAt:  now.Add(200 * time.Millisecond),
			DurationMs: 50.0,
			Attributes: map[string]any{"error_message": "timeout"},
		},
	}

	for _, s := range spans {
		if err := db.WriteSpan(ctx, s); err != nil {
			t.Fatalf("WriteSpan failed for %s: %v", s.SpanID, err)
		}
	}

	// Make the API request.
	req := httptest.NewRequest("GET", "/api/jobs/trace-api-job/trace", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	// Parse response.
	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify top-level shape.
	jobID, ok := response["job_id"].(string)
	if !ok || jobID != "trace-api-job" {
		t.Errorf("Expected job_id 'trace-api-job', got %v", response["job_id"])
	}

	nodeGroupsRaw, ok := response["node_groups"].([]interface{})
	if !ok {
		t.Fatalf("Expected node_groups array, got %T", response["node_groups"])
	}

	if len(nodeGroupsRaw) != 2 {
		t.Fatalf("Expected 2 node groups, got %d", len(nodeGroupsRaw))
	}

	// Verify first group (node-alpha) has 2 spans.
	group0 := nodeGroupsRaw[0].(map[string]interface{})
	if group0["node_id"] != "node-alpha" {
		t.Errorf("Expected first group node_id 'node-alpha', got %v", group0["node_id"])
	}
	group0Spans := group0["spans"].([]interface{})
	if len(group0Spans) != 2 {
		t.Errorf("Expected 2 spans in node-alpha group, got %d", len(group0Spans))
	}

	// Verify second group (node-beta) has 1 span.
	group1 := nodeGroupsRaw[1].(map[string]interface{})
	if group1["node_id"] != "node-beta" {
		t.Errorf("Expected second group node_id 'node-beta', got %v", group1["node_id"])
	}
	group1Spans := group1["spans"].([]interface{})
	if len(group1Spans) != 1 {
		t.Errorf("Expected 1 span in node-beta group, got %d", len(group1Spans))
	}

	// Verify a span's attributes survive the full round-trip through the API.
	span0 := group0Spans[0].(map[string]interface{})
	attrs0 := span0["attributes"].(map[string]interface{})
	if attrs0["model"] != "gpt-4" {
		t.Errorf("Expected span attribute model='gpt-4', got %v", attrs0["model"])
	}

	// Verify the error span's status comes through.
	spanB := group1Spans[0].(map[string]interface{})
	if spanB["status"] != "error" {
		t.Errorf("Expected span status 'error', got %v", spanB["status"])
	}

	// Verify parent_span_id is present on child span.
	span1 := group0Spans[1].(map[string]interface{})
	if span1["parent_span_id"] != "span-a1" {
		t.Errorf("Expected parent_span_id 'span-a1', got %v", span1["parent_span_id"])
	}
}

// TestTraceAPIEmptyTraces verifies that requesting traces for a job with no
// trace data returns 200 with an empty node_groups array (not 404).
func TestTraceAPIEmptyTraces(t *testing.T) {
	db, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	registry := providers.NewRegistry()
	api := NewWorkflowAPI(db, registry, newTestJobManager(db, registry))
	router := mux.NewRouter()
	api.RegisterRoutes(router)

	// Create a job with no trace spans.
	job := &storage.Job{
		ID:          "trace-empty-job",
		Description: "Empty trace test",
		Model:       "test-model",
		Status:      "completed",
	}
	if err := db.CreateExecution(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}

	// Request traces for the job.
	req := httptest.NewRequest("GET", "/api/jobs/trace-empty-job/trace", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Must return 200, not 404.
	if w.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d. Body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify job_id is present.
	jobID, ok := response["job_id"].(string)
	if !ok || jobID != "trace-empty-job" {
		t.Errorf("Expected job_id 'trace-empty-job', got %v", response["job_id"])
	}

	// Verify node_groups is present and empty (or null serialized as nil).
	nodeGroups := response["node_groups"]
	if nodeGroups == nil {
		// null is acceptable for an empty slice in Go JSON serialization.
		return
	}

	nodeGroupsArr, ok := nodeGroups.([]interface{})
	if !ok {
		t.Fatalf("Expected node_groups to be an array or null, got %T", nodeGroups)
	}

	if len(nodeGroupsArr) != 0 {
		t.Errorf("Expected empty node_groups, got %d groups", len(nodeGroupsArr))
	}
}

// TestTraceAPIJobNotFound verifies that requesting traces for a non-existent
// job returns 404.
func TestTraceAPIJobNotFound(t *testing.T) {
	db, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}

	registry := providers.NewRegistry()
	api := NewWorkflowAPI(db, registry, newTestJobManager(db, registry))
	router := mux.NewRouter()
	api.RegisterRoutes(router)

	req := httptest.NewRequest("GET", "/api/jobs/nonexistent-job/trace", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d for nonexistent job, got %d. Body: %s",
			http.StatusNotFound, w.Code, w.Body.String())
	}
}
