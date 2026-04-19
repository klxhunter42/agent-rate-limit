import { useRef, useCallback, useState } from 'react';
import type { ParsedMetric } from '@/lib/api';

export interface Anomaly {
  id: string;
  type: '429_spike' | 'error_burst' | 'queue_buildup' | 'rtt_spike' | 'key_exhaustion';
  severity: 'warning' | 'critical';
  message: string;
  value: number;
  threshold: number;
  detectedAt: Date;
}

interface AnomalyState {
  anomalies: Anomaly[];
  hasWarning: boolean;
  hasCritical: boolean;
}

interface TrackerState {
  previous: Map<string, number>;
  queueHistory: number[];
  rttBaseline: number | null;
  lastAnomalyByType: Map<string, number>;
}

function extractSum(metrics: ParsedMetric[], name: string): number {
  let total = 0;
  for (const m of metrics) {
    if (m.name === name) total += m.value;
  }
  return total;
}

function extractSumByLabel(metrics: ParsedMetric[], name: string, label: string): Map<string, number> {
  const map = new Map<string, number>();
  for (const m of metrics) {
    if (m.name !== name) continue;
    const key = m.labels[label] || 'unknown';
    map.set(key, (map.get(key) || 0) + m.value);
  }
  return map;
}

function isDuplicate(tracker: Map<string, number>, type: string, cooldownMs: number): boolean {
  const last = tracker.get(type);
  if (last && Date.now() - last < cooldownMs) return true;
  tracker.set(type, Date.now());
  return false;
}

export function useAnomalyDetection() {
  const trackerRef = useRef<TrackerState>({
    previous: new Map(),
    queueHistory: [],
    rttBaseline: null,
    lastAnomalyByType: new Map(),
  });
  const [state, setState] = useState<AnomalyState>({ anomalies: [], hasWarning: false, hasCritical: false });
  const idRef = useRef(0);

  const analyze = useCallback((metrics: ParsedMetric[]) => {
    if (metrics.length === 0) return [];

    const tracker = trackerRef.current;
    const prev = tracker.previous;
    const anomalies: Anomaly[] = [];
    const now = new Date();

    // Build current snapshot keyed by metric name
    const current = new Map<string, number>();
    for (const m of metrics) {
      current.set(m.name, (current.get(m.name) || 0) + m.value);
    }

    // 1. 429 Spike
    const current429s = extractSum(metrics, 'api_gateway_429_total');
    const prev429s = prev.get('api_gateway_429_total') ?? 0;
    const delta429 = current429s - prev429s;
    if (delta429 > 5 && !isDuplicate(tracker.lastAnomalyByType, '429_spike', 30_000)) {
      anomalies.push({
        id: `anom-${++idRef.current}`,
        type: '429_spike',
        severity: delta429 > 20 ? 'critical' : 'warning',
        message: `${delta429} requests rate-limited this interval`,
        value: delta429,
        threshold: delta429 > 20 ? 20 : 5,
        detectedAt: now,
      });
    }

    // 2. Error Burst
    const currentErrors = extractSum(metrics, 'api_gateway_error_total');
    const prevErrors = prev.get('api_gateway_error_total') ?? 0;
    const deltaErrors = currentErrors - prevErrors;
    if (deltaErrors > 5 && !isDuplicate(tracker.lastAnomalyByType, 'error_burst', 30_000)) {
      anomalies.push({
        id: `anom-${++idRef.current}`,
        type: 'error_burst',
        severity: deltaErrors > 15 ? 'critical' : 'warning',
        message: `${deltaErrors} errors detected this interval`,
        value: deltaErrors,
        threshold: deltaErrors > 15 ? 15 : 5,
        detectedAt: now,
      });
    }

    // 3. Queue Buildup: rising for 3+ consecutive polls
    const queueDepth = current.get('api_gateway_queue_depth') ?? 0;
    const qHistory = [...tracker.queueHistory, queueDepth].slice(-4);
    tracker.queueHistory = qHistory;
    if (qHistory.length >= 4) {
      const rising = qHistory.every((v, i) => i === 0 || v >= (qHistory[i - 1] ?? 0));
      const lastVal = qHistory[3] ?? 0;
      if (rising && lastVal > 10 && !isDuplicate(tracker.lastAnomalyByType, 'queue_buildup', 30_000)) {
        anomalies.push({
          id: `anom-${++idRef.current}`,
          type: 'queue_buildup',
          severity: lastVal > 50 ? 'critical' : 'warning',
          message: `Queue depth rising consecutively: ${lastVal}`,
          value: lastVal,
          threshold: 10,
          detectedAt: now,
        });
      }
    }

    // 4. RTT Spike: ewma_rtt > 2x baseline
    const currentRtt = extractSumByLabel(metrics, 'api_gateway_request_latency_seconds', 'model');
    const maxRttMs = Math.max(0, ...currentRtt.values()) * 1000;
    if (maxRttMs > 0) {
      tracker.rttBaseline = tracker.rttBaseline === null ? maxRttMs : Math.min(tracker.rttBaseline, maxRttMs);
    }
    if (tracker.rttBaseline !== null && tracker.rttBaseline > 0 && maxRttMs > tracker.rttBaseline * 2) {
      if (!isDuplicate(tracker.lastAnomalyByType, 'rtt_spike', 30_000)) {
        anomalies.push({
          id: `anom-${++idRef.current}`,
          type: 'rtt_spike',
          severity: maxRttMs > tracker.rttBaseline * 4 ? 'critical' : 'warning',
          message: `RTT spike: ${maxRttMs.toFixed(0)}ms (baseline: ${tracker.rttBaseline.toFixed(0)}ms)`,
          value: maxRttMs,
          threshold: tracker.rttBaseline * 2,
          detectedAt: now,
        });
      }
    }

    // 5. Key Exhaustion: all keys in cooldown
    // This requires key pool data passed separately; detect via 429 surge + no active capacity
    if (delta429 > 20 && deltaErrors > 10 && !isDuplicate(tracker.lastAnomalyByType, 'key_exhaustion', 60_000)) {
      anomalies.push({
        id: `anom-${++idRef.current}`,
        type: 'key_exhaustion',
        severity: 'critical',
        message: 'Possible key exhaustion: high 429s and errors',
        value: delta429,
        threshold: 20,
        detectedAt: now,
      });
    }

    // Update previous snapshot
    for (const m of metrics) {
      prev.set(m.name, (prev.get(m.name) || 0) + m.value);
    }

    const merged = [...state.anomalies, ...anomalies].slice(-20);
    const newState = {
      anomalies: merged,
      hasWarning: merged.some((a) => a.severity === 'warning'),
      hasCritical: merged.some((a) => a.severity === 'critical'),
    };
    setState(newState);
    return anomalies;
  }, [state.anomalies]);

  const dismiss = useCallback((id: string) => {
    setState((prev) => {
      const filtered = prev.anomalies.filter((a) => a.id !== id);
      return {
        anomalies: filtered,
        hasWarning: filtered.some((a) => a.severity === 'warning'),
        hasCritical: filtered.some((a) => a.severity === 'critical'),
      };
    });
  }, []);

  return { ...state, analyze, dismiss };
}
