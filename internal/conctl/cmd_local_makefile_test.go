package conctl

import (
	"os"
	"strings"
	"testing"
)

func TestMakefileExportsSQLitePoolDefaultsForLocalRunCommands(t *testing.T) {
	body, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	text := string(body)

	required := []string{
		"DB_MAX_OPEN_CONNS ?= $(shell ./scripts/dev-env-value.sh DB_MAX_OPEN_CONNS 4",
		"DB_MAX_IDLE_CONNS ?= $(shell ./scripts/dev-env-value.sh DB_MAX_IDLE_CONNS $(DB_MAX_OPEN_CONNS)",
		"export WORKTREE_PROFILE PORT FRONTEND_PORT BACKEND_URL FRONTEND_URL DB_PATH BIND_ADDR CONCTL_URL DB_MAX_OPEN_CONNS DB_MAX_IDLE_CONNS",
	}
	for _, want := range required {
		if !strings.Contains(text, want) {
			t.Fatalf("Makefile local-run defaults missing %q", want)
		}
	}
}

func TestMakefileRestartRunsLifecycleStepsSequentially(t *testing.T) {
	body, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	text := string(body)

	if strings.Contains(text, "restart: backend-stop frontend-stop") {
		t.Fatal("restart must not run stop targets as prerequisites; that makes stop/start ordering fragile")
	}

	required := []string{
		"restart:",
		"@$(MAKE) backend-stop",
		"@$(MAKE) frontend-stop",
		"@$(MAKE) backend-bg",
		"@$(MAKE) frontend-bg",
	}
	for _, want := range required {
		if !strings.Contains(text, want) {
			t.Fatalf("Makefile restart target missing sequential step %q", want)
		}
	}
}

func TestMakefileBackgroundStartsVerifyHTTPReadiness(t *testing.T) {
	body, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	text := string(body)

	required := []string{
		`scripts/wait-for-url.sh "$(BACKEND_URL)/health" "backend"`,
		`scripts/wait-for-url.sh "$(FRONTEND_URL)" "frontend"`,
		`scripts/stop-process-tree.sh "$(SERVER_PID_FILE)" "backend" "$(PORT)"`,
		`scripts/stop-process-tree.sh "$(FRONTEND_PID_FILE)" "frontend" "$(FRONTEND_PORT)"`,
	}
	for _, want := range required {
		if !strings.Contains(text, want) {
			t.Fatalf("Makefile background start missing readiness behavior %q", want)
		}
	}
}

func TestMakefileBackgroundStartsUseDetachedLauncher(t *testing.T) {
	body, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	text := string(body)

	required := []string{
		`scripts/start-background-service.sh "$(SERVER_PID_FILE)" "$(SERVER_LOG_FILE)"`,
		`scripts/start-background-service.sh "$(FRONTEND_PID_FILE)" "$(FRONTEND_LOG_FILE)"`,
	}
	for _, want := range required {
		if !strings.Contains(text, want) {
			t.Fatalf("Makefile background start missing detached launcher %q", want)
		}
	}
}

func TestViteConfigRequiresConfiguredPort(t *testing.T) {
	body, err := os.ReadFile("../../frontend/vite.config.ts")
	if err != nil {
		t.Fatalf("read vite config: %v", err)
	}
	text := string(body)

	if !strings.Contains(text, "strictPort: true") {
		t.Fatal("Vite must use strictPort so frontend lifecycle commands do not report the configured port while Vite serves another")
	}
}

func TestWorktreeSetupWritesSQLitePoolDefaults(t *testing.T) {
	body, err := os.ReadFile("../../scripts/setup-worktree-env.sh")
	if err != nil {
		t.Fatalf("read setup-worktree-env.sh: %v", err)
	}
	text := string(body)

	required := []string{
		"DB_MAX_OPEN_CONNS=4",
		"DB_MAX_IDLE_CONNS=4",
	}
	for _, want := range required {
		if !strings.Contains(text, want) {
			t.Fatalf("worktree setup defaults missing %q", want)
		}
	}
}
