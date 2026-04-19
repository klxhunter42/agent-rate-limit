import { useState, useEffect, useCallback } from 'react';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { toast } from 'sonner';
import { ArrowLeftRight, Layers } from 'lucide-react';
import { cn } from '@/lib/utils';

type Strategy = 'round-robin' | 'fill-first';

export function RoutingStrategy() {
  const [strategy, setStrategy] = useState<Strategy | null>(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  const fetchStrategy = useCallback(async () => {
    try {
      const res = await fetch('/v1/routing/strategy');
      if (!res.ok) throw new Error(`${res.status}`);
      const data = await res.json();
      setStrategy(data.strategy ?? 'round-robin');
    } catch {
      toast.error('Failed to fetch routing strategy');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchStrategy();
    const id = setInterval(fetchStrategy, 10_000);
    return () => clearInterval(id);
  }, [fetchStrategy]);

  const handleSwitch = async (next: Strategy) => {
    if (next === strategy || saving) return;
    setSaving(true);
    try {
      const res = await fetch('/v1/routing/strategy', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ strategy: next }),
      });
      if (!res.ok) throw new Error(`${res.status}`);
      setStrategy(next);
      toast.success(`Routing strategy: ${next}`);
    } catch {
      toast.error('Failed to update routing strategy');
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <Card>
        <CardContent className="py-8 text-center text-muted-foreground text-sm">
          Loading strategy...
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base flex items-center gap-2">
          <ArrowLeftRight className="h-4 w-4" /> Routing Strategy
        </CardTitle>
        <CardDescription>How requests are distributed across keys</CardDescription>
      </CardHeader>
      <CardContent>
        <div className="flex gap-3">
          <Button
            variant="outline"
            className={cn(
              'flex-1 h-12 flex-col gap-1',
              strategy === 'round-robin' && 'border-primary bg-primary/10 text-primary'
            )}
            disabled={saving}
            onClick={() => handleSwitch('round-robin')}
          >
            <ArrowLeftRight className="h-4 w-4" />
            <span className="text-sm font-medium">Round Robin</span>
          </Button>
          <Button
            variant="outline"
            className={cn(
              'flex-1 h-12 flex-col gap-1',
              strategy === 'fill-first' && 'border-primary bg-primary/10 text-primary'
            )}
            disabled={saving}
            onClick={() => handleSwitch('fill-first')}
          >
            <Layers className="h-4 w-4" />
            <span className="text-sm font-medium">Fill First</span>
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
