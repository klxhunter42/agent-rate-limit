import { Suspense, useEffect } from 'react';
import { Outlet } from 'react-router-dom';
import { SidebarProvider } from '@/components/ui/sidebar';
import { AppSidebar } from './app-sidebar';
import { Toaster } from 'sonner';
import { Skeleton } from '@/components/ui/skeleton';
import { useWebSocket } from '@/hooks/use-websocket';
import { wsEmit } from '@/lib/ws-events';

function WSBridge() {
  const { lastEvent } = useWebSocket();
  useEffect(() => {
    if (lastEvent) wsEmit(lastEvent);
  }, [lastEvent]);
  return null;
}

function PageLoader() {
  return (
    <div className="p-6 space-y-4">
      <Skeleton className="h-8 w-48" />
      <Skeleton className="h-64 w-full" />
    </div>
  );
}

export function Layout() {
  return (
    <SidebarProvider>
      <AppSidebar />
      <main className="flex-1 flex flex-col min-h-0 overflow-hidden bg-background">
        <div className="flex-1 overflow-auto">
          <div className="p-6 max-w-7xl mx-auto">
            <Suspense fallback={<PageLoader />}>
              <WSBridge />
              <Outlet />
            </Suspense>
          </div>
        </div>
      </main>
      <Toaster position="top-right" />
    </SidebarProvider>
  );
}
