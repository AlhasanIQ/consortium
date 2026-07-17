package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// TrimTrailingSlash — additional cases not covered by middleware_test.go
// ---------------------------------------------------------------------------

func TestTrimTrailingSlash_NilURL(t *testing.T) {
	// Requests with a nil URL should pass through unchanged without panicking.
	called := false
	handler := TrimTrailingSlash(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/path", nil)
	req.URL = nil
	rec := httptest.NewRecorder()

	// Must not panic.
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("expected inner handler to be called even with nil URL")
	}
}

func TestTrimTrailingSlash_PathWithNoSlash(t *testing.T) {
	// Paths that don't end in a slash should be passed through unchanged.
	var gotPath string
	handler := TrimTrailingSlash(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if gotPath != "/api/jobs" {
		t.Errorf("path = %q, want /api/jobs (unchanged)", gotPath)
	}
}

func TestTrimTrailingSlash_MultipleTrailingSlashes(t *testing.T) {
	// strings.TrimRight removes all trailing slashes.
	var gotPath string
	handler := TrimTrailingSlash(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/jobs///", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if gotPath != "/api/jobs" {
		t.Errorf("path = %q, want /api/jobs", gotPath)
	}
}

func TestTrimTrailingSlash_EmptyPathNormalisedToRoot(t *testing.T) {
	// If trimming leaves an empty string, it should fall back to "/".
	var gotPath string
	handler := TrimTrailingSlash(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))

	// Manually construct a request whose Path is just "/"  — trimming "/" must
	// preserve "/" (root guard fires before TrimRight).
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if gotPath != "/" {
		t.Errorf("path = %q, want /", gotPath)
	}
}

func TestTrimTrailingSlash_RawPath(t *testing.T) {
	// When r.URL.RawPath is set (percent-encoded path), the middleware must
	// trim its trailing slash as well.
	var gotRawPath string
	handler := TrimTrailingSlash(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawPath = r.URL.RawPath
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/resources/hello%20world/", nil)
	// httptest sets RawPath when the URL contains percent-encoded characters.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if strings.HasSuffix(gotRawPath, "/") {
		t.Errorf("RawPath = %q still has trailing slash", gotRawPath)
	}
}

func TestTrimTrailingSlash_RawPathEmptyNotModified(t *testing.T) {
	// When RawPath is empty, the middleware must not set it to some non-empty value.
	var gotRawPath string
	handler := TrimTrailingSlash(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawPath = r.URL.RawPath
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// RawPath was empty; must remain empty (no mutation).
	if gotRawPath != "" {
		t.Errorf("RawPath = %q, want empty when not originally set", gotRawPath)
	}
}

func TestTrimTrailingSlash_RawPathRootPreserved(t *testing.T) {
	// When RawPath is exactly "/", it must remain "/".
	var gotRawPath string
	handler := TrimTrailingSlash(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawPath = r.URL.RawPath
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/some-path", nil)
	// Manually set RawPath to "/" to hit the RawPath == "/" guard.
	req.URL.RawPath = "/"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if gotRawPath != "/" {
		t.Errorf("RawPath = %q, want / (root preserved)", gotRawPath)
	}
}

func TestTrimTrailingSlash_PreservesURLQuery(t *testing.T) {
	var gotURL *url.URL
	handler := TrimTrailingSlash(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/data/?q=foo", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if gotURL == nil {
		t.Fatal("handler did not receive a URL")
	}
	if gotURL.Path != "/api/data" {
		t.Errorf("Path = %q, want /api/data", gotURL.Path)
	}
	if gotURL.RawQuery != "q=foo" {
		t.Errorf("RawQuery = %q, want q=foo", gotURL.RawQuery)
	}
}

// ---------------------------------------------------------------------------
// Compression — default config zero-value fallback
// ---------------------------------------------------------------------------

func TestCompression_DefaultConfigApplied(t *testing.T) {
	// A zero CompressionConfig should apply defaults (MinSize=1024, Level=5)
	// and still compress large-enough responses.
	original := strings.Repeat(`{"key":"value"}`, 100) // > 1024 bytes
	handler := Compression(CompressionConfig{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, original)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip with default config", got)
	}
}

func TestCompression_ResponseBelowMinSizeNotCompressed(t *testing.T) {
	// A response smaller than MinSize must not be compressed even if the
	// content type is compressible.
	small := `{"ok":true}` // 11 bytes — below the 64-byte minimum
	handler := Compression(CompressionConfig{MinSize: 64, Level: 1})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, small)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/tiny", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got == "gzip" {
		t.Fatalf("Content-Encoding = %q, want empty for small response", got)
	}
	if rec.Body.String() != small {
		t.Fatalf("body mismatch: got %q, want %q", rec.Body.String(), small)
	}
}

// ---------------------------------------------------------------------------
// ShouldCompress — edge cases
// ---------------------------------------------------------------------------

func TestShouldCompress_AudioAndVideoSkipped(t *testing.T) {
	skip := []string{
		"audio/mpeg",
		"audio/ogg",
		"video/webm",
		"video/mpeg",
	}
	for _, ct := range skip {
		t.Run(ct, func(t *testing.T) {
			if ShouldCompress(ct) {
				t.Errorf("ShouldCompress(%q) = true, want false", ct)
			}
		})
	}
}

func TestShouldCompress_UnknownTypeFalse(t *testing.T) {
	unknowns := []string{
		"application/octet-stream",
		"application/pdf",
		"model/gltf+json", // not in compressible list
		"",                // empty string
	}
	for _, ct := range unknowns {
		t.Run(ct, func(t *testing.T) {
			if ShouldCompress(ct) {
				t.Errorf("ShouldCompress(%q) = true, want false", ct)
			}
		})
	}
}

func TestShouldCompress_GzipAndZstdSkipped(t *testing.T) {
	// Already-compressed application formats must not be double-compressed.
	for _, ct := range []string{"application/gzip", "application/zip", "application/zstd"} {
		if ShouldCompress(ct) {
			t.Errorf("ShouldCompress(%q) = true, want false (already compressed)", ct)
		}
	}
}

func TestShouldCompress_FontFormatsSkipped(t *testing.T) {
	for _, ct := range []string{"font/woff", "font/woff2"} {
		if ShouldCompress(ct) {
			t.Errorf("ShouldCompress(%q) = true, want false (font format)", ct)
		}
	}
}

func TestShouldCompress_ImageFormatsHandledCorrectly(t *testing.T) {
	// Only SVG is compressible; raster formats are not.
	compressible := []string{"image/svg+xml"}
	skip := []string{"image/png", "image/jpeg", "image/gif", "image/webp"}

	for _, ct := range compressible {
		if !ShouldCompress(ct) {
			t.Errorf("ShouldCompress(%q) = false, want true", ct)
		}
	}
	for _, ct := range skip {
		if ShouldCompress(ct) {
			t.Errorf("ShouldCompress(%q) = true, want false", ct)
		}
	}
}
