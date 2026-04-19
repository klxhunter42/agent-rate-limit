import { useState, useMemo, useEffect } from 'react';
import { useDashboard } from '@/contexts/dashboard-context';
import { LivePulse } from './live-pulse';
import { KeyNode } from './key-node';
import { ModelNode } from './model-node';
import { FlowPaths } from './flow-paths';
import { CheckCircle2, XCircle, Activity, Radio, Zap } from 'lucide-react';

export function KeyFlowMonitor() {
  const { models, keyPool, lastRefresh } = useDashboard();
  const [hoveredKey, setHoveredKey] = useState<number | null>(null);
  const [hoveredModel, setHoveredModel] = useState<number | null>(null);

  const keys = useMemo(() => keyPool?.keys ?? [], [keyPool]);
  const totalKeys = keyPool?.total_keys ?? 0;

  const totalSuccess = useMemo(() => keys.reduce((s, k) => s + k.success_count, 0), [keys]);
  const totalErrors = useMemo(() => keys.reduce((s, k) => s + k.error_count, 0), [keys]);
  const totalRequests = totalSuccess + totalErrors;
  const successRate = totalRequests > 0 ? Math.round((totalSuccess / totalRequests) * 100) : 100;
  const total429s = useMemo(() => models.reduce((s, m) => s + m.total_429s, 0), [models]);
  const totalInFlight = useMemo(() => models.reduce((s, m) => s + m.in_flight, 0), [models]);

  const timeSinceUpdate = useMemo(() => {
    if (!lastRefresh) return '';
    const diff = Math.floor((Date.now() - lastRefresh.getTime()) / 1000);
    if (diff < 60) return `${diff}s ago`;
    return `${Math.floor(diff / 60)}m ago`;
  }, [lastRefresh]);

  const [timeLabel, setTimeLabel] = useState('just now');

  useEffect(() => { setTimeLabel(timeSinceUpdate || 'just now'); }, [timeSinceUpdate]);

  const successColor = successRate === 100 ? '#10b981' : successRate >= 95 ? '#f59e0b' : '#ef4444';

  if (models.length === 0) return null;

  return (
    <div className="rounded-xl border border-border overflow-hidden font-mono text-[13px] text-foreground bg-card/50 dark:bg-zinc-900/60 backdrop-blur-sm">
      {/* Live Header */}
      <div className="flex items-center justify-between px-4 py-2.5 border-b border-border bg-gradient-to-r from-emerald-500/5 via-transparent to-transparent dark:from-emerald-500/10">
        <div className="flex items-center gap-2">
          <LivePulse />
          <span className="text-xs font-semibold tracking-tight">LIVE</span>
          <span className="text-[10px] text-muted-foreground">Key Flow Monitor</span>
        </div>
        <div className="flex items-center gap-4 text-[10px] text-muted-foreground">
          <div className="flex items-center gap-1.5">
            <Radio className="w-3 h-3 animate-pulse" />
            <span>{timeLabel || 'just now'}</span>
          </div>
          <span className="text-muted-foreground/50">|</span>
          <span>{totalKeys > 0 ? `${totalKeys} keys` : 'passthrough'}</span>
          <span>{models.length} models</span>
        </div>
      </div>

      {/* Summary Stats */}
      <div className="grid grid-cols-4 gap-3 p-4 border-b border-border bg-muted/20 dark:bg-zinc-900/30">
        <div className="flex items-center gap-3 p-3 rounded-lg bg-card/50 dark:bg-zinc-900/50 border border-border/50 dark:border-white/[0.06]">
          <div className="w-8 h-8 rounded-md flex items-center justify-center bg-blue-500/15 text-blue-500">
            <Zap className="w-4 h-4" />
          </div>
          <div>
            <div className="text-[10px] text-muted-foreground uppercase tracking-wider">In-Flight</div>
            <div className="text-lg font-semibold font-mono leading-tight">{totalInFlight}</div>
          </div>
        </div>
        <div className="flex items-center gap-3 p-3 rounded-lg bg-card/50 dark:bg-zinc-900/50 border border-border/50 dark:border-white/[0.06]">
          <div className="w-8 h-8 rounded-md flex items-center justify-center bg-emerald-500/15 text-emerald-500">
            <CheckCircle2 className="w-4 h-4" />
          </div>
          <div>
            <div className="text-[10px] text-muted-foreground uppercase tracking-wider">Success</div>
            <div className="text-lg font-semibold font-mono leading-tight">{totalSuccess.toLocaleString()}</div>
          </div>
        </div>
        <div className="flex items-center gap-3 p-3 rounded-lg bg-card/50 dark:bg-zinc-900/50 border border-border/50 dark:border-white/[0.06]">
          <div className="w-8 h-8 rounded-md flex items-center justify-center" style={{ backgroundColor: totalErrors > 0 ? '#ef444415' : 'var(--muted)', color: totalErrors > 0 ? '#ef4444' : 'var(--muted-foreground)' }}>
            <XCircle className="w-4 h-4" />
          </div>
          <div>
            <div className="text-[10px] text-muted-foreground uppercase tracking-wider">429s</div>
            <div className="text-lg font-semibold font-mono leading-tight">{total429s.toLocaleString()}</div>
          </div>
        </div>
        <div className="flex items-center gap-3 p-3 rounded-lg bg-card/50 dark:bg-zinc-900/50 border border-border/50 dark:border-white/[0.06]">
          <div className="w-8 h-8 rounded-md flex items-center justify-center" style={{ backgroundColor: `${successColor}15`, color: successColor }}>
            <Activity className="w-4 h-4" />
          </div>
          <div>
            <div className="text-[10px] text-muted-foreground uppercase tracking-wider">Success Rate</div>
            <div className="text-lg font-semibold font-mono leading-tight" style={{ color: successColor }}>
              {successRate}%
            </div>
          </div>
        </div>
      </div>

      {/* Flow Visualization */}
      <div className="relative min-h-[300px] p-6">
        <div className="text-[10px] text-muted-foreground uppercase tracking-widest mb-4">
          Request Distribution: Keys to Models
        </div>

        <FlowPaths
          keys={keys}
          models={models}
          hoveredKey={hoveredKey}
          hoveredModel={hoveredModel}
        />

        <div className="flex items-stretch gap-8">
          {/* Keys Column */}
            <div className="flex flex-col gap-3 w-48 shrink-0 z-10">
              {keys.length > 0 ? (
                keys.map((key, i) => (
                  <div key={i} data-key-idx={i}>
                    <KeyNode
                      entry={key}
                      index={i}
                      isHovered={hoveredKey === i}
                      onMouseEnter={() => setHoveredKey(i)}
                      onMouseLeave={() => setHoveredKey(null)}
                    />
                  </div>
                ))
              ) : (
                <div className="p-3 rounded-xl border border-dashed border-muted-foreground/30 bg-muted/10 flex flex-col items-center justify-center h-24 text-center">
                  <Activity className="w-5 h-5 text-muted-foreground/50 mb-1.5" />
                  <span className="text-xs text-muted-foreground">Passthrough</span>
                  <span className="text-[10px] text-muted-foreground/70">No upstream keys</span>
                </div>
              )}
            </div>

          {/* Center Gateway Node */}
          <div className="flex-1 flex items-center justify-center z-10">
            <div data-gateway className="w-24 h-24 rounded-full border-2 border-dashed border-muted-foreground/30 flex flex-col items-center justify-center bg-muted/10">
              <Zap className="w-6 h-6 text-muted-foreground mb-1" />
              <span className="text-[10px] text-muted-foreground">ARL</span>
              <span className="text-[10px] text-muted-foreground">Gateway</span>
            </div>
          </div>

          {/* Models Column */}
          <div className="flex flex-col gap-3 w-44 shrink-0 z-10">
            {models.map((model, i) => (
              <div key={model.name} data-model-idx={i}>
                <ModelNode
                  model={model}
                  isHovered={hoveredModel === i}
                  onMouseEnter={() => setHoveredModel(i)}
                  onMouseLeave={() => setHoveredModel(null)}
                />
              </div>
            ))}
          </div>
        </div>

        {keys.length === 0 && models.length === 0 && (
          <div className="absolute inset-0 flex items-center justify-center text-muted-foreground text-sm">
            Waiting for data...
          </div>
        )}
      </div>
    </div>
  );
}
