import { useDashboard } from '@/contexts/dashboard-context';
import { usePrometheusMetrics } from '@/hooks/use-prometheus-metrics';
import { useEventTimeline } from '@/hooks/use-event-timeline';
import { StatCard } from '@/components/shared/stat-card';
import { QuickCommands } from '@/components/shared/quick-commands';
import { KeyFlowMonitor } from '@/components/key-flow-monitor';
import { LiveAuthMonitor } from '@/components/monitoring/auth-monitor';
import { EventTimeline } from './event-timeline';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Progress } from '@/components/ui/progress';
import { Activity, Server, Wifi, Clock } from 'lucide-react';
import { formatUptime, formatNumber } from '@/lib/format';

export function OverviewPage() {
  const { models, health, loading, glmMode, seenModels } = useDashboard();
  const { metrics } = usePrometheusMetrics();
  const { events } = useEventTimeline(metrics, models, null);

  if (loading && models.length === 0) {
    return <div className="text-muted-foreground">Loading...</div>;
  }

  const totalInFlight = models.reduce((sum, m) => sum + m.in_flight, 0);
  const totalRequests = models.reduce((sum, m) => sum + m.total_requests, 0);
  const total429s = models.reduce((sum, m) => sum + m.total_429s, 0);
  const maxGlobal = Math.max(...models.map((m) => m.max_limit), 1);
  const concurrencyPct = (totalInFlight / maxGlobal) * 100;

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Overview</h1>

      {/* Stat cards */}
      <div className="grid gap-4 md:grid-cols-4">
        <StatCard
          title="Status"
          value={health?.status === 'healthy' ? 'Healthy' : 'Unhealthy'}
          subtitle={`uptime ${formatUptime(health?.uptime_seconds ?? 0)}`}
          icon={Wifi}
          variant={health?.status === 'healthy' ? 'success' : 'error'}
        />
        <StatCard
          title="Queue Depth"
          value={String(health?.queue_depth ?? 0)}
          subtitle="pending requests"
          icon={Server}
          variant={(health?.queue_depth ?? 0) > 0 ? 'warning' : 'default'}
        />
        <StatCard
          title="Total Requests"
          value={formatNumber(totalRequests)}
          subtitle={`${formatNumber(total429s)} rate-limited`}
          icon={Activity}
          variant="default"
        />
        <StatCard
          title="Concurrency"
          value={String(totalInFlight)}
          subtitle={`/ ${maxGlobal}`}
          icon={Clock}
          variant={concurrencyPct > 80 ? 'warning' : 'default'}
        />
      </div>

      {/* Global capacity + Model utilization */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Global Capacity</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            <div>
              <div className="flex justify-between text-sm mb-1">
                <span>Total In-Flight</span>
                <span>{totalInFlight} / {maxGlobal}</span>
              </div>
              <Progress value={concurrencyPct} />
            </div>
          </div>
        </CardContent>
      </Card>

      {models.length > 0 && (
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Model Utilization {!glmMode && <span className="text-xs text-muted-foreground ml-2">(live traffic)</span>}</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="space-y-3">
            {models.map((m) => {
              const pct = m.limit > 0 ? (m.in_flight / m.limit) * 100 : 0;
              return (
                <div key={m.name}>
                  <div className="flex justify-between text-sm mb-1">
                    <span className="font-mono">{m.name}</span>
                    <span className="text-muted-foreground">
                      {m.in_flight} / {m.limit}
                      {m.overridden && ' (pinned)'}
                    </span>
                  </div>
                  <Progress value={pct} />
                </div>
              );
            })}
            {!glmMode && seenModels.length > 0 && (
              <div className="pt-2 text-xs text-muted-foreground">
                Seen models: {seenModels.join(', ')}
              </div>
            )}
          </div>
        </CardContent>
      </Card>
      )}

      {/* Key Flow Monitor (OAuth-style Control Center) */}
      <KeyFlowMonitor />

      {/* Live Auth Monitor */}
      <LiveAuthMonitor />

      {/* Quick Commands + Event Timeline */}
      <div className="grid gap-4 lg:grid-cols-2">
        <QuickCommands />
        <EventTimeline events={events} />
      </div>
    </div>
  );
}
