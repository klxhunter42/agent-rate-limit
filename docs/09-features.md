# Features

## 1. Vision Auto-Routing

Gateway automatically detects image content in requests and routes to the native Zhipu vision endpoint instead of Z.AI Anthropic endpoint, with **auto-select vision model** based on image size and **SSE streaming** in real-time.

### Flow Diagram

```
Client sends request with images
|
v
arl-gateway (:8080)
|-- HasImageContent() scan messages for image blocks
|
+-- No images: ProxyTransparent -> Z.ai (as before)
|
+-- Has images: analyzeImagePayload()
    |-- Calculate totalBase64Bytes + imageCount
    |-- selectVisionModel(): score = totalBase64KB + (imageCount * 300)
    |   |-- score <= 2000 && count < 3 -> glm-4.6v (10 slots, best quality)
    |   |-- score > 2000 || count >= 3 -> glm-4.6v (heavy payload fallback)
    |
    |-- filterUnsupportedContent():
    |   strip server_tool_use blocks
    |   convert Anthropic image -> GLM image_url format
    |
    |-- AnthropicToOpenAI():
    |   Anthropic Messages format -> OpenAI/Zhipu format
    |   image blocks: {source:{type,media_type,data}} -> {image_url:{url}}
    |   system role: text prepend to first user message
    |   strip: server_tool_use, tool_use, tool_result, other unsupported
    |   only pass: user/assistant roles, text/image/image_url content types
    |
    |-- POST to NATIVE_VISION_URL
    |   Bearer auth with API key
    |
    +-- stream=true?
        |-- YES: convertZhipuStreamResponse()
        |   Zhipu SSE (OpenAI format) -> Anthropic SSE events
        |   message_start -> content_block_start -> content_block_delta...
        |   -> content_block_stop -> message_delta -> message_stop
        |
        |-- NO: zhipuToAnthropic()
            Zhipu JSON -> Anthropic JSON response
```

### Vision Model Auto-Select

Gateway auto-selects vision model based on **scoring formula**:

```
score = totalBase64KB + (imageCount * 300)
```

| Score / Condition | Selected Model | Slots | Reason |
|---|---|---|---|
| score <= 2000 && count < 3 | `glm-4.6v` | 10 | Best quality, high capacity |
| score > 2000 or count >= 3 | `glm-4.6v` | 10 | Fallback for heavy payloads |

**Examples:**

| Scenario | Total KB | Count | Score | Model |
|---|---|---|---|---|
| 1 screenshot (200KB) | 200 | 1 | 500 | glm-4.6v |
| 1 photo (1.5MB) | 1500 | 1 | 1800 | glm-4.6v |
| 2 photos (1MB each) | 2000 | 2 | 2600 | glm-4.6v |
| 5 screenshots (100KB each) | 500 | 5 | 2000 | glm-4.6v |
| 1 large photo (3MB) | 3000 | 1 | 3300 | glm-4.6v |

### SSE Streaming for Vision

Vision responses support SSE streaming -- Zhipu SSE chunks are converted to Anthropic SSE format in real-time:

```
Zhipu SSE (OpenAI format):
data: {"choices":[{"delta":{"content":"Hello"}}]}

Converted to Anthropic SSE:
event: content_block_delta
data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}
```

Supports both `delta.content` and `delta.reasoning_content` from Zhipu.

### Supported Vision Models

| Model | Slots | Status | Notes |
|---|---|---|---|
| `glm-5.1` | 5 | Native image input | V5 series, natively supports images |
| `glm-4.6v` | 5 | Recommended | Best quality, auto-selected default |
| `glm-4.5v` | 3 | Available | Good quality |

Native image models (bypass auto-selection): `glm-5.1`, `glm-4.6v`, `glm-4.5v`.

### Image Formats Supported

```json
// Anthropic base64 (auto-converted)
{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "..."}}

// Anthropic URL (auto-converted)
{"type": "image", "source": {"type": "url", "url": "https://..."}}

// Converted to GLM format before sending:
{"type": "image_url", "image_url": {"url": "data:image/png;base64,..."}}
```

### Configuration

```bash
# Native Zhipu vision endpoint (default)
NATIVE_VISION_URL=https://open.bigmodel.cn/api/paas/v4/chat/completions

# Vision model concurrency limits (separate from text model limits)
UPSTREAM_VISION_MODEL_LIMITS=glm-5.1:5,glm-4.6v:5,glm-4.5v:3
```

### Limitations

| Limitation | Detail |
|---|---|
| Privacy pipeline skipped | Vision path does not go through privacy masking |
| tool_use on vision stripped | `server_tool_use`, `tool_use`, `tool_result` content blocks are filtered before sending (Z.AI doesn't support them) |
| No auto-resize | Large images may be slow/fail |

> **Note**: Error 1210 ("API parameter error") that previously occurred from sending `system` role and Anthropic-specific content blocks has been fixed (commit 7c08cb0) -- gateway now auto-filters roles and content types.

---

## 2. Multi-Agent and Mode Selection

### Sync vs Async -- Which Mode

| Use Case | Mode | Endpoint |
|---|---|---|
| **Claude Code (interactive)** | Sync | `POST /v1/messages` |
| **Multiple Claude Code on same machine** | Sync | Each session uses different key |
| **CI/CD pipeline** | Async | `POST /v1/chat/completions` |
| **Batch processing (100+ jobs)** | Async | Send then poll result |
| **Agent framework (5-50 agents)** | Async | Each agent sends separate `agent_id` quota |
| **Cron / scheduled tasks** | Async | Queue manages pacing automatically |

### Sync Mode -- For Claude Code

```bash
# Set in ~/.claude/settings.json
{
  "ANTHROPIC_BASE_URL": "http://localhost:8080",
  "ANTHROPIC_AUTH_TOKEN": "your-glm-key"
}
```

- Real-time SSE streaming
- Tool loop works like direct API
- Per-key rate limit: `AGENT_RATE_LIMIT=5` (5 req/min per key)
- No need to set `GLM_API_KEYS` in `.env` (key comes from client)

### Async Mode -- For Batch Agents

```bash
# Send job
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "glm-5",
    "agent_id": "my-agent-1",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
# Response: {"request_id": "abc-123", "status": "queued"}

# Poll result
curl http://localhost:8080/v1/results/abc-123
```

- Must set `GLM_API_KEYS` in `.env` (worker needs keys)
- Queue + worker manages pacing automatically
- Per-agent rate limit (`agent_id` separates quota)
- Auto retry + exponential backoff
- Provider fallback chain

### Increasing Throughput

```bash
# 1 key = 5 RPM (default)
GLM_API_KEYS=key1

# 3 keys = 15 RPM
GLM_API_KEYS=key1,key2,key3
PROVIDER_RPM_LIMITS=glm:15

# Multiple providers = max throughput
GLM_API_KEYS=k1,k2,k3
OPENAI_API_KEYS=sk1,sk2
PROVIDER_RPM_LIMITS=glm:15,openai:120
```

### Scale Guidelines

| Scale | Mode | Config |
|---|---|---|
| 1 developer | Sync | Single key |
| 2-5 developers | Sync | Each person uses different key |
| 1 team + CI/CD | Sync + Async | Dev sync, CI async |
| Agent framework (5-50) | Async | `WORKER_CONCURRENCY=50` |
| Heavy batch (100+) | Async | Multiple keys + multiple providers |

---

## 3. Message Body Optimization

Gateway performs token optimization at 2 levels: system prompt (7-stage pipeline from 13 components) and message content (lightweight whitespace + dedup).

### Pipeline Flow

```
Request body (JSON)
|
+-- System prompt optimization (OptimizeSystemPrompt)
|   +-- Semantic dedup (DeduplicateSemantic)
|   +-- Chunker (F1)
|   +-- Delta encoding (F8)
|   +-- Sketch dedup (F9)
|   +-- Summarizer (F6, red budget only)
|   +-- Intent filter (F13)
|   +-- Caveman compression (F16)
|
+-- Message content optimization (OptimizeMessages)
|   +-- String content: whitespace collapse + sentence dedup
|   +-- Text blocks: whitespace collapse + sentence dedup
|   +-- Tool result blocks: whitespace collapse + sentence dedup
|   +-- Skip: tool_use blocks, code blocks (```...```), privacy placeholders
|
+-- Privacy masking (PasteGuard)
    +-- Detect + mask secrets/PII
```

### Content Types Handled

| Content Type | Optimized? | How |
|---|---|---|
| `messages[].content` (string) | Yes | `OptimizeWhitespace` + `DeduplicateSentences` |
| `messages[].content[].text` blocks | Yes | Same, metric: `message_block_text` |
| `messages[].content[]` tool_result `content` field | Yes | Same, metric: `message_block_tool_result` |
| `messages[].content[]` tool_use | No | Skipped (JSON input, not prose) |
| Code blocks inside text (` ```...``` `) | No | `SplitCodeBlocks` preserves code verbatim |
| Privacy placeholders (`__SECRET_1__`, `__PII_1__`) | No | Dedup skipped when placeholders present |

### Metrics

```
api_gateway_optimizer_runs_total{technique="message_text"} N
api_gateway_optimizer_chars_saved_total{technique="message_text"} M
api_gateway_optimizer_runs_total{technique="message_block_text"} N
api_gateway_optimizer_runs_total{technique="message_block_tool_result"} N
```
