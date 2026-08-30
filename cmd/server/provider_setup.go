package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/pkg/providers"
)

const compatibleCatalogRefreshTimeout = 5 * time.Second

// registerConfiguredProviders wires every provider explicitly configured by the
// operator. OpenRouter remains fully supported, but is no longer the only way to
// run real workflows: an OpenAI-compatible endpoint can be used on its own.
func registerConfiguredProviders(ctx context.Context, registry *providers.Registry) error {
	registered := 0

	if openrouterKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")); openrouterKey != "" {
		registry.Register(providers.NewOpenRouterProvider(providers.ProviderConfig{
			Name:    "openrouter",
			APIKey:  openrouterKey,
			Timeout: 10 * time.Minute,
		}))
		registered++
		log.Println("✅ Registered OpenRouter provider")
	}

	if baseURL := strings.TrimSpace(os.Getenv("OPENAI_COMPATIBLE_BASE_URL")); baseURL != "" {
		fallbackModels := parseCompatibleModelList(os.Getenv("OPENAI_COMPATIBLE_MODELS"))
		compatible := providers.NewOpenAICompatibleProvider(providers.OpenAICompatibleConfig{
			BaseURL: baseURL,
			APIKey:  strings.TrimSpace(os.Getenv("OPENAI_COMPATIBLE_API_KEY")),
			Timeout: 10 * time.Minute,
			Models:  fallbackModels,
		})

		refreshCtx, cancel := context.WithTimeout(ctx, compatibleCatalogRefreshTimeout)
		refreshErr := compatible.RefreshModels(refreshCtx)
		cancel()
		if refreshErr != nil {
			if len(fallbackModels) == 0 {
				return fmt.Errorf("OpenAI-compatible provider model discovery failed and OPENAI_COMPATIBLE_MODELS is empty: %w", refreshErr)
			}
			log.Printf("⚠️  OpenAI-compatible model discovery failed; using %d configured fallback model(s): %v", len(fallbackModels), refreshErr)
		}

		registry.Register(compatible)
		registered++
		log.Printf("✅ Registered OpenAI-compatible provider (%d model(s), public prefix %q)", len(compatible.Models()), providers.OpenAICompatibleModelPrefix)
	}

	if registered == 0 {
		return fmt.Errorf("no LLM provider configured: set OPENROUTER_API_KEY or OPENAI_COMPATIBLE_BASE_URL")
	}
	return nil
}

func parseCompatibleModelList(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})
	seen := make(map[string]struct{}, len(parts))
	models := make([]string, 0, len(parts))
	for _, part := range parts {
		model := strings.TrimSpace(part)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		models = append(models, model)
	}
	return models
}
