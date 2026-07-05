package providers

import (
	"reflect"
	"testing"
)

func TestParseOpenRouterModels_ParsesCapabilitiesAndSkipsNonNamespacedIDs(t *testing.T) {
	body := []byte(`{
		"data": [
			{
				"id": "openai/gpt-5.2",
				"name": "GPT-5.2",
				"context_length": 400000,
				"architecture": {
					"input_modalities": ["text", "image", "file"],
					"output_modalities": ["text"]
				},
				"supported_parameters": ["max_tokens", "tools", "tool_choice", "response_format", "structured_outputs"],
				"pricing": {
					"prompt": "0.00000125",
					"completion": "0.00001"
				},
				"top_provider": {
					"max_completion_tokens": 128000
				}
			},
			{
				"id": "gpt-5.2",
				"name": "Unnamespaced ID",
				"context_length": 1234,
				"pricing": {
					"prompt": "1",
					"completion": "1"
				},
				"top_provider": {
					"max_completion_tokens": 42
				}
			}
		]
	}`)

	models, err := parseOpenRouterModels(body)
	if err != nil {
		t.Fatalf("parseOpenRouterModels returned error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 parsed model, got %d", len(models))
	}

	got := models[0]
	if got.ID != "openai/gpt-5.2" {
		t.Fatalf("unexpected model ID: %q", got.ID)
	}
	if got.Provider != "openrouter" {
		t.Fatalf("unexpected provider: %q", got.Provider)
	}
	if got.ContextLen != 400000 {
		t.Fatalf("unexpected context length: %d", got.ContextLen)
	}
	if got.MaxTokens != 128000 {
		t.Fatalf("unexpected max tokens: %d", got.MaxTokens)
	}

	wantParams := []string{"max_tokens", "tools", "tool_choice", "response_format", "structured_outputs"}
	if !reflect.DeepEqual(got.SupportedParameters, wantParams) {
		t.Fatalf("supported parameters mismatch\ngot:  %#v\nwant: %#v", got.SupportedParameters, wantParams)
	}

	wantInput := []string{"text", "image", "file"}
	if !reflect.DeepEqual(got.InputModalities, wantInput) {
		t.Fatalf("input modalities mismatch\ngot:  %#v\nwant: %#v", got.InputModalities, wantInput)
	}

	wantOutput := []string{"text"}
	if !reflect.DeepEqual(got.OutputModalities, wantOutput) {
		t.Fatalf("output modalities mismatch\ngot:  %#v\nwant: %#v", got.OutputModalities, wantOutput)
	}
}

func TestParseOpenRouterModels_DefaultMaxTokensFallback(t *testing.T) {
	body := []byte(`{
		"data": [
			{
				"id": "anthropic/claude-sonnet-4.5",
				"name": "Claude Sonnet 4.5",
				"context_length": 1000000,
				"architecture": {
					"input_modalities": ["text"],
					"output_modalities": ["text"]
				},
				"supported_parameters": ["max_tokens"],
				"pricing": {
					"prompt": "0.000003",
					"completion": "0.000015"
				},
				"top_provider": {
					"max_completion_tokens": 0
				}
			}
		]
	}`)

	models, err := parseOpenRouterModels(body)
	if err != nil {
		t.Fatalf("parseOpenRouterModels returned error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 parsed model, got %d", len(models))
	}
	if models[0].MaxTokens != 4096 {
		t.Fatalf("expected default max tokens 4096, got %d", models[0].MaxTokens)
	}
}
