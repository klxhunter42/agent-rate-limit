import type { ParsedMetric } from '@/lib/api';

interface ModelTokens {
  model: string;
  input: number;
  output: number;
}

export function extractModelTokens(metrics: ParsedMetric[]): ModelTokens[] {
  const map = new Map<string, ModelTokens>();
  for (const m of metrics) {
    const model = m.labels.model || 'unknown';
    if (!map.has(model)) map.set(model, { model, input: 0, output: 0 });
    const entry = map.get(model)!;
    if (m.name === 'api_gateway_token_input_total') entry.input += m.value;
    if (m.name === 'api_gateway_token_output_total') entry.output += m.value;
  }
  return Array.from(map.values());
}

export function extractModelCosts(metrics: ParsedMetric[]): { model: string; cost: number }[] {
  const map = new Map<string, number>();
  for (const m of metrics) {
    if (m.name !== 'api_gateway_cost_total') continue;
    const model = m.labels.model || 'unknown';
    map.set(model, (map.get(model) || 0) + m.value);
  }
  return Array.from(map.entries()).map(([model, cost]) => ({ model, cost }));
}

export function extractTotalTokens(metrics: ParsedMetric[]): { input: number; output: number; total: number } {
  let input = 0;
  let output = 0;
  for (const m of metrics) {
    if (m.name === 'api_gateway_token_input_total') input += m.value;
    if (m.name === 'api_gateway_token_output_total') output += m.value;
  }
  return { input, output, total: input + output };
}

export function extractTotalCost(metrics: ParsedMetric[]): number {
  let cost = 0;
  for (const m of metrics) {
    if (m.name === 'api_gateway_cost_total') cost += m.value;
  }
  return cost;
}

export function extractErrorCounts(metrics: ParsedMetric[]): Record<string, number> {
  const counts: Record<string, number> = {};
  for (const m of metrics) {
    if (m.name !== 'api_gateway_error_total') continue;
    const t = m.labels.type || 'unknown';
    counts[t] = (counts[t] || 0) + m.value;
  }
  return counts;
}

export function extractLatency(metrics: ParsedMetric[]): number {
  let sum = 0;
  let count = 0;
  for (const m of metrics) {
    if (m.name === 'api_gateway_request_latency_seconds_sum') sum += m.value;
    if (m.name === 'api_gateway_request_latency_seconds_count') count += m.value;
  }
  return count > 0 ? sum / count : 0;
}

export function extractInfraMetrics(metrics: ParsedMetric[]): {
  queueDepth: number;
  activeConnections: number;
  upstream429: number;
  upstreamRetries: number;
} {
  let queueDepth = 0;
  let activeConnections = 0;
  let upstream429 = 0;
  let upstreamRetries = 0;
  for (const m of metrics) {
    if (m.name === 'api_gateway_queue_depth') queueDepth = m.value;
    if (m.name === 'api_gateway_active_connections') activeConnections = m.value;
    if (m.name === 'api_gateway_upstream_429_total') upstream429 = m.value;
    if (m.name === 'api_gateway_upstream_retries_total') upstreamRetries = m.value;
  }
  return { queueDepth, activeConnections, upstream429, upstreamRetries };
}
