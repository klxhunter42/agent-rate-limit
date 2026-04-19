import { useState, useEffect } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { formatCost } from '@/lib/format';
import { providerName } from '@/lib/providers';
import { Search } from 'lucide-react';

interface ModelEntry {
  name: string;
  provider: string;
  format: string;
  series?: string;
  limit?: number;
  input_price: number;
  output_price: number;
}

export function ModelsPage() {
  const [models, setModels] = useState<ModelEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState('');

  useEffect(() => {
    fetch('/v1/models')
      .then((r) => {
        if (!r.ok) throw new Error(`${r.status}`);
        return r.json();
      })
      .then((data) => {
        const raw: unknown[] = Array.isArray(data) ? data : data.models ?? [];
        return raw.map((m: any) => ({
          name: m.name,
          provider: m.provider,
          format: m.format,
          series: m.series,
          limit: m.limit,
          input_price: m.input_price ?? m.input_per_million ?? 0,
          output_price: m.output_price ?? m.output_per_million ?? 0,
        }));
      })
      .then(setModels)
      .catch(() => setModels([]))
      .finally(() => setLoading(false));
  }, []);

  const filtered = models.filter(
    (m) =>
      m.name.toLowerCase().includes(search.toLowerCase()) ||
      m.provider.toLowerCase().includes(search.toLowerCase())
  );

  const grouped = filtered.reduce<Record<string, ModelEntry[]>>((acc, m) => {
    (acc[m.provider] ??= []).push(m);
    return acc;
  }, {});

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Model Catalog</h1>

      <div className="relative">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
        <Input
          placeholder="Search by model name or provider..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="pl-9"
        />
      </div>

      {loading ? (
        <div className="text-center py-8 text-muted-foreground text-sm">Loading models...</div>
      ) : Object.keys(grouped).length === 0 ? (
        <div className="text-center py-8 text-muted-foreground text-sm">
          {search ? 'No models match your search' : 'No models available'}
        </div>
      ) : (
        Object.entries(grouped).map(([provider, entries]) => (
          <Card key={provider}>
            <CardHeader>
              <CardTitle className="text-base flex items-center gap-2">
                {providerName(provider.toLowerCase())}
                <Badge variant="secondary">{entries.length}</Badge>
              </CardTitle>
            </CardHeader>
            <CardContent>
              <div className="overflow-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b text-left text-muted-foreground">
                      <th className="pb-2 pr-4">Model</th>
                      <th className="pb-2 pr-4">Series</th>
                      <th className="pb-2 pr-4">Format</th>
                      <th className="pb-2 pr-4">Conc. Limit</th>
                      <th className="pb-2 pr-4">Input / 1M tokens</th>
                      <th className="pb-2">Output / 1M tokens</th>
                    </tr>
                  </thead>
                  <tbody>
                    {entries.map((m) => (
                      <tr key={m.name} className="border-b last:border-0">
                        <td className="py-2 pr-4 font-mono text-xs">{m.name}</td>
                        <td className="py-2 pr-4 text-xs text-muted-foreground">{m.series || '-'}</td>
                        <td className="py-2 pr-4">
                          <Badge variant="outline" className="text-xs">
                            {m.format}
                          </Badge>
                        </td>
                        <td className="py-2 pr-4 tabular-nums text-xs">{m.limit ?? '-'}</td>
                        <td className="py-2 pr-4 tabular-nums">{formatCost(m.input_price)}</td>
                        <td className="py-2 tabular-nums">{formatCost(m.output_price)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </CardContent>
          </Card>
        ))
      )}
    </div>
  );
}
