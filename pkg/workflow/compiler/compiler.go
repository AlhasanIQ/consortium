package compiler

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/alhasaniq/consortium/pkg/workflow"
	workflowruntime "github.com/alhasaniq/consortium/pkg/workflow/runtime"
)

const generatedIDDelimiter = "--"

var templateReferencePattern = regexp.MustCompile(`\{\{([^}]+)\}\}`)

// Resolver resolves source-level workflow references for compilation.
type Resolver interface {
	ResolveWorkflow(ctx context.Context, id string) (*workflow.Workflow, string, error)
}

// StaticResolver is a test and tooling resolver backed by in-memory workflow definitions.
type StaticResolver map[string]workflow.Workflow

// ResolveWorkflow implements Resolver.
func (r StaticResolver) ResolveWorkflow(_ context.Context, id string) (*workflow.Workflow, string, error) {
	wf, ok := r[id]
	if !ok {
		return nil, "", fmt.Errorf("workflow reference %q: not found", id)
	}
	clone, err := cloneWorkflow(&wf)
	if err != nil {
		return nil, "", fmt.Errorf("clone workflow reference %q: %w", id, err)
	}
	if clone.ID == "" {
		clone.ID = id
	}
	snapshot, err := workflowruntime.FreezeWorkflowDefinition(clone)
	if err != nil {
		return nil, "", fmt.Errorf("hash workflow reference %q: %w", id, err)
	}
	return clone, snapshot.DAGHash, nil
}

// Report describes source-level composition that was discovered during compilation.
type Report struct {
	ExpandedRefs []ExpandedRef
	ChildRefs    []ChildRef
}

// ExpandedRef records one compiled workflow_ref group.
type ExpandedRef struct {
	NodeID          string
	WorkflowID      string
	SourceHash      string
	ExpandedNodeIDs []string
}

// ChildRef records explicit child_workflow runtime boundaries that were preserved.
type ChildRef struct {
	NodeID     string
	WorkflowID string
}

type expansion struct {
	originalID     string
	workflowID     string
	sourceHash     string
	absorbedNodeID string
	nodes          []*workflow.Node
	edges          []*workflow.Edge
	roots          []string
	terminals      []string
	nodeIDs        []string
	rootInputs     map[string][]string
}

// Compile expands source-level workflow_ref nodes into the parent workflow DAG.
// Existing child_workflow nodes are explicit runtime boundaries and are preserved.
func Compile(ctx context.Context, wf *workflow.Workflow, resolver Resolver) (*workflow.Workflow, *Report, error) {
	report := &Report{}
	compiled, err := compileWorkflow(ctx, wf, resolver, nil, report)
	if err != nil {
		return nil, nil, err
	}
	return compiled, report, nil
}

func compileWorkflow(ctx context.Context, wf *workflow.Workflow, resolver Resolver, stack []string, report *Report) (*workflow.Workflow, error) {
	if wf == nil {
		return nil, fmt.Errorf("workflow is nil")
	}
	if wf.ID != "" {
		if containsString(stack, wf.ID) {
			return nil, fmt.Errorf("workflow reference cycle detected: %s -> %s", strings.Join(stack, " -> "), wf.ID)
		}
		stack = append(stack, wf.ID)
	}

	cloned, err := cloneWorkflow(wf)
	if err != nil {
		return nil, fmt.Errorf("clone workflow %q: %w", wf.ID, err)
	}

	expansions := make(map[string]*expansion)
	parentReferenceRewrites := make(map[string][]string)
	absorbedNodes := make(map[string]string)
	for _, node := range cloned.Nodes {
		switch node.Type {
		case workflow.NodeTypeChildWorkflow:
			if node.ChildWorkflowID != "" {
				report.ChildRefs = append(report.ChildRefs, ChildRef{
					NodeID:     node.ID,
					WorkflowID: node.ChildWorkflowID,
				})
			}
		case workflow.NodeTypeWorkflowRef:
			var exp *expansion
			var err error
			if isAggregationWorkflowRef(node) {
				exp, err = expandAggregationRef(ctx, cloned, node, resolver, stack, report)
			} else {
				exp, err = expandWorkflowRef(ctx, node, resolver, stack, report)
			}
			if err != nil {
				return nil, err
			}
			expansions[node.ID] = exp
			parentReferenceRewrites[node.ID] = exp.terminals
			if exp.absorbedNodeID != "" {
				absorbedNodes[exp.absorbedNodeID] = exp.originalID
				parentReferenceRewrites[exp.absorbedNodeID] = exp.terminals
			}
		}
	}

	if len(expansions) == 0 {
		return cloned, nil
	}

	compiled := &workflow.Workflow{
		ID:          cloned.ID,
		Name:        cloned.Name,
		Description: cloned.Description,
		Context:     cloned.Context,
		Limits:      cloned.Limits,
	}

	seenNodeIDs := make(map[string]struct{}, len(cloned.Nodes))
	for _, node := range cloned.Nodes {
		if exp, ok := expansions[node.ID]; ok {
			siblingReferenceRewrites := cloneReferenceRewritesExcept(parentReferenceRewrites, node.ID)
			for _, expandedNode := range exp.nodes {
				if err := rewriteNodeReferences(expandedNode, siblingReferenceRewrites); err != nil {
					return nil, fmt.Errorf("rewrite parent references for expanded node %q: %w", expandedNode.ID, err)
				}
				if err := addCompiledNode(compiled, seenNodeIDs, expandedNode); err != nil {
					return nil, err
				}
			}
			continue
		}
		if _, ok := absorbedNodes[node.ID]; ok {
			continue
		}

		if strings.Contains(node.ID, "__") {
			return nil, fmt.Errorf("node %q contains reserved delimiter %q", node.ID, "__")
		}
		if err := rewriteNodeReferences(node, parentReferenceRewrites); err != nil {
			return nil, fmt.Errorf("rewrite parent references for node %q: %w", node.ID, err)
		}
		if err := addCompiledNode(compiled, seenNodeIDs, node); err != nil {
			return nil, err
		}
	}

	edgeSet := make(map[string]struct{})
	for _, edge := range effectiveEdges(cloned.Nodes, cloned.Edges) {
		sources := []string{edge.Source}
		if exp, ok := expansions[edge.Source]; ok {
			sources = exp.terminals
		} else if replacement, ok := parentReferenceRewrites[edge.Source]; ok {
			sources = replacement
		}
		targets := []string{edge.Target}
		if exp, ok := expansions[edge.Target]; ok {
			targets = exp.rootsForParentInput(edge.Source)
		} else if replacement, ok := parentReferenceRewrites[edge.Target]; ok {
			targets = replacement
		}
		for _, source := range sources {
			for _, target := range targets {
				addCompiledEdge(compiled, edgeSet, source, target)
			}
		}
	}
	for _, exp := range expansions {
		for _, edge := range exp.edges {
			addCompiledEdge(compiled, edgeSet, edge.Source, edge.Target)
		}
	}

	for _, node := range compiled.Nodes {
		if node.Type == workflow.NodeTypeWorkflowRef {
			return nil, fmt.Errorf("unresolved workflow_ref node %q after compilation", node.ID)
		}
	}

	return compiled, nil
}

func expandWorkflowRef(ctx context.Context, node *workflow.Node, resolver Resolver, stack []string, parentReport *Report) (*expansion, error) {
	workflowID := strings.TrimSpace(node.WorkflowRefID)
	if workflowID == "" {
		return nil, fmt.Errorf("workflow_ref node %q missing workflow_ref_id", node.ID)
	}
	if containsString(stack, workflowID) {
		return nil, fmt.Errorf("workflow reference cycle detected: %s -> %s", strings.Join(stack, " -> "), workflowID)
	}
	if resolver == nil {
		return nil, fmt.Errorf("workflow_ref node %q requires a resolver for %q", node.ID, workflowID)
	}

	child, sourceHash, err := resolver.ResolveWorkflow(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("resolve workflow_ref node %q (%s): %w", node.ID, workflowID, err)
	}
	if child == nil {
		return nil, fmt.Errorf("resolve workflow_ref node %q (%s): resolver returned nil workflow", node.ID, workflowID)
	}
	if child.ID == "" {
		child.ID = workflowID
	}
	if len(child.Nodes) == 0 {
		return nil, fmt.Errorf("workflow_ref node %q references workflow %q with no nodes", node.ID, workflowID)
	}

	nestedReport := &Report{}
	compiledChild, err := compileWorkflow(ctx, child, resolver, stack, nestedReport)
	if err != nil {
		return nil, fmt.Errorf("compile workflow_ref node %q (%s): %w", node.ID, workflowID, err)
	}
	if len(compiledChild.Nodes) == 0 {
		return nil, fmt.Errorf("workflow_ref node %q references workflow %q with no nodes", node.ID, workflowID)
	}

	childIDMap := make(map[string][]string, len(compiledChild.Nodes))
	prefixedNodes := make([]*workflow.Node, 0, len(compiledChild.Nodes))
	prefixedNodeIDs := make([]string, 0, len(compiledChild.Nodes))
	for _, childNode := range compiledChild.Nodes {
		if strings.Contains(childNode.ID, "__") {
			return nil, fmt.Errorf("workflow_ref node %q generated illegal child node ID %q containing %q", node.ID, childNode.ID, "__")
		}
		generatedID := node.ID + generatedIDDelimiter + childNode.ID
		if strings.Contains(generatedID, "__") {
			return nil, fmt.Errorf("workflow_ref node %q generated illegal node ID %q containing %q", node.ID, generatedID, "__")
		}
		prefixed := cloneNode(childNode)
		prefixed.ID = generatedID
		prefixNestedAggregationMetadata(prefixed, node.ID)
		ensureSourceMetadata(prefixed, workflowID, sourceHash, childNode.ID)
		childIDMap[childNode.ID] = []string{generatedID}
		prefixedNodeIDs = append(prefixedNodeIDs, generatedID)
		prefixedNodes = append(prefixedNodes, prefixed)
	}

	for _, prefixed := range prefixedNodes {
		applyInputTemplate(prefixed, node.InputTemplate)
		if err := rewriteNodeReferences(prefixed, childIDMap); err != nil {
			return nil, fmt.Errorf("rewrite child references for node %q: %w", prefixed.ID, err)
		}
	}

	childEdges := effectiveEdges(compiledChild.Nodes, compiledChild.Edges)
	prefixedEdges := make([]*workflow.Edge, 0, len(childEdges))
	for _, edge := range childEdges {
		sourceIDs := childIDMap[edge.Source]
		targetIDs := childIDMap[edge.Target]
		if len(sourceIDs) == 0 || len(targetIDs) == 0 {
			return nil, fmt.Errorf("workflow_ref node %q child edge %q -> %q references missing nodes", node.ID, edge.Source, edge.Target)
		}
		prefixedEdges = append(prefixedEdges, &workflow.Edge{
			ID:     prefixedEdgeID(sourceIDs[0], targetIDs[0]),
			Source: sourceIDs[0],
			Target: targetIDs[0],
		})
	}

	roots, terminals := rootsAndTerminals(prefixedNodes, prefixedEdges)
	if len(roots) == 0 || len(terminals) == 0 {
		return nil, fmt.Errorf("workflow_ref node %q references workflow %q with no reachable entry/exit nodes", node.ID, workflowID)
	}
	if node.OutputKey != "" {
		selected, ok := selectOutputTerminal(node.OutputKey, compiledChild.Nodes, childIDMap)
		if !ok {
			return nil, fmt.Errorf("workflow_ref node %q output_key %q does not match a node id or output name in workflow %q", node.ID, node.OutputKey, workflowID)
		}
		terminals = []string{selected}
	}

	prefixNestedReport(parentReport, node.ID, nestedReport)
	parentReport.ExpandedRefs = append(parentReport.ExpandedRefs, ExpandedRef{
		NodeID:          node.ID,
		WorkflowID:      workflowID,
		SourceHash:      sourceHash,
		ExpandedNodeIDs: prefixedNodeIDs,
	})

	return &expansion{
		originalID: node.ID,
		workflowID: workflowID,
		sourceHash: sourceHash,
		nodes:      prefixedNodes,
		edges:      prefixedEdges,
		roots:      roots,
		terminals:  terminals,
		nodeIDs:    prefixedNodeIDs,
	}, nil
}

func (e *expansion) rootsForParentInput(sourceID string) []string {
	if e == nil || len(e.rootInputs) == 0 {
		return append([]string(nil), e.roots...)
	}
	roots := make([]string, 0, len(e.roots))
	for _, root := range e.roots {
		inputs, constrained := e.rootInputs[root]
		if !constrained || containsString(inputs, sourceID) {
			roots = append(roots, root)
		}
	}
	return roots
}

func addCompiledNode(wf *workflow.Workflow, seen map[string]struct{}, node *workflow.Node) error {
	if node.ID == "" {
		return fmt.Errorf("compiled workflow contains a node with empty ID")
	}
	if _, exists := seen[node.ID]; exists {
		return fmt.Errorf("compiled workflow has duplicate node ID %q", node.ID)
	}
	seen[node.ID] = struct{}{}
	wf.Nodes = append(wf.Nodes, node)
	return nil
}

func addCompiledEdge(wf *workflow.Workflow, seen map[string]struct{}, source, target string) {
	if source == "" || target == "" || source == target {
		return
	}
	key := source + "\x00" + target
	if _, exists := seen[key]; exists {
		return
	}
	seen[key] = struct{}{}
	wf.Edges = append(wf.Edges, &workflow.Edge{
		ID:     prefixedEdgeID(source, target),
		Source: source,
		Target: target,
	})
}

func effectiveEdges(nodes []*workflow.Node, explicit []*workflow.Edge) []*workflow.Edge {
	if len(explicit) > 0 {
		edges := make([]*workflow.Edge, 0, len(explicit))
		for _, edge := range explicit {
			if edge == nil || edge.Source == "" || edge.Target == "" {
				continue
			}
			edges = append(edges, &workflow.Edge{
				ID:     edge.ID,
				Source: edge.Source,
				Target: edge.Target,
			})
		}
		return edges
	}
	// Match FreezeWorkflow's legacy behavior: workflows with no explicit edges
	// execute linearly by node list order. Authored parallelism must use edges.
	edges := make([]*workflow.Edge, 0, max(0, len(nodes)-1))
	for i := 0; i < len(nodes)-1; i++ {
		edges = append(edges, &workflow.Edge{
			ID:     prefixedEdgeID(nodes[i].ID, nodes[i+1].ID),
			Source: nodes[i].ID,
			Target: nodes[i+1].ID,
		})
	}
	return edges
}

func rootsAndTerminals(nodes []*workflow.Node, edges []*workflow.Edge) ([]string, []string) {
	incoming := make(map[string]int, len(nodes))
	outgoing := make(map[string]int, len(nodes))
	for _, node := range nodes {
		incoming[node.ID] = 0
		outgoing[node.ID] = 0
	}
	for _, edge := range edges {
		outgoing[edge.Source]++
		incoming[edge.Target]++
	}

	roots := make([]string, 0)
	terminals := make([]string, 0)
	for _, node := range nodes {
		if incoming[node.ID] == 0 {
			roots = append(roots, node.ID)
		}
		if outgoing[node.ID] == 0 {
			terminals = append(terminals, node.ID)
		}
	}
	return roots, terminals
}

func selectOutputTerminal(outputKey string, childNodes []*workflow.Node, idMap map[string][]string) (string, bool) {
	for _, childNode := range childNodes {
		if childNode.ID == outputKey || childNode.OutputName == outputKey || metadataString(childNode.Metadata, "output_name") == outputKey {
			mapped := idMap[childNode.ID]
			if len(mapped) > 0 {
				return mapped[0], true
			}
		}
	}
	return "", false
}

func ensureSourceMetadata(node *workflow.Node, workflowID, sourceHash, sourceNodeID string) {
	if node.Metadata == nil {
		node.Metadata = make(map[string]interface{})
	}
	if _, exists := node.Metadata["source_workflow_id"]; !exists {
		node.Metadata["source_workflow_id"] = workflowID
	}
	if _, exists := node.Metadata["source_workflow_hash"]; !exists {
		node.Metadata["source_workflow_hash"] = sourceHash
	}
	if _, exists := node.Metadata["source_node_id"]; !exists {
		node.Metadata["source_node_id"] = sourceNodeID
	}
}

func prefixNestedAggregationMetadata(node *workflow.Node, prefix string) {
	if node == nil || node.Metadata == nil || node.Metadata["aggregation_group_node_id"] == nil {
		return
	}
	prefixMetadataValue := func(metadata map[string]interface{}, key string) {
		value, ok := metadata[key].(string)
		value = strings.TrimSpace(value)
		if !ok || value == "" || strings.HasPrefix(value, prefix+generatedIDDelimiter) {
			return
		}
		metadata[key] = prefix + generatedIDDelimiter + value
	}
	prefixMetadataValue(node.Metadata, "aggregation_anchor_id")
	prefixMetadataValue(node.Metadata, "source_parent_node_id")
	prefixNestedAggregationMetadata(node.TrueBranch, prefix)
	prefixNestedAggregationMetadata(node.FalseBranch, prefix)
}

func applyInputTemplate(node *workflow.Node, inputTemplate map[string]string) {
	if len(inputTemplate) == 0 {
		return
	}
	for key, value := range inputTemplate {
		node.Prompt = rewriteInputTemplateText(node.Prompt, key, value)
		node.SystemPrompt = rewriteInputTemplateText(node.SystemPrompt, key, value)
		node.Condition = rewriteInputTemplateText(node.Condition, key, value)
		rewriteInputTemplateMap(node.InputTemplate, key, value)
		rewriteInputTemplateMap(node.ChildInputTemplate, key, value)
	}
}

func rewriteNodeReferences(node *workflow.Node, replacements map[string][]string) error {
	if len(replacements) == 0 || node == nil {
		return nil
	}
	var err error
	if node.Prompt, err = rewriteReferenceTemplateText(node.Prompt, replacements); err != nil {
		return err
	}
	if node.SystemPrompt, err = rewriteReferenceTemplateText(node.SystemPrompt, replacements); err != nil {
		return err
	}
	if node.Condition, err = rewriteConditionReferences(node.Condition, replacements); err != nil {
		return err
	}
	if node.InheritFromNodeID != "" {
		if toIDs, ok := replacements[node.InheritFromNodeID]; ok {
			if len(toIDs) != 1 {
				return fmt.Errorf("ambiguous inherit_from_node_id reference %q expands to %d nodes", node.InheritFromNodeID, len(toIDs))
			}
			node.InheritFromNodeID = toIDs[0]
		}
	}
	if err := rewriteReferenceStringMap(node.InputTemplate, replacements); err != nil {
		return err
	}
	if err := rewriteReferenceStringMap(node.ChildInputTemplate, replacements); err != nil {
		return err
	}
	if err := rewriteAggregationConfigReferences(node.AggregationConfig, replacements); err != nil {
		return err
	}
	if err := rewriteOperationConfigReferences(node.OperationConfig, replacements); err != nil {
		return err
	}
	rewriteMetadataInputIDs(node.Metadata, replacements)
	if err := rewriteMetadataNodeID(node.Metadata, "aggregation_group_node_id", replacements); err != nil {
		return err
	}
	if err := rewriteNodeReferences(node.TrueBranch, replacements); err != nil {
		return err
	}
	if err := rewriteNodeReferences(node.FalseBranch, replacements); err != nil {
		return err
	}
	return nil
}

func rewriteReferenceTemplateText(text string, replacements map[string][]string) (string, error) {
	if text == "" {
		return text, nil
	}
	return rewriteTemplateTokens(text, func(key string) (string, bool, error) {
		to, ok, err := templateReferenceReplacement(key, replacements)
		if err != nil {
			return "", false, err
		}
		if !ok {
			return "", false, nil
		}
		return "{{" + to + "}}", true, nil
	})
}

func rewriteConditionReferences(text string, replacements map[string][]string) (string, error) {
	rewritten, err := rewriteReferenceTemplateText(text, replacements)
	if err != nil {
		return "", err
	}
	if rewritten == "" {
		return rewritten, nil
	}
	for from, toIDs := range replacements {
		pattern := regexp.MustCompile(`(^|\s)` + regexp.QuoteMeta(from) + `(\s|$)`)
		if !pattern.MatchString(rewritten) {
			continue
		}
		if len(toIDs) != 1 {
			return "", fmt.Errorf("ambiguous condition reference %q expands to %d nodes", from, len(toIDs))
		}
		to := toIDs[0]
		rewritten = pattern.ReplaceAllStringFunc(rewritten, func(match string) string {
			prefix := ""
			suffix := ""
			if len(match) > 0 && strings.TrimSpace(match[:1]) == "" {
				prefix = match[:1]
				match = match[1:]
			}
			if len(match) > 0 && strings.TrimSpace(match[len(match)-1:]) == "" {
				suffix = match[len(match)-1:]
			}
			return prefix + to + suffix
		})
	}
	return rewritten, nil
}

func rewriteReferenceStringMap(values map[string]string, replacements map[string][]string) error {
	for key, value := range values {
		rewritten, err := rewriteReferenceTemplateText(value, replacements)
		if err != nil {
			return err
		}
		values[key] = rewritten
	}
	return nil
}

func rewriteInputTemplateText(text, key, value string) string {
	if text == "" {
		return text
	}
	rewritten, _ := rewriteTemplateTokens(text, func(found string) (string, bool, error) {
		if found != key {
			return "", false, nil
		}
		return value, true, nil
	})
	return rewritten
}

func rewriteInputTemplateMap(values map[string]string, key, replacement string) {
	for mapKey, value := range values {
		values[mapKey] = rewriteInputTemplateText(value, key, replacement)
	}
}

func rewriteMetadataInputIDs(metadata map[string]interface{}, replacements map[string][]string) {
	if metadata == nil {
		return
	}
	value, ok := metadata["input_ids"]
	if !ok {
		return
	}
	switch typed := value.(type) {
	case []string:
		metadata["input_ids"] = rewriteStringIDs(typed, replacements)
	case []interface{}:
		ids := make([]string, 0, len(typed))
		for _, item := range typed {
			if id, ok := item.(string); ok {
				ids = append(ids, id)
			}
		}
		rewritten := rewriteStringIDs(ids, replacements)
		asInterface := make([]interface{}, len(rewritten))
		for i, id := range rewritten {
			asInterface[i] = id
		}
		metadata["input_ids"] = asInterface
	}
}

func rewriteMetadataNodeID(metadata map[string]interface{}, key string, replacements map[string][]string) error {
	if metadata == nil {
		return nil
	}
	value, ok := metadata[key]
	if !ok {
		return nil
	}
	id, ok := value.(string)
	if !ok || id == "" {
		return nil
	}
	toIDs, ok := replacements[id]
	if !ok {
		return nil
	}
	if len(toIDs) != 1 {
		return fmt.Errorf("ambiguous metadata %q reference %q expands to %d nodes", key, id, len(toIDs))
	}
	metadata[key] = toIDs[0]
	return nil
}

func rewriteStringIDs(ids []string, replacements map[string][]string) []string {
	rewritten := make([]string, 0, len(ids))
	for _, id := range ids {
		if replacement, ok := replacements[id]; ok {
			rewritten = append(rewritten, replacement...)
			continue
		}
		rewritten = append(rewritten, id)
	}
	return rewritten
}

func cloneReferenceRewritesExcept(replacements map[string][]string, excluded string) map[string][]string {
	if len(replacements) == 0 {
		return nil
	}
	cloned := make(map[string][]string, len(replacements))
	for key, value := range replacements {
		if key == excluded {
			continue
		}
		cloned[key] = append([]string(nil), value...)
	}
	return cloned
}

func rewriteAggregationConfigReferences(config map[string]interface{}, replacements map[string][]string) error {
	if len(config) == 0 {
		return nil
	}
	for key, value := range config {
		rewritten, err := rewriteReferenceValue(value, replacements)
		if err != nil {
			return fmt.Errorf("aggregation_config[%s]: %w", key, err)
		}
		config[key] = rewritten
	}
	return nil
}

func rewriteOperationConfigReferences(config map[string]interface{}, replacements map[string][]string) error {
	if len(config) == 0 {
		return nil
	}
	for key, value := range config {
		rewritten, err := rewriteReferenceValue(value, replacements)
		if err != nil {
			return fmt.Errorf("operation_config[%s]: %w", key, err)
		}
		config[key] = rewritten
	}
	return nil
}

func rewriteReferenceValue(value interface{}, replacements map[string][]string) (interface{}, error) {
	switch typed := value.(type) {
	case string:
		if rewritten, ok, err := bareReferenceReplacement(typed, replacements); ok || err != nil {
			return rewritten, err
		}
		return rewriteReferenceTemplateText(typed, replacements)
	case []interface{}:
		rewritten := make([]interface{}, len(typed))
		for i, item := range typed {
			value, err := rewriteReferenceValue(item, replacements)
			if err != nil {
				return nil, err
			}
			rewritten[i] = value
		}
		return rewritten, nil
	case []string:
		rewritten := make([]string, len(typed))
		for i, item := range typed {
			value, err := rewriteReferenceString(item, replacements)
			if err != nil {
				return nil, err
			}
			rewritten[i] = value
		}
		return rewritten, nil
	case map[string]interface{}:
		rewritten := make(map[string]interface{}, len(typed))
		for key, item := range typed {
			value, err := rewriteReferenceValue(item, replacements)
			if err != nil {
				return nil, err
			}
			rewrittenKey, err := rewriteReferenceMapKey(key, replacements)
			if err != nil {
				return nil, err
			}
			rewritten[rewrittenKey] = value
		}
		return rewritten, nil
	case map[string]string:
		rewritten := make(map[string]string, len(typed))
		for key, item := range typed {
			value, err := rewriteReferenceString(item, replacements)
			if err != nil {
				return nil, err
			}
			rewrittenKey, err := rewriteReferenceMapKey(key, replacements)
			if err != nil {
				return nil, err
			}
			rewritten[rewrittenKey] = value
		}
		return rewritten, nil
	default:
		return value, nil
	}
}

func rewriteReferenceString(value string, replacements map[string][]string) (string, error) {
	if rewritten, ok, err := bareReferenceReplacement(value, replacements); ok || err != nil {
		return rewritten, err
	}
	return rewriteReferenceTemplateText(value, replacements)
}

func rewriteReferenceMapKey(key string, replacements map[string][]string) (string, error) {
	if rewritten, ok, err := bareReferenceReplacement(key, replacements); ok || err != nil {
		return rewritten, err
	}
	return key, nil
}

func bareReferenceReplacement(id string, replacements map[string][]string) (string, bool, error) {
	toIDs, ok := replacements[id]
	if !ok {
		return "", false, nil
	}
	if len(toIDs) != 1 {
		return "", false, fmt.Errorf("ambiguous bare reference %q expands to %d nodes", id, len(toIDs))
	}
	return toIDs[0], true, nil
}

func rewriteTemplateTokens(text string, replace func(key string) (string, bool, error)) (string, error) {
	matches := templateReferencePattern.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return text, nil
	}

	var builder strings.Builder
	last := 0
	for _, match := range matches {
		keyStart, keyEnd := match[2], match[3]
		key := strings.TrimSpace(text[keyStart:keyEnd])
		replacement, ok, err := replace(key)
		if err != nil {
			return "", err
		}
		if !ok {
			continue
		}
		builder.WriteString(text[last:match[0]])
		builder.WriteString(replacement)
		last = match[1]
	}
	if last == 0 {
		return text, nil
	}
	builder.WriteString(text[last:])
	return builder.String(), nil
}

func templateReferenceReplacement(key string, replacements map[string][]string) (string, bool, error) {
	if toIDs, ok := replacements[key]; ok {
		return singleTemplateReplacement(key, toIDs)
	}
	if base, suffix, ok := strings.Cut(key, "__"); ok {
		if toIDs, exists := replacements[base]; exists {
			replacement, ok, err := singleTemplateReplacement(base, toIDs)
			if err != nil || !ok {
				return "", ok, err
			}
			return replacement + "__" + suffix, true, nil
		}
	}
	return "", false, nil
}

func singleTemplateReplacement(key string, toIDs []string) (string, bool, error) {
	if len(toIDs) == 0 {
		return "", false, nil
	}
	if len(toIDs) > 1 {
		return "", false, fmt.Errorf("ambiguous template reference %q expands to %d terminal nodes; use metadata input_ids or output_key", key, len(toIDs))
	}
	return toIDs[0], true, nil
}

func prefixNestedReport(report *Report, prefix string, nested *Report) {
	for _, child := range nested.ChildRefs {
		report.ChildRefs = append(report.ChildRefs, ChildRef{
			NodeID:     prefix + generatedIDDelimiter + child.NodeID,
			WorkflowID: child.WorkflowID,
		})
	}
	for _, expanded := range nested.ExpandedRefs {
		nodeIDs := make([]string, len(expanded.ExpandedNodeIDs))
		for i, id := range expanded.ExpandedNodeIDs {
			nodeIDs[i] = prefix + generatedIDDelimiter + id
		}
		report.ExpandedRefs = append(report.ExpandedRefs, ExpandedRef{
			NodeID:          prefix + generatedIDDelimiter + expanded.NodeID,
			WorkflowID:      expanded.WorkflowID,
			SourceHash:      expanded.SourceHash,
			ExpandedNodeIDs: nodeIDs,
		})
	}
}

func prefixedEdgeID(source, target string) string {
	return source + generatedIDDelimiter + target
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func metadataString(metadata map[string]interface{}, key string) string {
	if metadata == nil {
		return ""
	}
	value, _ := metadata[key].(string)
	return value
}

func cloneWorkflow(wf *workflow.Workflow) (*workflow.Workflow, error) {
	var clone workflow.Workflow
	if err := cloneJSON(wf, &clone); err != nil {
		return nil, err
	}
	return &clone, nil
}

func cloneNode(node *workflow.Node) *workflow.Node {
	if node == nil {
		return nil
	}
	var clone workflow.Node
	if err := cloneJSON(node, &clone); err != nil {
		panic(fmt.Sprintf("clone workflow node %q: %v", node.ID, err))
	}
	return &clone
}

func cloneJSON(src interface{}, dst interface{}) error {
	data, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}
