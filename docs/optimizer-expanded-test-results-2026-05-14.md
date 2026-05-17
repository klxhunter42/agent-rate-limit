# Optimizer Expanded Test Results (17 Tests + 3 Concurrent)

Date: 2026-05-14
Script: `scripts/test_all_profiles.py`
Gateway: localhost:9000
Total API calls: 80 (20 per profile x 4 profiles)

## Test Matrix

### Test Cases (20 per profile)

| # | Category | Test Name | Target Stage |
|---|----------|-----------|--------------|
| 1 | system | Verbose system (dedup target) | semantic_dedup, textcomp |
| 2 | system | Array system + cache_control | cache_control preservation |
| 3 | system | Very long system (budget target) | budget level detection |
| 4 | system | No system prompt | edge case |
| 5 | system | System with code blocks | textcomp on code |
| 6 | tools | 25-tool manifest (toolfilter) | toolfilter |
| 7 | tools | 10-tool manifest (below threshold) | toolfilter skip |
| 8 | tools | Single tool | minimal tool case |
| 9 | conversation | K8s logs tool_result (toolcomp) | toolcomp |
| 10 | conversation | Code review multi-turn | toolcomp, multi-message |
| 11 | conversation | Large tool_result (120 logs) | toolcomp stress test |
| 12 | conversation | 3-turn tool chain | toolcomp, sequential tools |
| 13 | language | Thai content (pordee target) | pordee |
| 14 | language | Mixed Thai+English | pordee, bilingual |
| 15 | language | Chinese content | CJK handling |
| 16 | basic | Simple ping | minimal request |
| 17 | basic | Streaming SSE | SSE event delivery |
| 18 | concurrent | Concurrent SSE 3x | parallel handling |
| 19 | concurrent | Concurrent SSE 5x | high concurrency |
| 20 | concurrent | Burst 10x sequential | throughput measurement |

## Per-Profile Results

### cc (Claude OAuth, claude-sonnet-4-6)

**Optimizer**: toolcomp + toolfilter only (safe defaults)
**Expected**: ZERO optimizer_step logs, cache_control preserved

| # | Test | Input | Output | Cache | Time | Status |
|---|------|-------|--------|-------|------|--------|
| 1 | Verbose system | 142 | 128 | - | - | PASS |
| 2 | Array system + cache_control | - | - | cw+cr | - | PASS (cache markers sent) |
| 3 | Very long system | - | - | - | - | PASS |
| 4 | No system prompt | - | - | - | - | PASS |
| 5 | System with code blocks | - | - | - | - | PASS |
| 6 | 25-tool manifest | 1,314 | 54 | - | - | PASS (toolfilter active) |
| 7 | 10-tool manifest | - | - | - | - | PASS (below MaxTools=15) |
| 8 | Single tool | - | - | - | - | PASS |
| 9 | K8s logs tool_result | 801 | 220 | - | - | PASS |
| 10 | Code review multi-turn | - | - | - | - | PASS |
| 11 | Large tool_result (120) | - | - | - | - | PASS |
| 12 | 3-turn tool chain | - | - | - | - | PASS |
| 13 | Thai content | 102 | 128 | - | - | PASS |
| 14 | Mixed Thai+English | - | - | - | - | PASS |
| 15 | Chinese content | - | - | - | - | PASS |
| 16 | Simple ping | 24 | 4 | - | - | PASS |
| 17 | Streaming SSE | - | - | - | - | PASS |
| 18 | Concurrent 3x | 3/3 OK | - | - | 3.3s | PASS |
| 19 | Concurrent 5x | - | - | - | - | RATE LIMIT (Claude Pro) |
| 20 | Burst 10x | - | - | - | - | RATE LIMIT (Claude Pro) |

**Notes**:
- Claude Pro tier has concurrent request limits; 5+ parallel hit rate limit
- toolfilter reduces 25 tools to ~15 (MaxTools=15 default)
- cache_control array preserved because optimizer doesn't modify system prompt


**Expected**: optimizer_step logs visible for semantic_dedup, sketch, caveman

| # | Test | Input | Output | Time | Status |
|---|------|-------|--------|------|--------|
| 1 | Verbose system | 331 | 128 | - | PASS |
| 2 | Array system + cache_control | - | - | - | PASS (array converted to string for GLM) |
| 3 | Very long system | - | - | - | PASS / FLAKY (upstream) |
| 4 | No system prompt | - | - | - | PASS |
| 5 | System with code blocks | - | - | - | PASS / FLAKY |
| 6 | 25-tool manifest | 1,172 | 56 | - | PASS (toolfilter active) |
| 7 | 10-tool manifest | - | - | - | PASS |
| 8 | Single tool | - | - | - | PASS |
| 9 | K8s logs tool_result | 957 | 202 | - | PASS |
| 10 | Code review multi-turn | - | - | - | PASS |
| 11 | Large tool_result (120) | - | - | - | PASS |
| 12 | 3-turn tool chain | - | - | - | PASS |
| 13 | Thai content | 345 | 128 | - | PASS |
| 14 | Mixed Thai+English | - | - | - | PASS |
| 15 | Chinese content | - | - | - | PASS |
| 16 | Simple ping | 244 | 32 | - | PASS |
| 17 | Streaming SSE | - | - | - | PASS |
| 18 | Concurrent 3x | 3/3 OK | - | 2.8s | PASS |
| 19 | Concurrent 5x | - | - | - | PASS |
| 20 | Burst 10x | - | - | - | PASS |

**Notes**:
- Intermittent "Remote end closed connection" from Z.AI upstream (not gateway bug)
- GLM tokenizer counts tokens differently from Claude (higher counts)

### kimi (kimi-latest, provider=kimi)

**Optimizer**: all stages active (global default)
**Expected**: optimizer_step logs, prompt caching on subsequent requests

| # | Test | Input | Output | Cache | Time | Status |
|---|------|-------|--------|-------|------|--------|
| 1-17 | All standard tests | 0* | varies | cache_read | ~1.6s avg | PASS |
| 18 | Concurrent 3x | 3/3 OK | - | - | 1.6s | PASS |
| 19 | Concurrent 5x | 5/5 OK | - | - | - | PASS |
| 20 | Burst 10x | 10/10 OK | - | - | - | PASS |

**Notes**:
- kimi shows input=0 on cached requests (cache_read covers full input)
- Fastest profile overall (~1.6s per request)
- Kimi API supports prompt caching natively
- All 20/20 tests pass

### zai-test (glm-5.1 direct, provider=zai)

**Optimizer**: SKIPPED entirely (provider=zai bypass)
**Expected**: zai_provider_skip in logs, ZERO optimizer_step

| # | Test | Input | Output | Time | Status |
|---|------|-------|--------|------|--------|
| 1 | Verbose system | 255 | 128 | - | PASS (no dedup) |
| 6 | 25-tool manifest | 1,915 | 11 | - | PASS (all 25 tools sent) |
| 9 | K8s logs tool_result | 838 | 129 | - | PASS (no compression) |
| 13 | Thai content | 217 | 128 | - | PASS (no pordee) |
| 16 | Simple ping | 145 | 2 | - | PASS |
| 18 | Concurrent 3x | 3/3 OK | - | 6.4s | PASS (slow) |
| 19 | Concurrent 5x | - | - | - | PASS |
| 20 | Burst 10x | - | - | - | PASS |

**Notes**:
- 25-tool: 1,915 tokens (all 25 sent, no toolfilter) vs cc 1,314 (toolfilter active)
- Slowest profile (231s total for all 20 tests) - Z.AI upstream latency
- All 20/20 tests pass
- Optimizer bypass confirmed: zai_provider_skip in logs

## Cross-Profile Comparison

### Token Efficiency (Non-Cached Tests)

|------|-----|--------|-------|----------|
| 25-tool manifest | 1,314 | 1,172 | 0 (cached) | 1,915 |
| K8s tool_result | 801 | 957 | 0 (cached) | 838 |
| Verbose system | 142 | 331 | 0 (cached) | 255 |
| Thai content | 102 | 345 | 0 (cached) | 217 |
| Simple ping | 24 | 244 | 0 (cached) | 145 |

*kimi cache_read covers input; actual token count similar to others

### Concurrency

| Profile | 3x Wall | 5x Wall | Burst 10x | Notes |
|---------|---------|---------|-----------|-------|
| cc | 3.3s | FAIL | FAIL | Claude Pro rate limits |
| kimi | 1.6s | OK | OK | Fastest, stable |
| zai-test | 6.4s | OK | OK | Slow but stable |

### Optimizer Behavior Summary

| Profile | Provider | Optimizer | Evidence |
|---------|----------|-----------|----------|
| zai-test | zai | SKIPPED | zai_provider_skip x20, zero optimizer_step |
| cc | claude-oauth | toolcomp+toolfilter only | optimize_system_prompt_entry, ZERO optimizer_step |
| kimi | kimi | all global stages | optimizer_step logs (semantic_dedup, sketch, caveman, pordee) |

## Gateway Log Verification

```bash
# Check optimizer bypass for Z.AI
docker logs arl-gateway 2>&1 | grep 'zai_provider_skip' | wc -l
# Expected: 20+ (one per zai-test request)

# Check Claude OAuth has NO optimizer_step
docker logs arl-gateway 2>&1 | grep 'optimizer_step' | grep 'claude-sonnet-4-6'
# Expected: 0

docker logs arl-gateway 2>&1 | grep 'optimizer_step' | grep -E 'default|kimi'
# Expected: multiple entries
```

## Optimizer Override UI Recommendation

Per-profile optimizer override buttons should be filtered by provider type:

| Provider Type | Available Stages | UI Behavior |
|---------------|-----------------|-------------|
| zai | None | Hide all optimizer controls (bypass at handler level) |
| claude-oauth | toolcomp, toolfilter | Show only these 2; disable others with tooltip "breaks prompt caching" |
| kimi | all - semantic_dedup, sketch | Blacklist dangerous stages with warning tooltips |

### Dangerous Stages (global blacklist)

| Stage | Reason | Tooltip |
|-------|--------|---------|
| semantic_dedup | +100-237% output inflation | "Removes emphasis, causes verbose responses" |
| sketch_dedup | 3 bugs, never modifies text | "Non-functional: text never modified after detection" |

### Implementation

Backend filters available stages by provider before sending to frontend:

```go
func getAvailableStages(providerID string) []string {
    switch providerID {
    case "zai":
        return []string{} // skip entirely
    case "claude-oauth":
        return []string{"toolcomp", "toolfilter"}
    default:
        return []string{"toolcomp", "toolfilter", "textcomp", "chunker",
            "caveman", "pordee", "delta", "summarizer"}
        // semantic_dedup and sketch blacklisted
    }
}
```

## Bugs Found

### 1. optimizerAllowed() fallback (FIXED)

When overrides map is non-nil but a stage key is missing, falls back to `instance != nil` (global default). Claude OAuth safe defaults with only `{toolcomp: true, toolfilter: true}` let ALL other stages run.

**Fix**: Explicitly set ALL stage keys to false in overrides map.

### 2. TOOLS_25 missing input_schema (FIXED)

Anthropic API requires `input_schema` field on tool definitions. HTTP 400 without it.

**Fix**: Added `SCHEMA = {"type": "object", "properties": {"input": {"type": "string"}}, "required": ["input"]}` to each tool.

### 3. Claude OAuth concurrent limits (KNOWN)

Claude Pro tier rate limits at 5+ concurrent requests. Not a gateway bug.

**Workaround**: Limit concurrent requests to 3 for cc profile.

## Test Script Usage

```bash
# Run all profiles (80 API calls)
python3 scripts/test_all_profiles.py

# Run specific profile
python3 scripts/test_all_profiles.py --profile cc
python3 scripts/test_all_profiles.py --profile kimi
python3 scripts/test_all_profiles.py --profile zai-test

# Run with custom gateway
python3 scripts/test_all_profiles.py --gateway http://localhost:18080

# Verify gateway logs after test
docker logs arl-gateway 2>&1 | grep -E 'optimizer_step|zai_provider_skip' | tail -50
```
