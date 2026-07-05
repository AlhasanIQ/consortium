package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/workflow"
)

func TestSubmitWorkflowPayloadPassesStrictWorkflowValidation(t *testing.T) {
	validator := workflow.NewValidator(nil)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/workflows/submit" {
			http.NotFound(w, r)
			return
		}
		defer r.Body.Close()

		var body struct {
			Workflow workflow.Workflow `json:"workflow"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if result := validator.Validate(&body.Workflow); !result.Valid {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(result)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"job_id":    "job-loadtest",
			"duplicate": false,
		})
	}))
	defer server.Close()

	worker := NewWorker(1, &Config{BaseURL: server.URL}, NewMetrics())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	jobID, duplicate, err := worker.submitWorkflow(ctx, "loadtest prompt", "loadtest-test-key")
	if err != nil {
		t.Fatalf("submitWorkflow returned error for validator-backed handler: %v", err)
	}
	if duplicate {
		t.Fatal("submitWorkflow duplicate = true, want false")
	}
	if jobID != "job-loadtest" {
		t.Fatalf("submitWorkflow jobID = %q, want job-loadtest", jobID)
	}
}

func TestSubmitWorkflowReportsValidationResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"bad workflow"}`, http.StatusBadRequest)
	}))
	defer server.Close()

	worker := NewWorker(1, &Config{BaseURL: server.URL}, NewMetrics())
	_, _, err := worker.submitWorkflow(context.Background(), "loadtest prompt", "loadtest-test-key")
	if err == nil {
		t.Fatal("submitWorkflow error = nil, want status error")
	}
	if got, want := err.Error(), fmt.Sprintf("submit returned status %d: %s", http.StatusBadRequest, `{"error":"bad workflow"}`+"\n"); got != want {
		t.Fatalf("submitWorkflow error = %q, want %q", got, want)
	}
}

func TestRunOpenAIScenarioExercisesProductionGateEndpoints(t *testing.T) {
	var mu sync.Mutex
	seen := make(map[string]int)
	idempotencyHits := make(map[string]int)
	nextBackgroundID := 0

	mark := func(r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		seen[r.Method+" "+r.URL.Path]++
		if idem := strings.TrimSpace(r.Header.Get("Idempotency-Key")); idem != "" {
			idempotencyHits[idem]++
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mark(r)
		if got := r.Header.Get("Authorization"); got != "Bearer test-openai-key" {
			t.Errorf("Authorization = %q, want bearer test key", got)
			http.Error(w, "bad auth", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"object": "list",
				"data":   []map[string]any{{"id": "gpt-test", "object": "model"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/chat/completions":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["model"] != "gpt-test" {
				t.Errorf("chat model = %v, want gpt-test", body["model"])
			}
			if body["stream"] == true {
				writeTestSSE(w, []string{
					`{"choices":[{"delta":{"content":"ok"}}]}`,
					`[DONE]`,
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":     "chatcmpl-openai-loadtest",
				"object": "chat.completion",
				"choices": []map[string]any{{
					"index":         0,
					"finish_reason": "stop",
					"message":       map[string]any{"role": "assistant", "content": "ok"},
				}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/responses":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["model"] != "gpt-test" {
				t.Errorf("responses model = %v, want gpt-test", body["model"])
			}
			switch {
			case body["stream"] == true:
				writeTestSSE(w, []string{
					`{"type":"response.created"}`,
					`{"type":"response.completed"}`,
					`[DONE]`,
				})
			case body["background"] == true:
				nextBackgroundID++
				id := "resp-background"
				if nextBackgroundID > 1 {
					id = "resp-cancel"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id":         id,
					"object":     "response",
					"status":     "in_progress",
					"background": true,
				})
			default:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id":          "resp-openai-loadtest",
					"object":      "response",
					"status":      "completed",
					"output_text": "ok",
				})
			}
		case r.Method == http.MethodGet && r.URL.Path == "/v1/responses/resp-background":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":          "resp-background",
				"object":      "response",
				"status":      "completed",
				"background":  true,
				"output_text": "background ok",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/responses/resp-background/input_items":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"object": "list",
				"data":   []map[string]any{{"id": "item-input", "type": "message"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/responses/resp-cancel/cancel":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         "resp-cancel",
				"object":     "response",
				"status":     "cancelled",
				"background": true,
			})
		default:
			body, _ := io.ReadAll(r.Body)
			t.Errorf("unexpected %s %s body=%s", r.Method, r.URL.Path, string(body))
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	metrics := NewMetrics()
	worker := NewWorker(1, &Config{
		BaseURL:      server.URL,
		Scenario:     ScenarioOpenAI,
		OpenAIAPIKey: "test-openai-key",
		OpenAIModel:  "gpt-test",
	}, metrics)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go worker.Run(ctx, &wg)
	wg.Wait()

	if errors := atomic.LoadInt64(&metrics.ErrorCount); errors != 0 {
		t.Fatalf("ErrorCount = %d, want 0", errors)
	}
	if successes := atomic.LoadInt64(&metrics.SuccessCount); successes != 1 {
		t.Fatalf("SuccessCount = %d, want 1", successes)
	}

	for _, key := range []string{
		"GET /v1/models",
		"POST /v1/chat/completions",
		"POST /v1/responses",
		"GET /v1/responses/resp-background",
		"GET /v1/responses/resp-background/input_items",
		"POST /v1/responses/resp-cancel/cancel",
	} {
		if seen[key] == 0 {
			t.Fatalf("endpoint %s was not exercised; seen=%v", key, seen)
		}
	}
	foundReplay := false
	for _, hits := range idempotencyHits {
		if hits == 2 {
			foundReplay = true
			break
		}
	}
	if !foundReplay {
		t.Fatalf("no idempotency key was replayed exactly twice: %v", idempotencyHits)
	}
}

func writeTestSSE(w http.ResponseWriter, events []string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, event := range events {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
	}
}
