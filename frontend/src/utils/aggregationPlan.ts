import type { Edge, Node } from '@xyflow/react';
import type { AggregationConfig, AggregationMethod, NodeData } from '../types/workflow';

export type AggregationModelOwnership = 'none' | 'owned' | 'conditional' | 'inherited';

export interface AggregationPlanStep {
  id: string;
  label: string;
  kind: 'input' | 'deterministic' | 'llm' | 'decision' | 'output';
  detail?: string;
  modelSource?: string;
}

export interface AggregationPlanEdge {
  source: string;
  target: string;
  label?: string;
}

export interface AggregationPlan {
  method: AggregationMethod;
  title: string;
  modelOwnership: AggregationModelOwnership;
  inputCount: number;
  steps: AggregationPlanStep[];
  edges: AggregationPlanEdge[];
}

interface BuildAggregationPlanOptions {
  method: AggregationMethod;
  inputIds?: string[];
  config?: AggregationConfig | Record<string, unknown>;
}

const configValue = (config: AggregationConfig | Record<string, unknown> | undefined, key: string): unknown =>
  config && typeof config === 'object' ? (config as Record<string, unknown>)[key] : undefined;

const assertNever = (value: never): never => {
  throw new Error(`Unsupported aggregation method: ${String(value)}`);
};

const presentationPlan = (
  method: AggregationMethod,
  title: string,
  modelOwnership: AggregationModelOwnership,
  inputCount: number,
): AggregationPlan => ({
  method,
  title,
  modelOwnership,
  inputCount,
  steps: [],
  edges: [],
});

export function buildAggregationPlan({ method, inputIds = [], config }: BuildAggregationPlanOptions): AggregationPlan {
  const inputCount = inputIds.length;

  switch (method) {
    case 'collect': {
      return presentationPlan(method, 'Collect', 'none', inputCount);
    }
    case 'majority_vote': {
      const tieBreaker = configValue(config, 'tie_breaker_method') || configValue(config, 'tie_breaker');
      const includeSynthesis = tieBreaker === 'synthesis';
      return presentationPlan(method, 'Majority Vote', includeSynthesis ? 'conditional' : 'none', inputCount);
    }
    case 'debate_decide': {
      return presentationPlan(method, 'Debate & Decide', 'owned', inputCount);
    }
    case 'judge': {
      return presentationPlan(method, 'Judge', 'owned', inputCount);
    }
    case 'scoring': {
      return presentationPlan(method, 'Scoring', 'owned', inputCount);
    }
    case 'synthesis': {
      return presentationPlan(method, 'Synthesis', 'owned', inputCount);
    }
    case 'peer_matrix': {
      return presentationPlan(method, 'Peer Matrix', 'inherited', inputCount);
    }
    default:
      return assertNever(method);
  }
}

function stripAggregationPreviewShifts(node: Node<NodeData>): Node<NodeData> {
  const shifts = node.data.config.aggregationPreviewShifts;
  if (!shifts || Object.keys(shifts).length === 0) {
    return node;
  }

  const total = Object.values(shifts).reduce(
    (sum, shift) => ({
      x: sum.x + (Number.isFinite(shift?.x) ? shift.x : 0),
      y: sum.y + (Number.isFinite(shift?.y) ? shift.y : 0),
    }),
    { x: 0, y: 0 },
  );
  const { aggregationPreviewShifts: _aggregationPreviewShifts, ...config } = node.data.config;

  return {
    ...node,
    position: {
      x: node.position.x - total.x,
      y: node.position.y - total.y,
    },
    data: {
      ...node.data,
      config,
    },
  };
}

export function filterEphemeralAggregationGraph({ nodes, edges }: { nodes: Node<NodeData>[]; edges: Edge[] }): {
  nodes: Node<NodeData>[];
  edges: Edge[];
} {
  const ephemeralIDs = new Set(nodes.filter(isEphemeralAggregationInternalNode).map((node) => node.id));
  if (ephemeralIDs.size === 0) {
    return {
      nodes: nodes.map(stripAggregationPreviewShifts),
      edges: edges.map((edge) => restorePreviewHiddenEdge(edge)),
    };
  }

  return {
    nodes: nodes.filter((node) => !ephemeralIDs.has(node.id)).map(stripAggregationPreviewShifts),
    edges: edges
      .filter((edge) => !ephemeralIDs.has(edge.source) && !ephemeralIDs.has(edge.target))
      .map((edge) => restorePreviewHiddenEdge(edge)),
  };
}

type AggregationNodeLike = { data: NodeData };

export function isExpandedAggregationFrameNode(node: AggregationNodeLike): boolean {
  return node.data.type === 'aggregation_frame' && node.data.config.aggregationInternalState === 'expanded';
}

export function isExpandedAggregationPreviewNode(node: AggregationNodeLike): boolean {
  return node.data.config.aggregationInternalState === 'expanded';
}

export function isMovableExpandedAggregationPreviewNode(node: AggregationNodeLike): boolean {
  return !isExpandedAggregationFrameNode(node) && isExpandedAggregationPreviewNode(node);
}

export function isEphemeralAggregationInternalNode(node: AggregationNodeLike): boolean {
  return (
    isExpandedAggregationFrameNode(node) ||
    isExpandedAggregationPreviewNode(node) ||
    node.data.config.sourceLocked === true
  );
}

export function restorePreviewHiddenEdge(edge: Edge, anchorID?: string): Edge {
  const data = (edge as { data?: Record<string, unknown> }).data;
  if (!data?.aggregationPreviewHidden) {
    return edge;
  }
  const ownerIDs = previewHiddenOwnerIDs(data.aggregationPreviewHiddenBy);
  if (anchorID && ownerIDs.length > 0 && !ownerIDs.includes(anchorID)) {
    return edge;
  }
  const remainingOwnerIDs = anchorID ? ownerIDs.filter((ownerID) => ownerID !== anchorID) : [];
  const {
    aggregationPreviewHidden: _aggregationPreviewHidden,
    aggregationPreviewHiddenBy: _aggregationPreviewHiddenBy,
    ...restData
  } = data;
  if (remainingOwnerIDs.length > 0) {
    return {
      ...edge,
      hidden: true,
      data: {
        ...restData,
        aggregationPreviewHidden: true,
        aggregationPreviewHiddenBy: remainingOwnerIDs,
      },
    } as Edge;
  }
  const {
    hidden: _hidden,
    data: _data,
    ...rest
  } = edge as Edge & {
    hidden?: boolean;
    data?: Record<string, unknown>;
  };
  return Object.keys(restData).length > 0 ? ({ ...rest, data: restData } as Edge) : (rest as Edge);
}

export function previewHiddenOwnerIDs(value: unknown): string[] {
  if (Array.isArray(value)) {
    return value.filter((ownerID): ownerID is string => typeof ownerID === 'string');
  }
  return typeof value === 'string' ? [value] : [];
}
