package admin

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type apiKeyCreateRequest struct {
	UserID            string `json:"user_id"`
	Name              string `json:"name"`
	WorkflowID        string `json:"workflow_id"`
	RequestsPerMinute int    `json:"requests_per_minute"`
	TokensPerMinute   int    `json:"tokens_per_minute"`
}

type apiModelRouteRequest struct {
	APIModel      string `json:"api_model"`
	Mode          string `json:"mode"`
	WorkflowID    string `json:"workflow_id"`
	ProviderModel string `json:"provider_model"`
	Description   string `json:"description"`
	IsDefault     bool   `json:"is_default"`
	Enabled       *bool  `json:"enabled"`
}

const adminAPIUsageMaxLimit = 10000

func (s *Server) handleAPIKeysCreate(w http.ResponseWriter, r *http.Request) {
	var req apiKeyCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid JSON request body", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeJSONError(w, "name is required", http.StatusBadRequest)
		return
	}

	token, prefix, err := generateAPIKeyToken()
	if err != nil {
		log.Printf("Error generating API key: %v", err)
		writeJSONError(w, "Failed to generate API key", http.StatusInternalServerError)
		return
	}
	sum := sha256.Sum256([]byte(token))
	key := storage.APIKey{
		ID:                "apikey-" + uuid.NewString(),
		UserID:            strings.TrimSpace(req.UserID),
		Name:              req.Name,
		Prefix:            prefix,
		KeyHash:           "sha256:" + hex.EncodeToString(sum[:]),
		WorkflowID:        strings.TrimSpace(req.WorkflowID),
		RequestsPerMinute: req.RequestsPerMinute,
		TokensPerMinute:   req.TokensPerMinute,
		CreatedAt:         time.Now().UTC(),
	}
	if err := s.storage.CreateAPIKey(&key); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, storage.ErrNotFound) {
			status = http.StatusBadRequest
		}
		writeJSONError(w, err.Error(), status)
		return
	}

	writeJSONResponse(w, map[string]interface{}{
		"key":     token,
		"api_key": key,
	})
}

func (s *Server) handleAPIKeysList(w http.ResponseWriter, r *http.Request) {
	includeRevoked := parseBoolQuery(r, "include_revoked", false)
	keys, err := s.storage.ListAPIKeys(r.URL.Query().Get("user_id"), includeRevoked)
	if err != nil {
		log.Printf("Error listing API keys: %v", err)
		writeJSONError(w, "Failed to list API keys", http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, map[string]interface{}{"api_keys": keys})
}

func (s *Server) handleAPIKeyRevoke(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(mux.Vars(r)["id"])
	if id == "" {
		writeJSONError(w, "id is required", http.StatusBadRequest)
		return
	}
	if err := s.storage.RevokeAPIKey(id, time.Now().UTC()); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeJSONError(w, "API key not found", http.StatusNotFound)
			return
		}
		log.Printf("Error revoking API key %s: %v", id, err)
		writeJSONError(w, "Failed to revoke API key", http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, map[string]string{"message": "API key revoked"})
}

func (s *Server) handleAPIUsageList(w http.ResponseWriter, r *http.Request) {
	filters := parseAPIUsageFilters(r)
	usage, err := s.storage.ListAPIUsage(filters)
	if err != nil {
		log.Printf("Error listing API usage: %v", err)
		writeJSONError(w, "Failed to list API usage", http.StatusInternalServerError)
		return
	}
	summary, err := s.storage.SummarizeAPIUsage(filters)
	if err != nil {
		log.Printf("Error summarizing API usage: %v", err)
		writeJSONError(w, "Failed to summarize API usage", http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, map[string]interface{}{
		"api_usage": usage,
		"usage":     usage,
		"summary":   summary,
	})
}

func (s *Server) handleAPIUsageExport(w http.ResponseWriter, r *http.Request) {
	filters := parseAPIUsageFilters(r)
	if filters.Limit <= 0 || filters.Limit > adminAPIUsageMaxLimit {
		filters.Limit = adminAPIUsageMaxLimit
	}
	usage, err := s.storage.ListAPIUsage(filters)
	if err != nil {
		log.Printf("Error exporting API usage: %v", err)
		writeJSONError(w, "Failed to export API usage", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="api-usage.csv"`)
	writer := csv.NewWriter(w)
	headers := []string{
		"id", "request_id", "key_id", "user_id", "endpoint", "requested_model",
		"resolved_model", "workflow_id", "job_id", "status", "http_status",
		"stream", "tokens_input", "tokens_output", "tokens_total", "cost",
		"latency_ms", "error_code", "error_message", "created_at", "completed_at",
	}
	if err := writer.Write(headers); err != nil {
		log.Printf("Error writing API usage CSV header: %v", err)
		return
	}
	for _, record := range usage {
		if err := writer.Write(apiUsageCSVRow(record)); err != nil {
			log.Printf("Error writing API usage CSV row: %v", err)
			return
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		log.Printf("Error flushing API usage CSV: %v", err)
	}
}

func (s *Server) handleAPIMetrics(w http.ResponseWriter, r *http.Request) {
	staleMinutes := 15
	if raw := r.URL.Query().Get("stale_minutes"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			staleMinutes = parsed
		}
	}
	if staleMinutes > 24*60 {
		staleMinutes = 24 * 60
	}
	now := time.Now().UTC()
	metrics, err := s.storage.GetOpenAIAPIMetrics(now.Add(-time.Duration(staleMinutes)*time.Minute), now)
	if err != nil {
		log.Printf("Error collecting API metrics: %v", err)
		writeJSONError(w, "Failed to collect API metrics", http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, map[string]interface{}{"openai_api_metrics": metrics})
}

func (s *Server) handleAPIModelRoutesList(w http.ResponseWriter, r *http.Request) {
	includeDisabled := parseBoolQuery(r, "include_disabled", true)
	routes, err := s.storage.ListAPIModelRoutes(includeDisabled)
	if err != nil {
		log.Printf("Error listing API model routes: %v", err)
		writeJSONError(w, "Failed to list API model routes", http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, map[string]interface{}{"model_routes": routes})
}

func (s *Server) handleAPIModelRouteUpsert(w http.ResponseWriter, r *http.Request) {
	var req apiModelRouteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid JSON request body", http.StatusBadRequest)
		return
	}
	if pathModel := strings.TrimSpace(mux.Vars(r)["model"]); pathModel != "" {
		req.APIModel = pathModel
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	route := storage.APIModelRoute{
		APIModel:      strings.TrimSpace(req.APIModel),
		Mode:          strings.TrimSpace(req.Mode),
		WorkflowID:    strings.TrimSpace(req.WorkflowID),
		ProviderModel: strings.TrimSpace(req.ProviderModel),
		Description:   strings.TrimSpace(req.Description),
		IsDefault:     req.IsDefault,
		Enabled:       enabled,
		UpdatedAt:     time.Now().UTC(),
	}
	if err := s.storage.UpsertAPIModelRoute(&route); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, storage.ErrNotFound) {
			status = http.StatusBadRequest
		}
		writeJSONError(w, err.Error(), status)
		return
	}
	created, err := s.storage.GetAPIModelRoute(route.APIModel)
	if err != nil {
		log.Printf("Error reloading API model route %s: %v", route.APIModel, err)
		writeJSONError(w, "Failed to load API model route", http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, map[string]interface{}{"model_route": created})
}

func (s *Server) handleAPIModelRouteDelete(w http.ResponseWriter, r *http.Request) {
	apiModel := strings.TrimSpace(mux.Vars(r)["model"])
	if apiModel == "" {
		writeJSONError(w, "api model is required", http.StatusBadRequest)
		return
	}
	if err := s.storage.DeleteAPIModelRoute(apiModel); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeJSONError(w, "API model route not found", http.StatusNotFound)
			return
		}
		log.Printf("Error deleting API model route %s: %v", apiModel, err)
		writeJSONError(w, "Failed to delete API model route", http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, map[string]string{"message": "API model route deleted"})
}

func parseAPIUsageFilters(r *http.Request) storage.APIUsageFilters {
	query := r.URL.Query()
	limit := 100
	if raw := query.Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > adminAPIUsageMaxLimit {
		limit = adminAPIUsageMaxLimit
	}
	return storage.APIUsageFilters{
		From:           parseNullableAdminTimestamp(query.Get("from")),
		To:             parseNullableAdminTimestamp(query.Get("to")),
		KeyID:          strings.TrimSpace(query.Get("key_id")),
		RequestedModel: strings.TrimSpace(query.Get("model")),
		Endpoint:       strings.TrimSpace(query.Get("endpoint")),
		Status:         strings.TrimSpace(query.Get("status")),
		Limit:          limit,
	}
}

func apiUsageCSVRow(record storage.APIUsageRecord) []string {
	completedAt := ""
	if record.CompletedAt != nil {
		completedAt = record.CompletedAt.Format(time.RFC3339Nano)
	}
	return []string{
		csvCell(record.ID),
		csvCell(record.RequestID),
		csvCell(record.KeyID),
		csvCell(record.UserID),
		csvCell(record.Endpoint),
		csvCell(record.RequestedModel),
		csvCell(record.ResolvedModel),
		csvCell(record.WorkflowID),
		csvCell(record.JobID),
		csvCell(record.Status),
		strconv.Itoa(record.HTTPStatus),
		strconv.FormatBool(record.Stream),
		strconv.Itoa(record.TokensInput),
		strconv.Itoa(record.TokensOutput),
		strconv.Itoa(record.TokensTotal),
		fmt.Sprintf("%.8f", record.Cost),
		fmt.Sprintf("%.2f", record.LatencyMs),
		csvCell(record.ErrorCode),
		csvCell(record.ErrorMessage),
		record.CreatedAt.Format(time.RFC3339Nano),
		completedAt,
	}
}

func csvCell(value string) string {
	if value == "" {
		return ""
	}
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed == "" {
		return value
	}
	switch trimmed[0] {
	case '=', '+', '-', '@':
		return "'" + value
	default:
		return value
	}
}

func generateAPIKeyToken() (token string, prefix string, err error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	if len(encoded) < 12 {
		return "", "", fmt.Errorf("generated token too short")
	}
	prefix = "sk-consortium-" + encoded[:12]
	token = prefix + "." + encoded[12:]
	return token, prefix, nil
}

func parseBoolQuery(r *http.Request, key string, defaultValue bool) bool {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return defaultValue
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return defaultValue
	}
	return parsed
}
