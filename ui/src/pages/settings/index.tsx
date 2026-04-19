import { useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Separator } from '@/components/ui/separator';
import { useLanguage } from '@/contexts/language-context';
import { Settings, Globe, Bell, RotateCcw, Info } from 'lucide-react';

const STORAGE_PREFIX = 'arl-';

function getSetting(key: string, fallback: string): string {
  return localStorage.getItem(`${STORAGE_PREFIX}${key}`) || fallback;
}

function setSetting(key: string, value: string): void {
  localStorage.setItem(`${STORAGE_PREFIX}${key}`, value);
}

function getBool(key: string, fallback: boolean): boolean {
  const v = localStorage.getItem(`${STORAGE_PREFIX}${key}`);
  if (v === null) return fallback;
  return v === 'true';
}

function setBool(key: string, value: boolean): void {
  localStorage.setItem(`${STORAGE_PREFIX}${key}`, String(value));
}

const NOTIFICATION_KEYS = [
  { key: 'key-cooldown', label: 'Key cooldown events' },
  { key: 'override', label: 'Override changes' },
  { key: 'anomaly', label: 'Anomaly alerts' },
  { key: 'connection', label: 'Connection status' },
  { key: 'oauth', label: 'OAuth events' },
  { key: 'token-refresh', label: 'Token refresh' },
];

function CheckboxSetting({ storageKey, label }: { storageKey: string; label: string }) {
  const [checked, setChecked] = useState(() => getBool(storageKey, false));

  function toggle() {
    const next = !checked;
    setChecked(next);
    setBool(storageKey, next);
  }

  return (
    <label className="flex items-center gap-3 cursor-pointer py-1.5">
      <button
        role="checkbox"
        aria-checked={checked}
        onClick={toggle}
        className={`h-4 w-4 rounded border shrink-0 flex items-center justify-center transition-colors ${
          checked
            ? 'bg-primary border-primary text-primary-foreground'
            : 'border-muted-foreground/40 hover:border-muted-foreground'
        }`}
      >
        {checked && <svg className="h-3 w-3" viewBox="0 0 12 12" fill="none"><path d="M2.5 6L5 8.5L9.5 3.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" /></svg>}
      </button>
      <span className="text-sm">{label}</span>
    </label>
  );
}

import { useState } from 'react';

export default function SettingsPage() {
  const navigate = useNavigate();
  const { lang, setLang, t } = useLanguage();

  const [pollingInterval, setPollingInterval] = useState(() => getSetting('polling-interval', '10s'));
  const [theme, setTheme] = useState(() => getSetting('default-theme', 'dark'));
  const [historyRetention, setHistoryRetention] = useState(() => getSetting('history-retention', '5min'));

  useEffect(() => {
    const handler = () => navigate('/settings');
    window.addEventListener('arl:open-settings', handler);
    return () => window.removeEventListener('arl:open-settings', handler);
  }, [navigate]);

  function handlePollingChange(v: string) {
    setPollingInterval(v);
    setSetting('polling-interval', v);
  }

  function handleThemeChange(v: string) {
    setTheme(v);
    setSetting('default-theme', v);
    if (v === 'system') {
      const dark = window.matchMedia('(prefers-color-scheme: dark)').matches;
      document.documentElement.classList.toggle('dark', dark);
      localStorage.setItem('theme', dark ? 'dark' : 'light');
    } else {
      document.documentElement.classList.toggle('dark', v === 'dark');
      localStorage.setItem('theme', v);
    }
  }

  function handleRetentionChange(v: string) {
    setHistoryRetention(v);
    setSetting('history-retention', v);
  }

  function resetAll() {
    const keys = Object.keys(localStorage).filter((k) => k.startsWith(STORAGE_PREFIX));
    keys.forEach((k) => localStorage.removeItem(k));
    setPollingInterval('10s');
    setTheme('dark');
    setHistoryRetention('5min');
    setLang('en');
    document.documentElement.classList.add('dark');
    localStorage.setItem('theme', 'dark');
  }

  return (
    <div className="space-y-6 p-6">
      <div>
        <h1 className="text-2xl font-bold flex items-center gap-2">
          <Settings className="h-6 w-6" />
          {t('settings.title')}
        </h1>
        <p className="text-sm text-muted-foreground mt-1">Configure dashboard preferences</p>
      </div>

      {/* General */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">{t('settings.general')}</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between">
            <label className="text-sm">{t('settings.polling_interval')}</label>
            <Select value={pollingInterval} onValueChange={handlePollingChange}>
              <SelectTrigger className="w-32">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="5s">5s</SelectItem>
                <SelectItem value="10s">10s</SelectItem>
                <SelectItem value="30s">30s</SelectItem>
                <SelectItem value="60s">60s</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <Separator />
          <div className="flex items-center justify-between">
            <label className="text-sm">{t('settings.theme')}</label>
            <Select value={theme} onValueChange={handleThemeChange}>
              <SelectTrigger className="w-32">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="dark">{t('common.dark')}</SelectItem>
                <SelectItem value="light">{t('common.light')}</SelectItem>
                <SelectItem value="system">System</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <Separator />
          <div className="flex items-center justify-between">
            <label className="text-sm">History Retention</label>
            <Select value={historyRetention} onValueChange={handleRetentionChange}>
              <SelectTrigger className="w-32">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="2min">2 min</SelectItem>
                <SelectItem value="5min">5 min</SelectItem>
                <SelectItem value="10min">10 min</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </CardContent>
      </Card>

      {/* Notifications */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base flex items-center gap-2">
            <Bell className="h-4 w-4" />
            {t('settings.notifications')}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {NOTIFICATION_KEYS.map((n) => (
            <CheckboxSetting key={n.key} storageKey={`notify-${n.key}`} label={n.label} />
          ))}
        </CardContent>
      </Card>

      {/* Language */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base flex items-center gap-2">
            <Globe className="h-4 w-4" />
            {t('settings.language')}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-between">
            <label className="text-sm">{t('settings.language')}</label>
            <Select value={lang} onValueChange={(v) => setLang(v as 'en' | 'th')}>
              <SelectTrigger className="w-32">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="en">English</SelectItem>
                <SelectItem value="th">Thai</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </CardContent>
      </Card>

      <ServerConfigSection />

      {/* About */}
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base flex items-center gap-2">
            <Info className="h-4 w-4" />
            {t('settings.about')}
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="text-sm text-muted-foreground">
            ARL Dashboard v1.0.0
          </div>
          <Separator />
          <Button variant="destructive" size="sm" onClick={resetAll} className="flex items-center gap-2">
            <RotateCcw className="h-4 w-4" />
            {t('settings.reset')}
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}

function ServerConfigSection() {
  const [config, setConfig] = useState<Record<string, unknown> | null>(null);
  const [thinking, setThinking] = useState<Record<string, unknown> | null>(null);
  const [globalEnv, setGlobalEnv] = useState<{enabled: boolean; env: Record<string, string>} | null>(null);

  useEffect(() => {
    fetch('/v1/config').then((r) => r.ok ? r.json() : null).then(setConfig).catch(() => {});
    fetch('/v1/thinking').then((r) => r.ok ? r.json() : null).then(setThinking).catch(() => {});
    fetch('/v1/global-env').then((r) => r.ok ? r.json() : null).then(setGlobalEnv).catch(() => {});
  }, []);

  return (
    <>
      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base flex items-center gap-2">
            <Settings className="h-4 w-4" />
            Server Config
          </CardTitle>
        </CardHeader>
        <CardContent>
          {config ? (
            <pre className="text-xs bg-muted/50 rounded-md p-3 overflow-auto max-h-64 font-mono">
              {JSON.stringify(config, null, 2)}
            </pre>
          ) : (
            <div className="text-sm text-muted-foreground">Loading server config...</div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">Thinking Configuration</CardTitle>
        </CardHeader>
        <CardContent>
          {thinking ? (
            <pre className="text-xs bg-muted/50 rounded-md p-3 overflow-auto max-h-40 font-mono">
              {JSON.stringify(thinking, null, 2)}
            </pre>
          ) : (
            <div className="text-sm text-muted-foreground">Loading thinking config...</div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base flex items-center justify-between">
            Global Environment
            {globalEnv && (
              <Badge variant={globalEnv.enabled ? 'default' : 'secondary'} className="text-xs">
                {globalEnv.enabled ? 'Enabled' : 'Disabled'}
              </Badge>
            )}
          </CardTitle>
        </CardHeader>
        <CardContent>
          {globalEnv ? (
            globalEnv.env && Object.keys(globalEnv.env).length > 0 ? (
              <div className="space-y-1">
                {Object.entries(globalEnv.env).map(([k, v]) => (
                  <div key={k} className="flex justify-between text-xs font-mono py-1 border-b last:border-0">
                    <span className="text-muted-foreground">{k}</span>
                    <span>{String(v)}</span>
                  </div>
                ))}
              </div>
            ) : (
              <div className="text-sm text-muted-foreground">No environment overrides configured.</div>
            )
          ) : (
            <div className="text-sm text-muted-foreground">Loading environment vars...</div>
          )}
        </CardContent>
      </Card>
    </>
  );
}
