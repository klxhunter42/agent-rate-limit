import { cn } from '@/lib/utils';
import type { TimelineEvent } from '@/hooks/use-event-timeline';

const severityStyles: Record<string, string> = {
  info: 'bg-blue-500',
  warning: 'bg-amber-500',
  error: 'bg-red-500',
};

export function EventTimeline({ events }: { events: TimelineEvent[] }) {
  const visible = events.slice(-10).reverse();

  if (visible.length === 0) {
    return (
      <div className="rounded-lg border bg-muted/30 p-4">
        <p className="text-xs font-medium mb-1">Event Timeline</p>
        <p className="text-sm text-muted-foreground">No events yet. Activity will appear here.</p>
      </div>
    );
  }

  return (
    <div className="rounded-lg border bg-muted/30 p-4">
      <p className="text-xs font-medium mb-3">Recent Events</p>
      <div className="space-y-2 max-h-[280px] overflow-y-auto">
        {visible.map((e) => (
          <div key={e.id} className="flex items-start gap-2">
            <div className={cn('h-2 w-2 rounded-full mt-1.5 shrink-0', severityStyles[e.severity])} />
            <div className="min-w-0 flex-1">
              <p className="text-sm">{e.message}</p>
              <p className="text-xs text-muted-foreground">{e.time.toLocaleTimeString()}</p>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
