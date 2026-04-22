import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, Legend, CartesianGrid } from 'recharts';
import type { ParsedMetric } from '@/lib/api';
import type { UsageModel } from '@/hooks/use-usage-api';
import { extractModelTokens, extractModelCosts } from '@/lib/metrics-helpers';
import { formatNumber, formatCost, formatPercent } from '@/lib/format';
import { usePrivacy } from '@/contexts/privacy-context';
import { PRIVACY_BLUR_CLASS } from '@/lib/privacy';
import { cn } from '@/lib/utils';

export function TokenBreakdownChart({ metrics, period: _period = '24h', usageModels = [] }: { metrics: ParsedMetric[]; period?: string; usageModels?: UsageModel[] }) {
  const { privacyMode } = usePrivacy();

  const hasUsageData = usageModels.length > 0 && usageModels.some((m) => m.input_tokens > 0 || m.output_tokens > 0);

  const tokens = hasUsageData
    ? usageModels.map((m) => ({ model: m.model, input: m.input_tokens, output: m.output_tokens }))
    : extractModelTokens(metrics);

  const costs = hasUsageData
    ? usageModels.map((m) => ({ model: m.model, cost: m.cost }))
    : extractModelCosts(metrics);

  if (tokens.length === 0 || (tokens.length === 1 && tokens[0]!.input === 0 && tokens[0]!.output === 0)) {
    return <div className="h-48 flex items-center justify-center text-muted-foreground text-sm">No token data</div>;
  }

  const totalCost = costs.reduce((s, c) => s + c.cost, 0);

  const chartData = tokens.map((d) => ({
    name: d.model,
    input: d.input,
    output: d.output,
  }));

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

  return (
    <div>
      <ResponsiveContainer width="100%" height={240}>
        <BarChart data={chartData} layout="vertical" margin={{ left: 60 }}>
          <CartesianGrid strokeDasharray="3 3" className="opacity-30" />
          <XAxis type="number" tick={{ fontSize: 10 }} tickFormatter={formatNumber} />
          <YAxis type="category" dataKey="name" tick={{ fontSize: 10 }} width={55} />
          <Tooltip formatter={(v: number) => formatNumber(v)} />
          <Legend />
          <Bar dataKey="input" fill="#3b82f6" name="Input" radius={[0, 2, 2, 0]} />
          <Bar dataKey="output" fill="#f97316" name="Output" radius={[0, 2, 2, 0]} />
        </BarChart>
      </ResponsiveContainer>
      <div className="grid grid-cols-2 gap-3 mt-3">
        <div className="flex items-center gap-2 text-xs rounded-md bg-muted/40 p-2">
          <div className="h-2 w-2 rounded-full bg-[#3b82f6] shrink-0" />
          <span>Input</span>
          <span className={cn('ml-auto font-mono', privacyMode && PRIVACY_BLUR_CLASS)}>{formatCost(inputCost)}</span>
          {totalCost > 0 && (
            <span className="text-muted-foreground">{formatPercent(inputCost, totalCost)}</span>
          )}
        </div>
        <div className="flex items-center gap-2 text-xs rounded-md bg-muted/40 p-2">
          <div className="h-2 w-2 rounded-full bg-[#f97316] shrink-0" />
          <span>Output</span>
          <span className={cn('ml-auto font-mono', privacyMode && PRIVACY_BLUR_CLASS)}>{formatCost(outputCost)}</span>
          {totalCost > 0 && (
            <span className="text-muted-foreground">{formatPercent(outputCost, totalCost)}</span>
          )}
        </div>
      </div>
    </div>
  );
}
