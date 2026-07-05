package e2e_test

import (
	"strings"
	"testing"

	"github.com/alhasaniq/consortium/pkg/events"
)

var e2eCheapL1AggregationSources = map[string]string{
	"reasoning-informed-captain-synthesis-cheap":     "aggregation-synthesis",
	"reasoning-judge-pick-cheap":                     "aggregation-judge",
	"reasoning-judge-score-pick-cheap":               "aggregation-scoring",
	"reasoning-peer-score-pick-cheap":                "aggregation-peer-matrix",
	"reasoning-majority-pick-cheap":                  "aggregation-majority-vote",
	"reasoning-self-consistency-majority-pick-cheap": "aggregation-majority-vote",
	"reasoning-camp-split-judge-pick-cheap":          "aggregation-debate-decide",
	"reasoning-adversarial-defense-judge-pick-cheap": "aggregation-judge",
	"reasoning-multi-round-majority-pick-cheap":      "aggregation-majority-vote",
}

func TestE2EL0AggregationWorkflowViaMinimalWrapper(t *testing.T) {
	app := newE2EApp(t)
	jobID := submitCompiledAggregationWorkflowAndWait(t, app, "collect", aggregationE2EAgentPrompts("collect"), aggregationE2EConfig("collect"))
	frozen := loadFrozenDAG(t, app, jobID)

	assertNoWorkflowRefs(t, frozen)
	requireNoChildExecutions(t, app, jobID)
	requireFrozenContainsSource(t, frozen, "aggregation-collect")
}

func TestE2EL1ReasoningWorkflowUsesCompiledL0Aggregation(t *testing.T) {
	app := newE2EApp(t)
	for workflowID, sourceWorkflowID := range e2eCheapL1AggregationSources {
		t.Run(workflowID, func(t *testing.T) {
			jobID := submitStoredWorkflowAndWait(t, app, workflowID, e2eQuestionInput())
			frozen := loadFrozenDAG(t, app, jobID)
			nodes := workflowNodes(t, app, jobID)

			assertNoWorkflowRefs(t, frozen)
			assertNoLegacyAggregationPath(t, frozen, nodes)
			requireNoChildExecutions(t, app, jobID)
			requireFrozenContainsSource(t, frozen, sourceWorkflowID)
			assertCompletedNodePersistence(t, nodes)
		})
	}
}

func TestE2EL2CompositeWorkflowCompilesNestedL1AndL0(t *testing.T) {
	app := newE2EApp(t)
	jobID := submitStoredWorkflowAndWait(t, app, "composite-judge-synthesis-cheap", e2eQuestionInput())
	frozen := loadFrozenDAG(t, app, jobID)
	nodes := workflowNodes(t, app, jobID)

	assertNoWorkflowRefs(t, frozen)
	assertNoLegacyAggregationPath(t, frozen, nodes)
	requireNoChildExecutions(t, app, jobID)
	requireFrozenContainsSource(t, frozen, "reasoning-judge-pick-cheap")
	requireFrozenContainsSource(t, frozen, "reasoning-informed-captain-synthesis-cheap")
	requireFrozenContainsSource(t, frozen, "aggregation-judge")
	requireFrozenContainsSource(t, frozen, "aggregation-synthesis")

	foundNestedGeneratedID := false
	for _, node := range frozen.Nodes {
		if strings.Count(node.ID, "--") >= 2 {
			foundNestedGeneratedID = true
			break
		}
	}
	if !foundNestedGeneratedID {
		t.Fatalf("expected nested L2 -> L1 -> L0 generated IDs, got %v", frozenNodeIDs(frozen))
	}
}

func TestE2EL3BenchmarkWrapperKeepsChildWorkflowBoundary(t *testing.T) {
	app := newE2EApp(t)
	parentJobID := submitStoredWorkflowAndWait(t, app, "benchmark-informed-captain-synthesis-cheap", e2eBenchmarkInput())
	parentFrozen := loadFrozenDAG(t, app, parentJobID)
	parentNodes := workflowNodes(t, app, parentJobID)

	assertNoWorkflowRefs(t, parentFrozen)
	childNode := findFrozenNode(parentFrozen, "child-reasoning-informed-captain-synthesis-cheap")
	if childNode == nil || childNode.Type != "child_workflow" {
		t.Fatalf("parent wrapper missing child_workflow boundary: %v", frozenNodeIDs(parentFrozen))
	}

	children := requireChildExecutions(t, app, parentJobID)
	if len(children) != 1 {
		t.Fatalf("expected one child execution, got %+v", children)
	}
	child := children[0]
	if child.WorkflowID != "reasoning-informed-captain-synthesis-cheap" {
		t.Fatalf("child WorkflowID = %q, want reasoning-informed-captain-synthesis-cheap", child.WorkflowID)
	}
	if child.Status != events.JobStatusCompleted {
		t.Fatalf("child status = %q, want completed", child.Status)
	}

	childFrozen := loadFrozenDAG(t, app, child.ID)
	childNodes := workflowNodes(t, app, child.ID)
	assertNoWorkflowRefs(t, childFrozen)
	assertNoLegacyAggregationPath(t, childFrozen, childNodes)
	requireFrozenContainsSource(t, childFrozen, "aggregation-synthesis")

	var childNodeCost float64
	foundPersistedChildNode := false
	for _, node := range parentNodes {
		if node.NodeID == "child-reasoning-informed-captain-synthesis-cheap" {
			foundPersistedChildNode = true
			childNodeCost = node.Cost
			metadata := workflowNodeMetadata(t, node)
			excluded, ok := metadata["child_accounting_excluded"].(bool)
			if !ok || !excluded {
				t.Fatalf("child workflow node metadata missing child_accounting_excluded=true: %+v", metadata)
			}
		}
	}
	if !foundPersistedChildNode {
		t.Fatalf("persisted parent child_workflow node missing: %v", workflowNodeIDs(parentNodes))
	}
	if childNodeCost != 0 {
		t.Fatalf("parent child_workflow node cost = %f, want 0 to avoid double-counting child job cost", childNodeCost)
	}
}

func e2eQuestionInput() map[string]interface{} {
	return map[string]interface{}{
		"user_prompt": "Which option is correct?\nA. Alpha\nB. Beta\nC. Gamma\nD. Delta",
	}
}

func e2eBenchmarkInput() map[string]interface{} {
	return map[string]interface{}{
		"user_prompt": "Which option is correct?\nA. Alpha\nB. Beta\nC. Gamma\nD. Delta",
	}
}
