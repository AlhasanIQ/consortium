package optimize

import (
	"fmt"
	"strings"

	"github.com/alhasaniq/consortium/pkg/failureinsights"
)

func failureCaseCategory(category string) string {
	normalized := failureinsights.NormalizeWrongAnswerCategory(category)
	if normalized == "" {
		return failureinsights.WrongAnswerCategoryUnclassified
	}
	return normalized
}

func failureCaseDiagnosis(failure FailureCase) string {
	base := strings.TrimSpace(failureinsights.WrongAnswerCategoryExplanation(failure.Category))
	parsedAgents := 0
	correctAgents := 0
	for _, answer := range failure.AgentAnswers {
		if answer.ParseOK {
			parsedAgents++
			if answer.Correct {
				correctAgents++
			}
		}
	}
	if parsedAgents > 0 {
		base = strings.TrimSpace(base + " " + fmt.Sprintf("Agent votes: %d/%d parsed, %d correct.", parsedAgents, len(failure.AgentAnswers), correctAgents))
	}
	if failure.Flagged {
		reason := strings.TrimSpace(failure.FlagReason)
		if reason != "" {
			base = strings.TrimSpace(base + " " + fmt.Sprintf("Dataset item is flagged: %s.", reason))
		} else {
			base = strings.TrimSpace(base + " Dataset item is flagged.")
		}
	}
	return base
}

func summarizeAgentAnswers(answers []FailureAgentAnswer, maxItems int) string {
	if len(answers) == 0 || maxItems <= 0 {
		return ""
	}
	parts := make([]string, 0, min(len(answers), maxItems))
	for i, answer := range answers {
		if i >= maxItems {
			break
		}
		label := strings.TrimSpace(answer.Model)
		if label == "" {
			label = strings.TrimSpace(answer.NodeID)
		}
		if label == "" {
			label = fmt.Sprintf("agent-%d", i+1)
		}
		pred := strings.TrimSpace(answer.Answer)
		if pred == "" {
			pred = "-"
		}
		mark := "N"
		if !answer.ParseOK {
			mark = "X"
		} else if answer.Correct {
			mark = "Y"
		}
		parts = append(parts, fmt.Sprintf("%s:%s:%s", label, pred, mark))
	}
	return strings.Join(parts, " | ")
}
