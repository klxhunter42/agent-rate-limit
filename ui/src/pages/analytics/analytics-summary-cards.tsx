import { StatCard } from '@/components/shared/stat-card';
import type { ParsedMetric } from '@/lib/api';
import type { ModelStatus } from '@/lib/api';
import { extractTotalTokens, extractTotalCost, extractLatency } from '@/lib/metrics-helpers';
import { formatNumber, formatCost } from '@/lib/format';
import { usePrivacy } from '@/contexts/privacy-context';
import { PRIVACY_BLUR_CLASS } from '@/lib/privacy';
import { cn } from '@/lib/utils';
import { FileText, DollarSign, ArrowDownRight, ArrowUpRight, Timer } from 'lucide-react';

export function AnalyticsSummaryCards({ metrics }: { metrics: ParsedMetric[]; models: ModelStatus[] }) {
  const tokens = extractTotalTokens(metrics);
  const cost = extractTotalCost(metrics);
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
  const { privacyMode } = usePrivacy();

  return (
    <div className="grid gap-4 grid-cols-2 md:grid-cols-5">
      <StatCard
        title="Total Tokens"
        value={formatNumber(tokens.total)}
        subtitle={`${formatNumber(tokens.input)} in / ${formatNumber(tokens.output)} out`}
        icon={FileText}
        variant="default"
        className={cn(privacyMode && PRIVACY_BLUR_CLASS)}
      />
      <StatCard
        title="Total Cost"
        value={formatCost(cost)}
        subtitle="estimated USD"
        icon={DollarSign}
        variant="success"
        className={cn(privacyMode && PRIVACY_BLUR_CLASS)}
      />
      <StatCard
        title="Input Cost"
        value={formatCost(inputCost)}
        subtitle={`${formatNumber(tokens.input)} tokens`}
        icon={ArrowDownRight}
        variant="accent"
        className={cn(privacyMode && PRIVACY_BLUR_CLASS)}
      />
      <StatCard
        title="Output Cost"
        value={formatCost(outputCost)}
        subtitle={`${formatNumber(tokens.output)} tokens`}
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
