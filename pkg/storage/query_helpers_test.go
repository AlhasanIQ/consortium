package storage

import (
	"testing"
)

func TestNormalizeNonEmptyIDs(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{"nil input", nil, nil},
		{"empty slice", []string{}, nil},
		{"all empty", []string{"", "  ", "	"}, nil},
		{"trims whitespace", []string{" a ", "  b  "}, []string{"a", "b"}},
		{"deduplicates", []string{"a", "a", "b"}, []string{"a", "b"}},
		{"dedup after trim", []string{" a", "a ", "b"}, []string{"a", "b"}},
		{"drops empty among valid", []string{"a", "", "b", "  "}, []string{"a", "b"}},
		{"preserves order", []string{"c", "a", "b"}, []string{"c", "a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeNonEmptyIDs(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d (got %v)", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestInClausePlaceholders(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, ""},
		{-1, ""},
		{1, "?"},
		{3, "?,?,?"},
		{5, "?,?,?,?,?"},
	}
	for _, tt := range tests {
		got := inClausePlaceholders(tt.n)
		if got != tt.want {
			t.Errorf("inClausePlaceholders(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
