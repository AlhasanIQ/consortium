package optimize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type ClaudeMutationOutput struct {
	RevisedPrompt  string `json:"revised_prompt"`
	Reasoning      string `json:"reasoning"`
	ChangesSummary string `json:"changes_summary"`
}

type ClaudePromptMutationResponse struct {
	Parsed     ClaudeMutationOutput
	RawOutput  string
	Model      string
	CLIVersion string
}

type ClaudePromptCandidate struct {
	RevisedPrompt  string  `json:"revised_prompt"`
	Reasoning      string  `json:"reasoning,omitempty"`
	ChangesSummary string  `json:"changes_summary,omitempty"`
	Score          float64 `json:"score,omitempty"`
}

type ClaudePromptCandidatesResponse struct {
	Candidates []ClaudePromptCandidate
	RawOutput  string
	Model      string
	CLIVersion string
}

func InvokeClaude(ctx context.Context, model string, prompt string) (string, error) {
	// TODO(v0.1): Claude CLI is the only LLM mutator backend today. Keep this
	// documented as an optional optimizer dependency until a provider interface
	// is added.
	model = strings.TrimSpace(model)
	if model == "" {
		model = "opus"
	}
	cmd := exec.CommandContext(ctx, "claude", "--print", "--model", model, "--max-turns", "1", prompt)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude CLI failed: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return "", fmt.Errorf("claude CLI returned empty output")
	}
	return out, nil
}

func InvokeClaudePromptMutation(ctx context.Context, model string, prompt string) (*ClaudePromptMutationResponse, error) {
	raw, err := InvokeClaude(ctx, model, prompt)
	if err != nil {
		return nil, err
	}

	jsonBlob := extractFirstJSONValue(raw)
	if jsonBlob == "" {
		return nil, fmt.Errorf("claude output did not contain JSON object")
	}

	var parsed ClaudeMutationOutput
	if err := json.Unmarshal([]byte(jsonBlob), &parsed); err != nil {
		return nil, fmt.Errorf("parse claude JSON: %w", err)
	}
	if strings.TrimSpace(parsed.RevisedPrompt) == "" {
		return nil, fmt.Errorf("claude response missing revised_prompt")
	}
	version := ""
	versionCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	version = strings.TrimSpace(InvokeClaudeVersion(versionCtx))
	return &ClaudePromptMutationResponse{
		Parsed:     parsed,
		RawOutput:  raw,
		Model:      strings.TrimSpace(model),
		CLIVersion: version,
	}, nil
}

func InvokeClaudePromptCandidates(ctx context.Context, model string, prompt string, maxCandidates int) (*ClaudePromptCandidatesResponse, error) {
	raw, err := InvokeClaude(ctx, model, prompt)
	if err != nil {
		return nil, err
	}
	jsonBlob := extractFirstJSONValue(raw)
	if jsonBlob == "" {
		return nil, fmt.Errorf("claude output did not contain JSON candidates")
	}
	type candidatesEnvelope struct {
		Candidates []ClaudePromptCandidate `json:"candidates"`
	}

	candidates := make([]ClaudePromptCandidate, 0)
	appendIfValid := func(items []ClaudePromptCandidate) {
		for _, item := range items {
			prompt := strings.TrimSpace(item.RevisedPrompt)
			if prompt == "" {
				continue
			}
			item.RevisedPrompt = prompt
			candidates = append(candidates, item)
		}
	}

	var envelope candidatesEnvelope
	if err := json.Unmarshal([]byte(jsonBlob), &envelope); err == nil && len(envelope.Candidates) > 0 {
		appendIfValid(envelope.Candidates)
	} else {
		var list []ClaudePromptCandidate
		if err := json.Unmarshal([]byte(jsonBlob), &list); err == nil && len(list) > 0 {
			appendIfValid(list)
		} else {
			var single ClaudePromptCandidate
			if err := json.Unmarshal([]byte(jsonBlob), &single); err == nil && strings.TrimSpace(single.RevisedPrompt) != "" {
				appendIfValid([]ClaudePromptCandidate{single})
			}
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("claude response missing candidate revised_prompt values")
	}
	if maxCandidates > 0 && len(candidates) > maxCandidates {
		candidates = candidates[:maxCandidates]
	}
	version := ""
	versionCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	version = strings.TrimSpace(InvokeClaudeVersion(versionCtx))
	return &ClaudePromptCandidatesResponse{
		Candidates: candidates,
		RawOutput:  raw,
		Model:      strings.TrimSpace(model),
		CLIVersion: version,
	}, nil
}

func InvokeClaudeVersion(ctx context.Context) string {
	cmd := exec.CommandContext(ctx, "claude", "--version")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(stdout.String())
}

func extractFirstJSONValue(raw string) string {
	objStart := strings.Index(raw, "{")
	arrStart := strings.Index(raw, "[")
	var start int
	switch {
	case objStart >= 0 && arrStart >= 0:
		if objStart < arrStart {
			start = objStart
		} else {
			start = arrStart
		}
	case objStart >= 0:
		start = objStart
	case arrStart >= 0:
		start = arrStart
	default:
		return ""
	}
	depth := 0
	var openByte byte
	var closeByte byte
	switch raw[start] {
	case '{':
		openByte, closeByte = '{', '}'
	case '[':
		openByte, closeByte = '[', ']'
	default:
		return ""
	}
	inString := false
	escaped := false
	for i := start; i < len(raw); i++ {
		ch := raw[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case openByte:
			depth++
		case closeByte:
			depth--
			if depth == 0 {
				return raw[start : i+1]
			}
		}
	}
	return ""
}

func extractFirstJSONObject(raw string) string {
	value := extractFirstJSONValue(raw)
	if strings.HasPrefix(strings.TrimSpace(value), "{") {
		return value
	}
	return ""
}
