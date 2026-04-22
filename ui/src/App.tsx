import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { DashboardProvider } from '@/contexts/dashboard-context';
import { PrivacyProvider } from '@/contexts/privacy-context';
import { LanguageProvider } from '@/contexts/language-context';
import { AuthProvider } from '@/contexts/auth-context';
import { NotificationProvider, ToastContainer } from '@/components/shared/notifications';
import { CommandPalette, useCommandPalette } from '@/components/shared/command-palette';
import { useKeyboardShortcuts } from '@/hooks/use-keyboard-shortcuts';
import { Layout } from '@/components/layout/layout';
import { OverviewPage } from '@/pages/overview';
import { ModelLimitsPage } from '@/pages/model-limits';
import { KeyPoolPage } from '@/pages/key-pool';
import { MetricsPage } from '@/pages/metrics';
import { ControlsPage } from '@/pages/controls';
import { AnalyticsPage } from '@/pages/analytics';
import { HealthPage } from '@/pages/health';
import LoginPage from '@/pages/login';
import ProvidersPage from '@/pages/providers';
import SettingsPage from '@/pages/settings';
import PrivacyPage from '@/pages/privacy';
import { LogsPage } from '@/pages/logs';
import { ModelsPage } from '@/pages/models';
import { ProfilesPage } from '@/pages/profiles';
import { QuotaPage } from '@/pages/quota';

function AppShell({ children }: { children: React.ReactNode }) {
  const { open, close } = useCommandPalette();
  useKeyboardShortcuts();
  return (
    <>
      {children}
      <CommandPalette open={open} onClose={close} />
      <ToastContainer />
    </>
  );
}

export default function App() {
  return (
    <BrowserRouter>
      <LanguageProvider>
        <PrivacyProvider>
          <AuthProvider>
            <DashboardProvider>
              <NotificationProvider>
                <AppShell>
                  <Routes>
                    <Route path="/login" element={<LoginPage />} />
                    <Route element={<Layout />}>
                      <Route path="/" element={<OverviewPage />} />
                      <Route path="/system-health" element={<HealthPage />} />
                      <Route path="/model-limits" element={<ModelLimitsPage />} />
                      <Route path="/key-pool" element={<KeyPoolPage />} />
                      <Route path="/analytics" element={<AnalyticsPage />} />
                      <Route path="/prometheus" element={<MetricsPage />} />
                      <Route path="/controls" element={<ControlsPage />} />
                      <Route path="/privacy" element={<PrivacyPage />} />
                      <Route path="/providers" element={<ProvidersPage />} />
                      <Route path="/settings" element={<SettingsPage />} />
                      <Route path="/logs" element={<LogsPage />} />
                      <Route path="/models" element={<ModelsPage />} />
                      <Route path="/profiles" element={<ProfilesPage />} />
                      <Route path="/quota" element={<QuotaPage />} />
                    </Route>
                    <Route path="*" element={<Navigate to="/" replace />} />
                  </Routes>
                </AppShell>
              </NotificationProvider>
            </DashboardProvider>
          </AuthProvider>
        </PrivacyProvider>
      </LanguageProvider>
    </BrowserRouter>
  );
}
