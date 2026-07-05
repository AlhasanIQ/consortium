package middleware

import (
	"net/http"
	"strings"
)

// TrimTrailingSlash normalizes request paths by removing trailing slashes.
// Root ("/") remains unchanged.
func TrimTrailingSlash(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL == nil {
			next.ServeHTTP(w, r)
			return
		}

		if r.URL.Path != "/" {
			r.URL.Path = strings.TrimRight(r.URL.Path, "/")
			if r.URL.Path == "" {
				r.URL.Path = "/"
			}
		}

		if r.URL.RawPath != "" && r.URL.RawPath != "/" {
			r.URL.RawPath = strings.TrimRight(r.URL.RawPath, "/")
			if r.URL.RawPath == "" {
				r.URL.RawPath = "/"
			}
		}

		next.ServeHTTP(w, r)
	})
}
