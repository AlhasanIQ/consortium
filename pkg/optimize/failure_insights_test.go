package optimize

import (
	"strings"
	"testing"

	"github.com/alhasaniq/consortium/pkg/failureinsights"
)

func TestFailureCaseCategory(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "known category passes through normalized",
			input: "ALL_STEPS_WRONG",
			want:  failureinsights.WrongAnswerCategoryAllStepsWrong,
		},
		{
			name:  "known category with whitespace",
			input: "  some_right_child_wrong  ",
			want:  failureinsights.WrongAnswerCategorySomeRightChildWrong,
		},
		{
			name:  "all_right_child_wrong",
			input: "ALL_RIGHT_CHILD_WRONG",
			want:  failureinsights.WrongAnswerCategoryAllRightChildWrong,
		},
		{
			name:  "child_right_parent_wrong",
			input: "child_right_parent_wrong",
			want:  failureinsights.WrongAnswerCategoryChildRightParentWrong,
		},
		{
			name:  "empty string returns unclassified",
			input: "",
			want:  failureinsights.WrongAnswerCategoryUnclassified,
		},
		{
			name:  "unknown non-empty category passes through normalized (not remapped)",
			input: "MYSTERY_ERROR",
			want:  "mystery_error",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := failureCaseCategory(tc.input)
			if got != tc.want {
				t.Errorf("failureCaseCategory(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestFailureCaseDiagnosis(t *testing.T) {
	tests := []struct {
		name            string
		failure         FailureCase
		wantContains    []string
		wantNotContains []string
	}{
		{
			name: "no agents no flag",
			failure: FailureCase{
				Category:     failureinsights.WrongAnswerCategoryAllStepsWrong,
				AgentAnswers: nil,
				Flagged:      false,
			},
			wantContains: []string{"All agents across child and parent workflows"},
		},
		{
			name: "agents with parse ok and correct count",
			failure: FailureCase{
				Category: failureinsights.WrongAnswerCategoryAllStepsWrong,
				AgentAnswers: []FailureAgentAnswer{
					{ParseOK: true, Correct: true},
					{ParseOK: true, Correct: false},
					{ParseOK: false, Correct: false},
				},
				Flagged: false,
			},
			// 2 out of 3 parsed, 1 correct
			wantContains: []string{"2/3 parsed", "1 correct"},
		},
		{
			name: "flagged item with reason",
			failure: FailureCase{
				Category:   failureinsights.WrongAnswerCategoryUnclassified,
				Flagged:    true,
				FlagReason: "ambiguous wording",
			},
			wantContains: []string{"Dataset item is flagged", "ambiguous wording"},
		},
		{
			name: "flagged item without reason",
			failure: FailureCase{
				Category:   failureinsights.WrongAnswerCategoryUnclassified,
				Flagged:    true,
				FlagReason: "",
			},
			wantContains:    []string{"Dataset item is flagged"},
			wantNotContains: []string{"flagged:"},
		},
		{
			name: "no parsed agents omits vote line",
			failure: FailureCase{
				Category: failureinsights.WrongAnswerCategorySomeRightChildWrong,
				AgentAnswers: []FailureAgentAnswer{
					{ParseOK: false, Correct: false},
				},
				Flagged: false,
			},
			wantNotContains: []string{"Agent votes"},
		},
		{
			name: "unknown category uses generic fallback",
			failure: FailureCase{
				Category: "mystery_error",
			},
			wantContains: []string{"does not match a known benchmark pipeline category"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := failureCaseDiagnosis(tc.failure)
			for _, want := range tc.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("failureCaseDiagnosis() = %q, want it to contain %q", got, want)
				}
			}
			for _, notWant := range tc.wantNotContains {
				if strings.Contains(got, notWant) {
					t.Errorf("failureCaseDiagnosis() = %q, should NOT contain %q", got, notWant)
				}
			}
		})
	}
}

func TestSummarizeAgentAnswers(t *testing.T) {
	tests := []struct {
		name     string
		answers  []FailureAgentAnswer
		maxItems int
		want     string
	}{
		{
			name:     "empty answers returns empty string",
			answers:  nil,
			maxItems: 5,
			want:     "",
		},
		{
			name:     "maxItems zero returns empty string",
			answers:  []FailureAgentAnswer{{Model: "m", Answer: "A", ParseOK: true, Correct: true}},
			maxItems: 0,
			want:     "",
		},
		{
			name: "correct answer uses Y mark",
			answers: []FailureAgentAnswer{
				{Model: "gpt-4", Answer: "Paris", ParseOK: true, Correct: true},
			},
			maxItems: 5,
			want:     "gpt-4:Paris:Y",
		},
		{
			name: "wrong but parsed uses N mark",
			answers: []FailureAgentAnswer{
				{Model: "gpt-4", Answer: "London", ParseOK: true, Correct: false},
			},
			maxItems: 5,
			want:     "gpt-4:London:N",
		},
		{
			name: "parse failed uses X mark",
			answers: []FailureAgentAnswer{
				{Model: "claude", Answer: "???", ParseOK: false, Correct: false},
			},
			maxItems: 5,
			want:     "claude:???:X",
		},
		{
			name: "empty answer uses dash",
			answers: []FailureAgentAnswer{
				{Model: "model-x", Answer: "", ParseOK: true, Correct: false},
			},
			maxItems: 5,
			want:     "model-x:-:N",
		},
		{
			name: "empty model falls back to node_id",
			answers: []FailureAgentAnswer{
				{NodeID: "node-1", Model: "", Answer: "B", ParseOK: true, Correct: true},
			},
			maxItems: 5,
			want:     "node-1:B:Y",
		},
		{
			name: "empty model and node_id uses agent-N label",
			answers: []FailureAgentAnswer{
				{NodeID: "", Model: "", Answer: "C", ParseOK: true, Correct: false},
			},
			maxItems: 5,
			want:     "agent-1:C:N",
		},
		{
			name: "multiple answers joined with pipe separator",
			answers: []FailureAgentAnswer{
				{Model: "a", Answer: "X", ParseOK: true, Correct: true},
				{Model: "b", Answer: "Y", ParseOK: true, Correct: false},
			},
			maxItems: 5,
			want:     "a:X:Y | b:Y:N",
		},
		{
			name: "maxItems limits output",
			answers: []FailureAgentAnswer{
				{Model: "a", Answer: "X", ParseOK: true, Correct: true},
				{Model: "b", Answer: "Y", ParseOK: true, Correct: false},
				{Model: "c", Answer: "Z", ParseOK: false, Correct: false},
			},
			maxItems: 2,
			want:     "a:X:Y | b:Y:N",
		},
		{
			name: "model label whitespace is trimmed",
			answers: []FailureAgentAnswer{
				{Model: "  trimmed-model  ", Answer: "A", ParseOK: true, Correct: true},
			},
			maxItems: 5,
			want:     "trimmed-model:A:Y",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := summarizeAgentAnswers(tc.answers, tc.maxItems)
			if got != tc.want {
				t.Errorf("summarizeAgentAnswers() = %q, want %q", got, tc.want)
			}
		})
	}
}
