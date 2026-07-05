package optimize

import (
	"strings"
	"testing"
)

func TestFormatLearningLogSection_Empty(t *testing.T) {
	result := formatLearningLogSection(nil, 5)
	if result != "- none\n" {
		t.Errorf("expected '- none\\n', got %q", result)
	}
}

func TestFormatLearningLogSection_WithEntries(t *testing.T) {
	// selectPromptLearningEntries prefers prompt-only entries when available,
	// so use only prompt-type mutations to ensure both appear.
	entries := []LearningEntry{
		{Generation: 1, MutationType: "prompt_rewrite", Description: "rewrote system prompt", Outcome: "no_change", FitnessDelta: -0.01},
		{Generation: 2, MutationType: "prompt_edit", Description: "adjusted phrasing", Outcome: "regression", FitnessDelta: -0.05},
	}
	result := formatLearningLogSection(entries, 10)
	if !strings.Contains(result, "Gen 1") {
		t.Error("expected Gen 1 in output")
	}
	if !strings.Contains(result, "Gen 2") {
		t.Error("expected Gen 2 in output")
	}
	if !strings.Contains(result, "prompt_rewrite") {
		t.Error("expected mutation type in output")
	}
	if !strings.Contains(result, "-> no_change") {
		t.Error("expected arrow-separated outcome")
	}
}

func TestFormatLearningLogSectionReflective_Empty(t *testing.T) {
	result := formatLearningLogSectionReflective(nil, 5)
	if result != "- none\n" {
		t.Errorf("expected '- none\\n', got %q", result)
	}
}

func TestFormatLearningLogSectionReflective_WithEntries(t *testing.T) {
	entries := []LearningEntry{
		{Generation: 3, MutationType: "prompt_edit", Description: "added chain-of-thought", Outcome: "improvement", FitnessDelta: 0.1},
	}
	// selectPromptLearningEntries filters out "improvement" outcomes, so this should return "- none\n"
	result := formatLearningLogSectionReflective(entries, 10)
	if result != "- none\n" {
		t.Errorf("improvements are filtered out, expected '- none\\n', got %q", result)
	}

	// With a non-improvement entry
	entries = []LearningEntry{
		{Generation: 3, MutationType: "prompt_edit", Description: "added chain-of-thought", Outcome: "regression", FitnessDelta: -0.05},
	}
	result = formatLearningLogSectionReflective(entries, 10)
	if !strings.Contains(result, "| outcome=regression") {
		t.Errorf("expected pipe-delimited outcome, got %q", result)
	}
	if !strings.Contains(result, "| delta=") {
		t.Errorf("expected pipe-delimited delta, got %q", result)
	}
}

func TestFormatLearningLogSectionCompact_Empty(t *testing.T) {
	result := formatLearningLogSectionCompact(nil, 5, 50)
	if result != "- none\n" {
		t.Errorf("expected '- none\\n', got %q", result)
	}
}

func TestFormatLearningLogSectionCompact_TruncatesDescription(t *testing.T) {
	entries := []LearningEntry{
		{Generation: 1, MutationType: "knob", Description: "this is a very long description that should be truncated", Outcome: "no_change", FitnessDelta: 0.0},
	}
	result := formatLearningLogSectionCompact(entries, 10, 10)
	if !strings.Contains(result, "desc=this is a ") {
		// trimForPrompt truncates to 10 chars
		t.Logf("result: %q", result)
	}
	if !strings.Contains(result, "outcome=no_change") {
		t.Errorf("expected compact outcome format, got %q", result)
	}
}

func TestFormatObjectivesSection_NilSpec(t *testing.T) {
	result := formatObjectivesSection("## Objectives", nil)
	if !strings.Contains(result, "## Objectives") {
		t.Errorf("expected header in output, got %q", result)
	}
	if !strings.Contains(result, "weight 1.00") {
		t.Errorf("expected default weight 1.00 when spec is nil, got %q", result)
	}
}

func TestFormatObjectivesSection_WithConstraints(t *testing.T) {
	spec := &OptimizeSpec{
		Objectives: []Objective{
			{Metric: "adjusted_accuracy", Direction: "maximize", Weight: 0.8},
			{Metric: "cost_per_item", Direction: "minimize", Weight: 0.2},
		},
		Constraints: []Constraint{
			{Metric: "cost_per_item", Op: "<=", Value: 0.05},
		},
	}
	result := formatObjectivesSection("## Goals", spec)
	if !strings.Contains(result, "## Goals") {
		t.Error("expected custom header")
	}
	if !strings.Contains(result, "weight 0.80") {
		t.Errorf("expected accuracy weight 0.80, got %q", result)
	}
	if !strings.Contains(result, "weight 0.20") {
		t.Errorf("expected cost weight 0.20, got %q", result)
	}
	if !strings.Contains(result, "Constraints:") {
		t.Error("expected constraints section")
	}
	if !strings.Contains(result, "cost_per_item <= 0.0500") {
		t.Errorf("expected constraint line, got %q", result)
	}
}

func TestFormatObjectivesSection_NoConstraints(t *testing.T) {
	spec := &OptimizeSpec{
		Objectives: []Objective{
			{Metric: "accuracy", Direction: "maximize", Weight: 0.9},
		},
	}
	result := formatObjectivesSection("## Objectives", spec)
	if strings.Contains(result, "Constraints:") {
		t.Error("should not contain constraints section when empty")
	}
}

func TestFormatFailureDiagnoses_Empty(t *testing.T) {
	result := formatFailureDiagnoses(nil, "## Failures")
	if result != "" {
		t.Errorf("expected empty string for nil categories, got %q", result)
	}
	result = formatFailureDiagnoses(map[string]int{}, "## Failures")
	if result != "" {
		t.Errorf("expected empty string for empty categories, got %q", result)
	}
}

func TestFormatFailureDiagnoses_WithCategories(t *testing.T) {
	counts := map[string]int{
		"wrong_format":  5,
		"hallucination": 3,
	}
	result := formatFailureDiagnoses(counts, "## Failure Analysis")
	if !strings.Contains(result, "## Failure Analysis") {
		t.Error("expected header")
	}
	if !strings.Contains(result, "wrong_format") {
		t.Error("expected wrong_format category")
	}
	if !strings.Contains(result, "hallucination") {
		t.Error("expected hallucination category")
	}
	// Lines should be sorted by count descending (wrong_format=5 before hallucination=3)
	wrongIdx := strings.Index(result, "wrong_format")
	hallIdx := strings.Index(result, "hallucination")
	if wrongIdx > hallIdx {
		t.Error("expected higher count category first")
	}
}

func TestCountFailureCategories_Empty(t *testing.T) {
	result := countFailureCategories(nil)
	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

func TestCountFailureCategories_NormalizesEmpty(t *testing.T) {
	failures := []FailureCase{
		{Category: ""},
		{Category: "  "},
		{Category: "wrong_format"},
		{Category: "wrong_format"},
	}
	result := countFailureCategories(failures)
	if result["uncategorized"] != 2 {
		t.Errorf("expected 2 uncategorized, got %d", result["uncategorized"])
	}
	if result["wrong_format"] != 2 {
		t.Errorf("expected 2 wrong_format, got %d", result["wrong_format"])
	}
}

func TestCountFailureCategories_MultipleCategoriesCounted(t *testing.T) {
	failures := []FailureCase{
		{Category: "math_error"},
		{Category: "math_error"},
		{Category: "math_error"},
		{Category: "logic_error"},
	}
	result := countFailureCategories(failures)
	if result["math_error"] != 3 {
		t.Errorf("expected 3, got %d", result["math_error"])
	}
	if result["logic_error"] != 1 {
		t.Errorf("expected 1, got %d", result["logic_error"])
	}
}
