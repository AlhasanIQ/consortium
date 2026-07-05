import { beforeAll, beforeEach, describe, expect, it } from 'bun:test';
import type { Edge, Node } from '@xyflow/react';
import type { CompilePreviewResponse, FlowNode, NodeData } from '../types/workflow';

type WorkflowStoreModule = typeof import('./workflowStore');
type LocalStorageModule = typeof import('../utils/localStorage');
let useWorkflowStore: WorkflowStoreModule['useWorkflowStore'];
let saveWorkflowToLocalStorage: LocalStorageModule['saveWorkflowToLocalStorage'];
let loadWorkflowFromLocalStorage: LocalStorageModule['loadWorkflowFromLocalStorage'];

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

const ensureAlert = () => {
  (globalThis as unknown as { alert: (message?: string) => void }).alert = () => {};
};

const resetWorkflowStore = () => {
  useWorkflowStore.setState({
    nodes: [],
    edges: [],
    selectedNodeId: null,
    selectedEdgeId: null,
    validationErrors: [],
    viewportCenter: null,
    history: [{ nodes: [], edges: [] }],
    historyIndex: 0,
    nodeExecutionStates: {},
    replayJobId: null,
    replayTimestamp: null,
    replayJob: null,
    replayNodes: [],
    replayNodeResults: {},
    replaySelectedNodeId: null,
    replayFocusNonce: 0,
  });
};

describe('WorkflowStore replay mode', () => {
  beforeAll(async () => {
    ensureLocalStorage();
    ensureAlert();
    ({ useWorkflowStore } = await import('./workflowStore'));
    ({ saveWorkflowToLocalStorage, loadWorkflowFromLocalStorage } = await import('../utils/localStorage'));
  });

  beforeEach(() => {
    globalThis.localStorage.clear();
    resetWorkflowStore();
  });

  it('prefers error_message over metadata for failed nodes', () => {
    useWorkflowStore.getState().enterReplayMode(
      {
        id: 'job-1',
        status: 'failed',
        created_at: '2025-01-01T00:00:00Z',
      },
      [
        {
          node_id: 'node-1',
          node_type: 'prompt',
          status: 'failed',
          output: '',
          error_message: 'boom',
          metadata: 'fallback metadata',
          tokens_input: 10,
          tokens_output: 5,
          cost: 0.01,
          latency_ms: 120,
        },
      ],
    );

    const state = useWorkflowStore.getState();
    expect(state.nodeExecutionStates['node-1']?.error).toBe('boom');
  });

  it('undo and redo navigate one change at a time', () => {
    const store = useWorkflowStore.getState();

    store.addNode('agent', { x: 0, y: 0 });
    store.addNode('agent', { x: 100, y: 0 });

    let state = useWorkflowStore.getState();
    expect(state.nodes).toHaveLength(2);

    state.undo();
    state = useWorkflowStore.getState();
    expect(state.nodes).toHaveLength(1);

    state.undo();
    state = useWorkflowStore.getState();
    expect(state.nodes).toHaveLength(0);

    state.redo();
    state = useWorkflowStore.getState();
    expect(state.nodes).toHaveLength(1);

    state.redo();
    state = useWorkflowStore.getState();
    expect(state.nodes).toHaveLength(2);
  });

  it('labels Novomo runtime nodes with user-facing names', () => {
    const store = useWorkflowStore.getState();

    store.addNode('agent_run', { x: 0, y: 0 });
    store.addNode('novo_run', { x: 100, y: 0 });

    const state = useWorkflowStore.getState();
    expect(state.nodes[0].data.label).toBe('Novomo Agent');
    expect(state.nodes[0].data.config.sandbox).toBe('docker');
    expect(state.nodes[0].data.config.inheritFromMode).toBe('auto');
    expect(state.nodes[1].data.label).toBe('Superagent');
    expect(state.nodes[1].data.config.identity).toBe('sde-novo');
    expect(state.nodes[1].data.config.sandbox).toBe('docker');
    expect(state.nodes[1].data.config.inheritFromMode).toBe('auto');
    expect(state.nodes[1].data.config.graceSeconds).toBe(10);
  });

  it('automatically selects the upstream Novomo node when connecting a handoff chain', () => {
    const store = useWorkflowStore.getState();

    store.addNode('agent_run', { x: 0, y: 0 });
    store.addNode('novo_run', { x: 100, y: 0 });

    let state = useWorkflowStore.getState();
    const sourceID = state.nodes[0].id;
    const targetID = state.nodes[1].id;

    state.onConnect({ source: sourceID, target: targetID, sourceHandle: null, targetHandle: null });
    state = useWorkflowStore.getState();

    const target = state.nodes.find((node) => node.id === targetID);
    expect(target?.data.config.inheritFromMode).toBe('auto');
    expect(target?.data.config.inheritFromNodeId).toBe(sourceID);
  });

  it('reserves the compiled aggregation delimiter in aliases', () => {
    const store = useWorkflowStore.getState();

    expect(store.isAliasValid('step-a').valid).toBe(true);
    expect(store.isAliasValid('step--a').valid).toBe(false);
  });

  it('filters expanded aggregation previews from validation and localStorage', () => {
    const expandedNode: Node<NodeData> = {
      id: 'agg--agent-judge',
      type: 'agent',
      position: { x: 0, y: 0 },
      data: {
        type: 'agent',
        label: 'Judge Preview',
        config: {
          alias: 'agg--agent-judge',
          aggregationInternalState: 'expanded',
          aggregationAnchorId: 'agg',
          sourceLocked: true,
        },
      },
    };
    const anchorNode: Node<NodeData> = {
      id: 'agg',
      type: 'aggregation',
      position: { x: 0, y: 0 },
      data: {
        type: 'aggregation',
        label: 'Aggregation',
        config: { alias: 'agg', aggregationWorkflowId: 'aggregation-judge' },
      },
    };
    const validOperationNode: Node<NodeData> = {
      id: 'op-valid',
      type: 'operation',
      position: { x: 100, y: 0 },
      data: {
        type: 'operation',
        label: 'Valid Operation',
        config: {
          alias: 'op-valid',
          operationType: 'count_votes',
          operationConfig: { answers: ['A', 'A', 'B'] },
        },
      },
    };
    const previewEdge: Edge = { id: 'preview-edge', source: 'agg--agent-judge', target: 'agg' };

    useWorkflowStore.setState({ nodes: [validOperationNode, expandedNode], edges: [] });
    useWorkflowStore.getState().validateWorkflow();
    expect(useWorkflowStore.getState().validationErrors.some((error) => error.nodeId === expandedNode.id)).toBe(false);

    saveWorkflowToLocalStorage([anchorNode, expandedNode], [previewEdge]);
    const loaded = loadWorkflowFromLocalStorage();
    expect(loaded?.nodes.map((node) => node.id)).toEqual(['agg']);
    expect(loaded?.edges).toEqual([]);
  });

  it('expands compiled aggregation previews in place and restores macro edges on collapse', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'agent-a',
        type: 'agent',
        position: { x: 0, y: 0 },
        data: { type: 'agent', label: 'Agent A', config: { alias: 'agent-a', model: 'openai/gpt-5-mini' } },
      },
      {
        id: 'agg',
        type: 'aggregation',
        position: { x: 260, y: 0 },
        data: {
          type: 'aggregation',
          label: 'Scoring',
          config: { alias: 'agg', aggregationWorkflowId: 'aggregation-scoring', aggregationMethod: 'scoring' },
        },
      },
      {
        id: 'final',
        type: 'result',
        position: { x: 520, y: 0 },
        data: { type: 'result', label: 'Final', config: { alias: 'final', name: 'final_answer' } },
      },
    ];
    const edges: Edge[] = [
      { id: 'agent-agg', source: 'agent-a', target: 'agg', data: { role: 'candidate' } },
      { id: 'agg-final', source: 'agg', target: 'final' },
    ];

    useWorkflowStore.setState({ nodes, edges });
    useWorkflowStore.getState().expandAggregationNode('agg', compilePreview());

    let state = useWorkflowStore.getState();
    expect(state.nodes.some((node) => node.id === 'agg--frame')).toBe(true);
    expect(state.nodes.some((node) => node.id === 'agg--score-agent-a')).toBe(true);
    expect(state.edges.find((edge) => edge.id === 'agent-agg')).toMatchObject({
      hidden: true,
      data: { role: 'candidate', aggregationPreviewHidden: true },
    });
    expect(state.edges.find((edge) => edge.id === 'agg-final')).toMatchObject({
      hidden: true,
      data: { aggregationPreviewHidden: true },
    });
    expect(state.edges.some((edge) => edge.source === 'agg' || edge.target === 'agg')).toBe(true);
    expect(state.edges.some((edge) => edge.source === 'agent-a' && edge.target === 'agg--score-agent-a')).toBe(true);
    expect(state.edges.some((edge) => edge.source === 'agg--result' && edge.target === 'final')).toBe(true);

    useWorkflowStore.getState().collapseAggregationNode('agg');
    state = useWorkflowStore.getState();
    expect(state.nodes.map((node) => node.id).sort()).toEqual(['agent-a', 'agg', 'final']);
    expect(state.edges).toEqual(edges);
  });

  it('collapses expanded previews when an upstream authored node config changes', () => {
    const nodes = aggregationPreviewBaseNodes();
    const edges = aggregationPreviewBaseEdges();

    useWorkflowStore.setState({ nodes, edges, history: [{ nodes, edges }], historyIndex: 0 });
    useWorkflowStore.getState().expandAggregationNode('agg', compilePreview());

    useWorkflowStore.getState().updateNodeConfig('agent-a', {
      alias: 'agent-a',
      model: 'openai/gpt-5',
      prompt: 'updated reasoning prompt',
    });

    const state = useWorkflowStore.getState();
    expect(state.nodes.some((node) => node.id === 'agg--frame')).toBe(false);
    expect(state.nodes.some((node) => node.id === 'agg--score-agent-a')).toBe(false);
    expect(state.nodes.find((node) => node.id === 'agent-a')?.data.config).toMatchObject({
      model: 'openai/gpt-5',
      prompt: 'updated reasoning prompt',
    });
    expect(state.edges).toEqual(edges);
    expect(state.historyIndex).toBe(1);
  });

  it('collapses expanded previews when an upstream authored node is deleted', () => {
    const nodes = aggregationPreviewBaseNodes();
    const edges = aggregationPreviewBaseEdges();

    useWorkflowStore.setState({ nodes, edges, history: [{ nodes, edges }], historyIndex: 0 });
    useWorkflowStore.getState().expandAggregationNode('agg', compilePreview());

    useWorkflowStore.getState().deleteNode('agent-a');

    const state = useWorkflowStore.getState();
    expect(state.nodes.map((node) => node.id).sort()).toEqual(['agg', 'final']);
    expect(state.nodes.some((node) => node.id === 'agg--frame')).toBe(false);
    expect(state.edges).toEqual([{ id: 'agg-final', source: 'agg', target: 'final' }]);
    expect(state.historyIndex).toBe(1);
  });

  it('collapses expanded previews when a non-preview authored edge is deleted', () => {
    const upstreamNode: Node<NodeData> = {
      id: 'agent-b',
      type: 'agent',
      position: { x: -260, y: 0 },
      data: { type: 'agent', label: 'Agent B', config: { alias: 'agent-b', model: 'openai/gpt-5-mini' } },
    };
    const nodes = [upstreamNode, ...aggregationPreviewBaseNodes()];
    const edges = [{ id: 'agent-b-agent-a', source: 'agent-b', target: 'agent-a' }, ...aggregationPreviewBaseEdges()];

    useWorkflowStore.setState({ nodes, edges, history: [{ nodes, edges }], historyIndex: 0 });
    useWorkflowStore.getState().expandAggregationNode('agg', compilePreview());

    useWorkflowStore.getState().onEdgesChange([{ id: 'agent-b-agent-a', type: 'remove' }]);

    const state = useWorkflowStore.getState();
    expect(state.nodes.some((node) => node.id === 'agg--frame')).toBe(false);
    expect(state.nodes.some((node) => node.id === 'agg--score-agent-a')).toBe(false);
    expect(state.edges.map((edge) => edge.id).sort()).toEqual(['agent-agg', 'agg-final'].sort());
    expect(state.historyIndex).toBe(1);
  });

  it('ignores direct deletion of expanded preview edges and hidden macro edges', () => {
    const nodes = aggregationPreviewBaseNodes();
    const edges = aggregationPreviewBaseEdges();

    useWorkflowStore.setState({ nodes, edges, history: [{ nodes, edges }], historyIndex: 0 });
    useWorkflowStore.getState().expandAggregationNode('agg', compilePreview());

    let expanded = useWorkflowStore.getState();
    const historyBeforePreviewDelete = expanded.history;
    expect(expanded.edges.some((edge) => edge.id === 'preview-edge-score-result')).toBe(true);

    useWorkflowStore.setState({ selectedEdgeId: 'preview-edge-score-result' });
    useWorkflowStore.getState().deleteEdge('preview-edge-score-result');

    expanded = useWorkflowStore.getState();
    expect(expanded.edges.some((edge) => edge.id === 'preview-edge-score-result')).toBe(true);
    expect(expanded.history).toBe(historyBeforePreviewDelete);
    expect(expanded.historyIndex).toBe(0);
    expect(expanded.selectedEdgeId).toBeNull();

    const historyBeforeHiddenDelete = expanded.history;
    expect(expanded.edges.find((edge) => edge.id === 'agent-agg')).toMatchObject({
      hidden: true,
      data: { aggregationPreviewHidden: true },
    });

    useWorkflowStore.setState({ selectedEdgeId: 'agent-agg' });
    useWorkflowStore.getState().deleteEdge('agent-agg');

    const state = useWorkflowStore.getState();
    expect(state.edges.find((edge) => edge.id === 'agent-agg')).toMatchObject({
      hidden: true,
      data: { aggregationPreviewHidden: true },
    });
    expect(state.history).toBe(historyBeforeHiddenDelete);
    expect(state.historyIndex).toBe(0);
    expect(state.selectedEdgeId).toBeNull();
  });

  it('filters React Flow remove changes for expanded preview edges', () => {
    const nodes = aggregationPreviewBaseNodes();
    const edges = aggregationPreviewBaseEdges();

    useWorkflowStore.setState({ nodes, edges, history: [{ nodes, edges }], historyIndex: 0 });
    useWorkflowStore.getState().expandAggregationNode('agg', compilePreview());

    const expanded = useWorkflowStore.getState();
    const historyBeforeRemove = expanded.history;
    useWorkflowStore.getState().onEdgesChange([{ id: 'preview-edge-score-result', type: 'remove' }]);

    const state = useWorkflowStore.getState();
    expect(state.edges.some((edge) => edge.id === 'preview-edge-score-result')).toBe(true);
    expect(state.history).toBe(historyBeforeRemove);
    expect(state.historyIndex).toBe(0);
  });

  it('ignores deletion of an aggregation anchor while its preview is expanded', () => {
    const nodes = aggregationPreviewBaseNodes();
    const edges = aggregationPreviewBaseEdges();

    useWorkflowStore.setState({ nodes, edges, history: [{ nodes, edges }], historyIndex: 0 });
    useWorkflowStore.getState().expandAggregationNode('agg', compilePreview());

    const expanded = useWorkflowStore.getState();
    const historyBeforeRemove = expanded.history;

    useWorkflowStore.getState().onNodesChange([{ id: 'agg', type: 'remove' }]);

    let state = useWorkflowStore.getState();
    expect(state.nodes.some((node) => node.id === 'agg')).toBe(true);
    expect(state.nodes.some((node) => node.id === 'agg--frame')).toBe(true);
    expect(state.history).toBe(historyBeforeRemove);
    expect(state.historyIndex).toBe(0);

    useWorkflowStore.getState().deleteNode('agg');

    state = useWorkflowStore.getState();
    expect(state.nodes.some((node) => node.id === 'agg')).toBe(true);
    expect(state.nodes.some((node) => node.id === 'agg--frame')).toBe(true);
    expect(state.history).toBe(historyBeforeRemove);
    expect(state.historyIndex).toBe(0);
  });

  it('rejects alias changes for an aggregation anchor while its preview is expanded', () => {
    const nodes = aggregationPreviewBaseNodes();
    const edges = aggregationPreviewBaseEdges();

    useWorkflowStore.setState({ nodes, edges, history: [{ nodes, edges }], historyIndex: 0 });
    useWorkflowStore.getState().expandAggregationNode('agg', compilePreview());

    const expanded = useWorkflowStore.getState();
    const historyBeforeRename = expanded.history;

    const result = useWorkflowStore.getState().updateNodeAlias('agg', 'agg2');

    const state = useWorkflowStore.getState();
    expect(result.success).toBe(false);
    expect(state.nodes.some((node) => node.id === 'agg')).toBe(true);
    expect(state.nodes.some((node) => node.id === 'agg2')).toBe(false);
    expect(state.nodes.some((node) => node.id === 'agg--frame')).toBe(true);
    expect(state.edges.find((edge) => edge.id === 'agent-agg')).toMatchObject({
      source: 'agent-a',
      target: 'agg',
    });
    expect(state.history).toBe(historyBeforeRename);
    expect(state.historyIndex).toBe(0);
  });

  it('ignores position changes for an aggregation anchor while its preview is expanded', () => {
    const nodes = aggregationPreviewBaseNodes();
    const edges = aggregationPreviewBaseEdges();

    useWorkflowStore.setState({ nodes, edges, history: [{ nodes, edges }], historyIndex: 0 });
    useWorkflowStore.getState().expandAggregationNode('agg', compilePreview());

    const expanded = useWorkflowStore.getState();
    const historyBeforeMove = expanded.history;
    const anchorBefore = expanded.nodes.find((node) => node.id === 'agg');

    useWorkflowStore
      .getState()
      .onNodesChange([{ id: 'agg', type: 'position', position: { x: 999, y: 999 }, dragging: false }]);

    const state = useWorkflowStore.getState();
    expect(state.nodes.find((node) => node.id === 'agg')?.position).toEqual(anchorBefore?.position);
    expect(state.history).toBe(historyBeforeMove);
    expect(state.historyIndex).toBe(0);
  });

  it('rejects new connections to or from an aggregation anchor while its preview is expanded', () => {
    const nodes: FlowNode[] = [
      ...aggregationPreviewBaseNodes(),
      {
        id: 'agent-b',
        type: 'agent',
        position: { x: -260, y: 0 },
        data: { type: 'agent', label: 'Agent B', config: { alias: 'agent-b', model: 'openai/gpt-5-mini' } },
      },
      {
        id: 'audit',
        type: 'result',
        position: { x: 780, y: 0 },
        data: { type: 'result', label: 'Audit', config: { alias: 'audit', name: 'audit' } },
      },
    ];
    const edges = aggregationPreviewBaseEdges();

    useWorkflowStore.setState({ nodes, edges, history: [{ nodes, edges }], historyIndex: 0 });
    useWorkflowStore.getState().expandAggregationNode('agg', compilePreview());

    const expanded = useWorkflowStore.getState();
    const historyBeforeConnect = expanded.history;
    const edgeIDsBefore = expanded.edges.map((edge) => edge.id).sort();

    useWorkflowStore.getState().onConnect({ source: 'agent-b', target: 'agg', sourceHandle: null, targetHandle: null });
    useWorkflowStore.getState().onConnect({ source: 'agg', target: 'audit', sourceHandle: null, targetHandle: null });

    const state = useWorkflowStore.getState();
    expect(state.edges.map((edge) => edge.id).sort()).toEqual(edgeIDsBefore);
    expect(state.history).toBe(historyBeforeConnect);
    expect(state.historyIndex).toBe(0);
  });

  it('deletes authored macro edges when the aggregation is collapsed', () => {
    const nodes = aggregationPreviewBaseNodes();
    const edges = aggregationPreviewBaseEdges();

    useWorkflowStore.setState({ nodes, edges, history: [{ nodes, edges }], historyIndex: 0 });
    useWorkflowStore.getState().deleteEdge('agent-agg');

    const state = useWorkflowStore.getState();
    expect(state.edges.some((edge) => edge.id === 'agent-agg')).toBe(false);
    expect(state.history).toHaveLength(2);
    expect(state.historyIndex).toBe(1);
  });

  it('keeps expanded preview internals out of undo and redo history', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'agent-a',
        type: 'agent',
        position: { x: 0, y: 0 },
        data: { type: 'agent', label: 'Agent A', config: { alias: 'agent-a', model: 'openai/gpt-5-mini' } },
      },
      {
        id: 'agg',
        type: 'aggregation',
        position: { x: 260, y: 0 },
        data: {
          type: 'aggregation',
          label: 'Scoring',
          config: { alias: 'agg', aggregationWorkflowId: 'aggregation-scoring', aggregationMethod: 'scoring' },
        },
      },
      {
        id: 'final',
        type: 'result',
        position: { x: 520, y: 0 },
        data: { type: 'result', label: 'Final', config: { alias: 'final', name: 'final_answer' } },
      },
    ];
    const edges: Edge[] = [
      { id: 'agent-agg', source: 'agent-a', target: 'agg' },
      { id: 'agg-final', source: 'agg', target: 'final' },
    ];

    useWorkflowStore.setState({ nodes, edges, history: [{ nodes, edges }], historyIndex: 0 });
    useWorkflowStore.getState().expandAggregationNode('agg', compilePreview());
    useWorkflowStore
      .getState()
      .onNodesChange([{ id: 'agent-a', type: 'position', position: { x: 40, y: 0 }, dragging: false }]);
    useWorkflowStore.getState().collapseAggregationNode('agg');
    useWorkflowStore.getState().undo();
    useWorkflowStore.getState().redo();

    const state = useWorkflowStore.getState();
    expect(state.nodes.some((node) => node.id.startsWith('agg--'))).toBe(false);
    expect(state.edges.some((edge) => edge.hidden)).toBe(false);
    expect(state.nodes.find((node) => node.id === 'agent-a')?.position).toEqual({ x: 40, y: 0 });
  });

  it('forks an aggregation from backend compiled preview rather than a source workflow descriptor', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'agent-a',
        type: 'agent',
        position: { x: 0, y: 0 },
        data: { type: 'agent', label: 'Agent A', config: { alias: 'agent-a', model: 'openai/gpt-5-mini' } },
      },
      {
        id: 'agg',
        type: 'aggregation',
        position: { x: 260, y: 0 },
        data: {
          type: 'aggregation',
          label: 'Scoring',
          config: { alias: 'agg', aggregationWorkflowId: 'aggregation-scoring', aggregationMethod: 'scoring' },
        },
      },
      {
        id: 'final',
        type: 'result',
        position: { x: 520, y: 0 },
        data: { type: 'result', label: 'Final', config: { alias: 'final', name: 'final_answer' } },
      },
    ];
    const edges: Edge[] = [
      { id: 'agent-agg', source: 'agent-a', target: 'agg' },
      { id: 'agg-final', source: 'agg', target: 'final' },
    ];

    useWorkflowStore.setState({ nodes, edges, history: [{ nodes, edges }], historyIndex: 0 });
    useWorkflowStore.getState().forkAggregationNode('agg', compilePreview());

    const state = useWorkflowStore.getState();
    expect(state.nodes.some((node) => node.id === 'agg')).toBe(false);
    expect(state.nodes.find((node) => node.id === 'agg--score-agent-a')?.data.config).toMatchObject({
      aggregationInternalState: 'forked',
      sourceLocked: false,
      forkedFromWorkflowId: 'aggregation-scoring',
    });
    expect(state.edges.some((edge) => edge.source === 'agent-a' && edge.target === 'agg--score-agent-a')).toBe(true);
    expect(state.edges.some((edge) => edge.source === 'agg--score-agent-a' && edge.target === 'agg--result')).toBe(
      true,
    );
    expect(state.edges.some((edge) => edge.source === 'agg--result' && edge.target === 'final')).toBe(true);
  });

  it('pushes downstream parent nodes below an expanded preview and restores them on collapse', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'agent-a',
        type: 'agent',
        position: { x: 160, y: 120 },
        data: { type: 'agent', label: 'Agent A', config: { alias: 'agent-a', model: 'openai/gpt-5-mini' } },
      },
      {
        id: 'agg',
        type: 'aggregation',
        position: { x: 160, y: 420 },
        data: {
          type: 'aggregation',
          label: 'Scoring',
          config: { alias: 'agg', aggregationWorkflowId: 'aggregation-scoring', aggregationMethod: 'scoring' },
        },
      },
      {
        id: 'final',
        type: 'result',
        position: { x: 160, y: 760 },
        data: { type: 'result', label: 'Final', config: { alias: 'final', name: 'final_answer' } },
      },
    ];
    const edges: Edge[] = [
      { id: 'agent-agg', source: 'agent-a', target: 'agg' },
      { id: 'agg-final', source: 'agg', target: 'final' },
    ];

    useWorkflowStore.setState({ nodes, edges, history: [{ nodes, edges }], historyIndex: 0 });
    useWorkflowStore.getState().expandAggregationNode('agg', compilePreview());

    let state = useWorkflowStore.getState();
    const frame = state.nodes.find((node) => node.id === 'agg--frame') as
      | (FlowNode & { style?: { width?: number; height?: number } })
      | undefined;
    const shiftedFinal = state.nodes.find((node) => node.id === 'final');
    const frameBottom = (frame?.position.y ?? 0) + Number(frame?.style?.height ?? 0);

    expect(frame).toBeDefined();
    expect(shiftedFinal?.position.y).toBeGreaterThan(frameBottom);
    expect(shiftedFinal?.data.config.aggregationPreviewShifts?.agg?.y).toBeGreaterThan(0);

    useWorkflowStore.getState().collapseAggregationNode('agg');

    state = useWorkflowStore.getState();
    expect(state.nodes.find((node) => node.id === 'final')?.position).toEqual({ x: 160, y: 760 });
    expect(state.nodes.find((node) => node.id === 'final')?.data.config.aggregationPreviewShifts).toBeUndefined();
  });

  it('does not shift unrelated lower parent nodes when expanding a preview', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'agent-a',
        type: 'agent',
        position: { x: 160, y: 120 },
        data: { type: 'agent', label: 'Agent A', config: { alias: 'agent-a', model: 'openai/gpt-5-mini' } },
      },
      {
        id: 'agg',
        type: 'aggregation',
        position: { x: 160, y: 420 },
        data: {
          type: 'aggregation',
          label: 'Scoring',
          config: { alias: 'agg', aggregationWorkflowId: 'aggregation-scoring', aggregationMethod: 'scoring' },
        },
      },
      {
        id: 'final',
        type: 'result',
        position: { x: 160, y: 760 },
        data: { type: 'result', label: 'Final', config: { alias: 'final', name: 'final_answer' } },
      },
      {
        id: 'unrelated',
        type: 'result',
        position: { x: 820, y: 900 },
        data: { type: 'result', label: 'Unrelated', config: { alias: 'unrelated' } },
      },
    ];
    const edges: Edge[] = [
      { id: 'agent-agg', source: 'agent-a', target: 'agg' },
      { id: 'agg-final', source: 'agg', target: 'final' },
    ];

    useWorkflowStore.setState({ nodes, edges, history: [{ nodes, edges }], historyIndex: 0 });
    useWorkflowStore.getState().expandAggregationNode('agg', compilePreview());

    const state = useWorkflowStore.getState();
    expect(
      state.nodes.find((node) => node.id === 'final')?.data.config.aggregationPreviewShifts?.agg?.y,
    ).toBeGreaterThan(0);
    expect(state.nodes.find((node) => node.id === 'unrelated')?.position).toEqual({ x: 820, y: 900 });
    expect(state.nodes.find((node) => node.id === 'unrelated')?.data.config.aggregationPreviewShifts).toBeUndefined();
  });

  it('keeps downstream parent nodes below the preview when dragging the expanded frame', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'agent-a',
        type: 'agent',
        position: { x: 160, y: 120 },
        data: { type: 'agent', label: 'Agent A', config: { alias: 'agent-a', model: 'openai/gpt-5-mini' } },
      },
      {
        id: 'agg',
        type: 'aggregation',
        position: { x: 160, y: 420 },
        data: {
          type: 'aggregation',
          label: 'Scoring',
          config: { alias: 'agg', aggregationWorkflowId: 'aggregation-scoring', aggregationMethod: 'scoring' },
        },
      },
      {
        id: 'final',
        type: 'result',
        position: { x: 160, y: 760 },
        data: { type: 'result', label: 'Final', config: { alias: 'final', name: 'final_answer' } },
      },
    ];
    const edges: Edge[] = [
      { id: 'agent-agg', source: 'agent-a', target: 'agg' },
      { id: 'agg-final', source: 'agg', target: 'final' },
    ];

    useWorkflowStore.setState({ nodes, edges, history: [{ nodes, edges }], historyIndex: 0 });
    useWorkflowStore.getState().expandAggregationNode('agg', compilePreview());

    const expanded = useWorkflowStore.getState();
    const frame = expanded.nodes.find((node) => node.id === 'agg--frame');
    const final = expanded.nodes.find((node) => node.id === 'final');
    const initialShift = final?.data.config.aggregationPreviewShifts?.agg?.y ?? 0;
    expect(frame).toBeDefined();
    expect(final).toBeDefined();
    expect(initialShift).toBeGreaterThan(0);

    useWorkflowStore.getState().onNodesChange([
      {
        id: 'agg--frame',
        type: 'position',
        position: {
          x: (frame?.position.x ?? 0) + 40,
          y: (frame?.position.y ?? 0) + 90,
        },
        dragging: false,
      },
    ]);

    let state = useWorkflowStore.getState();
    const shiftedFinal = state.nodes.find((node) => node.id === 'final');
    expect(shiftedFinal?.position.y).toBe((final?.position.y ?? 0) + 90);
    expect(shiftedFinal?.data.config.aggregationPreviewShifts?.agg?.y).toBe(initialShift + 90);

    useWorkflowStore.getState().collapseAggregationNode('agg');
    state = useWorkflowStore.getState();
    expect(state.nodes.find((node) => node.id === 'final')?.position).toEqual({ x: 160, y: 760 });
  });

  it('keeps downstream parent nodes below the preview when dragging an internal preview node', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'agent-a',
        type: 'agent',
        position: { x: 160, y: 120 },
        data: { type: 'agent', label: 'Agent A', config: { alias: 'agent-a', model: 'openai/gpt-5-mini' } },
      },
      {
        id: 'agg',
        type: 'aggregation',
        position: { x: 160, y: 420 },
        data: {
          type: 'aggregation',
          label: 'Scoring',
          config: { alias: 'agg', aggregationWorkflowId: 'aggregation-scoring', aggregationMethod: 'scoring' },
        },
      },
      {
        id: 'final',
        type: 'result',
        position: { x: 160, y: 760 },
        data: { type: 'result', label: 'Final', config: { alias: 'final', name: 'final_answer' } },
      },
    ];
    const edges: Edge[] = [
      { id: 'agent-agg', source: 'agent-a', target: 'agg' },
      { id: 'agg-final', source: 'agg', target: 'final' },
    ];

    useWorkflowStore.setState({ nodes, edges, history: [{ nodes, edges }], historyIndex: 0 });
    useWorkflowStore.getState().expandAggregationNode('agg', compilePreview());

    const expanded = useWorkflowStore.getState();
    const previewNode = expanded.nodes.find((node) => node.id === 'agg--result');
    const initialFinal = expanded.nodes.find((node) => node.id === 'final');
    expect(previewNode).toBeDefined();
    expect(initialFinal?.data.config.aggregationPreviewShifts?.agg?.y).toBeGreaterThan(0);

    useWorkflowStore.getState().onNodesChange([
      {
        id: 'agg--result',
        type: 'position',
        position: {
          x: previewNode?.position.x ?? 0,
          y: (previewNode?.position.y ?? 0) + 420,
        },
        dragging: false,
      },
    ]);

    const state = useWorkflowStore.getState();
    const frame = state.nodes.find((node) => node.id === 'agg--frame') as
      | (FlowNode & { style?: { width?: number; height?: number } })
      | undefined;
    const final = state.nodes.find((node) => node.id === 'final');
    const frameBottom = (frame?.position.y ?? 0) + Number(frame?.style?.height ?? 0);
    expect(final?.position.y).toBeGreaterThan(frameBottom);
    expect(final?.data.config.aggregationPreviewShifts?.agg?.y).toBeGreaterThan(
      initialFinal?.data.config.aggregationPreviewShifts?.agg?.y ?? 0,
    );
  });

  it('collapsing one aggregation keeps other expanded aggregation macro edges hidden', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'agent-a',
        type: 'agent',
        position: { x: 0, y: 0 },
        data: { type: 'agent', label: 'Agent A', config: { alias: 'agent-a', model: 'openai/gpt-5-mini' } },
      },
      {
        id: 'agg-a',
        type: 'aggregation',
        position: { x: 260, y: 0 },
        data: { type: 'aggregation', label: 'Scoring A', config: { alias: 'agg-a' } },
      },
      {
        id: 'result-a',
        type: 'result',
        position: { x: 520, y: 0 },
        data: { type: 'result', label: 'Result A', config: { alias: 'result-a' } },
      },
      {
        id: 'agent-b',
        type: 'agent',
        position: { x: 0, y: 220 },
        data: { type: 'agent', label: 'Agent B', config: { alias: 'agent-b', model: 'openai/gpt-5-mini' } },
      },
      {
        id: 'agg-b',
        type: 'aggregation',
        position: { x: 260, y: 220 },
        data: { type: 'aggregation', label: 'Scoring B', config: { alias: 'agg-b' } },
      },
      {
        id: 'result-b',
        type: 'result',
        position: { x: 520, y: 220 },
        data: { type: 'result', label: 'Result B', config: { alias: 'result-b' } },
      },
    ];
    const edges: Edge[] = [
      { id: 'agent-a-agg-a', source: 'agent-a', target: 'agg-a' },
      { id: 'agg-a-result-a', source: 'agg-a', target: 'result-a' },
      { id: 'agent-b-agg-b', source: 'agent-b', target: 'agg-b' },
      { id: 'agg-b-result-b', source: 'agg-b', target: 'result-b' },
    ];

    useWorkflowStore.setState({ nodes, edges });
    useWorkflowStore.getState().expandAggregationNode('agg-a', compilePreview('agg-a', 'agent-a', 'result-a'));
    useWorkflowStore.getState().expandAggregationNode('agg-b', compilePreview('agg-b', 'agent-b', 'result-b'));
    useWorkflowStore.getState().collapseAggregationNode('agg-a');

    const state = useWorkflowStore.getState();
    expect(state.edges.find((edge) => edge.id === 'agent-a-agg-a')).not.toHaveProperty('hidden');
    expect(state.edges.find((edge) => edge.id === 'agg-a-result-a')).not.toHaveProperty('hidden');
    expect(state.edges.find((edge) => edge.id === 'agent-b-agg-b')).toMatchObject({ hidden: true });
    expect(state.edges.find((edge) => edge.id === 'agg-b-result-b')).toMatchObject({ hidden: true });
  });

  it('does not hide aggregation macro edges when the compiled preview has no visible internals', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'agent-a',
        type: 'agent',
        position: { x: 0, y: 0 },
        data: { type: 'agent', label: 'Agent A', config: { alias: 'agent-a', model: 'openai/gpt-5-mini' } },
      },
      {
        id: 'agg',
        type: 'aggregation',
        position: { x: 260, y: 0 },
        data: { type: 'aggregation', label: 'Scoring', config: { alias: 'agg' } },
      },
      {
        id: 'result',
        type: 'result',
        position: { x: 520, y: 0 },
        data: { type: 'result', label: 'Result', config: { alias: 'result' } },
      },
    ];
    const edges: Edge[] = [
      { id: 'agent-agg', source: 'agent-a', target: 'agg' },
      { id: 'agg-result', source: 'agg', target: 'result' },
    ];

    useWorkflowStore.setState({ nodes, edges });
    useWorkflowStore.getState().expandAggregationNode('agg', emptyCompilePreview('agg'));

    const state = useWorkflowStore.getState();
    expect(state.nodes.some((node) => node.id.startsWith('agg--'))).toBe(false);
    expect(state.edges).toEqual(edges);
  });

  it('keeps chained aggregation macro edges hidden until all expanded owners collapse', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'agent-a',
        type: 'agent',
        position: { x: 0, y: 0 },
        data: { type: 'agent', label: 'Agent A', config: { alias: 'agent-a', model: 'openai/gpt-5-mini' } },
      },
      {
        id: 'agg-a',
        type: 'aggregation',
        position: { x: 260, y: 0 },
        data: { type: 'aggregation', label: 'Scoring A', config: { alias: 'agg-a' } },
      },
      {
        id: 'agg-b',
        type: 'aggregation',
        position: { x: 520, y: 0 },
        data: { type: 'aggregation', label: 'Scoring B', config: { alias: 'agg-b' } },
      },
      {
        id: 'result',
        type: 'result',
        position: { x: 780, y: 0 },
        data: { type: 'result', label: 'Result', config: { alias: 'result' } },
      },
    ];
    const edges: Edge[] = [
      { id: 'agent-agg-a', source: 'agent-a', target: 'agg-a' },
      { id: 'agg-a-agg-b', source: 'agg-a', target: 'agg-b' },
      { id: 'agg-b-result', source: 'agg-b', target: 'result' },
    ];

    useWorkflowStore.setState({ nodes, edges });
    useWorkflowStore.getState().expandAggregationNode('agg-a', compilePreview('agg-a', 'agent-a', 'agg-b'));
    useWorkflowStore.getState().expandAggregationNode('agg-b', compilePreview('agg-b', 'agg-a', 'result'));
    useWorkflowStore.getState().collapseAggregationNode('agg-b');

    let state = useWorkflowStore.getState();
    expect(state.edges.find((edge) => edge.id === 'agg-a-agg-b')).toMatchObject({ hidden: true });
    expect(state.edges.find((edge) => edge.id === 'agg-b-result')).not.toHaveProperty('hidden');

    useWorkflowStore.getState().collapseAggregationNode('agg-a');
    state = useWorkflowStore.getState();
    expect(state.edges.find((edge) => edge.id === 'agg-a-agg-b')).not.toHaveProperty('hidden');
  });

  it('allows React Flow dimensions changes for read-only expanded preview nodes', () => {
    const expandedNode: Node<NodeData> = {
      id: 'agg--score-agent-a',
      type: 'agent',
      position: { x: 0, y: 0 },
      data: {
        type: 'agent',
        label: 'Score Agent A',
        config: {
          aggregationInternalState: 'expanded',
          aggregationAnchorId: 'agg',
          sourceLocked: true,
        },
      },
    };

    useWorkflowStore.setState({ nodes: [expandedNode], edges: [] });
    useWorkflowStore
      .getState()
      .onNodesChange([
        { id: expandedNode.id, type: 'dimensions', dimensions: { width: 240, height: 180 }, resizing: false },
      ]);

    const updated = useWorkflowStore.getState().nodes.find((node) => node.id === expandedNode.id) as
      | (FlowNode & { measured?: { width?: number; height?: number } })
      | undefined;
    expect(updated?.measured).toEqual({ width: 240, height: 180 });
  });

  it('allows preview internals to be repositioned locally without history or persistence', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'agent-a',
        type: 'agent',
        position: { x: 0, y: 0 },
        data: { type: 'agent', label: 'Agent A', config: { alias: 'agent-a', model: 'openai/gpt-5-mini' } },
      },
      {
        id: 'agg',
        type: 'aggregation',
        position: { x: 260, y: 0 },
        data: {
          type: 'aggregation',
          label: 'Scoring',
          config: { alias: 'agg', aggregationWorkflowId: 'aggregation-scoring', aggregationMethod: 'scoring' },
        },
      },
      {
        id: 'final',
        type: 'result',
        position: { x: 520, y: 0 },
        data: { type: 'result', label: 'Final', config: { alias: 'final', name: 'final_answer' } },
      },
    ];
    const edges: Edge[] = [
      { id: 'agent-agg', source: 'agent-a', target: 'agg' },
      { id: 'agg-final', source: 'agg', target: 'final' },
    ];

    saveWorkflowToLocalStorage(nodes, edges);
    useWorkflowStore.setState({ nodes, edges, history: [{ nodes, edges }], historyIndex: 0 });
    useWorkflowStore.getState().expandAggregationNode('agg', compilePreview());

    const expandedBeforeMove = useWorkflowStore.getState();
    const historyBeforeMove = expandedBeforeMove.history;
    const previewNodeId = 'agg--score-agent-a';
    useWorkflowStore
      .getState()
      .onNodesChange([{ id: previewNodeId, type: 'position', position: { x: 100, y: -500 }, dragging: false }]);

    const state = useWorkflowStore.getState();
    const movedNode = state.nodes.find((node) => node.id === previewNodeId);
    const resultNode = state.nodes.find((node) => node.id === 'agg--result');
    const frame = state.nodes.find((node) => node.id === 'agg--frame') as
      | (FlowNode & { style?: { width?: number; height?: number } })
      | undefined;

    expect(movedNode?.position).toEqual({ x: 100, y: -500 });
    expect(state.history).toBe(historyBeforeMove);
    expect(state.historyIndex).toBe(0);
    const persisted = loadWorkflowFromLocalStorage();
    expect(persisted?.nodes).toEqual(nodes);
    expect(persisted?.edges).toEqual(edges);
    expect(frame).toBeDefined();
    expect(resultNode).toBeDefined();
    expect(frame?.position.x).toBeLessThan(movedNode?.position.x ?? 0);
    expect(frame?.position.y).toBeLessThan(movedNode?.position.y ?? 0);
    expect((frame?.position.x ?? 0) + Number(frame?.style?.width ?? 0)).toBeGreaterThan(
      (resultNode?.position.x ?? 0) + 500,
    );
    expect((frame?.position.y ?? 0) + Number(frame?.style?.height ?? 0)).toBeGreaterThan(
      (movedNode?.position.y ?? 0) + 170,
    );
  });

  it('persists real node movement while keeping preview movement ephemeral in a mixed drag batch', () => {
    const nodes = aggregationPreviewBaseNodes();
    const edges = aggregationPreviewBaseEdges();

    useWorkflowStore.setState({ nodes, edges, history: [{ nodes, edges }], historyIndex: 0 });
    useWorkflowStore.getState().expandAggregationNode('agg', compilePreview());
    useWorkflowStore.getState().onNodesChange([
      { id: 'agent-a', type: 'position', position: { x: 40, y: 0 }, dragging: false },
      { id: 'agg--score-agent-a', type: 'position', position: { x: 100, y: -500 }, dragging: false },
    ]);

    const state = useWorkflowStore.getState();
    saveWorkflowToLocalStorage(state.nodes, state.edges);
    const persisted = loadWorkflowFromLocalStorage();
    expect(state.nodes.find((node) => node.id === 'agent-a')?.position).toEqual({ x: 40, y: 0 });
    expect(state.nodes.find((node) => node.id === 'agg--score-agent-a')?.position).toEqual({ x: 100, y: -500 });
    expect(state.history).toHaveLength(2);
    expect(state.history[1].nodes.some((node) => node.id.startsWith('agg--'))).toBe(false);
    expect(state.history[1].nodes.find((node) => node.id === 'agent-a')?.position).toEqual({ x: 40, y: 0 });
    expect(persisted?.nodes.some((node) => node.id.startsWith('agg--'))).toBe(false);
    expect(persisted?.nodes.find((node) => node.id === 'agent-a')?.position).toEqual({ x: 40, y: 0 });
  });

  it('recomputes preview frames idempotently without including the frame in its own bounds', () => {
    const nodes = aggregationPreviewBaseNodes();
    const edges = aggregationPreviewBaseEdges();

    useWorkflowStore.setState({ nodes, edges });
    useWorkflowStore.getState().expandAggregationNode('agg', compilePreview());
    useWorkflowStore
      .getState()
      .onNodesChange([{ id: 'agg--score-agent-a', type: 'position', position: { x: 100, y: -500 }, dragging: false }]);

    expect(useWorkflowStore.getState().nodes.find((node) => node.id === 'agg--score-agent-a')?.position).toEqual({
      x: 100,
      y: -500,
    });
    const frameAfterFirstMove = useWorkflowStore.getState().nodes.find((node) => node.id === 'agg--frame') as
      | (FlowNode & { style?: { width?: number; height?: number } })
      | undefined;
    useWorkflowStore
      .getState()
      .onNodesChange([{ id: 'agg--score-agent-a', type: 'position', position: { x: 100, y: -500 }, dragging: false }]);
    const frameAfterSecondMove = useWorkflowStore.getState().nodes.find((node) => node.id === 'agg--frame') as
      | (FlowNode & { style?: { width?: number; height?: number } })
      | undefined;

    expect(frameAfterSecondMove?.position).toEqual(frameAfterFirstMove?.position);
    expect(frameAfterSecondMove?.style).toEqual(frameAfterFirstMove?.style);
  });

  it('moves preview internals with the draggable preview frame without history or persistence', () => {
    const nodes = aggregationPreviewBaseNodes();
    const edges = aggregationPreviewBaseEdges();

    saveWorkflowToLocalStorage(nodes, edges);
    useWorkflowStore.setState({ nodes, edges, history: [{ nodes, edges }], historyIndex: 0 });
    useWorkflowStore.getState().expandAggregationNode('agg', compilePreview());

    const expandedBeforeMove = useWorkflowStore.getState();
    const historyBeforeMove = expandedBeforeMove.history;
    const frameBefore = expandedBeforeMove.nodes.find((node) => node.id === 'agg--frame');
    const previewBefore = expandedBeforeMove.nodes.find((node) => node.id === 'agg--score-agent-a');
    expect(frameBefore).toBeDefined();
    expect(previewBefore).toBeDefined();

    useWorkflowStore.getState().onNodesChange([
      {
        id: 'agg--frame',
        type: 'position',
        position: {
          x: (frameBefore?.position.x ?? 0) + 120,
          y: (frameBefore?.position.y ?? 0) + 80,
        },
        dragging: false,
      },
    ]);

    const state = useWorkflowStore.getState();
    const frameAfter = state.nodes.find((node) => node.id === 'agg--frame');
    const previewAfter = state.nodes.find((node) => node.id === 'agg--score-agent-a');
    const persisted = loadWorkflowFromLocalStorage();

    expect(frameAfter?.position).toEqual({
      x: (frameBefore?.position.x ?? 0) + 120,
      y: (frameBefore?.position.y ?? 0) + 80,
    });
    expect(previewAfter?.position).toEqual({
      x: (previewBefore?.position.x ?? 0) + 120,
      y: (previewBefore?.position.y ?? 0) + 80,
    });
    expect(state.history).toBe(historyBeforeMove);
    expect(persisted?.nodes).toEqual(nodes);
    expect(persisted?.edges).toEqual(edges);
  });

  it('resizes the preview frame around measured node dimensions', () => {
    const nodes = aggregationPreviewBaseNodes();
    const edges = aggregationPreviewBaseEdges();

    useWorkflowStore.setState({ nodes, edges });
    useWorkflowStore.getState().expandAggregationNode('agg', compilePreview());
    useWorkflowStore.getState().onNodesChange([
      {
        id: 'agg--score-agent-a',
        type: 'dimensions',
        dimensions: { width: 900, height: 320 },
        resizing: false,
      },
    ]);

    const measured = useWorkflowStore.getState().nodes.find((node) => node.id === 'agg--score-agent-a') as
      | (FlowNode & { measured?: { width?: number; height?: number } })
      | undefined;
    const frame = useWorkflowStore.getState().nodes.find((node) => node.id === 'agg--frame') as
      | (FlowNode & { style?: { width?: number; height?: number } })
      | undefined;
    expect(measured?.measured).toEqual({ width: 900, height: 320 });
    expect((frame?.position.x ?? 0) + Number(frame?.style?.width ?? 0)).toBeGreaterThanOrEqual(
      (measured?.position.x ?? 0) + 900,
    );
    expect((frame?.position.y ?? 0) + Number(frame?.style?.height ?? 0)).toBeGreaterThanOrEqual(
      (measured?.position.y ?? 0) + 320,
    );
  });

  it('only reframes the aggregation group that owns the moved preview node', () => {
    const nodes: Node<NodeData>[] = [
      {
        id: 'agent-a',
        type: 'agent',
        position: { x: 0, y: 0 },
        data: { type: 'agent', label: 'Agent A', config: { alias: 'agent-a', model: 'openai/gpt-5-mini' } },
      },
      {
        id: 'agg-a',
        type: 'aggregation',
        position: { x: 260, y: -180 },
        data: { type: 'aggregation', label: 'Scoring A', config: { alias: 'agg-a' } },
      },
      {
        id: 'agg-b',
        type: 'aggregation',
        position: { x: 520, y: 180 },
        data: { type: 'aggregation', label: 'Scoring B', config: { alias: 'agg-b' } },
      },
      {
        id: 'final',
        type: 'result',
        position: { x: 780, y: 180 },
        data: { type: 'result', label: 'Final', config: { alias: 'final', name: 'final_answer' } },
      },
    ];
    const edges: Edge[] = [
      { id: 'agent-agg-a', source: 'agent-a', target: 'agg-a' },
      { id: 'agg-a-agg-b', source: 'agg-a', target: 'agg-b' },
      { id: 'agg-b-final', source: 'agg-b', target: 'final' },
    ];

    useWorkflowStore.setState({ nodes, edges });
    useWorkflowStore.getState().expandAggregationNode('agg-a', compilePreview('agg-a', 'agent-a', 'agg-b'));
    useWorkflowStore.getState().expandAggregationNode('agg-b', compilePreview('agg-b', 'agg-a', 'final'));
    const frameBBefore = useWorkflowStore.getState().nodes.find((node) => node.id === 'agg-b--frame') as
      | (FlowNode & { style?: { width?: number; height?: number } })
      | undefined;

    useWorkflowStore
      .getState()
      .onNodesChange([{ id: 'agg-a--score-agent-a', type: 'position', position: { x: 20, y: -700 }, dragging: false }]);

    const state = useWorkflowStore.getState();
    const movedNode = state.nodes.find((node) => node.id === 'agg-a--score-agent-a');
    const frameAAfter = state.nodes.find((node) => node.id === 'agg-a--frame') as
      | (FlowNode & { style?: { width?: number; height?: number } })
      | undefined;
    const frameBAfter = state.nodes.find((node) => node.id === 'agg-b--frame') as
      | (FlowNode & { style?: { width?: number; height?: number } })
      | undefined;
    expect(movedNode?.position).toEqual({ x: 20, y: -700 });
    expect(frameAAfter?.position.x).toBeLessThan(movedNode?.position.x ?? 0);
    expect(frameAAfter?.position.y).toBeLessThan(movedNode?.position.y ?? 0);
    const movedWidth = Number((movedNode as FlowNode & { style?: { width?: number } })?.style?.width ?? 280);
    const movedHeight = Number((movedNode as FlowNode & { style?: { height?: number } })?.style?.height ?? 128);
    expect((frameAAfter?.position.x ?? 0) + Number(frameAAfter?.style?.width ?? 0)).toBeGreaterThan(
      (movedNode?.position.x ?? 0) + movedWidth,
    );
    expect((frameAAfter?.position.y ?? 0) + Number(frameAAfter?.style?.height ?? 0)).toBeGreaterThan(
      (movedNode?.position.y ?? 0) + movedHeight,
    );
    expect(frameBAfter?.position).toEqual(frameBBefore?.position);
    expect(frameBAfter?.style).toEqual(frameBBefore?.style);
  });
});

function aggregationPreviewBaseNodes(): Node<NodeData>[] {
  return [
    {
      id: 'agent-a',
      type: 'agent',
      position: { x: 0, y: 0 },
      data: { type: 'agent', label: 'Agent A', config: { alias: 'agent-a', model: 'openai/gpt-5-mini' } },
    },
    {
      id: 'agg',
      type: 'aggregation',
      position: { x: 260, y: 0 },
      data: {
        type: 'aggregation',
        label: 'Scoring',
        config: { alias: 'agg', aggregationWorkflowId: 'aggregation-scoring', aggregationMethod: 'scoring' },
      },
    },
    {
      id: 'final',
      type: 'result',
      position: { x: 520, y: 0 },
      data: { type: 'result', label: 'Final', config: { alias: 'final', name: 'final_answer' } },
    },
  ];
}

function aggregationPreviewBaseEdges(): Edge[] {
  return [
    { id: 'agent-agg', source: 'agent-a', target: 'agg' },
    { id: 'agg-final', source: 'agg', target: 'final' },
  ];
}

function compilePreview(anchorID = 'agg', inputID = 'agent-a', presentationResultID = 'final'): CompilePreviewResponse {
  return {
    workflow_id: 'preview-test',
    aggregation_groups: [
      {
        anchor_node_id: anchorID,
        method: 'scoring',
        source_workflow_id: 'aggregation-scoring',
        terminal_node_id: `${anchorID}--result`,
        presentation_result_id: presentationResultID,
        input_node_ids: [inputID],
        node_ids: [`${anchorID}--score-${inputID}`, `${anchorID}--result`],
        llm_job_count: 1,
        top_level_llm_job_count: 1,
        conditional_llm_job_count: 0,
        conditional_llm_jobs: [],
        operation_count: 1,
      },
    ],
    nodes: [
      {
        id: `${anchorID}--score-${inputID}`,
        type: 'prompt',
        model: 'openai/gpt-5-mini',
        metadata: { label: `Score ${inputID}`, source_node_id: `score-${inputID}` },
      },
      {
        id: `${anchorID}--result`,
        type: 'result',
        metadata: { label: 'Result', source_node_id: 'result' },
      },
    ],
    edges: [
      { id: `edge-${inputID}-score`, source: inputID, target: `${anchorID}--score-${inputID}` },
      { id: 'edge-score-result', source: `${anchorID}--score-${inputID}`, target: `${anchorID}--result` },
    ],
  };
}

function emptyCompilePreview(anchorID: string): CompilePreviewResponse {
  return {
    workflow_id: 'preview-test',
    aggregation_groups: [
      {
        anchor_node_id: anchorID,
        method: 'scoring',
        source_workflow_id: 'aggregation-scoring',
        terminal_node_id: `${anchorID}--result`,
        presentation_result_id: undefined,
        input_node_ids: [],
        node_ids: [],
        llm_job_count: 0,
        top_level_llm_job_count: 0,
        conditional_llm_job_count: 0,
        conditional_llm_jobs: [],
        operation_count: 0,
      },
    ],
    nodes: [],
    edges: [],
  };
}
