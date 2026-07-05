package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/pkg/events"
	"github.com/alhasaniq/consortium/pkg/jobs"
	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/alhasaniq/consortium/pkg/workflow"
	"github.com/alhasaniq/consortium/pkg/workflow/compiler"
	workflowruntime "github.com/alhasaniq/consortium/pkg/workflow/runtime"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

const (
	openAIObjectChatCompletion     = "chat.completion"
	openAIObjectResponse           = "response"
	openAIObjectModel              = "model"
	openAIUsageEndpointChat        = "/v1/chat/completions"
	openAIUsageEndpointResponses   = "/v1/responses"
	openAIIdempotencyWaitTimeout   = 30 * time.Second
	openAIRequestBodyLimit         = 2 << 20
	openAIHeartbeatInterval        = 15 * time.Second
	openAIMaxIdempotencyKeyBytes   = 255
	openAIMaxBearerTokenBytes      = 4096
	openAIPreAuthRequestsPerMinute = 600
	openAIMaxRequestedTokens       = 1000000
	openAIMaxMetadataPairs         = 16
	openAIMaxMetadataKeyChars      = 64
	openAIMaxMetadataValueChars    = 512
	openAIPreviousResponseMaxHops  = 20
)

var (
	errOpenAIRequestBodyTooLarge          = errors.New("openai request body too large")
	errOpenAIPreviousResponseInvalid      = errors.New("invalid previous response chain")
	errOpenAIPreviousResponseNotCompleted = errors.New("previous response is not completed")
)

type openAIAuthContext struct {
	key *storage.APIKey
}

type openAIChatCompletionRequest struct {
	Model                string                     `json:"model"`
	Messages             []openAIChatMessage        `json:"messages"`
	Store                *bool                      `json:"store,omitempty"`
	Stream               bool                       `json:"stream,omitempty"`
	N                    *int                       `json:"n,omitempty"`
	Logprobs             *bool                      `json:"logprobs,omitempty"`
	TopLogprobs          *int                       `json:"top_logprobs,omitempty"`
	Prediction           json.RawMessage            `json:"prediction,omitempty"`
	Temperature          *float64                   `json:"temperature,omitempty"`
	TopP                 *float64                   `json:"top_p,omitempty"`
	MaxTokens            int                        `json:"max_tokens,omitempty"`
	MaxCompletionTokens  int                        `json:"max_completion_tokens,omitempty"`
	Stop                 interface{}                `json:"stop,omitempty"`
	Tools                []providers.ToolDefinition `json:"tools,omitempty"`
	ToolChoice           interface{}                `json:"tool_choice,omitempty"`
	ResponseFormat       *providers.ResponseFormat  `json:"response_format,omitempty"`
	Modalities           []string                   `json:"modalities,omitempty"`
	Audio                json.RawMessage            `json:"audio,omitempty"`
	Functions            json.RawMessage            `json:"functions,omitempty"`
	FunctionCall         json.RawMessage            `json:"function_call,omitempty"`
	Seed                 *int                       `json:"seed,omitempty"`
	Metadata             map[string]interface{}     `json:"metadata,omitempty"`
	Provider             *providers.ProviderRouting `json:"provider,omitempty"`
	Order                []string                   `json:"order,omitempty"`
	AllowFallbacks       *bool                      `json:"allow_fallbacks,omitempty"`
	RequireParameters    *bool                      `json:"require_parameters,omitempty"`
	StreamOptions        openAIStreamOptions        `json:"stream_options,omitempty"`
	ReasoningEffort      string                     `json:"reasoning_effort,omitempty"`
	Verbosity            string                     `json:"verbosity,omitempty"`
	ServiceTier          string                     `json:"service_tier,omitempty"`
	User                 string                     `json:"user,omitempty"`
	SafetyIdentifier     string                     `json:"safety_identifier,omitempty"`
	ParallelToolCalls    *bool                      `json:"parallel_tool_calls,omitempty"`
	SessionID            string                     `json:"session_id,omitempty"`
	PromptCacheKey       string                     `json:"prompt_cache_key,omitempty"`
	PromptCacheRetention string                     `json:"prompt_cache_retention,omitempty"`
}

type openAIStreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

type openAIResponsesRequest struct {
	Model                string                     `json:"model"`
	Input                json.RawMessage            `json:"input"`
	Instructions         string                     `json:"instructions,omitempty"`
	Store                *bool                      `json:"store,omitempty"`
	Background           bool                       `json:"background,omitempty"`
	PreviousResponseID   string                     `json:"previous_response_id,omitempty"`
	Conversation         json.RawMessage            `json:"conversation,omitempty"`
	Text                 openAIResponsesTextConfig  `json:"text,omitempty"`
	Reasoning            openAIReasoningConfig      `json:"reasoning,omitempty"`
	Stream               bool                       `json:"stream,omitempty"`
	N                    *int                       `json:"n,omitempty"`
	Logprobs             *bool                      `json:"logprobs,omitempty"`
	TopLogprobs          *int                       `json:"top_logprobs,omitempty"`
	Prediction           json.RawMessage            `json:"prediction,omitempty"`
	Temperature          *float64                   `json:"temperature,omitempty"`
	TopP                 *float64                   `json:"top_p,omitempty"`
	MaxOutputTokens      int                        `json:"max_output_tokens,omitempty"`
	Stop                 interface{}                `json:"stop,omitempty"`
	Tools                []openAIResponsesTool      `json:"tools,omitempty"`
	ToolChoice           interface{}                `json:"tool_choice,omitempty"`
	ResponseFormat       *providers.ResponseFormat  `json:"response_format,omitempty"`
	Seed                 *int                       `json:"seed,omitempty"`
	Metadata             map[string]interface{}     `json:"metadata,omitempty"`
	PromptCacheKey       string                     `json:"prompt_cache_key,omitempty"`
	PromptCacheRetention string                     `json:"prompt_cache_retention,omitempty"`
	Provider             *providers.ProviderRouting `json:"provider,omitempty"`
	Order                []string                   `json:"order,omitempty"`
	AllowFallbacks       *bool                      `json:"allow_fallbacks,omitempty"`
	RequireParameters    *bool                      `json:"require_parameters,omitempty"`
	ServiceTier          string                     `json:"service_tier,omitempty"`
	User                 string                     `json:"user,omitempty"`
	SafetyIdentifier     string                     `json:"safety_identifier,omitempty"`
	ParallelToolCalls    *bool                      `json:"parallel_tool_calls,omitempty"`
	SessionID            string                     `json:"session_id,omitempty"`
	StreamOptions        openAIStreamOptions        `json:"stream_options,omitempty"`
	Include              []string                   `json:"include,omitempty"`
	Truncation           string                     `json:"truncation,omitempty"`
	ContextManagement    json.RawMessage            `json:"context_management,omitempty"`
	MaxToolCalls         *int                       `json:"max_tool_calls,omitempty"`
	Moderation           json.RawMessage            `json:"moderation,omitempty"`
}

type openAIChatMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Name       string          `json:"name,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

type openAIResponsesTool struct {
	Type        string                            `json:"type"`
	Function    *providers.ToolFunctionDefinition `json:"function,omitempty"`
	Name        string                            `json:"name,omitempty"`
	Description string                            `json:"description,omitempty"`
	Parameters  map[string]interface{}            `json:"parameters,omitempty"`
	Strict      *bool                             `json:"strict,omitempty"`
}

type openAIResponsesTextConfig struct {
	Format *providers.ResponseFormat `json:"format,omitempty"`
}

type openAIReasoningConfig struct {
	Effort string `json:"effort,omitempty"`
}

type openAINormalizedPrompt struct {
	SystemPrompt string
	UserPrompt   string
	InputTokens  int
}

type openAIValidationError struct {
	Message string
	Code    string
	Param   string
}

type openAIRouteResolution struct {
	RequestedModel string
	ResolvedModel  string
	WorkflowID     string
	ProviderModel  string
	Mode           string
}

type openAIChatCompletionResponse struct {
	ID                string                 `json:"id"`
	Object            string                 `json:"object"`
	Created           int64                  `json:"created"`
	Model             string                 `json:"model"`
	SystemFingerprint string                 `json:"system_fingerprint"`
	Choices           []openAIChatChoice     `json:"choices"`
	Usage             openAIChatUsage        `json:"usage"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
}

type openAIChatChoice struct {
	Index        int                  `json:"index"`
	Message      openAIChatMessageOut `json:"message"`
	FinishReason string               `json:"finish_reason"`
}

type openAIChatMessageOut struct {
	Role      string               `json:"role"`
	Content   string               `json:"content,omitempty"`
	ToolCalls []providers.ToolCall `json:"tool_calls,omitempty"`
}

type openAIChatUsage struct {
	PromptTokens            int                          `json:"prompt_tokens"`
	CompletionTokens        int                          `json:"completion_tokens"`
	TotalTokens             int                          `json:"total_tokens"`
	PromptTokensDetails     openAIPromptTokensDetails    `json:"prompt_tokens_details"`
	CompletionTokensDetails openAICompletionTokenDetails `json:"completion_tokens_details"`
}

type openAIPromptTokensDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

type openAICompletionTokenDetails struct {
	ReasoningTokens          int `json:"reasoning_tokens"`
	AcceptedPredictionTokens int `json:"accepted_prediction_tokens"`
	RejectedPredictionTokens int `json:"rejected_prediction_tokens"`
}

type openAIErrorResponse struct {
	Error openAIError `json:"error"`
}

type openAIError struct {
	Message string      `json:"message"`
	Type    string      `json:"type"`
	Param   interface{} `json:"param"`
	Code    string      `json:"code,omitempty"`
}

func (api *WorkflowAPI) registerOpenAIRoutes(r *mux.Router) {
	r.HandleFunc("/v1/models", api.handleOpenAIModels).Methods(http.MethodGet)
	r.HandleFunc("/v1/models/{model}", api.handleOpenAIModel).Methods(http.MethodGet)
	r.HandleFunc("/v1/chat/completions", api.handleOpenAIChatCompletions).Methods(http.MethodPost)
	r.HandleFunc("/v1/chat/completions", api.handleOpenAIChatCompletionsList).Methods(http.MethodGet)
	r.HandleFunc("/v1/chat/completions/{completion_id}", api.handleOpenAIChatCompletionRetrieve).Methods(http.MethodGet)
	r.HandleFunc("/v1/chat/completions/{completion_id}/messages", api.handleOpenAIChatCompletionMessages).Methods(http.MethodGet)
	r.HandleFunc("/v1/responses", api.handleOpenAIResponses).Methods(http.MethodPost)
	r.HandleFunc("/v1/responses/{response_id}", api.handleOpenAIResponseRetrieve).Methods(http.MethodGet)
	r.HandleFunc("/v1/responses/{response_id}/input_items", api.handleOpenAIResponseInputItems).Methods(http.MethodGet)
	r.HandleFunc("/v1/responses/{response_id}/cancel", api.handleOpenAIResponseCancel).Methods(http.MethodPost)
}

func (api *WorkflowAPI) handleOpenAIModels(w http.ResponseWriter, r *http.Request) {
	auth, ok := api.authenticateOpenAI(w, r)
	if !ok {
		return
	}
	if api.rejectOpenAIReadRateLimited(w, auth, "/v1/models") {
		return
	}
	routes, err := api.storage.ListAPIModelRoutes(false)
	if err != nil {
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to list models", "server_error", "internal_error")
		return
	}
	data := make([]map[string]interface{}, 0, len(routes))
	for _, route := range routes {
		data = append(data, openAIModelObject(route))
	}
	api.respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"object": "list",
		"data":   data,
	})
}

func (api *WorkflowAPI) handleOpenAIModel(w http.ResponseWriter, r *http.Request) {
	auth, ok := api.authenticateOpenAI(w, r)
	if !ok {
		return
	}
	if api.rejectOpenAIReadRateLimited(w, auth, "/v1/models") {
		return
	}
	model := strings.TrimSpace(mux.Vars(r)["model"])
	route, err := api.storage.GetAPIModelRoute(model)
	if err != nil || route == nil || !route.Enabled {
		if err == nil || errors.Is(err, storage.ErrNotFound) {
			api.writeOpenAIError(w, http.StatusNotFound, "model not found", "invalid_request_error", "model_not_found")
			return
		}
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to retrieve model", "server_error", "internal_error")
		return
	}
	api.respondWithJSON(w, http.StatusOK, openAIModelObject(*route))
}

func openAIModelObject(route storage.APIModelRoute) map[string]interface{} {
	created := route.CreatedAt.Unix()
	if created <= 0 {
		created = time.Now().Unix()
	}
	return map[string]interface{}{
		"id":       route.APIModel,
		"object":   openAIObjectModel,
		"created":  created,
		"owned_by": "consortium",
	}
}

func (api *WorkflowAPI) handleOpenAIChatCompletionsList(w http.ResponseWriter, r *http.Request) {
	auth, ok := api.authenticateOpenAI(w, r)
	if !ok {
		return
	}
	if api.rejectOpenAIReadRateLimited(w, auth, openAIUsageEndpointChat) {
		return
	}
	pageReq := openAIListPageRequestFromHTTP(r)
	objects, page, err := api.storage.ListOpenAIObjects(storage.OpenAIObjectListFilters{
		KeyID:      auth.key.ID,
		ObjectType: storage.OpenAIObjectTypeChatCompletion,
		Limit:      pageReq.Limit,
		After:      pageReq.After,
		Order:      pageReq.Order,
	})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			api.writeOpenAIError(w, http.StatusNotFound, "chat completion not found", "invalid_request_error", "chat_completion_not_found")
			return
		}
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to list chat completions", "server_error", "internal_error")
		return
	}
	data := make([]interface{}, 0, len(objects))
	for i := range objects {
		data = append(data, openAIStoredObjectMap(&objects[i]))
	}
	api.respondWithJSON(w, http.StatusOK, openAIListEnvelope(data, page))
}

func (api *WorkflowAPI) handleOpenAIChatCompletionRetrieve(w http.ResponseWriter, r *http.Request) {
	auth, ok := api.authenticateOpenAI(w, r)
	if !ok {
		return
	}
	if api.rejectOpenAIReadRateLimited(w, auth, openAIUsageEndpointChat) {
		return
	}
	completionID := strings.TrimSpace(mux.Vars(r)["completion_id"])
	record, err := api.storage.GetOpenAIObject(completionID, auth.key.ID)
	if err != nil || record.ObjectType != storage.OpenAIObjectTypeChatCompletion {
		if err == nil || errors.Is(err, storage.ErrNotFound) {
			api.writeOpenAIError(w, http.StatusNotFound, "chat completion not found", "invalid_request_error", "chat_completion_not_found")
			return
		}
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to retrieve chat completion", "server_error", "internal_error")
		return
	}
	api.writeStoredOpenAIObject(w, record)
}

func (api *WorkflowAPI) handleOpenAIChatCompletionMessages(w http.ResponseWriter, r *http.Request) {
	auth, ok := api.authenticateOpenAI(w, r)
	if !ok {
		return
	}
	if api.rejectOpenAIReadRateLimited(w, auth, openAIUsageEndpointChat) {
		return
	}
	completionID := strings.TrimSpace(mux.Vars(r)["completion_id"])
	record, err := api.storage.GetOpenAIObject(completionID, auth.key.ID)
	if err != nil || record.ObjectType != storage.OpenAIObjectTypeChatCompletion {
		if err == nil || errors.Is(err, storage.ErrNotFound) {
			api.writeOpenAIError(w, http.StatusNotFound, "chat completion not found", "invalid_request_error", "chat_completion_not_found")
			return
		}
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to retrieve chat completion", "server_error", "internal_error")
		return
	}
	items, page, err := api.storage.ListOpenAIObjectItems(completionID, auth.key.ID, storage.OpenAIItemKindMessage, openAIListPageRequestFromHTTP(r))
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			api.writeOpenAIError(w, http.StatusNotFound, "chat completion not found", "invalid_request_error", "chat_completion_not_found")
			return
		}
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to list chat completion messages", "server_error", "internal_error")
		return
	}
	data := make([]interface{}, 0, len(items))
	for _, item := range items {
		data = append(data, openAIObjectItemPayload(item))
	}
	api.respondWithJSON(w, http.StatusOK, openAIListEnvelope(data, page))
}

func (api *WorkflowAPI) persistOpenAIChatCompletionObject(
	key *storage.APIKey,
	route openAIRouteResolution,
	req openAIChatCompletionRequest,
	bodyBytes []byte,
	resp openAIChatCompletionResponse,
	jobID string,
	responseJSON string,
	completedAt time.Time,
) error {
	metadataJSON, err := json.Marshal(openAIMetadataObject(req.Metadata))
	if err != nil {
		return err
	}
	usageJSON, err := json.Marshal(resp.Usage)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if completedAt.IsZero() {
		completedAt = now
	}
	return api.storage.CreateOpenAIObjectWithItems(&storage.OpenAIObjectRecord{
		ID:             resp.ID,
		ObjectType:     storage.OpenAIObjectTypeChatCompletion,
		KeyID:          key.ID,
		UserID:         key.UserID,
		Endpoint:       openAIUsageEndpointChat,
		JobID:          jobID,
		RequestedModel: req.Model,
		ResolvedModel:  route.ResolvedModel,
		WorkflowID:     route.WorkflowID,
		Status:         storage.OpenAIObjectStatusCompleted,
		Store:          true,
		MetadataJSON:   string(metadataJSON),
		RequestJSON:    string(bodyBytes),
		ResponseJSON:   responseJSON,
		UsageJSON:      string(usageJSON),
		CreatedAt:      now,
		UpdatedAt:      now,
		CompletedAt:    &completedAt,
	}, openAIChatStoredItems(resp.ID, req.Messages, resp))
}

func openAIChatStoredItems(completionID string, messages []openAIChatMessage, resp openAIChatCompletionResponse) []storage.OpenAIObjectItem {
	items := make([]storage.OpenAIObjectItem, 0, len(messages)+1)
	for i, msg := range messages {
		content, _ := normalizeOpenAIContent(msg.Content)
		messageID := "msg-" + uuid.NewString()
		payload := map[string]interface{}{
			"id":      messageID,
			"object":  "chat.completion.message",
			"role":    msg.Role,
			"content": content,
		}
		if msg.Name != "" {
			payload["name"] = msg.Name
		}
		raw, _ := json.Marshal(payload)
		contentJSON, _ := json.Marshal(map[string]interface{}{"text": content})
		items = append(items, storage.OpenAIObjectItem{
			ID:           "item-" + uuid.NewString(),
			ObjectID:     completionID,
			ItemKind:     storage.OpenAIItemKindMessage,
			ItemIndex:    i,
			OpenAIItemID: messageID,
			Role:         msg.Role,
			ContentJSON:  string(contentJSON),
			RawJSON:      string(raw),
		})
	}
	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		messageID := "msg-" + uuid.NewString()
		payload := map[string]interface{}{
			"id":      messageID,
			"object":  "chat.completion.message",
			"role":    choice.Message.Role,
			"content": choice.Message.Content,
		}
		if len(choice.Message.ToolCalls) > 0 {
			payload["tool_calls"] = choice.Message.ToolCalls
		}
		raw, _ := json.Marshal(payload)
		contentJSON, _ := json.Marshal(map[string]interface{}{"text": choice.Message.Content})
		items = append(items, storage.OpenAIObjectItem{
			ID:           "item-" + uuid.NewString(),
			ObjectID:     completionID,
			ItemKind:     storage.OpenAIItemKindMessage,
			ItemIndex:    len(items),
			OpenAIItemID: messageID,
			Role:         choice.Message.Role,
			ContentJSON:  string(contentJSON),
			RawJSON:      string(raw),
		})
	}
	return items
}

func (api *WorkflowAPI) handleOpenAIChatCompletions(w http.ResponseWriter, r *http.Request) {
	auth, ok := api.authenticateOpenAI(w, r)
	if !ok {
		return
	}

	started := time.Now().UTC()
	if retryAfter := api.checkOpenAIRequestRateLimit(auth.key); retryAfter > 0 {
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointChat, "", "", "", false, 0, http.StatusTooManyRequests, "rate_limit_exceeded", "rate limit exceeded", started)
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())))
		api.writeOpenAIError(w, http.StatusTooManyRequests, "rate limit exceeded", "rate_limit_error", "rate_limit_exceeded")
		return
	}

	bodyBytes, err := readOpenAIRequestBody(w, r)
	if err != nil {
		status := http.StatusBadRequest
		code := "invalid_request"
		message := "failed to read request body"
		if errors.Is(err, errOpenAIRequestBodyTooLarge) {
			status = http.StatusRequestEntityTooLarge
			code = "request_too_large"
			message = "request body too large"
		}
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointChat, "", "", "", false, 0, status, code, message, started)
		api.writeOpenAIError(w, status, message, "invalid_request_error", code)
		return
	}
	var req openAIChatCompletionRequest
	decoder := json.NewDecoder(bytes.NewReader(bodyBytes))
	decoder.UseNumber()
	if err := decoder.Decode(&req); err != nil {
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointChat, "", "", "", false, 0, http.StatusBadRequest, "invalid_json", "invalid JSON payload", started)
		api.writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload", "invalid_request_error", "invalid_json")
		return
	}
	if validation := validateOpenAIChatRequest(req); validation != nil {
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointChat, req.Model, "", "", req.Stream, 0, http.StatusBadRequest, validation.Code, validation.Message, started)
		api.writeOpenAIValidationError(w, validation)
		return
	}
	if req.Stream {
		api.streamOpenAIChatCompletion(w, r, auth, req, bodyBytes, started)
		return
	}
	if strings.TrimSpace(req.Model) == "" {
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointChat, req.Model, "", "", false, 0, http.StatusBadRequest, "missing_model", "model is required", started)
		api.writeOpenAIError(w, http.StatusBadRequest, "model is required", "invalid_request_error", "missing_model")
		return
	}
	if len(req.Messages) == 0 {
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointChat, req.Model, "", "", false, 0, http.StatusBadRequest, "missing_messages", "messages is required", started)
		api.writeOpenAIError(w, http.StatusBadRequest, "messages is required", "invalid_request_error", "missing_messages")
		return
	}
	normalized, err := normalizeOpenAIChatMessages(req.Messages)
	if err != nil {
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointChat, req.Model, "", "", false, 0, http.StatusBadRequest, "invalid_message_content", err.Error(), started)
		api.writeOpenAIError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "invalid_message_content")
		return
	}
	route, err := api.resolveOpenAIRoute(req.Model, auth.key)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointChat, req.Model, "", "", false, normalized.InputTokens, http.StatusNotFound, "model_not_found", "model route not found", started)
			api.writeOpenAIError(w, http.StatusNotFound, "model route not found", "invalid_request_error", "model_not_found")
			return
		}
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointChat, req.Model, "", "", false, normalized.InputTokens, http.StatusInternalServerError, "internal_error", "failed to resolve model route", started)
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to resolve model route", "server_error", "internal_error")
		return
	}
	submitWorkflow, err := api.buildOpenAIWorkflow(route, normalized, req)
	if err != nil {
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointChat, req.Model, route.ResolvedModel, route.WorkflowID, false, normalized.InputTokens, http.StatusBadRequest, "invalid_workflow", err.Error(), started)
		api.writeOpenAIError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "invalid_workflow")
		return
	}
	submitWorkflow, err = api.compileOpenAIWorkflowForSubmit(r.Context(), submitWorkflow)
	if err != nil {
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointChat, req.Model, route.ResolvedModel, route.WorkflowID, false, normalized.InputTokens, http.StatusBadRequest, "invalid_workflow", err.Error(), started)
		api.writeOpenAIError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "invalid_workflow")
		return
	}

	callerIdempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	idem, handled, apiIdempotencyKey := api.reserveOrReplayOpenAIIdempotency(w, r, auth.key.ID, openAIUsageEndpointChat, callerIdempotencyKey, bodyBytes, true, func(status int, code, message string) {
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointChat, req.Model, route.ResolvedModel, route.WorkflowID, false, normalized.InputTokens, status, code, message, started)
	})
	if handled {
		return
	}
	workflowIdempotencyKey := openAIWorkflowIdempotencyKey(auth.key.ID, openAIUsageEndpointChat, callerIdempotencyKey)

	usageID := "apiusage-" + uuid.NewString()
	usage := &storage.APIUsageRecord{
		ID:             usageID,
		RequestID:      "req-" + uuid.NewString(),
		KeyID:          auth.key.ID,
		UserID:         auth.key.UserID,
		Endpoint:       openAIUsageEndpointChat,
		RequestedModel: req.Model,
		ResolvedModel:  route.ResolvedModel,
		WorkflowID:     route.WorkflowID,
		Status:         storage.APIUsageStatusRunning,
		HTTPStatus:     http.StatusOK,
		Stream:         false,
		TokensInput:    normalized.InputTokens,
		CreatedAt:      started,
	}
	if err := api.storage.CreateAPIUsage(usage); err != nil {
		api.completeOpenAIIdempotencyError(idem, auth.key.ID, apiIdempotencyKey, http.StatusInternalServerError, "failed to create usage record", "server_error", "internal_error")
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to create usage record", "server_error", "internal_error")
		return
	}

	if retryAfter := api.checkOpenAITokenRateLimit(auth.key, estimateOpenAIRateLimitTokens(submitWorkflow, normalized, req)); retryAfter > 0 {
		api.completeOpenAIUsageFailure(usageID, http.StatusTooManyRequests, "rate_limit_exceeded", "rate limit exceeded", started)
		api.completeOpenAIIdempotencyError(idem, auth.key.ID, apiIdempotencyKey, http.StatusTooManyRequests, "rate limit exceeded", "rate_limit_error", "rate_limit_exceeded")
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())))
		api.writeOpenAIError(w, http.StatusTooManyRequests, "rate limit exceeded", "rate_limit_error", "rate_limit_exceeded")
		return
	}
	execResult, err := api.submitAndWaitOpenAIWorkflow(r.Context(), submitWorkflow, auth.key, workflowIdempotencyKey, func(jobID string) {
		api.attachOpenAIIdempotencyJob(idem, auth.key.ID, apiIdempotencyKey, jobID)
	})
	if err != nil {
		status, code := openAIStatusFromSubmitError(err)
		msg := openAIPublicSubmitErrorMessage(err, status)
		api.completeOpenAIUsageFailure(usageID, status, code, msg, started)
		api.completeOpenAIIdempotencyError(idem, auth.key.ID, apiIdempotencyKey, status, msg, openAIErrorTypeForStatus(status), code)
		api.writeOpenAIError(w, status, msg, openAIErrorTypeForStatus(status), code)
		return
	}
	if execResult == nil || execResult.Result == nil || !execResult.Success {
		msg := "workflow execution failed"
		code := "execution_failed"
		if execResult != nil && execResult.Error != "" {
			msg = execResult.Error
			code = execResult.ErrorCode
		}
		status, providerCode := openAIStatusFromExecutionResult(execResult)
		if providerCode != "" {
			code = providerCode
		}
		msg = openAIPublicExecutionErrorMessage(execResult, msg, status)
		api.completeOpenAIUsageFailure(usageID, status, code, msg, started)
		api.completeOpenAIIdempotencyError(idem, auth.key.ID, apiIdempotencyKey, status, msg, openAIErrorTypeForStatus(status), code)
		api.writeOpenAIError(w, status, msg, openAIErrorTypeForStatus(status), code)
		return
	}

	content := execResult.Result.FinalOutput
	var toolCalls []providers.ToolCall
	if route.Mode == storage.APIModelRouteModeDirectModel {
		toolCalls = extractOpenAIToolCalls(execResult.Result)
	}
	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}
	resp := openAIChatCompletionResponse{
		ID:                "chatcmpl-" + uuid.NewString(),
		Object:            openAIObjectChatCompletion,
		Created:           time.Now().Unix(),
		Model:             req.Model,
		SystemFingerprint: "fp_consortium",
		Choices: []openAIChatChoice{
			{
				Index: 0,
				Message: openAIChatMessageOut{
					Role:      "assistant",
					Content:   content,
					ToolCalls: toolCalls,
				},
				FinishReason: finishReason,
			},
		},
		Usage: chatUsageFromWorkflowResult(execResult.Result, normalized.InputTokens, content),
	}
	if req.Metadata != nil {
		resp.Metadata = openAIMetadataObject(req.Metadata)
	}

	completedAt := time.Now().UTC()
	usageCompletion := storage.APIUsageCompletion{
		JobID:        execResult.JobID,
		Status:       storage.APIUsageStatusSucceeded,
		HTTPStatus:   http.StatusOK,
		TokensInput:  resp.Usage.PromptTokens,
		TokensOutput: resp.Usage.CompletionTokens,
		TokensTotal:  resp.Usage.TotalTokens,
		Cost:         execResult.Result.TotalCost,
		LatencyMs:    float64(completedAt.Sub(started).Milliseconds()),
		CompletedAt:  completedAt,
	}
	respJSON, err := json.Marshal(resp)
	if err != nil {
		api.completeOpenAIUsageFailure(usageID, http.StatusInternalServerError, "internal_error", "failed to encode response", started)
		api.completeOpenAIIdempotencyTerminalError(idem, auth.key.ID, apiIdempotencyKey, http.StatusInternalServerError, "failed to encode response", "server_error", "internal_error")
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to encode response", "server_error", "internal_error")
		return
	}
	if openAIStoreEnabled(req.Store, false) {
		if err := api.persistOpenAIChatCompletionObject(auth.key, route, req, bodyBytes, resp, execResult.JobID, string(respJSON), completedAt); err != nil {
			api.completeOpenAIUsageFailure(usageID, http.StatusInternalServerError, "internal_error", "failed to persist chat completion object", started)
			api.completeOpenAIIdempotencyTerminalError(idem, auth.key.ID, apiIdempotencyKey, http.StatusInternalServerError, "failed to persist chat completion object", "server_error", "internal_error")
			api.writeOpenAIError(w, http.StatusInternalServerError, "failed to persist chat completion object", "server_error", "internal_error")
			return
		}
	}
	if err := api.storage.UpdateAPIUsageCompletion(usageID, usageCompletion); err != nil {
		api.completeOpenAIIdempotencyTerminalError(idem, auth.key.ID, apiIdempotencyKey, http.StatusInternalServerError, "failed to complete usage record", "server_error", "internal_error")
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to complete usage record", "server_error", "internal_error")
		return
	}
	if idem != nil {
		api.completeOpenAIIdempotencySuccess(idem, auth.key.ID, apiIdempotencyKey, string(respJSON), http.StatusOK, openAIRetainSuccessfulIdempotencyBody(req.Store))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(respJSON)
}

func (api *WorkflowAPI) handleOpenAIResponses(w http.ResponseWriter, r *http.Request) {
	auth, ok := api.authenticateOpenAI(w, r)
	if !ok {
		return
	}
	started := time.Now().UTC()
	if retryAfter := api.checkOpenAIRequestRateLimit(auth.key); retryAfter > 0 {
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointResponses, "", "", "", false, 0, http.StatusTooManyRequests, "rate_limit_exceeded", "rate limit exceeded", started)
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())))
		api.writeOpenAIError(w, http.StatusTooManyRequests, "rate limit exceeded", "rate_limit_error", "rate_limit_exceeded")
		return
	}

	bodyBytes, err := readOpenAIRequestBody(w, r)
	if err != nil {
		status := http.StatusBadRequest
		code := "invalid_request"
		message := "failed to read request body"
		if errors.Is(err, errOpenAIRequestBodyTooLarge) {
			status = http.StatusRequestEntityTooLarge
			code = "request_too_large"
			message = "request body too large"
		}
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointResponses, "", "", "", false, 0, status, code, message, started)
		api.writeOpenAIError(w, status, message, "invalid_request_error", code)
		return
	}
	var req openAIResponsesRequest
	decoder := json.NewDecoder(bytes.NewReader(bodyBytes))
	decoder.UseNumber()
	if err := decoder.Decode(&req); err != nil {
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointResponses, "", "", "", false, 0, http.StatusBadRequest, "invalid_json", "invalid JSON payload", started)
		api.writeOpenAIError(w, http.StatusBadRequest, "invalid JSON payload", "invalid_request_error", "invalid_json")
		return
	}
	normalizedResponsesTools, validation := normalizeOpenAIResponsesTools(req.Tools)
	if validation != nil {
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointResponses, req.Model, "", "", req.Stream, 0, http.StatusBadRequest, validation.Code, validation.Message, started)
		api.writeOpenAIValidationError(w, validation)
		return
	}
	if validation := validateOpenAIResponsesRequest(req, normalizedResponsesTools); validation != nil {
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointResponses, req.Model, "", "", req.Stream, 0, http.StatusBadRequest, validation.Code, validation.Message, started)
		api.writeOpenAIValidationError(w, validation)
		return
	}
	if strings.TrimSpace(req.Model) == "" {
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointResponses, req.Model, "", "", req.Stream, 0, http.StatusBadRequest, "missing_model", "model is required", started)
		api.writeOpenAIError(w, http.StatusBadRequest, "model is required", "invalid_request_error", "missing_model")
		return
	}
	input, err := normalizeOpenAIResponsesInput(req.Input)
	if err != nil {
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointResponses, req.Model, "", "", req.Stream, 0, http.StatusBadRequest, "invalid_input", err.Error(), started)
		api.writeOpenAIError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "invalid_input")
		return
	}
	if _, err := openAIResponseFunctionCallOutputIDs(req.Input); err != nil {
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointResponses, req.Model, "", "", req.Stream, 0, http.StatusBadRequest, "invalid_function_call_output", err.Error(), started)
		api.writeOpenAIError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "invalid_function_call_output")
		return
	}
	previousContext := ""
	if strings.TrimSpace(req.PreviousResponseID) != "" {
		previousContext, err = api.loadPreviousOpenAIResponseContext(auth.key.ID, req.PreviousResponseID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointResponses, req.Model, "", "", req.Stream, 0, http.StatusNotFound, "previous_response_not_found", "previous response not found", started)
				api.writeOpenAIError(w, http.StatusNotFound, "previous response not found", "invalid_request_error", "previous_response_not_found")
				return
			}
			if errors.Is(err, errOpenAIPreviousResponseNotCompleted) {
				api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointResponses, req.Model, "", "", req.Stream, 0, http.StatusBadRequest, "invalid_previous_response_id", "previous response is not completed", started)
				api.writeOpenAIError(w, http.StatusBadRequest, "previous response is not completed", "invalid_request_error", "invalid_previous_response_id")
				return
			}
			if errors.Is(err, errOpenAIPreviousResponseInvalid) {
				api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointResponses, req.Model, "", "", req.Stream, 0, http.StatusBadRequest, "invalid_previous_response_id", "invalid previous response chain", started)
				api.writeOpenAIError(w, http.StatusBadRequest, "invalid previous response chain", "invalid_request_error", "invalid_previous_response_id")
				return
			}
			api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointResponses, req.Model, "", "", req.Stream, 0, http.StatusInternalServerError, "internal_error", "failed to load previous response", started)
			api.writeOpenAIError(w, http.StatusInternalServerError, "failed to load previous response", "server_error", "internal_error")
			return
		}
	}
	if validation := api.validateOpenAIResponseFunctionCallOutputs(auth.key.ID, req.PreviousResponseID, req.Input); validation != nil {
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointResponses, req.Model, "", "", req.Stream, 0, http.StatusBadRequest, validation.Code, validation.Message, started)
		api.writeOpenAIValidationError(w, validation)
		return
	}
	promptInput := input
	if previousContext != "" {
		promptInput = strings.TrimSpace(previousContext + "\n\n" + input)
	}
	chatReq := openAIChatCompletionRequest{
		Model:                req.Model,
		Temperature:          req.Temperature,
		TopP:                 req.TopP,
		MaxCompletionTokens:  req.MaxOutputTokens,
		Stop:                 req.Stop,
		Tools:                normalizedResponsesTools,
		ToolChoice:           req.ToolChoice,
		ResponseFormat:       openAIResponseFormatForResponses(req),
		Seed:                 req.Seed,
		Metadata:             req.Metadata,
		Provider:             req.Provider,
		Order:                req.Order,
		AllowFallbacks:       req.AllowFallbacks,
		RequireParameters:    req.RequireParameters,
		ReasoningEffort:      req.Reasoning.Effort,
		ParallelToolCalls:    req.ParallelToolCalls,
		SessionID:            req.SessionID,
		PromptCacheKey:       req.PromptCacheKey,
		PromptCacheRetention: req.PromptCacheRetention,
	}
	prompt := openAINormalizedPrompt{
		SystemPrompt: req.Instructions,
		UserPrompt:   promptInput,
		InputTokens:  estimateOpenAITokens(req.Instructions + "\n" + promptInput),
	}
	route, err := api.resolveOpenAIRoute(req.Model, auth.key)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointResponses, req.Model, "", "", req.Stream, prompt.InputTokens, http.StatusNotFound, "model_not_found", "model route not found", started)
			api.writeOpenAIError(w, http.StatusNotFound, "model route not found", "invalid_request_error", "model_not_found")
			return
		}
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointResponses, req.Model, "", "", req.Stream, prompt.InputTokens, http.StatusInternalServerError, "internal_error", "failed to resolve model route", started)
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to resolve model route", "server_error", "internal_error")
		return
	}
	wf, err := api.buildOpenAIWorkflow(route, prompt, chatReq)
	if err != nil {
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointResponses, req.Model, route.ResolvedModel, route.WorkflowID, req.Stream, prompt.InputTokens, http.StatusBadRequest, "invalid_workflow", err.Error(), started)
		api.writeOpenAIError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "invalid_workflow")
		return
	}
	wf, err = api.compileOpenAIWorkflowForSubmit(r.Context(), wf)
	if err != nil {
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointResponses, req.Model, route.ResolvedModel, route.WorkflowID, req.Stream, prompt.InputTokens, http.StatusBadRequest, "invalid_workflow", err.Error(), started)
		api.writeOpenAIError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "invalid_workflow")
		return
	}

	callerIdempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	idem, handled, apiIdempotencyKey := api.reserveOrReplayOpenAIIdempotency(w, r, auth.key.ID, openAIUsageEndpointResponses, callerIdempotencyKey, bodyBytes, !req.Stream, func(status int, code, message string) {
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointResponses, req.Model, route.ResolvedModel, route.WorkflowID, req.Stream, prompt.InputTokens, status, code, message, started)
	})
	if handled {
		return
	}
	workflowIdempotencyKey := openAIWorkflowIdempotencyKey(auth.key.ID, openAIUsageEndpointResponses, callerIdempotencyKey)

	usageID := "apiusage-" + uuid.NewString()
	if err := api.storage.CreateAPIUsage(&storage.APIUsageRecord{
		ID:             usageID,
		RequestID:      "req-" + uuid.NewString(),
		KeyID:          auth.key.ID,
		UserID:         auth.key.UserID,
		Endpoint:       openAIUsageEndpointResponses,
		RequestedModel: req.Model,
		ResolvedModel:  route.ResolvedModel,
		WorkflowID:     route.WorkflowID,
		Status:         storage.APIUsageStatusRunning,
		HTTPStatus:     http.StatusOK,
		TokensInput:    prompt.InputTokens,
		CreatedAt:      started,
	}); err != nil {
		api.completeOpenAIIdempotencyError(idem, auth.key.ID, apiIdempotencyKey, http.StatusInternalServerError, "failed to create usage record", "server_error", "internal_error")
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to create usage record", "server_error", "internal_error")
		return
	}
	if retryAfter := api.checkOpenAITokenRateLimit(auth.key, estimateOpenAIRateLimitTokens(wf, prompt, chatReq)); retryAfter > 0 {
		api.completeOpenAIUsageFailure(usageID, http.StatusTooManyRequests, "rate_limit_exceeded", "rate limit exceeded", started)
		api.completeOpenAIIdempotencyError(idem, auth.key.ID, apiIdempotencyKey, http.StatusTooManyRequests, "rate limit exceeded", "rate_limit_error", "rate_limit_exceeded")
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())))
		api.writeOpenAIError(w, http.StatusTooManyRequests, "rate limit exceeded", "rate_limit_error", "rate_limit_exceeded")
		return
	}
	if req.Background {
		api.startOpenAIBackgroundResponse(w, auth, route, req, bodyBytes, prompt, input, wf, usageID, started, idem, apiIdempotencyKey, workflowIdempotencyKey)
		return
	}
	if req.Stream {
		api.streamOpenAIResponse(w, r, auth, req, prompt, wf, route.Mode, usageID, started, idem, apiIdempotencyKey, workflowIdempotencyKey)
		return
	}
	execResult, err := api.submitAndWaitOpenAIWorkflow(r.Context(), wf, auth.key, workflowIdempotencyKey, func(jobID string) {
		api.attachOpenAIIdempotencyJob(idem, auth.key.ID, apiIdempotencyKey, jobID)
	})
	if err != nil {
		status, code := openAIStatusFromSubmitError(err)
		msg := openAIPublicSubmitErrorMessage(err, status)
		api.completeOpenAIUsageFailure(usageID, status, code, msg, started)
		api.completeOpenAIIdempotencyError(idem, auth.key.ID, apiIdempotencyKey, status, msg, openAIErrorTypeForStatus(status), code)
		api.writeOpenAIError(w, status, msg, openAIErrorTypeForStatus(status), code)
		return
	}
	if execResult == nil || execResult.Result == nil || !execResult.Success {
		msg := "workflow execution failed"
		code := "execution_failed"
		if execResult != nil && execResult.Error != "" {
			msg = execResult.Error
			code = execResult.ErrorCode
		}
		status, providerCode := openAIStatusFromExecutionResult(execResult)
		if providerCode != "" {
			code = providerCode
		}
		msg = openAIPublicExecutionErrorMessage(execResult, msg, status)
		api.completeOpenAIUsageFailure(usageID, status, code, msg, started)
		api.completeOpenAIIdempotencyError(idem, auth.key.ID, apiIdempotencyKey, status, msg, openAIErrorTypeForStatus(status), code)
		api.writeOpenAIError(w, status, msg, openAIErrorTypeForStatus(status), code)
		return
	}
	usage := chatUsageFromWorkflowResult(execResult.Result, prompt.InputTokens, execResult.Result.FinalOutput)
	completedAt := time.Now().UTC()
	usageCompletion := storage.APIUsageCompletion{
		JobID:        execResult.JobID,
		Status:       storage.APIUsageStatusSucceeded,
		HTTPStatus:   http.StatusOK,
		TokensInput:  usage.PromptTokens,
		TokensOutput: usage.CompletionTokens,
		TokensTotal:  usage.TotalTokens,
		Cost:         execResult.Result.TotalCost,
		LatencyMs:    float64(completedAt.Sub(started).Milliseconds()),
		CompletedAt:  completedAt,
	}
	responseID := "resp-" + uuid.NewString()
	createdAt := time.Now().Unix()
	usageMap := openAIResponsesUsageMap(usage)
	outputText, outputItems := openAIResponseOutputItemsFromWorkflowResult(execResult.Result, route.Mode, storage.OpenAIObjectStatusCompleted)
	payload := openAIResponseObject(responseID, req.Model, createdAt, storage.OpenAIObjectStatusCompleted, outputText, outputItems, usageMap)
	payload["store"] = openAIStoreEnabled(req.Store, true)
	payload["background"] = false
	payload["metadata"] = openAIMetadataObject(req.Metadata)
	if req.PreviousResponseID != "" {
		payload["previous_response_id"] = strings.TrimSpace(req.PreviousResponseID)
	}
	respJSON, err := json.Marshal(payload)
	if err != nil {
		api.completeOpenAIUsageFailure(usageID, http.StatusInternalServerError, "internal_error", "failed to encode response", started)
		api.completeOpenAIIdempotencyTerminalError(idem, auth.key.ID, apiIdempotencyKey, http.StatusInternalServerError, "failed to encode response", "server_error", "internal_error")
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to encode response", "server_error", "internal_error")
		return
	}
	if openAIStoreEnabled(req.Store, true) {
		if err := api.persistOpenAIResponseObject(auth.key, route, req, bodyBytes, responseID, execResult.JobID, string(respJSON), usageMap, input, completedAt); err != nil {
			api.completeOpenAIUsageFailure(usageID, http.StatusInternalServerError, "internal_error", "failed to persist response object", started)
			api.completeOpenAIIdempotencyTerminalError(idem, auth.key.ID, apiIdempotencyKey, http.StatusInternalServerError, "failed to persist response object", "server_error", "internal_error")
			api.writeOpenAIError(w, http.StatusInternalServerError, "failed to persist response object", "server_error", "internal_error")
			return
		}
	}
	if err := api.storage.UpdateAPIUsageCompletion(usageID, usageCompletion); err != nil {
		api.completeOpenAIIdempotencyTerminalError(idem, auth.key.ID, apiIdempotencyKey, http.StatusInternalServerError, "failed to complete usage record", "server_error", "internal_error")
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to complete usage record", "server_error", "internal_error")
		return
	}
	if idem != nil {
		api.completeOpenAIIdempotencySuccess(idem, auth.key.ID, apiIdempotencyKey, string(respJSON), http.StatusOK, openAIRetainSuccessfulIdempotencyBody(req.Store))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(respJSON)
}

func (api *WorkflowAPI) handleOpenAIResponseRetrieve(w http.ResponseWriter, r *http.Request) {
	auth, ok := api.authenticateOpenAI(w, r)
	if !ok {
		return
	}
	if api.rejectOpenAIReadRateLimited(w, auth, openAIUsageEndpointResponses) {
		return
	}
	responseID := strings.TrimSpace(mux.Vars(r)["response_id"])
	record, err := api.storage.GetOpenAIObject(responseID, auth.key.ID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			api.writeOpenAIError(w, http.StatusNotFound, "response not found", "invalid_request_error", "response_not_found")
			return
		}
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to retrieve response", "server_error", "internal_error")
		return
	}
	if record.ObjectType != storage.OpenAIObjectTypeResponse {
		api.writeOpenAIError(w, http.StatusNotFound, "response not found", "invalid_request_error", "response_not_found")
		return
	}
	record, err = api.reconcileOpenAIBackgroundResponseIfTerminal(record)
	if err != nil {
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to reconcile response", "server_error", "internal_error")
		return
	}
	api.writeStoredOpenAIObject(w, record)
}

func (api *WorkflowAPI) handleOpenAIResponseInputItems(w http.ResponseWriter, r *http.Request) {
	auth, ok := api.authenticateOpenAI(w, r)
	if !ok {
		return
	}
	if api.rejectOpenAIReadRateLimited(w, auth, openAIUsageEndpointResponses) {
		return
	}
	responseID := strings.TrimSpace(mux.Vars(r)["response_id"])
	record, err := api.storage.GetOpenAIObject(responseID, auth.key.ID)
	if err != nil || record.ObjectType != storage.OpenAIObjectTypeResponse {
		if err == nil || errors.Is(err, storage.ErrNotFound) {
			api.writeOpenAIError(w, http.StatusNotFound, "response not found", "invalid_request_error", "response_not_found")
			return
		}
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to retrieve response", "server_error", "internal_error")
		return
	}
	if _, err := api.reconcileOpenAIBackgroundResponseIfTerminal(record); err != nil {
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to reconcile response", "server_error", "internal_error")
		return
	}
	items, page, err := api.storage.ListOpenAIObjectItems(responseID, auth.key.ID, storage.OpenAIItemKindInput, openAIListPageRequestFromHTTP(r))
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			api.writeOpenAIError(w, http.StatusNotFound, "response not found", "invalid_request_error", "response_not_found")
			return
		}
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to list response input items", "server_error", "internal_error")
		return
	}
	data := make([]interface{}, 0, len(items))
	for _, item := range items {
		data = append(data, openAIObjectItemPayload(item))
	}
	api.respondWithJSON(w, http.StatusOK, openAIListEnvelope(data, page))
}

func (api *WorkflowAPI) handleOpenAIResponseCancel(w http.ResponseWriter, r *http.Request) {
	auth, ok := api.authenticateOpenAI(w, r)
	if !ok {
		return
	}
	if api.rejectOpenAIReadRateLimited(w, auth, openAIUsageEndpointResponses) {
		return
	}
	responseID := strings.TrimSpace(mux.Vars(r)["response_id"])
	record, err := api.storage.GetOpenAIObject(responseID, auth.key.ID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			api.writeOpenAIError(w, http.StatusNotFound, "response not found", "invalid_request_error", "response_not_found")
			return
		}
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to retrieve response", "server_error", "internal_error")
		return
	}
	if record.ObjectType != storage.OpenAIObjectTypeResponse {
		api.writeOpenAIError(w, http.StatusNotFound, "response not found", "invalid_request_error", "response_not_found")
		return
	}
	if record.Status == storage.OpenAIObjectStatusCancelled {
		api.writeStoredOpenAIObject(w, record)
		return
	}
	if !record.Background || record.Status != storage.OpenAIObjectStatusInProgress {
		api.writeOpenAIError(w, http.StatusConflict, "response cannot be cancelled", "invalid_request_error", "response_not_cancellable")
		return
	}
	if strings.TrimSpace(record.JobID) != "" {
		if err := api.jobManager.CancelJob(record.JobID); err != nil {
			api.writeOpenAIError(w, http.StatusConflict, "response cannot be cancelled", "invalid_request_error", "response_not_cancellable")
			return
		}
	}
	payload := openAIStoredObjectMap(record)
	payload["status"] = storage.OpenAIObjectStatusCancelled
	payload["error"] = map[string]interface{}{
		"code":    "cancelled",
		"message": "Response cancelled by user request",
	}
	respJSON, err := json.Marshal(payload)
	if err != nil {
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to encode response", "server_error", "internal_error")
		return
	}
	if err := api.storage.UpdateOpenAIObjectCompletion(responseID, auth.key.ID, storage.OpenAIObjectCompletion{
		JobID:        record.JobID,
		Status:       storage.OpenAIObjectStatusCancelled,
		ResponseJSON: string(respJSON),
		UsageJSON:    record.UsageJSON,
		ErrorCode:    "cancelled",
		ErrorMessage: "Response cancelled by user request",
		CompletedAt:  time.Now().UTC(),
	}); err != nil {
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to update response", "server_error", "internal_error")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(respJSON)
}

func (api *WorkflowAPI) startOpenAIBackgroundResponse(
	w http.ResponseWriter,
	auth *openAIAuthContext,
	route openAIRouteResolution,
	req openAIResponsesRequest,
	bodyBytes []byte,
	prompt openAINormalizedPrompt,
	inputText string,
	wf *workflow.Workflow,
	usageID string,
	started time.Time,
	idem *storage.APIIdempotencyRecord,
	apiIdempotencyKey string,
	workflowIdempotencyKey string,
) {
	responseID := "resp-" + uuid.NewString()
	createdAt := time.Now().Unix()
	initialPayload := openAIResponseObject(responseID, req.Model, createdAt, storage.OpenAIObjectStatusInProgress, "", nil, nil)
	initialPayload["store"] = true
	initialPayload["background"] = true
	initialPayload["metadata"] = openAIMetadataObject(req.Metadata)
	if req.PreviousResponseID != "" {
		initialPayload["previous_response_id"] = strings.TrimSpace(req.PreviousResponseID)
	}
	initialJSON, err := json.Marshal(initialPayload)
	if err != nil {
		api.completeOpenAIUsageFailure(usageID, http.StatusInternalServerError, "internal_error", "failed to encode response", started)
		api.completeOpenAIIdempotencyError(idem, auth.key.ID, apiIdempotencyKey, http.StatusInternalServerError, "failed to encode response", "server_error", "internal_error")
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to encode response", "server_error", "internal_error")
		return
	}
	metadataJSON, err := json.Marshal(openAIMetadataObject(req.Metadata))
	if err != nil {
		api.completeOpenAIUsageFailure(usageID, http.StatusInternalServerError, "internal_error", "failed to encode response metadata", started)
		api.completeOpenAIIdempotencyError(idem, auth.key.ID, apiIdempotencyKey, http.StatusInternalServerError, "failed to encode response metadata", "server_error", "internal_error")
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to encode response metadata", "server_error", "internal_error")
		return
	}
	now := time.Now().UTC()
	if err := api.storage.CreateOpenAIObjectWithItems(&storage.OpenAIObjectRecord{
		ID:                 responseID,
		ObjectType:         storage.OpenAIObjectTypeResponse,
		KeyID:              auth.key.ID,
		UserID:             auth.key.UserID,
		Endpoint:           openAIUsageEndpointResponses,
		RequestedModel:     req.Model,
		ResolvedModel:      route.ResolvedModel,
		WorkflowID:         route.WorkflowID,
		Status:             storage.OpenAIObjectStatusInProgress,
		Store:              true,
		Background:         true,
		MetadataJSON:       string(metadataJSON),
		RequestJSON:        string(bodyBytes),
		ResponseJSON:       string(initialJSON),
		PreviousResponseID: req.PreviousResponseID,
		CreatedAt:          now,
		UpdatedAt:          now,
	}, openAIResponseInputStoredItems(responseID, req.Input, inputText)); err != nil {
		api.completeOpenAIUsageFailure(usageID, http.StatusInternalServerError, "internal_error", "failed to persist response object", started)
		api.completeOpenAIIdempotencyError(idem, auth.key.ID, apiIdempotencyKey, http.StatusInternalServerError, "failed to persist response object", "server_error", "internal_error")
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to persist response object", "server_error", "internal_error")
		return
	}
	submitResp, err := api.jobManager.SubmitWorkflow(context.Background(), &jobs.SubmitWorkflowRequest{
		Workflow:                wf,
		IdempotencyKey:          workflowIdempotencyKey,
		DisableRequestHashDedup: true,
		UserID:                  "api-key:" + auth.key.ID,
	})
	if err != nil {
		status, code := openAIStatusFromSubmitError(err)
		msg := openAIPublicSubmitErrorMessage(err, status)
		api.completeOpenAIUsageFailure(usageID, status, code, msg, started)
		api.completeOpenAIBackgroundResponseFailure(auth.key.ID, req, responseID, createdAt, idem, apiIdempotencyKey, code, msg)
		api.writeOpenAIError(w, status, msg, openAIErrorTypeForStatus(status), code)
		return
	}
	api.attachOpenAIIdempotencyJob(idem, auth.key.ID, apiIdempotencyKey, submitResp.JobID)
	_ = api.storage.AttachAPIUsageJob(usageID, submitResp.JobID)
	_ = api.storage.AttachOpenAIObjectJob(responseID, auth.key.ID, submitResp.JobID)
	if api.openAIBackgroundResponseCancelled(auth.key.ID, responseID) {
		_ = api.jobManager.CancelJob(submitResp.JobID)
	}
	if idem != nil || apiIdempotencyKey != "" {
		api.completeOpenAIIdempotencySuccess(idem, auth.key.ID, apiIdempotencyKey, string(initialJSON), http.StatusOK, true)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(initialJSON)

	go api.completeOpenAIBackgroundResponse(
		auth.key,
		req,
		prompt,
		inputText,
		wf,
		route.Mode,
		usageID,
		started,
		responseID,
		createdAt,
		idem,
		apiIdempotencyKey,
		workflowIdempotencyKey,
		submitResp.JobID,
	)
}

func (api *WorkflowAPI) completeOpenAIBackgroundResponse(
	key *storage.APIKey,
	req openAIResponsesRequest,
	prompt openAINormalizedPrompt,
	inputText string,
	wf *workflow.Workflow,
	routeMode string,
	usageID string,
	started time.Time,
	responseID string,
	createdAt int64,
	idem *storage.APIIdempotencyRecord,
	apiIdempotencyKey string,
	workflowIdempotencyKey string,
	jobID string,
) {
	execResult, err := api.jobManager.WaitForCompletion(context.Background(), jobID, wf.ID)
	if api.completeOpenAIBackgroundCancellation(key.ID, usageID, started, responseID, idem, apiIdempotencyKey) {
		return
	}
	if err != nil {
		status, code := openAIStatusFromSubmitError(err)
		msg := openAIPublicSubmitErrorMessage(err, status)
		api.completeOpenAIUsageFailure(usageID, status, code, msg, started)
		api.completeOpenAIBackgroundResponseFailure(key.ID, req, responseID, createdAt, idem, apiIdempotencyKey, code, msg)
		return
	}
	if execResult == nil || execResult.Result == nil || !execResult.Success {
		message := "workflow execution failed"
		code := "execution_failed"
		if execResult != nil && execResult.Error != "" {
			message = execResult.Error
			code = execResult.ErrorCode
		}
		status, providerCode := openAIStatusFromExecutionResult(execResult)
		if providerCode != "" {
			code = providerCode
		}
		message = openAIPublicExecutionErrorMessage(execResult, message, status)
		api.completeOpenAIUsageFailure(usageID, status, code, message, started)
		api.completeOpenAIBackgroundResponseFailure(key.ID, req, responseID, createdAt, idem, apiIdempotencyKey, code, message)
		return
	}

	content := execResult.Result.FinalOutput
	usage := chatUsageFromWorkflowResult(execResult.Result, prompt.InputTokens, content)
	usageMap := openAIResponsesUsageMap(usage)
	outputText, outputItems := openAIResponseOutputItemsFromWorkflowResult(execResult.Result, routeMode, storage.OpenAIObjectStatusCompleted)
	completedAt := time.Now().UTC()
	payload := openAIResponseObject(responseID, req.Model, createdAt, storage.OpenAIObjectStatusCompleted, outputText, outputItems, usageMap)
	payload["store"] = true
	payload["background"] = true
	payload["metadata"] = openAIMetadataObject(req.Metadata)
	if req.PreviousResponseID != "" {
		payload["previous_response_id"] = strings.TrimSpace(req.PreviousResponseID)
	}
	respJSON, err := json.Marshal(payload)
	if err != nil {
		api.completeOpenAIUsageFailure(usageID, http.StatusInternalServerError, "internal_error", "failed to encode response", started)
		api.completeOpenAIBackgroundResponseFailure(key.ID, req, responseID, createdAt, idem, apiIdempotencyKey, "internal_error", "failed to encode response")
		return
	}
	usageJSON, err := json.Marshal(usageMap)
	if err != nil {
		api.completeOpenAIUsageFailure(usageID, http.StatusInternalServerError, "internal_error", "failed to encode response usage", started)
		api.completeOpenAIBackgroundResponseFailure(key.ID, req, responseID, createdAt, idem, apiIdempotencyKey, "internal_error", "failed to encode response usage")
		return
	}
	inputItems, _, err := api.storage.ListOpenAIObjectItems(responseID, key.ID, storage.OpenAIItemKindInput, storage.OpenAIListPageRequest{Limit: 100, Order: storage.OpenAIListOrderAsc})
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) && api.completeOpenAIBackgroundCancellation(key.ID, usageID, started, responseID, idem, apiIdempotencyKey) {
			return
		}
		api.completeOpenAIUsageFailure(usageID, http.StatusInternalServerError, "internal_error", "failed to load background response input items", started)
		api.completeOpenAIBackgroundResponseFailure(key.ID, req, responseID, createdAt, idem, apiIdempotencyKey, "internal_error", "failed to load background response input items")
		return
	}
	if err := api.storage.CompleteOpenAIObjectWithItemsUsageAndIdempotency(
		responseID,
		key.ID,
		storage.OpenAIObjectCompletion{
			JobID:        execResult.JobID,
			Status:       storage.OpenAIObjectStatusCompleted,
			ResponseJSON: string(respJSON),
			UsageJSON:    string(usageJSON),
			CompletedAt:  completedAt,
		},
		openAIResponseStoredItemsFromInputs(responseID, inputItems, string(respJSON)),
		usageID,
		storage.APIUsageCompletion{
			JobID:        execResult.JobID,
			Status:       storage.APIUsageStatusSucceeded,
			HTTPStatus:   http.StatusOK,
			TokensInput:  usage.PromptTokens,
			TokensOutput: usage.CompletionTokens,
			TokensTotal:  usage.TotalTokens,
			Cost:         execResult.Result.TotalCost,
			LatencyMs:    float64(completedAt.Sub(started).Milliseconds()),
			CompletedAt:  completedAt,
		},
		idempotencyRecordID(idem),
		apiIdempotencyKey,
		string(respJSON),
		http.StatusOK,
	); err != nil {
		if errors.Is(err, storage.ErrNotFound) && api.completeOpenAIBackgroundCancellation(key.ID, usageID, started, responseID, idem, apiIdempotencyKey) {
			return
		}
		api.completeOpenAIUsageFailure(usageID, http.StatusInternalServerError, "internal_error", "failed to persist background response", started)
		api.completeOpenAIBackgroundResponseFailure(key.ID, req, responseID, createdAt, idem, apiIdempotencyKey, "internal_error", "failed to persist background response")
		return
	}
}

func (api *WorkflowAPI) completeOpenAIBackgroundResponseFailure(keyID string, req openAIResponsesRequest, responseID string, createdAt int64, idem *storage.APIIdempotencyRecord, apiIdempotencyKey, code, message string) {
	completedAt := time.Now().UTC()
	payload := openAIResponseObject(responseID, req.Model, createdAt, storage.OpenAIObjectStatusFailed, "", nil, nil)
	payload["store"] = true
	payload["background"] = true
	payload["metadata"] = openAIMetadataObject(req.Metadata)
	if req.PreviousResponseID != "" {
		payload["previous_response_id"] = strings.TrimSpace(req.PreviousResponseID)
	}
	payload["error"] = map[string]interface{}{
		"code":    code,
		"message": message,
	}
	respJSON, err := json.Marshal(payload)
	if err != nil {
		return
	}
	if err := api.storage.UpdateOpenAIObjectCompletion(responseID, keyID, storage.OpenAIObjectCompletion{
		Status:       storage.OpenAIObjectStatusFailed,
		ResponseJSON: string(respJSON),
		ErrorCode:    code,
		ErrorMessage: message,
		CompletedAt:  completedAt,
	}); err != nil {
		if errors.Is(err, storage.ErrNotFound) && apiIdempotencyKey != "" {
			if record, getErr := api.storage.GetOpenAIObject(responseID, keyID); getErr == nil && record.Status == storage.OpenAIObjectStatusCancelled && strings.TrimSpace(record.ResponseJSON) != "" {
				api.completeOpenAIIdempotencySuccess(idem, keyID, apiIdempotencyKey, record.ResponseJSON, http.StatusOK, true)
			}
		}
		return
	}
	if idem != nil || apiIdempotencyKey != "" {
		api.completeOpenAIIdempotencySuccess(idem, keyID, apiIdempotencyKey, string(respJSON), http.StatusOK, true)
	}
}

func (api *WorkflowAPI) openAIBackgroundResponseCancelled(keyID, responseID string) bool {
	record, err := api.storage.GetOpenAIObject(responseID, keyID)
	return err == nil && record.Status == storage.OpenAIObjectStatusCancelled
}

func (api *WorkflowAPI) completeOpenAIBackgroundCancellation(keyID, usageID string, started time.Time, responseID string, idem *storage.APIIdempotencyRecord, apiIdempotencyKey string) bool {
	record, err := api.storage.GetOpenAIObject(responseID, keyID)
	if err != nil || record.Status != storage.OpenAIObjectStatusCancelled {
		return false
	}
	completedAt := time.Now().UTC()
	_ = api.storage.UpdateAPIUsageCompletion(usageID, storage.APIUsageCompletion{
		Status:       storage.APIUsageStatusCancelled,
		HTTPStatus:   http.StatusOK,
		LatencyMs:    float64(completedAt.Sub(started).Milliseconds()),
		ErrorCode:    "cancelled",
		ErrorMessage: "response cancelled",
		CompletedAt:  completedAt,
	})
	if (idem != nil || apiIdempotencyKey != "") && strings.TrimSpace(record.ResponseJSON) != "" {
		api.completeOpenAIIdempotencySuccess(idem, keyID, apiIdempotencyKey, record.ResponseJSON, http.StatusOK, true)
	}
	return true
}

func (api *WorkflowAPI) persistOpenAIResponseObject(
	key *storage.APIKey,
	route openAIRouteResolution,
	req openAIResponsesRequest,
	bodyBytes []byte,
	responseID string,
	jobID string,
	responseJSON string,
	usageMap map[string]interface{},
	inputText string,
	completedAt time.Time,
) error {
	metadataJSON, err := json.Marshal(openAIMetadataObject(req.Metadata))
	if err != nil {
		return err
	}
	usageJSON, err := json.Marshal(usageMap)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if completedAt.IsZero() {
		completedAt = now
	}
	return api.storage.CreateOpenAIObjectWithItems(&storage.OpenAIObjectRecord{
		ID:                 responseID,
		ObjectType:         storage.OpenAIObjectTypeResponse,
		KeyID:              key.ID,
		UserID:             key.UserID,
		Endpoint:           openAIUsageEndpointResponses,
		JobID:              jobID,
		RequestedModel:     req.Model,
		ResolvedModel:      route.ResolvedModel,
		WorkflowID:         route.WorkflowID,
		Status:             storage.OpenAIObjectStatusCompleted,
		Store:              true,
		Background:         false,
		MetadataJSON:       string(metadataJSON),
		RequestJSON:        string(bodyBytes),
		ResponseJSON:       responseJSON,
		UsageJSON:          string(usageJSON),
		PreviousResponseID: req.PreviousResponseID,
		CreatedAt:          now,
		UpdatedAt:          now,
		CompletedAt:        &completedAt,
	}, openAIResponseStoredItems(responseID, req.Input, inputText, responseJSON))
}

func openAIResponseStoredItems(responseID string, rawInput json.RawMessage, inputText, responseJSON string) []storage.OpenAIObjectItem {
	items := openAIResponseInputStoredItems(responseID, rawInput, inputText)
	return openAIResponseStoredItemsFromInputs(responseID, items, responseJSON)
}

func openAIResponseStoredItemsFromInputs(responseID string, inputItems []storage.OpenAIObjectItem, responseJSON string) []storage.OpenAIObjectItem {
	items := append([]storage.OpenAIObjectItem(nil), inputItems...)
	for _, payload := range openAIResponseOutputsFromJSON(responseJSON) {
		outputContent := []byte("{}")
		outputItemID, _ := payload["id"].(string)
		role, _ := payload["role"].(string)
		outputRaw, _ := json.Marshal(payload)
		if content := openAITextContent(payload); content != "" {
			outputContent, _ = json.Marshal(map[string]interface{}{"text": content})
		}
		items = append(items, storage.OpenAIObjectItem{
			ID:           "item-" + uuid.NewString(),
			ObjectID:     responseID,
			ItemKind:     storage.OpenAIItemKindOutput,
			ItemIndex:    len(items),
			OpenAIItemID: outputItemID,
			Role:         role,
			ContentJSON:  string(outputContent),
			RawJSON:      string(outputRaw),
		})
	}
	return items
}

func openAIResponseInputStoredItems(responseID string, rawInput json.RawMessage, inputText string) []storage.OpenAIObjectItem {
	if typed := openAIResponseTypedInputStoredItems(responseID, rawInput); len(typed) > 0 {
		return typed
	}
	inputItemID := "msg-" + uuid.NewString()
	inputPayload := map[string]interface{}{
		"id":   inputItemID,
		"type": "message",
		"role": "user",
		"content": []map[string]interface{}{
			{"type": "input_text", "text": inputText},
		},
	}
	inputRaw, _ := json.Marshal(inputPayload)
	inputContent, _ := json.Marshal(map[string]interface{}{"text": inputText, "raw_input": json.RawMessage(rawInput)})
	return []storage.OpenAIObjectItem{
		{
			ID:           "item-" + uuid.NewString(),
			ObjectID:     responseID,
			ItemKind:     storage.OpenAIItemKindInput,
			ItemIndex:    0,
			OpenAIItemID: inputItemID,
			Role:         "user",
			ContentJSON:  string(inputContent),
			RawJSON:      string(inputRaw),
		},
	}
}

func openAIResponseTypedInputStoredItems(responseID string, rawInput json.RawMessage) []storage.OpenAIObjectItem {
	var rawItems []json.RawMessage
	if err := json.Unmarshal(rawInput, &rawItems); err != nil {
		rawItems = []json.RawMessage{rawInput}
	}
	items := make([]storage.OpenAIObjectItem, 0, len(rawItems))
	for _, rawItem := range rawItems {
		if payload, ok := openAIResponseFunctionCallPayload(rawItem); ok {
			openAIItemID, _ := payload["id"].(string)
			rawJSON, _ := json.Marshal(payload)
			contentJSON, _ := json.Marshal(map[string]interface{}{
				"type":      "function_call",
				"call_id":   payload["call_id"],
				"name":      payload["name"],
				"arguments": payload["arguments"],
			})
			items = append(items, storage.OpenAIObjectItem{
				ID:           "item-" + uuid.NewString(),
				ObjectID:     responseID,
				ItemKind:     storage.OpenAIItemKindInput,
				ItemIndex:    len(items),
				OpenAIItemID: openAIItemID,
				ContentJSON:  string(contentJSON),
				RawJSON:      string(rawJSON),
			})
			continue
		}
		if payload, text, role, ok := openAIResponseMessageInputPayload(rawItem); ok {
			openAIItemID, _ := payload["id"].(string)
			rawJSON, _ := json.Marshal(payload)
			contentJSON, _ := json.Marshal(map[string]interface{}{"text": text})
			items = append(items, storage.OpenAIObjectItem{
				ID:           "item-" + uuid.NewString(),
				ObjectID:     responseID,
				ItemKind:     storage.OpenAIItemKindInput,
				ItemIndex:    len(items),
				OpenAIItemID: openAIItemID,
				Role:         role,
				ContentJSON:  string(contentJSON),
				RawJSON:      string(rawJSON),
			})
			continue
		}
		payload, text, ok := openAIResponseFunctionCallOutputPayload(rawItem)
		if !ok {
			return nil
		}
		openAIItemID, _ := payload["id"].(string)
		rawJSON, _ := json.Marshal(payload)
		contentJSON, _ := json.Marshal(map[string]interface{}{
			"type":    "function_call_output",
			"text":    text,
			"call_id": payload["call_id"],
			"output":  text,
		})
		items = append(items, storage.OpenAIObjectItem{
			ID:           "item-" + uuid.NewString(),
			ObjectID:     responseID,
			ItemKind:     storage.OpenAIItemKindInput,
			ItemIndex:    len(items),
			OpenAIItemID: openAIItemID,
			ContentJSON:  string(contentJSON),
			RawJSON:      string(rawJSON),
		})
	}
	return items
}

func openAIResponseMessageInputPayload(raw json.RawMessage) (map[string]interface{}, string, string, bool) {
	text, err := normalizeOpenAIResponseInputItem(raw)
	if err != nil || strings.TrimSpace(text) == "" {
		return nil, "", "", false
	}
	role := "user"
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err == nil {
		itemType := strings.TrimSpace(jsonStringField(object, "type"))
		if itemType != "" && itemType != "message" && itemType != "input_text" && itemType != "text" && itemType != "output_text" {
			return nil, "", "", false
		}
		if rawRole := strings.TrimSpace(jsonStringField(object, "role")); rawRole != "" {
			role = rawRole
		}
	}
	itemID := "msg-" + uuid.NewString()
	return map[string]interface{}{
		"id":   itemID,
		"type": "message",
		"role": role,
		"content": []map[string]interface{}{
			{"type": "input_text", "text": text},
		},
	}, text, role, true
}

func openAIResponseFunctionCallPayload(raw json.RawMessage) (map[string]interface{}, bool) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, false
	}
	if strings.TrimSpace(jsonStringField(object, "type")) != "function_call" {
		return nil, false
	}
	callID := strings.TrimSpace(jsonStringField(object, "call_id"))
	name := strings.TrimSpace(jsonStringField(object, "name"))
	if callID == "" || name == "" {
		return nil, false
	}
	itemID := strings.TrimSpace(jsonStringField(object, "id"))
	if itemID == "" {
		itemID = "fc-" + uuid.NewString()
	}
	status := strings.TrimSpace(jsonStringField(object, "status"))
	if status == "" {
		status = storage.OpenAIObjectStatusCompleted
	}
	return map[string]interface{}{
		"id":        itemID,
		"type":      "function_call",
		"call_id":   callID,
		"name":      name,
		"arguments": jsonStringField(object, "arguments"),
		"status":    status,
	}, true
}

func openAIResponseFunctionCallOutputPayload(raw json.RawMessage) (map[string]interface{}, string, bool) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, "", false
	}
	itemType := strings.TrimSpace(jsonStringField(object, "type"))
	if itemType != "function_call_output" && itemType != "tool_result" {
		return nil, "", false
	}
	callID := strings.TrimSpace(jsonStringField(object, "call_id"))
	if callID == "" {
		return nil, "", false
	}
	output := ""
	for _, key := range []string{"output", "content", "text"} {
		if output = jsonStringField(object, key); output != "" {
			break
		}
	}
	if output == "" {
		return nil, "", false
	}
	itemID := strings.TrimSpace(jsonStringField(object, "id"))
	if itemID == "" {
		itemID = "fco-" + uuid.NewString()
	}
	return map[string]interface{}{
		"id":      itemID,
		"type":    "function_call_output",
		"call_id": callID,
		"output":  output,
	}, output, true
}

func (api *WorkflowAPI) loadPreviousOpenAIResponseContext(keyID, responseID string) (string, error) {
	records, err := api.loadPreviousOpenAIResponseChain(keyID, responseID)
	if err != nil {
		return "", err
	}
	var parts []string
	for _, record := range records {
		inputItems, _, err := api.storage.ListOpenAIObjectItems(record.ID, keyID, storage.OpenAIItemKindInput, storage.OpenAIListPageRequest{Limit: 100, Order: storage.OpenAIListOrderAsc})
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			return "", err
		}
		for _, item := range inputItems {
			if line := openAIResponseTypedTranscriptLine(item); line != "" {
				parts = append(parts, line)
			} else if text := openAIItemContentText(item); text != "" {
				parts = append(parts, "Previous user: "+text)
			}
		}
		outputItems, _, err := api.storage.ListOpenAIObjectItems(record.ID, keyID, storage.OpenAIItemKindOutput, storage.OpenAIListPageRequest{Limit: 100, Order: storage.OpenAIListOrderAsc})
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			return "", err
		}
		for _, item := range outputItems {
			if line := openAIResponseTypedTranscriptLine(item); line != "" {
				parts = append(parts, line)
			} else if text := openAIItemContentText(item); text != "" {
				parts = append(parts, "Previous assistant: "+text)
			}
		}
		if len(inputItems) == 0 && len(outputItems) == 0 {
			payload := openAIStoredObjectMap(record)
			if text, _ := payload["output_text"].(string); text != "" {
				parts = append(parts, "Previous assistant: "+text)
			}
		}
	}
	return strings.Join(parts, "\n"), nil
}

func (api *WorkflowAPI) loadPreviousOpenAIResponseChain(keyID, responseID string) ([]*storage.OpenAIObjectRecord, error) {
	responseID = strings.TrimSpace(responseID)
	if responseID == "" {
		return nil, nil
	}
	seen := make(map[string]struct{})
	reversed := make([]*storage.OpenAIObjectRecord, 0, 1)
	for responseID != "" {
		if _, ok := seen[responseID]; ok {
			return nil, errOpenAIPreviousResponseInvalid
		}
		if len(reversed) >= openAIPreviousResponseMaxHops {
			return nil, errOpenAIPreviousResponseInvalid
		}
		seen[responseID] = struct{}{}
		record, err := api.storage.GetOpenAIObject(responseID, keyID)
		if err != nil {
			return nil, err
		}
		if record.ObjectType != storage.OpenAIObjectTypeResponse {
			return nil, storage.ErrNotFound
		}
		if record.Status != storage.OpenAIObjectStatusCompleted {
			return nil, errOpenAIPreviousResponseNotCompleted
		}
		reversed = append(reversed, record)
		responseID = strings.TrimSpace(record.PreviousResponseID)
	}
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return reversed, nil
}

func (api *WorkflowAPI) writeStoredOpenAIObject(w http.ResponseWriter, record *storage.OpenAIObjectRecord) {
	api.respondWithJSON(w, http.StatusOK, openAIStoredObjectMap(record))
}

func (api *WorkflowAPI) StartOpenAIBackgroundReconciler(ctx context.Context, interval time.Duration, limit int) {
	if api == nil || api.storage == nil || api.jobManager == nil {
		return
	}
	if interval <= 0 {
		interval = time.Minute
	}
	if limit <= 0 {
		limit = 100
	}
	go func() {
		api.logOpenAIBackgroundReconcile(ctx, limit)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				api.logOpenAIBackgroundReconcile(ctx, limit)
			}
		}
	}()
}

func (api *WorkflowAPI) logOpenAIBackgroundReconcile(ctx context.Context, limit int) {
	reconciled, err := api.reconcileTerminalOpenAIBackgroundResponses(ctx, limit)
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("OpenAI background reconcile failed: %v", err)
		return
	}
	if reconciled > 0 {
		log.Printf("OpenAI background reconcile completed %d terminal response(s)", reconciled)
	}
}

func (api *WorkflowAPI) reconcileTerminalOpenAIBackgroundResponses(ctx context.Context, limit int) (int, error) {
	if api == nil || api.storage == nil {
		return 0, nil
	}
	records, err := api.storage.ListInProgressOpenAIBackgroundObjects(limit)
	if err != nil {
		return 0, err
	}
	reconciled := 0
	for i := range records {
		select {
		case <-ctx.Done():
			return reconciled, ctx.Err()
		default:
		}
		before := records[i].Status
		updated, err := api.reconcileOpenAIBackgroundResponseIfTerminal(&records[i])
		if err != nil {
			return reconciled, err
		}
		if updated != nil && before == storage.OpenAIObjectStatusInProgress && updated.Status != storage.OpenAIObjectStatusInProgress {
			reconciled++
		}
	}
	return reconciled, nil
}

func (api *WorkflowAPI) reconcileOpenAIBackgroundResponseIfTerminal(record *storage.OpenAIObjectRecord) (*storage.OpenAIObjectRecord, error) {
	if record == nil || !record.Background || record.Status != storage.OpenAIObjectStatusInProgress || strings.TrimSpace(record.JobID) == "" {
		return record, nil
	}
	job, err := api.storage.GetExecution(record.JobID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return record, nil
		}
		return nil, err
	}
	if !events.IsTerminalStatus(job.Status) {
		return record, nil
	}

	workflowID := strings.TrimSpace(record.WorkflowID)
	if workflowID == "" {
		workflowID = job.WorkflowID
	}
	if job.Status == events.JobStatusCancelled {
		if err := api.reconcileOpenAIBackgroundResponseCancelled(record); err != nil {
			return nil, err
		}
		return api.storage.GetOpenAIObject(record.ID, record.KeyID)
	}

	execResult, err := api.jobManager.WaitForCompletion(context.Background(), record.JobID, workflowID)
	if err != nil {
		return nil, err
	}
	req := openAIResponsesRequest{
		Model:    firstNonEmpty(record.RequestedModel, record.ResolvedModel),
		Metadata: map[string]interface{}{},
	}
	if strings.TrimSpace(record.RequestJSON) != "" {
		_ = json.Unmarshal([]byte(record.RequestJSON), &req)
	}
	if strings.TrimSpace(req.Model) == "" {
		req.Model = firstNonEmpty(record.RequestedModel, record.ResolvedModel)
	}
	if execResult == nil || execResult.Result == nil || !execResult.Success {
		if err := api.reconcileOpenAIBackgroundResponseFailed(record, req, execResult); err != nil {
			return nil, err
		}
		return api.storage.GetOpenAIObject(record.ID, record.KeyID)
	}

	content := execResult.Result.FinalOutput
	usage := chatUsageFromWorkflowResult(execResult.Result, 0, content)
	usageMap := openAIResponsesUsageMap(usage)
	routeMode := storage.APIModelRouteModeDirectModel
	if strings.TrimSpace(record.WorkflowID) != "" {
		routeMode = storage.APIModelRouteModeWorkflow
	}
	outputText, outputItems := openAIResponseOutputItemsFromWorkflowResult(execResult.Result, routeMode, storage.OpenAIObjectStatusCompleted)
	payload := openAIResponseObject(record.ID, req.Model, record.CreatedAt.Unix(), storage.OpenAIObjectStatusCompleted, outputText, outputItems, usageMap)
	payload["store"] = true
	payload["background"] = true
	payload["metadata"] = openAIMetadataObject(req.Metadata)
	if req.PreviousResponseID != "" {
		payload["previous_response_id"] = strings.TrimSpace(req.PreviousResponseID)
	}
	respJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	usageJSON, err := json.Marshal(usageMap)
	if err != nil {
		return nil, err
	}
	inputItems, _, err := api.storage.ListOpenAIObjectItems(record.ID, record.KeyID, storage.OpenAIItemKindInput, storage.OpenAIListPageRequest{Limit: 100, Order: storage.OpenAIListOrderAsc})
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	completedAt := time.Now().UTC()
	items := openAIResponseStoredItemsFromInputs(record.ID, inputItems, string(respJSON))
	if err := api.storage.CompleteOpenAIObjectWithItemsUsageAndIdempotencyByJob(
		record.ID,
		record.KeyID,
		storage.OpenAIObjectCompletion{
			JobID:        record.JobID,
			Status:       storage.OpenAIObjectStatusCompleted,
			ResponseJSON: string(respJSON),
			UsageJSON:    string(usageJSON),
			CompletedAt:  completedAt,
		},
		items,
		openAIUsageEndpointResponses,
		record.JobID,
		storage.APIUsageCompletion{
			JobID:        record.JobID,
			Status:       storage.APIUsageStatusSucceeded,
			HTTPStatus:   http.StatusOK,
			TokensInput:  usage.PromptTokens,
			TokensOutput: usage.CompletionTokens,
			TokensTotal:  usage.TotalTokens,
			Cost:         execResult.Result.TotalCost,
			LatencyMs:    float64(completedAt.Sub(record.CreatedAt).Milliseconds()),
			CompletedAt:  completedAt,
		},
		string(respJSON),
		http.StatusOK,
	); err != nil {
		if !errors.Is(err, storage.ErrNotFound) {
			return nil, err
		}
		if fallbackErr := api.reconcileOpenAIBackgroundResponseCompletedWithoutUsage(record, string(respJSON), string(usageJSON), items, completedAt); fallbackErr != nil {
			return nil, fallbackErr
		}
	}
	return api.storage.GetOpenAIObject(record.ID, record.KeyID)
}

func (api *WorkflowAPI) reconcileOpenAIBackgroundResponseCompletedWithoutUsage(record *storage.OpenAIObjectRecord, respJSON, usageJSON string, items []storage.OpenAIObjectItem, completedAt time.Time) error {
	if err := api.storage.UpdateOpenAIObjectCompletion(record.ID, record.KeyID, storage.OpenAIObjectCompletion{
		JobID:        record.JobID,
		Status:       storage.OpenAIObjectStatusCompleted,
		ResponseJSON: respJSON,
		UsageJSON:    usageJSON,
		CompletedAt:  completedAt,
	}); err != nil {
		return err
	}
	if err := api.storage.ReplaceOpenAIObjectItems(record.ID, record.KeyID, items); err != nil {
		return err
	}
	return nil
}

func (api *WorkflowAPI) reconcileOpenAIBackgroundResponseCancelled(record *storage.OpenAIObjectRecord) error {
	payload := openAIStoredObjectMap(record)
	payload["status"] = storage.OpenAIObjectStatusCancelled
	payload["error"] = map[string]interface{}{
		"code":    "cancelled",
		"message": "Response cancelled by user request",
	}
	respJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	completedAt := time.Now().UTC()
	if err := api.storage.UpdateOpenAIObjectCompletion(record.ID, record.KeyID, storage.OpenAIObjectCompletion{
		JobID:        record.JobID,
		Status:       storage.OpenAIObjectStatusCancelled,
		ResponseJSON: string(respJSON),
		ErrorCode:    "cancelled",
		ErrorMessage: "Response cancelled by user request",
		CompletedAt:  completedAt,
	}); err != nil {
		return err
	}
	_ = api.storage.UpdateAPIUsageCompletionByJob(record.KeyID, openAIUsageEndpointResponses, record.JobID, storage.APIUsageCompletion{
		JobID:        record.JobID,
		Status:       storage.APIUsageStatusCancelled,
		HTTPStatus:   http.StatusOK,
		LatencyMs:    float64(completedAt.Sub(record.CreatedAt).Milliseconds()),
		ErrorCode:    "cancelled",
		ErrorMessage: "response cancelled",
		CompletedAt:  completedAt,
	})
	_ = api.storage.CompleteAPIIdempotencyByJob(record.KeyID, record.JobID, string(respJSON), http.StatusOK)
	return nil
}

func (api *WorkflowAPI) reconcileOpenAIBackgroundResponseFailed(record *storage.OpenAIObjectRecord, req openAIResponsesRequest, execResult *jobs.WorkflowExecutionResult) error {
	message := "workflow execution failed"
	code := "execution_failed"
	if execResult != nil && execResult.Error != "" {
		message = execResult.Error
		code = execResult.ErrorCode
	}
	status, providerCode := openAIStatusFromExecutionResult(execResult)
	if providerCode != "" {
		code = providerCode
	}
	message = openAIPublicExecutionErrorMessage(execResult, message, status)
	payload := openAIResponseObject(record.ID, req.Model, record.CreatedAt.Unix(), storage.OpenAIObjectStatusFailed, "", nil, nil)
	payload["store"] = true
	payload["background"] = true
	payload["metadata"] = openAIMetadataObject(req.Metadata)
	if req.PreviousResponseID != "" {
		payload["previous_response_id"] = strings.TrimSpace(req.PreviousResponseID)
	}
	payload["error"] = map[string]interface{}{
		"code":    code,
		"message": message,
	}
	respJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	completedAt := time.Now().UTC()
	if err := api.storage.UpdateOpenAIObjectCompletion(record.ID, record.KeyID, storage.OpenAIObjectCompletion{
		JobID:        record.JobID,
		Status:       storage.OpenAIObjectStatusFailed,
		ResponseJSON: string(respJSON),
		ErrorCode:    code,
		ErrorMessage: message,
		CompletedAt:  completedAt,
	}); err != nil {
		return err
	}
	_ = api.storage.UpdateAPIUsageCompletionByJob(record.KeyID, openAIUsageEndpointResponses, record.JobID, storage.APIUsageCompletion{
		JobID:        record.JobID,
		Status:       storage.APIUsageStatusFailed,
		HTTPStatus:   status,
		LatencyMs:    float64(completedAt.Sub(record.CreatedAt).Milliseconds()),
		ErrorCode:    code,
		ErrorMessage: message,
		CompletedAt:  completedAt,
	})
	_ = api.storage.CompleteAPIIdempotencyByJob(record.KeyID, record.JobID, string(respJSON), http.StatusOK)
	return nil
}

func openAIStoredObjectMap(record *storage.OpenAIObjectRecord) map[string]interface{} {
	payload := map[string]interface{}{}
	if record != nil && strings.TrimSpace(record.ResponseJSON) != "" {
		_ = json.Unmarshal([]byte(record.ResponseJSON), &payload)
	}
	if record == nil {
		return payload
	}
	if payload == nil {
		payload = map[string]interface{}{}
	}
	if payload["id"] == nil {
		payload["id"] = record.ID
	}
	if payload["object"] == nil {
		payload["object"] = record.ObjectType
	}
	if payload["status"] == nil {
		payload["status"] = record.Status
	}
	if payload["metadata"] == nil {
		var metadata map[string]interface{}
		if err := json.Unmarshal([]byte(defaultJSONText(record.MetadataJSON)), &metadata); err == nil && metadata != nil {
			payload["metadata"] = metadata
		}
	}
	return payload
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func openAIResponseOutputsFromJSON(responseJSON string) []map[string]interface{} {
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(responseJSON), &payload); err != nil {
		return nil
	}
	output, _ := payload["output"].([]interface{})
	if len(output) == 0 {
		return nil
	}
	items := make([]map[string]interface{}, 0, len(output))
	for _, raw := range output {
		item, _ := raw.(map[string]interface{})
		if item != nil {
			items = append(items, item)
		}
	}
	return items
}

func openAIObjectItemPayload(item storage.OpenAIObjectItem) interface{} {
	var payload interface{}
	if strings.TrimSpace(item.RawJSON) != "" && item.RawJSON != "{}" {
		if err := json.Unmarshal([]byte(item.RawJSON), &payload); err == nil && payload != nil {
			return payload
		}
	}
	return map[string]interface{}{
		"id":      item.OpenAIItemID,
		"type":    "message",
		"role":    item.Role,
		"content": []map[string]interface{}{{"type": "input_text", "text": openAIItemContentText(item)}},
	}
}

func openAIItemContentText(item storage.OpenAIObjectItem) string {
	var content map[string]interface{}
	if err := json.Unmarshal([]byte(defaultJSONText(item.ContentJSON)), &content); err != nil {
		return ""
	}
	if text, _ := content["text"].(string); text != "" {
		return text
	}
	return ""
}

func openAIResponseTypedTranscriptLine(item storage.OpenAIObjectItem) string {
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(defaultJSONText(item.RawJSON)), &payload); err != nil {
		return ""
	}
	switch typ, _ := payload["type"].(string); typ {
	case "function_call":
		callID := strings.TrimSpace(openAIStringPayloadValue(payload, "call_id"))
		name := strings.TrimSpace(openAIStringPayloadValue(payload, "name"))
		arguments := openAIStringPayloadValue(payload, "arguments")
		if callID == "" || name == "" {
			return ""
		}
		return fmt.Sprintf("ASSISTANT_FUNCTION_CALL call_id=%s name=%s arguments=%s", callID, name, arguments)
	case "function_call_output", "tool_result":
		callID := strings.TrimSpace(openAIStringPayloadValue(payload, "call_id"))
		output := openAIStringPayloadValue(payload, "output")
		if callID == "" || output == "" {
			return ""
		}
		return fmt.Sprintf("FUNCTION_CALL_OUTPUT call_id=%s output=%s", callID, output)
	default:
		return ""
	}
}

func openAIStringPayloadValue(payload map[string]interface{}, key string) string {
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	b, err := json.Marshal(value)
	if err != nil || string(b) == "null" {
		return ""
	}
	return string(b)
}

func openAITextContent(payload map[string]interface{}) string {
	if text, _ := payload["output_text"].(string); text != "" {
		return text
	}
	content, _ := payload["content"].([]interface{})
	for _, part := range content {
		obj, _ := part.(map[string]interface{})
		if text, _ := obj["text"].(string); text != "" {
			return text
		}
	}
	return ""
}

func openAIListPageRequestFromHTTP(r *http.Request) storage.OpenAIListPageRequest {
	query := r.URL.Query()
	limit, _ := strconv.Atoi(query.Get("limit"))
	return storage.OpenAIListPageRequest{
		Limit: limit,
		After: query.Get("after"),
		Order: query.Get("order"),
	}
}

func openAIListEnvelope(data []interface{}, page storage.OpenAIListPage) map[string]interface{} {
	return map[string]interface{}{
		"object":   "list",
		"data":     data,
		"has_more": page.HasMore,
		"first_id": nullableString(page.FirstID),
		"last_id":  nullableString(page.LastID),
	}
}

func openAIStoreEnabled(value *bool, defaultValue bool) bool {
	if value == nil {
		return defaultValue
	}
	return *value
}

func openAIMetadataObject(metadata map[string]interface{}) map[string]interface{} {
	if metadata == nil {
		return map[string]interface{}{}
	}
	return metadata
}

func defaultJSONText(value string) string {
	if strings.TrimSpace(value) == "" {
		return "{}"
	}
	return value
}

func nullableString(value string) interface{} {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func (api *WorkflowAPI) streamOpenAIChatCompletion(w http.ResponseWriter, r *http.Request, auth *openAIAuthContext, req openAIChatCompletionRequest, body []byte, started time.Time) {
	if strings.TrimSpace(req.Model) == "" {
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointChat, req.Model, "", "", true, 0, http.StatusBadRequest, "missing_model", "model is required", started)
		api.writeOpenAIError(w, http.StatusBadRequest, "model is required", "invalid_request_error", "missing_model")
		return
	}
	if len(req.Messages) == 0 {
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointChat, req.Model, "", "", true, 0, http.StatusBadRequest, "missing_messages", "messages is required", started)
		api.writeOpenAIError(w, http.StatusBadRequest, "messages is required", "invalid_request_error", "missing_messages")
		return
	}
	normalized, err := normalizeOpenAIChatMessages(req.Messages)
	if err != nil {
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointChat, req.Model, "", "", true, 0, http.StatusBadRequest, "invalid_message_content", err.Error(), started)
		api.writeOpenAIError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "invalid_message_content")
		return
	}
	route, err := api.resolveOpenAIRoute(req.Model, auth.key)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointChat, req.Model, "", "", true, normalized.InputTokens, http.StatusNotFound, "model_not_found", "model route not found", started)
			api.writeOpenAIError(w, http.StatusNotFound, "model route not found", "invalid_request_error", "model_not_found")
			return
		}
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointChat, req.Model, "", "", true, normalized.InputTokens, http.StatusInternalServerError, "internal_error", "failed to resolve model route", started)
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to resolve model route", "server_error", "internal_error")
		return
	}
	wf, err := api.buildOpenAIWorkflow(route, normalized, req)
	if err != nil {
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointChat, req.Model, route.ResolvedModel, route.WorkflowID, true, normalized.InputTokens, http.StatusBadRequest, "invalid_workflow", err.Error(), started)
		api.writeOpenAIError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "invalid_workflow")
		return
	}
	wf, err = api.compileOpenAIWorkflowForSubmit(r.Context(), wf)
	if err != nil {
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointChat, req.Model, route.ResolvedModel, route.WorkflowID, true, normalized.InputTokens, http.StatusBadRequest, "invalid_workflow", err.Error(), started)
		api.writeOpenAIError(w, http.StatusBadRequest, err.Error(), "invalid_request_error", "invalid_workflow")
		return
	}
	callerIdempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	idem, handled, apiIdempotencyKey := api.reserveOrReplayOpenAIIdempotency(w, r, auth.key.ID, openAIUsageEndpointChat, callerIdempotencyKey, body, false, func(status int, code, message string) {
		api.recordOpenAIAuthenticatedFailure(auth, openAIUsageEndpointChat, req.Model, route.ResolvedModel, route.WorkflowID, true, normalized.InputTokens, status, code, message, started)
	})
	if handled {
		return
	}
	workflowIdempotencyKey := openAIWorkflowIdempotencyKey(auth.key.ID, openAIUsageEndpointChat, callerIdempotencyKey)

	usageID := "apiusage-" + uuid.NewString()
	if err := api.storage.CreateAPIUsage(&storage.APIUsageRecord{
		ID:             usageID,
		RequestID:      "req-" + uuid.NewString(),
		KeyID:          auth.key.ID,
		UserID:         auth.key.UserID,
		Endpoint:       openAIUsageEndpointChat,
		RequestedModel: req.Model,
		ResolvedModel:  route.ResolvedModel,
		WorkflowID:     route.WorkflowID,
		Status:         storage.APIUsageStatusRunning,
		HTTPStatus:     http.StatusOK,
		Stream:         true,
		TokensInput:    normalized.InputTokens,
		CreatedAt:      started,
	}); err != nil {
		api.completeOpenAIIdempotencyError(idem, auth.key.ID, apiIdempotencyKey, http.StatusInternalServerError, "failed to create usage record", "server_error", "internal_error")
		api.writeOpenAIError(w, http.StatusInternalServerError, "failed to create usage record", "server_error", "internal_error")
		return
	}
	if retryAfter := api.checkOpenAITokenRateLimit(auth.key, estimateOpenAIRateLimitTokens(wf, normalized, req)); retryAfter > 0 {
		api.completeOpenAIUsageFailure(usageID, http.StatusTooManyRequests, "rate_limit_exceeded", "rate limit exceeded", started)
		api.completeOpenAIIdempotencyError(idem, auth.key.ID, apiIdempotencyKey, http.StatusTooManyRequests, "rate limit exceeded", "rate_limit_error", "rate_limit_exceeded")
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())))
		api.writeOpenAIError(w, http.StatusTooManyRequests, "rate limit exceeded", "rate_limit_error", "rate_limit_exceeded")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		api.completeOpenAIUsageFailure(usageID, http.StatusInternalServerError, "streaming_unsupported", "streaming unsupported", started)
		api.writeOpenAIError(w, http.StatusInternalServerError, "streaming unsupported", "server_error", "streaming_unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	streamID := "chatcmpl-" + uuid.NewString()
	created := time.Now().Unix()
	writeSSEData(w, map[string]interface{}{
		"id":                 streamID,
		"object":             "chat.completion.chunk",
		"created":            created,
		"model":              req.Model,
		"system_fingerprint": "fp_consortium",
		"choices": []map[string]interface{}{
			{"index": 0, "delta": map[string]string{"role": "assistant"}, "finish_reason": nil},
		},
	})
	flusher.Flush()

	type streamResult struct {
		result *jobs.WorkflowExecutionResult
		err    error
	}
	resultCh := make(chan streamResult, 1)
	go func() {
		res, err := api.submitAndWaitOpenAIWorkflow(r.Context(), wf, auth.key, workflowIdempotencyKey, func(jobID string) {
			api.attachOpenAIIdempotencyJob(idem, auth.key.ID, apiIdempotencyKey, jobID)
		})
		resultCh <- streamResult{result: res, err: err}
	}()

	ticker := time.NewTicker(openAIHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			api.completeOpenAIUsageFailure(usageID, http.StatusRequestTimeout, "request_cancelled", r.Context().Err().Error(), started)
			api.completeOpenAIIdempotencyError(idem, auth.key.ID, apiIdempotencyKey, http.StatusRequestTimeout, "request cancelled", "server_error", "request_cancelled")
			return
		case <-ticker.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		case out := <-resultCh:
			if out.err != nil || out.result == nil || out.result.Result == nil || !out.result.Success {
				message := "workflow execution failed"
				code := "execution_failed"
				if out.err != nil {
					var status int
					status, code = openAIStatusFromSubmitError(out.err)
					message = openAIPublicSubmitErrorMessage(out.err, status)
					api.completeOpenAIUsageFailure(usageID, status, code, message, started)
					api.completeOpenAIIdempotencyError(idem, auth.key.ID, apiIdempotencyKey, status, message, openAIErrorTypeForStatus(status), code)
					writeSSEData(w, openAIErrorResponse{Error: openAIError{Message: message, Type: openAIErrorTypeForStatus(status), Param: nil, Code: code}})
					flusher.Flush()
					return
				} else if out.result != nil && out.result.Error != "" {
					message = out.result.Error
					code = out.result.ErrorCode
				}
				status, providerCode := openAIStatusFromExecutionResult(out.result)
				if providerCode != "" {
					code = providerCode
				}
				message = openAIPublicExecutionErrorMessage(out.result, message, status)
				api.completeOpenAIUsageFailure(usageID, status, code, message, started)
				api.completeOpenAIIdempotencyError(idem, auth.key.ID, apiIdempotencyKey, status, message, openAIErrorTypeForStatus(status), code)
				writeSSEData(w, openAIErrorResponse{Error: openAIError{Message: message, Type: openAIErrorTypeForStatus(status), Param: nil, Code: code}})
				flusher.Flush()
				return
			}
			content := out.result.Result.FinalOutput
			var toolCalls []providers.ToolCall
			if route.Mode == storage.APIModelRouteModeDirectModel {
				toolCalls = extractOpenAIToolCalls(out.result.Result)
			}
			delta := map[string]interface{}{"content": content}
			finishReason := "stop"
			if len(toolCalls) > 0 {
				delta = map[string]interface{}{"tool_calls": toolCalls}
				finishReason = "tool_calls"
			}
			writeSSEData(w, map[string]interface{}{
				"id":                 streamID,
				"object":             "chat.completion.chunk",
				"created":            created,
				"model":              req.Model,
				"system_fingerprint": "fp_consortium",
				"choices": []map[string]interface{}{
					{"index": 0, "delta": delta, "finish_reason": nil},
				},
			})
			writeSSEData(w, map[string]interface{}{
				"id":                 streamID,
				"object":             "chat.completion.chunk",
				"created":            created,
				"model":              req.Model,
				"system_fingerprint": "fp_consortium",
				"choices": []map[string]interface{}{
					{"index": 0, "delta": map[string]interface{}{}, "finish_reason": finishReason},
				},
			})
			if req.StreamOptions.IncludeUsage {
				writeSSEData(w, map[string]interface{}{
					"id":                 streamID,
					"object":             "chat.completion.chunk",
					"created":            created,
					"model":              req.Model,
					"system_fingerprint": "fp_consortium",
					"choices":            []interface{}{},
					"usage":              chatUsageFromWorkflowResult(out.result.Result, normalized.InputTokens, content),
				})
			}
			usage := chatUsageFromWorkflowResult(out.result.Result, normalized.InputTokens, content)
			completedAt := time.Now().UTC()
			_ = api.storage.UpdateAPIUsageCompletion(usageID, storage.APIUsageCompletion{
				JobID:        out.result.JobID,
				Status:       storage.APIUsageStatusSucceeded,
				HTTPStatus:   http.StatusOK,
				TokensInput:  usage.PromptTokens,
				TokensOutput: usage.CompletionTokens,
				TokensTotal:  usage.TotalTokens,
				Cost:         out.result.Result.TotalCost,
				LatencyMs:    float64(completedAt.Sub(started).Milliseconds()),
				CompletedAt:  completedAt,
			})
			api.completeOpenAIIdempotencySuccess(idem, auth.key.ID, apiIdempotencyKey, "", http.StatusOK, true)
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			flusher.Flush()
			return
		}
	}
}

func (api *WorkflowAPI) streamOpenAIResponse(
	w http.ResponseWriter,
	r *http.Request,
	auth *openAIAuthContext,
	req openAIResponsesRequest,
	prompt openAINormalizedPrompt,
	wf *workflow.Workflow,
	routeMode string,
	usageID string,
	started time.Time,
	idem *storage.APIIdempotencyRecord,
	apiIdempotencyKey string,
	workflowIdempotencyKey string,
) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		api.completeOpenAIUsageFailure(usageID, http.StatusInternalServerError, "streaming_unsupported", "streaming unsupported", started)
		api.writeOpenAIError(w, http.StatusInternalServerError, "streaming unsupported", "server_error", "streaming_unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	responseID := "resp-" + uuid.NewString()
	itemID := "msg-" + uuid.NewString()
	createdAt := time.Now().Unix()
	sequence := 0
	emit := func(event string, payload map[string]interface{}) {
		sequence++
		payload["type"] = event
		payload["sequence_number"] = sequence
		writeSSEEvent(w, event, payload)
		flusher.Flush()
	}
	emit("response.created", map[string]interface{}{
		"response": openAIResponseObject(responseID, req.Model, createdAt, "in_progress", "", nil, nil),
	})
	emit("response.in_progress", map[string]interface{}{
		"response": openAIResponseObject(responseID, req.Model, createdAt, "in_progress", "", nil, nil),
	})

	type streamResult struct {
		result *jobs.WorkflowExecutionResult
		err    error
	}
	resultCh := make(chan streamResult, 1)
	go func() {
		res, err := api.submitAndWaitOpenAIWorkflow(r.Context(), wf, auth.key, workflowIdempotencyKey, func(jobID string) {
			api.attachOpenAIIdempotencyJob(idem, auth.key.ID, apiIdempotencyKey, jobID)
		})
		resultCh <- streamResult{result: res, err: err}
	}()

	ticker := time.NewTicker(openAIHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			api.completeOpenAIUsageFailure(usageID, http.StatusRequestTimeout, "request_cancelled", r.Context().Err().Error(), started)
			api.completeOpenAIIdempotencyError(idem, auth.key.ID, apiIdempotencyKey, http.StatusRequestTimeout, "request cancelled", "server_error", "request_cancelled")
			return
		case <-ticker.C:
			_, _ = w.Write([]byte(": ping\n\n"))
			flusher.Flush()
		case out := <-resultCh:
			if out.err != nil || out.result == nil || out.result.Result == nil || !out.result.Success {
				message := "workflow execution failed"
				code := "execution_failed"
				if out.err != nil {
					var status int
					status, code = openAIStatusFromSubmitError(out.err)
					message = openAIPublicSubmitErrorMessage(out.err, status)
					api.completeOpenAIUsageFailure(usageID, status, code, message, started)
					api.completeOpenAIIdempotencyError(idem, auth.key.ID, apiIdempotencyKey, status, message, openAIErrorTypeForStatus(status), code)
					emit("response.failed", map[string]interface{}{
						"response": map[string]interface{}{
							"id":         responseID,
							"object":     openAIObjectResponse,
							"created_at": createdAt,
							"status":     "failed",
							"model":      req.Model,
							"error": map[string]interface{}{
								"code":    code,
								"message": message,
							},
						},
					})
					return
				} else if out.result != nil && out.result.Error != "" {
					message = out.result.Error
					code = out.result.ErrorCode
				}
				status, providerCode := openAIStatusFromExecutionResult(out.result)
				if providerCode != "" {
					code = providerCode
				}
				message = openAIPublicExecutionErrorMessage(out.result, message, status)
				api.completeOpenAIUsageFailure(usageID, status, code, message, started)
				api.completeOpenAIIdempotencyError(idem, auth.key.ID, apiIdempotencyKey, status, message, openAIErrorTypeForStatus(status), code)
				emit("response.failed", map[string]interface{}{
					"response": map[string]interface{}{
						"id":         responseID,
						"object":     openAIObjectResponse,
						"created_at": createdAt,
						"status":     "failed",
						"model":      req.Model,
						"error": map[string]interface{}{
							"code":    code,
							"message": message,
						},
					},
				})
				return
			}

			content := out.result.Result.FinalOutput
			usage := chatUsageFromWorkflowResult(out.result.Result, prompt.InputTokens, content)
			usageMap := openAIResponsesUsageMap(usage)
			outputText, outputItems := openAIResponseOutputItemsFromWorkflowResult(out.result.Result, routeMode, storage.OpenAIObjectStatusCompleted)
			if openAIResponseOutputItemsAreFunctionCalls(outputItems) {
				for i, item := range outputItems {
					itemID, _ := item["id"].(string)
					arguments, _ := item["arguments"].(string)
					emit("response.output_item.added", map[string]interface{}{
						"response_id":  responseID,
						"output_index": i,
						"item":         openAIResponseItemWithStatus(item, "in_progress"),
					})
					emit("response.function_call_arguments.delta", map[string]interface{}{
						"response_id":  responseID,
						"item_id":      itemID,
						"output_index": i,
						"delta":        arguments,
					})
					emit("response.function_call_arguments.done", map[string]interface{}{
						"response_id":  responseID,
						"item_id":      itemID,
						"output_index": i,
						"arguments":    arguments,
						"item":         item,
					})
					emit("response.output_item.done", map[string]interface{}{
						"response_id":  responseID,
						"output_index": i,
						"item":         item,
					})
				}

				completedAt := time.Now().UTC()
				_ = api.storage.UpdateAPIUsageCompletion(usageID, storage.APIUsageCompletion{
					JobID:        out.result.JobID,
					Status:       storage.APIUsageStatusSucceeded,
					HTTPStatus:   http.StatusOK,
					TokensInput:  usage.PromptTokens,
					TokensOutput: usage.CompletionTokens,
					TokensTotal:  usage.TotalTokens,
					Cost:         out.result.Result.TotalCost,
					LatencyMs:    float64(completedAt.Sub(started).Milliseconds()),
					CompletedAt:  completedAt,
				})
				api.completeOpenAIIdempotencySuccess(idem, auth.key.ID, apiIdempotencyKey, "", http.StatusOK, true)
				emit("response.completed", map[string]interface{}{
					"response": openAIResponseObject(responseID, req.Model, createdAt, "completed", outputText, outputItems, usageMap),
				})
				return
			}

			outputItem := openAIResponseOutputItem(itemID, "in_progress", content)
			emit("response.output_item.added", map[string]interface{}{
				"output_index": 0,
				"item":         outputItem,
			})
			emit("response.content_part.added", map[string]interface{}{
				"item_id":       itemID,
				"output_index":  0,
				"content_index": 0,
				"part":          map[string]interface{}{"type": "output_text", "text": ""},
			})
			emit("response.output_text.delta", map[string]interface{}{
				"item_id":       itemID,
				"output_index":  0,
				"content_index": 0,
				"delta":         content,
			})
			emit("response.output_text.done", map[string]interface{}{
				"item_id":       itemID,
				"output_index":  0,
				"content_index": 0,
				"text":          content,
			})
			emit("response.content_part.done", map[string]interface{}{
				"item_id":       itemID,
				"output_index":  0,
				"content_index": 0,
				"part":          map[string]interface{}{"type": "output_text", "text": content},
			})
			emit("response.output_item.done", map[string]interface{}{
				"output_index": 0,
				"item":         openAIResponseOutputItem(itemID, "completed", content),
			})

			completedAt := time.Now().UTC()
			_ = api.storage.UpdateAPIUsageCompletion(usageID, storage.APIUsageCompletion{
				JobID:        out.result.JobID,
				Status:       storage.APIUsageStatusSucceeded,
				HTTPStatus:   http.StatusOK,
				TokensInput:  usage.PromptTokens,
				TokensOutput: usage.CompletionTokens,
				TokensTotal:  usage.TotalTokens,
				Cost:         out.result.Result.TotalCost,
				LatencyMs:    float64(completedAt.Sub(started).Milliseconds()),
				CompletedAt:  completedAt,
			})
			api.completeOpenAIIdempotencySuccess(idem, auth.key.ID, apiIdempotencyKey, "", http.StatusOK, true)
			emit("response.completed", map[string]interface{}{
				"response": openAIResponseObject(responseID, req.Model, createdAt, "completed", content, []map[string]interface{}{
					openAIResponseOutputItem(itemID, "completed", content),
				}, usageMap),
			})
			return
		}
	}
}

func (api *WorkflowAPI) authenticateOpenAI(w http.ResponseWriter, r *http.Request) (*openAIAuthContext, bool) {
	if api.rejectOpenAIPreAuthRateLimited(w, r) {
		return nil, false
	}
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" || !strings.HasPrefix(strings.ToLower(header), "bearer ") {
		w.Header().Set("WWW-Authenticate", `Bearer realm="consortium"`)
		api.writeOpenAIError(w, http.StatusUnauthorized, "missing bearer token", "invalid_request_error", "invalid_api_key")
		return nil, false
	}
	token := strings.TrimSpace(header[len("Bearer "):])
	if len([]byte(token)) > openAIMaxBearerTokenBytes {
		api.writeOpenAIError(w, http.StatusUnauthorized, "invalid API key", "invalid_request_error", "invalid_api_key")
		return nil, false
	}
	sum := sha256.Sum256([]byte(token))
	want := "sha256:" + hex.EncodeToString(sum[:])
	prefix := openAIKeyPrefix(token)
	key, err := api.storage.GetAPIKeyByPrefix(prefix)
	if err != nil || key == nil || key.RevokedAt != nil || subtle.ConstantTimeCompare([]byte(key.KeyHash), []byte(want)) != 1 {
		key, err = api.storage.GetAPIKeyByHash(want)
	}
	if err != nil || key == nil || key.RevokedAt != nil || subtle.ConstantTimeCompare([]byte(key.KeyHash), []byte(want)) != 1 {
		api.writeOpenAIError(w, http.StatusUnauthorized, "invalid API key", "invalid_request_error", "invalid_api_key")
		return nil, false
	}
	_ = api.storage.TouchAPIKeyLastUsed(key.ID, time.Now().UTC())
	return &openAIAuthContext{key: key}, true
}

func (api *WorkflowAPI) rejectOpenAIPreAuthRateLimited(w http.ResponseWriter, r *http.Request) bool {
	if api == nil || api.rateLimiter == nil || api.preAuthRequestLimit <= 0 {
		return false
	}
	key := "preauth:" + openAIClientIdentity(r)
	if retryAfter := api.rateLimiter.reserve(key, api.preAuthRequestLimit, 0, 1, 0); retryAfter > 0 {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())))
		api.writeOpenAIError(w, http.StatusTooManyRequests, "rate limit exceeded", "rate_limit_error", "rate_limit_exceeded")
		return true
	}
	return false
}

func openAIClientIdentity(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	// TODO(v0.1-security): Only trust forwarded IP headers when the server has
	// an explicit trusted-proxy configuration. Direct internet exposure lets
	// clients spoof these values for pre-auth rate limiting.
	if forwardedFor := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwardedFor != "" {
		if first, _, ok := strings.Cut(forwardedFor, ","); ok {
			return strings.TrimSpace(first)
		}
		return forwardedFor
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	remote := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(remote); err == nil && strings.TrimSpace(host) != "" {
		return strings.TrimSpace(host)
	}
	if remote != "" {
		return remote
	}
	return "unknown"
}

func (api *WorkflowAPI) rejectOpenAIReadRateLimited(w http.ResponseWriter, auth *openAIAuthContext, endpoint string) bool {
	started := time.Now().UTC()
	if retryAfter := api.checkOpenAIRequestRateLimit(auth.key); retryAfter > 0 {
		api.recordOpenAIAuthenticatedFailure(auth, endpoint, "", "", "", false, 0, http.StatusTooManyRequests, "rate_limit_exceeded", "rate limit exceeded", started)
		w.Header().Set("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())))
		api.writeOpenAIError(w, http.StatusTooManyRequests, "rate limit exceeded", "rate_limit_error", "rate_limit_exceeded")
		return true
	}
	return false
}

func openAIKeyPrefix(token string) string {
	token = strings.TrimSpace(token)
	if idx := strings.Index(token, "."); idx > 0 {
		return token[:idx]
	}
	if idx := strings.Index(token, "_"); idx > 0 {
		return token[:idx]
	}
	return token
}

func (api *WorkflowAPI) reserveOrReplayOpenAIIdempotency(
	w http.ResponseWriter,
	r *http.Request,
	keyID string,
	endpoint string,
	idempotencyKey string,
	body []byte,
	replayBody bool,
	recordFailure func(status int, code, message string),
) (*storage.APIIdempotencyRecord, bool, string) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return nil, false, ""
	}
	if len([]byte(idempotencyKey)) > openAIMaxIdempotencyKeyBytes {
		message := "idempotency key is too long"
		if recordFailure != nil {
			recordFailure(http.StatusBadRequest, "invalid_idempotency_key", message)
		}
		api.writeOpenAIError(w, http.StatusBadRequest, message, "invalid_request_error", "invalid_idempotency_key")
		return nil, true, ""
	}
	storageIdempotencyKey := openAIAPIIdempotencyStorageKey(endpoint, idempotencyKey)
	created, existing, err := api.reserveOpenAIIdempotency(keyID, storageIdempotencyKey, body)
	if errors.Is(err, storage.ErrConflict) {
		message := "idempotency key reused with a different request"
		if recordFailure != nil {
			recordFailure(http.StatusConflict, "idempotency_conflict", message)
		}
		api.writeOpenAIError(w, http.StatusConflict, message, "invalid_request_error", "idempotency_conflict")
		return nil, true, storageIdempotencyKey
	}
	if err != nil {
		message := "failed to reserve idempotency key"
		if recordFailure != nil {
			recordFailure(http.StatusInternalServerError, "internal_error", message)
		}
		api.writeOpenAIError(w, http.StatusInternalServerError, message, "server_error", "internal_error")
		return nil, true, storageIdempotencyKey
	}
	if created || !replayBody {
		return existing, false, storageIdempotencyKey
	}
	record := existing
	if record.ResponseBody == "" {
		var waitErr error
		record, waitErr = api.waitForOpenAIIdempotencyCompletion(r.Context(), keyID, storageIdempotencyKey)
		if waitErr != nil {
			if errors.Is(waitErr, context.Canceled) {
				message := "request cancelled while waiting for idempotent response"
				if recordFailure != nil {
					recordFailure(http.StatusRequestTimeout, "request_cancelled", message)
				}
				api.writeOpenAIError(w, http.StatusRequestTimeout, message, "server_error", "request_cancelled")
				return nil, true, storageIdempotencyKey
			}
			if errors.Is(waitErr, context.DeadlineExceeded) {
				message := "idempotent request is still in progress"
				if recordFailure != nil {
					recordFailure(http.StatusConflict, "idempotency_in_progress", message)
				}
				api.writeOpenAIError(w, http.StatusConflict, message, "invalid_request_error", "idempotency_in_progress")
				return nil, true, storageIdempotencyKey
			}
			if errors.Is(waitErr, storage.ErrNotFound) {
				message := "idempotent request did not complete; retry the request"
				if recordFailure != nil {
					recordFailure(http.StatusConflict, "idempotency_retry", message)
				}
				api.writeOpenAIError(w, http.StatusConflict, message, "invalid_request_error", "idempotency_retry")
				return nil, true, storageIdempotencyKey
			}
			message := "failed to load idempotent response"
			if recordFailure != nil {
				recordFailure(http.StatusInternalServerError, "internal_error", message)
			}
			api.writeOpenAIError(w, http.StatusInternalServerError, message, "server_error", "internal_error")
			return nil, true, storageIdempotencyKey
		}
	}
	api.replayOpenAIIdempotentResponse(w, record)
	return record, true, storageIdempotencyKey
}

func openAIAPIIdempotencyStorageKey(endpoint, idempotencyKey string) string {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(endpoint) + "\x00" + idempotencyKey))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (api *WorkflowAPI) reserveOpenAIIdempotency(keyID, idempotencyKey string, body []byte) (bool, *storage.APIIdempotencyRecord, error) {
	sum := sha256.Sum256(body)
	now := time.Now().UTC()
	return api.storage.ReserveAPIIdempotency(&storage.APIIdempotencyRecord{
		ID:                 "idem-" + uuid.NewString(),
		KeyID:              keyID,
		IdempotencyKey:     idempotencyKey,
		RequestFingerprint: hex.EncodeToString(sum[:]),
		CreatedAt:          now,
		ExpiresAt:          now.Add(24 * time.Hour),
	})
}

func (api *WorkflowAPI) waitForOpenAIIdempotencyCompletion(ctx context.Context, keyID, idempotencyKey string) (*storage.APIIdempotencyRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, openAIIdempotencyWaitTimeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		record, err := api.storage.GetAPIIdempotency(keyID, idempotencyKey)
		if err != nil {
			return nil, err
		}
		if record.ResponseBody != "" {
			return record, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (api *WorkflowAPI) replayOpenAIIdempotentResponse(w http.ResponseWriter, record *storage.APIIdempotencyRecord) {
	status := http.StatusOK
	if record != nil && record.HTTPStatus > 0 {
		status = openAIHTTPStatus(record.HTTPStatus)
	}
	body := ""
	if record != nil {
		body = record.ResponseBody
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func (api *WorkflowAPI) attachOpenAIIdempotencyJob(idem *storage.APIIdempotencyRecord, keyID, idempotencyKey, jobID string) {
	if strings.TrimSpace(jobID) == "" {
		return
	}
	if idem != nil && strings.TrimSpace(idem.ID) != "" {
		_ = api.storage.AttachAPIIdempotencyJobByID(idem.ID, keyID, jobID)
		return
	}
	if strings.TrimSpace(idempotencyKey) == "" {
		return
	}
	_ = api.storage.AttachAPIIdempotencyJob(keyID, idempotencyKey, jobID)
}

func idempotencyRecordID(idem *storage.APIIdempotencyRecord) string {
	if idem == nil {
		return ""
	}
	return strings.TrimSpace(idem.ID)
}

func (api *WorkflowAPI) completeOpenAIIdempotencySuccess(idem *storage.APIIdempotencyRecord, keyID, idempotencyKey, responseBody string, status int, retainResponseBody bool) {
	if idem == nil && strings.TrimSpace(idempotencyKey) == "" {
		return
	}
	if !retainResponseBody {
		if idem != nil && strings.TrimSpace(idem.ID) != "" {
			_ = api.storage.DeleteAPIIdempotencyByID(idem.ID, keyID)
			return
		}
		if strings.TrimSpace(idempotencyKey) != "" {
			_ = api.storage.DeleteAPIIdempotency(keyID, idempotencyKey)
		}
		return
	}
	if idem != nil && strings.TrimSpace(idem.ID) != "" {
		_ = api.storage.CompleteAPIIdempotencyByID(idem.ID, keyID, responseBody, status)
		return
	}
	_ = api.storage.CompleteAPIIdempotency(keyID, idempotencyKey, responseBody, status)
}

func openAIRetainSuccessfulIdempotencyBody(store *bool) bool {
	return store == nil || *store
}

func (api *WorkflowAPI) completeOpenAIIdempotencyError(idem *storage.APIIdempotencyRecord, keyID, idempotencyKey string, status int, message, typ, code string) {
	if idem == nil && strings.TrimSpace(idempotencyKey) == "" {
		return
	}
	status = openAIHTTPStatus(status)
	if openAIIdempotencyErrorShouldClear(status) {
		if idem != nil && strings.TrimSpace(idem.ID) != "" {
			_ = api.storage.DeleteAPIIdempotencyByID(idem.ID, keyID)
			return
		}
		if strings.TrimSpace(idempotencyKey) != "" {
			_ = api.storage.DeleteAPIIdempotency(keyID, idempotencyKey)
		}
		return
	}
	if idem != nil && strings.TrimSpace(idem.ID) != "" {
		_ = api.storage.CompleteAPIIdempotencyByID(idem.ID, keyID, string(openAIErrorResponseJSON(message, typ, code)), status)
		return
	}
	_ = api.storage.CompleteAPIIdempotency(keyID, idempotencyKey, string(openAIErrorResponseJSON(message, typ, code)), status)
}

func (api *WorkflowAPI) completeOpenAIIdempotencyTerminalError(idem *storage.APIIdempotencyRecord, keyID, idempotencyKey string, status int, message, typ, code string) {
	if idem == nil && strings.TrimSpace(idempotencyKey) == "" {
		return
	}
	status = openAIHTTPStatus(status)
	if idem != nil && strings.TrimSpace(idem.ID) != "" {
		_ = api.storage.CompleteAPIIdempotencyByID(idem.ID, keyID, string(openAIErrorResponseJSON(message, typ, code)), status)
		return
	}
	_ = api.storage.CompleteAPIIdempotency(keyID, idempotencyKey, string(openAIErrorResponseJSON(message, typ, code)), status)
}

func openAIIdempotencyErrorShouldClear(status int) bool {
	status = openAIHTTPStatus(status)
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

func openAIWorkflowIdempotencyKey(keyID, endpoint, idempotencyKey string) string {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(idempotencyKey))
	return "openai:" + strings.TrimSpace(keyID) + ":" + endpoint + ":" + hex.EncodeToString(sum[:16])
}

func (api *WorkflowAPI) resolveOpenAIRoute(model string, key *storage.APIKey) (openAIRouteResolution, error) {
	model = strings.TrimSpace(model)
	if key != nil && strings.TrimSpace(key.WorkflowID) != "" {
		if _, err := api.storage.GetWorkflow(key.WorkflowID); err != nil {
			return openAIRouteResolution{}, err
		}
		return openAIRouteResolution{
			RequestedModel: model,
			ResolvedModel:  model,
			WorkflowID:     key.WorkflowID,
			Mode:           storage.APIModelRouteModeWorkflow,
		}, nil
	}
	route, err := api.storage.GetAPIModelRoute(model)
	if err == nil && !route.Enabled {
		return openAIRouteResolution{}, storage.ErrNotFound
	} else if errors.Is(err, storage.ErrNotFound) {
		route, err = api.storage.GetDefaultAPIModelRoute()
	}
	if err != nil {
		return openAIRouteResolution{}, err
	}
	return openAIRouteResolution{
		RequestedModel: model,
		ResolvedModel:  route.APIModel,
		WorkflowID:     route.WorkflowID,
		ProviderModel:  route.ProviderModel,
		Mode:           route.Mode,
	}, nil
}

func (api *WorkflowAPI) buildOpenAIWorkflow(route openAIRouteResolution, prompt openAINormalizedPrompt, req openAIChatCompletionRequest) (*workflow.Workflow, error) {
	switch route.Mode {
	case storage.APIModelRouteModeDirectModel:
		return buildOpenAIDirectModelWorkflow(route, prompt, req), nil
	case storage.APIModelRouteModeWorkflow:
		def, err := api.storage.GetWorkflow(route.WorkflowID)
		if err != nil {
			return nil, err
		}
		wf, err := jobs.WorkflowFromDefinition(def.Definition, map[string]interface{}{
			"system_prompt": prompt.SystemPrompt,
			"user_prompt":   prompt.UserPrompt,
		})
		if err != nil {
			return nil, err
		}
		if err := applyOpenAIWorkflowControls(wf, req); err != nil {
			return nil, err
		}
		return wf, nil
	default:
		return nil, fmt.Errorf("unsupported route mode %q", route.Mode)
	}
}

func (api *WorkflowAPI) compileOpenAIWorkflowForSubmit(ctx context.Context, wf *workflow.Workflow) (*workflow.Workflow, error) {
	compiled, _, err := compiler.Compile(ctx, wf, openAIWorkflowResolver{store: api.storage})
	if err != nil {
		return nil, err
	}
	return compiled, nil
}

type openAIWorkflowResolver struct {
	store *storage.Storage
}

func (r openAIWorkflowResolver) ResolveWorkflow(_ context.Context, id string) (*workflow.Workflow, string, error) {
	def, err := r.store.GetWorkflow(id)
	if err != nil {
		return nil, "", err
	}
	wf, err := jobs.WorkflowFromDefinition(def.Definition, map[string]interface{}{})
	if err != nil {
		return nil, "", err
	}
	if wf.ID == "" {
		wf.ID = id
	}
	snapshot, err := workflowruntime.FreezeWorkflowDefinition(wf)
	if err != nil {
		return nil, "", err
	}
	return wf, snapshot.DAGHash, nil
}

func buildOpenAIDirectModelWorkflow(route openAIRouteResolution, prompt openAINormalizedPrompt, req openAIChatCompletionRequest) *workflow.Workflow {
	temp := 0.7
	if req.Temperature != nil {
		temp = *req.Temperature
	}
	maxTokens := req.MaxCompletionTokens
	if maxTokens <= 0 {
		maxTokens = req.MaxTokens
	}
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	metadata := openAIControlMetadata(req)
	if openAIResponseFormatIsJSONSchema(req.ResponseFormat) {
		ensureOpenAIRequireProviderParameters(metadata)
	}
	return &workflow.Workflow{
		ID:   "api-direct-" + uuid.NewString(),
		Name: "OpenAI API Direct Model",
		Nodes: []*workflow.Node{
			{
				ID:             "completion",
				Type:           workflow.NodeTypePrompt,
				Model:          route.ProviderModel,
				SystemPrompt:   prompt.SystemPrompt,
				Prompt:         "{{user_prompt}}",
				Temperature:    &temp,
				MaxTokens:      maxTokens,
				TimeoutSeconds: 120,
				RetryPolicy: &workflow.RetryPolicy{
					MaxAttempts:     1,
					BackoffMs:       0,
					BackoffMultiply: 1,
					MaxBackoffMs:    0,
				},
				Metadata: metadata,
			},
		},
		Context: map[string]interface{}{
			"user_prompt": prompt.UserPrompt,
		},
	}
}

func openAIControlMetadata(req openAIChatCompletionRequest) map[string]interface{} {
	metadata := map[string]interface{}{}
	if req.TopP != nil {
		metadata["top_p"] = *req.TopP
	}
	if stop := normalizeStopStrings(req.Stop); len(stop) > 0 {
		metadata["stop"] = stop
	}
	if req.Seed != nil {
		metadata["seed"] = *req.Seed
	}
	if len(req.Tools) > 0 {
		metadata["tools"] = req.Tools
	}
	if req.ToolChoice != nil {
		metadata["tool_choice"] = req.ToolChoice
	}
	if req.ParallelToolCalls != nil {
		metadata["parallel_tool_calls"] = *req.ParallelToolCalls
	}
	if req.ResponseFormat != nil {
		metadata["response_format"] = req.ResponseFormat
	}
	if effort := strings.TrimSpace(req.ReasoningEffort); effort != "" {
		metadata["openrouter_reasoning"] = map[string]interface{}{"effort": effort}
	}
	if sessionID := strings.TrimSpace(req.SessionID); sessionID != "" {
		metadata["session_id"] = sessionID
	}
	if promptCacheKey := strings.TrimSpace(req.PromptCacheKey); promptCacheKey != "" {
		metadata["prompt_cache_key"] = promptCacheKey
	}
	if promptCacheRetention := strings.TrimSpace(req.PromptCacheRetention); promptCacheRetention != "" {
		metadata["prompt_cache_retention"] = promptCacheRetention
	}
	if req.Provider != nil || len(req.Order) > 0 || req.AllowFallbacks != nil || req.RequireParameters != nil {
		var routing *providers.ProviderRouting
		if req.Provider != nil {
			copied := *req.Provider
			routing = &copied
		} else {
			routing = &providers.ProviderRouting{}
		}
		if len(req.Order) > 0 {
			routing.Order = req.Order
		}
		if req.AllowFallbacks != nil {
			routing.AllowFallbacks = req.AllowFallbacks
		}
		if req.RequireParameters != nil {
			routing.RequireParameters = req.RequireParameters
		}
		metadata["openrouter_provider"] = routing
	}
	return metadata
}

func openAIResponseFormatIsJSONSchema(format *providers.ResponseFormat) bool {
	return format != nil && strings.EqualFold(strings.TrimSpace(format.Type), "json_schema")
}

func ensureOpenAIRequireProviderParameters(metadata map[string]interface{}) {
	if metadata == nil {
		return
	}
	requireParameters := true
	raw := metadata["openrouter_provider"]
	switch routing := raw.(type) {
	case nil:
		metadata["openrouter_provider"] = &providers.ProviderRouting{RequireParameters: &requireParameters}
	case *providers.ProviderRouting:
		if routing.RequireParameters == nil {
			copied := *routing
			copied.RequireParameters = &requireParameters
			metadata["openrouter_provider"] = &copied
		}
	case providers.ProviderRouting:
		if routing.RequireParameters == nil {
			routing.RequireParameters = &requireParameters
			metadata["openrouter_provider"] = routing
		}
	case map[string]interface{}:
		if _, ok := routing["require_parameters"]; !ok {
			copied := make(map[string]interface{}, len(routing)+1)
			for key, value := range routing {
				copied[key] = value
			}
			copied["require_parameters"] = true
			metadata["openrouter_provider"] = copied
		}
	default:
		metadata["openrouter_provider"] = &providers.ProviderRouting{RequireParameters: &requireParameters}
	}
}

func applyOpenAIWorkflowControls(wf *workflow.Workflow, req openAIChatCompletionRequest) error {
	if wf == nil {
		return fmt.Errorf("workflow is required")
	}
	metadata := openAIControlMetadata(req)
	maxTokens := openAIRequestedMaxTokens(req)
	if len(metadata) == 0 && req.Temperature == nil && maxTokens <= 0 {
		return nil
	}
	terminalPrompts := openAITerminalPromptNodes(wf)
	if len(terminalPrompts) == 0 {
		if openAIRequiresTerminalPrompt(req) {
			return fmt.Errorf("workflow route cannot enforce requested tools or response_format without a terminal prompt node")
		}
		return nil
	}
	for _, node := range terminalPrompts {
		if node.Metadata == nil {
			node.Metadata = make(map[string]interface{}, len(metadata))
		}
		for key, value := range metadata {
			node.Metadata[key] = value
		}
		if openAIResponseFormatIsJSONSchema(req.ResponseFormat) {
			ensureOpenAIRequireProviderParameters(node.Metadata)
		}
		if req.Temperature != nil {
			node.Temperature = req.Temperature
		}
		if maxTokens > 0 {
			node.MaxTokens = maxTokens
		}
	}
	return nil
}

func openAITerminalPromptNodes(wf *workflow.Workflow) []*workflow.Node {
	nodeByID := make(map[string]*workflow.Node, len(wf.Nodes))
	for _, node := range wf.Nodes {
		if node != nil {
			nodeByID[node.ID] = node
		}
	}
	outgoingToNonResult := map[string]bool{}
	for _, edge := range wf.Edges {
		if edge == nil {
			continue
		}
		target := nodeByID[edge.Target]
		if target == nil || target.Type != workflow.NodeTypeResult {
			outgoingToNonResult[edge.Source] = true
		}
	}
	var terminal []*workflow.Node
	for _, node := range wf.Nodes {
		if node != nil && node.Type == workflow.NodeTypePrompt && !outgoingToNonResult[node.ID] {
			terminal = append(terminal, node)
		}
	}
	return terminal
}

func openAIRequiresTerminalPrompt(req openAIChatCompletionRequest) bool {
	if req.ResponseFormat != nil {
		formatType := strings.TrimSpace(req.ResponseFormat.Type)
		if formatType != "" && formatType != "text" {
			return true
		}
	}
	return openAIToolChoiceForced(req.ToolChoice)
}

func openAIToolChoiceForced(choice interface{}) bool {
	switch typed := choice.(type) {
	case nil:
		return false
	case string:
		switch strings.TrimSpace(typed) {
		case "", "auto", "none":
			return false
		default:
			return true
		}
	default:
		return true
	}
}

func validateOpenAIChatRequest(req openAIChatCompletionRequest) *openAIValidationError {
	if validation := validateOpenAICommonGenerationControls(req.N, req.Logprobs, req.TopLogprobs, req.Prediction, req.Tools); validation != nil {
		return validation
	}
	if validation := validateOpenAINumericGenerationControls(req.Temperature, req.TopP, map[string]int{
		"max_tokens":            req.MaxTokens,
		"max_completion_tokens": req.MaxCompletionTokens,
	}); validation != nil {
		return validation
	}
	if validation := validateOpenAIChatMessageRoles(req.Messages); validation != nil {
		return validation
	}
	if validation := validateOpenAIPromptCacheRetention(req.PromptCacheRetention); validation != nil {
		return validation
	}
	if validation := validateOpenAIReasoningEffort(req.ReasoningEffort); validation != nil {
		return validation
	}
	if validation := validateOpenAIProviderRouting(req.Provider, req.Order, req.AllowFallbacks, req.RequireParameters); validation != nil {
		return validation
	}
	if validation := validateOpenAIMetadata(req.Metadata); validation != nil {
		return validation
	}
	if validation := validateOpenAIChatModalities(req.Modalities); validation != nil {
		return validation
	}
	if rawJSONPresent(req.Audio) {
		return &openAIValidationError{
			Message: "audio output is not supported by this OpenAI-compatible endpoint",
			Code:    "unsupported_audio",
			Param:   "audio",
		}
	}
	if rawJSONPresent(req.Functions) {
		return &openAIValidationError{
			Message: "deprecated functions are not supported; use tools instead",
			Code:    "unsupported_functions",
			Param:   "functions",
		}
	}
	if rawJSONPresent(req.FunctionCall) {
		return &openAIValidationError{
			Message: "deprecated function_call is not supported; use tool_choice instead",
			Code:    "unsupported_function_call",
			Param:   "function_call",
		}
	}
	return nil
}

func validateOpenAIResponsesRequest(req openAIResponsesRequest, tools []providers.ToolDefinition) *openAIValidationError {
	if validation := validateOpenAICommonGenerationControls(req.N, req.Logprobs, req.TopLogprobs, req.Prediction, tools); validation != nil {
		return validation
	}
	if validation := validateOpenAINumericGenerationControls(req.Temperature, req.TopP, map[string]int{
		"max_output_tokens": req.MaxOutputTokens,
	}); validation != nil {
		return validation
	}
	if validation := validateOpenAIMetadata(req.Metadata); validation != nil {
		return validation
	}
	if validation := validateOpenAIProviderRouting(req.Provider, req.Order, req.AllowFallbacks, req.RequireParameters); validation != nil {
		return validation
	}
	if req.Background && req.Stream {
		return &openAIValidationError{
			Message: "background streaming responses are not supported because stream resume cursors are not implemented",
			Code:    "unsupported_background_stream",
			Param:   "stream",
		}
	}
	if req.Background && req.Store != nil && !*req.Store {
		return &openAIValidationError{
			Message: "background responses require store to be true",
			Code:    "unsupported_background_store_false",
			Param:   "store",
		}
	}
	if validation := validateOpenAIPromptCacheRetention(req.PromptCacheRetention); validation != nil {
		return validation
	}
	if validation := validateOpenAIResponsesInclude(req.Include); validation != nil {
		return validation
	}
	if validation := validateOpenAIResponsesTruncation(req.Truncation); validation != nil {
		return validation
	}
	if rawJSONPresent(req.ContextManagement) {
		return &openAIValidationError{
			Message: "context_management is not supported by this OpenAI-compatible endpoint",
			Code:    "unsupported_context_management",
			Param:   "context_management",
		}
	}
	if req.MaxToolCalls != nil {
		return &openAIValidationError{
			Message: "max_tool_calls is not supported by this OpenAI-compatible endpoint",
			Code:    "unsupported_max_tool_calls",
			Param:   "max_tool_calls",
		}
	}
	if rawJSONPresent(req.Conversation) {
		return &openAIValidationError{
			Message: "conversation is not supported by this OpenAI-compatible endpoint",
			Code:    "unsupported_conversation",
			Param:   "conversation",
		}
	}
	if rawJSONPresent(req.Moderation) {
		return &openAIValidationError{
			Message: "inline moderation is not supported by this OpenAI-compatible endpoint",
			Code:    "unsupported_moderation",
			Param:   "moderation",
		}
	}
	if validation := validateOpenAIReasoningEffort(req.Reasoning.Effort); validation != nil {
		return validation
	}
	return nil
}

func validateOpenAIProviderRouting(provider *providers.ProviderRouting, order []string, allowFallbacks, requireParameters *bool) *openAIValidationError {
	switch {
	case provider != nil:
		return &openAIValidationError{
			Message: "caller-supplied provider routing is not supported on public OpenAI-compatible endpoints",
			Code:    "unsupported_provider_routing",
			Param:   "provider",
		}
	case len(order) > 0:
		return &openAIValidationError{
			Message: "caller-supplied provider routing is not supported on public OpenAI-compatible endpoints",
			Code:    "unsupported_provider_routing",
			Param:   "order",
		}
	case allowFallbacks != nil:
		return &openAIValidationError{
			Message: "caller-supplied provider routing is not supported on public OpenAI-compatible endpoints",
			Code:    "unsupported_provider_routing",
			Param:   "allow_fallbacks",
		}
	case requireParameters != nil:
		return &openAIValidationError{
			Message: "caller-supplied provider routing is not supported on public OpenAI-compatible endpoints",
			Code:    "unsupported_provider_routing",
			Param:   "require_parameters",
		}
	default:
		return nil
	}
}

func normalizeOpenAIResponsesTools(tools []openAIResponsesTool) ([]providers.ToolDefinition, *openAIValidationError) {
	if len(tools) == 0 {
		return nil, nil
	}
	normalized := make([]providers.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		toolType := strings.TrimSpace(tool.Type)
		if toolType == "" {
			return nil, &openAIValidationError{
				Message: "tool type is required",
				Code:    "invalid_tool",
				Param:   "tools",
			}
		}
		if toolType != "function" {
			return nil, &openAIValidationError{
				Message: fmt.Sprintf("tool type %q is not supported by this OpenAI-compatible endpoint", toolType),
				Code:    "unsupported_tool_type",
				Param:   "tools",
			}
		}

		function := providers.ToolFunctionDefinition{
			Name:        strings.TrimSpace(tool.Name),
			Description: tool.Description,
			Parameters:  tool.Parameters,
		}
		if tool.Strict != nil {
			function.Strict = *tool.Strict
		}
		if tool.Function != nil {
			function = *tool.Function
			function.Name = strings.TrimSpace(function.Name)
		}
		if function.Name == "" {
			return nil, &openAIValidationError{
				Message: "function tool name is required",
				Code:    "invalid_function_tool",
				Param:   "tools",
			}
		}
		if function.Parameters == nil {
			function.Parameters = map[string]interface{}{}
		}
		normalized = append(normalized, providers.ToolDefinition{
			Type:     "function",
			Function: function,
		})
	}
	return normalized, nil
}

func validateOpenAIMetadata(metadata map[string]interface{}) *openAIValidationError {
	if len(metadata) == 0 {
		return nil
	}
	if len(metadata) > openAIMaxMetadataPairs {
		return &openAIValidationError{
			Message: fmt.Sprintf("metadata must contain at most %d key-value pairs", openAIMaxMetadataPairs),
			Code:    "invalid_metadata",
			Param:   "metadata",
		}
	}
	for key, value := range metadata {
		if strings.TrimSpace(key) == "" {
			return &openAIValidationError{
				Message: "metadata keys must be non-empty strings",
				Code:    "invalid_metadata",
				Param:   "metadata",
			}
		}
		if len([]rune(key)) > openAIMaxMetadataKeyChars {
			return &openAIValidationError{
				Message: fmt.Sprintf("metadata keys must be at most %d characters", openAIMaxMetadataKeyChars),
				Code:    "invalid_metadata",
				Param:   "metadata",
			}
		}
		text, ok := value.(string)
		if !ok {
			return &openAIValidationError{
				Message: "metadata values must be strings",
				Code:    "invalid_metadata",
				Param:   "metadata",
			}
		}
		if len([]rune(text)) > openAIMaxMetadataValueChars {
			return &openAIValidationError{
				Message: fmt.Sprintf("metadata values must be at most %d characters", openAIMaxMetadataValueChars),
				Code:    "invalid_metadata",
				Param:   "metadata",
			}
		}
	}
	return nil
}

func validateOpenAIChatModalities(modalities []string) *openAIValidationError {
	if len(modalities) == 0 {
		return nil
	}
	if len(modalities) == 1 && strings.TrimSpace(modalities[0]) == "text" {
		return nil
	}
	return &openAIValidationError{
		Message: "only text chat modalities are supported by this OpenAI-compatible endpoint",
		Code:    "unsupported_modalities",
		Param:   "modalities",
	}
}

func validateOpenAIChatMessageRoles(messages []openAIChatMessage) *openAIValidationError {
	for _, msg := range messages {
		switch strings.TrimSpace(msg.Role) {
		case "system", "developer", "user", "assistant", "tool", "function":
			continue
		default:
			return &openAIValidationError{
				Message: "message role must be system, developer, user, assistant, tool, or function",
				Code:    "invalid_message_role",
				Param:   "messages",
			}
		}
	}
	return nil
}

func validateOpenAIResponsesInclude(include []string) *openAIValidationError {
	if len(include) == 0 {
		return nil
	}
	return &openAIValidationError{
		Message: "include values requiring hosted tools, logprobs, multimodal data, or encrypted reasoning are not supported",
		Code:    "unsupported_include",
		Param:   "include",
	}
}

func validateOpenAIResponsesTruncation(value string) *openAIValidationError {
	switch strings.TrimSpace(value) {
	case "", "disabled":
		return nil
	default:
		return &openAIValidationError{
			Message: "truncation is not supported; omit it or use disabled",
			Code:    "unsupported_truncation",
			Param:   "truncation",
		}
	}
}

func validateOpenAINumericGenerationControls(temperature, topP *float64, tokenFields map[string]int) *openAIValidationError {
	if temperature != nil && (*temperature < 0 || *temperature > 2) {
		return &openAIValidationError{
			Message: "temperature must be between 0 and 2",
			Code:    "invalid_temperature",
			Param:   "temperature",
		}
	}
	if topP != nil && (*topP < 0 || *topP > 1) {
		return &openAIValidationError{
			Message: "top_p must be between 0 and 1",
			Code:    "invalid_top_p",
			Param:   "top_p",
		}
	}
	for field, value := range tokenFields {
		if value < 0 {
			return &openAIValidationError{
				Message: field + " must be non-negative",
				Code:    "invalid_" + field,
				Param:   field,
			}
		}
		if value > openAIMaxRequestedTokens {
			return &openAIValidationError{
				Message: fmt.Sprintf("%s must be less than or equal to %d", field, openAIMaxRequestedTokens),
				Code:    "invalid_" + field,
				Param:   field,
			}
		}
	}
	return nil
}

func validateOpenAICommonGenerationControls(n *int, logprobs *bool, topLogprobs *int, prediction json.RawMessage, tools []providers.ToolDefinition) *openAIValidationError {
	if n != nil {
		if *n < 1 {
			return &openAIValidationError{
				Message: "n must be at least 1",
				Code:    "invalid_n",
				Param:   "n",
			}
		}
		if *n > 1 {
			return &openAIValidationError{
				Message: "n greater than 1 is not supported by this OpenAI-compatible endpoint",
				Code:    "unsupported_n",
				Param:   "n",
			}
		}
	}
	if logprobs != nil && *logprobs {
		return &openAIValidationError{
			Message: "logprobs is not supported by this OpenAI-compatible endpoint",
			Code:    "unsupported_logprobs",
			Param:   "logprobs",
		}
	}
	if topLogprobs != nil {
		if *topLogprobs < 0 {
			return &openAIValidationError{
				Message: "top_logprobs must be non-negative",
				Code:    "invalid_top_logprobs",
				Param:   "top_logprobs",
			}
		}
		if *topLogprobs > 0 {
			return &openAIValidationError{
				Message: "top_logprobs is not supported by this OpenAI-compatible endpoint",
				Code:    "unsupported_top_logprobs",
				Param:   "top_logprobs",
			}
		}
	}
	if rawJSONPresent(prediction) {
		return &openAIValidationError{
			Message: "prediction is not supported by this OpenAI-compatible endpoint",
			Code:    "unsupported_prediction",
			Param:   "prediction",
		}
	}
	for _, tool := range tools {
		toolType := strings.TrimSpace(tool.Type)
		if toolType != "" && toolType != "function" {
			return &openAIValidationError{
				Message: fmt.Sprintf("tool type %q is not supported by this OpenAI-compatible endpoint", toolType),
				Code:    "unsupported_tool_type",
				Param:   "tools",
			}
		}
	}
	return nil
}

func validateOpenAIPromptCacheRetention(value string) *openAIValidationError {
	switch strings.TrimSpace(value) {
	case "", "in_memory", "in-memory", "24h":
		return nil
	default:
		return &openAIValidationError{
			Message: "prompt_cache_retention must be in_memory, in-memory, or 24h",
			Code:    "invalid_prompt_cache_retention",
			Param:   "prompt_cache_retention",
		}
	}
}

func validateOpenAIReasoningEffort(value string) *openAIValidationError {
	switch strings.TrimSpace(value) {
	case "", "none", "minimal", "low", "medium", "high", "xhigh":
		return nil
	default:
		return &openAIValidationError{
			Message: "reasoning effort must be none, minimal, low, medium, high, or xhigh",
			Code:    "invalid_reasoning_effort",
			Param:   "reasoning_effort",
		}
	}
}

func rawJSONPresent(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && string(trimmed) != "null"
}

func openAIResponseFormatForResponses(req openAIResponsesRequest) *providers.ResponseFormat {
	if req.ResponseFormat != nil {
		return req.ResponseFormat
	}
	if req.Text.Format != nil {
		return req.Text.Format
	}
	return nil
}

func openAIRequestedMaxTokens(req openAIChatCompletionRequest) int {
	if req.MaxCompletionTokens > 0 {
		return req.MaxCompletionTokens
	}
	if req.MaxTokens > 0 {
		return req.MaxTokens
	}
	return 0
}

func estimateOpenAIRateLimitTokens(wf *workflow.Workflow, prompt openAINormalizedPrompt, req openAIChatCompletionRequest) int {
	if wf == nil {
		return prompt.InputTokens + openAIRequestedMaxTokens(req)
	}
	promptNodeCount := 0
	outputReservation := 0
	for _, node := range wf.Nodes {
		if node == nil {
			continue
		}
		switch node.Type {
		case workflow.NodeTypePrompt, workflow.NodeTypeContractExtract:
			promptNodeCount++
			if node.MaxTokens > 0 {
				outputReservation += node.MaxTokens
			}
		}
	}
	if promptNodeCount == 0 {
		promptNodeCount = 1
	}
	if outputReservation <= 0 {
		outputReservation = openAIRequestedMaxTokens(req)
	}
	return prompt.InputTokens*promptNodeCount + outputReservation
}

func (api *WorkflowAPI) submitAndWaitOpenAIWorkflow(ctx context.Context, wf *workflow.Workflow, key *storage.APIKey, idempotencyKey string, onSubmitted func(jobID string)) (*jobs.WorkflowExecutionResult, error) {
	submitResp, err := api.jobManager.SubmitWorkflow(ctx, &jobs.SubmitWorkflowRequest{
		Workflow:                wf,
		IdempotencyKey:          idempotencyKey,
		DisableRequestHashDedup: true,
		UserID:                  "api-key:" + key.ID,
	})
	if err != nil {
		return nil, err
	}
	if onSubmitted != nil {
		onSubmitted(submitResp.JobID)
	}
	return api.jobManager.WaitForCompletion(ctx, submitResp.JobID, wf.ID)
}

func normalizeOpenAIChatMessages(messages []openAIChatMessage) (openAINormalizedPrompt, error) {
	var systemParts []string
	var transcript []string
	for _, msg := range messages {
		role := strings.TrimSpace(msg.Role)
		text, err := normalizeOpenAIContent(msg.Content)
		if err != nil {
			return openAINormalizedPrompt{}, err
		}
		switch role {
		case "system", "developer":
			if text != "" {
				systemParts = append(systemParts, text)
			}
		default:
			if text != "" {
				transcript = append(transcript, strings.ToUpper(role)+": "+text)
			}
		}
	}
	userPrompt := strings.Join(transcript, "\n")
	return openAINormalizedPrompt{
		SystemPrompt: strings.Join(systemParts, "\n\n"),
		UserPrompt:   userPrompt,
		InputTokens:  estimateOpenAITokens(strings.Join(append(systemParts, userPrompt), "\n")),
	}, nil
}

func normalizeOpenAIContent(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}
	var parts []map[string]interface{}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var out []string
		for _, part := range parts {
			partType, _ := part["type"].(string)
			switch partType {
			case "text", "input_text", "":
				if v, ok := part["text"].(string); ok {
					out = append(out, v)
				}
			default:
				return "", fmt.Errorf("unsupported content part type %q", partType)
			}
		}
		return strings.Join(out, "\n"), nil
	}
	return "", fmt.Errorf("message content must be text")
}

func normalizeOpenAIResponsesInput(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", fmt.Errorf("input is required")
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err == nil {
		out := make([]string, 0, len(items))
		for _, item := range items {
			part, err := normalizeOpenAIResponseInputItem(item)
			if err != nil {
				return "", err
			}
			if part != "" {
				out = append(out, part)
			}
		}
		return strings.Join(out, "\n"), nil
	}
	text, err := normalizeOpenAIResponseInputItem(raw)
	if err != nil {
		return "", err
	}
	return text, nil
}

func normalizeOpenAIResponseInputItem(raw json.RawMessage) (string, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return "", fmt.Errorf("input item must be text or an object")
	}
	itemType := jsonStringField(object, "type")
	switch itemType {
	case "", "message":
	case "input_text", "text", "output_text":
		if text := jsonStringField(object, "text"); text != "" {
			return text, nil
		}
	case "function_call_output", "tool_result":
		callID := strings.TrimSpace(jsonStringField(object, "call_id"))
		for _, key := range []string{"output", "content", "text"} {
			if text := jsonStringField(object, key); text != "" {
				if callID != "" {
					return fmt.Sprintf("FUNCTION_CALL_OUTPUT call_id=%s output=%s", callID, text), nil
				}
				return text, nil
			}
		}
	case "function_call":
		payload, ok := openAIResponseFunctionCallPayload(raw)
		if !ok {
			return "", fmt.Errorf("function_call input item requires call_id and name")
		}
		return fmt.Sprintf(
			"ASSISTANT_FUNCTION_CALL call_id=%s name=%s arguments=%s",
			openAIStringPayloadValue(payload, "call_id"),
			openAIStringPayloadValue(payload, "name"),
			openAIStringPayloadValue(payload, "arguments"),
		), nil
	default:
		return "", fmt.Errorf("unsupported input item type %q", itemType)
	}
	if content, ok := object["content"]; ok {
		return normalizeOpenAIResponseContent(content)
	}
	if text := jsonStringField(object, "text"); text != "" {
		return text, nil
	}
	return "", fmt.Errorf("input item must contain text content")
}

func (api *WorkflowAPI) validateOpenAIResponseFunctionCallOutputs(keyID, previousResponseID string, rawInput json.RawMessage) *openAIValidationError {
	callIDs, err := openAIResponseFunctionCallOutputIDs(rawInput)
	if err != nil {
		return &openAIValidationError{
			Message: err.Error(),
			Code:    "invalid_function_call_output",
			Param:   "input",
		}
	}
	if len(callIDs) == 0 {
		return nil
	}
	allowedCallIDs, err := api.loadPreviousOpenAIResponseFunctionCallIDs(keyID, previousResponseID)
	if err != nil {
		return &openAIValidationError{
			Message: "failed to validate function_call_output",
			Code:    "invalid_function_call_output",
			Param:   "input",
		}
	}
	for _, callID := range openAIResponseFunctionCallIDs(rawInput) {
		allowedCallIDs[callID] = struct{}{}
	}
	for _, callID := range callIDs {
		if _, ok := allowedCallIDs[callID]; !ok {
			return &openAIValidationError{
				Message: "function_call_output call_id does not match a function_call",
				Code:    "invalid_function_call_output",
				Param:   "input",
			}
		}
	}
	return nil
}

func openAIResponseFunctionCallIDs(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err == nil {
		var out []string
		for _, item := range items {
			out = append(out, openAIResponseFunctionCallIDs(item)...)
		}
		return out
	}
	payload, ok := openAIResponseFunctionCallPayload(raw)
	if !ok {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return nil
		}
		content, ok := object["content"]
		if !ok {
			return nil
		}
		return openAIResponseFunctionCallIDs(content)
	}
	callID, _ := payload["call_id"].(string)
	if strings.TrimSpace(callID) == "" {
		return nil
	}
	return []string{strings.TrimSpace(callID)}
}

func openAIResponseFunctionCallOutputIDs(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err == nil {
		var out []string
		for _, item := range items {
			ids, err := openAIResponseFunctionCallOutputIDs(item)
			if err != nil {
				return nil, err
			}
			out = append(out, ids...)
		}
		return out, nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, nil
	}
	itemType := strings.TrimSpace(jsonStringField(object, "type"))
	switch itemType {
	case "function_call_output", "tool_result":
		callID := strings.TrimSpace(jsonStringField(object, "call_id"))
		if callID == "" {
			return nil, fmt.Errorf("function_call_output requires call_id")
		}
		return []string{callID}, nil
	}
	content, ok := object["content"]
	if !ok {
		return nil, nil
	}
	return openAIResponseFunctionCallOutputIDs(content)
}

func (api *WorkflowAPI) loadPreviousOpenAIResponseFunctionCallIDs(keyID, responseID string) (map[string]struct{}, error) {
	records, err := api.loadPreviousOpenAIResponseChain(keyID, responseID)
	if err != nil {
		return nil, err
	}
	callIDs := make(map[string]struct{})
	for _, record := range records {
		for _, kind := range []string{storage.OpenAIItemKindInput, storage.OpenAIItemKindOutput} {
			items, _, err := api.storage.ListOpenAIObjectItems(record.ID, keyID, kind, storage.OpenAIListPageRequest{Limit: 100, Order: storage.OpenAIListOrderAsc})
			if err != nil && !errors.Is(err, storage.ErrNotFound) {
				return nil, err
			}
			for _, item := range items {
				var payload map[string]interface{}
				if err := json.Unmarshal([]byte(defaultJSONText(item.RawJSON)), &payload); err != nil {
					continue
				}
				if typ, _ := payload["type"].(string); typ != "function_call" {
					continue
				}
				if callID, _ := payload["call_id"].(string); strings.TrimSpace(callID) != "" {
					callIDs[strings.TrimSpace(callID)] = struct{}{}
				}
			}
		}
	}
	return callIDs, nil
}

func normalizeOpenAIResponseContent(raw json.RawMessage) (string, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(raw, &parts); err == nil {
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			text, err := normalizeOpenAIResponseContentPart(part)
			if err != nil {
				return "", err
			}
			if text != "" {
				out = append(out, text)
			}
		}
		return strings.Join(out, "\n"), nil
	}
	return normalizeOpenAIResponseContentPart(raw)
}

func normalizeOpenAIResponseContentPart(raw json.RawMessage) (string, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return "", fmt.Errorf("content part must be text or an object")
	}
	partType := jsonStringField(object, "type")
	switch partType {
	case "", "input_text", "text", "output_text":
		if text := jsonStringField(object, "text"); text != "" {
			return text, nil
		}
	case "function_call_output", "tool_result":
		for _, key := range []string{"output", "content", "text"} {
			if text := jsonStringField(object, key); text != "" {
				return text, nil
			}
		}
	default:
		return "", fmt.Errorf("unsupported input part type %q", partType)
	}
	return "", fmt.Errorf("content part must contain text")
}

func jsonStringField(object map[string]json.RawMessage, key string) string {
	raw, ok := object[key]
	if !ok {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}

func normalizeStopStrings(raw interface{}) []string {
	switch v := raw.(type) {
	case nil:
		return nil
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []string{v}
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func chatUsageFromWorkflowResult(result *workflow.WorkflowResult, estimatedInput int, output string) openAIChatUsage {
	input := result.TotalInputTokens
	outputTokens := result.TotalOutputTokens
	total := result.TotalTokens
	if input <= 0 {
		input = estimatedInput
	}
	if outputTokens <= 0 {
		outputTokens = estimateOpenAITokens(output)
	}
	if total <= 0 {
		total = input + outputTokens
	}
	return openAIChatUsage{
		PromptTokens:     input,
		CompletionTokens: outputTokens,
		TotalTokens:      total,
		PromptTokensDetails: openAIPromptTokensDetails{
			CachedTokens: openAIWorkflowMetadataIntSum(result, "openrouter_cached_tokens"),
		},
		CompletionTokensDetails: openAICompletionTokenDetails{
			ReasoningTokens: openAIWorkflowMetadataIntSum(result, "openrouter_reasoning_tokens"),
		},
	}
}

func openAIWorkflowMetadataIntSum(result *workflow.WorkflowResult, key string) int {
	if result == nil {
		return 0
	}
	total := 0
	for _, node := range result.NodeResults {
		if node == nil || len(node.Metadata) == 0 {
			continue
		}
		total += openAIMetadataInt(node.Metadata[key])
	}
	return total
}

func openAIMetadataInt(raw interface{}) int {
	switch v := raw.(type) {
	case nil:
		return 0
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		if parsed, err := v.Int64(); err == nil {
			return int(parsed)
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return parsed
		}
	}
	return 0
}

func openAIResponsesUsageMap(usage openAIChatUsage) map[string]interface{} {
	return map[string]interface{}{
		"input_tokens":  usage.PromptTokens,
		"output_tokens": usage.CompletionTokens,
		"total_tokens":  usage.TotalTokens,
		"input_tokens_details": map[string]int{
			"cached_tokens": usage.PromptTokensDetails.CachedTokens,
		},
		"output_tokens_details": map[string]int{
			"reasoning_tokens": usage.CompletionTokensDetails.ReasoningTokens,
		},
	}
}

func openAIResponseOutputItemsFromWorkflowResult(result *workflow.WorkflowResult, routeMode, status string) (string, []map[string]interface{}) {
	content := ""
	if result != nil {
		content = result.FinalOutput
	}
	if routeMode == storage.APIModelRouteModeDirectModel {
		if toolCalls := extractOpenAIToolCalls(result); len(toolCalls) > 0 {
			items := make([]map[string]interface{}, 0, len(toolCalls))
			for _, call := range toolCalls {
				items = append(items, openAIResponseFunctionCallItem("fc-"+uuid.NewString(), status, call))
			}
			return "", items
		}
	}
	return content, []map[string]interface{}{
		openAIResponseOutputItem("msg-"+uuid.NewString(), status, content),
	}
}

func openAIResponseOutputItemsAreFunctionCalls(items []map[string]interface{}) bool {
	if len(items) == 0 {
		return false
	}
	for _, item := range items {
		if typ, _ := item["type"].(string); typ != "function_call" {
			return false
		}
	}
	return true
}

func openAIResponseItemWithStatus(item map[string]interface{}, status string) map[string]interface{} {
	copied := make(map[string]interface{}, len(item)+1)
	for key, value := range item {
		copied[key] = value
	}
	copied["status"] = status
	return copied
}

func openAIResponseOutputItem(itemID, status, text string) map[string]interface{} {
	return map[string]interface{}{
		"id":     itemID,
		"type":   "message",
		"role":   "assistant",
		"status": status,
		"content": []map[string]interface{}{
			{"type": "output_text", "text": text},
		},
	}
}

func openAIResponseFunctionCallItem(itemID, status string, call providers.ToolCall) map[string]interface{} {
	callID := strings.TrimSpace(call.ID)
	if callID == "" {
		callID = "call-" + uuid.NewString()
	}
	return map[string]interface{}{
		"id":        itemID,
		"type":      "function_call",
		"call_id":   callID,
		"name":      call.Function.Name,
		"arguments": call.Function.Arguments,
		"status":    status,
	}
}

func openAIResponseObject(responseID, model string, createdAt int64, status, outputText string, output []map[string]interface{}, usage map[string]interface{}) map[string]interface{} {
	if output == nil {
		output = []map[string]interface{}{}
	}
	payload := map[string]interface{}{
		"id":          responseID,
		"object":      openAIObjectResponse,
		"created_at":  createdAt,
		"status":      status,
		"model":       model,
		"output_text": outputText,
		"output":      output,
	}
	if usage != nil {
		payload["usage"] = usage
	}
	return payload
}

func extractOpenAIToolCalls(result *workflow.WorkflowResult) []providers.ToolCall {
	if result == nil {
		return nil
	}
	for i := len(result.NodeResults) - 1; i >= 0; i-- {
		meta := result.NodeResults[i].Metadata
		if len(meta) == 0 {
			continue
		}
		raw, ok := meta["tool_calls"]
		if !ok {
			continue
		}
		serialized, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		var calls []providers.ToolCall
		if err := json.Unmarshal(serialized, &calls); err == nil {
			return calls
		}
	}
	return nil
}

func estimateOpenAITokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	tokens := len(text) / 4
	if tokens <= 0 {
		return 1
	}
	return tokens
}

func openAIStatusFromSubmitError(err error) (int, string) {
	switch {
	case jobs.IsAdmissionPausedError(err), jobs.IsAdmissionError(err):
		return http.StatusServiceUnavailable, "server_overloaded"
	case jobs.IsWorkflowSubmitValidationError(err):
		return http.StatusBadRequest, "invalid_request"
	case errors.Is(err, context.Canceled):
		return 499, "request_cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, "timeout"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}

func openAIPublicSubmitErrorMessage(err error, status int) string {
	switch {
	case jobs.IsAdmissionPausedError(err), jobs.IsAdmissionError(err):
		return "server overloaded"
	case jobs.IsWorkflowSubmitValidationError(err):
		return "invalid workflow request"
	case errors.Is(err, context.Canceled):
		return "request cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "request timed out"
	default:
		if openAIHTTPStatus(status) >= 500 {
			return "workflow submission failed"
		}
		return "request failed"
	}
}

func readOpenAIRequestBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	body := http.MaxBytesReader(w, r.Body, openAIRequestBodyLimit)
	data, err := io.ReadAll(body)
	if err != nil {
		if strings.Contains(err.Error(), "http: request body too large") {
			return nil, errOpenAIRequestBodyTooLarge
		}
		return nil, err
	}
	return data, nil
}

func (api *WorkflowAPI) recordOpenAIAuthenticatedFailure(
	auth *openAIAuthContext,
	endpoint string,
	requestedModel string,
	resolvedModel string,
	workflowID string,
	stream bool,
	tokensInput int,
	status int,
	code string,
	message string,
	started time.Time,
) {
	if auth == nil || auth.key == nil {
		return
	}
	completedAt := time.Now().UTC()
	if started.IsZero() {
		started = completedAt
	}
	record := &storage.APIUsageRecord{
		ID:             "apiusage-" + uuid.NewString(),
		RequestID:      "req-" + uuid.NewString(),
		KeyID:          auth.key.ID,
		UserID:         auth.key.UserID,
		Endpoint:       endpoint,
		RequestedModel: requestedModel,
		ResolvedModel:  resolvedModel,
		WorkflowID:     workflowID,
		Status:         storage.APIUsageStatusFailed,
		HTTPStatus:     openAIHTTPStatus(status),
		Stream:         stream,
		TokensInput:    tokensInput,
		LatencyMs:      float64(completedAt.Sub(started).Milliseconds()),
		ErrorCode:      code,
		ErrorMessage:   message,
		CreatedAt:      started,
		CompletedAt:    &completedAt,
	}
	_ = api.storage.CreateAPIUsage(record)
}

func openAIStatusFromExecutionResult(execResult *jobs.WorkflowExecutionResult) (int, string) {
	code := "execution_failed"
	if execResult != nil && strings.TrimSpace(execResult.ErrorCode) != "" {
		code = strings.TrimSpace(execResult.ErrorCode)
	}
	if execResult == nil || execResult.Result == nil {
		return http.StatusInternalServerError, code
	}
	for _, nodeResult := range execResult.Result.NodeResults {
		if nodeResult == nil || nodeResult.Success || len(nodeResult.Metadata) == 0 {
			continue
		}
		status := openAIProviderStatusFromMetadata(nodeResult.Metadata)
		if status < 400 || status > 599 {
			continue
		}
		providerCode := openAIStringMetadata(nodeResult.Metadata, "provider_error_code")
		if providerCode != "" {
			code = strings.ToLower(providerCode)
		}
		return status, code
	}
	return http.StatusInternalServerError, code
}

func openAIPublicExecutionErrorMessage(execResult *jobs.WorkflowExecutionResult, fallback string, status int) string {
	if !openAIExecutionHasProviderFailure(execResult) {
		return "workflow execution failed"
	}
	switch status {
	case http.StatusPaymentRequired:
		return "provider credits exhausted"
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return "upstream provider timeout"
	case http.StatusTooManyRequests:
		return "rate limit exceeded"
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return "request rejected by provider"
	case http.StatusUnauthorized, http.StatusForbidden:
		return "provider authentication failed"
	case http.StatusNotFound:
		return "model not found"
	default:
		if status >= 500 && status <= 599 {
			return "upstream provider unavailable"
		}
		return "workflow execution failed"
	}
}

func openAIExecutionHasProviderFailure(execResult *jobs.WorkflowExecutionResult) bool {
	if execResult == nil || execResult.Result == nil {
		return false
	}
	for _, nodeResult := range execResult.Result.NodeResults {
		if nodeResult == nil || nodeResult.Success || len(nodeResult.Metadata) == 0 {
			continue
		}
		if _, ok := nodeResult.Metadata["provider_error_code"]; ok {
			return true
		}
		if _, ok := nodeResult.Metadata["provider_status_code"]; ok {
			return true
		}
	}
	return false
}

func openAIProviderStatusFromMetadata(metadata map[string]interface{}) int {
	switch value := metadata["provider_status_code"].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		parsed, err := value.Int64()
		if err == nil {
			return int(parsed)
		}
	case string:
		return openAIProviderStatusFromString(value)
	}
	return 0
}

func openAIProviderStatusFromString(value string) int {
	for _, token := range strings.FieldsFunc(strings.TrimSpace(value), func(r rune) bool {
		return r < '0' || r > '9'
	}) {
		if token == "" {
			continue
		}
		parsed, err := strconv.Atoi(token)
		if err == nil && parsed >= 100 && parsed <= 599 {
			return parsed
		}
	}
	return 0
}

func openAIStringMetadata(metadata map[string]interface{}, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	switch value := metadata[key].(type) {
	case string:
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

func openAIErrorTypeForStatus(status int) string {
	status = openAIHTTPStatus(status)
	if status == http.StatusTooManyRequests {
		return "rate_limit_error"
	}
	if status >= 400 && status < 500 {
		return "invalid_request_error"
	}
	return "server_error"
}

func (api *WorkflowAPI) completeOpenAIUsageFailure(id string, status int, code, message string, started time.Time) {
	_ = api.storage.UpdateAPIUsageCompletion(id, storage.APIUsageCompletion{
		Status:       storage.APIUsageStatusFailed,
		HTTPStatus:   status,
		ErrorCode:    code,
		ErrorMessage: message,
		LatencyMs:    float64(time.Since(started).Milliseconds()),
		CompletedAt:  time.Now().UTC(),
	})
}

func (api *WorkflowAPI) writeOpenAIError(w http.ResponseWriter, status int, message, typ, code string) {
	api.writeOpenAIErrorWithParam(w, status, message, typ, code, nil)
}

func (api *WorkflowAPI) writeOpenAIValidationError(w http.ResponseWriter, validation *openAIValidationError) {
	if validation == nil {
		api.writeOpenAIError(w, http.StatusBadRequest, "invalid request", "invalid_request_error", "invalid_request")
		return
	}
	api.writeOpenAIErrorWithParam(w, http.StatusBadRequest, validation.Message, "invalid_request_error", validation.Code, nullableString(validation.Param))
}

func (api *WorkflowAPI) writeOpenAIErrorWithParam(w http.ResponseWriter, status int, message, typ, code string, param interface{}) {
	status = openAIHTTPStatus(status)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(openAIErrorResponseJSONWithParam(message, typ, code, param))
}

func openAIHTTPStatus(status int) int {
	if status == 499 {
		return http.StatusRequestTimeout
	}
	return status
}

func openAIErrorResponseJSON(message, typ, code string) []byte {
	return openAIErrorResponseJSONWithParam(message, typ, code, nil)
}

func openAIErrorResponseJSONWithParam(message, typ, code string, param interface{}) []byte {
	payload := openAIErrorResponse{
		Error: openAIError{
			Message: message,
			Type:    typ,
			Param:   param,
			Code:    code,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return []byte(`{"error":{"message":"failed to encode error response","type":"server_error","param":null,"code":"internal_error"}}` + "\n")
	}
	return append(data, '\n')
}

func writeSSEData(w http.ResponseWriter, payload interface{}) {
	serialized, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(serialized)
	_, _ = w.Write([]byte("\n\n"))
}

func writeSSEEvent(w http.ResponseWriter, event string, payload interface{}) {
	serialized, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = w.Write([]byte("event: "))
	_, _ = w.Write([]byte(event))
	_, _ = w.Write([]byte("\n"))
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(serialized)
	_, _ = w.Write([]byte("\n\n"))
}
