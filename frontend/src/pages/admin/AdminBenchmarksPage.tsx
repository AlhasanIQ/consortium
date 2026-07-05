import { type FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { CartesianGrid, Legend, Line, LineChart, Tooltip, XAxis, YAxis } from 'recharts';
import { type AdminBenchmarksResponse, adminClient, type BenchmarkRunnerStatus } from '@/api/adminClient';
import { Breadcrumbs } from '@/components/admin/Breadcrumbs';
import { EmptyState } from '@/components/admin/EmptyState';
import { LiveIndicator } from '@/components/admin/LiveIndicator';
import { SortableTableHead } from '@/components/admin/SortableTableHead';
import { StatusBadge } from '@/components/admin/StatusBadge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Progress } from '@/components/ui/progress';
import { Select } from '@/components/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { formatCost } from '@/lib/formatters';
import { usePolling } from '@/lib/usePolling';
import { applySortParams, parseSortParams, sortRows } from '@/lib/useSort';

type RunSet = 'full' | 'small' | 'custom';

export default function AdminBenchmarksPage() {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const [data, setData] = useState<AdminBenchmarksResponse | null>(null);
  const [error, setError] = useState('');
  const [runLoading, setRunLoading] = useState(false);
  const [cancelOpen, setCancelOpen] = useState(false);
  const [idExpanded, setIdExpanded] = useState(false);

  // Run form state
  const [selectedBenchmarks, setSelectedBenchmarks] = useState<Set<string>>(new Set(['global-mmlu-lite']));
  const [selectedWorkflows, setSelectedWorkflows] = useState<Set<string>>(
    new Set(['benchmark-informed-captain-synthesis-cheap']),
  );
  const [runSet, setRunSet] = useState<RunSet>('full');
  const [split, setSplit] = useState('');
  const [limit, setLimit] = useState('20');
  const [concurrency, setConcurrency] = useState('20');
  const [maxNonLetterRetries, setMaxNonLetterRetries] = useState('2');
  const [maxTransientRetries, setMaxTransientRetries] = useState('3');
  const wasRunningRef = useRef(false);
  const [pollForced, setPollForced] = useState(false);

  const toggleSelection = (set: Set<string>, value: string, setter: (s: Set<string>) => void) => {
    const next = new Set(set);
    if (next.has(value)) {
      next.delete(value);
    } else {
      next.add(value);
    }
    setter(next);
  };

  // Filter state from query params
  const filterBenchmark = searchParams.get('benchmark') || '';
  const filterWorkflow = searchParams.get('workflow') || '';
  const filterStatus = searchParams.get('status') || '';
  const filterSplit = searchParams.get('split') || '';
  const includeOptimizer =
    searchParams.get('include_optimizer') === '1' || searchParams.get('include_optimizer') === 'true';
  const sort = parseSortParams(searchParams, 'started', 'desc');

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
    const params: {
      benchmark?: string;
      workflow?: string;
      status?: string;
      split?: string;
      include_optimizer?: boolean;
    } = {};
    if (filterBenchmark) params.benchmark = filterBenchmark;
    if (filterWorkflow) params.workflow = filterWorkflow;
    if (filterStatus) params.status = filterStatus;
    if (filterSplit) params.split = filterSplit;
    if (includeOptimizer) params.include_optimizer = true;

    adminClient
      .listBenchmarks(params)
      .then((response) => {
        setData(response);
        wasRunningRef.current = response.Runner.running;
      })
      .catch((err: Error) => setError(err.message || 'Failed to load benchmark runs'));
  }, [filterBenchmark, filterWorkflow, filterStatus, filterSplit, includeOptimizer]);

  const pollRunnerStatus = useCallback(() => {
    adminClient
      .benchmarkRunnerStatus()
      .then((status: BenchmarkRunnerStatus) => {
        if ((wasRunningRef.current || pollForced) && !status.running) {
          wasRunningRef.current = false;
          setPollForced(false);
          load();
          return;
        }
        wasRunningRef.current = status.running;
        setData((prev) => {
          if (!prev) return prev;
          return {
            ...prev,
            Runner: {
              running: status.running,
              started_at: status.started_at,
              completed_runs: status.completed_runs,
              total_runs: status.total_runs,
              completed_items: status.completed_items,
              total_items: status.total_items,
              error: status.error,
              benchmarks: status.benchmarks,
              workflows: status.workflows,
              run_set: status.run_set,
              split: status.split,
              limit: status.limit,
              concurrency: status.concurrency,
              max_non_letter_retries: status.max_non_letter_retries,
              max_transient_retries: status.max_transient_retries,
            },
          };
        });
      })
      .catch(() => {});
  }, [load, pollForced]);

  useEffect(() => {
    load();
  }, [load]);

  // Auto-poll every 3s when runner is active or we just started a run
  usePolling(pollRunnerStatus, 3000, (data?.Runner.running ?? false) || pollForced);

  // Pre-fill the form from the running benchmark's config.
  const formSyncedRef = useRef(false);
  useEffect(() => {
    const r = data?.Runner;
    if (!r?.running || formSyncedRef.current) return;
    formSyncedRef.current = true;
    if (r.benchmarks?.length) setSelectedBenchmarks(new Set(r.benchmarks));
    if (r.workflows?.length) setSelectedWorkflows(new Set(r.workflows));
    if (r.run_set) setRunSet(r.run_set as RunSet);
    if (r.split) setSplit(r.split);
    if (r.limit != null) setLimit(String(r.limit));
    if (r.concurrency != null) setConcurrency(String(r.concurrency));
    if (r.max_non_letter_retries != null) setMaxNonLetterRetries(String(r.max_non_letter_retries));
    if (r.max_transient_retries != null) setMaxTransientRetries(String(r.max_transient_retries));
  }, [data?.Runner]);

  const handleSort = (column: string) => {
    applySortParams(searchParams, setSearchParams, column, sort);
  };

  const runs = data?.Runs ?? [];
  const runner = data?.Runner ?? { running: false };
  const progressValue = runner.completed_items ?? 0;
  const progressMax = runner.total_items ?? 0;
  const maxCost = useMemo(() => Math.max(...runs.map((r) => r.total_cost_usd), 1), [runs]);
  const topTokenRuns = useMemo(() => [...runs].sort((a, b) => b.total_tokens - a.total_tokens).slice(0, 5), [runs]);
  const sortedRuns = useMemo(
    () =>
      sortRows(runs, sort, {
        benchmark: (r) => r.benchmark,
        workflow: (r) => r.workflow_id,
        split: (r) => r.split,
        status: (r) => r.status,
        accuracy: (r) => r.accuracy,
        items: (r) => r.total_items,
        tokens: (r) => r.total_tokens,
        cost: (r) => r.total_cost_usd,
        started: (r) => (r.started_at ? new Date(r.started_at).getTime() : 0),
      }),
    [runs, sort],
  );

  const run = async (event: FormEvent) => {
    event.preventDefault();
    if (selectedBenchmarks.size === 0 || selectedWorkflows.size === 0) {
      setError('Select at least one benchmark and one workflow.');
      return;
    }
    setRunLoading(true);
    try {
      await adminClient.runBenchmarks({
        benchmarks: [...selectedBenchmarks],
        workflows: [...selectedWorkflows],
        run_set: runSet !== 'full' ? runSet : undefined,
        split: runSet === 'custom' && split ? split : undefined,
        limit: limit ? Number(limit) : undefined,
        concurrency: concurrency ? Number(concurrency) : undefined,
        max_non_letter_retries: maxNonLetterRetries ? Number(maxNonLetterRetries) : undefined,
        max_transient_retries: maxTransientRetries ? Number(maxTransientRetries) : undefined,
      });
      setPollForced(true);
      load();
    } catch (err) {
      setError((err as Error).message || 'Failed to start benchmark run');
    } finally {
      setRunLoading(false);
    }
  };

  const importArtifacts = async () => {
    await adminClient.importBenchmarks();
    load();
  };

  const confirmCancel = async () => {
    setCancelOpen(false);
    await adminClient.cancelBenchmarkRun();
    load();
  };

  if (error) return <EmptyState message={error} />;
  if (!data) return <EmptyState message="Loading benchmarks..." />;

  return (
    <div className="space-y-4">
      <Breadcrumbs items={[{ label: 'Admin', to: '/admin' }, { label: 'Benchmarks' }]} />

      <Card>
        <CardHeader className="flex flex-col gap-2 md:flex-row md:items-center md:justify-between">
          <div className="flex items-center gap-3">
            <CardTitle>Benchmark Runner</CardTitle>
            {runner.running && <LiveIndicator label="Running" />}
          </div>
          <div className="flex gap-2">
            <Link to={`/admin/benchmarks/compare${filterBenchmark ? `?benchmark=${filterBenchmark}` : ''}`}>
              <Button size="sm" variant="outline">
                Compare Runs
              </Button>
            </Link>
            <Button size="sm" variant="outline" onClick={importArtifacts}>
              Import Artifacts
            </Button>
            <Button size="sm" variant="destructive" onClick={() => setCancelOpen(true)} disabled={!runner.running}>
              Cancel Run
            </Button>
            <Button size="sm" variant="secondary" onClick={load}>
              Refresh
            </Button>
          </div>
        </CardHeader>
        <CardContent className="space-y-3">
          <p className="text-sm text-zinc-600">
            Status: <span className="font-medium">{runner.running ? 'Running' : 'Idle'}</span>
            {runner.total_items ? ` \u00B7 ${runner.total_items - (runner.completed_items || 0)} items left` : ''}
            {runner.total_runs ? ` \u00B7 ${runner.completed_runs || 0}/${runner.total_runs} runs` : ''}
          </p>

          {runner.running && progressMax > 0 && (
            <div className="space-y-1">
              <Progress value={progressValue} max={progressMax} />
              <p className="text-xs text-zinc-500">
                {progressValue}/{progressMax} items (
                {progressMax > 0 ? ((progressValue / progressMax) * 100).toFixed(1) : '0'}%)
              </p>
            </div>
          )}

          <form className="space-y-3" onSubmit={run}>
            <div>
              <p className="mb-1.5 text-xs font-medium text-zinc-500">Benchmarks</p>
              <div className="flex flex-wrap gap-1.5">
                {(data.AvailableBenchmarks ?? []).map((b) => (
                  <button
                    key={b}
                    type="button"
                    onClick={() => toggleSelection(selectedBenchmarks, b, setSelectedBenchmarks)}
                    className={`rounded-full border px-3 py-1 font-mono text-xs transition-colors ${
                      selectedBenchmarks.has(b)
                        ? 'border-sky-500 bg-sky-500/15 text-sky-600'
                        : 'border-zinc-200 bg-zinc-50 text-zinc-500 hover:border-zinc-300'
                    }`}
                  >
                    {b}
                  </button>
                ))}
                {(data.AvailableBenchmarks ?? []).length === 0 && (
                  <span className="text-xs text-zinc-400">No benchmarks available</span>
                )}
              </div>
            </div>
            <div>
              <p className="mb-1.5 text-xs font-medium text-zinc-500">Workflows</p>
              <div className="flex flex-wrap gap-1.5">
                {(data.AvailableWorkflows ?? []).map((w) => (
                  <button
                    key={w}
                    type="button"
                    onClick={() => toggleSelection(selectedWorkflows, w, setSelectedWorkflows)}
                    className={`rounded-full border px-3 py-1 font-mono text-xs transition-colors ${
                      selectedWorkflows.has(w)
                        ? 'border-sky-500 bg-sky-500/15 text-sky-600'
                        : 'border-zinc-200 bg-zinc-50 text-zinc-500 hover:border-zinc-300'
                    }`}
                  >
                    {w}
                  </button>
                ))}
                {(data.AvailableWorkflows ?? []).length === 0 && (
                  <span className="text-xs text-zinc-400">No benchmark workflows found</span>
                )}
              </div>
            </div>
            <div className="grid gap-3 md:grid-cols-6">
              <div className="space-y-1">
                <span className="text-xs font-medium text-zinc-500">Run Set</span>
                <Select value={runSet} onChange={(event) => setRunSet(event.target.value as RunSet)}>
                  <option value="full">Full</option>
                  <option value="small">Small</option>
                  <option value="custom">Custom</option>
                </Select>
              </div>
              {runSet === 'custom' && (
                <div className="space-y-1">
                  <span className="text-xs font-medium text-zinc-500">Split</span>
                  <Input value={split} onChange={(event) => setSplit(event.target.value)} placeholder="test" />
                </div>
              )}
              <div className="space-y-1">
                <span className="text-xs font-medium text-zinc-500">Limit</span>
                <Input value={limit} onChange={(event) => setLimit(event.target.value)} type="number" />
              </div>
              <div className="space-y-1">
                <span className="text-xs font-medium text-zinc-500">Concurrency</span>
                <Input value={concurrency} onChange={(event) => setConcurrency(event.target.value)} type="number" />
              </div>
              <div className="space-y-1">
                <span className="text-xs font-medium text-zinc-500">Non-letter retries</span>
                <Input
                  value={maxNonLetterRetries}
                  onChange={(event) => setMaxNonLetterRetries(event.target.value)}
                  type="number"
                />
              </div>
              <div className="space-y-1">
                <span className="text-xs font-medium text-zinc-500">Transient retries</span>
                <Input
                  value={maxTransientRetries}
                  onChange={(event) => setMaxTransientRetries(event.target.value)}
                  type="number"
                />
              </div>
            </div>
            <Button
              type="submit"
              disabled={runLoading || runner.running || selectedBenchmarks.size === 0 || selectedWorkflows.size === 0}
            >
              {runner.running ? 'Run in Progress...' : runLoading ? 'Starting...' : 'Start Run'}
            </Button>
          </form>
        </CardContent>
      </Card>

      {/* Filters — shared by chart and table */}
      <div className="flex flex-wrap items-center gap-2">
        <Select
          value={filterBenchmark}
          onChange={(event) => setParam('benchmark', event.target.value)}
          className="w-48"
        >
          <option value="">All benchmarks</option>
          {(data.Benchmarks ?? []).map((b) => (
            <option key={b} value={b}>
              {b}
            </option>
          ))}
        </Select>
        <Select value={filterWorkflow} onChange={(event) => setParam('workflow', event.target.value)} className="w-64">
          <option value="">All workflows</option>
          {(data.Workflows ?? []).map((w) => (
            <option key={w} value={w}>
              {w}
            </option>
          ))}
        </Select>
        <Select value={filterStatus} onChange={(event) => setParam('status', event.target.value)} className="w-40">
          <option value="">All statuses</option>
          {(data.Statuses ?? []).map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </Select>
        <Select value={filterSplit} onChange={(event) => setParam('split', event.target.value)} className="w-32">
          <option value="">All splits</option>
          {(data.Splits ?? []).map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </Select>
        <label className="flex items-center gap-2 rounded-md border border-zinc-200 px-3 py-1.5 text-xs text-zinc-700">
          <input
            type="checkbox"
            checked={includeOptimizer}
            onChange={(event) => setParam('include_optimizer', event.target.checked ? '1' : '')}
          />
          Include optimizer
        </label>
      </div>

      <div className="grid gap-4 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <CardContent className="pt-5">
            {(data?.ChartSeries ?? []).every((series) => (series.Points?.length ?? 0) === 0) ? (
              <p className="text-sm text-zinc-500">No runs yet.</p>
            ) : (
              <QualityTrendChart series={data?.ChartSeries ?? []} />
            )}
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Top Token Runs</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            {topTokenRuns.length === 0 ? (
              <p className="text-sm text-zinc-500">No runs yet.</p>
            ) : (
              topTokenRuns.map((runRow) => {
                const width = (runRow.total_cost_usd / maxCost) * 100;
                return (
                  <div key={`top-${runRow.id}`} className="space-y-1">
                    <div className="flex items-center justify-between gap-2 text-xs">
                      <span className="truncate font-mono text-zinc-700">{runRow.id}</span>
                      <span className="text-zinc-500">{runRow.total_tokens.toLocaleString()} tok</span>
                    </div>
                    <div className="h-2 rounded-full bg-zinc-100">
                      <div className="h-full rounded-full bg-sky-500/80" style={{ width: `${Math.max(width, 4)}%` }} />
                    </div>
                    <div className="text-[11px] text-zinc-500">
                      {(runRow.accuracy * 100).toFixed(1)}% acc · {formatCost(runRow.total_cost_usd)}
                    </div>
                  </div>
                );
              })
            )}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Runs</CardTitle>
        </CardHeader>
        <CardContent>
          {runs.length === 0 ? (
            <EmptyState message="No benchmark runs found." />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>
                    <span className="flex items-center gap-1">
                      ID
                      <button
                        type="button"
                        onClick={() => setIdExpanded((v) => !v)}
                        className="rounded px-1 text-[10px] text-zinc-400 hover:bg-zinc-100 hover:text-zinc-600"
                        title={idExpanded ? 'Collapse IDs' : 'Expand IDs'}
                      >
                        {idExpanded ? '\u25C0' : '\u25B6'}
                      </button>
                    </span>
                  </TableHead>
                  <SortableTableHead column="benchmark" label="Benchmark" sort={sort} onSort={handleSort} />
                  <SortableTableHead column="split" label="Split" sort={sort} onSort={handleSort} />
                  <SortableTableHead column="workflow" label="Workflow" sort={sort} onSort={handleSort} />
                  <SortableTableHead column="status" label="Status" sort={sort} onSort={handleSort} />
                  <SortableTableHead column="accuracy" label="Accuracy" sort={sort} onSort={handleSort} />
                  <SortableTableHead column="items" label="Items" sort={sort} onSort={handleSort} />
                  <SortableTableHead column="tokens" label="Tokens" sort={sort} onSort={handleSort} />
                  <SortableTableHead column="cost" label="Cost" sort={sort} onSort={handleSort} />
                  <SortableTableHead column="started" label="Started" sort={sort} onSort={handleSort} />
                </TableRow>
              </TableHeader>
              <TableBody>
                {sortedRuns.map((runRow) => (
                  <TableRow
                    key={runRow.id}
                    className="cursor-pointer"
                    onClick={() => navigate(`/admin/benchmarks/${runRow.id}`)}
                  >
                    <TableCell className="font-mono text-xs">
                      <span className={idExpanded ? 'break-all' : 'block max-w-[80px] truncate'}>{runRow.id}</span>
                    </TableCell>
                    <TableCell>{runRow.benchmark}</TableCell>
                    <TableCell>{runRow.split}</TableCell>
                    <TableCell className="max-w-[280px] truncate">{runRow.workflow_id}</TableCell>
                    <TableCell>
                      <StatusBadge status={runRow.status} />
                    </TableCell>
                    <TableCell>{(runRow.accuracy * 100).toFixed(1)}%</TableCell>
                    <TableCell>
                      {runRow.correct_items}/{runRow.total_items}
                    </TableCell>
                    <TableCell>{runRow.total_tokens.toLocaleString()}</TableCell>
                    <TableCell>{formatCost(runRow.total_cost_usd)}</TableCell>
                    <TableCell className="text-xs text-zinc-500">
                      {runRow.started_at ? new Date(runRow.started_at).toLocaleString() : '-'}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <Dialog open={cancelOpen} onOpenChange={setCancelOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Cancel benchmark run?</DialogTitle>
            <DialogDescription>
              This will stop the currently running benchmark. Items already completed will be preserved.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <DialogClose asChild>
              <Button variant="outline">Keep running</Button>
            </DialogClose>
            <Button variant="destructive" onClick={confirmCancel}>
              Cancel run
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

// --- Chart types ---

type ChartPoint = {
  Split: string;
  Points: Array<{
    RunID: string;
    WorkflowID: string;
    Benchmark: string;
    Accuracy: number;
    CostUSD: number;
    TotalItems: number;
    Date: string;
  }>;
};

type TimeRange = '1h' | '1d' | '3d' | '7d' | '14d' | '30d' | 'all';

const RANGE_MS: Record<TimeRange, number> = {
  '1h': 3_600_000,
  '1d': 86_400_000,
  '3d': 3 * 86_400_000,
  '7d': 7 * 86_400_000,
  '14d': 14 * 86_400_000,
  '30d': 30 * 86_400_000,
  all: 0,
};

// --- Quality Trend Chart ---

function QualityTrendChart({ series }: { series: ChartPoint[] }) {
  const navigate = useNavigate();
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [chartSize, setChartSize] = useState<{ width: number; height: number }>({ width: 0, height: 0 });
  const [range, setRange] = useState<TimeRange>('7d');

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const observer = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (!entry) return;
      const width = Math.max(0, Math.floor(entry.contentRect.width));
      const height = Math.max(0, Math.floor(entry.contentRect.height));
      setChartSize((prev) => {
        if (prev.width === width && prev.height === height) return prev;
        return { width, height };
      });
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  const chartData = useMemo(() => {
    const cutoff = RANGE_MS[range] > 0 ? Date.now() - RANGE_MS[range] : 0;

    const rows: Array<{
      timestamp: number;
      test_accuracy?: number;
      dev_accuracy?: number;
      test_run_id?: string;
      test_workflow_id?: string;
      test_benchmark?: string;
      test_cost_usd?: number;
      test_total_items?: number;
      dev_run_id?: string;
      dev_workflow_id?: string;
      dev_benchmark?: string;
      dev_cost_usd?: number;
      dev_total_items?: number;
    }> = [];

    for (const splitSeries of series) {
      const sp = (splitSeries.Split || '').toLowerCase();
      if (sp !== 'test' && sp !== 'dev') continue;

      for (let i = 0; i < splitSeries.Points.length; i += 1) {
        const point = splitSeries.Points[i];
        const parsed = new Date(point.Date).getTime();
        const baseTS = Number.isFinite(parsed) ? parsed : Date.now();
        const timestamp = baseTS + i; // tiebreak for same-second entries

        if (cutoff > 0 && timestamp < cutoff) continue;

        if (sp === 'test') {
          rows.push({
            timestamp,
            test_accuracy: point.Accuracy * 100,
            test_run_id: point.RunID,
            test_workflow_id: point.WorkflowID,
            test_benchmark: point.Benchmark,
            test_cost_usd: point.CostUSD,
            test_total_items: point.TotalItems,
          });
        } else {
          rows.push({
            timestamp,
            dev_accuracy: point.Accuracy * 100,
            dev_run_id: point.RunID,
            dev_workflow_id: point.WorkflowID,
            dev_benchmark: point.Benchmark,
            dev_cost_usd: point.CostUSD,
            dev_total_items: point.TotalItems,
          });
        }
      }
    }

    rows.sort((a, b) => a.timestamp - b.timestamp);
    return rows.map((r, i) => ({ ...r, idx: i }));
  }, [series, range]);

  // Auto-widen range if default 7d shows nothing but there is data beyond.
  const allPointCount = useMemo(() => series.reduce((n, s) => n + (s.Points?.length ?? 0), 0), [series]);
  useEffect(() => {
    if (chartData.length === 0 && allPointCount > 0 && range === '7d') {
      setRange('all');
    }
  }, [chartData.length, allPointCount, range]);

  if (chartData.length === 0) {
    return <p className="text-sm text-zinc-500">No chart data.</p>;
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <span className="text-sm font-medium text-zinc-700">Run Quality Trend</span>
        <div className="flex gap-1">
          {(['1h', '1d', '3d', '7d', '14d', '30d', 'all'] as TimeRange[]).map((r) => (
            <button
              key={r}
              type="button"
              onClick={() => setRange(r)}
              className={`rounded px-2 py-0.5 text-xs transition-colors ${
                range === r ? 'bg-zinc-800 text-white' : 'bg-zinc-100 text-zinc-500 hover:bg-zinc-200'
              }`}
            >
              {r === 'all' ? 'All' : r}
            </button>
          ))}
        </div>
      </div>
      <div ref={containerRef} className="h-[260px] w-full min-w-0">
        {chartSize.width > 0 && chartSize.height > 0 ? (
          <LineChart
            width={chartSize.width}
            height={chartSize.height}
            data={chartData}
            margin={{ top: 8, right: 8, left: 0, bottom: 0 }}
            onClick={(state: unknown) => {
              const chartState = state as
                | {
                    activePayload?: Array<{
                      payload?: {
                        test_run_id?: string;
                        dev_run_id?: string;
                      };
                    }>;
                  }
                | undefined;
              const payload = chartState?.activePayload?.[0]?.payload as
                | {
                    test_run_id?: string;
                    dev_run_id?: string;
                  }
                | undefined;
              const runID = payload?.test_run_id || payload?.dev_run_id;
              if (runID) {
                navigate(`/admin/benchmarks/${runID}`);
              }
            }}
          >
            <CartesianGrid stroke="#e4e4e7" strokeDasharray="3 3" />
            <XAxis
              dataKey="idx"
              interval="preserveStartEnd"
              tickFormatter={(idx: number) => {
                const row = chartData[idx];
                if (!row) return '';
                const d = new Date(row.timestamp);
                return `${d.getMonth() + 1}/${d.getDate()} ${d.getHours().toString().padStart(2, '0')}:${d.getMinutes().toString().padStart(2, '0')}`;
              }}
              tick={{ fontSize: 10 }}
            />
            <YAxis
              domain={['auto', 'auto']}
              tickFormatter={(value) => `${Number(value).toFixed(1)}%`}
              tick={{ fontSize: 11 }}
            />
            <Tooltip content={<ChartTooltip />} />
            <Legend />
            <Line
              type="monotone"
              dataKey="test_accuracy"
              name="test (full)"
              stroke="#10b981"
              strokeWidth={2}
              dot={{ r: 3 }}
              connectNulls
            />
            <Line
              type="monotone"
              dataKey="dev_accuracy"
              name="dev (full)"
              stroke="#0ea5e9"
              strokeWidth={2}
              strokeDasharray="6 4"
              dot={{ r: 3 }}
              connectNulls
            />
          </LineChart>
        ) : null}
      </div>
      <p className="text-xs text-zinc-500">
        Completed full-item runs for dev/test splits (partial runs excluded). Click a point to view details.
      </p>
    </div>
  );
}

// --- Custom tooltip ---

function ChartTooltip({
  active,
  payload,
}: {
  active?: boolean;
  payload?: Array<{ payload?: Record<string, unknown> }>;
}) {
  if (!active || !payload?.length) return null;

  const row = payload[0]?.payload;
  if (!row) return null;

  const ts = row.timestamp ? new Date(row.timestamp as number).toLocaleString() : '';
  const testAcc = row.test_accuracy as number | undefined;
  const devAcc = row.dev_accuracy as number | undefined;
  const benchmark = (row.test_benchmark || row.dev_benchmark) as string | undefined;
  const workflow = (row.test_workflow_id || row.dev_workflow_id) as string | undefined;
  const cost = (row.test_cost_usd ?? row.dev_cost_usd) as number | undefined;
  const items = (row.test_total_items ?? row.dev_total_items) as number | undefined;

  return (
    <div className="rounded-lg border border-zinc-200 bg-white px-3 py-2 text-xs shadow-md">
      <p className="mb-1 text-zinc-500">{ts}</p>
      {testAcc != null && (
        <p>
          <span className="mr-1.5 inline-block h-2 w-2 rounded-full bg-emerald-500" />
          test: <span className="font-medium">{testAcc.toFixed(2)}%</span>
        </p>
      )}
      {devAcc != null && (
        <p>
          <span className="mr-1.5 inline-block h-2 w-2 rounded-full bg-sky-500" />
          dev: <span className="font-medium">{devAcc.toFixed(2)}%</span>
        </p>
      )}
      {benchmark && <p className="mt-1 text-zinc-500">{benchmark}</p>}
      {workflow && (
        <p className="truncate text-zinc-500" style={{ maxWidth: 240 }}>
          {workflow}
        </p>
      )}
      <div className="mt-1 flex gap-3 text-zinc-400">
        {cost != null && <span>{formatCost(cost)}</span>}
        {items != null && <span>{items} items</span>}
      </div>
    </div>
  );
}
