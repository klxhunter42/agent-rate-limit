import { useState, useEffect, useMemo, useCallback } from 'react';
import { listAccounts, type AccountInfo } from '@/lib/auth-api';
import { fetchMetrics, parsePrometheusText, type ParsedMetric } from '@/lib/api';
import { providerName } from '@/lib/providers';
import { LivePulse } from './live-pulse';
import { ProviderCard, type ProviderStats, getSuccessRate } from './provider-card';
import { Activity, CheckCircle2, XCircle, Radio, Pause } from 'lucide-react';

function groupByProvider(accounts: AccountInfo[], metrics: ParsedMetric[]): ProviderStats[] {
  const map = new Map<string, AccountInfo[]>();
  for (const acc of accounts) {
    const list = map.get(acc.provider) ?? [];
    list.push(acc);
    map.set(acc.provider, list);
  }

  // Extract success/error from Prometheus metrics
  // api_gateway_error_total{type="..."} and api_gateway_rate_limit_hits_total{key="..."}
  const successByProvider = new Map<string, number>();
  const failureByProvider = new Map<string, number>();

  for (const m of metrics) {
    if (m.name === 'api_gateway_request_latency_seconds_count') {
      // status label gives us success/failure per provider via path
      const status = m.labels.status;
      if (status && status.startsWith('2')) {
        // Extract provider from path if possible
        const provider = m.labels.provider;
        if (provider) {
          successByProvider.set(provider, (successByProvider.get(provider) ?? 0) + m.value);
        }
      } else if (status && status.startsWith('4')) {
        const provider = m.labels.provider;
        if (provider) {
          failureByProvider.set(provider, (failureByProvider.get(provider) ?? 0) + m.value);
        }
      }
    }
    if (m.name === 'api_gateway_rate_limit_hits_total') {
      const key = m.labels.key;
      if (key) {
        failureByProvider.set(key, (failureByProvider.get(key) ?? 0) + m.value);
      }
    }
  }

  return Array.from(map.entries())
    .map(([provider, accs]) => {
      const active = accs.filter((a) => !a.paused);
      return {
        provider,
        displayName: providerName(provider.toLowerCase()),
        accounts: accs,
        activeCount: active.length,
        pausedCount: accs.length - active.length,
        successCount: successByProvider.get(provider) ?? 0,
        failureCount: failureByProvider.get(provider) ?? 0,
      };
    })
    .sort((a, b) => a.displayName.localeCompare(b.displayName));
}

function timeSinceLabel(lastFetch: number): string {
  const diff = Math.floor((Date.now() - lastFetch) / 1000);
  if (diff < 5) return 'just now';
  if (diff < 60) return `${diff}s ago`;
  return `${Math.floor(diff / 60)}m ago`;
}

export function LiveAuthMonitor() {
  const [accounts, setAccounts] = useState<AccountInfo[]>([]);
  const [metrics, setMetrics] = useState<ParsedMetric[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [lastFetch, setLastFetch] = useState(Date.now());
  const [timeLabel, setTimeLabel] = useState('just now');

  const refresh = useCallback(async () => {
    try {
      const [accs, text] = await Promise.all([listAccounts(), fetchMetrics()]);
      setAccounts(accs);
      setMetrics(parsePrometheusText(text));
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'fetch failed');
    } finally {
      setLoading(false);
      setLastFetch(Date.now());
    }
  }, []);

  useEffect(() => {
    refresh();
    const id = setInterval(refresh, 5000);
    return () => clearInterval(id);
  }, [refresh]);

  // Update time label every second
  useEffect(() => {
    const id = setInterval(() => setTimeLabel(timeSinceLabel(lastFetch)), 1000);
    return () => clearInterval(id);
  }, [lastFetch]);

  const providerStats = useMemo(() => groupByProvider(accounts, metrics), [accounts, metrics]);

  const totalActive = accounts.filter((a) => !a.paused).length;
  const totalPaused = accounts.length - totalActive;
  const totalSuccess = providerStats.reduce((s, p) => s + p.successCount, 0);
  const totalFailure = providerStats.reduce((s, p) => s + p.failureCount, 0);
  const overallRate = getSuccessRate(totalSuccess, totalFailure);
  const rateColor = overallRate >= 95 ? '#10b981' : overallRate >= 80 ? '#f59e0b' : '#ef4444';

  if (loading) {
    return (
      <div className="rounded-xl border border-border overflow-hidden font-mono text-[13px] bg-card/50 dark:bg-zinc-900/60 backdrop-blur-sm">
        <div className="flex items-center justify-between px-4 py-3 border-b border-border">
          <div className="h-4 w-32 bg-muted animate-pulse rounded" />
          <div className="h-4 w-20 bg-muted animate-pulse rounded" />
        </div>
        <div className="p-4 space-y-4">
          <div className="flex gap-3">
            {[1, 2, 3, 4].map((i) => (
              <div key={i} className="h-16 flex-1 bg-muted animate-pulse rounded-lg" />
            ))}
          </div>
          <div className="grid grid-cols-2 lg:grid-cols-3 gap-4">
            {[1, 2, 3].map((i) => (
              <div key={i} className="h-32 bg-muted animate-pulse rounded-xl" />
            ))}
          </div>
        </div>
      </div>
    );
  }

  if (error && accounts.length === 0) {
    return (
      <div className="rounded-xl border border-border overflow-hidden font-mono text-[13px] bg-card/50 dark:bg-zinc-900/60 backdrop-blur-sm">
        <div className="flex items-center justify-between px-4 py-3 border-b border-border">
          <div className="flex items-center gap-2">
            <XCircle className="w-4 h-4 text-red-500" />
            <span className="text-xs font-semibold">Auth Monitor</span>
          </div>
          <span className="text-[10px] text-muted-foreground">{error}</span>
        </div>
      </div>
    );
  }

  return (
    <div className="rounded-xl border border-border overflow-hidden font-mono text-[13px] text-foreground bg-card/50 dark:bg-zinc-900/60 backdrop-blur-sm">
      {/* Live Header */}
      <div className="flex items-center justify-between px-4 py-2.5 border-b border-border bg-gradient-to-r from-emerald-500/5 via-transparent to-transparent dark:from-emerald-500/10">
        <div className="flex items-center gap-2">
          <LivePulse />
          <span className="text-xs font-semibold tracking-tight">LIVE</span>
          <span className="text-[10px] text-muted-foreground">Auth Monitor</span>
        </div>
        <div className="flex items-center gap-4 text-[10px] text-muted-foreground">
          <div className="flex items-center gap-1.5">
            <Radio className="w-3 h-3 animate-pulse" />
            <span>{timeLabel}</span>
          </div>
          <span className="text-muted-foreground/50">|</span>
          <span>{accounts.length} accounts</span>
          <span>{providerStats.length} providers</span>
        </div>
      </div>

      {/* Summary Stats */}
      <div className="grid grid-cols-4 gap-3 p-4 border-b border-border bg-muted/20 dark:bg-zinc-900/30">
        <div className="flex items-center gap-3 p-3 rounded-lg bg-card/50 dark:bg-zinc-900/50 border border-border/50 dark:border-white/[0.06]">
          <div className="w-8 h-8 rounded-md flex items-center justify-center bg-blue-500/15 text-blue-500">
            <Activity className="w-4 h-4" />
          </div>
          <div>
            <div className="text-[10px] text-muted-foreground uppercase tracking-wider">Accounts</div>
            <div className="text-lg font-semibold font-mono leading-tight">{accounts.length}</div>
          </div>
        </div>
        <div className="flex items-center gap-3 p-3 rounded-lg bg-card/50 dark:bg-zinc-900/50 border border-border/50 dark:border-white/[0.06]">
          <div className="w-8 h-8 rounded-md flex items-center justify-center bg-emerald-500/15 text-emerald-500">
            <CheckCircle2 className="w-4 h-4" />
          </div>
          <div>
            <div className="text-[10px] text-muted-foreground uppercase tracking-wider">Active</div>
            <div className="text-lg font-semibold font-mono leading-tight">{totalActive}</div>
          </div>
        </div>
        <div className="flex items-center gap-3 p-3 rounded-lg bg-card/50 dark:bg-zinc-900/50 border border-border/50 dark:border-white/[0.06]">
          <div
            className="w-8 h-8 rounded-md flex items-center justify-center"
            style={{
              backgroundColor: totalPaused > 0 ? '#ef444415' : 'var(--muted)',
              color: totalPaused > 0 ? '#ef4444' : 'var(--muted-foreground)',
            }}
          >
            <Pause className="w-4 h-4" />
          </div>
          <div>
            <div className="text-[10px] text-muted-foreground uppercase tracking-wider">Paused</div>
            <div className="text-lg font-semibold font-mono leading-tight">{totalPaused}</div>
          </div>
        </div>
        <div className="flex items-center gap-3 p-3 rounded-lg bg-card/50 dark:bg-zinc-900/50 border border-border/50 dark:border-white/[0.06]">
          <div
            className="w-8 h-8 rounded-md flex items-center justify-center"
            style={{ backgroundColor: `${rateColor}15`, color: rateColor }}
          >
            <Activity className="w-4 h-4" />
          </div>
          <div>
            <div className="text-[10px] text-muted-foreground uppercase tracking-wider">Success</div>
            <div className="text-lg font-semibold font-mono leading-tight" style={{ color: rateColor }}>
              {overallRate}%
            </div>
          </div>
        </div>
      </div>

      {/* Provider Cards Grid */}
      <div className="p-4">
        <div className="text-[10px] text-muted-foreground uppercase tracking-widest mb-3">
          Accounts by Provider
        </div>
        {providerStats.length > 0 ? (
          <div className="grid grid-cols-2 lg:grid-cols-3 gap-4">
            {providerStats.map((ps) => (
              <ProviderCard key={ps.provider} stats={ps} />
            ))}
          </div>
        ) : (
          <div className="p-6 rounded-xl border border-dashed border-muted-foreground/30 bg-muted/10 flex flex-col items-center justify-center text-center">
            <Activity className="w-5 h-5 text-muted-foreground/50 mb-1.5" />
            <span className="text-xs text-muted-foreground">No authenticated accounts</span>
            <span className="text-[10px] text-muted-foreground/70">Add accounts via Auth page</span>
          </div>
        )}
      </div>
    </div>
  );
}
