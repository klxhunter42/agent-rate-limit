import { useState } from 'react';
import { useDashboard } from '@/contexts/dashboard-context';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Badge } from '@/components/ui/badge';
import { setOverride } from '@/lib/api';
import { toast } from 'sonner';
import { Settings2, Trash2 } from 'lucide-react';
import { RoutingStrategy } from '@/components/routing/routing-strategy';

export function ControlsPage() {
  const { models, refresh } = useDashboard();
  const [selectedModel, setSelectedModel] = useState('');
  const [limitValue, setLimitValue] = useState('');
  const [saving, setSaving] = useState(false);

  const overriddenModels = models.filter((m) => m.overridden);

  const handleApply = async () => {
    if (!selectedModel || !limitValue) return;
    const limit = parseInt(limitValue, 10);
    if (Number.isNaN(limit) || limit < 0) {
      toast.error('Limit must be a non-negative integer');
      return;
    }
    setSaving(true);
    try {
      await setOverride({ model: selectedModel, limit });
      toast.success(`Override applied: ${selectedModel} = ${limit}`);
      refresh();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to apply override');
    } finally {
      setSaving(false);
    }
  };

  const handleClear = async (model: string) => {
    try {
      await setOverride({ model, limit: 0 });
      toast.success(`Override cleared for ${model}`);
      refresh();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : 'Failed to clear override');
    }
  };

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Controls</h1>

      {/* Apply override */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base flex items-center gap-2">
            <Settings2 className="h-4 w-4" /> Manual Override
          </CardTitle>
          <CardDescription>Pin a model's concurrency limit to a specific value</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex gap-3 items-end">
            <div className="flex-1">
              <label className="text-sm font-medium mb-1 block">Model</label>
              <Select value={selectedModel} onValueChange={setSelectedModel}>
                <SelectTrigger>
                  <SelectValue placeholder="Select model" />
                </SelectTrigger>
                <SelectContent>
                  {models.map((m) => (
                    <SelectItem key={m.name} value={m.name}>
                      {m.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="w-32">
              <label className="text-sm font-medium mb-1 block">Limit</label>
              <Input
                type="number"
                min="0"
                value={limitValue}
                onChange={(e) => setLimitValue(e.target.value)}
                placeholder="e.g. 10"
              />
            </div>
            <Button onClick={handleApply} disabled={!selectedModel || !limitValue || saving}>
              Apply
            </Button>
          </div>
        </CardContent>
      </Card>

      {/* Active overrides */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Active Overrides</CardTitle>
        </CardHeader>
        <CardContent>
          {overriddenModels.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground text-sm">
              No active overrides. All models are using adaptive limits.
            </div>
          ) : (
            <div className="space-y-2">
              {overriddenModels.map((m) => (
                <div key={m.name} className="flex items-center justify-between p-3 rounded-md bg-muted/50">
                  <div className="flex items-center gap-3">
                    <span className="font-mono font-medium">{m.name}</span>
                    <Badge variant="secondary">pinned at {m.limit}</Badge>
                  </div>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => handleClear(m.name)}
                    className="text-destructive"
                  >
                    <Trash2 className="h-4 w-4 mr-1" /> Clear
                  </Button>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      <RoutingStrategy />
    </div>
  );
}
