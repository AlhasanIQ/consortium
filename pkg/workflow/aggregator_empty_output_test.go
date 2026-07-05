package workflow

import (
	"strings"
	"testing"

	"github.com/alhasaniq/consortium/pkg/providers"
)

func TestHasEmptyOutputWithConsumedTokens_NilResponse(t *testing.T) {
	if hasEmptyOutputWithConsumedTokens(nil) {
		t.Error("nil response should return false")
	}
}

func TestHasEmptyOutputWithConsumedTokens_NonEmptyContent(t *testing.T) {
	resp := &providers.CompletionResponse{
		Content: "some output",
		Usage:   providers.Usage{CompletionTokens: 10},
	}
	if hasEmptyOutputWithConsumedTokens(resp) {
		t.Error("non-empty content should return false")
	}
}

func TestHasEmptyOutputWithConsumedTokens_EmptyWithCompletionTokens(t *testing.T) {
	resp := &providers.CompletionResponse{
		Content: "",
		Usage:   providers.Usage{CompletionTokens: 50, PromptTokens: 100, TotalTokens: 150},
	}
	if !hasEmptyOutputWithConsumedTokens(resp) {
		t.Error("empty content with completion tokens should return true")
	}
}

func TestHasEmptyOutputWithConsumedTokens_WhitespaceWithCompletionTokens(t *testing.T) {
	resp := &providers.CompletionResponse{
		Content: "   \n\t  ",
		Usage:   providers.Usage{CompletionTokens: 50},
	}
	if !hasEmptyOutputWithConsumedTokens(resp) {
		t.Error("whitespace-only content with completion tokens should return true")
	}
}

func TestHasEmptyOutputWithConsumedTokens_EmptyNoTokens(t *testing.T) {
	resp := &providers.CompletionResponse{
		Content: "",
		Usage:   providers.Usage{CompletionTokens: 0, PromptTokens: 0, TotalTokens: 0},
	}
	if hasEmptyOutputWithConsumedTokens(resp) {
		t.Error("empty content with zero tokens should return false")
	}
}

func TestHasEmptyOutputWithConsumedTokens_TotalExceedsPrompt(t *testing.T) {
	// Some providers don't populate CompletionTokens but set TotalTokens > PromptTokens
	resp := &providers.CompletionResponse{
		Content: "",
		Usage:   providers.Usage{CompletionTokens: 0, PromptTokens: 100, TotalTokens: 150},
	}
	if !hasEmptyOutputWithConsumedTokens(resp) {
		t.Error("empty content where TotalTokens > PromptTokens should return true")
	}
}

func TestConsumedOutputTokens_CompletionTokens(t *testing.T) {
	usage := providers.Usage{CompletionTokens: 42, PromptTokens: 100, TotalTokens: 142}
	got := consumedOutputTokens(usage)
	if got != 42 {
		t.Errorf("expected 42, got %d", got)
	}
}

func TestConsumedOutputTokens_FallbackToTotal(t *testing.T) {
	usage := providers.Usage{CompletionTokens: 0, PromptTokens: 100, TotalTokens: 130}
	got := consumedOutputTokens(usage)
	if got != 30 {
		t.Errorf("expected 30, got %d", got)
	}
}

func TestConsumedOutputTokens_NegativeFallback(t *testing.T) {
	usage := providers.Usage{CompletionTokens: 0, PromptTokens: 100, TotalTokens: 50}
	got := consumedOutputTokens(usage)
	if got != 0 {
		t.Errorf("expected 0 for negative output tokens, got %d", got)
	}
}

func TestNewEmptyOutputRetryableError(t *testing.T) {
	usage := providers.Usage{CompletionTokens: 25}
	err := newEmptyOutputRetryableError("aggregation", "gpt-4", usage)
	if err == nil {
		t.Fatal("expected non-nil error")
	}

	msg := err.Error()
	if !strings.Contains(msg, "empty content") {
		t.Errorf("error message should mention 'empty content': %s", msg)
	}
	if !strings.Contains(msg, "25 output tokens") {
		t.Errorf("error message should mention token count: %s", msg)
	}
	if !strings.Contains(msg, "gpt-4") {
		t.Errorf("error message should mention model: %s", msg)
	}

	// Verify it's a retryable error
	var retryable *RetryableError
	if !isRetryableError(err, &retryable) {
		t.Error("expected error to be RetryableError")
	}
}

func isRetryableError(err error, target **RetryableError) bool {
	re, ok := err.(*RetryableError)
	if ok && target != nil {
		*target = re
	}
	return ok
}
