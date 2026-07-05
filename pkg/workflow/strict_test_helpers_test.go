package workflow

import (
	"encoding/json"
	"strings"

	"github.com/alhasaniq/consortium/pkg/providers"
)

func testDefaultRetryPolicy() *RetryPolicy {
	return DefaultRetryPolicy().Clone()
}

func testNoRetryPolicy() *RetryPolicy {
	return NoRetryPolicy().Clone()
}

func withStrictNodeDefaults(node *Node) *Node {
	if node == nil {
		return nil
	}

	if node.RetryPolicy == nil {
		if node.Type == NodeTypePrompt || node.Type == NodeTypeContractExtract || node.Type == NodeTypeChildWorkflow {
			node.RetryPolicy = testDefaultRetryPolicy()
		} else {
			node.RetryPolicy = testNoRetryPolicy()
		}
	}

	switch node.Type {
	case NodeTypePrompt, NodeTypeContractExtract:
		if node.Temperature == nil {
			node.Temperature = providers.Float64Ptr(0.7)
		}
		if node.MaxTokens == 0 {
			node.MaxTokens = 1000
		}
		if node.TimeoutSeconds <= 0 {
			node.TimeoutSeconds = 120
		}
	case NodeTypeChildWorkflow:
		if node.TimeoutSeconds <= 0 {
			node.TimeoutSeconds = 120
		}
	case NodeTypeResult:
		if node.AggregationMethod == "" {
			node.AggregationMethod = AggMethodCollect
		}
		if node.AggregationConfig == nil {
			node.AggregationConfig = map[string]interface{}{}
		}
		ensureAggregationConfigForMethod(node.AggregationMethod, node.AggregationConfig)
	}

	node.TrueBranch = withStrictNodeDefaults(node.TrueBranch)
	node.FalseBranch = withStrictNodeDefaults(node.FalseBranch)
	return node
}

func withStrictWorkflowDefaults(wf *Workflow) *Workflow {
	if wf == nil {
		return nil
	}
	for _, node := range wf.Nodes {
		withStrictNodeDefaults(node)
	}
	return wf
}

func withStrictNodeContextDefaults(sc *NodeContext) *NodeContext {
	if sc == nil {
		return nil
	}
	if sc.WorkflowContext == nil {
		sc.WorkflowContext = map[string]interface{}{}
	}
	sc.Node = withStrictNodeDefaults(sc.Node)
	return sc
}

func ensureAggregationConfigForMethod(method AggregationMethod, cfg map[string]interface{}) {
	switch method {
	case AggMethodCollect:
		return
	case AggMethodJudge:
		setIfMissingTest(cfg, "judge_model", "mock-model")
		setIfMissingTest(cfg, "system_prompt", "You are an impartial judge evaluating candidate responses.")
		setIfMissingTest(cfg, "prompt", "Original Question: {{prompt}}\n\nResponses:\n{{responses}}\n\nReturn WINNER: [label].")
		setIfMissingTest(cfg, "temperature", 0.3)
		setIfMissingTest(cfg, "max_tokens", -1)
		setIfMissingTest(cfg, "repair_max_tokens", 256)
		setIfMissingTest(cfg, "extraction_strategy", "regex")
		setIfMissingTest(cfg, "extraction_pattern", DefaultExtractionPattern)
	case AggMethodScoring:
		setIfMissingTest(cfg, "scoring_model", "mock-model")
		setIfMissingTest(cfg, "system_prompt", "You are an expert evaluator. Score responses against a rubric.")
		setIfMissingTest(cfg, "prompt", "Response to evaluate:\n{{response}}")
		setIfMissingTest(cfg, "temperature", 0.3)
		setIfMissingTest(cfg, "max_tokens", -1)
		setIfMissingTest(cfg, "extraction_strategy", "regex")
		setIfMissingTest(cfg, "extraction_pattern", DefaultExtractionPattern)
	case AggMethodSynthesis:
		setIfMissingTest(cfg, "model", "mock-model")
		setIfMissingTest(cfg, "system_prompt", "You are a synthesis agent that combines candidate responses into a single best answer.")
		setIfMissingTest(cfg, "prompt", "Original Question: {{prompt}}\n\nResponses:\n{{responses}}")
		setIfMissingTest(cfg, "temperature", 0.7)
		setIfMissingTest(cfg, "max_tokens", -1)
	case AggMethodPeerMatrix:
		setIfMissingTest(cfg, "eval_system_prompt", "Evaluate the candidate answer.")
		setIfMissingTest(cfg, "eval_prompt", "Question: {{question}}\nResponse to evaluate:\n{{response}}\nReviewer response:\n{{reviewer_answer}}")
		setIfMissingTest(cfg, "temperature", 0.3)
		setIfMissingTest(cfg, "max_tokens", -1)
		setIfMissingTest(cfg, "normalization", "none")
		setIfMissingTest(cfg, "max_parallel", 2)
		if _, ok := cfg["rubric"]; !ok {
			cfg["rubric"] = []interface{}{
				map[string]interface{}{"name": "accuracy", "weight": 1.0, "description": "Correctness"},
			}
		}
		setIfMissingTest(cfg, "extraction_strategy", "regex")
		setIfMissingTest(cfg, "extraction_pattern", DefaultExtractionPattern)
	case AggMethodMajorityVote:
		setIfMissingTest(cfg, "extraction_strategy", "regex")
		setIfMissingTest(cfg, "extraction_pattern", DefaultExtractionPattern)
		setIfMissingTest(cfg, "tie_breaker_method", "first")
		// Required when tie_breaker_method=synthesis.
		setIfMissingTest(cfg, "system_prompt", "You are a synthesis agent that combines candidate responses into a single best answer.")
		setIfMissingTest(cfg, "prompt", "Original Question: {{prompt}}\n\nResponses:\n{{responses}}")
		setIfMissingTest(cfg, "max_tokens", -1)
	case AggMethodDebateDecide:
		setIfMissingTest(cfg, "judge_model", "mock-model")
		setIfMissingTest(cfg, "system_prompt", "You are an impartial judge selecting between answer camps.")
		setIfMissingTest(cfg, "prompt", "Original Question: {{prompt}}\n\nCamp summaries:\n{{camps}}\n\nReturn WINNER: [label].")
		setIfMissingTest(cfg, "temperature", 0.3)
		setIfMissingTest(cfg, "max_tokens", -1)
		setIfMissingTest(cfg, "repair_max_tokens", 256)
		setIfMissingTest(cfg, "extraction_strategy", "regex")
		setIfMissingTest(cfg, "extraction_pattern", DefaultExtractionPattern)
	}
}

func withJudgeConfig(overrides map[string]interface{}) map[string]interface{} {
	cfg := map[string]interface{}{}
	ensureAggregationConfigForMethod(AggMethodJudge, cfg)
	mergeTestConfig(cfg, overrides)
	return cfg
}

func withScoringConfig(overrides map[string]interface{}) map[string]interface{} {
	cfg := map[string]interface{}{}
	ensureAggregationConfigForMethod(AggMethodScoring, cfg)
	mergeTestConfig(cfg, overrides)
	return cfg
}

func withSynthesisConfig(overrides map[string]interface{}) map[string]interface{} {
	cfg := map[string]interface{}{}
	ensureAggregationConfigForMethod(AggMethodSynthesis, cfg)
	mergeTestConfig(cfg, overrides)
	return cfg
}

func withPeerMatrixConfig(overrides map[string]interface{}) map[string]interface{} {
	cfg := map[string]interface{}{}
	ensureAggregationConfigForMethod(AggMethodPeerMatrix, cfg)
	mergeTestConfig(cfg, overrides)
	return cfg
}

func withMajorityVoteConfig(overrides map[string]interface{}) map[string]interface{} {
	cfg := map[string]interface{}{}
	ensureAggregationConfigForMethod(AggMethodMajorityVote, cfg)
	mergeTestConfig(cfg, overrides)
	return cfg
}

func withDebateDecideConfig(overrides map[string]interface{}) map[string]interface{} {
	cfg := map[string]interface{}{}
	ensureAggregationConfigForMethod(AggMethodDebateDecide, cfg)
	mergeTestConfig(cfg, overrides)
	return cfg
}

func mergeTestConfig(dst map[string]interface{}, overrides map[string]interface{}) {
	for k, v := range overrides {
		dst[k] = deepCloneJSONValue(v)
	}
}

func setIfMissingTest(cfg map[string]interface{}, key string, value interface{}) {
	if cfg == nil {
		return
	}
	if existing, ok := cfg[key]; ok && !isTestEmpty(existing) {
		return
	}
	cfg[key] = deepCloneJSONValue(value)
}

func isTestEmpty(v interface{}) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	default:
		return false
	}
}

func deepCloneJSONValue(v interface{}) interface{} {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		return v
	}
	return out
}
