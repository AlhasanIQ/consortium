// Package typeconv provides type-conversion helpers for extracting typed values
// from interface{} (e.g. map[string]interface{} payloads from JSON).
package typeconv

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// AsString extracts a string from v. For string values it trims whitespace.
// For fmt.Stringer implementations it calls String(). Returns "" for nil/unknown.
func AsString(v interface{}) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case fmt.Stringer:
		return strings.TrimSpace(x.String())
	default:
		if v == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

// AsInt extracts an int from v, returning 0 for unrecognised types.
// Handles int, int32, int64, float32, float64, and json.Number.
func AsInt(v interface{}) int {
	n, _ := AsIntOK(v)
	return n
}

// AsIntOK extracts an int from v and reports whether it succeeded.
// Handles int, int32, int64, float32, float64, and json.Number.
func AsIntOK(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err == nil {
			return int(i), true
		}
		return 0, false
	default:
		return 0, false
	}
}

// AsNullableInt extracts an *int from v. Returns nil for unrecognised types.
// Additionally handles string values via strconv.Atoi.
func AsNullableInt(v interface{}) *int {
	switch x := v.(type) {
	case string:
		if parsed, err := strconv.Atoi(x); err == nil {
			return &parsed
		}
		return nil
	default:
		if n, ok := AsIntOK(v); ok {
			return &n
		}
		return nil
	}
}

// AsFloat64 extracts a float64 from v and reports whether it succeeded.
// Handles float64, float32, int, int32, int64, json.Number, and string.
func AsFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		return f, err == nil
	default:
		return 0, false
	}
}
