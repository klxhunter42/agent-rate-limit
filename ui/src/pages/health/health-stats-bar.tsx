interface Segment {
  label: string;
  value: number;
  color: string;
}

export function HealthStatsBar({ segments, total }: { segments: Segment[]; total: number }) {
  const colorMap: Record<string, string> = {
    passed: 'bg-green-500',
    warning: 'bg-yellow-500',
    error: 'bg-red-500',
    info: 'bg-blue-400',
  };

  return (
    <div className="space-y-2">
      <div className="flex h-3 rounded-full overflow-hidden bg-muted">
        {segments.map((s) => {
          if (total === 0) return null;
          const pct = (s.value / total) * 100;
          return (
            <div
              key={s.label}
              className={`${colorMap[s.color] || 'bg-muted-foreground'}`}
              style={{ width: `${pct}%`, minWidth: s.value > 0 ? 4 : 0 }}
            />
          );
        })}
      </div>
      <div className="flex gap-4 text-xs">
        {segments.map((s) => (
          <span key={s.label} className="flex items-center gap-1">
            <span className={`h-2 w-2 rounded-full ${colorMap[s.color] || 'bg-muted-foreground'}`} />
            <span className="text-muted-foreground">{s.label}</span>
            <span className="font-medium">{s.value}</span>
          </span>
        ))}
      </div>
    </div>
  );
}
