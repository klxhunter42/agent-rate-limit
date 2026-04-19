import { useState } from 'react';
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';
import { Globe, Loader2, Eye, EyeOff } from 'lucide-react';

interface SessionCookieDialogProps {
  open: boolean;
  onClose: () => void;
  provider: string;
  providerName: string;
  onSubmit: (cookie: string) => Promise<void>;
}

export function SessionCookieDialog({ open, onClose, providerName, onSubmit }: SessionCookieDialogProps) {
  const [cookie, setCookie] = useState('');
  const [show, setShow] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async () => {
    if (!cookie.trim()) return;
    setLoading(true);
    setError(null);
    try {
      await onSubmit(cookie.trim());
      setCookie('');
      onClose();
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to register session');
    } finally {
      setLoading(false);
    }
  };

  return (
    <Sheet open={open} onOpenChange={(v) => !v && onClose()}>
      <SheetContent>
        <SheetHeader>
          <SheetTitle className="flex items-center gap-2">
            <Globe className="h-4 w-4" />
            Add {providerName} Session
          </SheetTitle>
        </SheetHeader>

        <div className="space-y-4 mt-6">
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <label className="text-sm font-medium">Browser Cookies</label>
              <button
                type="button"
                onClick={() => setShow(!show)}
                className="text-xs text-muted-foreground hover:text-foreground flex items-center gap-1"
              >
                {show ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
                {show ? 'Hide' : 'Show'}
              </button>
            </div>
            <Textarea
              value={cookie}
              onChange={(e) => setCookie(e.target.value)}
              placeholder="Paste your browser cookies here...&#10;&#10;Example: sessionKey=abc123; otherCookie=value; ..."
              className={`font-mono text-xs min-h-[120px] ${show ? '' : 'blur-[3px] focus:blur-none'}`}
              onKeyDown={(e) => e.key === 'Enter' && e.metaKey && handleSubmit()}
            />
          </div>

          {error && (
            <p className="text-sm text-red-500">{error}</p>
          )}

          <div className="rounded-md border border-amber-500/30 bg-amber-500/5 p-3 space-y-1.5">
            <p className="text-xs font-medium text-amber-500">How to get your cookies:</p>
            <ol className="text-xs text-muted-foreground space-y-1 list-decimal list-inside">
              <li>Log into {providerName} in your browser</li>
              <li>Open DevTools (F12) → Application → Cookies</li>
              <li>Copy all cookies as <code className="font-mono text-xs">name=value; name2=value2</code></li>
              <li>Paste them above</li>
            </ol>
          </div>

          <div className="flex gap-2">
            <Button variant="outline" onClick={onClose} disabled={loading} className="flex-1">
              Cancel
            </Button>
            <Button onClick={handleSubmit} disabled={!cookie.trim() || loading} className="flex-1">
              {loading ? <Loader2 className="h-4 w-4 animate-spin mr-1" /> : <Globe className="h-4 w-4 mr-1" />}
              Connect
            </Button>
          </div>
        </div>
      </SheetContent>
    </Sheet>
  );
}
