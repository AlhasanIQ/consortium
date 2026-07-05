import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { cn } from '@/lib/utils';

const colorMap = {
  teal: 'from-teal-500/80 via-cyan-500/80 to-sky-500/80',
  emerald: 'from-emerald-500/80 via-emerald-400/80 to-teal-500/80',
  amber: 'from-amber-500/80 via-orange-400/80 to-yellow-500/80',
  red: 'from-red-500/80 via-rose-400/80 to-pink-500/80',
  sky: 'from-sky-500/80 via-blue-400/80 to-indigo-500/80',
  purple: 'from-indigo-500/80 via-blue-500/80 to-cyan-500/80',
  zinc: 'from-zinc-400/80 via-zinc-300/80 to-zinc-400/80',
} as const;

type StatColor = keyof typeof colorMap;

export function StatCard({
  label,
  value,
  hint,
  color = 'teal',
  className,
}: {
  label: string;
  value: string | number;
  hint?: string;
  color?: StatColor;
  className?: string;
}) {
  return (
    <Card className={cn('relative overflow-hidden', className)}>
      <div className={cn('absolute inset-x-0 top-0 h-1 bg-gradient-to-r', colorMap[color])} />
      <CardHeader className="px-4 pb-0.5 pt-4">
        <CardTitle className="text-muted-foreground text-[11px] font-semibold uppercase tracking-[0.14em]">
          {label}
        </CardTitle>
      </CardHeader>
      <CardContent className="px-4 pb-4">
        <p className="text-foreground text-2xl font-bold tracking-tight">{value}</p>
        {hint ? <p className="text-muted-foreground mt-1 text-xs">{hint}</p> : null}
      </CardContent>
    </Card>
  );
}
