package workflow

import (
	"errors"
	"testing"
)

func TestCostLimitError(t *testing.T) {
	t.Run("cost limit error has correct code", func(t *testing.T) {
		err := NewCostLimitError("cost", 0.5, 0.52, "node_2")
		if err.Code != ErrCodeCostLimitExceeded {
			t.Errorf("Expected code %s, got %s", ErrCodeCostLimitExceeded, err.Code)
		}
		if err.LimitType != "cost" {
			t.Errorf("Expected limit type 'cost', got %s", err.LimitType)
		}
		if err.LimitValue != 0.5 {
			t.Errorf("Expected limit value 0.5, got %f", err.LimitValue)
		}
		if err.CurrentValue != 0.52 {
			t.Errorf("Expected current value 0.52, got %f", err.CurrentValue)
		}
		if err.NodeID != "node_2" {
			t.Errorf("Expected node ID 'node_2', got %s", err.NodeID)
		}
	})

	t.Run("token limit error has correct code", func(t *testing.T) {
		err := NewCostLimitError("tokens", 1000, 1200, "node_3")
		if err.Code != ErrCodeTokenLimitExceeded {
			t.Errorf("Expected code %s, got %s", ErrCodeTokenLimitExceeded, err.Code)
		}
	})

	t.Run("input token limit error has correct code", func(t *testing.T) {
		err := NewCostLimitError("input_tokens", 500, 600, "node_1")
		if err.Code != ErrCodeInputTokenLimitExceeded {
			t.Errorf("Expected code %s, got %s", ErrCodeInputTokenLimitExceeded, err.Code)
		}
	})

	t.Run("output token limit error has correct code", func(t *testing.T) {
		err := NewCostLimitError("output_tokens", 250, 300, "node_1")
		if err.Code != ErrCodeOutputTokenLimitExceeded {
			t.Errorf("Expected code %s, got %s", ErrCodeOutputTokenLimitExceeded, err.Code)
		}
	})

	t.Run("error message is set", func(t *testing.T) {
		err := NewCostLimitError("cost", 0.5, 0.6, "node_1")
		if err.Error() == "" {
			t.Error("Expected non-empty error message")
		}
		if err.Message == "" {
			t.Error("Expected non-empty message field")
		}
	})
}

func TestIsCostLimitError(t *testing.T) {
	t.Run("returns true for CostLimitError", func(t *testing.T) {
		err := NewCostLimitError("cost", 0.5, 0.6, "node_1")
		if !IsCostLimitError(err) {
			t.Error("Expected IsCostLimitError to return true")
		}
	})

	t.Run("returns false for other errors", func(t *testing.T) {
		err := errors.New("some other error")
		if IsCostLimitError(err) {
			t.Error("Expected IsCostLimitError to return false for non-CostLimitError")
		}
	})

	t.Run("returns false for nil", func(t *testing.T) {
		if IsCostLimitError(nil) {
			t.Error("Expected IsCostLimitError to return false for nil")
		}
	})
}

func TestAsCostLimitError(t *testing.T) {
	t.Run("extracts CostLimitError", func(t *testing.T) {
		original := NewCostLimitError("cost", 0.5, 0.6, "node_1")
		extracted, ok := AsCostLimitError(original)
		if !ok {
			t.Error("Expected AsCostLimitError to return true")
		}
		if extracted != original {
			t.Error("Expected extracted error to be the same as original")
		}
		if extracted.LimitValue != 0.5 {
			t.Errorf("Expected limit value 0.5, got %f", extracted.LimitValue)
		}
	})

	t.Run("returns false for other errors", func(t *testing.T) {
		err := errors.New("some other error")
		_, ok := AsCostLimitError(err)
		if ok {
			t.Error("Expected AsCostLimitError to return false for non-CostLimitError")
		}
	})

	t.Run("returns false for nil", func(t *testing.T) {
		_, ok := AsCostLimitError(nil)
		if ok {
			t.Error("Expected AsCostLimitError to return false for nil")
		}
	})
}
