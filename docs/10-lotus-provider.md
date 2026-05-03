# Lotus Provider -- OpenAI-Compatible Tool Use Bridge

## Overview

Lotus is an OpenAI-compatible LLM endpoint (`api-cpxis.lotuss.com/llm`) that serves as a backend for Claude Code through the gateway. The gateway converts Anthropic API format to OpenAI format and back.

## Architecture Diagram

```
+-------------------------------------------------------------------------+
| Lotus Tool Use Bridge Architecture                                      |
|                                                                         |
| Claude Code (client)   API Gateway (Go)           Lotus LLM             |
| +--------------+  +------------------+  +-----------+                   |
| |              |  |  Anthropic       |  | OpenAI    |                   |
| | sends:       |  |  API format      |  | fmt       |  OpenAI-          |
| | model:       |--------->| 1 Resolve model  |------>| compatible       |
| | claude-*     |  |  lotus-*         |  | endpoint  |                   |
| |              |  | 2 Override to    |<------| returns  |                   |
| | tools:       |  |  "default"       |  |           |                   |
| | [Write,Bash, |  | 3 Convert tools  |  |           |                   |
| |  Read,...]   |  |  Anthropic->     |  | tool_calls|                   |
| |              |  |  OpenAI func     |  |           |                   |
| | receives:    |  | 4 Add system     |  +-----------+                   |
| | tool_use     |  |  role message    |                                 |
| | SSE stream   |  | 5 tool_choice:   |                                 |
| +--------------+  |  auto            |                                 |
|                   | 6 Convert resp   |                                 |
|                   |  OpenAI->        |                                 |
|                   |  Anthropic       |                                 |
|                   +------------------+                                 |
+-------------------------------------------------------------------------+
```

## Request Flow (step-by-step)

```
Claude Code              Gateway                  Lotus
|                        |                         |
| POST /v1/messages      |                         |
| model: claude-sonnet-4-6                         |
| tools: [Write,Bash,Read,...]                     |
| stream: true           |                         |
|----------------------->|                         |
|                        |                         |
|                        | Profile resolved        |
|                        | target = "lotus"        |
|                        |                         |
|                        | Model override:         |
|                        | claude-sonnet-4-6       |
|                        | -> "default"            |
|                        |                         |
|                        | max_tokens clamped:     |
|                        | 128000 -> 4096          |
|                        |                         |
|                        | AnthropicToOpenAI():    |
|                        | - tools: Anthropic schema
|                        |   -> OpenAI function fmt|
|                        | - system: -> "system"   |
|                        |   role message          |
|                        |   (not prepend to user) |
|                        | - tool_choice: "auto"   |
|                        | - messages: tool_use    |
|                        |   -> tool_calls,        |
|                        |   tool_result -> tool   |
|                        |   role                  |
|                        |                         |
|                        | POST /v1/chat/completions
|                        |------------------------>|
|                        |                         |
|                        | lotus processes         |
|                        | with function           |
|                        | calling                 |
|                        |                         |
|                        | SSE stream with tool_calls
|                        |<------------------------|
|                        |                         |
|                        | Convert stream:         |
|                        | OpenAI delta tool_calls |
|                        | -> Anthropic tool_use SSE
|                        |                         |
| SSE: message_start     |                         |
| SSE: content_block_start (text)                  |
| SSE: content_block_delta                         |
| SSE: content_block_stop                          |
| SSE: content_block_start                         |
|   (tool_use: Write)                              |
| SSE: input_json_delta                            |
| SSE: content_block_stop                          |
| SSE: message_delta                               |
|   stop_reason: "tool_use"                        |
| SSE: message_stop                                |
|<-----------------------|                         |
|                        |                         |
| Claude Code executes tool                         |
| (creates file, runs cmd, etc.)                   |
|                        |                         |
| POST /v1/messages (2nd turn)                     |
| with tool_result       |                         |
|----------------------->|                         |
|                        | tool_result -> tool role|
|                        |------------------------>|
|                        |<------------------------|
| ...more turns...       |                         |
```

## Problems Found and Fixes

### Problem 1: Lotus did not use tools at all (text-only responses)

**Symptom**: Claude Code sends "create file" request but lotus responds with plain text, doesn't call Write tool.

**Root cause**: Gateway used `toolMode = "simulate"` (prompt injection) but lotus model didn't follow `<tool_call name="...">` format in prompt.

**Fix**: Changed `providerToolMode["lotus"]` from `"simulate"` to `"native"` -- use OpenAI function calling format instead.

```
File: api-gateway/provider/resolver.go
var providerToolMode = map[string]string{
    "lotus": "native", // was "simulate"
}
```

### Problem 2: Streaming SSE missing `message_start` event

**Symptom**: Claude Code didn't process stream response from lotus at all (didn't see tool_use).

**Root cause**: Bug in `relayOpenAIStreamChunk()` -- `started` variable was inverted.

```
Before (WRONG):
started := !isContinuation
// isContinuation=false -> started=true -> skip message_start

After (CORRECT):
started := isContinuation
// isContinuation=false -> started=false -> emit message_start
```

**Fix**: `api-gateway/proxy/openai.go:397` -- changed `!isContinuation` to `isContinuation`

### Problem 3: Lotus model did not choose to use tools

**Symptom**: Lotus model responds with text instead of using tools, even though tools were sent.

**Root cause**: OpenAI request didn't have `tool_choice` field, so model wasn't encouraged to use tools.

**Fix**: Added `tool_choice: "auto"` as default for native mode.

```go
File: api-gateway/proxy/anthropic.go
// When no tool_choice specified but native mode with tools:
} else if toolMode == "native" {
    if _, hasTools := result["tools"]; hasTools {
        result["tool_choice"] = "auto"
    }
}
```

### Problem 4: Lotus did not understand working directory

**Symptom**: Lotus creates files at wrong/random paths, doesn't match working directory.

**Root cause**: System prompt was injected into first user message instead of using `system` role (done because Z.AI doesn't support system role but lotus does).

**Fix**: For native mode, use real system role message + inject tool-usage hint.

```go
File: api-gateway/proxy/anthropic.go

if toolMode == "native" {
    // Use real system role (OpenAI supports this)
    toolHint := "IMPORTANT: You have access to tools..."
    sysMsg := {"role": "system", "content": systemText + toolHint}
    messages = [sysMsg] + messages
} else {
    // Z.AI: prepend to first user message (no system role support)
    first["content"] = systemText + "\n\n" + userContent
}
```

### Problem 5: Context overflow (400 error) when conversation gets long

**Symptom**: Using lotus for a while then getting `400 max_tokens too large: 40960 context=40000, input=36063` or `400 input tokens exceeds 40000`

**Root cause**: Lotus context = 40,000 tokens (very small compared to claude 200k). Claude Code sends entire conversation history every request, when conversation gets long input tokens exceed 40k.

**Fix**: 3-layer defense in `ProxyOpenAI`:

**Layer 1: Auto-compaction (truncate old messages)**
- Estimate input tokens from OpenAI request body
- If estInput > 32000 (80% of 40000):
  - Keep: system message + last 4 messages (2 turns)
  - Don't split mid tool_call/tool_result sequence
  - Insert compaction notice for context continuity
  - Re-estimate and continue to Layer 2

**Layer 2: Dynamic max_tokens reduction**
- After compaction (or if under threshold):
  - available = 40000 - estInput - 1500 buffer
  - if max_tokens > available -> reduce max_tokens
- Ensures request never exceeds lotus context limit

**Layer 3: Retry on 400 (if estimation was wrong)**
- If lotus returns 400 with "max_tokens ...context length"
- Parse actual input token count from error message
- Retry with: max_tokens = 40000 - actualInput - 500

```
File: api-gateway/proxy/openai.go

// Layer 1: auto-compact when context nearly full
if estInput > 32000 {
    compactOpenAIMessages(openaiReq, estInput, 40000)
    // Keep: system + last 4 messages + compaction notice
    // Never split tool_call/tool_result pairs
    estInput = re-estimate from compacted body
}

// Layer 2: reduce max_tokens to fit
available := 40000 - estInput - 1500
if max_tokens > available {
    openaiReq["max_tokens"] = available
}

// Layer 3: catch 400, parse actual tokens, retry
if resp.StatusCode == 400 && strings.Contains(body, "max_tokens") {
    actualInput = parse from error message
    openaiReq["max_tokens"] = 40000 - actualInput - 500
    retry request
}
```

**Bug found during fix**: `buildDecision()` in `resolver.go` was not setting `MaxContinuations` field from `providerContinuations` map. Result: `maxContinuations=0` for lotus, making all 3 layers inactive. Fixed by adding `MaxContinuations: providerContinuations[providerID]` to the return struct.

### Problem 6: Simulate mode was dead code

**Symptom**: N/A (code cleanup after lotus switched to native mode)

**Root cause**: `toolMode = "simulate"` was the original approach using prompt injection with `<tool_call name="...">` tags. After switching lotus to `native` mode, no provider used simulate. Dead code: ~140 lines in `tool_sim.go` + ~120 lines in `openai.go` + tests.

**Fix**: Removed entirely:
- Deleted `proxy/tool_sim.go`, `proxy/tool_sim_test.go`, `proxy/tool_sim_integration_test.go`
- Removed simulate streaming path from `openai.go` (buffer, emitToolSimResponse)
- Removed simulate branch from `anthropic.go` (prompt injection)
- Only `native` and `""` (no tools) modes remain

### Problem 7: "Mismatched content block type" during auto-continuation

**Symptom**: `API Error: 400 Mismatched content block type content_block_delta text` when lotus auto-continuation triggers.

**Root cause**: In `relayOpenAIStreamChunk`, when `isContinuation=true` the code sets `started=true` to skip `message_start` (correct) but also skips `content_block_start`. The client receives `content_block_delta` events without a matching `content_block_start`, causing the protocol violation.

**Fix**: Added `else if !textBlockOpen && !toolBlockOpen` branch after the `!started` check. For continuation streams, this emits a new `content_block_start` at `contentBlockIdx+1` before the first text delta, then sets `textBlockOpen=true`.

## Configuration

```
# resolver.go - Lotus route config
providerRouteTable["lotus"] = {
    Format: FormatOpenAI,
    AuthMode: "bearer",
    URL: "/v1/chat/completions",
    ModelOverride: "default", // overrides claude-sonnet-4-6 -> "default"
    MaxTokens: 4096,           // clamped from 128000
}

providerContinuations["lotus"] = 3  // auto-continue up to 3 times on max_tokens
providerToolMode["lotus"] = "native"  // OpenAI function calling

# Model routing
modelRules: {"lotus-", []string{"lotus"}}

# Profile: "Lotus"
target: "lotus"
accountIds: ["mLoH9s"]
```

## Supported Tools (forwarded from Claude Code)

| Tool | Anthropic Format | OpenAI Format | Status |
|---|---|---|---|
| Bash | input_schema | function.parameters | Works |
| Write | input_schema | function.parameters | Works |
| Read | input_schema | function.parameters | Works |
| Edit | input_schema | function.parameters | Forwarded |
| MultiEdit | input_schema | function.parameters | Forwarded |
| Glob | input_schema | function.parameters | Forwarded |
| Grep | input_schema | function.parameters | Forwarded |
| LS | input_schema | function.parameters | Forwarded |
| TodoRead | input_schema | function.parameters | Forwarded |
| TodoWrite | input_schema | function.parameters | Forwarded |
| WebFetch | input_schema | function.parameters | Forwarded |
| WebSearch | input_schema | function.parameters | Forwarded |

All tools are converted via `input_schema` -> `function.parameters` mapping. Whether the lotus model uses them correctly depends on model capability.

## Key Files

| File | Role |
|---|---|
| `api-gateway/provider/resolver.go` | Lotus route config, toolMode, MaxContinuations, modelRules |
| `api-gateway/proxy/openai.go` | Streaming SSE conversion, auto-continuation, auto-compact, max_tokens dynamic adjustment |
| `api-gateway/proxy/anthropic.go` | AnthropicToOpenAI conversion, system role, tool_choice |
| `api-gateway/handler/handler.go` | Request routing, profile resolution, optimizer, privacy masking |
