import { useCallback, useEffect, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { type AdminJobsResponse, type AdminStatus, adminClient } from '@/api/adminClient';
import { Breadcrumbs } from '@/components/admin/Breadcrumbs';
import { EmptyState } from '@/components/admin/EmptyState';
import { LiveIndicator } from '@/components/admin/LiveIndicator';
import { SortableTableHead } from '@/components/admin/SortableTableHead';
import { StatCard } from '@/components/admin/StatCard';
import { StatusBadge } from '@/components/admin/StatusBadge';
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
import {
  Pagination,
  PaginationContent,
  PaginationItem,
  PaginationNext,
  PaginationPrevious,
} from '@/components/ui/pagination';
import { Select } from '@/components/ui/select';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { formatCost, relativeTime } from '@/lib/formatters';
import { usePolling } from '@/lib/usePolling';
import { applySortParams, parseSortParams } from '@/lib/useSort';

const statusOptions: Array<{ label: string; value: AdminStatus }> = [
  { label: 'All', value: '' },
  { label: 'Pending', value: 'pending' },
  { label: 'Running', value: 'running' },
  { label: 'Paused', value: 'paused' },
  { label: 'Completed', value: 'completed' },
  { label: 'Failed', value: 'failed' },
  { label: 'Cancelled', value: 'cancelled' },
  { label: 'Archived', value: 'archived' },
];

interface ConfirmDialogState {
  open: boolean;
  title: string;
  description: string;
  action: (() => Promise<unknown>) | null;
}

export default function AdminJobsPage() {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const [data, setData] = useState<AdminJobsResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string>('');
  const [confirmDialog, setConfirmDialog] = useState<ConfirmDialogState>({
    open: false,
    title: '',
    description: '',
    action: null,
  });

  const show = (searchParams.get('show') as 'parents' | 'children' | 'all') || 'parents';
  const status = (searchParams.get('status') as AdminStatus) || '';
  const page = Number(searchParams.get('page') || '1');
  const limit = Number(searchParams.get('limit') || '25');
  const sort = parseSortParams(searchParams, 'created_at', 'desc');

  const load = useCallback(
    (isPolling = false) => {
      if (!isPolling) {
        setLoading(true);
        setError('');
      }
      adminClient
        .listJobs({ show, status, page, limit, sort: sort.column, dir: sort.direction })
        .then((response) => setData(response))
        .catch((err: Error) => {
          if (!isPolling) setError(err.message || 'Failed to load jobs');
        })
        .finally(() => {
          if (!isPolling) setLoading(false);
        });
    },
    [show, status, page, limit, sort.column, sort.direction],
  );

  const poll = useCallback(() => load(true), [load]);

  useEffect(() => {
    load();
  }, [load]);

  usePolling(poll, 3000, data?.HasActiveJobs ?? false);

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

  const requestAction = (title: string, description: string, action: () => Promise<unknown>) => {
    setConfirmDialog({ open: true, title, description, action });
  };

  const executeConfirmedAction = async () => {
    if (confirmDialog.action) {
      await confirmDialog.action();
      load();
    }
    setConfirmDialog({ open: false, title: '', description: '', action: null });
  };

  const handleSort = (column: string) => {
    applySortParams(searchParams, setSearchParams, column, sort, true);
  };

  const rows = data?.Jobs ?? [];
  const hasActiveJobs = data?.HasActiveJobs ?? false;

  return (
    <div className="space-y-4">
      <Breadcrumbs items={[{ label: 'Admin', to: '/admin' }, { label: 'Jobs' }]} />

      <div className="flex items-center gap-3">
        <h1 className="text-foreground text-2xl font-bold tracking-tight">Jobs</h1>
        {hasActiveJobs && <LiveIndicator />}
      </div>

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4 2xl:grid-cols-7">
        <StatCard label="Workers" value={`${data?.ActiveWorkflows || 0}/${data?.PoolCapacity || 0}`} color="teal" />
        <StatCard label="Pending" value={data?.PendingJobs || 0} color="amber" />
        <StatCard label="Running" value={data?.RunningJobs || 0} color="sky" />
        <StatCard label="Paused" value={data?.PausedJobs || 0} color="purple" />
        <StatCard label="Completed" value={data?.CompletedJobs || 0} color="emerald" />
        <StatCard label="Failed" value={data?.FailedJobs || 0} color="red" />
        <StatCard label="Cancelled" value={data?.CancelledJobs || 0} color="zinc" />
      </div>

      <Card>
        <CardHeader className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
          <CardTitle>Jobs</CardTitle>
          <div className="flex flex-wrap items-center gap-2">
            <Button
              size="sm"
              variant="outline"
              onClick={() =>
                requestAction('Pause Pending Jobs', 'Are you sure you want to pause all pending jobs?', () =>
                  adminClient.pauseAllJobs(),
                )
              }
            >
              Pause Pending
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={() =>
                requestAction('Resume Paused Jobs', 'Are you sure you want to resume all paused jobs?', () =>
                  adminClient.resumeAllJobs(),
                )
              }
            >
              Resume Paused
            </Button>
            <Button
              size="sm"
              variant="destructive"
              onClick={() =>
                requestAction(
                  'Cancel Active Jobs',
                  'Are you sure you want to cancel all active jobs? This cannot be undone.',
                  () => adminClient.cancelAllJobs(),
                )
              }
            >
              Cancel Active
            </Button>
          </div>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex flex-wrap gap-2">
            <Select value={show} onChange={(event) => setParam('show', event.target.value)} className="w-40">
              <option value="parents">Parents</option>
              <option value="children">Children</option>
              <option value="all">All</option>
            </Select>
            <Select value={status} onChange={(event) => setParam('status', event.target.value)} className="w-40">
              {statusOptions.map((opt) => (
                <option key={opt.label} value={opt.value}>
                  {opt.label}
                </option>
              ))}
            </Select>
            <Button size="sm" variant="secondary" onClick={() => load()}>
              Refresh
            </Button>
          </div>

          {loading ? <EmptyState message="Loading jobs..." /> : null}
          {error ? <EmptyState message={error} /> : null}
          {!loading && !error && data && rows.length === 0 ? <EmptyState message="No jobs found." /> : null}

          {!loading && !error && data && rows.length > 0 ? (
            <>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>ID</TableHead>
                    <TableHead>Workflow</TableHead>
                    <TableHead>Model</TableHead>
                    <TableHead>Prompt</TableHead>
                    <SortableTableHead column="status" label="Status" sort={sort} onSort={handleSort} />
                    <SortableTableHead column="tokens" label="Tokens" sort={sort} onSort={handleSort} />
                    <SortableTableHead column="cost" label="Cost" sort={sort} onSort={handleSort} />
                    <SortableTableHead column="created_at" label="Created" sort={sort} onSort={handleSort} />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {rows.map((job) => (
                    <TableRow
                      key={job.id}
                      className="cursor-pointer transition-colors hover:bg-zinc-50"
                      onClick={() => navigate(`/admin/jobs/${job.id}`)}
                    >
                      <TableCell className="font-mono text-xs">
                        <span className="inline-flex items-center gap-1.5">
                          {job.id.slice(0, 8)}
                          {job.IsChild && job.ParentExecutionID && (
                            <Link
                              to={`/admin/jobs/${job.ParentExecutionID}`}
                              onClick={(e) => e.stopPropagation()}
                              title="View parent job"
                            >
                              <Badge
                                variant="outline"
                                className="border-purple-200 bg-purple-50 text-purple-700 text-[10px] cursor-pointer hover:bg-purple-100"
                              >
                                &larr; child
                              </Badge>
                            </Link>
                          )}
                          {!job.IsChild && job.DescendantCount > 0 && (
                            <Badge
                              variant="outline"
                              className="border-indigo-200 bg-indigo-50 text-indigo-700 text-[10px]"
                            >
                              parent &middot; {job.DescendantCount}
                            </Badge>
                          )}
                        </span>
                      </TableCell>
                      <TableCell className="max-w-[200px] truncate">{job.description}</TableCell>
                      <TableCell className="text-muted-foreground max-w-[160px] truncate text-xs">
                        {job.model || '-'}
                      </TableCell>
                      <TableCell className="max-w-[260px] truncate">{job.InputPrompt || '-'}</TableCell>
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

              <div className="flex items-center justify-between">
                <p className="text-muted-foreground text-xs">
                  Showing {data.Pagination.StartItem}-{data.Pagination.EndItem} of {data.Pagination.TotalItems}
                </p>
                <Pagination className="mx-0 w-auto justify-end">
                  <PaginationContent>
                    <PaginationItem>
                      <PaginationPrevious
                        href="#"
                        className={!data.Pagination.HasPrev ? 'pointer-events-none opacity-50' : ''}
                        onClick={(event) => {
                          event.preventDefault();
                          if (data.Pagination.HasPrev) setParam('page', String(page - 1));
                        }}
                      />
                    </PaginationItem>
                    <PaginationItem>
                      <span className="text-muted-foreground px-2 text-xs">Page {data.Pagination.Page}</span>
                    </PaginationItem>
                    <PaginationItem>
                      <PaginationNext
                        href="#"
                        className={!data.Pagination.HasNext ? 'pointer-events-none opacity-50' : ''}
                        onClick={(event) => {
                          event.preventDefault();
                          if (data.Pagination.HasNext) setParam('page', String(page + 1));
                        }}
                      />
                    </PaginationItem>
                  </PaginationContent>
                </Pagination>
              </div>
            </>
          ) : null}
        </CardContent>
      </Card>

      <Dialog
        open={confirmDialog.open}
        onOpenChange={(open) => {
          if (!open) setConfirmDialog((prev) => ({ ...prev, open: false }));
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{confirmDialog.title}</DialogTitle>
            <DialogDescription>{confirmDialog.description}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <DialogClose asChild>
              <Button variant="outline">Cancel</Button>
            </DialogClose>
            <Button variant="destructive" onClick={executeConfirmedAction}>
              Confirm
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
