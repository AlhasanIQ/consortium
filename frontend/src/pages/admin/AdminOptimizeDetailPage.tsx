import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import {
  adminClient,
  type OptimizationLearningEntry,
  type OptimizationOrganism,
  type OptimizationRun,
} from '@/api/adminClient';
import { Breadcrumbs } from '@/components/admin/Breadcrumbs';
import { BudgetChart } from '@/components/admin/BudgetChart';
import { EmptyState } from '@/components/admin/EmptyState';
import { GenerationChart } from '@/components/admin/GenerationChart';
import { LineageDAG } from '@/components/admin/LineageDAG';
import { StatCard } from '@/components/admin/StatCard';
import { StatusBadge } from '@/components/admin/StatusBadge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Select } from '@/components/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { formatCost } from '@/lib/formatters';
import { usePolling } from '@/lib/usePolling';

export default function AdminOptimizeDetailPage() {
  const { id = '' } = useParams();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();

  const [run, setRun] = useState<OptimizationRun | null>(null);
  const [organisms, setOrganisms] = useState<OptimizationOrganism[]>([]);
  const [learning, setLearning] = useState<OptimizationLearningEntry[]>([]);
  const [lineage, setLineage] = useState<{
    nodes: Array<{
      id: string;
      generation: number;
      parent_ids: string[];
      composite_score?: number;
      feasible?: boolean;
    }>;
    edges: Array<{ from: string; to: string }>;
  }>({
    nodes: [],
    edges: [],
  });
  const [selectedLineageNode, setSelectedLineageNode] = useState('');
  const [error, setError] = useState('');

  const tab = searchParams.get('tab') || 'overview';
  const generationFilter = searchParams.get('generation') || '';
  const learningOutcomeFilter = searchParams.get('outcome') || 'all';

  const setParam = (key: string, value: string) => {
    const next = new URLSearchParams(searchParams);
    if (value) {
      next.set(key, value);
    } else {
      next.delete(key);
    }
    setSearchParams(next);
  };

  const loadRun = useCallback(() => {
    if (!id) return Promise.resolve();
    return adminClient
      .getOptimizationRun(id)
      .then((resp) => setRun(resp))
      .catch((err: Error) => setError(err.message || 'Failed to load optimization run'));
  }, [id]);

  const loadStatic = useCallback(() => {
    if (!id) return;
    Promise.all([
      adminClient.listOptimizationOrganisms(id, { limit: 1000 }),
      adminClient.getOptimizationRunLineage(id),
      adminClient.getOptimizationLearningLog(id, { limit: 500 }),
    ])
      .then(([orgResp, lineageResp, learningResp]) => {
        setOrganisms(orgResp.organisms ?? []);
        setLineage(lineageResp);
        setLearning(learningResp.entries ?? []);
      })
      .catch((err: Error) => setError(err.message || 'Failed to load optimization details'));
  }, [id]);

  useEffect(() => {
    loadRun();
    loadStatic();
  }, [loadRun, loadStatic]);

  usePolling(() => loadRun(), 3000, run?.status === 'running');
  usePolling(() => loadStatic(), 5000, run?.status === 'running');

  const organismsByID = useMemo(() => {
    const out: Record<string, OptimizationOrganism> = {};
    for (const organism of organisms) out[organism.id] = organism;
    return out;
  }, [organisms]);

  const baseline = useMemo(() => {
    const seeded = organisms.find(
      (organism) => organism.generation === 0 && organism.mutation_type === 'seed' && organism.fitness,
    );
    if (seeded) return seeded;
    return organisms.find((organism) => organism.generation === 0 && organism.fitness) || null;
  }, [organisms]);

  const bestOrganism = useMemo(() => {
    if (run?.best_organism_id && organismsByID[run.best_organism_id]) {
      return organismsByID[run.best_organism_id];
    }
    return (
      [...organisms]
        .filter((organism) => organism.fitness)
        .sort((a, b) => (b.fitness?.composite_score ?? 0) - (a.fitness?.composite_score ?? 0))[0] || null
    );
  }, [organisms, organismsByID, run?.best_organism_id]);

  const generationData = useMemo(() => {
    const grouped = new Map<number, OptimizationOrganism[]>();
    for (const organism of organisms) {
      if (!organism.fitness) continue;
      const list = grouped.get(organism.generation) ?? [];
      list.push(organism);
      grouped.set(organism.generation, list);
    }

    const generations = [...grouped.keys()].sort((a, b) => a - b);
    let cumulativeCost = 0;
    const out: Array<{
      generation: number;
      best_accuracy: number;
      mean_accuracy: number;
      worst_accuracy: number;
      cumulative_cost: number;
    }> = [];

    for (const generation of generations) {
      const list = grouped.get(generation) ?? [];
      if (list.length === 0) continue;

      const accuracies = list
        .map((organism) => organism.fitness?.adjusted_accuracy)
        .filter((value): value is number => typeof value === 'number');
      if (accuracies.length === 0) continue;

      const generationCost = list.reduce((sum, organism) => {
        const fitness = organism.fitness;
        if (!fitness) return sum;
        const totalItems = fitness.total_items || run?.item_limit || 0;
        return sum + fitness.cost_per_item * totalItems;
      }, 0);
      cumulativeCost += generationCost;

      out.push({
        generation,
        best_accuracy: Math.max(...accuracies),
        mean_accuracy: accuracies.reduce((sum, value) => sum + value, 0) / accuracies.length,
        worst_accuracy: Math.min(...accuracies),
        cumulative_cost: cumulativeCost,
      });
    }

    return out;
  }, [organisms, run?.item_limit]);

  const feasibleCount = useMemo(
    () => organisms.filter((organism) => organism.fitness?.feasible !== false).length,
    [organisms],
  );

  const filteredPopulation = useMemo(() => {
    if (!generationFilter) return organisms;
    const generation = Number(generationFilter);
    if (Number.isNaN(generation)) return organisms;
    return organisms.filter((organism) => organism.generation === generation);
  }, [generationFilter, organisms]);

  const sortedPopulation = useMemo(
    () =>
      [...filteredPopulation].sort((a, b) => {
        const left = a.fitness?.composite_score ?? -Infinity;
        const right = b.fitness?.composite_score ?? -Infinity;
        return right - left;
      }),
    [filteredPopulation],
  );

  const learningCounts = useMemo(() => {
    const counts = {
      all: learning.length,
      improvement: 0,
      regression: 0,
      no_change: 0,
      constraint_violation: 0,
    };
    for (const entry of learning) {
      if (entry.outcome === 'improvement') counts.improvement += 1;
      else if (entry.outcome === 'regression') counts.regression += 1;
      else if (entry.outcome === 'no_change') counts.no_change += 1;
      else if (entry.outcome === 'constraint_violation') counts.constraint_violation += 1;
    }
    return counts;
  }, [learning]);

  const filteredLearning = useMemo(() => {
    if (learningOutcomeFilter === 'all') return learning;
    return learning.filter((entry) => entry.outcome === learningOutcomeFilter);
  }, [learning, learningOutcomeFilter]);

  const selectedOrganism = selectedLineageNode ? organismsByID[selectedLineageNode] : null;

  if (error) return <EmptyState message={error} />;
  if (!run) return <EmptyState message="Loading optimization run..." />;

  const baselineAcc = baseline?.fitness?.adjusted_accuracy;
  const bestAcc = run.best_fitness?.adjusted_accuracy;
  const deltaAcc = typeof baselineAcc === 'number' && typeof bestAcc === 'number' ? bestAcc - baselineAcc : null;

  return (
    <div className="space-y-4">
      <Breadcrumbs
        items={[{ label: 'Admin', to: '/admin' }, { label: 'Optimize', to: '/admin/optimize' }, { label: run.id }]}
      />

      <Card>
        <CardHeader className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
          <div>
            <CardTitle className="font-mono text-sm">{run.id}</CardTitle>
            <p className="text-sm text-zinc-600">Workflow: {run.workflow_id}</p>
            <p className="text-sm text-zinc-600">
              Benchmark: {run.benchmark}/{run.split}
            </p>
            {run.owner_hostname ? (
              <p className="text-xs text-zinc-500">
                Owner: {run.owner_hostname}:{run.owner_pid}
              </p>
            ) : null}
          </div>
          <div className="flex items-center gap-2">
            <StatusBadge status={run.status} />
            <Button
              size="sm"
              variant="secondary"
              onClick={() => {
                loadRun();
                loadStatic();
              }}
            >
              Refresh
            </Button>
          </div>
        </CardHeader>
      </Card>

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-6">
        <StatCard
          label="Generation"
          value={`${run.generation}/${maxGenerationsFromSpec(run) ?? '-'}`}
          hint={run.strategy}
          color="teal"
        />
        <StatCard
          label="Best Accuracy"
          value={run.best_fitness ? `${(run.best_fitness.adjusted_accuracy * 100).toFixed(2)}%` : '-'}
          hint={
            deltaAcc == null
              ? 'baseline unavailable'
              : `${deltaAcc >= 0 ? '+' : ''}${(deltaAcc * 100).toFixed(2)}pp vs baseline`
          }
          color="emerald"
        />
        <StatCard
          label="Parse Rate"
          value={run.best_fitness ? `${(run.best_fitness.parse_rate * 100).toFixed(2)}%` : '-'}
          color="sky"
        />
        <StatCard
          label="Cost / Item"
          value={run.best_fitness ? formatCost(run.best_fitness.cost_per_item) : '-'}
          color="amber"
        />
        <StatCard
          label="Budget"
          value={`${formatCost(run.spent_usd)} / ${formatCost(run.total_budget_usd)}`}
          color="purple"
        />
        <StatCard label="Organisms" value={run.total_organisms} hint={`${feasibleCount} feasible`} color="zinc" />
      </div>

      <Tabs value={tab} onValueChange={(value) => setParam('tab', value)}>
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="population">Population</TabsTrigger>
          <TabsTrigger value="lineage">Lineage</TabsTrigger>
          <TabsTrigger value="learning">Learning</TabsTrigger>
          <TabsTrigger value="config">Config</TabsTrigger>
        </TabsList>

        <TabsContent value="overview" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Generation Progress</CardTitle>
            </CardHeader>
            <CardContent>
              <GenerationChart points={generationData} baselineAccuracy={baselineAcc} />
            </CardContent>
          </Card>

          <div className="grid gap-4 lg:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle>Budget Consumption</CardTitle>
              </CardHeader>
              <CardContent>
                <BudgetChart
                  points={generationData.map((point) => ({
                    generation: point.generation,
                    cumulative_cost: point.cumulative_cost,
                  }))}
                  budgetCap={run.total_budget_usd}
                />
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle>Best Organism</CardTitle>
              </CardHeader>
              <CardContent>
                {bestOrganism?.fitness ? (
                  <div className="space-y-2 text-sm">
                    <p className="font-mono text-xs text-zinc-600">{bestOrganism.id}</p>
                    <p>
                      Generation {bestOrganism.generation} · {bestOrganism.mutation_type}
                    </p>
                    <div className="grid grid-cols-2 gap-2 text-xs">
                      <MetricCell label="Score" value={bestOrganism.fitness.composite_score.toFixed(4)} />
                      <MetricCell
                        label="Adj. Acc"
                        value={`${(bestOrganism.fitness.adjusted_accuracy * 100).toFixed(2)}%`}
                      />
                      <MetricCell label="Parse" value={`${(bestOrganism.fitness.parse_rate * 100).toFixed(2)}%`} />
                      <MetricCell label="Cost / Item" value={formatCost(bestOrganism.fitness.cost_per_item)} />
                      <MetricCell label="Avg Lat" value={`${bestOrganism.fitness.avg_latency_ms.toFixed(0)}ms`} />
                      <MetricCell label="P95 Lat" value={`${bestOrganism.fitness.p95_latency_ms.toFixed(0)}ms`} />
                    </div>
                    <div className="flex gap-2 pt-2">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => navigate(`/admin/optimize/${run.id}/organisms/${bestOrganism.id}`)}
                      >
                        View Details
                      </Button>
                      {bestOrganism.bench_run_id ? (
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => navigate(`/admin/benchmarks/${bestOrganism.bench_run_id}`)}
                        >
                          View Benchmark
                        </Button>
                      ) : null}
                    </div>
                  </div>
                ) : (
                  <p className="text-sm text-zinc-500">No best organism available yet.</p>
                )}
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        <TabsContent value="population" className="space-y-4">
          <Card>
            <CardHeader className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
              <CardTitle>Population</CardTitle>
              <Select
                value={generationFilter}
                onChange={(event) => setParam('generation', event.target.value)}
                className="w-40"
              >
                <option value="">All generations</option>
                {[...new Set(organisms.map((organism) => organism.generation))]
                  .sort((a, b) => a - b)
                  .map((generation) => (
                    <option key={generation} value={generation}>
                      {generation}
                    </option>
                  ))}
              </Select>
            </CardHeader>
            <CardContent>
              {sortedPopulation.length === 0 ? (
                <EmptyState message="No organisms found for this filter." />
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>ID</TableHead>
                      <TableHead>Gen</TableHead>
                      <TableHead>Parents</TableHead>
                      <TableHead>Mutation</TableHead>
                      <TableHead>Score</TableHead>
                      <TableHead>Adj. Acc</TableHead>
                      <TableHead>Parse</TableHead>
                      <TableHead>Cost / Item</TableHead>
                      <TableHead>Feasible</TableHead>
                      <TableHead>Bench Run</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {sortedPopulation.map((organism) => (
                      <TableRow
                        key={organism.id}
                        className="cursor-pointer"
                        onClick={() => navigate(`/admin/optimize/${run.id}/organisms/${organism.id}`)}
                      >
                        <TableCell className="font-mono text-xs">{organism.id}</TableCell>
                        <TableCell>{organism.generation}</TableCell>
                        <TableCell>{organism.parent_ids.length}</TableCell>
                        <TableCell>{organism.mutation_type || '-'}</TableCell>
                        <TableCell>{organism.fitness ? organism.fitness.composite_score.toFixed(4) : '-'}</TableCell>
                        <TableCell>
                          {organism.fitness ? `${(organism.fitness.adjusted_accuracy * 100).toFixed(2)}%` : '-'}
                        </TableCell>
                        <TableCell>
                          {organism.fitness ? `${(organism.fitness.parse_rate * 100).toFixed(2)}%` : '-'}
                        </TableCell>
                        <TableCell>{organism.fitness ? formatCost(organism.fitness.cost_per_item) : '-'}</TableCell>
                        <TableCell>
                          {organism.fitness?.feasible === false ? (
                            <span className="text-amber-600">!</span>
                          ) : (
                            <span className="text-emerald-600">✓</span>
                          )}
                        </TableCell>
                        <TableCell>
                          {organism.bench_run_id ? (
                            <Link
                              className="font-mono text-xs text-sky-700 hover:underline"
                              to={`/admin/benchmarks/${organism.bench_run_id}`}
                              onClick={(event) => event.stopPropagation()}
                            >
                              {organism.bench_run_id.slice(0, 14)}
                            </Link>
                          ) : (
                            '-'
                          )}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="lineage" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Lineage DAG</CardTitle>
            </CardHeader>
            <CardContent>
              <LineageDAG
                nodes={lineage.nodes}
                edges={lineage.edges}
                bestOrganismID={run.best_organism_id}
                fitnessLookup={Object.fromEntries(
                  organisms
                    .filter((organism) => organism.fitness)
                    .map((organism) => [organism.id, { adjusted_accuracy: organism.fitness?.adjusted_accuracy }]),
                )}
                onSelect={(organismID) => setSelectedLineageNode(organismID)}
                onOpenOrganism={(organismID) => navigate(`/admin/optimize/${run.id}/organisms/${organismID}`)}
              />
            </CardContent>
          </Card>

          {selectedOrganism && (
            <Card>
              <CardHeader>
                <CardTitle className="text-base">Selected: {selectedOrganism.id}</CardTitle>
              </CardHeader>
              <CardContent className="flex items-center justify-between gap-4">
                <div className="text-sm text-zinc-600">
                  Gen {selectedOrganism.generation} · {selectedOrganism.mutation_type || 'unknown'} ·{' '}
                  {selectedOrganism.fitness
                    ? `Adj. acc ${(selectedOrganism.fitness.adjusted_accuracy * 100).toFixed(2)}%`
                    : 'No fitness'}
                </div>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => navigate(`/admin/optimize/${run.id}/organisms/${selectedOrganism.id}`)}
                >
                  View Details
                </Button>
              </CardContent>
            </Card>
          )}
        </TabsContent>

        <TabsContent value="learning" className="space-y-4">
          <Card>
            <CardHeader className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
              <CardTitle>Learning Log</CardTitle>
              <Select
                value={learningOutcomeFilter}
                onChange={(event) => setParam('outcome', event.target.value)}
                className="w-52"
              >
                <option value="all">All ({learningCounts.all})</option>
                <option value="improvement">Improvement ({learningCounts.improvement})</option>
                <option value="regression">Regression ({learningCounts.regression})</option>
                <option value="no_change">No change ({learningCounts.no_change})</option>
                <option value="constraint_violation">
                  Constraint violation ({learningCounts.constraint_violation})
                </option>
              </Select>
            </CardHeader>
            <CardContent>
              <p className="mb-3 text-xs text-zinc-500">
                {learningCounts.all} entries · {learningCounts.improvement} improvements · {learningCounts.regression}{' '}
                regressions · {learningCounts.no_change} no change · {learningCounts.constraint_violation} violations
              </p>
              {filteredLearning.length === 0 ? (
                <EmptyState message="No learning entries for this filter." />
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Gen</TableHead>
                      <TableHead>Organism</TableHead>
                      <TableHead>Parent</TableHead>
                      <TableHead>Type</TableHead>
                      <TableHead>Verify</TableHead>
                      <TableHead>Outcome</TableHead>
                      <TableHead>Delta</TableHead>
                      <TableHead>Description</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {filteredLearning.map((entry, index) => (
                      <TableRow key={`${entry.organism_id}-${entry.created_at}-${index}`}>
                        <TableCell>{entry.generation}</TableCell>
                        <TableCell>
                          <Link
                            className="font-mono text-xs text-sky-700 hover:underline"
                            to={`/admin/optimize/${run.id}/organisms/${entry.organism_id}`}
                          >
                            {entry.organism_id.slice(0, 14)}
                          </Link>
                        </TableCell>
                        <TableCell className="font-mono text-xs">
                          {entry.parent_id ? entry.parent_id.slice(0, 14) : '-'}
                        </TableCell>
                        <TableCell>{entry.mutation_type || '-'}</TableCell>
                        <TableCell>{entry.verify_method || '-'}</TableCell>
                        <TableCell>{entry.outcome}</TableCell>
                        <TableCell className={entry.fitness_delta >= 0 ? 'text-emerald-700' : 'text-red-700'}>
                          {entry.fitness_delta >= 0 ? '+' : ''}
                          {entry.fitness_delta.toFixed(4)}
                        </TableCell>
                        <TableCell className="max-w-[340px] truncate" title={entry.description}>
                          {entry.description}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="config" className="space-y-4">
          <Card>
            <CardHeader>
              <CardTitle>Run Settings</CardTitle>
            </CardHeader>
            <CardContent className="grid gap-2 text-sm md:grid-cols-2">
              <ConfigCell label="Population" value={String(run.population_size)} />
              <ConfigCell label="Concurrency" value={String(run.concurrency)} />
              <ConfigCell label="Item limit" value={String(run.item_limit)} />
              <ConfigCell label="Mutator" value={run.mutator_mode || 'hybrid'} />
              <ConfigCell label="Claude model" value={run.claude_model || '-'} />
              <ConfigCell label="RNG seed" value={run.rng_seed != null ? String(run.rng_seed) : '-'} />
              <ConfigCell label="Children / parent" value={String(run.children_per_parent)} />
              <ConfigCell label="Max children / generation" value={String(run.max_children_per_generation)} />
              <ConfigCell label="Adaptive fanout" value={run.adaptive_fanout ? 'on' : 'off'} />
              <ConfigCell label="Compact artifacts" value={run.compact_artifacts ? 'on' : 'off'} />
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Spec</CardTitle>
            </CardHeader>
            <CardContent>
              <pre className="max-h-[560px] overflow-auto rounded-md border border-zinc-200 bg-zinc-50 p-3 text-xs">
                {JSON.stringify(run.spec, null, 2)}
              </pre>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}

function MetricCell({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-zinc-200 bg-zinc-50 px-2 py-1">
      <div className="text-[10px] uppercase tracking-wide text-zinc-500">{label}</div>
      <div className="text-xs font-semibold text-zinc-800">{value}</div>
    </div>
  );
}

function ConfigCell({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-zinc-200 bg-zinc-50 px-3 py-2">
      <div className="text-[10px] uppercase tracking-wide text-zinc-500">{label}</div>
      <div className="text-sm font-medium text-zinc-800">{value}</div>
    </div>
  );
}

function maxGenerationsFromSpec(run: OptimizationRun): number | null {
  const spec = run.spec;
  if (!spec || typeof spec !== 'object') return null;
  const stopPolicy = (spec as { stop_policy?: unknown }).stop_policy;
  if (!stopPolicy || typeof stopPolicy !== 'object') return null;
  const maxGenerations = (stopPolicy as { max_generations?: unknown }).max_generations;
  if (typeof maxGenerations !== 'number') return null;
  return maxGenerations;
}
