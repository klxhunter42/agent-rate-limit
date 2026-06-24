import { useState, useEffect, useCallback, useRef } from 'react';
import { toast } from 'sonner';
import * as authApi from '@/lib/auth-api';
import type { RateLimitStatus } from '@/lib/auth-api';
import { useAuthFlow } from '@/hooks/use-auth-flow';
import { useDashboard } from '@/contexts/dashboard-context';
import { AccountList } from './account-list';
import { DeviceCodeDialog } from '@/components/auth/device-code-dialog';
import { AuthCodeDialog } from '@/components/auth/auth-code-dialog';
import { ApiKeyDialog } from '@/components/auth/api-key-dialog';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Bot, Sparkles, Zap, Github, Plus, ChevronDown, ChevronUp, Loader2, Globe, Info, Brain, Cpu, Server, Code, Terminal, Coffee, Blocks, Pencil, Trash2, X, Check } from 'lucide-react';
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip';
import { cn } from '@/lib/utils';
import type { LucideIcon } from 'lucide-react';
import { fetchProviders, updateProviderUpstream, registerCustomProvider, deleteCustomProvider, updateCustomProvider } from '@/lib/providers';
import type { ProviderInfo } from '@/lib/providers';

interface ProviderDef {
  id: string;
  name: string;
  icon: LucideIcon;
  authType: 'API Key' | 'Device Code' | 'OAuth' | 'Session Cookie';
  setup: string[];
	unavailable?: boolean;
}

const PROVIDERS: ProviderDef[] = [
 // --- Available ---
 { id: 'zai', name: 'Z.AI', icon: Sparkles, authType: 'API Key',
    setup: [
      'Go to open.bigmodel.cn and sign up',
      'Navigate to API Keys in your dashboard',
      'Create a new API key and copy it',
      'Paste the key in the connect dialog',
    ] },
 { id: 'claude-oauth', name: 'Claude (OAuth)', icon: Bot, authType: 'OAuth',
    setup: [
      'Zero-config - uses Claude Code CLI Client ID',
      'Click Connect - browser opens Claude login',
      'Sign in with your Claude account',
      'Token works with api.anthropic.com/v1/messages',
      'Ref: github.com/anthropics/claude-code',
    ] },
 { id: 'gemini-oauth', name: 'Gemini (OAuth)', icon: Sparkles, authType: 'OAuth', unavailable: true,
    setup: [
      'Zero-config - uses bundled Google OAuth Client ID',
      'Click Connect - browser opens Google login',
      'Sign in with your Google account',
      'Routes through Code Assist proxy (cloudcode-pa.googleapis.com)',
      'Token auto-refreshes every 30 minutes',
      'Ref: github.com/google-gemini/gemini-cli',
    ] },
 { id: 'kimi', name: 'Kimi', icon: Sparkles, authType: 'API Key', unavailable: true,
    setup: [
      'Go to platform.moonshot.cn and sign up',
      'Navigate to API Keys in your dashboard',
      'Create a new API key and copy it',
      'Paste the key in the connect dialog',
    ] },
 { id: 'lotuss', name: 'Lotuss', icon: Globe, authType: 'API Key',
    setup: [
      'OpenAI-compatible endpoint at llm.internal/custom/llm',
      'Get API key from Lotuss admin',
      'Use model prefix "lotus-" (e.g., lotus-sonnet)',
      'Model is overridden to "default" automatically',
      'max_tokens is clamped to 14000',
    ] },
 // --- Unavailable ---
 { id: 'anthropic', name: 'Anthropic', icon: Bot, authType: 'API Key', unavailable: true,
    setup: [
      'Go to console.anthropic.com and sign up',
      'Navigate to API Keys section',
      'Create a new API key and copy it',
      'Paste the key in the connect dialog',
    ] },
 { id: 'openai', name: 'OpenAI', icon: Zap, authType: 'API Key', unavailable: true,
    setup: [
      'Go to platform.openai.com and sign up',
      'Navigate to API Keys in settings',
      'Create a new secret key and copy it',
      'Paste the key in the connect dialog',
    ] },
 { id: 'gemini', name: 'Gemini', icon: Sparkles, authType: 'API Key', unavailable: true,
    setup: [
      'Go to aistudio.google.com/apikey',
      'Sign in with your Google account',
      'Click "Create API Key"',
      'Copy the API key and paste it in the connect dialog',
      'Free tier: 15 RPM, 1M tokens/min',
    ] },
 { id: 'openrouter', name: 'OpenRouter', icon: Globe, authType: 'API Key', unavailable: true,
    setup: [
      'Go to openrouter.ai and sign up',
      'Navigate to API Keys in your dashboard',
      'Create a new API key and copy it',
      'Paste the key in the connect dialog',
      'Supports 200+ models: Claude, GPT, Gemini, Llama, and more',
      'Free tier models available',
    ] },
 { id: 'copilot', name: 'GitHub Copilot', icon: Github, authType: 'Device Code', unavailable: true,
    setup: [
      'Click Connect to start the device code flow',
      'A user code will be displayed - copy it',
      'Open github.com/login/device in your browser',
      'Paste the code and authorize the application',
      'The token will be automatically obtained',
      'Requires an active GitHub Copilot subscription',
    ] },
 { id: 'deepseek', name: 'DeepSeek', icon: Brain, authType: 'API Key', unavailable: true,
    setup: [
      'Go to platform.deepseek.com and sign up',
      'Navigate to API Keys in your dashboard',
      'Create a new API key and copy it',
      'Paste the key in the connect dialog',
    ] },
 { id: 'huggingface', name: 'HuggingFace', icon: Cpu, authType: 'API Key', unavailable: true,
    setup: [
      'Go to huggingface.co and sign up',
      'Navigate to Settings > Access Tokens',
      'Create a new token with read/write access',
      'Paste the token in the connect dialog',
    ] },
 { id: 'ollama', name: 'Ollama', icon: Server, authType: 'API Key', unavailable: true,
    setup: [
      'Ensure Ollama is running locally (ollama serve)',
      'Default endpoint: http://localhost:11434',
      'No API key required for local usage',
      'Paste any value or leave blank for local setups',
    ] },
 { id: 'agy', name: 'AGY', icon: Blocks, authType: 'API Key', unavailable: true,
    setup: [
      'Go to your AGY provider dashboard',
      'Generate an API key',
      'Paste the key in the connect dialog',
    ] },
 { id: 'cursor', name: 'Cursor', icon: Code, authType: 'API Key', unavailable: true,
    setup: [
      'Go to cursor.sh and sign up',
      'Navigate to Settings > API Keys',
      'Create a new API key and copy it',
      'Paste the key in the connect dialog',
    ] },
 { id: 'codebuddy', name: 'CodeBuddy', icon: Terminal, authType: 'API Key', unavailable: true,
    setup: [
      'Go to your CodeBuddy provider dashboard',
      'Navigate to API Keys section',
      'Create a new API key and copy it',
      'Paste the key in the connect dialog',
    ] },
 { id: 'kilo', name: 'Kilo', icon: Coffee, authType: 'API Key', unavailable: true,
    setup: [
      'Go to your Kilo provider dashboard',
      'Navigate to API Keys section',
      'Create a new API key and copy it',
      'Paste the key in the connect dialog',
    ] },
 { id: 'qwen', name: 'Qwen (Aliyun)', icon: Brain, authType: 'API Key', unavailable: true,
    setup: [
      'Go to dashscope.aliyun.com and sign up',
      'Navigate to API Keys in your dashboard',
      'Create a new API key and copy it',
      'Paste the key in the connect dialog',
    ] },
];

const AUTH_TYPE_STYLES: Record<string, string> = {
  'API Key': 'bg-amber-500/10 text-amber-500',
  'Device Code': 'bg-info/10 text-info',
  'OAuth': 'bg-brand-primary/10 text-brand-primary',
  'Session Cookie': 'bg-brand-coral/10 text-brand-coral',
};

import { useWSRefresh } from '@/hooks/use-ws-refresh';

export default function ProvidersPage() {
  const { glmMode } = useDashboard();
  const [accountsMap, setAccountsMap] = useState<Record<string, authApi.AccountInfo[]>>({});
  const [ratelimits, setRatelimits] = useState<RateLimitStatus[]>([]);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const abortRef = useRef<AbortController | null>(null);
  const [apiKeyDialogProvider, setApiKeyDialogProvider] = useState<ProviderDef | null>(null);
  const [upstreamMap, setUpstreamMap] = useState<Record<string, string>>({});
  const [editingUpstream, setEditingUpstream] = useState<string | null>(null);
  const [editUpstreamVal, setEditUpstreamVal] = useState('');
  const [showCustomDialog, setShowCustomDialog] = useState(false);
  const [customName, setCustomName] = useState('');
  const [customUpstream, setCustomUpstream] = useState('');
  const [customFormat, setCustomFormat] = useState('openai');
  const [customApiKey, setCustomApiKey] = useState('');
 const [customModels, setCustomModels] = useState('');
  const [customLoading, setCustomLoading] = useState(false);
  const [customProviders, setCustomProviders] = useState<ProviderInfo[]>([]);
  const [editingCustomId, setEditingCustomId] = useState<string | null>(null);
  const [editName, setEditName] = useState('');
  const [editUpstream, setEditUpstream] = useState('');
  const [editModels, setEditModels] = useState('');

  const authFlow = useAuthFlow();

  const loadAccounts = useCallback(async () => {
    try {
      const [all, rls, provs] = await Promise.all([authApi.listAccounts(), authApi.fetchRateLimits(), fetchProviders()]);
      const map: Record<string, authApi.AccountInfo[]> = {};
      for (const acct of all) {
        (map[acct.provider] ??= []).push(acct);
      }
      setAccountsMap(map);
      setRatelimits(rls);
      const um: Record<string, string> = {};
      for (const p of provs) {
        if (p.upstreamBase) um[p.id] = p.upstreamBase;
      }
      setUpstreamMap(um);
      setCustomProviders(provs.filter((p) => p.id.startsWith('custom-')));
    } catch {
      // accounts endpoint may not exist yet
    } finally {
      setLoading(false);
    }
  }, []);

  useWSRefresh('ratelimit-updated', loadAccounts);

  useEffect(() => {
    loadAccounts();
    const timer = setInterval(loadAccounts, 30_000);
    return () => clearInterval(timer);
  }, [loadAccounts]);

  useEffect(() => {
    if (authFlow.completed) {
      setTimeout(() => loadAccounts(), 500);
      const timer = setTimeout(() => authFlow.reset(), 2000);
      return () => clearTimeout(timer);
    }
  }, [authFlow.completed, loadAccounts, authFlow.reset]);

  const handleAction = async (id: string, fn: () => Promise<void>) => {
    abortRef.current?.abort();
    const ac = new AbortController();
    abortRef.current = ac;
    const timeout = setTimeout(() => ac.abort(), 15_000);

    setActionLoading(id);
    try {
      await fn();
      if (ac.signal.aborted) return;
      await loadAccounts();
    } catch (e: any) {
      if (e?.name !== 'AbortError') {
        toast.error('Action failed', { description: e?.message || 'Unknown error' });
      } else {
        toast.error('Action timed out', { description: 'Request took too long. Check backend logs.' });
      }
    } finally {
      clearTimeout(timeout);
      setActionLoading(null);
    }
  };

  const handleConnect = (provider: ProviderDef) => {
    if (provider.authType === 'API Key') {
      setApiKeyDialogProvider(provider);
      return;
    }

    setExpanded(provider.id);
    authFlow.startAuth(provider.id);
  };

  const handleApiKeySubmit = async (providerId: string, apiKey: string) => {
    await authApi.registerAPIKey(providerId, apiKey);
    await loadAccounts();
  };

  const handleCreateCustom = async () => {
    setCustomLoading(true);
    try {
      await registerCustomProvider({
        name: customName.trim(),
        format: customFormat,
        upstream: customUpstream.trim(),
        apiKey: customApiKey || undefined,
        models: customModels.split(",").map((s: string) => s.trim()).filter(Boolean) || undefined,
      });
      setShowCustomDialog(false);
      setCustomName('');
      setCustomUpstream('');
      setCustomFormat('openai');
      setCustomApiKey('');
      setCustomModels('');
      await loadAccounts();
    } finally {
      setCustomLoading(false);
    }
  };

  const handleDeleteCustom = async (id: string) => {
    await deleteCustomProvider(id);
    await loadAccounts();
  };

  const handleEditCustom = (cp: ProviderInfo) => {
    setEditingCustomId(cp.id);
    setEditName(cp.name);
    setEditUpstream(cp.upstreamBase || '');
    setEditModels((cp as any).models?.join(', ') || '');
  };

  const handleSaveEdit = async () => {
    if (!editingCustomId) return;
    await updateCustomProvider(editingCustomId, {
      name: editName.trim() || undefined,
      upstream: editUpstream.trim() || undefined,
      models: editModels.split(',').map((s: string) => s.trim()).filter(Boolean) || undefined,
    });
    setEditingCustomId(null);
    await loadAccounts();
  };


  const authCodeStatus = authFlow.flowType === 'auth_code'
    ? authFlow.completed
      ? 'complete'
      : authFlow.error
        ? 'error'
        : 'waiting'
    : 'waiting';

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Providers</h1>
        <Button size="sm" variant="outline" onClick={() => setShowCustomDialog(true)}>
          <Plus className="h-4 w-4 mr-1" /> Custom
        </Button>
      </div>

      {showCustomDialog && (
        <Card>
          <CardHeader><CardTitle className="text-base">Add Custom Provider</CardTitle></CardHeader>
          <CardContent className="space-y-3">
            <div>
              <label className="text-xs text-muted-foreground">Name *</label>
              <Input value={customName} onChange={(e) => setCustomName(e.target.value)} placeholder="My LLM" />
            </div>
            <div>
              <label className="text-xs text-muted-foreground">Upstream URL *</label>
              <Input value={customUpstream} onChange={(e) => setCustomUpstream(e.target.value)} placeholder="https://api.example.com/v1" />
            </div>
            <div>
              <label className="text-xs text-muted-foreground">Format</label>
              <select className="w-full h-9 rounded-md border bg-background px-3 text-sm" value={customFormat} onChange={(e) => setCustomFormat(e.target.value)}>
                <option value="openai">OpenAI-compatible</option>
                <option value="anthropic">Anthropic-compatible</option>
              </select>
            </div>
            <div>
              <label className="text-xs text-muted-foreground">API Key</label>
              <Input type="password" value={customApiKey} onChange={(e) => setCustomApiKey(e.target.value)} placeholder="sk-..." />
            </div>
       <div>
        <label className="text-xs text-muted-foreground">Models (comma-separated)</label>
        <Input value={customModels} onChange={(e) => setCustomModels(e.target.value)} placeholder="llama3, mistral, ..." className="h-7 text-xs" />
       </div>
            <div className="flex gap-2 justify-end">
              <Button size="sm" variant="ghost" onClick={() => setShowCustomDialog(false)}>
                <X className="h-4 w-4 mr-1" /> Cancel
              </Button>
              <Button size="sm" onClick={handleCreateCustom} disabled={!customName.trim() || !customUpstream.trim() || customLoading}>
                {customLoading ? <Loader2 className="h-4 w-4 mr-1 animate-spin" /> : <Check className="h-4 w-4 mr-1" />}
                Create
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      {loading ? (
        <div className="flex items-center gap-2 text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
          Loading providers...
        </div>
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {PROVIDERS.filter((p) => glmMode || p.id !== 'zai').map((provider) => {
            const accounts = accountsMap[provider.id] ?? [];
            const isExpanded = expanded === provider.id;
            const Icon = provider.icon;

 return (
 <Card
 key={provider.id}
 className={cn(
 'transition-all duration-200 border-transparent',
 provider.unavailable && 'opacity-50 cursor-not-allowed',
 !provider.unavailable && (isExpanded ? 'border-border' : 'hover:border-border hover:shadow-md'),
 )}
 >
 <CardHeader className="pb-3">
 <div className="flex items-center gap-3">
 <div className={cn(
 "flex items-center justify-center h-9 w-9 rounded-full shrink-0",
 provider.unavailable ? "bg-muted/50" : "bg-muted",
 )}>
 <Icon className="h-4.5 w-4.5 text-muted-foreground" />
 </div>
 <div className="flex-1 min-w-0">
 <CardTitle className="text-sm font-medium">{provider.name}</CardTitle>
 <div className="flex items-center gap-2 mt-1">
 {provider.unavailable ? (
 <Badge className="text-[10px] px-1.5 bg-gray-500/10 text-gray-400">Unavailable</Badge>
 ) : (
 <>
 <Tooltip>
 <TooltipTrigger asChild>
 <Info className="h-3.5 w-3.5 text-muted-foreground/50 hover:text-muted-foreground cursor-help" />
 </TooltipTrigger>
 <TooltipContent side="bottom" className="max-w-[260px]">
 <p className="font-medium mb-1.5">Setup Instructions</p>
 <ol className="list-decimal list-inside space-y-0.5">
 {provider.setup.map((step, i) => (
 <li key={i}>{step}</li>
 ))}
 </ol>
 </TooltipContent>
 </Tooltip>
 <Badge className={cn('text-[10px] px-1.5', AUTH_TYPE_STYLES[provider.authType])}>
 {provider.authType}
 </Badge>
 {accounts.length > 0 && (
 <span className="text-xs text-muted-foreground">
 {accounts.filter((a) => !a.paused).length}/{accounts.length} active
 </span>
 )}
 </>
 )}
 </div>
 </div>
 {!provider.unavailable && (
 <div className="flex items-center gap-2 shrink-0">
 <Button size="sm" variant="outline" onClick={() => handleConnect(provider)}>
 <Plus className="h-3.5 w-3.5" />
 {accounts.length === 0 ? 'Connect' : 'Add'}
 </Button>
 {accounts.length > 0 && (
 <Button
 size="icon"
 variant="ghost"
 className="h-7 w-7"
 onClick={() => setExpanded(isExpanded ? null : provider.id)}
 >
 {isExpanded ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
 </Button>
 )}
 </div>
 )}
 </div>
 </CardHeader>

 {isExpanded && accounts.length > 0 && (
 <CardContent>
 <span className="text-xs font-medium text-muted-foreground">Accounts</span>
 <AccountList
 provider={provider.id}
 accounts={accounts}
 ratelimits={ratelimits.filter((r) => r.provider === provider.id)}
 disabled={!!actionLoading}
 onRemove={(id) => handleAction(id, () => authApi.removeAccount(provider.id, id))}
 onPause={(id) => handleAction(id, () => authApi.pauseAccount(provider.id, id))}
 onResume={(id) => handleAction(id, () => authApi.resumeAccount(provider.id, id))}
 onSetDefault={(id) => handleAction(id, () => authApi.setDefaultAccount(provider.id, id))}
 onUpdate={loadAccounts}
 />
 {actionLoading && (
 <div className="flex items-center gap-2 mt-2 text-xs text-muted-foreground">
 <Loader2 className="h-3 w-3 animate-spin" />
 Updating...
 </div>
 )}
 {upstreamMap[provider.id] && (
 <div className="mt-3 pt-3 border-t">
 <div className="flex items-center gap-2">
 <span className="text-[10px] text-muted-foreground shrink-0">Upstream</span>
 {editingUpstream === provider.id ? (
 <div className="flex gap-1.5 flex-1">
 <Input
 value={editUpstreamVal}
 onChange={(e) => setEditUpstreamVal(e.target.value)}
 onKeyDown={async (e) => {
 if (e.key === 'Enter' && editUpstreamVal.trim()) {
 await updateProviderUpstream(provider.id, editUpstreamVal.trim());
 setUpstreamMap((prev) => ({ ...prev, [provider.id]: editUpstreamVal.trim() }));
 setEditingUpstream(null);
 } else if (e.key === 'Escape') {
 setEditingUpstream(null);
 }
 }}
 className="h-6 text-xs flex-1"
 autoFocus
 />
 <Button size="sm" variant="ghost" className="h-6 px-2 text-xs"
 onClick={async () => {
 await updateProviderUpstream(provider.id, editUpstreamVal.trim());
 setUpstreamMap((prev) => ({ ...prev, [provider.id]: editUpstreamVal.trim() }));
 setEditingUpstream(null);
 }}>
 Save
 </Button>
 </div>
 ) : (
 <>
 <span className="text-xs font-mono text-muted-foreground truncate flex-1">
 {upstreamMap[provider.id]}
 </span>
 <button
 onClick={() => { setEditingUpstream(provider.id); setEditUpstreamVal(upstreamMap[provider.id] ?? ''); }}
 className="shrink-0 p-1 rounded hover:bg-muted transition-colors text-muted-foreground/30 hover:text-muted-foreground"
 title="Edit upstream URL"
 >
 <Pencil className="h-3 w-3" />
 </button>
 </>
 )}
 </div>
 </div>
 )}
 </CardContent>
 )}
 </Card>
 );
          })}
        </div>
      )}

      {customProviders.length > 0 && (
        <div className="space-y-3">
          <h2 className="text-sm font-medium text-muted-foreground">Custom Providers</h2>
          <div className="grid gap-4 md:grid-cols-2">
            {customProviders.map((cp) => {
              const accounts = accountsMap[cp.id] ?? [];
              const isExpanded = expanded === cp.id;
              return (
                <Card key={cp.id} className={cn('transition-all duration-200 border-transparent', isExpanded ? 'border-border' : 'hover:border-border hover:shadow-md')}>
                  <CardHeader className="pb-3">
                    <div className="flex items-center gap-3">
                      <div className="flex items-center justify-center h-9 w-9 rounded-full bg-muted shrink-0">
                        <Globe className="h-4.5 w-4.5 text-muted-foreground" />
                      </div>
                      <div className="flex-1 min-w-0">
                        {editingCustomId === cp.id ? (
                          <div className="space-y-1.5">
                            <Input value={editName} onChange={(e) => setEditName(e.target.value)} placeholder="Name" className="h-7 text-xs" />
                            <Input value={editUpstream} onChange={(e) => setEditUpstream(e.target.value)} placeholder="Upstream URL" className="h-7 text-xs" />
                            <Input value={editModels} onChange={(e) => setEditModels(e.target.value)} placeholder="Models (comma separated)" className="h-7 text-xs" />
                          </div>
                        ) : (
                          <>
                            <CardTitle className="text-sm font-medium">{cp.name}</CardTitle>
                            <div className="flex items-center gap-2 mt-1">
                              <Badge className="text-[10px] px-1.5 bg-cyan-500/10 text-cyan-500">Custom</Badge>
                              {(cp as any).models?.length > 0 && (
                                <span className="text-[10px] text-muted-foreground">{(cp as any).models.join(', ')}</span>
                              )}
                              {accounts.length > 0 && (
                                <span className="text-xs text-muted-foreground">{accounts.filter((a) => !a.paused).length}/{accounts.length} active</span>
                              )}
                            </div>
                          </>
                        )}
                      </div>
                      <div className="flex items-center gap-1 shrink-0">
                        {editingCustomId === cp.id ? (
                          <>
                            <Button size="icon" variant="ghost" className="h-7 w-7 text-green-600 hover:text-green-600" onClick={handleSaveEdit} title="Save">
                              <Check className="h-3.5 w-3.5" />
                            </Button>
                            <Button size="icon" variant="ghost" className="h-7 w-7" onClick={() => setEditingCustomId(null)} title="Cancel">
                              <X className="h-3.5 w-3.5" />
                            </Button>
                          </>
                        ) : (
                          <>
                            <Button size="sm" variant="outline" onClick={() => { setApiKeyDialogProvider({ id: cp.id, name: cp.name, icon: Globe, authType: 'API Key', setup: [] }); }}>
                              <Plus className="h-3.5 w-3.5" /> {accounts.length === 0 ? 'Connect' : 'Add'}
                            </Button>
                            <Button size="icon" variant="ghost" className="h-7 w-7" onClick={() => handleEditCustom(cp)} title="Edit provider">
                              <Pencil className="h-3.5 w-3.5" />
                            </Button>
                            {accounts.length > 0 && (
                              <Button size="icon" variant="ghost" className="h-7 w-7" onClick={() => setExpanded(isExpanded ? null : cp.id)}>
                                {isExpanded ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
                              </Button>
                            )}
                            <Button size="icon" variant="ghost" className="h-7 w-7 text-destructive hover:text-destructive" onClick={() => handleDeleteCustom(cp.id)} title="Delete provider">
                              <Trash2 className="h-3.5 w-3.5" />
                            </Button>
                          </>
                        )}
                      </div>
                    </div>
                  </CardHeader>
                  {isExpanded && accounts.length > 0 && (
                    <CardContent>
                      <div className="flex items-center justify-between mb-3">
                        <span className="text-xs font-medium text-muted-foreground">Accounts</span>
                      </div>
                      <AccountList
                        provider={cp.id}
                        accounts={accounts}
                        ratelimits={ratelimits.filter((r) => r.provider === cp.id)}
                        onRemove={(id) => handleAction(id, () => authApi.removeAccount(cp.id, id))}
                        onPause={(id) => handleAction(id, () => authApi.pauseAccount(cp.id, id))}
                        onResume={(id) => handleAction(id, () => authApi.resumeAccount(cp.id, id))}
                        onSetDefault={(id) => handleAction(id, () => authApi.setDefaultAccount(cp.id, id))}
                        onUpdate={loadAccounts}
                      />
                    </CardContent>
                  )}
                </Card>
              );
            })}
          </div>
        </div>
      )}

      {/* API Key Dialog */}
      {apiKeyDialogProvider && (
        <ApiKeyDialog
          open={!!apiKeyDialogProvider}
          onClose={() => setApiKeyDialogProvider(null)}
          provider={apiKeyDialogProvider.id}
          providerName={apiKeyDialogProvider.name}
          onSubmit={(key) => handleApiKeySubmit(apiKeyDialogProvider.id, key)}
        />
      )}



      {/* Device Code Dialog */}
      <DeviceCodeDialog
        open={authFlow.flowType === 'device_code' && (authFlow.isAuthenticating || !!authFlow.error)}
        onClose={() => authFlow.cancelAuth()}
        userCode={authFlow.userCode ?? ''}
        verificationUrl={authFlow.verificationUrl ?? ''}
        provider={authFlow.provider ?? ''}
        expiresInSeconds={300}
        error={authFlow.error ?? undefined}
      />

      {/* Auth Code Dialog */}
      <AuthCodeDialog
        open={authFlow.flowType === 'auth_code' && (authFlow.isAuthenticating || authFlow.completed || !!authFlow.error)}
        onClose={() => authFlow.cancelAuth()}
        provider={authFlow.provider ?? ''}
        authUrl={authFlow.authUrl ?? ''}
        status={authCodeStatus}
        error={authFlow.error ?? undefined}
        needsEmail={authFlow.needsEmail}
        onSubmitCallback={(url) => { authFlow.submitCallback(authFlow.provider ?? '', url); setTimeout(loadAccounts, 1000); }}
        onSubmitEmail={(email) => authFlow.submitEmail(email)}
      />
    </div>
  );
}
