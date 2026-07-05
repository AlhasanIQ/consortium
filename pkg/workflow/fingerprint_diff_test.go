package workflow

import (
	"testing"

	"github.com/alhasaniq/consortium/pkg/providers"
)

func TestDiffWorkflows_Identical(t *testing.T) {
	wf := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt, Model: "m1", Prompt: "Hello"},
	}, nil, nil)

	diff := DiffWorkflows(wf, wf)

	if !diff.Identical {
		t.Error("identical workflows should produce Identical=true")
	}
	if len(diff.Diffs) != 0 {
		t.Errorf("expected 0 diffs, got %d", len(diff.Diffs))
	}
	if diff.LeftHash != diff.RightHash {
		t.Error("hashes should match for identical workflows")
	}
}

func TestDiffWorkflows_ExplicitVsUnsetDiffer(t *testing.T) {
	wfA := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt, Temperature: providers.Float64Ptr(0.7), MaxTokens: 1000},
	}, nil, nil)

	wfB := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt},
	}, nil, nil)

	diff := DiffWorkflows(wfA, wfB)

	if diff.Identical {
		t.Error("workflows with explicit vs unset values should differ")
	}
}

func TestDiffWorkflows_ModelChange(t *testing.T) {
	wfA := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt, Model: "openai/gpt-4o"},
	}, nil, nil)

	wfB := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt, Model: "anthropic/claude-3.5-sonnet"},
	}, nil, nil)

	diff := DiffWorkflows(wfA, wfB)

	if diff.Identical {
		t.Error("should not be identical")
	}

	found := findDiff(diff.Diffs, "nodes.s1.model")
	if found == nil {
		t.Fatal("expected model diff")
	}
	if found.Category != "model" {
		t.Errorf("expected category 'model', got %q", found.Category)
	}
	if found.Left != "openai/gpt-4o" {
		t.Errorf("expected left 'openai/gpt-4o', got %v", found.Left)
	}
	if found.Right != "anthropic/claude-3.5-sonnet" {
		t.Errorf("expected right 'anthropic/claude-3.5-sonnet', got %v", found.Right)
	}
}

func TestDiffWorkflows_TemperatureChange(t *testing.T) {
	wfA := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt, Temperature: providers.Float64Ptr(0.5)},
	}, nil, nil)

	wfB := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt, Temperature: providers.Float64Ptr(0.9)},
	}, nil, nil)

	diff := DiffWorkflows(wfA, wfB)

	found := findDiff(diff.Diffs, "nodes.s1.temperature")
	if found == nil {
		t.Fatal("expected temperature diff")
	}
	if found.Category != "decoding" {
		t.Errorf("expected category 'decoding', got %q", found.Category)
	}
}

func TestDiffWorkflows_PromptChange(t *testing.T) {
	wfA := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt, Prompt: "Hello cats"},
	}, nil, nil)

	wfB := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt, Prompt: "Hello dogs"},
	}, nil, nil)

	diff := DiffWorkflows(wfA, wfB)

	found := findDiff(diff.Diffs, "nodes.s1.prompt")
	if found == nil {
		t.Fatal("expected prompt diff")
	}
	if found.Category != "prompt" {
		t.Errorf("expected category 'prompt', got %q", found.Category)
	}
}

func TestDiffWorkflows_SystemPromptChange(t *testing.T) {
	wfA := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt, SystemPrompt: "You are helpful"},
	}, nil, nil)

	wfB := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt, SystemPrompt: "You are concise"},
	}, nil, nil)

	diff := DiffWorkflows(wfA, wfB)

	found := findDiff(diff.Diffs, "nodes.s1.system_prompt")
	if found == nil {
		t.Fatal("expected system_prompt diff")
	}
	if found.Category != "prompt" {
		t.Errorf("expected category 'prompt', got %q", found.Category)
	}
}

func TestDiffWorkflows_NodeAdded(t *testing.T) {
	wfA := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt},
	}, nil, nil)

	wfB := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt},
		{ID: "s2", Type: NodeTypePrompt},
	}, nil, nil)

	diff := DiffWorkflows(wfA, wfB)

	found := findDiff(diff.Diffs, "nodes.s2")
	if found == nil {
		t.Fatal("expected node addition diff")
	}
	if found.Category != "structure" {
		t.Errorf("expected category 'structure', got %q", found.Category)
	}
	if found.Left != nil {
		t.Error("left should be nil for added node")
	}
}

func TestDiffWorkflows_NodeRemoved(t *testing.T) {
	wfA := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt},
		{ID: "s2", Type: NodeTypePrompt},
	}, nil, nil)

	wfB := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt},
	}, nil, nil)

	diff := DiffWorkflows(wfA, wfB)

	found := findDiff(diff.Diffs, "nodes.s2")
	if found == nil {
		t.Fatal("expected node removal diff")
	}
	if found.Right != nil {
		t.Error("right should be nil for removed node")
	}
}

func TestDiffWorkflows_EdgeChange(t *testing.T) {
	nodes := []*Node{
		{ID: "a", Type: NodeTypePrompt},
		{ID: "b", Type: NodeTypePrompt},
		{ID: "c", Type: NodeTypeResult},
	}

	wfA := makeTestWorkflow(nodes, []*Edge{
		{Source: "a", Target: "c"},
	}, nil)

	wfB := makeTestWorkflow(nodes, []*Edge{
		{Source: "a", Target: "c"},
		{Source: "b", Target: "c"},
	}, nil)

	diff := DiffWorkflows(wfA, wfB)

	if diff.Identical {
		t.Error("should not be identical")
	}

	hasStructureDiff := false
	for _, d := range diff.Diffs {
		if d.Category == "structure" {
			hasStructureDiff = true
			break
		}
	}
	if !hasStructureDiff {
		t.Error("expected structure category diff for edge change")
	}
}

func TestDiffWorkflows_LimitsChange(t *testing.T) {
	nodes := []*Node{
		{ID: "s1", Type: NodeTypePrompt},
	}

	wfA := makeTestWorkflow(nodes, nil, &CostLimits{MaxCostUSD: 1.0})
	wfB := makeTestWorkflow(nodes, nil, &CostLimits{MaxCostUSD: 5.0})

	diff := DiffWorkflows(wfA, wfB)

	found := findDiff(diff.Diffs, "limits.max_cost_usd")
	if found == nil {
		t.Fatal("expected limits diff")
	}
	if found.Category != "limits" {
		t.Errorf("expected category 'limits', got %q", found.Category)
	}
}

func TestDiffWorkflows_AggregationChange(t *testing.T) {
	wfA := makeTestWorkflow([]*Node{
		{ID: "out", Type: NodeTypeResult, AggregationMethod: AggMethodCollect},
	}, nil, nil)

	wfB := makeTestWorkflow([]*Node{
		{ID: "out", Type: NodeTypeResult, AggregationMethod: AggMethodJudge},
	}, nil, nil)

	diff := DiffWorkflows(wfA, wfB)

	found := findDiff(diff.Diffs, "nodes.out.aggregation_method")
	if found == nil {
		t.Fatal("expected aggregation_method diff")
	}
	if found.Category != "aggregation" {
		t.Errorf("expected category 'aggregation', got %q", found.Category)
	}
}

func TestDiffWorkflows_OperationChange(t *testing.T) {
	wfA := makeTestWorkflow([]*Node{{
		ID:              "op",
		Type:            NodeTypeOperation,
		OperationType:   OperationCountVotes,
		OperationConfig: map[string]interface{}{"answers": []interface{}{"A", "B"}},
	}}, nil, nil)

	wfB := makeTestWorkflow([]*Node{{
		ID:              "op",
		Type:            NodeTypeOperation,
		OperationType:   OperationReduceScores,
		OperationConfig: map[string]interface{}{"scores": map[string]interface{}{"a": []interface{}{1, 0}}},
	}}, nil, nil)

	diff := DiffWorkflows(wfA, wfB)

	if found := findDiff(diff.Diffs, "nodes.op.operation_type"); found == nil {
		t.Fatal("expected operation_type diff")
	} else if found.Category != "operation" {
		t.Fatalf("expected operation category, got %q", found.Category)
	}
	if found := findDiff(diff.Diffs, "nodes.op.operation_config"); found == nil {
		t.Fatal("expected operation_config diff")
	} else if found.Category != "operation" {
		t.Fatalf("expected operation category, got %q", found.Category)
	}
}

func TestDiffWorkflows_SourceWorkflowHashChange(t *testing.T) {
	wfA := makeTestWorkflow([]*Node{{
		ID:       "compiled_node",
		Type:     NodeTypePrompt,
		Metadata: map[string]interface{}{"source_workflow_hash": "sha-a"},
	}}, nil, nil)
	wfB := makeTestWorkflow([]*Node{{
		ID:       "compiled_node",
		Type:     NodeTypePrompt,
		Metadata: map[string]interface{}{"source_workflow_hash": "sha-b"},
	}}, nil, nil)

	diff := DiffWorkflows(wfA, wfB)

	found := findDiff(diff.Diffs, "nodes.compiled_node.source_workflow_hash")
	if found == nil {
		t.Fatal("expected source_workflow_hash diff")
	}
	if found.Category != "composition" {
		t.Fatalf("expected composition category, got %q", found.Category)
	}
}

func TestDiffWorkflows_MultipleChanges(t *testing.T) {
	wfA := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt, Model: "m1", Temperature: providers.Float64Ptr(0.5), Prompt: "Hello"},
	}, nil, nil)

	wfB := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt, Model: "m2", Temperature: providers.Float64Ptr(0.9), Prompt: "World"},
	}, nil, nil)

	diff := DiffWorkflows(wfA, wfB)

	if diff.Identical {
		t.Error("should not be identical")
	}
	if len(diff.Diffs) != 3 {
		t.Errorf("expected 3 diffs (model, temperature, prompt), got %d", len(diff.Diffs))
	}
}

func TestDiffWorkflows_HashesPopulated(t *testing.T) {
	wfA := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt, Model: "m1"},
	}, nil, nil)

	wfB := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt, Model: "m2"},
	}, nil, nil)

	diff := DiffWorkflows(wfA, wfB)

	if diff.LeftHash == "" || diff.RightHash == "" {
		t.Error("hashes should be populated")
	}
	if diff.LeftHash == diff.RightHash {
		t.Error("different workflows should have different hashes")
	}
}

// findDiff finds a FieldDiff by path.
func findDiff(diffs []FieldDiff, path string) *FieldDiff {
	for i := range diffs {
		if diffs[i].Path == path {
			return &diffs[i]
		}
	}
	return nil
}
