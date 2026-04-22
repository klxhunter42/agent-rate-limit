import { PieChart, Pie, Cell, Tooltip, ResponsiveContainer, Legend } from 'recharts';
import { PieChart as PieChartIcon } from 'lucide-react';
import type { ParsedMetric } from '@/lib/api';
import type { UsageModel } from '@/hooks/use-usage-api';
import { extractModelTokens } from '@/lib/metrics-helpers';
import { formatNumber, formatPercent } from '@/lib/format';
import { CHART_COLORS } from '@/lib/providers';

export function ModelDistributionChart({ metrics, period: _period = '24h', usageModels = [] }: { metrics: ParsedMetric[]; period?: string; usageModels?: UsageModel[] }) {

  const hasUsageData = usageModels.length > 0 && usageModels.some((m) => m.input_tokens > 0 || m.output_tokens > 0);

  const data = hasUsageData
    ? usageModels.map((m) => ({ model: m.model, input: m.input_tokens, output: m.output_tokens }))
    : extractModelTokens(metrics);

  if (data.length === 0 || (data.length === 1 && data[0]!.input === 0 && data[0]!.output === 0)) return (
    <div className="h-48 flex flex-col items-center justify-center text-muted-foreground text-sm gap-2">
      <PieChartIcon className="h-8 w-8 opacity-30" />
      <span>No token data yet</span>
    </div>
  );

  const total = data.reduce((s, d) => s + d.input + d.output, 0);
  const chartData = data.map((d) => ({
    name: d.model,
    value: d.input + d.output,
  }));

  return (
    <ResponsiveContainer width="100%" height={280}>
      <PieChart>
        <Pie
          data={chartData}
          cx="50%"
          cy="50%"
          innerRadius={50}
          outerRadius={80}
          paddingAngle={3}
          dataKey="value"
          label={({ name, value }) => `${name} ${formatPercent(value, total)}`}
          labelLine={false}
        >
          {chartData.map((_, i) => (
            <Cell key={i} fill={CHART_COLORS[i % CHART_COLORS.length]} />
          ))}
        </Pie>
        <Tooltip formatter={(v: number) => formatNumber(v)} />
        <Legend />
      </PieChart>
    </ResponsiveContainer>
  );
}
