const STORAGE_PREFIX = 'arl-';

function getSetting(key: string, fallback: string): string {
  return localStorage.getItem(`${STORAGE_PREFIX}${key}`) || fallback;
}

function parseInterval(value: string): number {
  const ms: Record<string, number> = { '5s': 5000, '10s': 10000, '30s': 30000, '60s': 60000 };
  return ms[value] ?? 10000;
}

let cached: number | null = null;

export function getPollingInterval(): number {
  return cached ?? parseInterval(getSetting('polling-interval', '10s'));
}

export function invalidatePollingCache(): void {
  cached = null;
}

if (typeof window !== 'undefined') {
  window.addEventListener('storage', (e) => {
    if (e.key === `${STORAGE_PREFIX}polling-interval`) cached = null;
  });
  const originalSetItem = localStorage.setItem.bind(localStorage);
  localStorage.setItem = (key: string, value: string) => {
    originalSetItem(key, value);
    if (key === `${STORAGE_PREFIX}polling-interval`) cached = null;
  };
}
