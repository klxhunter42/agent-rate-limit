import { useState, useEffect, useCallback } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Progress } from '@/components/ui/progress';
import { Activity, AlertTriangle, RefreshCw } from 'lucide-react';
import { Button } from '@/components/ui/button';
import type { AccountInfo } from '@/lib/auth-api';
import { listAccounts } from '@/lib/auth-api';

interface ModelQuota {
  name: string;
  displayName: string;
  percentage: number;
  resetTime?: string;
}

interface QuotaResult {
  success: boolean;
  models: ModelQuota[];
  lastUpdated: string;
  error?: string;
  accountId: string;
  provider: string;
}

interface ProviderQuotaResult {
  provider: string;
  accounts: QuotaResult[];
  lastUpdated: string;
}

interface ModelUsage {
  model: string;
  input_tokens: number;
  output_tokens: number;
  cost: number;
  requests: number;
  errors: number;
}

interface UsageResponse {
  period: string;
  models: ModelUsage[];
}

import { useDashboard } from '@/contexts/dashboard-context';

function pctColor(pct: number): string {
  if (pct >= 95) return 'text-red-500';
  if (pct >= 80) return 'text-yellow-500';
  if (pct >= 50) return 'text-blue-500';
  return 'text-green-500';
}

function pctVariant(pct: number): 'destructive' | 'secondary' | 'outline' {
  if (pct >= 95) return 'destructive';
  if (pct >= 80) return 'secondary';
  return 'outline';
}

function fmtNum(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M';
  if (n >= 1_000) return (n / 1_000).toFixed(1) + 'K';
  return n.toLocaleString();
}

function fmtCost(n: number): string {
  if (n === 0) return '$0';
  if (n < 0.01) return '<$0.01';
  return '$' + n.toFixed(2);
}

export function QuotaPage() {
  const { glmMode } = useDashboard();
  const [accounts, setAccounts] = useState<AccountInfo[]>([]);
  const [providerResults, setProviderResults] = useState<ProviderQuotaResult[]>([]);
  const [usageMap, setUsageMap] = useState<Record<string, ModelUsage>>({});
  const [loading, setLoading] = useState(true);

  const fetchData = useCallback(async () => {
    try {
      const accs = await listAccounts();
      setAccounts(accs);

      const providers = new Set<string>();
      for (const a of accs) providers.add(a.provider);
      if (glmMode) providers.add('zai');

      const results: ProviderQuotaResult[] = [];
      for (const provider of providers) {
        try {
          const res = await fetch(`/v1/quota/${encodeURIComponent(provider)}`);
          if (res.ok) {
            const data: ProviderQuotaResult = await res.json();
            results.push(data);
          }
        } catch {
          // skip failed provider
        }
      }
      setProviderResults(results);

      // Fetch real usage data
      try {
        const res = await fetch('/v1/usage/models?period=24h');
        if (res.ok) {
          const data: UsageResponse = await res.json();
          const map: Record<string, ModelUsage> = {};
          for (const m of data.models) {
            map[m.model] = m;
          }
          setUsageMap(map);
        }
      } catch {
        // usage fetch failed, proceed without it
      }
    } catch {
      setAccounts([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { fetchData(); }, [fetchData]);

  if (loading) {
    return (
      <div className="space-y-6">
        <h1 className="text-2xl font-bold">Quota</h1>
        <div className="text-center py-8 text-muted-foreground text-sm">Loading quota data...</div>
      </div>
    );
  }

  if (providerResults.length === 0) {
    return (
      <div className="space-y-6">
        <h1 className="text-2xl font-bold">Quota</h1>
        <div className="text-center py-8 text-muted-foreground text-sm">
          {accounts.length === 0 ? 'No accounts configured' : 'No quota data available'}
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Quota &amp; Usage (24h)</h1>
        <Button variant="outline" size="sm" onClick={fetchData}>
          <RefreshCw className="h-4 w-4 mr-1" /> Refresh
        </Button>
      </div>

      {providerResults.map((pr) => (
        <Card key={pr.provider}>
          <CardHeader>
            <CardTitle className="text-base flex items-center gap-2">
              {pr.provider}
              <Badge variant="secondary">{pr.accounts.length} account(s)</Badge>
            </CardTitle>
            <p className="text-xs text-muted-foreground">Updated: {pr.lastUpdated}</p>
          </CardHeader>
          <CardContent>
            {pr.accounts.map((account) => (
              <div key={account.accountId} className="mb-4 last:mb-0">
                <div className="flex items-center gap-2 mb-2">
                  <span className="text-sm font-medium">{account.accountId}</span>
                  {account.error ? (
                    <Badge variant="destructive" className="text-xs">
                      <AlertTriangle className="h-3 w-3 mr-1" /> {account.error}
                    </Badge>
                  ) : (
                    <Badge variant="outline" className="text-xs">
                      <Activity className="h-3 w-3 mr-1" /> Active
                    </Badge>
                  )}
                </div>

                {account.models && account.models.length > 0 && (
                  <div className="overflow-auto">
                    <table className="w-full text-sm">
                      <thead>
                        <tr className="border-b text-left text-muted-foreground">
                          <th className="pb-2 pr-4">Model</th>
                          <th className="pb-2 pr-4">Quota</th>
                          <th className="pb-2 pr-4">Requests</th>
                          <th className="pb-2 pr-4">Tokens In/Out</th>
                          <th className="pb-2 pr-4">Cost</th>
                          <th className="pb-2 pr-4">Errors</th>
                          <th className="pb-2 pr-4">Reset</th>
                          <th className="pb-2">Status</th>
                        </tr>
                      </thead>
                      <tbody>
                        {account.models.map((m) => {
                          const usage = usageMap[m.name];
                          return (
                            <tr key={m.name} className="border-b last:border-0">
                              <td className="py-2 pr-4 font-mono text-xs">{m.displayName}</td>
                              <td className="py-2 pr-4 w-48">
                                <div className="flex items-center gap-2">
                                  <Progress value={Math.min(m.percentage, 100)} className="h-2 flex-1" />
                                  <span className={`text-xs font-mono tabular-nums ${pctColor(m.percentage)}`}>
                                    {m.percentage.toFixed(1)}%
                                  </span>
                                </div>
                              </td>
                              <td className="py-2 pr-4 text-xs font-mono tabular-nums">
                                {usage ? fmtNum(usage.requests) : '-'}
                              </td>
                              <td className="py-2 pr-4 text-xs font-mono tabular-nums">
                                {usage ? (
                                  <span>
                                    {fmtNum(usage.input_tokens)}
                                    <span className="text-muted-foreground">/</span>
                                    {fmtNum(usage.output_tokens)}
                                  </span>
                                ) : '-'}
                              </td>
                              <td className="py-2 pr-4 text-xs font-mono tabular-nums">
                                {usage ? fmtCost(usage.cost) : '-'}
                              </td>
                              <td className="py-2 pr-4 text-xs font-mono tabular-nums">
                                {usage && usage.errors > 0 ? (
                                  <span className="text-red-500">{usage.errors}</span>
                                ) : usage ? (
                                  <span className="text-green-500">0</span>
                                ) : '-'}
                              </td>
                              <td className="py-2 pr-4 text-xs text-muted-foreground">
                                {m.resetTime ? new Date(m.resetTime).toLocaleString() : '-'}
                              </td>
                              <td className="py-2">
                                <Badge variant={pctVariant(m.percentage)} className="text-xs">
                                  {m.percentage >= 95 ? 'Critical' : m.percentage >= 80 ? 'Warning' : 'OK'}
                                </Badge>
                              </td>
                            </tr>
                          );
                        })}
                      </tbody>
                    </table>
                  </div>
                )}
              </div>
            ))}
          </CardContent>
        </Card>
      ))}
    </div>
  );
}
