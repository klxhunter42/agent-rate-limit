import { useState, useEffect, useMemo } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { usePrometheusMetrics } from '@/hooks/use-prometheus-metrics';
import {
  LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer,
  BarChart, Bar, Legend,
} from 'recharts';
import { CHART_COLORS } from '@/lib/providers';

interface TimeSeriesPoint {
  time: string;
  [key: string]: number | string;
}

export function MetricsPage() {
  const { metrics, loading } = usePrometheusMetrics();
  const [history, setHistory] = useState<Map<string, TimeSeriesPoint[]>>(new Map());

  useEffect(() => {
    if (metrics.length === 0) return;
    const now = new Date().toLocaleTimeString();

    setHistory((prev) => {
      const next = new Map(prev);

      // Request rate by model from histogram _count
      const reqCount = metrics.filter((m) => m.name === 'api_gateway_request_latency_seconds_count');
      if (reqCount.length > 0) {
        const pts = next.get('request_rate') || [];
        const point: TimeSeriesPoint = { time: now };
        for (const r of reqCount) {
          const model = r.labels.model || 'unknown';
          point[model] = (point[model] as number || 0) + r.value;
        }
        next.set('request_rate', [...pts.slice(-29), point]);
      }

      // Token usage by model
      const inputTokens = metrics.filter((m) => m.name === 'api_gateway_token_input_total');
      const outputTokens = metrics.filter((m) => m.name === 'api_gateway_token_output_total');
      if (inputTokens.length > 0 || outputTokens.length > 0) {
        const pts = next.get('tokens') || [];
        const point: TimeSeriesPoint = { time: now };
        for (const t of inputTokens) {
          const key = `input_${t.labels.model || 'unknown'}`;
          point[key] = t.value;
        }
        for (const t of outputTokens) {
          const key = `output_${t.labels.model || 'unknown'}`;
          point[key] = t.value;
        }
        next.set('tokens', [...pts.slice(-29), point]);
      }

      // Errors by type
      const errors = metrics.filter((m) => m.name === 'api_gateway_error_total');
      if (errors.length > 0) {
        const pts = next.get('errors') || [];
        const point: TimeSeriesPoint = { time: now };
        for (const e of errors) {
          point[e.labels.type || 'unknown'] = e.value;
        }
        next.set('errors', [...pts.slice(-29), point]);
      }

      return next;
    });
  }, [metrics]);

  const rateData = history.get('request_rate') || [];
  const tokenData = history.get('tokens') || [];
  const errorData = history.get('errors') || [];

  const uniqueMetricNames = useMemo(
    () => Array.from(new Set(metrics.map((m) => m.name))),
    [metrics],
  );

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Metrics</h1>

      {loading && metrics.length === 0 ? (
        <div className="text-muted-foreground">Loading metrics...</div>
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {/* Request Rate */}
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Request Rate</CardTitle>
            </CardHeader>
            <CardContent>
              {rateData.length < 2 ? (
                <div className="h-48 flex items-center justify-center text-muted-foreground text-sm">
                  Collecting data...
                </div>
              ) : (
                <ResponsiveContainer width="100%" height={200}>
                  <LineChart data={rateData}>
                    <CartesianGrid strokeDasharray="3 3" className="opacity-30" />
                    <XAxis dataKey="time" tick={{ fontSize: 10 }} />
                    <YAxis tick={{ fontSize: 10 }} />
                    <Tooltip />
                    {Object.keys(rateData[0]!)
                      .filter((k) => k !== 'time')
                      .map((key, i) => (
                        <Line key={key} type="monotone" dataKey={key} stroke={CHART_COLORS[i % CHART_COLORS.length]} dot={false} />
                      ))}
                  </LineChart>
                </ResponsiveContainer>
              )}
            </CardContent>
          </Card>

          {/* Token Usage */}
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Token Usage</CardTitle>
            </CardHeader>
            <CardContent>
              {tokenData.length < 2 ? (
                <div className="h-48 flex items-center justify-center text-muted-foreground text-sm">
                  Collecting data...
                </div>
              ) : (
                <ResponsiveContainer width="100%" height={200}>
                  <BarChart data={tokenData.slice(-10)}>
                    <CartesianGrid strokeDasharray="3 3" className="opacity-30" />
                    <XAxis dataKey="time" tick={{ fontSize: 10 }} />
                    <YAxis tick={{ fontSize: 10 }} />
                    <Tooltip />
                    <Legend />
                    {Object.keys(tokenData[0]!)
                      .filter((k) => k !== 'time')
                      .map((key, i) => (
                        <Bar key={key} dataKey={key} fill={CHART_COLORS[i % CHART_COLORS.length]} />
                      ))}
                  </BarChart>
                </ResponsiveContainer>
              )}
            </CardContent>
          </Card>

          {/* Errors */}
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Errors</CardTitle>
            </CardHeader>
            <CardContent>
              {errorData.length < 2 ? (
                <div className="h-48 flex items-center justify-center text-muted-foreground text-sm">
                  Collecting data...
                </div>
              ) : (
                <ResponsiveContainer width="100%" height={200}>
                  <LineChart data={errorData}>
                    <CartesianGrid strokeDasharray="3 3" className="opacity-30" />
                    <XAxis dataKey="time" tick={{ fontSize: 10 }} />
                    <YAxis tick={{ fontSize: 10 }} />
                    <Tooltip />
                    {Object.keys(errorData[0]!)
                      .filter((k) => k !== 'time')
                      .map((key, i) => (
                        <Line key={key} type="monotone" dataKey={key} stroke={CHART_COLORS[i % CHART_COLORS.length]} />
                      ))}
                  </LineChart>
                </ResponsiveContainer>
              )}
            </CardContent>
          </Card>

          {/* Raw metrics count */}
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Raw Metrics Summary</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="space-y-2 text-sm">
                <div className="flex justify-between">
                  <span>Total metric series</span>
                  <span className="font-medium">{metrics.length}</span>
                </div>
                <div className="flex justify-between">
                  <span>Unique metric names</span>
                  <span className="font-medium">{uniqueMetricNames.length}</span>
                </div>
                {uniqueMetricNames.slice(0, 8).map((name) => (
                  <div key={name} className="flex justify-between text-muted-foreground">
                    <span className="font-mono text-xs">{name}</span>
                    <span>{metrics.filter((m) => m.name === name).length}</span>
                  </div>
                ))}
              </div>
            </CardContent>
          </Card>
        </div>
      )}
    </div>
  );
}
