package jobs

import (
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/storage"
)

// setupNodeLoggerTest creates an in-memory storage and a storageNodeLogger backed by it.
func setupNodeLoggerTest(t *testing.T) (*storageNodeLogger, *storage.Storage) {
	t.Helper()
	store, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("failed to create in-memory storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return NewStorageNodeLogger(store), store
}

// createJobForNodeLogger creates a minimal job record so foreign-key constraints are satisfied.
func createJobForNodeLogger(t *testing.T, store *storage.Storage, jobID string) {
	t.Helper()
	job := &storage.Job{
		ID:          jobID,
		Description: "test job",
		Model:       "test-model",
		Status:      "running",
	}
	if err := store.CreateExecution(job); err != nil {
		t.Fatalf("failed to create job %q: %v", jobID, err)
	}
}

// ---------------------------------------------------------------------------
// NewStorageNodeLogger
// ---------------------------------------------------------------------------

func TestNewStorageNodeLogger_ReturnsNonNil(t *testing.T) {
	store, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("storage.NewStorage: %v", err)
	}
	defer func() { _ = store.Close() }()

	logger := NewStorageNodeLogger(store)
	if logger == nil {
		t.Fatal("NewStorageNodeLogger returned nil")
	}
	if logger.store != store {
		t.Fatal("NewStorageNodeLogger: store field not set correctly")
	}
}

// ---------------------------------------------------------------------------
// LogLLMRequestFull
// ---------------------------------------------------------------------------

func TestLogLLMRequestFull(t *testing.T) {
	tests := []struct {
		name    string
		log     *storage.LLMRequestLog
		wantErr bool
	}{
		{
			name: "full log entry persisted",
			log: &storage.LLMRequestLog{
				JobID:         "job-001",
				NodeID:        "node-a",
				Model:         "gpt-4o",
				Prompt:        "Hello world",
				Response:      "Hi there",
				TokensIn:      10,
				TokensOut:     5,
				Cost:          0.00015,
				Latency:       123.4,
				Status:        "completed",
				ErrMsg:        "",
				NodeLabel:     "LLM Call",
				NodeName:      "my-node",
				AttemptNumber: 1,
				ExecutionUID:  "job-001:node-a:1",
			},
			wantErr: false,
		},
		{
			name: "minimal log entry with auto-generated node ID",
			log: &storage.LLMRequestLog{
				JobID:  "job-002",
				Model:  "claude-3-opus",
				Status: "completed",
			},
			wantErr: false,
		},
		{
			name: "failed request logged",
			log: &storage.LLMRequestLog{
				JobID:  "job-003",
				NodeID: "node-fail",
				Model:  "gpt-4",
				Status: "failed",
				ErrMsg: "rate limit exceeded",
			},
			wantErr: false,
		},
		{
			name: "log with parent node ID",
			log: &storage.LLMRequestLog{
				JobID:        "job-004",
				NodeID:       "child-node",
				ParentNodeID: "parent-node",
				Model:        "gemini-pro",
				Status:       "completed",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, store := setupNodeLoggerTest(t)
			createJobForNodeLogger(t, store, tt.log.JobID)

			err := logger.LogLLMRequestFull(tt.log)
			if (err != nil) != tt.wantErr {
				t.Fatalf("LogLLMRequestFull() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLogLLMRequestFull_DelegatesToStorage(t *testing.T) {
	logger, store := setupNodeLoggerTest(t)

	jobID := "job-delegate-test"
	createJobForNodeLogger(t, store, jobID)

	log := &storage.LLMRequestLog{
		JobID:         jobID,
		NodeID:        "node-xyz",
		Model:         "gpt-4o",
		Prompt:        "test prompt",
		Response:      "test response",
		TokensIn:      20,
		TokensOut:     10,
		Cost:          0.0003,
		Latency:       50.0,
		Status:        "completed",
		AttemptNumber: 1,
		ExecutionUID:  "job-delegate-test:node-xyz:1",
	}

	if err := logger.LogLLMRequestFull(log); err != nil {
		t.Fatalf("LogLLMRequestFull() unexpected error: %v", err)
	}

	// Verify the node was actually persisted via storage
	nodes, err := store.GetWorkflowNodes(jobID)
	if err != nil {
		t.Fatalf("GetWorkflowNodes: %v", err)
	}
	if len(nodes) == 0 {
		t.Fatal("expected at least one node to be persisted after LogLLMRequestFull")
	}

	found := false
	for _, n := range nodes {
		if n.NodeID == "node-xyz" {
			found = true
			if n.Model != "gpt-4o" {
				t.Errorf("node.Model = %q, want %q", n.Model, "gpt-4o")
			}
		}
	}
	if !found {
		t.Errorf("node with NodeID=node-xyz not found in persisted nodes")
	}
}

// ---------------------------------------------------------------------------
// AddSubNode
// ---------------------------------------------------------------------------

func TestAddSubNode(t *testing.T) {
	now := time.Now()
	nowPtr := &now

	tests := []struct {
		name         string
		jobID        string
		runID        string
		nodeID       string
		parentNodeID string
		nodeType     string
		label        string
		nodeName     string
		output       string
		latencyMs    float64
		startedAt    *time.Time
		completedAt  *time.Time
		wantErr      bool
	}{
		{
			name:         "basic sub-node with all fields",
			jobID:        "job-sub-001",
			runID:        "run-sub-001",
			nodeID:       "subnode-a",
			parentNodeID: "parent-a",
			nodeType:     "prompt",
			label:        "Sub Prompt",
			nodeName:     "sub-prompt-node",
			output:       "some output",
			latencyMs:    123.45,
			startedAt:    nowPtr,
			completedAt:  nowPtr,
			wantErr:      false,
		},
		{
			name:         "sub-node without timing",
			jobID:        "job-sub-002",
			runID:        "run-sub-002",
			nodeID:       "subnode-b",
			parentNodeID: "",
			nodeType:     "result",
			label:        "Result",
			nodeName:     "result-node",
			output:       "final result",
			latencyMs:    0,
			startedAt:    nil,
			completedAt:  nil,
			wantErr:      false,
		},
		{
			name:         "sub-node with empty parent node ID",
			jobID:        "job-sub-003",
			runID:        "run-sub-003",
			nodeID:       "subnode-c",
			parentNodeID: "",
			nodeType:     "aggregator",
			label:        "Agg",
			nodeName:     "agg-node",
			output:       "aggregated",
			latencyMs:    55.5,
			startedAt:    nowPtr,
			completedAt:  nil,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, store := setupNodeLoggerTest(t)
			createJobForNodeLogger(t, store, tt.jobID)

			err := logger.AddSubNode(providers.SubNodeRecord{
				JobID:        tt.jobID,
				RunID:        tt.runID,
				NodeID:       tt.nodeID,
				ParentNodeID: tt.parentNodeID,
				NodeType:     tt.nodeType,
				Label:        tt.label,
				Name:         tt.nodeName,
				Output:       tt.output,
				LatencyMs:    tt.latencyMs,
				StartedAt:    tt.startedAt,
				CompletedAt:  tt.completedAt,
			})
			if (err != nil) != tt.wantErr {
				t.Fatalf("AddSubNode() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAddSubNode_PersistsCorrectFields(t *testing.T) {
	logger, store := setupNodeLoggerTest(t)

	jobID := "job-persist-fields"
	// Use empty runID so the storage layer falls back to the job's own ID,
	// which is what GetWorkflowNodes also resolves to (NULL run_id → jobID).
	runID := ""
	nodeID := "verify-node"
	parentNodeID := "parent-verify"
	nodeType := "prompt"
	label := "Verify Label"
	nodeName := "verify-name"
	output := "verify output text"
	latencyMs := 77.7
	now := time.Now().Truncate(time.Second)
	startedAt := &now
	completedAt := &now

	createJobForNodeLogger(t, store, jobID)

	if err := logger.AddSubNode(providers.SubNodeRecord{
		JobID:        jobID,
		RunID:        runID,
		NodeID:       nodeID,
		ParentNodeID: parentNodeID,
		NodeType:     nodeType,
		Label:        label,
		Name:         nodeName,
		Output:       output,
		LatencyMs:    latencyMs,
		StartedAt:    startedAt,
		CompletedAt:  completedAt,
	}); err != nil {
		t.Fatalf("AddSubNode() unexpected error: %v", err)
	}

	nodes, err := store.GetWorkflowNodes(jobID)
	if err != nil {
		t.Fatalf("GetWorkflowNodes: %v", err)
	}
	if len(nodes) == 0 {
		t.Fatal("expected at least one node to be persisted")
	}

	var found *storage.WorkflowNode
	for i := range nodes {
		if nodes[i].NodeID == nodeID {
			found = &nodes[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("node with NodeID=%q not found in persisted nodes", nodeID)
	}

	if found.ExecutionID != jobID {
		t.Errorf("ExecutionID = %q, want %q", found.ExecutionID, jobID)
	}
	if found.NodeType != nodeType {
		t.Errorf("NodeType = %q, want %q", found.NodeType, nodeType)
	}
	if found.Status != "completed" {
		t.Errorf("Status = %q, want %q", found.Status, "completed")
	}
	if found.NodeLabel != label {
		t.Errorf("NodeLabel = %q, want %q", found.NodeLabel, label)
	}
	if found.NodeName != nodeName {
		t.Errorf("NodeName = %q, want %q", found.NodeName, nodeName)
	}
	if found.Output != output {
		t.Errorf("Output = %q, want %q", found.Output, output)
	}
	if found.LatencyMs != latencyMs {
		t.Errorf("LatencyMs = %v, want %v", found.LatencyMs, latencyMs)
	}
	if found.ParentNodeID != parentNodeID {
		t.Errorf("ParentNodeID = %q, want %q", found.ParentNodeID, parentNodeID)
	}
	if found.Metadata != "{}" {
		t.Errorf("Metadata = %q, want {}", found.Metadata)
	}
	if found.AttemptNumber != 1 {
		t.Errorf("AttemptNumber = %d, want 1", found.AttemptNumber)
	}

	// ExecutionUID must follow the {jobID}:{nodeID}:1 pattern
	wantUID := jobID + ":" + nodeID + ":1"
	if found.ExecutionUID != wantUID {
		t.Errorf("ExecutionUID = %q, want %q", found.ExecutionUID, wantUID)
	}
}

func TestAddSubNode_MultipleNodes_DistinctEntries(t *testing.T) {
	logger, store := setupNodeLoggerTest(t)

	jobID := "job-multi-subnodes"
	createJobForNodeLogger(t, store, jobID)

	nodeDefs := []struct {
		nodeID string
		label  string
	}{
		{"node-1", "First"},
		{"node-2", "Second"},
		{"node-3", "Third"},
	}

	for _, n := range nodeDefs {
		// Use empty runID to align with the job's effective run_id (NULL → jobID fallback).
		if err := logger.AddSubNode(providers.SubNodeRecord{
			JobID:     jobID,
			NodeID:    n.nodeID,
			NodeType:  "prompt",
			Label:     n.label,
			Name:      n.nodeID,
			Output:    "output",
			LatencyMs: 10.0,
		}); err != nil {
			t.Fatalf("AddSubNode(%q): %v", n.nodeID, err)
		}
	}

	persisted, err := store.GetWorkflowNodes(jobID)
	if err != nil {
		t.Fatalf("GetWorkflowNodes: %v", err)
	}
	if len(persisted) != len(nodeDefs) {
		t.Errorf("got %d nodes, want %d", len(persisted), len(nodeDefs))
	}
}
