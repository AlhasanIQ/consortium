package compiler

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/workflow"
)

func TestCompileAggregationExecutionSelectsExpectedOutput(t *testing.T) {
	tests := []struct {
		name       string
		method     workflow.AggregationMethod
		workflowID string
		config     map[string]interface{}
		candidates []executionCandidate
		want       string
	}{
		{
			name:       "collect",
			method:     workflow.AggMethodCollect,
			workflowID: "aggregation-collect",
			config:     map[string]interface{}{"separator": "\n---\n"},
			candidates: []executionCandidate{{"agent-a", "Answer: A"}, {"agent-b", "Answer: B"}},
			want:       "Answer: A\n---\nAnswer: B",
		},
		{
			name:       "majority_vote",
			method:     workflow.AggMethodMajorityVote,
			workflowID: "aggregation-majority-vote",
			config: map[string]interface{}{
				"extraction_strategy": "regex",
				"extraction_pattern":  `Answer:\s*([A-D])`,
				"tie_breaker_method":  "error",
			},
			candidates: []executionCandidate{{"agent-a", "Answer: A"}, {"agent-b", "Answer: B"}, {"agent-c", "Answer: B"}},
			want:       "Answer: B",
		},
		{
			name:       "synthesis",
			method:     workflow.AggMethodSynthesis,
			workflowID: "aggregation-synthesis",
			config: map[string]interface{}{
				"model":         "mock-model",
				"system_prompt": "Synthesize.",
				"prompt":        "synthesis-response {{responses}}",
				"temperature":   float64(0),
				"max_tokens":    float64(512),
			},
			candidates: []executionCandidate{{"agent-a", "Answer: A"}, {"agent-b", "Answer: B"}},
			want:       "Synthesized answer",
		},
		{
			name:       "judge",
			method:     workflow.AggMethodJudge,
			workflowID: "aggregation-judge",
			config: map[string]interface{}{
				"judge_model":       "mock-model",
				"system_prompt":     "Judge.",
				"prompt":            "judge-response {{responses}}",
				"temperature":       float64(0),
				"max_tokens":        float64(512),
				"repair_max_tokens": float64(128),
			},
			candidates: []executionCandidate{{"agent-a", "Answer: A"}, {"agent-b", "Answer: B"}},
			want:       "Answer: B",
		},
		{
			name:       "debate_decide",
			method:     workflow.AggMethodDebateDecide,
			workflowID: "aggregation-debate-decide",
			config: map[string]interface{}{
				"judge_model":         "mock-model",
				"system_prompt":       "Judge camps.",
				"prompt":              "debate-response {{camps}}",
				"extraction_strategy": "regex",
				"extraction_pattern":  `Answer:\s*([A-D])`,
				"temperature":         float64(0),
				"max_tokens":          float64(512),
				"repair_max_tokens":   float64(128),
			},
			candidates: []executionCandidate{{"agent-a", "Answer: A"}, {"agent-b", "Answer: B"}},
			want:       "Answer: B",
		},
		{
			name:       "scoring",
			method:     workflow.AggMethodScoring,
			workflowID: "aggregation-scoring",
			config: map[string]interface{}{
				"scoring_model": "mock-model",
				"system_prompt": "Score.",
				"prompt":        "score-response {{candidate}}",
				"temperature":   float64(0),
				"max_tokens":    float64(512),
			},
			candidates: []executionCandidate{{"agent-a", "Answer: A"}, {"agent-b", "Answer: B"}},
			want:       "Answer: B",
		},
		{
			name:       "peer_matrix",
			method:     workflow.AggMethodPeerMatrix,
			workflowID: "aggregation-peer-matrix",
			config: map[string]interface{}{
				"eval_system_prompt": "Review.",
				"eval_prompt":        "peer-response {{candidate}}",
				"rubric": []interface{}{
					map[string]interface{}{"name": "quality", "weight": float64(1), "description": "Prefer the best answer."},
				},
				"temperature":   float64(0),
				"max_tokens":    float64(512),
				"normalization": "none",
				"max_parallel":  float64(6),
			},
			candidates: []executionCandidate{{"agent-a", "Answer: A"}, {"agent-b", "Answer: B"}},
			want:       "Answer: B",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := aggregationExecutionParent(tt.method, tt.workflowID, tt.config, tt.candidates)
			compiled, _, err := Compile(context.Background(), parent, StaticResolver{
				tt.workflowID: testAggregationSourceWorkflow(tt.workflowID),
			})
			if err != nil {
				t.Fatalf("Compile failed: %v", err)
			}
			result := workflow.NewValidator(nil).Validate(compiled)
			if !result.Valid {
				t.Fatalf("compiled validation failed: %+v", result.Errors)
			}
			execResult, err := workflow.NewExecutor(testAggregationLLMClient()).Execute(context.Background(), compiled, nil)
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}
			if !execResult.Success {
				t.Fatalf("Execute result failed: %+v", execResult)
			}
			got, _ := execResult.Context["agg--result"].(string)
			if got != tt.want {
				t.Fatalf("agg--result = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCompileAggregationMajorityVoteTieModes(t *testing.T) {
	tests := []struct {
		name        string
		config      map[string]interface{}
		want        string
		wantErrPart string
	}{
		{
			name: "first",
			config: map[string]interface{}{
				"extraction_strategy": "regex",
				"extraction_pattern":  `Answer:\s*([A-D])`,
				"tie_breaker_method":  "first",
			},
			want: "Answer: A",
		},
		{
			name: "error",
			config: map[string]interface{}{
				"extraction_strategy": "regex",
				"extraction_pattern":  `Answer:\s*([A-D])`,
				"tie_breaker_method":  "error",
			},
			wantErrPart: "tie",
		},
		{
			name: "synthesis",
			config: map[string]interface{}{
				"extraction_strategy":     "regex",
				"extraction_pattern":      `Answer:\s*([A-D])`,
				"tie_breaker_method":      "synthesis",
				"tie_breaker_model":       "mock-model",
				"tie_breaker_temperature": float64(0),
				"system_prompt":           "Break ties.",
				"prompt":                  "synthesis-response {{responses}}",
				"max_tokens":              float64(128),
			},
			want: "Synthesized answer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := aggregationExecutionParent(workflow.AggMethodMajorityVote, "aggregation-majority-vote", tt.config, []executionCandidate{
				{"agent-a", "Answer: A"},
				{"agent-b", "Answer: B"},
			})
			got, err := executeCompiledAggregationForTest(parent, "aggregation-majority-vote", testAggregationLLMClient())
			if tt.wantErrPart != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErrPart) {
					t.Fatalf("Execute error = %v, want containing %q", err, tt.wantErrPart)
				}
				return
			}
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}
			if got != tt.want {
				t.Fatalf("agg--result = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCompileAggregationMajorityVoteSynthesizesWhenNoAnswerExtracts(t *testing.T) {
	parent := aggregationExecutionParent(workflow.AggMethodMajorityVote, "aggregation-majority-vote", map[string]interface{}{
		"extraction_strategy":     "regex",
		"extraction_pattern":      `Final:\s*([A-D])`,
		"tie_breaker_method":      "synthesis",
		"tie_breaker_model":       "mock-model",
		"tie_breaker_temperature": float64(0),
		"system_prompt":           "Synthesize missing discrete answers.",
		"prompt":                  "synthesis-response {{responses}}",
		"max_tokens":              float64(128),
	}, []executionCandidate{
		{"agent-a", "The answer is twenty seven."},
		{"agent-b", "I would compute the expression directly."},
	})

	got, err := executeCompiledAggregationForTest(parent, "aggregation-majority-vote", testAggregationLLMClient())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got != "Synthesized answer" {
		t.Fatalf("agg--result = %q, want synthesis fallback", got)
	}
}

func TestCompileAggregationDebateDecideSynthesizesWhenNoCampExtracts(t *testing.T) {
	parent := aggregationExecutionParent(workflow.AggMethodDebateDecide, "aggregation-debate-decide", map[string]interface{}{
		"judge_model":         "mock-model",
		"system_prompt":       "Judge camps.",
		"prompt":              "debate-response {{camps}}",
		"extraction_strategy": "regex",
		"extraction_pattern":  `Final:\s*([A-D])`,
		"temperature":         float64(0),
		"max_tokens":          float64(128),
		"repair_max_tokens":   float64(32),
	}, []executionCandidate{
		{"agent-a", "The answer is twenty seven."},
		{"agent-b", "I would compute the expression directly."},
	})

	got, err := executeCompiledAggregationForTest(parent, "aggregation-debate-decide", testAggregationLLMClientWithResponses(map[string]string{
		"Please provide a synthesized response": "Synthesized no-camps answer",
	}))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got != "Synthesized no-camps answer" {
		t.Fatalf("agg--result = %q, want no-camps synthesis fallback", got)
	}
}

func TestCompileAggregationScoringParsesWeightedRubricScores(t *testing.T) {
	parent := aggregationExecutionParent(workflow.AggMethodScoring, "aggregation-scoring", map[string]interface{}{
		"scoring_model": "mock-model",
		"system_prompt": "Score.",
		"prompt":        "weighted-score-response {{response}}\n{{rubric}}",
		"temperature":   float64(0),
		"max_tokens":    float64(512),
		"rubric": []interface{}{
			map[string]interface{}{"name": "correctness", "weight": float64(0.75), "description": "Correctness"},
			map[string]interface{}{"name": "clarity", "weight": float64(0.25), "description": "Clarity"},
		},
	}, []executionCandidate{
		{"agent-a", "Answer: A"},
		{"agent-b", "Answer: B"},
	})
	got, err := executeCompiledAggregationForTest(parent, "aggregation-scoring", testAggregationLLMClient())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got != "Answer: B" {
		t.Fatalf("agg--result = %q, want weighted scoring winner", got)
	}
}

func TestCompileAggregationPeerMatrixParsesCriterionScoreObjects(t *testing.T) {
	parent := aggregationExecutionParent(workflow.AggMethodPeerMatrix, "aggregation-peer-matrix", map[string]interface{}{
		"eval_system_prompt": "Review.",
		"eval_prompt":        "weighted-peer-response {{candidate}}\n{{rubric}}",
		"temperature":        float64(0),
		"max_tokens":         float64(512),
		"normalization":      "none",
		"max_parallel":       float64(6),
		"rubric": []interface{}{
			map[string]interface{}{"name": "correctness", "weight": float64(0.75), "description": "Correctness"},
			map[string]interface{}{"name": "clarity", "weight": float64(0.25), "description": "Clarity"},
		},
	}, []executionCandidate{
		{"agent-a", "Answer: A"},
		{"agent-b", "Answer: B"},
	})
	got, err := executeCompiledAggregationForTest(parent, "aggregation-peer-matrix", testAggregationLLMClient())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got != "Answer: B" {
		t.Fatalf("agg--result = %q, want weighted peer winner", got)
	}
}

func TestCompileAggregationScoringDynamicRubricGeneratesRubricJob(t *testing.T) {
	parent := aggregationExecutionParent(workflow.AggMethodScoring, "aggregation-scoring", map[string]interface{}{
		"rubric_mode":   "dynamic",
		"scoring_model": "mock-model",
		"system_prompt": "Score.",
		"prompt":        "weighted-score-response {{response}}\nRubric:\n{{rubric}}",
		"temperature":   float64(0),
		"max_tokens":    float64(512),
		"rubric": []interface{}{
			map[string]interface{}{"name": "fallback", "weight": float64(1), "description": "Fallback rubric"},
		},
	}, []executionCandidate{
		{"agent-a", "Answer: A"},
		{"agent-b", "Answer: B"},
	})

	compiled, _, err := Compile(context.Background(), parent, StaticResolver{
		"aggregation-scoring": testAggregationSourceWorkflow("aggregation-scoring"),
	})
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	assertNodeIDs(t, compiled, "agg--generate-rubric", "agg--parse-rubric", "agg--format-rubric", "agg--score-agent-a", "agg--score-agent-b")
	rubric := findNode(compiled, "agg--generate-rubric")
	if rubric.Type != workflow.NodeTypePrompt {
		t.Fatalf("agg--generate-rubric type = %s, want prompt", rubric.Type)
	}
	score := findNode(compiled, "agg--score-agent-a")
	if !strings.Contains(score.Prompt, "{{agg--format-rubric}}") {
		t.Fatalf("score prompt did not reference parsed dynamic rubric: %q", score.Prompt)
	}
	parse := findNode(compiled, "agg--parse-score-agent-a")
	if got := parse.OperationConfig["rubric"]; got != "{{agg--parse-rubric}}" {
		t.Fatalf("parse-score rubric = %#v, want dynamic rubric template", got)
	}

	got, err := executeCompiledAggregationForTest(parent, "aggregation-scoring", testAggregationLLMClientWithResponses(map[string]string{
		"Generate scoring criteria": `{"rubric":[{"name":"correctness","weight":0.75,"description":"Correctness"},{"name":"clarity","weight":0.25,"description":"Clarity"}]}`,
	}))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got != "Answer: B" {
		t.Fatalf("agg--result = %q, want dynamic rubric scoring winner", got)
	}
}

func TestCompileAggregationScoringDynamicRubricUsesStaticFallbackWhenGeneratedRubricInvalid(t *testing.T) {
	parent := aggregationExecutionParent(workflow.AggMethodScoring, "aggregation-scoring", map[string]interface{}{
		"rubric_mode":   "dynamic",
		"scoring_model": "mock-model",
		"system_prompt": "Score.",
		"prompt":        "fallback-weighted-score {{response}}\nRubric:\n{{rubric}}",
		"temperature":   float64(0),
		"max_tokens":    float64(512),
		"rubric": []interface{}{
			map[string]interface{}{"name": "fallback", "weight": float64(1), "description": "Fallback rubric"},
		},
	}, []executionCandidate{
		{"agent-a", "Answer: A"},
		{"agent-b", "Answer: B"},
	})

	got, err := executeCompiledAggregationForTest(parent, "aggregation-scoring", testAggregationLLMClientWithResponses(map[string]string{
		"Generate scoring criteria":         `{"rubric":"not a criteria list"}`,
		"fallback-weighted-score Answer: B": `{"scores":{"fallback":9}}`,
		"fallback-weighted-score Answer: A": `{"scores":{"fallback":2}}`,
		"weighted-score-response Answer: B": `{"scores":{"correctness":1,"clarity":1}}`,
		"weighted-score-response Answer: A": `{"scores":{"correctness":9,"clarity":9}}`,
	}))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got != "Answer: B" {
		t.Fatalf("agg--result = %q, want configured fallback rubric scoring winner", got)
	}
}

func TestCompileAggregationPeerMatrixDynamicRubricGeneratesSingleRubricJob(t *testing.T) {
	parent := aggregationExecutionParent(workflow.AggMethodPeerMatrix, "aggregation-peer-matrix", map[string]interface{}{
		"rubric_mode":        "dynamic",
		"rubric_model":       "mock-rubric-model",
		"eval_system_prompt": "Review.",
		"eval_prompt":        "weighted-peer-response {{candidate}}\nRubric:\n{{rubric}}",
		"temperature":        float64(0),
		"max_tokens":         float64(512),
		"normalization":      "none",
		"max_parallel":       float64(6),
		"rubric": []interface{}{
			map[string]interface{}{"name": "fallback", "weight": float64(1), "description": "Fallback rubric"},
		},
	}, []executionCandidate{
		{"agent-a", "Answer: A"},
		{"agent-b", "Answer: B"},
		{"agent-c", "Answer: C"},
	})

	compiled, _, err := Compile(context.Background(), parent, StaticResolver{
		"aggregation-peer-matrix": testAggregationSourceWorkflow("aggregation-peer-matrix"),
	})
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	assertNodeIDs(t, compiled, "agg--generate-rubric", "agg--parse-rubric", "agg--format-rubric")
	if got := findNode(compiled, "agg--generate-rubric").Model; got != "mock-rubric-model" {
		t.Fatalf("generate-rubric model = %q, want rubric_model", got)
	}
	if got := countCompiledAggregationPrompts(compiled, "agg"); got != 7 {
		t.Fatalf("compiled prompt count = %d, want 1 rubric + 6 peer reviews", got)
	}
	review := findNode(compiled, "agg--review-agent-a-agent-b")
	if !strings.Contains(review.Prompt, "{{agg--format-rubric}}") {
		t.Fatalf("peer review prompt did not reference parsed dynamic rubric: %q", review.Prompt)
	}
	parse := findNode(compiled, "agg--parse-review-agent-a-agent-b")
	if got := parse.OperationConfig["rubric"]; got != "{{agg--parse-rubric}}" {
		t.Fatalf("parse-review rubric = %#v, want dynamic rubric template", got)
	}

	got, err := executeCompiledAggregationForTest(parent, "aggregation-peer-matrix", testAggregationLLMClientWithResponses(map[string]string{
		"Generate scoring criteria": `{"rubric":[{"name":"correctness","weight":0.75,"description":"Correctness"},{"name":"clarity","weight":0.25,"description":"Clarity"}]}`,
	}))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got != "Answer: B" {
		t.Fatalf("agg--result = %q, want dynamic rubric peer winner", got)
	}
}

func TestCompileAggregationPeerMatrixDynamicRubricUsesStaticFallbackWhenGeneratedRubricInvalid(t *testing.T) {
	parent := aggregationExecutionParent(workflow.AggMethodPeerMatrix, "aggregation-peer-matrix", map[string]interface{}{
		"rubric_mode":        "dynamic",
		"rubric_model":       "mock-rubric-model",
		"eval_system_prompt": "Review.",
		"eval_prompt":        "fallback-weighted-peer {{candidate}}\nRubric:\n{{rubric}}",
		"temperature":        float64(0),
		"max_tokens":         float64(512),
		"normalization":      "none",
		"max_parallel":       float64(6),
		"rubric": []interface{}{
			map[string]interface{}{"name": "fallback", "weight": float64(1), "description": "Fallback rubric"},
		},
	}, []executionCandidate{
		{"agent-a", "Answer: A"},
		{"agent-b", "Answer: B"},
		{"agent-c", "Answer: C"},
	})

	got, err := executeCompiledAggregationForTest(parent, "aggregation-peer-matrix", testAggregationLLMClientWithResponses(map[string]string{
		"Generate scoring criteria":        `{"rubric":"not a criteria list"}`,
		"fallback-weighted-peer Answer: B": `{"fallback":{"score":9}}`,
		"fallback-weighted-peer Answer: A": `{"fallback":{"score":2}}`,
		"fallback-weighted-peer Answer: C": `{"fallback":{"score":4}}`,
		"weighted-peer-response Answer: B": `{"correctness":{"score":1},"clarity":{"score":1}}`,
		"weighted-peer-response Answer: A": `{"correctness":{"score":9},"clarity":{"score":9}}`,
		"weighted-peer-response Answer: C": `{"correctness":{"score":5},"clarity":{"score":5}}`,
	}))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got != "Answer: B" {
		t.Fatalf("agg--result = %q, want configured fallback rubric peer winner", got)
	}
}

func TestCompileAggregationUnanimousFallbackStillShowsEvaluatorJobs(t *testing.T) {
	tests := []struct {
		name         string
		method       workflow.AggregationMethod
		workflowID   string
		config       map[string]interface{}
		candidates   []executionCandidate
		wantLLMCalls int
	}{
		{
			name:       "judge keeps judge evaluator visible",
			method:     workflow.AggMethodJudge,
			workflowID: "aggregation-judge",
			config: map[string]interface{}{
				"judge_model":         "mock-model",
				"system_prompt":       "Judge.",
				"prompt":              "judge-response {{responses}}",
				"extraction_strategy": "regex",
				"extraction_pattern":  `Answer:\s*([A-D])`,
				"temperature":         float64(0),
				"max_tokens":          float64(512),
				"repair_max_tokens":   float64(128),
			},
			candidates:   []executionCandidate{{"agent-a", "Answer: A"}, {"agent-b", "Answer: A"}},
			wantLLMCalls: 3,
		},
		{
			name:       "debate_decide keeps judge evaluator visible",
			method:     workflow.AggMethodDebateDecide,
			workflowID: "aggregation-debate-decide",
			config: map[string]interface{}{
				"judge_model":         "mock-model",
				"system_prompt":       "Judge camps.",
				"prompt":              "debate-response {{camps}}",
				"extraction_strategy": "regex",
				"extraction_pattern":  `Answer:\s*([A-D])`,
				"temperature":         float64(0),
				"max_tokens":          float64(512),
				"repair_max_tokens":   float64(128),
			},
			candidates:   []executionCandidate{{"agent-a", "Answer: A"}, {"agent-b", "Answer: A"}},
			wantLLMCalls: 3,
		},
		{
			name:       "scoring keeps per-candidate scorers visible",
			method:     workflow.AggMethodScoring,
			workflowID: "aggregation-scoring",
			config: map[string]interface{}{
				"scoring_model":       "mock-model",
				"system_prompt":       "Score.",
				"prompt":              "score-response {{candidate}}",
				"extraction_strategy": "regex",
				"extraction_pattern":  `Answer:\s*([A-D])`,
				"temperature":         float64(0),
				"max_tokens":          float64(512),
			},
			candidates:   []executionCandidate{{"agent-a", "Answer: A"}, {"agent-b", "Answer: A"}},
			wantLLMCalls: 4,
		},
		{
			name:       "peer_matrix keeps peer review cross product visible",
			method:     workflow.AggMethodPeerMatrix,
			workflowID: "aggregation-peer-matrix",
			config: map[string]interface{}{
				"eval_system_prompt":  "Review.",
				"eval_prompt":         "peer-response {{candidate}}",
				"extraction_strategy": "regex",
				"extraction_pattern":  `Answer:\s*([A-D])`,
				"rubric": []interface{}{
					map[string]interface{}{"name": "quality", "weight": float64(1), "description": "Prefer the best answer."},
				},
				"temperature":   float64(0),
				"max_tokens":    float64(512),
				"normalization": "none",
				"max_parallel":  float64(6),
			},
			candidates:   []executionCandidate{{"agent-a", "Answer: A"}, {"agent-b", "Answer: A"}, {"agent-c", "Answer: A"}},
			wantLLMCalls: 9,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := aggregationExecutionParent(tt.method, tt.workflowID, tt.config, tt.candidates)
			client, provider := testAggregationCountingLLMClientWithResponses(map[string]string{
				"emit agent-c":    "Answer: A",
				"emit agent-b":    "Answer: A",
				"debate-response": "A",
			})
			got, err := executeCompiledAggregationForTest(parent, tt.workflowID, client)
			if err != nil {
				t.Fatalf("Execute failed: %v", err)
			}
			if got != "Answer: A" {
				t.Fatalf("agg--result = %q, want unanimous first answer", got)
			}
			if got := provider.CallCount(); got != tt.wantLLMCalls {
				t.Fatalf("LLM calls = %d, want %d visible/executed jobs", got, tt.wantLLMCalls)
			}
		})
	}
}

func TestCompileAggregationDebateParsesFreeformCampAnswer(t *testing.T) {
	parent := aggregationExecutionParent(workflow.AggMethodDebateDecide, "aggregation-debate-decide", map[string]interface{}{
		"judge_model":         "mock-model",
		"system_prompt":       "Judge camps.",
		"prompt":              "freeform-debate-response {{camps}}",
		"extraction_strategy": "regex",
		"extraction_pattern":  `Final answer:\s*([A-Za-z]+)`,
		"temperature":         float64(0),
		"max_tokens":          float64(512),
		"repair_max_tokens":   float64(128),
	}, []executionCandidate{
		{"agent-a", "Final answer: Paris"},
		{"agent-b", "Final answer: London"},
	})
	got, err := executeCompiledAggregationForTest(parent, "aggregation-debate-decide", testAggregationLLMClientWithResponses(map[string]string{
		"emit agent-a":              "Final answer: Paris",
		"emit agent-b":              "Final answer: London",
		"freeform-debate-response":  "WINNER: London",
		"repair-selection-call":     "invalid repair",
		"Return only the winning":   "invalid repair",
		"Return only the winning c": "invalid repair",
	}))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got != "Final answer: London" {
		t.Fatalf("agg--result = %q, want freeform camp winner output", got)
	}
}

func TestCompileAggregationPeerMatrixAveragesMultipleReviewers(t *testing.T) {
	parent := aggregationExecutionParent(workflow.AggMethodPeerMatrix, "aggregation-peer-matrix", map[string]interface{}{
		"eval_system_prompt": "Review.",
		"eval_prompt":        "weighted-peer-response {{candidate}}\n{{rubric}}",
		"temperature":        float64(0),
		"max_tokens":         float64(512),
		"normalization":      "none",
		"max_parallel":       float64(6),
		"rubric": []interface{}{
			map[string]interface{}{"name": "correctness", "weight": float64(0.75), "description": "Correctness"},
			map[string]interface{}{"name": "clarity", "weight": float64(0.25), "description": "Clarity"},
		},
	}, []executionCandidate{
		{"agent-a", "Answer: A"},
		{"agent-b", "Answer: B"},
		{"agent-c", "Answer: C"},
	})
	got, err := executeCompiledAggregationForTest(parent, "aggregation-peer-matrix", testAggregationLLMClientWithResponses(map[string]string{
		"emit agent-c": "Answer: C",
	}))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if got != "Answer: B" {
		t.Fatalf("agg--result = %q, want candidate with highest averaged peer score", got)
	}
}

func executeCompiledAggregationForTest(parent *workflow.Workflow, workflowID string, client *providers.Client) (string, error) {
	compiled, _, err := Compile(context.Background(), parent, StaticResolver{
		workflowID: testAggregationSourceWorkflow(workflowID),
	})
	if err != nil {
		return "", err
	}
	result := workflow.NewValidator(nil).Validate(compiled)
	if !result.Valid {
		return "", fmt.Errorf("compiled validation failed: %+v", result.Errors)
	}
	execResult, err := workflow.NewExecutor(client).Execute(context.Background(), compiled, nil)
	if err != nil {
		return "", err
	}
	if !execResult.Success {
		return "", fmt.Errorf("execution failed: %s", execResult.Error)
	}
	got, _ := execResult.Context["agg--result"].(string)
	return got, nil
}

type executionCandidate struct {
	id     string
	output string
}

func aggregationExecutionParent(method workflow.AggregationMethod, workflowID string, config map[string]interface{}, candidates []executionCandidate) *workflow.Workflow {
	nodes := make([]*workflow.Node, 0, len(candidates)+1)
	edges := make([]*workflow.Edge, 0, len(candidates))
	inputIDs := make([]interface{}, 0, len(candidates))
	for _, candidate := range candidates {
		node := testPromptNode(candidate.id)
		node.Prompt = "emit " + candidate.id
		nodes = append(nodes, node)
		edges = append(edges, &workflow.Edge{ID: candidate.id + "-agg", Source: candidate.id, Target: "agg"})
		inputIDs = append(inputIDs, candidate.id)
	}
	nodes = append(nodes, &workflow.Node{
		ID:                "agg",
		Type:              workflow.NodeTypeWorkflowRef,
		WorkflowRefID:     workflowID,
		AggregationMethod: method,
		AggregationConfig: config,
		Metadata: map[string]interface{}{
			"input_ids":             inputIDs,
			"output_name":           "final_answer",
			"output_format":         "text",
			"aggregation_anchor_id": "agg",
			"aggregation_method":    string(method),
		},
	})
	return &workflow.Workflow{
		ID:      "parent",
		Context: map[string]interface{}{"user_prompt": "Pick the best answer."},
		Nodes:   nodes,
		Edges:   edges,
	}
}

func testAggregationLLMClient() *providers.Client {
	return testAggregationLLMClientWithResponses(nil)
}

func testAggregationLLMClientWithResponses(overrides map[string]string) *providers.Client {
	client, _ := testAggregationCountingLLMClientWithResponses(overrides)
	return client
}

func testAggregationCountingLLMClientWithResponses(overrides map[string]string) (*providers.Client, *aggregationScriptedProvider) {
	responses := map[string]string{
		"emit agent-a":                      "Answer: A",
		"emit agent-b":                      "Answer: B",
		"emit agent-c":                      "Answer: B",
		"synthesis-response":                "Synthesized answer",
		"judge-response":                    "WINNER: B",
		"debate-response":                   "B",
		"score-response Answer: A":          `{"score":2}`,
		"peer-response Answer: A":           `{"score":2}`,
		"weighted-score-response Answer: B": `{"scores":{"correctness":9,"clarity":8}}`,
		"weighted-score-response Answer: A": `{"scores":{"correctness":2,"clarity":3}}`,
		"weighted-peer-response Answer: B":  `{"correctness":{"score":9},"clarity":{"score":8}}`,
		"weighted-peer-response Answer: A":  `{"correctness":{"score":2},"clarity":{"score":3}}`,
		"weighted-peer-response Answer: C":  `{"correctness":{"score":4},"clarity":{"score":4}}`,
		"Answer: B":                         `{"score":9}`,
		"Answer: A":                         `{"score":2}`,
		"invalid-judge-response":            "no valid label",
	}
	for key, value := range overrides {
		responses[key] = value
	}
	provider := &aggregationScriptedProvider{responses: responses}
	registry := providers.NewRegistry()
	registry.Register(provider)
	return providers.NewClient(registry, nil), provider
}

type aggregationScriptedProvider struct {
	mu        sync.Mutex
	responses map[string]string
	calls     int
}

func (p *aggregationScriptedProvider) Name() string { return "mock" }

func (p *aggregationScriptedProvider) Models() []providers.Model {
	return []providers.Model{
		{
			ID:         "mock-model",
			Name:       "Mock Model",
			Provider:   "mock",
			ContextLen: 4096,
			MaxTokens:  2048,
			Available:  true,
		},
		{
			ID:         "mock-rubric-model",
			Name:       "Mock Rubric Model",
			Provider:   "mock",
			ContextLen: 4096,
			MaxTokens:  2048,
			Available:  true,
		},
	}
}

func (p *aggregationScriptedProvider) Complete(_ context.Context, req *providers.CompletionRequest) (*providers.CompletionResponse, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()

	var prompt strings.Builder
	for _, msg := range req.Messages {
		prompt.WriteString(msg.Content)
		prompt.WriteString("\n")
	}
	response := "Mock response"
	longest := ""
	for key, candidate := range p.responses {
		if strings.Contains(prompt.String(), key) && len(key) > len(longest) {
			longest = key
			response = candidate
		}
	}
	return &providers.CompletionResponse{
		ID:      "mock-response",
		Content: response,
		Model:   req.Model,
		Usage: providers.Usage{
			PromptTokens:     len(prompt.String()) / 4,
			CompletionTokens: len(response) / 4,
			TotalTokens:      (len(prompt.String()) + len(response)) / 4,
		},
	}, nil
}

func (p *aggregationScriptedProvider) EstimateTokens(text string) int { return len(text) / 4 }

func (p *aggregationScriptedProvider) Cost(string, int, int) float64 { return 0 }

func (p *aggregationScriptedProvider) CallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func countCompiledAggregationPrompts(wf *workflow.Workflow, anchorID string) int {
	count := 0
	for _, node := range wf.Nodes {
		if node.Type == workflow.NodeTypePrompt && strings.HasPrefix(node.ID, anchorID+generatedIDDelimiter) {
			count++
		}
	}
	return count
}
