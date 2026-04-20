export interface ModelStatus {
  name: string;
  in_flight: number;
  limit: number;
  max_limit: number;
  learned_ceiling: number;
  total_requests: number;
  total_429s: number;
  min_rtt_ms: number;
  ewma_rtt_ms: number;
  series: number;
  overridden: boolean;
}

export interface GlobalStatus {
  global_in_flight: number;
  global_limit: number;
}

export interface KeyPoolStatus {
  total_keys: number;
  keys: KeyStatusEntry[];
}

export interface KeyStatusEntry {
  suffix: string;
  rpm: number;
  rpm_limit: number;
  rpm_used: number;
  cooldown_until: number;
  in_cooldown: boolean;
  success_count: number;
  error_count: number;
}

export interface HealthStatus {
  status: string;
  queue_depth: number;
  uptime_seconds: number;
}

export interface OverrideRequest {
  model: string;
  limit: number;
}

export interface LimiterStatus {
  global: GlobalStatus;
  models: ModelStatus[];
  keyPool: KeyPoolStatus;
  seenModels: string[];
  glmMode: boolean;
}

export async function fetchLimiterStatus(): Promise<LimiterStatus> {
  const res = await fetch('/v1/limiter-status');
  if (!res.ok) throw new Error(`limiter-status: ${res.status}`);
  return res.json();
}

export async function fetchModelStatus(): Promise<ModelStatus[]> {
  const data = await fetchLimiterStatus();
  return data.models ?? [];
}

export async function fetchHealth(): Promise<HealthStatus> {
  const res = await fetch('/health');
  if (!res.ok) throw new Error(`health: ${res.status}`);
  return res.json();
}

export async function fetchMetrics(): Promise<string> {
  const res = await fetch('/metrics');
  if (!res.ok) throw new Error(`metrics: ${res.status}`);
  return res.text();
}

export async function setOverride(req: OverrideRequest): Promise<void> {
  const res = await fetch('/v1/limiter-override', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  });
  if (!res.ok) throw new Error(`override: ${res.status}`);
}

export interface ParsedMetric {
  name: string;
  labels: Record<string, string>;
  value: number;
}

export function parsePrometheusText(text: string): ParsedMetric[] {
  const metrics: ParsedMetric[] = [];
  for (const line of text.split('\n')) {
    if (line.startsWith('#') || !line.trim()) continue;
    const match = line.match(/^(\w+)(?:\{([^}]*)\})?\s+([\d.e+-]+|NaN)/);
    if (!match) continue;
    const [, name, labelStr, valStr] = match;
    const labels: Record<string, string> = {};
    if (labelStr) {
      for (const pair of labelStr.split(',')) {
        const eqIdx = pair.indexOf('=');
        if (eqIdx > 0) {
          const k = pair.slice(0, eqIdx).trim();
          const v = pair.slice(eqIdx + 1).replace(/^"|"$/g, '');
          labels[k] = v;
        }
      }
    }
    const v = parseFloat(valStr!);
    if (!Number.isNaN(v)) {
      metrics.push({ name: name!, labels, value: v });
    }
  }
  return metrics;
}
