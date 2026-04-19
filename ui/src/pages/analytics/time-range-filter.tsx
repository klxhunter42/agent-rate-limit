import { cn } from '@/lib/utils';

export type TimeRange = '1H' | '6H' | '24H' | '7D' | '30D';

const RANGES: TimeRange[] = ['1H', '6H', '24H', '7D', '30D'];

export const RANGE_POINTS: Record<TimeRange, number> = {
  '1H': 12,
  '6H': 72,
  '24H': 288,
  '7D': 2016,
  '30D': 8640,
};

interface TimeRangeFilterProps {
  value: TimeRange;
  onChange: (range: TimeRange) => void;
}

export function TimeRangeFilter({ value, onChange }: TimeRangeFilterProps) {
  return (
    <div className="flex gap-1">
      {RANGES.map((r) => (
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
