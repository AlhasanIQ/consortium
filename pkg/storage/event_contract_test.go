package storage

import (
	"context"
	"testing"

	"github.com/alhasaniq/consortium/pkg/events"
)

// allAgentEventTypes lists the agent/memory event types that flow through the event envelope pipeline.
var allAgentEventTypes = []string{
	events.EventAgentPlanCreated,
	events.EventAgentIterationStarted,
	events.EventAgentToolCalled,
	events.EventAgentToolResult,
	events.EventAgentIterationCompleted,
	events.EventAgentBranchCreated,
	events.EventAgentBranchPruned,
	events.EventAgentBranchSelected,
	events.EventAgentTerminated,
	events.EventAgentFailed,
	events.EventMemoryRead,
	events.EventMemoryWrite,
	events.EventRetrievalExecuted,
	events.EventRetrievalResultUsed,
}

// createTestJob is a helper that creates a minimal job record in the DB so that
// AppendEventWithDetails can increment its last_event_sequence.
func createTestJob(t *testing.T, store *Storage, jobID string) {
	t.Helper()
	err := store.CreateExecution(&Job{
		ID:          jobID,
		Description: "test job for event contract",
		Model:       "test-model",
		Status:      "pending",
	})
	if err != nil {
		t.Fatalf("failed to create test job %q: %v", jobID, err)
	}
}

// TestAgentEventTypesRoundTrip verifies that each of the 14 new agent/memory event types
// can be written via AppendEventWithDetails and read back via GetEventsAfter with all
// correlation fields and payload intact.
func TestAgentEventTypesRoundTrip(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	jobID := "job-agent-roundtrip"
	createTestJob(t, store, jobID)

	for i, eventType := range allAgentEventTypes {
		t.Run(eventType, func(t *testing.T) {
			nodeID := "node-" + eventType
			executionID := "exec-" + eventType
			runID := "run-" + eventType
			agentRunID := "arun-" + eventType
			iteration := i + 1

			payload := map[string]interface{}{
				"node_id":      nodeID,
				"execution_id": executionID,
				"run_id":       runID,
				"agent_run_id": agentRunID,
				"iteration":    iteration,
				"custom_field": "value-" + eventType,
			}

			// Write the event
			env, err := store.AppendEventWithDetails(ctx, jobID, eventType, "", "", "", "", payload)
			if err != nil {
				t.Fatalf("AppendEventWithDetails failed: %v", err)
			}

			// Verify the returned envelope
			if env.Type != eventType {
				t.Errorf("returned envelope Type: got %q, want %q", env.Type, eventType)
			}
			if env.NodeID != nodeID {
				t.Errorf("returned envelope NodeID: got %q, want %q", env.NodeID, nodeID)
			}
			if env.ExecutionID != executionID {
				t.Errorf("returned envelope ExecutionID: got %q, want %q", env.ExecutionID, executionID)
			}
			if env.RunID != runID {
				t.Errorf("returned envelope RunID: got %q, want %q", env.RunID, runID)
			}
			if env.AgentRunID != agentRunID {
				t.Errorf("returned envelope AgentRunID: got %q, want %q", env.AgentRunID, agentRunID)
			}
			if env.Iteration != iteration {
				t.Errorf("returned envelope Iteration: got %d, want %d", env.Iteration, iteration)
			}

			// Read back via GetEventsAfter (use sequence-1 to capture our event)
			readEnvs, err := store.GetEventsAfter(ctx, jobID, env.Sequence-1)
			if err != nil {
				t.Fatalf("GetEventsAfter failed: %v", err)
			}

			// Find our specific event in the results
			var found *events.EventEnvelope
			for _, e := range readEnvs {
				if e.Sequence == env.Sequence {
					found = e
					break
				}
			}
			if found == nil {
				t.Fatalf("event with sequence %d not found in GetEventsAfter results (got %d events)", env.Sequence, len(readEnvs))
			}

			// Verify all correlation fields round-tripped through the DB
			if found.Type != eventType {
				t.Errorf("read-back Type: got %q, want %q", found.Type, eventType)
			}
			if found.NodeID != nodeID {
				t.Errorf("read-back NodeID: got %q, want %q", found.NodeID, nodeID)
			}
			if found.ExecutionID != executionID {
				t.Errorf("read-back ExecutionID: got %q, want %q", found.ExecutionID, executionID)
			}
			if found.RunID != runID {
				t.Errorf("read-back RunID: got %q, want %q", found.RunID, runID)
			}
			if found.AgentRunID != agentRunID {
				t.Errorf("read-back AgentRunID: got %q, want %q", found.AgentRunID, agentRunID)
			}
			if found.Iteration != iteration {
				t.Errorf("read-back Iteration: got %d, want %d", found.Iteration, iteration)
			}

			// Verify the payload is preserved
			if found.Payload == nil {
				t.Fatal("read-back Payload is nil")
			}
			if v, ok := found.Payload["custom_field"].(string); !ok || v != "value-"+eventType {
				t.Errorf("read-back Payload[custom_field]: got %v, want %q", found.Payload["custom_field"], "value-"+eventType)
			}
		})
	}
}

// TestCorrelationFieldsExtractedFromPayload verifies that correlation fields
// embedded inside the payload map (not passed as top-level nodeID parameter)
// are correctly extracted into the envelope's typed fields during both
// write (AppendEventWithDetails) and read (GetEventsAfter).
func TestCorrelationFieldsExtractedFromPayload(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	jobID := "job-correlation-extract"
	createTestJob(t, store, jobID)

	// Pass correlation fields only inside the payload, with nodeID param left empty.
	payload := map[string]interface{}{
		"node_id":      "node-alpha",
		"execution_id": "exec-001",
		"run_id":       "run-001",
		"agent_run_id": "arun-001",
		"iteration":    3,
		"detail":       "some extra data",
	}

	env, err := store.AppendEventWithDetails(ctx, jobID, events.EventAgentToolCalled, "", "", "", "", payload)
	if err != nil {
		t.Fatalf("AppendEventWithDetails failed: %v", err)
	}

	// --- Verify write-side extraction (returned envelope) ---

	if env.NodeID != "node-alpha" {
		t.Errorf("write-side NodeID: got %q, want %q", env.NodeID, "node-alpha")
	}
	if env.ExecutionID != "exec-001" {
		t.Errorf("write-side ExecutionID: got %q, want %q", env.ExecutionID, "exec-001")
	}
	if env.RunID != "run-001" {
		t.Errorf("write-side RunID: got %q, want %q", env.RunID, "run-001")
	}
	if env.AgentRunID != "arun-001" {
		t.Errorf("write-side AgentRunID: got %q, want %q", env.AgentRunID, "arun-001")
	}
	if env.Iteration != 3 {
		t.Errorf("write-side Iteration: got %d, want %d", env.Iteration, 3)
	}

	// --- Verify read-side extraction (GetEventsAfter) ---

	readEnvs, err := store.GetEventsAfter(ctx, jobID, 0)
	if err != nil {
		t.Fatalf("GetEventsAfter failed: %v", err)
	}
	if len(readEnvs) != 1 {
		t.Fatalf("expected 1 event, got %d", len(readEnvs))
	}

	got := readEnvs[0]
	if got.NodeID != "node-alpha" {
		t.Errorf("read-side NodeID: got %q, want %q", got.NodeID, "node-alpha")
	}
	if got.ExecutionID != "exec-001" {
		t.Errorf("read-side ExecutionID: got %q, want %q", got.ExecutionID, "exec-001")
	}
	if got.RunID != "run-001" {
		t.Errorf("read-side RunID: got %q, want %q", got.RunID, "run-001")
	}
	if got.AgentRunID != "arun-001" {
		t.Errorf("read-side AgentRunID: got %q, want %q", got.AgentRunID, "arun-001")
	}
	if got.Iteration != 3 {
		t.Errorf("read-side Iteration: got %d, want %d", got.Iteration, 3)
	}
	if v, ok := got.Payload["detail"].(string); !ok || v != "some extra data" {
		t.Errorf("read-side Payload[detail]: got %v, want %q", got.Payload["detail"], "some extra data")
	}
}

// TestExistingEventTypesUnaffected verifies that a standard node_start event
// still works correctly and has empty/zero values for the agent correlation fields.
func TestExistingEventTypesUnaffected(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	jobID := "job-existing-event"
	createTestJob(t, store, jobID)

	// Emit a classic node_start event with only core node fields.
	payload := map[string]interface{}{
		"node_id": "node-1",
		"model":   "gpt-4",
		"prompt":  "Hello world",
	}

	env, err := store.AppendEventWithDetails(ctx, jobID, events.EventNodeStart, "node-1", "Starting node-1", "", "", payload)
	if err != nil {
		t.Fatalf("AppendEventWithDetails failed: %v", err)
	}

	// Verify that the new correlation fields are at their zero values on the returned envelope.
	if env.ExecutionID != "" {
		t.Errorf("returned ExecutionID should be empty for classic event, got %q", env.ExecutionID)
	}
	if env.RunID != "" {
		t.Errorf("returned RunID should be empty for classic event, got %q", env.RunID)
	}
	if env.AgentRunID != "" {
		t.Errorf("returned AgentRunID should be empty for classic event, got %q", env.AgentRunID)
	}
	if env.Iteration != 0 {
		t.Errorf("returned Iteration should be 0 for classic event, got %d", env.Iteration)
	}

	// The NodeID should still be set (from the nodeID parameter).
	if env.NodeID != "node-1" {
		t.Errorf("returned NodeID: got %q, want %q", env.NodeID, "node-1")
	}
	if env.Type != events.EventNodeStart {
		t.Errorf("returned Type: got %q, want %q", env.Type, events.EventNodeStart)
	}

	// Read back and verify round-trip.
	readEnvs, err := store.GetEventsAfter(ctx, jobID, 0)
	if err != nil {
		t.Fatalf("GetEventsAfter failed: %v", err)
	}
	if len(readEnvs) != 1 {
		t.Fatalf("expected 1 event, got %d", len(readEnvs))
	}

	got := readEnvs[0]
	if got.Type != events.EventNodeStart {
		t.Errorf("read-back Type: got %q, want %q", got.Type, events.EventNodeStart)
	}
	if got.NodeID != "node-1" {
		t.Errorf("read-back NodeID: got %q, want %q", got.NodeID, "node-1")
	}
	if got.ExecutionID != "" {
		t.Errorf("read-back ExecutionID should be empty, got %q", got.ExecutionID)
	}
	if got.RunID != "" {
		t.Errorf("read-back RunID should be empty, got %q", got.RunID)
	}
	if got.AgentRunID != "" {
		t.Errorf("read-back AgentRunID should be empty, got %q", got.AgentRunID)
	}
	if got.Iteration != 0 {
		t.Errorf("read-back Iteration should be 0, got %d", got.Iteration)
	}

	// Verify payload is preserved.
	if v, ok := got.Payload["model"].(string); !ok || v != "gpt-4" {
		t.Errorf("read-back Payload[model]: got %v, want %q", got.Payload["model"], "gpt-4")
	}
	if v, ok := got.Payload["prompt"].(string); !ok || v != "Hello world" {
		t.Errorf("read-back Payload[prompt]: got %v, want %q", got.Payload["prompt"], "Hello world")
	}
}
