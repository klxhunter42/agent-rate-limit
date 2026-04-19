import { AreaChart, Area, XAxis, YAxis, Tooltip, ResponsiveContainer, CartesianGrid } from 'recharts';
import { useMetricsHistory } from '@/hooks/use-metrics-history';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import type { ParsedMetric } from '@/lib/api';

interface LatencyChartProps {
  metrics: ParsedMetric[];
  maxPoints?: number;
}

export function LatencyChart({ metrics, maxPoints = 60 }: LatencyChartProps) {
  const { latencyHistory } = useMetricsHistory(metrics, maxPoints);

  if (latencyHistory.length === 0) {
    return (
      <Card className="h-full">
        <CardHeader><CardTitle className="text-base">Latency</CardTitle></CardHeader>
        <CardContent>
          <div className="h-48 flex items-center justify-center text-muted-foreground text-sm">
            No latency data
          </div>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className="h-full">
      <CardHeader><CardTitle className="text-base">Latency (avg ms)</CardTitle></CardHeader>
      <CardContent>
        <ResponsiveContainer width="100%" height={260}>
          <AreaChart data={latencyHistory}>
            <CartesianGrid strokeDasharray="3 3" className="opacity-30" />
            <XAxis dataKey="time" tick={{ fontSize: 10 }} interval="preserveStartEnd" />
            <YAxis tick={{ fontSize: 10 }} unit="ms" />
            <Tooltip
              content={({ active, payload, label }) => {
                if (!active || !payload?.length) return null;
                return (
                  <div className="rounded-lg border bg-background p-2 shadow-sm text-xs">
                    <p className="font-medium mb-1">{label}</p>
                    <p>Avg: {Number(payload[0]?.value ?? 0).toFixed(1)}ms</p>
                  </div>
                );
              }}
            />
            <Area
              type="monotone"
              dataKey="avg"
              stroke="#8b5cf6"
              fill="#8b5cf6"
              fillOpacity={0.2}
              strokeWidth={2}
            />
          </AreaChart>
        </ResponsiveContainer>
      </CardContent>
    </Card>
  );
}
