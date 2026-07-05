import dagre from '@dagrejs/dagre';
import type { Edge, Node } from '@xyflow/react';
import type { NodeData, NodeType } from '../types/workflow';

type LayoutNode = Node<NodeData> & {
  measured?: { width?: number; height?: number };
  style?: { width?: unknown; height?: unknown };
};

interface LayoutPoint {
  x: number;
  y: number;
}

interface LayoutSize {
  width: number;
  height: number;
}

interface LayoutWorkflowOptions {
  origin?: LayoutPoint;
  rankSep?: number;
  nodeSep?: number;
  getNodeSize?: (node: Node<NodeData>) => LayoutSize;
}

const DEFAULT_ORIGIN: LayoutPoint = { x: 0, y: 0 };
const DEFAULT_RANK_SEP = 180;
const DEFAULT_NODE_SEP = 96;

const DEFAULT_NODE_SIZE: Record<NodeType, LayoutSize> = {
  input: { width: 220, height: 110 },
  agent: { width: 500, height: 170 },
  agent_run: { width: 500, height: 170 },
  novo_run: { width: 500, height: 170 },
  contract_extract: { width: 500, height: 170 },
  child_workflow: { width: 320, height: 150 },
  workflow_ref: { width: 320, height: 150 },
  operation: { width: 500, height: 170 },
  conditional: { width: 360, height: 150 },
  aggregation: { width: 320, height: 190 },
  aggregation_frame: { width: 640, height: 360 },
  result: { width: 260, height: 130 },
};

export function layoutWorkflowNodesTopToBottom<T extends Node<NodeData>>(
  nodes: T[],
  edges: Pick<Edge, 'source' | 'target'>[],
  options: LayoutWorkflowOptions = {},
): T[] {
  if (nodes.length === 0) {
    return nodes;
  }

  const origin = options.origin || DEFAULT_ORIGIN;
  const graph = new dagre.graphlib.Graph();
  graph.setDefaultEdgeLabel(() => ({}));
  graph.setGraph({
    rankdir: 'TB',
    ranksep: options.rankSep ?? DEFAULT_RANK_SEP,
    nodesep: options.nodeSep ?? DEFAULT_NODE_SEP,
    marginx: 0,
    marginy: 0,
  });

  const ids = new Set(nodes.map((node) => node.id));
  for (const node of [...nodes].sort((left, right) => left.id.localeCompare(right.id))) {
    const size = options.getNodeSize?.(node) ?? measuredNodeSize(node);
    graph.setNode(node.id, size);
  }
  for (const edge of [...edges].sort((left, right) =>
    `${left.source}->${left.target}`.localeCompare(`${right.source}->${right.target}`),
  )) {
    if (!ids.has(edge.source) || !ids.has(edge.target) || edge.source === edge.target) {
      continue;
    }
    graph.setEdge(edge.source, edge.target);
  }

  dagre.layout(graph);

  const positions = new Map<string, LayoutPoint>();
  for (const node of nodes) {
    const positioned = graph.node(node.id);
    const size = options.getNodeSize?.(node) ?? measuredNodeSize(node);
    positions.set(node.id, {
      x: (positioned?.x ?? 0) - size.width / 2,
      y: (positioned?.y ?? 0) - size.height / 2,
    });
  }

  const minX = Math.min(...[...positions.values()].map((position) => position.x));
  const minY = Math.min(...[...positions.values()].map((position) => position.y));

  return nodes.map((node) => {
    const position = positions.get(node.id) || DEFAULT_ORIGIN;
    return {
      ...node,
      position: {
        x: Math.round(position.x - minX + origin.x),
        y: Math.round(position.y - minY + origin.y),
      },
    };
  });
}

export function workflowNodeLayoutSize(node: Node<NodeData>): LayoutSize {
  const flowNode = node as LayoutNode;
  return {
    width:
      numericDimension(flowNode.measured?.width) ??
      numericDimension(flowNode.style?.width) ??
      DEFAULT_NODE_SIZE[node.data.type]?.width ??
      320,
    height:
      numericDimension(flowNode.measured?.height) ??
      numericDimension(flowNode.style?.height) ??
      DEFAULT_NODE_SIZE[node.data.type]?.height ??
      150,
  };
}

const measuredNodeSize = workflowNodeLayoutSize;

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
