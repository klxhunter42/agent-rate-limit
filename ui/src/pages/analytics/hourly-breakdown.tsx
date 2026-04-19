import { useState, useEffect, useRef, useMemo } from 'react';
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, CartesianGrid } from 'recharts';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { cn } from '@/lib/utils';
import { formatNumber, formatCost } from '@/lib/format';
import type { ParsedMetric } from '@/lib/api';

interface HourlyData {
  hour: string;
  requests: number;
  tokens: number;
  cost: number;
}

type MetricView = 'requests' | 'tokens' | 'cost';

const VIEW_COLORS: Record<MetricView, string> = {
  requests: '#0080FF',
  tokens: '#8b5cf6',
  cost: '#00C49F',
};

function extractDeltas(
  metrics: ParsedMetric[],
  prev: Map<string, number>,
): { requests: number; tokens: number; cost: number } {
  let requests = 0;
  let tokens = 0;
  let cost = 0;

  for (const m of metrics) {
    if (m.name === 'api_gateway_request_latency_seconds_count') {
      const prevVal = prev.get(m.name + m.labels.model) ?? 0;
      requests += m.value - prevVal;
      prev.set(m.name + m.labels.model, m.value);
    }
    if (m.name === 'api_gateway_token_input_total' || m.name === 'api_gateway_token_output_total') {
      const key = m.name + (m.labels.model || '');
      const prevVal = prev.get(key) ?? 0;
      tokens += m.value - prevVal;
      prev.set(key, m.value);
    }
    if (m.name === 'api_gateway_cost_total') {
      const key = m.name + (m.labels.model || '') + (m.labels.type || '');
      const prevVal = prev.get(key) ?? 0;
      cost += m.value - prevVal;
      prev.set(key, m.value);
    }
  }

  return { requests: Math.max(0, requests), tokens: Math.max(0, tokens), cost: Math.max(0, cost) };
}

function currentHourKey(): string {
  const now = new Date();
  return `${String(now.getHours()).padStart(2, '0')}:00`;
}

function buildEmptySlots(count: number): Map<string, HourlyData> {
  const map = new Map<string, HourlyData>();
  const now = new Date();
  for (let i = count - 1; i >= 0; i--) {
    const d = new Date(now.getTime() - i * 3600_000);
    const key = `${String(d.getHours()).padStart(2, '0')}:00`;
    map.set(key, { hour: key, requests: 0, tokens: 0, cost: 0 });
  }
  return map;
}

interface HourlyBreakdownProps {
  metrics: ParsedMetric[];
}

export function HourlyBreakdown({ metrics }: HourlyBreakdownProps) {
  const [view, setView] = useState<MetricView>('requests');
  const bucketsRef = useRef<Map<string, HourlyData>>(buildEmptySlots(24));
  const prevMetricsRef = useRef<Map<string, number>>(new Map());
  const initializedRef = useRef(false);

  useEffect(() => {
    if (metrics.length === 0) return;

    const prev = prevMetricsRef.current;

    // Skip first poll to establish baseline
    if (!initializedRef.current) {
      initializedRef.current = true;
      for (const m of metrics) {
        const key = m.name + (m.labels.model || '') + (m.labels.type || '');
        prev.set(key, m.value);
        // Also store request counter keyed by model
        if (m.name === 'api_gateway_request_latency_seconds_count') {
          prev.set(m.name + m.labels.model, m.value);
        }
      }
      return;
    }

    const deltas = extractDeltas(metrics, prev);
    const hourKey = currentHourKey();
    const buckets = bucketsRef.current;

    // Prune hours older than 24
    const currentHour = new Date().getHours();
    for (const key of buckets.keys()) {
      const h = parseInt(key, 10);
      const diff = (currentHour - h + 24) % 24;
      if (diff >= 24) buckets.delete(key);
    }

    const existing = buckets.get(hourKey);
    if (existing) {
      existing.requests += deltas.requests;
      existing.tokens += deltas.tokens;
      existing.cost += deltas.cost;
    } else {
      buckets.set(hourKey, {
        hour: hourKey,
        requests: deltas.requests,
        tokens: deltas.tokens,
        cost: deltas.cost,
      });
    }
  }, [metrics]);

  const data = useMemo(() => {
    const buckets = bucketsRef.current;
    const now = new Date();
    const result: HourlyData[] = [];
    for (let i = 23; i >= 0; i--) {
      const d = new Date(now.getTime() - i * 3600_000);
      const key = `${String(d.getHours()).padStart(2, '0')}:00`;
      result.push(buckets.get(key) || { hour: key, requests: 0, tokens: 0, cost: 0 });
    }
    return result;
  }, [metrics]);

  const hasData = data.some((d) => d.requests > 0 || d.tokens > 0 || d.cost > 0);

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <CardTitle className="text-base">Hourly Breakdown</CardTitle>
        <div className="flex gap-1">
          {(['requests', 'tokens', 'cost'] as MetricView[]).map((v) => (
            <button
              key={v}
              onClick={() => setView(v)}
              className={cn(
                'px-2 py-0.5 text-xs rounded transition-colors capitalize',
                view === v ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground hover:bg-muted/80',
              )}
            >
              {v}
            </button>
          ))}
        </div>
      </CardHeader>
      <CardContent>
        {!hasData ? (
          <div className="h-64 flex items-center justify-center text-muted-foreground text-sm">
            Collecting hourly data...
          </div>
        ) : (
          <ResponsiveContainer width="100%" height={280}>
            <BarChart data={data}>
              <CartesianGrid strokeDasharray="3 3" className="opacity-30" />
              <XAxis
                dataKey="hour"
                tick={{ fontSize: 10 }}
                interval={2}
              />
              <YAxis
                tick={{ fontSize: 10 }}
                tickFormatter={view === 'cost' ? (v) => `$${v.toFixed(2)}` : formatNumber}
              />
              <Tooltip
                content={({ active, payload }) => {
                  if (!active || !payload?.length) return null;
                  const d = (payload[0]?.payload ?? {}) as HourlyData;
                  return (
                    <div className="rounded-lg border bg-background p-2 shadow-sm text-xs">
                      <p className="font-medium mb-1">{d.hour}</p>
                      <p>Requests: {formatNumber(d.requests)}</p>
                      <p>Tokens: {formatNumber(d.tokens)}</p>
                      <p>Cost: {formatCost(d.cost)}</p>
                    </div>
                  );
                }}
              />
              <Bar dataKey={view} fill={VIEW_COLORS[view]} radius={[4, 4, 0, 0]} />
            </BarChart>
          </ResponsiveContainer>
        )}
      </CardContent>
    </Card>
  );
}
