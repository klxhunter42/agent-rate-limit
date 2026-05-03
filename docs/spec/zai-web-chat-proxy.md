# Z.AI Web Chat Proxy Integration

## Overview

Added native Z.AI web chat API support to the gateway, replicating the [zai-proxy](https://github.com/kao0312/zai-proxy) functionality directly in Go. This enables free access to GLM models through chat.z.ai's signed web API without needing a separate proxy container.

## What Changed

### New Files

| File | Description |
|---|---|
| `proxy/zaiweb.go` | Z.AI web chat proxy: HMAC-SHA256 signing, JWT auth, FE version scraping, Anthropic<->Z.AI format conversion, streaming SSE translation |

### Modified Files

| File | Changes |
|---|---|
| `config/config.go` | Added `ZAIWebEnabled`, `ZAIWebToken`, `ZAIWebModels` fields + `IsZAIWebModel()` method + `parseModelList()` helper |
| `handler/handler.go` | Added `zaiWebProxy` field to Handler, ZAI web routing branch in `Messages()`, `ZAIWebStatus` + `ZAIWebSetToken` API endpoints |
| `main.go` | Initialize `ZAIWebProxy` when `ZAI_WEB_ENABLED=true`, pass to handler, register `/v1/zaiweb/*` routes |
| `.env` | Added `ZAI_WEB_ENABLED`, `ZAI_WEB_TOKEN`, `ZAI_WEB_MODELS` env vars |

## Architecture

```
+------------------+     +------------------------------------------------------+
| Claude Code /    |     |                  API Gateway                         |
| API Client       |     |                                                      |
|                  |     |  handler.Messages()                                  |
| POST /v1/messages|---->|    |                                                 |
| model: glm-5    |     |    +-- ZAI_WEB_ENABLED && IsZAIWebModel(model)?      |
+------------------+     |    |   |                        |                     |
                         |    |  YES                      NO                    |
                         |    |   |                        |                     |
                         |    |   v                        v                     |
                         |    | ZAIWebProxy           existing routing           |
                         |    | .ProxyZAIWeb()        (API key / OAuth / etc)    |
                         |    |   |                                              |
                         |    |   +-- Token (JWT or anonymous)                   |
                         |    |   +-- FE version scraper                         |
                         |    |   +-- Anthropic -> Z.AI format                   |
                         |    |   +-- HMAC-SHA256 signing                        |
                         |    |   +-- POST chat.z.ai/api/v2/...                 |
                         |    |   +-- Z.AI SSE -> Anthropic SSE                  |
                         |    |                                                  |
                         +----+--------------------------------------------------+
```

### Request Signing Flow

```
                          ZAIWebProxy.ProxyZAIWeb()
                                    |
                     +--------------+---------------+
                     |                              |
               1. Get Token                  2. Scrape FE Version
               (JWT or anonymous)            (GET chat.z.ai, regex prod-fe-X.X.X)
                     |                              |
                     +--------------+---------------+
                                    |
                      3. Convert Anthropic -> Z.AI Web Format
                         - system -> user+assistant pair
                         - content blocks -> plain text
                         - model name mapping
                                    |
                      4. Build Signed Request
                         +------------------------------------------+
                         | requestID = UUID                         |
                         | timestamp = now().UnixMilli()            |
                         | period = timestamp / (5*60*1000)        |
                         |                                          |
                         | Step 1: Time-derived key                 |
                         | firstHmac = HMAC-SHA256(staticKey,       |
                         |                        string(period))   |
                         |                                          |
                         | Step 2: Sign request data                |
                         | requestInfo = "requestId,{id},           |
                         |               timestamp,{ts},            |
                         |               user_id,{jwt_id}"          |
                         | contentBase64 = Base64(userContent)      |
                         | signData = requestInfo+"|"+              |
                         |            contentBase64+"|"+timestamp   |
                         | signature = HMAC-SHA256(firstHmac,       |
                         |                        signData)         |
                         +------------------------------------------+
                                    |
                      5. POST chat.z.ai/api/v2/chat/completions
                         Headers:
                           Authorization: Bearer {token}
                           X-FE-Version: prod-fe-0.0.1
                           X-Signature: {signature}
                           Origin: https://chat.z.ai
                           Referer: https://chat.z.ai/c/{chatID}
                         Query: timestamp, requestId, user_id,
                                version, platform, token,
                                current_url, signature_timestamp
                                    |
                      6. Convert Z.AI SSE -> Anthropic SSE
                         +------------------------------------------+
                         | Z.AI format:                             |
                         |   data: {"choices":[{"delta":            |
                         |     {"content":"text"}}]}                |
                         |                                          |
                         | Anthropic format:                        |
                         |   event: message_start                   |
                         |   event: content_block_start             |
                         |   event: content_block_delta             |
                         |     data: {"delta":{"text_delta":...}}   |
                         |   event: content_block_stop              |
                         |   event: message_delta                   |
                         |   event: message_stop                    |
                         +------------------------------------------+
```

### Z.AI Routing Paths (Full Gateway View)

```
                          handler.Messages()
                                |
                 +--------------+----------------+
                 |                               |
           has images?                      no images
                 |                               |
     +-----------+-----------+         resolve provider
     |           |           |               |
  zai?    non-zai?    zai openai?     +------+------+------+------+
   |         |          |             |      |      |      |      |
 vision   normal    openai        direct  oauth  oauth  fmt    proxy
 select   route     proxy         proxy  sidecar proxy         |
   |         |        |              |      |      |      |     |
   v         v        v              +------+------+------+-----+
 [glm-4.6v] [glm-5]                               |
 [glm-4.5v] etc     etc                            v
   |         |        |                    upstream response
   +----+----+--------+
        |
        v
  ZAI Web Chat? (ZAI_WEB_ENABLED + IsZAIWebModel)
    |
   YES --> ZAIWebProxy.ProxyZAIWeb() --> chat.z.ai (free, signed)
    |
   NO --> api.z.ai/api/anthropic (paid API key)
```

## Request Signing Algorithm

Replicated from zai-proxy's `internal/auth/signature.go`:

1. **Time-based key derivation**:
   ```
   period = timestamp_ms / (5 * 60 * 1000)
   firstHmac = HMAC-SHA256-Hex("key-@@@@)))()((9))-xxxx&&&%%%%%", period)
   ```

2. **Request signing**:
   ```
   requestInfo = "requestId,{uuid},timestamp,{ts},user_id,{jwt_id}"
   signData = "{requestInfo}|{base64(content)}|{timestamp}"
   signature = HMAC-SHA256-Hex(firstHmac, signData)
   ```

3. **Headers**:
   - `Authorization: Bearer {token}`
   - `X-FE-Version: prod-fe-{version}` (scraped from chat.z.ai)
   - `X-Signature: {signature}`

## Model Name Mapping

| Gateway Model | Z.AI Upstream Name |
|---|---|
| `glm-4.5` | `0727-360B-API` |
| `glm-4.6` | `GLM-4-6-API-V1` |
| `glm-4.7` | `glm-4.7` |
| `glm-5` | `glm-5` |
| `glm-5.1` | `glm-5.1` |
| `glm-4.5-v` | `glm-4.5v` |
| `glm-4.6-v` | `glm-4.6v` |
| `glm-4.5-air` | `0727-106B-API` |

## Configuration

### Environment Variables

| Variable | Default | Description |
|---|---|---|
| `ZAI_WEB_ENABLED` | `false` | Enable Z.AI web chat routing |
| `ZAI_WEB_TOKEN` | `""` | JWT Bearer token from chat.z.ai (empty = anonymous) |
| `ZAI_WEB_MODELS` | `""` | Comma-separated models to route through web chat. Supports prefix matching (e.g., `glm-` matches all GLM models) |

### Example Configuration

```env
# Enable ZAI web chat for all GLM models (free, no API key needed)
ZAI_WEB_ENABLED=true
ZAI_WEB_TOKEN=
ZAI_WEB_MODELS=glm-5,glm-5.1,glm-4.7,glm-4.6,glm-4.5

# Or use with a real JWT token for higher limits
ZAI_WEB_TOKEN=eyJhbGciOiJFUzI1NiIsInR5cCI6IkpXVCJ9...

# Or match all GLM models with prefix
ZAI_WEB_MODELS=glm-
```

## API Endpoints

### GET /v1/zaiweb/status

Returns current ZAI web chat proxy status.

```json
{
  "enabled": true,
  "token_set": true,
  "token_prefix": "eyJhbGciOiJFUzI1NiI...",
  "user_id": "7d714fb7-89ba-4efe-ab20-f711904ea09d",
  "models": ["glm-5", "glm-5.1", "glm-4.7"],
  "fe_version": "prod-fe-0.0.1"
}
```

### POST /v1/zaiweb/token

Update the JWT token at runtime (no restart needed).

```json
{
  "token": "eyJhbGciOiJFUzI1NiIsInR5cCI6IkpXVCJ9..."
}
```

## Token Options

| Mode | How | Limits | Notes |
|---|---|---|---|
| **Anonymous** | `ZAI_WEB_TOKEN=` (empty) | Lower rate limits | Auto-fetched from `chat.z.ai/api/v1/auths/` |
| **JWT** | Set `ZAI_WEB_TOKEN` or POST `/v1/zaiweb/token` | Higher rate limits | Extracted from browser cookies at chat.z.ai |

## What This Enables

- **Free GLM model access**: No API key or billing required
- **All GLM chat models**: glm-5, glm-5.1, glm-4.7, glm-4.6, glm-4.5, glm-4.5-air, etc.
- **Streaming support**: Real-time SSE response translation
- **Runtime token update**: Switch tokens without restart via API
- **Auto anonymous fallback**: No configuration needed for basic usage

## Limitations (Current)

- **Image upload not implemented**: Images marked as `[image attached]` in messages
- **Tool calling**: Basic support, complex tool schemas may not translate perfectly
- **System prompt injection**: Converted to user+assistant pair (Z.AI ignores system role)
- **Thinking mode**: Not yet exposed (features.enable_thinking = false)
- **Web search**: Not yet exposed (features.web_search = false)
- **Image generation**: Not yet exposed (features.image_generation = false)

## Relationship to Existing ZAI Routing

The gateway now has 3 Z.AI routing paths:

| Path | Endpoint | Auth | Use Case |
|---|---|---|---|
| **PaaS API** | `api.z.ai/api/paas/v4/` | API key + credits | Paid, highest limits |
| **Anthropic-compat** | `api.z.ai/api/anthropic` | Coding Plan key | Subscription chat only |
| **Web Chat** (NEW) | `chat.z.ai/api/v2/` | JWT/anonymous | Free, signed requests |

Routing priority in `Messages()`:
1. Image requests -> vision model auto-selection
2. **ZAI web chat** -> if model matches `ZAI_WEB_MODELS`
3. ZAI OpenAI endpoint -> if model in `ZAI_OPENAI_MODELS`
4. Provider resolution -> normal routing
