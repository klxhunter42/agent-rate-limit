# Troubleshooting

## Service Not Healthy

```bash
docker-compose ps                           # Check status
docker-compose logs <service> --tail 50     # View logs
docker-compose up -d --build <service>      # Rebuild
```

### Service Names

| Compose Service    | Container Name   | Role                            |
|--------------------|------------------|---------------------------------|
| `arl-gateway`      | arl-gateway      | API Gateway (Go)                |
| `arl-rate-limiter` | arl-rate-limiter | Rate Limiter (Java/Spring)      |
| `arl-dragonfly`    | arl-dragonfly    | Dragonfly (Redis-compatible)    |
| `arl-worker`       | arl-worker       | AI Worker (Python)              |
| `arl-prometheus`   | arl-prometheus   | Metrics collection              |
| `arl-grafana`      | arl-grafana      | Dashboards                      |
| `arl-otel`         | arl-otel         | OpenTelemetry Collector         |
| `arl-rl-dashboard` | arl-rl-dashboard | Rate Limiter Dashboard (React)  |
| `arl-proxy`        | arl-proxy        | Caddy reverse proxy (port 9000) |

> **Note:** The main gateway dashboard is embedded in the Go binary (no separate container). It is served by `arl-gateway` on port 8080 (via Caddy on port 9000).

## DOCKER_DEFAULT_PLATFORM

If you encounter error `platform (linux/amd64) does not match`:

```bash
unset DOCKER_DEFAULT_PLATFORM
# Or add to ~/.zshrc / ~/.bashrc
```

## arl-worker crash (SettingsError)

```bash
# Check .env for empty or incorrectly formatted values
cat .env | grep API_KEYS
# If not using a provider, delete that line or leave empty
```

## Rate Limiter Returns 403

```bash
# Check docker profile is active
docker exec arl-rate-limiter env | grep SPRING_PROFILES_ACTIVE
# Should return: SPRING_PROFILES_ACTIVE=docker
```

## Reset Everything

```bash
docker-compose down -v && docker-compose up -d --build
```

> **Warning**: `down -v` removes all volumes including Grafana dashboards and Dragonfly data.

## Port Reference

| Port      | Service                  | External                | Protocol  |
|-----------|--------------------------|-------------------------|-----------|
| **9000**  | arl-proxy (Caddy)        | **Yes**                 | HTTP      |
| 8080      | arl-gateway              | No (internal)           | HTTP      |
| 8080      | arl-rate-limiter         | No (internal)           | HTTP      |
| 6379      | arl-dragonfly (Redis)    | No (internal)           | TCP       |
| 9090      | arl-prometheus           | No (internal)           | HTTP      |
| 9090/9091 | arl-worker (metrics)     | No (internal)           | HTTP      |
| 5173      | Dashboard UI (Vite dev)  | No (internal)           | HTTP      |
| 3000      | arl-grafana              | No (via Caddy /grafana) | HTTP      |
| 4317/4318 | arl-otel (OTLP)          | No (internal)           | gRPC/HTTP |

All external traffic goes through arl-proxy (Caddy) on port 9000, which reverse-proxies to internal services. No other ports are published to the host.

## Testing Z.AI Vision (GLM Mode)

### Terminal Test (curl via Docker network)

Test a simple image request through the gateway from inside the Docker network:

```bash
# 1. Base64-encode an image
IMG_B64=$(base64 -i ~/Pictures/KLxPicture/devops.png | tr -d '\n')

# 2. Send via the proxy container (has access to arl-gateway)
ssh klxhunter@192.168.5.111 "docker exec arl-proxy curl -s -X POST \
  http://arl-gateway:8080/v1/messages \
  -H 'Content-Type: application/json' \
  -H 'x-api-key: YOUR_ZAI_API_KEY' \
  -H 'anthropic-version: 2023-06-01' \
  -d '{\"model\":\"glm-5.1\",\"max_tokens\":100,\"stream\":true,\"messages\":[{\"role\":\"user\",\"content\":[{\"type\":\"image\",\"source\":{\"type\":\"base64\",\"media_type\":\"image/png\",\"data\":\"$IMG_B64\"}},{\"type\":\"text\",\"text\":\"What do you see?\"}]}]}'"
```

For large payloads (>100KB), write to file first to avoid shell escaping issues:

```bash
# 1. Generate payload with Python
python3 -c "
import json, base64
img = base64.b64encode(open('$HOME/Pictures/KLxPicture/devops.png','rb').read()).decode()
payload = {
    'model': 'glm-5.1',
    'max_tokens': 100,
    'stream': True,
    'messages': [{'role': 'user', 'content': [
        {'type': 'image', 'source': {'type': 'base64', 'media_type': 'image/png', 'data': img}},
        {'type': 'text', 'text': 'What do you see?'}
    ]}]
}
json.dump(payload, open('/tmp/test_payload.json','w'))
"

# 2. Copy to server and into Docker container
scp /tmp/test_payload.json klxhunter@192.168.5.111:/tmp/test_payload.json
ssh klxhunter@192.168.5.111 "docker cp /tmp/test_payload.json arl-proxy:/tmp/test_payload.json"

# 3. Send from inside the proxy container
ssh klxhunter@192.168.5.111 'docker exec arl-proxy curl -s -X POST \
  http://arl-gateway:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: YOUR_ZAI_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -d @/tmp/test_payload.json'
```

### Terminal Test (direct to Z.AI)

Bypass the gateway to test if Z.AI itself accepts the payload:

```bash
curl -s -X POST https://api.z.ai/api/anthropic/v1/messages \
  -H 'Content-Type: application/json' \
  -H 'x-api-key: YOUR_ZAI_API_KEY' \
  -H 'anthropic-version: 2023-06-01' \
  -d '{"model":"glm-4.6v","max_tokens":50,"stream":true,"messages":[{"role":"user","content":"Say hi"}]}'
```

### VSCode Claude Code Panel Test

1. Open VSCode with Claude Code extension configured to use the gateway proxy
2. Open the Claude Code panel (sidebar or command palette)
3. Attach an image: drag & drop or paste from clipboard (e.g., `~/Pictures/KLxPicture/*.png`)
4. Type a prompt like "Describe this image" and send
5. Check gateway logs for the result:

```bash
ssh klxhunter@192.168.5.111 "docker logs arl-gateway --tail 30 2>&1 | grep -v health\|metrics"
```

**What to look for in logs:**
- `strip debug` - confirms field stripping ran (all should be `false`)
- `content analysis` - shows all content block types and extra keys
- `vision model auto-selected` - confirms glm-5.1 -> glm-4.6v routing
- `vision via anthropic endpoint` - shows final payload_keys and body_preview
- `upstream error response` with code `1210` - Z.AI rejected the request

**Error 1210 (Invalid API parameter)** means Z.AI received a field it does not support. The gateway strips these fields automatically:
- Top-level: `tools`, `tool_choice`, `thinking`, `budget_tokens`, `effort`, `stream_options`, `metadata`, `output_config`, `context_management`, `service_tier`
- Content blocks: `cache_control` on any block (server_tool_use no longer filtered)

If 1210 still occurs, the `content analysis` log will show exactly which block types and extra keys remain in the payload.

## GLM "undefined" Garbled Output in Response

**Symptom:** GLM model responses contain bare "undefined" tokens in output text. Can appear as single "undefined" or repeated "undefinedundefinedundefined...".

**Root cause:** GLM models emit "undefined" instead of preserving `[[TYPE_N]]` placeholder tokens. This happens regardless of whether privacy masking is active.

**Why it keeps coming back:** The fix was originally applied only to `relayStreamWithTracking` (main Anthropic streaming path). GLM requests can route through multiple proxy paths (OpenAI, Gemini, Claude Session, Zhipu conversion), and none of those had sanitization. Additionally, the regex `(?:undefined[\s]*){2,}` only matched 2+ consecutive, so single "undefined" passed through.

**Fix (2026-05-10): Comprehensive coverage across all proxy paths:**

1. Regex changed from `{2,}` to `+` (matches single + repeated "undefined")
2. `SanitizeGarbledOutput` added to ALL 19 output paths across 6 proxy files
3. All flush paths (`unmasker.Flush()`, `stripper.Flush()`) now sanitize before writing to client

```
Sanitization coverage (every output path):

  anthropic.go:
    relayStreamWithTracking text/thinking    -> SanitizeGarbledOutput
    relayStreamWithTracking flush            -> SanitizeGarbledOutput
    ProxySidecar ProcessChunk (active)       -> SanitizeGarbledOutput
    ProxySidecar (inactive)                  -> SanitizeGarbledOutput
    ProxySidecar flush                       -> SanitizeGarbledOutput
    handleNonStreamResponse                  -> SanitizeGarbledOutput
    convertOpenAIStreamResponse              -> SanitizeGarbledOutput
    convertOpenAIResponse non-stream         -> SanitizeGarbledOutput

  openai.go:
    streaming text path                      -> SanitizeGarbledOutput
    streaming flush paths                    -> SanitizeGarbledOutput
    non-stream response                      -> SanitizeGarbledOutput

  gemini-apikey.go:
    streaming ProcessChunk                   -> SanitizeGarbledOutput
    streaming flush                          -> SanitizeGarbledOutput
    non-stream (raw fallback)                -> SanitizeGarbledOutput
    non-stream (converted)                   -> SanitizeGarbledOutput

  gemini-codeassist.go:
    streaming ProcessChunk                   -> SanitizeGarbledOutput
    streaming flush                          -> SanitizeGarbledOutput
    non-stream response                      -> SanitizeGarbledOutput

  claude-session.go:
    streaming ProcessChunk                   -> SanitizeGarbledOutput
    streaming flush                          -> SanitizeGarbledOutput
```

**Stream pipeline order (per content_block_delta):**
```
1. stripper.Feed(text)       <- buffer + strip HTML/XML
2. unmasker.ProcessChunk()   <- unmask PII/secrets
3. SanitizeGarbledOutput()   <- strip garbled "undefined"
4. relay to client
```

**Verification:**
```bash
# Check gateway is running latest code
ssh klxhunter@192.168.5.111 "docker logs arl-gateway --tail 5 2>&1"

# Search for "undefined" in a test GLM response
ssh klxhunter@192.168.5.111 'docker exec arl-proxy curl -s -X POST \
  http://arl-gateway:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: YOUR_ZAI_API_KEY" \
  -H "anthropic-version: 2023-06-01" \
  -d "{\"model\":\"glm-5.1\",\"max_tokens\":50,\"stream\":true,\"messages\":[{\"role\":\"user\",\"content\":\"Say hello\"}]}"' | grep -i undefined
# Should return nothing (all "undefined" stripped)
```

## GLM Streaming: `<details><summary>` HTML Tags in Response

**Symptom:** GLM model responses contain raw HTML tags like `<details><summary>temp/</summary>` instead of formatted markdown. Happens in streaming mode only.

**Why Claude Code CLI normally doesn't have this problem:** CLI connects directly to Anthropic API (Claude models). Claude models never emit `<details><summary>` HTML. This noise is specific to GLM models (Z.AI) only. The gateway must clean it before forwarding to the client.

**Root cause:** The `toolUseStripper` struct (defined in `proxy/anthropic.go`) was only wired into `openai.go` streaming, never into `relayStreamWithTracking` (the Anthropic streaming handler). GLM models emit `<details><summary>` as formatting noise that must be buffered across SSE chunks and converted to markdown.

```
GLM SSE chunks arrive split across multiple events:
  chunk 1: "Here is the <det"
  chunk 2: "ails><summary>temp/"
  chunk 3: "</summary>"

Without toolUseStripper:
  → client sees raw HTML fragments as-is

With toolUseStripper:
  chunk 1: "Here is the <det"  → buffer (incomplete tag)
  chunk 2: "ails><summary>temp/" → buffer (still incomplete)
  chunk 3: "</summary>"         → complete! convert to markdown
  → client sees: "Here is the **temp/**"
```

**Fix (2026-05-09):** Wired `toolUseStripper` into `relayStreamWithTracking`:
1. Initialize `stripper = &toolUseStripper{}` for GLM models (`strings.HasPrefix(model, "glm-")`)
2. Apply `stripper.Feed()` on text deltas before unmasking (step in stream pipeline)
3. Call `stripper.Flush()` at stream end before unmasker flush
4. Also wired `convertHTMLDetails()` into both non-stream paths (`handleNonStreamResponse` and `ProxySidecar`)

**Stream pipeline order (per content_block_delta):**
```
1. stripper.Feed(text)       ← NEW: buffer + strip HTML/XML
2. unmasker.ProcessChunk()   ← existing: unmask PII/secrets
3. SanitizeGarbledOutput()   ← existing: strip repeated "undefined"
4. relay to client
```

**Verification:**
```bash
ssh klxhunter@192.168.5.111 "docker logs arl-gateway --tail 50 2>&1 | grep stripper"
# Should see: "stripper flushed remaining" if any HTML was processed
```

## Agent 401: GLM Model Request with Wrong API Key

**Symptom:** Agent (Claude Code, VS Code) calls gateway with `model=glm-*` and gets 401. Server has `GLM_MODE=true`.

**Root cause chain:**

1. `ZAI_API_KEYS` key pool is empty or all keys are exhausted
2. Fallback at `handler.go:694` picks up client's `x-api-key` (Claude OAuth token `sk-ant-oat01-*`)
3. `handler.go:715` detects `sk-ant-oat01-` prefix and forces `transparent = true`
4. `ProxyTransparent` forwards ALL headers to Z.AI upstream with the Anthropic OAuth token
5. Z.AI doesn't recognize Anthropic tokens -> 401

```
BEFORE (401 - opaque error)

  Agent: model=glm-5.1, x-api-key=sk-ant-oat01-...
        |
        v
  Key resolution:
    profile?       -> no
    transparent?   -> no
    decision key?  -> empty
    key pool?      -> empty
    fallback:      -> client x-api-key = "sk-ant-oat01-..."
        |
        v
  sk-ant-oat01- detected -> transparent = true
        |
        v
  ProxyTransparent: sends Anthropic token to api.z.ai
        |
        v
  Z.AI: 401 (doesn't recognize Anthropic OAuth token)
        |
        v
  Agent sees opaque "Unauthorized" with no actionable info


AFTER (clear rejection before upstream)

  Agent: model=glm-5.1, x-api-key=sk-ant-oat01-...
        |
        v
  Key resolution:
    profile?       -> no
    transparent?   -> no
    decision key?  -> empty
    key pool?      -> empty
    fallback:      -> client x-api-key = "sk-ant-oat01-..."
        |
        v
  GLM guard (handler.go):
    GLMMode=true?         YES
    ProviderID=="zai"?    YES (glm-5.1 routes to zai)
    apiKey="sk-ant-oat01-"? YES
        |
        v
  REJECT immediately:
    401 "No Z.AI API key available.
    Configure ZAI_API_KEYS or use a profile
    with a Z.AI token."
        |
        v
  Agent sees clear error with actionable fix
```

**Fix (2026-05-09):** Two-part fix:

1. **GLM guard** (`handler.go:715-729`): Before transparent-mode detection, check if `GLM_MODE=true`, provider is `zai`, and the resolved API key is an Anthropic OAuth token. If so, reject immediately with a clear error message instead of forwarding to Z.AI.

2. **Transparent 401 handler** (`proxy/anthropic.go`): Added retry logic in `ProxyTransparent` that attempts token refresh when upstream returns 401 for transparent OAuth requests (covers the case where a Claude OAuth token expires mid-session).

**Verification:**
```bash
# Check if key pool is the issue
ssh klxhunter@192.168.5.111 "docker logs arl-gateway --tail 100 2>&1 | grep -E 'key pool|glm mode'"

# Check for GLM guard rejections
ssh klxhunter@192.168.5.111 "docker logs arl-gateway --tail 100 2>&1 | grep 'glm request with anthropic oauth token rejected'"

# Check for transparent token sent to Z.AI (should NOT appear after fix)
ssh klxhunter@192.168.5.111 "docker logs arl-gateway --tail 100 2>&1 | grep -E 'transparent.*sidecar|upstream 401'"
```

