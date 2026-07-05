package typeconv

import (
	"encoding/json"
	"fmt"
	"testing"
)

// stringer implements fmt.Stringer for testing.
type stringer struct{ s string }

func (s stringer) String() string { return s.s }

func TestAsString(t *testing.T) {
	tests := []struct {
		name string
		in   interface{}
		want string
	}{
		{"nil", nil, ""},
		{"empty string", "", ""},
		{"plain string", "hello", "hello"},
		{"whitespace trimmed", "  hello  ", "hello"},
		{"int", 42, "42"},
		{"float", 3.14, "3.14"},
		{"bool", true, "true"},
		{"stringer", stringer{"world"}, "world"},
		{"stringer with whitespace", stringer{"  trimmed  "}, "trimmed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AsString(tt.in)
			if got != tt.want {
				t.Errorf("AsString(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestAsInt(t *testing.T) {
	tests := []struct {
		name string
		in   interface{}
		want int
	}{
		{"nil", nil, 0},
		{"int", 7, 7},
		{"int32", int32(32), 32},
		{"int64", int64(64), 64},
		{"float64", float64(3.9), 3},
		{"float32", float32(2.1), 2},
		{"json.Number valid", json.Number("99"), 99},
		{"json.Number invalid", json.Number("not_a_number"), 0},
		{"string (unsupported)", "42", 0},
		{"bool (unsupported)", true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AsInt(tt.in)
			if got != tt.want {
				t.Errorf("AsInt(%v) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestAsIntOK(t *testing.T) {
	tests := []struct {
		name   string
		in     interface{}
		wantN  int
		wantOK bool
	}{
		{"nil", nil, 0, false},
		{"int", 5, 5, true},
		{"int32", int32(10), 10, true},
		{"int64", int64(20), 20, true},
		{"float64", float64(9.7), 9, true},
		{"float32", float32(4.2), 4, true},
		{"json.Number valid", json.Number("123"), 123, true},
		{"json.Number float string", json.Number("1.5"), 0, false},
		{"json.Number invalid", json.Number("abc"), 0, false},
		{"string", "42", 0, false},
		{"bool", false, 0, false},
		{"slice", []int{1}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, ok := AsIntOK(tt.in)
			if n != tt.wantN || ok != tt.wantOK {
				t.Errorf("AsIntOK(%v) = (%d, %v), want (%d, %v)", tt.in, n, ok, tt.wantN, tt.wantOK)
			}
		})
	}
}

func TestAsNullableInt(t *testing.T) {
	tests := []struct {
		name    string
		in      interface{}
		wantNil bool
		wantVal int
	}{
		{"nil", nil, true, 0},
		{"int", 42, false, 42},
		{"int64", int64(100), false, 100},
		{"float64", float64(7.9), false, 7},
		{"string valid", "55", false, 55},
		{"string invalid", "nope", true, 0},
		{"string empty", "", true, 0},
		{"bool", true, true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AsNullableInt(tt.in)
			if tt.wantNil {
				if got != nil {
					t.Errorf("AsNullableInt(%v) = %d, want nil", tt.in, *got)
				}
			} else {
				if got == nil {
					t.Errorf("AsNullableInt(%v) = nil, want %d", tt.in, tt.wantVal)
				} else if *got != tt.wantVal {
					t.Errorf("AsNullableInt(%v) = %d, want %d", tt.in, *got, tt.wantVal)
				}
			}
		})
	}
}

func TestAsFloat64(t *testing.T) {
	tests := []struct {
		name   string
		in     interface{}
		wantF  float64
		wantOK bool
	}{
		{"nil", nil, 0, false},
		{"float64", float64(3.14), 3.14, true},
		{"float32", float32(2.5), 2.5, true},
		{"int", 10, 10.0, true},
		{"int32", int32(20), 20.0, true},
		{"int64", int64(30), 30.0, true},
		{"json.Number", json.Number("1.618"), 1.618, true},
		{"json.Number invalid", json.Number("xyz"), 0, false},
		{"string valid", "99.9", 99.9, true},
		{"string with whitespace", "  42.0  ", 42.0, true},
		{"string invalid", "not_float", 0, false},
		{"string empty", "", 0, false},
		{"bool", true, 0, false},
		{"slice", []int{}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, ok := AsFloat64(tt.in)
			if ok != tt.wantOK {
				t.Errorf("AsFloat64(%v) ok = %v, want %v", tt.in, ok, tt.wantOK)
			}
			if tt.wantOK {
				// Use tolerance for float32 conversions
				diff := f - tt.wantF
				if diff < -0.01 || diff > 0.01 {
					t.Errorf("AsFloat64(%v) = %f, want %f", tt.in, f, tt.wantF)
				}
			} else if f != 0 {
				t.Errorf("AsFloat64(%v) = %f on failure, want 0", tt.in, f)
			}
		})
	}
}

func TestAsStringFormats(t *testing.T) {
	// Verify that non-string, non-Stringer types use fmt.Sprintf("%v", v).
	type custom struct{ X int }
	got := AsString(custom{X: 5})
	want := fmt.Sprintf("%v", custom{X: 5})
	if got != want {
		t.Errorf("AsString(custom{5}) = %q, want %q", got, want)
	}
}
