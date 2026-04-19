import { fetchMetrics, parsePrometheusText, type ParsedMetric } from './api';

export interface SecretTypeCount {
  type: string;
  count: number;
}

export interface PIITypeCount {
  type: string;
  count: number;
}

export interface MaskDurationPhase {
  phase: string;
  p95: number;
}

export interface PrivacyMetrics {
  totalMaskedRequests: number;
  secretsDetected: SecretTypeCount[];
  piiDetected: PIITypeCount[];
  maskDuration: MaskDurationPhase[];
}

function extractMaskRequests(metrics: ParsedMetric[]): number {
  let total = 0;
  for (const m of metrics) {
    if (m.name === 'api_gateway_mask_requests_total') {
      total += m.value;
    }
  }
  return total;
}

function extractSecretsByType(metrics: ParsedMetric[]): SecretTypeCount[] {
  const map = new Map<string, number>();
  for (const m of metrics) {
    if (m.name === 'api_gateway_secrets_detected_total') {
      const t = m.labels.type ?? 'unknown';
      map.set(t, (map.get(t) ?? 0) + m.value);
    }
  }
  return Array.from(map.entries())
    .map(([type, count]) => ({ type, count }))
    .sort((a, b) => b.count - a.count);
}

function extractPIIByType(metrics: ParsedMetric[]): PIITypeCount[] {
  const map = new Map<string, number>();
  for (const m of metrics) {
    if (m.name === 'api_gateway_pii_detected_total') {
      const t = m.labels.type ?? 'unknown';
      map.set(t, (map.get(t) ?? 0) + m.value);
    }
  }
  return Array.from(map.entries())
    .map(([type, count]) => ({ type, count }))
    .sort((a, b) => b.count - a.count);
}

function extractMaskDuration(metrics: ParsedMetric[]): MaskDurationPhase[] {
  const phases = new Map<string, { sum: number; count: number; buckets: Map<string, number> }>();

  for (const m of metrics) {
    if (m.name === 'api_gateway_mask_duration_seconds_bucket') {
      const phase = m.labels.phase ?? 'unknown';
      if (!phases.has(phase)) phases.set(phase, { sum: 0, count: 0, buckets: new Map() });
      const le = m.labels.le;
      if (le !== undefined) phases.get(phase)!.buckets.set(le, m.value);
    }
    if (m.name === 'api_gateway_mask_duration_seconds_sum') {
      const phase = m.labels.phase ?? 'unknown';
      if (!phases.has(phase)) phases.set(phase, { sum: 0, count: 0, buckets: new Map() });
      phases.get(phase)!.sum = m.value;
    }
    if (m.name === 'api_gateway_mask_duration_seconds_count') {
      const phase = m.labels.phase ?? 'unknown';
      if (!phases.has(phase)) phases.set(phase, { sum: 0, count: 0, buckets: new Map() });
      phases.get(phase)!.count = m.value;
    }
  }

  const result: MaskDurationPhase[] = [];
  for (const [phase, data] of phases) {
    if (data.count === 0) {
      result.push({ phase, p95: 0 });
      continue;
    }
    const target = data.count * 0.95;
    const leVals = Array.from(data.buckets.entries())
      .map(([le, count]) => ({ le: parseFloat(le), count }))
      .filter(({ le }) => Number.isFinite(le))
      .sort((a, b) => a.le - b.le);

    let p95 = leVals.length > 0 ? leVals[leVals.length - 1]?.le ?? 0 : 0;
    for (const { le, count } of leVals) {
      if (count >= target) { p95 = le; break; }
    }
    result.push({ phase, p95 });
  }

  return result.sort((a, b) => a.phase.localeCompare(b.phase));
}

export function parsePrivacyMetrics(metrics: ParsedMetric[]): PrivacyMetrics {
  return {
    totalMaskedRequests: extractMaskRequests(metrics),
    secretsDetected: extractSecretsByType(metrics),
    piiDetected: extractPIIByType(metrics),
    maskDuration: extractMaskDuration(metrics),
  };
}

export async function fetchPrivacyMetrics(): Promise<PrivacyMetrics> {
  const text = await fetchMetrics();
  const metrics = parsePrometheusText(text);
  return parsePrivacyMetrics(metrics);
}
