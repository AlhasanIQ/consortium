import { ExternalLink, Loader2, Square } from 'lucide-react';
import { Fragment, useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { type AdminJobDetailResponse, type AgentRunSummary, adminClient } from '@/api/adminClient';
import { Breadcrumbs } from '@/components/admin/Breadcrumbs';
import { EmptyState } from '@/components/admin/EmptyState';
import { NodeTraceList } from '@/components/admin/NodeTraceCard';
import { StatCard } from '@/components/admin/StatCard';
import { StatusBadge } from '@/components/admin/StatusBadge';
import { TimelineChart } from '@/components/admin/TimelineChart';
import { Badge } from '@/components/ui/badge';
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
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { durationLabel, formatCost, isNodeReplayed, relativeTime } from '@/lib/formatters';
import { usePolling } from '@/lib/usePolling';
import {
  adminActionErrorMessage,
  agentRunKindLabel,
  agentRunNovomoIDLinks,
  canStopAgentRun,
  enrichNodesWithChildJobIDs,
} from './adminJobDetailUtils';

export default function AdminJobDetailPage() {
  const { id = '' } = useParams();
  const navigate = useNavigate();
  const [data, setData] = useState<AdminJobDetailResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [actionError, setActionError] = useState('');
  const [actionMessage, setActionMessage] = useState('');
  const [runningActionKey, setRunningActionKey] = useState<string | null>(null);
  const [confirmAction, setConfirmAction] = useState<{
    title: string;
    description: string;
    action: () => Promise<unknown>;
    variant: 'destructive' | 'default';
    actionKey?: string;
    successMessage?: string;
  } | null>(null);

  const load = useCallback(() => {
    if (!id) return;
    adminClient
      .getJob(id)
      .then((response) => setData(response))
      .catch((err: Error) => setError(err.message || 'Failed to load job'))
      .finally(() => setLoading(false));
  }, [id]);

  useEffect(() => {
    setLoading(true);
    load();
  }, [load]);

  const isActive = data?.Job.status === 'running' || data?.Job.status === 'pending';
  usePolling(load, 2000, isActive);

  const runAction = async (action: NonNullable<typeof confirmAction>) => {
    setActionError('');
    setActionMessage('');
    setRunningActionKey(action.actionKey ?? 'action');
    try {
      await action.action();
      setConfirmAction(null);
      setActionMessage(action.successMessage ?? 'Action completed.');
      load();
    } catch (err) {
      setActionError(adminActionErrorMessage(err));
    } finally {
      setRunningActionKey(null);
    }
  };

  const attempts = data?.Attempts ?? [];
  const childJobs = data?.ChildJobs ?? [];
  const agentRuns = data?.AgentRuns ?? [];
  const nodes = useMemo(
    () => enrichNodesWithChildJobIDs(data?.NodesWithMetrics ?? [], childJobs),
    [data?.NodesWithMetrics, childJobs],
  );
  const lifecycle = data?.Lifecycle ?? {
    QueueDelayMs: 0,
    ExecutionDurationMs: 0,
    ActiveAttemptDurationMs: 0,
    IdleDurationMs: 0,
  };
  const perf = useMemo(() => {
    const latencies = nodes
      .map((n) => n.latency_ms)
      .filter((n) => n > 0)
      .sort((a, b) => a - b);
    const p95Index = Math.max(Math.ceil(latencies.length * 0.95) - 1, 0);
    const avgNodeLatency = latencies.length ? latencies.reduce((sum, n) => sum + n, 0) / latencies.length : 0;
    const p95NodeLatency = latencies.length ? latencies[p95Index] : 0;
    const totalAttempts = attempts.length;
    const retryAttempts = attempts.filter((a) => a.attempt > 1).length;
    const queueDelay = lifecycle.QueueDelayMs;
    const activeMs = lifecycle.ActiveAttemptDurationMs;
    const idleMs = lifecycle.IdleDurationMs;
    const activeShare = activeMs + idleMs > 0 ? (activeMs / (activeMs + idleMs)) * 100 : 0;
    return {
      avgNodeLatency,
      p95NodeLatency,
      totalAttempts,
      retryAttempts,
      queueDelay,
      activeShare,
      endToEnd: lifecycle.ExecutionDurationMs + queueDelay,
    };
  }, [
    attempts,
    lifecycle.ActiveAttemptDurationMs,
    lifecycle.ExecutionDurationMs,
    lifecycle.IdleDurationMs,
    lifecycle.QueueDelayMs,
    nodes,
  ]);

  if (loading) return <EmptyState message="Loading job..." loading />;
  if (error || !data) return <EmptyState message={error || 'Job not found'} />;

  const exportCSV = () => {
    const header = 'Node,Type,Model,Input Tokens,Output Tokens,Total Tokens,Cost,Latency (ms)\n';
    const rows = nodes.map((n) =>
      [
        n.node_label || n.node_name || n.node_type,
        n.node_type,
        n.model || '',
        n.tokens_input,
        n.tokens_output,
        n.tokens_input + n.tokens_output,
        n.cost.toFixed(8),
        n.latency_ms,
      ].join(','),
    );
    const blob = new Blob([header + rows.join('\n')], { type: 'text/csv' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `job-${id.slice(0, 8)}-tokens.csv`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const exportJSON = () => {
    const blob = new Blob([JSON.stringify({ job: data.Job, nodes, attempts, childJobs }, null, 2)], {
      type: 'application/json',
    });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `job-${id.slice(0, 8)}-trace.json`;
    a.click();
    URL.revokeObjectURL(url);
  };

  return (
    <div className="space-y-4">
      <Breadcrumbs
        items={[{ label: 'Admin', to: '/admin' }, { label: 'Jobs', to: '/admin/jobs' }, { label: id.slice(0, 12) }]}
      />

      {/* Header card */}
      <Card>
        <CardHeader className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
          <div className="space-y-2">
            <CardTitle className="font-mono text-sm">{data.Job.id}</CardTitle>
            <div className="flex flex-wrap items-center gap-2">
              <StatusBadge status={data.Job.status} />
              {data.Job.parent_execution_id && (
                <Link
                  className="text-xs text-zinc-500 underline-offset-4 hover:text-zinc-900 hover:underline"
                  to={`/admin/jobs/${data.Job.parent_execution_id}`}
                >
                  Parent Job
                </Link>
              )}
              {data.Job.workflow_id && data.WorkflowSaved && (
                <Link to={`/admin/workflows/${data.Job.workflow_id}`}>
                  <Badge variant="secondary" className="cursor-pointer hover:bg-zinc-200">
                    Workflow: {data.Job.workflow_id}
                  </Badge>
                </Link>
              )}
              {data.Job.workflow_id && !data.WorkflowSaved && (
                <Badge variant="outline">Ad-hoc Workflow: {data.Job.workflow_id}</Badge>
              )}
              {data.ParentWorkflowID && (
                <Link to={`/admin/workflows/${data.ParentWorkflowID}`}>
                  <Badge variant="secondary" className="cursor-pointer hover:bg-indigo-100 text-indigo-700">
                    Parent Workflow: {data.ParentWorkflowID}
                  </Badge>
                </Link>
              )}
              {data.BenchmarkRun && (
                <Link to={`/admin/benchmarks/${data.BenchmarkRun.run_id}`}>
                  <Badge variant="secondary" className="cursor-pointer hover:bg-amber-100 text-amber-700">
                    Benchmark: {data.BenchmarkRun.benchmark}
                  </Badge>
                </Link>
              )}
            </div>
          </div>
          <div className="flex flex-wrap gap-2">
            {isActive && (
              <Button
                size="sm"
                variant="destructive"
                onClick={() =>
                  setConfirmAction({
                    title: 'Cancel Job',
                    description: 'Are you sure you want to cancel this job? This cannot be undone.',
                    action: () => adminClient.cancelJob(id),
                    variant: 'destructive',
                  })
                }
              >
                Cancel Job
              </Button>
            )}
            {data.Job.status === 'archived' && (
              <Button
                size="sm"
                onClick={() =>
                  setConfirmAction({
                    title: 'Unarchive Job',
                    description: 'Restore this job from the archive?',
                    action: () => adminClient.unarchiveJob(id),
                    variant: 'default',
                  })
                }
              >
                Unarchive
              </Button>
            )}
            {(data.Job.status === 'completed' || data.Job.status === 'failed') && (
              <Button
                size="sm"
                variant="outline"
                onClick={() =>
                  setConfirmAction({
                    title: 'Archive Job',
                    description: 'Move this job to the archive?',
                    action: () => adminClient.archiveJob(id),
                    variant: 'default',
                  })
                }
              >
                Archive
              </Button>
            )}
            <Button size="sm" variant="outline" onClick={exportCSV}>
              Export CSV
            </Button>
            <Button size="sm" variant="outline" onClick={exportJSON}>
              Export JSON
            </Button>
            {data.Job.workflow_id && (
              <Button size="sm" variant="outline" asChild>
                <Link to={`/workflow/from-job/${data.Job.id}`}>Open Snapshot</Link>
              </Button>
            )}
            {data.Job.workflow_id && data.WorkflowSaved && (
              <Button size="sm" variant="outline" asChild>
                <Link to={`/admin/workflows/${data.Job.workflow_id}`}>View Workflow</Link>
              </Button>
            )}
            <Button size="sm" variant="secondary" onClick={() => navigate('/admin/jobs')}>
              Back to Jobs
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-zinc-600">{data.Job.description}</p>
          {data.InputPrompt && (
            <p className="mt-2 rounded-md bg-zinc-50 p-3 text-sm leading-relaxed text-zinc-700">{data.InputPrompt}</p>
          )}
        </CardContent>
      </Card>

      {(actionError || actionMessage) && (
        <div
          className={`rounded-md border px-3 py-2 text-sm ${
            actionError ? 'border-red-200 bg-red-50 text-red-700' : 'border-emerald-200 bg-emerald-50 text-emerald-700'
          }`}
          role={actionError ? 'alert' : 'status'}
        >
          {actionError || actionMessage}
        </div>
      )}

      {/* Stat cards */}
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-6">
        <StatCard label="Total Cost" value={formatCost(data.JobCostSummary.TotalCost)} color="emerald" />
        <StatCard
          label="Tokens"
          value={data.Job.tokens_total.toLocaleString()}
          hint={`In: ${data.Job.tokens_input.toLocaleString()} / Out: ${data.Job.tokens_output.toLocaleString()}`}
          color="sky"
        />
        <StatCard label="History Events" value={data.Lifecycle.HistoryEventCount} color="purple" />
        <StatCard label="Queue Delay" value={durationLabel(data.Lifecycle.QueueDelayMs)} color="amber" />
        <StatCard label="Active Time" value={durationLabel(data.Lifecycle.ActiveAttemptDurationMs)} color="teal" />
        <StatCard label="Idle Time" value={durationLabel(data.Lifecycle.IdleDurationMs)} color="red" />
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Performance Focus</CardTitle>
        </CardHeader>
        <CardContent className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6">
          <PerfMetric label="End-to-End" value={durationLabel(perf.endToEnd)} hint="queue + execution" />
          <PerfMetric
            label="Avg Node Latency"
            value={durationLabel(perf.avgNodeLatency)}
            hint={`${nodes.length} nodes`}
          />
          <PerfMetric label="P95 Node Latency" value={durationLabel(perf.p95NodeLatency)} hint="tail behavior" />
          <PerfMetric
            label="Retry Attempts"
            value={String(perf.retryAttempts)}
            hint={`${perf.totalAttempts} total attempts`}
          />
          <PerfMetric label="Queue Delay" value={durationLabel(perf.queueDelay)} hint="scheduler backlog" />
          <PerfMetric label="Active Share" value={`${perf.activeShare.toFixed(1)}%`} hint="active vs idle time" />
        </CardContent>
      </Card>

      {/* Timeline + trace tabs */}
      <Tabs defaultValue="timeline">
        <TabsList>
          <TabsTrigger value="timeline">Timeline</TabsTrigger>
          <TabsTrigger value="trace">Workflow Trace ({nodes.length})</TabsTrigger>
          <TabsTrigger value="details">Node Table</TabsTrigger>
          <TabsTrigger value="attempts">Attempts ({attempts.length})</TabsTrigger>
          <TabsTrigger value="agent-runs">Agent Runs ({agentRuns.length})</TabsTrigger>
        </TabsList>

        <TabsContent value="timeline">
          <Card>
            <CardHeader>
              <CardTitle>Execution Timeline</CardTitle>
            </CardHeader>
            <CardContent>
              <TimelineChart nodes={nodes} lifecycle={data.Lifecycle} />
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="trace">
          <Card>
            <CardHeader>
              <CardTitle>Workflow Trace</CardTitle>
            </CardHeader>
            <CardContent>
              <NodeTraceList nodes={nodes} />
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="details">
          <Card>
            <CardHeader>
              <CardTitle>Node Details</CardTitle>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Node</TableHead>
                    <TableHead>Type</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Model</TableHead>
                    <TableHead>Input</TableHead>
                    <TableHead>Output</TableHead>
                    <TableHead>Cost</TableHead>
                    <TableHead>Latency</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {nodes.map((node) => (
                    <Fragment key={node.node_id}>
                      <TableRow
                        className={node.ChildJobID ? 'cursor-pointer transition-colors hover:bg-indigo-50/60' : ''}
                        onClick={node.ChildJobID ? () => navigate(`/admin/jobs/${node.ChildJobID}`) : undefined}
                      >
                        <TableCell>
                          <p className="font-medium">S{node.DisplayOrder}</p>
                          <p className="max-w-[200px] truncate text-xs text-zinc-500">
                            {node.node_label || node.node_name || node.node_type}
                          </p>
                        </TableCell>
                        <TableCell>
                          <div className="flex items-center gap-1.5">
                            <Badge variant="outline" className="text-[10px]">
                              {node.node_type}
                            </Badge>
                            {node.ChildJobID && <span className="text-[10px] text-indigo-500">→ child job</span>}
                          </div>
                        </TableCell>
                        <TableCell>
                          <div className="flex items-center gap-1.5">
                            <StatusBadge status={node.status} />
                            {isNodeReplayed(node.metadata) && (
                              <Badge
                                variant="outline"
                                className="border-amber-300 bg-amber-50 text-amber-700 text-[10px]"
                              >
                                replayed
                              </Badge>
                            )}
                          </div>
                        </TableCell>
                        <TableCell className="text-xs">{node.model || '-'}</TableCell>
                        <TableCell>{node.tokens_input.toLocaleString()}</TableCell>
                        <TableCell>{node.tokens_output.toLocaleString()}</TableCell>
                        <TableCell>{formatCost(node.cost)}</TableCell>
                        <TableCell>{durationLabel(node.latency_ms)}</TableCell>
                      </TableRow>
                      {node.ChildJobNodes &&
                        node.ChildJobNodes.length > 0 &&
                        node.ChildJobNodes.map((child) => (
                          <TableRow key={`${node.node_id}-child-${child.node_id}`} className="bg-indigo-50/40">
                            <TableCell>
                              <p className="pl-4 font-medium text-indigo-600">↳ {child.DisplayOrder}</p>
                              <p className="max-w-[200px] truncate pl-4 text-xs text-indigo-400">
                                {child.node_label || child.node_name || child.node_type}
                              </p>
                            </TableCell>
                            <TableCell>
                              <Badge variant="outline" className="border-indigo-200 text-indigo-600 text-[10px]">
                                {child.node_type}
                              </Badge>
                            </TableCell>
                            <TableCell>
                              <div className="flex items-center gap-1.5">
                                <StatusBadge status={child.status} />
                                {isNodeReplayed(child.metadata) && (
                                  <Badge
                                    variant="outline"
                                    className="border-amber-300 bg-amber-50 text-amber-700 text-[10px]"
                                  >
                                    replayed
                                  </Badge>
                                )}
                              </div>
                            </TableCell>
                            <TableCell className="text-xs">{child.model || '-'}</TableCell>
                            <TableCell>{child.tokens_input.toLocaleString()}</TableCell>
                            <TableCell>{child.tokens_output.toLocaleString()}</TableCell>
                            <TableCell>{formatCost(child.cost)}</TableCell>
                            <TableCell>{durationLabel(child.latency_ms)}</TableCell>
                          </TableRow>
                        ))}
                    </Fragment>
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="attempts">
          <Card>
            <CardHeader>
              <CardTitle>Execution Attempts</CardTitle>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Node</TableHead>
                    <TableHead>Attempt</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Error</TableHead>
                    <TableHead>Tokens</TableHead>
                    <TableHead>Cost</TableHead>
                    <TableHead>Effort</TableHead>
                    <TableHead>Latency</TableHead>
                    <TableHead>Started</TableHead>
                    <TableHead>Completed</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {attempts.map((attempt) => (
                    <TableRow key={`${attempt.node_id}-${attempt.attempt}`}>
                      <TableCell className="font-mono text-xs">
                        <span>{attempt.node_id}</span>
                        {typeof attempt.metadata?.child_job_id === 'string' && (
                          <Link
                            to={`/admin/jobs/${attempt.metadata.child_job_id}`}
                            className="ml-1.5 text-[10px] text-indigo-500 underline-offset-4 hover:underline"
                            onClick={(e) => e.stopPropagation()}
                          >
                            child job
                          </Link>
                        )}
                      </TableCell>
                      <TableCell>#{attempt.attempt}</TableCell>
                      <TableCell>
                        <div className="flex items-center gap-1.5">
                          <StatusBadge status={attempt.status} />
                          {attempt.metadata?.replayed === true && (
                            <Badge
                              variant="outline"
                              className="border-amber-300 bg-amber-50 text-amber-700 text-[10px]"
                            >
                              replayed
                            </Badge>
                          )}
                        </div>
                      </TableCell>
                      <TableCell className="text-xs">
                        {attempt.error_code ? (
                          <span title={attempt.error_message}>
                            <Badge variant="outline" className="border-red-200 text-red-600 text-[10px]">
                              {attempt.error_code}
                            </Badge>
                          </span>
                        ) : attempt.error_message ? (
                          <span
                            className="inline-block max-w-[200px] truncate text-red-600"
                            title={attempt.error_message}
                          >
                            {attempt.error_message}
                          </span>
                        ) : (
                          '-'
                        )}
                      </TableCell>
                      <TableCell className="text-xs tabular-nums">
                        {attempt.tokens_input + attempt.tokens_output > 0
                          ? `${attempt.tokens_input.toLocaleString()} / ${attempt.tokens_output.toLocaleString()}`
                          : '-'}
                      </TableCell>
                      <TableCell className="text-xs tabular-nums">
                        {attempt.cost > 0 ? formatCost(attempt.cost) : '-'}
                      </TableCell>
                      <TableCell className="text-xs">{(attempt.metadata?.reasoning_effort as string) ?? '-'}</TableCell>
                      <TableCell>{durationLabel(attempt.latency_ms)}</TableCell>
                      <TableCell className="text-xs">
                        {attempt.started_at ? relativeTime(attempt.started_at) : '-'}
                      </TableCell>
                      <TableCell className="text-xs">
                        {attempt.completed_at ? relativeTime(attempt.completed_at) : '-'}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="agent-runs">
          <Card>
            <CardHeader>
              <CardTitle>Agent Runs</CardTitle>
            </CardHeader>
            <CardContent>
              {agentRuns.length === 0 ? (
                <EmptyState message="No Novomo-backed agent runs for this job." />
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Node</TableHead>
                      <TableHead>Kind</TableHead>
                      <TableHead>Status</TableHead>
                      <TableHead>Novomo IDs</TableHead>
                      <TableHead>Usage</TableHead>
                      <TableHead>Error</TableHead>
                      <TableHead className="text-right">Action</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {agentRuns.map((run) => {
                      const actionKey = `stop-agent-run:${run.id}`;
                      const stoppable = canStopAgentRun(run);
                      return (
                        <TableRow key={run.id}>
                          <TableCell>
                            <p className="font-mono text-xs">{run.node_id}</p>
                            <p className="text-xs text-zinc-500">Attempt #{run.attempt}</p>
                          </TableCell>
                          <TableCell>
                            <Badge variant="outline" className="text-[10px]">
                              {agentRunKindLabel(run.run_kind)}
                            </Badge>
                          </TableCell>
                          <TableCell>
                            <StatusBadge status={run.status} />
                          </TableCell>
                          <TableCell>
                            <NovomoIDsCell run={run} />
                          </TableCell>
                          <TableCell className="text-xs tabular-nums">
                            {run.tokens_input + run.tokens_output > 0
                              ? `${(run.tokens_input + run.tokens_output).toLocaleString()} tokens`
                              : '-'}
                            {run.cost_usd > 0 && <span className="ml-2 text-zinc-500">{formatCost(run.cost_usd)}</span>}
                          </TableCell>
                          <TableCell
                            className="max-w-[220px] truncate text-xs"
                            title={run.error_message || run.error_code}
                          >
                            {run.error_code ? (
                              <Badge variant="outline" className="border-red-200 text-red-600 text-[10px]">
                                {run.error_code}
                              </Badge>
                            ) : run.error_message ? (
                              run.error_message
                            ) : (
                              '-'
                            )}
                          </TableCell>
                          <TableCell className="text-right">
                            {stoppable ? (
                              <Button
                                size="sm"
                                variant="destructive"
                                disabled={runningActionKey === actionKey}
                                onClick={() =>
                                  setConfirmAction({
                                    title: `Stop ${agentRunKindLabel(run.run_kind)}`,
                                    description:
                                      'Stop the running Novomo-backed work for this node. The workflow node will finish as cancelled or failed after the runtime reconciles.',
                                    action: () => adminClient.stopAgentRun(id, run.id),
                                    variant: 'destructive',
                                    actionKey,
                                    successMessage: `${agentRunKindLabel(run.run_kind)} stop requested.`,
                                  })
                                }
                              >
                                {runningActionKey === actionKey ? (
                                  <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
                                ) : (
                                  <Square className="mr-1.5 h-3.5 w-3.5" />
                                )}
                                Stop
                              </Button>
                            ) : (
                              <span className="text-xs text-zinc-400">-</span>
                            )}
                          </TableCell>
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      {/* Child jobs */}
      {childJobs.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Child Jobs ({childJobs.length})</CardTitle>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>ID</TableHead>
                  <TableHead>Description</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Tokens</TableHead>
                  <TableHead>Cost</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {childJobs.map((job) => (
                  <TableRow key={job.id} className="cursor-pointer" onClick={() => navigate(`/admin/jobs/${job.id}`)}>
                    <TableCell className="font-mono text-xs">
                      <Link to={`/admin/jobs/${job.id}`} className="underline-offset-4 hover:underline">
                        {job.id.slice(0, 8)}
                      </Link>
                    </TableCell>
                    <TableCell className="max-w-[300px] truncate">{job.description}</TableCell>
                    <TableCell>
                      <StatusBadge status={job.status} />
                    </TableCell>
                    <TableCell>{job.tokens_total.toLocaleString()}</TableCell>
                    <TableCell>{formatCost(job.cost)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      {/* Confirmation dialog */}
      <Dialog open={!!confirmAction} onOpenChange={(open) => !open && setConfirmAction(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{confirmAction?.title}</DialogTitle>
            <DialogDescription>{confirmAction?.description}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <DialogClose asChild>
              <Button variant="outline">Cancel</Button>
            </DialogClose>
            <Button
              variant={confirmAction?.variant === 'destructive' ? 'destructive' : 'default'}
              disabled={!!runningActionKey}
              onClick={() => confirmAction && runAction(confirmAction)}
            >
              {runningActionKey ? 'Working...' : 'Confirm'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function NovomoIDsCell({ run }: { run: AgentRunSummary }) {
  const links = agentRunNovomoIDLinks(run);
  if (links.length === 0) {
    return <span className="text-xs text-zinc-400">-</span>;
  }

  return (
    <div className="flex max-w-[360px] flex-col gap-1">
      {links.map((link) => {
        const content = (
          <>
            <span className="shrink-0 rounded border border-zinc-200 px-1.5 py-0.5 font-sans text-[10px] text-zinc-500">
              {link.label}
            </span>
            <span className="min-w-0 break-all font-mono text-xs">{link.id}</span>
            {link.url && <ExternalLink className="h-3 w-3 shrink-0" />}
          </>
        );

        return link.url ? (
          <a
            key={`${link.label}:${link.id}`}
            href={link.url}
            target="_blank"
            rel="noreferrer"
            className="flex items-start gap-1.5 text-teal-700 underline-offset-4 hover:underline"
            title={link.url}
          >
            {content}
          </a>
        ) : (
          <div key={`${link.label}:${link.id}`} className="flex items-start gap-1.5 text-zinc-700">
            {content}
          </div>
        );
      })}
    </div>
  );
}

function PerfMetric({ label, value, hint }: { label: string; value: string; hint: string }) {
  return (
    <div className="rounded-md border border-zinc-200 bg-zinc-50 px-3 py-2">
      <div className="text-[11px] uppercase tracking-wide text-zinc-500">{label}</div>
      <div className="text-base font-semibold text-zinc-900">{value}</div>
      <div className="text-xs text-zinc-500">{hint}</div>
    </div>
  );
}
