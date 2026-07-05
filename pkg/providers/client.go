package providers

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/trace"
	"github.com/google/uuid"
)

// SubNodeRecord holds the fields for a non-LLM child node (e.g. aggregation decision steps).
type SubNodeRecord struct {
	JobID        string
	RunID        string
	NodeID       string
	ParentNodeID string
	NodeType     string
	Label        string
	Name         string
	Output       string
	LatencyMs    float64
	StartedAt    *time.Time
	CompletedAt  *time.Time
}

// NodeLogger abstracts the storage operations that Client needs for request
// logging and sub-node tracking. *storage.Storage does NOT satisfy this
// interface directly — callers must provide an adapter (see NewStorageNodeLogger
// in pkg/jobs).
type NodeLogger interface {
	// LogLLMRequestFull logs an LLM request start or completion (upsert by node).
	LogLLMRequestFull(l *storage.LLMRequestLog) error

	// AddSubNode persists a non-LLM child node (e.g. aggregation decision steps).
	AddSubNode(r SubNodeRecord) error
}

// Client is the centralized LLM client that enforces accounting and logging.
// This is the ONLY way to make LLM requests in the application.
// All requests are automatically logged via the NodeLogger for observability and billing.
type Client struct {
	registry *Registry
	logger   NodeLogger
}

// NewClient creates a new LLM client with automatic accounting and logging.
// The logger parameter is optional (nil disables request logging).
func NewClient(registry *Registry, logger NodeLogger) *Client {
	return &Client{
		registry: registry,
		logger:   logger,
	}
}

// ClientRequest represents a high-level LLM completion request with convenience
// fields for provider-specific extensions. The Client.Complete method translates
// this into the lower-level CompletionRequest used by Provider implementations.
// Temperature uses a pointer: nil = unset (use provider default), 0.0 = explicit zero.
type ClientRequest struct {
	Model       string
	Messages    []Message
	MaxTokens   int
	Temperature *float64
	TopP        *float64
	Stop        []string
	Metadata    map[string]interface{} // Optional metadata for logging

	// OpenRouter-specific request controls
	ResponseFormat     *ResponseFormat
	Seed               *int
	ProviderRouting    *ProviderRouting
	Reasoning          *ReasoningConfig
	SessionID          string
	OpenRouterMetadata *bool

	// OpenAI/OpenRouter tool-call controls
	Tools             []ToolDefinition
	ToolChoice        interface{}
	ParallelToolCalls *bool
}

// CostAccumulator tracks token/cost usage for a workflow execution.
// workflow.CostTracker satisfies this interface.
type CostAccumulator interface {
	Add(inputTokens, outputTokens int, cost float64)
}

// CompletionContext holds contextual information for tracking
type CompletionContext struct {
	JobID        string                 // Parent job ID for tracking
	RunID        string                 // Durable per-run identity (optional)
	NodeID       string                 // Workflow node ID (if applicable)
	Attempt      int                    // Attempt number for retry tracking (1-based)
	NodeLabel    string                 // Human-readable label (e.g., "Claude")
	NodeName     string                 // Full name (e.g., "Claude Response")
	Meta         map[string]interface{} // Additional metadata
	TraceWriter  trace.Writer           // Optional trace span writer (nil = no tracing)
	ParentSpanID string                 // Parent node span for trace correlation
	ParentNodeID string                 // Parent node ID (for aggregation child nodes)
	CostTracker  CostAccumulator        // Optional shared workflow cost tracker
}

// Complete performs an LLM completion with automatic accounting and logging.
// This is the primary method for all LLM requests in the application.
func (c *Client) Complete(ctx context.Context, req *ClientRequest, compCtx *CompletionContext) (*CompletionResponse, error) {
	if compCtx == nil {
		compCtx = &CompletionContext{}
	}

	// Marshal messages once — reused for both START and COMPLETION logging.
	shouldLog := c.logger != nil && compCtx.JobID != "" && compCtx.NodeID != ""
	var promptJSON []byte
	if shouldLog {
		promptJSON, _ = json.Marshal(req.Messages)
	}

	// Log node START before execution (for real-time streaming)
	if shouldLog {
		if logErr := c.logger.LogLLMRequestFull(&storage.LLMRequestLog{
			JobID:         compCtx.JobID,
			NodeID:        compCtx.NodeID,
			Model:         req.Model,
			Prompt:        string(promptJSON),
			Status:        "running",
			NodeLabel:     compCtx.NodeLabel,
			NodeName:      compCtx.NodeName,
			AttemptNumber: compCtx.Attempt,
			ParentNodeID:  compCtx.ParentNodeID,
			RunID:         compCtx.RunID,
		}); logErr != nil {
			log.Printf("WARNING: Failed to log node start: %v", logErr)
		}
	}

	startTime := time.Now()

	// Create provider request (Messages already uses providers.Message type)
	providerReq := &CompletionRequest{
		Model:             req.Model,
		Messages:          req.Messages,
		MaxTokens:         req.MaxTokens,
		Temperature:       req.Temperature,
		TopP:              req.TopP,
		Stop:              req.Stop,
		Tools:             req.Tools,
		ToolChoice:        req.ToolChoice,
		ParallelToolCalls: req.ParallelToolCalls,
	}
	// Package provider-specific extensions if any are set.
	if req.ResponseFormat != nil || req.Seed != nil || req.ProviderRouting != nil || req.Reasoning != nil || req.SessionID != "" || req.OpenRouterMetadata != nil {
		providerReq.Extensions = &ProviderExtensions{
			ResponseFormat:     req.ResponseFormat,
			Seed:               req.Seed,
			ProviderRouting:    req.ProviderRouting,
			Reasoning:          req.Reasoning,
			SessionID:          req.SessionID,
			OpenRouterMetadata: req.OpenRouterMetadata,
		}
	}

	// Call provider
	providerResp, err := c.registry.Complete(ctx, providerReq)
	latency := float64(time.Since(startTime).Milliseconds())

	// Use actual cost from provider if available, otherwise estimate from token counts
	var cost float64
	if err == nil && providerResp != nil {
		if providerResp.Usage.Cost != nil {
			cost = *providerResp.Usage.Cost
		} else if model, modelErr := c.registry.GetModel(req.Model); modelErr == nil {
			cost = float64(providerResp.Usage.PromptTokens)*model.InputCost +
				float64(providerResp.Usage.CompletionTokens)*model.OutputCost
		}
	}

	// Update shared workflow cost tracker for every successful LLM call.
	if err == nil && providerResp != nil && compCtx.CostTracker != nil {
		compCtx.CostTracker.Add(providerResp.Usage.PromptTokens, providerResp.Usage.CompletionTokens, cost)
	}

	// Write trace span for the LLM call
	c.writeCallSpan(ctx, compCtx, req, providerResp, err, startTime, latency, cost)

	// Update node with completion data (LogLLMRequest now handles UPSERT)
	if shouldLog {
		logStatus := "completed"
		logResponse := ""
		logTokensIn := 0
		logTokensOut := 0
		logError := ""

		if err != nil {
			logStatus = "failed"
			logError = err.Error()
		} else if providerResp != nil {
			logResponse = providerResp.Content
			logTokensIn = providerResp.Usage.PromptTokens
			logTokensOut = providerResp.Usage.CompletionTokens
		}

		// Log completion (will UPDATE the existing record with proper timestamp)
		if logErr := c.logger.LogLLMRequestFull(&storage.LLMRequestLog{
			JobID:         compCtx.JobID,
			NodeID:        compCtx.NodeID,
			Model:         req.Model,
			Prompt:        string(promptJSON),
			Response:      logResponse,
			TokensIn:      logTokensIn,
			TokensOut:     logTokensOut,
			Cost:          cost,
			Latency:       latency,
			Status:        logStatus,
			ErrMsg:        logError,
			NodeLabel:     compCtx.NodeLabel,
			NodeName:      compCtx.NodeName,
			AttemptNumber: compCtx.Attempt,
			ParentNodeID:  compCtx.ParentNodeID,
			RunID:         compCtx.RunID,
		}); logErr != nil {
			log.Printf("WARNING: Failed to log node completion: %v", logErr)
		}
	}

	if err != nil {
		return nil, err
	}

	return providerResp, nil
}

// writeCallSpan emits a trace span for an LLM call. Best-effort, never fails the request.
func (c *Client) writeCallSpan(ctx context.Context, compCtx *CompletionContext, req *ClientRequest, resp *CompletionResponse, callErr error, startTime time.Time, latencyMs float64, cost float64) {
	if compCtx.TraceWriter == nil {
		return
	}
	if compCtx.JobID == "" {
		log.Printf("WARNING: TraceWriter configured but JobID is empty; skipping call span (node_id=%s)", compCtx.NodeID)
		return
	}

	status := trace.StatusOK
	attrs := map[string]any{
		"call_type":      "llm",
		"model":          req.Model,
		"messages_count": len(req.Messages),
		"max_tokens":     req.MaxTokens,
	}
	if req.Temperature != nil {
		attrs["temperature"] = *req.Temperature
	}

	if callErr != nil {
		status = trace.StatusError
		attrs["error"] = callErr.Error()
	} else if resp != nil {
		attrs["tokens_input"] = resp.Usage.PromptTokens
		attrs["tokens_output"] = resp.Usage.CompletionTokens
		attrs["cost"] = cost
		attrs["finish_reason"] = resp.Finish
		if resp.RequestID != "" {
			attrs["provider_request_id"] = resp.RequestID
		}
		if resp.GenerationID != "" {
			attrs["provider_generation_id"] = resp.GenerationID
		}
		if resp.RateLimitWaitMs > 0 {
			attrs["rate_limit_wait_ms"] = resp.RateLimitWaitMs
		}
	}

	span := &trace.Span{
		JobID:        compCtx.JobID,
		NodeID:       compCtx.NodeID,
		SpanID:       uuid.NewString(),
		ParentSpanID: compCtx.ParentSpanID,
		Kind:         trace.KindCall,
		Status:       status,
		StartedAt:    startTime,
		DurationMs:   latencyMs,
		Attributes:   attrs,
	}
	traceCtx := ctx
	traceCancel := func() {}
	if traceCtx == nil || traceCtx.Err() != nil {
		// When the node context has timed out/cancelled we still want to persist
		// the failure span for post-mortem analysis.
		traceCtx, traceCancel = context.WithTimeout(context.Background(), 2*time.Second)
	}
	defer traceCancel()

	if err := compCtx.TraceWriter.WriteSpan(traceCtx, span); err != nil {
		log.Printf("WARNING: Failed to write LLM call trace span (job_id=%s, node_id=%s, span_id=%s, kind=%s): %v",
			span.JobID, span.NodeID, span.SpanID, span.Kind, err)
	}
}

// LogSubNode saves a non-LLM child workflow node (e.g. aggregation decision steps).
// Best-effort: errors are logged but never returned.
func (c *Client) LogSubNode(jobID, runID, nodeID, parentNodeID, nodeType, label, name, output string, latencyMs float64) {
	if c.logger == nil || jobID == "" || nodeID == "" {
		return
	}
	now := time.Now()
	completed := now
	if err := c.logger.AddSubNode(SubNodeRecord{
		JobID:        jobID,
		RunID:        runID,
		NodeID:       nodeID,
		ParentNodeID: parentNodeID,
		NodeType:     nodeType,
		Label:        label,
		Name:         name,
		Output:       output,
		LatencyMs:    latencyMs,
		StartedAt:    &now,
		CompletedAt:  &completed,
	}); err != nil {
		log.Printf("WARNING: Failed to log sub-node %s: %v", nodeID, err)
	}
}

// GetModel returns model information by ID
func (c *Client) GetModel(modelID string) (Model, error) {
	return c.registry.GetModel(modelID)
}

// GetModels returns all available models
func (c *Client) GetModels() []Model {
	return c.registry.GetModels()
}
