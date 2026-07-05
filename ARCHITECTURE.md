# Architecture

Detailed technical guide for Consortium's LLM workflow orchestration system.

## System Design

### Core Flow

```
Frontend → API → JobManager → DAGRuntime → NodeRunners → LLM Client → OpenRouter
                      ↓            ↓             ↓            ↓
                  Jobs DB  execution_history activity_results  workflow_node_executions
```

### Design Principles

1. **Centralized Job Tracking** - ALL workflow executions MUST go through JobManager
2. **Centralized LLM Accounting** - ALL LLM requests MUST use `providers.Client`
3. **UUIDs Everywhere** - UUID v4 for all identifiers
4. **Automatic Retry** - Configurable per-node retry with exponential backoff

## Critical Rules

### Rule #1: JobManager Required

```go
// ✅ CORRECT - Automatic DB tracking
manager := jobs.NewManager(storage, registry)
result, err := manager.ExecuteWorkflow(ctx, wf)

// ❌ WRONG - No DB tracking, not visible in admin
executor.Execute(ctx, wf, nil)
```

**Why mandatory:**
- Creates job record before execution
- Updates status automatically (pending → running → completed/failed)
- Tracks tokens, cost, latency
- Single source of truth

### Rule #2: LLM Client Required

```go
// ✅ CORRECT - Automatic cost tracking
llmClient := providers.NewClient(registry, nodeLogger)
resp, err := llmClient.Complete(ctx, req, &providers.CompletionContext{JobID: jobID})

// ❌ WRONG - No logging/accounting
resp, err := registry.Complete(ctx, req)
```

**Auto actions:**
1. Call provider API
2. Measure latency
3. Calculate cost (tokens × pricing)
4. Log request + attempt telemetry (`workflow_node_executions`, `workflow_node_execution_attempts`)
5. Return response/error

All requests logged, even failures.

## Key Components

### JobManager ([pkg/jobs/manager.go](pkg/jobs/manager.go))

Entry point for all workflow executions.

```go
type Manager struct {
    storage        *storage.Storage
    registry       *providers.Registry
    durableRuntime *durable.DAGRuntime
}

// Worker lifecycle (required)
func (m *Manager) StartWorkers()
func (m *Manager) StopWorkers(ctx context.Context)

// Async submit (used with WS stream)
func (m *Manager) SubmitWorkflow(ctx context.Context, req *SubmitWorkflowRequest) (*SubmitWorkflowResponse, error)

// Sync API path (internally submit + wait)
func (m *Manager) ExecuteWorkflow(ctx context.Context, wf *workflow.Workflow) (*WorkflowExecutionResult, error)
```

### DAGRuntime ([pkg/workflow/runtime/durable/](pkg/workflow/runtime/durable/))

Durable execution engine. Schedules activities from a frozen workflow snapshot, persists history for replay, and caches activity results for idempotency.

```go
type DAGRuntime struct {
    storage  *storage.Storage
    handlers map[string]ActivityHandler  // node type → handler
}

func (r *DAGRuntime) Execute(ctx, job, snapshot, callback) error
```

**Key properties:**
- Event-sourced: `execution_history` table records all state transitions
- Idempotent: `activity_results` table prevents duplicate work on replay
- Deterministic scheduling: `ReadySet(deps)` returns topologically sorted ready nodes

### Executor ([pkg/workflow/executor.go](pkg/workflow/executor.go))

Low-level node execution logic. **Not called directly in production** — the durable runtime dispatches to `NodeRunner` implementations instead. Kept for unit tests that validate node runner behavior.

**Parallel execution:** Achieved via edges (DAG). Nodes at the same level run concurrently with a context snapshot.

**Per-node retry:** Each node can have a `RetryPolicy` (max attempts, backoff, retryable errors). Failed attempts logged to `workflow_node_execution_attempts`.

### LLM Client ([pkg/providers/client.go](pkg/providers/client.go))

All LLM requests go through this client for automatic accounting.

```go
type Client struct {
    registry *providers.Registry
    storage  *storage.Storage
}

func (c *Client) Complete(ctx, req, compCtx) (*CompletionResponse, error)
```

### Storage ([pkg/storage/storage.go](pkg/storage/storage.go))

SQLite with WAL mode, busy_timeout=5s, and MaxOpenConns=4 for file-backed DBs (`:memory:` stays single-connection).

**Tables:**
- `jobs` - Workflow metadata, results, tokens, cost
- `workflow_node_execution_attempts` - Retry attempts
- `workflow_node_executions` - Per-node metrics (auto node_order)
- `workflows` - Workflow definitions (JSON)

## Workflow Patterns

### Sequential

```go
Nodes: []*Node{
    {ID: "node_0", Prompt: "Research {{topic}}"},
    {ID: "node_1", Prompt: "Summarize {{node_0}}"},
}
Context: {"topic": "AI"}
```

**Flow:** node_0 executes → output stored as `context["node_0"]` → node_1 reads `{{node_0}}`

### Parallel (via DAG Edges)

```go
{
    Nodes: []*Node{
        {ID: "tech", Type: NodeTypePrompt, Prompt: "Technical analysis {{topic}}"},
        {ID: "biz", Type: NodeTypePrompt, Prompt: "Business analysis {{topic}}"},
    },
    Edges: []*Edge{}, // No edges = all nodes can run in parallel
}
```

**Flow:** Nodes without dependencies (no incoming edges) execute concurrently. Results stored in context.

**Performance:** 3 parallel LLM calls ≈ 1x latency (not 3x)

### Conditional

```go
{
    Type: NodeTypeConditional,
    Condition: "sentiment contains positive",
    TrueBranch: &Node{Prompt: "Thank you message"},
    FalseBranch: &Node{Prompt: "Address concern"},
}
```

**Operators:** `contains`, `equals`, `not_empty`

**Format:** `variable_name operator value`

### Result (Output Aggregation)

```go
{
    Type: NodeTypeResult,
    OutputName: "final_report",
    Metadata: {"input_ids": ["node_0", "node_1"]},
    AggregationMethod: "peer_matrix",  // collect, judge, scoring, synthesis, peer_matrix, majority_vote, debate_decide
    AggregationConfig: { ... },
}
```

Aggregates multiple inputs → `WorkflowResult.Outputs["final_report"]`

## Aggregation Methods

| Method | LLM Calls | Description |
|--------|-----------|-------------|
| `collect` | 0 | Join with separator |
| `judge` | 1 | Single LLM picks winner |
| `scoring` | N | Score each against rubric |
| `synthesis` | 1 | Combine into unified response |
| `peer_matrix` | N×(N-1) | Cross-agent evaluation |
| `majority_vote` | 0 | Extract discrete answers, count votes |
| `debate_decide` | 0-1 | Group by answer camps, judge decides |

### peer_matrix

Each agent scores every other agent's response. Most robust for ensemble evaluation.

```go
config: {
    "judge_model": "deepseek/deepseek-v4-flash",
    "normalization": "none",               // Raw scores (default)
    "max_parallel": 6,                     // Concurrent evaluations
    "rubric_mode": "dynamic",             // Optional: LLM-generated task-specific rubric
}
```

**Flow:** Collect → Plan N×(N-1) tasks → Parallel evaluate → Normalize → Average → Select winner

**Sub-node IDs:** `{parent}__agg__peer__{reviewer}__{candidate}` (prevents log collisions)

## Context & Variables

**Syntax:** `{{variable_name}}`

**Nodes as variables:** `{{node_uuid}}`

**Updates:** Context updated after each node with node output

**Parallel nodes:** Read-only snapshot to prevent race conditions

## Execution Lifecycle

```
pending → running → completed
                  → failed (retry exhausted)
pending/running → cancelled (user cancel)
```

### Retry Strategy

Configurable per node via `RetryPolicy`. Default for `llm_call` nodes: 3 attempts, 1s initial backoff, 2x multiplier, 30s max. Non-retryable: cost limit, context cancellation, explicitly marked errors.

## Database Schema

Source of truth: [pkg/storage/schema.sql](pkg/storage/schema.sql)

### Identity Model

Several tables share a three-level identity scheme for the durable runtime:

| Column | Meaning | Lifecycle |
|--------|---------|-----------|
| `jobs.id` | Primary key. Unique per execution record. | Immutable |
| `jobs.workflow_execution_id` | Stable logical identity. Survives run rollover. | Stable across runs |
| `jobs.run_id` | Per-run identity. Changes on each rollover attempt. | New per run |
| `job_id` (child tables) | FK to `jobs.id`. Links to the parent execution record. | Matches `jobs.id` |
| `execution_id` (child tables) | Stable logical identity (= `jobs.workflow_execution_id`). | Stable across runs |
| `run_id` (child tables) | Per-run identity (= `jobs.run_id`). | New per run |

For a first execution (no rollover), all three are equal: `id = workflow_execution_id = run_id`. After rollover, a new `jobs` row is created with a new `id` and `run_id` but the same `workflow_execution_id`.

---

### Core Execution

#### `jobs`

Top-level workflow execution record. One row per execution. This is the parent table referenced by most other tables via `job_id`.

Key columns: `id`, `status` (pending/running/completed/failed/cancelled/archived), `query` (workflow description), `model`, `tokens_*`, `cost`, `result_text`, `workflow_id` (FK to `workflows`), `dag_snapshot` + `dag_hash` (frozen workflow for durable runtime), `workflow_execution_id` + `run_id` + `run_number` (durable identity), `idempotency_key` + `request_hash` (deduplication), `last_event_sequence` (monotonic counter for event ordering).

#### `workflow_node_executions`

Latest execution state per node per run. Projection table — upserted on each attempt, so it always reflects the most recent attempt's metrics. Use this for current node status; use `workflow_node_execution_attempts` for full history.

Unique on `(job_id, run_id, node_id)`. `node_order` is auto-assigned per execution.

#### `workflow_node_execution_attempts`

Full retry/attempt history per node. Every attempt gets its own row, preserving the complete execution timeline including failures. Unique on `(job_id, run_id, node_id, attempt_number)`.

#### `workflows`

Workflow definitions (blueprints/templates). The `definition` column stores the complete workflow file format as JSON. These are the reusable templates that get frozen into `jobs.dag_snapshot` at submit time.

---

### Streaming & Durability

#### `job_events`

Append-only event log for WebSocket stream resilience. Each event gets a per-job monotonic `sequence` number. Clients reconnect with `resume_from=sequence` to replay missed events without re-executing. Indexed by `(job_id, sequence)`.

#### `side_effect_outbox`

Outbox pattern for durable side effects (tool calls, webhooks). Ensures at-least-once delivery with configurable retry (`max_attempts`, `next_attempt_at`). Status: pending → processing → completed/failed.

---

### Tracing

#### `job_node_traces`

Fine-grained execution spans within nodes, aligned with OpenTelemetry conventions. Three span kinds: `node` (overall node execution), `call` (LLM API call), `decision` (conditional evaluation). Spans form a tree via `parent_span_id`. Attributes stored as JSON.

---

### Durable Runtime

#### `execution_history`

Append-only history events with per-run monotonic sequence. The durable runtime replays these to reconstruct scheduler state after server restart. Events include `workflow_started`, `schedule_activity`, `activity_completed`, `activity_failed`, `workflow_completed`, etc. Unique on `(run_id, sequence)`.

#### `activity_results`

Idempotent activity outcome cache. Keyed by `idempotency_key` (`{execution_id}:{node_id}:{attempt}`). On replay, the runtime checks this table first — if a result exists, it skips re-execution. Prevents duplicate LLM calls.

---

### Agent Storage

#### `agent_runs`

Per-node autonomous agent run summary. Tracks iteration count, tool call count, budget usage (tokens, cost, time), decision summary, stop reason, and evidence references. One row per agent invocation.

#### `agent_events`

Ordered internal loop events from agent execution. Captures per-iteration reasoning, decisions, and state transitions. Sequenced per `agent_run_id`. Supports optional `reasoning_trace` storage for debug-verbose mode.

#### `agent_tool_calls`

Individual tool invocations within agent loops. Records tool name, input/output payloads, duration, status, and error details. Linked to both `agent_run_id` and `iteration` for drill-down.

## Coding Style

### Imports

Three groups, blank-line separated:

```go
import (
    "context"
    "fmt"

    "github.com/google/uuid"
    "github.com/labstack/echo/v4"

    "consortium/pkg/storage"
    "consortium/pkg/workflow"
)
```

### Names

- **Private:** camelCase
- **Public:** PascalCase

### Errors

Return errors (not log.Fatal except main):

```go
func Process() error {
    if err != nil {
        return fmt.Errorf("process failed: %w", err)
    }
    return nil
}
```

### SQL

Use COALESCE for nulls:

```sql
SELECT COALESCE(cost, 0) FROM jobs
```

### IDs

UUID v4 everywhere:

```go
import "github.com/google/uuid"
id := uuid.New().String()
```

## Common Tasks

### Add Model

OpenRouter models are loaded dynamically via provider cache (`/models`) with static fallback in [pkg/providers/openrouter.go](pkg/providers/openrouter.go).  
Update fallback metadata only when needed (offline safety, pinning, or tests).

### Modify Workflow Execution

Edit [pkg/workflow/executor.go](pkg/workflow/executor.go)

### Add API Endpoint

Edit [cmd/server/main.go](cmd/server/main.go) or [pkg/api/workflow.go](pkg/api/workflow.go)

### Debug Failed Jobs

```bash
conctl local db-query --sql "SELECT * FROM jobs WHERE status='failed'"
conctl local db-query --sql "SELECT * FROM workflow_node_executions WHERE job_id='xxx' ORDER BY node_order"
```

## API Endpoints

Canonical endpoint tables are maintained in `docs/workflow-system.md` to avoid drift.
Use that document as the source of truth for workflow, jobs, admin, and legacy endpoint status.

## Middleware Order

1. **Recovery** - Catch panics
2. **RequestID** - Tracing
3. **Logger** - Audit
4. **CORS** - Headers

## Environment Variables

```bash
# Required
OPENROUTER_API_KEY=sk-...

# Optional
DB_PATH=consortium.db              # Default: consortium.db
PORT=8080                          # Default: 8080
BIND_ADDR=:8080                    # Default: :PORT
EMBED_FRONTEND=false               # true (release binary) = serve SPA embedded in binary; leave false in dev
DEV_STATIC_PATH=./frontend/dist    # Filesystem SPA path the backend serves when EMBED_FRONTEND=false (stale build snapshot in dev)
MAX_CONCURRENT_WORKFLOWS=150
MAX_PARALLEL_NODES_PER_WORKFLOW=32
WORKER_COUNT=300
WORKER_POLL_INTERVAL_MS=100
WORKER_CLAIM_ERROR_BACKOFF_MAX_MS=2000
DB_MAX_OPEN_CONNS=4
DB_MAX_IDLE_CONNS=4
REASONING_TRACE_TTL_SECONDS=21600
REASONING_PURGE_INTERVAL_SECONDS=1800
PAUSE_ADMISSION_ON_TERMINAL_FAILURE=true
```

## Monitoring

### Admin Panel

http://localhost:8080/admin (all jobs, per-node metrics)

> **Dev note:** the admin *data* (`/api/admin/*`) is always live, but the admin *page* HTML at `:8080/admin` is served from the stale `frontend/dist` snapshot in dev (the backend never rebuilds it — only `make build-frontend`/`make build-release` do, or it's embedded in a release binary). The live frontend (Ensemble, Builder) is the Vite dev server on port **3000**. See [docs/environment-variables.md](docs/environment-variables.md#how-the-backend-chooses-what-frontend-to-serve).

### Database

```bash
conctl local db-query --sql "SELECT * FROM jobs WHERE status='running' AND updated_at < datetime('now', '-10 minutes')"
conctl local db-query --sql "SELECT * FROM workflow_node_executions WHERE job_id='xxx'"
```

## Stuck Workflow Prevention

**Server startup recovery:**
- Legacy jobs (no `dag_hash`): force-failed on restart
- Durable jobs (`dag_hash` present): background workers automatically resume from `execution_history`

**Background workers:**
- Poll for claimable jobs (pending durable + resumable running durable)
- Atomic claim via `ClaimPendingDurableJob()` (single transaction claim/update)
- Release admission slot on completion

**Panic recovery:**
- Guarantees status update even on crashes

## Best Practices

### ✅ DO

```go
// Use JobManager
manager := jobs.NewManager(storage, registry)
result, err := manager.ExecuteWorkflow(ctx, wf)

// Use LLM Client
llmClient := providers.NewClient(registry, nodeLogger)
resp, err := llmClient.Complete(ctx, req, &providers.CompletionContext{JobID: jobID})

// Use UUIDs
workflowID := uuid.New().String()

// Handle errors
if err != nil {
    log.Printf("Failed: %v", err)
    return err
}
```

### ❌ DON'T

```go
// Skip JobManager
executor.Execute(ctx, wf, nil)

// Skip LLM Client
resp, err := registry.Complete(ctx, req)

// String IDs
workflowID := "workflow-123"

// log.Fatal outside main
if err != nil {
    log.Fatal(err)
}
```

## Troubleshooting

**Jobs stuck pending:**
```bash
make backend-restart     # Restart server
```

**DB locked:**
```go
// Already using WAL + busy_timeout=5s
// If persists: increase timeout
dsn := fmt.Sprintf("%s?_busy_timeout=10000", dbPath)
```

**Jobs not in admin:**
- Must use JobManager, not executor directly

**Timeout:**
- Increase context timeout in executor

## Further Reading

- [docs/workflow-system.md](docs/workflow-system.md) - Comprehensive workflow guide with diagrams
- [docs/deployment.md](docs/deployment.md) - Build and deployment guide
- [pkg/workflow/types.go](pkg/workflow/types.go) - Type definitions and examples
