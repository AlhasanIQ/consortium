package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const defaultCacheTTL = 1 * time.Hour

// ModelCache provides a TTL-based cache for the OpenRouter model catalog.
// It fetches from the OpenRouter /api/v1/models endpoint and falls back
// to a static list when the API is unreachable.
type ModelCache struct {
	mu          sync.RWMutex
	models      []Model
	lastRefresh time.Time
	ttl         time.Duration
	apiKey      string
	baseURL     string
	httpClient  *http.Client
	fallback    []Model
	refreshing  atomic.Bool
}

// NewModelCache creates a model cache with the given fallback list.
func NewModelCache(apiKey, baseURL string, ttl time.Duration, fallback []Model) *ModelCache {
	if ttl == 0 {
		ttl = defaultCacheTTL
	}
	return &ModelCache{
		models:     fallback,
		ttl:        ttl,
		apiKey:     apiKey,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		fallback:   fallback,
	}
}

// Models returns the cached model list. If stale, triggers a background refresh.
// Never blocks — always returns current (possibly stale) data.
func (mc *ModelCache) Models() []Model {
	mc.mu.RLock()
	stale := time.Since(mc.lastRefresh) > mc.ttl
	models := make([]Model, len(mc.models))
	copy(models, mc.models)
	mc.mu.RUnlock()

	if stale && mc.refreshing.CompareAndSwap(false, true) {
		go func() {
			defer mc.refreshing.Store(false)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := mc.Refresh(ctx); err != nil {
				log.Printf("WARNING: Model cache refresh failed: %v", err)
			}
		}()
	}

	return models
}

// Refresh fetches the latest model list from the OpenRouter models API.
// On failure, the existing cache is preserved.
func (mc *ModelCache) Refresh(ctx context.Context) error {
	url := mc.baseURL + "/models"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+mc.apiKey)

	resp, err := mc.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("models API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	models, err := parseOpenRouterModels(body)
	if err != nil {
		return fmt.Errorf("parse models: %w", err)
	}

	if len(models) == 0 {
		return fmt.Errorf("models API returned empty list")
	}

	mc.mu.Lock()
	mc.models = models
	mc.lastRefresh = time.Now()
	mc.mu.Unlock()

	log.Printf("Model cache refreshed: %d models loaded", len(models))
	return nil
}

// openRouterModelsResponse matches the OpenRouter /api/v1/models response.
type openRouterModelsResponse struct {
	Data []openRouterModel `json:"data"`
}

type openRouterModel struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextLength int    `json:"context_length"`
	Architecture  struct {
		InputModalities  []string `json:"input_modalities"`
		OutputModalities []string `json:"output_modalities"`
	} `json:"architecture"`
	SupportedParameters []string `json:"supported_parameters"`
	Pricing             struct {
		Prompt     string `json:"prompt"`     // per-token cost as string
		Completion string `json:"completion"` // per-token cost as string
	} `json:"pricing"`
	TopProvider struct {
		MaxCompletionTokens int `json:"max_completion_tokens"`
	} `json:"top_provider"`
}

func parseOpenRouterModels(body []byte) ([]Model, error) {
	var resp openRouterModelsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	models := make([]Model, 0, len(resp.Data))
	for _, m := range resp.Data {
		// Skip models without a namespaced ID (provider/model format)
		if !strings.Contains(m.ID, "/") {
			continue
		}

		inputCost, _ := strconv.ParseFloat(m.Pricing.Prompt, 64)
		outputCost, _ := strconv.ParseFloat(m.Pricing.Completion, 64)

		maxTokens := m.TopProvider.MaxCompletionTokens
		if maxTokens == 0 {
			maxTokens = 4096
		}

		models = append(models, Model{
			ID:                  m.ID,
			Name:                m.Name,
			Provider:            "openrouter",
			ContextLen:          m.ContextLength,
			InputCost:           inputCost,
			OutputCost:          outputCost,
			MaxTokens:           maxTokens,
			Available:           true,
			SupportedParameters: append([]string(nil), m.SupportedParameters...),
			InputModalities:     append([]string(nil), m.Architecture.InputModalities...),
			OutputModalities:    append([]string(nil), m.Architecture.OutputModalities...),
		})
	}

	return models, nil
}
