package workflow

import (
	"testing"
)

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
	// With nil config, ParseExtractorConfig returns zero-value config (no strategy).
	// ExtractAnswer with empty strategy returns "", so no extraction happens.
	inputs := []AgentOutput{
		{AgentID: "agent-1", Output: "Final answer: A"},
	}
	decision, ok := maybeUnanimousAnswerDecision(inputs, nil)
	if ok || decision != nil {
		t.Error("nil config produces no extraction strategy, should return nil, false")
	}
}
