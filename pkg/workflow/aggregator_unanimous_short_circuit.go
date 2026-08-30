package workflow

import (
	"fmt"
	"sort"
	"strings"
)

const (
	defaultConsensusThreshold = 1.0
	defaultConsensusMinVotes  = 2
)

// consensusAnswerDecision captures a deterministic answer-level consensus
// decision. Expensive evaluation aggregators can use it to avoid an LLM judge
// when the ensemble already has enough agreement.
//
// The default threshold is 1.0, preserving the historical unanimous-only
// behavior. Lower thresholds are opt-in through aggregationConfig.
type consensusAnswerDecision struct {
	Answer         string
	WinnerAgentID  string
	WinningOutput  string
	AgreementRatio float64
	AgreeingIDs    []string
	DissentingIDs  []string
	ExtractedVotes int
	TotalInputs    int
	Threshold      float64
}

// unanimousAnswerDecision is retained as an alias for compatibility with tests
// and callers that use the original helper name.
type unanimousAnswerDecision = consensusAnswerDecision

// parseConsensusShortCircuitConfig reads the optional adaptive fast-path knobs.
//
// short_circuit_threshold: fraction of *all* inputs that must agree, [0.5, 1.0].
// short_circuit_min_votes:  minimum number of agreeing extracted votes, >= 2.
//
// Counting all inputs in the denominator is deliberately conservative: an
// unextractable response does not silently make a weak quorum look stronger.
func parseConsensusShortCircuitConfig(config map[string]interface{}) (threshold float64, minVotes int, err error) {
	threshold = defaultConsensusThreshold
	minVotes = defaultConsensusMinVotes
	if config == nil {
		return threshold, minVotes, nil
	}

	if raw, ok := config["short_circuit_threshold"]; ok {
		value, valid := numericToFloat64(raw)
		if !valid || value < 0.5 || value > 1.0 {
			return 0, 0, fmt.Errorf("short_circuit_threshold must be a number between 0.5 and 1.0")
		}
		threshold = value
	}
	if raw, ok := config["short_circuit_min_votes"]; ok {
		value, valid := numericToInt(raw)
		if !valid || value < 2 {
			return 0, 0, fmt.Errorf("short_circuit_min_votes must be an integer >= 2")
		}
		minVotes = value
	}
	return threshold, minVotes, nil
}

// maybeConsensusAnswerDecision extracts discrete answers and returns a fast-path
// decision when the strongest answer reaches the configured quorum. The result
// is deterministic: ties are broken lexicographically for counting purposes,
// but an equal top count is never accepted.
func maybeConsensusAnswerDecision(inputs []AgentOutput, config map[string]interface{}) (*consensusAnswerDecision, bool) {
	if len(inputs) == 0 {
		return nil, false
	}

	threshold, minVotes, err := parseConsensusShortCircuitConfig(config)
	if err != nil {
		return nil, false
	}

	extractCfg := ParseExtractorConfig(config)
	answerToIDs := make(map[string][]string)
	answerByID := make(map[string]string, len(inputs))
	for _, input := range inputs {
		answer := strings.TrimSpace(ExtractAnswer(input.Output, extractCfg))
		if answer == "" {
			continue
		}
		answerByID[input.AgentID] = answer
		answerToIDs[answer] = append(answerToIDs[answer], input.AgentID)
	}
	if len(answerByID) == 0 {
		return nil, false
	}

	type vote struct {
		answer string
		ids    []string
	}
	votes := make([]vote, 0, len(answerToIDs))
	for answer, ids := range answerToIDs {
		votes = append(votes, vote{answer: answer, ids: ids})
	}
	sort.Slice(votes, func(i, j int) bool {
		if len(votes[i].ids) != len(votes[j].ids) {
			return len(votes[i].ids) > len(votes[j].ids)
		}
		return votes[i].answer < votes[j].answer
	})

	best := votes[0]
	if len(best.ids) < minVotes {
		return nil, false
	}
	if len(votes) > 1 && len(votes[1].ids) == len(best.ids) {
		return nil, false
	}

	ratio := float64(len(best.ids)) / float64(len(inputs))
	if ratio+1e-12 < threshold {
		return nil, false
	}

	agreeingSet := make(map[string]struct{}, len(best.ids))
	for _, id := range best.ids {
		agreeingSet[id] = struct{}{}
	}

	var winnerID, winningOutput string
	dissenting := make([]string, 0, len(inputs)-len(best.ids))
	for _, input := range inputs {
		if _, ok := agreeingSet[input.AgentID]; ok {
			if winnerID == "" {
				winnerID = input.AgentID
				winningOutput = input.Output
			}
			continue
		}
		dissenting = append(dissenting, input.AgentID)
	}

	return &consensusAnswerDecision{
		Answer:         best.answer,
		WinnerAgentID:  winnerID,
		WinningOutput:  winningOutput,
		AgreementRatio: ratio,
		AgreeingIDs:    append([]string(nil), best.ids...),
		DissentingIDs:  dissenting,
		ExtractedVotes: len(answerByID),
		TotalInputs:    len(inputs),
		Threshold:      threshold,
	}, true
}

// maybeUnanimousAnswerDecision preserves the historical unanimous-only fast
// path for scoring and peer_matrix. Adaptive quorum is opt-in at call sites that
// explicitly use maybeConsensusAnswerDecision, so their result metadata remains
// truthful until they adopt quorum-aware scoring semantics.
func maybeUnanimousAnswerDecision(inputs []AgentOutput, config map[string]interface{}) (*unanimousAnswerDecision, bool) {
	if len(inputs) == 0 {
		return nil, false
	}
	if len(inputs) == 1 {
		extractCfg := ParseExtractorConfig(config)
		answer := strings.TrimSpace(ExtractAnswer(inputs[0].Output, extractCfg))
		if answer == "" {
			return nil, false
		}
		return &consensusAnswerDecision{
			Answer:         answer,
			WinnerAgentID:  inputs[0].AgentID,
			WinningOutput:  inputs[0].Output,
			AgreementRatio: 1.0,
			AgreeingIDs:    []string{inputs[0].AgentID},
			ExtractedVotes: 1,
			TotalInputs:    1,
			Threshold:      1.0,
		}, true
	}

	strict := make(map[string]interface{}, len(config)+2)
	for key, value := range config {
		strict[key] = value
	}
	strict["short_circuit_threshold"] = 1.0
	strict["short_circuit_min_votes"] = len(inputs)
	return maybeConsensusAnswerDecision(inputs, strict)
}
