import { describe, expect, it } from 'bun:test';
import type { Edge, Node } from '@xyflow/react';
import type { CompilePreviewResponse, CompilePreviewWorkflowNode, NodeData } from '../types/workflow';
import { filterEphemeralAggregationGraph } from './aggregationPlan';
import { buildCompiledAggregationPreviewGroup, buildForkedCompiledAggregationGroup } from './compiledPreviewToFlow';
import { flowToWorkflow } from './flowToWorkflow';

const parentNodes = (): Node<NodeData>[] => [
  {
    id: 'agent-deepseek-flash',
    type: 'agent',
    position: { x: 100, y: 80 },
    data: { type: 'agent', label: 'DeepSeek', config: { model: 'deepseek/deepseek-v4-flash' } },
  },
  {
    id: 'agent-minimax',
    type: 'agent',
    position: { x: 100, y: 240 },
    data: { type: 'agent', label: 'MiniMax', config: { model: 'minimax/minimax-m3' } },
  },
  {
    id: 'agent-mimo',
    type: 'agent',
    position: { x: 100, y: 400 },
    data: { type: 'agent', label: 'MiMo', config: { model: 'xiaomi/mimo-v2.5' } },
  },
  {
    id: 'agg',
    type: 'aggregation',
    position: { x: 420, y: 240 },
    data: {
      type: 'aggregation',
      label: 'Peer Matrix',
      config: { aggregationMethod: 'peer_matrix', aggregationWorkflowId: 'aggregation-peer-matrix' },
    },
  },
  {
    id: 'final',
    type: 'result',
    position: { x: 900, y: 240 },
    data: { type: 'result', label: 'Final', config: { name: 'final_answer' } },
  },
];

const parentEdges: Edge[] = [
  { id: 'deepseek-agg', source: 'agent-deepseek-flash', target: 'agg' },
  { id: 'minimax-agg', source: 'agent-minimax', target: 'agg' },
  { id: 'mimo-agg', source: 'agent-mimo', target: 'agg' },
  { id: 'agg-final', source: 'agg', target: 'final' },
];

const candidates = ['agent-deepseek-flash', 'agent-minimax', 'agent-mimo'];

const peerPreview = (): CompilePreviewResponse => {
  const extractIDs = candidates.map((id) => `agg--extract-answer-${id}`);
  const reviewIDs = candidates.flatMap((reviewer) =>
    candidates.filter((candidate) => candidate !== reviewer).map((candidate) => `agg--review-${reviewer}-${candidate}`),
  );
  const parseReviewIDs = reviewIDs.map((id) => `agg--parse-${id.replace('agg--', '')}`);
  const nodeIDs = [
    ...extractIDs,
    'agg--check-unanimous',
    'agg--unanimous-policy',
    ...reviewIDs,
    ...parseReviewIDs,
    'agg--reduce-peer-scores',
    'agg--select-winner',
    'agg--result',
  ];

  return {
    workflow_id: 'preview-test',
    aggregation_groups: [
      {
        anchor_node_id: 'agg',
        method: 'peer_matrix',
        source_workflow_id: 'aggregation-peer-matrix',
        terminal_node_id: 'agg--result',
        presentation_result_id: 'final',
        input_node_ids: candidates,
        node_ids: nodeIDs,
        llm_job_count: 6,
        top_level_llm_job_count: 6,
        conditional_llm_job_count: 0,
        conditional_llm_jobs: [],
        operation_count: 13,
      },
    ],
    nodes: [
      ...extractIDs.map((id) => previewNode(id, 'operation')),
      previewNode('agg--check-unanimous', 'operation'),
      previewNode('agg--unanimous-policy', 'operation'),
      ...reviewIDs.map((id) => previewNode(id, 'prompt')),
      ...parseReviewIDs.map((id) => previewNode(id, 'operation')),
      previewNode('agg--reduce-peer-scores', 'operation'),
      previewNode('agg--select-winner', 'operation'),
      previewNode('agg--result', 'result'),
    ],
    edges: [
      ...candidates.map((candidate) => ({
        id: `edge-${candidate}-extract`,
        source: candidate,
        target: `agg--extract-answer-${candidate}`,
      })),
      ...extractIDs.map((id) => ({ id: `edge-${id}-check`, source: id, target: 'agg--check-unanimous' })),
      { id: 'edge-check-unanimous-policy', source: 'agg--check-unanimous', target: 'agg--unanimous-policy' },
      ...reviewIDs.map((id) => ({ id: `edge-check-${id}`, source: 'agg--check-unanimous', target: id })),
      ...reviewIDs.map((id, index) => ({ id: `edge-${id}-parse`, source: id, target: parseReviewIDs[index] })),
      ...parseReviewIDs.map((id) => ({ id: `edge-${id}-reduce`, source: id, target: 'agg--reduce-peer-scores' })),
      { id: 'edge-reduce-select', source: 'agg--reduce-peer-scores', target: 'agg--select-winner' },
      { id: 'edge-unanimous-select', source: 'agg--unanimous-policy', target: 'agg--select-winner' },
      { id: 'edge-select-result', source: 'agg--select-winner', target: 'agg--result' },
      { id: 'edge-result-final', source: 'agg--result', target: 'final' },
    ],
  };
};

const judgePreview = (): CompilePreviewResponse => ({
  workflow_id: 'preview-test',
  aggregation_groups: [
    {
      anchor_node_id: 'agg',
      method: 'judge',
      source_workflow_id: 'aggregation-judge',
      terminal_node_id: 'agg--result',
      presentation_result_id: 'final',
      input_node_ids: candidates,
      node_ids: [
        'agg--format-blind-candidates',
        'agg--judge',
        'agg--parse-selection',
        'agg--repair-selection',
        'agg--select-winner',
        'agg--result',
      ],
      llm_job_count: 2,
      top_level_llm_job_count: 1,
      conditional_llm_job_count: 1,
      conditional_llm_jobs: [
        {
          id: 'agg--repair-selection--true',
          parent_node_id: 'agg--repair-selection',
          branch: 'true',
          type: 'prompt',
          model: 'deepseek/deepseek-v4-pro',
          label: 'repair selection call',
        },
      ],
      operation_count: 3,
    },
  ],
  nodes: [
    previewNode('agg--format-blind-candidates', 'operation'),
    previewNode('agg--judge', 'prompt'),
    previewNode('agg--parse-selection', 'operation'),
    previewNode('agg--repair-selection', 'conditional'),
    previewNode('agg--select-winner', 'operation'),
    previewNode('agg--result', 'result'),
  ],
  edges: [
    { id: 'edge-agent-format', source: 'agent-deepseek-flash', target: 'agg--format-blind-candidates' },
    { id: 'edge-format-judge', source: 'agg--format-blind-candidates', target: 'agg--judge' },
    { id: 'edge-judge-parse', source: 'agg--judge', target: 'agg--parse-selection' },
    { id: 'edge-parse-repair', source: 'agg--parse-selection', target: 'agg--repair-selection' },
    { id: 'edge-repair-select', source: 'agg--repair-selection', target: 'agg--select-winner' },
    { id: 'edge-select-result', source: 'agg--select-winner', target: 'agg--result' },
  ],
});

const scoringPreview = (): CompilePreviewResponse => ({
  workflow_id: 'preview-test',
  aggregation_groups: [
    {
      anchor_node_id: 'agg',
      method: 'scoring',
      source_workflow_id: 'aggregation-scoring',
      terminal_node_id: 'agg--result',
      presentation_result_id: 'final',
      input_node_ids: candidates,
      node_ids: ['agg--format', 'agg--score-a', 'agg--score-b', 'agg--score-c', 'agg--select', 'agg--result'],
      llm_job_count: 3,
      top_level_llm_job_count: 3,
      conditional_llm_job_count: 0,
      conditional_llm_jobs: [],
      operation_count: 3,
    },
  ],
  nodes: [
    previewNode('agg--format', 'input'),
    previewNode('agg--score-a', 'prompt'),
    previewNode('agg--score-b', 'prompt'),
    previewNode('agg--score-c', 'prompt'),
    previewNode('agg--select', 'operation'),
    previewNode('agg--result', 'output'),
  ],
  edges: [
    { id: 'edge-agent-format', source: 'agent-deepseek-flash', target: 'agg--format' },
    { id: 'edge-format-score-a', source: 'agg--format', target: 'agg--score-a' },
    { id: 'edge-format-score-b', source: 'agg--format', target: 'agg--score-b' },
    { id: 'edge-format-score-c', source: 'agg--format', target: 'agg--score-c' },
    { id: 'edge-score-a-select', source: 'agg--score-a', target: 'agg--select' },
    { id: 'edge-score-b-select', source: 'agg--score-b', target: 'agg--select' },
    { id: 'edge-score-c-select', source: 'agg--score-c', target: 'agg--select' },
    { id: 'edge-select-result', source: 'agg--select', target: 'agg--result' },
  ],
});

function previewNode(id: string, type: string): CompilePreviewWorkflowNode {
  return {
    id,
    type,
    metadata: {
      label: id.replace(/-/g, ' '),
      aggregation_anchor_id: 'agg',
      aggregation_group_node_id: 'agg--result',
      source_workflow_id: 'aggregation-peer-matrix',
      source_node_id: id.replace('agg--', ''),
    },
  };
}

function edgeTargets(edges: Edge[], source: string, target: string): Edge[] {
  return edges.filter((edge) => edge.source === source && edge.target === target);
}

describe('buildCompiledAggregationPreviewGroup', () => {
  it('renders real-id peer matrix jobs in a frame without duplicating the terminal presentation edge', () => {
    const group = buildCompiledAggregationPreviewGroup({
      anchorNodeId: 'agg',
      parentNodes: parentNodes(),
      parentEdges,
      preview: peerPreview(),
    });

    expect(group.nodes.find((node) => node.id === 'agg--frame')?.data.type).toBe('aggregation_frame');
    expect(group.nodes.filter((node) => node.data.type === 'agent')).toHaveLength(6);
    expect(edgeTargets(group.edges, 'agent-deepseek-flash', 'agg--extract-answer-agent-deepseek-flash')).toHaveLength(
      1,
    );
    expect(edgeTargets(group.edges, 'agent-deepseek-flash', 'agg--extract-answer-agent-minimax')).toHaveLength(0);
    expect(edgeTargets(group.edges, 'agg--result', 'final')).toHaveLength(1);
    expect(group.edges.some((edge) => edge.source === 'agg' || edge.target === 'agg')).toBe(false);
    expect(group.edges.some((edge) => edge.id.startsWith('preview-preview-'))).toBe(false);
    expect(group.nodes.find((node) => node.id === 'agg--frame')?.zIndex).toBe(-1);
    expect(group.nodes.find((node) => node.id === 'agg--frame')?.style).not.toHaveProperty('zIndex');
    expect(group.nodes.some((node) => node.parentId)).toBe(false);
  });

  it('allows preview internals and the preview frame to be dragged', () => {
    const group = buildCompiledAggregationPreviewGroup({
      anchorNodeId: 'agg',
      parentNodes: parentNodes(),
      parentEdges,
      preview: scoringPreview(),
    });
    const frame = group.nodes.find((node) => node.id === 'agg--frame');
    const internals = group.nodes.filter((node) => node.id !== 'agg--frame');

    expect(frame?.draggable).toBe(true);
    expect(frame?.selectable).toBe(true);
    expect(internals.length).toBeGreaterThan(0);
    expect(internals.every((node) => node.draggable === true)).toBe(true);
  });

  it('renders conditional branch LLM jobs returned by the backend', () => {
    const group = buildCompiledAggregationPreviewGroup({
      anchorNodeId: 'agg',
      parentNodes: parentNodes(),
      parentEdges,
      preview: judgePreview(),
    });

    expect(group.nodes.find((node) => node.id === 'agg--repair-selection--true')?.data.type).toBe('agent');
    expect(edgeTargets(group.edges, 'agg--repair-selection', 'agg--repair-selection--true')).toHaveLength(1);
    const frameConfig = group.nodes.find((node) => node.id === 'agg--frame')?.data.config;
    expect(frameConfig?.aggregationFrameLLMJobCount).toBe(2);
    expect(frameConfig?.aggregationFrameTopLevelLLMJobCount).toBe(1);
    expect(frameConfig?.aggregationFrameConditionalLLMJobCount).toBe(1);
  });

  it('does not emit branch-job edges when the branch parent is not rendered', () => {
    const preview = judgePreview();
    preview.aggregation_groups[0].conditional_llm_jobs.push({
      id: 'agg--nested-branch-call',
      parent_node_id: 'agg--nested-condition',
      branch: 'true',
      type: 'prompt',
      model: 'deepseek/deepseek-v4-pro',
      label: 'nested branch call',
    });

    const group = buildCompiledAggregationPreviewGroup({
      anchorNodeId: 'agg',
      parentNodes: parentNodes(),
      parentEdges,
      preview,
    });

    expect(group.nodes.find((node) => node.id === 'agg--nested-branch-call')?.data.type).toBe('agent');
    expect(edgeTargets(group.edges, 'agg--nested-condition', 'agg--nested-branch-call')).toHaveLength(0);
  });

  it('renders scoring jobs and maps compiled result nodes to valid builder result nodes', () => {
    const group = buildCompiledAggregationPreviewGroup({
      anchorNodeId: 'agg',
      parentNodes: parentNodes(),
      parentEdges,
      preview: scoringPreview(),
    });

    expect(group.nodes.filter((node) => node.data.type === 'agent')).toHaveLength(3);
    expect(group.nodes.find((node) => node.id === 'agg--format')?.data.type).toBe('operation');
    const result = group.nodes.find((node) => node.id === 'agg--result');
    expect(result?.data.type).toBe('result');
    expect(result?.data.config.operationType).toBeUndefined();
    expect(result?.data.config.outputFormat).toBeUndefined();
  });

  it('does not synthesize an edge when the presentation result is not on the canvas', () => {
    const preview = peerPreview();
    preview.edges = preview.edges.filter((edge) => edge.target !== 'final');
    preview.aggregation_groups[0].presentation_result_id = 'missing-final';

    const group = buildCompiledAggregationPreviewGroup({
      anchorNodeId: 'agg',
      parentNodes: parentNodes(),
      parentEdges,
      preview,
    });

    expect(edgeTargets(group.edges, 'agg--result', 'missing-final')).toHaveLength(0);
  });

  it('handles empty aggregation groups and missing anchors as empty render output', () => {
    const preview = peerPreview();
    preview.aggregation_groups[0].node_ids = [];

    expect(
      buildCompiledAggregationPreviewGroup({
        anchorNodeId: 'agg',
        parentNodes: parentNodes(),
        parentEdges,
        preview,
      }),
    ).toEqual({ nodes: [], edges: [] });
    expect(
      buildCompiledAggregationPreviewGroup({
        anchorNodeId: 'missing-agg',
        parentNodes: parentNodes(),
        parentEdges,
        preview: peerPreview(),
      }),
    ).toEqual({ nodes: [], edges: [] });
  });

  it('renders a blank-method preview frame without leaking an invalid aggregation method into config', () => {
    const preview = scoringPreview();
    preview.aggregation_groups[0].method = '';

    const group = buildCompiledAggregationPreviewGroup({
      anchorNodeId: 'agg',
      parentNodes: parentNodes(),
      parentEdges,
      preview,
    });
    const frame = group.nodes.find((node) => node.id === 'agg--frame');

    expect(frame?.data.config.aggregationFrameTitle).toBe('Compiled aggregation preview');
    expect(frame?.data.config.aggregationFrameMethod).toBeUndefined();
  });

  it('lays out preview nodes and frame deterministically from returned edges', () => {
    const first = buildCompiledAggregationPreviewGroup({
      anchorNodeId: 'agg',
      parentNodes: parentNodes(),
      parentEdges,
      preview: scoringPreview(),
    });
    const second = buildCompiledAggregationPreviewGroup({
      anchorNodeId: 'agg',
      parentNodes: parentNodes(),
      parentEdges,
      preview: scoringPreview(),
    });
    const positions = (nodes: Node<NodeData>[]) =>
      nodes
        .map((node) => [node.id, node.position.x, node.position.y])
        .sort((left, right) => String(left[0]).localeCompare(String(right[0])));

    const firstByID = new Map(first.nodes.map((node) => [node.id, node]));
    const frame = firstByID.get('agg--frame');
    const internals = first.nodes.filter((node) => node.id !== 'agg--frame');
    const frameRight = (frame?.position.x ?? 0) + Number(frame?.style?.width ?? 0);
    const frameBottom = (frame?.position.y ?? 0) + Number(frame?.style?.height ?? 0);

    expect(positions(first.nodes)).toEqual(positions(second.nodes));
    expect(firstByID.get('agg--format')?.position.y).toBeLessThan(firstByID.get('agg--score-a')?.position.y ?? 0);
    expect(firstByID.get('agg--format')?.position.y).toBeLessThan(firstByID.get('agg--score-b')?.position.y ?? 0);
    expect(firstByID.get('agg--format')?.position.y).toBeLessThan(firstByID.get('agg--score-c')?.position.y ?? 0);
    expect(firstByID.get('agg--score-a')?.position.y).toBeLessThan(firstByID.get('agg--select')?.position.y ?? 0);
    expect(firstByID.get('agg--select')?.position.y).toBeLessThan(firstByID.get('agg--result')?.position.y ?? 0);
    expect(firstByID.get('agg--score-a')?.position.x).not.toBe(firstByID.get('agg--score-b')?.position.x);
    expect(internals.every((node) => node.position.x >= (frame?.position.x ?? 0))).toBe(true);
    expect(internals.every((node) => node.position.y >= (frame?.position.y ?? 0))).toBe(true);
    expect(internals.every((node) => node.position.x + Number(node.style?.width ?? 0) <= frameRight)).toBe(true);
    expect(internals.every((node) => node.position.y + Number(node.style?.height ?? 128) <= frameBottom)).toBe(true);
  });

  it('filters frame, preview internals, and preview-hidden macro edges from persistence', () => {
    const group = buildCompiledAggregationPreviewGroup({
      anchorNodeId: 'agg',
      parentNodes: parentNodes(),
      parentEdges,
      preview: peerPreview(),
    });
    const hiddenParentEdges = parentEdges.map((edge) =>
      edge.source === 'agg' || edge.target === 'agg'
        ? { ...edge, hidden: true, data: { aggregationPreviewHidden: true } }
        : edge,
    );

    const filtered = filterEphemeralAggregationGraph({
      nodes: [...parentNodes(), ...group.nodes],
      edges: [...hiddenParentEdges, ...group.edges],
    });

    expect(filtered.nodes.map((node) => node.id).sort()).toEqual(
      parentNodes()
        .map((node) => node.id)
        .sort(),
    );
    expect(filtered.edges).toEqual(parentEdges);
  });
});

describe('buildForkedCompiledAggregationGroup', () => {
  it('builds editable forked nodes from backend compiled preview without a preview frame', () => {
    const group = buildForkedCompiledAggregationGroup({
      anchorNodeId: 'agg',
      parentNodes: parentNodes(),
      parentEdges,
      preview: peerPreview(),
    });

    expect(group.nodes.some((node) => node.id === 'agg--frame')).toBe(false);
    expect(group.nodes.length).toBeGreaterThan(0);
    expect(group.nodes.every((node) => node.draggable === true && node.selectable === true)).toBe(true);
    expect(group.nodes.every((node) => node.data.config.aggregationInternalState === 'forked')).toBe(true);
    expect(group.nodes.every((node) => node.data.config.sourceLocked === false)).toBe(true);
    expect(group.nodes.find((node) => node.id === 'agg--review-agent-deepseek-flash-agent-minimax')?.data.type).toBe(
      'agent',
    );
    expect(edgeTargets(group.edges, 'agg--result', 'final')).toHaveLength(1);
    expect(group.edges.some((edge) => edge.source === 'agg' || edge.target === 'agg')).toBe(false);
  });

  it('materializes backend conditional branch jobs as editable forked branch nodes', () => {
    const group = buildForkedCompiledAggregationGroup({
      anchorNodeId: 'agg',
      parentNodes: parentNodes(),
      parentEdges,
      preview: judgePreview(),
    });

    const branchNode = group.nodes.find((node) => node.id === 'agg--repair-selection--true');
    expect(branchNode?.data.type).toBe('agent');
    expect(branchNode?.data.config.aggregationInternalState).toBe('forked');
    expect(branchNode?.data.config.aggregationBranch).toBe('true');
    expect(branchNode?.data.config.aggregationBranchParentId).toBe('agg--repair-selection');

    const branchEdges = edgeTargets(group.edges, 'agg--repair-selection', 'agg--repair-selection--true');
    expect(branchEdges).toHaveLength(1);
    expect(branchEdges[0].sourceHandle).toBe('true');
  });

  it('round-trips a forked backend preview into runtime nodes and inline branch config', () => {
    const preview = judgePreview();
    const group = buildForkedCompiledAggregationGroup({
      anchorNodeId: 'agg',
      parentNodes: parentNodes(),
      parentEdges,
      preview,
    });
    const authoredNodes = [...parentNodes().filter((node) => node.id !== 'agg'), ...group.nodes];
    const authoredEdges = [
      ...parentEdges.filter((edge) => edge.source !== 'agg' && edge.target !== 'agg'),
      ...group.edges,
    ];

    const workflow = flowToWorkflow(authoredNodes, authoredEdges);
    const runtimeIDs = new Set(workflow.nodes.map((node) => node.id));
    const runtimeEdges = workflow.edges ?? [];
    const groupNodeIDs = preview.aggregation_groups[0].node_ids;

    expect(runtimeIDs.has('agg')).toBe(false);
    for (const nodeID of groupNodeIDs) {
      expect(runtimeIDs.has(nodeID)).toBe(true);
    }

    const conditional = workflow.nodes.find((node) => node.id === 'agg--repair-selection');
    expect(conditional?.type).toBe('conditional');
    expect(conditional?.true_branch?.type).toBe('prompt');
    expect(conditional?.true_branch?.model).toBe('deepseek/deepseek-v4-pro');
    expect(runtimeIDs.has('agg--repair-selection--true')).toBe(false);
    expect(
      runtimeEdges.some(
        (edge) => edge.source === 'agg--repair-selection' && edge.target === 'agg--repair-selection--true',
      ),
    ).toBe(false);
    expect(
      runtimeEdges.some((edge) => edge.source === 'agg--repair-selection' && edge.target === 'agg--select-winner'),
    ).toBe(true);
    expect(runtimeEdges.some((edge) => edge.source === 'agg--result' && edge.target === 'final')).toBe(true);
  });
});
