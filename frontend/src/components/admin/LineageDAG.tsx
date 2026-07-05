import dagre from '@dagrejs/dagre';
import {
  Background,
  Controls,
  type Edge,
  MarkerType,
  MiniMap,
  type Node,
  type NodeMouseHandler,
  ReactFlow,
} from '@xyflow/react';
import { useMemo } from 'react';
import { LineageNode } from '@/components/admin/LineageNode';
import '@xyflow/react/dist/style.css';

interface LineageInputNode {
  id: string;
  generation: number;
  parent_ids: string[];
  composite_score?: number;
  feasible?: boolean;
}

interface LineageInputEdge {
  from: string;
  to: string;
}

interface OrganismFitnessLookup {
  [id: string]: {
    adjusted_accuracy?: number;
  };
}

const NODE_WIDTH = 156;
const NODE_HEIGHT = 74;

export function LineageDAG({
  nodes,
  edges,
  bestOrganismID,
  fitnessLookup,
  onSelect,
  onOpenOrganism,
}: {
  nodes: LineageInputNode[];
  edges: LineageInputEdge[];
  bestOrganismID?: string;
  fitnessLookup?: OrganismFitnessLookup;
  onSelect?: (organismID: string) => void;
  onOpenOrganism?: (organismID: string) => void;
}) {
  const { flowNodes, flowEdges } = useMemo(() => {
    const graph = new dagre.graphlib.Graph();
    graph.setDefaultEdgeLabel(() => ({}));
    graph.setGraph({ rankdir: 'LR', ranksep: 140, nodesep: 56 });

    for (const node of nodes) {
      graph.setNode(node.id, { width: NODE_WIDTH, height: NODE_HEIGHT });
    }
    for (const edge of edges) {
      graph.setEdge(edge.from, edge.to);
    }
    dagre.layout(graph);

    const ancestryEdgeKeys = new Set<string>();
    if (bestOrganismID) {
      const byID = new Map(nodes.map((node) => [node.id, node]));
      const stack: string[] = [bestOrganismID];
      const seen = new Set<string>();
      while (stack.length > 0) {
        const id = stack.pop() as string;
        if (seen.has(id)) continue;
        seen.add(id);
        const node = byID.get(id);
        if (!node) continue;
        for (const parentID of node.parent_ids || []) {
          ancestryEdgeKeys.add(`${parentID}->${id}`);
          stack.push(parentID);
        }
      }
    }

    const flowNodesMapped: Node[] = nodes.map((node) => {
      const positioned = graph.node(node.id);
      const fitness = fitnessLookup?.[node.id];
      return {
        id: node.id,
        type: 'lineageNode',
        position: {
          x: (positioned?.x ?? 0) - NODE_WIDTH / 2,
          y: (positioned?.y ?? 0) - NODE_HEIGHT / 2,
        },
        data: {
          label: node.id.length > 12 ? `${node.id.slice(0, 12)}…` : node.id,
          generation: node.generation,
          score: node.composite_score,
          feasible: node.feasible,
          adjusted_accuracy: fitness?.adjusted_accuracy,
          best: bestOrganismID === node.id,
        },
      };
    });

    const flowEdgesMapped: Edge[] = edges.map((edge) => {
      const key = `${edge.from}->${edge.to}`;
      const highlighted = ancestryEdgeKeys.has(key);
      return {
        id: key,
        source: edge.from,
        target: edge.to,
        type: 'smoothstep',
        markerEnd: { type: MarkerType.ArrowClosed, width: 14, height: 14 },
        style: {
          stroke: highlighted ? '#10b981' : '#d4d4d8',
          strokeWidth: highlighted ? 2 : 1,
        },
      };
    });

    return { flowNodes: flowNodesMapped, flowEdges: flowEdgesMapped };
  }, [bestOrganismID, edges, fitnessLookup, nodes]);

  if (flowNodes.length === 0) {
    return <p className="text-sm text-zinc-500">No lineage data available.</p>;
  }

  const handleNodeClick: NodeMouseHandler = (_, node) => {
    onSelect?.(node.id);
  };

  const handleNodeDoubleClick: NodeMouseHandler = (_, node) => {
    onOpenOrganism?.(node.id);
  };

  return (
    <div className="h-[520px] w-full rounded-lg border border-zinc-200 bg-zinc-50/30">
      <ReactFlow
        nodes={flowNodes}
        edges={flowEdges}
        nodeTypes={{ lineageNode: LineageNode }}
        fitView
        fitViewOptions={{ padding: 0.25 }}
        onNodeClick={handleNodeClick}
        onNodeDoubleClick={handleNodeDoubleClick}
      >
        <MiniMap zoomable pannable />
        <Controls />
        <Background color="#e4e4e7" gap={20} />
      </ReactFlow>
    </div>
  );
}
