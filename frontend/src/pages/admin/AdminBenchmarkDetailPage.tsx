import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { type AdminBenchmarkDetailResponse, adminClient, type BenchmarkRunnerStatus } from '@/api/adminClient';
import { Breadcrumbs } from '@/components/admin/Breadcrumbs';
import { EmptyState } from '@/components/admin/EmptyState';
import { StatCard } from '@/components/admin/StatCard';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import {
  Pagination,
  PaginationContent,
  PaginationItem,
  PaginationNext,
  PaginationPrevious,
} from '@/components/ui/pagination';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { benchmarkItemDetailPath } from '@/lib/adminPaths';
import { durationLabel, formatCost } from '@/lib/formatters';

export default function AdminBenchmarkDetailPage() {
  const { id = '' } = useParams();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const [data, setData] = useState<AdminBenchmarkDetailResponse | null>(null);
  const [error, setError] = useState('');
  const [rerunning, setRerunning] = useState(false);
  const [runnerBusy, setRunnerBusy] = useState(false);
  const [failureView, setFailureView] = useState<'final' | 'all_attempts'>('final');

  // Poll runner status to disable rerun button when active.
  useEffect(() => {
    let cancelled = false;
    const check = () => {
      adminClient
        .benchmarkRunnerStatus()
        .then((s: BenchmarkRunnerStatus) => {
          if (!cancelled) setRunnerBusy(s.running);
        })
        .catch(() => {});
    };
    check();
    const interval = setInterval(check, 5000);
    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, []);

  const handleRerunFailures = useCallback(async () => {
    if (!data) return;
    const incorrectCount = data.Run.total_items - data.Run.correct_items;
    if (!window.confirm(`Rerun ${incorrectCount} failed/incorrect items and merge results back into this run?`)) return;
    setRerunning(true);
    try {
      await adminClient.rerunBenchmarkFailures(id);
      setRerunning(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Rerun failed');
      setRerunning(false);
    }
  }, [data, id]);

  const page = Number(searchParams.get('page') || '1');
  const pageSize = Number(searchParams.get('page_size') || '100');
  const incorrect = searchParams.get('incorrect') === '1';
  const subject = searchParams.get('subject') || '';
  const failureReason = searchParams.get('failure_reason') || '';

  useEffect(() => {
    if (!id) return;
    adminClient
      .getBenchmark(id, {
        page,
        page_size: pageSize,
        incorrect: incorrect ? '1' : '',
        subject,
        failure_reason: failureReason,
      })
      .then((response) => setData(response))
      .catch((err: Error) => setError(err.message || 'Failed to load benchmark detail'));
  }, [id, page, pageSize, incorrect, subject, failureReason]);

  const setParam = (key: string, value: string) => {
    const next = new URLSearchParams(searchParams);
    if (value) {
      next.set(key, value);
    } else {
      next.delete(key);
    }
    if (key !== 'page') {
      next.set('page', '1');
    }
    setSearchParams(next);
  };

  const items = data?.Items ?? [];

  const parseRate = useMemo(() => {
    if (!data) return 0;
    if (typeof data.Run.parse_rate === 'number') return data.Run.parse_rate;
    if (!items.length) return 0;
    const parsed = items.filter((item) => item.parse_ok).length;
    return parsed / items.length;
  }, [data, items]);

  const finalFailureBreakdown = useMemo((): Array<[string, number]> => {
    const labels = data?.FinalFailureLabels ?? [];
    const series = data?.FinalFailureSeries ?? [];
    if (labels.length > 0 && labels.length === series.length) {
      return labels
        .map((label, i) => [label, series[i]] as [string, number])
        .filter(([, count]) => count > 0)
        .sort((a, b) => b[1] - a[1]);
    }
    // Fallback to Run-level item counts
    const counts = data?.Run.failure_reason_counts ?? {};
    return Object.entries(counts)
      .filter(([, count]) => count > 0)
      .sort((a, b) => b[1] - a[1]);
  }, [data]);

  const allAttemptsFailureBreakdown = useMemo((): Array<[string, number]> => {
    const labels = data?.AllAttemptsFailureLabels ?? [];
    const series = data?.AllAttemptsFailureSeries ?? [];
    if (labels.length > 0 && labels.length === series.length) {
      return labels
        .map((label, i) => [label, series[i]] as [string, number])
        .filter(([, count]) => count > 0)
        .sort((a, b) => b[1] - a[1]);
    }
    // Fallback to Run-level attempt counts
    const counts = data?.Run.all_attempt_failure_counts ?? {};
    return Object.entries(counts)
      .filter(([, count]) => count > 0)
      .sort((a, b) => b[1] - a[1]);
  }, [data]);

  const failureBreakdown = failureView === 'final' ? finalFailureBreakdown : allAttemptsFailureBreakdown;

  const retryPressure = useMemo(() => {
    const totalAttempts = data?.Run.total_attempts ?? 0;
    const totalItems = data?.Run.total_items ?? 0;
    const extraAttempts = Math.max(totalAttempts - totalItems, 0);
    return {
      totalAttempts,
      totalItems,
      extraAttempts,
      retriesPerItem: totalItems > 0 ? extraAttempts / totalItems : 0,
      nonLetterRetries: data?.Run.total_non_letter_retries ?? 0,
    };
  }, [data]);

  if (error) return <EmptyState message={error} />;
  if (!data) return <EmptyState message="Loading benchmark run..." />;

  const run = data.Run;

  return (
    <div className="space-y-4">
      <Breadcrumbs
        items={[
          { label: 'Admin', to: '/admin' },
          { label: 'Benchmarks', to: '/admin/benchmarks' },
          { label: id.slice(0, 12) },
        ]}
      />

      <Card>
        <CardHeader className="flex flex-row items-start justify-between gap-4">
          <div>
            <CardTitle className="font-mono text-sm">{run.id}</CardTitle>
            <p className="text-sm text-zinc-500">
              {run.benchmark}/{run.split} &middot; {run.workflow_id} &middot; {run.status}
            </p>
          </div>
          <div className="flex gap-2">
            {run.total_items - run.correct_items > 0 && (
              <>
                <Button size="sm" variant="outline" onClick={() => navigate(`/admin/benchmarks/${id}/analysis`)}>
                  Analyze Wrong Answers
                </Button>
                <Button size="sm" variant="outline" disabled={rerunning || runnerBusy} onClick={handleRerunFailures}>
                  {rerunning ? 'Retrying...' : `Retry & merge ${run.total_items - run.correct_items} failures`}
                </Button>
              </>
            )}
          </div>
        </CardHeader>
      </Card>

      {data.Optimization && (
        <Card className="border-sky-200 bg-sky-50/40">
          <CardContent className="flex flex-col gap-3 pt-5 md:flex-row md:items-center md:justify-between">
            <div>
              <p className="text-sm font-semibold text-sky-900">Part of optimization run</p>
              <p className="text-sm text-sky-800">
                Run <span className="font-mono">{data.Optimization.run_id}</span> · Organism{' '}
                <span className="font-mono">{data.Optimization.organism_id}</span>
              </p>
            </div>
            <div className="flex gap-2">
              <Button asChild size="sm" variant="outline">
                <Link to={`/admin/optimize/${data.Optimization.run_id}`}>View Optimization Run</Link>
              </Button>
              <Button asChild size="sm" variant="outline">
                <Link to={`/admin/optimize/${data.Optimization.run_id}/organisms/${data.Optimization.organism_id}`}>
                  View Organism
                </Link>
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      <div className="grid gap-4 md:grid-cols-4 xl:grid-cols-7">
        <StatCard
          label="Accuracy"
          value={`${(run.accuracy * 100).toFixed(2)}%`}
          hint={`${run.correct_items}/${run.total_items} correct`}
          color="emerald"
        />
        <StatCard
          label="Parse Rate"
          value={`${(parseRate * 100).toFixed(1)}%`}
          hint="Items with valid parse"
          color="sky"
        />
        <StatCard label="Correct" value={run.correct_items} color="emerald" />
        <StatCard label="Incorrect" value={run.total_items - run.correct_items} color="red" />
        <StatCard label="Total Items" value={run.total_items} color="teal" />
        <StatCard label="Tokens" value={run.total_tokens.toLocaleString()} color="sky" />
        <StatCard label="Cost" value={formatCost(run.total_cost_usd)} color="amber" />
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between gap-4">
            <CardTitle>Failure Mix</CardTitle>
            <div className="flex rounded-md border border-zinc-200 text-xs">
              <button
                type="button"
                className={`px-3 py-1 rounded-l-md transition-colors ${failureView === 'final' ? 'bg-zinc-900 text-white' : 'text-zinc-600 hover:bg-zinc-100'}`}
                onClick={() => setFailureView('final')}
              >
                Final outcomes
              </button>
              <button
                type="button"
                className={`px-3 py-1 rounded-r-md transition-colors ${failureView === 'all_attempts' ? 'bg-zinc-900 text-white' : 'text-zinc-600 hover:bg-zinc-100'}`}
                onClick={() => setFailureView('all_attempts')}
              >
                All attempts
              </button>
            </div>
          </CardHeader>
          <CardContent>
            {failureBreakdown.length === 0 ? (
              <p className="text-sm text-zinc-500">
                {failureView === 'final'
                  ? 'No final failures recorded for this run.'
                  : 'No attempt failures recorded for this run.'}
              </p>
            ) : (
              <BarBreakdownChart data={failureBreakdown} />
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Retry Pressure</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <MiniMetric
              label="Total Attempts"
              value={retryPressure.totalAttempts.toLocaleString()}
              hint={`${retryPressure.totalItems.toLocaleString()} items`}
            />
            <MiniMetric
              label="Extra Attempts"
              value={retryPressure.extraAttempts.toLocaleString()}
              hint={`${retryPressure.retriesPerItem.toFixed(2)} retries/item`}
            />
            <MiniMetric
              label="Non-letter Retries"
              value={retryPressure.nonLetterRetries.toLocaleString()}
              hint="Contract formatting recovery"
            />
            <div className="grid grid-cols-3 gap-2 pt-1">
              <Pill label="P50" value={durationLabel(run.p50_latency_ms ?? 0)} />
              <Pill label="P95" value={durationLabel(run.p95_latency_ms ?? 0)} />
              <Pill label="P99" value={durationLabel(run.p99_latency_ms ?? 0)} />
            </div>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
          <CardTitle>Items</CardTitle>
          <div className="flex flex-wrap gap-2">
            <Input
              placeholder="Subject"
              value={subject}
              onChange={(event) => setParam('subject', event.target.value)}
              className="w-40"
            />
            <Input
              placeholder="Failure reason"
              value={failureReason}
              onChange={(event) => setParam('failure_reason', event.target.value)}
              className="w-48"
            />
            <Button
              size="sm"
              variant={incorrect ? 'default' : 'outline'}
              onClick={() => setParam('incorrect', incorrect ? '' : '1')}
            >
              Incorrect only
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          {items.length === 0 ? (
            <EmptyState message="No items for current filter." />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Item ID</TableHead>
                  <TableHead>Subject</TableHead>
                  <TableHead>Gold</TableHead>
                  <TableHead>Predicted</TableHead>
                  <TableHead>Correct</TableHead>
                  <TableHead>Attempts</TableHead>
                  <TableHead>Failure</TableHead>
                  <TableHead>Job</TableHead>
                  <TableHead>Tokens</TableHead>
                  <TableHead>Cost</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((item) => (
                  <TableRow
                    key={item.item_id}
                    className="cursor-pointer"
                    onClick={() => navigate(benchmarkItemDetailPath(id, item.item_id))}
                  >
                    <TableCell className="font-mono text-xs">{item.item_id}</TableCell>
                    <TableCell>{item.subject}</TableCell>
                    <TableCell>{item.answer_label}</TableCell>
                    <TableCell>{item.predicted || '-'}</TableCell>
                    <TableCell>
                      {item.correct ? (
                        <span className="inline-flex items-center gap-1 rounded-full bg-emerald-50 px-2 py-0.5 text-xs font-medium text-emerald-700">
                          <svg className="h-3 w-3" viewBox="0 0 12 12" fill="none" role="img" aria-label="Correct">
                            <path
                              d="M2.5 6l2.5 2.5 4.5-5"
                              stroke="currentColor"
                              strokeWidth="1.5"
                              strokeLinecap="round"
                              strokeLinejoin="round"
                            />
                          </svg>
                          correct
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-1 rounded-full bg-red-50 px-2 py-0.5 text-xs font-medium text-red-700">
                          <svg className="h-3 w-3" viewBox="0 0 12 12" fill="none" role="img" aria-label="Wrong">
                            <path d="M3 3l6 6M9 3l-6 6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
                          </svg>
                          wrong
                        </span>
                      )}
                    </TableCell>
                    <TableCell>{item.attempts ?? 1}</TableCell>
                    <TableCell className="max-w-[180px] truncate text-xs text-zinc-500">
                      {item.failure_reason || '-'}
                    </TableCell>
                    <TableCell className="font-mono text-xs">{item.job_id ? item.job_id.slice(0, 8) : '-'}</TableCell>
                    <TableCell>{item.total_tokens.toLocaleString()}</TableCell>
                    <TableCell>{formatCost(item.cost_usd)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}

          <div className="mt-3 flex items-center justify-between">
            <p className="text-muted-foreground text-xs">Total items: {data.TotalItems}</p>
            <Pagination className="mx-0 w-auto justify-end">
              <PaginationContent>
                <PaginationItem>
                  <PaginationPrevious
                    href="#"
                    className={!data.HasPrev ? 'pointer-events-none opacity-50' : ''}
                    onClick={(event) => {
                      event.preventDefault();
                      if (data.HasPrev) setParam('page', String(page - 1));
                    }}
                  />
                </PaginationItem>
                <PaginationItem>
                  <PaginationNext
                    href="#"
                    className={!data.HasNext ? 'pointer-events-none opacity-50' : ''}
                    onClick={(event) => {
                      event.preventDefault();
                      if (data.HasNext) setParam('page', String(page + 1));
                    }}
                  />
                </PaginationItem>
              </PaginationContent>
            </Pagination>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}

function BarBreakdownChart({ data }: { data: Array<[string, number]> }) {
  const max = Math.max(...data.map(([, v]) => v), 1);
  return (
    <div className="space-y-2">
      {data.map(([label, value]) => {
        const width = (value / max) * 100;
        return (
          <div key={label} className="space-y-1">
            <div className="flex items-center justify-between gap-2 text-xs">
              <span className="truncate font-medium text-zinc-700">{label.replace(/_/g, ' ')}</span>
              <span className="text-zinc-500">{value}</span>
            </div>
            <div className="h-2 rounded-full bg-zinc-100">
              <div className="h-full rounded-full bg-rose-500/80" style={{ width: `${width}%` }} />
            </div>
          </div>
        );
      })}
    </div>
  );
}

function MiniMetric({ label, value, hint }: { label: string; value: string; hint: string }) {
  return (
    <div className="rounded-md border border-zinc-200 bg-zinc-50 px-3 py-2">
      <div className="text-[11px] uppercase tracking-wide text-zinc-500">{label}</div>
      <div className="text-base font-semibold text-zinc-900">{value}</div>
      <div className="text-xs text-zinc-500">{hint}</div>
    </div>
  );
}

function Pill({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-full border border-zinc-200 px-3 py-1 text-center">
      <div className="text-[10px] uppercase tracking-wide text-zinc-500">{label}</div>
      <div className="text-xs font-medium text-zinc-800">{value}</div>
    </div>
  );
}
