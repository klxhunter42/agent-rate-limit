# Claude Code Edit Failure Fix - 2026-05-27

## Root Causes Found (10-agent cross-check)

| ID | Severity | Root Cause | File | Fix |
|----|----------|-----------|------|-----|
| P0-1 | Critical | max_tokens capped at 64K, truncates extended thinking mid-tool_use | handler.go, optimizer.go | Raised to 128K for opus/sonnet |
| P0-2 | Critical | Stream scanner error faked `end_turn`, hiding incomplete tool_use JSON | anthropic.go | Now emits `event: error` SSE |
| P0-1b | Critical | countCacheControlBlocks missed `tools` array, Anthropic 400 error | handler.go | Added tools iteration |
| P1-2 | High | ToolComp truncated diffs to 50 lines, model edits from incomplete context | toolcomp.go | Raised to 200 lines |
| P1-3 | High | 31 max retries + 4h timeout = 90+ min request hang | config.go, shared_transport.go | Reduced to 9 retries, 60s timeout |

## What Was NOT Changed (and why)

- **Rate limiting cascade** (adaptive 50% slash, 5-min cooldown): Requires tuning with real traffic data, not safe to change without metrics.
- **fixToolPairBoundary single-step lookback**: Edge case only during context overflow recovery; changing risks breaking existing truncation logic.
- **Privacy masking defaults**: Already safe (EMAIL + PHONE only).

## Environment Variable Overrides

All defaults can be reverted via env vars:

```
UPSTREAM_MAX_RETRIES=5        # was 20
TRANSIENT_RETRY_MAX=3         # was 10
TOOLCOMP_MAX_LINES=200        # was 50
```

## Follow-up Fix: Privacy Prompt Placeholder Leak (2026-05-27c)

Gateway response was leaking `[[ENV_PASSWORD_XXXX]]` placeholder format to users because the privacy prompt explicitly taught Claude the `[[TYPE_N]]` naming convention.

| ID | Severity | Root Cause | File | Fix |
|----|----------|-----------|------|-----|
| P0 | Critical | Privacy prompt showed format examples (`[[IP_ADDRESS_1]]`), "anonymized" concept, Correct/Wrong usage | `privacy/pipeline.go` | Rewrote to minimal: "Preserve all tokens enclosed in [[...]] exactly as written" |
| P1 | High | `leftoverPlaceholderRe` regex `\d+` missed hallucinated variants like `[[ENV_PASSWORD_XXXX]]` | `privacy/masking/stream.go` | Broadened to `[A-Za-z0-9]+` |

## Follow-up Fix: Tools Not Called via Claude OAuth CLI (2026-05-27d)

Claude Code CLI through gateway did not call tools while VSCode Claude Code panel worked fine. Same `arl_*` profile, same `claude-oauth` provider.

### Root Causes (8-agent investigation)

| ID | Severity | Root Cause | File | Fix |
|----|----------|-----------|------|-----|
| P0 | Critical | Tool filter dropped ~10 of 25 tools. Filter activated at >15 tools (MaxTools=15), scored all 0 due to empty recentText, kept only 4 AlwaysKeep (Read,Edit,Write,Bash) + top 11 by description length | `handler.go:1259` | Guard with `!transparent` |
| P0 | Critical | Content extraction `mm["content"].(string)` failed for array-format `[{"type":"text","text":"..."}]`, producing empty recentText | `handler.go:1273` | Type switch: string + []any |
| P1 | High | `injectCachedTools` mutated transparent passthrough payload | `handler.go:1115` | Guard with `!transparent` |
| P1 | High | `desctrim` modified tool descriptions in transparent passthrough | `handler.go:1225` | Guard with `!transparent` |

### Why Transparent OAuth Should Skip Optimizers

Transparent mode = gateway forwards client's own OAuth token to Anthropic. The optimizer pipeline (tool filter, desctrim, tool cache injection) was designed for Z.AI where token budgets are constrained. For transparent passthrough:

- No token budget constraint - client pays Anthropic directly
- Client chose its tools - gateway should not remove them
- Anthropic supports up to 128 tools (25 is well within limits)
- Tool filter heuristic (intent + keyword scoring) can wrongly drop needed tools

### The Content Extraction Bug

Claude Code sends message content as arrays:
```json
{"role": "user", "content": [{"type": "text", "text": "Hello"}]}
```

Old code only handled string: `mm["content"].(string)` - silently failed, `recentText` stayed empty.

With empty text:
1. `ClassifyIntent("")` returns `"code"` (default)
2. `extractKeywords("")` returns empty map
3. All tools score 0.0 (only base score from description length)
4. Filter keeps 4 AlwaysKeep + top 11 by description length = 15
5. Remaining ~10 tools (Agent, Glob, Grep, WebSearch, etc.) dropped

Fix: type switch handling both `string` and `[]any` content blocks.

## Verification

```
go build ./...                  # clean
go test ./handler/ -count=1    # all pass (including 11 new tests)
go test ./proxy/ -run TestGracefulStreamCloseOnScannerError  # pass
```
