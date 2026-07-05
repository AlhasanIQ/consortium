package benchloop

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TranscriptMessage is a high-level extracted message from a Claude transcript.
type TranscriptMessage struct {
	Role      string `json:"role"`
	Text      string `json:"text"`
	Timestamp string `json:"timestamp,omitempty"`
}

// TranscriptReadOptions controls transcript extraction behavior.
type TranscriptReadOptions struct {
	IncludeUser  bool
	IncludeTools bool
	MaxMessages  int
}

// ClaudeTranscriptPath resolves the global Claude transcript path for a session.
// TODO(v0.1): Make transcript providers/path roots configurable if benchloop
// is expected to support non-Claude agents or non-default CLI history layouts.
func ClaudeTranscriptPath(workdir, claudeSessionID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	projectSlug := claudeProjectSlug(workdir)
	return filepath.Join(home, ".claude", "projects", projectSlug, claudeSessionID+".jsonl"), nil
}

func claudeProjectSlug(workdir string) string {
	slug := filepath.Clean(workdir)
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-")
	return replacer.Replace(slug)
}

// ReadTranscriptMessages extracts high-level text messages from a Claude session JSONL.
func ReadTranscriptMessages(path string, opts TranscriptReadOptions) ([]TranscriptMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open transcript: %w", err)
	}
	defer func() { _ = f.Close() }()

	type contentPart struct {
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		Name      string          `json:"name,omitempty"`
		ID        string          `json:"id,omitempty"`
		ToolUseID string          `json:"tool_use_id,omitempty"`
		Input     json.RawMessage `json:"input,omitempty"`
		Content   json.RawMessage `json:"content,omitempty"`
	}
	type messageEnvelope struct {
		Type      string `json:"type"`
		Timestamp string `json:"timestamp"`
		Message   struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 8*1024*1024)

	messages := make([]TranscriptMessage, 0, 64)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var ev messageEnvelope
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}

		role := strings.TrimSpace(ev.Message.Role)
		if role == "" {
			role = strings.TrimSpace(ev.Type)
		}
		if role != "assistant" && role != "user" {
			continue
		}

		// Parse content array. User tool_result messages sometimes have
		// content as a plain string; handle both forms.
		var parts []contentPart
		if len(ev.Message.Content) > 0 {
			switch ev.Message.Content[0] {
			case '[':
				_ = json.Unmarshal(ev.Message.Content, &parts)
			case '"':
				var s string
				if json.Unmarshal(ev.Message.Content, &s) == nil {
					parts = []contentPart{{Type: "text", Text: s}}
				}
			}
		}

		for _, part := range parts {
			switch part.Type {
			case "text":
				if role == "user" && !opts.IncludeUser {
					continue
				}
				text := strings.TrimSpace(part.Text)
				if text == "" {
					continue
				}
				messages = append(messages, TranscriptMessage{
					Role:      role,
					Text:      text,
					Timestamp: ev.Timestamp,
				})

			case "tool_use":
				if !opts.IncludeTools {
					continue
				}
				messages = append(messages, TranscriptMessage{
					Role:      "tool_call",
					Text:      FormatToolCall(part.Name, part.Input),
					Timestamp: ev.Timestamp,
				})

			case "tool_result":
				if !opts.IncludeTools {
					continue
				}
				messages = append(messages, TranscriptMessage{
					Role:      "tool_result",
					Text:      formatToolResult(part.ToolUseID, part.Content),
					Timestamp: ev.Timestamp,
				})
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan transcript: %w", err)
	}

	if opts.MaxMessages > 0 && len(messages) > opts.MaxMessages {
		messages = messages[len(messages)-opts.MaxMessages:]
	}
	return messages, nil
}

// FormatToolCall returns a human-readable one-liner for a tool_use block.
func FormatToolCall(name string, input json.RawMessage) string {
	var fields struct {
		Command     string `json:"command"`
		Description string `json:"description"`
		Pattern     string `json:"pattern"`
		FilePath    string `json:"file_path"`
		Query       string `json:"query"`
		URL         string `json:"url"`
		OldString   string `json:"old_string"`
		NewString   string `json:"new_string"`
	}
	_ = json.Unmarshal(input, &fields)

	// Bash: show description + command in backticks.
	if fields.Command != "" {
		if fields.Description != "" {
			return fmt.Sprintf("[%s (`%s`)] %s", name, fields.Command, fields.Description)
		}
		return fmt.Sprintf("[%s] `%s`", name, fields.Command)
	}

	var detail string
	switch {
	case fields.FilePath != "":
		detail = fields.FilePath
	case fields.Pattern != "":
		detail = fields.Pattern
	case fields.Query != "":
		detail = fields.Query
	case fields.URL != "":
		detail = fields.URL
	case fields.Description != "":
		detail = fields.Description
	}

	if detail == "" {
		return fmt.Sprintf("[%s]", name)
	}
	return fmt.Sprintf("[%s] %s", name, detail)
}

func formatToolResult(_ string, content json.RawMessage) string {
	// Content can be a string or a structured object.
	var s string
	if json.Unmarshal(content, &s) == nil && s != "" {
		return s
	}
	// Fall back to raw content as string.
	return string(content)
}
