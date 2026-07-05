package bench

import "strings"

// IsCorrectPrediction applies benchmark-specific correctness grading.
func IsCorrectPrediction(item DatasetItem, predicted string, parseOK bool) bool {
	if !parseOK {
		return false
	}

	format := BenchmarkFormatMCQA
	if normalized, err := NormalizeBenchmark(item.Benchmark); err == nil {
		format = FormatForBenchmark(normalized)
	} else if strings.TrimSpace(item.GoldAnswer) != "" && len(item.Choices) == 0 {
		format = BenchmarkFormatMathAnswer
	}

	if format == BenchmarkFormatMathAnswer {
		gold := strings.TrimSpace(item.GoldAnswer)
		if gold == "" {
			gold = strings.TrimSpace(item.AnswerLabel)
		}
		return MathAnswersEquivalent(predicted, gold)
	}
	return strings.EqualFold(strings.TrimSpace(predicted), strings.TrimSpace(item.AnswerLabel))
}
