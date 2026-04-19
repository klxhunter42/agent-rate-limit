import { useState, useEffect, useCallback } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { Search, Plus, Copy, Download, Upload, Trash2, Edit2, Check, X } from 'lucide-react';
import { fetchProviders, providerName } from '@/lib/providers';
import type { ProviderInfo } from '@/lib/providers';

interface Profile {
  name: string;
  provider: string;
  model?: string;
  apiKey?: string;
  baseUrl?: string;
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
    baseUrl: string;
    apiKey: string;
    model: string;
    target: string;
  }) {
    const res = await fetch('/v1/profiles', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(data),
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
          <Button size="sm" variant="outline" onClick={() => setShowImport(!showImport)}>
            <Upload className="h-4 w-4 mr-1" /> Import
          </Button>
          <Button size="sm" onClick={() => setShowCreate(!showCreate)}>
            <Plus className="h-4 w-4 mr-1" /> New
          </Button>
        </div>
      </div>

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
          {search ? 'No profiles match your search' : 'No profiles yet'}
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
              onSave={(name, data) => {
                fetch(`/v1/profiles/${encodeURIComponent(name)}`, {
                  method: 'PUT',
                  headers: { 'Content-Type': 'application/json' },
                  body: JSON.stringify(data),
                }).then((r) => {
                  if (r.ok) { setEditing(null); fetchProfiles(); }
                });
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

function CreateProfileForm({
  onSubmit,
  onCancel,
}: {
  onSubmit: (data: { name: string; baseUrl: string; apiKey: string; model: string; target: string }) => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState('');
  const [baseUrl, setBaseUrl] = useState('');
  const [apiKey, setApiKey] = useState('');
  const [model, setModel] = useState('');
  const [target, setTarget] = useState('');
  const [providers, setProviders] = useState<ProviderInfo[]>([]);
  const canSubmit = name.trim() && apiKey.trim() && target;

  useEffect(() => {
    fetchProviders().then((list) => {
      setProviders(list);
      if (list.length > 0 && !target) setTarget(list[0]!.id);
    });
  }, []);

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
            onChange={(e) => setTarget(e.target.value)}
          >
            {providers.length === 0 && <option value="">Loading...</option>}
            {providers.map((p) => (
              <option key={p.id} value={p.id}>{providerName(p.id)}</option>
            ))}
          </select>
        </div>
      </div>
      <div className="grid grid-cols-2 gap-3">
        <div>
          <label className="text-xs text-muted-foreground">Base URL</label>
          <Input value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} placeholder="https://api.anthropic.com" />
        </div>
        <div>
          <label className="text-xs text-muted-foreground">Model</label>
          <Input value={model} onChange={(e) => setModel(e.target.value)} placeholder="claude-sonnet-4-20250514" />
        </div>
      </div>
      <div>
        <label className="text-xs text-muted-foreground">API Key *</label>
        <Input type="password" value={apiKey} onChange={(e) => setApiKey(e.target.value)} placeholder="sk-..." />
      </div>
      <div className="flex gap-2 justify-end">
        <Button size="sm" variant="ghost" onClick={onCancel}>
          <X className="h-4 w-4 mr-1" /> Cancel
        </Button>
        <Button size="sm" onClick={() => onSubmit({ name, baseUrl, apiKey, model, target })} disabled={!canSubmit}>
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
  const [model, setModel] = useState(profile.model ?? '');

  if (editing) {
    return (
      <Card>
        <CardContent className="pt-4 flex gap-3 items-end">
          <div className="flex-1">
            <label className="text-xs text-muted-foreground">Model</label>
            <Input value={model} onChange={(e) => setModel(e.target.value)} />
          </div>
          <Button size="sm" onClick={() => onSave(profile.name, { ...profile, model })}>
            <Check className="h-4 w-4" />
          </Button>
          <Button size="sm" variant="ghost" onClick={onCancelEdit}>
            <X className="h-4 w-4" />
          </Button>
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
            <Badge variant="outline">{providerName(profile.provider)}</Badge>
            {profile.model && <span className="text-xs text-muted-foreground">{profile.model}</span>}
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
      </CardContent>
    </Card>
  );
}
