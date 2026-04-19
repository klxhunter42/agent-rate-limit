import { useState, useEffect, useCallback } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, CartesianGrid, LineChart, Line, Legend } from 'recharts';
import { Shield, Eye, Fingerprint, Timer } from 'lucide-react';
import { fetchPrivacyMetrics, type PrivacyMetrics } from '@/lib/privacy-api';

export default function PrivacyPage() {
  const [data, setData] = useState<PrivacyMetrics | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const result = await fetchPrivacyMetrics();
      setData(result);
      setError(null);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      console.error('[privacy] fetch failed:', msg);
      setError(msg);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
    const id = setInterval(load, 5000);
    return () => clearInterval(id);
  }, [load]);

  if (loading) return <div className="text-muted-foreground">Loading...</div>;
  if (!data) return <div className="text-red-500 text-sm">Error loading privacy metrics: {error ?? 'unknown'}</div>;

  const secretsLast24h = data.secretsDetected.reduce((s, d) => s + d.count, 0);
  const piiLast24h = data.piiDetected.reduce((s, d) => s + d.count, 0);
  const rawP95 = data.maskDuration.length > 0
    ? Math.max(...data.maskDuration.map((d) => d.p95)) * 1000
    : 0;
  const p95Display = Number.isFinite(rawP95) ? rawP95.toFixed(1) : 'N/A';

  const cards = [
    { title: 'Total Masked Requests', value: data.totalMaskedRequests.toLocaleString(), sub: 'through pipeline', icon: Shield, iconColor: 'text-blue-500' },
    { title: 'Secrets Detected', value: secretsLast24h.toLocaleString(), sub: 'by type', icon: Eye, iconColor: 'text-red-500' },
    { title: 'PII Detected', value: piiLast24h.toLocaleString(), sub: 'by type', icon: Fingerprint, iconColor: 'text-orange-500' },
    { title: 'Mask Duration p95', value: `${p95Display}ms`, sub: 'slowest phase', icon: Timer, iconColor: 'text-purple-500' },
  ];

  const secretsChartData = data.secretsDetected.map((d) => ({
    type: d.type,
    count: d.count,
  }));

  const piiChartData = data.piiDetected.map((d) => ({
    type: d.type,
    count: d.count,
  }));

  const durationChartData = data.maskDuration.map((d) => ({
    phase: d.phase.replace(/_/g, ' '),
    p95: Number.isFinite(d.p95) ? +(d.p95 * 1000).toFixed(2) : 0,
  }));

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Privacy</h1>
      {error && <div className="text-red-500 text-xs bg-red-50 dark:bg-red-950 p-2 rounded">Last refresh error: {error}</div>}

      <div className="grid gap-4 md:grid-cols-4">
        {cards.map((c) => (
          <Card key={c.title}>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">{c.title}</CardTitle>
              <c.icon className={`h-4 w-4 ${c.iconColor}`} />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{c.value}</div>
              <p className="text-xs text-muted-foreground">{c.sub}</p>
            </CardContent>
          </Card>
        ))}
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Secrets by Type</CardTitle>
          </CardHeader>
          <CardContent>
            {secretsChartData.length === 0 ? (
              <div className="h-48 flex items-center justify-center text-muted-foreground text-sm">No secret detections</div>
            ) : (
              <ResponsiveContainer width="100%" height={240}>
                <BarChart data={secretsChartData}>
                  <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
                  <XAxis dataKey="type" tick={{ fontSize: 11 }} />
                  <YAxis tick={{ fontSize: 11 }} />
                  <Tooltip />
                  <Bar dataKey="count" fill="#ef4444" radius={[4, 4, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">PII by Type</CardTitle>
          </CardHeader>
          <CardContent>
            {piiChartData.length === 0 ? (
              <div className="h-48 flex items-center justify-center text-muted-foreground text-sm">No PII detections</div>
            ) : (
              <ResponsiveContainer width="100%" height={240}>
                <BarChart data={piiChartData}>
                  <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
                  <XAxis dataKey="type" tick={{ fontSize: 11 }} />
                  <YAxis tick={{ fontSize: 11 }} />
                  <Tooltip />
                  <Bar dataKey="count" fill="#f97316" radius={[4, 4, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            )}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Mask Duration by Phase (p95)</CardTitle>
        </CardHeader>
        <CardContent>
          {durationChartData.length === 0 ? (
            <div className="h-48 flex items-center justify-center text-muted-foreground text-sm">No duration data</div>
          ) : (
            <ResponsiveContainer width="100%" height={240}>
              <LineChart data={durationChartData}>
                <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
                <XAxis dataKey="phase" tick={{ fontSize: 11 }} />
                <YAxis tick={{ fontSize: 11 }} unit="ms" />
                <Tooltip />
                <Legend />
                <Line type="monotone" dataKey="p95" stroke="#8b5cf6" strokeWidth={2} name="p95 (ms)" dot={{ r: 4 }} />
              </LineChart>
            </ResponsiveContainer>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
