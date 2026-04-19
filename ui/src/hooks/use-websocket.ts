import { useState, useEffect, useRef, useCallback } from 'react';

export interface WSEvent {
  type: string;
  data: unknown;
  timestamp: string;
}

export interface UseWebSocketReturn {
  connected: boolean;
  lastEvent: WSEvent | null;
  events: WSEvent[];
}

const MAX_EVENTS = 50;
const BASE_DELAY = 1000;
const MAX_DELAY = 30000;

export function useWebSocket(onEvent?: (event: WSEvent) => void): UseWebSocketReturn {
  const [connected, setConnected] = useState(false);
  const [lastEvent, setLastEvent] = useState<WSEvent | null>(null);
  const [events, setEvents] = useState<WSEvent[]>([]);

  const wsRef = useRef<WebSocket | null>(null);
  const retryRef = useRef(0);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const mountedRef = useRef(true);
  const onEventRef = useRef(onEvent);
  onEventRef.current = onEvent;

  const clearTimer = useCallback(() => {
    if (timerRef.current) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  }, []);

  useEffect(() => {
    mountedRef.current = true;

    const buildURL = (): string => {
      const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      return `${proto}//${window.location.host}/ws`;
    };

    const connect = () => {
      if (!mountedRef.current) return;

      const ws = new WebSocket(buildURL());
      wsRef.current = ws;

      ws.onopen = () => {
        if (!mountedRef.current) return;
        setConnected(true);
        retryRef.current = 0;
      };

      ws.onmessage = (ev) => {
        if (!mountedRef.current) return;
        try {
          const parsed: WSEvent = JSON.parse(ev.data);
          setLastEvent(parsed);
          setEvents((prev) => [...prev.slice(-(MAX_EVENTS - 1)), parsed]);
          onEventRef.current?.(parsed);
        } catch {
          // ignore malformed messages
        }
      };

      ws.onclose = () => {
        if (!mountedRef.current) return;
        setConnected(false);
        wsRef.current = null;
        const delay = Math.min(BASE_DELAY * Math.pow(2, retryRef.current), MAX_DELAY);
        retryRef.current++;
        timerRef.current = setTimeout(connect, delay);
      };

      ws.onerror = () => {
        // onclose will fire after onerror, reconnect handled there
      };
    };

    connect();

    return () => {
      mountedRef.current = false;
      clearTimer();
      if (wsRef.current) {
        wsRef.current.onclose = null;
        wsRef.current.close();
        wsRef.current = null;
      }
    };
  }, [clearTimer]);

  return { connected, lastEvent, events };
}
