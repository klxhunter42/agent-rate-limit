# Proxy Layer Technical Specification

Generated: 2026-05-03
Source: `api-gateway/proxy/`

---

## Table of Contents

1. [Architecture Overview](#1-architecture-overview)
2. [Shared Infrastructure](#2-shared-infrastructure)
3. [AnthropicProxy (Native/Transparent/Sidecar)](#3-anthropicproxy)
4. [OpenAIProxy](#4-openaiproxy)
5. [GeminiCodeAssistProxy](#5-geminicodeassistproxy)
6. [GeminiAPIProxy](#6-geminiapiproxy)
7. [ClaudeSessionProxy](#7-claudsessionproxy)
8. [ClaudeSessionManager](#8-claudsessionmanager)
9. [Format Conversion](#9-format-conversion)
10. [Error Recovery & Retry](#10-error-recovery--retry)
11. [Key Pool](#11-key-pool)
12. [Privacy Masking Integration](#12-privacy-masking-integration)
13. [State Machine Reference](#13-state-machine-reference)

---

## 1. Architecture Overview

The proxy layer sits between the HTTP handler and upstream LLM APIs. All proxies share a common HTTP transport, metrics collector, and privacy masking pipeline. Each proxy implements format conversion to/from the Anthropic Messages API format, which is the gateway's canonical wire format.

```
Client (Anthropic format)
  |
  v
[Handler Layer] -- picks proxy based on provider routing
  |
  +-- AnthropicProxy       (direct Anthropic, transparent passthrough, sidecar)
  +-- GeminiCodeAssistProxy (Google Code Assist OAuth)
  +-- GeminiAPIProxy       (Google Gemini API key)
  +-- ClaudeSessionProxy   (claude.ai web session cookies)
  |
  v
Upstream Provider
```

### Files

| File | Lines | Purpose |
|---|---|---|
| `anthropic.go` | ~2120 | Native Anthropic, transparent passthrough, sidecar, vision proxy, format converters |
| `gemini-codeassist.go` | ~725 | Google Code Assist via OAuth, Anthropic<->Gemini conversion |
| `gemini-apikey.go` | ~322 | Gemini API key auth, reuses conversion from gemini-codeassist.go |
| `claude_session.go` | ~386 | claude.ai web API session proxy |
| `claude-session.go` | ~139 | Claude CLI session bootstrap (profile, roles, settings, policy) |
| `recovery.go` | ~299 | Error classification, context truncation, token estimation |
| `key_pool.go` | ~363 | Multi-key RPM management with cooldown |
| `shared_transport.go` | ~108 | DNS-cached HTTP transport singleton |
| `sse_writer.go` | ~74 | SSE buffer pool and writers |

---

## 2. Shared Infrastructure

### 2.1 HTTP Transport (`shared_transport.go`)

```go
func SharedTransport() *http.Transport
func SharedClient(timeout time.Duration) *http.Client
```

**Singleton transport** with:
- DNS cache: 30s TTL, resolved before TCP dial
- Connection pooling: `MaxIdleConns=200`, `MaxIdleConnsPerHost=100`
- Idle connection timeout: 120s
- HTTP/2 enabled (`ForceAttemptHTTP2=true`)
- Dial timeout: 10s
- Keep-alive: 30s
- TLS handshake timeout: 10s
- Response header timeout: **4 hours** (for long streaming responses)
- No per-host connection limit (`MaxConnsPerHost=0`)

**Clients:**
- `SharedClient(0)` -- no global timeout (streaming); used by all proxies
- `imageClient = SharedClient(15s)` -- for image URL downloads, 15s timeout

### 2.2 SSE Buffer Pool (`sse_writer.go`)

```go
func getBuf() *bytes.Buffer       // acquire from pool
func putBuf(b *bytes.Buffer)      // return to pool (drops if >512KB)
func writeSSEEvent(w io.Writer, event string, data []byte)
func writeSSEData(w io.Writer, data []byte)
func writeSSEJSON(w io.Writer, flusher http.Flusher, event string, v any)
```

Buffer pool caps at 512KB. Used by `gemini-codeassist.go` and `gemini-apikey.go`.

### 2.3 Constants

```go
const maxSSELineSize = 256 * 1024  // 256KB max per SSE line (scanner buffer)
const maxResponseSize = 100 * 1024 * 1024  // 100MB max response body
const streamTimeout = 10 * time.Minute  // max stream duration (Anthropic proxy only)
```

### 2.4 FeedbackFunc

```go
type FeedbackFunc func(statusCode int, rtt time.Duration, headers http.Header)
```

Called after each upstream attempt. Used by the adaptive rate limiter. Called on every attempt **except** 429 retries (only called on final 429 or non-429).

### 2.5 Allowed Response Headers

25 headers whitelisted for passthrough to client:

```
Content-Type, Content-Encoding,
X-RateLimit-Limit, X-RateLimit-Remaining, X-RateLimit-Reset, Retry-After, Request-Id,
Anthropic-Ratelimit-Requests-Remaining, Anthropic-Ratelimit-Tokens-Remaining,
Anthropic-Ratelimit-Unified-*, X-Ratelimit-Limit-*, X-Ratelimit-Remaining-*, X-Ratelimit-Reset-*
```

Blocked: `Set-Cookie`, `Server`, `X-Powered-By`, `Transfer-Encoding`.

---

## 3. AnthropicProxy

### 3.1 Struct

```go
type AnthropicProxy struct {
    cfg     *config.Config
    client  *http.Client          // SharedClient(0)
    metrics *metrics.Metrics
}
```

### 3.2 Proxy Modes

#### 3.2.1 `ProxyTransparent` -- Main Anthropic Passthrough

```go
func (p *AnthropicProxy) ProxyTransparent(
    w http.ResponseWriter,
    r *http.Request,
    apiKey string,
    body []byte,
    model string,
    isStream bool,
    feedback FeedbackFunc,
    maskResult *privacy.MaskResult,
    opts *ProxyOptions,
) error
```

**Input contract:** Raw Anthropic Messages API JSON body, `model` from URL path or payload.

**ProxyOptions:**
```go
type ProxyOptions struct {
    AuthMode          string            // "api_key" (default) or "bearer"
    UpstreamOverride  string            // override cfg.UpstreamURL
    ExtraHeaders      map[string]string // additional headers
    Transparent       bool              // skip all modifications (claude-oauth passthrough)
    BillingInjected   bool              // return ErrBillingRejected on 400 reserved keyword
    OnAuthError       func(oldKey string) (newKey string, ok bool)   // 401 refresh
    OnRateLimitError  func(oldKey string) (newKey string, ok bool)   // 429 rotation
}
```

**Upstream URL logic:**
- Default: `cfg.UpstreamURL + "/v1/messages"`
- `opts.UpstreamOverride != ""`: uses override
- `opts.Transparent`: `cfg.AnthropicDirectURL + "/v1/messages" + raw query`

**Pre-request processing:**
1. Estimate input tokens via `tokenizer.QuickEstimateTokens(body)`
2. If no masking active, optimize system prompt (whitespace + dedup)
3. **Transparent mode**: forward ALL client headers (except hop-by-hop), set `Accept-Encoding: identity`
4. **Bearer mode**: set `Authorization: Bearer`, `x-client-request-id`, `X-Claude-Code-Session-Id`, `x-anthropic-billing-header`, `x-mcp-client-session-id`
5. **API key mode**: set `x-api-key`
6. Strip unsupported beta flags for haiku/3.5-sonnet models (removes `effort-*`, `interleaved-thinking-*`)
7. Set `Content-Length`, `anthropic-version`

**Retry logic:**

```
maxAttempts = cfg.UpstreamMaxRetries + 1 + maxTransient
maxTransient defaults to 2
```

| Trigger | Action | Condition |
|---|---|---|
| HTTP 429 | Retry with backoff | `attempt < UpstreamMaxRetries` |
| HTTP 429 + OnRateLimitError | Retry with key rotation | `OnRateLimitError != nil` |
| HTTP 401 + bearer + OnAuthError | Retry with refreshed token | Single attempt |
| HTTP 200 + empty body | Retry as transient | `transientAttempts < maxTransient` |
| HTTP 200 + malformed body | Retry as transient | `transientAttempts < maxTransient` |
| ClassifyError = ActionTruncateAndRetry | Truncate + retry | `truncationAttempts < 1 && EnableAutoTruncate` |
| ClassifyError = ActionRetryTransient | Retry with backoff | `transientAttempts < maxTransient` |
| ErrBillingRejected | Return early (no retry) | `BillingInjected && 400 && "reserved keyword"` |

**Backoff formula:** `base * attempt^2`, capped at 5 minutes. Checks `r.Context().Done()` between retries.

**HTTP 200 validation (anti-empty/malformed):**
- Non-stream: read 1KB peek, check starts with `{` or `[`
- Stream: read 1KB peek, check contains `data: ` or `event: ` line
- Invalid responses counted as transient retries
- Valid peeked data reassembled via `io.MultiReader(peeked, remainingBody)`

**Non-stream response handling (`handleNonStreamResponse`):**
1. Read full body with `maxResponseSize` (100MB) limit
2. Parse token usage (primary: `usage.input_tokens`/`output_tokens`, fallback: `prompt_tokens`/`completion_tokens`)
3. Unmask secrets/PII if maskResult active
4. Trim verbose patterns if `cfg.EnableResponseTrim`
5. Filter out `server_tool_use` content blocks
6. Copy allowed response headers
7. Write status + body

**Stream response handling (`relayStreamWithTracking`):**
1. Wrap body with context timeout (10 min)
2. Scan lines, forward non-data lines as-is
3. Unmask `content_block_delta` events (text, thinking, partial_json)
4. Unmask lines containing `[[` (direct JSON replacement)
5. Flush unmasker buffer at `content_block_stop` boundaries
6. Filter `server_tool_use` blocks (skip start/delta/stop for that index)
7. Parse `message_start` for input tokens, `message_delta` for output tokens
8. Record TTFB on first line
9. After stream ends, flush remaining unmasker buffer
10. Fallback to estimated input tokens if upstream reported 0

#### 3.2.2 `ProxySidecar` -- Node.js Sidecar Relay

```go
func (p *AnthropicProxy) ProxySidecar(
    w http.ResponseWriter, r *http.Request,
    sidecarURL string, body []byte, model string,
    isStream bool, feedback FeedbackFunc,
    maskResult *privacy.MaskResult, opts *ProxyOptions,
) error
```

- Forwards to `sidecarURL + r.URL.Path + "?beta=true"`
- Forwards ALL client headers (same as transparent mode, minus hop-by-hop)
- On 400 with "reserved keyword": returns error (no retry)
- Copies allowed response headers
- Stream: line-by-line relay with unmasking (text, thinking, partial_json, `[[` patterns)
- Non-stream: full buffer, unmask, write

#### 3.2.3 `ProxyNativeVision` -- Z.AI Vision API

```go
func (p *AnthropicProxy) ProxyNativeVision(
    w http.ResponseWriter, r *http.Request,
    apiKey string, body []byte, model string,
    isStream bool, feedback FeedbackFunc,
    maskResult *privacy.MaskResult,
) error
```

- Converts Anthropic payload to Zhipu/OpenAI format via `AnthropicToOpenAI(body, model, metrics, "")`
- Sends to `cfg.NativeVisionURL`
- Retries on 429 (up to `UpstreamMaxRetries`)
- Converts response back via `convertOpenAIResponse`

### 3.3 Streaming State Machine (relayStreamWithTracking)

```
State variables:
  ttfbRecorded: bool     -- first line seen
  inputTokens: int       -- from message_start.usage
  outputTokens: int      -- from message_delta.usage
  filteredBlocks: map[int]bool -- server_tool_use block indices to skip

Per-line processing:
  1. If not ttfbRecorded -> RecordTTFB, set true
  2. If not "data: " prefix -> forward as-is, flush
  3. If unmasker active:
     a. Contains "content_block_delta" -> parse, unmask text/thinking/partial_json
     b. Contains "[[" -> ReplaceDirectJSON
  4. Contains "content_block_stop" -> Flush unmasker buffer
  5. Contains "content_block_start" -> check if server_tool_use, skip if so
  6. If block index in filteredBlocks -> skip
  7. Forward line, flush
  8. Parse message_start -> extract input_tokens
  9. Parse message_delta -> extract output_tokens

Post-stream:
  - Flush remaining unmasker buffer as content_block_delta
  - Use fallback input tokens if upstream reported 0
  - Record token metrics
```

### 3.4 Billing Header Injection

```go
func InjectBillingHeader(body []byte) []byte
```

Injects Claude Code billing header into system prompt array:
- `system[0]`: `"x-anthropic-billing-header: cc_version=2.1.123.{hash}; cc_entrypoint=cli; cch=00000;"`
- `system[1]`: `"You are Claude Code, Anthropic's official CLI for Claude."`
- Original system entries shifted after these

**Build hash algorithm:**
1. Extract first non-meta user message text
2. Take chars at positions 4, 7, 20 (default '0' if out of bounds)
3. `sha256(salt + chars + version)[:3]`
4. Salt: `"59cf53e54c78"`, version: `"2.1.123"`

### 3.5 Beta Header Stripping

```go
func stripUnsupportedBetas(h *http.Header, model string)
```

Removes `effort-*` and `interleaved-thinking-*` beta flags for haiku and 3.5-sonnet models.

### 3.6 Response Trimming

```go
func trimResponse(body []byte) []byte
func trimVerbose(text string) string
```

Strips verbose prefixes/suffixes from text content blocks in non-stream responses. Returns nil if no changes (invalid JSON, no text blocks, no patterns matched). Validates trimmed text is printable UTF-8.

**Patterns stripped:**
- Prefixes: "Here's the...", "Here is the...", "Let me explain/help/show/walk/break down/tell you about...", "I'll help/explain/show/walk...", "Sure!", "Certainly!", "Of course!", "Great question!", "I'd be happy to help/explain/show/assist..."
- Suffixes: "Hope this helps!", "Hope that is helpful.", "Let me know if you need anything else/more help/further assistance."

---

## 4. OpenAIProxy

### 4.1 Struct

```go
type OpenAIProxy struct {
    cfg     *config.Config
    client  *http.Client          // SharedClient(0)
    metrics *metrics.Metrics
}
```

### 4.2 Main Entry: `ProxyOpenAI`

```go
func (p *OpenAIProxy) ProxyOpenAI(
    w http.ResponseWriter, r *http.Request,
    upstreamURL, apiKey string, body []byte, model string,
    isStream bool, feedback FeedbackFunc, maskResult *privacy.MaskResult,
    maxContinuations int, toolMode string,
) error
```

**Parameters:**
- `toolMode`: "" = no tools, "native" = OpenAI function calling

**Processing pipeline:**
1. Convert body via `AnthropicToOpenAI(body, model, metrics, toolMode)`
2. Estimate input tokens from OpenAI body (not Anthropic -- conversion adds tools/system)
3. If `maxContinuations > 0`:
   - Auto-compact if `estInput > 32000` (80% of 40k context)
   - Adjust `max_tokens` to fit: `min(requested, 40000 - estInput - 1500)`, minimum 256
4. Retry loop (same pattern as Anthropic: 429 + transient + backoff)
5. Special handling: if 400 mentions "max_tokens" + "context length", retry with reduced max_tokens
6. Stream or non-stream response

**Auto-compaction (`compactOpenAIMessages`):**
- Triggered when `estInput > 32000` and `maxContinuations > 0`
- Requires >= 6 conversation messages
- Keeps: system message + compaction notice + last 4 messages (2 turns)
- Walks back to include assistant that owns tool results at boundary (max 8 messages)
- Inserts compaction notice as user message: `"[System: Previous conversation was auto-compacted...]"`

**Auto-continuation (stream only):**
```
for contIdx := 0; contIdx <= maxContinuations; contIdx++:
  relay chunk -> if !truncated: break
  if input_tokens > 35000: break  (context nearly full)
  Build continuation request:
    messages += [assistant: accumulatedText, user: "Continue exactly from where you left off..."]
    max_tokens = min(origMaxTokens, 40000 - totalInput - totalOutput - 2000)
    minimum 256
  Send continuation request
```

### 4.3 Stream Chunk Relay: `relayOpenAIStreamChunk`

```go
func (p *OpenAIProxy) relayOpenAIStreamChunk(
    w http.ResponseWriter, resp *http.Response, model string,
    unmasker *masking.StreamUnmasker, msgID string,
    isContinuation bool, isFinal bool,
    streamStart time.Time, toolMode string,
) (truncated bool, accumulatedText string, inputTokens int, outputTokens int, err error)
```

**State machine:**
```
Variables:
  started: bool           -- message_start emitted
  doneReceived: bool      -- [DONE] received
  stopReason: string      -- "end_turn" | "max_tokens" | "tool_use"
  ttfbRecorded: bool
  contentBlockIdx: int    -- current Anthropic content block index
  textBlockOpen: bool     -- text content_block_start emitted, not yet stopped
  toolBlockOpen: bool     -- tool_use content_block_start emitted, not yet stopped
  accumulatedText: string

Per-chunk processing:
  1. Parse "data: " line
  2. "[DONE]" -> close events (if started and not max_tokens-or-not-final)
  3. Extract usage (prompt_tokens, completion_tokens)
  4. Extract choice[0].delta
  5. finish_reason mapping:
     - "length" -> stopReason = "max_tokens"
     - "stop" -> continue
     - "tool_calls" -> stopReason = "tool_use"
  6. If toolMode == "native" && delta.tool_calls:
     a. New tool call (has id): close previous block, start tool_use block
     b. Arguments delta: emit input_json_delta
     c. Continue (skip text processing)
  7. If text == "": continue
  8. Record TTFB if first text
  9. If !started: emit message_start + content_block_start(index=0), set textBlockOpen=true
  10. If continuation && !textBlockOpen && !toolBlockOpen:
      emit content_block_start(index=++contentBlockIdx), set textBlockOpen=true
  11. accumulatedText += text, outputTokens++
  12. Unmask if active, skip if empty after unmasking
  13. Emit content_block_delta (text_delta)

On stream end without [DONE]:
  - Flush unmasker buffer
  - Emit close events (content_block_stop, message_delta, message_stop)

Return: stopReason == "max_tokens" (truncated flag)
```

**SSE event sequence (non-continuation):**
```
event: message_start
data: {"type":"message_start","message":{"id":"msg_openai_...","type":"message","role":"assistant","content":[],"model":"...","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":N,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"..."}}

... (more deltas)

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":N}}

event: message_stop
data: {"type":"message_stop"}
```

**SSE event sequence (tool use, toolMode="native"):**
```
message_start
content_block_start(index=0, type=text)     -- if text present
content_block_delta(text_delta)               -- text chunks
content_block_stop(index=0)
content_block_start(index=1, type=tool_use, id=..., name=...)   -- new tool block
content_block_delta(input_json_delta, partial_json=...)          -- arg chunks
content_block_stop(index=1)
message_delta(stop_reason="tool_use")
message_stop
```

### 4.4 Non-Stream Response: `handleOpenAIResponse`

1. Read body (100MB limit)
2. Parse usage, record tokens
3. Convert via `OpenAIToAnthropic(openaiResp, model, toolMode)`
4. Unmask if active
5. Trim verbose if enabled
6. Write 200 + body

---

## 5. GeminiCodeAssistProxy

### 5.1 Struct

```go
type GeminiCodeAssistProxy struct {
    client         *http.Client     // SharedClient(0)
    metrics        *metrics.Metrics
    endpoint       string           // cfg.GeminiCodeAssistEndpoint
    defaultModel   string           // cfg.GeminiDefaultModel
}
```

### 5.2 Project Resolution

```go
func (p *GeminiCodeAssistProxy) ResolveProjectID(ctx context.Context, accessToken string) (string, error)
```

- POST to `{endpoint}:loadCodeAssist` with `{}`, Bearer auth
- Parses `cloudaicompanionProject` from response

### 5.3 Main Entry: `ProxyCodeAssist`

```go
func (p *GeminiCodeAssistProxy) ProxyCodeAssist(
    w http.ResponseWriter, r *http.Request,
    accessToken string, body []byte, model string,
    isStream bool, feedback FeedbackFunc,
    maskResult *privacy.MaskResult,
    onAuthError func(string) (string, bool),
    projectID string,
) error
```

**Processing:**
1. Parse Anthropic payload, convert via `anthropicToGemini(payload, defaultModel, metrics)`
2. Wrap in Code Assist envelope: `{model, project, request: geminiRequest}`
3. Endpoint: `{endpoint}:generateContent` (non-stream) or `{endpoint}:streamGenerateContent?alt=sse` (stream)
4. Bearer auth with access token
5. On 401: call `onAuthError` to refresh, retry once
6. On non-200: unmask error body, write Anthropic-format error response
7. Stream or non-stream response conversion

**Code Assist envelope:**
```go
type codeAssistRequest struct {
    Model              string          `json:"model"`
    Project            string          `json:"project,omitempty"`
    Request            geminiRequest   `json:"request"`
    EnabledCreditTypes []string        `json:"enabled_credit_types,omitempty"`
}

type codeAssistResponse struct {
    Response           *geminiResponse `json:"response,omitempty"`
    TraceID            string          `json:"traceId,omitempty"`
    ConsumedCredits    []any           `json:"consumedCredits,omitempty"`
    RemainingCredits   []any           `json:"remainingCredits,omitempty"`
    Error              *geminiError    `json:"error,omitempty"`
}
```

### 5.4 Stream Response

**SSE event sequence (written by proxy):**
```
event: message_start
data: {"type":"message_start","message":{"id":"msg_...","type":"message","role":"assistant","content":[],"model":"...","stop_reason":null,"usage":{"input_tokens":0,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta     (for each text part)
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"..."}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn|max_tokens","stop_sequence":null},"usage":{"output_tokens":N}}

event: message_stop
data: {"type":"message_stop"}
```

**State:**
- `stopReason`: defaults "end_turn", set to "max_tokens" if any candidate has `FinishReason == "MAX_TOKENS"`
- No multi-block support: all text goes to index 0
- Flushes unmasker buffer before content_block_stop
- Uses `writeSSE()` helper from same file (not sse_writer.go)

---

## 6. GeminiAPIProxy

### 6.1 Struct

```go
type GeminiAPIProxy struct {
    cfg     *config.Config
    client  *http.Client     // SharedClient(0)
    metrics *metrics.Metrics
}
```

### 6.2 Main Entry: `ProxyGemini`

```go
func (p *GeminiAPIProxy) ProxyGemini(
    w http.ResponseWriter, r *http.Request,
    upstreamURL, apiKey string, body []byte, model string,
    isStream bool, feedback FeedbackFunc, maskResult *privacy.MaskResult,
) error
```

**Auth logic:**
- Key starts with `"ya29."`: `Authorization: Bearer {key}` (OAuth token)
- Otherwise: `x-goog-api-key: {key}` (API key)

**Retry logic:**
- Same pattern: 429 retries + transient retries + quadratic backoff
- No auto-continuation, no auto-compaction
- On non-retriable error: unmask body, forward status code and body as-is (does NOT convert to Anthropic error format)

**Endpoint:**
```
{cfg.GeminiAPIEndpoint}/v1beta/models/{mappedModel}:generateContent
{cfg.GeminiAPIEndpoint}/v1beta/models/{mappedModel}:streamGenerateContent?alt=sse
```

### 6.3 Stream Response: `relayGeminiStream`

- Lazy `message_start` + `content_block_start`: emitted on first valid data chunk (TTFB)
- Parses `usageMetadata.promptTokenCount` / `candidatesTokenCount` from chunks
- Extracts `candidates[0].content.parts[].text`
- Unmasks via StreamUnmasker, skips empty-after-unmask chunks
- outputTokens incremented per chunk (not from usage -- rough estimate)
- Closes with content_block_stop, message_delta, message_stop

### 6.4 Non-Stream Response: `handleGeminiResponse`

1. Read body, parse as `geminiResponse`
2. If parse fails: forward raw body (passthrough for non-Gemini responses)
3. Record tokens from usageMetadata
4. Convert via `geminiToAnthropic(gResp, model, false)`
5. Unmask, write 200

---

## 7. ClaudeSessionProxy

### 7.1 Struct

```go
type ClaudeSessionProxy struct {
    client  *http.Client     // SharedClient(0)
    metrics *metrics.Metrics
}
```

### 7.2 Main Entry: `ProxySession`

```go
func (p *ClaudeSessionProxy) ProxySession(
    w http.ResponseWriter, r *http.Request,
    cookie string, body []byte, model string,
    isStream bool, maskResult *privacy.MaskResult,
    feedback FeedbackFunc,
) error
```

**Flow:**
1. Get org ID: `GET https://claude.ai/api/organizations` with cookie
2. Create conversation: `POST https://claude.ai/api/organizations/{orgID}/chat_conversations`
3. Build completion: extract prompt + model from Anthropic payload
4. Send completion: `POST https://claude.ai/api/organizations/{orgID}/chat_conversations/{chatID}/completion`
5. Delete conversation in deferred cleanup (5s timeout)
6. Convert claude.ai SSE to Anthropic SSE

**Prompt extraction (`extractPrompt`):**
- Converts multi-turn Anthropic messages to single prompt string
- Prepends system prompt as `"system: ..."`
- For multi-turn: includes last 3 messages for context (finds last user message, includes 3 before it)
- Returns `"{role}: {text}\n\n"` format per message

**claude.ai request format:**
```json
{
  "prompt": "...",
  "timezone": "UTC",
  "attachments": [],
  "files": [],
  "model": "..."
}
```

**Browser headers:** Full Chrome User-Agent, Accept-Language, DNT, Sec-Fetch-*, TE: trailers, Referer, Origin.

### 7.3 Stream Conversion: `convertSessionSSE`

- Scanner buffer: 256KB
- Parses claude.ai SSE: `{"completion": "...", "stop_reason": "end_turn"}`
- Maps to Anthropic SSE: message_start, content_block_start, content_block_delta, content_block_stop, message_delta, message_stop
- outputTokens estimated as `len(completion) / 4`
- Unmasks via StreamUnmasker
- On error chunk: break, emit close events
- On stop_reason: break, emit close events

---

## 8. ClaudeSessionManager

### 8.1 Purpose

Bootstraps Claude CLI OAuth sessions by fetching profile, roles, settings, and policy limits from Anthropic endpoints.

### 8.2 Bootstrap URLs

```
https://api.anthropic.com/api/oauth/profile
https://api.anthropic.com/api/oauth/claude_cli/roles
https://api.anthropic.com/api/claude_code/settings
https://api.anthropic.com/api/claude_code/policy_limits
```

### 8.3 Session Lifecycle

```go
type ClaudeSession struct {
    Token         string
    Profile       json.RawMessage
    Roles         json.RawMessage
    Settings      json.RawMessage
    PolicyLimits  json.RawMessage
    Bootstrapped  bool
    BootstrapAt   time.Time
}
```

- `BootstrapSession(token)`: Fetches all 4 endpoints, stores in `sync.Map`. Profile failure is warned but not fatal. Roles failure is fatal.
- `GetOrCreateSession(token)`: Returns cached or bootstraps new.
- `InvalidateSession(token)`: Deletes from map.
- `BootstrapIfNeeded(token)`: Only bootstraps `sk-ant-oat` prefixed tokens.
- Client timeout: 30s. Response body limit: 1MB.

---

## 9. Format Conversion

### 9.1 Anthropic -> OpenAI (`AnthropicToOpenAI`)

```go
func AnthropicToOpenAI(body []byte, model string, m *metrics.Metrics, toolMode string) (map[string]any, error)
```

**Conversion rules:**
- System prompt:
  - `toolMode == "native"`: sent as `"role": "system"` message with tool hint appended
  - Otherwise: prepended to first user message text/content array
- System prompt optimization: whitespace cleanup + sentence deduplication
- Messages:
  - `toolMode == ""`: only user + assistant, filter unsupported types (keep text, image, image_url)
  - `toolMode == "native"`: full conversion via `convertMessageWithTools`
- Images:
  - `source.type == "base64"`: convert to `data:{media_type};base64,{data}` as `image_url`
  - `source.type == "url"`: download via `FetchImageAsBase64` (20MB limit, 15s timeout), convert to base64 data URI. On failure: replace with text `"[image could not be loaded]"`
- Tools:
  - `toolMode == "native"`: convert Anthropic `tools[]` to OpenAI `tools[].type=function` format
  - `tool_choice`: `"auto"/"none"` pass through, `"tool"` -> `{"type":"function","function":{"name":...}}`, `"any"` -> `"required"`
- Thai detection: if Thai characters found in user messages, prepend language hint
- `max_tokens`: capped at 4096 for vision models
- `stream`: pass through, add `stream_options.include_usage=true` for streams

**Message conversion with tools (`convertMessageWithTools`):**

| Anthropic | OpenAI |
|---|---|
| assistant text blocks | `{"role":"assistant","content":"joined text"}` |
| assistant tool_use blocks | `tool_calls: [{id, type:"function", function:{name, arguments:JSON_string}}]` |
| user text blocks | `{"role":"user","content":[{type:"text",...}]}` |
| user tool_result blocks | `{"role":"tool","tool_call_id":"...","content":"flattened text"}` |

### 9.2 OpenAI -> Anthropic (`OpenAIToAnthropic`)

```go
func OpenAIToAnthropic(zhipu map[string]any, model string, toolMode ...string) map[string]any
```

**Conversion rules:**
- `choices[0].message.content` -> `content[0] = {type: "text", text: ...}`
- `choices[0].message.tool_calls` -> `content[N] = {type: "tool_use", id, name, input}` (only if `toolMode[0] == "native"`)
- `choices[0].finish_reason`: `"stop"` -> `"end_turn"`, `"tool_calls"` -> `"tool_use"`, else pass through
- `usage.prompt_tokens` -> `input_tokens`, `usage.completion_tokens` -> `output_tokens`
- Preserves `zhipu["id"]` as response ID

### 9.3 Anthropic -> Gemini (`anthropicToGemini`)

```go
func anthropicToGemini(payload map[string]any, defaultModel string, m ...*metrics.Metrics) geminiConversionResult
```

**Conversion rules:**
- System: `systemInstruction.parts[0].text` (optimized: whitespace + dedup)
- Messages: `user` -> `user`, `assistant` -> `model`
- Content types:
  - `text` -> `parts[].text`
  - `image` (base64) -> `parts[].inlineData.{mimeType, data}`
  - `tool_use` -> `parts[].functionCall.{name, args}`
  - `tool_result` -> `parts[].functionResponse.{name, response:{result:...}}`
- Generation config: temperature, topP, topK, maxOutputTokens, stopSequences
- Tools: `tools[].functionDeclarations[].{name, description}`
- Model mapping (see below)

### 9.4 Gemini -> Anthropic (`geminiToAnthropic`)

```go
func geminiToAnthropic(gResp geminiResponse, model string, stream bool) map[string]any
```

**Conversion rules:**
- `finishReason`: `"MAX_TOKENS"` -> `"max_tokens"`, `"STOP"` -> `"end_turn"`, `"SAFETY"` -> `"stop_sequence"`
- Content parts: text -> `{type: "text"}`, functionCall -> `{type: "tool_use", id: "toolu_{name}", name, input}`
- Empty content -> `[{type: "text", text: ""}]`
- Usage: `promptTokenCount` -> `input_tokens`, `candidatesTokenCount` -> `output_tokens`
- Message ID: `msg_{unix_nano}`

### 9.5 Model Mapping: Anthropic -> Gemini

```go
func mapModelToGemini(model string) string
```

| Input | Output |
|---|---|
| gemini-2.5-pro | gemini-2.5-pro |
| gemini-2.5-flash | gemini-2.5-flash |
| gemini-2.5-flash-lite | gemini-2.5-flash-lite |
| gemini-2.0-flash | gemini-2.5-flash |
| gemini-2.0-flash-lite | gemini-2.5-flash-lite |
| gemini-1.5-pro | gemini-2.5-flash |
| gemini-1.5-flash | gemini-2.5-flash |
| models/{name} | {name} (strip prefix) |
| other | passthrough |

---

## 10. Error Recovery & Retry

### 10.1 Error Classification (`ClassifyError`)

```go
func ClassifyError(statusCode int, body []byte) RecoveryAction
```

```go
type RecoveryAction int
const (
    ActionForward           RecoveryAction = iota  // send to client as-is
    ActionRetryTransient                           // retry with backoff
    ActionTruncateAndRetry                         // truncate messages, retry
)
```

**Classification rules:**

| Condition | Action |
|---|---|
| Status 500, 502, 503, 529 | `ActionRetryTransient` |
| Status 413 | `ActionTruncateAndRetry` |
| Status 400/422 + `"code":"1234"` or `"internal network failure"` | `ActionRetryTransient` |
| Status 400/422 + context window patterns | `ActionTruncateAndRetry` |
| Other | `ActionForward` |

**Context window patterns:**
```
"prompt is too long", "prompt exceeds maximum length", "context_length_exceeded",
"maximum context length", "token count exceeds", "context window",
"context limit exceeded", "too many tokens", "reduced context window"
```

### 10.2 Context Truncation (`TruncateMessages`)

```go
func TruncateMessages(body []byte, model string) *TruncationResult
```

**Algorithm:**
1. Requires >= 4 messages
2. Target tokens: `(ContextWindow - MaxOutputTokens) * 0.75`
3. Walk messages backwards, keep as many as fit within budget
4. Minimum keep: 2 messages
5. Fix tool pair boundary: if first kept message is tool_result without preceding tool_use, include one more
6. Append truncation note to system prompt: `"\n\n[Note: older conversation messages were truncated to fit context window limits.]\n"`
7. Return new body with dropped message count and token counts

**Token estimation:**
- Text: `tokenizer.QuickEstimateTokens(text)`
- Image: 1000 tokens
- tool_use: estimate from `json.Marshal(input)`
- tool_result: estimate from content text

---

## 11. Key Pool

### 11.1 Struct

```go
type KeyPool struct {
    keys    []*keyEntry
    rpmLimit int64
    mu      sync.Mutex
    idx     int              // round-robin cursor
    notify  *sync.Cond       // signaled when key exits cooldown
}

type keyEntry struct {
    apiKey         string
    timestamps     []int64    // unix-millis of requests in sliding window
    cooldownUntil  int64      // unix-millis; 0 = not in cooldown
}
```

### 11.2 Strategy

```go
func SetStrategy(s string)  // "round-robin" (default) or "fill-first"
func GetStrategy() string
```

- **round-robin**: selects key with most remaining RPM budget (>0 preferred, falls back to highest)
- **fill-first**: selects key with most remaining RPM budget regardless of zero

### 11.3 Acquire Flow

```
1. If passthrough (no keys): return ("", true)
2. Lock mutex
3. Trim timestamps older than 60s for all keys
4. findBest(): skip keys in cooldown, pick key with most budget
5. If found: record timestamp, return key
6. If all in cooldown:
   a. Find soonest cooldown expiry
   b. If none in cooldown but no budget: findLeastLoaded()
   c. Wait via sync.Cond until cooldown expires
   d. Retry findBest()
```

### 11.4 Feedback

- `Report429(apiKey)`: puts key in cooldown for 10 seconds, broadcasts to waiters
- `ReportSuccess(apiKey)`: clears cooldown, broadcasts to waiters

### 11.5 Dynamic Updates

```go
func (kp *KeyPool) SyncFromStore(keys []string)
```

Replaces pool entries, preserving RPM state for existing keys.

---

## 12. Privacy Masking Integration

All proxies support optional privacy masking via `maskResult *privacy.MaskResult`.

### 12.1 Non-Stream Unmasking

```go
pipeline := privacy.NewPipeline(&privacy.Config{}, nil)
body = pipeline.UnmaskResponse(body, maskResult)
```

Applied to full response body before writing to client. Replaces placeholder tokens (e.g., `[[SECRET_1]]`, `[[EMAIL_ADDRESS_3]]`) with original values.

### 12.2 Stream Unmasking

```go
unmasker := masking.NewStreamUnmasker(maskResult.PIICtx, maskResult.SecretsCtx)
text = unmasker.ProcessChunk(text)              // for text/thinking deltas
text = unmasker.ReplaceDirectJSON(data)         // for raw SSE data lines with [[
remaining := unmasker.Flush()                    // at block boundaries and stream end
```

**Critical invariant:** `content_block_delta` events are ALWAYS processed when unmasker is active, even if the chunk doesn't contain `[[`. This is because a `[[PERSON_N]]` placeholder can be split across chunks -- the second chunk won't contain `[[` but the buffer still holds the first half.

**Flush points:**
- `content_block_stop` events (prevent cross-block contamination)
- Stream end / `[DONE]`
- Unmasker buffer not empty at stream end (emitted as extra content_block_delta)

### 12.3 Mask-Aware Processing

- System prompt optimization is SKIPPED when masking is active (would corrupt placeholders)
- Error bodies are unmasked before forwarding to client
- Sidecar proxy handles: text, thinking, partial_json, and `[[` patterns in stream

---

## 13. State Machine Reference

### 14.1 Anthropic Transparent Stream

```
                    +-----------+
                    |  Start    |
                    +-----------+
                         |
                    [first line]
                         |
                    v
              [RecordTTFB]
                    |
              [line type?]
               /    |    \
         event   data:   other
         line    [DONE]  line
          |        |       |
       forward  close    forward
          |    events     |
          |        |       |
          +--------+-------+
                   |
              [scanner.Err?]
              /          \
           no           yes
            |            |
         [continue]  [return err]
```

### 14.2 OpenAI Stream (with tool use)

```
                    +-----------+
                    |  Start    |
                    +-----------+
                         |
                    [data: line]
                         |
              +-- [DONE]? --+
              | yes        | no
         close events  [parse chunk]
              |              |
              |     [finish_reason?]
              |      /    |    \
              |   length  stop  tool_calls
              |     |      |       |
              |   max_tok  |   tool_use
              |              |
              |   [tool_calls in delta?]
              |    /              \
              |  yes (toolMode)   no
              |   |                |
              | [close prev block] |
              | [start tool block] |
              | [emit args delta]  |
              |                    |
              |            [text != ""?]
              |              /        \
              |           yes        no
              |            |          |
              |     [!started?]      |
              |      /       \       |
              |    yes       no      |
              |  [emit     [cont?    |
              |   start]   new blk?] |
              |            /    \    |
              |          yes    no   |
              |       [new blk]     |
              |            |        |
              |     [unmask + emit  |
              |      text_delta]    |
              |            |        |
              +-----+------+--------+
                    |
              [stream end without DONE?]
                    |
              [flush unmasker]
              [emit close events]
```

### 14.3 Gemini CodeAssist Stream

```
+-----------+     +----------------+     +------------------+     +------------------+
| message_  | --> | content_block_ | --> | content_block_   | --> | content_block_   |
| start     |     | start (idx=0)  |     | delta (text)     |     | stop             |
+-----------+     +----------------+     +------------------+     +------------------+
                                                                           |
                    +------------------+     +------------------+          |
                    | message_delta    | <-- | (flush unmasker) | <--------+
                    +------------------+     +------------------+
                           |
                    +------------------+
                    | message_stop     |
                    +------------------+
```

### 14.4 OpenAI Auto-Continuation State Machine

```
+-------------------+
| relay chunk       |
| (contIdx=0)       |
+-------------------+
         |
    [truncated?]
      /        \
    yes         no
     |           |
  [context    [done]
   nearly
   full?]
    /      \
  yes      no
   |        |
 [done]  [build cont req]
           |
      [send cont req]
           |
      [200 OK?]
        /     \
      yes      no
       |        |
  [relay chunk  [done]
   contIdx++]
```

---

## Appendix A: Error Response Formats

### Anthropic Error (gateway-generated)

```json
{
  "type": "error",
  "error": {
    "type": "rate_limit_error|overloaded_error|authentication_error",
    "message": "..."
  }
}
```

### Gemini CodeAssist Error (upstream forwarded)

```json
{
  "type": "error",
  "error": {
    "type": "authentication_error",
    "message": "upstream error ({status}): {body}"
  }
}
```

## Appendix B: Retry Budget Summary

| Proxy | 429 Retries | Transient Retries | Truncation | Backoff | Max Backoff | Special |
|---|---|---|---|---|---|---|
| Anthropic (transparent) | `cfg.UpstreamMaxRetries` | `cfg.TransientRetryMax` (default 2) | 1 attempt | quadratic | 5 min | Key rotation on 429, token refresh on 401 |
| OpenAI | `cfg.UpstreamMaxRetries` | `cfg.TransientRetryMax` (default 2) | 0 | quadratic | 5 min | max_tokens reduction on 400 |
| GeminiCodeAssist | 0 | 0 | 0 | none | n/a | 401 refresh once |
| GeminiAPI | `cfg.UpstreamMaxRetries` | `cfg.TransientRetryMax` (default 2) | 0 | quadratic | 5 min | none |
| Anthropic (vision) | `cfg.UpstreamMaxRetries` | 0 | 0 | quadratic | 5 min | none |
| ClaudeSession | 0 | 0 | 0 | none | n/a | none |

## Appendix C: Metrics Recorded

All proxies call:
- `metrics.RecordTokens(ctx, model, inputTokens, outputTokens)` -- on response completion
- `metrics.RecordTTFB(model, duration)` -- on first stream chunk (Anthropic, OpenAI, GeminiAPI)
- `metrics.Inc429()` -- on 429 from upstream
- `metrics.IncRetry()` -- on retry attempt
- `metrics.IncTransientRetry(statusCode, model)` -- on transient retry
- `metrics.IncContextTruncation(model)` -- on auto-truncation
- `metrics.RecordOptimization(type, saved)` -- on system prompt optimization
