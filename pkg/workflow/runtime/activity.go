package runtime

import (
	"context"
	"fmt"
	"sync"

	"github.com/alhasaniq/consortium/pkg/workflow"
)

// ActivityType defines the kind of activity (maps to existing node types).
type ActivityType string

const (
	ActivityTypeLLMCall       ActivityType = "llm_call"
	ActivityTypeConditional   ActivityType = "conditional"
	ActivityTypeResult        ActivityType = "result"
	ActivityTypeChildWorkflow ActivityType = "child_workflow"
	ActivityTypeAgentRun      ActivityType = "agent_run"
	ActivityTypeNovoRun       ActivityType = "novo_run"
)

// ActivityInput provides all context needed to execute an activity.
type ActivityInput struct {
	ActivityID      string                 `json:"activity_id"`
	NodeID          string                 `json:"node_id"`
	Type            ActivityType           `json:"type"`
	Node            *workflow.Node         `json:"node"`
	WorkflowContext map[string]interface{} `json:"workflow_context"`
	Attempt         int                    `json:"attempt"`
	IdempotencyKey  string                 `json:"idempotency_key"` // {job_id}:{node_id}:{attempt}
	ExecCtx         *workflow.ExecutionContext
	CostTracker     *workflow.CostTracker
}

// ActivityOutput is the result of executing an activity.
type ActivityOutput struct {
	NodeID       string                 `json:"node_id"`
	Success      bool                   `json:"success"`
	Output       string                 `json:"output,omitempty"`
	Error        string                 `json:"error,omitempty"`
	ErrorCode    string                 `json:"error_code,omitempty"`
	TokensInput  int                    `json:"tokens_input"`
	TokensOutput int                    `json:"tokens_output"`
	Cost         float64                `json:"cost"`
	LatencyMs    float64                `json:"latency_ms"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// ActivityHandler executes a single activity type.
type ActivityHandler interface {
	Type() ActivityType
	Execute(ctx context.Context, input *ActivityInput) (*ActivityOutput, error)
}

// ActivityHandlerRegistry maps activity types to their handlers.
type ActivityHandlerRegistry struct {
	mu       sync.RWMutex
	handlers map[ActivityType]ActivityHandler
}

// NewActivityHandlerRegistry creates a new registry.
func NewActivityHandlerRegistry() *ActivityHandlerRegistry {
	return &ActivityHandlerRegistry{
		handlers: make(map[ActivityType]ActivityHandler),
	}
}

// Register adds a handler for an activity type.
func (r *ActivityHandlerRegistry) Register(handler ActivityHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[handler.Type()] = handler
}

// Get retrieves the handler for an activity type.
func (r *ActivityHandlerRegistry) Get(actType ActivityType) (ActivityHandler, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[actType]
	if !ok {
		return nil, fmt.Errorf("no handler registered for activity type: %s", actType)
	}
	return h, nil
}

// NodeTypeToActivityType maps a workflow node type to an activity type.
func NodeTypeToActivityType(nodeType workflow.NodeType) ActivityType {
	switch nodeType {
	case workflow.NodeTypePrompt:
		return ActivityTypeLLMCall
	case workflow.NodeTypeConditional:
		return ActivityTypeConditional
	case workflow.NodeTypeResult:
		return ActivityTypeResult
	case workflow.NodeTypeChildWorkflow:
		return ActivityTypeChildWorkflow
	case workflow.NodeTypeAgentRun:
		return ActivityTypeAgentRun
	case workflow.NodeTypeNovoRun:
		return ActivityTypeNovoRun
	default:
		return ActivityType(string(nodeType))
	}
}
