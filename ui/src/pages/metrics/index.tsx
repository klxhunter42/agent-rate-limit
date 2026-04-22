import { useState, useEffect, useMemo } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { usePrometheusMetrics } from '@/hooks/use-prometheus-metrics';
import {
  LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer,
  BarChart, Bar, Legend,
} from 'recharts';
import { CHART_COLORS } from '@/lib/providers';
import { InfoTip } from '@/components/shared/info-tip';
import { useTimeRange, RANGE_POINTS } from '@/hooks/use-time-range';
import { TimeRangeFilter } from '@/components/shared/time-range-filter';

interface TimeSeriesPoint {
  time: string;
  [key: string]: number | string;
}

export function MetricsPage() {
  const { metrics, loading } = usePrometheusMetrics();
  const { range, setRange, points } = useTimeRange('5m');
  const [history, setHistory] = useState<Map<string, TimeSeriesPoint[]>>(new Map());

  useEffect(() => {
    if (metrics.length === 0) return;
    const now = new Date().toLocaleTimeString();

    setHistory((prev) => {
      const next = new Map(prev);

      const reqCount = metrics.filter((m) => m.name === 'api_gateway_request_latency_seconds_count');
      if (reqCount.length > 0) {
        const pts = next.get('request_rate') || [];
        const point: TimeSeriesPoint = { time: now };
        for (const r of reqCount) {
          const key = r.labels.path || 'unknown';
          point[key] = (point[key] as number || 0) + r.value;
        }
        next.set('request_rate', [...pts.slice(-points), point]);
      }

      const inputTokens = metrics.filter((m) => m.name === 'api_gateway_token_input_total');
      const outputTokens = metrics.filter((m) => m.name === 'api_gateway_token_output_total');
      if (inputTokens.length > 0 || outputTokens.length > 0) {
        const pts = next.get('tokens') || [];
        const point: TimeSeriesPoint = { time: now };
        for (const t of inputTokens) {
          point[`in_${t.labels.model || 'unknown'}`] = t.value;
        }
        for (const t of outputTokens) {
          point[`out_${t.labels.model || 'unknown'}`] = t.value;
        }
        next.set('tokens', [...pts.slice(-points), point]);
      }

      const errors = metrics.filter((m) => m.name === 'api_gateway_error_total');
      if (errors.length > 0) {
        const pts = next.get('errors') || [];
        const point: TimeSeriesPoint = { time: now };
        for (const e of errors) {
          point[e.labels.type || 'unknown'] = e.value;
        }
        next.set('errors', [...pts.slice(-points), point]);
      }

      const charsSaved = metrics.filter((m) => m.name === 'api_gateway_optimizer_chars_saved_total');
      if (charsSaved.length > 0) {
        const pts = next.get('optimizer_chars') || [];
        const point: TimeSeriesPoint = { time: now };
        for (const c of charsSaved) {
          point[c.labels.technique || 'unknown'] = c.value;
        }
        next.set('optimizer_chars', [...pts.slice(-points), point]);
      }

      const optRuns = metrics.filter((m) => m.name === 'api_gateway_optimizer_runs_total');
      if (optRuns.length > 0) {
        const pts = next.get('optimizer_runs') || [];
        const point: TimeSeriesPoint = { time: now };
        for (const r of optRuns) {
          point[r.labels.technique || 'unknown'] = r.value;
        }
        next.set('optimizer_runs', [...pts.slice(-points), point]);
      }

      return next;
    });
  }, [metrics]);

  const tokenData = history.get('tokens') || [];
  const errorData = history.get('errors') || [];
  const optimizerCharsData = history.get('optimizer_chars') || [];
  const optimizerRunsData = history.get('optimizer_runs') || [];

  const uniqueMetricNames = useMemo(
    () => Array.from(new Set(metrics.map((m) => m.name))),
    [metrics],
  );

  const totalRequests = useMemo(
    () => metrics
      .filter((m) => m.name === 'api_gateway_request_latency_seconds_count')
      .reduce((sum, m) => sum + m.value, 0),
    [metrics],
  );

  const totalInputTokens = useMemo(
    () => metrics.filter((m) => m.name === 'api_gateway_token_input_total').reduce((s, m) => s + m.value, 0),
    [metrics],
  );

  const totalOutputTokens = useMemo(
    () => metrics.filter((m) => m.name === 'api_gateway_token_output_total').reduce((s, m) => s + m.value, 0),
    [metrics],
  );

  const totalErrors = useMemo(
    () => metrics.filter((m) => m.name === 'api_gateway_error_total').reduce((s, m) => s + m.value, 0),
    [metrics],
  );

  const totalCharsSaved = useMemo(
    () => metrics.filter((m) => m.name === 'api_gateway_optimizer_chars_saved_total').reduce((s, m) => s + m.value, 0),
    [metrics],
  );

  const totalOptRuns = useMemo(
    () => metrics.filter((m) => m.name === 'api_gateway_optimizer_runs_total').reduce((s, m) => s + m.value, 0),
    [metrics],
  );

  const pathBreakdown = useMemo(() => {
    const map: Record<string, number> = {};
    for (const m of metrics.filter((m) => m.name === 'api_gateway_request_latency_seconds_count')) {
      const p = m.labels.path || 'unknown';
      map[p] = (map[p] || 0) + m.value;
    }
    return Object.entries(map).sort((a, b) => b[1] - a[1]).slice(0, 10);
  }, [metrics]);

  function fmtNum(n: number): string {
    if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M';
    if (n >= 1_000) return (n / 1_000).toFixed(1) + 'K';
    return n.toLocaleString();
  }

  function renderChart(data: TimeSeriesPoint[], fallbackMsg: string) {
    if (data.length === 0) {
      return <div className="h-48 flex items-center justify-center text-muted-foreground text-sm">{fallbackMsg}</div>;
    }
    return (
      <ResponsiveContainer width="100%" height={200}>
        <LineChart data={data}>
          <CartesianGrid strokeDasharray="3 3" className="opacity-30" />
          <XAxis dataKey="time" tick={{ fontSize: 10 }} />
          <YAxis tick={{ fontSize: 10 }} />
          <Tooltip />
          <Legend />
          {Object.keys(data[0]!).filter((k) => k !== 'time').map((key, i) => (
            <Line key={key} type="monotone" dataKey={key} stroke={CHART_COLORS[i % CHART_COLORS.length]} dot={false} />
          ))}
        </LineChart>
      </ResponsiveContainer>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Metrics</h1>
        <TimeRangeFilter value={range} onChange={setRange} variant="short" />
      </div>

      {loading && metrics.length === 0 ? (
        <div className="text-muted-foreground">Loading metrics...</div>
      ) : (
        <>
          {/* Summary cards */}
          <div className="grid gap-4 grid-cols-2 md:grid-cols-3 lg:grid-cols-6">
            <Card>
              <CardContent className="pt-4 pb-4">
                <div className="text-xs text-muted-foreground flex items-center gap-1">Requests<InfoTip text="Total HTTP requests processed by the gateway since startup, including all API paths." /></div>
                <div className="text-xl font-bold tabular-nums">{fmtNum(totalRequests)}</div>
              </CardContent>
            </Card>
            <Card>
              <CardContent className="pt-4 pb-4">
                <div className="text-xs text-muted-foreground flex items-center gap-1">Tokens In<InfoTip text="Total input tokens (prompt) sent to AI models. Billed at input token rate." /></div>
                <div className="text-xl font-bold tabular-nums">{fmtNum(totalInputTokens)}</div>
              </CardContent>
            </Card>
            <Card>
              <CardContent className="pt-4 pb-4">
                <div className="text-xs text-muted-foreground flex items-center gap-1">Tokens Out<InfoTip text="Total output tokens (completion) generated by AI models. Billed at output token rate." /></div>
                <div className="text-xl font-bold tabular-nums">{fmtNum(totalOutputTokens)}</div>
              </CardContent>
            </Card>
            <Card>
              <CardContent className="pt-4 pb-4">
                <div className="text-xs text-muted-foreground flex items-center gap-1">Errors<InfoTip text="Total errors encountered including upstream failures, timeouts, and rate limits." /></div>
                <div className="text-xl font-bold tabular-nums">{fmtNum(totalErrors)}</div>
              </CardContent>
            </Card>
            <Card>
              <CardContent className="pt-4 pb-4">
                <div className="text-xs text-muted-foreground flex items-center gap-1">Chars Saved<InfoTip text="Total characters removed by token optimization. Fewer characters = fewer tokens = lower cost." /></div>
                <div className="text-xl font-bold tabular-nums text-green-500">{fmtNum(totalCharsSaved)}</div>
              </CardContent>
            </Card>
            <Card>
              <CardContent className="pt-4 pb-4">
                <div className="text-xs text-muted-foreground flex items-center gap-1">Opt Runs<InfoTip text="Number of times token optimization was applied to request payloads." /></div>
                <div className="text-xl font-bold tabular-nums">{fmtNum(totalOptRuns)}</div>
              </CardContent>
            </Card>
          </div>

          <div className="grid gap-4 md:grid-cols-2">
            {/* Request Rate by path */}
            <Card>
              <CardHeader>
                <CardTitle className="text-base flex items-center gap-1.5">Request Rate (by path)<InfoTip text="Breakdown of request count by API path since gateway startup." /></CardTitle>
              </CardHeader>
              <CardContent>
                {pathBreakdown.length > 0 ? (
                  <div className="space-y-2">
                    {pathBreakdown.map(([path, count]) => (
                      <div key={path} className="flex items-center justify-between text-sm">
                        <span className="font-mono text-xs truncate mr-2">{path}</span>
                        <span className="font-mono tabular-nums shrink-0">{fmtNum(count)}</span>
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className="h-24 flex items-center justify-center text-muted-foreground text-sm">No request data</div>
                )}
              </CardContent>
            </Card>

            {/* Token Usage chart */}
            <Card>
              <CardHeader>
                <CardTitle className="text-base flex items-center gap-1.5">Token Usage (over time)<InfoTip text="Input and output token usage tracked over time per model. Input tokens are the prompt, output tokens are the AI response." /></CardTitle>
              </CardHeader>
              <CardContent>
                {tokenData.length > 0 ? (
                  <ResponsiveContainer width="100%" height={200}>
                    <BarChart data={tokenData.slice(-10)}>
                      <CartesianGrid strokeDasharray="3 3" className="opacity-30" />
                      <XAxis dataKey="time" tick={{ fontSize: 10 }} />
                      <YAxis tick={{ fontSize: 10 }} />
                      <Tooltip />
                      <Legend />
                      {Object.keys(tokenData[0]!).filter((k) => k !== 'time').map((key, i) => (
                        <Bar key={key} dataKey={key} fill={CHART_COLORS[i % CHART_COLORS.length]} />
                      ))}
                    </BarChart>
                  </ResponsiveContainer>
                ) : (
                  <div className="h-48 flex items-center justify-center text-muted-foreground text-sm">No token data yet</div>
                )}
              </CardContent>
            </Card>

            {/* Errors */}
            <Card>
              <CardHeader>
                <CardTitle className="text-base flex items-center gap-1.5">Errors (over time)<InfoTip text="Error count over time grouped by error type. Types include upstream_fail, timeout, and parse_error." /></CardTitle>
              </CardHeader>
              <CardContent>
                {renderChart(errorData, 'No errors')}
              </CardContent>
            </Card>

            {/* Optimizer Chars Saved */}
            <Card>
              <CardHeader>
                <CardTitle className="text-base flex items-center gap-1.5">Token Optimization - Chars Saved<InfoTip text="Characters removed by each optimization technique. whitespace = collapsed extra spaces/newlines. dedup = removed duplicate content blocks. dedup_sentences = removed repeated sentences." /></CardTitle>
              </CardHeader>
              <CardContent>
                {renderChart(optimizerCharsData, 'No optimization data yet - send a request to trigger')}
              </CardContent>
            </Card>

            {/* Optimizer Runs */}
            <Card>
              <CardHeader>
                <CardTitle className="text-base flex items-center gap-1.5">Token Optimization - Runs<InfoTip text="How many times each optimization technique was triggered. A single request may trigger multiple techniques." /></CardTitle>
              </CardHeader>
              <CardContent>
                {renderChart(optimizerRunsData, 'No optimization data yet - send a request to trigger')}
              </CardContent>
            </Card>

            {/* Raw metrics summary */}
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
                  {uniqueMetricNames.slice(0, 10).map((name) => (
                    <div key={name} className="flex justify-between text-muted-foreground">
                      <span className="font-mono text-xs">{name}</span>
                      <span>{metrics.filter((m) => m.name === name).length}</span>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
          </div>
        </>
      )}
    </div>
  );
}
