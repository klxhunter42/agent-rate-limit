export interface HealthCheck {
  id: string;
  name: string;
  status: 'ok' | 'warning' | 'error' | 'info';
  message: string;
}

export interface HealthCheckGroup {
  id: string;
  name: string;
  checks: HealthCheck[];
}

export function deriveHealthChecks(data: {
  health: { status: string; queue_depth: number; uptime_seconds: number } | null;
  models: { name: string; in_flight: number; limit: number; total_requests: number; total_429s: number }[];
  keyPool: { total_keys: number; keys: { cooldown_until: number; in_cooldown: boolean; rpm: number; rpm_used: number; rpm_limit: number }[] } | null;
  metrics: Record<string, { value: number } | { sum: number; count: number }>;
}): HealthCheckGroup[] {
  const { health, models, keyPool } = data;
  const get = (name: string) => data.metrics[name];

  const groups: HealthCheckGroup[] = [];

  // Gateway group
  const gatewayChecks: HealthCheck[] = [];
  gatewayChecks.push({
    id: 'gateway-status',
    name: 'Gateway Status',
    status: health?.status === 'healthy' ? 'ok' : 'error',
    message: health?.status === 'healthy' ? 'Gateway is healthy' : 'Gateway is unhealthy',
  });
  const uptime = health?.uptime_seconds ?? 0;
  if (uptime > 0) {
    const h = Math.floor(uptime / 3600);
    const m = Math.floor((uptime % 3600) / 60);
    gatewayChecks.push({
      id: 'gateway-uptime',
      name: 'Uptime',
      status: 'info',
      message: h > 0 ? `${h}h ${m}m` : `${m}m`,
    });
  }
  if (gatewayChecks.length) groups.push({ id: 'gateway', name: 'Gateway', checks: gatewayChecks });

  // Queue group
  const queueChecks: HealthCheck[] = [];
  const qd = health?.queue_depth ?? 0;
  const gl = models.length > 0 ? models.reduce((s, m) => s + m.limit, 0) : 100;
  const queueRatio = gl > 0 ? qd / gl : 0;
  queueChecks.push({
    id: 'queue-depth',
    name: 'Queue Depth',
    status: queueRatio > 0.8 ? 'error' : queueRatio > 0.5 ? 'warning' : 'ok',
    message: `${qd} pending requests`,
  });
  if (queueChecks.length) groups.push({ id: 'queue', name: 'Queue', checks: queueChecks });

  // Models group
  const modelChecks: HealthCheck[] = [];
  for (const m of models) {
    const ratio = m.limit > 0 ? m.in_flight / m.limit : 0;
    modelChecks.push({
      id: `model-${m.name}`,
      name: m.name,
      status: ratio > 0.9 ? 'error' : ratio > 0.7 ? 'warning' : 'ok',
      message: `${m.in_flight}/${m.limit} slots used`,
    });
  }
  if (models.length > 0) {
    const total429s = models.reduce((s, m) => s + m.total_429s, 0);
    const totalReqs = models.reduce((s, m) => s + m.total_requests, 0);
    const rate429 = totalReqs > 0 ? total429s / totalReqs : 0;
    modelChecks.push({
      id: 'model-429-rate',
      name: '429 Rate',
      status: rate429 > 0.2 ? 'error' : rate429 > 0.05 ? 'warning' : 'ok',
      message: `${total429s} 429s / ${totalReqs} requests (${(rate429 * 100).toFixed(1)}%)`,
    });
  }
  if (modelChecks.length) groups.push({ id: 'models', name: 'Models', checks: modelChecks });

  // Key Pool group
  const keyChecks: HealthCheck[] = [];
  const keys = keyPool?.keys ?? [];
  const now = Date.now() / 1000;
  const cooldownCount = keys.filter((k) => k.in_cooldown || k.cooldown_until > now).length;
  if (keyPool) {
    keyChecks.push({
      id: 'key-availability',
      name: 'Key Availability',
      status: keyPool.total_keys === 0 ? 'info' : cooldownCount === keyPool.total_keys ? 'error' : cooldownCount > 0 ? 'warning' : 'ok',
      message: keyPool.total_keys === 0
        ? 'Passthrough mode (no keys)'
        : `${keyPool.total_keys - cooldownCount}/${keyPool.total_keys} active`,
    });
  }
  if (keyChecks.length) groups.push({ id: 'keys', name: 'Key Pool', checks: keyChecks });

  // Infrastructure group
  const infraChecks: HealthCheck[] = [];
  const err429 = (get('api_gateway_upstream_429_total') as { value: number } | undefined)?.value ?? 0;
  const retries = (get('api_gateway_upstream_retries_total') as { value: number } | undefined)?.value ?? 0;
  const connections = (get('api_gateway_active_connections') as { value: number } | undefined)?.value ?? 0;

  infraChecks.push({
    id: 'infra-upstream-429',
    name: 'Upstream 429s',
    status: err429 > 100 ? 'error' : err429 > 10 ? 'warning' : err429 > 0 ? 'info' : 'ok',
    message: `${err429} total`,
  });
  infraChecks.push({
    id: 'infra-retries',
    name: 'Upstream Retries',
    status: retries > 50 ? 'error' : retries > 10 ? 'warning' : retries > 0 ? 'info' : 'ok',
    message: `${retries} total`,
  });
  infraChecks.push({
    id: 'infra-connections',
    name: 'Active Connections',
    status: connections > 0 ? 'ok' : 'info',
    message: `${connections} active`,
  });
  groups.push({ id: 'infra', name: 'Infrastructure', checks: infraChecks });

  return groups;
}

export function computeHealthSummary(groups: HealthCheckGroup[]): {
  percentage: number;
  status: 'ok' | 'warning' | 'error';
  total: number;
  passed: number;
  warnings: number;
  errors: number;
  info: number;
} {
  let passed = 0, warnings = 0, errors = 0, info = 0;
  for (const g of groups) {
    for (const c of g.checks) {
      if (c.status === 'ok') passed++;
      else if (c.status === 'warning') warnings++;
      else if (c.status === 'error') errors++;
      else info++;
    }
  }
  const total = passed + warnings + errors + info;
  const okLike = passed + info;
  const percentage = total > 0 ? Math.round((okLike / total) * 100) : 100;
  const status: 'ok' | 'warning' | 'error' = errors > 0 ? 'error' : warnings > 0 ? 'warning' : 'ok';
  return { percentage, status, total, passed, warnings, errors, info };
}
