import { useEffect, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { type AdminJobsResponse, type AdminOverviewResponse, adminClient } from '@/api/adminClient';
import { EmptyState } from '@/components/admin/EmptyState';
import { StatCard } from '@/components/admin/StatCard';
import { StatusBadge } from '@/components/admin/StatusBadge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { formatCost, relativeTime } from '@/lib/formatters';

export default function AdminOverviewPage() {
  const navigate = useNavigate();
  const [overview, setOverview] = useState<AdminOverviewResponse | null>(null);
  const [jobs, setJobs] = useState<AdminJobsResponse | null>(null);
  const [error, setError] = useState<string>('');

  useEffect(() => {
    let mounted = true;
    Promise.all([adminClient.getOverview(), adminClient.listJobs({ limit: 8, show: 'all' })])
      .then(([overviewData, jobsData]) => {
        if (!mounted) return;
        setOverview(overviewData);
        setJobs(jobsData);
      })
      .catch((err: Error) => {
        if (!mounted) return;
        setError(err.message || 'Failed to load overview');
      });
    return () => {
      mounted = false;
    };
  }, []);

  if (error) {
    return <EmptyState message={error} />;
  }

  if (!overview || !jobs) {
    return <EmptyState message="Loading admin overview..." />;
  }

  const recentJobs = jobs.Jobs ?? [];

  return (
    <div className="space-y-6">
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-6">
        <StatCard label="Total Jobs" value={overview.Stats.TotalJobs} color="teal" />
        <StatCard label="Completed" value={overview.Stats.CompletedJobs} color="emerald" />
        <StatCard label="Failed" value={overview.Stats.FailedJobs} color="red" />
        <StatCard label="Total Cost" value={formatCost(overview.Stats.TotalCost)} color="amber" />
        <StatCard label="Total Tokens" value={overview.Stats.TotalTokens.toLocaleString()} color="sky" />
        <StatCard label="Workflows" value={overview.Stats.TotalWorkflows} color="purple" />
      </div>

      {/* Quick-link buttons */}
      <div className="flex flex-wrap gap-3">
        <Button variant="outline" asChild>
          <Link to="/admin/jobs">View Jobs</Link>
        </Button>
        <Button variant="outline" asChild>
          <Link to="/admin/workflows">View Workflows</Link>
        </Button>
        <Button variant="outline" asChild>
          <Link to="/admin/benchmarks">View Benchmarks</Link>
        </Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Recent Jobs</CardTitle>
        </CardHeader>
        <CardContent>
          {recentJobs.length === 0 ? (
            <EmptyState message="No jobs found." />
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>ID</TableHead>
                  <TableHead>Workflow</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Tokens</TableHead>
                  <TableHead>Cost</TableHead>
                  <TableHead>Created</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {recentJobs.map((job) => (
                  <TableRow
                    key={job.id}
                    className="cursor-pointer transition-colors hover:bg-zinc-50"
                    onClick={() => navigate(`/admin/jobs/${job.id}`)}
                  >
                    <TableCell className="font-mono text-xs">
                      <Link
                        className="underline-offset-4 hover:underline"
                        to={`/admin/jobs/${job.id}`}
                        onClick={(e) => e.stopPropagation()}
                      >
                        {job.id.slice(0, 8)}
                      </Link>
                    </TableCell>
                    <TableCell className="max-w-[260px] truncate">{job.description}</TableCell>
                    <TableCell>
                      <StatusBadge status={job.status} />
                    </TableCell>
                    <TableCell>{job.tokens_total.toLocaleString()}</TableCell>
                    <TableCell>{formatCost(job.DisplayCost)}</TableCell>
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
    </div>
  );
}
