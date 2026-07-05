package storage

import (
	"testing"
)

func TestCompleteExecution(t *testing.T) {
	store := newTestStore(t)

	t.Run("Success", func(t *testing.T) {
		createTestExecution(t, store, "complete-ok", "running")

		err := store.CompleteExecution("complete-ok", "completed", "result text", 0.05, 100, 50, 150, "")
		if err != nil {
			t.Fatalf("CompleteExecution failed: %v", err)
		}

		job, err := store.GetExecution("complete-ok")
		if err != nil {
			t.Fatalf("GetExecution failed: %v", err)
		}

		if job.Status != "completed" {
			t.Errorf("status = %q, want %q", job.Status, "completed")
		}
		if job.ResultText != "result text" {
			t.Errorf("result_text = %q, want %q", job.ResultText, "result text")
		}
		if job.Cost != 0.05 {
			t.Errorf("cost = %f, want 0.05", job.Cost)
		}
		if job.TokensInput != 100 {
			t.Errorf("tokens_input = %d, want 100", job.TokensInput)
		}
		if job.TokensOutput != 50 {
			t.Errorf("tokens_output = %d, want 50", job.TokensOutput)
		}
		if job.TokensTotal != 150 {
			t.Errorf("tokens_total = %d, want 150", job.TokensTotal)
		}
		if job.ErrorMessage != "" {
			t.Errorf("error_message = %q, want empty", job.ErrorMessage)
		}
	})

	t.Run("Failed_WithError", func(t *testing.T) {
		createTestExecution(t, store, "complete-fail", "running")

		err := store.CompleteExecution("complete-fail", "failed", "", 0.01, 10, 5, 15, "something broke")
		if err != nil {
			t.Fatalf("CompleteExecution failed: %v", err)
		}

		job, err := store.GetExecution("complete-fail")
		if err != nil {
			t.Fatalf("GetExecution failed: %v", err)
		}

		if job.Status != "failed" {
			t.Errorf("status = %q, want %q", job.Status, "failed")
		}
		if job.ResultText != "" {
			t.Errorf("result_text = %q, want empty", job.ResultText)
		}
		if job.Cost != 0.01 {
			t.Errorf("cost = %f, want 0.01", job.Cost)
		}
		if job.TokensInput != 10 {
			t.Errorf("tokens_input = %d, want 10", job.TokensInput)
		}
		if job.TokensOutput != 5 {
			t.Errorf("tokens_output = %d, want 5", job.TokensOutput)
		}
		if job.TokensTotal != 15 {
			t.Errorf("tokens_total = %d, want 15", job.TokensTotal)
		}
		if job.ErrorMessage != "something broke" {
			t.Errorf("error_message = %q, want %q", job.ErrorMessage, "something broke")
		}
	})
}
