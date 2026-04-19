import { useState, useEffect, useMemo } from 'react';
import { Input } from '@/components/ui/input';
import { Loader2, Search } from 'lucide-react';
import { cn } from '@/lib/utils';

interface ModelEntry {
  name: string;
  provider: string;
  format: string;
  series?: string;
  limit?: number;
  input_price: number;
  output_price: number;
}

interface OpenRouterModelPickerProps {
  onSelect: (model: ModelEntry) => void;
  className?: string;
}

const CACHE_KEY = 'arl-openrouter-models';
const CACHE_TTL = 24 * 60 * 60 * 1000; // 24h

interface CacheEntry {
  models: ModelEntry[];
  ts: number;
}

function loadCache(): ModelEntry[] | null {
  try {
    const raw = localStorage.getItem(CACHE_KEY);
    if (!raw) return null;
    const entry: CacheEntry = JSON.parse(raw);
    if (Date.now() - entry.ts > CACHE_TTL) {
      localStorage.removeItem(CACHE_KEY);
      return null;
    }
    return entry.models;
  } catch {
    return null;
  }
}

function saveCache(models: ModelEntry[]) {
  try {
    const entry: CacheEntry = { models, ts: Date.now() };
    localStorage.setItem(CACHE_KEY, JSON.stringify(entry));
  } catch {
    // localStorage full or unavailable
  }
}

export function OpenRouterModelPicker({ onSelect, className }: OpenRouterModelPickerProps) {
  const [models, setModels] = useState<ModelEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState('');

  useEffect(() => {
    const cached = loadCache();
    if (cached) {
      setModels(cached);
      setLoading(false);
      return;
    }

    fetch('/v1/models')
      .then((r) => {
        if (!r.ok) throw new Error(`${r.status}`);
        return r.json();
      })
      .then((data) => {
        const raw: unknown[] = Array.isArray(data) ? data : data.models ?? [];
        const all: ModelEntry[] = raw.map((m: any) => ({
          name: m.name,
          provider: m.provider,
          format: m.format,
          series: m.series,
          limit: m.limit,
          input_price: m.input_price ?? m.input_per_million ?? 0,
          output_price: m.output_price ?? m.output_per_million ?? 0,
        }));
        const openrouter = all.filter((m) => m.provider === 'openrouter');
        setModels(openrouter);
        saveCache(openrouter);
      })
      .catch(() => setModels([]))
      .finally(() => setLoading(false));
  }, []);

  const filtered = useMemo(() => {
    const q = search.toLowerCase();
    if (!q) return models;
    return models.filter(
      (m) =>
        m.name.toLowerCase().includes(q) ||
        (m.series ?? '').toLowerCase().includes(q),
    );
  }, [models, search]);

  if (loading) {
    return (
      <div className="flex items-center gap-2 py-4 justify-center text-muted-foreground text-sm">
        <Loader2 className="h-4 w-4 animate-spin" />
        Loading OpenRouter models...
      </div>
    );
  }

  if (models.length === 0) {
    return (
      <div className="text-sm text-muted-foreground py-4 text-center">
        No OpenRouter models available. Connect an OpenRouter account first.
      </div>
    );
  }

  return (
    <div className={cn('space-y-2', className)}>
      <div className="relative">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
        <Input
          placeholder="Search OpenRouter models..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="pl-9"
        />
      </div>
      <div className="max-h-[320px] overflow-y-auto border rounded-md">
        {filtered.length === 0 ? (
          <div className="text-sm text-muted-foreground py-4 text-center">
            No models match your search
          </div>
        ) : (
          filtered.map((m) => (
            <button
              key={m.name}
              type="button"
              className="w-full text-left px-3 py-2 hover:bg-muted transition-colors flex items-center justify-between gap-2 border-b last:border-0"
              onClick={() => onSelect(m)}
            >
              <div className="min-w-0 flex-1">
                <p className="text-sm font-mono truncate">{m.name}</p>
                {m.series && (
                  <p className="text-xs text-muted-foreground">{m.series}</p>
                )}
              </div>
              <span className="text-xs text-muted-foreground shrink-0">
                {m.format}
              </span>
            </button>
          ))
        )}
      </div>
    </div>
  );
}
