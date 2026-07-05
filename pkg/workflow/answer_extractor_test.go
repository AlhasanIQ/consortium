package workflow

import "testing"

func TestExtractAnswerRegexDefault(t *testing.T) {
	cfg := DefaultExtractorConfig()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"final answer format", "I think the answer is clear.\nFinal answer: B", "B"},
		{"answer colon", "Answer: C", "C"},
		{"case insensitive", "final answer: d", "D"},
		{"no match", "I'm not sure about the answer", ""},
		{"empty", "", ""},
		{"answer with colon", "The correct answer: A because...", "A"},
		{"answer is", "The correct answer is A because...", "A"},
		{"answer is colon", "The answer is: B", "B"},
		{"answer dash", "Answer - C", "C"},
		{"answer parens", "Final answer: (D)", "D"},
		{"answer is ambiguous (no match)", "The answer is ambiguous", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractAnswer(tt.input, cfg)
			if got != tt.want {
				t.Errorf("ExtractAnswer(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractAnswerFirstLetter(t *testing.T) {
	cfg := ExtractorConfig{Strategy: ExtractFirstLetter}

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"starts with uppercase", "B is the answer", "B"},
		{"starts with lowercase", "the answer is D clearly", "D"},
		{"no uppercase", "no answer here", ""},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractAnswer(tt.input, cfg)
			if got != tt.want {
				t.Errorf("ExtractAnswer(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractAnswerJSONField(t *testing.T) {
	cfg := ExtractorConfig{Strategy: ExtractJSONField, Pattern: "answer"}

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple json", `{"answer": "B"}`, "B"},
		{"json with extra fields", `{"reasoning": "because...", "answer": "C"}`, "C"},
		{"json in markdown", "```json\n{\"answer\": \"A\"}\n```", "A"},
		{"numeric value", `{"answer": 42}`, "42"},
		{"no json", "just plain text", ""},
		{"wrong field", `{"response": "B"}`, ""},
		{"default field name", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractAnswer(tt.input, cfg)
			if got != tt.want {
				t.Errorf("ExtractAnswer(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractAnswerCustomRegex(t *testing.T) {
	cfg := ExtractorConfig{
		Strategy: ExtractRegex,
		Pattern:  `(?i)choice:\s*([A-D])`,
	}

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"match", "Choice: B", "B"},
		{"no match", "Answer: B", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractAnswer(tt.input, cfg)
			if got != tt.want {
				t.Errorf("ExtractAnswer(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractAnswerRegexUsesFirstNonEmptyCapture(t *testing.T) {
	cfg := ExtractorConfig{
		Strategy: ExtractRegex,
		Pattern:  `(?i)answer:\s*(?:\(?([A-Z])\)?\s+is\b|([^\n]{1,80}?)(?:[.;]\s*$|$|\n))`,
	}

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "letter branch", input: "Answer: A is correct.", want: "A"},
		{name: "free form branch", input: "Answer: St. Louis.", want: "ST. LOUIS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractAnswer(tt.input, cfg)
			if got != tt.want {
				t.Errorf("ExtractAnswer(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseExtractorConfig(t *testing.T) {
	// Empty config: no implicit defaults.
	cfg := ParseExtractorConfig(map[string]interface{}{})
	if cfg.Strategy != "" {
		t.Errorf("Expected empty strategy for missing config, got %q", cfg.Strategy)
	}

	// Custom strategy
	cfg = ParseExtractorConfig(map[string]interface{}{
		"extraction_strategy": "first_letter",
	})
	if cfg.Strategy != ExtractFirstLetter {
		t.Errorf("Expected first_letter strategy, got %q", cfg.Strategy)
	}

	// Custom pattern
	cfg = ParseExtractorConfig(map[string]interface{}{
		"extraction_pattern": `answer:\s*(\w+)`,
	})
	if cfg.Pattern != `answer:\s*(\w+)` {
		t.Errorf("Expected custom pattern, got %q", cfg.Pattern)
	}
}

func TestComputeAgreementUnanimous(t *testing.T) {
	inputs := []AgentOutput{
		{AgentID: "a1", Output: "Final answer: B"},
		{AgentID: "a2", Output: "The answer is B"},
		{AgentID: "a3", Output: "Answer: B"},
	}
	cfg := DefaultExtractorConfig()

	ratio, consensus, dissenting := ComputeAgreement(inputs, cfg)
	if ratio != 1.0 {
		t.Errorf("Expected ratio 1.0, got %f", ratio)
	}
	if consensus != "B" {
		t.Errorf("Expected consensus B, got %q", consensus)
	}
	if len(dissenting) != 0 {
		t.Errorf("Expected no dissenting, got %v", dissenting)
	}
}

func TestComputeAgreementMajority(t *testing.T) {
	inputs := []AgentOutput{
		{AgentID: "a1", Output: "Final answer: A"},
		{AgentID: "a2", Output: "Final answer: B"},
		{AgentID: "a3", Output: "Final answer: A"},
	}
	cfg := DefaultExtractorConfig()

	ratio, consensus, dissenting := ComputeAgreement(inputs, cfg)
	if ratio < 0.66 || ratio > 0.67 {
		t.Errorf("Expected ratio ~0.67, got %f", ratio)
	}
	if consensus != "A" {
		t.Errorf("Expected consensus A, got %q", consensus)
	}
	if len(dissenting) != 1 || dissenting[0] != "a2" {
		t.Errorf("Expected dissenting [a2], got %v", dissenting)
	}
}

func TestComputeAgreementNoExtraction(t *testing.T) {
	inputs := []AgentOutput{
		{AgentID: "a1", Output: "I have no idea"},
		{AgentID: "a2", Output: "Cannot determine"},
	}
	cfg := DefaultExtractorConfig()

	ratio, consensus, dissenting := ComputeAgreement(inputs, cfg)
	if ratio != 0 {
		t.Errorf("Expected ratio 0, got %f", ratio)
	}
	if consensus != "" {
		t.Errorf("Expected empty consensus, got %q", consensus)
	}
	if dissenting != nil {
		t.Errorf("Expected nil dissenting, got %v", dissenting)
	}
}

func TestComputeAgreementAllDifferent(t *testing.T) {
	inputs := []AgentOutput{
		{AgentID: "a1", Output: "Final answer: A"},
		{AgentID: "a2", Output: "Final answer: B"},
		{AgentID: "a3", Output: "Final answer: C"},
	}
	cfg := DefaultExtractorConfig()

	ratio, consensus, dissenting := ComputeAgreement(inputs, cfg)

	// Each answer has 1/3 votes — plurality picks one, ratio = 1/3
	if ratio < 0.33 || ratio > 0.34 {
		t.Errorf("Expected ratio ~0.33 (no majority), got %f", ratio)
	}
	// Deterministic tie-break: alphabetical order picks "A" as consensus
	if consensus != "A" {
		t.Errorf("Expected deterministic consensus 'A' (alphabetical tie-break), got %q", consensus)
	}
	if len(dissenting) != 2 {
		t.Errorf("Expected 2 dissenting agents, got %d: %v", len(dissenting), dissenting)
	}
}

func TestComputeAgreementPartialExtraction(t *testing.T) {
	inputs := []AgentOutput{
		{AgentID: "a1", Output: "Final answer: B"},
		{AgentID: "a2", Output: "I have no idea"},
		{AgentID: "a3", Output: "Final answer: B"},
	}
	cfg := DefaultExtractorConfig()

	ratio, consensus, dissenting := ComputeAgreement(inputs, cfg)

	// Only 2 out of 3 are extractable, both say B → ratio = 2/2 = 1.0
	if ratio != 1.0 {
		t.Errorf("Expected ratio 1.0 (both extracted agree), got %f", ratio)
	}
	if consensus != "B" {
		t.Errorf("Expected consensus B, got %q", consensus)
	}
	if len(dissenting) != 0 {
		t.Errorf("Expected no dissenting (unextractable agents not counted), got %v", dissenting)
	}
}

func TestComputeAgreementEmpty(t *testing.T) {
	ratio, consensus, dissenting := ComputeAgreement(nil, DefaultExtractorConfig())
	if ratio != 0 {
		t.Errorf("Expected ratio 0, got %f", ratio)
	}
	if consensus != "" {
		t.Errorf("Expected empty consensus, got %q", consensus)
	}
	if dissenting != nil {
		t.Errorf("Expected nil dissenting, got %v", dissenting)
	}
}
