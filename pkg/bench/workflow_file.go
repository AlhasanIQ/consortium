package bench

import (
	"encoding/json"
	"fmt"
)

type WorkflowFile struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Nodes       []WorkflowNode `json:"nodes"`
	Edges       []WorkflowEdge `json:"edges"`
}

type WorkflowNode struct {
	ID   string           `json:"id"`
	Type string           `json:"type"`
	Data WorkflowNodeData `json:"data"`
}

type WorkflowNodeData struct {
	Type   string             `json:"type"`
	Label  string             `json:"label,omitempty"`
	Config WorkflowNodeConfig `json:"config"`
}

type WorkflowNodeConfig struct {
	Name              string                 `json:"name,omitempty"`
	Model             string                 `json:"model,omitempty"`
	SystemPrompt      string                 `json:"systemPrompt,omitempty"`
	UserPrompt        string                 `json:"userPrompt,omitempty"`
	Temperature       float64                `json:"temperature,omitempty"`
	MaxTokens         int                    `json:"maxTokens,omitempty"`
	OutputFormat      string                 `json:"outputFormat,omitempty"`
	AggregationMethod string                 `json:"aggregationMethod,omitempty"`
	AggregationConfig map[string]interface{} `json:"aggregationConfig,omitempty"`
}

type WorkflowEdge struct {
	ID     string `json:"id,omitempty"`
	Source string `json:"source"`
	Target string `json:"target"`
}

func ParseWorkflowFile(rawJSON string) (*WorkflowFile, error) {
	var wf WorkflowFile
	if err := json.Unmarshal([]byte(rawJSON), &wf); err != nil {
		return nil, fmt.Errorf("parse workflow definition: %w", err)
	}
	if len(wf.Nodes) == 0 {
		return nil, fmt.Errorf("workflow has no nodes")
	}
	return &wf, nil
}
