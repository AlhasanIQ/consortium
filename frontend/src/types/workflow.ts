// TypeScript definitions for workflow types matching the backend
import type React from 'react';

// Node types used by frontend editor and backend workflow runtime.
export type NodeType =
  | 'input'
  | 'agent'
  | 'agent_run'
  | 'novo_run'
  | 'contract_extract'
  | 'child_workflow'
  | 'workflow_ref'
  | 'operation'
  | 'conditional'
  | 'aggregation'
  | 'aggregation_frame'
  | 'result';

export const DEFAULT_NOVOMO_SANDBOX = 'docker' as const;

export type NovomoHandoffKind = 'job_run' | 'job' | 'novo_run' | 'task';
export type NovomoHandoffMode = 'auto' | 'none' | 'upstream' | 'explicit';

export interface NovomoHandoffRef {
  kind: NovomoHandoffKind;
  id: string;
  policy?: string;
}

// Aggregation methods matching backend AggregationMethod constants
export type AggregationMethod =
  | 'collect' // Join all inputs with separator (default)
  | 'judge' // Single LLM judge selects best response
  | 'scoring' // Rubric-based scoring
  | 'synthesis' // LLM synthesis
  | 'peer_matrix' // Each agent scores every other agent
  | 'majority_vote' // Extract discrete answers and pick majority winner
  | 'debate_decide'; // Group by answer into camps, adjudicate

// Rubric criterion for scoring aggregation
export interface RubricCriterion {
  name: string;
  weight: number;
  description: string;
}

// Aggregation configuration by method
export interface CollectConfig {
  separator?: string; // Default: "\n---\n"
}

export interface JudgeConfig {
  judge_model?: string;
  system_prompt?: string;
  prompt?: string;
  temperature?: number;
  max_tokens?: number;
  repair_max_tokens?: number;
  openRouterProvider?: OpenRouterProviderConfig;
  openRouterReasoning?: OpenRouterReasoningConfig;
}

export interface ScoringConfig {
  rubric_mode?: 'static' | 'dynamic';
  rubric?: RubricCriterion[];
  scoring_model?: string;
  system_prompt?: string;
  prompt?: string;
  temperature?: number;
  max_tokens?: number;
  openRouterProvider?: OpenRouterProviderConfig;
  openRouterReasoning?: OpenRouterReasoningConfig;
}

export interface SynthesisConfig {
  model?: string;
  system_prompt?: string;
  prompt?: string;
  temperature?: number;
  max_tokens?: number;
  openRouterProvider?: OpenRouterProviderConfig;
  openRouterReasoning?: OpenRouterReasoningConfig;
}

export interface PeerMatrixConfig {
  rubric_mode?: 'static' | 'dynamic';
  rubric_model?: string;
  eval_system_prompt?: string;
  eval_prompt?: string;
  temperature?: number;
  max_tokens?: number;
  normalization?: 'none';
  max_parallel?: number;
  rubric?: RubricCriterion[];
  openRouterProvider?: OpenRouterProviderConfig;
  openRouterReasoning?: OpenRouterReasoningConfig;
}

export interface MajorityVoteConfig {
  extraction_strategy?: 'regex' | 'first_letter' | 'json_field';
  extraction_pattern?: string;
  tie_breaker_method?: 'synthesis' | 'first' | 'error';
  tie_breaker?: 'synthesis' | 'first' | 'error'; // Legacy frontend key; normalized before submission.
  tie_breaker_model?: string;
  tie_breaker_temperature?: number;
  system_prompt?: string;
  prompt?: string;
  max_tokens?: number;
}

export interface DebateDecideConfig {
  judge_model?: string;
  system_prompt?: string;
  prompt?: string;
  extraction_strategy?: 'regex' | 'first_letter' | 'json_field';
  extraction_pattern?: string;
  temperature?: number;
  max_tokens?: number;
  repair_max_tokens?: number;
  openRouterProvider?: OpenRouterProviderConfig;
  openRouterReasoning?: OpenRouterReasoningConfig;
}

export type AggregationConfig =
  | CollectConfig
  | JudgeConfig
  | ScoringConfig
  | SynthesisConfig
  | PeerMatrixConfig
  | MajorityVoteConfig
  | DebateDecideConfig;

export interface OpenRouterProviderConfig {
  order?: string[];
  only?: string[];
  ignore?: string[];
  allow_fallbacks?: boolean;
  require_parameters?: boolean;
}

export interface OpenRouterReasoningConfig {
  effort?: 'none' | 'low' | 'medium' | 'high';
}

/** Type-safe accessor for aggregation config. Returns the config cast to T, or {} if undefined. */
export function getAggConfig<T extends AggregationConfig>(config: AggregationConfig | undefined): T {
  return (config ?? {}) as T;
}

/** Merge a partial update into an existing aggregation config. */
export function mergeAggConfig<T extends AggregationConfig>(base: AggregationConfig | undefined, patch: Partial<T>): T {
  return { ...(base ?? {}), ...patch } as T;
}

export const DEFAULT_AGGREGATION_MAX_TOKENS = -1;
export const DEFAULT_AGGREGATION_REPAIR_MAX_TOKENS = 256;

function hasNumericField(config: Record<string, unknown>, ...keys: string[]): boolean {
  for (const key of keys) {
    if (typeof config[key] === 'number') {
      return true;
    }
  }
  return false;
}

function hasNonEmptyStringField(config: Record<string, unknown>, key: string): boolean {
  return typeof config[key] === 'string' && config[key].trim() !== '';
}

export function ensureAggregationConfigDefaults(
  method: AggregationMethod | undefined,
  config: AggregationConfig | undefined,
): AggregationConfig | undefined {
  if (!method || method === 'collect' || method === 'majority_vote') {
    return config;
  }

  const normalized = { ...(config ?? {}) } as Record<string, unknown>;

  if (!hasNumericField(normalized, 'max_tokens', 'maxTokens')) {
    normalized.max_tokens = DEFAULT_AGGREGATION_MAX_TOKENS;
  }

  if (method === 'synthesis' && !hasNonEmptyStringField(normalized, 'model')) {
    normalized.model = DEFAULT_MODEL;
  }
  if ((method === 'judge' || method === 'debate_decide') && !hasNonEmptyStringField(normalized, 'judge_model')) {
    normalized.judge_model = DEFAULT_MODEL;
  }
  if (method === 'scoring' && !hasNonEmptyStringField(normalized, 'scoring_model')) {
    normalized.scoring_model = DEFAULT_MODEL;
  }

  if (
    (method === 'judge' || method === 'debate_decide') &&
    !hasNumericField(normalized, 'repair_max_tokens', 'repairMaxTokens')
  ) {
    normalized.repair_max_tokens = DEFAULT_AGGREGATION_REPAIR_MAX_TOKENS;
  }

  return normalized as AggregationConfig;
}

// Evaluation matrix for peer_matrix aggregation (from backend)
export interface EvaluationMatrix {
  raw_scores: Record<string, Record<string, number>>; // [reviewer][candidate] -> score
  normalized_scores: Record<string, Record<string, number>>; // After bias correction
  final_scores: Record<string, number>; // Averaged per candidate
  reviewer_bias: Record<string, number>; // Bias stats per reviewer
  invalid_count: number; // Number of failed evaluations
}

// Aggregation details returned from backend after completion
export interface AggregationDetails {
  method: AggregationMethod;
  winner?: string; // ID of winning agent
  scores?: Record<string, number>; // agent_id -> score
  reasoning?: string; // Judge/scoring explanation
  evalMatrix?: EvaluationMatrix; // For peer_matrix
  agreement_ratio?: number; // Fraction of agents that agree (0.0-1.0)
  consensus_answer?: string; // Most common extracted answer
  dissenting_ids?: string[]; // Agent IDs that disagree
}

// Workflow layer derived from the seed ID prefix convention:
// L0 aggregation internals, L1 reasoning primitives, L2 composite strategies,
// L3 benchmark harnesses, '' uncategorized.
export type WorkflowLayer = 'L0' | 'L1' | 'L2' | 'L3' | '';

// Seed workflow info from /api/workflows/seeds
export interface SeedWorkflowInfo {
  id: string;
  name: string;
  description: string;
  aggregation_method: AggregationMethod;
  agent_count: number;
  layer: WorkflowLayer;
}

export interface WorkflowCanvasNode {
  id: string;
  type: NodeType;
  config: NodeConfig;
}

export interface NodeConfig {
  // Common fields
  name?: string;
  description?: string;
  alias?: string; // User-defined reference ID for {{alias}} syntax

  // Input node specific
  schema?: Record<string, unknown>;

  // Agent node specific
  provider?: string;
  model?: string;
  openRouterProvider?: OpenRouterProviderConfig;
  openRouterReasoning?: OpenRouterReasoningConfig;
  systemPrompt?: string; // System message - defines the agent's role/persona
  userPrompt?: string; // User message - the actual task/input (optional, defaults to input variables)
  prompt?: string; // Novomo agent_run task prompt
  harness?: 'claude-code' | 'codex';
  taskId?: string; // Existing Novomo Task ID for Superagent wake
  taskSummary?: string;
  identity?: string;
  image?: string;
  sandbox?: 'host' | 'docker';
  runtimeUrl?: string;
  graceSeconds?: number;
  repoSpecsJson?: string;
  workSourceJson?: string;
  inheritFromMode?: NovomoHandoffMode;
  inheritFromNodeId?: string;
  inheritFromKind?: NovomoHandoffKind;
  inheritFromId?: string;
  inheritFromPolicy?: string;
  temperature?: number;
  maxTokens?: number;
  sourceVariable?: string; // contract_extract node source key in workflow context
  extractionPatterns?: string[]; // contract_extract regex patterns evaluated in order

  // Retry policy (agent, contract_extract, and child_workflow nodes).
  // Builder-edited nodes use the flat retry* fields below; seed/API workflows
  // carry a nested retryPolicy object. resolveRetryPolicy() reconciles both.
  retryMaxAttempts?: number;
  retryBackoffMs?: number;
  retryBackoffMultiplier?: number;
  retryMaxBackoffMs?: number;
  retryPolicy?: RetryPolicyConfig;

  // Child workflow node specific
  childWorkflowId?: string;
  childInputTemplate?: Record<string, string>;
  childOutputKey?: string;
  childAwait?: boolean;
  timeoutSeconds?: number;

  // Workflow reference node specific
  workflowId?: string;
  workflowRefId?: string;
  inputTemplate?: Record<string, string>;
  outputKey?: string;

  // Deterministic operation node specific
  operationType?: string;
  operationConfig?: Record<string, unknown>;

  // Conditional node specific
  condition?: string;

  // Result node specific (formerly output)
  outputSchema?: Record<string, unknown>;
  outputFormat?: 'json' | 'text' | 'structured';
  aggregationMethod?: AggregationMethod;
  aggregationWorkflowId?: string;
  aggregationConfig?: AggregationConfig;
  benchmarkOutputPackaging?: boolean;

  // Read-only compiled preview frame. This is UI-only and filtered from saved
  // workflow files and runtime submission.
  aggregationFrameTitle?: string;
  aggregationFrameMethod?: AggregationMethod;
  aggregationFrameSourceWorkflowId?: string;
  aggregationFrameLLMJobCount?: number;
  aggregationFrameTopLevelLLMJobCount?: number;
  aggregationFrameConditionalLLMJobCount?: number;
  aggregationFrameOperationCount?: number;
  aggregationPreviewShifts?: Record<string, { x: number; y: number }>;
  aggregationBranch?: 'true' | 'false';
  aggregationBranchParentId?: string;

  // Aggregation workflow expansion/fork display metadata. Expanded nodes are
  // read-only builder previews and are filtered from save/submit; forked nodes
  // are normal editable graph nodes with non-binding provenance metadata.
  aggregationInternalState?: 'expanded' | 'forked';
  aggregationAnchorId?: string;
  sourceLocked?: boolean;
  sourceWorkflowId?: string;
  sourceWorkflowVersion?: string;
  sourceWorkflowHash?: string;
  sourceNodeId?: string;
  forkedFromWorkflowId?: string;
  forkedFromSourceVersion?: string;
  forkedFromSourceHash?: string;
  boundaryType?: 'compiled_workflow_ref' | 'child_workflow';
}

export interface WorkflowEdge {
  id: string;
  source: string;
  target: string;
  sourceHandle?: string;
  targetHandle?: string;
  label?: string;
}

// Fallbacks for prompt-node fields the backend now requires explicitly. Seed/
// API workflows always carry real values (these only apply to builder-authored
// nodes that lack the field — e.g. the agent config panel has no timeout input).
export const DEFAULT_PROMPT_TEMPERATURE = 0;
export const DEFAULT_PROMPT_MAX_TOKENS = 1000;
export const DEFAULT_PROMPT_TIMEOUT_SECONDS = 600;
export const DEFAULT_MODEL = 'deepseek/deepseek-v4-flash';

// resolveRetryPolicy produces an explicit retry_policy for submission. The
// backend rejects nodes without one (no implicit defaults), so every converter
// must emit it. Nested seed/API policies win and are preserved whole (including
// retryable_errors / adaptive_reasoning); otherwise the flat builder-UI fields
// are assembled with sane fallbacks.
export function resolveRetryPolicy(config: NodeConfig): RetryPolicyConfig {
  const nested = config.retryPolicy;
  if (nested && typeof nested.max_attempts === 'number') {
    return { backoff_ms: 1000, backoff_multiply: 2.0, max_backoff_ms: 30000, ...nested };
  }
  return {
    max_attempts: config.retryMaxAttempts ?? 3,
    backoff_ms: config.retryBackoffMs ?? 1000,
    backoff_multiply: config.retryBackoffMultiplier ?? 2.0,
    max_backoff_ms: config.retryMaxBackoffMs ?? 30000,
  };
}

// Typed metadata for workflow nodes (matches backend NodeDisplayMeta)
export interface NodeMetadata {
  label?: string;
  name?: string;
  description?: string;
  input_ids?: string[];
  output_name?: string;
  output_format?: string;
  aggregation_anchor_id?: string;
  aggregation_method?: AggregationMethod;
  aggregation_group_node_id?: string;
  benchmark_output_packaging?: boolean;
  presentation_result_id?: string;
  source_workflow_id?: string;
  source_workflow_hash?: string;
  source_node_id?: string;
  source_parent_node_id?: string;
  forked_from_workflow_id?: string;
  forked_from_source_hash?: string;
  openrouter_provider?: OpenRouterProviderConfig;
  openrouter_reasoning?: OpenRouterReasoningConfig;
  source_variable?: string;
  extraction_patterns?: string[];
  extraction_method?: string;
}

// RetryPolicyConfig mirrors the backend workflow.RetryPolicy wire shape. Extra
// optional fields (retryable_errors, adaptive_reasoning) are preserved verbatim
// so seed-defined policies survive the round-trip to /workflows/submit.
export interface RetryPolicyConfig {
  max_attempts: number;
  backoff_ms?: number;
  backoff_multiply?: number;
  max_backoff_ms?: number;
  retryable_errors?: string[];
  adaptive_reasoning?: Record<string, unknown>;
}

export interface WorkflowNode {
  id?: string;
  type:
    | 'prompt'
    | 'agent_run'
    | 'novo_run'
    | 'contract_extract'
    | 'conditional'
    | 'result'
    | 'workflow_ref'
    | 'operation'
    | 'child_workflow'
    | 'output'; // 'output' is legacy, prefer 'result'
  prompt?: string;
  harness?: string;
  task_id?: string;
  task_summary?: string;
  identity?: string;
  image?: string;
  sandbox?: string;
  runtime_url?: string;
  grace_seconds?: number;
  repo_specs?: Array<Record<string, unknown>>;
  work_source?: Record<string, unknown>;
  inherit_from?: NovomoHandoffRef;
  inherit_from_node_id?: string;
  inherit_from_policy?: string;
  inherit_from_workflow_task?: boolean;
  system_prompt?: string; // System message for LLM context
  model?: string;
  temperature?: number; // LLM temperature setting
  max_tokens?: number; // LLM max tokens setting
  timeout_seconds?: number;
  retry_policy?: RetryPolicyConfig;
  condition?: string;
  true_branch?: WorkflowNode;
  false_branch?: WorkflowNode;
  child_workflow_id?: string;
  child_input_template?: Record<string, string>;
  child_output_key?: string;
  child_await?: boolean;
  workflow_ref_id?: string;
  input_template?: Record<string, string>;
  output_key?: string;
  operation_type?: string;
  operation_config?: Record<string, unknown>;
  output_name?: string;
  output_format?: string;
  aggregation_method?: AggregationMethod;
  aggregation_config?: AggregationConfig;
  metadata?: NodeMetadata;
}

export interface Workflow {
  id: string;
  name?: string;
  description?: string;
  nodes: WorkflowNode[];
  edges?: WorkflowEdge[];
  context?: Record<string, unknown>;
  created_at?: string;
  updated_at?: string;
}

export interface CompilePreviewConditionalJob {
  id: string;
  parent_node_id: string;
  // Expected branch labels are currently "true" and "false".
  branch: string;
  // Current generated branch jobs are prompt nodes, but the wire shape is open.
  type: string;
  model?: string;
  system_prompt?: string;
  prompt?: string;
  temperature?: number;
  max_tokens?: number;
  timeout_seconds?: number;
  retry_policy?: RetryPolicyConfig;
  label?: string;
}

export interface CompilePreviewAggregationGroup {
  anchor_node_id: string;
  method: AggregationMethod | '';
  source_workflow_id: string;
  terminal_node_id: string;
  presentation_result_id?: string;
  input_node_ids: string[];
  node_ids: string[];
  llm_job_count: number;
  top_level_llm_job_count: number;
  conditional_llm_job_count: number;
  conditional_llm_jobs: CompilePreviewConditionalJob[];
  operation_count: number;
}

// Compile previews return the full compiled DAG, including runtime-only node
// types that are not directly authored in the builder.
export type CompilePreviewWorkflowNode = Omit<WorkflowNode, 'type'> & {
  type: WorkflowNode['type'] | 'input' | 'aggregation' | string;
};

export interface CompilePreviewResponse {
  workflow_id: string;
  nodes: CompilePreviewWorkflowNode[];
  edges: WorkflowEdge[];
  aggregation_groups: CompilePreviewAggregationGroup[];
}

// React Flow types
export interface NodeData {
  type: NodeType;
  label: string;
  config: NodeConfig;
  onConfigChange?: (config: NodeConfig) => void;
  [key: string]: unknown; // Index signature for React Flow v12 compatibility
}

export interface FlowNode {
  id: string;
  type?: string; // Optional to match React Flow's Node type
  position: { x: number; y: number };
  data: NodeData;
  draggable?: boolean;
  selectable?: boolean;
  zIndex?: number;
  parentId?: string;
  style?: React.CSSProperties;
  measured?: {
    width?: number;
    height?: number;
  };
}

export interface FlowEdge {
  id: string;
  source: string;
  target: string;
  sourceHandle?: string | null; // Match React Flow's Edge type
  targetHandle?: string | null; // Match React Flow's Edge type
  label?: React.ReactNode; // Match React Flow's Edge type
  hidden?: boolean;
  data?: Record<string, unknown>;
}
