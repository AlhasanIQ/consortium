package providers

import (
	"errors"
	"fmt"
	"testing"
)

func TestProviderError_Error(t *testing.T) {
	tests := []struct {
		name  string
		err   *ProviderError
		check func(string) bool
	}{
		{
			name: "with model",
			err: &ProviderError{
				Code:       ErrCodeRateLimited,
				StatusCode: 429,
				Message:    "rate limited",
				Provider:   "openrouter",
				Model:      "gpt-4",
			},
			check: func(s string) bool {
				return contains(s, "rate limited") &&
					contains(s, "openrouter") &&
					contains(s, "gpt-4") &&
					contains(s, "429")
			},
		},
		{
			name: "without model",
			err: &ProviderError{
				Code:       ErrCodeUpstreamError,
				StatusCode: 500,
				Message:    "server error",
				Provider:   "openrouter",
			},
			check: func(s string) bool {
				return contains(s, "server error") &&
					contains(s, "openrouter") &&
					contains(s, "500") &&
					!contains(s, "model=")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.err.Error()
			if !tt.check(msg) {
				t.Errorf("unexpected error string: %s", msg)
			}
		})
	}
}

func TestIsProviderError(t *testing.T) {
	pe := &ProviderError{Code: ErrCodeBadRequest, Message: "bad"}
	wrapped := fmt.Errorf("wrapper: %w", pe)
	plain := errors.New("plain error")

	if !IsProviderError(pe) {
		t.Error("expected IsProviderError=true for ProviderError")
	}
	if !IsProviderError(wrapped) {
		t.Error("expected IsProviderError=true for wrapped ProviderError")
	}
	if IsProviderError(plain) {
		t.Error("expected IsProviderError=false for plain error")
	}
	if IsProviderError(nil) {
		t.Error("expected IsProviderError=false for nil")
	}
}

func TestAsProviderError(t *testing.T) {
	pe := &ProviderError{Code: ErrCodeAuthError, Message: "auth fail", StatusCode: 401}
	wrapped := fmt.Errorf("outer: %w", pe)

	// Direct
	got, ok := AsProviderError(pe)
	if !ok || got == nil {
		t.Fatal("expected ok=true for ProviderError")
	}
	if got.Code != ErrCodeAuthError {
		t.Errorf("expected code=%s, got %s", ErrCodeAuthError, got.Code)
	}

	// Wrapped
	got, ok = AsProviderError(wrapped)
	if !ok || got == nil {
		t.Fatal("expected ok=true for wrapped ProviderError")
	}

	// Plain
	_, ok = AsProviderError(errors.New("nope"))
	if ok {
		t.Error("expected ok=false for plain error")
	}

	// Nil
	_, ok = AsProviderError(nil)
	if ok {
		t.Error("expected ok=false for nil")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
