# Response Unmasking Audit & Fixes

Date: 2026-04-22

## Overview

Security audit of response unmasking across all proxy handlers (Anthropic, OpenAI, Gemini, Z.AI/GLM, Claude Session) for both streaming and non-streaming paths, with GLM mode true and false.

## Findings & Fixes

### 1. [HIGH] relayStreamWithTracking - No streaming unmask (Anthropic transparent proxy)

**File:** `api-gateway/proxy/anthropic.go`

**Problem:** The `relayStreamWithTracking` method is the default streaming path for Anthropic-native upstream (`ProxyTransparent`). It created a `StreamUnmasker` but never called `ProcessChunk()` on any SSE data. Every SSE line was relayed raw to the client, meaning masked placeholders like `[[API_KEY_SK_1]]`, `[[PERSON_1]]`, `[[EMAIL_ADDRESS_1]]` passed through unmodified.

Additionally, the `Flush()` at the end wrote raw text instead of a properly formatted SSE event.

**Fix:** Parse `content_block_delta` SSE events, extract the text field, apply `ProcessChunk()`, re-serialize, and relay. Fixed `Flush()` to emit a proper SSE event with correct JSON format. Non-data lines (event type, empty lines) are still relayed as-is since they contain no text content.

**Impact:** This is the default streaming path for Anthropic providers. Every streaming request through `ProxyTransparent` was returning masked placeholders to the client instead of original secrets/PII.

---

### 2. [HIGH] relayOpenAIStream - Missing unmasker.Flush()

**File:** `api-gateway/proxy/openai.go`

**Problem:** `relayOpenAIStream` called `ProcessChunk()` per text chunk but never called `Flush()` after the scanner loop. When a placeholder spans an SSE chunk boundary (e.g., `[[PERSON` in one chunk and `_1]]` in the next), the partial placeholder remains buffered. Without `Flush()`, those buffered characters are silently dropped.

**Fix:** Added `Flush()` call after the scanner loop, before token tracking. Emits remaining text as a proper `content_block_delta` SSE event, matching the pattern used in `convertOpenAIStreamResponse` and `relayGeminiStream`.

**Impact:** Truncated text at stream boundaries when masked placeholders are split across chunks.

---

### 3. [MEDIUM] Upstream error responses forwarded without unmasking

**Files:**
- `api-gateway/proxy/openai.go` - `OpenAIProxy.ProxyRequest` error path
- `api-gateway/proxy/gemini-apikey.go` - `GeminiAPIProxy.ProxyGemini` error path
- `api-gateway/proxy/anthropic.go` - `convertOpenAIResponse` error path

**Problem:** When upstream returned a non-200 status, all proxy handlers forwarded the raw upstream error body without unmasking. If the upstream echoed back masked placeholders in error messages (e.g., content policy violations that include the prompt), the client received placeholder strings.

**Fix:** Changed `io.Copy(w, resp.Body)` to `io.ReadAll` + `UnmaskResponse` before writing to client. Applied consistently across all three proxy error paths.

---

### 4. [MEDIUM] ClaudeSessionProxy - No masking/unmasking integration

**File:** `api-gateway/proxy/claude-session.go`

**Problem:** `ProxySession` and `convertSessionSSE` accepted no `maskResult` parameter and had zero privacy pipeline integration. While dormant (not wired into the main handler routing), activating it would bypass the entire privacy pipeline.

**Fix:**
- Added `privacy` and `masking` imports
- Added `maskResult *privacy.MaskResult` parameter to `ProxySession`
- Added `maskResult` parameter to `convertSessionSSE`
- Created `StreamUnmasker` when masking is active
- Applied `ProcessChunk()` to each completion text chunk
- Added `Flush()` at stream end with proper SSE event format

**Note:** Caller sites need to pass `maskResult` when activating this proxy path.

---

### 5. [MEDIUM] GeminiCodeAssist error body unmasking

**File:** `api-gateway/proxy/gemini-codeassist.go`

**Problem:** Error responses from upstream were included in the client-facing JSON error message without unmasking. If `errBody` contained masked placeholders, the client saw raw placeholder strings.

**Fix:** Added `UnmaskResponse` call on `errBody` before encoding the error response to client.

---

## GLM Mode Analysis

GLM mode toggle does not create separate code paths for masking/unmasking. The `maskResult` is computed once in the handler layer before routing to any proxy, and passed through regardless of GLM mode. Both GLM mode true and false share the same proxy handler code, so fixes apply uniformly to both modes.

## Unmasking Coverage Matrix (After Fix)

| Proxy Handler | Non-Stream | Stream | Error | Flush |
|---|---|---|---|---|
| AnthropicProxy (transparent) | OK | FIXED | N/A (passes through) | FIXED |
| AnthropicProxy (OpenAI convert) | OK | OK | FIXED | OK |
| OpenAIProxy | OK | FIXED | FIXED | FIXED |
| GeminiAPIProxy | OK | OK | FIXED | OK |
| GeminiCodeAssistProxy | OK | OK | FIXED | OK |
| ClaudeSessionProxy | N/A (stream only) | FIXED | N/A | FIXED |

## Files Changed

- `api-gateway/proxy/anthropic.go` - Fix #1, Fix #3 (convertOpenAIResponse error path)
- `api-gateway/proxy/openai.go` - Fix #2, Fix #3
- `api-gateway/proxy/gemini-apikey.go` - Fix #3
- `api-gateway/proxy/gemini-codeassist.go` - Fix #5
- `api-gateway/proxy/claude-session.go` - Fix #4
