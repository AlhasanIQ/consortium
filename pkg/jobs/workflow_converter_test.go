package jobs

import (
	"testing"

	"github.com/alhasaniq/consortium/pkg/workflow"
)

func TestWorkflowFromDefinitionChildWorkflow(t *testing.T) {
	raw := `{
		"id":"benchmark-parent",
		"name":"Benchmark Parent",
		"nodes":[
			{"id":"input","type":"input","data":{"type":"input","config":{"name":"user_prompt"}}},
			{"id":"child","type":"child_workflow","data":{"type":"child_workflow","config":{"childWorkflowId":"reasoning-informed-captain-synthesis","inputTemplate":{"user_prompt":"{{user_prompt}}"},"outputKey":"synthesized_response","await":true,"timeoutSeconds":360}}},
			{"id":"result","type":"result","data":{"type":"result","config":{"name":"benchmark_answer","aggregationMethod":"collect"}}}
		],
		"edges":[
			{"source":"input","target":"child"},
			{"source":"child","target":"result"}
		]
	}`

	wf, err := WorkflowFromDefinition(raw, map[string]interface{}{"user_prompt": "Q"})
	if err != nil {
		t.Fatalf("WorkflowFromDefinition failed: %v", err)
	}
	if wf.ID != "benchmark-parent" {
		t.Fatalf("unexpected workflow id: %s", wf.ID)
	}
	if len(wf.Nodes) != 2 {
		t.Fatalf("expected 2 executable nodes, got %d", len(wf.Nodes))
	}
	if wf.Nodes[0].Type != "child_workflow" {
		t.Fatalf("expected first node to be child_workflow, got %s", wf.Nodes[0].Type)
	}
	if wf.Nodes[0].ChildWorkflowID != "reasoning-informed-captain-synthesis" {
		t.Fatalf("unexpected child workflow id: %s", wf.Nodes[0].ChildWorkflowID)
	}
	if wf.Nodes[0].TimeoutSeconds != 360 {
		t.Fatalf("expected child timeout_seconds=360, got %d", wf.Nodes[0].TimeoutSeconds)
	}
}

func TestWorkflowConverter_WorkflowRefCompilesByDefault(t *testing.T) {
	raw := `{
	  "id":"parent",
	  "nodes":[
	    {"id":"ref","type":"workflow_ref","data":{"type":"workflow_ref","config":{
	      "workflowId":"aggregation-synthesis",
	      "inputTemplate":{"user_prompt":"{{user_prompt}}"},
	      "outputKey":"result"
	    }}}
	  ],
	  "edges":[]
	}`
	wf, err := WorkflowFromDefinition(raw, map[string]interface{}{"user_prompt": "x"})
	if err != nil {
		t.Fatalf("WorkflowFromDefinition failed: %v", err)
	}
	if len(wf.Nodes) != 1 || wf.Nodes[0].Type != workflow.NodeTypeWorkflowRef {
		t.Fatalf("expected workflow_ref node, got %+v", wf.Nodes)
	}
	if wf.Nodes[0].WorkflowRefID != "aggregation-synthesis" {
		t.Fatalf("WorkflowRefID = %q", wf.Nodes[0].WorkflowRefID)
	}
	if got := wf.Nodes[0].InputTemplate["user_prompt"]; got != "{{user_prompt}}" {
		t.Fatalf("InputTemplate[user_prompt] = %q", got)
	}
	if wf.Nodes[0].OutputKey != "result" {
		t.Fatalf("OutputKey = %q", wf.Nodes[0].OutputKey)
	}
	if wf.Nodes[0].WorkflowRefID == wf.Nodes[0].ChildWorkflowID {
		t.Fatalf("workflow_ref should not be represented as child_workflow fields: %+v", wf.Nodes[0])
	}
	if wf.Nodes[0].ChildInputTemplate != nil || wf.Nodes[0].ChildOutputKey != "" {
		t.Fatalf("workflow_ref should not populate child workflow input/output fields: %+v", wf.Nodes[0])
	}
}

func TestWorkflowConverter_WorkflowRefRequiresWorkflowID(t *testing.T) {
	raw := `{
	  "id":"parent",
	  "nodes":[
	    {"id":"ref","type":"workflow_ref","data":{"type":"workflow_ref","config":{
	      "inputTemplate":{"user_prompt":"{{user_prompt}}"},
	      "outputKey":"result"
	    }}}
	  ],
	  "edges":[]
	}`
	if _, err := WorkflowFromDefinition(raw, map[string]interface{}{"user_prompt": "x"}); err == nil {
		t.Fatal("expected missing workflowId error")
	}
}

func TestWorkflowConverter_AggregationMacroWithWorkflowIDBecomesWorkflowRef(t *testing.T) {
	raw := `{
	  "id":"parent",
	  "nodes":[
	    {"id":"agent-a","type":"agent","data":{"type":"agent","config":{"model":"openai/gpt-4o-mini","userPrompt":"Answer A","maxTokens":256}}},
	    {"id":"agent-b","type":"agent","data":{"type":"agent","config":{"model":"openai/gpt-4o-mini","userPrompt":"Answer B","maxTokens":256}}},
	    {"id":"agg","type":"aggregation","data":{"type":"aggregation","config":{
	      "aggregationMethod":"majority_vote",
	      "aggregationWorkflowId":"aggregation-majority-vote",
	      "timeoutSeconds":120,
	      "retryPolicy":{"max_attempts":1},
	      "benchmarkOutputPackaging":true,
	      "aggregationConfig":{"tie_breaker":"first","extraction_strategy":"first_letter"}
	    }}},
	    {"id":"visible-result","type":"result","data":{"type":"result","config":{"name":"final_answer","outputFormat":"text"}}}
	  ],
	  "edges":[
	    {"source":"agent-a","target":"agg"},
	    {"source":"agent-b","target":"agg"},
	    {"source":"agg","target":"visible-result"}
	  ]
	}`
	wf, err := WorkflowFromDefinition(raw, map[string]interface{}{})
	if err != nil {
		t.Fatalf("WorkflowFromDefinition failed: %v", err)
	}
	var agg *workflow.Node
	for _, node := range wf.Nodes {
		if node.ID == "agg" {
			agg = node
		}
		if node.ID == "visible-result" {
			t.Fatalf("presentation result should not be emitted as runtime node")
		}
	}
	if agg == nil {
		t.Fatalf("aggregation node missing: %+v", wf.Nodes)
	}
	if agg.Type != workflow.NodeTypeWorkflowRef {
		t.Fatalf("aggregation macro type = %q, want workflow_ref", agg.Type)
	}
	if agg.WorkflowRefID != "aggregation-majority-vote" {
		t.Fatalf("WorkflowRefID = %q", agg.WorkflowRefID)
	}
	if agg.TimeoutSeconds != 120 {
		t.Fatalf("TimeoutSeconds = %d, want 120", agg.TimeoutSeconds)
	}
	if agg.RetryPolicy == nil || agg.RetryPolicy.MaxAttempts != 1 {
		t.Fatalf("RetryPolicy = %+v, want max_attempts=1", agg.RetryPolicy)
	}
	if agg.AggregationMethod != workflow.AggMethodMajorityVote {
		t.Fatalf("AggregationMethod = %q", agg.AggregationMethod)
	}
	if got := agg.AggregationConfig["tie_breaker_method"]; got != "first" {
		t.Fatalf("tie_breaker_method = %#v, want first", got)
	}
	if _, exists := agg.AggregationConfig["tie_breaker"]; exists {
		t.Fatalf("legacy tie_breaker should be normalized out: %+v", agg.AggregationConfig)
	}
	if got := agg.Metadata["presentation_result_id"]; got != "visible-result" {
		t.Fatalf("presentation_result_id = %#v", got)
	}
	if got := agg.Metadata["benchmark_output_packaging"]; got != true {
		t.Fatalf("benchmark_output_packaging = %#v, want true", got)
	}
	inputIDs, ok := agg.Metadata["input_ids"].([]string)
	if !ok || len(inputIDs) != 2 || inputIDs[0] != "agent-a" || inputIDs[1] != "agent-b" {
		t.Fatalf("input_ids = %#v", agg.Metadata["input_ids"])
	}
}

func TestWorkflowConverter_AggregationMacroWithoutWorkflowIDStaysLegacyResult(t *testing.T) {
	raw := `{
	  "id":"parent",
	  "nodes":[
	    {"id":"agent-a","type":"agent","data":{"type":"agent","config":{"model":"openai/gpt-4o-mini","userPrompt":"Answer A","maxTokens":256}}},
	    {"id":"agent-b","type":"agent","data":{"type":"agent","config":{"model":"openai/gpt-4o-mini","userPrompt":"Answer B","maxTokens":256}}},
	    {"id":"agg","type":"aggregation","data":{"type":"aggregation","config":{
	      "aggregationMethod":"judge",
	      "aggregationConfig":{"judge_model":"openai/gpt-4o-mini","prompt":"Pick one","repair_max_tokens":16}
	    }}},
	    {"id":"visible-result","type":"result","data":{"type":"result","config":{"name":"final_answer","outputFormat":"text"}}}
	  ],
	  "edges":[
	    {"source":"agent-a","target":"agg"},
	    {"source":"agent-b","target":"agg"},
	    {"source":"agg","target":"visible-result"}
	  ]
	}`
	wf, err := WorkflowFromDefinition(raw, map[string]interface{}{})
	if err != nil {
		t.Fatalf("WorkflowFromDefinition failed: %v", err)
	}
	var agg *workflow.Node
	for _, node := range wf.Nodes {
		if node.ID == "agg" {
			agg = node
		}
		if node.ID == "visible-result" {
			t.Fatalf("presentation result should not be emitted as runtime node")
		}
	}
	if agg == nil {
		t.Fatalf("aggregation node missing: %+v", wf.Nodes)
	}
	if agg.Type != workflow.NodeTypeResult {
		t.Fatalf("aggregation macro without aggregationWorkflowId type = %q, want legacy result", agg.Type)
	}
	if agg.WorkflowRefID != "" {
		t.Fatalf("legacy aggregation unexpectedly populated WorkflowRefID = %q", agg.WorkflowRefID)
	}
	if agg.AggregationMethod != workflow.AggMethodJudge {
		t.Fatalf("AggregationMethod = %q, want judge", agg.AggregationMethod)
	}
	if got := agg.Metadata["presentation_result_id"]; got != "visible-result" {
		t.Fatalf("presentation_result_id = %#v", got)
	}
	if got := agg.Metadata["aggregation_method"]; got != "judge" {
		t.Fatalf("metadata aggregation_method = %#v, want judge", got)
	}
}

func TestWorkflowFromDefinitionOperation(t *testing.T) {
	raw := `{
		"id":"operation-wrapper",
		"name":"Operation Wrapper",
		"nodes":[
			{"id":"agent-a","type":"agent","data":{"type":"agent","label":"Agent A","config":{
				"name":"Agent A",
				"model":"mock-model",
				"systemPrompt":"Answer A",
				"maxTokens":1000,
				"timeoutSeconds":120
			}}},
			{"id":"vote-counter","type":"operation","data":{"type":"operation","label":"Vote Counter","config":{
				"name":"Count votes",
				"description":"Tally extracted answers",
				"operationType":"count_votes",
				"operationConfig":{"answers":"{{answers}}"}
			}}},
			{"id":"result","type":"result","data":{"type":"result","config":{"name":"answer","aggregationMethod":"collect"}}}
		],
		"edges":[
			{"source":"agent-a","target":"vote-counter"},
			{"source":"vote-counter","target":"result"}
		]
	}`

	wf, err := WorkflowFromDefinition(raw, map[string]interface{}{"answers": []string{"A", "B", "A"}})
	if err != nil {
		t.Fatalf("WorkflowFromDefinition failed: %v", err)
	}
	if len(wf.Nodes) != 3 {
		t.Fatalf("expected 3 executable nodes, got %d", len(wf.Nodes))
	}
	op := wf.Nodes[1]
	if op.Type != workflow.NodeTypeOperation {
		t.Fatalf("expected operation node, got %s", op.Type)
	}
	if op.OperationType != workflow.OperationCountVotes {
		t.Fatalf("expected operation_type=count_votes, got %q", op.OperationType)
	}
	if op.OperationConfig["answers"] != "{{answers}}" {
		t.Fatalf("unexpected operation_config: %+v", op.OperationConfig)
	}
	if op.RetryPolicy != nil {
		t.Fatalf("operation node should not get a retry policy from converter, got %+v", op.RetryPolicy)
	}
	if op.Metadata["description"] != "Tally extracted answers" {
		t.Fatalf("expected operation metadata to preserve description, got %+v", op.Metadata)
	}
}

func TestWorkflowFromDefinitionAgentRun(t *testing.T) {
	raw := `{
		"id":"agent-run-wrapper",
		"name":"Agent Run Wrapper",
		"nodes":[
			{"id":"input","type":"input","data":{"type":"input","config":{"name":"user_prompt"}}},
			{"id":"agent-run","type":"agent_run","data":{"type":"agent_run","config":{
				"name":"Novomo Agent",
				"prompt":"Investigate {{user_prompt}}",
				"harness":"claude-code",
				"sandbox":"host",
				"timeoutSeconds":120,
				"retryPolicy":{"max_attempts":2,"backoff_ms":1}
			}}},
			{"id":"result","type":"result","data":{"type":"result","config":{"name":"answer","aggregationMethod":"collect"}}}
		],
		"edges":[
			{"source":"input","target":"agent-run"},
			{"source":"agent-run","target":"result"}
		]
	}`

	wf, err := WorkflowFromDefinition(raw, map[string]interface{}{"user_prompt": "durability"})
	if err != nil {
		t.Fatalf("WorkflowFromDefinition failed: %v", err)
	}
	if len(wf.Nodes) != 2 {
		t.Fatalf("expected 2 executable nodes, got %d", len(wf.Nodes))
	}
	agent := wf.Nodes[0]
	if agent.Type != workflow.NodeTypeAgentRun {
		t.Fatalf("expected first node to be agent_run, got %s", agent.Type)
	}
	if agent.Prompt != "Investigate {{user_prompt}}" {
		t.Fatalf("prompt should preserve explicit placeholder, got %q", agent.Prompt)
	}
	if agent.Harness != "claude-code" || agent.Sandbox != "host" || agent.TimeoutSeconds != 120 {
		t.Fatalf("unexpected agent run config: %+v", agent)
	}
	if agent.RetryPolicy == nil || agent.RetryPolicy.MaxAttempts != 2 {
		t.Fatalf("retry policy not converted: %+v", agent.RetryPolicy)
	}
}

func TestWorkflowFromDefinitionAgentRunDefaultsSandboxToDocker(t *testing.T) {
	raw := `{
		"id":"agent-run-wrapper",
		"name":"Agent Run Wrapper",
		"nodes":[
			{"id":"agent-run","type":"agent_run","data":{"type":"agent_run","config":{
				"prompt":"Investigate",
				"harness":"claude-code",
				"timeoutSeconds":120
			}}}
		],
		"edges":[]
	}`

	wf, err := WorkflowFromDefinition(raw, nil)
	if err != nil {
		t.Fatalf("WorkflowFromDefinition failed: %v", err)
	}
	if len(wf.Nodes) != 1 {
		t.Fatalf("expected 1 executable node, got %d", len(wf.Nodes))
	}
	if wf.Nodes[0].Sandbox != "docker" {
		t.Fatalf("Sandbox = %q, want docker", wf.Nodes[0].Sandbox)
	}
}

func TestWorkflowFromDefinitionAgentRunInheritFromExplicitHandle(t *testing.T) {
	raw := `{
		"id":"agent-run-wrapper",
		"name":"Agent Run Wrapper",
		"nodes":[
			{"id":"agent-run","type":"agent_run","data":{"type":"agent_run","config":{
				"prompt":"Investigate",
				"harness":"claude-code",
				"timeoutSeconds":120,
				"inheritFromMode":"explicit",
				"inheritFromKind":"job_run",
				"inheritFromId":"jobrun-123",
				"inheritFromPolicy":"latest"
			}}}
		],
		"edges":[]
	}`

	wf, err := WorkflowFromDefinition(raw, nil)
	if err != nil {
		t.Fatalf("WorkflowFromDefinition failed: %v", err)
	}
	if got := wf.Nodes[0].InheritFrom; got == nil || got.Kind != "job_run" || got.ID != "jobrun-123" || got.Policy != "latest" {
		t.Fatalf("inherit_from not converted: %+v", got)
	}
}

func TestWorkflowFromDefinitionAgentRunInheritFromNoneIgnoresStaleFields(t *testing.T) {
	raw := `{
		"id":"agent-run-wrapper",
		"name":"Agent Run Wrapper",
		"nodes":[
			{"id":"agent-run","type":"agent_run","data":{"type":"agent_run","config":{
				"prompt":"Investigate",
				"harness":"claude-code",
				"timeoutSeconds":120,
				"inheritFromMode":"none",
				"inheritFromKind":"job_run",
				"inheritFromId":"",
				"inheritFromNodeId":"previous-node",
				"inheritFromPolicy":"latest"
			}}}
		],
		"edges":[]
	}`

	wf, err := WorkflowFromDefinition(raw, nil)
	if err != nil {
		t.Fatalf("WorkflowFromDefinition failed: %v", err)
	}
	node := wf.Nodes[0]
	if node.InheritFrom != nil || node.InheritFromNodeID != "" || node.InheritFromPolicy != "" {
		t.Fatalf("explicit none should ignore stale handoff fields: %+v", node)
	}
}

func TestWorkflowFromDefinitionAgentRunPreservesInvalidSandboxForValidation(t *testing.T) {
	raw := `{
		"id":"agent-run-wrapper",
		"name":"Agent Run Wrapper",
		"nodes":[
			{"id":"agent-run","type":"agent_run","data":{"type":"agent_run","config":{
				"prompt":"Investigate",
				"harness":"claude-code",
				"sandbox":" vm ",
				"timeoutSeconds":120
			}}}
		],
		"edges":[]
	}`

	wf, err := WorkflowFromDefinition(raw, nil)
	if err != nil {
		t.Fatalf("WorkflowFromDefinition failed: %v", err)
	}
	if len(wf.Nodes) != 1 {
		t.Fatalf("expected 1 executable node, got %d", len(wf.Nodes))
	}
	if wf.Nodes[0].Sandbox != "vm" {
		t.Fatalf("Sandbox = %q, want vm", wf.Nodes[0].Sandbox)
	}

	res := workflow.NewValidator(nil).Validate(wf)
	if res.Valid {
		t.Fatal("expected invalid sandbox to fail workflow validation")
	}
}

func TestWorkflowFromDefinitionNovoRun(t *testing.T) {
	raw := `{
		"id":"superagent-wrapper",
		"name":"Superagent Wrapper",
		"nodes":[
			{"id":"input","type":"input","data":{"type":"input","config":{"name":"user_prompt"}}},
			{"id":"superagent","type":"novo_run","data":{"type":"novo_run","label":"Superagent","config":{
				"name":"Superagent",
				"prompt":"Investigate {{user_prompt}}",
				"taskSummary":"brief",
				"identity":"sde-novo",
				"sandbox":"host",
				"runtimeUrl":"http://127.0.0.1:18082",
				"timeoutSeconds":120,
				"graceSeconds":5,
				"repoSpecsJson":"[{\"name\":\"app\",\"source\":{\"type\":\"host_path\",\"host_path\":{\"path\":\"/tmp/app\"}}}]",
				"workSourceJson":"{\"type\":\"gitea_branch\",\"gitea_branch\":{\"branch_ref\":\"main\"}}",
				"retryPolicy":{"max_attempts":2,"backoff_ms":1}
			}}},
			{"id":"result","type":"result","data":{"type":"result","config":{"name":"answer","aggregationMethod":"collect"}}}
		],
		"edges":[
			{"source":"input","target":"superagent"},
			{"source":"superagent","target":"result"}
		]
	}`

	wf, err := WorkflowFromDefinition(raw, map[string]interface{}{"user_prompt": "durability"})
	if err != nil {
		t.Fatalf("WorkflowFromDefinition failed: %v", err)
	}
	if len(wf.Nodes) != 2 {
		t.Fatalf("expected 2 executable nodes, got %d", len(wf.Nodes))
	}
	node := wf.Nodes[0]
	if node.Type != workflow.NodeTypeNovoRun {
		t.Fatalf("expected first node to be novo_run, got %s", node.Type)
	}
	if node.Prompt != "Investigate {{user_prompt}}" || node.TaskSummary != "brief" {
		t.Fatalf("unexpected prompt/task summary: %+v", node)
	}
	if node.Identity != "sde-novo" || node.Sandbox != "host" || node.RuntimeURL != "http://127.0.0.1:18082" {
		t.Fatalf("unexpected wake config: %+v", node)
	}
	if node.TimeoutSeconds != 120 || node.GraceSeconds != 5 {
		t.Fatalf("unexpected durations: %+v", node)
	}
	if len(node.RepoSpecs) != 1 || node.WorkSource["type"] != "gitea_branch" {
		t.Fatalf("repo/work source not converted: %+v", node)
	}
	if node.RetryPolicy == nil || node.RetryPolicy.MaxAttempts != 2 {
		t.Fatalf("retry policy not converted: %+v", node.RetryPolicy)
	}
}

func TestWorkflowFromDefinitionNovoRunInheritFromUpstreamNode(t *testing.T) {
	raw := `{
		"id":"superagent-wrapper",
		"name":"Superagent Wrapper",
		"nodes":[
			{"id":"upstream-agent","type":"agent_run","data":{"type":"agent_run","config":{
				"prompt":"First",
				"harness":"claude-code",
				"timeoutSeconds":120
			}}},
			{"id":"superagent","type":"novo_run","data":{"type":"novo_run","label":"Superagent","config":{
				"prompt":"Continue",
				"timeoutSeconds":120,
				"inheritFromMode":"upstream",
				"inheritFromNodeId":"upstream-agent",
				"inheritFromPolicy":"latest"
			}}}
		],
		"edges":[{"source":"upstream-agent","target":"superagent"}]
	}`

	wf, err := WorkflowFromDefinition(raw, nil)
	if err != nil {
		t.Fatalf("WorkflowFromDefinition failed: %v", err)
	}
	if len(wf.Nodes) != 2 {
		t.Fatalf("expected 2 executable nodes, got %d", len(wf.Nodes))
	}
	node := wf.Nodes[1]
	if node.InheritFromNodeID != "upstream-agent" || node.InheritFromPolicy != "latest" {
		t.Fatalf("upstream handoff not converted: %+v", node)
	}
}

func TestWorkflowFromDefinitionNovoRunAutoInheritFromNearestNovomoAncestor(t *testing.T) {
	raw := `{
		"id":"superagent-wrapper",
		"name":"Superagent Wrapper",
		"nodes":[
			{"id":"upstream-agent","type":"agent_run","data":{"type":"agent_run","config":{
				"prompt":"First",
				"harness":"claude-code",
				"timeoutSeconds":120
			}}},
			{"id":"classical-agent","type":"agent","data":{"type":"agent","config":{
				"model":"openai/gpt-4o-mini",
				"userPrompt":"Bridge",
				"maxTokens":1000
			}}},
			{"id":"superagent","type":"novo_run","data":{"type":"novo_run","label":"Superagent","config":{
				"prompt":"Continue",
				"timeoutSeconds":120,
				"inheritFromMode":"auto",
				"inheritFromPolicy":"latest"
			}}}
		],
		"edges":[
			{"source":"upstream-agent","target":"classical-agent"},
			{"source":"classical-agent","target":"superagent"}
		]
	}`

	wf, err := WorkflowFromDefinition(raw, nil)
	if err != nil {
		t.Fatalf("WorkflowFromDefinition failed: %v", err)
	}
	if len(wf.Nodes) != 3 {
		t.Fatalf("expected 3 executable nodes, got %d", len(wf.Nodes))
	}
	node := wf.Nodes[2]
	if node.InheritFromNodeID != "upstream-agent" || node.InheritFromPolicy != "latest" {
		t.Fatalf("auto handoff not converted: %+v", node)
	}
}

func TestWorkflowFromDefinitionAgentRunAutoFanInUsesWorkflowTaskHandoff(t *testing.T) {
	raw := `{
		"id":"agent-run-fanin",
		"name":"Agent Run Fan-In",
		"nodes":[
			{"id":"repo","type":"agent_run","data":{"type":"agent_run","config":{
				"prompt":"Create repo",
				"harness":"claude-code",
				"timeoutSeconds":120,
				"inheritFromMode":"none"
			}}},
			{"id":"octopus","type":"agent_run","data":{"type":"agent_run","config":{
				"prompt":"Add octopus",
				"harness":"claude-code",
				"timeoutSeconds":120,
				"inheritFromMode":"auto"
			}}},
			{"id":"jellyfish","type":"agent_run","data":{"type":"agent_run","config":{
				"prompt":"Add jellyfish",
				"harness":"claude-code",
				"timeoutSeconds":120,
				"inheritFromMode":"auto"
			}}},
			{"id":"qa","type":"agent_run","data":{"type":"agent_run","label":"Novomo Agent","config":{
				"prompt":"Review both branches",
				"harness":"claude-code",
				"timeoutSeconds":120,
				"inheritFromMode":"auto",
				"inheritFromPolicy":"latest"
			}}}
		],
		"edges":[
			{"source":"repo","target":"octopus"},
			{"source":"repo","target":"jellyfish"},
			{"source":"octopus","target":"qa"},
			{"source":"jellyfish","target":"qa"}
		]
	}`

	wf, err := WorkflowFromDefinition(raw, nil)
	if err != nil {
		t.Fatalf("WorkflowFromDefinition failed: %v", err)
	}
	var qa *workflow.Node
	for _, node := range wf.Nodes {
		if node.ID == "qa" {
			qa = node
			break
		}
	}
	if qa == nil {
		t.Fatal("qa node not converted")
	}
	if !qa.InheritFromWorkflowTask || qa.InheritFromNodeID != "" || qa.InheritFrom != nil {
		t.Fatalf("fan-in auto handoff should use workflow task: %+v", qa)
	}
	if qa.InheritFromPolicy != "latest" {
		t.Fatalf("inherit policy = %q, want latest", qa.InheritFromPolicy)
	}
}

func TestWorkflowFromDefinitionRejectsIncompleteUpstreamHandoff(t *testing.T) {
	raw := `{
		"id":"superagent-wrapper",
		"name":"Superagent Wrapper",
		"nodes":[
			{"id":"superagent","type":"novo_run","data":{"type":"novo_run","label":"Superagent","config":{
				"prompt":"Continue",
				"timeoutSeconds":120,
				"inheritFromMode":"upstream"
			}}}
		],
		"edges":[]
	}`

	if _, err := WorkflowFromDefinition(raw, nil); err == nil {
		t.Fatal("expected incomplete upstream handoff to fail conversion")
	}
}

func TestWorkflowFromDefinitionAgentToolsMetadata(t *testing.T) {
	raw := `{
		"id":"benchmark-toolcall-wrapper",
		"name":"Benchmark Toolcall Wrapper",
		"nodes":[
			{"id":"input","type":"input","data":{"type":"input","config":{"name":"user_prompt"}}},
			{"id":"contract","type":"agent","data":{"type":"agent","config":{
				"name":"Benchmark Contract",
				"model":"openai/gpt-4o-mini",
				"userPrompt":"{{user_prompt}}",
				"maxTokens":1024,
				"tools":[
					{
						"type":"function",
						"function":{
							"name":"submit_answer",
							"parameters":{
								"type":"object",
								"properties":{"answer":{"type":"string"}},
								"required":["answer"]
							}
						}
					}
				],
				"toolChoice":"required"
			}}},
			{"id":"result","type":"result","data":{"type":"result","config":{"name":"benchmark_answer","aggregationMethod":"collect"}}}
		],
		"edges":[
			{"source":"input","target":"contract"},
			{"source":"contract","target":"result"}
		]
	}`

	wf, err := WorkflowFromDefinition(raw, map[string]interface{}{"user_prompt": "Q"})
	if err != nil {
		t.Fatalf("WorkflowFromDefinition failed: %v", err)
	}
	if len(wf.Nodes) != 2 {
		t.Fatalf("expected 2 executable nodes, got %d", len(wf.Nodes))
	}
	if wf.Nodes[0].Type != "prompt" {
		t.Fatalf("expected first node to be prompt, got %s", wf.Nodes[0].Type)
	}

	metadata := wf.Nodes[0].Metadata
	if _, ok := metadata["tools"]; !ok {
		t.Fatalf("expected tools metadata on agent node, got %+v", metadata)
	}
	if got, ok := metadata["tool_choice"]; !ok || got != "required" {
		t.Fatalf("expected tool_choice=required, got %+v", metadata["tool_choice"])
	}
}

func TestWorkflowFromDefinitionAgentOpenRouterProviderMetadata(t *testing.T) {
	raw := `{
		"id":"provider-routing-wrapper",
		"name":"Provider Routing Wrapper",
		"nodes":[
			{"id":"input","type":"input","data":{"type":"input","config":{"name":"user_prompt"}}},
			{"id":"agent","type":"agent","data":{"type":"agent","config":{
				"name":"Provider-Routed Agent",
				"model":"openai/gpt-4o-mini",
				"userPrompt":"{{user_prompt}}",
				"maxTokens":1024,
				"openRouterProvider":{
					"only":["OpenAI"],
					"allow_fallbacks":false
				}
			}}},
			{"id":"result","type":"result","data":{"type":"result","config":{"name":"answer","aggregationMethod":"collect"}}}
		],
		"edges":[
			{"source":"input","target":"agent"},
			{"source":"agent","target":"result"}
		]
	}`

	wf, err := WorkflowFromDefinition(raw, map[string]interface{}{"user_prompt": "Q"})
	if err != nil {
		t.Fatalf("WorkflowFromDefinition failed: %v", err)
	}
	if len(wf.Nodes) != 2 {
		t.Fatalf("expected 2 executable nodes, got %d", len(wf.Nodes))
	}
	metadata := wf.Nodes[0].Metadata
	routingRaw, ok := metadata["openrouter_provider"]
	if !ok {
		t.Fatalf("expected openrouter_provider metadata on agent node, got %+v", metadata)
	}

	routing, ok := routingRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("expected openrouter_provider to be object, got %T", routingRaw)
	}

	if got, ok := routing["allow_fallbacks"].(bool); !ok || got {
		t.Fatalf("expected allow_fallbacks=false, got %+v", routing["allow_fallbacks"])
	}
	onlyRaw, ok := routing["only"].([]interface{})
	if !ok || len(onlyRaw) != 1 || onlyRaw[0] != "OpenAI" {
		t.Fatalf("expected only=[OpenAI], got %+v", routing["only"])
	}
}

func TestWorkflowFromDefinitionAgentOpenRouterReasoningMetadata(t *testing.T) {
	raw := `{
		"id":"reasoning-wrapper",
		"name":"Reasoning Wrapper",
		"nodes":[
			{"id":"input","type":"input","data":{"type":"input","config":{"name":"user_prompt"}}},
			{"id":"agent","type":"agent","data":{"type":"agent","config":{
				"name":"Reasoning Agent",
				"model":"xiaomi/mimo-v2-flash",
				"userPrompt":"{{user_prompt}}",
				"maxTokens":1024,
				"openRouterReasoning":{
					"effort":"high"
				}
			}}},
			{"id":"result","type":"result","data":{"type":"result","config":{"name":"answer","aggregationMethod":"collect"}}}
		],
		"edges":[
			{"source":"input","target":"agent"},
			{"source":"agent","target":"result"}
		]
	}`

	wf, err := WorkflowFromDefinition(raw, map[string]interface{}{"user_prompt": "Q"})
	if err != nil {
		t.Fatalf("WorkflowFromDefinition failed: %v", err)
	}
	if len(wf.Nodes) != 2 {
		t.Fatalf("expected 2 executable nodes, got %d", len(wf.Nodes))
	}
	metadata := wf.Nodes[0].Metadata
	reasoningRaw, ok := metadata["openrouter_reasoning"]
	if !ok {
		t.Fatalf("expected openrouter_reasoning metadata on agent node, got %+v", metadata)
	}

	reasoning, ok := reasoningRaw.(map[string]interface{})
	if !ok {
		t.Fatalf("expected openrouter_reasoning to be object, got %T", reasoningRaw)
	}

	if got, ok := reasoning["effort"].(string); !ok || got != "high" {
		t.Fatalf("expected effort=high, got %+v", reasoning["effort"])
	}
}

func TestWorkflowFromDefinitionResultOpenRouterConfigMergedIntoAggregationConfig(t *testing.T) {
	raw := `{
		"id":"result-openrouter-config",
		"name":"Result OpenRouter Config",
		"nodes":[
			{"id":"input","type":"input","data":{"type":"input","config":{"name":"user_prompt"}}},
			{"id":"agent","type":"agent","data":{"type":"agent","config":{
				"name":"Reasoning Agent",
				"model":"xiaomi/mimo-v2-flash",
				"userPrompt":"{{user_prompt}}",
				"maxTokens":1024
			}}},
			{"id":"result","type":"result","data":{"type":"result","config":{
				"name":"answer",
				"aggregationMethod":"judge",
				"aggregationConfig":{"judge_model":"openai/gpt-4o-mini"},
				"openRouterProvider":{"only":["OpenAI"],"allow_fallbacks":false},
				"openRouterReasoning":{"effort":"high"}
			}}}
		],
		"edges":[
			{"source":"input","target":"agent"},
			{"source":"agent","target":"result"}
		]
	}`

	wf, err := WorkflowFromDefinition(raw, map[string]interface{}{"user_prompt": "Q"})
	if err != nil {
		t.Fatalf("WorkflowFromDefinition failed: %v", err)
	}
	if len(wf.Nodes) != 2 {
		t.Fatalf("expected 2 executable nodes, got %d", len(wf.Nodes))
	}
	result := wf.Nodes[1]
	if result.Type != workflow.NodeTypeResult {
		t.Fatalf("expected second node to be result, got %s", result.Type)
	}
	if result.AggregationConfig == nil {
		t.Fatalf("expected aggregation config to be set")
	}
	if result.AggregationConfig["judge_model"] != "openai/gpt-4o-mini" {
		t.Fatalf("expected judge_model to be preserved, got %+v", result.AggregationConfig["judge_model"])
	}
	if result.AggregationConfig["openRouterProvider"] == nil {
		t.Fatalf("expected openRouterProvider to be merged into aggregation config, got %+v", result.AggregationConfig)
	}
	if result.AggregationConfig["openRouterReasoning"] == nil {
		t.Fatalf("expected openRouterReasoning to be merged into aggregation config, got %+v", result.AggregationConfig)
	}
}

func TestWorkflowFromDefinitionAgentMissingMaxTokensReturnsError(t *testing.T) {
	raw := `{
		"id":"missing-max-tokens",
		"name":"Missing Max Tokens",
		"nodes":[
			{"id":"input","type":"input","data":{"type":"input","config":{"name":"user_prompt"}}},
			{"id":"agent","type":"agent","data":{"type":"agent","config":{
				"name":"Reasoning Agent",
				"model":"xiaomi/mimo-v2-flash",
				"userPrompt":"{{user_prompt}}"
			}}},
			{"id":"result","type":"result","data":{"type":"result","config":{"name":"answer","aggregationMethod":"collect"}}}
		],
		"edges":[
			{"source":"input","target":"agent"},
			{"source":"agent","target":"result"}
		]
	}`

	_, err := WorkflowFromDefinition(raw, map[string]interface{}{"user_prompt": "Q"})
	if err == nil {
		t.Fatalf("expected error for missing maxTokens")
	}
	if got := err.Error(); got != `workflow node "agent" (agent) is missing required config.maxTokens` {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWorkflowFromDefinitionAgentNegativeMaxTokensPreserved(t *testing.T) {
	raw := `{
		"id":"negative-max-tokens",
		"name":"Negative Max Tokens",
		"nodes":[
			{"id":"input","type":"input","data":{"type":"input","config":{"name":"user_prompt"}}},
			{"id":"agent","type":"agent","data":{"type":"agent","config":{
				"name":"Reasoning Agent",
				"model":"xiaomi/mimo-v2-flash",
				"userPrompt":"{{user_prompt}}",
				"maxTokens":-1
			}}},
			{"id":"result","type":"result","data":{"type":"result","config":{"name":"answer","aggregationMethod":"collect"}}}
		],
		"edges":[
			{"source":"input","target":"agent"},
			{"source":"agent","target":"result"}
		]
	}`

	wf, err := WorkflowFromDefinition(raw, map[string]interface{}{"user_prompt": "Q"})
	if err != nil {
		t.Fatalf("WorkflowFromDefinition failed: %v", err)
	}
	if len(wf.Nodes) != 2 {
		t.Fatalf("expected 2 executable nodes, got %d", len(wf.Nodes))
	}
	if wf.Nodes[0].MaxTokens != -1 {
		t.Fatalf("expected max_tokens=-1 to be preserved, got %d", wf.Nodes[0].MaxTokens)
	}
}

func TestWorkflowFromDefinitionContractExtract(t *testing.T) {
	raw := `{
		"id":"benchmark-test",
		"name":"Benchmark Test",
		"nodes":[
			{"id":"input","type":"input","data":{"type":"input","config":{"name":"user_prompt"}}},
			{"id":"child","type":"child_workflow","data":{"type":"child_workflow","config":{
				"childWorkflowId":"reasoning-test",
				"inputTemplate":{"user_prompt":"{{user_prompt}}"},
				"outputKey":"result",
				"await":true
			}}},
			{"id":"agent-contract","type":"contract_extract","data":{"type":"contract_extract","config":{
				"sourceVariable":"child",
				"extractionPatterns":[
					"^\\s*([A-Za-z])\\s*$",
					"(?i)\\b(?:final\\s+answer|answer)\\b(?:\\s+is)?\\s*[:\\-]?\\s*\\(?\\s*([A-Za-z])\\s*\\)?\\s*(?:$|\\n|[^A-Za-z])",
					"^\\s*\\(?\\s*([A-Za-z])\\s*\\)?[\\.\\):\\-]"
				],
				"name":"Benchmark Contract",
				"model":"x-ai/grok-4.1-fast",
				"systemPrompt":"Extract answer.",
				"userPrompt":"Source: {{child}}\nExtract:",
				"temperature":0,
				"maxTokens":1024,
				"timeoutSeconds":20,
				"openRouterReasoning":{"effort":"none"}
			}}},
			{"id":"result","type":"result","data":{"type":"result","config":{"name":"benchmark_answer","aggregationMethod":"collect"}}}
		],
		"edges":[
			{"source":"input","target":"child"},
			{"source":"child","target":"agent-contract"},
			{"source":"agent-contract","target":"result"}
		]
	}`

	wf, err := WorkflowFromDefinition(raw, map[string]interface{}{"user_prompt": "Q"})
	if err != nil {
		t.Fatalf("WorkflowFromDefinition failed: %v", err)
	}
	if len(wf.Nodes) != 3 {
		t.Fatalf("expected 3 executable nodes, got %d", len(wf.Nodes))
	}

	// Find the contract_extract node
	var contract *workflow.Node
	for _, n := range wf.Nodes {
		if n.ID == "agent-contract" {
			contract = n
			break
		}
	}
	if contract == nil {
		t.Fatal("contract_extract node not found")
	}

	// Verify type
	if contract.Type != workflow.NodeTypeContractExtract {
		t.Fatalf("expected type contract_extract, got %s", contract.Type)
	}

	// Verify model and prompt preserved for LLM fallback
	if contract.Model != "x-ai/grok-4.1-fast" {
		t.Fatalf("expected model x-ai/grok-4.1-fast, got %s", contract.Model)
	}
	if contract.SystemPrompt != "Extract answer." {
		t.Fatalf("expected system prompt preserved, got %q", contract.SystemPrompt)
	}
	if contract.MaxTokens != 1024 {
		t.Fatalf("expected maxTokens=1024, got %d", contract.MaxTokens)
	}
	if contract.TimeoutSeconds != 20 {
		t.Fatalf("expected timeoutSeconds=20, got %d", contract.TimeoutSeconds)
	}

	// Verify metadata
	sv, ok := contract.Metadata["source_variable"].(string)
	if !ok || sv != "child" {
		t.Fatalf("expected source_variable=child, got %v", contract.Metadata["source_variable"])
	}

	patterns, ok := contract.Metadata["extraction_patterns"].([]string)
	if !ok || len(patterns) != 3 {
		t.Fatalf("expected 3 extraction_patterns, got %v", contract.Metadata["extraction_patterns"])
	}

	reasoning, ok := contract.Metadata["openrouter_reasoning"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected openrouter_reasoning in metadata, got %v", contract.Metadata["openrouter_reasoning"])
	}
	if reasoning["effort"] != "none" {
		t.Fatalf("expected effort=none, got %v", reasoning["effort"])
	}
}

func TestWorkflowFromDefinitionContractExtractNoPatterns(t *testing.T) {
	raw := `{
		"id":"benchmark-no-patterns",
		"name":"No Patterns",
		"nodes":[
			{"id":"input","type":"input","data":{"type":"input","config":{"name":"user_prompt"}}},
			{"id":"agent-contract","type":"contract_extract","data":{"type":"contract_extract","config":{
				"sourceVariable":"input",
				"name":"Contract",
				"model":"x-ai/grok-4.1-fast",
				"userPrompt":"{{user_prompt}}",
				"maxTokens":1024
			}}},
			{"id":"result","type":"result","data":{"type":"result","config":{"name":"answer","aggregationMethod":"collect"}}}
		],
		"edges":[
			{"source":"input","target":"agent-contract"},
			{"source":"agent-contract","target":"result"}
		]
	}`

	wf, err := WorkflowFromDefinition(raw, map[string]interface{}{"user_prompt": "Q"})
	if err != nil {
		t.Fatalf("WorkflowFromDefinition failed: %v", err)
	}

	var contract *workflow.Node
	for _, n := range wf.Nodes {
		if n.ID == "agent-contract" {
			contract = n
			break
		}
	}
	if contract == nil {
		t.Fatal("contract_extract node not found")
	}
	if contract.Type != workflow.NodeTypeContractExtract {
		t.Fatalf("expected type contract_extract, got %s", contract.Type)
	}

	// No extraction_patterns key should be absent from metadata
	if _, ok := contract.Metadata["extraction_patterns"]; ok {
		t.Fatal("expected no extraction_patterns in metadata when not specified")
	}
}

func TestWorkflowFromDefinitionRetryPolicyParsedForChildWorkflow(t *testing.T) {
	raw := `{
		"id":"retry-policy-wrapper",
		"name":"Retry Policy Wrapper",
		"nodes":[
			{"id":"input","type":"input","data":{"type":"input","config":{"name":"user_prompt"}}},
			{"id":"child","type":"child_workflow","data":{"type":"child_workflow","config":{
				"childWorkflowId":"reasoning-informed-captain-synthesis",
				"inputTemplate":{"user_prompt":"{{user_prompt}}"},
				"timeoutSeconds":300,
				"retryPolicy":{
					"max_attempts":5,
					"backoff_ms":250,
					"retryable_errors":["TIMEOUT","RATE_LIMIT"],
					"adaptive_reasoning":{
						"trigger_error_codes":["OUTPUT_TRUNCATED_EMPTY"],
						"activate_after_consecutive":2,
						"ladder":["high","low","none"]
					}
				}
			}}},
			{"id":"result","type":"result","data":{"type":"result","config":{"name":"benchmark_answer","aggregationMethod":"collect"}}}
		],
		"edges":[
			{"source":"input","target":"child"},
			{"source":"child","target":"result"}
		]
	}`

	wf, err := WorkflowFromDefinition(raw, map[string]interface{}{"user_prompt": "Q"})
	if err != nil {
		t.Fatalf("WorkflowFromDefinition failed: %v", err)
	}
	if len(wf.Nodes) != 2 {
		t.Fatalf("expected 2 executable nodes, got %d", len(wf.Nodes))
	}
	child := wf.Nodes[0]
	if child.Type != workflow.NodeTypeChildWorkflow {
		t.Fatalf("expected first node child_workflow, got %s", child.Type)
	}
	if child.RetryPolicy == nil {
		t.Fatalf("expected retry policy to be parsed")
	}
	if child.RetryPolicy.MaxAttempts != 5 {
		t.Fatalf("expected max_attempts=5, got %d", child.RetryPolicy.MaxAttempts)
	}
	if child.RetryPolicy.AdaptiveReasoning == nil {
		t.Fatalf("expected adaptive_reasoning to be parsed")
	}
	if child.RetryPolicy.AdaptiveReasoning.ActivateAfterConsecutive != 2 {
		t.Fatalf("expected adaptive activate_after_consecutive=2, got %d", child.RetryPolicy.AdaptiveReasoning.ActivateAfterConsecutive)
	}
}
