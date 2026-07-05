package http

import (
	"fmt"
	"testing"
)

func TestExitCode_Nil(t *testing.T) {
	if code := ExitCode(nil); code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestExitCode_NonAPIError(t *testing.T) {
	if code := ExitCode(fmt.Errorf("generic error")); code != 1 {
		t.Errorf("expected 1, got %d", code)
	}
}

func TestExitCode_NetworkError(t *testing.T) {
	err := &APIError{StatusCode: 0, Message: "dial tcp: connection refused"}
	if code := ExitCode(err); code != 6 {
		t.Errorf("expected 6, got %d", code)
	}
}

func TestExitCode_BadRequest(t *testing.T) {
	err := &APIError{StatusCode: 400, Message: "bad request"}
	if code := ExitCode(err); code != 2 {
		t.Errorf("expected 2, got %d", code)
	}
}

func TestExitCode_Unauthorized(t *testing.T) {
	err := &APIError{StatusCode: 401, Message: "unauthorized"}
	if code := ExitCode(err); code != 3 {
		t.Errorf("expected 3, got %d", code)
	}
}

func TestExitCode_Forbidden(t *testing.T) {
	err := &APIError{StatusCode: 403, Message: "forbidden"}
	if code := ExitCode(err); code != 3 {
		t.Errorf("expected 3, got %d", code)
	}
}

func TestExitCode_NotFound(t *testing.T) {
	err := &APIError{StatusCode: 404, Message: "not found"}
	if code := ExitCode(err); code != 4 {
		t.Errorf("expected 4, got %d", code)
	}
}

func TestExitCode_Conflict(t *testing.T) {
	err := &APIError{StatusCode: 409, Message: "conflict"}
	if code := ExitCode(err); code != 5 {
		t.Errorf("expected 5, got %d", code)
	}
}

func TestExitCode_ServerError(t *testing.T) {
	err := &APIError{StatusCode: 500, Message: "internal server error"}
	if code := ExitCode(err); code != 6 {
		t.Errorf("expected 6, got %d", code)
	}
}

func TestAPIError_Error(t *testing.T) {
	tests := []struct {
		err  *APIError
		want string
	}{
		{&APIError{StatusCode: 0, Message: "timeout"}, "network error: timeout"},
		{&APIError{StatusCode: 404, Message: "not found"}, "HTTP 404: not found"},
	}
	for _, tt := range tests {
		if got := tt.err.Error(); got != tt.want {
			t.Errorf("Error() = %q, want %q", got, tt.want)
		}
	}
}
