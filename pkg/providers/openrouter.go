package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	defaultOpenRouterURL     = "https://openrouter.ai/api/v1"
	openRouterAttributionURL = "https://consortium.alhasaniq.com"
	openRouterTitle          = "Consortium"
)

// OpenRouterProvider implements the Provider interface for OpenRouter
// OpenRouter provides access to multiple AI models through a unified API
type OpenRouterProvider struct {
	config     ProviderConfig
	httpClient *http.Client
	baseURL    string // API base URL (configurable for testing)
	modelCache *ModelCache
}

// NewOpenRouterProvider creates a new OpenRouter provider
func NewOpenRouterProvider(config ProviderConfig) *OpenRouterProvider {
	baseURL := defaultOpenRouterURL
	if config.BaseURL != "" {
		baseURL = config.BaseURL
	}
	return &OpenRouterProvider{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		baseURL:    baseURL,
		modelCache: NewModelCache(config.APIKey, baseURL, 0, staticModels()),
	}
}

// Name returns the provider name
func (p *OpenRouterProvider) Name() string {
	return "openrouter"
}

// Models returns available models, delegating to the dynamic model cache.
func (p *OpenRouterProvider) Models() []Model {
	return p.modelCache.Models()
}

// RefreshModels forces a synchronous refresh of the OpenRouter model cache.
// Callers can still use Models() to read current cached values.
func (p *OpenRouterProvider) RefreshModels(ctx context.Context) error {
	return p.modelCache.Refresh(ctx)
}

// staticModels returns the hardcoded fallback model list.
// Used as the initial cache seed before the first API refresh completes.
func staticModels() []Model {
	return []Model{
		// === PLATFORM DEFAULT ===

		{
			ID:         "deepseek/deepseek-v4-flash",
			Name:       "DeepSeek V4 Flash",
			Provider:   "openrouter",
			ContextLen: 1000000,
			InputCost:  0.09 / 1000000, // $0.09 per million tokens
			OutputCost: 0.18 / 1000000, // $0.18 per million tokens
			MaxTokens:  8192,
			Available:  true,
		},

		// === TOP 20 BY USAGE (Feb 2026) ===

		// #1 - Claude Sonnet 4.5 (2.86T tokens)
		{
			ID:         "anthropic/claude-sonnet-4.5",
			Name:       "Claude Sonnet 4.5",
			Provider:   "openrouter",
			ContextLen: 1000000,
			InputCost:  3.00 / 1000000,  // $3.00 per million tokens
			OutputCost: 15.00 / 1000000, // $15.00 per million tokens
			MaxTokens:  8192,
			Available:  true,
		},
		// #2 - Gemini 3 Flash Preview (2.3T tokens)
		{
			ID:         "google/gemini-3-flash-preview",
			Name:       "Gemini 3 Flash Preview",
			Provider:   "openrouter",
			ContextLen: 1000000,
			InputCost:  0.50 / 1000000, // $0.50 per million tokens
			OutputCost: 3.00 / 1000000, // $3.00 per million tokens
			MaxTokens:  8192,
			Available:  true,
		},
		// #3 - Grok Code Fast 1 (1.94T tokens)
		{
			ID:         "x-ai/grok-code-fast-1",
			Name:       "Grok Code Fast 1",
			Provider:   "openrouter",
			ContextLen: 128000,
			InputCost:  0.20 / 1000000, // $0.20 per million tokens
			OutputCost: 1.50 / 1000000, // $1.50 per million tokens
			MaxTokens:  8192,
			Available:  true,
		},
		// #4 - Claude Opus 4.5 (1.86T tokens)
		{
			ID:         "anthropic/claude-opus-4.5",
			Name:       "Claude Opus 4.5",
			Provider:   "openrouter",
			ContextLen: 200000,
			InputCost:  5.00 / 1000000,  // $5.00 per million tokens
			OutputCost: 25.00 / 1000000, // $25.00 per million tokens
			MaxTokens:  8192,
			Available:  true,
		},
		// #5 - DeepSeek V3.2 (1.74T tokens)
		{
			ID:         "deepseek/deepseek-v3.2",
			Name:       "DeepSeek V3.2",
			Provider:   "openrouter",
			ContextLen: 256000,
			InputCost:  0.14 / 1000000, // $0.14 per million tokens
			OutputCost: 0.28 / 1000000, // $0.28 per million tokens
			MaxTokens:  8192,
			Available:  true,
		},
		// #6 - Gemini 2.5 Flash (1.64T tokens)
		{
			ID:         "google/gemini-2.5-flash",
			Name:       "Gemini 2.5 Flash",
			Provider:   "openrouter",
			ContextLen: 1000000,
			InputCost:  0.075 / 1000000, // $0.075 per million tokens
			OutputCost: 0.30 / 1000000,  // $0.30 per million tokens
			MaxTokens:  8192,
			Available:  true,
		},
		// #7 - MiMo-V2-Flash FREE (1.57T tokens)
		{
			ID:         "xiaomi/mimo-v2-flash",
			Name:       "Xiaomi: MiMo-V2-Flash",
			Provider:   "openrouter",
			ContextLen: 262144,
			InputCost:  0.09 / 1000000, // $0.09 per million tokens
			OutputCost: 0.29 / 1000000, // $0.29 per million tokens
			MaxTokens:  4096,
			Available:  true,
		},
		// #7b - MiMo-V2-Flash FREE (1.57T tokens)
		{
			ID:         "xiaomi/mimo-v2-flash:free",
			Name:       "MiMo-V2-Flash (Free)",
			Provider:   "openrouter",
			ContextLen: 128000,
			InputCost:  0.00 / 1000000, // Free
			OutputCost: 0.00 / 1000000, // Free
			MaxTokens:  8192,
			Available:  true,
		},
		// #8 - Grok 4.1 Fast (1.21T tokens)
		{
			ID:         "x-ai/grok-4.1-fast",
			Name:       "Grok 4.1 Fast",
			Provider:   "openrouter",
			ContextLen: 128000,
			InputCost:  0.30 / 1000000, // $0.30 per million tokens
			OutputCost: 2.00 / 1000000, // $2.00 per million tokens
			MaxTokens:  8192,
			Available:  true,
		},
		// #9 - Gemini 2.5 Flash Lite (1.17T tokens)
		{
			ID:         "google/gemini-2.5-flash-lite",
			Name:       "Gemini 2.5 Flash Lite",
			Provider:   "openrouter",
			ContextLen: 1000000,
			InputCost:  0.02 / 1000000, // $0.02 per million tokens
			OutputCost: 0.10 / 1000000, // $0.10 per million tokens
			MaxTokens:  8192,
			Available:  true,
		},
		// #10 - GPT OSS 120B (966B tokens)
		{
			ID:         "openai/gpt-oss-120b",
			Name:       "GPT OSS 120B",
			Provider:   "openrouter",
			ContextLen: 128000,
			InputCost:  0.10 / 1000000, // $0.10 per million tokens
			OutputCost: 0.10 / 1000000, // $0.10 per million tokens
			MaxTokens:  16384,
			Available:  true,
		},
		// #11 - Gemini 2.5 Pro (748B tokens)
		{
			ID:         "google/gemini-2.5-pro",
			Name:       "Gemini 2.5 Pro",
			Provider:   "openrouter",
			ContextLen: 1000000,
			InputCost:  1.25 / 1000000, // $1.25 per million tokens
			OutputCost: 5.00 / 1000000, // $5.00 per million tokens
			MaxTokens:  8192,
			Available:  true,
		},
		// #12 - MiniMax M2.1 (730B tokens)
		{
			ID:         "minimax/minimax-m2.1",
			Name:       "MiniMax M2.1",
			Provider:   "openrouter",
			ContextLen: 1000000,
			InputCost:  0.10 / 1000000, // $0.10 per million tokens
			OutputCost: 0.30 / 1000000, // $0.30 per million tokens
			MaxTokens:  16384,
			Available:  true,
		},
		// #13 - Gemini 3 Pro Preview (729B tokens)
		{
			ID:         "google/gemini-3-pro-preview",
			Name:       "Gemini 3 Pro Preview",
			Provider:   "openrouter",
			ContextLen: 1000000,
			InputCost:  1.25 / 1000000, // $1.25 per million tokens
			OutputCost: 5.00 / 1000000, // $5.00 per million tokens
			MaxTokens:  8192,
			Available:  true,
		},
		// #14 - Gemini 2.0 Flash (701B tokens)
		{
			ID:         "google/gemini-2.0-flash-001",
			Name:       "Gemini 2.0 Flash",
			Provider:   "openrouter",
			ContextLen: 1000000,
			InputCost:  0.075 / 1000000, // $0.075 per million tokens
			OutputCost: 0.30 / 1000000,  // $0.30 per million tokens
			MaxTokens:  8192,
			Available:  true,
		},
		// #15 - GLM 4.7 (622B tokens)
		{
			ID:         "z-ai/glm-4.7",
			Name:       "GLM 4.7",
			Provider:   "openrouter",
			ContextLen: 128000,
			InputCost:  0.20 / 1000000, // $0.20 per million tokens
			OutputCost: 0.60 / 1000000, // $0.60 per million tokens
			MaxTokens:  8192,
			Available:  true,
		},
		// #16 - Grok 4 Fast (595B tokens)
		{
			ID:         "x-ai/grok-4-fast",
			Name:       "Grok 4 Fast",
			Provider:   "openrouter",
			ContextLen: 128000,
			InputCost:  0.30 / 1000000, // $0.30 per million tokens
			OutputCost: 2.00 / 1000000, // $2.00 per million tokens
			MaxTokens:  8192,
			Available:  true,
		},
		// #17 - GPT-4o Mini (545B tokens)
		{
			ID:         "openai/gpt-4o-mini",
			Name:       "GPT-4o Mini",
			Provider:   "openrouter",
			ContextLen: 128000,
			InputCost:  0.15 / 1000000, // $0.15 per million tokens
			OutputCost: 0.60 / 1000000, // $0.60 per million tokens
			MaxTokens:  16384,
			Available:  true,
		},
		// #18 - Kimi K2.5 (484B tokens) - NEW
		{
			ID:         "moonshotai/kimi-k2.5",
			Name:       "Kimi K2.5",
			Provider:   "openrouter",
			ContextLen: 128000,
			InputCost:  0.30 / 1000000, // $0.30 per million tokens
			OutputCost: 1.00 / 1000000, // $1.00 per million tokens
			MaxTokens:  8192,
			Available:  true,
		},
		// #19 - GPT-5.2 (467B tokens)
		{
			ID:         "openai/gpt-5.2",
			Name:       "GPT-5.2",
			Provider:   "openrouter",
			ContextLen: 400000,
			InputCost:  1.50 / 1000000,  // $1.50 per million tokens
			OutputCost: 12.00 / 1000000, // $12.00 per million tokens
			MaxTokens:  16384,
			Available:  true,
		},
		// #20 - Claude Haiku 4.5 (457B tokens)
		{
			ID:         "anthropic/claude-haiku-4.5",
			Name:       "Claude Haiku 4.5",
			Provider:   "openrouter",
			ContextLen: 200000,
			InputCost:  0.80 / 1000000, // $0.80 per million tokens
			OutputCost: 4.00 / 1000000, // $4.00 per million tokens
			MaxTokens:  8192,
			Available:  true,
		},

		// === ADDITIONAL USEFUL MODELS ===

		// GPT-5 Mini (used in ensemble)
		{
			ID:         "openai/gpt-5-mini",
			Name:       "GPT-5 Mini",
			Provider:   "openrouter",
			ContextLen: 128000,
			InputCost:  0.20 / 1000000, // $0.20 per million tokens
			OutputCost: 0.80 / 1000000, // $0.80 per million tokens
			MaxTokens:  16384,
			Available:  true,
		},
		// Llama 4 Maverick (multimodal, 1M context)
		{
			ID:         "meta-llama/llama-4-maverick",
			Name:       "Llama 4 Maverick",
			Provider:   "openrouter",
			ContextLen: 1000000,
			InputCost:  0.20 / 1000000, // $0.20 per million tokens
			OutputCost: 0.60 / 1000000, // $0.60 per million tokens
			MaxTokens:  8192,
			Available:  true,
		},
		// Llama 4 Scout (10M context, efficient)
		{
			ID:         "meta-llama/llama-4-scout",
			Name:       "Llama 4 Scout",
			Provider:   "openrouter",
			ContextLen: 10000000,
			InputCost:  0.10 / 1000000, // $0.10 per million tokens
			OutputCost: 0.30 / 1000000, // $0.30 per million tokens
			MaxTokens:  8192,
			Available:  true,
		},
		// MiniMax M2.5 (successor to M2.1, 200K context)
		{
			ID:         "minimax/minimax-m2.5",
			Name:       "MiniMax M2.5",
			Provider:   "openrouter",
			ContextLen: 204800,
			InputCost:  0.30 / 1000000, // $0.30 per million tokens
			OutputCost: 1.20 / 1000000, // $1.20 per million tokens
			MaxTokens:  131072,
			Available:  true,
		},
		// GLM 5 (successor to GLM 4.7, 200K context)
		{
			ID:         "z-ai/glm-5",
			Name:       "GLM 5",
			Provider:   "openrouter",
			ContextLen: 202752,
			InputCost:  0.80 / 1000000, // $0.80 per million tokens
			OutputCost: 2.56 / 1000000, // $2.56 per million tokens
			MaxTokens:  131072,
			Available:  true,
		},
	}
}

// Complete performs a completion request to OpenRouter
func (p *OpenRouterProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	startTime := time.Now()

	// Prepare OpenRouter request (OpenAI-compatible format)
	openrouterReq := map[string]interface{}{
		"model":    req.Model,
		"messages": req.Messages,
	}

	if req.MaxTokens > 0 {
		openrouterReq["max_tokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		openrouterReq["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		openrouterReq["top_p"] = *req.TopP
	}
	if len(req.Stop) > 0 {
		openrouterReq["stop"] = req.Stop
	}
	if ext := req.Extensions; ext != nil {
		if ext.ResponseFormat != nil {
			openrouterReq["response_format"] = ext.ResponseFormat
		}
		if ext.Seed != nil {
			openrouterReq["seed"] = *ext.Seed
		}
		if ext.ProviderRouting != nil {
			openrouterReq["provider"] = ext.ProviderRouting
		}
		if ext.Reasoning != nil {
			openrouterReq["reasoning"] = ext.Reasoning
		}
		if ext.SessionID != "" {
			openrouterReq["session_id"] = ext.SessionID
		}
	}
	if len(req.Tools) > 0 {
		openrouterReq["tools"] = req.Tools
	}
	if req.ToolChoice != nil {
		openrouterReq["tool_choice"] = req.ToolChoice
	}
	if req.ParallelToolCalls != nil {
		openrouterReq["parallel_tool_calls"] = *req.ParallelToolCalls
	}

	// Marshal request
	reqBody, err := json.Marshal(openrouterReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Execute HTTP request with transient network retry (max 3 attempts).
	// Retries transient transport errors and transient response-read errors.
	// HTTP error responses (4xx, 5xx) are NOT retried here — those are handled by
	// the workflow engine/runtime to preserve one deterministic retry path.
	const maxNetworkRetries = 3
	var resp *http.Response
	var respBody []byte
	for attempt := 0; attempt < maxNetworkRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(100<<uint(attempt-1)) * time.Millisecond // 100ms, 200ms
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(reqBody))
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+p.config.APIKey)
		httpReq.Header.Set("HTTP-Referer", openRouterAttributionURL)
		httpReq.Header.Set("X-Title", openRouterTitle)
		httpReq.Header.Set("X-OpenRouter-Title", openRouterTitle)
		if req.Extensions != nil && req.Extensions.OpenRouterMetadata != nil && *req.Extensions.OpenRouterMetadata {
			httpReq.Header.Set("X-OpenRouter-Metadata", "enabled")
		}

		resp, err = p.httpClient.Do(httpReq)
		if err != nil {
			// Classify the network error
			if errors.Is(err, context.Canceled) {
				return nil, err
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, &ProviderError{
					Code:       ErrCodeUpstreamTimeout,
					Message:    fmt.Sprintf("request timed out: %v", err),
					Provider:   "openrouter",
					Model:      req.Model,
					ErrorPhase: "http_do",
					Retryable:  true,
				}
			}
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				return nil, &ProviderError{
					Code:       ErrCodeUpstreamTimeout,
					Message:    fmt.Sprintf("network timeout: %v", err),
					Provider:   "openrouter",
					Model:      req.Model,
					ErrorPhase: "http_do",
					Retryable:  true,
				}
			}
			transient := isTransientNetworkError(err)
			if transient && attempt < maxNetworkRetries-1 {
				continue // retry
			}
			return nil, &ProviderError{
				Code:       ErrCodeUpstreamError,
				Message:    fmt.Sprintf("failed to make request: %v", err),
				Provider:   "openrouter",
				Model:      req.Model,
				ErrorPhase: "http_do",
				Retryable:  transient,
			}
		}

		respBody, err = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			requestID := extractRequestID(resp.Header)
			generationID := extractGenerationID(resp.Header)
			if isTransientNetworkError(err) && attempt < maxNetworkRetries-1 {
				continue // retry full request on transient read error
			}

			if errors.Is(err, context.DeadlineExceeded) {
				return nil, &ProviderError{
					Code:         ErrCodeUpstreamTimeout,
					Message:      fmt.Sprintf("failed to read response: %v", err),
					Provider:     "openrouter",
					Model:        req.Model,
					RequestID:    requestID,
					GenerationID: generationID,
					ErrorPhase:   "http_read",
					Retryable:    true,
				}
			}
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				return nil, &ProviderError{
					Code:         ErrCodeUpstreamTimeout,
					Message:      fmt.Sprintf("failed to read response: %v", err),
					Provider:     "openrouter",
					Model:        req.Model,
					RequestID:    requestID,
					GenerationID: generationID,
					ErrorPhase:   "http_read",
					Retryable:    true,
				}
			}

			transient := isTransientNetworkError(err)
			return nil, &ProviderError{
				Code:         ErrCodeUpstreamError,
				Message:      fmt.Sprintf("failed to read response: %v", err),
				Provider:     "openrouter",
				Model:        req.Model,
				RequestID:    requestID,
				GenerationID: generationID,
				ErrorPhase:   "http_read",
				Retryable:    transient,
			}
		}
		break // success — got an HTTP response
	}
	if resp.StatusCode != http.StatusOK {
		var retryAfter time.Duration
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, parseErr := strconv.Atoi(ra); parseErr == nil && secs > 0 {
				retryAfter = time.Duration(secs) * time.Second
			}
		}
		return nil, p.parseErrorResponse(resp.StatusCode, respBody, req.Model, retryAfter, extractRequestID(resp.Header), extractGenerationID(resp.Header))
	}

	// Parse response (OpenAI-compatible format)
	var openrouterResp struct {
		ID                 string          `json:"id"`
		Model              string          `json:"model"`
		ServiceTier        string          `json:"service_tier"`
		OpenRouterMetadata json.RawMessage `json:"openrouter_metadata"`
		Choices            []struct {
			Message struct {
				Content          string          `json:"content"`
				ToolCalls        []ToolCall      `json:"tool_calls"`
				Reasoning        string          `json:"reasoning"`
				ReasoningDetails json.RawMessage `json:"reasoning_details"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage json.RawMessage `json:"usage"`
	}

	requestID := extractRequestID(resp.Header)
	generationID := extractGenerationID(resp.Header)
	if err := json.Unmarshal(respBody, &openrouterResp); err != nil {
		return nil, &ProviderError{
			Code:         ErrCodeUpstreamError,
			StatusCode:   http.StatusOK,
			Message:      fmt.Sprintf("failed to unmarshal response: %v", err),
			Provider:     "openrouter",
			Model:        req.Model,
			RequestID:    requestID,
			GenerationID: generationID,
			ErrorPhase:   "response_parse",
			Retryable:    true,
		}
	}

	// Check if we have choices
	if len(openrouterResp.Choices) == 0 {
		return nil, &ProviderError{
			Code:         ErrCodeNoChoices,
			StatusCode:   http.StatusOK,
			Message:      "no choices returned from OpenRouter",
			Provider:     "openrouter",
			Model:        req.Model,
			RequestID:    requestID,
			GenerationID: generationID,
			ErrorPhase:   "provider_response",
			Retryable:    true,
		}
	}

	var usage Usage
	if len(bytes.TrimSpace(openrouterResp.Usage)) > 0 && string(bytes.TrimSpace(openrouterResp.Usage)) != "null" {
		var parsedUsage struct {
			PromptTokens            int                      `json:"prompt_tokens"`
			CompletionTokens        int                      `json:"completion_tokens"`
			TotalTokens             int                      `json:"total_tokens"`
			Cost                    *float64                 `json:"cost,omitempty"`
			PromptTokensDetails     *PromptTokensDetails     `json:"prompt_tokens_details,omitempty"`
			CompletionTokensDetails *CompletionTokensDetails `json:"completion_tokens_details,omitempty"`
			CostDetails             map[string]interface{}   `json:"cost_details,omitempty"`
			IsBYOK                  *bool                    `json:"is_byok,omitempty"`
		}
		if err := json.Unmarshal(openrouterResp.Usage, &parsedUsage); err != nil {
			return nil, &ProviderError{
				Code:         ErrCodeUpstreamError,
				StatusCode:   http.StatusOK,
				Message:      fmt.Sprintf("failed to unmarshal usage: %v", err),
				Provider:     "openrouter",
				Model:        req.Model,
				RequestID:    requestID,
				GenerationID: generationID,
				ErrorPhase:   "response_parse",
				Retryable:    true,
			}
		}
		usage = Usage{
			PromptTokens:            parsedUsage.PromptTokens,
			CompletionTokens:        parsedUsage.CompletionTokens,
			TotalTokens:             parsedUsage.TotalTokens,
			Cost:                    parsedUsage.Cost,
			PromptTokensDetails:     parsedUsage.PromptTokensDetails,
			CompletionTokensDetails: parsedUsage.CompletionTokensDetails,
			CostDetails:             parsedUsage.CostDetails,
			IsBYOK:                  parsedUsage.IsBYOK,
			RawJSON:                 cloneRawJSON(openrouterResp.Usage),
		}
	}

	// Build our response. In tool-call mode prefer the selected function
	// over free-form content to keep benchmark contracts deterministic.
	content := openrouterResp.Choices[0].Message.Content
	if extracted, ok := extractToolCallAnswer(openrouterResp.Choices[0].Message.ToolCalls); ok {
		content = extracted
	}

	response := &CompletionResponse{
		ID:                 openrouterResp.ID,
		RequestID:          requestID,
		GenerationID:       generationID,
		Model:              openrouterResp.Model,
		Content:            content,
		ToolCalls:          openrouterResp.Choices[0].Message.ToolCalls,
		Usage:              usage,
		Finish:             openrouterResp.Choices[0].FinishReason,
		ServiceTier:        openrouterResp.ServiceTier,
		OpenRouterMetadata: cloneRawJSON(openrouterResp.OpenRouterMetadata),
		Reasoning:          openrouterResp.Choices[0].Message.Reasoning,
		ReasoningDetails:   cloneRawJSON(openrouterResp.Choices[0].Message.ReasoningDetails),
		Latency:            float64(time.Since(startTime).Milliseconds()),
	}

	return response, nil
}

func extractToolCallAnswer(toolCalls []ToolCall) (string, bool) {
	for _, call := range toolCalls {
		nameLabel, hasNameLabel := optionLabelFromToolName(call.Function.Name)

		argsJSON := strings.TrimSpace(call.Function.Arguments)
		if argsJSON == "" {
			if hasNameLabel {
				return nameLabel, true
			}
			continue
		}

		var args map[string]interface{}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			if hasNameLabel {
				return nameLabel, true
			}
			continue
		}

		for _, key := range []string{"answer", "benchmark_answer", "label", "choice"} {
			if raw, ok := args[key]; ok {
				answer := strings.TrimSpace(fmt.Sprintf("%v", raw))
				if answer != "" {
					return answer, true
				}
			}
		}
		if hasNameLabel {
			return nameLabel, true
		}
	}
	return "", false
}

func optionLabelFromToolName(name string) (string, bool) {
	upper := strings.ToUpper(strings.TrimSpace(name))
	if len(upper) == 1 && upper[0] >= 'A' && upper[0] <= 'Z' {
		return upper, true
	}

	for _, prefix := range []string{"SELECT_OPTION_", "OPTION_", "ANSWER_", "CHOICE_"} {
		if !strings.HasPrefix(upper, prefix) {
			continue
		}
		suffix := strings.TrimPrefix(upper, prefix)
		if len(suffix) == 1 && suffix[0] >= 'A' && suffix[0] <= 'Z' {
			return suffix, true
		}
	}

	if idx := strings.LastIndex(upper, "_"); idx >= 0 && idx < len(upper)-1 {
		suffix := upper[idx+1:]
		if len(suffix) == 1 && suffix[0] >= 'A' && suffix[0] <= 'Z' {
			return suffix, true
		}
	}

	return "", false
}

// parseErrorResponse builds a structured ProviderError from an OpenRouter API error response.
func (p *OpenRouterProvider) parseErrorResponse(statusCode int, body []byte, model string, retryAfter time.Duration, requestID, generationID string) *ProviderError {
	// Try to parse OpenRouter error body: {"error": {"code": ..., "message": ...}}
	var parsed struct {
		Error struct {
			Code     any    `json:"code"` // Can be int or string
			Message  string `json:"message"`
			Metadata struct {
				ErrorType    string `json:"error_type"`
				ProviderCode any    `json:"provider_code"`
			} `json:"metadata"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &parsed)

	pe := &ProviderError{
		StatusCode:   statusCode,
		Provider:     "openrouter",
		Model:        model,
		RetryAfter:   retryAfter,
		RequestID:    requestID,
		GenerationID: generationID,
		NativeCode:   stringifyOpenRouterValue(parsed.Error.Code),
		ErrorType:    parsed.Error.Metadata.ErrorType,
		ProviderCode: stringifyOpenRouterValue(parsed.Error.Metadata.ProviderCode),
		ErrorPhase:   "http_status",
	}

	switch statusCode {
	case http.StatusPaymentRequired: // 402
		pe.Code = ErrCodeInsufficientCredits
		pe.Message = "Insufficient OpenRouter credits"
		pe.Retryable = false
	case http.StatusTooManyRequests: // 429
		pe.Code = ErrCodeRateLimited
		pe.Message = "OpenRouter rate limit exceeded"
		pe.Retryable = true
	case http.StatusRequestTimeout: // 408
		pe.Code = ErrCodeUpstreamTimeout
		pe.Message = "OpenRouter upstream timeout"
		pe.Retryable = true
	case http.StatusBadRequest: // 400
		pe.Code = ErrCodeBadRequest
		pe.Message = "Bad request"
		pe.Retryable = false
	case http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity: // 413, 422
		pe.Code = ErrCodeBadRequest
		pe.Message = "Request rejected by OpenRouter"
		pe.Retryable = false
	case http.StatusUnauthorized, http.StatusForbidden: // 401, 403
		pe.Code = ErrCodeAuthError
		pe.Message = "OpenRouter authentication failed"
		pe.Retryable = false
	case http.StatusNotFound: // 404
		pe.Code = ErrCodeModelNotFound
		pe.Message = fmt.Sprintf("Model %q not found on OpenRouter", model)
		pe.Retryable = false
	case http.StatusBadGateway, http.StatusServiceUnavailable: // 502, 503
		pe.Code = ErrCodeUpstreamError
		pe.Message = "OpenRouter upstream unavailable"
		pe.Retryable = true
	case http.StatusGatewayTimeout: // 504
		pe.Code = ErrCodeUpstreamTimeout
		pe.Message = "OpenRouter upstream timeout"
		pe.Retryable = true
	default:
		pe.Code = ErrCodeUpstreamError
		pe.Message = fmt.Sprintf("OpenRouter error (status %d)", statusCode)
		pe.Retryable = statusCode >= 500
	}

	// Enrich with parsed error message from response body
	if parsed.Error.Message != "" {
		pe.Message = fmt.Sprintf("%s: %s", pe.Message, parsed.Error.Message)
	}

	return pe
}

func cloneRawJSON(raw json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	out := make([]byte, len(trimmed))
	copy(out, trimmed)
	return out
}

func stringifyOpenRouterValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return strings.TrimSpace(v.String())
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	default:
		return ""
	}
}

func extractRequestID(headers http.Header) string {
	if headers == nil {
		return ""
	}
	for _, key := range []string{
		"x-request-id",
		"request-id",
		"x-openrouter-request-id",
		"openrouter-request-id",
	} {
		if value := strings.TrimSpace(headers.Get(key)); value != "" {
			return value
		}
	}
	return ""
}

func extractGenerationID(headers http.Header) string {
	if headers == nil {
		return ""
	}
	for _, key := range []string{
		"x-generation-id",
		"generation-id",
		"x-openrouter-generation-id",
		"openrouter-generation-id",
	} {
		if value := strings.TrimSpace(headers.Get(key)); value != "" {
			return value
		}
	}
	return ""
}

// EstimateTokens provides a rough token count estimation.
// 1 token ≈ 4 characters for English text.
func (p *OpenRouterProvider) EstimateTokens(text string) int {
	return (len(text) + 3) / 4
}

// Cost calculates the cost for given token usage
func (p *OpenRouterProvider) Cost(model string, inputTokens, outputTokens int) float64 {
	models := p.Models()
	for _, m := range models {
		if m.ID == model {
			return float64(inputTokens)*m.InputCost + float64(outputTokens)*m.OutputCost
		}
	}
	return 0 // Unknown model
}

// isTransientNetworkError reports whether err is a transient network-level failure
// worth retrying (TCP reset, broken pipe, EOF, connection refused).
// Context cancellation and deadline errors are NOT considered transient.
func isTransientNetworkError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	msg := err.Error()
	for _, substr := range []string{"connection reset", "broken pipe", "EOF", "connection refused"} {
		if strings.Contains(msg, substr) {
			return true
		}
	}
	return false
}
