package bench

import (
	"fmt"
	"strings"
)

func BuildQuestionPrompt(item DatasetItem) (string, error) {
	if strings.TrimSpace(item.Question) == "" {
		return "", fmt.Errorf("dataset item %q has empty question", item.ID)
	}

	format := BenchmarkFormatMCQA
	if normalizedBenchmark, err := NormalizeBenchmark(item.Benchmark); err == nil {
		format = FormatForBenchmark(normalizedBenchmark)
	}

	switch format {
	case BenchmarkFormatMathAnswer:
		// Only structural formatting here — math-answer instructions live in
		// benchmark wrapper seed inputTemplates (single source of truth).
		var b strings.Builder
		b.WriteString("Question:\n")
		b.WriteString(item.Question)
		b.WriteString("\n")
		return b.String(), nil
	default:
		if len(item.Choices) < 2 {
			return "", fmt.Errorf("dataset item %q must have at least two choices", item.ID)
		}

		// Only structural formatting here — MCQA instructions live in
		// benchmark wrapper seed inputTemplates (single source of truth).
		var b strings.Builder
		b.WriteString("Question:\n")
		b.WriteString(item.Question)
		b.WriteString("\n\nOptions:\n")

		for i, choice := range item.Choices {
			label := LabelForIndex(i)
			if label == "" {
				return "", fmt.Errorf("dataset item %q has more than 26 options", item.ID)
			}
			b.WriteString(label)
			b.WriteString(". ")
			b.WriteString(choice)
			b.WriteString("\n")
		}

		return b.String(), nil
	}
}
