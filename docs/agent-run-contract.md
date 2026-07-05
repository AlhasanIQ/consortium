# Agent Runtime Contract (Consortium ↔ Novomo)

Joint HTTP contract between Consortium (workflow orchestrator) and Novomo (agent runtime & sandbox). Consortium supports two Novomo-backed workflow node surfaces:

Novomo project references:
- Repo: https://github.com/alhasaniq/novomo
- Site: https://novomo.alhasaniq.com

| Consortium node | Builder label | Novomo primitive | Purpose |
|---|---|---|---|
| `agent_run` | Novomo Agent | `Job` via `/v1/runs` | Current thin harness run contract for Novomo JobAgent jobs. |
| `novo_run` | Superagent | `Task` wake via `/v1/tasks/{id}/wake`, resulting in a `NovoRun` | Wake Novomo's Novo runtime inside a Consortium workflow. |

**Status:** Current spec — block-and-poll against Novomo's current `JobDetail` and `NovoRunDetail` responses, with stop calls for persisted active Novomo-backed runs. Webhook, async completion, stronger idempotency, richer status, and NovoGoal support are deferred.

## Boundary

| | Consortium | Novomo |
|---|---|---|
| Owns | Workflow DAG semantics, retry/budget caps, result storage, variable interpolation, replay/idempotency at the *node* level | Harness runtime, sandbox/container, workspace, MCP tools, LLM provider routing, per-iteration events, mutation log |
| Knows about the other | Novomo as an HTTP service that runs agents and wakes Novo Tasks | Consortium as one of several callers alongside Novomo's own CLI and UI |

The `agent_run` node maps to Novomo's legacy Job API. The `novo_run` node maps to Novomo's Task wake API. Consortium deliberately does not launch NovoGoals yet; a Superagent node runs one Novo wake session, not a goal graph.

## Opaque Handoff / Inheritance

Both Novomo-backed node types support an optional opaque handoff envelope. This is how Consortium chains one agent node into another without modeling Novomo workspaces, memory episodes, repo sources, or other runtime internals.

**Explicit handle shape**

```json
{
  "inherit_from": {
    "kind": "job_run",
    "id": "01JR...",
    "policy": "latest"
  }
}
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `kind` | string | yes | One of `job_run`, `job`, `novo_run`, or `task`. |
| `id` | string | yes | Opaque Novomo identifier. Consortium does not inspect the referenced object. |
| `policy` | string | no | Opaque Novomo policy selector. Consortium trims and forwards it unchanged. Novomo accepts `default` and the compatibility alias `latest` for the current handoff semantics. |

Workflow nodes can also set `inherit_from_node_id` plus optional `inherit_from_policy`. In that mode Consortium resolves the upstream node from the same workflow run:

- upstream `novo_run` rows become `{kind:"novo_run", id: external_run_id}`
- upstream `agent_run` rows become `{kind:"job_run", id: external_job_run_id}` when Novomo exposes a JobRun ID, otherwise `{kind:"job", id: external_run_id}`

`inherit_from` and `inherit_from_node_id` are mutually exclusive. `inherit_from_node_id` must point to an upstream `agent_run` or `novo_run` node in the DAG. The builder exposes these controls as **Inherit From** on both Novomo Agent and Superagent nodes.

Builder workflow files additionally support `inheritFromMode = "auto"`. This is the default UI mode for Novomo-backed nodes. During conversion, Consortium picks the nearest upstream Novomo-backed ancestor in the graph and emits `inherit_from_node_id`; classical nodes can sit between the producer and consumer. If Auto reaches multiple nearest Novomo-backed ancestors at a fan-in boundary, conversion emits the internal `inherit_from_workflow_task` marker instead of choosing one branch. Runtime resolves that marker to `{kind:"task", id:<workflow Novomo task>}`. `inheritFromMode = "none"` disables handoff even when a graph source exists. Runtime execution snapshots store the deterministic resolved shape rather than rerunning auto selection against edited graph topology.

Consortium stores the submitted/resolved handoff in `agent_runs.inherit_from_json` for restart/debug visibility; Novomo owns all semantics beyond the envelope, including artifact and memory frontiers. Auto mode either selects one upstream Novomo-backed ancestor or, for fan-in, hands off from the workflow-level task so Novomo can resolve the task frontier. Consortium does not merge multiple parent workspaces itself.

Current fan-in limitation: Novomo task handoff materializes a single
task-frontier workspace only when Git can merge the live frontier branches
cleanly. If sibling branches conflict, Novomo fails the submitted node with
`handoff_merge_conflict` and does not create the downstream JobRun. Consortium
surfaces that node failure and preserves the Novomo error message; it does not
pick a latest branch, inspect branch refs, or synthesize a merge workspace.

## Authentication

- Shared bearer token when Novomo auth is enabled. Consortium reads it from `NOVOMO_API_KEY`.
- Consortium sends `Authorization: Bearer <token>` on every request when `NOVOMO_API_KEY` is configured.
- No mTLS. Environment is trusted (same VPC / dev box).

## Agent Run Endpoints

Base URL defaults to `http://localhost:8090` and is configurable per environment via `NOVOMO_URL`. All paths versioned under `/v1`.

### POST /v1/runs — spawn an agent run

**Request**

```json
{
  "prompt": "Summarize the architecture of this codebase in 5 bullets.",
  "harness": "codex",
  "sandbox": "docker",
  "task_id": "consortium-990edebb-5e92-4484-a2d6-4f63614177b8",
  "timeout_seconds": 600,
  "inherit_from": {
    "kind": "job_run",
    "id": "01JR...",
    "policy": "latest"
  }
}
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `prompt` | string | yes | Task description handed to the agent verbatim. Maps to Novomo's `Job.goal`. |
| `harness` | string | yes | Supported Novomo JobAgent harness: `"claude-code"` or `"codex"`. Unknown harnesses are rejected before submission. |
| `sandbox` | string | no | `host` or `docker`. Consortium defaults missing or empty `agent_run` sandbox values to `docker` before submit. Set `host` explicitly for host execution. |
| `task_id` | string | no | Consortium sends a deterministic task id for every `agent_run` in a workflow execution so Novomo can group sibling branches and resolve fan-in task handoffs. Older ad-hoc callers may omit it and let Novomo synthesize a task. |
| `timeout_seconds` | integer | yes | Hard ceiling. Novomo terminates the run if exceeded; status becomes `"failed"` with `error.code = "timeout"`. |
| `inherit_from` | object | no | Opaque handoff envelope described above. |

Novomo runtimes must include the per-job sandbox field before Consortium rolls out this contract; older runtimes that reject unknown JSON fields will return a 4xx when `sandbox` is sent. Because the defaulted sandbox is part of Consortium's frozen canonical node identity, re-converting an existing `workflow_file` `agent_run` without an explicit sandbox will produce a new `dag_hash`.

**Response 200**

```json
{
  "run_id": "01HZ...",
  "job_run_id": "01JR...",
  "status": "pending"
}
```

`status` is `"pending"` or `"running"` immediately; the run is queued or executing. Caller polls `GET /v1/runs/{run_id}` for terminal status.

**Errors**

- `400` — invalid request (unknown harness, missing field, timeout out of range).
- `401` — bad bearer token.
- `409` — handoff conflict or non-terminal handoff source. For task fan-in,
  Novomo may return `handoff_merge_conflict` when frontier branches cannot be
  merged cleanly.
- `503` — Novomo at capacity; caller may retry.

Consortium sends `Idempotency-Key: {workflow_execution_id}:{node_id}:{attempt}` on submit. Current Novomo may ignore this header; until Novomo stores and honors it, a network loss after Novomo creates a run but before Consortium receives `run_id` can still duplicate the external run. Once Consortium has persisted `external_run_id`, replay/resume must poll the existing run instead of submitting again.

### GET /v1/runs/{run_id} — fetch current state

**Response 200 — running**

```json
{
  "job": {
    "id": "01HZ...",
    "status": "running",
    "goal": "Summarize the architecture of this codebase in 5 bullets.",
    "harness": "claude-code",
    "sandbox": "docker",
    "timeout_seconds": 600
  },
  "runs": [
    {
      "id": "01JA...",
      "job_id": "01HZ...",
      "status": "running"
    }
  ]
}
```

**Response 200 — completed**

```json
{
  "job": {
    "id": "01HZ...",
    "status": "completed",
    "summary": "...agent's final text output...",
    "tokens_input": 12450,
    "tokens_output": 3210,
    "cost_usd": 0.184,
    "harness": "claude-code",
    "sandbox": "docker",
    "timeout_seconds": 600,
    "started_at": "2026-05-12T18:11:04Z",
    "finished_at": "2026-05-12T18:18:33Z"
  },
  "runs": []
}
```

**Response 200 — failed**

```json
{
  "job": {
    "id": "01HZ...",
    "status": "failed",
    "error_code": "timeout",
    "error_message": "Agent exceeded timeout_seconds (600).",
    "tokens_input": 8200,
    "tokens_output": 1100,
    "cost_usd": 0.092,
    "harness": "claude-code",
    "sandbox": "docker",
    "timeout_seconds": 600,
    "started_at": "2026-05-12T18:11:04Z",
    "finished_at": "2026-05-12T18:21:04Z"
  },
  "runs": []
}
```

**Primary response shape (iter 1)** — Novomo returns `JobDetail` with top-level `job` and `runs`. Consortium reads `job.id` as `external_run_id`, the latest `runs[].id` as `external_job_run_id` when present, `job.summary` as output, `job.error_code` / `job.error_message` as failure detail, and token/cost fields from `job`.

**Compatibility fallback** — Older mocks or contract tests may use the flat sketch `{run_id,status,output,tokens_input,tokens_output,cost_usd,error}`. Consortium may decode that shape for compatibility, but new Novomo integrations should use `JobDetail`.

**Status enum (iter 1)** — `"pending" | "running" | "completed" | "failed"` at the Novomo job level. Novomo job-run rows can expose richer terminal statuses (`timeout`, `crashed`, `startup_failed`, `cancelled`), but Consortium collapses them to node failure while preserving the specific code in `error_code`. Iter 2 may expose a richer top-level enum.

**Error codes (iter 1)** — at minimum: `"timeout"`, `"crashed"`, `"startup_failed"`, `"unknown"`. Free-form `message` for human reading.

**Errors**

- `404` — unknown `run_id`.
- `409 not_stoppable` — the Novomo Job is already terminal.

### POST /v1/runs/{run_id}/stop — stop an Agent run

Consortium calls this endpoint in two cases:

- best-effort cleanup when a parent workflow context is cancelled after `external_run_id` is persisted
- explicit operator action from the Consortium admin job detail Agent Runs tab or `conctl jobs stop-agent-run`

Novomo marks pending Jobs failed/stopped and cancels active JobRuns. Terminal Jobs return `409 not_stoppable`.

**Response 200**

```json
{
  "run_id": "01HZ...",
  "status": "failed"
}
```

**Errors**

- `404` — unknown `run_id`.
- `409 not_stoppable` — the Novomo Job is already terminal.

### Polling discipline (caller responsibility)

- Consortium polls every **2–5 seconds** while `status == "running"`.
- Consortium enforces its own outer deadline (`timeout_seconds + small grace`) and abandons the poll if Novomo never reaches a terminal state. The Consortium-side node fails with a `NOVOMO_UNRESPONSIVE` error code.
- On caller-side cancellation after an `external_run_id` is persisted, Consortium calls the matching Novomo stop endpoint before marking its local node cancelled.
- Poll reconciliation never downgrades a local terminal row back to `running`; non-terminal poll updates, operator-stop writes, and Consortium-side give-up terminal writes are conditional on the persisted `agent_runs` row still being non-terminal, so the first terminal reconciliation wins races.

## Superagent Novo Run Endpoints

The `novo_run` workflow node is shown as **Superagent** in the builder. It launches a real Novomo `NovoRun` by waking a Task. The user-facing workflow node can either provide an existing `task_id` or let Consortium create a deterministic operator Task for the workflow node attempt.

### GET /v1/tasks/{task_id} — inspect an existing Task

Consortium calls this before creating or waking a Task.

**Response 200**

```json
{
  "task_id": "consortium-abc123",
  "goal": "Investigate the failing workflow.",
  "summary": "Investigate workflow failure",
  "state": "idle",
  "current_novo_run_id": ""
}
```

If the Task exists and is already executing with a `current_novo_run_id`, Consortium adopts that run and polls it instead of sending another wake. If the Task does not exist and the node did not provide `task_id`, Consortium creates it.

### POST /v1/tasks — create an operator Task

Used only when the `novo_run` node does not provide an existing `task_id`.

**Request**

```json
{
  "id": "consortium-abc123",
  "goal": "Investigate the failing workflow.",
  "summary": "Investigate workflow failure"
}
```

The generated ID is deterministic from the node attempt idempotency key so replay before `external_run_id` persistence does not create unbounded Task records. Novomo still owns the Task lifecycle after creation.

### POST /v1/tasks/{task_id}/wake — launch a NovoRun

**Request**

```json
{
  "goal": "Investigate the failing workflow.",
  "identity": "sde-novo",
  "sandbox": "docker",
  "timeout": "10m0s",
  "grace": "10s",
  "inherit_from": {
    "kind": "novo_run",
    "id": "nr_01J...",
    "policy": "latest"
  },
  "repo_specs": [
    {
      "name": "app",
      "source": {
        "type": "host_path",
        "host_path": {
          "path": "/path/to/your/consortium-checkout"
        }
      }
    }
  ],
  "work_source": {
    "type": "gitea_branch",
    "gitea_branch": {
      "branch_ref": "main"
    }
  }
}
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `goal` | string | yes unless existing Task already has usable goal | Interpolated workflow prompt. |
| `identity` | string | no | Defaults to `sde-novo` in Consortium. |
| `sandbox` | string | no | `host` or `docker`. Defaults to `docker` in Consortium when omitted. |
| `image` | string | no | Container image for docker sandbox. |
| `runtime_url` | string | no | Passed through when a Novomo runtime override is required. If omitted by the workflow node, Consortium sends its configured Novomo base URL, defaulting to `http://localhost:8090`, so the wake can call back to the runtime API. For Docker wakes, omitted local URLs using `localhost`, `127.0.0.1`, or `::1` are rewritten to `host.docker.internal`; explicit per-node values are not rewritten. |
| `timeout` | duration string | yes | Derived from `timeout_seconds`. |
| `grace` | duration string | no | Derived from `grace_seconds`. |
| `inherit_from` | object | no | Opaque handoff envelope described above. |
| `repo_specs` | array | no | Opaque Novomo repo specs; the builder stores this as JSON. |
| `work_source` | object | no | Opaque Novomo work source; the builder stores this as JSON. |

Set `sandbox` to `host` explicitly when a workflow needs host execution. Missing or empty values are interpreted as `docker` by Consortium before the wake request is sent.

**Response 200**

```json
{
  "task_id": "task_01J...",
  "novo_run_id": "nr_01J...",
  "status": "running"
}
```

Consortium persists `external_run_id = novo_run_id`, `run_kind = "novo_run"`, `external_task_id = task_id`, and any submitted/resolved handoff JSON in `agent_runs`.

### GET /v1/novo-runs/{novo_run_id} — fetch current NovoRun state

**Response 200**

```json
{
  "novo_run": {
    "id": "nr_01J...",
    "active_task_id": "consortium-abc123",
    "status": "running",
    "summary": "",
    "error_code": "",
    "error_message": "",
    "tokens_input": 0,
    "tokens_output": 0,
    "cost_usd": 0
  },
  "jobs": []
}
```

Consortium reads `novo_run.id` as `external_run_id`, `novo_run.active_task_id` as the live Task pointer when Novomo provides it, `novo_run.summary` as output, and token/cost/error fields from `novo_run`. The durable `external_task_id` is primarily captured from the wake response because Novomo can clear `active_task_id` after terminal Task cleanup.

**Status enum** — `running` and `pending` are non-terminal. `completed` succeeds. `failed`, `timeout`, `startup_failed`, `crashed`, `cancelled`, and `paused` fail the node while preserving the specific code in `error_code`.

### POST /v1/novo-runs/{novo_run_id}/stop — stop a Superagent wake

Consortium calls this endpoint in two cases:

- best-effort cleanup when a parent workflow context is cancelled after the wake response has been persisted as `external_run_id`
- explicit operator action from the Consortium admin job detail Agent Runs tab or `conctl jobs stop-agent-run`

Novomo cancels the live daemon-owned wake, cancels running child JobRuns, fails pending child Jobs, releases the active Task lease, and marks the NovoRun cancelled. Terminal NovoRuns return `409 not_stoppable`.

Consortium uses the same persisted `agent_runs` reconciliation rule for Superagent wakes as for normal agent runs: a local terminal row is never overwritten by a later non-terminal poll response.

**Errors**

- `404` — unknown `novo_run_id`.
- `409 not_stoppable` — the NovoRun is already terminal.

## What's NOT in iter 1 (deliberate cuts)

The following are real concerns but explicitly deferred. Don't add them speculatively; they earn their way in when a concrete need arises.

| Concern | Why deferred | When to revisit |
|---|---|---|
| Webhook / async completion | Adds an `awaiting_external` activity status, completion endpoint, restart reconciliation. Real infra work. | When typical run durations exceed worker-pool tolerance (≥15 min routinely). |
| Strong submit idempotency | Consortium sends `Idempotency-Key`, but current Novomo may not persist/deduplicate it. | When Novomo stores idempotency keys and returns existing `run_id` on duplicate submit. |
| Tool allowlist (`tool_subset`) | Novomo applies its default Novo allowlist for now. | When Consortium needs to restrict tools per node. |
| Workspace internals | Consortium passes `inherit_from`, Superagent `repo_specs`, and Superagent `work_source` as opaque Novomo-owned values. It does not model workspace types, memory episodes, source checkout internals, or branch/materialization policy. | Revisit only if Consortium must route or verify a concrete Novomo artifact type. |
| Budget overrides (`max_tokens`, `max_cost_usd`, `max_iterations`) | Novomo's per-Novo budget applies. Consortium's outer `timeout_seconds` is the only Consortium-enforced cap in iter 1. | When per-node budget tuning becomes necessary. |
| Workspace branch refs in output | Novomo M3 will add Gitea branches. Surfacing `branch_ref` in the response lets downstream nodes consume agent output as code. | When Novomo M3 lands AND a workflow needs to chain on agent code output. |
| Rich status enum | Iter 1 collapses to `running/completed/failed`. Specific failure modes are in `error.code`. | When admin UX needs to distinguish `budget_exceeded` from `crashed` natively. |
| Memory episodes | Novomo's structured per-run summary. Consortium doesn't need it for iter 1; the plain `output` string is enough. | Likely never on Consortium side — Novomo's own UI is the place for memory episodes. |
| NovoGoal launch | The Superagent node wakes a Task and polls one `NovoRun`; it does not create or manage Novomo goal graphs. | When workflows need multi-wake Novo goal orchestration as a first-class node. |

### Known gap: Novomo cost is reported, not metered

`agent_run` and `novo_run` token/cost figures are owned entirely by Novomo. Consortium does **no** pricing or token math for these nodes — `cost_usd`, `tokens_input`, and `tokens_output` are read from the Novomo run response (`pkg/novomo/client.go`) and stored verbatim on the `agent_runs` row. There is no fallback estimate: when Novomo reports `0` or a null `cost_usd` (e.g. a `novo_run`, whose usage fields are nullable), Consortium records `0`.

Two consequences follow, and they are easy to conflate:

- **It IS in the parent job total.** Novomo's `cost_usd`/tokens flow into the node's `NodeResult.Cost`/tokens, get recorded on the durable runtime's `activity_completed` history event, and are summed into `jobs.cost` / `jobs.tokens_total` when `Storage.CompleteExecution` finalizes the run. So per-job and aggregate reporting (admin job detail, workflow stats) **do** include agent-run spend. (This is the durable-runtime history path; the legacy `Executor.Execute` aggregator is not on the live execution path.)
- **It is NOT metered against the workflow cost cap.** The in-workflow limiter (`CostTracker` / `CostLimits`, enforced via `CheckLimits`) is fed in exactly one production place — `providers.Client`, on each successful LLM completion. `agent_run`/`novo_run` runners pass the shared `CostTracker` through but never call `Add`, so a workflow cost limit cannot pre-empt or trip on Novomo spend. Together with the deferred `max_cost_usd` budget override (row above), `timeout_seconds` is the only Consortium-enforced ceiling on an agent run.

**When to revisit:** when workflows must bound agent *spend* (not just wall-clock). Either push a real budget to Novomo via the deferred `max_cost_usd` override (enforced where the spend actually happens), or fold the terminal Novomo `cost_usd` into `CostTracker` once the run completes — noting that a post-hoc add can only cap *subsequent* nodes, not the in-flight agent.

## Iteration 2 (sketch, not yet specified)

When iter 1 hurts:

- `webhook_url` optional field on the spawn request; Novomo POSTs `{run_id, status, …terminal payload…}` to it on terminal. Caller responds 2xx to ack; Novomo retries on non-2xx with backoff.
- Persisted caller-supplied idempotency token; Novomo dedupes within a TTL window and returns the existing `run_id` on replay.
- Optional richer fields in the completion payload as Novomo milestones add them: `workspace_branch_ref`, `mutation_log_summary`, fine-grained `status` enum.

## Glossary

Naming collides between systems; use these mappings consistently in code.

| Concept | Consortium | Novomo |
|---|---|---|
| The workflow orchestration unit | "job" (workflow execution) | n/a |
| A single agent invocation | `agent_run` node attempt; `external_run_id` is the Novomo Job ID | "Job" (`Job.id`) |
| A single container execution of an agent | `external_job_run_id` when Novomo exposes it; usable as `inherit_from.kind = "job_run"` | "JobRun" (`JobRun.id`) |
| A Superagent wake | `novo_run` node attempt; `external_run_id` is the NovoRun ID; `external_task_id` is the Task ID | "NovoRun" (`NovoRun.id`) launched by waking a "Task" (`Task.id`) |

**Rule for Consortium code:** never call Novomo's `Job` a "job" in Consortium user-facing workflow code — use `external_run_id` or a qualified handoff kind (`job`, `job_run`) when an opaque Novomo handle must be passed through. Reserve unqualified "job" for Consortium workflow executions. For Superagent nodes, use `novo_run` for the Consortium node type, "Superagent" for the UI label, and `external_task_id` for the Novomo Task.

## Versioning

`/v1` is the iter 1 surface. Breaking changes require a new prefix (`/v2`) and a sunset window. Additive fields (new optional request fields, new response fields) do not bump the version.
