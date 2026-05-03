import { useState, useEffect, useCallback } from 'react';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Input } from '@/components/ui/input';
import {
  seedMockData,
  startMockLoop,
  stopMockLoop,
  fetchMockStatus,
  fetchWasteFindings,
  fetchMetrics,
  parsePrometheusText,
} from '@/lib/api';
import type { WasteFinding, ParsedMetric } from '@/lib/api';
import { toast } from 'sonner';
import {
  Play,
  Square,
  Database,
  Trash2,
  RefreshCw,
  Search,
  AlertTriangle,
  CheckCircle2,
} from 'lucide-react';

const SEVERITY_COLORS: Record<string, string> = {
  low: 'bg-blue-500/10 text-blue-500 border-blue-500/20',
  medium: 'bg-yellow-500/10 text-yellow-500 border-yellow-500/20',
  high: 'bg-red-500/10 text-red-500 border-red-500/20',
};

export function DebugMetricsPage() {
  const [loopRunning, setLoopRunning] = useState(false);
  const [findings, setFindings] = useState<WasteFinding[]>([]);
  const [metricsText, setMetricsText] = useState('');
  const [metricsFilter, setMetricsFilter] = useState('');
  const [parsedMetrics, setParsedMetrics] = useState<ParsedMetric[]>([]);
  const [loading, setLoading] = useState({ seed: false, loop: false, waste: false, metrics: false });

  const pollLoopStatus = useCallback(async () => {
    try {
      const s = await fetchMockStatus();
      setLoopRunning(s.running);
    } catch { /* ignore */ }
  }, []);

  useEffect(() => { pollLoopStatus(); }, [pollLoopStatus]);

  const handleSeed = async () => {
    setLoading(p => ({ ...p, seed: true }));
    try {
      await seedMockData();
      toast.success('Mock data seeded');
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Seed failed');
    } finally {
      setLoading(p => ({ ...p, seed: false }));
    }
  };

  const handleLoopToggle = async () => {
    setLoading(p => ({ ...p, loop: true }));
    try {
      if (loopRunning) {
        await stopMockLoop();
        setLoopRunning(false);
        toast.success('Mock loop stopped');
      } else {
        await startMockLoop();
        setLoopRunning(true);
        toast.success('Mock loop started (5s interval)');
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Loop toggle failed');
    } finally {
      setLoading(p => ({ ...p, loop: false }));
    }
  };

  const loadWaste = async () => {
    setLoading(p => ({ ...p, waste: true }));
    try {
      const f = await fetchWasteFindings();
      setFindings(f);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to load waste findings');
    } finally {
      setLoading(p => ({ ...p, waste: false }));
    }
  };

  const loadMetrics = async () => {
    setLoading(p => ({ ...p, metrics: true }));
    try {
      const text = await fetchMetrics();
      setMetricsText(text);
      const filtered = metricsFilter
        ? parsePrometheusText(text).filter(m => m.name.includes(metricsFilter))
        : parsePrometheusText(text);
      setParsedMetrics(filtered);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to load metrics');
    } finally {
      setLoading(p => ({ ...p, metrics: false }));
    }
  };

  useEffect(() => {
    if (metricsText && metricsFilter !== undefined) {
      const filtered = metricsFilter
        ? parsePrometheusText(metricsText).filter(m => m.name.includes(metricsFilter))
        : parsePrometheusText(metricsText);
      setParsedMetrics(filtered);
    }
  }, [metricsFilter, metricsText]);

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Debug Metrics</h1>

      {/* Mock Data Controls */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base flex items-center gap-2">
            <Database className="h-4 w-4" /> Mock Data Controls
          </CardTitle>
          <CardDescription>
            Seed one batch of mock data or start/stop a continuous loop (5s interval) for Grafana timeseries panels
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex items-center gap-3 flex-wrap">
            <Button onClick={handleSeed} disabled={loading.seed} variant="outline">
              <Database className="h-4 w-4 mr-1" /> Seed Once
            </Button>
            <Button onClick={handleLoopToggle} disabled={loading.loop} variant={loopRunning ? 'destructive' : 'default'}>
              {loopRunning ? <Square className="h-4 w-4 mr-1" /> : <Play className="h-4 w-4 mr-1" />}
              {loopRunning ? 'Stop Loop' : 'Start Loop'}
            </Button>
            <Badge variant={loopRunning ? 'default' : 'secondary'} className="ml-2">
              {loopRunning ? 'Loop Running' : 'Loop Idle'}
            </Badge>
          </div>
        </CardContent>
      </Card>

      {/* Waste Findings */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base flex items-center gap-2">
            <AlertTriangle className="h-4 w-4" /> Waste Findings
          </CardTitle>
          <CardDescription>Detects token waste patterns from accumulated session data</CardDescription>
        </CardHeader>
        <CardContent>
          <Button onClick={loadWaste} disabled={loading.waste} variant="outline" size="sm" className="mb-4">
            <RefreshCw className={`h-3.5 w-3.5 mr-1 ${loading.waste ? 'animate-spin' : ''}`} /> Scan
          </Button>
          {findings.length === 0 ? (
            <div className="text-center py-6 text-muted-foreground text-sm">
              No findings yet. Click Scan to run detectors.
            </div>
          ) : (
            <div className="space-y-3">
              {findings.map((f, i) => (
                <div key={i} className={`p-3 rounded-lg border ${SEVERITY_COLORS[f.severity] ?? ''}`}>
                  <div className="flex items-center gap-2 mb-1">
                    <Badge variant="outline" className="text-xs">{f.detector}</Badge>
                    <Badge variant="outline" className="text-xs uppercase">{f.severity}</Badge>
                    <span className="text-xs text-muted-foreground ml-auto">
                      {f.tokens_wasted.toLocaleString()} tokens wasted
                    </span>
                  </div>
                  <p className="text-sm">{f.message}</p>
                  {f.suggestion && (
                    <p className="text-xs text-muted-foreground mt-1">{f.suggestion}</p>
                  )}
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Live Prometheus Metrics */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base flex items-center gap-2">
            <CheckCircle2 className="h-4 w-4" /> Live Prometheus Metrics
          </CardTitle>
          <CardDescription>Raw /metrics endpoint output, filterable by metric name</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex items-center gap-3 mb-4">
            <div className="relative flex-1 max-w-sm">
              <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
              <Input
                placeholder="Filter metric name..."
                value={metricsFilter}
                onChange={e => setMetricsFilter(e.target.value)}
                className="pl-8"
              />
            </div>
            <Button onClick={loadMetrics} disabled={loading.metrics} variant="outline" size="sm">
              <RefreshCw className={`h-3.5 w-3.5 mr-1 ${loading.metrics ? 'animate-spin' : ''}`} /> Load
            </Button>
          </div>

          {parsedMetrics.length > 0 && (
            <div className="mb-3 text-xs text-muted-foreground">
              Showing {parsedMetrics.length} metrics
            </div>
          )}

          <div className="rounded-lg border bg-muted/30 max-h-[500px] overflow-auto">
            <table className="w-full text-xs font-mono">
              <thead className="sticky top-0 bg-background border-b">
                <tr>
                  <th className="text-left p-2 font-medium">Metric</th>
                  <th className="text-left p-2 font-medium">Labels</th>
                  <th className="text-right p-2 font-medium">Value</th>
                </tr>
              </thead>
              <tbody>
                {parsedMetrics.map((m, i) => (
                  <tr key={i} className="border-b border-border/50 hover:bg-muted/50">
                    <td className="p-2 text-blue-500 whitespace-nowrap">{m.name}</td>
                    <td className="p-2 text-muted-foreground">
                      {Object.keys(m.labels).length > 0
                        ? Object.entries(m.labels).map(([k, v]) => `${k}="${v}"`).join(', ')
                        : '-'}
                    </td>
                    <td className="p-2 text-right">{m.value}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            {parsedMetrics.length === 0 && metricsText && (
              <div className="p-6 text-center text-muted-foreground text-sm">
                No metrics match filter
              </div>
            )}
            {parsedMetrics.length === 0 && !metricsText && (
              <div className="p-6 text-center text-muted-foreground text-sm">
                Click Load to fetch metrics
              </div>
            )}
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
