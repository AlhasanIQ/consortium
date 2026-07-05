package http

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
)

func TestClientSendsAdminTokenOnlyToAdminAPI(t *testing.T) {
	t.Setenv("CONCTL_ADMIN_API_TOKEN", "admin-secret")
	seen := map[string]string{}
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		seen[r.URL.Path] = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "5s", false)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	var dst map[string]string
	if err := client.GetJSON(context.Background(), "/health", nil, &dst); err != nil {
		t.Fatalf("GetJSON health: %v", err)
	}
	if err := client.GetJSON(context.Background(), "/api/admin/overview", nil, &dst); err != nil {
		t.Fatalf("GetJSON admin: %v", err)
	}

	if seen["/health"] != "" {
		t.Fatalf("health Authorization = %q, want empty", seen["/health"])
	}
	if seen["/api/admin/overview"] != "Bearer admin-secret" {
		t.Fatalf("admin Authorization = %q, want bearer token", seen["/api/admin/overview"])
	}
}
