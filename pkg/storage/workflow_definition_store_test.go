package storage

import (
	"errors"
	"testing"
)

func TestCreateAndGetWorkflow(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	wf := &WorkflowDefinition{
		ID:          "test-wf-1",
		Name:        "Test Workflow",
		Description: "A test workflow",
		Definition:  `{"id":"test-wf-1","name":"Test Workflow","nodes":[]}`,
	}

	if err := store.CreateWorkflow(wf); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if wf.Version != 1 {
		t.Errorf("expected version 1 after create, got %d", wf.Version)
	}

	got, err := store.GetWorkflow("test-wf-1")
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if got.ID != "test-wf-1" {
		t.Errorf("expected id test-wf-1, got %s", got.ID)
	}
	if got.Name != "Test Workflow" {
		t.Errorf("expected name 'Test Workflow', got %s", got.Name)
	}
	if got.Description != "A test workflow" {
		t.Errorf("expected description 'A test workflow', got %s", got.Description)
	}
	if got.Version != 1 {
		t.Errorf("expected version 1, got %d", got.Version)
	}
}

func TestGetWorkflow_NotFound(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	_, err = store.GetWorkflow("nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestCreateWorkflow_DefaultVersion(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	wf := &WorkflowDefinition{
		ID:         "ver-test",
		Name:       "Version Test",
		Definition: `{"id":"ver-test"}`,
		Version:    0, // should default to 1
	}
	if err := store.CreateWorkflow(wf); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if wf.Version != 1 {
		t.Errorf("expected version defaulted to 1, got %d", wf.Version)
	}
}

func TestUpdateWorkflow(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	wf := &WorkflowDefinition{
		ID:         "update-test",
		Name:       "Original",
		Definition: `{"id":"update-test"}`,
	}
	if err := store.CreateWorkflow(wf); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	wf.Name = "Updated"
	wf.Description = "Updated description"
	if err := store.UpdateWorkflow(wf); err != nil {
		t.Fatalf("UpdateWorkflow: %v", err)
	}
	if wf.Version != 2 {
		t.Errorf("expected version 2 after update, got %d", wf.Version)
	}

	got, err := store.GetWorkflow("update-test")
	if err != nil {
		t.Fatalf("GetWorkflow after update: %v", err)
	}
	if got.Name != "Updated" {
		t.Errorf("expected updated name, got %s", got.Name)
	}
	if got.Version != 2 {
		t.Errorf("expected version 2, got %d", got.Version)
	}
}

func TestUpdateWorkflow_ConflictOnStaleVersion(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	wf := &WorkflowDefinition{
		ID:         "conflict-test",
		Name:       "Original",
		Definition: `{"id":"conflict-test"}`,
	}
	if err := store.CreateWorkflow(wf); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	// Simulate stale version
	stale := &WorkflowDefinition{
		ID:         "conflict-test",
		Name:       "Stale Update",
		Definition: `{"id":"conflict-test"}`,
		Version:    999, // wrong version
	}
	err = store.UpdateWorkflow(stale)
	if !errors.Is(err, ErrConflict) {
		t.Errorf("expected ErrConflict, got %v", err)
	}
}

func TestUpdateWorkflow_NotFoundNoVersion(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	wf := &WorkflowDefinition{
		ID:      "nonexistent",
		Name:    "Ghost",
		Version: 0,
	}
	err = store.UpdateWorkflow(wf)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteWorkflow(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	wf := &WorkflowDefinition{
		ID:         "delete-test",
		Name:       "To Delete",
		Definition: `{"id":"delete-test"}`,
	}
	if err := store.CreateWorkflow(wf); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	if err := store.DeleteWorkflow("delete-test"); err != nil {
		t.Fatalf("DeleteWorkflow: %v", err)
	}

	_, err = store.GetWorkflow("delete-test")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestListWorkflows(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	for i := 0; i < 3; i++ {
		wf := &WorkflowDefinition{
			ID:         "list-" + string(rune('a'+i)),
			Name:       "Workflow " + string(rune('A'+i)),
			Definition: `{"id":"list-` + string(rune('a'+i)) + `"}`,
		}
		if err := store.CreateWorkflow(wf); err != nil {
			t.Fatalf("CreateWorkflow %d: %v", i, err)
		}
	}

	workflows, err := store.ListWorkflows(10)
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	// Seeds are also loaded, so we expect at least 3
	if len(workflows) < 3 {
		t.Errorf("expected at least 3 workflows, got %d", len(workflows))
	}

	// Test limit
	limited, err := store.ListWorkflows(2)
	if err != nil {
		t.Fatalf("ListWorkflows limited: %v", err)
	}
	if len(limited) != 2 {
		t.Errorf("expected exactly 2 workflows with limit, got %d", len(limited))
	}
}
