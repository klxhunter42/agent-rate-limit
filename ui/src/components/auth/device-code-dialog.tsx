import { useState, useEffect } from 'react';
import { Button } from '@/components/ui/button';
import { Copy, Check, ExternalLink, X } from 'lucide-react';
import { cn } from '@/lib/utils';

interface DeviceCodeDialogProps {
  open: boolean;
  onClose: () => void;
  userCode: string;
  verificationUrl: string;
  provider: string;
  expiresInSeconds: number;
  error?: string;
}

export function DeviceCodeDialog({
  open,
  onClose,
  userCode,
  verificationUrl,
  provider,
  expiresInSeconds,
  error,
}: DeviceCodeDialogProps) {
  const [copied, setCopied] = useState(false);
  const [remaining, setRemaining] = useState(expiresInSeconds);

  useEffect(() => {
    if (!open) return;
    setRemaining(expiresInSeconds);
    const id = setInterval(() => {
      setRemaining((r) => {
        if (r <= 1) {
          clearInterval(id);
          return 0;
        }
        return r - 1;
      });
    }, 1000);
    return () => clearInterval(id);
  }, [open, expiresInSeconds]);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', onKey);
    return () => document.removeEventListener('keydown', onKey);
  }, [open, onClose]);

  if (!open) return null;

  const handleCopy = () => {
    navigator.clipboard.writeText(userCode);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const minutes = Math.floor(remaining / 60);
  const seconds = remaining % 60;

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

        <h2 className="text-lg font-semibold capitalize">{provider} - Authenticate</h2>
        <p className="text-sm text-muted-foreground mt-1">
          Enter the code below at the verification URL to link your account.
        </p>

        {error && (
          <p className="text-sm text-red-500 mt-2">{error}</p>
        )}

        <div className="mt-6 flex flex-col items-center gap-4">
          <div className="rounded-lg border bg-muted px-8 py-4">
            <code className="text-2xl font-mono font-bold tracking-widest">{userCode}</code>
          </div>

          <Button variant="outline" size="sm" onClick={handleCopy}>
            {copied ? <Check className="h-4 w-4 text-green-500" /> : <Copy className="h-4 w-4" />}
            {copied ? 'Copied' : 'Copy Code'}
          </Button>

          <a
            href={verificationUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="inline-flex items-center gap-1.5 text-sm text-primary hover:underline"
          >
            {verificationUrl}
            <ExternalLink className="h-3.5 w-3.5" />
          </a>

          <Button variant="outline" size="sm" asChild>
            <a href={verificationUrl} target="_blank" rel="noopener noreferrer">
              <ExternalLink className="h-4 w-4" />
              Open URL
            </a>
          </Button>
        </div>

        <div className="mt-6 flex items-center justify-between">
          <span className={cn('text-xs', remaining < 30 ? 'text-amber-500' : 'text-muted-foreground')}>
            Expires in {minutes}:{seconds.toString().padStart(2, '0')}
          </span>
          <Button variant="ghost" size="sm" onClick={onClose}>
            Cancel
          </Button>
        </div>
      </div>
    </div>
  );
}
