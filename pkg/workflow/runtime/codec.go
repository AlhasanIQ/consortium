package runtime

import (
	"encoding/json"
	"fmt"
)

// PayloadCodec encodes and decodes payloads for durable storage.
// The default implementation uses plain JSON. Future implementations
// may add compression or encryption without schema changes.
type PayloadCodec interface {
	Encode(payload interface{}) ([]byte, error)
	Decode(data []byte, target interface{}) error
	Version() string
}

// JSONCodecV1 is the default payload codec using plain JSON serialization.
type JSONCodecV1 struct{}

// Encode serializes a payload to JSON bytes.
func (c *JSONCodecV1) Encode(payload interface{}) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("json encode: %w", err)
	}
	return data, nil
}

// Decode deserializes JSON bytes into the target.
func (c *JSONCodecV1) Decode(data []byte, target interface{}) error {
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("json decode: %w", err)
	}
	return nil
}

// Version returns the codec version identifier.
func (c *JSONCodecV1) Version() string {
	return "json/v1"
}

// DefaultCodec returns the default payload codec.
func DefaultCodec() PayloadCodec {
	return &JSONCodecV1{}
}
