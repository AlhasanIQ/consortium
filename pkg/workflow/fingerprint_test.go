package workflow

import (
	"testing"

	"github.com/alhasaniq/consortium/pkg/providers"
)

func makeTestWorkflow(nodes []*Node, edges []*Edge, limits *CostLimits) *Workflow {
	return &Workflow{
		ID:          "test-wf",
		Name:        "Test Workflow",
		Description: "A test workflow",
		Nodes:       nodes,
		Edges:       edges,
		Limits:      limits,
	}
}

func TestComputeConfigHash_Deterministic(t *testing.T) {
	wf := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt, Model: "openai/gpt-4o", Prompt: "Hello"},
	}, nil, nil)

	hash1 := ComputeConfigHash(wf)
	hash2 := ComputeConfigHash(wf)

	if hash1 == "" {
		t.Fatal("hash should not be empty")
	}
	if hash1 != hash2 {
		t.Errorf("hash not deterministic: %s != %s", hash1, hash2)
	}
}

func TestComputeConfigHash_ExplicitGraphNodeOrderIndependent(t *testing.T) {
	edges := []*Edge{{Source: "a", Target: "b"}}
	wf1 := makeTestWorkflow([]*Node{
		{ID: "a", Type: NodeTypePrompt, Model: "m1", Prompt: "p1"},
		{ID: "b", Type: NodeTypePrompt, Model: "m2", Prompt: "p2"},
	}, edges, nil)

	wf2 := makeTestWorkflow([]*Node{
		{ID: "b", Type: NodeTypePrompt, Model: "m2", Prompt: "p2"},
		{ID: "a", Type: NodeTypePrompt, Model: "m1", Prompt: "p1"},
	}, edges, nil)

	if ComputeConfigHash(wf1) != ComputeConfigHash(wf2) {
		t.Error("explicit graph hash should be independent of serialized node order")
	}
}

func TestComputeConfigHash_ImplicitSequenceDependsOnNodeOrder(t *testing.T) {
	wf1 := makeTestWorkflow([]*Node{
		{ID: "a", Type: NodeTypePrompt, Model: "m1", Prompt: "p1"},
		{ID: "b", Type: NodeTypePrompt, Model: "m2", Prompt: "p2"},
	}, nil, nil)
	wf2 := makeTestWorkflow([]*Node{
		{ID: "b", Type: NodeTypePrompt, Model: "m2", Prompt: "p2"},
		{ID: "a", Type: NodeTypePrompt, Model: "m1", Prompt: "p1"},
	}, nil, nil)

	if ComputeConfigHash(wf1) == ComputeConfigHash(wf2) {
		t.Error("node order defines execution order when explicit edges are absent")
	}
}

func TestComputeConfigHash_EdgeOrderIndependent(t *testing.T) {
	edges1 := []*Edge{
		{Source: "a", Target: "c"},
		{Source: "b", Target: "c"},
	}
	edges2 := []*Edge{
		{Source: "b", Target: "c"},
		{Source: "a", Target: "c"},
	}

	nodes := []*Node{
		{ID: "a", Type: NodeTypePrompt},
		{ID: "b", Type: NodeTypePrompt},
		{ID: "c", Type: NodeTypeResult},
	}

	wf1 := makeTestWorkflow(nodes, edges1, nil)
	wf2 := makeTestWorkflow(nodes, edges2, nil)

	if ComputeConfigHash(wf1) != ComputeConfigHash(wf2) {
		t.Error("hash should be independent of edge order")
	}
}

func TestComputeConfigHash_ExplicitVsUnsetDiffer(t *testing.T) {
	wf1 := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt, Temperature: providers.Float64Ptr(0.7), MaxTokens: 1000, TimeoutSeconds: 120},
	}, nil, nil)

	wf2 := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt},
	}, nil, nil)

	if ComputeConfigHash(wf1) == ComputeConfigHash(wf2) {
		t.Error("explicit values should differ from unset values")
	}
}

func TestComputeConfigHash_RetryPolicyAffectsExecutionIdentity(t *testing.T) {
	base := makeTestWorkflow([]*Node{{
		ID:          "s1",
		Type:        NodeTypePrompt,
		Model:       "openai/gpt-4o",
		RetryPolicy: &RetryPolicy{MaxAttempts: 1},
	}}, nil, nil)

	tests := []struct {
		name   string
		policy *RetryPolicy
	}{
		{
			name:   "attempt count",
			policy: &RetryPolicy{MaxAttempts: 3},
		},
		{
			name: "retryable errors",
			policy: &RetryPolicy{
				MaxAttempts:     1,
				RetryableErrors: []string{"RATE_LIMIT"},
			},
		},
		{
			name: "adaptive reasoning",
			policy: &RetryPolicy{
				MaxAttempts: 1,
				AdaptiveReasoning: &AdaptiveReasoningPolicy{
					ActivateAfterConsecutive: 2,
					Ladder:                   []string{"high", "low", "none"},
				},
			},
		},
	}

	baseHash := ComputeConfigHash(base)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed := makeTestWorkflow([]*Node{{
				ID:          "s1",
				Type:        NodeTypePrompt,
				Model:       "openai/gpt-4o",
				RetryPolicy: tt.policy,
			}}, nil, nil)
			if got := ComputeConfigHash(changed); got == baseHash {
				t.Fatalf("retry policy change did not affect config hash: %s", got)
			}
		})
	}
}

func TestComputeConfigHash_RuntimeSemanticsExcludeOnlyCosmeticMetadata(t *testing.T) {
	makeWorkflow := func() *Workflow {
		return makeTestWorkflow([]*Node{{
			ID:           "decision",
			Type:         NodeTypeConditional,
			Condition:    "{{answer}} == yes",
			TrueBranch:   &Node{Type: NodeTypeResult, OutputName: "accepted"},
			FalseBranch:  &Node{Type: NodeTypeResult, OutputName: "rejected"},
			OutputName:   "decision",
			OutputFormat: "text",
			Metadata: map[string]interface{}{
				"label": "Decision",
				"tools": []interface{}{map[string]interface{}{"type": "function", "name": "lookup"}},
			},
		}}, nil, nil)
	}

	base := makeWorkflow()
	baseHash := ComputeConfigHash(base)
	changes := []struct {
		name   string
		mutate func(*Node)
	}{
		{name: "true branch", mutate: func(n *Node) { n.TrueBranch.OutputName = "changed" }},
		{name: "false branch", mutate: func(n *Node) { n.FalseBranch.OutputName = "changed" }},
		{name: "output format", mutate: func(n *Node) { n.OutputFormat = "json" }},
		{name: "tools", mutate: func(n *Node) {
			n.Metadata["tools"] = []interface{}{map[string]interface{}{"type": "function", "name": "different"}}
		}},
	}
	for _, tt := range changes {
		t.Run(tt.name, func(t *testing.T) {
			changed := makeWorkflow()
			tt.mutate(changed.Nodes[0])
			if got := ComputeConfigHash(changed); got == baseHash {
				t.Fatalf("%s change did not affect config hash", tt.name)
			}
		})
	}

	cosmetic := makeWorkflow()
	cosmetic.Nodes[0].Metadata["label"] = "Renamed for display"
	cosmetic.Nodes[0].Metadata["description"] = "Cosmetic help text"
	if got := ComputeConfigHash(cosmetic); got != baseHash {
		t.Fatalf("cosmetic metadata changed config hash: %s != %s", got, baseHash)
	}
}

func TestComputeConfigHash_ExcludesContext(t *testing.T) {
	nodes := []*Node{
		{ID: "s1", Type: NodeTypePrompt, Prompt: "Hello {{name}}"},
	}

	wf1 := makeTestWorkflow(nodes, nil, nil)
	wf1.Context = map[string]interface{}{"name": "Alice"}

	wf2 := makeTestWorkflow(nodes, nil, nil)
	wf2.Context = map[string]interface{}{"name": "Bob"}

	if ComputeConfigHash(wf1) != ComputeConfigHash(wf2) {
		t.Error("context variables should not affect config hash")
	}
}

func TestComputeConfigHash_ExcludesNameDescription(t *testing.T) {
	nodes := []*Node{
		{ID: "s1", Type: NodeTypePrompt},
	}

	wf1 := makeTestWorkflow(nodes, nil, nil)
	wf1.Name = "Workflow A"
	wf1.Description = "Description A"

	wf2 := makeTestWorkflow(nodes, nil, nil)
	wf2.Name = "Workflow B"
	wf2.Description = "Description B"

	if ComputeConfigHash(wf1) != ComputeConfigHash(wf2) {
		t.Error("name and description should not affect config hash")
	}
}

func TestComputeConfigHash_DifferentModel(t *testing.T) {
	wf1 := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt, Model: "openai/gpt-4o"},
	}, nil, nil)

	wf2 := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt, Model: "anthropic/claude-3.5-sonnet"},
	}, nil, nil)

	if ComputeConfigHash(wf1) == ComputeConfigHash(wf2) {
		t.Error("different models should produce different hashes")
	}
}

func TestComputeConfigHash_DifferentTemperature(t *testing.T) {
	wf1 := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt, Temperature: providers.Float64Ptr(0.5)},
	}, nil, nil)

	wf2 := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt, Temperature: providers.Float64Ptr(0.9)},
	}, nil, nil)

	if ComputeConfigHash(wf1) == ComputeConfigHash(wf2) {
		t.Error("different temperatures should produce different hashes")
	}
}

func TestComputeConfigHash_DifferentPrompt(t *testing.T) {
	wf1 := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt, Prompt: "Tell me about cats"},
	}, nil, nil)

	wf2 := makeTestWorkflow([]*Node{
		{ID: "s1", Type: NodeTypePrompt, Prompt: "Tell me about dogs"},
	}, nil, nil)

	if ComputeConfigHash(wf1) == ComputeConfigHash(wf2) {
		t.Error("different prompts should produce different hashes")
	}
}

func TestComputeConfigHash_OperationFieldsAffectHash(t *testing.T) {
	base := makeTestWorkflow([]*Node{{
		ID:              "op",
		Type:            NodeTypeOperation,
		OperationType:   OperationCountVotes,
		OperationConfig: map[string]interface{}{"answers": []interface{}{"A", "B"}},
	}}, nil, nil)

	changedType := makeTestWorkflow([]*Node{{
		ID:              "op",
		Type:            NodeTypeOperation,
		OperationType:   OperationExtractAnswer,
		OperationConfig: map[string]interface{}{"answers": []interface{}{"A", "B"}},
	}}, nil, nil)
	if ComputeConfigHash(base) == ComputeConfigHash(changedType) {
		t.Fatal("operation_type should affect workflow config hash")
	}

	changedConfig := makeTestWorkflow([]*Node{{
		ID:              "op",
		Type:            NodeTypeOperation,
		OperationType:   OperationCountVotes,
		OperationConfig: map[string]interface{}{"answers": []interface{}{"A", "B", "A"}},
	}}, nil, nil)
	if ComputeConfigHash(base) == ComputeConfigHash(changedConfig) {
		t.Fatal("operation_config should affect workflow config hash")
	}
}

func TestComputeConfigHash_WorkflowReferenceIdentityAffectsHash(t *testing.T) {
	base := makeTestWorkflow([]*Node{{
		ID:            "ref",
		Type:          NodeTypeWorkflowRef,
		WorkflowRefID: "aggregation-synthesis",
		InputTemplate: map[string]string{"user_prompt": "{{user_prompt}}"},
		OutputKey:     "answer",
	}}, nil, nil)

	changedRef := makeTestWorkflow([]*Node{{
		ID:            "ref",
		Type:          NodeTypeWorkflowRef,
		WorkflowRefID: "aggregation-judge",
		InputTemplate: map[string]string{"user_prompt": "{{user_prompt}}"},
		OutputKey:     "answer",
	}}, nil, nil)
	if ComputeConfigHash(base) == ComputeConfigHash(changedRef) {
		t.Fatal("workflow_ref_id should affect workflow config hash")
	}

	changedSourceHash := makeTestWorkflow([]*Node{{
		ID:            "ref",
		Type:          NodeTypeWorkflowRef,
		WorkflowRefID: "aggregation-synthesis",
		InputTemplate: map[string]string{"user_prompt": "{{user_prompt}}"},
		OutputKey:     "answer",
		Metadata:      map[string]interface{}{"source_workflow_hash": "sha-b"},
	}}, nil, nil)
	base.Nodes[0].Metadata = map[string]interface{}{"source_workflow_hash": "sha-a"}
	if ComputeConfigHash(base) == ComputeConfigHash(changedSourceHash) {
		t.Fatal("source_workflow_hash should affect workflow config hash")
	}
}

func TestComputeConfigHash_DifferentAgentRunHandoff(t *testing.T) {
	wf1 := makeTestWorkflow([]*Node{
		{
			ID:             "agent-1",
			Type:           NodeTypeAgentRun,
			Prompt:         "do work",
			Harness:        "claude-code",
			Sandbox:        "docker",
			TimeoutSeconds: 600,
			InheritFrom:    &NovomoHandoffRef{Kind: NovomoHandoffKindJobRun, ID: "job-run-a"},
		},
	}, nil, nil)

	wf2 := makeTestWorkflow([]*Node{
		{
			ID:             "agent-1",
			Type:           NodeTypeAgentRun,
			Prompt:         "do work",
			Harness:        "claude-code",
			Sandbox:        "docker",
			TimeoutSeconds: 600,
			InheritFrom:    &NovomoHandoffRef{Kind: NovomoHandoffKindJobRun, ID: "job-run-b"},
		},
	}, nil, nil)

	if ComputeConfigHash(wf1) == ComputeConfigHash(wf2) {
		t.Error("different agent_run inheritance handles should produce different hashes")
	}
}

func TestComputeConfigHash_DifferentNovoRunRuntimeConfig(t *testing.T) {
	wf1 := makeTestWorkflow([]*Node{
		{
			ID:             "superagent-1",
			Type:           NodeTypeNovoRun,
			Prompt:         "investigate",
			Sandbox:        "docker",
			RuntimeURL:     "http://localhost:8090",
			TimeoutSeconds: 600,
			GraceSeconds:   10,
			WorkSource:     map[string]interface{}{"kind": "job", "id": "job-a"},
		},
	}, nil, nil)

	wf2 := makeTestWorkflow([]*Node{
		{
			ID:             "superagent-1",
			Type:           NodeTypeNovoRun,
			Prompt:         "investigate",
			Sandbox:        "host",
			RuntimeURL:     "http://localhost:8090",
			TimeoutSeconds: 600,
			GraceSeconds:   10,
			WorkSource:     map[string]interface{}{"kind": "job", "id": "job-a"},
		},
	}, nil, nil)

	if ComputeConfigHash(wf1) == ComputeConfigHash(wf2) {
		t.Error("different novo_run sandbox config should produce different hashes")
	}
}

func TestComputeConfigHash_DifferentEdges(t *testing.T) {
	nodes := []*Node{
		{ID: "a", Type: NodeTypePrompt},
		{ID: "b", Type: NodeTypePrompt},
		{ID: "c", Type: NodeTypeResult},
	}

	wf1 := makeTestWorkflow(nodes, []*Edge{
		{Source: "a", Target: "c"},
	}, nil)

	wf2 := makeTestWorkflow(nodes, []*Edge{
		{Source: "a", Target: "c"},
		{Source: "b", Target: "c"},
	}, nil)

	if ComputeConfigHash(wf1) == ComputeConfigHash(wf2) {
		t.Error("different edges should produce different hashes")
	}
}

func TestComputeConfigHash_DifferentLimits(t *testing.T) {
	nodes := []*Node{
		{ID: "s1", Type: NodeTypePrompt},
	}

	wf1 := makeTestWorkflow(nodes, nil, &CostLimits{MaxCostUSD: 1.0})
	wf2 := makeTestWorkflow(nodes, nil, &CostLimits{MaxCostUSD: 5.0})

	if ComputeConfigHash(wf1) == ComputeConfigHash(wf2) {
		t.Error("different limits should produce different hashes")
	}
}

func TestNormalizeForDisplayUsesCanonicalOrderingAndHash(t *testing.T) {
	temp := 0.25
	wf := makeTestWorkflow([]*Node{
		{ID: "z", Type: NodeTypePrompt, Model: "m-z", Temperature: &temp, MaxTokens: 128, TimeoutSeconds: 30},
		{ID: "a", Type: NodeTypeResult, AggregationMethod: AggMethodCollect},
	}, []*Edge{
		{Source: "z", Target: "a"},
	}, nil)

	display := NormalizeForDisplay(wf)
	if display.ConfigHash != ComputeConfigHash(wf) {
		t.Fatalf("display hash = %q, ComputeConfigHash = %q", display.ConfigHash, ComputeConfigHash(wf))
	}
	if len(display.Nodes) != 2 || display.Nodes[0].NodeID != "a" || display.Nodes[1].NodeID != "z" {
		t.Fatalf("display nodes are not sorted by node ID: %+v", display.Nodes)
	}
	if len(display.Edges) != 1 || display.Edges[0].Source != "z" || display.Edges[0].Target != "a" {
		t.Fatalf("display edges are not preserved in canonical form: %+v", display.Edges)
	}
	if display.Nodes[1].Temperature != temp || display.Nodes[1].MaxTokens != 128 || display.Nodes[1].TimeoutSeconds != 30 {
		t.Fatalf("display omitted effective node settings: %+v", display.Nodes[1])
	}
}

func TestComputeConfigHash_ExcludesWorkflowID(t *testing.T) {
	nodes := []*Node{
		{ID: "s1", Type: NodeTypePrompt},
	}

	wf1 := makeTestWorkflow(nodes, nil, nil)
	wf1.ID = "wf-1"

	wf2 := makeTestWorkflow(nodes, nil, nil)
	wf2.ID = "wf-2"

	if ComputeConfigHash(wf1) != ComputeConfigHash(wf2) {
		t.Error("workflow ID should not affect config hash")
	}
}

func TestComputeConfigHash_EmptyWorkflow(t *testing.T) {
	wf := makeTestWorkflow(nil, nil, nil)
	hash := ComputeConfigHash(wf)
	if hash == "" {
		t.Error("empty workflow should still produce a hash")
	}
}

func TestNormalizeNode_PreservesUnsetState(t *testing.T) {
	s := &Node{
		ID:   "s1",
		Type: NodeTypePrompt,
	}

	ns := normalizeNode(s)

	if ns.TemperatureSet {
		t.Errorf("expected TemperatureSet=false for unset value")
	}
	if ns.MaxTokensSet {
		t.Errorf("expected MaxTokensSet=false for unset value")
	}
	if ns.TimeoutSecondsSet {
		t.Errorf("expected TimeoutSecondsSet=false for unset value")
	}
	if ns.Temperature != 0 {
		t.Errorf("expected zero temperature when unset, got %f", ns.Temperature)
	}
	if ns.MaxTokens != 0 {
		t.Errorf("expected zero max_tokens when unset, got %d", ns.MaxTokens)
	}
	if ns.TimeoutSeconds != 0 {
		t.Errorf("expected zero timeout when unset, got %d", ns.TimeoutSeconds)
	}
}

func TestNormalizeNode_PreservesExplicitValues(t *testing.T) {
	s := &Node{
		ID:             "s1",
		Type:           NodeTypePrompt,
		Temperature:    providers.Float64Ptr(0.5),
		MaxTokens:      2000,
		TimeoutSeconds: 60,
	}

	ns := normalizeNode(s)

	if ns.Temperature != 0.5 {
		t.Errorf("expected temperature 0.5, got %f", ns.Temperature)
	}
	if !ns.TemperatureSet {
		t.Errorf("expected TemperatureSet=true")
	}
	if ns.MaxTokens != 2000 {
		t.Errorf("expected max_tokens 2000, got %d", ns.MaxTokens)
	}
	if !ns.MaxTokensSet {
		t.Errorf("expected MaxTokensSet=true")
	}
	if ns.TimeoutSeconds != 60 {
		t.Errorf("expected timeout 60, got %d", ns.TimeoutSeconds)
	}
	if !ns.TimeoutSecondsSet {
		t.Errorf("expected TimeoutSecondsSet=true")
	}
}
