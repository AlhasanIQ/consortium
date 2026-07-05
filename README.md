# Consortium

Consortium is a self-hosted workflow runner for coordinating multiple LLM calls through durable DAGs. It includes a Go backend, SQLite persistence, an OpenAI-compatible `/v1` API, a React workflow builder, an admin/operator UI, and benchmark tooling.

Status: v0.1. The core workflow runtime is usable, but this is still early software. Read the security notes before exposing it outside a trusted environment.

## What Works

- Visual workflow builder and preset workflows.
- Durable job execution with retries, replay data, event history, and WebSocket progress.
- OpenRouter-backed model calls with cost/token accounting.
- OpenAI-compatible Chat Completions and Responses endpoints under `/v1`.
- Local admin/operator UI for jobs, workflows, benchmarks, optimization, and API keys.
- Optional Novomo agent-runtime nodes (`agent_run`, `novo_run` / Superagent).
- Experimental `benchloop` benchmark-tuning workflow.

## Requirements

- Go 1.25+
- Bun 1.3.7 for frontend builds (`.bun-version`)
- Make
- POSIX shell tools for local dev scripts (`sh`, `bash`, `lsof`, `curl`, `tail`, `kill`)
- OpenRouter API key for real LLM calls

Windows users should use WSL for the full local development workflow. Release binaries should run natively once published for the target platform.

## Quick Start

```bash
git clone https://github.com/alhasaniq/consortium.git
cd consortium

cp .env.example .env
# Edit .env and set OPENROUTER_API_KEY=<your key>

make frontend-install
make dev
```

Open:

- Ensemble UI: http://localhost:3000
- Workflow Builder: http://localhost:3000/builder
- Admin UI: http://localhost:8080/admin

Verify:

```bash
curl http://localhost:8080/health
make test
```

Dev mode uses two ports: Vite serves the live frontend on `FRONTEND_PORT` (default `3000`) and the Go backend serves APIs on `PORT` (default `8080`). Production release builds use a single binary with embedded frontend assets.

## Production Build

```bash
make build-release
EMBED_FRONTEND=true OPENROUTER_API_KEY=<OPENROUTER_API_KEY> ./bin/consortium-release
```

For non-loopback binds, set `ADMIN_API_TOKEN` and put the server behind TLS:

```bash
EMBED_FRONTEND=true \
OPENROUTER_API_KEY=<OPENROUTER_API_KEY> \
ADMIN_API_TOKEN=<long random token> \
BIND_ADDR=0.0.0.0:8080 \
./bin/consortium-release
```

Do not publish a raw local working directory. Use Git archives or release artifacts so ignored local files such as `.env`, SQLite databases, benchmark outputs, and generated scratch files cannot leak.

See [docs/deployment.md](docs/deployment.md).

Container build:

```bash
OPENROUTER_API_KEY=<OPENROUTER_API_KEY> \
ADMIN_API_TOKEN=<long random token> \
docker compose up --build
```

## Security Notes

For v0.1, treat Consortium as an operator-facing service unless you have reviewed and configured its auth boundary.

- `/v1/*` requires Consortium API keys.
- `/api/admin/*` can be protected with `ADMIN_API_TOKEN`.
- Builder/job endpoints under `/api/workflows` and `/api/jobs` are intended for the local UI/operator surface in v0.1. Do not expose them directly to untrusted networks.
- Job records may contain prompts, model outputs, workflow config, costs, and provider metadata.
- SQLite database files and logs should be stored in a private directory with restricted permissions.
- If using a reverse proxy, preserve auth headers and configure TLS there.

See [SECURITY.md](SECURITY.md) and [docs/deployment.md](docs/deployment.md#v01-security-limits).

## Optional Novomo Runtime

Novomo-backed nodes are included because Novomo is intended to be a public companion runtime. Consortium only documents the integration surface; detailed Novomo setup belongs in the Novomo project:

- Novomo repo: https://github.com/alhasaniq/novomo
- Novomo site: https://novomo.alhasaniq.com

If `NOVOMO_URL` is not configured/reachable, workflows using `agent_run` or `novo_run` will fail at those nodes. Normal LLM workflows do not require Novomo.

## Experimental Benchloop

`benchloop` is public but experimental. It automates benchmark tuning and can launch Claude CLI sessions with broad local permissions. Review [docs/benchmarks.md](docs/benchmarks.md#experimental-benchloop) before running it.

## Common Commands

```bash
make test             # Go tests
make ci               # Backend + frontend checks
make build            # Go server binary
make build-release    # Single binary with embedded frontend
make conctl-build     # Operator CLI
make benchloop-build  # Experimental benchmark tuning CLI
```

Frontend:

```bash
make frontend-install
make typecheck
make lint-frontend
make ci-frontend
```

Admin CLI examples:

```bash
./bin/conctl local backend-status
./bin/conctl local db-query --sql "SELECT id, status, created_at FROM jobs LIMIT 5"
./bin/conctl api key-create --name local-test --yes
```

## Repository Layout

```text
cmd/server/       Go server entrypoint
cmd/conctl/       Operator CLI
cmd/benchloop/    Experimental benchmark tuning CLI
frontend/         React/Vite frontend
internal/         Internal Go packages
pkg/api/          REST, WebSocket, and OpenAI-compatible API
pkg/admin/        Admin/operator API
pkg/jobs/         Job manager and durable execution coordination
pkg/workflow/     Workflow types, validation, runners, compiler, runtime
pkg/storage/      SQLite schema and stores
pkg/providers/    OpenRouter provider
pkg/bench/        Benchmark loading and evaluation helpers
docs/             Public documentation
scripts/          Local development and release helper scripts
```

## Documentation

- [Deployment](docs/deployment.md)
- [Environment variables](docs/environment-variables.md)
- [Public API](docs/public-api.md)
- [Workflow system](docs/workflow-system.md)
- [Benchmarks](docs/benchmarks.md)
- [Reasoning architecture](docs/reasoning-architecture.md)
- [Optimization](docs/optimization.md)

## License

Consortium is licensed under GPL-3.0-only. See [LICENSE](LICENSE). Third-party notices are in [NOTICE](NOTICE).
