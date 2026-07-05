package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/storage"
	workflowruntime "github.com/alhasaniq/consortium/pkg/workflow/runtime"
	"github.com/google/uuid"
)

func TestAggregationWorkflowSharedAssertions(t *testing.T) {
	app := newE2EApp(t)
	jobID := submitCompiledAggregationWorkflowAndWait(t, app, "synthesis", aggregationE2EAgentPrompts("synthesis"), aggregationE2EConfig("synthesis"))

	frozen := loadFrozenDAG(t, app, jobID)
	nodes := workflowNodes(t, app, jobID)
	assertNoWorkflowRefs(t, frozen)
	assertNoLegacyAggregationPath(t, frozen, nodes)
	assertExpandedAggregation(t, frozen, nodes, "agg-1", "aggregation-synthesis", "synthesis")
	assertCompletedNodePersistence(t, nodes)
}

func TestE2EAggregationCollectWorkflow(t *testing.T) {
	app, jobID := runE2EAggregationWorkflow(t, "collect", []string{
		"Answer: A\nCollect first.",
		"Answer: B\nCollect second.",
	}, aggregationE2EConfig("collect"))
	frozen := loadFrozenDAG(t, app, jobID)
	nodes := workflowNodes(t, app, jobID)

	collect := requireWorkflowNode(t, nodes, "agg-1--collect-inputs")
	requireStringOrder(t, collect.Output, []string{"Collect first.", "Collect second."})
	if strings.Contains(collect.Output, "Collect second.") && strings.Index(collect.Output, "Collect second.") < strings.Index(collect.Output, "Collect first.") {
		t.Fatalf("collect output order reversed: %q", collect.Output)
	}
	collectFrozen := requireFrozenNode(t, frozen, "agg-1--collect-inputs")
	requireInputIDs(t, collectFrozen.OperationConfig["input_ids"], []string{"agent-1", "agent-2"})
	for _, nodeID := range []string{"agent-1", "agent-2"} {
		node := requireWorkflowNode(t, nodes, nodeID)
		if node.Model != "mock/fast" {
			t.Fatalf("%s model = %q, want mock/fast", nodeID, node.Model)
		}
	}
	for _, node := range nodes {
		if node.ParentNodeID == "agg-1--result" && node.NodeType == "prompt" {
			t.Fatalf("collect should not call an LLM aggregation node, found %+v", node)
		}
	}
}

func TestE2EAggregationMajorityVoteWorkflow(t *testing.T) {
	app, jobID := runE2EAggregationWorkflow(t, "majority_vote", aggregationE2EAgentPrompts("majority_vote"), aggregationE2EConfig("majority_vote"))
	nodes := workflowNodes(t, app, jobID)
	requireNodeOutputContains(t, nodes, "agg-1--select-winner", "Answer: B")
	requireNodeOutputContains(t, nodes, "agg-1--result", "Answer: B")

	t.Run("tie path uses visible synthesis branch", func(t *testing.T) {
		app := newE2EApp(t)
		config := aggregationE2EConfig("majority_vote")
		config["tie_breaker_method"] = "synthesis"
		config["tie_breaker_model"] = "mock/fast"
		config["tie_breaker_temperature"] = 0.0
		config["system_prompt"] = "Synthesize the tied answers."
		config["prompt"] = "{{responses}}"
		config["max_tokens"] = 128
		jobID := submitCompiledAggregationWorkflowAndWait(t, app, "majority_vote", []string{
			"Answer: A\nReasoning one.",
			"Answer: B\nReasoning two.",
		}, config)
		frozen := loadFrozenDAG(t, app, jobID)
		nodes := workflowNodes(t, app, jobID)
		if findFrozenNode(frozen, "agg-1--tie-policy") == nil {
			t.Fatalf("tie-policy conditional missing from frozen DAG: %s", debugWorkflowShape(frozen))
		}
		foundBranch := false
		for _, node := range nodes {
			if node.NodeID == "agg-1--tie-policy_true" {
				foundBranch = true
				if node.ParentNodeID != "agg-1--result" {
					t.Fatalf("tie branch ParentNodeID = %q, want agg-1--result", node.ParentNodeID)
				}
			}
		}
		if !foundBranch {
			t.Fatalf("tie synthesis branch row missing from persisted nodes: %v", workflowNodeIDs(nodes))
		}
		requireNodeOutputContains(t, nodes, "agg-1--result", "deterministic:")
	})

	t.Run("self consistency repeated samples map to one answer", func(t *testing.T) {
		app := newE2EApp(t)
		wf := compiledAggregationWorkflowFile("e2e-self-consistency-"+uuid.NewString(), "majority_vote", []string{
			"Answer: C\nSample one.",
			"Answer: C\nSample two.",
			"Answer: A\nSample three.",
		}, aggregationE2EConfig("majority_vote"))
		setAgentModel(t, wf, "agent-1", "mock/slow")
		setAgentModel(t, wf, "agent-2", "mock/slow")
		setAgentModel(t, wf, "agent-3", "mock/slow")
		jobID := submitWorkflowFileAndWait(t, app, wf, e2eQuestionInput())
		nodes := workflowNodes(t, app, jobID)
		requireNodeOutputContains(t, nodes, "agg-1--select-winner", "Answer: C")
	})
}

func TestE2EAggregationSynthesisWorkflow(t *testing.T) {
	app, jobID := runE2EAggregationWorkflow(t, "synthesis", aggregationE2EAgentPrompts("synthesis"), aggregationE2EConfig("synthesis"))
	frozen := loadFrozenDAG(t, app, jobID)
	synthesize := requireFrozenNode(t, frozen, "agg-1--synthesize")
	if synthesize.Model != "mock/fast" {
		t.Fatalf("explicit synthesize model = %q, want mock/fast", synthesize.Model)
	}

	t.Run("explicit model override is honored", func(t *testing.T) {
		app := newE2EApp(t)
		config := aggregationE2EConfig("synthesis")
		config["model"] = "mock/slow"
		jobID := submitCompiledAggregationWorkflowAndWait(t, app, "synthesis", aggregationE2EAgentPrompts("synthesis"), config)
		frozen := loadFrozenDAG(t, app, jobID)
		if got := requireFrozenNode(t, frozen, "agg-1--synthesize").Model; got != "mock/slow" {
			t.Fatalf("synthesize model = %q, want explicit mock/slow", got)
		}
	})

	t.Run("rendered prompt sees anonymized candidates", func(t *testing.T) {
		promptCh := make(chan string, 1)
		app := newE2EAppWithResponseRouter(t, func(_ *providers.CompletionRequest, last string) (string, bool) {
			if strings.Contains(last, "Synthesize.") {
				select {
				case promptCh <- last:
				default:
				}
			}
			return "", false
		})
		submitCompiledAggregationWorkflowAndWait(t, app, "synthesis", aggregationE2EAgentPrompts("synthesis"), aggregationE2EConfig("synthesis"))
		var prompt string
		select {
		case prompt = <-promptCh:
		default:
			t.Fatal("synthesize prompt was not observed by provider")
		}
		if strings.Contains(prompt, "agent-1") || strings.Contains(prompt, "mock/fast") {
			t.Fatalf("synthesis prompt should anonymize candidate identities/models: %q", prompt)
		}
		if !strings.Contains(prompt, "Candidate A") || !strings.Contains(prompt, "Candidate B") {
			t.Fatalf("synthesis prompt missing anonymized candidates: %q", prompt)
		}
	})

	t.Run("missing model is rejected by source config validation", func(t *testing.T) {
		app := newE2EApp(t)
		config := aggregationE2EConfig("synthesis")
		delete(config, "model")
		wf := compiledAggregationWorkflowFile("e2e-synthesis-inherit-"+uuid.NewString(), "synthesis", aggregationE2EAgentPrompts("synthesis"), config)
		setAgentModel(t, wf, "agent-1", "mock/slow")
		setAgentModel(t, wf, "agent-2", "mock/fast")
		resp := app.request(http.MethodPost, "/api/workflows/submit", map[string]interface{}{
			"workflow_file":   wf,
			"input_values":    e2eQuestionInput(),
			"idempotency_key": "e2e-" + uuid.NewString(),
			"force_new_run":   true,
		})
		requireStatus(t, resp, http.StatusBadRequest)
		body := decodeResponse(t, resp)
		if !strings.Contains(fmt.Sprint(body["details"]), "synthesis requires model") {
			t.Fatalf("missing synthesis model details = %v", body["details"])
		}
	})
}

func TestE2EAggregationJudgeWorkflow(t *testing.T) {
	app, jobID := runE2EAggregationWorkflow(t, "judge", aggregationE2EAgentPrompts("judge"), aggregationE2EConfig("judge"))
	requireNodeOutputContains(t, workflowNodes(t, app, jobID), "agg-1--unanimous-policy", "Answer: A")

	t.Run("non-unanimous malformed judge output repairs selection", func(t *testing.T) {
		app := newE2EAppWithResponseRouter(t, func(req *providers.CompletionRequest, last string) (string, bool) {
			if req.Model != "mock/judge" {
				return "", false
			}
			if strings.Contains(last, "Return only the winning response label") {
				return "B", true
			}
			return "undecided", true
		})
		config := aggregationE2EConfig("judge")
		config["judge_model"] = "mock/judge"
		jobID := submitCompiledAggregationWorkflowAndWait(t, app, "judge", []string{
			"Answer: A\nJudge low.",
			"Answer: B\nJudge high.",
		}, config)
		nodes := workflowNodes(t, app, jobID)
		requireWorkflowNode(t, nodes, "agg-1--repair-selection_true")
		requireNodeOutputContains(t, nodes, "agg-1--select-winner", "Judge high.")
		requireNodeOutputContains(t, nodes, "agg-1--result", "Answer: B")
	})
}

func TestE2EAggregationDebateDecideWorkflow(t *testing.T) {
	app, jobID := runE2EAggregationWorkflow(t, "debate_decide", aggregationE2EAgentPrompts("debate_decide"), aggregationE2EConfig("debate_decide"))
	requireNodeOutputContains(t, workflowNodes(t, app, jobID), "agg-1--single-camp-policy", "Answer: A")

	t.Run("multi camp malformed judge output repairs winner answer", func(t *testing.T) {
		app := newE2EAppWithResponseRouter(t, func(req *providers.CompletionRequest, last string) (string, bool) {
			if req.Model != "mock/debate" {
				return "", false
			}
			if strings.Contains(last, "Return only the winning camp answer") {
				return "B", true
			}
			return "No camp winner in parseable form.", true
		})
		config := aggregationE2EConfig("debate_decide")
		config["judge_model"] = "mock/debate"
		jobID := submitCompiledAggregationWorkflowAndWait(t, app, "debate_decide", []string{
			"Answer: A\nCamp low.",
			"Answer: B\nCamp high.",
		}, config)
		nodes := workflowNodes(t, app, jobID)
		requireWorkflowNode(t, nodes, "agg-1--group-camps")
		requireWorkflowNode(t, nodes, "agg-1--judge-camp")
		requireWorkflowNode(t, nodes, "agg-1--repair-selection_true")
		requireNodeOutputContains(t, nodes, "agg-1--select-winner", "Camp high.")
		requireNodeOutputContains(t, nodes, "agg-1--result", "Answer: B")
	})
}

func TestE2EAggregationScoringWorkflow(t *testing.T) {
	app, jobID := runE2EAggregationWorkflow(t, "scoring", aggregationE2EAgentPrompts("scoring"), aggregationE2EConfig("scoring"))
	requireNodeOutputContains(t, workflowNodes(t, app, jobID), "agg-1--unanimous-policy", "Answer: A")

	t.Run("score reduction selects the maximum scoring candidate", func(t *testing.T) {
		app := newE2EAppWithResponseRouter(t, func(req *providers.CompletionRequest, last string) (string, bool) {
			if req.Model != "mock/scorer" {
				return "", false
			}
			if strings.Contains(last, "ScoreHigh") {
				return `{"score": 9, "scores": {"Correctness": 9, "Clarity": 9}}`, true
			}
			return `{"score": 2, "scores": {"Correctness": 2, "Clarity": 2}}`, true
		})
		config := aggregationE2EConfig("scoring")
		config["scoring_model"] = "mock/scorer"
		jobID := submitCompiledAggregationWorkflowAndWait(t, app, "scoring", []string{
			"Answer: A\nScoreLow.",
			"Answer: B\nScoreHigh.",
		}, config)
		nodes := workflowNodes(t, app, jobID)
		requireWorkflowNode(t, nodes, "agg-1--score-agent-1")
		requireWorkflowNode(t, nodes, "agg-1--score-agent-2")
		requireNodeOutputContains(t, nodes, "agg-1--reduce-scores", `"winner":"agent-2"`)
		requireNodeOutputContains(t, nodes, "agg-1--select-winner", "ScoreHigh.")
		requireNodeOutputContains(t, nodes, "agg-1--result", "Answer: B")
	})
}

func TestE2EAggregationPeerMatrixWorkflow(t *testing.T) {
	app, jobID := runE2EAggregationWorkflow(t, "peer_matrix", aggregationE2EAgentPrompts("peer_matrix"), aggregationE2EConfig("peer_matrix"))
	requireNodeOutputContains(t, workflowNodes(t, app, jobID), "agg-1--unanimous-policy", "Answer: A")

	t.Run("pairwise matrix reduction selects the highest reviewed candidate", func(t *testing.T) {
		app := newE2EAppWithResponseRouter(t, func(req *providers.CompletionRequest, last string) (string, bool) {
			if !strings.Contains(last, "Response:") {
				return "", false
			}
			if strings.Contains(last, "PeerHigh") {
				return `{"score": 9, "scores": {"Correctness": 9, "Clarity": 9}}`, true
			}
			return `{"score": 1, "scores": {"Correctness": 1, "Clarity": 1}}`, true
		})
		jobID := submitCompiledAggregationWorkflowAndWait(t, app, "peer_matrix", []string{
			"Answer: A\nPeerLow.",
			"Answer: B\nPeerHigh.",
		}, aggregationE2EConfig("peer_matrix"))
		nodes := workflowNodes(t, app, jobID)
		requireWorkflowNode(t, nodes, "agg-1--review-agent-1-agent-2")
		requireWorkflowNode(t, nodes, "agg-1--review-agent-2-agent-1")
		requireNodeOutputContains(t, nodes, "agg-1--reduce-peer-scores", `"winner":"agent-2"`)
		requireNodeOutputContains(t, nodes, "agg-1--select-winner", "PeerHigh.")
		requireNodeOutputContains(t, nodes, "agg-1--result", "Answer: B")
	})
}

func runE2EAggregationWorkflow(t *testing.T, method string, prompts []string, config map[string]interface{}) (*e2eApp, string) {
	t.Helper()
	app := newE2EApp(t)
	jobID := submitCompiledAggregationWorkflowAndWait(t, app, method, prompts, config)
	frozen := loadFrozenDAG(t, app, jobID)
	nodes := workflowNodes(t, app, jobID)

	sourceWorkflowID := aggregationWorkflowIDForMethod(method)
	assertNoWorkflowRefs(t, frozen)
	assertNoLegacyAggregationPath(t, frozen, nodes)
	assertExpandedAggregation(t, frozen, nodes, "agg-1", sourceWorkflowID, method)
	assertCompletedNodePersistence(t, nodes)
	assertJobCompletedWithOutput(t, app, jobID)
	return app, jobID
}

func submitCompiledAggregationWorkflowAndWait(t *testing.T, app *e2eApp, method string, prompts []string, config map[string]interface{}) string {
	t.Helper()
	wf := compiledAggregationWorkflowFile("e2e-compiled-"+method+"-"+uuid.NewString(), method, prompts, config)
	return submitWorkflowFileAndWait(t, app, wf, map[string]interface{}{"user_prompt": "Which answer should win?"})
}

func compiledAggregationWorkflowFile(id, method string, prompts []string, config map[string]interface{}) map[string]interface{} {
	wf := aggregationWorkflowFile(id, method, prompts, config)
	for _, rawNode := range wf["nodes"].([]interface{}) {
		node := rawNode.(map[string]interface{})
		if node["id"] != "agg-1" {
			continue
		}
		data := node["data"].(map[string]interface{})
		nodeConfig := data["config"].(map[string]interface{})
		nodeConfig["aggregationWorkflowId"] = aggregationWorkflowIDForMethod(method)
		return wf
	}
	panic("aggregationWorkflowFile did not include agg-1")
}

func aggregationWorkflowIDForMethod(method string) string {
	switch method {
	case "collect":
		return "aggregation-collect"
	case "majority_vote":
		return "aggregation-majority-vote"
	case "synthesis":
		return "aggregation-synthesis"
	case "judge":
		return "aggregation-judge"
	case "debate_decide":
		return "aggregation-debate-decide"
	case "scoring":
		return "aggregation-scoring"
	case "peer_matrix":
		return "aggregation-peer-matrix"
	default:
		panic("unknown aggregation method " + method)
	}
}

func submitWorkflowFileAndWait(t *testing.T, app *e2eApp, wfFile map[string]interface{}, inputValues map[string]interface{}) string {
	t.Helper()
	payload := map[string]interface{}{
		"workflow_file":   wfFile,
		"input_values":    inputValues,
		"idempotency_key": "e2e-" + uuid.NewString(),
		"force_new_run":   true,
	}
	submit := app.request(http.MethodPost, "/api/workflows/submit", payload)
	requireStatus(t, submit, http.StatusCreated)
	body := decodeResponse(t, submit)
	jobID, ok := body["job_id"].(string)
	if !ok || jobID == "" {
		t.Fatalf("submit response missing job_id: %v", body)
	}
	waitForJobStatus(t, app, jobID, events.JobStatusCompleted)
	return jobID
}

func submitStoredWorkflowAndWait(t *testing.T, app *e2eApp, workflowID string, inputValues map[string]interface{}) string {
	t.Helper()
	def, err := app.store.GetWorkflow(workflowID)
	if err != nil {
		t.Fatalf("GetWorkflow(%s): %v", workflowID, err)
	}
	var wfFile map[string]interface{}
	if err := json.Unmarshal([]byte(def.Definition), &wfFile); err != nil {
		t.Fatalf("unmarshal workflow %s definition: %v", workflowID, err)
	}
	return submitWorkflowFileAndWait(t, app, wfFile, inputValues)
}

func loadFrozenDAG(t *testing.T, app *e2eApp, jobID string) workflowruntime.CanonicalWorkflow {
	t.Helper()
	job, err := app.store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("GetExecution(%s): %v", jobID, err)
	}
	if strings.TrimSpace(job.DAGSnapshot) == "" {
		t.Fatalf("job %s missing DAGSnapshot", jobID)
	}
	var frozen workflowruntime.CanonicalWorkflow
	if err := json.Unmarshal([]byte(job.DAGSnapshot), &frozen); err != nil {
		t.Fatalf("unmarshal DAGSnapshot for %s: %v", jobID, err)
	}
	return frozen
}

func workflowNodes(t *testing.T, app *e2eApp, jobID string) []storage.WorkflowNode {
	t.Helper()
	nodes, err := app.store.GetWorkflowNodes(jobID)
	if err != nil {
		t.Fatalf("GetWorkflowNodes(%s): %v", jobID, err)
	}
	return nodes
}

func nodeAttempts(t *testing.T, app *e2eApp, jobID string) []storage.NodeExecutionAttempt {
	t.Helper()
	attempts, err := app.store.ListNodeExecutionAttemptsByJob(context.Background(), jobID)
	if err != nil {
		t.Fatalf("ListNodeExecutionAttemptsByJob(%s): %v", jobID, err)
	}
	return attempts
}

func assertNoWorkflowRefs(t *testing.T, frozen workflowruntime.CanonicalWorkflow) {
	t.Helper()
	for _, node := range frozen.Nodes {
		if node.Type == "workflow_ref" {
			t.Fatalf("frozen DAG still contains workflow_ref node %q", node.ID)
		}
		if strings.Contains(node.ID, "__") {
			t.Fatalf("frozen node %q contains reserved delimiter __", node.ID)
		}
	}
}

func assertNoLegacyAggregationPath(t *testing.T, frozen workflowruntime.CanonicalWorkflow, nodes []storage.WorkflowNode) {
	t.Helper()
	for _, node := range frozen.Nodes {
		if strings.Contains(node.ID, "__agg__") {
			t.Fatalf("frozen node %q used legacy aggregation sub-node delimiter", node.ID)
		}
		if node.Type == "result" && node.AggregationMethod != "" && node.AggregationMethod != "collect" {
			t.Fatalf("result node %q still owns legacy aggregation method %q", node.ID, node.AggregationMethod)
		}
	}
	for _, node := range nodes {
		if strings.Contains(node.NodeID, "__agg__") {
			t.Fatalf("persisted node %q used legacy aggregation sub-node delimiter", node.NodeID)
		}
		metadata := workflowNodeMetadata(t, node)
		if metadata["legacy_result_aggregation"] == true {
			t.Fatalf("compiled aggregation node %q was marked as legacy result aggregation: %+v", node.NodeID, metadata)
		}
	}
}

func assertExpandedAggregation(t *testing.T, frozen workflowruntime.CanonicalWorkflow, nodes []storage.WorkflowNode, anchorID, sourceWorkflowID, method string) {
	t.Helper()
	prefix := anchorID + "--"
	groupID := anchorID + "--result"
	expandedCount := 0
	groupedCount := 0
	sourceHash := ""

	for _, node := range frozen.Nodes {
		if !strings.HasPrefix(node.ID, prefix) {
			continue
		}
		expandedCount++
		if got := metadataString(node.Metadata, "source_workflow_id"); got != sourceWorkflowID {
			t.Fatalf("node %q source_workflow_id = %q, want %q metadata=%v", node.ID, got, sourceWorkflowID, node.Metadata)
		}
		if got := metadataString(node.Metadata, "source_node_id"); got == "" {
			t.Fatalf("node %q missing source_node_id metadata=%v", node.ID, node.Metadata)
		}
		if got := metadataString(node.Metadata, "source_workflow_hash"); got == "" {
			t.Fatalf("node %q missing source_workflow_hash metadata=%v", node.ID, node.Metadata)
		} else if sourceHash == "" {
			sourceHash = got
		}
		if got := metadataString(node.Metadata, "source_parent_node_id"); got == "" {
			t.Fatalf("node %q missing source_parent_node_id metadata=%v", node.ID, node.Metadata)
		}
		if got := metadataString(node.Metadata, "aggregation_method"); got != method {
			t.Fatalf("node %q aggregation_method = %q, want %q metadata=%v", node.ID, got, method, node.Metadata)
		}
	}
	if expandedCount < 2 {
		t.Fatalf("expected expanded aggregation nodes with prefix %q, found %d in %v", prefix, expandedCount, frozenNodeIDs(frozen))
	}
	if sourceHash == "" {
		t.Fatalf("source hash was not captured for %s", sourceWorkflowID)
	}

	for _, node := range nodes {
		if node.ParentNodeID == groupID {
			groupedCount++
			if node.Status != events.JobStatusCompleted {
				t.Fatalf("grouped node %q status = %q, want completed", node.NodeID, node.Status)
			}
			metadata := workflowNodeMetadata(t, node)
			if got := metadataString(metadata, "source_workflow_id"); got != "" && got != sourceWorkflowID {
				t.Fatalf("persisted node %q source_workflow_id = %q, want %q", node.NodeID, got, sourceWorkflowID)
			}
		}
	}
	if groupedCount == 0 {
		t.Fatalf("expected persisted nodes grouped under %q; nodes=%v", groupID, workflowNodeIDs(nodes))
	}
}

func assertCompletedNodePersistence(t *testing.T, nodes []storage.WorkflowNode) {
	t.Helper()
	if len(nodes) == 0 {
		t.Fatalf("expected persisted workflow nodes")
	}
	sawPromptWithUsage := false
	sawResult := false
	for _, node := range nodes {
		if node.Status != events.JobStatusCompleted {
			t.Fatalf("node %q status = %q, want completed", node.NodeID, node.Status)
		}
		if node.NodeType == "prompt" && node.TokensInput > 0 && node.Cost > 0 {
			sawPromptWithUsage = true
		}
		if node.NodeType == "result" {
			sawResult = true
		}
	}
	if !sawPromptWithUsage {
		t.Fatalf("expected at least one prompt node with token/cost usage: %+v", nodes)
	}
	if !sawResult {
		t.Fatalf("expected persisted result node: %+v", nodes)
	}
}

func assertJobCompletedWithOutput(t *testing.T, app *e2eApp, jobID string) {
	t.Helper()
	job, err := app.store.GetExecution(jobID)
	if err != nil {
		t.Fatalf("GetExecution(%s): %v", jobID, err)
	}
	if job.Status != events.JobStatusCompleted {
		t.Fatalf("job %s status = %q, want completed", jobID, job.Status)
	}
	if strings.TrimSpace(job.ResultText) == "" {
		t.Fatalf("job %s completed with empty result_text", jobID)
	}
}

func workflowNodeMetadata(t *testing.T, node storage.WorkflowNode) map[string]interface{} {
	t.Helper()
	if strings.TrimSpace(node.Metadata) == "" {
		return nil
	}
	var metadata map[string]interface{}
	if err := json.Unmarshal([]byte(node.Metadata), &metadata); err != nil {
		t.Fatalf("unmarshal metadata for node %q: %v", node.NodeID, err)
	}
	return metadata
}

func metadataString(metadata map[string]interface{}, key string) string {
	if metadata == nil {
		return ""
	}
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

func frozenNodeIDs(frozen workflowruntime.CanonicalWorkflow) []string {
	ids := make([]string, 0, len(frozen.Nodes))
	for _, node := range frozen.Nodes {
		ids = append(ids, node.ID)
	}
	return ids
}

func workflowNodeIDs(nodes []storage.WorkflowNode) []string {
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		ids = append(ids, node.NodeID)
	}
	return ids
}

func requireFrozenContainsSource(t *testing.T, frozen workflowruntime.CanonicalWorkflow, sourceWorkflowID string) []workflowruntime.CanonicalNode {
	t.Helper()
	var matches []workflowruntime.CanonicalNode
	for _, node := range frozen.Nodes {
		if metadataString(node.Metadata, "source_workflow_id") == sourceWorkflowID {
			matches = append(matches, node)
		}
	}
	if len(matches) == 0 {
		t.Fatalf("frozen DAG missing source workflow %q; nodes=%v", sourceWorkflowID, frozenNodeIDs(frozen))
	}
	return matches
}

func requireNoChildExecutions(t *testing.T, app *e2eApp, parentJobID string) {
	t.Helper()
	parent, err := app.store.GetExecution(parentJobID)
	if err != nil {
		t.Fatalf("GetExecution(%s): %v", parentJobID, err)
	}
	children, err := app.store.GetChildExecutions(parent.WorkflowExecutionID)
	if err != nil {
		t.Fatalf("GetChildExecutions(%s): %v", parent.WorkflowExecutionID, err)
	}
	if len(children) != 0 {
		t.Fatalf("expected no child executions for %s, got %+v", parentJobID, children)
	}
}

func requireChildExecutions(t *testing.T, app *e2eApp, parentJobID string) []storage.WorkflowExecution {
	t.Helper()
	parent, err := app.store.GetExecution(parentJobID)
	if err != nil {
		t.Fatalf("GetExecution(%s): %v", parentJobID, err)
	}
	children, err := app.store.GetChildExecutions(parent.WorkflowExecutionID)
	if err != nil {
		t.Fatalf("GetChildExecutions(%s): %v", parent.WorkflowExecutionID, err)
	}
	if len(children) == 0 {
		t.Fatalf("expected child executions for %s", parentJobID)
	}
	return children
}

func findFrozenNode(frozen workflowruntime.CanonicalWorkflow, id string) *workflowruntime.CanonicalNode {
	for i := range frozen.Nodes {
		if frozen.Nodes[i].ID == id {
			return &frozen.Nodes[i]
		}
	}
	return nil
}

func requireWorkflowNode(t *testing.T, nodes []storage.WorkflowNode, nodeID string) storage.WorkflowNode {
	t.Helper()
	for _, node := range nodes {
		if node.NodeID == nodeID {
			return node
		}
	}
	t.Fatalf("missing workflow node %q in %v", nodeID, workflowNodeIDs(nodes))
	return storage.WorkflowNode{}
}

func requireNodeOutputContains(t *testing.T, nodes []storage.WorkflowNode, nodeID string, want string) {
	t.Helper()
	node := requireWorkflowNode(t, nodes, nodeID)
	if !strings.Contains(node.Output, want) {
		t.Fatalf("%s output does not contain %q:\n%s", nodeID, want, node.Output)
	}
}

func requireFrozenNode(t *testing.T, frozen workflowruntime.CanonicalWorkflow, nodeID string) workflowruntime.CanonicalNode {
	t.Helper()
	node := findFrozenNode(frozen, nodeID)
	if node == nil {
		t.Fatalf("missing frozen node %q in %v", nodeID, frozenNodeIDs(frozen))
	}
	return *node
}

func requireStringOrder(t *testing.T, text string, wants []string) {
	t.Helper()
	lastIndex := -1
	for _, want := range wants {
		index := strings.Index(text, want)
		if index < 0 {
			t.Fatalf("text missing %q:\n%s", want, text)
		}
		if index < lastIndex {
			t.Fatalf("text has %q out of order in:\n%s", want, text)
		}
		lastIndex = index
	}
}

func requireInputIDs(t *testing.T, raw interface{}, want []string) {
	t.Helper()
	items, ok := raw.([]interface{})
	if !ok {
		t.Fatalf("input_ids = %#v, want array", raw)
	}
	if len(items) != len(want) {
		t.Fatalf("input_ids len = %d, want %d: %#v", len(items), len(want), raw)
	}
	for i, rawItem := range items {
		got, _ := rawItem.(string)
		if got != want[i] {
			t.Fatalf("input_ids[%d] = %q, want %q: %#v", i, got, want[i], raw)
		}
	}
}

func setAgentModel(t *testing.T, wf map[string]interface{}, agentID, model string) {
	t.Helper()
	for _, rawNode := range wf["nodes"].([]interface{}) {
		node := rawNode.(map[string]interface{})
		if node["id"] != agentID {
			continue
		}
		config := node["data"].(map[string]interface{})["config"].(map[string]interface{})
		config["model"] = model
		return
	}
	t.Fatalf("workflow file missing agent %q", agentID)
}

func newE2EAppWithResponseRouter(t *testing.T, route func(req *providers.CompletionRequest, lastMessage string) (string, bool)) *e2eApp {
	t.Helper()
	return newE2EAppWithWorkersAndProvider(t, true, &deterministicProvider{
		name:     "mock",
		models:   e2eProviderModels(),
		response: "deterministic",
		responseFunc: func(req *providers.CompletionRequest) (string, error) {
			last := lastCompletionMessage(req)
			if route != nil {
				if content, ok := route(req, last); ok {
					return content, nil
				}
			}
			return defaultDeterministicCompletionContent(req, last), nil
		},
	})
}

func lastCompletionMessage(req *providers.CompletionRequest) string {
	if req == nil || len(req.Messages) == 0 {
		return ""
	}
	return req.Messages[len(req.Messages)-1].Content
}

func defaultDeterministicCompletionContent(_ *providers.CompletionRequest, lastMessage string) string {
	return fmt.Sprintf("deterministic: %s\nAnswer: A\nFinal answer: A\n{\"score\": 9, \"scores\": {\"Correctness\": 9, \"Clarity\": 8}}", lastMessage)
}

func debugWorkflowShape(frozen workflowruntime.CanonicalWorkflow) string {
	return fmt.Sprintf("nodes=%v", frozenNodeIDs(frozen))
}
