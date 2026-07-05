package bench

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	FailureReasonToolRuntimeFailure = "tool/runtime_failure"
	FailureReasonProviderFailure    = "provider_failure"
	FailureReasonBenchmarkPaused    = "benchmark_paused"
	FailureReasonEmptyFinalOutput   = "empty_final_output"
	FailureReasonContractTruncated  = "contract_truncated"
	FailureReasonInvalidContract    = "invalid_contract_output"
	FailureReasonMultipleLetters    = "multiple_letters"
	FailureReasonConflictingAnswer  = "conflicting_answer"
	FailureReasonNoLetter           = "no_letter"
	FailureReasonOtherParseError    = "other_parse_error"

	OutputSourceBenchmarkAnswer        = "benchmark_answer"
	OutputSourceFinalOutput            = "final_output"
	OutputSourceFinalOutputUngraded    = "final_output_ungraded"
	OutputSourceMissingBenchmarkAnswer = "missing_benchmark_answer"
	OutputSourceNone                   = "none"
)

type FailureClassificationInput struct {
	Err              string
	CanonicalOutput  string
	CanonicalPresent bool
	ChoiceCount      int
	Outputs          map[string]interface{}
}

type FailureClassification struct {
	Reason    string
	Predicted string
}

func FailureReasonPrecedence() []string {
	return []string{
		FailureReasonToolRuntimeFailure,
		FailureReasonProviderFailure,
		FailureReasonBenchmarkPaused,
		FailureReasonEmptyFinalOutput,
		FailureReasonContractTruncated,
		FailureReasonInvalidContract,
		FailureReasonMultipleLetters,
		FailureReasonConflictingAnswer,
		FailureReasonNoLetter,
		FailureReasonOtherParseError,
	}
}

// ExtractCanonicalBenchmarkAnswer returns the canonical benchmark output,
// plus metadata describing where output was observed.
// For structured-output (JSON schema) variants, it unwraps {"answer": "X"}
// to extract the inner value before returning.
func ExtractCanonicalBenchmarkAnswer(outputs map[string]interface{}, finalOutput string) (string, string, bool) {
	if outputs != nil {
		if raw, ok := outputs["benchmark_answer"]; ok {
			value := strings.TrimSpace(fmt.Sprintf("%v", raw))
			if extracted, ok := extractAnswerFromJSON(value); ok {
				value = extracted
			}
			return value, OutputSourceBenchmarkAnswer, true
		}
	}

	if strings.TrimSpace(finalOutput) != "" {
		return "", OutputSourceFinalOutputUngraded, false
	}
	return "", OutputSourceMissingBenchmarkAnswer, false
}

// ExtractCanonicalOutputForFormat returns canonical output based on benchmark format.
// MCQA requires benchmark_answer; math-answer benchmarks allow final_output fallback.
func ExtractCanonicalOutputForFormat(format BenchmarkFormat, outputs map[string]interface{}, finalOutput string) (string, string, bool) {
	if format != BenchmarkFormatMathAnswer {
		return ExtractCanonicalBenchmarkAnswer(outputs, finalOutput)
	}

	if outputs != nil {
		if raw, ok := outputs["benchmark_answer"]; ok {
			value := strings.TrimSpace(fmt.Sprintf("%v", raw))
			if extracted, ok := extractAnswerFromJSON(value); ok {
				value = extracted
			}
			if value != "" {
				return value, OutputSourceBenchmarkAnswer, true
			}
		}
	}

	value := strings.TrimSpace(finalOutput)
	if value == "" {
		return "", OutputSourceMissingBenchmarkAnswer, false
	}
	if extracted, ok := extractAnswerFromJSON(value); ok {
		value = extracted
	}
	return value, OutputSourceFinalOutput, true
}

// extractAnswerFromJSON attempts to parse raw as a JSON object and extract
// the "answer" field. Returns the extracted value and true on success.
// This supports structured-output variants where the model returns
// {"answer": "B"} instead of a bare letter.
func extractAnswerFromJSON(raw string) (string, bool) {
	if len(raw) == 0 || raw[0] != '{' {
		return "", false
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return "", false
	}
	answer, ok := obj["answer"]
	if !ok {
		return "", false
	}
	return strings.TrimSpace(fmt.Sprintf("%v", answer)), true
}

func ClassifyFailure(input FailureClassificationInput) FailureClassification {
	trimmed := strings.TrimSpace(input.CanonicalOutput)
	predictedAny, _ := NormalizeBenchmarkAnswerForChoices(trimmed, 0)

	if reason := classifyExecutionError(input.Err); reason != "" {
		return FailureClassification{Reason: reason}
	}

	if !input.CanonicalPresent || trimmed == "" {
		return FailureClassification{
			Reason:    FailureReasonEmptyFinalOutput,
			Predicted: predictedAny,
		}
	}

	normalized, ok := NormalizeBenchmarkAnswerForChoices(trimmed, input.ChoiceCount)
	if !ok {
		return FailureClassification{
			Reason:    FailureReasonInvalidContract,
			Predicted: predictedAny,
		}
	}

	if hasConflictingDeclaredAnswer(normalized, input.Outputs, input.ChoiceCount) {
		return FailureClassification{
			Reason:    FailureReasonConflictingAnswer,
			Predicted: normalized,
		}
	}

	return FailureClassification{
		Reason:    "",
		Predicted: normalized,
	}
}

func classifyExecutionError(errMsg string) string {
	if strings.TrimSpace(errMsg) == "" {
		return ""
	}
	if isRuntimeFailure(errMsg) {
		return FailureReasonToolRuntimeFailure
	}
	if isProviderFailure(errMsg) {
		return FailureReasonProviderFailure
	}
	return FailureReasonToolRuntimeFailure
}

func isRuntimeFailure(errMsg string) bool {
	lower := strings.ToLower(errMsg)
	runtimeIndicators := []string{
		"database is locked",
		"sqlite_busy",
		"failed to create job",
		"failed to create execution",
		"failed to get execution",
		"failed to check active durable backlog",
		"poll job",
		"wait for child workflow",
		"failed to convert workflow",
		"workflow execution failed",
		"nil execution result",
		"marshal workflow",
		"create request",
		"parse response",
		"read response",
		"execute workflow: post http://localhost",
		"backend not reachable",
	}
	for _, marker := range runtimeIndicators {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func isProviderFailure(errMsg string) bool {
	lower := strings.ToLower(errMsg)
	providerIndicators := []string{
		"openrouter",
		"provider_no_choices",
		"no choices returned",
		"provider returned error",
		"provider error",
		"llm call failed",
		"synthesis llm call failed",
		"rate limit",
		"upstream timeout",
		"status 429",
		"status 502",
		"status 503",
		"status 504",
		"auth error",
		"authentication",
		"invalid api key",
	}
	for _, marker := range providerIndicators {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func hasConflictingDeclaredAnswer(canonicalLabel string, outputs map[string]interface{}, choiceCount int) bool {
	if outputs == nil {
		return false
	}

	contractKeys := []string{
		"answer",
		"final_answer",
		"finalAnswer",
		"choice",
		"selected_choice",
		"selected_answer",
		"benchmark_choice",
	}

	for _, key := range contractKeys {
		raw, exists := outputs[key]
		if !exists {
			continue
		}
		value := strings.TrimSpace(fmt.Sprintf("%v", raw))
		if value == "" {
			continue
		}

		otherLabel, ok := NormalizeBenchmarkAnswerForChoices(value, choiceCount)
		if !ok {
			continue
		}
		if !strings.EqualFold(otherLabel, canonicalLabel) {
			return true
		}
	}

	return false
}
