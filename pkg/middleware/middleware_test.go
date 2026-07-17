package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestID(t *testing.T) {
	t.Run("generates new ID when none provided", func(t *testing.T) {
		handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := GetRequestID(r.Context())
			if id == "" || id == "unknown" {
				t.Error("Expected request ID to be set in context")
			}
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		responseID := rec.Header().Get("X-Request-ID")
		if responseID == "" {
			t.Error("Expected X-Request-ID header in response")
		}
	})

	t.Run("preserves provided request ID", func(t *testing.T) {
		providedID := "test-request-id-123"
		var capturedID string

		handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedID = GetRequestID(r.Context())
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("X-Request-ID", providedID)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if capturedID != providedID {
			t.Errorf("Context ID = %q, want %q", capturedID, providedID)
		}
		if got := rec.Header().Get("X-Request-ID"); got != providedID {
			t.Errorf("Response header = %q, want %q", got, providedID)
		}
	})
}

func TestRecovery(t *testing.T) {
	t.Run("recovers from panic", func(t *testing.T) {
		handler := Recovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("test panic")
		}))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req = req.WithContext(withRequestID(req.Context(), "test-id"))
		rec := httptest.NewRecorder()

		// Should not panic
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Errorf("Status = %d, want %d", rec.Code, http.StatusInternalServerError)
		}
		if !strings.Contains(rec.Body.String(), "Internal server error") {
			t.Error("Response should contain error message")
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
	})

	t.Run("passes through non-panic requests", func(t *testing.T) {
		handler := Recovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("success"))
		}))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
		}
		if rec.Body.String() != "success" {
			t.Errorf("Body = %q, want success", rec.Body.String())
		}
	})
}

func TestLogger(t *testing.T) {
	t.Run("logs request and captures status", func(t *testing.T) {
		handler := Logger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte("created"))
		}))

		req := httptest.NewRequest(http.MethodPost, "/api/test", nil)
		req = req.WithContext(withRequestID(req.Context(), "test-id"))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Errorf("Status = %d, want %d", rec.Code, http.StatusCreated)
		}
	})

	t.Run("defaults to 200 when WriteHeader not called", func(t *testing.T) {
		handler := Logger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("ok"))
		}))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req = req.WithContext(withRequestID(req.Context(), "test-id"))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
		}
	})

	t.Run("preserves flusher for streaming responses", func(t *testing.T) {
		handler := Logger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Fatal("wrapped response writer does not implement http.Flusher")
			}
			w.WriteHeader(http.StatusOK)
			flusher.Flush()
		}))

		req := httptest.NewRequest(http.MethodGet, "/stream", nil)
		req = req.WithContext(withRequestID(req.Context(), "test-id"))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if !rec.Flushed {
			t.Fatal("underlying recorder was not flushed")
		}
	})
}

func TestGetRequestID(t *testing.T) {
	t.Run("retrieves ID from context", func(t *testing.T) {
		ctx := withRequestID(context.Background(), "test-id-123")
		id := GetRequestID(ctx)
		if id != "test-id-123" {
			t.Errorf("GetRequestID() = %q, want test-id-123", id)
		}
	})

	t.Run("returns unknown when not set", func(t *testing.T) {
		ctx := context.Background()
		id := GetRequestID(ctx)
		if id != "unknown" {
			t.Errorf("GetRequestID() = %q, want unknown", id)
		}
	})

	t.Run("returns unknown for wrong type", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), requestIDKey, 12345)
		id := GetRequestID(ctx)
		if id != "unknown" {
			t.Errorf("GetRequestID() = %q, want unknown", id)
		}
	})
}

func TestTrimTrailingSlash(t *testing.T) {
	t.Run("trims trailing slash for non-root path", func(t *testing.T) {
		var gotPath string
		handler := TrimTrailingSlash(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Status = %d, want %d", rec.Code, http.StatusOK)
		}
		if gotPath != "/admin" {
			t.Fatalf("Path = %q, want /admin", gotPath)
		}
	})

	t.Run("keeps root path unchanged", func(t *testing.T) {
		var gotPath string
		handler := TrimTrailingSlash(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if gotPath != "/" {
			t.Fatalf("Path = %q, want /", gotPath)
		}
	})

	t.Run("preserves method and query", func(t *testing.T) {
		var gotPath, gotRawQuery, gotMethod string
		handler := TrimTrailingSlash(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotRawQuery = r.URL.RawQuery
			gotMethod = r.Method
			w.WriteHeader(http.StatusCreated)
		}))

		req := httptest.NewRequest(http.MethodPost, "/api/workflows/submit/?a=1&b=2", strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("Status = %d, want %d", rec.Code, http.StatusCreated)
		}
		if gotPath != "/api/workflows/submit" {
			t.Fatalf("Path = %q, want /api/workflows/submit", gotPath)
		}
		if gotRawQuery != "a=1&b=2" {
			t.Fatalf("RawQuery = %q, want a=1&b=2", gotRawQuery)
		}
		if gotMethod != http.MethodPost {
			t.Fatalf("Method = %q, want %q", gotMethod, http.MethodPost)
		}
	})
}
