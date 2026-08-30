package workflow

import "testing"

func TestMaybeUnanimousAnswerDecision_EmptyInputs(t *testing.T) {
	decision, ok := maybeUnanimousAnswerDecision(nil, nil)
	if ok || decision != nil {
		t.Error("empty inputs should return nil, false")
	}
}

// defaultConfig returns a config map that triggers the default regex extraction strategy.
func defaultConfig() map[string]interface{} {
	return map[string]interface{}{
		"extraction_strategy": "regex",
		"extraction_pattern":  DefaultExtractionPattern,
	}
}

func TestMaybeUnanimousAnswerDecision_SingleAgent(t *testing.T) {
	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "The answer is B."},
	}
	decision, ok := maybeUnanimousAnswerDecision(inputs, defaultConfig())
	if !ok {
		t.Fatal("single agent with extractable answer should be unanimous")
	}
	if decision.Answer != "B" {
		t.Errorf("expected answer 'B', got %q", decision.Answer)
	}
	if decision.WinnerAgentID != "agent-1" {
		t.Errorf("expected winner agent-1, got %q", decision.WinnerAgentID)
	}
}

func TestMaybeUnanimousAnswerDecision_AllAgree(t *testing.T) {
	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: C"},
		{AgentID: "agent-2", Output: "The answer is C."},
		{AgentID: "agent-3", Output: "Answer: C"},
	}
	decision, ok := maybeUnanimousAnswerDecision(inputs, defaultConfig())
	if !ok {
		t.Fatal("all agents agreeing should return unanimous decision")
	}
	if decision.Answer != "C" {
		t.Errorf("expected answer 'C', got %q", decision.Answer)
	}
	if decision.WinnerAgentID != "agent-1" {
		t.Errorf("expected winner agent-1, got %q", decision.WinnerAgentID)
	}
	if decision.AgreementRatio != 1.0 {
		t.Fatalf("expected full agreement, got %.3f", decision.AgreementRatio)
	}
}

func TestMaybeUnanimousAnswerDecision_Disagree(t *testing.T) {
	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: A"},
		{AgentID: "agent-2", Output: "Final answer: B"},
	}
	decision, ok := maybeUnanimousAnswerDecision(inputs, defaultConfig())
	if ok || decision != nil {
		t.Error("disagreeing agents should return nil, false")
	}
}

func TestMaybeUnanimousAnswerDecision_NoExtractableAnswer(t *testing.T) {
	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "I'm not sure about this question."},
		{AgentID: "agent-2", Output: "This is a complex topic."},
	}
	decision, ok := maybeUnanimousAnswerDecision(inputs, defaultConfig())
	if ok || decision != nil {
		t.Error("non-extractable answers should return nil, false")
	}
}

func TestMaybeUnanimousAnswerDecision_MixedExtractable(t *testing.T) {
	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: A"},
		{AgentID: "agent-2", Output: "I don't know the answer."},
	}
	decision, ok := maybeUnanimousAnswerDecision(inputs, defaultConfig())
	if ok || decision != nil {
		t.Error("mixed extractable/non-extractable should return nil, false")
	}
}

func TestMaybeUnanimousAnswerDecision_NilConfig(t *testing.T) {
	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: A"},
	}
	decision, ok := maybeUnanimousAnswerDecision(inputs, nil)
	if ok || decision != nil {
		t.Error("nil config produces no extraction strategy, should return nil, false")
	}
}

func TestMaybeConsensusAnswerDecision_OptInQuorum(t *testing.T) {
	config := defaultConfig()
	config["short_circuit_threshold"] = 0.75
	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: B"},
		{AgentID: "agent-2", Output: "Answer: B"},
		{AgentID: "agent-3", Output: "The answer is B."},
		{AgentID: "agent-4", Output: "Final answer: C"},
	}

	decision, ok := maybeConsensusAnswerDecision(inputs, config)
	if !ok {
		t.Fatal("3/4 agreement should satisfy an explicit 0.75 quorum")
	}
	if decision.Answer != "B" || decision.WinnerAgentID != "agent-1" {
		t.Fatalf("unexpected consensus decision: %+v", decision)
	}
	if decision.AgreementRatio != 0.75 {
		t.Fatalf("expected 0.75 agreement, got %.3f", decision.AgreementRatio)
	}
	if len(decision.DissentingIDs) != 1 || decision.DissentingIDs[0] != "agent-4" {
		t.Fatalf("unexpected dissenters: %v", decision.DissentingIDs)
	}
}

func TestMaybeConsensusAnswerDecision_DefaultRemainsUnanimous(t *testing.T) {
	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: B"},
		{AgentID: "agent-2", Output: "Final answer: B"},
		{AgentID: "agent-3", Output: "Final answer: C"},
	}
	if decision, ok := maybeConsensusAnswerDecision(inputs, defaultConfig()); ok || decision != nil {
		t.Fatal("default behavior must remain unanimous-only for backward compatibility")
	}
}

func TestMaybeConsensusAnswerDecision_UsesAllInputsAsDenominator(t *testing.T) {
	config := defaultConfig()
	config["short_circuit_threshold"] = 0.75
	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: A"},
		{AgentID: "agent-2", Output: "Final answer: A"},
		{AgentID: "agent-3", Output: "No extractable conclusion here"},
		{AgentID: "agent-4", Output: "No discrete answer"},
	}
	if decision, ok := maybeConsensusAnswerDecision(inputs, config); ok || decision != nil {
		t.Fatal("2 extracted votes out of 4 inputs must not be treated as 100% consensus")
	}
}

func TestMaybeConsensusAnswerDecision_MinVotes(t *testing.T) {
	config := defaultConfig()
	config["short_circuit_threshold"] = 0.5
	config["short_circuit_min_votes"] = 3
	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: A"},
		{AgentID: "agent-2", Output: "Final answer: A"},
		{AgentID: "agent-3", Output: "Final answer: B"},
	}
	if decision, ok := maybeConsensusAnswerDecision(inputs, config); ok || decision != nil {
		t.Fatal("quorum must respect short_circuit_min_votes")
	}
}

func TestMaybeConsensusAnswerDecision_RejectsTie(t *testing.T) {
	config := defaultConfig()
	config["short_circuit_threshold"] = 0.5
	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: A"},
		{AgentID: "agent-2", Output: "Final answer: A"},
		{AgentID: "agent-3", Output: "Final answer: B"},
		{AgentID: "agent-4", Output: "Final answer: B"},
	}
	if decision, ok := maybeConsensusAnswerDecision(inputs, config); ok || decision != nil {
		t.Fatal("a tied plurality must never short-circuit")
	}
}

func TestParseConsensusShortCircuitConfig_Validation(t *testing.T) {
	cases := []map[string]interface{}{
		{"short_circuit_threshold": 0.49},
		{"short_circuit_threshold": 1.01},
		{"short_circuit_threshold": "fast"},
		{"short_circuit_min_votes": 1},
	}
	for _, config := range cases {
		if _, _, err := parseConsensusShortCircuitConfig(config); err == nil {
			t.Fatalf("expected invalid config to fail: %#v", config)
		}
	}
}
