# Environment Variables

Consortium loads `.env` and then `.env.local` at startup.

## LLM Providers

At least one LLM provider must be configured.

| Variable | Default | Description |
| --- | --- | --- |
| `OPENROUTER_API_KEY` | unset | OpenRouter API key. Configure this for the built-in OpenRouter catalog and hosted model routing. |
| `OPENAI_COMPATIBLE_BASE_URL` | unset | Optional OpenAI-compatible API root containing `/models` and `/chat/completions`, for example `http://127.0.0.1:11434/v1` for Ollama. |
| `OPENAI_COMPATIBLE_API_KEY` | unset | Optional bearer token for the compatible endpoint. Local Ollama/LM Studio/vLLM setups commonly leave it empty. |
| `OPENAI_COMPATIBLE_MODELS` | unset | Optional comma-separated fallback model IDs when the compatible endpoint does not expose `/models`. |

Models discovered or configured through the compatibility provider are exposed to Consortium workflows with the `compatible/` prefix. For example, upstream model `qwen3:8b` becomes `compatible/qwen3:8b`. The prefix prevents collisions with OpenRouter model IDs and is removed before the upstream request is sent.

When `OPENAI_COMPATIBLE_BASE_URL` is configured, startup attempts `/models`. If discovery fails, `OPENAI_COMPATIBLE_MODELS` must provide at least one fallback model. Provider-reported `usage.cost` is preserved when present; generic model discovery does not invent pricing for endpoints that do not publish it.

## Server

| Variable | Default | Description |
| --- | --- | --- |
| `PORT` | `8080` | Backend port. |
| `BIND_ADDR` | `127.0.0.1:$PORT` | Full bind address. Use non-loopback only behind trusted auth/proxy. |
| `BACKEND_URL` | `http://localhost:$PORT` | Backend URL used by local tooling. |
| `FRONTEND_PORT` | `3000` | Vite dev-server port. |
| `FRONTEND_URL` | `http://localhost:$FRONTEND_PORT` | Frontend URL surfaced in links. |
| `CONCTL_URL` | `$BACKEND_URL` | Server URL used by `conctl`. |

## Auth And Operator Access

| Variable | Default | Description |
| --- | --- | --- |
| `ADMIN_API_TOKEN` | unset | Protects `/api/admin/*` with `Authorization: Bearer <token>` or `X-Admin-Token`. Use it before exposing operator routes. |
| `CONCTL_ADMIN_API_TOKEN` | unset | Token sent by `conctl` for protected admin endpoints. |
| `ALLOW_UNAUTH_ADMIN` | `false` | Unsafe override for non-loopback binds without `ADMIN_API_TOKEN`; use only behind equivalent trusted auth. |

The OpenAI-compatible `/v1/*` API uses Consortium API keys, not `ADMIN_API_TOKEN`.

## Storage

| Variable | Default | Description |
| --- | --- | --- |
| `DB_PATH` | `./consortium.db` | SQLite database path. Use a private persistent directory in production. |
| `DB_MAX_OPEN_CONNS` | `4` | Max open SQLite connections for file-backed DBs. |
| `DB_MAX_IDLE_CONNS` | `DB_MAX_OPEN_CONNS` | Max idle SQLite connections. |
| `DB_QUERY_LOG_ENABLED` | `false` | Enable diagnostic SQL timing logs. |
| `DB_QUERY_LOG_ALL` | `false` | Log every SQL statement when diagnostics are enabled. |
| `DB_SLOW_QUERY_THRESHOLD_MS` | `250` | Slow query threshold. |
| `DB_STATS_LOG_INTERVAL_SECONDS` | `0` | Periodic DB stats interval; `0` disables it. |

## Workers And Admission

| Variable | Default | Description |
| --- | --- | --- |
| `MAX_CONCURRENT_WORKFLOWS` | `150` | Max concurrent workflow executions. Tune down for small machines. |
| `MAX_PARALLEL_NODES_PER_WORKFLOW` | `32` | Max parallel DAG node goroutines per workflow. |
| `WORKER_COUNT` | `300` | Max durable worker goroutines. |
| `WORKER_INITIAL_COUNT` | `10` | Workers started on boot. |
| `WORKER_POLL_INTERVAL_MS` | `100` | Worker polling interval. |
| `WORKER_CLAIM_ERROR_BACKOFF_MAX_MS` | `2000` | Max backoff after transient claim errors. |
| `WORKER_IDLE_BACKOFF_MAX_MS` | `1000` | Max backoff when the queue is empty. |
| `PAUSE_ADMISSION_ON_TERMINAL_FAILURE` | `true` | Pause root admissions after systemic terminal failures. |

## Frontend Serving

| Variable | Default | Description |
| --- | --- | --- |
| `EMBED_FRONTEND` | `false` | Set to `true` for release binaries built with embedded assets. |
| `DEV_STATIC_PATH` | `./static` or `./frontend/dist` | Filesystem SPA path when `EMBED_FRONTEND=false`. In dev, use Vite on `FRONTEND_PORT` for the live UI. |
| `VITE_API_TARGET` | `$BACKEND_URL` | Frontend dev proxy target. |
| `VITE_ALLOWED_HOSTS` | unset | Comma-separated extra hosts accepted by Vite dev server. |

## Optional Novomo Runtime

| Variable | Default | Description |
| --- | --- | --- |
| `NOVOMO_URL` | `http://localhost:8090` | Base URL for Novomo-backed `agent_run` and `novo_run` nodes. |
| `NOVOMO_API_KEY` | unset | Bearer token sent to Novomo when configured. |
| `NOVOMO_FRONTEND_V2_URL` | `http://127.0.0.1:5174` | Base URL for admin deep links to Novomo. |

Novomo docs live in the Novomo project: https://github.com/alhasaniq/novomo and https://novomo.alhasaniq.com.

## Optional Benchmark Tooling

| Variable | Description |
| --- | --- |
| `ARTIFICIAL_ANALYSIS_API_KEY` | Required by `conctl benchmarks models sync` for Artificial Analysis V2 model metadata. |
| `WORKTREE_PROFILE` | Local worktree profile name used by helper scripts. |

## Tracing Retention

| Variable | Default | Description |
| --- | --- | --- |
| `REASONING_TRACE_TTL_SECONDS` | `21600` | Verbose reasoning trace retention. |
| `REASONING_PURGE_INTERVAL_SECONDS` | `1800` | Reasoning trace purge interval. |
