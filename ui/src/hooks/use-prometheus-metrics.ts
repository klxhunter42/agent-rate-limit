import { useState, useEffect, useCallback } from 'react';
import { fetchMetrics, parsePrometheusText, type ParsedMetric } from '@/lib/api';

export function usePrometheusMetrics() {
  const [metrics, setMetrics] = useState<ParsedMetric[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const text = await fetchMetrics();
      setMetrics(parsePrometheusText(text));
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to fetch metrics');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
    const id = setInterval(load, 5000);
    return () => clearInterval(id);
  }, [load]);

  return { metrics, loading, error };
}
