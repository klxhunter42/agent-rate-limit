import { useState, useEffect, useCallback, createContext, useContext, type ReactNode } from 'react';
import { fetchLimiterStatus, fetchHealth, type ModelStatus, type HealthStatus, type GlobalStatus, type KeyPoolStatus } from '@/lib/api';

interface DashboardData {
  models: ModelStatus[];
  global: GlobalStatus | null;
  keyPool: KeyPoolStatus | null;
  health: HealthStatus | null;
  loading: boolean;
  error: string | null;
  lastRefresh: Date | null;
  refresh: () => void;
}

const DashboardContext = createContext<DashboardData>({
  models: [],
  global: null,
  keyPool: null,
  health: null,
  loading: false,
  error: null,
  lastRefresh: null,
  refresh: () => {},
});

export function DashboardProvider({ children }: { children: ReactNode }) {
  const [models, setModels] = useState<ModelStatus[]>([]);
  const [global, setGlobal] = useState<GlobalStatus | null>(null);
  const [keyPool, setKeyPool] = useState<KeyPoolStatus | null>(null);
  const [health, setHealth] = useState<HealthStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [lastRefresh, setLastRefresh] = useState<Date | null>(null);

  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [limiterRes, healthRes] = await Promise.allSettled([fetchLimiterStatus(), fetchHealth()]);
      if (limiterRes.status === 'fulfilled') {
        setModels(limiterRes.value.models ?? []);
        setGlobal(limiterRes.value.global ?? null);
        setKeyPool(limiterRes.value.keyPool ?? null);
      } else {
        setError(limiterRes.reason?.message || 'Failed to fetch limiter status');
      }
      if (healthRes.status === 'fulfilled') setHealth(healthRes.value);
      setLastRefresh(new Date());
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
    const id = setInterval(refresh, 5000);
    return () => clearInterval(id);
  }, [refresh]);

  return (
    <DashboardContext.Provider value={{ models, global, keyPool, health, loading, error, lastRefresh, refresh }}>
      {children}
    </DashboardContext.Provider>
  );
}

export function useDashboard() {
  return useContext(DashboardContext);
}
