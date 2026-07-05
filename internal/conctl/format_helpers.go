package conctl

import (
	"fmt"
	"io"
	"strings"

	"github.com/alhasaniq/consortium/internal/conctl/output"
	"github.com/alhasaniq/consortium/pkg/bench"
)

// stringVal extracts a string from a map.
func stringVal(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// floatVal extracts a float64 from a map.
func floatVal(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return 0
}

// boolVal extracts a bool from a map.
func boolVal(m map[string]interface{}, key string) bool {
	if v, ok := m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// printTableAuto calls PrintTable or PrintMarkdownTable based on format.
func printTableAuto(w io.Writer, headers []string, rows [][]string, format string) {
	if format == "md" {
		output.PrintMarkdownTable(w, headers, rows)
	} else {
		output.PrintTable(w, headers, rows)
	}
}

// countItems returns the length of a JSON list field inside a map, or the
// length of the top-level list if data is []interface{}.
func countItems(data interface{}, keys ...string) int {
	if items, ok := data.([]interface{}); ok {
		return len(items)
	}
	if m, ok := data.(map[string]interface{}); ok {
		for _, key := range keys {
			if items, ok := m[key].([]interface{}); ok {
				return len(items)
			}
		}
	}
	return 0
}

// truncate shortens a string to maxLen, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// extractAnswer extracts a single-letter answer (A-Z) from node output using
// the same regex-based parser as the benchmark harness. Returns the full output
// when parsing fails so AI agents can interpret it themselves.
func extractAnswer(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return "-"
	}
	if label, ok := bench.ParseAnswerLabel(output); ok {
		return label
	}
	return output
}

// extractAnswerShort is like extractAnswer but truncates unparseable output
// for compact table display.
func extractAnswerShort(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return "-"
	}
	if label, ok := bench.ParseAnswerLabel(output); ok {
		return label
	}
	return truncate(output, 30)
}

// truncateModel shortens model names for table display.
func truncateModel(model string) string {
	model = strings.TrimPrefix(model, "openai/")
	model = strings.TrimPrefix(model, "anthropic/")
	model = strings.TrimPrefix(model, "google/")
	model = strings.TrimPrefix(model, "meta-llama/")
	model = strings.TrimPrefix(model, "deepseek/")
	return truncate(model, 25)
}

// truncateModelShort shortens model names for compact column display.
func truncateModelShort(model string) string {
	model = strings.TrimPrefix(model, "openai/")
	model = strings.TrimPrefix(model, "anthropic/")
	model = strings.TrimPrefix(model, "google/")
	model = strings.TrimPrefix(model, "meta-llama/")
	model = strings.TrimPrefix(model, "deepseek/")
	model = strings.TrimPrefix(model, "xiaomi/")
	model = strings.TrimPrefix(model, "x-ai/")
	return truncate(model, 20)
}

// formatModelWithLabel formats a model ID with its display label.
// Shows "model (label)" when both exist, falls back to just one.
func formatModelWithLabel(model, label string) string {
	m := truncateModel(model)
	l := truncate(label, 20)
	if m != "" && l != "" {
		return m + " (" + l + ")"
	}
	if m != "" {
		return m
	}
	return l
}

// formatCostVal formats a per-token cost value.
func formatCostVal(cost float64) string {
	if cost == 0 {
		return "$0"
	}
	if cost < 0.0001 {
		return fmt.Sprintf("$%.8f", cost)
	}
	if cost < 0.01 {
		return fmt.Sprintf("$%.6f", cost)
	}
	return fmt.Sprintf("$%.4f", cost)
}
