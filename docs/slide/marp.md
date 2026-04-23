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
<span class="chip chip-purple">Adaptive Limiter</span>
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
<h4>Adaptive Concurrency</h4>
<p>Envoy-inspired gradient limiter auto-discovers optimal throughput</p>
</div>

</div>
</div>

---

## System Architecture

<div class="cols" style="align-items: flex-start;">
<div style="flex: 1.2;">

<div class="flow-box" style="border-left: 3px solid #06b6d4;">
<strong>Clients</strong> &mdash; Claude Code, AI Agents, CI/CD
</div>
<div class="flow-arrow">&darr;</div>

<div class="flow-box" style="border-left: 3px solid #6366f1;">
<strong>arl-gateway :8080</strong> (Go)
<span style="font-size: 0.85em; display: block; margin-top: 4px; color: #64748b;">
  Security &middot; Rate Limit &middot; Key Pool<br>
  Adaptive Limiter &middot; Quota &middot; Proxy
</span>
</div>

<div class="cols" style="gap: 0.5em; margin-top: 4px;">
<div style="flex: 1;">
<div class="flow-arrow">&darr; sync</div>
<div class="flow-box" style="border-left: 3px solid #4ade80; font-size: 0.78em;">
<strong>Transparent Proxy</strong><br>
SSE streaming to upstream
</div>
</div>
<div style="flex: 1;">
<div class="flow-arrow">&darr; async</div>
<div class="flow-box" style="border-left: 3px solid #fbbf24; font-size: 0.78em;">
<strong>Dragonfly Queue</strong><br>
arl-worker (50 coroutines)
</div>
</div>
</div>

</div>
<div style="flex: 0.8;">

<div class="feat">
<h4>Sync Mode</h4>
<p><code>/v1/messages</code><br>
Real-time SSE streaming<br>
Transparent proxy to upstream</p>
</div>

<div class="feat">
<h4>Async Mode</h4>
<p><code>/v1/chat/completions</code><br>
Queue + Worker + Cache<br>
Poll <code>/v1/results/{id}</code></p>
</div>

<div class="feat">
<h4>17 Providers</h4>
<p>Auto-fallback chain<br>
Key rotation with 60s cooldown<br>
Per-provider RPM limiting</p>
</div>

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
Request -> arl-gateway (:8080)
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

### Learnings

- Gradient > 1.0 = system faster than baseline, increase
- Gradient < 1.0 = RTT rising, increase conservatively
- Learned ceiling decays after 5 minutes
- 5s cooldown after any 429

</div>
<div>

### Series-Based Routing

```
Series 5 (preferred)
  glm-5.1     (limit 1, priority 100)
  glm-5-turbo (limit 1, priority  90)
  glm-5       (limit 2, priority  80)
  = 4 slots

Series 4 (fallback)
  glm-4.7 (limit 2, priority 70)
  glm-4.6 (limit 3, priority 60)
  glm-4.5 (limit 10, priority 50)
  = 15 slots

Vision (auto-routed)
  glm-4.6v, glm-4.5v, flashx, flash
  = 24 slots

Global cap: 9 concurrent
```

<br>

<blockquote>
Total model capacity: <strong>19</strong> (text) + <strong>24</strong> (vision) = <strong>43 slots</strong>, capped by global limit of 9.
</blockquote>

</div>
</div>

---

## Model Fallback Flow

```
Request: { "model": "glm-5" }
  |
  +-- 1. Wait for global slot
  |     sync.Cond signal-based (zero CPU while waiting)
  |
  +-- 2. Try glm-5
  |     Non-blocking atomic CAS
  |     Available? -> use it, done
  |
  +-- 3. Same-series round-robin (series 5)
  |     glm-5.1 -> glm-5-turbo -> glm-5
  |     Pooled candidate slices (sync.Pool, zero alloc)
  |     Any available? -> use it
  |
  +-- 4. Latency pressure check
  |     EWMA RTT > 1.5x minRTT for majority of series 5?
  |     YES -> spill to series 4 (glm-4.7, glm-4.6, glm-4.5)
  |     NO  -> stay in series 5, wait for slot
  |
  +-- 5. All full
        Release global slot (prevent starvation)
        Block-wait on requested model (30s timeout)
        Re-acquire global slot
```

<br>

<div class="cols">
<div class="feat" style="border-top: 3px solid #06b6d4;">
<h4>Lock-Free Hot Path</h4>
<p>Per-model <code>tryAcquire</code> uses atomic CAS. Mutex only serializes limit adjustments (cold path).</p>
</div>
<div class="feat" style="border-top: 3px solid #8b5cf6;">
<h4>sync.Pool</h4>
<p>Candidate slices pooled and reused. Zero per-request heap allocation on fallback path.</p>
</div>
</div>

---

## Limit Discovery Example

```
Real upstream limit = 4, initial = 1, probeMultiplier = 5

  1 ---> 2 ---> 3 ---> 4 ---> 5
  ok     ok     ok     ok     429!
                         ^
                         peakBefore429 = 5

  5 ---> 2     (halved)
          |
          v
  2 ---> 3 ---> 4 (cap at peak-1)
  ok     ok     ok
                  ^
                  converged at 4

After 5 min stability: ceiling decays, re-probing allowed
```

<br>

| Model | Initial | Max (x5) | Priority | Series |
|---|---|---|---|---|
| glm-5.1 | 1 | 5 | 100 | 5 |
| glm-5-turbo | 1 | 5 | 90 | 5 |
| glm-5 | 2 | 10 | 80 | 5 |
| glm-4.7 | 2 | 10 | 70 | 4 |
| glm-4.6 | 3 | 15 | 60 | 4 |
| glm-4.5 | 10 | 50 | 50 | 4 |

---

<!-- _class: lead -->

<h2>17 Providers</h2>

<br>

<div style="display: flex; flex-wrap: wrap; gap: 8px; justify-content: center; max-width: 90%; margin: 0 auto;">
<span class="chip" style="font-size: 0.9em;">Z.AI (GLM)</span>
<span class="chip" style="font-size: 0.9em;">Anthropic</span>
<span class="chip" style="font-size: 0.9em;">OpenAI</span>
<span class="chip" style="font-size: 0.9em;">Google Gemini</span>
<span class="chip" style="font-size: 0.9em;">OpenRouter</span>
<span class="chip chip-purple" style="font-size: 0.9em;">GitHub Copilot</span>
<span class="chip" style="font-size: 0.9em;">DeepSeek</span>
<span class="chip chip-purple" style="font-size: 0.9em;">Claude OAuth</span>
<span class="chip chip-purple" style="font-size: 0.9em;">Gemini OAuth</span>
<span class="chip chip-purple" style="font-size: 0.9em;">Qwen (Aliyun)</span>
<span class="chip" style="font-size: 0.9em;">Kimi</span>
<span class="chip" style="font-size: 0.9em;">Hugging Face</span>
<span class="chip" style="font-size: 0.9em;">Ollama</span>
<span class="chip" style="font-size: 0.9em;">AGY</span>
<span class="chip" style="font-size: 0.9em;">Cursor</span>
<span class="chip" style="font-size: 0.9em;">CodeBuddy</span>
<span class="chip" style="font-size: 0.9em;">Kilo</span>
</div>

<br>

| | API Key | OAuth / Device Code |
|---|---|---|
| **Auth** | 13 providers | 4 providers (Claude, Gemini, Copilot, Qwen) |
| **SDK** | Anthropic, OpenAI, Google AI, native | PKCE + device code flows |
| **Fallback** | Automatic, key-gated | Manual via dashboard |

<br>

<blockquote>
Only providers with configured keys join the fallback chain. Add a key = instant integration, zero code changes.
</blockquote>

---

## Key Rotation & Fallback

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
Job arrives
  |
  +-- Provider A
  |     |-- 429 -> rotate key, retry A
  |     |-- Other error -> next provider
  |
  +-- Provider B
  |     |-- 429 -> rotate key, retry B
  |     |-- Other error -> next provider
  |
  +-- ... more providers
  |
  +-- All fail?
        |-- retries < 3: exponential backoff + jitter, re-enqueue
        |-- retries >= 3: store error result
```

<br>

**Throughput scaling:**

```
1 GLM key              =   5 RPM
3 GLM keys             =  15 RPM
3 GLM + 2 OpenAI       = 135 RPM
All providers           = 400+ RPM
```

</div>
</div>

---

## Vision Auto-Routing

<div class="cols">
<div>

```
Client sends image request
  |
  v
Gateway detects image content
  |-- analyzeImagePayload()
  |     count images + total base64 bytes
  |
  |-- selectVisionModel()
  |     score = totalKB + (count * 300)
  |
  |     score <= 2000 && < 3 imgs
  |       -> glm-4.6v (10 slots)
  |
  |     score > 2000 || >= 3 imgs
  |       -> glm-4.6v-flashx (3 slots)
  |
  |-- Format conversion
  |     Anthropic -> Zhipu (OpenAI-compat)
  |     image -> image_url
  |     system -> prepend to user msg
  |     strip unsupported types
  |
  |-- Send to native Zhipu endpoint
  |-- SSE stream conversion (real-time)
  |
  v
Client gets Anthropic-format response
```

</div>
<div>

### Dual-Path

<div class="feat" style="border-left: 3px solid #06b6d4;">
<h4>Text Path</h4>
<p><code>api.z.ai/api/anthropic</code><br>
Anthropic-compatible endpoint</p>
</div>

<div class="feat" style="border-left: 3px solid #8b5cf6;">
<h4>Vision Path</h4>
<p><code>open.bigmodel.cn/api/paas/v4</code><br>
Native Zhipu endpoint with format conversion</p>
</div>

<br>

### Supported Formats

- Anthropic base64: <code>{"type":"image","source":{...}}</code>
- Anthropic URL: <code>{"type":"image","source":{"type":"url"}}</code>
- Auto-converted to Zhipu <code>image_url</code> format

<br>

<blockquote>
Client does nothing different. Gateway handles everything transparently.
</blockquote>

</div>
</div>

---

## Profile & Quota

<div class="cols">
<div>

### Profile-Based Routing

<div class="flow-box" style="border-left: 3px solid #06b6d4;">
<strong>X-Profile Header</strong>
</div>

```
Request with X-Profile: meow
  |
  Lookup profile:{name} in Redis
  |
  +-- Found:
  |     Override model, apiKey, baseUrl
  |     Skip key pool + model fallback
  |     Proxy to profile.baseUrl
  |
  +-- Not found:
        Fall through to normal routing
```

<br>

<span class="chip">meow</span>
<span class="chip">fast</span>
<span class="chip">cheap</span>
<span class="chip">premium</span>

<p style="color: #64748b; font-size: 0.8em;">Route sessions to different providers without changing client config</p>

</div>
<div>

### Quota Enforcement

```
CheckQuota(provider, account, model)
  |
  +-- >= 95%   429 hard block
  +-- >= 80%   WS warning (continue)
  +-- <  80%   proceed normally
  +--  Error   fail-open
```

<br>

### Usage Buckets (Redis)

| Bucket | TTL |
|---|---|
| Hourly | 48h |
| Daily | 35d |
| Monthly | 400d |
| Session | 35d |

<br>

Auto-recorded via <code>metrics.RecordTokens()</code> callback on every request.

</div>
</div>

---

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

</div>
</div>

---

## Dashboard

<div class="cols">
<div>

<div class="feat" style="border-top: 3px solid #06b6d4;">
<h4>Overview</h4>
<p>Status cards, global capacity bar, model utilization, key flow monitor (live SVG), event timeline</p>
</div>

<div class="feat" style="border-top: 3px solid #8b5cf6;">
<h4>Health</h4>
<p>6 automated checks: dragonfly, rate-limiter, prometheus, key-pool, upstream, memory</p>
</div>

<div class="feat" style="border-top: 3px solid #4ade80;">
<h4>Providers</h4>
<p>OAuth flows, API key management, account CRUD, key status</p>
</div>

<div class="feat" style="border-top: 3px solid #fbbf24;">
<h4>Usage & Quota</h4>
<p>Time-bucket analytics, per-model breakdown, per-account quota tracking</p>
</div>

<div class="feat" style="border-top: 3px solid #f472b6;">
<h4>Limiter & Config</h4>
<p>Adaptive limit override, thinking budget, global env, live config editing</p>
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

### Key Flow Monitor

```
  API Keys        AI Gateway          Models
+---------+      +=============+    +---------+
| key-1   | ---> |             | -> | glm-5.1 |
| key-2   | ---> |  gateway    | -> | glm-5   |
| key-3   | ---> |  :8080      | -> | glm-4.7 |
+---------+      +=============+    +---------+

  Hover key   -> highlight all model paths
  Hover model -> highlight all key paths
  Live pulse  -> real-time indicator
```

<br>

<p style="color: #64748b; font-size: 0.8em;">Embedded Vite build served at <code>/admin</code>. Optional cookie-based auth via <code>DASHBOARD_API_KEY</code>.</p>

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

### Setup

```json
// ~/.claude/settings.json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:8080",
    "ANTHROPIC_AUTH_TOKEN": "your-api-key"
  }
}
```

### Profile-Based (Docker)

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://arl-gateway:8080",
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

## Load Test Results

<div class="cols">
<div>

### Test Config

- **1 GLM API key**, 5 RPM limit
- **19 model slots**, global cap 9
- **50 worker coroutines**
- Endpoint: <code>/v1/chat/completions</code>

<br>

### Results

| Agents | Reqs | OK | Time | Key |
|---|---|---|---|---|
| 3 | 3 | 100% | 19s | Safe |
| 5 | 5 | 100% | 33s | Safe |
| 5x2 | 10 | 100% | 15s | Burst |
| 10 | 10 | 100% | 32s | Burst |

<br>

<blockquote>
<strong>Zero 429 errors</strong> across all tests. Key auto-recovers after 60s cooldown.
</blockquote>

</div>
<div>

### Model Distribution (10 agents)

```
glm-5.1      x3  (proactive round-robin)
glm-5-turbo  x1  (same-series)
glm-4.7      x2  (spillover)
glm-4.6      x3  (spillover)
glm-4.5      x1  (overflow)

10/10 OK, 0 errors
```

### Bottleneck Hierarchy

```
1. Provider RPM   5 req/min  (1 key)
2. Global cap     9 concurrent
3. Model slots    19 total
4. Workers        50 coroutines
```

### How to Scale

| Add | Effect |
|---|---|
| GLM keys | +5 RPM each |
| OpenAI | +120 RPM fallback |
| Anthropic | +50 RPM fallback |

</div>
</div>

---

## Resources & Ports

<div class="cols">
<div>

### Service Limits

| Service | Mem | CPU |
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

| Port | Service |
|---|---|
| **8080** | API Gateway (external) |
| 6379 | Dragonfly (internal) |
| 9090 | Worker / Prometheus |
| 3000 | Grafana (external) |
| 4317/4318 | OTel Collector |

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

<h2>Performance Highlights</h2>

<br>

<div style="display: flex; gap: 1.2em; justify-content: center; margin-top: 0.8em;">
<div class="stat-card" style="flex: 1; max-width: 180px;">
  <div class="num">100%</div>
  <div class="lbl">Success Rate</div>
</div>
<div class="stat-card" style="flex: 1; max-width: 180px;">
  <div class="num">0</div>
  <div class="lbl">429 Errors</div>
</div>
<div class="stat-card" style="flex: 1; max-width: 180px;">
  <div class="num">17</div>
  <div class="lbl">Providers</div>
</div>
<div class="stat-card" style="flex: 1; max-width: 180px;">
  <div class="num">21</div>
  <div class="lbl">Metrics</div>
</div>
</div>

<br>

<div style="display: flex; gap: 1.2em; justify-content: center;">
<div class="stat-card" style="flex: 1; max-width: 200px;">
  <div class="num" style="font-size: 1.6em;">Lock-free</div>
  <div class="lbl">Hot Path (atomic CAS)</div>
</div>
<div class="stat-card" style="flex: 1; max-width: 200px;">
  <div class="num" style="font-size: 1.6em;">Signal-based</div>
  <div class="lbl">Waiting (sync.Cond)</div>
</div>
<div class="stat-card" style="flex: 1; max-width: 200px;">
  <div class="num" style="font-size: 1.6em;">Zero</div>
  <div class="lbl">Body Modification</div>
</div>
</div>

---

<!-- _class: cover -->
<!-- _paginate: false -->

<br><br>

<h1 style="font-size: 2.8em;">Thank You</h1>

<hr class="divider">

<p class="subtitle" style="font-size: 1.3em;">AI Gateway</p>

<br>

<span class="chip chip-purple" style="font-size: 0.9em;">github.com/klxhunter/agent-rate-limit</span>

<br><br>

<p style="color: #475569; font-size: 1em;">Questions?</p>
