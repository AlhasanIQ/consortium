import { beforeAll, describe, expect, it } from 'bun:test';
import { ReactFlowProvider } from '@xyflow/react';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';
import type { NodeData } from '../../../types/workflow';

type NodeComponent = React.ElementType;

let AgentNode: NodeComponent;
let AggregationFrameNode: NodeComponent;
let ConditionalNode: NodeComponent;
let OperationNode: NodeComponent;

const ensureLocalStorage = () => {
  if (typeof globalThis.localStorage !== 'undefined') return;

  const store = new Map<string, string>();
  (globalThis as unknown as { localStorage: Storage }).localStorage = {
    getItem: (key: string) => store.get(key) ?? null,
    setItem: (key: string, value: string) => {
      store.set(key, value);
    },
    removeItem: (key: string) => {
      store.delete(key);
    },
    clear: () => {
      store.clear();
    },
    key: (_index: number) => null,
    length: 0,
  };
};

const renderNode = (Component: NodeComponent, data: NodeData, id = 'node-id') =>
  renderToStaticMarkup(
    React.createElement(
      ReactFlowProvider,
      null,
      React.createElement(Component, { id, data, selected: false, isConnectable: true }),
    ),
  );

const previewConfig = {
  aggregationInternalState: 'expanded' as const,
  aggregationAnchorId: 'agg',
  sourceLocked: true,
  sourceWorkflowId: 'aggregation-peer-matrix',
};

describe('compiled aggregation preview nodes', () => {
  beforeAll(async () => {
    ensureLocalStorage();
    ({ default: AgentNode } = await import('./AgentNode'));
    ({ default: AggregationFrameNode } = await import('./AggregationFrameNode'));
    ({ default: ConditionalNode } = await import('./ConditionalNode'));
    ({ default: OperationNode } = await import('./OperationNode'));
  });

  it('renders the aggregation frame title and job counts as a draggable container', () => {
    const html = renderNode(AggregationFrameNode, {
      type: 'aggregation_frame',
      label: 'Compiled aggregation preview',
      config: {
        ...previewConfig,
        aggregationFrameTitle: 'peer_matrix compiled preview',
        aggregationFrameMethod: 'peer_matrix',
        aggregationFrameSourceWorkflowId: 'aggregation-peer-matrix',
        aggregationFrameLLMJobCount: 6,
        aggregationFrameOperationCount: 11,
      },
    });

    expect(html).toContain('data-testid="aggregation-frame-node"');
    expect(html).toContain('data-node-id="node-id"');
    expect(html).toContain('peer_matrix compiled preview');
    expect(html).toContain('aggregation-peer-matrix');
    expect(html).toContain('6 LLM jobs');
    expect(html).toContain('11 ops');
    expect(html).toContain('pointer-events:auto');
    expect(html).toContain('cursor:move');
  });

  it('renders conditional LLM job counts separately from top-level jobs', () => {
    const html = renderNode(AggregationFrameNode, {
      type: 'aggregation_frame',
      label: 'Compiled aggregation preview',
      config: {
        ...previewConfig,
        aggregationFrameTitle: 'judge compiled preview',
        aggregationFrameMethod: 'judge',
        aggregationFrameLLMJobCount: 2,
        aggregationFrameTopLevelLLMJobCount: 1,
        aggregationFrameConditionalLLMJobCount: 1,
        aggregationFrameOperationCount: 3,
      },
    });

    expect(html).toContain('2 LLM jobs (1 top-level + 1 conditional)');
    expect(html).toContain('3 ops');
  });

  it('derives top-level LLM job count from total when only conditional count is present', () => {
    const html = renderNode(AggregationFrameNode, {
      type: 'aggregation_frame',
      label: 'Compiled aggregation preview',
      config: {
        ...previewConfig,
        aggregationFrameTitle: 'judge compiled preview',
        aggregationFrameMethod: 'judge',
        aggregationFrameLLMJobCount: 2,
        aggregationFrameConditionalLLMJobCount: 1,
        aggregationFrameOperationCount: 3,
      },
    });

    expect(html).toContain('2 LLM jobs (1 top-level + 1 conditional)');
  });

  it('shows preview badges and model routing on read-only agent nodes', () => {
    const html = renderNode(
      AgentNode,
      {
        type: 'agent',
        label: 'review agent',
        config: {
          ...previewConfig,
          alias: 'agg--review-agent-a-agent-b',
          model: 'deepseek/deepseek-v4-flash',
        },
      },
      'agg--review-agent-a-agent-b',
    );

    expect(html).toContain('data-testid="workflow-node-agent"');
    expect(html).toContain('Compiled preview');
    expect(html).toContain('aggregation-peer-matrix');
    expect(html).toContain('deepseek/deepseek-v4-flash');
  });

  it('shows a single preview source badge on operation nodes', () => {
    const html = renderNode(OperationNode, {
      type: 'operation',
      label: 'Reduce scores',
      config: { ...previewConfig, operationType: 'reduce_scores' },
    });

    expect(html).toContain('data-testid="workflow-node-operation"');
    expect(html).toContain('Compiled preview');
    expect(html.split('aggregation-peer-matrix')).toHaveLength(2);
  });

  it('preserves source provenance on forked operation nodes', () => {
    const html = renderNode(OperationNode, {
      type: 'operation',
      label: 'Forked reducer',
      config: {
        aggregationInternalState: 'forked',
        sourceWorkflowId: 'aggregation-peer-matrix',
        operationType: 'reduce_scores',
      },
    });

    expect(html).toContain('aggregation-peer-matrix');
    expect(html).not.toContain('Compiled preview');
  });

  it('shows preview badges only for expanded conditional nodes', () => {
    const expanded = renderNode(ConditionalNode, {
      type: 'conditional',
      label: 'Repair gate',
      config: { ...previewConfig, condition: '{{parse-selection}} == null' },
    });
    const forked = renderNode(ConditionalNode, {
      type: 'conditional',
      label: 'Editable fork',
      config: { aggregationInternalState: 'forked', sourceWorkflowId: 'aggregation-peer-matrix' },
    });

    expect(expanded).toContain('data-testid="workflow-node-conditional"');
    expect(expanded).toContain('Compiled preview');
    expect(forked).not.toContain('Compiled preview');
  });
});
