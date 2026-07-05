package events

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// IsTerminalStatus
// ---------------------------------------------------------------------------

func TestIsTerminalStatus(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{JobStatusCompleted, true},
		{JobStatusFailed, true},
		{JobStatusCancelled, true},
		{JobStatusPending, false},
		{JobStatusRunning, false},
		{JobStatusPaused, false},
		{"", false},
		{"unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := IsTerminalStatus(tt.status)
			if got != tt.want {
				t.Fatalf("IsTerminalStatus(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// IsTerminalEvent
// ---------------------------------------------------------------------------

func TestIsTerminalEvent(t *testing.T) {
	tests := []struct {
		eventType string
		want      bool
	}{
		{EventComplete, true},
		{EventError, true},
		{EventCancelled, true},
		{EventStatus, false},
		{EventNodeStart, false},
		{EventNodeComplete, false},
		{EventNodeFailed, false},
		{EventPaused, false},
		{"", false},
		{"unknown_event", false},
	}

	for _, tt := range tests {
		t.Run(tt.eventType, func(t *testing.T) {
			got := IsTerminalEvent(tt.eventType)
			if got != tt.want {
				t.Fatalf("IsTerminalEvent(%q) = %v, want %v", tt.eventType, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// NewEventEnvelope
// ---------------------------------------------------------------------------

func TestNewEventEnvelope_BasicFields(t *testing.T) {
	before := time.Now()
	env := NewEventEnvelope("job-123", EventNodeStart, map[string]interface{}{"node": "n1"})
	after := time.Now()

	if env.JobID != "job-123" {
		t.Errorf("JobID = %q, want %q", env.JobID, "job-123")
	}
	if env.Type != EventNodeStart {
		t.Errorf("Type = %q, want %q", env.Type, EventNodeStart)
	}
	if env.Version != CurrentEventVersion {
		t.Errorf("Version = %d, want %d", env.Version, CurrentEventVersion)
	}
	if env.Payload == nil {
		t.Fatal("expected non-nil Payload")
	}
	if env.Payload["node"] != "n1" {
		t.Errorf("Payload[node] = %v, want %q", env.Payload["node"], "n1")
	}
	if env.Timestamp.Before(before) || env.Timestamp.After(after) {
		t.Errorf("Timestamp %v not in range [%v, %v]", env.Timestamp, before, after)
	}
}

func TestNewEventEnvelope_NilPayloadBecomesEmptyMap(t *testing.T) {
	env := NewEventEnvelope("job-456", EventComplete, nil)
	if env.Payload == nil {
		t.Fatal("expected non-nil Payload when nil was provided")
	}
	if len(env.Payload) != 0 {
		t.Fatalf("expected empty payload map, got %v", env.Payload)
	}
}

func TestNewEventEnvelope_EmptyPayload(t *testing.T) {
	env := NewEventEnvelope("job-789", EventError, map[string]interface{}{})
	if env.Payload == nil {
		t.Fatal("expected non-nil Payload")
	}
	if len(env.Payload) != 0 {
		t.Errorf("expected empty payload, got %v", env.Payload)
	}
}

// ---------------------------------------------------------------------------
// EventEnvelope builder methods (WithNodeID, WithMessage, WithError)
// ---------------------------------------------------------------------------

func TestEventEnvelope_WithNodeID(t *testing.T) {
	env := NewEventEnvelope("job-1", EventNodeStart, nil)
	returned := env.WithNodeID("node-abc")

	// Should return the same pointer (mutates in place)
	if returned != env {
		t.Fatal("WithNodeID should return the same envelope pointer")
	}
	if env.NodeID != "node-abc" {
		t.Errorf("NodeID = %q, want %q", env.NodeID, "node-abc")
	}
}

func TestEventEnvelope_WithMessage(t *testing.T) {
	env := NewEventEnvelope("job-1", EventStatus, nil)
	returned := env.WithMessage("workflow starting")

	if returned != env {
		t.Fatal("WithMessage should return the same envelope pointer")
	}
	if env.Message != "workflow starting" {
		t.Errorf("Message = %q, want %q", env.Message, "workflow starting")
	}
}

func TestEventEnvelope_WithError(t *testing.T) {
	env := NewEventEnvelope("job-1", EventError, nil)
	returned := env.WithError("something went wrong", "ERR_TIMEOUT")

	if returned != env {
		t.Fatal("WithError should return the same envelope pointer")
	}
	if env.Error != "something went wrong" {
		t.Errorf("Error = %q, want %q", env.Error, "something went wrong")
	}
	if env.Code != "ERR_TIMEOUT" {
		t.Errorf("Code = %q, want %q", env.Code, "ERR_TIMEOUT")
	}
}

func TestEventEnvelope_BuilderChaining(t *testing.T) {
	env := NewEventEnvelope("job-chain", EventNodeFailed, map[string]interface{}{"k": "v"}).
		WithNodeID("node-x").
		WithMessage("node failed after 3 retries").
		WithError("deadline exceeded", "ERR_DEADLINE")

	if env.NodeID != "node-x" {
		t.Errorf("NodeID = %q", env.NodeID)
	}
	if env.Message != "node failed after 3 retries" {
		t.Errorf("Message = %q", env.Message)
	}
	if env.Error != "deadline exceeded" {
		t.Errorf("Error = %q", env.Error)
	}
	if env.Code != "ERR_DEADLINE" {
		t.Errorf("Code = %q", env.Code)
	}
	if env.Payload["k"] != "v" {
		t.Errorf("Payload[k] = %v, want %q", env.Payload["k"], "v")
	}
}

// ---------------------------------------------------------------------------
// CloseCodeToReason
// ---------------------------------------------------------------------------

func TestCloseCodeToReason(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{CloseNormal, "workflow completed"},
		{CloseFailed, "workflow failed"},
		{CloseCancelled, "workflow cancelled"},
		{CloseServerShutdown, "server shutdown"},
		{CloseReconnectRequired, "reconnect required"},
		{ClosePoolExhausted, "server at capacity"},
		{9999, "unknown"},
		{0, "unknown"},
		{-1, "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := CloseCodeToReason(tt.code)
			if got != tt.want {
				t.Fatalf("CloseCodeToReason(%d) = %q, want %q", tt.code, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// StatusToCloseCode
// ---------------------------------------------------------------------------

func TestStatusToCloseCode(t *testing.T) {
	tests := []struct {
		status string
		want   int
	}{
		{JobStatusCompleted, CloseNormal},
		{JobStatusFailed, CloseFailed},
		{JobStatusCancelled, CloseCancelled},
		// Non-terminal and unknown statuses default to CloseNormal
		{JobStatusPending, CloseNormal},
		{JobStatusRunning, CloseNormal},
		{JobStatusPaused, CloseNormal},
		{"", CloseNormal},
		{"unknown_status", CloseNormal},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := StatusToCloseCode(tt.status)
			if got != tt.want {
				t.Fatalf("StatusToCloseCode(%q) = %d, want %d", tt.status, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Close code constants sanity check
// ---------------------------------------------------------------------------

func TestCloseCodeConstants(t *testing.T) {
	// Verify the numeric values are stable (WebSocket close codes are part of the
	// public protocol contract and must not change silently).
	expected := map[string]int{
		"CloseNormal":            4000,
		"CloseFailed":            4001,
		"CloseCancelled":         4002,
		"CloseServerShutdown":    4003,
		"CloseReconnectRequired": 4004,
		"ClosePoolExhausted":     4005,
	}
	got := map[string]int{
		"CloseNormal":            CloseNormal,
		"CloseFailed":            CloseFailed,
		"CloseCancelled":         CloseCancelled,
		"CloseServerShutdown":    CloseServerShutdown,
		"CloseReconnectRequired": CloseReconnectRequired,
		"ClosePoolExhausted":     ClosePoolExhausted,
	}
	for name, want := range expected {
		if got[name] != want {
			t.Errorf("constant %s = %d, want %d", name, got[name], want)
		}
	}
}
