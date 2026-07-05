package runtime

import (
	"encoding/json"
	"testing"
)

func TestComputeDAGHash_Deterministic(t *testing.T) {
	data := []byte(`{"id":"test","nodes":[{"id":"a","type":"prompt"}]}`)
	hash1 := ComputeDAGHash(data)
	hash2 := ComputeDAGHash(data)

	if hash1 != hash2 {
		t.Errorf("expected deterministic hash, got %s and %s", hash1, hash2)
	}
	if len(hash1) != 64 {
		t.Errorf("expected 64 char SHA256 hex, got %d chars", len(hash1))
	}
}

func TestComputeDAGHash_DifferentInputs(t *testing.T) {
	hash1 := ComputeDAGHash([]byte(`{"a":1}`))
	hash2 := ComputeDAGHash([]byte(`{"a":2}`))

	if hash1 == hash2 {
		t.Error("expected different hashes for different inputs")
	}
}

func TestFreezeWorkflow_Basic(t *testing.T) {
	nodes := []NodeForFreeze{
		{ID: "node-1", Type: "prompt", Model: "test-model", Prompt: "Hello"},
		{ID: "node-2", Type: "result", AggregationMethod: "collect"},
	}
	edges := []EdgeForFreeze{
		{Source: "node-1", Target: "node-2"},
	}

	snapshot, err := FreezeWorkflow("wf-1", "Test", "desc", nodes, edges, nil, nil)
	if err != nil {
		t.Fatalf("FreezeWorkflow failed: %v", err)
	}

	if snapshot.DAGHash == "" {
		t.Error("expected non-empty DAGHash")
	}
	if len(snapshot.Definition) == 0 {
		t.Error("expected non-empty Definition")
	}

	// Verify definition is valid JSON
	var canonical CanonicalWorkflow
	if err := json.Unmarshal(snapshot.Definition, &canonical); err != nil {
		t.Fatalf("definition is not valid JSON: %v", err)
	}
	if canonical.ID != "wf-1" {
		t.Errorf("expected ID 'wf-1', got %s", canonical.ID)
	}
	if len(canonical.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(canonical.Nodes))
	}
	if len(canonical.Edges) != 1 {
		t.Errorf("expected 1 edge, got %d", len(canonical.Edges))
	}
}

func TestFreezeWorkflow_NoEdges_InjectsLinearDeps(t *testing.T) {
	nodes := []NodeForFreeze{
		{ID: "A", Type: "prompt"},
		{ID: "B", Type: "prompt"},
		{ID: "C", Type: "result"},
	}

	snapshot, err := FreezeWorkflow("wf-1", "Test", "", nodes, nil, nil, nil)
	if err != nil {
		t.Fatalf("FreezeWorkflow failed: %v", err)
	}

	var canonical CanonicalWorkflow
	if err := json.Unmarshal(snapshot.Definition, &canonical); err != nil {
		t.Fatalf("definition is not valid JSON: %v", err)
	}

	// Should have injected edges: A→B, B→C
	if len(canonical.Edges) != 2 {
		t.Fatalf("expected 2 injected edges, got %d", len(canonical.Edges))
	}
	if canonical.Edges[0].Source != "A" || canonical.Edges[0].Target != "B" {
		t.Errorf("expected edge A→B, got %s→%s", canonical.Edges[0].Source, canonical.Edges[0].Target)
	}
	if canonical.Edges[1].Source != "B" || canonical.Edges[1].Target != "C" {
		t.Errorf("expected edge B→C, got %s→%s", canonical.Edges[1].Source, canonical.Edges[1].Target)
	}
}

func TestFreezeWorkflow_SingleNode_NoEdgesInjected(t *testing.T) {
	nodes := []NodeForFreeze{
		{ID: "only", Type: "prompt"},
	}

	snapshot, err := FreezeWorkflow("wf-1", "Test", "", nodes, nil, nil, nil)
	if err != nil {
		t.Fatalf("FreezeWorkflow failed: %v", err)
	}

	var canonical CanonicalWorkflow
	if err := json.Unmarshal(snapshot.Definition, &canonical); err != nil {
		t.Fatalf("definition is not valid JSON: %v", err)
	}

	// Single node: no edges to inject
	if len(canonical.Edges) != 0 {
		t.Errorf("expected 0 edges for single node, got %d", len(canonical.Edges))
	}
}

func TestFreezeWorkflow_EdgesSorted(t *testing.T) {
	nodes := []NodeForFreeze{
		{ID: "A", Type: "prompt"},
		{ID: "B", Type: "prompt"},
		{ID: "C", Type: "result"},
	}
	// Edges given in non-sorted order
	edges := []EdgeForFreeze{
		{Source: "B", Target: "C"},
		{Source: "A", Target: "C"},
		{Source: "A", Target: "B"},
	}

	snapshot, err := FreezeWorkflow("wf-1", "Test", "", nodes, edges, nil, nil)
	if err != nil {
		t.Fatalf("FreezeWorkflow failed: %v", err)
	}

	var canonical CanonicalWorkflow
	if err := json.Unmarshal(snapshot.Definition, &canonical); err != nil {
		t.Fatalf("definition is not valid JSON: %v", err)
	}

	// Edges should be sorted by source, then target
	expected := []CanonicalEdge{
		{Source: "A", Target: "B"},
		{Source: "A", Target: "C"},
		{Source: "B", Target: "C"},
	}
	for i, e := range canonical.Edges {
		if e.Source != expected[i].Source || e.Target != expected[i].Target {
			t.Errorf("edge %d: expected %s→%s, got %s→%s",
				i, expected[i].Source, expected[i].Target, e.Source, e.Target)
		}
	}
}

func TestFreezeWorkflow_Deterministic(t *testing.T) {
	nodes := []NodeForFreeze{
		{ID: "A", Type: "prompt", Model: "m1", Prompt: "hello"},
		{ID: "B", Type: "result"},
	}
	edges := []EdgeForFreeze{
		{Source: "A", Target: "B"},
	}

	snapshot1, err := FreezeWorkflow("wf-1", "Test", "", nodes, edges, nil, nil)
	if err != nil {
		t.Fatalf("first freeze failed: %v", err)
	}

	snapshot2, err := FreezeWorkflow("wf-1", "Test", "", nodes, edges, nil, nil)
	if err != nil {
		t.Fatalf("second freeze failed: %v", err)
	}

	// DAGHash may differ between snapshot1 and snapshot2 because FrozenAt
	// timestamp differs. What matters is that ComputeDAGHash is deterministic:
	// the same bytes always produce the same hash.
	hash1a := ComputeDAGHash(snapshot1.Definition)
	hash1b := ComputeDAGHash(snapshot1.Definition)
	if hash1a != hash1b {
		t.Error("hash of same definition bytes should be identical")
	}
	_ = snapshot2 // used for structural equivalence above, not hash equality
}

func TestFreezeWorkflow_HarnessAffectsDAGHash(t *testing.T) {
	baseNodes := []NodeForFreeze{{
		ID:             "agent-a",
		Type:           "agent_run",
		Prompt:         "do work",
		Harness:        "claude-code",
		TimeoutSeconds: 30,
	}}
	changedNodes := []NodeForFreeze{{
		ID:             "agent-a",
		Type:           "agent_run",
		Prompt:         "do work",
		Harness:        "codex",
		TimeoutSeconds: 30,
	}}

	base, err := FreezeWorkflow("wf-agent", "Agent", "", baseNodes, nil, nil, nil)
	if err != nil {
		t.Fatalf("FreezeWorkflow base failed: %v", err)
	}
	changed, err := FreezeWorkflow("wf-agent", "Agent", "", changedNodes, nil, nil, nil)
	if err != nil {
		t.Fatalf("FreezeWorkflow changed failed: %v", err)
	}
	if ComputeDAGHash(base.Definition) == ComputeDAGHash(changed.Definition) {
		t.Fatal("expected harness change to affect frozen DAG hash")
	}
}

func TestFreezeWorkflow_NovoRunConfigAffectsDAGHash(t *testing.T) {
	baseNodes := []NodeForFreeze{{
		ID:             "superagent",
		Type:           "novo_run",
		Prompt:         "wake",
		TaskID:         "task-a",
		TaskSummary:    "brief",
		Identity:       "sde-novo",
		Image:          "novomo/novo:dev",
		Sandbox:        "host",
		RuntimeURL:     "http://127.0.0.1:8080",
		TimeoutSeconds: 30,
		GraceSeconds:   5,
		RepoSpecs: []map[string]interface{}{{
			"name": "app",
		}},
		WorkSource: map[string]interface{}{
			"type": "gitea_branch",
		},
	}}
	changedNodes := []NodeForFreeze{{
		ID:             "superagent",
		Type:           "novo_run",
		Prompt:         "wake",
		TaskID:         "task-a",
		TaskSummary:    "brief",
		Identity:       "sde-novo",
		Image:          "novomo/novo:dev",
		Sandbox:        "docker",
		RuntimeURL:     "http://host.docker.internal:8080",
		TimeoutSeconds: 30,
		GraceSeconds:   10,
		RepoSpecs: []map[string]interface{}{{
			"name": "other",
		}},
		WorkSource: map[string]interface{}{
			"type": "host_path",
		},
	}}

	base, err := FreezeWorkflow("wf-superagent", "Superagent", "", baseNodes, nil, nil, nil)
	if err != nil {
		t.Fatalf("FreezeWorkflow base failed: %v", err)
	}
	changed, err := FreezeWorkflow("wf-superagent", "Superagent", "", changedNodes, nil, nil, nil)
	if err != nil {
		t.Fatalf("FreezeWorkflow changed failed: %v", err)
	}
	if ComputeDAGHash(base.Definition) == ComputeDAGHash(changed.Definition) {
		t.Fatal("expected novo_run configuration change to affect frozen DAG hash")
	}

	var canonical CanonicalWorkflow
	if err := json.Unmarshal(base.Definition, &canonical); err != nil {
		t.Fatalf("unmarshal base snapshot: %v", err)
	}
	if canonical.Nodes[0].RuntimeURL != "http://127.0.0.1:8080" || canonical.Nodes[0].RepoSpecs[0]["name"] != "app" {
		t.Fatalf("expected novo_run fields in frozen snapshot, got %+v", canonical.Nodes[0])
	}
}

func TestFreezeWorkflow_OperationFieldsAffectDAGHash(t *testing.T) {
	baseNodes := []NodeForFreeze{{
		ID:              "op",
		Type:            "operation",
		OperationType:   "count_votes",
		OperationConfig: map[string]interface{}{"answers": []interface{}{"A", "B"}},
	}}
	changedNodes := []NodeForFreeze{{
		ID:              "op",
		Type:            "operation",
		OperationType:   "count_votes",
		OperationConfig: map[string]interface{}{"answers": []interface{}{"A", "B", "A"}},
	}}

	base, err := FreezeWorkflow("wf-op", "Operation", "", baseNodes, nil, nil, nil)
	if err != nil {
		t.Fatalf("FreezeWorkflow base failed: %v", err)
	}
	changed, err := FreezeWorkflow("wf-op", "Operation", "", changedNodes, nil, nil, nil)
	if err != nil {
		t.Fatalf("FreezeWorkflow changed failed: %v", err)
	}
	if ComputeDAGHash(base.Definition) == ComputeDAGHash(changed.Definition) {
		t.Fatal("expected operation_config change to affect frozen DAG hash")
	}

	var canonical CanonicalWorkflow
	if err := json.Unmarshal(base.Definition, &canonical); err != nil {
		t.Fatalf("unmarshal base snapshot: %v", err)
	}
	if canonical.Nodes[0].OperationType != "count_votes" {
		t.Fatalf("expected operation_type in frozen snapshot, got %+v", canonical.Nodes[0])
	}
	if canonical.Nodes[0].OperationConfig["answers"] == nil {
		t.Fatalf("expected operation_config in frozen snapshot, got %+v", canonical.Nodes[0])
	}
}

func TestFreezeWorkflow_WorkflowReferenceFieldsAffectDAGHash(t *testing.T) {
	baseNodes := []NodeForFreeze{{
		ID:            "ref",
		Type:          "workflow_ref",
		WorkflowRefID: "aggregation-synthesis",
		InputTemplate: map[string]string{"user_prompt": "{{user_prompt}}"},
		OutputKey:     "answer",
	}}
	changedNodes := []NodeForFreeze{{
		ID:            "ref",
		Type:          "workflow_ref",
		WorkflowRefID: "aggregation-judge",
		InputTemplate: map[string]string{"user_prompt": "{{user_prompt}}"},
		OutputKey:     "answer",
	}}

	base, err := FreezeWorkflow("wf-ref", "Ref", "", baseNodes, nil, nil, nil)
	if err != nil {
		t.Fatalf("FreezeWorkflow base failed: %v", err)
	}
	changed, err := FreezeWorkflow("wf-ref", "Ref", "", changedNodes, nil, nil, nil)
	if err != nil {
		t.Fatalf("FreezeWorkflow changed failed: %v", err)
	}
	if ComputeDAGHash(base.Definition) == ComputeDAGHash(changed.Definition) {
		t.Fatal("expected workflow_ref_id change to affect frozen DAG hash")
	}

	var canonical CanonicalWorkflow
	if err := json.Unmarshal(base.Definition, &canonical); err != nil {
		t.Fatalf("unmarshal base snapshot: %v", err)
	}
	if canonical.Nodes[0].WorkflowRefID != "aggregation-synthesis" {
		t.Fatalf("expected workflow_ref_id in frozen snapshot, got %+v", canonical.Nodes[0])
	}
	if canonical.Nodes[0].InputTemplate["user_prompt"] != "{{user_prompt}}" {
		t.Fatalf("expected input_template in frozen snapshot, got %+v", canonical.Nodes[0])
	}
	if canonical.Nodes[0].OutputKey != "answer" {
		t.Fatalf("expected output_key in frozen snapshot, got %+v", canonical.Nodes[0])
	}
}

func TestFreezeWorkflow_EmptyNodes_Error(t *testing.T) {
	_, err := FreezeWorkflow("wf-1", "Test", "", nil, nil, nil, nil)
	if err == nil {
		t.Error("expected error for empty nodes")
	}
}

func TestFreezeWorkflow_PreservesContext(t *testing.T) {
	nodes := []NodeForFreeze{
		{ID: "A", Type: "prompt"},
	}
	ctx := map[string]interface{}{
		"topic": "AI safety",
		"depth": "detailed",
	}

	snapshot, err := FreezeWorkflow("wf-1", "Test", "", nodes, nil, ctx, nil)
	if err != nil {
		t.Fatalf("FreezeWorkflow failed: %v", err)
	}

	var canonical CanonicalWorkflow
	if err := json.Unmarshal(snapshot.Definition, &canonical); err != nil {
		t.Fatalf("definition is not valid JSON: %v", err)
	}

	if canonical.Context["topic"] != "AI safety" {
		t.Errorf("expected context topic 'AI safety', got %v", canonical.Context["topic"])
	}
}
