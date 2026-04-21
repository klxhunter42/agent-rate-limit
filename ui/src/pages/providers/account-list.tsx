import { useState } from 'react';
import type { AccountInfo } from '@/lib/auth-api';
import { updateAccountEmail } from '@/lib/auth-api';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Pause, Play, Star, Trash2, Pencil } from 'lucide-react';
import { usePrivacy } from '@/contexts/privacy-context';
import { cn } from '@/lib/utils';

interface AccountListProps {
  provider: string;
  accounts: AccountInfo[];
  onRemove: (id: string) => void;
  onPause: (id: string) => void;
  onResume: (id: string) => void;
  onSetDefault: (id: string) => void;
  onUpdate?: () => void;
}

const TIER_STYLES: Record<string, string> = {
  free: 'bg-muted text-muted-foreground',
  pro: 'bg-blue-500/10 text-blue-500',
  ultra: 'bg-purple-500/10 text-purple-500',
  unknown: 'bg-muted text-muted-foreground',
};

function blurEmail(email: string | undefined): string {
  if (!email) return '--';
  return email.replace(/^(.{2})(.*)(@.*)$/, (_, a, b, c) => a + '*'.repeat(b.length) + c);
}

export function AccountList({
  provider,
  accounts,
  onPause,
  onResume,
  onSetDefault,
  onRemove,
  onUpdate,
}: AccountListProps) {
  const { privacyMode } = usePrivacy();
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editEmail, setEditEmail] = useState('');

  const handleSaveEmail = async (id: string) => {
    if (!editEmail.includes('@')) return;
    await updateAccountEmail(provider, id, editEmail.trim());
    setEditingId(null);
    onUpdate?.();
  };

  if (accounts.length === 0) {
    return <p className="text-sm text-muted-foreground py-2">No accounts connected.</p>;
  }

  return (
    <div className="space-y-2">
      {accounts.map((acct) => (
        <div
          key={acct.id}
          className={cn(
            'flex items-center gap-3 rounded-lg border px-3 py-2 transition-colors',
            acct.paused ? 'opacity-60' : 'hover:bg-muted/30',
          )}
        >
          {/* Status dot */}
          <span
            className={cn(
              'h-2 w-2 rounded-full shrink-0',
              acct.paused ? 'bg-amber-500' : 'bg-green-500',
            )}
          />

          {/* Email */}
          {editingId === acct.id ? (
            <div className="flex gap-1.5 flex-1 min-w-0">
              <Input
                type="email"
                placeholder="you@example.com"
                value={editEmail}
                onChange={(e) => setEditEmail(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && handleSaveEmail(acct.id)}
                className="h-6 text-xs"
                autoFocus
              />
              <Button size="sm" variant="ghost" className="h-6 px-2 text-xs"
                disabled={!editEmail.includes('@')}
                onClick={() => handleSaveEmail(acct.id)}>
                Save
              </Button>
              <Button size="sm" variant="ghost" className="h-6 px-2 text-xs"
                onClick={() => setEditingId(null)}>
                X
              </Button>
            </div>
          ) : (
            <span className="text-sm font-mono truncate flex-1 min-w-0">
              {privacyMode ? blurEmail(acct.email) : (acct.email ?? acct.id)}
            </span>
          )}

          {/* Edit email */}
          {editingId !== acct.id && (
            <button
              onClick={() => { setEditingId(acct.id); setEditEmail(acct.email ?? ''); }}
              className="shrink-0 p-1 rounded hover:bg-muted transition-colors text-muted-foreground/30 hover:text-muted-foreground"
              title="Edit email"
            >
              <Pencil className="h-3 w-3" />
            </button>
          )}

          {/* Tier badge */}
          {acct.tier && (
            <Badge className={cn('text-[10px] px-1.5', TIER_STYLES[acct.tier] ?? TIER_STYLES.unknown)}>
              {acct.tier.charAt(0).toUpperCase() + acct.tier.slice(1)}
            </Badge>
          )}

          {/* Default star */}
          <button
            onClick={() => onSetDefault(acct.id)}
            className={cn(
              'shrink-0 p-1 rounded hover:bg-muted transition-colors',
              acct.isDefault ? 'text-amber-500' : 'text-muted-foreground/30 hover:text-muted-foreground',
            )}
            title={acct.isDefault ? 'Default account' : 'Set as default'}
          >
            <Star className="h-3.5 w-3.5" fill={acct.isDefault ? 'currentColor' : 'none'} />
          </button>

          {/* Actions */}
          <div className="flex items-center gap-1 shrink-0">
            {acct.paused ? (
              <Button variant="ghost" size="icon" className="h-7 w-7" onClick={() => onResume(acct.id)} title="Resume">
                <Play className="h-3.5 w-3.5" />
              </Button>
            ) : (
              <Button variant="ghost" size="icon" className="h-7 w-7" onClick={() => onPause(acct.id)} title="Pause">
                <Pause className="h-3.5 w-3.5" />
              </Button>
            )}
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7 text-destructive hover:text-destructive"
              onClick={() => onRemove(acct.id)}
              title="Remove"
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>
      ))}
    </div>
  );
}
