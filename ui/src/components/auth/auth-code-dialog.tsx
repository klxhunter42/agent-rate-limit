import { useState } from 'react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Loader2, CheckCircle, XCircle, X, Terminal } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip';

function getSSHTunnelCommand(): string {
  const host = window.location.hostname || 'localhost';
  const port = window.location.port || (window.location.protocol === 'https:' ? '443' : '80');
  return `ssh -L 8080:localhost:8080 -L ${port}:localhost:${port} user@${host}`;
}

interface AuthCodeDialogProps {
  open: boolean;
  onClose: () => void;
  provider: string;
  authUrl: string;
  status: 'opening' | 'waiting' | 'complete' | 'error';
  error?: string;
  onSubmitCallback?: (url: string) => void;
}

export function AuthCodeDialog({
  open,
  onClose,
  provider,
  authUrl,
  status,
  error,
  onSubmitCallback,
}: AuthCodeDialogProps) {
  const [callbackUrl, setCallbackUrl] = useState('');

  if (!open) return null;

  const statusConfig = {
    opening: { icon: <Loader2 className="h-5 w-5 animate-spin text-blue-500" />, text: 'Opening browser...' },
    waiting: { icon: <Loader2 className="h-5 w-5 animate-spin text-blue-500" />, text: 'Waiting for authorization...' },
    complete: { icon: <CheckCircle className="h-5 w-5 text-green-500" />, text: 'Authorization complete!' },
    error: { icon: <XCircle className="h-5 w-5 text-red-500" />, text: error ?? 'Authentication failed' },
  };

  const { icon, text } = statusConfig[status];

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div className="fixed inset-0 bg-black/50" onClick={onClose} />
      <div className="relative z-50 w-full max-w-md rounded-xl border bg-card p-6 shadow-lg">
        <button
          onClick={onClose}
          className="absolute top-4 right-4 text-muted-foreground hover:text-foreground"
        >
          <X className="h-4 w-4" />
        </button>

        <h2 className="text-lg font-semibold capitalize">{provider} - Authenticating</h2>

        <div className="mt-6 flex flex-col items-center gap-3">
          {icon}
          <p className={cn('text-sm', status === 'error' ? 'text-red-500' : 'text-muted-foreground')}>
            {text}
          </p>
        </div>

        {status === 'waiting' && (
          <div className="mt-6 space-y-3">
            <div className="border-t pt-4">
              <div className="flex items-center gap-2 mb-2">
                <p className="text-xs font-medium text-muted-foreground">
                  Manual callback (for remote servers)
                </p>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Terminal className="h-3.5 w-3.5 text-muted-foreground/50 hover:text-muted-foreground cursor-help" />
                  </TooltipTrigger>
                  <TooltipContent side="bottom" className="max-w-[320px]">
                    <p className="font-medium mb-1.5">SSH Tunnel</p>
                    <p className="text-xs text-muted-foreground mb-2">If the gateway is on a remote server, run this on your local machine:</p>
                    <code className="block bg-muted px-2 py-1.5 rounded text-xs font-mono break-all select-all">
                      {getSSHTunnelCommand()}
                    </code>
                  </TooltipContent>
                </Tooltip>
              </div>
              <div className="flex gap-2">
                <Input
                  placeholder="Paste callback URL here..."
                  value={callbackUrl}
                  onChange={(e) => setCallbackUrl(e.target.value)}
                  className="text-xs"
                />
                <Button
                  size="sm"
                  variant="outline"
                  disabled={!callbackUrl.trim()}
                  onClick={() => {
                    if (onSubmitCallback && callbackUrl.trim()) {
                      onSubmitCallback(callbackUrl.trim());
                    }
                  }}
                >
                  Submit
                </Button>
              </div>
            </div>

            <details className="text-xs text-muted-foreground">
              <summary className="cursor-pointer hover:text-foreground">Auth URL (click to expand)</summary>
              <a
                href={authUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="mt-1 block break-all text-primary hover:underline"
              >
                {authUrl}
              </a>
            </details>
          </div>
        )}

        {status === 'complete' && (
          <div className="mt-4 flex justify-end">
            <Button size="sm" onClick={onClose}>
              Done
            </Button>
          </div>
        )}

        {status !== 'complete' && (
          <div className="mt-4 flex justify-end">
            <Button variant="ghost" size="sm" onClick={onClose}>
              Cancel
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}
