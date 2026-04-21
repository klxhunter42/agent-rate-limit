import { useState, useEffect } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { StatCard } from '@/components/shared/stat-card';
import { formatNumber, formatCost } from '@/lib/format';
import { Loader2, Activity, DollarSign, Hash, AlertTriangle } from 'lucide-react';

interface UsageSummary {
  total_requests: number;
  total_tokens: number;
  total_cost: number;
  error_rate: number;
}

interface DailyUsage {
  date: string;
  model: string;
  requests: number;
  input_tokens: number;
  output_tokens: number;
  cost: number;
  errors: number;
  period: string;
}

interface SessionUsage {
  session: string;
  model: string;
  requests: number;
  input_tokens: number;
  output_tokens: number;
  cost: number;
  errors: number;
  period: string;
}

export function UsageApiSection() {
  const [summary, setSummary] = useState<UsageSummary | null>(null);
  const [daily, setDaily] = useState<DailyUsage[]>([]);
  const [sessions, setSessions] = useState<SessionUsage[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    async function load() {
      try {
        const [summaryRes, dailyRes, sessionsRes] = await Promise.all([
          fetch('/v1/usage/summary?period=24h'),
          fetch('/v1/usage/daily'),
          fetch('/v1/usage/sessions'),
        ]);

        if (!summaryRes.ok || !dailyRes.ok || !sessionsRes.ok) {
          setError('Usage API not available');
          return;
        }

        const [summaryData, dailyData, sessionsData] = await Promise.all([
          summaryRes.json(),
          dailyRes.json(),
          sessionsRes.json(),
        ]);

        if (!cancelled) {
          setSummary(summaryData);
          const dailyArr = Array.isArray(dailyData) ? dailyData : [];
          const sessArr = Array.isArray(sessionsData) ? sessionsData : [];
          // Map period -> date/session for display.
          setDaily(dailyArr.map((d: any) => ({ ...d, date: d.date || d.period || '' })));
          setSessions(sessArr.map((s: any) => ({ ...s, session: s.session || s.period || '' })));
        }
      } catch {
        setError('Failed to load usage data');
      } finally {
        if (!cancelled) setLoading(false);
      }
    }

    load();
    return () => { cancelled = true; };
  }, []);

  if (loading) {
    return (
      <div className="flex items-center gap-2 py-8 justify-center text-muted-foreground text-sm">
        <Loader2 className="h-4 w-4 animate-spin" />
        Loading usage data...
      </div>
    );
  }

  if (error) {
    return (
      <div className="text-sm text-muted-foreground py-8 text-center">{error}</div>
    );
  }

  if (!summary) return null;

  const hasDaily = daily.length > 0;
  const hasSessions = sessions.length > 0;

  // Aggregate daily data by date (sum across models).
  const dailyAgg = daily.reduce<Record<string, DailyUsage>>((acc, d) => {
    const key = d.date || d.period || 'unknown';
    if (!acc[key]) acc[key] = { date: key, model: '', requests: 0, input_tokens: 0, output_tokens: 0, cost: 0, errors: 0, period: key };
    acc[key].requests += d.requests;
    acc[key].input_tokens += d.input_tokens;
    acc[key].output_tokens += d.output_tokens;
    acc[key].cost += d.cost;
    acc[key].errors += d.errors;
    return acc;
  }, {});
  const dailyRows = Object.values(dailyAgg).sort((a, b) => b.date.localeCompare(a.date));

  // Aggregate session data by session.
  const sessAgg = sessions.reduce<Record<string, SessionUsage>>((acc, s) => {
    const key = s.session || s.period || 'unknown';
    if (!acc[key]) acc[key] = { session: key, model: '', requests: 0, input_tokens: 0, output_tokens: 0, cost: 0, errors: 0, period: key };
    acc[key].requests += s.requests;
    acc[key].input_tokens += s.input_tokens;
    acc[key].output_tokens += s.output_tokens;
    acc[key].cost += s.cost;
    acc[key].errors += s.errors;
    return acc;
  }, {});
  const sessRows = Object.values(sessAgg).sort((a, b) => b.session.localeCompare(a.session));

  return (
    <div className="space-y-4">
      <div className="grid gap-4 grid-cols-2 md:grid-cols-4">
        <StatCard
          title="Total Requests"
          value={formatNumber(summary.total_requests)}
          icon={Activity}
          variant="accent"
        />
        <StatCard
          title="Total Tokens"
          value={formatNumber(summary.total_tokens)}
          icon={Hash}
          variant="default"
        />
        <StatCard
          title="Total Cost"
          value={formatCost(summary.total_cost)}
          icon={DollarSign}
          variant="success"
        />
        <StatCard
          title="Error Rate"
          value={`${((summary.error_rate ?? 0) * 100).toFixed(1)}%`}
          icon={AlertTriangle}
          variant={(summary.error_rate ?? 0) > 0.1 ? 'error' : 'warning'}
        />
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-base">Daily Breakdown</CardTitle>
          </CardHeader>
          <CardContent>
            {!hasDaily ? (
              <div className="text-sm text-muted-foreground py-6 text-center">No daily data available</div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b text-left text-muted-foreground">
                      <th className="pb-2 font-medium">Date</th>
                      <th className="pb-2 font-medium text-right">Requests</th>
                      <th className="pb-2 font-medium text-right">Input Tokens</th>
                      <th className="pb-2 font-medium text-right">Output Tokens</th>
                      <th className="pb-2 font-medium text-right">Cost</th>
                    </tr>
                  </thead>
                  <tbody>
                    {dailyRows.map((d) => (
                      <tr key={d.date} className="border-b last:border-0">
                        <td className="py-2 text-xs">{d.date}</td>
                        <td className="py-2 text-right">{formatNumber(d.requests)}</td>
                        <td className="py-2 text-right">{formatNumber(d.input_tokens)}</td>
                        <td className="py-2 text-right">{formatNumber(d.output_tokens)}</td>
                        <td className="py-2 text-right">{formatCost(d.cost)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-base">Sessions</CardTitle>
          </CardHeader>
          <CardContent>
            {!hasSessions ? (
              <div className="text-sm text-muted-foreground py-6 text-center">No session data available</div>
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b text-left text-muted-foreground">
                      <th className="pb-2 font-medium">Session</th>
                      <th className="pb-2 font-medium text-right">Requests</th>
                      <th className="pb-2 font-medium text-right">Input</th>
                      <th className="pb-2 font-medium text-right">Output</th>
                      <th className="pb-2 font-medium text-right">Cost</th>
                    </tr>
                  </thead>
                  <tbody>
                    {sessRows.map((s) => (
                      <tr key={s.session} className="border-b last:border-0">
                        <td className="py-2 font-mono text-xs max-w-[140px] truncate">{s.session}</td>
                        <td className="py-2 text-right">{formatNumber(s.requests)}</td>
                        <td className="py-2 text-right">{formatNumber(s.input_tokens)}</td>
                        <td className="py-2 text-right">{formatNumber(s.output_tokens)}</td>
                        <td className="py-2 text-right">{formatCost(s.cost)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
