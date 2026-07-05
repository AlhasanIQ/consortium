package bench

import (
	"strings"
	"testing"
)

func TestBuildQuestionPromptMCQA(t *testing.T) {
	item := DatasetItem{
		ID:        "q1",
		Benchmark: BenchmarkGlobalMMLULite,
		Question:  "2+2?",
		Choices:   []string{"1", "2", "4"},
	}
	prompt, err := BuildQuestionPrompt(item)
	if err != nil {
		t.Fatalf("BuildQuestionPrompt failed: %v", err)
	}
	if !strings.Contains(prompt, "Options:") {
		t.Fatalf("prompt missing options section: %q", prompt)
	}
	if !strings.Contains(prompt, "C. 4") {
		t.Fatalf("prompt missing labeled options: %q", prompt)
	}
}

func TestBuildQuestionPromptMath(t *testing.T) {
	item := DatasetItem{
		ID:         "m1",
		Benchmark:  BenchmarkMath500,
		Question:   "Compute 7+5",
		GoldAnswer: "12",
	}
	prompt, err := BuildQuestionPrompt(item)
	if err != nil {
		t.Fatalf("BuildQuestionPrompt failed: %v", err)
	}
	if strings.Contains(prompt, "Options:") {
		t.Fatalf("math prompt should not include options: %q", prompt)
	}
	// Math behavioral instructions live in seed inputTemplates, not the harness.
	if strings.Contains(prompt, "Final answer") {
		t.Fatalf("math prompt should not include behavioral instructions (seed owns those): %q", prompt)
	}
	if !strings.Contains(prompt, "Compute 7+5") {
		t.Fatalf("math prompt missing question text: %q", prompt)
	}
}
