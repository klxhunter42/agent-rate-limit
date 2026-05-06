# Claude-OAuth Transparent Passthrough

## Overview

Transparent passthrough is a request routing mode for the `claude-oauth` provider that forwards the exact raw request bytes and headers from the Claude Code CLI to the Anthropic API without any modifications.

When Claude Code CLI sends requests through the gateway, the gateway normally runs a modification pipeline: system prompt injection, token optimization, smart max_tokens, field stripping, content filtering, JSON re-marshaling, and privacy masking. These transformations are designed for non-Anthropic upstreams (Z.AI, OpenAI, Gemini) that need payload normalization.

For `claude-oauth` requests hitting native Anthropic, these modifications are destructive:

- `system` array with `cache_control` markers gets flattened to a plain string, destroying prompt cache markers
- `json.Marshal` reorders JSON fields and changes numeric formatting, producing a different request fingerprint
- `anthropic-beta` header gets merged with gateway defaults or stripped, losing CLI-specific flags like `prompt-caching-scope-2026-01-05`
- `anthropic-version` gets overwritten with a static gateway value instead of the CLI's version

Transparent passthrough preserves the original payload byte-for-byte and forwards client headers as-is, maintaining prompt caching, correct request fingerprinting, and CLI beta flag compatibility.


## Request Flow

```
                              Claude Code CLI
                                    |
                                    | POST /v1/messages
                                    | Authorization: Bearer <oauth>
                                    | anthropic-beta: prompt-caching-...
                                    | anthropic-version: 2025-...
                                    |
                                    v
                        +------------------------+
                        |    Gateway Handler     |
                        |   Messages() function  |
                        +------------------------+
                                    |
                      +-------------+-------------+
                      |                           |
              provider == claude-oauth     provider != claude-oauth
              AND Bearer token present          |
                      |                         |
                      v                         v
            +------------------+      +------------------+
|  TRANSPARENT     |      |  NORMAL PATH     |
|  MODE            |      |                  |
|                  |      | 1. clampMaxTokens|
|  body = rawBody  |      | 2. injectSystem  |
|  (no changes)    |      |    Prompt        |
|                  |      | 3. OptimizeSystem|
            +------------------+      |    Prompt        |
|               | 4. applySmartMax |
|               |    Tokens        |
|               | 5. stripUnsupp   |
|               |    ortedFields   |
|               | 6. filterUnsupp  |
|               |    ortedContent  |
|               | 7. json.Marshal  |
|               | 8. privacy.Mask  |
|               |    Request       |
                      |               +------------------+
                      |                         |
                      +-------------+-----------+
                                    |
                                    v
                        +------------------------+
                        |  ProxyTransparent()    |
                        +------------------------+
                                    |
                      +-------------+-------------+
                      |                           |
              transparent == true        transparent == false
                      |                           |
                      v                           v
            +------------------+      +------------------+
| Forward raw body |      | Run whitespace   |
| Forward client   |      | dedup on system  |
|  anthropic-beta  |      | Set static       |
| Forward client   |      |  anthropic-      |
|  anthropic-      |      |  version         |
|  version         |      | Merge/override   |
| Skip beta strip  |      |  anthropic-beta  |
            +------------------+      | Strip unsupported|
|               |  betas           |
                      |               +------------------+
                      +-------------+-----------+
                                    |
                                    v
                           Anthropic API
                        api.anthropic.com
```


## Detection Logic

Transparent mode activates when ALL of the following conditions are true:

1. The resolved provider for the requested model is `claude-oauth`
2. The request includes an `Authorization: Bearer <token>` header
3. The Bearer token is non-empty and does NOT start with `arl_` (gateway-issued profile tokens)

Detection happens early in `Messages()`, immediately after extracting `requestedModel` from the parsed JSON body:

```go
// handler/handler.go - lines 288-299
transparent := false
if h.resolver != nil {
    d := h.resolver.Resolve(requestedModel)
    if d != nil && d.ProviderID == "claude-oauth" {
        if ah := r.Header.Get("Authorization"); strings.HasPrefix(ah, "Bearer ") {
            if tok := strings.TrimPrefix(ah, "Bearer "); tok != "" && !strings.HasPrefix(tok, "arl_") {
                transparent = true
            }
        }
    }
}
rawBody := body
```

The original `body` bytes are saved to `rawBody` before any modifications. If transparent is true, the modification pipeline is skipped entirely and `body = rawBody` is restored before proxying.

### Token selection with transparent mode

When transparent mode is detected, the gateway prefers the client's Bearer token over the stored provider API key:

```go
// handler/handler.go - lines 411-418
if decision.ProviderID == "claude-oauth" {
    if ah := r.Header.Get("Authorization"); strings.HasPrefix(ah, "Bearer ") {
        if tok := strings.TrimPrefix(ah, "Bearer "); tok != "" && !strings.HasPrefix(tok, "arl_") {
            apiKey = tok
        }
    }
}
```

This ensures the CLI's own valid OAuth session is used instead of potentially stale stored tokens.


## Modification Pipeline Comparison

| Pipeline Stage             | Normal Mode                                              | Transparent Mode |
|----------------------------|----------------------------------------------------------|------------------|
| `clampMaxTokens`           | Applied (caps to model hard limit)                       | SKIPPED          |
| `injectSystemPrompt`       | Applied (prepends token efficiency prompt)               | SKIPPED          |
| `OptimizeSystemPrompt`     | Applied (13-stage optimizer pipeline)                    | SKIPPED          |
| `applySmartMaxTokens`      | Applied (sets default max_tokens)                        | SKIPPED          |
| `stripUnsupportedFields`   | Applied (removes context_management, thinking for haiku) | SKIPPED          |
| `filterUnsupportedContent` | Applied (removes server_tool_use, rewrites images)       | SKIPPED          |
| `json.Marshal`             | Applied (re-encodes modified payload)                    | SKIPPED          |
| `privacy.MaskRequest`      | Applied (masks secrets/PII)                              | SKIPPED          |
| `OptimizeWhitespace`       | Applied in ProxyTransparent (system prompt)              | SKIPPED          |
| `DeduplicateSentences`     | Applied in ProxyTransparent (system prompt)              | SKIPPED          |

Handler-side skip (lines 524-621):

```go
if !transparent {
    // ... full modification pipeline ...
} else {
    body = rawBody
}
```

Proxy-side skip (line 770):

```go
if (maskResult == nil || (!maskResult.HasSecrets && !maskResult.HasPII)) && (opts == nil || !opts.Transparent) {
    // whitespace optimization and dedup
}
```


## Header Handling

### anthropic-version

| Mode        | Behavior                                                                                                                               |
|-------------|----------------------------------------------------------------------------------------------------------------------------------------|
| Normal      | Set to `cfg.AnthropicVersion` (static config value)                                                                                    |
| Transparent | Forwarded from client's `anthropic-version` header as-is. Falls back to `cfg.AnthropicVersion` only if client did not send the header. |

```go
// proxy/anthropic.go - lines 848-856
if opts != nil && opts.Transparent {
    if av := r.Header.Get("anthropic-version"); av != "" {
        httpReq.Header.Set("anthropic-version", av)
    } else {
        httpReq.Header.Set("anthropic-version", p.cfg.AnthropicVersion)
    }
} else {
    httpReq.Header.Set("anthropic-version", p.cfg.AnthropicVersion)
}
```

### anthropic-beta

| Mode        | Behavior                                                                                                                                                                                              |
|-------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Normal      | Client's beta header is merged with `ExtraHeaders["anthropic-beta"]` via `mergeBetas()`. Unsupported betas are stripped by `stripUnsupportedBetas()`.                                                 |
| Transparent | Client's `anthropic-beta` header is forwarded directly, no merge. `ExtraHeaders["anthropic-beta"]` is explicitly skipped. Other ExtraHeaders are still applied. `stripUnsupportedBetas()` is skipped. |

```go
// proxy/anthropic.go - lines 859-890
if opts.Transparent {
    if incoming := r.Header.Get("anthropic-beta"); incoming != "" {
        httpReq.Header.Set("anthropic-beta", incoming)
    }
    for k, v := range opts.ExtraHeaders {
        if k == "anthropic-beta" {
            continue  // skip merge
        }
        if k == "Accept" && isStream {
            continue
        }
        httpReq.Header.Set(k, v)
    }
} else {
    // ... merge logic ...
}

// Skip beta stripping in transparent mode.
if opts == nil || !opts.Transparent {
    stripUnsupportedBetas(&httpReq.Header, model)
}
```

### Other headers (unchanged between modes)

These headers are always set regardless of transparent mode:

| Header                       | Source                                                       |
|------------------------------|--------------------------------------------------------------|
| `Content-Type`               | Always `application/json`                                    |
| `Authorization`              | `Bearer <apiKey>` (client's OAuth token in transparent mode) |
| `x-client-request-id`        | From client header or generated UUID                         |
| `X-Claude-Code-Session-Id`   | From client header or generated UUID                         |
| `x-anthropic-billing-header` | From client header (if present)                              |
| `x-mcp-client-session-id`    | From client header (if present)                              |


## Configuration

### ProxyOptions struct

```go
type ProxyOptions struct {
    AuthMode         string
    UpstreamOverride string
    ExtraHeaders     map[string]string
    Transparent      bool  // skip all body/header modifications
    OnAuthError      func(oldKey string) (newKey string, ok bool)
    OnRateLimitError func(oldKey string) (newKey string, ok bool)
}
```

### Provider route table

Transparent mode requires the provider to be configured as `claude-oauth` in the resolver:

```yaml
# Example provider route entry
- provider_id: claude-oauth
  format: anthropic
  auth_mode: bearer
  models:
    - claude-sonnet-4-6
    - claude-opus-4-7
    - claude-sonnet-4-20250514
    - claude-haiku-4-5-20251001
```

### No environment variables

Transparent mode has no dedicated environment variable toggle. It is automatically triggered by the detection logic based on provider ID and auth header. There is no way to force transparent mode for non-claude-oauth providers.

### Profile interaction

If a profile is active (`X-Profile` header or `arl_` token), the `profileOpts.Transparent` field is set from the transparent detection result:

```go
// handler/handler.go - line 627
profileOpts.Transparent = transparent
```

If the profile has `PassthroughAuth: true` and the resolved provider is `claude-oauth`, transparent mode still activates as long as the original request came with a valid Bearer token (not `arl_` prefixed).


## Testing

### Verify transparent mode is active

Check gateway logs for the `claude-oauth passthrough activated` message:

```
level=INFO msg="claude-oauth passthrough activated" token_prefix=eyJhbGciOi...
```

### Test with sonnet

```bash
curl -v https://your-gateway/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $(cat ~/.claude/credentials.json | jq -r '.OAuthToken')" \
  -H "anthropic-version: 2023-06-01" \
  -H "anthropic-beta: prompt-caching-scope-2026-01-05" \
  -d '{
    "model": "claude-sonnet-4-6",
    "max_tokens": 1024,
    "system": [
      {
        "type": "text",
        "text": "You are a helpful assistant.",
        "cache_control": {"type": "ephemeral"}
      }
    ],
    "messages": [
      {"role": "user", "content": "Say hello in one word."}
    ],
    "stream": true
  }'
```

### Test with opus

```bash
curl -v https://your-gateway/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $(cat ~/.claude/credentials.json | jq -r '.OAuthToken')" \
  -H "anthropic-version: 2023-06-01" \
  -H "anthropic-beta: prompt-caching-scope-2026-01-05" \
  -d '{
    "model": "claude-opus-4-7",
    "max_tokens": 2048,
    "system": [
      {
        "type": "text",
        "text": "You are a code reviewer.",
        "cache_control": {"type": "ephemeral"}
      }
    ],
    "messages": [
      {"role": "user", "content": "Review this function for bugs: func add(a, b) { return a - b }"}
    ],
    "stream": true
  }'
```

### Verify cache_control is preserved

Send two identical requests in sequence. If transparent mode is working, the second request should show cache hit metrics in the response usage:

```json
{
  "usage": {
    "input_tokens": 15,
    "output_tokens": 5,
    "cache_creation_input_tokens": 0,
    "cache_read_input_tokens": 27
  }
}
```

If `cache_creation_input_tokens` stays > 0 on subsequent requests, transparent mode is NOT active and `cache_control` markers are being destroyed.

### Verify headers are forwarded

Check the gateway debug log for upstream request details:

```
level=INFO msg="upstream req" model=claude-sonnet-4-6 beta="prompt-caching-scope-2026-01-05"
```

If `beta` is empty or different from what the CLI sent, transparent mode header forwarding is not working.

### Compare with normal mode

To confirm the difference, send a request with an `arl_` API key instead of a Bearer token. This bypasses transparent mode:

```bash
curl -v https://your-gateway/v1/messages \
  -H "Content-Type: application/json" \
  -H "x-api-key: arl_your_api_key_here" \
  -H "anthropic-version: 2023-06-01" \
  -d '{
    "model": "claude-sonnet-4-6",
    "max_tokens": 1024,
    "system": [
      {
        "type": "text",
        "text": "You are a helpful assistant.",
        "cache_control": {"type": "ephemeral"}
      }
    ],
    "messages": [
      {"role": "user", "content": "Say hello in one word."}
    ]
  }'
```

In this case the `system` array will be flattened to a string, `cache_control` will be lost, and the `anthropic-beta` header will be set from provider ExtraHeaders (not the client).
