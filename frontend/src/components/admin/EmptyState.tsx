import { Skeleton } from '@/components/ui/skeleton';

export function EmptyState({ message, loading }: { message: string; loading?: boolean }) {
  if (loading) {
    return (
      <div className="space-y-3 py-4">
        <Skeleton className="h-4 w-3/4" />
        <Skeleton className="h-4 w-1/2" />
        <Skeleton className="h-4 w-2/3" />
      </div>
    );
  }
  return (
    <div className="text-muted-foreground bg-card rounded-lg border border-dashed p-6 text-center text-sm">
      {message}
    </div>
  );
}
