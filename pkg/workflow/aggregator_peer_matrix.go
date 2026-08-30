package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/alhasaniq/consortium/pkg/providers"
)

// PeerMatrixAggregator has each agent score every other agent's response.
// This creates an N×(N-1) evaluation matrix where each agent evaluates all others.
type PeerMatrixAggregator struct{}

// Name returns the aggregation method name
func (p *PeerMatrixAggregator) Name() AggregationMethod {
	return AggMethodPeerMatrix
}

// DefaultPeerEvalSystemPrompt is the default system prompt for peer evaluation
const DefaultPeerEvalSystemPrompt = `You are a critical evaluator scoring AI responses. Be discriminating and look for meaningful differences. A score of 10 is reserved for exceptional responses; most good responses score 6-8.

Score on a scale of 1-10:
- 9-10: Exceptional - comprehensive, insightful, no flaws
- 7-8: Good - accurate and complete with minor issues
- 5-6: Adequate - correct but lacking depth or clarity
- 3-4: Poor - significant gaps or errors
- 1-2: Very poor - mostly incorrect or irrelevant

IMPORTANT: Be critical. Find weaknesses. Differentiate between responses.

You MUST respond with valid JSON where each criterion key maps to a reasoning and score:
{"criterion_key": {"reasoning": "<1-2 sentences>", "score": <1-10>}, ...}

Use the exact criterion keys shown in the rubric.`

// DefaultPeerEvalPrompt is the default user prompt for peer evaluation (contains the data).
// Truly blind: the reviewer does not see their own answer, only the candidate's response.
// This eliminates anchoring to own reasoning and is consistent with blind evaluation (Principle #1).
const DefaultPeerEvalPrompt = `Question: {{question}}

Response to evaluate:
{{response}}

{{rubric}}Score this response on each criterion (1-10).`

// DefaultPeerRubric provides a default scoring rubric for peer evaluation.
// Evaluators don't have an answer key — criteria focus on what reviewers can assess:
// reasoning quality, evidence handling, and completeness.
var DefaultPeerRubric = []RubricCriterion{
	{Name: "Logical Soundness", Weight: 0.4, Description: "Is the reasoning internally consistent, free of logical fallacies, and does the conclusion follow from the evidence presented?"},
	{Name: "Evidence Analysis", Weight: 0.3, Description: "Are relevant facts, data, or options properly identified and analyzed? For multiple-choice: are incorrect alternatives systematically eliminated with valid justification?"},
	{Name: "Completeness", Weight: 0.2, Description: "Does the response address all aspects of the question? Are edge cases, caveats, or alternative interpretations considered?"},
	{Name: "Clarity", Weight: 0.1, Description: "Is the response well-structured and easy to follow?"},
}

// evalTask represents a single evaluation task
type evalTask struct {
	ReviewerID     string
	ReviewerName   string
	ReviewerModel  string // Model for this reviewer (for per-agent routing)
	CandidateID    string
	Response       string
	ReviewerAnswer string // The reviewer's own response (available via {{reviewer_answer}} in custom eval_prompt; not used by default)
}

// evalResult represents the result of a single evaluation
type evalResult struct {
	ReviewerID      string
	CandidateID     string
	Score           float64
	CriterionScores map[string]float64 // Per-criterion scores (normalized keys), nil for single-score fallback
	Reasoning       string
	Valid           bool
	TokensUsed      int
}

// Aggregate performs peer matrix evaluation
func (p *PeerMatrixAggregator) Aggregate(ctx context.Context, inputs []AgentOutput, config map[string]interface{}, llmClient *providers.Client, aggCtx *AggregationContext) (*AggregationResult, error) {
	if single, err := checkSingleInput(inputs, AggMethodPeerMatrix); err != nil {
		return nil, err
	} else if single != nil {
		// Peer matrix scores are on a 1-10 scale, not 0-1.
		single.Result.Scores[inputs[0].AgentID] = 10.0
		return single.Result, nil
	}

	// Optional fast path: if all agents produced the same extractable answer,
	// skip peer-evaluation LLM calls entirely.
	if decision, ok := maybeUnanimousAnswerDecision(inputs, config); ok {
		scores := make(map[string]float64, len(inputs))
		for _, input := range inputs {
			scores[input.AgentID] = 10.0
		}
		return &AggregationResult{
			Output:          decision.WinningOutput,
			Method:          AggMethodPeerMatrix,
			Winner:          decision.WinnerAgentID,
			Scores:          scores,
			Reasoning:       fmt.Sprintf("Unanimous extracted answer %q — short-circuited peer_matrix aggregation", decision.Answer),
			AgreementRatio:  1.0,
			ConsensusAnswer: decision.Answer,
		}, nil
	}

	if llmClient == nil {
		return nil, fmt.Errorf("LLM client is required for peer_matrix aggregation")
	}
	if model, _ := config["judge_model"].(string); strings.TrimSpace(model) != "" {
		return nil, fmt.Errorf("peer_matrix does not support judge_model; each reviewer must have its own model")
	}

	// Parse required configuration.
	cfg, err := parsePeerMatrixConfig(config)
	if err != nil {
		return nil, err
	}
	certifiedEarlyStop, err := peerMatrixCertifiedEarlyStop(config)
	if err != nil {
		return nil, err
	}

	// Get original prompt from config if available
	originalPrompt, _ := config["original_prompt"].(string)

	// Dynamic rubric: generate task-specific criteria once before N*(N-1) eval loop.
	// Requires rubric_model when rubric_mode is dynamic.
	var rubricTokens int
	if rubricMode, _ := config["rubric_mode"].(string); strings.EqualFold(strings.TrimSpace(rubricMode), "dynamic") {
		if strings.TrimSpace(cfg.RubricModel) == "" {
			return nil, fmt.Errorf("peer_matrix aggregation requires rubric_model when rubric_mode is dynamic: %w", ErrAggregationConfig)
		}
		if dynamicRubric, tokens, err := GenerateDynamicRubric(ctx, originalPrompt, cfg.RubricModel, llmClient, aggCtx, cfg.Rubric); err == nil {
			cfg.Rubric = dynamicRubric
			rubricTokens = tokens
		}
	}
	if certifiedEarlyStop {
		if err := validateCertifiedPeerMatrixConfig(cfg); err != nil {
			return nil, err
		}
	}

	// Validate all reviewer models before starting either exhaustive or progressive evaluation.
	tasks := buildEvalTasks(inputs)
	if err := ensurePeerReviewerModels(tasks); err != nil {
		return nil, err
	}

	var results []evalResult
	var certificate *PeerMatrixCertificate
	if certifiedEarlyStop {
		results, certificate, err = p.executeCertifiedEvaluations(ctx, inputs, cfg, originalPrompt, llmClient, aggCtx)
		if err != nil {
			return nil, err
		}
	} else {
		// Existing exhaustive behavior remains the default.
		results = p.executeEvaluations(ctx, tasks, cfg, originalPrompt, llmClient, aggCtx)
	}

	// Build evaluation matrix from the reviews that actually ran. In certified
	// mode these are observed averages; the certificate proves omitted reviews
	// cannot change the winner.
	matrix := buildEvaluationMatrix(results, inputs, cfg.Normalization)
	matrix.Certificate = certificate

	winner, winningOutput := selectWinner(matrix.FinalScores, inputs)
	if certificate != nil && certificate.Certified {
		winner = certificate.Winner
		winningOutput = outputForAgent(inputs, winner)
	}

	// Handle all-invalid evaluations exactly as the exhaustive implementation did.
	if winner == "" && len(inputs) > 0 {
		winner = inputs[0].AgentID
		winningOutput = inputs[0].Output
	}

	// Calculate total tokens (including rubric generation if dynamic)
	totalTokens := rubricTokens
	for _, r := range results {
		totalTokens += r.TokensUsed
	}

	// Best-effort agreement metadata — extraction config is intentionally optional here.
	// Peer matrix uses rubric-scored peer evaluations, not extracted answers. Extraction only
	// feeds diagnostic ComputeAgreement metrics; missing config degrades to no-op extractor.
	extractCfg := ParseExtractorConfig(config)
	agreeRatio, consensus, dissenting := ComputeAgreement(inputs, extractCfg)

	reasoning := fmt.Sprintf("Peer evaluation with %d reviews (%d invalid)", len(results), matrix.InvalidCount)
	if certificate != nil {
		if certificate.Certified {
			marginText := ""
			if certificate.GuaranteedMargin != nil {
				marginText = fmt.Sprintf(", guaranteed margin %.4f", *certificate.GuaranteedMargin)
			}
			reasoning = fmt.Sprintf(
				"Certified progressive peer evaluation locked %s after %d/%d reviews; skipped %d (%.1f%%)%s (%d invalid)",
				certificate.Winner,
				certificate.CompletedEvaluations,
				certificate.TotalEvaluations,
				certificate.SkippedEvaluations,
				certificate.SavingsRatio*100,
				marginText,
				matrix.InvalidCount,
			)
		} else {
			reasoning = fmt.Sprintf(
				"Certified progressive peer evaluation completed %d/%d reviews without an early winner certificate (%d invalid)",
				certificate.CompletedEvaluations,
				certificate.TotalEvaluations,
				matrix.InvalidCount,
			)
		}
	}

	return &AggregationResult{
		Output:          winningOutput,
		Method:          AggMethodPeerMatrix,
		Winner:          winner,
		Scores:          matrix.FinalScores,
		TokensUsed:      totalTokens,
		Reasoning:       reasoning,
		EvalMatrix:      matrix,
		AgreementRatio:  agreeRatio,
		ConsensusAnswer: consensus,
		DissentingIDs:   dissenting,
	}, nil
}

// parsePeerMatrixConfig extracts peer matrix config from map
func parsePeerMatrixConfig(config map[string]interface{}) (PeerMatrixConfig, error) {
	cfg := PeerMatrixConfig{}
	if config == nil {
		return cfg, fmt.Errorf("peer_matrix aggregation requires aggregationConfig: %w", ErrAggregationConfig)
	}

	if model, ok := config["rubric_model"].(string); ok && model != "" {
		cfg.RubricModel = model
	}
	sysPrompt, _ := config["eval_system_prompt"].(string)
	if strings.TrimSpace(sysPrompt) == "" {
		return cfg, fmt.Errorf("peer_matrix aggregation requires aggregationConfig.eval_system_prompt: %w", ErrAggregationConfig)
	}
	cfg.EvalSystemPrompt = sysPrompt

	prompt, _ := config["eval_prompt"].(string)
	if strings.TrimSpace(prompt) == "" {
		return cfg, fmt.Errorf("peer_matrix aggregation requires aggregationConfig.eval_prompt: %w", ErrAggregationConfig)
	}
	cfg.EvalPrompt = prompt

	// Agent identity is never exposed to evaluators (blind evaluation, Principle #1)
	norm, _ := config["normalization"].(string)
	if strings.TrimSpace(norm) == "" {
		return cfg, fmt.Errorf("peer_matrix aggregation requires aggregationConfig.normalization: %w", ErrAggregationConfig)
	}
	cfg.Normalization = norm

	maxPRaw, ok := numericToFloat64(config["max_parallel"])
	if !ok || maxPRaw <= 0 {
		return cfg, fmt.Errorf("peer_matrix aggregation requires positive numeric aggregationConfig.max_parallel: %w", ErrAggregationConfig)
	}
	cfg.MaxParallel = int(maxPRaw)

	temp, ok := numericToFloat64(config["temperature"])
	if !ok {
		return cfg, fmt.Errorf("peer_matrix aggregation requires numeric aggregationConfig.temperature: %w", ErrAggregationConfig)
	}
	cfg.Temperature = temp

	maxTokens, ok := aggregationMaxTokens(config)
	if !ok {
		return cfg, fmt.Errorf("peer_matrix aggregation requires aggregationConfig.max_tokens: %w", ErrAggregationConfig)
	}
	cfg.MaxTokens = maxTokens

	if routing := parseOpenRouterProviderConfig(config); routing != nil {
		cfg.OpenRouterProvider = routing
	}
	if reasoning := parseOpenRouterReasoningConfig(config); reasoning != nil {
		cfg.OpenRouterReasoning = reasoning
	}

	rubricData, ok := config["rubric"]
	if !ok {
		return cfg, fmt.Errorf("peer_matrix aggregation requires aggregationConfig.rubric: %w", ErrAggregationConfig)
	}
	rubric := parsePeerRubric(rubricData)
	if len(rubric) == 0 {
		return cfg, fmt.Errorf("peer_matrix aggregation requires non-empty aggregationConfig.rubric: %w", ErrAggregationConfig)
	}
	cfg.Rubric = rubric

	return cfg, nil
}

// parsePeerRubric parses rubric data from config
func parsePeerRubric(rubricData interface{}) []RubricCriterion {
	return parseRubricCriteria(rubricData)
}

// buildEvalTasks creates evaluation tasks for all reviewer-candidate pairs
func buildEvalTasks(inputs []AgentOutput) []evalTask {
	var tasks []evalTask
	for _, reviewer := range inputs {
		reviewerName := reviewer.AgentID
		if name, ok := reviewer.Metadata["name"].(string); ok && name != "" {
			reviewerName = name
		}
		for _, candidate := range inputs {
			// Skip self-evaluation
			if reviewer.AgentID == candidate.AgentID {
				continue
			}
			tasks = append(tasks, evalTask{
				ReviewerID:     reviewer.AgentID,
				ReviewerName:   reviewerName,
				ReviewerModel:  reviewer.Model,
				CandidateID:    candidate.AgentID,
				Response:       candidate.Output,
				ReviewerAnswer: reviewer.Output,
			})
		}
	}
	return tasks
}

// executeEvaluations runs all evaluation tasks in parallel
func (p *PeerMatrixAggregator) executeEvaluations(
	ctx context.Context,
	tasks []evalTask,
	cfg PeerMatrixConfig,
	originalPrompt string,
	llmClient *providers.Client,
	aggCtx *AggregationContext,
) []evalResult {
	results := make([]evalResult, len(tasks))
	if len(tasks) == 0 {
		return results
	}

	workers := cfg.MaxParallel
	if workers < 1 {
		workers = 1
	}
	if workers > len(tasks) {
		workers = len(tasks)
	}

	promptTemplate := cfg.EvalPrompt

	type evalJob struct {
		index int
		task  evalTask
	}
	jobs := make(chan evalJob)

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				result := p.executeSingleEvaluation(ctx, job.task, cfg, promptTemplate, originalPrompt, llmClient, aggCtx)
				results[job.index] = result
			}
		}()
	}

	for i, task := range tasks {
		jobs <- evalJob{
			index: i,
			task:  task,
		}
	}
	close(jobs)

	wg.Wait()
	return results
}

// executeSingleEvaluation runs a single evaluation
func (p *PeerMatrixAggregator) executeSingleEvaluation(
	ctx context.Context,
	task evalTask,
	cfg PeerMatrixConfig,
	promptTemplate string,
	originalPrompt string,
	llmClient *providers.Client,
	aggCtx *AggregationContext,
) evalResult {
	result := evalResult{
		ReviewerID:  task.ReviewerID,
		CandidateID: task.CandidateID,
		Valid:       false,
	}

	// Build rubric text if rubric is provided
	var rubricText string
	if len(cfg.Rubric) > 0 {
		var rubricBuilder strings.Builder
		rubricBuilder.WriteString("Scoring Rubric:\n")
		for _, criterion := range cfg.Rubric {
			fmt.Fprintf(&rubricBuilder, "- %s (%.0f%%): %s\n", normalizeRubricKey(criterion.Name), criterion.Weight*100, criterion.Description)
		}
		rubricBuilder.WriteString("\n")
		rubricText = rubricBuilder.String()
	}

	// Build the evaluation user prompt
	userPrompt := promptTemplate
	userPrompt = strings.ReplaceAll(userPrompt, "{{question}}", originalPrompt)
	userPrompt = strings.ReplaceAll(userPrompt, "{{reviewer_answer}}", task.ReviewerAnswer)
	userPrompt = strings.ReplaceAll(userPrompt, "{{response}}", task.Response)
	userPrompt = strings.ReplaceAll(userPrompt, "{{rubric}}", rubricText)

	// Build messages with system and user prompts
	var messages []providers.Message
	if cfg.EvalSystemPrompt != "" {
		messages = append(messages, providers.Message{Role: "system", Content: cfg.EvalSystemPrompt})
	}
	messages = append(messages, providers.Message{Role: "user", Content: userPrompt})

	// Per-agent routing: use the reviewer's own model when available,
	// and fail if reviewer model is unavailable.
	if strings.TrimSpace(task.ReviewerModel) == "" {
		return result
	}
	req := &providers.ClientRequest{
		Model:       task.ReviewerModel,
		Messages:    messages,
		MaxTokens:   cfg.MaxTokens,
		Temperature: providers.Float64Ptr(cfg.Temperature),
	}
	if cfg.OpenRouterProvider != nil {
		req.ProviderRouting = cfg.OpenRouterProvider
	}
	if cfg.OpenRouterReasoning != nil {
		req.Reasoning = cfg.OpenRouterReasoning
	}

	compCtx := &providers.CompletionContext{
		NodeLabel: "Peer Eval",
		NodeName:  fmt.Sprintf("%s evaluates %s", task.ReviewerID, task.CandidateID),
	}
	if aggCtx != nil {
		compCtx.JobID = aggCtx.JobID
		compCtx.RunID = aggCtx.RunID
		compCtx.ParentNodeID = aggCtx.NodeID
		compCtx.CostTracker = aggCtx.CostTracker
		// Use unique sub-node ID for each evaluation
		compCtx.NodeID = fmt.Sprintf("%s__agg__peer__%s__%s", aggCtx.NodeID, task.ReviewerID, task.CandidateID)
	}

	resp, err := llmClient.Complete(ctx, req, compCtx)
	if err != nil {
		return result
	}

	result.TokensUsed = resp.Usage.TotalTokens

	// Guard: empty evaluations are invalid (including reasoning-only completions).
	if strings.TrimSpace(resp.Content) == "" {
		return result
	}

	// Parse the score using a 2-level fallback chain:
	// 1. Per-criterion with reasoning: {"key": {"reasoning": "...", "score": N}}
	if criterionScores, ok := parseCriterionScores(resp.Content); ok && len(criterionScores) > 0 {
		if weighted, matched := calculateWeightedScore(criterionScores, cfg.Rubric); matched {
			result.Score = weighted
			result.CriterionScores = criterionScores
			result.Valid = true
			return result
		}
	}

	// 2. Scores-only: {"scores": {"key": N}}. Keep the documented 1-10
	// contract strict here too; the shared parser historically accepted
	// out-of-range values in its JSON branch.
	if scores := parseScores(resp.Content); peerScoresWithinCertifiedRange(scores) {
		if weighted, matched := calculateWeightedScore(scores, cfg.Rubric); matched {
			result.Score = weighted
			result.CriterionScores = scores
			result.Valid = true
			return result
		}
	}

	return result
}

// parseCriterionScores extracts per-criterion scores from a JSON response in the format:
// {"criterion_key": {"reasoning": "...", "score": N}, ...}
// Returns normalized keys and true if at least one criterion was parsed.
func parseCriterionScores(response string) (map[string]float64, bool) {
	// Find the outermost JSON object in the response
	start := strings.Index(response, "{")
	if start < 0 {
		return nil, false
	}
	end := strings.LastIndex(response, "}")
	if end <= start {
		return nil, false
	}
	jsonStr := response[start : end+1]

	// Unmarshal into generic map
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil, false
	}

	scores := make(map[string]float64)
	for key, val := range raw {
		// Try to parse as {"reasoning": "...", "score": N}
		var entry struct {
			Reasoning string  `json:"reasoning"`
			Score     float64 `json:"score"`
		}
		if err := json.Unmarshal(val, &entry); err != nil {
			continue
		}
		if entry.Score < 1 || entry.Score > 10 {
			continue
		}
		scores[normalizeRubricKey(key)] = entry.Score
	}

	if len(scores) == 0 {
		return nil, false
	}
	return scores, true
}

// buildEvaluationMatrix constructs the matrix from evaluation results
func buildEvaluationMatrix(results []evalResult, inputs []AgentOutput, normalization string) *EvaluationMatrix {
	matrix := &EvaluationMatrix{
		RawScores:        make(map[string]map[string]float64),
		NormalizedScores: make(map[string]map[string]float64),
		FinalScores:      make(map[string]float64),
		ReviewerBias:     make(map[string]float64),
	}

	// Initialize maps for all agents
	for _, input := range inputs {
		matrix.RawScores[input.AgentID] = make(map[string]float64)
		matrix.NormalizedScores[input.AgentID] = make(map[string]float64)
	}

	// Populate raw scores
	for _, r := range results {
		if r.Valid {
			matrix.RawScores[r.ReviewerID][r.CandidateID] = r.Score
		} else {
			matrix.InvalidCount++
		}
	}

	// Apply normalization (currently only "none" is supported; field kept for future strategies)
	for reviewer, scores := range matrix.RawScores {
		for candidate, score := range scores {
			matrix.NormalizedScores[reviewer][candidate] = score
		}
	}

	// Calculate final scores in stable task/result order. Iterating nested Go
	// maps made floating-point summation order nondeterministic across runs.
	candidateTotals := make(map[string]float64)
	candidateCounts := make(map[string]int)
	for _, r := range results {
		if !r.Valid {
			continue
		}
		candidateTotals[r.CandidateID] += r.Score
		candidateCounts[r.CandidateID]++
	}

	for candidate, total := range candidateTotals {
		if candidateCounts[candidate] > 0 {
			matrix.FinalScores[candidate] = total / float64(candidateCounts[candidate])
		}
	}

	return matrix
}

// selectWinner picks the candidate with the highest final score
func selectWinner(finalScores map[string]float64, inputs []AgentOutput) (string, string) {
	if len(finalScores) == 0 {
		return "", ""
	}

	// Sort by score descending, then by ID ascending for deterministic tie-breaking
	type scored struct {
		id    string
		score float64
	}
	var sorted []scored
	for id, score := range finalScores {
		sorted = append(sorted, scored{id, score})
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].score != sorted[j].score {
			return sorted[i].score > sorted[j].score
		}
		// Tie-breaker: alphabetical by ID for deterministic results
		return sorted[i].id < sorted[j].id
	})

	winnerID := sorted[0].id
	return winnerID, outputForAgent(inputs, winnerID)
}

func outputForAgent(inputs []AgentOutput, agentID string) string {
	for _, input := range inputs {
		if input.AgentID == agentID {
			return input.Output
		}
	}
	return ""
}

func ensurePeerReviewerModels(tasks []evalTask) error {
	missingByReviewer := make(map[string]struct{})
	for _, task := range tasks {
		if strings.TrimSpace(task.ReviewerModel) == "" {
			missingByReviewer[task.ReviewerID] = struct{}{}
		}
	}
	if len(missingByReviewer) == 0 {
		return nil
	}

	missing := make([]string, 0, len(missingByReviewer))
	for reviewerID := range missingByReviewer {
		missing = append(missing, reviewerID)
	}
	sort.Strings(missing)
	return fmt.Errorf("peer_matrix requires reviewer model for all evaluators; missing model for: %s", strings.Join(missing, ", "))
}
