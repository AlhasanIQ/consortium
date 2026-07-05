package storage

import (
	"testing"
)

func TestDurableFieldsRoundTrip(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	job := &Job{
		ID:                  "durable-1",
		Description:         "Durable test",
		Model:               "workflow",
		Status:              "pending",
		WorkflowExecutionID: "durable-1",
		RunID:               "durable-1",
		RunNumber:           1,
		DAGSnapshot:         `{"id":"wf-1","name":"test","nodes":[{"id":"n1"}],"edges":[]}`,
		DAGHash:             "deadbeef1234567890abcdef",
	}
	if err := store.CreateExecution(job); err != nil {
		t.Fatalf("Failed to create execution: %v", err)
	}

	retrieved, err := store.GetExecution("durable-1")
	if err != nil {
		t.Fatalf("Failed to get execution: %v", err)
	}

	if retrieved.WorkflowExecutionID != "durable-1" {
		t.Errorf("Expected workflow_execution_id durable-1, got %s", retrieved.WorkflowExecutionID)
	}
	if retrieved.RunID != "durable-1" {
		t.Errorf("Expected run_id durable-1, got %s", retrieved.RunID)
	}
	if retrieved.RunNumber != 1 {
		t.Errorf("Expected run_number 1, got %d", retrieved.RunNumber)
	}
	if retrieved.DAGHash != "deadbeef1234567890abcdef" {
		t.Errorf("Expected dag_hash deadbeef..., got %s", retrieved.DAGHash)
	}
	if retrieved.DAGSnapshot != job.DAGSnapshot {
		t.Errorf("DAGSnapshot mismatch")
	}
}

func TestMigrationSafety_NullDurableFields(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	// Create a job WITHOUT durable fields (simulates pre-migration data)
	job := &Job{
		ID:          "legacy-1",
		Description: "Legacy job",
		Model:       "gpt-4",
		Status:      "completed",
	}
	if err := store.CreateExecution(job); err != nil {
		t.Fatalf("Failed to create legacy job: %v", err)
	}

	// Retrieve — durable fields should have zero values, not error
	retrieved, err := store.GetExecution("legacy-1")
	if err != nil {
		t.Fatalf("Failed to get legacy job: %v", err)
	}
	if retrieved.WorkflowExecutionID != "" {
		t.Errorf("Expected empty workflow_execution_id, got %s", retrieved.WorkflowExecutionID)
	}
	if retrieved.RunNumber != 1 {
		t.Errorf("Expected default run_number 1, got %d", retrieved.RunNumber)
	}
	if retrieved.DAGHash != "" {
		t.Errorf("Expected empty dag_hash, got %s", retrieved.DAGHash)
	}
}
