import { useState, useEffect, useCallback } from 'react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, CartesianGrid, LineChart, Line, Legend } from 'recharts';
import { Shield, Eye, Fingerprint, Timer, Key, Lock, Server, CreditCard, Globe } from 'lucide-react';
import { fetchPrivacyMetrics, type PrivacyMetrics } from '@/lib/privacy-api';
import { InfoTip } from '@/components/shared/info-tip';
import { useTimeRange } from '@/hooks/use-time-range';
import { TimeRangeFilter } from '@/components/shared/time-range-filter';

const detectableTypes = [
  {
    category: 'Private Keys',
    icon: Key,
    items: [
      { name: 'OpenSSH Private Key', tag: 'OPENSSH_PRIVATE_KEY', description: 'OpenSSH format private key used for SSH authentication.', example: '-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA...\n-----END OPENSSH PRIVATE KEY-----' },
      { name: 'PEM Private Key', tag: 'PEM_PRIVATE_KEY', description: 'PEM-encoded private key (RSA, EC, DSA, generic).', example: '-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA...\n-----END RSA PRIVATE KEY-----' },
    ],
  },
  {
    category: 'API Keys & Tokens',
    icon: Lock,
    items: [
      { name: 'Generic API Key (sk-)', tag: 'API_KEY_SK', description: 'API keys starting with sk- prefix (OpenAI, Anthropic, etc.).', example: 'sk-ant-api03-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx' },
      { name: 'AWS Access Key', tag: 'API_KEY_AWS', description: 'AWS IAM access key ID (starts with AKIA).', example: 'AKIAIOSFODNN7EXAMPLE' },
      { name: 'GitHub Token', tag: 'API_KEY_GITHUB', description: 'GitHub personal access, OAuth, or refresh tokens.', example: 'ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx' },
      { name: 'GitLab Token', tag: 'API_KEY_GITLAB', description: 'GitLab personal access (glpat-), deploy (gldt-), CI build (glcbt-), or pipeline trigger (glptt-) tokens.', example: 'glpat-xxxxxxxxxxxxxxxxxxxx' },
      { name: 'JWT Token', tag: 'JWT_TOKEN', description: 'JSON Web Token with 3 base64url-encoded segments.', example: 'eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abc123def456ghi789' },
      { name: 'Bearer Token', tag: 'BEARER_TOKEN', description: 'HTTP Authorization header with Bearer scheme (40+ chars).', example: 'Bearer abcdefghijklmnopqrstuvwxyz0123456789ABCD' },
    ],
  },
  {
    category: 'Environment Secrets',
    icon: Server,
    items: [
      { name: 'Password Variable', tag: 'ENV_PASSWORD', description: 'Environment variables or config entries containing PASSWORD or _PWD.', example: 'DB_PASSWORD=supersecretvalue123' },
      { name: 'Secret Variable', tag: 'ENV_SECRET', description: 'Environment variables ending with _SECRET.', example: 'JWT_SECRET=myjwtsecretproduction123' },
      { name: 'Connection String', tag: 'CONNECTION_STRING', description: 'Database or message broker URLs with embedded credentials.', example: 'postgres://admin:pass123@db.example.com:5432/mydb' },
    ],
  },
  {
    category: 'PII (via Presidio)',
    icon: Globe,
    items: [
      { name: 'Email Address', tag: 'EMAIL_ADDRESS', description: 'Email addresses detected by Microsoft Presidio analyzer.', example: 'user.name@example.com' },
      { name: 'Phone Number', tag: 'PHONE_NUMBER', description: 'Phone numbers in various international formats.', example: '+1 (555) 123-4567' },
      { name: 'Person Name', tag: 'PERSON', description: 'Personal names detected by NLP-based analysis.', example: 'John Smith' },
      { name: 'Credit Card Number', tag: 'CREDIT_CARD', description: 'Payment card numbers (Visa, Mastercard, Amex, etc.).', example: '4111 1111 1111 1111' },
      { name: 'IBAN Code', tag: 'IBAN_CODE', description: 'International Bank Account Numbers.', example: 'DE89370400440532013000' },
      { name: 'IP Address', tag: 'IP_ADDRESS', description: 'IPv4 and IPv6 addresses.', example: '192.168.1.100' },
    ],
  },
  {
    category: 'Local PII (Regex)',
    icon: CreditCard,
    items: [
      { name: 'Thai National ID', tag: 'THAI_NATIONAL_ID', description: '13-digit Thai national identification number (starts with 1-8). Not covered by Presidio.', example: '1100100473221' },
    ],
  },
];

export default function PrivacyPage() {
  const [data, setData] = useState<PrivacyMetrics | null>(null);
  const [loading, setLoading] = useState(true);
  const { range, setRange } = useTimeRange('5m');
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const result = await fetchPrivacyMetrics();
      setData(result);
      setError(null);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      console.error('[privacy] fetch failed:', msg);
      setError(msg);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
    const id = setInterval(load, 5000);
    return () => clearInterval(id);
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
    { title: 'Mask Duration p95', value: `${p95Display}ms`, sub: 'slowest phase', icon: Timer, iconColor: 'text-purple-500' },
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
        <CardContent>
          <div className="grid gap-4 md:grid-cols-2">
            {detectableTypes.map((group) => (
              <div key={group.category}>
                <h3 className="text-sm font-semibold mb-2 flex items-center gap-2">
                  <group.icon className="w-4 h-4" />
                  {group.category}
                </h3>
                <div className="space-y-2">
                  {group.items.map((item) => (
                    <div key={item.name} className="rounded-lg border p-3 text-xs space-y-1">
                      <div className="flex items-center justify-between">
                        <span className="font-medium">{item.name}</span>
                        <code className="text-muted-foreground bg-muted px-1.5 py-0.5 rounded">{item.tag}</code>
                      </div>
                      <div className="text-muted-foreground">{item.description}</div>
                      <div className="bg-muted/50 rounded p-2 font-mono text-[11px] break-all">{item.example}</div>
                    </div>
                  ))}
                </div>
              </div>
            ))}
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
