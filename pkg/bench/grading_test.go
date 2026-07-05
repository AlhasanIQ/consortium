package bench

import "testing"

func TestIsCorrectPrediction_MCQABenchmark(t *testing.T) {
	tests := []struct {
		name      string
		item      DatasetItem
		predicted string
		parseOK   bool
		want      bool
	}{
		{
			name: "correct letter exact match",
			item: DatasetItem{
				Benchmark:   "global-mmlu",
				AnswerLabel: "B",
				Choices:     []string{"opt1", "opt2", "opt3", "opt4"},
			},
			predicted: "B",
			parseOK:   true,
			want:      true,
		},
		{
			name: "correct letter case insensitive",
			item: DatasetItem{
				Benchmark:   "global-mmlu",
				AnswerLabel: "B",
				Choices:     []string{"opt1", "opt2", "opt3", "opt4"},
			},
			predicted: "b",
			parseOK:   true,
			want:      true,
		},
		{
			name: "correct letter with whitespace",
			item: DatasetItem{
				Benchmark:   "global-mmlu",
				AnswerLabel: "C",
				Choices:     []string{"opt1", "opt2", "opt3", "opt4"},
			},
			predicted: "  C  ",
			parseOK:   true,
			want:      true,
		},
		{
			name: "wrong letter",
			item: DatasetItem{
				Benchmark:   "global-mmlu",
				AnswerLabel: "A",
				Choices:     []string{"opt1", "opt2", "opt3", "opt4"},
			},
			predicted: "B",
			parseOK:   true,
			want:      false,
		},
		{
			name: "parse not ok always returns false",
			item: DatasetItem{
				Benchmark:   "global-mmlu",
				AnswerLabel: "A",
				Choices:     []string{"opt1", "opt2", "opt3", "opt4"},
			},
			predicted: "A",
			parseOK:   false,
			want:      false,
		},
		{
			name: "empty predicted with parse ok false",
			item: DatasetItem{
				Benchmark:   "global-mmlu",
				AnswerLabel: "A",
				Choices:     []string{"opt1", "opt2", "opt3", "opt4"},
			},
			predicted: "",
			parseOK:   false,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsCorrectPrediction(tt.item, tt.predicted, tt.parseOK)
			if got != tt.want {
				t.Fatalf("IsCorrectPrediction() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsCorrectPrediction_MathBenchmark(t *testing.T) {
	tests := []struct {
		name      string
		item      DatasetItem
		predicted string
		parseOK   bool
		want      bool
	}{
		{
			name: "exact integer match",
			item: DatasetItem{
				Benchmark:  "math-500",
				GoldAnswer: "42",
			},
			predicted: "42",
			parseOK:   true,
			want:      true,
		},
		{
			name: "uses gold_answer field for math",
			item: DatasetItem{
				Benchmark:   "math-500",
				GoldAnswer:  "7",
				AnswerLabel: "7",
			},
			predicted: "7",
			parseOK:   true,
			want:      true,
		},
		{
			name: "wrong math answer",
			item: DatasetItem{
				Benchmark:  "math-500",
				GoldAnswer: "42",
			},
			predicted: "43",
			parseOK:   true,
			want:      false,
		},
		{
			name: "math parse not ok returns false",
			item: DatasetItem{
				Benchmark:  "math-500",
				GoldAnswer: "42",
			},
			predicted: "42",
			parseOK:   false,
			want:      false,
		},
		{
			name: "falls back to answer_label when gold_answer empty",
			item: DatasetItem{
				Benchmark:   "math-500",
				GoldAnswer:  "",
				AnswerLabel: "15",
			},
			predicted: "15",
			parseOK:   true,
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsCorrectPrediction(tt.item, tt.predicted, tt.parseOK)
			if got != tt.want {
				t.Fatalf("IsCorrectPrediction() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsCorrectPrediction_UnknownBenchmarkFallback(t *testing.T) {
	// Unknown benchmark with non-empty GoldAnswer and no Choices uses MathAnswer format
	item := DatasetItem{
		Benchmark:  "some-unknown-benchmark",
		GoldAnswer: "Paris",
		Choices:    nil,
	}
	if !IsCorrectPrediction(item, "Paris", true) {
		t.Fatal("expected true for exact gold_answer match on unknown benchmark with no choices")
	}
	if IsCorrectPrediction(item, "london", true) {
		t.Fatal("expected false for non-matching answer on unknown benchmark")
	}
}

func TestIsCorrectPrediction_UnknownBenchmarkMCQAFallback(t *testing.T) {
	// Unknown benchmark with choices falls back to MCQA (case-insensitive label match)
	item := DatasetItem{
		Benchmark:   "some-other-unknown",
		AnswerLabel: "A",
		Choices:     []string{"opt1", "opt2"},
		GoldAnswer:  "",
	}
	if !IsCorrectPrediction(item, "A", true) {
		t.Fatal("expected true for matching label on unknown MCQA benchmark")
	}
	if IsCorrectPrediction(item, "B", true) {
		t.Fatal("expected false for wrong label on unknown MCQA benchmark")
	}
}
