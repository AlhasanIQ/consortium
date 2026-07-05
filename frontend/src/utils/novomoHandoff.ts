import type { Edge, Node } from '@xyflow/react';
import type { NodeData } from '../types/workflow';

export const isNovomoNodeType = (type?: string): boolean => type === 'agent_run' || type === 'novo_run';

export function findNearestNovomoHandoffSources(
  targetNodeId: string,
  nodes: Node<NodeData>[],
  edges: Edge[],
): string[] {
  if (!targetNodeId || edges.length === 0) return [];

  const nodeById = new Map(nodes.map((node) => [node.id, node]));
  const reverseEdges = new Map<string, string[]>();
  for (const edge of edges) {
    if (!edge.source || !edge.target) continue;
    reverseEdges.set(edge.target, [...(reverseEdges.get(edge.target) || []), edge.source]);
  }

  const seen = new Set<string>([targetNodeId]);
  let frontier = reverseEdges.get(targetNodeId) || [];
  while (frontier.length > 0) {
    const nextFrontier: string[] = [];
    const candidates: string[] = [];

    for (const nodeId of frontier) {
      if (seen.has(nodeId)) continue;
      seen.add(nodeId);

      const node = nodeById.get(nodeId);
      if (isNovomoNodeType(node?.data.type)) {
        candidates.push(nodeId);
        continue;
      }

      nextFrontier.push(...(reverseEdges.get(nodeId) || []));
    }

    const uniqueCandidates = Array.from(new Set(candidates)).sort();
    if (uniqueCandidates.length > 0) return uniqueCandidates;
    frontier = nextFrontier;
  }

  return [];
}

export function findAutoNovomoHandoffSource(
  targetNodeId: string,
  nodes: Node<NodeData>[],
  edges: Edge[],
): string | undefined {
  const candidates = findNearestNovomoHandoffSources(targetNodeId, nodes, edges);
  return candidates.length === 1 ? candidates[0] : undefined;
}

export function applyAutoNovomoHandoff(nodes: Node<NodeData>[], edges: Edge[]): Node<NodeData>[] {
  return nodes.map((node) => {
    if (!isNovomoNodeType(node.data.type)) return node;
    const mode = node.data.config.inheritFromMode || 'auto';
    if (mode !== 'auto') return node;

    const autoSource = findAutoNovomoHandoffSource(node.id, nodes, edges);
    const currentSource = node.data.config.inheritFromNodeId || undefined;
    if (autoSource === currentSource) return node;

    return {
      ...node,
      data: {
        ...node.data,
        config: {
          ...node.data.config,
          inheritFromMode: 'auto',
          inheritFromNodeId: autoSource,
          inheritFromKind: undefined,
          inheritFromId: undefined,
        },
      },
    };
  });
}
