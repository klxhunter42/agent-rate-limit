const SIZE_MAP = { sm: 80, md: 120, lg: 160 } as const;
const RADIUS_MAP = { sm: 30, md: 45, lg: 60 } as const;
const STROKE_MAP = { sm: 6, md: 8, lg: 10 } as const;
const COLOR_MAP = {
  ok: { stroke: '#22c55e', glow: 'rgba(34,197,94,0.4)' },
  warning: { stroke: '#eab308', glow: 'rgba(234,179,8,0.4)' },
  error: { stroke: '#ef4444', glow: 'rgba(239,68,68,0.4)' },
} as const;

export function HealthGauge({ percentage, status, size = 'md' }: {
  percentage: number;
  status: 'ok' | 'warning' | 'error';
  size?: 'sm' | 'md' | 'lg';
}) {
  const dim = SIZE_MAP[size];
  const r = RADIUS_MAP[size];
  const sw = STROKE_MAP[size];
  const circ = 2 * Math.PI * r;
  const offset = circ * (1 - percentage / 100);
  const colors = COLOR_MAP[status];
  const cx = dim / 2;
  const cy = dim / 2;

  const dotAngle = (percentage / 100) * 2 * Math.PI - Math.PI / 2;
  const dotX = cx + r * Math.cos(dotAngle);
  const dotY = cy + r * Math.sin(dotAngle);

  return (
    <svg width={dim} height={dim} viewBox={`0 0 ${dim} ${dim}`}>
      <defs>
        <filter id={`gauge-glow-${size}`} x="-50%" y="-50%" width="200%" height="200%">
          <feGaussianBlur stdDeviation="3" result="blur" />
          <feFlood floodColor={colors.glow} />
          <feComposite in2="blur" operator="in" />
          <feMerge>
            <feMergeNode />
            <feMergeNode in="SourceGraphic" />
          </feMerge>
        </filter>
      </defs>
      <circle cx={cx} cy={cy} r={r} fill="none" stroke="currentColor" className="text-muted-foreground/20" strokeWidth={sw} />
      <circle
        cx={cx} cy={cy} r={r} fill="none"
        stroke={colors.stroke}
        strokeWidth={sw}
        strokeLinecap="round"
        strokeDasharray={circ}
        strokeDashoffset={offset}
        transform={`rotate(-90 ${cx} ${cy})`}
        filter={`url(#gauge-glow-${size})`}
        style={{ transition: 'stroke-dashoffset 0.6s ease' }}
      />
      <circle cx={dotX} cy={dotY} r={sw} fill={colors.stroke} opacity={0.8}>
        <animate attributeName="r" values={`${sw};${sw + 2};${sw}`} dur="2s" repeatCount="indefinite" />
      </circle>
      <text x={cx} y={cy - 6} textAnchor="middle" dominantBaseline="middle" className="fill-foreground text-lg font-bold">
        {percentage}%
      </text>
      <text x={cx} y={cy + 14} textAnchor="middle" dominantBaseline="middle" className="fill-muted-foreground text-xs uppercase">
        Health
      </text>
    </svg>
  );
}
