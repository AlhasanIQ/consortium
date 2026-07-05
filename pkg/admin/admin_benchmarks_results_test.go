package admin

import (
	"strings"
	"testing"

	"github.com/alhasaniq/consortium/pkg/bench"
	"github.com/alhasaniq/consortium/pkg/jobs"
	"github.com/alhasaniq/consortium/pkg/workflow"
)

// --- benchmarkRunError ---

func TestBenchmarkRunErrorNoErrors(t *testing.T) {
	if err := benchmarkRunError(nil); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if err := benchmarkRunError([]string{}); err != nil {
		t.Fatalf("expected nil error for empty slice, got %v", err)
	}
}

func TestBenchmarkRunErrorWithErrors(t *testing.T) {
	err := benchmarkRunError([]string{"first error", "second error"})
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "2 save/upsert errors") {
		t.Fatalf("expected count in message, got %q", msg)
	}
	if !strings.Contains(msg, "first error") || !strings.Contains(msg, "second error") {
		t.Fatalf("expected both errors in message, got %q", msg)
	}
}

// --- benchmarkRunnerError ---

func TestBenchmarkRunnerErrorRunError(t *testing.T) {
	guard := &benchmarkFatalGuard{repeatThreshold: 3}
	outcome := benchmarkRunOutcome{CancelEffective: true}
	result := benchmarkRunnerError(testErr("run failed"), guard, outcome)
	if result != "run failed" {
		t.Fatalf("expected 'run failed', got %q", result)
	}
}

func TestBenchmarkRunnerErrorFatalGuardTriggered(t *testing.T) {
	guard := &benchmarkFatalGuard{
		repeatThreshold: 3,
		signatureCounts: make(map[string]int),
	}
	// Trigger the guard with a hard fatal cause so snapshotMessage() returns a real message.
	guard.note(benchmarkFatalCause{
		Code:      "AUTH_ERROR",
		Reason:    "auth_or_access_denied",
		Message:   "invalid api key",
		Signature: "AUTH_ERROR",
		Hard:      true,
	})

	outcome := benchmarkRunOutcome{FatalGuardTriggered: true, CancelEffective: true}
	result := benchmarkRunnerError(nil, guard, outcome)
	if result == "" {
		t.Fatal("expected non-empty guard message")
	}
	if !strings.Contains(result, "AUTH_ERROR") {
		t.Errorf("expected guard message to contain AUTH_ERROR, got %q", result)
	}
}

func TestBenchmarkRunnerErrorCancelEffective(t *testing.T) {
	guard := &benchmarkFatalGuard{repeatThreshold: 3}
	outcome := benchmarkRunOutcome{CancelEffective: true}
	result := benchmarkRunnerError(nil, guard, outcome)
	if result != "cancelled by user" {
		t.Fatalf("expected 'cancelled by user', got %q", result)
	}
}

func TestBenchmarkRunnerErrorNormalCompletion(t *testing.T) {
	guard := &benchmarkFatalGuard{repeatThreshold: 3}
	outcome := benchmarkRunOutcome{}
	result := benchmarkRunnerError(nil, guard, outcome)
	if result != "" {
		t.Fatalf("expected empty string, got %q", result)
	}
}

// --- shouldRetryBenchmarkContractFailure ---

func TestShouldRetryBenchmarkContractFailure(t *testing.T) {
	tests := []struct {
		reason string
		want   bool
	}{
		{bench.FailureReasonEmptyFinalOutput, true},
		{bench.FailureReasonInvalidContract, true},
		{bench.FailureReasonContractTruncated, true},
		{bench.FailureReasonProviderFailure, false},
		{bench.FailureReasonBenchmarkPaused, false},
		{"", false},
	}
	for _, tt := range tests {
		if got := shouldRetryBenchmarkContractFailure(tt.reason); got != tt.want {
			t.Errorf("shouldRetryBenchmarkContractFailure(%q) = %v, want %v", tt.reason, got, tt.want)
		}
	}
}

// --- shouldRetryTransientExecutionFailure ---

func TestShouldRetryTransientExecutionFailureProviderFailure(t *testing.T) {
	if !shouldRetryTransientExecutionFailure(bench.FailureReasonProviderFailure, "some error", "") {
		t.Error("expected retry for provider failure")
	}
}

func TestShouldRetryTransientExecutionFailureTransientMessages(t *testing.T) {
	transientMessages := []string{
		"context deadline exceeded",
		"request timed out",
		"failed to read response",
		"stream error occurred",
		"database is locked",
		"connection reset by peer",
		"broken pipe",
		"EOF",
	}
	for _, msg := range transientMessages {
		if !shouldRetryTransientExecutionFailure(bench.FailureReasonToolRuntimeFailure, msg, "") {
			t.Errorf("expected retry for tool_runtime_failure with message %q", msg)
		}
	}
}

func TestShouldRetryTransientExecutionFailureNonTransientMessage(t *testing.T) {
	if shouldRetryTransientExecutionFailure(bench.FailureReasonToolRuntimeFailure, "model not found", "") {
		t.Error("expected no retry for non-transient tool runtime failure")
	}
}

func TestShouldRetryTransientExecutionFailureRetryableErrorCode(t *testing.T) {
	// RATE_LIMIT and TIMEOUT are retryable per workflowruntime.IsRetryableCode
	if !shouldRetryTransientExecutionFailure("", "some error", "RATE_LIMIT") {
		t.Error("expected retry for RATE_LIMIT error code")
	}
	if !shouldRetryTransientExecutionFailure("", "some error", "TIMEOUT") {
		t.Error("expected retry for TIMEOUT error code")
	}
}

func TestShouldRetryTransientExecutionFailureOtherReasons(t *testing.T) {
	if shouldRetryTransientExecutionFailure(bench.FailureReasonBenchmarkPaused, "paused", "") {
		t.Error("expected no retry for paused reason")
	}
	if shouldRetryTransientExecutionFailure(bench.FailureReasonEmptyFinalOutput, "empty", "") {
		t.Error("expected no retry for empty output reason")
	}
}

// --- shouldRetryAdmissionExhaustion ---

func TestShouldRetryAdmissionExhaustionErrorCode(t *testing.T) {
	result := &jobs.WorkflowExecutionResult{ErrorCode: "POOL_EXHAUSTED"}
	if !shouldRetryAdmissionExhaustion(result, "") {
		t.Error("expected retry for POOL_EXHAUSTED error code")
	}
}

func TestShouldRetryAdmissionExhaustionErrMsg(t *testing.T) {
	if !shouldRetryAdmissionExhaustion(nil, "admission pool exhausted") {
		t.Error("expected retry for admission pool exhausted message")
	}
	if !shouldRetryAdmissionExhaustion(nil, "server at capacity") {
		t.Error("expected retry for server at capacity message")
	}
}

func TestShouldRetryAdmissionExhaustionNegativeCases(t *testing.T) {
	if shouldRetryAdmissionExhaustion(nil, "some random error") {
		t.Error("expected no retry for random error")
	}
	result := &jobs.WorkflowExecutionResult{ErrorCode: "AUTH_ERROR"}
	if shouldRetryAdmissionExhaustion(result, "") {
		t.Error("expected no retry for auth error code")
	}
}

// --- classifyMathAttemptOutput ---

func TestClassifyMathAttemptOutputValidMath(t *testing.T) {
	result := classifyMathAttemptOutput(`\frac{14}{3}`, true)
	if result.Reason != "" {
		t.Errorf("expected no failure reason for valid math output, got %q", result.Reason)
	}
	if result.Predicted == "" {
		t.Error("expected non-empty predicted for valid math output")
	}
}

func TestClassifyMathAttemptOutputEmptyOutput(t *testing.T) {
	result := classifyMathAttemptOutput("", true)
	if result.Reason != bench.FailureReasonEmptyFinalOutput {
		t.Errorf("expected empty_final_output, got %q", result.Reason)
	}
}

func TestClassifyMathAttemptOutputNotPresent(t *testing.T) {
	result := classifyMathAttemptOutput("42", false)
	if result.Reason != bench.FailureReasonEmptyFinalOutput {
		t.Errorf("expected empty_final_output when not present, got %q", result.Reason)
	}
}

func TestClassifyMathAttemptOutputWhitespaceOnly(t *testing.T) {
	result := classifyMathAttemptOutput("   ", true)
	if result.Reason != bench.FailureReasonEmptyFinalOutput {
		t.Errorf("expected empty_final_output for whitespace-only output, got %q", result.Reason)
	}
}

// --- benchmarkExecutionFailureMessage ---

func TestBenchmarkExecutionFailureMessageNilResult(t *testing.T) {
	msg := benchmarkExecutionFailureMessage(nil)
	if msg != "workflow execution failed" {
		t.Errorf("expected default message for nil result, got %q", msg)
	}
}

func TestBenchmarkExecutionFailureMessageWithError(t *testing.T) {
	result := &jobs.WorkflowExecutionResult{Error: "model not available"}
	msg := benchmarkExecutionFailureMessage(result)
	if msg != "model not available" {
		t.Errorf("expected error message, got %q", msg)
	}
}

func TestBenchmarkExecutionFailureMessageWithCodeAndError(t *testing.T) {
	result := &jobs.WorkflowExecutionResult{Error: "some provider failure", ErrorCode: "PROVIDER_FAIL"}
	msg := benchmarkExecutionFailureMessage(result)
	if !strings.HasPrefix(msg, "PROVIDER_FAIL:") {
		t.Errorf("expected code prefix, got %q", msg)
	}
}

func TestBenchmarkExecutionFailureMessageCodeAlreadyInError(t *testing.T) {
	// If the code is already embedded in the error string, don't prepend it again
	result := &jobs.WorkflowExecutionResult{Error: "PROVIDER_FAIL: something went wrong", ErrorCode: "PROVIDER_FAIL"}
	msg := benchmarkExecutionFailureMessage(result)
	// Should not double-prepend
	if strings.Count(msg, "PROVIDER_FAIL") > 1 {
		t.Errorf("expected code not to be duplicated, got %q", msg)
	}
}

func TestBenchmarkExecutionFailureMessageEmptyError(t *testing.T) {
	result := &jobs.WorkflowExecutionResult{Error: "   ", ErrorCode: ""}
	msg := benchmarkExecutionFailureMessage(result)
	if msg != "workflow execution failed" {
		t.Errorf("expected default for empty error string, got %q", msg)
	}
}

// --- buildOptionTools ---

func TestBuildOptionToolsBasic(t *testing.T) {
	choices := []string{"Paris", "London", "Berlin", "Madrid"}
	tools := buildOptionTools(choices)
	if len(tools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(tools))
	}
	first := tools[0]
	if first["type"] != "function" {
		t.Errorf("expected type=function, got %v", first["type"])
	}
	fn, ok := first["function"].(map[string]interface{})
	if !ok {
		t.Fatal("expected function field to be a map")
	}
	if fn["name"] != "choose_a" {
		t.Errorf("expected name=choose_a, got %v", fn["name"])
	}
	if fn["strict"] != true {
		t.Errorf("expected strict=true")
	}
}

func TestBuildOptionToolsEmpty(t *testing.T) {
	tools := buildOptionTools(nil)
	if len(tools) != 0 {
		t.Fatalf("expected 0 tools for nil choices, got %d", len(tools))
	}
}

func TestBuildOptionToolsDescriptionWithChoiceText(t *testing.T) {
	choices := []string{"The Eiffel Tower"}
	tools := buildOptionTools(choices)
	fn := tools[0]["function"].(map[string]interface{})
	desc, _ := fn["description"].(string)
	if !strings.Contains(desc, "The Eiffel Tower") {
		t.Errorf("expected choice text in description, got %q", desc)
	}
}

func TestBuildOptionToolsEmptyChoiceText(t *testing.T) {
	choices := []string{"   "}
	tools := buildOptionTools(choices)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool for empty choice text, got %d", len(tools))
	}
	fn := tools[0]["function"].(map[string]interface{})
	desc, _ := fn["description"].(string)
	// Empty choice text falls back to generic description
	if !strings.Contains(desc, "option A") {
		t.Errorf("expected generic description for empty choice text, got %q", desc)
	}
}

// --- applyPerOptionToolcallContract ---

func TestApplyPerOptionToolcallContractNilWorkflow(t *testing.T) {
	err := applyPerOptionToolcallContract(nil, bench.DatasetItem{})
	if err != nil {
		t.Fatalf("expected no error for nil workflow, got %v", err)
	}
}

func TestApplyPerOptionToolcallContractInjectsTools(t *testing.T) {
	temp := 0.0
	wf := &workflow.Workflow{
		Nodes: []*workflow.Node{
			{
				ID:          "llm-node",
				Type:        workflow.NodeTypePrompt,
				Temperature: &temp,
				Metadata:    map[string]interface{}{"tool_choice": "auto"},
			},
		},
	}
	item := bench.DatasetItem{
		Choices: []string{"Paris", "London", "Berlin", "Madrid"},
	}
	err := applyPerOptionToolcallContract(wf, item)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tools, ok := wf.Nodes[0].Metadata["tools"]
	if !ok {
		t.Fatal("expected tools to be injected")
	}
	toolList, ok := tools.([]map[string]interface{})
	if !ok || len(toolList) != 4 {
		t.Fatalf("expected 4 tools, got %v", tools)
	}
}

func TestApplyPerOptionToolcallContractSkipsNoToolChoice(t *testing.T) {
	// Node without tool_choice should not have tools injected
	wf := &workflow.Workflow{
		Nodes: []*workflow.Node{
			{
				ID:       "llm-node",
				Type:     workflow.NodeTypePrompt,
				Metadata: map[string]interface{}{"some_other_key": "value"},
			},
		},
	}
	item := bench.DatasetItem{Choices: []string{"A", "B"}}
	err := applyPerOptionToolcallContract(wf, item)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := wf.Nodes[0].Metadata["tools"]; ok {
		t.Error("expected no tools to be injected for node without tool_choice")
	}
}

func TestApplyPerOptionToolcallContractErrorOnNoChoices(t *testing.T) {
	wf := &workflow.Workflow{
		Nodes: []*workflow.Node{
			{
				ID:       "llm-node",
				Type:     workflow.NodeTypePrompt,
				Metadata: map[string]interface{}{"tool_choice": "auto"},
			},
		},
	}
	// Item with no choices → error
	item := bench.DatasetItem{Choices: nil}
	err := applyPerOptionToolcallContract(wf, item)
	if err == nil {
		t.Fatal("expected error when tool_choice node has no choices in item")
	}
}

// --- extractContractNodeDiagnostics ---

func TestExtractContractNodeDiagnosticsFound(t *testing.T) {
	nodes := []*workflow.NodeResult{
		{
			NodeID: "contract-node",
			Metadata: map[string]interface{}{
				"model":             "gpt-4o",
				"finish_reason":     "stop",
				"max_tokens":        1024,
				"extraction_method": "tool_call",
			},
			TokensOutput: 150,
		},
	}
	diag := extractContractNodeDiagnostics(nodes)
	if diag.NodeID != "contract-node" {
		t.Errorf("expected NodeID=contract-node, got %q", diag.NodeID)
	}
	if diag.Model != "gpt-4o" {
		t.Errorf("expected Model=gpt-4o, got %q", diag.Model)
	}
	if diag.FinishReason != "stop" {
		t.Errorf("expected FinishReason=stop, got %q", diag.FinishReason)
	}
	if diag.TokensOutput != 150 {
		t.Errorf("expected TokensOutput=150, got %d", diag.TokensOutput)
	}
	if diag.ExtractionMethod != "tool_call" {
		t.Errorf("expected ExtractionMethod=tool_call, got %q", diag.ExtractionMethod)
	}
}

func TestExtractContractNodeDiagnosticsNotFound(t *testing.T) {
	nodes := []*workflow.NodeResult{
		{NodeID: "agent-a", Metadata: nil},
		{NodeID: "result", Metadata: nil},
	}
	diag := extractContractNodeDiagnostics(nodes)
	if diag.NodeID != "" {
		t.Errorf("expected empty NodeID when no contract node found, got %q", diag.NodeID)
	}
}

func TestExtractContractNodeDiagnosticsNilNodes(t *testing.T) {
	diag := extractContractNodeDiagnostics([]*workflow.NodeResult{nil, nil})
	if diag.NodeID != "" {
		t.Error("expected empty diagnostics for nil node entries")
	}
}

// --- applyContractNodeDiagnostics ---

func TestApplyContractNodeDiagnosticsPopulates(t *testing.T) {
	attempt := &bench.AttemptDetail{}
	diag := contractNodeDiagnostics{
		NodeID:           "contract-node",
		Model:            "gpt-4o",
		FinishReason:     "stop",
		MaxTokens:        512,
		TokensOutput:     200,
		ExtractionMethod: "tool_call",
	}
	applyContractNodeDiagnostics(attempt, diag)

	if attempt.ContractNodeID != "contract-node" {
		t.Errorf("expected ContractNodeID=contract-node, got %q", attempt.ContractNodeID)
	}
	if attempt.ContractModel != "gpt-4o" {
		t.Errorf("expected ContractModel=gpt-4o, got %q", attempt.ContractModel)
	}
	if attempt.ContractFinishReason != "stop" {
		t.Errorf("expected ContractFinishReason=stop, got %q", attempt.ContractFinishReason)
	}
	if attempt.ContractMaxTokens != 512 {
		t.Errorf("expected ContractMaxTokens=512, got %d", attempt.ContractMaxTokens)
	}
	if attempt.ContractTokensOutput != 200 {
		t.Errorf("expected ContractTokensOutput=200, got %d", attempt.ContractTokensOutput)
	}
}

func TestApplyContractNodeDiagnosticsNilAttempt(t *testing.T) {
	// Should not panic
	applyContractNodeDiagnostics(nil, contractNodeDiagnostics{NodeID: "x"})
}

func TestApplyContractNodeDiagnosticsEmptyNodeID(t *testing.T) {
	attempt := &bench.AttemptDetail{}
	applyContractNodeDiagnostics(attempt, contractNodeDiagnostics{}) // NodeID is empty
	if attempt.ContractNodeID != "" {
		t.Error("expected no change when NodeID is empty")
	}
}

// --- enrichContractFailureDetails ---

func TestEnrichContractFailureDetailsPopulatesDiagnostic(t *testing.T) {
	attempt := &bench.AttemptDetail{
		FailureReason:            bench.FailureReasonEmptyFinalOutput,
		ContractNodeID:           "contract-node",
		ContractModel:            "gpt-4o",
		ContractFinishReason:     "length",
		ContractTokensOutput:     200,
		ContractMaxTokens:        256,
		ContractExtractionMethod: "tool_call",
	}
	enrichContractFailureDetails(attempt)
	if attempt.ContractDiagnostic == "" {
		t.Fatal("expected ContractDiagnostic to be populated")
	}
	if !strings.Contains(attempt.ContractDiagnostic, "contract_node=contract-node") {
		t.Errorf("expected contract_node in diagnostic, got %q", attempt.ContractDiagnostic)
	}
	if !strings.Contains(attempt.ContractDiagnostic, "model=gpt-4o") {
		t.Errorf("expected model in diagnostic, got %q", attempt.ContractDiagnostic)
	}
	// Error field should be populated for empty_final_output
	if attempt.Error == "" {
		t.Error("expected Error to be populated for empty_final_output failure")
	}
}

func TestEnrichContractFailureDetailsSkipsNilOrMissingNodeID(t *testing.T) {
	enrichContractFailureDetails(nil) // should not panic

	attempt := &bench.AttemptDetail{FailureReason: bench.FailureReasonEmptyFinalOutput}
	enrichContractFailureDetails(attempt) // ContractNodeID is empty
	if attempt.ContractDiagnostic != "" {
		t.Error("expected no diagnostic when ContractNodeID is empty")
	}
}

func TestEnrichContractFailureDetailsDoesNotOverrideExistingError(t *testing.T) {
	attempt := &bench.AttemptDetail{
		FailureReason:  bench.FailureReasonEmptyFinalOutput,
		ContractNodeID: "contract-node",
		Error:          "already set error",
	}
	enrichContractFailureDetails(attempt)
	// Existing error should be preserved (not overwritten)
	if attempt.Error != "already set error" {
		t.Errorf("expected error to remain 'already set error', got %q", attempt.Error)
	}
}

// testErr is a minimal error type for testing.
type testErr string

func (e testErr) Error() string { return string(e) }
