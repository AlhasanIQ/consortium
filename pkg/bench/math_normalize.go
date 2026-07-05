package bench

import (
	"regexp"
	"strings"
)

var (
	finalAnswerLineRegex      = regexp.MustCompile(`(?im)^\s*(?:final\s+answer|answer)\s*[:\-]\s*(.+?)\s*$`)
	finalAnswerPrefixRegex    = regexp.MustCompile(`(?i)^\s*(?:final\s+answer|answer)\s*[:\-]\s*`)
	varAssignmentRegex        = regexp.MustCompile(`^\s*[A-Za-z][A-Za-z0-9_]*\s*=\s*(.+?)\s*$`)
	latexCommandRegex         = regexp.MustCompile(`\\([A-Za-z]+)`)
	latexTextWrapperRegex     = regexp.MustCompile(`(?is)^\\text\s*\{(.+)\}$`)
	properCommaRegex          = regexp.MustCompile(`(\d),(\d{3})($|\D)`)
	commonUnitRegex           = regexp.MustCompile(`(?i)\b(?:degree|cm|centimeter|meter|mile|second|minute|hour|day|week|month|year|foot|feet|inch|yard)(?:es|s)?(?:\s*\^\s*\d+)?\b`)
	latexFracShortNumRegex    = regexp.MustCompile(`\\frac\s*(\d)\s*(\{[^{}]+\})`)
	latexFracTwoDigitRegex    = regexp.MustCompile(`\\frac\s*(\d)\s*(\d)`)
	latexTextMboxInlineRegex  = regexp.MustCompile(`\\(?:text|mbox)\s*\{[^{}]*\}(?:\s*\^\s*(?:\{[^{}]*\}|\d+))?`)
	latexSubscriptBracesRegex = regexp.MustCompile(`_\{(\w)\}`)
	digitCommaSpaceRegex      = regexp.MustCompile(`(\d),\s+(\d)`)
)

// ParseMathAnswer extracts a short canonical final answer from free-form output.
func ParseMathAnswer(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}

	if extracted, ok := extractAnswerFromJSON(trimmed); ok {
		trimmed = extracted
	}

	// Strip markdown bold markers so **Final answer: X** matches finalAnswerLineRegex
	trimmed = strings.ReplaceAll(trimmed, "**", "")

	if matches := finalAnswerLineRegex.FindAllStringSubmatch(trimmed, -1); len(matches) > 0 {
		candidate := NormalizeMathAnswer(matches[len(matches)-1][1])
		return candidate, candidate != ""
	}

	if boxed, ok := extractLastBoxed(normalizeMathSyntax(trimmed)); ok {
		candidate := NormalizeMathAnswer(boxed)
		return candidate, candidate != ""
	}

	lines := nonEmptyLines(trimmed)
	if len(lines) == 0 {
		return "", false
	}
	lastLine := NormalizeMathAnswer(lines[len(lines)-1])
	if lastLine == "" {
		return "", false
	}
	if len(lines) <= 3 && len(lastLine) <= 200 {
		return lastLine, true
	}
	if len(lastLine) <= 80 && !strings.Contains(lastLine, ". ") && looksMathAnswerCandidate(lastLine) {
		return lastLine, true
	}
	return "", false
}

func looksMathAnswerCandidate(value string) bool {
	tokens := strings.Fields(value)
	if len(tokens) == 0 || len(tokens) > 8 {
		return false
	}

	alphaWords := 0
	for _, tok := range tokens {
		letters := 0
		digits := 0
		for _, ch := range tok {
			switch {
			case ch >= 'a' && ch <= 'z':
				letters++
			case ch >= 'A' && ch <= 'Z':
				letters++
			case ch >= '0' && ch <= '9':
				digits++
			}
		}
		if letters > 2 && digits == 0 {
			alphaWords++
		}
	}
	return alphaWords <= 3
}

func nonEmptyLines(raw string) []string {
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		value := strings.TrimSpace(line)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func extractLastBoxed(raw string) (string, bool) {
	search := raw
	for {
		idx := strings.LastIndex(search, `\boxed`)
		if idx < 0 {
			return "", false
		}
		// idx is valid in both search and raw because search is only
		// truncated from the right, so positions [0,idx) are identical.
		open := strings.Index(raw[idx:], "{")
		if open < 0 {
			search = search[:idx]
			continue
		}
		open += idx
		content, ok := extractBalancedBraces(raw, open)
		if ok {
			return content, true
		}
		search = search[:idx]
	}
}

func extractBalancedBraces(raw string, open int) (string, bool) {
	if open < 0 || open >= len(raw) || raw[open] != '{' {
		return "", false
	}
	depth := 0
	start := open + 1
	for i := open; i < len(raw); i++ {
		switch raw[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return raw[start:i], true
			}
		}
	}
	return "", false
}

// normalizeMathSyntax canonicalizes common latex/unicode formatting variants so
// grading comparisons are resilient to superficial style differences.
func normalizeMathSyntax(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}

	value = strings.ReplaceAll(value, "\u2212", "-")
	value = strings.ReplaceAll(value, "\u00d7", "*")
	value = strings.ReplaceAll(value, "\u00b7", "*")
	value = strings.ReplaceAll(value, "\u221a", "sqrt")
	value = strings.ReplaceAll(value, "\u03c0", `\pi`)
	value = strings.ReplaceAll(value, "\u03a0", `\pi`)
	value = strings.ReplaceAll(value, "\u221e", "inf")
	value = strings.ReplaceAll(value, "\u00b1", `\pm`)

	value = latexCommandRegex.ReplaceAllStringFunc(value, func(cmd string) string {
		return `\` + strings.ToLower(cmd[1:])
	})

	value = strings.ReplaceAll(value, `\tfrac`, `\frac`)
	value = strings.ReplaceAll(value, `\dfrac`, `\frac`)
	value = strings.ReplaceAll(value, `\cdot`, `*`)
	value = strings.ReplaceAll(value, `\times`, `*`)
	value = strings.ReplaceAll(value, `\!`, "")
	value = strings.ReplaceAll(value, `\,`, "")
	// Collapse spaces around commas between digits (e.g., "11, 111" from \! removal)
	value = digitCommaSpaceRegex.ReplaceAllString(value, "$1,$2")
	value = strings.ReplaceAll(value, `\left`, "")
	value = strings.ReplaceAll(value, `\right`, "")
	value = strings.ReplaceAll(value, `\$`, "")

	if m := latexTextWrapperRegex.FindStringSubmatch(value); len(m) == 2 {
		value = strings.TrimSpace(m[1])
	}

	// Strip remaining inline \text{...} and \mbox{...} with optional exponent (unit annotations)
	value = latexTextMboxInlineRegex.ReplaceAllString(value, "")

	// Normalize short-form \frac (single-digit numerator without braces)
	value = latexFracShortNumRegex.ReplaceAllString(value, `\frac{$1}$2`)
	value = latexFracTwoDigitRegex.ReplaceAllString(value, `\frac{$1}{$2}`)

	// Normalize subscript braces _{X} to _X for single chars
	value = latexSubscriptBracesRegex.ReplaceAllString(value, `_$1`)

	return value
}

func stripProperlyFormattedCommas(raw string) string {
	expr := raw
	for {
		next := properCommaRegex.ReplaceAllString(expr, "${1}${2}${3}")
		if next == expr {
			return next
		}
		expr = next
	}
}

func stripCommonUnits(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, `^{\circ}`, "")
	value = strings.ReplaceAll(value, `^\circ`, "")
	value = strings.ReplaceAll(value, `\circ`, "")
	value = commonUnitRegex.ReplaceAllString(value, "")
	value = strings.Join(strings.Fields(value), " ")
	return strings.TrimSpace(value)
}

// NormalizeMathAnswer strips common wrappers and formatting noise.
func NormalizeMathAnswer(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}

	if extracted, ok := extractAnswerFromJSON(value); ok {
		value = extracted
	}

	// Strip markdown bold markers before prefix regex matching
	value = strings.ReplaceAll(value, "**", "")

	value = strings.TrimSpace(finalAnswerPrefixRegex.ReplaceAllString(value, ""))
	value = strings.Trim(value, "` \t\n\r")
	value = normalizeMathSyntax(value)

	for {
		trimmed := strings.TrimSpace(value)
		if inner, ok := unwrapWrapped(trimmed, "$", "$"); ok {
			value = inner
			continue
		}
		if inner, ok := unwrapWrapped(trimmed, `\(`, `\)`); ok {
			value = inner
			continue
		}
		if inner, ok := unwrapWrapped(trimmed, `\[`, `\]`); ok {
			value = inner
			continue
		}
		if strings.HasPrefix(trimmed, `\boxed{`) && strings.HasSuffix(trimmed, "}") {
			open := strings.Index(trimmed, "{")
			if open >= 0 {
				if inner, ok := extractBalancedBraces(trimmed, open); ok {
					value = inner
					continue
				}
			}
		}
		break
	}

	value = strings.TrimSpace(value)
	// Strip remaining dollar signs (currency, after $...$ delimiter unwrap)
	value = strings.ReplaceAll(value, "$", "")
	value = strings.Trim(value, `"'`)
	value = strings.TrimSpace(value)
	value = strings.TrimRight(value, ".;")
	value = strings.Join(strings.Fields(value), " ")
	return strings.TrimSpace(value)
}

func unwrapWrapped(raw, prefix, suffix string) (string, bool) {
	if strings.HasPrefix(raw, prefix) && strings.HasSuffix(raw, suffix) && len(raw) > len(prefix)+len(suffix) {
		inner := raw[len(prefix) : len(raw)-len(suffix)]
		return strings.TrimSpace(inner), true
	}
	return "", false
}

func stripVariableAssignment(raw string) (string, bool) {
	match := varAssignmentRegex.FindStringSubmatch(raw)
	if len(match) != 2 {
		return "", false
	}
	value := strings.TrimSpace(match[1])
	return value, value != ""
}

func looseMathString(raw string) string {
	value := strings.ToLower(strings.TrimSpace(normalizeMathSyntax(raw)))
	value = strings.ReplaceAll(value, " ", "")
	return value
}
