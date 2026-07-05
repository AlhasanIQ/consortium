package workflow

import "strings"

// NormalizeNovomoSandbox trims whitespace and applies the workflow default.
// It intentionally preserves unknown non-empty values so validation can reject
// them instead of silently coercing user input.
func NormalizeNovomoSandbox(sandbox string) string {
	normalized := strings.TrimSpace(sandbox)
	if normalized == "" {
		return DefaultNovomoSandbox
	}
	return normalized
}

// IsSupportedNovomoSandbox reports whether sandbox is accepted by the Novomo
// runtime API.
func IsSupportedNovomoSandbox(sandbox string) bool {
	switch strings.TrimSpace(sandbox) {
	case "host", "docker":
		return true
	default:
		return false
	}
}
