import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import {
  type AdminBenchmarkComparisonResponse,
  adminClient,
  type BenchmarkItemComparisonResponse,
  type WrongAnswerCategory,
} from '@/api/adminClient';
import { Breadcrumbs } from '@/components/admin/Breadcrumbs';
import { EmptyState } from '@/components/admin/EmptyState';
import { StatCard } from '@/components/admin/StatCard';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Select } from '@/components/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { benchmarkItemDetailPath } from '@/lib/adminPaths';
import { formatCost } from '@/lib/formatters';

const FAILURE_REASONS = [
  'tool/runtime_failure',
  'provider_failure',
  'benchmark_paused',
  'empty_final_output',
  'invalid_contract_output',
  'multiple_letters',
  'conflicting_answer',
  'no_letter',
  'other_parse_error',
];

const CATEGORY_LABELS: Record<WrongAnswerCategory, string> = {
  all_steps_wrong: 'All Steps Wrong',
  some_right_child_wrong: 'Some Right, Child Wrong',
  all_right_child_wrong: 'All Right, Child Wrong',
  child_right_parent_wrong: 'Child Right, Parent Wrong',
  unclassified: 'Unclassified',
};

function formatFailureLabel(failureReason?: string, category?: WrongAnswerCategory): string {
  if (failureReason) return failureReason.replace(/_/g, ' ');
  if (category && category !== 'unclassified') return CATEGORY_LABELS[category];
  return 'wrong answer';
}

export default function AdminBenchmarkComparisonPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const [data, setData] = useState<AdminBenchmarkComparisonResponse | null>(null);
  const [error, setError] = useState('');
  const [enabledRuns, setEnabledRuns] = useState<Set<string>>(new Set());
  const [controlWorkflow, setControlWorkflow] = useState<string>('');
  const [itemComparison, setItemComparison] = useState<BenchmarkItemComparisonResponse | null>(null);
  const [itemCompareLoading, setItemCompareLoading] = useState(false);

  const selectedOption = searchParams.get('benchmark_split') || '';
  const benchmark = searchParams.get('benchmark') || '';
  const split = searchParams.get('split') || '';

  // Track previous benchmark/split so we only reset enabledRuns on dataset change.
  const prevDatasetRef = useRef('');
  const hasInitializedRef = useRef(false);

  const load = useCallback(() => {
    const params: { benchmark?: string; split?: string } = {
      benchmark: benchmark || undefined,
      split: split || undefined,
    };
    const datasetKey = `${benchmark}|${split}`;
    const isDatasetChange = datasetKey !== prevDatasetRef.current;
    prevDatasetRef.current = datasetKey;

    adminClient
      .compareBenchmarks(params)
      .then((resp) => {
        setData(resp);
        if (!hasInitializedRef.current || isDatasetChange) {
          hasInitializedRef.current = true;
          setEnabledRuns(new Set(resp.metrics.map((m) => m.run.id)));
        }
      })
      .catch((err: Error) => setError(err.message || 'Failed to load comparison'));
  }, [benchmark, split]);

  useEffect(() => {
    load();
  }, [load]);

  const handleOptionChange = (value: string) => {
    const [b, s] = value.split('|');
    const next = new URLSearchParams();
    next.set('benchmark_split', value);
    if (b) next.set('benchmark', b);
    if (s) next.set('split', s);
    setSearchParams(next);
  };

  const toggleRun = (id: string) => {
    setEnabledRuns((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  const toggleAll = () => {
    const allIds = (data?.metrics ?? []).map((m) => m.run.id);
    setEnabledRuns((prev) => (prev.size === allIds.length ? new Set() : new Set(allIds)));
  };

  const allMetrics = data?.metrics ?? [];
  const options = data?.options ?? [];
  const currentOptionValue = data ? `${data.benchmark}|${data.split}` : selectedOption;

  const availableWorkflows = useMemo(() => {
    const set = new Set<string>();
    for (const m of allMetrics) {
      if (enabledRuns.has(m.run.id)) set.add(m.run.workflow_id);
    }
    return Array.from(set).sort();
  }, [allMetrics, enabledRuns]);

  // Auto-reset control when current control workflow is no longer among checked runs.
  useEffect(() => {
    if (!controlWorkflow || availableWorkflows.length === 0) return;
    if (!availableWorkflows.includes(controlWorkflow)) {
      setControlWorkflow('');
    }
  }, [controlWorkflow, availableWorkflows]);

  // Recompute control + deltas on the frontend based on checked runs.
  // When controlWorkflow is '' (auto), pick the most accurate checked run as control.
  // When controlWorkflow is set, pick the first checked run matching that workflow.
  const { sortedAllMetrics, metrics } = useMemo(() => {
    const enabledMetrics = allMetrics.filter((m) => enabledRuns.has(m.run.id));

    // Find control run among checked runs
    let controlRun: (typeof allMetrics)[number] | undefined;
    if (controlWorkflow) {
      controlRun = enabledMetrics.find((m) => m.run.workflow_id === controlWorkflow);
    }
    if (!controlRun && enabledMetrics.length > 0) {
      // Auto: pick most accurate among checked runs
      controlRun = enabledMetrics.reduce((best, m) => (m.run.accuracy > best.run.accuracy ? m : best));
    }

    // Recompute is_control and deltas for all runs (including unchecked, for the table)
    const recomputed = allMetrics.map((m) => {
      const isControl = controlRun ? m.run.id === controlRun.run.id : false;
      let accuracyDelta: number | null = null;
      let parseRateDelta: number | null = null;
      let costDeltaPct: number | null = null;
      if (controlRun && !isControl) {
        accuracyDelta = m.run.accuracy - controlRun.run.accuracy;
        parseRateDelta = m.run.parse_rate - controlRun.run.parse_rate;
        if (controlRun.run.total_cost_usd > 0) {
          costDeltaPct = ((m.run.total_cost_usd - controlRun.run.total_cost_usd) / controlRun.run.total_cost_usd) * 100;
        }
      }
      return {
        ...m,
        is_control: isControl,
        accuracy_delta: accuracyDelta,
        parse_rate_delta: parseRateDelta,
        cost_delta_pct: costDeltaPct,
      };
    });

    // Sort: control first, then by accuracy descending
    const sorted = [...recomputed].sort((a, b) => {
      if (a.is_control !== b.is_control) return a.is_control ? -1 : 1;
      return b.run.accuracy - a.run.accuracy;
    });
    const filtered = sorted.filter((m) => enabledRuns.has(m.run.id));
    return { sortedAllMetrics: sorted, metrics: filtered };
  }, [allMetrics, enabledRuns, controlWorkflow]);

  // Item-level comparison: enabled when exactly 2 runs are checked.
  const enabledRunIds = useMemo(() => Array.from(enabledRuns), [enabledRuns]);
  const canCompareItems = enabledRunIds.length === 2;

  const handleCompareItems = useCallback(async () => {
    if (!canCompareItems) return;
    setItemCompareLoading(true);
    setItemComparison(null);
    try {
      const [baseId, candidateId] = enabledRunIds;
      const result = await adminClient.compareBenchmarkItems(baseId, candidateId);
      setItemComparison(result);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Item comparison failed');
    } finally {
      setItemCompareLoading(false);
    }
  }, [canCompareItems, enabledRunIds]);

  const bestAccuracy = useMemo(() => {
    if (metrics.length === 0) return 0;
    return Math.max(...metrics.map((m) => m.run.accuracy));
  }, [metrics]);

  const bestParseRate = useMemo(() => {
    if (metrics.length === 0) return 0;
    return Math.max(...metrics.map((m) => m.run.parse_rate));
  }, [metrics]);

  // Chart data
  const chartData = useMemo(() => {
    return metrics.map((m) => {
      const date = m.run.started_at
        ? new Date(m.run.started_at).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
        : '';
      return {
        label: date ? `${m.run.workflow_id} (${date})` : m.run.workflow_id,
        accuracy: m.run.accuracy * 100,
        cost: m.run.total_cost_usd,
        isControl: m.is_control,
      };
    });
  }, [metrics]);

  if (error) return <EmptyState message={error} />;
  if (!data) return <EmptyState message="Loading comparison..." />;

  return (
    <div className="space-y-4">
      <Breadcrumbs
        items={[
          { label: 'Admin', to: '/admin' },
          { label: 'Benchmarks', to: '/admin/benchmarks' },
          { label: 'Compare' },
        ]}
      />

      {/* Filter */}
      <Card>
        <CardContent className="flex flex-wrap items-end gap-3 pt-4">
          <div className="space-y-1">
            <span className="text-xs font-medium text-zinc-500">Benchmark / Split</span>
            <Select value={currentOptionValue} onChange={(e) => handleOptionChange(e.target.value)} className="w-72">
              {options.map((opt) => (
                <option key={`${opt.benchmark}|${opt.split}`} value={`${opt.benchmark}|${opt.split}`}>
                  {opt.benchmark} / {opt.split}
                </option>
              ))}
            </Select>
          </div>
          {availableWorkflows.length > 1 && (
            <div className="space-y-1">
              <span className="text-xs font-medium text-zinc-500">Control</span>
              <Select value={controlWorkflow} onChange={(e) => setControlWorkflow(e.target.value)} className="w-72">
                <option value="">Most accurate (auto)</option>
                {availableWorkflows.map((wf) => (
                  <option key={wf} value={wf}>
                    {wf}
                  </option>
                ))}
              </Select>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Run Selection */}
      {sortedAllMetrics.length > 0 && (
        <Card>
          <CardHeader className="flex flex-row items-center justify-between pb-2">
            <CardTitle className="text-sm">
              Select Runs ({enabledRuns.size}/{sortedAllMetrics.length})
            </CardTitle>
            <button
              type="button"
              onClick={toggleAll}
              className="text-xs text-sky-600 hover:text-sky-700 hover:underline"
            >
              {enabledRuns.size === sortedAllMetrics.length ? 'Deselect all' : 'Select all'}
            </button>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-10">
                    <input
                      type="checkbox"
                      checked={enabledRuns.size === sortedAllMetrics.length}
                      onChange={toggleAll}
                      className="rounded border-zinc-300"
                    />
                  </TableHead>
                  <TableHead>Workflow</TableHead>
                  <TableHead>Accuracy</TableHead>
                  <TableHead>Items</TableHead>
                  <TableHead>Cost</TableHead>
                  <TableHead>Started</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {sortedAllMetrics.map((m) => {
                  const checked = enabledRuns.has(m.run.id);
                  return (
                    <TableRow
                      key={m.run.id}
                      className={`cursor-pointer ${checked ? '' : 'opacity-40'}`}
                      onClick={() => toggleRun(m.run.id)}
                    >
                      <TableCell>
                        <input
                          type="checkbox"
                          checked={checked}
                          onChange={() => toggleRun(m.run.id)}
                          onClick={(e) => e.stopPropagation()}
                          className="rounded border-zinc-300"
                        />
                      </TableCell>
                      <TableCell>
                        <span className="font-mono text-xs">{m.run.workflow_id}</span>
                        {m.is_control && (
                          <Badge variant="info" className="ml-1.5">
                            control
                          </Badge>
                        )}
                      </TableCell>
                      <TableCell>{(m.run.accuracy * 100).toFixed(1)}%</TableCell>
                      <TableCell>
                        {m.run.correct_items}/{m.run.total_items}
                      </TableCell>
                      <TableCell>{formatCost(m.run.total_cost_usd)}</TableCell>
                      <TableCell className="text-xs text-zinc-500">
                        {m.run.started_at ? new Date(m.run.started_at).toLocaleString() : '-'}
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      {metrics.length > 0 ? (
        <>
          {/* Stats */}
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <StatCard label="Runs Compared" value={metrics.length} color="sky" />
            <StatCard label="Best Accuracy" value={`${(bestAccuracy * 100).toFixed(2)}%`} color="emerald" />
            <StatCard label="Best Parse Rate" value={`${(bestParseRate * 100).toFixed(2)}%`} color="amber" />
            <StatCard label="Dataset" value={`${data.benchmark} / ${data.split}`} color="purple" />
          </div>

          {/* Comparison Table */}
          <Card>
            <CardHeader>
              <CardTitle>Comparison Table</CardTitle>
              <p className="text-xs text-zinc-500">
                Control workflow marked with badge. Deltas shown relative to control.
              </p>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Workflow</TableHead>
                    <TableHead>Items</TableHead>
                    <TableHead>Accuracy</TableHead>
                    <TableHead>Parse Rate</TableHead>
                    <TableHead>Failed</TableHead>
                    <TableHead>Cost (USD)</TableHead>
                    <TableHead>Avg Cost/Item</TableHead>
                    <TableHead>P95 Latency</TableHead>
                    <TableHead>Retries</TableHead>
                    <TableHead>Elapsed</TableHead>
                    <TableHead>Started</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {metrics.map((m) => (
                    <TableRow key={m.run.id}>
                      <TableCell>
                        <Link
                          to={`/admin/benchmarks/${m.run.id}`}
                          className="font-mono text-xs text-sky-600 hover:underline"
                        >
                          {m.run.workflow_id}
                        </Link>
                        {m.is_control && (
                          <Badge variant="info" className="ml-1.5">
                            control
                          </Badge>
                        )}
                      </TableCell>
                      <TableCell>
                        {m.run.correct_items}/{m.run.total_items}
                      </TableCell>
                      <TableCell className={deltaCellBg(m.accuracy_delta, true)}>
                        <span>{(m.run.accuracy * 100).toFixed(2)}%</span>
                        {m.accuracy_delta != null && m.accuracy_delta !== 0 && (
                          <DeltaBadge value={m.accuracy_delta * 100} suffix="" higherBetter />
                        )}
                      </TableCell>
                      <TableCell className={deltaCellBg(m.parse_rate_delta, true)}>
                        <span>{(m.run.parse_rate * 100).toFixed(2)}%</span>
                        {m.parse_rate_delta != null && m.parse_rate_delta < 0 && (
                          <DeltaBadge value={m.parse_rate_delta * 100} suffix="" higherBetter />
                        )}
                      </TableCell>
                      <TableCell>
                        {m.run.failed_items > 0 ? <Badge variant="destructive">{m.run.failed_items}</Badge> : '0'}
                      </TableCell>
                      <TableCell className={deltaCellBg(m.cost_delta_pct, false)}>
                        <span>{formatCost(m.run.total_cost_usd)}</span>
                        {m.cost_delta_pct != null && Math.abs(m.cost_delta_pct) > 5 && (
                          <DeltaBadge value={m.cost_delta_pct} suffix="%" higherBetter={false} />
                        )}
                      </TableCell>
                      <TableCell>{formatCost(m.run.avg_cost_usd_per_item)}</TableCell>
                      <TableCell>{m.run.p95_latency_ms > 0 ? `${m.run.p95_latency_ms.toFixed(0)}ms` : '-'}</TableCell>
                      <TableCell>{m.run.total_non_letter_retries}</TableCell>
                      <TableCell>{m.run.elapsed_seconds > 0 ? `${m.run.elapsed_seconds.toFixed(0)}s` : '-'}</TableCell>
                      <TableCell className="text-xs text-zinc-500">
                        {m.run.started_at ? new Date(m.run.started_at).toLocaleString() : '-'}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>

          {/* Failure Breakdown */}
          <Card>
            <CardHeader>
              <CardTitle>Failure Breakdown</CardTitle>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Failure Class</TableHead>
                    {metrics.map((m) => (
                      <TableHead key={m.run.id} className="text-xs">
                        {m.run.workflow_id}
                      </TableHead>
                    ))}
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {FAILURE_REASONS.map((reason) => {
                    const hasAny = metrics.some((m) => (m.run.failure_reason_counts?.[reason] ?? 0) > 0);
                    return (
                      <TableRow key={reason} className={hasAny ? '' : 'text-zinc-400'}>
                        <TableCell className="font-mono text-xs">{reason}</TableCell>
                        {metrics.map((m) => {
                          const count = m.run.failure_reason_counts?.[reason] ?? 0;
                          return (
                            <TableCell key={m.run.id}>
                              {count > 0 ? <Badge variant="destructive">{count}</Badge> : '0'}
                            </TableCell>
                          );
                        })}
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            </CardContent>
          </Card>

          {/* Charts */}
          <Card>
            <CardHeader>
              <CardTitle>Charts</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="grid gap-6 lg:grid-cols-2">
                <BarChart
                  title="Accuracy by Workflow"
                  data={chartData.map((d) => ({ label: d.label, value: d.accuracy, isControl: d.isControl }))}
                  unit="%"
                />
                <BarChart
                  title="Total Cost by Workflow"
                  data={chartData.map((d) => ({ label: d.label, value: d.cost, isControl: d.isControl }))}
                  unit="$"
                />
              </div>
            </CardContent>
          </Card>

          {/* Item-Level Comparison */}
          <Card>
            <CardHeader className="flex flex-row items-center justify-between">
              <div>
                <CardTitle>Item-Level Comparison</CardTitle>
                <p className="text-xs text-zinc-500">
                  {canCompareItems
                    ? 'Compare individual items between the two selected runs.'
                    : 'Select exactly 2 runs above to compare individual items.'}
                </p>
              </div>
              <Button size="sm" disabled={!canCompareItems || itemCompareLoading} onClick={handleCompareItems}>
                {itemCompareLoading ? 'Loading...' : 'Compare Items'}
              </Button>
            </CardHeader>
            {itemComparison && (
              <CardContent className="space-y-4">
                <div className="grid gap-3 sm:grid-cols-4">
                  <StatCard label="Regressed" value={itemComparison.summary.regressed} color="red" />
                  <StatCard label="Improved" value={itemComparison.summary.improved} color="emerald" />
                  <StatCard label="Unchanged Correct" value={itemComparison.summary.unchanged_correct} color="sky" />
                  <StatCard label="Unchanged Wrong" value={itemComparison.summary.unchanged_wrong} color="amber" />
                </div>

                {itemComparison.regressions.length > 0 && (
                  <div>
                    <h4 className="mb-2 text-sm font-medium text-red-700">
                      Regressions ({itemComparison.regressions.length})
                    </h4>
                    <Table>
                      <TableHeader>
                        <TableRow>
                          <TableHead>Item ID</TableHead>
                          <TableHead>Subject</TableHead>
                          <TableHead>Expected</TableHead>
                          <TableHead>{itemComparison.base_workflow_id || 'Base'}</TableHead>
                          <TableHead>{itemComparison.candidate_workflow_id || 'Candidate'}</TableHead>
                          <TableHead>Failure</TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {itemComparison.regressions.map((item) => (
                          <TableRow key={item.item_id}>
                            <TableCell>
                              <Link
                                to={benchmarkItemDetailPath(itemComparison.candidate_run_id, item.item_id)}
                                className="font-mono text-xs text-sky-600 hover:underline"
                              >
                                {item.item_id}
                              </Link>
                            </TableCell>
                            <TableCell className="text-xs">{item.subject}</TableCell>
                            <TableCell>
                              <Badge variant="info">{item.answer_label}</Badge>
                            </TableCell>
                            <TableCell>
                              <Badge variant="success">{item.base_predicted || '-'}</Badge>
                            </TableCell>
                            <TableCell>
                              <Badge variant="destructive">{item.candidate_predicted || '-'}</Badge>
                            </TableCell>
                            <TableCell className="text-xs text-zinc-500">
                              {formatFailureLabel(item.candidate_failure_reason, item.candidate_category)}
                            </TableCell>
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
                  </div>
                )}

                {itemComparison.improvements.length > 0 && (
                  <div>
                    <h4 className="mb-2 text-sm font-medium text-emerald-700">
                      Improvements ({itemComparison.improvements.length})
                    </h4>
                    <Table>
                      <TableHeader>
                        <TableRow>
                          <TableHead>Item ID</TableHead>
                          <TableHead>Subject</TableHead>
                          <TableHead>Expected</TableHead>
                          <TableHead>{itemComparison.base_workflow_id || 'Base'}</TableHead>
                          <TableHead>{itemComparison.candidate_workflow_id || 'Candidate'}</TableHead>
                          <TableHead>Was</TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {itemComparison.improvements.map((item) => (
                          <TableRow key={item.item_id}>
                            <TableCell>
                              <Link
                                to={benchmarkItemDetailPath(itemComparison.candidate_run_id, item.item_id)}
                                className="font-mono text-xs text-sky-600 hover:underline"
                              >
                                {item.item_id}
                              </Link>
                            </TableCell>
                            <TableCell className="text-xs">{item.subject}</TableCell>
                            <TableCell>
                              <Badge variant="info">{item.answer_label}</Badge>
                            </TableCell>
                            <TableCell>
                              <Badge variant="destructive">{item.base_predicted || '-'}</Badge>
                            </TableCell>
                            <TableCell>
                              <Badge variant="success">{item.candidate_predicted || '-'}</Badge>
                            </TableCell>
                            <TableCell className="text-xs text-zinc-500">
                              {formatFailureLabel(item.base_failure_reason, item.base_category)}
                            </TableCell>
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
                  </div>
                )}

                {itemComparison.regressions.length === 0 && itemComparison.improvements.length === 0 && (
                  <p className="text-sm text-zinc-500">No regressions or improvements detected between these runs.</p>
                )}
              </CardContent>
            )}
          </Card>
        </>
      ) : (
        <Card>
          <CardContent className="py-8">
            <EmptyState message="No benchmark runs found for the selected dataset. Import benchmark artifacts first." />
          </CardContent>
        </Card>
      )}
    </div>
  );
}

function deltaCellBg(delta: number | null | undefined, higherBetter: boolean): string {
  if (delta == null || delta === 0) return '';
  const isPositive = delta > 0;
  const isGood = higherBetter ? isPositive : !isPositive;
  return isGood ? 'bg-emerald-50' : 'bg-red-50';
}

function DeltaBadge({ value, suffix, higherBetter }: { value: number; suffix: string; higherBetter: boolean }) {
  if (value === 0)
    return (
      <Badge variant="secondary" className="ml-1.5">
        0
      </Badge>
    );
  const isPositive = value > 0;
  const isGood = higherBetter ? isPositive : !isPositive;
  const sign = isPositive ? '+' : '';
  return (
    <Badge variant={isGood ? 'success' : 'destructive'} className="ml-1.5">
      {sign}
      {value.toFixed(2)}
      {suffix}
    </Badge>
  );
}

function BarChart({
  title,
  data,
  unit,
}: {
  title: string;
  data: Array<{ label: string; value: number; isControl: boolean }>;
  unit: string;
}) {
  const maxValue = Math.max(...data.map((d) => d.value), 1e-9);
  const barColors = [
    'bg-sky-500',
    'bg-emerald-500',
    'bg-amber-500',
    'bg-red-400',
    'bg-violet-500',
    'bg-pink-500',
    'bg-cyan-500',
    'bg-lime-500',
  ];

  return (
    <div className="space-y-3">
      <p className="text-sm font-medium text-zinc-700">{title}</p>
      <div className="space-y-2">
        {data.map((d, i) => {
          const widthPct = (d.value / maxValue) * 100;
          const color = barColors[i % barColors.length];
          return (
            <div key={d.label} className="space-y-0.5">
              <div className="flex items-center justify-between text-xs">
                <span className="truncate font-mono text-zinc-600">{d.label}</span>
                <span className="text-zinc-500">
                  {unit === '$' ? formatCost(d.value) : `${d.value.toFixed(2)}${unit}`}
                </span>
              </div>
              <div className="h-5 rounded bg-zinc-100">
                <div
                  className={`h-full rounded ${color} transition-all`}
                  style={{ width: `${Math.max(widthPct, 2)}%` }}
                />
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
