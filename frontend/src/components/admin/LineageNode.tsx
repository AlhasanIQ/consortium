import type { NodeProps } from '@xyflow/react';

interface LineageNodeData {
  label: string;
  generation: number;
  score?: number;
  adjusted_accuracy?: number;
  feasible?: boolean;
  best?: boolean;
}

export function LineageNode({ data, selected }: NodeProps) {
  const nodeData = data as unknown as LineageNodeData;
  const bgClass = nodeData.best
    ? 'bg-emerald-50 border-emerald-300'
    : nodeData.feasible === false
      ? 'bg-amber-50 border-amber-300 border-dashed'
      : 'bg-white border-zinc-300';

  return (
    <div
      className={`min-w-[132px] rounded-md border px-2 py-1.5 text-xs shadow-sm ${bgClass} ${selected ? 'ring-2 ring-sky-400' : ''}`}
    >
      <div className="mb-1 flex items-center justify-between gap-2">
        <span className="rounded bg-zinc-900 px-1.5 py-0.5 text-[10px] font-semibold text-white">
          g{nodeData.generation}
        </span>
        <span className="truncate font-mono text-[10px] text-zinc-600">{nodeData.label}</span>
      </div>
      <div className="text-[11px] font-semibold text-zinc-800">
        {typeof nodeData.adjusted_accuracy === 'number'
          ? `${(nodeData.adjusted_accuracy * 100).toFixed(1)}%`
          : typeof nodeData.score === 'number'
            ? `score ${nodeData.score.toFixed(3)}`
            : 'no fitness'}
      </div>
      <div className="text-[10px] text-zinc-500">{nodeData.feasible === false ? 'infeasible' : 'feasible'}</div>
    </div>
  );
}
