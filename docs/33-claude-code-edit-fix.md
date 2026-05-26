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

## Verification

```
go build ./...                  # clean
go test ./handler/ -count=1    # all pass (including 11 new tests)
go test ./proxy/ -run TestGracefulStreamCloseOnScannerError  # pass
```
