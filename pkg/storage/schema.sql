-- Jobs table: stores LLM job requests and their results
CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    query TEXT NOT NULL,
    model TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('pending', 'running', 'paused', 'completed', 'failed', 'cancelled', 'archived')),
    request_data TEXT, -- JSON of full request
    response_data TEXT, -- JSON of full response
    result_text TEXT, -- Quick access to result text
    tokens_input INTEGER DEFAULT 0,
    tokens_output INTEGER DEFAULT 0,
    tokens_total INTEGER DEFAULT 0,
    cost REAL DEFAULT 0.0,
    error_message TEXT,
    retry_count INTEGER DEFAULT 0,
    workflow_id TEXT, -- Reference to workflow definition
    parent_execution_id TEXT, -- For parent/child linkage
    idempotency_key TEXT, -- Client-provided key to prevent duplicate submissions
    request_hash TEXT, -- Hash of workflow request for deduplication window
    user_id TEXT, -- Nullable, for user-scoped dedup
    last_event_sequence INTEGER DEFAULT 0, -- Atomic sequence counter for job_events
    config_hash TEXT, -- Deterministic workflow config fingerprint
    workflow_execution_id TEXT, -- Stable logical execution identity (durable runtime)
    run_id TEXT, -- Per-run identity (durable runtime)
    run_number INTEGER DEFAULT 1, -- 1-based run counter within an execution
    previous_run_id TEXT, -- Previous run ID on rollover
    dag_snapshot TEXT, -- Frozen canonical workflow JSON
    dag_hash TEXT, -- SHA256 of dag_snapshot
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    archived_at DATETIME, -- When the job was archived
    -- Durable runtime fields must be consistently ALL present or ALL absent.
    CHECK (
        (COALESCE(dag_hash, '') = '' AND COALESCE(dag_snapshot, '') = '' AND COALESCE(workflow_execution_id, '') = '' AND COALESCE(run_id, '') = '')
        OR
        (COALESCE(dag_hash, '') != '' AND COALESCE(dag_snapshot, '') != '' AND COALESCE(workflow_execution_id, '') != '' AND COALESCE(run_id, '') != '')
    )
);

-- Workflow node executions table: latest execution projection per node per run.
CREATE TABLE IF NOT EXISTS workflow_node_executions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id TEXT NOT NULL,
    execution_id TEXT NOT NULL, -- Stable logical execution identity
    run_id TEXT NOT NULL, -- Per-run identity
    node_id TEXT NOT NULL,
    node_type TEXT NOT NULL,
    node_order INTEGER NOT NULL, -- Execution order
    status TEXT NOT NULL,
    node_label TEXT, -- Human-readable label (e.g., "Claude")
    node_name TEXT, -- Full name (e.g., "Claude Response")
    prompt TEXT,
    model TEXT,
    output TEXT,
    tokens_input INTEGER DEFAULT 0,
    tokens_output INTEGER DEFAULT 0,
    cost REAL DEFAULT 0.0,
    latency_ms REAL DEFAULT 0.0,
    error_message TEXT,
    error_code TEXT,
    metadata TEXT DEFAULT '{}', -- JSON metadata
    execution_uid TEXT, -- Deterministic: job_id:node_id:attempt
    attempt_number INTEGER DEFAULT 1, -- Attempt number (1-based)
    activity_id TEXT, -- Durable runtime activity correlation
    started_at DATETIME, -- When node execution started
    completed_at DATETIME, -- When node execution completed
    parent_node_id TEXT, -- References another node_id in same job (aggregation child nodes)
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE CASCADE,
    UNIQUE(job_id, run_id, node_id)
);

-- Workflow node execution attempts table: retry/attempt history per node.
CREATE TABLE IF NOT EXISTS workflow_node_execution_attempts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id TEXT NOT NULL,
    execution_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    node_type TEXT NOT NULL,
    attempt_number INTEGER NOT NULL, -- Attempt number (1-based)
    status TEXT NOT NULL,
    activity_id TEXT, -- Durable runtime activity correlation
    started_at DATETIME,
    completed_at DATETIME,
    latency_ms REAL DEFAULT 0.0,
    tokens_input INTEGER DEFAULT 0,
    tokens_output INTEGER DEFAULT 0,
    cost REAL DEFAULT 0.0,
    error_code TEXT,
    error_message TEXT,
    metadata TEXT DEFAULT '{}', -- JSON metadata
    execution_uid TEXT, -- Deterministic: job_id:node_id:attempt
    node_label TEXT,
    node_name TEXT,
    prompt TEXT,
    model TEXT,
    output TEXT,
    parent_node_id TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE CASCADE,
    UNIQUE(job_id, run_id, node_id, attempt_number)
);

-- Workflows table: stores workflow definitions
CREATE TABLE IF NOT EXISTS workflows (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT,
    definition TEXT NOT NULL, -- JSON of complete workflow file format
    version INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- OpenAI-compatible API key management.
CREATE TABLE IF NOT EXISTS api_keys (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL DEFAULT 'system',
    name TEXT NOT NULL,
    prefix TEXT NOT NULL UNIQUE,
    key_hash TEXT NOT NULL UNIQUE,
    workflow_id TEXT,
    requests_per_minute INTEGER NOT NULL DEFAULT 60,
    tokens_per_minute INTEGER NOT NULL DEFAULT 120000,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_used_at DATETIME,
    revoked_at DATETIME
);

-- OpenAI-compatible API model-to-workflow route mapping.
CREATE TABLE IF NOT EXISTS api_model_routes (
    api_model TEXT PRIMARY KEY,
    mode TEXT NOT NULL CHECK(mode IN ('workflow', 'direct_model')),
    workflow_id TEXT,
    provider_model TEXT,
    description TEXT,
    is_default INTEGER NOT NULL DEFAULT 0,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- OpenAI-compatible API request usage.
CREATE TABLE IF NOT EXISTS api_usage (
    id TEXT PRIMARY KEY,
    request_id TEXT NOT NULL,
    key_id TEXT NOT NULL,
    user_id TEXT NOT NULL DEFAULT 'system',
    endpoint TEXT NOT NULL,
    requested_model TEXT,
    resolved_model TEXT,
    workflow_id TEXT,
    job_id TEXT,
    status TEXT NOT NULL,
    http_status INTEGER,
    stream INTEGER NOT NULL DEFAULT 0,
    tokens_input INTEGER NOT NULL DEFAULT 0,
    tokens_output INTEGER NOT NULL DEFAULT 0,
    tokens_total INTEGER NOT NULL DEFAULT 0,
    cost REAL NOT NULL DEFAULT 0,
    latency_ms REAL NOT NULL DEFAULT 0,
    error_code TEXT,
    error_message TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME
);

-- OpenAI-compatible Idempotency-Key replay metadata.
CREATE TABLE IF NOT EXISTS api_idempotency (
    id TEXT PRIMARY KEY,
    key_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_fingerprint TEXT NOT NULL,
    job_id TEXT,
    response_body TEXT,
    http_status INTEGER,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL,
    UNIQUE(key_id, idempotency_key)
);

-- OpenAI-compatible public resource objects.
CREATE TABLE IF NOT EXISTS api_openai_objects (
    id TEXT PRIMARY KEY,
    object_type TEXT NOT NULL,
    key_id TEXT NOT NULL,
    user_id TEXT NOT NULL DEFAULT 'system',
    endpoint TEXT NOT NULL,
    job_id TEXT,
    requested_model TEXT,
    resolved_model TEXT,
    workflow_id TEXT,
    status TEXT NOT NULL,
    store INTEGER NOT NULL DEFAULT 1,
    background INTEGER NOT NULL DEFAULT 0,
    metadata_json TEXT DEFAULT '{}',
    request_json TEXT DEFAULT '{}',
    response_json TEXT DEFAULT '{}',
    usage_json TEXT DEFAULT '{}',
    error_code TEXT,
    error_message TEXT,
    previous_response_id TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME
);

-- OpenAI-compatible ordered resource items.
CREATE TABLE IF NOT EXISTS api_openai_items (
    id TEXT PRIMARY KEY,
    object_id TEXT NOT NULL,
    item_kind TEXT NOT NULL,
    item_index INTEGER NOT NULL,
    openai_item_id TEXT,
    role TEXT,
    content_json TEXT DEFAULT '{}',
    raw_json TEXT DEFAULT '{}',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (object_id) REFERENCES api_openai_objects(id) ON DELETE CASCADE,
    UNIQUE(object_id, item_kind, item_index)
);

-- Index for fast job lookups
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
CREATE INDEX IF NOT EXISTS idx_jobs_overview_stats_cover ON jobs(status, cost, tokens_total);
CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_jobs_workflow_id ON jobs(workflow_id);
CREATE INDEX IF NOT EXISTS idx_jobs_workflow_created_at ON jobs(workflow_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_jobs_workflow_updated_at_id ON jobs(workflow_id, updated_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_jobs_parent_execution_created ON jobs(parent_execution_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_jobs_parent_execution_status ON jobs(parent_execution_id, status);
CREATE INDEX IF NOT EXISTS idx_jobs_parent_norm_created ON jobs(COALESCE(parent_execution_id, ''), created_at DESC);
CREATE INDEX IF NOT EXISTS idx_jobs_parent_norm_status ON jobs(COALESCE(parent_execution_id, ''), status);
CREATE INDEX IF NOT EXISTS idx_jobs_root_status
    ON jobs(status)
    WHERE parent_execution_id IS NULL OR parent_execution_id = '';
CREATE INDEX IF NOT EXISTS idx_jobs_child_status
    ON jobs(status)
    WHERE parent_execution_id IS NOT NULL AND parent_execution_id != '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_idempotency_key ON jobs(COALESCE(user_id, ''), idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_jobs_request_hash ON jobs(request_hash, created_at DESC) WHERE request_hash IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_jobs_config_hash ON jobs(config_hash) WHERE config_hash IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_jobs_user_idempotency ON jobs(COALESCE(user_id, ''), idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_jobs_user_request_hash ON jobs(user_id, request_hash, created_at DESC) WHERE request_hash IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_jobs_dag_hash ON jobs(dag_hash) WHERE dag_hash IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_jobs_workflow_execution_id ON jobs(workflow_execution_id) WHERE workflow_execution_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_jobs_pending_durable_root_created
    ON jobs(created_at, id)
    WHERE status = 'pending'
      AND dag_hash IS NOT NULL
      AND dag_hash != ''
      AND (parent_execution_id IS NULL OR parent_execution_id = '');
CREATE INDEX IF NOT EXISTS idx_jobs_pending_durable_child_created
    ON jobs(created_at, id)
    WHERE status = 'pending'
      AND dag_hash IS NOT NULL
      AND dag_hash != ''
      AND parent_execution_id IS NOT NULL
      AND parent_execution_id != '';
CREATE INDEX IF NOT EXISTS idx_jobs_pending_durable_created
    ON jobs(created_at, id)
    WHERE status = 'pending'
      AND dag_hash IS NOT NULL
      AND dag_hash != '';
CREATE INDEX IF NOT EXISTS idx_jobs_running_durable_created
    ON jobs(created_at, id)
    WHERE status = 'running'
      AND dag_hash IS NOT NULL
      AND dag_hash != '';
CREATE INDEX IF NOT EXISTS idx_jobs_workflow_stats_cover
    ON jobs(workflow_id, updated_at DESC, id DESC, status, cost, tokens_total)
    WHERE workflow_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_workflow_node_executions_order ON workflow_node_executions(job_id, run_id, node_order);
CREATE INDEX IF NOT EXISTS idx_workflow_node_executions_job_node ON workflow_node_executions(job_id, node_id);
CREATE INDEX IF NOT EXISTS idx_workflow_node_execution_attempts_job_node_attempt ON workflow_node_execution_attempts(job_id, node_id, attempt_number);
CREATE INDEX IF NOT EXISTS idx_workflow_node_execution_attempts_run_status ON workflow_node_execution_attempts(job_id, run_id, status);
CREATE INDEX IF NOT EXISTS idx_workflow_node_execution_attempts_activity_id ON workflow_node_execution_attempts(activity_id);
CREATE INDEX IF NOT EXISTS idx_workflows_name ON workflows(name);
CREATE INDEX IF NOT EXISTS idx_workflows_updated_at ON workflows(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_api_keys_user_created ON api_keys(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_api_keys_prefix ON api_keys(prefix);
CREATE UNIQUE INDEX IF NOT EXISTS idx_api_model_routes_single_default
    ON api_model_routes(is_default)
    WHERE is_default = 1 AND enabled = 1;
CREATE INDEX IF NOT EXISTS idx_api_model_routes_enabled ON api_model_routes(enabled, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_api_usage_created_at ON api_usage(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_api_usage_key_created ON api_usage(key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_api_usage_model_created ON api_usage(requested_model, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_api_usage_status_created ON api_usage(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_api_usage_request_id ON api_usage(request_id);
CREATE INDEX IF NOT EXISTS idx_api_usage_job_id ON api_usage(job_id) WHERE job_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_api_idempotency_key ON api_idempotency(key_id, idempotency_key);
CREATE INDEX IF NOT EXISTS idx_api_idempotency_expires ON api_idempotency(expires_at);
CREATE INDEX IF NOT EXISTS idx_api_openai_objects_key_type_created ON api_openai_objects(key_id, object_type, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_api_openai_objects_key_status_created ON api_openai_objects(key_id, status, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_api_openai_objects_key_model_created ON api_openai_objects(key_id, requested_model, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_api_openai_objects_job_id ON api_openai_objects(job_id) WHERE job_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_api_openai_items_object_kind_order ON api_openai_items(object_id, item_kind, item_index, id);
CREATE INDEX IF NOT EXISTS idx_api_openai_items_object_kind_openai_id ON api_openai_items(object_id, item_kind, openai_item_id);

-- Optimization run storage.
CREATE TABLE IF NOT EXISTS optimization_runs (
    id TEXT PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    workflow_version INTEGER NOT NULL DEFAULT 1,
    benchmark TEXT NOT NULL,
    split TEXT NOT NULL,
    item_limit INTEGER DEFAULT 0,
    concurrency INTEGER DEFAULT 1,
    spec TEXT NOT NULL DEFAULT '{}',          -- JSON OptimizeSpec
    strategy TEXT NOT NULL DEFAULT 'evolutionary',
    population_size INTEGER DEFAULT 10,
    children_per_parent INTEGER DEFAULT 1,
    max_children_per_generation INTEGER DEFAULT 0,
    adaptive_fanout INTEGER DEFAULT 0,
    claude_model TEXT NOT NULL DEFAULT '',
    mutator_mode TEXT NOT NULL DEFAULT 'auto',
    rng_seed INTEGER,
    compact_artifacts INTEGER DEFAULT 0,
    total_budget_usd REAL DEFAULT 0.0,
    spent_usd REAL DEFAULT 0.0,
    dspy_metric_calls_used INTEGER DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'running', 'paused', 'completed', 'failed', 'cancelled')),
    generation INTEGER DEFAULT 0,
    total_organisms INTEGER DEFAULT 0,
    best_organism_id TEXT,
    best_fitness TEXT,                        -- JSON Fitness
    learning_log TEXT DEFAULT '[]',           -- JSON []LearningEntry
    owner_pid INTEGER,
    owner_hostname TEXT,
    last_heartbeat_at DATETIME,
    started_at DATETIME,
    completed_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_optimization_runs_status_created
    ON optimization_runs(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_optimization_runs_workflow_created
    ON optimization_runs(workflow_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_optimization_runs_heartbeat
    ON optimization_runs(last_heartbeat_at DESC);

CREATE TABLE IF NOT EXISTS optimization_organisms (
    id TEXT PRIMARY KEY,
    opt_run_id TEXT NOT NULL,
    generation INTEGER NOT NULL,
    parent_ids TEXT DEFAULT '[]',             -- JSON []string
    param_values TEXT NOT NULL,               -- JSON map[path]jsonEncodedValue
    workflow_json TEXT NOT NULL,              -- full materialized workflow JSON
    mutation_type TEXT NOT NULL DEFAULT '',
    mutation_log TEXT NOT NULL DEFAULT '',
    bench_run_id TEXT,
    accuracy REAL,
    adjusted_accuracy REAL,
    parse_rate REAL,
    cost_per_item REAL,
    avg_latency_ms REAL,
    p95_latency_ms REAL,
    failed_items INTEGER DEFAULT 0,
    total_items INTEGER DEFAULT 0,
    flagged_items INTEGER DEFAULT 0,
    composite_score REAL,
    feasible INTEGER DEFAULT 1,
    constraint_violations TEXT DEFAULT '[]',  -- JSON []string
    evaluated_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (opt_run_id) REFERENCES optimization_runs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_optimization_organisms_run_generation
    ON optimization_organisms(opt_run_id, generation, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_optimization_organisms_run_score
    ON optimization_organisms(opt_run_id, composite_score DESC);
CREATE INDEX IF NOT EXISTS idx_optimization_organisms_bench_run
    ON optimization_organisms(bench_run_id) WHERE bench_run_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS optimization_param_changes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    organism_id TEXT NOT NULL,
    param_path TEXT NOT NULL,
    old_value TEXT,
    new_value TEXT,
    reason TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (organism_id) REFERENCES optimization_organisms(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_optimization_param_changes_org
    ON optimization_param_changes(organism_id, id);

CREATE TABLE IF NOT EXISTS optimization_mutation_artifacts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    organism_id TEXT NOT NULL,
    input_prompt_hash TEXT NOT NULL,
    input_prompt TEXT,
    raw_output_hash TEXT NOT NULL,
    raw_output TEXT,
    claude_model TEXT,
    claude_cli_version TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (organism_id) REFERENCES optimization_organisms(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_optimization_mutation_artifacts_org
    ON optimization_mutation_artifacts(organism_id, id);

-- Optional replay request payload for targeted re-execution.
CREATE TABLE IF NOT EXISTS execution_replay_requests (
    job_id TEXT PRIMARY KEY,
    replay_request TEXT NOT NULL, -- JSON payload (runtime.ReplayRequest)
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_execution_replay_requests_updated_at
    ON execution_replay_requests(updated_at DESC);

-- Job events table: durable event store for stream resilience.
CREATE TABLE IF NOT EXISTS job_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    version INTEGER NOT NULL DEFAULT 1,
    job_id TEXT NOT NULL,
    sequence INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    execution_id TEXT, -- Stable logical execution identity
    run_id TEXT, -- Per-run identity (durable runtime)
    node_id TEXT, -- Node-level correlation for fast timeline queries
    agent_run_id TEXT, -- Correlates agent internal loop events
    iteration INTEGER, -- Agent iteration counter (nullable)
    timestamp DATETIME NOT NULL,
    payload TEXT NOT NULL, -- JSON event data
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE CASCADE,
    UNIQUE(job_id, sequence)
);

CREATE INDEX IF NOT EXISTS idx_job_events_job_node ON job_events(job_id, node_id);
CREATE INDEX IF NOT EXISTS idx_job_events_agent_seq ON job_events(agent_run_id, sequence);
CREATE INDEX IF NOT EXISTS idx_job_events_timestamp ON job_events(timestamp);
CREATE INDEX IF NOT EXISTS idx_job_events_created_at ON job_events(created_at);

-- Side-effect outbox table for tool-call/webhook durability.
CREATE TABLE IF NOT EXISTS side_effect_outbox (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tool_call_id TEXT NOT NULL UNIQUE,
    job_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    payload TEXT NOT NULL, -- JSON: tool type, parameters, etc.
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending', 'processing', 'completed', 'failed')),
    attempts INTEGER DEFAULT 0,
    max_attempts INTEGER DEFAULT 3,
    next_attempt_at DATETIME,
    last_error TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_outbox_pending ON side_effect_outbox(status, next_attempt_at);
CREATE INDEX IF NOT EXISTS idx_outbox_job_id ON side_effect_outbox(job_id);

-- Trace spans table: fine-grained execution spans within nodes
CREATE TABLE IF NOT EXISTS job_node_traces (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    span_id TEXT NOT NULL,
    parent_span_id TEXT,
    kind TEXT NOT NULL,                    -- "node", "call", "decision"
    status TEXT NOT NULL DEFAULT 'ok',     -- "ok", "error", "timeout", "cancelled"
    started_at DATETIME NOT NULL,
    duration_ms REAL,
    attributes TEXT NOT NULL DEFAULT '{}', -- JSON: span-specific data
    tool_call_id TEXT,                     -- FK link to side_effect_outbox (nullable)
    FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_traces_job_node ON job_node_traces(job_id, node_id);

-- Durable runtime tables.

-- Execution history table: durable history events with per-run monotonic sequence
CREATE TABLE IF NOT EXISTS execution_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    sequence INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    node_id TEXT,
    activity_id TEXT,
    timestamp DATETIME NOT NULL,
    attributes TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(run_id, sequence)
);

CREATE INDEX IF NOT EXISTS idx_history_run_node ON execution_history(run_id, node_id);

-- Activity results table: idempotent activity outcomes
CREATE TABLE IF NOT EXISTS activity_results (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    idempotency_key TEXT NOT NULL UNIQUE,
    run_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    attempt INTEGER NOT NULL,
    activity_type TEXT NOT NULL,
    status TEXT NOT NULL,
    output_payload TEXT,
    error_code TEXT,
    error_message TEXT,
    started_at DATETIME,
    completed_at DATETIME,
    checksum TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_activity_results_run ON activity_results(run_id, node_id);

-- Agent storage.
-- Per-node external Novomo agent run summary.
CREATE TABLE IF NOT EXISTS agent_runs (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    execution_id TEXT NOT NULL,
    run_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    attempt INTEGER NOT NULL DEFAULT 1,
    run_kind TEXT NOT NULL DEFAULT 'agent_run' CHECK(run_kind IN ('agent_run', 'novo_run')),
    external_run_id TEXT NOT NULL,
    external_job_run_id TEXT,
    external_task_id TEXT,
    inherit_from_json TEXT,
    harness TEXT NOT NULL,
    status TEXT NOT NULL, -- running, completed, failed
    output TEXT,
    tokens_input INTEGER DEFAULT 0,
    tokens_output INTEGER DEFAULT 0,
    cost_usd REAL DEFAULT 0.0,
    error_code TEXT,
    error_message TEXT,
    started_at DATETIME,
    finished_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (job_id) REFERENCES jobs(id) ON DELETE CASCADE,
    UNIQUE(job_id, run_id, node_id, attempt)
);

CREATE INDEX IF NOT EXISTS idx_agent_runs_job_node ON agent_runs(job_id, node_id);
CREATE INDEX IF NOT EXISTS idx_agent_runs_job_created_at ON agent_runs(job_id, created_at, id);
CREATE INDEX IF NOT EXISTS idx_agent_runs_external_run_id ON agent_runs(external_run_id);

-- Benchmark run storage: canonical metadata and item-level outcomes for UI inspection.
CREATE TABLE IF NOT EXISTS benchmark_runs (
    id TEXT PRIMARY KEY, -- run_id from benchmark artifacts
    benchmark TEXT NOT NULL,
    split TEXT NOT NULL,
    item_limit INTEGER DEFAULT 0, -- 0 = full run, >0 = limited run
    workflow_id TEXT NOT NULL,
    workflow_name TEXT NOT NULL DEFAULT '',
    dataset_path TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'completed' CHECK(status IN ('queued', 'running', 'completed', 'failed', 'cancelled', 'imported')),
    total_items INTEGER DEFAULT 0,
    completed_items INTEGER DEFAULT 0,
    failed_items INTEGER DEFAULT 0,
    parsed_items INTEGER DEFAULT 0,
    correct_items INTEGER DEFAULT 0,
    accuracy REAL DEFAULT 0.0,
    parse_rate REAL DEFAULT 0.0,
    retried_items INTEGER DEFAULT 0,
    total_attempts INTEGER DEFAULT 0,
    total_non_letter_retries INTEGER DEFAULT 0,
    admission_retries INTEGER DEFAULT 0,
    items_with_admission_retries INTEGER DEFAULT 0,
    failure_reason_counts TEXT NOT NULL DEFAULT '{}', -- JSON object
    all_attempt_failure_counts TEXT NOT NULL DEFAULT '{}', -- JSON object
    total_latency_ms REAL DEFAULT 0.0,
    avg_latency_ms REAL DEFAULT 0.0,
    p50_latency_ms REAL DEFAULT 0.0,
    p95_latency_ms REAL DEFAULT 0.0,
    p99_latency_ms REAL DEFAULT 0.0,
    total_tokens_input INTEGER DEFAULT 0,
    total_tokens_output INTEGER DEFAULT 0,
    total_tokens INTEGER DEFAULT 0,
    avg_tokens_per_item REAL DEFAULT 0.0,
    total_cost_usd REAL DEFAULT 0.0,
    avg_cost_usd_per_item REAL DEFAULT 0.0,
    started_at DATETIME,
    completed_at DATETIME,
    elapsed_seconds REAL DEFAULT 0.0,
    execution_engine TEXT NOT NULL DEFAULT '',
    execution_engine_notes TEXT,
    source TEXT NOT NULL DEFAULT 'manual', -- manual | benchloop | optimizer | imported | replay
    opt_run_id TEXT,
    opt_organism_id TEXT,
    artifact_path TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_benchmark_runs_created_at ON benchmark_runs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_benchmark_runs_benchmark_workflow ON benchmark_runs(benchmark, workflow_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_benchmark_runs_status ON benchmark_runs(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_benchmark_runs_started_or_created ON benchmark_runs(COALESCE(started_at, created_at) DESC);
CREATE INDEX IF NOT EXISTS idx_benchmark_runs_source ON benchmark_runs(source, created_at DESC);

CREATE TABLE IF NOT EXISTS benchmark_run_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    subject TEXT,
    language TEXT,
    answer_label TEXT,
    predicted TEXT,
    parse_ok INTEGER NOT NULL DEFAULT 0, -- sqlite boolean
    correct INTEGER NOT NULL DEFAULT 0, -- sqlite boolean
    job_id TEXT,
    latency_ms REAL DEFAULT 0.0,
    tokens_input INTEGER DEFAULT 0,
    tokens_output INTEGER DEFAULT 0,
    total_tokens INTEGER DEFAULT 0,
    cost_usd REAL DEFAULT 0.0,
    raw_output TEXT,
    error TEXT,
    failure_reason TEXT,
    output_source TEXT,
    workflow_id TEXT NOT NULL DEFAULT '',
    benchmark_name TEXT NOT NULL DEFAULT '',
    attempts INTEGER DEFAULT 0,
    non_letter_retries INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (run_id) REFERENCES benchmark_runs(id) ON DELETE CASCADE,
    UNIQUE(run_id, item_id)
);

CREATE INDEX IF NOT EXISTS idx_benchmark_run_items_run_correct ON benchmark_run_items(run_id, correct);
CREATE INDEX IF NOT EXISTS idx_benchmark_run_items_run_failure ON benchmark_run_items(run_id, failure_reason);
CREATE INDEX IF NOT EXISTS idx_benchmark_run_items_run_subject ON benchmark_run_items(run_id, subject);
CREATE INDEX IF NOT EXISTS idx_benchmark_run_items_run_job ON benchmark_run_items(run_id, job_id);

CREATE TABLE IF NOT EXISTS benchmark_run_item_attempts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    attempt_number INTEGER NOT NULL,
    job_id TEXT,
    latency_ms REAL DEFAULT 0.0,
    tokens_input INTEGER DEFAULT 0,
    tokens_output INTEGER DEFAULT 0,
    total_tokens INTEGER DEFAULT 0,
    cost_usd REAL DEFAULT 0.0,
    raw_output TEXT,
    predicted TEXT,
    parse_ok INTEGER NOT NULL DEFAULT 0, -- sqlite boolean
    error TEXT,
    failure_reason TEXT,
    output_source TEXT,
    contract_node_id TEXT,
    contract_model TEXT,
    contract_finish_reason TEXT,
    contract_tokens_output INTEGER DEFAULT 0,
    contract_max_tokens INTEGER DEFAULT 0,
    contract_diagnostic TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (run_id) REFERENCES benchmark_runs(id) ON DELETE CASCADE,
    UNIQUE(run_id, item_id, attempt_number)
);

CREATE INDEX IF NOT EXISTS idx_benchmark_item_attempts_run_item ON benchmark_run_item_attempts(run_id, item_id, attempt_number);
CREATE INDEX IF NOT EXISTS idx_benchmark_item_attempts_run_job
    ON benchmark_run_item_attempts(run_id, job_id)
    WHERE job_id IS NOT NULL AND job_id != '';

-- Normalized failure counts for cross-run SQL aggregation
-- (supplements JSON failure_reason_counts / all_attempt_failure_counts columns on benchmark_runs)
CREATE TABLE IF NOT EXISTS benchmark_run_failure_counts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL,
    failure_reason TEXT NOT NULL,
    scope TEXT NOT NULL CHECK(scope IN ('final', 'all_attempts')),
    count INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (run_id) REFERENCES benchmark_runs(id) ON DELETE CASCADE,
    UNIQUE(run_id, failure_reason, scope)
);

CREATE INDEX IF NOT EXISTS idx_benchmark_failure_counts_run
    ON benchmark_run_failure_counts(run_id);
CREATE INDEX IF NOT EXISTS idx_benchmark_failure_counts_reason
    ON benchmark_run_failure_counts(failure_reason, scope);

-- Standalone job_id indexes for trigger efficiency
CREATE INDEX IF NOT EXISTS idx_benchmark_run_items_job_id
    ON benchmark_run_items(job_id) WHERE job_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_benchmark_item_attempts_job_id
    ON benchmark_run_item_attempts(job_id) WHERE job_id IS NOT NULL;

-- Benchmark runner session: tracks lifecycle of a benchmark execution session
-- (one session may span multiple benchmark_runs). Persisted to DB so status
-- survives server restarts. Stale sessions marked 'abandoned' on startup.
CREATE TABLE IF NOT EXISTS benchmark_runner_sessions (
    id TEXT PRIMARY KEY,
    status TEXT NOT NULL DEFAULT 'running' CHECK(status IN ('running', 'completed', 'failed', 'cancelled', 'abandoned')),
    command TEXT NOT NULL DEFAULT '',
    error TEXT,
    total_runs INTEGER NOT NULL DEFAULT 0,
    completed_runs INTEGER NOT NULL DEFAULT 0,
    imported_runs INTEGER NOT NULL DEFAULT 0,
    total_items INTEGER NOT NULL DEFAULT 0,
    completed_items INTEGER NOT NULL DEFAULT 0,
    correct_items INTEGER NOT NULL DEFAULT 0,
    incorrect_items INTEGER NOT NULL DEFAULT 0,
    current_run_id TEXT,
    current_benchmark TEXT,
    current_workflow TEXT,
    current_item_id TEXT,
    cancel_requested INTEGER NOT NULL DEFAULT 0,
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at DATETIME,
    last_heartbeat_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_benchmark_sessions_status ON benchmark_runner_sessions(status);

-- Benchmark dataset flags: cross-run flags for suspected wrong gold labels.
-- Active flags exclude items from tuning deltas; resolving preserves audit trail.
CREATE TABLE IF NOT EXISTS benchmark_dataset_flags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    benchmark TEXT NOT NULL,
    split TEXT NOT NULL,
    item_id TEXT NOT NULL,
    dataset_version TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL CHECK(length(trim(reason)) > 0),
    source TEXT NOT NULL DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    resolved_at DATETIME,
    resolved_by TEXT,
    resolved_reason TEXT
);

-- Only one active flag per item
CREATE UNIQUE INDEX IF NOT EXISTS idx_dataset_flags_active
    ON benchmark_dataset_flags (benchmark, split, item_id)
    WHERE resolved_at IS NULL;

-- Listing/filtering
CREATE INDEX IF NOT EXISTS idx_dataset_flags_benchmark_split
    ON benchmark_dataset_flags (benchmark, split, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_dataset_flags_item
    ON benchmark_dataset_flags (benchmark, split, item_id);

-- Null out benchmark job references when jobs are deleted
-- (PRAGMA foreign_keys is not enabled, so we use a trigger instead of FK ON DELETE SET NULL)
CREATE TRIGGER IF NOT EXISTS trg_benchmark_nullify_job_id
AFTER DELETE ON jobs
FOR EACH ROW
BEGIN
    UPDATE benchmark_run_items SET job_id = NULL WHERE job_id = OLD.id;
    UPDATE benchmark_run_item_attempts SET job_id = NULL WHERE job_id = OLD.id;
END;
