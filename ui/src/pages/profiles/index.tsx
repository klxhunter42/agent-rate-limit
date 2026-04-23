import { useState, useEffect, useCallback } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { Search, Plus, Copy, Download, Upload, Trash2, Edit2, Check, X, Info, Terminal, ChevronDown, ChevronUp, Key, Eye, EyeOff, Activity, Loader2 } from 'lucide-react';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/ui/dialog';
import { fetchProviders, providerName } from '@/lib/providers';
import type { ProviderInfo } from '@/lib/providers';
import { listAccounts } from '@/lib/auth-api';
import type { AccountInfo } from '@/lib/auth-api';
import { fetchProfileUsage } from '@/lib/api';
import type { ProfileUsage } from '@/lib/api';
import { InfoTip } from '@/components/shared/info-tip';

interface Profile {
  name: string;
  provider: string;
  model?: string;
  target?: string;
  accountIds?: string[];
  maxTokens?: number;
  temperature?: number;
  createdAt?: string;
  updatedAt?: string;
  apiKey?: string;
  [key: string]: unknown;
}

export function ProfilesPage() {
  const [profiles, setProfiles] = useState<Profile[]>([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [editing, setEditing] = useState<string | null>(null);
  const [importText, setImportText] = useState('');
  const [showImport, setShowImport] = useState(false);
  const [showGuide, setShowGuide] = useState(false);

  const fetchProfiles = useCallback(() => {
    fetch('/v1/profiles')
      .then((r) => (r.ok ? r.json() : []))
      .then((data) => {
        const list = Array.isArray(data) ? data : data.profiles ?? [];
        setProfiles(list);
      })
      .catch(() => setProfiles([]))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    fetchProfiles();
  }, [fetchProfiles]);

  const filtered = profiles.filter(
    (p) =>
      p.name.toLowerCase().includes(search.toLowerCase()) ||
      (p.provider ?? '').toLowerCase().includes(search.toLowerCase())
  );

  async function createProfile(data: {
    name: string;
    target: string;
    accountIds: string[];
  }) {
    const res = await fetch('/v1/profiles', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...data, provider: data.target }),
    });
    if (res.ok) {
      setShowCreate(false);
      fetchProfiles();
    }
  }

  async function deleteProfile(name: string) {
    const res = await fetch(`/v1/profiles/${encodeURIComponent(name)}`, { method: 'DELETE' });
    if (res.ok) fetchProfiles();
  }

  async function copyProfile(name: string) {
    const dest = prompt(`Copy "${name}" to:`);
    if (!dest) return;
    const res = await fetch(`/v1/profiles/${encodeURIComponent(name)}/copy`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ destination: dest }),
    });
    if (res.ok) fetchProfiles();
  }

  async function exportProfile(name: string) {
    const res = await fetch(`/v1/profiles/${encodeURIComponent(name)}/export`);
    if (res.ok) {
      const blob = await res.json();
      const json = JSON.stringify(blob, null, 2);
      const url = URL.createObjectURL(new Blob([json], { type: 'application/json' }));
      const a = document.createElement('a');
      a.href = url;
      a.download = `profile-${name}.json`;
      a.click();
      URL.revokeObjectURL(url);
    }
  }

  async function importProfiles() {
    if (!importText.trim()) return;
    const res = await fetch('/v1/profiles/import', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: importText,
    });
    if (res.ok) {
      setShowImport(false);
      setImportText('');
      fetchProfiles();
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold flex items-center gap-1.5">Profiles <InfoTip text="Profiles group API keys, account pools, and routing rules. Each profile gets its own token for authentication." /></h1>
        <div className="flex gap-2">
          <Button size="sm" variant="outline" onClick={() => setShowGuide(!showGuide)}>
            <Info className="h-4 w-4 mr-1" /> Setup Guide
          </Button>
          <Button size="sm" variant="outline" onClick={() => setShowImport(!showImport)}>
            <Upload className="h-4 w-4 mr-1" /> Import
          </Button>
          <Button size="sm" onClick={() => setShowCreate(!showCreate)}>
            <Plus className="h-4 w-4 mr-1" /> New
          </Button>
        </div>
      </div>

      {showGuide && <SetupGuideCard />}

      {showImport && (
        <Card>
          <CardHeader><CardTitle className="text-base">Import Profiles</CardTitle></CardHeader>
          <CardContent className="space-y-3">
            <textarea
              className="w-full h-32 rounded-md border bg-background p-3 text-sm font-mono"
              placeholder="Paste profile JSON bundle..."
              value={importText}
              onChange={(e) => setImportText(e.target.value)}
            />
            <Button size="sm" onClick={importProfiles}>Import</Button>
          </CardContent>
        </Card>
      )}

      {showCreate && (
        <Card>
          <CardHeader><CardTitle className="text-base">Create Profile</CardTitle></CardHeader>
          <CardContent>
            <CreateProfileForm onSubmit={createProfile} onCancel={() => setShowCreate(false)} />
          </CardContent>
        </Card>
      )}

      <div className="relative">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
        <Input
          placeholder="Search profiles..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="pl-9"
        />
      </div>

      {loading ? (
        <div className="text-center py-8 text-muted-foreground text-sm">Loading profiles...</div>
      ) : filtered.length === 0 ? (
        <div className="text-center py-8 text-muted-foreground text-sm">
          {search ? 'No profiles match your search' : 'No profiles yet. Create one or check the Setup Guide.'}
        </div>
      ) : (
        <div className="grid gap-3">
          {filtered.map((p) => (
            <ProfileCard
              key={p.name}
              profile={p}
              editing={editing === p.name}
              onEdit={() => setEditing(p.name)}
              onCancelEdit={() => setEditing(null)}
              onSave={async (name, data) => {
                const newName = (data.name as string) ?? name;
                const body = { ...data };
                if (newName !== name) {
                  // Rename: create new + delete old
                  const res = await fetch('/v1/profiles', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ ...body, name: newName }),
                  });
                  if (res.ok) {
                    await fetch(`/v1/profiles/${encodeURIComponent(name)}`, { method: 'DELETE' });
                    setEditing(null);
                    fetchProfiles();
                  }
                } else {
                  const res = await fetch(`/v1/profiles/${encodeURIComponent(name)}`, {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(body),
                  });
                  if (res.ok) { setEditing(null); fetchProfiles(); }
                }
              }}
              onDelete={() => deleteProfile(p.name)}
              onCopy={() => copyProfile(p.name)}
              onExport={() => exportProfile(p.name)}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function SetupGuideCard() {
  const [open, setOpen] = useState<string | null>('usage');

  function Section({ id, title, children }: { id: string; title: string; children: React.ReactNode }) {
    const isOpen = open === id;
    return (
      <div className="border rounded-md">
        <button
          className="w-full flex items-center justify-between px-4 py-3 text-sm font-medium hover:bg-muted/50"
          onClick={() => setOpen(isOpen ? null : id)}
        >
          {title}
          {isOpen ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
        </button>
        {isOpen && <div className="px-4 pb-4 text-sm text-muted-foreground space-y-2">{children}</div>}
      </div>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base flex items-center gap-2">
          <Terminal className="h-4 w-4" /> Profile Setup Guide
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-2">
        <Section id="usage" title="How to Use Profiles">
          <p>Profiles let you route requests through specific provider configurations. Send the <code className="bg-muted px-1 rounded text-xs">X-Profile</code> header with your request:</p>
          <pre className="bg-muted p-3 rounded-md text-xs overflow-x-auto">
{`# With curl
curl http://localhost:8080/v1/chat/completions \\
  -H "X-Profile: my-profile" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"claude-sonnet-4-20250514","messages":[...]}'`}
          </pre>
          <p>The proxy looks up the profile and uses its <strong>target</strong>, <strong>baseUrl</strong>, <strong>apiKey</strong>, <strong>model</strong>, and <strong>accountIds</strong> to route the request. The profile's <strong>target</strong> determines which provider handles the request.</p>
        </Section>

        <Section id="claude-oauth" title="Claude OAuth Profile">
          <p>Routes through Anthropic API using Claude OAuth Bearer token. Requires OAuth account with <code className="bg-muted px-1 rounded text-xs">user:inference</code> scope.</p>
          <p className="mt-2"><strong>Setup:</strong></p>
          <ol className="list-decimal list-inside space-y-1 ml-2">
            <li>Create profile with target <code className="bg-muted px-1 rounded text-xs">claude-oauth</code></li>
            <li>Authenticate via Providers page to get OAuth token</li>
            <li>Select which accounts to include in Account Pool</li>
            <li>Click <strong>Generate</strong> on the profile card to create an API key</li>
          </ol>
          <pre className="bg-muted p-3 rounded-md text-xs overflow-x-auto mt-2">
{`# ~/.claude/settings.json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:8080",
    "ANTHROPIC_AUTH_TOKEN": "arl_your-generated-token"
  }
}`}
          </pre>
          <p className="text-xs text-muted-foreground mt-1">
            The <code className="bg-muted px-1 rounded text-xs">arl_</code> token identifies the profile. No <code className="bg-muted px-1 rounded text-xs">X-Profile</code> header needed. Model: any <code className="bg-muted px-1 rounded">claude-*</code> model.
          </p>
        </Section>

        <Section id="gemini-oauth" title="Gemini OAuth Profile">
          <p>Routes through Google Gemini CodeAssist API using Google OAuth token. Gateway auto-translates Anthropic format to Gemini format, so Claude Code works seamlessly.</p>
          <p className="mt-2"><strong>Setup:</strong></p>
          <ol className="list-decimal list-inside space-y-1 ml-2">
            <li>Create profile with target <code className="bg-muted px-1 rounded text-xs">gemini-oauth</code></li>
            <li>Authenticate via Providers page to get Google OAuth token</li>
            <li>Select which accounts to include in Account Pool</li>
          </ol>
          <pre className="bg-muted p-3 rounded-md text-xs overflow-x-auto mt-2">
{`# ~/.claude/settings.json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:8080",
    "ANTHROPIC_AUTH_TOKEN": "arl_your-generated-token"
  }
}`}
          </pre>
          <p className="text-xs text-muted-foreground mt-1">
            The <code className="bg-muted px-1 rounded text-xs">arl_</code> token identifies the profile. No <code className="bg-muted px-1 rounded text-xs">X-Profile</code> header needed. Model: <code className="bg-muted px-1 rounded">claude-*</code> or <code className="bg-muted px-1 rounded">gemini-*</code>.
          </p>
        </Section>

        <Section id="zai-mode" title="Z.AI / GLM Mode (Default)">
          <p>When <code className="bg-muted px-1 rounded text-xs">GLM_MODE=true</code>, the default routing sends all requests to Z.AI API. No profile needed.</p>
          <pre className="bg-muted p-3 rounded-md text-xs overflow-x-auto mt-2">
{`# ~/.claude/settings.json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:8080",
    "ANTHROPIC_AUTH_TOKEN": "arl_your-generated-token"
  }
}`}
          </pre>
          <p className="text-xs text-muted-foreground mt-1">
            Model: <code className="bg-muted px-1 rounded">glm-*</code>. Adaptive limiter distributes across same-series models.
          </p>
        </Section>

        <Section id="account-pool" title="Account Pool Selection">
          <p>When a profile has <strong>accountIds</strong> set, the proxy selects an account from only those IDs in the provider token pool. This is useful for:</p>
          <ul className="list-disc list-inside space-y-1">
            <li>Isolating specific OAuth accounts per profile</li>
            <li>Separating free-tier vs paid-tier usage</li>
            <li>Rotating through a subset of available accounts</li>
          </ul>
          <p>Leave <strong>accountIds</strong> empty to use all available accounts for the provider.</p>
          <p className="text-xs text-muted-foreground mt-1">
            Deleting an account from Providers page automatically removes it from all profiles.
          </p>
        </Section>

        <Section id="api-key" title="API Key (Profile Token)">
          <p>Each profile can generate a unique <code className="bg-muted px-1 rounded text-xs">arl_</code> token. Use this as <code className="bg-muted px-1 rounded text-xs">ANTHROPIC_AUTH_TOKEN</code> in Claude Code or any client. The gateway resolves the token to its profile automatically.</p>
          <ul className="list-disc list-inside space-y-1">
            <li>Click <strong>Generate</strong> on any profile card</li>
            <li>Copy the token and set it as <code className="bg-muted px-1 rounded text-xs">ANTHROPIC_AUTH_TOKEN</code></li>
            <li>No <code className="bg-muted px-1 rounded text-xs">X-Profile</code> header needed - the token identifies the profile</li>
            <li>Click <strong>Revoke</strong> to invalidate a token at any time</li>
          </ul>
          <pre className="bg-muted p-3 rounded-md text-xs overflow-x-auto mt-2">
{`# Example: generate and use
curl -X POST http://localhost:8080/v1/profiles/meow/token
# => {"token":"arl_abc123...","profile":"meow"}

# Then set in Claude Code settings:
ANTHROPIC_AUTH_TOKEN=arl_abc123...`}
          </pre>
        </Section>

        <Section id="docker-haiku" title="Claude Code Container (Haiku)">
          <p>Run [[PERSON_2]] Code in a Docker container, routed through a profile to use Haiku via Claude OAuth. No local install needed.</p>
          <p className="mt-2"><strong>Setup:</strong></p>
          <ol className="list-decimal list-inside space-y-1 ml-2">
            <li>Create a profile with model <code className="bg-muted px-1 rounded text-xs">claude-haiku-4-5-20251001</code> and target <code className="bg-muted px-1 rounded text-xs">claude-oauth</code></li>
            <li>Generate an <code className="bg-muted px-1 rounded text-xs">arl_</code> token for the profile</li>
            <li>Create <code className="bg-muted px-1 rounded text-xs">docker/settings-meow.json</code>:</li>
          </ol>
          <pre className="bg-muted p-3 rounded-md text-xs overflow-x-auto mt-2">
{`{
  "env": {
    "ANTHROPIC_BASE_URL": "http://arl-gateway:8080",
    "ANTHROPIC_API_KEY": "arl_your-generated-token"
  }
}`}
          </pre>
          <p className="text-xs text-muted-foreground mt-1">
            Works with both <code className="bg-muted px-1 rounded text-xs">ANTHROPIC_API_KEY</code> (x-api-key header) and <code className="bg-muted px-1 rounded text-xs">ANTHROPIC_AUTH_TOKEN</code> (Authorization: Bearer header). The gateway reads profile tokens from either.
          </p>
          <ol className="list-decimal list-inside space-y-1 ml-2" start={4}>
            <li>Start the container:</li>
          </ol>
          <pre className="bg-muted p-3 rounded-md text-xs overflow-x-auto mt-2">
{`docker compose run -d --name meow claude-code-meow`}
          </pre>
          <ol className="list-decimal list-inside space-y-1 ml-2" start={5}>
            <li>Use it:</li>
          </ol>
          <pre className="bg-muted p-3 rounded-md text-xs overflow-x-auto mt-2">
{`# One-shot prompt (no --bare needed)
docker exec meow claude -p "say hello"

# Interactive mode (--bare skips OAuth login)
docker exec -it meow claude --bare`}
          </pre>
          <p className="text-xs text-muted-foreground mt-1">
            <strong>--bare</strong> is required for interactive mode because Claude Code tries OAuth login first (<a href="https://github.com/anthropics/claude-code/issues/27900" className="underline" target="_blank" rel="noopener">known issue</a>). <code className="bg-muted px-1 rounded text-xs">-p</code> mode uses the API key directly. The gateway auto-strips unsupported parameters (effort, thinking) for Haiku.
          </p>
        </Section>

        <Section id="target" title="Target / Provider Types">
          <p>The <strong>target</strong> field determines the upstream API format:</p>
          <ul className="list-disc list-inside space-y-1">
            <li><code className="bg-muted px-1 rounded text-xs">claude-oauth</code> - Claude via OAuth (Bearer token + Anthropic API)</li>
            <li><code className="bg-muted px-1 rounded text-xs">gemini-oauth</code> - Gemini via OAuth (Bearer token + CodeAssist API)</li>
            <li><code className="bg-muted px-1 rounded text-xs">anthropic</code> - Anthropic API key format</li>
            <li><code className="bg-muted px-1 rounded text-xs">gemini</code> - Google Gemini API key format</li>
            <li><code className="bg-muted px-1 rounded text-xs">openai</code> - OpenAI API format</li>
          </ul>
        </Section>
      </CardContent>
    </Card>
  );
}

function CreateProfileForm({
  onSubmit,
  onCancel,
}: {
  onSubmit: (data: { name: string; target: string; accountIds: string[] }) => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState('');

  const [target, setTarget] = useState('');
  const [accountIds, setAccountIds] = useState<string[]>([]);
  const [providers, setProviders] = useState<ProviderInfo[]>([]);
  const [accounts, setAccounts] = useState<AccountInfo[]>([]);
  const [accountsLoading, setAccountsLoading] = useState(false);
  const canSubmit = name.trim() && target;

  useEffect(() => {
    fetchProviders().then((list) => {
      setProviders(list);
      if (list.length > 0 && !target) setTarget(list[0]!.id);
    });
  }, []);

  useEffect(() => {
    if (!target) { setAccounts([]); return; }
    setAccountsLoading(true);
    listAccounts(target)
      .then((list) => setAccounts(list))
      .catch(() => setAccounts([]))
      .finally(() => setAccountsLoading(false));
  }, [target]);

  function toggleAccount(id: string) {
    setAccountIds((prev) =>
      prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]
    );
  }

  return (
    <div className="space-y-3">
      <div className="grid grid-cols-2 gap-3">
        <div>
          <label className="text-xs text-muted-foreground">Name *</label>
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="my-profile" />
        </div>
        <div>
          <label className="text-xs text-muted-foreground">Target</label>
          <select
            className="w-full h-9 rounded-md border bg-background px-3 text-sm"
            value={target}
            onChange={(e) => { setTarget(e.target.value); setAccountIds([]); }}
          >
            {providers.length === 0 && <option value="">Loading...</option>}
            {providers.map((p) => (
              <option key={p.id} value={p.id}>{providerName(p.id)}</option>
            ))}
          </select>
        </div>
      </div>



      {accounts.length > 0 && (
        <div>
          <label className="text-xs text-muted-foreground">Account Pool (select accounts to use, leave empty for all)</label>
          <div className="mt-1 max-h-40 overflow-y-auto rounded-md border p-2 space-y-1">
            {accountsLoading && <div className="text-xs text-muted-foreground px-2">Loading accounts...</div>}
            {accounts.map((acc) => (
              <label key={acc.id} className="flex items-center gap-2 px-2 py-1 hover:bg-muted/50 rounded text-sm cursor-pointer">
                <input
                  type="checkbox"
                  checked={accountIds.includes(acc.id)}
                  onChange={() => toggleAccount(acc.id)}
                  className="rounded"
                />
                <span className="font-mono text-xs">{acc.id}</span>
                {acc.email && <span className="text-xs text-muted-foreground">({acc.email})</span>}
                {acc.isDefault && <Badge variant="secondary" className="text-[10px] h-4">default</Badge>}
                {acc.paused && <Badge variant="destructive" className="text-[10px] h-4">paused</Badge>}
              </label>
            ))}
          </div>
          {accountIds.length > 0 && (
            <p className="text-xs text-muted-foreground mt-1">{accountIds.length} account(s) selected</p>
          )}
        </div>
      )}

      <div className="flex gap-2 justify-end">
        <Button size="sm" variant="ghost" onClick={onCancel}>
          <X className="h-4 w-4 mr-1" /> Cancel
        </Button>
        <Button size="sm" onClick={() => onSubmit({ name, target, accountIds })} disabled={!canSubmit}>
          <Check className="h-4 w-4 mr-1" /> Create
        </Button>
      </div>
    </div>
  );
}

function ProfileCard({
  profile,
  editing,
  onEdit,
  onCancelEdit,
  onSave,
  onDelete,
  onCopy,
  onExport,
}: {
  profile: Profile;
  editing: boolean;
  onEdit: () => void;
  onCancelEdit: () => void;
  onSave: (name: string, data: Record<string, unknown>) => void;
  onDelete: () => void;
  onCopy: () => void;
  onExport: () => void;
}) {
  const [editName, setEditName] = useState(profile.name);
  const [editAccountIds, setEditAccountIds] = useState<string[]>((profile.accountIds ?? []).filter((id) => id));
  const [accounts, setAccounts] = useState<AccountInfo[]>([]);
  const [accountsLoading, setAccountsLoading] = useState(false);

  const resolvedProvider = profile.provider || profile.target || '';

  useEffect(() => {
    if (editing) {
      setEditName(profile.name);
      const raw = profile.accountIds;
      if (raw === undefined || raw === null) {
        // Legacy: never had accountIds set
      } else {
        setEditAccountIds(raw.filter((id) => id));
      }
    }
  }, [editing, profile.name, profile.accountIds]);

  useEffect(() => {
    if (editing && resolvedProvider) {
      setAccountsLoading(true);
      listAccounts(resolvedProvider)
        .then((list) => {
          setAccounts(list);
          const raw = profile.accountIds;
          if ((raw === undefined || raw === null) && list.length > 0) {
            setEditAccountIds(list.map((a) => a.id));
          }
        })
        .catch(() => setAccounts([]))
        .finally(() => setAccountsLoading(false));
    }
  }, [editing, resolvedProvider, profile.accountIds]);

  function toggleAccount(id: string) {
    setEditAccountIds((prev) =>
      prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]
    );
  }

  if (editing) {
    return (
      <Card>
        <CardContent className="pt-4 space-y-3">
          <div>
            <label className="text-xs text-muted-foreground">Profile Name</label>
            <Input value={editName} onChange={(e) => setEditName(e.target.value)} />
          </div>
          {accounts.length > 0 && (
            <div>
              <label className="text-xs text-muted-foreground">Account Pool</label>
              <div className="mt-1 max-h-40 overflow-y-auto rounded-md border p-2 space-y-1">
                {accountsLoading && <div className="text-xs text-muted-foreground px-2">Loading...</div>}
                {accounts.map((acc) => (
                  <label key={acc.id} className="flex items-center gap-2 px-2 py-1 hover:bg-muted/50 rounded text-sm cursor-pointer">
                    <input
                      type="checkbox"
                      checked={editAccountIds.includes(acc.id)}
                      onChange={() => toggleAccount(acc.id)}
                      className="rounded"
                    />
                    <span className="font-mono text-xs">{acc.id}</span>
                    {acc.email && <span className="text-xs text-muted-foreground">({acc.email})</span>}
                    {acc.isDefault && <Badge variant="secondary" className="text-[10px] h-4">default</Badge>}
                    {acc.paused && <Badge variant="destructive" className="text-[10px] h-4">paused</Badge>}
                  </label>
                ))}
              </div>
              {editAccountIds.length > 0 && (
                <p className="text-xs text-muted-foreground mt-1">{editAccountIds.length} account(s) selected</p>
              )}
            </div>
          )}
          <div className="flex gap-2 justify-end">
            <Button size="sm" variant="ghost" onClick={onCancelEdit}>
              <X className="h-4 w-4 mr-1" /> Cancel
            </Button>
            <Button size="sm" disabled={!editName.trim()} onClick={() => onSave(profile.name, { ...profile, name: editName.trim(), accountIds: editAccountIds })}>
              <Check className="h-4 w-4 mr-1" /> Save
            </Button>
          </div>
        </CardContent>
      </Card>
    );
  }

  return <ProfileCardView profile={profile} onEdit={onEdit} onDelete={onDelete} onCopy={onCopy} onExport={onExport} />;
}

interface TokenInfo {
  keyName: string;
  token: string;
  profile: string;
  expiresAt?: string;
  createdAt: string;
}

function ProfileCardView({
  profile,
  onEdit,
  onDelete,
  onCopy,
  onExport,
}: {
  profile: Profile;
  onEdit: () => void;
  onDelete: () => void;
  onCopy: () => void;
  onExport: () => void;
}) {
  const resolvedProvider = profile.provider || profile.target || '';
  const [tokens, setTokens] = useState<TokenInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [showNewKey, setShowNewKey] = useState(false);
  const [newKeyName, setNewKeyName] = useState('');
  const [newKeyExpiry, setNewKeyExpiry] = useState(0);
  const [generating, setGenerating] = useState(false);
  const [revealedToken, setRevealedToken] = useState<string | null>(null);
  const [revealedKeys, setRevealedKeys] = useState<Set<string>>(new Set());
  const [copiedKey, setCopiedKey] = useState<string | null>(null);
  const [accountMap, setAccountMap] = useState<Map<string, string>>(new Map());
  const [usage, setUsage] = useState<ProfileUsage | null>(null);
  const [revoking, setRevoking] = useState<string | null>(null);
  const [revokeConfirm, setRevokeConfirm] = useState<string | null>(null);

  useEffect(() => {
    if (!resolvedProvider || !profile.accountIds?.length) return;
    listAccounts(resolvedProvider)
      .then((accounts) => {
        const m = new Map<string, string>();
        for (const a of accounts) {
          m.set(a.id, a.email || a.id);
        }
        setAccountMap(m);
      })
      .catch(() => {});
  }, [resolvedProvider, profile.accountIds]);

  useEffect(() => {
    fetchProfileUsage(profile.name)
      .then((data) => setUsage(data as ProfileUsage))
      .catch(() => {});
  }, [profile.name]);

  const fetchTokens = useCallback(() => {
    fetch(`/v1/profiles/${encodeURIComponent(profile.name)}/tokens`)
      .then((r) => (r.ok ? r.json() : { tokens: [] }))
      .then((data) => setTokens(data.tokens ?? []))
      .catch(() => setTokens([]))
      .finally(() => setLoading(false));
  }, [profile.name]);

  useEffect(() => {
    fetchTokens();
  }, [fetchTokens]);

  async function generateKey() {
    if (!newKeyName.trim()) return;
    setGenerating(true);
    try {
      const body: Record<string, unknown> = { keyName: newKeyName.trim() };
      if (newKeyExpiry > 0) body.expiresIn = newKeyExpiry;
      const res = await fetch(`/v1/profiles/${encodeURIComponent(profile.name)}/tokens`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      if (res.ok) {
        const data = await res.json();
        setRevealedToken(data.token);
        setShowNewKey(false);
        setNewKeyName('');
        setNewKeyExpiry(0);
        fetchTokens();
      }
    } finally {
      setGenerating(false);
    }
  }

  async function revokeKey(keyName: string) {
    setRevoking(keyName);
    try {
      const res = await fetch(`/v1/profiles/${encodeURIComponent(profile.name)}/tokens/${encodeURIComponent(keyName)}`, {
        method: 'DELETE',
      });
      if (res.ok) {
        fetchTokens();
      } else {
        alert(`Failed to revoke: ${res.status} ${res.statusText}`);
      }
    } catch (e) {
      alert(`Failed to revoke: ${e instanceof Error ? e.message : 'network error'}`);
    } finally {
      setRevoking(null);
      setRevokeConfirm(null);
    }
  }

  function copyToken(token: string, keyName: string) {
    navigator.clipboard.writeText(token);
    setCopiedKey(keyName);
    setTimeout(() => setCopiedKey(null), 2000);
  }

  return (
    <Card>
      <CardContent className="pt-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <span className="font-mono text-sm font-semibold">{profile.name}</span>
            <Badge variant="outline">{providerName(resolvedProvider)}</Badge>
            {profile.model && <span className="text-xs text-muted-foreground">{profile.model}</span>}
            {profile.accountIds && profile.accountIds.length > 0 && (
              <Badge variant="secondary" className="text-[10px] h-4">
                {profile.accountIds.length} account{profile.accountIds.length > 1 ? 's' : ''}
              </Badge>
            )}
          </div>
          <div className="flex gap-1">
            <Button size="icon" variant="ghost" className="h-7 w-7" onClick={onEdit} title="Edit">
              <Edit2 className="h-3.5 w-3.5" />
            </Button>
            <Button size="icon" variant="ghost" className="h-7 w-7" onClick={onCopy} title="Copy">
              <Copy className="h-3.5 w-3.5" />
            </Button>
            <Button size="icon" variant="ghost" className="h-7 w-7" onClick={onExport} title="Export">
              <Download className="h-3.5 w-3.5" />
            </Button>
            <Button size="icon" variant="ghost" className="h-7 w-7 text-destructive" onClick={onDelete} title="Delete">
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>
        {profile.accountIds && profile.accountIds.length > 0 && (
          <div className="mt-2 flex flex-wrap gap-1">
            {profile.accountIds.map((id) => (
              <span key={id} className="text-[10px] font-mono bg-muted px-1.5 py-0.5 rounded">{accountMap.get(id) || id}</span>
            ))}
          </div>
        )}
        <div className="mt-3 border-t pt-3">
          <div className="flex items-center justify-between mb-2">
            <div className="flex items-center gap-2">
              <Key className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="text-xs font-medium">API Keys</span>
              {tokens.length > 0 && (
                <Badge variant="secondary" className="text-[10px] h-4">{tokens.length}</Badge>
              )}
            </div>
            <Button size="sm" variant="outline" className="h-6 text-xs" onClick={() => setShowNewKey(true)}>
              <Plus className="h-3 w-3 mr-1" /> New Key
            </Button>
          </div>

          {showNewKey && (
            <div className="mb-2 p-3 rounded-md border bg-muted/30 space-y-2">
              <div className="grid grid-cols-2 gap-2">
                <div>
                  <label className="text-[10px] text-muted-foreground">Key Name</label>
                  <Input
                    value={newKeyName}
                    onChange={(e) => setNewKeyName(e.target.value)}
                    placeholder="e.g. my-laptop, ci-pipeline"
                    className="h-7 text-xs"
                  />
                </div>
                <div>
                  <label className="text-[10px] text-muted-foreground">Expires</label>
                  <select
                    className="w-full h-7 rounded-md border bg-background px-2 text-xs"
                    value={newKeyExpiry}
                    onChange={(e) => setNewKeyExpiry(Number(e.target.value))}
                  >
                    <option value={0}>Never</option>
                    <option value={3600}>1 hour</option>
                    <option value={86400}>1 day</option>
                    <option value={604800}>7 days</option>
                    <option value={2592000}>30 days</option>
                    <option value={31536000}>1 year</option>
                  </select>
                </div>
              </div>
              <div className="flex gap-1 justify-end">
                <Button size="sm" variant="ghost" className="h-6 text-xs" onClick={() => { setShowNewKey(false); setNewKeyName(''); }}>
                  Cancel
                </Button>
                <Button size="sm" className="h-6 text-xs" onClick={generateKey} disabled={!newKeyName.trim() || generating}>
                  {generating ? 'Generating...' : 'Generate'}
                </Button>
              </div>
            </div>
          )}

          {revealedToken && (
            <div className="mb-2 p-2 rounded-md border border-green-500/30 bg-green-500/5 space-y-1">
              <div className="flex items-center justify-between">
                <span className="text-[10px] text-green-600 font-medium">New token generated - copy now:</span>
                <div className="flex gap-1">
                  <Button size="sm" variant="outline" className="h-5 text-[10px] gap-1" onClick={() => { copyToken(revealedToken, '__new__'); }}>
                    <Copy className="h-3 w-3" />
                    {copiedKey === '__new__' ? 'Copied!' : 'Copy'}
                  </Button>
                  <Button size="icon" variant="ghost" className="h-5 w-5" onClick={() => setRevealedToken(null)}>
                    <X className="h-3 w-3" />
                  </Button>
                </div>
              </div>
              <code className="text-xs font-mono break-all select-all">{revealedToken}</code>
            </div>
          )}

          {loading ? (
            <div className="text-xs text-muted-foreground py-1">Loading keys...</div>
          ) : tokens.length === 0 ? (
            <div className="text-xs text-muted-foreground py-1">No API keys. Click "New Key" to generate one.</div>
          ) : (
            <div className="space-y-1">
              {tokens.map((t) => {
                const isRevealed = revealedKeys.has(t.keyName);
                const displayToken = isRevealed ? t.token : (t.token.length > 8 ? t.token.slice(0, 8) + '****' : t.token);
                return (
                <div key={t.keyName} className="flex items-center gap-2 py-1 px-2 rounded hover:bg-muted/50 text-xs">
                  <span className="font-mono font-medium w-28 truncate" title={t.keyName}>{t.keyName}</span>
                  <code className="font-mono text-muted-foreground flex-1 truncate">{displayToken}</code>
                  {t.expiresAt && (
                    <span className="text-[10px] text-muted-foreground whitespace-nowrap">
                      exp: {new Date(t.expiresAt).toLocaleDateString()}
                    </span>
                  )}
                  <Button size="icon" variant="ghost" className="h-5 w-5 shrink-0" onClick={() => setRevealedKeys((s) => { const n = new Set(s); isRevealed ? n.delete(t.keyName) : n.add(t.keyName); return n; })} title={isRevealed ? 'Hide' : 'Reveal'}>
                    {isRevealed ? <EyeOff className="h-3 w-3" /> : <Eye className="h-3 w-3" />}
                  </Button>
                  <Button size="icon" variant="ghost" className="h-5 w-5 shrink-0" onClick={() => copyToken(t.token, t.keyName)} title="Copy">
                    {copiedKey === t.keyName ? <Check className="h-3 w-3 text-green-500" /> : <Copy className="h-3 w-3" />}
                  </Button>
                  <Button size="icon" variant="ghost" className="h-5 w-5 shrink-0" onClick={() => setRevokeConfirm(t.keyName)} title="Revoke">
                    {revoking === t.keyName ? <Loader2 className="h-3 w-3 animate-spin" /> : <Trash2 className="h-3 w-3 text-destructive" />}
                  </Button>
                </div>
              )})}
            </div>
          )}
        </div>
        {usage && usage.total_requests > 0 && (
          <div className="mt-3 border-t pt-3">
            <div className="flex items-center gap-2 mb-2">
              <Activity className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="text-xs font-medium">Usage</span>
            </div>
            <div className="grid grid-cols-4 gap-2 text-xs">
              <div>
                <div className="text-muted-foreground text-[10px]">Requests</div>
                <div className="font-mono">{usage.total_requests.toLocaleString()}</div>
              </div>
              <div>
                <div className="text-muted-foreground text-[10px]">Tokens In</div>
                <div className="font-mono">{usage.total_tokens_in > 1000 ? `${(usage.total_tokens_in / 1000).toFixed(1)}k` : usage.total_tokens_in}</div>
              </div>
              <div>
                <div className="text-muted-foreground text-[10px]">Tokens Out</div>
                <div className="font-mono">{usage.total_tokens_out > 1000 ? `${(usage.total_tokens_out / 1000).toFixed(1)}k` : usage.total_tokens_out}</div>
              </div>
              <div>
                <div className="text-muted-foreground text-[10px]">Cost</div>
                <div className="font-mono">${usage.total_cost.toFixed(4)}</div>
              </div>
            </div>
            {usage.models && usage.models.length > 0 && (
              <div className="mt-2 space-y-1">
                {usage.models.map((m) => (
                  <div key={m.model} className="flex items-center gap-2 text-[10px] text-muted-foreground">
                    <span className="font-mono">{m.model}</span>
                    <span>{m.requests}req</span>
                    <span>{(m.input_tokens / 1000).toFixed(1)}k in</span>
                    <span>{(m.output_tokens / 1000).toFixed(1)}k out</span>
                    <span>${m.cost.toFixed(4)}</span>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
      </CardContent>
      <Dialog open={revokeConfirm !== null} onOpenChange={(open) => { if (!open) setRevokeConfirm(null); }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Revoke API Key</DialogTitle>
            <DialogDescription>
              Revoke token <span className="font-mono font-medium">{revokeConfirm}</span>? This cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <div className="flex justify-end gap-2 mt-4">
            <Button size="sm" variant="ghost" onClick={() => setRevokeConfirm(null)}>Cancel</Button>
            <Button size="sm" variant="destructive" onClick={() => revokeConfirm && revokeKey(revokeConfirm)} disabled={revoking !== null}>
              {revoking ? <Loader2 className="h-4 w-4 animate-spin mr-1" /> : <Trash2 className="h-4 w-4 mr-1" />}
              Revoke
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </Card>
  );
}
