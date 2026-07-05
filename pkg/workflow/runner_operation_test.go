package workflow

import (
	"context"
	"strings"
	"testing"
)

func TestOperationRunnerCollectInputs(t *testing.T) {
	res := runOperationForTest(t, OperationCollectInputs, map[string]interface{}{
		"input_ids": []interface{}{"a", "b"},
		"separator": "\n",
	}, map[string]interface{}{
		"a": "Alpha", "a__model": "model-a",
		"b": "Beta", "b__model": "model-b",
	})
	if !strings.Contains(res.Output, `"text":"Alpha\nBeta"`) {
		t.Fatalf("output = %s", res.Output)
	}
	if !strings.Contains(res.Output, `"model":"model-a"`) {
		t.Fatalf("missing model metadata: %s", res.Output)
	}
}

func TestOperationRunnerCollectInputsRawText(t *testing.T) {
	res := runOperationForTest(t, OperationCollectInputs, map[string]interface{}{
		"input_ids": []interface{}{"a", "b"},
		"separator": "|",
		"raw_text":  true,
	}, map[string]interface{}{"a": "Alpha", "b": "Beta"})
	if res.Output != "Alpha|Beta" {
		t.Fatalf("output = %q, want raw joined text", res.Output)
	}
}

func TestOperationRunnerFormatCandidates(t *testing.T) {
	res := runOperationForTest(t, OperationFormatCandidates, map[string]interface{}{
		"candidates": []interface{}{
			map[string]interface{}{"id": "a", "model": "m1", "output": "Alpha"},
			map[string]interface{}{"id": "b", "model": "m2", "output": "Beta"},
		},
	}, nil)
	if !strings.Contains(res.Output, "Candidate A") || !strings.Contains(res.Output, "model m1") {
		t.Fatalf("output = %s", res.Output)
	}
}

func TestOperationRunnerFormatCandidatesBlindRawText(t *testing.T) {
	res := runOperationForTest(t, OperationFormatCandidates, map[string]interface{}{
		"input_ids": []interface{}{"a", "b"},
		"blind":     true,
		"raw_text":  true,
	}, map[string]interface{}{
		"a": "Alpha", "a__model": "model-a",
		"b": "Beta", "b__model": "model-b",
	})
	if !strings.Contains(res.Output, "Candidate A:\nAlpha") || strings.Contains(res.Output, "model-a") || strings.Contains(res.Output, "a)") {
		t.Fatalf("output = %s", res.Output)
	}
}

func TestOperationRunnerExtractAnswer(t *testing.T) {
	res := runOperationForTest(t, OperationExtractAnswer, map[string]interface{}{
		"text": "Final answer: B",
	}, nil)
	if !strings.Contains(res.Output, `"answer":"B"`) {
		t.Fatalf("output = %s", res.Output)
	}
}

func TestOperationRunnerExtractAnswerRawAnswer(t *testing.T) {
	res := runOperationForTest(t, OperationExtractAnswer, map[string]interface{}{
		"text":       "Final answer: B",
		"raw_answer": true,
	}, nil)
	if res.Output != "B" {
		t.Fatalf("output = %q, want raw answer", res.Output)
	}
}

func TestOperationRunnerCountVotes(t *testing.T) {
	res := runOperationForTest(t, OperationCountVotes, map[string]interface{}{
		"answers": []interface{}{"A", "B", "A"},
	}, nil)
	if !strings.Contains(res.Output, `"winner":"A"`) {
		t.Fatalf("output = %s", res.Output)
	}
	if !strings.Contains(res.Output, `"A":2`) {
		t.Fatalf("output = %s", res.Output)
	}
}

func TestOperationRunnerCountVotesUnanimityRequiresEveryAnswerExtracted(t *testing.T) {
	res := runOperationForTest(t, OperationCountVotes, map[string]interface{}{
		"answers": []interface{}{"A", "", "A"},
	}, nil)
	if strings.Contains(res.Output, `"unanimous":true`) {
		t.Fatalf("output = %s, want blank answer to disable unanimity", res.Output)
	}
	if !strings.Contains(res.Output, `"winner":"A"`) {
		t.Fatalf("output = %s, want plurality winner still recorded", res.Output)
	}
}

func TestOperationRunnerGroupAnswerCamps(t *testing.T) {
	res := runOperationForTest(t, OperationGroupAnswerCamps, map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{"id": "a", "answer": "A"},
			map[string]interface{}{"id": "b", "answer": "B"},
			map[string]interface{}{"id": "c", "answer": "A"},
		},
	}, nil)
	if !strings.Contains(res.Output, `"answer":"A"`) || !strings.Contains(res.Output, `"member_ids":["a","c"]`) {
		t.Fatalf("output = %s", res.Output)
	}
}

func TestOperationRunnerSelectWinner(t *testing.T) {
	res := runOperationForTest(t, OperationSelectWinner, map[string]interface{}{
		"scores":  map[string]interface{}{"a": 0.2, "b": 0.9},
		"outputs": map[string]interface{}{"a": "Alpha", "b": "Beta"},
	}, nil)
	if !strings.Contains(res.Output, `"winner":"b"`) || !strings.Contains(res.Output, `"output":"Beta"`) {
		t.Fatalf("output = %s", res.Output)
	}
}

func TestOperationRunnerSelectWinnerAcceptsReduceScoresJSON(t *testing.T) {
	res := runOperationForTest(t, OperationSelectWinner, map[string]interface{}{
		"scores":  `{"scores":{"a":0.2,"b":0.9},"winner":"b"}`,
		"outputs": map[string]interface{}{"a": "Alpha", "b": "Beta"},
	}, nil)
	if !strings.Contains(res.Output, `"winner":"b"`) || !strings.Contains(res.Output, `"output":"Beta"`) {
		t.Fatalf("output = %s", res.Output)
	}
}

func TestOperationRunnerSelectWinnerByLabel(t *testing.T) {
	res := runOperationForTest(t, OperationSelectWinner, map[string]interface{}{
		"selection":   "WINNER: B",
		"labels":      []interface{}{"A", "B"},
		"label_to_id": map[string]interface{}{"A": "agent-a", "B": "agent-b"},
		"outputs":     map[string]interface{}{"agent-a": "Alpha", "agent-b": "Beta"},
		"raw_output":  true,
	}, nil)
	if res.Output != "Beta" {
		t.Fatalf("output = %q, want selected candidate output", res.Output)
	}
}

func TestOperationRunnerSelectWinnerByVoteResult(t *testing.T) {
	res := runOperationForTest(t, OperationSelectWinner, map[string]interface{}{
		"vote_result":        `{"counts":{"A":2,"B":1},"winner":"A","tie":false,"total":3}`,
		"tie_breaker_method": "error",
		"items": []interface{}{
			map[string]interface{}{"id": "agent-a", "answer": "A", "output": "Alpha"},
			map[string]interface{}{"id": "agent-b", "answer": "B", "output": "Beta"},
			map[string]interface{}{"id": "agent-c", "answer": "A", "output": "Gamma"},
		},
		"raw_output": true,
	}, nil)
	if res.Output != "Alpha" {
		t.Fatalf("output = %q, want first winning answer output", res.Output)
	}
}

func TestOperationRunnerSelectWinnerByVoteResultRejectsEmptyWinnerUnlessFirstFallback(t *testing.T) {
	_, err := (&OperationNodeRunner{}).Execute(&NodeContext{
		Ctx:    context.Background(),
		NodeID: "winner",
		Node: &Node{
			ID:            "winner",
			Type:          NodeTypeOperation,
			OperationType: OperationSelectWinner,
			OperationConfig: map[string]interface{}{
				"vote_result":        `{"counts":{},"winner":"","tie":false,"total":2,"unanimous":false}`,
				"tie_breaker_method": "error",
				"items": []interface{}{
					map[string]interface{}{"id": "agent-a", "answer": "", "output": "Alpha"},
					map[string]interface{}{"id": "agent-b", "answer": "", "output": "Beta"},
				},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "no winning answer") {
		t.Fatalf("expected no winning answer error, got %v", err)
	}

	res := runOperationForTest(t, OperationSelectWinner, map[string]interface{}{
		"vote_result":        `{"counts":{},"winner":"","tie":false,"total":2,"unanimous":false}`,
		"tie_breaker_method": "first",
		"items": []interface{}{
			map[string]interface{}{"id": "agent-a", "answer": "", "output": "Alpha"},
			map[string]interface{}{"id": "agent-b", "answer": "", "output": "Beta"},
		},
		"raw_output": true,
	}, nil)
	if res.Output != "Alpha" {
		t.Fatalf("output = %q, want first fallback output", res.Output)
	}
}

func TestOperationRunnerSelectWinnerByAnswer(t *testing.T) {
	res := runOperationForTest(t, OperationSelectWinner, map[string]interface{}{
		"winner_answer": "B",
		"items": []interface{}{
			map[string]interface{}{"id": "agent-a", "answer": "A", "output": "Alpha"},
			map[string]interface{}{"id": "agent-b", "answer": "B", "output": "Beta"},
		},
		"raw_output": true,
	}, nil)
	if res.Output != "Beta" {
		t.Fatalf("output = %q, want selected answer output", res.Output)
	}
}

func TestOperationRunnerReduceScores(t *testing.T) {
	res := runOperationForTest(t, OperationReduceScores, map[string]interface{}{
		"scores": []interface{}{
			map[string]interface{}{"id": "a", "score": 4},
			map[string]interface{}{"id": "a", "score": 2},
			map[string]interface{}{"id": "b", "score": 3},
		},
	}, nil)
	if !strings.Contains(res.Output, `"a":3`) || !strings.Contains(res.Output, `"winner":"a"`) {
		t.Fatalf("output = %s", res.Output)
	}
}

func TestOperationRunnerParseJSONField(t *testing.T) {
	res := runOperationForTest(t, OperationParseJSONField, map[string]interface{}{
		"text":  `{"answer":"C","confidence":0.7}`,
		"field": "answer",
	}, nil)
	if !strings.Contains(res.Output, `"value":"C"`) {
		t.Fatalf("output = %s", res.Output)
	}
}

func TestOperationRunnerParseJSONFieldRaw(t *testing.T) {
	res := runOperationForTest(t, OperationParseJSONField, map[string]interface{}{
		"text":  `{"answer":"C","confidence":0.7}`,
		"field": "answer",
		"raw":   true,
	}, nil)
	if res.Output != "C" {
		t.Fatalf("output = %q, want raw field", res.Output)
	}
}

func TestOperationRunnerParseJSONFieldFallbacksAndOptional(t *testing.T) {
	tests := []struct {
		name      string
		config    map[string]interface{}
		want      string
		wantExact bool
	}{
		{
			name: "empty text uses fallback",
			config: map[string]interface{}{
				"text":     "",
				"field":    "answer",
				"fallback": "fallback answer",
			},
			want: `"value":"fallback answer"`,
		},
		{
			name: "missing field uses raw fallback",
			config: map[string]interface{}{
				"text":     `{"other":"value"}`,
				"field":    "answer",
				"raw":      true,
				"fallback": "fallback answer",
			},
			want: "fallback answer",
		},
		{
			name: "empty array uses fallback",
			config: map[string]interface{}{
				"text":     `{"rubric":[]}`,
				"field":    "rubric",
				"raw":      true,
				"json_raw": true,
				"fallback": []interface{}{map[string]interface{}{"name": "fallback", "weight": float64(1)}},
			},
			want: `"name":"fallback"`,
		},
		{
			name: "optional missing field returns empty",
			config: map[string]interface{}{
				"text":     `{"other":"value"}`,
				"field":    "answer",
				"optional": true,
			},
			want:      "",
			wantExact: true,
		},
		{
			name: "json raw preserves object",
			config: map[string]interface{}{
				"text":     `{"rubric":{"correctness":1}}`,
				"field":    "rubric",
				"raw":      true,
				"json_raw": true,
			},
			want: `{"correctness":1}`,
		},
		{
			name: "raw string preserves case",
			config: map[string]interface{}{
				"text":  `{"answer":"choice a"}`,
				"field": "answer",
				"raw":   true,
			},
			want: "choice a",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := runOperationForTest(t, OperationParseJSONField, tt.config, nil)
			if tt.wantExact {
				if res.Output != tt.want {
					t.Fatalf("output = %q, want %q", res.Output, tt.want)
				}
				return
			}
			if !strings.Contains(res.Output, tt.want) {
				t.Fatalf("output = %q, want to contain %q", res.Output, tt.want)
			}
		})
	}
}

func TestOperationRunnerParseJSONFieldUsesFallbackForInvalidRubric(t *testing.T) {
	fallbackRubric := []interface{}{
		map[string]interface{}{"name": "fallback", "weight": float64(1), "description": "Fallback rubric"},
	}
	tests := []struct {
		name string
		text string
	}{
		{
			name: "not criteria list",
			text: `{"rubric":"not a criteria list"}`,
		},
		{
			name: "too many criteria",
			text: `{"rubric":[{"name":"a","weight":0.1},{"name":"b","weight":0.1},{"name":"c","weight":0.1},{"name":"d","weight":0.1},{"name":"e","weight":0.1},{"name":"f","weight":0.1},{"name":"g","weight":0.1},{"name":"h","weight":0.1},{"name":"i","weight":0.1},{"name":"j","weight":0.1},{"name":"k","weight":0.1}]}`,
		},
		{
			name: "blank criterion name",
			text: `{"rubric":[{"name":"Correctness","weight":0.5},{"name":" ","weight":0.5}]}`,
		},
		{
			name: "non-positive criterion weight",
			text: `{"rubric":[{"name":"Correctness","weight":1},{"name":"Clarity","weight":0}]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := runOperationForTest(t, OperationParseJSONField, map[string]interface{}{
				"text":     tt.text,
				"field":    "rubric",
				"raw":      true,
				"json_raw": true,
				"fallback": fallbackRubric,
			}, nil)
			if !strings.Contains(res.Output, `"name":"fallback"`) {
				t.Fatalf("output = %q, want configured fallback rubric JSON", res.Output)
			}
		})
	}
}

func TestOperationRunnerParseJSONFieldFormatsRubric(t *testing.T) {
	res := runOperationForTest(t, OperationParseJSONField, map[string]interface{}{
		"text":          `{"rubric":[{"name":"Correctness","weight":0.75,"description":"Right answer"},{"name":"Clarity","weight":0.25,"description":"Clear explanation"}]}`,
		"field":         "rubric",
		"raw":           true,
		"format_rubric": true,
	}, nil)
	if !strings.Contains(res.Output, "- Correctness (0.75): Right answer") ||
		!strings.Contains(res.Output, "- Clarity (0.25): Clear explanation") {
		t.Fatalf("output = %q, want formatted rubric bullets", res.Output)
	}
}

func TestOperationRunnerSelectWinnerUsesSecondaryFallback(t *testing.T) {
	res := runOperationForTest(t, OperationSelectWinner, map[string]interface{}{
		"winner_answer":      "",
		"fallback_output":    "",
		"secondary_fallback": "single camp output",
		"items": []interface{}{
			map[string]interface{}{"id": "agent-a", "answer": "A", "output": "agent output"},
		},
		"raw_output": true,
	}, nil)
	if res.Output != "single camp output" {
		t.Fatalf("output = %q, want secondary fallback", res.Output)
	}
}

func TestOperationRunnerParseSelection(t *testing.T) {
	res := runOperationForTest(t, OperationParseSelection, map[string]interface{}{
		"text":   "After review.\nWINNER: B",
		"labels": []interface{}{"A", "B"},
		"raw":    true,
	}, nil)
	if res.Output != "B" {
		t.Fatalf("output = %q, want parsed selection", res.Output)
	}
}

func TestOperationRunnerInterpolatesExactTemplateToRawValue(t *testing.T) {
	res := runOperationForTest(t, OperationCountVotes, map[string]interface{}{
		"answers": "{{answers}}",
	}, map[string]interface{}{"answers": []interface{}{"D", "D", "C"}})
	if !strings.Contains(res.Output, `"winner":"D"`) {
		t.Fatalf("output = %s", res.Output)
	}
}

func TestOperationRunnerRejectsUnknownOperation(t *testing.T) {
	_, err := (&OperationNodeRunner{}).Execute(&NodeContext{
		Ctx:    context.Background(),
		NodeID: "bad",
		Node:   &Node{ID: "bad", Type: NodeTypeOperation, OperationType: "bad_op"},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown operation_type") {
		t.Fatalf("expected unknown operation error, got %v", err)
	}
}

func TestOperationRunnerRejectsMissingRequiredConfig(t *testing.T) {
	_, err := (&OperationNodeRunner{}).Execute(&NodeContext{
		Ctx:    context.Background(),
		NodeID: "votes",
		Node:   &Node{ID: "votes", Type: NodeTypeOperation, OperationType: OperationCountVotes},
	})
	if err == nil || !strings.Contains(err.Error(), "answers is required") {
		t.Fatalf("expected missing answers error, got %v", err)
	}
}

func TestOperationRunnerRejectsMissingInputContext(t *testing.T) {
	_, err := (&OperationNodeRunner{}).Execute(&NodeContext{
		Ctx:    context.Background(),
		NodeID: "collect",
		Node: &Node{
			ID:              "collect",
			Type:            NodeTypeOperation,
			OperationType:   OperationCollectInputs,
			OperationConfig: map[string]interface{}{"input_ids": []interface{}{"missing"}},
		},
		WorkflowContext: map[string]interface{}{},
	})
	if err == nil || !strings.Contains(err.Error(), `input "missing" not found in workflow context`) {
		t.Fatalf("expected missing context error, got %v", err)
	}
}

func TestExecutorRunsOperationWithoutRetryPolicy(t *testing.T) {
	wf := &Workflow{
		ID: "operation-executor",
		Nodes: []*Node{
			{
				ID:              "votes",
				Type:            NodeTypeOperation,
				OperationType:   OperationCountVotes,
				OperationConfig: map[string]interface{}{"answers": "{{answers}}"},
			},
			{
				ID:                "result",
				Type:              NodeTypeResult,
				AggregationMethod: AggMethodCollect,
				RetryPolicy:       NoRetryPolicy(),
				Metadata: map[string]interface{}{
					"input_ids": []interface{}{"votes"},
				},
			},
		},
		Edges: []*Edge{
			{ID: "votes-result", Source: "votes", Target: "result"},
		},
		Context: map[string]interface{}{"answers": []interface{}{"A", "B", "A"}},
	}

	result, err := NewExecutor(nil).Execute(context.Background(), wf, nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got %+v", result)
	}
	if got := result.Context["votes"]; !strings.Contains(got.(string), `"winner":"A"`) {
		t.Fatalf("expected operation output in workflow context, got %v", got)
	}
	if got := result.Outputs["result"]; !strings.Contains(got.(string), `"winner":"A"`) {
		t.Fatalf("expected result output to include operation output, got %v", got)
	}
}

func runOperationForTest(t *testing.T, operationType string, config map[string]interface{}, ctx map[string]interface{}) *NodeResult {
	t.Helper()
	res, err := (&OperationNodeRunner{}).Execute(&NodeContext{
		Ctx:             context.Background(),
		NodeID:          "op",
		Node:            &Node{ID: "op", Type: NodeTypeOperation, OperationType: operationType, OperationConfig: config},
		WorkflowContext: ctx,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !res.Success {
		t.Fatalf("result not successful: %+v", res)
	}
	return res
}
