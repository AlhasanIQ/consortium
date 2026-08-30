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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	openAICompatibleProviderName = "openai-compatible"
	// OpenAICompatibleModelPrefix keeps models from a generic compatibility
	// endpoint distinct from native provider catalogs such as OpenRouter.
	OpenAICompatibleModelPrefix = "compatible/"
)

// OpenAICompatibleConfig configures a generic OpenAI Chat Completions endpoint.
// BaseURL should point at the API root that contains /models and
// /chat/completions (for example http://127.0.0.1:11434/v1 for Ollama).
// APIKey is optional because many local/self-hosted endpoints do not require one.
// Models is an optional fallback catalog for endpoints that do not expose /models.
type OpenAICompatibleConfig struct {
	BaseURL string
	APIKey  string
	Timeout time.Duration
	Models  []string
}

// OpenAICompatibleProvider connects Consortium to an OpenAI-compatible Chat
// Completions API such as Ollama, LM Studio, vLLM, or another compatible server.
// Public model IDs are namespaced as compatible/<upstream-model-id> so they can
// coexist with OpenRouter models without ambiguous registry lookups.
type OpenAICompatibleProvider struct {
	config     OpenAICompatibleConfig
	httpClient *http.Client
	baseURL    string

	mu             sync.RWMutex
	models         []Model
	fallbackModels []Model
}

// NewOpenAICompatibleProvider creates a provider with an optional static model
// fallback. Call RefreshModels to discover the endpoint's live /models catalog.
func NewOpenAICompatibleProvider(config OpenAICompatibleConfig) *OpenAICompatibleProvider {
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	config.Timeout = timeout
	config.BaseURL = strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	config.APIKey = strings.TrimSpace(config.APIKey)

	fallback := compatibleModelsFromIDs(config.Models)
	return &OpenAICompatibleProvider{
		config:         config,
		httpClient:     &http.Client{Timeout: timeout},
		baseURL:        config.BaseURL,
		models:         append([]Model(nil), fallback...),
		fallbackModels: append([]Model(nil), fallback...),
	}
}

func (p *OpenAICompatibleProvider) Name() string { return openAICompatibleProviderName }

// Models returns the currently known model catalog. The returned slice is a copy
// so callers cannot mutate provider state.
func (p *OpenAICompatibleProvider) Models() []Model {
	p.mu.RLock()
	defer p.mu.RUnlock()
	models := make([]Model, len(p.models))
	copy(models, p.models)
	return models
}

// RefreshModels fetches the standard OpenAI-compatible /models endpoint and
// merges the discovered catalog with explicitly configured fallback models.
// Existing models are preserved when refresh fails.
func (p *OpenAICompatibleProvider) RefreshModels(ctx context.Context) error {
	if p.baseURL == "" {
		return fmt.Errorf("OpenAI-compatible base URL is empty")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return fmt.Errorf("create models request: %w", err)
	}
	if p.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.config.APIKey)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch compatible models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("compatible models API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read compatible models response: %w", err)
	}
	discovered, err := parseOpenAICompatibleModels(body)
	if err != nil {
		return fmt.Errorf("parse compatible models: %w", err)
	}
	if len(discovered) == 0 && len(p.fallbackModels) == 0 {
		return fmt.Errorf("compatible models API returned empty list")
	}

	merged := make(map[string]Model, len(discovered)+len(p.fallbackModels))
	for _, model := range p.fallbackModels {
		merged[model.ID] = model
	}
	for _, model := range discovered {
		merged[model.ID] = model
	}
	models := make([]Model, 0, len(merged))
	for _, model := range merged {
		models = append(models, model)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })

	p.mu.Lock()
	p.models = models
	p.mu.Unlock()
	return nil
}

// Complete performs a standard OpenAI-compatible Chat Completions request.
func (p *OpenAICompatibleProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	startTime := time.Now()
	if p.baseURL == "" {
		return nil, p.badRequest(req.Model, "OpenAI-compatible base URL is empty")
	}
	upstreamModel, ok := compatibleUpstreamModelID(req.Model)
	if !ok {
		return nil, p.badRequest(req.Model, fmt.Sprintf("model %q must use the %q prefix", req.Model, OpenAICompatibleModelPrefix))
	}

	payload := map[string]interface{}{
		"model":    upstreamModel,
		"messages": req.Messages,
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		payload["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		payload["top_p"] = *req.TopP
	}
	if len(req.Stop) > 0 {
		payload["stop"] = req.Stop
	}
	if ext := req.Extensions; ext != nil {
		// response_format and seed are part of the OpenAI-compatible request
		// surface. OpenRouter-only routing/reasoning/session controls are
		// intentionally not forwarded to generic endpoints.
		if ext.ResponseFormat != nil {
			payload["response_format"] = ext.ResponseFormat
		}
		if ext.Seed != nil {
			payload["seed"] = *ext.Seed
		}
	}
	if len(req.Tools) > 0 {
		payload["tools"] = req.Tools
	}
	if req.ToolChoice != nil {
		payload["tool_choice"] = req.ToolChoice
	}
	if req.ParallelToolCalls != nil {
		payload["parallel_tool_calls"] = *req.ParallelToolCalls
	}

	reqBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal compatible request: %w", err)
	}

	resp, respBody, err := p.doChatRequest(ctx, reqBody, req.Model)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, p.parseErrorResponse(resp.StatusCode, respBody, req.Model, parseRetryAfter(resp.Header.Get("Retry-After")), extractRequestID(resp.Header))
	}

	var parsed struct {
		ID          string          `json:"id"`
		Model       string          `json:"model"`
		ServiceTier string          `json:"service_tier"`
		Choices     []struct {
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
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, &ProviderError{
			Code:       ErrCodeUpstreamError,
			StatusCode: http.StatusOK,
			Message:    fmt.Sprintf("failed to unmarshal compatible response: %v", err),
			Provider:   p.Name(),
			Model:      req.Model,
			RequestID:  requestID,
			ErrorPhase: "response_parse",
			Retryable:  true,
		}
	}
	if len(parsed.Choices) == 0 {
		return nil, &ProviderError{
			Code:       ErrCodeNoChoices,
			StatusCode: http.StatusOK,
			Message:    "no choices returned from OpenAI-compatible endpoint",
			Provider:   p.Name(),
			Model:      req.Model,
			RequestID:  requestID,
			ErrorPhase: "provider_response",
			Retryable:  true,
		}
	}

	usage, err := parseCompatibleUsage(parsed.Usage)
	if err != nil {
		return nil, &ProviderError{
			Code:       ErrCodeUpstreamError,
			StatusCode: http.StatusOK,
			Message:    fmt.Sprintf("failed to unmarshal compatible usage: %v", err),
			Provider:   p.Name(),
			Model:      req.Model,
			RequestID:  requestID,
			ErrorPhase: "response_parse",
			Retryable:  true,
		}
	}

	choice := parsed.Choices[0]
	content := choice.Message.Content
	if extracted, ok := extractToolCallAnswer(choice.Message.ToolCalls); ok {
		content = extracted
	}
	responseModel := strings.TrimSpace(parsed.Model)
	if responseModel == "" {
		responseModel = upstreamModel
	}

	return &CompletionResponse{
		ID:               parsed.ID,
		RequestID:        requestID,
		Model:            compatiblePublicModelID(responseModel),
		Content:          content,
		ToolCalls:        choice.Message.ToolCalls,
		Usage:            usage,
		Finish:           choice.FinishReason,
		ServiceTier:      parsed.ServiceTier,
		Reasoning:        choice.Message.Reasoning,
		ReasoningDetails: cloneRawJSON(choice.Message.ReasoningDetails),
		Latency:          float64(time.Since(startTime).Milliseconds()),
	}, nil
}

func (p *OpenAICompatibleProvider) doChatRequest(ctx context.Context, reqBody []byte, model string) (*http.Response, []byte, error) {
	const maxNetworkRetries = 3
	for attempt := 0; attempt < maxNetworkRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(100<<uint(attempt-1)) * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(reqBody))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create compatible request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if p.config.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+p.config.APIKey)
		}

		resp, err := p.httpClient.Do(httpReq)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil, nil, err
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, nil, p.transportError(ErrCodeUpstreamTimeout, model, "http_do", fmt.Sprintf("request timed out: %v", err), true)
			}
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				return nil, nil, p.transportError(ErrCodeUpstreamTimeout, model, "http_do", fmt.Sprintf("network timeout: %v", err), true)
			}
			transient := isTransientNetworkError(err)
			if transient && attempt < maxNetworkRetries-1 {
				continue
			}
			return nil, nil, p.transportError(ErrCodeUpstreamError, model, "http_do", fmt.Sprintf("failed to make request: %v", err), transient)
		}

		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			requestID := extractRequestID(resp.Header)
			if isTransientNetworkError(readErr) && attempt < maxNetworkRetries-1 {
				continue
			}
			code := ErrCodeUpstreamError
			retryable := isTransientNetworkError(readErr)
			if errors.Is(readErr, context.DeadlineExceeded) {
				code = ErrCodeUpstreamTimeout
				retryable = true
			} else {
				var netErr net.Error
				if errors.As(readErr, &netErr) && netErr.Timeout() {
					code = ErrCodeUpstreamTimeout
					retryable = true
				}
			}
			pe := p.transportError(code, model, "http_read", fmt.Sprintf("failed to read response: %v", readErr), retryable)
			pe.RequestID = requestID
			return nil, nil, pe
		}
		return resp, body, nil
	}
	return nil, nil, p.transportError(ErrCodeUpstreamError, model, "http_do", "compatible request retries exhausted", true)
}

func (p *OpenAICompatibleProvider) transportError(code, model, phase, message string, retryable bool) *ProviderError {
	return &ProviderError{
		Code:       code,
		Message:    message,
		Provider:   p.Name(),
		Model:      model,
		ErrorPhase: phase,
		Retryable:  retryable,
	}
}

func (p *OpenAICompatibleProvider) badRequest(model, message string) *ProviderError {
	return &ProviderError{
		Code:       ErrCodeBadRequest,
		StatusCode: http.StatusBadRequest,
		Message:    message,
		Provider:   p.Name(),
		Model:      model,
		ErrorPhase: "request_validation",
		Retryable:  false,
	}
}

func (p *OpenAICompatibleProvider) parseErrorResponse(statusCode int, body []byte, model string, retryAfter time.Duration, requestID string) *ProviderError {
	var parsed struct {
		Error struct {
			Code    any    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &parsed)
	message := strings.TrimSpace(parsed.Error.Message)
	if message == "" {
		message = strings.TrimSpace(parsed.Message)
	}

	pe := &ProviderError{
		StatusCode:   statusCode,
		Provider:     p.Name(),
		Model:        model,
		RetryAfter:   retryAfter,
		RequestID:    requestID,
		NativeCode:   stringifyOpenRouterValue(parsed.Error.Code),
		ProviderCode: stringifyOpenRouterValue(parsed.Error.Code),
		ErrorPhase:   "http_status",
	}
	switch statusCode {
	case http.StatusPaymentRequired:
		pe.Code = ErrCodeInsufficientCredits
		pe.Message = "OpenAI-compatible endpoint reported insufficient credits"
		pe.Retryable = false
	case http.StatusTooManyRequests:
		pe.Code = ErrCodeRateLimited
		pe.Message = "OpenAI-compatible endpoint rate limit exceeded"
		pe.Retryable = true
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		pe.Code = ErrCodeUpstreamTimeout
		pe.Message = "OpenAI-compatible endpoint timed out"
		pe.Retryable = true
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		pe.Code = ErrCodeBadRequest
		pe.Message = "OpenAI-compatible endpoint rejected the request"
		pe.Retryable = false
	case http.StatusUnauthorized, http.StatusForbidden:
		pe.Code = ErrCodeAuthError
		pe.Message = "OpenAI-compatible endpoint authentication failed"
		pe.Retryable = false
	case http.StatusNotFound:
		pe.Code = ErrCodeModelNotFound
		pe.Message = fmt.Sprintf("model %q was not found by the OpenAI-compatible endpoint", model)
		pe.Retryable = false
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		pe.Code = ErrCodeUpstreamError
		pe.Message = "OpenAI-compatible endpoint unavailable"
		pe.Retryable = true
	default:
		pe.Code = ErrCodeUpstreamError
		pe.Message = fmt.Sprintf("OpenAI-compatible endpoint error (status %d)", statusCode)
		pe.Retryable = statusCode >= 500
	}
	if message != "" {
		pe.Message += ": " + message
	}
	return pe
}

// EstimateTokens provides the same lightweight approximation as the native
// OpenRouter provider. Endpoint-specific tokenizers remain the source of truth.
func (p *OpenAICompatibleProvider) EstimateTokens(text string) int {
	return (len(text) + 3) / 4
}

// Cost returns the configured catalog estimate. Generic/local model discovery
// does not provide pricing, so discovered models default to zero unless a future
// endpoint-specific catalog supplies prices. Provider-reported usage.cost, when
// present, is preserved on CompletionResponse and takes precedence in Client.
func (p *OpenAICompatibleProvider) Cost(model string, inputTokens, outputTokens int) float64 {
	for _, candidate := range p.Models() {
		if candidate.ID == model {
			return float64(inputTokens)*candidate.InputCost + float64(outputTokens)*candidate.OutputCost
		}
	}
	return 0
}

func parseOpenAICompatibleModels(body []byte) ([]Model, error) {
	var response struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(response.Data))
	for _, item := range response.Data {
		ids = append(ids, item.ID)
	}
	return compatibleModelsFromIDs(ids), nil
}

func compatibleModelsFromIDs(ids []string) []Model {
	seen := make(map[string]struct{}, len(ids))
	models := make([]Model, 0, len(ids))
	for _, raw := range ids {
		upstreamID := strings.TrimSpace(raw)
		upstreamID = strings.TrimPrefix(upstreamID, OpenAICompatibleModelPrefix)
		upstreamID = strings.TrimSpace(upstreamID)
		if upstreamID == "" {
			continue
		}
		publicID := compatiblePublicModelID(upstreamID)
		if _, exists := seen[publicID]; exists {
			continue
		}
		seen[publicID] = struct{}{}
		models = append(models, Model{
			ID:        publicID,
			Name:      upstreamID,
			Provider:  openAICompatibleProviderName,
			Available: true,
		})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models
}

func compatiblePublicModelID(upstreamID string) string {
	upstreamID = strings.TrimPrefix(strings.TrimSpace(upstreamID), OpenAICompatibleModelPrefix)
	return OpenAICompatibleModelPrefix + upstreamID
}

func compatibleUpstreamModelID(publicID string) (string, bool) {
	publicID = strings.TrimSpace(publicID)
	if !strings.HasPrefix(publicID, OpenAICompatibleModelPrefix) {
		return "", false
	}
	upstream := strings.TrimSpace(strings.TrimPrefix(publicID, OpenAICompatibleModelPrefix))
	return upstream, upstream != ""
}

func parseCompatibleUsage(raw json.RawMessage) (Usage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return Usage{}, nil
	}
	var parsed struct {
		PromptTokens            int                      `json:"prompt_tokens"`
		CompletionTokens        int                      `json:"completion_tokens"`
		TotalTokens             int                      `json:"total_tokens"`
		Cost                    *float64                 `json:"cost,omitempty"`
		PromptTokensDetails     *PromptTokensDetails     `json:"prompt_tokens_details,omitempty"`
		CompletionTokensDetails *CompletionTokensDetails `json:"completion_tokens_details,omitempty"`
		CostDetails             map[string]interface{}   `json:"cost_details,omitempty"`
	}
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return Usage{}, err
	}
	return Usage{
		PromptTokens:            parsed.PromptTokens,
		CompletionTokens:        parsed.CompletionTokens,
		TotalTokens:             parsed.TotalTokens,
		Cost:                    parsed.Cost,
		PromptTokensDetails:     parsed.PromptTokensDetails,
		CompletionTokensDetails: parsed.CompletionTokensDetails,
		CostDetails:             parsed.CostDetails,
		RawJSON:                 cloneRawJSON(trimmed),
	}, nil
}

func parseRetryAfter(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
		return 0
	}
	if when, err := http.ParseTime(value); err == nil {
		if delay := time.Until(when); delay > 0 {
			return delay
		}
	}
	return 0
}
