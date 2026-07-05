package benchloop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeProjectSlug(t *testing.T) {
	tests := []struct {
		workdir string
		want    string
	}{
		{"/home/user/projects/consortium", "-home-user-projects-consortium"},
		{"/workspaces/alice/dev/myapp", "-workspaces-alice-dev-myapp"},
		// Clean normalizes trailing slashes before replacement.
		{"/home/user/project/", "-home-user-project"},
	}
	for _, tt := range tests {
		got := claudeProjectSlug(tt.workdir)
		if got != tt.want {
			t.Errorf("claudeProjectSlug(%q) = %q, want %q", tt.workdir, got, tt.want)
		}
	}
}

func TestFormatToolCall_BashWithDescription(t *testing.T) {
	input, _ := json.Marshal(map[string]string{
		"command":     "ls -la",
		"description": "List directory contents",
	})
	got := FormatToolCall("Bash", input)
	want := "[Bash (`ls -la`)] List directory contents"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatToolCall_BashWithoutDescription(t *testing.T) {
	input, _ := json.Marshal(map[string]string{
		"command": "go test ./...",
	})
	got := FormatToolCall("Bash", input)
	want := "[Bash] `go test ./...`"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatToolCall_FilePathTool(t *testing.T) {
	input, _ := json.Marshal(map[string]string{
		"file_path": "/src/main.go",
	})
	got := FormatToolCall("Read", input)
	want := "[Read] /src/main.go"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatToolCall_PatternTool(t *testing.T) {
	input, _ := json.Marshal(map[string]string{
		"pattern": "**/*.go",
	})
	got := FormatToolCall("Glob", input)
	want := "[Glob] **/*.go"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatToolCall_QueryTool(t *testing.T) {
	input, _ := json.Marshal(map[string]string{
		"query": "TestMain",
	})
	got := FormatToolCall("Grep", input)
	want := "[Grep] TestMain"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatToolCall_URLTool(t *testing.T) {
	input, _ := json.Marshal(map[string]string{
		"url": "https://example.com",
	})
	got := FormatToolCall("WebFetch", input)
	want := "[WebFetch] https://example.com"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatToolCall_NoKnownFields(t *testing.T) {
	// When no recognized fields are present, just show the tool name.
	input, _ := json.Marshal(map[string]string{"unknown": "value"})
	got := FormatToolCall("CustomTool", input)
	want := "[CustomTool]"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatToolCall_DescriptionFallback(t *testing.T) {
	// description is used as fallback when no command/path/pattern/query/url.
	input, _ := json.Marshal(map[string]string{
		"description": "Create new file",
	})
	got := FormatToolCall("Write", input)
	want := "[Write] Create new file"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReadTranscriptMessages_AssistantOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")

	lines := []string{
		`{"type":"message","timestamp":"2024-01-01T00:00:00Z","message":{"role":"assistant","content":[{"type":"text","text":"Hello there"}]}}`,
		`{"type":"message","timestamp":"2024-01-01T00:00:01Z","message":{"role":"user","content":[{"type":"text","text":"Hi back"}]}}`,
	}
	content := strings.Join(lines, "\n")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	opts := TranscriptReadOptions{IncludeUser: false}
	msgs, err := ReadTranscriptMessages(path, opts)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "assistant" {
		t.Errorf("expected assistant, got %q", msgs[0].Role)
	}
	if msgs[0].Text != "Hello there" {
		t.Errorf("expected 'Hello there', got %q", msgs[0].Text)
	}
}

func TestReadTranscriptMessages_IncludeUser(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")

	lines := []string{
		`{"type":"message","timestamp":"2024-01-01T00:00:00Z","message":{"role":"assistant","content":[{"type":"text","text":"A"}]}}`,
		`{"type":"message","timestamp":"2024-01-01T00:00:01Z","message":{"role":"user","content":[{"type":"text","text":"U"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatal(err)
	}

	opts := TranscriptReadOptions{IncludeUser: true}
	msgs, err := ReadTranscriptMessages(path, opts)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
}

func TestReadTranscriptMessages_MaxMessages(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")

	var lines []string
	for i := 0; i < 10; i++ {
		lines = append(lines, `{"type":"message","timestamp":"2024-01-01T00:00:00Z","message":{"role":"assistant","content":[{"type":"text","text":"msg"}]}}`)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatal(err)
	}

	opts := TranscriptReadOptions{MaxMessages: 3}
	msgs, err := ReadTranscriptMessages(path, opts)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages (MaxMessages=3), got %d", len(msgs))
	}
}

func TestReadTranscriptMessages_MissingFile(t *testing.T) {
	_, err := ReadTranscriptMessages("/nonexistent/path.jsonl", TranscriptReadOptions{})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
