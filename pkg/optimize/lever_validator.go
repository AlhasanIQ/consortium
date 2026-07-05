package optimize

import (
	"fmt"
	"strings"
)

var defaultForbiddenSubstrings = []string{
	"benchmark_runner",
	"admission",
	"worker_count",
	"max_concurrent_workflows",
	"api_key",
	"rate_limit",
	"split",
	"dataset",
}

var benchmarkSpecificPromptSignals = []string{
	"choose a/b/c/d",
	"choose a, b, c, or d",
	"single letter",
	"answer as a single letter",
	"multiple-choice",
	"mcqa",
	"option a",
	"option b",
	"option c",
	"option d",
}

// ValidateParamScope enforces optimizer boundary between workflow semantics and infra knobs.
func ValidateParamScope(param ParamDeclaration) error {
	path := strings.ToLower(strings.TrimSpace(param.Path))
	if path == "" {
		return fmt.Errorf("param path is empty")
	}
	// All optimizer paths must target workflow node config fields.
	if !strings.HasPrefix(path, "nodes[") || !strings.Contains(path, "].data.config.") {
		return fmt.Errorf("path %q is outside workflow node config scope", param.Path)
	}
	for _, forbidden := range defaultForbiddenSubstrings {
		if strings.Contains(path, forbidden) {
			return fmt.Errorf("path %q targets forbidden infra scope (%s)", param.Path, forbidden)
		}
	}
	return nil
}

// ValidatePromptIsolation blocks benchmark-specific content from L1 reasoning prompts.
func ValidatePromptIsolation(workflowID string, paramPath string, prompt string) error {
	if !strings.HasPrefix(strings.ToLower(workflowID), "reasoning-") {
		return nil
	}
	if !strings.Contains(strings.ToLower(paramPath), "prompt") {
		return nil
	}
	lower := strings.ToLower(prompt)
	for _, signal := range benchmarkSpecificPromptSignals {
		if strings.Contains(lower, signal) {
			return fmt.Errorf("benchmark-specific prompt content is not allowed in L1 seed prompts: %q", signal)
		}
	}
	return nil
}
