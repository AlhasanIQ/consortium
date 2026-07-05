package workflow

import (
	"context"
	"testing"
	"time"
)

func TestScoringParseScores(t *testing.T) {
	tests := []struct {
		name      string
		response  string
		wantCount int
		wantKey   string
		wantScore float64
	}{
		{
			name:      "valid JSON scores object",
			response:  `{"scores": {"logical_soundness": 8, "clarity": 7}}`,
			wantCount: 2,
			wantKey:   "logical_soundness",
			wantScore: 8,
		},
		{
			name:      "scores embedded in text",
			response:  `Here is my evaluation.\n{"scores": {"accuracy": 9}}\nThat is all.`,
			wantCount: 1,
			wantKey:   "accuracy",
			wantScore: 9,
		},
		{
			name:      "fallback line-based parsing",
			response:  "Logical Soundness: 8\nClarity: 6\nEvidence Analysis: 7/10",
			wantCount: 3,
			wantKey:   "logical_soundness",
			wantScore: 8,
		},
		{
			name:      "score out of range is excluded from fallback",
			response:  "Accuracy: 15\nClarity: 7",
			wantCount: 1,
			wantKey:   "clarity",
			wantScore: 7,
		},
		{
			name:      "empty response",
			response:  "",
			wantCount: 0,
		},
		{
			name:      "no recognizable scores",
			response:  "This is just plain text with no scores.",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scores := parseScores(tt.response)
			if len(scores) != tt.wantCount {
				t.Fatalf("parseScores() returned %d scores, want %d; got %v", len(scores), tt.wantCount, scores)
			}
			if tt.wantKey != "" && tt.wantCount > 0 {
				if got, ok := scores[tt.wantKey]; !ok {
					t.Errorf("expected key %q not found in scores %v", tt.wantKey, scores)
				} else if got != tt.wantScore {
					t.Errorf("scores[%q] = %v, want %v", tt.wantKey, got, tt.wantScore)
				}
			}
		})
	}
}

func TestScoringNormalizeRubricKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Logical Soundness", "logical_soundness"},
		{"evidence_analysis", "evidence_analysis"},
		{"Evidence & Analysis", "evidence_analysis"},
		{"  Clarity  ", "clarity"},
		{"COMPLETENESS", "completeness"},
		{"multi---dash", "multi_dash"},
		{"trailing-and-leading", "trailing_and_leading"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeRubricKey(tt.input)
			if got != tt.want {
				t.Errorf("normalizeRubricKey(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestScoringCalculateWeightedScore(t *testing.T) {
	rubric := []RubricCriterion{
		{Name: "Logical Soundness", Weight: 0.4},
		{Name: "Evidence Analysis", Weight: 0.3},
		{Name: "Completeness", Weight: 0.2},
		{Name: "Clarity", Weight: 0.1},
	}

	tests := []struct {
		name        string
		scores      map[string]float64
		wantScore   float64
		wantMatched bool
	}{
		{
			name: "all criteria match",
			scores: map[string]float64{
				"logical_soundness": 8,
				"evidence_analysis": 7,
				"completeness":      6,
				"clarity":           9,
			},
			// (8*0.4 + 7*0.3 + 6*0.2 + 9*0.1) / 1.0 = 3.2+2.1+1.2+0.9 = 7.4
			wantScore:   7.4,
			wantMatched: true,
		},
		{
			name: "partial match (only 2 of 4)",
			scores: map[string]float64{
				"logical_soundness": 10,
				"clarity":           5,
			},
			// (10*0.4 + 5*0.1) / (0.4+0.1) = 4.5/0.5 = 9.0
			wantScore:   9.0,
			wantMatched: true,
		},
		{
			name:        "no criteria match",
			scores:      map[string]float64{"unrelated_key": 8},
			wantScore:   0.0,
			wantMatched: false,
		},
		{
			name:        "empty scores",
			scores:      map[string]float64{},
			wantScore:   0.0,
			wantMatched: false,
		},
		{
			name: "mixed case keys from LLM response",
			scores: map[string]float64{
				"Logical Soundness": 7,
				"Evidence Analysis": 8,
			},
			// (7*0.4 + 8*0.3) / (0.4+0.3) = 5.2/0.7 = 7.4285...
			wantScore:   5.2 / 0.7,
			wantMatched: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, matched := calculateWeightedScore(tt.scores, rubric)
			if matched != tt.wantMatched {
				t.Fatalf("calculateWeightedScore() matched = %v, want %v", matched, tt.wantMatched)
			}
			if tt.wantMatched {
				diff := score - tt.wantScore
				if diff < -0.001 || diff > 0.001 {
					t.Errorf("calculateWeightedScore() = %f, want %f", score, tt.wantScore)
				}
			}
		})
	}
}

func TestScoringParseRubric(t *testing.T) {
	tests := []struct {
		name      string
		config    map[string]interface{}
		wantCount int
	}{
		{
			name:      "no rubric key returns default",
			config:    map[string]interface{}{},
			wantCount: len(DefaultRubric),
		},
		{
			name: "rubric from interface slice",
			config: map[string]interface{}{
				"rubric": []interface{}{
					map[string]interface{}{"name": "A", "weight": 0.5, "description": "desc A"},
					map[string]interface{}{"name": "B", "weight": 0.5, "description": "desc B"},
				},
			},
			wantCount: 2,
		},
		{
			name: "typed rubric",
			config: map[string]interface{}{
				"rubric": []RubricCriterion{
					{Name: "X", Weight: 1.0, Description: "desc X"},
				},
			},
			wantCount: 1,
		},
		{
			name: "JSON string rubric skips empty names",
			config: map[string]interface{}{
				"rubric": `[{"weight":0.5,"description":"missing name"},{"name":"Valid","weight":0.5,"description":"desc"}]`,
			},
			wantCount: 1,
		},
		{
			name: "nested interface rubric wrapper",
			config: map[string]interface{}{
				"rubric": map[string]interface{}{
					"rubric": []interface{}{
						map[string]interface{}{"name": "Correctness", "weight": 0.75, "description": "right"},
						map[string]interface{}{"name": "Clarity", "weight": 0.25, "description": "clear"},
					},
				},
			},
			wantCount: 2,
		},
		{
			name: "JSON string rubric wrapper",
			config: map[string]interface{}{
				"rubric": `{"rubric":[{"name":"Correctness","weight":0.75,"description":"right"},{"name":"Clarity","weight":0.25,"description":"clear"}]}`,
			},
			wantCount: 2,
		},
		{
			name:      "invalid rubric type falls back to default",
			config:    map[string]interface{}{"rubric": "not-a-slice"},
			wantCount: len(DefaultRubric),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rubric := parseRubric(tt.config)
			if len(rubric) != tt.wantCount {
				t.Errorf("parseRubric() returned %d criteria, want %d", len(rubric), tt.wantCount)
			}
		})
	}
}

func TestScoringSingleInput(t *testing.T) {
	agg := &ScoringAggregator{}

	t.Run("empty inputs returns error", func(t *testing.T) {
		_, err := agg.Aggregate(context.Background(), nil, nil, nil, nil)
		if err == nil {
			t.Fatal("expected error for empty inputs")
		}
	})

	t.Run("single input returns that input with score 1.0", func(t *testing.T) {
		inputs := []AgentOutput{{AgentID: "solo", Output: "answer"}}
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
		if score, ok := result.Scores["solo"]; !ok || score != 1.0 {
			t.Errorf("score = %v, want 1.0", result.Scores)
		}
	})
}

func TestScoringSubcallRetryConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      map[string]interface{}
		wantMax     int
		wantBackoff int
	}{
		{
			name:        "nil config uses defaults",
			config:      nil,
			wantMax:     3,
			wantBackoff: 250,
		},
		{
			name: "custom values",
			config: map[string]interface{}{
				"subcall_retry_max_attempts": float64(5),
				"subcall_retry_backoff_ms":   float64(500),
			},
			wantMax:     5,
			wantBackoff: 500,
		},
		{
			name:        "empty config uses defaults",
			config:      map[string]interface{}{},
			wantMax:     3,
			wantBackoff: 250,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := parseScoringSubcallRetryConfig(tt.config)
			if cfg.MaxAttempts != tt.wantMax {
				t.Errorf("MaxAttempts = %d, want %d", cfg.MaxAttempts, tt.wantMax)
			}
			if cfg.BackoffMs != tt.wantBackoff {
				t.Errorf("BackoffMs = %d, want %d", cfg.BackoffMs, tt.wantBackoff)
			}
		})
	}
}

func TestScoringBackoffForAttempt(t *testing.T) {
	cfg := scoringSubcallRetryConfig{
		MaxAttempts:     3,
		BackoffMs:       100,
		BackoffMultiply: 2.0,
		MaxBackoffMs:    500,
	}

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 0},                       // attempt < 1
		{1, 100 * time.Millisecond},  // base
		{2, 200 * time.Millisecond},  // 100 * 2
		{3, 400 * time.Millisecond},  // 100 * 2 * 2
		{4, 500 * time.Millisecond},  // capped at max
		{10, 500 * time.Millisecond}, // still capped
	}

	for _, tt := range tests {
		got := cfg.backoffForAttempt(tt.attempt)
		if got != tt.want {
			t.Errorf("backoffForAttempt(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

func TestScoringShouldRetryScoringSubcallError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"timeout in message", errWithMsg("upstream timeout"), true},
		{"connection reset", errWithMsg("connection reset by peer"), true},
		{"random error", errWithMsg("something else"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldRetryScoringSubcallError(tt.err)
			if got != tt.want {
				t.Errorf("shouldRetryScoringSubcallError() = %v, want %v", got, tt.want)
			}
		})
	}
}

// errWithMsg is a simple error for testing.
type errWithMsg string

func (e errWithMsg) Error() string { return string(e) }

func TestScoringScoringSubcallError(t *testing.T) {
	inner := errWithMsg("connection failed")
	sce := &scoringSubcallError{
		AgentID:     "agent1",
		Attempts:    2,
		MaxAttempts: 3,
		Err:         inner,
	}

	if sce.Error() == "" {
		t.Error("Error() should return non-empty string")
	}

	if sce.Unwrap() != inner {
		t.Error("Unwrap() should return inner error")
	}

	meta := sce.metadata()
	if meta["scoring_subcall_agent"] != "agent1" {
		t.Errorf("metadata agent = %v, want agent1", meta["scoring_subcall_agent"])
	}

	// Test asScoringSubcallError
	found, ok := asScoringSubcallError(sce)
	if !ok || found != sce {
		t.Error("asScoringSubcallError should find the error")
	}

	_, ok = asScoringSubcallError(nil)
	if ok {
		t.Error("asScoringSubcallError(nil) should return false")
	}
}
