# Dashboard Guide / คู่มือ Dashboard

ARL Dashboard เป็น React SPA (Vite + shadcn/ui + Recharts) ให้บริการที่ `/admin` โดย Go gateway.
Poll backend endpoints ทุก 5 วินาที. ไม่มี WebSocket, ไม่มี server-side historical queries.

Stack: React 18, Vite, TypeScript, Tailwind CSS, shadcn/ui, Recharts, Lucide icons, React Router

---

## Access / การเข้าถึง

Dashboard ให้บริการที่ path `/admin` ของ gateway (default port 8080):

```
http://localhost:8080/admin
```

### Authentication

ถ้าตั้ง `DASHBOARD_API_KEY` env var, dashboard จะเปิด auth mode:
- Login page จะแสดงก่อนเข้า dashboard
- Auth ใช้ cookie-based session
- Endpoints: `POST /v1/auth/login`, `POST /v1/auth/logout`, `GET /v1/auth/check`

ถ้าไม่ตั้ง `DASHBOARD_API_KEY` = เข้าได้เลยไม่ต้อง login.

---

## Pages / หน้าต่างๆ (10 หน้า)

### 1. Overview (`/`)

หน้าหลักแสดงสถานะรวมของระบบทั้งหมด

**4 StatCards:**

| Card | ข้อมูล | สี |
|---|---|---|
| Status | healthy/unhealthy + uptime duration | green/red |
| Queue Depth | pending requests count, เหลืองถ้า > 0 | warning/default |
| Total Requests | cumulative count + rate-limited count | default |
| Concurrency | in-flight / max global limit | warning ถ้า > 80% |

**Global Capacity:** Progress bar แสดง total in-flight vs max limit ทุก model รวมกัน

**Model Utilization:** Per-model bars แสดง in-flight/limit ratio:
- แต่ละ model มี progress bar + numeric values
- Badge "pinned" สำหรับ model ที่ถูก override (ไม่ใช่ adaptive)
- Badge "adaptive" สำหรับ model ที่ใช้ auto-scaling limit

**Key Flow Monitor:** Real-time SVG visualization แสดง request flow:
- แบ่งเป็น 3 columns: API Keys -> ARL Gateway -> Models
- Hover key node = highlight flow paths ไปยัง models ทั้งหมด
- Hover model node = highlight flow paths จาก keys ทั้งหมด
- Live pulse indicator ที่ header
- Summary stats: In-Flight, Success, 429s, Success Rate (สีเปลี่ยนตาม rate)
- แสดง "Passthrough" เมื่อไม่มี upstream keys

**Quick Commands:** Copyable curl snippets:
- `curl {host}/v1/limiter-status` - check model limits
- `curl {host}/health` - health status
- `curl {host}/api/metrics` - Prometheus metrics
- `curl -X POST {host}/v1/limiter-override -d '{"model":"...","limit":10}'` - set override
- Click icon คัดลอก, check mark แสดง 2 วินาที

**Event Timeline:** ตรวจจับ events จาก metric deltas:
- 429 spike events
- Key cooldown events
- Override changes
- Queue buildup
- Error bursts
- เก็บ 50 events ล่าสุด, dedupe within 30s window
- แสดง timestamp + event type + description

---

### 2. Health (`/system-health`)

แสดงสถานะ health ของระบบแบบ real-time

**Health Gauge:** Large circular gauge แสดง health percentage:
- คำนวณจาก health checks ทั้งหมด (passed/warning/error/info)
- สี: green (healthy), yellow (warning), red (error)

**System Health Card:**
- Uptime duration
- Summary: X checks: Y passed, Z warnings, W errors

**Health Stats Bar:** Horizontal bar แสดงสัดส่วน passed/warning/error/info

**Health Checks:** Grouped check list ที่ derived จาก `/health` endpoint data + metrics:
- ตรวจจับจาก: health status, model limits, key pool, Prometheus metrics
- แต่ละ group แสดง check name + status + optional message

ข้อมูลมาจาก `GET /health` + Prometheus metrics

---

### 3. Model Limits (`/model-limits`)

ตารางแสดง concurrency limits ของแต่ละ model

| Column | คำอธิบาย |
|---|---|
| Model | ชื่อ model (font-mono) |
| Series | model series grouping |
| In-Flight | concurrent requests ปัจจุบัน |
| Limit | concurrency limit ปัจจุบัน |
| Max | max limit ที่ตั้งไว้ |
| Ceiling | learned ceiling จาก adaptive limiter (แสดง `-` ถ้ายังไม่มี) |
| Min RTT | minimum round-trip time (ms) |
| EWMA RTT | exponentially weighted moving average RTT (ms) |
| Requests | cumulative request count |
| 429s | cumulative 429 responses (สีแดงถ้า > 0) |
| Status | `Pinned` (badge, manual override) หรือ `Adaptive` (badge, auto-scaling) |

ข้อมูลมาจาก `GET /v1/limiter-status`

---

### 4. Key Pool (`/key-pool`)

จัดการและตรวจสอบ API key pool สำหรับ sync proxy path

**3 StatCards:**

| Card | ข้อมูล |
|---|---|
| Total Keys | จำนวน keys ใน pool (แสดง "passthrough mode" ถ้า 0) |
| Global Concurrency | in-flight / global limit |
| Queue Depth | pending requests |

**Pool Health Summary:** Summary component แสดงสถานะรวมของ key pool

**Key Table:** แสดงรายละเอียดแต่ละ key:

| Column | คำอธิบาย |
|---|---|
| Key | suffix ของ key (เช่น `...abc123`), blur เมื่อเปิด privacy mode |
| RPM | current RPM / RPM limit |
| RPM Util | progress bar + percentage |
| Success | success count (สีเขียว) |
| Errors | error count (สีแดง) |
| Error Rate | percentage, เหลือง > 0%, แดง > 10% |
| Status | health indicator (active/cooldown/error) |

**Passthrough Mode:** เมื่อไม่มี keys ใน pool (`UPSTREAM_API_KEYS` ว่าง):
- แสดง icon + "Passthrough mode" message
- Client keys ถูก forward ไป upstream โดยตรง

ข้อมูลมาจาก `GET /v1/limiter-status` (keyPool field)

---

### 5. Analytics (`/analytics`)

Usage analytics แสดง token usage, cost, และ model distribution

**5 Summary StatCards:**

| Card | ข้อมูล | Privacy-blurrable |
|---|---|---|
| Total Tokens | cumulative input + output tokens | yes |
| Total Cost | cumulative cost | yes |
| Input Cost | cumulative input cost | yes |
| Output Cost | cumulative output cost | yes |
| Avg Latency | average request latency | yes |

**Usage Trend Chart:** Dual-axis area chart:
- Left Y-axis: tokens (blue area)
- Right Y-axis: cost (green area)
- X-axis: time labels
- Time range toggle: 2m / 5m / 10m
- Data สะสมจาก 5s polling, compute deltas จาก cumulative Prometheus counters
- Tooltip แสดง tokens + cost (blur ใน privacy mode)
- แสดง "Collecting data..." ถ้าข้อมูลไม่พอ

**Cost By Model Card:** Sorted model list:
- Stacked horizontal bars: input cost (blue) + output cost (orange)
- Clickable เพื่อเปิด Model Details Popover

**Model Distribution Chart:** Donut/pie chart แสดง token share ต่อ model

**Token Breakdown Chart:** Horizontal stacked bar chart:
- Input/output tokens ต่อ model
- Cost summary boxes

**Model Details Popover:** แสดงเมื่อ click ที่ model ใน Cost By Model:
- I/O ratio badge
- Cost + tokens stats
- Token breakdown bars (input vs output)

**Hourly Breakdown:** 24-hour bar chart:
- Toggle view: requests / tokens / cost
- Data สะสมจาก metric deltas ทุกชั่วโมง (ใช้ ref เก็บ buckets, prune ชั่วโมงที่เกิน 24)
- Skip first poll เพื่อ establish baseline
- Tooltip แสดง requests + tokens + cost ของแต่ละ hour

ข้อมูลมาจาก `GET /metrics` (Prometheus) + `GET /v1/limiter-status`

---

### 6. Metrics (`/metrics`)

Prometheus metrics dashboard แสดง raw metrics จาก gateway

ข้อมูลมาจาก `GET /metrics`

---

### 7. Controls (`/controls`)

ตั้งค่า manual override สำหรับ model concurrency limits

**Manual Override Form:**
1. เลือก Model จาก dropdown (models ทั้งหมดจาก `/v1/limiter-status`)
2. ใส่ Limit (non-negative integer)
3. Click "Apply"
4. Endpoint: `POST /v1/limiter-override` with `{ model: string, limit: number }`
5. Toast notification แสดงผล

**Active Overrides:**
- แสดง model ทั้งหมดที่ถูก pinned (badge "pinned at X")
- ปุ่ม "Clear" เพื่อลบ override (ส่ง limit: 0)
- เมื่อไม่มี override: แสดง "No active overrides. All models are using adaptive limits."

การตั้ง override ทำให้ model นั้นเปลี่ยนจาก "Adaptive" เป็น "Pinned" ในหน้า Model Limits

---

### 8. Privacy (`/privacy`)

Privacy metrics dashboard แสดง data masking และ PII detection

**4 StatCards:**

| Card | ข้อมูล | Icon |
|---|---|---|
| Total Masked Requests | cumulative masked requests | Shield (blue) |
| Secrets Detected | 24h aggregate by type | Eye (red) |
| PII Detected | 24h aggregate by type | Fingerprint (orange) |
| Mask Duration p95 | slowest masking phase in ms | Timer (purple) |

**Charts:**
- Secrets by Type: bar chart แสดง count ต่อ secret type (สีแดง)
- PII by Type: bar chart แสดง count ต่อ PII type (สีส้ม)
- Mask Duration by Phase: line chart แสดง p95 duration ต่อ phase (สีม่วง)

ข้อมูลมาจาก privacy metrics endpoint, refresh ทุก 5s

---

### 9. Providers (`/providers`)

จัดการ multi-provider connections สำหรับ async queue path

**5 Provider Cards:**

| Provider | Icon | Auth Type | Badge Color |
|---|---|---|---|
| Z.AI | Sparkles | API Key | amber |
| Anthropic | Bot | API Key | amber |
| OpenAI | Zap | API Key | amber |
| Gemini | Sparkles | OAuth | green |
| GitHub Copilot | Github | Device Code | blue |

แต่ละ card แสดง:
- Icon + provider name
- Auth type badge (สีตาม type)
- Active count (เช่น "2/3 active")

**Connect Flow:**

API Key providers (Z.AI, Anthropic, OpenAI):
1. Click "Connect" / "Add"
2. Sheet dialog เปิดขึ้นพร้อม password input
3. Toggle show/hide key
4. Click "Register"
5. Endpoint: `POST /v1/auth/{provider}/register`

Device Code providers (GitHub Copilot):
1. Click "Connect"
2. Dialog แสดง user code + verification URL
3. User เปิด URL ใส่ code ยืนยัน
4. Poll `GET /v1/auth/{provider}/status` จน complete
5. Timeout 5 นาที (300s)
6. Cancel: `POST /v1/auth/{provider}/cancel`

OAuth providers (Gemini):
1. Click "Connect"
2. Dialog แสดง auth URL + status
3. เปิด browser สำหรับ auth code flow
4. Callback: `GET /v1/auth/{provider}/callback`
5. Start URL: `POST /v1/auth/{provider}/start-url`

**Account List:** Expand ดู accounts ของแต่ละ provider:
- Account info + status
- Actions: Remove, Pause, Resume, Set Default
- Multi-account support (หลาย key ต่อ provider)

---

### 10. Settings (`/settings`)

ตั้งค่า dashboard preferences

**General:**
- Polling Interval: 5s / 10s / 30s / 60s (default 10s)
- Theme: Dark / Light / System (default dark)
- History Retention: 2min / 5min / 10min (default 5min)

**Notifications:** Toggle แต่ละ type:
- Key cooldown events
- Override changes
- Anomaly alerts
- Connection status
- OAuth events
- Token refresh

**Language:** English / Thai (TH/EN, auto-persisted)

**About:** Version info + Reset All button (clear all localStorage settings)

Settings ทั้งหมดเก็บใน localStorage ด้วย prefix `arl-`

---

## Cross-cutting Features / ฟีเจอร์ร่วม

### Privacy Mode

Toggle ที่ sidebar footer (Eye/EyeOff icon):
- เปิด/ปิดได้จาก sidebar หรือ Command Palette
- CSS blur บนข้อมูล sensitive: costs, token counts, key suffixes
- Hover เพื่อ reveal (CSS hover effect)
- Persisted ใน localStorage
- ใช้ `PRIVACY_BLUR_CLASS` CSS class

### Dark/Light Theme

Toggle ที่ sidebar footer:
- Dark (default), Light, System
- Persisted ใน localStorage key `theme`
- เปลี่ยน class `dark` บน `<html>` element
- ตั้งค่าได้จาก Settings page หรือ Command Palette

### Command Palette

เปิดด้วย `Cmd+K`:
- Fuzzy search ของ commands ทั้งหมด
- Grouped: Navigation, Actions, Quick
- Arrow key navigation + Enter เลือก
- ESC ปิด

Commands ที่มี:

| Group | Command | Shortcut |
|---|---|---|
| Navigation | Go to Overview | - |
| Navigation | Go to Health | 2 |
| Navigation | Go to Model Limits | 3 |
| Navigation | Go to Key Pool | 4 |
| Navigation | Go to Analytics | 5 |
| Navigation | Go to Metrics | 6 |
| Navigation | Go to Controls | 7 |
| Navigation | Go to Privacy | 8 |
| Navigation | Go to Settings | 9 |
| Actions | Toggle Privacy Mode | Cmd+P |
| Actions | Toggle Theme | - |
| Actions | Refresh Data | Cmd+R |
| Quick | Copy Limiter Status URL | - |
| Quick | Copy Health URL | - |

### Keyboard Shortcuts

| Shortcut | Action |
|---|---|
| `Cmd+K` | เปิด Command Palette |
| `Cmd+R` | Refresh data |
| `Cmd+B` | Toggle sidebar |
| `Cmd+P` | Toggle privacy mode |
| `Cmd+,` | เปิด Settings |
| `1-9` | Navigate ไปยัง page ตามลำดับ |

### Toast Notifications

- Max 5 visible toasts
- Auto-dismiss
- ใช้ sonner library
- แสดง success/error สำหรับ override actions, auth flows, ฯลฯ

### i18n (Internationalization)

- ภาษา: English (EN) / Thai (TH)
- 90+ translation keys
- Auto-persisted ใน localStorage
- ตั้งค่าได้จาก Settings page

### Collapsible Sidebar

- Toggle icon-only mode
- Tooltip labels เมื่อ collapsed
- Toggle ด้วย `Cmd+B`

### Connection Status

- Green/red dot ที่ sidebar footer
- Green = healthy, connected
- Red = disconnected
- แสดง last refresh time

---

## Backend Endpoints Reference

| Endpoint | Method | ใช้ในหน้า |
|---|---|---|
| `/health` | GET | Overview, Health |
| `/v1/limiter-status` | GET | Overview, Model Limits, Key Pool |
| `/v1/limiter-override` | POST | Controls |
| `/metrics` | GET | Metrics, Analytics |
| `/v1/auth/check` | GET | Auth guard |
| `/v1/auth/login` | POST | Login page |
| `/v1/auth/logout` | POST | Login page |
| `/v1/auth/{provider}/start` | POST | Providers (device code) |
| `/v1/auth/{provider}/start-url` | POST | Providers (OAuth) |
| `/v1/auth/{provider}/register` | POST | Providers (API key) |
| `/v1/auth/{provider}/callback` | GET | Providers (OAuth callback) |
| `/v1/auth/{provider}/status` | GET | Providers (poll status) |
| `/v1/auth/{provider}/cancel` | POST | Providers (cancel flow) |
| `/v1/auth/accounts` | GET | Providers |
| `/v1/auth/accounts/{provider}` | GET | Providers |
| `/v1/auth/accounts/{provider}/{id}` | DELETE | Providers (remove) |
| `/v1/auth/accounts/{provider}/{id}/pause` | POST | Providers (pause) |
| `/v1/auth/accounts/{provider}/{id}/resume` | POST | Providers (resume) |
| `/v1/auth/accounts/{provider}/{id}/default` | POST | Providers (set default) |

---

## Data Flow / การไหลของข้อมูล

```
Browser (5s poll)
  |
  +---> GET /health          -> Overview, Health pages
  +---> GET /v1/limiter-status -> Overview, Model Limits, Key Pool pages
  +---> GET /metrics         -> Metrics, Analytics pages
  |
  +---> (on user action)
       |
       +---> POST /v1/limiter-override  -> Controls page
       +---> POST /v1/auth/login        -> Login page
       +---> POST /v1/auth/{p}/register -> Providers page
       +---> ...
```

Dashboard ไม่มี WebSocket. ทุกอย่างเป็น polling-based:
- Default: ทุก 5 วินาที (configurable ใน Settings: 5s/10s/30s/60s)
- Analytics history: สะสมจาก polling data, compute deltas จาก cumulative counters
- Hourly breakdown: เก็บใน memory (ref), reset เมื่อ refresh page

---

## Configuration / การตั้งค่าที่เกี่ยวข้อง

```bash
# Dashboard auth (optional)
DASHBOARD_API_KEY=your-secret-key    # เปิด auth mode, ว่าง = no auth

# Upstream key pool (sync proxy path)
UPSTREAM_API_KEYS=key1,key2,key3     # comma-separated, ว่าง = passthrough
UPSTREAM_RPM_LIMIT=40                # RPM limit ต่อ key

# Model concurrency
UPSTREAM_MODEL_LIMITS=glm-5.1:1,glm-5:2,glm-4.7:2
UPSTREAM_DEFAULT_LIMIT=1
UPSTREAM_GLOBAL_LIMIT=9
```

---

## UI State Persistence

ข้อมูลที่เก็บใน localStorage:

| Key | ค่า | Default |
|---|---|---|
| `theme` | `dark` / `light` | `dark` |
| `arl-polling-interval` | `5s` / `10s` / `30s` / `60s` | `10s` |
| `arl-default-theme` | `dark` / `light` / `system` | `dark` |
| `arl-history-retention` | `2min` / `5min` / `10min` | `5min` |
| `arl-lang` | `en` / `th` | `en` |
| `arl-notify-*` | `true` / `false` | `false` |
| Privacy mode | (via context) | `false` |

Reset ทั้งหมดได้จาก Settings > About > Reset All
