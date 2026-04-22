import { useState, useEffect } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { InfoTip } from '@/components/shared/info-tip';
import { usePrometheusMetrics } from '@/hooks/use-prometheus-metrics';
import { useDashboard } from '@/contexts/dashboard-context';
import { useAnomalyDetection } from '@/hooks/use-anomaly-detection';
import { AnalyticsSummaryCards } from './analytics-summary-cards';
import { ModelDistributionChart } from './model-distribution-chart';
import { TokenBreakdownChart } from './token-breakdown-chart';
import { UsageTrendChart } from './usage-trend-chart';
import { CostByModelCard } from './cost-by-model-card';
import { ModelDetailsPopover } from './model-details-popover';
import { HourlyBreakdown } from './hourly-breakdown';
import { ModelCostTable } from './model-cost-table';
import { ErrorRateChart } from './error-rate-chart';
import { LatencyChart } from './latency-chart';
import { AnomalyInsightsCard } from './anomaly-insights-card';
import { UsageApiSection } from './usage-api-section';
import { useTimeRange, RANGE_PERIOD } from '@/hooks/use-time-range';
import { TimeRangeFilter } from '@/components/shared/time-range-filter';
import { filterByModels } from '@/lib/metrics-helpers';

export function AnalyticsPage() {
  const { metrics, loading } = usePrometheusMetrics();
  const { models, glmMode, seenModels } = useDashboard();
  const [selectedModel, setSelectedModel] = useState<string | null>(null);
  const { range, setRange, points: rangePoints } = useTimeRange('1H');
  const anomaly = useAnomalyDetection();

  const seenSet = new Set(seenModels);
  const filteredMetrics = glmMode ? metrics : filterByModels(metrics, seenSet);

  useEffect(() => {
    anomaly.analyze(metrics);
  }, [metrics]);

  if (loading) {
    return <div className="text-muted-foreground">Loading...</div>;
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Usage Analytics</h1>
        <TimeRangeFilter value={range} onChange={setRange} variant="long" />
      </div>

      <AnalyticsSummaryCards metrics={filteredMetrics} models={models} />

      <UsageTrendChart metrics={filteredMetrics} />

      <div className="grid gap-4 lg:grid-cols-12">
        <div className="lg:col-span-4">
          <CostByModelCard metrics={filteredMetrics} onModelClick={setSelectedModel} />
        </div>
        <div className="lg:col-span-4 h-full">
          <Card className="h-full">
            <CardHeader><CardTitle className="text-base flex items-center gap-1.5">Model Distribution<InfoTip text="Percentage of requests handled by each AI model." /></CardTitle></CardHeader>
            <CardContent>
              <ModelDistributionChart metrics={filteredMetrics} />
            </CardContent>
          </Card>
        </div>
        <div className="lg:col-span-4 h-full">
          <Card className="h-full">
            <CardHeader><CardTitle className="text-base flex items-center gap-1.5">Token Breakdown<InfoTip text="Input vs output token ratio per model. Input = prompt tokens, Output = completion tokens." /></CardTitle></CardHeader>
            <CardContent>
              <TokenBreakdownChart metrics={filteredMetrics} />
            </CardContent>
          </Card>
        </div>
      </div>

      <HourlyBreakdown metrics={filteredMetrics} />

      <div className="grid gap-4 md:grid-cols-2">
        <ErrorRateChart metrics={filteredMetrics} maxPoints={rangePoints} />
        <LatencyChart metrics={filteredMetrics} maxPoints={rangePoints} />
      </div>

      <Card>
        <CardHeader><CardTitle className="text-base flex items-center gap-1.5">Model Cost Breakdown<InfoTip text="Estimated cost per model based on token usage and configured pricing." /></CardTitle></CardHeader>
        <CardContent>
          <ModelCostTable metrics={filteredMetrics} />
        </CardContent>
      </Card>

      <AnomalyInsightsCard anomalies={anomaly.anomalies} onDismiss={anomaly.dismiss} />

      <UsageApiSection period={RANGE_PERIOD[range]} />

      {selectedModel && (
        <ModelDetailsPopover
          model={selectedModel}
          metrics={filteredMetrics}
          onClose={() => setSelectedModel(null)}
        />
      )}
    </div>
  );
}
