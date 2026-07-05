package jobs

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/alhasaniq/consortium/pkg/workflow"
)

var definitionVariablePattern = regexp.MustCompile(`\{\{\s*[^}]+\s*\}\}`)

type workflowFile struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Nodes       []workflowFileNode `json:"nodes"`
	Edges       []workflowFileEdge `json:"edges"`
}

type workflowFileNode struct {
	ID   string               `json:"id"`
	Type string               `json:"type"`
	Data workflowFileNodeData `json:"data"`
}

type workflowFileNodeData struct {
	Type   string                 `json:"type"`
	Label  string                 `json:"label"`
	Config workflowFileNodeConfig `json:"config"`
}

type workflowFileNodeConfig struct {
	Name                      string                   `json:"name"`
	Description               string                   `json:"description"`
	Model                     string                   `json:"model"`
	Prompt                    string                   `json:"prompt"`
	Harness                   string                   `json:"harness"`
	TaskID                    string                   `json:"taskId"`
	TaskSummary               string                   `json:"taskSummary"`
	Identity                  string                   `json:"identity"`
	Image                     string                   `json:"image"`
	Sandbox                   string                   `json:"sandbox"`
	RuntimeURL                string                   `json:"runtimeUrl"`
	GraceSeconds              int                      `json:"graceSeconds"`
	RepoSpecsJSON             string                   `json:"repoSpecsJson"`
	WorkSourceJSON            string                   `json:"workSourceJson"`
	InheritFromMode           string                   `json:"inheritFromMode"`
	InheritFromNodeID         string                   `json:"inheritFromNodeId"`
	InheritFromKind           string                   `json:"inheritFromKind"`
	InheritFromID             string                   `json:"inheritFromId"`
	InheritFromPolicy         string                   `json:"inheritFromPolicy"`
	SystemPrompt              string                   `json:"systemPrompt"`
	UserPrompt                string                   `json:"userPrompt"`
	Temperature               *float64                 `json:"temperature"`
	MaxTokens                 *int                     `json:"maxTokens"`
	TimeoutSeconds            int                      `json:"timeoutSeconds"`
	Tools                     []map[string]interface{} `json:"tools"`
	ToolChoice                interface{}              `json:"toolChoice"`
	ResponseFormat            map[string]interface{}   `json:"response_format"`
	OpenRouterProvider        map[string]interface{}   `json:"openRouterProvider"`
	OpenRouterReasoning       map[string]interface{}   `json:"openRouterReasoning"`
	OutputFormat              string                   `json:"outputFormat"`
	Condition                 string                   `json:"condition"`
	AggregationMethod         string                   `json:"aggregationMethod"`
	AggregationWorkflowID     string                   `json:"aggregationWorkflowId"`
	AggregationConfig         map[string]interface{}   `json:"aggregationConfig"`
	AggregationInternalState  string                   `json:"aggregationInternalState"`
	AggregationAnchorID       string                   `json:"aggregationAnchorId"`
	AggregationBranch         string                   `json:"aggregationBranch"`
	AggregationBranchParentID string                   `json:"aggregationBranchParentId"`
	BenchmarkOutputPackaging  bool                     `json:"benchmarkOutputPackaging"`
	OperationType             string                   `json:"operationType"`
	OperationConfig           map[string]interface{}   `json:"operationConfig"`
	WorkflowID                string                   `json:"workflowId"`
	WorkflowRefID             string                   `json:"workflowRefId"`
	ChildWorkflowID           string                   `json:"childWorkflowId"`
	InputTemplate             map[string]string        `json:"inputTemplate"`
	OutputKey                 string                   `json:"outputKey"`
	ChildAwait                *bool                    `json:"await"`
	RetryPolicy               *workflow.RetryPolicy    `json:"retryPolicy"`
	RetryPolicySnake          *workflow.RetryPolicy    `json:"retry_policy"`
	RetryMaxAttempts          *int                     `json:"retryMaxAttempts"`
	RetryBackoffMs            *int                     `json:"retryBackoffMs"`
	RetryBackoffFactor        *float64                 `json:"retryBackoffMultiplier"`
	RetryMaxBackoffMs         *int                     `json:"retryMaxBackoffMs"`
	SourceVariable            string                   `json:"sourceVariable"`
	ExtractionPatterns        []string                 `json:"extractionPatterns"`
}

type workflowFileEdge struct {
	ID           string `json:"id"`
	Source       string `json:"source"`
	Target       string `json:"target"`
	SourceHandle string `json:"sourceHandle"`
}

// WorkflowFromDefinition converts stored workflow definition JSON into executable workflow format.
// The definition may already be in runtime format or in workflow-file builder format.
func WorkflowFromDefinition(definition string, inputValues map[string]interface{}) (*workflow.Workflow, error) {
	raw := strings.TrimSpace(definition)
	if raw == "" {
		return nil, fmt.Errorf("workflow definition is empty")
	}

	var rawObj map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &rawObj); err != nil {
		return nil, fmt.Errorf("parse workflow definition: %w", err)
	}

	if isWorkflowFileShape(rawObj) {
		return workflowFromWorkflowFile(rawObj, inputValues)
	}

	var wf workflow.Workflow
	if err := json.Unmarshal([]byte(raw), &wf); err != nil {
		return nil, fmt.Errorf("parse runtime workflow definition: %w", err)
	}
	if len(wf.Nodes) == 0 {
		return nil, fmt.Errorf("workflow has no nodes")
	}
	if wf.Context == nil {
		wf.Context = make(map[string]interface{})
	}
	for k, v := range inputValues {
		wf.Context[k] = v
	}
	return &wf, nil
}

func isWorkflowFileShape(raw map[string]interface{}) bool {
	nodesRaw, ok := raw["nodes"].([]interface{})
	if !ok || len(nodesRaw) == 0 {
		return false
	}
	first, ok := nodesRaw[0].(map[string]interface{})
	if !ok {
		return false
	}
	_, hasData := first["data"]
	return hasData
}

func workflowFromWorkflowFile(raw map[string]interface{}, inputValues map[string]interface{}) (*workflow.Workflow, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal workflow_file: %w", err)
	}

	var file workflowFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse workflow_file: %w", err)
	}

	nodeByID := make(map[string]workflowFileNode, len(file.Nodes))
	nodeIDs := make([]string, 0, len(file.Nodes))
	hasInputNode := false
	for _, node := range file.Nodes {
		if node.ID == "" {
			return nil, fmt.Errorf("workflow node id is required")
		}
		nodeByID[node.ID] = node
		nodeIDs = append(nodeIDs, node.ID)
		if effectiveNodeType(node) == "input" {
			hasInputNode = true
		}
	}

	workflowContext := make(map[string]interface{})
	if hasInputNode {
		for key, value := range inputValues {
			workflowContext[key] = value
		}
	}

	sortedNodeIDs := topologicalSortNodeIDs(nodeIDs, file.Edges)
	incomingSources := make(map[string][]string)
	for _, edge := range file.Edges {
		if edge.Source == "" || edge.Target == "" {
			continue
		}
		incomingSources[edge.Target] = append(incomingSources[edge.Target], edge.Source)
	}
	forkedBranches := forkedAggregationBranches(nodeByID, file.Edges)

	workflowNodes := make([]*workflow.Node, 0, len(sortedNodeIDs))
	includedNodeIDs := make(map[string]struct{}, len(sortedNodeIDs))

	for _, nodeID := range sortedNodeIDs {
		node, ok := nodeByID[nodeID]
		if !ok {
			continue
		}

		nodeType := effectiveNodeType(node)
		config := node.Data.Config
		if isForkedAggregationBranchNode(node) {
			continue
		}

		switch nodeType {
		case "input":
			continue
		case "conditional":
			if !isForkedAggregationNode(node) {
				continue
			}
			branches := forkedBranches[node.ID]
			trueBranch, err := forkedBranchNodeToWorkflow(branches.TrueID, nodeByID, forkedBranches, workflowContext)
			if err != nil {
				return nil, fmt.Errorf("workflow node %q (%s) true branch: %w", node.ID, nodeType, err)
			}
			falseBranch, err := forkedBranchNodeToWorkflow(branches.FalseID, nodeByID, forkedBranches, workflowContext)
			if err != nil {
				return nil, fmt.Errorf("workflow node %q (%s) false branch: %w", node.ID, nodeType, err)
			}
			wfNode := &workflow.Node{
				ID:          node.ID,
				Type:        workflow.NodeTypeConditional,
				Condition:   config.Condition,
				TrueBranch:  trueBranch,
				FalseBranch: falseBranch,
				RetryPolicy: config.effectiveRetryPolicy().Clone(),
				Metadata: map[string]interface{}{
					"label":       formatNodeLabel(node.ID, nodeType, config.Name, node.Data.Label),
					"name":        config.Name,
					"description": config.Description,
					"input_ids":   append([]string(nil), incomingSources[node.ID]...),
				},
			}
			workflowNodes = append(workflowNodes, wfNode)
			includedNodeIDs[node.ID] = struct{}{}
		case "agent_run":
			basePrompt := config.Prompt
			if basePrompt == "" {
				basePrompt = config.UserPrompt
			}
			userPrompt := injectInputPlaceholders(basePrompt, workflowContext)

			wfNode := &workflow.Node{
				ID:             node.ID,
				Type:           workflow.NodeTypeAgentRun,
				Prompt:         userPrompt,
				Harness:        config.Harness,
				Sandbox:        workflow.NormalizeNovomoSandbox(config.Sandbox),
				TimeoutSeconds: config.TimeoutSeconds,
				RetryPolicy:    config.effectiveRetryPolicy().Clone(),
				Metadata: map[string]interface{}{
					"label":       formatNodeLabel(node.ID, nodeType, config.Name, node.Data.Label),
					"name":        config.Name,
					"description": config.Description,
				},
			}
			if err := applyNovomoHandoffConfig(wfNode, config, node.ID, nodeByID, incomingSources); err != nil {
				return nil, fmt.Errorf("workflow node %q (%s): %w", node.ID, nodeType, err)
			}
			workflowNodes = append(workflowNodes, wfNode)
			includedNodeIDs[node.ID] = struct{}{}
		case "novo_run":
			basePrompt := config.Prompt
			if basePrompt == "" {
				basePrompt = config.UserPrompt
			}
			userPrompt := injectInputPlaceholders(basePrompt, workflowContext)
			repoSpecs, err := parseJSONArrayConfig(config.RepoSpecsJSON, "repoSpecsJson")
			if err != nil {
				return nil, fmt.Errorf("workflow node %q (%s): %w", node.ID, nodeType, err)
			}
			workSource, err := parseJSONObjectConfig(config.WorkSourceJSON, "workSourceJson")
			if err != nil {
				return nil, fmt.Errorf("workflow node %q (%s): %w", node.ID, nodeType, err)
			}

			wfNode := &workflow.Node{
				ID:             node.ID,
				Type:           workflow.NodeTypeNovoRun,
				Prompt:         userPrompt,
				TaskID:         config.TaskID,
				TaskSummary:    config.TaskSummary,
				Identity:       config.Identity,
				Image:          config.Image,
				Sandbox:        workflow.NormalizeNovomoSandbox(config.Sandbox),
				RuntimeURL:     config.RuntimeURL,
				TimeoutSeconds: config.TimeoutSeconds,
				GraceSeconds:   config.GraceSeconds,
				RepoSpecs:      repoSpecs,
				WorkSource:     workSource,
				RetryPolicy:    config.effectiveRetryPolicy().Clone(),
				Metadata: map[string]interface{}{
					"label":       formatNodeLabel(node.ID, "superagent", config.Name, node.Data.Label),
					"name":        config.Name,
					"description": config.Description,
				},
			}
			if err := applyNovomoHandoffConfig(wfNode, config, node.ID, nodeByID, incomingSources); err != nil {
				return nil, fmt.Errorf("workflow node %q (%s): %w", node.ID, nodeType, err)
			}
			workflowNodes = append(workflowNodes, wfNode)
			includedNodeIDs[node.ID] = struct{}{}
		case "agent":
			if strings.TrimSpace(config.Model) == "" {
				return nil, fmt.Errorf("workflow node %q (%s) is missing required config.model", node.ID, nodeType)
			}
			if config.MaxTokens == nil {
				return nil, fmt.Errorf("workflow node %q (%s) is missing required config.maxTokens", node.ID, nodeType)
			}

			systemPrompt := config.SystemPrompt
			basePrompt := config.UserPrompt
			if basePrompt == "" {
				basePrompt = systemPrompt
			}
			userPrompt := injectInputPlaceholders(basePrompt, workflowContext)

			nodeMetadata := map[string]interface{}{
				"label":       formatNodeLabel(node.ID, nodeType, config.Name, node.Data.Label),
				"name":        config.Name,
				"description": config.Description,
			}
			if len(config.Tools) > 0 {
				nodeMetadata["tools"] = config.Tools
			}
			if config.ToolChoice != nil {
				nodeMetadata["tool_choice"] = config.ToolChoice
			}
			if config.ResponseFormat != nil {
				nodeMetadata["response_format"] = config.ResponseFormat
			}
			if config.OpenRouterProvider != nil {
				nodeMetadata["openrouter_provider"] = config.OpenRouterProvider
			}
			if config.OpenRouterReasoning != nil {
				nodeMetadata["openrouter_reasoning"] = config.OpenRouterReasoning
			}

			maxTokens := *config.MaxTokens

			wfNode := &workflow.Node{
				ID:             node.ID,
				Type:           workflow.NodeTypePrompt,
				Model:          config.Model,
				Prompt:         userPrompt,
				SystemPrompt:   systemPrompt,
				MaxTokens:      maxTokens,
				TimeoutSeconds: config.TimeoutSeconds,
				RetryPolicy:    config.effectiveRetryPolicy().Clone(),
				Metadata:       nodeMetadata,
			}
			if config.Temperature != nil {
				temp := *config.Temperature
				wfNode.Temperature = &temp
			}
			workflowNodes = append(workflowNodes, wfNode)
			includedNodeIDs[node.ID] = struct{}{}
		case "contract_extract":
			if strings.TrimSpace(config.Model) == "" {
				return nil, fmt.Errorf("workflow node %q (%s) is missing required config.model", node.ID, nodeType)
			}
			if config.MaxTokens == nil {
				return nil, fmt.Errorf("workflow node %q (%s) is missing required config.maxTokens", node.ID, nodeType)
			}

			systemPrompt := config.SystemPrompt
			basePrompt := config.UserPrompt
			if basePrompt == "" {
				basePrompt = systemPrompt
			}
			userPrompt := injectInputPlaceholders(basePrompt, workflowContext)

			nodeMetadata := map[string]interface{}{
				"label":       formatNodeLabel(node.ID, nodeType, config.Name, node.Data.Label),
				"name":        config.Name,
				"description": config.Description,
			}
			if config.SourceVariable != "" {
				nodeMetadata["source_variable"] = config.SourceVariable
			}
			if len(config.ExtractionPatterns) > 0 {
				nodeMetadata["extraction_patterns"] = config.ExtractionPatterns
			}
			if config.OpenRouterProvider != nil {
				nodeMetadata["openrouter_provider"] = config.OpenRouterProvider
			}
			if config.OpenRouterReasoning != nil {
				nodeMetadata["openrouter_reasoning"] = config.OpenRouterReasoning
			}

			maxTokens := *config.MaxTokens

			wfNode := &workflow.Node{
				ID:             node.ID,
				Type:           workflow.NodeTypeContractExtract,
				Model:          config.Model,
				Prompt:         userPrompt,
				SystemPrompt:   systemPrompt,
				MaxTokens:      maxTokens,
				TimeoutSeconds: config.TimeoutSeconds,
				RetryPolicy:    config.effectiveRetryPolicy().Clone(),
				Metadata:       nodeMetadata,
			}
			if config.Temperature != nil {
				temp := *config.Temperature
				wfNode.Temperature = &temp
			}
			workflowNodes = append(workflowNodes, wfNode)
			includedNodeIDs[node.ID] = struct{}{}
		case "child_workflow":
			await := true
			if config.ChildAwait != nil {
				await = *config.ChildAwait
			}
			wfNode := &workflow.Node{
				ID:                 node.ID,
				Type:               workflow.NodeTypeChildWorkflow,
				ChildWorkflowID:    config.ChildWorkflowID,
				ChildInputTemplate: config.InputTemplate,
				ChildAwait:         await,
				ChildOutputKey:     config.OutputKey,
				TimeoutSeconds:     config.TimeoutSeconds,
				RetryPolicy:        config.effectiveRetryPolicy().Clone(),
				Metadata: map[string]interface{}{
					"label":       formatNodeLabel(node.ID, nodeType, config.Name, node.Data.Label),
					"name":        config.Name,
					"description": config.Description,
				},
			}
			workflowNodes = append(workflowNodes, wfNode)
			includedNodeIDs[node.ID] = struct{}{}
		case "workflow_ref":
			workflowRefID := defaultString(config.WorkflowID, config.WorkflowRefID)
			if strings.TrimSpace(workflowRefID) == "" {
				return nil, fmt.Errorf("workflow node %q (%s) is missing required config.workflowId", node.ID, nodeType)
			}
			wfNode := &workflow.Node{
				ID:            node.ID,
				Type:          workflow.NodeTypeWorkflowRef,
				WorkflowRefID: workflowRefID,
				InputTemplate: config.InputTemplate,
				OutputKey:     config.OutputKey,
				Metadata: map[string]interface{}{
					"label":       formatNodeLabel(node.ID, nodeType, config.Name, node.Data.Label),
					"name":        config.Name,
					"description": config.Description,
				},
			}
			workflowNodes = append(workflowNodes, wfNode)
			includedNodeIDs[node.ID] = struct{}{}
		case "operation":
			wfNode := &workflow.Node{
				ID:              node.ID,
				Type:            workflow.NodeTypeOperation,
				OperationType:   config.OperationType,
				OperationConfig: config.OperationConfig,
				Metadata: map[string]interface{}{
					"label":       formatNodeLabel(node.ID, nodeType, config.Name, node.Data.Label),
					"name":        config.Name,
					"description": config.Description,
				},
			}
			workflowNodes = append(workflowNodes, wfNode)
			includedNodeIDs[node.ID] = struct{}{}
		case "aggregation":
			outputNode, hasOutputNode := downstreamResultNode(node.ID, nodeByID, file.Edges)
			outputConfig := workflowFileNodeConfig{}
			if hasOutputNode {
				outputConfig = outputNode.Data.Config
			}
			outputName := defaultString(outputConfig.Name, defaultString(config.Name, node.ID))
			outputFormat := defaultString(outputConfig.OutputFormat, defaultString(config.OutputFormat, "text"))
			sources := append([]string(nil), incomingSources[node.ID]...)
			aggregationConfig := normalizeWorkflowFileAggregationConfig(
				config.AggregationMethod,
				config.AggregationConfig,
				config.OpenRouterProvider,
				config.OpenRouterReasoning,
			)
			description := config.Description
			if description == "" {
				description = outputConfig.Description
			}
			metadata := map[string]interface{}{
				"input_ids":             sources,
				"label":                 formatNodeLabel(node.ID, nodeType, config.Name, node.Data.Label),
				"output_name":           outputName,
				"output_format":         outputFormat,
				"description":           description,
				"aggregation_anchor_id": node.ID,
				"aggregation_method":    config.AggregationMethod,
			}
			if config.BenchmarkOutputPackaging {
				metadata["benchmark_output_packaging"] = true
			}
			if hasOutputNode {
				metadata["presentation_result_id"] = outputNode.ID
			}
			if strings.TrimSpace(config.AggregationWorkflowID) != "" {
				wfNode := &workflow.Node{
					ID:                node.ID,
					Type:              workflow.NodeTypeWorkflowRef,
					WorkflowRefID:     strings.TrimSpace(config.AggregationWorkflowID),
					AggregationMethod: workflow.AggregationMethod(config.AggregationMethod),
					AggregationConfig: aggregationConfig,
					TimeoutSeconds:    config.TimeoutSeconds,
					RetryPolicy:       config.effectiveRetryPolicy().Clone(),
					Metadata:          metadata,
				}
				workflowNodes = append(workflowNodes, wfNode)
				includedNodeIDs[node.ID] = struct{}{}
				continue
			}
			wfNode := &workflow.Node{
				ID:                node.ID,
				Type:              workflow.NodeTypeResult,
				OutputName:        outputName,
				OutputFormat:      outputFormat,
				AggregationMethod: workflow.AggregationMethod(config.AggregationMethod),
				AggregationConfig: aggregationConfig,
				TimeoutSeconds:    config.TimeoutSeconds,
				RetryPolicy:       config.effectiveRetryPolicy().Clone(),
				Metadata:          metadata,
			}
			workflowNodes = append(workflowNodes, wfNode)
			includedNodeIDs[node.ID] = struct{}{}
		case "result":
			if hasIncomingAggregation(node.ID, nodeByID, file.Edges) {
				continue
			}
			outputName := defaultString(config.Name, node.ID)
			outputFormat := defaultString(config.OutputFormat, "text")
			sources := append([]string(nil), incomingSources[node.ID]...)
			aggregationConfig := normalizeWorkflowFileAggregationConfig(
				config.AggregationMethod,
				config.AggregationConfig,
				config.OpenRouterProvider,
				config.OpenRouterReasoning,
			)

			wfNode := &workflow.Node{
				ID:                node.ID,
				Type:              workflow.NodeTypeResult,
				OutputName:        outputName,
				OutputFormat:      outputFormat,
				AggregationMethod: workflow.AggregationMethod(config.AggregationMethod),
				AggregationConfig: aggregationConfig,
				TimeoutSeconds:    config.TimeoutSeconds,
				RetryPolicy:       config.effectiveRetryPolicy().Clone(),
				Metadata: map[string]interface{}{
					"input_ids":     sources,
					"label":         formatNodeLabel(node.ID, nodeType, config.Name, node.Data.Label),
					"output_name":   outputName,
					"output_format": outputFormat,
					"description":   config.Description,
				},
			}
			workflowNodes = append(workflowNodes, wfNode)
			includedNodeIDs[node.ID] = struct{}{}
		default:
			return nil, fmt.Errorf("unsupported workflow node type %q", nodeType)
		}
	}

	workflowEdges := make([]*workflow.Edge, 0, len(file.Edges))
	for _, edge := range file.Edges {
		if edge.Source == "" || edge.Target == "" {
			continue
		}
		if _, ok := includedNodeIDs[edge.Source]; !ok {
			continue
		}
		if _, ok := includedNodeIDs[edge.Target]; !ok {
			continue
		}
		if isForkedAggregationBranchEdge(edge, nodeByID) {
			continue
		}
		sourceNode, ok := nodeByID[edge.Source]
		if ok && effectiveNodeType(sourceNode) == "conditional" && !isForkedAggregationNode(sourceNode) {
			continue
		}
		edgeID := edge.ID
		if edgeID == "" {
			edgeID = fmt.Sprintf("%s-%s", edge.Source, edge.Target)
		}
		workflowEdges = append(workflowEdges, &workflow.Edge{
			ID:     edgeID,
			Source: edge.Source,
			Target: edge.Target,
		})
	}

	name := defaultString(file.Name, "Untitled Workflow")
	return &workflow.Workflow{
		ID:          file.ID,
		Name:        name,
		Description: file.Description,
		Nodes:       workflowNodes,
		Edges:       workflowEdges,
		Context:     workflowContext,
	}, nil
}

func downstreamResultNode(nodeID string, nodes map[string]workflowFileNode, edges []workflowFileEdge) (workflowFileNode, bool) {
	for _, edge := range edges {
		if edge.Source != nodeID {
			continue
		}
		target, ok := nodes[edge.Target]
		if ok && effectiveNodeType(target) == "result" {
			return target, true
		}
	}
	return workflowFileNode{}, false
}

func hasIncomingAggregation(nodeID string, nodes map[string]workflowFileNode, edges []workflowFileEdge) bool {
	for _, edge := range edges {
		if edge.Target != nodeID {
			continue
		}
		source, ok := nodes[edge.Source]
		if ok && effectiveNodeType(source) == "aggregation" {
			return true
		}
	}
	return false
}

type forkedBranchPair struct {
	TrueID  string
	FalseID string
}

func isForkedAggregationNode(node workflowFileNode) bool {
	config := node.Data.Config
	return config.AggregationInternalState == "forked" && strings.TrimSpace(config.AggregationAnchorID) != ""
}

func isForkedAggregationBranchNode(node workflowFileNode) bool {
	config := node.Data.Config
	return isForkedAggregationNode(node) && strings.TrimSpace(config.AggregationBranchParentID) != ""
}

func forkedAggregationBranches(
	nodes map[string]workflowFileNode,
	edges []workflowFileEdge,
) map[string]forkedBranchPair {
	out := make(map[string]forkedBranchPair)
	for _, edge := range edges {
		source, sourceOK := nodes[edge.Source]
		target, targetOK := nodes[edge.Target]
		if !sourceOK || !targetOK || effectiveNodeType(source) != "conditional" || !isForkedAggregationBranchNode(target) {
			continue
		}
		branch := "true"
		if edge.SourceHandle == "false" || target.Data.Config.AggregationBranch == "false" {
			branch = "false"
		}
		pair := out[source.ID]
		if branch == "false" {
			pair.FalseID = target.ID
		} else {
			pair.TrueID = target.ID
		}
		out[source.ID] = pair
	}
	return out
}

func isForkedAggregationBranchEdge(edge workflowFileEdge, nodes map[string]workflowFileNode) bool {
	source, sourceOK := nodes[edge.Source]
	if !sourceOK || effectiveNodeType(source) != "conditional" || !isForkedAggregationNode(source) {
		return false
	}
	target, targetOK := nodes[edge.Target]
	return edge.SourceHandle == "true" || edge.SourceHandle == "false" || (targetOK && isForkedAggregationBranchNode(target))
}

func forkedBranchNodeToWorkflow(
	nodeID string,
	nodes map[string]workflowFileNode,
	branches map[string]forkedBranchPair,
	workflowContext map[string]interface{},
) (*workflow.Node, error) {
	if strings.TrimSpace(nodeID) == "" {
		return nil, nil
	}
	node, ok := nodes[nodeID]
	if !ok {
		return nil, fmt.Errorf("branch node %q not found", nodeID)
	}
	nodeType := effectiveNodeType(node)
	config := node.Data.Config
	label := formatNodeLabel(node.ID, nodeType, config.Name, node.Data.Label)

	switch nodeType {
	case "agent":
		if strings.TrimSpace(config.Model) == "" {
			return nil, fmt.Errorf("workflow node %q (%s) is missing required config.model", node.ID, nodeType)
		}
		if config.MaxTokens == nil {
			return nil, fmt.Errorf("workflow node %q (%s) is missing required config.maxTokens", node.ID, nodeType)
		}
		systemPrompt := config.SystemPrompt
		basePrompt := config.UserPrompt
		if basePrompt == "" {
			basePrompt = systemPrompt
		}
		metadata := map[string]interface{}{
			"label":       label,
			"name":        config.Name,
			"description": config.Description,
		}
		if len(config.Tools) > 0 {
			metadata["tools"] = config.Tools
		}
		if config.ToolChoice != nil {
			metadata["tool_choice"] = config.ToolChoice
		}
		if config.ResponseFormat != nil {
			metadata["response_format"] = config.ResponseFormat
		}
		if config.OpenRouterProvider != nil {
			metadata["openrouter_provider"] = config.OpenRouterProvider
		}
		if config.OpenRouterReasoning != nil {
			metadata["openrouter_reasoning"] = config.OpenRouterReasoning
		}

		maxTokens := *config.MaxTokens
		wfNode := &workflow.Node{
			ID:             node.ID,
			Type:           workflow.NodeTypePrompt,
			Model:          config.Model,
			Prompt:         injectInputPlaceholders(basePrompt, workflowContext),
			SystemPrompt:   systemPrompt,
			MaxTokens:      maxTokens,
			TimeoutSeconds: config.TimeoutSeconds,
			RetryPolicy:    config.effectiveRetryPolicy().Clone(),
			Metadata:       metadata,
		}
		if config.Temperature != nil {
			temp := *config.Temperature
			wfNode.Temperature = &temp
		}
		return wfNode, nil
	case "operation":
		return &workflow.Node{
			ID:              node.ID,
			Type:            workflow.NodeTypeOperation,
			OperationType:   config.OperationType,
			OperationConfig: config.OperationConfig,
			Metadata: map[string]interface{}{
				"label":       label,
				"name":        config.Name,
				"description": config.Description,
			},
		}, nil
	case "conditional":
		pair := branches[node.ID]
		trueBranch, err := forkedBranchNodeToWorkflow(pair.TrueID, nodes, branches, workflowContext)
		if err != nil {
			return nil, fmt.Errorf("true branch: %w", err)
		}
		falseBranch, err := forkedBranchNodeToWorkflow(pair.FalseID, nodes, branches, workflowContext)
		if err != nil {
			return nil, fmt.Errorf("false branch: %w", err)
		}
		return &workflow.Node{
			ID:          node.ID,
			Type:        workflow.NodeTypeConditional,
			Condition:   config.Condition,
			TrueBranch:  trueBranch,
			FalseBranch: falseBranch,
			RetryPolicy: config.effectiveRetryPolicy().Clone(),
			Metadata: map[string]interface{}{
				"label":       label,
				"name":        config.Name,
				"description": config.Description,
			},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported forked conditional branch node type %q", nodeType)
	}
}

func normalizeWorkflowFileAggregationConfig(
	_ string,
	config map[string]interface{},
	openRouterProvider map[string]interface{},
	openRouterReasoning map[string]interface{},
) map[string]interface{} {
	return workflow.MergeAggregationConfig(config, openRouterProvider, openRouterReasoning)
}

func topologicalSortNodeIDs(nodeIDs []string, edges []workflowFileEdge) []string {
	inDegree := make(map[string]int, len(nodeIDs))
	adjacency := make(map[string][]string, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		inDegree[nodeID] = 0
		adjacency[nodeID] = []string{}
	}

	for _, edge := range edges {
		if _, ok := inDegree[edge.Source]; !ok {
			continue
		}
		if _, ok := inDegree[edge.Target]; !ok {
			continue
		}
		adjacency[edge.Source] = append(adjacency[edge.Source], edge.Target)
		inDegree[edge.Target]++
	}

	queue := make([]string, 0, len(nodeIDs))
	for nodeID, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, nodeID)
		}
	}
	sort.Strings(queue)

	sorted := make([]string, 0, len(nodeIDs))
	visited := make(map[string]struct{}, len(nodeIDs))
	for len(queue) > 0 {
		nodeID := queue[0]
		queue = queue[1:]

		sorted = append(sorted, nodeID)
		visited[nodeID] = struct{}{}

		for _, next := range adjacency[nodeID] {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
		sort.Strings(queue)
	}

	if len(sorted) == len(nodeIDs) {
		return sorted
	}

	remaining := make([]string, 0, len(nodeIDs)-len(sorted))
	for _, nodeID := range nodeIDs {
		if _, ok := visited[nodeID]; !ok {
			remaining = append(remaining, nodeID)
		}
	}
	sort.Strings(remaining)
	return append(sorted, remaining...)
}

func effectiveNodeType(node workflowFileNode) string {
	if node.Data.Type != "" {
		return node.Data.Type
	}
	return node.Type
}

func injectInputPlaceholders(basePrompt string, workflowContext map[string]interface{}) string {
	if len(workflowContext) == 0 || definitionVariablePattern.MatchString(basePrompt) {
		return basePrompt
	}

	keys := make([]string, 0, len(workflowContext))
	for key := range workflowContext {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var builder strings.Builder
	builder.WriteString("Input data:\n")
	for idx, key := range keys {
		if idx > 0 {
			builder.WriteString("\n")
		}
		builder.WriteString("{{")
		builder.WriteString(key)
		builder.WriteString("}}")
	}

	inputBlock := builder.String()
	if basePrompt == "" {
		return inputBlock
	}
	return inputBlock + "\n\n" + basePrompt
}

func formatNodeLabel(nodeID, nodeType, name, fallback string) string {
	displayName := defaultString(name, defaultString(fallback, "unnamed"))
	return fmt.Sprintf("%s %s(%s)", nodeID, toTitle(nodeType), displayName)
}

func (c workflowFileNodeConfig) effectiveRetryPolicy() *workflow.RetryPolicy {
	if c.RetryPolicy != nil {
		return c.RetryPolicy
	}
	if c.RetryPolicySnake != nil {
		return c.RetryPolicySnake
	}
	if c.RetryMaxAttempts == nil && c.RetryBackoffMs == nil && c.RetryBackoffFactor == nil && c.RetryMaxBackoffMs == nil {
		return nil
	}
	return &workflow.RetryPolicy{
		MaxAttempts:     intValue(c.RetryMaxAttempts, 0),
		BackoffMs:       intValue(c.RetryBackoffMs, 0),
		BackoffMultiply: floatValue(c.RetryBackoffFactor, 0),
		MaxBackoffMs:    intValue(c.RetryMaxBackoffMs, 0),
	}
}

func applyNovomoHandoffConfig(node *workflow.Node, config workflowFileNodeConfig, nodeID string, nodeByID map[string]workflowFileNode, incomingSources map[string][]string) error {
	if node == nil {
		return nil
	}
	mode := strings.TrimSpace(config.InheritFromMode)
	switch mode {
	case "none":
		return nil
	case "":
		if strings.TrimSpace(config.InheritFromID) != "" {
			node.InheritFrom = &workflow.NovomoHandoffRef{
				Kind:   strings.TrimSpace(config.InheritFromKind),
				ID:     strings.TrimSpace(config.InheritFromID),
				Policy: strings.TrimSpace(config.InheritFromPolicy),
			}
			return workflow.ValidateNovomoHandoffRef("inherit_from", node.InheritFrom)
		}
		if strings.TrimSpace(config.InheritFromNodeID) != "" {
			node.InheritFromNodeID = strings.TrimSpace(config.InheritFromNodeID)
			node.InheritFromPolicy = strings.TrimSpace(config.InheritFromPolicy)
			return nil
		}
		return applyAutoNovomoHandoffConfig(node, config, nodeID, nodeByID, incomingSources)
	case "auto":
		return applyAutoNovomoHandoffConfig(node, config, nodeID, nodeByID, incomingSources)
	case "explicit":
		node.InheritFrom = &workflow.NovomoHandoffRef{
			Kind:   strings.TrimSpace(config.InheritFromKind),
			ID:     strings.TrimSpace(config.InheritFromID),
			Policy: strings.TrimSpace(config.InheritFromPolicy),
		}
		return workflow.ValidateNovomoHandoffRef("inherit_from", node.InheritFrom)
	case "upstream":
		if strings.TrimSpace(config.InheritFromNodeID) == "" {
			return fmt.Errorf("inheritFromNodeId is required when inheritFromMode is upstream")
		}
		node.InheritFromNodeID = strings.TrimSpace(config.InheritFromNodeID)
		node.InheritFromPolicy = strings.TrimSpace(config.InheritFromPolicy)
	default:
		return fmt.Errorf("inheritFromMode must be one of auto, none, upstream, or explicit")
	}
	return nil
}

func applyAutoNovomoHandoffConfig(node *workflow.Node, config workflowFileNodeConfig, nodeID string, nodeByID map[string]workflowFileNode, incomingSources map[string][]string) error {
	sourceID, found, useWorkflowTask, err := autoNovomoHandoffSource(nodeID, nodeByID, incomingSources)
	if err != nil {
		return err
	}
	if useWorkflowTask {
		node.InheritFromWorkflowTask = true
		node.InheritFromPolicy = strings.TrimSpace(config.InheritFromPolicy)
		return nil
	}
	if !found {
		return nil
	}
	node.InheritFromNodeID = sourceID
	node.InheritFromPolicy = strings.TrimSpace(config.InheritFromPolicy)
	return nil
}

func autoNovomoHandoffSource(nodeID string, nodeByID map[string]workflowFileNode, incomingSources map[string][]string) (string, bool, bool, error) {
	if strings.TrimSpace(nodeID) == "" {
		return "", false, false, nil
	}
	seen := map[string]bool{nodeID: true}
	frontier := append([]string(nil), incomingSources[nodeID]...)

	for len(frontier) > 0 {
		candidateSet := map[string]struct{}{}
		nextFrontier := make([]string, 0)

		for _, sourceID := range frontier {
			sourceID = strings.TrimSpace(sourceID)
			if sourceID == "" || seen[sourceID] {
				continue
			}
			seen[sourceID] = true

			sourceNode, ok := nodeByID[sourceID]
			if !ok {
				continue
			}
			switch effectiveNodeType(sourceNode) {
			case "agent_run", "novo_run":
				candidateSet[sourceID] = struct{}{}
			default:
				nextFrontier = append(nextFrontier, incomingSources[sourceID]...)
			}
		}

		if len(candidateSet) == 1 {
			for candidate := range candidateSet {
				return candidate, true, false, nil
			}
		}
		if len(candidateSet) > 1 {
			return "", false, true, nil
		}

		frontier = nextFrontier
	}

	return "", false, false, nil
}

func parseJSONArrayConfig(raw, field string) ([]map[string]interface{}, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var out []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("%s must be a JSON array of objects: %w", field, err)
	}
	return out, nil
}

func parseJSONObjectConfig(raw, field string) (map[string]interface{}, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("%s must be a JSON object: %w", field, err)
	}
	return out, nil
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func intValue(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func floatValue(value *float64, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	return *value
}

func toTitle(value string) string {
	if value == "" {
		return value
	}
	return strings.ToUpper(value[:1]) + value[1:]
}
