package admin

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alhasaniq/consortium/pkg/storage"
)

func TestAdminAPIKeysCreateListAndRevoke(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	createBody := `{"name":"CI key","requests_per_minute":7,"tokens_per_minute":8000}`
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/admin/api-keys", strings.NewReader(createBody)))
	if w.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", w.Code, w.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created["key"] == "" {
		t.Fatalf("create response missing one-time key: %#v", created)
	}
	createdKey := created["api_key"].(map[string]any)
	secret := created["key"].(string)
	prefix := createdKey["prefix"].(string)
	secretParts := strings.SplitN(secret, ".", 2)
	if len(secretParts) != 2 || secretParts[0] != prefix {
		t.Fatalf("generated key %q does not preserve stored prefix %q with dot separator", secret, prefix)
	}
	if createdKey["key_hash"] != nil {
		t.Fatalf("api_key leaked key_hash: %#v", createdKey)
	}

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/admin/api-keys", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", w.Code, w.Body.String())
	}
	var listed map[string][]map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed["api_keys"]) != 1 {
		t.Fatalf("listed = %#v, want one key", listed)
	}
	if listed["api_keys"][0]["key_hash"] != nil || listed["api_keys"][0]["key"] != nil {
		t.Fatalf("list leaked secret/hash: %#v", listed["api_keys"][0])
	}

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/admin/api-keys/"+createdKey["id"].(string), nil))
	if w.Code != http.StatusOK {
		t.Fatalf("revoke status = %d body=%s", w.Code, w.Body.String())
	}
	key, err := server.storage.GetAPIKeyByPrefix(prefix)
	if err != nil {
		t.Fatalf("GetAPIKeyByPrefix: %v", err)
	}
	if key.RevokedAt == nil {
		t.Fatalf("expected key to be soft revoked")
	}
}

func TestAdminAPIUsageExportEscapesCSVFormulaCells(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	if err := server.storage.CreateAPIUsage(&storage.APIUsageRecord{
		ID:             "usage-csv",
		RequestID:      "req-csv",
		KeyID:          "key-csv",
		UserID:         "system",
		Endpoint:       "/v1/chat/completions",
		RequestedModel: "=SUM(1,1)",
		ResolvedModel:  "+model",
		WorkflowID:     " =CMD()",
		Status:         storage.APIUsageStatusFailed,
		HTTPStatus:     400,
		ErrorCode:      "\t@bad",
		ErrorMessage:   "@bad",
		CreatedAt:      time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateAPIUsage: %v", err)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/admin/api-usage/export", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("export status = %d body=%s", w.Code, w.Body.String())
	}
	reader := csv.NewReader(strings.NewReader(w.Body.String()))
	records, err := reader.ReadAll()
	if err != nil && err != io.EOF {
		t.Fatalf("read csv: %v body=%s", err, w.Body.String())
	}
	if len(records) < 2 {
		t.Fatalf("csv records = %#v, want header and row", records)
	}
	row := strings.Join(records[1], "|")
	for _, want := range []string{"'=SUM(1,1)", "'+model", "'=CMD()", "'@bad"} {
		if !strings.Contains(row, want) {
			t.Fatalf("csv row %q missing escaped cell %q", row, want)
		}
	}
}

func TestParseAPIUsageFiltersCapsListLimit(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/admin/api-usage?limit=50000", nil)
	filters := parseAPIUsageFilters(req)
	if filters.Limit != 10000 {
		t.Fatalf("Limit = %d, want 10000", filters.Limit)
	}
}

func TestAdminAPIMetricsReturnsOpenAIUsageAndLifecycleSignals(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	now := time.Now().UTC()
	if err := server.storage.CreateAPIUsage(&storage.APIUsageRecord{
		ID:             "usage-metrics-ok",
		RequestID:      "req-metrics-ok",
		KeyID:          "key-metrics",
		UserID:         "system",
		Endpoint:       "/v1/chat/completions",
		RequestedModel: "gpt-test",
		Status:         storage.APIUsageStatusSucceeded,
		HTTPStatus:     http.StatusOK,
		TokensTotal:    12,
		LatencyMs:      25,
		CreatedAt:      now,
		CompletedAt:    &now,
	}); err != nil {
		t.Fatalf("CreateAPIUsage: %v", err)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/admin/api-metrics", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("metrics status = %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		OpenAIAPIMetrics struct {
			Usage              storage.APIUsageSummary `json:"usage"`
			RequestsByStatus   map[string]int          `json:"requests_by_status"`
			RequestsByEndpoint map[string]int          `json:"requests_by_endpoint"`
			HTTPStatusClasses  map[string]int          `json:"http_status_classes"`
			AvgLatencyMs       float64                 `json:"avg_latency_ms"`
		} `json:"openai_api_metrics"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode metrics: %v", err)
	}
	metrics := resp.OpenAIAPIMetrics
	if metrics.Usage.Requests != 1 || metrics.Usage.TokensTotal != 12 {
		t.Fatalf("usage metrics = %+v", metrics.Usage)
	}
	if metrics.RequestsByStatus[storage.APIUsageStatusSucceeded] != 1 {
		t.Fatalf("requests_by_status = %+v", metrics.RequestsByStatus)
	}
	if metrics.RequestsByEndpoint["/v1/chat/completions"] != 1 {
		t.Fatalf("requests_by_endpoint = %+v", metrics.RequestsByEndpoint)
	}
	if metrics.HTTPStatusClasses["2xx"] != 1 || metrics.AvgLatencyMs != 25 {
		t.Fatalf("status/latency metrics = classes=%+v avg=%v", metrics.HTTPStatusClasses, metrics.AvgLatencyMs)
	}
}
