package api

import (
	"encoding/json"
	"testing"
)

func TestNewAPIError(t *testing.T) {
	tests := []struct {
		name        string
		message     string
		code        string
		wantMessage string
		wantCode    string
	}{
		{
			name:        "creates error with message and code",
			message:     "workflow validation failed",
			code:        ErrCodeInvalidWorkflow,
			wantMessage: "workflow validation failed",
			wantCode:    ErrCodeInvalidWorkflow,
		},
		{
			name:        "creates error with empty message",
			message:     "",
			code:        ErrCodeInternalError,
			wantMessage: "",
			wantCode:    ErrCodeInternalError,
		},
		{
			name:        "creates error with empty code",
			message:     "some error",
			code:        "",
			wantMessage: "some error",
			wantCode:    "",
		},
		{
			name:        "details and nodeID are nil/empty by default",
			message:     "msg",
			code:        ErrCodeNodeFailed,
			wantMessage: "msg",
			wantCode:    ErrCodeNodeFailed,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NewAPIError(tc.message, tc.code)
			if got == nil {
				t.Fatal("expected non-nil APIError")
			}
			if got.Error != tc.wantMessage {
				t.Errorf("Error = %q, want %q", got.Error, tc.wantMessage)
			}
			if got.Code != tc.wantCode {
				t.Errorf("Code = %q, want %q", got.Code, tc.wantCode)
			}
			if got.Details != nil {
				t.Errorf("Details = %v, want nil", got.Details)
			}
			if got.NodeID != "" {
				t.Errorf("NodeID = %q, want empty", got.NodeID)
			}
		})
	}
}

func TestAPIErrorWithDetails(t *testing.T) {
	t.Run("string details", func(t *testing.T) {
		err := NewAPIError("msg", ErrCodeExecutionFailed)
		got := err.WithDetails("additional context here")
		if got != err {
			t.Error("WithDetails should return the same *APIError for chaining")
		}
		if got.Details != "additional context here" {
			t.Errorf("Details = %v, want %q", got.Details, "additional context here")
		}
	})

	t.Run("nil details", func(t *testing.T) {
		err := NewAPIError("msg", ErrCodeExecutionFailed)
		got := err.WithDetails(nil)
		if got != err {
			t.Error("WithDetails should return the same *APIError for chaining")
		}
		if got.Details != nil {
			t.Errorf("Details = %v, want nil", got.Details)
		}
	})

	t.Run("integer details", func(t *testing.T) {
		err := NewAPIError("msg", ErrCodeExecutionFailed)
		got := err.WithDetails(42)
		if got != err {
			t.Error("WithDetails should return the same *APIError for chaining")
		}
		if got.Details != 42 {
			t.Errorf("Details = %v, want 42", got.Details)
		}
	})

	t.Run("map details stored and returned", func(t *testing.T) {
		d := map[string]string{"field": "value"}
		err := NewAPIError("msg", ErrCodeExecutionFailed)
		got := err.WithDetails(d)
		if got != err {
			t.Error("WithDetails should return the same *APIError for chaining")
		}
		// Verify the stored value round-trips through JSON correctly
		data, jsonErr := json.Marshal(got)
		if jsonErr != nil {
			t.Fatalf("json.Marshal failed: %v", jsonErr)
		}
		var m map[string]interface{}
		if jsonErr := json.Unmarshal(data, &m); jsonErr != nil {
			t.Fatalf("json.Unmarshal failed: %v", jsonErr)
		}
		details, ok := m["details"].(map[string]interface{})
		if !ok {
			t.Fatalf("details not a map: %T", m["details"])
		}
		if details["field"] != "value" {
			t.Errorf("details[field] = %v, want %q", details["field"], "value")
		}
	})

	t.Run("slice details stored and returned", func(t *testing.T) {
		d := []string{"err1", "err2"}
		err := NewAPIError("msg", ErrCodeExecutionFailed)
		got := err.WithDetails(d)
		if got != err {
			t.Error("WithDetails should return the same *APIError for chaining")
		}
		data, jsonErr := json.Marshal(got)
		if jsonErr != nil {
			t.Fatalf("json.Marshal failed: %v", jsonErr)
		}
		var m map[string]interface{}
		if jsonErr := json.Unmarshal(data, &m); jsonErr != nil {
			t.Fatalf("json.Unmarshal failed: %v", jsonErr)
		}
		details, ok := m["details"].([]interface{})
		if !ok {
			t.Fatalf("details not a slice: %T", m["details"])
		}
		if len(details) != 2 {
			t.Errorf("len(details) = %d, want 2", len(details))
		}
	})
}

func TestAPIErrorWithNodeID(t *testing.T) {
	tests := []struct {
		name       string
		nodeID     string
		wantNodeID string
	}{
		{
			name:       "sets node ID",
			nodeID:     "node-abc-123",
			wantNodeID: "node-abc-123",
		},
		{
			name:       "sets empty node ID",
			nodeID:     "",
			wantNodeID: "",
		},
		{
			name:       "sets node ID with special characters",
			nodeID:     "agent-1/step-2",
			wantNodeID: "agent-1/step-2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := NewAPIError("msg", ErrCodeNodeFailed)
			got := err.WithNodeID(tc.nodeID)

			// WithNodeID should return the same pointer for chaining
			if got != err {
				t.Error("WithNodeID should return the same *APIError for chaining")
			}
			if got.NodeID != tc.wantNodeID {
				t.Errorf("NodeID = %q, want %q", got.NodeID, tc.wantNodeID)
			}
		})
	}
}

func TestAPIErrorChaining(t *testing.T) {
	err := NewAPIError("node failed", ErrCodeNodeFailed).
		WithDetails("extra info").
		WithNodeID("my-node")

	if err.Error != "node failed" {
		t.Errorf("Error = %q, want %q", err.Error, "node failed")
	}
	if err.Code != ErrCodeNodeFailed {
		t.Errorf("Code = %q, want %q", err.Code, ErrCodeNodeFailed)
	}
	if err.Details != "extra info" {
		t.Errorf("Details = %v, want %q", err.Details, "extra info")
	}
	if err.NodeID != "my-node" {
		t.Errorf("NodeID = %q, want %q", err.NodeID, "my-node")
	}
}

func TestAPIErrorJSONSerialization(t *testing.T) {
	tests := []struct {
		name       string
		err        *APIError
		wantKeys   []string
		wantAbsent []string
	}{
		{
			name:       "minimal error omits optional fields",
			err:        NewAPIError("bad request", ErrCodeInvalidJSON),
			wantKeys:   []string{"error", "code"},
			wantAbsent: []string{"details", "node_id"},
		},
		{
			name:       "error with details includes details field",
			err:        NewAPIError("bad request", ErrCodeInvalidJSON).WithDetails("some detail"),
			wantKeys:   []string{"error", "code", "details"},
			wantAbsent: []string{"node_id"},
		},
		{
			name:       "error with node ID includes node_id field",
			err:        NewAPIError("node failed", ErrCodeNodeFailed).WithNodeID("n1"),
			wantKeys:   []string{"error", "code", "node_id"},
			wantAbsent: []string{"details"},
		},
		{
			name:       "error with all fields includes all",
			err:        NewAPIError("full error", ErrCodeExecutionFailed).WithDetails(map[string]int{"count": 3}).WithNodeID("n2"),
			wantKeys:   []string{"error", "code", "details", "node_id"},
			wantAbsent: []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.err)
			if err != nil {
				t.Fatalf("json.Marshal failed: %v", err)
			}

			var m map[string]interface{}
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatalf("json.Unmarshal failed: %v", err)
			}

			for _, key := range tc.wantKeys {
				if _, ok := m[key]; !ok {
					t.Errorf("expected key %q in JSON output, got keys: %v", key, mapKeys(m))
				}
			}
			for _, key := range tc.wantAbsent {
				if _, ok := m[key]; ok {
					t.Errorf("expected key %q to be absent from JSON output, but found it", key)
				}
			}
		})
	}
}

func TestAPIErrorJSONValues(t *testing.T) {
	err := NewAPIError("job not found", ErrCodeJobNotFound).
		WithDetails("check the job ID").
		WithNodeID("step-1")

	data, jsonErr := json.Marshal(err)
	if jsonErr != nil {
		t.Fatalf("json.Marshal failed: %v", jsonErr)
	}

	var m map[string]interface{}
	if jsonErr := json.Unmarshal(data, &m); jsonErr != nil {
		t.Fatalf("json.Unmarshal failed: %v", jsonErr)
	}

	if m["error"] != "job not found" {
		t.Errorf("error = %v, want %q", m["error"], "job not found")
	}
	if m["code"] != ErrCodeJobNotFound {
		t.Errorf("code = %v, want %q", m["code"], ErrCodeJobNotFound)
	}
	if m["details"] != "check the job ID" {
		t.Errorf("details = %v, want %q", m["details"], "check the job ID")
	}
	if m["node_id"] != "step-1" {
		t.Errorf("node_id = %v, want %q", m["node_id"], "step-1")
	}
}

func TestErrCodeConstants(t *testing.T) {
	// Ensure all constants have non-empty values and match expected strings
	constants := map[string]string{
		"ErrCodeInvalidWorkflow":   ErrCodeInvalidWorkflow,
		"ErrCodeCycleDetected":     ErrCodeCycleDetected,
		"ErrCodeInvalidModel":      ErrCodeInvalidModel,
		"ErrCodeInvalidJSON":       ErrCodeInvalidJSON,
		"ErrCodeExecutionFailed":   ErrCodeExecutionFailed,
		"ErrCodeNodeFailed":        ErrCodeNodeFailed,
		"ErrCodeExecutionTimeout":  ErrCodeExecutionTimeout,
		"ErrCodeCancelled":         ErrCodeCancelled,
		"ErrCodeJobNotFound":       ErrCodeJobNotFound,
		"ErrCodeJobNotRunning":     ErrCodeJobNotRunning,
		"ErrCodeJobNotPending":     ErrCodeJobNotPending,
		"ErrCodeJobNotPaused":      ErrCodeJobNotPaused,
		"ErrCodePoolExhausted":     ErrCodePoolExhausted,
		"ErrCodeAdmissionPaused":   ErrCodeAdmissionPaused,
		"ErrCodeConnectionLost":    ErrCodeConnectionLost,
		"ErrCodeConnectionTimeout": ErrCodeConnectionTimeout,
		"ErrCodeInternalError":     ErrCodeInternalError,
		"ErrCodeDatabaseError":     ErrCodeDatabaseError,
	}

	for name, value := range constants {
		if value == "" {
			t.Errorf("constant %s has empty value", name)
		}
	}

	// Spot-check specific values
	expectedValues := map[string]string{
		ErrCodeInvalidWorkflow:   "INVALID_WORKFLOW",
		ErrCodeCycleDetected:     "CYCLE_DETECTED",
		ErrCodeInvalidModel:      "INVALID_MODEL",
		ErrCodeInvalidJSON:       "INVALID_JSON",
		ErrCodeExecutionFailed:   "EXECUTION_FAILED",
		ErrCodeNodeFailed:        "NODE_FAILED",
		ErrCodeExecutionTimeout:  "EXECUTION_TIMEOUT",
		ErrCodeCancelled:         "CANCELLED",
		ErrCodeJobNotFound:       "JOB_NOT_FOUND",
		ErrCodeJobNotRunning:     "JOB_NOT_RUNNING",
		ErrCodeJobNotPending:     "JOB_NOT_PENDING",
		ErrCodeJobNotPaused:      "JOB_NOT_PAUSED",
		ErrCodePoolExhausted:     "POOL_EXHAUSTED",
		ErrCodeAdmissionPaused:   "ADMISSION_PAUSED",
		ErrCodeConnectionLost:    "CONNECTION_LOST",
		ErrCodeConnectionTimeout: "CONNECTION_TIMEOUT",
		ErrCodeInternalError:     "INTERNAL_ERROR",
		ErrCodeDatabaseError:     "DATABASE_ERROR",
	}

	for code, want := range expectedValues {
		if code != want {
			t.Errorf("constant value = %q, want %q", code, want)
		}
	}
}

// mapKeys is a helper to list the keys of a map for error messages.
func mapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
