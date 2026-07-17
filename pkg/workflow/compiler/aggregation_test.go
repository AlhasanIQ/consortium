package compiler

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/alhasaniq/consortium/pkg/workflow"
)

func TestCompileAggregationReferencesExpandMethodDAGs(t *testing.T) {
	tests := []struct {
		name          string
		method        workflow.AggregationMethod
		workflowID    string
		config        map[string]interface{}
		wantNodeIDs   []string
		wantFanoutIDs []string
	}{
		{
			name:       "collect",
			method:     workflow.AggMethodCollect,
			workflowID: "aggregation-collect",
			config:     map[string]interface{}{"separator": "\n---\n"},
			wantNodeIDs: []string{
				"agg--collect-inputs",
				"agg--result",
			},
		},
		{
			name:       "majority_vote",
			method:     workflow.AggMethodMajorityVote,
			workflowID: "aggregation-majority-vote",
			config: map[string]interface{}{
				"extraction_strategy":     "regex",
				"extraction_pattern":      `Answer:\s*([A-D])`,
				"tie_breaker_method":      "synthesis",
				"tie_breaker_model":       "openai/gpt-5-mini",
				"tie_breaker_temperature": float64(0),
				"system_prompt":           "Synthesize the tied answers.",
				"prompt":                  "{{responses}}",
				"max_tokens":              float64(512),
			},
			wantNodeIDs: []string{
				"agg--extract-answer-agent-a",
				"agg--extract-answer-agent-b",
				"agg--count-votes",
				"agg--tie-policy",
				"agg--result",
			},
		},
		{
			name:       "synthesis",
			method:     workflow.AggMethodSynthesis,
			workflowID: "aggregation-synthesis",
			config: map[string]interface{}{
				"model":         "openai/gpt-5-mini",
				"system_prompt": "Synthesize.",
				"prompt":        "{{responses}}",
				"temperature":   float64(0),
				"max_tokens":    float64(512),
			},
			wantNodeIDs: []string{
				"agg--format-candidates",
				"agg--synthesize",
				"agg--result",
			},
		},
		{
			name:       "judge",
			method:     workflow.AggMethodJudge,
			workflowID: "aggregation-judge",
			config: map[string]interface{}{
				"judge_model":       "openai/gpt-5-mini",
				"system_prompt":     "Judge.",
				"prompt":            "{{responses}}",
				"temperature":       float64(0),
				"max_tokens":        float64(512),
				"repair_max_tokens": float64(128),
			},
			wantNodeIDs: []string{
				"agg--check-unanimous",
				"agg--format-blind-candidates",
				"agg--judge",
				"agg--parse-selection",
				"agg--repair-selection",
				"agg--select-winner",
				"agg--result",
			},
		},
		{
			name:       "debate_decide",
			method:     workflow.AggMethodDebateDecide,
			workflowID: "aggregation-debate-decide",
			config: map[string]interface{}{
				"judge_model":         "openai/gpt-5-mini",
				"system_prompt":       "Judge camps.",
				"prompt":              "{{camps}}",
				"extraction_strategy": "regex",
				"extraction_pattern":  `Answer:\s*([A-D])`,
				"temperature":         float64(0),
				"max_tokens":          float64(512),
				"repair_max_tokens":   float64(128),
			},
			wantNodeIDs: []string{
				"agg--extract-answer-agent-a",
				"agg--extract-answer-agent-b",
				"agg--group-camps",
				"agg--single-camp-policy",
				"agg--judge-camp",
				"agg--parse-selection",
				"agg--repair-selection",
				"agg--select-winner",
				"agg--result",
			},
		},
		{
			name:       "scoring",
			method:     workflow.AggMethodScoring,
			workflowID: "aggregation-scoring",
			config: map[string]interface{}{
				"scoring_model": "openai/gpt-5-mini",
				"system_prompt": "Score.",
				"prompt":        "{{candidate}}",
				"temperature":   float64(0),
				"max_tokens":    float64(512),
				"rubric": []interface{}{
					map[string]interface{}{"name": "correctness", "weight": float64(1), "description": "Correctness"},
				},
			},
			wantNodeIDs: []string{
				"agg--check-unanimous",
				"agg--reduce-scores",
				"agg--select-winner",
				"agg--result",
			},
			wantFanoutIDs: []string{
				"agg--score-agent-a",
				"agg--score-agent-b",
				"agg--parse-score-agent-a",
				"agg--parse-score-agent-b",
			},
		},
		{
			name:       "peer_matrix",
			method:     workflow.AggMethodPeerMatrix,
			workflowID: "aggregation-peer-matrix",
			config: map[string]interface{}{
				"eval_system_prompt": "Review peers.",
				"eval_prompt":        "{{candidate}}",
				"temperature":        float64(0),
				"max_tokens":         float64(512),
				"normalization":      "none",
				"max_parallel":       float64(6),
				"rubric": []interface{}{
					map[string]interface{}{"name": "correctness", "weight": float64(1), "description": "Correctness"},
				},
			},
			wantNodeIDs: []string{
				"agg--check-unanimous",
				"agg--reduce-peer-scores",
				"agg--select-winner",
				"agg--result",
			},
			wantFanoutIDs: []string{
				"agg--review-agent-a-agent-b",
				"agg--review-agent-b-agent-a",
				"agg--parse-review-agent-a-agent-b",
				"agg--parse-review-agent-b-agent-a",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := testAggregationParent(tt.method, tt.workflowID, tt.config)
			compiled, report, err := Compile(context.Background(), parent, StaticResolver{
				tt.workflowID: testAggregationSourceWorkflow(tt.workflowID),
			})
			if err != nil {
				t.Fatalf("Compile failed: %v", err)
			}
			assertNodeIDs(t, compiled, append(tt.wantNodeIDs, tt.wantFanoutIDs...)...)
			assertNoLegacyAggregationResult(t, compiled, tt.method)
			assertExpandedSourceMetadata(t, compiled, "agg", tt.workflowID)
			assertAggregationGroupMetadata(t, compiled, "agg", "agg--result")
			assertCompiledValidationPasses(t, compiled)
			assertReportIncludesAggregationRef(t, report, "agg", tt.workflowID)
		})
	}
}

func TestCompileAggregationReferenceAbsorbsPresentationResult(t *testing.T) {
	parent := testAggregationParent(workflow.AggMethodSynthesis, "aggregation-synthesis", map[string]interface{}{
		"model":         "openai/gpt-5-mini",
		"system_prompt": "Synthesize.",
		"prompt":        "{{responses}}",
		"temperature":   float64(0),
		"max_tokens":    float64(512),
	})
	parent.Nodes = append(parent.Nodes, testResultNode("presentation", []interface{}{"agg"}))
	parent.Nodes[len(parent.Nodes)-1].OutputName = "visible_answer"
	parent.Nodes[len(parent.Nodes)-1].OutputFormat = "json"
	parent.Edges = append(parent.Edges, &workflow.Edge{ID: "agg-presentation", Source: "agg", Target: "presentation"})

	compiled, _, err := Compile(context.Background(), parent, StaticResolver{
		"aggregation-synthesis": testAggregationSourceWorkflow("aggregation-synthesis"),
	})
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	if findNode(compiled, "presentation") != nil {
		t.Fatalf("presentation result should be absorbed, nodes=%s", nodeIDs(compiled))
	}
	result := findNode(compiled, "agg--result")
	if result == nil {
		t.Fatalf("compiled result missing, nodes=%s", nodeIDs(compiled))
	}
	if result.OutputName != "visible_answer" || result.OutputFormat != "json" {
		t.Fatalf("compiled result output = %q/%q, want visible_answer/json", result.OutputName, result.OutputFormat)
	}
}

func TestCompileAggregationReferencePreservesExecutionPolicy(t *testing.T) {
	parent := testAggregationParent(workflow.AggMethodJudge, "aggregation-judge", map[string]interface{}{
		"judge_model":       "openai/gpt-5-mini",
		"system_prompt":     "Judge.",
		"prompt":            "{{responses}}",
		"temperature":       float64(0),
		"max_tokens":        float64(512),
		"repair_max_tokens": float64(128),
	})
	agg := findNode(parent, "agg")
	agg.TimeoutSeconds = 123
	agg.RetryPolicy = &workflow.RetryPolicy{
		MaxAttempts:     1,
		RetryableErrors: []string{"CUSTOM_TRANSIENT"},
		AdaptiveReasoning: &workflow.AdaptiveReasoningPolicy{
			TriggerErrorCodes:        []string{workflow.RetryCodeTimeout},
			ActivateAfterConsecutive: 2,
			Ladder:                   []string{"high", "none"},
		},
	}

	compiled, _, err := Compile(context.Background(), parent, StaticResolver{
		"aggregation-judge": testAggregationSourceWorkflow("aggregation-judge"),
	})
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	for _, id := range []string{"agg--judge"} {
		node := findNode(compiled, id)
		if node == nil {
			t.Fatalf("compiled prompt node %q missing, nodes=%s", id, nodeIDs(compiled))
		}
		if node.TimeoutSeconds != 123 {
			t.Fatalf("%s TimeoutSeconds = %d, want 123", id, node.TimeoutSeconds)
		}
		if node.RetryPolicy == nil || node.RetryPolicy.MaxAttempts != 1 {
			t.Fatalf("%s RetryPolicy = %+v, want max_attempts=1", id, node.RetryPolicy)
		}
		if len(node.RetryPolicy.RetryableErrors) != 1 || node.RetryPolicy.RetryableErrors[0] != "CUSTOM_TRANSIENT" {
			t.Fatalf("%s RetryableErrors = %+v, want custom policy", id, node.RetryPolicy.RetryableErrors)
		}
		if node.RetryPolicy.AdaptiveReasoning == nil || node.RetryPolicy.AdaptiveReasoning.Ladder[0] != "high" {
			t.Fatalf("%s AdaptiveReasoning = %+v, want cloned policy", id, node.RetryPolicy.AdaptiveReasoning)
		}
	}
	compiledJudge := findNode(compiled, "agg--judge")
	compiledJudge.RetryPolicy.RetryableErrors[0] = "MUTATED"
	if agg.RetryPolicy.RetryableErrors[0] != "CUSTOM_TRANSIENT" {
		t.Fatalf("compiled retry policy mutation changed source aggregation node: %+v", agg.RetryPolicy)
	}

	repair := findNode(compiled, "agg--repair-selection")
	if repair == nil || repair.TrueBranch == nil {
		t.Fatalf("compiled repair conditional branch missing: %+v", repair)
	}
	if repair.TrueBranch.TimeoutSeconds != 123 {
		t.Fatalf("repair TrueBranch TimeoutSeconds = %d, want 123", repair.TrueBranch.TimeoutSeconds)
	}
	if repair.TrueBranch.RetryPolicy == nil || repair.TrueBranch.RetryPolicy.MaxAttempts != 1 {
		t.Fatalf("repair TrueBranch RetryPolicy = %+v, want max_attempts=1", repair.TrueBranch.RetryPolicy)
	}

	for _, id := range []string{"agg--repair-selection", "agg--result"} {
		node := findNode(compiled, id)
		if node == nil {
			t.Fatalf("compiled node %q missing, nodes=%s", id, nodeIDs(compiled))
		}
		if node.RetryPolicy == nil || node.RetryPolicy.MaxAttempts != 1 {
			t.Fatalf("%s RetryPolicy = %+v, want max_attempts=1", id, node.RetryPolicy)
		}
	}
}

func TestCompileAggregationReferenceRejectsMissingCandidates(t *testing.T) {
	parent := &workflow.Workflow{
		ID: "parent",
		Nodes: []*workflow.Node{{
			ID:                "agg",
			Type:              workflow.NodeTypeWorkflowRef,
			WorkflowRefID:     "aggregation-collect",
			AggregationMethod: workflow.AggMethodCollect,
		}},
	}

	_, _, err := Compile(context.Background(), parent, StaticResolver{
		"aggregation-collect": testAggregationSourceWorkflow("aggregation-collect"),
	})
	if err == nil || !strings.Contains(err.Error(), "requires at least one upstream candidate") {
		t.Fatalf("Compile error = %v, want missing-candidate contract error", err)
	}
}

func TestCompileAggregationReferenceRejectsUnknownMethod(t *testing.T) {
	parent := testAggregationParent(workflow.AggregationMethod("unsupported"), "custom-aggregation", nil)

	_, _, err := Compile(context.Background(), parent, StaticResolver{
		"custom-aggregation": testAggregationSourceWorkflow("custom-aggregation"),
	})
	if err == nil || !strings.Contains(err.Error(), "unknown aggregation method") {
		t.Fatalf("Compile error = %v, want unknown aggregation method", err)
	}
}

func TestCompileAggregationReferenceRejectsKnownSourceMethodMismatch(t *testing.T) {
	parent := testAggregationParent(workflow.AggMethodJudge, "aggregation-scoring", map[string]interface{}{
		"judge_model":       "openai/gpt-5-mini",
		"system_prompt":     "Judge.",
		"prompt":            "{{responses}}",
		"temperature":       float64(0),
		"max_tokens":        float64(512),
		"repair_max_tokens": float64(128),
	})

	_, _, err := Compile(context.Background(), parent, StaticResolver{
		"aggregation-scoring": testAggregationSourceWorkflow("aggregation-scoring"),
	})
	if err == nil {
		t.Fatal("Compile succeeded with aggregation-judge method pointing at aggregation-scoring source")
	}
	if !strings.Contains(err.Error(), "aggregation-scoring") ||
		!strings.Contains(err.Error(), "scoring") ||
		!strings.Contains(err.Error(), "judge") {
		t.Fatalf("Compile error = %v, want source/method mismatch details", err)
	}
}

func TestCompileAggregationReferenceRejectsConflictingMethodMetadata(t *testing.T) {
	parent := testAggregationParent(workflow.AggMethodJudge, "custom-aggregation", map[string]interface{}{
		"judge_model":       "openai/gpt-5-mini",
		"system_prompt":     "Judge.",
		"prompt":            "{{responses}}",
		"temperature":       float64(0),
		"max_tokens":        float64(512),
		"repair_max_tokens": float64(128),
	})
	agg := findNode(parent, "agg")
	agg.Metadata["aggregation_method"] = string(workflow.AggMethodSynthesis)

	_, _, err := Compile(context.Background(), parent, StaticResolver{
		"custom-aggregation": testAggregationSourceWorkflow("custom-aggregation"),
	})
	if err == nil {
		t.Fatal("Compile succeeded with conflicting explicit and metadata aggregation methods")
	}
	if !strings.Contains(err.Error(), "conflicting aggregation methods") ||
		!strings.Contains(err.Error(), "judge") ||
		!strings.Contains(err.Error(), "synthesis") {
		t.Fatalf("Compile error = %v, want conflicting method metadata details", err)
	}
}

func TestCompileAggregationReferenceValidatesSourceAggregationConfig(t *testing.T) {
	tests := []struct {
		name       string
		method     workflow.AggregationMethod
		workflowID string
		config     map[string]interface{}
		want       string
	}{
		{
			name:       "dynamic scoring prompt must consume generated rubric",
			method:     workflow.AggMethodScoring,
			workflowID: "aggregation-scoring",
			config: map[string]interface{}{
				"scoring_model": "openai/gpt-5-mini",
				"system_prompt": "Score.",
				"prompt":        "Score {{candidate}} only.",
				"temperature":   float64(0),
				"max_tokens":    float64(512),
				"rubric_mode":   "dynamic",
			},
			want: "{{rubric}}",
		},
		{
			name:       "majority vote requires explicit tie breaker",
			method:     workflow.AggMethodMajorityVote,
			workflowID: "aggregation-majority-vote",
			config: map[string]interface{}{
				"extraction_strategy": "regex",
				"extraction_pattern":  `Answer:\s*([A-D])`,
			},
			want: "tie_breaker_method",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := testAggregationParent(tt.method, tt.workflowID, tt.config)
			_, _, err := Compile(context.Background(), parent, StaticResolver{
				tt.workflowID: testAggregationSourceWorkflow(tt.workflowID),
			})
			if err == nil {
				t.Fatal("Compile succeeded with invalid aggregation config")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Compile error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestCompileDynamicRubricDoesNotDependOnCandidateOutputs(t *testing.T) {
	parent := testAggregationParent(workflow.AggMethodScoring, "aggregation-scoring", map[string]interface{}{
		"scoring_model": "openai/gpt-5-mini",
		"system_prompt": "Score.",
		"prompt":        "Score {{candidate}}\n{{rubric}}",
		"temperature":   float64(0),
		"max_tokens":    float64(512),
		"rubric_mode":   "dynamic",
	})

	compiled, _, err := Compile(context.Background(), parent, StaticResolver{
		"aggregation-scoring": testAggregationSourceWorkflow("aggregation-scoring"),
	})
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	assertNodeIDs(t, compiled, "agg--generate-rubric", "agg--parse-rubric", "agg--format-rubric", "agg--score-agent-a")
	if hasEdge(compiled, "agent-a", "agg--generate-rubric") || hasEdge(compiled, "agent-b", "agg--generate-rubric") {
		t.Fatalf("generate-rubric should not depend on candidate outputs: %+v", compiled.Edges)
	}
	if !hasEdge(compiled, "agg--generate-rubric", "agg--parse-rubric") {
		t.Fatalf("missing raw dynamic rubric parse edge")
	}
	if !hasEdge(compiled, "agg--generate-rubric", "agg--format-rubric") {
		t.Fatalf("missing formatted dynamic rubric edge")
	}
	if !hasEdge(compiled, "agg--format-rubric", "agg--score-agent-a") {
		t.Fatalf("score prompt should depend on formatted rubric")
	}
	if !hasEdge(compiled, "agg--parse-rubric", "agg--parse-score-agent-a") {
		t.Fatalf("score parse should depend on raw rubric")
	}
}

func TestCompileAggregationCandidateRootsOnlyDependOnTheirCandidate(t *testing.T) {
	parent := testAggregationParent(workflow.AggMethodMajorityVote, "aggregation-majority-vote", map[string]interface{}{
		"extraction_strategy": "regex",
		"extraction_pattern":  `Answer:\s*([A-D])`,
		"tie_breaker_method":  "error",
	})

	compiled, _, err := Compile(context.Background(), parent, StaticResolver{
		"aggregation-majority-vote": testAggregationSourceWorkflow("aggregation-majority-vote"),
	})
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if !hasEdge(compiled, "agent-a", "agg--extract-answer-agent-a") {
		t.Fatalf("agent-a should feed its own extraction root: %+v", compiled.Edges)
	}
	if !hasEdge(compiled, "agent-b", "agg--extract-answer-agent-b") {
		t.Fatalf("agent-b should feed its own extraction root: %+v", compiled.Edges)
	}
	if hasEdge(compiled, "agent-a", "agg--extract-answer-agent-b") || hasEdge(compiled, "agent-b", "agg--extract-answer-agent-a") {
		t.Fatalf("candidate extraction roots should not cross-depend on unrelated candidates: %+v", compiled.Edges)
	}
}

func TestCompileAggregationAllInputRootsStillDependOnAllCandidates(t *testing.T) {
	parent := testAggregationParent(workflow.AggMethodSynthesis, "aggregation-synthesis", map[string]interface{}{
		"model":         "openai/gpt-5-mini",
		"system_prompt": "Synthesize.",
		"prompt":        "{{responses}}",
		"temperature":   float64(0),
		"max_tokens":    float64(512),
	})

	compiled, _, err := Compile(context.Background(), parent, StaticResolver{
		"aggregation-synthesis": testAggregationSourceWorkflow("aggregation-synthesis"),
	})
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	for _, candidateID := range []string{"agent-a", "agent-b"} {
		if !hasEdge(compiled, candidateID, "agg--format-candidates") {
			t.Fatalf("%s should feed all-input synthesis formatter: %+v", candidateID, compiled.Edges)
		}
	}
}

func TestCompileAggregationEdgeSetsMatchSupportedTopologies(t *testing.T) {
	tests := []struct {
		name       string
		method     workflow.AggregationMethod
		workflowID string
		config     map[string]interface{}
		wantEdges  []string
	}{
		{
			name:       "collect",
			method:     workflow.AggMethodCollect,
			workflowID: "aggregation-collect",
			config:     map[string]interface{}{"separator": "\n---\n"},
			wantEdges: []string{
				"agent-a->agg--collect-inputs",
				"agent-b->agg--collect-inputs",
				"agg--collect-inputs->agg--result",
			},
		},
		{
			name:       "majority_vote_synthesis_tie_breaker",
			method:     workflow.AggMethodMajorityVote,
			workflowID: "aggregation-majority-vote",
			config: map[string]interface{}{
				"extraction_strategy":     "regex",
				"extraction_pattern":      `Answer:\s*([A-D])`,
				"tie_breaker_method":      "synthesis",
				"tie_breaker_model":       "openai/gpt-5-mini",
				"tie_breaker_temperature": float64(0),
				"system_prompt":           "Synthesize tied answers.",
				"prompt":                  "{{responses}}",
				"max_tokens":              float64(512),
			},
			wantEdges: []string{
				"agent-a->agg--extract-answer-agent-a",
				"agent-b->agg--extract-answer-agent-b",
				"agg--extract-answer-agent-a->agg--count-votes",
				"agg--extract-answer-agent-b->agg--count-votes",
				"agg--count-votes->agg--format-candidates",
				"agg--format-candidates->agg--tie-policy",
				"agg--tie-policy->agg--select-winner",
				"agg--select-winner->agg--result",
			},
		},
		{
			name:       "synthesis",
			method:     workflow.AggMethodSynthesis,
			workflowID: "aggregation-synthesis",
			config: map[string]interface{}{
				"model":         "openai/gpt-5-mini",
				"system_prompt": "Synthesize.",
				"prompt":        "{{responses}}",
				"temperature":   float64(0),
				"max_tokens":    float64(512),
			},
			wantEdges: []string{
				"agent-a->agg--format-candidates",
				"agent-b->agg--format-candidates",
				"agg--format-candidates->agg--synthesize",
				"agg--synthesize->agg--result",
			},
		},
		{
			name:       "judge",
			method:     workflow.AggMethodJudge,
			workflowID: "aggregation-judge",
			config: map[string]interface{}{
				"judge_model":       "openai/gpt-5-mini",
				"system_prompt":     "Judge.",
				"prompt":            "{{responses}}",
				"temperature":       float64(0),
				"max_tokens":        float64(512),
				"repair_max_tokens": float64(128),
			},
			wantEdges: []string{
				"agent-a->agg--extract-answer-agent-a",
				"agent-a->agg--format-blind-candidates",
				"agent-b->agg--extract-answer-agent-b",
				"agent-b->agg--format-blind-candidates",
				"agg--extract-answer-agent-a->agg--check-unanimous",
				"agg--extract-answer-agent-b->agg--check-unanimous",
				"agg--check-unanimous->agg--unanimous-policy",
				"agg--format-blind-candidates->agg--judge",
				"agg--judge->agg--parse-selection",
				"agg--parse-selection->agg--repair-selection",
				"agg--repair-selection->agg--select-winner",
				"agg--unanimous-policy->agg--select-winner",
				"agg--select-winner->agg--result",
			},
		},
		{
			name:       "debate_decide",
			method:     workflow.AggMethodDebateDecide,
			workflowID: "aggregation-debate-decide",
			config: map[string]interface{}{
				"judge_model":         "openai/gpt-5-mini",
				"system_prompt":       "Judge camps.",
				"prompt":              "{{camps}}",
				"extraction_strategy": "regex",
				"extraction_pattern":  `Answer:\s*([A-D])`,
				"temperature":         float64(0),
				"max_tokens":          float64(512),
				"repair_max_tokens":   float64(128),
			},
			wantEdges: []string{
				"agent-a->agg--extract-answer-agent-a",
				"agent-b->agg--extract-answer-agent-b",
				"agg--extract-answer-agent-a->agg--check-unanimous",
				"agg--extract-answer-agent-a->agg--group-camps",
				"agg--extract-answer-agent-b->agg--check-unanimous",
				"agg--extract-answer-agent-b->agg--group-camps",
				"agg--check-unanimous->agg--single-camp-policy",
				"agg--group-camps->agg--format-candidates",
				"agg--format-candidates->agg--no-camps-policy",
				"agg--check-unanimous->agg--no-camps-policy",
				"agg--group-camps->agg--judge-camp",
				"agg--judge-camp->agg--parse-selection",
				"agg--parse-selection->agg--repair-selection",
				"agg--repair-selection->agg--select-winner",
				"agg--single-camp-policy->agg--select-winner",
				"agg--no-camps-policy->agg--select-winner",
				"agg--select-winner->agg--result",
			},
		},
		{
			name:       "scoring",
			method:     workflow.AggMethodScoring,
			workflowID: "aggregation-scoring",
			config: map[string]interface{}{
				"scoring_model": "openai/gpt-5-mini",
				"system_prompt": "Score.",
				"prompt":        "{{candidate}}",
				"temperature":   float64(0),
				"max_tokens":    float64(512),
				"rubric": []interface{}{
					map[string]interface{}{"name": "correctness", "weight": float64(1), "description": "Correctness"},
				},
			},
			wantEdges: []string{
				"agent-a->agg--extract-answer-agent-a",
				"agent-b->agg--extract-answer-agent-b",
				"agg--extract-answer-agent-a->agg--check-unanimous",
				"agg--extract-answer-agent-b->agg--check-unanimous",
				"agg--check-unanimous->agg--unanimous-policy",
				"agg--check-unanimous->agg--score-agent-a",
				"agg--score-agent-a->agg--parse-score-agent-a",
				"agg--check-unanimous->agg--score-agent-b",
				"agg--score-agent-b->agg--parse-score-agent-b",
				"agg--parse-score-agent-a->agg--reduce-scores",
				"agg--parse-score-agent-b->agg--reduce-scores",
				"agg--reduce-scores->agg--select-winner",
				"agg--unanimous-policy->agg--select-winner",
				"agg--select-winner->agg--result",
			},
		},
		{
			name:       "peer_matrix",
			method:     workflow.AggMethodPeerMatrix,
			workflowID: "aggregation-peer-matrix",
			config: map[string]interface{}{
				"eval_system_prompt": "Review peers.",
				"eval_prompt":        "{{candidate}}",
				"temperature":        float64(0),
				"max_tokens":         float64(512),
				"normalization":      "none",
				"max_parallel":       float64(6),
				"rubric": []interface{}{
					map[string]interface{}{"name": "correctness", "weight": float64(1), "description": "Correctness"},
				},
			},
			wantEdges: []string{
				"agent-a->agg--extract-answer-agent-a",
				"agent-b->agg--extract-answer-agent-b",
				"agg--extract-answer-agent-a->agg--check-unanimous",
				"agg--extract-answer-agent-b->agg--check-unanimous",
				"agg--check-unanimous->agg--unanimous-policy",
				"agg--check-unanimous->agg--review-agent-a-agent-b",
				"agg--review-agent-a-agent-b->agg--parse-review-agent-a-agent-b",
				"agg--check-unanimous->agg--review-agent-b-agent-a",
				"agg--review-agent-b-agent-a->agg--parse-review-agent-b-agent-a",
				"agg--parse-review-agent-a-agent-b->agg--reduce-peer-scores",
				"agg--parse-review-agent-b-agent-a->agg--reduce-peer-scores",
				"agg--reduce-peer-scores->agg--select-winner",
				"agg--unanimous-policy->agg--select-winner",
				"agg--select-winner->agg--result",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := testAggregationParent(tt.method, tt.workflowID, tt.config)
			compiled, _, err := Compile(context.Background(), parent, StaticResolver{
				tt.workflowID: testAggregationSourceWorkflow(tt.workflowID),
			})
			if err != nil {
				t.Fatalf("Compile failed: %v", err)
			}
			assertEdgeSet(t, compiled, tt.wantEdges)
		})
	}
}

func TestCompileDebateDecideEdgesMatchRuntimeDataDependencies(t *testing.T) {
	parent := testAggregationParent(workflow.AggMethodDebateDecide, "aggregation-debate-decide", map[string]interface{}{
		"judge_model":         "openai/gpt-5-mini",
		"system_prompt":       "Judge camps.",
		"prompt":              "{{camps}}",
		"extraction_strategy": "regex",
		"extraction_pattern":  `Answer:\s*([A-D])`,
		"temperature":         float64(0),
		"max_tokens":          float64(512),
		"repair_max_tokens":   float64(128),
	})

	compiled, _, err := Compile(context.Background(), parent, StaticResolver{
		"aggregation-debate-decide": testAggregationSourceWorkflow("aggregation-debate-decide"),
	})
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	for _, edge := range [][2]string{
		{"agg--check-unanimous", "agg--no-camps-policy"},
		{"agg--single-camp-policy", "agg--select-winner"},
		{"agg--group-camps", "agg--judge-camp"},
		{"agg--repair-selection", "agg--select-winner"},
	} {
		if !hasReachablePath(compiled, edge[0], edge[1]) {
			t.Fatalf("missing dependency path %s -> %s; edges=%+v", edge[0], edge[1], compiled.Edges)
		}
	}
	if hasEdge(compiled, "agg--single-camp-policy", "agg--judge-camp") {
		t.Fatalf("judge-camp should not be directly sequenced behind single-camp-policy: %+v", compiled.Edges)
	}
}

func TestCompiledAggregationTemplateReferencesHaveSchedulingPaths(t *testing.T) {
	tests := []struct {
		name       string
		method     workflow.AggregationMethod
		workflowID string
		config     map[string]interface{}
	}{
		{
			name:       "majority_vote",
			method:     workflow.AggMethodMajorityVote,
			workflowID: "aggregation-majority-vote",
			config: map[string]interface{}{
				"extraction_strategy":     "regex",
				"extraction_pattern":      `Answer:\s*([A-D])`,
				"tie_breaker_method":      "synthesis",
				"tie_breaker_model":       "openai/gpt-5-mini",
				"tie_breaker_temperature": float64(0),
				"system_prompt":           "Synthesize tied answers.",
				"prompt":                  "{{responses}}",
				"max_tokens":              float64(512),
			},
		},
		{
			name:       "judge",
			method:     workflow.AggMethodJudge,
			workflowID: "aggregation-judge",
			config: map[string]interface{}{
				"judge_model":       "openai/gpt-5-mini",
				"system_prompt":     "Judge.",
				"prompt":            "{{responses}}",
				"temperature":       float64(0),
				"max_tokens":        float64(512),
				"repair_max_tokens": float64(128),
			},
		},
		{
			name:       "debate_decide",
			method:     workflow.AggMethodDebateDecide,
			workflowID: "aggregation-debate-decide",
			config: map[string]interface{}{
				"judge_model":         "openai/gpt-5-mini",
				"system_prompt":       "Judge camps.",
				"prompt":              "{{camps}}",
				"extraction_strategy": "regex",
				"extraction_pattern":  `Answer:\s*([A-D])`,
				"temperature":         float64(0),
				"max_tokens":          float64(512),
				"repair_max_tokens":   float64(128),
			},
		},
		{
			name:       "scoring_dynamic_rubric",
			method:     workflow.AggMethodScoring,
			workflowID: "aggregation-scoring",
			config: map[string]interface{}{
				"scoring_model": "openai/gpt-5-mini",
				"system_prompt": "Score.",
				"prompt":        "{{candidate}}\n{{rubric}}",
				"temperature":   float64(0),
				"max_tokens":    float64(512),
				"rubric_mode":   "dynamic",
			},
		},
		{
			name:       "peer_matrix_dynamic_rubric",
			method:     workflow.AggMethodPeerMatrix,
			workflowID: "aggregation-peer-matrix",
			config: map[string]interface{}{
				"eval_system_prompt": "Review peers.",
				"eval_prompt":        "{{candidate}}\n{{rubric}}",
				"temperature":        float64(0),
				"max_tokens":         float64(512),
				"normalization":      "none",
				"max_parallel":       float64(6),
				"rubric_mode":        "dynamic",
				"rubric_model":       "openai/gpt-5-mini",
				"rubric": []interface{}{
					map[string]interface{}{"name": "correctness", "weight": float64(1), "description": "Correctness"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := testAggregationParent(tt.method, tt.workflowID, tt.config)
			compiled, _, err := Compile(context.Background(), parent, StaticResolver{
				tt.workflowID: testAggregationSourceWorkflow(tt.workflowID),
			})
			if err != nil {
				t.Fatalf("Compile failed: %v", err)
			}
			assertTemplateReferencesHaveSchedulingPaths(t, compiled)
		})
	}
}

func TestCompileScoringAndPeerMatrixResponsesTemplateAddsFormattedCandidates(t *testing.T) {
	tests := []struct {
		name       string
		method     workflow.AggregationMethod
		workflowID string
		config     map[string]interface{}
		wantRefs   []string
	}{
		{
			name:       "scoring",
			method:     workflow.AggMethodScoring,
			workflowID: "aggregation-scoring",
			config: map[string]interface{}{
				"scoring_model": "openai/gpt-5-mini",
				"system_prompt": "Score.",
				"prompt":        "All responses:\n{{responses}}\nCandidate:\n{{candidate}}\n{{rubric}}",
				"temperature":   float64(0),
				"max_tokens":    float64(512),
				"rubric": []interface{}{
					map[string]interface{}{"name": "correctness", "weight": float64(1), "description": "Correctness"},
				},
			},
			wantRefs: []string{"agg--score-agent-a", "agg--score-agent-b"},
		},
		{
			name:       "peer_matrix",
			method:     workflow.AggMethodPeerMatrix,
			workflowID: "aggregation-peer-matrix",
			config: map[string]interface{}{
				"eval_system_prompt": "Review peers.",
				"eval_prompt":        "All responses:\n{{responses}}\nCandidate:\n{{candidate}}\nReviewer:\n{{reviewer_answer}}\n{{rubric}}",
				"temperature":        float64(0),
				"max_tokens":         float64(512),
				"normalization":      "none",
				"max_parallel":       float64(6),
				"rubric": []interface{}{
					map[string]interface{}{"name": "correctness", "weight": float64(1), "description": "Correctness"},
				},
			},
			wantRefs: []string{"agg--review-agent-a-agent-b", "agg--review-agent-b-agent-a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := testAggregationParent(tt.method, tt.workflowID, tt.config)
			compiled, _, err := Compile(context.Background(), parent, StaticResolver{
				tt.workflowID: testAggregationSourceWorkflow(tt.workflowID),
			})
			if err != nil {
				t.Fatalf("Compile failed: %v", err)
			}
			if findNode(compiled, "agg--format-candidates") == nil {
				t.Fatalf("format-candidates missing for {{responses}} prompt: %s", nodeIDs(compiled))
			}
			for _, targetID := range tt.wantRefs {
				target := findNode(compiled, targetID)
				if target == nil {
					t.Fatalf("target %q missing: %s", targetID, nodeIDs(compiled))
				}
				if !strings.Contains(target.Prompt, "{{agg--format-candidates}}") {
					t.Fatalf("%s prompt = %q, want formatted candidate reference", targetID, target.Prompt)
				}
				if !hasReachablePath(compiled, "agg--format-candidates", targetID) {
					t.Fatalf("missing scheduling path agg--format-candidates -> %s; edges=%+v", targetID, compiled.Edges)
				}
			}
			assertTemplateReferencesHaveSchedulingPaths(t, compiled)
		})
	}
}

func TestCompileAggregationUsesRawAnswerExtractionWithoutParseFanout(t *testing.T) {
	parent := testAggregationParent(workflow.AggMethodMajorityVote, "aggregation-majority-vote", map[string]interface{}{
		"extraction_strategy": "regex",
		"extraction_pattern":  `Answer:\s*([A-D])`,
		"tie_breaker_method":  "error",
	})

	compiled, _, err := Compile(context.Background(), parent, StaticResolver{
		"aggregation-majority-vote": testAggregationSourceWorkflow("aggregation-majority-vote"),
	})
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	if findNode(compiled, "agg--parse-answer-agent-a") != nil || findNode(compiled, "agg--parse-answer-agent-b") != nil {
		t.Fatalf("deterministic parse-answer fanout should be hidden from compiled DAG: %s", nodeIDs(compiled))
	}
	count := findNode(compiled, "agg--count-votes")
	if count == nil {
		t.Fatalf("count-votes missing: %s", nodeIDs(compiled))
	}
	if got := fmt.Sprintf("%v", count.OperationConfig["answers"]); !strings.Contains(got, "{{agg--extract-answer-agent-a}}") || strings.Contains(got, "parse-answer") {
		t.Fatalf("count-votes answers = %#v, want raw extract-answer refs", count.OperationConfig["answers"])
	}
}

func TestCompileNestedAggregationReferenceRewritesGroupMetadata(t *testing.T) {
	l1 := testAggregationParent(workflow.AggMethodJudge, "aggregation-judge", map[string]interface{}{
		"judge_model":       "openai/gpt-5-mini",
		"system_prompt":     "Judge.",
		"prompt":            "{{responses}}",
		"temperature":       float64(0),
		"max_tokens":        float64(512),
		"repair_max_tokens": float64(128),
	})
	l1.ID = "l1-judge"
	parent := &workflow.Workflow{
		ID: "parent",
		Nodes: []*workflow.Node{{
			ID:            "outer",
			Type:          workflow.NodeTypeWorkflowRef,
			WorkflowRefID: "l1-judge",
		}},
	}

	compiled, _, err := Compile(context.Background(), parent, StaticResolver{
		"l1-judge":          *l1,
		"aggregation-judge": testAggregationSourceWorkflow("aggregation-judge"),
	})
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}

	groupNodeID := "outer--agg--result"
	assertNodeIDs(t, compiled, "outer--agg--judge", "outer--agg--repair-selection", groupNodeID)
	for _, id := range []string{"outer--agg--judge", "outer--agg--repair-selection"} {
		node := findNode(compiled, id)
		if got := node.Metadata["aggregation_group_node_id"]; got != groupNodeID {
			t.Fatalf("node %q aggregation_group_node_id = %#v, want %q", id, got, groupNodeID)
		}
		if got := node.Metadata["aggregation_anchor_id"]; got != "outer--agg" {
			t.Fatalf("node %q aggregation_anchor_id = %#v, want outer--agg", id, got)
		}
		if got := node.Metadata["source_parent_node_id"]; got != "outer--agg" {
			t.Fatalf("node %q source_parent_node_id = %#v, want outer--agg", id, got)
		}
	}
	format := findNode(compiled, "outer--agg--format-blind-candidates")
	if format == nil {
		t.Fatalf("nested format operation missing, nodes=%s", nodeIDs(compiled))
	}
	if got := format.OperationConfig["input_ids"]; !containsInputID(got, "outer--agent-a") || !containsInputID(got, "outer--agent-b") {
		t.Fatalf("nested format input_ids not rewritten: %+v", got)
	}
	repair := findNode(compiled, "outer--agg--repair-selection")
	if repair.TrueBranch == nil {
		t.Fatalf("repair true branch missing")
	}
	if got := repair.TrueBranch.Metadata["aggregation_group_node_id"]; got != groupNodeID {
		t.Fatalf("repair branch aggregation_group_node_id = %#v, want %q", got, groupNodeID)
	}
	if got := repair.TrueBranch.Metadata["aggregation_anchor_id"]; got != "outer--agg" {
		t.Fatalf("repair branch aggregation_anchor_id = %#v, want outer--agg", got)
	}
}

func TestCompileLegacyResultAggregationWithoutReferenceIsPreserved(t *testing.T) {
	parent := &workflow.Workflow{
		ID: "legacy",
		Nodes: []*workflow.Node{
			testPromptNode("agent-a"),
			testPromptNode("agent-b"),
			{
				ID:                "legacy-result",
				Type:              workflow.NodeTypeResult,
				OutputName:        "answer",
				AggregationMethod: workflow.AggMethodJudge,
				AggregationConfig: map[string]interface{}{
					"judge_model":       "openai/gpt-5-mini",
					"system_prompt":     "Judge.",
					"prompt":            "{{responses}}",
					"temperature":       float64(0),
					"max_tokens":        float64(512),
					"repair_max_tokens": float64(128),
				},
				RetryPolicy: workflow.DefaultRetryPolicy(),
				Metadata: map[string]interface{}{
					"input_ids": []interface{}{"agent-a", "agent-b"},
				},
			},
		},
		Edges: []*workflow.Edge{
			{ID: "a-result", Source: "agent-a", Target: "legacy-result"},
			{ID: "b-result", Source: "agent-b", Target: "legacy-result"},
		},
	}
	compiled, _, err := Compile(context.Background(), parent, StaticResolver{})
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	result := findNode(compiled, "legacy-result")
	if result == nil {
		t.Fatalf("legacy result missing, nodes=%s", nodeIDs(compiled))
	}
	if result.Type != workflow.NodeTypeResult || result.AggregationMethod != workflow.AggMethodJudge {
		t.Fatalf("legacy aggregation was not preserved: %+v", result)
	}
}

func testAggregationParent(method workflow.AggregationMethod, workflowID string, config map[string]interface{}) *workflow.Workflow {
	return &workflow.Workflow{
		ID: "parent",
		Context: map[string]interface{}{
			"user_prompt": "Pick the best answer.",
		},
		Nodes: []*workflow.Node{
			testPromptNode("agent-a"),
			testPromptNode("agent-b"),
			{
				ID:                "agg",
				Type:              workflow.NodeTypeWorkflowRef,
				WorkflowRefID:     workflowID,
				AggregationMethod: method,
				AggregationConfig: config,
				Metadata: map[string]interface{}{
					"input_ids":             []interface{}{"agent-a", "agent-b"},
					"output_name":           "final_answer",
					"output_format":         "text",
					"aggregation_anchor_id": "agg",
					"aggregation_method":    string(method),
				},
			},
		},
		Edges: []*workflow.Edge{
			{ID: "a-agg", Source: "agent-a", Target: "agg"},
			{ID: "b-agg", Source: "agent-b", Target: "agg"},
		},
	}
}

func testAggregationSourceWorkflow(id string) workflow.Workflow {
	return workflow.Workflow{
		ID:    id,
		Name:  id,
		Nodes: []*workflow.Node{testPromptNode("template")},
	}
}

func assertNodeIDs(t *testing.T, wf *workflow.Workflow, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if findNode(wf, id) == nil {
			t.Fatalf("missing node %q; got %s", id, nodeIDs(wf))
		}
	}
}

func hasEdge(wf *workflow.Workflow, source, target string) bool {
	for _, edge := range wf.Edges {
		if edge.Source == source && edge.Target == target {
			return true
		}
	}
	return false
}

func assertEdgeSet(t *testing.T, wf *workflow.Workflow, want []string) {
	t.Helper()
	got := make([]string, 0, len(wf.Edges))
	for _, edge := range wf.Edges {
		got = append(got, edge.Source+"->"+edge.Target)
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("compiled edges mismatch\nwant:\n%s\n\ngot:\n%s", strings.Join(want, "\n"), strings.Join(got, "\n"))
	}
}

func hasReachablePath(wf *workflow.Workflow, source, target string) bool {
	if source == target {
		return true
	}
	adjacent := make(map[string][]string, len(wf.Edges))
	for _, edge := range wf.Edges {
		adjacent[edge.Source] = append(adjacent[edge.Source], edge.Target)
	}
	seen := map[string]struct{}{source: {}}
	queue := []string{source}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range adjacent[current] {
			if next == target {
				return true
			}
			if _, ok := seen[next]; ok {
				continue
			}
			seen[next] = struct{}{}
			queue = append(queue, next)
		}
	}
	return false
}

func assertTemplateReferencesHaveSchedulingPaths(t *testing.T, wf *workflow.Workflow) {
	t.Helper()
	nodeIDs := make(map[string]struct{}, len(wf.Nodes))
	for _, node := range wf.Nodes {
		nodeIDs[node.ID] = struct{}{}
	}
	for _, node := range wf.Nodes {
		assertNodeTemplateReferencesHaveSchedulingPaths(t, wf, nodeIDs, node.ID, node)
	}
}

func assertNodeTemplateReferencesHaveSchedulingPaths(t *testing.T, wf *workflow.Workflow, nodeIDs map[string]struct{}, targetID string, node *workflow.Node) {
	t.Helper()
	if node == nil {
		return
	}
	for _, ref := range collectNodeTemplateReferences(t, node) {
		sourceID := ref
		if base, _, ok := strings.Cut(ref, "__"); ok {
			sourceID = base
		}
		if _, ok := nodeIDs[sourceID]; !ok || sourceID == targetID {
			continue
		}
		if !hasReachablePath(wf, sourceID, targetID) {
			t.Fatalf("node %q references %q but has no scheduling path from %q; edges=%+v", targetID, ref, sourceID, wf.Edges)
		}
	}
	assertNodeTemplateReferencesHaveSchedulingPaths(t, wf, nodeIDs, targetID, node.TrueBranch)
	assertNodeTemplateReferencesHaveSchedulingPaths(t, wf, nodeIDs, targetID, node.FalseBranch)
}

func collectNodeTemplateReferences(t *testing.T, node *workflow.Node) []string {
	t.Helper()
	payload := map[string]interface{}{
		"prompt":               node.Prompt,
		"system_prompt":        node.SystemPrompt,
		"condition":            node.Condition,
		"input_template":       node.InputTemplate,
		"child_input_template": node.ChildInputTemplate,
		"aggregation_config":   node.AggregationConfig,
		"operation_config":     node.OperationConfig,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal node %q template payload: %v", node.ID, err)
	}
	matches := templateReferencePattern.FindAllStringSubmatch(string(data), -1)
	refs := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) > 1 {
			refs = append(refs, strings.TrimSpace(match[1]))
		}
	}
	return refs
}

func assertNoLegacyAggregationResult(t *testing.T, wf *workflow.Workflow, method workflow.AggregationMethod) {
	t.Helper()
	for _, node := range wf.Nodes {
		if node.Type != workflow.NodeTypeResult {
			continue
		}
		if node.AggregationMethod != "" && node.AggregationMethod != workflow.AggMethodCollect {
			t.Fatalf("legacy aggregation method %q survived on result node %q for %q", node.AggregationMethod, node.ID, method)
		}
	}
}

func assertExpandedSourceMetadata(t *testing.T, wf *workflow.Workflow, refID, workflowID string) {
	t.Helper()
	prefix := refID + generatedIDDelimiter
	for _, node := range wf.Nodes {
		if !strings.HasPrefix(node.ID, prefix) {
			continue
		}
		if got := node.Metadata["source_workflow_id"]; got != workflowID {
			t.Fatalf("node %q source_workflow_id = %#v, want %q", node.ID, got, workflowID)
		}
		if got := node.Metadata["source_workflow_hash"]; got == "" {
			t.Fatalf("node %q missing source_workflow_hash metadata", node.ID)
		}
		if got := node.Metadata["source_node_id"]; got == "" {
			t.Fatalf("node %q missing source_node_id metadata", node.ID)
		}
		if strings.Contains(node.ID, "__") {
			t.Fatalf("compiled node %q contains reserved delimiter", node.ID)
		}
	}
}

func assertAggregationGroupMetadata(t *testing.T, wf *workflow.Workflow, refID, groupNodeID string) {
	t.Helper()
	prefix := refID + generatedIDDelimiter
	for _, node := range wf.Nodes {
		walkNodeAndBranches(node, func(node *workflow.Node) {
			if !strings.HasPrefix(node.ID, prefix) {
				return
			}
			if node.ID == groupNodeID {
				if got := node.Metadata["aggregation_group_node_id"]; got != nil {
					t.Fatalf("group node %q should not point to itself, aggregation_group_node_id = %#v", node.ID, got)
				}
				return
			}
			if got := node.Metadata["aggregation_group_node_id"]; got != groupNodeID {
				t.Fatalf("node %q aggregation_group_node_id = %#v, want %q", node.ID, got, groupNodeID)
			}
		})
	}
}

func walkNodeAndBranches(node *workflow.Node, visit func(*workflow.Node)) {
	if node == nil {
		return
	}
	visit(node)
	walkNodeAndBranches(node.TrueBranch, visit)
	walkNodeAndBranches(node.FalseBranch, visit)
}

func assertCompiledValidationPasses(t *testing.T, wf *workflow.Workflow) {
	t.Helper()
	result := workflow.NewValidator(nil).Validate(wf)
	if !result.Valid {
		t.Fatalf("compiled workflow validation failed: %+v", result.Errors)
	}
}

func assertReportIncludesAggregationRef(t *testing.T, report *Report, nodeID, workflowID string) {
	t.Helper()
	for _, expanded := range report.ExpandedRefs {
		if expanded.NodeID == nodeID && expanded.WorkflowID == workflowID && len(expanded.ExpandedNodeIDs) > 0 {
			return
		}
	}
	t.Fatalf("expanded aggregation ref not reported: %+v", report.ExpandedRefs)
}

func findNode(wf *workflow.Workflow, id string) *workflow.Node {
	for _, node := range wf.Nodes {
		if node.ID == id {
			return node
		}
	}
	return nil
}

func nodeIDs(wf *workflow.Workflow) string {
	ids := make([]string, 0, len(wf.Nodes))
	for _, node := range wf.Nodes {
		ids = append(ids, node.ID)
	}
	return fmt.Sprintf("%v", ids)
}
