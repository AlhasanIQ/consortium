package conctl

import "testing"

func TestNormalizeBenchmarkItemID(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"row-17", "row-17"},
		{"Row-17", "row-17"},
		{"  row-17  ", "row-17"},
		{"17", "row-17"},
		{"001", "row-001"},
		{"item-17", "item-17"},
		{"", ""},
	}

	for _, tt := range tests {
		if got := normalizeBenchmarkItemID(tt.in); got != tt.want {
			t.Fatalf("normalizeBenchmarkItemID(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
