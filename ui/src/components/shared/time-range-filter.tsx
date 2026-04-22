import { cn } from '@/lib/utils';
import { type TimeRange, ALL_RANGES } from '@/hooks/use-time-range';

const SHORT_RANGES: TimeRange[] = ['5m', '15m', '1H'];
const LONG_RANGES: TimeRange[] = ['1H', '6H', '24H', '7D', '30D'];

interface TimeRangeFilterProps {
  value: TimeRange;
  onChange: (range: TimeRange) => void;
  variant?: 'short' | 'long';
}

export function TimeRangeFilter({ value, onChange, variant = 'short' }: TimeRangeFilterProps) {
  const ranges = variant === 'short' ? SHORT_RANGES : LONG_RANGES;
  return (
    <div className="flex gap-1">
      {ranges.map((r) => (
        <button
          key={r}
          onClick={() => onChange(r)}
          className={cn(
            'px-2 py-0.5 text-xs rounded transition-colors',
            value === r ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground hover:bg-muted/80',
          )}
        >
          {r}
        </button>
      ))}
    </div>
  );
}
