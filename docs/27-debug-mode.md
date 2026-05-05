# Debug Mode

Set `DEBUG=true` in `.env` to enable detailed request logging. All debug output is structured JSON via slog and respects the toggle - no code changes needed between sessions.

## Enable

```bash
# .env
DEBUG=true
```

Then restart:
```bash
docker-compose up -d arl-gateway
```

Disable by setting `DEBUG=false` or removing the variable (default is off).

## What Gets Logged

Each debug line appears only when `DEBUG=true`. Normal production logs (health, metrics, request completed) always appear regardless.

### 1. Incoming Request

**Log**: `debug incoming request`

| Field | Description |
|---|---|
| `method` | HTTP method |
| `path` | Request path |
| `content_length` | Raw body size |
| `model_requested` | Model from client |
| `model_selected` | Model after fallback |
| `stream` | Stream flag |
| `has_system` | Whether system prompt exists |
| `msg_count` | Number of messages |
| `provider` | Resolved provider (zai, anthropic, etc) |
| `auth_mode` | api_key or bearer |
| `headers` | All HTTP headers |

### 2. Raw Payload

**Log**: `debug RAW PAYLOAD before strip`

Full JSON payload as received from the client, before any field stripping or content filtering. Useful for seeing exactly what Claude Code Terminal vs VSCode Panel sends.

### 3. Post-Strip Analysis

**Log**: `debug strip result`

Shows which top-level keys remain after stripping unsupported fields (`tools`, `tool_choice`, `thinking`, `output_config`, `metadata`, `context_management`, etc). Also flags any prohibited fields that might have survived.

### 4. Content Block Analysis

**Log**: `debug content analysis`

Walks every message and system block, logging:
- Block type (text, image, tool_use, etc)
- Extra keys beyond the standard ones (e.g. `cache_control`)
- Per-message index for tracing

Example:
```
msg_blocks: ["msg[0]:text", "msg[0]:image", "msg[0]:text(cache_control:map[type:ephemeral])"]
sys_blocks: ["sys[0]:text", "sys[1]:text(cache_control:map[type:ephemeral])"]
```

### 5. Token Count (Before Optimizer)

**Log**: `debug tokens before optimize`

| Field | Description |
|---|---|
| `model` | Target model |
| `sys_tokens` | Estimated tokens in system prompt |
| `msg_tokens` | Estimated tokens in messages |
| `total_tokens` | sys + msg |
| `context_limit` | Model context window |
| `pct_used` | Percentage of context used |
| `budget_level` | 0=normal, 1=moderate(>60%), 2=aggressive(>80%) |

### 6. Token Count (After Optimizer)

**Log**: `debug tokens after optimize`

| Field | Description |
|---|---|
| `sys_tokens_before` | System tokens before optimization |
| `sys_tokens_after` | System tokens after optimization |
| `saved_tokens` | Tokens saved |
| `optimized` | Whether any changes were made |

### 7. Privacy Masking

**Log**: `debug privacy detail`

| Field | Description |
|---|---|
| `secret_types` | List of secret entity types found |
| `pii_types` | List of PII entity types found |
| `body_len_before` | Body size before masking |
| `body_len_after` | Body size after masking |

Normal `privacy mask applied` log (secrets_count, pii_count) always shows. The debug line adds the specific types and size delta.

## Typical Debugging Flow

### 1210 Error from Z.AI

```bash
# 1. Enable debug
echo "DEBUG=true" >> .env
docker-compose up -d arl-gateway

# 2. Reproduce the error from VSCode Panel

# 3. Check logs
docker logs arl-gateway --tail 200 2>&1 | grep debug

# 4. Look at:
#    - debug RAW PAYLOAD before strip -> see what client sent
#    - debug strip result -> verify fields were stripped
#    - debug content analysis -> check for unsupported block types
#    - upstream error response -> the 1210 from Z.AI
```

### Token Optimization Not Working

```bash
# Check before/after token counts
docker logs arl-gateway 2>&1 | grep "debug tokens"
```

### Privacy Masking Issues

```bash
# Check what's being detected
docker logs arl-gateway 2>&1 | grep "debug privacy"
```

## Log Filtering

```bash
# All debug lines
docker logs arl-gateway 2>&1 | grep "debug "

# Only token counts
docker logs arl-gateway 2>&1 | grep "debug tokens"

# Only raw payload (large!)
docker logs arl-gateway 2>&1 | grep "debug RAW PAYLOAD" | tail -1 | python3 -m json.tool

# Only for specific model
docker logs arl-gateway 2>&1 | grep "debug incoming" | grep "glm-4.6v"

# Exclude health/metrics noise
docker logs arl-gateway 2>&1 | grep -v "health\|metrics" | grep "debug\|upstream error"
```

## Performance Impact

Debug mode has minimal overhead for non-image requests:
- JSON marshal of payload for raw dump (one extra allocation per request)
- Token estimation is already computed for budget level
- Content block walk is O(n) over message blocks

For image requests, debug logging is limited to headers and metadata - base64 image data is not logged separately (it's already in the raw payload dump).
