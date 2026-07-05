package optimize

import (
	"math/rand"
	"strings"
	"testing"
)

func TestBootstrapDemoPools(t *testing.T) {
	rng := rand.New(rand.NewSource(42)) //nolint:gosec

	t.Run("empty successes returns nil", func(t *testing.T) {
		got := bootstrapDemoPools(nil, 3, 4, rng)
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("numSets zero returns nil", func(t *testing.T) {
		successes := []SuccessExample{{Question: "Q", CorrectAnswer: "A"}}
		got := bootstrapDemoPools(successes, 0, 4, rng)
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("maxPerSet zero returns nil", func(t *testing.T) {
		successes := []SuccessExample{{Question: "Q", CorrectAnswer: "A"}}
		got := bootstrapDemoPools(successes, 3, 0, rng)
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("items missing question or answer are filtered out", func(t *testing.T) {
		successes := []SuccessExample{
			{Question: "", CorrectAnswer: "A"},   // no question
			{Question: "Q2", CorrectAnswer: ""},  // no answer
			{Question: "  ", CorrectAnswer: "A"}, // whitespace-only question
		}
		got := bootstrapDemoPools(successes, 2, 4, rng)
		if got != nil {
			t.Errorf("expected nil when all items are ineligible, got %v", got)
		}
	})

	t.Run("returns correct number of sets", func(t *testing.T) {
		successes := makeSuccessExamples(10)
		got := bootstrapDemoPools(successes, 3, 4, rng)
		if len(got) != 3 {
			t.Errorf("expected 3 sets, got %d", len(got))
		}
	})

	t.Run("each set is capped at maxPerSet", func(t *testing.T) {
		successes := makeSuccessExamples(20)
		got := bootstrapDemoPools(successes, 5, 4, rng)
		for i, set := range got {
			if len(set) > 4 {
				t.Errorf("set[%d] has %d items, want <= 4", i, len(set))
			}
		}
	})

	t.Run("fewer eligible items than maxPerSet caps at eligible count", func(t *testing.T) {
		successes := makeSuccessExamples(3)
		got := bootstrapDemoPools(successes, 2, 10, rng)
		for i, set := range got {
			if len(set) != 3 {
				t.Errorf("set[%d] has %d items, want 3", i, len(set))
			}
		}
	})

	t.Run("demos have question and answer populated", func(t *testing.T) {
		successes := makeSuccessExamples(5)
		got := bootstrapDemoPools(successes, 1, 5, rng)
		if len(got) == 0 {
			t.Fatal("expected at least one set")
		}
		for i, demo := range got[0] {
			if demo.Question == "" {
				t.Errorf("demo[%d].Question is empty", i)
			}
			if demo.Answer == "" {
				t.Errorf("demo[%d].Answer is empty", i)
			}
		}
	})

	t.Run("nil rng does not panic", func(t *testing.T) {
		successes := makeSuccessExamples(5)
		// Should not panic — falls back to seeded internal RNG.
		got := bootstrapDemoPools(successes, 2, 3, nil)
		if len(got) != 2 {
			t.Errorf("expected 2 sets, got %d", len(got))
		}
	})

	t.Run("question and answer whitespace is trimmed", func(t *testing.T) {
		successes := []SuccessExample{
			{Question: "  trimmed Q  ", CorrectAnswer: "  trimmed A  "},
		}
		got := bootstrapDemoPools(successes, 1, 1, rng)
		if len(got) == 0 || len(got[0]) == 0 {
			t.Fatal("expected one demo")
		}
		demo := got[0][0]
		if demo.Question != "trimmed Q" {
			t.Errorf("Question = %q, want %q", demo.Question, "trimmed Q")
		}
		if demo.Answer != "trimmed A" {
			t.Errorf("Answer = %q, want %q", demo.Answer, "trimmed A")
		}
	})
}

func TestFormatDemoSection(t *testing.T) {
	tests := []struct {
		name            string
		demos           []DemoExample
		wantEmpty       bool
		wantContains    []string
		wantNotContains []string
	}{
		{
			name:      "empty demos returns empty string",
			demos:     nil,
			wantEmpty: true,
		},
		{
			name:      "empty slice returns empty string",
			demos:     []DemoExample{},
			wantEmpty: true,
		},
		{
			name: "header includes demo count",
			demos: []DemoExample{
				{Question: "What is 2+2?", Answer: "4"},
				{Question: "Capital of France?", Answer: "Paris"},
			},
			wantContains: []string{"## Few-Shot Examples (2)", "The following are examples"},
		},
		{
			name: "each demo is numbered from 1",
			demos: []DemoExample{
				{Question: "Q1", Answer: "A1"},
				{Question: "Q2", Answer: "A2"},
			},
			wantContains: []string{"Example 1:", "Example 2:"},
		},
		{
			name: "question and answer are included",
			demos: []DemoExample{
				{Question: "What is Go?", Answer: "A programming language"},
			},
			wantContains: []string{"Q: What is Go?", "A: A programming language"},
		},
		{
			name: "long question is truncated",
			demos: []DemoExample{
				{Question: strings.Repeat("x", 500), Answer: "A"},
			},
			// trimForPrompt(question, 400) will truncate to 400 chars
			wantNotContains: []string{strings.Repeat("x", 500)},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatDemoSection(tc.demos)
			if tc.wantEmpty {
				if got != "" {
					t.Errorf("expected empty string, got %q", got)
				}
				return
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("formatDemoSection() = %q\nwant to contain %q", got, want)
				}
			}
			for _, notWant := range tc.wantNotContains {
				if strings.Contains(got, notWant) {
					t.Errorf("formatDemoSection() should NOT contain %q", notWant)
				}
			}
		})
	}
}

func TestAugmentCandidatesWithDemos(t *testing.T) {
	makeCandidates := func(n int) []ClaudePromptCandidate {
		c := make([]ClaudePromptCandidate, n)
		for i := range c {
			c[i] = ClaudePromptCandidate{
				RevisedPrompt:  strings.Repeat("instruction", i+1),
				Reasoning:      "reason",
				ChangesSummary: "summary",
			}
		}
		return c
	}

	demos1 := []DemoExample{{Question: "Q1", Answer: "A1"}}
	demos2 := []DemoExample{{Question: "Q2", Answer: "A2"}}

	t.Run("empty candidates returns candidates unchanged", func(t *testing.T) {
		got := augmentCandidatesWithDemos(nil, [][]DemoExample{demos1}, 10)
		if got != nil {
			t.Errorf("expected nil candidates returned as-is, got %v", got)
		}
	})

	t.Run("empty demo pools returns candidates unchanged", func(t *testing.T) {
		candidates := makeCandidates(2)
		got := augmentCandidatesWithDemos(candidates, nil, 10)
		if len(got) != 2 {
			t.Errorf("expected 2 candidates, got %d", len(got))
		}
	})

	t.Run("original candidates are preserved at front", func(t *testing.T) {
		candidates := makeCandidates(2)
		got := augmentCandidatesWithDemos(candidates, [][]DemoExample{demos1}, 10)
		if len(got) < 2 {
			t.Fatalf("expected at least 2 results, got %d", len(got))
		}
		for i := 0; i < 2; i++ {
			if got[i].RevisedPrompt != candidates[i].RevisedPrompt {
				t.Errorf("original candidate[%d] was altered", i)
			}
		}
	})

	t.Run("augmented candidate appended after originals", func(t *testing.T) {
		candidates := makeCandidates(2)
		got := augmentCandidatesWithDemos(candidates, [][]DemoExample{demos1}, 10)
		// 2 original + 1 augmented
		if len(got) != 3 {
			t.Errorf("expected 3 candidates, got %d", len(got))
		}
		augmented := got[2]
		if !strings.HasPrefix(augmented.Reasoning, "demo-augmented:") {
			t.Errorf("augmented reasoning = %q, want prefix demo-augmented:", augmented.Reasoning)
		}
		if !strings.Contains(augmented.ChangesSummary, "few-shot demos") {
			t.Errorf("augmented changes summary = %q, want 'few-shot demos'", augmented.ChangesSummary)
		}
	})

	t.Run("augmented prompt includes demo section", func(t *testing.T) {
		candidates := makeCandidates(2)
		got := augmentCandidatesWithDemos(candidates, [][]DemoExample{demos1}, 10)
		augmented := got[len(got)-1]
		if !strings.Contains(augmented.RevisedPrompt, "Few-Shot Examples") {
			t.Errorf("augmented prompt does not contain demo section: %q", augmented.RevisedPrompt)
		}
	})

	t.Run("limit caps total output", func(t *testing.T) {
		candidates := makeCandidates(3)
		pools := [][]DemoExample{demos1, demos2}
		got := augmentCandidatesWithDemos(candidates, pools, 4)
		if len(got) > 4 {
			t.Errorf("expected <= 4 candidates, got %d", len(got))
		}
	})

	t.Run("zero limit means no cap", func(t *testing.T) {
		candidates := makeCandidates(2)
		pools := [][]DemoExample{demos1, demos2}
		got := augmentCandidatesWithDemos(candidates, pools, 0)
		// 2 originals + up to 2 augmented
		if len(got) < 2 {
			t.Errorf("expected at least 2 results, got %d", len(got))
		}
	})

	t.Run("single candidate uses index 0 as base", func(t *testing.T) {
		candidates := makeCandidates(1)
		got := augmentCandidatesWithDemos(candidates, [][]DemoExample{demos1}, 10)
		// 1 original + 1 augmented
		if len(got) != 2 {
			t.Errorf("expected 2 results, got %d", len(got))
		}
		// augmented should be based on the only candidate (index 0)
		if !strings.HasPrefix(got[1].RevisedPrompt, candidates[0].RevisedPrompt) {
			t.Errorf("augmented not based on candidate[0]")
		}
	})
}

// makeSuccessExamples generates n SuccessExample entries with non-empty Q+A.
func makeSuccessExamples(n int) []SuccessExample {
	examples := make([]SuccessExample, n)
	for i := range examples {
		examples[i] = SuccessExample{
			ItemID:        strings.Repeat("i", i+1),
			Question:      strings.Repeat("Q", i+1),
			CorrectAnswer: strings.Repeat("A", i+1),
			Predicted:     strings.Repeat("A", i+1),
		}
	}
	return examples
}
