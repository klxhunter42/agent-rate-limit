import { useState, useEffect, useCallback } from 'react';
import { toast } from 'sonner';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { Search, Plus, Copy, Upload, Trash2, Edit2, Check, X, Info, Terminal, ChevronDown, ChevronUp, Key, Eye, EyeOff, Activity, Loader2, ArrowUp, ArrowDown } from 'lucide-react';
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/ui/dialog';
import { fetchProviders, providerName, isProviderAvailable } from '@/lib/providers';
import type { ProviderInfo } from '@/lib/providers';
import { listAccounts } from '@/lib/auth-api';
import type { AccountInfo } from '@/lib/auth-api';
import { fetchProfileUsage, fetchAccountUsage } from '@/lib/api';
import type { ProfileUsage, AccountUsage } from '@/lib/api';
import { InfoTip } from '@/components/shared/info-tip';
import { copyToClipboard } from '@/lib/clipboard';
import { cn } from '@/lib/utils';

const uid = () => crypto.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2, 11)}`;

const OPTIMIZER_STAGES = [
  { key: 'semantic_dedup', label: 'Semantic Dedup', desc: 'Remove semantic duplicates' },
  { key: 'chunker', label: 'Chunker', desc: 'Chunk and reorder system prompt' },
  { key: 'delta', label: 'Delta', desc: 'Delta metrics tracking' },
  { key: 'sketch', label: 'Sketch Dedup', desc: 'Sketch-based deduplication' },
  { key: 'summarizer', label: 'Summarizer', desc: 'Summarize on high budget usage' },
  { key: 'textcomp', label: 'Text Compression', desc: 'Regex filler/hedge removal' },
  { key: 'caveman', label: 'Caveman', desc: 'English terse output injection' },
  { key: 'pordee', label: 'Pordee (Thai)', desc: 'Thai terse output injection' },
  { key: 'toolcomp', label: 'Tool Compression', desc: 'Format-aware tool result compression' },
  { key: 'toolfilter', label: 'Tool Filter', desc: 'Filter tools by relevance' },
] as const;

interface ProfileTarget {
  id: string;
  target: string;
  baseUrl?: string;
  apiKey?: string;
  accountIds?: string[];
  passthroughAuth?: boolean;
}

interface Profile {
  name: string;
  provider: string;
  model?: string;
  target?: string;
  accountIds?: string[];
  targets?: ProfileTarget[];
  maxTokens?: number;
  temperature?: number;
  createdAt?: string;
  updatedAt?: string;
  apiKey?: string;
  passthroughAuth?: boolean;
  baseUrl?: string;
  optimizerOverrides?: Record<string, boolean>;
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
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);

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
    targets: ProfileTarget[];
  }) {
    const primary = data.targets[0];
    const provider = primary?.target ?? '';
 try {
 const res = await fetch('/v1/profiles', {
 method: 'POST',
 headers: { 'Content-Type': 'application/json' },
 body: JSON.stringify({ ...data, target: provider, provider, accountIds: primary?.accountIds ??[] }),
 });
 if (res.ok) {
 setShowCreate(false);
 fetchProfiles();
 } else {
 const text = await res.text().catch(() => '');
 if (res.status === 409) {
 toast.error(`Profile "${data.name}" already exists.`);
 } else {
				toast.error(`Failed to create profile: ${res.status} ${res.statusText}${text ? '\n' + text : ''}`);
 }
 }
 } catch (e) {
		toast.error(`Failed to create profile: ${e instanceof Error ? e.message : 'network error'}`);
 }
	}
  async function deleteProfile(name: string) {
    setDeleting(true);
    try {
      const res = await fetch(`/v1/profiles/delete`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name }),
      });
      if (res.ok) {
        fetchProfiles();
      } else {
        const text = await res.text().catch(() => '');
        toast.error(`Failed to delete profile "${name}": ${res.status} ${res.statusText}${text ? '\n' + text : ''}`);
      }
    } catch (e) {
      toast.error(`Failed to delete profile "${name}": ${e instanceof Error ? e.message : 'network error'}`);
    } finally {
      setDeleting(false);
      setDeleteConfirm(null);
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
              onDelete={() => setDeleteConfirm(p.name)}
            />
          ))}
        </div>
      )}
      <Dialog open={deleteConfirm !== null} onOpenChange={(open) => { if (!open) setDeleteConfirm(null); }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Profile</DialogTitle>
            <DialogDescription>
              Delete profile <span className="font-mono font-medium">{deleteConfirm}</span>? This cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <div className="flex justify-end gap-2 mt-4">
            <Button size="sm" variant="ghost" onClick={() => setDeleteConfirm(null)}>Cancel</Button>
            <Button size="sm" variant="destructive" onClick={() => deleteConfirm && deleteProfile(deleteConfirm)} disabled={deleting}>
              {deleting ? <Loader2 className="h-4 w-4 animate-spin mr-1" /> : <Trash2 className="h-4 w-4 mr-1" />}
              Delete
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function SetupGuideCard() {
  const [open, setOpen] = useState<string | null>('usage');
	const baseUrl = window.location.origin;

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
curl ${baseUrl}/v1/messages \
  -H "X-Profile: my-profile" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4-20250514",
    "max_tokens": 100,
    "messages": [...]
  }'`}
          </pre>
          <p>The gateway looks up the profile and uses its <strong>target</strong>, <strong>accountIds</strong>, <strong>model</strong>, and <strong>baseUrl</strong> to route the request. The profile&apos;s <strong>target</strong> determines which provider handles the request.</p>
          <p className="text-xs text-muted-foreground mt-1">
            Or use a profile API token (<code className="bg-muted px-1 rounded text-xs">arl_</code> prefix) - no <code className="bg-muted px-1 rounded text-xs">X-Profile</code> header needed. See <strong>API Key</strong> section below.
          </p>
        </Section>

        <Section id="claude-oauth" title="Claude OAuth Profile">
          <p>Routes through Anthropic API using Claude OAuth Bearer token. Requires OAuth account with <code className="bg-muted px-1 rounded text-xs">user:inference</code> scope.</p>
          <p className="mt-2">The gateway enables <strong>transparent passthrough</strong> for Claude OAuth requests:</p>
          <ul className="list-disc list-inside space-y-1 ml-2">
            <li>3-path billing injection: Go direct {">"} Sidecar fallback {">"} Direct proxy</li>
            <li>Skips optimizer and privacy masking pipeline</li>
            <li>Preserves exact client payload for compatibility</li>
            <li>Bootstraps Claude session automatically on first use</li>
          </ul>
          <p className="mt-2"><strong>Setup:</strong></p>
          <ol className="list-decimal list-inside space-y-1 ml-2">
            <li>Go to <strong>Providers</strong> page, click <strong>Connect</strong> on Claude (OAuth)</li>
            <li>Browser opens Claude login - authorize with your account</li>
            <li>Token is stored in Dragonfly automatically, refreshes every 30 min</li>
            <li>Create a profile with target <code className="bg-muted px-1 rounded text-xs">claude-oauth</code></li>
            <li>Select which accounts to include in Account Pool</li>
            <li>Click <strong>Generate</strong> on the profile card to create an <code className="bg-muted px-1 rounded text-xs">arl_</code> token</li>
          </ol>
          <pre className="bg-muted p-3 rounded-md text-xs overflow-x-auto mt-2">
{`// ~/.claude/settings.json
{
  "env": {
    "ANTHROPIC_BASE_URL": ${baseUrl},
    "ANTHROPIC_AUTH_TOKEN": "arl_your-generated-token"
  }
}`}
          </pre>
          <p className="text-xs text-muted-foreground mt-1">
            Model: any <code className="bg-muted px-1 rounded">claude-*</code> model. Default: <code className="bg-muted px-1 rounded">claude-haiku-4-5-20251001</code>. Utilization-aware round-robin across accounts (prefers &lt;80% 5h usage).
          </p>
        </Section>

        <Section id="gemini-oauth" title="Gemini OAuth Profile">
          <p>Routes through Google Gemini CodeAssist API using Google OAuth token. Gateway auto-translates Anthropic format to Gemini format, so Claude Code works seamlessly.</p>
          <ul className="list-disc list-inside space-y-1 ml-2 mt-1">
            <li>Uses CodeAssist proxy at <code className="bg-muted px-1 rounded text-xs">cloudcode-pa.googleapis.com</code></li>
            <li>Token auto-refreshes every 30 minutes</li>
            <li>Project ID resolved automatically during refresh cycle</li>
          </ul>
          <p className="mt-2"><strong>Setup:</strong></p>
          <ol className="list-decimal list-inside space-y-1 ml-2">
            <li>Go to <strong>Providers</strong> page, click <strong>Connect</strong> on Gemini (OAuth)</li>
            <li>Browser opens Google login - authorize with your account</li>
            <li>Token is stored and refreshed automatically</li>
            <li>Create profile with target <code className="bg-muted px-1 rounded text-xs">gemini-oauth</code></li>
            <li>Select which accounts to include in Account Pool</li>
            <li>Generate an <code className="bg-muted px-1 rounded text-xs">arl_</code> token for the profile</li>
          </ol>
          <pre className="bg-muted p-3 rounded-md text-xs overflow-x-auto mt-2">
{`// ~/.claude/settings.json
{
  "env": {
    "ANTHROPIC_BASE_URL": ${baseUrl},
    "ANTHROPIC_AUTH_TOKEN": "arl_your-generated-token"
  }
}`}
          </pre>
          <p className="text-xs text-muted-foreground mt-1">
            Model: <code className="bg-muted px-1 rounded">claude-*</code> or <code className="bg-muted px-1 rounded">gemini-*</code>. Default: <code className="bg-muted px-1 rounded">gemini-2.5-flash</code>. Note: <code className="bg-muted px-1 rounded text-xs">gemini-oauth</code> and <code className="bg-muted px-1 rounded text-xs">gemini</code> are separate providers - no cross-fallback.
          </p>
        </Section>

        <Section id="zai-mode" title="Z.AI / GLM Mode">
          <p>Controlled by <code className="bg-muted px-1 rounded text-xs">GLM_MODE</code> env var in <code className="bg-muted px-1 rounded text-xs">.env</code>:</p>
          <ul className="list-disc list-inside space-y-1 ml-2">
            <li><strong>GLM_MODE=true</strong>: Default routing sends all requests to Z.AI API. No profile needed. Key pool from <code className="bg-muted px-1 rounded text-xs">ZAI_API_KEYS</code> with adaptive limiter.</li>
            <li><strong>GLM_MODE=false</strong>: Multi-provider proxy mode. Profile required for all requests (<code className="bg-muted px-1 rounded text-xs">X-Profile</code> header or <code className="bg-muted px-1 rounded text-xs">arl_</code> token).</li>
          </ul>
          <pre className="bg-muted p-3 rounded-md text-xs overflow-x-auto mt-2">
{`// GLM_MODE=true: no profile needed
// ~/.claude/settings.json
{
  "env": {
    "ANTHROPIC_BASE_URL": ${baseUrl},
    "ANTHROPIC_AUTH_TOKEN": "any-zai-api-key"
  }
}`}
          </pre>
          <p className="text-xs text-muted-foreground mt-1">
            Model: <code className="bg-muted px-1 rounded">glm-*</code>. Vision: <code className="bg-muted px-1 rounded">glm-4.6v</code>, <code className="bg-muted px-1 rounded">glm-4.5v</code>. Adaptive limiter distributes across same-series models. Vision auto-routes images through native Zhipu endpoint.
          </p>
        </Section>

        <Section id="account-pool" title="Account Pool Selection">
          <p>When a profile has <strong>accountIds</strong> set, the gateway selects an account from only those IDs in the provider token pool. This is useful for:</p>
          <ul className="list-disc list-inside space-y-1">
            <li>Isolating specific OAuth accounts per profile</li>
            <li>Separating free-tier vs paid-tier usage</li>
            <li>Rotating through a subset of available accounts</li>
          </ul>
          <p className="mt-2"><strong>Token selection priority:</strong></p>
          <ol className="list-decimal list-inside space-y-1 ml-2">
            <li><code className="bg-muted px-1 rounded text-xs">accountIds</code> set - round-robin among selected accounts (prefers &lt;80% 5h utilization, skips paused/expired)</li>
            <li><code className="bg-muted px-1 rounded text-xs">passthroughAuth</code> - use client&apos;s own Bearer token</li>
            <li>Provider default token from token store</li>
            <li>Fallback to resolver key pool</li>
          </ol>
          <p className="mt-1">Leave <strong>accountIds</strong> empty to use all available accounts for the provider.</p>
          <p className="text-xs text-muted-foreground mt-1">
            Deleting an account from Providers page automatically removes it from all profiles.
          </p>
        </Section>

        <Section id="api-key" title="API Key (Profile Token)">
          <p>Each profile can generate a unique <code className="bg-muted px-1 rounded text-xs">arl_</code> token. Use this as <code className="bg-muted px-1 rounded text-xs">ANTHROPIC_AUTH_TOKEN</code> or <code className="bg-muted px-1 rounded text-xs">ANTHROPIC_API_KEY</code> in Claude Code or any client. The gateway resolves the token to its profile automatically.</p>
          <ul className="list-disc list-inside space-y-1">
            <li>Click <strong>Generate</strong> on any profile card</li>
            <li>Copy the token and set it as <code className="bg-muted px-1 rounded text-xs">ANTHROPIC_AUTH_TOKEN</code></li>
            <li>No <code className="bg-muted px-1 rounded text-xs">X-Profile</code> header needed - the token identifies the profile</li>
            <li>Click <strong>Revoke</strong> to invalidate a token at any time</li>
          </ul>
          <pre className="bg-muted p-3 rounded-md text-xs overflow-x-auto mt-2">
{`# Generate token
curl -X POST ${baseUrl}/v1/profiles/meow/tokens
# => {"token":"arl_abc123...","profile":"meow"}

# Option A: Environment variables
export ANTHROPIC_BASE_URL=${baseUrl}
export ANTHROPIC_AUTH_TOKEN=arl_abc123...
claude

# Option B: settings.json
# ~/.claude/settings.json
{
  "env": {
    "ANTHROPIC_BASE_URL": ${baseUrl},
    "ANTHROPIC_AUTH_TOKEN": "arl_abc123..."
  }
}`}
          </pre>
        </Section>

        <Section id="docker-haiku" title="Claude Code Container">
          <p>Run Claude Code in a Docker container, routed through a profile to use any model via OAuth. No local install needed.</p>
          <p className="mt-2"><strong>Setup:</strong></p>
          <ol className="list-decimal list-inside space-y-1 ml-2">
            <li>Create a profile with target <code className="bg-muted px-1 rounded text-xs">claude-oauth</code> (or any provider)</li>
            <li>Generate an <code className="bg-muted px-1 rounded text-xs">arl_</code> token for the profile</li>
            <li>Create <code className="bg-muted px-1 rounded text-xs">{"docker/settings-{name}.json"}</code>:</li>
          </ol>
          <pre className="bg-muted p-3 rounded-md text-xs overflow-x-auto mt-2">
{`{
  "env": {
    "ANTHROPIC_BASE_URL": ${baseUrl},
    "ANTHROPIC_AUTH_TOKEN": "arl_your-generated-token"
  }
}`}
          </pre>
          <p className="text-xs text-muted-foreground mt-1">
            Works with both <code className="bg-muted px-1 rounded text-xs">ANTHROPIC_AUTH_TOKEN</code> (Authorization: Bearer header) and <code className="bg-muted px-1 rounded text-xs">ANTHROPIC_API_KEY</code> (x-api-key header). The gateway reads profile tokens from either.
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
{`# One-shot prompt
docker exec meow claude -p "say hello"

# Interactive mode
docker exec -it meow claude`}
          </pre>
          <p className="text-xs text-muted-foreground mt-1">
            The gateway auto-strips unsupported parameters (effort, thinking) for Haiku. Model overrides available: <code className="bg-muted px-1 rounded text-xs">opusModel</code>, <code className="bg-muted px-1 rounded text-xs">sonnetModel</code>, <code className="bg-muted px-1 rounded text-xs">haikuModel</code>.
          </p>
        </Section>

        <Section id="multi-target" title="Multi-Target Profiles (Failover)">
          <p>One profile can have multiple targets with automatic failover. When target #1 fails (rate limit, error, timeout), the gateway falls back to target #2, then #3, etc.</p>
          <p className="mt-2"><strong>Setup:</strong></p>
          <ol className="list-decimal list-inside space-y-1 ml-2">
            <li>Click <strong>New</strong> on the Profiles page</li>
            <li>Fill in the profile name</li>
            <li>First target is created automatically - select provider and accounts</li>
            <li>Click <strong>Add Target</strong> to add more targets</li>
            <li>Use arrow buttons (up/down) to reorder priority</li>
            <li>Click <strong>Create</strong></li>
          </ol>
          <pre className="bg-muted p-3 rounded-md text-xs overflow-x-auto mt-2">
{`# Via API: create hybrid failover profile
curl -X POST ${baseUrl}/v1/profiles \
  -H "Content-Type: application/json" \
  -d '{
    "name": "hybrid",
    "targets": [
      {"id": "t1", "target": "claude-oauth", "accountIds": ["account-1"]},
      {"id": "t2", "target": "gemini-oauth", "accountIds": ["account-2"]}
    ]
  }'`}
          </pre>
          <p className="text-xs text-muted-foreground mt-1">
            Each target can have its own <code className="bg-muted px-1 rounded text-xs">accountIds</code>, <code className="bg-muted px-1 rounded text-xs">baseUrl</code>, and <code className="bg-muted px-1 rounded text-xs">apiKey</code>. Priority is determined by array order (index 0 = highest).
          </p>
        </Section>

        <Section id="passthrough" title="Passthrough Auth">
          <p>When <code className="bg-muted px-1 rounded text-xs">passthroughAuth</code> is enabled on a profile, the gateway uses the client&apos;s own <code className="bg-muted px-1 rounded text-xs">Authorization: Bearer</code> or <code className="bg-muted px-1 rounded text-xs">x-api-key</code> header instead of stored tokens. Useful for:</p>
          <ul className="list-disc list-inside space-y-1">
            <li>Letting each user authenticate with their own credentials</li>
            <li>Transparent proxying without storing upstream tokens</li>
          </ul>
          <p className="text-xs text-muted-foreground mt-1">
            Claude OAuth transparent passthrough is enabled automatically when the token has <code className="bg-muted px-1 rounded text-xs">sk-ant-oat01-</code> prefix.
          </p>
        </Section>

        <Section id="target" title="Target / Provider Types">
          <p>The <strong>target</strong> field determines the upstream API format and routing:</p>
          <ul className="list-disc list-inside space-y-1">
            <li><code className="bg-muted px-1 rounded text-xs">claude-oauth</code> - Claude via OAuth (Bearer + Anthropic API, billing injection)</li>
            <li><code className="bg-muted px-1 rounded text-xs">gemini-oauth</code> - Gemini via OAuth (Bearer + CodeAssist proxy)</li>
            <li><code className="bg-muted px-1 rounded text-xs">anthropic</code> - Anthropic direct API key</li>
            <li><code className="bg-muted px-1 rounded text-xs">gemini</code> - Gemini direct API key</li>
            <li><code className="bg-muted px-1 rounded text-xs">openai</code> - OpenAI API</li>
            <li><code className="bg-muted px-1 rounded text-xs">zai</code> - Z.AI / Zhipu API (Anthropic format)</li>
            <li><code className="bg-muted px-1 rounded text-xs">openrouter</code> - OpenRouter (200+ models)</li>
            <li><code className="bg-muted px-1 rounded text-xs">copilot</code> - GitHub Copilot (device code auth)</li>
            <li><code className="bg-muted px-1 rounded text-xs">deepseek</code> - DeepSeek</li>
            <li><code className="bg-muted px-1 rounded text-xs">qwen</code> - Qwen / Aliyun DashScope (device code auth)</li>
            <li><code className="bg-muted px-1 rounded text-xs">kimi</code> - Kimi / Moonshot</li>
            <li><code className="bg-muted px-1 rounded text-xs">huggingface</code> - HuggingFace Inference</li>
            <li><code className="bg-muted px-1 rounded text-xs">ollama</code> - Ollama (local models)</li>
            <li><code className="bg-muted px-1 rounded text-xs">lotus</code> - Lotuss (OpenAI-compatible, model override: "default")</li>
            <li><code className="bg-muted px-1 rounded text-xs">cursor</code> / <code className="bg-muted px-1 rounded text-xs">codebuddy</code> / <code className="bg-muted px-1 rounded text-xs">kilo</code> / <code className="bg-muted px-1 rounded text-xs">agy</code> - Other providers</li>
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
  onSubmit: (data: { name: string; targets: ProfileTarget[] }) => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState('');
  const [targets, setTargets] = useState<ProfileTarget[]>([
    { id: uid(), target: '', accountIds: [] },
  ]);
  const [providers, setProviders] = useState<ProviderInfo[]>([]);
  const [accountsMap, setAccountsMap] = useState<Map<string, AccountInfo[]>>(new Map());
  const [accountsLoading, setAccountsLoading] = useState<Set<string>>(new Set());
  const canSubmit = name.trim() && targets.some((t) => t.target);

  useEffect(() => {
    fetchProviders().then((list) => {
      setProviders(list);
      if (list.length > 0 && targets[0] && !targets[0].target) {
        setTargets((prev) =>
          prev.map((t, i) => (i === 0 ? { ...t, target: list[0]!.id } : t))
        );
      }
    });
  }, []);

  useEffect(() => {
    for (const t of targets) {
      if (!t.target || accountsMap.has(t.target)) continue;
      setAccountsLoading((prev) => new Set(prev).add(t.target!));
      listAccounts(t.target)
        .then((list) => setAccountsMap((prev) => new Map(prev).set(t.target!, list)))
        .catch(() => setAccountsMap((prev) => new Map(prev).set(t.target!, [])))
        .finally(() => setAccountsLoading((prev) => { const n = new Set(prev); n.delete(t.target!); return n; }));
    }
  }, [targets.map((t) => t.target).join(','), accountsMap]);

  function addTarget() {
    const firstProvider = providers[0]?.id ?? '';
    setTargets((prev) => [
      ...prev,
      { id: uid(), target: firstProvider, accountIds: [] },
    ]);
  }

  function removeTarget(id: string) {
    setTargets((prev) => prev.filter((t) => t.id !== id));
  }

  function updateTarget(id: string, field: keyof ProfileTarget, value: unknown) {
    setTargets((prev) =>
      prev.map((t) => {
        if (t.id !== id) return t;
        const updated = { ...t, [field]: value };
        if (field === 'target') updated.accountIds = [];
        return updated;
      })
    );
  }

  function toggleAccount(targetId: string, accountId: string) {
    setTargets((prev) =>
      prev.map((t) => {
        if (t.id !== targetId) return t;
        const ids = t.accountIds ?? [];
        return {
          ...t,
          accountIds: ids.includes(accountId)
            ? ids.filter((x) => x !== accountId)
            : [...ids, accountId],
        };
      })
    );
  }

  function moveTarget(id: string, dir: -1 | 1) {
    setTargets((prev) => {
      const idx = prev.findIndex((t) => t.id === id);
      if (idx < 0) return prev;
      const swap = idx + dir;
      if (swap < 0 || swap >= prev.length) return prev;
      const next = [...prev];
      [next[idx], next[swap]] = [next[swap]!, next[idx]!];
      return next;
    });
  }

  return (
    <div className="space-y-3">
      <div>
        <label className="text-xs text-muted-foreground">Name *</label>
        <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="my-profile" />
      </div>

      <div>
        <div className="flex items-center justify-between mb-1">
          <label className="text-xs text-muted-foreground">Targets (priority order)</label>
          <Button size="sm" variant="outline" className="h-6 text-xs" onClick={addTarget}>
            <Plus className="h-3 w-3 mr-1" /> Add Target
          </Button>
        </div>
        <div className="space-y-2">
          {targets.map((t, idx) => (
            <div key={t.id} className="rounded-md border p-2 space-y-2">
              <div className="flex items-center gap-2">
                <span className="text-xs font-mono text-muted-foreground w-5">#{idx + 1}</span>
                <select
                  className="flex-1 h-8 rounded-md border bg-background px-2 text-sm"
                  value={t.target}
                  onChange={(e) => updateTarget(t.id, 'target', e.target.value)}
                >
                  {providers.length === 0 && <option value="">Loading...</option>}
                  {providers.filter((p) => isProviderAvailable(p.id)).map((p) => (
                    <option key={p.id} value={p.id}>{p.name || providerName(p.id)}</option>
                  ))}
                </select>
                <Button size="icon" variant="ghost" className="h-7 w-7" onClick={() => moveTarget(t.id, -1)} disabled={idx === 0} title="Move up">
                  <ArrowUp className="h-3 w-3" />
                </Button>
                <Button size="icon" variant="ghost" className="h-7 w-7" onClick={() => moveTarget(t.id, 1)} disabled={idx === targets.length - 1} title="Move down">
                  <ArrowDown className="h-3 w-3" />
                </Button>
                {targets.length > 1 && (
                  <Button size="icon" variant="ghost" className="h-7 w-7 text-destructive" onClick={() => removeTarget(t.id)} title="Remove">
                    <X className="h-3 w-3" />
                  </Button>
                )}
              </div>
              {(() => {
                const accs = accountsMap.get(t.target);
                if (!accs || accs.length === 0) return null;
                return (
                  <div className="ml-5">
                    <div className="max-h-32 overflow-y-auto rounded-md border p-1.5 space-y-0.5">
                      {accountsLoading.has(t.target) && <div className="text-xs text-muted-foreground px-2">Loading...</div>}
                      {accs.map((acc) => (
                        <label key={acc.id} className="flex items-center gap-2 px-2 py-0.5 hover:bg-muted/50 rounded text-sm cursor-pointer">
                          <input
                            type="checkbox"
                            checked={(t.accountIds ?? []).includes(acc.id)}
                            onChange={() => toggleAccount(t.id, acc.id)}
                            className="rounded"
                          />
                          <span className="font-mono text-xs">{acc.id}</span>
                          {acc.email && <span className="text-xs text-muted-foreground">({acc.email})</span>}
                        </label>
                      ))}
                    </div>
                  </div>
                );
              })()}
            </div>
          ))}
        </div>
      </div>

      <div className="flex gap-2 justify-end">
        <Button size="sm" variant="ghost" onClick={onCancel}>
          <X className="h-4 w-4 mr-1" /> Cancel
        </Button>
        <Button size="sm" onClick={() => onSubmit({ name, targets })} disabled={!canSubmit}>
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
}: {
  profile: Profile;
  editing: boolean;
  onEdit: () => void;
  onCancelEdit: () => void;
  onSave: (name: string, data: Record<string, unknown>) => void;
  onDelete: () => void;
}) {
  const [editName, setEditName] = useState(profile.name);
  const [editTargets, setEditTargets] = useState<ProfileTarget[]>([]);
  const [editOptimizerOverrides, setEditOptimizerOverrides] = useState<Record<string, boolean>>({});
  const [providers, setProviders] = useState<ProviderInfo[]>([]);
  const [accountsMap, setAccountsMap] = useState<Map<string, AccountInfo[]>>(new Map());
  const [accountsLoading, setAccountsLoading] = useState<Set<string>>(new Set());

  const resolvedProvider = profile.provider || profile.target || '';

  useEffect(() => {
    fetchProviders().then(setProviders);
  }, []);

  useEffect(() => {
    if (editing) {
      setEditName(profile.name);
      setEditOptimizerOverrides((profile.optimizerOverrides as Record<string, boolean>) ?? {});
      if (profile.targets && profile.targets.length > 0) {
        setEditTargets(profile.targets.map((t) => ({ ...t, id: t.id || uid() })));
      } else {
        setEditTargets([
          {
            id: uid(),
            target: resolvedProvider,
            accountIds: (profile.accountIds ?? []).filter(Boolean),
            apiKey: undefined,
            baseUrl: undefined,
            passthroughAuth: profile.passthroughAuth,
          },
        ]);
      }
    }
  }, [editing, profile.name, profile.targets, profile.accountIds, profile.passthroughAuth, resolvedProvider]);

  useEffect(() => {
    if (!editing) return;
    for (const t of editTargets) {
      if (!t.target || accountsMap.has(t.target)) continue;
      setAccountsLoading((prev) => new Set(prev).add(t.target!));
      listAccounts(t.target)
        .then((list) => setAccountsMap((prev) => new Map(prev).set(t.target!, list)))
        .catch(() => setAccountsMap((prev) => new Map(prev).set(t.target!, [])))
        .finally(() => setAccountsLoading((prev) => { const n = new Set(prev); n.delete(t.target!); return n; }));
    }
  }, [editing, editTargets]);

  function addTarget() {
    const firstProvider = providers[0]?.id ?? '';
    setEditTargets((prev) => [
      ...prev,
      { id: uid(), target: firstProvider, accountIds: [] },
    ]);
  }

  function removeTarget(id: string) {
    setEditTargets((prev) => prev.filter((t) => t.id !== id));
  }

  function updateTarget(id: string, field: keyof ProfileTarget, value: unknown) {
    setEditTargets((prev) =>
      prev.map((t) => {
        if (t.id !== id) return t;
        const updated = { ...t, [field]: value };
        if (field === 'target') updated.accountIds = [];
        return updated;
      })
    );
  }

  function toggleAccount(targetId: string, accountId: string) {
    setEditTargets((prev) =>
      prev.map((t) => {
        if (t.id !== targetId) return t;
        const ids = t.accountIds ?? [];
        return {
          ...t,
          accountIds: ids.includes(accountId)
            ? ids.filter((x) => x !== accountId)
            : [...ids, accountId],
        };
      })
    );
  }

  function moveTarget(id: string, dir: -1 | 1) {
    setEditTargets((prev) => {
      const idx = prev.findIndex((t) => t.id === id);
      if (idx < 0) return prev;
      const swap = idx + dir;
      if (swap < 0 || swap >= prev.length) return prev;
      const next = [...prev];
      [next[idx], next[swap]] = [next[swap]!, next[idx]!];
      return next;
    });
  }

  if (editing) {
    return (
      <Card>
        <CardContent className="pt-4 space-y-3">
          <div>
            <label className="text-xs text-muted-foreground">Profile Name</label>
            <Input value={editName} onChange={(e) => setEditName(e.target.value)} />
          </div>

          <div>
            <div className="flex items-center justify-between mb-1">
              <label className="text-xs text-muted-foreground">Targets (priority order)</label>
              <Button size="sm" variant="outline" className="h-6 text-xs" onClick={addTarget}>
                <Plus className="h-3 w-3 mr-1" /> Add Target
              </Button>
            </div>
            <div className="space-y-2">
              {editTargets.map((t, idx) => (
                <div key={t.id} className="rounded-md border p-2 space-y-2">
                  <div className="flex items-center gap-2">
                    <span className="text-xs font-mono text-muted-foreground w-5">#{idx + 1}</span>
                    <select
                      className="flex-1 h-8 rounded-md border bg-background px-2 text-sm"
                      value={t.target}
                      onChange={(e) => updateTarget(t.id, 'target', e.target.value)}
                    >
                      {providers.filter((p) => isProviderAvailable(p.id)).map((p) => (
                        <option key={p.id} value={p.id}>{p.name || providerName(p.id)}</option>
                      ))}
                    </select>
                    <Button size="icon" variant="ghost" className="h-7 w-7" onClick={() => moveTarget(t.id, -1)} disabled={idx === 0}>
                      <ArrowUp className="h-3 w-3" />
                    </Button>
                    <Button size="icon" variant="ghost" className="h-7 w-7" onClick={() => moveTarget(t.id, 1)} disabled={idx === editTargets.length - 1}>
                      <ArrowDown className="h-3 w-3" />
                    </Button>
                    {editTargets.length > 1 && (
                      <Button size="icon" variant="ghost" className="h-7 w-7 text-destructive" onClick={() => removeTarget(t.id)}>
                        <X className="h-3 w-3" />
                      </Button>
                    )}
                  </div>
                  {(() => {
                    const accs = accountsMap.get(t.target);
                    if (!accs || accs.length === 0) return null;
                    return (
                      <div className="ml-5">
                        <div className="max-h-32 overflow-y-auto rounded-md border p-1.5 space-y-0.5">
                          {accountsLoading.has(t.target) && <div className="text-xs text-muted-foreground px-2">Loading...</div>}
                          {accs.map((acc) => (
                            <label key={acc.id} className="flex items-center gap-2 px-2 py-0.5 hover:bg-muted/50 rounded text-sm cursor-pointer">
                              <input
                                type="checkbox"
                                checked={(t.accountIds ?? []).includes(acc.id)}
                                onChange={() => toggleAccount(t.id, acc.id)}
                                className="rounded"
                              />
                              <span className="font-mono text-xs">{acc.id}</span>
                              {acc.email && <span className="text-xs text-muted-foreground">({acc.email})</span>}
                            </label>
                          ))}
                        </div>
                      </div>
                    );
                  })()}
                </div>
              ))}
            </div>
          </div>

          {/* Optimizer Overrides */}
          <div>
            <div className="flex items-center justify-between mb-1.5">
              <label className="text-xs text-muted-foreground font-medium">Optimizer Overrides</label>
              <div className="flex gap-1">
                <Button size="sm" variant="ghost" className="h-5 text-[10px] px-1.5 text-muted-foreground hover:text-foreground" onClick={() => {
                  const all: Record<string, boolean> = {};
                  OPTIMIZER_STAGES.forEach(({ key }) => { all[key] = true; });
                  setEditOptimizerOverrides(all);
                }}>All On</Button>
                <Button size="sm" variant="ghost" className="h-5 text-[10px] px-1.5 text-muted-foreground hover:text-foreground" onClick={() => {
                  const all: Record<string, boolean> = {};
                  OPTIMIZER_STAGES.forEach(({ key }) => { all[key] = false; });
                  setEditOptimizerOverrides(all);
                }}>All Off</Button>
                <Button size="sm" variant="ghost" className="h-5 text-[10px] px-1.5 text-muted-foreground hover:text-foreground" onClick={() => setEditOptimizerOverrides({})}>
                  Reset
                </Button>
              </div>
            </div>
            <div className="rounded-md border p-2 space-y-0.5">
              {OPTIMIZER_STAGES.map(({ key, label, desc }) => {
                const overridden = key in editOptimizerOverrides;
                const enabled = editOptimizerOverrides[key];
                return (
                  <div key={key} className="flex items-center gap-2 px-1 py-1 rounded hover:bg-muted/30 group">
                    <button
                      type="button"
                      role="switch"
                      aria-checked={overridden ? (enabled ? 'true' : 'false') : 'mixed'}
                      className={cn(
                        'relative inline-flex h-4 w-8 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors',
                        overridden
                          ? enabled ? 'bg-green-500' : 'bg-red-400'
                          : 'bg-muted'
                      )}
                      onClick={() => {
                        setEditOptimizerOverrides(prev => {
                          const next = { ...prev };
                          if (!overridden) {
                            next[key] = false;
                          } else if (!enabled) {
                            next[key] = true;
                          } else {
                            delete next[key];
                          }
                          return next;
                        });
                      }}
                    >
                      <span className={cn(
                        'pointer-events-none block h-3 w-3 rounded-full bg-white shadow-sm ring-0 transition-transform',
                        overridden
                          ? (enabled ? 'translate-x-3.5' : 'translate-x-0')
                          : 'translate-x-1.5'
                      )} />
                    </button>
                    <span className="text-xs font-medium w-28">{label}</span>
                    <span className="text-[10px] text-muted-foreground flex-1 hidden sm:inline">{desc}</span>
                    <span className={cn(
                      'text-[9px] font-mono px-1 py-0.5 rounded min-w-[32px] text-center',
                      overridden
                        ? enabled ? 'bg-green-500/10 text-green-600 dark:text-green-400' : 'bg-red-500/10 text-red-500 dark:text-red-400'
                        : 'bg-muted text-muted-foreground'
                    )}>
                      {overridden ? (enabled ? 'ON' : 'OFF') : 'AUTO'}
                    </span>
                  </div>
                );
              })}
            </div>
          </div>

          <div className="flex gap-2 justify-end">
            <Button size="sm" variant="ghost" onClick={onCancelEdit}>
              <X className="h-4 w-4 mr-1" /> Cancel
            </Button>
            <Button size="sm" disabled={!editName.trim()} onClick={() => {
              const primary = editTargets[0];
              const primaryTarget = primary?.target ?? '';
              const primaryAccountIds = primary?.accountIds ?? [];
              onSave(profile.name, {
                ...profile,
                name: editName.trim(),
                target: primaryTarget,
                provider: primaryTarget,
                accountIds: primaryAccountIds,
                baseUrl: profile.baseUrl,
                apiKey: profile.apiKey,
                targets: editTargets,
                optimizerOverrides: Object.keys(editOptimizerOverrides).length > 0 ? editOptimizerOverrides : undefined,
              });
            }}>
              <Check className="h-4 w-4 mr-1" /> Save
            </Button>
          </div>
        </CardContent>
      </Card>
    );
  }

	return (
		<_ProfileCardView profile={profile} onEdit={onEdit} onDelete={onDelete} />
	);
}

interface TokenInfo {
  keyName: string;
  token: string;
  profile: string;
  expiresAt?: string;
  createdAt: string;
}

function _ProfileCardView({
  profile,
  onEdit,
  onDelete,
}: {
  profile: Profile;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const resolvedProvider = profile.provider || profile.target || '';
  const [tokens, setTokens] = useState<TokenInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [showNewKey, setShowNewKey] = useState(false);
  const [newKeyName, setNewKeyName] = useState('');
  const [newKeyExpiry, setNewKeyExpiry] = useState(0);
const [customMinutes, setCustomMinutes] = useState(60);
  const [generating, setGenerating] = useState(false);
  const [revealedToken, setRevealedToken] = useState<string | null>(null);
  const [revealedKeys, setRevealedKeys] = useState<Set<string>>(new Set());
  const [copiedKey, setCopiedKey] = useState<string | null>(null);
  const [accountMap, setAccountMap] = useState<Map<string, string>>(new Map());
  const [usage, setUsage] = useState<ProfileUsage | null>(null);
  const [accountUsage, setAccountUsage] = useState<AccountUsage[]>([]);
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

  useEffect(() => {
    fetchAccountUsage()
      .then((data) => {
        const ids = new Set(profile.accountIds ?? []);
        const allTargets = profile.targets ?? [];
        for (const t of allTargets) {
          for (const a of t.accountIds ?? []) ids.add(a);
        }
        setAccountUsage(ids.size > 0 ? data.filter((a) => ids.has(a.accountId)) : data);
      })
      .catch(() => {});
  }, [profile.name, profile.accountIds, profile.targets]);

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
      const expirySeconds = newKeyExpiry === -1 ? customMinutes * 60 : newKeyExpiry;
if (expirySeconds > 0) body.expiresIn = expirySeconds;
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
        toast.error(`Failed to revoke: ${res.status} ${res.statusText}`);
      }
    } catch (e) {
      toast.error(`Failed to revoke: ${e instanceof Error ? e.message : 'network error'}`);
    } finally {
      setRevoking(null);
      setRevokeConfirm(null);
    }
  }

  function copyToken(token: string, keyName: string) {
    copyToClipboard(token).then(() => {
      setCopiedKey(keyName);
      setTimeout(() => setCopiedKey(null), 2000);
    });
  }

  return (
    <Card>
      <CardContent className="pt-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <span className="font-mono text-sm font-semibold">{profile.name}</span>
            {profile.targets && profile.targets.length > 0 ? (
              <div className="flex items-center gap-1">
                {profile.targets.map((t, idx) => (
                  <Badge key={t.id || idx} variant="outline" className="text-[10px] h-5 gap-0.5">
                    <span className="text-muted-foreground">#{idx + 1}</span>
                    {providerName(t.target)}
                  </Badge>
                ))}
              </div>
            ) : (
              <Badge variant="outline">{providerName(resolvedProvider)}</Badge>
            )}
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
        {profile.optimizerOverrides && Object.keys(profile.optimizerOverrides).length > 0 && (
          <div className="mt-1.5 flex items-center gap-1.5 flex-wrap">
            <span className="text-[10px] text-muted-foreground">Optimizers:</span>
            {Object.entries(profile.optimizerOverrides).map(([key, enabled]) => (
              <span
                key={key}
                className={cn(
                  'text-[9px] font-mono px-1.5 py-0.5 rounded',
                  enabled ? 'bg-green-500/10 text-green-600 dark:text-green-400' : 'bg-red-500/10 text-red-500 dark:text-red-400'
                )}
              >
                {key}: {enabled ? 'ON' : 'OFF'}
              </span>
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
            <option value={300}>5 minutes</option>
            <option value={900}>15 minutes</option>
            <option value={1800}>30 minutes</option>
                    <option value={3600}>1 hour</option>
                    <option value={86400}>1 day</option>
                    <option value={604800}>7 days</option>
                    <option value={2592000}>30 days</option>
                    <option value={31536000}>1 year</option>
                              <option value={-1}>Custom...</option>
                  </select>
          {newKeyExpiry === -1 && (
            <div className="flex items-center gap-1 mt-1">
              <Input
                type="number"
                min={1}
                max={525600}
                value={customMinutes}
                onChange={(e) => setCustomMinutes(Number(e.target.value))}
                className="h-6 text-xs w-20"
                placeholder="minutes"
              />
              <span className="text-[10px] text-muted-foreground whitespace-nowrap">min</span>
            </div>
          )}
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
                    {isRevealed ? <Eye className="h-3 w-3" /> : <EyeOff className="h-3 w-3" />}
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
        {accountUsage.length > 0 && (
          <div className="mt-3 border-t pt-3">
            <div className="flex items-center gap-2 mb-2">
              <Activity className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="text-xs font-medium">Account Usage</span>
              <Badge variant="secondary" className="text-[10px] h-4">{accountUsage.length}</Badge>
            </div>
            <div className="space-y-1.5">
              {accountUsage.map((a) => {
                const pct = usage && usage.total_cost > 0 ? (a.total_cost / usage.total_cost * 100) : 0;
                return (
                  <div key={a.accountId} className="rounded-md border p-2">
                    <div className="flex items-center justify-between mb-1">
                      <span className="font-mono text-[11px] truncate">{accountMap.get(a.accountId) || a.accountId}</span>
                      <div className="flex items-center gap-2 text-[10px] text-muted-foreground">
                        <span>{a.total_requests} req</span>
                        <span>${a.total_cost.toFixed(4)}</span>
                        {pct > 0 && (
                          <span className="font-medium text-foreground">{pct.toFixed(1)}%</span>
                        )}
                      </div>
                    </div>
                    <div className="flex items-center gap-3 text-[10px] text-muted-foreground">
                      <span>{(a.total_tokens_in / 1000).toFixed(1)}k in</span>
                      <span>{(a.total_tokens_out / 1000).toFixed(1)}k out</span>
                    </div>
                    {pct > 0 && (
                      <div className="mt-1 h-1 bg-muted rounded-full overflow-hidden">
                        <div className="h-full bg-primary rounded-full" style={{ width: `${Math.min(pct, 100)}%` }} />
                      </div>
                    )}
                    {a.models && a.models.length > 0 && (
                      <div className="mt-1 space-y-0.5">
                        {a.models.map((m) => (
                          <div key={m.model} className="flex items-center gap-1 text-[9px] text-muted-foreground">
                            <span className="font-mono">{m.model}</span>
                            <span>{m.requests}req</span>
                            <span>${m.cost.toFixed(4)}</span>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
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
