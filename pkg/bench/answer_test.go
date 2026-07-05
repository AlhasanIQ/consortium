package bench

import "testing"

func TestParseAnswerLabel(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		parseOK bool
	}{
		{name: "single letter", input: "B", want: "B", parseOK: true},
		{name: "single letter with spaces", input: "  c  ", want: "C", parseOK: true},
		{name: "answer prefix", input: "Answer: D", want: "D", parseOK: true},
		{name: "final answer", input: "Final answer - a", want: "A", parseOK: true},
		{name: "answer is", input: "The answer is c", want: "C", parseOK: true},
		{name: "leading label", input: "E) option text", want: "E", parseOK: true},
		{name: "sentence fallback", input: "I think maybe B", want: "", parseOK: false},
		{name: "empty", input: "", want: "", parseOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseAnswerLabel(tt.input)
			if ok != tt.parseOK {
				t.Fatalf("expected parse_ok=%v, got %v", tt.parseOK, ok)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestLabelIndexRoundTrip(t *testing.T) {
	for i := 0; i < 10; i++ {
		label := LabelForIndex(i)
		got, ok := IndexFromLabel(label)
		if !ok {
			t.Fatalf("expected valid label %q", label)
		}
		if got != i {
			t.Fatalf("expected index %d, got %d", i, got)
		}
	}
}

func TestParseAnswerLabelForChoices(t *testing.T) {
	if got, ok := ParseAnswerLabelForChoices("I am not sure", 4); ok || got != "" {
		t.Fatalf("expected parse failure for out-of-range letter, got %q (ok=%v)", got, ok)
	}

	if got, ok := ParseAnswerLabelForChoices("Final answer: C", 4); !ok || got != "C" {
		t.Fatalf("expected C, got %q (ok=%v)", got, ok)
	}
}

func TestNormalizeBenchmarkAnswerForChoices(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		choiceCount int
		want        string
		ok          bool
	}{
		{name: "trim and uppercase", raw: "  b  ", choiceCount: 4, want: "B", ok: true},
		{name: "single exact letter", raw: "D", choiceCount: 4, want: "D", ok: true},
		{name: "empty", raw: "", choiceCount: 4, want: "", ok: false},
		{name: "whitespace only", raw: "  ", choiceCount: 4, want: "", ok: false},
		{name: "out of range", raw: "H", choiceCount: 4, want: "", ok: false},
		{name: "multi letter token", raw: "AB", choiceCount: 4, want: "", ok: false},
		{name: "phrase is invalid", raw: "Final answer: C", choiceCount: 4, want: "", ok: false},
		{name: "punctuation is invalid", raw: "A.", choiceCount: 4, want: "", ok: false},
		{name: "choiceCount disabled allows any letter", raw: "z", choiceCount: 0, want: "Z", ok: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := NormalizeBenchmarkAnswerForChoices(tt.raw, tt.choiceCount)
			if ok != tt.ok {
				t.Fatalf("expected ok=%v, got %v", tt.ok, ok)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}
