package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/pkg/providers"
)

// LLMCallNodeRunner executes prompt/llm_call nodes by calling an LLM provider.
type LLMCallNodeRunner struct {
	llmClient *providers.Client
}

// NodeType returns NodeTypePrompt ("prompt").
func (r *LLMCallNodeRunner) NodeType() NodeType { return NodeTypePrompt }

// Execute runs the LLM completion for a prompt node.
func (r *LLMCallNodeRunner) Execute(sc *NodeContext) (*NodeResult, error) {
	startTime := time.Now()

	// Node timeout is mandatory; validator enforces this on submitted workflows.
	timeoutSeconds := sc.Node.TimeoutSeconds
	if timeoutSeconds <= 0 {
		err := NewNonRetryableError(fmt.Errorf("node timeout_seconds is required"), "INVALID_CONFIG")
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}

	// Create context with timeout and ensure cleanup
	nodeCtx, cancel := context.WithTimeout(sc.Ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	// Interpolate variables in prompt
	prompt := InterpolateVariables(sc.Node.Prompt, sc.WorkflowContext)

	// max_tokens is mandatory; validator enforces this on submitted workflows.
	maxTokens := sc.Node.MaxTokens
	if maxTokens == 0 {
		err := NewNonRetryableError(fmt.Errorf("node max_tokens is required"), "INVALID_CONFIG")
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}
	if maxTokens < 0 {
		maxTokens = 0
	}
	temperature := sc.Node.Temperature
	if temperature == nil {
		err := NewNonRetryableError(fmt.Errorf("node temperature is required"), "INVALID_CONFIG")
		return newErrorResult(sc.NodeID, err.Error(), float64(time.Since(startTime).Milliseconds())), err
	}

	// Build messages list
	var messages []providers.Message

	// Add system prompt if present
	if sc.Node.SystemPrompt != "" {
		systemPrompt := InterpolateVariables(sc.Node.SystemPrompt, sc.WorkflowContext)
		messages = append(messages, providers.Message{Role: "system", Content: systemPrompt})
	}

	// Add user message
	messages = append(messages, providers.Message{Role: "user", Content: prompt})

	// Build completion request
	req := &providers.ClientRequest{
		Model:       sc.Node.Model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: temperature,
		Metadata:    sc.Node.Metadata,
	}
	if topP := parseOptionalFloat(sc.Node.Metadata["top_p"]); topP != nil {
		req.TopP = topP
	}
	if stop := parseStringList(sc.Node.Metadata["stop"]); len(stop) > 0 {
		req.Stop = stop
	}
	if seed := parseOptionalInt(sc.Node.Metadata["seed"]); seed != nil {
		req.Seed = seed
	}
	if routing := parseOpenRouterProviderConfig(sc.Node.Metadata); routing != nil {
		req.ProviderRouting = routing
	}
	if reasoning := parseOpenRouterReasoningConfig(sc.Node.Metadata); reasoning != nil {
		req.Reasoning = reasoning
	}
	if sessionID := parseOptionalString(sc.Node.Metadata["session_id"]); sessionID != "" {
		req.SessionID = sessionID
	} else if sessionID := parseOptionalString(sc.Node.Metadata["openrouter_session_id"]); sessionID != "" {
		req.SessionID = sessionID
	}
	if metadataEnabled := parseOptionalBool(sc.Node.Metadata["openrouter_metadata_enabled"]); metadataEnabled != nil {
		req.OpenRouterMetadata = metadataEnabled
	}
	if tools := parseToolDefinitions(sc.Node.Metadata["tools"]); len(tools) > 0 {
		req.Tools = tools
		if toolChoice, ok := parseToolChoice(sc.Node.Metadata["tool_choice"]); ok {
			req.ToolChoice = toolChoice
		}
	}
	if parallelToolCalls := parseOptionalBool(sc.Node.Metadata["parallel_tool_calls"]); parallelToolCalls != nil {
		req.ParallelToolCalls = parallelToolCalls
	}
	if rf := parseResponseFormat(sc.Node.Metadata["response_format"]); rf != nil {
		req.ResponseFormat = rf
	}

	// Extract node label and name from metadata if available
	meta := sc.Node.DisplayMeta()
	nodeLabel, nodeName := meta.Label, meta.Name

	compCtx := &providers.CompletionContext{
		NodeID:       sc.NodeID,
		Attempt:      sc.Attempt,
		NodeLabel:    nodeLabel,
		NodeName:     nodeName,
		Meta:         sc.Node.Metadata,
		ParentSpanID: sc.ParentSpanID,
		ParentNodeID: compiledAggregationGroupParentNodeID(sc.NodeID, sc.Node.Metadata),
	}
	if sc.CostTracker != nil {
		compCtx.CostTracker = sc.CostTracker
	}
	if sc.ExecCtx != nil {
		compCtx.JobID = sc.ExecCtx.JobID
		compCtx.RunID = sc.ExecCtx.RunID
		compCtx.TraceWriter = sc.ExecCtx.TraceWriter
	}

	// Execute LLM completion with timeout context
	resp, err := r.llmClient.Complete(nodeCtx, req, compCtx)
	latency := float64(time.Since(startTime).Milliseconds())

	if err != nil {
		// Check if the error is due to timeout
		errorMsg := err.Error()
		var wrappedErr error
		if errors.Is(err, context.DeadlineExceeded) {
			if errors.Is(nodeCtx.Err(), context.DeadlineExceeded) {
				errorMsg = fmt.Sprintf("node timeout after %d seconds", timeoutSeconds)
				log.Printf("⏱️ Node %s timed out after %d seconds", sc.NodeID, timeoutSeconds)
				wrappedErr = NewRetryableError(fmt.Errorf("%s", errorMsg), RetryCodeTimeout)
			} else {
				// Preserve upstream/transport timeout details when the node deadline
				// itself did not expire.
				wrappedErr = classifyLLMCallError(err, errorMsg)
				if _, ok := wrappedErr.(*RetryableError); !ok {
					wrappedErr = NewRetryableError(fmt.Errorf("%s", errorMsg), RetryCodeTimeout)
				}
			}
		} else if errors.Is(err, context.Canceled) {
			errorMsg = "node cancelled"
			log.Printf("❌ Node %s was cancelled", sc.NodeID)
			wrappedErr = NewNonRetryableError(fmt.Errorf("%s", errorMsg), "CANCELLED")
		} else {
			wrappedErr = classifyLLMCallError(err, errorMsg)
		}

		errorMeta := BuildErrorObservabilityMetadata(err)
		metadata := MergeMetadata(sc.Node.Metadata, errorMeta)

		return newErrorResult(sc.NodeID, errorMsg, latency, metadata), wrappedErr
	}

	// Use actual cost from provider if available, otherwise estimate from token counts.
	// The providers.Client already prefers provider-reported cost; this handles the same
	// fallback for the NodeResult returned to the executor.
	var cost float64
	if resp.Usage.Cost != nil {
		cost = *resp.Usage.Cost
	} else if model, modelErr := r.llmClient.GetModel(sc.Node.Model); modelErr == nil {
		cost = float64(resp.Usage.PromptTokens)*model.InputCost + float64(resp.Usage.CompletionTokens)*model.OutputCost
	}

	// Cost limit enforcement is handled centrally by executeNode after runner returns.

	callMeta := BuildProviderResponseObservabilityMetadata(modelProviderPrefix(sc.Node.Model), resp)
	successMetadata := MergeMetadata(sc.Node.Metadata, callMeta)
	if len(resp.ToolCalls) > 0 {
		if successMetadata == nil {
			successMetadata = make(map[string]interface{})
		}
		successMetadata["tool_calls"] = resp.ToolCalls
	}

	if strings.EqualFold(strings.TrimSpace(resp.Finish), "length") && strings.TrimSpace(resp.Content) == "" {
		errMsg := fmt.Sprintf("output truncated with empty content (finish_reason=length, tokens_output=%d, max_tokens=%d)",
			resp.Usage.CompletionTokens, maxTokens)
		errorMeta := map[string]interface{}{
			"retry_layer": "contract",
			"error_phase": "output_truncated",
		}
		failureMetadata := MergeMetadata(successMetadata, errorMeta)
		return &NodeResult{
			NodeID:       sc.NodeID,
			Success:      false,
			Error:        errMsg,
			TokensInput:  resp.Usage.PromptTokens,
			TokensOutput: resp.Usage.CompletionTokens,
			Cost:         cost,
			LatencyMs:    latency,
			Metadata:     failureMetadata,
		}, NewRetryableError(fmt.Errorf("%s", errMsg), RetryCodeOutputTruncatedEmpty)
	}

	return &NodeResult{
		NodeID:       sc.NodeID,
		Success:      true,
		Output:       resp.Content,
		TokensInput:  resp.Usage.PromptTokens,
		TokensOutput: resp.Usage.CompletionTokens,
		Cost:         cost,
		LatencyMs:    latency,
		Metadata:     successMetadata,
	}, nil
}

func modelProviderPrefix(modelID string) string {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return ""
	}
	if idx := strings.Index(modelID, "/"); idx > 0 {
		return modelID[:idx]
	}
	return ""
}

func compiledAggregationGroupParentNodeID(nodeID string, metadata map[string]interface{}) string {
	if len(metadata) == 0 {
		return ""
	}
	groupID, _ := metadata["aggregation_group_node_id"].(string)
	groupID = strings.TrimSpace(groupID)
	if groupID == "" || groupID == nodeID {
		return ""
	}
	return groupID
}

func classifyLLMCallError(err error, errorMsg string) error {
	if pe, ok := providers.AsProviderError(err); ok {
		switch pe.Code {
		case providers.ErrCodeAuthError, providers.ErrCodeBadRequest, providers.ErrCodeModelNotFound, providers.ErrCodeInsufficientCredits:
			return NewNonRetryableError(fmt.Errorf("%s: %w", errorMsg, err), strings.ToUpper(pe.Code))
		case providers.ErrCodeRateLimited:
			return NewRetryableError(fmt.Errorf("%s: %w", errorMsg, err), "RATE_LIMIT")
		case providers.ErrCodeUpstreamTimeout:
			return NewRetryableError(fmt.Errorf("%s: %w", errorMsg, err), RetryCodeTimeout)
		case providers.ErrCodeUpstreamError:
			if pe.Retryable {
				return NewRetryableError(fmt.Errorf("%s: %w", errorMsg, err), "5xx")
			}
			return NewNonRetryableError(fmt.Errorf("%s: %w", errorMsg, err), strings.ToUpper(pe.Code))
		case providers.ErrCodeNoChoices:
			return NewRetryableError(fmt.Errorf("%s: %w", errorMsg, err), RetryCodeProviderNoChoices)
		default:
			if pe.Retryable {
				return NewRetryableError(fmt.Errorf("%s: %w", errorMsg, err), "TEMPORARY")
			}
			return NewNonRetryableError(fmt.Errorf("%s: %w", errorMsg, err), strings.ToUpper(pe.Code))
		}
	}

	lower := strings.ToLower(strings.TrimSpace(errorMsg))
	switch {
	case strings.Contains(lower, "rate limit wait") && strings.Contains(lower, "exceeds context deadline"):
		return NewRetryableError(fmt.Errorf("%s: %w", errorMsg, err), RetryCodeRateLimitWaitDeadline)
	case strings.Contains(lower, "no choices returned from openrouter"):
		return NewRetryableError(fmt.Errorf("%s: %w", errorMsg, err), RetryCodeProviderNoChoices)
	}

	// Structural Go error checks for unclassified errors
	if errors.Is(err, context.DeadlineExceeded) {
		return NewRetryableError(fmt.Errorf("%s: %w", errorMsg, err), RetryCodeTimeout)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return NewRetryableError(fmt.Errorf("%s: %w", errorMsg, err), RetryCodeTimeout)
	}

	// Wrap remaining unknown errors as retryable TEMPORARY so the retry
	// policy can decide whether to retry based on error code matching.
	return NewRetryableError(fmt.Errorf("%s: %w", errorMsg, err), "TEMPORARY")
}

func parseToolDefinitions(raw interface{}) []providers.ToolDefinition {
	if raw == nil {
		return nil
	}
	serialized, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var tools []providers.ToolDefinition
	if err := json.Unmarshal(serialized, &tools); err != nil {
		return nil
	}
	if len(tools) == 0 {
		return nil
	}
	return tools
}

func parseResponseFormat(raw interface{}) *providers.ResponseFormat {
	if raw == nil {
		return nil
	}
	serialized, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var rf providers.ResponseFormat
	if err := json.Unmarshal(serialized, &rf); err != nil {
		return nil
	}
	if rf.Type == "" {
		return nil
	}
	return &rf
}

func parseOptionalInt(raw interface{}) *int {
	switch v := raw.(type) {
	case nil:
		return nil
	case int:
		return providers.IntPtr(v)
	case int64:
		return providers.IntPtr(int(v))
	case float64:
		return providers.IntPtr(int(v))
	case json.Number:
		if parsed, err := v.Int64(); err == nil {
			return providers.IntPtr(int(parsed))
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return providers.IntPtr(parsed)
		}
	}
	return nil
}

func parseOptionalFloat(raw interface{}) *float64 {
	switch v := raw.(type) {
	case nil:
		return nil
	case float64:
		return providers.Float64Ptr(v)
	case float32:
		return providers.Float64Ptr(float64(v))
	case int:
		return providers.Float64Ptr(float64(v))
	case int64:
		return providers.Float64Ptr(float64(v))
	case json.Number:
		if parsed, err := v.Float64(); err == nil {
			return providers.Float64Ptr(parsed)
		}
	case string:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return providers.Float64Ptr(parsed)
		}
	}
	return nil
}

func parseOptionalString(raw interface{}) string {
	switch v := raw.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	default:
		return ""
	}
}

func parseOptionalBool(raw interface{}) *bool {
	switch v := raw.(type) {
	case nil:
		return nil
	case bool:
		return providers.BoolPtr(v)
	case string:
		if parsed, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			return providers.BoolPtr(parsed)
		}
	}
	return nil
}

func parseStringList(raw interface{}) []string {
	switch v := raw.(type) {
	case nil:
		return nil
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []string{v}
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if strings.TrimSpace(item) != "" {
				out = append(out, item)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		serialized, err := json.Marshal(raw)
		if err != nil {
			return nil
		}
		var out []string
		if err := json.Unmarshal(serialized, &out); err != nil {
			return nil
		}
		return parseStringList(out)
	}
}

func parseToolChoice(raw interface{}) (interface{}, bool) {
	if raw == nil {
		return nil, false
	}
	if s, ok := raw.(string); ok {
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, false
		}
		return s, true
	}
	return raw, true
}
