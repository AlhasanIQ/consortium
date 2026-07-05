import { useEffect, useRef, useState } from 'react';
import {
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';

export interface GenerationChartPoint {
  generation: number;
  best_accuracy: number;
  mean_accuracy: number;
  worst_accuracy: number;
}

export function GenerationChart({
  points,
  baselineAccuracy,
}: {
  points: GenerationChartPoint[];
  baselineAccuracy?: number;
}) {
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
    return <p className="text-sm text-zinc-500">No generation data yet.</p>;
  }

  return (
    <div ref={containerRef} className="h-[280px] w-full min-w-0">
      {hasSize ? (
        <ResponsiveContainer width="100%" height="100%" minWidth={0}>
          <LineChart data={points} margin={{ top: 10, right: 8, left: 0, bottom: 0 }}>
            <CartesianGrid strokeDasharray="3 3" stroke="#e4e4e7" />
            <XAxis dataKey="generation" tick={{ fontSize: 11 }} />
            <YAxis tick={{ fontSize: 11 }} tickFormatter={(v) => `${(Number(v) * 100).toFixed(1)}%`} />
            <Tooltip
              formatter={(value: number | string | undefined, name: string | undefined) => {
                const label = (name || 'metric').replace('_accuracy', '').replace(/^./, (m) => m.toUpperCase());
                return [`${(Number(value ?? 0) * 100).toFixed(2)}%`, label];
              }}
              labelFormatter={(value) => `Generation ${value}`}
            />
            <Legend />
            {typeof baselineAccuracy === 'number' && (
              <ReferenceLine
                y={baselineAccuracy}
                stroke="#71717a"
                strokeDasharray="6 4"
                label={{ value: `baseline ${(baselineAccuracy * 100).toFixed(1)}%`, position: 'insideBottomRight' }}
              />
            )}
            <Line type="monotone" dataKey="best_accuracy" name="Best" stroke="#10b981" strokeWidth={2} dot={{ r: 3 }} />
            <Line
              type="monotone"
              dataKey="mean_accuracy"
              name="Mean"
              stroke="#71717a"
              strokeWidth={1.5}
              strokeDasharray="6 3"
              dot={false}
            />
            <Line
              type="monotone"
              dataKey="worst_accuracy"
              name="Worst"
              stroke="#a1a1aa"
              strokeWidth={1.5}
              strokeDasharray="3 3"
              dot={false}
            />
          </LineChart>
        </ResponsiveContainer>
      ) : null}
    </div>
  );
}
