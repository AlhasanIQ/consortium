package workflow

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/alhasaniq/consortium/pkg/providers"
)

// MockProvider implements a deterministic provider for testing
type MockProvider struct {
	name      string
	models    []providers.Model
	responses map[string]string // Map prompt keywords to responses
}

func NewMockProvider(name string) *MockProvider {
	return &MockProvider{
		name: name,
		models: []providers.Model{
			{
				ID:         "mock-model",
				Name:       "Mock Model",
				Provider:   name,
				ContextLen: 4096,
				InputCost:  0.0,
				OutputCost: 0.0,
				MaxTokens:  2048,
				Available:  true,
			},
		},
		responses: make(map[string]string),
	}
}

func (m *MockProvider) Name() string {
	return m.name
}

func (m *MockProvider) Models() []providers.Model {
	return m.models
}

func (m *MockProvider) Complete(ctx context.Context, req *providers.CompletionRequest) (*providers.CompletionResponse, error) {
	// Deterministic response based on prompt content
	response := "Mock response"

	// Concatenate all message contents for keyword matching
	var allContent strings.Builder
	for _, msg := range req.Messages {
		allContent.WriteString(msg.Content)
		allContent.WriteString(" ")
	}
	prompt := allContent.String()

	// Return configured response if available
	// Match longest keyword first for deterministic behavior
	longestMatch := ""
	for keyword, resp := range m.responses {
		if contains(prompt, keyword) && len(keyword) > len(longestMatch) {
			longestMatch = keyword
			response = resp
		}
	}

	// Calculate fake token counts
	inputTokens := len(prompt) / 4 // Rough approximation
	outputTokens := len(response) / 4

	return &providers.CompletionResponse{
		ID:      "mock-response-id",
		Content: response,
		Model:   req.Model,
		Usage: providers.Usage{
			PromptTokens:     inputTokens,
			CompletionTokens: outputTokens,
			TotalTokens:      inputTokens + outputTokens,
		},
	}, nil
}

func (m *MockProvider) Cost(model string, inputTokens, outputTokens int) float64 {
	return 0.0 // Free for testing
}

func (m *MockProvider) EstimateTokens(text string) int {
	return len(text) / 4
}

func (m *MockProvider) SetResponse(keyword, response string) {
	m.responses[keyword] = response
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr || findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// MockStorage for testing job persistence
type MockStorage struct {
	jobs     map[string]*MockJob
	execLogs map[string][]*MockExecLog
}

type MockJob struct {
	ID           string
	Status       string
	ResultText   string
	TokensTotal  int
	Cost         float64
	ErrorMessage string
	RetryCount   int
}

type MockExecLog struct {
	JobID         string
	AttemptNumber int
	Status        string
	LatencyMs     float64
	TokensInput   int
	TokensOutput  int
	ErrorMessage  string
}

func NewMockStorage() *MockStorage {
	return &MockStorage{
		jobs:     make(map[string]*MockJob),
		execLogs: make(map[string][]*MockExecLog),
	}
}

func (s *MockStorage) CreateJob(id string) {
	s.jobs[id] = &MockJob{
		ID:     id,
		Status: "pending",
	}
}

func (s *MockStorage) GetJob(id string) (*MockJob, error) {
	job, exists := s.jobs[id]
	if !exists {
		return nil, fmt.Errorf("job not found")
	}
	return job, nil
}

func (s *MockStorage) UpdateJob(job *MockJob) error {
	s.jobs[job.ID] = job
	return nil
}

func (s *MockStorage) AddExecLog(log *MockExecLog) error {
	s.execLogs[log.JobID] = append(s.execLogs[log.JobID], log)
	return nil
}

func (s *MockStorage) GetExecLogs(jobID string) []*MockExecLog {
	return s.execLogs[jobID]
}

func NewMockLLMClient(registry *providers.Registry) *providers.Client {
	// For tests, we create a client without a logger (nil is OK — logging is skipped).
	return providers.NewClient(registry, nil)
}

// Helper to create test executor with mock provider
func NewTestExecutor() *Executor {
	registry := providers.NewRegistry()
	mockProvider := NewMockProvider("test")
	registry.Register(mockProvider)
	client := NewMockLLMClient(registry)
	return NewExecutor(client)
}

// Helper to create test executor with custom registry
func NewExecutorWithRegistry(registry *providers.Registry) *Executor {
	client := NewMockLLMClient(registry)
	return NewExecutor(client)
}

// MockProviderWithCost is a mock provider that returns non-zero costs for testing cost limits
type MockProviderWithCost struct {
	name       string
	models     []providers.Model
	responses  map[string]string
	inputCost  float64 // Cost per input token
	outputCost float64 // Cost per output token
}

func NewMockProviderWithCost(name string, inputCost, outputCost float64) *MockProviderWithCost {
	return &MockProviderWithCost{
		name: name,
		models: []providers.Model{
			{
				ID:         "mock-model",
				Name:       "Mock Model",
				Provider:   name,
				ContextLen: 4096,
				InputCost:  inputCost,
				OutputCost: outputCost,
				MaxTokens:  2048,
				Available:  true,
			},
		},
		responses:  make(map[string]string),
		inputCost:  inputCost,
		outputCost: outputCost,
	}
}

func (m *MockProviderWithCost) Name() string {
	return m.name
}

func (m *MockProviderWithCost) Models() []providers.Model {
	return m.models
}

func (m *MockProviderWithCost) Complete(ctx context.Context, req *providers.CompletionRequest) (*providers.CompletionResponse, error) {
	response := "Mock response with cost"

	prompt := ""
	if len(req.Messages) > 0 {
		prompt = req.Messages[0].Content
	}

	for keyword, resp := range m.responses {
		if contains(prompt, keyword) {
			response = resp
			break
		}
	}

	// Fixed token counts for predictable cost testing
	inputTokens := 100
	outputTokens := 50

	return &providers.CompletionResponse{
		ID:      "mock-response-id",
		Content: response,
		Model:   req.Model,
		Usage: providers.Usage{
			PromptTokens:     inputTokens,
			CompletionTokens: outputTokens,
			TotalTokens:      inputTokens + outputTokens,
		},
	}, nil
}

func (m *MockProviderWithCost) Cost(model string, inputTokens, outputTokens int) float64 {
	return float64(inputTokens)*m.inputCost + float64(outputTokens)*m.outputCost
}

func (m *MockProviderWithCost) EstimateTokens(text string) int {
	return len(text) / 4
}

func (m *MockProviderWithCost) SetResponse(keyword, response string) {
	m.responses[keyword] = response
}

// Helper to create test executor with non-zero costs for cost limit testing
func NewTestExecutorWithCost(inputCost, outputCost float64) *Executor {
	registry := providers.NewRegistry()
	mockProvider := NewMockProviderWithCost("test", inputCost, outputCost)
	registry.Register(mockProvider)
	client := NewMockLLMClient(registry)
	return NewExecutor(client)
}

// FailingMockProvider fails for a configurable number of calls, then succeeds.
// Used for testing retry behavior in the executor.
type FailingMockProvider struct {
	name      string
	models    []providers.Model
	failCount int   // Number of initial calls that should fail
	callCount int   // Current call count
	failErr   error // The error to return on failure
	mu        sync.Mutex
}

func NewFailingMockProvider(name string, failCount int, failErr error) *FailingMockProvider {
	return &FailingMockProvider{
		name:      name,
		failCount: failCount,
		failErr:   failErr,
		models: []providers.Model{
			{
				ID:         "mock-model",
				Name:       "Mock Model",
				Provider:   name,
				ContextLen: 4096,
				InputCost:  0.0,
				OutputCost: 0.0,
				MaxTokens:  2048,
				Available:  true,
			},
		},
	}
}

func (m *FailingMockProvider) Name() string                                 { return m.name }
func (m *FailingMockProvider) Models() []providers.Model                    { return m.models }
func (m *FailingMockProvider) EstimateTokens(text string) int               { return len(text) / 4 }
func (m *FailingMockProvider) Cost(model string, input, output int) float64 { return 0 }

func (m *FailingMockProvider) Complete(ctx context.Context, req *providers.CompletionRequest) (*providers.CompletionResponse, error) {
	m.mu.Lock()
	m.callCount++
	count := m.callCount
	m.mu.Unlock()

	if count <= m.failCount {
		return nil, m.failErr
	}

	return &providers.CompletionResponse{
		ID:      "mock-success",
		Content: "Success after retries",
		Model:   req.Model,
		Usage: providers.Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}, nil
}

func (m *FailingMockProvider) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

// AlwaysFailProvider always fails — used to test max attempts exhaustion.
type AlwaysFailProvider struct {
	name   string
	models []providers.Model
	err    error
	mu     sync.Mutex
	calls  int
}

func NewAlwaysFailProvider(name string, err error) *AlwaysFailProvider {
	return &AlwaysFailProvider{
		name: name,
		err:  err,
		models: []providers.Model{
			{
				ID:         "mock-model",
				Name:       "Mock Model",
				Provider:   name,
				ContextLen: 4096,
				MaxTokens:  2048,
				Available:  true,
			},
		},
	}
}

func (m *AlwaysFailProvider) Name() string                                 { return m.name }
func (m *AlwaysFailProvider) Models() []providers.Model                    { return m.models }
func (m *AlwaysFailProvider) EstimateTokens(text string) int               { return 0 }
func (m *AlwaysFailProvider) Cost(model string, input, output int) float64 { return 0 }

func (m *AlwaysFailProvider) Complete(ctx context.Context, req *providers.CompletionRequest) (*providers.CompletionResponse, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	return nil, m.err
}

func (m *AlwaysFailProvider) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}
