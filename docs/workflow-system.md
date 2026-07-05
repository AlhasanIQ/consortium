# Workflow System

Comprehensive workflow architecture for Consortium.

## Overview

LLM orchestration system:
- Visual builder (React Flow)
- Real-time streaming (WebSocket)
- Auto LLM tracking
- Multiple patterns (sequential, parallel, conditional)
- SQLite persistence

### Design Principles

1. **Centralized Job Tracking** - MUST use JobManager
2. **Centralized LLM Accounting** - ALL requests via `providers.Client`
3. **UUID IDs** - UUID v4 everywhere
4. **Explicit per-node retry** - External-call nodes carry first-class retry policy with exponential backoff; retries are node execution behavior, not a workflow layer
5. **Default model** - When an LLM-capable node omits `model`, Consortium defaults to `deepseek/deepseek-v4-flash`

## Architecture

```
Frontend → REST/WS → JobManager → DAGRuntime → NodeRunners → LLM Client → OpenRouter
                          ↓            ↓             ↓            ↓
                       SQLite  execution_history activity_results  Accounting
```

## Core Components

### Workflow ([pkg/workflow/types.go](pkg/workflow/types.go))

```go
type Workflow struct {
    ID          string                 // UUID v4
    Name        string
    Description string
    Nodes       []*Node
    Context     map[string]interface{}
}
```

**Workflow layers:** workflow IDs define the layer. `aggregation-*` workflows are L0 reusable aggregation internals, `reasoning-*` workflows are L1 user-facing reasoning primitives, `composite-*` workflows are L2 compositions of L1 workflows, and `benchmark-*` workflows are L3 benchmark wrappers. Benchmarks call L1 workflows, not L0 workflows, so benchmark attribution continues to measure product behavior rather than aggregation internals in isolation.

**Node types:**
- `prompt` - LLM completion
- `conditional` - Branch on condition
- `result` - Output aggregation (supports multiple inputs + aggregation methods)
- `aggregation` - Workflow Builder authoring macro for visible aggregation internals. New builder/seed workflows attach an L0 `aggregation-*` source workflow ID; submit converts that macro to `workflow_ref` and compiles it into expanded `operation`/`prompt`/`result` nodes before freezing.
- `workflow_ref` - Source-level compiled composition. The submit path resolves the referenced workflow, expands its nodes into the parent durable DAG with stable `<reference-node-id>--<child-node-id>` IDs, and stores source workflow metadata on expanded nodes before freezing.
- `operation` - Deterministic zero-cost data operation. Used by compiled aggregation internals for extraction, candidate formatting, vote counting, camp grouping, score reduction, winner selection, and JSON field parsing. Operation nodes run inside the parent DAG and do not make LLM calls.
- `child_workflow` - Execute another workflow as a native child execution and wait for completion. Empty selected output fails the node (`OUTPUT_TRUNCATED_EMPTY` retryable when child consumed output tokens; otherwise non-retryable `EMPTY_CHILD_OUTPUT`)
- `contract_extract` - Regex-first extraction with LLM fallback. Reads upstream output from a configured `sourceVariable`, applies ordered `extractionPatterns` (regex cascade), and returns the first capture group match. Falls back to LLM call only when all patterns fail. Used by benchmark wrappers to extract canonical single-letter answers at zero cost.
- `agent_run` - Delegate execution to an external agent harness via Novomo. Consortium submits a prompt, polls until terminal, and consumes the final output as the node's `Output`. Current supported Novomo JobAgent harnesses are `claude-code` and `codex`. See [Agent Run Execution](#agent-run-execution) below and [docs/agent-run-contract.md](agent-run-contract.md) for the Consortium↔Novomo HTTP contract.
- `novo_run` - Frontend label: **Superagent**. Wake Novomo's Novo runtime through Novomo Tasks and poll the resulting `NovoRun`. This is distinct from `agent_run`: `agent_run` launches a Novomo Job, while `novo_run` wakes or creates a Task and launches one Novo wake session. See [Superagent Novo Run Execution](#superagent-novo-run-execution).

#### Compiled Workflow References

`workflow_ref` means "use this workflow here." During `SubmitWorkflow`, Consortium compiles every resolved `workflow_ref` into the parent workflow before validation, request/config hashing, replay-plan candidate freezing, and durable snapshot creation. The frozen DAG contains normal executable nodes, not `workflow_ref` authoring nodes.

Compiled expansion is generic across workflow layers:
- L0 `aggregation-*` references compile reusable aggregation internals into L1 workflows.
- L1 `reasoning-*` references can compile into L2 composite workflows.
- L2 `composite-*` references can compile into higher-order workflows when authored.

Each expanded node ID is prefixed with the reference node ID and `--`, for example `reasoning--agent-a`. The compiler rejects generated IDs that would collide or use the reserved `__` context-metadata delimiter. Expanded nodes receive `metadata.source_workflow_id`, `metadata.source_workflow_hash`, and `metadata.source_node_id` so frozen jobs remain explainable and replayable after the referenced source workflow changes.

`workflow_ref` is not a runtime execution boundary. Its nodes share the parent job, parent history stream, parent retry accounting, and parent DAG hash. Existing `child_workflow` nodes remain the explicit separate-run boundary with a separate job, separate frozen DAG, and parent-child observability.

Benchmark wrappers stay on `child_workflow` for L3 -> L1 execution in this milestone because benchmark analysis currently uses parent/child job relationships to attribute model results, aggregation behavior, latency, and retries. Compiled benchmark wrappers require a later benchmark-analysis redesign around source groups inside a single job.

#### Deterministic Operation Nodes

`operation` nodes execute deterministic transformations in the workflow runtime without LLM calls or cost accounting. They are runtime nodes, not separate jobs, and are registered through `OperationNodeRunner`.

Supported `operation_type` values:
- `collect_inputs` - collect configured candidate outputs into structured `items` plus joined `text`
- `format_candidates` - format candidate outputs into labeled text and structured candidate records
- `extract_answer` - extract a discrete answer from text using the shared answer extraction helpers
- `count_votes` - tally answer strings and return counts, total, tie status, and winner
- `group_answer_camps` - group candidate IDs by answer
- `select_winner` - choose the highest-scoring candidate and return its output when available
- `reduce_scores` - reduce repeated candidate scores to averages and select the winner
- `parse_json_field` - parse a field from JSON text

`operation_config` supports literal values and `{{variable}}` interpolation against workflow context. A config value that is exactly a single template token preserves the raw context value, so arrays and objects can flow into operations without stringification. Operation type and config participate in workflow config hashes and frozen DAG hashes. Expanded compiled-reference nodes also carry `metadata.source_workflow_hash`, which participates in workflow diffing so replay planning can see source workflow changes.

#### Child Workflow Execution

The `child_workflow` node type (`ChildWorkflowNodeRunner` in `pkg/workflow/runner_child_workflow.go`) enables workflow composition. A parent workflow can invoke any other workflow as a child, wait for completion, and consume its output.

**Execution flow:**
1. Parent executor reaches a `child_workflow` node
2. Runner calls `ExecuteChildWorkflow` callback on `ExecutionContext`
3. `ExecuteChildWorkflow` (in `pkg/jobs/manager_child_workflow.go`) submits the child workflow with `ParentExecutionID` set to the parent's `workflow_execution_id`
4. Child executes as a normal durable job through the worker pool
5. Runner polls for child completion, then extracts the selected output key
6. Child output becomes available in parent workflow context as `{{child-node-id}}`

**Identity linkage:**
- Parent job has `workflow_execution_id` and `run_id` (both initially equal `job_id`)
- Child job has `parent_execution_id` = parent's `workflow_execution_id`
- Child gets its own `workflow_execution_id`, `run_id`, `dag_hash`, and `dag_snapshot`
- `GET /api/admin/jobs?parent=<execution_id>` lists children of a parent

**Admission and deadlock safety:**
- Child submissions bypass admission control to avoid parent/child worker starvation deadlock
- `WORKER_COUNT` (default 300) is clamped to at least `MAX_CONCURRENT_WORKFLOWS` (150)

**Failure propagation:**
- Child failure propagates to the parent as a node-level error
- Empty child output: `EMPTY_CHILD_OUTPUT` (non-retryable) or `OUTPUT_TRUNCATED_EMPTY` (retryable when child consumed output tokens)
- Parent node retry policy applies — if the `child_workflow` node has retries configured, the entire child workflow re-executes on failure
- Child-internal retries (per-node retry policy within the child) are independent of parent retries

**Variable interpolation:**
- After a `child_workflow` node completes, its output is stored in the workflow context keyed by the node ID
- Downstream parent nodes can reference it via `{{child-node-id}}` in their prompts
- `interpolateVariables` (in `executor.go`) resolves these references at execution time

#### Agent Run Execution

The `agent_run` node type (`AgentRunNodeRunner` in `pkg/workflow/runner_agent_run.go`) delegates execution to **Novomo**, an external agent runtime that owns the harness, sandbox, workspace, and MCP tools. Consortium does not run the agent loop — it submits the task, waits for a result, and consumes the agent's final output as a regular node output. Current supported Novomo JobAgent harnesses are `claude-code` and `codex`; other harness strings are rejected before submission. See [docs/agent-run-contract.md](agent-run-contract.md) for the full HTTP contract.

**Node config (iter 1):**
- `prompt` (string, required) — task description handed to the agent. Supports `{{variable}}` interpolation against upstream node outputs.
- `harness` (`"claude-code"` or `"codex"`, required) — selects the JobAgent harness inside Novomo.
- `sandbox` (`host` or `docker`, optional; default `docker`) — Novomo Job sandbox selection. Set `host` explicitly when a workflow needs host execution.
- `timeout_seconds` (integer, required) — hard outer ceiling. If exceeded, the node fails with `error.code = "timeout"`.
- `inherit_from` (object, optional), `inherit_from_node_id` (string, optional), or `inherit_from_workflow_task` (boolean, internal) — opaque Novomo handoff. Explicit handles support `kind = job_run | job | novo_run | task`; upstream node references must point to an earlier `agent_run` or `novo_run` node. Builder workflow files default Novomo-backed nodes to `inheritFromMode: "auto"`, which resolves one nearest upstream Novomo-backed ancestor into `inherit_from_node_id` during conversion, skipping classical nodes in between. When Auto reaches multiple nearest Novomo-backed ancestors at a fan-in boundary, conversion emits `inherit_from_workflow_task`; runtime resolves that to the shared Novomo task for the Consortium workflow execution. Consortium forwards the resolved handle and does not inspect Novomo workspace/memory internals.

**Execution flow:**
1. Executor reaches an `agent_run` node.
2. Runner interpolates `{{var}}` references in `prompt` and calls `ExecuteAgentRun` on `ExecutionContext`.
3. `ExecuteAgentRun` (in `pkg/jobs/manager_agent_run.go`) looks up the slim `agent_runs` row by `(job_id, run_id, node_id, attempt)`. If a running row already exists after process restart, it resumes polling the existing `external_run_id` instead of submitting a duplicate.
4. If the node has `inherit_from_node_id`, the manager resolves that upstream node's persisted Novomo handle from `agent_runs` before submit. Upstream Superagent rows resolve to `novo_run`; upstream Agent rows resolve to `job_run` when `external_job_run_id` is known, otherwise `job`.
5. If no row exists, the manager calls `pkg/novomo` client's `POST /v1/runs` with `{prompt, harness, sandbox, task_id, timeout_seconds, inherit_from}` and an `Idempotency-Key`, receives an `external_run_id`, then persists a row in the slim `agent_runs` table (`{job_id, run_id, node_id, attempt, external_run_id, external_job_run_id, external_task_id, inherit_from_json, status: "running", ...}`). `task_id` is deterministic per Consortium workflow run so fan-in Auto can hand off from the workflow's Novomo task.
6. Manager polls `GET /v1/runs/{external_run_id}` every 2–5s until terminal (`completed`, `failed`, or `cancelled`).
7. If the parent workflow context is cancelled after `external_run_id` is persisted, Manager calls `POST /v1/runs/{external_run_id}/stop` on a best-effort basis before marking the local `agent_runs` row cancelled.
8. On terminal, the `agent_runs` row is updated with `output`, `tokens_input`, `tokens_output`, `cost_usd`, `error_code`, and latest `external_job_run_id` when Novomo exposes it. The runner returns a `NodeResult`:
   - `Output` ← Novomo's `output` string
   - `TokensInput/Output`, `Cost` ← totals from Novomo, summed into the job's `cost`/`tokens_total` but **not** metered against the workflow `CostTracker`/cost cap (see [agent-run-contract.md](agent-run-contract.md#known-gap-novomo-cost-is-reported-not-metered))
   - `Metadata` ← `{harness, external_run_id, external_job_run_id, inherit_from, error_code}`
9. Downstream nodes can reference the agent's output via `{{agent-run-node-id}}` in their prompts (same pattern as `prompt` and `child_workflow` outputs), or use `inherit_from_node_id` to ask Novomo to inherit runtime-owned context from the upstream agent run.

The admin job detail view includes an **Agent Runs** tab. It shows the corresponding Novomo Job/JobRun IDs for `agent_run` rows and NovoRun/Task IDs for Superagent (`novo_run`) rows, with frontend-v2 deep links built from `NOVOMO_FRONTEND_V2_URL`. Active Novomo-backed rows with a persisted `external_run_id` expose a stop action that calls the matching Novomo stop endpoint and marks the local row cancelled for immediate operator feedback. Terminal rows or rows without an external ID do not expose a stop action.

**Failure modes** (iter 1 collapses Novomo's richer status enum to retryable vs non-retryable):
- `timeout`, `crashed`, `startup_failed` → retryable
- Auth / bad request / unknown harness → non-retryable
- Consortium's own outer poll deadline (`timeout_seconds + grace`) → `NOVOMO_UNRESPONSIVE` (non-retryable)

**WebSocket events:** Standard `node_start` → `node_complete` pair, like any long-running node. No intermediate per-iteration agent events are streamed in iter 1 — Novomo owns iteration-level visibility. (Iter 2 may add streaming once webhook/async-completion infra lands.)

**What Consortium does NOT do for `agent_run`:**
- Run an agent loop, manage a tool registry, or gate model capabilities.
- Persist per-iteration events, tool calls, or workspace deltas. Those live in Novomo.
- Stream intermediate agent state to clients in iter 1.

**Environment:** Uses `NOVOMO_URL` when set, otherwise defaults to `http://localhost:8090`. Set `NOVOMO_API_KEY` when the target Novomo runtime has auth enabled. Invalid configuration or Novomo auth failures fail the node before or during submit.

Workflows that need Novomo's host sandbox must set `sandbox: "host"` explicitly. Omitted or empty `sandbox` values resolve to `docker`, including legacy snapshots that did not persist a sandbox selection.

#### Superagent Novo Run Execution

The `novo_run` node type (`NovoRunNodeRunner` in `pkg/workflow/runner_novo_run.go`) is exposed in the Workflow Builder as **Superagent**. It uses Novomo's Novo wake API rather than the legacy Job API. Consortium either wakes an existing Novomo Task (`task_id`) or creates a deterministic operator Task from the node attempt idempotency key, then calls `POST /v1/tasks/{task_id}/wake` and polls `GET /v1/novo-runs/{novo_run_id}`.

**Node config:**
- `prompt` (string, optional when `task_id` is set) — goal for the Novo wake. Supports `{{variable}}` interpolation.
- `task_id` (string, optional) — existing Novomo Task to wake. If omitted, Consortium creates a deterministic Task for this workflow node attempt.
- `task_summary` (string, optional) — Task summary when Consortium creates the Task.
- `identity` (string, optional; default `sde-novo`) — Novomo identity/profile for the wake.
- `sandbox` (`host` or `docker`, optional; default `docker`) — Novomo wake sandbox selection.
- `image` (string, optional) — container image when `sandbox=docker`.
- `runtime_url` (string, optional) — per-node override for the runtime URL passed through to Novomo. When omitted, Consortium sends the configured Novomo base URL, defaulting to `http://localhost:8090`, so the wake can reach the runtime API and finish its Task. For Docker sandbox wakes, omitted local URLs using `localhost`, `127.0.0.1`, or `::1` are rewritten to `host.docker.internal` before submission; explicit per-node `runtime_url` values are passed through unchanged.
- `timeout_seconds` (integer, required) — hard outer ceiling for the Consortium node and the Novomo wake.
- `grace_seconds` (integer, optional; default `10`) — local poll grace after `timeout_seconds`.
- `inherit_from` (object, optional) or `inherit_from_node_id` (string, optional) — same opaque handoff contract as `agent_run`; passed through to Novomo's wake request as `inherit_from`.
- `repo_specs` (array of objects, optional) and `work_source` (object, optional) — passed through to Novomo wake for workspace/repo context.

Workflows that need Novomo's host sandbox must set `sandbox: "host"` explicitly. Omitted or empty `sandbox` values resolve to `docker`, including legacy snapshots that did not persist a sandbox selection.

**Execution flow:**
1. Executor reaches a `novo_run` node.
2. Runner interpolates `prompt` and calls `ExecuteNovoRun` on `ExecutionContext`.
3. `ExecuteNovoRun` uses the same `agent_runs` persistence table as `agent_run`, but stores `run_kind = "novo_run"` and, when known, `external_task_id`.
4. If the row already has a running `external_run_id`, Consortium resumes polling it. This prevents duplicate wakes after restart once the ID has been persisted.
5. If the node has `inherit_from_node_id`, the manager resolves that upstream node's persisted Novomo handle from `agent_runs` before waking the Task.
6. If no row exists, the Novomo client ensures the Task exists, adopts its current `novo_run_id` when the Task is already executing, or wakes it to create a fresh `NovoRun`.
7. Manager polls `GET /v1/novo-runs/{novo_run_id}` until terminal. Terminal output, token/cost totals, error details, and the submitted/resolved handoff are written to `agent_runs` and returned as a normal node output.
8. If the parent workflow context is cancelled after `external_run_id` is persisted, Manager calls `POST /v1/novo-runs/{external_run_id}/stop` on a best-effort basis before marking the local row cancelled. The same stop path is available from the admin Agent Runs tab and `conctl jobs stop-agent-run`; the tab links the persisted NovoRun and Task IDs into Novomo frontend v2.

**Failure modes:** `timeout`, `crashed`, `startup_failed`, `cancelled`, and `paused` fail the node. Auth, invalid request shape, invalid sandbox, and invalid Novomo configuration fail before or during submit. The Consortium-side outer deadline reports `NOVOMO_UNRESPONSIVE`.

**What Consortium does NOT do for `novo_run`:**
- It does not launch a NovoGoal; NovoGoal support remains deferred.
- It does not manage Novo internals, per-iteration memory, tool loops, workspace branch mutation, handoff policy semantics, or Task scheduling.
- It does not stream intermediate Novo events. The node emits normal workflow start/complete/failure events.

### JobManager ([pkg/jobs/manager.go](pkg/jobs/manager.go)) - REQUIRED

```go
type Manager struct {
    storage        *storage.Storage
    registry       *providers.Registry
    durableRuntime *durable.DAGRuntime
}

// Worker lifecycle (required for durable execution)
func (m *Manager) StartWorkers()
func (m *Manager) StopWorkers(ctx context.Context)

// Two access patterns
func (m *Manager) ExecuteWorkflow(ctx context.Context, wf *workflow.Workflow) (*WorkflowExecutionResult, error) // submit + wait
func (m *Manager) SubmitWorkflow(ctx context.Context, req *SubmitWorkflowRequest) (*SubmitWorkflowResponse, error) // async submit
```

**Why mandatory:**
- Creates job before execution
- Updates status automatically
- Tracks tokens/cost/latency
- Single source of truth

### DAGRuntime ([pkg/workflow/runtime/durable/](pkg/workflow/runtime/durable/))

Durable execution engine. All production execution routes through this runtime.

- Event-sourced: `execution_history` table records all state transitions
- Idempotent: `activity_results` table prevents duplicate work on replay
- Deterministic scheduling: `ReadySet(deps)` returns topologically sorted ready nodes
- Targeted replay seeding: fresh runs can preload selected upstream nodes as completed from a prior run (`ReplayRequest`), then execute only dirty/downstream nodes. Seeded nodes record zero cost/tokens (original values preserved in history as `seed_*` attributes)
- Replay plan builder: `pkg/workflow/runtime/BuildReplayPlan` compares baseline vs candidate frozen DAGs and computes `reuse_node_ids` / `execute_node_ids`
- `NodeRunnerAdapter` wraps existing `NodeRunner` implementations as `ActivityHandler`

### Executor ([pkg/workflow/executor.go](pkg/workflow/executor.go))

Low-level node execution logic. **Not called directly in production** — the durable runtime dispatches to `NodeRunner` implementations instead. Kept for unit tests that validate node runner behavior.

**Context/variables:**
- Syntax: `{{variable_name}}`
- Nodes: `{{uuid}}`
- Updated after each node
- Parallel: read-only snapshot

**Conditions:**
- Operators: `contains`, `equals`, `not_empty`, `is_empty`, `matches`, `>`, `<`, `>=`, `<=`, `==`, `is_number`, `is_string`
- Format: `variable_name operator value`
- Example: `sentiment contains positive`

### LLM Client ([pkg/providers/client.go](pkg/providers/client.go)) - REQUIRED

```go
type Client struct {
    registry *providers.Registry
    storage  *storage.Storage
}

func (c *Client) Complete(ctx, req, compCtx) (*CompletionResponse, error)
```

**Auto actions:**
1. Call provider
2. Measure latency
3. Calculate cost
4. Log request (via `LogLLMRequest`)
5. Return response/error

All requests logged, even failures.

### Storage ([pkg/storage/storage.go](pkg/storage/storage.go))

SQLite, WAL mode.

**Tables:**
- `jobs` - Metadata, results, tokens, cost, durable execution fields
- `workflows` - Definitions (JSON)
- `workflow_node_executions` - Per-node metrics (auto `node_order`)
- `workflow_node_execution_attempts` - Retry attempts
- `execution_history` - Durable runtime event log (M6.5)
- `activity_results` - Activity result idempotency cache (M6.5)
- `job_events` - Stream resilience events (M3)
- `side_effect_outbox` - Outbox pattern for durable side effects (M3)
- `agent_runs` - Slim Novomo external-run reference and terminal summary for `agent_run` and `novo_run` nodes (`run_kind` distinguishes Novomo Job vs Novo wake; `external_job_run_id` and `inherit_from_json` support opaque Novomo handoff chaining)
- `benchmark_runs`, `benchmark_run_items`, `benchmark_run_item_attempts`, `benchmark_run_failure_counts` - Benchmark infrastructure (M8)

**Config:** busy_timeout=5s, MaxOpenConns=4 for file-backed DBs (tunable via `DB_MAX_OPEN_CONNS`, `DB_MAX_IDLE_CONNS`; `:memory:` stays single-connection)

## Data Flow

### Sync Execution (HTTP)

```
POST /api/workflows/execute
 → WorkflowAPI.handleExecuteWorkflow()
 → jobs.Manager.ExecuteWorkflow()
   → Submit durable job (status=pending, frozen snapshot)
   → Wait for terminal job status
   → Background worker claims + executes via durable runtime
   → Persist node/events/results to DB
 → Return result
```

### Streaming (WebSocket)

Single pattern for streaming execution:

```
POST /api/workflows/submit
 → Creates job with status=pending
 → Returns job_id, workflow_id, status

WS /api/jobs/{id}/stream
 → Upgrade to WebSocket
 → Send snapshot message (current state)
 → Replay persisted events (supports resume_from)
 → Subscribe to durable execution events
 → Background workers execute the job independently
 → Stream continues until terminal status
 → Close with code (4000=success, 4001=failed, 4002=cancelled)
```

## Execution Lifecycle

```
pending → running → completed
         → paused
                  → failed (retry exhausted)
pending/running/paused → cancelled (user cancel)
```

**Retry strategy:** Configurable per node via explicit `RetryPolicy`. Builder-authored external-call nodes submit 3 attempts, 1s initial backoff, 2x multiplier, and 30s max backoff unless the user changes those fields. Retryable error codes default to `RATE_LIMIT`, `TIMEOUT`, `TEMPORARY`, `UNAVAILABLE`, `CONNECTION`, `5xx` when `retryable_errors` is omitted. Non-retryable: `CostLimitError`, context cancellation, explicitly marked errors. Retries are not a workflow layer; L0/L1/L2/L3 describe workflow purpose and composition boundaries, while retry policy belongs to each executable node.

**Monitoring:**
- DB tracking: jobs + workflow_node_executions
- Admin panel: all jobs, per-node metrics

## Workflow Patterns

### Sequential

```go
Nodes: []*Node{
    {ID: "node_0", Prompt: "Research {{topic}}"},
    {ID: "node_1", Prompt: "Summarize {{node_0}}"},
}
Context: {"topic": "AI"}
```

Flow: node_0 → context["node_0"] → node_1 reads {{node_0}}

### Parallel (via DAG Edges)

```go
{
    Nodes: []*Node{
        {ID: "tech", Type: NodeTypePrompt, Prompt: "Technical {{topic}}"},
        {ID: "biz", Type: NodeTypePrompt, Prompt: "Business {{topic}}"},
    },
    Edges: []*Edge{}, // No edges = all nodes can run in parallel
}
```

Flow: Nodes without dependencies execute concurrently. Results stored in context by node ID.

Performance: 3 parallel LLM calls = ~1x latency (not 3x)

### Conditional

```go
{
    Type: NodeTypeConditional,
    Condition: "sentiment contains positive",
    TrueBranch: &Node{Prompt: "Thank you"},
    FalseBranch: &Node{Prompt: "Address concern"},
}
```

Flow: Evaluate condition → execute matching branch

### Output (Result Node)

```go
{
    Type: NodeTypeResult,
    OutputName: "final_report",
    Metadata: {"input_ids": ["node_0", "node_1"]},  // Supports multiple inputs
    AggregationMethod: "peer_matrix",
    AggregationConfig: { ... },
}
```

Mark value as named output → `WorkflowResult.Outputs["final_report"]`
When multiple inputs are provided, they are aggregated using the specified method.

### Builder Aggregation Macro

The Workflow Builder exposes aggregation as a first-class `aggregation` node so the canvas can show the method, evaluator model ownership, and a backend-compiled expansion preview. New workflows store the selected reusable L0 source in `data.config.aggregationWorkflowId`:

```text
agent/result producers -> aggregation -> result
```

Before execution, the submit path converts an L0-backed aggregation macro to a source-level `workflow_ref`, then the compiler expands that referenced `aggregation-*` workflow into the parent frozen DAG:

- the collapsed `aggregation` node becomes the expansion anchor
- upstream producer IDs bind to the L0 workflow's candidate inputs
- deterministic `operation` nodes perform extraction, candidate formatting, vote counting, score reduction, and winner selection
- LLM evaluator nodes are normal `prompt` nodes with their own retry/usage rows
- the downstream `result` node is presentation-only and supplies `output_name`/`output_format`
- expanded nodes are grouped under the compiled terminal result via `parent_node_id` and provenance metadata

The committed L0 `aggregation-*` seed JSON files are descriptors: they name the method, expose method knobs, and provide provenance for the canonical aggregation family. They are not the runtime source of truth for aggregation internals. Execution, Builder **Expand**, and Builder **Fork** all use the backend compiler (`pkg/workflow/compiler`) through submit or compile-preview to generate the actual operation/prompt/result DAG from the parent workflow's upstream edges and aggregation config.

Historical `result`-owned aggregation workflows without an L0 source remain valid as the legacy compatibility path. New seeds and builder-authored aggregation workflows should use `aggregationWorkflowId` so hidden LLM aggregation orchestration is represented by compiled, retryable runtime nodes. Non-collect result-owned aggregation runs through the legacy aggregation registry and persists `legacy_result_aggregation=true` in the node execution metadata runtime projection for debugging, not in the frozen DAG node definition. `peer_matrix` still inherits reviewer models from the upstream producer IDs via the `{{node_id}}__model` context keys, not from the aggregation node itself.

Compiled L0 aggregation is statically unrolled from authored upstream edges. Dynamic candidate counts are outside this milestone; keep those workflows on the legacy result-owned aggregation path or add an explicit runtime fanout/map node before migrating them to L0 references. For fixed candidate sets, compiled judge/scoring/peer/debate workflows emit visible unanimous or single-camp fallback operation nodes, but evaluator prompt nodes are still part of the static DAG. Runtime evaluator-skip scheduling is a future optimization, not current compiled-L0 behavior.

Workflow Builder's **Expand** action calls `POST /api/workflows/compile-preview` to render a read-only dry run of the same backend compiler path. The preview graph is backend-owned: `scoring` shows one scorer prompt per response/candidate plus a rubric generator when `rubric_mode: "dynamic"`, `peer_matrix` shows the reviewer-candidate cross product with no self-review plus a rubric generator when dynamic, and conditional branch LLM jobs such as tie synthesis or repair calls are counted from backend-provided branch summaries. The frontend lays out and frames the returned graph, reconnecting the compiled terminal result to the presentation result, and auto-fits the canvas to the expanded frame, but it does not compile aggregation methods.

Expanded preview nodes are UI-only. They can be dragged locally for visual clarity while remaining read-only. Save, export, submit, validation, local storage, and history snapshots filter them out and restore the hidden macro edges, so Expand/Collapse and preview repositioning do not mutate the saved workflow. Undo/redo restores authored workflow snapshots and can discard active preview layout because expanded previews are ephemeral.

**Fork** calls the same compile-preview endpoint and materializes that backend-owned compiled group as editable workflow nodes in place of the collapsed aggregation macro. Conditional branch LLM jobs are preserved as editable branch nodes with branch-labeled edges so submit can reconstruct the inline conditional branch config. Forked nodes are normal authored graph state: users can move, edit, save, export, submit, and undo/redo them. This keeps graph compilation and L0 aggregation topology in the backend rather than duplicating it in the frontend.

## Aggregation Methods

Result nodes aggregate outputs from multiple upstream agents. Seven methods available:

| Method | Description | LLM Calls (compiled L0) | Use Case |
|--------|-------------|-----------|----------|
| `collect` | Join outputs with separator | 0 | Simple concatenation |
| `judge` | Single LLM picks winner with visible unanimous fallback selection | 1 top-level (+ conditional repair in preview) | Quick winner selection |
| `scoring` | One scorer prompt per response with visible unanimous fallback selection | N (+1 dynamic rubric) | Rubric-based evaluation |
| `synthesis` | Single LLM combines responses | 1 | Create unified answer |
| `peer_matrix` | Each agent scores all others with visible unanimous fallback selection | N×(N-1) (+1 dynamic rubric) | Robust cross-evaluation |
| `majority_vote` | Extract discrete answers, count votes | 0 top-level (+ conditional synthesis when `tie_breaker_method=synthesis`) | Zero-cost consensus voting |
| `debate_decide` | Group by answer camps, judge decides with visible single-camp/no-camps fallback selection | 1 top-level (+ up to 2 conditional branch jobs in preview) | Camp-based adjudication |

Builder expansion displays static compiled prompt-job counts, including conditional branch prompt jobs. These counts describe the compiled DAG shape; they do not change the current runtime behavior described above.

### collect (default)

Concatenates all inputs with a configurable separator.

```go
config: {
    "separator": "\n---\n"  // default
}
```

### judge

Single LLM evaluates all responses and picks a winner. The compiled L0 workflow also computes a visible unanimous fallback node so the selected output remains deterministic and inspectable when every input has the same extracted answer.

```go
config: {
    "judge_model": "deepseek/deepseek-v4-flash",
    "prompt": "Pick the best response...",
    "extraction_strategy": "regex",
    "extraction_pattern": "(?im)(?:^|\\n)\\s*(?:final answer|answer)\\s*(?:is)?\\s*[:\\-\\s]\\s*(?:\\(?([A-Z])\\)?\\s+(?:is|was|are|because|since|as|therefore|so)\\b|\\(?([^\\n)]{1,80}?)\\)?\\s*(?:[.;]\\s*$|$|\\n|[.;]\\s+(?:because|since|as|this|the|it)\\b|,?\\s+(?:because|since|as|therefore|so)\\b))"
}
```

### scoring

Single LLM scores each response against a rubric. The compiled L0 workflow also computes a visible unanimous fallback node; evaluator prompt nodes remain part of the fixed candidate DAG.

```go
config: {
    "scoring_model": "deepseek/deepseek-v4-flash",
    "prompt": "Question: {{question}}\n\nResponse:\n{{response}}\n\n{{rubric}}\nScore this response.",
    "rubric": "Accuracy, completeness, clarity...",
    "rubric_mode": "dynamic",  // Optional; prompt must include {{rubric}} when dynamic
    "extraction_strategy": "regex",
    "extraction_pattern": "(?im)(?:^|\\n)\\s*(?:final answer|answer)\\s*(?:is)?\\s*[:\\-\\s]\\s*(?:\\(?([A-Z])\\)?\\s+(?:is|was|are|because|since|as|therefore|so)\\b|\\(?([^\\n)]{1,80}?)\\)?\\s*(?:[.;]\\s*$|$|\\n|[.;]\\s+(?:because|since|as|this|the|it)\\b|,?\\s+(?:because|since|as|therefore|so)\\b))"
}
```

Score ties are resolved deterministically by candidate ID in the score reducer. A separate visible tie-policy branch for scoring and peer-matrix reductions is not implemented in this milestone.

### synthesis

Single LLM synthesizes all responses into one combined answer.

```go
config: {
    "model": "openai/gpt-4o",
    "prompt": "Combine these responses..."
}
```

### peer_matrix

Each agent (as reviewer) scores every other agent's response using that agent's own model. Creates N×(N-1) evaluations with true evaluator diversity — different models catch different flaws. The default prompt enforces blind review — evaluators never see their own response. A custom `eval_prompt` can opt in to non-blind evaluation by including `{{reviewer_answer}}`. The compiled L0 workflow also computes a visible unanimous fallback node; evaluator prompt nodes remain part of the fixed candidate DAG.

```go
config: {
    "rubric_model": "z-ai/glm-5",         // Model for dynamic rubric generation (required for rubric_mode=dynamic)
    "eval_prompt": "{{rubric}}\nScore 1-10...", // Must include {{rubric}} when rubric_mode=dynamic
    "normalization": "none",               // Raw scores (default)
    "max_parallel": 6,                     // Concurrent evaluations
    "rubric_mode": "dynamic",             // "dynamic" = LLM-generated task-specific rubric; omit for static default
    "extraction_strategy": "regex",
    "extraction_pattern": "(?im)(?:^|\\n)\\s*(?:final answer|answer)\\s*(?:is)?\\s*[:\\-\\s]\\s*(?:\\(?([A-Z])\\)?\\s+(?:is|was|are|because|since|as|therefore|so)\\b|\\(?([^\\n)]{1,80}?)\\)?\\s*(?:[.;]\\s*$|$|\\n|[.;]\\s+(?:because|since|as|this|the|it)\\b|,?\\s+(?:because|since|as|therefore|so)\\b))"
}
```

**Model routing:** Each evaluation is routed to the reviewing agent's own model (read from `__model` workflow context keys). Routing is strict: every reviewer must have a model, and validation rejects peer_matrix setups that cannot provide per-reviewer models. A separate `rubric_model` controls dynamic rubric generation. Dynamic scoring and peer prompts must include `{{rubric}}`; otherwise validation rejects the config because the generated rubric would be unused.

**Execution flow:**
1. Collect N candidate responses from upstream agents (with model info from `__model` context keys)
2. Extract answers and compute a visible unanimous fallback
3. Optionally generate a dynamic task-specific rubric (if `rubric_mode: "dynamic"`, using `rubric_model`)
4. Plan N×(N-1) evaluation tasks (each agent reviews all others, no self-review)
5. Execute evaluations in parallel (each routed to reviewer's own model); each returns per-criterion reasoning + scores
6. Compute deterministic weighted score per evaluation from rubric weights (fallback: single holistic score for backward compat)
7. Average weighted scores per candidate
8. Select winner with highest score, using the unanimous fallback when configured to prefer it

**Unique compiled node IDs:** Each evaluation is logged with a stable compiled ID such as `{anchor}--review-{reviewer}-{candidate}`. Legacy hidden aggregation rows used `__agg__` subcall IDs; compiled DAG node IDs never use `__`.

### majority_vote

Extracts discrete answers from each agent's output and selects the majority/plurality winner. Zero LLM cost unless a tie triggers synthesis fallback.

```go
config: {
    "extraction_strategy": "regex",           // "regex" (default), "first_letter", "json_field"
    "extraction_pattern": "(?im)(?:^|\\n)\\s*(?:final answer|answer)\\s*(?:is)?\\s*[:\\-\\s]\\s*(?:\\(?([A-Z])\\)?\\s+(?:is|was|are|because|since|as|therefore|so)\\b|\\(?([^\\n)]{1,80}?)\\)?\\s*(?:[.;]\\s*$|$|\\n|[.;]\\s+(?:because|since|as|this|the|it)\\b|,?\\s+(?:because|since|as|therefore|so)\\b))",  // Custom regex (for "regex" strategy)
    "tie_breaker_method": "synthesis",        // "synthesis" (LLM fallback), "first", "error"
    "tie_breaker_model": "deepseek/deepseek-v4-flash", // Required when tie_breaker_method="synthesis"
    "tie_breaker_temperature": 0.0,                  // Required when tie_breaker_method="synthesis"
}
```

**Execution flow:**
1. Parse extraction config from aggregation config
2. Extract discrete answer from each agent's output using configured strategy
3. Count votes — select plurality winner
4. On tie: apply `tie_breaker_method` policy (synthesis delegates to `SynthesisAggregator` using explicit `tie_breaker_model` + `tie_breaker_temperature`; "first" returns first agent's response; "error" returns an error)
5. Return winning agent's full response + agreement metadata

### debate_decide

Groups agent responses into "camps" by extracted answer, then uses a single judge call to pick the winning camp. The compiled L0 workflow also computes a visible single-camp fallback node for unanimous agreement.

```go
config: {
    "judge_model": "deepseek/deepseek-v4-flash", // Model for judge call
    "extraction_strategy": "regex",           // Same extraction config as majority_vote
    "extraction_pattern": "(?im)(?:^|\\n)\\s*(?:final answer|answer)\\s*(?:is)?\\s*[:\\-\\s]\\s*(?:\\(?([A-Z])\\)?\\s+(?:is|was|are|because|since|as|therefore|so)\\b|\\(?([^\\n)]{1,80}?)\\)?\\s*(?:[.;]\\s*$|$|\\n|[.;]\\s+(?:because|since|as|this|the|it)\\b|,?\\s+(?:because|since|as|therefore|so)\\b))",
}
```

**Execution flow:**
1. Extract discrete answers from all agents (reuses `ExtractAnswer`)
2. Group into "camps" by answer
3. Single camp → visible fallback winner
4. Multiple camps → build anonymized camp briefs, one judge call picks winning camp
5. Return first agent from winning camp + agreement metadata

### Agreement Metadata

All aggregation methods populate agreement metadata on the result:

```go
type AggregationResult struct {
    Output          string                    // Winner's response
    Method          AggregationMethod         // Aggregation method used
    Winner          string                    // Winning agent ID
    Scores          map[string]float64        // Final scores per agent
    Reasoning       string                    // Summary of evaluation
    EvalMatrix      *EvaluationMatrix         // Full evaluation details (peer_matrix only)
    AgreementRatio  float64                   // Fraction of agents agreeing with winner (0.0-1.0)
    ConsensusAnswer string                    // The consensus answer (extracted)
    DissentingIDs   []string                  // Agent IDs that disagreed
}
```

- `majority_vote` and `debate_decide`: naturally produce agreement data from vote counting
- `judge`, `scoring`, `peer_matrix`: compute agreement after winner selection via best-effort answer extraction; compiled L0 workflows also expose visible unanimous fallback operation nodes, but evaluator prompt nodes remain scheduled and visible in the compiled DAG
- `synthesis`: computes agreement before the LLM synthesis call
- `collect`: no agreement signal (fields remain at zero values)

## Database Schema

**jobs (selected columns):**
```sql
id, query, model, status, result_text,
tokens_input, tokens_output, tokens_total, cost,
error_message, retry_count, workflow_id,
parent_execution_id, idempotency_key, request_hash,
last_event_sequence, config_hash, workflow_execution_id,
run_id, run_number, previous_run_id, dag_snapshot, dag_hash,
created_at, updated_at, archived_at
```

**workflow_node_execution_attempts:**
```sql
id, job_id, execution_id, run_id, node_id, node_type, attempt_number, status,
activity_id, started_at, completed_at, latency_ms,
tokens_input, tokens_output, cost, error_code, error_message,
metadata, execution_uid, created_at, updated_at
```

**workflow_node_executions:**
```sql
id, job_id, execution_id, run_id, node_id, node_type, node_order, status,
node_label, node_name, prompt, model, output,
tokens_input, tokens_output, cost, latency_ms,
error_message, error_code, metadata, execution_uid, attempt_number,
activity_id, started_at, completed_at, parent_node_id, created_at, updated_at
```

Note: `node_order` is assigned per `(job_id, run_id)` execution.
Compiled L0 aggregation internals use the existing `parent_node_id` grouping
model: expanded internal nodes persist `parent_node_id` as the terminal compiled
result node ID (for example, `aggregation--result`). Their `metadata` also
includes `aggregation_group_node_id` plus source workflow provenance keys such as
`source_workflow_id`, `source_workflow_hash`, `source_node_id`, and
`source_parent_node_id`; benchmark and admin views use this projection to
attribute evaluator latency/cost to the aggregation row instead of solver rows.

**workflows:**
```sql
id, name, description, definition (JSON), created_at, updated_at
```

**api_keys:**
```sql
id, user_id, name, prefix, key_hash, workflow_id,
requests_per_minute, tokens_per_minute,
created_at, last_used_at, revoked_at
```

API key plaintext is returned only from key creation responses. Runtime auth
extracts the displayed prefix, narrows lookup by `prefix`, then compares the
stored SHA-256 hash using constant-time comparison. `workflow_id` optionally
locks a key to one workflow route.

**api_model_routes:**
```sql
api_model, mode, workflow_id, provider_model, description,
is_default, enabled, created_at, updated_at
```

`mode` is `workflow` or `direct_model`. Enabled default routes are unique.
Unknown requested model names intentionally fall back to the default enabled
route; missing default route returns model-not-found.

**api_usage:**
```sql
id, request_id, key_id, user_id, endpoint, requested_model, resolved_model,
workflow_id, job_id, status, http_status, stream,
tokens_input, tokens_output, tokens_total, cost, latency_ms,
error_code, error_message, created_at, completed_at
```

Usage records are written for successful API calls and authenticated failures
that occur after request normalization/routing, including rate-limit failures.
Background Response cancellation is recorded with status `cancelled` so
operator reports can distinguish user cancellation from provider/server
failures. Workflow-routed calls report aggregate workflow token/cost totals.

**api_idempotency:**
```sql
id, key_id, idempotency_key, request_fingerprint, job_id,
response_body, http_status, created_at, expires_at
```

Non-streaming Chat Completions and Responses with `Idempotency-Key` replay the
stored response body for matching request fingerprints and return conflict on
key/body mismatch. Background Responses first replay the accepted
`in_progress` object, then replay the final stored object after completion.
Streaming requests are not byte-replayed.

**api_openai_objects:**
```sql
id, object_type, key_id, user_id, endpoint, job_id,
requested_model, resolved_model, workflow_id, status,
store, background, metadata_json, request_json, response_json, usage_json,
error_code, error_message, previous_response_id,
created_at, updated_at, completed_at
```

Stores OpenAI-compatible public resource objects such as persisted Responses
and stored Chat Completions. Reads are always scoped by `key_id`; a different
API key receives `404`, not a cross-tenant visibility signal.

**api_openai_items:**
```sql
id, object_id, item_kind, item_index, openai_item_id, role,
content_json, raw_json, created_at
```

Stores ordered input/output/message items for `GET /v1/responses/{id}/input_items`
and `GET /v1/chat/completions/{id}/messages`.

**job_events** (stream resilience):
```sql
id, version, job_id, sequence, event_type, timestamp, payload, created_at
```
Indexed by `(job_id, sequence)` for efficient replay on reconnection.

**side_effect_outbox** (tool-call durability):
```sql
tool_call_id, job_id, node_id, payload, status, attempts, max_attempts,
next_attempt_at, last_error, created_at, updated_at
```
Outbox pattern for durable side effects (tool calls, webhooks).

**Indexes:**
- `idx_jobs_status`, `idx_jobs_created_at`
- `idx_jobs_idempotency_key` (unique partial)
- `idx_workflow_node_executions_order`
- `idx_workflow_node_executions_job_node`
- `idx_workflow_node_execution_attempts_job_node_attempt`
- `idx_workflow_node_execution_attempts_run_status`
- `idx_job_events_job_node`
- `idx_api_keys_prefix`
- `idx_api_model_routes_single_default`
- `idx_api_usage_*` filters for key/model/status/request/job
- `idx_api_idempotency_key`, `idx_api_idempotency_expires`
- `idx_api_openai_objects_*` filters for key/type/status/model/job
- `idx_api_openai_items_*` filters for object/item ordering

## API Endpoints

### Workflow Management

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/workflows` | List all saved workflow definitions |
| POST | `/api/workflows` | Create a new workflow definition |
| GET | `/api/workflows/{id}` | Get workflow definition by ID |
| PUT | `/api/workflows/{id}` | Update workflow definition |
| DELETE | `/api/workflows/{id}` | Delete workflow definition |
| GET | `/api/workflows/seeds` | Get available seed workflow templates |

### Workflow Execution

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/workflows/execute` | Execute workflow synchronously (blocking) |
| POST | `/api/workflows/submit` | Submit workflow for async execution (returns job_id) |
| POST | `/api/workflows/validate` | Validate workflow without executing |
| POST | `/api/workflows/compile-preview` | Compile a workflow or workflow_file into a read-only Builder preview DAG |

`POST /api/workflows/compile-preview` accepts either `workflow` (runtime workflow shape) or `workflow_file` (Builder file shape), plus optional `input_values`. It returns the compiled DAG `nodes`/`edges` and `aggregation_groups[]` metadata for each expanded aggregation anchor, including `input_node_ids`, `node_ids`, `presentation_result_id`, `llm_job_count`, `top_level_llm_job_count`, `conditional_llm_job_count`, `conditional_llm_jobs[]`, and `operation_count`.

Workflow Builder's **Expand** action uses this endpoint to render a read-only, in-place compiled aggregation preview. The backend owns compilation and job enumeration; the frontend maps the returned nodes/edges into a top-to-bottom React Flow layout, frames the expanded aggregation as a draggable container, and filters preview-only nodes, edges, and temporary layout shifts from save/export/submit. When an expanded preview needs more vertical space, downstream parent workflow nodes are shifted ephemerally so preview edges continue to read top-to-bottom; **Collapse** restores the authored workflow positions. **Fork** also uses this endpoint, then materializes the returned compiled aggregation group as normal editable builder nodes and branch-labeled conditional edges.

For UI verification, use the frontend test suite and targeted browser checks for Builder render paths, expanded-preview layout, and save/export behavior.

### Job Operations

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/jobs` | List jobs with pagination (cursor-based) |
| GET | `/api/jobs/{id}` | Get job details and nodes |
| GET | `/api/jobs/{id}/trace` | Get trace spans grouped by node |
| GET | `/api/jobs/{id}/config` | Get normalized run config snapshot |
| GET | `/api/jobs/{id}/workflow` | Get the executable workflow snapshot used by a job |
| GET | `/api/jobs/{id}/diff/{id2}` | Diff normalized configs between two jobs |
| POST | `/api/jobs/pause-all` | Pause all pending root jobs |
| POST | `/api/jobs/resume-all` | Resume paused jobs |
| POST | `/api/jobs/cancel-all` | Cancel pending/running/paused jobs |
| POST | `/api/jobs/{id}/cancel` | Cancel a pending/running/paused job |
| POST | `/api/jobs/{id}/resume` | Resume a paused job |
| WS | `/api/jobs/{id}/stream` | Stream job execution events (snapshot + replay + live) |

`GET /api/jobs/{id}/workflow` is for ad-hoc runs whose `workflow_id` may not
exist in saved workflow storage. It returns `{job_id, workflow_id, workflow,
source, saved_workflow_exists}` where `workflow` is the executable runtime
workflow. Source precedence is `request_data` first, then `dag_snapshot`; the
endpoint returns `404` when neither field contains a workflow snapshot. The
builder route `/workflow/from-job/{id}` uses this endpoint to open a job
snapshot for editing. Saving from the builder creates a normal saved workflow
definition, and prompts before overwriting if another saved workflow already
exists with the same ID.

### OpenAI-Compatible API

This section is the implementation reference. For user-facing usage,
examples, and compatibility notes, see [Public API](public-api.md).

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/v1/models` | List enabled OpenAI-facing model routes |
| GET | `/v1/models/{model}` | Retrieve one enabled OpenAI-facing model route |
| POST | `/v1/chat/completions` | OpenAI-compatible Chat Completions request |
| GET | `/v1/chat/completions` | List stored Chat Completions for the API key |
| GET | `/v1/chat/completions/{completion_id}` | Retrieve one stored Chat Completion |
| GET | `/v1/chat/completions/{completion_id}/messages` | List stored Chat Completion messages |
| POST | `/v1/responses` | OpenAI-compatible Responses request |
| GET | `/v1/responses/{response_id}` | Retrieve one stored Response |
| GET | `/v1/responses/{response_id}/input_items` | List stored Response input items |
| POST | `/v1/responses/{response_id}/cancel` | Cancel a pending/running background Response job |

All `/v1/*` endpoints require `Authorization: Bearer <api-key>`. API keys are
created through the admin API or `conctl api key-create`. Stored object reads
and cancellation are scoped to the authenticating API key and return `404` for
objects owned by other keys.
Per-key request/token rate limits are enforced in-process by the backend
instance that receives the request; they are not shared across horizontally
scaled replicas and reset on process restart. Deploy an external/shared limiter
before running multiple API-serving replicas for the same keyspace.
OpenAI-compatible requests are also subject to a lightweight process-local
pre-auth limiter keyed by client IP before API-key storage lookup, and bearer
tokens larger than 4096 bytes are rejected.

#### Compatibility Matrix

| Area | Status | Notes |
|------|--------|-------|
| `/v1/models` list/retrieve | Supported | Lists enabled OpenAI-facing model routes. |
| Chat text messages | Supported | Text-only messages are normalized into workflow prompts. |
| Responses text input | Supported | Text and text content parts are supported. Image, file, audio, and hosted-tool inputs are rejected. |
| Non-streaming Chat/Responses | Supported | Executes the routed Consortium workflow through the configured route. |
| Chat `stream:true` | Supported with limits | Lifecycle/final-content SSE: opens immediately, emits keepalives, and sends final text after workflow completion. It is not provider token streaming. |
| Responses `stream:true` | Supported with limits | Emits typed Responses events around final workflow output, including buffered direct-model function-call argument events. It is not provider token streaming, live argument-delta streaming, or durable stream resume. |
| Responses `background:true` | Supported with limits | Requires storage. The durable job is submitted before the in-progress response is returned; retrieve/cancel and the periodic server sweeper reconcile stale in-progress objects when the durable job is already terminal. Poll retrieve or cancel by response ID. |
| Responses `background:true` + `stream:true` | Rejected with `400` | Deferred until OpenAI SSE events are persisted and `starting_after` resume is implemented. |
| Chat stored resources | Supported with limits | Only created when `store:true`. |
| Responses stored resources | Supported with limits | Non-streaming and background Responses default to `store:true`; streaming Responses are not stored as retrievable resources. |
| `store:false` | Supported with limits | Suppresses OpenAI-compatible public resources and successful idempotency replay bodies only; workflow jobs, usage, and audit records still persist. |
| `previous_response_id` | Supported with limits | Same-key stored Responses can carry prior text and direct-model function-call items into a follow-up request. Reasoning-state carryover and native provider chat/tool-message replay remain out of scope. |
| Function tools | Partially supported | Function tool definitions pass through on direct-model routes. Direct-model Responses return provider tool calls as typed `function_call` output items, and `function_call_output` input must include a matching `call_id` from either the current input or replayed `previous_response_id` chain. Workflow routes suppress internal provider tool calls in the public OpenAI response. Hosted tools are rejected. |
| Structured outputs | Pass-through with routing guardrail | `response_format` and Responses `text.format` are forwarded to compatible routes. `json_schema` requests add OpenRouter `provider.require_parameters:true` unless route metadata already explicitly controls it; schema adherence still depends on provider/model support. |
| Metadata | Accepted with limits | Max 16 pairs, key length <= 64 characters, string value length <= 512 characters. |
| Prompt cache fields | Accepted as metadata/pass-through | No Consortium cache guarantee; effects depend on provider/runtime telemetry. |
| Caller provider routing | Rejected with `400` | Public `/v1` callers cannot override provider/order/fallback/privacy routing until route/key policy exists. Operators can still encode provider routing in workflow definitions. |
| No-op SDK fields | Accepted as no-op | Examples: `n:1`, `logprobs:false`, `verbosity`, `service_tier`, `user`, `safety_identifier`, Chat `modalities:["text"]`, Responses `truncation:"disabled"`. |
| Unsupported value-changing fields | Rejected with `400` | Examples: `n>1`, `logprobs:true`, `top_logprobs>0`, `prediction`, caller provider routing, Chat audio/modalities beyond text, deprecated Chat `functions`/`function_call`, Responses `include`, `truncation:auto`, `context_management`, `max_tool_calls`, `conversation`, `moderation`, hosted/non-function tools. |
| Out-of-scope OpenAI APIs | Out of scope | Files, Vector Stores, Batches, Embeddings, Moderation endpoint parity, Realtime, Responses WebSocket, org/project/admin APIs, hosted OpenAI tools. |

Chat Completions accepts text-only `messages`, `temperature`, `top_p`,
`max_completion_tokens`/`max_tokens`, `stop`, `seed`, `tools`, `tool_choice`,
`parallel_tool_calls`, `response_format`, metadata within the limits above, and
`session_id`.
Accepted message roles are `system`, `developer`, `user`, `assistant`, `tool`,
and `function`; `system`/`developer` messages become workflow system guidance,
while `tool`/`function` messages are text transcript compatibility only, not
typed tool-loop state.
Compatibility fields `n: 1`, `logprobs: false`, `reasoning_effort`,
`verbosity`, `service_tier`, `user`, `safety_identifier`,
and `modalities:["text"]` are accepted; `parallel_tool_calls` is forwarded
when present; `n > 1`, logprobs, prediction, caller-supplied provider routing
(`provider`, `order`, `allow_fallbacks`, `require_parameters`), audio output,
deprecated `functions`/`function_call`, and hosted/non-function tools return
explicit `400` errors.
Accepted `reasoning_effort` values are `none`, `minimal`, `low`, `medium`,
`high`, and `xhigh`; route/provider model support still determines whether the
upstream request can honor a selected effort.
`stream: true` returns Chat-Completions-style SSE chunks, sends an immediate
assistant role chunk, uses heartbeat comments while the workflow runs, honors
`stream_options.include_usage`, and terminates with `data: [DONE]`. Stored Chat
Completion resources are created only when `store: true`. Streaming is buffered
workflow streaming: the SSE connection opens immediately and sends keepalives,
but final model text is emitted after the routed workflow completes rather than
as provider token deltas or function-call argument deltas.

Responses accepts text/JSON `input`, `instructions`, `temperature`, `top_p`,
`max_output_tokens`, `stop`, `seed`, `tools`, `tool_choice`,
`parallel_tool_calls`, `response_format`, Responses `text.format`,
`reasoning.effort`, `previous_response_id`, `session_id`, metadata within the
limits above, `prompt_cache_key`, and
`prompt_cache_retention` values `in_memory`, `in-memory`, or `24h`.
`truncation:"disabled"` is accepted as a no-op. `stream: true` returns typed
Responses SSE events including `response.created`, `response.in_progress`,
`response.output_item.added`, `response.content_part.added`,
`response.output_text.delta`, `response.output_text.done`,
`response.content_part.done`, `response.output_item.done`, and
`response.completed`. Direct-model function
calls use `response.output_item.added`,
`response.function_call_arguments.delta`,
`response.function_call_arguments.done`, `response.output_item.done`, and
`response.completed`; the argument delta is buffered and emitted after workflow
completion, not streamed from the upstream provider in real time.
Non-streaming Responses default to `store: true`;
`store: false` disables OpenAI-compatible retrieve/input-items resources but the
underlying workflow job and `api_usage` audit record still persist. Successful
`store:false` responses are not retained in the idempotency replay cache, so a
later retry with the same `Idempotency-Key` can execute again rather than replay
the original generated text. Background Responses return an immediate
`in_progress` object after the durable job has been accepted and finish
asynchronously; retrieve also reconciles terminal jobs, usage rows, and
idempotency replay state after process restarts. They require storage so
`background: true` with `store: false` is rejected.
`background:true` with `stream:true` is rejected until stream event persistence
and cursor resume are implemented.
Responses streaming uses the same buffered workflow model as Chat Completions:
event frames and heartbeats are live, while output text or function-call
arguments arrive after workflow completion. It is not provider token streaming,
live function-call argument streaming, or background stream resume. Streaming
Responses are not stored as retrievable `/v1/responses/{id}` resources; use
non-streaming or background Responses when retrieve/input-items APIs are
required.

`previous_response_id` loads the completed stored Response chain owned by the
same API key, oldest to newest, and prepends stored input/output items as
context for the new run. Replay is capped at 20 hops to prevent unbounded prompt
growth. For direct-model Responses, provider tool calls are stored as typed
`function_call` output items. Follow-up `function_call_output` input items
preserve their `call_id` and must match a `function_call.call_id` from either
the current input or the replayed chain. Consortium still renders this typed
state into deterministic workflow text at the provider boundary using
`ASSISTANT_FUNCTION_CALL ...` and `FUNCTION_CALL_OUTPUT ...` transcript lines;
it is not native OpenAI conversation-state execution, reasoning-state carryover,
or provider chat/tool-message replay.
Clients that manage context manually may also pass `function_call` plus
`function_call_output` items directly in Responses `input`; mixed manual arrays
of text/message items and function items are preserved as separate stored input
items and rendered through the same deterministic transcript bridge. This is
still a text workflow boundary, not native provider typed-message replay.
Responses `conversation` and inline `moderation` are explicitly unsupported.
Responses `include`, `truncation` modes other than `disabled`,
`context_management`, and `max_tool_calls` are also rejected because the current
runtime cannot honor their semantics.

Route modes:
- `direct_model` builds an in-memory single-prompt workflow that routes to the
  configured provider model through the existing provider registry.
- `workflow` executes the stored workflow with `system_prompt` and
  `user_prompt` context values. Request controls that can be enforced at an LLM
  boundary are merged onto terminal prompt nodes for that execution. Optional
  tools are ignored when no terminal prompt exists; forced tool choice or
  structured response format is rejected in that case. Provider tool calls made
  inside workflow routes are treated as internal workflow metadata and are not
  exposed as public Chat `tool_calls`; a terminal workflow step that returns
  only tool calls and no text can therefore produce an empty public message until
  the typed tool-loop milestone is implemented.

M12 prompt cache fields are compatibility pass-through controls. They do not
guarantee Consortium-owned prompt cache behavior until the provider-aware prompt
caching milestone. `prompt_cache_key` and retention are stored as provider
metadata only when the routed provider honors them.

OpenRouter provider telemetry is captured on workflow node metadata when the
upstream response provides it. Persisted node metadata can include
`provider_request_id`, `provider_response_id`, `provider_generation_id`,
`openrouter_service_tier`, `openrouter_cached_tokens`,
`openrouter_cache_write_tokens`, `openrouter_reasoning_tokens`,
`openrouter_is_byok`, and opt-in response `openrouter_metadata` when request
metadata `openrouter_metadata_enabled` is true. Provider-native error
details such as `provider_error_type`, `provider_native_code`, and
`provider_code` are retained for operator diagnosis, while public
OpenAI-compatible errors still use Consortium's stable mapped error codes. Chat
usage `prompt_tokens_details.cached_tokens` and
`completion_tokens_details.reasoning_tokens`, and Responses
`input_tokens_details.cached_tokens` and
`output_tokens_details.reasoning_tokens`, are populated from this compact
workflow metadata when available.

For `response_format.type:"json_schema"` and Responses
`text.format.type:"json_schema"`, Consortium sets OpenRouter
`provider.require_parameters:true` internally so OpenRouter avoids providers
that would ignore structured-output parameters. Public `/v1` callers still
cannot send provider routing controls directly; workflow/model-route operator
metadata remains the place to encode explicit provider policy. If an operator
route already sets `require_parameters`, that explicit value is preserved.

### Admin API

#### Overview + Jobs

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/admin/overview` | Overview metrics + recent activity |
| GET | `/api/admin/db-diagnostics` | SQLite pool, query-trace, queue, and worker diagnostics (`?tables=true` includes hot-table row counts) |
| GET | `/api/admin/jobs` | Admin job list |
| GET | `/api/admin/jobs/{id}` | Admin job detail |
| GET | `/api/admin/jobs/{id}/nodes` | Job node list for admin views |
| GET | `/api/admin/jobs/{id}/node-execution-attempts` | Node execution attempts |
| GET | `/api/admin/jobs/{id}/agent-runs` | Agent runs for a job |
| POST | `/api/admin/jobs/{id}/agent-runs/{agentRunID}/stop` | Stop an active Novomo-backed agent or Superagent run |
| POST | `/api/admin/jobs/pause-all` | Pause pending root jobs |
| POST | `/api/admin/jobs/resume-all` | Resume paused jobs |
| POST | `/api/admin/jobs/cancel-all` | Cancel pending/running/paused jobs |
| POST | `/api/admin/jobs/{id}/cancel` | Cancel single job |
| POST | `/api/admin/jobs/{id}/resume` | Resume single paused job |
| POST | `/api/admin/jobs/{id}/retry` | Submit a fresh retry run from stored request snapshot |
| POST | `/api/admin/jobs/{id}/archive` | Archive a job |
| POST | `/api/admin/jobs/{id}/unarchive` | Unarchive a job |

`GET /api/admin/jobs/{id}` includes `WorkflowSaved` in its job detail payload.
When false, `Job.workflow_id` is only an execution snapshot identifier; admin UI
links to `/workflow/from-job/{id}` instead of `/admin/workflows/{workflow_id}`.

#### Workflows

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/admin/workflows` | Admin workflow list |
| GET | `/api/admin/workflows/{id}` | Admin workflow detail |
| GET | `/api/admin/workflows/{id}/export` | Export workflow definition JSON |
| PUT | `/api/admin/workflows/{id}` | Update workflow definition JSON |
| POST | `/api/admin/test/workflow` | Run admin workflow test execution |

#### OpenAI-Compatible API Management

Admin API endpoints are trusted-operator controls. By default the server binds
to `127.0.0.1:$PORT`. Non-loopback binds require `ADMIN_API_TOKEN` or explicit
`ALLOW_UNAUTH_ADMIN=true`; when `ADMIN_API_TOKEN` is set, `/api/admin/*`
requires `Authorization: Bearer <token>` or `X-Admin-Token`.

OpenAI-compatible API usage rows are append-only in M12. Stored public resource
objects and their ordered items are retained until a future pruning/retention
policy is introduced or operators remove data directly; object rows are updated
in place for lifecycle transitions such as in-progress to completed, failed, or
cancelled. User-cancelled background Responses are tracked as `cancelled` usage
rows rather than generic failures.
`store: false` suppresses `api_openai_objects`/`api_openai_items` creation and
successful idempotency replay-body retention; it does not erase job, node
execution, provider accounting, or `api_usage` audit data. Typed function-call
arguments and function-call outputs can still appear in those internal
operational records when they are part of the submitted workflow request.
Caller `Idempotency-Key` values are stored as endpoint-scoped SHA-256 storage
keys rather than raw caller tokens. Active request completion/attach operations
target the immutable idempotency reservation row ID so a late completion cannot
overwrite a later reused logical key. Successful replay bodies are retained for
24 hours unless the request explicitly set `store:false`; expired idempotency
rows are pruned opportunistically when new idempotency reservations are made.

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/admin/api-keys` | List API keys; excludes plaintext and hash |
| POST | `/api/admin/api-keys` | Create API key; returns plaintext once |
| DELETE | `/api/admin/api-keys/{id}` | Soft revoke API key |
| GET | `/api/admin/api-usage` | Usage rows plus summary; filters by time/key/model/endpoint/status |
| GET | `/api/admin/api-usage/export` | CSV usage export with formula-cell escaping |
| GET | `/api/admin/api-metrics` | OpenAI-compatible API metrics summary, status buckets, and stale lifecycle counters |
| GET | `/api/admin/model-routes` | List model routes |
| POST | `/api/admin/model-routes` | Create or update model route |
| PUT | `/api/admin/model-routes/{model}` | Create or update model route by path model |
| DELETE | `/api/admin/model-routes/{model}` | Delete model route |

#### Benchmarks

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/admin/benchmarks` | Benchmark runs list |
| GET | `/api/admin/benchmarks/compare` | Compare benchmark runs |
| GET | `/api/admin/benchmarks/compare-items` | Item-level regressions/improvements |
| GET | `/api/admin/benchmarks/runner-status` | Benchmark runner status |
| POST | `/api/admin/benchmarks/import` | Import benchmark artifacts |
| POST | `/api/admin/benchmarks/run` | Start benchmark run |
| POST | `/api/admin/benchmarks/run/cancel` | Cancel benchmark run |
| GET | `/api/admin/benchmarks/dataset-flags` | List dataset flags |
| POST | `/api/admin/benchmarks/dataset-flags` | Create/upsert dataset flags |
| PATCH | `/api/admin/benchmarks/dataset-flags/{id}/resolve` | Resolve a dataset flag |
| DELETE | `/api/admin/benchmarks/dataset-flags/{id}` | Hard delete a dataset flag |
| GET | `/api/admin/benchmarks/{id}` | Benchmark run detail |
| GET | `/api/admin/benchmarks/{id}/analysis` | Wrong-answer analysis + diagnostics |
| GET | `/api/admin/benchmarks/{id}/items` | Benchmark item detail (query form) |
| GET | `/api/admin/benchmarks/{id}/items/{itemID}` | Benchmark item detail (path form) |
| POST | `/api/admin/benchmarks/{id}/rerun-failures` | Rerun failed/incorrect items |
| POST | `/api/admin/benchmarks/{id}/replay-items` | Replay selected items with seed reuse |

#### Optimization

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/admin/optimize/runs` | List optimization runs |
| POST | `/api/admin/optimize/runs` | Create optimization run |
| GET | `/api/admin/optimize/runs/{id}` | Optimization run detail |
| POST | `/api/admin/optimize/runs/{id}/pause` | Pause run |
| POST | `/api/admin/optimize/runs/{id}/resume` | Resume run |
| POST | `/api/admin/optimize/runs/{id}/cancel` | Cancel run |
| PATCH | `/api/admin/optimize/runs/{id}/heartbeat` | Lease heartbeat |
| PATCH | `/api/admin/optimize/runs/{id}/progress` | Persist generation progress |
| PATCH | `/api/admin/optimize/runs/{id}/status` | Patch run status |
| GET | `/api/admin/optimize/runs/{id}/organisms` | List run organisms |
| POST | `/api/admin/optimize/runs/{id}/organisms` | Create run organism |
| GET | `/api/admin/optimize/runs/{id}/organisms/{orgID}` | Organism detail (scoped by run) |
| GET | `/api/admin/optimize/runs/{id}/lineage` | Run lineage DAG |
| GET | `/api/admin/optimize/runs/{id}/learning-log` | Run learning log |
| POST | `/api/admin/optimize/runs/{id}/learning-log` | Append learning entry |
| POST | `/api/admin/optimize/runs/{id}/promote` | Promote best organism |
| GET | `/api/admin/optimize/compare` | Batch compare optimization runs |
| GET | `/api/admin/optimize/organisms/{orgID}` | Organism detail |
| PATCH | `/api/admin/optimize/organisms/{orgID}/fitness` | Patch organism fitness |
| GET | `/api/admin/optimize/organisms/{orgID}/lineage` | Organism ancestry lineage |
| GET | `/api/admin/optimize/organisms/{orgID}/param-changes` | Param-change audit trail |
| POST | `/api/admin/optimize/organisms/{orgID}/param-changes` | Persist param-change records |
| GET | `/api/admin/optimize/organisms/{orgID}/mutation-artifacts` | Mutation artifacts |
| POST | `/api/admin/optimize/organisms/{orgID}/mutation-artifacts` | Persist mutation artifacts |

#### Admission + Benchloop Observability

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/admin/admission` | Admission gate state (`accepting`/`paused`) |
| POST | `/api/admin/admission/pause` | Pause root admission |
| POST | `/api/admin/admission/resume` | Resume root admission |
| GET | `/api/admin/benchloop/status` | Benchloop status/state snapshot |
| GET | `/api/admin/benchloop/transcript` | Benchloop transcript view |
| GET | `/api/admin/benchloop/log` | Benchloop iteration log |
| GET | `/api/admin/benchloop/memory` | Benchloop memory file |
| GET | `/api/admin/benchloop/archive/{sessionID}` | Archived benchloop session |

### Legacy Endpoints (Deprecated)

| Method | Endpoint | Replacement |
|--------|----------|-------------|
| GET | `/api/workflows/execute/ws` | Removed (410 Gone). Use `/api/workflows/submit` + `/api/jobs/{id}/stream` |

## WebSocket Event Contract

### Event Types

All WebSocket events follow a canonical schema defined in `pkg/api/events.go`.

| Event Type | Description | Terminal |
|------------|-------------|----------|
| `status` | General status update (connection established, workflow starting) | No |
| `job_created` | New job created and ready for execution | No |
| `node_start` | Workflow node started execution | No |
| `node_complete` | Workflow node completed successfully | No |
| `node_failed` | Workflow node failed | No |
| `node_retry_start` | Node retry attempt starting | No |
| `node_retry_backoff` | Node waiting before retry | No |
| `node_retry_exhausted` | All retry attempts exhausted | No |
| `paused` | Execution paused by operator action | No |
| `complete` | Entire workflow completed successfully | Yes |
| `error` | Workflow-level error (not node-specific) | Yes |
| `cancelled` | Workflow cancelled by user request | Yes |

Additional agent/memory diagnostic events are also emitted when agent runtime features are active (for example `agent_tool_called`, `agent_iteration_started`, `memory_read`, `retrieval_executed`).

### Job Status Values

Jobs transition through these states:

| Status | Description |
|--------|-------------|
| `pending` | Job created but not yet started |
| `running` | Job currently executing |
| `paused` | Job queued but paused by operator/system admission control |
| `completed` | Job finished successfully |
| `failed` | Job finished with an error |
| `cancelled` | Job cancelled by user request |

Admission semantics:
- Admission pause blocks new root submissions only; child submissions stay allowed.
- `resume` / `resume-all` only change job status. They do not reopen admission.
- Admission is reopened explicitly via admin admission resume control.

Operator controls (`conctl`):
- `conctl admission status` — check whether root admission is `accepting` or `paused`.
- `conctl jobs retry --id <job-id> --yes --admission-bypass` — run a one-off probe while admission remains paused.
- `conctl admission resume --yes` — reopen root admission after validation.

### Event Schema

All events include these base fields:

```typescript
interface WebSocketEvent {
  type: string;           // Required: One of the event types above
  job_id: string;         // Required: Job identifier
  message: string;        // Required: Human-readable description
  timestamp: string;      // Required: ISO 8601 timestamp

  // Node-related (node_start, node_complete, node_failed)
  node_id?: string;       // Node identifier
  output?: string;        // Node output (node_complete)

  // Error-related (error, node_failed, cancelled)
  error?: string;         // Error message
  code?: string;          // Machine-readable error code

  // Metrics (node_complete, complete)
  tokens_input?: number;  // Input tokens used
  tokens_output?: number; // Output tokens generated
  cost?: number;          // Cost in USD
  latency_ms?: number;    // Latency in milliseconds

  // Additional data
  data?: Record<string, any>;  // Event-specific metadata
}
```

### Error Codes

Machine-readable error codes for structured error handling:

| Code | Description |
|------|-------------|
| `INVALID_WORKFLOW` | Workflow validation failed; submit responses return `details` as a post-compile validation string |
| `CYCLE_DETECTED` | Workflow contains circular dependencies |
| `INVALID_MODEL` | Unknown or unavailable model |
| `INVALID_JSON` | Malformed request payload |
| `EXECUTION_FAILED` | General execution failure |
| `NODE_FAILED` | Node execution failed |
| `EXECUTION_TIMEOUT` | Execution exceeded time limit |
| `COST_LIMIT_EXCEEDED` | Cost budget exhausted |
| `TOKEN_LIMIT_EXCEEDED` | Token budget exhausted |
| `CANCELLED` | Cancelled by user request |
| `JOB_NOT_FOUND` | Job ID not found |
| `JOB_NOT_RUNNING` | Cannot cancel non-running job |
| `POOL_EXHAUSTED` | Admission pool at capacity |
| `ADMISSION_PAUSED` | Root admission paused (systemic terminal failure/manual pause) |

### Node Metadata

Node events include metadata for display purposes:

| Field | Type | Description |
|-------|------|-------------|
| `node_type` | string | Type of node (`prompt`, `result`, `conditional`, `operation`, `workflow_ref`, `child_workflow`, `contract_extract`, `agent_run`, `novo_run`) |
| `node_label` | string | Short label for display (e.g., "Claude") |
| `node_name` | string | Full name (e.g., "Claude Response") |
| `node_description` | string | Optional description |
| `node_index` | int | 0-based index in execution order |
| `node_total` | int | Total number of nodes |
| `aggregation_method` | string | For result nodes: `collect`, `judge`, `scoring`, `synthesis`, `peer_matrix`, `majority_vote`, `debate_decide` |
| `source_workflow_id` | string | Referenced workflow that produced a compiled node, such as `aggregation-synthesis` |
| `source_workflow_hash` | string | Frozen hash of the referenced source workflow at submit time |
| `source_node_id` | string | Node ID inside the referenced source workflow before prefixing |
| `source_parent_node_id` | string | Collapsed parent/anchor node that owns this compiled source group |
| `aggregation_group_node_id` | string | Parent grouping key for compiled aggregation internals |

Example `node_start` event:
```json
{
  "type": "node_start",
  "job_id": "abc-123",
  "node_id": "agent-claude",
  "message": "Starting Claude",
  "data": {
    "sequence": 3,
    "node_label": "Claude",
    "node_name": "Claude Response",
    "node_type": "prompt",
    "node_index": 0,
    "node_total": 4
  }
}
```

### Event Sequence Numbers

Every event includes a `sequence` number in its `data` field for tracking and reconnection:

```json
{
  "type": "node_complete",
  "job_id": "...",
  "node_id": "...",
  "data": {
    "sequence": 5,
    "node_label": "Claude",
    "node_name": "Claude Response"
  }
}
```

Track the sequence on the client to enable reconnection:
```javascript
let lastSequence = 0;
ws.onmessage = (e) => {
  const event = JSON.parse(e.data);
  if (event.data?.sequence) {
    lastSequence = event.data.sequence;
  }
};
```

### WebSocket Close Codes

| Code | Name | Description |
|------|------|-------------|
| 4000 | Normal completion | Job completed successfully |
| 4001 | Failed | Job failed with error |
| 4002 | Cancelled | Job cancelled by user |
| 4003 | Server shutdown | Server is shutting down |
| 4004 | Reconnect required | Client too slow, reconnect with `resume_from` |
| 4005 | Server at capacity | Admission pool exhausted; retry with backoff |

## Two-Node Execution Flow

The recommended pattern for executing workflows with real-time updates:

### Node 1: Submit Workflow

```http
POST /api/workflows/submit
Content-Type: application/json

{
  "workflow": { ... workflow definition ... },
  "idempotency_key": "optional-unique-key"
}
```

Response:
```json
{
  "job_id": "uuid-v4",
  "workflow_id": "uuid-v4",
  "status": "pending",
  "duplicate": false
}
```

If `duplicate: true`, the request matched an existing job (via idempotency key or request hash).

### Node 2: Connect to Stream

```javascript
const ws = new WebSocket(`ws://localhost:8080/api/jobs/${jobId}/stream`);

ws.onmessage = (e) => {
  const event = JSON.parse(e.data);
  console.log(event.type, event.node_id, event.data?.sequence);
};
```

### For Completed Jobs

If a job is already complete (status: `completed`/`failed`/`cancelled`), the endpoint returns an HTTP JSON response instead of upgrading to WebSocket:

```json
{
  "job_id": "...",
  "status": "completed",
  "complete": true,
  "message": "Job already finished",
  "snapshot_sequence": 15,
  "result_text": "...",
  "nodes": [...]
}
```

## WebSocket Reconnection

If the WebSocket connection is lost, reconnect with the last known sequence number to avoid duplicate events:

```javascript
const ws = new WebSocket(`ws://localhost:8080/api/jobs/${jobId}/stream?resume_from=${lastSequence}`);
```

### Server Behavior on Reconnect

1. Sends a `snapshot` message with current job and node state
2. Replays any events with `sequence > resume_from`
3. Continues streaming new events

### Snapshot Message

The snapshot includes `snapshot_sequence` - the sequence number at the time of the snapshot. Drop any replayed events with `sequence <= snapshot_sequence`:

```javascript
ws.onmessage = (e) => {
  const event = JSON.parse(e.data);

  if (event.type === 'snapshot') {
    snapshotSequence = event.snapshot_sequence;
    // Update UI with snapshot state
    return;
  }

  // Drop stale events
  if (event.data?.sequence <= snapshotSequence) {
    return;
  }

  // Process event normally
  lastSequence = event.data?.sequence || lastSequence;
};
```

## Job Cancellation

Cancel a pending, running, or paused job:

```http
POST /api/jobs/{id}/cancel
```

Response (success):
```json
{
  "success": true,
  "message": "Job cancellation requested successfully",
  "job_id": "...",
  "event_type": "cancelled"
}
```

Error responses:
- `404` with `JOB_NOT_FOUND`: Job does not exist
- `409` with `JOB_NOT_RUNNING`: Job is not in a cancellable state (`pending`/`running`/`paused`)

Connected WebSocket clients close with code `4002` on cancellation. If cancellation happens during active execution, a `cancelled` event is also emitted.

## Seed Workflows

Pre-configured workflow templates are available via the API:

```http
GET /api/workflows/seeds
```

Response:
```json
{
  "seeds": [
    {
      "id": "reasoning-informed-captain-synthesis",
      "name": "Reasoning Synthesis",
      "description": "Multi-model AI council with synthesis aggregation",
      "aggregation_method": "synthesis",
      "agent_count": 3,
      "layer": "L1"
    },
    {
      "id": "reasoning-judge-pick",
      "name": "Reasoning Judge",
      "description": "Multiple models judged by a single LLM",
      "aggregation_method": "judge",
      "agent_count": 3,
      "layer": "L1"
    }
  ]
}
```

The `layer` field is derived from the seed ID prefix (`aggregation-` → `L0`, `reasoning-` → `L1`, `composite-` → `L2`, `benchmark-` → `L3`, otherwise `""`) by `seeds.Layer()` — the ID prefix is the single source of truth; there is no stored layer column. The same value is exposed as `Layer` on each entry of the admin `GET /api/admin/workflows` response. The Ensemble workflow selector lists only `L1` seeds; the admin Workflows page can filter by layer.

Available seed workflows include reasoning templates and benchmark wrapper templates. Query `/api/workflows/seeds` for the canonical current list.

| Name | Aggregation | Description |
|------|-------------|-------------|
| Reasoning Synthesis | `synthesis` | Multi-model synthesis |
| Reasoning Synthesis CHEAP | `synthesis` | Lower-cost synthesis variant |
| Reasoning Judge | `judge` | Single LLM picks winner |
| Reasoning Judge CHEAP | `judge` | Lower-cost judge variant |
| Reasoning Scored | `scoring` | Score each response against rubric |
| Reasoning Scored CHEAP | `scoring` | Lower-cost scored variant |
| Reasoning Debate | `judge` | Adversarial defense (R1 independent → R2 defend/challenge → judge) |
| Reasoning Debate CHEAP | `judge` | Lower-cost adversarial defense variant |
| Reasoning Peer Review | `peer_matrix` | Cross-evaluation matrix |
| Reasoning Peer Review CHEAP | `peer_matrix` | Lower-cost peer review variant |
| Reasoning Vote | `majority_vote` | Zero-cost majority voting |
| Reasoning Vote CHEAP | `majority_vote` | Lower-cost voting variant |
| Reasoning Camp Debate | `debate_decide` | Camp-based adjudication |
| Reasoning Camp Debate CHEAP | `debate_decide` | Lower-cost camp debate variant |
| Reasoning Self-Consistency | `majority_vote` | Same model 3x at temp 0.5, majority vote |
| Reasoning Self-Consistency CHEAP | `majority_vote` | Lower-cost self-consistency variant |
| Reasoning Deliberation | `majority_vote` | Two-round debate with majority vote |
| Reasoning Deliberation CHEAP | `majority_vote` | Lower-cost deliberation variant |
| Benchmark wrappers (`benchmark-*`) | varies | Parent wrappers for benchmark harness contracts |

## Best Practices

### Use JobManager

```go
// CORRECT
manager := jobs.NewManager(storage, registry)
result, err := manager.ExecuteWorkflow(ctx, wf)

// WRONG - no DB tracking
executor.Execute(ctx, wf, nil)
```

### Use LLM Client

```go
// CORRECT
llmClient := providers.NewClient(registry, nodeLogger)
resp, err := llmClient.Complete(ctx, req, &providers.CompletionContext{JobID: jobID})

// WRONG - no logging
resp, err := registry.Complete(ctx, req)
```

### UUIDs

```go
import "github.com/google/uuid"
workflowID := uuid.New().String()
```

### Error Handling

```go
result, err := manager.ExecuteWorkflow(ctx, wf)
if err != nil {
    log.Printf("Failed: %v", err)
    return err
}
if !result.Success {
    log.Printf("Workflow failed: %s", result.Error)
}
```

### Streaming

```go
resp, err := manager.SubmitWorkflow(ctx, &jobs.SubmitWorkflowRequest{
    Workflow:    wf,
    ForceNewRun: true,
})
if err != nil {
    return err
}
log.Printf("Submitted job: %s", resp.JobID)
// Clients subscribe via WS /api/jobs/{id}/stream
```

### Archive Old Jobs

```go
jobs, _ := storage.ListExecutionsByStatus("completed", 1000)
for _, job := range jobs {
    if time.Since(job.CreatedAt) > 30*24*time.Hour {
        storage.ArchiveExecution(job.ID)
    }
}
```

### Monitor Execution

- Admin Panel: http://localhost:8080/admin
- Failed jobs: `SELECT * FROM jobs WHERE status='failed'`
- Node logs: `SELECT * FROM workflow_node_executions WHERE job_id='xxx'`

## Examples

See full examples in [pkg/workflow/types.go](pkg/workflow/types.go).

**Simple sequential:**
```go
wf := &workflow.Workflow{
    ID: uuid.New().String(),
    Nodes: []*workflow.Node{
        {ID: uuid.New().String(), Type: "prompt", Model: "deepseek/deepseek-v4-flash", Prompt: "Research {{topic}}"},
        {ID: uuid.New().String(), Type: "prompt", Model: "deepseek/deepseek-v4-flash", Prompt: "Summarize {{node_0}}"},
    },
    Context: {"topic": "AI"},
}
result, err := manager.ExecuteWorkflow(ctx, wf)
```

**Parallel analysis (using edges):**
```go
wf := &workflow.Workflow{
    Nodes: []*workflow.Node{
        {ID: "tech", Type: "prompt", Prompt: "Tech analysis {{product}}"},
        {ID: "biz", Type: "prompt", Prompt: "Biz analysis {{product}}"},
        {ID: "ux", Type: "prompt", Prompt: "UX analysis {{product}}"},
        {ID: "synth", Type: "prompt", Prompt: "Synthesize: {{tech}} {{biz}} {{ux}}"},
    },
    Edges: []*workflow.Edge{
        {Source: "input", Target: "tech"},
        {Source: "input", Target: "biz"},
        {Source: "input", Target: "ux"},
        {Source: "tech", Target: "synth"},
        {Source: "biz", Target: "synth"},
        {Source: "ux", Target: "synth"},
    },
    Context: {"product": "AI chatbot"},
}
```

**WebSocket (Two-Node Pattern):**
```javascript
// Node 1: Submit workflow
const response = await fetch('/api/workflows/submit', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ workflow: { id, name, nodes, context } })
});
const { job_id } = await response.json();

// Node 2: Stream execution
const ws = new WebSocket(`ws://localhost:8080/api/jobs/${job_id}/stream`);
let lastSequence = 0;

ws.onmessage = (e) => {
  const event = JSON.parse(e.data);
  if (event.data?.sequence) lastSequence = event.data.sequence;
  // event.type: "status", "node_start", "node_complete", "complete", "error", "cancelled"
};

// On disconnect, reconnect with resume_from
ws.onclose = () => {
  const reconnect = new WebSocket(`ws://localhost:8080/api/jobs/${job_id}/stream?resume_from=${lastSequence}`);
};
```

## Troubleshooting

**Jobs stuck pending:**
```bash
make backend-restart     # Restart server
```

**Timeout:**
- Increase context timeout in executor

**DB locked:**
Already WAL + busy_timeout. If persists: `dsn := fmt.Sprintf("%s?_busy_timeout=10000", dbPath)`

**Jobs not in admin:**
MUST use JobManager, not executor directly.

## Error Handling

**API errors:**
```json
{
  "error": "message",
  "code": "INVALID_WORKFLOW|INVALID_JSON|JOB_NOT_FOUND|JOB_NOT_RUNNING|POOL_EXHAUSTED|ADMISSION_PAUSED|INTERNAL_ERROR|...",
  "details": "..."
}
```

**Middleware order:**
1. Recovery (panics)
2. RequestID (tracing)
3. Logger (audit)
4. CORS (headers)

**WebSocket:**
- Read deadline: 30s
- Write deadline: 10s
- Close codes: 4000 (complete), 4001 (failed), 4002 (cancelled), 4003 (shutdown), 4004 (reconnect), 4005 (capacity)

## Summary

Robust LLM workflow orchestration.

**Key points:**
1. JobManager mandatory - auto tracking
2. LLM Client mandatory - auto cost tracking
3. WebSocket - real-time streaming
4. Comprehensive tracking - all requests logged
5. Flexible patterns - sequential, parallel, conditional, output
6. UUID IDs - global uniqueness
7. SQLite - WAL mode, single-file

**Dev workflow:**
```bash
make dev            # Backend + Frontend
# http://localhost:3000 - visual builder
# http://localhost:8080/admin - monitoring
```

**Source:**
- [pkg/workflow/](pkg/workflow/)
- [pkg/jobs/](pkg/jobs/)
- [pkg/providers/](pkg/providers/)
- [pkg/storage/](pkg/storage/)
- [pkg/api/](pkg/api/)
