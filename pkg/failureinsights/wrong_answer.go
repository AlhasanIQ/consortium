package failureinsights

import "strings"

const (
	WrongAnswerCategoryAllStepsWrong         = "all_steps_wrong"
	WrongAnswerCategorySomeRightChildWrong   = "some_right_child_wrong"
	WrongAnswerCategoryAllRightChildWrong    = "all_right_child_wrong"
	WrongAnswerCategoryChildRightParentWrong = "child_right_parent_wrong"
	WrongAnswerCategoryUnclassified          = "unclassified"
)

// NormalizeWrongAnswerCategory canonicalizes category labels used in benchmark analysis.
func NormalizeWrongAnswerCategory(category string) string {
	return strings.ToLower(strings.TrimSpace(category))
}

// WrongAnswerCategoryExplanation returns a concise diagnosis for a benchmark failure category.
func WrongAnswerCategoryExplanation(category string) string {
	switch NormalizeWrongAnswerCategory(category) {
	case WrongAnswerCategoryAllStepsWrong:
		return "All agents across child and parent workflows answered incorrectly."
	case WrongAnswerCategorySomeRightChildWrong:
		return "Some child agents were correct, but child synthesis selected the wrong answer."
	case WrongAnswerCategoryAllRightChildWrong:
		return "All child agents were correct, but child synthesis still produced the wrong answer."
	case WrongAnswerCategoryChildRightParentWrong:
		return "Child workflow returned the correct answer, but parent synthesis overrode it."
	default:
		return "Failure pattern does not match a known benchmark pipeline category."
	}
}
