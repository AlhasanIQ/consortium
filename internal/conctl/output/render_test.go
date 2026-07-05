package output

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestRenderJSON(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]string{"key": "value"}
	err := Render(&buf, "json", data, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}
	if got["key"] != "value" {
		t.Errorf("expected key=value, got %v", got)
	}
}

func TestRenderJSONL(t *testing.T) {
	var buf bytes.Buffer
	data := []interface{}{
		map[string]string{"a": "1"},
		map[string]string{"b": "2"},
	}
	err := Render(&buf, "jsonl", data, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d: %q", len(lines), buf.String())
	}
}

func TestRenderTable(t *testing.T) {
	var buf bytes.Buffer
	called := false
	err := Render(&buf, "table", "data", func(w io.Writer, d interface{}, format string) {
		called = true
		PrintTable(w, []string{"A", "B"}, [][]string{{"1", "2"}, {"3", "4"}})
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("table function was not called")
	}
	if !strings.Contains(buf.String(), "A") {
		t.Errorf("expected table output, got %q", buf.String())
	}
}

func TestPrintTable(t *testing.T) {
	var buf bytes.Buffer
	PrintTable(&buf, []string{"ID", "NAME"}, [][]string{
		{"1", "alice"},
		{"2", "bob"},
	})
	out := buf.String()
	if !strings.Contains(out, "ID") {
		t.Error("missing header")
	}
	if !strings.Contains(out, "alice") {
		t.Error("missing data")
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 { // header + separator + 2 rows
		t.Errorf("expected 4 lines, got %d", len(lines))
	}
}

func TestPrintMarkdownTable(t *testing.T) {
	var buf bytes.Buffer
	PrintMarkdownTable(&buf, []string{"ID", "NAME"}, [][]string{
		{"1", "alice"},
	})
	out := buf.String()
	if !strings.Contains(out, "|") {
		t.Error("missing markdown pipe")
	}
	if !strings.Contains(out, "---") {
		t.Error("missing separator")
	}
}

func TestRenderTable_FallsBackToJSON(t *testing.T) {
	var buf bytes.Buffer
	err := Render(&buf, "table", map[string]string{"a": "b"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should fall back to JSON when no table function provided.
	if !strings.Contains(buf.String(), `"a"`) {
		t.Errorf("expected JSON fallback, got %q", buf.String())
	}
}
