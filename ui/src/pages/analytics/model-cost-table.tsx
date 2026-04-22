import { extractModelTokens, extractModelCosts } from '@/lib/metrics-helpers';
import { formatNumber, formatCost, formatPercent } from '@/lib/format';
import type { ParsedMetric } from '@/lib/api';
import type { UsageModel } from '@/hooks/use-usage-api';

export function ModelCostTable({ metrics, period = '24h', usageModels = [] }: { metrics: ParsedMetric[]; period?: string; usageModels?: UsageModel[] }) {

  // Prefer usage API (Redis, persistent), fall back to Prometheus
  const hasUsageData = usageModels.length > 0 && usageModels.some((m) => m.input_tokens > 0 || m.output_tokens > 0);

  const tokens = hasUsageData
    ? usageModels.map((m) => ({ model: m.model, input: m.input_tokens, output: m.output_tokens }))
    : extractModelTokens(metrics);

  const costs = hasUsageData
    ? usageModels.map((m) => ({ model: m.model, cost: m.cost }))
    : extractModelCosts(metrics);

  const costMap = new Map(costs.map((c) => [c.model, c.cost]));
  const totalCost = costs.reduce((s, c) => s + c.cost, 0);
  const totalTokens = tokens.reduce((s, t) => s + t.input + t.output, 0);

  const sorted = [...tokens].sort((a, b) => {
    if (a.model === 'unknown') return 1;
    if (b.model === 'unknown') return -1;
    return (b.input + b.output) - (a.input + a.output);
  });

  if (tokens.length === 0 || (tokens.length === 1 && tokens[0]!.input === 0 && tokens[0]!.output === 0)) {
    return <div className="text-sm text-muted-foreground py-8 text-center">No model data available</div>;
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b text-left text-muted-foreground">
            <th className="pb-2 font-medium">Model</th>
            <th className="pb-2 font-medium text-right">Input Tokens</th>
            <th className="pb-2 font-medium text-right">Output Tokens</th>
            <th className="pb-2 font-medium text-right">Total Tokens</th>
            <th className="pb-2 font-medium text-right">Cost</th>
            <th className="pb-2 font-medium text-right">% of Total</th>
          </tr>
        </thead>
        <tbody>
          {sorted.map((t) => {
            const cost = costMap.get(t.model) || 0;
            const total = t.input + t.output;
            return (
              <tr key={t.model} className="border-b last:border-0">
                <td className="py-2 font-mono text-xs">{t.model}</td>
                <td className="py-2 text-right">{formatNumber(t.input)}</td>
                <td className="py-2 text-right">{formatNumber(t.output)}</td>
                <td className="py-2 text-right font-medium">{formatNumber(total)}</td>
                <td className="py-2 text-right">{formatCost(cost)}</td>
                <td className="py-2 text-right text-muted-foreground">{formatPercent(total, totalTokens)}</td>
              </tr>
            );
          })}
        </tbody>
        <tfoot>
          <tr className="border-t font-medium">
            <td className="pt-2">Total</td>
            <td className="pt-2 text-right">{formatNumber(tokens.reduce((s, t) => s + t.input, 0))}</td>
            <td className="pt-2 text-right">{formatNumber(tokens.reduce((s, t) => s + t.output, 0))}</td>
            <td className="pt-2 text-right">{formatNumber(totalTokens)}</td>
            <td className="pt-2 text-right">{formatCost(totalCost)}</td>
            <td className="pt-2 text-right">100%</td>
          </tr>
        </tfoot>
      </table>
    </div>
  );
}
