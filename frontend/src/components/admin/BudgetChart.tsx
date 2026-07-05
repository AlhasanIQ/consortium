import { useEffect, useRef, useState } from 'react';
import { Area, AreaChart, CartesianGrid, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';

export interface BudgetChartPoint {
  generation: number;
  cumulative_cost: number;
}

export function BudgetChart({ points, budgetCap }: { points: BudgetChartPoint[]; budgetCap: number }) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [hasSize, setHasSize] = useState(false);

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const observer = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (!entry) return;
      setHasSize(entry.contentRect.width > 0 && entry.contentRect.height > 0);
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  if (points.length === 0) {
    return <p className="text-sm text-zinc-500">No budget data yet.</p>;
  }

  return (
    <div ref={containerRef} className="h-[250px] w-full min-w-0">
      {hasSize ? (
        <ResponsiveContainer width="100%" height="100%" minWidth={0}>
          <AreaChart data={points} margin={{ top: 10, right: 8, left: 0, bottom: 0 }}>
            <defs>
              <linearGradient id="budgetFill" x1="0" y1="0" x2="0" y2="1">
                <stop offset="5%" stopColor="#34d399" stopOpacity={0.35} />
                <stop offset="95%" stopColor="#f59e0b" stopOpacity={0.08} />
              </linearGradient>
            </defs>
            <CartesianGrid strokeDasharray="3 3" stroke="#e4e4e7" />
            <XAxis dataKey="generation" tick={{ fontSize: 11 }} />
            <YAxis tick={{ fontSize: 11 }} tickFormatter={(v) => `$${Number(v).toFixed(2)}`} />
            <Tooltip
              formatter={(value: number | string | undefined) => [
                `$${Number(value ?? 0).toFixed(3)}`,
                'Cumulative spend',
              ]}
              labelFormatter={(value) => `Generation ${value}`}
            />
            <ReferenceLine
              y={budgetCap}
              stroke="#7c3aed"
              strokeDasharray="6 4"
              label={{ value: `budget $${budgetCap.toFixed(2)}`, position: 'insideBottomRight' }}
            />
            <Area
              type="monotone"
              dataKey="cumulative_cost"
              stroke="#10b981"
              strokeWidth={2}
              fill="url(#budgetFill)"
              dot={{ r: 3 }}
            />
          </AreaChart>
        </ResponsiveContainer>
      ) : null}
    </div>
  );
}
