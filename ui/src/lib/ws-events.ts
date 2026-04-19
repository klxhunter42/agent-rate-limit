import { WSEvent } from '@/hooks/use-websocket';

type Listener = (event: WSEvent) => void;

const listeners = new Map<string, Set<Listener>>();

function getSet(type: string): Set<Listener> {
  let s = listeners.get(type);
  if (!s) {
    s = new Set();
    listeners.set(type, s);
  }
  return s;
}

export function wsOn(type: string, fn: Listener): () => void {
  getSet(type).add(fn);
  return () => getSet(type).delete(fn);
}

export function wsEmit(event: WSEvent): void {
  const specific = listeners.get(event.type);
  const wildcard = listeners.get('*');
  if (specific) {
    for (const fn of specific) {
      try { fn(event); } catch { /* ignore */ }
    }
  }
  if (wildcard) {
    for (const fn of wildcard) {
      try { fn(event); } catch { /* ignore */ }
    }
  }
}

export function wsOff(type: string, fn: Listener): void {
  getSet(type).delete(fn);
}
