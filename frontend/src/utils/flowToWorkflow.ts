import type { Edge, Node } from '@xyflow/react';
import {
  type AggregationConfig,
  type AggregationMethod,
  DEFAULT_MODEL,
  DEFAULT_NOVOMO_SANDBOX,
  DEFAULT_PROMPT_MAX_TOKENS,
  DEFAULT_PROMPT_TEMPERATURE,
  DEFAULT_PROMPT_TIMEOUT_SECONDS,
  ensureAggregationConfigDefaults,
  type NodeData,
  type NodeType,
  type NovomoHandoffKind,
  type NovomoHandoffRef,
  type OpenRouterProviderConfig,
  type OpenRouterReasoningConfig,
  resolveRetryPolicy,
  type Workflow,
  type WorkflowNode,
} from '../types/workflow';
import { WORKFLOW_FILE_VERSION, type WorkflowFileFormat } from '../types/workflowFile';
import { filterEphemeralAggregationGraph } from './aggregationPlan';
import { layoutWorkflowNodesTopToBottom } from './dagreLayout';
import { topologicalSort } from './dagSort';
import { findNearestNovomoHandoffSources } from './novomoHandoff';

/**
 * Convert React Flow graph to complete workflow file format (for export/import)
 * This preserves ALL data including positions, labels, configurations, etc.
 */
export function flowToWorkflowFile(
  nodes: Node<NodeData>[],
  edges: Edge[],
  name?: string,
  description?: string,
  id?: string,
): WorkflowFileFormat {
  const filtered = filterEphemeralAggregationGraph({ nodes, edges });
  return {
    version: WORKFLOW_FILE_VERSION,
    id: id, // Backend will assign UUID if undefined
    name: name || 'Untitled Workflow',
    description: description || '',
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    nodes: filtered.nodes,
    edges: filtered.edges,
  };
}

/**
 * Convert workflow file format back to React Flow graph
 */
export function workflowFileToFlow(file: WorkflowFileFormat): { nodes: Node<NodeData>[]; edges: Edge[] } {
  return {
    nodes: file.nodes,
    edges: file.edges,
  };
}

interface RuntimeWorkflowFileOptions {
  jobId?: string;
  source?: string;
  savedWorkflowExists?: boolean;
}

/**
 * Convert an executable backend workflow snapshot into the editor's workflow
 * file format. Ad-hoc jobs store runtime workflows in request_data/dag_snapshot;
 * this recreates editable React Flow nodes without requiring a saved workflow.
 */
export function runtimeWorkflowToWorkflowFile(
  workflow: Workflow,
  options: RuntimeWorkflowFileOptions = {},
): WorkflowFileFormat {
  const nodes: Node<NodeData>[] = [];
  const presentationEdges: Edge[] = [];
  let layoutIndex = 0;
  workflow.nodes.forEach((node) => {
    if (isRuntimeAggregationMacroNode(node)) {
      const expanded = runtimeAggregationMacroToFlowNodes(node, layoutIndex);
      nodes.push(expanded.aggregationNode, expanded.resultNode);
      presentationEdges.push(expanded.edge);
      layoutIndex += 2;
      return;
    }
    nodes.push(runtimeNodeToFlowNode(node, layoutIndex));
    layoutIndex += 1;
  });
  const edges = [...runtimeEdgesToFlowEdges(workflow), ...presentationEdges];
  const laidOutNodes = layoutWorkflowNodesTopToBottom(nodes, edges, {
    origin: { x: 160, y: 240 },
    rankSep: 180,
    nodeSep: 96,
  });
  const now = new Date().toISOString();

  return {
    version: WORKFLOW_FILE_VERSION,
    id: workflow.id || undefined,
    name: workflow.name || workflow.id || 'Ad-hoc Workflow',
    description: workflow.description || '',
    created_at: workflow.created_at || now,
    updated_at: workflow.updated_at || now,
    nodes: laidOutNodes,
    edges,
    metadata: {
      source: options.source || 'job_snapshot',
      ...(options.jobId ? { job_id: options.jobId } : {}),
      ...(typeof options.savedWorkflowExists === 'boolean'
        ? { saved_workflow_exists: options.savedWorkflowExists }
        : {}),
    },
  };
}

function isRuntimeAggregationMacroNode(node: WorkflowNode): boolean {
  return (
    (node.type === 'result' || node.type === 'output') &&
    Boolean(node.metadata?.aggregation_anchor_id) &&
    node.metadata?.aggregation_anchor_id === node.id &&
    Boolean(node.metadata?.presentation_result_id)
  );
}

function runtimeAggregationMacroToFlowNodes(
  node: WorkflowNode,
  index: number,
): { aggregationNode: Node<NodeData>; resultNode: Node<NodeData>; edge: Edge } {
  const aggregationID = node.metadata?.aggregation_anchor_id || node.id || `aggregation-${index + 1}`;
  const resultID = node.metadata?.presentation_result_id || `${aggregationID}__result`;
  const x = 160 + index * 320;
  const y = 240;
  const retry = node.retry_policy;
  const aggregationConfig: NodeData['config'] = {
    name: node.metadata?.name || aggregationID,
    description: node.metadata?.description || '',
    aggregationMethod: (node.aggregation_method || 'collect') as AggregationMethod,
    aggregationConfig: node.aggregation_config,
    ...(retry
      ? {
          retryMaxAttempts: retry.max_attempts,
          retryBackoffMs: retry.backoff_ms,
          retryBackoffMultiplier: retry.backoff_multiply,
          retryMaxBackoffMs: retry.max_backoff_ms,
        }
      : {}),
  };

  const outputName = node.metadata?.output_name || node.output_name || resultID;
  const outputFormat = (node.metadata?.output_format ||
    node.output_format ||
    'text') as NodeData['config']['outputFormat'];

  return {
    aggregationNode: {
      id: aggregationID,
      type: 'aggregation',
      position: { x, y },
      data: {
        type: 'aggregation',
        label: node.metadata?.name || aggregationID,
        config: aggregationConfig,
      },
    },
    resultNode: {
      id: resultID,
      type: 'result',
      position: { x: x + 320, y },
      data: {
        type: 'result',
        label: outputName,
        config: {
          name: outputName,
          description: node.metadata?.description || '',
          outputFormat,
        },
      },
    },
    edge: {
      id: `${aggregationID}-${resultID}`,
      source: aggregationID,
      target: resultID,
    },
  };
}

function runtimeNodeToFlowNode(node: WorkflowNode, index: number): Node<NodeData> {
  const editorType = editorTypeForRuntimeNode(node);
  const name = runtimeNodeName(node, editorType);
  const description = node.metadata?.description || '';
  const retry = node.retry_policy;
  const config: NodeData['config'] = {
    name,
    description,
    ...(retry
      ? {
          retryMaxAttempts: retry.max_attempts,
          retryBackoffMs: retry.backoff_ms,
          retryBackoffMultiplier: retry.backoff_multiply,
          retryMaxBackoffMs: retry.max_backoff_ms,
        }
      : {}),
  };

  switch (editorType) {
    case 'agent':
    case 'contract_extract':
      config.model = node.model || DEFAULT_MODEL;
      config.systemPrompt = node.system_prompt || '';
      config.userPrompt = node.prompt || '';
      config.temperature = node.temperature;
      config.maxTokens = node.max_tokens;
      config.timeoutSeconds = node.timeout_seconds;
      if (editorType === 'contract_extract') {
        config.sourceVariable = node.metadata?.source_variable;
        config.extractionPatterns = node.metadata?.extraction_patterns;
      }
      break;
    case 'agent_run':
      config.prompt = node.prompt || '';
      config.harness = node.harness === 'claude-code' || node.harness === 'codex' ? node.harness : undefined;
      config.sandbox = normalizeSandbox(node.sandbox);
      config.timeoutSeconds = node.timeout_seconds;
      applyRuntimeHandoffConfig(config, node);
      break;
    case 'novo_run':
      config.prompt = node.prompt || '';
      config.taskId = node.task_id;
      config.taskSummary = node.task_summary;
      config.identity = node.identity;
      config.image = node.image;
      config.sandbox = normalizeSandbox(node.sandbox);
      config.runtimeUrl = node.runtime_url;
      config.graceSeconds = node.grace_seconds;
      config.timeoutSeconds = node.timeout_seconds;
      config.repoSpecsJson = stringifyJSONConfig(node.repo_specs);
      config.workSourceJson = stringifyJSONConfig(node.work_source);
      applyRuntimeHandoffConfig(config, node);
      break;
    case 'child_workflow':
      config.childWorkflowId = node.child_workflow_id;
      config.childInputTemplate = node.child_input_template;
      config.childOutputKey = node.child_output_key;
      config.childAwait = node.child_await;
      config.timeoutSeconds = node.timeout_seconds;
      break;
    case 'workflow_ref':
      config.workflowId = node.workflow_ref_id;
      config.workflowRefId = node.workflow_ref_id;
      config.inputTemplate = node.input_template;
      config.outputKey = node.output_key;
      break;
    case 'operation':
      config.operationType = node.operation_type;
      config.operationConfig = node.operation_config;
      break;
    case 'conditional':
      config.condition = node.condition;
      break;
    case 'result':
      config.name = node.metadata?.output_name || node.output_name || name;
      config.outputFormat = (node.metadata?.output_format ||
        node.output_format ||
        'text') as NodeData['config']['outputFormat'];
      config.aggregationMethod = (node.aggregation_method || 'collect') as AggregationMethod;
      config.aggregationConfig = node.aggregation_config;
      break;
    case 'input':
      break;
  }

  return {
    id: node.id || `node-${index + 1}`,
    type: editorType,
    position: { x: 160 + index * 320, y: 240 },
    data: {
      type: editorType,
      label: name,
      config,
    },
  };
}

function editorTypeForRuntimeNode(node: WorkflowNode): NodeType {
  switch (node.type) {
    case 'prompt':
      return 'agent';
    case 'output':
    case 'result':
      return 'result';
    case 'agent_run':
    case 'novo_run':
    case 'contract_extract':
    case 'child_workflow':
    case 'workflow_ref':
    case 'operation':
    case 'conditional':
      return node.type;
    default:
      console.warn(`Unknown runtime node type "${node.type}" while opening workflow snapshot; rendering as agent.`);
      return 'agent';
  }
}

function runtimeNodeName(node: WorkflowNode, editorType: NodeType): string {
  if (node.metadata?.name) return node.metadata.name;
  if (editorType === 'result' && (node.metadata?.output_name || node.output_name)) {
    return node.metadata?.output_name || node.output_name || node.id || 'Result';
  }
  if (node.metadata?.label) return node.metadata.label;
  return node.id || editorType;
}

function runtimeEdgesToFlowEdges(workflow: Workflow): Edge[] {
  if (workflow.edges && workflow.edges.length > 0) {
    return workflow.edges.map((edge, index) => ({
      id: edge.id || `${edge.source}-${edge.target}-${index}`,
      source: edge.source,
      target: edge.target,
    }));
  }

  const edges: Edge[] = [];
  for (const node of workflow.nodes) {
    for (const inputID of node.metadata?.input_ids || []) {
      if (!node.id) continue;
      edges.push({
        id: `${inputID}-${node.id}`,
        source: inputID,
        target: node.id,
      });
    }
  }
  return edges;
}

function stringifyJSONConfig(value: unknown): string | undefined {
  if (value == null) return undefined;
  if (Array.isArray(value) && value.length === 0) return undefined;
  if (typeof value === 'object' && !Array.isArray(value) && Object.keys(value).length === 0) return undefined;
  return JSON.stringify(value, null, 2);
}

function normalizeSandbox(sandbox?: string): 'host' | 'docker' {
  return sandbox === 'host' || sandbox === 'docker' ? sandbox : DEFAULT_NOVOMO_SANDBOX;
}

function normalizeHandoffKind(kind?: string): NovomoHandoffKind {
  switch (kind) {
    case 'job_run':
    case 'job':
    case 'novo_run':
    case 'task':
      return kind;
    default:
      return 'job_run';
  }
}

function applyRuntimeHandoffConfig(config: NodeData['config'], node: WorkflowNode) {
  if (node.inherit_from_workflow_task) {
    config.inheritFromMode = 'auto';
    config.inheritFromPolicy = node.inherit_from_policy;
    return;
  }
  if (node.inherit_from_node_id) {
    config.inheritFromMode = 'upstream';
    config.inheritFromNodeId = node.inherit_from_node_id;
    config.inheritFromPolicy = node.inherit_from_policy;
    return;
  }
  if (node.inherit_from) {
    config.inheritFromMode = 'explicit';
    config.inheritFromKind = normalizeHandoffKind(node.inherit_from.kind);
    config.inheritFromId = node.inherit_from.id;
    config.inheritFromPolicy = node.inherit_from.policy;
    return;
  }
  config.inheritFromMode = 'none';
}

function novomoHandoffFields(
  config: NodeData['config'],
  nodeId: string,
  nodes: Node<NodeData>[],
  edges: Edge[],
): Partial<WorkflowNode> {
  const policy = config.inheritFromPolicy?.trim() || undefined;
  switch (config.inheritFromMode || 'auto') {
    case 'auto': {
      const autoSources = findNearestNovomoHandoffSources(nodeId, nodes, edges);
      if (autoSources.length > 1) {
        return {
          inherit_from_workflow_task: true,
          ...(policy ? { inherit_from_policy: policy } : {}),
        };
      }
      const autoSource = autoSources[0];
      if (!autoSource) return {};
      return {
        inherit_from_node_id: autoSource,
        ...(policy ? { inherit_from_policy: policy } : {}),
      };
    }
    case 'explicit': {
      const ref: NovomoHandoffRef = {
        kind: normalizeHandoffKind(config.inheritFromKind),
        id: config.inheritFromId?.trim() || '',
        ...(policy ? { policy } : {}),
      };
      return { inherit_from: ref };
    }
    case 'upstream':
      return {
        inherit_from_node_id: config.inheritFromNodeId?.trim() || '',
        ...(policy ? { inherit_from_policy: policy } : {}),
      };
    default:
      return {};
  }
}

function firstDownstreamResultNode(nodeId: string, nodes: Node<NodeData>[], edges: Edge[]): Node<NodeData> | undefined {
  const outgoing = edges.filter((edge) => edge.source === nodeId);
  for (const edge of outgoing) {
    const target = nodes.find((node) => node.id === edge.target);
    if (target?.data.type === 'result') {
      return target;
    }
  }
  return undefined;
}

function hasIncomingAggregation(nodeId: string, nodes: Node<NodeData>[], edges: Edge[]): boolean {
  return edges.some((edge) => {
    if (edge.target !== nodeId) return false;
    const source = nodes.find((node) => node.id === edge.source);
    return source?.data.type === 'aggregation';
  });
}

function normalizeAggregationConfig(
  method: AggregationMethod,
  config: AggregationConfig | undefined,
  openRouterProvider?: OpenRouterProviderConfig,
  openRouterReasoning?: OpenRouterReasoningConfig,
): Record<string, unknown> {
  const normalized = { ...(ensureAggregationConfigDefaults(method, config) || {}) } as Record<string, unknown>;
  if (method === 'majority_vote' && normalized.tie_breaker && !normalized.tie_breaker_method) {
    normalized.tie_breaker_method = normalized.tie_breaker;
  }
  if (method === 'majority_vote') {
    delete normalized.tie_breaker;
  }
  if (openRouterProvider && !('openRouterProvider' in normalized) && !('openrouter_provider' in normalized)) {
    normalized.openRouterProvider = openRouterProvider;
  }
  if (openRouterReasoning && !('openRouterReasoning' in normalized) && !('openrouter_reasoning' in normalized)) {
    normalized.openRouterReasoning = openRouterReasoning;
  }
  return normalized;
}

/**
 * Extract input schema from input nodes
 */
export function extractInputSchema(nodes: Node<NodeData>[]): Record<string, unknown> {
  const inputNodes = nodes.filter((n) => n.data.type === 'input');
  if (inputNodes.length === 0) return {};

  // Use the first input node's schema
  const inputNode = inputNodes[0];
  return inputNode.data.config.schema || {};
}

function parseObjectArrayJSON(value?: string): Array<Record<string, unknown>> | undefined {
  const text = (value || '').trim();
  if (!text) return undefined;
  try {
    const parsed = JSON.parse(text);
    if (
      !Array.isArray(parsed) ||
      parsed.some((item) => item == null || typeof item !== 'object' || Array.isArray(item))
    ) {
      return undefined;
    }
    return parsed as Array<Record<string, unknown>>;
  } catch {
    return undefined;
  }
}

function parseObjectJSON(value?: string): Record<string, unknown> | undefined {
  const text = (value || '').trim();
  if (!text) return undefined;
  try {
    const parsed = JSON.parse(text);
    if (parsed == null || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return undefined;
    }
    return parsed as Record<string, unknown>;
  } catch {
    return undefined;
  }
}

/**
 * Convert React Flow graph to backend Workflow format (for execution)
 * Uses topological sort to handle DAG structures with multiple incoming edges.
 *
 * The live Builder submit path sends workflow_file to the backend converter
 * (see useWorkflowExecution); this direct runtime converter remains as a
 * reference/test path for runtime payload parity.
 */
export function flowToWorkflow(
  nodes: Node<NodeData>[],
  edges: Edge[],
  name?: string,
  description?: string,
  inputValues?: Record<string, unknown>,
): Workflow {
  const filtered = filterEphemeralAggregationGraph({ nodes, edges });
  nodes = filtered.nodes;
  edges = filtered.edges;

  const nodeMap = new Map(nodes.map((n) => [n.id, n]));
  const forkedBranchByParent = forkedAggregationBranches(nodes, edges);

  // Collect input data from input nodes and user-provided values
  const workflowContext: Record<string, unknown> = {};

  // Get all input nodes to build context
  const inputNodes = nodes.filter((n) => n.data.type === 'input');
  for (const _inputNode of inputNodes) {
    // If inputValues were provided, use them (these come from the InputDialog)
    if (inputValues && Object.keys(inputValues).length > 0) {
      // Merge all input values into context
      Object.assign(workflowContext, inputValues);
    }
  }

  // Topological sort to get execution order
  const sortedNodeIds = topologicalSort(
    nodes.map((n) => n.id),
    edges,
  );

  // Convert sorted graph nodes to executable workflow nodes (skip input nodes)
  const workflowNodes: WorkflowNode[] = [];
  const includedNodeIds = new Set<string>();

  for (const nodeId of sortedNodeIds) {
    const node = nodeMap.get(nodeId);
    if (!node) continue;
    if (isForkedAggregationBranchNode(node)) continue;

    const config = node.data.config;
    const incomingEdges = edges.filter((e) => e.target === nodeId);
    const inputIds = incomingEdges.map((e) => e.source);

    switch (node.data.type) {
      case 'input':
        // Skip input nodes - they just provide context
        break;

      case 'aggregation': {
        const outputNode = firstDownstreamResultNode(node.id, nodes, edges);
        const outputConfig = outputNode?.data.config;
        const nodeTypeName = 'Aggregation';
        const nodeName = config.name || node.data.label || 'unnamed';
        const formattedLabel = `${node.id} ${nodeTypeName}(${nodeName})`;
        const method = (config.aggregationMethod || 'collect') as AggregationMethod;
        const aggregationConfig = normalizeAggregationConfig(
          method,
          config.aggregationConfig,
          config.openRouterProvider,
          config.openRouterReasoning,
        );
        const aggregationWorkflowId = config.aggregationWorkflowId?.trim();
        const outputName = outputConfig?.name || config.name || node.id;
        const outputFormat = outputConfig?.outputFormat || config.outputFormat || 'text';

        const metadata = {
          input_ids: inputIds,
          label: formattedLabel,
          output_name: outputName,
          output_format: outputFormat,
          description: config.description || outputConfig?.description,
          aggregation_anchor_id: node.id,
          aggregation_method: method,
          ...(config.benchmarkOutputPackaging ? { benchmark_output_packaging: true } : {}),
          ...(outputNode ? { presentation_result_id: outputNode.id } : {}),
        };

        if (aggregationWorkflowId) {
          workflowNodes.push({
            id: node.id,
            type: 'workflow_ref',
            workflow_ref_id: aggregationWorkflowId,
            aggregation_method: method,
            aggregation_config: Object.keys(aggregationConfig).length > 0 ? aggregationConfig : undefined,
            metadata,
          });
        } else {
          workflowNodes.push({
            id: node.id,
            type: 'result',
            output_name: outputName,
            output_format: outputFormat,
            aggregation_method: method,
            aggregation_config: Object.keys(aggregationConfig).length > 0 ? aggregationConfig : undefined,
            retry_policy: resolveRetryPolicy(config),
            metadata,
          });
        }
        includedNodeIds.add(node.id);
        break;
      }

      case 'agent':
      case 'contract_extract': {
        const systemPrompt = config.systemPrompt || '';
        const rawUserPrompt = config.userPrompt ?? '';
        const basePrompt = rawUserPrompt !== '' ? rawUserPrompt : systemPrompt;

        // Build user prompt - inject input variables if none are referenced yet
        let userPrompt = basePrompt;
        const hasVariables = /\{\{\s*[^}]+\s*\}\}/.test(basePrompt);
        if (!hasVariables && Object.keys(workflowContext).length > 0) {
          const inputVars = Object.keys(workflowContext)
            .map((key) => `{{${key}}}`)
            .join('\n');
          const inputBlock = `Input data:\n${inputVars}`;
          userPrompt = basePrompt ? `${inputBlock}\n\n${basePrompt}` : inputBlock;
        }

        // Format label as "node-id NodeType(NodeName)" e.g., "node-abc123 Agent(summarizer)"
        const nodeTypeName = node.data.type.charAt(0).toUpperCase() + node.data.type.slice(1);
        const nodeName = config.name || node.data.label || 'unnamed';
        const formattedLabel = `${node.id} ${nodeTypeName}(${nodeName})`;

        const isContractExtract = node.data.type === 'contract_extract';
        workflowNodes.push({
          id: node.id,
          type: isContractExtract ? 'contract_extract' : 'prompt',
          prompt: userPrompt,
          system_prompt: systemPrompt, // Keep system prompt separate
          model: config.model || DEFAULT_MODEL,
          // Backend requires these explicitly (no implicit defaults). The agent
          // config panel has no timeout input, so it falls back to the default.
          temperature: config.temperature ?? DEFAULT_PROMPT_TEMPERATURE,
          max_tokens: config.maxTokens ?? DEFAULT_PROMPT_MAX_TOKENS,
          timeout_seconds: config.timeoutSeconds ?? DEFAULT_PROMPT_TIMEOUT_SECONDS,
          retry_policy: resolveRetryPolicy(config),
          metadata: {
            label: formattedLabel,
            name: config.name,
            description: config.description,
            ...(isContractExtract && config.sourceVariable ? { source_variable: config.sourceVariable } : {}),
            ...(isContractExtract && config.extractionPatterns && config.extractionPatterns.length > 0
              ? { extraction_patterns: config.extractionPatterns }
              : {}),
            ...(config.openRouterProvider ? { openrouter_provider: config.openRouterProvider } : {}),
            ...(config.openRouterReasoning ? { openrouter_reasoning: config.openRouterReasoning } : {}),
          },
        });
        includedNodeIds.add(node.id);
        break;
      }

      case 'agent_run': {
        const basePrompt = config.prompt || '';
        let prompt = basePrompt;
        const hasVariables = /\{\{\s*[^}]+\s*\}\}/.test(basePrompt);
        if (!hasVariables && Object.keys(workflowContext).length > 0) {
          const inputVars = Object.keys(workflowContext)
            .map((key) => `{{${key}}}`)
            .join('\n');
          const inputBlock = `Input data:\n${inputVars}`;
          prompt = basePrompt ? `${inputBlock}\n\n${basePrompt}` : inputBlock;
        }

        const nodeName = config.name || node.data.label || 'unnamed';
        const formattedLabel = `${node.id} AgentRun(${nodeName})`;
        workflowNodes.push({
          id: node.id,
          type: 'agent_run',
          prompt,
          harness: config.harness || 'claude-code',
          sandbox: normalizeSandbox(config.sandbox),
          timeout_seconds: config.timeoutSeconds || 600,
          ...novomoHandoffFields(config, node.id, nodes, edges),
          retry_policy: resolveRetryPolicy(config),
          metadata: {
            label: formattedLabel,
            name: config.name,
            description: config.description,
          },
        });
        includedNodeIds.add(node.id);
        break;
      }

      case 'novo_run': {
        const basePrompt = config.prompt || '';
        let prompt = basePrompt;
        const hasVariables = /\{\{\s*[^}]+\s*\}\}/.test(basePrompt);
        const isTaskOnlyWake = Boolean(config.taskId && !basePrompt.trim());
        if (!isTaskOnlyWake && !hasVariables && Object.keys(workflowContext).length > 0) {
          const inputVars = Object.keys(workflowContext)
            .map((key) => `{{${key}}}`)
            .join('\n');
          const inputBlock = `Input data:\n${inputVars}`;
          prompt = basePrompt ? `${inputBlock}\n\n${basePrompt}` : inputBlock;
        }

        const nodeName = config.name || node.data.label || 'unnamed';
        const formattedLabel = `${node.id} Superagent(${nodeName})`;
        workflowNodes.push({
          id: node.id,
          type: 'novo_run',
          prompt,
          task_id: config.taskId,
          task_summary: config.taskSummary,
          identity: config.identity || 'sde-novo',
          image: config.image,
          sandbox: normalizeSandbox(config.sandbox),
          runtime_url: config.runtimeUrl,
          timeout_seconds: config.timeoutSeconds || 600,
          grace_seconds: config.graceSeconds ?? 10,
          repo_specs: parseObjectArrayJSON(config.repoSpecsJson),
          work_source: parseObjectJSON(config.workSourceJson),
          ...novomoHandoffFields(config, node.id, nodes, edges),
          retry_policy: resolveRetryPolicy(config),
          metadata: {
            label: formattedLabel,
            name: config.name,
            description: config.description,
          },
        });
        includedNodeIds.add(node.id);
        break;
      }

      case 'result': {
        if (hasIncomingAggregation(node.id, nodes, edges)) {
          break;
        }
        // Frontend 'result' maps to backend 'result'
        const nodeTypeName = 'Result';
        const nodeName = config.name || node.data.label || 'unnamed';
        const formattedLabel = `${node.id} ${nodeTypeName}(${nodeName})`;
        const method = (config.aggregationMethod || 'collect') as AggregationMethod;
        const aggregationConfig = normalizeAggregationConfig(
          method,
          config.aggregationConfig,
          config.openRouterProvider,
          config.openRouterReasoning,
        );

        workflowNodes.push({
          id: node.id,
          type: 'result',
          output_name: config.name || node.id,
          output_format: config.outputFormat || 'text',
          aggregation_method: method,
          aggregation_config: Object.keys(aggregationConfig).length > 0 ? aggregationConfig : undefined,
          retry_policy: resolveRetryPolicy(config), // required on every node
          metadata: {
            input_ids: inputIds,
            label: formattedLabel,
            output_name: config.name || node.id,
            output_format: config.outputFormat || 'text',
            description: config.description,
          },
        });
        includedNodeIds.add(node.id);
        break;
      }

      case 'child_workflow': {
        // child_workflow nodes are converted by the backend from the file format.
        // Include them so edges referencing them are preserved.
        const cwNodeTypeName = 'ChildWorkflow';
        const cwNodeName = config.name || node.data.label || 'unnamed';
        const cwFormattedLabel = `${node.id} ${cwNodeTypeName}(${cwNodeName})`;

        workflowNodes.push({
          id: node.id,
          type: 'child_workflow',
          metadata: {
            label: cwFormattedLabel,
            name: config.name,
            description: config.description,
            input_ids: inputIds,
          },
        });
        includedNodeIds.add(node.id);
        break;
      }

      case 'workflow_ref': {
        const workflowID = config.workflowId || config.workflowRefId;
        const refNodeTypeName = 'WorkflowRef';
        const refNodeName = config.name || node.data.label || workflowID || 'unnamed';
        const refFormattedLabel = `${node.id} ${refNodeTypeName}(${refNodeName})`;

        workflowNodes.push({
          id: node.id,
          type: 'workflow_ref',
          workflow_ref_id: workflowID,
          input_template: config.inputTemplate,
          output_key: config.outputKey,
          metadata: {
            label: refFormattedLabel,
            name: config.name,
            description: config.description,
            input_ids: inputIds,
          },
        });
        includedNodeIds.add(node.id);
        break;
      }

      case 'operation': {
        const opNodeTypeName = 'Operation';
        const opNodeName = config.name || node.data.label || config.operationType || 'unnamed';
        const opFormattedLabel = `${node.id} ${opNodeTypeName}(${opNodeName})`;

        // Edges preserve scheduling/display. Operation runners consume concrete
        // input bindings from operationConfig until compiler expansion fills them.
        workflowNodes.push({
          id: node.id,
          type: 'operation',
          operation_type: config.operationType,
          operation_config: config.operationConfig,
          metadata: {
            label: opFormattedLabel,
            name: config.name,
            description: config.description,
            input_ids: inputIds,
          },
        });
        includedNodeIds.add(node.id);
        break;
      }

      case 'conditional': {
        if (!isForkedAggregationNode(node)) {
          // Hand-authored conditional nodes are not yet supported in backend
          // execution. Keep current behavior: branch targets are handled as
          // ordinary sibling nodes.
          break;
        }
        const conditionalName = config.name || node.data.label || 'unnamed';
        const conditionalLabel = `${node.id} Conditional(${conditionalName})`;
        const branches = forkedBranchByParent.get(node.id);
        workflowNodes.push({
          id: node.id,
          type: 'conditional',
          condition: config.condition,
          true_branch: branchNodeToWorkflow(branches?.true, forkedBranchByParent),
          false_branch: branchNodeToWorkflow(branches?.false, forkedBranchByParent),
          retry_policy: resolveRetryPolicy(config),
          metadata: {
            label: conditionalLabel,
            name: config.name,
            description: config.description,
            input_ids: inputIds.filter((id) => id !== branches?.true?.id && id !== branches?.false?.id),
          },
        });
        includedNodeIds.add(node.id);
        break;
      }
    }
  }

  // Convert React Flow edges to backend Edge format
  // Filter out edges that reference nodes that were skipped (like input nodes)
  // Also filter out edges that are part of conditional branches (handled inline)

  const workflowEdges = edges
    .filter((edge) => {
      // Include edge only if both endpoints are included and it's not a conditional branch edge
      if (!includedNodeIds.has(edge.source) || !includedNodeIds.has(edge.target)) {
        return false;
      }
      if (isForkedAggregationBranchEdge(edge, nodeMap)) {
        return false;
      }
      // Exclude edges from hand-authored conditional nodes (they're handled as
      // existing sibling nodes until authored conditionals are supported).
      const sourceNode = nodeMap.get(edge.source);
      if (sourceNode?.data.type === 'conditional' && !isForkedAggregationNode(sourceNode)) {
        return false;
      }
      return true;
    })
    .map((edge) => ({
      id: edge.id,
      source: edge.source,
      target: edge.target,
    }));

  return {
    id: '', // Backend will assign UUID
    name: name || 'Untitled Workflow',
    description: description || '',
    nodes: workflowNodes,
    edges: workflowEdges,
    context: workflowContext,
  };
}

type ForkedBranchPair = {
  true?: Node<NodeData>;
  false?: Node<NodeData>;
};

function isForkedAggregationNode(node: Node<NodeData>): boolean {
  return node.data.config.aggregationInternalState === 'forked' && Boolean(node.data.config.aggregationAnchorId);
}

function isForkedAggregationBranchNode(node: Node<NodeData>): boolean {
  return isForkedAggregationNode(node) && Boolean(node.data.config.aggregationBranchParentId);
}

function forkedAggregationBranches(nodes: Node<NodeData>[], edges: Edge[]): Map<string, ForkedBranchPair> {
  const nodeByID = new Map(nodes.map((node) => [node.id, node]));
  const out = new Map<string, ForkedBranchPair>();
  for (const edge of edges) {
    const source = nodeByID.get(edge.source);
    const target = nodeByID.get(edge.target);
    if (!source || source.data.type !== 'conditional' || !target || !isForkedAggregationBranchNode(target)) {
      continue;
    }
    const branch = edge.sourceHandle === 'false' || target.data.config.aggregationBranch === 'false' ? 'false' : 'true';
    const existing = out.get(source.id) ?? {};
    existing[branch] = target;
    out.set(source.id, existing);
  }
  return out;
}

function isForkedAggregationBranchEdge(edge: Edge, nodeMap: Map<string, Node<NodeData>>): boolean {
  const source = nodeMap.get(edge.source);
  const target = nodeMap.get(edge.target);
  if (!source || source.data.type !== 'conditional' || !isForkedAggregationNode(source)) {
    return false;
  }
  return (
    edge.sourceHandle === 'true' ||
    edge.sourceHandle === 'false' ||
    Boolean(target && isForkedAggregationBranchNode(target))
  );
}

function branchNodeToWorkflow(
  node: Node<NodeData> | undefined,
  branchesByParent: Map<string, ForkedBranchPair>,
): WorkflowNode | undefined {
  if (!node) {
    return undefined;
  }
  const config = node.data.config;
  const label = `${node.id} ${node.data.type}(${config.name || node.data.label || 'unnamed'})`;
  switch (node.data.type) {
    case 'agent':
      return {
        id: node.id,
        type: 'prompt',
        prompt: config.userPrompt ?? config.systemPrompt ?? '',
        system_prompt: config.systemPrompt || '',
        model: config.model || DEFAULT_MODEL,
        temperature: config.temperature ?? DEFAULT_PROMPT_TEMPERATURE,
        max_tokens: config.maxTokens ?? DEFAULT_PROMPT_MAX_TOKENS,
        timeout_seconds: config.timeoutSeconds ?? DEFAULT_PROMPT_TIMEOUT_SECONDS,
        retry_policy: resolveRetryPolicy(config),
        metadata: {
          label,
          name: config.name,
          description: config.description,
        },
      };
    case 'operation':
      return {
        id: node.id,
        type: 'operation',
        operation_type: config.operationType,
        operation_config: config.operationConfig,
        metadata: {
          label,
          name: config.name,
          description: config.description,
        },
      };
    case 'conditional': {
      const branches = branchesByParent.get(node.id);
      return {
        id: node.id,
        type: 'conditional',
        condition: config.condition,
        true_branch: branchNodeToWorkflow(branches?.true, branchesByParent),
        false_branch: branchNodeToWorkflow(branches?.false, branchesByParent),
        retry_policy: resolveRetryPolicy(config),
        metadata: {
          label,
          name: config.name,
          description: config.description,
        },
      };
    }
    default:
      return undefined;
  }
}
