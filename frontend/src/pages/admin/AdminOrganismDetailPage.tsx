import { useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import {
  adminClient,
  type OptimizationMutationArtifact,
  type OptimizationOrganism,
  type OptimizationParamChange,
} from '@/api/adminClient';
import { Breadcrumbs } from '@/components/admin/Breadcrumbs';
import { EmptyState } from '@/components/admin/EmptyState';
import { StatCard } from '@/components/admin/StatCard';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { formatCost } from '@/lib/formatters';

export default function AdminOrganismDetailPage() {
  const { id = '', orgId = '' } = useParams();
  const navigate = useNavigate();
  const [organism, setOrganism] = useState<OptimizationOrganism | null>(null);
  const [paramChanges, setParamChanges] = useState<OptimizationParamChange[]>([]);
  const [lineage, setLineage] = useState<OptimizationOrganism[]>([]);
  const [artifacts, setArtifacts] = useState<OptimizationMutationArtifact[]>([]);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!id || !orgId) return;
    Promise.all([
      adminClient.getOptimizationOrganism(id, orgId),
      adminClient.getOptimizationOrganismLineage(orgId),
      adminClient.getOptimizationMutationArtifacts(orgId),
    ])
      .then(([orgResp, lineageResp, artifactsResp]) => {
        setOrganism(orgResp.organism);
        setParamChanges(orgResp.param_changes ?? []);
        setLineage(lineageResp.organisms ?? []);
        setArtifacts(artifactsResp.artifacts ?? []);
      })
      .catch((err: Error) => setError(err.message || 'Failed to load organism detail'));
  }, [id, orgId]);

  const lineageByID = useMemo(() => {
    const out: Record<string, OptimizationOrganism> = {};
    for (const item of lineage) out[item.id] = item;
    return out;
  }, [lineage]);

  const parent = useMemo(() => {
    const parentID = organism?.parent_ids?.[0];
    if (!parentID) return null;
    return lineageByID[parentID] ?? null;
  }, [lineageByID, organism?.parent_ids]);

  const ancestryChain = useMemo(() => {
    if (!organism) return [] as OptimizationOrganism[];
    const out: OptimizationOrganism[] = [];
    let cursor: OptimizationOrganism | null = organism;
    const seen = new Set<string>();
    while (cursor && !seen.has(cursor.id)) {
      seen.add(cursor.id);
      out.push(cursor);
      const nextID: string | undefined = cursor.parent_ids?.[0];
      cursor = nextID ? (lineageByID[nextID] ?? null) : null;
    }
    return out.reverse();
  }, [lineageByID, organism]);

  if (error) return <EmptyState message={error} />;
  if (!organism) return <EmptyState message="Loading organism..." />;

  const scoreDelta = delta(organism.fitness?.composite_score, parent?.fitness?.composite_score);
  const accuracyDelta = delta(organism.fitness?.adjusted_accuracy, parent?.fitness?.adjusted_accuracy);
  const parseDelta = delta(organism.fitness?.parse_rate, parent?.fitness?.parse_rate);
  const costDelta = delta(organism.fitness?.cost_per_item, parent?.fitness?.cost_per_item);
  const latDelta = delta(organism.fitness?.avg_latency_ms, parent?.fitness?.avg_latency_ms);
  const p95Delta = delta(organism.fitness?.p95_latency_ms, parent?.fitness?.p95_latency_ms);

  return (
    <div className="space-y-4">
      <Breadcrumbs
        items={[
          { label: 'Admin', to: '/admin' },
          { label: 'Optimize', to: '/admin/optimize' },
          { label: id, to: `/admin/optimize/${id}` },
          { label: organism.id },
        ]}
      />

      <Card>
        <CardHeader className="space-y-2">
          <CardTitle className="font-mono text-sm">{organism.id}</CardTitle>
          <p className="text-sm text-zinc-600">
            Generation {organism.generation} · Mutation {organism.mutation_type || '-'}
          </p>
          <p className="text-sm text-zinc-600">
            Parents: {organism.parent_ids.length > 0 ? organism.parent_ids.join(', ') : '-'}
          </p>
          {organism.mutation_log ? <p className="text-sm text-zinc-700">"{organism.mutation_log}"</p> : null}
          {organism.bench_run_id ? (
            <p className="text-sm text-zinc-600">
              Benchmark run:{' '}
              <Link
                className="font-mono text-sky-700 hover:underline"
                to={`/admin/benchmarks/${organism.bench_run_id}`}
              >
                {organism.bench_run_id}
              </Link>
            </p>
          ) : null}
        </CardHeader>
      </Card>

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-6">
        <StatCard
          label="Score"
          value={organism.fitness ? organism.fitness.composite_score.toFixed(4) : '-'}
          hint={renderDelta(scoreDelta)}
          color="teal"
        />
        <StatCard
          label="Adj. Acc"
          value={organism.fitness ? `${(organism.fitness.adjusted_accuracy * 100).toFixed(2)}%` : '-'}
          hint={renderDelta(accuracyDelta, true)}
          color="emerald"
        />
        <StatCard
          label="Parse"
          value={organism.fitness ? `${(organism.fitness.parse_rate * 100).toFixed(2)}%` : '-'}
          hint={renderDelta(parseDelta, true)}
          color="sky"
        />
        <StatCard
          label="Cost / Item"
          value={organism.fitness ? formatCost(organism.fitness.cost_per_item) : '-'}
          hint={renderDelta(costDelta)}
          color="amber"
        />
        <StatCard
          label="Avg Latency"
          value={organism.fitness ? `${organism.fitness.avg_latency_ms.toFixed(0)}ms` : '-'}
          hint={renderDelta(latDelta)}
          color="zinc"
        />
        <StatCard
          label="P95 Latency"
          value={organism.fitness ? `${organism.fitness.p95_latency_ms.toFixed(0)}ms` : '-'}
          hint={renderDelta(p95Delta)}
          color="zinc"
        />
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Parameter Changes</CardTitle>
        </CardHeader>
        <CardContent>
          {paramChanges.length === 0 ? (
            <p className="text-sm text-zinc-500">No recorded parameter changes.</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Path</TableHead>
                  <TableHead>Old</TableHead>
                  <TableHead>New</TableHead>
                  <TableHead>Reason</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {paramChanges.map((change, index) => (
                  <TableRow key={`${change.path}-${index}`}>
                    <TableCell className="max-w-[320px] truncate font-mono text-xs" title={change.path}>
                      {change.path}
                    </TableCell>
                    <TableCell>
                      <pre className="max-w-[320px] overflow-auto rounded bg-red-50 px-2 py-1 text-xs text-red-800">
                        {prettyValue(change.old_value)}
                      </pre>
                    </TableCell>
                    <TableCell>
                      <pre className="max-w-[320px] overflow-auto rounded bg-emerald-50 px-2 py-1 text-xs text-emerald-800">
                        {prettyValue(change.new_value)}
                      </pre>
                    </TableCell>
                    <TableCell>{change.reason || '-'}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Ancestry</CardTitle>
        </CardHeader>
        <CardContent>
          {ancestryChain.length === 0 ? (
            <p className="text-sm text-zinc-500">No ancestry data.</p>
          ) : (
            <div className="flex flex-wrap items-center gap-2 text-sm">
              {ancestryChain.map((node, index) => (
                <div key={node.id} className="flex items-center gap-2">
                  <Button
                    size="sm"
                    variant={node.id === organism.id ? 'default' : 'outline'}
                    onClick={() => navigate(`/admin/optimize/${id}/organisms/${node.id}`)}
                  >
                    {node.id.slice(0, 10)} ·{' '}
                    {(node.fitness?.adjusted_accuracy ?? 0) * 100 > 0
                      ? `${((node.fitness?.adjusted_accuracy ?? 0) * 100).toFixed(1)}%`
                      : 'n/a'}
                  </Button>
                  {index < ancestryChain.length - 1 ? <span className="text-zinc-400">→</span> : null}
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {artifacts.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Mutation Artifacts</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            {artifacts.map((artifact, index) => (
              <details
                key={`${artifact.input_prompt_hash}-${index}`}
                className="rounded-md border border-zinc-200 bg-zinc-50 p-3"
              >
                <summary className="cursor-pointer text-sm font-medium text-zinc-800">
                  Artifact {index + 1} · {artifact.claude_model || 'unknown model'}
                </summary>
                <div className="mt-3 space-y-3">
                  <div>
                    <p className="mb-1 text-xs font-semibold uppercase tracking-wide text-zinc-500">Input Prompt</p>
                    <pre className="max-h-[260px] overflow-auto rounded border border-zinc-200 bg-white p-2 text-xs">
                      {artifact.input_prompt || `(compact mode) hash=${artifact.input_prompt_hash}`}
                    </pre>
                  </div>
                  <div>
                    <p className="mb-1 text-xs font-semibold uppercase tracking-wide text-zinc-500">Raw Output</p>
                    <pre className="max-h-[260px] overflow-auto rounded border border-zinc-200 bg-white p-2 text-xs">
                      {artifact.raw_output || `(compact mode) hash=${artifact.raw_output_hash}`}
                    </pre>
                  </div>
                </div>
              </details>
            ))}
          </CardContent>
        </Card>
      )}
    </div>
  );
}

function delta(current?: number, previous?: number): number | null {
  if (typeof current !== 'number' || typeof previous !== 'number') return null;
  return current - previous;
}

function renderDelta(value: number | null, asPercent = false): string {
  if (value == null) return 'no parent baseline';
  const formatted = asPercent ? `${(value * 100).toFixed(2)}pp` : value.toFixed(4);
  return `${value >= 0 ? '+' : ''}${formatted} vs parent`;
}

function prettyValue(raw: string): string {
  const trimmed = (raw || '').trim();
  if (!trimmed) return '-';
  try {
    return JSON.stringify(JSON.parse(trimmed), null, 2);
  } catch {
    return trimmed;
  }
}
