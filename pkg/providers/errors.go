package providers

import (
	"errors"
	"fmt"
	"time"
)

// Provider error code constants — machine-readable codes for structured error handling.
const (
	ErrCodeInsufficientCredits = "INSUFFICIENT_CREDITS" // HTTP 402
	ErrCodeRateLimited         = "RATE_LIMITED"         // HTTP 429
	ErrCodeUpstreamError       = "UPSTREAM_ERROR"       // HTTP 5xx (except 504)
	ErrCodeUpstreamTimeout     = "UPSTREAM_TIMEOUT"     // HTTP 504 / transport timeout
	ErrCodeBadRequest          = "BAD_REQUEST"          // HTTP 400
	ErrCodeAuthError           = "AUTH_ERROR"           // HTTP 401/403
	ErrCodeModelNotFound       = "MODEL_NOT_FOUND"      // HTTP 404 from provider
	ErrCodeContentFiltered     = "CONTENT_FILTERED"     // Moderation refusal
	ErrCodeNoChoices           = "NO_CHOICES"           // Provider returned empty toolcal choices
)

// ProviderError is a structured error for provider-level failures.
// It carries machine-readable classification so downstream consumers
// (executor, job manager, API layer) can map to appropriate HTTP status
// codes and user-facing messages without string parsing.
type ProviderError struct {
	Code         string        // Machine-readable error code (one of ErrCode* constants)
	StatusCode   int           // HTTP status code from provider
	Message      string        // Human-readable error message
	Provider     string        // Provider name (e.g., "openrouter")
	Model        string        // Model that was being called
	RequestID    string        // Provider request identifier when available
	GenerationID string        // OpenRouter generation identifier when available
	ErrorType    string        // Provider-native error type (for example OpenRouter error.metadata.error_type)
	ProviderCode string        // Provider-native nested code (for example OpenRouter error.metadata.provider_code)
	NativeCode   string        // Provider-native top-level error code normalized to string
	ErrorPhase   string        // Failure phase (e.g. http_do, http_read, http_status, response_parse)
	Retryable    bool          // Whether the caller should retry
	RetryAfter   time.Duration // Suggested wait before retry (from Retry-After header)
}

func (e *ProviderError) Error() string {
	if e.Model != "" {
		return fmt.Sprintf("%s (provider=%s, model=%s, status=%d)", e.Message, e.Provider, e.Model, e.StatusCode)
	}
	return fmt.Sprintf("%s (provider=%s, status=%d)", e.Message, e.Provider, e.StatusCode)
}

// IsProviderError checks if an error is a provider error.
func IsProviderError(err error) bool {
	var e *ProviderError
	return errors.As(err, &e)
}

// AsProviderError attempts to extract a ProviderError from an error.
func AsProviderError(err error) (*ProviderError, bool) {
	if err == nil {
		return nil, false
	}
	var pe *ProviderError
	if errors.As(err, &pe) {
		return pe, true
	}
	return nil, false
}
