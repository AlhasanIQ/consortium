package conctl

import (
	"bytes"
	"flag"
	"testing"
)

func TestHelperStringVal(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]interface{}
		key  string
		want string
	}{
		{"string value", map[string]interface{}{"k": "hello"}, "k", "hello"},
		{"numeric value formatted", map[string]interface{}{"k": 42.0}, "k", "42"},
		{"bool value formatted", map[string]interface{}{"k": true}, "k", "true"},
		{"missing key", map[string]interface{}{"other": "x"}, "k", ""},
		{"nil map value", map[string]interface{}{"k": nil}, "k", "<nil>"},
		{"empty string", map[string]interface{}{"k": ""}, "k", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringVal(tt.m, tt.key)
			if got != tt.want {
				t.Errorf("stringVal(%v, %q) = %q, want %q", tt.m, tt.key, got, tt.want)
			}
		})
	}
}

func TestHelperFloatVal(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]interface{}
		key  string
		want float64
	}{
		{"float value", map[string]interface{}{"k": 3.14}, "k", 3.14},
		{"zero value", map[string]interface{}{"k": 0.0}, "k", 0},
		{"missing key", map[string]interface{}{}, "k", 0},
		{"wrong type string", map[string]interface{}{"k": "3.14"}, "k", 0},
		{"wrong type int", map[string]interface{}{"k": 42}, "k", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := floatVal(tt.m, tt.key)
			if got != tt.want {
				t.Errorf("floatVal(%v, %q) = %v, want %v", tt.m, tt.key, got, tt.want)
			}
		})
	}
}

func TestHelperBoolVal(t *testing.T) {
	tests := []struct {
		name string
		m    map[string]interface{}
		key  string
		want bool
	}{
		{"true", map[string]interface{}{"k": true}, "k", true},
		{"false", map[string]interface{}{"k": false}, "k", false},
		{"missing key", map[string]interface{}{}, "k", false},
		{"wrong type string", map[string]interface{}{"k": "true"}, "k", false},
		{"wrong type number", map[string]interface{}{"k": 1.0}, "k", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := boolVal(tt.m, tt.key)
			if got != tt.want {
				t.Errorf("boolVal(%v, %q) = %v, want %v", tt.m, tt.key, got, tt.want)
			}
		})
	}
}

func TestHelperTruncate(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		maxLen int
		want   string
	}{
		{"short string unchanged", "hello", 10, "hello"},
		{"exact length unchanged", "hello", 5, "hello"},
		{"truncated with ellipsis", "hello world", 8, "hello..."},
		{"maxLen 3 no ellipsis", "hello", 3, "hel"},
		{"maxLen 2 no ellipsis", "hello", 2, "he"},
		{"maxLen 1", "hello", 1, "h"},
		{"maxLen 0", "hello", 0, ""},
		{"empty string", "", 10, ""},
		{"maxLen 4 one char plus ellipsis", "hello", 4, "h..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.s, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestHelperTruncateModel(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  string
	}{
		{"openai prefix stripped", "openai/gpt-4o", "gpt-4o"},
		{"anthropic prefix stripped", "anthropic/claude-3-opus", "claude-3-opus"},
		{"google prefix stripped", "google/gemini-pro", "gemini-pro"},
		{"meta prefix stripped", "meta-llama/llama-3-70b", "llama-3-70b"},
		{"deepseek prefix stripped", "deepseek/deepseek-r1", "deepseek-r1"},
		{"unknown prefix kept", "mistral/mistral-large", "mistral/mistral-large"},
		{"no prefix", "gpt-4o", "gpt-4o"},
		{"long model truncated", "openai/this-is-a-very-long-model-name-that-exceeds-limit", "this-is-a-very-long-mo..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateModel(tt.model)
			if got != tt.want {
				t.Errorf("truncateModel(%q) = %q, want %q", tt.model, got, tt.want)
			}
		})
	}
}

func TestHelperTruncateModelShort(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  string
	}{
		{"xiaomi stripped", "xiaomi/megrez-3b", "megrez-3b"},
		{"x-ai stripped", "x-ai/grok-2", "grok-2"},
		{"long model truncated to 20", "openai/this-is-a-very-long-model-name", "this-is-a-very-lo..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateModelShort(tt.model)
			if got != tt.want {
				t.Errorf("truncateModelShort(%q) = %q, want %q", tt.model, got, tt.want)
			}
		})
	}
}

func TestHelperFormatModelWithLabel(t *testing.T) {
	tests := []struct {
		name  string
		model string
		label string
		want  string
	}{
		{"both present", "openai/gpt-4o", "primary", "gpt-4o (primary)"},
		{"model only", "openai/gpt-4o", "", "gpt-4o"},
		{"label only", "", "captain", "captain"},
		{"both empty", "", "", ""},
		{"long label truncated", "openai/gpt-4o", "this-is-a-very-long-label-name", "gpt-4o (this-is-a-very-lo...)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatModelWithLabel(tt.model, tt.label)
			if got != tt.want {
				t.Errorf("formatModelWithLabel(%q, %q) = %q, want %q", tt.model, tt.label, got, tt.want)
			}
		})
	}
}

func TestHelperFormatCostVal(t *testing.T) {
	tests := []struct {
		name string
		cost float64
		want string
	}{
		{"zero", 0, "$0"},
		{"very small", 0.00001, "$0.00001000"},
		{"small", 0.005, "$0.005000"},
		{"normal", 0.05, "$0.0500"},
		{"large", 1.50, "$1.5000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatCostVal(tt.cost)
			if got != tt.want {
				t.Errorf("formatCostVal(%v) = %q, want %q", tt.cost, got, tt.want)
			}
		})
	}
}

func TestHelperIsFlagSet(t *testing.T) {
	tests := []struct {
		name string
		args []string
		flag string
		want bool
	}{
		{"flag set", []string{"--limit", "10"}, "limit", true},
		{"flag not set", []string{}, "limit", false},
		{"other flag set", []string{"--verbose"}, "limit", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			fs.Int("limit", 50, "")
			fs.Bool("verbose", false, "")
			if err := fs.Parse(tt.args); err != nil {
				t.Fatal(err)
			}
			got := isFlagSet(fs, tt.flag)
			if got != tt.want {
				t.Errorf("isFlagSet(fs, %q) = %v, want %v", tt.flag, got, tt.want)
			}
		})
	}
}

func TestHelperCountItems(t *testing.T) {
	tests := []struct {
		name string
		data interface{}
		keys []string
		want int
	}{
		{"top-level list", []interface{}{1, 2, 3}, nil, 3},
		{"empty list", []interface{}{}, nil, 0},
		{"map with matching key", map[string]interface{}{
			"items": []interface{}{"a", "b"},
		}, []string{"items"}, 2},
		{"map first key matches", map[string]interface{}{
			"results": []interface{}{"a"},
			"items":   []interface{}{"a", "b", "c"},
		}, []string{"results", "items"}, 1},
		{"map second key matches", map[string]interface{}{
			"items": []interface{}{"a", "b", "c"},
		}, []string{"results", "items"}, 3},
		{"map no key matches", map[string]interface{}{
			"data": "not a list",
		}, []string{"items"}, 0},
		{"non-list non-map", "string", nil, 0},
		{"nil data", nil, []string{"items"}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countItems(tt.data, tt.keys...)
			if got != tt.want {
				t.Errorf("countItems(%v, %v) = %d, want %d", tt.data, tt.keys, got, tt.want)
			}
		})
	}
}

func TestHelperExtractAnswer(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{"empty string", "", "-"},
		{"whitespace only", "   ", "-"},
		{"single letter A", "A", "A"},
		{"answer prefix", "Answer: B", "B"},
		{"unparseable returns full", "some complex answer", "some complex answer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAnswer(tt.output)
			if got != tt.want {
				t.Errorf("extractAnswer(%q) = %q, want %q", tt.output, got, tt.want)
			}
		})
	}
}

func TestHelperExtractAnswerShort(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{"empty string", "", "-"},
		{"single letter", "C", "C"},
		{"long unparseable truncated", "42 is the result of computing 6 times 7 in most number systems", "42 is the result of computi..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAnswerShort(tt.output)
			if got != tt.want {
				t.Errorf("extractAnswerShort(%q) = %q, want %q", tt.output, got, tt.want)
			}
		})
	}
}

func TestHelperPrintTableAuto(t *testing.T) {
	headers := []string{"Name", "Value"}
	rows := [][]string{{"a", "1"}, {"b", "2"}}

	t.Run("table format", func(t *testing.T) {
		var buf bytes.Buffer
		printTableAuto(&buf, headers, rows, "table")
		out := buf.String()
		if out == "" {
			t.Error("expected non-empty table output")
		}
		// Should contain headers
		if !bytes.Contains(buf.Bytes(), []byte("Name")) {
			t.Error("expected table to contain header 'Name'")
		}
	})

	t.Run("md format", func(t *testing.T) {
		var buf bytes.Buffer
		printTableAuto(&buf, headers, rows, "md")
		out := buf.String()
		if out == "" {
			t.Error("expected non-empty markdown output")
		}
		// Markdown tables use pipe separators
		if !bytes.Contains(buf.Bytes(), []byte("|")) {
			t.Error("expected markdown table to contain pipe separators")
		}
	})
}
