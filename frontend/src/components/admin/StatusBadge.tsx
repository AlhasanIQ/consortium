import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';

export function StatusBadge({ status, className }: { status: string; className?: string }) {
  const normalized = (status || '').toLowerCase();
  const label = (status || 'unknown').toUpperCase();

  if (normalized === 'completed' || normalized === 'imported') {
    return (
      <Badge variant="success" className={className}>
        {label}
      </Badge>
    );
  }
  if (normalized === 'failed') {
    return (
      <Badge variant="destructive" className={className}>
        {label}
      </Badge>
    );
  }
  if (normalized === 'running') {
    return (
      <Badge variant="info" className={cn('gap-1.5', className)}>
        <span className="relative flex h-2 w-2">
          <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-sky-400 opacity-75" />
          <span className="relative inline-flex h-2 w-2 rounded-full bg-sky-500" />
        </span>
        {label}
      </Badge>
    );
  }
  if (normalized === 'pending') {
    return (
      <Badge variant="warning" className={className}>
        {label}
      </Badge>
    );
  }
  if (normalized === 'paused') {
    return (
      <Badge variant="outline" className={cn('border-amber-300/60 bg-amber-500/10 text-amber-700', className)}>
        {label}
      </Badge>
    );
  }
  if (normalized === 'cancelled') {
    return (
      <Badge variant="outline" className={cn('border-zinc-300 bg-zinc-100 text-zinc-600', className)}>
        {label}
      </Badge>
    );
  }
  if (normalized === 'archived') {
    return (
      <Badge variant="outline" className={cn('border-zinc-200 bg-zinc-50 text-zinc-400', className)}>
        {label}
      </Badge>
    );
  }
  return (
    <Badge variant="secondary" className={className}>
      {label}
    </Badge>
  );
}
