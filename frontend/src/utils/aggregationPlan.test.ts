import { describe, expect, it } from 'bun:test';
import type { Edge, Node } from '@xyflow/react';
import type { NodeData } from '../types/workflow';
import { buildAggregationPlan, filterEphemeralAggregationGraph } from './aggregationPlan';

describe('buildAggregationPlan', () => {
  const inputIds = ['agent-a', 'agent-b', 'agent-c'];

  it('returns collapsed-card presentation metadata for collect', () => {
    const plan = buildAggregationPlan({ method: 'collect', inputIds, config: { separator: '\n---\n' } });

    expect(plan.title).toBe('Collect');
    expect(plan.modelOwnership).toBe('none');
    expect(plan.inputCount).toBe(3);
    expect(plan.steps).toEqual([]);
    expect(plan.edges).toEqual([]);
  });

  it('returns conditional model ownership for majority vote with synthesis fallback', () => {
    const plan = buildAggregationPlan({
      method: 'majority_vote',
      inputIds,
      config: { tie_breaker_method: 'synthesis', tie_breaker_model: 'openai/gpt-4o-mini' },
    });

    expect(plan.title).toBe('Majority Vote');
    expect(plan.modelOwnership).toBe('conditional');
    expect(plan.steps).toEqual([]);
    expect(plan.edges).toEqual([]);
  });

  it('does not invent majority vote synthesis ownership without explicit config', () => {
    const plan = buildAggregationPlan({
      method: 'majority_vote',
      inputIds,
      config: {},
    });

    expect(plan.title).toBe('Majority Vote');
    expect(plan.modelOwnership).toBe('none');
  });

  it('returns owned model metadata for judge-style methods', () => {
    const plan = buildAggregationPlan({
      method: 'debate_decide',
      inputIds,
      config: { judge_model: 'anthropic/claude-sonnet-4.5' },
    });

    expect(plan.title).toBe('Debate & Decide');
    expect(plan.modelOwnership).toBe('owned');
  });

  it('returns owned model metadata for judge', () => {
    const plan = buildAggregationPlan({ method: 'judge', inputIds, config: { judge_model: 'openai/gpt-5-mini' } });

    expect(plan.title).toBe('Judge');
    expect(plan.modelOwnership).toBe('owned');
  });

  it('does not expose local scoring topology, fanout counts, or skip policy', () => {
    const plan = buildAggregationPlan({
      method: 'scoring',
      inputIds,
      config: { rubric_mode: 'dynamic', scoring_model: 'openai/gpt-5-mini' },
    });

    expect(plan.title).toBe('Scoring');
    expect(plan.modelOwnership).toBe('owned');
    expect(plan.steps).toEqual([]);
    expect(plan.edges).toEqual([]);
  });

  it('returns owned model metadata for synthesis', () => {
    const plan = buildAggregationPlan({ method: 'synthesis', inputIds, config: { model: 'openai/gpt-5-mini' } });

    expect(plan.title).toBe('Synthesis');
    expect(plan.modelOwnership).toBe('owned');
  });

  it('does not expose local peer-matrix topology, fanout counts, or skip policy', () => {
    const plan = buildAggregationPlan({
      method: 'peer_matrix',
      inputIds,
      config: { rubric_mode: 'dynamic', rubric_model: 'openai/gpt-5-mini', max_parallel: 4 },
    });

    expect(plan.title).toBe('Peer Matrix');
    expect(plan.modelOwnership).toBe('inherited');
    expect(plan.steps).toEqual([]);
    expect(plan.edges).toEqual([]);
  });
});

const aggregationAnchor = (): Node<NodeData> => ({
  id: 'agg-judge',
  type: 'aggregation',
  position: { x: 240, y: 120 },
  data: {
    type: 'aggregation',
    label: 'Judge',
    config: {
      alias: 'agg-judge',
      aggregationMethod: 'judge',
      aggregationWorkflowId: 'aggregation-judge',
      aggregationConfig: { judge_model: 'openai/gpt-5-mini' },
    },
  },
});

describe('filterEphemeralAggregationGraph', () => {
  const parentEdges: Edge[] = [
    { id: 'a-agg', source: 'agent-a', target: 'agg-judge' },
    { id: 'b-agg', source: 'agent-b', target: 'agg-judge' },
    { id: 'agg-result', source: 'agg-judge', target: 'final-result' },
  ];

  it('filters expanded internals while preserving forked internals', () => {
    const baseNodes = [aggregationAnchor()];
    const expandedNode: Node<NodeData> = {
      id: 'agg-judge--agent-judge',
      type: 'agent',
      position: { x: 0, y: 0 },
      data: {
        type: 'agent',
        label: 'Judge Preview',
        config: {
          aggregationInternalState: 'expanded',
          aggregationAnchorId: 'agg-judge',
          sourceLocked: true,
        },
      },
    };
    const expandedEdge: Edge = { id: 'preview-edge', source: 'agent-a', target: expandedNode.id };
    const forkedNodes: Node<NodeData>[] = [
      {
        id: 'agg-judge--format-candidates',
        type: 'operation',
        position: { x: 0, y: 0 },
        data: {
          type: 'operation',
          label: 'Format Candidates',
          config: {
            aggregationInternalState: 'forked',
            aggregationAnchorId: 'agg-judge',
            sourceLocked: false,
          },
        },
      },
      {
        id: 'agg-judge--judge',
        type: 'agent',
        position: { x: 0, y: 220 },
        data: {
          type: 'agent',
          label: 'Judge',
          config: {
            aggregationInternalState: 'forked',
            aggregationAnchorId: 'agg-judge',
            sourceLocked: false,
          },
        },
      },
    ];
    const forkedEdges: Edge[] = [
      { id: 'forked-a-format', source: 'agent-a', target: 'agg-judge--format-candidates' },
      { id: 'forked-format-judge', source: 'agg-judge--format-candidates', target: 'agg-judge--judge' },
    ];

    const expandedFiltered = filterEphemeralAggregationGraph({
      nodes: [...baseNodes, expandedNode],
      edges: [...parentEdges, expandedEdge],
    });
    expect(expandedFiltered.nodes.map((node) => node.id)).toEqual(['agg-judge']);
    expect(expandedFiltered.edges).toEqual(parentEdges);

    const forkedFiltered = filterEphemeralAggregationGraph({
      nodes: forkedNodes,
      edges: forkedEdges,
    });
    expect(forkedFiltered.nodes).toEqual(forkedNodes);
    expect(forkedFiltered.edges).toEqual(forkedEdges);
  });

  it('restores preview-hidden macro edges without dropping pre-existing edge data', () => {
    const expandedNode: Node<NodeData> = {
      id: 'agg-judge--agent-judge',
      type: 'agent',
      position: { x: 0, y: 0 },
      data: {
        type: 'agent',
        label: 'Judge Preview',
        config: {
          aggregationInternalState: 'expanded',
          aggregationAnchorId: 'agg-judge',
          sourceLocked: true,
        },
      },
    };
    const hiddenMacroEdge: Edge = {
      id: 'a-agg',
      source: 'agent-a',
      target: 'agg-judge',
      hidden: true,
      data: { role: 'candidate', aggregationPreviewHidden: true },
    };

    const filtered = filterEphemeralAggregationGraph({
      nodes: [aggregationAnchor(), expandedNode],
      edges: [hiddenMacroEdge, { id: 'preview-edge', source: 'agent-a', target: expandedNode.id }],
    });

    expect(filtered.edges).toEqual([
      { id: 'a-agg', source: 'agent-a', target: 'agg-judge', data: { role: 'candidate' } },
    ]);
  });
});
