// Canonical WebSocket event types - must match backend pkg/events/events.go

// Event type constants
export const EventStatus = 'status' as const;
export const EventJobCreated = 'job_created' as const;
export const EventNodeStart = 'node_start' as const;
export const EventNodeComplete = 'node_complete' as const;
export const EventNodeFailed = 'node_failed' as const;
export const EventNodeRetryBackoff = 'node_retry_backoff' as const;
export const EventNodeRetryStart = 'node_retry_start' as const;
export const EventNodeRetryExhausted = 'node_retry_exhausted' as const;
export const EventComplete = 'complete' as const;
export const EventError = 'error' as const;
export const EventCancelled = 'cancelled' as const;
export const EventAgentPlanCreated = 'agent_plan_created' as const;
export const EventAgentIterationStarted = 'agent_iteration_started' as const;
export const EventAgentToolCalled = 'agent_tool_called' as const;
export const EventAgentToolResult = 'agent_tool_result' as const;
export const EventAgentIterationCompleted = 'agent_iteration_completed' as const;
export const EventAgentBranchCreated = 'agent_branch_created' as const;
export const EventAgentBranchPruned = 'agent_branch_pruned' as const;
export const EventAgentBranchSelected = 'agent_branch_selected' as const;
export const EventAgentTerminated = 'agent_terminated' as const;
export const EventAgentFailed = 'agent_failed' as const;
export const EventMemoryRead = 'memory_read' as const;
export const EventMemoryWrite = 'memory_write' as const;
export const EventRetrievalExecuted = 'retrieval_executed' as const;
export const EventRetrievalResultUsed = 'retrieval_result_used' as const;

// Event type union
export type EventType =
  | typeof EventStatus
  | typeof EventJobCreated
  | typeof EventNodeStart
  | typeof EventNodeComplete
  | typeof EventNodeFailed
  | typeof EventNodeRetryBackoff
  | typeof EventNodeRetryStart
  | typeof EventNodeRetryExhausted
  | typeof EventComplete
  | typeof EventError
  | typeof EventCancelled
  | typeof EventAgentPlanCreated
  | typeof EventAgentIterationStarted
  | typeof EventAgentToolCalled
  | typeof EventAgentToolResult
  | typeof EventAgentIterationCompleted
  | typeof EventAgentBranchCreated
  | typeof EventAgentBranchPruned
  | typeof EventAgentBranchSelected
  | typeof EventAgentTerminated
  | typeof EventAgentFailed
  | typeof EventMemoryRead
  | typeof EventMemoryWrite
  | typeof EventRetrievalExecuted
  | typeof EventRetrievalResultUsed;

// Job status constants
export const JobStatusPending = 'pending' as const;
export const JobStatusRunning = 'running' as const;
export const JobStatusCompleted = 'completed' as const;
export const JobStatusFailed = 'failed' as const;
export const JobStatusCancelled = 'cancelled' as const;

// Job status type union
export type JobStatus =
  | typeof JobStatusPending
  | typeof JobStatusRunning
  | typeof JobStatusCompleted
  | typeof JobStatusFailed
  | typeof JobStatusCancelled;

// Helper functions
export function isTerminalStatus(status: string): boolean {
  return status === JobStatusCompleted || status === JobStatusFailed || status === JobStatusCancelled;
}

export function isTerminalEvent(eventType: string): boolean {
  return eventType === EventComplete || eventType === EventError || eventType === EventCancelled;
}

// WebSocket event interface
export interface WebSocketEvent {
  type: EventType;
  job_id: string;
  node_id?: string;
  message: string;
  output?: string;
  error?: string;
  code?: string;
  timestamp: string;
  tokens_input?: number;
  tokens_output?: number;
  cost?: number;
  latency_ms?: number;
  data?: Record<string, unknown>;
}

// Trace span types (from GET /api/jobs/{id}/trace)
export interface TraceSpan {
  id?: number;
  job_id: string;
  node_id: string;
  span_id: string;
  parent_span_id?: string;
  kind: 'node' | 'call' | 'decision';
  status: 'ok' | 'error' | 'timeout' | 'cancelled';
  started_at: string;
  duration_ms?: number;
  attributes: Record<string, unknown>;
  tool_call_id?: string;
}

export interface TraceNodeGroup {
  node_id: string;
  spans: TraceSpan[];
}

export interface TraceResponse {
  job_id: string;
  node_groups: TraceNodeGroup[];
}

// Fingerprint types (from GET /api/jobs/{id}/config and /api/jobs/{id}/diff/{id2})

export type DiffCategory = 'structure' | 'model' | 'prompt' | 'decoding' | 'aggregation' | 'limits';

export interface FieldDiff {
  path: string;
  left: unknown;
  right: unknown;
  category: DiffCategory;
}

export interface ConfigDiff {
  left_hash: string;
  right_hash: string;
  identical: boolean;
  diffs: FieldDiff[];
}

export interface NormalizedNodeConfig {
  node_id: string;
  node_type: string;
  model?: string;
  temperature: number;
  max_tokens: number;
  timeout_seconds: number;
  system_prompt?: string;
  prompt?: string;
  aggregation_method?: string;
}

export interface NormalizedConfig {
  workflow_id: string;
  config_hash: string;
  nodes: NormalizedNodeConfig[];
  edges: { source: string; target: string }[];
  limits?: { max_cost_usd?: number; max_tokens?: number; max_input_tokens?: number; max_output_tokens?: number };
}
