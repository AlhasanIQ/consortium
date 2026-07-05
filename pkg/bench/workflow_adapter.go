package bench

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/alhasaniq/consortium/pkg/workflow"
)

var interpolationVarRegex = regexp.MustCompile(`\{\{(\w+)\}\}`)

func BuildExecutionWorkflow(file *WorkflowFile, userPrompt string) (*workflow.Workflow, error) {
	if file == nil {
		return nil, fmt.Errorf("workflow file is nil")
	}
	if strings.TrimSpace(userPrompt) == "" {
		return nil, fmt.Errorf("user prompt is empty")
	}

	agentNodes := make([]*workflow.Node, 0, len(file.Nodes))
	agentIDs := make([]string, 0, len(file.Nodes))
	var resultNode *WorkflowNode

	for i := range file.Nodes {
		node := file.Nodes[i]
		nodeType := effectiveNodeType(node)
		switch nodeType {
		case "agent":
			wfNode, err := agentNodeToWorkflowNode(node, userPrompt)
			if err != nil {
				return nil, err
			}
			agentNodes = append(agentNodes, wfNode)
			agentIDs = append(agentIDs, node.ID)
		case "result":
			n := node
			resultNode = &n
		}
	}

	if len(agentNodes) == 0 {
		return nil, fmt.Errorf("workflow %q has no agent nodes", file.ID)
	}

	resultNodeID := "result"
	if resultNode != nil && strings.TrimSpace(resultNode.ID) != "" {
		resultNodeID = resultNode.ID
	}

	resultNode2, err := buildResultNode(resultNode, resultNodeID, agentIDs, userPrompt)
	if err != nil {
		return nil, err
	}

	edges := make([]*workflow.Edge, 0, len(agentIDs))
	for _, agentID := range agentIDs {
		edges = append(edges, &workflow.Edge{
			Source: agentID,
			Target: resultNodeID,
		})
	}

	wfID := strings.TrimSpace(file.ID)
	if wfID == "" {
		wfID = "benchmark-workflow"
	}

	wfName := strings.TrimSpace(file.Name)
	if wfName == "" {
		wfName = wfID
	}

	nodes := append(agentNodes, resultNode2)
	return &workflow.Workflow{
		ID:    wfID,
		Name:  wfName,
		Nodes: nodes,
		Edges: edges,
		Context: map[string]interface{}{
			"user_prompt": userPrompt,
		},
	}, nil
}

func effectiveNodeType(node WorkflowNode) string {
	dataType := strings.TrimSpace(node.Data.Type)
	if dataType != "" {
		return dataType
	}
	return strings.TrimSpace(node.Type)
}

func agentNodeToWorkflowNode(src WorkflowNode, userPrompt string) (*workflow.Node, error) {
	model := strings.TrimSpace(src.Data.Config.Model)
	if model == "" {
		return nil, fmt.Errorf("agent node %q has empty model", src.ID)
	}

	prompt := interpolatePrompt(src.Data.Config.UserPrompt, userPrompt)
	if strings.TrimSpace(prompt) == "" {
		prompt = userPrompt
	}

	node := &workflow.Node{
		ID:           src.ID,
		Type:         workflow.NodeTypePrompt,
		Model:        model,
		Prompt:       prompt,
		SystemPrompt: interpolatePrompt(src.Data.Config.SystemPrompt, userPrompt),
		Metadata: map[string]interface{}{
			"name": src.Data.Config.Name,
		},
	}
	if src.Data.Config.Temperature > 0 {
		t := src.Data.Config.Temperature
		node.Temperature = &t
	}
	if src.Data.Config.MaxTokens > 0 {
		node.MaxTokens = src.Data.Config.MaxTokens
	}
	return node, nil
}

func buildResultNode(src *WorkflowNode, nodeID string, inputIDs []string, userPrompt string) (*workflow.Node, error) {
	node := &workflow.Node{
		ID:                nodeID,
		Type:              workflow.NodeTypeResult,
		OutputName:        nodeID,
		AggregationMethod: workflow.AggMethodCollect,
		Metadata: map[string]interface{}{
			"input_ids": inputIDs,
		},
	}

	if src == nil {
		return node, nil
	}

	cfg := src.Data.Config
	if name := strings.TrimSpace(cfg.Name); name != "" {
		node.OutputName = name
	}
	if outputFormat := strings.TrimSpace(cfg.OutputFormat); outputFormat != "" {
		node.OutputFormat = outputFormat
	}

	if method := strings.TrimSpace(cfg.AggregationMethod); method != "" {
		aggMethod := workflow.AggregationMethod(method)
		if _, ok := workflow.NewAggregatorRegistry().Get(aggMethod); !ok {
			return nil, fmt.Errorf("result node %q has unsupported aggregation method %q", src.ID, method)
		}
		node.AggregationMethod = aggMethod
	}

	if len(cfg.AggregationConfig) > 0 {
		node.AggregationConfig = workflow.DeepCopyContext(cfg.AggregationConfig)
	} else {
		node.AggregationConfig = make(map[string]interface{})
	}
	node.AggregationConfig["original_prompt"] = userPrompt

	return node, nil
}

func interpolatePrompt(template, userPrompt string) string {
	t := strings.TrimSpace(template)
	if t == "" {
		return ""
	}
	return interpolationVarRegex.ReplaceAllStringFunc(t, func(match string) string {
		parts := interpolationVarRegex.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		switch strings.ToLower(parts[1]) {
		case "user_prompt", "prompt", "topic":
			return userPrompt
		default:
			return match
		}
	})
}
