# Deployment

This guide covers v0.1 deployments of Consortium.

## Build

```bash
make build-release
```

`make build-release`:

1. Installs frontend dependencies with Bun.
2. Builds the Vite frontend.
3. Pre-compresses frontend assets.
4. Copies assets into `pkg/static/dist`.
5. Builds the Go server with the `release` build tag.
6. Starts the binary and verifies liveness, the embedded frontend and one hashed asset, cache headers, and the unauthenticated `/v1` boundary.

The output binary is `bin/consortium-release`.

## Run Locally

Using OpenRouter:

```bash
EMBED_FRONTEND=true \
OPENROUTER_API_KEY=<OPENROUTER_API_KEY> \
./bin/consortium-release
```

Using a local OpenAI-compatible endpoint instead:

```bash
EMBED_FRONTEND=true \
OPENAI_COMPATIBLE_BASE_URL=http://127.0.0.1:11434/v1 \
./bin/consortium-release
```

At least one provider must be configured. See [openai-compatible-provider.md](openai-compatible-provider.md) for Ollama, LM Studio, vLLM, model discovery, and fallback-model configuration.

Default bind is `127.0.0.1:8080`.

## Run Behind a Reverse Proxy

```bash
EMBED_FRONTEND=true \
OPENROUTER_API_KEY=<OPENROUTER_API_KEY> \
ADMIN_API_TOKEN=<long random token> \
BIND_ADDR=127.0.0.1:8080 \
DB_PATH=/var/lib/consortium/consortium.db \
./bin/consortium-release
```

Put TLS and public host routing in Caddy, nginx, or another reverse proxy. Preserve:

- `Authorization`
- `Content-Type`
- `Idempotency-Key`
- `X-Admin-Token` if you use it

## v0.1 Security Limits

Consortium v0.1 is not a hardened multi-tenant SaaS boundary.

- `/v1/*` is the intended external API and requires Consortium API keys.
- `/api/admin/*` is an operator API. Set `ADMIN_API_TOKEN` before exposing it.
- `/api/workflows`, `/api/jobs`, and the admin UI are local/operator surfaces in v0.1. They can expose prompts, outputs, workflow configs, traces, metadata, and cost data. Do not expose them to untrusted networks.
- Job records may contain provider endpoint metadata, prompts, model outputs, and usage data. Treat them as sensitive.
- The process-local pre-auth limiter uses request IP identity and needs proxy review before internet exposure.
- CORS defaults are convenient for local development. Set explicit production origins at the proxy or application boundary.
- SQLite database files, WAL/SHM files, and logs can contain sensitive operational data. Store them in private directories and back them up according to your retention policy.
- Provider metadata may be persisted for debugging. Treat job history as sensitive.

These limitations are tracked as code TODOs and should be revisited before a stronger production release.

## Scheduler and Restart Model

Consortium v0.1 supports exactly one active server process for each SQLite database. Do not point multiple replicas at the same database: worker ownership is coordinated inside one process and does not yet use a distributed lease or fencing token.

Durable jobs left running by an abrupt process failure can be recovered from persisted history when that single process restarts. A graceful server shutdown currently cancels active work before exit; it is not equivalent to crash recovery. Plan deployments and maintenance windows around that distinction.

## Public API

The OpenAI-compatible API lives under `/v1/*`. Create API keys through the admin UI or:

```bash
./bin/conctl api key-create --name local-client --yes
```

Use:

```bash
curl -s http://127.0.0.1:8080/v1/models \
  -H "Authorization: Bearer <CONSORTIUM_API_KEY>"
```

See [public-api.md](public-api.md).

## Optional Novomo Runtime

Novomo-backed workflow nodes are optional:

- `agent_run`
- `novo_run` / Superagent

Consortium links to Novomo but does not duplicate Novomo setup docs.

- Novomo repo: https://github.com/alhasaniq/novomo
- Novomo site: https://novomo.alhasaniq.com

Set `NOVOMO_URL` and `NOVOMO_API_KEY` only when you run workflows that use Novomo nodes.

## Development Mode

```bash
make dev
```

Development uses two ports:

- `FRONTEND_PORT` default `3000`: Vite dev server with hot reload.
- `PORT` default `8080`: Go backend APIs and WebSocket streams.

Use `http://localhost:3000` for frontend development. The backend port may serve stale filesystem frontend assets unless you build them.

## Release Artifacts

GitHub release builds produce:

- Linux amd64/arm64 tarballs
- macOS amd64/arm64 tarballs
- Windows amd64 zip
- SHA256 checksum files

Before packaging, CI runs `scripts/audit-release.sh` to prevent known internal docs, local paths, datasets, DBs, and generated artifacts from being tracked.

## Containers

Build and run the local container with either provider path. OpenRouter example:

```bash
OPENROUTER_API_KEY=<OPENROUTER_API_KEY> \
ADMIN_API_TOKEN=<long random token> \
docker compose up --build
```

For an OpenAI-compatible endpoint, set `OPENAI_COMPATIBLE_BASE_URL` and optionally `OPENAI_COMPATIBLE_API_KEY` / `OPENAI_COMPATIBLE_MODELS`. The configured base URL must be reachable **from inside the container**, not only from the host loopback namespace.

The container:

- runs the release binary with embedded frontend assets
- listens on `0.0.0.0:8080` inside the container
- publishes `127.0.0.1:8080` on the host by default
- runs as a non-root user
- stores SQLite data in the `consortium-data` volume at `/data/consortium.db`
- checks `/health`

For internet-facing deployments, keep the app behind a TLS reverse proxy and expose only the routes you intend to support.
