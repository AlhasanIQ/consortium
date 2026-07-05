package appenv

import "testing"

func TestAllowedBrowserOriginsIncludesEnvURLsAndLoopbackVariants(t *testing.T) {
	t.Setenv("FRONTEND_URL", "http://localhost:3300")
	t.Setenv("BACKEND_URL", "http://127.0.0.1:8181")

	origins := AllowedBrowserOrigins()

	for _, origin := range []string{
		"http://localhost:3300",
		"http://127.0.0.1:3300",
		"http://127.0.0.1:8181",
		"http://localhost:8181",
	} {
		if !origins[origin] {
			t.Fatalf("expected origin %q to be allowed", origin)
		}
	}
}
