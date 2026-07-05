package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/alhasaniq/consortium/pkg/providers"
)

// ErrAggregationConfig is returned when an aggregator is called with missing or
// invalid configuration fields. Callers can use errors.Is to distinguish
// configuration errors from runtime (LLM call / parse) failures.
var ErrAggregationConfig = errors.New("aggregation configuration error")

// AgentOutput represents the output from a single agent for aggregation
type AgentOutput struct {
	AgentID  string                 `json:"agent_id"`
	Model    string                 `json:"model,omitempty"`
	Output   string                 `json:"output"`
	Tokens   int                    `json:"tokens"`
	Cost     float64                `json:"cost"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// EvaluationMatrix tracks who evaluated whom and the scores (for peer_matrix)
type EvaluationMatrix struct {
	RawScores        map[string]map[string]float64 `json:"raw_scores"`        // [reviewer][candidate] -> score
	NormalizedScores map[string]map[string]float64 `json:"normalized_scores"` // After bias correction
	FinalScores      map[string]float64            `json:"final_scores"`      // Averaged per candidate
	ReviewerBias     map[string]float64            `json:"reviewer_bias"`     // Bias stats per reviewer
	InvalidCount     int                           `json:"invalid_count"`     // Number of failed evaluations
}

// AggregationResult contains the result of an aggregation operation
type AggregationResult struct {
	// Output is the final aggregated output string
	Output string `json:"output"`
	// Method is the aggregation method used
	Method AggregationMethod `json:"method"`
	// Winner is the ID of the winning agent (for judge/scoring/peer_matrix)
	Winner string `json:"winner,omitempty"`
	// Scores contains per-agent scores (for scoring/peer_matrix methods)
	Scores map[string]float64 `json:"scores,omitempty"`
	// Reasoning explains the aggregation decision
	Reasoning string `json:"reasoning,omitempty"`
	// TokensInput is the total input tokens used during aggregation
	TokensInput int `json:"tokens_input"`
	// TokensOutput is the total output tokens used during aggregation
	TokensOutput int `json:"tokens_output"`
	// TokensUsed is the total tokens used (for backward compatibility)
	TokensUsed int `json:"tokens_used"`
	// Cost is the total cost of aggregation operations
	Cost float64 `json:"cost"`
	// EvalMatrix contains the full evaluation matrix (for peer_matrix)
	EvalMatrix *EvaluationMatrix `json:"eval_matrix,omitempty"`
	// AgreementRatio is the fraction of agents that agree on the consensus answer (0.0-1.0)
	AgreementRatio float64 `json:"agreement_ratio,omitempty"`
	// ConsensusAnswer is the most common extracted answer across agents
	ConsensusAnswer string `json:"consensus_answer,omitempty"`
	// DissentingIDs lists agent IDs that disagree with the consensus
	DissentingIDs []string `json:"dissenting_ids,omitempty"`
}

// AggregationContext provides context for aggregation operations
type AggregationContext struct {
	JobID       string       // Parent job ID for tracking
	RunID       string       // Durable per-run identity
	NodeID      string       // The output node ID
	CostTracker *CostTracker // Shared tracker for aggregation LLM sub-calls
}

// Aggregator defines the interface for aggregation strategies
type Aggregator interface {
	// Name returns the aggregation method name
	Name() AggregationMethod
	// Aggregate combines multiple agent outputs into a single result
	Aggregate(ctx context.Context, inputs []AgentOutput, config map[string]interface{}, llmClient *providers.Client, aggCtx *AggregationContext) (*AggregationResult, error)
}

// AggregatorRegistry manages available aggregation strategies
type AggregatorRegistry struct {
	mu          sync.RWMutex
	aggregators map[AggregationMethod]Aggregator
}

// NewAggregatorRegistry creates a new registry with default aggregators
func NewAggregatorRegistry() *AggregatorRegistry {
	r := &AggregatorRegistry{
		aggregators: make(map[AggregationMethod]Aggregator),
	}
	// Register all aggregators
	r.Register(&CollectAggregator{})
	r.Register(&SynthesisAggregator{})
	r.Register(&JudgeAggregator{})
	r.Register(&ScoringAggregator{})
	r.Register(&PeerMatrixAggregator{})
	r.Register(&MajorityVoteAggregator{})
	r.Register(&DebateDecideAggregator{})
	return r
}

// Register adds an aggregator to the registry
func (r *AggregatorRegistry) Register(a Aggregator) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.aggregators[a.Name()] = a
}

// Get retrieves an aggregator by method name
func (r *AggregatorRegistry) Get(method AggregationMethod) (Aggregator, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.aggregators[method]
	return a, ok
}

// CollectAggregator is the default aggregator that simply collects all inputs
type CollectAggregator struct{}

// Name returns the aggregation method name
func (c *CollectAggregator) Name() AggregationMethod {
	return AggMethodCollect
}

// Aggregate collects all inputs and joins them with a configurable separator
func (c *CollectAggregator) Aggregate(ctx context.Context, inputs []AgentOutput, config map[string]interface{}, llmClient *providers.Client, aggCtx *AggregationContext) (*AggregationResult, error) {
	if single, err := checkSingleInput(inputs, AggMethodCollect); err != nil {
		return nil, err
	} else if single != nil {
		// Collect doesn't pick a "winner" — clear winner-specific fields.
		single.Result.Winner = ""
		single.Result.Scores = nil
		single.Result.Reasoning = ""
		return single.Result, nil
	}

	// Multiple inputs: join with separator
	separator, _ := config["separator"].(string)
	if separator == "" {
		separator = "\n---\n"
	}

	var outputs string
	for i, input := range inputs {
		if i > 0 {
			outputs += separator
		}
		outputs += input.Output
	}

	return &AggregationResult{
		Output: outputs,
		Method: AggMethodCollect,
	}, nil
}

// singleInputResult is returned by checkSingleInput when there is exactly one
// input, providing the caller with a pre-built AggregationResult that can be
// returned directly or augmented with method-specific fields.
type singleInputResult struct {
	Result *AggregationResult
}

// checkSingleInput handles the common empty/single-input checks that every
// aggregator performs at the top of its Aggregate method.
//
// Returns:
//   - (nil, error) when inputs is empty
//   - (*singleInputResult, nil) when there is exactly one input — the caller
//     should return Result (possibly after adding method-specific fields)
//   - (nil, nil) when there are multiple inputs and normal aggregation should proceed
func checkSingleInput(inputs []AgentOutput, method AggregationMethod) (*singleInputResult, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("%s aggregation requires at least one input", method)
	}
	if len(inputs) == 1 {
		return &singleInputResult{
			Result: &AggregationResult{
				Output:    inputs[0].Output,
				Method:    method,
				Winner:    inputs[0].AgentID,
				Scores:    map[string]float64{inputs[0].AgentID: 1.0},
				Reasoning: "Single response - wins by default",
			},
		}, nil
	}
	return nil, nil
}

// aggLLMCall bundles parameters for callAggregatorLLM to avoid long argument lists.
type aggLLMCall struct {
	// Scope identifies the aggregation method for error messages (e.g. "judge", "synthesis").
	Scope string
	// Model is the LLM model to use.
	Model string
	// SystemPrompt is appended as a system message (may be empty to omit).
	SystemPrompt string
	// UserPrompt is the user message content.
	UserPrompt string
	// MaxTokens caps the completion length.
	MaxTokens int
	// Temperature controls sampling randomness (nil = provider default).
	Temperature *float64
	// Config is the raw aggregation config map, used to extract provider routing
	// and reasoning config. May be nil.
	Config map[string]interface{}
	// Messages overrides the default [system, user] message construction.
	// When non-nil, SystemPrompt and UserPrompt are ignored and Messages is
	// used directly. This supports callers that need custom message sequences
	// (e.g. few-shot examples, repair calls).
	Messages []providers.Message
	// NodeLabel is a short label for the CompletionContext (e.g. "Judge").
	NodeLabel string
	// NodeName is a human-readable name for the CompletionContext (e.g. "Judge Aggregation").
	NodeName string
	// SubNodeID is the suffix for the unique sub-node ID (e.g. "judge", "judge_repair").
	// The full NodeID is built as "{aggCtx.NodeID}__agg__{SubNodeID}".
	SubNodeID string
	// Attempt is the 1-based attempt number (0 or 1 both mean first attempt).
	Attempt int
}

// callAggregatorLLM performs a single LLM call with the shared boilerplate:
// message construction, request building (with provider routing & reasoning),
// CompletionContext wiring, the Complete call, and the empty-output guard.
//
// Returns the raw CompletionResponse on success. Callers extract text, tokens,
// and cost as needed for their specific aggregation logic.
func callAggregatorLLM(
	ctx context.Context,
	p aggLLMCall,
	llmClient *providers.Client,
	aggCtx *AggregationContext,
) (*providers.CompletionResponse, error) {
	if llmClient == nil {
		return nil, fmt.Errorf("LLM client is required for %s aggregation", p.Scope)
	}

	// Build messages: use explicit Messages if provided, otherwise construct from system+user.
	messages := p.Messages
	if messages == nil {
		if p.SystemPrompt != "" {
			messages = append(messages, providers.Message{Role: "system", Content: p.SystemPrompt})
		}
		messages = append(messages, providers.Message{Role: "user", Content: p.UserPrompt})
	}

	req := &providers.ClientRequest{
		Model:       p.Model,
		Messages:    messages,
		MaxTokens:   p.MaxTokens,
		Temperature: p.Temperature,
	}
	if routing := parseOpenRouterProviderConfig(p.Config); routing != nil {
		req.ProviderRouting = routing
	}
	if reasoning := parseOpenRouterReasoningConfig(p.Config); reasoning != nil {
		req.Reasoning = reasoning
	}

	compCtx := &providers.CompletionContext{
		NodeLabel: p.NodeLabel,
		NodeName:  p.NodeName,
		Attempt:   p.Attempt,
	}
	if aggCtx != nil {
		compCtx.JobID = aggCtx.JobID
		compCtx.RunID = aggCtx.RunID
		compCtx.ParentNodeID = aggCtx.NodeID
		compCtx.CostTracker = aggCtx.CostTracker
		compCtx.NodeID = fmt.Sprintf("%s__agg__%s", aggCtx.NodeID, p.SubNodeID)
	}

	resp, err := llmClient.Complete(ctx, req, compCtx)
	if err != nil {
		return nil, fmt.Errorf("%s LLM call failed: %w", p.Scope, err)
	}

	if hasEmptyOutputWithConsumedTokens(resp) {
		return nil, newEmptyOutputRetryableError(p.Scope, p.Model, resp.Usage)
	}

	return resp, nil
}

// responseTokensInput returns PromptTokens from a CompletionResponse.
func responseTokensInput(resp *providers.CompletionResponse) int {
	if resp == nil {
		return 0
	}
	return resp.Usage.PromptTokens
}

// responseTokensOutput returns CompletionTokens from a CompletionResponse.
func responseTokensOutput(resp *providers.CompletionResponse) int {
	if resp == nil {
		return 0
	}
	return resp.Usage.CompletionTokens
}

// responseTotalTokens returns TotalTokens from a CompletionResponse.
func responseTotalTokens(resp *providers.CompletionResponse) int {
	if resp == nil {
		return 0
	}
	return resp.Usage.TotalTokens
}

// responseCost returns the cost from a CompletionResponse (0 if nil/unavailable).
func responseCost(resp *providers.CompletionResponse) float64 {
	if resp == nil || resp.Usage.Cost == nil {
		return 0
	}
	return *resp.Usage.Cost
}

// ValidateAggregationConfig validates the configuration for a given method
func ValidateAggregationConfig(method AggregationMethod, config map[string]interface{}) error {
	if strings.TrimSpace(string(method)) == "" {
		return fmt.Errorf("aggregation method is required")
	}

	requireString := func(key string) error {
		val, _ := config[key].(string)
		if strings.TrimSpace(val) == "" {
			return fmt.Errorf("%s requires %s in aggregationConfig", method, key)
		}
		return nil
	}
	requireTemperature := func() error {
		if _, ok := numericToFloat64(config["temperature"]); !ok {
			return fmt.Errorf("%s requires numeric temperature in aggregationConfig", method)
		}
		return nil
	}
	requireExtractionConfig := func() error {
		strategy, _ := config["extraction_strategy"].(string)
		pattern, _ := config["extraction_pattern"].(string)
		if strings.TrimSpace(strategy) == "" {
			return fmt.Errorf("%s requires extraction_strategy in aggregationConfig", method)
		}
		if strings.TrimSpace(pattern) == "" {
			return fmt.Errorf("%s requires extraction_pattern in aggregationConfig", method)
		}
		return nil
	}
	requireLLMFields := func() error {
		if err := requireString("system_prompt"); err != nil {
			return err
		}
		if err := requireString("prompt"); err != nil {
			return err
		}
		if err := requireTemperature(); err != nil {
			return err
		}
		if _, found := aggregationMaxTokens(config); !found {
			return fmt.Errorf("%s requires max_tokens in aggregationConfig", method)
		}
		return nil
	}
	requireRubricPlaceholder := func(promptKey string) error {
		prompt, _ := config[promptKey].(string)
		normalizedPrompt := strings.ReplaceAll(prompt, " ", "")
		if !strings.Contains(normalizedPrompt, "{{rubric}}") {
			return fmt.Errorf("%s requires %s to include {{rubric}} when rubric_mode is dynamic", method, promptKey)
		}
		return nil
	}

	switch method {
	case AggMethodCollect:
		// Optional: separator
		return nil
	case AggMethodJudge:
		if err := requireString("judge_model"); err != nil {
			return err
		}
		if err := requireLLMFields(); err != nil {
			return err
		}
		if _, found := aggregationRepairMaxTokens(config); !found {
			return fmt.Errorf("judge requires repair_max_tokens in aggregationConfig")
		}
		return nil
	case AggMethodScoring:
		if err := requireString("scoring_model"); err != nil {
			return err
		}
		if err := requireLLMFields(); err != nil {
			return err
		}
		if rubricMode, _ := config["rubric_mode"].(string); strings.EqualFold(rubricMode, "dynamic") {
			return requireRubricPlaceholder("prompt")
		}
		return nil
	case AggMethodSynthesis:
		if err := requireString("model"); err != nil {
			return err
		}
		return requireLLMFields()
	case AggMethodPeerMatrix:
		if err := requireString("eval_system_prompt"); err != nil {
			return err
		}
		if err := requireString("eval_prompt"); err != nil {
			return err
		}
		if err := requireString("normalization"); err != nil {
			return err
		}
		if err := requireTemperature(); err != nil {
			return err
		}
		if _, found := aggregationMaxTokens(config); !found {
			return fmt.Errorf("peer_matrix requires max_tokens in aggregationConfig")
		}
		if raw, ok := config["max_parallel"]; !ok {
			return fmt.Errorf("peer_matrix requires max_parallel in aggregationConfig")
		} else if value, ok := numericToFloat64(raw); !ok || value <= 0 {
			return fmt.Errorf("peer_matrix requires positive numeric max_parallel in aggregationConfig")
		}
		if rubricRaw, ok := config["rubric"]; !ok {
			return fmt.Errorf("peer_matrix requires rubric in aggregationConfig")
		} else if len(parsePeerRubric(rubricRaw)) == 0 {
			return fmt.Errorf("peer_matrix requires non-empty rubric in aggregationConfig")
		}
		if model, ok := config["judge_model"].(string); ok && strings.TrimSpace(model) != "" {
			return fmt.Errorf("peer_matrix does not support judge_model; each reviewer must have its own model")
		}
		if rubricMode, _ := config["rubric_mode"].(string); strings.EqualFold(rubricMode, "dynamic") {
			if model, _ := config["rubric_model"].(string); strings.TrimSpace(model) == "" {
				return fmt.Errorf("peer_matrix requires rubric_model when rubric_mode is dynamic")
			}
			if err := requireRubricPlaceholder("eval_prompt"); err != nil {
				return err
			}
		}
		return nil
	case AggMethodMajorityVote:
		// Required: tie_breaker_method.
		// If tie_breaker_method=synthesis, tie_breaker_model+tie_breaker_temperature are required,
		// plus the synthesis-specific fields that the fallback delegates to.
		// Extraction configuration is mandatory; no implicit defaults.
		if err := requireExtractionConfig(); err != nil {
			return err
		}
		tieBreaker, err := parseMajorityVoteTieBreaker(config)
		if err != nil {
			return err
		}
		if tieBreaker == "synthesis" {
			if err := requireString("system_prompt"); err != nil {
				return fmt.Errorf("majority_vote with tie_breaker_method=synthesis requires system_prompt in aggregationConfig (used by synthesis fallback)")
			}
			if err := requireString("prompt"); err != nil {
				return fmt.Errorf("majority_vote with tie_breaker_method=synthesis requires prompt in aggregationConfig (used by synthesis fallback)")
			}
			if _, found := aggregationMaxTokens(config); !found {
				return fmt.Errorf("majority_vote with tie_breaker_method=synthesis requires max_tokens in aggregationConfig (used by synthesis fallback)")
			}
		}
		return nil
	case AggMethodDebateDecide:
		if err := requireString("judge_model"); err != nil {
			return err
		}
		if err := requireLLMFields(); err != nil {
			return err
		}
		if _, found := aggregationRepairMaxTokens(config); !found {
			return fmt.Errorf("debate_decide requires repair_max_tokens in aggregationConfig")
		}
		return requireExtractionConfig()
	default:
		return fmt.Errorf("unknown aggregation method: %s", method)
	}
}
