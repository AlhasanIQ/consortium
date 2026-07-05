import { useNavigate } from 'react-router-dom';
import type { AdminJobDetailResponse, NodeMetric } from '@/api/adminClient';
import { StatusBadge } from '@/components/admin/StatusBadge';
import { Badge } from '@/components/ui/badge';
import { durationLabel, isNodeReplayed } from '@/lib/formatters';
import { cn } from '@/lib/utils';

const nodeTypeColors: Record<string, string> = {
  agent: 'bg-sky-500',
  prompt: 'bg-teal-500',
  conditional: 'bg-amber-500',
  result: 'bg-emerald-500',
  child_workflow: 'bg-indigo-500',
  input: 'bg-zinc-400',
};

function barColor(node: NodeMetric): string {
  if (node.status === 'failed') return 'bg-red-500';
  return nodeTypeColors[node.node_type] || 'bg-zinc-900';
}

export function TimelineChart({
  nodes,
  lifecycle,
}: {
  nodes: NodeMetric[];
  lifecycle: AdminJobDetailResponse['Lifecycle'];
}) {
  const navigate = useNavigate();
  if (nodes.length === 0) {
    return (
      <div className="rounded-lg border border-dashed border-zinc-300 p-6 text-center text-sm text-zinc-500">
        No timeline data available.
      </div>
    );
  }

  return (
    <div className="space-y-3">
      {/* Job lifecycle bar */}
      <div className="space-y-1">
        <div className="flex items-center justify-between text-xs text-zinc-500">
          <span className="font-medium text-zinc-700">Job Lifecycle</span>
          <span>{durationLabel(lifecycle.ExecutionDurationMs)}</span>
        </div>
        <div className="relative h-3 rounded-full bg-zinc-100">
          {lifecycle.QueueDelayMs > 0 && lifecycle.ExecutionDurationMs > 0 && (
            <div
              className="absolute inset-y-0 left-0 rounded-l-full bg-amber-200"
              style={{
                width: `${Math.min((lifecycle.QueueDelayMs / (lifecycle.QueueDelayMs + lifecycle.ExecutionDurationMs)) * 100, 100)}%`,
              }}
            />
          )}
          <div
            className="absolute inset-y-0 rounded-r-full bg-teal-300"
            style={{
              left:
                lifecycle.ExecutionDurationMs > 0
                  ? `${Math.min((lifecycle.QueueDelayMs / (lifecycle.QueueDelayMs + lifecycle.ExecutionDurationMs)) * 100, 100)}%`
                  : '0%',
              right: '0%',
            }}
          />
        </div>
        <div className="flex gap-4 text-[10px] text-zinc-400">
          <span className="flex items-center gap-1">
            <span className="inline-block h-2 w-2 rounded-full bg-amber-200" /> Queued:{' '}
            {durationLabel(lifecycle.QueueDelayMs)}
          </span>
          <span className="flex items-center gap-1">
            <span className="inline-block h-2 w-2 rounded-full bg-teal-300" /> Executing:{' '}
            {durationLabel(lifecycle.ExecutionDurationMs)}
          </span>
        </div>
      </div>

      {/* Per-node bars */}
      <div className="space-y-1.5">
        {nodes.map((node) => {
          const width = Math.max(node.WidthPercent || 0, 1.5);
          const offset = node.StartOffsetPercent || 0;
          const childNodes = node.ChildJobNodes ?? [];
          return (
            <div key={`${node.node_id}-${node.DisplayOrder}`}>
              <div
                className={cn('group', node.ChildJobID && 'cursor-pointer rounded-md px-1 -mx-1 hover:bg-indigo-50/60')}
                onClick={node.ChildJobID ? () => navigate(`/admin/jobs/${node.ChildJobID}`) : undefined}
                onKeyDown={
                  node.ChildJobID
                    ? (e) => {
                        if (e.key === 'Enter') navigate(`/admin/jobs/${node.ChildJobID}`);
                      }
                    : undefined
                }
                role={node.ChildJobID ? 'link' : undefined}
                tabIndex={node.ChildJobID ? 0 : undefined}
              >
                <div className="flex items-center justify-between gap-2 text-xs">
                  <div className="flex items-center gap-2 truncate">
                    <span className={cn('inline-block h-2 w-2 shrink-0 rounded-full', barColor(node))} />
                    <span className="font-medium text-zinc-700">S{node.DisplayOrder}</span>
                    <span className="truncate text-zinc-500">
                      {node.node_label || node.node_name || node.node_type}
                    </span>
                    <StatusBadge status={node.status} className="scale-90" />
                    {isNodeReplayed(node.metadata) && (
                      <Badge
                        variant="outline"
                        className="scale-90 border-amber-300 bg-amber-50 text-amber-700 text-[10px]"
                      >
                        replayed
                      </Badge>
                    )}
                    {node.ChildJobID && <span className="text-[10px] text-indigo-500">→ child job</span>}
                  </div>
                  <div className="flex items-center gap-2 shrink-0 text-zinc-400">
                    {node.ActiveDurationMs > 0 && node.ActiveDurationMs !== node.TimelineDurationMs && (
                      <span title="Active time">{durationLabel(node.ActiveDurationMs)} active</span>
                    )}
                    <span className="font-medium text-zinc-600">{durationLabel(node.TimelineDurationMs)}</span>
                  </div>
                </div>
                <div className="relative mt-0.5 h-2 rounded bg-zinc-100">
                  <div
                    className={cn(
                      'absolute inset-y-0 rounded transition-all',
                      barColor(node),
                      'opacity-80 group-hover:opacity-100',
                    )}
                    style={{
                      left: `${offset}%`,
                      width: `${width}%`,
                    }}
                  />
                </div>
              </div>
              {/* Child workflow substep bars */}
              {childNodes.length > 0 && (
                <div className="ml-5 mt-1 space-y-1 border-l-2 border-indigo-200 pl-3">
                  {childNodes.map((child) => {
                    const cw = Math.max(child.WidthPercent || 0, 1.5);
                    const co = child.StartOffsetPercent || 0;
                    return (
                      <div key={`${node.node_id}-child-${child.node_id}`} className="group">
                        <div className="flex items-center justify-between gap-2 text-xs">
                          <div className="flex items-center gap-2 truncate">
                            <span className="inline-block h-1.5 w-1.5 shrink-0 rounded-full bg-indigo-400" />
                            <span className="font-medium text-indigo-600">↳ {child.DisplayOrder}</span>
                            <span className="truncate text-indigo-400">
                              {child.node_label || child.node_name || child.node_type}
                            </span>
                            <StatusBadge status={child.status} className="scale-75" />
                            {isNodeReplayed(child.metadata) && (
                              <Badge
                                variant="outline"
                                className="scale-75 border-amber-300 bg-amber-50 text-amber-700 text-[10px]"
                              >
                                replayed
                              </Badge>
                            )}
                          </div>
                          <span className="shrink-0 text-indigo-400">{durationLabel(child.TimelineDurationMs)}</span>
                        </div>
                        <div className="relative mt-0.5 h-1.5 rounded bg-indigo-50">
                          <div
                            className="absolute inset-y-0 rounded bg-indigo-400 opacity-70 transition-all group-hover:opacity-100"
                            style={{
                              left: `${co}%`,
                              width: `${cw}%`,
                            }}
                          />
                        </div>
                      </div>
                    );
                  })}
                </div>
              )}
            </div>
          );
        })}
      </div>

      {/* Legend */}
      <div className="flex flex-wrap gap-3 border-t border-zinc-100 pt-2 text-[10px] text-zinc-400">
        {Object.entries(nodeTypeColors).map(([type, bg]) => (
          <span key={type} className="flex items-center gap-1">
            <span className={cn('inline-block h-2 w-2 rounded-full', bg)} />
            {type}
          </span>
        ))}
        <span className="flex items-center gap-1">
          <span className="inline-block h-2 w-2 rounded-full bg-red-500" />
          failed
        </span>
      </div>
    </div>
  );
}
