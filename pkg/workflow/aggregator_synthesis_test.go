package workflow

import (
	"context"
	"strings"
	"testing"
)

// Tests in this file cover synthesis aggregator behavior NOT already in aggregator_test.go.
// Existing tests there cover: empty content retry, single input, multi-input needing LLM.

func TestSynthesisAggregator_NameMethod(t *testing.T) {
	s := &SynthesisAggregator{}
	if s.Name() != AggMethodSynthesis {
		t.Errorf("Name() = %q, want %q", s.Name(), AggMethodSynthesis)
	}
}

func TestSynthesisAggregator_SingleInputClearsWinnerFields(t *testing.T) {
	s := &SynthesisAggregator{}
	result, err := s.Aggregate(context.Background(), []AgentOutput{
		{AgentID: "only-one", Output: "solo output"},
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Synthesis specifically clears winner-related fields for single input
	if result.Winner != "" {
		t.Errorf("Winner = %q, want empty (synthesis clears it)", result.Winner)
	}
	if result.Scores != nil {
		t.Errorf("Scores = %v, want nil", result.Scores)
	}
	if result.Reasoning != "" {
		t.Errorf("Reasoning = %q, want empty", result.Reasoning)
	}
	if result.Output != "solo output" {
		t.Errorf("Output = %q, want %q", result.Output, "solo output")
	}
	if result.Method != AggMethodSynthesis {
		t.Errorf("Method = %q, want %q", result.Method, AggMethodSynthesis)
	}
}

func TestSynthesisAggregator_ConfigValidationChain(t *testing.T) {
	s := &SynthesisAggregator{}
	inputs := []AgentOutput{
		{AgentID: "a", Output: "one"},
		{AgentID: "b", Output: "two"},
	}

	// Each test adds one more valid field to show the config chain.
	tests := []struct {
		name   string
		config map[string]interface{}
		errStr string
	}{
		{
			name:   "missing model",
			config: map[string]interface{}{},
			errStr: "model",
		},
		{
			name:   "missing system_prompt",
			config: map[string]interface{}{"model": "m"},
			errStr: "system_prompt",
		},
		{
			name:   "missing prompt",
			config: map[string]interface{}{"model": "m", "system_prompt": "sp"},
			errStr: "prompt",
		},
		{
			name:   "missing temperature",
			config: map[string]interface{}{"model": "m", "system_prompt": "sp", "prompt": "p"},
			errStr: "temperature",
		},
		{
			name:   "missing max_tokens",
			config: map[string]interface{}{"model": "m", "system_prompt": "sp", "prompt": "p", "temperature": 0.5},
			errStr: "max_tokens",
		},
		{
			name:   "nil client (all config valid)",
			config: map[string]interface{}{"model": "m", "system_prompt": "sp", "prompt": "p", "temperature": 0.5, "max_tokens": float64(100)},
			errStr: "LLM client",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.Aggregate(context.Background(), inputs, tt.config, nil, nil)
			if err == nil {
				t.Fatalf("expected error containing %q", tt.errStr)
			}
			if !strings.Contains(err.Error(), tt.errStr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.errStr)
			}
		})
	}
}

func TestSynthesisAggregator_EmptyInputs(t *testing.T) {
	s := &SynthesisAggregator{}
	_, err := s.Aggregate(context.Background(), []AgentOutput{}, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for empty inputs")
	}
}

func TestRegisterSynthesisAggregator_InRegistry(t *testing.T) {
	registry := &AggregatorRegistry{
		aggregators: make(map[AggregationMethod]Aggregator),
	}
	RegisterSynthesisAggregator(registry)

	agg, ok := registry.Get(AggMethodSynthesis)
	if !ok {
		t.Fatal("synthesis aggregator not registered")
	}
	if agg.Name() != AggMethodSynthesis {
		t.Errorf("Name() = %q, want %q", agg.Name(), AggMethodSynthesis)
	}
}

func TestDefaultSynthesisPromptConstants(t *testing.T) {
	if DefaultSynthesisSystemPrompt == "" {
		t.Error("DefaultSynthesisSystemPrompt is empty")
	}
	if DefaultSynthesisPrompt == "" {
		t.Error("DefaultSynthesisPrompt is empty")
	}
	if !strings.Contains(DefaultSynthesisPrompt, "{{prompt}}") {
		t.Error("missing {{prompt}} placeholder")
	}
	if !strings.Contains(DefaultSynthesisPrompt, "{{responses}}") {
		t.Error("missing {{responses}} placeholder")
	}
}
