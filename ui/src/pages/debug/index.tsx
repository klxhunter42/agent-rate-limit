import { useState, useEffect, useCallback } from 'react';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { toast } from 'sonner';
import { Bug, Eye, EyeOff, ExternalLink, Activity } from 'lucide-react';
import { cn } from '@/lib/utils';

interface MitmConfig {
	enabled: boolean;
	proxy_url: string;
}

export function DebugPage() {
	const [config, setConfig] = useState<MitmConfig | null>(null);
	const [loading, setLoading] = useState(true);
	const [toggling, setToggling] = useState(false);

	const fetchConfig = useCallback(async () => {
		try {
			const res = await fetch('/v1/config/mitm');
			if (!res.ok) throw new Error(`${res.status}`);
			const data = await res.json();
			setConfig(data);
		} catch {
			setConfig(null);
		} finally {
			setLoading(false);
		}
	}, []);

	useEffect(() => {
		fetchConfig();
		const id = setInterval(fetchConfig, 5_000);
		return () => clearInterval(id);
	}, [fetchConfig]);

	const toggleMitm = async (enabled: boolean) => {
		setToggling(true);
		try {
			const res = await fetch('/v1/config/mitm', {
				method: 'PUT',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ enabled }),
			});
			if (!res.ok) throw new Error(`${res.status}`);
			const data = await res.json();
			setConfig(data);
			toast.success(enabled ? 'Interception enabled' : 'Interception disabled');
		} catch {
			toast.error('Failed to toggle mitmproxy');
		} finally {
			setToggling(false);
		}
	};

	const hasMitm = config !== null && config.proxy_url !== '';

	return (
		<div className="space-y-6">
			<h1 className="text-2xl font-bold">Wiretap</h1>

			{loading ? (
				<Card>
					<CardContent className="py-8 text-center text-muted-foreground text-sm">
						Loading...
					</CardContent>
				</Card>
			) : !hasMitm ? (
				<Card>
					<CardHeader>
						<CardTitle className="text-base flex items-center gap-2">
							<Bug className="h-4 w-4" /> MITM Proxy
						</CardTitle>
					</CardHeader>
					<CardContent>
						<div className="text-center py-8 text-muted-foreground text-sm">
							No MITM proxy URL configured.<br />
							Set <code className="bg-muted px-1.5 py-0.5 rounded text-xs">MITM_PROXY_URL</code> or{' '}
							<code className="bg-muted px-1.5 py-0.5 rounded text-xs">HTTPS_PROXY</code> in your environment.
						</div>
					</CardContent>
				</Card>
			) : (
				<>
					<Card>
						<CardHeader>
							<CardTitle className="text-base flex items-center gap-2">
								<Bug className="h-4 w-4" /> MITM Interception
							</CardTitle>
							<CardDescription>
								Toggle traffic interception in real-time. All upstream requests route through
								mitmproxy when enabled.
							</CardDescription>
						</CardHeader>
						<CardContent>
							<div className="flex gap-3">
								<Button
									variant="outline"
									className={cn(
										'flex-1 h-16 flex-col gap-1',
										config.enabled && 'border-green-500 bg-green-500/10 text-green-600 dark:text-green-400'
									)}
									disabled={toggling || config.enabled}
									onClick={() => toggleMitm(true)}
								>
									<Eye className="h-5 w-5" />
									<span className="text-sm font-medium">Intercept</span>
								</Button>
								<Button
									variant="outline"
									className={cn(
										'flex-1 h-16 flex-col gap-1',
										!config.enabled && 'border-primary bg-primary/10 text-primary'
									)}
									disabled={toggling || !config.enabled}
									onClick={() => toggleMitm(false)}
								>
									<EyeOff className="h-5 w-5" />
									<span className="text-sm font-medium">Pass-through</span>
								</Button>
							</div>

							<div className="mt-4 flex items-center gap-3 text-sm text-muted-foreground">
								<Activity className="h-4 w-4" />
								<span>Status:</span>
								<Badge variant={config.enabled ? 'default' : 'secondary'}>
									{config.enabled ? 'Intercepting' : 'Pass-through'}
								</Badge>
								<span className="ml-2 font-mono text-xs">{config.proxy_url}</span>
							</div>
						</CardContent>
					</Card>

					<Card>
						<CardHeader>
							<CardTitle className="text-base">mitmweb Console</CardTitle>
							<CardDescription>Open the mitmproxy web UI to inspect captured traffic</CardDescription>
						</CardHeader>
						<CardContent>
							<Button
								variant="outline"
								asChild
								disabled={!config.enabled}
							>
								<a href={`${window.location.origin}/mitmweb/`} target="_blank" rel="noopener">
									<ExternalLink className="h-4 w-4 mr-2" />
									Open mitmweb
								</a>
							</Button>
						</CardContent>
					</Card>
				</>
			)}
		</div>
	);
}
