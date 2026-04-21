import type { ModelStatus } from '@/lib/api';
import { cn } from '@/lib/utils';

const MODEL_COLORS: Record<string, string> = {
  'gpt-4': '#10a37f',
  'gpt-4o': '#10a37f',
  'gpt-3.5': '#10a37f',
  'claude': '#d4a27f',
  'claude-oauth': '#d4a27f',
  'claude-3': '#d4a27f',
  'claude-4': '#d4a27f',
  'gemini': '#4285f4',
  'glm': '#1a73e8',
  'deepseek': '#5b6ef7',
  'llama': '#7c3aed',
  'qwen': '#6366f1',
};

function getModelColor(name: string): string {
  const lower = name.toLowerCase();
  for (const [key, color] of Object.entries(MODEL_COLORS)) {
    if (lower.includes(key)) return color;
  }
  return '#6b7280';
}

export function ModelNode({
  model,
  isHovered,
  isCenter,
  onMouseEnter,
  onMouseLeave,
}: {
  model: ModelStatus;
  isHovered: boolean;
  isCenter?: boolean;
  onMouseEnter: () => void;
  onMouseLeave: () => void;
}) {
  const color = getModelColor(model.name);
  const usagePct = model.limit > 0 ? (model.in_flight / model.limit) * 100 : 0;
  const successRate = model.total_requests > 0
    ? Math.round(((model.total_requests - model.total_429s) / model.total_requests) * 100)
    : 100;

  return (
    <div
      onMouseEnter={onMouseEnter}
      onMouseLeave={onMouseLeave}
      className={cn(
        'p-3 rounded-xl border transition-all duration-300 cursor-default',
        'bg-card/50 dark:bg-zinc-900/60 backdrop-blur-sm',
        isCenter
          ? 'w-40'
          : 'w-44',
        isHovered ? 'ring-1 scale-[1.02] shadow-lg' : 'hover:bg-card/80'
      )}
      style={{ borderColor: isHovered ? color : undefined, '--tw-ring-color': color } as React.CSSProperties}
    >
      <div className="flex items-center gap-2 mb-2">
        <div className="w-3 h-3 rounded-full shrink-0" style={{ backgroundColor: color }} />
        <span className="text-sm font-semibold truncate">{model.name}</span>
        {model.overridden && (
          <span className="text-[10px] bg-amber-500/20 text-amber-500 px-1 rounded">pinned</span>
        )}
      </div>

      <div className="grid grid-cols-2 gap-x-3 gap-y-1 text-xs mb-2">
        <div>
          <span className="text-muted-foreground">In-Flight</span>
          <span className="ml-1 font-mono">{model.in_flight}/{model.limit}</span>
        </div>
        <div>
          <span className="text-muted-foreground">429s</span>
          <span className={cn('ml-1 font-mono', model.total_429s > 0 && 'text-red-500')}>
            {model.total_429s}
          </span>
        </div>
      </div>

      <div className="w-full bg-muted dark:bg-zinc-800/50 h-1 rounded-full overflow-hidden mb-1">
        <div
          className="h-full rounded-full transition-all duration-500"
          style={{
            width: `${usagePct}%`,
            backgroundColor: usagePct > 80 ? '#ef4444' : usagePct > 60 ? '#f59e0b' : color,
          }}
        />
      </div>
      <div className="flex justify-between text-[10px] text-muted-foreground">
        <span>{usagePct.toFixed(0)}% capacity</span>
        <span className={successRate === 100 ? 'text-green-500' : successRate >= 95 ? 'text-yellow-500' : 'text-red-500'}>
          {successRate}% ok
        </span>
      </div>

      {model.series > 0 && (
        <div className="text-[10px] text-muted-foreground mt-1">
          {model.series} series | RTT {model.ewma_rtt_ms}ms
        </div>
      )}
    </div>
  );
}
