import { useDashboard } from '@/contexts/dashboard-context';
import { StatCard } from '@/components/shared/stat-card';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Progress } from '@/components/ui/progress';
import { Key, Activity, Server } from 'lucide-react';
import { formatPercent } from '@/lib/format';
import { usePrivacy } from '@/contexts/privacy-context';
import { PRIVACY_BLUR_CLASS } from '@/lib/privacy';
import { cn } from '@/lib/utils';
import { PoolHealthSummary } from './pool-health-summary';
import { KeyHealthIndicator } from './key-health-indicator';

export function KeyPoolPage() {
  const { health, keyPool, global, loading } = useDashboard();
  const { privacyMode } = usePrivacy();

  if (loading) {
    return <div className="text-muted-foreground">Loading...</div>;
  }

  const keys = keyPool?.keys ?? [];
  const totalKeys = keyPool?.total_keys ?? 0;

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Key Pool</h1>

      <div className="grid gap-4 md:grid-cols-3">
        <StatCard
          title="Total Keys"
          value={String(totalKeys)}
          subtitle={totalKeys === 0 ? 'passthrough mode' : 'configured'}
          icon={Key}
          variant={totalKeys > 0 ? 'success' : 'default'}
        />
        <StatCard
          title="Global Concurrency"
          value={`${global?.global_in_flight ?? 0} / ${global?.global_limit ?? 0}`}
          subtitle="in-flight / limit"
          icon={Activity}
          variant="default"
        />
        <StatCard
          title="Queue Depth"
          value={String(health?.queue_depth ?? 0)}
          subtitle="pending requests"
          icon={Server}
          variant={(health?.queue_depth ?? 0) > 0 ? 'warning' : 'default'}
        />
      </div>

      {totalKeys > 0 ? (
        <>
          <PoolHealthSummary keyPool={keyPool!} />
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Keys</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b text-left text-muted-foreground">
                      <th className="pb-2 font-medium">Key</th>
                      <th className="pb-2 font-medium">RPM</th>
                      <th className="pb-2 font-medium">RPM Util</th>
                      <th className="pb-2 font-medium">Status</th>
                    </tr>
                  </thead>
                  <tbody>
                    {keys.map((k, i) => {
                      const rpmUsed = k.rpm_used ?? k.rpm ?? 0;
                      const rpmLimit = k.rpm_limit ?? 0;
                      const rpmPct = rpmLimit > 0 ? (rpmUsed / rpmLimit) * 100 : 0;
                      return (
                        <tr key={i} className="border-b last:border-0">
                          <td className={cn('py-2 font-mono text-xs', privacyMode && PRIVACY_BLUR_CLASS)}>
                            ...{k.suffix}
                          </td>
                          <td className="py-2">{rpmUsed} / {rpmLimit}</td>
                          <td className="py-2 min-w-[100px]">
                            <div className="flex items-center gap-2">
                              <Progress value={rpmPct} className="h-1.5 flex-1" />
                              <span className="text-xs text-muted-foreground w-10 text-right">
                                {formatPercent(rpmUsed, rpmLimit)}
                              </span>
                            </div>
                          </td>
                          <td className="py-2">
                            <KeyHealthIndicator entry={k} />
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            </CardContent>
          </Card>
        </>
      ) : (
        <Card>
          <CardHeader>
            <CardTitle className="text-base flex items-center gap-2">
              <Key className="h-4 w-4" /> Pool Status
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div className="text-center py-12 text-muted-foreground">
              <Key className="h-12 w-12 mx-auto mb-4 opacity-30" />
              <p className="text-lg font-medium">Passthrough mode</p>
              <p className="text-sm mt-1">
                No upstream API keys configured. Client keys are forwarded directly.
              </p>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
