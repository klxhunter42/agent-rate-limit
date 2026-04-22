import { useState, useEffect } from 'react';

export interface UsageModel {
  model: string;
  input_tokens: number;
  output_tokens: number;
  cost: number;
  requests: number;
  errors: number;
}

export interface UsageSummary {
  total_requests: number;
  total_errors: number;
  total_tokens_in: number;
  total_tokens_out: number;
  total_cost: number;
  models: number;
  period: string;
}

export function summaryTotalTokens(s: UsageSummary | null): number {
  return s ? s.total_tokens_in + s.total_tokens_out : 0;
}

export function summaryErrorRate(s: UsageSummary | null): number {
  if (!s || s.total_requests === 0) return 0;
  return s.total_errors / s.total_requests;
}

export function useUsageModels(period: string, intervalMs = 5000) {
  const [models, setModels] = useState<UsageModel[]>([]);

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;

    async function load() {
      try {
        const r = await fetch(`/v1/usage/models?period=${period}`);
        if (!cancelled && r.ok) {
          const data = await r.json();
          setModels(data.models || []);
        }
      } catch { /* ignore */ }
      if (!cancelled) timer = setTimeout(load, intervalMs);
    }

    load();
    return () => { cancelled = true; clearTimeout(timer); };
  }, [period, intervalMs]);

  return models;
}

export function useUsageSummary(period: string, intervalMs = 5000) {
  const [summary, setSummary] = useState<UsageSummary | null>(null);

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout>;

    async function load() {
      try {
        const r = await fetch(`/v1/usage/summary?period=${period}`);
        if (!cancelled && r.ok) {
          setSummary(await r.json());
        }
      } catch { /* ignore */ }
      if (!cancelled) timer = setTimeout(load, intervalMs);
    }

    load();
    return () => { cancelled = true; clearTimeout(timer); };
  }, [period, intervalMs]);

  return summary;
}
