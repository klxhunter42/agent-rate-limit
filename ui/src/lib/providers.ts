export interface ProviderInfo {
  id: string;
  name: string;
  authType: 'api_key' | 'device_code' | 'auth_code' | 'session_cookie';
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
};

export function providerName(id: string): string {
  return FALLBACK_NAMES[id] ?? id;
}

const PROVIDER_ACCENT: Record<string, string> = {
  zai: '#6366f1',
  anthropic: '#d97706',
  'claude-oauth': '#d97706',
  openai: '#10b981',
  gemini: '#3b82f6',
  'gemini-oauth': '#3b82f6',
  copilot: '#6b7280',
  openrouter: '#8b5cf6',
  deepseek: '#06b6d4',
  kimi: '#f59e0b',
  huggingface: '#ec4899',
  ollama: '#78716c',
  qwen: '#ef4444',
};

export function providerColor(id: string): string {
  return PROVIDER_ACCENT[id] ?? '#6b7280';
}

// Shared color palette for charts.
export const CHART_COLORS = [
  '#6366f1', '#3b82f6', '#10b981', '#f59e0b',
  '#ef4444', '#8b5cf6', '#06b6d4', '#ec4899',
];

export const INPUT_TOKEN_COLOR = '#3b82f6';
export const OUTPUT_TOKEN_COLOR = '#f97316';

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
