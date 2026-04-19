import { AreaChart, Area, XAxis, YAxis, Tooltip, ResponsiveContainer, CartesianGrid } from 'recharts';
import { useMetricsHistory } from '@/hooks/use-metrics-history';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import type { ParsedMetric } from '@/lib/api';
import { CHART_COLORS } from '@/lib/providers';

interface ErrorRateChartProps {
  metrics: ParsedMetric[];
  maxPoints?: number;
}

export function ErrorRateChart({ metrics, maxPoints = 60 }: ErrorRateChartProps) {
  const { errorHistory } = useMetricsHistory(metrics, maxPoints);

  const types = Array.from(
    new Set(errorHistory.flatMap((pt) => Object.keys(pt).filter((k) => k !== 'time'))),
  );

  if (errorHistory.length === 0) {
    return (
      <Card className="h-full">
        <CardHeader><CardTitle className="text-base">Error Rate</CardTitle></CardHeader>
        <CardContent>
          <div className="h-48 flex items-center justify-center text-muted-foreground text-sm">
            No error data
          </div>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className="h-full">
      <CardHeader><CardTitle className="text-base">Error Rate</CardTitle></CardHeader>
      <CardContent>
        <ResponsiveContainer width="100%" height={260}>
          <AreaChart data={errorHistory}>
            <CartesianGrid strokeDasharray="3 3" className="opacity-30" />
            <XAxis dataKey="time" tick={{ fontSize: 10 }} interval="preserveStartEnd" />
            <YAxis tick={{ fontSize: 10 }} allowDecimals={false} />
            <Tooltip
              content={({ active, payload, label }) => {
                if (!active || !payload?.length) return null;
                return (
                  <div className="rounded-lg border bg-background p-2 shadow-sm text-xs">
                    <p className="font-medium mb-1">{label}</p>
                    {payload.map((p) => (
                      <p key={p.name} style={{ color: p.color }}>
                        {p.name}: {Number(p.value).toFixed(0)}
                      </p>
                    ))}
                  </div>
                );
              }}
            />
            {types.map((type, i) => (
              <Area
                key={type}
                type="monotone"
                dataKey={type}
                stackId="1"
                fill={CHART_COLORS[i % CHART_COLORS.length]}
                stroke={CHART_COLORS[i % CHART_COLORS.length]}
                fillOpacity={0.4}
              />
            ))}
          </AreaChart>
        </ResponsiveContainer>
      </CardContent>
    </Card>
  );
}
