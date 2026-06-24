export interface ProviderInfo {
  id: string;
  name: string;
  authType: 'api_key' | 'device_code' | 'auth_code' | 'session_cookie';
  upstreamBase?: string;
}

const AUTH_TYPE_LABELS: Record<string, string> = {
  api_key: 'API Key',
  device_code: 'Device Code',
  auth_code: 'OAuth',
  session_cookie: 'Session Cookie',
};

export function authTypeLabel(t: string): string {
  return AUTH_TYPE_LABELS[t] ?? t;
}

// Fallback provider names when /v1/providers is unavailable.
const FALLBACK_NAMES: Record<string, string> = {
  zai: 'Z.AI',
  anthropic: 'Anthropic',
  'claude-oauth': 'Claude (OAuth)',
  openai: 'OpenAI',
  gemini: 'Gemini',
  'gemini-oauth': 'Gemini (OAuth)',
  openrouter: 'OpenRouter',
  copilot: 'GitHub Copilot',
  deepseek: 'DeepSeek',
  kimi: 'Kimi',
  huggingface: 'HuggingFace',
  ollama: 'Ollama',
  agy: 'AGY',
  cursor: 'Cursor',
  codebuddy: 'CodeBuddy',
  kilo: 'Kilo',
  qwen: 'Qwen',
  lotuss: 'Lotuss',
};

export const UNAVAILABLE_PROVIDER_IDS = new Set([
 'anthropic', 'openai', 'gemini', 'gemini-oauth', 'openrouter', 'copilot',
 'deepseek', 'huggingface', 'ollama', 'agy', 'cursor', 'codebuddy', 'kilo', 'kimi',
]);

export function isProviderAvailable(id: string): boolean {
 return !UNAVAILABLE_PROVIDER_IDS.has(id);
}

export function providerName(id: string): string {
  if (FALLBACK_NAMES[id]) return FALLBACK_NAMES[id];
  if (cachedProviders) {
    const p = cachedProviders.find(x => x.id === id);
    if (p) return p.name;
  }
  return id;
}

const PROVIDER_ACCENT: Record<string, string> = {
  zai: '#00A651',
  anthropic: '#FFDD00',
  'claude-oauth': '#FFDD00',
  openai: '#2BA8A2',
  gemini: '#5DADE2',
  'gemini-oauth': '#5DADE2',
  copilot: '#9E9E9E',
  openrouter: '#EF6C4A',
  deepseek: '#FFD23F',
  kimi: '#EF6C4A',
  huggingface: '#00964A',
  ollama: '#757575',
  qwen: '#EF6C4A',
  lotuss: '#00A651',
};

export function providerColor(id: string): string {
  return PROVIDER_ACCENT[id] ?? '#6b7280';
}

// Shared color palette for charts.
export const CHART_COLORS = [
  '#00A651', '#FFDD00', '#2BA8A2', '#EF6C4A',
  '#FFD23F', '#5DADE2', '#00964A', '#007C3D',
];

export const INPUT_TOKEN_COLOR = '#2BA8A2';
export const OUTPUT_TOKEN_COLOR = '#FFDD00';

let cachedProviders: ProviderInfo[] | null = null;

export async function fetchProviders(): Promise<ProviderInfo[]> {
  if (cachedProviders) return cachedProviders;
  try {
    const res = await fetch('/v1/providers');
    if (res.ok) {
      const data = await res.json();
      cachedProviders = data;
      return data;
    }
  } catch {
    // fall through
  }
  return [];
}

export function clearProviderCache() {
  cachedProviders = null;
}

export async function updateProviderUpstream(providerId: string, upstream: string): Promise<void> {
  const res = await fetch(`/v1/providers/${providerId}/upstream`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ upstream }),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'update failed' }));
    throw new Error(err.error || 'update failed');
  }
  clearProviderCache();
}

export interface CustomProviderResult {
  id: string;
  name: string;
  status: string;
}

export async function registerCustomProvider(data: {
  name: string;
  format: string;
  upstream: string;
  apiKey?: string;
 models?: string[];
}): Promise<CustomProviderResult> {
  const res = await fetch('/v1/providers/custom', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'create failed' }));
    throw new Error(err.error || 'create failed');
  }
  clearProviderCache();
  return res.json();
}

export async function deleteCustomProvider(providerId: string): Promise<void> {
  const res = await fetch(`/v1/providers/custom/${providerId}`, {
    method: 'DELETE',
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'delete failed' }));
    throw new Error(err.error || 'delete failed');
  }
  clearProviderCache();
}

export async function updateCustomProvider(providerId: string, data: {
  name?: string;
  format?: string;
  upstream?: string;
  models?: string[];
}): Promise<CustomProviderResult> {
  const res = await fetch(`/v1/providers/custom/${providerId}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: 'update failed' }));
    throw new Error(err.error || 'update failed');
  }
  clearProviderCache();
  return res.json();
}
