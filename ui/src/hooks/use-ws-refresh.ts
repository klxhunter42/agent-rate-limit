import { useEffect, useCallback, useRef } from 'react';
import { wsOn, wsOff } from '@/lib/ws-events';
import type { WSEvent } from '@/hooks/use-websocket';

export function useWSRefresh(eventType: string, onRefresh: () => void): void {
  const refRef = useRef(onRefresh);
  refRef.current = onRefresh;

  const handler = useCallback((_event: WSEvent) => {
    refRef.current();
  }, []);

  useEffect(() => {
    wsOn(eventType, handler);
    return () => wsOff(eventType, handler);
  }, [eventType, handler]);
}

export function useWSRefreshAny(onRefresh: () => void): void {
  const refRef = useRef(onRefresh);
  refRef.current = onRefresh;

  const handler = useCallback((_event: WSEvent) => {
    refRef.current();
  }, []);

  useEffect(() => {
    wsOn('*', handler);
    return () => wsOff('*', handler);
  }, [handler]);
}
