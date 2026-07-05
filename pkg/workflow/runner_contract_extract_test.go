package workflow

import (
	"context"
	"fmt"
	"regexp"
	"testing"
)

// --- ContractExtractNodeRunner unit tests ---

func TestContractExtractNodeRunner_NodeType(t *testing.T) {
	runner := &ContractExtractNodeRunner{}
	if runner.NodeType() != NodeTypeContractExtract {
		t.Errorf("NodeType() = %q, want %q", runner.NodeType(), NodeTypeContractExtract)
	}
}

// --- Regex extraction success cases ---

func TestContractExtractNodeRunner_RegexExactLetter(t *testing.T) {
	runner := contractExtractTestRunner(t)
	for _, input := range []string{"B", "  c  ", " A "} {
		sc := contractExtractContext("source", input, benchmarkExtractionPatterns())
		result, err := runner.Execute(withStrictNodeContextDefaults(sc))
		if err != nil {
			t.Fatalf("input %q: unexpected error: %v", input, err)
		}
		if !result.Success {
			t.Fatalf("input %q: expected success, got error: %s", input, result.Error)
		}
		if result.Cost != 0 || result.TokensInput != 0 || result.TokensOutput != 0 {
			t.Errorf("input %q: regex path should have zero cost/tokens, got cost=%.4f tokens=%d/%d",
				input, result.Cost, result.TokensInput, result.TokensOutput)
		}
		method, _ := result.Metadata["extraction_method"].(string)
		if method != "regex" {
			t.Errorf("input %q: extraction_method = %q, want 'regex'", input, method)
		}
	}
}

func TestContractExtractNodeRunner_RegexAnswerLine(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"final answer colon", "After analysis...\nFinal answer: C\nReasoning: because...", "C"},
		{"answer is", "The answer is B", "B"},
		{"answer dash", "answer - D", "D"},
		{"answer parens", "Answer: (A)", "A"},
		{"final answer lowercase", "final answer: e", "E"},
		{"answer is uppercase", "ANSWER IS C", "C"},
		{"long reasoning then answer", "This is a very long analysis of the problem. " +
			"We should consider multiple factors including X, Y, and Z. " +
			"After careful consideration, Final answer: B\nReasoning: B is correct.", "B"},
	}
	runner := contractExtractTestRunner(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := contractExtractContext("source", tt.input, benchmarkExtractionPatterns())
			result, err := runner.Execute(withStrictNodeContextDefaults(sc))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !result.Success || result.Output != tt.want {
				t.Errorf("got output=%q success=%v, want %q", result.Output, result.Success, tt.want)
			}
			if method, _ := result.Metadata["extraction_method"].(string); method != "regex" {
				t.Errorf("extraction_method = %q, want 'regex'", method)
			}
		})
	}
}

func TestContractExtractNodeRunner_RegexLeadingLetter(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"A dot", "A. Because the first option...", "A"},
		{"B paren", "(B) The reason is...", "B"},
		{"C colon", "C: it follows that...", "C"},
		{"D close paren", "D) explanation", "D"},
	}
	runner := contractExtractTestRunner(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := contractExtractContext("source", tt.input, benchmarkExtractionPatterns())
			result, err := runner.Execute(withStrictNodeContextDefaults(sc))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !result.Success || result.Output != tt.want {
				t.Errorf("got output=%q success=%v, want %q", result.Output, result.Success, tt.want)
			}
			if method, _ := result.Metadata["extraction_method"].(string); method != "regex" {
				t.Errorf("extraction_method = %q, want 'regex'", method)
			}
		})
	}
}

// --- Regex extraction failure → LLM fallback ---

func TestContractExtractNodeRunner_LLMFallback(t *testing.T) {
	runner := contractExtractTestRunner(t)
	// Text that no pattern can extract from
	sc := contractExtractContext("source", "I think it's probably the second option", benchmarkExtractionPatterns())
	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success from LLM fallback, got error: %s", result.Error)
	}
	method, _ := result.Metadata["extraction_method"].(string)
	if method != "llm_fallback" {
		t.Errorf("extraction_method = %q, want 'llm_fallback'", method)
	}
	// LLM fallback should have non-zero tokens
	if result.TokensInput == 0 && result.TokensOutput == 0 {
		t.Error("expected non-zero tokens from LLM fallback")
	}
}

func TestContractExtractNodeRunner_EmptySourceText_NonRetryableError(t *testing.T) {
	runner := contractExtractTestRunner(t)
	sc := contractExtractContext("source", "", benchmarkExtractionPatterns())
	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err == nil {
		t.Fatal("expected error for empty source text")
	}
	if result.Success {
		t.Fatal("expected failure result for empty source text")
	}
	assertNonRetryableError(t, err)
}

func TestContractExtractNodeRunner_WhitespaceOnlySourceText_NonRetryableError(t *testing.T) {
	runner := contractExtractTestRunner(t)
	sc := contractExtractContext("source", "   \n\t  ", benchmarkExtractionPatterns())
	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err == nil {
		t.Fatal("expected error for whitespace-only source text")
	}
	if result.Success {
		t.Fatal("expected failure result for whitespace-only source text")
	}
	assertNonRetryableError(t, err)
}

// --- Missing/invalid configuration ---

func TestContractExtractNodeRunner_MissingSourceVariable(t *testing.T) {
	runner := contractExtractTestRunner(t)
	sc := &NodeContext{
		Ctx:    context.Background(),
		NodeID: "contract-node",
		Node: &Node{
			Type:     NodeTypeContractExtract,
			Model:    "mock-model",
			Prompt:   "extract",
			Metadata: map[string]interface{}{}, // no source_variable
		},
		WorkflowContext: map[string]interface{}{"source": "B"},
	}
	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err == nil {
		t.Fatal("expected error for missing source_variable")
	}
	if result.Success {
		t.Fatal("expected failure result for missing source_variable")
	}
	assertNonRetryableError(t, err)
}

func TestContractExtractNodeRunner_SourceVariableNotInContext(t *testing.T) {
	runner := contractExtractTestRunner(t)
	sc := &NodeContext{
		Ctx:    context.Background(),
		NodeID: "contract-node",
		Node: &Node{
			Type:   NodeTypeContractExtract,
			Model:  "mock-model",
			Prompt: "extract {{missing}}",
			Metadata: map[string]interface{}{
				"source_variable":     "nonexistent",
				"extraction_patterns": benchmarkExtractionPatterns(),
			},
		},
		WorkflowContext: map[string]interface{}{}, // source not present
	}
	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err == nil {
		t.Fatal("expected non-retryable error for missing source variable in workflow context")
	}
	if result.Success {
		t.Fatal("expected failure result for missing source variable in workflow context")
	}
	assertNonRetryableError(t, err)
}

func TestContractExtractNodeRunner_NilSourceValue(t *testing.T) {
	runner := contractExtractTestRunner(t)
	sc := &NodeContext{
		Ctx:    context.Background(),
		NodeID: "contract-node",
		Node: &Node{
			Type:   NodeTypeContractExtract,
			Model:  "mock-model",
			Prompt: "extract",
			Metadata: map[string]interface{}{
				"source_variable":     "source",
				"extraction_patterns": benchmarkExtractionPatterns(),
			},
		},
		WorkflowContext: map[string]interface{}{"source": nil},
	}
	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err == nil {
		t.Fatal("expected non-retryable error for nil source value")
	}
	if result.Success {
		t.Fatal("expected failure result for nil source value")
	}
	assertNonRetryableError(t, err)
}

// --- Extraction patterns edge cases ---

func TestContractExtractNodeRunner_NoPatterns_DirectLLM(t *testing.T) {
	runner := contractExtractTestRunner(t)
	sc := &NodeContext{
		Ctx:    context.Background(),
		NodeID: "contract-node",
		Node: &Node{
			Type:   NodeTypeContractExtract,
			Model:  "mock-model",
			Prompt: "extract",
			Metadata: map[string]interface{}{
				"source_variable": "source",
				// no extraction_patterns — should go straight to LLM
			},
		},
		WorkflowContext: map[string]interface{}{"source": "B"},
	}
	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	method, _ := result.Metadata["extraction_method"].(string)
	if method != "llm_fallback" {
		t.Errorf("no patterns should go to LLM, got extraction_method=%q", method)
	}
}

func TestContractExtractNodeRunner_InvalidPattern_Skipped(t *testing.T) {
	runner := contractExtractTestRunner(t)
	sc := contractExtractContext("source", "Final answer: D", []interface{}{
		"[invalid regex", // invalid — should be skipped
		`(?i)\b(?:final\s+answer|answer)\b(?:\s+is)?\s*[:\-]?\s*\(?\s*([A-Za-z])\s*\)?\s*(?:$|\n|[^A-Za-z])`,
	})
	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "D" {
		t.Errorf("expected D, got %q", result.Output)
	}
	if method, _ := result.Metadata["extraction_method"].(string); method != "regex" {
		t.Errorf("extraction_method = %q, want 'regex'", method)
	}
}

func TestContractExtractNodeRunner_AllPatternsInvalid_LLMFallback(t *testing.T) {
	runner := contractExtractTestRunner(t)
	sc := contractExtractContext("source", "B", []interface{}{
		"[bad1",
		"[bad2",
	})
	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	method, _ := result.Metadata["extraction_method"].(string)
	if method != "llm_fallback" {
		t.Errorf("all invalid patterns should fall back to LLM, got extraction_method=%q", method)
	}
}

func TestContractExtractNodeRunner_PatternPrecedence(t *testing.T) {
	runner := contractExtractTestRunner(t)
	// Both patterns can match, but first one should win.
	sc := contractExtractContext("source", "A. The answer is C", []interface{}{
		`^\s*\(?\s*([A-Za-z])\s*\)?[\.\):\-]`, // leading letter — captures "A"
		`(?i)answer\s+is\s+([A-Za-z])`,        // answer-is — captures "C"
	})
	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "A" {
		t.Errorf("expected A (first pattern wins), got %q", result.Output)
	}
}

// --- matchFirstCaptureGroup unit tests ---

func TestMatchFirstCaptureGroup(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		text    string
		want    string
	}{
		{"exact letter", `^\s*([A-Za-z])\s*$`, "B", "B"},
		{"lowercase normalized", `^\s*([A-Za-z])\s*$`, "c", "C"},
		{"no match", `^\s*([A-Za-z])\s*$`, "hello", ""},
		{"no capture group", `^[A-Z]$`, "B", ""},
		{"empty text", `^\s*([A-Za-z])\s*$`, "", ""},
		{"multi-char capture", `^(\w+)$`, "hello", "HELLO"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			re := mustCompile(t, tt.pattern)
			got := matchFirstCaptureGroup(re, tt.text)
			if got != tt.want {
				t.Errorf("matchFirstCaptureGroup(%q, %q) = %q, want %q", tt.pattern, tt.text, got, tt.want)
			}
		})
	}
}

// --- parseExtractionPatterns unit tests ---

func TestParseExtractionPatterns(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]interface{}
		want     int
	}{
		{"nil metadata", nil, 0},
		{"no key", map[string]interface{}{}, 0},
		{"[]string", map[string]interface{}{"extraction_patterns": []string{"a", "b"}}, 2},
		{"[]interface{}", map[string]interface{}{"extraction_patterns": []interface{}{"a", "b", "c"}}, 3},
		{"mixed with non-strings", map[string]interface{}{"extraction_patterns": []interface{}{"a", 42, "b"}}, 2},
		{"empty strings filtered", map[string]interface{}{"extraction_patterns": []interface{}{"a", "", "b"}}, 2},
		{"wrong type", map[string]interface{}{"extraction_patterns": 42}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseExtractionPatterns(tt.metadata)
			if len(got) != tt.want {
				t.Errorf("parseExtractionPatterns returned %d patterns, want %d", len(got), tt.want)
			}
		})
	}
}

// --- Smoke tests: realistic benchmark scenarios ---

func TestContractExtractNodeRunner_SmokeRealisticSynthesisOutput(t *testing.T) {
	runner := contractExtractTestRunner(t)
	// Realistic synthesis output from L1 reasoning seed
	upstreamOutput := `After analyzing all three expert responses, I can identify a strong consensus.

Expert 1 (Gemini) correctly identified that mitochondria are the powerhouse of the cell and selected option B.
Expert 2 (Kimi) provided detailed reasoning about cellular respiration occurring in mitochondria and also selected B.
Expert 3 (GLM) agreed with the other experts.

All three experts converge on the same answer with solid reasoning.

Final answer: B
Reasoning: Mitochondria are responsible for cellular respiration and ATP production, making them the "powerhouse" of the cell.`

	sc := contractExtractContext("child-reasoning-synthesis", upstreamOutput, benchmarkExtractionPatterns())
	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success || result.Output != "B" {
		t.Errorf("expected B, got %q (success=%v, err=%s)", result.Output, result.Success, result.Error)
	}
	if method, _ := result.Metadata["extraction_method"].(string); method != "regex" {
		t.Errorf("expected regex extraction, got %q", method)
	}
	if result.Cost != 0 {
		t.Errorf("expected zero cost for regex path, got %.4f", result.Cost)
	}
}

func TestContractExtractNodeRunner_SmokeRealisticJudgeOutput(t *testing.T) {
	runner := contractExtractTestRunner(t)
	// Realistic judge output — the winning response is returned directly
	upstreamOutput := `The question asks about the capital of France. Let me analyze the options:
A) London - This is the capital of the United Kingdom
B) Paris - This is the capital of France
C) Berlin - This is the capital of Germany
D) Madrid - This is the capital of Spain

Final answer: B
Reasoning: Paris is the capital of France.`

	sc := contractExtractContext("child-reasoning-judge-pick", upstreamOutput, benchmarkExtractionPatterns())
	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "B" {
		t.Errorf("expected B, got %q", result.Output)
	}
}

func TestContractExtractNodeRunner_SmokeBaselineSolverOutput(t *testing.T) {
	runner := contractExtractTestRunner(t)
	// Baseline solver (single-model) output
	upstreamOutput := `Let me think through this step by step.

The question asks which element has atomic number 6. Looking at the periodic table:
- A) Nitrogen has atomic number 7
- B) Oxygen has atomic number 8
- C) Carbon has atomic number 6
- D) Boron has atomic number 5

Final answer: C
Reasoning: Carbon has atomic number 6.`

	sc := contractExtractContext("agent-solver", upstreamOutput, benchmarkExtractionPatterns())
	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "C" {
		t.Errorf("expected C, got %q", result.Output)
	}
}

func TestContractExtractNodeRunner_SmokeAmbiguousOutput_LLMFallback(t *testing.T) {
	runner := contractExtractTestRunner(t)
	// Output where the model didn't follow the format — needs LLM repair
	upstreamOutput := `Based on my analysis, I believe the correct response is the second option because
it provides the most comprehensive explanation of the phenomenon. The reasoning is
that thermodynamic principles support this conclusion.`

	sc := contractExtractContext("child-reasoning-synthesis", upstreamOutput, benchmarkExtractionPatterns())
	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	method, _ := result.Metadata["extraction_method"].(string)
	if method != "llm_fallback" {
		t.Errorf("ambiguous output should trigger LLM fallback, got extraction_method=%q", method)
	}
}

func TestContractExtractNodeRunner_SmokeBareLetterOutput(t *testing.T) {
	runner := contractExtractTestRunner(t)
	// Some aggregation methods produce a clean single letter
	sc := contractExtractContext("child-vote", "A", benchmarkExtractionPatterns())
	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "A" {
		t.Errorf("expected A, got %q", result.Output)
	}
	if method, _ := result.Metadata["extraction_method"].(string); method != "regex" {
		t.Errorf("bare letter should match exact pattern, got extraction_method=%q", method)
	}
}

// --- Metadata preservation ---

func TestContractExtractNodeRunner_SourceVariableInResultMetadata(t *testing.T) {
	runner := contractExtractTestRunner(t)
	sc := contractExtractContext("my-source-node", "Final answer: D", benchmarkExtractionPatterns())
	result, err := runner.Execute(withStrictNodeContextDefaults(sc))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sv, _ := result.Metadata["source_variable"].(string)
	if sv != "my-source-node" {
		t.Errorf("source_variable in metadata = %q, want 'my-source-node'", sv)
	}
}

// --- Test helpers ---

func contractExtractTestRunner(t *testing.T) *ContractExtractNodeRunner {
	t.Helper()
	registry := newMockRegistry()
	client := NewMockLLMClient(registry.registry)
	return &ContractExtractNodeRunner{
		llmRunner: &LLMCallNodeRunner{llmClient: client},
	}
}

func contractExtractContext(sourceVar, sourceValue string, patterns interface{}) *NodeContext {
	return &NodeContext{
		Ctx:    context.Background(),
		NodeID: "agent-contract",
		Node: &Node{
			Type:   NodeTypeContractExtract,
			Model:  "mock-model",
			Prompt: fmt.Sprintf("Extract from {{%s}}", sourceVar),
			Metadata: map[string]interface{}{
				"source_variable":     sourceVar,
				"extraction_patterns": patterns,
			},
		},
		WorkflowContext: map[string]interface{}{sourceVar: sourceValue},
	}
}

// benchmarkExtractionPatterns returns the 3 patterns used in all benchmark seeds.
func benchmarkExtractionPatterns() []interface{} {
	return []interface{}{
		`^\s*([A-Za-z])\s*$`,
		`(?i)\b(?:final\s+answer|answer)\b(?:\s+is)?\s*[:\-]?\s*\(?\s*([A-Za-z])\s*\)?\s*(?:$|\n|[^A-Za-z])`,
		`^\s*\(?\s*([A-Za-z])\s*\)?[\.\):\-]`,
	}
}

func mustCompile(t *testing.T, pattern string) *regexp.Regexp {
	t.Helper()
	re, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatalf("invalid regex %q: %v", pattern, err)
	}
	return re
}

func assertNonRetryableError(t *testing.T, err error) {
	t.Helper()
	re, ok := err.(*RetryableError)
	if !ok || re.Retryable {
		t.Fatalf("expected non-retryable RetryableError, got %T", err)
	}
}
