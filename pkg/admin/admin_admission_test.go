package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/alhasaniq/consortium/pkg/storage"
)

// TestParseAdminBool verifies the helper that converts form values to booleans.
func TestParseAdminBool(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"1", true},
		{"true", true},
		{"TRUE", true},
		{"True", true},
		{"yes", true},
		{"YES", true},
		{"Yes", true},
		{"on", true},
		{"ON", true},
		{"On", true},
		// Leading/trailing whitespace should be trimmed before comparison.
		{"  true  ", true},
		{"  1  ", true},
		// Everything else is false.
		{"0", false},
		{"false", false},
		{"FALSE", false},
		{"no", false},
		{"off", false},
		{"", false},
		{"random", false},
		{"2", false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			got := parseAdminBool(tc.input)
			if got != tc.want {
				t.Errorf("parseAdminBool(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// TestHandleAdmissionStatus_InitiallyAccepting verifies that a fresh manager
// starts with admission open.
func TestHandleAdmissionStatus_InitiallyAccepting(t *testing.T) {
	_, router, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/admin/admission", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if paused, ok := resp["paused"].(bool); !ok || paused {
		t.Errorf("expected paused=false, got %v", resp["paused"])
	}
	if state, ok := resp["state"].(string); !ok || state != "accepting" {
		t.Errorf("expected state=accepting, got %v", resp["state"])
	}
}

// TestHandleAdmissionPause_ThenStatus tests that pausing transitions state to
// "paused" and the status endpoint reflects it.
func TestHandleAdmissionPause_ThenStatus(t *testing.T) {
	_, router, cleanup := setupTestServer(t)
	defer cleanup()

	// Pause with a message.
	form := url.Values{}
	form.Set("message", "routine maintenance")
	req := httptest.NewRequest(http.MethodPost, "/api/admin/admission/pause",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("pause: expected 200, got %d. body: %s", w.Code, w.Body.String())
	}

	var pauseResp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &pauseResp); err != nil {
		t.Fatalf("pause: invalid JSON: %v", err)
	}
	if success, _ := pauseResp["success"].(bool); !success {
		t.Errorf("pause: expected success=true, got %v", pauseResp["success"])
	}
	if changed, _ := pauseResp["changed"].(bool); !changed {
		t.Errorf("pause: first pause should report changed=true, got %v", pauseResp["changed"])
	}
	if state, _ := pauseResp["state"].(string); state != "paused" {
		t.Errorf("pause: expected state=paused, got %v", pauseResp["state"])
	}
	if paused, _ := pauseResp["paused"].(bool); !paused {
		t.Errorf("pause: expected paused=true, got %v", pauseResp["paused"])
	}

	// Check status reflects paused state.
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/api/admin/admission", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("status after pause: expected 200, got %d", w2.Code)
	}
	var statusResp map[string]interface{}
	if err := json.Unmarshal(w2.Body.Bytes(), &statusResp); err != nil {
		t.Fatalf("status after pause: invalid JSON: %v", err)
	}
	if state, _ := statusResp["state"].(string); state != "paused" {
		t.Errorf("status after pause: expected state=paused, got %v", statusResp["state"])
	}
}

// TestHandleAdmissionPause_Idempotent verifies that a second pause call returns
// changed=false (gate was already closed).
func TestHandleAdmissionPause_Idempotent(t *testing.T) {
	_, router, cleanup := setupTestServer(t)
	defer cleanup()

	pauseOnce := func() map[string]interface{} {
		t.Helper()
		form := url.Values{}
		form.Set("message", "test")
		req := httptest.NewRequest(http.MethodPost, "/api/admin/admission/pause",
			strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("pause: expected 200, got %d", w.Code)
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("pause: invalid JSON: %v", err)
		}
		return resp
	}

	first := pauseOnce()
	if changed, _ := first["changed"].(bool); !changed {
		t.Errorf("first pause should report changed=true, got %v", first["changed"])
	}

	second := pauseOnce()
	if changed, _ := second["changed"].(bool); changed {
		t.Errorf("second pause should report changed=false (already paused), got %v", second["changed"])
	}
}

// TestHandleAdmissionResume tests the full pause → resume cycle.
func TestHandleAdmissionResume(t *testing.T) {
	_, router, cleanup := setupTestServer(t)
	defer cleanup()

	// Pause first.
	form := url.Values{}
	form.Set("message", "test pause")
	req := httptest.NewRequest(http.MethodPost, "/api/admin/admission/pause",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(httptest.NewRecorder(), req)

	// Now resume.
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/admin/admission/resume", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("resume: expected 200, got %d. body: %s", w.Code, w.Body.String())
	}
	var resumeResp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resumeResp); err != nil {
		t.Fatalf("resume: invalid JSON: %v", err)
	}
	if success, _ := resumeResp["success"].(bool); !success {
		t.Errorf("resume: expected success=true")
	}
	if changed, _ := resumeResp["changed"].(bool); !changed {
		t.Errorf("resume: expected changed=true (was paused), got %v", resumeResp["changed"])
	}
	if state, _ := resumeResp["state"].(string); state != "accepting" {
		t.Errorf("resume: expected state=accepting, got %v", resumeResp["state"])
	}

	// Status endpoint must also show accepting.
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/api/admin/admission", nil))
	var statusResp map[string]interface{}
	if err := json.Unmarshal(w2.Body.Bytes(), &statusResp); err != nil {
		t.Fatalf("status after resume: invalid JSON: %v", err)
	}
	if state, _ := statusResp["state"].(string); state != "accepting" {
		t.Errorf("status after resume: expected accepting, got %v", statusResp["state"])
	}
}

// TestHandleAdmissionResume_Idempotent ensures resuming when not paused returns
// changed=false.
func TestHandleAdmissionResume_Idempotent(t *testing.T) {
	_, router, cleanup := setupTestServer(t)
	defer cleanup()

	// Resume on a fresh (already accepting) manager.
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/admin/admission/resume", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if changed, _ := resp["changed"].(bool); changed {
		t.Errorf("resume on non-paused state should report changed=false, got %v", resp["changed"])
	}
}

// TestHandleAdmissionPause_EmptyMessageUsesDefault verifies that an empty
// message param does not break the handler and a default reason is used.
func TestHandleAdmissionPause_EmptyMessageUsesDefault(t *testing.T) {
	_, router, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/admin/admission/pause", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. body: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if state, _ := resp["state"].(string); state != "paused" {
		t.Errorf("expected paused state, got %v", resp["state"])
	}
}

// TestHandleRetryJob_MissingID confirms a 400 when no job ID is given.
// We call the handler directly (bypassing the router) so that mux.Vars(r)
// returns an empty map, leaving the "id" variable unset.
func TestHandleRetryJob_MissingID(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/admin/jobs//retry", nil)
	w := httptest.NewRecorder()
	server.handleRetryJob(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d. body: %s", w.Code, w.Body.String())
	}
}

// TestHandleRetryJob_NotFound checks 404 when the job doesn't exist.
func TestHandleRetryJob_NotFound(t *testing.T) {
	_, router, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/admin/jobs/does-not-exist/retry", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d. body: %s", w.Code, w.Body.String())
	}
}

// TestHandleRetryJob_ConflictOnActiveJob verifies that retrying a
// pending/running job returns 409 Conflict.
func TestHandleRetryJob_ConflictOnActiveJob(t *testing.T) {
	server, router, cleanup := setupTestServer(t)
	defer cleanup()

	if err := server.storage.CreateExecution(&storage.Job{
		ID:     "retry-active-1",
		Model:  "test-model",
		Status: "pending",
	}); err != nil {
		t.Fatalf("create job: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/jobs/retry-active-1/retry", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d. body: %s", w.Code, w.Body.String())
	}
}

// TestHandleAdmissionPauseResume_RoundTrip exercises the complete lifecycle
// through the HTTP router: accept → pause → verify paused → resume → verify accepting.
func TestHandleAdmissionPauseResume_RoundTrip(t *testing.T) {
	_, router, cleanup := setupTestServer(t)
	defer cleanup()

	get := func() map[string]interface{} {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/admin/admission", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("GET /api/admin/admission: expected 200, got %d", w.Code)
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("GET /api/admin/admission: invalid JSON: %v", err)
		}
		return resp
	}

	post := func(path string) map[string]interface{} {
		t.Helper()
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("POST %s: expected 200, got %d. body: %s", path, w.Code, w.Body.String())
		}
		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("POST %s: invalid JSON: %v", path, err)
		}
		return resp
	}

	// Initially accepting.
	if state := get()["state"]; state != "accepting" {
		t.Fatalf("initial state should be accepting, got %v", state)
	}

	// Pause.
	pauseResp := post("/api/admin/admission/pause")
	if pauseResp["state"] != "paused" {
		t.Fatalf("after pause, state should be paused, got %v", pauseResp["state"])
	}

	// Status is paused.
	if state := get()["state"]; state != "paused" {
		t.Fatalf("GET after pause should show paused, got %v", state)
	}

	// Resume.
	resumeResp := post("/api/admin/admission/resume")
	if resumeResp["state"] != "accepting" {
		t.Fatalf("after resume, state should be accepting, got %v", resumeResp["state"])
	}

	// Status is accepting again.
	if state := get()["state"]; state != "accepting" {
		t.Fatalf("GET after resume should show accepting, got %v", state)
	}
}
