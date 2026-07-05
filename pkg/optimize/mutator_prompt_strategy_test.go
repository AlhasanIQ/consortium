package optimize

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// ---------------------------------------------------------------------------
// sha256Hex
// ---------------------------------------------------------------------------

func TestSha256Hex_EmptyString(t *testing.T) {
	got := sha256Hex("")
	sum := sha256.Sum256([]byte(""))
	want := hex.EncodeToString(sum[:])
	if got != want {
		t.Errorf("sha256Hex(\"\") = %q, want %q", got, want)
	}
}

func TestSha256Hex_KnownInputs(t *testing.T) {
	tests := []struct {
		input string
	}{
		{"hello world"},
		{"You are a helpful assistant.\n\nPlease respond concisely."},
		{"abc123"},
		{"{\"revised_prompt\":\"test\"}"},
	}
	for _, tc := range tests {
		sum := sha256.Sum256([]byte(tc.input))
		want := hex.EncodeToString(sum[:])
		got := sha256Hex(tc.input)
		if got != want {
			t.Errorf("sha256Hex(%q) = %q, want %q", tc.input, got, want)
		}
	}
}

func TestSha256Hex_OutputLength(t *testing.T) {
	// SHA-256 produces 32 bytes → 64 hex characters.
	got := sha256Hex("any content here")
	if len(got) != 64 {
		t.Errorf("sha256Hex output length = %d, want 64", len(got))
	}
}

func TestSha256Hex_Deterministic(t *testing.T) {
	input := "determinism check"
	h1 := sha256Hex(input)
	h2 := sha256Hex(input)
	if h1 != h2 {
		t.Errorf("sha256Hex not deterministic: %q vs %q", h1, h2)
	}
}

func TestSha256Hex_DifferentInputsDifferentOutputs(t *testing.T) {
	h1 := sha256Hex("prompt version A")
	h2 := sha256Hex("prompt version B")
	if h1 == h2 {
		t.Error("different inputs produced the same hash (collision)")
	}
}

func TestSha256Hex_HexEncodedOnly(t *testing.T) {
	got := sha256Hex("test input")
	for _, c := range got {
		if c < '0' || (c > '9' && c < 'a') || c > 'f' {
			t.Errorf("sha256Hex output contains non-hex character: %c", c)
		}
	}
}
