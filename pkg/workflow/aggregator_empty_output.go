package workflow

import (
	"fmt"
	"strings"

	"github.com/alhasaniq/consortium/pkg/providers"
)

// hasEmptyOutputWithConsumedTokens reports whether an LLM call returned blank
// text while still charging output tokens (e.g., reasoning-only completion).
func hasEmptyOutputWithConsumedTokens(resp *providers.CompletionResponse) bool {
	if resp == nil {
		return false
	}
	if strings.TrimSpace(resp.Content) != "" {
		return false
	}
	if resp.Usage.CompletionTokens > 0 {
		return true
	}
	// Some providers may not populate completion_tokens but still report totals.
	return resp.Usage.TotalTokens > resp.Usage.PromptTokens
}

func consumedOutputTokens(usage providers.Usage) int {
	if usage.CompletionTokens > 0 {
		return usage.CompletionTokens
	}
	outputTokens := usage.TotalTokens - usage.PromptTokens
	if outputTokens < 0 {
		return 0
	}
	return outputTokens
}

func newEmptyOutputRetryableError(scope, model string, usage providers.Usage) error {
	return NewRetryableError(
		fmt.Errorf(
			"%s LLM returned empty content after consuming %d output tokens (model %s)",
			scope,
			consumedOutputTokens(usage),
			model,
		),
		RetryCodeOutputTruncatedEmpty,
	)
}
