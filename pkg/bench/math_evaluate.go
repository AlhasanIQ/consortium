package bench

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

var (
	mixedNumberRegex = regexp.MustCompile(`^([+\-]?\d+)\s+([+\-]?\d+)\s*/\s*([+\-]?\d+)$`)
	latexFracRegex   = regexp.MustCompile(`^\\frac\s*\{([^{}]+)\}\{([^{}]+)\}$`)
	latexFracPiRegex = regexp.MustCompile(`^([+\-]?\d*(?:\.\d+)?)?\s*\\frac\s*\{\\pi\}\{([+\-]?\d+(?:\.\d+)?)\}$`)
	latexPiDivRegex  = regexp.MustCompile(`^([+\-]?\d*(?:\.\d+)?)?\s*(?:\\pi|pi)\s*/\s*([+\-]?\d+(?:\.\d+)?)$`)
	latexPiMulRegex  = regexp.MustCompile(`^([+\-]?\d*(?:\.\d+)?)?\s*\*?\s*(?:\\pi|pi)$`)
	latexSqrtRegex   = regexp.MustCompile(`^([+\-]?\d+(?:\.\d+)?)?\s*(?:\\sqrt|sqrt)\s*\{?([+\-]?\d+(?:\.\d+)?)\}?$`)
)

func parseNumericValue(raw string, depth int) (float64, bool) {
	if depth > maxComparisonDepth {
		return 0, false
	}
	value := strings.TrimSpace(normalizeMathSyntax(raw))
	if value == "" {
		return 0, false
	}

	if rhs, ok := stripVariableAssignment(value); ok {
		value = strings.TrimSpace(normalizeMathSyntax(rhs))
	}
	value = stripProperlyFormattedCommas(value)

	if strings.HasPrefix(value, "(") && strings.HasSuffix(value, ")") {
		inner := strings.TrimSpace(value[1 : len(value)-1])
		if !strings.Contains(inner, ",") {
			value = inner
		}
	}

	if match := mixedNumberRegex.FindStringSubmatch(value); len(match) == 4 {
		whole, errWhole := strconv.ParseFloat(strings.TrimSpace(match[1]), 64)
		num, errNum := strconv.ParseFloat(strings.TrimSpace(match[2]), 64)
		den, errDen := strconv.ParseFloat(strings.TrimSpace(match[3]), 64)
		if errWhole == nil && errNum == nil && errDen == nil && den != 0 {
			if whole < 0 {
				return whole - (num / den), true
			}
			return whole + (num / den), true
		}
	}

	if match := latexFracRegex.FindStringSubmatch(value); len(match) == 3 {
		num, okNum := parseNumericValue(match[1], depth+1)
		den, okDen := parseNumericValue(match[2], depth+1)
		if okNum && okDen && den != 0 {
			return num / den, true
		}
	}

	if match := latexFracPiRegex.FindStringSubmatch(value); len(match) == 3 {
		coef, ok := parseOptionalCoefficient(match[1])
		if !ok {
			return 0, false
		}
		den, err := strconv.ParseFloat(strings.TrimSpace(match[2]), 64)
		if err != nil || den == 0 {
			return 0, false
		}
		return coef * math.Pi / den, true
	}

	if match := latexPiDivRegex.FindStringSubmatch(value); len(match) == 3 {
		coef, ok := parseOptionalCoefficient(match[1])
		if !ok {
			return 0, false
		}
		den, err := strconv.ParseFloat(strings.TrimSpace(match[2]), 64)
		if err != nil || den == 0 {
			return 0, false
		}
		return coef * math.Pi / den, true
	}

	if match := latexPiMulRegex.FindStringSubmatch(value); len(match) == 2 {
		coef, ok := parseOptionalCoefficient(match[1])
		if !ok {
			return 0, false
		}
		return coef * math.Pi, true
	}

	if match := latexSqrtRegex.FindStringSubmatch(value); len(match) == 3 {
		coef := 1.0
		if strings.TrimSpace(match[1]) != "" {
			v, err := strconv.ParseFloat(strings.TrimSpace(match[1]), 64)
			if err != nil {
				return 0, false
			}
			coef = v
		}
		base, err := strconv.ParseFloat(strings.TrimSpace(match[2]), 64)
		if err != nil || base < 0 {
			return 0, false
		}
		return coef * math.Sqrt(base), true
	}

	if strings.Contains(value, "/") {
		parts := strings.Split(value, "/")
		if len(parts) == 2 {
			num, okNum := parseNumericValue(parts[0], depth+1)
			den, okDen := parseNumericValue(parts[1], depth+1)
			if okNum && okDen && den != 0 {
				return num / den, true
			}
		}
	}

	numericValue := stripCommonUnits(value)
	if strings.HasSuffix(numericValue, "%") {
		base, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(numericValue, "%")), 64)
		if err == nil {
			return base / 100.0, true
		}
	}

	if parsed, err := strconv.ParseFloat(strings.ReplaceAll(numericValue, ",", ""), 64); err == nil {
		return parsed, true
	}
	return 0, false
}

func parseOptionalCoefficient(raw string) (float64, bool) {
	value := strings.TrimSpace(raw)
	switch value {
	case "", "+":
		return 1.0, true
	case "-":
		return -1.0, true
	default:
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	}
}

func almostEqual(a, b float64) bool {
	scale := math.Max(1.0, math.Max(math.Abs(a), math.Abs(b)))
	return math.Abs(a-b) <= 1e-9*scale
}

func splitTopLevelSequence(raw string) ([]string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, false
	}
	if len(value) >= 2 {
		first := value[0]
		last := value[len(value)-1]
		if (first == '(' && last == ')') || (first == '[' && last == ']') || (first == '{' && last == '}') {
			value = strings.TrimSpace(value[1 : len(value)-1])
		}
	}
	value = stripProperlyFormattedCommas(value)

	var parts []string
	start := 0
	depth := 0
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				part := strings.TrimSpace(value[start:i])
				parts = append(parts, part)
				start = i + 1
			}
		}
	}
	if len(parts) == 0 {
		return nil, false
	}
	last := strings.TrimSpace(value[start:])
	parts = append(parts, last)
	for _, part := range parts {
		if part == "" {
			return nil, false
		}
	}
	return parts, true
}

func expandPlusMinus(expr string) ([]string, bool) {
	idx := strings.Index(expr, `\pm`)
	if idx < 0 {
		return nil, false
	}
	// Ensure \pm is not part of a longer command (e.g., \pmatrix)
	end := idx + 3
	if end < len(expr) {
		ch := expr[end]
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' {
			return nil, false
		}
	}
	left := strings.TrimSpace(expr[:idx])
	right := strings.TrimSpace(expr[end:])
	if left == "" || right == "" {
		return nil, false
	}
	return []string{left + " + " + right, left + " - " + right}, true
}
