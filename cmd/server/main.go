package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/alhasaniq/consortium/internal/appenv"
	"github.com/alhasaniq/consortium/pkg/admin"
	"github.com/alhasaniq/consortium/pkg/api"
	"github.com/alhasaniq/consortium/pkg/jobs"
	"github.com/alhasaniq/consortium/pkg/middleware"
	"github.com/alhasaniq/consortium/pkg/providers"
	"github.com/alhasaniq/consortium/pkg/static"
	"github.com/alhasaniq/consortium/pkg/storage"
	"github.com/gorilla/mux"
)

func main() {
	appenv.LoadLocalEnv()

	// Initialize every provider explicitly configured by the operator.
	registry := providers.NewRegistry()
	if err := registerConfiguredProviders(context.Background(), registry); err != nil {
		log.Fatal(err)
	}

	// Initialize storage
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./consortium.db"
	}
	store, err := storage.NewStorage(dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			log.Printf("Error closing storage: %v", err)
		}
	}()
	log.Printf("✅ Initialized SQLite storage: %s", dbPath)

	// Reconcile running workflows from previous server session.
	legacyFailed, durableRunning, err := store.ReconcileRunningJobs()
	if err != nil {
		log.Printf("Warning: job reconciliation: %v", err)
	}
	if legacyFailed > 0 {
		log.Printf("Reconciled %d legacy running job(s) as failed (cannot resume)", legacyFailed)
	}
	if durableRunning > 0 {
		log.Printf("Found %d durable running job(s) — will be resumed by background workers", durableRunning)
	}

	// Create router
	r := mux.NewRouter()

	// Apply middleware in order: Recovery -> RequestID -> Logger -> CORS
	r.Use(middleware.Recovery)
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.CORS)
	adminToken := strings.TrimSpace(os.Getenv("ADMIN_API_TOKEN"))
	if adminToken != "" {
		r.Use(adminTokenMiddleware(adminToken))
		log.Println("✅ Admin API token authentication enabled")
	}

	// Create a single shared job manager for the entire process.
	// All execution surfaces (API, admin, WebSocket) use this instance
	// so admission gating and pool limits are consistent.
	manager := jobs.NewManagerWithConfig(store, registry, jobs.DefaultManagerConfig())
	manager.StartWorkers()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		manager.StopWorkers(ctx)
	}()
	log.Printf("✅ Job manager initialized (max concurrent workflows: %d, workers initial/max: %d/%d)",
		manager.Config().MaxConcurrentWorkflows, manager.Config().WorkerInitialCount, manager.Config().WorkerCount)

	// Register admin panel routes
	workdir, _ := os.Getwd()
	adminServer := admin.NewServer(store, store.DB(), registry, manager, workdir)
	adminServer.RegisterRoutes(r)

	// Register workflow API routes
	workflowAPI := api.NewWorkflowAPI(store, registry, manager)
	workflowAPI.RegisterRoutes(r)
	openAIReconcilerCtx, stopOpenAIReconciler := context.WithCancel(context.Background())
	defer stopOpenAIReconciler()
	workflowAPI.StartOpenAIBackgroundReconciler(openAIReconcilerCtx, time.Minute, 100)

	// Health check endpoint (liveness — always returns 200 OK)
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("OK")); err != nil {
			log.Printf("Error writing health check response: %v", err)
		}
	}).Methods("GET")

	// Readiness / system capacity endpoint
	r.HandleFunc("/system/readiness", func(w http.ResponseWriter, r *http.Request) {
		active, capacity := manager.PoolStats()
		utilization := float64(0)
		if capacity > 0 {
			utilization = float64(active) / float64(capacity)
		}
		admissionState := "accepting"
		admissionPaused, pauseReason := manager.AdmissionState()
		if admissionPaused {
			admissionState = "paused"
		} else if active >= capacity {
			admissionState = "full"
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"status":                 "ready",
			"active_workflows":       active,
			"pool_capacity":          capacity,
			"pool_utilization":       utilization,
			"admission_state":        admissionState,
			"admission_pause_reason": pauseReason,
		}); err != nil {
			log.Printf("Error writing readiness response: %v", err)
		}
	}).Methods("GET")

	// Serve frontend static files
	// In production: EMBED_FRONTEND=true serves embedded assets from binary
	// In development: serves from ./frontend/dist or ./static if available
	embedFrontend := os.Getenv("EMBED_FRONTEND") == "true"
	devStaticPath := os.Getenv("DEV_STATIC_PATH")
	if devStaticPath == "" {
		// Check for Docker-style ./static first, then ./frontend/dist
		if _, err := os.Stat("./static"); err == nil {
			devStaticPath = "./static"
		} else {
			devStaticPath = "./frontend/dist"
		}
	}

	if embedFrontend && static.HasEmbeddedAssets() {
		log.Println("✅ Serving embedded frontend assets")
		staticHandler := static.Handler(static.Config{DevMode: false})
		compressedHandler := middleware.Compression(middleware.CompressionConfig{
			MinSize: 1024,
			Level:   5,
		})(staticHandler)
		r.PathPrefix("/").Handler(compressedHandler)
	} else if _, err := os.Stat(devStaticPath); err == nil {
		log.Printf("✅ Serving frontend from filesystem: %s", devStaticPath)
		r.PathPrefix("/").Handler(static.Handler(static.Config{
			DevMode: true,
			DevPath: devStaticPath,
		}))
	} else {
		log.Println("ℹ️  No frontend assets found - frontend must be served separately (dev mode)")
	}

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Allow configurable bind address (default: loopback; admin endpoints are
	// intended for trusted local/operator access unless fronted by auth/proxy).
	bindAddr := os.Getenv("BIND_ADDR")
	if bindAddr == "" {
		bindAddr = "127.0.0.1:" + port
	}
	if err := validateAdminExposure(bindAddr, adminToken, os.Getenv("ALLOW_UNAUTH_ADMIN")); err != nil {
		log.Fatal(err)
	}

	log.Printf("🚀 Server starting on %s", bindAddr)
	log.Printf("Available models: %d", len(registry.GetModels()))

	server := &http.Server{
		Addr:        bindAddr,
		Handler:     middleware.TrimTrailingSlash(r),
		ReadTimeout: 15 * time.Second,
		// /api/workflows/execute is synchronous submit+wait and can exceed 60s
		// under benchmark parent/child workloads.
		WriteTimeout: 15 * time.Minute,
		IdleTimeout:  120 * time.Second,
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Println("Shutting down gracefully...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("Graceful shutdown failed: %v", err)
		}
	}()

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func validateAdminExposure(bindAddr, adminToken, allowUnauthAdmin string) error {
	// TODO(v0.1-security): Loopback-only is acceptable for local development,
	// but tunnels and reverse proxies can change the real exposure boundary.
	// Require ADMIN_API_TOKEN in packaged deployment examples and revisit this
	// exception before hosted or multi-user deployments.
	if isLoopbackBindAddress(bindAddr) || strings.TrimSpace(adminToken) != "" || truthyEnv(allowUnauthAdmin) {
		return nil
	}
	return fmt.Errorf("refusing to bind %s with unauthenticated /api/admin/*; set ADMIN_API_TOKEN or ALLOW_UNAUTH_ADMIN=true explicitly", bindAddr)
}

func isLoopbackBindAddress(bindAddr string) bool {
	host := strings.TrimSpace(bindAddr)
	if parsedHost, _, err := net.SplitHostPort(bindAddr); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func truthyEnv(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func adminTokenMiddleware(token string) func(http.Handler) http.Handler {
	token = strings.TrimSpace(token)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isAdminAPIPath(r.URL.Path) || token == "" {
				next.ServeHTTP(w, r)
				return
			}
			if subtle.ConstantTimeCompare([]byte(adminTokenFromRequest(r)), []byte(token)) == 1 {
				next.ServeHTTP(w, r)
				return
			}
			http.Error(w, "admin authentication required", http.StatusUnauthorized)
		})
	}
}

func isAdminAPIPath(path string) bool {
	return path == "/api/admin" || strings.HasPrefix(path, "/api/admin/")
}

func adminTokenFromRequest(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if token, ok := strings.CutPrefix(auth, "Bearer "); ok {
		return strings.TrimSpace(token)
	}
	return strings.TrimSpace(r.Header.Get("X-Admin-Token"))
}
