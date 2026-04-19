import { useState, useEffect, useCallback } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';

interface ErrorLog {
  time: string;
  method: string;
  path: string;
  status: number;
  duration_ms: number;
  error: string;
  model: string;
}

export function LogsPage() {
  const [logs, setLogs] = useState<ErrorLog[]>([]);
  const [loading, setLoading] = useState(true);

  const fetchLogs = useCallback(async () => {
    try {
      const res = await fetch('/v1/logs/errors');
      if (!res.ok) throw new Error(`${res.status}`);
      const data = await res.json();
      setLogs(Array.isArray(data) ? data : data.logs ?? []);
    } catch {
      setLogs([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchLogs();
    const id = setInterval(fetchLogs, 5_000);
    return () => clearInterval(id);
  }, [fetchLogs]);

  const count4xx = logs.filter((l) => l.status >= 400 && l.status < 500).length;
  const count5xx = logs.filter((l) => l.status >= 500).length;

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Error Logs</h1>

      <div className="grid grid-cols-2 gap-4">
        <Card>
          <CardContent className="pt-6">
            <div className="text-2xl font-bold text-amber-500">{count4xx}</div>
            <div className="text-sm text-muted-foreground">Client Errors (4xx)</div>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="pt-6">
            <div className="text-2xl font-bold text-red-500">{count5xx}</div>
            <div className="text-sm text-muted-foreground">Server Errors (5xx)</div>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Recent Errors</CardTitle>
        </CardHeader>
        <CardContent>
          {loading ? (
            <div className="text-center py-8 text-muted-foreground text-sm">Loading...</div>
          ) : logs.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground text-sm">No errors recorded</div>
          ) : (
            <div className="overflow-auto">
              <table className="w-full text-sm">
                <thead>
                  <tr className="border-b text-left text-muted-foreground">
                    <th className="pb-2 pr-4 whitespace-nowrap">Time</th>
                    <th className="pb-2 pr-4">Method</th>
                    <th className="pb-2 pr-4">Path</th>
                    <th className="pb-2 pr-4">Status</th>
                    <th className="pb-2 pr-4">Duration</th>
                    <th className="pb-2 pr-4">Error</th>
                    <th className="pb-2">Model</th>
                  </tr>
                </thead>
                <tbody>
                  {logs.map((log, i) => (
                    <tr key={i} className="border-b last:border-0">
                      <td className="py-2 pr-4 whitespace-nowrap text-muted-foreground">
                        {log.time}
                      </td>
                      <td className="py-2 pr-4">
                        <Badge variant="outline" className="font-mono text-xs">
                          {log.method}
                        </Badge>
                      </td>
                      <td className="py-2 pr-4 font-mono text-xs">{log.path}</td>
                      <td className="py-2 pr-4">
                        <Badge
                          className={cn(
                            log.status >= 500
                              ? 'bg-red-500/10 text-red-500 border-red-500/20'
                              : 'bg-amber-500/10 text-amber-500 border-amber-500/20'
                          )}
                        >
                          {log.status}
                        </Badge>
                      </td>
                      <td className="py-2 pr-4 tabular-nums">{log.duration_ms}ms</td>
                      <td className="py-2 pr-4 font-mono text-xs max-w-xs truncate" title={log.error}>
                        {log.error}
                      </td>
                      <td className="py-2 font-mono text-xs">{log.model || '-'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
