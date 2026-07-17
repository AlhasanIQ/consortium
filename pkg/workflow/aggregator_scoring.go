package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/pkg/providers"
)

// ScoringAggregator uses rubric-based scoring to evaluate and select responses
type ScoringAggregator struct{}

// Name returns the aggregation method name
func (s *ScoringAggregator) Name() AggregationMethod {
	return AggMethodScoring
}

// RubricCriterion defines a single scoring criterion
type RubricCriterion struct {
	Name        string  `json:"name"`
	Weight      float64 `json:"weight"`
	Description string  `json:"description"`
}

// DefaultRubric provides a default scoring rubric.
// Evaluators don't have an answer key — criteria focus on what scorers can assess:
// reasoning quality, evidence handling, and completeness.
var DefaultRubric = []RubricCriterion{
	{Name: "Logical Soundness", Weight: 0.4, Description: "Is the reasoning internally consistent, free of logical fallacies, and does the conclusion follow from the evidence presented?"},
	{Name: "Evidence Analysis", Weight: 0.3, Description: "Are relevant facts, data, or options properly identified and analyzed? For multiple-choice: are incorrect alternatives systematically eliminated with valid justification?"},
	{Name: "Completeness", Weight: 0.2, Description: "Does the response address all aspects of the question? Are edge cases, caveats, or alternative interpretations considered?"},
	{Name: "Clarity", Weight: 0.1, Description: "Is the response well-structured and easy to follow?"},
}

// DefaultScoringSystemPrompt is the default system prompt for the scorer persona
const DefaultScoringSystemPrompt = `You are an expert evaluator scoring AI responses. For each criterion, provide a score (1-10) and brief justification.

End your response with a JSON object containing the scores. Use the exact criterion keys shown in the rubric:
{"scores": {"criterion_key": score_number, ...}}`

// DefaultScoringPrompt is the default user prompt template for scoring (contains the data)
const DefaultScoringPrompt = `Original Question: {{prompt}}

Response to evaluate:
{{response}}

Scoring Rubric:
{{rubric}}

Score this response on each criterion.`

// Aggregate scores all responses and selects the highest-scoring one
func (s *ScoringAggregator) Aggregate(ctx context.Context, inputs []AgentOutput, config map[string]interface{}, llmClient *providers.Client, aggCtx *AggregationContext) (*AggregationResult, error) {
	if single, err := checkSingleInput(inputs, AggMethodScoring); err != nil {
		return nil, err
	} else if single != nil {
		return single.Result, nil
	}

	// Optional fast path: if all agents produced the same extractable answer,
	// skip scoring LLM calls entirely.
	if decision, ok := maybeUnanimousAnswerDecision(inputs, config); ok {
		scores := make(map[string]float64, len(inputs))
		for _, input := range inputs {
			scores[input.AgentID] = 1.0
		}
		return &AggregationResult{
			Output:          decision.WinningOutput,
			Method:          AggMethodScoring,
			Winner:          decision.WinnerAgentID,
			Scores:          scores,
			Reasoning:       fmt.Sprintf("Unanimous extracted answer %q — short-circuited scoring aggregation", decision.Answer),
			AgreementRatio:  1.0,
			ConsensusAnswer: decision.Answer,
		}, nil
	}

	// Parse rubric from config
	rubric := parseRubric(config)

	// Required configuration.
	model, _ := config["scoring_model"].(string)
	if model == "" {
		return nil, fmt.Errorf("scoring aggregation requires aggregationConfig.scoring_model: %w", ErrAggregationConfig)
	}

	// Dynamic rubric: generate task-specific criteria if configured
	var rubricTokens int
	if rubricMode, _ := config["rubric_mode"].(string); strings.EqualFold(strings.TrimSpace(rubricMode), "dynamic") && llmClient != nil {
		originalPrompt, _ := config["original_prompt"].(string)
		if dynamicRubric, tokens, err := GenerateDynamicRubric(ctx, originalPrompt, model, llmClient, aggCtx, rubric); err == nil {
			rubric = dynamicRubric
			rubricTokens = tokens
		}
	}

	systemPrompt, _ := config["system_prompt"].(string)
	if systemPrompt == "" {
		return nil, fmt.Errorf("scoring aggregation requires aggregationConfig.system_prompt: %w", ErrAggregationConfig)
	}

	promptTemplate, _ := config["prompt"].(string)
	if promptTemplate == "" {
		return nil, fmt.Errorf("scoring aggregation requires aggregationConfig.prompt: %w", ErrAggregationConfig)
	}

	// Get original prompt from config if available
	originalPrompt, _ := config["original_prompt"].(string)

	temperature, ok := aggregationTemperature(config)
	if !ok {
		return nil, fmt.Errorf("scoring aggregation requires numeric aggregationConfig.temperature: %w", ErrAggregationConfig)
	}
	maxTokens, ok := aggregationMaxTokens(config)
	if !ok {
		return nil, fmt.Errorf("scoring aggregation requires aggregationConfig.max_tokens: %w", ErrAggregationConfig)
	}

	// Build rubric section
	var rubricBuilder strings.Builder
	for _, criterion := range rubric {
		fmt.Fprintf(&rubricBuilder, "- %s (%.0f%%): %s\n", normalizeRubricKey(criterion.Name), criterion.Weight*100, criterion.Description)
	}
	rubricText := rubricBuilder.String()

	if llmClient == nil {
		return nil, fmt.Errorf("LLM client is required for scoring aggregation")
	}

	// Score each response
	scores := make(map[string]float64)
	totalTokens := rubricTokens
	var reasoningBuilder strings.Builder
	var parseFailures int
	subcallRetryCfg := parseScoringSubcallRetryConfig(config)

	for _, input := range inputs {
		// Build the scoring prompt for this response
		userPrompt := promptTemplate
		userPrompt = strings.ReplaceAll(userPrompt, "{{prompt}}", originalPrompt)
		userPrompt = strings.ReplaceAll(userPrompt, "{{response}}", input.Output)
		userPrompt = strings.ReplaceAll(userPrompt, "{{rubric}}", rubricText)

		callParams := aggLLMCall{
			Scope:        "scoring",
			Model:        model,
			SystemPrompt: systemPrompt,
			UserPrompt:   userPrompt,
			MaxTokens:    maxTokens,
			Temperature:  temperature,
			Config:       config,
			NodeLabel:    "Scorer",
			NodeName:     fmt.Sprintf("Score %s", input.AgentID),
			SubNodeID:    fmt.Sprintf("score__%s", input.AgentID),
		}

		var (
			resp      *providers.CompletionResponse
			err       error
			attemptNo int
		)
		for attempt := 1; attempt <= subcallRetryCfg.MaxAttempts; attempt++ {
			attemptNo = attempt
			callParams.Attempt = attempt

			resp, err = callAggregatorLLM(ctx, callParams, llmClient, aggCtx)
			if err == nil {
				totalTokens += responseTotalTokens(resp)
				break
			}
			if attempt >= subcallRetryCfg.MaxAttempts || !shouldRetryScoringSubcallError(err) {
				break
			}
			backoff := subcallRetryCfg.backoffForAttempt(attempt)
			if !sleepWithContextOrCancel(ctx, backoff) {
				err = ctx.Err()
				break
			}
		}
		if err != nil {
			return nil, &scoringSubcallError{
				AgentID:     input.AgentID,
				Attempts:    attemptNo,
				MaxAttempts: subcallRetryCfg.MaxAttempts,
				Err:         err,
			}
		}

		// Parse scores from response
		criterionScores := parseScores(resp.Content)
		weightedScore, matched := calculateWeightedScore(criterionScores, rubric)
		if !matched {
			parseFailures++
		}
		scores[input.AgentID] = weightedScore

		if attemptNo > 1 {
			fmt.Fprintf(&reasoningBuilder, "(%s scoring call retries: %d)\n", input.AgentID, attemptNo-1)
		}
		fmt.Fprintf(&reasoningBuilder, "### %s (Score: %.2f)\n%s\n\n", input.AgentID, weightedScore, resp.Content)
	}

	// If ALL scores failed to parse, return an error rather than picking a fake winner
	if parseFailures == len(inputs) {
		return nil, fmt.Errorf("scoring aggregation failed: all %d responses failed to parse scores", len(inputs))
	}

	// Note parse failures in reasoning if some (but not all) failed
	if parseFailures > 0 {
		fmt.Fprintf(&reasoningBuilder, "**Warning:** %d of %d score evaluations failed to parse (scored 0.0)\n\n", parseFailures, len(inputs))
	}

	// Find the winner (highest score, with alphabetical tie-breaking for determinism)
	var winner string
	var highestScore float64 = -1
	var winningOutput string

	for _, input := range inputs {
		score := scores[input.AgentID]
		if score > highestScore || (score == highestScore && (winner == "" || input.AgentID < winner)) {
			highestScore = score
			winner = input.AgentID
			winningOutput = input.Output
		}
	}

	// Best-effort agreement metadata — extraction config is intentionally optional here.
	// Scoring uses rubric-scored rankings, not extracted answers. Extraction only feeds
	// diagnostic ComputeAgreement metrics; missing config degrades to no-op extractor.
	extractCfg := ParseExtractorConfig(config)
	agreeRatio, consensus, dissenting := ComputeAgreement(inputs, extractCfg)

	return &AggregationResult{
		Output:          winningOutput,
		Method:          AggMethodScoring,
		Winner:          winner,
		Scores:          scores,
		TokensUsed:      totalTokens,
		Reasoning:       reasoningBuilder.String(),
		AgreementRatio:  agreeRatio,
		ConsensusAnswer: consensus,
		DissentingIDs:   dissenting,
	}, nil
}

// parseRubric extracts the rubric from config or returns default
func parseRubric(config map[string]interface{}) []RubricCriterion {
	rubricData, ok := config["rubric"]
	if !ok {
		return DefaultRubric
	}

	if rubric := parseRubricCriteria(rubricData); len(rubric) > 0 {
		return rubric
	}

	return DefaultRubric
}

func parseRubricCriteria(rubricData interface{}) []RubricCriterion {
	switch r := rubricData.(type) {
	case []RubricCriterion:
		return sanitizeRubricCriteria(r)
	case []interface{}:
		return parseRubricCriteriaItems(r)
	case map[string]interface{}:
		if nested, ok := r["rubric"]; ok {
			return parseRubricCriteria(nested)
		}
		return parseRubricCriteriaItems([]interface{}{r})
	case string:
		trimmed := strings.TrimSpace(r)
		if trimmed == "" {
			return nil
		}
		var wrapped struct {
			Rubric []RubricCriterion `json:"rubric"`
		}
		if err := json.Unmarshal([]byte(trimmed), &wrapped); err == nil && len(wrapped.Rubric) > 0 {
			return sanitizeRubricCriteria(wrapped.Rubric)
		}
		var rubric []RubricCriterion
		if err := json.Unmarshal([]byte(trimmed), &rubric); err == nil {
			return sanitizeRubricCriteria(rubric)
		}
	}
	return nil
}

func sanitizeRubricCriteria(items []RubricCriterion) []RubricCriterion {
	rubric := make([]RubricCriterion, 0, len(items))
	for _, item := range items {
		item.Name = strings.TrimSpace(item.Name)
		item.Description = strings.TrimSpace(item.Description)
		if item.Name == "" {
			continue
		}
		rubric = append(rubric, item)
	}
	return rubric
}

func parseRubricCriteriaItems(items []interface{}) []RubricCriterion {
	var rubric []RubricCriterion
	for _, item := range items {
		if m, ok := item.(map[string]interface{}); ok {
			criterion := RubricCriterion{}
			if name, ok := m["name"].(string); ok {
				criterion.Name = name
			}
			if weight, ok := m["weight"].(float64); ok {
				criterion.Weight = weight
			}
			if desc, ok := m["description"].(string); ok {
				criterion.Description = desc
			}
			criterion.Name = strings.TrimSpace(criterion.Name)
			criterion.Description = strings.TrimSpace(criterion.Description)
			if criterion.Name != "" {
				rubric = append(rubric, criterion)
			}
		}
	}
	return rubric
}

type scoringSubcallRetryConfig struct {
	MaxAttempts     int
	BackoffMs       int
	BackoffMultiply float64
	MaxBackoffMs    int
}

type scoringSubcallError struct {
	AgentID     string
	Attempts    int
	MaxAttempts int
	Err         error
}

func (e *scoringSubcallError) Error() string {
	return fmt.Sprintf("scoring LLM call failed for %s after %d/%d attempts: %v", e.AgentID, e.Attempts, e.MaxAttempts, e.Err)
}

func (e *scoringSubcallError) Unwrap() error { return e.Err }

func (e *scoringSubcallError) metadata() map[string]interface{} {
	return map[string]interface{}{
		"scoring_subcall_agent":        e.AgentID,
		"scoring_subcall_attempts":     e.Attempts,
		"scoring_subcall_max_attempts": e.MaxAttempts,
	}
}

func asScoringSubcallError(err error) (*scoringSubcallError, bool) {
	if err == nil {
		return nil, false
	}
	var target *scoringSubcallError
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

func parseScoringSubcallRetryConfig(config map[string]interface{}) scoringSubcallRetryConfig {
	cfg := scoringSubcallRetryConfig{
		MaxAttempts:     3,
		BackoffMs:       250,
		BackoffMultiply: 2.0,
		MaxBackoffMs:    2000,
	}
	if config == nil {
		return cfg
	}
	if raw, ok := config["subcall_retry_max_attempts"]; ok {
		if n, ok := numericToInt(raw); ok && n > 0 {
			cfg.MaxAttempts = n
		}
	}
	if raw, ok := config["subcall_retry_backoff_ms"]; ok {
		if n, ok := numericToInt(raw); ok && n >= 0 {
			cfg.BackoffMs = n
		}
	}
	if raw, ok := numericToFloat64(config["subcall_retry_backoff_multiply"]); ok && raw > 0 {
		cfg.BackoffMultiply = raw
	}
	if raw, ok := config["subcall_retry_max_backoff_ms"]; ok {
		if n, ok := numericToInt(raw); ok && n > 0 {
			cfg.MaxBackoffMs = n
		}
	}
	if cfg.MaxAttempts < 1 {
		cfg.MaxAttempts = 1
	}
	if cfg.BackoffMs < 0 {
		cfg.BackoffMs = 0
	}
	if cfg.BackoffMultiply <= 0 {
		cfg.BackoffMultiply = 2.0
	}
	return cfg
}

func (c scoringSubcallRetryConfig) backoffForAttempt(attempt int) time.Duration {
	if c.BackoffMs <= 0 || attempt < 1 {
		return 0
	}
	backoff := float64(c.BackoffMs)
	for i := 1; i < attempt; i++ {
		backoff *= c.BackoffMultiply
		if c.MaxBackoffMs > 0 && backoff >= float64(c.MaxBackoffMs) {
			backoff = float64(c.MaxBackoffMs)
			break
		}
	}
	if c.MaxBackoffMs > 0 && backoff > float64(c.MaxBackoffMs) {
		backoff = float64(c.MaxBackoffMs)
	}
	return time.Duration(backoff) * time.Millisecond
}

func shouldRetryScoringSubcallError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var retryErr *RetryableError
	if errors.As(err, &retryErr) {
		return retryErr.Retryable
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	if pe, ok := providers.AsProviderError(err); ok {
		return pe.Retryable
	}
	lower := strings.ToLower(strings.TrimSpace(err.Error()))
	for _, marker := range []string{
		"connection reset",
		"broken pipe",
		"unexpected eof",
		"failed to read response",
		"temporarily unavailable",
		"timeout",
		"timed out",
		"upstream timeout",
		"upstream unavailable",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func sleepWithContextOrCancel(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// parseScores extracts criterion scores from the LLM response
func parseScores(response string) map[string]float64 {
	scores := make(map[string]float64)

	// Try to find JSON object in the response
	jsonRegex := regexp.MustCompile(`\{[^{}]*"scores"[^{}]*\{[^{}]*\}[^{}]*\}`)
	match := jsonRegex.FindString(response)
	if match != "" {
		var result struct {
			Scores map[string]float64 `json:"scores"`
		}
		if err := json.Unmarshal([]byte(match), &result); err == nil {
			return result.Scores
		}
	}

	// Fallback: look for patterns like "Accuracy: 8", "Logical Soundness: 8/10", "evidence_analysis: 7"
	lines := strings.Split(response, "\n")
	scoreRegex := regexp.MustCompile(`(?i)([\w][\w\s&/-]*[\w]):\s*(\d+(?:\.\d+)?)\s*(?:/10)?`)
	for _, line := range lines {
		matches := scoreRegex.FindStringSubmatch(line)
		if len(matches) >= 3 {
			criterion := normalizeRubricKey(matches[1])
			score, err := strconv.ParseFloat(matches[2], 64)
			if err == nil && score >= 1 && score <= 10 {
				scores[criterion] = score
			}
		}
	}

	return scores
}

// nonAlphanumRe matches runs of non-alphanumeric characters for key normalization.
var nonAlphanumRe = regexp.MustCompile(`[^a-z0-9]+`)

// normalizeRubricKey converts a criterion name to snake_case for matching.
// Handles spaces, hyphens, punctuation, and mixed case:
// "Logical Soundness" → "logical_soundness", "Evidence & Analysis" → "evidence_analysis".
func normalizeRubricKey(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = nonAlphanumRe.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	return s
}

// calculateWeightedScore computes the weighted average score.
// Both criterionScores keys and rubric names are normalized before matching.
// Returns (0.0, false) when no rubric criteria match — callers must handle the failure.
func calculateWeightedScore(criterionScores map[string]float64, rubric []RubricCriterion) (float64, bool) {
	// Normalize criterionScores keys (from LLM response)
	normalizedScores := make(map[string]float64, len(criterionScores))
	for key, score := range criterionScores {
		normalizedScores[normalizeRubricKey(key)] = score
	}

	// Match against normalized rubric keys
	var totalWeight float64
	var weightedSum float64

	for _, criterion := range rubric {
		normKey := normalizeRubricKey(criterion.Name)
		if score, ok := normalizedScores[normKey]; ok {
			weightedSum += score * criterion.Weight
			totalWeight += criterion.Weight
		}
	}

	if totalWeight == 0 {
		return 0.0, false // No criteria matched — strict failure
	}

	return weightedSum / totalWeight, true
}

// RegisterScoringAggregator registers the scoring aggregator with the registry
func RegisterScoringAggregator(registry *AggregatorRegistry) {
	registry.Register(&ScoringAggregator{})
}
