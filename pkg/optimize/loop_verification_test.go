package optimize

import (
	"strings"
	"testing"
)

// The basic TestNormalizeVerifyMode and TestNormalizeReplayMode are already in loop_test.go.
// These tests cover additional edge cases not present there.

func TestNormalizeVerifyMode_AdditionalCases(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"  replay  ", "replay"},
		{"  FULL  ", "full"},
		{"strict", "replay"},      // "strict" is not "full", falls back to "replay"
		{"best_effort", "replay"}, // only "full" and "replay" are valid verify modes
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := normalizeVerifyMode(tc.input)
			if got != tc.want {
				t.Errorf("normalizeVerifyMode(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeReplayMode_AdditionalCases(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"  best_effort  ", "best_effort"},
		{"REQUIRED", "required"},
		{"STRICT", "required"},
		{"replay", "best_effort"}, // "replay" is not a replay mode, falls back
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got := normalizeReplayMode(tc.input)
			if got != tc.want {
				t.Errorf("normalizeReplayMode(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// Regression: ensure that all normalizeVerifyMode results are one of the two expected values.
func TestNormalizeVerifyMode_AlwaysKnownValue(t *testing.T) {
	inputs := []string{"replay", "full", "FULL", "", "garbage", "123", "best_effort", "strict"}
	valid := map[string]bool{"replay": true, "full": true}
	for _, input := range inputs {
		got := normalizeVerifyMode(input)
		if !valid[got] {
			t.Errorf("normalizeVerifyMode(%q) = %q, which is not a known value", input, got)
		}
	}
}

// Regression: ensure that all normalizeReplayMode results are one of the two expected values.
func TestNormalizeReplayMode_AlwaysKnownValue(t *testing.T) {
	inputs := []string{"best_effort", "best-effort", "required", "strict", "", "garbage", "replay", "REQUIRED"}
	valid := map[string]bool{"best_effort": true, "required": true}
	for _, input := range inputs {
		got := normalizeReplayMode(input)
		if !valid[got] {
			t.Errorf("normalizeReplayMode(%q) = %q, which is not a known value", input, got)
		}
	}
}

// Ensure the normalize functions do not produce values with uppercase or leading/trailing spaces.
func TestNormalizeVerifyMode_NormalizedOutput(t *testing.T) {
	for _, input := range []string{"REPLAY", "FULL", "unknown"} {
		got := normalizeVerifyMode(input)
		if got != strings.ToLower(strings.TrimSpace(got)) {
			t.Errorf("normalizeVerifyMode(%q) = %q, expected lowercase trimmed", input, got)
		}
	}
}

func TestNormalizeReplayMode_NormalizedOutput(t *testing.T) {
	for _, input := range []string{"BEST_EFFORT", "REQUIRED", "STRICT", "garbage"} {
		got := normalizeReplayMode(input)
		if got != strings.ToLower(strings.TrimSpace(got)) {
			t.Errorf("normalizeReplayMode(%q) = %q, expected lowercase trimmed", input, got)
		}
	}
}
