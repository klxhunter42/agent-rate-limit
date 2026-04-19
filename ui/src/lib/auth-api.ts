export interface AccountInfo {
  id: string;
  email?: string;
  provider: string;
  isDefault: boolean;
  tier?: 'free' | 'pro' | 'ultra' | 'unknown';
  paused?: boolean;
  createdAt: string;
}

export interface AuthStatus {
  provider: string;
  authenticated: boolean;
  accounts: AccountInfo[];
}

export interface DeviceCodeInfo {
  user_code: string;
  verification_url: string;
  device_code: string;
  expires_in: number;
}

export interface AuthURLInfo {
  auth_url: string;
  state: string;
}

export interface PollResult {
  status: 'pending' | 'complete';
  account?: AccountInfo;
}

export async function startDeviceAuth(provider: string): Promise<DeviceCodeInfo> {
  const res = await fetch(`/v1/auth/${provider}/start`, { method: 'POST' });
  if (!res.ok) throw new Error(`start device auth: ${res.status}`);
  return res.json();
}

export async function startAuthCode(provider: string): Promise<AuthURLInfo> {
  const res = await fetch(`/v1/auth/${provider}/start-url`, { method: 'POST' });
  if (!res.ok) throw new Error(`start auth code: ${res.status}`);
  return res.json();
}

export async function pollAuthStatus(
  provider: string,
  params: { state?: string; device_code?: string },
): Promise<PollResult> {
  const qs = new URLSearchParams();
  if (params.state) qs.set('state', params.state);
  if (params.device_code) qs.set('device_code', params.device_code);
  const res = await fetch(`/v1/auth/${provider}/status?${qs}`);
  if (!res.ok) throw new Error(`poll auth status: ${res.status}`);
  const data = await res.json();
  return {
    status: data.status,
    account: data.account ? mapAccount(data.account) : undefined,
  };
}

export async function cancelAuth(provider: string): Promise<void> {
  const res = await fetch(`/v1/auth/${provider}/cancel`, { method: 'POST' });
  if (!res.ok) throw new Error(`cancel auth: ${res.status}`);
}

export async function listAccounts(provider?: string): Promise<AccountInfo[]> {
  const url = provider ? `/v1/auth/accounts/${provider}` : '/v1/auth/accounts';
  const res = await fetch(url);
  if (!res.ok) throw new Error(`list accounts: ${res.status}`);
  const data = await res.json();
  const raw: any[] = data.accounts ?? data;
  return raw.map((a: any) => ({
    id: a.account_id ?? a.id,
    email: a.email,
    provider: a.provider,
    isDefault: a.is_default ?? false,
    tier: a.tier,
    paused: a.paused ?? false,
    createdAt: a.created_at ?? a.createdAt ?? '',
  }));
}

export async function removeAccount(provider: string, accountId: string): Promise<void> {
  const res = await fetch(`/v1/auth/accounts/${provider}/${accountId}`, { method: 'DELETE' });
  if (!res.ok) throw new Error(`remove account: ${res.status}`);
}

export async function pauseAccount(provider: string, accountId: string): Promise<void> {
  const res = await fetch(`/v1/auth/accounts/${provider}/${accountId}/pause`, { method: 'POST' });
  if (!res.ok) throw new Error(`pause account: ${res.status}`);
}

export async function resumeAccount(provider: string, accountId: string): Promise<void> {
  const res = await fetch(`/v1/auth/accounts/${provider}/${accountId}/resume`, { method: 'POST' });
  if (!res.ok) throw new Error(`resume account: ${res.status}`);
}

export async function setDefaultAccount(provider: string, accountId: string): Promise<void> {
  const res = await fetch(`/v1/auth/accounts/${provider}/${accountId}/default`, { method: 'POST' });
  if (!res.ok) throw new Error(`set default account: ${res.status}`);
}

export async function registerAPIKey(provider: string, apiKey: string): Promise<{ status: string; account: AccountInfo }> {
  const res = await fetch(`/v1/auth/${provider}/register`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ api_key: apiKey }),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new Error((err as { error?: string }).error || `register: ${res.status}`);
  }
  const data = await res.json();
  return { status: data.status, account: mapAccount(data.account) };
}

export async function registerSessionCookie(provider: string, cookie: string): Promise<{ status: string; account: AccountInfo }> {
  const res = await fetch(`/v1/auth/${provider}/register`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ session_cookie: cookie }),
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new Error((err as { error?: string }).error || `register: ${res.status}`);
  }
  const data = await res.json();
  return { status: data.status, account: mapAccount(data.account) };
}

function mapAccount(a: any): AccountInfo {
  return {
    id: a.account_id ?? a.id,
    email: a.email,
    provider: a.provider,
    isDefault: a.is_default ?? false,
    tier: a.tier,
    paused: a.paused ?? false,
    createdAt: a.created_at ?? a.createdAt ?? '',
  };
}

export async function login(apiKey: string): Promise<{ success: boolean }> {
  const res = await fetch('/v1/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ api_key: apiKey }),
  });
  if (!res.ok) throw new Error(`login: ${res.status}`);
  return res.json();
}

export async function logout(): Promise<void> {
  const res = await fetch('/v1/auth/logout', { method: 'POST' });
  if (!res.ok) throw new Error(`logout: ${res.status}`);
}

export async function checkAuth(): Promise<{ authenticated: boolean }> {
  const res = await fetch('/v1/auth/check');
  if (!res.ok) throw new Error(`check auth: ${res.status}`);
  return res.json();
}
