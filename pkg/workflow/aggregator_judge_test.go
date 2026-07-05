package workflow

import (
	"context"
	"testing"
)

// Tests in this file cover parseWinner and helper functions NOT already in aggregator_test.go.
// The LLM-calling paths and TestContainsIDAsWord are already tested there.

func TestParseWinner_ExactMatchPriority(t *testing.T) {
	labels := []string{"A", "B", "C"}

	// Exact match on WINNER: line should take priority
	tests := []struct {
		name     string
		response string
		want     string
	}{
		{
			name:     "exact label after WINNER prefix",
			response: "Analysis done.\nWINNER: B",
			want:     "B",
		},
		{
			name:     "mixed case winner prefix",
			response: "Winner: C",
			want:     "C",
		},
		{
			name:     "WINNER with padding",
			response: "WINNER:   A  ",
			want:     "A",
		},
		{
			name:     "winner in sentence (pass 2 fallback)",
			response: "Based on quality, the winner here is B.",
			want:     "B",
		},
		{
			name:     "no valid label at all",
			response: "WINNER: Z\nNo winner found.",
			want:     "",
		},
		{
			name:     "empty response",
			response: "",
			want:     "",
		},
		{
			name:     "WINNER Response A wins (word boundary)",
			response: "WINNER: Response A wins",
			want:     "A",
		},
		{
			name:     "multiline with WINNER on last line",
			response: "Response A is good.\nResponse B is better.\nWINNER: B",
			want:     "B",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseWinner(tt.response, labels)
			if got != tt.want {
				t.Errorf("parseWinner() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseWinner_MultiCharLabels(t *testing.T) {
	// Test with multi-character agent IDs (not just single letters)
	labels := []string{"model-a", "model-b", "model-c"}

	got := parseWinner("WINNER: model-b", labels)
	if got != "model-b" {
		t.Errorf("got %q, want %q", got, "model-b")
	}

	got = parseWinner("The winner is model-c because it's better.", labels)
	if got != "model-c" {
		t.Errorf("got %q, want %q", got, "model-c")
	}
}

func TestParseWinner_CaseInsensitivePrefix(t *testing.T) {
	labels := []string{"A", "B"}

	cases := []string{
		"WINNER: A",
		"Winner: A",
		"winner: A",
		"WINNER:A",
	}
	for _, c := range cases {
		got := parseWinner(c, labels)
		if got != "A" {
			t.Errorf("parseWinner(%q) = %q, want %q", c, got, "A")
		}
	}
}

func TestParseWinner_Pass2DoesNotMatchWithoutWinnerKeyword(t *testing.T) {
	labels := []string{"A", "B"}

	// Text mentions "A" but not "winner" -- should NOT match
	got := parseWinner("Response A is clearly better than B.", labels)
	if got != "" {
		t.Errorf("expected empty, got %q (pass 2 should require 'winner' keyword)", got)
	}
}

func TestJudgeAggregator_NameMethod(t *testing.T) {
	j := &JudgeAggregator{}
	if j.Name() != AggMethodJudge {
		t.Errorf("Name() = %q, want %q", j.Name(), AggMethodJudge)
	}
}

func TestJudgeAggregator_EmptyAndSingleInputs(t *testing.T) {
	j := &JudgeAggregator{}

	// Empty
	_, err := j.Aggregate(context.Background(), []AgentOutput{}, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for empty inputs")
	}

	// Single input: should return directly without needing LLM
	result, err := j.Aggregate(context.Background(), []AgentOutput{
		{AgentID: "solo", Output: "my answer"},
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "my answer" {
		t.Errorf("Output = %q, want %q", result.Output, "my answer")
	}
	if result.Winner != "solo" {
		t.Errorf("Winner = %q, want %q", result.Winner, "solo")
	}
	if result.Method != AggMethodJudge {
		t.Errorf("Method = %q, want %q", result.Method, AggMethodJudge)
	}
}

func TestJudgeAggregator_MissingConfigFields(t *testing.T) {
	j := &JudgeAggregator{}
	inputs := []AgentOutput{
		{AgentID: "a1", Output: "one"},
		{AgentID: "a2", Output: "two"},
	}

	// Missing judge_model
	_, err := j.Aggregate(context.Background(), inputs, map[string]interface{}{}, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing judge_model")
	}

	// Has judge_model but missing system_prompt
	_, err = j.Aggregate(context.Background(), inputs, map[string]interface{}{
		"judge_model": "test",
	}, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing system_prompt")
	}

	// Has model+system_prompt but missing prompt
	_, err = j.Aggregate(context.Background(), inputs, map[string]interface{}{
		"judge_model":   "test",
		"system_prompt": "judge",
	}, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing prompt")
	}

	// Has model+system_prompt+prompt but missing temperature
	_, err = j.Aggregate(context.Background(), inputs, map[string]interface{}{
		"judge_model":   "test",
		"system_prompt": "judge",
		"prompt":        "pick one",
	}, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing temperature")
	}

	// Has everything except max_tokens
	_, err = j.Aggregate(context.Background(), inputs, map[string]interface{}{
		"judge_model":   "test",
		"system_prompt": "judge",
		"prompt":        "pick one",
		"temperature":   0.0,
	}, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing max_tokens")
	}
}

func TestDefaultJudgePromptConstants(t *testing.T) {
	if DefaultJudgeSystemPrompt == "" {
		t.Error("DefaultJudgeSystemPrompt should not be empty")
	}
	if DefaultJudgePrompt == "" {
		t.Error("DefaultJudgePrompt should not be empty")
	}
}
