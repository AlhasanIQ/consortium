WORKTREE_DIR ?= $(shell cd "$(dir $(abspath $(lastword $(MAKEFILE_LIST))))" && git worktree list --porcelain | head -1 | sed 's/^worktree //')/../consortium-worktrees

.PHONY: help build test clean dev backend backend-bg backend-stop backend-restart backend-status backend-logs restart frontend frontend-install frontend-bg frontend-stop frontend-restart frontend-status frontend-logs worktree-add worktree-setup install-hooks lint-frontend fix-frontend typecheck fetch-openrouter-models loadtest loadtest-heavy bench-data ci ci-backend ci-frontend fmt lint tidy install-tools db-query db-reset build-frontend frontend-precompress build-release release-audit conctl-build conctl conctl-completion benchloop-build benchloop

BINDIR ?= bin
SERVER_BIN := $(BINDIR)/consortium
RELEASE_BIN := $(BINDIR)/consortium-release
CONCTL_BIN := $(BINDIR)/conctl
BENCHLOOP_BIN := $(BINDIR)/benchloop
BUN ?= $(shell command -v bun 2>/dev/null || echo "$(HOME)/.bun/bin/bun")
PYTHON ?= python3
WORKTREE_PROFILE ?= $(shell ./scripts/dev-env-value.sh WORKTREE_PROFILE default 2>/dev/null)
PORT ?= $(shell ./scripts/dev-env-value.sh PORT 8080 2>/dev/null)
FRONTEND_PORT ?= $(shell ./scripts/dev-env-value.sh FRONTEND_PORT 3000 2>/dev/null)
BACKEND_URL ?= $(shell ./scripts/dev-env-value.sh BACKEND_URL http://localhost:$(PORT) 2>/dev/null)
FRONTEND_URL ?= $(shell ./scripts/dev-env-value.sh FRONTEND_URL http://localhost:$(FRONTEND_PORT) 2>/dev/null)
DB_PATH ?= $(shell ./scripts/dev-env-value.sh DB_PATH ./consortium.db 2>/dev/null)
DB_MAX_OPEN_CONNS ?= $(shell ./scripts/dev-env-value.sh DB_MAX_OPEN_CONNS 4 2>/dev/null)
DB_MAX_IDLE_CONNS ?= $(shell ./scripts/dev-env-value.sh DB_MAX_IDLE_CONNS $(DB_MAX_OPEN_CONNS) 2>/dev/null)
BIND_ADDR ?= $(shell ./scripts/dev-env-value.sh BIND_ADDR 127.0.0.1:$(PORT) 2>/dev/null)
CONCTL_URL ?= $(shell ./scripts/dev-env-value.sh CONCTL_URL $(BACKEND_URL) 2>/dev/null)
BACKEND_READY_ATTEMPTS ?= 240
FRONTEND_READY_ATTEMPTS ?= 80
READY_SLEEP_SECONDS ?= 0.25
VERIFY_PORT ?= $(shell echo $$(( $(PORT) + 1000 )))
VERIFY_URL ?= http://localhost:$(VERIFY_PORT)
SERVER_PID_FILE ?= .server.pid
FRONTEND_PID_FILE ?= .frontend.pid
SERVER_LOG_FILE ?= logs/server.log
FRONTEND_LOG_FILE ?= logs/frontend.log
DB_DIR := $(dir $(DB_PATH))

export WORKTREE_PROFILE PORT FRONTEND_PORT BACKEND_URL FRONTEND_URL DB_PATH BIND_ADDR CONCTL_URL DB_MAX_OPEN_CONNS DB_MAX_IDLE_CONNS

## help: Show this help message
help:
	@echo "Available targets:"
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' | sed -e 's/^/ /'

## worktree-add: Create a new worktree (NAME=agent-a BRANCH=wt/agent-a/task)
worktree-add:
	@if [ -z "$(NAME)" ]; then echo "Usage: make worktree-add NAME=<name> [BRANCH=wt/<name>/<task>]"; exit 1; fi
	@BRANCH="$(or $(BRANCH),wt/$(NAME))"; \
	mkdir -p "$(WORKTREE_DIR)"; \
	git worktree add "$(WORKTREE_DIR)/$(NAME)" -b "$$BRANCH"; \
	cd "$(WORKTREE_DIR)/$(NAME)" && $(MAKE) worktree-setup PROFILE="$(NAME)"; \
	echo ""; \
	echo "Worktree ready: $(WORKTREE_DIR)/$(NAME)"; \
	echo "  cd $(WORKTREE_DIR)/$(NAME)"

## worktree-setup: Create or refresh .env.local for this worktree (PROFILE=name FORCE=1 to overwrite)
worktree-setup:
	@FORCE="$(FORCE)" ./scripts/setup-worktree-env.sh "$(PROFILE)"

## dev: Start both backend and frontend in background
dev:
	@$(MAKE) backend-bg
	@$(MAKE) frontend-bg

## backend: Run Go server in foreground (use FORCE=1 to skip safety checks)
backend:
	@if [ "$(FORCE)" != "1" ]; then scripts/check-safe-to-stop.sh || exit 1; fi
	@if [ -f $(SERVER_PID_FILE) ]; then \
		scripts/stop-process-tree.sh "$(SERVER_PID_FILE)" "backend" "$(PORT)"; \
	fi
	@if lsof -nP -iTCP:$(PORT) -sTCP:LISTEN -t >/dev/null 2>&1; then \
		echo "Backend port $(PORT) is already in use. Adjust PORT/BIND_ADDR or stop the owning process."; \
		exit 1; \
	fi
	@mkdir -p logs "$(DB_DIR)"
	@echo "Starting Go server locally for profile $(WORKTREE_PROFILE) on $(BACKEND_URL)..."
	@go run ./cmd/server

## backend-bg: Run Go server in background with PID tracking and logging
backend-bg:
	@mkdir -p logs "$(DB_DIR)"
	@if [ -f $(SERVER_PID_FILE) ] && kill -0 $$(cat $(SERVER_PID_FILE)) 2>/dev/null; then \
		echo "Backend already running (PID: $$(cat $(SERVER_PID_FILE))). Use 'conctl local backend-restart --yes' or 'make backend-restart' to restart."; \
		exit 1; \
	fi
	@if lsof -nP -iTCP:$(PORT) -sTCP:LISTEN -t >/dev/null 2>&1; then \
		echo "Backend port $(PORT) is already in use. Adjust PORT/BIND_ADDR or stop the owning process."; \
		exit 1; \
	fi
	@echo "Starting backend server in background for profile $(WORKTREE_PROFILE)..."
	@scripts/start-background-service.sh "$(SERVER_PID_FILE)" "$(SERVER_LOG_FILE)" env PATH="$(PATH):/usr/local/go/bin:$(HOME)/go/bin" go run ./cmd/server
	@sleep 1
	@if ! kill -0 $$(cat $(SERVER_PID_FILE)) 2>/dev/null; then \
		echo "❌ Backend failed to start. Check $(SERVER_LOG_FILE)"; \
		rm -f $(SERVER_PID_FILE); \
		exit 1; \
	fi
	@if ! scripts/wait-for-url.sh "$(BACKEND_URL)/health" "backend" "$(BACKEND_READY_ATTEMPTS)" "$(READY_SLEEP_SECONDS)"; then \
		scripts/stop-process-tree.sh "$(SERVER_PID_FILE)" "backend" "$(PORT)" >/dev/null 2>&1 || true; \
		echo "❌ Backend failed readiness check. Check $(SERVER_LOG_FILE)"; \
		exit 1; \
	fi
	@echo "✅ Backend started (PID: $$(cat $(SERVER_PID_FILE)))"
	@echo "   Profile: $(WORKTREE_PROFILE)"
	@echo "   Logs: $(SERVER_LOG_FILE)"
	@echo "   Admin: $(BACKEND_URL)/admin"
	@echo "   Use 'conctl local backend-status' to check health"
	@echo "   Use 'conctl local backend-stop --yes' to stop"

## backend-stop: Stop the background backend server (use FORCE=1 to skip safety checks)
backend-stop:
	@if [ "$(FORCE)" != "1" ]; then scripts/check-safe-to-stop.sh || exit 1; fi
	@scripts/stop-process-tree.sh "$(SERVER_PID_FILE)" "backend" "$(PORT)"

## backend-restart: Restart the background backend server
backend-restart: backend-stop
	@sleep 1
	@$(MAKE) backend-bg

## restart: Restart both backend and frontend servers
restart:
	@$(MAKE) backend-stop
	@$(MAKE) frontend-stop
	@sleep 1
	@$(MAKE) backend-bg
	@$(MAKE) frontend-bg

## backend-status: Check backend server status and health
backend-status:
	@echo "=== Backend Status ==="
	@if [ -f $(SERVER_PID_FILE) ]; then \
		PID=$$(cat $(SERVER_PID_FILE)); \
		if kill -0 $$PID 2>/dev/null; then \
			echo "✅ Backend running (PID: $$PID)"; \
			echo "Profile: $(WORKTREE_PROFILE)"; \
			echo "URL: $(BACKEND_URL)"; \
			echo ""; \
			echo "=== Health Check ==="; \
			curl -s $(BACKEND_URL)/health && echo "" || echo "❌ Health check failed"; \
			echo ""; \
			echo "=== Recent Logs (last 10 lines) ==="; \
			tail -10 $(SERVER_LOG_FILE) 2>/dev/null || echo "No logs found"; \
		else \
			echo "❌ Backend not running (stale PID: $$PID)"; \
			rm -f $(SERVER_PID_FILE); \
		fi; \
	else \
		if lsof -nP -iTCP:$(PORT) -sTCP:LISTEN -t >/dev/null 2>&1; then \
			echo "⚠️  Backend running on port $(PORT) but no PID file (started manually?)"; \
			echo "   PID: $$(lsof -nP -iTCP:$(PORT) -sTCP:LISTEN -t)"; \
		else \
			echo "❌ Backend not running"; \
		fi; \
	fi

## backend-logs: View backend logs
backend-logs:
	@if [ -f $(SERVER_LOG_FILE) ]; then \
		cat $(SERVER_LOG_FILE); \
	else \
		echo "No logs found. Start backend with 'make backend-bg' first."; \
	fi

## build: Build the consortium server locally
build:
	@mkdir -p $(BINDIR)
	go build -o $(SERVER_BIN) ./cmd/server

## test: Run all tests
test:
	go test ./...

## test-verbose: Run tests with verbose output
test-verbose:
	go test -v ./...

## conctl-build: Build conctl CLI binary
conctl-build:
	@mkdir -p $(BINDIR)
	go build -o $(CONCTL_BIN) ./cmd/conctl

## conctl: Run conctl CLI (pass ARGS, e.g. make conctl ARGS="jobs list")
conctl: conctl-build
	@$(CONCTL_BIN) $(ARGS)

## conctl-completion: Install bash completions for conctl
conctl-completion: conctl-build
	@$(CONCTL_BIN) completion bash > /usr/local/etc/bash_completion.d/conctl 2>/dev/null || \
		(mkdir -p ~/.local/share/bash-completion/completions && \
		 $(CONCTL_BIN) completion bash > ~/.local/share/bash-completion/completions/conctl)
	@echo "✅ Bash completions installed for conctl"
	@echo "   Restart your shell or run: source <($(CONCTL_BIN) completion bash)"

## loadtest: Run load test harness (default: 10 workers, 30s, mixed scenarios)
loadtest:
	go run ./cmd/loadtest -concurrency=10 -duration=30s -scenario=mixed

## loadtest-heavy: Run intensive load test (50 workers, 60s)
loadtest-heavy:
	go run ./cmd/loadtest -concurrency=50 -duration=60s -scenario=mixed -verbose

## benchloop-build: Build benchloop binary
benchloop-build:
	@mkdir -p $(BINDIR)
	go build -o $(BENCHLOOP_BIN) ./cmd/benchloop

## benchloop: Run benchmark tuning loop (pass ARGS, e.g. make benchloop ARGS="run --workflows benchmark-informed-captain-synthesis-cheap --run-set custom --split test --item-limit 50 --concurrency 100 --dry-run")
benchloop: benchloop-build
	@$(BENCHLOOP_BIN) $(ARGS)

## bench-data: Fetch benchmark datasets into benchmarks/data (BENCHMARK=all SPLITS=all)
bench-data:
	@$(PYTHON) scripts/fetch_benchmark_data.py --benchmark=$(or $(BENCHMARK),all) --splits=$(or $(SPLITS),all) --output-dir=benchmarks/data

## clean: Remove build artifacts (use FORCE=1 to skip safety checks)
clean:
	@if [ "$(FORCE)" != "1" ]; then scripts/check-safe-to-stop.sh || exit 1; fi
	rm -f consortium consortium-release conctl benchloop
	rm -f $(SERVER_BIN) $(RELEASE_BIN) $(CONCTL_BIN) $(BENCHLOOP_BIN)
	rm -f $(DB_PATH) $(DB_PATH)-shm $(DB_PATH)-wal

## fmt: Format Go code using golangci-lint
fmt:
	@GOLANGCI_LINT=$$(command -v golangci-lint 2>/dev/null || echo "$(HOME)/go/bin/golangci-lint"); \
	if [ -x "$$GOLANGCI_LINT" ]; then \
		$$GOLANGCI_LINT fmt ./...; \
	else \
		echo "golangci-lint not found, falling back to go fmt"; \
		go fmt ./...; \
	fi

## lint: Run golangci-lint (Go backend)
lint:
	@GOLANGCI_LINT=$$(command -v golangci-lint 2>/dev/null || echo "$(HOME)/go/bin/golangci-lint"); \
	if [ -x "$$GOLANGCI_LINT" ]; then \
		$$GOLANGCI_LINT run --timeout=5m ./...; \
	else \
		echo "❌ golangci-lint not found. Install it with: make install-tools"; \
		exit 1; \
	fi

## lint-frontend: Check frontend (Biome lint + format)
lint-frontend:
	@$(BUN) run lint

## fix-frontend: Auto-fix frontend issues (Biome lint + format)
fix-frontend:
	@$(BUN) run fix

## typecheck: Run TypeScript type checking
typecheck:
	@$(BUN) run typecheck

## tidy: Tidy Go modules
tidy:
	go mod tidy

## ci: Run all CI checks locally (mirrors GitHub Actions)
ci: ci-backend ci-frontend
	@echo "\n✅ All CI checks passed!"

## ci-backend: Run only backend CI checks
ci-backend:
	@echo "🚀 Running backend CI checks..."
	@echo "Running tests..."
	@$(MAKE) test-verbose
	@echo "\n🔍 Running golangci-lint..."
	@$(MAKE) lint
	@echo "\n🔨 Building binary..."
	@$(MAKE) build
	@echo "\n✅ Backend CI checks passed!"

## ci-frontend: Run only frontend CI checks
ci-frontend:
	@echo "🚀 Running frontend CI checks..."
	@echo "Installing dependencies..."
	@$(MAKE) frontend-install
	@echo "\n🔍 Running TypeScript type checking..."
	@$(MAKE) typecheck
	@echo "\n🔍 Running Biome checks (lint + format)..."
	@$(MAKE) lint-frontend
	@echo "\n🧪 Running tests..."
	@$(BUN) run test:run
	@echo "\n🔨 Building..."
	@$(BUN) run build
	@echo "\n✅ Frontend CI checks passed!"

## install-tools: Install development tools
install-tools:
	@echo "Installing development tools..."
	go install golang.org/x/tools/cmd/goimports@latest
	@echo "Installing golangci-lint via binary (recommended method)..."
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(shell go env GOPATH)/bin
	@echo "✅ Tools installed successfully"

## install-hooks: Install git pre-commit hook
install-hooks:
	@HOOK_PATH="$$(git rev-parse --git-path hooks/pre-commit)"; \
	cp scripts/pre-commit "$$HOOK_PATH"; \
	chmod +x "$$HOOK_PATH"
	@echo "✅ Pre-commit hook installed"

## db-query: Run a SQL query against the database (e.g., make db-query SQL="SELECT * FROM jobs LIMIT 5")
db-query:
	@./scripts/query-db.sh "$(SQL)"

## db-reset: Delete database and restart fresh (use FORCE=1 to skip safety checks)
db-reset:
	@if [ "$(FORCE)" != "1" ]; then scripts/check-safe-to-stop.sh || exit 1; fi
	rm -f $(DB_PATH) $(DB_PATH)-shm $(DB_PATH)-wal
	@echo "Database reset at $(DB_PATH). It will be recreated on next server start."

## frontend-install: Install frontend dependencies with bun
frontend-install:
	$(BUN) install

## frontend: Run frontend dev server with bun
frontend:
	@if lsof -ti:$(FRONTEND_PORT) >/dev/null 2>&1; then \
		echo "Frontend port $(FRONTEND_PORT) is already in use. Adjust FRONTEND_PORT or stop the owning process."; \
		exit 1; \
	fi
	@echo "Starting frontend dev server on $(FRONTEND_URL)..."
	$(BUN) run dev

## frontend-bg: Run frontend dev server in background with PID tracking and logging
frontend-bg:
	@mkdir -p logs
	@if [ -f $(FRONTEND_PID_FILE) ] && kill -0 $$(cat $(FRONTEND_PID_FILE)) 2>/dev/null; then \
		echo "Frontend already running (PID: $$(cat $(FRONTEND_PID_FILE))). Use 'conctl local frontend-restart --yes' or 'make frontend-restart' to restart."; \
		exit 1; \
	fi
	@if lsof -ti:$(FRONTEND_PORT) >/dev/null 2>&1; then \
		echo "Frontend port $(FRONTEND_PORT) is already in use. Adjust FRONTEND_PORT or stop the owning process."; \
		exit 1; \
	fi
	@echo "Starting frontend dev server in background for profile $(WORKTREE_PROFILE)..."
	@scripts/start-background-service.sh "$(FRONTEND_PID_FILE)" "$(FRONTEND_LOG_FILE)" $(BUN) run dev
	@sleep 1
	@if ! kill -0 $$(cat $(FRONTEND_PID_FILE)) 2>/dev/null; then \
		echo "❌ Frontend failed to start. Check $(FRONTEND_LOG_FILE)"; \
		rm -f $(FRONTEND_PID_FILE); \
		exit 1; \
	fi
	@if ! scripts/wait-for-url.sh "$(FRONTEND_URL)" "frontend" "$(FRONTEND_READY_ATTEMPTS)" "$(READY_SLEEP_SECONDS)"; then \
		scripts/stop-process-tree.sh "$(FRONTEND_PID_FILE)" "frontend" "$(FRONTEND_PORT)" >/dev/null 2>&1 || true; \
		echo "❌ Frontend failed readiness check. Check $(FRONTEND_LOG_FILE)"; \
		exit 1; \
	fi
	@echo "✅ Frontend started (PID: $$(cat $(FRONTEND_PID_FILE)))"
	@echo "   Profile: $(WORKTREE_PROFILE)"
	@echo "   Logs: $(FRONTEND_LOG_FILE)"
	@echo "   URL: $(FRONTEND_URL)"
	@echo "   Use 'conctl local frontend-status' to check health"
	@echo "   Use 'conctl local frontend-stop --yes' to stop"

## frontend-stop: Stop the background frontend server
frontend-stop:
	@scripts/stop-process-tree.sh "$(FRONTEND_PID_FILE)" "frontend" "$(FRONTEND_PORT)"

## frontend-restart: Restart the background frontend server
frontend-restart: frontend-stop
	@sleep 1
	@$(MAKE) frontend-bg

## frontend-status: Check frontend server status
frontend-status:
	@echo "=== Frontend Status ==="
	@if [ -f $(FRONTEND_PID_FILE) ]; then \
		PID=$$(cat $(FRONTEND_PID_FILE)); \
		if kill -0 $$PID 2>/dev/null; then \
			echo "✅ Frontend running (PID: $$PID)"; \
			echo "Profile: $(WORKTREE_PROFILE)"; \
			echo "URL: $(FRONTEND_URL)"; \
			echo ""; \
			echo "=== Health Check ==="; \
			curl -s -o /dev/null -w "HTTP Status: %{http_code}\n" $(FRONTEND_URL) || echo "❌ Health check failed"; \
			echo ""; \
			echo "=== Recent Logs (last 10 lines) ==="; \
			tail -10 $(FRONTEND_LOG_FILE) 2>/dev/null || echo "No logs found"; \
		else \
			echo "❌ Frontend not running (stale PID: $$PID)"; \
			rm -f $(FRONTEND_PID_FILE); \
		fi; \
	else \
		if lsof -ti:$(FRONTEND_PORT) >/dev/null 2>&1; then \
			echo "⚠️  Frontend running on port $(FRONTEND_PORT) but no PID file (started manually?)"; \
			echo "   PID: $$(lsof -ti:$(FRONTEND_PORT))"; \
		else \
			echo "❌ Frontend not running"; \
		fi; \
	fi

## frontend-logs: View frontend logs
frontend-logs:
	@if [ -f $(FRONTEND_LOG_FILE) ]; then \
		cat $(FRONTEND_LOG_FILE); \
	else \
		echo "No logs found. Start frontend with 'make frontend-bg' first."; \
	fi

## build-frontend: Build frontend for production with Bun
build-frontend:
	@echo "Building frontend..."
	@$(BUN) install --frozen-lockfile
	@NODE_ENV=production $(BUN) run build
	@echo "✅ Frontend built to frontend/dist/"

## frontend-precompress: Pre-compress frontend assets (gzip + zstd)
frontend-precompress: build-frontend
	@echo "Pre-compressing assets..."
	@./scripts/precompress.sh frontend/dist
	@echo "✅ Assets pre-compressed"

## build-release: Build single binary with embedded frontend
build-release: frontend-precompress
	@echo "Building release binary with embedded frontend..."
	@mkdir -p $(BINDIR)
	@mkdir -p pkg/static/dist
	@cp -r frontend/dist/* pkg/static/dist/
	@CGO_ENABLED=0 go build -tags release -ldflags="-s -w" -o $(RELEASE_BIN) ./cmd/server
	@echo ""
	@echo "✅ Release binary built: $(RELEASE_BIN)"
	@du -sh $(RELEASE_BIN)
	@echo ""
	@echo "Verifying binary..."
	@OPENROUTER_API_KEY="$${OPENROUTER_API_KEY:-release-build-placeholder}" PORT=$(VERIFY_PORT) BIND_ADDR=127.0.0.1:$(VERIFY_PORT) EMBED_FRONTEND=true $(RELEASE_BIN) > /dev/null 2>&1 & echo $$! > .release-verify.pid
	@sleep 3
	@echo ""
	@echo "=== Health Check ==="
	@curl -s $(VERIFY_URL)/health && echo " OK" || echo " FAILED"
	@echo ""
	@echo "=== Frontend Check ==="
	@curl -s -o /dev/null -w "Status: %{http_code}, Size: %{size_download} bytes\n" $(VERIFY_URL)/ || echo "Frontend check failed"
	@echo ""
	@echo "=== Cache Headers (index.html) ==="
	@curl -sI $(VERIFY_URL)/ | grep -i cache-control || echo "No cache headers"
	@echo ""
	@if [ -f .release-verify.pid ]; then kill $$(cat .release-verify.pid) 2>/dev/null || true; rm -f .release-verify.pid; fi
	@echo "✅ Release binary built and verified"

## release-audit: Check tracked files for known publication blockers
release-audit:
	@scripts/audit-release.sh
