package storage

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Helper: create a durable job (all four durable fields required by CHECK).
// ---------------------------------------------------------------------------

func createDurableTestJob(t *testing.T, s *Storage, id, status string) {
	t.Helper()
	job := &Job{
		ID:                  id,
		Description:         "test",
		Model:               "workflow",
		Status:              status,
		WorkflowID:          "wf-1",
		WorkflowExecutionID: id,
		RunID:               id,
		RunNumber:           1,
		DAGHash:             "hash-" + id,
		DAGSnapshot:         `{"nodes":[]}`,
	}
	if err := s.CreateExecution(job); err != nil {
		t.Fatalf("failed to create durable test job %s: %v", id, err)
	}
}

// ---------------------------------------------------------------------------
// AddWorkflowNode
// ---------------------------------------------------------------------------

func TestNodeStore_AddWorkflowNode_Basic(t *testing.T) {
	store := newTestStore(t)
	createDurableTestJob(t, store, "job-add-node", "running")

	node := &WorkflowNode{
		ExecutionID:   "job-add-node",
		RunID:         "job-add-node",
		NodeID:        "prompt-1",
		NodeType:      "prompt",
		Status:        "completed",
		NodeLabel:     "GPT-4",
		Prompt:        "hello",
		Model:         "gpt-4",
		Output:        "world",
		TokensInput:   100,
		TokensOutput:  50,
		Cost:          0.01,
		LatencyMs:     200.0,
		AttemptNumber: 1,
	}
	if err := store.AddWorkflowNode(node); err != nil {
		t.Fatalf("AddWorkflowNode failed: %v", err)
	}

	nodes, err := store.GetWorkflowNodes("job-add-node")
	if err != nil {
		t.Fatalf("GetWorkflowNodes failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].NodeID != "prompt-1" {
		t.Errorf("NodeID = %q, want prompt-1", nodes[0].NodeID)
	}
	if nodes[0].Status != "completed" {
		t.Errorf("Status = %q, want completed", nodes[0].Status)
	}
	if nodes[0].Output != "world" {
		t.Errorf("Output = %q, want world", nodes[0].Output)
	}
	if nodes[0].TokensInput != 100 {
		t.Errorf("TokensInput = %d, want 100", nodes[0].TokensInput)
	}
	if nodes[0].Cost != 0.01 {
		t.Errorf("Cost = %f, want 0.01", nodes[0].Cost)
	}
}

func TestNodeStore_AddWorkflowNode_NilReturnsError(t *testing.T) {
	store := newTestStore(t)
	err := store.AddWorkflowNode(nil)
	if err == nil {
		t.Fatal("expected error for nil node")
	}
}

func TestNodeStore_AddWorkflowNode_MissingRequiredFields(t *testing.T) {
	store := newTestStore(t)

	tests := []struct {
		name string
		node *WorkflowNode
	}{
		{"missing ExecutionID", &WorkflowNode{NodeID: "n", NodeType: "prompt", Status: "completed"}},
		{"missing NodeID", &WorkflowNode{ExecutionID: "e", NodeType: "prompt", Status: "completed"}},
		{"missing NodeType", &WorkflowNode{ExecutionID: "e", NodeID: "n", Status: "completed"}},
		{"missing Status", &WorkflowNode{ExecutionID: "e", NodeID: "n", NodeType: "prompt"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := store.AddWorkflowNode(tt.node); err == nil {
				t.Error("expected error for missing required field")
			}
		})
	}
}

func TestNodeStore_AddWorkflowNode_DefaultsAttemptTo1(t *testing.T) {
	store := newTestStore(t)
	createDurableTestJob(t, store, "job-attempt-default", "running")

	node := &WorkflowNode{
		ExecutionID:   "job-attempt-default",
		RunID:         "job-attempt-default",
		NodeID:        "n1",
		NodeType:      "prompt",
		Status:        "completed",
		AttemptNumber: 0, // should be defaulted to 1
	}
	if err := store.AddWorkflowNode(node); err != nil {
		t.Fatalf("AddWorkflowNode failed: %v", err)
	}

	nodes, err := store.GetWorkflowNodes("job-attempt-default")
	if err != nil {
		t.Fatalf("GetWorkflowNodes failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].AttemptNumber != 1 {
		t.Errorf("AttemptNumber = %d, want 1", nodes[0].AttemptNumber)
	}
}

func TestNodeStore_AddWorkflowNode_Upsert(t *testing.T) {
	store := newTestStore(t)
	createDurableTestJob(t, store, "job-upsert", "running")

	// Insert initial node
	node := &WorkflowNode{
		ExecutionID:   "job-upsert",
		RunID:         "job-upsert",
		NodeID:        "n1",
		NodeType:      "prompt",
		Status:        "running",
		AttemptNumber: 1,
	}
	if err := store.AddWorkflowNode(node); err != nil {
		t.Fatalf("first AddWorkflowNode failed: %v", err)
	}

	// Upsert with completed status
	node.Status = "completed"
	node.Output = "done"
	node.TokensInput = 50
	node.TokensOutput = 25
	node.Cost = 0.005
	if err := store.AddWorkflowNode(node); err != nil {
		t.Fatalf("upsert AddWorkflowNode failed: %v", err)
	}

	nodes, err := store.GetWorkflowNodes("job-upsert")
	if err != nil {
		t.Fatalf("GetWorkflowNodes failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node after upsert, got %d", len(nodes))
	}
	if nodes[0].Status != "completed" {
		t.Errorf("Status = %q, want completed", nodes[0].Status)
	}
	if nodes[0].Output != "done" {
		t.Errorf("Output = %q, want done", nodes[0].Output)
	}
}

func TestNodeStore_AddWorkflowNode_SetsCompletedAt(t *testing.T) {
	store := newTestStore(t)
	createDurableTestJob(t, store, "job-completed-at", "running")

	node := &WorkflowNode{
		ExecutionID:   "job-completed-at",
		RunID:         "job-completed-at",
		NodeID:        "n1",
		NodeType:      "prompt",
		Status:        "completed",
		AttemptNumber: 1,
	}
	if err := store.AddWorkflowNode(node); err != nil {
		t.Fatalf("AddWorkflowNode failed: %v", err)
	}

	nodes, err := store.GetWorkflowNodes("job-completed-at")
	if err != nil {
		t.Fatalf("GetWorkflowNodes failed: %v", err)
	}
	if nodes[0].CompletedAt == nil {
		t.Error("CompletedAt should be auto-set for completed status")
	}
}

// ---------------------------------------------------------------------------
// UpdateWorkflowNode
// ---------------------------------------------------------------------------

func TestNodeStore_UpdateWorkflowNode(t *testing.T) {
	store := newTestStore(t)
	createDurableTestJob(t, store, "job-update", "running")

	// First add
	node := &WorkflowNode{
		ExecutionID:   "job-update",
		RunID:         "job-update",
		NodeID:        "n1",
		NodeType:      "prompt",
		Status:        "running",
		AttemptNumber: 1,
	}
	if err := store.AddWorkflowNode(node); err != nil {
		t.Fatalf("AddWorkflowNode failed: %v", err)
	}

	// Update
	err := store.UpdateWorkflowNode("job-update", "n1", "completed", "output text", "", 100, 50, 0.02, 300.0)
	if err != nil {
		t.Fatalf("UpdateWorkflowNode failed: %v", err)
	}

	nodes, err := store.GetWorkflowNodes("job-update")
	if err != nil {
		t.Fatalf("GetWorkflowNodes failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].Status != "completed" {
		t.Errorf("Status = %q, want completed", nodes[0].Status)
	}
	if nodes[0].Output != "output text" {
		t.Errorf("Output = %q, want 'output text'", nodes[0].Output)
	}
	if nodes[0].TokensInput != 100 {
		t.Errorf("TokensInput = %d, want 100", nodes[0].TokensInput)
	}
}

// ---------------------------------------------------------------------------
// GetWorkflowNodes / ListWorkflowNodesByJobIDs
// ---------------------------------------------------------------------------

func TestNodeStore_GetWorkflowNodes_Empty(t *testing.T) {
	store := newTestStore(t)
	createDurableTestJob(t, store, "job-empty-nodes", "running")

	nodes, err := store.GetWorkflowNodes("job-empty-nodes")
	if err != nil {
		t.Fatalf("GetWorkflowNodes failed: %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(nodes))
	}
}

func TestNodeStore_GetWorkflowNodes_OrderedByNodeOrder(t *testing.T) {
	store := newTestStore(t)
	createDurableTestJob(t, store, "job-order", "running")

	for i, nid := range []string{"n3", "n1", "n2"} {
		node := &WorkflowNode{
			ExecutionID:   "job-order",
			RunID:         "job-order",
			NodeID:        nid,
			NodeType:      "prompt",
			Status:        "completed",
			NodeOrder:     i,
			AttemptNumber: 1,
		}
		if err := store.AddWorkflowNode(node); err != nil {
			t.Fatalf("AddWorkflowNode %s failed: %v", nid, err)
		}
	}

	nodes, err := store.GetWorkflowNodes("job-order")
	if err != nil {
		t.Fatalf("GetWorkflowNodes failed: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}
	// Expect ordering by node_order: n3(0), n1(1), n2(2)
	if nodes[0].NodeID != "n3" || nodes[1].NodeID != "n1" || nodes[2].NodeID != "n2" {
		t.Errorf("unexpected order: %s, %s, %s", nodes[0].NodeID, nodes[1].NodeID, nodes[2].NodeID)
	}
}

func TestNodeStore_ListWorkflowNodesByJobIDs(t *testing.T) {
	store := newTestStore(t)
	createDurableTestJob(t, store, "job-list-a", "running")
	createDurableTestJob(t, store, "job-list-b", "running")

	for _, jid := range []string{"job-list-a", "job-list-b"} {
		node := &WorkflowNode{
			ExecutionID:   jid,
			RunID:         jid,
			NodeID:        "n1",
			NodeType:      "prompt",
			Status:        "completed",
			AttemptNumber: 1,
		}
		if err := store.AddWorkflowNode(node); err != nil {
			t.Fatalf("AddWorkflowNode for %s failed: %v", jid, err)
		}
	}

	result, err := store.ListWorkflowNodesByJobIDs([]string{"job-list-a", "job-list-b"})
	if err != nil {
		t.Fatalf("ListWorkflowNodesByJobIDs failed: %v", err)
	}
	if len(result["job-list-a"]) != 1 {
		t.Errorf("expected 1 node for job-list-a, got %d", len(result["job-list-a"]))
	}
	if len(result["job-list-b"]) != 1 {
		t.Errorf("expected 1 node for job-list-b, got %d", len(result["job-list-b"]))
	}
}

func TestNodeStore_ListWorkflowNodesByJobIDs_Empty(t *testing.T) {
	store := newTestStore(t)

	result, err := store.ListWorkflowNodesByJobIDs(nil)
	if err != nil {
		t.Fatalf("ListWorkflowNodesByJobIDs failed: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty map, got %d entries", len(result))
	}
}

// ---------------------------------------------------------------------------
// LogLLMRequestFull
// ---------------------------------------------------------------------------

func TestNodeStore_LogLLMRequestFull_Basic(t *testing.T) {
	store := newTestStore(t)
	createDurableTestJob(t, store, "job-llm", "running")

	req := &LLMRequestLog{
		JobID:     "job-llm",
		NodeID:    "llm-node-1",
		Model:     "gpt-4",
		Prompt:    "What is 2+2?",
		Response:  "4",
		TokensIn:  10,
		TokensOut: 5,
		Cost:      0.001,
		Latency:   150.0,
		Status:    "completed",
	}
	if err := store.LogLLMRequestFull(req); err != nil {
		t.Fatalf("LogLLMRequestFull failed: %v", err)
	}

	// Verify via GetWorkflowNodes (LogLLMRequestFull writes both attempts and nodes)
	nodes, err := store.GetWorkflowNodes("job-llm")
	if err != nil {
		t.Fatalf("GetWorkflowNodes failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].Model != "gpt-4" {
		t.Errorf("Model = %q, want gpt-4", nodes[0].Model)
	}
	if nodes[0].Output != "4" {
		t.Errorf("Output = %q, want 4", nodes[0].Output)
	}
	if nodes[0].Status != "completed" {
		t.Errorf("Status = %q, want completed", nodes[0].Status)
	}

	// Also verify attempt was written
	attempts, err := store.GetNodeExecutionAttempts("job-llm")
	if err != nil {
		t.Fatalf("GetNodeExecutionAttempts failed: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(attempts))
	}
	if attempts[0].NodeID != "llm-node-1" {
		t.Errorf("attempt NodeID = %q, want llm-node-1", attempts[0].NodeID)
	}
}

func TestNodeStore_LogLLMRequestFull_GeneratesNodeID(t *testing.T) {
	store := newTestStore(t)
	createDurableTestJob(t, store, "job-llm-gen", "running")

	req := &LLMRequestLog{
		JobID:  "job-llm-gen",
		NodeID: "", // should be auto-generated
		Model:  "test",
		Status: "completed",
	}
	if err := store.LogLLMRequestFull(req); err != nil {
		t.Fatalf("LogLLMRequestFull failed: %v", err)
	}
	if req.NodeID == "" {
		t.Error("NodeID should have been auto-generated")
	}
}

func TestNodeStore_LogLLMRequestFull_DefaultsAttemptTo1(t *testing.T) {
	store := newTestStore(t)
	createDurableTestJob(t, store, "job-llm-att", "running")

	req := &LLMRequestLog{
		JobID:         "job-llm-att",
		NodeID:        "n1",
		Model:         "test",
		Status:        "completed",
		AttemptNumber: 0, // should default to 1
	}
	if err := store.LogLLMRequestFull(req); err != nil {
		t.Fatalf("LogLLMRequestFull failed: %v", err)
	}
	if req.AttemptNumber != 1 {
		t.Errorf("AttemptNumber = %d, want 1", req.AttemptNumber)
	}
}

func TestNodeStore_LogLLMRequestFull_WithParentNodeID(t *testing.T) {
	store := newTestStore(t)
	createDurableTestJob(t, store, "job-llm-parent", "running")

	req := &LLMRequestLog{
		JobID:        "job-llm-parent",
		NodeID:       "child-1",
		Model:        "test",
		Status:       "completed",
		ParentNodeID: "parent-agg",
	}
	if err := store.LogLLMRequestFull(req); err != nil {
		t.Fatalf("LogLLMRequestFull failed: %v", err)
	}

	nodes, err := store.GetWorkflowNodes("job-llm-parent")
	if err != nil {
		t.Fatalf("GetWorkflowNodes failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].ParentNodeID != "parent-agg" {
		t.Errorf("ParentNodeID = %q, want parent-agg", nodes[0].ParentNodeID)
	}
}

func TestNodeStore_LogLLMRequest_Wrapper(t *testing.T) {
	store := newTestStore(t)
	createDurableTestJob(t, store, "job-llm-wrap", "running")

	err := store.LogLLMRequest("job-llm-wrap", "n1", "gpt-4", "hi", "hello", 10, 5, 0.001, 100.0, "completed", "")
	if err != nil {
		t.Fatalf("LogLLMRequest failed: %v", err)
	}

	nodes, err := store.GetWorkflowNodes("job-llm-wrap")
	if err != nil {
		t.Fatalf("GetWorkflowNodes failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	if nodes[0].Model != "gpt-4" {
		t.Errorf("Model = %q, want gpt-4", nodes[0].Model)
	}
}

// ---------------------------------------------------------------------------
// GetNodeExecutionAttempts / GetNodeExecutionAttemptsForNode
// ---------------------------------------------------------------------------

func TestNodeStore_GetNodeExecutionAttempts_Empty(t *testing.T) {
	store := newTestStore(t)
	createDurableTestJob(t, store, "job-no-attempts", "running")

	attempts, err := store.GetNodeExecutionAttempts("job-no-attempts")
	if err != nil {
		t.Fatalf("GetNodeExecutionAttempts failed: %v", err)
	}
	if len(attempts) != 0 {
		t.Errorf("expected 0 attempts, got %d", len(attempts))
	}
}

func TestNodeStore_GetNodeExecutionAttemptsForNode(t *testing.T) {
	store := newTestStore(t)
	createDurableTestJob(t, store, "job-node-att", "running")

	// Write two attempts for different nodes via LogLLMRequestFull
	for _, nid := range []string{"n1", "n2"} {
		req := &LLMRequestLog{
			JobID:  "job-node-att",
			NodeID: nid,
			Model:  "test",
			Status: "completed",
		}
		if err := store.LogLLMRequestFull(req); err != nil {
			t.Fatalf("LogLLMRequestFull %s failed: %v", nid, err)
		}
	}

	// Query for just n1
	attempts, err := store.GetNodeExecutionAttemptsForNode("job-node-att", "n1")
	if err != nil {
		t.Fatalf("GetNodeExecutionAttemptsForNode failed: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected 1 attempt for n1, got %d", len(attempts))
	}
	if attempts[0].NodeID != "n1" {
		t.Errorf("NodeID = %q, want n1", attempts[0].NodeID)
	}
}

func TestNodeStore_GetNodeExecutionAttempts_MultipleAttempts(t *testing.T) {
	store := newTestStore(t)
	createDurableTestJob(t, store, "job-multi-att", "running")

	// Attempt 1: failed
	req1 := &LLMRequestLog{
		JobID:         "job-multi-att",
		NodeID:        "n1",
		Model:         "test",
		Status:        "failed",
		ErrMsg:        "timeout",
		AttemptNumber: 1,
	}
	if err := store.LogLLMRequestFull(req1); err != nil {
		t.Fatalf("LogLLMRequestFull attempt 1 failed: %v", err)
	}

	// Attempt 2: succeeded
	req2 := &LLMRequestLog{
		JobID:         "job-multi-att",
		NodeID:        "n1",
		Model:         "test",
		Status:        "completed",
		Response:      "ok",
		AttemptNumber: 2,
	}
	if err := store.LogLLMRequestFull(req2); err != nil {
		t.Fatalf("LogLLMRequestFull attempt 2 failed: %v", err)
	}

	attempts, err := store.GetNodeExecutionAttempts("job-multi-att")
	if err != nil {
		t.Fatalf("GetNodeExecutionAttempts failed: %v", err)
	}
	if len(attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(attempts))
	}
	// Ordered by attempt_number ASC
	if attempts[0].Attempt != 1 {
		t.Errorf("first attempt = %d, want 1", attempts[0].Attempt)
	}
	if attempts[0].Status != "failed" {
		t.Errorf("first attempt status = %q, want failed", attempts[0].Status)
	}
	if attempts[1].Attempt != 2 {
		t.Errorf("second attempt = %d, want 2", attempts[1].Attempt)
	}
	if attempts[1].Status != "completed" {
		t.Errorf("second attempt status = %q, want completed", attempts[1].Status)
	}
}

// ---------------------------------------------------------------------------
// ListNodeExecutionAttemptsByJobIDs
// ---------------------------------------------------------------------------

func TestNodeStore_ListNodeExecutionAttemptsByJobIDs(t *testing.T) {
	store := newTestStore(t)
	createDurableTestJob(t, store, "job-att-a", "running")
	createDurableTestJob(t, store, "job-att-b", "running")

	for _, jid := range []string{"job-att-a", "job-att-b"} {
		req := &LLMRequestLog{
			JobID:  jid,
			NodeID: "n1",
			Model:  "test",
			Status: "completed",
		}
		if err := store.LogLLMRequestFull(req); err != nil {
			t.Fatalf("LogLLMRequestFull %s failed: %v", jid, err)
		}
	}

	result, err := store.ListNodeExecutionAttemptsByJobIDs([]string{"job-att-a", "job-att-b"})
	if err != nil {
		t.Fatalf("ListNodeExecutionAttemptsByJobIDs failed: %v", err)
	}
	if len(result["job-att-a"]) != 1 {
		t.Errorf("expected 1 attempt for job-att-a, got %d", len(result["job-att-a"]))
	}
	if len(result["job-att-b"]) != 1 {
		t.Errorf("expected 1 attempt for job-att-b, got %d", len(result["job-att-b"]))
	}
}

func TestNodeStore_ListNodeExecutionAttemptsByJobIDs_Empty(t *testing.T) {
	store := newTestStore(t)

	result, err := store.ListNodeExecutionAttemptsByJobIDs(nil)
	if err != nil {
		t.Fatalf("ListNodeExecutionAttemptsByJobIDs failed: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty map, got %d entries", len(result))
	}
}
