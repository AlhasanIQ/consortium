package bench

import "testing"

func TestExtractCanonicalBenchmarkAnswer(t *testing.T) {
	tests := []struct {
		name          string
		outputs       map[string]interface{}
		finalOutput   string
		wantOutput    string
		wantSource    string
		wantCanonical bool
	}{
		{
			name:          "canonical output present",
			outputs:       map[string]interface{}{"benchmark_answer": "  B  "},
			finalOutput:   "ignored",
			wantOutput:    "B",
			wantSource:    OutputSourceBenchmarkAnswer,
			wantCanonical: true,
		},
		{
			name:          "canonical output key present but empty",
			outputs:       map[string]interface{}{"benchmark_answer": ""},
			finalOutput:   "fallback should not be used",
			wantOutput:    "",
			wantSource:    OutputSourceBenchmarkAnswer,
			wantCanonical: true,
		},
		{
			name:          "canonical output missing but final output present",
			outputs:       map[string]interface{}{"other": "A"},
			finalOutput:   "A",
			wantOutput:    "",
			wantSource:    OutputSourceFinalOutputUngraded,
			wantCanonical: false,
		},
		{
			name:          "no outputs",
			outputs:       nil,
			finalOutput:   "",
			wantOutput:    "",
			wantSource:    OutputSourceMissingBenchmarkAnswer,
			wantCanonical: false,
		},
		{
			name:          "json schema variant extracts answer field",
			outputs:       map[string]interface{}{"benchmark_answer": `{"answer": "B"}`},
			finalOutput:   "",
			wantOutput:    "B",
			wantSource:    OutputSourceBenchmarkAnswer,
			wantCanonical: true,
		},
		{
			name:          "json schema variant trims and extracts answer",
			outputs:       map[string]interface{}{"benchmark_answer": `{"answer": " C "}`},
			finalOutput:   "",
			wantOutput:    "C",
			wantSource:    OutputSourceBenchmarkAnswer,
			wantCanonical: true,
		},
		{
			name:          "json schema variant with empty answer field",
			outputs:       map[string]interface{}{"benchmark_answer": `{"answer": ""}`},
			finalOutput:   "",
			wantOutput:    "",
			wantSource:    OutputSourceBenchmarkAnswer,
			wantCanonical: true,
		},
		{
			name:          "json without answer field falls through as raw",
			outputs:       map[string]interface{}{"benchmark_answer": `{"other": "B"}`},
			finalOutput:   "",
			wantOutput:    `{"other": "B"}`,
			wantSource:    OutputSourceBenchmarkAnswer,
			wantCanonical: true,
		},
		{
			name:          "plain letter not affected by json extraction",
			outputs:       map[string]interface{}{"benchmark_answer": "D"},
			finalOutput:   "",
			wantOutput:    "D",
			wantSource:    OutputSourceBenchmarkAnswer,
			wantCanonical: true,
		},
		{
			name:          "invalid json falls through as raw",
			outputs:       map[string]interface{}{"benchmark_answer": "{broken"},
			finalOutput:   "",
			wantOutput:    "{broken",
			wantSource:    OutputSourceBenchmarkAnswer,
			wantCanonical: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOutput, gotSource, gotCanonical := ExtractCanonicalBenchmarkAnswer(tt.outputs, tt.finalOutput)
			if gotOutput != tt.wantOutput {
				t.Fatalf("expected output %q, got %q", tt.wantOutput, gotOutput)
			}
			if gotSource != tt.wantSource {
				t.Fatalf("expected source %q, got %q", tt.wantSource, gotSource)
			}
			if gotCanonical != tt.wantCanonical {
				t.Fatalf("expected canonical=%v, got %v", tt.wantCanonical, gotCanonical)
			}
		})
	}
}

func TestExtractCanonicalOutputForFormat(t *testing.T) {
	output, source, canonical := ExtractCanonicalOutputForFormat(
		BenchmarkFormatMathAnswer,
		map[string]interface{}{"other": "x"},
		"Final answer: 14/3",
	)
	if output != "Final answer: 14/3" {
		t.Fatalf("math output=%q, want %q", output, "Final answer: 14/3")
	}
	if source != OutputSourceFinalOutput {
		t.Fatalf("math source=%q, want %q", source, OutputSourceFinalOutput)
	}
	if !canonical {
		t.Fatal("math output should be canonical when final output exists")
	}

	output, source, canonical = ExtractCanonicalOutputForFormat(
		BenchmarkFormatMCQA,
		map[string]interface{}{"other": "x"},
		"B",
	)
	if output != "" || source != OutputSourceFinalOutputUngraded || canonical {
		t.Fatalf("mcqa fallback changed unexpectedly: output=%q source=%q canonical=%v", output, source, canonical)
	}
}

func TestExtractCanonicalOutputForFormatMathPrecedence(t *testing.T) {
	// When math format has both benchmark_answer and finalOutput, benchmark_answer wins
	output, source, canonical := ExtractCanonicalOutputForFormat(
		BenchmarkFormatMathAnswer,
		map[string]interface{}{"benchmark_answer": "42"},
		"Final answer: 99",
	)
	if output != "42" || source != OutputSourceBenchmarkAnswer || !canonical {
		t.Fatalf("expected benchmark_answer to take precedence: output=%q source=%q canonical=%v", output, source, canonical)
	}
}

func TestExtractCanonicalOutputForFormatMathEmptyBenchmarkAnswer(t *testing.T) {
	// When math format has empty benchmark_answer, falls back to finalOutput
	output, source, canonical := ExtractCanonicalOutputForFormat(
		BenchmarkFormatMathAnswer,
		map[string]interface{}{"benchmark_answer": ""},
		"Final answer: 14/3",
	)
	if output != "Final answer: 14/3" || source != OutputSourceFinalOutput || !canonical {
		t.Fatalf("expected fallback to finalOutput: output=%q source=%q canonical=%v", output, source, canonical)
	}
}

func TestClassifyFailurePrecedence(t *testing.T) {
	tests := []struct {
		name     string
		input    FailureClassificationInput
		want     string
		wantPred string
	}{
		{
			name: "runtime beats provider when both patterns appear",
			input: FailureClassificationInput{
				Err: "failed to create job record: database is locked; openrouter returned error",
			},
			want: FailureReasonToolRuntimeFailure,
		},
		{
			name: "provider failure",
			input: FailureClassificationInput{
				Err: "synthesis LLM call failed: OpenRouter API error (status 429)",
			},
			want: FailureReasonProviderFailure,
		},
		{
			name: "provider failure real rate-limit wait exceeds context deadline",
			input: FailureClassificationInput{
				Err: "aggregation failed: synthesis LLM call failed: rate limit exceeded: rate limit wait (541ms) exceeds context deadline (408ms remaining)",
			},
			want: FailureReasonProviderFailure,
		},
		{
			name: "provider failure real request context deadline exceeded",
			input: FailureClassificationInput{
				Err: "aggregation failed: synthesis LLM call failed: failed to make request: Post \"https://openrouter.ai/api/v1/chat/completions\": context deadline exceeded",
			},
			want: FailureReasonProviderFailure,
		},
		{
			name: "runtime failure real read response context deadline exceeded",
			input: FailureClassificationInput{
				Err: "aggregation failed: synthesis LLM call failed: failed to read response: context deadline exceeded",
			},
			want: FailureReasonToolRuntimeFailure,
		},
		{
			name: "runtime failure real child workflow failed",
			input: FailureClassificationInput{
				Err: "child workflow failed",
			},
			want: FailureReasonToolRuntimeFailure,
		},
		{
			name: "empty final output when canonical key missing",
			input: FailureClassificationInput{
				CanonicalPresent: false,
				CanonicalOutput:  "",
				ChoiceCount:      4,
			},
			want: FailureReasonEmptyFinalOutput,
		},
		{
			name: "invalid contract output out of range label",
			input: FailureClassificationInput{
				CanonicalPresent: true,
				CanonicalOutput:  "H",
				ChoiceCount:      4,
			},
			want:     FailureReasonInvalidContract,
			wantPred: "H",
		},
		{
			name: "multiple letters is invalid contract",
			input: FailureClassificationInput{
				CanonicalPresent: true,
				CanonicalOutput:  "A or B",
				ChoiceCount:      4,
			},
			want: FailureReasonInvalidContract,
		},
		{
			name: "conflicting answer",
			input: FailureClassificationInput{
				CanonicalPresent: true,
				CanonicalOutput:  "B",
				ChoiceCount:      4,
				Outputs: map[string]interface{}{
					"benchmark_answer": "B",
					"final_answer":     "C",
				},
			},
			want:     FailureReasonConflictingAnswer,
			wantPred: "B",
		},
		{
			name: "no letter is invalid contract",
			input: FailureClassificationInput{
				CanonicalPresent: true,
				CanonicalOutput:  "I have no idea",
				ChoiceCount:      4,
			},
			want: FailureReasonInvalidContract,
		},
		{
			name: "verbose sentence is invalid contract",
			input: FailureClassificationInput{
				CanonicalPresent: true,
				CanonicalOutput:  "I think B is likely.",
				ChoiceCount:      4,
			},
			want: FailureReasonInvalidContract,
		},
		{
			name: "strict single letter is success",
			input: FailureClassificationInput{
				CanonicalPresent: true,
				CanonicalOutput:  " c ",
				ChoiceCount:      4,
			},
			want:     "",
			wantPred: "C",
		},
		{
			name: "real failure from 8.2 missing canonical with final output present",
			input: FailureClassificationInput{
				CanonicalPresent: false,
				CanonicalOutput:  "",
				ChoiceCount:      4,
				Outputs: map[string]interface{}{
					"other": "D",
				},
			},
			want: FailureReasonEmptyFinalOutput,
		},
		{
			name: "real failure from durable projection with explicit empty benchmark_answer",
			input: FailureClassificationInput{
				CanonicalPresent: true,
				CanonicalOutput:  "",
				ChoiceCount:      4,
				Outputs: map[string]interface{}{
					"benchmark_answer": "",
				},
			},
			want: FailureReasonEmptyFinalOutput,
		},
		{
			name: "invalid contract real toolcall xml payload",
			input: FailureClassificationInput{
				CanonicalPresent: true,
				CanonicalOutput:  "<tool_call>\n<function=example_function_name>\n<parameter=example_parameter_1>value_1</parameter>\n<parameter=example_parameter_2>This is the value for the second parameter\nthat can span\nmultiple lines</parameter>\n</function>\n</tool_call>",
				ChoiceCount:      4,
				Outputs: map[string]interface{}{
					"benchmark_answer": "<tool_call>\n<function=example_function_name>\n<parameter=example_parameter_1>value_1</parameter>\n<parameter=example_parameter_2>This is the value for the second parameter\nthat can span\nmultiple lines</parameter>\n</function>\n</tool_call>",
				},
			},
			want: FailureReasonInvalidContract,
		},
		{
			name: "invalid contract real toolcall action json payload",
			input: FailureClassificationInput{
				CanonicalPresent: true,
				CanonicalOutput:  "{\n  \"action\": \"select_option_D\",\n  \"action_input\": {}\n}",
				ChoiceCount:      4,
				Outputs: map[string]interface{}{
					"benchmark_answer": "{\n  \"action\": \"select_option_D\",\n  \"action_input\": {}\n}",
				},
			},
			want: FailureReasonInvalidContract,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyFailure(tt.input)
			if got.Reason != tt.want {
				t.Fatalf("expected reason %q, got %q", tt.want, got.Reason)
			}
			if got.Predicted != tt.wantPred {
				t.Fatalf("expected predicted %q, got %q", tt.wantPred, got.Predicted)
			}
		})
	}
}
