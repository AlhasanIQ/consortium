import { Check, ChevronRight, Copy } from 'lucide-react';
import { useState } from 'react';
import { Link } from 'react-router-dom';
import type { NodeMetric } from '@/api/adminClient';
import { StatusBadge } from '@/components/admin/StatusBadge';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import { copyToClipboard, durationLabel, formatCost, getExtractionMethod, isNodeReplayed } from '@/lib/formatters';
import { cn } from '@/lib/utils';

const nodeTypeBadge: Record<string, { label: string; className: string }> = {
  agent: { label: 'Agent', className: 'border-sky-200 bg-sky-50 text-sky-700' },
  prompt: { label: 'Prompt', className: 'border-teal-200 bg-teal-50 text-teal-700' },
  conditional: { label: 'Conditional', className: 'border-amber-200 bg-amber-50 text-amber-700' },
  result: { label: 'Result', className: 'border-emerald-200 bg-emerald-50 text-emerald-700' },
  child_workflow: { label: 'Child Workflow', className: 'border-indigo-200 bg-indigo-50 text-indigo-700' },
  contract_extract: { label: 'Contract', className: 'border-violet-200 bg-violet-50 text-violet-700' },
  input: { label: 'Input', className: 'border-zinc-200 bg-zinc-50 text-zinc-600' },
};

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    const ok = await copyToClipboard(text);
    if (ok) {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  return (
    <button
      type="button"
      onClick={handleCopy}
      className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] text-zinc-400 transition-colors hover:bg-zinc-100 hover:text-zinc-600"
    >
      {copied ? <Check className="h-3 w-3 text-emerald-500" /> : <Copy className="h-3 w-3" />}
      {copied ? 'Copied' : 'Copy'}
    </button>
  );
}

function Section({
  title,
  content,
  defaultCollapsed = false,
}: {
  title: string;
  content?: string;
  defaultCollapsed?: boolean;
}) {
  const [collapsed, setCollapsed] = useState(defaultCollapsed);
  if (!content) return null;
  return (
    <div className="space-y-1">
      <div className="flex items-center justify-between">
        <button
          type="button"
          className="flex items-center gap-1 text-[11px] font-semibold uppercase tracking-wide text-zinc-400 hover:text-zinc-600"
          onClick={() => setCollapsed((c) => !c)}
        >
          <ChevronRight className={cn('h-3 w-3 transition-transform', !collapsed && 'rotate-90')} />
          {title}
        </button>
        <CopyButton text={content} />
      </div>
      {!collapsed && (
        <pre className="max-h-[300px] overflow-auto rounded-md bg-zinc-950 p-3 text-xs leading-relaxed text-zinc-200">
          {content}
        </pre>
      )}
    </div>
  );
}

export function NodeTraceCard({ node, defaultOpen = false }: { node: NodeMetric; defaultOpen?: boolean }) {
  const typeBadge = nodeTypeBadge[node.node_type];

  return (
    <Collapsible defaultOpen={defaultOpen}>
      <div className="rounded-lg border border-zinc-200 bg-white transition-shadow hover:shadow-sm">
        <CollapsibleTrigger asChild>
          <button type="button" className="flex w-full items-center justify-between gap-3 px-4 py-3 text-left">
            <div className="flex items-center gap-2 truncate">
              <ChevronRight className="h-4 w-4 shrink-0 text-zinc-400 transition-transform [[data-state=open]_&]:rotate-90" />
              <span className="font-mono text-xs font-semibold text-zinc-600">S{node.DisplayOrder}</span>
              <span className="truncate text-sm font-medium text-zinc-800">
                {node.node_label || node.node_name || node.node_type}
              </span>
              {typeBadge && (
                <Badge variant="outline" className={cn('text-[10px]', typeBadge.className)}>
                  {typeBadge.label}
                </Badge>
              )}
              <StatusBadge status={node.status} />
              {isNodeReplayed(node.metadata) && (
                <Badge variant="outline" className="border-amber-300 bg-amber-50 text-amber-700 text-[10px]">
                  replayed
                </Badge>
              )}
              {getExtractionMethod(node.metadata) === 'regex' && (
                <Badge variant="outline" className="border-emerald-300 bg-emerald-50 text-emerald-700 text-[10px]">
                  regex
                </Badge>
              )}
              {getExtractionMethod(node.metadata) === 'llm_fallback' && (
                <Badge variant="outline" className="border-amber-300 bg-amber-50 text-amber-700 text-[10px]">
                  llm fallback
                </Badge>
              )}
            </div>
            <div className="flex items-center gap-3 shrink-0 text-xs text-zinc-500">
              {node.model && <span className="hidden sm:inline">{node.model}</span>}
              <span>{(node.tokens_input + node.tokens_output).toLocaleString()} tok</span>
              <span>{formatCost(node.cost)}</span>
              <span>{durationLabel(node.latency_ms)}</span>
            </div>
          </button>
        </CollapsibleTrigger>
        <CollapsibleContent>
          <div className="space-y-3 border-t border-zinc-100 px-4 py-3">
            {node.ChildJobID && (
              <Link
                to={`/admin/jobs/${node.ChildJobID}`}
                className="inline-flex items-center gap-1.5 rounded-md border border-indigo-200 bg-indigo-50 px-3 py-1.5 text-xs font-medium text-indigo-600 transition-colors hover:bg-indigo-100"
              >
                View Child Job →
              </Link>
            )}
            <Section title="Config" content={node.NodeConfig} defaultCollapsed />
            <Section title="System Prompt" content={node.SystemPrompt} />
            <Section title="Prompt" content={node.prompt} />
            <Section title="Output" content={node.output} />
            {node.error_message && (
              <div className="space-y-1">
                <h4 className="text-[11px] font-semibold uppercase tracking-wide text-red-400">Error</h4>
                <pre className="max-h-[200px] overflow-auto rounded-md bg-red-950 p-3 text-xs leading-relaxed text-red-200">
                  {node.error_message}
                </pre>
              </div>
            )}
            <div className="flex flex-wrap gap-3 text-xs text-zinc-500">
              {node.model && <span>Model: {node.model}</span>}
              <span>Input: {node.tokens_input.toLocaleString()} tok</span>
              <span>Output: {node.tokens_output.toLocaleString()} tok</span>
              <span>Latency: {durationLabel(node.latency_ms)}</span>
              <span>Cost: {formatCost(node.cost)}</span>
            </div>
            {/* Child workflow substeps */}
            {node.ChildJobNodes && node.ChildJobNodes.length > 0 && (
              <div className="mt-3 space-y-2 rounded-md border border-indigo-200 bg-indigo-50/30 p-3">
                <h4 className="text-[11px] font-semibold uppercase tracking-wide text-indigo-500">
                  Child Substeps ({node.ChildJobNodes.length})
                </h4>
                {node.ChildJobNodes.map((child) => (
                  <div key={`child-${child.node_id}`} className="rounded-md border border-indigo-100 bg-white p-2.5">
                    <div className="flex items-center justify-between gap-2">
                      <div className="flex items-center gap-2 truncate">
                        <span className="font-mono text-xs font-semibold text-indigo-600">↳ {child.DisplayOrder}</span>
                        <span className="truncate text-sm font-medium text-zinc-700">
                          {child.node_label || child.node_name || child.node_type}
                        </span>
                        <Badge variant="outline" className="border-indigo-200 text-indigo-600 text-[10px]">
                          {child.node_type}
                        </Badge>
                        <StatusBadge status={child.status} />
                        {isNodeReplayed(child.metadata) && (
                          <Badge variant="outline" className="border-amber-300 bg-amber-50 text-amber-700 text-[10px]">
                            replayed
                          </Badge>
                        )}
                      </div>
                      <div className="flex items-center gap-3 shrink-0 text-xs text-zinc-500">
                        {child.model && <span>{child.model}</span>}
                        <span>{(child.tokens_input + child.tokens_output).toLocaleString()} tok</span>
                        <span>{formatCost(child.cost)}</span>
                        <span>{durationLabel(child.latency_ms)}</span>
                      </div>
                    </div>
                    {child.output && (
                      <pre className="mt-2 max-h-[120px] overflow-auto rounded bg-zinc-950 p-2 text-[11px] leading-relaxed text-zinc-300">
                        {child.output}
                      </pre>
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>
        </CollapsibleContent>
      </div>
    </Collapsible>
  );
}

export function NodeTraceList({ nodes }: { nodes: NodeMetric[] }) {
  const [allOpen, setAllOpen] = useState(false);

  if (nodes.length === 0) {
    return (
      <div className="rounded-lg border border-dashed border-zinc-300 p-6 text-center text-sm text-zinc-500">
        No workflow nodes found.
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <div className="flex justify-end gap-2">
        <Button size="sm" variant="outline" onClick={() => setAllOpen(true)}>
          Expand All
        </Button>
        <Button size="sm" variant="outline" onClick={() => setAllOpen(false)}>
          Collapse All
        </Button>
      </div>
      {/* Key forces remount when allOpen toggles, resetting collapsible state */}
      <div key={String(allOpen)} className="space-y-2">
        {nodes.map((node) => (
          <NodeTraceCard key={`${node.node_id}-${node.DisplayOrder}`} node={node} defaultOpen={allOpen} />
        ))}
      </div>
    </div>
  );
}
