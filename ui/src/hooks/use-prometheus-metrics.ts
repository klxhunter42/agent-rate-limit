import { useState, useEffect, useCallback, useRef } from 'react';
import { fetchMetrics, parsePrometheusText, type ParsedMetric } from '@/lib/api';
import { getPollingInterval } from '@/lib/polling';

export function usePrometheusMetrics() {
  const [metrics, setMetrics] = useState<ParsedMetric[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const firstLoad = useRef(true);
  const abortRef = useRef<AbortController | null>(null);

  const load = useCallback(async (signal?: AbortSignal) => {
    if (firstLoad.current) setLoading(true);
    try {
      const text = await fetchMetrics(signal);
      setMetrics(parsePrometheusText(text));
      setError(null);
    } catch (e) {
      if (signal?.aborted) return;
      setError(e instanceof Error ? e.message : 'failed to fetch metrics');
    } finally {
      if (firstLoad.current) {
        firstLoad.current = false;
        setLoading(false);
      }
    }
  }, []);

  const timerRef = useRef<ReturnType<typeof setInterval>>(null);

  useEffect(() => {
    abortRef.current?.abort();
    const ac = new AbortController();
    abortRef.current = ac;
    load(ac.signal);
    const schedule = () => {
      timerRef.current = setInterval(() => {
        load(ac.signal);
        clearInterval(timerRef.current!);
        schedule();
      }, getPollingInterval());
    };
    schedule();
    return () => {
      ac.abort();
      if (timerRef.current) clearInterval(timerRef.current);
    };
  }, [load]);

  return { metrics, loading, error };
}
