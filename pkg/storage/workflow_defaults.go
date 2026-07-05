package storage

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	legacyAggMethodCollect     = "collect"
	legacyAggMethodJudge       = "judge"
	legacyAggMethodSynthesis   = "synthesis"
	legacyAggMethodScoring     = "scoring"
	legacyAggMethodPeerMatrix  = "peer_matrix"
	legacyAggMethodDebate      = "debate_decide"
	legacyAggMethodMajority    = "majority_vote"
	defaultAggregationModel    = "deepseek/deepseek-v4-flash"
	legacyExtractionPatternMCQ = `(?i)(?:final answer|answer)\s*(?:is)?\s*[:\-\s]\s*\(?([A-Z])\)?\b`
)

var (
	legacyRetryDefault = map[string]interface{}{
		"max_attempts":     5,
		"backoff_ms":       1000,
		"backoff_multiply": 2.0,
		"max_backoff_ms":   30000,
		"retryable_errors": []interface{}{
			"RATE_LIMIT",
			"RATE_LIMIT_WAIT_DEADLINE",
			"TIMEOUT",
			"TEMPORARY",
			"UNAVAILABLE",
			"CONNECTION",
			"5xx",
			"PROVIDER_NO_CHOICES",
			"OUTPUT_TRUNCATED_EMPTY",
			"CHILD_WORKFLOW_TRANSIENT",
			"AGG_PARSE_FAILURE",
		},
	}
	legacyRetryNone = map[string]interface{}{
		"max_attempts": 1,
	}
	legacyPeerRubric = []interface{}{
		map[string]interface{}{"name": "logical_soundness", "description": "Reasoning is coherent and internally consistent", "weight": 0.35},
		map[string]interface{}{"name": "evidence_analysis", "description": "Uses relevant details and avoids unsupported claims", "weight": 0.30},
		map[string]interface{}{"name": "completeness", "description": "Addresses key parts of the question", "weight": 0.20},
		map[string]interface{}{"name": "clarity", "description": "Answer is understandable and concise", "weight": 0.15},
	}
)

// ApplyLegacyWorkflowDefaults materializes formerly-implicit runtime defaults into workflow definition JSON.
// This is used during startup migration so existing DB workflows stay executable under strict validation.
func ApplyLegacyWorkflowDefaults(definition string) (string, bool, error) {
	var wf map[string]interface{}
	if err := json.Unmarshal([]byte(definition), &wf); err != nil {
		return "", false, fmt.Errorf("parse workflow definition JSON: %w", err)
	}
	nodesRaw, ok := wf["nodes"]
	if !ok {
		return definition, false, nil
	}
	nodes, ok := nodesRaw.([]interface{})
	if !ok {
		return "", false, fmt.Errorf("workflow nodes is not an array")
	}

	changed := false
	for _, rawNode := range nodes {
		nodeMap, ok := rawNode.(map[string]interface{})
		if !ok {
			continue
		}
		data := ensureMap(nodeMap, "data", &changed)
		config := ensureMap(data, "config", &changed)

		nodeType := asStringValue(data["type"])
		if nodeType == "" {
			nodeType = asStringValue(nodeMap["type"])
		}

		// Retry policy used to be inferred by node type.
		if _, hasRetryPolicy := config["retryPolicy"]; !hasRetryPolicy {
			if _, hasSnake := config["retry_policy"]; !hasSnake {
				if nodeType == "agent" || nodeType == "prompt" || nodeType == "contract_extract" || nodeType == "child_workflow" {
					config["retryPolicy"] = cloneJSONMap(legacyRetryDefault)
					changed = true
				} else {
					config["retryPolicy"] = cloneJSONMap(legacyRetryNone)
					changed = true
				}
			}
		}

		switch nodeType {
		case "agent", "prompt", "contract_extract":
			if setIfMissing(config, "temperature", 0.7) {
				changed = true
			}
			if setIfMissing(config, "maxTokens", 1000) {
				changed = true
			}
			if setIfMissing(config, "timeoutSeconds", 120) {
				changed = true
			}
		case "child_workflow":
			if setIfMissing(config, "timeoutSeconds", 120) {
				changed = true
			}
		case "result":
			method := asStringValue(config["aggregationMethod"])
			if strings.TrimSpace(method) == "" {
				config["aggregationMethod"] = legacyAggMethodCollect
				method = legacyAggMethodCollect
				changed = true
			}
			agg := ensureMap(config, "aggregationConfig", &changed)
			if backfillAggregationConfig(method, agg) {
				changed = true
			}
			if setIfMissing(config, "timeoutSeconds", 120) {
				changed = true
			}
		}
	}

	if !changed {
		return definition, false, nil
	}
	updated, err := json.Marshal(wf)
	if err != nil {
		return "", false, fmt.Errorf("marshal updated workflow definition JSON: %w", err)
	}
	return string(updated), true, nil
}

func backfillAggregationConfig(method string, config map[string]interface{}) bool {
	changed := false
	method = strings.TrimSpace(method)
	if method == "" {
		method = legacyAggMethodCollect
	}

	switch method {
	case legacyAggMethodJudge:
		changed = setIfMissing(config, "judge_model", defaultAggregationModel) || changed
		changed = setIfMissing(config, "temperature", 0.3) || changed
		changed = setIfMissing(config, "system_prompt", "You are an impartial judge evaluating AI responses.") || changed
		changed = setIfMissing(config, "prompt", "Original Question: {{prompt}}\\n\\nResponses:\\n{{responses}}\\n\\nReturn WINNER: [agent_id].") || changed
		changed = setIfMissing(config, "max_tokens", 1024) || changed
		changed = setIfMissing(config, "repair_max_tokens", 256) || changed
		changed = ensureExtractionDefaults(config) || changed
	case legacyAggMethodSynthesis:
		changed = setIfMissing(config, "model", defaultAggregationModel) || changed
		changed = setIfMissing(config, "temperature", 0.7) || changed
		changed = setIfMissing(config, "system_prompt", "You synthesize the strongest final answer from multiple responses.") || changed
		changed = setIfMissing(config, "prompt", "Original Question: {{prompt}}\\n\\nResponses:\\n{{responses}}\\n\\nProduce a single best answer.") || changed
		changed = setIfMissing(config, "max_tokens", 1024) || changed
	case legacyAggMethodScoring:
		changed = setIfMissing(config, "scoring_model", defaultAggregationModel) || changed
		changed = setIfMissing(config, "temperature", 0.3) || changed
		changed = setIfMissing(config, "system_prompt", "You score each response across rubric criteria.") || changed
		changed = setIfMissing(config, "prompt", "Original Question: {{prompt}}\\n\\nResponses:\\n{{responses}}\\n\\nReturn scores as JSON.") || changed
		changed = setIfMissing(config, "max_tokens", 1024) || changed
	case legacyAggMethodPeerMatrix:
		changed = setIfMissing(config, "temperature", 0.3) || changed
		changed = setIfMissing(config, "eval_system_prompt", "You are a critical evaluator scoring peer responses.") || changed
		changed = setIfMissing(config, "eval_prompt", "Question: {{question}}\\nCandidate: {{candidate_response}}\\nReviewer response: {{reviewer_response}}\\nReturn criterion scores as JSON.") || changed
		changed = setIfMissing(config, "rubric", legacyPeerRubric) || changed
		changed = setIfMissing(config, "max_parallel", 6) || changed
		changed = setIfMissing(config, "normalization", "none") || changed
		changed = setIfMissing(config, "max_tokens", 1024) || changed
	case legacyAggMethodDebate:
		changed = setIfMissing(config, "judge_model", defaultAggregationModel) || changed
		changed = setIfMissing(config, "temperature", 0.3) || changed
		changed = setIfMissing(config, "system_prompt", "You are an impartial judge selecting the strongest camp argument.") || changed
		changed = setIfMissing(config, "prompt", "Original Question: {{prompt}}\\n\\nCamp summaries:\\n{{camps}}\\n\\nReturn WINNER: [camp_id].") || changed
		changed = setIfMissing(config, "max_tokens", 1024) || changed
		changed = setIfMissing(config, "repair_max_tokens", 256) || changed
		changed = ensureExtractionDefaults(config) || changed
	case legacyAggMethodMajority:
		changed = ensureExtractionDefaults(config) || changed
	}

	return changed
}

func ensureExtractionDefaults(config map[string]interface{}) bool {
	return setIfMissing(config, "extraction_strategy", "regex") ||
		setIfMissing(config, "extraction_pattern", legacyExtractionPatternMCQ)
}

func cloneJSONMap(source map[string]interface{}) map[string]interface{} {
	if source == nil {
		return nil
	}
	data, err := json.Marshal(source)
	if err != nil {
		return map[string]interface{}{"max_attempts": 1}
	}
	decoded := make(map[string]interface{})
	if err := json.Unmarshal(data, &decoded); err != nil {
		return map[string]interface{}{"max_attempts": 1}
	}
	return decoded
}

func ensureMap(parent map[string]interface{}, key string, changed *bool) map[string]interface{} {
	if raw, ok := parent[key]; ok {
		if existing, ok := raw.(map[string]interface{}); ok {
			return existing
		}
	}
	created := make(map[string]interface{})
	parent[key] = created
	if changed != nil {
		*changed = true
	}
	return created
}

func setIfMissing(target map[string]interface{}, key string, value interface{}) bool {
	if target == nil {
		return false
	}
	if existing, ok := target[key]; ok {
		if !isEmptyValue(existing) {
			return false
		}
	}
	target[key] = value
	return true
}

func isEmptyValue(value interface{}) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(v) == ""
	default:
		return false
	}
}

func asStringValue(value interface{}) string {
	s, _ := value.(string)
	return s
}
