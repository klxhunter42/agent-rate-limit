import { useState } from 'react';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import type { HealthCheckGroup } from '@/lib/health-checks';
import { ChevronDown, Server, ListOrdered, Key, Cpu, AlertTriangle } from 'lucide-react';

const GROUP_ICONS: Record<string, typeof Server> = {
  gateway: Server,
  queue: ListOrdered,
  models: Cpu,
  keys: Key,
  infra: AlertTriangle,
};

const STATUS_DOT: Record<string, string> = {
  ok: 'bg-green-500',
  warning: 'bg-yellow-500',
  error: 'bg-red-500',
  info: 'bg-blue-400',
};

function groupHasIssues(group: HealthCheckGroup): boolean {
  return group.checks.some((c) => c.status === 'error' || c.status === 'warning');
}

export function HealthChecks({ groups }: { groups: HealthCheckGroup[] }) {
  const [openGroups, setOpenGroups] = useState<Set<string>>(() => {
    const set = new Set<string>();
    for (const g of groups) {
      if (groupHasIssues(g)) set.add(g.id);
    }
    return set;
  });

  const toggle = (id: string) => {
    setOpenGroups((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  return (
    <div className="space-y-3">
      {groups.map((group) => {
        const Icon = GROUP_ICONS[group.id] || Server;
        const isOpen = openGroups.has(group.id);
        const issueCount = group.checks.filter((c) => c.status === 'error' || c.status === 'warning').length;

        return (
          <Collapsible key={group.id} open={isOpen} onOpenChange={() => toggle(group.id)}>
            <CollapsibleTrigger className="w-full">
              <div className="flex items-center gap-3 rounded-lg border bg-card p-3 hover:bg-accent/50 transition-colors">
                <Icon className="h-4 w-4 text-muted-foreground shrink-0" />
                <span className="font-medium text-sm flex-1 text-left">{group.name}</span>
                {issueCount > 0 && (
                  <span className={`text-xs px-2 py-0.5 rounded font-medium ${issueCount > 0 ? 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400' : ''}`}>
                    {issueCount}
                  </span>
                )}
                <span className="text-xs text-muted-foreground">{group.checks.length}</span>
                <ChevronDown className={`h-4 w-4 text-muted-foreground transition-transform ${isOpen ? 'rotate-180' : ''}`} />
              </div>
            </CollapsibleTrigger>
            <CollapsibleContent>
              <div className="ml-7 mt-1 space-y-1">
                {group.checks.map((check) => (
                  <div key={check.id} className="flex items-center gap-2 py-1.5 text-sm">
                    <span className={`h-2 w-2 rounded-full shrink-0 ${STATUS_DOT[check.status]}`} />
                    <span className="flex-1">{check.name}</span>
                    <span className="text-muted-foreground text-xs">{check.message}</span>
                  </div>
                ))}
              </div>
            </CollapsibleContent>
          </Collapsible>
        );
      })}
    </div>
  );
}
