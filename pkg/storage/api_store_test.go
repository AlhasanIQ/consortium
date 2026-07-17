package storage

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAPIKeysCreateListLookupTouchAndRevoke(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	defer store.Close()

	key := &APIKey{
		ID:                "key_test_1",
		UserID:            "system",
		Name:              "CI key",
		Prefix:            "sk-test-prefix",
		KeyHash:           "sha256:testhash",
		RequestsPerMinute: 12,
		TokensPerMinute:   3456,
	}
	if err := store.CreateAPIKey(key); err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}

	got, err := store.GetAPIKeyByPrefix("sk-test-prefix")
	if err != nil {
		t.Fatalf("GetAPIKeyByPrefix: %v", err)
	}
	if got.KeyHash != "sha256:testhash" {
		t.Fatalf("KeyHash = %q, want persisted hash", got.KeyHash)
	}
	if got.Name != "CI key" || got.RequestsPerMinute != 12 || got.TokensPerMinute != 3456 {
		t.Fatalf("unexpected key fields: %+v", got)
	}
	got, err = store.GetAPIKeyByHash("sha256:testhash")
	if err != nil {
		t.Fatalf("GetAPIKeyByHash: %v", err)
	}
	if got.ID != "key_test_1" {
		t.Fatalf("GetAPIKeyByHash returned %+v, want key_test_1", got)
	}

	keys, err := store.ListAPIKeys("system", false)
	if err != nil {
		t.Fatalf("ListAPIKeys: %v", err)
	}
	if len(keys) != 1 || keys[0].ID != "key_test_1" {
		t.Fatalf("ListAPIKeys = %+v, want key_test_1", keys)
	}

	lastUsed := time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)
	if err := store.TouchAPIKeyLastUsed("key_test_1", lastUsed); err != nil {
		t.Fatalf("TouchAPIKeyLastUsed: %v", err)
	}
	got, err = store.GetAPIKeyByPrefix("sk-test-prefix")
	if err != nil {
		t.Fatalf("GetAPIKeyByPrefix after touch: %v", err)
	}
	if got.LastUsedAt == nil || !got.LastUsedAt.Equal(lastUsed) {
		t.Fatalf("LastUsedAt = %v, want %v", got.LastUsedAt, lastUsed)
	}

	revokedAt := lastUsed.Add(time.Minute)
	if err := store.RevokeAPIKey("key_test_1", revokedAt); err != nil {
		t.Fatalf("RevokeAPIKey: %v", err)
	}
	keys, err = store.ListAPIKeys("system", false)
	if err != nil {
		t.Fatalf("ListAPIKeys after revoke: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("ListAPIKeys(includeRevoked=false) = %+v, want none", keys)
	}
	keys, err = store.ListAPIKeys("system", true)
	if err != nil {
		t.Fatalf("ListAPIKeys(includeRevoked=true): %v", err)
	}
	if len(keys) != 1 || keys[0].RevokedAt == nil || !keys[0].RevokedAt.Equal(revokedAt) {
		t.Fatalf("revoked key = %+v, want revoked_at %v", keys, revokedAt)
	}
}

func TestAPIModelRoutesValidateDefaultsAndResolve(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	defer store.Close()

	if err := store.CreateWorkflow(&WorkflowDefinition{
		ID:         "wf-api-route",
		Name:       "API Route Workflow",
		Definition: `{"id":"wf-api-route","name":"API Route Workflow","nodes":[]}`,
	}); err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if err := store.CreateWorkflow(&WorkflowDefinition{
		ID:         "wf-api-route-2",
		Name:       "API Route Workflow 2",
		Definition: `{"id":"wf-api-route-2","name":"API Route Workflow 2","nodes":[]}`,
	}); err != nil {
		t.Fatalf("CreateWorkflow second: %v", err)
	}

	if err := store.UpsertAPIModelRoute(&APIModelRoute{
		APIModel:    "consortium-test",
		Mode:        APIModelRouteModeWorkflow,
		WorkflowID:  "wf-api-route",
		Description: "workflow route",
		IsDefault:   true,
		Enabled:     true,
	}); err != nil {
		t.Fatalf("UpsertAPIModelRoute workflow: %v", err)
	}
	if err := store.UpsertAPIModelRoute(&APIModelRoute{
		APIModel:      "gpt-test",
		Mode:          APIModelRouteModeDirectModel,
		ProviderModel: "openai/gpt-test",
		Description:   "direct route",
		IsDefault:     true,
		Enabled:       true,
	}); err != nil {
		t.Fatalf("UpsertAPIModelRoute direct default: %v", err)
	}

	defaultRoute, err := store.GetDefaultAPIModelRoute()
	if err != nil {
		t.Fatalf("GetDefaultAPIModelRoute: %v", err)
	}
	if defaultRoute.APIModel != "gpt-test" {
		t.Fatalf("default route = %s, want gpt-test", defaultRoute.APIModel)
	}
	old, err := store.GetAPIModelRoute("consortium-test")
	if err != nil {
		t.Fatalf("GetAPIModelRoute consortium-test: %v", err)
	}
	if old.IsDefault {
		t.Fatalf("old default route still marked default: %+v", old)
	}

	err = store.UpsertAPIModelRoute(&APIModelRoute{
		APIModel:   "missing-workflow",
		Mode:       APIModelRouteModeWorkflow,
		WorkflowID: "does-not-exist",
		Enabled:    true,
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing workflow error = %v, want ErrNotFound", err)
	}

	err = store.UpsertAPIModelRoute(&APIModelRoute{
		APIModel: "bad-direct",
		Mode:     APIModelRouteModeDirectModel,
		Enabled:  true,
	})
	if err == nil || !strings.Contains(err.Error(), "provider_model") {
		t.Fatalf("bad direct route error = %v, want provider_model validation", err)
	}

	routes, err := store.ListAPIModelRoutes(false)
	if err != nil {
		t.Fatalf("ListAPIModelRoutes: %v", err)
	}
	if len(routes) < 2 {
		t.Fatalf("ListAPIModelRoutes len = %d, want at least 2", len(routes))
	}

	if err := store.DeleteAPIModelRoute("gpt-test"); err != nil {
		t.Fatalf("DeleteAPIModelRoute: %v", err)
	}
	if _, err := store.GetAPIModelRoute("gpt-test"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get deleted route error = %v, want ErrNotFound", err)
	}
}

func TestAPIUsageCreateUpdateListAndSummary(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	defer store.Close()

	createdAt := time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)
	usage := &APIUsageRecord{
		ID:             "usage-1",
		RequestID:      "req-1",
		KeyID:          "key-1",
		UserID:         "system",
		Endpoint:       "/v1/chat/completions",
		RequestedModel: "consortium-default",
		ResolvedModel:  "consortium-default",
		WorkflowID:     "wf-1",
		Status:         APIUsageStatusRunning,
		HTTPStatus:     200,
		Stream:         true,
		CreatedAt:      createdAt,
	}
	if err := store.CreateAPIUsage(usage); err != nil {
		t.Fatalf("CreateAPIUsage: %v", err)
	}

	completedAt := createdAt.Add(2 * time.Second)
	if err := store.UpdateAPIUsageCompletion("usage-1", APIUsageCompletion{
		JobID:        "job-1",
		Status:       APIUsageStatusSucceeded,
		HTTPStatus:   200,
		TokensInput:  11,
		TokensOutput: 7,
		TokensTotal:  18,
		Cost:         0.0123,
		LatencyMs:    2000,
		CompletedAt:  completedAt,
	}); err != nil {
		t.Fatalf("UpdateAPIUsageCompletion: %v", err)
	}

	rows, err := store.ListAPIUsage(APIUsageFilters{
		KeyID:  "key-1",
		Status: APIUsageStatusSucceeded,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("ListAPIUsage: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("ListAPIUsage len = %d, want 1 rows=%+v", len(rows), rows)
	}
	got := rows[0]
	if got.JobID != "job-1" || got.TokensTotal != 18 || got.CompletedAt == nil || !got.CompletedAt.Equal(completedAt) {
		t.Fatalf("usage row after completion = %+v", got)
	}
	if err := store.UpdateAPIUsageCompletion("usage-1", APIUsageCompletion{
		Status:       APIUsageStatusSucceeded,
		HTTPStatus:   200,
		TokensInput:  11,
		TokensOutput: 7,
		TokensTotal:  18,
		Cost:         0.0123,
		LatencyMs:    2000,
		CompletedAt:  completedAt,
	}); err != nil {
		t.Fatalf("UpdateAPIUsageCompletion without job: %v", err)
	}
	rows, err = store.ListAPIUsage(APIUsageFilters{KeyID: "key-1", Status: APIUsageStatusSucceeded, Limit: 10})
	if err != nil {
		t.Fatalf("ListAPIUsage after second completion: %v", err)
	}
	if len(rows) != 1 || rows[0].JobID != "job-1" {
		t.Fatalf("usage row after no-job completion = %+v", rows)
	}

	summary, err := store.SummarizeAPIUsage(APIUsageFilters{KeyID: "key-1"})
	if err != nil {
		t.Fatalf("SummarizeAPIUsage: %v", err)
	}
	if summary.Requests != 1 || summary.TokensTotal != 18 || summary.Cost != 0.0123 {
		t.Fatalf("summary = %+v, want one request and totals", summary)
	}
}

func TestAPIUsageCompletionByJobUpdatesAttachedUsage(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	defer store.Close()

	createdAt := time.Date(2026, 6, 24, 11, 0, 0, 0, time.UTC)
	if err := store.CreateAPIUsage(&APIUsageRecord{
		ID:        "usage-by-job",
		RequestID: "req-by-job",
		KeyID:     "key-by-job",
		UserID:    "system",
		Endpoint:  "/v1/responses",
		Status:    APIUsageStatusRunning,
		CreatedAt: createdAt,
	}); err != nil {
		t.Fatalf("CreateAPIUsage: %v", err)
	}
	if err := store.AttachAPIUsageJob("usage-by-job", "job-by-job"); err != nil {
		t.Fatalf("AttachAPIUsageJob: %v", err)
	}

	completedAt := createdAt.Add(time.Second)
	if err := store.UpdateAPIUsageCompletionByJob("key-by-job", "/v1/responses", "job-by-job", APIUsageCompletion{
		Status:       APIUsageStatusFailed,
		HTTPStatus:   502,
		TokensInput:  4,
		TokensOutput: 2,
		TokensTotal:  6,
		ErrorCode:    "UPSTREAM_ERROR",
		ErrorMessage: "provider unavailable",
		CompletedAt:  completedAt,
	}); err != nil {
		t.Fatalf("UpdateAPIUsageCompletionByJob: %v", err)
	}

	rows, err := store.ListAPIUsage(APIUsageFilters{KeyID: "key-by-job", Limit: 1})
	if err != nil {
		t.Fatalf("ListAPIUsage: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("ListAPIUsage len = %d, want 1", len(rows))
	}
	got := rows[0]
	if got.JobID != "job-by-job" || got.Status != APIUsageStatusFailed || got.HTTPStatus != 502 {
		t.Fatalf("usage row = %+v, want attached failed completion", got)
	}
	if got.ErrorCode != "UPSTREAM_ERROR" || got.ErrorMessage != "provider unavailable" || got.TokensTotal != 6 {
		t.Fatalf("usage failure details = %+v", got)
	}
}

func TestOpenAIAPIMetricsSummarizeLifecycleAndStaleRows(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	completedAt := now
	for _, record := range []*APIUsageRecord{
		{
			ID:             "usage-v1-ok",
			RequestID:      "req-v1-ok",
			KeyID:          "key-1",
			UserID:         "system",
			Endpoint:       "/v1/chat/completions",
			RequestedModel: "gpt-test",
			Status:         APIUsageStatusSucceeded,
			HTTPStatus:     200,
			TokensInput:    3,
			TokensOutput:   7,
			TokensTotal:    10,
			Cost:           0.01,
			LatencyMs:      120,
			CreatedAt:      now.Add(-time.Minute),
			CompletedAt:    &completedAt,
		},
		{
			ID:             "usage-v1-running-stale",
			RequestID:      "req-v1-running-stale",
			KeyID:          "key-1",
			UserID:         "system",
			Endpoint:       "/v1/responses",
			RequestedModel: "gpt-test",
			Status:         APIUsageStatusRunning,
			HTTPStatus:     200,
			CreatedAt:      now.Add(-30 * time.Minute),
		},
		{
			ID:             "usage-admin-ignored",
			RequestID:      "req-admin-ignored",
			KeyID:          "key-1",
			UserID:         "system",
			Endpoint:       "/api/workflows/submit",
			RequestedModel: "gpt-test",
			Status:         APIUsageStatusSucceeded,
			HTTPStatus:     200,
			TokensTotal:    99,
			CreatedAt:      now,
		},
	} {
		if err := store.CreateAPIUsage(record); err != nil {
			t.Fatalf("CreateAPIUsage %s: %v", record.ID, err)
		}
	}
	if err := store.CreateOpenAIObject(&OpenAIObjectRecord{
		ID:           "resp-stale",
		ObjectType:   OpenAIObjectTypeResponse,
		KeyID:        "key-1",
		UserID:       "system",
		Endpoint:     "/v1/responses",
		JobID:        "job-stale",
		Status:       OpenAIObjectStatusInProgress,
		Store:        true,
		Background:   true,
		RequestJSON:  `{}`,
		ResponseJSON: `{}`,
		UsageJSON:    `{}`,
		CreatedAt:    now.Add(-40 * time.Minute),
		UpdatedAt:    now.Add(-40 * time.Minute),
	}); err != nil {
		t.Fatalf("CreateOpenAIObject: %v", err)
	}
	if _, _, err := store.ReserveAPIIdempotency(&APIIdempotencyRecord{
		ID:                 "idem-pending",
		KeyID:              "key-1",
		IdempotencyKey:     "idem-key",
		RequestFingerprint: "fingerprint",
		CreatedAt:          now.Add(-5 * time.Minute),
		ExpiresAt:          now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("ReserveAPIIdempotency: %v", err)
	}

	metrics, err := store.GetOpenAIAPIMetrics(now.Add(-15*time.Minute), now)
	if err != nil {
		t.Fatalf("GetOpenAIAPIMetrics: %v", err)
	}
	if metrics.Usage.Requests != 2 || metrics.Usage.TokensTotal != 10 || metrics.Usage.Cost != 0.01 {
		t.Fatalf("usage metrics = %+v", metrics.Usage)
	}
	if metrics.RequestsByStatus[APIUsageStatusSucceeded] != 1 || metrics.RequestsByStatus[APIUsageStatusRunning] != 1 {
		t.Fatalf("requests by status = %+v", metrics.RequestsByStatus)
	}
	if metrics.RequestsByEndpoint["/v1/chat/completions"] != 1 || metrics.RequestsByEndpoint["/v1/responses"] != 1 {
		t.Fatalf("requests by endpoint = %+v", metrics.RequestsByEndpoint)
	}
	if metrics.HTTPStatusClasses["2xx"] != 2 {
		t.Fatalf("http status classes = %+v", metrics.HTTPStatusClasses)
	}
	if metrics.StaleRunningUsage != 1 || metrics.StaleBackgroundResponses != 1 || metrics.PendingIdempotency != 1 {
		t.Fatalf("stale metrics = %+v", metrics)
	}
	if metrics.AvgLatencyMs != 120 {
		t.Fatalf("avg latency = %v, want 120", metrics.AvgLatencyMs)
	}
}

func TestAPIIdempotencyReserveCompleteAndConflict(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	rec := &APIIdempotencyRecord{
		ID:                 "idem-1",
		KeyID:              "key-1",
		IdempotencyKey:     "idem-key",
		RequestFingerprint: "fingerprint-a",
		JobID:              "job-1",
		CreatedAt:          now,
		ExpiresAt:          now.Add(24 * time.Hour),
	}
	created, existing, err := store.ReserveAPIIdempotency(rec)
	if err != nil {
		t.Fatalf("ReserveAPIIdempotency: %v", err)
	}
	if !created || existing.ID != "idem-1" {
		t.Fatalf("created=%v existing=%+v, want created idem-1", created, existing)
	}
	if err := store.AttachAPIIdempotencyJob("key-1", "idem-key", "job-attached"); err != nil {
		t.Fatalf("AttachAPIIdempotencyJob: %v", err)
	}
	existing, err = store.GetAPIIdempotency("key-1", "idem-key")
	if err != nil {
		t.Fatalf("GetAPIIdempotency after attach: %v", err)
	}
	if existing.JobID != "job-attached" {
		t.Fatalf("attached idempotency = %+v, want job-attached", existing)
	}

	created, existing, err = store.ReserveAPIIdempotency(&APIIdempotencyRecord{
		ID:                 "idem-2",
		KeyID:              "key-1",
		IdempotencyKey:     "idem-key",
		RequestFingerprint: "fingerprint-a",
		JobID:              "job-2",
		CreatedAt:          now,
		ExpiresAt:          now.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("ReserveAPIIdempotency same fingerprint: %v", err)
	}
	if created || existing.JobID != "job-attached" {
		t.Fatalf("created=%v existing=%+v, want original job", created, existing)
	}

	_, _, err = store.ReserveAPIIdempotency(&APIIdempotencyRecord{
		ID:                 "idem-3",
		KeyID:              "key-1",
		IdempotencyKey:     "idem-key",
		RequestFingerprint: "fingerprint-b",
		CreatedAt:          now,
		ExpiresAt:          now.Add(24 * time.Hour),
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("different fingerprint error = %v, want ErrConflict", err)
	}

	if err := store.CompleteAPIIdempotency("key-1", "idem-key", `{"id":"chatcmpl_1"}`, 200); err != nil {
		t.Fatalf("CompleteAPIIdempotency: %v", err)
	}
	existing, err = store.GetAPIIdempotency("key-1", "idem-key")
	if err != nil {
		t.Fatalf("GetAPIIdempotency: %v", err)
	}
	if existing.ResponseBody != `{"id":"chatcmpl_1"}` || existing.HTTPStatus != 200 {
		t.Fatalf("completed idempotency = %+v", existing)
	}
	if err := store.DeleteAPIIdempotency("key-1", "idem-key"); err != nil {
		t.Fatalf("DeleteAPIIdempotency: %v", err)
	}
	if _, err := store.GetAPIIdempotency("key-1", "idem-key"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetAPIIdempotency after delete error = %v, want ErrNotFound", err)
	}
}

func TestAPIIdempotencyReservePrunesExpiredRows(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	if _, _, err := store.ReserveAPIIdempotency(&APIIdempotencyRecord{
		ID:                 "idem-expired",
		KeyID:              "key-1",
		IdempotencyKey:     "expired-key",
		RequestFingerprint: "fingerprint-expired",
		CreatedAt:          now.Add(-2 * time.Hour),
		ExpiresAt:          now.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("ReserveAPIIdempotency expired seed: %v", err)
	}
	if _, _, err := store.ReserveAPIIdempotency(&APIIdempotencyRecord{
		ID:                 "idem-active",
		KeyID:              "key-1",
		IdempotencyKey:     "active-key",
		RequestFingerprint: "fingerprint-active",
		CreatedAt:          now,
		ExpiresAt:          now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("ReserveAPIIdempotency active row: %v", err)
	}

	if _, err := store.GetAPIIdempotency("key-1", "expired-key"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired row lookup err = %v, want ErrNotFound", err)
	}
	if _, err := store.GetAPIIdempotency("key-1", "active-key"); err != nil {
		t.Fatalf("active row lookup: %v", err)
	}
}

func TestAPIIdempotencyCompletionByIDDoesNotOverwriteReusedKey(t *testing.T) {
	store, err := NewStorage(":memory:")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 6, 24, 10, 0, 0, 0, time.UTC)
	if _, _, err := store.ReserveAPIIdempotency(&APIIdempotencyRecord{
		ID:                 "idem-old",
		KeyID:              "key-1",
		IdempotencyKey:     "same-key",
		RequestFingerprint: "fingerprint-old",
		CreatedAt:          now.Add(-2 * time.Hour),
		ExpiresAt:          now.Add(-time.Hour),
	}); err != nil {
		t.Fatalf("ReserveAPIIdempotency old row: %v", err)
	}
	created, current, err := store.ReserveAPIIdempotency(&APIIdempotencyRecord{
		ID:                 "idem-new",
		KeyID:              "key-1",
		IdempotencyKey:     "same-key",
		RequestFingerprint: "fingerprint-new",
		CreatedAt:          now,
		ExpiresAt:          now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("ReserveAPIIdempotency reused key: %v", err)
	}
	if !created || current.ID != "idem-new" {
		t.Fatalf("created=%v current=%+v, want new reservation", created, current)
	}

	if err := store.CompleteAPIIdempotencyByID("idem-old", "key-1", `{"id":"old-response"}`, 200); !errors.Is(err, ErrNotFound) {
		t.Fatalf("CompleteAPIIdempotencyByID old row err = %v, want ErrNotFound", err)
	}
	if err := store.AttachAPIIdempotencyJobByID("idem-old", "key-1", "job-old"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AttachAPIIdempotencyJobByID old row err = %v, want ErrNotFound", err)
	}

	current, err = store.GetAPIIdempotency("key-1", "same-key")
	if err != nil {
		t.Fatalf("GetAPIIdempotency reused key: %v", err)
	}
	if current.ID != "idem-new" || current.ResponseBody != "" || current.JobID != "" || current.HTTPStatus != 0 {
		t.Fatalf("current row was overwritten by old completion: %+v", current)
	}
}
