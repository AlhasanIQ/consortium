package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// jsonHandler returns a handler that writes a large JSON response with the
// given status code. The response is large enough to trigger compression.
func jsonHandler(status int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
}

// applyProductionMiddleware applies the same middleware chain used in
// cmd/server/main.go: TrimTrailingSlash wraps the outer layer, then
// Recovery -> RequestID -> Logger -> CORS are applied via Use(), and
// Compression is applied to the inner handler.
func applyProductionMiddleware(inner http.Handler, withCompression bool) http.Handler {
	if withCompression {
		inner = Compression(CompressionConfig{MinSize: 64, Level: 1})(inner)
	}

	// Simulate mux.Use() ordering: Recovery -> RequestID -> Logger -> CORS.
	// mux.Use applies middleware so that the first registered runs outermost.
	h := CORS(inner)
	h = Logger(h)
	h = RequestID(h)
	h = Recovery(h)

	// TrimTrailingSlash wraps the entire router.
	h = TrimTrailingSlash(h)
	return h
}

func TestIntegration_CORSAndCompression(t *testing.T) {
	largeJSON := strings.Repeat(`{"key":"value"},`, 200)
	handler := applyProductionMiddleware(jsonHandler(http.StatusOK, largeJSON), true)

	t.Run("compressed response includes CORS headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
		req.Header.Set("Origin", "http://localhost:3000")
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		// CORS headers must be present.
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
			t.Fatalf("Access-Control-Allow-Origin=%q want=%q", got, "http://localhost:3000")
		}
		if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
			t.Fatalf("Access-Control-Allow-Credentials=%q want=%q", got, "true")
		}

		// Response must be gzip-compressed.
		if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
			t.Fatalf("Content-Encoding=%q want=%q", got, "gzip")
		}

		// Verify the body decompresses to the original.
		zr, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
		if err != nil {
			t.Fatalf("gzip.NewReader: %v", err)
		}
		defer func() { _ = zr.Close() }()
		decompressed, err := io.ReadAll(zr)
		if err != nil {
			t.Fatalf("decompress: %v", err)
		}
		if string(decompressed) != largeJSON {
			t.Fatal("decompressed body does not match original")
		}
	})

	t.Run("compressed response includes request ID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if got := rec.Header().Get("X-Request-ID"); got == "" {
			t.Fatal("expected X-Request-ID header in compressed response")
		}
		if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
			t.Fatalf("Content-Encoding=%q want=%q", got, "gzip")
		}
	})
}

func TestIntegration_PathNormalizationWithMiddlewareChain(t *testing.T) {
	var capturedPath string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	handler := applyProductionMiddleware(inner, false)

	t.Run("trailing slash trimmed before handler sees request", func(t *testing.T) {
		capturedPath = ""
		req := httptest.NewRequest(http.MethodGet, "/api/workflows/", nil)
		req.Header.Set("Origin", "http://localhost:5173")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if capturedPath != "/api/workflows" {
			t.Fatalf("handler saw path=%q want=%q", capturedPath, "/api/workflows")
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d want=%d", rec.Code, http.StatusOK)
		}
		// CORS should still work after path normalization.
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
			t.Fatalf("Access-Control-Allow-Origin=%q want=%q", got, "http://localhost:5173")
		}
	})

	t.Run("root path preserved", func(t *testing.T) {
		capturedPath = ""
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if capturedPath != "/" {
			t.Fatalf("handler saw path=%q want=%q", capturedPath, "/")
		}
	})

	t.Run("query string preserved after trailing slash trim", func(t *testing.T) {
		capturedPath = ""
		var capturedQuery string
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedPath = r.URL.Path
			capturedQuery = r.URL.RawQuery
			w.WriteHeader(http.StatusOK)
		})
		h := applyProductionMiddleware(inner, false)

		req := httptest.NewRequest(http.MethodGet, "/api/jobs/?status=running&limit=10", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if capturedPath != "/api/jobs" {
			t.Fatalf("path=%q want=%q", capturedPath, "/api/jobs")
		}
		if capturedQuery != "status=running&limit=10" {
			t.Fatalf("query=%q want=%q", capturedQuery, "status=running&limit=10")
		}
	})
}

func TestIntegration_PreflightThroughFullChain(t *testing.T) {
	nextCalled := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})
	handler := applyProductionMiddleware(inner, true)

	t.Run("OPTIONS preflight returns 204 without reaching handler", func(t *testing.T) {
		nextCalled = false
		req := httptest.NewRequest(http.MethodOptions, "/api/workflows", nil)
		req.Header.Set("Origin", "http://localhost:3000")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if nextCalled {
			t.Fatal("inner handler should not be called for preflight")
		}
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status=%d want=%d", rec.Code, http.StatusNoContent)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
			t.Fatalf("Access-Control-Allow-Origin=%q want=%q", got, "http://localhost:3000")
		}
		if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
			t.Fatal("expected Access-Control-Allow-Methods header")
		}
		// Request ID should still be generated for preflight.
		if got := rec.Header().Get("X-Request-ID"); got == "" {
			t.Fatal("expected X-Request-ID header on preflight response")
		}
	})

	t.Run("OPTIONS preflight with trailing slash still works", func(t *testing.T) {
		nextCalled = false
		req := httptest.NewRequest(http.MethodOptions, "/api/workflows/", nil)
		req.Header.Set("Origin", "http://localhost:3000")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if nextCalled {
			t.Fatal("inner handler should not be called for preflight with trailing slash")
		}
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status=%d want=%d", rec.Code, http.StatusNoContent)
		}
	})
}

func TestIntegration_RecoveryProtectsFullChain(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("simulated crash")
	})
	handler := applyProductionMiddleware(inner, false)

	req := httptest.NewRequest(http.MethodGet, "/api/crash", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()

	// Must not panic.
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d want=%d", rec.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(rec.Body.String(), "Internal server error") {
		t.Fatalf("body=%q should contain error message", rec.Body.String())
	}
	// Request ID should be present even on panic recovery.
	if got := rec.Header().Get("X-Request-ID"); got == "" {
		t.Fatal("expected X-Request-ID header on panic recovery response")
	}
}

func TestIntegration_RequestIDPropagatesThroughChain(t *testing.T) {
	var ctxRequestID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxRequestID = GetRequestID(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	handler := applyProductionMiddleware(inner, false)

	t.Run("generated ID reaches handler and response", func(t *testing.T) {
		ctxRequestID = ""
		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		responseID := rec.Header().Get("X-Request-ID")
		if responseID == "" {
			t.Fatal("expected X-Request-ID in response")
		}
		if ctxRequestID == "" || ctxRequestID == "unknown" {
			t.Fatal("expected request ID in context")
		}
		if ctxRequestID != responseID {
			t.Fatalf("context ID=%q != response ID=%q", ctxRequestID, responseID)
		}
	})

	t.Run("client-provided ID preserved through chain", func(t *testing.T) {
		ctxRequestID = ""
		clientID := "client-trace-abc-123"
		req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		req.Header.Set("X-Request-ID", clientID)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if ctxRequestID != clientID {
			t.Fatalf("context ID=%q want=%q", ctxRequestID, clientID)
		}
		if got := rec.Header().Get("X-Request-ID"); got != clientID {
			t.Fatalf("response ID=%q want=%q", got, clientID)
		}
	})
}

func TestIntegration_DisallowedOriginDoesNotBlockCompression(t *testing.T) {
	largeJSON := strings.Repeat(`{"data":"x"},`, 200)
	handler := applyProductionMiddleware(jsonHandler(http.StatusOK, largeJSON), true)

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no Access-Control-Allow-Origin for disallowed origin, got=%q", got)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding=%q want=%q", got, "gzip")
	}
}

func TestIntegration_EndToEndMockHandler(t *testing.T) {
	// Simulates a full request lifecycle through the complete middleware chain
	// with a mock API handler, similar to what an actual /api/workflows request
	// would go through.

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := GetRequestID(r.Context())
		body := `{"workflows":["wf-1","wf-2","wf-3"],"request_id":"` + reqID + `"}`
		// Pad to exceed compression minimum.
		body = body + strings.Repeat(" ", 200)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	})

	handler := applyProductionMiddleware(inner, true)

	req := httptest.NewRequest(http.MethodGet, "/api/workflows/", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("X-Request-ID", "e2e-test-id-001")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Status OK.
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want=%d", rec.Code, http.StatusOK)
	}

	// Request ID preserved.
	if got := rec.Header().Get("X-Request-ID"); got != "e2e-test-id-001" {
		t.Fatalf("X-Request-ID=%q want=%q", got, "e2e-test-id-001")
	}

	// CORS headers present.
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("Access-Control-Allow-Origin=%q want=%q", got, "http://localhost:3000")
	}

	// Response is compressed.
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding=%q want=%q", got, "gzip")
	}

	// Decompress and verify body content.
	zr, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer func() { _ = zr.Close() }()
	decompressed, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	body := string(decompressed)

	// Verify the request ID was available to the handler via context.
	if !strings.Contains(body, `"request_id":"e2e-test-id-001"`) {
		t.Fatalf("body should contain request_id from context, got: %s", body[:min(len(body), 200)])
	}
	if !strings.Contains(body, `"workflows"`) {
		t.Fatalf("body should contain workflows key, got: %s", body[:min(len(body), 200)])
	}
}
