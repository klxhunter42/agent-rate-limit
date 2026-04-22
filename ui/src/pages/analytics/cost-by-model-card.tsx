import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { InfoTip } from '@/components/shared/info-tip';
import { extractModelTokens, extractModelCosts } from '@/lib/metrics-helpers';
import { formatNumber, formatCost, formatPercent } from '@/lib/format';
import { usePrivacy } from '@/contexts/privacy-context';
import { PRIVACY_BLUR_CLASS } from '@/lib/privacy';
import { cn } from '@/lib/utils';
import { CHART_COLORS, INPUT_TOKEN_COLOR, OUTPUT_TOKEN_COLOR } from '@/lib/providers';
import type { ParsedMetric } from '@/lib/api';
import type { UsageModel } from '@/hooks/use-usage-api';

export function CostByModelCard({ metrics, onModelClick, period = '24h', usageModels = [] }: { metrics: ParsedMetric[]; onModelClick?: (model: string) => void; period?: string; usageModels?: UsageModel[] }) {
  const { privacyMode } = usePrivacy();

  const hasUsageData = usageModels.length > 0 && usageModels.some((m) => m.input_tokens > 0 || m.output_tokens > 0);

  const tokens = hasUsageData
    ? usageModels.map((m) => ({ model: m.model, input: m.input_tokens, output: m.output_tokens }))
    : extractModelTokens(metrics);

  const costs = hasUsageData
    ? usageModels.map((m) => ({ model: m.model, cost: m.cost }))
    : extractModelCosts(metrics);

  const costMap = new Map(costs.map((c) => [c.model, c.cost]));
  const totalTokens = tokens.reduce((s, t) => s + t.input + t.output, 0);

  const rows = tokens
    .map((t) => {
      const cost = costMap.get(t.model) || 0;
      const total = t.input + t.output;
      return { model: t.model, input: t.input, output: t.output, total, cost };
    })
    .sort((a, b) => {
      const aUnk = a.model === 'unknown' ? 1 : 0;
      const bUnk = b.model === 'unknown' ? 1 : 0;
      if (aUnk !== bUnk) return aUnk - bUnk;
      return b.cost - a.cost;
    });

  if (rows.length === 0 || (rows.length === 1 && rows[0]!.total === 0)) {
    return (
      <Card>
        <CardHeader><CardTitle className="text-base flex items-center gap-1.5">Cost by Model<InfoTip text="Cumulative estimated cost per model based on token usage multiplied by per-model pricing." /></CardTitle></CardHeader>
        <CardContent>
          <div className="h-48 flex items-center justify-center text-muted-foreground text-sm">No cost data</div>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">Cost by Model</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="space-y-3">
          {rows.map((r, i) => {
            const inputPct = r.total > 0 ? (r.input / r.total) * 100 : 0;
            return (
              <div
                key={r.model}
                className={cn('flex items-center gap-3 rounded-lg p-2 transition-colors', onModelClick && 'cursor-pointer hover:bg-muted/50')}
                onClick={() => onModelClick?.(r.model)}
              >
                <div className="h-2.5 w-2.5 rounded-full shrink-0" style={{ backgroundColor: CHART_COLORS[i % CHART_COLORS.length] }} />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center justify-between mb-1">
                    <span className="text-sm font-mono truncate">{r.model}</span>
                    <span className={cn('text-sm font-medium ml-2', privacyMode && PRIVACY_BLUR_CLASS)}>
                      {formatCost(r.cost)}
                    </span>
                  </div>
                  <div className="flex h-1.5 rounded-full overflow-hidden bg-muted">
                    <div className="transition-all" style={{ width: `${inputPct}%`, backgroundColor: INPUT_TOKEN_COLOR }} />
                    <div className="transition-all" style={{ width: `${100 - inputPct}%`, backgroundColor: OUTPUT_TOKEN_COLOR }} />
                  </div>
                  <p className="text-xs text-muted-foreground mt-1">
                    {formatNumber(r.total)} tokens ({formatPercent(r.total, totalTokens)})
                  </p>
                </div>
              </div>
            );
          })}
        </div>
        <div className="flex gap-4 mt-4 pt-3 border-t text-xs text-muted-foreground">
          <div className="flex items-center gap-1.5"><div className="h-2 w-2 rounded-full" style={{ backgroundColor: INPUT_TOKEN_COLOR }} /> Input</div>
          <div className="flex items-center gap-1.5"><div className="h-2 w-2 rounded-full" style={{ backgroundColor: OUTPUT_TOKEN_COLOR }} /> Output</div>
        </div>
      </CardContent>
    </Card>
  );
}
