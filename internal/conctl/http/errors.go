package http

import "fmt"

// APIError represents an HTTP API error with status code mapping.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e.StatusCode == 0 {
		return fmt.Sprintf("network error: %s", e.Message)
	}
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
}

// ExitCode maps HTTP status codes to deterministic CLI exit codes.
//
//	0  success
//	2  usage/client error (400)
//	3  auth/permission (401, 403)
//	4  not found (404)
//	5  conflict/state (409)
//	6  network/server (0, 500+)
//	1  other
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		return 1
	}
	switch {
	case apiErr.StatusCode == 0:
		return 6 // network error
	case apiErr.StatusCode == 400:
		return 2
	case apiErr.StatusCode == 401 || apiErr.StatusCode == 403:
		return 3
	case apiErr.StatusCode == 404:
		return 4
	case apiErr.StatusCode == 409:
		return 5
	case apiErr.StatusCode >= 500:
		return 6
	default:
		return 1
	}
}
