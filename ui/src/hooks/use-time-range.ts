import { useState, useCallback } from 'react';

export type TimeRange = '5m' | '15m' | '1H' | '6H' | '24H' | '7D' | '30D';

const ALL_RANGES: TimeRange[] = ['5m', '15m', '1H', '6H', '24H', '7D', '30D'];

// How many Prometheus poll points to keep per range (at ~5s interval)
export const RANGE_POINTS: Record<TimeRange, number> = {
  '5m': 60,
  '15m': 180,
  '1H': 720,
  '6H': 720,
  '24H': 720,
  '7D': 720,
  '30D': 720,
};

// Map to backend API period query param
export const RANGE_PERIOD: Record<TimeRange, string> = {
  '5m': '5m',
  '15m': '15m',
  '1H': '1h',
  '6H': '6h',
  '24H': '24h',
  '7D': '7d',
  '30D': '30d',
};

const STORAGE_KEY = 'arl-time-range';
const DEFAULT_RANGE: TimeRange = '5m';

export function useTimeRange(customDefault?: TimeRange) {
  const [range, setRangeState] = useState<TimeRange>(() => {
    const stored = localStorage.getItem(STORAGE_KEY) as TimeRange | null;
    return stored && ALL_RANGES.includes(stored) ? stored : (customDefault || DEFAULT_RANGE);
  });

  const setRange = useCallback((r: TimeRange) => {
    setRangeState(r);
    localStorage.setItem(STORAGE_KEY, r);
  }, []);

  return { range, setRange, points: RANGE_POINTS[range], period: RANGE_PERIOD[range] };
}

export { ALL_RANGES };
