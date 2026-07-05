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

func TestCORS_LocalDevOriginsAllowed(t *testing.T) {
	devOrigins := []string{
		"http://localhost:3000",
		"http://localhost:3001",
		"http://localhost:5173",
		"http://localhost:5174",
		"http://127.0.0.1:3000",
		"http://127.0.0.1:3001",
		"http://127.0.0.1:5173",
		"http://127.0.0.1:5174",
	}

	for _, origin := range devOrigins {
		t.Run(origin, func(t *testing.T) {
			nextCalled := false
			handler := CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusAccepted)
			}))

			req := httptest.NewRequest(http.MethodGet, "/api/workflows", nil)
			req.Header.Set("Origin", origin)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if !nextCalled {
				t.Fatal("expected next handler to be called")
			}
			if rec.Code != http.StatusAccepted {
				t.Fatalf("status=%d want=%d", rec.Code, http.StatusAccepted)
			}
			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
				t.Fatalf("allow-origin=%q want=%q", got, origin)
			}
			if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
				t.Fatalf("allow-credentials=%q want=%q", got, "true")
			}
		})
	}
}

func TestCORS_CIAndProductionScenarios(t *testing.T) {
	tests := []struct {
		name              string
		origin            string
		wantAllowOrigin   string
		wantAllowCreds    string
		wantNextCalled    bool
		wantStatus        int
		requestMethod     string
		expectNoBodyWrite bool
	}{
		{
			name:              "ci-no-origin",
			origin:            "",
			wantAllowOrigin:   "*",
			wantAllowCreds:    "",
			wantNextCalled:    true,
			wantStatus:        http.StatusCreated,
			requestMethod:     http.MethodPost,
			expectNoBodyWrite: false,
		},
		{
			name:              "production-cross-origin-disallowed",
			origin:            "https://app.example.com",
			wantAllowOrigin:   "",
			wantAllowCreds:    "",
			wantNextCalled:    true,
			wantStatus:        http.StatusCreated,
			requestMethod:     http.MethodPost,
			expectNoBodyWrite: false,
		},
		{
			name:              "preflight-local-dev",
			origin:            "http://localhost:3000",
			wantAllowOrigin:   "http://localhost:3000",
			wantAllowCreds:    "true",
			wantNextCalled:    false,
			wantStatus:        http.StatusNoContent,
			requestMethod:     http.MethodOptions,
			expectNoBodyWrite: true,
		},
		{
			name:              "preflight-ci-no-origin",
			origin:            "",
			wantAllowOrigin:   "*",
			wantAllowCreds:    "",
			wantNextCalled:    false,
			wantStatus:        http.StatusNoContent,
			requestMethod:     http.MethodOptions,
			expectNoBodyWrite: true,
		},
		{
			name:              "preflight-production-disallowed",
			origin:            "https://app.example.com",
			wantAllowOrigin:   "",
			wantAllowCreds:    "",
			wantNextCalled:    false,
			wantStatus:        http.StatusNoContent,
			requestMethod:     http.MethodOptions,
			expectNoBodyWrite: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextCalled := false
			handler := CORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte("ok"))
			}))

			req := httptest.NewRequest(tt.requestMethod, "/api/jobs", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if nextCalled != tt.wantNextCalled {
				t.Fatalf("nextCalled=%v want=%v", nextCalled, tt.wantNextCalled)
			}
			if rec.Code != tt.wantStatus {
				t.Fatalf("status=%d want=%d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != tt.wantAllowOrigin {
				t.Fatalf("allow-origin=%q want=%q", got, tt.wantAllowOrigin)
			}
			if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != tt.wantAllowCreds {
				t.Fatalf("allow-credentials=%q want=%q", got, tt.wantAllowCreds)
			}
			if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
				t.Fatal("expected Access-Control-Allow-Methods header")
			}
			if got := rec.Header().Get("Access-Control-Allow-Headers"); got == "" {
				t.Fatal("expected Access-Control-Allow-Headers header")
			}
			if got := rec.Header().Get("Access-Control-Expose-Headers"); got == "" {
				t.Fatal("expected Access-Control-Expose-Headers header")
			}
			if got := rec.Header().Get("Access-Control-Max-Age"); got == "" {
				t.Fatal("expected Access-Control-Max-Age header")
			}
			if tt.expectNoBodyWrite && rec.Body.Len() != 0 {
				t.Fatalf("expected empty body for preflight, got %q", rec.Body.String())
			}
		})
	}
}

func TestShouldCompress(t *testing.T) {
	tests := []struct {
		contentType string
		want        bool
	}{
		{contentType: "application/json", want: true},
		{contentType: "application/json; charset=utf-8", want: true},
		{contentType: "text/html", want: true},
		{contentType: "image/svg+xml", want: true},
		{contentType: "image/png", want: false},
		{contentType: "font/woff2", want: false},
		{contentType: "video/mp4", want: false},
		{contentType: "application/zip", want: false},
		{contentType: "application/octet-stream", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			got := ShouldCompress(tt.contentType)
			if got != tt.want {
				t.Fatalf("ShouldCompress(%q)=%v want=%v", tt.contentType, got, tt.want)
			}
		})
	}
}

func TestCompression_CompressesCompressibleResponse(t *testing.T) {
	original := strings.Repeat(`{"k":"v"}`, 200)
	handler := Compression(CompressionConfig{
		MinSize: 64,
		Level:   1,
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, original)
	}))

	req := httptest.NewRequest(http.MethodGet, "/assets/app.json", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("content-encoding=%q want=%q", got, "gzip")
	}

	zr, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("gzip.NewReader failed: %v", err)
	}
	defer func() { _ = zr.Close() }()
	decompressed, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("failed to decompress response: %v", err)
	}
	if got := string(decompressed); got != original {
		t.Fatalf("decompressed body mismatch")
	}
}

func TestCompression_SkipsNonCompressibleResponse(t *testing.T) {
	original := strings.Repeat("PNGDATA", 200)
	handler := Compression(CompressionConfig{
		MinSize: 64,
		Level:   1,
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = io.WriteString(w, original)
	}))

	req := httptest.NewRequest(http.MethodGet, "/images/logo.png", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("content-encoding=%q want empty", got)
	}
	if got := rec.Body.String(); got != original {
		t.Fatalf("body mismatch for uncompressed response")
	}
}

func TestCompression_SkipsWhenClientDoesNotAcceptGzip(t *testing.T) {
	original := strings.Repeat("payload", 200)
	handler := Compression(CompressionConfig{
		MinSize: 64,
		Level:   1,
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, original)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("content-encoding=%q want empty", got)
	}
	if got := rec.Body.String(); got != original {
		t.Fatalf("body mismatch when gzip is not requested")
	}
}
