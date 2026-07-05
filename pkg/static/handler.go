package static

import (
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds static file handler configuration
type Config struct {
	// DevMode disables embedded assets and serves from filesystem
	DevMode bool
	// DevPath is the filesystem path to serve in dev mode
	DevPath string
}

// Handler returns an http.Handler that serves the embedded frontend
// with SPA routing, proper MIME types, and cache headers
func Handler(cfg Config) http.Handler {
	var fileSystem fs.FS
	var err error

	if cfg.DevMode {
		fileSystem = os.DirFS(cfg.DevPath)
	} else {
		fileSystem, err = FS()
		if err != nil {
			// Return a handler that always 404s if embedding failed
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "Frontend not embedded", http.StatusNotFound)
			})
		}
	}

	return &spaHandler{
		fs:    http.FS(fileSystem),
		rawFS: fileSystem,
	}
}

type spaHandler struct {
	fs    http.FileSystem
	rawFS fs.FS
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Clean the path
	urlPath := path.Clean(r.URL.Path)
	if urlPath == "/" {
		h.serveIndexHTML(w, r)
		return
	}

	// Remove leading slash for fs.Open
	filePath := strings.TrimPrefix(urlPath, "/")

	// Try to open the file
	file, err := h.rawFS.Open(filePath)
	if err != nil {
		// File not found, serve index.html for SPA routing
		h.serveIndexHTML(w, r)
		return
	}

	// Check if it's a directory
	stat, err := file.Stat()
	_ = file.Close()
	if err != nil || stat.IsDir() {
		// Directory or error, serve index.html
		h.serveIndexHTML(w, r)
		return
	}

	// Set cache headers based on filename pattern
	setCacheHeaders(w, filePath)

	// Set correct content type
	ext := filepath.Ext(filePath)
	if mimeType := mime.TypeByExtension(ext); mimeType != "" {
		w.Header().Set("Content-Type", mimeType)
	}

	// Serve the file
	http.FileServer(h.fs).ServeHTTP(w, r)
}

func (h *spaHandler) serveIndexHTML(w http.ResponseWriter, r *http.Request) {
	// No cache for index.html - must revalidate
	w.Header().Set("Cache-Control", "no-cache")

	// Open and serve index.html directly
	file, err := h.rawFS.Open("index.html")
	if err != nil {
		http.Error(w, "Frontend not found", http.StatusNotFound)
		return
	}
	defer func() { _ = file.Close() }()

	// Get file info for Content-Length
	stat, err := file.Stat()
	if err != nil {
		http.Error(w, "Frontend not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))

	// Read and write the content
	content, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read index.html", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

// setCacheHeaders sets appropriate Cache-Control headers based on filename
func setCacheHeaders(w http.ResponseWriter, filePath string) {
	// Check if file has content hash in name (Vite pattern: name-HASH.ext)
	base := filepath.Base(filePath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)

	// Vite hashed files contain a hyphen followed by hash
	// Examples: index-Capkq623.js, index--jBwn3_M.css
	if strings.Contains(name, "-") {
		parts := strings.Split(name, "-")
		hash := parts[len(parts)-1]
		// Vite hashes are typically 6-8 characters
		if len(hash) >= 6 && len(hash) <= 12 {
			// Immutable asset with content hash
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			return
		}
	}

	// Non-hashed files - no cache
	w.Header().Set("Cache-Control", "no-cache")
}

// IsHashedAsset checks if a filename appears to have a content hash
func IsHashedAsset(filename string) bool {
	base := filepath.Base(filename)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)

	if strings.Contains(name, "-") {
		parts := strings.Split(name, "-")
		hash := parts[len(parts)-1]
		return len(hash) >= 6 && len(hash) <= 12
	}
	return false
}
