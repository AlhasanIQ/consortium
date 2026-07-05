package compiler

import (
	"context"
	"strings"
	"testing"

	"github.com/alhasaniq/consortium/pkg/workflow"
)

func testPromptNode(id string) *workflow.Node {
	temp := 0.0
	return &workflow.Node{
		ID:             id,
		Type:           workflow.NodeTypePrompt,
		Model:          "mock-model",
		Prompt:         "Answer",
		Temperature:    &temp,
		MaxTokens:      256,
		TimeoutSeconds: 30,
		RetryPolicy:    workflow.DefaultRetryPolicy(),
	}
}

func testResultNode(id string, inputIDs []interface{}) *workflow.Node {
	return &workflow.Node{
		ID:                id,
		Type:              workflow.NodeTypeResult,
		OutputName:        "result",
		AggregationMethod: workflow.AggMethodCollect,
		RetryPolicy:       workflow.DefaultRetryPolicy(),
		Metadata: map[string]interface{}{
			"input_ids": inputIDs,
		},
	}
}

func TestCompileWorkflowRefExpandsNodesWithStableIDs(t *testing.T) {
	parent := workflow.Workflow{
		ID: "parent",
		Nodes: []*workflow.Node{{
			ID:            "agg",
			Type:          workflow.NodeTypeWorkflowRef,
			WorkflowRefID: "aggregation-collect",
		}},
	}
	child := workflow.Workflow{
		ID:    "aggregation-collect",
		Nodes: []*workflow.Node{testPromptNode("join")},
	}
	resolver := StaticResolver{"aggregation-collect": child}
	compiled, report, err := Compile(context.Background(), &parent, resolver)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	if len(compiled.Nodes) != 1 || compiled.Nodes[0].ID != "agg--join" {
		t.Fatalf("expanded node IDs = %+v", compiled.Nodes)
	}
	if compiled.Nodes[0].Metadata["source_workflow_id"] != "aggregation-collect" {
		t.Fatalf("missing source workflow metadata: %+v", compiled.Nodes[0].Metadata)
	}
	if compiled.Nodes[0].Metadata["source_workflow_hash"] == "" {
		t.Fatalf("missing source workflow hash: %+v", compiled.Nodes[0].Metadata)
	}
	if compiled.Nodes[0].Metadata["source_node_id"] != "join" {
		t.Fatalf("missing source node metadata: %+v", compiled.Nodes[0].Metadata)
	}
	if len(report.ExpandedRefs) != 1 {
		t.Fatalf("expanded refs = %+v", report.ExpandedRefs)
	}
}

func TestCompileNoWorkflowRefsIsNoop(t *testing.T) {
	parent := workflow.Workflow{
		ID:    "parent",
		Nodes: []*workflow.Node{testPromptNode("agent")},
	}
	compiled, report, err := Compile(context.Background(), &parent, StaticResolver{})
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	if compiled == &parent {
		t.Fatal("Compile should return a cloned workflow")
	}
	if len(compiled.Nodes) != 1 || compiled.Nodes[0].ID != "agent" {
		t.Fatalf("unexpected nodes: %+v", compiled.Nodes)
	}
	if len(report.ExpandedRefs) != 0 {
		t.Fatalf("expanded refs = %+v", report.ExpandedRefs)
	}
}

func TestCompilePreservesExplicitRuntimeChildWorkflow(t *testing.T) {
	parent := workflow.Workflow{
		ID: "parent",
		Nodes: []*workflow.Node{{
			ID:              "child",
			Type:            workflow.NodeTypeChildWorkflow,
			ChildWorkflowID: "reasoning-informed-captain-synthesis",
		}},
	}
	compiled, report, err := Compile(context.Background(), &parent, StaticResolver{})
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	if compiled.Nodes[0].Type != workflow.NodeTypeChildWorkflow {
		t.Fatalf("child_workflow was unexpectedly changed: %+v", compiled.Nodes[0])
	}
	if len(report.ChildRefs) != 1 || report.ChildRefs[0].WorkflowID != "reasoning-informed-captain-synthesis" {
		t.Fatalf("child refs = %+v", report.ChildRefs)
	}
}

func TestCompileWorkflowRefTargetsAnyLayerByDefault(t *testing.T) {
	parent := workflow.Workflow{
		ID: "parent",
		Nodes: []*workflow.Node{{
			ID:            "reasoning",
			Type:          workflow.NodeTypeWorkflowRef,
			WorkflowRefID: "reasoning-informed-captain-synthesis",
		}},
	}
	child := workflow.Workflow{
		ID:    "reasoning-informed-captain-synthesis",
		Nodes: []*workflow.Node{testPromptNode("agent-a")},
	}
	compiled, _, err := Compile(context.Background(), &parent, StaticResolver{"reasoning-informed-captain-synthesis": child})
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	if len(compiled.Nodes) != 1 || compiled.Nodes[0].ID != "reasoning--agent-a" {
		t.Fatalf("workflow_ref did not compile into parent DAG: %+v", compiled.Nodes)
	}
	if compiled.Nodes[0].Type != workflow.NodeTypePrompt {
		t.Fatalf("expanded node type = %q", compiled.Nodes[0].Type)
	}
}

func TestCompileRejectsMissingReference(t *testing.T) {
	parent := workflow.Workflow{
		ID: "parent",
		Nodes: []*workflow.Node{{
			ID:            "agg",
			Type:          workflow.NodeTypeWorkflowRef,
			WorkflowRefID: "missing",
		}},
	}
	_, _, err := Compile(context.Background(), &parent, StaticResolver{})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected missing reference error, got %v", err)
	}
}

func TestCompileRejectsCycles(t *testing.T) {
	parent := workflow.Workflow{
		ID: "parent",
		Nodes: []*workflow.Node{{
			ID:            "self",
			Type:          workflow.NodeTypeWorkflowRef,
			WorkflowRefID: "parent",
		}},
	}
	_, _, err := Compile(context.Background(), &parent, StaticResolver{"parent": parent})
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle error, got %v", err)
	}
}

func TestCompileRewritesParentIncomingAndOutgoingEdges(t *testing.T) {
	parent := workflow.Workflow{
		ID: "parent",
		Nodes: []*workflow.Node{
			testPromptNode("upstream"),
			{ID: "agg", Type: workflow.NodeTypeWorkflowRef, WorkflowRefID: "aggregation-collect"},
			testResultNode("final", []interface{}{"agg"}),
		},
		Edges: []*workflow.Edge{
			{ID: "e1", Source: "upstream", Target: "agg"},
			{ID: "e2", Source: "agg", Target: "final"},
		},
	}
	child := workflow.Workflow{
		ID: "aggregation-collect",
		Nodes: []*workflow.Node{
			testPromptNode("extract"),
			testPromptNode("join"),
		},
		Edges: []*workflow.Edge{{ID: "child-edge", Source: "extract", Target: "join"}},
	}
	compiled, _, err := Compile(context.Background(), &parent, StaticResolver{"aggregation-collect": child})
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	wantEdges := map[string]bool{
		"upstream->agg--extract":  true,
		"agg--extract->agg--join": true,
		"agg--join->final":        true,
	}
	if len(compiled.Edges) != len(wantEdges) {
		t.Fatalf("edges = %+v", compiled.Edges)
	}
	for _, edge := range compiled.Edges {
		key := edge.Source + "->" + edge.Target
		if !wantEdges[key] {
			t.Fatalf("unexpected edge %s in %+v", key, compiled.Edges)
		}
		delete(wantEdges, key)
	}
	if got := compiled.Nodes[len(compiled.Nodes)-1].Metadata["input_ids"]; !containsInputID(got, "agg--join") {
		t.Fatalf("result input_ids not rewritten: %+v", got)
	}
}

func TestCompileExpandsNestedWorkflowRefs(t *testing.T) {
	parent := workflow.Workflow{
		ID:    "parent",
		Nodes: []*workflow.Node{{ID: "outer", Type: workflow.NodeTypeWorkflowRef, WorkflowRefID: "l1"}},
	}
	l1 := workflow.Workflow{
		ID:    "l1",
		Nodes: []*workflow.Node{{ID: "inner", Type: workflow.NodeTypeWorkflowRef, WorkflowRefID: "l0"}},
	}
	l0 := workflow.Workflow{
		ID:    "l0",
		Nodes: []*workflow.Node{testPromptNode("leaf")},
	}
	compiled, _, err := Compile(context.Background(), &parent, StaticResolver{"l1": l1, "l0": l0})
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	if len(compiled.Nodes) != 1 || compiled.Nodes[0].ID != "outer--inner--leaf" {
		t.Fatalf("nested node IDs = %+v", compiled.Nodes)
	}
	if got := compiled.Nodes[0].Metadata["source_workflow_id"]; got != "l0" {
		t.Fatalf("source workflow metadata = %q", got)
	}
}

func TestCompileRejectsDuplicateGeneratedIDs(t *testing.T) {
	parent := workflow.Workflow{
		ID: "parent",
		Nodes: []*workflow.Node{
			{ID: "agg", Type: workflow.NodeTypeWorkflowRef, WorkflowRefID: "aggregation-collect"},
			testPromptNode("agg--join"),
		},
	}
	child := workflow.Workflow{
		ID:    "aggregation-collect",
		Nodes: []*workflow.Node{testPromptNode("join")},
	}
	_, _, err := Compile(context.Background(), &parent, StaticResolver{"aggregation-collect": child})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate generated ID error, got %v", err)
	}
}

func TestCompileRejectsGeneratedIDsWithReservedDelimiter(t *testing.T) {
	parent := workflow.Workflow{
		ID:    "parent",
		Nodes: []*workflow.Node{{ID: "agg", Type: workflow.NodeTypeWorkflowRef, WorkflowRefID: "aggregation-collect"}},
	}
	child := workflow.Workflow{
		ID:    "aggregation-collect",
		Nodes: []*workflow.Node{testPromptNode("bad__id")},
	}
	_, _, err := Compile(context.Background(), &parent, StaticResolver{"aggregation-collect": child})
	if err == nil || !strings.Contains(err.Error(), "__") {
		t.Fatalf("expected reserved delimiter error, got %v", err)
	}
}

func TestCompileRejectsEmptyReferencedWorkflow(t *testing.T) {
	parent := workflow.Workflow{
		ID:    "parent",
		Nodes: []*workflow.Node{{ID: "agg", Type: workflow.NodeTypeWorkflowRef, WorkflowRefID: "empty"}},
	}
	child := workflow.Workflow{ID: "empty"}
	_, _, err := Compile(context.Background(), &parent, StaticResolver{"empty": child})
	if err == nil || !strings.Contains(err.Error(), "no nodes") {
		t.Fatalf("expected empty workflow error, got %v", err)
	}
}

func TestCompileRewritesExpandedInheritFromNodeID(t *testing.T) {
	parent := workflow.Workflow{
		ID:    "parent",
		Nodes: []*workflow.Node{{ID: "agent-group", Type: workflow.NodeTypeWorkflowRef, WorkflowRefID: "agent-pair"}},
	}
	child := workflow.Workflow{
		ID: "agent-pair",
		Nodes: []*workflow.Node{
			{ID: "source", Type: workflow.NodeTypeAgentRun},
			{ID: "sink", Type: workflow.NodeTypeAgentRun, InheritFromNodeID: "source"},
		},
		Edges: []*workflow.Edge{{ID: "source-sink", Source: "source", Target: "sink"}},
	}
	compiled, _, err := Compile(context.Background(), &parent, StaticResolver{"agent-pair": child})
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	var sink *workflow.Node
	for _, node := range compiled.Nodes {
		if node.ID == "agent-group--sink" {
			sink = node
			break
		}
	}
	if sink == nil {
		t.Fatalf("expanded sink node missing: %+v", compiled.Nodes)
	}
	if sink.InheritFromNodeID != "agent-group--source" {
		t.Fatalf("InheritFromNodeID = %q, want expanded source", sink.InheritFromNodeID)
	}
}

func TestCompileRewritesConditionalBranchReferences(t *testing.T) {
	parent := workflow.Workflow{
		ID:    "parent",
		Nodes: []*workflow.Node{{ID: "group", Type: workflow.NodeTypeWorkflowRef, WorkflowRefID: "conditional-child"}},
	}
	child := workflow.Workflow{
		ID: "conditional-child",
		Nodes: []*workflow.Node{
			testPromptNode("source"),
			{
				ID:        "router",
				Type:      workflow.NodeTypeConditional,
				Condition: "{{source}} contains yes",
				TrueBranch: &workflow.Node{
					ID:     "true",
					Type:   workflow.NodeTypePrompt,
					Prompt: "Use {{source}}",
				},
			},
		},
		Edges: []*workflow.Edge{{ID: "source-router", Source: "source", Target: "router"}},
	}
	compiled, _, err := Compile(context.Background(), &parent, StaticResolver{"conditional-child": child})
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	var router *workflow.Node
	for _, node := range compiled.Nodes {
		if node.ID == "group--router" {
			router = node
			break
		}
	}
	if router == nil || router.TrueBranch == nil {
		t.Fatalf("expanded router branch missing: %+v", compiled.Nodes)
	}
	if router.Condition != "{{group--source}} contains yes" {
		t.Fatalf("condition = %q", router.Condition)
	}
	if router.TrueBranch.Prompt != "Use {{group--source}}" {
		t.Fatalf("branch prompt = %q", router.TrueBranch.Prompt)
	}
}

func TestCompileRejectsAmbiguousPromptReferenceToMultiTerminalRef(t *testing.T) {
	parent := workflow.Workflow{
		ID: "parent",
		Nodes: []*workflow.Node{
			{ID: "agg", Type: workflow.NodeTypeWorkflowRef, WorkflowRefID: "fanout"},
			testPromptNode("downstream"),
		},
		Edges: []*workflow.Edge{{ID: "agg-downstream", Source: "agg", Target: "downstream"}},
	}
	parent.Nodes[1].Prompt = "Combine {{agg}}"
	child := workflow.Workflow{
		ID: "fanout",
		Nodes: []*workflow.Node{
			testPromptNode("root"),
			testPromptNode("left"),
			testPromptNode("right"),
		},
		Edges: []*workflow.Edge{
			{ID: "root-left", Source: "root", Target: "left"},
			{ID: "root-right", Source: "root", Target: "right"},
		},
	}
	_, _, err := Compile(context.Background(), &parent, StaticResolver{"fanout": child})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous prompt reference error, got %v", err)
	}
}

func TestCompileRejectsAmbiguousPromptReferenceWithRuntimeWhitespace(t *testing.T) {
	parent := workflow.Workflow{
		ID: "parent",
		Nodes: []*workflow.Node{
			{ID: "agg", Type: workflow.NodeTypeWorkflowRef, WorkflowRefID: "fanout"},
			testPromptNode("downstream"),
		},
		Edges: []*workflow.Edge{{ID: "agg-downstream", Source: "agg", Target: "downstream"}},
	}
	parent.Nodes[1].Prompt = "Combine {{  agg\t}}"
	child := workflow.Workflow{
		ID: "fanout",
		Nodes: []*workflow.Node{
			testPromptNode("root"),
			testPromptNode("left"),
			testPromptNode("right"),
		},
		Edges: []*workflow.Edge{
			{ID: "root-left", Source: "root", Target: "left"},
			{ID: "root-right", Source: "root", Target: "right"},
		},
	}
	_, _, err := Compile(context.Background(), &parent, StaticResolver{"fanout": child})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous prompt reference error, got %v", err)
	}
}

func TestCompileRewritesAggregationConfigReferences(t *testing.T) {
	parent := workflow.Workflow{
		ID: "parent",
		Nodes: []*workflow.Node{
			{ID: "agg", Type: workflow.NodeTypeWorkflowRef, WorkflowRefID: "aggregation-collect"},
			testResultNode("final", []interface{}{"agg"}),
		},
		Edges: []*workflow.Edge{{ID: "agg-final", Source: "agg", Target: "final"}},
	}
	parent.Nodes[1].AggregationConfig = map[string]interface{}{
		"prompt":      "Judge {{ agg }} from {{agg__model}}",
		"nested_list": []interface{}{"{{ agg }}"},
	}
	child := workflow.Workflow{
		ID:    "aggregation-collect",
		Nodes: []*workflow.Node{testPromptNode("join")},
	}
	compiled, _, err := Compile(context.Background(), &parent, StaticResolver{"aggregation-collect": child})
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	final := compiled.Nodes[len(compiled.Nodes)-1]
	if got := final.AggregationConfig["prompt"]; got != "Judge {{agg--join}} from {{agg--join__model}}" {
		t.Fatalf("aggregation prompt = %#v", got)
	}
	nested, ok := final.AggregationConfig["nested_list"].([]interface{})
	if !ok || len(nested) != 1 || nested[0] != "{{agg--join}}" {
		t.Fatalf("nested aggregation config not rewritten: %#v", final.AggregationConfig["nested_list"])
	}
}

func TestCompileRewritesParentWorkflowRefTokensInjectedByInputTemplate(t *testing.T) {
	parent := workflow.Workflow{
		ID: "parent",
		Nodes: []*workflow.Node{
			{ID: "other", Type: workflow.NodeTypeWorkflowRef, WorkflowRefID: "producer"},
			{
				ID:            "consumer",
				Type:          workflow.NodeTypeWorkflowRef,
				WorkflowRefID: "consumer",
				InputTemplate: map[string]string{"upstream": "{{ other }}"},
			},
		},
		Edges: []*workflow.Edge{{ID: "other-consumer", Source: "other", Target: "consumer"}},
	}
	producer := workflow.Workflow{
		ID:    "producer",
		Nodes: []*workflow.Node{testPromptNode("out")},
	}
	consumer := workflow.Workflow{
		ID:    "consumer",
		Nodes: []*workflow.Node{testPromptNode("ask")},
	}
	consumer.Nodes[0].Prompt = "Use {{upstream}}"

	compiled, _, err := Compile(context.Background(), &parent, StaticResolver{
		"producer": producer,
		"consumer": consumer,
	})
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	var ask *workflow.Node
	for _, node := range compiled.Nodes {
		if node.ID == "consumer--ask" {
			ask = node
			break
		}
	}
	if ask == nil {
		t.Fatalf("expanded consumer node missing: %+v", compiled.Nodes)
	}
	if ask.Prompt != "Use {{other--out}}" {
		t.Fatalf("prompt = %q", ask.Prompt)
	}
}

func containsInputID(value interface{}, id string) bool {
	switch typed := value.(type) {
	case []interface{}:
		for _, item := range typed {
			if item == id {
				return true
			}
		}
	case []string:
		for _, item := range typed {
			if item == id {
				return true
			}
		}
	}
	return false
}
