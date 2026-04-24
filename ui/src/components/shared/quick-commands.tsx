import { useState, useMemo } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Terminal, Copy, Check } from 'lucide-react';
import { copyToClipboard } from '@/lib/clipboard';

interface Snippet {
  label: string;
  command: string;
  description?: string;
}

function useSnippets(): Snippet[] {
  return useMemo(() => {
    const base = typeof window !== 'undefined'
      ? `${window.location.hostname}:9000`
      : 'localhost:9000';
      return [
      { label: 'Limiter Status', command: `curl ${base}/v1/limiter-status`, description: 'Check all model limits' },
      { label: 'Health Check', command: `curl ${base}/health`, description: 'System health status' },
      { label: 'Metrics', command: `curl ${base}/api/metrics`, description: 'Prometheus metrics' },
      { label: 'Set Override', command: `curl -X POST ${base}/v1/limiter-override -d '{"model":"gpt-4","limit":10}'`, description: 'Pin model limit' },
    ];
  }, []);
}

export function QuickCommands({ snippets }: { snippets?: Snippet[] }) {
  const defaultSnippets = useSnippets();
  const items = snippets ?? defaultSnippets;
  const [copied, setCopied] = useState<string | null>(null);

  const handleCopy = (command: string) => {
    copyToClipboard(command).then(() => {
      setCopied(command);
      setTimeout(() => setCopied(null), 2000);
    });
  };

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="text-base flex items-center gap-2">
          <Terminal className="h-4 w-4" />
          Quick Commands
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="grid gap-3 sm:grid-cols-2">
          {items.map((s) => (
            <div
              key={s.command}
              className="group relative rounded-lg border bg-muted/30 p-3 transition-colors hover:bg-muted/50"
            >
              <p className="text-xs font-medium mb-1">{s.label}</p>
              <code className="text-xs font-mono text-muted-foreground block truncate">{s.command}</code>
              <button
                onClick={() => handleCopy(s.command)}
                className="absolute top-2 right-2 opacity-0 group-hover:opacity-100 transition-opacity p-1 rounded hover:bg-muted"
              >
                {copied === s.command ? (
                  <Check className="h-3.5 w-3.5 text-green-500" />
                ) : (
                  <Copy className="h-3.5 w-3.5 text-muted-foreground" />
                )}
              </button>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  );
}
