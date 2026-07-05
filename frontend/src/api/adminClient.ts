import axios from 'axios';

const API_BASE = '/api/admin';

export type AdminStatus = 'pending' | 'running' | 'paused' | 'completed' | 'failed' | 'cancelled' | 'archived' | '';

export interface AdminJobRow {
  id: string;
  description: string;
  model: string;
  status: string;
  tokens_total: number;
  created_at: string;
  cost: number;
  InputPrompt: string;
  IsChild: boolean;
  ParentExecutionID: string;
  DirectCost: number;
  ChildCost: number;
  DisplayCost: number;
  DescendantCount: number;
}

export interface AdminJobsResponse {
  Jobs: AdminJobRow[];
  Pagination: {
    Page: number;
    Limit: number;
    TotalItems: number;
    TotalPages: number;
    HasPrev: boolean;
    HasNext: boolean;
    StartItem: number;
    EndItem: number;
    Pages: number[];
  };
  ShowFilter: 'parents' | 'children' | 'all';
  StatusFilter: AdminStatus;
  ActiveWorkflows: number;
  PoolCapacity: number;
  PendingJobs: number;
  RunningJobs: number;
  PausedJobs: number;
  CompletedJobs: number;
  FailedJobs: number;
  CancelledJobs: number;
  ScopedTotal: number;
  ParentCount: number;
  ChildCount: number;
  AllCount: number;
  HasActiveJobs: boolean;
}

export interface AdminOverviewResponse {
  Stats: {
    TotalJobs: number;
    CompletedJobs: number;
    FailedJobs: number;
    TotalCost: number;
    TotalTokens: number;
    TotalWorkflows: number;
  };
}

export interface AdminWorkflowStats {
  id: string;
  name: string;
  description: string;
  Layer: string; // L0/L1/L2/L3 derived from ID prefix, or '' if uncategorized
  updated_at?: string;
  ExecutionCount: number;
  SuccessCount: number;
  SuccessRate: number;
  TotalCost: number;
  TotalTokens: number;
  NodeCount: number;
  NodeTypes: Record<string, number>;
  ModelsUsed: string[];
  AggregationSourceIDs: string[];
  WorkflowRefIDs: string[];
  ChildWorkflowIDs: string[];
  ReferencesL0DirectlyAsBenchmark: boolean;
  LastRunAt?: string;
  LastRunStatus?: string;
}

export interface AdminWorkflowsResponse {
  Workflows: AdminWorkflowStats[];
  TotalWorkflows: number;
  ActiveWorkflows: number;
  TotalExecutions: number;
  FrontendURL: string;
}

export interface AdminWorkflowDetailResponse {
  Workflow: AdminWorkflowStats;
  Definition: unknown;
  DefinitionJSON: string;
  RecentExecutions: Array<{
    id: string;
    description: string;
    status: string;
    cost: number;
    tokens_total: number;
    created_at: string;
    updated_at: string;
  }>;
  FrontendURL: string;
}

export interface AdminAPIKey {
  id: string;
  user_id: string;
  name: string;
  prefix: string;
  workflow_id?: string;
  requests_per_minute: number;
  tokens_per_minute: number;
  created_at: string;
  last_used_at?: string;
  revoked_at?: string;
}

export interface AdminAPIUsageRecord {
  id: string;
  request_id: string;
  key_id: string;
  user_id: string;
  endpoint: string;
  requested_model?: string;
  resolved_model?: string;
  workflow_id?: string;
  job_id?: string;
  status: string;
  http_status?: number;
  stream: boolean;
  tokens_input: number;
  tokens_output: number;
  tokens_total: number;
  cost: number;
  latency_ms: number;
  error_code?: string;
  error_message?: string;
  created_at: string;
  completed_at?: string;
}

export interface AdminAPIUsageSummary {
  requests: number;
  tokens_input: number;
  tokens_output: number;
  tokens_total: number;
  cost: number;
}

export interface AdminAPIModelRoute {
  api_model: string;
  mode: 'workflow' | 'direct_model' | string;
  workflow_id?: string;
  provider_model?: string;
  description?: string;
  is_default: boolean;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface AdminAPIUsageFilters {
  from?: string;
  to?: string;
  key_id?: string;
  model?: string;
  endpoint?: string;
  status?: string;
  limit?: number;
}

export interface NodeMetric {
  id: number;
  node_id: string;
  node_order: number;
  node_type: string;
  node_label?: string;
  node_name?: string;
  status: string;
  model?: string;
  output?: string;
  prompt?: string;
  error_message?: string;
  tokens_input: number;
  tokens_output: number;
  cost: number;
  latency_ms: number;
  WidthPercent: number;
  StartOffsetPercent: number;
  TimelineDurationMs: number;
  ActiveDurationMs: number;
  WaitDurationMs: number;
  DisplayOrder: string;
  IsChildNode: boolean;
  IsParentNode: boolean;
  metadata?: string;
  ChildJobID?: string;
  NodeConfig?: string;
  SystemPrompt?: string;
  ChildJobNodes?: NodeMetric[];
}

export interface AgentRunSummary {
  id: string;
  job_id: string;
  execution_id: string;
  run_id: string;
  node_id: string;
  attempt: number;
  run_kind: string;
  external_run_id: string;
  external_job_run_id?: string;
  external_task_id?: string;
  inherit_from_json?: string;
  harness: string;
  status: string;
  output?: string;
  tokens_input: number;
  tokens_output: number;
  cost_usd: number;
  error_code?: string;
  error_message?: string;
  started_at?: string;
  finished_at?: string;
  created_at: string;
  updated_at: string;
  novomo_urls?: NovomoURL[];
}

export interface NovomoURL {
  kind: string;
  label: string;
  id: string;
  url: string;
}

export interface AdminJobDetailResponse {
  Job: {
    id: string;
    description: string;
    model: string;
    status: string;
    tokens_total: number;
    tokens_input: number;
    tokens_output: number;
    cost: number;
    retry_count: number;
    error_message?: string;
    created_at: string;
    updated_at: string;
    workflow_id?: string;
    parent_execution_id?: string;
  };
  WorkflowSaved: boolean;
  Lifecycle: {
    SubmittedAt: string;
    CompletedAt: string;
    QueueDelayMs: number;
    ExecutionDurationMs: number;
    ActiveAttemptDurationMs: number;
    IdleDurationMs: number;
    HistoryEventCount: number;
  };
  JobCostSummary: {
    DirectCost: number;
    ChildCost: number;
    TotalCost: number;
    DescendantCount: number;
  };
  InputPrompt: string;
  NodesWithMetrics: NodeMetric[];
  Attempts: Array<{
    id: number;
    node_id: string;
    node_type: string;
    status: string;
    attempt: number;
    latency_ms: number;
    tokens_input: number;
    tokens_output: number;
    cost: number;
    error_code?: string;
    error_message?: string;
    metadata?: Record<string, unknown>;
    started_at?: string;
    completed_at?: string;
  }>;
  AgentRuns: AgentRunSummary[];
  ChildJobs: Array<{
    id: string;
    description: string;
    status: string;
    model: string;
    tokens_total: number;
    cost: number;
  }>;
  BenchmarkRun?: {
    run_id: string;
    benchmark: string;
    item_id?: string;
  };
  ParentWorkflowID?: string;
}

export interface BenchmarkRunnerStatus {
  running: boolean;
  started_at?: string;
  finished_at?: string;
  command?: string;
  error?: string;
  imported_runs?: number;
  total_runs?: number;
  completed_runs?: number;
  total_items?: number;
  completed_items?: number;
  correct_items?: number;
  incorrect_items?: number;
  current_run_id?: string;
  current_benchmark?: string;
  current_workflow?: string;
  current_item_id?: string;
  cancel_requested?: boolean;
  last_update_at?: string;
  benchmarks?: string[];
  workflows?: string[];
  run_set?: string;
  split?: string;
  limit?: number;
  concurrency?: number;
  max_non_letter_retries?: number;
  max_transient_retries?: number;
}

export interface AdminBenchmarksResponse {
  Runs: Array<{
    id: string;
    benchmark: string;
    split: string;
    item_limit: number;
    workflow_id: string;
    status: string;
    source: string;
    opt_run_id?: string;
    opt_organism_id?: string;
    total_items: number;
    correct_items: number;
    accuracy: number;
    total_cost_usd: number;
    total_tokens: number;
    started_at?: string;
    completed_at?: string;
    updated_at?: string;
  }>;
  Runner: {
    running: boolean;
    started_at?: string;
    completed_runs?: number;
    total_runs?: number;
    completed_items?: number;
    total_items?: number;
    error?: string;
    benchmarks?: string[];
    workflows?: string[];
    run_set?: string;
    split?: string;
    limit?: number;
    concurrency?: number;
    max_non_letter_retries?: number;
    max_transient_retries?: number;
  };
  Benchmarks: string[];
  Workflows: string[];
  Statuses: string[];
  Splits: string[];
  AvailableBenchmarks: string[];
  AvailableWorkflows: string[];
  FilterBenchmark: string;
  FilterWorkflow: string;
  FilterStatus: string;
  FilterSplit: string;
  FilterIncludeOpt: boolean;
  ChartSeries: Array<{
    Split: string;
    Points: Array<{
      RunID: string;
      WorkflowID: string;
      Benchmark: string;
      Accuracy: number;
      CostUSD: number;
      TotalItems: number;
      Date: string;
    }>;
  }>;
}

export interface AdminBenchmarkDetailResponse {
  Run: {
    id: string;
    benchmark: string;
    split: string;
    item_limit: number;
    workflow_id: string;
    status: string;
    source: string;
    opt_run_id?: string;
    opt_organism_id?: string;
    total_items: number;
    correct_items: number;
    accuracy: number;
    parse_rate?: number;
    total_attempts?: number;
    total_non_letter_retries?: number;
    p50_latency_ms?: number;
    p95_latency_ms?: number;
    p99_latency_ms?: number;
    total_cost_usd: number;
    total_tokens: number;
    failure_reason_counts?: Record<string, number>;
    all_attempt_failure_counts?: Record<string, number>;
  };
  Optimization?: {
    run_id: string;
    organism_id: string;
  };
  Items: Array<{
    item_id: string;
    subject: string;
    answer_label: string;
    predicted: string;
    correct: boolean;
    parse_ok: boolean;
    job_id: string;
    latency_ms: number;
    total_tokens: number;
    cost_usd: number;
    failure_reason: string;
    attempts?: number;
    non_letter_retries?: number;
  }>;
  TotalItems: number;
  Page: number;
  PageSize: number;
  HasPrev: boolean;
  HasNext: boolean;
  OnlyIncorrect: boolean;
  Subject: string;
  FailureReason: string;
  FailureLabels: string[];
  FailureSeries: number[];
  FinalFailureLabels: string[];
  FinalFailureSeries: number[];
  AllAttemptsFailureLabels: string[];
  AllAttemptsFailureSeries: number[];
}

export interface AdminBenchmarkItemDetailResponse {
  Run: {
    id: string;
    benchmark: string;
    split: string;
    workflow_id: string;
  };
  Detail: {
    item: {
      item_id: string;
      answer_label: string;
      predicted: string;
      parse_ok: boolean;
      correct: boolean;
      raw_output: string;
      error?: string;
      failure_reason?: string;
      total_tokens: number;
      cost_usd: number;
      job_id?: string;
      attempts?: number;
      non_letter_retries?: number;
    };
    attempts: Array<{
      attempt: number;
      job_id: string;
      predicted: string;
      parse_ok: boolean;
      error?: string;
      failure_reason?: string;
      latency_ms?: number;
      tokens_input?: number;
      tokens_output?: number;
      total_tokens: number;
      cost_usd: number;
      raw_output: string;
      output_source?: string;
      contract_node_id?: string;
      contract_finish_reason?: string;
    }>;
  };
  DatasetItem?: {
    id: string;
    benchmark: string;
    split: string;
    subject: string;
    language: string;
    question: string;
    choices?: string[];
    answer_index?: number;
    answer_label?: string;
  };
  JobSummaries?: Array<{
    JobID: string;
    Job?: {
      id: string;
      status: string;
      description: string;
      created_at: string;
      updated_at: string;
    };
    Nodes?: Array<{
      id: number;
      node_id: string;
      node_type: string;
      node_name: string;
      node_label?: string;
      parent_node_id?: string;
      status: string;
      latency_ms: number;
      cost: number;
      tokens_input: number;
      tokens_output: number;
      model?: string;
      output?: string;
      prompt?: string;
    }>;
    Attempts?: Array<{
      node_id: string;
      attempt: number;
      status: string;
      latency_ms: number;
    }>;
    TotalTokens?: number;
    TotalCost?: number;
    DescendantJobs?: number;
    ChildJobs?: Array<{
      JobID: string;
      Job?: {
        id: string;
        status: string;
        description: string;
      };
      Nodes?: Array<{
        id: number;
        node_id: string;
        node_type: string;
        node_name: string;
        node_label?: string;
        parent_node_id?: string;
        status: string;
        latency_ms: number;
        cost: number;
        tokens_input: number;
        tokens_output: number;
        model?: string;
        output?: string;
        prompt?: string;
      }>;
    }>;
  }>;
  NotFoundInSet?: boolean;
}

export interface AdminBenchmarkComparisonResponse {
  benchmark: string;
  split: string;
  control_id: string;
  metrics: Array<{
    run: {
      id: string;
      benchmark: string;
      split: string;
      workflow_id: string;
      workflow_name: string;
      status: string;
      total_items: number;
      completed_items: number;
      failed_items: number;
      parsed_items: number;
      correct_items: number;
      accuracy: number;
      parse_rate: number;
      total_non_letter_retries: number;
      failure_reason_counts: Record<string, number>;
      p95_latency_ms: number;
      total_cost_usd: number;
      avg_cost_usd_per_item: number;
      elapsed_seconds: number;
      started_at?: string;
    };
    is_control: boolean;
    accuracy_delta?: number;
    cost_delta_pct?: number;
    parse_rate_delta?: number;
  }>;
  options: Array<{ benchmark: string; split: string }>;
}

export interface BenchmarkItemComparisonResult {
  item_id: string;
  subject: string;
  answer_label: string;
  base_predicted?: string;
  candidate_predicted?: string;
  base_failure_reason?: string;
  candidate_failure_reason?: string;
  base_category?: WrongAnswerCategory;
  candidate_category?: WrongAnswerCategory;
}

export interface BenchmarkItemComparisonResponse {
  base_run_id: string;
  candidate_run_id: string;
  base_workflow_id: string;
  candidate_workflow_id: string;
  summary: {
    total_compared: number;
    regressed: number;
    improved: number;
    unchanged_correct: number;
    unchanged_wrong: number;
  };
  regressions: BenchmarkItemComparisonResult[];
  improvements: BenchmarkItemComparisonResult[];
}

export interface OptimizationFitness {
  accuracy: number;
  adjusted_accuracy: number;
  parse_rate: number;
  total_cost: number;
  cost_per_item: number;
  avg_latency_ms: number;
  p95_latency_ms: number;
  failed_items: number;
  total_items: number;
  flagged_items: number;
  composite_score: number;
  feasible: boolean;
  constraint_violations: string[];
}

export interface OptimizationLearningEntry {
  generation: number;
  organism_id: string;
  parent_id: string;
  mutation_type: string;
  verify_method?: string;
  description: string;
  outcome: 'improvement' | 'regression' | 'no_change' | 'constraint_violation' | string;
  fitness_delta: number;
  created_at: string;
}

export interface OptimizationRun {
  id: string;
  workflow_id: string;
  workflow_version: number;
  benchmark: string;
  split: string;
  item_limit: number;
  concurrency: number;
  spec: Record<string, unknown>;
  strategy: string;
  population_size: number;
  children_per_parent: number;
  max_children_per_generation: number;
  adaptive_fanout: boolean;
  claude_model: string;
  mutator_mode: string;
  rng_seed?: number;
  compact_artifacts: boolean;
  total_budget_usd: number;
  spent_usd: number;
  status: string;
  generation: number;
  total_organisms: number;
  best_organism_id?: string;
  best_fitness?: OptimizationFitness;
  learning_log: OptimizationLearningEntry[];
  owner_pid: number;
  owner_hostname: string;
  last_heartbeat_at?: string;
  started_at?: string;
  completed_at?: string;
  created_at: string;
  updated_at: string;
}

export interface OptimizationOrganism {
  id: string;
  opt_run_id: string;
  generation: number;
  parent_ids: string[];
  param_values: Record<string, string>;
  workflow_json: Record<string, unknown>;
  mutation_type: string;
  mutation_log: string;
  bench_run_id?: string;
  fitness?: OptimizationFitness;
  created_at: string;
  evaluated_at?: string;
}

export interface OptimizationParamChange {
  path: string;
  old_value: string;
  new_value: string;
  reason: string;
}

export interface OptimizationMutationArtifact {
  input_prompt_hash: string;
  input_prompt?: string;
  raw_output_hash: string;
  raw_output?: string;
  claude_model?: string;
  claude_cli_version?: string;
}

export interface OptimizationLineageResponse {
  nodes: Array<{
    id: string;
    generation: number;
    parent_ids: string[];
    composite_score?: number;
    feasible?: boolean;
  }>;
  edges: Array<{
    from: string;
    to: string;
  }>;
}

export interface OptimizationCompareResponse {
  runs: OptimizationRun[];
  best_organisms: OptimizationOrganism[];
  generation_data: Record<
    string,
    Array<{
      generation: number;
      best_accuracy: number;
      mean_accuracy: number;
      worst_accuracy: number;
      cumulative_cost: number;
    }>
  >;
}

export type WrongAnswerCategory =
  | 'all_steps_wrong'
  | 'some_right_child_wrong'
  | 'all_right_child_wrong'
  | 'child_right_parent_wrong'
  | 'unclassified';

export interface WrongAnswerAnalysisResponse {
  run_id: string;
  benchmark: string;
  split: string;
  workflow_id: string;
  summary: {
    total_incorrect: number;
    all_steps_wrong: number;
    some_right_child_wrong: number;
    all_right_child_wrong: number;
    child_right_parent_wrong: number;
    unclassified: number;
  };
  items: Array<{
    item_id: string;
    subject: string;
    answer_label: string;
    parent_predicted: string;
    child_predicted: string;
    agent_answers: Array<{
      node_id: string;
      model?: string;
      answer: string;
      correct: boolean;
      parse_ok: boolean;
    }>;
    category: WrongAnswerCategory;
    parent_job_id?: string;
    child_job_id?: string;
  }>;
}

// ---------------------------------------------------------------------------
// Benchloop types
// ---------------------------------------------------------------------------

export interface BenchloopStatusResponse {
  active: boolean;
  state: BenchloopState | null;
  matrix: BenchloopMatrix | null;
  lock: BenchloopLockInfo | null;
  sessions: BenchloopSession[];
  decision: BenchloopDecision | null;
  archived_ids: string[];
  elapsed_ms: number;
}

export interface BenchloopState {
  session_id: string;
  target_workflows: string[];
  benchmark: string;
  split: string;
  item_limit: number;
  concurrency: number;
  baseline_accuracy: number;
  baseline_parse_rate: number;
  baseline_cost_per_item: number;
  baseline_failed_items: number;
  baseline_run_id: string;
  baseline_total_items: number;
  baseline_avg_latency_ms: number;
  baseline_p95_latency_ms: number;
  current_accuracy: number;
  current_parse_rate: number;
  current_cost_per_item: number;
  current_failed_items: number;
  current_run_id: string;
  current_avg_latency_ms: number;
  current_p95_latency_ms: number;
  iteration: number;
  max_iterations: number;
  plateau_count: number;
  agent_crash_count: number;
  total_attempts: number;
  total_agent_cost_usd: number;
  total_bench_cost_usd: number;
  status: string;
  started_at: string;
  last_iteration_at?: string;
  live?: BenchloopLiveStatus;
  last_claude_session_id?: string;
  last_transcript_path?: string;
}

export interface BenchloopLiveStatus {
  phase?: string;
  step?: string;
  message?: string;
  iteration?: number;
  attempt?: number;
  agent_pid?: number;
  agent_session_id?: string;
  agent_log_path?: string;
  transcript_path?: string;
  started_at?: string;
  updated_at?: string;
}

export interface BenchloopMatrix {
  benchmark: string;
  run_set: string;
  split: string;
  item_limit: number;
  concurrency: number;
  workflow_order: string[];
  target_workflows: string[];
  rationale: string;
  approved_at: string;
  session_id: string;
}

export interface BenchloopLockInfo {
  pid: number;
  created_at: string;
  session_id: string;
  alive: boolean;
}

export interface BenchloopSession {
  loop_session_id: string;
  phase: string;
  iteration: number;
  attempt: number;
  claude_session_id: string;
  transcript_path: string;
  log_path: string;
  exit_code: number;
  started_at: string;
  finished_at: string;
  in_flight?: boolean;
  duration_ms?: number;
}

export interface BenchloopDecision {
  verdict: string;
  run_id: string;
  accuracy: number;
  parse_rate: number;
  failed_items: number;
  total_items: number;
  cost_per_item: number;
  avg_latency_ms?: number;
  p95_latency_ms?: number;
  workflows_modified: string[];
  changes_summary: string;
  reasoning: string;
  sanity_passed: boolean;
  failure_analysis?: string;
}

export interface BenchloopTranscriptResponse {
  claude_session_id: string;
  messages: BenchloopTranscriptMsg[];
  error?: string;
}

export interface BenchloopTranscriptMsg {
  role: string;
  text: string;
  timestamp?: string;
}

export interface BenchloopMemoryResponse {
  content: string;
  truncated: boolean;
}

export interface BenchloopLogResponse {
  path: string;
  content: string;
  lines: number;
}

export const adminClient = {
  async getOverview() {
    const response = await axios.get<AdminOverviewResponse>(`${API_BASE}/overview`);
    return response.data;
  },

  async listAPIKeys(params: { user_id?: string; include_revoked?: boolean } = {}) {
    const response = await axios.get<{ api_keys: AdminAPIKey[] }>(`${API_BASE}/api-keys`, { params });
    return response.data;
  },

  async createAPIKey(payload: {
    name: string;
    user_id?: string;
    workflow_id?: string;
    requests_per_minute?: number;
    tokens_per_minute?: number;
  }) {
    const response = await axios.post<{ key: string; api_key: AdminAPIKey }>(`${API_BASE}/api-keys`, payload);
    return response.data;
  },

  async revokeAPIKey(id: string) {
    const response = await axios.delete<{ message: string }>(`${API_BASE}/api-keys/${encodeURIComponent(id)}`);
    return response.data;
  },

  async listAPIUsage(params: AdminAPIUsageFilters = {}) {
    const response = await axios.get<{
      api_usage: AdminAPIUsageRecord[];
      usage: AdminAPIUsageRecord[];
      summary: AdminAPIUsageSummary;
    }>(`${API_BASE}/api-usage`, { params });
    return response.data;
  },

  async exportAPIUsage(params: AdminAPIUsageFilters = {}) {
    const response = await axios.get<Blob>(`${API_BASE}/api-usage/export`, {
      params,
      responseType: 'blob',
    });
    return response.data;
  },

  async listAPIModelRoutes(params: { include_disabled?: boolean } = {}) {
    const response = await axios.get<{ model_routes: AdminAPIModelRoute[] }>(`${API_BASE}/model-routes`, { params });
    return response.data;
  },

  async upsertAPIModelRoute(payload: {
    api_model: string;
    mode: 'workflow' | 'direct_model';
    workflow_id?: string;
    provider_model?: string;
    description?: string;
    is_default?: boolean;
    enabled?: boolean;
  }) {
    const response = await axios.post<{ model_route: AdminAPIModelRoute }>(`${API_BASE}/model-routes`, payload);
    return response.data;
  },

  async deleteAPIModelRoute(model: string) {
    const response = await axios.delete<{ message: string }>(`${API_BASE}/model-routes/${encodeURIComponent(model)}`);
    return response.data;
  },

  async listJobs(params: {
    page?: number;
    limit?: number;
    show?: 'parents' | 'children' | 'all';
    status?: AdminStatus;
    sort?: string;
    dir?: string;
  }) {
    const response = await axios.get<AdminJobsResponse>(`${API_BASE}/jobs`, { params });
    return response.data;
  },

  async getJob(id: string) {
    const response = await axios.get<AdminJobDetailResponse>(`${API_BASE}/jobs/${id}`);
    return response.data;
  },

  async listAgentRuns(id: string) {
    const response = await axios.get<{ job_id: string; agent_runs: AgentRunSummary[] }>(
      `${API_BASE}/jobs/${encodeURIComponent(id)}/agent-runs`,
    );
    return response.data;
  },

  async stopAgentRun(jobID: string, agentRunID: string) {
    return axios.post(
      `${API_BASE}/jobs/${encodeURIComponent(jobID)}/agent-runs/${encodeURIComponent(agentRunID)}/stop`,
    );
  },

  async cancelJob(id: string) {
    return axios.post(`${API_BASE}/jobs/${id}/cancel`);
  },

  async archiveJob(id: string) {
    return axios.post(`${API_BASE}/jobs/${id}/archive`);
  },

  async unarchiveJob(id: string) {
    return axios.post(`${API_BASE}/jobs/${id}/unarchive`);
  },

  async pauseAllJobs() {
    return axios.post(`${API_BASE}/jobs/pause-all`);
  },

  async resumeAllJobs() {
    return axios.post(`${API_BASE}/jobs/resume-all`);
  },

  async cancelAllJobs() {
    return axios.post(`${API_BASE}/jobs/cancel-all`);
  },

  async listWorkflows() {
    const response = await axios.get<AdminWorkflowsResponse>(`${API_BASE}/workflows`);
    return response.data;
  },

  async getWorkflow(id: string) {
    const response = await axios.get<AdminWorkflowDetailResponse>(`${API_BASE}/workflows/${id}`);
    return response.data;
  },

  async listBenchmarks(
    params: {
      benchmark?: string;
      workflow?: string;
      status?: string;
      split?: string;
      include_optimizer?: boolean;
    } = {},
  ) {
    const response = await axios.get<AdminBenchmarksResponse>(`${API_BASE}/benchmarks`, { params });
    return response.data;
  },

  async getBenchmark(id: string, params: Record<string, string | number | boolean> = {}) {
    const response = await axios.get<AdminBenchmarkDetailResponse>(`${API_BASE}/benchmarks/${id}`, { params });
    return response.data;
  },

  async getWrongAnswerAnalysis(runId: string) {
    const response = await axios.get<WrongAnswerAnalysisResponse>(`${API_BASE}/benchmarks/${runId}/analysis`);
    return response.data;
  },

  async getBenchmarkItem(runID: string, itemID: string) {
    const response = await axios.get<AdminBenchmarkItemDetailResponse>(`${API_BASE}/benchmarks/${runID}/items`, {
      params: { item_id: itemID },
    });
    return response.data;
  },

  async importBenchmarks() {
    return axios.post(`${API_BASE}/benchmarks/import`);
  },

  async runBenchmarks(payload: {
    benchmarks: string[];
    workflows: string[];
    source?: string;
    run_set?: string;
    split?: string;
    limit?: number;
    concurrency?: number;
    max_non_letter_retries?: number;
    max_transient_retries?: number;
  }) {
    const form = new URLSearchParams();
    form.set('benchmarks', payload.benchmarks.join(','));
    form.set('workflows', payload.workflows.join(','));
    if (payload.source) form.set('source', payload.source);
    if (payload.run_set) form.set('run_set', payload.run_set);
    if (payload.split) form.set('split', payload.split);
    if (payload.limit != null) form.set('limit', String(payload.limit));
    if (payload.concurrency != null) form.set('concurrency', String(payload.concurrency));
    if (payload.max_non_letter_retries != null)
      form.set('max_non_letter_retries', String(payload.max_non_letter_retries));
    if (payload.max_transient_retries != null) form.set('max_transient_retries', String(payload.max_transient_retries));
    return axios.post(`${API_BASE}/benchmarks/run`, form, {
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    });
  },

  async cancelBenchmarkRun() {
    return axios.post(`${API_BASE}/benchmarks/run/cancel`);
  },

  async benchmarkRunnerStatus(): Promise<BenchmarkRunnerStatus> {
    const response = await axios.get<BenchmarkRunnerStatus>(`${API_BASE}/benchmarks/runner-status`);
    return response.data;
  },

  async compareBenchmarks(params: { benchmark?: string; split?: string; control?: string } = {}) {
    const response = await axios.get<AdminBenchmarkComparisonResponse>(`${API_BASE}/benchmarks/compare`, { params });
    return response.data;
  },

  async rerunBenchmarkFailures(
    runId: string,
    opts?: { concurrency?: number; max_non_letter_retries?: number; max_transient_retries?: number },
  ) {
    const form = new URLSearchParams();
    if (opts?.concurrency) form.set('concurrency', String(opts.concurrency));
    if (opts?.max_non_letter_retries != null) form.set('max_non_letter_retries', String(opts.max_non_letter_retries));
    if (opts?.max_transient_retries != null) form.set('max_transient_retries', String(opts.max_transient_retries));
    const response = await axios.post(`${API_BASE}/benchmarks/${runId}/rerun-failures`, form, {
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    });
    return response.data as {
      started: boolean;
      failed_item_count: number;
      run_id: string;
    };
  },

  async compareBenchmarkItems(baseRunId: string, candidateRunId: string) {
    const response = await axios.get<BenchmarkItemComparisonResponse>(`${API_BASE}/benchmarks/compare-items`, {
      params: { base: baseRunId, candidate: candidateRunId },
    });
    return response.data;
  },

  async listOptimizationRuns(params: { status?: string; workflow?: string; limit?: number } = {}) {
    const response = await axios.get<{ runs: OptimizationRun[] }>(`${API_BASE}/optimize/runs`, { params });
    return response.data;
  },

  async getOptimizationRun(id: string) {
    const response = await axios.get<OptimizationRun>(`${API_BASE}/optimize/runs/${id}`);
    return response.data;
  },

  async listOptimizationOrganisms(runID: string, params: { generation?: number; best?: boolean; limit?: number } = {}) {
    const query: Record<string, string | number> = {};
    if (params.generation != null) query.generation = params.generation;
    if (params.best) query.best = 1;
    if (params.limit != null) query.limit = params.limit;
    const response = await axios.get<{ organisms: OptimizationOrganism[]; total: number }>(
      `${API_BASE}/optimize/runs/${runID}/organisms`,
      { params: query },
    );
    return response.data;
  },

  async getOptimizationOrganism(runID: string, organismID: string) {
    const response = await axios.get<{ organism: OptimizationOrganism; param_changes: OptimizationParamChange[] }>(
      `${API_BASE}/optimize/runs/${runID}/organisms/${organismID}`,
    );
    return response.data;
  },

  async getOptimizationRunLineage(runID: string) {
    const response = await axios.get<OptimizationLineageResponse>(`${API_BASE}/optimize/runs/${runID}/lineage`);
    return response.data;
  },

  async getOptimizationLearningLog(runID: string, params: { limit?: number } = {}) {
    const response = await axios.get<{ entries: OptimizationLearningEntry[] }>(
      `${API_BASE}/optimize/runs/${runID}/learning-log`,
      { params },
    );
    return response.data;
  },

  async getOptimizationOrganismLineage(organismID: string) {
    const response = await axios.get<{ organisms: OptimizationOrganism[] }>(
      `${API_BASE}/optimize/organisms/${organismID}/lineage`,
    );
    return response.data;
  },

  async getOptimizationMutationArtifacts(organismID: string) {
    const response = await axios.get<{ artifacts: OptimizationMutationArtifact[] }>(
      `${API_BASE}/optimize/organisms/${organismID}/mutation-artifacts`,
    );
    return response.data;
  },

  async compareOptimizationRuns(ids: string[]) {
    const response = await axios.get<OptimizationCompareResponse>(`${API_BASE}/optimize/compare`, {
      params: { ids: ids.join(',') },
    });
    return response.data;
  },

  async testWorkflow(workflow: Record<string, unknown>) {
    const response = await axios.post(`${API_BASE}/test/workflow`, { workflow });
    return response.data;
  },

  // Benchloop observability
  async getBenchloopStatus() {
    const response = await axios.get<BenchloopStatusResponse>(`${API_BASE}/benchloop/status`);
    return response.data;
  },

  async getBenchloopTranscript(params: {
    session_id: string;
    include_user?: boolean;
    include_tools?: boolean;
    max_messages?: number;
  }) {
    const response = await axios.get<BenchloopTranscriptResponse>(`${API_BASE}/benchloop/transcript`, { params });
    return response.data;
  },

  async getBenchloopLog(params: { session_id: string; attempt: number; tail?: number; log_path?: string }) {
    const response = await axios.get<BenchloopLogResponse>(`${API_BASE}/benchloop/log`, { params });
    return response.data;
  },

  async getBenchloopMemory(params?: { session_id?: string; max_lines?: number }) {
    const response = await axios.get<BenchloopMemoryResponse>(`${API_BASE}/benchloop/memory`, { params });
    return response.data;
  },

  async getBenchloopArchive(sessionID: string) {
    const response = await axios.get<BenchloopStatusResponse>(`${API_BASE}/benchloop/archive/${sessionID}`);
    return response.data;
  },
};
