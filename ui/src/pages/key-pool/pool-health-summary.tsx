import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Progress } from '@/components/ui/progress';
import type { KeyPoolStatus } from '@/lib/api';

export function PoolHealthSummary({ keyPool }: { keyPool: KeyPoolStatus | null }) {
  if (!keyPool || keyPool.total_keys === 0) return null;

  const keys = keyPool.keys;
  const now = Date.now() / 1000;
  const activeCount = keys.filter((k) => !k.in_cooldown && k.cooldown_until <= now).length;
  const cooldownCount = keys.filter((k) => k.in_cooldown || k.cooldown_until > now).length;
  const totalRpm = keys.reduce((s, k) => s + (k.rpm_used ?? k.rpm ?? 0), 0);
  const totalRpmLimit = keys.reduce((s, k) => s + (k.rpm_limit ?? 0), 0);
  const avgRpm = keys.length > 0 ? Math.round(totalRpm / keys.length) : 0;
  const utilization = totalRpmLimit > 0 ? Math.round((totalRpm / totalRpmLimit) * 100) : 0;

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base">Pool Overview</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-3 gap-4 mb-4">
          <div>
            <div className="text-2xl font-bold text-green-500">{activeCount}</div>
            <p className="text-xs text-muted-foreground">Active Keys</p>
          </div>
          <div>
            <div className="text-2xl font-bold text-red-500">{cooldownCount}</div>
            <p className="text-xs text-muted-foreground">Cooldown</p>
          </div>
          <div>
            <div className="text-2xl font-bold">{avgRpm}</div>
            <p className="text-xs text-muted-foreground">Avg RPM/Key</p>
          </div>
        </div>
        <div className="space-y-1">
          <div className="flex justify-between text-xs text-muted-foreground">
            <span>RPM Utilization</span>
            <span>{totalRpm} / {totalRpmLimit}</span>
          </div>
          <Progress value={utilization} className="h-2" />
        </div>
      </CardContent>
    </Card>
  );
}
