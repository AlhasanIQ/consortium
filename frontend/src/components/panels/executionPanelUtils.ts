import type { ReplayJobInfo, ReplayNodeResult } from '../../stores/workflowStore';
import type { ScrubberItem } from './TimelineScrubber';

export interface ReplayNodeGroup {
  baseId: string;
  primary?: ReplayNodeResult;
  children: ReplayNodeResult[];
  order: number;
}

export interface TraceEvent {
  type?: string;
  name?: string;
  label?: string;
  timestamp?: string;
  data?: unknown;
}

// Color constant objects
export const statusColors: Record<string, string> = {
  completed: '#2e7d32',
  failed: '#c62828',
  running: '#1976d2',
  pending: '#f57c00',
  cancelled: '#757575',
};

export const kindColors: Record<string, string> = {
  node: '#1976d2',
  call: '#7b1fa2',
  decision: '#e65100',
};

export const traceStatusColors: Record<string, string> = {
  ok: '#2e7d32',
  error: '#c62828',
  timeout: '#f57c00',
  cancelled: '#757575',
};

export const getBaseNodeId = (nodeId: string): string => {
  const parts = nodeId.split('__');
  return parts[0] || nodeId;
};

export const deriveGroupStatus = (nodeRuns: ReplayNodeResult[]): string => {
  if (nodeRuns.some((nodeRun) => nodeRun.status === 'failed' || nodeRun.status === 'cancelled')) {
    return 'failed';
  }
  if (nodeRuns.some((nodeRun) => nodeRun.status === 'running' || nodeRun.status === 'pending')) {
    return 'running';
  }
  if (nodeRuns.some((nodeRun) => nodeRun.status === 'completed')) {
    return 'completed';
  }
  return 'pending';
};

export const parsePromptMessages = (prompt?: string): { role: string; content: string }[] => {
  if (!prompt) return [];
  try {
    const parsed = JSON.parse(prompt);
    if (Array.isArray(parsed)) {
      return parsed
        .map((msg) => ({
          role: typeof msg?.role === 'string' ? msg.role : 'message',
          content: typeof msg?.content === 'string' ? msg.content : JSON.stringify(msg),
        }))
        .filter((msg) => msg.content);
    }
    if (typeof parsed === 'object' && parsed !== null) {
      return [
        {
          role: 'prompt',
          content: JSON.stringify(parsed, null, 2),
        },
      ];
    }
  } catch {
    // Fall through to raw prompt
  }
  return [{ role: 'prompt', content: prompt }];
};

export const extractTraceEvents = (metadata?: string): TraceEvent[] => {
  if (!metadata) return [];
  try {
    const parsed = JSON.parse(metadata);
    const candidates = [parsed?.trace, parsed?.events, parsed?.tool_calls, parsed?.observations];
    for (const candidate of candidates) {
      if (Array.isArray(candidate)) {
        return candidate as TraceEvent[];
      }
    }
  } catch {
    return [];
  }
  return [];
};

export const formatDuration = (start?: string | null, end?: string | null): string | null => {
  if (!start || !end) return null;
  const startMs = new Date(start).getTime();
  const endMs = new Date(end).getTime();
  if (Number.isNaN(startMs) || Number.isNaN(endMs) || endMs < startMs) {
    return null;
  }
  const durationMs = endMs - startMs;
  if (durationMs < 1000) {
    return `${durationMs}ms`;
  }
  const seconds = durationMs / 1000;
  if (seconds < 60) {
    return `${seconds.toFixed(1)}s`;
  }
  const minutes = Math.floor(seconds / 60);
  const remainder = seconds % 60;
  return `${minutes}m ${remainder.toFixed(0)}s`;
};

export function getMessageColor(type: string): string {
  switch (type) {
    case 'error':
      return '#ffebee';
    case 'complete':
      return '#e8f5e9';
    case 'node_complete':
      return '#e3f2fd';
    case 'node_start':
      return '#fff9c4';
    default:
      return 'white';
  }
}

export function getBorderColor(type: string): string {
  switch (type) {
    case 'error':
      return '#c62828';
    case 'complete':
      return '#2e7d32';
    case 'node_complete':
      return '#1976d2';
    case 'node_start':
      return '#f57c00';
    case 'output':
      return '#9c27b0';
    default:
      return '#999';
  }
}

export function getReplayStatusColor(status: string): string {
  switch (status) {
    case 'completed':
      return '#2e7d32';
    case 'failed':
      return '#c62828';
    case 'running':
      return '#1976d2';
    case 'pending':
      return '#f57c00';
    case 'cancelled':
      return '#757575';
    default:
      return '#9e9e9e';
  }
}

export function formatNumber(value?: number): string {
  if (value === undefined || value === null) return '0';
  return value.toLocaleString();
}

export function formatCost(value?: number): string {
  if (value === undefined || value === null) return '$0.000000';
  return `$${value.toFixed(6)}`;
}

export function formatLatency(value?: number): string {
  if (value === undefined || value === null) return '-';
  return `${Math.round(value)}ms`;
}

export function formatMetadata(metadata: string): string {
  try {
    const parsed = JSON.parse(metadata);
    return JSON.stringify(parsed, null, 2);
  } catch {
    return metadata;
  }
}

export function groupReplayNodes(replayNodes: ReplayNodeResult[]): ReplayNodeGroup[] {
  const groups = new Map<string, ReplayNodeGroup>();

  replayNodes.forEach((node, index) => {
    const baseId = node.parent_node_id || getBaseNodeId(node.node_id);
    const isChild = !!node.parent_node_id || node.node_id !== baseId;
    const existing = groups.get(baseId);
    if (existing) {
      if (!isChild) {
        existing.primary = node;
      } else {
        existing.children.push(node);
      }
      existing.order = Math.min(existing.order, index);
      return;
    }

    groups.set(baseId, {
      baseId,
      primary: !isChild ? node : undefined,
      children: isChild ? [node] : [],
      order: index,
    });
  });

  return Array.from(groups.values()).sort((a, b) => a.order - b.order);
}

export function buildFlatReplayNodes(groupedReplayNodes: ReplayNodeGroup[]): ScrubberItem[] {
  const items: ScrubberItem[] = [];
  for (const group of groupedReplayNodes) {
    if (group.primary) {
      items.push({
        nodeId: group.primary.node_id,
        label: group.primary.node_name || group.primary.node_label || group.primary.node_id,
        status: group.primary.status,
        isSubnode: false,
      });
    }
    for (const child of group.children) {
      items.push({
        nodeId: child.node_id,
        label: child.node_name || child.node_label || child.node_id,
        status: child.status,
        isSubnode: true,
      });
    }
  }
  return items;
}

export function computeReplayTotals(
  replayJob: ReplayJobInfo | null,
  replayNodes: ReplayNodeResult[],
): { tokensInput: number; tokensOutput: number; tokensTotal: number; cost: number; latency: number } {
  const parentsWithChildren = new Set<string>();
  for (const node of replayNodes) {
    if (node.parent_node_id) {
      parentsWithChildren.add(node.parent_node_id);
    }
  }

  const sums = replayNodes.reduce(
    (acc, node) => {
      if (parentsWithChildren.has(node.node_id)) {
        return acc;
      }
      acc.tokensInput += node.tokens_input || 0;
      acc.tokensOutput += node.tokens_output || 0;
      acc.cost += node.cost || 0;
      acc.latency += node.latency_ms || 0;
      return acc;
    },
    { tokensInput: 0, tokensOutput: 0, cost: 0, latency: 0 },
  );

  return {
    tokensInput: replayJob?.tokens_input ?? sums.tokensInput,
    tokensOutput: replayJob?.tokens_output ?? sums.tokensOutput,
    tokensTotal: replayJob?.tokens_total ?? sums.tokensInput + sums.tokensOutput,
    cost: replayJob?.cost ?? sums.cost,
    latency: sums.latency,
  };
}
