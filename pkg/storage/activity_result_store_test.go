package storage

import (
	"context"
	"testing"
	"time"
)

func TestActivityResult_SaveAndGet(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	now := time.Now()

	result := &ActivityResult{
		IdempotencyKey: "exec-1:node-a:1",
		RunID:          "run-1",
		NodeID:         "node-a",
		Attempt:        1,
		ActivityType:   "llm_call",
		Status:         "completed",
		OutputPayload:  `{"output":"hello"}`,
		ErrorCode:      "",
		ErrorMessage:   "",
		CompletedAt:    &now,
		Checksum:       "abc123",
	}
	if err := store.SaveActivityResult(ctx, result); err != nil {
		t.Fatalf("SaveActivityResult failed: %v", err)
	}

	got, err := store.GetActivityResult(ctx, "exec-1:node-a:1")
	if err != nil {
		t.Fatalf("GetActivityResult failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected result, got nil")
	}
	if got.IdempotencyKey != "exec-1:node-a:1" {
		t.Errorf("IdempotencyKey = %q, want %q", got.IdempotencyKey, "exec-1:node-a:1")
	}
	if got.RunID != "run-1" {
		t.Errorf("RunID = %q, want %q", got.RunID, "run-1")
	}
	if got.NodeID != "node-a" {
		t.Errorf("NodeID = %q, want %q", got.NodeID, "node-a")
	}
	if got.Attempt != 1 {
		t.Errorf("Attempt = %d, want 1", got.Attempt)
	}
	if got.ActivityType != "llm_call" {
		t.Errorf("ActivityType = %q, want %q", got.ActivityType, "llm_call")
	}
	if got.Status != "completed" {
		t.Errorf("Status = %q, want %q", got.Status, "completed")
	}
	if got.OutputPayload != `{"output":"hello"}` {
		t.Errorf("OutputPayload = %q, want %q", got.OutputPayload, `{"output":"hello"}`)
	}
	if got.Checksum != "abc123" {
		t.Errorf("Checksum = %q, want %q", got.Checksum, "abc123")
	}
	if got.ID == 0 {
		t.Error("expected non-zero ID")
	}
}

func TestActivityResult_GetNonExistent(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage failed: %v", err)
	}
	defer store.Close()

	got, err := store.GetActivityResult(context.Background(), "does-not-exist")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil result, got %+v", got)
	}
}

func TestActivityResult_DuplicateKeyPreservesOriginal(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	now := time.Now()
	key := "exec-dup:node-x:1"

	first := &ActivityResult{
		IdempotencyKey: key,
		RunID:          "run-1",
		NodeID:         "node-x",
		Attempt:        1,
		ActivityType:   "llm_call",
		Status:         "completed",
		OutputPayload:  "first-output",
		CompletedAt:    &now,
	}
	if err := store.SaveActivityResult(ctx, first); err != nil {
		t.Fatalf("first save failed: %v", err)
	}

	// Second save with same key but different payload — should be ignored.
	second := &ActivityResult{
		IdempotencyKey: key,
		RunID:          "run-1",
		NodeID:         "node-x",
		Attempt:        1,
		ActivityType:   "llm_call",
		Status:         "completed",
		OutputPayload:  "OVERWRITTEN",
		CompletedAt:    &now,
	}
	if err := store.SaveActivityResult(ctx, second); err != nil {
		t.Fatalf("second save failed: %v", err)
	}

	got, err := store.GetActivityResult(ctx, key)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.OutputPayload != "first-output" {
		t.Errorf("idempotency violated: OutputPayload = %q, want %q", got.OutputPayload, "first-output")
	}
}

func TestActivityResult_DuplicateKeyStatusMismatchLogsWarning(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	now := time.Now()
	key := "exec-mismatch:node-y:1"

	first := &ActivityResult{
		IdempotencyKey: key,
		RunID:          "run-1",
		NodeID:         "node-y",
		Attempt:        1,
		ActivityType:   "llm_call",
		Status:         "completed",
		OutputPayload:  "ok",
		CompletedAt:    &now,
	}
	if err := store.SaveActivityResult(ctx, first); err != nil {
		t.Fatalf("first save failed: %v", err)
	}

	// Save with different status — should not error, but original persists.
	conflicting := &ActivityResult{
		IdempotencyKey: key,
		RunID:          "run-1",
		NodeID:         "node-y",
		Attempt:        1,
		ActivityType:   "llm_call",
		Status:         "failed",
		ErrorMessage:   "something went wrong",
		CompletedAt:    &now,
	}
	if err := store.SaveActivityResult(ctx, conflicting); err != nil {
		t.Fatalf("conflicting save should not error: %v", err)
	}

	got, err := store.GetActivityResult(ctx, key)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.Status != "completed" {
		t.Errorf("original status should be preserved: got %q, want %q", got.Status, "completed")
	}
}

func TestActivityResult_NullableFieldsHandled(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Save with all optional fields empty.
	result := &ActivityResult{
		IdempotencyKey: "exec-null:node-z:1",
		RunID:          "run-1",
		NodeID:         "node-z",
		Attempt:        1,
		ActivityType:   "llm_call",
		Status:         "failed",
	}
	if err := store.SaveActivityResult(ctx, result); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	got, err := store.GetActivityResult(ctx, "exec-null:node-z:1")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected result, got nil")
	}
	// COALESCE in the query should turn NULLs into empty strings.
	if got.OutputPayload != "" {
		t.Errorf("OutputPayload = %q, want empty", got.OutputPayload)
	}
	if got.ErrorCode != "" {
		t.Errorf("ErrorCode = %q, want empty", got.ErrorCode)
	}
	if got.ErrorMessage != "" {
		t.Errorf("ErrorMessage = %q, want empty", got.ErrorMessage)
	}
	if got.Checksum != "" {
		t.Errorf("Checksum = %q, want empty", got.Checksum)
	}
}

func TestActivityResult_MultipleKeysIndependent(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("new storage failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	now := time.Now()

	keys := []string{"exec-1:a:1", "exec-1:b:1", "exec-2:a:1"}
	for i, key := range keys {
		r := &ActivityResult{
			IdempotencyKey: key,
			RunID:          "run-1",
			NodeID:         "node",
			Attempt:        1,
			ActivityType:   "llm_call",
			Status:         "completed",
			OutputPayload:  key, // use key as payload for easy verification
			CompletedAt:    &now,
		}
		if err := store.SaveActivityResult(ctx, r); err != nil {
			t.Fatalf("save %d failed: %v", i, err)
		}
	}

	for _, key := range keys {
		got, err := store.GetActivityResult(ctx, key)
		if err != nil {
			t.Fatalf("get %q failed: %v", key, err)
		}
		if got == nil {
			t.Fatalf("expected result for %q, got nil", key)
		}
		if got.OutputPayload != key {
			t.Errorf("key %q: OutputPayload = %q, want %q", key, got.OutputPayload, key)
		}
	}
}

func TestActivityResult_MarshalActivityOutput(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		wantErr bool
	}{
		{
			name:  "string",
			input: "hello world",
		},
		{
			name:  "map",
			input: map[string]interface{}{"key": "value", "num": 42},
		},
		{
			name:  "struct",
			input: struct{ Name string }{"test"},
		},
		{
			name:    "channel is not serializable",
			input:   make(chan int),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := MarshalActivityOutput(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out == "" {
				t.Error("expected non-empty output")
			}
		})
	}
}
