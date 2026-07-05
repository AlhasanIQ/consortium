import { ExternalLink } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { type AdminWorkflowDetailResponse, adminClient } from '@/api/adminClient';
import { Breadcrumbs } from '@/components/admin/Breadcrumbs';
import { EmptyState } from '@/components/admin/EmptyState';
import { StatCard } from '@/components/admin/StatCard';
import { StatusBadge } from '@/components/admin/StatusBadge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { formatCost, formatTokens, relativeTime } from '@/lib/formatters';

export default function AdminWorkflowDetailPage() {
  const { id = '' } = useParams();
  const [data, setData] = useState<AdminWorkflowDetailResponse | null>(null);
  const [error, setError] = useState('');
  const navigate = useNavigate();

  useEffect(() => {
    if (!id) return;
    adminClient
      .getWorkflow(id)
      .then((response) => setData(response))
      .catch((err: Error) => setError(err.message || 'Failed to load workflow'));
  }, [id]);

  if (error) return <EmptyState message={error} />;
  if (!data) return <EmptyState message="Loading workflow..." />;

  const recentExecutions = data.RecentExecutions ?? [];
  const workflowName = data.Workflow.name || data.Workflow.id;

  return (
    <div className="space-y-4">
      <Breadcrumbs
        items={[
          { label: 'Admin', to: '/admin' },
          { label: 'Workflows', to: '/admin/workflows' },
          { label: workflowName },
        ]}
      />

      <Card>
        <CardHeader className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
          <div>
            <CardTitle>{workflowName}</CardTitle>
            <p className="text-xs text-zinc-500">{data.Workflow.id}</p>
          </div>
          <a href={`${data.FrontendURL}/workflow/${data.Workflow.id}`} target="_blank" rel="noreferrer">
            <Button size="sm" className="gap-1.5">
              Open In Builder
              <ExternalLink className="h-3.5 w-3.5" />
            </Button>
          </a>
        </CardHeader>
      </Card>

      <div className="grid gap-4 md:grid-cols-3 lg:grid-cols-6">
        <StatCard label="Nodes" value={data.Workflow.NodeCount} color="teal" />
        <StatCard label="Runs" value={data.Workflow.ExecutionCount} color="sky" />
        <StatCard label="Success Rate" value={`${data.Workflow.SuccessRate.toFixed(1)}%`} color="emerald" />
        <StatCard label="Tokens" value={formatTokens(data.Workflow.TotalTokens)} color="purple" />
        <StatCard label="Cost" value={formatCost(data.Workflow.TotalCost)} color="amber" />
        <StatCard label="Last Status" value={data.Workflow.LastRunStatus || '-'} color="zinc" />
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Recent Executions</CardTitle>
        </CardHeader>
        <CardContent>
          {recentExecutions.length === 0 ? (
            <EmptyState message="No executions found." />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>ID</TableHead>
                  <TableHead>Description</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Tokens</TableHead>
                  <TableHead>Cost</TableHead>
                  <TableHead>Created</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {recentExecutions.map((job) => (
                  <TableRow key={job.id} className="cursor-pointer" onClick={() => navigate(`/admin/jobs/${job.id}`)}>
                    <TableCell className="font-mono text-xs">{job.id.slice(0, 8)}</TableCell>
                    <TableCell className="max-w-[300px] truncate">{job.description}</TableCell>
                    <TableCell>
                      <StatusBadge status={job.status} />
                    </TableCell>
                    <TableCell>{formatTokens(job.tokens_total)}</TableCell>
                    <TableCell>{formatCost(job.cost)}</TableCell>
                    <TableCell title={new Date(job.created_at).toLocaleString()}>
                      {relativeTime(job.created_at)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Definition JSON</CardTitle>
        </CardHeader>
        <CardContent>
          <pre className="max-h-[520px] overflow-auto rounded-md bg-zinc-950 p-4 text-xs text-zinc-100">
            {data.DefinitionJSON}
          </pre>
        </CardContent>
      </Card>
    </div>
  );
}
