# UI Dashboard - Detailed Technical Reference

This document covers the ARL Dashboard, a Vite + React 19 single-page application providing real-time monitoring, analytics, and management for the API rate limiter gateway.

> **Note:** Despite initial descriptions referencing Next.js, this is a **Vite + React SPA** with client-side rendering only (no SSR/SSG). Build output is embedded in the Go binary via `vite build`.

---

## 1 UI Architecture

### Tech Stack

| Layer           | Technology       | Version |
|-----------------|------------------|---------|
| Build           | Vite             | 7.x     |
| UI Framework    | React            | 19.x    |
| Routing         | React Router DOM | 7.x     |
| Styling         | Tailwind CSS     | 4.x     |
| Components      | Radix UI         | latest  |
| Charts          | Recharts         | 2.14    |
| Icons           | Lucide React     | latest  |
| Language        | TypeScript       | 5.9     |
| Testing         | Playwright       | latest  |
| Package Manager | bun              | latest  |

### Build Configuration (`vite.config.ts`)

- **Output directory:** `../api-gateway/static` (embedded in Go binary)
- **Sourcemaps:** disabled in production
- **Minification:** esbuild
- **Manual chunks:** react-vendor, radix-ui, icons, charts (reduces initial bundle)
- **Dev proxy:** All `/v1`, `/health`, `/api/metrics`, `/metrics` routes proxied to `arl-gateway:8080`
- **HMR:** Configurable via `VITE_HMR_HOST` / `VITE_HMR_PROTOCOL` / `VITE_HMR_PORT` env vars with polling fallback
- **Path alias:** `@/` maps to `./src/`

### TypeScript Configuration

- **Target:** ES2020
- **JSX:** react-jsx (automatic runtime)
- **Strict mode:** enabled
- **Path alias:** `@/*` via `@types/bun`

### Styling System

Tailwind CSS v4 uses CSS-based configuration (no `tailwind.config.js`). Key design tokens defined in `src/index.css`:

- **Color system:** oklch color space for perceptual uniformity
- **Dark mode:** Class-based toggle (`document.documentElement.classList.toggle('dark')`)
- **Theme persistence:** localStorage key `theme`, defaults to dark
- **Privacy blur class:** `blur-[4px] select-none hover:blur-none transition-all`

### Entry Point (`src/main.tsx`)

React 19 `createRoot` renders `<App />` into `#root` div.

---

## 2 Page Structure

### Route Map

All routes are defined in `src/App.tsx`. The login page renders outside the layout; all other pages render inside `<Layout />` which provides the sidebar.

```
/login                  LoginPage (no layout)
/                       OverviewPage
/system-health          HealthPage
/model-limits           ModelLimitsPage
/key-pool               KeyPoolPage
/analytics              AnalyticsPage
/prometheus             MetricsPage
/controls               ControlsPage
/providers              ProvidersPage
/profiles               ProfilesPage
/quota                  QuotaPage
/privacy                PrivacyPage
/models                 ModelsPage
/logs                   LogsPage
/settings               SettingsPage
*                       Redirect to /
```

### Provider Nesting Order

```
BrowserRouter
  LanguageProvider
    PrivacyProvider
      AuthProvider
        DashboardProvider
          NotificationProvider
            AppShell (CommandPalette + ToastContainer + KeyboardShortcuts)
              Routes
```

### Navigation Groups (Sidebar)

| Group      | Pages                                                     |
|------------|-----------------------------------------------------------|
| Monitoring | Overview, Health, Model Limits, Key Pool, Privacy, Models |
| Analytics  | Analytics, Metrics                                        |
| Management | Controls, Providers, Profiles, Quota                      |
| System     | Logs, Settings                                            |

---

## 3 Components

### Layout Components

#### `AppSidebar` (`src/components/layout/app-sidebar.tsx`)

Main sidebar navigation. Props: none (uses context/hooks).

- 14 nav items grouped into 4 categories
- Connection status indicator (green/red dot) from `DashboardContext.health`
- Last refresh timestamp
- Dark/light theme toggle (Sun/Moon icons)
- Privacy toggle button
- Sidebar collapse trigger
- `isActive` highlighting via `useLocation().pathname`

#### `Layout` (`src/components/layout/layout.tsx`)

Wraps all authenticated pages. Contains:
- `SidebarProvider` + `AppSidebar` + `<Outlet />`
- `WSBridge` component (forwards WebSocket events)
- `Toaster` from sonner
- `Suspense` fallback with skeleton loader

### Shared Components

#### `StatCard` (`src/components/shared/stat-card.tsx`)

Reusable metric display card.

```typescript
interface StatCardProps {
  icon: React.ElementType;
  label: string;
  value: string | number;
  subtitle?: string;
  variant?: 'default' | 'success' | 'warning' | 'error' | 'accent';
  className?: string;
}
```

Five variants map to border/background color classes: default (border), success (green), warning (amber), error (red), accent (blue).

#### `CommandPalette` (`src/components/shared/command-palette.tsx`)

Cmd+K searchable command palette.

```typescript
interface CommandPaletteProps {
  open: boolean;
  onClose: () => void;
}
```

- Fuzzy search over nav items and actions
- Keyboard navigation (arrow keys + enter)
- Quick URL navigation commands
- Actions: refresh data, toggle privacy, toggle theme

#### `Notifications` / `ToastContainer` (`src/components/shared/notifications.tsx`)

Custom toast notification system (not using sonner directly despite dependency).

```typescript
interface Toast {
  id: string;
  title: string;
  description?: string;
  variant?: 'default' | 'success' | 'error';
  duration?: number;
}
```

- Max 5 concurrent toasts
- Auto-dismiss with progress bar animation
- Stack from bottom-right

#### `QuickCommands` (`src/components/shared/quick-commands.tsx`)

Pre-built curl command snippets with copy-to-clipboard buttons for common API operations.

#### `PrivacyToggle` (`src/components/shared/privacy-toggle.tsx`)

Eye/EyeOff icon button. Toggles `PrivacyContext.privacy` mode.

#### `InfoTip` (`src/components/shared/info-tip.tsx`)

Info icon that shows a `Tooltip` on hover.

```typescript
interface InfoTipProps {
  text: string;
}
```

#### `TimeRangeFilter` (`src/components/shared/time-range-filter.tsx`)

Button group for time range selection.

```typescript
interface TimeRangeFilterProps {
  ranges: TimeRange[];
  selected: TimeRange;
  onSelect: (range: TimeRange) => void;
}
```

#### `ExportButton` (`src/components/shared/export-button.tsx`)

CSV/JSON export with automatic file download trigger.

```typescript
interface ExportButtonProps {
  data: Record<string, unknown>[];
  filename: string;
  format?: 'csv' | 'json';
}
```

#### `ContainerStatusCard` (`src/components/shared/container-status-card.tsx`)

Displays uptime, queue depth, and gateway version.

#### `RateForecast` (`src/components/shared/rate-forecast.tsx`)

Predicts key exhaustion time from current burn rate.

### Key Flow Monitor (`src/components/key-flow-monitor/`)

Live visualization of key-to-model request routing.

#### `KeyFlowMonitor` (index)

Main container showing key nodes, gateway node, model nodes, and animated SVG flow paths between them. Includes summary stats row (total keys, active models, total RPM).

#### `KeyNode` (`key-node.tsx`)

```typescript
interface KeyNodeProps {
  suffix: string;
  successCount: number;
  errorCount: number;
  rpm: number;
  rpmLimit: number;
  isActive: boolean;
  onClick?: () => void;
}
```

Color-coded card: green (active), amber (warning), red (cooldown).

#### `ModelNode` (`model-node.tsx`)

```typescript
interface ModelNodeProps {
  name: string;
  inFlight: number;
  limit: number;
  t429s: number;
  series: number;
  isActive: boolean;
  onClick?: () => void;
}
```

Shows capacity progress bar, 429 count, series info.

#### `FlowPaths` (`flow-paths.tsx`)

SVG curved bezier paths connecting key nodes to gateway to model nodes. Hover highlighting shows active flow.

#### `LivePulse` (`live-pulse.tsx`)

Animated green pulse dot indicating live data flow.

### Auth Monitor (`src/components/monitoring/auth-monitor/`)

#### `LiveAuthMonitor` (index)

Grid display of authenticated accounts with provider, status, rate limit utilization. Uses `DashboardContext`.

### Auth Dialog Components (`src/components/auth/`)

| Component               | Purpose                                      |
|-------------------------|----------------------------------------------|
| `ApiKeyDialog`          | API key input form                           |
| `DeviceCodeDialog`      | GitHub Copilot device code flow with polling |
| `AuthCodeDialog`        | OAuth authorization code flow                |
| `SessionCookieDialog`   | Cookie-based session auth                    |
| `OpenRouterModelPicker` | Model selection for OpenRouter provider      |

### UI Primitives (`src/components/ui/`)

Thin wrappers around Radix UI primitives, styled with Tailwind:

| Component     | Radix Primitive             | Notes                                                                          |
|---------------|-----------------------------|--------------------------------------------------------------------------------|
| `Badge`       | -                           | Custom, 4 variants (default/secondary/destructive/outline)                     |
| `Button`      | -                           | Custom, 6 variants + 3 sizes                                                   |
| `Card`        | -                           | Custom (Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter) |
| `Collapsible` | @radix-ui/react-collapsible | Collapsible container                                                          |
| `Dialog`      | @radix-ui/react-dialog      | Modal dialog with overlay                                                      |
| `Input`       | -                           | Custom styled input                                                            |
| `Progress`    | @radix-ui/react-progress    | Progress bar with indicator                                                    |
| `Select`      | @radix-ui/react-select      | Dropdown select with trigger/content/item                                      |
| `Separator`   | @radix-ui/react-separator   | Horizontal/vertical separator                                                  |
| `Sheet`       | -                           | Slide-in panel (side drawer)                                                   |
| `Sidebar`     | -                           | Custom sidebar with collapsible support                                        |
| `Skeleton`    | -                           | Loading placeholder                                                            |
| `Tabs`        | @radix-ui/react-tabs        | Tab navigation with content panels                                             |
| `Textarea`    | -                           | Custom styled textarea                                                         |
| `Tooltip`     | @radix-ui/react-tooltip     | Hover tooltip with provider                                                    |

---

## 4 Data Fetching

### API Client (`src/lib/api.ts`)

Core REST API functions, all returning typed promises:

| Function | Endpoint | Method | Return Type |
|---|---|---|---|
| `fetchLimiterStatus` | `/v1/limiter-status` | GET | `LimiterStatus` |
| `fetchModelStatus` | `/v1/limiter-status` | GET | `ModelStatus[]` |
| `fetchHealth` | `/health` | GET | `HealthStatus` |
| `fetchMetrics` | `/api/metrics` | GET | `string` (Prometheus text) |
| `setOverride` | `/v1/limiter-override` | POST | `void` |
| `fetchProfileUsage` | `/v1/usage/profiles[/:name]` | GET | `ProfileUsage \| ProfileUsage[]` |
| `fetchAccountUsage` | `/v1/usage/accounts` | GET | `AccountUsage[]` |
| `parsePrometheusText` | - | - | `ParsedMetric[]` |

Key interfaces:

```typescript
interface ModelStatus {
  name: string;
  in_flight: number;
  limit: number;
  max_limit: number;
  learned_ceiling: number;
  total_requests: number;
  total_429s: number;
  min_rtt_ms: number;
  ewma_rtt_ms: number;
  series: number;
  overridden: boolean;
}

interface LimiterStatus {
  global: GlobalStatus;
  models: ModelStatus[];
  keyPool: KeyPoolStatus;
  seenModels: string[];
  glmMode: boolean;
}

interface KeyStatusEntry {
  suffix: string;
  rpm: number;
  rpm_limit: number;
  rpm_used: number;
  cooldown_until: number;
  in_cooldown: boolean;
  success_count: number;
  error_count: number;
}
```

### Auth API (`src/lib/auth-api.ts`)

| Function                         | Purpose                                |
|----------------------------------|----------------------------------------|
| `startDeviceAuth`                | Initiate device code flow              |
| `startAuthCode`                  | Initiate OAuth authorization code flow |
| `pollAuthStatus`                 | Poll auth completion status            |
| `cancelAuth`                     | Cancel in-progress auth                |
| `listAccounts`                   | List all authenticated accounts        |
| `removeAccount`                  | Remove an account                      |
| `pauseAccount` / `resumeAccount` | Toggle account active state            |
| `setDefaultAccount`              | Set account as default for provider    |
| `updateAccountEmail`             | Update account email                   |
| `registerAPIKey`                 | Register new API key auth              |
| `registerSessionCookie`          | Register cookie-based session          |
| `fetchRateLimits`                | Get rate limits for account            |
| `login` / `logout` / `checkAuth` | Session management                     |

### Polling Architecture (`src/lib/polling.ts` + `src/contexts/dashboard-context.tsx`)

- **Intervals:** 5s, 10s, 30s, 60s (configurable in Settings, persisted in localStorage `arl-polling-interval`)
- **Pattern:** Each polling hook uses `setTimeout` chains (not `setInterval`) for adaptive rescheduling
- **Parallel fetch:** `DashboardContext` fetches `/v1/limiter-status` and `/health` in parallel via `Promise.all`
- **Cache invalidation:** Cross-tab sync via `storage` event listener + monkey-patched `localStorage.setItem`
- **GLM mode filtering:** When `glmMode` is true, models list is filtered to show only GLM-family models

```typescript
// Adaptive polling pattern (simplified)
useEffect(() => {
  let timeout: number;
  const poll = async () => {
    await fetchData();
    timeout = window.setTimeout(poll, interval);
  };
  poll();
  return () => clearTimeout(timeout);
}, [interval, deps]);
```

### WebSocket Connection (`src/hooks/use-websocket.ts`)

- **Endpoint:** `/ws` (proxied to gateway)
- **Reconnection:** Exponential backoff (1s base, 30s max)
- **Event buffer:** Max 50 events
- **Auto-reconnect:** Enabled by default
- **Event bus:** `src/lib/ws-events.ts` provides `wsOn` / `wsEmit` / `wsOff` with wildcard `*` support
- **WS-driven refresh:** `use-ws-refresh.ts` subscribes to ws-events and triggers data refresh callbacks

### Prometheus Metrics Hooks

| Hook                   | Purpose                                                |
|------------------------|--------------------------------------------------------|
| `usePrometheusMetrics` | Polls `/api/metrics`, parses Prometheus text format    |
| `useMetricsHistory`    | Builds time series from metric deltas (max 120 points) |
| `useAnomalyDetection`  | Detects 5 anomaly types with 30s cooldown              |
| `useEventTimeline`     | Derives timeline events from metrics/models/keyPool    |
| `useUsageApi`          | Polls `/v1/usage/models` and `/v1/usage/summary`       |

### Metrics Helpers (`src/lib/metrics-helpers.ts`)

Extraction functions for Prometheus data:
- `filterByModels` - Filter metrics by model label
- `extractModelTokens` - Per-model input/output token counts
- `extractModelCosts` - Per-model cost breakdown
- `extractTotalTokens` - Aggregate token counts
- `extractTotalCost` - Aggregate cost
- `extractErrorCounts` - Error counts by type
- `extractLatency` - Latency percentiles
- `extractInfraMetrics` - Go runtime metrics (goroutines, memory, GC)

### Privacy Metrics (`src/lib/privacy-api.ts`)

Extracts privacy-related metrics from Prometheus:
- `totalMaskedRequests` - Total PII-masked requests
- `secretsDetected` - Count by secret type (API key, token, etc.)
- `piiDetected` - Count by PII type (email, phone, etc.)
- `maskDuration` - P95 mask processing latency by phase

---

## 5 Dashboard Features

### Overview Page (`/`)

Main dashboard with 8 sub-components:

| Component          | Description                                      |
|--------------------|--------------------------------------------------|
| `StatCards`        | Status, Queue Depth, Total Requests, Concurrency |
| `GlobalCapacity`   | Progress bar showing global in-flight vs limit   |
| `ModelUtilization` | Table of top models by utilization               |
| `KeyFlowMonitor`   | Live key-to-model request flow visualization     |
| `LiveAuthMonitor`  | Authenticated accounts status grid               |
| `QuickCommands`    | Pre-built curl snippets for common operations    |
| `EventTimeline`    | Last 10 events with severity coloring            |

### Health Page (`/system-health`)

- **HealthGauge:** SVG circular gauge (sm/md/lg) with glow filter and animated dot
- **HealthStatsBar:** Horizontal stacked bar (passed/warning/error/info segments)
- **HealthChecks:** Collapsible groups (gateway, queue, models, keys, infra) with per-check status icons

Health check derivation (`src/lib/health-checks.ts`):
- Gateway: uptime > threshold
- Queue: depth within limits
- Models: no models at max capacity
- Keys: minimum active key count
- Infra: goroutine count, memory usage, GC pause

### Analytics Page (`/analytics`)

Full analytics dashboard with 12+ sub-components:

| Component                | Type                | Description                                                    |
|--------------------------|---------------------|----------------------------------------------------------------|
| `AnalyticsSummaryCards`  | 5 StatCards         | Total Tokens, Total Cost, Input Cost, Output Cost, Avg Latency |
| `UsageTrendChart`        | Dual-axis AreaChart | Tokens + cost over time with 2m/5m/10m range                   |
| `CostByModelCard`        | List + popover      | Model costs with token ratio bars                              |
| `ModelDistributionChart` | Donut PieChart      | Model percentage distribution                                  |
| `TokenBreakdownChart`    | Horizontal BarChart | Input vs output per model                                      |
| `HourlyBreakdown`        | BarChart            | Requests/tokens/cost in 24h buckets                            |
| `ErrorRateChart`         | Stacked AreaChart   | Errors by type over time                                       |
| `LatencyChart`           | AreaChart           | Average latency over time                                      |
| `ModelCostTable`         | Table               | Model, I/O tokens, cost, % of total                            |
| `AnomalyInsightsCard`    | List                | Recent anomalies with dismiss                                  |
| `UsageApiSection`        | Tables              | Daily breakdown + session table                                |
| `ModelDetailsPopover`    | Modal               | Detailed model view with I/O ratio, token bars                 |
| `TimeRangeFilter`        | Button group        | 1H/6H/24H/7D/30D selection                                     |

### Model Limits Page (`/model-limits`)

Table with columns: model, series, in-flight, limit, max limit, learned ceiling, min RTT, EWMA RTT, requests, 429s, adaptive/pinned status. Supports GLM mode filtering.

### Key Pool Page (`/key-pool`)

- StatCards: total keys, active, cooldown, avg RPM
- PoolHealthSummary: active/cooldown counts, utilization progress bar
- Key table: suffix, RPM, RPM limit, RPM used, cooldown status, success/error counts
- KeyHealthIndicator: color-coded dots (green/amber/red)

### Metrics Page (`/prometheus`)

Raw Prometheus dashboard:
- 6 summary cards (total requests, tokens, cost, errors, latency, active keys)
- Request rate by path chart
- Token usage BarChart
- Errors LineChart
- Optimizer charts (savings, cache hit rate)
- Raw metrics text display

### Controls Page (`/controls`)

- Manual override form: model select dropdown + limit number input
- Active overrides list with clear button
- RoutingStrategy component for strategy configuration

### Providers Page (`/providers`)

- 16 provider cards with auth setup instructions
- Account management (pause/resume/remove/default)
- Custom provider CRUD (add/edit/delete)
- Auth dialogs: API Key, Device Code, Auth Code, Session Cookie
- Account list with email editing, tier badges, rate limit utilization bars (5h/7d windows)

### Profiles Page (`/profiles`)

- Profile CRUD with multi-target routing
- Account pool selection per profile
- API key generation (arl_ prefixed tokens) with reveal/copy/revoke
- Usage display per profile
- Import/export functionality
- Setup guide sections: Claude OAuth, Gemini OAuth, Z.AI, Docker Haiku

### Quota Page (`/quota`)

- Per-account quota usage from `/v1/quota/:provider`
- Progress bars for quota consumption
- Usage data from `/v1/usage/models`

### Privacy Page (`/privacy`)

- Masked requests counter
- Secrets detected by type (bar chart)
- PII detected by type (bar chart)
- Mask duration P95 by phase
- Detectable types reference catalog

### Models Page (`/models`)

- Model catalog with search filtering
- Grouped by provider
- Pricing information display

### Logs Page (`/logs`)

- Error log table polling `/v1/logs/errors`
- 4xx/5xx count summary
- Timestamp, status, path, message columns

### Settings Page (`/settings`)

- Polling interval selector (5s/10s/30s/60s)
- Theme selector (dark/light/system)
- History retention configuration
- Notification preferences (checkboxes)
- Language selector (English/Thai)
- Server config display (thinking mode, global env vars)
- Reset all settings button

### Login Page (`/login`)

Simple API key input form. Calls `auth-api.login()` which validates against the gateway.

---

## 6 State Management

### Context Providers

#### `DashboardContext` (`src/contexts/dashboard-context.tsx`)

Core application state. Single source of truth for all monitoring data.

```typescript
interface DashboardContextType {
  models: ModelStatus[];
  global: GlobalStatus;
  keyPool: KeyPoolStatus;
  health: HealthStatus | null;
  glmMode: boolean;
  seenModels: string[];
  loading: boolean;
  error: string | null;
  lastRefresh: Date | null;
  refresh: () => Promise<void>;
}
```

- Fetches `/v1/limiter-status` + `/health` in parallel
- Filters models based on `glmMode` flag
- Exposes `refresh()` for manual data reload
- Error state propagated to consumers

#### `PrivacyContext` (`src/contexts/privacy-context.tsx`)

```typescript
interface PrivacyContextType {
  privacy: boolean;
  setPrivacy: (v: boolean) => void;
}
```

Persisted in localStorage `arl-privacy-mode`. When active, components add `PRIVACY_BLUR_CLASS` to sensitive elements.

#### `LanguageContext` (`src/contexts/language-context.tsx`)

```typescript
interface LanguageContextType {
  lang: 'en' | 'th';
  setLang: (lang: 'en' | 'th') => void;
}
```

Dispatches `arl:lang-changed` custom event for components that need to react to language changes. Persisted in localStorage.

#### `AuthContext` (`src/contexts/auth-context.tsx`)

```typescript
interface AuthContextType {
  isAuthenticated: boolean;
  login: (apiKey: string) => Promise<void>;
  logout: () => Promise<void>;
  checkAuth: () => Promise<boolean>;
}
```

Manages authentication state. Unauthenticated users are redirected to `/login`.

### Hooks as State Managers

| Hook                   | State Held                          | Source                              |
|------------------------|-------------------------------------|-------------------------------------|
| `useWebSocket`         | Connection status, event buffer     | WebSocket `/ws`                     |
| `usePrometheusMetrics` | Parsed metrics array                | `/api/metrics` polling              |
| `useMetricsHistory`    | Time series arrays (120 points max) | Delta computation                   |
| `useAnomalyDetection`  | Active anomalies list               | Delta detection on metrics          |
| `useEventTimeline`     | Timeline events array               | Derived from metrics/models/keyPool |
| `useUsageApi`          | Usage models + summary              | `/v1/usage/*` polling               |
| `useTimeRange`         | Selected time range                 | localStorage                        |
| `useMobile`            | `boolean` (768px breakpoint)        | Window resize listener              |
| `useSidebar`           | Expanded/collapsed state            | Sidebar component state             |
| `useAuthFlow`          | Auth flow state machine             | Device code / OAuth polling         |
| `useKeyboardShortcuts` | No state (event handlers)           | Global keyboard events              |

### State Flow Pattern

```
DashboardProvider (polls /v1/limiter-status + /health)
  |
  +---> models[], global{}, keyPool{}, health{}, glmMode
  |       |
  |       +---> OverviewPage (stat cards, key flow, event timeline)
  |       +---> ModelLimitsPage (model table)
  |       +---> KeyPoolPage (key table, pool health)
  |       +---> ControlsPage (override form)
  |
  +---> WebSocket events (useWebSocket -> ws-events bus)
          |
          +---> use-ws-refresh (triggers refresh on events)
          +---> use-event-timeline (derives events from data)

usePrometheusMetrics (polls /api/metrics)
  |
  +---> useMetricsHistory (builds time series)
          |
          +---> AnalyticsPage (charts, summary cards)
          +---> MetricsPage (raw display)
          +---> useAnomalyDetection (detects anomalies)
```

---

## 7 Text Diagram: UI Component Hierarchy and Data Flow

```
App
|
+-- BrowserRouter
    |
    +-- LanguageProvider
    |     dispatches 'arl:lang-changed' events
    |
    +-- PrivacyProvider
    |     persists to localStorage 'arl-privacy-mode'
    |     |
    |     +-- PRIVACY_BLUR_CLASS applied to sensitive elements
    |
    +-- AuthProvider
    |     login/logout/checkAuth via auth-api.ts
    |     redirects to /login if unauthenticated
    |
    +-- DashboardProvider  <====== polls /v1/limiter-status + /health
    |     |                     stores: models, global, keyPool, health, glmMode
    |     |                     exposes: refresh(), loading, error, lastRefresh
    |     |
    |     +-- NotificationProvider
    |           |
    |           +-- AppShell
    |                 |
    |                 +-- CommandPalette (Cmd+K)
    |                 +-- ToastContainer
    |                 +-- useKeyboardShortcuts
    |                 |
    |                 +-- Routes
    |                       |
    |                       +-- LoginPage (outside Layout)
    |                       |
    |                       +-- Layout
    |                             |
    |                             +-- SidebarProvider
|                             |     |
    |                             |     +-- AppSidebar
    |                             |           +-- NAV_ITEMS (14 items, 4 groups)
    |                             |           +-- DarkModeToggle
    |                             |           +-- PrivacyToggle
    |                             |           +-- ConnectionStatus
    |                             |
    |                             +-- WSBridge  <====== WebSocket /ws
|                             |     |
    |                             |     +-- ws-events bus (pub/sub)
|                             |           |
    |                             |           +-- use-ws-refresh -> triggers refresh()
    |                             |           +-- use-event-timeline -> derives events
    |                             |
    |                             +-- <Outlet />
    |                                   |
    |                                   +-- OverviewPage
    |                                   |     +-- StatCard x4 (Status, Queue, Requests, Concurrency)
    |                                   |     +-- GlobalCapacity (Progress)
    |                                   |     +-- ModelUtilization (table)
    |                                   |     +-- KeyFlowMonitor
    |                                   |     |     +-- KeyNode x N
    |                                   |     |     +-- ModelNode x N
    |                                   |     |     +-- FlowPaths (SVG bezier)
    |                                   |     |     +-- LivePulse
    |                                   |     +-- LiveAuthMonitor
    |                                   |     +-- QuickCommands
    |                                   |     +-- EventTimeline (last 10 events)
    |                                   |
    |                                   +-- HealthPage
    |                                   |     +-- HealthGauge (SVG circular)
    |                                   |     +-- HealthStatsBar (stacked)
    |                                   |     +-- HealthChecks (collapsible groups)
    |                                   |
    |                                   +-- AnalyticsPage
    |                                   |     +-- TimeRangeFilter
    |                                   |     +-- AnalyticsSummaryCards x5
    |                                   |     +-- UsageTrendChart (dual-axis AreaChart)
    |                                   |     +-- CostByModelCard
    |                                   |     +-- ModelDistributionChart (PieChart donut)
    |                                   |     +-- TokenBreakdownChart (horizontal BarChart)
    |                                   |     +-- HourlyBreakdown (BarChart)
    |                                   |     +-- ErrorRateChart (stacked AreaChart)
    |                                   |     +-- LatencyChart (AreaChart)
    |                                   |     +-- ModelCostTable
    |                                   |     +-- AnomalyInsightsCard
    |                                   |     +-- UsageApiSection (daily + sessions tables)
    |                                   |     +-- ModelDetailsPopover
    |                                   |
    |                                   +-- ModelLimitsPage (table: all models)
    |                                   |
    |                                   +-- KeyPoolPage
    |                                   |     +-- StatCards x4
    |                                   |     +-- PoolHealthSummary
    |                                   |     +-- KeyHealthIndicator x N
    |                                   |     +-- Key table
    |                                   |
    |                                   +-- MetricsPage (Prometheus)
    |                                   |     +-- SummaryCards x6
    |                                   |     +-- RequestRateChart
    |                                   |     +-- TokenUsageChart
    |                                   |     +-- ErrorChart
    |                                   |     +-- OptimizerCharts
    |                                   |     +-- RawMetrics
    |                                   |
    |                                   +-- ControlsPage
    |                                   |     +-- OverrideForm (model select + limit input)
    |                                   |     +-- ActiveOverridesList
    |                                   |     +-- RoutingStrategy
    |                                   |
    |                                   +-- ProvidersPage
    |                                   |     +-- ProviderCard x 16
    |                                   |     +-- AccountList
    |                                   |     +-- CustomProviderForm
    |                                   |     +-- ApiKeyDialog
    |                                   |     +-- DeviceCodeDialog
    |                                   |     +-- AuthCodeDialog
    |                                   |     +-- SessionCookieDialog
    |                                   |
    |                                   +-- ProfilesPage
    |                                   |     +-- ProfileCard (CRUD)
    |                                   |     +-- ApiKeyGenerator (arl_ tokens)
    |                                   |     +-- AccountPoolSelector
    |                                   |     +-- ImportExportButtons
    |                                   |     +-- SetupGuide
    |                                   |
    |                                   +-- QuotaPage
    |                                   |     +-- QuotaProgressBar
    |                                   |     +-- UsageBreakdown
    |                                   |
    |                                   +-- PrivacyPage
    |                                   |     +-- MaskedRequestsCounter
    |                                   |     +-- SecretsChart (bar)
    |                                   |     +-- PIIChart (bar)
    |                                   |     +-- MaskDurationChart
    |                                   |     +-- DetectableTypesCatalog
    |                                   |
    |                                   +-- ModelsPage
    |                                   |     +-- SearchFilter
    |                                   |     +-- ModelCatalog (grouped by provider)
    |                                   |
    |                                   +-- LogsPage
    |                                   |     +-- ErrorSummary (4xx/5xx counts)
    |                                   |     +-- ErrorLogTable
    |                                   |
    |                                   +-- SettingsPage
    |                                         +-- PollingIntervalSelector
    |                                         +-- ThemeSelector
    |                                         +-- LanguageSelector
    |                                         +-- NotificationPreferences
    |                                         +-- ServerConfigDisplay
    |                                         +-- ResetButton
```

### Data Flow Diagram

```
                          API Gateway (Go)
                          /v1/*  /health  /ws  /api/metrics
                                |
                    +-----------+-----------+
|           |           |
              REST polling   WebSocket   Prometheus
              (Dashboard     (real-time   (metrics
               Context)      events)     parsing)
|           |           |
                    v           v           v
              +-----+-----+ +--+---+ +----+----+
| models[]  | | ws-  | | Parsed  |
| global{}  | |events| | Metric[]|
              | keyPool{} | | bus  | +----+----+
| health{}  | +--+---+      |
              +-----+-----+    |     +----+----+
|          |     | Metrics |
|          |     | History |
                    |          |     +----+----+
|          |          |
                    v          v          v
              +-----+----------+----------+----+
              |         Page Components              |
              |  Overview / Analytics / Models / ... |
              +-----+----------+----------+----+
|          |          |
                    v          v          v
              +----------+ +---------+ +---------+
| Recharts | | Radix   | | Tailwind|
| (charts) | | UI prims| | CSS     |
              +----------+ +---------+ +---------+
```

---

## 8 Internationalization (i18n)

### Translation System (`src/lib/i18n.ts`)

- Two languages: English (`en`) and Thai (`th`)
- Translation dictionaries as plain objects
- `useTranslation()` hook returns `t()` function
- Language preference persisted in localStorage
- Language change dispatches `arl:lang-changed` custom DOM event
- Default language: English

### Usage Pattern

```typescript
const { t, lang, setLang } = useTranslation();
// t('key') returns translated string based on current lang
```

---

## 9 Privacy System

### Architecture

- **Toggle:** `PrivacyContext` boolean, persisted in localStorage
- **Blur class:** `PRIVACY_BLUR_CLASS = 'blur-[4px] select-none hover:blur-none transition-all'`
- **Scope:** Applied to API keys, costs, email addresses, and other sensitive data
- **Hover reveal:** Blur removes on hover (user explicitly interacts)
- **Privacy page:** Dedicated dashboard showing mask statistics

### Privacy Metrics (`src/lib/privacy-api.ts`)

Extracts from Prometheus:
- Total masked requests count
- Secrets detected: API keys, bearer tokens, passwords
- PII detected: email addresses, phone numbers
- Mask duration P95: parsing, detection, masking, total phases

---

## 10 Anomaly Detection

### System (`src/hooks/use-anomaly-detection.ts`)

Five anomaly types with independent 30-second cooldowns:

| Type             | Detection Logic               | Severity |
|------------------|-------------------------------|----------|
| `429_spike`      | Rate limit errors > threshold | High     |
| `error_burst`    | Error rate > threshold        | High     |
| `queue_buildup`  | Queue depth > threshold       | Medium   |
| `rtt_spike`      | RTT > baseline + margin       | Medium   |
| `key_exhaustion` | Active keys < minimum         | Critical |

- Tracks previous metrics snapshot for delta computation
- Dismiss individual anomalies (persisted in component state)
- Feeds into `AnomalyInsightsCard` on Analytics page and `EventTimeline` on Overview

---

## 11 Testing

### Test Framework

- **Playwright** for E2E testing
- **Config:** `playwright.config.ts`
- **CI mode:** retries enabled, local webServer via `bun run dev`

### Test Files

| File                            | Scope                                         |
|---------------------------------|-----------------------------------------------|
| `tests/dashboard.spec.ts`       | Core dashboard load, navigation, data display |
| `tests/profiles.spec.ts`        | Profile CRUD operations                       |
| `tests/profiles-check.spec.ts`  | Profile validation checks                     |
| `tests/profiles-delete.spec.ts` | Profile deletion flow                         |
| `tests/profiles-edit.spec.ts`   | Profile editing flow                          |

---

## 12 Keyboard Shortcuts

Defined in `src/hooks/use-keyboard-shortcuts.ts`:

| Shortcut | Action                             |
|----------|------------------------------------|
| `Cmd+K`  | Open command palette               |
| `Cmd+R`  | Refresh data                       |
| `Cmd+B`  | Toggle sidebar                     |
| `Cmd+P`  | Toggle privacy mode                |
| `Cmd+,`  | Navigate to settings               |
| `Escape` | Close active dialog/palette        |
| `1-9`    | Navigate to pages by sidebar order |

---

## 13 Build and Deployment

### Development

```bash
cd ui && bun install
cd ui && bun run dev    # Vite dev server on :5173 with proxy
```

### Production Build

```bash
cd ui && bun run build  # Outputs to ../api-gateway/static/
```

The Go gateway embeds the built static files and serves them at `/`. No separate web server needed.

### Docker

The UI is built during the Docker image build stage and the output directory is copied into the Go binary's embed path.

### Environment Variables

| Variable             | Default                   | Purpose                                    |
|----------------------|---------------------------|--------------------------------------------|
| `VITE_PROXY_TARGET`  | `http://arl-gateway:8080` | API proxy target                           |
| `VITE_ALLOWED_HOSTS` | `localhost`               | Dev server allowed hosts (comma-separated) |
| `VITE_HMR_HOST`      | -                         | HMR WebSocket host (enables HMR config)    |
| `VITE_HMR_PROTOCOL`  | `wss`                     | HMR protocol (ws or wss)                   |
| `VITE_HMR_PORT`      | `443`                     | HMR client port                            |
