import { useState, useEffect, useCallback, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import { usePrivacy } from '@/contexts/privacy-context';
import { cn } from '@/lib/utils';
import {
  LayoutDashboard,
  Activity,
  Gauge,
  Key,
  BarChart3,
  Settings2,
  Shield,
  Moon,
  Sun,
  RefreshCw,
  Copy,
  Link,
  Search,
  type LucideIcon,
} from 'lucide-react';

interface CommandItem {
  id: string;
  label: string;
  group: string;
  icon?: LucideIcon;
  shortcut?: string;
  action: () => void;
}

function useCommands(): CommandItem[] {
  const navigate = useNavigate();
  const { togglePrivacyMode, privacyMode } = usePrivacy();

  return [
    // Navigation
    { id: 'nav-overview', label: 'Go to Overview', group: 'Navigation', icon: LayoutDashboard, action: () => navigate('/') },
    { id: 'nav-health', label: 'Go to Health', group: 'Navigation', icon: Activity, shortcut: '2', action: () => navigate('/system-health') },
    { id: 'nav-models', label: 'Go to Model Limits', group: 'Navigation', icon: Gauge, shortcut: '3', action: () => navigate('/model-limits') },
    { id: 'nav-keys', label: 'Go to Key Pool', group: 'Navigation', icon: Key, shortcut: '4', action: () => navigate('/key-pool') },
    { id: 'nav-analytics', label: 'Go to Analytics', group: 'Navigation', icon: BarChart3, shortcut: '5', action: () => navigate('/analytics') },
    { id: 'nav-metrics', label: 'Go to Metrics', group: 'Navigation', icon: BarChart3, shortcut: '6', action: () => navigate('/metrics') },
    { id: 'nav-controls', label: 'Go to Controls', group: 'Navigation', icon: Settings2, shortcut: '7', action: () => navigate('/controls') },
    { id: 'nav-privacy', label: 'Go to Privacy', group: 'Navigation', icon: Shield, shortcut: '8', action: () => navigate('/privacy') },
    { id: 'nav-settings', label: 'Go to Settings', group: 'Navigation', icon: Settings2, shortcut: '9', action: () => navigate('/settings') },
    // Actions
    { id: 'act-privacy', label: 'Toggle Privacy Mode', group: 'Actions', icon: privacyMode ? Moon : Sun, shortcut: '\u2318P', action: togglePrivacyMode },
    { id: 'act-theme', label: 'Toggle Theme', group: 'Actions', icon: Moon, shortcut: '', action: () => window.dispatchEvent(new CustomEvent('arl:toggle-theme')) },
    { id: 'act-refresh', label: 'Refresh Data', group: 'Actions', icon: RefreshCw, shortcut: '\u2318R', action: () => window.location.reload() },
    // Quick
    { id: 'quick-limiter', label: 'Copy Limiter Status URL', group: 'Quick', icon: Link, action: () => navigator.clipboard.writeText(`${window.location.origin}/v1/limiter-status`) },
    { id: 'quick-health', label: 'Copy Health URL', group: 'Quick', icon: Copy, action: () => navigator.clipboard.writeText(`${window.location.origin}/health`) },
  ];
}

export function CommandPalette({ open, onClose }: { open: boolean; onClose: () => void }) {
  const commands = useCommands();
  const [query, setQuery] = useState('');
  const [activeIdx, setActiveIdx] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (open) {
      setQuery('');
      setActiveIdx(0);
      setTimeout(() => inputRef.current?.focus(), 0);
    }
  }, [open]);

  const filtered = commands.filter((c) =>
    c.label.toLowerCase().includes(query.toLowerCase())
  );

  useEffect(() => {
    setActiveIdx(0);
  }, [query]);

  const execute = useCallback(
    (item: CommandItem) => {
      item.action();
      onClose();
    },
    [onClose]
  );

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setActiveIdx((i) => (i + 1) % filtered.length);
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setActiveIdx((i) => (i - 1 + filtered.length) % filtered.length);
    } else if (e.key === 'Enter' && filtered[activeIdx]) {
      e.preventDefault();
      execute(filtered[activeIdx]);
    } else if (e.key === 'Escape') {
      e.preventDefault();
      onClose();
    }
  }

  useEffect(() => {
    if (!listRef.current) return;
    const active = listRef.current.querySelector('[data-active="true"]');
    active?.scrollIntoView({ block: 'nearest' });
  }, [activeIdx]);

  if (!open) return null;

  const groups = filtered.reduce<Record<string, CommandItem[]>>((acc, item) => {
    (acc[item.group] ??= []).push(item);
    return acc;
  }, {});

  let flatIdx = 0;

  return (
    <div className="fixed inset-0 z-[100] flex items-start justify-center pt-[15vh]">
      <div className="fixed inset-0 bg-black/50 backdrop-blur-sm" onClick={onClose} />
      <div className="relative z-10 w-full max-w-lg rounded-xl border border-border bg-popover shadow-2xl overflow-hidden">
        <div className="flex items-center border-b border-border px-3">
          <Search className="h-4 w-4 shrink-0 text-muted-foreground" />
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Type a command..."
            className="flex h-11 w-full bg-transparent px-3 text-sm outline-none placeholder:text-muted-foreground"
          />
          <kbd className="hidden sm:inline-flex h-5 items-center gap-1 rounded border border-border bg-muted px-1.5 font-mono text-[10px] text-muted-foreground">
            ESC
          </kbd>
        </div>
        <div ref={listRef} className="max-h-80 overflow-y-auto p-2">
          {filtered.length === 0 && (
            <p className="py-6 text-center text-sm text-muted-foreground">No results found.</p>
          )}
          {Object.entries(groups).map(([group, items]) => (
            <div key={group}>
              <p className="px-2 py-1.5 text-xs font-medium text-muted-foreground">{group}</p>
              {items.map((item) => {
                const idx = flatIdx++;
                const Icon = item.icon;
                return (
                  <button
                    key={item.id}
                    data-active={idx === activeIdx}
                    className={cn(
                      'flex w-full items-center gap-3 rounded-md px-2 py-1.5 text-sm transition-colors',
                      idx === activeIdx
                        ? 'bg-accent text-accent-foreground'
                        : 'text-foreground hover:bg-accent/50'
                    )}
                    onClick={() => execute(item)}
                    onMouseEnter={() => setActiveIdx(idx)}
                  >
                    {Icon && <Icon className="h-4 w-4 shrink-0 text-muted-foreground" />}
                    <span className="flex-1 text-left">{item.label}</span>
                    {item.shortcut && (
                      <kbd className="hidden sm:inline-flex h-5 items-center gap-1 rounded border border-border bg-muted px-1.5 font-mono text-[10px] text-muted-foreground">
                        {item.shortcut}
                      </kbd>
                    )}
                  </button>
                );
              })}
            </div>
          ))}
        </div>
        <div className="border-t border-border px-3 py-2 flex items-center gap-4 text-[11px] text-muted-foreground">
          <span className="flex items-center gap-1">
            <kbd className="rounded border border-border bg-muted px-1 font-mono">\u2191\u2193</kbd> navigate
          </span>
          <span className="flex items-center gap-1">
            <kbd className="rounded border border-border bg-muted px-1 font-mono">\u21B5</kbd> select
          </span>
          <span className="flex items-center gap-1">
            <kbd className="rounded border border-border bg-muted px-1 font-mono">esc</kbd> close
          </span>
        </div>
      </div>
    </div>
  );
}

export function useCommandPalette() {
  const [open, setOpen] = useState(false);

  useEffect(() => {
    const openHandler = () => setOpen(true);
    const closeHandler = () => setOpen(false);
    window.addEventListener('arl:toggle-palette', openHandler);
    window.addEventListener('arl:escape', closeHandler);
    return () => {
      window.removeEventListener('arl:toggle-palette', openHandler);
      window.removeEventListener('arl:escape', closeHandler);
    };
  }, []);

  return {
    open,
    toggle: () => setOpen((o) => !o),
    close: () => setOpen(false),
  };
}
