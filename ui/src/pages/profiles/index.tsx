import { useState, useEffect, useCallback } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { Search, Plus, Copy, Download, Upload, Trash2, Edit2, Check, X, Info, Terminal, ChevronDown, ChevronUp } from 'lucide-react';
import { fetchProviders, providerName } from '@/lib/providers';
import type { ProviderInfo } from '@/lib/providers';
import { listAccounts } from '@/lib/auth-api';
import type { AccountInfo } from '@/lib/auth-api';

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
        <h1 className="text-2xl font-bold">Profiles</h1>
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
          </ol>
          <pre className="bg-muted p-3 rounded-md text-xs overflow-x-auto mt-2">
{`# ~/.claude/settings.json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:8080",
    "ANTHROPIC_AUTH_TOKEN": "proxy-no-key"
  },
  "headers": {
    "X-Profile": "my-claude-profile"
  }
}`}
          </pre>
          <p className="text-xs text-muted-foreground mt-1">
            Model: any <code className="bg-muted px-1 rounded">claude-*</code> model. Gateway forwards directly to Anthropic API.
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
    "ANTHROPIC_AUTH_TOKEN": "proxy-no-key"
  },
  "headers": {
    "X-Profile": "my-gemini-profile"
  }
}`}
          </pre>
          <p className="text-xs text-muted-foreground mt-1">
            Model: <code className="bg-muted px-1 rounded">claude-*</code> or <code className="bg-muted px-1 rounded">gemini-*</code>. Gateway translates to Gemini CodeAssist automatically.
          </p>
        </Section>

        <Section id="zai-mode" title="Z.AI / GLM Mode (Default)">
          <p>When <code className="bg-muted px-1 rounded text-xs">GLM_MODE=true</code>, the default routing sends all requests to Z.AI API. No profile needed.</p>
          <pre className="bg-muted p-3 rounded-md text-xs overflow-x-auto mt-2">
{`# ~/.claude/settings.json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:8080",
    "ANTHROPIC_AUTH_TOKEN": "proxy-no-key"
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
        // Legacy: never had accountIds set — will be resolved by accounts loading effect
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
          // Auto-check-all only for legacy profiles (never set or null)
          // An explicit [] means user unchecked all — respect that
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
              <span key={id} className="text-[10px] font-mono bg-muted px-1.5 py-0.5 rounded">{id}</span>
            ))}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
