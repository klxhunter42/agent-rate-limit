import { useState, useEffect, useRef } from 'react';
import type { ParsedMetric } from '@/lib/api';

interface TimeSeriesPoint {
  time: string;
  [key: string]: number | string;
}

function aggregateByLabel(metrics: ParsedMetric[], names: string[], label: string) {
  const map = new Map<string, number>();
  for (const m of metrics) {
    if (!names.includes(m.name)) continue;
    const key = (m.labels as Record<string, string>)[label] || 'unknown';
    map.set(key, (map.get(key) || 0) + m.value);
  }
  return map;
}

function aggregateSum(metrics: ParsedMetric[], names: string[]) {
  let total = 0;
  for (const m of metrics) {
    if (names.includes(m.name)) total += m.value;
  }
  return total;
}

export function useMetricsHistory(metrics: ParsedMetric[], maxPoints = 120) {
  const [tokenHistory, setTokenHistory] = useState<TimeSeriesPoint[]>([]);
  const [costHistory, setCostHistory] = useState<TimeSeriesPoint[]>([]);
  const [rateHistory, setRateHistory] = useState<TimeSeriesPoint[]>([]);
  const [errorHistory, setErrorHistory] = useState<TimeSeriesPoint[]>([]);
  const [latencyHistory, setLatencyHistory] = useState<TimeSeriesPoint[]>([]);

  const prevRef = useRef({
    tokens: new Map<string, number>(),
    costs: new Map<string, number>(),
    rates: new Map<string, number>(),
    errors: new Map<string, number>(),
    latencySum: 0,
    latencyCount: 0,
  });

  useEffect(() => {
    if (metrics.length === 0) return;
    const now = new Date().toLocaleTimeString();
    const prev = prevRef.current;

    // Token deltas
    const currentTokens = aggregateByLabel(metrics, ['api_gateway_token_input_total', 'api_gateway_token_output_total'], 'model');
    const tokenPt: TimeSeriesPoint = { time: now };
    for (const [model, val] of currentTokens) {
      tokenPt[model] = val - (prev.tokens.get(model) || 0);
    }
    if (prev.tokens.size > 0) {
      setTokenHistory((h) => [...h.slice(-(maxPoints - 1)), tokenPt]);
    }
    prev.tokens = currentTokens;

    // Cost deltas
    const currentCosts = aggregateByLabel(metrics, ['api_gateway_cost_total'], 'model');
    const costPt: TimeSeriesPoint = { time: now };
    for (const [model, val] of currentCosts) {
      costPt[model] = val - (prev.costs.get(model) || 0);
    }
    if (prev.costs.size > 0) {
      setCostHistory((h) => [...h.slice(-(maxPoints - 1)), costPt]);
    }
    prev.costs = currentCosts;

    // Rate deltas (request count)
    const currentRates = aggregateByLabel(metrics, ['api_gateway_request_latency_seconds_count'], 'model');
    const ratePt: TimeSeriesPoint = { time: now };
    for (const [model, val] of currentRates) {
      ratePt[model] = val - (prev.rates.get(model) || 0);
    }
    if (prev.rates.size > 0) {
      setRateHistory((h) => [...h.slice(-(maxPoints - 1)), ratePt]);
    }
    prev.rates = currentRates;

    // Error deltas
    const currentErrors = aggregateByLabel(metrics, ['api_gateway_error_total'], 'type');
    const errorPt: TimeSeriesPoint = { time: now };
    for (const [type, val] of currentErrors) {
      errorPt[type] = val - (prev.errors.get(type) || 0);
    }
    if (prev.errors.size > 0) {
      setErrorHistory((h) => [...h.slice(-(maxPoints - 1)), errorPt]);
    }
    prev.errors = currentErrors;

    // Latency snapshot
    const sum = aggregateSum(metrics, ['api_gateway_request_latency_seconds_sum']);
    const count = aggregateSum(metrics, ['api_gateway_request_latency_seconds_count']);
    if (prev.latencyCount > 0 && count > prev.latencyCount) {
      const avgMs = ((sum - prev.latencySum) / (count - prev.latencyCount)) * 1000;
      setLatencyHistory((h) => [...h.slice(-(maxPoints - 1)), { time: now, avg: avgMs }]);
    }
    prev.latencySum = sum;
    prev.latencyCount = count;
  }, [metrics, maxPoints]);

  return { tokenHistory, costHistory, rateHistory, errorHistory, latencyHistory };
}
