import { useDashboard } from '@/contexts/dashboard-context';
import { usePrometheusMetrics } from '@/hooks/use-prometheus-metrics';
import { useEventTimeline } from '@/hooks/use-event-timeline';
import { StatCard } from '@/components/shared/stat-card';
import { QuickCommands } from '@/components/shared/quick-commands';
import { KeyFlowMonitor } from '@/components/key-flow-monitor';
import { LiveAuthMonitor } from '@/components/monitoring/auth-monitor';
import { EventTimeline } from './event-timeline';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Progress } from '@/components/ui/progress';
import { Activity, Server, Wifi, Clock, Users, UserCircle, BarChart3, ExternalLink } from 'lucide-react';
import { Link } from 'react-router-dom';
import { InfoTip } from '@/components/shared/info-tip';
import { SetupGuideModal } from '@/components/setup-guide-modal';
import { formatUptime, formatNumber } from '@/lib/format';

export function OverviewPage() {
 const { models, health, loading, glmMode, seenModels } = useDashboard();
 const { metrics } = usePrometheusMetrics();
 const { events } = useEventTimeline(metrics, models, null);

 if (loading && models.length === 0) {
 return <div className="text-muted-foreground">Loading...</div>;
 }

 const totalInFlight = models.reduce((sum, m) => sum + m.in_flight, 0);
 const totalRequests = models.reduce((sum, m) => sum + m.total_requests, 0);
 const total429s = models.reduce((sum, m) => sum + m.total_429s, 0);
 const maxGlobal = Math.max(...models.map((m) => m.max_limit), 1);
 const concurrencyPct = (totalInFlight / maxGlobal) * 100;

 return (
 <div className="space-y-6">
 <h1 className="text-2xl font-bold">Overview</h1>

 {/* Stat cards - GLM mode only */}
 {glmMode && (
 <div className="grid gap-4 md:grid-cols-4">
 <StatCard
 title="Status"
 value={health?.status === 'healthy' ? 'Healthy' : 'Unhealthy'}
 subtitle={`uptime ${formatUptime(health?.uptime_seconds ?? 0)}`}
 icon={Wifi}
 variant={health?.status === 'healthy' ? 'success' : 'error'}
 />
 <StatCard
 title="Queue Depth"
 value={String(health?.queue_depth ?? 0)}
 subtitle="pending requests"
 icon={Server}
 variant={(health?.queue_depth ?? 0) > 0 ? 'warning' : 'default'}
 />
 <StatCard
 title="Total Requests"
 value={formatNumber(totalRequests)}
 subtitle={`${formatNumber(total429s)} rate-limited`}
 icon={Activity}
 variant="default"
 />
 <StatCard
 title="Concurrency"
 value={String(totalInFlight)}
 subtitle={`/ ${maxGlobal}`}
 icon={Clock}
 variant={concurrencyPct > 80 ? 'warning' : 'default'}
 />
 </div>
 )}

 {/* Quick Access - 2 rows */}
 <div className="flex flex-col items-center gap-5 py-2">
 {/* Row 1: Setup Guide (centered above) */}
 <div className="flex justify-center">
 <SetupGuideModal />
 </div>
 {/* Row 2: Providers, Profiles, Grafana */}
 <div className="flex items-center gap-5">
 <Link to="/providers" className="group quick-access-card rounded-2xl flex flex-col items-center gap-4 px-8 py-7 bg-card hover:bg-gradient-to-b hover:from-brand-teal/10 hover:to-brand-teal/5 transition-all duration-300 hover:-translate-y-1 active:scale-[0.97]" data-qa="teal">
 <div className="flex items-center justify-center h-14 w-14 rounded-2xl bg-gradient-to-br from-brand-teal/20 to-brand-teal/10 group-hover:from-brand-teal/40 group-hover:to-brand-teal/20 transition-all duration-300 group-hover:shadow-lg group-hover:shadow-brand-teal/25">
 <Users className="h-7 w-7 text-brand-teal group-hover:text-brand-teal transition-colors" />
 </div>
 <span className="text-sm font-semibold tracking-wide">Providers</span>
 </Link>
 <Link to="/profiles" className="group quick-access-card rounded-2xl flex flex-col items-center gap-4 px-8 py-7 bg-card hover:bg-gradient-to-b hover:from-brand-gold/10 hover:to-brand-gold/5 transition-all duration-300 hover:-translate-y-1 active:scale-[0.97]" data-qa="gold">
 <div className="flex items-center justify-center h-14 w-14 rounded-2xl bg-gradient-to-br from-brand-gold/20 to-brand-gold/10 group-hover:from-brand-gold/40 group-hover:to-brand-gold/20 transition-all duration-300 group-hover:shadow-lg group-hover:shadow-brand-gold/25">
 <UserCircle className="h-7 w-7 text-brand-gold group-hover:text-brand-gold transition-colors" />
 </div>
 <span className="text-sm font-semibold tracking-wide">Profiles</span>
 </Link>
 <a href="/grafana" target="_blank" rel="noopener noreferrer" className="group quick-access-card rounded-2xl relative flex flex-col items-center gap-4 px-8 py-7 bg-card hover:bg-gradient-to-b hover:from-brand-coral/10 hover:to-brand-coral/5 transition-all duration-300 hover:-translate-y-1 active:scale-[0.97]" data-qa="coral">
 <div className="flex items-center justify-center h-14 w-14 rounded-2xl bg-gradient-to-br from-brand-coral/20 to-brand-coral/10 group-hover:from-brand-coral/40 group-hover:to-brand-coral/20 transition-all duration-300 group-hover:shadow-lg group-hover:shadow-brand-coral/25">
 <BarChart3 className="h-7 w-7 text-brand-coral group-hover:text-brand-coral transition-colors" />
 </div>
 <span className="text-sm font-semibold tracking-wide">Grafana</span>
 <ExternalLink className="absolute right-[10%] bottom-[20%] h-3.5 w-3.5 text-muted-foreground opacity-0 group-hover:opacity-60 transition-opacity" />
 </a>
 </div>
 </div>

 {glmMode && (
 <>
 {/* Global capacity + Model utilization */}
 <Card>
 <CardHeader>
 <CardTitle className="text-base flex items-center gap-1.5">
 Global Capacity
 <InfoTip text="Combined concurrency across all models. Shows how many requests are being processed simultaneously vs the maximum allowed." />
 </CardTitle>
 </CardHeader>
 <CardContent>
 <div className="space-y-4">
 <div>
 <div className="flex justify-between text-sm mb-1">
 <span>Total In-Flight</span>
 <span>{totalInFlight} / {maxGlobal}</span>
 </div>
 <Progress value={concurrencyPct} />
 </div>
 </div>
 </CardContent>
 </Card>

 {models.length > 0 && (
 <Card>
 <CardHeader>
 <CardTitle className="text-base flex items-center gap-1.5">
 Model Utilization
 <InfoTip text="Per-model concurrent request usage. 'pinned' means the limit was manually overridden via Controls page." />
 </CardTitle>
 </CardHeader>
 <CardContent>
 <div className="space-y-3">
 {models.map((m) => {
 const pct = m.limit > 0 ? (m.in_flight / m.limit) * 100 : 0;
 return (
 <div key={m.name}>
 <div className="flex justify-between text-sm mb-1">
 <span className="font-mono">{m.name}</span>
 <span className="text-muted-foreground">
 {m.in_flight} / {m.limit}
 {m.overridden && ' (pinned)'}
 </span>
 </div>
 <Progress value={pct} />
 </div>
 );
 })}
 {!glmMode && seenModels.length > 0 && (
 <div className="pt-2 text-xs text-muted-foreground">
 Seen models: {seenModels.join(', ')}
 </div>
 )}
 </div>
 </CardContent>
 </Card>
 )}

 {/* Key Flow Monitor (OAuth-style Control Center) */}
 <KeyFlowMonitor />
 </>
 )}

 {/* Live Auth Monitor */}
 <LiveAuthMonitor />

 {/* Quick Commands + Event Timeline */}
 <div className="grid gap-4 lg:grid-cols-2">
 <QuickCommands />
 <EventTimeline events={events} />
 </div>
 </div>
 );
}
