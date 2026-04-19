import { useState, useEffect, useCallback } from 'react';
import * as authApi from '@/lib/auth-api';
import { useAuthFlow } from '@/hooks/use-auth-flow';
import { AccountList } from './account-list';
import { DeviceCodeDialog } from '@/components/auth/device-code-dialog';
import { AuthCodeDialog } from '@/components/auth/auth-code-dialog';
import { ApiKeyDialog } from '@/components/auth/api-key-dialog';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Bot, Sparkles, Zap, Github, Plus, ChevronDown, ChevronUp, Loader2, Globe, Info, Brain, Cpu, Server, Code, Terminal, Coffee, Blocks } from 'lucide-react';
import { Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip';
import { cn } from '@/lib/utils';
import type { LucideIcon } from 'lucide-react';

interface ProviderDef {
  id: string;
  name: string;
  icon: LucideIcon;
  authType: 'API Key' | 'Device Code' | 'OAuth' | 'Session Cookie';
  setup: string[];
}

const PROVIDERS: ProviderDef[] = [
  { id: 'zai', name: 'Z.AI', icon: Sparkles, authType: 'API Key',
    setup: [
      'Go to open.bigmodel.cn and sign up',
      'Navigate to API Keys in your dashboard',
      'Create a new API key and copy it',
      'Paste the key in the connect dialog',
    ] },
  { id: 'anthropic', name: 'Anthropic', icon: Bot, authType: 'API Key',
    setup: [
      'Go to console.anthropic.com and sign up',
      'Navigate to API Keys section',
      'Create a new API key and copy it',
      'Paste the key in the connect dialog',
    ] },
  { id: 'claude', name: 'Claude (OAuth)', icon: Bot, authType: 'OAuth',
    setup: [
      'Zero-config - uses Claude Code CLI Client ID',
      'Click Connect - browser opens Claude login',
      'Sign in with your Claude account',
      'Token works with api.anthropic.com/v1/messages',
      'Ref: github.com/anthropics/claude-code',
    ] },
  { id: 'openai', name: 'OpenAI', icon: Zap, authType: 'API Key',
    setup: [
      'Go to platform.openai.com and sign up',
      'Navigate to API Keys in settings',
      'Create a new secret key and copy it',
      'Paste the key in the connect dialog',
    ] },
  { id: 'gemini', name: 'Gemini', icon: Sparkles, authType: 'API Key',
    setup: [
      'Go to aistudio.google.com/apikey',
      'Sign in with your Google account',
      'Click "Create API Key"',
      'Copy the API key and paste it in the connect dialog',
      'Free tier: 15 RPM, 1M tokens/min',
    ] },
  { id: 'gemini-oauth', name: 'Gemini (OAuth)', icon: Sparkles, authType: 'OAuth',
    setup: [
      'Zero-config - uses bundled Google OAuth Client ID',
      'Click Connect - browser opens Google login',
      'Sign in with your Google account',
      'Routes through Code Assist proxy (cloudcode-pa.googleapis.com)',
      'Token auto-refreshes every 30 minutes',
      'Ref: github.com/google-gemini/gemini-cli',
    ] },
  { id: 'openrouter', name: 'OpenRouter', icon: Globe, authType: 'API Key',
    setup: [
      'Go to openrouter.ai and sign up',
      'Navigate to API Keys in your dashboard',
      'Create a new API key and copy it',
      'Paste the key in the connect dialog',
      'Supports 200+ models: Claude, GPT, Gemini, Llama, and more',
      'Free tier models available',
    ] },
  { id: 'copilot', name: 'GitHub Copilot', icon: Github, authType: 'Device Code',
    setup: [
      'Click Connect to start the device code flow',
      'A user code will be displayed - copy it',
      'Open github.com/login/device in your browser',
      'Paste the code and authorize the application',
      'The token will be automatically obtained',
      'Requires an active GitHub Copilot subscription',
    ] },
  { id: 'deepseek', name: 'DeepSeek', icon: Brain, authType: 'API Key',
    setup: [
      'Go to platform.deepseek.com and sign up',
      'Navigate to API Keys in your dashboard',
      'Create a new API key and copy it',
      'Paste the key in the connect dialog',
    ] },
  { id: 'kimi', name: 'Kimi', icon: Sparkles, authType: 'API Key',
    setup: [
      'Go to platform.moonshot.cn and sign up',
      'Navigate to API Keys in your dashboard',
      'Create a new API key and copy it',
      'Paste the key in the connect dialog',
    ] },
  { id: 'huggingface', name: 'HuggingFace', icon: Cpu, authType: 'API Key',
    setup: [
      'Go to huggingface.co and sign up',
      'Navigate to Settings > Access Tokens',
      'Create a new token with read/write access',
      'Paste the token in the connect dialog',
    ] },
  { id: 'ollama', name: 'Ollama', icon: Server, authType: 'API Key',
    setup: [
      'Ensure Ollama is running locally (ollama serve)',
      'Default endpoint: http://localhost:11434',
      'No API key required for local usage',
      'Paste any value or leave blank for local setups',
    ] },
  { id: 'agy', name: 'AGY', icon: Blocks, authType: 'API Key',
    setup: [
      'Go to your AGY provider dashboard',
      'Generate an API key',
      'Paste the key in the connect dialog',
    ] },
  { id: 'cursor', name: 'Cursor', icon: Code, authType: 'API Key',
    setup: [
      'Go to cursor.sh and sign up',
      'Navigate to Settings > API Keys',
      'Create a new API key and copy it',
      'Paste the key in the connect dialog',
    ] },
  { id: 'codebuddy', name: 'CodeBuddy', icon: Terminal, authType: 'API Key',
    setup: [
      'Go to your CodeBuddy provider dashboard',
      'Navigate to API Keys section',
      'Create a new API key and copy it',
      'Paste the key in the connect dialog',
    ] },
  { id: 'kilo', name: 'Kilo', icon: Coffee, authType: 'API Key',
    setup: [
      'Go to your Kilo provider dashboard',
      'Navigate to API Keys section',
      'Create a new API key and copy it',
      'Paste the key in the connect dialog',
    ] },
];

const AUTH_TYPE_STYLES: Record<string, string> = {
  'API Key': 'bg-amber-500/10 text-amber-500',
  'Device Code': 'bg-blue-500/10 text-blue-500',
  'OAuth': 'bg-green-500/10 text-green-500',
  'Session Cookie': 'bg-purple-500/10 text-purple-500',
};

export default function ProvidersPage() {
  const [accountsMap, setAccountsMap] = useState<Record<string, authApi.AccountInfo[]>>({});
  const [expanded, setExpanded] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [apiKeyDialogProvider, setApiKeyDialogProvider] = useState<ProviderDef | null>(null);

  const authFlow = useAuthFlow();

  const loadAccounts = useCallback(async () => {
    try {
      const all = await authApi.listAccounts();
      const map: Record<string, authApi.AccountInfo[]> = {};
      for (const acct of all) {
        (map[acct.provider] ??= []).push(acct);
      }
      setAccountsMap(map);
    } catch {
      // accounts endpoint may not exist yet
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadAccounts();
  }, [loadAccounts]);

  useEffect(() => {
    if (authFlow.completed) {
      loadAccounts();
      const timer = setTimeout(() => authFlow.reset(), 1500);
      return () => clearTimeout(timer);
    }
  }, [authFlow.completed, loadAccounts, authFlow.reset]);

  const handleAction = async (id: string, fn: () => Promise<void>) => {
    setActionLoading(id);
    try {
      await fn();
      await loadAccounts();
    } finally {
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


  const authCodeStatus = authFlow.flowType === 'auth_code'
    ? authFlow.completed
      ? 'complete'
      : authFlow.error
        ? 'error'
        : 'waiting'
    : 'waiting';

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">Providers</h1>

      {loading ? (
        <div className="flex items-center gap-2 text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" />
          Loading providers...
        </div>
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {PROVIDERS.map((provider) => {
            const accounts = accountsMap[provider.id] ?? [];
            const isExpanded = expanded === provider.id;
            const Icon = provider.icon;

            return (
              <Card
                key={provider.id}
                className={cn(
                  'transition-all duration-200 border-transparent',
                  isExpanded ? 'border-border' : 'hover:border-border hover:shadow-md',
                )}
              >
                <CardHeader className="pb-3">
                  <div className="flex items-center gap-3">
                    <div className="flex items-center justify-center h-9 w-9 rounded-full bg-muted shrink-0">
                      <Icon className="h-4.5 w-4.5 text-muted-foreground" />
                    </div>
                    <div className="flex-1 min-w-0">
                      <CardTitle className="text-sm font-medium">{provider.name}</CardTitle>
                        <div className="flex items-center gap-2 mt-1">
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
                      </div>
                    </div>
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
                  </div>
                </CardHeader>

                {isExpanded && accounts.length > 0 && (
                  <CardContent>
                    <div className="flex items-center justify-between mb-3">
                      <span className="text-xs font-medium text-muted-foreground">Accounts</span>
                      <Button size="sm" variant="outline" onClick={() => handleConnect(provider)}>
                        <Plus className="h-3 w-3" />
                        Add
                      </Button>
                    </div>
                    <AccountList
                      provider={provider.id}
                      accounts={accounts}
                      onRemove={(id) => handleAction(id, () => authApi.removeAccount(provider.id, id))}
                      onPause={(id) => handleAction(id, () => authApi.pauseAccount(provider.id, id))}
                      onResume={(id) => handleAction(id, () => authApi.resumeAccount(provider.id, id))}
                      onSetDefault={(id) => handleAction(id, () => authApi.setDefaultAccount(provider.id, id))}
                    />
                    {actionLoading && (
                      <div className="flex items-center gap-2 mt-2 text-xs text-muted-foreground">
                        <Loader2 className="h-3 w-3 animate-spin" />
                        Updating...
                      </div>
                    )}
                  </CardContent>
                )}
              </Card>
            );
          })}
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
        onSubmitCallback={(url) => authFlow.submitCallback(authFlow.provider ?? '', url)}
      />
    </div>
  );
}
