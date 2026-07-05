package middleware

import (
	"net/http"
	"strings"

	"github.com/klauspost/compress/gzhttp"
)

// CompressionConfig holds compression middleware configuration
type CompressionConfig struct {
	// MinSize is the minimum response size to compress (default: 1024 bytes)
	MinSize int
	// Level is the compression level (1-9 for gzip)
	Level int
}

// Compression returns middleware that compresses responses using gzip.
// It supports pre-compressed files (.gz variants) for zero-runtime CPU cost.
// Skips compression for already-compressed content types.
func Compression(cfg CompressionConfig) func(http.Handler) http.Handler {
	if cfg.MinSize == 0 {
		cfg.MinSize = 1024 // 1KB minimum
	}
	if cfg.Level == 0 {
		cfg.Level = 5 // Balanced default
	}

	// Configure gzhttp wrapper
	wrapper, err := gzhttp.NewWrapper(
		gzhttp.MinSize(cfg.MinSize),
		gzhttp.CompressionLevel(cfg.Level),
		gzhttp.ContentTypes(compressibleTypes()),
	)
	if err != nil {
		// Fallback to identity if wrapper creation fails
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	return func(next http.Handler) http.Handler {
		return wrapper(next)
	}
}

// compressibleTypes returns content types that should be compressed
func compressibleTypes() []string {
	return []string{
		"text/html",
		"text/css",
		"text/plain",
		"text/javascript",
		"text/xml",
		"application/javascript",
		"application/json",
		"application/xml",
		"application/xhtml+xml",
		"image/svg+xml",
	}
}

// ShouldCompress checks if the content type is compressible
func ShouldCompress(contentType string) bool {
	// Skip already compressed formats
	skipPrefixes := []string{
		"image/png", "image/jpeg", "image/gif", "image/webp",
		"application/gzip", "application/zip", "application/zstd",
		"font/woff", "font/woff2",
		"video/", "audio/",
	}

	for _, prefix := range skipPrefixes {
		if strings.HasPrefix(contentType, prefix) {
			return false
		}
	}

	// Check if it's a compressible type
	for _, ct := range compressibleTypes() {
		if strings.HasPrefix(contentType, ct) {
			return true
		}
	}

	return false
}
