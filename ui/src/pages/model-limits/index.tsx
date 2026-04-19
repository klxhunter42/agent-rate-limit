import { useDashboard } from '@/contexts/dashboard-context';
import { Card, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';

export function ModelLimitsPage() {
  const { models, loading } = useDashboard();

  if (loading && models.length === 0) {
    return <div className="text-muted-foreground">Loading...</div>;
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Model Limits</h1>

      <Card>
        <CardContent className="p-0">
          <div className="overflow-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="text-left p-3 font-medium">Model</th>
                  <th className="text-right p-3 font-medium">Series</th>
                  <th className="text-right p-3 font-medium">In-Flight</th>
                  <th className="text-right p-3 font-medium">Limit</th>
                  <th className="text-right p-3 font-medium">Max</th>
                  <th className="text-right p-3 font-medium">Ceiling</th>
                  <th className="text-right p-3 font-medium">Min RTT</th>
                  <th className="text-right p-3 font-medium">EWMA RTT</th>
                  <th className="text-right p-3 font-medium">Requests</th>
                  <th className="text-right p-3 font-medium">429s</th>
                  <th className="text-center p-3 font-medium">Status</th>
                </tr>
              </thead>
              <tbody>
                {models.map((m) => (
                  <tr key={m.name} className="border-b hover:bg-muted/30 transition-colors">
                    <td className="p-3 font-mono font-medium">{m.name}</td>
                    <td className="p-3 text-right text-muted-foreground">{m.series}</td>
                    <td className="p-3 text-right">{m.in_flight}</td>
                    <td className="p-3 text-right font-medium">{m.limit}</td>
                    <td className="p-3 text-right text-muted-foreground">{m.max_limit}</td>
                    <td className="p-3 text-right text-muted-foreground">
                      {m.learned_ceiling > 0 ? m.learned_ceiling : '-'}
                    </td>
                    <td className="p-3 text-right">{m.min_rtt_ms}ms</td>
                    <td className="p-3 text-right">{m.ewma_rtt_ms}ms</td>
                    <td className="p-3 text-right">{m.total_requests.toLocaleString()}</td>
                    <td className="p-3 text-right">
                      {m.total_429s > 0 ? (
                        <span className="text-destructive font-medium">{m.total_429s}</span>
                      ) : (
                        '0'
                      )}
                    </td>
                    <td className="p-3 text-center">
                      {m.overridden ? (
                        <Badge variant="secondary">Pinned</Badge>
                      ) : (
                        <Badge variant="outline">Adaptive</Badge>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
