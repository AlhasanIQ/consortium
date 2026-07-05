package workflow

import (
	"context"
	"testing"

	"github.com/alhasaniq/consortium/pkg/providers"
)

func TestParseDynamicRubricValid(t *testing.T) {
	input := `{"rubric": [
		{"name": "Accuracy", "weight": 0.4, "description": "Factual correctness"},
		{"name": "Reasoning", "weight": 0.35, "description": "Quality of reasoning chain"},
		{"name": "Clarity", "weight": 0.25, "description": "Clear presentation"}
	]}`

	rubric, err := parseDynamicRubric(input)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(rubric) != 3 {
		t.Fatalf("Expected 3 criteria, got %d", len(rubric))
	}
	if rubric[0].Name != "Accuracy" {
		t.Errorf("Expected first criterion 'Accuracy', got %q", rubric[0].Name)
	}
	if rubric[0].Weight != 0.4 {
		t.Errorf("Expected weight 0.4, got %f", rubric[0].Weight)
	}
}

func TestParseDynamicRubricWeightNormalization(t *testing.T) {
	// Weights sum to 2.0 — should be normalized to sum to 1.0
	input := `{"rubric": [
		{"name": "A", "weight": 1.0, "description": "First"},
		{"name": "B", "weight": 1.0, "description": "Second"}
	]}`

	rubric, err := parseDynamicRubric(input)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if rubric[0].Weight != 0.5 || rubric[1].Weight != 0.5 {
		t.Errorf("Expected normalized weights 0.5/0.5, got %f/%f", rubric[0].Weight, rubric[1].Weight)
	}
}

func TestParseDynamicRubricWeightsAlreadyNormalized(t *testing.T) {
	// Weights sum to 1.0 — should not be modified
	input := `{"rubric": [
		{"name": "A", "weight": 0.6, "description": "First"},
		{"name": "B", "weight": 0.4, "description": "Second"}
	]}`

	rubric, err := parseDynamicRubric(input)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if rubric[0].Weight != 0.6 || rubric[1].Weight != 0.4 {
		t.Errorf("Expected unchanged weights 0.6/0.4, got %f/%f", rubric[0].Weight, rubric[1].Weight)
	}
}

func TestParseDynamicRubricInvalidJSON(t *testing.T) {
	_, err := parseDynamicRubric("This is not JSON at all")
	if err == nil {
		t.Fatal("Expected error for invalid input")
	}
}

func TestParseDynamicRubricTooFewCriteria(t *testing.T) {
	input := `{"rubric": [{"name": "Only", "weight": 1.0, "description": "Single"}]}`
	_, err := parseDynamicRubric(input)
	if err == nil {
		t.Fatal("Expected error for single criterion")
	}
}

func TestParseDynamicRubricMissingName(t *testing.T) {
	input := `{"rubric": [
		{"name": "", "weight": 0.5, "description": "No name"},
		{"name": "B", "weight": 0.5, "description": "Has name"}
	]}`
	_, err := parseDynamicRubric(input)
	if err == nil {
		t.Fatal("Expected error for missing criterion name")
	}
}

func TestParseDynamicRubricZeroWeight(t *testing.T) {
	input := `{"rubric": [
		{"name": "A", "weight": 0, "description": "Zero weight"},
		{"name": "B", "weight": 0.5, "description": "Good"}
	]}`
	_, err := parseDynamicRubric(input)
	if err == nil {
		t.Fatal("Expected error for zero weight")
	}
}

func TestGenerateDynamicRubricFallbackOnNilClient(t *testing.T) {
	rubric, tokens, err := GenerateDynamicRubric(context.Background(), "test", "model", nil, nil, DefaultRubric)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if tokens != 0 {
		t.Errorf("Expected 0 tokens on fallback, got %d", tokens)
	}
	if len(rubric) != len(DefaultRubric) {
		t.Errorf("Expected fallback to default rubric (%d criteria), got %d", len(DefaultRubric), len(rubric))
	}
}

func TestGenerateDynamicRubricFallbackOnEmptyPrompt(t *testing.T) {
	registry := providers.NewRegistry()
	mock := NewMockProvider("test")
	registry.Register(mock)
	client := NewMockLLMClient(registry)

	rubric, tokens, err := GenerateDynamicRubric(context.Background(), "", "mock-model", client, nil, DefaultRubric)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if tokens != 0 {
		t.Errorf("Expected 0 tokens on fallback, got %d", tokens)
	}
	if len(rubric) != len(DefaultRubric) {
		t.Errorf("Expected fallback to default rubric, got %d criteria", len(rubric))
	}
}

func TestGenerateDynamicRubricSuccess(t *testing.T) {
	registry := providers.NewRegistry()
	mock := NewMockProvider("test")
	mock.SetResponse("Generate scoring criteria", `{"rubric": [
		{"name": "Mathematical Accuracy", "weight": 0.5, "description": "Correct calculation"},
		{"name": "Explanation", "weight": 0.3, "description": "Clear step-by-step"},
		{"name": "Format", "weight": 0.2, "description": "Well-structured"}
	]}`)
	registry.Register(mock)
	client := NewMockLLMClient(registry)

	rubric, tokens, err := GenerateDynamicRubric(context.Background(), "What is 2+2?", "mock-model", client, nil, DefaultRubric)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(rubric) != 3 {
		t.Fatalf("Expected 3 dynamic criteria, got %d", len(rubric))
	}
	if rubric[0].Name != "Mathematical Accuracy" {
		t.Errorf("Expected first criterion 'Mathematical Accuracy', got %q", rubric[0].Name)
	}
	if tokens == 0 {
		t.Error("Expected non-zero tokens from successful rubric generation")
	}
}

func TestGenerateDynamicRubricFallbackOnBadResponse(t *testing.T) {
	registry := providers.NewRegistry()
	mock := NewMockProvider("test")
	// Return garbage that can't be parsed as rubric
	mock.SetResponse("Generate scoring criteria", "I cannot generate criteria right now.")
	registry.Register(mock)
	client := NewMockLLMClient(registry)

	rubric, tokens, err := GenerateDynamicRubric(context.Background(), "Some question", "mock-model", client, nil, DefaultRubric)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if tokens != 0 {
		t.Errorf("Expected 0 tokens on parse-failure fallback, got %d", tokens)
	}
	// Should fall back to static
	if len(rubric) != len(DefaultRubric) {
		t.Errorf("Expected fallback to default rubric (%d criteria), got %d", len(DefaultRubric), len(rubric))
	}
}

func TestParseDynamicRubricWithSurroundingText(t *testing.T) {
	// LLM might wrap JSON in extra text
	input := `Here are the criteria:
{"rubric": [
	{"name": "Logic", "weight": 0.5, "description": "Logical reasoning"},
	{"name": "Evidence", "weight": 0.5, "description": "Supporting evidence"}
]}
I hope this helps!`

	rubric, err := parseDynamicRubric(input)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(rubric) != 2 {
		t.Fatalf("Expected 2 criteria, got %d", len(rubric))
	}
}
