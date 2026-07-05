import { AlertTriangle, ExternalLink } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { type AdminWorkflowsResponse, adminClient } from '@/api/adminClient';
import { Breadcrumbs } from '@/components/admin/Breadcrumbs';
import { EmptyState } from '@/components/admin/EmptyState';
import { SortableTableHead } from '@/components/admin/SortableTableHead';
import { StatCard } from '@/components/admin/StatCard';
import { StatusBadge } from '@/components/admin/StatusBadge';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { formatCost, relativeTime } from '@/lib/formatters';
import { applySortParams, parseSortParams, sortRows } from '@/lib/useSort';
import { benchmarkL0WarningLabel, workflowReferenceGroups } from './adminWorkflowUtils';

// Layer filter buckets. 'other' = workflows whose ID matches no L0/L1/L2/L3 prefix.
type LayerFilter = 'all' | 'L0' | 'L1' | 'L2' | 'L3' | 'other';

const LAYER_FILTERS: Array<{ value: LayerFilter; label: string }> = [
  { value: 'all', label: 'All' },
  { value: 'L0', label: 'L0 · Aggregation' },
  { value: 'L1', label: 'L1 · Reasoning' },
  { value: 'L2', label: 'L2 · Composite' },
  { value: 'L3', label: 'L3 · Benchmark' },
  { value: 'other', label: 'Other' },
];

// Badge styling per layer; falls back to muted for uncategorized.
const layerBadgeClass: Record<string, string> = {
  L0: 'border-violet-500/40 text-violet-400',
  L1: 'border-emerald-500/40 text-emerald-400',
  L2: 'border-amber-500/40 text-amber-400',
  L3: 'border-sky-500/40 text-sky-400',
};

// Cost-tier filter, orthogonal to layer. Cheap variants carry a '-cheap' ID suffix.
type CheapFilter = 'all' | 'standard' | 'cheap';

const CHEAP_FILTERS: Array<{ value: CheapFilter; label: string }> = [
  { value: 'all', label: 'All tiers' },
  { value: 'standard', label: 'Standard' },
  { value: 'cheap', label: 'Cheap' },
];

const isCheapVariant = (id: string): boolean => id.endsWith('-cheap');

export default function AdminWorkflowsPage() {
  const [data, setData] = useState<AdminWorkflowsResponse | null>(null);
  const [error, setError] = useState('');
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const sort = parseSortParams(searchParams, 'updated', 'desc');
  const [layerFilter, setLayerFilter] = useState<LayerFilter>('all');
  const [cheapFilter, setCheapFilter] = useState<CheapFilter>('all');

  useEffect(() => {
    adminClient
      .listWorkflows()
      .then((response) => setData(response))
      .catch((err: Error) => setError(err.message || 'Failed to load workflows'));
  }, []);

  const workflows = data?.Workflows ?? [];

  const handleSort = (column: string) => {
    applySortParams(searchParams, setSearchParams, column, sort);
  };

  const layerCounts = useMemo(() => {
    const counts: Record<LayerFilter, number> = { all: workflows.length, L0: 0, L1: 0, L2: 0, L3: 0, other: 0 };
    for (const w of workflows) {
      if (w.Layer === 'L0' || w.Layer === 'L1' || w.Layer === 'L2' || w.Layer === 'L3') counts[w.Layer] += 1;
      else counts.other += 1;
    }
    return counts;
  }, [workflows]);

  const cheapCounts = useMemo(() => {
    let cheap = 0;
    for (const w of workflows) if (isCheapVariant(w.id)) cheap += 1;
    return { all: workflows.length, standard: workflows.length - cheap, cheap };
  }, [workflows]);

  const filteredWorkflows = useMemo(() => {
    return workflows.filter((w) => {
      if (layerFilter === 'other') {
        if (w.Layer === 'L0' || w.Layer === 'L1' || w.Layer === 'L2' || w.Layer === 'L3') return false;
      } else if (layerFilter !== 'all' && w.Layer !== layerFilter) {
        return false;
      }
      if (cheapFilter === 'cheap' && !isCheapVariant(w.id)) return false;
      if (cheapFilter === 'standard' && isCheapVariant(w.id)) return false;
      return true;
    });
  }, [workflows, layerFilter, cheapFilter]);

  const sortedWorkflows = useMemo(
    () =>
      sortRows(filteredWorkflows, sort, {
        name: (w) => (w.name || w.id).toLowerCase(),
        layer: (w) => w.Layer || 'zzz', // uncategorized sorts last
        runs: (w) => w.ExecutionCount,
        success: (w) => w.SuccessRate,
        cost: (w) => w.TotalCost,
        updated: (w) => (w.updated_at ? new Date(w.updated_at).getTime() : 0),
      }),
    [filteredWorkflows, sort],
  );

  if (error) return <EmptyState message={error} />;
  if (!data) return <EmptyState message="Loading workflows..." />;

  return (
    <div className="space-y-4">
      <Breadcrumbs items={[{ label: 'Admin', to: '/admin' }, { label: 'Workflows' }]} />

      <div className="grid gap-4 md:grid-cols-3">
        <StatCard label="Total Workflows" value={data.TotalWorkflows} color="teal" />
        <StatCard label="Active (7d)" value={data.ActiveWorkflows} color="sky" />
        <StatCard label="Total Executions" value={data.TotalExecutions} color="emerald" />
      </div>

      <Card>
        <CardHeader className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
          <CardTitle>Workflow Definitions</CardTitle>
          <div className="flex flex-wrap items-center gap-3">
            <div className="flex flex-wrap gap-1">
              {LAYER_FILTERS.map((f) => (
                <Button
                  key={f.value}
                  size="sm"
                  variant={layerFilter === f.value ? 'default' : 'outline'}
                  onClick={() => setLayerFilter(f.value)}
                >
                  {f.label}
                  <span className="ml-1.5 opacity-60">{layerCounts[f.value]}</span>
                </Button>
              ))}
            </div>
            <div className="flex flex-wrap gap-1">
              {CHEAP_FILTERS.map((f) => (
                <Button
                  key={f.value}
                  size="sm"
                  variant={cheapFilter === f.value ? 'default' : 'outline'}
                  onClick={() => setCheapFilter(f.value)}
                >
                  {f.label}
                  <span className="ml-1.5 opacity-60">{cheapCounts[f.value]}</span>
                </Button>
              ))}
            </div>
          </div>
        </CardHeader>
        <CardContent>
          {workflows.length === 0 ? (
            <EmptyState message="No workflows found." />
          ) : sortedWorkflows.length === 0 ? (
            <EmptyState message="No workflows match these filters." />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <SortableTableHead column="name" label="Name" sort={sort} onSort={handleSort} />
                  <SortableTableHead column="layer" label="Layer" sort={sort} onSort={handleSort} />
                  <TableHead>Sources</TableHead>
                  <TableHead>Node Types</TableHead>
                  <TableHead>Models</TableHead>
                  <SortableTableHead column="runs" label="Runs" sort={sort} onSort={handleSort} />
                  <SortableTableHead column="success" label="Success" sort={sort} onSort={handleSort} />
                  <SortableTableHead column="cost" label="Cost" sort={sort} onSort={handleSort} />
                  <TableHead>Last Status</TableHead>
                  <SortableTableHead column="updated" label="Updated" sort={sort} onSort={handleSort} />
                  <TableHead className="w-[80px]" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {sortedWorkflows.map((workflow) => {
                  const modelsUsed = Array.isArray(workflow.ModelsUsed) ? workflow.ModelsUsed : [];
                  const referenceGroups = workflowReferenceGroups(workflow);
                  const warningLabel = benchmarkL0WarningLabel(workflow);
                  const nodeTypes =
                    workflow.NodeTypes && !Array.isArray(workflow.NodeTypes)
                      ? workflow.NodeTypes
                      : ({} as Record<string, number>);

                  return (
                    <TableRow
                      key={workflow.id}
                      className="cursor-pointer"
                      onClick={() => navigate(`/admin/workflows/${workflow.id}`)}
                    >
                      <TableCell>
                        <p className="font-medium">{workflow.name || workflow.id}</p>
                        {workflow.description ? <p className="text-xs text-zinc-500">{workflow.description}</p> : null}
                        {warningLabel ? (
                          <Badge
                            variant="outline"
                            className="mt-1.5 inline-flex items-center gap-1 border-amber-300 bg-amber-50 text-amber-700 text-[10px]"
                          >
                            <AlertTriangle className="h-3 w-3" />
                            {warningLabel}
                          </Badge>
                        ) : null}
                      </TableCell>
                      <TableCell>
                        {workflow.Layer ? (
                          <Badge variant="outline" className={layerBadgeClass[workflow.Layer]}>
                            {workflow.Layer}
                          </Badge>
                        ) : (
                          <span className="text-xs text-zinc-500">—</span>
                        )}
                      </TableCell>
                      <TableCell>
                        <div className="flex max-w-[260px] flex-col gap-1">
                          {referenceGroups.map((group) => (
                            <div key={group.label} className="flex flex-wrap items-center gap-1">
                              <span className="text-[10px] font-medium text-zinc-500">{group.label}</span>
                              {group.ids.slice(0, 2).map((id) => (
                                <Badge key={id} variant="outline" className="max-w-[180px] truncate text-[10px]">
                                  {id}
                                </Badge>
                              ))}
                              {group.ids.length > 2 ? (
                                <Badge variant="secondary" className="text-[10px]">
                                  +{group.ids.length - 2}
                                </Badge>
                              ) : null}
                            </div>
                          ))}
                          {referenceGroups.length === 0 ? <span className="text-xs text-zinc-500">-</span> : null}
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="flex max-w-[220px] flex-wrap gap-1">
                          {Object.entries(nodeTypes).map(([type, count]) => (
                            <Badge key={type} variant="outline" className="text-[10px]">
                              {type} ({count})
                            </Badge>
                          ))}
                          {Object.keys(nodeTypes).length === 0 ? (
                            <Badge variant="outline" className="text-[10px]">
                              -
                            </Badge>
                          ) : null}
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className="flex max-w-[220px] flex-wrap gap-1">
                          {modelsUsed.slice(0, 4).map((model) => (
                            <Badge key={model} variant="secondary">
                              {model}
                            </Badge>
                          ))}
                          {modelsUsed.length > 4 ? <Badge variant="outline">+{modelsUsed.length - 4}</Badge> : null}
                          {modelsUsed.length === 0 ? <Badge variant="outline">-</Badge> : null}
                        </div>
                      </TableCell>
                      <TableCell>{workflow.ExecutionCount}</TableCell>
                      <TableCell>{workflow.SuccessRate.toFixed(1)}%</TableCell>
                      <TableCell>{formatCost(workflow.TotalCost)}</TableCell>
                      <TableCell>
                        {workflow.LastRunStatus ? <StatusBadge status={workflow.LastRunStatus} /> : '-'}
                      </TableCell>
                      <TableCell className="text-xs text-zinc-500" title={workflow.updated_at || ''}>
                        {workflow.updated_at ? relativeTime(workflow.updated_at) : '-'}
                      </TableCell>
                      <TableCell>
                        <Button
                          size="sm"
                          variant="ghost"
                          className="gap-1"
                          onClick={(e) => {
                            e.stopPropagation();
                            window.open(`/workflow/${workflow.id}`, '_blank');
                          }}
                        >
                          Edit
                          <ExternalLink className="h-3.5 w-3.5" />
                        </Button>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
