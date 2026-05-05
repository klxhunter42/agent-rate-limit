# Request Flow by Provider

> Updated: 2026-05-06
> All providers share the same middleware chain and optimizer pipeline.
> Differences are in format conversion, auth, and upstream dispatch.

---

## Shared Middleware (every request)

```
Client POST /v1/messages or /v1/chat/completions
        |
+---[ Middleware ]------------------------------------------------+
| 1. SecurityHeaders       (CSP, X-Frame-Options, etc.)          |
| 2. CorrelationID         (X-Correlation-ID propagate/generate)  |
| 3. RealIP                (CF-Connecting-IP > X-Real-IP > XFF)   |
| 4. IPFilter              (whitelist/blacklist by CIDR)          |
| 5. Logging               (structured: method, path, duration)   |
| 6. RateLimiter           (external RL service, fail-open)       |
| 7. AdaptiveLimiter       (per-model concurrency semaphore)      |
+-----------------------------------------------------------------+
        |
        v
+---[ Handler Core ]---------------------------------------------+
| 1. Read body (max 10 MB)                                       |
| 2. Parse JSON, extract "model"                                 |
| 3. Resolve provider via modelRules prefix match                |
| 4. Profile routing (arl_ token / X-Profile header)             |
| 5. API key resolution (7-level priority chain)                 |
| 6. Quota check (daily budget vs QuotaBlockPct)                 |
| 7. AdaptiveLimiter.Acquire(model)                              |
| 8. System prompt injection (if enabled)                        |
| 9. Smart max_tokens adjustment                                 |
| 10. Token optimizer pipeline (13 stages)                       |
| 11. Privacy masking (secrets + PII -> placeholders)            |
+-----------------------------------------------------------------+
        |
        +-- provider dispatch --> (see each provider below)
```

---

## 1. Z.AI / GLM (Primary Flow)

```
Request enters handler
        |
        v
  Resolve "glm-*" -> provider: zai, format: FormatAnthropic
        |
        v
  stripUnsupportedFields():
    - Only removes context_management, service_tier (non-Anthropic fields)
    - NO GLM-specific stripping (tools, thinking, etc. pass through)
        |
        v
  filterUnsupportedContent(): SKIPPED for glm-* models
    - Content blocks pass through as-is (tool_use, tool_result, etc.)
        |
        v
  Vision check:
 - Images detected? -> select vision model (glm-5.1)
 - Large images (>500KB base64): compress with bimg/libvips
   - Resize to max 1024px longest dimension (Lanczos3)
   - Re-encode as WebP (better compression than JPEG/PNG)
   - Applies to all large images regardless of source format
        |
        v
  Privacy masking (secrets + PII)
        |
        v
  +-- No images: AnthropicProxy.ProxyTransparent()
  |   -> POST https://api.z.ai/api/anthropic
  |   -> Retry on 429 (exp backoff), 401 (refresh), context overflow (truncate)
  |
  +-- Images: AnthropicToOpenAI conversion
  |   -> POST https://open.bigmodel.cn/api/paas/v4/chat/completions
  |
  +-- ZAI_OPENAI_MODELS: OpenAIProxy.ProxyOpenAI()
      -> POST https://api.z.ai/api/paas/v4/chat/completions
        |
        v
  Response relay (stream + non-stream):
    - SSE relay to client
    - Privacy unmasking in-flight
    - NO response rewriting (no HTML conversion, no stop_reason rewrite,
      no server_tool filtering, no XML stripping)
    - Token tracking + telemetry feedback
```

**Key config:**
- `UPSTREAM_URL` = `https://api.z.ai/api/anthropic`
- `NATIVE_VISION_URL` = `https://open.bigmodel.cn/api/paas/v4/chat/completions`
- `ZAI_OPENAI_URL` = `https://api.z.ai/api/paas/v4/chat/completions`
- Default model: `glm-5`
- Key pool: round-robin from `ZAI_API_KEYS`, synced every 30s

---

## 2. Claude OAuth

```
Request enters handler
        |
        v
  Resolve "claude-*" -> provider: claude-oauth (primary), anthropic (fallback)
        |
        v
  Auth detection:
    +-- Client has Bearer sk-ant-oat01-* or x-api-key sk-ant-oat01-*
    |   -> Transparent passthrough (client's own token)
    |   -> ResolveTransparent() builds decision without stored token
    |
    +-- No client token
        -> tryResolveRoundRobin() selects from managed pool
        -> Picks lowest-utilization account from TokenStore
        |
        v
  stripUnsupportedFields():
    - Remove thinking/budget_tokens/effort for haiku, 3-5-sonnet
    - Keep context_management if nativeAnthropic=true
        |
        v
  Privacy masking
        |
        v
  Billing header injection (3-path):
    +-- Path A (fast): InjectBillingHeader() in Go
    |   -> Modifies system prompt with cc_version, cch hash, identity
    |   -> ProxyTransparent()
    |
    +-- Path B (sidecar): Node.js sidecar at CLI_SIDECAR_URL
    |   -> TLS fingerprint-based injection
    |   -> ProxySidecar()
    |
    +-- Path C (bare): Direct proxy without billing header
        -> ProxyTransparent()
        |
        v
  -> POST https://api.anthropic.com/v1/messages
  -> Extra headers: anthropic-beta (interleaved-thinking, code-execution,
     extended-cache-ttl, prompt-caching), X-Stainless-*
        |
        v
  Response: direct relay + privacy unmasking
```

**Auth modes:**
- Managed pool: PKCE OAuth flow -> token stored in Redis -> round-robin
- Transparent: client provides own OAuth token -> passthrough

---

## 3. Anthropic Direct (API Key)

```
Resolve "claude-*" -> fallback to provider: anthropic
        |
        v
  Auth: x-api-key from TokenStore or profile override
  Upstream: https://api.anthropic.com
  Path: /v1/messages
  Headers: anthropic-version: 2023-06-01
        |
        v
  stripUnsupportedFields() + privacy masking
        |
        v
  ProxyTransparent() -> direct relay
```

---

## 4. OpenAI-Compatible Providers


```
Resolve model prefix -> provider -> format: FormatOpenAI, auth: bearer
        |
        v
  stripUnsupportedFields():
    - Remove context_management, service_tier, thinking params
    - Remove tools/tool_choice, stream_options (OpenAI format doesn't use these)
        |
        v
  AnthropicToOpenAI() conversion:
    - system message -> messages[0] with role: "system"
    - tool_use blocks -> tool_calls with function format
    - tool_result blocks -> tool role messages
    - image blocks -> image_url format
    - thinking blocks -> stripped
        |
        v
  Privacy masking
        |
        v
  OpenAIProxy.ProxyOpenAI():
    -> POST {UpstreamBase}/v1/chat/completions
    -> Authorization: Bearer {key}
    -> Retry on 429, 529, 500, 502, 503
        |
        v
  OpenAIToAnthropic() back-conversion:
    - function_call -> tool_use content blocks
    - tool role -> tool_result content blocks
    - finish_reason: stop -> stop_reason: end_turn
    - SSE format conversion (OpenAI -> Anthropic)
        |
        v
  Auto-continuation (if maxContinuations > 0):
    - On finish_reason: length -> append response, re-send with context
        |
        v
  Response: relay + unmasking
```

**Per-provider upstream URLs:**
| Provider | Base URL |
|----------|----------|
| openai | `https://api.openai.com` |
| copilot | `https://api.github.com/copilot` |
| openrouter | `https://openrouter.ai/api` |
| qwen | `https://dashscope.aliyuncs.com/compatible-mode` |
| deepseek | `https://api.deepseek.com` |
| kimi | `https://api.moonshot.cn` |
| huggingface | custom per-endpoint |
| ollama | `http://localhost:11434` |

---

## 5. Gemini (API Key)

```
Resolve "gemini-*" -> provider: gemini, format: FormatGemini, auth: api_key
        |
        v
  AnthropicToGemini() conversion:
    - Messages -> contents array with role: user/model
    - tool_use -> functionCall format
    - system prompt -> systemInstruction
        |
        v
  -> POST https://generativelanguage.googleapis.com/v1beta/models/{model}:streamGenerateContent?alt=sse&key={apiKey}
        |
        v
  GeminiToAnthropic() back-conversion + relay
```

---

## 6. Gemini OAuth (Code Assist)

```
Resolve "gemini-*" -> provider: gemini-oauth, format: FormatGemini, auth: bearer
        |
        v
  Auth: Google OAuth token from TokenStore
        |
        v
  loadCodeAssist() -> get project ID from Google Cloud
        |
        v
  Wrap request in CodeAssist envelope:
    {
      "request": { gemini payload },
      "projectScoped": true,
      "metadata": { "project": "{project_id}", "region": "us-central1" }
    }
        |
        v
  -> POST https://cloudcode-pa.googleapis.com/v1internal
        |
        v
  GeminiToAnthropic() back-conversion + relay
```

---

## 7. Claude Session (Web Chat)

```
Client sends request with claude.ai session cookie
        |
        v
  Auth: session cookies from TokenStore
        |
        v
  Flow:
    1. GET organizations -> get org ID
    2. POST conversations -> create conversation
    3. POST conversations/{id}/completion -> send message
    4. DELETE conversations/{id} -> cleanup
        |
        v
  Convert claude.ai SSE format -> Anthropic Messages API format
  Handle pagination, thinking blocks, tool use
```

---

## 8. Custom Providers

```
Runtime registration via POST /v1/auth/custom-providers
  -> Stored in Redis at arl:providers:custom:{id}
  -> Loaded on startup via LoadCustomProviders()

Request resolution:
  -> model prefix -> custom provider -> FormatOpenAI
  -> Upstream: custom BaseURL from registration
  -> Auth: bearer with registered API key
```

---

## Provider Dispatch Summary

```
                     +-- glm-* -----> Z.AI (FormatAnthropic)
                     |
                     +-- claude-* --> Claude OAuth (FormatAnthropic)
                     |                fallback: Anthropic Direct
                     |
                     +-- gpt-,o3-,o4- -> OpenAI (FormatOpenAI)
                     |
  POST /v1/messages  +-- gemini-* --> Gemini OAuth / Gemini API Key
  or                 |                (FormatGemini)
  POST /v1/chat/     |
  completions        +-- qwen-* ----> Qwen (FormatOpenAI)
                     |
                     +-- deepseek-* -> DeepSeek (FormatOpenAI)
                     |
                     +-- or-* ------> OpenRouter (FormatOpenAI)
                     |
                     +-- kimi-* ----> Kimi (FormatOpenAI)
                     |
                     |
                     +-- custom-* --> Custom Provider (FormatOpenAI)
                     |
                     +-- (other) + GLM mode -> Z.AI fallback
```
