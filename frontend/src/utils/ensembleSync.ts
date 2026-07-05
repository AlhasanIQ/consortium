import { workflowClient } from '../api/workflowClient';
import type { AgentState } from '../stores/ensembleStore';
import {
  type AggregationMethod,
  DEFAULT_PROMPT_MAX_TOKENS,
  DEFAULT_PROMPT_TEMPERATURE,
  DEFAULT_PROMPT_TIMEOUT_SECONDS,
  ensureAggregationConfigDefaults,
  type RetryPolicyConfig,
  resolveRetryPolicy,
} from '../types/workflow';
import type { WorkflowFileFormat } from '../types/workflowFile';

// Fixed ID for the default reasoning workflow
export const DEFAULT_WORKFLOW_ID = 'reasoning-informed-captain-synthesis';

// Provider color mapping (duplicated from ensembleStore for isolation)
const providerColors: Record<string, string> = {
  anthropic: '#D97706',
  openai: '#10B981',
  google: '#3B82F6',
  meta: '#8B5CF6',
  mistral: '#EC4899',
  deepseek: '#06B6D4',
  xai: '#EF4444',
  'x-ai': '#EF4444',
  qwen: '#F59E0B',
  default: '#6B7280',
};

const getProviderColor = (provider: string, model?: string): string => {
  const lowerProvider = provider.toLowerCase();

  // For OpenRouter, extract provider from model ID (e.g., "anthropic/claude-..." -> "anthropic")
  if (lowerProvider === 'openrouter' && model) {
    const modelProvider = model.split('/')[0].toLowerCase();
    for (const [key, color] of Object.entries(providerColors)) {
      if (modelProvider.includes(key)) return color;
    }
  }

  for (const [key, color] of Object.entries(providerColors)) {
    if (lowerProvider.includes(key)) return color;
  }
  return providerColors.default;
};

const getDisplayName = (model: string): string => {
  const modelLower = model.toLowerCase();
  if (modelLower.includes('claude')) return 'Claude';
  if (modelLower.includes('gpt-4')) return 'GPT-4';
  if (modelLower.includes('gpt-3')) return 'GPT-3.5';
  if (modelLower.includes('gemini')) return 'Gemini';
  if (modelLower.includes('llama')) return 'Llama';
  if (modelLower.includes('mistral')) return 'Mistral';
  if (modelLower.includes('deepseek')) return 'DeepSeek';
  if (modelLower.includes('grok')) return 'Grok';
  if (modelLower.includes('qwen')) return 'Qwen';
  if (modelLower.includes('o1')) return 'o1';
  if (modelLower.includes('o3')) return 'o3';

  const parts = model.split('/');
  const modelName = parts[parts.length - 1];
  return modelName.charAt(0).toUpperCase() + modelName.slice(1, 11);
};

/**
 * Extract agent information from a workflow definition
 * Finds all agent nodes in the workflow
 */
export function extractAgentsFromWorkflow(workflow: WorkflowFileFormat): AgentState[] {
  const agents: AgentState[] = [];

  // Find all agent nodes
  // Filter by data.type === 'agent' to support various ID naming conventions (agent-*, expert-*, etc.)
  const agentNodes = workflow.nodes.filter((node) => node.data.type === 'agent');

  agentNodes.forEach((node, _index) => {
    const config = node.data.config;
    const model = config.model || '';
    const provider = config.provider || '';

    agents.push({
      id: node.id, // Preserve workflow node ID to match backend node_ids
      model,
      provider,
      displayName: config.name || getDisplayName(model),
      color: getProviderColor(provider, model),
      status: 'idle',
      output: '',
      streamingText: '',
      tokens: 0,
      cost: 0,
      latencyMs: 0,
    });
  });

  return agents;
}

/**
 * Load a workflow by ID and return it for execution
 */
export async function loadWorkflowById(workflowId: string): Promise<WorkflowFileFormat | null> {
  try {
    const workflow = await workflowClient.getWorkflow(workflowId);
    return workflow as WorkflowFileFormat;
  } catch (error) {
    console.error('[EnsembleSync] Failed to load workflow:', workflowId, error);
    return null;
  }
}

/**
 * Convert workflow to execution format with user prompt injected
 * Uses edges to enable parallel execution with individual node events
 * Aggregation may be authored as either legacy result-owned config or the
 * Builder-native aggregation -> result macro. The executable request always
 * sends one runtime result node that owns aggregation config.
 */
export function buildExecutionRequest(
  workflow: WorkflowFileFormat,
  prompt: string,
): {
  id: string;
  name: string;
  context: Record<string, unknown>;
  nodes: Array<{
    id: string;
    type: string;
    model?: string;
    prompt?: string;
    system_prompt?: string;
    temperature?: number;
    max_tokens?: number;
    timeout_seconds?: number;
    retry_policy?: RetryPolicyConfig;
    output_name?: string;
    aggregation_method?: string;
    aggregation_config?: Record<string, unknown>;
    metadata?: Record<string, unknown>;
  }>;
  edges: Array<{ source: string; target: string }>;
} {
  // Extract all agent nodes
  const agentNodes = workflow.nodes.filter((n) => n.data.type === 'agent');
  const aggregationNode = workflow.nodes.find((n) => n.type === 'aggregation' || n.data.type === 'aggregation');
  const presentationResultNode = aggregationNode
    ? workflow.nodes.find(
        (node) =>
          (node.type === 'result' || node.data.type === 'result') &&
          workflow.edges.some((edge) => edge.source === aggregationNode.id && edge.target === node.id),
      )
    : undefined;
  const resultNode = presentationResultNode || workflow.nodes.find((n) => n.data.type === 'result');

  // Build node IDs from agent nodes
  const agentNodeIds = agentNodes.map((n) => n.id);

  // Build agent nodes as top-level nodes (not nested in parallel)
  const agentExecNodes = agentNodes.map((node) => {
    const config = node.data.config;
    // Get system prompt and interpolate variables
    let systemPrompt = config.systemPrompt || '';
    // Replace {{variable}} placeholders with actual values
    systemPrompt = systemPrompt.replace(/\{\{(\w+)\}\}/g, (_match, varName) => {
      if (varName === 'user_prompt' || varName === 'prompt' || varName === 'topic') {
        return prompt;
      }
      return `{{${varName}}}`; // Keep unmatched placeholders
    });

    // Get user prompt - use explicit userPrompt if provided, otherwise use the raw prompt
    const userPrompt = config.userPrompt || prompt;

    return {
      id: node.id,
      type: 'prompt' as const,
      model: config.model,
      prompt: userPrompt,
      system_prompt: systemPrompt, // Pass system prompt separately
      // Backend requires these explicitly (no implicit defaults). Seed values
      // win; defaults only cover builder-authored nodes that omit a field.
      temperature: config.temperature ?? DEFAULT_PROMPT_TEMPERATURE,
      max_tokens: config.maxTokens ?? DEFAULT_PROMPT_MAX_TOKENS,
      timeout_seconds: config.timeoutSeconds ?? DEFAULT_PROMPT_TIMEOUT_SECONDS,
      retry_policy: resolveRetryPolicy(config),
    };
  });

  // Determine the final node from result node (handles all aggregation methods)
  let finalNode: {
    id: string;
    type: string;
    model?: string;
    prompt?: string;
    output_name?: string;
    output_format?: string;
    aggregation_method?: string;
    aggregation_config?: Record<string, unknown>;
    timeout_seconds?: number;
    retry_policy?: RetryPolicyConfig;
    metadata?: Record<string, unknown>;
  };
  let finalNodeId: string;

  if (aggregationNode) {
    const config = aggregationNode.data.config;
    const outputConfig = presentationResultNode?.data.config;
    const method = (config.aggregationMethod || 'collect') as AggregationMethod;
    const normalizedAggregationConfig = ensureAggregationConfigDefaults(method, config.aggregationConfig);
    const aggregationConfig: Record<string, unknown> = {
      ...(normalizedAggregationConfig || {}),
      original_prompt: prompt,
    };
    if (
      config.openRouterProvider &&
      !('openRouterProvider' in aggregationConfig) &&
      !('openrouter_provider' in aggregationConfig)
    ) {
      aggregationConfig.openRouterProvider = config.openRouterProvider;
    }
    if (
      config.openRouterReasoning &&
      !('openRouterReasoning' in aggregationConfig) &&
      !('openrouter_reasoning' in aggregationConfig)
    ) {
      aggregationConfig.openRouterReasoning = config.openRouterReasoning;
    }

    finalNode = {
      id: aggregationNode.id,
      type: 'result',
      output_name: outputConfig?.name || config.name || aggregationNode.id,
      output_format: outputConfig?.outputFormat || config.outputFormat || 'text',
      aggregation_method: method,
      aggregation_config: aggregationConfig,
      timeout_seconds: config.timeoutSeconds,
      retry_policy: resolveRetryPolicy(config),
      metadata: {
        input_ids: agentNodeIds,
        aggregation_anchor_id: aggregationNode.id,
        ...(presentationResultNode ? { presentation_result_id: presentationResultNode.id } : {}),
      },
    };
    finalNodeId = aggregationNode.id;
  } else if (resultNode) {
    // Use legacy result node with configured aggregation method.
    const config = resultNode.data.config;
    const method = (config.aggregationMethod || 'collect') as AggregationMethod;
    const normalizedAggregationConfig = ensureAggregationConfigDefaults(method, config.aggregationConfig);
    const aggregationConfig: Record<string, unknown> = {
      ...(normalizedAggregationConfig || {}),
      original_prompt: prompt, // Pass prompt for synthesis/judge/peer_matrix evaluation
    };
    if (
      config.openRouterProvider &&
      !('openRouterProvider' in aggregationConfig) &&
      !('openrouter_provider' in aggregationConfig)
    ) {
      aggregationConfig.openRouterProvider = config.openRouterProvider;
    }
    if (
      config.openRouterReasoning &&
      !('openRouterReasoning' in aggregationConfig) &&
      !('openrouter_reasoning' in aggregationConfig)
    ) {
      aggregationConfig.openRouterReasoning = config.openRouterReasoning;
    }

    finalNode = {
      id: resultNode.id,
      type: 'result',
      output_name: config.name || resultNode.id,
      output_format: config.outputFormat || 'text',
      aggregation_method: method,
      aggregation_config: aggregationConfig,
      timeout_seconds: config.timeoutSeconds,
      retry_policy: resolveRetryPolicy(config), // required on every node
      metadata: {
        input_ids: agentNodeIds,
      },
    };
    finalNodeId = resultNode.id;
  } else {
    // Fallback: collect all outputs
    finalNode = {
      id: 'result',
      type: 'result',
      output_name: 'result',
      aggregation_method: 'collect',
      retry_policy: resolveRetryPolicy({}), // required on every node
      metadata: {
        input_ids: agentNodeIds,
      },
    };
    finalNodeId = 'result';
  }

  // Build edges: all agents -> final node (agents run in parallel, then aggregate)
  const edges = agentNodeIds.map((agentId) => ({
    source: agentId,
    target: finalNodeId,
  }));

  return {
    id: workflow.id || DEFAULT_WORKFLOW_ID, // Preserve workflow ID so jobs are correctly associated
    name: workflow.name || 'ensemble-query',
    context: {
      user_prompt: prompt,
    },
    nodes: [...agentExecNodes, finalNode],
    edges,
  };
}
