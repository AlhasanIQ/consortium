package compiler

import (
	"context"
	"fmt"
	"strings"

	"github.com/alhasaniq/consortium/pkg/workflow"
)

var aggregationWorkflowIDsByMethod = map[workflow.AggregationMethod]string{
	workflow.AggMethodCollect:      "aggregation-collect",
	workflow.AggMethodMajorityVote: "aggregation-majority-vote",
	workflow.AggMethodSynthesis:    "aggregation-synthesis",
	workflow.AggMethodJudge:        "aggregation-judge",
	workflow.AggMethodDebateDecide: "aggregation-debate-decide",
	workflow.AggMethodScoring:      "aggregation-scoring",
	workflow.AggMethodPeerMatrix:   "aggregation-peer-matrix",
}

type aggregationBinding struct {
	node             *workflow.Node
	workflowID       string
	sourceHash       string
	method           workflow.AggregationMethod
	candidateIDs     []string
	candidateModels  map[string]string
	config           map[string]interface{}
	outputName       string
	outputFormat     string
	timeoutSeconds   int
	retryPolicy      *workflow.RetryPolicy
	absorbedResultID string
}

type aggregationDAGBuilder struct {
	binding          *aggregationBinding
	nodes            []*workflow.Node
	edges            []*workflow.Edge
	last             string
	contextOnlyRoots map[string]struct{}
	rootInputs       map[string][]string
}

type peerScoreParse struct {
	candidateID string
	parseID     string
}

func isAggregationWorkflowRef(node *workflow.Node) bool {
	if node == nil || node.Type != workflow.NodeTypeWorkflowRef {
		return false
	}
	if node.AggregationMethod != "" {
		return true
	}
	if node.Metadata == nil {
		return false
	}
	if _, ok := node.Metadata["aggregation_anchor_id"]; ok {
		return true
	}
	if _, ok := node.Metadata["aggregation_method"]; ok {
		return true
	}
	return false
}

func expandAggregationRef(ctx context.Context, parent *workflow.Workflow, node *workflow.Node, resolver Resolver, stack []string, report *Report) (*expansion, error) {
	binding, err := aggregationBindingFromNode(parent, node)
	if err != nil {
		return nil, err
	}
	if containsString(stack, binding.workflowID) {
		return nil, fmt.Errorf("workflow reference cycle detected: %s -> %s", strings.Join(stack, " -> "), binding.workflowID)
	}
	if resolver == nil {
		return nil, fmt.Errorf("aggregation workflow_ref node %q requires a resolver for %q", node.ID, binding.workflowID)
	}
	child, sourceHash, err := resolver.ResolveWorkflow(ctx, binding.workflowID)
	if err != nil {
		return nil, fmt.Errorf("resolve aggregation workflow_ref node %q (%s): %w", node.ID, binding.workflowID, err)
	}
	if child == nil {
		return nil, fmt.Errorf("resolve aggregation workflow_ref node %q (%s): resolver returned nil workflow", node.ID, binding.workflowID)
	}
	if child.ID == "" {
		child.ID = binding.workflowID
	}
	binding.sourceHash = sourceHash

	builder := &aggregationDAGBuilder{binding: binding}
	if err := builder.build(); err != nil {
		return nil, err
	}
	roots, _ := rootsAndTerminals(builder.nodes, builder.edges)
	roots = builder.parentInputRoots(roots)
	if len(builder.nodes) == 0 || len(roots) == 0 || builder.last == "" {
		return nil, fmt.Errorf("aggregation workflow_ref node %q did not produce a reachable DAG", node.ID)
	}

	nodeIDs := make([]string, 0, len(builder.nodes))
	for _, expandedNode := range builder.nodes {
		nodeIDs = append(nodeIDs, expandedNode.ID)
	}
	report.ExpandedRefs = append(report.ExpandedRefs, ExpandedRef{
		NodeID:          node.ID,
		WorkflowID:      binding.workflowID,
		SourceHash:      sourceHash,
		ExpandedNodeIDs: nodeIDs,
	})

	return &expansion{
		originalID:     node.ID,
		workflowID:     binding.workflowID,
		sourceHash:     sourceHash,
		absorbedNodeID: binding.absorbedResultID,
		nodes:          builder.nodes,
		edges:          builder.edges,
		roots:          roots,
		terminals:      []string{builder.last},
		nodeIDs:        nodeIDs,
		rootInputs:     builder.rootInputs,
	}, nil
}

func aggregationBindingFromNode(parent *workflow.Workflow, node *workflow.Node) (*aggregationBinding, error) {
	workflowID := strings.TrimSpace(node.WorkflowRefID)
	if workflowID == "" {
		return nil, fmt.Errorf("aggregation workflow_ref node %q missing workflow_ref_id", node.ID)
	}
	method, err := aggregationMethodFromNode(node, workflowID)
	if err != nil {
		return nil, err
	}
	if method == "" {
		return nil, fmt.Errorf("aggregation workflow_ref node %q missing aggregation method", node.ID)
	}
	if err := workflow.ValidateAggregationConfig(method, node.AggregationConfig); err != nil {
		return nil, fmt.Errorf("aggregation workflow_ref node %q invalid aggregation config: %w", node.ID, err)
	}
	candidateIDs := node.DisplayMeta().InputIDs
	if len(candidateIDs) == 0 {
		candidateIDs = incomingNodeIDs(parent, node.ID)
	}
	if len(candidateIDs) == 0 {
		return nil, fmt.Errorf("aggregation workflow_ref node %q requires at least one upstream candidate", node.ID)
	}

	outputName := metadataString(node.Metadata, "output_name")
	outputFormat := metadataString(node.Metadata, "output_format")
	absorbedResultID := ""
	if result := directPresentationResult(parent, node.ID); result != nil {
		absorbedResultID = result.ID
		if result.OutputName != "" {
			outputName = result.OutputName
		} else if meta := metadataString(result.Metadata, "output_name"); meta != "" {
			outputName = meta
		}
		if result.OutputFormat != "" {
			outputFormat = result.OutputFormat
		} else if meta := metadataString(result.Metadata, "output_format"); meta != "" {
			outputFormat = meta
		}
	}
	if outputName == "" {
		outputName = node.OutputKey
	}
	if outputName == "" {
		outputName = node.ID
	}
	if outputFormat == "" {
		outputFormat = "text"
	}

	return &aggregationBinding{
		node:             node,
		workflowID:       workflowID,
		method:           method,
		candidateIDs:     candidateIDs,
		candidateModels:  candidateModels(parent, candidateIDs),
		config:           cloneInterfaceMap(node.AggregationConfig),
		outputName:       outputName,
		outputFormat:     outputFormat,
		timeoutSeconds:   node.TimeoutSeconds,
		retryPolicy:      node.RetryPolicy.Clone(),
		absorbedResultID: absorbedResultID,
	}, nil
}

func aggregationMethodFromNode(node *workflow.Node, workflowID string) (workflow.AggregationMethod, error) {
	explicitMethod := node.AggregationMethod
	if metadataMethod := metadataString(node.Metadata, "aggregation_method"); metadataMethod != "" {
		if explicitMethod != "" && explicitMethod != workflow.AggregationMethod(metadataMethod) {
			return "", fmt.Errorf(
				"aggregation workflow_ref node %q has conflicting aggregation methods %q and %q",
				node.ID,
				explicitMethod,
				metadataMethod,
			)
		}
		explicitMethod = workflow.AggregationMethod(metadataMethod)
	}
	if sourceMethod, ok := aggregationMethodForWorkflowID(workflowID); ok {
		if explicitMethod != "" && explicitMethod != sourceMethod {
			return "", fmt.Errorf(
				"aggregation workflow_ref node %q source workflow %q maps to aggregation method %q, but node declares %q",
				node.ID,
				workflowID,
				sourceMethod,
				explicitMethod,
			)
		}
		return sourceMethod, nil
	}
	return explicitMethod, nil
}

func aggregationMethodForWorkflowID(workflowID string) (workflow.AggregationMethod, bool) {
	for method, id := range aggregationWorkflowIDsByMethod {
		if id == workflowID {
			return method, true
		}
	}
	return "", false
}

func incomingNodeIDs(parent *workflow.Workflow, nodeID string) []string {
	ids := make([]string, 0)
	for _, edge := range effectiveEdges(parent.Nodes, parent.Edges) {
		if edge.Target == nodeID && edge.Source != "" {
			ids = append(ids, edge.Source)
		}
	}
	return ids
}

func directPresentationResult(parent *workflow.Workflow, nodeID string) *workflow.Node {
	for _, edge := range effectiveEdges(parent.Nodes, parent.Edges) {
		if edge.Source != nodeID {
			continue
		}
		for _, node := range parent.Nodes {
			if node.ID == edge.Target && node.Type == workflow.NodeTypeResult {
				return node
			}
		}
	}
	return nil
}

func candidateModels(parent *workflow.Workflow, candidateIDs []string) map[string]string {
	byID := make(map[string]*workflow.Node, len(parent.Nodes))
	for _, node := range parent.Nodes {
		byID[node.ID] = node
	}
	models := make(map[string]string, len(candidateIDs))
	for _, id := range candidateIDs {
		if node := byID[id]; node != nil {
			models[id] = node.Model
		}
	}
	return models
}

func (b *aggregationDAGBuilder) build() error {
	switch b.binding.method {
	case workflow.AggMethodCollect:
		return b.buildCollect()
	case workflow.AggMethodMajorityVote:
		return b.buildMajorityVote()
	case workflow.AggMethodSynthesis:
		return b.buildSynthesis()
	case workflow.AggMethodJudge:
		return b.buildJudge()
	case workflow.AggMethodDebateDecide:
		return b.buildDebateDecide()
	case workflow.AggMethodScoring:
		return b.buildScoring()
	case workflow.AggMethodPeerMatrix:
		return b.buildPeerMatrix()
	default:
		return fmt.Errorf("aggregation workflow_ref node %q has unsupported method %q", b.binding.node.ID, b.binding.method)
	}
}

func (b *aggregationDAGBuilder) buildCollect() error {
	collect := b.operation("collect-inputs", workflow.OperationCollectInputs, map[string]interface{}{
		"input_ids": interfaceSliceFromStrings(b.binding.candidateIDs),
		"separator": stringConfigFromMap(b.binding.config, "separator", "\n---\n"),
		"raw_text":  true,
	})
	b.addNode(collect)
	b.markParentInputRoot(collect.ID, b.binding.candidateIDs)
	b.finishWithResult(collect.ID)
	return nil
}

func (b *aggregationDAGBuilder) buildMajorityVote() error {
	answerIDs := b.addAnswerExtractionFanout()
	count := b.operation("count-votes", workflow.OperationCountVotes, map[string]interface{}{"answers": templateList(answerIDs)})
	b.addNode(count)
	b.chainAll(answerIDs, count.ID)

	fallbackOutput := ""
	selectionDependencyID := count.ID
	if strings.EqualFold(stringConfigFromMap(b.binding.config, "tie_breaker_method", "error"), "synthesis") {
		format := b.operation("format-candidates", workflow.OperationFormatCandidates, map[string]interface{}{
			"input_ids": interfaceSliceFromStrings(b.binding.candidateIDs),
			"blind":     true,
			"raw_text":  true,
		})
		b.addNode(format)
		b.addEdge(count.ID, format.ID)

		tie := b.conditional("tie-policy", fmt.Sprintf("%s contains \"tie\":true OR %s contains \"winner\":\"\"", count.ID, count.ID))
		tie.TrueBranch = b.promptBranch("tie-synthesis", stringConfigFromMap(b.binding.config, "tie_breaker_model", ""), stringConfigFromMap(b.binding.config, "system_prompt", ""), b.renderPrompt(stringConfigFromMap(b.binding.config, "prompt", "{{responses}}"), "format-candidates"), floatConfigFromMap(b.binding.config, "tie_breaker_temperature", 0), intConfigFromMap(b.binding.config, "max_tokens", 256))
		b.addNode(tie)
		b.addEdge(format.ID, tie.ID)
		fallbackOutput = "{{" + tie.ID + "}}"
		selectionDependencyID = tie.ID
	}
	selectWinner := b.operation("select-winner", workflow.OperationSelectWinner, map[string]interface{}{
		"vote_result":        "{{" + count.ID + "}}",
		"items":              answerItemsForCandidates(b.binding.candidateIDs, answerIDs),
		"tie_breaker_method": stringConfigFromMap(b.binding.config, "tie_breaker_method", "error"),
		"fallback_output":    fallbackOutput,
		"raw_output":         true,
	})
	b.addNode(selectWinner)
	b.addEdge(selectionDependencyID, selectWinner.ID)

	b.finishWithResult(selectWinner.ID)
	return nil
}

func (b *aggregationDAGBuilder) buildSynthesis() error {
	format := b.operation("format-candidates", workflow.OperationFormatCandidates, map[string]interface{}{
		"input_ids": interfaceSliceFromStrings(b.binding.candidateIDs),
		"blind":     true,
		"raw_text":  true,
	})
	b.addNode(format)
	b.markParentInputRoot(format.ID, b.binding.candidateIDs)
	synthesize := b.prompt("synthesize", stringConfigFromMap(b.binding.config, "model", b.firstCandidateModel()), stringConfigFromMap(b.binding.config, "system_prompt", ""), b.renderPrompt(stringConfigFromMap(b.binding.config, "prompt", "{{responses}}"), "format-candidates"), floatConfigFromMap(b.binding.config, "temperature", 0), intConfigFromMap(b.binding.config, "max_tokens", 256))
	b.addNode(synthesize)
	b.addEdge(format.ID, synthesize.ID)
	b.finishWithResult(synthesize.ID)
	return nil
}

func (b *aggregationDAGBuilder) buildJudge() error {
	answerIDs := b.addAnswerExtractionFanout()
	check := b.operation("check-unanimous", workflow.OperationCountVotes, map[string]interface{}{"answers": templateList(answerIDs)})
	b.addNode(check)
	b.chainAll(answerIDs, check.ID)
	unanimous := b.operation("unanimous-policy", workflow.OperationSelectWinner, map[string]interface{}{
		"vote_result":       "{{" + check.ID + "}}",
		"items":             answerItemsForCandidates(b.binding.candidateIDs, answerIDs),
		"require_unanimous": true,
		"raw_output":        true,
	})
	b.addNode(unanimous)
	b.addEdge(check.ID, unanimous.ID)
	format := b.operation("format-blind-candidates", workflow.OperationFormatCandidates, map[string]interface{}{
		"input_ids": interfaceSliceFromStrings(b.binding.candidateIDs),
		"blind":     true,
		"raw_text":  true,
	})
	b.addNode(format)
	judge := b.prompt("judge", stringConfigFromMap(b.binding.config, "judge_model", ""), stringConfigFromMap(b.binding.config, "system_prompt", ""), b.renderPrompt(stringConfigFromMap(b.binding.config, "prompt", "{{responses}}"), "format-blind-candidates"), floatConfigFromMap(b.binding.config, "temperature", 0), intConfigFromMap(b.binding.config, "max_tokens", 256))
	b.addNode(judge)
	parse := b.operation("parse-selection", workflow.OperationParseSelection, map[string]interface{}{"text": "{{" + judge.ID + "}}", "labels": interfaceSliceFromStrings(candidateLabelsForIDs(b.binding.candidateIDs)), "raw": true})
	b.addNode(parse)
	repair := b.conditional("repair-selection", fmt.Sprintf("%s is_empty", parse.ID))
	repair.TrueBranch = b.promptBranch("repair-selection-call", stringConfigFromMap(b.binding.config, "judge_model", ""), "", "Return only the winning response label. Valid labels are: "+strings.Join(candidateLabelsForIDs(b.binding.candidateIDs), ", ")+"\n\n{{"+judge.ID+"}}", floatConfigFromMap(b.binding.config, "temperature", 0), intConfigFromMap(b.binding.config, "repair_max_tokens", 128))
	b.addNode(repair)
	selectWinner := b.operation("select-winner", workflow.OperationSelectWinner, map[string]interface{}{
		"selection":          "{{" + parse.ID + "}}",
		"fallback_selection": "{{" + repair.ID + "}}",
		"labels":             interfaceSliceFromStrings(candidateLabelsForIDs(b.binding.candidateIDs)),
		"label_to_id":        labelToIDMap(b.binding.candidateIDs),
		"outputs":            outputTemplateMap(b.binding.candidateIDs),
		"fallback_output":    "{{" + unanimous.ID + "}}",
		"raw_output":         true,
	})
	b.addNode(selectWinner)
	b.chain(format.ID, judge.ID, parse.ID, repair.ID, selectWinner.ID)
	b.addEdge(unanimous.ID, selectWinner.ID)
	b.finishWithResult(selectWinner.ID)
	return nil
}

func (b *aggregationDAGBuilder) buildDebateDecide() error {
	answerIDs := b.addAnswerExtractionFanout()
	check := b.operation("check-unanimous", workflow.OperationCountVotes, map[string]interface{}{"answers": templateList(answerIDs)})
	b.addNode(check)
	group := b.operation("group-camps", workflow.OperationGroupAnswerCamps, map[string]interface{}{"answers": templateList(answerIDs)})
	b.addNode(group)
	b.chainAll(answerIDs, group.ID)
	b.chainAll(answerIDs, check.ID)
	policy := b.operation("single-camp-policy", workflow.OperationSelectWinner, map[string]interface{}{
		"vote_result":       "{{" + check.ID + "}}",
		"items":             answerItemsForCandidates(b.binding.candidateIDs, answerIDs),
		"require_unanimous": true,
		"raw_output":        true,
	})
	b.addNode(policy)
	b.addEdge(check.ID, policy.ID)
	format := b.operation("format-candidates", workflow.OperationFormatCandidates, map[string]interface{}{
		"input_ids": interfaceSliceFromStrings(b.binding.candidateIDs),
		"blind":     true,
		"raw_text":  true,
	})
	b.addNode(format)
	b.addEdge(group.ID, format.ID)
	noCamps := b.conditional("no-camps-policy", fmt.Sprintf("%s contains \"winner\":\"\"", check.ID))
	noCamps.TrueBranch = b.promptBranch("no-camps-synthesis", stringConfigFromMap(b.binding.config, "judge_model", ""), workflow.DefaultSynthesisSystemPrompt, b.renderPrompt(workflow.DefaultSynthesisPrompt, "format-candidates"), floatConfigFromMap(b.binding.config, "temperature", 0), intConfigFromMap(b.binding.config, "max_tokens", 256))
	b.addNode(noCamps)
	b.addEdge(format.ID, noCamps.ID)
	b.addEdge(check.ID, noCamps.ID)
	judge := b.prompt("judge-camp", stringConfigFromMap(b.binding.config, "judge_model", ""), stringConfigFromMap(b.binding.config, "system_prompt", ""), b.renderPrompt(stringConfigFromMap(b.binding.config, "prompt", "{{camps}}"), "group-camps"), floatConfigFromMap(b.binding.config, "temperature", 0), intConfigFromMap(b.binding.config, "max_tokens", 256))
	b.addNode(judge)
	parse := b.operation("parse-selection", workflow.OperationParseSelection, map[string]interface{}{"text": "{{" + judge.ID + "}}", "labels": templateList(answerIDs), "raw": true})
	b.addNode(parse)
	repair := b.conditional("repair-selection", fmt.Sprintf("%s is_empty", parse.ID))
	repair.TrueBranch = b.promptBranch("repair-selection-call", stringConfigFromMap(b.binding.config, "judge_model", ""), "", "Return only the winning camp answer.\n\n{{"+judge.ID+"}}", floatConfigFromMap(b.binding.config, "temperature", 0), intConfigFromMap(b.binding.config, "repair_max_tokens", 128))
	b.addNode(repair)
	selectWinner := b.operation("select-winner", workflow.OperationSelectWinner, map[string]interface{}{
		"winner_answer":      "{{" + parse.ID + "}}",
		"fallback_selection": "{{" + repair.ID + "}}",
		"items":              answerItemsForCandidates(b.binding.candidateIDs, answerIDs),
		"fallback_output":    "{{" + noCamps.ID + "}}",
		"secondary_fallback": "{{" + policy.ID + "}}",
		"raw_output":         true,
	})
	b.addNode(selectWinner)
	b.chain(group.ID, judge.ID, parse.ID, repair.ID, selectWinner.ID)
	b.addEdge(policy.ID, selectWinner.ID)
	b.addEdge(noCamps.ID, selectWinner.ID)
	b.finishWithResult(selectWinner.ID)
	return nil
}

func (b *aggregationDAGBuilder) buildScoring() error {
	answerIDs := b.addAnswerExtractionFanout()
	check := b.operation("check-unanimous", workflow.OperationCountVotes, map[string]interface{}{"answers": templateList(answerIDs)})
	b.addNode(check)
	b.chainAll(answerIDs, check.ID)
	unanimous := b.operation("unanimous-policy", workflow.OperationSelectWinner, map[string]interface{}{
		"vote_result":       "{{" + check.ID + "}}",
		"items":             answerItemsForCandidates(b.binding.candidateIDs, answerIDs),
		"require_unanimous": true,
		"raw_output":        true,
	})
	b.addNode(unanimous)
	b.addEdge(check.ID, unanimous.ID)
	rubricReplacement := rubricTextFromConfig(b.binding.config)
	rubricConfig := b.binding.config["rubric"]
	dynamicRubricID := ""
	dynamicRubricTextID := ""
	if b.dynamicRubricEnabled() {
		dynamicRubricID, dynamicRubricTextID = b.addDynamicRubricNodes("scoring_model")
		rubricReplacement = "{{" + dynamicRubricTextID + "}}"
		rubricConfig = "{{" + dynamicRubricID + "}}"
	}
	promptTemplate := stringConfigFromMap(b.binding.config, "prompt", "{{candidate}}")
	formattedResponsesID := b.addFormattedCandidatesForResponses(promptTemplate)
	scoreParseIDs := make([]string, 0, len(b.binding.candidateIDs))
	for _, candidateID := range b.binding.candidateIDs {
		score := b.prompt("score-"+candidateID, stringConfigFromMap(b.binding.config, "scoring_model", ""), stringConfigFromMap(b.binding.config, "system_prompt", ""), b.renderCandidatePromptWithRubric(promptTemplate, candidateID, rubricReplacement), floatConfigFromMap(b.binding.config, "temperature", 0), intConfigFromMap(b.binding.config, "max_tokens", 256))
		b.addNode(score)
		parse := b.operation("parse-score-"+candidateID, workflow.OperationParseJSONField, map[string]interface{}{"text": "{{" + score.ID + "}}", "field": "score", "rubric": rubricConfig, "raw": true})
		b.addNode(parse)
		b.addEdge(check.ID, score.ID)
		if formattedResponsesID != "" {
			b.addEdge(formattedResponsesID, score.ID)
		}
		if dynamicRubricID != "" {
			b.addEdge(dynamicRubricTextID, score.ID)
			b.addEdge(dynamicRubricID, parse.ID)
		}
		b.addEdge(score.ID, parse.ID)
		scoreParseIDs = append(scoreParseIDs, parse.ID)
	}
	reduce := b.operation("reduce-scores", workflow.OperationReduceScores, map[string]interface{}{"scores": scoreItems(b.binding.candidateIDs, scoreParseIDs)})
	b.addNode(reduce)
	b.chainAll(scoreParseIDs, reduce.ID)
	selectWinner := b.operation("select-winner", workflow.OperationSelectWinner, map[string]interface{}{
		"scores":                 "{{" + reduce.ID + "}}",
		"outputs":                outputTemplateMap(b.binding.candidateIDs),
		"fallback_output":        "{{" + unanimous.ID + "}}",
		"prefer_fallback_output": true,
		"raw_output":             true,
	})
	b.addNode(selectWinner)
	b.addEdge(reduce.ID, selectWinner.ID)
	b.addEdge(unanimous.ID, selectWinner.ID)
	b.finishWithResult(selectWinner.ID)
	return nil
}

func (b *aggregationDAGBuilder) buildPeerMatrix() error {
	answerIDs := b.addAnswerExtractionFanout()
	check := b.operation("check-unanimous", workflow.OperationCountVotes, map[string]interface{}{"answers": templateList(answerIDs)})
	b.addNode(check)
	b.chainAll(answerIDs, check.ID)
	unanimous := b.operation("unanimous-policy", workflow.OperationSelectWinner, map[string]interface{}{
		"vote_result":       "{{" + check.ID + "}}",
		"items":             answerItemsForCandidates(b.binding.candidateIDs, answerIDs),
		"require_unanimous": true,
		"raw_output":        true,
	})
	b.addNode(unanimous)
	b.addEdge(check.ID, unanimous.ID)
	rubricReplacement := rubricTextFromConfig(b.binding.config)
	rubricConfig := b.binding.config["rubric"]
	dynamicRubricID := ""
	dynamicRubricTextID := ""
	if b.dynamicRubricEnabled() {
		dynamicRubricID, dynamicRubricTextID = b.addDynamicRubricNodes("rubric_model")
		rubricReplacement = "{{" + dynamicRubricTextID + "}}"
		rubricConfig = "{{" + dynamicRubricID + "}}"
	}
	evalPromptTemplate := stringConfigFromMap(b.binding.config, "eval_prompt", "{{candidate}}")
	formattedResponsesID := b.addFormattedCandidatesForResponses(evalPromptTemplate)
	parses := make([]peerScoreParse, 0, len(b.binding.candidateIDs)*(len(b.binding.candidateIDs)-1))
	for _, reviewerID := range b.binding.candidateIDs {
		for _, candidateID := range b.binding.candidateIDs {
			if reviewerID == candidateID {
				continue
			}
			reviewID := "review-" + reviewerID + "-" + candidateID
			review := b.prompt(reviewID, b.binding.candidateModels[reviewerID], stringConfigFromMap(b.binding.config, "eval_system_prompt", ""), b.renderPeerPromptWithRubric(evalPromptTemplate, reviewerID, candidateID, rubricReplacement), floatConfigFromMap(b.binding.config, "temperature", 0), intConfigFromMap(b.binding.config, "max_tokens", 256))
			b.addNode(review)
			parse := b.operation("parse-"+reviewID, workflow.OperationParseJSONField, map[string]interface{}{"text": "{{" + review.ID + "}}", "field": "score", "rubric": rubricConfig, "raw": true})
			b.addNode(parse)
			b.addEdge(check.ID, review.ID)
			if formattedResponsesID != "" {
				b.addEdge(formattedResponsesID, review.ID)
			}
			if dynamicRubricID != "" {
				b.addEdge(dynamicRubricTextID, review.ID)
				b.addEdge(dynamicRubricID, parse.ID)
			}
			b.addEdge(review.ID, parse.ID)
			parses = append(parses, peerScoreParse{candidateID: candidateID, parseID: parse.ID})
		}
	}
	reduce := b.operation("reduce-peer-scores", workflow.OperationReduceScores, map[string]interface{}{"scores": peerScoreItems(parses)})
	b.addNode(reduce)
	parseIDs := make([]string, 0, len(parses))
	for _, parse := range parses {
		parseIDs = append(parseIDs, parse.parseID)
	}
	b.chainAll(parseIDs, reduce.ID)
	selectWinner := b.operation("select-winner", workflow.OperationSelectWinner, map[string]interface{}{
		"scores":                 "{{" + reduce.ID + "}}",
		"outputs":                outputTemplateMap(b.binding.candidateIDs),
		"fallback_output":        "{{" + unanimous.ID + "}}",
		"prefer_fallback_output": true,
		"raw_output":             true,
	})
	b.addNode(selectWinner)
	b.addEdge(reduce.ID, selectWinner.ID)
	b.addEdge(unanimous.ID, selectWinner.ID)
	b.finishWithResult(selectWinner.ID)
	return nil
}

func (b *aggregationDAGBuilder) addAnswerExtractionFanout() []string {
	answerIDs := make([]string, 0, len(b.binding.candidateIDs))
	for _, candidateID := range b.binding.candidateIDs {
		config := extractionConfigForCandidate(b.binding.config, candidateID)
		config["raw_answer"] = true
		extract := b.operation("extract-answer-"+candidateID, workflow.OperationExtractAnswer, config)
		b.addNode(extract)
		b.markParentInputRoot(extract.ID, []string{candidateID})
		answerIDs = append(answerIDs, extract.ID)
	}
	return answerIDs
}

func (b *aggregationDAGBuilder) finishWithResult(inputID string) {
	result := b.result("result", inputID)
	b.addNode(result)
	b.addEdge(inputID, result.ID)
	b.stampAggregationGroup(result.ID)
	b.last = result.ID
}

func (b *aggregationDAGBuilder) addNode(node *workflow.Node) {
	b.stampNode(node)
	b.nodes = append(b.nodes, node)
}

func (b *aggregationDAGBuilder) stampAggregationGroup(groupNodeID string) {
	for _, node := range b.nodes {
		b.stampAggregationGroupNode(node, groupNodeID)
	}
}

func (b *aggregationDAGBuilder) stampAggregationGroupNode(node *workflow.Node, groupNodeID string) {
	if node == nil {
		return
	}
	prefix := b.binding.node.ID + generatedIDDelimiter
	if node.ID != groupNodeID && strings.HasPrefix(node.ID, prefix) {
		if node.Metadata == nil {
			node.Metadata = map[string]interface{}{}
		}
		node.Metadata["aggregation_group_node_id"] = groupNodeID
	}
	b.stampAggregationGroupNode(node.TrueBranch, groupNodeID)
	b.stampAggregationGroupNode(node.FalseBranch, groupNodeID)
}

func (b *aggregationDAGBuilder) stampNode(node *workflow.Node) {
	if node.Metadata == nil {
		node.Metadata = map[string]interface{}{}
	}
	ensureSourceMetadata(node, b.binding.workflowID, b.binding.sourceHash, sourceNodeIDForGenerated(b.binding.node.ID, node.ID))
	node.Metadata["aggregation_anchor_id"] = b.binding.node.ID
	node.Metadata["aggregation_method"] = string(b.binding.method)
	if _, ok := node.Metadata["source_parent_node_id"]; !ok {
		node.Metadata["source_parent_node_id"] = b.binding.node.ID
	}
}

func (b *aggregationDAGBuilder) addEdge(source, target string) {
	b.edges = append(b.edges, &workflow.Edge{
		ID:     prefixedEdgeID(source, target),
		Source: source,
		Target: target,
	})
}

func (b *aggregationDAGBuilder) chain(ids ...string) {
	for i := 0; i < len(ids)-1; i++ {
		b.addEdge(ids[i], ids[i+1])
	}
}

func (b *aggregationDAGBuilder) chainAll(sources []string, target string) {
	for _, source := range sources {
		b.addEdge(source, target)
	}
}

func (b *aggregationDAGBuilder) id(sourceNodeID string) string {
	return b.binding.node.ID + generatedIDDelimiter + sourceNodeID
}

func (b *aggregationDAGBuilder) baseMetadata(sourceNodeID, label string) map[string]interface{} {
	return map[string]interface{}{
		"label":          label,
		"source_node_id": sourceNodeID,
	}
}

func (b *aggregationDAGBuilder) operation(sourceNodeID, operationType string, config map[string]interface{}) *workflow.Node {
	return &workflow.Node{
		ID:              b.id(sourceNodeID),
		Type:            workflow.NodeTypeOperation,
		OperationType:   operationType,
		OperationConfig: config,
		Metadata:        b.baseMetadata(sourceNodeID, strings.ReplaceAll(sourceNodeID, "-", " ")),
	}
}

func (b *aggregationDAGBuilder) prompt(sourceNodeID, model, systemPrompt, prompt string, temperature float64, maxTokens int) *workflow.Node {
	return &workflow.Node{
		ID:             b.id(sourceNodeID),
		Type:           workflow.NodeTypePrompt,
		Model:          strings.TrimSpace(model),
		SystemPrompt:   systemPrompt,
		Prompt:         prompt,
		Temperature:    &temperature,
		MaxTokens:      maxTokens,
		TimeoutSeconds: b.executionTimeoutSeconds(),
		RetryPolicy:    b.executionRetryPolicy(),
		Metadata:       mergePromptMetadata(b.baseMetadata(sourceNodeID, strings.ReplaceAll(sourceNodeID, "-", " ")), b.binding.config),
	}
}

func (b *aggregationDAGBuilder) promptBranch(sourceNodeID, model, systemPrompt, prompt string, temperature float64, maxTokens int) *workflow.Node {
	node := b.prompt(sourceNodeID, model, systemPrompt, prompt, temperature, maxTokens)
	b.stampNode(node)
	return node
}

func (b *aggregationDAGBuilder) dynamicRubricEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(stringConfigFromMap(b.binding.config, "rubric_mode", "")), "dynamic")
}

func (b *aggregationDAGBuilder) addDynamicRubricNodes(modelKey string) (rawRubricID string, textRubricID string) {
	generate := b.prompt(
		"generate-rubric",
		stringConfigFromMap(b.binding.config, modelKey, ""),
		workflow.RubricGeneratorSystemPrompt,
		"Generate scoring criteria for evaluating responses to this question:\n\n{{user_prompt}}",
		0,
		intConfigFromMap(b.binding.config, "rubric_max_tokens", 512),
	)
	b.addNode(generate)
	b.markContextOnlyRoot(generate.ID)
	fallbackRubric := b.binding.config["rubric"]
	if fallbackRubric == nil {
		fallbackRubric = workflow.DefaultRubric
	}
	parseRaw := b.operation("parse-rubric", workflow.OperationParseJSONField, map[string]interface{}{
		"text":     "{{" + generate.ID + "}}",
		"field":    "rubric",
		"raw":      true,
		"json_raw": true,
		"fallback": fallbackRubric,
	})
	b.addNode(parseRaw)
	parseText := b.operation("format-rubric", workflow.OperationParseJSONField, map[string]interface{}{
		"text":          "{{" + generate.ID + "}}",
		"field":         "rubric",
		"raw":           true,
		"format_rubric": true,
		"fallback":      fallbackRubric,
	})
	b.addNode(parseText)
	b.addEdge(generate.ID, parseRaw.ID)
	b.addEdge(generate.ID, parseText.ID)
	return parseRaw.ID, parseText.ID
}

func (b *aggregationDAGBuilder) addFormattedCandidatesForResponses(template string) string {
	if !templateReferencesResponses(template) {
		return ""
	}
	format := b.operation("format-candidates", workflow.OperationFormatCandidates, map[string]interface{}{
		"input_ids": interfaceSliceFromStrings(b.binding.candidateIDs),
		"raw_text":  true,
	})
	b.addNode(format)
	return format.ID
}

func templateReferencesResponses(template string) bool {
	return strings.Contains(template, "{{responses}}") || strings.Contains(template, "{{ responses }}")
}

func (b *aggregationDAGBuilder) markContextOnlyRoot(nodeID string) {
	if b.contextOnlyRoots == nil {
		b.contextOnlyRoots = make(map[string]struct{})
	}
	b.contextOnlyRoots[nodeID] = struct{}{}
}

func (b *aggregationDAGBuilder) markParentInputRoot(nodeID string, candidateIDs []string) {
	if b.rootInputs == nil {
		b.rootInputs = make(map[string][]string)
	}
	b.rootInputs[nodeID] = append([]string(nil), candidateIDs...)
}

func (b *aggregationDAGBuilder) parentInputRoots(roots []string) []string {
	if len(b.contextOnlyRoots) == 0 {
		return roots
	}
	filtered := make([]string, 0, len(roots))
	for _, root := range roots {
		if _, contextOnly := b.contextOnlyRoots[root]; contextOnly {
			continue
		}
		filtered = append(filtered, root)
	}
	return filtered
}

func (b *aggregationDAGBuilder) conditional(sourceNodeID, condition string) *workflow.Node {
	return &workflow.Node{
		ID:          b.id(sourceNodeID),
		Type:        workflow.NodeTypeConditional,
		Condition:   condition,
		RetryPolicy: b.executionRetryPolicy(),
		Metadata:    b.baseMetadata(sourceNodeID, strings.ReplaceAll(sourceNodeID, "-", " ")),
	}
}

func (b *aggregationDAGBuilder) result(sourceNodeID, inputID string) *workflow.Node {
	return &workflow.Node{
		ID:                b.id(sourceNodeID),
		Type:              workflow.NodeTypeResult,
		OutputName:        b.binding.outputName,
		OutputFormat:      b.binding.outputFormat,
		AggregationMethod: workflow.AggMethodCollect,
		RetryPolicy:       b.executionRetryPolicy(),
		Metadata: map[string]interface{}{
			"label":          b.binding.outputName,
			"input_ids":      []interface{}{inputID},
			"output_name":    b.binding.outputName,
			"output_format":  b.binding.outputFormat,
			"source_node_id": sourceNodeID,
		},
	}
}

func (b *aggregationDAGBuilder) executionTimeoutSeconds() int {
	if b.binding.timeoutSeconds > 0 {
		return b.binding.timeoutSeconds
	}
	return 600
}

func (b *aggregationDAGBuilder) executionRetryPolicy() *workflow.RetryPolicy {
	if b.binding.retryPolicy != nil {
		return b.binding.retryPolicy.Clone()
	}
	return workflow.DefaultRetryPolicy()
}

func (b *aggregationDAGBuilder) renderPrompt(template, responseNodeSourceID string) string {
	if strings.TrimSpace(template) == "" {
		template = "{{responses}}"
	}
	text := strings.ReplaceAll(template, "{{prompt}}", "{{user_prompt}}")
	text = strings.ReplaceAll(text, "{{ prompt }}", "{{user_prompt}}")
	text = strings.ReplaceAll(text, "{{question}}", "{{user_prompt}}")
	text = strings.ReplaceAll(text, "{{ question }}", "{{user_prompt}}")
	text = strings.ReplaceAll(text, "{{responses}}", "{{"+b.id(responseNodeSourceID)+"}}")
	text = strings.ReplaceAll(text, "{{ responses }}", "{{"+b.id(responseNodeSourceID)+"}}")
	text = strings.ReplaceAll(text, "{{camps}}", "{{"+b.id(responseNodeSourceID)+"}}")
	text = strings.ReplaceAll(text, "{{ camps }}", "{{"+b.id(responseNodeSourceID)+"}}")
	return text
}

func (b *aggregationDAGBuilder) renderCandidatePromptWithRubric(template, candidateID, rubric string) string {
	text := b.renderPrompt(template, "format-candidates")
	text = strings.ReplaceAll(text, "{{candidate}}", "{{"+candidateID+"}}")
	text = strings.ReplaceAll(text, "{{ candidate }}", "{{"+candidateID+"}}")
	text = strings.ReplaceAll(text, "{{response}}", "{{"+candidateID+"}}")
	text = strings.ReplaceAll(text, "{{ response }}", "{{"+candidateID+"}}")
	text = strings.ReplaceAll(text, "{{rubric}}", rubric)
	text = strings.ReplaceAll(text, "{{ rubric }}", rubric)
	return text
}

func (b *aggregationDAGBuilder) renderPeerPromptWithRubric(template, reviewerID, candidateID, rubric string) string {
	text := b.renderCandidatePromptWithRubric(template, candidateID, rubric)
	text = strings.ReplaceAll(text, "{{reviewer_answer}}", "{{"+reviewerID+"}}")
	text = strings.ReplaceAll(text, "{{ reviewer_answer }}", "{{"+reviewerID+"}}")
	return text
}

func (b *aggregationDAGBuilder) firstCandidateModel() string {
	for _, id := range b.binding.candidateIDs {
		if model := strings.TrimSpace(b.binding.candidateModels[id]); model != "" {
			return model
		}
	}
	return ""
}

func sourceNodeIDForGenerated(parentID, generatedID string) string {
	prefix := parentID + generatedIDDelimiter
	if strings.HasPrefix(generatedID, prefix) {
		return strings.TrimPrefix(generatedID, prefix)
	}
	return generatedID
}

func cloneInterfaceMap(in map[string]interface{}) map[string]interface{} {
	if len(in) == 0 {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func interfaceSliceFromStrings(values []string) []interface{} {
	out := make([]interface{}, len(values))
	for i, value := range values {
		out[i] = value
	}
	return out
}

func templateList(nodeIDs []string) []interface{} {
	out := make([]interface{}, len(nodeIDs))
	for i, id := range nodeIDs {
		out[i] = "{{" + id + "}}"
	}
	return out
}

func extractionConfigForCandidate(config map[string]interface{}, candidateID string) map[string]interface{} {
	out := map[string]interface{}{"text": "{{" + candidateID + "}}"}
	for _, key := range []string{"extraction_strategy", "extraction_pattern", "json_field"} {
		if value, ok := config[key]; ok {
			out[key] = value
		}
	}
	return out
}

func outputTemplateMap(candidateIDs []string) map[string]interface{} {
	out := make(map[string]interface{}, len(candidateIDs))
	for _, id := range candidateIDs {
		out[id] = "{{" + id + "}}"
	}
	return out
}

func labelToIDMap(candidateIDs []string) map[string]interface{} {
	labels := candidateLabelsForIDs(candidateIDs)
	out := make(map[string]interface{}, len(candidateIDs))
	for i, id := range candidateIDs {
		out[labels[i]] = id
	}
	return out
}

func answerItemsForCandidates(candidateIDs, answerNodeIDs []string) []interface{} {
	items := make([]interface{}, 0, len(candidateIDs))
	for i, id := range candidateIDs {
		answerRef := ""
		if i < len(answerNodeIDs) {
			answerRef = "{{" + answerNodeIDs[i] + "}}"
		}
		items = append(items, map[string]interface{}{
			"id":     id,
			"answer": answerRef,
			"output": "{{" + id + "}}",
		})
	}
	return items
}

func scoreItems(candidateIDs, scoreNodeIDs []string) []interface{} {
	items := make([]interface{}, 0, len(candidateIDs))
	for i, id := range candidateIDs {
		scoreRef := ""
		if i < len(scoreNodeIDs) {
			scoreRef = "{{" + scoreNodeIDs[i] + "}}"
		}
		items = append(items, map[string]interface{}{"id": id, "score": scoreRef})
	}
	return items
}

func peerScoreItems(parses []peerScoreParse) []interface{} {
	items := make([]interface{}, 0, len(parses))
	for _, parse := range parses {
		items = append(items, map[string]interface{}{"id": parse.candidateID, "score": "{{" + parse.parseID + "}}"})
	}
	return items
}

func candidateLabelsForIDs(candidateIDs []string) []string {
	return workflow.CandidateLabels(len(candidateIDs))
}

func rubricTextFromConfig(config map[string]interface{}) string {
	rubricData, ok := config["rubric"]
	if !ok {
		return ""
	}
	items, ok := rubricData.([]interface{})
	if !ok {
		return fmt.Sprint(rubricData)
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		criterion, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name := strings.TrimSpace(fmt.Sprint(criterion["name"]))
		if name == "" {
			continue
		}
		weight := fmt.Sprint(criterion["weight"])
		description := strings.TrimSpace(fmt.Sprint(criterion["description"]))
		lines = append(lines, fmt.Sprintf("- %s (%s): %s", name, weight, description))
	}
	return strings.Join(lines, "\n")
}

func stringConfigFromMap(config map[string]interface{}, key, fallback string) string {
	if value, ok := config[key]; ok {
		text := fmt.Sprint(value)
		if strings.TrimSpace(text) != "" {
			return text
		}
	}
	return fallback
}

func floatConfigFromMap(config map[string]interface{}, key string, fallback float64) float64 {
	switch value := config[key].(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	}
	return fallback
}

func intConfigFromMap(config map[string]interface{}, key string, fallback int) int {
	switch value := config[key].(type) {
	case float64:
		return int(value)
	case float32:
		return int(value)
	case int:
		return value
	case int64:
		return int(value)
	}
	return fallback
}

func mergePromptMetadata(base map[string]interface{}, config map[string]interface{}) map[string]interface{} {
	for _, key := range []string{"openrouter_provider", "openRouterProvider", "provider_routing", "providerRouting", "openrouter_reasoning", "openRouterReasoning"} {
		if value, ok := config[key]; ok {
			base[key] = value
		}
	}
	return base
}
