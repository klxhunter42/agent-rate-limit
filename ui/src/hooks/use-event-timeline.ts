import { useState, useEffect, useRef } from 'react';
import type { ParsedMetric } from '@/lib/api';
import type { ModelStatus } from '@/lib/api';
import type { KeyStatusEntry } from '@/lib/api';

export interface TimelineEvent {
  id: string;
  time: Date;
  type: 'rate_limit' | 'key_cooldown' | 'override_change' | 'queue_buildup' | 'error_burst' | 'info';
  message: string;
  severity: 'info' | 'warning' | 'error';
}

interface PrevState {
  total429s: number;
  queueDepth: number;
  errorTotals: Record<string, number>;
  overrides: Set<string>;
  cooldownKeys: Set<string>;
}

export function useEventTimeline(
  metrics: ParsedMetric[],
  models: ModelStatus[],
  keyPool: { keys?: KeyStatusEntry[] } | null,
) {
  const [events, setEvents] = useState<TimelineEvent[]>([]);
  const prevRef = useRef<PrevState>({
    total429s: 0,
    queueDepth: 0,
    errorTotals: {},
    overrides: new Set(),
    cooldownKeys: new Set(),
  });
  const idRef = useRef(0);

  useEffect(() => {
    const prev = prevRef.current;
    const newEvents: TimelineEvent[] = [];

    // 429 spike
    const current429s = models.reduce((s, m) => s + m.total_429s, 0);
    const delta429 = current429s - prev.total429s;
    if (delta429 > 0) {
      newEvents.push({
        id: `ev-${++idRef.current}`,
        time: new Date(),
        type: 'rate_limit',
        message: `${delta429} request${delta429 > 1 ? 's' : ''} rate-limited`,
        severity: 'warning',
      });
    }
    prev.total429s = current429s;

    // Queue buildup
    let currentQueueDepth = 0;
    for (const m of metrics) {
      if (m.name === 'api_gateway_queue_depth') currentQueueDepth = m.value;
    }
    if (currentQueueDepth > prev.queueDepth + 5 && currentQueueDepth > 10) {
      newEvents.push({
        id: `ev-${++idRef.current}`,
        time: new Date(),
        type: 'queue_buildup',
        message: `Queue depth rising: ${currentQueueDepth}`,
        severity: 'warning',
      });
    }
    prev.queueDepth = currentQueueDepth;

    // Error burst
    const currentErrors: Record<string, number> = {};
    for (const m of metrics) {
      if (m.name !== 'api_gateway_error_total') continue;
      const t = m.labels.type || 'unknown';
      currentErrors[t] = (currentErrors[t] || 0) + m.value;
    }
    let totalErrorDelta = 0;
    for (const [t, v] of Object.entries(currentErrors)) {
      const delta = v - (prev.errorTotals[t] || 0);
      totalErrorDelta += delta;
    }
    if (totalErrorDelta > 5) {
      newEvents.push({
        id: `ev-${++idRef.current}`,
        time: new Date(),
        type: 'error_burst',
        message: `${totalErrorDelta} error${totalErrorDelta > 1 ? 's' : ''} detected`,
        severity: 'error',
      });
    }
    prev.errorTotals = currentErrors;

    // Override changes
    const currentOverrides = new Set(models.filter((m) => m.overridden).map((m) => m.name));
    for (const name of currentOverrides) {
      if (!prev.overrides.has(name)) {
        newEvents.push({
          id: `ev-${++idRef.current}`,
          time: new Date(),
          type: 'override_change',
          message: `Model ${name} limit pinned`,
          severity: 'info',
        });
      }
    }
    for (const name of prev.overrides) {
      if (!currentOverrides.has(name)) {
        newEvents.push({
          id: `ev-${++idRef.current}`,
          time: new Date(),
          type: 'override_change',
          message: `Model ${name} limit unpinned`,
          severity: 'info',
        });
      }
    }
    prev.overrides = currentOverrides;

    // Key cooldown
    const keys = keyPool?.keys ?? [];
    const currentCooldownKeys = new Set(keys.filter((k) => k.in_cooldown || (k.cooldown_until && k.cooldown_until > Date.now() / 1000)).map((k) => k.suffix));
    for (const suffix of currentCooldownKeys) {
      if (!prev.cooldownKeys.has(suffix)) {
        newEvents.push({
          id: `ev-${++idRef.current}`,
          time: new Date(),
          type: 'key_cooldown',
          message: `Key ...${suffix} entering cooldown`,
          severity: 'error',
        });
      }
    }
    prev.cooldownKeys = currentCooldownKeys;

    if (newEvents.length > 0) {
      setEvents((prev) => [...prev, ...newEvents].slice(-50));
    }
  }, [metrics, models, keyPool]);

  return { events };
}
