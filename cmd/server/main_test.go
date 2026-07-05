package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateAdminExposureAllowsLoopbackWithoutToken(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:8080", "localhost:8080", "[::1]:8080"} {
		if err := validateAdminExposure(addr, "", ""); err != nil {
			t.Fatalf("validateAdminExposure(%q) = %v, want nil", addr, err)
		}
	}
}

func TestValidateAdminExposureRejectsPublicBindWithoutGuard(t *testing.T) {
	for _, addr := range []string{":8080", "0.0.0.0:8080", "[::]:8080", "192.0.2.10:8080"} {
		if err := validateAdminExposure(addr, "", ""); err == nil {
			t.Fatalf("validateAdminExposure(%q) succeeded without token/override", addr)
		}
	}
}

func TestValidateAdminExposureAllowsPublicBindWithTokenOrExplicitOverride(t *testing.T) {
	if err := validateAdminExposure("0.0.0.0:8080", "admin-secret", ""); err != nil {
		t.Fatalf("validateAdminExposure with token: %v", err)
	}
	if err := validateAdminExposure("0.0.0.0:8080", "", "true"); err != nil {
		t.Fatalf("validateAdminExposure with override: %v", err)
	}
}

func TestAdminTokenMiddlewareProtectsOnlyAdminRoutes(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := adminTokenMiddleware("admin-secret")(next)

	for _, path := range []string{"/health", "/v1/models"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusNoContent {
			t.Fatalf("%s status = %d, want passthrough", path, w.Code)
		}
	}

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/admin/overview", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d, want 401", w.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/overview", nil)
	req.Header.Set("Authorization", "Bearer admin-secret")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("bearer token status = %d, want passthrough", w.Code)
	}
}
