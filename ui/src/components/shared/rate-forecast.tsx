import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';

interface KeyInfo {
  suffix: string;
  rpmUsed: number;
  rpmLimit: number;
  inCooldown: boolean;
}

interface ForecastResult {
  suffix: string;
  remaining: number;
  burnRate: number;
  secondsUntilExhausted: number | null;
  status: 'ok' | 'warning' | 'critical' | 'cooldown';
}

interface RateForecastProps {
  keys: KeyInfo[];
}

function computeForecast(key: KeyInfo): ForecastResult {
  if (key.inCooldown) {
    return { suffix: key.suffix, remaining: 0, burnRate: 0, secondsUntilExhausted: null, status: 'cooldown' };
  }

  const remaining = Math.max(0, key.rpmLimit - key.rpmUsed);
  if (remaining === 0) {
    return { suffix: key.suffix, remaining: 0, burnRate: 0, secondsUntilExhausted: 0, status: 'critical' };
  }

  // Linear extrapolation: assume current rpmUsed accumulates at the same rate
  // rpmUsed is the count over the current minute window
  const burnRate = key.rpmUsed / 60; // requests per second
  if (burnRate <= 0) {
    return { suffix: key.suffix, remaining, burnRate: 0, secondsUntilExhausted: null, status: 'ok' };
  }

  const seconds = Math.floor(remaining / burnRate);
  let status: ForecastResult['status'] = 'ok';
  if (seconds < 30) status = 'critical';
  else if (seconds < 60) status = 'warning';

  return { suffix: key.suffix, remaining, burnRate, secondsUntilExhausted: seconds, status };
}

function formatSeconds(s: number | null): string {
  if (s === null) return 'stable';
  if (s < 60) return `~${s}s`;
  if (s < 3600) return `~${Math.floor(s / 60)}m ${s % 60}s`;
  return `~${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`;
}

const statusStyles: Record<string, string> = {
  ok: 'text-emerald-500 border-emerald-500/30 bg-emerald-500/10',
  warning: 'text-amber-500 border-amber-500/30 bg-amber-500/10',
  critical: 'text-red-500 border-red-500/30 bg-red-500/10',
  cooldown: 'text-muted-foreground border-muted-foreground/30 bg-muted/50',
};

export function RateForecast({ keys }: RateForecastProps) {
  const forecasts = keys.map(computeForecast);
  const allCooldown = forecasts.every((f) => f.status === 'cooldown');
  const anyWarning = forecasts.some((f) => f.status === 'warning' || f.status === 'critical');

  if (allCooldown) {
    return (
      <div className="flex flex-wrap gap-2">
        <Badge className={cn('text-xs border', statusStyles.critical)}>
          All keys in cooldown
        </Badge>
      </div>
    );
  }

  // Only show badges for keys that need attention
  const alertForecasts = forecasts.filter(
    (f) => f.status === 'warning' || f.status === 'critical' || f.status === 'cooldown',
  );

  if (alertForecasts.length === 0 && !anyWarning) {
    return null;
  }

  return (
    <div className="flex flex-wrap gap-2">
      {alertForecasts.map((f) => (
        <Badge key={f.suffix} className={cn('text-xs border', statusStyles[f.status])}>
          ...{f.suffix}: {f.status === 'cooldown' ? 'cooldown' : formatSeconds(f.secondsUntilExhausted)}
        </Badge>
      ))}
    </div>
  );
}
