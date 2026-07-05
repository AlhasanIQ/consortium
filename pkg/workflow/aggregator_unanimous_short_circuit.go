package workflow

import "strings"

// unanimousAnswerDecision captures a deterministic unanimous-answer decision.
// It is used by evaluation-based aggregators to skip expensive LLM aggregation
// when every agent produced the same extractable answer.
type unanimousAnswerDecision struct {
	Answer        string
	WinnerAgentID string
	WinningOutput string
}

func maybeUnanimousAnswerDecision(inputs []AgentOutput, config map[string]interface{}) (*unanimousAnswerDecision, bool) {
	if len(inputs) == 0 {
		return nil, false
	}

	extractCfg := ParseExtractorConfig(config)
	consensus := ""
	for _, input := range inputs {
		answer := strings.TrimSpace(ExtractAnswer(input.Output, extractCfg))
		if answer == "" {
			return nil, false
		}
		if consensus == "" {
			consensus = answer
			continue
		}
		if answer != consensus {
			return nil, false
		}
	}
	if consensus == "" {
		return nil, false
	}

	return &unanimousAnswerDecision{
		Answer:        consensus,
		WinnerAgentID: inputs[0].AgentID,
		WinningOutput: inputs[0].Output,
	}, true
}
