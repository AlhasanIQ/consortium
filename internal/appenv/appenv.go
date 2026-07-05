package appenv

import (
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

// LoadLocalEnv loads .env, then applies .env.local on top of it without
// overriding variables that were already present in the parent shell.
func LoadLocalEnv() {
	original := existingKeys()
	_ = godotenv.Load(".env")
	applyEnvFile(".env.local", original)
}

func existingKeys() map[string]struct{} {
	keys := make(map[string]struct{})
	for _, item := range os.Environ() {
		if idx := strings.IndexByte(item, '='); idx > 0 {
			keys[item[:idx]] = struct{}{}
		}
	}
	return keys
}

func applyEnvFile(path string, preserve map[string]struct{}) {
	values, err := godotenv.Read(path)
	if err != nil {
		return
	}
	for key, value := range values {
		if _, ok := preserve[key]; ok {
			continue
		}
		_ = os.Setenv(key, value)
	}
}

func envOrDefault(key, def string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return def
}

func normalizeBaseURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

func BackendPort() string {
	return envOrDefault("PORT", "8080")
}

func FrontendPort() string {
	return envOrDefault("FRONTEND_PORT", "3000")
}

func BackendURL() string {
	if value := normalizeBaseURL(os.Getenv("BACKEND_URL")); value != "" {
		return value
	}
	return "http://localhost:" + BackendPort()
}

func FrontendURL() string {
	if value := normalizeBaseURL(os.Getenv("FRONTEND_URL")); value != "" {
		return value
	}
	return "http://localhost:" + FrontendPort()
}

func NovomoFrontendV2URL() string {
	if value := normalizeBaseURL(os.Getenv("NOVOMO_FRONTEND_V2_URL")); value != "" {
		return value
	}
	return "http://127.0.0.1:5174"
}

func AllowedBrowserOrigins() map[string]bool {
	origins := map[string]bool{}
	addOriginSet(origins, FrontendURL())
	addOriginSet(origins, BackendURL())
	for _, raw := range []string{
		"http://localhost:3000",
		"http://localhost:3001",
		"http://localhost:5173",
		"http://localhost:5174",
	} {
		addOriginSet(origins, raw)
	}
	return origins
}

func OriginAllowed(origin string) bool {
	return AllowedBrowserOrigins()[strings.TrimSpace(origin)]
}

func addOriginSet(origins map[string]bool, raw string) {
	base := normalizeBaseURL(raw)
	if base == "" {
		return
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return
	}
	origins[parsed.Scheme+"://"+parsed.Host] = true

	host, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		return
	}
	switch host {
	case "localhost":
		origins[parsed.Scheme+"://127.0.0.1:"+port] = true
	case "127.0.0.1":
		origins[parsed.Scheme+"://localhost:"+port] = true
	}
}
