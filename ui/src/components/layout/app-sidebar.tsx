import { Link, useLocation, useNavigate } from 'react-router-dom';
import {
	LayoutDashboard,
	Gauge,
	Key,
	BarChart3,
	Settings2,
	Activity,
	Moon,
	Sun,
	Shield,
	Users,
	Settings,
	FileText,
	Box,
	UserCircle,
	PieChart,
	Bug,
	LogOut,
} from 'lucide-react';
import {
	Sidebar,
	SidebarContent,
	SidebarMenu,
	SidebarMenuItem,
	SidebarMenuButton,
	SidebarGroup,
	SidebarGroupLabel,
	SidebarGroupContent,
	SidebarHeader,
	SidebarFooter,
	SidebarTrigger,
} from '@/components/ui/sidebar';
import { Button } from '@/components/ui/button';
import { useDashboard } from '@/contexts/dashboard-context';
import { useAuth } from '@/contexts/auth-context';
import { cn } from '@/lib/utils';
import { useEffect, useState } from 'react';

const GLM_ONLY_PATHS = ['/model-limits', '/key-pool', '/controls'];

const NAV_ITEMS_ALL = [
	{ path: '/', label: 'Overview', icon: LayoutDashboard, group: 'Monitoring' },
	{ path: '/system-health', label: 'Health', icon: Activity, group: 'Monitoring' },
	{ path: '/model-limits', label: 'Model Limits', icon: Gauge, group: 'Monitoring' },
	{ path: '/key-pool', label: 'Key Pool', icon: Key, group: 'Monitoring' },
	{ path: '/analytics', label: 'Analytics', icon: BarChart3, group: 'Analytics' },
	{ path: '/prometheus', label: 'Metrics', icon: BarChart3, group: 'Analytics' },
	{ path: '/controls', label: 'Controls', icon: Settings2, group: 'Management' },
	{ path: '/providers', label: 'Providers', icon: Users, group: 'Management' },
	{ path: '/profiles', label: 'Profiles', icon: UserCircle, group: 'Management' },
	{ path: '/quota', label: 'Quota', icon: PieChart, group: 'Management' },
	{ path: '/privacy', label: 'Privacy', icon: Shield, group: 'Monitoring' },
	{ path: '/models', label: 'Models', icon: Box, group: 'Monitoring' },
	{ path: '/wiretap', label: 'Wiretap', icon: Bug, group: 'System' },
	{ path: '/logs', label: 'Logs', icon: FileText, group: 'System' },
	{ path: '/settings', label: 'Settings', icon: Settings, group: 'System' },
];

export function AppSidebar() {
	const location = useLocation();
	const { lastRefresh, health, glmMode } = useDashboard();
	const { logout } = useAuth();
	const navigate = useNavigate();
	const [dark, setDark] = useState(() => {
		const stored = localStorage.getItem('theme');
		return stored ? stored === 'dark' : true;
	});

	useEffect(() => {
		document.documentElement.classList.toggle('dark', dark);
		localStorage.setItem('theme', dark ? 'dark' : 'light');
	}, [dark]);

	const navItems = glmMode
		? NAV_ITEMS_ALL
		: NAV_ITEMS_ALL.filter(i => !GLM_ONLY_PATHS.includes(i.path));
	const groups = navItems.reduce<Record<string, typeof NAV_ITEMS_ALL>>((acc, item) => {
		(acc[item.group] ??= []).push(item);
		return acc;
	}, {});

	return (
		<Sidebar collapsible="icon">
			<SidebarHeader className="h-14 flex items-center justify-center">
				<Link to="/" className="flex items-center gap-2 px-2">
					<Activity className="h-5 w-5 text-sidebar-primary shrink-0" />
					<span className="font-semibold text-sm group-data-[collapsible=icon]:hidden">
						ARL Dashboard
					</span>
				</Link>
			</SidebarHeader>

			<SidebarContent>
				{Object.entries(groups).map(([title, items]) => (
					<SidebarGroup key={title}>
						<SidebarGroupLabel>{title}</SidebarGroupLabel>
						<SidebarGroupContent>
							<SidebarMenu>
								{items.map(({ path, label, icon: Icon }) => (
									<SidebarMenuItem key={path}>
										<SidebarMenuButton
											asChild
											isActive={location.pathname === path}
											tooltip={label}
										>
											<Link to={path}>
												<Icon className="h-4 w-4" />
												<span>{label}</span>
											</Link>
										</SidebarMenuButton>
									</SidebarMenuItem>
								))}
							</SidebarMenu>
						</SidebarGroupContent>
					</SidebarGroup>
				))}
			</SidebarContent>

			<SidebarFooter className="border-t p-3">
				<div className="flex items-center gap-2 text-xs text-muted-foreground px-2 group-data-[collapsible=icon]:hidden">
					<span
						className={cn(
							'h-2 w-2 rounded-full shrink-0',
							health?.status === 'healthy' ? 'bg-green-500' : 'bg-red-500'
						)}
					/>
					{health?.status === 'healthy' ? 'Connected' : 'Disconnected'}
					{lastRefresh && (
						<span className="ml-auto">{lastRefresh.toLocaleTimeString()}</span>
					)}
				</div>
				<div className="flex items-center gap-1">
					<Button variant="ghost" size="icon" onClick={() => setDark(!dark)} className="h-8 w-8 group-data-[collapsible=icon]:hidden">
						{dark ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
					</Button>
					<Button variant="ghost" size="icon" onClick={async () => { await logout(); navigate("/login"); }} className="h-8 w-8 group-data-[collapsible=icon]:hidden" title="Logout">
						<LogOut className="h-4 w-4" />
					</Button>
					<SidebarTrigger className="h-8 w-8" />
				</div>
			</SidebarFooter>
		</Sidebar>
	);
}
