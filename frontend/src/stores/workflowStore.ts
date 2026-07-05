import {
  addEdge,
  applyEdgeChanges,
  applyNodeChanges,
  type Connection,
  type EdgeChange,
  type NodeChange,
} from '@xyflow/react';
import { create } from 'zustand';
import {
  type CompilePreviewResponse,
  DEFAULT_NOVOMO_SANDBOX,
  type FlowEdge,
  type FlowNode,
  type NodeConfig,
  type NodeType,
} from '../types/workflow';
import {
  filterEphemeralAggregationGraph,
  isEphemeralAggregationInternalNode,
  isExpandedAggregationFrameNode,
  isExpandedAggregationPreviewNode,
  isMovableExpandedAggregationPreviewNode,
  previewHiddenOwnerIDs,
  restorePreviewHiddenEdge,
} from '../utils/aggregationPlan';
import {
  buildCompiledAggregationPreviewGroup,
  buildForkedCompiledAggregationGroup,
  reframeCompiledAggregationPreviewGroups,
} from '../utils/compiledPreviewToFlow';
import { loadWorkflowFromLocalStorage, saveWorkflowToLocalStorage } from '../utils/localStorage';
import { applyAutoNovomoHandoff } from '../utils/novomoHandoff';
import { isValidConnection, type ValidationError, validateWorkflow } from '../utils/workflowValidation';

interface HistoryState {
  nodes: FlowNode[];
  edges: FlowEdge[];
}

const getReplayBaseNodeId = (nodeId: string): string => {
  if (nodeId.includes('__')) {
    return nodeId.split('__')[0] || nodeId;
  }
  if (nodeId.includes('--')) {
    return nodeId.split('--')[0] || nodeId;
  }
  return nodeId;
};

// Validation pattern for alias: starts with letter, allows letters, numbers, hyphens, underscores
const ALIAS_PATTERN = /^[a-zA-Z][a-zA-Z0-9_-]*$/;

// Helper to generate a default alias based on node type
const generateDefaultAlias = (type: NodeType, existingNodes: FlowNode[]): string => {
  const prefixMap: Partial<Record<NodeType, string>> = {
    conditional: 'cond',
    child_workflow: 'child',
    workflow_ref: 'workflow',
    agent: 'llm',
    agent_run: 'agent-run',
    novo_run: 'superagent',
    aggregation: 'agg',
    operation: 'op',
  };
  const prefix = prefixMap[type] || type;
  const existingCount = existingNodes.filter((n) => n.data.type === type).length;
  return `${prefix}-${existingCount + 1}`;
};

// Check if an alias is valid format.
// "__" is reserved for workflow context keys; "--" is reserved for compiled aggregation internals.
const isValidAlias = (alias: string): boolean => {
  return (
    ALIAS_PATTERN.test(alias) &&
    alias.length >= 1 &&
    alias.length <= 50 &&
    !alias.includes('__') &&
    !alias.includes('--')
  );
};

// Check if an alias is unique among nodes
const isAliasUnique = (alias: string, nodes: FlowNode[], excludeNodeId?: string): boolean => {
  return !nodes.some((n) => {
    if (excludeNodeId && n.id === excludeNodeId) return false;
    const nodeAlias = n.data.config.alias || n.id;
    return nodeAlias === alias;
  });
};

const isAllowedExpandedNodeChange = (node: FlowNode, change: NodeChange): boolean =>
  change.type === 'select' ||
  change.type === 'dimensions' ||
  (change.type === 'position' &&
    (isMovableExpandedAggregationPreviewNode(node) || isExpandedAggregationFrameNode(node)));

const expandedFrameMoveDeltas = (nodes: FlowNode[], changes: NodeChange[]): Map<string, { x: number; y: number }> => {
  const deltas = new Map<string, { x: number; y: number }>();
  for (const change of changes) {
    if (change.type !== 'position' || !('id' in change) || !change.position) {
      continue;
    }
    const node = nodes.find((candidate) => candidate.id === change.id);
    const anchorID = node?.data.config.aggregationAnchorId;
    if (!node || !anchorID || !isExpandedAggregationFrameNode(node)) {
      continue;
    }
    deltas.set(anchorID, {
      x: change.position.x - node.position.x,
      y: change.position.y - node.position.y,
    });
  }
  return deltas;
};

const translateExpandedPreviewFrameInternals = (
  nodes: FlowNode[],
  deltas: Map<string, { x: number; y: number }>,
): FlowNode[] => {
  if (deltas.size === 0) {
    return nodes;
  }

  return nodes.map((node) => {
    const anchorID = node.data.config.aggregationAnchorId;
    const delta = anchorID ? deltas.get(anchorID) : undefined;
    if (!delta || !isMovableExpandedAggregationPreviewNode(node)) {
      return node;
    }
    return {
      ...node,
      position: {
        x: node.position.x + delta.x,
        y: node.position.y + delta.y,
      },
    };
  });
};

const applyShiftDeltaToConfig = (node: FlowNode, anchorID: string, deltaY: number): FlowNode => {
  const shifts = node.data.config.aggregationPreviewShifts;
  const currentShift = shifts?.[anchorID];
  if (!currentShift || deltaY === 0) {
    return node;
  }

  const appliedY = Math.max(-currentShift.y, deltaY);
  if (appliedY === 0) {
    return node;
  }

  const nextShift = {
    x: currentShift.x,
    y: currentShift.y + appliedY,
  };
  const { [anchorID]: _removedShift, ...remainingShifts } = shifts;
  const { aggregationPreviewShifts: _aggregationPreviewShifts, ...config } = node.data.config;
  const nextShifts = nextShift.y === 0 ? remainingShifts : { ...remainingShifts, [anchorID]: nextShift };

  return {
    ...node,
    position: {
      x: node.position.x,
      y: node.position.y + appliedY,
    },
    data: {
      ...node.data,
      config: {
        ...config,
        ...(Object.keys(nextShifts).length > 0 ? { aggregationPreviewShifts: nextShifts } : {}),
      },
    },
  };
};

const translateExpandedPreviewShiftedParents = (
  nodes: FlowNode[],
  deltas: Map<string, { x: number; y: number }>,
): FlowNode[] => {
  if (deltas.size === 0) {
    return nodes;
  }

  return nodes.map((node) => {
    let nextNode = node;
    for (const [anchorID, delta] of deltas.entries()) {
      nextNode = applyShiftDeltaToConfig(nextNode, anchorID, delta.y);
    }
    return nextNode;
  });
};

const isAuthoredWorkflowChange = (nodes: FlowNode[], change: NodeChange): boolean => {
  if (change.type !== 'position' && change.type !== 'remove') {
    return false;
  }
  if (!('id' in change)) {
    return true;
  }
  const node = nodes.find((candidate) => candidate.id === change.id);
  return !node || !isEphemeralAggregationInternalNode(node);
};

const ownsExpandedAggregationPreview = (nodes: FlowNode[], nodeId: string): boolean =>
  nodes.some(
    (node) =>
      isEphemeralAggregationInternalNode(node) &&
      (node.data.config.aggregationAnchorId === nodeId || node.id.startsWith(`${nodeId}--`)),
  );

const isAllowedExpandedAnchorChange = (change: NodeChange): boolean =>
  change.type === 'select' || change.type === 'dimensions';

const isExpandedPreviewEndpoint = (nodes: FlowNode[], nodeId: string): boolean => {
  const node = nodes.find((candidate) => candidate.id === nodeId);
  return Boolean(node && (isExpandedAggregationPreviewNode(node) || isExpandedAggregationFrameNode(node)));
};

const isEphemeralAggregationPreviewEdge = (edge: FlowEdge, nodes: FlowNode[]): boolean => {
  if (edge.data?.aggregationInternalState === 'expanded' || edge.data?.aggregationPreviewHidden === true) {
    return true;
  }
  return isExpandedPreviewEndpoint(nodes, edge.source) || isExpandedPreviewEndpoint(nodes, edge.target);
};

const filteredPreviewEdgeRemovals = (
  nodes: FlowNode[],
  edges: FlowEdge[],
  changes: EdgeChange[],
): { changes: EdgeChange[]; blockedRemoveIDs: Set<string> } => {
  const edgeByID = new Map(edges.map((edge) => [edge.id, edge]));
  const blockedRemoveIDs = new Set<string>();
  const filteredChanges = changes.filter((change) => {
    if (change.type !== 'remove' || !('id' in change)) {
      return true;
    }
    const edge = edgeByID.get(change.id);
    if (!edge || !isEphemeralAggregationPreviewEdge(edge, nodes)) {
      return true;
    }
    blockedRemoveIDs.add(change.id);
    return false;
  });
  return { changes: filteredChanges, blockedRemoveIDs };
};

const shouldReframeExpandedPreviews = (nodes: FlowNode[], changes: NodeChange[]): boolean =>
  changes.some((change) => {
    if (change.type !== 'position' && change.type !== 'dimensions') {
      return false;
    }
    if (!('id' in change)) {
      return false;
    }
    const node = nodes.find((candidate) => candidate.id === change.id);
    return Boolean(node && isMovableExpandedAggregationPreviewNode(node));
  });

const previewAnchorsToRecheck = (nodes: FlowNode[], changes: NodeChange[]): string[] => {
  const anchorIDs = new Set<string>();
  for (const change of changes) {
    if (change.type !== 'position' && change.type !== 'dimensions') {
      continue;
    }
    if (!('id' in change)) {
      continue;
    }
    const node = nodes.find((candidate) => candidate.id === change.id);
    const anchorID = node?.data.config.aggregationAnchorId;
    if (anchorID && node && isMovableExpandedAggregationPreviewNode(node)) {
      anchorIDs.add(anchorID);
    }
  }
  return [...anchorIDs];
};

const removeExpandedAggregationGroup = (
  nodes: FlowNode[],
  edges: FlowEdge[],
  anchorID: string,
): { nodes: FlowNode[]; edges: FlowEdge[] } => {
  const unshiftedNodes = removeExpandedPreviewParentShift(nodes, anchorID);
  const expandedIDs = new Set(
    unshiftedNodes
      .filter(isEphemeralAggregationInternalNode)
      .filter((node) => node.data.config.aggregationAnchorId === anchorID)
      .map((node) => node.id),
  );
  if (expandedIDs.size === 0) {
    return { nodes: unshiftedNodes, edges };
  }
  return {
    nodes: unshiftedNodes.filter((node) => !expandedIDs.has(node.id)),
    edges: edges
      .filter((edge) => !expandedIDs.has(edge.source) && !expandedIDs.has(edge.target))
      .map((edge) => restorePreviewHiddenEdge(edge, anchorID) as FlowEdge),
  };
};

const hideAggregationMacroEdges = (edges: FlowEdge[], anchorID: string): FlowEdge[] =>
  edges.map((edge) => {
    if (edge.source !== anchorID && edge.target !== anchorID) {
      return edge;
    }
    const ownerIDs = previewHiddenOwnerIDs(edge.data?.aggregationPreviewHiddenBy);
    const nextOwnerIDs = ownerIDs.includes(anchorID) ? ownerIDs : [...ownerIDs, anchorID];
    return {
      ...edge,
      hidden: true,
      data: {
        ...(edge.data ?? {}),
        aggregationPreviewHidden: true,
        aggregationPreviewHiddenBy: nextOwnerIDs,
      },
    };
  });

const EXPANDED_PREVIEW_PARENT_GAP = 96;

const numericStyleDimension = (value: unknown): number | undefined => {
  if (typeof value === 'number' && Number.isFinite(value) && value > 0) {
    return value;
  }
  if (typeof value === 'string') {
    const parsed = Number.parseFloat(value);
    return Number.isFinite(parsed) && parsed > 0 ? parsed : undefined;
  }
  return undefined;
};

const removeExpandedPreviewParentShift = (nodes: FlowNode[], anchorID: string): FlowNode[] =>
  nodes.map((node) => {
    const shifts = node.data.config.aggregationPreviewShifts;
    const shift = shifts?.[anchorID];
    if (!shift) {
      return node;
    }
    const { [anchorID]: _removedShift, ...remainingShifts } = shifts;
    const { aggregationPreviewShifts: _aggregationPreviewShifts, ...config } = node.data.config;
    return {
      ...node,
      position: {
        x: node.position.x - shift.x,
        y: node.position.y - shift.y,
      },
      data: {
        ...node.data,
        config: {
          ...config,
          ...(Object.keys(remainingShifts).length > 0 ? { aggregationPreviewShifts: remainingShifts } : {}),
        },
      },
    };
  });

const downstreamPreviewShiftTargets = (nodes: FlowNode[], edges: FlowEdge[], anchor: FlowNode): Set<string> => {
  const parentIDs = new Set(nodes.filter((node) => !isEphemeralAggregationInternalNode(node)).map((node) => node.id));
  const downstreamBySource = new Map<string, string[]>();
  for (const edge of edges) {
    if (!parentIDs.has(edge.source) || !parentIDs.has(edge.target) || edge.source === edge.target) {
      continue;
    }
    downstreamBySource.set(edge.source, [...(downstreamBySource.get(edge.source) || []), edge.target]);
  }

  const targets = new Set<string>();
  const queue = [...(downstreamBySource.get(anchor.id) || [])];
  while (queue.length > 0) {
    const nextID = queue.shift();
    if (!nextID || targets.has(nextID) || nextID === anchor.id) {
      continue;
    }
    targets.add(nextID);
    queue.push(...(downstreamBySource.get(nextID) || []));
  }
  return targets;
};

const applyExpandedPreviewParentShift = (
  nodes: FlowNode[],
  edges: FlowEdge[],
  anchorID: string,
  expandedNodes: FlowNode[],
): FlowNode[] => {
  const frame = expandedNodes.find(isExpandedAggregationFrameNode);
  const anchor = nodes.find((node) => node.id === anchorID);
  const frameHeight = numericStyleDimension(frame?.style?.height);
  if (!frame || !anchor || !frameHeight) {
    return nodes;
  }

  const targetIDs = downstreamPreviewShiftTargets(nodes, edges, anchor);
  if (targetIDs.size === 0) {
    return nodes;
  }

  const targetMinY = Math.min(...nodes.filter((node) => targetIDs.has(node.id)).map((node) => node.position.y));
  const frameBottom = frame.position.y + frameHeight;
  const deltaY = Math.ceil(frameBottom + EXPANDED_PREVIEW_PARENT_GAP - targetMinY);
  if (deltaY <= 0) {
    return nodes;
  }

  return nodes.map((node) => {
    if (!targetIDs.has(node.id)) {
      return node;
    }
    const currentShifts = node.data.config.aggregationPreviewShifts || {};
    const currentShift = currentShifts[anchorID] || { x: 0, y: 0 };
    return {
      ...node,
      position: {
        x: node.position.x,
        y: node.position.y + deltaY,
      },
      data: {
        ...node.data,
        config: {
          ...node.data.config,
          aggregationPreviewShifts: {
            ...currentShifts,
            [anchorID]: {
              x: currentShift.x,
              y: currentShift.y + deltaY,
            },
          },
        },
      },
    };
  });
};

const defaultConfigForNode = (type: NodeType, alias: string): NodeConfig => {
  if (type === 'agent_run') {
    return {
      alias,
      harness: 'claude-code',
      sandbox: DEFAULT_NOVOMO_SANDBOX,
      inheritFromMode: 'auto',
      timeoutSeconds: 600,
      retryMaxAttempts: 3,
      retryBackoffMs: 1000,
      retryBackoffMultiplier: 2.0,
      retryMaxBackoffMs: 30000,
    };
  }
  if (type === 'novo_run') {
    return {
      alias,
      identity: 'sde-novo',
      sandbox: DEFAULT_NOVOMO_SANDBOX,
      inheritFromMode: 'auto',
      timeoutSeconds: 600,
      graceSeconds: 10,
      retryMaxAttempts: 1,
      retryBackoffMs: 1000,
      retryBackoffMultiplier: 2.0,
      retryMaxBackoffMs: 30000,
    };
  }
  if (type === 'aggregation') {
    return {
      alias,
      aggregationMethod: 'collect',
      aggregationConfig: { separator: '\n---\n' },
    };
  }
  if (type === 'workflow_ref') {
    return {
      alias,
      inputTemplate: {},
    };
  }
  if (type === 'operation') {
    return {
      alias,
      operationType: 'collect_inputs',
      operationConfig: { input_ids: [] },
    };
  }
  return { alias };
};

const displayLabelForNodeType = (type: NodeType): string => {
  if (type === 'novo_run') return 'Superagent';
  if (type === 'agent_run') return 'Novomo Agent';
  if (type === 'workflow_ref') return 'Use Workflow';
  return type
    .split('_')
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(' ');
};

export interface NodeExecutionState {
  status: 'idle' | 'running' | 'completed' | 'error' | 'retrying';
  output?: string;
  error?: string;
  startTime?: string;
  endTime?: string;
  retryAttempt?: number;
  retryMaxAttempts?: number;
  retryBackoffMs?: number;
  metrics?: {
    tokens_input?: number;
    tokens_output?: number;
    cost?: number;
    latency_ms?: number;
  };
}

// Job node from API response
export interface ReplayNodeResult {
  node_id: string;
  node_label?: string;
  node_name?: string;
  node_type: string;
  node_order?: number;
  status: string;
  prompt?: string;
  output?: string;
  error_message?: string;
  tokens_input: number;
  tokens_output: number;
  cost: number;
  latency_ms: number;
  model?: string;
  execution_uid?: string;
  attempt_number?: number;
  started_at?: string | null;
  completed_at?: string | null;
  metadata?: string;
  parent_node_id?: string;
}

export interface ReplayJobInfo {
  id: string;
  status: string;
  description?: string;
  created_at: string;
  updated_at?: string;
  tokens_input?: number;
  tokens_output?: number;
  tokens_total?: number;
  cost?: number;
  result_text?: string;
}

interface WorkflowState {
  nodes: FlowNode[];
  edges: FlowEdge[];
  selectedNodeId: string | null;
  selectedEdgeId: string | null;
  validationErrors: ValidationError[];
  viewportCenter: { x: number; y: number } | null;
  history: HistoryState[];
  historyIndex: number;
  nodeExecutionStates: Record<string, NodeExecutionState>;

  // Replay mode
  replayJobId: string | null;
  replayTimestamp: string | null;
  replayJob: ReplayJobInfo | null;
  replayNodes: ReplayNodeResult[];
  replayNodeResults: Record<string, ReplayNodeResult>;
  replaySelectedNodeId: string | null;
  replayFocusNonce: number;

  // Actions
  onNodesChange: (changes: NodeChange[]) => void;
  onEdgesChange: (changes: EdgeChange[]) => void;
  onConnect: (connection: Connection) => void;
  addNode: (type: NodeType, position?: { x: number; y: number }) => void;
  updateNodeConfig: (nodeId: string, config: NodeConfig) => void;
  setSelectedNodeId: (nodeId: string | null) => void;
  setSelectedEdgeId: (edgeId: string | null) => void;
  deleteNode: (nodeId: string) => void;
  deleteEdge: (edgeId: string) => void;
  clearWorkflow: () => void;
  loadWorkflow: (nodes: FlowNode[], edges: FlowEdge[]) => void;
  validateWorkflow: () => void;
  setViewportCenter: (center: { x: number; y: number }) => void;
  undo: () => void;
  redo: () => void;
  updateNodeExecutionState: (nodeId: string, state: NodeExecutionState) => void;
  clearExecutionStates: () => void;
  getNodeExecutionState: (nodeId: string) => NodeExecutionState | undefined;
  updateNodeAlias: (nodeId: string, newAlias: string) => { success: boolean; error?: string };
  isAliasValid: (alias: string, excludeNodeId?: string) => { valid: boolean; error?: string };
  expandAggregationNode: (nodeId: string, preview: CompilePreviewResponse) => void;
  collapseAggregationNode: (nodeId: string) => void;
  forkAggregationNode: (nodeId: string, preview: CompilePreviewResponse) => void;

  // Replay mode
  enterReplayMode: (job: ReplayJobInfo, nodes: ReplayNodeResult[]) => void;
  exitReplayMode: () => void;
  isInReplayMode: () => boolean;
  setReplaySelectedNodeId: (nodeId: string | null) => void;
  requestReplayFocus: (nodeId: string) => void;
}

const cloneHistoryState = (nodes: FlowNode[], edges: FlowEdge[]): HistoryState => {
  const filtered = filterEphemeralAggregationGraph({ nodes, edges });
  return {
    nodes: JSON.parse(JSON.stringify(filtered.nodes)),
    edges: JSON.parse(JSON.stringify(filtered.edges)),
  };
};

// Push the post-change snapshot into history, trimming any redo branch.
const pushHistory = (state: WorkflowState, nextNodes: FlowNode[], nextEdges: FlowEdge[]) => {
  const nextHistory = state.history.slice(0, state.historyIndex + 1);
  nextHistory.push(cloneHistoryState(nextNodes, nextEdges));

  if (nextHistory.length > 50) {
    nextHistory.shift();
  }

  return {
    history: nextHistory,
    historyIndex: nextHistory.length - 1,
  };
};

const collapseExpandedPreviewsForEdit = (nodes: FlowNode[], edges: FlowEdge[]) => {
  const filtered = filterEphemeralAggregationGraph({ nodes, edges });
  return {
    nodes: filtered.nodes as FlowNode[],
    edges: filtered.edges as FlowEdge[],
  };
};

// Helper function to persist state to localStorage
const persistToLocalStorage = (nodes: FlowNode[], edges: FlowEdge[]) => {
  // Debounce localStorage saves to avoid excessive writes
  if (typeof window !== 'undefined') {
    saveWorkflowToLocalStorage(nodes, edges);
  }
};

// Load initial state from localStorage if available
const loadInitialState = () => {
  const saved = loadWorkflowFromLocalStorage();
  if (saved) {
    return {
      nodes: saved.nodes,
      edges: saved.edges,
    };
  }
  return {
    nodes: [],
    edges: [],
  };
};

export const useWorkflowStore = create<WorkflowState>((set, get) => {
  const initialState = loadInitialState();
  const initialHistoryState = cloneHistoryState(initialState.nodes, initialState.edges);

  return {
    nodes: initialState.nodes,
    edges: initialState.edges,
    selectedNodeId: null,
    selectedEdgeId: null,
    validationErrors: [],
    viewportCenter: null,
    history: [initialHistoryState],
    historyIndex: 0,
    nodeExecutionStates: {},
    replayJobId: null,
    replayTimestamp: null,
    replayJob: null,
    replayNodes: [],
    replayNodeResults: {},
    replaySelectedNodeId: null,
    replayFocusNonce: 0,

    onNodesChange: (changes: NodeChange[]) => {
      const currentState = get();
      const filteredChanges = changes.filter((change) => {
        if (!('id' in change)) return true;
        if (ownsExpandedAggregationPreview(currentState.nodes, change.id)) {
          return isAllowedExpandedAnchorChange(change);
        }
        const node = currentState.nodes.find((candidate) => candidate.id === change.id);
        if (!node || !isEphemeralAggregationInternalNode(node)) return true;
        return isAllowedExpandedNodeChange(node, change);
      });
      const frameMoveDeltas = expandedFrameMoveDeltas(currentState.nodes, filteredChanges);
      const changedNodesWithInternals = translateExpandedPreviewFrameInternals(
        applyNodeChanges(filteredChanges, currentState.nodes) as FlowNode[],
        frameMoveDeltas,
      );
      const changedNodes = translateExpandedPreviewShiftedParents(changedNodesWithInternals, frameMoveDeltas);
      const shouldReframe = shouldReframeExpandedPreviews(currentState.nodes, filteredChanges);
      let newNodes = shouldReframe ? reframeCompiledAggregationPreviewGroups(changedNodes) : changedNodes;
      if (shouldReframe) {
        for (const anchorID of previewAnchorsToRecheck(currentState.nodes, filteredChanges)) {
          const expandedNodes = newNodes.filter(
            (node) => isEphemeralAggregationInternalNode(node) && node.data.config.aggregationAnchorId === anchorID,
          );
          newNodes = applyExpandedPreviewParentShift(newNodes, currentState.edges, anchorID, expandedNodes);
        }
      }

      // Only save to history if it's a significant change (not just selection)
      const hasSignificantChange = filteredChanges.some((change) =>
        isAuthoredWorkflowChange(currentState.nodes, change),
      );

      if (hasSignificantChange) {
        let historyNodes = newNodes;
        let historyEdges = currentState.edges;
        if (filteredChanges.some((change) => change.type === 'remove')) {
          const remainingIDs = new Set(newNodes.map((node) => node.id));
          const edgesWithoutRemovedNodes = currentState.edges.filter(
            (edge) => remainingIDs.has(edge.source) && remainingIDs.has(edge.target),
          );
          const collapsed = collapseExpandedPreviewsForEdit(newNodes, edgesWithoutRemovedNodes);
          historyNodes = collapsed.nodes;
          historyEdges = collapsed.edges;
        }
        const historyUpdate = pushHistory(currentState, historyNodes, historyEdges);
        set({
          nodes: historyNodes,
          edges: historyEdges,
          ...historyUpdate,
        });
        persistToLocalStorage(historyNodes, historyEdges);
      } else {
        set({ nodes: newNodes });
      }
    },

    onEdgesChange: (changes: EdgeChange[]) => {
      const currentState = get();
      const { changes: filteredChanges, blockedRemoveIDs } = filteredPreviewEdgeRemovals(
        currentState.nodes,
        currentState.edges,
        changes,
      );
      let newEdges = applyEdgeChanges(filteredChanges, currentState.edges) as FlowEdge[];
      const isStructuralChange = filteredChanges.some((change) => change.type !== 'select');
      let newNodes = isStructuralChange
        ? (applyAutoNovomoHandoff(currentState.nodes, newEdges) as FlowNode[])
        : currentState.nodes;

      // Track edge selection for delete key support
      const selectedEdge = filteredChanges.find((change) => change.type === 'select' && change.selected);
      const deselectedEdge = filteredChanges.find((change) => change.type === 'select' && !change.selected);

      let selectedEdgeId = currentState.selectedEdgeId;
      if (selectedEdge && 'id' in selectedEdge) {
        selectedEdgeId = selectedEdge.id;
      } else if (deselectedEdge && 'id' in deselectedEdge && deselectedEdge.id === selectedEdgeId) {
        selectedEdgeId = null;
      }
      if (selectedEdgeId && blockedRemoveIDs.has(selectedEdgeId)) {
        selectedEdgeId = null;
      }

      // Save to history for edge changes (remove is significant)
      const hasSignificantChange = filteredChanges.some((change) => change.type === 'remove');

      if (hasSignificantChange) {
        const collapsed = collapseExpandedPreviewsForEdit(newNodes, newEdges);
        newNodes = collapsed.nodes;
        newEdges = collapsed.edges;
        const historyUpdate = pushHistory(currentState, newNodes, newEdges);
        set({
          nodes: newNodes,
          edges: newEdges,
          selectedEdgeId,
          ...historyUpdate,
        });
        persistToLocalStorage(newNodes, newEdges);
      } else {
        set({ nodes: newNodes, edges: newEdges, selectedEdgeId });
      }
    },

    onConnect: (connection: Connection) => {
      const currentState = get();
      const { nodes } = currentState;
      const sourceNode = nodes.find((n) => n.id === connection.source);
      const targetNode = nodes.find((n) => n.id === connection.target);

      if (
        (sourceNode && isEphemeralAggregationInternalNode(sourceNode)) ||
        (targetNode && isEphemeralAggregationInternalNode(targetNode)) ||
        (connection.source && ownsExpandedAggregationPreview(nodes, connection.source)) ||
        (connection.target && ownsExpandedAggregationPreview(nodes, connection.target))
      ) {
        alert(
          'Expanded aggregation previews are read-only. Collapse the preview or fork the aggregation before editing.',
        );
        return;
      }

      if (sourceNode && targetNode) {
        const validation = isValidConnection(sourceNode, targetNode, connection.sourceHandle);
        if (!validation.valid) {
          alert(validation.message || 'Invalid connection');
          return;
        }
      }

      const newEdges = addEdge(connection, get().edges) as FlowEdge[];
      const newNodes = applyAutoNovomoHandoff(nodes, newEdges) as FlowNode[];
      const historyUpdate = pushHistory(currentState, newNodes, newEdges);
      set({
        nodes: newNodes,
        edges: newEdges,
        ...historyUpdate,
      });

      persistToLocalStorage(newNodes, newEdges);

      // Validate after connecting
      setTimeout(() => get().validateWorkflow(), 100);
    },

    addNode: (type: NodeType, position?: { x: number; y: number }) => {
      const currentState = get();
      const selectedNode = currentState.nodes.find((node) => node.id === currentState.selectedNodeId);
      const forkContext =
        type === 'operation' && selectedNode?.data.config.aggregationInternalState === 'forked'
          ? selectedNode.data.config
          : undefined;

      if (type === 'operation' && !forkContext) {
        alert('Operation nodes can only be added while editing a forked aggregation workflow.');
        return;
      }

      // Generate a user-friendly alias as the node ID
      const alias = generateDefaultAlias(type, currentState.nodes);

      // Use provided position, or viewport center, or default
      const nodePosition = position || currentState.viewportCenter || { x: 250, y: 250 };

      const newNode: FlowNode = {
        id: alias, // Use alias as the actual node ID
        type,
        position: nodePosition,
        data: {
          type,
          label: displayLabelForNodeType(type),
          config: {
            ...defaultConfigForNode(type, alias),
            ...(forkContext
              ? {
                  aggregationInternalState: 'forked' as const,
                  aggregationAnchorId: forkContext.aggregationAnchorId,
                  sourceLocked: false,
                  forkedFromWorkflowId: forkContext.forkedFromWorkflowId,
                  forkedFromSourceVersion: forkContext.forkedFromSourceVersion,
                  forkedFromSourceHash: forkContext.forkedFromSourceHash,
                  boundaryType: forkContext.boundaryType,
                }
              : {}),
          },
        },
      };

      const newNodes = [...currentState.nodes, newNode];
      const historyUpdate = pushHistory(currentState, newNodes, currentState.edges);
      set({
        nodes: newNodes,
        ...historyUpdate,
      });

      persistToLocalStorage(newNodes, currentState.edges);
    },

    updateNodeConfig: (nodeId: string, config: NodeConfig) => {
      const currentState = get();
      const target = currentState.nodes.find((node) => node.id === nodeId);
      if (
        target &&
        (isEphemeralAggregationInternalNode(target) || ownsExpandedAggregationPreview(currentState.nodes, nodeId))
      ) {
        return;
      }

      const editableState = collapseExpandedPreviewsForEdit(currentState.nodes, currentState.edges);
      const newNodes = editableState.nodes.map((node) =>
        node.id === nodeId ? { ...node, data: { ...node.data, config } } : node,
      );
      const historyUpdate = pushHistory(currentState, newNodes, editableState.edges);

      set({
        nodes: newNodes,
        edges: editableState.edges,
        ...historyUpdate,
      });

      persistToLocalStorage(newNodes, editableState.edges);
    },

    setSelectedNodeId: (nodeId: string | null) => {
      if (get().selectedNodeId === nodeId) {
        return;
      }
      set({ selectedNodeId: nodeId });
    },

    setSelectedEdgeId: (edgeId: string | null) => {
      if (get().selectedEdgeId === edgeId) {
        return;
      }
      set({ selectedEdgeId: edgeId });
    },

    deleteNode: (nodeId: string) => {
      const currentState = get();
      const target = currentState.nodes.find((node) => node.id === nodeId);
      if (
        target &&
        (isEphemeralAggregationInternalNode(target) || ownsExpandedAggregationPreview(currentState.nodes, nodeId))
      ) {
        return;
      }

      const editableState = collapseExpandedPreviewsForEdit(currentState.nodes, currentState.edges);
      const newNodes = editableState.nodes.filter((node) => node.id !== nodeId);
      const newEdges = editableState.edges.filter((edge) => edge.source !== nodeId && edge.target !== nodeId);
      const historyUpdate = pushHistory(currentState, newNodes, newEdges);

      set({
        nodes: newNodes,
        edges: newEdges,
        selectedNodeId: currentState.selectedNodeId === nodeId ? null : currentState.selectedNodeId,
        ...historyUpdate,
      });

      persistToLocalStorage(newNodes, newEdges);
    },

    deleteEdge: (edgeId: string) => {
      const currentState = get();
      const target = currentState.edges.find((edge) => edge.id === edgeId);
      if (target && isEphemeralAggregationPreviewEdge(target, currentState.nodes)) {
        set({
          selectedEdgeId: currentState.selectedEdgeId === edgeId ? null : currentState.selectedEdgeId,
        });
        return;
      }

      // Filter out the edge to delete
      const newEdges = currentState.edges.filter((edge) => edge.id !== edgeId);
      const historyUpdate = pushHistory(currentState, currentState.nodes, newEdges);

      set({
        edges: newEdges,
        selectedEdgeId: currentState.selectedEdgeId === edgeId ? null : currentState.selectedEdgeId,
        ...historyUpdate,
      });

      persistToLocalStorage(currentState.nodes, newEdges);
    },

    clearWorkflow: () => {
      const currentState = get();
      const historyUpdate = pushHistory(currentState, [], []);

      set({
        nodes: [],
        edges: [],
        selectedNodeId: null,
        ...historyUpdate,
      });

      persistToLocalStorage([], []);
    },

    expandAggregationNode: (nodeId: string, preview: CompilePreviewResponse) => {
      const currentState = get();
      const anchor = currentState.nodes.find((node) => node.id === nodeId && node.data.type === 'aggregation');
      if (!anchor) return;

      const withoutExisting = removeExpandedAggregationGroup(currentState.nodes, currentState.edges, nodeId);
      const expanded = buildCompiledAggregationPreviewGroup({
        anchorNodeId: nodeId,
        parentNodes: withoutExisting.nodes,
        parentEdges: withoutExisting.edges,
        preview,
      });
      const macroEdges =
        expanded.nodes.length > 0 ? hideAggregationMacroEdges(withoutExisting.edges, nodeId) : withoutExisting.edges;
      const parentNodes =
        expanded.nodes.length > 0
          ? applyExpandedPreviewParentShift(
              withoutExisting.nodes,
              withoutExisting.edges,
              nodeId,
              expanded.nodes as FlowNode[],
            )
          : withoutExisting.nodes;

      set({
        nodes: [...parentNodes, ...(expanded.nodes as FlowNode[])],
        edges: [...macroEdges, ...(expanded.edges as FlowEdge[])],
      });
    },

    collapseAggregationNode: (nodeId: string) => {
      const currentState = get();
      const collapsed = removeExpandedAggregationGroup(currentState.nodes, currentState.edges, nodeId);
      set(collapsed);
    },

    forkAggregationNode: (nodeId: string, preview: CompilePreviewResponse) => {
      const currentState = get();

      const withoutExpanded = removeExpandedAggregationGroup(currentState.nodes, currentState.edges, nodeId);
      const forked = buildForkedCompiledAggregationGroup({
        anchorNodeId: nodeId,
        parentEdges: withoutExpanded.edges,
        parentNodes: withoutExpanded.nodes,
        preview,
      });
      if (forked.nodes.length === 0) return;
      const newNodes = [...withoutExpanded.nodes.filter((node) => node.id !== nodeId), ...(forked.nodes as FlowNode[])];
      const newEdges = [
        ...withoutExpanded.edges.filter((edge) => edge.source !== nodeId && edge.target !== nodeId),
        ...(forked.edges as FlowEdge[]),
      ];
      const historyUpdate = pushHistory(currentState, newNodes, newEdges);

      set({
        nodes: newNodes,
        edges: newEdges,
        selectedNodeId: forked.nodes[0]?.id ?? null,
        ...historyUpdate,
      });
      persistToLocalStorage(newNodes, newEdges);
      setTimeout(() => get().validateWorkflow(), 100);
    },

    loadWorkflow: (nodes: FlowNode[], edges: FlowEdge[]) => {
      const filtered = filterEphemeralAggregationGraph({ nodes, edges });
      nodes = filtered.nodes as FlowNode[];
      edges = filtered.edges as FlowEdge[];

      // Normalize aliases on load: ensure config.alias matches node.id for readable IDs
      const normalizedNodes = nodes.map((node) => {
        const hasReadableId = !node.id.startsWith('temp-');
        const hasAlias = Boolean(node.data.config.alias);

        if (hasReadableId && !hasAlias) {
          // Node has a readable ID but no alias - set alias to match ID
          return {
            ...node,
            data: {
              ...node.data,
              config: { ...node.data.config, alias: node.id },
            },
          };
        }
        return node;
      });

      set({
        nodes: normalizedNodes,
        edges,
        selectedNodeId: null,
        history: [cloneHistoryState(normalizedNodes, edges)],
        historyIndex: 0,
      });
      persistToLocalStorage(normalizedNodes, edges);
      setTimeout(() => get().validateWorkflow(), 100);
    },

    validateWorkflow: () => {
      const { nodes, edges } = get();
      const filtered = filterEphemeralAggregationGraph({ nodes, edges });
      const result = validateWorkflow(filtered.nodes as FlowNode[], filtered.edges as FlowEdge[]);
      set({ validationErrors: result.errors });
    },

    setViewportCenter: (center: { x: number; y: number }) => {
      set({ viewportCenter: center });
    },

    undo: () => {
      const { history, historyIndex } = get();
      if (historyIndex > 0) {
        const prevState = history[historyIndex - 1];
        const newNodes = JSON.parse(JSON.stringify(prevState.nodes));
        const newEdges = JSON.parse(JSON.stringify(prevState.edges));
        set({
          nodes: newNodes,
          edges: newEdges,
          historyIndex: historyIndex - 1,
        });
        persistToLocalStorage(newNodes, newEdges);
      }
    },

    redo: () => {
      const { history, historyIndex } = get();
      if (historyIndex < history.length - 1) {
        const nextState = history[historyIndex + 1];
        const newNodes = JSON.parse(JSON.stringify(nextState.nodes));
        const newEdges = JSON.parse(JSON.stringify(nextState.edges));
        set({
          nodes: newNodes,
          edges: newEdges,
          historyIndex: historyIndex + 1,
        });
        persistToLocalStorage(newNodes, newEdges);
      }
    },

    updateNodeExecutionState: (nodeId: string, state: NodeExecutionState) => {
      const currentStates = get().nodeExecutionStates;
      const newStates = { ...currentStates, [nodeId]: state };
      set({ nodeExecutionStates: newStates });
    },

    clearExecutionStates: () => {
      set({ nodeExecutionStates: {} });
    },

    getNodeExecutionState: (nodeId: string) => {
      return get().nodeExecutionStates[nodeId];
    },

    isAliasValid: (alias: string, excludeNodeId?: string) => {
      const { nodes } = get();
      if (!alias || alias.trim() === '') {
        return { valid: false, error: 'Reference ID cannot be empty' };
      }
      if (!isValidAlias(alias)) {
        return {
          valid: false,
          error: 'Must start with a letter; use only letters, numbers, single hyphens, or underscores',
        };
      }
      if (!isAliasUnique(alias, nodes, excludeNodeId)) {
        return { valid: false, error: 'This Reference ID is already in use' };
      }
      return { valid: true };
    },

    updateNodeAlias: (nodeId: string, newAlias: string) => {
      const currentState = get();
      const { nodes, edges } = currentState;
      const target = nodes.find((node) => node.id === nodeId);
      if (
        target &&
        (isEphemeralAggregationInternalNode(target) || ownsExpandedAggregationPreview(currentState.nodes, nodeId))
      ) {
        return { success: false, error: 'Expanded aggregation internals are read-only' };
      }

      // Validate the new alias
      const validation = get().isAliasValid(newAlias, nodeId);
      if (!validation.valid) {
        return { success: false, error: validation.error };
      }

      // Update the node: change its ID and config.alias
      const editableState = collapseExpandedPreviewsForEdit(nodes, edges);
      const newNodes = editableState.nodes.map((node) => {
        if (node.id === nodeId) {
          return {
            ...node,
            id: newAlias,
            data: {
              ...node.data,
              config: { ...node.data.config, alias: newAlias },
            },
          };
        }
        return node;
      });

      // Update edges that reference the old node ID
      const newEdges = editableState.edges.map((edge) => {
        let updated = edge;
        if (edge.source === nodeId) {
          updated = { ...updated, source: newAlias };
        }
        if (edge.target === nodeId) {
          updated = { ...updated, target: newAlias };
        }
        return updated;
      });

      // Update selected node ID if it was the renamed node
      const newSelectedNodeId = currentState.selectedNodeId === nodeId ? newAlias : currentState.selectedNodeId;
      const historyUpdate = pushHistory(currentState, newNodes, newEdges);

      set({
        nodes: newNodes,
        edges: newEdges,
        selectedNodeId: newSelectedNodeId,
        ...historyUpdate,
      });

      persistToLocalStorage(newNodes, newEdges);
      return { success: true };
    },

    // Replay mode actions
    enterReplayMode: (job: ReplayJobInfo, nodeRuns: ReplayNodeResult[]) => {
      // Build node results map keyed by node_id
      const replayNodeResults: Record<string, ReplayNodeResult> = {};
      const nodeGroups = new Map<
        string,
        {
          primary?: ReplayNodeResult;
          children: ReplayNodeResult[];
        }
      >();
      for (const node of nodeRuns) {
        replayNodeResults[node.node_id] = node;
        // Use parent_node_id when available; fall back to string splitting for old data
        const baseId = node.parent_node_id || getReplayBaseNodeId(node.node_id);
        const isChild = !!node.parent_node_id || node.node_id !== baseId;
        const group = nodeGroups.get(baseId) || { children: [] };
        if (!isChild) {
          group.primary = node;
        } else {
          group.children.push(node);
        }
        nodeGroups.set(baseId, group);
      }

      const deriveStatus = (groupNodes: ReplayNodeResult[]): NodeExecutionState['status'] => {
        if (groupNodes.some((node) => node.status === 'failed' || node.status === 'cancelled')) {
          return 'error';
        }
        if (groupNodes.some((node) => node.status === 'running' || node.status === 'pending')) {
          return 'running';
        }
        if (groupNodes.some((node) => node.status === 'completed')) {
          return 'completed';
        }
        return 'idle';
      };

      // Build execution states from grouped nodes to display on canvas nodes
      const nodeExecutionStates: Record<string, NodeExecutionState> = {};
      for (const [baseId, group] of nodeGroups.entries()) {
        const groupNodes = group.primary ? [group.primary, ...group.children] : group.children;
        if (groupNodes.length === 0) {
          continue;
        }

        const status = deriveStatus(groupNodes);
        const primary = group.primary;
        const aggregateMetrics = groupNodes.reduce(
          (acc, node) => {
            acc.tokens_input += node.tokens_input || 0;
            acc.tokens_output += node.tokens_output || 0;
            acc.cost += node.cost || 0;
            acc.latency_ms += node.latency_ms || 0;
            return acc;
          },
          { tokens_input: 0, tokens_output: 0, cost: 0, latency_ms: 0 },
        );

        const errorNode = groupNodes.find((node) => node.status === 'failed' || node.status === 'cancelled');

        nodeExecutionStates[baseId] = {
          status,
          output: primary?.output,
          error: errorNode ? errorNode.error_message || errorNode.metadata || 'Cancelled' : undefined,
          metrics: primary
            ? {
                tokens_input: primary.tokens_input,
                tokens_output: primary.tokens_output,
                cost: primary.cost,
                latency_ms: primary.latency_ms,
              }
            : aggregateMetrics,
        };
      }

      const firstFailed = nodeRuns.find((node) => node.status === 'failed' || node.status === 'cancelled');
      const defaultSelectedNodeId = firstFailed?.node_id || nodeRuns[0]?.node_id || null;

      set({
        replayJobId: job.id,
        replayTimestamp: job.created_at,
        replayJob: job,
        replayNodes: nodeRuns,
        replayNodeResults,
        nodeExecutionStates,
        replaySelectedNodeId: defaultSelectedNodeId,
        replayFocusNonce: 0,
      });
    },

    exitReplayMode: () => {
      set({
        replayJobId: null,
        replayTimestamp: null,
        replayJob: null,
        replayNodes: [],
        replayNodeResults: {},
        replaySelectedNodeId: null,
        replayFocusNonce: 0,
        nodeExecutionStates: {},
      });
    },

    isInReplayMode: () => {
      return get().replayJobId !== null;
    },

    setReplaySelectedNodeId: (nodeId: string | null) => {
      set({ replaySelectedNodeId: nodeId });
    },

    requestReplayFocus: (nodeId: string) => {
      if (!nodeId) return;
      set((state) => ({
        replaySelectedNodeId: nodeId,
        replayFocusNonce: state.replayFocusNonce + 1,
      }));
    },
  };
});
