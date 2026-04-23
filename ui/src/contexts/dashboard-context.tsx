import { useState, useEffect, useCallback, useRef, createContext, useContext, type ReactNode } from 'react';
import { fetchLimiterStatus, fetchHealth, type ModelStatus, type HealthStatus, type GlobalStatus, type KeyPoolStatus } from '@/lib/api';
import { getPollingInterval } from '@/lib/polling';

interface DashboardData {
  models: ModelStatus[];
  global: GlobalStatus | null;
  keyPool: KeyPoolStatus | null;
  health: HealthStatus | null;
  glmMode: boolean;
  seenModels: string[];
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
  glmMode: true,
  seenModels: [],
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
  const [glmMode, setGlmMode] = useState(true);
  const [seenModels, setSeenModels] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [lastRefresh, setLastRefresh] = useState<Date | null>(null);
  const firstLoad = useRef(true);

  const refresh = useCallback(async () => {
    if (firstLoad.current) setLoading(true);
    setError(null);
    try {
      const [limiterRes, healthRes] = await Promise.allSettled([fetchLimiterStatus(), fetchHealth()]);
      if (limiterRes.status === 'fulfilled') {
        const raw = limiterRes.value;
        const isGlm = raw.glmMode ?? true;
        const seen = raw.seenModels ?? [];
        const seenSet = new Set(seen);
        const filtered = isGlm
          ? (raw.models ?? [])
          : (raw.models ?? []).filter((m) => seenSet.has(m.name));
        setModels(filtered);
        setGlobal(raw.global ?? null);
        setKeyPool(isGlm ? (raw.keyPool ?? null) : null);
        setGlmMode(isGlm);
        setSeenModels(seen);
      } else {
        setError(limiterRes.reason?.message || 'Failed to fetch limiter status');
      }
      if (healthRes.status === 'fulfilled') setHealth(healthRes.value);
      setLastRefresh(new Date());
    } finally {
      if (firstLoad.current) {
        firstLoad.current = false;
        setLoading(false);
      }
    }
  }, []);

  const timerRef = useRef<ReturnType<typeof setInterval>>();

  useEffect(() => {
    refresh();
    const schedule = () => {
      timerRef.current = setInterval(() => {
        refresh();
        clearInterval(timerRef.current!);
        schedule();
      }, getPollingInterval());
    };
    schedule();
    return () => { if (timerRef.current) clearInterval(timerRef.current); };
  }, [refresh]);

  return (
    <DashboardContext.Provider value={{ models, global, keyPool, health, glmMode, seenModels, loading, error, lastRefresh, refresh }}>
      {children}
    </DashboardContext.Provider>
  );
}

export function useDashboard() {
  return useContext(DashboardContext);
}
