package workflow

import (
	"encoding/json"
	"sort"
	"strings"
)

// ExtractInputPromptFromRequestData extracts a display prompt from persisted
// workflow request_data. It handles both full Workflow JSON and legacy context
// payloads stored at the top level.
func ExtractInputPromptFromRequestData(requestData string) string {
	context := ExtractContextFromRequestData(requestData)
	if len(context) > 0 {
		for _, key := range []string{"user_prompt", "input", "prompt", "query", "user_input", "text", "message"} {
			if val, ok := context[key]; ok {
				if str, ok := val.(string); ok && str != "" {
					return str
				}
			}
		}
	}

	if prompt := extractWorkflowNodePrompt(requestData); prompt != "" {
		return prompt
	}

	if len(context) > 0 {
		return firstStringValue(context)
	}

	return ""
}

// ExtractContextFromRequestData returns request context values from request_data.
func ExtractContextFromRequestData(requestData string) map[string]interface{} {
	if strings.TrimSpace(requestData) == "" {
		return nil
	}

	var payload struct {
		Context map[string]interface{} `json:"context"`
	}
	if err := json.Unmarshal([]byte(requestData), &payload); err == nil && len(payload.Context) > 0 {
		return payload.Context
	}

	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(requestData), &raw); err != nil || len(raw) == 0 {
		return nil
	}
	if context, ok := raw["context"].(map[string]interface{}); ok && len(context) > 0 {
		return context
	}

	return raw
}

func extractWorkflowNodePrompt(requestData string) string {
	if strings.TrimSpace(requestData) == "" {
		return ""
	}

	var wf Workflow
	if err := json.Unmarshal([]byte(requestData), &wf); err != nil {
		return ""
	}

	for _, node := range wf.Nodes {
		if node == nil {
			continue
		}
		if prompt := strings.TrimSpace(node.Prompt); prompt != "" {
			return prompt
		}
	}
	return ""
}

func firstStringValue(values map[string]interface{}) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		if str, ok := values[key].(string); ok && str != "" {
			return str
		}
	}
	return ""
}
