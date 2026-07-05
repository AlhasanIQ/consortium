package workflow

import "testing"

func TestNormalizeNovomoSandbox(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty defaults to docker", in: "", want: "docker"},
		{name: "whitespace defaults to docker", in: "   ", want: "docker"},
		{name: "trims explicit docker", in: " docker ", want: "docker"},
		{name: "preserves explicit host", in: "host", want: "host"},
		{name: "preserves unknown for validator", in: " vm ", want: "vm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeNovomoSandbox(tt.in); got != tt.want {
				t.Fatalf("NormalizeNovomoSandbox(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsSupportedNovomoSandbox(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{in: "host", want: true},
		{in: " docker ", want: true},
		{in: "", want: false},
		{in: "vm", want: false},
	}

	for _, tt := range tests {
		if got := IsSupportedNovomoSandbox(tt.in); got != tt.want {
			t.Fatalf("IsSupportedNovomoSandbox(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
