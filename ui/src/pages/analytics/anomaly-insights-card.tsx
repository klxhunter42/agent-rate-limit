import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { InfoTip } from '@/components/shared/info-tip';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { AlertTriangle, XCircle, X } from 'lucide-react';
import { cn } from '@/lib/utils';
import type { Anomaly } from '@/hooks/use-anomaly-detection';

interface AnomalyInsightsCardProps {
  anomalies: Anomaly[];
  onDismiss: (id: string) => void;
}

const TYPE_LABELS: Record<Anomaly['type'], string> = {
  '429_spike': 'Rate Limit Spike',
  'error_burst': 'Error Burst',
  'queue_buildup': 'Queue Buildup',
  'rtt_spike': 'RTT Spike',
  'key_exhaustion': 'Key Exhaustion',
};

export function AnomalyInsightsCard({ anomalies, onDismiss }: AnomalyInsightsCardProps) {
  if (anomalies.length === 0) return null;

  const sorted = [...anomalies].reverse().slice(0, 10);
  const criticalCount = anomalies.filter((a) => a.severity === 'critical').length;
  const warningCount = anomalies.filter((a) => a.severity === 'warning').length;

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <div className="flex items-center gap-2">
          <CardTitle className="text-base flex items-center gap-1.5">Anomaly Insights<InfoTip text="Automated anomaly detection highlights unusual patterns in traffic, latency, or error rates." /></CardTitle>
          {criticalCount > 0 && (
            <Badge className="bg-red-500/10 text-red-500 text-[10px] px-1.5">
              {criticalCount} critical
            </Badge>
          )}
          {warningCount > 0 && (
            <Badge className="bg-yellow-500/10 text-yellow-500 text-[10px] px-1.5">
              {warningCount} warning
            </Badge>
          )}
        </div>
      </CardHeader>
      <CardContent>
        <div className="space-y-2">
          {sorted.map((anomaly) => (
            <div
              key={anomaly.id}
              className={cn(
                'flex items-start gap-3 rounded-lg border p-3 text-sm',
                anomaly.severity === 'critical' ? 'border-red-500/30 bg-red-500/5' : 'border-yellow-500/30 bg-yellow-500/5',
              )}
            >
              {anomaly.severity === 'critical' ? (
                <XCircle className="h-4 w-4 text-red-500 mt-0.5 shrink-0" />
              ) : (
                <AlertTriangle className="h-4 w-4 text-yellow-500 mt-0.5 shrink-0" />
              )}
              <div className="flex-1 min-w-0">
                <p className="font-medium text-xs">{TYPE_LABELS[anomaly.type]}</p>
                <p className="text-xs text-muted-foreground mt-0.5">{anomaly.message}</p>
                <p className="text-[10px] text-muted-foreground/60 mt-1">
                  {anomaly.detectedAt.toLocaleTimeString()}
                </p>
              </div>
              <Button
                variant="ghost"
                size="icon"
                className="h-5 w-5 shrink-0"
                onClick={() => onDismiss(anomaly.id)}
              >
                <X className="h-3 w-3" />
              </Button>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  );
}
