import type { Edge, Node } from '@xyflow/react';
import type {
  CompilePreviewAggregationGroup,
  CompilePreviewConditionalJob,
  CompilePreviewResponse,
  CompilePreviewWorkflowNode,
  NodeData,
  NodeType,
  WorkflowEdge,
} from '../types/workflow';
import { isExpandedAggregationFrameNode, isMovableExpandedAggregationPreviewNode } from './aggregationPlan';
import { layoutWorkflowNodesTopToBottom } from './dagreLayout';

interface BuildCompiledAggregationPreviewGroupOptions {
  anchorNodeId: string;
  parentNodes: Node<NodeData>[];
  // Candidate/root edges are backend-emitted in the compile preview; retained
  // for call-site symmetry with the store's expansion APIs.
  parentEdges?: Edge[];
  preview: CompilePreviewResponse;
}

interface CompiledAggregationPreviewGroup {
  nodes: Node<NodeData>[];
  edges: Edge[];
}

type PreviewPosition = { x: number; y: number };
type PreviewFrameBounds = PreviewPosition & { width: number; height: number };
type PreviewFlowNode = {
  position: PreviewPosition;
  data: NodeData;
  style?: { width?: unknown; height?: unknown };
  measured?: { width?: number; height?: number };
};
type PreviewInternalState = 'expanded' | 'forked';
type MapPreviewNodeOptions = {
  internalState: PreviewInternalState;
  sourceLocked: boolean;
  branch?: {
    parentID: string;
    branch: 'true' | 'false';
  };
};

// Visual estimates for the current preview node components. The frame padding
// intentionally leaves slack for model labels and the read-only preview badge.
const NODE_WIDTH = 280;
const NODE_HEIGHT = 128;
const ROW_GAP = 128;
const NODE_GAP = 72;
const FRAME_PADDING = 56;
const PREVIEW_ORIGIN_X_OFFSET = -420;
const PREVIEW_ORIGIN_Y_OFFSET = 320;
const RESULT_OUTPUT_FORMATS = new Set(['json', 'text', 'structured']);

export function buildCompiledAggregationPreviewGroup({
  anchorNodeId,
  parentNodes,
  preview,
}: BuildCompiledAggregationPreviewGroupOptions): CompiledAggregationPreviewGroup {
  const group = preview.aggregation_groups.find((item) => item.anchor_node_id === anchorNodeId);
  const anchor = parentNodes.find((node) => node.id === anchorNodeId);
  if (!group || !anchor || group.node_ids.length === 0) {
    return { nodes: [], edges: [] };
  }

  const parentIDs = new Set(parentNodes.map((node) => node.id));
  const groupIDs = new Set(group.node_ids);
  const branchNodes = group.conditional_llm_jobs.map((job) => conditionalJobToWorkflowNode(job, group));
  const displayWorkflowNodes = [
    ...preview.nodes.filter((node): node is CompilePreviewWorkflowNode => Boolean(node.id && groupIDs.has(node.id))),
    ...branchNodes,
  ];
  const displayIDs = new Set(displayWorkflowNodes.map((node) => node.id).filter(Boolean) as string[]);
  if (displayIDs.size === 0) {
    return { nodes: [], edges: [] };
  }

  const previewEdges = preview.edges.filter((edge) =>
    includesPreviewEndpoint(edge, displayIDs, parentIDs, anchorNodeId),
  );
  for (const job of group.conditional_llm_jobs) {
    if (displayIDs.has(job.parent_node_id) && displayIDs.has(job.id)) {
      previewEdges.push({ id: `${job.parent_node_id}-${job.id}`, source: job.parent_node_id, target: job.id });
    }
  }
  if (
    group.presentation_result_id &&
    parentIDs.has(group.presentation_result_id) &&
    group.terminal_node_id &&
    !previewEdges.some((edge) => edge.source === group.terminal_node_id && edge.target === group.presentation_result_id)
  ) {
    previewEdges.push({
      id: `${group.terminal_node_id}-${group.presentation_result_id}`,
      source: group.terminal_node_id,
      target: group.presentation_result_id,
    });
  }

  const unmappedPosition = { x: 0, y: 0 };
  const mappedNodes = displayWorkflowNodes.map((node) => {
    if (!node.id) {
      throw new Error('Compiled preview node is missing an id');
    }
    return mapPreviewNode(node, group, unmappedPosition, { internalState: 'expanded', sourceLocked: true });
  });
  const laidOutNodes = layoutWorkflowNodesTopToBottom(mappedNodes, previewEdges, {
    origin: {
      x: anchor.position.x + PREVIEW_ORIGIN_X_OFFSET,
      y: anchor.position.y + PREVIEW_ORIGIN_Y_OFFSET,
    },
    rankSep: ROW_GAP,
    nodeSep: NODE_GAP,
  });
  const frame = buildFrameNode(group, laidOutNodes);

  return {
    nodes: [frame, ...laidOutNodes],
    edges: previewEdges.map((edge) => ({
      ...edge,
      id: edge.id.startsWith('preview-') ? edge.id : `preview-${edge.id}`,
      data: { aggregationInternalState: 'expanded', aggregationAnchorId: anchorNodeId },
    })),
  };
}

export function buildForkedCompiledAggregationGroup({
  anchorNodeId,
  parentNodes,
  preview,
}: BuildCompiledAggregationPreviewGroupOptions): CompiledAggregationPreviewGroup {
  const group = preview.aggregation_groups.find((item) => item.anchor_node_id === anchorNodeId);
  const anchor = parentNodes.find((node) => node.id === anchorNodeId);
  if (!group || !anchor || group.node_ids.length === 0) {
    return { nodes: [], edges: [] };
  }

  const parentIDs = new Set(parentNodes.map((node) => node.id));
  const groupIDs = new Set(group.node_ids);
  const branchNodes = group.conditional_llm_jobs.map((job) => conditionalJobToWorkflowNode(job, group));
  const branchJobByID = new Map(group.conditional_llm_jobs.map((job) => [job.id, job]));
  const displayWorkflowNodes = [
    ...preview.nodes.filter((node): node is CompilePreviewWorkflowNode => Boolean(node.id && groupIDs.has(node.id))),
    ...branchNodes,
  ];
  const displayIDs = new Set(displayWorkflowNodes.map((node) => node.id).filter(Boolean) as string[]);
  if (displayIDs.size === 0) {
    return { nodes: [], edges: [] };
  }

  const previewEdges = preview.edges.filter((edge) =>
    includesPreviewEndpoint(edge, displayIDs, parentIDs, anchorNodeId),
  );
  for (const job of group.conditional_llm_jobs) {
    if (displayIDs.has(job.parent_node_id) && displayIDs.has(job.id)) {
      previewEdges.push({
        id: `${job.parent_node_id}-${job.branch}-${job.id}`,
        source: job.parent_node_id,
        target: job.id,
        sourceHandle: job.branch,
      });
    }
  }
  if (
    group.presentation_result_id &&
    parentIDs.has(group.presentation_result_id) &&
    group.terminal_node_id &&
    !previewEdges.some((edge) => edge.source === group.terminal_node_id && edge.target === group.presentation_result_id)
  ) {
    previewEdges.push({
      id: `${group.terminal_node_id}-${group.presentation_result_id}`,
      source: group.terminal_node_id,
      target: group.presentation_result_id,
    });
  }

  const mappedNodes = displayWorkflowNodes.map((node) => {
    if (!node.id) {
      throw new Error('Compiled preview node is missing an id');
    }
    const branchJob = branchJobByID.get(node.id);
    return mapPreviewNode(
      node,
      group,
      { x: 0, y: 0 },
      {
        internalState: 'forked',
        sourceLocked: false,
        branch: branchJob
          ? {
              parentID: branchJob.parent_node_id,
              branch: branchJob.branch === 'false' ? 'false' : 'true',
            }
          : undefined,
      },
    );
  });
  const laidOutNodes = layoutWorkflowNodesTopToBottom(mappedNodes, previewEdges, {
    origin: {
      x: anchor.position.x + 300,
      y: anchor.position.y + 260,
    },
    rankSep: ROW_GAP,
    nodeSep: NODE_GAP,
  });

  return {
    nodes: laidOutNodes,
    edges: previewEdges.map((edge) => ({
      ...edge,
      data: {
        ...(edge as Edge & { data?: Record<string, unknown> }).data,
        aggregationInternalState: 'forked',
        aggregationAnchorId: anchorNodeId,
      },
    })),
  };
}

function conditionalJobToWorkflowNode(
  job: CompilePreviewConditionalJob,
  group: CompilePreviewAggregationGroup,
): CompilePreviewWorkflowNode {
  const method = group.method || undefined;
  return {
    id: job.id,
    type: 'prompt',
    model: job.model,
    system_prompt: job.system_prompt,
    prompt: job.prompt,
    temperature: job.temperature,
    max_tokens: job.max_tokens,
    timeout_seconds: job.timeout_seconds,
    retry_policy: job.retry_policy,
    metadata: {
      label: job.label || `${job.parent_node_id} ${job.branch} branch`,
      aggregation_anchor_id: group.anchor_node_id,
      aggregation_method: method,
      aggregation_group_node_id: group.terminal_node_id,
      source_workflow_id: group.source_workflow_id,
      source_parent_node_id: job.parent_node_id,
    },
  };
}

function includesPreviewEndpoint(
  edge: WorkflowEdge,
  displayIDs: Set<string>,
  parentIDs: Set<string>,
  anchorNodeId: string,
): boolean {
  if (edge.source === anchorNodeId || edge.target === anchorNodeId) {
    return false;
  }
  const sourceVisible = displayIDs.has(edge.source) || parentIDs.has(edge.source);
  const targetVisible = displayIDs.has(edge.target) || parentIDs.has(edge.target);
  return sourceVisible && targetVisible && (displayIDs.has(edge.source) || displayIDs.has(edge.target));
}

function mapPreviewNode(
  node: CompilePreviewWorkflowNode,
  group: CompilePreviewAggregationGroup,
  position: PreviewPosition,
  options: MapPreviewNodeOptions,
): Node<NodeData> {
  if (!node.id) {
    throw new Error('Compiled preview node is missing an id');
  }

  const metadata = node.metadata || {};
  const type = flowNodeTypeForPreview(node);
  const operationConfig =
    node.type === 'operation' && node.operation_type
      ? { operationType: node.operation_type, operationConfig: node.operation_config }
      : {};
  const promptConfig =
    type === 'agent'
      ? {
          model: node.model,
          systemPrompt: node.system_prompt,
          userPrompt: node.prompt,
          temperature: node.temperature,
          maxTokens: node.max_tokens,
          timeoutSeconds: node.timeout_seconds,
          retryPolicy: node.retry_policy,
        }
      : {};
  const resultConfig =
    type === 'result'
      ? {
          name: metadata.output_name || node.output_name,
          outputFormat: normalizeResultOutputFormat(node.output_format),
          aggregationMethod: node.aggregation_method,
          aggregationConfig: node.aggregation_config,
        }
      : {};
  return {
    id: node.id,
    type,
    position,
    style: { width: NODE_WIDTH },
    draggable: true,
    selectable: true,
    data: {
      type,
      label: metadata.label || metadata.name || node.id,
      config: {
        ...promptConfig,
        ...operationConfig,
        ...resultConfig,
        condition: node.condition,
        aggregationInternalState: options.internalState,
        aggregationAnchorId: group.anchor_node_id,
        sourceLocked: options.sourceLocked,
        sourceWorkflowId: group.source_workflow_id,
        sourceWorkflowHash: metadata.source_workflow_hash,
        sourceNodeId: metadata.source_node_id || node.id,
        ...(options.internalState === 'forked'
          ? {
              forkedFromWorkflowId: group.source_workflow_id,
              forkedFromSourceHash: metadata.source_workflow_hash,
              boundaryType: 'compiled_workflow_ref' as const,
            }
          : {}),
        ...(options.branch
          ? {
              aggregationBranch: options.branch.branch,
              aggregationBranchParentId: options.branch.parentID,
            }
          : {}),
      },
    },
  };
}

function normalizeResultOutputFormat(value: string | undefined): NodeData['config']['outputFormat'] {
  return value && RESULT_OUTPUT_FORMATS.has(value) ? (value as NodeData['config']['outputFormat']) : undefined;
}

export function reframeCompiledAggregationPreviewGroups<T extends PreviewFlowNode>(nodes: T[]): T[] {
  const internalsByAnchor = new Map<string, T[]>();
  for (const node of nodes) {
    const anchorID = node.data.config.aggregationAnchorId;
    if (!anchorID || !isMovableExpandedAggregationPreviewNode(node)) {
      continue;
    }
    const bucket = internalsByAnchor.get(anchorID);
    if (bucket) {
      bucket.push(node);
    } else {
      internalsByAnchor.set(anchorID, [node]);
    }
  }

  let changed = false;
  const reframed = nodes.map((node) => {
    const anchorID = node.data.config.aggregationAnchorId;
    if (!anchorID || !isExpandedAggregationFrameNode(node)) {
      return node;
    }

    const internals = internalsByAnchor.get(anchorID);
    if (!internals || internals.length === 0) {
      return node;
    }

    const bounds = calculateCompiledAggregationFrameBounds(internals);
    const nextStyle = {
      ...node.style,
      width: bounds.width,
      height: bounds.height,
    };
    if (
      node.position.x === bounds.x &&
      node.position.y === bounds.y &&
      node.style?.width === nextStyle.width &&
      node.style?.height === nextStyle.height
    ) {
      return node;
    }
    changed = true;
    return {
      ...node,
      position: { x: bounds.x, y: bounds.y },
      style: nextStyle,
    } as T;
  });

  return changed ? reframed : nodes;
}

function calculateCompiledAggregationFrameBounds(nodes: PreviewFlowNode[]): PreviewFrameBounds {
  const minX = Math.min(...nodes.map((node) => node.position.x));
  const minY = Math.min(...nodes.map((node) => node.position.y));
  const maxX = Math.max(...nodes.map((node) => node.position.x + measuredNodeWidth(node)));
  const maxY = Math.max(...nodes.map((node) => node.position.y + measuredNodeHeight(node)));

  return {
    x: minX - FRAME_PADDING,
    y: minY - FRAME_PADDING,
    width: maxX - minX + FRAME_PADDING * 2,
    height: maxY - minY + FRAME_PADDING * 2,
  };
}

function measuredNodeWidth(node: PreviewFlowNode): number {
  return numericDimension(node.measured?.width) ?? numericDimension(node.style?.width) ?? NODE_WIDTH;
}

function measuredNodeHeight(node: PreviewFlowNode): number {
  return numericDimension(node.measured?.height) ?? numericDimension(node.style?.height) ?? NODE_HEIGHT;
}

function numericDimension(value: unknown): number | undefined {
  if (typeof value === 'number' && Number.isFinite(value) && value > 0) {
    return value;
  }
  if (typeof value === 'string') {
    const parsed = Number.parseFloat(value);
    if (Number.isFinite(parsed) && parsed > 0) {
      return parsed;
    }
  }
  return undefined;
}

function flowNodeTypeForPreview(node: CompilePreviewWorkflowNode): NodeType {
  switch (node.type) {
    case 'prompt':
      return 'agent';
    case 'result':
    case 'output':
      return 'result';
    case 'conditional':
      return 'conditional';
    case 'agent_run':
    case 'novo_run':
    case 'contract_extract':
    case 'workflow_ref':
    case 'child_workflow':
      return node.type;
    default:
      return 'operation';
  }
}

function buildFrameNode(group: CompilePreviewAggregationGroup, nodes: Node<NodeData>[]): Node<NodeData> {
  const bounds = calculateCompiledAggregationFrameBounds(nodes);
  const method = group.method || undefined;

  return {
    id: `${group.anchor_node_id}--frame`,
    type: 'aggregation_frame',
    position: { x: bounds.x, y: bounds.y },
    zIndex: -1,
    selectable: true,
    draggable: true,
    style: {
      width: bounds.width,
      height: bounds.height,
    },
    data: {
      type: 'aggregation_frame',
      label: 'Compiled aggregation preview',
      config: {
        aggregationInternalState: 'expanded',
        aggregationAnchorId: group.anchor_node_id,
        sourceLocked: true,
        aggregationFrameTitle: method ? `${method} compiled preview` : 'Compiled aggregation preview',
        aggregationFrameMethod: method,
        aggregationFrameSourceWorkflowId: group.source_workflow_id,
        aggregationFrameLLMJobCount: group.llm_job_count,
        aggregationFrameTopLevelLLMJobCount: group.top_level_llm_job_count,
        aggregationFrameConditionalLLMJobCount: group.conditional_llm_job_count,
        aggregationFrameOperationCount: group.operation_count,
      },
    },
  };
}
