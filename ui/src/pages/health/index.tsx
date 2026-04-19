import { Card, CardContent } from '@/components/ui/card';
import { useDashboard } from '@/contexts/dashboard-context';
import { usePrometheusMetrics } from '@/hooks/use-prometheus-metrics';
import { deriveHealthChecks, computeHealthSummary } from '@/lib/health-checks';
import { formatUptime } from '@/lib/format';
import { HealthGauge } from './health-gauge';
import { HealthStatsBar } from './health-stats-bar';
import { HealthChecks } from './health-checks';

export function HealthPage() {
  const { health, models, keyPool } = useDashboard();
  const { metrics, loading } = usePrometheusMetrics();

  if (loading) {
    return <div className="text-muted-foreground">Loading...</div>;
  }

  const metricsMap: Record<string, { value: number } | { sum: number; count: number }> = {};
  for (const m of metrics) {
    if (m.name.endsWith('_total') || m.name === 'api_gateway_queue_depth' || m.name === 'api_gateway_active_connections') {
      metricsMap[m.name + (m.labels.model ? ':' + m.labels.model : '')] = { value: m.value };
    } else if (m.name.endsWith('_sum')) {
      const base = m.name.replace(/_sum$/, '');
      if (!metricsMap[base]) metricsMap[base] = { sum: 0, count: 0 };
      (metricsMap[base] as { sum: number; count: number }).sum += m.value;
    } else if (m.name.endsWith('_count')) {
      const base = m.name.replace(/_count$/, '');
      if (!metricsMap[base]) metricsMap[base] = { sum: 0, count: 0 };
      (metricsMap[base] as { sum: number; count: number }).count += m.value;
    }
  }

  const groups = deriveHealthChecks({ health, models, keyPool, metrics: metricsMap });
  const summary = computeHealthSummary(groups);

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Live Health</h1>

      <Card>
        <CardContent className="p-6 flex items-center gap-8">
          <HealthGauge percentage={summary.percentage} status={summary.status} size="lg" />
          <div className="space-y-1">
            <div className="text-lg font-semibold">System Health</div>
            <div className="text-sm text-muted-foreground">
              Uptime: {formatUptime(health?.uptime_seconds ?? 0)}
            </div>
            <div className="text-sm text-muted-foreground">
              {summary.total} checks: {summary.passed} passed, {summary.warnings} warnings, {summary.errors} errors
            </div>
          </div>
        </CardContent>
      </Card>

      <HealthStatsBar
        segments={[
          { label: 'Passed', value: summary.passed, color: 'passed' },
          { label: 'Warnings', value: summary.warnings, color: 'warning' },
          { label: 'Errors', value: summary.errors, color: 'error' },
          { label: 'Info', value: summary.info, color: 'info' },
        ]}
        total={summary.total}
      />

      <HealthChecks groups={groups} />
    </div>
  );
}
