package storage

import (
	"testing"
	"time"
)

func TestWorkflowStorage(t *testing.T) {
	t.Run("create and get workflow", func(t *testing.T) {
		store := setupStorageTest(t)

		wf := &WorkflowDefinition{
			ID:          "wf-test-1",
			Name:        "Test Workflow",
			Description: "Test description",
			Definition:  `{"id":"wf-test-1","name":"Test","nodes":[]}`,
		}

		err := store.CreateWorkflow(wf)
		if err != nil {
			t.Fatalf("failed to create workflow: %v", err)
		}

		retrieved, err := store.GetWorkflow("wf-test-1")
		if err != nil {
			t.Fatalf("failed to get workflow: %v", err)
		}

		if retrieved.Name != "Test Workflow" {
			t.Errorf("expected name 'Test Workflow', got %s", retrieved.Name)
		}
		if retrieved.Description != "Test description" {
			t.Errorf("expected description, got %s", retrieved.Description)
		}
	})

	t.Run("get non-existent workflow", func(t *testing.T) {
		store := setupStorageTest(t)

		_, err := store.GetWorkflow("non-existent")
		if err == nil {
			t.Fatal("expected error for non-existent workflow")
		}
	})

	t.Run("update workflow", func(t *testing.T) {
		store := setupStorageTest(t)

		wf := &WorkflowDefinition{
			ID:         "wf-update",
			Name:       "Original",
			Definition: `{"id":"wf-update"}`,
		}
		store.CreateWorkflow(wf)

		wf.Name = "Updated"
		wf.Description = "New description"
		err := store.UpdateWorkflow(wf)
		if err != nil {
			t.Fatalf("failed to update: %v", err)
		}

		retrieved, _ := store.GetWorkflow("wf-update")
		if retrieved.Name != "Updated" {
			t.Errorf("expected 'Updated', got %s", retrieved.Name)
		}
	})

	t.Run("update non-existent workflow returns ErrNotFound", func(t *testing.T) {
		store := setupStorageTest(t)

		err := store.UpdateWorkflow(&WorkflowDefinition{
			ID:         "wf-missing",
			Name:       "Missing",
			Definition: `{}`,
		})
		if err != ErrNotFound {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("delete workflow", func(t *testing.T) {
		store := setupStorageTest(t)

		wf := &WorkflowDefinition{
			ID:         "wf-delete",
			Name:       "To Delete",
			Definition: `{}`,
		}
		store.CreateWorkflow(wf)

		err := store.DeleteWorkflow("wf-delete")
		if err != nil {
			t.Fatalf("failed to delete: %v", err)
		}

		_, err = store.GetWorkflow("wf-delete")
		if err == nil {
			t.Fatal("expected error after delete")
		}
	})

	t.Run("list workflows", func(t *testing.T) {
		store := setupStorageTest(t)

		// Count existing workflows (may include seeded ones)
		existingWorkflows, _ := store.ListWorkflows(100)
		existingCount := len(existingWorkflows)

		for i := 1; i <= 3; i++ {
			wf := &WorkflowDefinition{
				ID:         "wf-" + string(rune('0'+i)),
				Name:       "Workflow " + string(rune('0'+i)),
				Definition: `{}`,
			}
			store.CreateWorkflow(wf)
			time.Sleep(10 * time.Millisecond) // Ensure different timestamps
		}

		expectedCount := existingCount + 3
		workflows, err := store.ListWorkflows(expectedCount + 5)
		if err != nil {
			t.Fatalf("failed to list workflows: %v", err)
		}
		if len(workflows) != expectedCount {
			t.Errorf("expected %d workflows, got %d", expectedCount, len(workflows))
		}

		// Should be ordered by updated_at DESC
		if workflows[0].Name != "Workflow 3" {
			t.Errorf("expected newest first, got %s", workflows[0].Name)
		}
	})

	t.Run("list workflows with limit", func(t *testing.T) {
		store := setupStorageTest(t)

		for i := 1; i <= 5; i++ {
			wf := &WorkflowDefinition{
				ID:         "wf-limit-" + string(rune('0'+i)),
				Name:       "Workflow " + string(rune('0'+i)),
				Definition: `{}`,
			}
			store.CreateWorkflow(wf)
		}

		workflows, err := store.ListWorkflows(2)
		if err != nil {
			t.Fatalf("failed to list workflows: %v", err)
		}

		if len(workflows) != 2 {
			t.Errorf("expected 2 workflows, got %d", len(workflows))
		}
	})
}

func TestLogLLMRequest(t *testing.T) {
	t.Run("log successful request", func(t *testing.T) {
		store := setupStorageTest(t)

		job := &Job{
			ID:          "job-1",
			Description: "test",
			Model:       "test-model",
			Status:      "running",
		}
		store.CreateExecution(job)

		err := store.LogLLMRequest(
			"job-1",
			"node-1",
			"gpt-4",
			"test prompt",
			"test response",
			100,
			50,
			0.001,
			123.45,
			"completed",
			"",
		)

		if err != nil {
			t.Fatalf("failed to log request: %v", err)
		}

		// Verify node was created via workflow_node_executions
		nodes, err := store.GetWorkflowNodes("job-1")
		if err != nil {
			t.Fatal(err)
		}

		if len(nodes) != 1 {
			t.Errorf("expected 1 node, got %d", len(nodes))
		}

		if nodes[0].Model != "gpt-4" {
			t.Errorf("expected gpt-4, got %s", nodes[0].Model)
		}
	})

	t.Run("log failed request", func(t *testing.T) {
		store := setupStorageTest(t)

		job := &Job{
			ID:          "job-2",
			Description: "test",
			Model:       "test-model",
			Status:      "running",
		}
		store.CreateExecution(job)

		err := store.LogLLMRequest(
			"job-2",
			"node-1",
			"gpt-4",
			"test prompt",
			"",
			0,
			0,
			0,
			0,
			"failed",
			"API error",
		)

		if err != nil {
			t.Fatalf("failed to log request: %v", err)
		}

		// Verify error was logged in workflow_node_executions
		nodes, err := store.GetWorkflowNodes("job-2")
		if err != nil {
			t.Fatal(err)
		}

		if len(nodes) != 1 {
			t.Fatal("expected 1 node")
		}

		if nodes[0].ErrorMessage != "API error" {
			t.Errorf("expected error message, got %s", nodes[0].ErrorMessage)
		}
	})
}

func TestWorkflowNodeLogging(t *testing.T) {
	t.Run("add workflow node", func(t *testing.T) {
		store := setupStorageTest(t)

		job := &Job{
			ID:          "job-wf-1",
			Description: "workflow test",
			Model:       "workflow",
			Status:      "running",
		}
		store.CreateExecution(job)

		node := &WorkflowNode{
			ExecutionID: "job-wf-1",
			NodeID:      "node-1",
			NodeType:    "prompt",
			NodeOrder:   1,
			Status:      "completed",
			Prompt:      "test prompt",
			Model:       "gpt-4",
			Output:      "test output",
			TokensInput: 100,
		}

		err := store.AddWorkflowNode(node)
		if err != nil {
			t.Fatalf("failed to add node: %v", err)
		}

		// Verify node was created
		rows, err := store.db.Query("SELECT node_id, status FROM workflow_node_executions WHERE job_id = ?", "job-wf-1")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()

		var nodeID, status string
		if rows.Next() {
			rows.Scan(&nodeID, &status)
		}

		if nodeID != "node-1" {
			t.Errorf("expected node-1, got %s", nodeID)
		}
		if status != "completed" {
			t.Errorf("expected completed, got %s", status)
		}
	})
}

func TestMarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		wantErr bool
	}{
		{"simple map", map[string]string{"key": "value"}, false},
		{"struct", struct{ Name string }{"test"}, false},
		{"slice", []int{1, 2, 3}, false},
		{"nil", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := MarshalJSON(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("MarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && result == "" {
				t.Error("expected non-empty JSON string")
			}
		})
	}
}

func setupStorageTest(t *testing.T) *Storage {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	return store
}
