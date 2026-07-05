package providers

import (
	"context"
	"encoding/json"
	"time"
)

// Provider represents a generic LLM provider interface
type Provider interface {
	// Name returns the provider name (e.g., "openai", "anthropic")
	Name() string

	// Models returns available models for this provider
	Models() []Model

	// Complete performs a completion request
	Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)

	// EstimateTokens estimates token count for a given text
	EstimateTokens(text string) int

	// Cost calculates cost for a given number of tokens
	Cost(model string, inputTokens, outputTokens int) float64
}

// Model represents an LLM model configuration
type Model struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Provider   string  `json:"provider"`
	ContextLen int     `json:"context_length"`
	InputCost  float64 `json:"input_cost_per_token"`  // Cost per input token in USD
	OutputCost float64 `json:"output_cost_per_token"` // Cost per output token in USD
	MaxTokens  int     `json:"max_tokens"`
	Available  bool    `json:"available"`

	// Capability metadata from provider catalogs (OpenRouter /models).
	SupportedParameters []string `json:"supported_parameters,omitempty"`
	InputModalities     []string `json:"input_modalities,omitempty"`
	OutputModalities    []string `json:"output_modalities,omitempty"`
}

// CompletionRequest represents a completion request.
// Temperature and TopP use pointers: nil = unset (use provider default), 0.0 = explicit zero.
type CompletionRequest struct {
	Model       string            `json:"model"`
	Messages    []Message         `json:"messages"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Temperature *float64          `json:"temperature,omitempty"`
	TopP        *float64          `json:"top_p,omitempty"`
	Stop        []string          `json:"stop,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`

	// Provider-specific extensions (OpenRouter routing, reasoning, etc.)
	Extensions *ProviderExtensions `json:"extensions,omitempty"`

	// OpenAI/OpenRouter tool-call controls
	Tools             []ToolDefinition `json:"tools,omitempty"`
	ToolChoice        interface{}      `json:"tool_choice,omitempty"`
	ParallelToolCalls *bool            `json:"parallel_tool_calls,omitempty"`
}

// ProviderExtensions holds provider-specific request controls that are not part
// of the standard OpenAI-compatible completion API. Isolating them here prevents
// provider implementation details from leaking into the generic request contract.
type ProviderExtensions struct {
	ResponseFormat     *ResponseFormat  `json:"response_format,omitempty"`
	Seed               *int             `json:"seed,omitempty"`
	ProviderRouting    *ProviderRouting `json:"provider,omitempty"`
	Reasoning          *ReasoningConfig `json:"reasoning,omitempty"`
	SessionID          string           `json:"session_id,omitempty"`
	OpenRouterMetadata *bool            `json:"openrouter_metadata,omitempty"`
}

// ResponseFormat controls the output format of the model response.
// Supports "json_object" (basic JSON mode) and "json_schema" (strict schema mode).
type ResponseFormat struct {
	Type       string      `json:"type"`                  // "json_object", "json_schema", or "text"
	JsonSchema *JsonSchema `json:"json_schema,omitempty"` // required when Type is "json_schema"
}

// JsonSchema defines a named JSON Schema for structured output enforcement.
type JsonSchema struct {
	Name   string          `json:"name"`
	Strict bool            `json:"strict,omitempty"`
	Schema json.RawMessage `json:"schema"`
}

// ToolDefinition describes an OpenAI-compatible tool (function).
type ToolDefinition struct {
	Type     string                 `json:"type"` // "function"
	Function ToolFunctionDefinition `json:"function"`
}

// ToolFunctionDefinition describes a function tool schema.
type ToolFunctionDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
	Strict      bool                   `json:"strict,omitempty"`
}

// ProviderRouting controls OpenRouter provider routing behavior.
type ProviderRouting struct {
	Order                  []string          `json:"order,omitempty"`
	Only                   []string          `json:"only,omitempty"`
	Ignore                 []string          `json:"ignore,omitempty"`
	AllowFallbacks         *bool             `json:"allow_fallbacks,omitempty"`
	RequireParameters      *bool             `json:"require_parameters,omitempty"`
	Sort                   interface{}       `json:"sort,omitempty"`
	Quantizations          []string          `json:"quantizations,omitempty"`
	MaxPrice               *ProviderMaxPrice `json:"max_price,omitempty"`
	DataCollection         string            `json:"data_collection,omitempty"`
	ZDR                    *bool             `json:"zdr,omitempty"`
	PreferredMaxLatency    interface{}       `json:"preferred_max_latency,omitempty"`
	PreferredMinThroughput interface{}       `json:"preferred_min_throughput,omitempty"`
	EnforceDistillableText *bool             `json:"enforce_distillable_text,omitempty"`
}

type ProviderMaxPrice struct {
	Prompt     interface{} `json:"prompt,omitempty"`
	Completion interface{} `json:"completion,omitempty"`
	Request    interface{} `json:"request,omitempty"`
	Image      interface{} `json:"image,omitempty"`
	Audio      interface{} `json:"audio,omitempty"`
}

// ReasoningConfig controls OpenRouter reasoning behavior.
type ReasoningConfig struct {
	Effort    string `json:"effort,omitempty"`
	MaxTokens *int   `json:"max_tokens,omitempty"`
	Enabled   *bool  `json:"enabled,omitempty"`
	Exclude   *bool  `json:"exclude,omitempty"`
	Summary   string `json:"summary,omitempty"`
}

// Float64Ptr returns a pointer to the given float64 value.
func Float64Ptr(v float64) *float64 { return &v }

// IntPtr returns a pointer to the given int value.
func IntPtr(v int) *int { return &v }

// BoolPtr returns a pointer to the given bool value.
func BoolPtr(v bool) *bool { return &v }

// Message represents a chat message
type Message struct {
	Role    string `json:"role"` // "system", "user", "assistant"
	Content string `json:"content"`
}

// CompletionResponse represents a completion response
type CompletionResponse struct {
	ID                 string          `json:"id"`
	RequestID          string          `json:"request_id,omitempty"`    // Provider request ID from response headers (when available)
	GenerationID       string          `json:"generation_id,omitempty"` // OpenRouter generation ID from response headers
	Model              string          `json:"model"`
	Content            string          `json:"content"`
	ToolCalls          []ToolCall      `json:"tool_calls,omitempty"`
	Usage              Usage           `json:"usage"`
	Finish             string          `json:"finish_reason"`
	ServiceTier        string          `json:"service_tier,omitempty"`
	OpenRouterMetadata json.RawMessage `json:"openrouter_metadata,omitempty"`
	Reasoning          string          `json:"reasoning,omitempty"`
	ReasoningDetails   json.RawMessage `json:"reasoning_details,omitempty"`
	Latency            float64         `json:"latency_ms"`
	RateLimitWaitMs    float64         `json:"rate_limit_wait_ms,omitempty"` // Time spent waiting on client-side rate limiter
}

// ToolCall is the OpenAI-compatible tool call payload returned by providers.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// Usage represents token usage information
type Usage struct {
	PromptTokens            int                      `json:"prompt_tokens"`
	CompletionTokens        int                      `json:"completion_tokens"`
	TotalTokens             int                      `json:"total_tokens"`
	Cost                    *float64                 `json:"cost,omitempty"` // Actual cost in USD reported by the provider (nil if not available)
	PromptTokensDetails     *PromptTokensDetails     `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *CompletionTokensDetails `json:"completion_tokens_details,omitempty"`
	CostDetails             map[string]interface{}   `json:"cost_details,omitempty"`
	IsBYOK                  *bool                    `json:"is_byok,omitempty"`
	RawJSON                 json.RawMessage          `json:"raw_json,omitempty"`
}

type PromptTokensDetails struct {
	CachedTokens     int `json:"cached_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
	AudioTokens      int `json:"audio_tokens,omitempty"`
	ImageTokens      int `json:"image_tokens,omitempty"`
	VideoTokens      int `json:"video_tokens,omitempty"`
}

type CompletionTokensDetails struct {
	ReasoningTokens          int `json:"reasoning_tokens,omitempty"`
	AudioTokens              int `json:"audio_tokens,omitempty"`
	ImageTokens              int `json:"image_tokens,omitempty"`
	VideoTokens              int `json:"video_tokens,omitempty"`
	AcceptedPredictionTokens int `json:"accepted_prediction_tokens,omitempty"`
	RejectedPredictionTokens int `json:"rejected_prediction_tokens,omitempty"`
}

// ProviderConfig holds configuration for a provider
type ProviderConfig struct {
	Name     string            `yaml:"name"`
	APIKey   string            `yaml:"api_key"`
	BaseURL  string            `yaml:"base_url,omitempty"`
	Timeout  time.Duration     `yaml:"timeout"`
	Metadata map[string]string `yaml:"metadata,omitempty"`
}
