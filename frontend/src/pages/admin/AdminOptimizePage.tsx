import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { adminClient, type OptimizationRun } from '@/api/adminClient';
import { Breadcrumbs } from '@/components/admin/Breadcrumbs';
import { EmptyState } from '@/components/admin/EmptyState';
import { SortableTableHead } from '@/components/admin/SortableTableHead';
import { StatCard } from '@/components/admin/StatCard';
import { StatusBadge } from '@/components/admin/StatusBadge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Progress } from '@/components/ui/progress';
import { Select } from '@/components/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { formatCost } from '@/lib/formatters';
import { usePolling } from '@/lib/usePolling';
import { applySortParams, parseSortParams, sortRows } from '@/lib/useSort';

export default function AdminOptimizePage() {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const [runs, setRuns] = useState<OptimizationRun[]>([]);
  const [error, setError] = useState('');

  const statusFilter = searchParams.get('status') || '';
  const workflowFilter = searchParams.get('workflow') || '';
  const sort = parseSortParams(searchParams, 'created', 'desc');

  const setParam = (key: string, value: string) => {
    const next = new URLSearchParams(searchParams);
    if (value) {
      next.set(key, value);
    } else {
      next.delete(key);
    }
    setSearchParams(next);
  };

  const load = useCallback(() => {
    adminClient
      .listOptimizationRuns({
        status: statusFilter || undefined,
        workflow: workflowFilter || undefined,
        limit: 200,
      })
      .then((resp) => setRuns(resp.runs ?? []))
      .catch((err: Error) => setError(err.message || 'Failed to load optimization runs'));
  }, [statusFilter, workflowFilter]);

  useEffect(() => {
    load();
  }, [load]);

  usePolling(
    load,
    3000,
    runs.some((run) => run.status === 'running'),
  );

  const workflows = useMemo(() => {
    const out = new Set<string>();
    for (const run of runs) {
      if (run.workflow_id) out.add(run.workflow_id);
    }
    return [...out].sort();
  }, [runs]);

  const statuses = useMemo(() => {
    const out = new Set<string>();
    for (const run of runs) {
      if (run.status) out.add(run.status);
    }
    return [...out].sort();
  }, [runs]);

  const sortedRuns = useMemo(
    () =>
      sortRows(runs, sort, {
        id: (r) => r.id,
        workflow: (r) => r.workflow_id,
        benchmark: (r) => r.benchmark,
        status: (r) => r.status,
        generation: (r) => r.generation,
        accuracy: (r) => r.best_fitness?.adjusted_accuracy ?? 0,
        budget: (r) => r.spent_usd,
        organisms: (r) => r.total_organisms,
        created: (r) => new Date(r.created_at).getTime(),
      }),
    [runs, sort],
  );

  const activeRun = useMemo(() => runs.find((run) => run.status === 'running') ?? null, [runs]);
  const bestAccuracy = useMemo(
    () =>
      runs
        .filter((run) => run.status === 'completed' && run.best_fitness)
        .reduce((max, run) => Math.max(max, run.best_fitness?.adjusted_accuracy ?? 0), 0),
    [runs],
  );

  const handleSort = (column: string) => {
    applySortParams(searchParams, setSearchParams, column, sort);
  };

  if (error) return <EmptyState message={error} />;

  return (
    <div className="space-y-4">
      <Breadcrumbs items={[{ label: 'Admin', to: '/admin' }, { label: 'Optimize' }]} />

      {activeRun && (
        <Card className="border-sky-200 bg-sky-50/40">
          <CardContent className="pt-5">
            <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
              <div>
                <p className="text-xs font-semibold uppercase tracking-wide text-sky-700">Active run</p>
                <p className="font-mono text-sm text-sky-900">{activeRun.id}</p>
                <p className="text-sm text-sky-800">
                  Gen {activeRun.generation} · Budget {formatCost(activeRun.spent_usd)} /{' '}
                  {formatCost(activeRun.total_budget_usd)} · Best{' '}
                  {activeRun.best_fitness ? `${(activeRun.best_fitness.adjusted_accuracy * 100).toFixed(2)}%` : '-'}
                </p>
              </div>
              <div className="flex gap-2">
                <Button size="sm" onClick={() => navigate(`/admin/optimize/${activeRun.id}`)}>
                  Open Run
                </Button>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <StatCard label="Total Runs" value={runs.length} color="teal" />
        <StatCard label="Active" value={runs.filter((run) => run.status === 'running').length} color="sky" />
        <StatCard label="Completed" value={runs.filter((run) => run.status === 'completed').length} color="emerald" />
        <StatCard label="Best Acc" value={`${(bestAccuracy * 100).toFixed(2)}%`} color="purple" />
      </div>

      <Card>
        <CardHeader className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
          <CardTitle>Optimization Runs</CardTitle>
          <div className="flex flex-wrap gap-2">
            <Button asChild size="sm" variant="outline">
              <Link to="/admin/optimize/compare">Compare Runs</Link>
            </Button>
            <Button size="sm" variant="secondary" onClick={load}>
              Refresh
            </Button>
            <Select value={statusFilter} onChange={(event) => setParam('status', event.target.value)} className="w-40">
              <option value="">All statuses</option>
              {statuses.map((status) => (
                <option key={status} value={status}>
                  {status}
                </option>
              ))}
            </Select>
            <Select
              value={workflowFilter}
              onChange={(event) => setParam('workflow', event.target.value)}
              className="w-72"
            >
              <option value="">All workflows</option>
              {workflows.map((workflow) => (
                <option key={workflow} value={workflow}>
                  {workflow}
                </option>
              ))}
            </Select>
          </div>
        </CardHeader>
        <CardContent>
          {sortedRuns.length === 0 ? (
            <EmptyState message="No optimization runs found." />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <SortableTableHead column="id" label="ID" sort={sort} onSort={handleSort} />
                  <SortableTableHead column="workflow" label="Workflow" sort={sort} onSort={handleSort} />
                  <SortableTableHead column="benchmark" label="Benchmark" sort={sort} onSort={handleSort} />
                  <SortableTableHead column="status" label="Status" sort={sort} onSort={handleSort} />
                  <SortableTableHead column="generation" label="Gen" sort={sort} onSort={handleSort} />
                  <SortableTableHead column="accuracy" label="Best Acc" sort={sort} onSort={handleSort} />
                  <SortableTableHead column="budget" label="Budget" sort={sort} onSort={handleSort} />
                  <SortableTableHead column="organisms" label="Organisms" sort={sort} onSort={handleSort} />
                  <TableHead>Mutator</TableHead>
                  <SortableTableHead column="created" label="Started" sort={sort} onSort={handleSort} />
                </TableRow>
              </TableHeader>
              <TableBody>
                {sortedRuns.map((run) => {
                  const budgetPct =
                    run.total_budget_usd > 0 ? Math.min((run.spent_usd / run.total_budget_usd) * 100, 100) : 0;
                  return (
                    <TableRow
                      key={run.id}
                      className="cursor-pointer"
                      onClick={() => navigate(`/admin/optimize/${run.id}`)}
                    >
                      <TableCell className="font-mono text-xs">{run.id}</TableCell>
                      <TableCell className="max-w-[280px] truncate">{run.workflow_id}</TableCell>
                      <TableCell>
                        {run.benchmark}/{run.split}
                      </TableCell>
                      <TableCell>
                        <StatusBadge status={run.status} />
                      </TableCell>
                      <TableCell>
                        {run.generation}/
                        {run.spec?.stop_policy && typeof run.spec.stop_policy === 'object'
                          ? String((run.spec.stop_policy as { max_generations?: number }).max_generations ?? '-')
                          : '-'}
                      </TableCell>
                      <TableCell>
                        {run.best_fitness ? `${(run.best_fitness.adjusted_accuracy * 100).toFixed(2)}%` : '-'}
                      </TableCell>
                      <TableCell className="min-w-[160px]">
                        <div className="space-y-1">
                          <Progress value={budgetPct} max={100} />
                          <div className="text-xs text-zinc-500">
                            {formatCost(run.spent_usd)} / {formatCost(run.total_budget_usd)}
                          </div>
                        </div>
                      </TableCell>
                      <TableCell>{run.total_organisms}</TableCell>
                      <TableCell>{run.mutator_mode || 'hybrid'}</TableCell>
                      <TableCell className="text-xs text-zinc-500">
                        {run.started_at
                          ? new Date(run.started_at).toLocaleString()
                          : new Date(run.created_at).toLocaleString()}
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
