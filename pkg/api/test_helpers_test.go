package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/jobs"
	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type MockExecuteProvider struct {
	name        string
	models      []providers.Model
	response    string
	shouldError bool
	errorMsg    string
	err         error
	failCount   int
	toolCalls   []providers.ToolCall
	delay       time.Duration
	mu          sync.Mutex
	callCount   int
	lastRequest *providers.CompletionRequest
}

func (m *MockExecuteProvider) Name() string              { return m.name }
func (m *MockExecuteProvider) Models() []providers.Model { return m.models }
func (m *MockExecuteProvider) EstimateTokens(text string) int {
	return len(text) / 4
}
func (m *MockExecuteProvider) Cost(model string, inputTokens, outputTokens int) float64 {
	return float64(inputTokens)*0.000001 + float64(outputTokens)*0.000002
}

func (m *MockExecuteProvider) Complete(ctx context.Context, req *providers.CompletionRequest) (*providers.CompletionResponse, error) {
	copied := *req
	m.mu.Lock()
	m.lastRequest = &copied
	m.callCount++
	shouldError := m.shouldError
	if m.failCount > 0 {
		m.failCount--
		shouldError = true
	}
	errorMsg := m.errorMsg
	errValue := m.err
	delay := m.delay
	m.mu.Unlock()

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if shouldError {
		if errValue != nil {
			return nil, errValue
		}
		return nil, fmt.Errorf("%s", errorMsg)
	}

	return &providers.CompletionResponse{
		Content:   m.response,
		Model:     req.Model,
		ToolCalls: m.toolCalls,
		Usage: providers.Usage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}, nil
}

func (m *MockExecuteProvider) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

type SlowMockProvider struct {
	name   string
	models []providers.Model
	delay  time.Duration
}

func (m *SlowMockProvider) Name() string              { return m.name }
func (m *SlowMockProvider) Models() []providers.Model { return m.models }
func (m *SlowMockProvider) EstimateTokens(text string) int {
	return len(text) / 4
}
func (m *SlowMockProvider) Cost(model string, inputTokens, outputTokens int) float64 {
	return float64(inputTokens)*0.000001 + float64(outputTokens)*0.000002
}
func (m *SlowMockProvider) Complete(ctx context.Context, req *providers.CompletionRequest) (*providers.CompletionResponse, error) {
	select {
	case <-time.After(m.delay):
		return &providers.CompletionResponse{
			Content: "Slow response",
			Model:   req.Model,
			Usage: providers.Usage{
				PromptTokens:     10,
				CompletionTokens: 20,
				TotalTokens:      30,
			},
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func setupWorkflowAPI(t *testing.T) (*WorkflowAPI, *storage.Storage) {
	t.Helper()
	db, err := storage.NewStorage(":memory:")
	if err != nil {
		t.Fatalf("Failed to create in-memory storage: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	registry := providers.NewRegistry()
	manager := newTestJobManager(db, registry)
	manager.StartWorkers()
	t.Cleanup(func() { manager.StopWorkers(context.Background()) })
	return NewWorkflowAPI(db, registry, manager), db
}

func newTestJobManager(db *storage.Storage, registry *providers.Registry) *jobs.Manager {
	cfg := jobs.DefaultManagerConfig()
	cfg.MaxConcurrentWorkflows = 4
	cfg.WorkerCount = 4
	cfg.WorkerPollInterval = 2 * time.Millisecond
	return jobs.NewManagerWithConfig(db, registry, cfg)
}

func registerMockProvider(t *testing.T, api *WorkflowAPI, response string) *MockExecuteProvider {
	t.Helper()
	p := &MockExecuteProvider{
		name: "mock",
		models: []providers.Model{
			{ID: "mock-model", Provider: "mock", InputCost: 0.000001, OutputCost: 0.000002},
		},
		response: response,
	}
	api.registry.Register(p)
	return p
}

func strictRetryPolicy() *workflow.RetryPolicy {
	return &workflow.RetryPolicy{
		MaxAttempts:     1,
		BackoffMs:       0,
		BackoffMultiply: 1,
		MaxBackoffMs:    0,
	}
}

func strictPromptNode(id, model, prompt string) *workflow.Node {
	temp := 0.0
	return &workflow.Node{
		ID:             id,
		Type:           workflow.NodeTypePrompt,
		Model:          model,
		Prompt:         prompt,
		Temperature:    &temp,
		MaxTokens:      256,
		TimeoutSeconds: 30,
		RetryPolicy:    strictRetryPolicy(),
	}
}

func strictPromptNodeMap(id, model, prompt string) map[string]interface{} {
	return map[string]interface{}{
		"id":              id,
		"type":            "prompt",
		"model":           model,
		"prompt":          prompt,
		"temperature":     0.0,
		"max_tokens":      256,
		"timeout_seconds": 30,
		"retry_policy": map[string]interface{}{
			"max_attempts":       1,
			"backoff_ms":         0,
			"backoff_multiply":   1.0,
			"max_backoff_ms":     0,
			"retryable_errors":   []interface{}{},
			"adaptive_reasoning": nil,
		},
	}
}

func createTestJob(t *testing.T, db *storage.Storage, status string, opts ...func(*storage.Job)) *storage.Job {
	t.Helper()
	job := &storage.Job{
		ID:          "job-" + uuid.New().String(),
		Description: "test",
		Model:       "test-model",
		Status:      status,
	}
	for _, opt := range opts {
		opt(job)
	}
	if err := db.CreateExecution(job); err != nil {
		t.Fatalf("Failed to create job: %v", err)
	}
	return job
}

func decodeJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v. Body: %s", err, w.Body.String())
	}
	return resp
}

func jsonRequest(t *testing.T, method, url string, body interface{}) *http.Request {
	t.Helper()
	if body == nil {
		return httptest.NewRequest(method, url, nil)
	}
	if str, ok := body.(string); ok {
		return httptest.NewRequest(method, url, bytes.NewReader([]byte(str)))
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("Failed to marshal request body: %v", err)
	}
	return httptest.NewRequest(method, url, bytes.NewReader(b))
}

func serveHTTP(t *testing.T, router *mux.Router, method, url string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	router.ServeHTTP(w, jsonRequest(t, method, url, body))
	return w
}
