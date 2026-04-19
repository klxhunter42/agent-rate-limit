import type { KeyStatusEntry } from '@/lib/api';

function getKeyStatus(k: KeyStatusEntry): { status: string; dotClass: string } {
  const now = Date.now() / 1000;
  if (k.in_cooldown || k.cooldown_until > now) return { status: 'cooldown', dotClass: 'bg-red-500' };
  const rpmUsed = k.rpm_used ?? k.rpm ?? 0;
  if (rpmUsed > 0 && k.rpm_limit > 0 && rpmUsed / k.rpm_limit > 0.8) return { status: 'warning', dotClass: 'bg-yellow-500' };
  return { status: 'active', dotClass: 'bg-green-500' };
}

export function KeyHealthIndicator({ entry }: { entry: KeyStatusEntry }) {
  const { status, dotClass } = getKeyStatus(entry);
  const labelClass = status === 'cooldown'
    ? 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400'
    : status === 'warning' || status === 'degraded'
    ? 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400'
    : 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400';

  return (
    <span className="inline-flex items-center gap-1.5 text-xs px-2 py-0.5 rounded">
      <span className={`h-2 w-2 rounded-full ${dotClass}`} />
      <span className={labelClass}>{status}</span>
    </span>
  );
}
