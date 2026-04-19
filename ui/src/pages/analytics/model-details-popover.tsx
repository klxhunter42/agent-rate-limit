import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { extractModelTokens, extractModelCosts } from '@/lib/metrics-helpers';
import { formatNumber, formatCost } from '@/lib/format';
import type { ParsedMetric } from '@/lib/api';

export function ModelDetailsPopover({
  model,
  metrics,
  onClose,
}: {
  model: string;
  metrics: ParsedMetric[];
  onClose: () => void;
}) {
  const tokens = extractModelTokens(metrics).find((t) => t.model === model);
  const costs = extractModelCosts(metrics).find((c) => c.model === model);
  const totalTokens = tokens ? tokens.input + tokens.output : 0;
  const cost = costs?.cost ?? 0;
  const allTokens = extractModelTokens(metrics);
  const grandTotal = allTokens.reduce((s, t) => s + t.input + t.output, 0);
  const pct = grandTotal > 0 ? (totalTokens / grandTotal) * 100 : 0;
  const ioRatio = tokens && tokens.output > 0 ? tokens.input / tokens.output : 0;

  return (
    <div className="fixed inset-0 z-50" onClick={onClose}>
      <div className="absolute inset-0 bg-black/20" />
      <Card
        className="fixed z-50 w-80 shadow-lg"
        style={{ top: '50%', left: '50%', transform: 'translate(-50%, -50%)' }}
        onClick={(e) => e.stopPropagation()}
      >
        <CardHeader className="pb-3">
          <div className="flex items-center justify-between">
            <CardTitle className="text-base font-mono">{model}</CardTitle>
            <Badge variant={pct > 20 ? 'default' : 'secondary'}>{pct.toFixed(1)}%</Badge>
          </div>
          {ioRatio > 0 && (
            <Badge
              variant={ioRatio >= 200 ? 'destructive' : ioRatio >= 50 ? 'secondary' : 'outline'}
              className="w-fit"
            >
              I/O Ratio: {ioRatio.toFixed(1)}x
            </Badge>
          )}
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid grid-cols-2 gap-3 text-sm">
            <div>
              <p className="text-muted-foreground text-xs">Cost</p>
              <p className="font-medium font-mono">{formatCost(cost)}</p>
            </div>
            <div>
              <p className="text-muted-foreground text-xs">Tokens</p>
              <p className="font-medium font-mono">{formatNumber(totalTokens)}</p>
            </div>
          </div>
          {tokens && (
            <div className="space-y-2">
              <div>
                <div className="flex justify-between text-xs mb-1">
                  <span className="text-[#3b82f6]">Input</span>
                  <span>{formatNumber(tokens.input)}</span>
                </div>
                <div className="h-2 rounded-full bg-muted overflow-hidden">
                  <div className="h-full bg-[#3b82f6] rounded-full" style={{ width: `${totalTokens > 0 ? (tokens.input / totalTokens) * 100 : 0}%` }} />
                </div>
              </div>
              <div>
                <div className="flex justify-between text-xs mb-1">
                  <span className="text-[#f97316]">Output</span>
                  <span>{formatNumber(tokens.output)}</span>
                </div>
                <div className="h-2 rounded-full bg-muted overflow-hidden">
                  <div className="h-full bg-[#f97316] rounded-full" style={{ width: `${totalTokens > 0 ? (tokens.output / totalTokens) * 100 : 0}%` }} />
                </div>
              </div>
            </div>
          )}
          <button
            onClick={onClose}
            className="w-full text-xs text-muted-foreground hover:text-foreground transition-colors py-1"
          >
            Close
          </button>
        </CardContent>
      </Card>
    </div>
  );
}
