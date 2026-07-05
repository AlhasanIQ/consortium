package conctl

import (
	"bytes"
	"strings"
	"testing"

	"github.com/alhasaniq/consortium/internal/conctl/app"
)

func TestAPIKeysTableDoesNotRenderSecrets(t *testing.T) {
	data := map[string]interface{}{
		"api_keys": []interface{}{
			map[string]interface{}{
				"id":                  "key-1",
				"name":                "CI",
				"prefix":              "sk-consortium-abc",
				"user_id":             "system",
				"requests_per_minute": float64(7),
				"tokens_per_minute":   float64(8000),
				"key":                 "secret",
				"key_hash":            "sha256:secret",
			},
		},
	}
	var buf bytes.Buffer
	apiKeysTable(&buf, data, "md")
	got := buf.String()
	if strings.Contains(got, "secret") || strings.Contains(got, "key_hash") {
		t.Fatalf("table leaked secret/hash:\n%s", got)
	}
	if !strings.Contains(got, "sk-consortium-abc") {
		t.Fatalf("table missing safe prefix:\n%s", got)
	}
}

func TestAPIKeyCreateRequiresYes(t *testing.T) {
	cmd := apiKeyCreateCmd()
	code := cmd.Run(app.GlobalFlags{URL: "http://127.0.0.1:1", Timeout: "1s", Format: "json", Output: "-"}, []string{"--name", "CI"})
	if code != app.ExitUsage {
		t.Fatalf("exit = %d, want usage when --yes missing", code)
	}
}

func TestAPIRoutesTableRendersWorkflowAndDirectTargets(t *testing.T) {
	data := map[string]interface{}{
		"model_routes": []interface{}{
			map[string]interface{}{
				"api_model":      "gpt-test",
				"mode":           "direct_model",
				"provider_model": "mock-model",
				"is_default":     true,
				"enabled":        true,
			},
			map[string]interface{}{
				"api_model":   "consortium",
				"mode":        "workflow",
				"workflow_id": "wf-default",
				"enabled":     true,
			},
		},
	}
	var buf bytes.Buffer
	apiRoutesTable(&buf, data, "table")
	got := buf.String()
	for _, want := range []string{"gpt-test", "mock-model", "consortium", "wf-default"} {
		if !strings.Contains(got, want) {
			t.Fatalf("table missing %q:\n%s", want, got)
		}
	}
}

func TestAPIMetricsTableRendersLifecycleSignals(t *testing.T) {
	data := map[string]interface{}{
		"openai_api_metrics": map[string]interface{}{
			"usage": map[string]interface{}{
				"requests":     float64(3),
				"tokens_total": float64(42),
				"cost":         float64(0.25),
			},
			"requests_by_status": map[string]interface{}{
				"succeeded": float64(2),
				"failed":    float64(1),
			},
			"stale_running_usage":        float64(4),
			"stale_background_responses": float64(5),
			"pending_idempotency":        float64(6),
		},
	}
	var buf bytes.Buffer
	apiMetricsTable(&buf, data, "table")
	got := buf.String()
	for _, want := range []string{"Requests", "3", "Tokens", "42", "Succeeded", "2", "Failed", "1", "Stale Running Usage", "4", "Stale Background Responses", "5", "Pending Idempotency", "6"} {
		if !strings.Contains(got, want) {
			t.Fatalf("metrics table missing %q:\n%s", want, got)
		}
	}
}
