package runtime

import (
	"testing"
)

func TestJSONCodecV1_Encode(t *testing.T) {
	codec := &JSONCodecV1{}

	tests := []struct {
		name    string
		input   interface{}
		want    string
		wantErr bool
	}{
		{
			name:  "string value",
			input: "hello",
			want:  `"hello"`,
		},
		{
			name:  "integer value",
			input: 42,
			want:  "42",
		},
		{
			name:  "map value",
			input: map[string]string{"key": "value"},
			want:  `{"key":"value"}`,
		},
		{
			name:  "nil value",
			input: nil,
			want:  "null",
		},
		{
			name:  "nested struct",
			input: struct{ Name string }{Name: "test"},
			want:  `{"Name":"test"}`,
		},
		{
			name:    "unencodable value",
			input:   make(chan int),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := codec.Encode(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(data) != tt.want {
				t.Errorf("got %q, want %q", string(data), tt.want)
			}
		})
	}
}

func TestJSONCodecV1_Decode(t *testing.T) {
	codec := &JSONCodecV1{}

	t.Run("decode string", func(t *testing.T) {
		var s string
		if err := codec.Decode([]byte(`"hello"`), &s); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s != "hello" {
			t.Errorf("got %q, want %q", s, "hello")
		}
	})

	t.Run("decode map", func(t *testing.T) {
		var m map[string]interface{}
		if err := codec.Decode([]byte(`{"a":1,"b":"two"}`), &m); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m["b"] != "two" {
			t.Errorf("got %v, want %q", m["b"], "two")
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		var s string
		if err := codec.Decode([]byte("not json"), &s); err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}

func TestJSONCodecV1_RoundTrip(t *testing.T) {
	codec := &JSONCodecV1{}

	type payload struct {
		ID     string  `json:"id"`
		Score  float64 `json:"score"`
		Active bool    `json:"active"`
	}

	original := payload{ID: "abc-123", Score: 99.5, Active: true}

	data, err := codec.Encode(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var decoded payload
	if err := codec.Decode(data, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if decoded != original {
		t.Errorf("round-trip mismatch: got %+v, want %+v", decoded, original)
	}
}

func TestJSONCodecV1_Version(t *testing.T) {
	codec := &JSONCodecV1{}
	if v := codec.Version(); v != "json/v1" {
		t.Errorf("got version %q, want %q", v, "json/v1")
	}
}

func TestDefaultCodec(t *testing.T) {
	codec := DefaultCodec()
	if codec == nil {
		t.Fatal("DefaultCodec returned nil")
	}
	if codec.Version() != "json/v1" {
		t.Errorf("default codec version = %q, want %q", codec.Version(), "json/v1")
	}

	// Verify it implements PayloadCodec by using it
	data, err := codec.Encode("test")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var s string
	if err := codec.Decode(data, &s); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if s != "test" {
		t.Errorf("got %q, want %q", s, "test")
	}
}
