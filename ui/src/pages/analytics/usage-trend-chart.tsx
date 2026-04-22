import { useState, useMemo } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { InfoTip } from '@/components/shared/info-tip';
import { AreaChart, Area, XAxis, YAxis, Tooltip, ResponsiveContainer, CartesianGrid } from 'recharts';
import type { ParsedMetric } from '@/lib/api';
import { useMetricsHistory } from '@/hooks/use-metrics-history';
import { usePrivacy } from '@/contexts/privacy-context';
import { PRIVACY_BLUR_CLASS } from '@/lib/privacy';
import { cn } from '@/lib/utils';
import { formatNumber, formatCost } from '@/lib/format';

type Range = '2m' | '5m' | '10m';

const RANGE_POINTS: Record<Range, number> = { '2m': 24, '5m': 60, '10m': 120 };

export function UsageTrendChart({ metrics }: { metrics: ParsedMetric[] }) {
  const [range, setRange] = useState<Range>('5m');
  const { rateHistory, costHistory } = useMetricsHistory(metrics);
  const { privacyMode } = usePrivacy();

  const data = useMemo(() => {
    const points = RANGE_POINTS[range];
    const rates = rateHistory.slice(-points);
    const costs = costHistory.slice(-points);
    const len = Math.max(rates.length, costs.length);

    const result: Array<{ time: string; tokens: number; cost: number }> = [];
    for (let i = 0; i < len; i++) {
      const r = rates[rates.length - len + i];
      const c = costs[costs.length - len + i];
      const tokens = r ? Object.entries(r).filter(([k]) => k !== 'time').reduce((s, [, v]) => s + (v as number), 0) : 0;
      const cost = c ? Object.entries(c).filter(([k]) => k !== 'time').reduce((s, [, v]) => s + (v as number), 0) : 0;
      result.push({
        time: r?.time as string || c?.time as string || '',
        tokens,
        cost,
      });
    }
    return result;
  }, [rateHistory, costHistory, range]);

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <CardTitle className="text-base flex items-center gap-1.5">Usage Trend<InfoTip text="Token usage trend over time. Shows how consumption changes across polling intervals." /></CardTitle>
        <div className="flex gap-1">
          {(['2m', '5m', '10m'] as Range[]).map((r) => (
            <button
              key={r}
              onClick={() => setRange(r)}
              className={cn(
                'px-2 py-0.5 text-xs rounded transition-colors',
                range === r ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground hover:bg-muted/80',
              )}
            >
              {r}
            </button>
          ))}
        </div>
      </CardHeader>
      <CardContent>
        {data.length < 2 ? (
          <div className="h-64 flex items-center justify-center text-muted-foreground text-sm">
            Collecting data...
          </div>
        ) : (
          <ResponsiveContainer width="100%" height={280}>
            <AreaChart data={data}>
              <defs>
                <linearGradient id="tokenGrad" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor="#0080FF" stopOpacity={0.3} />
                  <stop offset="95%" stopColor="#0080FF" stopOpacity={0} />
                </linearGradient>
                <linearGradient id="costGrad" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="5%" stopColor="#00C49F" stopOpacity={0.3} />
                  <stop offset="95%" stopColor="#00C49F" stopOpacity={0} />
                </linearGradient>
              </defs>
              <CartesianGrid strokeDasharray="3 3" className="opacity-30" />
              <XAxis dataKey="time" tick={{ fontSize: 10 }} />
              <YAxis yAxisId="left" tick={{ fontSize: 10 }} tickFormatter={formatNumber} />
              <YAxis yAxisId="right" orientation="right" tick={{ fontSize: 10 }} tickFormatter={(v) => `$${v.toFixed(4)}`} />
              <Tooltip
                content={({ active, payload }) => {
                  if (!active || !payload?.length) return null;
                  const d = payload[0]?.payload as { tokens: number; cost: number; time: string } | undefined;
                  if (!d) return null;
                  return (
                    <div className="rounded-lg border bg-background p-2 shadow-sm text-xs">
                      <p className="font-medium mb-1">{d.time}</p>
                      <p className={cn(privacyMode && PRIVACY_BLUR_CLASS)}>
                        Tokens: {formatNumber(d.tokens)}
                      </p>
                      <p className={cn(privacyMode && PRIVACY_BLUR_CLASS)}>
                        Cost: {formatCost(d.cost)}
                      </p>
                    </div>
                  );
                }}
              />
              <Area yAxisId="left" type="monotone" dataKey="tokens" stroke="#0080FF" fill="url(#tokenGrad)" dot={false} />
              <Area yAxisId="right" type="monotone" dataKey="cost" stroke="#00C49F" fill="url(#costGrad)" dot={false} />
            </AreaChart>
          </ResponsiveContainer>
        )}
      </CardContent>
    </Card>
  );
}
