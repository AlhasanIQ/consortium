package e2e_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/storage"
	workflowruntime "github.com/alhasaniq/consortium/pkg/workflow/runtime"
	"github.com/google/uuid"
)

func TestE2ECompiledAggregationRetryOnlyRetriesFailedInternalNode(t *testing.T) {
	provider := &deterministicProvider{
		name:              "mock",
		models:            e2eProviderModels(),
		response:          "deterministic",
		failuresRemaining: map[string]int{"mock/flaky": 1},
	}
	app := newE2EAppWithWorkersAndProvider(t, true, provider)

	config := aggregationE2EConfig("synthesis")
	config["model"] = "mock/flaky"
	wf := compiledAggregationWorkflowFile("e2e-retry-"+uuid.NewString(), "synthesis", aggregationE2EAgentPrompts("synthesis"), config)
	setAggregationRetryPolicy(wf, retryPolicyWithAttempts(2))

	jobID := submitWorkflowFileAndWait(t, app, wf, e2eQuestionInput())
	attempts := nodeAttempts(t, app, jobID)

	synthAttempts := attemptsForNode(attempts, "agg-1--synthesize")
	if len(synthAttempts) != 2 {
		t.Fatalf("agg-1--synthesize attempts = %d, want 2: %+v", len(synthAttempts), synthAttempts)
	}
	if synthAttempts[0].Status != "failed" || synthAttempts[1].Status != events.JobStatusCompleted {
		t.Fatalf("synthesize attempt statuses = %q/%q, want failed/completed", synthAttempts[0].Status, synthAttempts[1].Status)
	}
	for _, nodeID := range []string{"agent-1", "agent-2"} {
		agentAttempts := attemptsForNode(attempts, nodeID)
		if len(agentAttempts) != 1 {
			t.Fatalf("%s attempts = %d, want 1: %+v", nodeID, len(agentAttempts), agentAttempts)
		}
		if agentAttempts[0].Status != events.JobStatusCompleted {
			t.Fatalf("%s status = %q, want completed", nodeID, agentAttempts[0].Status)
		}
	}
}

func TestE2ECompiledAggregationResumeUsesFrozenExpandedDag(t *testing.T) {
	app := newE2EAppWithWorkers(t, false)
	wf := compiledAggregationWorkflowFile("e2e-resume-"+uuid.NewString(), "synthesis", aggregationE2EAgentPrompts("synthesis"), aggregationE2EConfig("synthesis"))
	jobID := submitWorkflowFile(t, app, wf, e2eQuestionInput())

	before := loadFrozenDAG(t, app, jobID)
	beforeIDs := frozenNodeIDs(before)
	beforeHash := requireSourceHash(t, before, "aggregation-synthesis")
	mutateStoredAggregationSynthesis(t, app, "resume-"+uuid.NewString())

	afterEdit := loadFrozenDAG(t, app, jobID)
	if got := requireSourceHash(t, afterEdit, "aggregation-synthesis"); got != beforeHash {
		t.Fatalf("pending frozen source hash changed after source edit: %q -> %q", beforeHash, got)
	}
	if gotIDs := frozenNodeIDs(afterEdit); !slices.Equal(beforeIDs, gotIDs) {
		t.Fatalf("pending frozen node IDs changed after source edit: before=%v after=%v", beforeIDs, gotIDs)
	}

	app.manager.StartWorkers()
	waitForJobStatus(t, app, jobID, events.JobStatusCompleted)
	completed := loadFrozenDAG(t, app, jobID)
	if got := requireSourceHash(t, completed, "aggregation-synthesis"); got != beforeHash {
		t.Fatalf("completed frozen source hash changed after resume: %q -> %q", beforeHash, got)
	}
	if gotIDs := frozenNodeIDs(completed); !slices.Equal(beforeIDs, gotIDs) {
		t.Fatalf("completed frozen node IDs changed after resume: before=%v after=%v", beforeIDs, gotIDs)
	}
}

func TestE2ECompiledAggregationSourceEditDoesNotMutateHistoricalJob(t *testing.T) {
	app := newE2EApp(t)
	wf := compiledAggregationWorkflowFile("e2e-source-edit-"+uuid.NewString(), "synthesis", aggregationE2EAgentPrompts("synthesis"), aggregationE2EConfig("synthesis"))
	jobID := submitWorkflowFileAndWait(t, app, wf, e2eQuestionInput())

	original := loadFrozenDAG(t, app, jobID)
	originalHash := requireSourceHash(t, original, "aggregation-synthesis")
	originalIDs := frozenNodeIDs(original)
	mutateStoredAggregationSynthesis(t, app, "historical-"+uuid.NewString())

	historical := loadFrozenDAG(t, app, jobID)
	if got := requireSourceHash(t, historical, "aggregation-synthesis"); got != originalHash {
		t.Fatalf("historical job source hash changed after source edit: %q -> %q", originalHash, got)
	}
	if gotIDs := frozenNodeIDs(historical); !slices.Equal(originalIDs, gotIDs) {
		t.Fatalf("historical frozen node IDs changed after source edit: before=%v after=%v", originalIDs, gotIDs)
	}

	nextJobID := submitWorkflowFileAndWait(t, app, compiledAggregationWorkflowFile("e2e-source-edit-next-"+uuid.NewString(), "synthesis", aggregationE2EAgentPrompts("synthesis"), aggregationE2EConfig("synthesis")), e2eQuestionInput())
	nextHash := requireSourceHash(t, loadFrozenDAG(t, app, nextJobID), "aggregation-synthesis")
	if nextHash == originalHash {
		t.Fatalf("source edit did not affect future source hash; hash remained %q", originalHash)
	}
}

func TestE2ECompilerRejectsInvalidExpandedDagBeforeFreeze(t *testing.T) {
	app := newE2EApp(t)
	wf := compiledAggregationWorkflowFile("e2e-invalid-ref-"+uuid.NewString(), "synthesis", aggregationE2EAgentPrompts("synthesis"), aggregationE2EConfig("synthesis"))
	for _, rawNode := range wf["nodes"].([]interface{}) {
		node := rawNode.(map[string]interface{})
		if node["id"] != "agg-1" {
			continue
		}
		config := node["data"].(map[string]interface{})["config"].(map[string]interface{})
		config["aggregationWorkflowId"] = "aggregation-does-not-exist"
	}

	rec := app.request(http.MethodPost, "/api/workflows/submit", map[string]interface{}{
		"workflow_file":   wf,
		"input_values":    e2eQuestionInput(),
		"idempotency_key": "e2e-" + uuid.NewString(),
		"force_new_run":   true,
	})
	requireStatus(t, rec, http.StatusBadRequest)
	body := decodeResponse(t, rec)
	if body["code"] != "INVALID_WORKFLOW" {
		t.Fatalf("code = %v, want INVALID_WORKFLOW body=%v", body["code"], body)
	}
	jobs, err := app.store.ListExecutions(10)
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("invalid workflow created jobs before freeze: %+v", jobs)
	}
}

func TestE2ECompilerRejectsPostCompileInvalidDagBeforeFreeze(t *testing.T) {
	app := newE2EApp(t)
	wf := compiledAggregationWorkflowFile("e2e-invalid-compiled-"+uuid.NewString(), "synthesis", aggregationE2EAgentPrompts("synthesis"), aggregationE2EConfig("synthesis"))
	setAgentModel(t, wf, "agent-1", "missing/model")

	rec := app.request(http.MethodPost, "/api/workflows/submit", map[string]interface{}{
		"workflow_file":   wf,
		"input_values":    e2eQuestionInput(),
		"idempotency_key": "e2e-" + uuid.NewString(),
		"force_new_run":   true,
	})
	requireStatus(t, rec, http.StatusBadRequest)
	body := decodeResponse(t, rec)
	if body["code"] != "INVALID_WORKFLOW" {
		t.Fatalf("code = %v, want INVALID_WORKFLOW body=%v", body["code"], body)
	}
	if !strings.Contains(fmt.Sprintf("%v", body["details"]), "missing/model") {
		t.Fatalf("details should name missing model: %v", body)
	}
	jobs, err := app.store.ListExecutions(10)
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("invalid compiled workflow created jobs before freeze: %+v", jobs)
	}
}

func TestE2EExplicitChildWorkflowStillCreatesSeparateDurableBoundary(t *testing.T) {
	app := newE2EApp(t)
	parentJobID := submitStoredWorkflowAndWait(t, app, "benchmark-informed-captain-synthesis-cheap", e2eBenchmarkInput())
	parentFrozen := loadFrozenDAG(t, app, parentJobID)
	childNode := findFrozenNode(parentFrozen, "child-reasoning-informed-captain-synthesis-cheap")
	if childNode == nil || childNode.Type != "child_workflow" {
		t.Fatalf("parent frozen DAG missing explicit child_workflow boundary: %v", frozenNodeIDs(parentFrozen))
	}

	children := requireChildExecutions(t, app, parentJobID)
	if len(children) != 1 {
		t.Fatalf("child execution count = %d, want 1: %+v", len(children), children)
	}
	child := children[0]
	if child.ID == parentJobID {
		t.Fatalf("child execution reused parent job ID %s", parentJobID)
	}
	childFrozen := loadFrozenDAG(t, app, child.ID)
	requireFrozenContainsSource(t, childFrozen, "aggregation-synthesis")
	requireNoChildExecutions(t, app, child.ID)
}

func submitWorkflowFile(t *testing.T, app *e2eApp, wfFile map[string]interface{}, inputValues map[string]interface{}) string {
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
	if !ok || strings.TrimSpace(jobID) == "" {
		t.Fatalf("submit response missing job_id: %v", body)
	}
	return jobID
}

func retryPolicyWithAttempts(maxAttempts int) map[string]interface{} {
	return map[string]interface{}{
		"max_attempts":     maxAttempts,
		"backoff_ms":       0,
		"backoff_multiply": 1.0,
		"max_backoff_ms":   0,
	}
}

func setAggregationRetryPolicy(wf map[string]interface{}, retryPolicy map[string]interface{}) {
	for _, rawNode := range wf["nodes"].([]interface{}) {
		node := rawNode.(map[string]interface{})
		if node["id"] != "agg-1" {
			continue
		}
		config := node["data"].(map[string]interface{})["config"].(map[string]interface{})
		config["retryPolicy"] = retryPolicy
		return
	}
}

func attemptsForNode(attempts []storage.NodeExecutionAttempt, nodeID string) []storage.NodeExecutionAttempt {
	var matches []storage.NodeExecutionAttempt
	for _, attempt := range attempts {
		if attempt.NodeID == nodeID {
			matches = append(matches, attempt)
		}
	}
	return matches
}

func requireSourceHash(t *testing.T, frozen workflowruntime.CanonicalWorkflow, sourceWorkflowID string) string {
	t.Helper()
	nodes := requireFrozenContainsSource(t, frozen, sourceWorkflowID)
	sourceHash := metadataString(nodes[0].Metadata, "source_workflow_hash")
	if sourceHash == "" {
		t.Fatalf("source workflow %s missing source_workflow_hash metadata", sourceWorkflowID)
	}
	for _, node := range nodes[1:] {
		if got := metadataString(node.Metadata, "source_workflow_hash"); got != sourceHash {
			t.Fatalf("source workflow %s has inconsistent source hashes %q and %q", sourceWorkflowID, sourceHash, got)
		}
	}
	return sourceHash
}

func mutateStoredAggregationSynthesis(t *testing.T, app *e2eApp, marker string) {
	t.Helper()
	def, err := app.store.GetWorkflow("aggregation-synthesis")
	if err != nil {
		t.Fatalf("GetWorkflow(aggregation-synthesis): %v", err)
	}
	var body map[string]interface{}
	if err := json.Unmarshal([]byte(def.Definition), &body); err != nil {
		t.Fatalf("unmarshal aggregation-synthesis: %v", err)
	}
	found := false
	nodes, _ := body["nodes"].([]interface{})
	for _, rawNode := range nodes {
		node, _ := rawNode.(map[string]interface{})
		if node["id"] != "agent-synthesizer" {
			continue
		}
		data := node["data"].(map[string]interface{})
		config := data["config"].(map[string]interface{})
		config["systemPrompt"] = "Mutated e2e synthesis source " + marker
		found = true
		break
	}
	if !found {
		t.Fatalf("aggregation-synthesis source missing agent-synthesizer node")
	}
	updated, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal updated aggregation-synthesis: %v", err)
	}
	def.Definition = string(updated)
	if err := app.store.UpdateWorkflow(def); err != nil {
		t.Fatalf("UpdateWorkflow(aggregation-synthesis): %v", err)
	}
}
