import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Activity, Clock, Box } from 'lucide-react';
import { cn } from '@/lib/utils';
import { formatUptime } from '@/lib/format';

interface ContainerStatusProps {
  uptime: number;
  queueDepth: number;
  version?: string;
}

function healthStatus(queueDepth: number): { label: string; color: string; dotColor: string } {
  if (queueDepth === 0) return { label: 'Healthy', color: 'text-emerald-500', dotColor: 'bg-emerald-500' };
  if (queueDepth < 20) return { label: 'Busy', color: 'text-amber-500', dotColor: 'bg-amber-500' };
  return { label: 'Stressed', color: 'text-red-500', dotColor: 'bg-red-500' };
}

export function ContainerStatusCard({ uptime, queueDepth, version }: ContainerStatusProps) {
  const status = healthStatus(queueDepth);

  return (
    <Card className="border-transparent hover:border-border transition-all duration-200">
      <CardHeader className="flex flex-row items-center justify-between pb-2">
        <CardTitle className="text-base flex items-center gap-2">
          <Box className="h-4 w-4 text-muted-foreground" />
          Container
        </CardTitle>
        <div className="flex items-center gap-1.5">
          <div className={cn('h-2 w-2 rounded-full', status.dotColor)} />
          <span className={cn('text-xs font-medium', status.color)}>{status.label}</span>
        </div>
      </CardHeader>
      <CardContent className="p-4 pt-0">
        <div className="grid grid-cols-2 gap-3">
          <div className="flex items-center gap-2">
            <Clock className="h-4 w-4 text-muted-foreground shrink-0" />
            <div className="min-w-0">
              <p className="text-xs text-muted-foreground">Uptime</p>
              <p className="text-sm font-mono font-medium truncate">{formatUptime(uptime)}</p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Activity className="h-4 w-4 text-muted-foreground shrink-0" />
            <div className="min-w-0">
              <p className="text-xs text-muted-foreground">Queue</p>
              <p className={cn('text-sm font-mono font-medium', status.color)}>{queueDepth}</p>
            </div>
          </div>
        </div>
        {version && (
          <div className="mt-3">
            <Badge variant="outline" className="text-[10px] font-mono">
              v{version}
            </Badge>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
