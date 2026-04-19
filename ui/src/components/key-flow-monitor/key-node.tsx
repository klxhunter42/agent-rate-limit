import type { KeyStatusEntry } from '@/lib/api';
import { usePrivacy } from '@/contexts/privacy-context';
import { PRIVACY_BLUR_CLASS } from '@/lib/privacy';
import { cn } from '@/lib/utils';

const KEY_COLORS = [
  '#1e6091', '#2d8a6e', '#d4a012', '#c92a2d', '#c45a1a',
  '#6b9c4d', '#3d5a73', '#cc7614', '#3a7371', '#7c5fc4',
];

export function KeyNode({
  entry,
  index,
  isHovered,
  onMouseEnter,
  onMouseLeave,
}: {
  entry: KeyStatusEntry;
  index: number;
  isHovered: boolean;
  onMouseEnter: () => void;
  onMouseLeave: () => void;
}) {
  const { privacyMode } = usePrivacy();
  const total = entry.success_count + entry.error_count;
  const errorRate = total > 0 ? (entry.error_count / total) * 100 : 0;
  const rpmPct = entry.rpm_limit > 0 ? (entry.rpm / entry.rpm_limit) * 100 : 0;
  const color = KEY_COLORS[index % KEY_COLORS.length];
  const isCooling = entry.cooldown_until > Date.now() / 1000;

  return (
    <div
      onMouseEnter={onMouseEnter}
      onMouseLeave={onMouseLeave}
      className={cn(
        'w-48 p-3 rounded-xl border transition-all duration-300 cursor-default',
        'bg-card/50 dark:bg-zinc-900/60 backdrop-blur-sm',
        isHovered ? 'ring-1 scale-[1.02] shadow-lg' : 'hover:bg-card/80',
        isCooling && 'opacity-50'
      )}
      style={{ borderColor: isHovered ? color : undefined, '--tw-ring-color': color } as React.CSSProperties}
    >
      <div className="flex items-center gap-2 mb-2">
        <div className="w-2.5 h-2.5 rounded-full shrink-0" style={{ backgroundColor: color }} />
        <span className={cn('text-sm font-mono truncate', privacyMode && PRIVACY_BLUR_CLASS)}>
          ...{entry.suffix}
        </span>
        {isCooling && (
          <span className="text-[10px] text-amber-500 font-medium ml-auto">cooldown</span>
        )}
      </div>

      <div className="flex gap-3 text-xs text-muted-foreground mb-1.5">
        <span className="text-green-500">{entry.success_count}</span>
        <span className="text-red-500">{entry.error_count}</span>
        <span className={cn('ml-auto', errorRate > 10 ? 'text-red-500' : errorRate > 0 ? 'text-yellow-500' : '')}>
          {errorRate.toFixed(0)}%
        </span>
      </div>

      <div className="w-full bg-muted dark:bg-zinc-800/50 h-1 rounded-full overflow-hidden">
        <div
          className="h-full rounded-full transition-all duration-500"
          style={{ width: `${rpmPct}%`, backgroundColor: color }}
        />
      </div>
      <div className="text-[10px] text-muted-foreground mt-1">
        {entry.rpm}/{entry.rpm_limit} RPM
      </div>
    </div>
  );
}
