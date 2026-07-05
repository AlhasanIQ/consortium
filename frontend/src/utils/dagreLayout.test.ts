import { describe, expect, it } from 'bun:test';
import type { Edge, Node } from '@xyflow/react';
import type { NodeData } from '../types/workflow';
import { layoutWorkflowNodesTopToBottom } from './dagreLayout';

function node(id: string, type: NodeData['type'] = 'agent'): Node<NodeData> {
  return {
    id,
    type,
    position: { x: 0, y: 0 },
    data: { type, label: id, config: { alias: id } },
  };
}

const diamondEdges: Edge[] = [
  { id: 'input-a', source: 'input', target: 'a' },
  { id: 'input-b', source: 'input', target: 'b' },
  { id: 'a-result', source: 'a', target: 'result' },
  { id: 'b-result', source: 'b', target: 'result' },
];

describe('layoutWorkflowNodesTopToBottom', () => {
  it('lays out DAG ranks vertically from roots to leaves', () => {
    const laidOut = layoutWorkflowNodesTopToBottom(
      [node('input', 'input'), node('a'), node('b'), node('result', 'result')],
      diamondEdges,
      { origin: { x: 100, y: 200 } },
    );
    const byID = new Map(laidOut.map((item) => [item.id, item]));

    expect(byID.get('input')?.position.y).toBeLessThan(byID.get('a')?.position.y ?? 0);
    expect(byID.get('input')?.position.y).toBeLessThan(byID.get('b')?.position.y ?? 0);
    expect(byID.get('a')?.position.y).toBeLessThan(byID.get('result')?.position.y ?? 0);
    expect(byID.get('b')?.position.y).toBeLessThan(byID.get('result')?.position.y ?? 0);
    expect(byID.get('a')?.position.x).not.toBe(byID.get('b')?.position.x);
    expect(Math.min(...laidOut.map((item) => item.position.x))).toBe(100);
    expect(Math.min(...laidOut.map((item) => item.position.y))).toBe(200);
  });

  it('returns deterministic positions independent of input ordering', () => {
    const first = layoutWorkflowNodesTopToBottom(
      [node('input', 'input'), node('a'), node('b'), node('result', 'result')],
      diamondEdges,
    );
    const second = layoutWorkflowNodesTopToBottom(
      [node('result', 'result'), node('b'), node('a'), node('input', 'input')],
      [...diamondEdges].reverse(),
    );

    const positions = (items: Node<NodeData>[]) =>
      items
        .map((item) => [item.id, item.position.x, item.position.y])
        .sort((left, right) => String(left[0]).localeCompare(String(right[0])));

    expect(positions(second)).toEqual(positions(first));
  });
});
