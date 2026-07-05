package static

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestIsHashedAsset(t *testing.T) {
	tests := []struct {
		filename string
		want     bool
	}{
		// Vite-style hashed assets
		{"index-Capkq623.js", true},
		{"index--jBwn3_M.css", true},
		{"chunk-abc123.js", true},
		{"vendor-12ab34.js", true},

		// Non-hashed files
		{"index.html", false},
		{"favicon.ico", false},
		{"robots.txt", false},
		{"manifest.json", false},

		// Edge cases
		{"file-ab.js", false},            // Hash too short
		{"file-abcdefghijklm.js", false}, // Hash too long
		{"noextension", false},           // No extension, no hyphen
		{"simple.txt", false},            // No hash pattern
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got := IsHashedAsset(tt.filename)
			if got != tt.want {
				t.Errorf("IsHashedAsset(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

func TestSetCacheHeaders(t *testing.T) {
	tests := []struct {
		filePath  string
		wantCache string
	}{
		// Hashed assets get immutable
		{"assets/index-Capkq623.js", "public, max-age=31536000, immutable"},
		{"assets/index--jBwn3_M.css", "public, max-age=31536000, immutable"},
		{"assets/chunk-abc123.js", "public, max-age=31536000, immutable"},

		// Non-hashed files get no-cache
		{"index.html", "no-cache"},
		{"favicon.ico", "no-cache"},
		{"assets/logo.svg", "no-cache"},
	}

	for _, tt := range tests {
		t.Run(tt.filePath, func(t *testing.T) {
			w := httptest.NewRecorder()
			setCacheHeaders(w, tt.filePath)
			got := w.Header().Get("Cache-Control")
			if got != tt.wantCache {
				t.Errorf("setCacheHeaders(%q) = %q, want %q", tt.filePath, got, tt.wantCache)
			}
		})
	}
}

func TestSPAHandler_DevMode(t *testing.T) {
	// Create a temporary directory with test files
	tmpDir := t.TempDir()

	// Create index.html
	indexContent := `<!DOCTYPE html><html><head></head><body><div id="root"></div></body></html>`
	if err := os.WriteFile(filepath.Join(tmpDir, "index.html"), []byte(indexContent), 0644); err != nil {
		t.Fatalf("Failed to create index.html: %v", err)
	}

	// Create assets directory with a hashed JS file
	assetsDir := filepath.Join(tmpDir, "assets")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatalf("Failed to create assets dir: %v", err)
	}
	jsContent := `console.log("test");`
	if err := os.WriteFile(filepath.Join(assetsDir, "index-abc123.js"), []byte(jsContent), 0644); err != nil {
		t.Fatalf("Failed to create JS file: %v", err)
	}

	handler := Handler(Config{
		DevMode: true,
		DevPath: tmpDir,
	})

	tests := []struct {
		name          string
		path          string
		wantStatus    int
		wantCacheCtrl string
		wantContains  string
	}{
		{
			name:          "root serves index.html",
			path:          "/",
			wantStatus:    http.StatusOK,
			wantCacheCtrl: "no-cache",
			wantContains:  `<div id="root">`,
		},
		{
			name:          "SPA route serves index.html",
			path:          "/builder/123",
			wantStatus:    http.StatusOK,
			wantCacheCtrl: "no-cache",
			wantContains:  `<div id="root">`,
		},
		{
			name:          "deep SPA route serves index.html",
			path:          "/ensemble/test/run",
			wantStatus:    http.StatusOK,
			wantCacheCtrl: "no-cache",
			wantContains:  `<div id="root">`,
		},
		{
			name:          "hashed asset has immutable cache",
			path:          "/assets/index-abc123.js",
			wantStatus:    http.StatusOK,
			wantCacheCtrl: "public, max-age=31536000, immutable",
			wantContains:  `console.log`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			cacheCtrl := w.Header().Get("Cache-Control")
			if cacheCtrl != tt.wantCacheCtrl {
				t.Errorf("Cache-Control = %q, want %q", cacheCtrl, tt.wantCacheCtrl)
			}

			body := w.Body.String()
			if tt.wantContains != "" && !contains(body, tt.wantContains) {
				t.Errorf("body does not contain %q, got: %s", tt.wantContains, body)
			}
		})
	}
}

func TestSPAHandler_NotFoundFallback(t *testing.T) {
	// Create a temporary directory with only index.html
	tmpDir := t.TempDir()

	indexContent := `<!DOCTYPE html><html><body>SPA</body></html>`
	if err := os.WriteFile(filepath.Join(tmpDir, "index.html"), []byte(indexContent), 0644); err != nil {
		t.Fatalf("Failed to create index.html: %v", err)
	}

	handler := Handler(Config{
		DevMode: true,
		DevPath: tmpDir,
	})

	// Request a non-existent file - should get index.html (SPA fallback)
	req := httptest.NewRequest("GET", "/nonexistent/path/file.js", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	if !contains(w.Body.String(), "SPA") {
		t.Error("expected SPA fallback to index.html")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && searchString(s, substr)))
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
