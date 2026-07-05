package bench

import "strings"

// maxComparisonDepth caps recursion in both mathAnswersEquivalent (variable
// assignment stripping, tuple element-wise) and parseNumericValue (nested
// \frac).  The limit is generous — real math answers rarely nest deeper than
// 3-4 levels.
const maxComparisonDepth = 10

// MathAnswersEquivalent compares math answers with simple symbolic/numeric heuristics.
func MathAnswersEquivalent(predicted, gold string) bool {
	return mathAnswersEquivalent(predicted, gold, 0)
}

func mathAnswersEquivalent(predicted, gold string, depth int) bool {
	if depth > maxComparisonDepth {
		return false
	}
	p := NormalizeMathAnswer(predicted)
	g := NormalizeMathAnswer(gold)
	if p == "" || g == "" {
		return false
	}

	if strings.EqualFold(p, g) {
		return true
	}
	if looseMathString(p) == looseMathString(g) {
		return true
	}

	if rhs, ok := stripVariableAssignment(p); ok {
		if mathAnswersEquivalent(rhs, g, depth+1) {
			return true
		}
	}
	if rhs, ok := stripVariableAssignment(g); ok {
		if mathAnswersEquivalent(p, rhs, depth+1) {
			return true
		}
	}

	pParts, pSeq := splitTopLevelSequence(p)
	gParts, gSeq := splitTopLevelSequence(g)
	if pSeq && gSeq && len(pParts) == len(gParts) {
		if orderedSequenceMatch(pParts, gParts, depth+1) {
			return true
		}
		// Try unordered set comparison (e.g., "1,-2" vs "-2,1")
		if unorderedSequenceMatch(pParts, gParts, depth+1) {
			return true
		}
	}

	// Try \pm expansion (e.g., "1 \pm \sqrt{19}" vs "1+\sqrt{19}, 1-\sqrt{19}")
	if expanded, ok := expandPlusMinus(g); ok {
		if pSeq && len(pParts) == len(expanded) {
			if unorderedSequenceMatch(pParts, expanded, depth+1) {
				return true
			}
		}
	}
	if expanded, ok := expandPlusMinus(p); ok {
		if gSeq && len(gParts) == len(expanded) {
			if unorderedSequenceMatch(gParts, expanded, depth+1) {
				return true
			}
		}
	}

	pNum, pOK := parseNumericValue(p, 0)
	gNum, gOK := parseNumericValue(g, 0)
	if pOK && gOK {
		return almostEqual(pNum, gNum)
	}

	return false
}

func orderedSequenceMatch(pParts, gParts []string, depth int) bool {
	for i := range pParts {
		if !mathAnswersEquivalent(pParts[i], gParts[i], depth) {
			return false
		}
	}
	return true
}

func unorderedSequenceMatch(pParts, gParts []string, depth int) bool {
	if len(pParts) != len(gParts) || len(pParts) > 10 {
		return false
	}
	used := make([]bool, len(pParts))
	for _, gPart := range gParts {
		found := false
		for j, pPart := range pParts {
			if !used[j] && mathAnswersEquivalent(pPart, gPart, depth) {
				used[j] = true
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
