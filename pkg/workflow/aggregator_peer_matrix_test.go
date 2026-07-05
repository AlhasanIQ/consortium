package workflow

import (
	"context"
	"testing"
)

func TestPeerMatrixParseCriterionScores(t *testing.T) {
	tests := []struct {
		name      string
		response  string
		wantOK    bool
		wantCount int
		wantKey   string
		wantScore float64
	}{
		{
			name:      "valid criterion scores with reasoning",
			response:  `{"logical_soundness": {"reasoning": "Good logic", "score": 8}, "clarity": {"reasoning": "Clear", "score": 7}}`,
			wantOK:    true,
			wantCount: 2,
			wantKey:   "logical_soundness",
			wantScore: 8,
		},
		{
			name:      "mixed case keys are normalized",
			response:  `{"Logical Soundness": {"reasoning": "ok", "score": 9}}`,
			wantOK:    true,
			wantCount: 1,
			wantKey:   "logical_soundness",
			wantScore: 9,
		},
		{
			name:      "score out of range low",
			response:  `{"key": {"reasoning": "bad", "score": 0}}`,
			wantOK:    false,
			wantCount: 0,
		},
		{
			name:      "score out of range high",
			response:  `{"key": {"reasoning": "too good", "score": 11}}`,
			wantOK:    false,
			wantCount: 0,
		},
		{
			name:      "no JSON in response",
			response:  `This has no JSON at all`,
			wantOK:    false,
			wantCount: 0,
		},
		{
			name:      "empty string",
			response:  "",
			wantOK:    false,
			wantCount: 0,
		},
		{
			name:      "JSON embedded in text",
			response:  `Here is my evaluation: {"evidence_analysis": {"reasoning": "thorough", "score": 6}} That's my score.`,
			wantOK:    true,
			wantCount: 1,
			wantKey:   "evidence_analysis",
			wantScore: 6,
		},
		{
			name:      "malformed JSON",
			response:  `{"key": {"reasoning": "ok", "score": `,
			wantOK:    false,
			wantCount: 0,
		},
		{
			name:      "boundary score 1",
			response:  `{"completeness": {"reasoning": "minimal", "score": 1}}`,
			wantOK:    true,
			wantCount: 1,
			wantKey:   "completeness",
			wantScore: 1,
		},
		{
			name:      "boundary score 10",
			response:  `{"completeness": {"reasoning": "perfect", "score": 10}}`,
			wantOK:    true,
			wantCount: 1,
			wantKey:   "completeness",
			wantScore: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scores, ok := parseCriterionScores(tt.response)
			if ok != tt.wantOK {
				t.Fatalf("parseCriterionScores() ok = %v, want %v", ok, tt.wantOK)
			}
			if len(scores) != tt.wantCount {
				t.Fatalf("parseCriterionScores() count = %d, want %d", len(scores), tt.wantCount)
			}
			if tt.wantOK && tt.wantKey != "" {
				if got, exists := scores[tt.wantKey]; !exists {
					t.Errorf("expected key %q not found in scores", tt.wantKey)
				} else if got != tt.wantScore {
					t.Errorf("scores[%q] = %v, want %v", tt.wantKey, got, tt.wantScore)
				}
			}
		})
	}
}

func TestPeerMatrixBuildEvalTasks(t *testing.T) {
	tests := []struct {
		name      string
		inputs    []AgentOutput
		wantCount int
	}{
		{
			name:      "empty inputs",
			inputs:    nil,
			wantCount: 0,
		},
		{
			name: "single input produces no tasks (no self-evaluation)",
			inputs: []AgentOutput{
				{AgentID: "a1", Model: "m1", Output: "out1"},
			},
			wantCount: 0,
		},
		{
			name: "two inputs produce 2 tasks",
			inputs: []AgentOutput{
				{AgentID: "a1", Model: "m1", Output: "out1"},
				{AgentID: "a2", Model: "m2", Output: "out2"},
			},
			wantCount: 2,
		},
		{
			name: "three inputs produce 6 tasks (N*(N-1))",
			inputs: []AgentOutput{
				{AgentID: "a1", Model: "m1", Output: "out1"},
				{AgentID: "a2", Model: "m2", Output: "out2"},
				{AgentID: "a3", Model: "m3", Output: "out3"},
			},
			wantCount: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tasks := buildEvalTasks(tt.inputs)
			if len(tasks) != tt.wantCount {
				t.Fatalf("buildEvalTasks() returned %d tasks, want %d", len(tasks), tt.wantCount)
			}
			// Verify no self-evaluation
			for _, task := range tasks {
				if task.ReviewerID == task.CandidateID {
					t.Errorf("self-evaluation found: reviewer=%s candidate=%s", task.ReviewerID, task.CandidateID)
				}
			}
		})
	}
}

func TestPeerMatrixBuildEvaluationMatrix(t *testing.T) {
	inputs := []AgentOutput{
		{AgentID: "a1", Output: "out1"},
		{AgentID: "a2", Output: "out2"},
		{AgentID: "a3", Output: "out3"},
	}

	tests := []struct {
		name             string
		results          []evalResult
		wantInvalidCount int
		wantWinnerID     string
	}{
		{
			name: "all valid scores, a2 wins",
			results: []evalResult{
				{ReviewerID: "a1", CandidateID: "a2", Score: 8, Valid: true},
				{ReviewerID: "a1", CandidateID: "a3", Score: 6, Valid: true},
				{ReviewerID: "a2", CandidateID: "a1", Score: 5, Valid: true},
				{ReviewerID: "a2", CandidateID: "a3", Score: 7, Valid: true},
				{ReviewerID: "a3", CandidateID: "a1", Score: 4, Valid: true},
				{ReviewerID: "a3", CandidateID: "a2", Score: 9, Valid: true},
			},
			wantInvalidCount: 0,
			wantWinnerID:     "a2", // avg(8,9)=8.5 > avg(6,7)=6.5 > avg(5,4)=4.5
		},
		{
			name: "some invalid reviews",
			results: []evalResult{
				{ReviewerID: "a1", CandidateID: "a2", Score: 7, Valid: true},
				{ReviewerID: "a1", CandidateID: "a3", Score: 0, Valid: false},
				{ReviewerID: "a2", CandidateID: "a1", Score: 5, Valid: true},
				{ReviewerID: "a2", CandidateID: "a3", Score: 6, Valid: true},
				{ReviewerID: "a3", CandidateID: "a1", Score: 0, Valid: false},
				{ReviewerID: "a3", CandidateID: "a2", Score: 8, Valid: true},
			},
			wantInvalidCount: 2,
			wantWinnerID:     "a2", // avg(7,8)=7.5
		},
		{
			name:             "no valid reviews",
			results:          []evalResult{{Valid: false}, {Valid: false}},
			wantInvalidCount: 2,
			wantWinnerID:     "", // empty scores
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matrix := buildEvaluationMatrix(tt.results, inputs, "none")
			if matrix.InvalidCount != tt.wantInvalidCount {
				t.Errorf("InvalidCount = %d, want %d", matrix.InvalidCount, tt.wantInvalidCount)
			}
			winner, _ := selectWinner(matrix.FinalScores, inputs)
			if winner != tt.wantWinnerID {
				t.Errorf("winner = %q, want %q (scores: %v)", winner, tt.wantWinnerID, matrix.FinalScores)
			}
		})
	}
}

func TestPeerMatrixSelectWinner(t *testing.T) {
	inputs := []AgentOutput{
		{AgentID: "a1", Output: "out1"},
		{AgentID: "a2", Output: "out2"},
		{AgentID: "a3", Output: "out3"},
	}

	tests := []struct {
		name       string
		scores     map[string]float64
		wantID     string
		wantOutput string
	}{
		{
			name:       "empty scores",
			scores:     map[string]float64{},
			wantID:     "",
			wantOutput: "",
		},
		{
			name:       "clear winner",
			scores:     map[string]float64{"a1": 5.0, "a2": 9.0, "a3": 7.0},
			wantID:     "a2",
			wantOutput: "out2",
		},
		{
			name:       "tie breaks alphabetically",
			scores:     map[string]float64{"a1": 8.0, "a2": 8.0, "a3": 5.0},
			wantID:     "a1", // alphabetically first among tied
			wantOutput: "out1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, output := selectWinner(tt.scores, inputs)
			if id != tt.wantID {
				t.Errorf("selectWinner() id = %q, want %q", id, tt.wantID)
			}
			if output != tt.wantOutput {
				t.Errorf("selectWinner() output = %q, want %q", output, tt.wantOutput)
			}
		})
	}
}

func TestPeerMatrixSingleInput(t *testing.T) {
	agg := &PeerMatrixAggregator{}

	t.Run("empty inputs returns error", func(t *testing.T) {
		_, err := agg.Aggregate(context.Background(), nil, nil, nil, nil)
		if err == nil {
			t.Fatal("expected error for empty inputs")
		}
	})

	t.Run("single input returns that input with score 10", func(t *testing.T) {
		inputs := []AgentOutput{{AgentID: "solo", Output: "answer", Model: "m1"}}
		result, err := agg.Aggregate(context.Background(), inputs, nil, nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Output != "answer" {
			t.Errorf("output = %q, want %q", result.Output, "answer")
		}
		if result.Winner != "solo" {
			t.Errorf("winner = %q, want %q", result.Winner, "solo")
		}
		if score, ok := result.Scores["solo"]; !ok || score != 10.0 {
			t.Errorf("score = %v, want 10.0", result.Scores)
		}
	})
}

func TestPeerMatrixParsePeerRubric(t *testing.T) {
	tests := []struct {
		name      string
		data      interface{}
		wantCount int
	}{
		{
			name:      "nil data",
			data:      nil,
			wantCount: 0,
		},
		{
			name:      "typed rubric slice",
			data:      []RubricCriterion{{Name: "A", Weight: 0.5}, {Name: "B", Weight: 0.5}},
			wantCount: 2,
		},
		{
			name: "interface slice from JSON",
			data: []interface{}{
				map[string]interface{}{"name": "Logic", "weight": 0.6, "description": "Is it logical?"},
				map[string]interface{}{"name": "Clarity", "weight": 0.4, "description": "Is it clear?"},
			},
			wantCount: 2,
		},
		{
			name: "interface slice with empty name is skipped",
			data: []interface{}{
				map[string]interface{}{"name": "", "weight": 0.5},
				map[string]interface{}{"name": "Valid", "weight": 0.5},
			},
			wantCount: 1,
		},
		{
			name:      "unsupported type",
			data:      "not a rubric",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rubric := parsePeerRubric(tt.data)
			if len(rubric) != tt.wantCount {
				t.Errorf("parsePeerRubric() returned %d criteria, want %d", len(rubric), tt.wantCount)
			}
		})
	}
}

func TestPeerMatrixParsePeerMatrixConfig(t *testing.T) {
	validConfig := map[string]interface{}{
		"eval_system_prompt": "You are an evaluator",
		"eval_prompt":        "Score this: {{response}}",
		"normalization":      "none",
		"max_parallel":       float64(4),
		"temperature":        float64(0.3),
		"max_tokens":         float64(1024),
		"rubric": []interface{}{
			map[string]interface{}{"name": "Logic", "weight": 0.5, "description": "desc"},
		},
	}

	t.Run("valid config", func(t *testing.T) {
		cfg, err := parsePeerMatrixConfig(validConfig)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.MaxParallel != 4 {
			t.Errorf("MaxParallel = %d, want 4", cfg.MaxParallel)
		}
		if len(cfg.Rubric) != 1 {
			t.Errorf("Rubric count = %d, want 1", len(cfg.Rubric))
		}
	})

	t.Run("nil config returns error", func(t *testing.T) {
		_, err := parsePeerMatrixConfig(nil)
		if err == nil {
			t.Fatal("expected error for nil config")
		}
	})

	t.Run("missing eval_system_prompt", func(t *testing.T) {
		cfg := map[string]interface{}{
			"eval_prompt":   "prompt",
			"normalization": "none",
			"max_parallel":  float64(4),
			"temperature":   float64(0.3),
			"max_tokens":    float64(1024),
			"rubric":        []interface{}{map[string]interface{}{"name": "A", "weight": 1.0}},
		}
		_, err := parsePeerMatrixConfig(cfg)
		if err == nil {
			t.Fatal("expected error for missing eval_system_prompt")
		}
	})

	t.Run("missing rubric", func(t *testing.T) {
		cfg := map[string]interface{}{
			"eval_system_prompt": "sys",
			"eval_prompt":        "prompt",
			"normalization":      "none",
			"max_parallel":       float64(4),
			"temperature":        float64(0.3),
			"max_tokens":         float64(1024),
		}
		_, err := parsePeerMatrixConfig(cfg)
		if err == nil {
			t.Fatal("expected error for missing rubric")
		}
	})

	t.Run("zero max_parallel", func(t *testing.T) {
		cfg := map[string]interface{}{
			"eval_system_prompt": "sys",
			"eval_prompt":        "prompt",
			"normalization":      "none",
			"max_parallel":       float64(0),
			"temperature":        float64(0.3),
			"max_tokens":         float64(1024),
			"rubric":             []interface{}{map[string]interface{}{"name": "A", "weight": 1.0}},
		}
		_, err := parsePeerMatrixConfig(cfg)
		if err == nil {
			t.Fatal("expected error for zero max_parallel")
		}
	})
}

func TestPeerMatrixEnsurePeerReviewerModels(t *testing.T) {
	tests := []struct {
		name    string
		tasks   []evalTask
		wantErr bool
	}{
		{
			name:    "all have models",
			tasks:   []evalTask{{ReviewerID: "a", ReviewerModel: "m1"}, {ReviewerID: "b", ReviewerModel: "m2"}},
			wantErr: false,
		},
		{
			name:    "one missing model",
			tasks:   []evalTask{{ReviewerID: "a", ReviewerModel: "m1"}, {ReviewerID: "b", ReviewerModel: ""}},
			wantErr: true,
		},
		{
			name:    "empty tasks",
			tasks:   nil,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ensurePeerReviewerModels(tt.tasks)
			if (err != nil) != tt.wantErr {
				t.Errorf("ensurePeerReviewerModels() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
