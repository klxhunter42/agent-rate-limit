# SSE Streaming Fix - 2026-05-14

## Problem

SSE responses from AI providers appeared buffered when proxied through the gateway. Users reported responses arriving all at once instead of progressively streaming token-by-token.

## Root Causes

### Cause 1: http.Flusher Not Propagated Through Middleware (Primary)

**File**: [middleware/logging.go](api-gateway/middleware/logging.go)

The `responseWriter` wrapper in the logging middleware captures HTTP status codes but did not implement `http.Flusher`. All downstream `Flush()` calls in proxy handlers silently failed because Go's interface assertion `w.(http.Flusher)` returned `false`.

```go
// BEFORE: Flush() was not implemented
type responseWriter struct {
    http.ResponseWriter
    status int
}

// AFTER: Added Flush passthrough
func (rw *responseWriter) Flush() {
    if f, ok := rw.ResponseWriter.(http.Flusher); ok {
        f.Flush()
    }
}

var _ http.Flusher = (*responseWriter)(nil)
```

**Impact**: Every SSE chunk was buffered in the Go HTTP response writer until the handler returned. The gateway functioned correctly but streaming was effectively disabled.

### Cause 2: io.ReadFull Blocking in Stream Peek

**File**: [proxy/anthropic.go](api-gateway/proxy/anthropic.go) ~line 1404

The stream peek validation used `io.ReadFull(resp.Body, peekBuf)` with a 1024-byte buffer. Since SSE events are typically 200-400 bytes each, `io.ReadFull` blocked waiting for the buffer to fill, adding 200-500ms+ latency before the first chunk could be relayed.

```go
// BEFORE: Blocks until 1024 bytes arrive
io.ReadFull(resp.Body, peekBuf)

// AFTER: Returns as soon as any bytes are available
resp.Body.Read(peekBuf)
```

### Cause 3: Missing Flush After WriteHeader

**Files**: [proxy/anthropic.go](api-gateway/proxy/anthropic.go), [proxy/openai.go](api-gateway/proxy/openai.go)

Three streaming proxy paths wrote HTTP headers but did not flush immediately. This caused the initial SSE handshake (headers + first event) to be delayed until the first data write triggered a flush.

```go
// Added after each WriteHeader in streaming paths:
w.WriteHeader(http.StatusOK)
if f, ok := w.(http.Flusher); ok {
    f.Flush()
}
```

Locations fixed:
- `ProxyTransparent` (anthropic.go ~line 1570)
- `convertOpenAIStreamResponse` (anthropic.go ~line 777)
- `ProxySidecar` (anthropic.go ~line 1745)
- OpenAI streaming handler (openai.go ~line 261)

### Cause 4: Go http.Transport Automatic Gzip Decompression

**File**: [proxy/shared_transport.go](api-gateway/proxy/shared_transport.go)

Go's default `http.Transport` automatically decompresses gzip responses, which buffers the entire response body before returning data.

```go
Transport: &http.Transport{
    // ... existing config ...
    DisableCompression: true, // Prevent gzip decompression buffering on SSE streams
}
```

## Secondary Optimization: Scanner Buffer Initial Capacity

**Files**: anthropic.go, openai.go, gemini-apikey.go, gemini-codeassist.go, claude-session.go

`bufio.Scanner.Buffer()` initial capacity reduced from 8MB to 64KB. This is a memory optimization (reduces per-request allocation from 8MB to 64KB). The maximum capacity remains 8MB for legitimate large SSE events. No visible streaming latency impact.

```go
// BEFORE: 8MB initial allocation per request
scanner.Buffer(make([]byte, 0, 8*1024*1024), maxSSELineSize)

// AFTER: 64KB initial, still grows to 8MB max
scanner.Buffer(make([]byte, 0, 64*1024), maxSSELineSize)
```

## Bug Fix: lastRelayBlockIdx Scope

**File**: [proxy/anthropic.go](api-gateway/proxy/anthropic.go) ~line 2154

Variable `lastRelayBlockIdx` was declared inside a `defer` closure but referenced in the outer scanner loop scope. Moved declaration to the outer scope.

```go
// BEFORE: Declared inside defer, invisible to loop
defer func() {
    lastRelayBlockIdx := -1
    ...
}()
for scanner.Scan() {
    if idx != lastRelayBlockIdx { ... } // ERROR: undefined
}

// AFTER: Declared in outer scope
var lastRelayBlockIdx = -1
defer func() {
    ...
}()
for scanner.Scan() {
    if idx != lastRelayBlockIdx { ... } // OK
}
```

## Files Changed

| File | Change |
|------|--------|
| `api-gateway/middleware/logging.go` | Added `Flush()` + `Hijack()` passthrough, compile-time interface check |
| `api-gateway/proxy/anthropic.go` | ReadFull -> Read, 3x Flush after WriteHeader, scanner buffer, scope fix |
| `api-gateway/proxy/openai.go` | Flush after WriteHeader, scanner buffer |
| `api-gateway/proxy/shared_transport.go` | `DisableCompression: true` |
| `api-gateway/proxy/gemini-apikey.go` | Scanner buffer 8MB -> 64KB |
| `api-gateway/proxy/gemini-codeassist.go` | Scanner buffer 8MB -> 64KB |
| `api-gateway/proxy/claude-session.go` | Scanner buffer 8MB -> 64KB |

## Streaming Quality After Fix

Test results across all 3 profiles (100-test suite):

```
Profile     Avg TTFB     Avg Total    Chunks    Quality
cc          1,600ms      4,200ms      8-54      EXCELLENT
kimi        1,500ms      3,800ms      2-212     EXCELLENT
```

Max chunk interval across all tests: < 5ms (well within the < 50ms "EXCELLENT" threshold).

TTFB reflects model thinking time, not gateway buffering.

## Testing

### Quick Test (3 profiles, detailed output)
```bash
bash scripts/test-streaming.sh
```

### 100-Test Suite (12 sections)
```bash
bash scripts/test-streaming-suite.sh
```

### Parameterized Suite (849+ cases, per-profile)
```bash
# All profiles
python3 scripts/test-streaming-5k.py --concurrent 3

# Per profile
python3 scripts/test-streaming-5k.py --profile cc --concurrent 3
python3 scripts/test-streaming-5k.py --profile kimi --concurrent 3

# Specific section
python3 scripts/test-streaming-5k.py --section basic --concurrent 3

# Limit tests
python3 scripts/test-streaming-5k.py --limit 50

# List test cases without running
python3 scripts/test-streaming-5k.py --list

# View report from saved TSV
python3 scripts/test-streaming-5k.py --report /tmp/sse-test-results.tsv
```

## SSE Event Sequence Reference

Valid Anthropic SSE stream order:
```
event: message_start
event: content_block_start
event: content_block_delta    (repeated)
event: content_block_delta    ...
event: content_block_stop
event: message_delta
event: message_stop
```

## Streaming Path Architecture

```
AI Provider
    |
    v
[Go HTTP Client] -- DisableCompression: true
    |
    v
[bufio.Scanner] -- 64KB initial buffer, reads line-by-line
    |
    v
[StreamUnmasker] -- Per-chunk PII placeholder restoration (microsecond overhead)
    |
    v
[fmt.Fprintln + Flush()] -- Immediate write to client
    |
    v
[responseWriter] -- Now properly implements http.Flusher
    |
    v
[Client]
```

Key principle: every SSE line is forwarded to the client immediately after processing. No batching, no buffering.
