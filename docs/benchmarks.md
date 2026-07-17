# Benchmark Harness

Benchmark evaluation harness (MCQA + non-MCQA) that runs through the same durable workflow engine as normal jobs.

## Overview

- **Supported benchmarks:** `global-mmlu`, `global-mmlu-lite`, `mmlu-pro`, `math-500`
- **Execution:** `POST /api/admin/benchmarks/run` → `jobs.Manager.ExecuteWorkflowWithReplayOptions(...)` → durable runtime workers
- **Prerequisite:** Backend server must be running (`make backend-bg`)
- **Deadlock safety:** Child workflow submissions bypass admission control; autoscaling workers use `WORKER_INITIAL_COUNT` (default 10) up to `WORKER_COUNT` (default 300, clamped to at least `MAX_CONCURRENT_WORKFLOWS`)
- **List filtering default:** Optimizer-generated runs are hidden by default in benchmark list APIs/UI/CLI unless explicitly included.
- **Model fallback:** LLM-capable nodes that omit `model` use Consortium's platform default, `deepseek/deepseek-v4-flash`.

## Architecture

MCQA benchmark workflows are typically a **parent wrapper** (`benchmark-*`) that calls a **reasoning primitive** (`reasoning-*`) via a `child_workflow` node, then extracts a canonical single-letter answer via a `contract_extract` node.

```
input → child_workflow(reasoning-*) → contract_extract → result(collect)
```

Math wrappers (`benchmark-math-*-cheap`) are thinner and skip `contract_extract`:

```
input → child_workflow(reasoning-*) → result(collect as benchmark_answer)
```

- The `contract_extract` node applies a regex cascade first (zero cost, sub-ms latency), falling back to an LLM call only when all patterns fail. Extraction patterns are configured per-seed in `extractionPatterns`.
- Parent/child linkage persisted via `jobs.parent_execution_id`
- MCQA grading reads only the `benchmark_answer` output key from the parent wrapper
- For non-MCQA (`math-500`), grading accepts `benchmark_answer` when present, otherwise falls back to `final_output`
- Child nodes use `timeoutSeconds: 1440` and contract nodes use `maxTokens: 1024` to avoid timeouts and truncation at high concurrency

### Design Philosophy

Reasoning workflows (`reasoning-*`) are **benchmark-agnostic reusable modules**. They know nothing about MCQA format, option labels, or grading — they take a prompt, run a multi-model reasoning strategy (synthesis, debate, peer review, etc.), and return a natural-language answer.

Benchmark-specific concerns — instruction-following, few-shot examples, output-shaping into a single letter, and grading — live exclusively in the **parent wrapper** (`benchmark-*`). This separation means:

- The same reasoning workflows power both the benchmark harness and the user-facing Ensemble UI
- Adding a new benchmark format (e.g., open-ended, numeric) only requires a new parent wrapper, not changes to reasoning workflows
- Reasoning workflow tuning (model selection, prompts, aggregation) improves both benchmark scores and production quality simultaneously

### Wrapper Matrix

Every `reasoning-*` primitive has a corresponding `benchmark-*` wrapper (with `-cheap` variants). The pattern is mechanical: `benchmark-{strategy}` → `reasoning-{strategy}`.

| Wrapper | Child | Notes |
|---------|-------|-------|
| `benchmark-informed-captain-synthesis` / `-cheap` | `reasoning-informed-captain-synthesis` / `-cheap` | |
| `benchmark-judge-pick` / `-cheap` | `reasoning-judge-pick` / `-cheap` | |
| `benchmark-judge-score-pick` / `-cheap` | `reasoning-judge-score-pick` / `-cheap` | |
| `benchmark-peer-score-pick` / `-cheap` | `reasoning-peer-score-pick` / `-cheap` | |
| `benchmark-adversarial-defense-judge-pick` / `-cheap` | `reasoning-adversarial-defense-judge-pick` / `-cheap` | |
| `benchmark-majority-pick` / `-cheap` | `reasoning-majority-pick` / `-cheap` | |
| `benchmark-camp-split-judge-pick` / `-cheap` | `reasoning-camp-split-judge-pick` / `-cheap` | |
| `benchmark-self-consistency-majority-pick` / `-cheap` | `reasoning-self-consistency-majority-pick` / `-cheap` | |
| `benchmark-multi-round-majority-pick` / `-cheap` | `reasoning-multi-round-majority-pick` / `-cheap` | |

**27 wrapper seeds** total in `pkg/seeds/data/` (18 MCQA + 9 math-500, plus 6 single-model baselines).

Math wrappers (cheap only for now) mirror the cheap variants:
- `benchmark-math-informed-captain-synthesis-cheap`
- `benchmark-math-judge-pick-cheap`
- `benchmark-math-judge-score-pick-cheap`
- `benchmark-math-peer-score-pick-cheap`
- `benchmark-math-adversarial-defense-judge-pick-cheap`
- `benchmark-math-majority-pick-cheap`
- `benchmark-math-camp-split-judge-pick-cheap`
- `benchmark-math-self-consistency-majority-pick-cheap`
- `benchmark-math-multi-round-majority-pick-cheap`
- plus cheap single-model baselines: `benchmark-math-baseline-{grok,mimo,minimax}-cheap`

### Format Conversion

Stored workflows use builder file format (`nodes`, `edges`, `data.config`). Benchmark wrappers remain L3 and invoke L1 reasoning workflows through explicit `child_workflow` nodes. Inside those child L1 jobs, collapsed aggregation nodes with `aggregationWorkflowId` compile to reusable L0 `aggregation-*` internals before the durable DAG is frozen. Direct-model baseline wrappers skip the L1 child boundary and run the solver in the parent workflow.

Retry policy is first-class per executable node. It is not a benchmark layer: L3 describes the wrapper purpose, while retry attempts remain attached to parent wrapper nodes, child workflow nodes, and compiled L0 internals according to each node's policy.

## Data Prep

```bash
python3 -m pip install datasets   # one-time
make bench-data                   # fetches all splits for all benchmarks
```

Writes JSONL to `benchmarks/data/{benchmark_name}/{split}.jsonl`.

## Running Benchmarks

```bash
# Start a run
curl -X POST http://localhost:8080/api/admin/benchmarks/run \
  -d 'benchmarks=global-mmlu-lite&workflows=benchmark-informed-captain-synthesis-cheap&run_set=full&limit=20&concurrency=20'

# Check progress
curl -sS http://localhost:8080/api/admin/benchmarks/runner-status | jq .

# Cancel
curl -X POST http://localhost:8080/api/admin/benchmarks/run/cancel

# Rerun failed items (merges results back into original run)
curl -X POST http://localhost:8080/api/admin/benchmarks/<run_id>/rerun-failures

# Replay specific items with upstream seed reuse from a baseline run
curl -X POST http://localhost:8080/api/admin/benchmarks/<run_id>/replay-items \
  -d 'items=row-17&changed_workflows=reasoning-judge-pick-cheap&mode=required&concurrency=1'
```

`conctl` equivalents:

```bash
./bin/conctl benchmarks run --yes --benchmarks global-mmlu-lite --workflows benchmark-informed-captain-synthesis-cheap --run-set full --limit 20 --concurrency 20
./bin/conctl benchmarks runner-status --wait-until idle
./bin/conctl benchmarks rerun-failures --id <run_id> --yes
./bin/conctl benchmarks rerun-failures --id <run_id> --item row-17 --yes --admission-bypass
./bin/conctl benchmarks replay-items --id <run_id> --items row-17 --changed-workflows reasoning-judge-pick-cheap --mode required --concurrency 1 --yes
./bin/conctl benchmarks list --limit 25 --include-optimizer
```

Aggregation workflow E2E smoke:

```bash
go test ./pkg/e2e ./pkg/admin -run 'TestE2EBenchmarkAttribution|BenchmarksAnalysis' -count=1
bun run test:e2e:builder
bash scripts/e2e-aggregation-workflows.sh
```

The deterministic `TestE2EBenchmarkAttribution*` tests submit real L3 workflows and analyze real parent/child workflow node rows, but seed the benchmark result item rows directly so analysis attribution is repeatable in memory. The live smoke script exercises the actual conctl benchmark runner and verifies runner-preserved item fields, child job linkage, and analysis output against the local server.

### API Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `benchmarks` | *(required)* | Comma-separated benchmark IDs |
| `workflows` | *(required)* | Comma-separated workflow IDs |
| `run_set` | `full` | `full`, `small`/`lite`, or `custom` |
| `split` | — | Required when `run_set=custom` |
| `limit` | `20` | Max items per benchmark (0 = all) |
| `concurrency` | `20` | Max concurrent workflow executions |
| `max_non_letter_retries` | `2` | Retries for contract output failures |
| `max_transient_retries` | `3` | Retries for transient execution/provider failures |
| `source` | `manual` | Run source metadata: `manual`, `benchloop`, `optimizer`, `imported`, `replay` |
| `pause_on_fatal` | `true` | Auto-pause on unrecoverable errors |
| `fatal_repeat_threshold` | `3` | Repeated error signature count before pausing |

**Split mapping:** `full` → `test`, `small`/`lite` → `dev` (or `validation` for mmlu-pro), `custom` → explicit `split` value. For `math-500`, `dev` is a deterministic synthetic subset (first 100 items of `test`) generated by the fetch script.

Work is interleaved round-robin across benchmark/workflow combinations to prevent starvation.

List/query behavior:

- `GET /api/admin/benchmarks` hides runs with `source=optimizer` by default.
- Set `include_optimizer=true` (or `1`) to include optimizer-generated runs.
- `conctl benchmarks list` mirrors this behavior and requires `--include-optimizer` to show optimizer runs.

### Targeted Replay Parameters (`POST /api/admin/benchmarks/{id}/replay-items`)

| Parameter | Default | Description |
|-----------|---------|-------------|
| `items` | *(required)* | Comma-separated item IDs (e.g., `row-17,row-43`) |
| `workflow` | baseline workflow | Optional workflow override for replay run |
| `changed_workflows` | — | Comma-separated changed workflow IDs; parent `child_workflow` nodes targeting these IDs are forced dirty |
| `mode` | `best_effort` | Replay strictness: `best_effort`, `required`, `off` |
| `concurrency` | `1` | Max concurrent replayed items |
| `max_non_letter_retries` | `1` | Contract retry count |
| `max_transient_retries` | `1` | Transient execution retry count |
| `admission_bypass` | `false` | One-off probe while admission is paused (requires exactly one item) |

Replayed (seeded) nodes have zero cost, tokens, and latency — no actual API call is made. Only dirty and downstream nodes execute live. Seeded nodes display `completed [replay]` in conctl and an amber "replayed" badge in the admin panel. Original seed costs are preserved in execution history attributes (`seed_cost`, `seed_tokens_input`, `seed_tokens_output`) for audit.

### Rerun-failures Optional Parameters (`POST /api/admin/benchmarks/{id}/rerun-failures`)

| Parameter | Default | Description |
|-----------|---------|-------------|
| `item` | — | Optional single failed item ID to rerun |
| `admission_bypass` | `false` | One-off probe while admission is paused (requires exactly one item) |

Admission behavior:
- Benchmark `run`, `rerun-failures`, and `replay-items` fail fast with `ADMISSION_PAUSED` when root admission is paused.
- `admission_bypass=true` is only accepted for single-item probe runs.

## Model Guidance Repo (Bench Iteration Support)

Use the benchmark model guidance tool to inspect Artificial Analysis intelligence vs cost-to-run data before model swaps. Generated snapshots are local artifacts and are not committed to Git:

```bash
# Refresh local snapshot from Artificial Analysis and map to OpenRouter IDs
./bin/conctl benchmarks models sync

# Browse or filter models
./bin/conctl benchmarks models list --limit 20 --sort value

# Get quick hint tables
./bin/conctl benchmarks models suggest --top 10

# Inspect one model deeply
./bin/conctl benchmarks models show --model gpt-5-2
```

Notes:
- Default output format is markdown; use `--format json` when machine parsing is needed.
- Snapshot files are written under `benchmarks/models_repo/` (`models_flat.json`, plus `source_raw.json`, which preserves every V2 response page).
- `ARTIFICIAL_ANALYSIS_API_KEY` is required for sync. The default is the Artificial Analysis V2 Free endpoint, which returns model-level data and is paginated. It does not provide host-specific data, reasoning/deprecation flags, blended pricing, token-count breakdowns, context windows, or end-to-end P95 latency; unavailable Boolean flags are explicitly marked with their `*_known=false` companion fields, and unavailable numeric/string fields are omitted. A Pro key may use `--source-url https://artificialanalysis.ai/api/v2/language/models?prompt_type=medium` to populate the documented Pro fields.

## Experimental Benchloop

`benchloop` is experimental operator tooling. It mutates benchmark workflows, shells out to Claude CLI, and runs with broad local permissions. Do not run it in unattended CI or on repositories/workspaces you would not allow that CLI to edit.

## Automated Tuning Loop (`benchloop`)

For iterative tuning (benchmark run -> failure analysis -> workflow edits -> rerun), use the dedicated loop CLI:

```bash
# Dry-run resolved config first
make benchloop ARGS="run --workflows benchmark-informed-captain-synthesis-cheap --run-set custom --split test --item-limit 50 --concurrency 100 --dry-run"

# Archive current loop state for a clean-slate run
./bin/benchloop archive

# Start loop (matrix is operator-defined from CLI flags)
make benchloop ARGS="run --workflows benchmark-informed-captain-synthesis-cheap --run-set custom --split test --item-limit 50 --concurrency 100 --max-iterations 6"

# Resume
make benchloop ARGS="run --resume"

# Inspect in-flight status while loop runs
./bin/benchloop status --watch

# Render full prompt context package (system + user prompts)
./bin/benchloop prompts --phase all --json

# Emulate prompts from CLI flags only (ignore existing lock/state files)
./bin/benchloop prompts --phase all --json --ignore-lock --workflows benchmark-informed-captain-synthesis-cheap --run-set custom --split test --item-limit 50 --concurrency 100
```

Permissions:
- Benchloop always runs Claude in bypass mode (`--permission-mode bypassPermissions --dangerously-skip-permissions`).
- Permission-mode and escalation flags were removed from benchloop.
- Treat the working tree, environment variables, logs, and benchmark artifacts as sensitive while a loop is running.

Model-swap policy:
- Model swaps are disabled by default (`--allow-model-swaps=false`).
- This minimizes evaluation noise while tuning non-model variables (prompts, aggregation behavior, reasoning effort, temperature, timeout, structure).
- Enable `--allow-model-swaps` only when you explicitly want to test model changes.

Split/run-set policy:
- `--run-set custom` requires `--split`.
- Fresh runs require explicit `--split`, `--item-limit`, and `--concurrency` (matrix is no longer agent-proposed).
- If you need `test` split, use `--run-set custom --split test`.
- `--run-set lite` maps to dev/validation only; do not use it for test-split tuning.
- Resume mode auto-migrates legacy `run_set=lite` + `split=test` matrix locks to `run_set=custom`.
- On resume, matrix-defining flags come from `matrix.lock.json`; explicit matrix flags must match the lock.
- Benchloop enforces matrix scope strictly; runs outside the locked benchmark/workflow/split are auto-rolled-back.

Loop safety:
- Benchloop enforces one active run per workspace using `benchmarks/loop/.lock`.
- Stale lock files are auto-cleaned when the recorded PID no longer exists.
- Lock metadata includes PID and loop session ID for debugging/resume workflows.
- `benchloop archive` validates `decision.json` when present and archives full runtime state for clean-slate runs.
- `benchloop prompts` loads `matrix.lock.json` / `state.json` when present; use `--ignore-lock` to render purely from CLI flags.

Agent output format tradeoff:
- `--agent-output-format json` (default): compact logs, stable post-run parsing.
- `--agent-output-format stream-json`: richer event stream in iteration logs for deep debugging, but noisier/larger artifacts.
- Recommended workflow: keep `json` for normal runs, switch to `stream-json` temporarily when diagnosing loop behavior.

Iteration protocol (benchloop agent prompt):
0. Optional model-swap hint check via `conctl benchmarks models suggest` / `conctl benchmarks models show` (only when `--allow-model-swaps` is enabled)
1. Targeted replay of one failing item using `benchmarks replay-items`
2. Sanity run (`--limit 5`)
3. Main small-set run (matrix item limit)

### Debugging a loop run

Use benchloop-native debug commands (high-level transcript output, low token/noise):

```bash
./bin/benchloop status --watch
./bin/benchloop sessions --limit 20
./bin/benchloop transcript
./bin/benchloop transcript --attempt 7
./bin/benchloop transcript --iteration 3
./bin/benchloop transcript --json
./bin/benchloop prompts --phase iteration
```

Notes:
- `benchloop sessions` includes in-flight attempts from the `live` block in `state.json` with `state=running`, even before they are persisted to `session_index.jsonl`.
- For a live attempt, use `./bin/benchloop transcript --session-id <uuid>` (from `status --json` -> `state.live.agent_session_id`).
- With default `--agent-output-format json`, attempt log files may show mostly benchloop heartbeat/metadata lines until the agent exits.

Key artifacts:
- `benchmarks/loop/state.json`
- `benchmarks/loop/session_index.jsonl` (loop attempt -> Claude session mapping)
- `benchmarks/loop/sessions/<loop_session_id>/iterations/*.log` (session-scoped agent logs)
- `benchmarks/loop/.lock` (active-run lock metadata)

`benchloop transcript` resolves transcript locations from the Claude CLI session mapping used by the local operator environment.

## Retry Logic

**Contract failure retries** (up to `max_non_letter_retries`):
- `empty_final_output`, `invalid_contract_output`, `contract_truncated`

**Transient failure retries** (up to `max_transient_retries`, exponential backoff 400ms–3s):
- `provider_failure` (all instances)
- `tool/runtime_failure` when error matches transient markers (timeout, DB-busy, connection reset, stream error)

**Admission exhaustion retries** (up to 120, exponential backoff 250ms–30s):
- `POOL_EXHAUSTED` / admission saturation errors

Costs/tokens/latency from all retries accumulate into the item result.

### Fatal Guard

Detects unrecoverable errors and pauses the run:

**Hard fatal** (immediate pause): `ADMISSION_PAUSED`, `INSUFFICIENT_CREDITS`, `AUTH_ERROR`, `MODEL_NOT_FOUND`, `BAD_REQUEST`, `COST_LIMIT` — plus message-pattern matching for these categories.

**Soft fatal** (repeat-based): Non-retryable error signatures that repeat `fatal_repeat_threshold` times trigger a pause. Signature format: `"{CODE}|{NORMALIZED_MESSAGE}"`.

## Output Contract

MCQA grading uses the parent wrapper canonical key `benchmark_answer`.

**MCQA normalization** (`NormalizeBenchmarkAnswerForChoices`): trim → uppercase → require single letter → validate against item option count.

`math-500` grading uses benchmark-specific parsing (`ParseMathAnswer`) and deterministic equivalence heuristics (`MathAnswersEquivalent`) over extracted final answers (supports exact, fraction/decimal, assignment/value, tuple, and common LaTeX numeric forms like `\frac{...}{...}` and `\pi` variants).

**Fail-closed:**
- Missing/blank canonical value → `empty_final_output`
- Present but not a valid single-letter option → `invalid_contract_output`
- No fallback from `final_output` or any non-canonical field for MCQA

## Failure Taxonomy

Deterministic, first-match-wins precedence (`pkg/bench/failure.go`):

1. `tool/runtime_failure` — DB-busy, job creation failure, workflow conversion error, etc.
2. `provider_failure` — OpenRouter errors, rate limits, provider timeouts, auth errors
3. `benchmark_paused` — run paused by fatal guard
4. `empty_final_output` — canonical key missing or blank
5. `contract_truncated` — contract output truncated (tokens hit max)
6. `invalid_contract_output` — canonical value not a valid single-letter option
7. `multiple_letters` — multiple option labels in output
8. `conflicting_answer` — inconsistent answers across fields
9. `no_letter` — no valid label recoverable
10. `other_parse_error` — everything else

## Result Artifacts

Saved to `benchmarks/results/` and persisted to database.

**`<run_id>.summary.json`:** accuracy, parse rate, latency percentiles (p50/p95/p99), token/cost totals, retry counts, `failure_reason_counts`, per-subject and per-language accuracy breakdowns.

**`<run_id>.json`:** Per-item records. Top-level fields always represent the final attempt only.

| Field | Description |
|-------|-------------|
| `failure_reason` | Failure class from taxonomy above |
| `output_source` | `benchmark_answer`, `final_output`, `final_output_ungraded`, `missing_benchmark_answer`, or `none` |
| `attempt_details[]` | Per-attempt: `job_id`, `raw_output`, `predicted`, `parse_ok`, `failure_reason`, `output_source`, tokens/cost/latency |

Attempt details include contract-node diagnostics for parse failures: `contract_node_id`, `contract_model`, `contract_finish_reason`, `contract_tokens_output`, `contract_max_tokens`, `contract_diagnostic`.

### Usage Semantics (Raw vs Inclusive)

- **Raw usage:** Direct totals from the parent workflow result. Used in per-item artifacts.
- **Inclusive usage:** Parent + child execution totals across the full execution graph. Used in admin views.

The difference represents child workflow cost not directly on the parent result record.

## Admin API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/admin/benchmarks` | List benchmark runs |
| GET | `/api/admin/benchmarks/compare` | Compare runs side-by-side |
| GET | `/api/admin/benchmarks/compare-items` | Item-level regression comparison |
| GET | `/api/admin/benchmarks/runner-status` | Active runner status |
| POST | `/api/admin/benchmarks/import` | Import results from `benchmarks/results/` |
| POST | `/api/admin/benchmarks/run` | Start benchmark run |
| POST | `/api/admin/benchmarks/run/cancel` | Cancel active run |
| GET | `/api/admin/benchmarks/dataset-flags` | List dataset flags (supports `benchmark`, `split`, `active_only`) |
| POST | `/api/admin/benchmarks/dataset-flags` | Create/upsert active dataset flags |
| PATCH | `/api/admin/benchmarks/dataset-flags/{id}/resolve` | Resolve a dataset flag |
| DELETE | `/api/admin/benchmarks/dataset-flags/{id}` | Hard delete a dataset flag |
| GET | `/api/admin/benchmarks/{id}` | Run detail |
| GET | `/api/admin/benchmarks/{id}/analysis` | Wrong-answer analysis + performance + diagnostics |
| GET | `/api/admin/benchmarks/{id}/items/{itemID}` | Item detail (legacy path form) |
| GET | `/api/admin/benchmarks/{id}/items?item_id=...` | Item detail (slash-safe form) |
| POST | `/api/admin/benchmarks/{id}/rerun-failures` | Rerun failed/incorrect items |
| POST | `/api/admin/benchmarks/{id}/replay-items` | Replay selected items with DAG-based upstream reuse |

`/api/admin/benchmarks/{id}/analysis` also returns:
- `agent_models[]`: per-model cost, retries, latency, parse rate, and accuracy over child workflow agent nodes. Direct-model baselines fall back to parent workflow agent nodes because they intentionally have no L1 child job.
- `aggregation_nodes[]`: per-aggregation-node cost, retries, latency, parse rate, and accuracy over compiled L0 aggregation internals. Wrapper-level `collect` nodes marked as benchmark-output packaging are excluded so baseline wrappers are not reported as product aggregation methods.
- `diagnostics.slowest_items[]`: top-N items by total latency, including parent/child latency and retries.
- `diagnostics.most_retries_items[]`: top-N items by retries, including parent vs child retry breakdown.

Optional query parameter:
- `top` (default `10`, max `50`) controls diagnostics list length.

Dataset-flag CLI:

- `conctl benchmarks flag ...` to create flags
- `conctl benchmarks flags ...` to list flags
- `conctl benchmarks unflag ...` to resolve flags

## High-Concurrency Guardrails

For large runs (e.g., `concurrency=150`):

- SQLite single writer with a small WAL read pool (default): `DB_MAX_OPEN_CONNS=4`, `DB_MAX_IDLE_CONNS=4`
- Worker max >= admission capacity (default): `WORKER_COUNT >= MAX_CONCURRENT_WORKFLOWS`
- Startup worker baseline: `WORKER_INITIAL_COUNT=10` (autoscaled upward as the active pool saturates)

**Pre-run checks:**

```bash
curl -sS http://localhost:8080/health
curl -sS http://localhost:8080/system/readiness
./bin/conctl local db-query --sql "SELECT status, COUNT(*) FROM jobs GROUP BY status ORDER BY status;"
```

## Verification Checklist

```bash
make ci                    # full backend + frontend checks
make backend-bg && curl -sS http://localhost:8080/health
./bin/conctl local db-query --sql "SELECT id, name FROM workflows WHERE id LIKE 'benchmark-%' ORDER BY id;"
```

Artifact checks after a run:
- Summary has `failure_reason_counts` with all taxonomy keys
- Each item has `failure_reason`, `output_source`, and `attempt_details`
- Top-level item fields represent final attempt only

## Runbook

### Readiness Gate (before full-split runs)

All checks must pass before committing to an expensive full-split run:

1. **Parse rate >= 0.98** for every workflow in the candidate set
2. **failed_items = 0** on short validation runs (`--run-set lite`)
3. **Retry behavior deterministic** — no flaky retries or spurious failures
4. **No fallback scoring** (MCQA) — grading uses canonical `benchmark_answer` only; math-500 allows `final_output` fallback by design

```bash
# Run validation pass on dev split (all 9 mandatory cheap workflows)
./bin/conctl benchmarks run --yes \
  --benchmarks global-mmlu-lite \
  --workflows benchmark-informed-captain-synthesis-cheap,benchmark-judge-pick-cheap,benchmark-judge-score-pick-cheap,benchmark-peer-score-pick-cheap,benchmark-adversarial-defense-judge-pick-cheap,benchmark-majority-pick-cheap,benchmark-camp-split-judge-pick-cheap,benchmark-self-consistency-majority-pick-cheap,benchmark-multi-round-majority-pick-cheap \
  --run-set lite --limit 0 --concurrency 20 \
  --wait --interval 5s

# Verify gate criteria
./bin/conctl benchmarks list --limit 9
# All workflows should show: parse_rate = 1.0, failed_items = 0
```

### Full-Split Run

After gate pass and human approval of spend envelope:

```bash
# Check system health and admission state
./bin/conctl system health
./bin/conctl admission status
./bin/conctl system readiness

# Submit full test split (400 items x 9 workflows = 3,600 items)
./bin/conctl benchmarks run --yes \
  --benchmarks global-mmlu-lite \
  --workflows benchmark-informed-captain-synthesis-cheap,benchmark-judge-pick-cheap,benchmark-judge-score-pick-cheap,benchmark-peer-score-pick-cheap,benchmark-adversarial-defense-judge-pick-cheap,benchmark-majority-pick-cheap,benchmark-camp-split-judge-pick-cheap,benchmark-self-consistency-majority-pick-cheap,benchmark-multi-round-majority-pick-cheap \
  --run-set full --limit 400 --concurrency 150

# Monitor progress
./bin/conctl benchmarks runner-status --diagnostics
./bin/conctl benchmarks runner-status --wait-until idle --interval 30s

# Review results
./bin/conctl benchmarks list --limit 9
```

### Debugging Failures

```bash
# Find incorrect items
./bin/conctl benchmarks get --id <run-id> --incorrect

# Categorize failures
./bin/conctl benchmarks analysis --id <run-id> --top 10

# Filter by failure category
./bin/conctl benchmarks get --id <run-id> --incorrect --category some_right_child_wrong

# Deep investigation
./bin/conctl benchmarks drill --id <run-id> --item <item-id>
./bin/conctl benchmarks explain --id <run-id> --item <item-id>

# View frozen DAG (parent or child)
./bin/conctl benchmarks dag --id <run-id> --item <item-id>
./bin/conctl benchmarks dag --id <run-id> --item <item-id> --child

# Targeted replay after workflow edits
./bin/conctl benchmarks replay-items --id <run-id> --items <item-id> \
  --changed-workflows <workflow-id> --mode required --yes --concurrency 1
./bin/conctl benchmarks runner-status --wait-until idle

# Export failures for offline analysis
./bin/conctl benchmarks export --id <run-id> --incorrect --output failures.jsonl
```

### Admission Recovery

When admission auto-pauses due to provider errors:

```bash
# Check gate state
./bin/conctl admission status

# Probe with one item while gate stays paused
./bin/conctl benchmarks rerun-failures --id <run-id> --item <item-id> --yes --admission-bypass

# If probe succeeds, reopen admission
./bin/conctl admission resume --yes
```
