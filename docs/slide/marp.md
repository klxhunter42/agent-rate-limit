---
marp: true
theme: uncover
class: invert
paginate: true
style: |
  /* ── Global ── */
  section {
    background: #0a0a1a;
    background-image:
      radial-gradient(ellipse at 20% 80%, rgba(99, 102, 241, 0.08) 0%, transparent 50%),
      radial-gradient(ellipse at 80% 20%, rgba(34, 211, 238, 0.06) 0%, transparent 50%);
    font-family: 'Inter', 'Segoe UI', system-ui, -apple-system, sans-serif;
    color: #cbd5e1;
    letter-spacing: 0.01em;
  }

  /* ── Headings ── */
  h1 {
    color: #f1f5f9;
    font-weight: 800;
    line-height: 1.15;
    letter-spacing: -0.02em;
  }
  h2 {
    color: #e2e8f0;
    font-weight: 700;
    font-size: 1.55em;
    position: relative;
    padding-bottom: 0.4em;
    margin-bottom: 0.7em;
  }
  h2::after {
    content: '';
    position: absolute;
    bottom: 0;
    left: 0;
    width: 60px;
    height: 3px;
    background: linear-gradient(90deg, #06b6d4, #8b5cf6);
    border-radius: 2px;
  }
  h3 {
    color: #94a3b8;
    font-weight: 600;
    font-size: 1.05em;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    margin-bottom: 0.5em;
  }

  /* ── Code ── */
  code {
    background: rgba(99, 102, 241, 0.12);
    border: 1px solid rgba(99, 102, 241, 0.2);
    border-radius: 5px;
    padding: 2px 8px;
    font-size: 0.82em;
    color: #c4b5fd;
    font-family: 'JetBrains Mono', 'Fira Code', monospace;
  }
  pre {
    background: rgba(15, 15, 35, 0.9) !important;
    border: 1px solid rgba(99, 102, 241, 0.15);
    border-radius: 12px;
    font-size: 0.62em;
    line-height: 1.5;
    box-shadow: 0 4px 24px rgba(0,0,0,0.3), inset 0 1px 0 rgba(255,255,255,0.03);
  }
  pre code {
    background: none;
    border: none;
    padding: 0;
    color: #94a3b8;
  }

  /* ── Tables ── */
  table {
    font-size: 0.72em;
    border-collapse: separate;
    border-spacing: 0;
    width: 100%;
    border-radius: 10px;
    overflow: hidden;
    border: 1px solid rgba(99, 102, 241, 0.12);
  }
  th {
    background: rgba(99, 102, 241, 0.1);
    color: #c4b5fd;
    padding: 10px 14px;
    text-align: left;
    font-weight: 600;
    font-size: 0.9em;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    border-bottom: 1px solid rgba(99, 102, 241, 0.2);
  }
  td {
    padding: 8px 14px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.04);
    color: #94a3b8;
  }
  tr:nth-child(even) td {
    background: rgba(255, 255, 255, 0.02);
  }
  tr:last-child td {
    border-bottom: none;
  }

  /* ── Emphasis ── */
  strong {
    color: #22d3ee;
    font-weight: 600;
  }
  em {
    color: #c4b5fd;
    font-style: normal;
    font-weight: 500;
  }

  /* ── Blockquote ── */
  blockquote {
    border-left: 3px solid;
    border-image: linear-gradient(to bottom, #06b6d4, #8b5cf6) 1;
    background: linear-gradient(135deg, rgba(6, 182, 212, 0.05), rgba(139, 92, 246, 0.05));
    padding: 10px 18px;
    border-radius: 0 10px 10px 0;
    font-size: 0.85em;
    color: #94a3b8;
  }

  /* ── Lists ── */
  li {
    margin-bottom: 0.3em;
    line-height: 1.5;
  }
  li::marker {
    color: #6366f1;
  }

  /* ── Layout ── */
  .cols {
    display: flex;
    gap: 1.5em;
  }
  .cols > div {
    flex: 1;
  }

  /* ── Tags / Chips ── */
  .chip {
    display: inline-block;
    background: rgba(6, 182, 212, 0.1);
    border: 1px solid rgba(6, 182, 212, 0.25);
    border-radius: 20px;
    padding: 4px 14px;
    font-size: 0.78em;
    color: #22d3ee;
    margin: 3px;
    font-weight: 500;
  }
  .chip-purple {
    background: rgba(139, 92, 246, 0.1);
    border-color: rgba(139, 92, 246, 0.25);
    color: #a78bfa;
  }
  .chip-green {
    background: rgba(34, 197, 94, 0.1);
    border-color: rgba(34, 197, 94, 0.25);
    color: #4ade80;
  }
  .chip-amber {
    background: rgba(245, 158, 11, 0.1);
    border-color: rgba(245, 158, 11, 0.25);
    color: #fbbf24;
  }

  /* ── Stat Cards ── */
  .stat-card {
    background: rgba(15, 15, 35, 0.6);
    border: 1px solid rgba(99, 102, 241, 0.15);
    border-radius: 16px;
    padding: 20px 24px;
    text-align: center;
    backdrop-filter: blur(10px);
  }
  .stat-card .num {
    font-size: 2.2em;
    font-weight: 800;
    background: linear-gradient(135deg, #06b6d4, #8b5cf6);
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
    background-clip: text;
    line-height: 1.2;
  }
  .stat-card .lbl {
    font-size: 0.78em;
    color: #64748b;
    margin-top: 4px;
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  /* ── Feature Card ── */
  .feat {
    background: rgba(15, 15, 35, 0.5);
    border: 1px solid rgba(99, 102, 241, 0.1);
    border-radius: 14px;
    padding: 18px;
    margin-bottom: 10px;
    backdrop-filter: blur(8px);
  }
  .feat h4 {
    margin: 0 0 6px 0;
    color: #e2e8f0;
    font-size: 0.95em;
    font-weight: 600;
  }
  .feat p {
    margin: 0;
    font-size: 0.78em;
    color: #64748b;
    line-height: 1.5;
  }

  /* ── Section Variants ── */
  section.cover {
    text-align: center;
    justify-content: center;
    background:
      radial-gradient(ellipse at 30% 50%, rgba(99, 102, 241, 0.15) 0%, transparent 60%),
      radial-gradient(ellipse at 70% 50%, rgba(6, 182, 212, 0.1) 0%, transparent 60%),
      #0a0a1a;
  }
  section.cover h1 {
    font-size: 3.2em;
    margin-bottom: 0.1em;
    background: linear-gradient(135deg, #f1f5f9 0%, #94a3b8 100%);
    -webkit-background-clip: text;
    -webkit-text-fill-color: transparent;
    background-clip: text;
  }
  section.cover .subtitle {
    color: #64748b;
    font-size: 1.1em;
    font-weight: 400;
    margin-top: 0;
  }

  section.lead {
    text-align: center;
    justify-content: center;
  }
  section.lead h2 {
    display: inline-block;
  }
  section.lead h2::after {
    left: 50%;
    transform: translateX(-50%);
  }

  /* ── Gradient Divider ── */
  .divider {
    height: 2px;
    background: linear-gradient(90deg, transparent, #6366f1, #06b6d4, transparent);
    border: none;
    margin: 1.2em 0;
  }

  /* ── Flow Step ── */
  .step {
    display: flex;
    align-items: flex-start;
    gap: 12px;
    margin-bottom: 8px;
  }
  .step-icon {
    min-width: 28px;
    height: 28px;
    border-radius: 8px;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 0.7em;
    font-weight: 700;
    color: #0a0a1a;
  }
  .step-body {
    flex: 1;
  }
  .step-body strong {
    display: block;
    font-size: 0.88em;
    margin-bottom: 2px;
  }
  .step-body span {
    font-size: 0.78em;
    color: #64748b;
  }

  /* ── Check / Cross marks ── */
  .ok { color: #4ade80; font-weight: 700; }
  .no { color: #f87171; font-weight: 700; }

  /* ── Page number ── */
  section::after {
    color: #475569;
    font-size: 0.7em;
  }

  /* ── Connector Lines ── */
  .flow-box {
    background: rgba(15, 15, 35, 0.6);
    border: 1px solid rgba(99, 102, 241, 0.12);
    border-radius: 10px;
    padding: 12px 16px;
    margin-bottom: 6px;
    font-size: 0.82em;
  }
  .flow-arrow {
    text-align: center;
    color: #475569;
    font-size: 0.9em;
    margin: 2px 0;
  }
---

<!-- _class: cover -->
<!-- _paginate: false -->

<br>

<h1>AI Gateway</h1>

<p class="subtitle">Smart Rate Limiting & Multi-Provider Proxy for AI Agents</p>

<hr class="divider">

<br>

<span class="chip">Go Gateway</span>
<span class="chip">Python Worker</span>
<span class="chip">Dragonfly</span>
<span class="chip chip-purple">17 Providers</span>
<span class="chip chip-purple">OAuth + API Key</span>
<span class="chip chip-green">Real-time Dashboard</span>

<br>

<p style="color: #475569; font-size: 0.85em; font-weight: 500;">Thanapat Taweerat &mdash; 2026</p>

---

<!-- _class: lead -->

<h2>Why AI Gateway?</h2>

<div class="cols" style="margin-top: 0.5em;">
<div>

<h3>The Problem</h3>

<div class="feat" style="border-left: 3px solid #f87171;">
<h4>API Hammering</h4>
<p>Claude Code and AI agents send rapid-fire requests that exhaust API rate limits</p>
</div>

<div class="feat" style="border-left: 3px solid #f87171;">
<h4>Single Key SPOF</h4>
<p>One key fails = entire system down. No fallback, no recovery.</p>
</div>

<div class="feat" style="border-left: 3px solid #f87171;">
<h4>Zero Visibility</h4>
<p>No usage tracking, no cost estimation, no anomaly detection</p>
</div>

</div>
<div>

<h3>The Solution</h3>

<div class="feat" style="border-left: 3px solid #4ade80;">
<h4>Transparent Proxy</h4>
<p>Every byte passes through unchanged. Client sees no difference.</p>
</div>

<div class="feat" style="border-left: 3px solid #4ade80;">
<h4>Key Pool + Auto-Rotation</h4>
<p>Multiple keys with automatic cooldown and recovery on 429</p>
</div>

<div class="feat" style="border-left: 3px solid #4ade80;">
<h4>17 Providers with OAuth</h4>
<p>Claude OAuth, Gemini OAuth, Copilot, OpenAI, and 13 more with auto-fallback</p>
</div>

</div>
</div>

---

## System Architecture

```
  Clients (Claude Code, AI Agents, CI/CD, Dashboard)
      |
      v
+==========================================================+
|                   API Gateway (:8080)                     |
|                                                           |
|  +-- SecurityHeaders -- CorrelationID -- RealIP -- IPFil  |
|  +-- Logging -- Metrics -- RateLimiter -- LoginLimiter    |
|                                                           |
|  +-------------+  +-------------+  +-------------+       |
|  | PasteGuard  |  | Token Optim |  | Quota Check |       |
|  | Secrets+PII |  | WS+Dedup+Tr |  | >=95%: 429  |       |
|  +-------------+  +-------------+  +-------------+       |
|                                                           |
|  +----------+  +-----------+  +------------------+       |
|  | Key Pool |  | Adaptive  |  | Profile Router   |       |
|  | Rotation |  | Limiter   |  | X-Profile Header |       |
|  +----------+  +-----------+  +------------------+       |
+======================+=========+==========================+
                       |                    |
            +----------+----------+    +----+----+
            |    Sync Proxy       |    |  Async  |
            |  /v1/messages SSE   |    |  Queue  |
            +----------+----------+    +----+----+
                       |                    |
              +--------+--------+     +----+----+
              |  17 Providers    |     | Worker  |
              |  4 Auth Types    |     | 50 coro |
              +------------------+     +---------+

  +----------+   +------------+   +-----------+   +---------+
  | Dragonfly|   | Prometheus |   |  Grafana  |   | Presidio|
  |  :6379   |   |   :9090    |   |   :3000   |   |  :3000  |
  +----------+   +------------+   +-----------+   +---------+
```

<div class="cols" style="margin-top: 0.3em;">
<div class="feat" style="border-top: 2px solid #06b6d4;">
<p><strong>Sync</strong> - Real-time SSE streaming, transparent proxy to upstream</p>
</div>
<div class="feat" style="border-top: 2px solid #fbbf24;">
<p><strong>Async</strong> - Queue + Worker (50 coroutines), poll GET /v1/results/{id}</p>
</div>
<div class="feat" style="border-top: 2px solid #8b5cf6;">
<p><strong>17 Providers</strong> - 4 auth types, auto-fallback, per-provider RPM limiting</p>
</div>
</div>

## Sync vs Async Mode

| | Sync (<code>/v1/messages</code>) | Async (<code>/v1/chat/completions</code>) |
|---|---|---|
| **Use case** | Claude Code (real-time) | Batch agents, CI/CD |
| **Flow** | Gateway -> Proxy -> Upstream -> Response | Gateway -> Queue -> Worker -> Cache |
| **Response** | Real-time SSE streaming | Request ID -> Poll <code>GET /v1/results/{id}</code> |
| **Rate limit** | Global + Per API key | Global + Per agent_id |
| **Timeout** | <code>STREAM_TIMEOUT</code> (300s) | Worker poll 5s |

<br>

<div class="cols">
<div class="feat" style="border-left: 3px solid #4ade80;">
<h4>Sync = Transparent Proxy</h4>
<p>Every byte passes through unchanged. Client experience is identical to hitting upstream directly.</p>
</div>
<div class="feat" style="border-left: 3px solid #fbbf24;">
<h4>Async = Queue-Based</h4>
<p>Burst-friendly. Accept all jobs, worker paces requests. Exponential backoff + retry on failure.</p>
</div>
</div>

---

## Transparent Proxy Design

<div class="cols">
<div>

<div class="flow-box" style="border-left: 3px solid #4ade80;">
<strong>What it DOES</strong>
</div>

<div class="step">
<div class="step-icon" style="background: #4ade80;">1</div>
<div class="step-body">
<strong>Receive raw bytes</strong>
<span>Read request body as-is</span>
</div>
</div>
<div class="step">
<div class="step-icon" style="background: #4ade80;">2</div>
<div class="step-body">
<strong>Select key + model slot</strong>
<span>Key pool + adaptive limiter</span>
</div>
</div>
<div class="step">
<div class="step-icon" style="background: #4ade80;">3</div>
<div class="step-body">
<strong>Rewrite model field (only change)</strong>
<span>When fallback selects a different model</span>
</div>
</div>
<div class="step">
<div class="step-icon" style="background: #4ade80;">4</div>
<div class="step-body">
<strong>Forward to upstream</strong>
<span>Every other byte untouched</span>
</div>
</div>
<div class="step">
<div class="step-icon" style="background: #4ade80;">5</div>
<div class="step-body">
<strong>Relay response to client</strong>
<span>Status code, headers, body - all passthrough</span>
</div>
</div>

</div>
<div>

<div class="flow-box" style="border-left: 3px solid #f87171;">
<strong>What it does NOT do</strong>
</div>

<br>

- <span class="no">x</span> Decode request body to Go struct
- <span class="no">x</span> Re-encode request body
- <span class="no">x</span> Modify response body
- <span class="no">x</span> Add or remove JSON fields
- <span class="no">x</span> Transform content blocks
- <span class="no">x</span> Separate text/tool_use/thinking
- <span class="no">x</span> Convert 429 to 502

<br>

<blockquote>
If it can't be done by changing just the <code>model</code> field, the gateway doesn't do it.
</blockquote>

</div>
</div>

---
---

## PasteGuard - Data Privacy

<div class="cols">
<div>

### How It Works

```
Client Request
    |
    v
[Extract Text Spans]
 system prompt, messages, tool_result
    |
    v
[Secret Detection] (regex, 12 types)
 API keys, JWT, PEM, AWS, GitHub...
    |
    v
[PII Detection] (Presidio NLP)
 Person, Email, Phone (conf >= 0.7)
    |
    v
[Mask] -> [[API_KEY_SK_1]], [[PERSON_2]]
    |
    v
[Proxy] -> Upstream sees placeholders only
    |
    v
[Unmask] -> Restore real values in response
  Non-stream: bulk replace
  Stream: per SSE chunk
```

</div>
<div>

### 12 Secret Entity Types

| Entity | Example |
|---|---|
| OpenSSH Private Key | `-----BEGIN OPENSSH...` |
| PEM Private Key | `-----BEGIN RSA...` |
| API Key (sk-) | `sk-proj-xxxx...` |
| AWS Access Key | `AKIAxxxxxxxxxxxxxxxx` |
| GitHub PAT | `ghp_xxxxxxxxxxxxxxxx` |
| GitLab Token | `glpat-xxxxxxxxxxxxxx` |
| JWT Token | `eyJhbGciOi...` |
| Bearer Token | `Bearer xxxxxxxx...` |
| ENV Password | `PASSWORD=...` |
| ENV Secret | `_SECRET=...` |
| Connection String | `postgres://user:pass@` |
| Thai National ID | `[1-8]xxxxxxxxxxxxx` |

<br>

<div class="feat" style="border-left: 3px solid #4ade80;">
<h4>Parallel Processing</h4>
<p>Each span runs in its own goroutine. 10 spans: ~10ms vs ~100ms sequential.</p>
</div>

<div class="feat" style="border-left: 3px solid #8b5cf6;">
<h4>4 Prometheus Metrics</h4>
<p><code>mask_duration_seconds</code> by phase<br>
<code>secrets_detected_total</code> by type<br>
<code>pii_detected_total</code> by type<br>
<code>mask_requests_total</code> by result</p>
</div>

</div>
</div>


## Claude Code Tool Loop

<div style="max-width: 80%; margin: 0 auto;">

<div class="flow-box" style="border-left: 3px solid #06b6d4; text-align: center;">
<strong>Turn 1:</strong> User asks "Read main.go"
</div>
<div class="flow-arrow">&darr;</div>

<div class="flow-box" style="border-left: 3px solid #6366f1;">
POST <code>/v1/messages</code> &rarr; Gateway &rarr; Upstream<br>
<span style="color: #64748b; font-size: 0.85em;">tools: [Read, Edit, Bash, Write, Grep, Glob, MCP...]</span>
</div>
<div class="flow-arrow">&darr;</div>

<div class="flow-box" style="border-left: 3px solid #8b5cf6;">
Response: <code>tool_use</code> { name: "Read", input: { file_path: "/path/main.go" } }<br>
<span style="color: #64748b; font-size: 0.85em;">stop_reason: "tool_use"</span>
</div>
<div class="flow-arrow">&darr;</div>

<div class="flow-box" style="border-left: 3px solid #fbbf24; text-align: center;">
Client executes <strong>Read</strong> locally (reads real file)
</div>
<div class="flow-arrow">&darr;</div>

<div class="flow-box" style="border-left: 3px solid #6366f1;">
POST <code>/v1/messages</code> &rarr; Gateway &rarr; Upstream<br>
<span style="color: #64748b; font-size: 0.85em;">messages: [user, assistant(tool_use), user(tool_result)]</span>
</div>
<div class="flow-arrow">&darr;</div>

<div class="flow-box" style="border-left: 3px solid #4ade80; text-align: center;">
Response: <code>end_turn</code> &mdash; Loop complete, show result to user
</div>

</div>

---

## Middleware Stack

<div class="cols">
<div style="flex: 1.3;">

```
Request -> gateway (:8080)
  |
  1  SecurityHeaders
     X-Content-Type-Options: nosniff
     X-Frame-Options: DENY
  |
  2  CorrelationID
     Generate / propagate X-Correlation-ID
  |
  3  RealIP
     CF-Connecting-IP > X-Real-IP > X-Forwarded-For
  |
  4  IPFilter
     Optional CIDR whitelist / blacklist
  |
  5  Logging
     Structured JSON
  |
  6  Metrics
     Latency + connections + status
  |
  7  Rate Limiter
     Global (100/min) + Per-agent (5/min)
  |
  8  Login Limiter
     5 attempts / 15min per IP
  |
  v
Route Handler
```

</div>
<div style="flex: 0.7;">

<br>

<div class="feat">
<h4>Fail-Open Design</h4>
<p>Rate limiter unreachable? Requests pass through. Never a SPOF.</p>
</div>

<div class="feat">
<h4>Identity Extraction</h4>
<p><code>/v1/messages</code>: <code>x-api-key</code> or <code>Authorization</code> header</p>
<p>Other routes: <code>?agent_id=</code> query param</p>
</div>

<div class="feat">
<h4>Error Format</h4>
<p>Anthropic-compatible for <code>/v1/messages</code>:</p>
<p><code>{"type":"rate_limit_error"}</code></p>
</div>

</div>
</div>

---

## Adaptive Concurrency Limiter

<div class="cols">
<div>

### Gradient Algorithm

Inspired by **Envoy** gradient controller + **Netflix** concurrency limits.

```
On 429/503:
  peakBefore429 = current limit
  limit = max(min, limit * 0.5)    // halve

On success (every 5th):
  gradient = (minRTT + buffer) / sampleRTT
  gradient = clamp(0.8, 2.0)
  limit = gradient * limit + sqrt(limit)
  limit = min(maxLimit, limit)
  cap at peakBefore429 - 1
```

<br>

### Key Properties

- **Lock-free hot path**: atomic CAS on per-model tryAcquire
- **Signal-based waiting**: sync.Cond, zero CPU while blocked
- **sync.Pool**: candidate slices reused, zero alloc on fallback
- **Learned ceiling**: remembers peak limit before 429, decays after 5 min

</div>
<div>

### Limit Discovery

```
Real upstream limit = 4, initial = 1

  1 -> 2 -> 3 -> 4 -> 5
                     429! peak=5

  5 -> 2 (halved)
       |
  2 -> 3 -> 4 (cap at peak-1)
                converged

After 5 min: ceiling decays, re-probing allowed
```

<br>

### Configuration

| Env Var | Default | Description |
|---|---|---|
| <code>UPSTREAM_MODEL_LIMITS</code> | (per model) | Concurrent request limits |
| <code>UPSTREAM_GLOBAL_LIMIT</code> | 9 | Total concurrent cap |
| <code>UPSTREAM_PROBE_MULTIPLIER</code> | 5 | maxLimit = initial * this |

<br>

<blockquote>
Auto-discovers real upstream limit. Starts conservative, probes upward, backs off on 429.
</blockquote>

</div>
</div>

---

## Provider Registry

<div class="cols">
<div>

### API Key Auth (13 providers)

| Provider | Upstream | Default Model |
|---|---|---|
| **Anthropic** | api.anthropic.com | claude-sonnet-4 |
| **OpenAI** | api.openai.com | gpt-4o |
| **Google Gemini** | generativelanguage.googleapis.com | gemini-2.0-flash |
| **OpenRouter** | openrouter.ai/api/v1 | openai/gpt-4o |
| **DeepSeek** | api.deepseek.com | deepseek-chat |
| **Kimi** | api.moonshot.cn/v1 | moonshot-v1-8k |
| **Hugging Face** | api-inference.huggingface.co | model repo ID |
| **Ollama** | localhost:11434 | local model |
| **AGY, Cursor, CodeBuddy, Kilo** | Various | Various |

</div>
<div>

### OAuth / Device Code (4 providers)

| Provider | Auth Type | Flow |
|---|---|---|
| **Claude (OAuth)** | Auth code + PKCE | platform.claude.com |
| **Gemini (OAuth)** | Auth code | Google Code Assist |
| **GitHub Copilot** | Device code | github.com/login/device |
| **Qwen (Aliyun)** | Device code | dashscope.aliyuncs.com |

<br>

<div class="feat" style="border-left: 3px solid #8b5cf6;">
<h4>Token Store</h4>
<p>OAuth tokens persisted in Dragonfly. Background RefreshWorker auto-refreshes expiring tokens. No manual intervention.</p>
</div>

</div>
</div>

---

## OAuth Flows

<div class="cols">
<div>

### Claude OAuth (PKCE)

```
Dashboard -> "Connect Claude"
  |
  v
Browser opens platform.claude.com
  |-- authorize URL with PKCE code_challenge
  |-- User logs in, grants access
  |-- Redirect back with auth code
  |
  v
Gateway exchanges code -> tokens
  |-- access_token + refresh_token
  |-- Store in Dragonfly
  |-- RefreshWorker auto-refreshes
```

<br>

**Scopes**: org:create_api_key, user:inference, user:sessions:claude_code, user:profile

</div>
<div>

### Gemini OAuth (Code Assist)

```
Dashboard -> "Connect Gemini"
  |
  v
Browser opens accounts.google.com
  |-- Google OAuth consent
  |-- Redirect back with code
  |
  v
Gateway exchanges code -> tokens
  |-- Upstream: cloudcode-pa.googleapis.com
  |-- (not generativelanguage.googleapis.com)
```

<br>

### GitHub Copilot (Device Code)

```
Dashboard -> "Connect Copilot"
  |
  v
Show device code + verification URL
  |-- User visits github.com/login/device
  |-- Enters code, authorizes
  |-- Gateway polls for token
```

</div>
</div>

---

## Key Rotation & Provider Fallback

<div class="cols">
<div>

### Key Pool Behavior

```
Keys: [key1, key2, key3]

  Request
    |
    v
  key1 selected (random)
    |
    v
  429 Rate Limit!
    |
    +-- cooldown(key1, 60s)
    +-- available: [key2, key3]
    +-- retry with key2
    |
    v
  200 OK

  ... 60s later ...

  key1 auto-recovers
  pool: [key1, key2, key3]
```

</div>
<div>

### Provider Fallback Chain

```
Job arrives for provider X
  |
  +-- Provider X
  |     |-- 429 -> rotate key, retry X
  |     |-- Other error -> next provider
  |
  +-- Provider Y
  |     |-- 429 -> rotate key, retry Y
  |     |-- Other error -> next provider
  |
  +-- ... more providers
  |
  +-- All fail?
        |-- retries < 3: backoff + jitter, re-enqueue
        |-- retries >= 3: store error result
```

<br>

**Throughput scaling:**

```
1 Anthropic key         =  50 RPM
3 Anthropic keys        = 150 RPM
+ 2 OpenAI keys         = 270 RPM
+ Gemini free tier      =  15 RPM
```

</div>
</div>

---
---

## Token Optimization

<div class="cols">
<div>

### 6 Techniques (Progressive)

```
Request enters
    |
    v
[1. Token Estimation] -- chars/ratio by content type
    |   Code: /2.5  JSON: /2.8  MD: /3.5  Text: /4.0
    v
[2. Model Lookup] -- static context window map
    |   Claude: 200K   GPT-4o: 128K   Gemini: 1M
    v
[3. Whitespace Opt] -- double spaces, trailing WS
    |   ~3-5% token savings, zero cost
    v
[4. Deduplication] -- sentence hash + near-duplicate
    |   3.5% reduction, code blocks preserved
    v
[5. Budget Check] -- % of context limit used
    |   Green (<50%): normal
    |   Yellow (50-75%): apply opt + dedup
    |   Red (>75%): force truncation
    v
[6. Head/Tail Truncation] -- emergency only
        40% head + 60% tail preserved
        71.4% token reduction
```

</div>
<div>

### Measured Results

| Technique | Before | After | Saved |
|-----------|--------|-------|-------|
| Whitespace | 1253 tok | 1252 tok | 0.1% |
| Dedup | 1253 tok | 1209 tok | **3.5%** |
| Head/Tail Trunc | 4423 tok | 1265 tok | **71.4%** |

<br>

### Budget Thresholds

| Usage | Level | Action |
|-------|-------|--------|
| < 50% | GREEN | Normal operation |
| 50-75% | YELLOW | WS opt + dedup |
| 80% | Auto | Proactive optimization |
| > 75% | RED | Force truncation |
| 90% | Emergency | Aggressive truncation |

<br>

<div class="feat" style="border-left: 3px solid #06b6d4;">
<h4>Prometheus Metrics</h4>
<p><code>optimizer_chars_saved_total{technique}</code><br>
<code>optimizer_runs_total{technique}</code></p>
</div>

</div>
</div>


## Profile-Based Routing & Account Pool

<div class="cols">
<div>

### Routing Flow

```
Request with X-Profile: meow
  |
  v
Handler.Messages()
  |-- Extract X-Profile header
  |-- Lookup profile:{name} from Redis
  |     |
  |     +-- Found:
  |     |     Override model, apiKey, baseUrl
  |     |     Skip key pool + adaptive limiter
  |     |     Proxy to profile.baseUrl
  |     |
  |     +-- Not found:
  |           Log warning, fall through
  |
  +-- No header:
        Normal routing (key pool + limiter)
```

<br>

### Token Authentication

```
POST /v1/profiles/{name}/tokens
  |-- Generate arl_<64-hex> token
  |-- Optional TTL (expiresIn seconds)
  |-- Reverse lookup: profile_token:<tok> -> name
  |
DELETE /v1/profiles/{name}/tokens/{keyName}
  |-- Revoke token
```

</div>
<div>

### Profile CRUD + Extras

| Method | Path | Description |
|---|---|---|
| GET | <code>/v1/profiles</code> | List all |
| POST | <code>/v1/profiles</code> | Create |
| GET | <code>/v1/profiles/{name}</code> | Get by name |
| PUT | <code>/v1/profiles/{name}</code> | Update |
| DELETE | <code>/v1/profiles/{name}</code> | Delete + revoke tokens |
| POST | <code>/v1/profiles/{name}/copy</code> | Copy (clears API key) |
| POST | <code>/v1/profiles/{name}/export</code> | Export bundle |
| POST | <code>/v1/profiles/import</code> | Import from bundle |
| GET | <code>/v1/profiles/recommended-models</code> | Per-provider list |

<br>

### Recommended Models per Provider

| Provider | Flagship | Standard | Light |
|---|---|---|---|
| Claude OAuth | opus-4-7 | sonnet-4 | haiku-4 |
| Gemini OAuth | 2.5-pro | 2.5-flash | 2.0-flash |
| OpenAI | o3 | gpt-4o | gpt-4o-mini |
| DeepSeek | deepseek-chat | - | - |

<br>

<span class="chip">meow</span>
<span class="chip">fast</span>
<span class="chip">cheap</span>
<span class="chip">premium</span>

<p style="color: #64748b; font-size: 0.78em;">Per-profile cost tracking: <code>profile_cost_total{profile, model}</code></p>

</div>
</div>

---

## Quota Enforcement & Usage Tracking

<div class="cols">
<div>

### Quota Enforcement

```
Messages() handler
  |
  Resolve model + provider
  |
  CheckQuota(provider, accountID, model)
  |
  +-- >= 95%   429 hard block
  |             Anthropic error format
  +-- >= 80%   WS quota-warning event
  |             (soft warning, continue)
  +-- <  80%   proceed normally
  +--  Error   fail-open (log + continue)
```

<br>

<blockquote>
Fail-open: if Redis is down, quota check logs a warning and the request proceeds. Quota never blocks on infrastructure failure.
</blockquote>

</div>
<div>

### Usage Analytics (Redis)

Auto-recorded via <code>metrics.RecordTokens()</code> callback on every request.

| Bucket | Key Pattern | TTL |
|---|---|---|
| Hourly | <code>usage:hourly:YYYY-MM-DDTHH</code> | 48h |
| Daily | <code>usage:daily:YYYY-MM-DD</code> | 35d |
| Monthly | <code>usage:monthly:YYYY-MM</code> | 400d |
| Session | <code>usage:sessions:YYYY-MM-DD</code> | 35d |

<br>

### Query Endpoints

| Endpoint | Description |
|---|---|
| <code>/v1/usage/summary</code> | Aggregated totals |
| <code>/v1/usage/hourly</code> | Hourly breakdown |
| <code>/v1/usage/daily</code> | Last 30 days |
| <code>/v1/usage/models</code> | Per-model breakdown |
| <code>/v1/usage/sessions</code> | Session-level usage |

</div>
</div>

---
---

## Cost Calculator

<div class="cols">
<div>

### Per-Request Cost Engine

```
Request completes
    |
    v
metrics.RecordTokens(model, input, output)
    |
    +-- inputCost = input / 1M * price.input
    +-- outputCost = output / 1M * price.output
    +-- cost = inputCost + outputCost
    |
    v
Auto-record to Redis time buckets:
  usage:hourly:YYYY-MM-DDTHH  (TTL 48h)
  usage:daily:YYYY-MM-DD      (TTL 35d)
  usage:monthly:YYYY-MM       (TTL 400d)
  usage:sessions:YYYY-MM-DD   (TTL 35d)
    |
    v
Per-profile tracking:
  profile_cost_total{profile, model}
  profile_token_input_total
  profile_token_output_total
```

</div>
<div>

### Model Pricing (USD / 1M tokens)

| Model | Input | Output |
|-------|-------|--------|
| claude-opus-4-7 | $15.00 | $75.00 |
| claude-sonnet-4 | $3.00 | $15.00 |
| claude-haiku-4 | $0.80 | $4.00 |
| gpt-4o | $2.50 | $10.00 |
| gemini-2.5-pro | $1.25 | $10.00 |
| deepseek-chat | $0.27 | $1.10 |

<p style="color: #64748b; font-size: 0.75em;">Configurable via <code>MODEL_PRICING</code> env var</p>

<br>

### Query Endpoints

| Endpoint | Description |
|---|---|
| <code>/v1/usage/summary</code> | Aggregated totals |
| <code>/v1/usage/hourly</code> | Per-hour breakdown |
| <code>/v1/usage/daily</code> | Last 30 days |
| <code>/v1/usage/monthly</code> | Last 12 months |
| <code>/v1/usage/models</code> | Per-model breakdown |
| <code>/v1/usage/profiles</code> | Per-profile aggregated |

</div>
</div>


## Observability

<div class="cols">
<div>

### 21 Prometheus Metrics

| Metric | Type |
|---|---|
| <code>request_duration_seconds</code> | Histogram |
| <code>token_input/output_total</code> | Counter |
| <code>cost_total</code> | Counter |
| <code>adaptive_limit</code> | Gauge |
| <code>ttfb_seconds</code> | Histogram |
| <code>model_fallback_total</code> | Counter |
| <code>anomaly_total</code> | Counter |
| <code>upstream_429_total</code> | Counter |
| <code>upstream_retries_total</code> | Counter |
| <code>go_goroutines</code> | Gauge |
| <code>dragonfly_up</code> | Gauge |

<br>

Scrape interval: **5s**

</div>
<div>

### Anomaly Detection

Z-score ring buffer (1000 samples):

| z-score | Severity |
|---|---|
| > 4.0 | Critical |
| > 3.0 | High |
| > 2.0 | Medium |

5+ consecutive = **Sustained** anomaly

<br>

### 6 WebSocket Events

<div style="display: flex; flex-wrap: wrap; gap: 5px;">
<span class="chip chip-green" style="font-size: 0.75em;">request-completed</span>
<span class="chip" style="font-size: 0.75em; border-color: #f87171; color: #f87171;">request-error</span>
<span class="chip chip-amber" style="font-size: 0.75em;">anomaly-detected</span>
<span class="chip chip-purple" style="font-size: 0.75em;">request-queued</span>
<span class="chip chip-amber" style="font-size: 0.75em;">quota-warning</span>
<span class="chip" style="font-size: 0.75em;">config-changed</span>
</div>

<br>

### Stack

```
gateway -> OTel Collector -> Prometheus -> Grafana
               (:4317)         (:9090)      (:3000)
```

</div>
</div>

---

## Dashboard

<div class="cols">
<div>

### 10 Pages

<div class="feat" style="border-top: 2px solid #06b6d4;">
<p><strong>Overview</strong> - 4 stat cards, global capacity bar, model utilization, live SVG Key Flow Monitor, event timeline</p>
</div>

<div class="feat" style="border-top: 2px solid #8b5cf6;">
<p><strong>Health</strong> - Circular gauge, 6 auto checks (dragonfly, rate-limiter, prometheus, key-pool, upstream, memory)</p>
</div>

<div class="feat" style="border-top: 2px solid #4ade80;">
<p><strong>Model Limits</strong> - Per-model: in-flight, limit, ceiling, RTT (min + EWMA), 429s, adaptive/pinned</p>
</div>

<div class="feat" style="border-top: 2px solid #fbbf24;">
<p><strong>Key Pool</strong> - RPM, success/error, error rate, status (active/cooldown/error), passthrough mode</p>
</div>

<div class="feat" style="border-top: 2px solid #f472b6;">
<p><strong>Analytics</strong> - Tokens, cost, dual-axis trend, cost-by-model bars, donut distribution, 24h breakdown</p>
</div>

<div class="feat" style="border-top: 2px solid #06b6d4;">
<p><strong>Privacy</strong> - Masked requests, secrets/PII by type, mask duration p95 bar/line charts</p>
</div>

</div>
<div>

### Tech Stack

<span class="chip">React 18</span>
<span class="chip">TypeScript</span>
<span class="chip">Vite</span>
<span class="chip">Tailwind CSS</span>
<span class="chip">shadcn/ui</span>
<span class="chip">Recharts</span>

<br>

### Key Flow Monitor (Live SVG)

```
  API Keys         Gateway           Models
+---------+     +============+    +-----------+
| Claude  | --> |            | -> | sonnet-4  |
| OpenAI  | --> |  gateway   | -> | gpt-4o    |
| Gemini  | --> |  :8080     | -> | gemini-2.0|
+---------+     +============+    +-----------+

  Hover key   -> highlight all model paths
  Hover model -> highlight all key paths
  Live pulse  -> real-time indicator
```

<br>

### UX Features

| Feature | Shortcut |
|---|---|
| Privacy Mode (blur sensitive) | <code>Cmd+P</code> |
| Command Palette (fuzzy) | <code>Cmd+K</code> |
| Navigate pages 1-9 | <code>1-9</code> keys |
| Toggle sidebar | <code>Cmd+B</code> |
| Refresh data | <code>Cmd+R</code> |

<br>

<p style="color: #64748b; font-size: 0.78em;">i18n: EN + TH (90+ keys) &middot; Polling: 5/10/30/60s &middot; WebSocket: 6 real-time events &middot; Dark/Light/System theme</p>

</div>
</div>
---

## Claude Code Compatibility

<div class="cols">
<div>

### All Features Work

| Feature | Status |
|---|---|
| Read / Edit / Bash / Write | <span class="ok">PASS</span> |
| Grep / Glob | <span class="ok">PASS</span> |
| Streaming (SSE) | <span class="ok">PASS</span> |
| Skills (slash commands) | <span class="ok">PASS</span> |
| Memory | <span class="ok">PASS</span> |
| Extended thinking | <span class="ok">PASS</span> |
| Image / Vision | <span class="ok">PASS</span> |
| MCP Servers | <span class="ok">PASS</span> |
| Multi-turn conversation | <span class="ok">PASS</span> |
| NotebookEdit | <span class="ok">PASS</span> |
| TodoRead / TodoWrite | <span class="ok">PASS</span> |

</div>
<div>

### Setup (Direct)

```json
// ~/.claude/settings.json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:8080",
    "ANTHROPIC_AUTH_TOKEN": "your-api-key"
  }
}
```

### Setup (Profile-based / Docker)

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://gateway:8080",
    "ANTHROPIC_API_KEY": "<profile-token>"
  }
}
```

<br>

### Why it works

```
Skills    -> expanded at client
Memory    -> local files
Artifacts -> client-side render
MCP       -> client-side register
Gateway   -> pass-through only
```

</div>
</div>

---

## API Routes Summary

| Method | Path | Description |
|---|---|---|
| POST | <code>/v1/messages</code> | Sync transparent proxy |
| POST | <code>/v1/chat/completions</code> | Async enqueue |
| GET | <code>/v1/results/{id}</code> | Poll async result |
| GET | <code>/v1/profiles</code> | Profile CRUD |
| GET | <code>/v1/usage/*</code> | Usage analytics (5 endpoints) |
| GET | <code>/quota/{provider}</code> | Per-account quota |
| GET | <code>/v1/overview</code> | Dashboard summary |
| GET | <code>/v1/health/detailed</code> | 6 health checks |
| GET/PUT | <code>/v1/config</code> | Server config management |
| GET/PUT | <code>/v1/thinking</code> | Thinking budget config |
| GET | <code>/v1/limiter-status</code> | Adaptive limiter state |
| POST | <code>/v1/limiter-override</code> | Pin/clear model limit |
| GET | <code>/ws</code> | WebSocket (6 event types) |
| POST | <code>/v1/auth/*</code> | OAuth + API key auth flows |
| GET | <code>/admin/*</code> | Dashboard SPA |

---

## Resources & Ports

<div class="cols">
<div>

### Service Limits

| Service | Memory | CPU |
|---|---|---|
| gateway | 512M | 1.0 |
| rate-limiter | 768M | 1.0 |
| dragonfly | 6G | 2.0 |
| worker | 1G | 2.0 |
| prometheus | 512M | 0.5 |
| grafana | 256M | 0.5 |
| otel | 256M | 0.5 |

</div>
<div>

### Ports

| Port | Service | Access |
|---|---|---|
| **8080** | API Gateway | External |
| **3000** | Grafana | External |
| 6379 | Dragonfly | Internal |
| 9090 | Worker / Prometheus | Internal |
| 4317/4318 | OTel Collector | Internal |

<br>

### Dragonfly Tuning

```
--maxmemory=2gb
--proactor_threads=4
--cache_mode=true
--pipeline_squash=10
```

</div>
</div>

---

<!-- _class: lead -->

<br>

<h2>Highlights</h2>

<br>

<div style="display: flex; gap: 1em; justify-content: center; margin-top: 0.8em;">
<div class="stat-card" style="flex: 1; max-width: 160px;">
  <div class="num">17</div>
  <div class="lbl">Providers</div>
</div>
<div class="stat-card" style="flex: 1; max-width: 160px;">
  <div class="num">12</div>
  <div class="lbl">Secret Types</div>
</div>
<div class="stat-card" style="flex: 1; max-width: 160px;">
  <div class="num">21</div>
  <div class="lbl">Metrics</div>
</div>
<div class="stat-card" style="flex: 1; max-width: 160px;">
  <div class="num">6</div>
  <div class="lbl">Token Opt</div>
</div>
<div class="stat-card" style="flex: 1; max-width: 160px;">
  <div class="num">10</div>
  <div class="lbl">Dash Pages</div>
</div>
</div>

<br>

<div style="display: flex; gap: 1em; justify-content: center;">
<div class="stat-card" style="flex: 1; max-width: 180px;">
  <div class="num" style="font-size: 1.5em;">Lock-free</div>
  <div class="lbl">Hot Path (atomic CAS)</div>
</div>
<div class="stat-card" style="flex: 1; max-width: 180px;">
  <div class="num" style="font-size: 1.5em;">Fail-open</div>
  <div class="lbl">Rate Limiter SPOF-safe</div>
</div>
<div class="stat-card" style="flex: 1; max-width: 180px;">
  <div class="num" style="font-size: 1.5em;">Zero</div>
  <div class="lbl">Body Modification</div>
</div>
<div class="stat-card" style="flex: 1; max-width: 180px;">
  <div class="num" style="font-size: 1.5em;">71.4%</div>
  <div class="lbl">Token Savings (truncation)</div>
</div>
</div>

---

<!-- _class: cover -->
<!-- _paginate: false -->

<br><br>

<h1 style="font-size: 2.8em;">Thank You</h1>

<hr class="divider"><hr class="divider">

<p class="subtitle" style="font-size: 1.3em;">AI Gateway</p>

<br>

<span class="chip chip-purple" style="font-size: 0.9em;">github.com/klxhunter/agent-rate-limit</span>

<br><br>

<p style="color: #475569; font-size: 1em;">Questions?</p>
