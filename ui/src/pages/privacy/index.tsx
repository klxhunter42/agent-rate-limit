import { useState, useEffect, useCallback, useRef } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, CartesianGrid, LineChart, Line, Legend } from 'recharts';
import { Shield, Eye, Fingerprint, Timer, Key, Lock, Server, CreditCard, Globe, Terminal } from 'lucide-react';
import { fetchPrivacyMetrics, type PrivacyMetrics } from '@/lib/privacy-api';
import { InfoTip } from '@/components/shared/info-tip';
import { useTimeRange } from '@/hooks/use-time-range';
import { TimeRangeFilter } from '@/components/shared/time-range-filter';
import { getPollingInterval } from '@/lib/polling';

const detectableTypes = [
  {
    category: 'Private Keys',
    icon: Key,
    items: [
      { name: 'OpenSSH Private Key', tag: 'OPENSSH_PRIVATE_KEY', description: 'OpenSSH format private key used for SSH authentication.', example: '-----BEGIN OPENSSH PRIVATE KEY-----\nMIIEvgIBADANBgk...' },
      { name: 'PEM Private Key', tag: 'PEM_PRIVATE_KEY', description: 'PEM-encoded private key (RSA, EC, DSA, generic).', example: '-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA...' },
    ],
  },
  {
    category: 'API Keys & Tokens',
    icon: Lock,
    items: [
      { name: 'Generic API Key (sk-)', tag: 'API_KEY_SK', description: 'API keys starting with sk- prefix (OpenAI, Anthropic, etc.).', example: 'sk-proj-abc123def456ghi789...' },
      { name: 'AWS Access Key', tag: 'API_KEY_AWS', description: 'AWS IAM access key ID (starts with AKIA).', example: 'AKIAIOSFODNN7EXAMPLE' },
      { name: 'GitHub Token', tag: 'API_KEY_GITHUB', description: 'GitHub personal access, OAuth, or refresh tokens.', example: 'ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx' },
      { name: 'GitLab Token', tag: 'API_KEY_GITLAB', description: 'GitLab personal access (glpat-), deploy (gldt-), CI build (glcbt-), or pipeline trigger (glptt-) tokens.', example: 'glpat-xxxxxxxxxxxxxxxxxxxx' },
      { name: 'GCP API Key', tag: 'API_KEY_GCP', description: 'Google Cloud API key (starts with AIza).', example: 'AIzaSyA...35-char-key' },
      { name: 'Tencent Cloud SecretId', tag: 'API_KEY_TENCENT', description: 'Tencent Cloud API SecretId (starts with AKID) or SecretKey assignment.', example: 'AKID_EXAMPLE_FAKE_REPLACE_0000' },
      { name: 'Alibaba Cloud AccessKey', tag: 'API_KEY_ALIBABA', description: 'Alibaba Cloud AccessKey ID (starts with LTAI).', example: 'LTAI5txxxxxxxxxx' },
      { name: 'Slack Token', tag: 'API_KEY_SLACK', description: 'Slack bot (xoxb-), user (xoxp-), or app (xoxa-) tokens.', example: 'xoxb-xxxx-xxxx-xxxx-xxxx' },
      { name: 'Stripe Secret Key', tag: 'API_KEY_STRIPE', description: 'Stripe live secret or restricted key (sk_live_/rk_live_).', example: 'sk_live_FAKE_EXAMPLE_KEY_REPLACE' },
      { name: 'SendGrid API Key', tag: 'API_KEY_SENDGRID', description: 'SendGrid API key (starts with SG.).', example: 'SG.xxxx.yyyy' },
      { name: 'Webhook URL', tag: 'WEBHOOK_URL', description: 'Webhook URLs from Slack, Discord, Teams, PagerDuty, Zapier, etc.', example: 'https://hooks.slack.com/services/T00/B00/xxxx' },
      { name: 'JWT Token', tag: 'JWT_TOKEN', description: 'JSON Web Token with 3 base64url-encoded segments.', example: 'eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abc123def456ghi789' },
      { name: 'Bearer Token', tag: 'BEARER_TOKEN', description: 'HTTP Authorization header with Bearer scheme (40+ chars).', example: 'Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...' },
    ],
  },
  {
    category: 'Environment Secrets',
    icon: Server,
    items: [
      { name: 'Password Variable', tag: 'ENV_PASSWORD', description: 'Variables containing PASSWORD, PASSWD, _PWD, or _PASS.', example: 'DB_PASSWORD=myS3cretP@ss' },
      { name: 'Secret Variable', tag: 'ENV_SECRET', description: 'Variables ending with _SECRET.', example: 'JWT_SECRET=abc123def456ghi789' },
      { name: 'Token Assignment', tag: 'ENV_TOKEN', description: 'Token or *_TOKEN variable assignments with 16+ char values.', example: 'AUTH_TOKEN=eyJhbGciOiJIUzI1NiJ9...' },
      { name: 'Credential Assignment', tag: 'ENV_CREDENTIAL', description: 'CLIENT_ID, _CID, or _ACCESS_KEY assignments.', example: 'AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE' },
      { name: 'User Variable', tag: 'ENV_USER', description: 'Variables containing USER, USERNAME, or LOGIN with 2+ char values.', example: 'DB_USERNAME=admin' },
      { name: 'Connection String', tag: 'CONNECTION_STRING', description: 'Database or message broker URLs with embedded credentials.', example: 'postgresql://user:pass@db.host:5432/mydb' },
      { name: 'Basic Auth URL', tag: 'BASIC_AUTH_URL', description: 'URLs with embedded user:password credentials.', example: 'https://admin:s3cret@api.example.com/endpoint' },
      { name: 'Vault Token', tag: 'VAULT_TOKEN', description: 'HashiCorp Vault token (starts with hvs.).', example: 'hvs.xxxxxxxxxxxxxxxxxxxxxxxxxxxx' },
      { name: 'Azure Credential', tag: 'AZURE_CREDENTIAL', description: 'Azure client secrets or tenant IDs (UUID format with Azure/AAD/Tenant context).', example: 'AZURE_CLIENT_SECRET=12345678-1234-1234-1234-123456789012' },
    ],
  },
  {
    category: 'CLI & HTTP Auth',
    icon: Terminal,
    items: [
      { name: 'CLI Auth Flag', tag: 'CLI_AUTH', description: 'Authentication flags in CLI commands (-u, -p, --password, --token, --api-key, etc.) including mysql/psql password shorthand.', example: 'mysql -u root -pS3cretP@ss mydb' },
      { name: 'curl/wget Basic Auth', tag: 'CURL_BASIC_AUTH', description: 'curl or wget basic auth via -u/--user/--username flag.', example: 'curl -u admin:s3cret https://api.example.com' },
    ],
  },
  {
    category: 'PII (Regex)',
    icon: Globe,
    items: [
      { name: 'Email Address', tag: 'EMAIL_ADDRESS', description: 'Standard email addresses.', example: 'user@example.com' },
      { name: 'Phone Number', tag: 'PHONE_NUMBER', description: 'International phone numbers in various formats.', example: '+1-555-123-4567' },
      { name: 'Credit Card Number', tag: 'CREDIT_CARD', description: 'Payment card numbers (Visa, Mastercard, Amex, Discover).', example: '4111 1111 1111 1111' },
      { name: 'US SSN', tag: 'SSN', description: 'US Social Security Number (xxx-xx-xxxx).', example: '123-45-6789' },
      { name: 'IBAN Code', tag: 'IBAN', description: 'International Bank Account Numbers.', example: 'DE89 3704 0044 0532 0130 00' },
      { name: 'IP Address', tag: 'IP_ADDRESS', description: 'IPv4 addresses.', example: '192.168.1.100' },
    ],
  },
  {
    category: 'Local PII (Regex)',
    icon: CreditCard,
    items: [
      { name: 'Thai National ID', tag: 'THAI_NATIONAL_ID', description: '13-digit Thai national identification number (starts with 1-8).', example: '1-2345-67890-12-3' },
      { name: 'Thai Phone', tag: 'THAI_PHONE', description: 'Thai phone numbers (0x-xxx-xxxx or +66x-xxx-xxxx).', example: '081-234-5678' },
    ],
  },
];

export default function PrivacyPage() {
  const [data, setData] = useState<PrivacyMetrics | null>(null);
  const [loading, setLoading] = useState(true);
  const { range, setRange } = useTimeRange('5m');
  const [error, setError] = useState<string | null>(null);
  const firstLoad = useRef(true);

  const load = useCallback(async () => {
    if (firstLoad.current) setLoading(true);
    try {
      const result = await fetchPrivacyMetrics();
      setData(result);
      setError(null);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      console.error('[privacy] fetch failed:', msg);
      setError(msg);
    } finally {
      if (firstLoad.current) {
        firstLoad.current = false;
        setLoading(false);
      }
    }
  }, []);

  const timerRef = useRef<ReturnType<typeof setInterval>>(null);

  useEffect(() => {
    load();
    const schedule = () => {
      timerRef.current = setInterval(() => {
        load();
        clearInterval(timerRef.current!);
        schedule();
      }, getPollingInterval());
    };
    schedule();
    return () => { if (timerRef.current) clearInterval(timerRef.current); };
  }, [load]);

  if (loading) return <div className="text-muted-foreground">Loading...</div>;
  if (!data) return <div className="text-red-500 text-sm">Error loading privacy metrics: {error ?? 'unknown'}</div>;

  const secretsLast24h = data.secretsDetected.reduce((s, d) => s + d.count, 0);
  const piiLast24h = data.piiDetected.reduce((s, d) => s + d.count, 0);
  const rawP95 = data.maskDuration.length > 0
    ? Math.max(...data.maskDuration.map((d) => d.p95)) * 1000
    : 0;
  const p95Display = Number.isFinite(rawP95) ? rawP95.toFixed(1) : 'N/A';

  const cards = [
    { title: 'Total Masked Requests', value: data.totalMaskedRequests.toLocaleString(), sub: 'through pipeline', icon: Shield, iconColor: 'text-blue-500' },
    { title: 'Secrets Detected', value: secretsLast24h.toLocaleString(), sub: 'by type', icon: Eye, iconColor: 'text-red-500' },
    { title: 'PII Detected', value: piiLast24h.toLocaleString(), sub: 'by type', icon: Fingerprint, iconColor: 'text-orange-500' },
    { title: 'Mask Duration p95', value: `${p95Display}ms`, sub: 'slowest phase', icon: Timer, iconColor: 'text-brand-teal' },
  ];

  const secretsChartData = data.secretsDetected.map((d) => ({
    type: d.type,
    count: d.count,
  }));

  const piiChartData = data.piiDetected.map((d) => ({
    type: d.type,
    count: d.count,
  }));

  const durationChartData = data.maskDuration.map((d) => ({
    phase: d.phase.replace(/_/g, ' '),
    p95: Number.isFinite(d.p95) ? +(d.p95 * 1000).toFixed(2) : 0,
  }));

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold flex items-center gap-1.5">Privacy <InfoTip text="PastGuard privacy pipeline masks secrets and PII before sending requests to upstream AI providers." /></h1>
        <TimeRangeFilter value={range} onChange={setRange} />
      </div>
      {error && <div className="text-red-500 text-xs bg-red-50 dark:bg-red-950 p-2 rounded">Last refresh error: {error}</div>}

      <div className="grid gap-4 md:grid-cols-4">
        {cards.map((c) => (
          <Card key={c.title}>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">{c.title}</CardTitle>
              <c.icon className={`h-4 w-4 ${c.iconColor}`} />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{c.value}</div>
              <p className="text-xs text-muted-foreground">{c.sub}</p>
            </CardContent>
          </Card>
        ))}
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Secrets by Type</CardTitle>
          </CardHeader>
          <CardContent>
            {secretsChartData.length === 0 ? (
              <div className="h-48 flex items-center justify-center text-muted-foreground text-sm">No secret detections</div>
            ) : (
              <ResponsiveContainer width="100%" height={240}>
                <BarChart data={secretsChartData}>
                  <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
                  <XAxis dataKey="type" tick={{ fontSize: 11 }} />
                  <YAxis tick={{ fontSize: 11 }} />
                  <Tooltip />
                  <Bar dataKey="count" fill="#ef4444" radius={[4, 4, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">PII by Type</CardTitle>
          </CardHeader>
          <CardContent>
            {piiChartData.length === 0 ? (
              <div className="h-48 flex items-center justify-center text-muted-foreground text-sm">No PII detections</div>
            ) : (
              <ResponsiveContainer width="100%" height={240}>
                <BarChart data={piiChartData}>
                  <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
                  <XAxis dataKey="type" tick={{ fontSize: 11 }} />
                  <YAxis tick={{ fontSize: 11 }} />
                  <Tooltip />
                  <Bar dataKey="count" fill="#f97316" radius={[4, 4, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            )}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Mask Duration by Phase (p95)</CardTitle>
        </CardHeader>
        <CardContent>
          {durationChartData.length === 0 ? (
            <div className="h-48 flex items-center justify-center text-muted-foreground text-sm">No duration data</div>
          ) : (
            <ResponsiveContainer width="100%" height={240}>
              <LineChart data={durationChartData}>
                <CartesianGrid strokeDasharray="3 3" className="stroke-muted" />
                <XAxis dataKey="phase" tick={{ fontSize: 11 }} />
                <YAxis tick={{ fontSize: 11 }} unit="ms" />
                <Tooltip />
                <Legend />
                <Line type="monotone" dataKey="p95" stroke="#8b5cf6" strokeWidth={2} name="p95 (ms)" dot={{ r: 4 }} />
              </LineChart>
            </ResponsiveContainer>
          )}
        </CardContent>
      </Card>

      <Card>
 <CardHeader>
 <CardTitle className="text-base">Detectable Types Reference</CardTitle>
 </CardHeader>
 <CardContent className="px-3 pb-3 md:px-4 md:pb-4">
 <div className="columns-1 md:columns-2 gap-3 space-y-3">
 {detectableTypes.map((group) => {
   const catColors: Record<string, string> = {
     'Private Keys': 'from-yellow-500/15 to-yellow-600/5 border-yellow-500/20 text-yellow-500',
     'API Keys & Tokens': 'from-red-500/15 to-red-600/5 border-red-500/20 text-red-400',
     'Environment Secrets': 'from-blue-500/15 to-blue-600/5 border-blue-500/20 text-blue-400',
     'CLI & HTTP Auth': 'from-emerald-500/15 to-emerald-600/5 border-emerald-500/20 text-emerald-400',
     'PII (Regex)': 'from-orange-500/15 to-orange-600/5 border-orange-500/20 text-orange-400',
     'Local PII (Regex)': 'from-pink-500/15 to-pink-600/5 border-pink-500/20 text-pink-400',
   };
   const colors = catColors[group.category] ?? 'from-muted to-muted border-border text-muted-foreground';
   return (
   <div key={group.category} className="rounded-lg border border-border/40 overflow-hidden break-inside-avoid">
     <h3 className={`text-xs font-bold px-3 py-2 flex items-center gap-2 bg-gradient-to-r ${colors} border-b border-border/30`}>
       <group.icon className="w-3.5 h-3.5" />
       {group.category}
       <span className="ml-auto text-[10px] font-normal opacity-60">{group.items.length}</span>
     </h3>
     <div className="divide-y divide-border/20">
     {group.items.map((item) => (
       <div key={item.name} className="group/item px-3 py-2 text-xs hover:bg-muted/20 transition-colors cursor-default">
         <div className="flex items-center gap-2">
           <span className="font-semibold text-foreground shrink-0">{item.name}</span>
           <code className="text-[10px] opacity-50 font-mono truncate">{item.tag}</code>
         </div>
         <p className="text-muted-foreground leading-snug mt-0.5 line-clamp-2">{item.description}</p>
         <div className="bg-black/30 rounded px-2 py-1 font-mono text-[10px] break-all text-muted-foreground/70 mt-1 leading-relaxed line-clamp-2">{item.example}</div>
       </div>
     ))}
     </div>
   </div>
   );
 })}
 </div>
 </CardContent>
 </Card>
    </div>
  );
}
