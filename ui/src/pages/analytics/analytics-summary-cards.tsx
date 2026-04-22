import { StatCard } from '@/components/shared/stat-card';
import type { ParsedMetric } from '@/lib/api';
import type { ModelStatus } from '@/lib/api';
import type { UsageSummary } from '@/hooks/use-usage-api';
import { extractTotalTokens, extractTotalCost, extractLatency } from '@/lib/metrics-helpers';
import { formatNumber, formatCost } from '@/lib/format';
import { usePrivacy } from '@/contexts/privacy-context';
import { PRIVACY_BLUR_CLASS } from '@/lib/privacy';
import { cn } from '@/lib/utils';
import { FileText, DollarSign, ArrowDownRight, ArrowUpRight, Timer } from 'lucide-react';

export function AnalyticsSummaryCards({ metrics, models, period = '24h', usageSummary }: { metrics: ParsedMetric[]; models: ModelStatus[]; period?: string; usageSummary: UsageSummary | null }) {
  const { privacyMode } = usePrivacy();

  // Prefer usage API (Redis, persistent), fall back to Prometheus
  const promTokens = extractTotalTokens(metrics);
  const promCost = extractTotalCost(metrics);

  const totalTokens = usageSummary ? usageSummary.total_tokens_in + usageSummary.total_tokens_out : promTokens.total;
  const totalIn = usageSummary ? usageSummary.total_tokens_in : promTokens.input;
  const totalOut = usageSummary ? usageSummary.total_tokens_out : promTokens.output;
  const totalCost = usageSummary ? usageSummary.total_cost : promCost;

  const inputCost = (() => {
    let c = 0;
    for (const m of metrics) {
      if (m.name === 'api_gateway_cost_total' && m.labels.type === 'input') c += m.value;
    }
    return c;
  })();
  const outputCost = (() => {
    let c = 0;
    for (const m of metrics) {
      if (m.name === 'api_gateway_cost_total' && m.labels.type === 'output') c += m.value;
    }
    return c;
  })();
  const avgLatency = extractLatency(metrics) * 1000;

  return (
    <div className="grid gap-4 grid-cols-2 md:grid-cols-5">
      <StatCard
        title="Total Tokens"
        value={formatNumber(totalTokens)}
        subtitle={`${formatNumber(totalIn)} in / ${formatNumber(totalOut)} out`}
        icon={FileText}
        variant="default"
        className={cn(privacyMode && PRIVACY_BLUR_CLASS)}
      />
      <StatCard
        title="Total Cost"
        value={formatCost(totalCost)}
        subtitle="estimated USD"
        icon={DollarSign}
        variant="success"
        className={cn(privacyMode && PRIVACY_BLUR_CLASS)}
      />
      <StatCard
        title="Input Cost"
        value={formatCost(inputCost)}
        subtitle={`${formatNumber(totalIn)} tokens`}
        icon={ArrowDownRight}
        variant="accent"
        className={cn(privacyMode && PRIVACY_BLUR_CLASS)}
      />
      <StatCard
        title="Output Cost"
        value={formatCost(outputCost)}
        subtitle={`${formatNumber(totalOut)} tokens`}
        icon={ArrowUpRight}
        variant="warning"
        className={cn(privacyMode && PRIVACY_BLUR_CLASS)}
      />
      <StatCard
        title="Avg Latency"
        value={`${avgLatency.toFixed(0)}ms`}
        subtitle="from histogram"
        icon={Timer}
        variant="default"
      />
    </div>
  );
}
