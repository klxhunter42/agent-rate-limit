import { Link, useLocation } from 'react-router-dom';
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
import { PrivacyToggle } from '@/components/shared/privacy-toggle';
import { useDashboard } from '@/contexts/dashboard-context';
import { cn } from '@/lib/utils';
import { useEffect, useState } from 'react';

const NAV_ITEMS = [
  { path: '/', label: 'Overview', icon: LayoutDashboard, group: 'Monitoring' },
  { path: '/system-health', label: 'Health', icon: Activity, group: 'Monitoring' },
  { path: '/model-limits', label: 'Model Limits', icon: Gauge, group: 'Monitoring' },
  { path: '/key-pool', label: 'Key Pool', icon: Key, group: 'Monitoring' },
  { path: '/analytics', label: 'Analytics', icon: BarChart3, group: 'Analytics' },
  { path: '/metrics', label: 'Metrics', icon: BarChart3, group: 'Analytics' },
  { path: '/controls', label: 'Controls', icon: Settings2, group: 'Management' },
  { path: '/providers', label: 'Providers', icon: Users, group: 'Management' },
  { path: '/profiles', label: 'Profiles', icon: UserCircle, group: 'Management' },
  { path: '/quota', label: 'Quota', icon: PieChart, group: 'Management' },
  { path: '/privacy', label: 'Privacy', icon: Shield, group: 'Monitoring' },
  { path: '/models', label: 'Models', icon: Box, group: 'Monitoring' },
  { path: '/logs', label: 'Logs', icon: FileText, group: 'System' },
  { path: '/settings', label: 'Settings', icon: Settings, group: 'System' },
];

export function AppSidebar() {
  const location = useLocation();
  const { lastRefresh, health } = useDashboard();
  const [dark, setDark] = useState(() => {
    const stored = localStorage.getItem('theme');
    return stored ? stored === 'dark' : true;
  });

  useEffect(() => {
    document.documentElement.classList.toggle('dark', dark);
    localStorage.setItem('theme', dark ? 'dark' : 'light');
  }, [dark]);

  const groups = NAV_ITEMS.reduce<Record<string, typeof NAV_ITEMS>>((acc, item) => {
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
          <Button variant="ghost" size="icon" onClick={() => setDark(!dark)} className="h-8 w-8">
            {dark ? <Sun className="h-4 w-4" /> : <Moon className="h-4 w-4" />}
          </Button>
          <PrivacyToggle />
          <SidebarTrigger className="h-8 w-8" />
        </div>
      </SidebarFooter>
    </Sidebar>
  );
}
