import { useState, useEffect, useCallback, useRef } from 'react';
import { fetchMetrics, parsePrometheusText, type ParsedMetric } from '@/lib/api';
import { getPollingInterval } from '@/lib/polling';

export function usePrometheusMetrics() {
  const [metrics, setMetrics] = useState<ParsedMetric[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const firstLoad = useRef(true);

  const load = useCallback(async () => {
    if (firstLoad.current) setLoading(true);
    try {
      const text = await fetchMetrics();
      setMetrics(parsePrometheusText(text));
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to fetch metrics');
    } finally {
      if (firstLoad.current) {
        firstLoad.current = false;
        setLoading(false);
      }
    }
  }, []);

  const timerRef = useRef<ReturnType<typeof setInterval>>();

  useEffect(() => {
    load();
    const schedule = () => {
      timerRef.current = setInterval(() => {
        load();
        clearInterval(timerRef.current!);
        schedule();
      }, getPollingInterval());
    };
    schedule();
    return () => { if (timerRef.current) clearInterval(timerRef.current); };
  }, [load]);

  return { metrics, loading, error };
}
