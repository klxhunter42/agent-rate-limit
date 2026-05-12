import {
 Dialog,
 DialogContent,
 DialogHeader,
 DialogTitle,
 DialogTrigger,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import {
 Users,
 KeyRound,
 UserCircle,
 BadgeCheck,
 Rocket,
 BookOpen,
 ArrowRight,
} from 'lucide-react';

const STEPS = [
 {
 num: 1,
 icon: Users,
 title: 'Create Provider',
 desc: 'Connect Claude, Anthropic, Gemini, or any OpenAI-compatible provider.',
 action: '/providers',
 btn: 'Go to Providers',
 color: 'violet',
 },
 {
 num: 2,
 icon: KeyRound,
 title: 'Grant Access',
 desc: 'Authorize via OAuth flow or enter your API key to activate the provider.',
 action: '/providers',
 btn: 'Go to Providers',
 color: 'blue',
 },
 {
 num: 3,
 icon: UserCircle,
 title: 'Create Profile',
 desc: 'Group one or more providers with failover priority routing.',
 action: '/profiles',
 btn: 'Go to Profiles',
 color: 'cyan',
 },
 {
 num: 4,
 icon: BadgeCheck,
 title: 'Generate API Key',
 desc: 'Create a scoped token for your profile to authenticate requests.',
 action: '/profiles',
 btn: 'Go to Profiles',
 color: 'emerald',
 },
 {
 num: 5,
 icon: Rocket,
 title: 'Start Using',
 desc: 'Install Claude Code CLI and configure it to use the gateway.',
 action: null,
 btn: null,
 color: 'orange',
 },
];

const colorMap: Record<string, { circle: string; glow: string; line: string; badge: string }> = {
 violet: {
 circle: 'from-violet-500 to-violet-600',
 glow: 'shadow-violet-500/30',
 line: 'bg-gradient-to-b from-violet-500 to-blue-500',
 badge: 'bg-violet-500/15 text-violet-400 border-violet-500/20',
 },
 blue: {
 circle: 'from-blue-500 to-blue-600',
 glow: 'shadow-blue-500/30',
 line: 'bg-gradient-to-b from-blue-500 to-cyan-500',
 badge: 'bg-blue-500/15 text-blue-400 border-blue-500/20',
 },
 cyan: {
 circle: 'from-cyan-500 to-cyan-600',
 glow: 'shadow-cyan-500/30',
 line: 'bg-gradient-to-b from-cyan-500 to-emerald-500',
 badge: 'bg-cyan-500/15 text-cyan-400 border-cyan-500/20',
 },
 emerald: {
 circle: 'from-emerald-500 to-emerald-600',
 glow: 'shadow-emerald-500/30',
 line: 'bg-gradient-to-b from-emerald-500 to-orange-500',
 badge: 'bg-emerald-500/15 text-emerald-400 border-emerald-500/20',
 },
 orange: {
 circle: 'from-orange-500 to-orange-600',
 glow: 'shadow-orange-500/30',
 line: '',
 badge: 'bg-orange-500/15 text-orange-400 border-orange-500/20',
 },
};

export function SetupGuideModal() {
 const baseUrl = window.location.origin;

 const handleAction = (action: string | null) => {
 if (action) window.open(action, '_blank');
 };

 return (
 <Dialog>
 <DialogTrigger asChild>
 <button className="group quick-access-guide rounded-2xl flex flex-col items-center gap-4 px-8 py-7 bg-card hover:bg-gradient-to-b hover:from-rose-500/10 hover:to-rose-500/5 border border-border/50 hover:border-rose-500/50 transition-all duration-300 hover:-translate-y-1 active:scale-[0.97] cursor-pointer">
 <div className="flex items-center justify-center h-14 w-14 rounded-2xl bg-gradient-to-br from-rose-500/20 to-rose-600/10 group-hover:from-rose-500/40 group-hover:to-rose-600/20 transition-all duration-300 group-hover:shadow-lg group-hover:shadow-rose-500/25">
 <BookOpen className="h-7 w-7 text-rose-400 group-hover:text-rose-300 transition-colors" />
 </div>
 <span className="text-sm font-semibold tracking-wide">Setup</span>
 </button>
 </DialogTrigger>
 <DialogContent aria-describedby="setup-guide-desc" className="max-w-[40rem] p-0 overflow-y-auto max-h-[85vh] border-border/50 setup-guide-scroll">
 <div className="bg-gradient-to-b from-background to-muted/30 px-6 pt-6 pb-4">
 <DialogHeader>
 <DialogTitle className="text-2xl flex items-center gap-2">
 <Rocket className="h-6 w-6 text-orange-400" />
 Quick Start Guide
 </DialogTitle>
 </DialogHeader>
 <p id="setup-guide-desc" className="text-base text-muted-foreground mt-1">
 Get started in 5 steps. Each step links to the right page.
 </p>
 </div>

 <div className="px-6 pb-6 setup-guide-scroll-area">
 <div className="relative pt-2">
 {STEPS.map((step, i) => {
 const c = colorMap[step.color] ?? colorMap.violet!;
 const Icon = step.icon;
 const isLast = i === STEPS.length - 1;

 return (
 <div key={step.num} className="relative flex gap-4">
 {/* Vertical line + circle */}
 <div className="flex flex-col items-center">
 <div
 className={`setup-step-circle flex items-center justify-center h-10 w-10 rounded-full bg-gradient-to-br ${c.circle} shadow-lg ${c.glow} shrink-0 z-10`}
 style={{ animationDelay: `${i * 150}ms` }}
 >
 <Icon className="h-5 w-5 text-white" />
 </div>
 {!isLast && (
 <div className={`w-0.5 flex-1 min-h-[40px] setup-step-line ${c.line} opacity-30`} style={{ animationDelay: `${i * 150 + 300}ms` }} />
 )}
 </div>

 {/* Content */}
 <div className={`pb-6 flex-1 ${isLast ? 'pb-0' : ''}`}>
 <div
 className="setup-step-card rounded-xl border border-border/50 bg-card/50 backdrop-blur-sm p-4 hover:border-border transition-colors"
 style={{ animationDelay: `${i * 150}ms` }}
 >
 <div className="flex items-start justify-between gap-3">
 <div className="flex-1 min-w-0">
 <div className="flex items-center gap-2 mb-1">
 <span className={`text-xs font-mono font-bold px-1.5 py-0.5 rounded border ${c.badge}`}>
 {String(step.num).padStart(2, '0')}
 </span>
 <h4 className="font-semibold text-base">{step.title}</h4>
 </div>
 <p className="text-sm text-muted-foreground leading-relaxed">
 {step.desc}
 </p>
 </div>
 </div>

 {/* Step 5: install + config */}
 {step.num === 5 && (
 <div className="mt-3 space-y-2">
 <a href="https://docs.anthropic.com/en/docs/claude-code/quickstart" target="_blank" rel="noopener noreferrer" className="text-sm text-orange-400 hover:text-orange-300 underline underline-offset-2 inline-flex items-center gap-1">
 Install Claude Code CLI
 <ArrowRight className="h-3 w-3" />
 </a>
 <p className="text-sm text-muted-foreground">
 Config file: <code className="px-1.5 py-0.5 rounded bg-black/40 border border-border/50 text-sm">~/.claude/settings.json</code>
 </p>
 <pre className="rounded-lg bg-black/40 border border-border/50 p-3 text-sm font-mono text-muted-foreground overflow-x-auto">
{JSON.stringify({
 "env": {
 "ANTHROPIC_BASE_URL": baseUrl,
 "ANTHROPIC_API_KEY": "<your-arl-key>"
 }
}, null, 2)}
 </pre>
 </div>
 )}

 {/* Action button */}
 {step.btn && (
 <Button
 variant="ghost"
 size="sm"
 className="mt-2.5 gap-1.5 h-7 text-sm text-muted-foreground hover:text-foreground"
 onClick={() => handleAction(step.action)}
 >
 {step.btn}
 <ArrowRight className="h-3 w-3" />
 </Button>
 )}
 </div>
 </div>
 </div>
 );
 })}
 </div>
 </div>
 </DialogContent>
 </Dialog>
 );
}
