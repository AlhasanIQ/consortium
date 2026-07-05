import type { Edge, Node } from '@xyflow/react';
import type { NodeData } from '../types/workflow';

export interface ValidationError {
  nodeId?: string;
  edgeId?: string;
  message: string;
  severity: 'error' | 'warning';
}

export interface ValidationResult {
  isValid: boolean;
  errors: ValidationError[];
}

/**
 * Validate edge connections based on node types
 */
export function isValidConnection(
  source: Node<NodeData>,
  target: Node<NodeData>,
  sourceHandle?: string | null,
): { valid: boolean; message?: string } {
  // Input nodes can only be sources
  if (target.data.type === 'input') {
    return { valid: false, message: 'Cannot connect to an Input node' };
  }

  // Result nodes can only be targets
  if (source.data.type === 'result') {
    return { valid: false, message: 'Cannot connect from a Result node' };
  }

  // Conditional nodes must use specific handles
  if (source.data.type === 'conditional') {
    if (!sourceHandle || (sourceHandle !== 'true' && sourceHandle !== 'false')) {
      return { valid: false, message: 'Conditional node requires true/false handle' };
    }
  }

  // Agent nodes should have model configured
  if ((source.data.type === 'agent' || source.data.type === 'contract_extract') && !source.data.config.model) {
    return { valid: true, message: 'Warning: Agent node has no model configured' };
  }
  if (source.data.type === 'agent_run' && !source.data.config.harness) {
    return { valid: true, message: 'Warning: Novomo Agent node has no harness configured' };
  }

  return { valid: true };
}

/**
 * Detect cycles in the workflow graph using DFS
 */
function hasCycle(nodes: Node<NodeData>[], edges: Edge[]): string[] {
  const adjList = new Map<string, string[]>();
  const visited = new Set<string>();
  const recStack = new Set<string>();
  const cycleNodes: string[] = [];

  // Build adjacency list
  for (const node of nodes) {
    adjList.set(node.id, []);
  }
  edges.forEach((edge) => {
    const neighbors = adjList.get(edge.source) || [];
    neighbors.push(edge.target);
    adjList.set(edge.source, neighbors);
  });

  function dfs(nodeId: string, path: string[]): boolean {
    visited.add(nodeId);
    recStack.add(nodeId);
    path.push(nodeId);

    const neighbors = adjList.get(nodeId) || [];
    for (const neighbor of neighbors) {
      if (!visited.has(neighbor)) {
        if (dfs(neighbor, path)) {
          return true;
        }
      } else if (recStack.has(neighbor)) {
        // Cycle detected
        const cycleStart = path.indexOf(neighbor);
        cycleNodes.push(...path.slice(cycleStart));
        return true;
      }
    }

    recStack.delete(nodeId);
    path.pop();
    return false;
  }

  for (const node of nodes) {
    if (!visited.has(node.id)) {
      if (dfs(node.id, [])) {
        break;
      }
    }
  }

  return cycleNodes;
}

/**
 * Find nodes with no incoming edges (entry points)
 */
function findEntryNodes(nodes: Node<NodeData>[], edges: Edge[]): string[] {
  const nodesWithIncoming = new Set(edges.map((e) => e.target));
  return nodes.filter((node) => !nodesWithIncoming.has(node.id)).map((node) => node.id);
}

function isObjectArrayJSON(value?: string): boolean {
  const text = (value || '').trim();
  if (!text) return true;
  try {
    const parsed = JSON.parse(text);
    return (
      Array.isArray(parsed) && parsed.every((item) => item != null && typeof item === 'object' && !Array.isArray(item))
    );
  } catch {
    return false;
  }
}

function isObjectJSON(value?: string): boolean {
  const text = (value || '').trim();
  if (!text) return true;
  try {
    const parsed = JSON.parse(text);
    return parsed != null && typeof parsed === 'object' && !Array.isArray(parsed);
  } catch {
    return false;
  }
}

function isNovomoHandoffKind(kind?: string): boolean {
  return kind === 'job_run' || kind === 'job' || kind === 'novo_run' || kind === 'task';
}

function isNovomoRunNode(node?: Node<NodeData>): boolean {
  return node?.data.type === 'agent_run' || node?.data.type === 'novo_run';
}

const OPERATION_CONFIG_REQUIREMENTS: Record<string, string[][]> = {
  collect_inputs: [['input_ids'], ['candidates']],
  format_candidates: [['input_ids'], ['candidates']],
  extract_answer: [['text']],
  count_votes: [['answers']],
  group_answer_camps: [['answers'], ['items']],
  select_winner: [
    ['scores'],
    ['selection', 'outputs'],
    ['winner_answer', 'items'],
    ['winner_answer', 'answers'],
    ['vote_result', 'items'],
    ['vote_result', 'answers'],
  ],
  reduce_scores: [['scores']],
  parse_json_field: [['text', 'field']],
  parse_selection: [['text', 'labels']],
};

function hasConfiguredOperationValue(config: Record<string, unknown> | undefined, key: string): boolean {
  if (!config || !(key in config)) return false;
  const value = config[key];
  if (value == null) return false;
  if (typeof value === 'string') return value.trim() !== '';
  if (Array.isArray(value)) return value.length > 0;
  if (typeof value === 'object') return Object.keys(value).length > 0;
  return true;
}

function hasRequiredOperationConfigGroup(config: Record<string, unknown> | undefined, groups: string[][]): boolean {
  return groups.some((group) => group.every((key) => hasConfiguredOperationValue(config, key)));
}

function describeOperationRequirement(groups: string[][]): string {
  if (groups.length === 1 && groups[0].length > 1) {
    return `requires: ${groups[0].join(', ')}`;
  }
  return `requires one of: ${groups.map((group) => group.join(', ')).join('; ')}`;
}

function isUpstreamNode(sourceId: string, targetId: string, nodes: Node<NodeData>[], edges: Edge[]): boolean {
  if (sourceId === targetId) return false;
  if (edges.length === 0) {
    const sourceIndex = nodes.findIndex((node) => node.id === sourceId);
    const targetIndex = nodes.findIndex((node) => node.id === targetId);
    return sourceIndex >= 0 && targetIndex >= 0 && sourceIndex < targetIndex;
  }

  const neighbors = new Map<string, string[]>();
  for (const node of nodes) {
    neighbors.set(node.id, []);
  }
  for (const edge of edges) {
    neighbors.set(edge.source, [...(neighbors.get(edge.source) || []), edge.target]);
  }

  const seen = new Set<string>();
  const queue = [sourceId];
  while (queue.length > 0) {
    const current = queue.shift();
    if (!current || seen.has(current)) continue;
    seen.add(current);
    for (const next of neighbors.get(current) || []) {
      if (next === targetId) return true;
      queue.push(next);
    }
  }
  return false;
}

function validateNovomoHandoff(
  node: Node<NodeData>,
  nodes: Node<NodeData>[],
  edges: Edge[],
  errors: ValidationError[],
) {
  const mode = node.data.config.inheritFromMode || 'auto';
  if (mode === 'none') return;

  if (mode === 'auto') {
    return;
  }

  if (mode === 'explicit') {
    if (!isNovomoHandoffKind(node.data.config.inheritFromKind)) {
      errors.push({
        nodeId: node.id,
        message: `${node.data.label} has an invalid inheritance handle type`,
        severity: 'error',
      });
    }
    if (!node.data.config.inheritFromId?.trim()) {
      errors.push({
        nodeId: node.id,
        message: `${node.data.label} has no inheritance handle ID`,
        severity: 'error',
      });
    }
    return;
  }

  if (mode === 'upstream') {
    const upstreamID = node.data.config.inheritFromNodeId?.trim();
    if (!upstreamID) {
      errors.push({
        nodeId: node.id,
        message: `${node.data.label} has no upstream inheritance node selected`,
        severity: 'error',
      });
      return;
    }
    const upstream = nodes.find((candidate) => candidate.id === upstreamID);
    if (!upstream) {
      errors.push({
        nodeId: node.id,
        message: `${node.data.label} inherits from missing node "${upstreamID}"`,
        severity: 'error',
      });
      return;
    }
    if (!isNovomoRunNode(upstream)) {
      errors.push({
        nodeId: node.id,
        message: `${node.data.label} can only inherit from a Novomo Agent or Superagent node`,
        severity: 'error',
      });
    }
    if (!isUpstreamNode(upstreamID, node.id, nodes, edges)) {
      errors.push({
        nodeId: node.id,
        message: `${node.data.label} can only inherit from an upstream Novomo node`,
        severity: 'error',
      });
    }
    return;
  }

  errors.push({
    nodeId: node.id,
    message: `${node.data.label} has an invalid inheritance mode`,
    severity: 'error',
  });
}

/**
 * Validate the entire workflow
 */
export function validateWorkflow(nodes: Node<NodeData>[], edges: Edge[]): ValidationResult {
  const errors: ValidationError[] = [];

  // Check for empty workflow
  if (nodes.length === 0) {
    errors.push({
      message: 'Workflow is empty',
      severity: 'error',
    });
    return { isValid: false, errors };
  }

  // Check for cycles
  const cycleNodes = hasCycle(nodes, edges);
  if (cycleNodes.length > 0) {
    errors.push({
      message: `Cycle detected: ${cycleNodes.join(' → ')}`,
      severity: 'error',
    });
  }

  // Check for entry nodes
  const entryNodes = findEntryNodes(nodes, edges);
  if (entryNodes.length === 0 && nodes.length > 1) {
    errors.push({
      message: 'No entry point found. Add an Input node or a node with no incoming connections',
      severity: 'error',
    });
  }

  // Check for multiple disconnected entry points
  if (entryNodes.length > 3) {
    errors.push({
      message: `Multiple entry points (${entryNodes.length}). Consider consolidating workflow`,
      severity: 'warning',
    });
  }

  // Check each node
  nodes.forEach((node) => {
    switch (node.data.type) {
      case 'agent':
      case 'contract_extract':
        if (!node.data.config.model) {
          errors.push({
            nodeId: node.id,
            message: `${node.data.type === 'agent' ? 'Agent' : 'Contract Extract'} node "${node.data.label}" has no model configured`,
            severity: 'error',
          });
        }
        if (!node.data.config.systemPrompt) {
          errors.push({
            nodeId: node.id,
            message: `${node.data.type === 'agent' ? 'Agent' : 'Contract Extract'} node "${node.data.label}" has no prompt configured`,
            severity: 'warning',
          });
        }
        if (node.data.type === 'contract_extract' && !node.data.config.sourceVariable) {
          errors.push({
            nodeId: node.id,
            message: `Contract Extract node "${node.data.label}" has no source variable configured`,
            severity: 'error',
          });
        }
        break;

      case 'agent_run':
        if (!node.data.config.prompt) {
          errors.push({
            nodeId: node.id,
            message: `Novomo Agent node "${node.data.label}" has no prompt configured`,
            severity: 'error',
          });
        }
        if (!node.data.config.harness) {
          errors.push({
            nodeId: node.id,
            message: `Novomo Agent node "${node.data.label}" has no harness configured`,
            severity: 'error',
          });
        }
        if (!node.data.config.timeoutSeconds || node.data.config.timeoutSeconds <= 0) {
          errors.push({
            nodeId: node.id,
            message: `Novomo Agent node "${node.data.label}" has no timeout configured`,
            severity: 'error',
          });
        }
        if (node.data.config.sandbox && node.data.config.sandbox !== 'host' && node.data.config.sandbox !== 'docker') {
          errors.push({
            nodeId: node.id,
            message: `Novomo Agent node "${node.data.label}" has an invalid sandbox`,
            severity: 'error',
          });
        }
        validateNovomoHandoff(node, nodes, edges, errors);
        break;

      case 'novo_run':
        if (!node.data.config.prompt && !node.data.config.taskId) {
          errors.push({
            nodeId: node.id,
            message: `Superagent node "${node.data.label}" has no prompt or task configured`,
            severity: 'error',
          });
        }
        if (!node.data.config.timeoutSeconds || node.data.config.timeoutSeconds <= 0) {
          errors.push({
            nodeId: node.id,
            message: `Superagent node "${node.data.label}" has no timeout configured`,
            severity: 'error',
          });
        }
        if (node.data.config.sandbox && node.data.config.sandbox !== 'host' && node.data.config.sandbox !== 'docker') {
          errors.push({
            nodeId: node.id,
            message: `Superagent node "${node.data.label}" has an invalid sandbox`,
            severity: 'error',
          });
        }
        if (node.data.config.graceSeconds != null && node.data.config.graceSeconds < 0) {
          errors.push({
            nodeId: node.id,
            message: `Superagent node "${node.data.label}" has an invalid grace period`,
            severity: 'error',
          });
        }
        if (!isObjectArrayJSON(node.data.config.repoSpecsJson)) {
          errors.push({
            nodeId: node.id,
            message: `Superagent node "${node.data.label}" repo specs must be a JSON array of objects`,
            severity: 'error',
          });
        }
        if (!isObjectJSON(node.data.config.workSourceJson)) {
          errors.push({
            nodeId: node.id,
            message: `Superagent node "${node.data.label}" work source must be a JSON object`,
            severity: 'error',
          });
        }
        validateNovomoHandoff(node, nodes, edges, errors);
        break;

      case 'conditional': {
        if (!node.data.config.condition) {
          errors.push({
            nodeId: node.id,
            message: `Conditional node "${node.data.label}" has no condition configured`,
            severity: 'error',
          });
        }

        // Check if both branches are connected
        const trueEdge = edges.find((e) => e.source === node.id && e.sourceHandle === 'true');
        const falseEdge = edges.find((e) => e.source === node.id && e.sourceHandle === 'false');

        if (!trueEdge) {
          errors.push({
            nodeId: node.id,
            message: `Conditional node "${node.data.label}" missing true branch`,
            severity: 'warning',
          });
        }
        if (!falseEdge) {
          errors.push({
            nodeId: node.id,
            message: `Conditional node "${node.data.label}" missing false branch`,
            severity: 'warning',
          });
        }
        break;
      }

      case 'input': {
        // Input nodes should have outgoing connections
        const hasOutgoing = edges.some((e) => e.source === node.id);
        if (!hasOutgoing) {
          errors.push({
            nodeId: node.id,
            message: `Input node "${node.data.label}" has no outgoing connections`,
            severity: 'warning',
          });
        }
        break;
      }

      case 'child_workflow': {
        if (!node.data.config.childWorkflowId) {
          errors.push({
            nodeId: node.id,
            message: `Child Workflow node "${node.data.label}" has no child workflow selected`,
            severity: 'error',
          });
        }
        const cwHasIncoming = edges.some((e) => e.target === node.id);
        if (!cwHasIncoming) {
          errors.push({
            nodeId: node.id,
            message: `Child Workflow node "${node.data.label}" has no incoming connections`,
            severity: 'warning',
          });
        }
        const cwHasOutgoing = edges.some((e) => e.source === node.id);
        if (!cwHasOutgoing) {
          errors.push({
            nodeId: node.id,
            message: `Child Workflow node "${node.data.label}" has no outgoing connections`,
            severity: 'warning',
          });
        }
        break;
      }

      case 'workflow_ref': {
        if (!node.data.config.workflowRefId && !node.data.config.workflowId) {
          errors.push({
            nodeId: node.id,
            message: `Workflow Reference node "${node.data.label}" has no workflow selected`,
            severity: 'error',
          });
        }
        const refHasIncoming = edges.some((e) => e.target === node.id);
        if (!refHasIncoming) {
          errors.push({
            nodeId: node.id,
            message: `Workflow Reference node "${node.data.label}" has no incoming connections`,
            severity: 'warning',
          });
        }
        const refHasOutgoing = edges.some((e) => e.source === node.id);
        if (!refHasOutgoing) {
          errors.push({
            nodeId: node.id,
            message: `Workflow Reference node "${node.data.label}" has no outgoing connections`,
            severity: 'warning',
          });
        }
        break;
      }

      case 'operation': {
        const operationType = node.data.config.operationType?.trim();
        if (!operationType) {
          errors.push({
            nodeId: node.id,
            message: `Operation node "${node.data.label}" has no operation type configured`,
            severity: 'error',
          });
          break;
        }

        const requiredGroups = OPERATION_CONFIG_REQUIREMENTS[operationType];
        if (!requiredGroups) {
          errors.push({
            nodeId: node.id,
            message: `Operation node "${node.data.label}" has unsupported operation type "${operationType}"`,
            severity: 'error',
          });
          break;
        }

        if (!hasRequiredOperationConfigGroup(node.data.config.operationConfig, requiredGroups)) {
          errors.push({
            nodeId: node.id,
            message: `Operation node "${node.data.label}" ${describeOperationRequirement(requiredGroups)}`,
            severity: 'error',
          });
        }
        break;
      }

      case 'aggregation': {
        const incoming = edges.filter((e) => e.target === node.id);
        const outgoing = edges.filter((e) => e.source === node.id);
        const outgoingResultCount = outgoing.filter((edge) => {
          const target = nodes.find((candidate) => candidate.id === edge.target);
          return target?.data.type === 'result';
        }).length;

        if (incoming.length === 0) {
          errors.push({
            nodeId: node.id,
            message: `Aggregation node "${node.data.label}" has no incoming inputs`,
            severity: 'error',
          });
        }
        if (outgoing.length !== 1 || outgoingResultCount !== 1) {
          errors.push({
            nodeId: node.id,
            message: `Aggregation node "${node.data.label}" must connect to exactly one Result node`,
            severity: 'error',
          });
        }
        if (!node.data.config.aggregationMethod) {
          errors.push({
            nodeId: node.id,
            message: `Aggregation node "${node.data.label}" has no aggregation method configured`,
            severity: 'error',
          });
        }
        break;
      }

      case 'result': {
        // Result nodes should have incoming connections
        const incoming = edges.filter((e) => e.target === node.id);
        const hasIncoming = incoming.length > 0;
        if (!hasIncoming) {
          errors.push({
            nodeId: node.id,
            message: `Result node "${node.data.label}" has no incoming connections`,
            severity: 'warning',
          });
        }
        const hasIncomingAggregation = incoming.some((edge) => {
          const source = nodes.find((candidate) => candidate.id === edge.source);
          return source?.data.type === 'aggregation';
        });
        const hasOwnAggregationConfig =
          Boolean(node.data.config.aggregationMethod) ||
          Boolean(node.data.config.aggregationConfig && Object.keys(node.data.config.aggregationConfig).length > 0);
        if (hasIncomingAggregation && hasOwnAggregationConfig) {
          errors.push({
            nodeId: node.id,
            message: `Result node "${node.data.label}" is downstream of an Aggregation node and cannot define its own aggregation method`,
            severity: 'error',
          });
        }
        break;
      }
    }
  });

  // Check for orphaned nodes (no connections at all)
  nodes.forEach((node) => {
    const hasConnections = edges.some((e) => e.source === node.id || e.target === node.id);
    if (!hasConnections && nodes.length > 1) {
      errors.push({
        nodeId: node.id,
        message: `Node "${node.data.label}" is disconnected from the workflow`,
        severity: 'warning',
      });
    }
  });

  const hasErrors = errors.some((e) => e.severity === 'error');
  return {
    isValid: !hasErrors,
    errors,
  };
}
