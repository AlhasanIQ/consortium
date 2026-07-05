import { useCallback, useEffect, useMemo, useState } from 'react';
import { CartesianGrid, Legend, Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { adminClient, type OptimizationCompareResponse, type OptimizationRun } from '@/api/adminClient';
import { Breadcrumbs } from '@/components/admin/Breadcrumbs';
import { EmptyState } from '@/components/admin/EmptyState';
import { StatCard } from '@/components/admin/StatCard';
import { StatusBadge } from '@/components/admin/StatusBadge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Select } from '@/components/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { formatCost } from '@/lib/formatters';

const RUN_COLORS = ['#10b981', '#0ea5e9', '#f97316', '#8b5cf6', '#ef4444'];

export default function AdminOptimizeComparePage() {
  const [runs, setRuns] = useState<OptimizationRun[]>([]);
  const [selectedIDs, setSelectedIDs] = useState<string[]>([]);
  const [workflowFilter, setWorkflowFilter] = useState('');
  const [compareData, setCompareData] = useState<OptimizationCompareResponse | null>(null);
  const [error, setError] = useState('');

  const loadRuns = useCallback(() => {
    adminClient
      .listOptimizationRuns({ status: 'completed', workflow: workflowFilter || undefined, limit: 200 })
      .then((resp) => {
        setRuns(resp.runs ?? []);
      })
      .catch((err: Error) => setError(err.message || 'Failed to load optimization runs'));
  }, [workflowFilter]);

  useEffect(() => {
    loadRuns();
  }, [loadRuns]);

  useEffect(() => {
    if (selectedIDs.length < 2) {
      setCompareData(null);
      return;
    }
    adminClient
      .compareOptimizationRuns(selectedIDs)
      .then((resp) => setCompareData(resp))
      .catch((err: Error) => setError(err.message || 'Failed to compare runs'));
  }, [selectedIDs]);

  const workflows = useMemo(() => {
    const out = new Set<string>();
    for (const run of runs) out.add(run.workflow_id);
    return [...out].sort();
  }, [runs]);

  const runsByID = useMemo(() => {
    const out: Record<string, OptimizationRun> = {};
    for (const run of runs) out[run.id] = run;
    return out;
  }, [runs]);

  const selectedRuns = useMemo(() => selectedIDs.map((id) => runsByID[id]).filter(Boolean), [runsByID, selectedIDs]);

  const controlRunID = useMemo(() => {
    let bestID = '';
    let bestAcc = -Infinity;
    for (const run of selectedRuns) {
      const accuracy = run.best_fitness?.adjusted_accuracy ?? -Infinity;
      if (accuracy > bestAcc) {
        bestAcc = accuracy;
        bestID = run.id;
      }
    }
    return bestID;
  }, [selectedRuns]);

  const controlRun = controlRunID ? (selectedRuns.find((run) => run.id === controlRunID) ?? null) : null;

  const chartData = useMemo(() => {
    if (!compareData) return [] as Array<Record<string, number | string>>;

    const byGen = new Map<number, Record<string, number | string>>();
    for (const [runID, points] of Object.entries(compareData.generation_data || {})) {
      for (const point of points) {
        const row = byGen.get(point.generation) ?? { generation: point.generation };
        row[`${runID}_best`] = point.best_accuracy * 100;
        byGen.set(point.generation, row);
      }
    }
    return [...byGen.entries()].sort((a, b) => a[0] - b[0]).map(([, row]) => row);
  }, [compareData]);

  const bestOrganismByRun = useMemo(() => {
    const out: Record<string, NonNullable<OptimizationCompareResponse['best_organisms']>[number]> = {};
    for (const organism of compareData?.best_organisms ?? []) {
      out[organism.opt_run_id] = organism;
    }
    return out;
  }, [compareData?.best_organisms]);

  if (error) return <EmptyState message={error} />;

  return (
    <div className="space-y-4">
      <Breadcrumbs
        items={[{ label: 'Admin', to: '/admin' }, { label: 'Optimize', to: '/admin/optimize' }, { label: 'Compare' }]}
      />

      <Card>
        <CardHeader className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
          <CardTitle>Select Runs</CardTitle>
          <div className="flex gap-2">
            <Select value={workflowFilter} onChange={(event) => setWorkflowFilter(event.target.value)} className="w-72">
              <option value="">All workflows</option>
              {workflows.map((workflow) => (
                <option key={workflow} value={workflow}>
                  {workflow}
                </option>
              ))}
            </Select>
            <Button size="sm" variant="secondary" onClick={loadRuns}>
              Refresh
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          {runs.length === 0 ? (
            <EmptyState message="No completed runs found." />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead />
                  <TableHead>ID</TableHead>
                  <TableHead>Workflow</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Mutator</TableHead>
                  <TableHead>Gen</TableHead>
                  <TableHead>Best Acc</TableHead>
                  <TableHead>Budget</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {runs.map((run) => {
                  const checked = selectedIDs.includes(run.id);
                  return (
                    <TableRow key={run.id}>
                      <TableCell>
                        <input
                          type="checkbox"
                          checked={checked}
                          onChange={(event) => {
                            const next = new Set(selectedIDs);
                            if (event.target.checked) {
                              if (next.size >= 5) return;
                              next.add(run.id);
                            } else {
                              next.delete(run.id);
                            }
                            setSelectedIDs([...next]);
                          }}
                        />
                      </TableCell>
                      <TableCell className="font-mono text-xs">{run.id}</TableCell>
                      <TableCell className="max-w-[320px] truncate">{run.workflow_id}</TableCell>
                      <TableCell>
                        <StatusBadge status={run.status} />
                      </TableCell>
                      <TableCell>{run.mutator_mode || 'hybrid'}</TableCell>
                      <TableCell>{run.generation}</TableCell>
                      <TableCell>
                        {run.best_fitness ? `${(run.best_fitness.adjusted_accuracy * 100).toFixed(2)}%` : '-'}
                      </TableCell>
                      <TableCell>
                        {formatCost(run.spent_usd)} / {formatCost(run.total_budget_usd)}
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {selectedRuns.length > 0 && (
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
          <StatCard label="Runs Selected" value={selectedRuns.length} color="teal" />
          <StatCard
            label="Best Accuracy"
            value={`${(
              selectedRuns.reduce((max, run) => Math.max(max, run.best_fitness?.adjusted_accuracy ?? 0), 0) * 100
            ).toFixed(2)}%`}
            color="emerald"
          />
          <StatCard
            label="Cheapest Spend"
            value={formatCost(
              selectedRuns.reduce((min, run) => Math.min(min, run.spent_usd), Number.POSITIVE_INFINITY) || 0,
            )}
            color="amber"
          />
          <StatCard
            label="Best Efficiency"
            value={`${bestEfficiency(selectedRuns).toFixed(2)}pp / $`}
            hint="(best_acc - baseline) / spend"
            color="sky"
          />
        </div>
      )}

      {selectedRuns.length >= 2 && compareData && (
        <>
          <Card>
            <CardHeader>
              <CardTitle>Comparison Table</CardTitle>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Run</TableHead>
                    <TableHead>Mutator</TableHead>
                    <TableHead>Gens</TableHead>
                    <TableHead>Organisms</TableHead>
                    <TableHead>Best Acc</TableHead>
                    <TableHead>Acc Delta</TableHead>
                    <TableHead>Parse</TableHead>
                    <TableHead>Cost / Item</TableHead>
                    <TableHead>Budget</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {selectedRuns.map((run) => {
                    const controlAcc = controlRun?.best_fitness?.adjusted_accuracy;
                    const delta =
                      typeof controlAcc === 'number' ? (run.best_fitness?.adjusted_accuracy ?? 0) - controlAcc : null;
                    return (
                      <TableRow key={run.id}>
                        <TableCell className="font-mono text-xs">
                          {run.id}
                          {run.id === controlRunID ? ' *' : ''}
                        </TableCell>
                        <TableCell>{run.mutator_mode || 'hybrid'}</TableCell>
                        <TableCell>{run.generation}</TableCell>
                        <TableCell>{run.total_organisms}</TableCell>
                        <TableCell>
                          {run.best_fitness ? `${(run.best_fitness.adjusted_accuracy * 100).toFixed(2)}%` : '-'}
                        </TableCell>
                        <TableCell className={delta == null ? '' : delta >= 0 ? 'text-emerald-700' : 'text-red-700'}>
                          {delta == null ? '-' : `${delta >= 0 ? '+' : ''}${(delta * 100).toFixed(2)}pp`}
                        </TableCell>
                        <TableCell>
                          {run.best_fitness ? `${(run.best_fitness.parse_rate * 100).toFixed(2)}%` : '-'}
                        </TableCell>
                        <TableCell>{run.best_fitness ? formatCost(run.best_fitness.cost_per_item) : '-'}</TableCell>
                        <TableCell>{formatCost(run.spent_usd)}</TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Generation Curves</CardTitle>
            </CardHeader>
            <CardContent>
              {chartData.length === 0 ? (
                <p className="text-sm text-zinc-500">No generation data available for selected runs.</p>
              ) : (
                <div className="h-[300px] w-full">
                  <ResponsiveContainer width="100%" height="100%">
                    <LineChart data={chartData} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
                      <CartesianGrid strokeDasharray="3 3" stroke="#e4e4e7" />
                      <XAxis dataKey="generation" />
                      <YAxis tickFormatter={(value) => `${Number(value).toFixed(1)}%`} />
                      <Tooltip
                        formatter={(value: number | string | undefined) => `${Number(value ?? 0).toFixed(2)}%`}
                      />
                      <Legend />
                      {selectedRuns.map((run, index) => (
                        <Line
                          key={run.id}
                          type="monotone"
                          dataKey={`${run.id}_best`}
                          name={run.id}
                          stroke={RUN_COLORS[index % RUN_COLORS.length]}
                          strokeWidth={2}
                          dot={{ r: 2 }}
                        />
                      ))}
                    </LineChart>
                  </ResponsiveContainer>
                </div>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Best Organism Parameter Diff</CardTitle>
            </CardHeader>
            <CardContent>
              {selectedRuns.length < 2 ? (
                <p className="text-sm text-zinc-500">Select at least two runs.</p>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Path</TableHead>
                      {selectedRuns.map((run) => (
                        <TableHead key={run.id}>{run.id}</TableHead>
                      ))}
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {collectDiffPaths(selectedRuns.map((run) => bestOrganismByRun[run.id]).filter(Boolean)).map(
                      (path) => (
                        <TableRow key={path}>
                          <TableCell className="font-mono text-xs">{path}</TableCell>
                          {selectedRuns.map((run) => {
                            const org = bestOrganismByRun[run.id];
                            return <TableCell key={`${path}-${run.id}`}>{org?.param_values?.[path] ?? '-'}</TableCell>;
                          })}
                        </TableRow>
                      ),
                    )}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}

function bestEfficiency(runs: OptimizationRun[]): number {
  let best = 0;
  for (const run of runs) {
    const baseline = baselineForRun(run);
    const bestAcc = run.best_fitness?.adjusted_accuracy ?? 0;
    const spent = run.spent_usd || 0;
    if (spent <= 0) continue;
    const efficiency = ((bestAcc - baseline) * 100) / spent;
    if (efficiency > best) best = efficiency;
  }
  return best;
}

function baselineForRun(run: OptimizationRun): number {
  const learning = run.learning_log || [];
  const seedEntry = learning.find((entry) => entry.generation === 0);
  if (seedEntry) {
    const bestAcc = run.best_fitness?.adjusted_accuracy ?? 0;
    return bestAcc - seedEntry.fitness_delta;
  }
  return 0;
}

function collectDiffPaths(organisms: Array<{ param_values: Record<string, string> }>): string[] {
  const out = new Set<string>();
  for (const organism of organisms) {
    for (const path of Object.keys(organism.param_values || {})) {
      out.add(path);
    }
  }
  return [...out].sort();
}
