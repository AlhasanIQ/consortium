import { CheckCircle, ChevronDown, ExternalLink, XCircle } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { type AdminBenchmarkItemDetailResponse, adminClient } from '@/api/adminClient';
import { Breadcrumbs } from '@/components/admin/Breadcrumbs';
import { EmptyState } from '@/components/admin/EmptyState';
import { StatCard } from '@/components/admin/StatCard';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { durationLabel, formatCost } from '@/lib/formatters';

export default function AdminBenchmarkItemDetailPage() {
  const params = useParams();
  const id = params.id ?? '';
  const itemRef = params.itemID ?? params['*'] ?? '';
  let itemID = itemRef;
  try {
    itemID = decodeURIComponent(itemRef);
  } catch {
    itemID = itemRef;
  }
  const [data, setData] = useState<AdminBenchmarkItemDetailResponse | null>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!id || !itemID) return;
    adminClient
      .getBenchmarkItem(id, itemID)
      .then((response) => setData(response))
      .catch((err: Error) => setError(err.message || 'Failed to load benchmark item'));
  }, [id, itemID]);
  const attempts = data?.Detail.attempts ?? [];

  const attemptHealth = useMemo(() => {
    const total = attempts.length;
    const parsed = attempts.filter((a) => a.parse_ok).length;
    const failed = attempts.filter((a) => !a.parse_ok || !!a.failure_reason).length;
    return { total, parsed, failed };
  }, [attempts]);

  if (error) return <EmptyState message={error} />;
  if (!data) return <EmptyState message="Loading benchmark item..." />;

  const { item } = data.Detail;
  const datasetItem = data.DatasetItem;
  const jobSummaries = data.JobSummaries ?? [];

  return (
    <div className="space-y-4">
      <Breadcrumbs
        items={[
          { label: 'Admin', to: '/admin' },
          { label: 'Benchmarks', to: '/admin/benchmarks' },
          { label: data.Run.id.slice(0, 8), to: `/admin/benchmarks/${data.Run.id}` },
          { label: `Item ${item.item_id}` },
        ]}
      />

      <div className="flex items-center gap-3">
        <h2 className="text-lg font-semibold">Item {item.item_id}</h2>
        {item.correct ? (
          <Badge variant="success" className="gap-1">
            <CheckCircle className="h-3.5 w-3.5" />
            Correct
          </Badge>
        ) : (
          <Badge variant="destructive" className="gap-1">
            <XCircle className="h-3.5 w-3.5" />
            Incorrect
          </Badge>
        )}
        {data.NotFoundInSet ? (
          <Badge variant="outline" className="text-amber-700">
            Item not present in current dataset split
          </Badge>
        ) : null}
      </div>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4 xl:grid-cols-6">
        <StatCard label="Expected" value={item.answer_label} color="sky" />
        <StatCard label="Predicted" value={item.predicted || '-'} color={item.correct ? 'emerald' : 'red'} />
        <StatCard label="Parse OK" value={item.parse_ok ? 'Yes' : 'No'} color={item.parse_ok ? 'emerald' : 'amber'} />
        <StatCard label="Tokens" value={item.total_tokens.toLocaleString()} color="sky" />
        <StatCard label="Cost" value={formatCost(item.cost_usd)} color="amber" />
        <StatCard label="Attempts" value={attempts.length} color="purple" />
      </div>

      <div className="grid gap-4 lg:grid-cols-5">
        <Card className="lg:col-span-3">
          <CardHeader>
            <CardTitle>Question Context</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            {datasetItem?.question ? (
              <>
                <p className="text-sm leading-relaxed text-zinc-800">{datasetItem.question}</p>
                {datasetItem.choices && datasetItem.choices.length > 0 ? (
                  <div className="space-y-1.5">
                    {datasetItem.choices.map((choice, index) => {
                      const label = String.fromCharCode(65 + index);
                      const isGold = label === datasetItem.answer_label;
                      const isPred = label === item.predicted;
                      return (
                        <div
                          key={`${label}-${choice}`}
                          className="flex items-start gap-2 rounded-md border border-zinc-200 bg-zinc-50 px-3 py-2 text-sm"
                        >
                          <span className="font-mono text-zinc-700">{label}.</span>
                          <span className="flex-1">{choice}</span>
                          {isGold ? (
                            <Badge variant="outline" className="border-emerald-200 bg-emerald-50 text-emerald-700">
                              gold
                            </Badge>
                          ) : null}
                          {isPred ? (
                            <Badge variant="outline" className="border-sky-200 bg-sky-50 text-sky-700">
                              predicted
                            </Badge>
                          ) : null}
                        </div>
                      );
                    })}
                  </div>
                ) : null}
              </>
            ) : (
              <p className="text-sm text-zinc-500">Dataset question is unavailable for this item.</p>
            )}
          </CardContent>
        </Card>

        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle>Attempt Quality</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <AttemptStackChart
              total={attemptHealth.total}
              parsed={attemptHealth.parsed}
              failed={attemptHealth.failed}
              nonLetterRetries={item.non_letter_retries ?? 0}
            />
            {item.failure_reason ? (
              <div className="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700">
                <span className="font-medium">Final failure:</span> {item.failure_reason}
              </div>
            ) : (
              <p className="text-sm text-zinc-500">No terminal failure reason recorded.</p>
            )}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Attempts</CardTitle>
        </CardHeader>
        <CardContent>
          {attempts.length === 0 ? (
            <EmptyState message="No attempts found." />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>#</TableHead>
                  <TableHead>Job</TableHead>
                  <TableHead>Predicted</TableHead>
                  <TableHead>Parse OK</TableHead>
                  <TableHead>Failure</TableHead>
                  <TableHead>Latency</TableHead>
                  <TableHead>Tokens</TableHead>
                  <TableHead>Cost</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {attempts.map((attempt) => (
                  <TableRow key={`${attempt.attempt}-${attempt.job_id}`}>
                    <TableCell>{attempt.attempt}</TableCell>
                    <TableCell className="font-mono text-xs">
                      {attempt.job_id ? (
                        <Link
                          className="text-sky-600 underline-offset-4 hover:underline"
                          to={`/admin/jobs/${attempt.job_id}`}
                        >
                          {attempt.job_id.slice(0, 8)}
                        </Link>
                      ) : (
                        '-'
                      )}
                    </TableCell>
                    <TableCell>{attempt.predicted || '-'}</TableCell>
                    <TableCell>{attempt.parse_ok ? 'yes' : 'no'}</TableCell>
                    <TableCell className="max-w-[220px] truncate text-xs">{attempt.failure_reason || '-'}</TableCell>
                    <TableCell>{durationLabel(attempt.latency_ms ?? 0)}</TableCell>
                    <TableCell>{attempt.total_tokens.toLocaleString()}</TableCell>
                    <TableCell>{formatCost(attempt.cost_usd)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Execution Drilldown</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          {jobSummaries.length === 0 ? (
            <p className="text-sm text-zinc-500">No linked jobs found for attempts.</p>
          ) : (
            jobSummaries.map((summary) => <JobDrilldown key={summary.JobID} summary={summary} />)
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Raw Output</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          {attempts.length > 0 ? (
            attempts.map((attempt) => (
              <AttemptOutput
                key={`output-${attempt.attempt}-${attempt.job_id}`}
                label={`Attempt #${attempt.attempt}`}
                output={attempt.raw_output}
              />
            ))
          ) : (
            <pre className="max-h-[500px] overflow-auto rounded-md bg-zinc-950 p-4 text-xs text-zinc-100">
              {item.raw_output}
            </pre>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function AttemptStackChart({
  total,
  parsed,
  failed,
  nonLetterRetries,
}: {
  total: number;
  parsed: number;
  failed: number;
  nonLetterRetries: number;
}) {
  const safeTotal = Math.max(total, 1);
  const parsedPct = (parsed / safeTotal) * 100;
  const failedPct = (failed / safeTotal) * 100;
  return (
    <div className="space-y-2">
      <div className="text-xs text-zinc-500">Attempt-level parse and failure pressure</div>
      <div className="h-3 rounded-full bg-zinc-100">
        <div className="h-full rounded-full bg-emerald-500/80" style={{ width: `${parsedPct}%` }} />
      </div>
      <div className="h-3 rounded-full bg-zinc-100">
        <div className="h-full rounded-full bg-rose-500/80" style={{ width: `${failedPct}%` }} />
      </div>
      <div className="grid grid-cols-3 gap-2 pt-1 text-center">
        <MetricPill label="Total" value={String(total)} />
        <MetricPill label="Parsed" value={String(parsed)} />
        <MetricPill label="Non-letter retries" value={String(nonLetterRetries)} />
      </div>
    </div>
  );
}

function MetricPill({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-full border border-zinc-200 px-2 py-1">
      <div className="text-[10px] uppercase tracking-wide text-zinc-500">{label}</div>
      <div className="text-xs font-semibold text-zinc-800">{value}</div>
    </div>
  );
}

type JobSummaryType = NonNullable<AdminBenchmarkItemDetailResponse['JobSummaries']>[number];
type NodeType = NonNullable<JobSummaryType['Nodes']>[number];

function nodeDisplayName(node: NodeType): string {
  // Prefer the human-readable name, fall back to label, then id
  if (node.node_name) return node.node_name;
  // node_label from backend is "node-id Type(Name)" — extract just the type/name part
  if (node.node_label) {
    const parenMatch = node.node_label.match(/\w+\((.+)\)$/);
    if (parenMatch) return parenMatch[1];
    return node.node_label;
  }
  return node.node_id;
}

function extractFinalAnswer(output: string): string | null {
  const match = output.match(/Final answer:\s*(.+)/i);
  return match ? match[1].trim() : null;
}

function NodeOutputBlock({ node, jobID, indented }: { node: NodeType; jobID: string; indented?: boolean }) {
  const [showOutput, setShowOutput] = useState(false);
  const output = node.output?.trim() || '';
  const finalAnswer = extractFinalAnswer(output);

  return (
    <div
      key={`${jobID}-${node.id}`}
      className={`rounded-md border border-zinc-200 bg-white ${indented ? 'ml-5 border-l-2 border-l-zinc-300' : ''}`}
    >
      <button
        type="button"
        className="flex w-full items-center justify-between gap-2 px-3 py-2 text-left transition-colors hover:bg-zinc-50"
        onClick={() => output && setShowOutput(!showOutput)}
      >
        <div className="flex items-center gap-2">
          {indented ? <span className="text-[10px] text-zinc-400">↳</span> : null}
          <span className="text-[11px] font-medium text-zinc-700">{nodeDisplayName(node)}</span>
          <span className="rounded bg-zinc-100 px-1.5 py-0.5 text-[10px] text-zinc-400">{node.node_type}</span>
          {node.model ? <span className="text-[11px] text-zinc-400">{node.model}</span> : null}
          {finalAnswer ? (
            <span className="rounded-full border border-sky-200 bg-sky-50 px-2 py-0.5 text-[11px] font-medium text-sky-700">
              {finalAnswer}
            </span>
          ) : null}
        </div>
        <div className="flex items-center gap-2 text-[11px] text-zinc-500">
          <span>{durationLabel(node.latency_ms)}</span>
          <span>{(node.tokens_input + node.tokens_output).toLocaleString()} tok</span>
          <span>{formatCost(node.cost)}</span>
          {output ? (
            <ChevronDown
              className={`h-3.5 w-3.5 text-zinc-400 transition-transform ${showOutput ? 'rotate-180' : ''}`}
            />
          ) : null}
        </div>
      </button>
      {showOutput && output ? (
        <div className="border-t border-zinc-100 px-3 py-2">
          <pre className="max-h-[400px] overflow-auto whitespace-pre-wrap text-xs leading-relaxed text-zinc-700">
            {output}
          </pre>
        </div>
      ) : !output ? (
        <div className="border-t border-zinc-100 px-3 py-2 text-xs text-zinc-400">(no output)</div>
      ) : null}
    </div>
  );
}

function JobDrilldown({ summary }: { summary: JobSummaryType }) {
  const [open, setOpen] = useState(true);
  const childJobs = summary.ChildJobs ?? [];
  // For the parent (benchmark wrapper), only show the child_workflow node summary.
  // The real content is in the child job nodes.
  const parentNodes = (summary.Nodes ?? []).filter((n) => n.node_type !== 'child_workflow');
  // Collect all child job nodes — these are the actual ensemble nodes with model outputs.
  const allChildNodes = childJobs.flatMap((cj) => cj.Nodes ?? []);
  const hasChildNodes = allChildNodes.length > 0;

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-center justify-between rounded-md border border-zinc-200 bg-zinc-50 px-4 py-2.5 text-sm font-medium text-zinc-700 transition-colors hover:bg-zinc-100">
        <div className="flex items-center gap-2">
          <span className="font-mono text-xs">{summary.JobID.slice(0, 8)}</span>
          {summary.Job ? <Badge variant="outline">{summary.Job.status}</Badge> : null}
          {typeof summary.TotalTokens === 'number' ? (
            <span className="text-xs text-zinc-500">{summary.TotalTokens?.toLocaleString()} tok</span>
          ) : null}
          {typeof summary.TotalCost === 'number' ? (
            <span className="text-xs text-zinc-500">{formatCost(summary.TotalCost)}</span>
          ) : null}
        </div>
        <div className="flex items-center gap-2">
          <Link
            className="inline-flex items-center gap-1 text-xs text-sky-600 underline-offset-4 hover:underline"
            to={`/admin/jobs/${summary.JobID}`}
            onClick={(e) => e.stopPropagation()}
          >
            Open job <ExternalLink className="h-3 w-3" />
          </Link>
          <ChevronDown className={`h-4 w-4 text-zinc-400 transition-transform ${open ? 'rotate-180' : ''}`} />
        </div>
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className="mt-2 space-y-3 rounded-md border border-zinc-200 p-3">
          {/* Child workflow nodes (the actual ensemble) — this is the primary content */}
          {hasChildNodes ? (
            <div className="space-y-2">
              <div className="text-xs font-medium text-zinc-500 uppercase tracking-wide">
                Ensemble Nodes ({allChildNodes.length})
              </div>
              {allChildNodes.map((node) => (
                <NodeOutputBlock
                  key={`child-${childJobs[0]?.JobID}-${node.id}`}
                  node={node}
                  jobID={childJobs[0]?.JobID ?? summary.JobID}
                  indented={!!node.parent_node_id}
                />
              ))}
            </div>
          ) : null}

          {/* Parent wrapper nodes (agent contract, result extraction) */}
          {parentNodes.length > 0 ? (
            <div className="space-y-2">
              <div className="text-xs font-medium text-zinc-500 uppercase tracking-wide">
                {hasChildNodes ? 'Wrapper Nodes' : 'Nodes'} ({parentNodes.length})
              </div>
              {parentNodes.map((node) => (
                <NodeOutputBlock
                  key={`parent-${summary.JobID}-${node.id}`}
                  node={node}
                  jobID={summary.JobID}
                  indented={!!node.parent_node_id}
                />
              ))}
            </div>
          ) : null}

          {!hasChildNodes && parentNodes.length === 0 ? (
            <p className="text-sm text-zinc-500">No node-level details available.</p>
          ) : null}
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}

function AttemptOutput({ label, output }: { label: string; output: string }) {
  const [open, setOpen] = useState(false);

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger className="flex w-full items-center justify-between rounded-md border border-zinc-200 bg-zinc-50 px-4 py-2.5 text-sm font-medium text-zinc-700 transition-colors hover:bg-zinc-100">
        <span>{label}</span>
        <ChevronDown className={`h-4 w-4 text-zinc-400 transition-transform ${open ? 'rotate-180' : ''}`} />
      </CollapsibleTrigger>
      <CollapsibleContent>
        <pre className="mt-1 max-h-[400px] overflow-auto rounded-md bg-zinc-950 p-4 text-xs text-zinc-100">
          {output || '(empty)'}
        </pre>
      </CollapsibleContent>
    </Collapsible>
  );
}
