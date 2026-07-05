package bench

import (
	"regexp"
	"strings"
)

var (
	exactLetterRegex = regexp.MustCompile(`^\s*([A-Za-z])\s*$`)
	answerLineRegex  = regexp.MustCompile(`(?i)\b(?:final\s+answer|answer)\b(?:\s+is)?\s*[:\-]?\s*\(?\s*([A-Za-z])\s*\)?`)
	leadLetterRegex  = regexp.MustCompile(`^\s*\(?\s*([A-Za-z])\s*\)?[\.\):\-]`)
)

func LabelForIndex(idx int) string {
	if idx < 0 {
		return ""
	}
	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	if idx >= len(letters) {
		return ""
	}
	return string(letters[idx])
}

func IndexFromLabel(label string) (int, bool) {
	l := strings.ToUpper(strings.TrimSpace(label))
	if len(l) != 1 {
		return -1, false
	}
	ch := l[0]
	if ch < 'A' || ch > 'Z' {
		return -1, false
	}
	return int(ch - 'A'), true
}

func ParseAnswerLabel(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}

	for _, pattern := range []*regexp.Regexp{exactLetterRegex, answerLineRegex, leadLetterRegex} {
		match := pattern.FindStringSubmatch(trimmed)
		if len(match) < 2 {
			continue
		}
		label := strings.ToUpper(match[1])
		if _, ok := IndexFromLabel(label); ok {
			return label, true
		}
	}
	return "", false
}

func ParseAnswerLabelForChoices(raw string, choiceCount int) (string, bool) {
	label, ok := ParseAnswerLabel(raw)
	if !ok {
		return "", false
	}
	if choiceCount <= 0 {
		return label, true
	}
	idx, ok := IndexFromLabel(label)
	if !ok || idx >= choiceCount {
		return "", false
	}
	return label, true
}

// NormalizeBenchmarkAnswerForChoices enforces the benchmark output contract:
// trim whitespace, uppercase, require exactly one letter, and validate
// membership against the current item option labels (A..).
func NormalizeBenchmarkAnswerForChoices(raw string, choiceCount int) (string, bool) {
	normalized := strings.ToUpper(strings.TrimSpace(raw))
	if len(normalized) != 1 {
		return "", false
	}

	idx, ok := IndexFromLabel(normalized)
	if !ok {
		return "", false
	}

	if choiceCount > 0 && idx >= choiceCount {
		return "", false
	}

	return normalized, true
}
