package failureinsights

import "testing"

func TestNormalizeWrongAnswerCategory(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"already normalized", "all_steps_wrong", "all_steps_wrong"},
		{"uppercase", "ALL_STEPS_WRONG", "all_steps_wrong"},
		{"mixed case", "Some_Right_Child_Wrong", "some_right_child_wrong"},
		{"leading/trailing whitespace", "  all_steps_wrong  ", "all_steps_wrong"},
		{"whitespace and case", "  ALL_STEPS_WRONG  ", "all_steps_wrong"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeWrongAnswerCategory(tt.in)
			if got != tt.want {
				t.Errorf("NormalizeWrongAnswerCategory(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestWrongAnswerCategoryExplanation(t *testing.T) {
	tests := []struct {
		name     string
		category string
		wantSub  string // substring that must appear in the explanation
	}{
		{"all steps wrong", WrongAnswerCategoryAllStepsWrong, "All agents across child and parent"},
		{"some right child wrong", WrongAnswerCategorySomeRightChildWrong, "Some child agents were correct"},
		{"all right child wrong", WrongAnswerCategoryAllRightChildWrong, "All child agents were correct"},
		{"child right parent wrong", WrongAnswerCategoryChildRightParentWrong, "Child workflow returned the correct answer"},
		{"unclassified", WrongAnswerCategoryUnclassified, "does not match a known"},
		{"unknown category", "something_totally_new", "does not match a known"},
		{"empty", "", "does not match a known"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WrongAnswerCategoryExplanation(tt.category)
			if got == "" {
				t.Error("WrongAnswerCategoryExplanation returned empty string")
			}
			if len(tt.wantSub) > 0 && !contains(got, tt.wantSub) {
				t.Errorf("WrongAnswerCategoryExplanation(%q) = %q, want substring %q", tt.category, got, tt.wantSub)
			}
		})
	}
}

func TestWrongAnswerCategoryExplanationNormalization(t *testing.T) {
	// Verify that explanation lookup normalizes the input (uppercase + whitespace).
	base := WrongAnswerCategoryExplanation(WrongAnswerCategoryAllStepsWrong)
	got := WrongAnswerCategoryExplanation("  ALL_STEPS_WRONG  ")
	if got != base {
		t.Errorf("explanation not normalized: got %q, want %q", got, base)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
