import type { AccountInfo } from '@/lib/auth-api';
import { providerColor, providerName } from '@/lib/providers';
import { cn } from '@/lib/utils';

export interface ProviderStats {
  provider: string;
  displayName: string;
  accounts: AccountInfo[];
  activeCount: number;
  pausedCount: number;
  successCount: number;
  failureCount: number;
}

function getSuccessRate(success: number, failure: number): number {
  const total = success + failure;
  if (total === 0) return 100;
  return Math.round((success / total) * 100);
}

function successRateColor(rate: number): string {
  if (rate >= 95) return '#10b981';
  if (rate >= 80) return '#f59e0b';
  return '#ef4444';
}

interface ProviderCardProps {
  stats: ProviderStats;
}

export function ProviderCard({ stats }: ProviderCardProps) {
  const accent = providerColor(stats.provider.toLowerCase());
  const label = providerName(stats.provider.toLowerCase());
  const rate = getSuccessRate(stats.successCount, stats.failureCount);
  const rateColor = successRateColor(rate);

  return (
    <div
      className={cn(
        'rounded-xl p-4 transition-all duration-200',
        'bg-muted/30 dark:bg-zinc-900/60 backdrop-blur-sm',
        'border border-border/50 dark:border-white/[0.08]',
      )}
    >
      {/* Header: provider name + count */}
      <div className="flex items-center gap-3 mb-3">
        <div
          className="w-8 h-8 rounded-lg flex items-center justify-center text-xs font-bold text-white shrink-0"
          style={{ backgroundColor: accent }}
        >
          {label.slice(0, 2).toUpperCase()}
        </div>
        <div className="min-w-0">
          <h3 className="text-sm font-semibold text-foreground tracking-tight truncate">
            {label}
          </h3>
          <p className="text-[10px] text-muted-foreground">
            {stats.activeCount}/{stats.accounts.length} active
          </p>
        </div>
      </div>

      {/* Account dots */}
      <div className="flex flex-wrap gap-1.5 mb-3">
        {stats.accounts.map((acc) => (
          <div
            key={acc.id}
            className={cn(
              'w-3 h-3 rounded-full transition-all',
              acc.paused && 'opacity-40 grayscale',
            )}
            style={{
              backgroundColor: acc.isDefault ? '#f59e0b' : accent,
              boxShadow: acc.isDefault ? `0 0 6px ${accent}80` : undefined,
            }}
            title={
              acc.paused
                ? `${acc.email ?? acc.id} (paused)`
                : acc.email ?? acc.id
            }
          />
        ))}
      </div>

      {/* Success rate row */}
      <div className="flex items-center justify-between text-xs">
        <span className="text-muted-foreground">Success rate</span>
        <span className="font-mono font-semibold" style={{ color: rateColor }}>
          {rate}%
        </span>
      </div>

      {/* Progress bar */}
      <div className="w-full bg-muted dark:bg-zinc-800/50 h-1 rounded-full overflow-hidden mt-1.5">
        <div
          className="h-full rounded-full transition-all duration-500"
          style={{ width: `${rate}%`, backgroundColor: rateColor }}
        />
      </div>
    </div>
  );
}

export { getSuccessRate, successRateColor };
