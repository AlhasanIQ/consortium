import { useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { adminClient, type WrongAnswerAnalysisResponse, type WrongAnswerCategory } from '@/api/adminClient';
import { Breadcrumbs } from '@/components/admin/Breadcrumbs';
import { EmptyState } from '@/components/admin/EmptyState';
import { StatCard } from '@/components/admin/StatCard';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { benchmarkItemDetailPath } from '@/lib/adminPaths';

const CATEGORY_META: Record<WrongAnswerCategory, { label: string; color: string; description: string }> = {
  all_steps_wrong: {
    label: 'All Steps Wrong',
    color: 'red',
    description: 'Every agent node answered incorrectly',
  },
  some_right_child_wrong: {
    label: 'Some Right, Child Wrong',
    color: 'orange',
    description: 'Some agents correct, but aggregation chose wrong',
  },
  all_right_child_wrong: {
    label: 'All Right, Child Wrong',
    color: 'amber',
    description: 'All agents correct, but judge returned wrong answer',
  },
  child_right_parent_wrong: {
    label: 'Child Right, Parent Wrong',
    color: 'yellow',
    description: 'Child workflow correct, but contract extraction failed',
  },
  unclassified: {
    label: 'Unclassified',
    color: 'zinc',
    description: 'Could not determine failure point',
  },
};

const CATEGORY_ORDER: WrongAnswerCategory[] = [
  'all_steps_wrong',
  'some_right_child_wrong',
  'all_right_child_wrong',
  'child_right_parent_wrong',
  'unclassified',
];

const STAT_COLORS: Record<WrongAnswerCategory, 'red' | 'amber' | 'purple' | 'sky' | 'zinc'> = {
  all_steps_wrong: 'red',
  some_right_child_wrong: 'purple',
  all_right_child_wrong: 'amber',
  child_right_parent_wrong: 'sky',
  unclassified: 'zinc',
};

export default function AdminBenchmarkAnalysisPage() {
  const { id = '' } = useParams();
  const navigate = useNavigate();
  const [data, setData] = useState<WrongAnswerAnalysisResponse | null>(null);
  const [error, setError] = useState('');
  const [activeFilter, setActiveFilter] = useState<WrongAnswerCategory | 'all'>('all');

  useEffect(() => {
    if (!id) return;
    adminClient
      .getWrongAnswerAnalysis(id)
      .then(setData)
      .catch((err: Error) => setError(err.message || 'Failed to load analysis'));
  }, [id]);

  const filteredItems = useMemo(() => {
    if (!data) return [];
    if (activeFilter === 'all') return data.items;
    return data.items.filter((item) => item.category === activeFilter);
  }, [data, activeFilter]);

  if (error) return <EmptyState message={error} />;
  if (!data) return <EmptyState message="Analyzing wrong answers..." />;

  const { summary } = data;

  return (
    <div className="space-y-4">
      <Breadcrumbs
        items={[
          { label: 'Admin', to: '/admin' },
          { label: 'Benchmarks', to: '/admin/benchmarks' },
          { label: id.slice(0, 12), to: `/admin/benchmarks/${id}` },
          { label: 'Wrong Answer Analysis' },
        ]}
      />

      <Card>
        <CardHeader>
          <CardTitle className="text-base">
            Wrong Answer Analysis &middot;{' '}
            <span className="font-mono text-sm font-normal text-zinc-500">{data.run_id.slice(0, 12)}</span>
          </CardTitle>
          <p className="text-sm text-zinc-500">
            {data.benchmark}/{data.split} &middot; {data.workflow_id} &middot; {summary.total_incorrect} incorrect items
          </p>
        </CardHeader>
      </Card>

      <div className="grid gap-4 md:grid-cols-3 xl:grid-cols-6">
        <StatCard label="Total Incorrect" value={summary.total_incorrect} color="red" />
        {CATEGORY_ORDER.map((cat) => {
          const count = summary[cat as keyof typeof summary] as number;
          const pct = summary.total_incorrect > 0 ? ((count / summary.total_incorrect) * 100).toFixed(1) : '0';
          return (
            <StatCard
              key={cat}
              label={CATEGORY_META[cat].label}
              value={count}
              hint={`${pct}%`}
              color={STAT_COLORS[cat]}
            />
          );
        })}
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Category Breakdown</CardTitle>
        </CardHeader>
        <CardContent>
          <CategoryBreakdownChart summary={summary} />
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
          <CardTitle>Items</CardTitle>
          <div className="flex flex-wrap gap-1.5">
            <FilterBadge
              label="All"
              count={summary.total_incorrect}
              active={activeFilter === 'all'}
              onClick={() => setActiveFilter('all')}
            />
            {CATEGORY_ORDER.map((cat) => {
              const count = summary[cat as keyof typeof summary] as number;
              if (count === 0) return null;
              return (
                <FilterBadge
                  key={cat}
                  label={CATEGORY_META[cat].label}
                  count={count}
                  active={activeFilter === cat}
                  onClick={() => setActiveFilter(cat)}
                />
              );
            })}
          </div>
        </CardHeader>
        <CardContent>
          {filteredItems.length === 0 ? (
            <EmptyState message="No items match the current filter." />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Item ID</TableHead>
                  <TableHead>Subject</TableHead>
                  <TableHead>Gold</TableHead>
                  <TableHead>Parent Predicted</TableHead>
                  <TableHead>Child Predicted</TableHead>
                  <TableHead>Agent Answers</TableHead>
                  <TableHead>Category</TableHead>
                  <TableHead>Job</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredItems.map((item) => (
                  <TableRow
                    key={item.item_id}
                    className="cursor-pointer"
                    onClick={() => navigate(benchmarkItemDetailPath(id, item.item_id))}
                  >
                    <TableCell className="font-mono text-xs">{item.item_id}</TableCell>
                    <TableCell className="max-w-[120px] truncate">{item.subject}</TableCell>
                    <TableCell className="font-medium">{item.answer_label}</TableCell>
                    <TableCell className={item.parent_predicted !== item.answer_label ? 'text-red-600' : ''}>
                      {item.parent_predicted || '-'}
                    </TableCell>
                    <TableCell
                      className={item.child_predicted !== item.answer_label ? 'text-red-600' : 'text-emerald-600'}
                    >
                      {item.child_predicted || '-'}
                    </TableCell>
                    <TableCell>
                      <div className="flex gap-0.5">
                        {(item.agent_answers ?? []).map((a) => (
                          <AgentBadge
                            key={a.node_id}
                            answer={a.answer}
                            correct={a.correct}
                            parseOK={a.parse_ok}
                            nodeId={a.node_id}
                          />
                        ))}
                      </div>
                    </TableCell>
                    <TableCell>
                      <span
                        className={`inline-block rounded-full px-2 py-0.5 text-[11px] font-medium ${categoryBadgeClasses(item.category)}`}
                      >
                        {CATEGORY_META[item.category].label}
                      </span>
                    </TableCell>
                    <TableCell className="font-mono text-xs">
                      {item.parent_job_id ? (
                        <button
                          type="button"
                          className="text-sky-600 hover:underline"
                          onClick={(e) => {
                            e.stopPropagation();
                            navigate(`/admin/jobs/${item.parent_job_id}`);
                          }}
                        >
                          {item.parent_job_id.slice(0, 8)}
                        </button>
                      ) : (
                        '-'
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
          <p className="text-muted-foreground mt-3 text-xs">Showing {filteredItems.length} items</p>
        </CardContent>
      </Card>
    </div>
  );
}

function AgentBadge({
  answer,
  correct,
  parseOK,
  nodeId,
}: {
  answer: string;
  correct: boolean;
  parseOK: boolean;
  nodeId: string;
}) {
  if (!parseOK) {
    return (
      <span
        title={`${nodeId}: unparseable`}
        className="inline-flex h-5 w-5 items-center justify-center rounded-sm bg-zinc-100 text-[10px] text-zinc-400"
      >
        ?
      </span>
    );
  }
  return (
    <span
      title={`${nodeId}: ${answer}`}
      className={`inline-flex h-5 w-5 items-center justify-center rounded-sm text-[10px] font-medium ${
        correct ? 'bg-emerald-100 text-emerald-700' : 'bg-red-100 text-red-700'
      }`}
    >
      {answer}
    </span>
  );
}

function FilterBadge({
  label,
  count,
  active,
  onClick,
}: {
  label: string;
  count: number;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`rounded-full border px-2.5 py-1 text-xs font-medium transition-colors ${
        active ? 'border-zinc-900 bg-zinc-900 text-white' : 'border-zinc-200 bg-white text-zinc-700 hover:bg-zinc-50'
      }`}
    >
      {label} ({count})
    </button>
  );
}

function CategoryBreakdownChart({ summary }: { summary: WrongAnswerAnalysisResponse['summary'] }) {
  const data: Array<[string, number, WrongAnswerCategory]> = CATEGORY_ORDER.map(
    (cat) =>
      [CATEGORY_META[cat].label, summary[cat as keyof typeof summary] as number, cat] as [
        string,
        number,
        WrongAnswerCategory,
      ],
  ).filter(([, count]) => count > 0);
  const max = Math.max(...data.map(([, v]) => v), 1);

  const barColors: Record<WrongAnswerCategory, string> = {
    all_steps_wrong: 'bg-red-500/80',
    some_right_child_wrong: 'bg-orange-500/80',
    all_right_child_wrong: 'bg-amber-500/80',
    child_right_parent_wrong: 'bg-yellow-500/80',
    unclassified: 'bg-zinc-400/80',
  };

  return (
    <div className="space-y-2">
      {data.map(([label, value, cat]) => {
        const width = (value / max) * 100;
        const pct = summary.total_incorrect > 0 ? ((value / summary.total_incorrect) * 100).toFixed(1) : '0';
        return (
          <div key={cat} className="space-y-1">
            <div className="flex items-center justify-between gap-2 text-xs">
              <span className="truncate font-medium text-zinc-700">{label}</span>
              <span className="text-zinc-500">
                {value} ({pct}%)
              </span>
            </div>
            <div className="h-2 rounded-full bg-zinc-100">
              <div className={`h-full rounded-full ${barColors[cat]}`} style={{ width: `${width}%` }} />
            </div>
          </div>
        );
      })}
    </div>
  );
}

function categoryBadgeClasses(category: WrongAnswerCategory): string {
  switch (category) {
    case 'all_steps_wrong':
      return 'bg-red-50 text-red-700';
    case 'some_right_child_wrong':
      return 'bg-orange-50 text-orange-700';
    case 'all_right_child_wrong':
      return 'bg-amber-50 text-amber-700';
    case 'child_right_parent_wrong':
      return 'bg-yellow-50 text-yellow-700';
    default:
      return 'bg-zinc-50 text-zinc-600';
  }
}
